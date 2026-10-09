package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

const (
	dtLoginEmail = "4960023587.ca@empresa.com.br" // conta ativa do az (login do app)
	dtSSOEmail   = "fulano.silva@empresa.com.br"  // e-mail corporativo do Perfil SSO
)

func newDTTestStore(t *testing.T) *storage.UserTokensStore {
	t.Helper()
	client, err := storage.NewSQLiteClient(filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	store := storage.NewUserTokensStore(client)
	if err := store.CreateTable(); err != nil {
		t.Fatal(err)
	}
	return store
}

// withSSOProfile aponta o resolvedor para um diretório com (ou sem) sso_profile.json.
func withSSOProfile(t *testing.T, email string) {
	t.Helper()
	dir := t.TempDir()
	if email != "" {
		b, _ := json.Marshal(map[string]string{"email": email, "matricula": "123"})
		if err := os.WriteFile(filepath.Join(dir, "sso_profile.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	SetDynatraceSSOProfileDir(dir)
	t.Cleanup(func() { SetDynatraceSSOProfileDir("") })
}

func TestDynatraceIdentity(t *testing.T) {
	withSSOProfile(t, "")
	if id, src := dynatraceIdentity(dtLoginEmail); id != dtLoginEmail || src != "login" {
		t.Errorf("sem Perfil SSO: %s %s", id, src)
	}
	withSSOProfile(t, dtSSOEmail)
	if id, src := dynatraceIdentity(dtLoginEmail); id != dtSSOEmail || src != "sso" {
		t.Errorf("com Perfil SSO: %s %s", id, src)
	}
	if got := dynatraceLookupEmails(dtLoginEmail); len(got) != 2 || got[0] != dtSSOEmail || got[1] != dtLoginEmail {
		t.Errorf("ordem de leitura: %v", got)
	}
	withSSOProfile(t, dtLoginEmail) // SSO igual ao login: uma chave só
	if got := dynatraceLookupEmails(dtLoginEmail); len(got) != 1 {
		t.Errorf("sem duplicar chave: %v", got)
	}
}

func TestDynatraceLegacyCredentialsStillFound(t *testing.T) {
	store := newDTTestStore(t)
	// credencial gravada antes da correção: sob o e-mail do login
	if err := store.SaveTokens(dtLoginEmail, &storage.UserTokens{UserEmail: dtLoginEmail, PreferredProvider: "ollama",
		DynatraceURL: "https://abc123.live.dynatrace.com", DynatraceToken: "dt0c01.LEGADO"}); err != nil {
		t.Fatal(err)
	}
	withSSOProfile(t, dtSSOEmail)

	tokens, key := dynatraceTokensFor(store, dtLoginEmail)
	if key != dtLoginEmail || tokens.DynatraceToken != "dt0c01.LEGADO" {
		t.Fatalf("legado não encontrado: key=%q %+v", key, tokens)
	}
	if c, err := dynatraceClientForCluster(store, dtLoginEmail, ""); err != nil || c.BaseURL() == "" {
		t.Errorf("cliente com a credencial legada: %v", err)
	}
	resp := dynatraceConfigResponse(tokens, dtLoginEmail, key)
	if resp["identity_email"] != dtSSOEmail || resp["identity_source"] != "sso" || resp["stored_under_login"] != true {
		t.Errorf("config: %+v", resp)
	}
}

func TestDynatraceSaveConfigMovesToSSOIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newDTTestStore(t)
	if err := store.SaveTokens(dtLoginEmail, &storage.UserTokens{UserEmail: dtLoginEmail, PreferredProvider: "ollama",
		DynatraceURL: "https://abc123.live.dynatrace.com", DynatraceToken: "dt0c01.LEGADO"}); err != nil {
		t.Fatal(err)
	}
	withSSOProfile(t, dtSSOEmail)
	h := &DynatraceHandler{tokensStore: store}

	// salvar só o filtro de tags (sem redigitar o token)
	body, _ := json.Marshal(map[string]string{"dynatrace_tag_filter": "env:prd"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/dynatrace/config", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_email", dtLoginEmail)
	h.SaveConfig(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	saved, err := store.GetTokens(dtSSOEmail)
	if err != nil || saved == nil {
		t.Fatalf("nada gravado sob o e-mail do SSO: %v", err)
	}
	if saved.DynatraceToken != "dt0c01.LEGADO" || saved.DynatraceURL == "" || saved.DynatraceTagFilter != "env:prd" {
		t.Errorf("token/URL perdidos ou filtro não salvo: %+v", saved)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["identity_email"] != dtSSOEmail || resp["stored_under_login"] != false {
		t.Errorf("resposta: %+v", resp)
	}
}
