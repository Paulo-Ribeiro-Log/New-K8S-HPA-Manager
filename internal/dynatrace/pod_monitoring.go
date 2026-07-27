package dynatrace

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// podMonitoringCacheEntry guarda o resultado de ListMonitoredPods para um cluster com timestamp.
type podMonitoringCacheEntry struct {
	pods     map[string]bool
	cachedAt time.Time
}

// podMonitoringCache é o cache em memória por cluster — evita escanear todas as entidades
// CLOUD_APPLICATION_INSTANCE do cluster a cada request da aba Pods.
var podMonitoringCache sync.Map

const podMonitoringCacheTTL = 2 * time.Minute

// ListMonitoredPods retorna o conjunto de pods ("namespace/nome") com entidade Dynatrace
// correspondente (CLOUD_APPLICATION_INSTANCE, uma por pod) neste cluster. Cacheado em memória
// por cluster (TTL curto, mesmo espírito de gcp.IsGcloudAuthActive) — ListEntitiesByCluster
// escaneia todas as entidades do tipo, não é barato repetir por request da aba Pods.
func (c *Client) ListMonitoredPods(ctx context.Context, clusterName string) (map[string]bool, error) {
	if raw, ok := podMonitoringCache.Load(clusterName); ok {
		entry := raw.(podMonitoringCacheEntry)
		if time.Since(entry.cachedAt) < podMonitoringCacheTTL {
			return entry.pods, nil
		}
		podMonitoringCache.Delete(clusterName)
	}

	stubs, err := c.ListEntitiesByCluster(ctx, clusterName, "CLOUD_APPLICATION_INSTANCE")
	if err != nil {
		return nil, fmt.Errorf("ListMonitoredPods: %w", err)
	}

	enriched := c.EnrichEntitiesWithK8s(ctx, stubs)

	pods := make(map[string]bool, len(enriched))
	for _, stub := range enriched {
		if stub.K8sNamespace == "" || stub.K8sPodName == "" {
			continue
		}
		pods[fmt.Sprintf("%s/%s", stub.K8sNamespace, stub.K8sPodName)] = true
	}

	podMonitoringCache.Store(clusterName, podMonitoringCacheEntry{pods: pods, cachedAt: time.Now()})
	return pods, nil
}
