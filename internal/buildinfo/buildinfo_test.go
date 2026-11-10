package buildinfo

import (
	"runtime/debug"
	"testing"
	"testing/fstest"
)

func bi(settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{GoVersion: "go1.25.0", Settings: settings}
}

func TestFromBuildInfo(t *testing.T) {
	in := FromBuildInfo(bi(
		debug.BuildSetting{Key: "vcs.revision", Value: "4133705b0d1c2e3f4a5b6c7d8e9f001122334455"},
		debug.BuildSetting{Key: "vcs.time", Value: "2026-11-10T11:06:47Z"},
		debug.BuildSetting{Key: "vcs.modified", Value: "false"},
		debug.BuildSetting{Key: "GOOS", Value: "linux"},
	), "1.3.49")
	if in.Version != "1.3.49" || in.CommitShort != "4133705" || in.Commit[:7] != "4133705" || in.CommitTime != "2026-11-10T11:06:47Z" || in.Dirty || in.GoVersion != "go1.25.0" {
		t.Errorf("info: %+v", in)
	}
	if in.Label() != "1.3.49 · 4133705" {
		t.Errorf("label limpo: %q", in.Label())
	}
}

func TestFromBuildInfoComMudancasSemCommit(t *testing.T) {
	in := FromBuildInfo(bi(
		debug.BuildSetting{Key: "vcs.revision", Value: "abcdef0123456789"},
		debug.BuildSetting{Key: "vcs.modified", Value: "true"},
	), "")
	if !in.Dirty || in.Version != "dev" || in.Label() != "dev · abcdef0*" {
		t.Errorf("dirty: %+v label=%q", in, in.Label())
	}
}

func TestFromBuildInfoSemVCS(t *testing.T) {
	// binário de teste ou compilado fora de um repositório: não há vcs.*, e nada pode quebrar
	for name, b := range map[string]*debug.BuildInfo{"nil": nil, "sem settings": bi()} {
		in := FromBuildInfo(b, "dev")
		if in.Commit != "" || in.CommitShort != "" || in.Dirty || in.Label() != "dev" {
			t.Errorf("%s: %+v label=%q", name, in, in.Label())
		}
	}
}

func TestFrontendBundle(t *testing.T) {
	html := `<!doctype html><html><head>
<script type="module" crossorigin src="/assets/index-DHpr11oR.js"></script>
<link rel="modulepreload" href="/assets/vendor-AbCd_-12.js">
<link rel="stylesheet" href="/assets/index-Clf8UfOz.css"></head></html>`
	if got := FrontendBundle(fstest.MapFS{"index.html": {Data: []byte(html)}}); got != "index-DHpr11oR.js" {
		t.Errorf("bundle = %q", got)
	}
	if got := FrontendBundle(fstest.MapFS{"index.html": {Data: []byte("<html>sem script</html>")}}); got != "" {
		t.Errorf("sem bundle deveria dar vazio: %q", got)
	}
	if got := FrontendBundle(fstest.MapFS{}); got != "" {
		t.Errorf("sem index.html deveria dar vazio: %q", got)
	}
}

func TestGetEhEstavelEPreencheOProcesso(t *testing.T) {
	a, b := Get(), Get()
	if a != b {
		t.Error("Get deveria devolver sempre o mesmo valor")
	}
	if a.StartedAt == "" || a.Version == "" {
		t.Errorf("faltou StartedAt/Version: %+v", a)
	}
}
