package dynatrace

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// k8sContainerMetricDefs — CPU/memória por CONTAINER_GROUP_INSTANCE (um container real de um pod
// específico), família de métricas DIFERENTE de k8sWorkloadMetricDefs (que é agregada por
// workload inteiro e REJEITA CLOUD_APPLICATION_INSTANCE como entidade primária — confirmado ao
// vivo contra o tenant real: "Entity type mismatch... Possible primary entity types:
// [CLOUD_APPLICATION]", sempre 0 pontos, é por isso que metricsForEntityType mapear
// CLOUD_APPLICATION_INSTANCE pra k8sWorkloadMetricDefs em metrics.go nunca funcionou de fato —
// bug pré-existente ali, não corrigido aqui pra não mudar o comportamento do overlay de
// problems/EntityMetricsData, fora do escopo desta mudança).
//
// "builtin:containers.memory.workingSet" (citado no comentário histórico de metrics.go como
// existente) NÃO existe no catálogo real deste tenant (404 tanto em GET /metrics/<id> quanto em
// metrics/query) — o nome real confirmado via busca no catálogo (GET /metrics?text=...) é
// "builtin:containers.memory.residentSetBytes" ("Containers: Memory usage, bytes"). Os dois
// selectors abaixo foram validados ao vivo contra um pod real (namespace vv-categoria-frontend-
// ex-prd, 2 containers: app + istio-proxy) — CPU retornou mCPU plausíveis (123 e 4.3), memória
// retornou MB plausível (148MB) pro container principal.
var k8sContainerMetricDefs = []metricDef{
	{"cpu_milli", "builtin:containers.cpu.usageMilliCores", "CPU Uso", "mCPU", 1},
	{"memory_mb", "builtin:containers.memory.residentSetBytes", "Memória", "MB", 1.0 / (1024 * 1024)},
}

// podEntityResolverCacheEntry — mesmo padrão de workloadResolverCacheEntry (workload_resolver.go),
// mas chaveado por pod em vez de workload.
type podEntityResolverCacheEntry struct {
	entityID string
	found    bool
	cachedAt time.Time
}

var podEntityResolverCache sync.Map

func podEntityResolverCacheKey(clusterName, namespace, podName string) string {
	return fmt.Sprintf("%s/%s/%s", clusterName, namespace, podName)
}

// ResolveEntityForPod resolve o entityID Dynatrace (CLOUD_APPLICATION_INSTANCE) de UM pod
// específico — variação de ResolveEntityForWorkload (workload_resolver.go) filtrando por
// K8sPodName em vez de K8sWorkload. Só existe em clusters Cloud Native Full Stack (mesma
// instrumentação que cria CLOUD_APPLICATION_INSTANCE — ver ListMonitoredPods em
// pod_monitoring.go); em clusters classicFullStack (maioria da frota) found=false sempre, sem
// erro — o chamador (GetDeploymentBehavior) cai de volta pro agregado por workload nesse caso.
func (c *Client) ResolveEntityForPod(ctx context.Context, clusterName, namespace, podName string) (string, bool, error) {
	key := podEntityResolverCacheKey(clusterName, namespace, podName)
	if raw, ok := podEntityResolverCache.Load(key); ok {
		entry := raw.(podEntityResolverCacheEntry)
		if time.Since(entry.cachedAt) < workloadResolverCacheTTL {
			return entry.entityID, entry.found, nil
		}
		podEntityResolverCache.Delete(key)
	}

	stubs, err := c.ListEntitiesByCluster(ctx, clusterName, "CLOUD_APPLICATION_INSTANCE")
	if err != nil {
		return "", false, fmt.Errorf("ResolveEntityForPod: %w", err)
	}

	enriched := c.EnrichEntitiesWithK8s(ctx, stubs)

	var entityID string
	found := false
	for _, stub := range enriched {
		if strings.EqualFold(stub.K8sNamespace, namespace) && strings.EqualFold(stub.K8sPodName, podName) {
			entityID = stub.EntityID.ID
			found = true
			break
		}
	}

	podEntityResolverCache.Store(key, podEntityResolverCacheEntry{entityID: entityID, found: found, cachedAt: time.Now()})
	return entityID, found, nil
}

// GetPodBehaviorMetrics busca CPU/memória ABSOLUTAS (mCPU, MB) somadas entre os containers de UM
// pod específico — fonte real de granularidade por pod no Dynatrace (diferente de
// GetDeploymentBehaviorMetrics, que é sempre agregado pro workload inteiro). Caminho:
//
//  1. CLOUD_APPLICATION_INSTANCE do pod (caiEntityID, já resolvido via ResolveEntityForPod) →
//  2. CONTAINER_GROUP_INSTANCE (um por container do pod, ex: app + istio-proxy sidecar) via a
//     relação topológica "isCgiOfCai" (confirmado ao vivo via GET /api/v2/entityTypes/
//     CONTAINER_GROUP_INSTANCE — não documentado de forma óbvia na doc pública) →
//  3. builtin:containers.cpu.usageMilliCores / builtin:containers.memory.residentSetBytes por CGI,
//     somados ponto-a-ponto entre os containers (soma = uso total do pod, mesmo espírito de
//     kube_pod_container_resource_requests somar entre containers no caminho Prometheus).
//
// Sem equivalente confirmado pra restarts/replicas nesta granularidade (métricas de container não
// carregam esse conceito) — o chamador (deployment_behavior.go) só usa cpu/memory daqui,
// replicas/restarts continuam 0 no caminho Dynatrace pod-scoped, mesma limitação documentada já
// existente pro caminho agregado (dynatraceSeriesToPointMap).
//
// found=false quando o pod não tem nenhuma CONTAINER_GROUP_INSTANCE correlacionada (cluster
// classicFullStack, ou pod não instrumentado) — o chamador cai de volta pro agregado por workload.
func (c *Client) GetPodBehaviorMetrics(ctx context.Context, caiEntityID string, from, to time.Time) ([]MetricSeriesData, bool) {
	selector := fmt.Sprintf(`type("CONTAINER_GROUP_INSTANCE"),fromRelationships.isCgiOfCai(entityId("%s"))`, caiEntityID)
	cgiStubs, err := c.listEntitiesBySelector(ctx, selector)
	if err != nil || len(cgiStubs) == 0 {
		return nil, false
	}

	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)
	resolution := autoResolution(from, to)

	// Soma ponto-a-ponto entre containers, por metricId — um mapa timestamp->valor por chave.
	sums := make(map[string]map[int64]float64, len(k8sContainerMetricDefs))
	for _, def := range k8sContainerMetricDefs {
		sums[def.Key] = make(map[int64]float64)
	}

	anyData := false
	for _, cgi := range cgiStubs {
		for _, def := range k8sContainerMetricDefs {
			pts := c.fetchMetricPoints(ctx, cgi.EntityID.ID, def, fromStr, toStr, resolution)
			if len(pts) > 0 {
				anyData = true
			}
			for _, p := range pts {
				sums[def.Key][p.T] += p.V
			}
		}
	}

	if !anyData {
		return nil, false
	}

	result := make([]MetricSeriesData, 0, len(k8sContainerMetricDefs))
	for _, def := range k8sContainerMetricDefs {
		m := sums[def.Key]
		points := make([]MetricPoint, 0, len(m))
		for ts, v := range m {
			points = append(points, MetricPoint{T: ts, V: v})
		}
		result = append(result, MetricSeriesData{Key: def.Key, Label: def.Label, Unit: def.Unit, Points: points})
	}
	return result, true
}
