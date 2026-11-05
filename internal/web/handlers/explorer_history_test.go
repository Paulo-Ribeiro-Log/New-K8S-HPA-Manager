package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
)

func TestExplorerResourceLabel(t *testing.T) {
	if got := explorerResourceLabel("secrets", "app", "db"); got != "secrets/app/db" {
		t.Errorf("com namespace: %q", got)
	}
	if got := explorerResourceLabel("clusterroles", "", "admin"); got != "clusterroles/admin" {
		t.Errorf("sem namespace: %q", got)
	}
}

// O Explorer aplica qualquer tipo, inclusive Secret: o histórico (somente-inclusão) guarda só
// SHA-256 e tamanho do YAML, nunca o conteúdo.
func TestExplorerApplyHistoryNaoGravaConteudo(t *testing.T) {
	yaml := "kind: Secret\ndata:\n  password: c3VwZXItc2VjcmV0bw==\n"
	after := explorerApplyHistory("external-secrets.io", yaml, true)
	for k, v := range after {
		if s, ok := v.(string); ok && strings.Contains(s, "c3VwZXItc2VjcmV0bw==") {
			t.Fatalf("campo %q guarda o conteúdo do YAML", k)
		}
	}
	if after["yaml_sha256"] == "" || after["yaml_bytes"] != len(yaml) || after["force"] != true || after["group"] != "external-secrets.io" {
		t.Fatalf("identificação incompleta: %v", after)
	}
	if _, ok := explorerApplyHistory("", yaml, false)["group"]; ok {
		t.Error("group vazio não deve ser gravado")
	}
}

// O que Apply e Delete gravam passa por logOperation: falha também fica registrada, com o erro.
func TestExplorerRegistraAplicarEExcluir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ht, err := history.NewHistoryTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryTracker: %v", err)
	}
	t.Cleanup(func() { ht.Close() })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/", nil)
	logOperation(c, ht, "explorer_apply", explorerResourceLabel("configmaps", "app", "cfg"), "cl",
		nil, explorerApplyHistory("", "kind: ConfigMap\n", true), time.Now(), nil)
	logOperation(c, ht, "explorer_delete", explorerResourceLabel("namespaces", "", "teste"), "cl",
		nil, nil, time.Now(), errTesteExplorer("forbidden"))

	entries, _, err := ht.Query(history.HistoryFilter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entradas=%d err=%v", len(entries), err)
	}
	got := map[string]history.HistoryEntry{}
	for _, e := range entries {
		got[e.Action] = e
	}
	if got["explorer_apply"].Resource != "configmaps/app/cfg" || got["explorer_apply"].Status != history.StatusSuccess {
		t.Errorf("apply: %+v", got["explorer_apply"])
	}
	if got["explorer_delete"].Resource != "namespaces/teste" || got["explorer_delete"].Status != history.StatusFailed || got["explorer_delete"].ErrorMsg == "" {
		t.Errorf("delete: %+v", got["explorer_delete"])
	}
}

type errTesteExplorer string

func (e errTesteExplorer) Error() string { return string(e) }
