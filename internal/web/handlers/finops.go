package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/monitoring/discovery"
	"k8s-hpa-manager/internal/storage"
)

// FinOpsHandler expõe análise de custo real de clusters AKS
type FinOpsHandler struct {
	kubeManager     *config.KubeConfigManager
	npRegistryStore *storage.NodePoolRegistryStore
	timelineStore   *storage.FinOpsTimelineStore // pode ser nil se DB não disponível
	pricer          *finops.AzurePricer
	diskPricer      *finops.DiskPricer // nil = análise de storage omitida
	exchange        *finops.ExchangeRateProvider
	aiHandler       *AIDiagnosticsHandler // opcional — nil se AI não configurado
	dtTokenStore    dtTokenReader          // para criar DTEnricher sob demanda
}

// dtTokenReader é satisfeito por *storage.UserTokensStore — evita import circular.
type dtTokenReader interface {
	GetDynatraceConfig() (url string, token string, ok bool)
}

// NewFinOpsHandler cria o handler com as dependências compartilhadas.
// AzurePricer e DiskPricer são inicializados uma única vez (cache SQLite interno).
func NewFinOpsHandler(kubeManager *config.KubeConfigManager, npRegistryStore *storage.NodePoolRegistryStore, timelineStore *storage.FinOpsTimelineStore, aiHandler *AIDiagnosticsHandler, dtTokens dtTokenReader) *FinOpsHandler {
	pricer, err := finops.NewAzurePricer("")
	if err != nil {
		log.Warn().Err(err).Msg("FinOps: falha ao inicializar AzurePricer, usando apenas fallback")
	}
	diskPricer, err := finops.NewDiskPricer("")
	if err != nil {
		log.Warn().Err(err).Msg("FinOps: falha ao inicializar DiskPricer, análise de storage omitida")
	}
	return &FinOpsHandler{
		kubeManager:     kubeManager,
		npRegistryStore: npRegistryStore,
		timelineStore:   timelineStore,
		pricer:          pricer,
		diskPricer:      diskPricer,
		exchange:        finops.NewExchangeRateProvider(),
		aiHandler:       aiHandler,
		dtTokenStore:    dtTokens,
	}
}

// GetReport godoc
// GET /api/v1/finops/report?cluster=X[&namespaces=ns1,ns2][&with_prometheus=true&prometheus_url=http://...&window_days=7]
//
// Retorna o relatório FinOps completo: custo de node pools, alocação por workload,
// cenários HPA e resumo de oportunidades de saving.
// Com with_prometheus=true, enriquece workloads com P95 CPU/Mem real (mais lento).
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

	windowDays, _ := strconv.Atoi(c.Query("window_days"))
	if windowDays <= 0 {
		windowDays = 30
	}

	// Dynatrace (primário) — criado automaticamente se token configurado
	var dtEnricher *finops.DTEnricher
	if h.dtTokenStore != nil {
		if dtURL, dtToken, ok := h.dtTokenStore.GetDynatraceConfig(); ok {
			dtClient, err := dynatrace.NewClient(dtURL, dtToken)
			if err != nil {
				log.Warn().Err(err).Msg("FinOps: falha ao criar cliente DT, enriquecimento DT desativado")
			} else {
				dtEnricher = finops.NewDTEnricher(dtClient, windowDays)
				log.Info().Str("cluster", cluster).Msg("FinOps: DT enricher ativado como fonte primária")
			}
		}
	}

	// Prometheus (fallback) — URL auto-descoberta pelo cluster se não fornecida
	var enricher *finops.PrometheusEnricher
	if c.Query("with_prometheus") == "true" {
		promURL := strings.TrimSpace(c.Query("prometheus_url"))
		if promURL == "" {
			promURL = discovery.GetPrometheusURL(cluster)
			log.Info().Str("cluster", cluster).Str("prometheus_url", promURL).Msg("FinOps: URL Prometheus auto-descoberta")
		}
		var err error
		enricher, err = finops.NewPrometheusEnricher(promURL, windowDays)
		if err != nil {
			log.Warn().Err(err).Str("prometheus_url", promURL).Msg("FinOps: falha ao criar enricher Prometheus")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Falha ao conectar ao Prometheus: " + err.Error()})
			return
		}
		log.Info().Str("prometheus_url", promURL).Int("window_days", windowDays).Msg("FinOps: enricher Prometheus criado")
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

	// Gerar relatório (com Prometheus e Storage opcionais)
	// prometheusURL também é passado ao StorageCalculator para obter uso real de Blob/Files
	// via kubelet_volume_stats_used_bytes (capacidade placeholder > 50 TB → fallback para uso real)
	storagePromURL := strings.TrimSpace(c.Query("prometheus_url"))
	if storagePromURL == "" {
		storagePromURL = discovery.GetPrometheusURL(cluster)
	}
	calc := finops.NewCalculator(h.pricer, h.diskPricer, h.exchange).WithPrometheusURL(storagePromURL)
	report, err := calc.BuildReport(c.Request.Context(), cluster, k8sClient, pools, namespaces, dtEnricher, enricher)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao gerar relatório")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gerar relatório FinOps: " + err.Error()})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Int("workloads", report.Summary.WorkloadsAnalyzed).
		Float64("cost_brl", report.Summary.TotalMonthlyCostBRL).
		Float64("waste_brl", report.Summary.PotentialSavingsBRL).
		Bool("dynatrace", dtEnricher != nil).
		Bool("prometheus", enricher != nil).
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

// RefreshDiskPricing godoc
// POST /api/v1/finops/storage/refresh
//
// Invalida o cache de preços de storage (discos managed, Azure Files, Blob).
func (h *FinOpsHandler) RefreshDiskPricing(c *gin.Context) {
	if h.diskPricer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "DiskPricer não está disponível"})
		return
	}
	if err := h.diskPricer.InvalidateDiskCache(); err != nil {
		log.Error().Err(err).Msg("FinOps: falha ao invalidar cache de preços de disco")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao invalidar cache: " + err.Error()})
		return
	}
	log.Info().Msg("FinOps: cache de preços de storage invalidado")
	c.JSON(http.StatusOK, gin.H{"message": "Cache de preços de storage invalidado."})
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

// GetTimeline godoc
// GET /api/v1/finops/timeline?cluster=X[&days=30][&prometheus_url=http://...]
//
// Retorna a série temporal diária de réplicas de HPA e número de nodes do cluster.
// Salva o resultado no banco SQLite para comparação futura.
func (h *FinOpsHandler) GetTimeline(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}

	promURL := strings.TrimSpace(c.Query("prometheus_url"))
	if promURL == "" {
		promURL = discovery.GetPrometheusURL(cluster)
	}

	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = 30
	}

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	report, err := finops.QueryTimeline(ctx, promURL, start, end)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Str("prometheus_url", promURL).Msg("FinOps: falha ao buscar timeline")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar timeline: " + err.Error()})
		return
	}
	report.Cluster = cluster

	// Salvar snapshot async para comparação futura
	if h.timelineStore != nil {
		go func() {
			if err := h.timelineStore.Save(cluster, report.StartDate, report.EndDate, days, report); err != nil {
				log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao salvar snapshot de timeline")
			}
		}()
	}

	log.Info().
		Str("cluster", cluster).
		Int("hpas", len(report.HPAs)).
		Int("days", days).
		Msg("FinOps: timeline retornada")

	c.JSON(http.StatusOK, report)
}

// GetTimelineCompare godoc
// GET /api/v1/finops/timeline/compare?cluster=X[&days=30][&prometheus_url=http://...]
//
// Busca o período atual via Prometheus e compara com o snapshot anterior salvo no banco.
// Retorna {current, previous, has_previous} para exibição comparativa no frontend.
func (h *FinOpsHandler) GetTimelineCompare(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}

	if h.timelineStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "banco de snapshots não disponível"})
		return
	}

	promURL := strings.TrimSpace(c.Query("prometheus_url"))
	if promURL == "" {
		promURL = discovery.GetPrometheusURL(cluster)
	}

	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = 30
	}

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	// Busca período atual do Prometheus
	current, err := finops.QueryTimeline(ctx, promURL, start, end)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps/compare: falha ao buscar timeline atual")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar timeline atual: " + err.Error()})
		return
	}
	current.Cluster = cluster

	// Salva período atual async
	go func() {
		if err := h.timelineStore.Save(cluster, current.StartDate, current.EndDate, days, current); err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/compare: falha ao salvar snapshot atual")
		}
	}()

	// Busca período anterior do banco (snapshot com end_date < start do período atual)
	previous, err := h.timelineStore.GetPreviousPeriod(cluster, days, current.StartDate)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/compare: falha ao buscar período anterior")
	}

	resp := gin.H{
		"cluster":      cluster,
		"days":         days,
		"current":      current,
		"has_previous": previous != nil,
	}
	if previous != nil {
		resp["previous"] = previous.Data
		resp["previous_saved_at"] = previous.SavedAt
	}

	log.Info().
		Str("cluster", cluster).
		Int("days", days).
		Bool("has_previous", previous != nil).
		Msg("FinOps: comparação de períodos retornada")

	c.JSON(http.StatusOK, resp)
}

// GetVMAlternatives godoc
// GET /api/v1/finops/vm-alternatives?sku=Standard_F4s_v2&cpu_pct=25&mem_pct=80&node_count=3
//
// Retorna até 3 SKUs alternativos sugeridos para o VM SKU informado, levando em conta
// os percentuais de utilização de CPU e Memória do cluster para identificar o gargalo.
// Se cpu_pct e mem_pct forem 0, nenhuma sugestão de troca de família é emitida.
func (h *FinOpsHandler) GetVMAlternatives(c *gin.Context) {
	sku := c.Query("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'sku' é obrigatório"})
		return
	}

	cpuPct, _ := strconv.Atoi(c.DefaultQuery("cpu_pct", "0"))
	memPct, _ := strconv.Atoi(c.DefaultQuery("mem_pct", "0"))
	nodeCount, _ := strconv.Atoi(c.DefaultQuery("node_count", "1"))
	if nodeCount <= 0 {
		nodeCount = 1
	}

	rate, _ := h.exchange.Get()

	alternatives := finops.SuggestAlternatives(sku, cpuPct, memPct, h.pricer, rate, nodeCount)
	if alternatives == nil {
		alternatives = []finops.VMAlternative{}
	}

	cpu, mem := finops.GetVMSpecs(sku)
	c.JSON(http.StatusOK, gin.H{
		"sku":          sku,
		"cpu_cores":    cpu,
		"memory_gb":    mem,
		"cpu_pct":      cpuPct,
		"mem_pct":      memPct,
		"node_count":   nodeCount,
		"alternatives": alternatives,
	})
}

// GetSavedTimelines godoc
// GET /api/v1/finops/timeline/saved?cluster=X
//
// Lista todos os snapshots salvos para o cluster, sem os dados completos (apenas metadados).
func (h *FinOpsHandler) GetSavedTimelines(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}

	if h.timelineStore == nil {
		c.JSON(http.StatusOK, gin.H{"snapshots": []any{}, "count": 0})
		return
	}

	snaps, err := h.timelineStore.ListSnapshots(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao listar snapshots: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":   cluster,
		"snapshots": snaps,
		"count":     len(snaps),
	})
}

// CompareWithSnapshot godoc
// GET /api/v1/finops/timeline/compare-snapshot?cluster=X&snapshot_id=Y[&days=30][&prometheus_url=...]
//
// Compara o período atual (buscado do Prometheus) com um snapshot específico salvo no banco.
// Permite comparar qualquer mês/período salvo com o presente.
func (h *FinOpsHandler) CompareWithSnapshot(c *gin.Context) {
	cluster := c.Query("cluster")
	snapshotID := c.Query("snapshot_id")
	if cluster == "" || snapshotID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros 'cluster' e 'snapshot_id' são obrigatórios"})
		return
	}

	if h.timelineStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "banco de snapshots não disponível"})
		return
	}

	snap, err := h.timelineStore.GetByID(snapshotID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao carregar snapshot: " + err.Error()})
		return
	}
	if snap == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Snapshot não encontrado: " + snapshotID})
		return
	}

	promURL := strings.TrimSpace(c.Query("prometheus_url"))
	if promURL == "" {
		promURL = discovery.GetPrometheusURL(cluster)
	}

	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = snap.Days // usa mesmo intervalo do snapshot para comparação coerente
	}

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	current, err := finops.QueryTimeline(ctx, promURL, start, end)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("FinOps/compare-snapshot: falha ao buscar timeline atual")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar timeline atual: " + err.Error()})
		return
	}
	current.Cluster = cluster

	// Salva período atual async
	go func() {
		if err := h.timelineStore.Save(cluster, current.StartDate, current.EndDate, days, current); err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/compare-snapshot: falha ao salvar snapshot atual")
		}
	}()

	log.Info().
		Str("cluster", cluster).
		Str("snapshot_id", snapshotID).
		Str("snapshot_period", snap.StartDate+" → "+snap.EndDate).
		Msg("FinOps: comparação com snapshot específico retornada")

	c.JSON(http.StatusOK, gin.H{
		"cluster":         cluster,
		"days":            days,
		"current":         current,
		"previous":        snap.Data,
		"has_previous":    true,
		"previous_saved_at": snap.SavedAt,
		"snapshot_id":     snap.ID,
		"snapshot_period": snap.StartDate + " → " + snap.EndDate,
	})
}

// CompareSnapshots godoc
// GET /api/v1/finops/timeline/compare-saved?cluster=X&snap1=ID1&snap2=ID2
//
// Compara dois snapshots salvos no banco sem consultar Prometheus.
// snap1 = período mais recente ("atual"), snap2 = período anterior.
// Permite comparação entre quaisquer dois meses históricos.
func (h *FinOpsHandler) CompareSnapshots(c *gin.Context) {
	cluster := c.Query("cluster")
	snap1ID := c.Query("snap1")
	snap2ID := c.Query("snap2")
	if cluster == "" || snap1ID == "" || snap2ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros 'cluster', 'snap1' e 'snap2' são obrigatórios"})
		return
	}

	if h.timelineStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "banco de snapshots não disponível"})
		return
	}

	snap1, err := h.timelineStore.GetByID(snap1ID)
	if err != nil || snap1 == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Snapshot 1 não encontrado: " + snap1ID})
		return
	}
	snap2, err := h.timelineStore.GetByID(snap2ID)
	if err != nil || snap2 == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Snapshot 2 não encontrado: " + snap2ID})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Str("snap1", snap1ID).
		Str("snap2", snap2ID).
		Msg("FinOps: comparação entre dois snapshots salvos")

	c.JSON(http.StatusOK, gin.H{
		"cluster":             cluster,
		"days":                snap1.Days,
		"current":             snap1.Data,
		"previous":            snap2.Data,
		"has_previous":        true,
		"current_saved_at":    snap1.SavedAt,
		"previous_saved_at":   snap2.SavedAt,
		"current_period":      snap1.StartDate + " → " + snap1.EndDate,
		"previous_period":     snap2.StartDate + " → " + snap2.EndDate,
		"snapshot_id":         snap1.ID,
		"snapshot_period":     snap2.StartDate + " → " + snap2.EndDate,
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

	// Seção de armazenamento (se disponível)
	if r.Storage.TotalMonthlyCostBRL > 0 {
		sb.WriteString("\n## === ARMAZENAMENTO ===\n")
		sb.WriteString(fmt.Sprintf("- Custo total storage: R$ %.2f/mês (USD %.2f)\n", r.Storage.TotalMonthlyCostBRL, r.Storage.TotalMonthlyCostUSD))
		sb.WriteString(fmt.Sprintf("- Disco OS (node pools): R$ %.2f/mês\n", r.Storage.OSDiskCostBRL))
		sb.WriteString(fmt.Sprintf("- PVCs: %d total · %d montados · %d orfãos\n", r.Storage.PVCCount, r.Storage.BoundPVCCount, r.Storage.OrphanedPVCCount))
		sb.WriteString(fmt.Sprintf("- Capacidade total: %.0f GB\n", r.Storage.TotalCapacityGB))
		if r.Storage.OrphanedCostBRL > 0 {
			sb.WriteString(fmt.Sprintf("- CUSTO DESPERDIÇADO (orfãos): R$ %.2f/mês\n", r.Storage.OrphanedCostBRL))
		}
		if len(r.Storage.ByStorageClass) > 0 {
			sb.WriteString("\n### Breakdown por tipo:\n")
			for _, sc := range r.Storage.ByStorageClass {
				sb.WriteString(fmt.Sprintf("- %s (%s): %d PVCs · %.0f GB · R$ %.2f/mês\n",
					sc.StorageClass, sc.AzureType, sc.PVCCount, sc.TotalGB, sc.MonthlyCostBRL))
			}
		}
		// Top 5 workloads por custo de storage
		type wlStorage struct {
			ref  string
			cost float64
		}
		var wlList []wlStorage
		for _, w := range r.Workloads {
			if w.StorageCostBRL > 0 {
				wlList = append(wlList, wlStorage{ref: w.Namespace + "/" + w.Workload, cost: w.StorageCostBRL})
			}
		}
		// sort descending
		for i := 0; i < len(wlList)-1; i++ {
			for j := i + 1; j < len(wlList); j++ {
				if wlList[j].cost > wlList[i].cost {
					wlList[i], wlList[j] = wlList[j], wlList[i]
				}
			}
		}
		if len(wlList) > 0 {
			sb.WriteString("\n### Top 5 workloads por storage:\n")
			for i, wl := range wlList {
				if i >= 5 {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s: R$ %.2f/mês\n", wl.ref, wl.cost))
			}
		}
		// PVCs orfãos com Retain
		var retainOrphans []finops.PVCCostItem
		for _, pvc := range r.PVCs {
			if pvc.IsOrphaned && pvc.ReclaimPolicy == "Retain" {
				retainOrphans = append(retainOrphans, pvc)
			}
		}
		if len(retainOrphans) > 0 {
			sb.WriteString("\n### PVCs orfãos com reclaimPolicy=Retain (disco persiste após delete):\n")
			for _, pvc := range retainOrphans {
				sb.WriteString(fmt.Sprintf("- %s/%s: %s · %.0f GB · R$ %.2f/mês\n",
					pvc.Namespace, pvc.Name, pvc.AzureDiskType, pvc.CapacityGB, pvc.MonthlyCostBRL))
			}
		}
	}

	return sb.String()
}
