package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLooksLikeRenderedManifest(t *testing.T) {
	manifestUnico := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n"
	manifestMulti := "---\n# Source: chart/templates/svc.yaml\napiVersion: v1\nkind: Service\nmetadata:\n  name: a\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: b\n"

	deveRecusar := map[string]string{
		"um documento":            manifestUnico,
		"vários documentos":       manifestMulti,
		"só o segundo é manifest": "replicaCount: 2\n---\n" + manifestUnico,
	}
	for nome, y := range deveRecusar {
		if !looksLikeRenderedManifest(y) {
			t.Errorf("%s: deveria ser reconhecido como manifest", nome)
		}
	}

	valuesValidos := map[string]string{
		"vazio":              "",
		"só espaços":         "  \n\n",
		"values simples":     "replicaCount: 3\nimage:\n  repository: nginx\n  tag: \"1.25\"\n",
		"só apiVersion":      "apiVersion: v2\nname: meu-chart\n", // Chart.yaml, não manifest
		"só kind":            "kind: ClusterIP\n",
		"manifest aninhado":  "extraObjects:\n  - apiVersion: v1\n    kind: ConfigMap\n",
		"comentário com ---": "# --- seção ---\nfoo: bar\n",
		"yaml inválido":      "a: [1, 2\n",
		"lista na raiz":      "- a\n- b\n",
	}
	for nome, y := range valuesValidos {
		if looksLikeRenderedManifest(y) {
			t.Errorf("%s: values legítimo foi tratado como manifest", nome)
		}
	}
}

// A API recusa o manifest como values (install e upgrade) antes de chamar o serviço do Helm — o
// handler é montado sem serviço (nil): se a trava falhasse, o teste entraria em pânico.
func TestHelmRecusaManifestComoValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHelmHandler(nil, nil)
	r := gin.New()
	r.POST("/releases", h.Install)
	r.PUT("/releases/:release", h.Upgrade)

	manifest := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n"
	casos := []struct{ nome, metodo, url, corpo string }{
		{"install", http.MethodPost, "/releases?cluster=c1", `{"releaseName":"r","chartRef":"repo/chart","valuesYaml":` + jsonStr(manifest) + `}`},
		{"upgrade", http.MethodPut, "/releases/r?cluster=c1", `{"valuesYaml":` + jsonStr(manifest) + `}`},
	}
	for _, c := range casos {
		req := httptest.NewRequest(c.metodo, c.url, strings.NewReader(c.corpo))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, quer 400 (%s)", c.nome, w.Code, w.Body.String())
		}
		var resp struct {
			Success bool `json:"success"`
			Error   struct{ Code, Message string }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Success || resp.Error.Code != "VALUES_LOOK_LIKE_MANIFEST" || resp.Error.Message == "" {
			t.Errorf("%s: resposta inesperada: %s", c.nome, w.Body.String())
		}
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
