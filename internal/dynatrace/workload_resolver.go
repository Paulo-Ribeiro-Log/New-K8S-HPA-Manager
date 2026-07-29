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

// workloadEntityIDsCacheEntry guarda o resultado de ResolveEntityIDsForWorkload — separado de
// workloadResolverCacheEntry porque o shape difere (slice, não um único ID).
type workloadEntityIDsCacheEntry struct {
	entityIDs []string
	cachedAt  time.Time
}

var workloadEntityIDsCache sync.Map

// ResolveEntityIDsForWorkload retorna TODOS os entityIDs Dynatrace relevantes pra um workload —
// usado pelo overlay de problems (Fase 2), que precisa cobrir tanto Cloud Native Full Stack
// quanto classicFullStack.
//
// Tenta CLOUD_APPLICATION primeiro (ResolveEntityForWorkload — 1 entidade por workload inteiro,
// só existe em clusters Cloud Native Full Stack). Bug real corrigido: a MAIORIA da frota AKS usa
// OneAgent em modo classicFullStack (confirmado via `kubectl get dynakube -o yaml` contra vários
// clusters reais), que NUNCA cria CLOUD_APPLICATION — nesses clusters ResolveEntityForWorkload
// sempre retornava found=false, então o overlay de problems nunca aparecia pra nenhum Deployment
// rodando em AKS "clássico", só nos poucos clusters Cloud Native Full Stack (ex: o EKS usado pra
// validar a Fase 2 originalmente). Fallback: PROCESS_GROUP_INSTANCE via HOST_GROUP — mesma
// correlação já usada por ListMonitoredPods (pod_monitoring.go) pro indicador de monitoramento na
// aba Pods. Como cada réplica do Deployment tem sua própria PROCESS_GROUP_INSTANCE, pode retornar
// MÚLTIPLOS IDs (um por pod/processo) — problems de cada um são buscados e mesclados pelo
// chamador (deployment_behavior.go).
func (c *Client) ResolveEntityIDsForWorkload(ctx context.Context, clusterName, namespace, deploymentName string) ([]string, error) {
	key := workloadResolverCacheKey(clusterName, namespace, deploymentName)
	if raw, ok := workloadEntityIDsCache.Load(key); ok {
		entry := raw.(workloadEntityIDsCacheEntry)
		if time.Since(entry.cachedAt) < workloadResolverCacheTTL {
			return entry.entityIDs, nil
		}
		workloadEntityIDsCache.Delete(key)
	}

	var ids []string

	if id, found, err := c.ResolveEntityForWorkload(ctx, clusterName, namespace, deploymentName); err == nil && found {
		ids = []string{id}
	} else {
		pgiStubs, perr := c.ListProcessGroupInstancesByHostGroup(ctx, clusterName)
		if perr != nil {
			return nil, fmt.Errorf("ResolveEntityIDsForWorkload: %w", perr)
		}
		seen := make(map[string]bool)
		for _, stub := range c.EnrichEntitiesWithK8s(ctx, pgiStubs) {
			if !strings.EqualFold(stub.K8sNamespace, namespace) || !strings.EqualFold(stub.K8sWorkload, deploymentName) {
				continue
			}
			if seen[stub.EntityID.ID] {
				continue
			}
			seen[stub.EntityID.ID] = true
			ids = append(ids, stub.EntityID.ID)
		}
	}

	workloadEntityIDsCache.Store(key, workloadEntityIDsCacheEntry{entityIDs: ids, cachedAt: time.Now()})
	return ids, nil
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
