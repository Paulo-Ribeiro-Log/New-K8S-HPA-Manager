package alerts

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"k8s-hpa-manager/internal/monitoring/discovery"
)

// TestGetAlerts_SecondCallUsesNegativeCache cobre o bug real corrigido: cada endpoint HTTP que
// consulta alertas (hpa/nodepool/summary/geral) cria seu próprio *Client, mas eles agora
// compartilham o cache negativo de discovery — uma 2ª tentativa contra a mesma URL Prometheus
// (ainda dentro do TTL) não deve gerar uma 2ª requisição real, só reaproveitar o erro já conhecido.
func TestGetAlerts_SecondCallUsesNegativeCache(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Cleanup(func() { discovery.MarkPrometheusReachable(srv.URL) })

	client := NewClient(srv.URL)

	if _, err := client.GetAlerts(); err == nil {
		t.Fatalf("esperava erro na 1ª chamada (servidor retorna 500)")
	}

	// Novo Client, mesma URL — simula o padrão real de cada handler HTTP criando sua própria
	// instância (alerts.NewClient(promURL) por endpoint).
	client2 := NewClient(srv.URL)
	if _, err := client2.GetAlerts(); err == nil {
		t.Fatalf("esperava erro na 2ª chamada (vindo do cache negativo)")
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("esperava exatamente 1 requisição real ao servidor (2ª deveria vir do cache negativo), got %d", got)
	}
}

// TestGetAlerts_SuccessClearsNegativeCache garante que uma resposta válida volta a permitir
// requisições reais, sem depender do TTL expirar sozinho.
func TestGetAlerts_SuccessClearsNegativeCache(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"alerts":[]}}`))
	}))
	defer srv.Close()
	t.Cleanup(func() { discovery.MarkPrometheusReachable(srv.URL) })

	client := NewClient(srv.URL)
	if _, err := client.GetAlerts(); err != nil {
		t.Fatalf("1ª chamada não deveria falhar: %v", err)
	}
	if _, err := client.GetAlerts(); err != nil {
		t.Fatalf("2ª chamada não deveria falhar: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("esperava 2 requisições reais (sucesso não deveria ficar preso em nenhum cache negativo), got %d", got)
	}
}
