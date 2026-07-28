package dynatrace

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// workloadResolverCacheEntry guarda o resultado de ResolveEntityForWorkload com timestamp.
type workloadResolverCacheEntry struct {
	entityID string
	found    bool
	cachedAt time.Time
}

// workloadResolverCache é o cache em memória por (cluster,namespace,deployment) — mesmo espírito
// de podMonitoringCache (pod_monitoring.go): ListEntitiesByCluster escaneia todas as entidades
// CLOUD_APPLICATION do cluster, não é barato repetir a cada request do gráfico de comportamento
// nem do overlay de problems (Fase 2, que reaproveita esta função).
var workloadResolverCache sync.Map

const workloadResolverCacheTTL = 5 * time.Minute

func workloadResolverCacheKey(clusterName, namespace, deploymentName string) string {
	return fmt.Sprintf("%s/%s/%s", clusterName, namespace, deploymentName)
}

// ResolveEntityForWorkload resolve o entityID Dynatrace (CLOUD_APPLICATION) de um Deployment,
// compondo ListEntitiesByCluster + EnrichEntitiesWithK8s + filtro por K8sNamespace/K8sWorkload
// (comparação case-insensitive — tags do OneAgent nem sempre preservam a casing exata do K8s).
// Cacheado em memória por (cluster,namespace,deployment). Compartilhado entre o gráfico de
// comportamento (Fase 1) e o overlay de problems (Fase 2) — não duplicar esta resolução.
func (c *Client) ResolveEntityForWorkload(ctx context.Context, clusterName, namespace, deploymentName string) (string, bool, error) {
	key := workloadResolverCacheKey(clusterName, namespace, deploymentName)
	if raw, ok := workloadResolverCache.Load(key); ok {
		entry := raw.(workloadResolverCacheEntry)
		if time.Since(entry.cachedAt) < workloadResolverCacheTTL {
			return entry.entityID, entry.found, nil
		}
		workloadResolverCache.Delete(key)
	}

	stubs, err := c.ListEntitiesByCluster(ctx, clusterName, "CLOUD_APPLICATION")
	if err != nil {
		return "", false, fmt.Errorf("ResolveEntityForWorkload: %w", err)
	}

	enriched := c.EnrichEntitiesWithK8s(ctx, stubs)

	var entityID string
	found := false
	for _, stub := range enriched {
		if strings.EqualFold(stub.K8sNamespace, namespace) && strings.EqualFold(stub.K8sWorkload, deploymentName) {
			entityID = stub.EntityID.ID
			found = true
			break
		}
	}

	workloadResolverCache.Store(key, workloadResolverCacheEntry{entityID: entityID, found: found, cachedAt: time.Now()})
	return entityID, found, nil
}

// GetDeploymentBehaviorMetrics busca a série temporal de métricas de workload K8s
// (k8sWorkloadMetricDefs: pods_running/pods_ready_pct/pod_restarts/cpu_milli/cpu_throttle/
// memory_mb) para uma entidade CLOUD_APPLICATION já resolvida via ResolveEntityForWorkload —
// fonte FALLBACK de série temporal do gráfico de comportamento (DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md,
// Fase 1) quando o cluster não tem Prometheus instalado. Reaproveita getMetricsBatch (já usado
// pelo fluxo de métricas de problems) — não duplicar a paralelização por métrica.
func (c *Client) GetDeploymentBehaviorMetrics(ctx context.Context, entityID string, from, to time.Time) []MetricSeriesData {
	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)
	resolution := autoResolution(from, to)
	return c.getMetricsBatch(ctx, entityID, k8sWorkloadMetricDefs, fromStr, toStr, resolution)
}
