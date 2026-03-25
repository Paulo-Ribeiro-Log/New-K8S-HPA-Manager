package handlers

import (
	"net/http"
	"strings"

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
}

// NewFinOpsHandler cria o handler com as dependências compartilhadas.
// AzurePricer é inicializado uma única vez (cache SQLite interno).
func NewFinOpsHandler(kubeManager *config.KubeConfigManager, npRegistryStore *storage.NodePoolRegistryStore) *FinOpsHandler {
	pricer, err := finops.NewAzurePricer("")
	if err != nil {
		log.Warn().Err(err).Msg("FinOps: falha ao inicializar AzurePricer, usando apenas fallback")
		// pricer nil é tratado graciosamente no calculator
	}
	return &FinOpsHandler{
		kubeManager:     kubeManager,
		npRegistryStore: npRegistryStore,
		pricer:          pricer,
		exchange:        finops.NewExchangeRateProvider(),
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
