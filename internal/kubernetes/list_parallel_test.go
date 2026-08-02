package kubernetes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestListNamespacedInParallel_RunsConcurrently cobre o bug real de performance corrigido:
// ListConfigMaps/ListIngresses/ListDaemonSets/ListStatefulSets/ListSecrets/ListDeployments
// faziam 1 chamada List(ns) por namespace, sequencialmente, sempre que o caller pedia múltiplos
// namespaces específicos (não "todos"). Um "listFn" com atraso artificial + contador de pico de
// chamadas simultâneas confirma que agora rodam em paralelo (pedido do usuário: pelo menos 3).
func TestListNamespacedInParallel_RunsConcurrently(t *testing.T) {
	const numNamespaces = 12
	const perCallDelay = 50 * time.Millisecond

	var inFlight int32
	var peakInFlight int32
	var mu sync.Mutex

	namespaces := make(map[string]struct{}, numNamespaces)
	for i := 0; i < numNamespaces; i++ {
		namespaces[string(rune('a'+i))] = struct{}{}
	}

	listFn := func(ctx context.Context, ns string) ([]string, error) {
		cur := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if cur > peakInFlight {
			peakInFlight = cur
		}
		mu.Unlock()
		defer atomic.AddInt32(&inFlight, -1)

		time.Sleep(perCallDelay)
		return []string{ns}, nil
	}

	start := time.Now()
	all, err := listNamespacedInParallel(context.Background(), namespaces, listFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(all) != numNamespaces {
		t.Fatalf("esperava %d itens, got %d", numNamespaces, len(all))
	}
	if peakInFlight < 3 {
		t.Errorf("esperava pelo menos 3 chamadas simultâneas, pico observado foi %d", peakInFlight)
	}

	sequentialCost := time.Duration(numNamespaces) * perCallDelay
	if elapsed > sequentialCost/2 {
		t.Errorf("execução levou %s — esperava bem menos que a metade do custo sequencial (%s)", elapsed, sequentialCost)
	}
}

// TestListNamespacedInParallel_PropagatesError garante que uma falha isolada num namespace ainda
// vira erro pro caller (comportamento equivalente ao loop sequencial original, que abortava no
// primeiro erro).
func TestListNamespacedInParallel_PropagatesError(t *testing.T) {
	namespaces := map[string]struct{}{"ns-ok": {}, "ns-falha": {}}
	wantErr := errors.New("falha simulada")

	_, err := listNamespacedInParallel(context.Background(), namespaces, func(ctx context.Context, ns string) ([]string, error) {
		if ns == "ns-falha" {
			return nil, wantErr
		}
		return []string{ns}, nil
	})
	if err == nil {
		t.Fatalf("esperava erro propagado")
	}
}

// TestPrefetchByNamespace_GroupsResultsAndToleratesFailure cobre o uso em ListSecrets/
// ListDeployments (cache de Services/Pods por namespace): resultados devem vir agrupados por
// namespace, e uma falha isolada num namespace não deve derrubar os outros (mesma semântica
// best-effort do cache lazy que isto substituiu).
func TestPrefetchByNamespace_GroupsResultsAndToleratesFailure(t *testing.T) {
	namespaces := map[string]struct{}{"ns-a": {}, "ns-b": {}, "ns-falha": {}}

	out := prefetchByNamespace(context.Background(), namespaces, func(ctx context.Context, ns string) ([]string, error) {
		if ns == "ns-falha" {
			return nil, errors.New("falha simulada")
		}
		return []string{"item-" + ns}, nil
	})

	if len(out["ns-a"]) != 1 || out["ns-a"][0] != "item-ns-a" {
		t.Errorf("ns-a incorreto: %+v", out["ns-a"])
	}
	if len(out["ns-b"]) != 1 || out["ns-b"][0] != "item-ns-b" {
		t.Errorf("ns-b incorreto: %+v", out["ns-b"])
	}
	if len(out["ns-falha"]) != 0 {
		t.Errorf("ns-falha deveria ter resultado vazio (best-effort), got %+v", out["ns-falha"])
	}
}
