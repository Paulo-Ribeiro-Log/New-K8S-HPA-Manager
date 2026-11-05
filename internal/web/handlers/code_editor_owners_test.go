package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/history"
)

// newDeleteRepoRouter monta o handler com uma pasta de repos temporária. O usuário da requisição
// vem do header X-Test-User (no servidor real vem do JWT, via InjectUserEmail).
func newDeleteRepoRouter(t *testing.T) (*gin.Engine, *CodeEditorHandler, *history.HistoryTracker) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ht, err := history.NewHistoryTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryTracker: %v", err)
	}
	t.Cleanup(func() { ht.Close() })

	logger := zerolog.Nop()
	h := NewCodeEditorHandler(nil, nil, ht, &logger)
	h.reposBase = filepath.Join(t.TempDir(), "repos")
	if err := os.MkdirAll(h.reposBase, 0755); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if u := c.GetHeader("X-Test-User"); u != "" {
			c.Set("user_email", u)
		}
	})
	r.DELETE("/repos/:id", h.DeleteRepo)
	return r, h, ht
}

func makeClone(t *testing.T, h *CodeEditorHandler, id string) string {
	t.Helper()
	dir := filepath.Join(h.reposBase, id)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trabalho-nao-commitado.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func doDelete(r *gin.Engine, id, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/repos/"+id, nil)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestCanDeleteRepo(t *testing.T) {
	cases := []struct {
		owner, caller string
		want          bool
	}{
		{"", "qualquer@corp", true}, // clone antigo, sem dono
		{"ana@corp", "ana@corp", true},
		{"Ana@Corp", "ana@corp", true}, // sem diferenciar maiúsculas
		{"ana@corp", "bia@corp", false},
		{"ana@corp", "", false}, // dono registrado e usuário desconhecido: nega
	}
	for _, c := range cases {
		if got := canDeleteRepo(c.owner, c.caller); got != c.want {
			t.Errorf("canDeleteRepo(%q,%q) = %v, quer %v", c.owner, c.caller, got, c.want)
		}
	}
}

func TestDeleteRepoSoODonoExclui(t *testing.T) {
	r, h, _ := newDeleteRepoRouter(t)
	dir := makeClone(t, h, "org-app")
	h.setRepoOwner("org-app", "ana@corp")

	w := doDelete(r, "org-app", "bia@corp")
	if w.Code != http.StatusForbidden {
		t.Fatalf("outro usuário: status %d, quer 403 (%s)", w.Code, w.Body.String())
	}
	if !exists(filepath.Join(dir, "trabalho-nao-commitado.txt")) {
		t.Fatal("o clone foi apagado mesmo com 403")
	}
	if h.repoOwner("org-app") != "ana@corp" {
		t.Error("o dono mudou depois de uma exclusão negada")
	}

	w = doDelete(r, "org-app", "ANA@corp")
	if w.Code != http.StatusOK {
		t.Fatalf("dono: status %d, quer 200 (%s)", w.Code, w.Body.String())
	}
	if exists(dir) {
		t.Error("o clone deveria ter sido apagado pelo dono")
	}
	if h.repoOwner("org-app") != "" {
		t.Error("o registro do dono deveria sair junto com o clone")
	}
}

func TestDeleteRepoSemDonoContinuaPermitido(t *testing.T) {
	r, h, _ := newDeleteRepoRouter(t)
	dir := makeClone(t, h, "org-antigo") // clone anterior a esta mudança: sem registro

	if w := doDelete(r, "org-antigo", "qualquer@corp"); w.Code != http.StatusOK {
		t.Fatalf("status %d, quer 200 (%s)", w.Code, w.Body.String())
	}
	if exists(dir) {
		t.Error("o clone antigo deveria ter sido apagado")
	}
}

// Exclusão fica no Change History, inclusive a negada, sem conteúdo do repositório.
func TestDeleteRepoRegistraNoHistorico(t *testing.T) {
	r, h, ht := newDeleteRepoRouter(t)
	makeClone(t, h, "org-hist")
	h.setRepoOwner("org-hist", "ana@corp")

	doDelete(r, "org-hist", "bia@corp") // negada
	doDelete(r, "org-hist", "ana@corp") // permitida

	entries, _, err := ht.Query(history.HistoryFilter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entradas=%d err=%v", len(entries), err)
	}
	var negadas, ok int
	for _, e := range entries {
		if e.Action != "code_editor_delete_repo" || e.Resource != "org-hist" {
			t.Errorf("entrada inesperada: %+v", e)
		}
		if e.Status == history.StatusFailed {
			negadas++
		} else {
			ok++
		}
	}
	if negadas != 1 || ok != 1 {
		t.Errorf("negadas=%d ok=%d, quer 1 e 1", negadas, ok)
	}
}

// "Abrir pasta": a exclusão remove só o link e nunca os arquivos, então não depende de dono.
func TestDeleteRepoPastaLocalNaoExigeDono(t *testing.T) {
	r, h, _ := newDeleteRepoRouter(t)
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "meu.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(h.reposBase, "minha-pasta")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink indisponível: %v", err)
	}
	h.setRepoOwner("minha-pasta", "ana@corp") // mesmo com um registro, o link sai para qualquer um

	if w := doDelete(r, "minha-pasta", "bia@corp"); w.Code != http.StatusOK {
		t.Fatalf("status %d, quer 200 (%s)", w.Code, w.Body.String())
	}
	if exists(link) {
		t.Error("o link deveria ter sido removido")
	}
	if !exists(filepath.Join(real, "meu.txt")) {
		t.Fatal("os arquivos da pasta local foram apagados")
	}
}

func TestRepoOwnersArquivoPrivadoESimultaneo(t *testing.T) {
	_, h, _ := newDeleteRepoRouter(t)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			id := string(rune('a' + i))
			h.setRepoOwner(id, id+"@corp")
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		if got := h.repoOwner(id); got != id+"@corp" {
			t.Errorf("dono de %s = %q (perdeu gravação concorrente)", id, got)
		}
	}
	fi, err := os.Stat(h.repoOwnersPath())
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("arquivo de donos: %v, %v (quer 0600)", fi, err)
	}
	h.setRepoOwner("x", "") // e-mail vazio não registra
	if h.repoOwner("x") != "" {
		t.Error("e-mail vazio não deveria registrar dono")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

// Clone com trabalho não commitado: a exclusão recusa (409) e nada é apagado; com force=true apaga.
func TestDeleteRepoComMudancasPendentesExigeForce(t *testing.T) {
	r, h, _ := newDeleteRepoRouter(t)
	dir := filepath.Join(h.reposBase, "org-sujo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "novo.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := doDelete(r, "org-sujo", "ana@corp")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "UNCOMMITTED_CHANGES") {
		t.Fatalf("status %d (%s), quer 409 UNCOMMITTED_CHANGES", w.Code, w.Body.String())
	}
	if !exists(filepath.Join(dir, "novo.txt")) {
		t.Fatal("o trabalho não commitado foi apagado sem confirmação")
	}

	req := httptest.NewRequest(http.MethodDelete, "/repos/org-sujo?force=true", nil)
	req.Header.Set("X-Test-User", "ana@corp")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || exists(dir) {
		t.Fatalf("com force deveria apagar: status %d", w.Code)
	}
}

// Clone limpo é apagado sem pedir confirmação.
func TestDeleteRepoLimpoNaoExigeForce(t *testing.T) {
	r, h, _ := newDeleteRepoRouter(t)
	dir := filepath.Join(h.reposBase, "org-limpo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, dir)
	if w := doDelete(r, "org-limpo", "ana@corp"); w.Code != http.StatusOK || exists(dir) {
		t.Fatalf("status %d, quer 200 e pasta removida", w.Code)
	}
}
