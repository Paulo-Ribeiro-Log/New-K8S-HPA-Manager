package handlers

import (
	"context"
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
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/monitoring/discovery"
	"k8s-hpa-manager/internal/storage"
)

// nodePoolTierResponse é o shape de resposta de um pool com alternativas já decodificadas (o
// store guarda AlternativesJSON como string — nunca expõe isso cru pro frontend).
type nodePoolTierResponse struct {
	NodePool   string  `json:"node_pool"`
	CurrentSKU string  `json:"current_sku"`
	CPUUtilPct float64 `json:"cpu_util_pct"`
	MemUtilPct float64 `json:"mem_util_pct"`
	// CPUP95Pct/MemP95Pct — percentil de uso REAL do pool (P95, sem a margem de segurança que
	// CPUUtilPct/MemUtilPct já embutem) — ver comentário de poolUsage em
	// persistRightsizingFromReport, e storage.NodePoolTierSuggestion.CPUP95Pct.
	CPUP95Pct          float64                `json:"cpu_p95_pct,omitempty"`
	MemP95Pct          float64                `json:"mem_p95_pct,omitempty"`
	WorkloadCount      int                    `json:"workload_count"`
	NodeCount          int                    `json:"node_count,omitempty"`
	MinNodeCount       int                    `json:"min_node_count,omitempty"`
	MaxNodeCount       int                    `json:"max_node_count,omitempty"`
	AutoscalingEnabled bool                   `json:"autoscaling_enabled,omitempty"`
	VMCPUCores         int                    `json:"vm_cpu_cores,omitempty"`
	VMMemoryGB         int                    `json:"vm_memory_gb,omitempty"`
	Alternatives       []finops.VMAlternative `json:"alternatives"`
	GeneratedAt        time.Time              `json:"generated_at"`
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
			NodePool:           r.NodePool,
			CurrentSKU:         r.CurrentSKU,
			CPUUtilPct:         r.CPUUtilPct,
			MemUtilPct:         r.MemUtilPct,
			CPUP95Pct:          r.CPUP95Pct,
			MemP95Pct:          r.MemP95Pct,
			WorkloadCount:      r.WorkloadCount,
			NodeCount:          r.NodeCount,
			MinNodeCount:       r.MinNodeCount,
			MaxNodeCount:       r.MaxNodeCount,
			AutoscalingEnabled: r.AutoscalingEnabled,
			VMCPUCores:         r.VMCPUCores,
			VMMemoryGB:         r.VMMemoryGB,
			Alternatives:       alts,
			GeneratedAt:        r.GeneratedAt,
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
// workload) e delega a persistRightsizingFromReport a agregação por pool + SuggestVMTier +
// persistência. É o caminho STANDALONE (usado pelo botão "Reanalisar agora" da aba Rightsizing,
// quando não há nenhum relatório já construído na mesma requisição pra reaproveitar — ver
// GetReport's persist_rightsizing=true pro caminho reaproveitado, que é o que o "Analisar"
// principal do FinOps dispara).
func (h *FinOpsHandler) ScanRightsizing(c *gin.Context) {
	handlerStart := time.Now()
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
				dtEnricher = finops.NewDTEnricher(dtClient, windowDays, cluster)
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
	// h.diskPricer (não mais nil) + WithPrometheusURL: standalone precisa computar storage/PVC
	// igual ao relatório principal (GetReport) computa — antes ScanRightsizing passava nil pro
	// diskPricer, deixando report.Storage/PVCs sempre vazios nesse caminho (achado ao investigar
	// "isso [PVC] está sendo levado em conta nos custos apresentados?" — o total geral do FinOps
	// (Dashboard/Relatório) sempre incluiu storage via GetReport; só o report INTERNO gerado por
	// um scan standalone de Rightsizing não incluía. waste_brl/recomendações continuam só de
	// compute — storage nunca fez parte de "rightsizing" de request/limit, propositalmente).
	calc := finops.NewCalculator(pricer, h.diskPricer, h.exchange).WithPrometheusURL(promURL, requiresGCPAuth)
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

	workloadRecs, tierSuggestions, nodeUsageRecs, persistErr := h.persistRightsizingFromReport(c.Request.Context(), cluster, report, windowDays, pricer)
	if persistErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao persistir análise: " + persistErr.Error()})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Int("workloads", len(workloadRecs)).
		Int("node_pools", len(tierSuggestions)).
		Int("nodes", len(nodeUsageRecs)).
		Bool("dynatrace", dtEnricher != nil).
		Bool("prometheus", enricher != nil).
		Dur("elapsed_total_handler", time.Since(handlerStart)).
		Msg("FinOps/Rightsizing: scan standalone concluído e persistido")

	c.JSON(http.StatusOK, gin.H{
		"cluster":         cluster,
		"scanned":         true,
		"last_scanned_at": time.Now(),
		"workloads":       workloadRecs,
		"node_pools":      nodePoolTierResponses(tierSuggestions),
		"nodes":           nodeUsageRecs,
	})
}

// persistRightsizingFromReport agrega o uso real recomendado por node pool, computa as sugestões
// de tier de VM (SuggestVMTier) e persiste tudo no rightsizingStore — a partir de um
// *finops.FinOpsReport JÁ CONSTRUÍDO (por ScanRightsizing, ou por GetReport quando o relatório
// principal também pede ?persist_rightsizing=true). Extraída pra ser compartilhada pelos dois
// call sites — bug real corrigido, relatado pelo usuário: "o que me leva a crer que está fazendo
// o mesmo scan 2 vezes" (exatamente o que acontecia: clicar "Analisar" disparava GetReport
// completo, e o auto-trigger do scan de Rightsizing em seguida refazia TODO o pipeline
// Dynatrace/Prometheus/K8s/storage do zero, dobrando o tempo total). Esta função nunca
// re-consulta Prometheus/Dynatrace — só faz o trabalho que é exclusivo do Rightsizing.
func (h *FinOpsHandler) persistRightsizingFromReport(
	ctx context.Context,
	cluster string,
	report *finops.FinOpsReport,
	windowDays int,
	pricer finops.CloudPricer,
) (workloadRecs []storage.WorkloadRecommendation, tierSuggestions []storage.NodePoolTierSuggestion, nodeUsageRecs []storage.NodeUsage, err error) {
	now := time.Now()

	// ── Workloads: persiste o snapshot + agrega uso real recomendado por pool ──────────────────
	// cpu/mem = Σ CPURecommendedMillis/MemRecommendedMi (P95-ou-avg × SafetyMargin=1.20) — já usado
	// por SuggestVMTier pra decidir a troca de tier, JÁ com a margem de segurança embutida.
	// cpuP95/memP95 = Σ do percentil PURO (P95Millis, ou avg quando a fonte não supre P95 — mesmo
	// fallback já usado pelo enriquecimento DT, ver dynatrace_enricher.go), SEM margem nenhuma —
	// pedido explícito do usuário: "preciso que o percentil de uso... seja evidenciado nas
	// análises... pode nos dar uma visão mais adequada da possibilidade de troca de família de
	// máquina" — antes só existia o número já misturado com a margem, sem visibilidade do
	// percentil real por trás da decisão.
	type poolUsage struct {
		cpu, mem       float64
		cpuP95, memP95 float64
		n              int
	}
	poolAgg := make(map[string]*poolUsage)

	workloadRecs = make([]storage.WorkloadRecommendation, 0, len(report.Workloads))
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
			HPAMin:                    wl.HPAMin,
			HPAMax:                    wl.HPAMax,
			HPACurrent:                wl.HPACurrent,
			HPAAvgReplicas:            wl.HPAAvgReplicas,
			HPAMaxObserved:            wl.HPAMaxObserved,
			HPAMinObserved:            wl.HPAMinObserved,
			HPAScaleEvents:            wl.HPAScaleEvents,
			HPANeverScaled:            wl.HPANeverScaled,
			CPUMaxAt:                  wl.CPUMaxAt,
			MemMaxAt:                  wl.MemMaxAt,
			OldestPodStartedAt:        wl.OldestPodStartedAt,
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
			// Percentil puro — P95 quando a fonte supre (Prometheus sempre; Dynatrace, ainda não
			// nesta família de métrica, ver finops_metrics.go), senão cai pro avg, igual ao mesmo
			// fallback já aplicado em CPURecommendedMillis/MemRecommendedMi antes da margem.
			cpuP95Basis := wl.CPUP95Millis
			if cpuP95Basis == 0 {
				cpuP95Basis = wl.CPUAvgMillis
			}
			memP95Basis := wl.MemP95Mi
			if memP95Basis == 0 {
				memP95Basis = wl.MemAvgMi
			}
			agg.cpuP95 += cpuP95Basis
			agg.memP95 += memP95Basis
			agg.n++
		}
	}

	if err = h.rightsizingStore.ReplaceWorkloadRecommendations(cluster, workloadRecs); err != nil {
		return nil, nil, nil, err
	}

	// ── Node Pools: tier de VM guiado pelo uso REAL agregado (não o request nominal) ───────────
	provider := config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster)
	rate, _ := h.exchange.Get()

	// Min/max de node count + autoscaling — só disponível via chamada ao cloud provider (az/
	// gcloud/aws CLI), não vem do Node Pool Registry (snapshot leve, só nome/vm_size/node_count).
	// Best-effort: falha aqui (VPN/CLI indisponível) não aborta o scan — o modal de detalhe só
	// fica sem min/max/autoscaling pra esse pool, tudo o mais continua funcionando.
	liveNodeGroups := make(map[string]models.NodePool)
	if npProvider := h.kubeManager.GetNodeGroupProvider(cluster); npProvider != nil {
		// Timing explícito — chamada CLI (az/gcloud/aws), já documentada como podendo levar até
		// 60s sozinha; candidata real à lentidão relatada ("definitivamente... scan de 2
		// minutos... não parece haver nenhum paralelismo"). Não dá pra paralelizar (é 1 chamada
		// só, não um loop por pool), mas expor o tempo aqui tira a adivinhação de qual fase é a
		// culpada da próxima vez que o scan estiver lento.
		listStart := time.Now()
		groups, npErr := npProvider.ListNodeGroups(ctx, cluster)
		log.Info().Str("cluster", cluster).Str("step", "npProvider.ListNodeGroups").Dur("elapsed", time.Since(listStart)).Msg("FinOps/timing")
		if npErr != nil {
			log.Warn().Err(npErr).Str("cluster", cluster).Msg("FinOps/Rightsizing: falha ao buscar min/max de node count ao vivo — cenário de resize de node count ficará incompleto")
		} else {
			for _, g := range groups {
				liveNodeGroups[g.Name] = g
			}
		}
	}

	tierSuggestions = make([]storage.NodePoolTierSuggestion, 0, len(report.NodePools))
	coveredPools := make(map[string]bool, len(report.NodePools))
	for _, pool := range report.NodePools {
		if pool.Mode == "System" {
			continue // mesma exclusão já usada hoje pelo PoolSKUAlternatives do frontend
		}
		coveredPools[pool.Name] = true

		// Node count "atual": prefere o valor AO VIVO (chamada real à API do cloud provider,
		// já buscada acima em liveNodeGroups) em vez do snapshot do Node Pool Registry
		// (pool.NodeCount) — esse snapshot só reflete a contagem de objetos K8s Node no
		// momento do último "Escanear Clusters" MANUAL, o que é especialmente errado pra
		// pools spot/preemptible, que oscilam por eviction/replacement entre um scan e outro
		// (bug relatado: "um node spot não tem o seus node count coletados"). Só cai de
		// volta pro registry quando a chamada ao vivo falhou pro cluster inteiro ou esse pool
		// específico não veio na resposta (ex: provider sem suporte, erro pontual).
		live, liveOK := liveNodeGroups[pool.Name]
		currentNodeCount := pool.NodeCount
		if liveOK && live.NodeCount > 0 {
			currentNodeCount = int(live.NodeCount)
		}

		agg := poolAgg[pool.Name]
		var cpuUtilPct, memUtilPct, cpuP95Pct, memP95Pct float64
		var workloadCount int
		if agg != nil {
			workloadCount = agg.n
			if pool.VMCPUCores > 0 && currentNodeCount > 0 {
				cpuUtilPct = agg.cpu / (float64(pool.VMCPUCores) * 1000 * float64(currentNodeCount)) * 100
				cpuP95Pct = agg.cpuP95 / (float64(pool.VMCPUCores) * 1000 * float64(currentNodeCount)) * 100
			}
			if pool.VMMemoryGB > 0 && currentNodeCount > 0 {
				memUtilPct = agg.mem / (float64(pool.VMMemoryGB) * 1024 * float64(currentNodeCount)) * 100
				memP95Pct = agg.memP95 / (float64(pool.VMMemoryGB) * 1024 * float64(currentNodeCount)) * 100
			}
		}

		alts := finops.SuggestVMTier(provider, pool.VMSize, cpuUtilPct, memUtilPct, pricer, rate, currentNodeCount)
		altJSON, _ := json.Marshal(alts)

		tierSuggestions = append(tierSuggestions, storage.NodePoolTierSuggestion{
			NodePool:           pool.Name,
			CurrentSKU:         pool.VMSize,
			CPUUtilPct:         rightsizingRound2(cpuUtilPct),
			MemUtilPct:         rightsizingRound2(memUtilPct),
			CPUP95Pct:          rightsizingRound2(cpuP95Pct),
			MemP95Pct:          rightsizingRound2(memP95Pct),
			WorkloadCount:      workloadCount,
			NodeCount:          currentNodeCount,
			MinNodeCount:       int(live.MinNodeCount),
			MaxNodeCount:       int(live.MaxNodeCount),
			AutoscalingEnabled: live.AutoscalingEnabled,
			VMCPUCores:         pool.VMCPUCores,
			VMMemoryGB:         pool.VMMemoryGB,
			AlternativesJSON:   string(altJSON),
			GeneratedAt:        now,
		})
	}

	// Pools que existem AO VIVO (confirmados via API real do cloud provider) mas nunca
	// apareceram em report.NodePools — calculatePoolCosts (calculator.go) descarta
	// silenciosamente qualquer pool cujo NodeCount do REGISTRY seja 0 (snapshot stale — ex:
	// pool spot totalmente evictado no instante do último "Escanear Clusters" manual) ou cujo
	// VMSize esteja vazio. Sem isto, um pool spot genuinamente ativo agora nunca ganharia
	// sequer um card na aba, mesmo aparecendo corretamente na chamada ao vivo — adicionamos
	// uma entrada sintética usando as specs do próprio pricer (sem depender do registry).
	for name, live := range liveNodeGroups {
		if coveredPools[name] || live.IsSystemPool || live.NodeCount == 0 || live.VMSize == "" {
			continue
		}
		cpuCores, memGB := pricer.GetVMSpecs(live.VMSize)
		agg := poolAgg[name]
		var cpuUtilPct, memUtilPct, cpuP95Pct, memP95Pct float64
		var workloadCount int
		if agg != nil {
			workloadCount = agg.n
			if cpuCores > 0 {
				cpuUtilPct = agg.cpu / (float64(cpuCores) * 1000 * float64(live.NodeCount)) * 100
				cpuP95Pct = agg.cpuP95 / (float64(cpuCores) * 1000 * float64(live.NodeCount)) * 100
			}
			if memGB > 0 {
				memUtilPct = agg.mem / (float64(memGB) * 1024 * float64(live.NodeCount)) * 100
				memP95Pct = agg.memP95 / (float64(memGB) * 1024 * float64(live.NodeCount)) * 100
			}
		}

		alts := finops.SuggestVMTier(provider, live.VMSize, cpuUtilPct, memUtilPct, pricer, rate, int(live.NodeCount))
		altJSON, _ := json.Marshal(alts)

		tierSuggestions = append(tierSuggestions, storage.NodePoolTierSuggestion{
			NodePool:           name,
			CurrentSKU:         live.VMSize,
			CPUUtilPct:         rightsizingRound2(cpuUtilPct),
			MemUtilPct:         rightsizingRound2(memUtilPct),
			CPUP95Pct:          rightsizingRound2(cpuP95Pct),
			MemP95Pct:          rightsizingRound2(memP95Pct),
			WorkloadCount:      workloadCount,
			NodeCount:          int(live.NodeCount),
			MinNodeCount:       int(live.MinNodeCount),
			MaxNodeCount:       int(live.MaxNodeCount),
			AutoscalingEnabled: live.AutoscalingEnabled,
			VMCPUCores:         cpuCores,
			VMMemoryGB:         memGB,
			AlternativesJSON:   string(altJSON),
			GeneratedAt:        now,
		})
		log.Info().Str("cluster", cluster).Str("node_pool", name).
			Msg("FinOps/Rightsizing: pool presente ao vivo mas ausente do relatório (registry stale) — entrada sintética adicionada")
	}

	if err = h.rightsizingStore.ReplaceNodePoolTierSuggestions(cluster, tierSuggestions); err != nil {
		return nil, nil, nil, err
	}

	// ── Nodes: current (live, metrics-server) + top (pico histórico, Prometheus) por node único
	// onde os workloads rodam — correlaciona "esta app roda no node X, que está em Y% agora
	// (pico Z%)" na UI. Best-effort, já vem pronto de report.NodeUsage (calc.BuildReport).
	nodeUsageRecs = make([]storage.NodeUsage, 0, len(report.NodeUsage))
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
			CPUTopAt:         nu.CPUTopAt,
			MemTopAt:         nu.MemTopAt,
			NodeCreatedAt:    nu.NodeCreatedAt,
			MetricsAvailable: nu.MetricsAvailable,
			MetricsError:     nu.MetricsError,
			GeneratedAt:      now,
		})
	}
	if err = h.rightsizingStore.ReplaceNodeUsage(cluster, nodeUsageRecs); err != nil {
		return nil, nil, nil, err
	}

	return workloadRecs, tierSuggestions, nodeUsageRecs, nil
}
