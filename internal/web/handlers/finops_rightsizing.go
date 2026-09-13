package handlers

import (
	"encoding/json"
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

// nodePoolTierResponse é o shape de resposta de um pool com alternativas já decodificadas (o
// store guarda AlternativesJSON como string — nunca expõe isso cru pro frontend).
type nodePoolTierResponse struct {
	NodePool      string                 `json:"node_pool"`
	CurrentSKU    string                 `json:"current_sku"`
	CPUUtilPct    float64                `json:"cpu_util_pct"`
	MemUtilPct    float64                `json:"mem_util_pct"`
	WorkloadCount int                    `json:"workload_count"`
	Alternatives  []finops.VMAlternative `json:"alternatives"`
	GeneratedAt   time.Time              `json:"generated_at"`
}

func nodePoolTierResponses(raw []storage.NodePoolTierSuggestion) []nodePoolTierResponse {
	out := make([]nodePoolTierResponse, 0, len(raw))
	for _, r := range raw {
		var alts []finops.VMAlternative
		if r.AlternativesJSON != "" {
			_ = json.Unmarshal([]byte(r.AlternativesJSON), &alts)
		}
		if alts == nil {
			alts = []finops.VMAlternative{}
		}
		out = append(out, nodePoolTierResponse{
			NodePool:      r.NodePool,
			CurrentSKU:    r.CurrentSKU,
			CPUUtilPct:    r.CPUUtilPct,
			MemUtilPct:    r.MemUtilPct,
			WorkloadCount: r.WorkloadCount,
			Alternatives:  alts,
			GeneratedAt:   r.GeneratedAt,
		})
	}
	return out
}

func rightsizingRound2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// GetRightsizing godoc
// GET /api/v1/finops/rightsizing?cluster=X
//
// Lê a análise de rightsizing JÁ PERSISTIDA (nunca recalcula/consulta Prometheus/Dynatrace/cloud
// pricing API) — rápido, existe pra não re-escanear toda vez que alguém só quer OLHAR uma análise
// de um cluster já analisado antes. scanned=false quando o cluster nunca foi analisado (use
// POST /finops/rightsizing/scan pra gerar a 1ª análise).
func (h *FinOpsHandler) GetRightsizing(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if h.rightsizingStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Rightsizing store não disponível"})
		return
	}

	lastScanned, scanned := h.rightsizingStore.LastScannedAt(cluster)
	if !scanned {
		c.JSON(http.StatusOK, gin.H{"cluster": cluster, "scanned": false})
		return
	}

	workloads, err := h.rightsizingStore.GetWorkloadRecommendations(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler recomendações: " + err.Error()})
		return
	}
	pools, err := h.rightsizingStore.GetNodePoolTierSuggestions(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler sugestões de tier: " + err.Error()})
		return
	}
	nodeUsage, err := h.rightsizingStore.GetNodeUsage(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler uso de nodes: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":         cluster,
		"scanned":         true,
		"last_scanned_at": lastScanned,
		"workloads":       workloads,
		"node_pools":      nodePoolTierResponses(pools),
		"nodes":           nodeUsage,
	})
}

// ScanRightsizing godoc
// POST /api/v1/finops/rightsizing/scan?cluster=X&window_days=30&prometheus_url=
//
// Roda o mesmo pipeline que GetReport já usa pra montar o FinOpsReport (Dynatrace/Prometheus
// enrichers, agora populando CPULimitRecommendedMillis/MemLimitRecommendedMi e NodePool por
// workload), agrega o uso real recomendado (não o request nominal) por node pool e chama
// SuggestVMTier — persiste tudo e devolve o resultado fresco. É o único caminho "caro" desta
// aba; GET /rightsizing nunca dispara isto sozinho.
func (h *FinOpsHandler) ScanRightsizing(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if h.rightsizingStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Rightsizing store não disponível"})
		return
	}
	if h.npRegistryStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "NodePool Registry não está disponível. Execute um scan de node pools primeiro."})
		return
	}

	windowDays, _ := strconv.Atoi(c.Query("window_days"))
	if windowDays <= 0 {
		windowDays = 30
	}

	// Dynatrace (primário) — mesma construção de GetReport.
	var dtEnricher *finops.DTEnricher
	if h.dtTokenStore != nil {
		if dtURL, dtToken, ok := h.dtTokenStore.GetDynatraceConfig(); ok {
			if dtClient, err := dynatrace.NewClient(dtURL, dtToken); err != nil {
				log.Warn().Err(err).Msg("FinOps/Rightsizing: falha ao criar cliente DT, enriquecimento DT desativado")
			} else {
				dtEnricher = finops.NewDTEnricher(dtClient, windowDays)
			}
		}
	}

	// Prometheus — SEMPRE tentado aqui (diferente de GetReport, onde é opt-in via
	// with_prometheus=true): sem uso real não há rightsizing nenhum pra sugerir, então esta aba
	// não faz sentido sem métricas. Auto-descobre a URL pelo cluster, mesmo padrão de GetReport.
	// Falha ao criar o enricher não aborta o scan — segue só com DT, se houver.
	promURL := strings.TrimSpace(c.Query("prometheus_url"))
	requiresGCPAuth := false
	if promURL == "" {
		promURL = discovery.GetPrometheusURL(cluster)
		requiresGCPAuth = discovery.RequiresGCPAuth(cluster)
	}
	enricher, enricherErr := finops.NewPrometheusEnricher(promURL, windowDays, requiresGCPAuth)
	if enricherErr != nil {
		log.Warn().Err(enricherErr).Str("prometheus_url", promURL).Msg("FinOps/Rightsizing: falha ao criar enricher Prometheus, seguindo só com Dynatrace (se houver)")
		enricher = nil
	}
	if dtEnricher == nil && enricher == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nenhuma fonte de métricas disponível (Dynatrace não configurado e Prometheus inacessível) — rightsizing exige uso real histórico."})
		return
	}

	pools, err := h.npRegistryStore.GetAll(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar node pools: " + err.Error()})
		return
	}
	if len(pools) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nenhum node pool encontrado para o cluster '" + cluster + "'. Execute um scan de node pools primeiro."})
		return
	}

	k8sClient, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao conectar ao cluster: " + err.Error()})
		return
	}

	pricer := h.pricerForCluster(cluster)
	calc := finops.NewCalculator(pricer, nil, h.exchange)
	// metrics-server best-effort — nil aqui só significa que os campos "current" (live) e
	// NodeUsage ficam sem dado ao vivo, nunca bloqueia o scan (mesmo padrão de GetReport).
	metricsClient, metricsErr := h.kubeManager.GetMetricsClient(cluster)
	if metricsErr != nil {
		log.Debug().Err(metricsErr).Str("cluster", cluster).Msg("FinOps/Rightsizing: metrics-server indisponível, uso 'current' ao vivo ficará vazio")
		metricsClient = nil
	}
	report, err := calc.BuildReport(c.Request.Context(), cluster, k8sClient, pools, nil, dtEnricher, enricher, metricsClient)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gerar análise: " + err.Error()})
		return
	}

	now := time.Now()

	// ── Workloads: persiste o snapshot + agrega uso real recomendado por pool ──────────────────
	type poolUsage struct {
		cpu, mem float64
		n        int
	}
	poolAgg := make(map[string]*poolUsage)

	workloadRecs := make([]storage.WorkloadRecommendation, 0, len(report.Workloads))
	for _, wl := range report.Workloads {
		workloadRecs = append(workloadRecs, storage.WorkloadRecommendation{
			Namespace:                 wl.Namespace,
			Workload:                  wl.Workload,
			NodePool:                  wl.NodePool,
			NodeName:                  wl.NodeName,
			Pods:                      wl.Pods,
			CPURequestMillis:          wl.CPURequestMillis,
			MemRequestMi:              wl.MemRequestMi,
			CPULimitMillis:            wl.CPULimitMillis,
			MemLimitMi:                wl.MemLimitMi,
			CPUP95Millis:              wl.CPUP95Millis,
			MemP95Mi:                  wl.MemP95Mi,
			CPUMaxMillis:              wl.CPUMaxMillis,
			MemMaxMi:                  wl.MemMaxMi,
			CPUCurrentMillis:          wl.CPUCurrentMillis,
			MemCurrentMi:              wl.MemCurrentMi,
			CPURecommendedMillis:      wl.CPURecommendedMillis,
			MemRecommendedMi:          wl.MemRecommendedMi,
			CPULimitRecommendedMillis: wl.CPULimitRecommendedMillis,
			MemLimitRecommendedMi:     wl.MemLimitRecommendedMi,
			Verdict:                   wl.Verdict,
			WasteBRL:                  wl.WasteBRL,
			MetricsSource:             wl.MetricsSource,
			WindowDays:                windowDays,
			GeneratedAt:               now,
		})

		if wl.NodePool != "" && (wl.CPURecommendedMillis > 0 || wl.MemRecommendedMi > 0) {
			agg, ok := poolAgg[wl.NodePool]
			if !ok {
				agg = &poolUsage{}
				poolAgg[wl.NodePool] = agg
			}
			agg.cpu += wl.CPURecommendedMillis
			agg.mem += wl.MemRecommendedMi
			agg.n++
		}
	}

	if err := h.rightsizingStore.ReplaceWorkloadRecommendations(cluster, workloadRecs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao persistir recomendações: " + err.Error()})
		return
	}

	// ── Node Pools: tier de VM guiado pelo uso REAL agregado (não o request nominal) ───────────
	provider := config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster)
	rate, _ := h.exchange.Get()

	tierSuggestions := make([]storage.NodePoolTierSuggestion, 0, len(report.NodePools))
	for _, pool := range report.NodePools {
		if pool.Mode == "System" {
			continue // mesma exclusão já usada hoje pelo PoolSKUAlternatives do frontend
		}
		agg := poolAgg[pool.Name]
		var cpuUtilPct, memUtilPct float64
		var workloadCount int
		if agg != nil {
			workloadCount = agg.n
			if pool.VMCPUCores > 0 && pool.NodeCount > 0 {
				cpuUtilPct = agg.cpu / (float64(pool.VMCPUCores) * 1000 * float64(pool.NodeCount)) * 100
			}
			if pool.VMMemoryGB > 0 && pool.NodeCount > 0 {
				memUtilPct = agg.mem / (float64(pool.VMMemoryGB) * 1024 * float64(pool.NodeCount)) * 100
			}
		}

		alts := finops.SuggestVMTier(provider, pool.VMSize, cpuUtilPct, memUtilPct, pricer, rate, pool.NodeCount)
		altJSON, _ := json.Marshal(alts)

		tierSuggestions = append(tierSuggestions, storage.NodePoolTierSuggestion{
			NodePool:         pool.Name,
			CurrentSKU:       pool.VMSize,
			CPUUtilPct:       rightsizingRound2(cpuUtilPct),
			MemUtilPct:       rightsizingRound2(memUtilPct),
			WorkloadCount:    workloadCount,
			AlternativesJSON: string(altJSON),
			GeneratedAt:      now,
		})
	}

	if err := h.rightsizingStore.ReplaceNodePoolTierSuggestions(cluster, tierSuggestions); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao persistir sugestões de tier: " + err.Error()})
		return
	}

	// ── Nodes: current (live, metrics-server) + top (pico histórico, Prometheus) por node único
	// onde os workloads rodam — correlaciona "esta app roda no node X, que está em Y% agora
	// (pico Z%)" na UI. Best-effort, já vem pronto de report.NodeUsage (calc.BuildReport).
	nodeUsageRecs := make([]storage.NodeUsage, 0, len(report.NodeUsage))
	for _, nu := range report.NodeUsage {
		nodeUsageRecs = append(nodeUsageRecs, storage.NodeUsage{
			NodeName:         nu.NodeName,
			NodePool:         nu.NodePool,
			CPUCapMillis:     nu.CPUCapMillis,
			MemCapMi:         nu.MemCapMi,
			CPUCurrentPct:    nu.CPUCurrentPct,
			MemCurrentPct:    nu.MemCurrentPct,
			CPUTopPct:        nu.CPUTopPct,
			MemTopPct:        nu.MemTopPct,
			MetricsAvailable: nu.MetricsAvailable,
			MetricsError:     nu.MetricsError,
			GeneratedAt:      now,
		})
	}
	if err := h.rightsizingStore.ReplaceNodeUsage(cluster, nodeUsageRecs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao persistir uso de nodes: " + err.Error()})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Int("workloads", len(workloadRecs)).
		Int("node_pools", len(tierSuggestions)).
		Int("nodes", len(nodeUsageRecs)).
		Bool("dynatrace", dtEnricher != nil).
		Bool("prometheus", enricher != nil).
		Msg("FinOps/Rightsizing: scan concluído e persistido")

	c.JSON(http.StatusOK, gin.H{
		"cluster":         cluster,
		"scanned":         true,
		"last_scanned_at": now,
		"workloads":       workloadRecs,
		"node_pools":      nodePoolTierResponses(tierSuggestions),
		"nodes":           nodeUsageRecs,
	})
}
