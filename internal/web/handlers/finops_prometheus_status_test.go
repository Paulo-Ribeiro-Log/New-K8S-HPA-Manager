package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbePrometheus(t *testing.T) {
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prom.Close()

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	h := &FinOpsHandler{}
	if st := h.probePrometheus(context.Background(), down.URL+"/", false); st.Available || !strings.Contains(st.Reason, "CrashLoop") {
		t.Fatalf("503 deveria indicar pod fora do ar, veio %+v", st)
	}
	if st := h.probePrometheus(context.Background(), notFound.URL+"/", false); st.Available || !strings.Contains(st.Reason, "404") {
		t.Fatalf("404 deveria ficar indisponível com motivo 404, veio %+v", st)
	}
	if st := h.probePrometheus(context.Background(), prom.URL+"/", false); !st.Available {
		t.Fatalf("Prometheus válido deveria ficar disponível, veio %+v", st)
	}
	if st := h.probePrometheus(context.Background(), "", false); st.Available {
		t.Fatal("URL vazia não pode ficar disponível")
	}
	if len(h.promStatusCache) != 4 {
		t.Fatalf("cache deveria ter 4 entradas, tem %d", len(h.promStatusCache))
	}
}
