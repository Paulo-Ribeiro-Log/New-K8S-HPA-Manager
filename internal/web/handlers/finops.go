package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
)

// FinOpsHandler expõe análise de custo real de clusters AKS
type FinOpsHandler struct {
	kubeManager     *config.KubeConfigManager
	npRegistryStore *storage.NodePoolRegistryStore
	pricer          *finops.AzurePricer
	exchange        *finops.ExchangeRateProvider
	aiHandler       *AIDiagnosticsHandler // opcional — nil se AI não configurado
}

// NewFinOpsHandler cria o handler com as dependências compartilhadas.
// AzurePricer é inicializado uma única vez (cache SQLite interno).
func NewFinOpsHandler(kubeManager *config.KubeConfigManager, npRegistryStore *storage.NodePoolRegistryStore, aiHandler *AIDiagnosticsHandler) *FinOpsHandler {
	pricer, err := finops.NewAzurePricer("")
	if err != nil {
		log.Warn().Err(err).Msg("FinOps: falha ao inicializar AzurePricer, usando apenas fallback")
	}
	return &FinOpsHandler{
		kubeManager:     kubeManager,
		npRegistryStore: npRegistryStore,
		pricer:          pricer,
		exchange:        finops.NewExchangeRateProvider(),
		aiHandler:       aiHandler,
	}
}

// GetReport godoc
// GET /api/v1/finops/report?cluster=X[&namespaces=ns1,ns2]
//
// Retorna o relatório FinOps completo: custo de node pools, alocação por workload,
// cenários HPA e resumo de oportunidades de saving.
func (h *FinOpsHandler) GetReport(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}

	// Namespaces opcionais (CSV)
	var namespaces []string
	if ns := c.Query("namespaces"); ns != "" {
		for _, n := range strings.Split(ns, ",") {
			if trimmed := strings.TrimSpace(n); trimmed != "" {
				namespaces = append(namespaces, trimmed)
			}
		}
	}

	if h.npRegistryStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "NodePool Registry não está disponível. Execute um scan de node pools primeiro.",
		})
		return
	}

	// Buscar node pools do cluster no registry
	pools, err := h.npRegistryStore.GetAll(cluster)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao buscar node pools")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar node pools: " + err.Error()})
		return
	}
	if len(pools) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Nenhum node pool encontrado para o cluster '" + cluster + "'. Execute um scan de node pools primeiro.",
		})
		return
	}

	// Obter cliente K8s
	k8sClient, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao obter cliente K8s")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao conectar ao cluster: " + err.Error()})
		return
	}

	// Gerar relatório com timeout via context do Gin
	calc := finops.NewCalculator(h.pricer, h.exchange)
	report, err := calc.BuildReport(c.Request.Context(), cluster, k8sClient, pools, namespaces)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao gerar relatório")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gerar relatório FinOps: " + err.Error()})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Int("workloads", report.Summary.WorkloadsAnalyzed).
		Float64("cost_brl", report.Summary.TotalMonthlyCostBRL).
		Msg("FinOps: relatório gerado")

	c.JSON(http.StatusOK, report)
}

// GetPricing godoc
// GET /api/v1/finops/pricing?sku=Standard_D4s_v3
//
// Retorna o preço USD/hora de um VM SKU específico (com fonte: api ou fallback).
func (h *FinOpsHandler) GetPricing(c *gin.Context) {
	sku := c.Query("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'sku' é obrigatório"})
		return
	}

	if h.pricer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Azure Pricer não está disponível"})
		return
	}

	price, source, err := h.pricer.GetPrice(sku)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":  "Preço não encontrado para SKU: " + sku,
			"detail": err.Error(),
		})
		return
	}

	cpuCores, memGB := finops.GetVMSpecs(sku)
	c.JSON(http.StatusOK, gin.H{
		"sku":              sku,
		"price_usd_hour":   price,
		"price_usd_month":  price * finops.HoursPerMonth,
		"source":           source,
		"vm_cpu_cores":     cpuCores,
		"vm_memory_gb":     memGB,
	})
}

// RefreshPricing godoc
// POST /api/v1/finops/pricing/refresh
//
// Invalida o cache de preços SQLite e força nova busca na Azure Pricing API.
func (h *FinOpsHandler) RefreshPricing(c *gin.Context) {
	if h.pricer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Azure Pricer não está disponível"})
		return
	}

	if err := h.pricer.InvalidateCache(); err != nil {
		log.Error().Err(err).Msg("FinOps: falha ao invalidar cache de preços")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao invalidar cache: " + err.Error()})
		return
	}

	log.Info().Msg("FinOps: cache de preços Azure invalidado")
	c.JSON(http.StatusOK, gin.H{"message": "Cache de preços invalidado. Próxima consulta buscará preços atualizados da Azure Pricing API."})
}

// GetExchangeRate godoc
// GET /api/v1/finops/exchange-rate
//
// Retorna a cotação atual USD→BRL (com cache de 1h).
func (h *FinOpsHandler) GetExchangeRate(c *gin.Context) {
	rate, date := h.exchange.Get()
	c.JSON(http.StatusOK, gin.H{
		"usd_brl":  rate,
		"date":     date,
		"source":   "economia.awesomeapi.com.br",
		"fallback": rate == finops.DefaultExchangeRate,
	})
}

// AnalyzeReport godoc
// POST /api/v1/finops/analyze
//
// Envia o relatório FinOps para análise pelo provider AI configurado.
// Recebe ai_email e o FinOpsReport completo; retorna recomendações de rightsizing e saving.
func (h *FinOpsHandler) AnalyzeReport(c *gin.Context) {
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI não configurado neste servidor"})
		return
	}

	var req struct {
		AIEmail string          `json:"ai_email"`
		Report  finops.FinOpsReport `json:"report"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AIEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai_email e report são obrigatórios"})
		return
	}

	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	prompt := buildFinOpsPrompt(req.Report)
	analysis, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha na análise AI: " + err.Error()})
		return
	}

	log.Info().
		Str("ai_email", req.AIEmail).
		Str("cluster", req.Report.Cluster).
		Float64("cost_brl", req.Report.Summary.TotalMonthlyCostBRL).
		Msg("FinOps: análise AI concluída")

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"analysis":    analysis,
		"analyzed_at": time.Now(),
	})
}

// buildFinOpsPrompt monta o prompt para análise AI do relatório FinOps
func buildFinOpsPrompt(r finops.FinOpsReport) string {
	var sb strings.Builder

	sb.WriteString("Você é um especialista em FinOps e Kubernetes. Analise o relatório de custo abaixo e forneça:\n")
	sb.WriteString("1. Resumo executivo do custo atual\n")
	sb.WriteString("2. Top 3 oportunidades de redução de custo com estimativa de saving\n")
	sb.WriteString("3. Recomendações de rightsizing para os workloads mais caros\n")
	sb.WriteString("4. Ações práticas ordenadas por impacto financeiro\n\n")

	sb.WriteString(fmt.Sprintf("## Cluster: %s\n", r.Cluster))
	sb.WriteString(fmt.Sprintf("- Custo total mensal: R$ %.2f (USD %.2f)\n", r.Summary.TotalMonthlyCostBRL, r.Summary.TotalMonthlyCostUSD))
	sb.WriteString(fmt.Sprintf("- Câmbio USD/BRL: %.4f (%s)\n", r.ExchangeRate, r.ExchangeDate))
	sb.WriteString(fmt.Sprintf("- Workloads analisados: %d\n", r.Summary.WorkloadsAnalyzed))
	sb.WriteString(fmt.Sprintf("- Workloads superprovisionados: %d\n", r.Summary.SuperprovisionedCount))
	sb.WriteString(fmt.Sprintf("- Workloads sem request definido: %d\n", r.Summary.NoRequestCount))
	sb.WriteString(fmt.Sprintf("- Economia potencial (HPA no mínimo): R$ %.2f/mês\n\n", r.Summary.HPASavingsIfMinBRL))

	sb.WriteString("## Node Pools\n")
	for _, p := range r.NodePools {
		sb.WriteString(fmt.Sprintf("- %s: %s × %d nodes = R$ %.2f/mês (%.2f vCPU, %d GB RAM por node)\n",
			p.Name, p.VMSize, p.NodeCount, p.MonthlyCostBRL, float64(p.VMCPUCores), p.VMMemoryGB))
	}

	sb.WriteString("\n## Top 5 Namespaces por Custo\n")
	for i, ns := range r.Namespaces {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("- %s: R$ %.2f/mês (%d workloads)\n", ns.Namespace, ns.MonthlyCostBRL, ns.WorkloadCount))
	}

	sb.WriteString("\n## Top 10 Workloads por Custo\n")
	for i, w := range r.Workloads {
		if i >= 10 {
			break
		}
		hpaInfo := "sem HPA"
		if w.HPAMax > 0 {
			hpaInfo = fmt.Sprintf("HPA %d/%d/%d", w.HPAMin, w.HPACurrent, w.HPAMax)
		}
		sb.WriteString(fmt.Sprintf("- %s/%s: R$ %.2f/mês | CPU %dm | Mem %.0fMi | %s | %s\n",
			w.Namespace, w.Workload, w.CostShareBRL,
			int(w.CPURequestMillis), w.MemRequestMi,
			hpaInfo, w.Verdict))
	}

	if r.Summary.SuperprovisionedCount > 0 {
		sb.WriteString("\n## Workloads Superprovisionados (HPA abaixo de 35% do máximo)\n")
		count := 0
		for _, w := range r.Workloads {
			if w.Verdict == "superprovisioned" && count < 10 {
				saving := w.HPACostCurrentBRL - w.HPACostMinBRL
				sb.WriteString(fmt.Sprintf("- %s/%s: atual R$ %.2f/mês → mínimo R$ %.2f/mês (saving R$ %.2f/mês)\n",
					w.Namespace, w.Workload, w.HPACostCurrentBRL, w.HPACostMinBRL, saving))
				count++
			}
		}
	}

	return sb.String()
}
