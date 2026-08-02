package dynatrace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListMonitoredPods_AuthFailure_ReturnsError cobre o bug real corrigido: antes, as duas
// goroutines internas de ListMonitoredPods engoliam qualquer erro (incluindo 401/403 de token
// inválido) e a função sempre retornava (mapa vazio, nil) — uma falha de autenticação real ficava
// indistinguível de "cluster genuinamente sem instrumentação Dynatrace". Um servidor de teste que
// sempre responde 401 simula um token revogado/expirado; ambos os caminhos (CLOUD_APPLICATION_INSTANCE
// via KUBERNETES_CLUSTER/tags e PROCESS_GROUP_INSTANCE via HOST_GROUP) devem esgotar suas
// tentativas e a função deve retornar erro não-nulo.
func TestListMonitoredPods_AuthFailure_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"token invalid"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token-invalido")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	pods, err := client.ListMonitoredPods(context.Background(), "cluster-teste-401-auth-failure")
	if err == nil {
		t.Fatalf("esperava erro de autenticação, ListMonitoredPods retornou sucesso com %d pods", len(pods))
	}
}

// TestListMonitoredPods_NoInstrumentation_NoError cobre o caso normal (não é bug): um cluster que
// simplesmente não usa nenhum dos dois modos de instrumentação retorna listas vazias das duas
// fontes, sem nenhum erro HTTP — isso NÃO deve virar um "erro de autenticação" (mapa vazio, nil).
func TestListMonitoredPods_NoInstrumentation_NoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"entities":[],"nextPageKey":""}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token-valido")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	pods, err := client.ListMonitoredPods(context.Background(), "cluster-teste-sem-instrumentacao")
	if err != nil {
		t.Fatalf("cluster sem instrumentação não deveria produzir erro, got: %v", err)
	}
	if len(pods) != 0 {
		t.Fatalf("esperava mapa vazio, got %d entradas", len(pods))
	}
}
