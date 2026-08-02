package kubernetes

import (
	"context"
	"sync"
)

// listNamespacedConcurrency limita quantas chamadas namespaced simultâneas
// listNamespacedInParallel dispara — mesmo padrão/valor já usado em internal/dynatrace
// (EnrichEntitiesWithK8s/BatchQueryMetrics) e no handler de /api/v1/pods.
const listNamespacedConcurrency = 8

// listNamespacedInParallel busca um recurso namespaced para múltiplos namespaces em paralelo.
//
// Bug real de performance corrigido: ListConfigMaps/ListIngresses/ListDaemonSets/
// ListStatefulSets/ListSecrets tinham um fast path de 1 chamada só quando NENHUM namespace
// específico era pedido (List("") cobre o cluster inteiro), mas caíam num loop sequencial — uma
// chamada `List(ns)` de cada vez, esperando a anterior terminar — assim que o caller pedia
// múltiplos namespaces explícitos. Mesma classe de bug já corrigida em /api/v1/pods (PR #325):
// chamadas independentes empilhando atraso em vez de dividir.
func listNamespacedInParallel[T any](ctx context.Context, namespaces map[string]struct{}, listFn func(ctx context.Context, ns string) ([]T, error)) ([]T, error) {
	sem := make(chan struct{}, listNamespacedConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var all []T
	var firstErr error

	for ns := range namespaces {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			items, err := listFn(ctx, ns)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			all = append(all, items...)
		}(ns)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

// prefetchByNamespace busca um recurso namespaced para múltiplos namespaces em paralelo,
// retornando os resultados agrupados por namespace (ao contrário de listNamespacedInParallel,
// que devolve tudo achatado numa lista só) — usado quando o caller precisa saber quais itens
// pertencem a qual namespace, ex: um cache de Services por namespace pra associar ClusterIPs a
// Secrets/Deployments. Best-effort: uma falha isolada num namespace vira entrada vazia (mesma
// semântica que o cache lazy sequencial que isto substitui já tinha), não aborta os outros.
func prefetchByNamespace[T any](ctx context.Context, namespaces map[string]struct{}, listFn func(ctx context.Context, ns string) ([]T, error)) map[string][]T {
	sem := make(chan struct{}, listNamespacedConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := make(map[string][]T, len(namespaces))

	for ns := range namespaces {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			items, err := listFn(ctx, ns)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[ns] = nil
				return
			}
			out[ns] = items
		}(ns)
	}
	wg.Wait()
	return out
}
