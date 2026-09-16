package finops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/dynatrace"
)

// TestDTEnricher_EnrichWorkloads_FallsBackToAvgWhenNoP95 cobre o bug real corrigido: a família
// de métricas do Dynatrace usada pelo FinOps (builtin:kubernetes.workload.*) nunca supre
// percentile(95) neste tenant — o gate antigo de EnrichWorkloads exigia CPUP95Millicores ou
// MemP95Bytes > 0 pra considerar um workload "enriquecido", o que descartava TODO workload
// silenciosamente (nunca CPURecommendedMillis/WasteBRL calculados, mesmo com avg/max reais
// disponíveis). Confirma que, com avg > 0 e P95 sempre 0 (resposta fake do servidor de teste),
// o workload é enriquecido e CPURecommendedMillis/MemRecommendedMi são calculados a partir do
// avg (não ficam 0).
func TestDTEnricher_EnrichWorkloads_FallsBackToAvgWhenNoP95(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selector := r.URL.Query().Get("metricSelector")
		w.Header().Set("Content-Type", "application/json")
		// Confirma o bug real corrigido em finops_metrics.go: a query agora SEMPRE inclui
		// filter(and(eq("k8s.cluster.name","..."))) — sem isso, o tenant DT (compartilhado entre
		// toda a frota) agregaria o mesmo namespace/workload através de outros clusters.
		if !strings.Contains(selector, `filter(and(eq("k8s.cluster.name","test-cluster")))`) {
			t.Fatalf("selector deveria filtrar por k8s.cluster.name=\"test-cluster\", veio: %s", selector)
		}
		var value float64
		switch {
		case strings.Contains(selector, "cpu_usage") && strings.Contains(selector, ":avg:"):
			value = 500 // 500 mCPU avg
		case strings.Contains(selector, "cpu_usage") && strings.Contains(selector, ":max:"):
			value = 800
		case strings.Contains(selector, "memory_working_set") && strings.Contains(selector, ":avg:"):
			value = 200 * 1048576 // 200Mi
		case strings.Contains(selector, "memory_working_set") && strings.Contains(selector, ":max:"):
			value = 300 * 1048576 // 300Mi
		default:
			t.Fatalf("selector inesperado nesta 1ª rodada (não deveria pedir percentile): %s", selector)
		}
		body := fmt.Sprintf(`{"result":[{"data":[{"dimensionMap":{"k8s.namespace.name":"ns-a","k8s.workload.name":"app-a"},"values":[%f]}]}]}`, value)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client, err := dynatrace.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	enricher := NewDTEnricher(client, 30, "test-cluster")
	workloads := []FinOpsWorkload{
		{Namespace: "ns-a", Workload: "app-a", CPURequestMillis: 1000, MemRequestMi: 1024},
	}

	enriched := enricher.EnrichWorkloads(context.Background(), workloads)
	if !enriched["ns-a/app-a"] {
		t.Fatalf("esperava ns-a/app-a enriquecido, veio %+v", enriched)
	}

	wl := workloads[0]
	if wl.CPUP95Millis != 0 {
		t.Errorf("CPUP95Millis deveria continuar 0 (nunca inventado) — veio %v", wl.CPUP95Millis)
	}
	if wl.CPUAvgMillis != 500 {
		t.Errorf("CPUAvgMillis esperado 500, veio %v", wl.CPUAvgMillis)
	}
	wantCPURec := round2(500 * SafetyMargin)
	if wl.CPURecommendedMillis != wantCPURec {
		t.Errorf("CPURecommendedMillis esperado %v (avg×margem, fallback sem P95), veio %v", wantCPURec, wl.CPURecommendedMillis)
	}
	wantMemRec := round2(200 * SafetyMargin)
	if wl.MemRecommendedMi != wantMemRec {
		t.Errorf("MemRecommendedMi esperado %v (avg×margem, fallback sem P95), veio %v", wantMemRec, wl.MemRecommendedMi)
	}
	if wl.MetricsSource != "dynatrace" {
		t.Errorf("MetricsSource esperado 'dynatrace', veio %q", wl.MetricsSource)
	}
}

// TestDTEnricher_CollectionError_DistinguishesRealFailureFromEmptyCoverage cobre o bug real
// corrigido: o banner de aviso do FinOps ("Nenhum dos N workloads recebeu dado real de uso")
// sempre assumia "provável falha de coleta transitória" mesmo quando as consultas tiveram
// sucesso e o cluster genuinamente não tem cobertura DT — CollectionError() é a fonte de
// verdade que agora distingue os dois casos (ver comentário de DTEnricher.collectionErr).
func TestDTEnricher_CollectionError_DistinguishesRealFailureFromEmptyCoverage(t *testing.T) {
	workloads := []FinOpsWorkload{{Namespace: "ns-a", Workload: "app-a"}}

	t.Run("falha real (HTTP 500) fica registrada", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		}))
		defer srv.Close()

		client, err := dynatrace.NewClient(srv.URL, "test-token")
		if err != nil {
			t.Fatalf("NewClient falhou: %v", err)
		}
		enricher := NewDTEnricher(client, 30, "test-cluster")
		enricher.EnrichWorkloads(context.Background(), append([]FinOpsWorkload(nil), workloads...))

		if enricher.CollectionError() == nil {
			t.Fatal("esperava CollectionError() != nil após falha HTTP real, veio nil")
		}
	})

	t.Run("sucesso com zero cobertura NÃO conta como falha", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"data":[]}]}`))
		}))
		defer srv.Close()

		client, err := dynatrace.NewClient(srv.URL, "test-token")
		if err != nil {
			t.Fatalf("NewClient falhou: %v", err)
		}
		enricher := NewDTEnricher(client, 30, "test-cluster")
		enricher.EnrichWorkloads(context.Background(), append([]FinOpsWorkload(nil), workloads...))

		if enricher.CollectionError() != nil {
			t.Fatalf("esperava CollectionError() == nil (consulta teve sucesso, só sem dado), veio %v", enricher.CollectionError())
		}
	})
}
