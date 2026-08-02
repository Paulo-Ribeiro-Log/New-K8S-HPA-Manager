package dynatrace

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnrichEntitiesWithK8s_RunsInParallel cobre o bug real de performance corrigido:
// EnrichEntitiesWithK8s chamava GetEntity uma entidade de cada vez (sequencial) para qualquer
// stub com cache frio — comum nas entidades vindas de Problems (AffectedEntities/ImpactedEntities),
// que nunca passam por listEntitiesBySelector antes. Um servidor de teste que atrasa cada resposta
// e conta o pico de requisições simultâneas confirma que, após a correção, várias chamadas GetEntity
// acontecem ao mesmo tempo (pedido explícito: pelo menos 3 simultâneas).
func TestEnrichEntitiesWithK8s_RunsInParallel(t *testing.T) {
	const numEntities = 12
	const perRequestDelay = 100 * time.Millisecond

	var inFlight int32
	var peakInFlight int32
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if cur > peakInFlight {
			peakInFlight = cur
		}
		mu.Unlock()
		defer atomic.AddInt32(&inFlight, -1)

		time.Sleep(perRequestDelay)

		id := strings.TrimPrefix(r.URL.Path, "/api/v2/entities/")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"entityId":%q,"displayName":"display-%s","type":"PROCESS_GROUP_INSTANCE"}`, id, id)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	stubs := make([]EntityStub, numEntities)
	for i := 0; i < numEntities; i++ {
		id := fmt.Sprintf("PROCESS_GROUP_INSTANCE-%02d", i)
		stubs[i] = EntityStub{EntityID: EntityID{ID: id, Type: "PROCESS_GROUP_INSTANCE"}, Name: "orig-" + id}
	}

	start := time.Now()
	enriched := client.EnrichEntitiesWithK8s(context.Background(), stubs)
	elapsed := time.Since(start)

	if len(enriched) != numEntities {
		t.Fatalf("esperava %d entidades enriquecidas, got %d", numEntities, len(enriched))
	}

	// Ordem de entrada precisa ser preservada (índice a índice) — callers dependem disso.
	for i, e := range enriched {
		wantID := fmt.Sprintf("PROCESS_GROUP_INSTANCE-%02d", i)
		if e.EntityID.ID != wantID {
			t.Errorf("ordem não preservada no índice %d: esperava %q, got %q", i, wantID, e.EntityID.ID)
		}
		if e.DisplayName != "display-"+wantID {
			t.Errorf("displayName não enriquecido no índice %d: got %q", i, e.DisplayName)
		}
	}

	if peakInFlight < 3 {
		t.Errorf("esperava pelo menos 3 chamadas GetEntity simultâneas, pico observado foi %d", peakInFlight)
	}

	// Sequencial custaria numEntities*perRequestDelay (1.2s); paralelo com concorrência >=3 deve
	// ficar bem abaixo disso — limiar de metade do tempo sequencial dá boa margem sem ficar frágil
	// em CI mais lento.
	sequentialCost := time.Duration(numEntities) * perRequestDelay
	if elapsed > sequentialCost/2 {
		t.Errorf("execução levou %s — esperava bem menos que a metade do custo sequencial (%s), indicando que não está paralelizando", elapsed, sequentialCost)
	}
}
