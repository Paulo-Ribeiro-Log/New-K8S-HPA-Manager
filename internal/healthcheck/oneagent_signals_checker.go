package healthcheck

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	dtclient "k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/storage"
)

// OneAgentSignalsChecker varre todas as entidades instrumentadas pelo OneAgent em um cluster
// e identifica workloads com métricas elevadas mesmo sem um problem DT ativo.
type OneAgentSignalsChecker struct{}

func NewOneAgentSignalsChecker() *OneAgentSignalsChecker {
	return &OneAgentSignalsChecker{}
}

// CheckAll executa o scan de sinais OneAgent para o cluster.
//
// existingDTWorkloads: chave "namespace/workload" dos workloads já cobertos por DT problems
// (Fases 1-4) — usados para marcar HasDTProblem=true sem omitir o sinal.
//
// nodePoolStore e depRegistry podem ser nil (correlações são omitidas sem erro).
func (c *OneAgentSignalsChecker) CheckAll(
	ctx context.Context,
	dtURL, dtToken string,
	cluster string,
	existingDTWorkloads map[string]struct{},
	nodePoolStore *storage.NodePoolRegistryStore,
	depRegistry *storage.DependencyRegistry,
	thresholds OneAgentThresholds,
	timeoutSec int,
) []OneAgentSignal {

	thresholds = thresholds.resolve()

	client, err := dtclient.NewClient(dtURL, dtToken)
	if err != nil {
		log.Error().Err(err).Msg("[OneAgentSignals] Falha ao criar client DT")
		return nil
	}

	scanCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	clusterNorm := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(cluster, "-admin")))

	// 1. Listar entidades CLOUD_APPLICATION e SERVICE em paralelo
	type listResult struct {
		entityType string
		stubs      []dtclient.EntityStub
		err        error
	}
	listCh := make(chan listResult, 2)

	for _, entityType := range []string{"CLOUD_APPLICATION", "SERVICE"} {
		go func(et string) {
			stubs, err := client.ListEntitiesByCluster(scanCtx, clusterNorm, et)
			listCh <- listResult{entityType: et, stubs: stubs, err: err}
		}(entityType)
	}

	allStubs := make([]dtclient.EntityStub, 0, 100)
	seen := make(map[string]struct{})
	for i := 0; i < 2; i++ {
		r := <-listCh
		if r.err != nil {
			log.Warn().Err(r.err).Str("entityType", r.entityType).Str("cluster", clusterNorm).
				Msg("[OneAgentSignals] Falha ao listar entidades")
			continue
		}
		for _, s := range r.stubs {
			if _, dup := seen[s.EntityID.ID]; !dup {
				seen[s.EntityID.ID] = struct{}{}
				allStubs = append(allStubs, s)
			}
		}
	}

	if len(allStubs) == 0 {
		log.Info().Str("cluster", clusterNorm).Msg("[OneAgentSignals] Nenhuma entidade encontrada — verificar tag dt.host_group.id")
		return nil
	}

	log.Info().Str("cluster", clusterNorm).Int("entities", len(allStubs)).
		Msg("[OneAgentSignals] Entidades encontradas, consultando métricas")

	// 2. Consultar métricas (últimos 60min) em batch
	metricsMap := client.BatchQueryMetrics(scanCtx, allStubs, 60)

	// 3. Pré-carregar node pools do cluster (uma query SQLite para todos)
	var clusterPools []NodePoolSummary
	if nodePoolStore != nil {
		entries, err := nodePoolStore.GetAll(clusterNorm)
		if err == nil {
			clusterPools = make([]NodePoolSummary, 0, len(entries))
			for _, e := range entries {
				clusterPools = append(clusterPools, NodePoolSummary{
					NodePool:  e.NodePool,
					VMSize:    e.VMSize,
					Mode:      e.Mode,
					NodeCount: e.NodeCount,
				})
			}
		}
	}

	// 4. Avaliar cada entidade
	now := time.Now()
	signals := make([]OneAgentSignal, 0)

	for _, stub := range allStubs {
		metrics := metricsMap[stub.EntityID.ID]
		if len(metrics) == 0 {
			continue // sem dados = não instrumentado ou inativo
		}

		signal := OneAgentSignal{
			EntityID:     stub.EntityID.ID,
			EntityType:   stub.EntityID.Type,
			Cluster:      cluster,
			ClusterPools: clusterPools,
			CheckedAt:    now,
		}

		// Resolver nome e namespace via DTLabels
		signal.WorkloadName = stub.BestName()
		if stub.Labels != nil {
			signal.Namespace = stub.Labels.Namespace
			signal.AppVersion = stub.Labels.AppVersion
			signal.Squad = stub.Labels.ComponentSquad
		}
		if signal.Namespace == "" {
			signal.Namespace = stub.K8sNamespace
		}
		if wl := stub.K8sWorkload; wl != "" {
			signal.WorkloadName = wl
		}

		// Extrair métricas relevantes
		signal.ErrorRate = metrics["error_rate"]
		signal.ResponseP90Ms = metrics["response_p90"]
		signal.PodRestarts = metrics["pod_restarts"]
		signal.CPUThrottlePct = metrics["cpu_throttle"]
		if v, ok := metrics["pods_ready_pct"]; ok {
			signal.PodsReadyPct = v * 100 // DT retorna fração 0-1
		}

		// Calcular risco por threshold
		signal.RiskLevel, signal.RiskReasons = evaluateRisk(signal, thresholds)

		// Marcar se já coberto por problem DT ativo
		key := strings.ToLower(signal.Namespace) + "/" + strings.ToLower(signal.WorkloadName)
		if _, covered := existingDTWorkloads[key]; covered {
			signal.HasDTProblem = true
		}

		// Correlação de dependências
		if depRegistry != nil && signal.WorkloadName != "" {
			signal.DependedBy, signal.DependsOn = resolveDepLinks(depRegistry, signal.WorkloadName, signal.Cluster, signal.Namespace)
		}

		signals = append(signals, signal)
	}

	// Ordenar: críticos primeiro, depois por risk level descendente
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].RiskLevel.Weight() > signals[j].RiskLevel.Weight()
	})

	log.Info().
		Str("cluster", cluster).
		Int("total", len(allStubs)).
		Int("with_signals", len(signals)).
		Msg("[OneAgentSignals] CheckAll concluído")

	return signals
}

// evaluateRisk aplica os thresholds e retorna RiskLevel + lista de razões.
func evaluateRisk(s OneAgentSignal, t OneAgentThresholds) (Severity, []string) {
	var reasons []string
	level := SeverityInfo

	upgrade := func(candidate Severity, reason string) {
		reasons = append(reasons, reason)
		if candidate.Weight() > level.Weight() {
			level = candidate
		}
	}

	if s.ErrorRate > 0 {
		if s.ErrorRate >= t.ErrorRateCriticalPct {
			upgrade(SeverityCritical, fmt.Sprintf("Error rate %.1f%% ≥ %.0f%%", s.ErrorRate, t.ErrorRateCriticalPct))
		} else if s.ErrorRate >= t.ErrorRateWarnPct {
			upgrade(SeverityMedium, fmt.Sprintf("Error rate %.1f%% ≥ %.0f%%", s.ErrorRate, t.ErrorRateWarnPct))
		}
	}

	if s.ResponseP90Ms > 0 {
		if s.ResponseP90Ms >= t.ResponseP90CritMs {
			upgrade(SeverityHigh, fmt.Sprintf("P90 %.0fms ≥ %.0fms", s.ResponseP90Ms, t.ResponseP90CritMs))
		} else if s.ResponseP90Ms >= t.ResponseP90WarnMs {
			upgrade(SeverityMedium, fmt.Sprintf("P90 %.0fms ≥ %.0fms", s.ResponseP90Ms, t.ResponseP90WarnMs))
		}
	}

	if s.PodRestarts > 0 {
		restarts := int(s.PodRestarts)
		if restarts >= t.PodRestartsCrit {
			upgrade(SeverityCritical, fmt.Sprintf("Pod restarts %d ≥ %d (1h)", restarts, t.PodRestartsCrit))
		} else if restarts >= t.PodRestartsWarn {
			upgrade(SeverityMedium, fmt.Sprintf("Pod restarts %d ≥ %d (1h)", restarts, t.PodRestartsWarn))
		}
	}

	if s.CPUThrottlePct > 0 {
		if s.CPUThrottlePct >= t.CPUThrottleCritPct {
			upgrade(SeverityHigh, fmt.Sprintf("CPU throttle %.1f%% ≥ %.0f%%", s.CPUThrottlePct, t.CPUThrottleCritPct))
		} else if s.CPUThrottlePct >= t.CPUThrottleWarnPct {
			upgrade(SeverityMedium, fmt.Sprintf("CPU throttle %.1f%% ≥ %.0f%%", s.CPUThrottlePct, t.CPUThrottleWarnPct))
		}
	}

	if s.PodsReadyPct > 0 {
		if s.PodsReadyPct <= t.PodsReadyCritPct {
			upgrade(SeverityCritical, fmt.Sprintf("Pods prontos %.1f%% ≤ %.0f%%", s.PodsReadyPct, t.PodsReadyCritPct))
		} else if s.PodsReadyPct <= t.PodsReadyWarnPct {
			upgrade(SeverityMedium, fmt.Sprintf("Pods prontos %.1f%% ≤ %.0f%%", s.PodsReadyPct, t.PodsReadyWarnPct))
		}
	}

	return level, reasons
}

// resolveDepLinks consulta o DependencyRegistry para encontrar quem depende deste workload
// e de quem este workload depende. Retorna (dependedBy, dependsOn).
func resolveDepLinks(reg *storage.DependencyRegistry, workloadName, cluster, namespace string) ([]string, []string) {
	results, err := reg.SearchByServiceName(workloadName)
	if err != nil || len(results) == 0 {
		return nil, nil
	}

	wlLower := strings.ToLower(workloadName)
	dependedBySet := make(map[string]struct{})
	dependsOnSet := make(map[string]struct{})

	for _, dep := range results {
		// dep.ServiceName é o serviço externo/dependência; dep.SourceService (se existir) é quem depende
		// O campo ServiceName no DependencyRecord é o target (ex: "rdsh-regional01.dc.nova")
		// Precisamos distinguir: este workload É a dependência ou CHAMA a dependência?
		svcLower := strings.ToLower(dep.ServiceName)
		if strings.Contains(svcLower, wlLower) || strings.Contains(wlLower, svcLower) {
			// ServiceName contém o nome do workload → outros serviços dependem dele
			if dep.Namespace != namespace {
				dependedBySet[dep.Namespace+"/"+dep.ServiceName] = struct{}{}
			}
		} else if dep.Cluster == cluster && dep.Namespace == namespace {
			// Este workload (mesmo cluster/ns) usa essa dependência
			dependsOnSet[dep.ServiceName] = struct{}{}
		}
	}

	toSlice := func(m map[string]struct{}) []string {
		s := make([]string, 0, len(m))
		for k := range m {
			s = append(s, k)
		}
		return s
	}

	return toSlice(dependedBySet), toSlice(dependsOnSet)
}
