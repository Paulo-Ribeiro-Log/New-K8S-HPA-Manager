package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHealthPayloadTrazOBuildEOBundleDoFrontend(t *testing.T) {
	b, err := json.Marshal(healthPayload())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Status  string `json:"status"`
		Mode    string `json:"mode"`
		Version string `json:"version"`
		Build   struct {
			Version        string `json:"version"`
			StartedAt      string `json:"started_at"`
			GoVersion      string `json:"go_version"`
			FrontendBundle string `json:"frontend_bundle"`
		} `json:"build"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Mode != "web" {
		t.Errorf("campos de sempre: %+v", got)
	}
	if got.Version == "1.0.0-poc" || got.Version == "" || got.Version != got.Build.Version {
		t.Errorf("version deve ser a real e igual a build.version: %q / %q", got.Version, got.Build.Version)
	}
	if got.Build.StartedAt == "" {
		t.Error("faltou started_at")
	}
	// o frontend embutido (internal/web/static) tem index.html apontando para um index-XXXX.js
	if !strings.HasPrefix(got.Build.FrontendBundle, "index-") || !strings.HasSuffix(got.Build.FrontendBundle, ".js") {
		t.Errorf("frontend_bundle = %q", got.Build.FrontendBundle)
	}
	if !strings.Contains(string(b), `"commit_short"`) || !strings.Contains(string(b), `"dirty"`) || !strings.Contains(string(b), `"built_at"`) {
		t.Errorf("faltam campos do build: %s", b)
	}
}
