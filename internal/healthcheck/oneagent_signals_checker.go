package healthcheck

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	dtclient "k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/storage"
)

// aksNodeNameRe extrai o nome do node pool de nomes AKS: aks-<nodepool>-XXXXXXXX-vmssXXXX
var aksNodeNameRe = regexp.MustCompile(`^aks-(.+?)-\d{5,8}-vmss[0-9a-f]+`)

// dtInternalPrefixes lista prefixos de nomes que identificam componentes internos do Dynatrace.
// Entidades com esses nomes não são aplicações do usuário e devem ser ignoradas nos sinais.
var dtInternalPrefixes = []string{
	"ace-op",           // Dynatrace ActiveGate Operator
	"dynatrace-operator",
	"dynatrace-webhook",
	"oneagent",
	"activegate",
	"easytravel",       // app de demo do DT
}

// isDynatraceInternal retorna true se o nome da entidade for um componente interno do Dynatrace.
func isDynatraceInternal(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range dtInternalPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// OneAgentSignalsChecker implementa o scan híbrido de dois fases:
//   - Fase 1 (prioridade): workloads K8s com problema → enriquecimento DT + registros locais
//   - Fase 2 (complementar): varredura geral por threshold em todas as entidades DT do cluster
type OneAgentSignalsChecker struct{}

func NewOneAgentSignalsChecker() *OneAgentSignalsChecker {
	return &OneAgentSignalsChecker{}
}

// CheckAll executa o scan híbrido de sinais OneAgent para o cluster.
//
// troubledWorkloads: workloads K8s com problema sem match DT ativo (Fase 1 — prioridade).
// existingDTWorkloads: chave "namespace/workload" dos workloads cobertos por DT problems ativos.
// nodePoolStore e depRegistry podem ser nil (correlações omitidas sem erro).
func (c *OneAgentSignalsChecker) CheckAll(
	ctx context.Context,
	dtURL, dtToken string,
	cluster string,
	existingDTWorkloads map[string]struct{},
	troubledWorkloads []TroubledWorkloadInfo,
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
	now := time.Now()

	// Pré-carregar node pools do cluster (uma query SQLite para todos)
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

	signals := make([]OneAgentSignal, 0)
	phase1EntityIDs := make(map[string]struct{})

	// ─── FASE 1: Workloads K8s com problema → enriquecimento DT ──────────────
	if len(troubledWorkloads) > 0 {
		// Índice rápido: nome em lowercase → info
		twByName := make(map[string]TroubledWorkloadInfo, len(troubledWorkloads))
		names := make([]string, 0, len(troubledWorkloads))
		for _, tw := range troubledWorkloads {
			lower := strings.ToLower(tw.Name)
			twByName[lower] = tw
			names = append(names, tw.Name)
		}

		// Buscar entidades DT por nome (SearchEntitiesByName usa entityName.startsWith)
		foundStubs := client.SearchEntitiesByName(scanCtx, names)

		// Construir mapa nome → stub para associação
		foundByName := make(map[string]dtclient.EntityStub, len(foundStubs))
		for _, stub := range foundStubs {
			phase1EntityIDs[stub.EntityID.ID] = struct{}{}
			bestName := strings.ToLower(stub.BestName())
			if wl := stub.K8sWorkload; wl != "" {
				bestName = strings.ToLower(wl)
			}
			if _, exists := foundByName[bestName]; !exists {
				foundByName[bestName] = stub
			}
		}

		// Consultar métricas para stubs encontrados
		var metricsP1 map[string]map[string]float64
		if len(foundStubs) > 0 {
			metricsP1 = client.BatchQueryMetrics(scanCtx, foundStubs, 60)
		}

		// Gerar sinal para cada workload K8s com problema
		for _, tw := range troubledWorkloads {
			if isDynatraceInternal(tw.Name) {
				continue
			}
			stub, foundInDT := foundByName[strings.ToLower(tw.Name)]

			signal := OneAgentSignal{
				Cluster:      cluster,
				WorkloadName: tw.Name,
				Namespace:    tw.Namespace,
				ClusterPools: clusterPools,
				CheckedAt:    now,
				FromK8sIssue: true,
				K8sIssues:    tw.Issues,
			}

			if foundInDT {
				signal.EntityID = stub.EntityID.ID
				signal.EntityType = stub.EntityID.Type
				if stub.Labels != nil {
					signal.AppVersion = stub.Labels.AppVersion
					signal.Squad = stub.Labels.ComponentSquad
					if signal.Namespace == "" {
						signal.Namespace = stub.Labels.Namespace
					}
				}
				if signal.Namespace == "" {
					signal.Namespace = stub.K8sNamespace
				}

				metrics := metricsP1[stub.EntityID.ID]
				if len(metrics) > 0 {
					signal.ErrorRate = metrics["error_rate"]
					signal.ResponseP90Ms = metrics["response_p90"]
					signal.PodRestarts = metrics["pod_restarts"]
					signal.CPUThrottlePct = metrics["cpu_throttle"]
					if v, ok := metrics["pods_ready_pct"]; ok {
						signal.PodsReadyPct = v * 100
					}
				}
			}

			// Avaliar risco via threshold; se sem métricas DT, usar severidade K8s
			signal.RiskLevel, signal.RiskReasons = evaluateRisk(signal, thresholds)
			if len(signal.RiskReasons) == 0 {
				signal.RiskLevel = tw.Severity
				signal.RiskReasons = tw.Issues
			}

			// Marcar se coberto por DT problem ativo
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

		log.Info().
			Str("cluster", clusterNorm).
			Int("troubled_workloads", len(troubledWorkloads)).
			Int("found_in_dt", len(foundStubs)).
			Msg("[OneAgentSignals] Fase 1 concluída")
	}

	// ─── FASE 2: Varredura geral por threshold (complementar) ─────────────────
	// Lista CLOUD_APPLICATION e SERVICE, pulando entidades já cobertas na Fase 1

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

	p2Stubs := make([]dtclient.EntityStub, 0, 100)
	seen := make(map[string]struct{})
	for i := 0; i < 2; i++ {
		r := <-listCh
		if r.err != nil {
			log.Warn().Err(r.err).Str("entityType", r.entityType).Str("cluster", clusterNorm).
				Msg("[OneAgentSignals] Fase 2: falha ao listar entidades")
			continue
		}
		log.Info().Str("entityType", r.entityType).Str("cluster", clusterNorm).
			Int("count", len(r.stubs)).Msg("[OneAgentSignals] Fase 2: entidades listadas")
		for _, s := range r.stubs {
			if _, dup := seen[s.EntityID.ID]; dup {
				continue
			}
			if _, inP1 := phase1EntityIDs[s.EntityID.ID]; inP1 {
				continue // já coberto na Fase 1
			}
			seen[s.EntityID.ID] = struct{}{}
			p2Stubs = append(p2Stubs, s)
		}
	}

	if len(p2Stubs) > 0 {
		metricsMap := client.BatchQueryMetrics(scanCtx, p2Stubs, 60)

		log.Info().Str("cluster", clusterNorm).
			Int("entities_p2", len(p2Stubs)).
			Int("with_metrics", len(metricsMap)).
			Msg("[OneAgentSignals] Fase 2: métricas coletadas")

		for _, stub := range p2Stubs {
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

			// Ignorar componentes internos do Dynatrace (OneAgent Operator, ActiveGate, etc.)
			if isDynatraceInternal(signal.WorkloadName) {
				continue
			}

			signal.ErrorRate = metrics["error_rate"]
			signal.ResponseP90Ms = metrics["response_p90"]
			signal.PodRestarts = metrics["pod_restarts"]
			signal.CPUThrottlePct = metrics["cpu_throttle"]
			if v, ok := metrics["pods_ready_pct"]; ok {
				signal.PodsReadyPct = v * 100
			}

			signal.RiskLevel, signal.RiskReasons = evaluateRisk(signal, thresholds)

			key := strings.ToLower(signal.Namespace) + "/" + strings.ToLower(signal.WorkloadName)
			if _, covered := existingDTWorkloads[key]; covered {
				signal.HasDTProblem = true
			}

			if depRegistry != nil && signal.WorkloadName != "" {
				signal.DependedBy, signal.DependsOn = resolveDepLinks(depRegistry, signal.WorkloadName, signal.Cluster, signal.Namespace)
			}

			signals = append(signals, signal)
		}
	} else if len(phase1EntityIDs) == 0 {
		log.Warn().Str("cluster", clusterNorm).
			Msg("[OneAgentSignals] Nenhuma entidade encontrada — verificar: (1) tag dt.host_group.id no OneAgent, (2) nome do cluster no DT sem '-admin', (3) token DT tem permissão 'Read entities'")
	}

	// Ordenar: Fase 1 (FromK8sIssue) primeiro, depois por risk level descendente
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].FromK8sIssue != signals[j].FromK8sIssue {
			return signals[i].FromK8sIssue // Fase 1 sempre antes
		}
		return signals[i].RiskLevel.Weight() > signals[j].RiskLevel.Weight()
	})

	log.Info().
		Str("cluster", cluster).
		Int("phase1", len(troubledWorkloads)).
		Int("phase2_entities", len(p2Stubs)).
		Int("total_signals", len(signals)).
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
		svcLower := strings.ToLower(dep.ServiceName)
		if strings.Contains(svcLower, wlLower) || strings.Contains(wlLower, svcLower) {
			if dep.Namespace != namespace {
				dependedBySet[dep.Namespace+"/"+dep.ServiceName] = struct{}{}
			}
		} else if dep.Cluster == cluster && dep.Namespace == namespace {
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
