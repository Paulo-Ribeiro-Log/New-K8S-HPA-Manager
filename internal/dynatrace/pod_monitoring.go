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

// ListMonitoredPods retorna o conjunto de pods ("namespace/nome") monitorados pelo Dynatrace
// neste cluster. Cacheado em memória por cluster (TTL curto) — as buscas abaixo escaneiam
// entidades do tenant, não é barato repetir por request da aba Pods.
//
// Mescla DUAS fontes, porque o modo de instrumentação do OneAgent varia por cluster (confirmado
// via `kubectl get dynakube -o yaml` contra a frota real — a maioria usa classicFullStack, só uma
// minoria usa Cloud Native Full Stack):
//   - Cloud Native Full Stack: CLOUD_APPLICATION_INSTANCE (uma por pod) — via ListEntitiesByCluster.
//   - classicFullStack (maioria real da frota): não cria CLOUD_APPLICATION_INSTANCE nenhuma — a
//     única entidade com granularidade de pod é PROCESS_GROUP_INSTANCE, correlacionada via
//     ListProcessGroupInstancesByHostGroup. Sem esse segundo caminho, ListMonitoredPods sempre
//     retornava vazio pra praticamente todo cluster real do app, mesmo com OneAgent ativo em
//     todos os nós — bug real confirmado e corrigido.
//
// Um cluster tipicamente usa só um dos dois modos, mas nada impede tentar os dois e mesclar —
// cada chamada que não encontra nada (cluster não existe naquele modo) retorna vazio sem erro.
func (c *Client) ListMonitoredPods(ctx context.Context, clusterName string) (map[string]bool, error) {
	if raw, ok := podMonitoringCache.Load(clusterName); ok {
		entry := raw.(podMonitoringCacheEntry)
		if time.Since(entry.cachedAt) < podMonitoringCacheTTL {
			return entry.pods, nil
		}
		podMonitoringCache.Delete(clusterName)
	}

	// As duas fontes agora paginam internamente (ver listEntitiesBySelectorMaxPages) — clusters
	// grandes (milhares de PROCESS_GROUP_INSTANCE, ex: akspriv-oferta-prd) levam bem mais tempo
	// que antes (quando a busca parava em 500 resultados). Rodar em paralelo evita que o tempo
	// total seja a SOMA das duas buscas — cada cluster real só usa uma das duas de qualquer
	// forma (a outra retorna vazio rápido), mas isso só é sabido depois de tentar.
	var wg sync.WaitGroup
	var caiPods, pgiPods map[string]bool
	var caiErr, pgiErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		caiPods = make(map[string]bool)
		caiStubs, err := c.ListEntitiesByCluster(ctx, clusterName, "CLOUD_APPLICATION_INSTANCE")
		if err != nil {
			caiErr = err
			return
		}
		for _, stub := range c.EnrichEntitiesWithK8s(ctx, caiStubs) {
			if stub.K8sNamespace != "" && stub.K8sPodName != "" {
				caiPods[fmt.Sprintf("%s/%s", stub.K8sNamespace, stub.K8sPodName)] = true
			}
		}
	}()

	go func() {
		defer wg.Done()
		pgiPods = make(map[string]bool)
		pgiStubs, err := c.ListProcessGroupInstancesByHostGroup(ctx, clusterName)
		if err != nil {
			pgiErr = err
			return
		}
		for _, stub := range c.EnrichEntitiesWithK8s(ctx, pgiStubs) {
			if stub.K8sNamespace != "" && stub.K8sPodName != "" {
				pgiPods[fmt.Sprintf("%s/%s", stub.K8sNamespace, stub.K8sPodName)] = true
			}
		}
	}()

	wg.Wait()

	// Só é falha de verdade quando OS DOIS caminhos falham — um cluster real só usa um dos dois
	// modos de instrumentação (ver comentário do pacote acima), e o outro retorna vazio SEM erro
	// nesse caso normal ("não existe entidade desse tipo aqui"), não por falha de autenticação/rede.
	// Bug real corrigido: antes os dois `if err == nil` engoliam qualquer erro (incluindo 401/403
	// de token expirado/revogado) sem sinal nenhum — ListMonitoredPods nunca retornava erro, então
	// uma falha de autenticação real produzia o mesmo resultado vazio de "nenhum pod monitorado",
	// indistinguível pro usuário de "cluster genuinamente sem instrumentação".
	if caiErr != nil && pgiErr != nil {
		return nil, fmt.Errorf("Dynatrace: falha ao listar entidades monitoradas (CLOUD_APPLICATION_INSTANCE: %v | PROCESS_GROUP_INSTANCE: %v)", caiErr, pgiErr)
	}

	pods := make(map[string]bool, len(caiPods)+len(pgiPods))
	for k := range caiPods {
		pods[k] = true
	}
	for k := range pgiPods {
		pods[k] = true
	}

	podMonitoringCache.Store(clusterName, podMonitoringCacheEntry{pods: pods, cachedAt: time.Now()})
	return pods, nil
}
