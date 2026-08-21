package elasticsearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNamespaceErrorCounts_HappyPath cobre o caminho feliz: query bate no endpoint certo, envia
// Basic Auth, e o parse da agregação devolve a contagem por namespace corretamente.
func TestNamespaceErrorCounts_HappyPath(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"aggregations": {
				"by_namespace": {
					"buckets": [
						{"key": "checkout", "doc_count": 42},
						{"key": "pagamentos", "doc_count": 3}
					]
				}
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "pass", "k8s-logs-*")
	counts, err := client.NamespaceErrorCounts(context.Background(), "akspriv-abastecimento-hlg", "now-15m")
	if err != nil {
		t.Fatalf("NamespaceErrorCounts: %v", err)
	}

	if counts["checkout"] != 42 || counts["pagamentos"] != 3 {
		t.Fatalf("counts = %v, want checkout=42 pagamentos=3", counts)
	}

	if capturedPath != "/k8s-logs-*/_search" {
		t.Errorf("path = %q, want /k8s-logs-*/_search", capturedPath)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if capturedAuth != wantAuth {
		t.Errorf("Authorization header = %q, want %q", capturedAuth, wantAuth)
	}

	// Confirma que o filtro de cluster foi incluído na query (achado real: sem isso, logs de
	// clusters diferentes se misturariam na mesma contagem — ver DefaultClusterField).
	bodyJSON, _ := json.Marshal(capturedBody)
	if !strings.Contains(string(bodyJSON), `"cluster_name":"akspriv-abastecimento-hlg"`) {
		t.Errorf("query não incluiu o filtro de cluster, body = %s", bodyJSON)
	}
}

// TestNamespaceErrorCounts_EmptyResult cobre "query funcionou, mas não achou nada" — deve
// retornar mapa vazio, não erro (mesmo espírito de Available/Namespaces vazio no TargetSource).
func TestNamespaceErrorCounts_EmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"aggregations": {"by_namespace": {"buckets": []}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "pass", "")
	counts, err := client.NamespaceErrorCounts(context.Background(), "cluster-a", "")
	if err != nil {
		t.Fatalf("esperava sucesso, veio erro: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("esperava mapa vazio, veio %v", counts)
	}
}

// TestNamespaceErrorCounts_HTTPError cobre falha real (auth inválida, endpoint fora do ar) — deve
// retornar erro, não um mapa vazio (distinção crítica pro TargetSource decidir Available=false).
func TestNamespaceErrorCounts_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "wrong-pass", "")
	_, err := client.NamespaceErrorCounts(context.Background(), "cluster-a", "")
	if err == nil {
		t.Fatalf("esperava erro (HTTP 401), veio sucesso")
	}
}

// TestNamespaceErrorCounts_DefaultIndexPattern confirma que indexPattern vazio cai pro default
// "*" (busca em todos os índices) em vez de gerar uma URL malformada.
func TestNamespaceErrorCounts_DefaultIndexPattern(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"aggregations": {"by_namespace": {"buckets": []}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "pass", "")
	if _, err := client.NamespaceErrorCounts(context.Background(), "cluster-a", ""); err != nil {
		t.Fatalf("NamespaceErrorCounts: %v", err)
	}
	if capturedPath != "/*/_search" {
		t.Errorf("path = %q, want /*/_search (default index pattern)", capturedPath)
	}
}

// TestTestConnection_HappyPath cobre a checagem de conectividade (GET / — usada pelo botão
// "Testar Conexão" da credencial no perfil).
func TestTestConnection_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			t.Errorf("esperava GET /, veio %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "pass", "")
	latency, err := client.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency = %d, esperava >= 0", latency)
	}
}
