package dynatrace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"k8s-hpa-manager/internal/storage"
)

// TestLogIngestion_Integration confirma se o OneAgent (ou Log Module) do Dynatrace está de fato
// ingerindo logs no tenant configurado — não só se a API responde, mas se existe QUALQUER linha
// de log recente indexada. Roda contra o tenant real, por isso é gated como as demais integrações
// deste repositório (padrão testing.Short() + sufixo _Integration, ver
// internal/monitoring/client/prometheus_client_test.go):
//
//	go test -v ./internal/dynatrace/ -run TestLogIngestion_Integration
//
// Credenciais: tenta reaproveitar a config já salva em ~/.k8s-hpa-manager/ai_diagnostics.db (a
// mesma usada pela aba AI Settings da aplicação — GetDynatraceConfig pega a mais recente entre
// os usuários); se não achar, cai para as env vars DT_API_URL/DT_API_TOKEN (mesmo fallback usado
// por dynatrace.NewClient).
//
// Filtro opcional por namespace/pod via DT_LOG_TEST_QUERY, ex:
//
//	DT_LOG_TEST_QUERY='k8s.namespace.name=="meu-namespace"' go test -v ./internal/dynatrace/ -run TestLogIngestion_Integration
func TestLogIngestion_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	dtURL, dtToken := loadDynatraceCredsForTest(t)
	client, err := NewClient(dtURL, dtToken)
	if err != nil {
		t.Skipf("Dynatrace não configurado (nem em ai_diagnostics.db, nem em DT_API_URL/DT_API_TOKEN): %v", err)
	}

	query := os.Getenv("DT_LOG_TEST_QUERY")
	if query != "" {
		t.Logf("filtrando por: %s", query)
	}

	result, err := client.SearchRecentLogs(context.Background(), query, 5)
	if err != nil {
		// 403 = token sem escopo logs.read; 404 = endpoint inexistente nesse tenant/versão.
		// Nenhum dos dois confirma que a ingestão está desligada, só que esta checagem via API
		// não conseguiu confirmar — nesse caso, verificar manualmente pela UI (Apps → Logs).
		t.Logf("chamada à API de logs falhou: %v", err)
		t.Log("isso NÃO confirma que a ingestão está desligada — só que esta checagem via API não conseguiu confirmar. Verifique manualmente: Dynatrace UI → Apps → Logs.")
		return
	}

	if len(result.Results) == 0 {
		t.Log("API respondeu OK mas 0 linhas de log encontradas — log ingestion provavelmente NÃO está ativo (ou o filtro/janela de tempo não bateu com nada recente).")
		return
	}

	t.Logf("✅ log ingestion ATIVO — %d linha(s) de log encontrada(s) na amostra. Prévia:", len(result.Results))
	for i, entry := range result.Results {
		if i >= 3 {
			break
		}
		pretty, _ := json.MarshalIndent(entry, "  ", "  ")
		t.Logf("  [%d] %s", i, string(pretty))
	}
}

// loadDynatraceCredsForTest tenta ler a config Dynatrace já salva pela aplicação (mesmo arquivo
// usado pela aba AI Settings); retorna strings vazias em qualquer falha, deixando NewClient cair
// para o fallback de env vars (DT_API_URL/DT_API_TOKEN) — nunca falha o teste por conta disso.
func loadDynatraceCredsForTest(t *testing.T) (string, string) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	dbPath := filepath.Join(home, ".k8s-hpa-manager", "ai_diagnostics.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", ""
	}

	sqliteClient, err := storage.NewSQLiteClient(dbPath)
	if err != nil {
		t.Logf("não foi possível abrir %s: %v (tentando env vars)", dbPath, err)
		return "", ""
	}
	defer sqliteClient.Close()

	store := storage.NewUserTokensStore(sqliteClient)
	dtURL, dtToken, ok := store.GetDynatraceConfig()
	if !ok {
		return "", ""
	}
	t.Logf("usando config Dynatrace salva em %s", dbPath)
	return dtURL, dtToken
}
