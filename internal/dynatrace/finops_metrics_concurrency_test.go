package dynatrace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetAllWorkloadMetrics_RunsInParallel cobre o mesmo bug real de performance já corrigido
// para EnrichEntitiesWithK8s (ver enrich_concurrency_test.go), agora achado em
// GetAllWorkloadMetrics: as 4 queries batch (avg+max × cpu+mem — as de percentile(95) foram
// removidas depois, ver comentário de metricCPUMillicores em finops_metrics.go: a família
// builtin:kubernetes.workload.* nunca suporta agregação percentile neste tenant) eram disparadas
// sequencialmente, cada uma pagando o RTT completo do endpoint metrics/query do DT (relatado
// como parte do "scans ainda estão levando 2 minutos cada", junto do mesmo problema já corrigido
// do lado Prometheus). Um servidor de teste que atrasa cada resposta e conta o pico de
// requisições simultâneas confirma que, após a correção, várias queries acontecem ao mesmo
// tempo.
func TestGetAllWorkloadMetrics_RunsInParallel(t *testing.T) {
	const numQueries = 4
	const perRequestDelay = 80 * time.Millisecond

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

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"data":[{"dimensionMap":{"k8s.namespace.name":"ns1","k8s.workload.name":"wl1"},"values":[42]}]}]}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	start := time.Now()
	metrics, err := client.GetAllWorkloadMetrics(context.Background(), 30, "test-cluster")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("GetAllWorkloadMetrics falhou: %v", err)
	}

	m, ok := metrics["ns1/wl1"]
	if !ok {
		t.Fatalf("esperava métricas de ns1/wl1, veio %+v", metrics)
	}
	// CPUP95Millicores/MemP95Bytes nunca são consultados (percentile não suportado) — ficam 0.
	if m.CPUAvgMillicores != 42 || m.CPUP95Millicores != 0 || m.CPUMaxMillicores != 42 ||
		m.MemAvgBytes != 42 || m.MemP95Bytes != 0 || m.MemMaxBytes != 42 {
		t.Fatalf("esperava avg/max = 42 e P95 = 0 (mesma resposta fake pras 4 queries reais), veio %+v", m)
	}

	if peakInFlight < 2 {
		t.Errorf("esperava pelo menos 2 queries simultâneas ao DT, pico observado foi %d", peakInFlight)
	}

	// Sequencial custaria numQueries*perRequestDelay (480ms); paralelo deve ficar bem abaixo.
	sequentialCost := time.Duration(numQueries) * perRequestDelay
	if elapsed > sequentialCost/2 {
		t.Errorf("execução levou %s — esperava bem menos que a metade do custo sequencial (%s), indicando que não está paralelizando", elapsed, sequentialCost)
	}
}
