package storage

import (
	"path/filepath"
	"testing"
)

func newTestUserTokensStore(t *testing.T) *UserTokensStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_user_tokens.db")
	client, err := NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	store := NewUserTokensStore(client)
	if err := store.CreateTable(); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	return store
}

// TestGetTokens_RowFromPartialInsert_NoScanError cobre o bug real corrigido: linhas criadas por
// caminhos de INSERT parciais (SaveGitHubEditorProfiles/SaveCloudAccountHints — só preenchem
// user_email + a própria coluna + preferred_provider) deixam as demais colunas (gemini_api_key,
// openai_api_key, etc.) com NULL de verdade no SQLite, não string vazia. GetTokens escaneava
// direto num Go string (não-nullable) pra essas colunas e quebrava com "converting NULL to
// string is unsupported" para qualquer usuário que só tivesse configurado um perfil do GitHub
// Editor (nunca abriu AI Settings) — confirmado ao vivo ao tentar salvar a config do Dynatrace
// no Perfil pra um usuário nessa situação.
func TestGetTokens_RowFromPartialInsert_NoScanError(t *testing.T) {
	store := newTestUserTokensStore(t)
	email := "somente-github@example.com"

	if err := store.SaveGitHubEditorProfiles(email, []GitHubEditorProfile{
		{ID: "p1", Name: "Pessoal", Token: "ghp_teste"},
	}); err != nil {
		t.Fatalf("SaveGitHubEditorProfiles falhou: %v", err)
	}

	tokens, err := store.GetTokens(email)
	if err != nil {
		t.Fatalf("GetTokens não deveria falhar numa linha parcial, got: %v", err)
	}
	if tokens == nil {
		t.Fatalf("esperava tokens não-nil (linha existe, só com colunas NULL)")
	}
	if tokens.GeminiAPIKey != "" || tokens.OpenAIAPIKey != "" || tokens.ClaudeAPIKey != "" || tokens.CopilotAPIKey != "" {
		t.Errorf("esperava strings vazias para campos NULL, got: %+v", tokens)
	}
	if tokens.DynatraceURL != "" || tokens.DynatraceToken != "" {
		t.Errorf("esperava dynatrace vazio, got url=%q token=%q", tokens.DynatraceURL, tokens.DynatraceToken)
	}

	// Confirma que dá pra seguir o merge-and-save (fluxo real do SaveConfig do Dynatrace) sem
	// perder o perfil do GitHub já salvo.
	tokens.DynatraceURL = "https://teste.live.dynatrace.com"
	tokens.DynatraceToken = "dt0c01.teste"
	if err := store.SaveTokens(email, tokens); err != nil {
		t.Fatalf("SaveTokens (merge) falhou: %v", err)
	}

	reloaded, err := store.GetTokens(email)
	if err != nil {
		t.Fatalf("GetTokens após merge falhou: %v", err)
	}
	if reloaded.DynatraceURL != "https://teste.live.dynatrace.com" || reloaded.DynatraceToken != "dt0c01.teste" {
		t.Errorf("dynatrace não persistiu corretamente: %+v", reloaded)
	}

	profiles, err := store.GetGitHubEditorProfiles(email)
	if err != nil {
		t.Fatalf("GetGitHubEditorProfiles falhou: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "Pessoal" {
		t.Errorf("perfil do GitHub deveria continuar intacto após salvar Dynatrace, got: %+v", profiles)
	}
}

// TestGetTokens_NoRow_ReturnsNilWithoutError garante que o caso genuinamente "sem nenhuma linha"
// continua funcionando como antes (nil, nil) — não confundir com o caso de linha parcial acima.
func TestGetTokens_NoRow_ReturnsNilWithoutError(t *testing.T) {
	store := newTestUserTokensStore(t)

	tokens, err := store.GetTokens("nunca-configurou-nada@example.com")
	if err != nil {
		t.Fatalf("esperava nil error para usuário sem nenhuma linha, got: %v", err)
	}
	if tokens != nil {
		t.Errorf("esperava tokens nil, got: %+v", tokens)
	}
}
