// Package buildinfo diz QUAL build está rodando: commit, se havia mudanças sem commit, quando o
// binário foi gerado e qual bundle do frontend ele embute. Existe porque, sem isso, "está no build
// antigo?" só se descobria por tentativa e erro (hard refresh, reinício esquecido, binário de outro commit).
//
// O Go embute o commit sozinho (debug.ReadBuildInfo) em qualquer `go build` dentro de um repositório
// git — não depende do `make build` nem de ldflags, ao contrário de updater.Version.
package buildinfo

import (
	"io/fs"
	"os"
	"regexp"
	"runtime/debug"
	"sync"
	"time"

	"k8s-hpa-manager/internal/updater"
)

// Info descreve o build em execução.
type Info struct {
	Version     string `json:"version"`      // updater.Version (ldflags do make build) ou "dev"
	Commit      string `json:"commit"`       // SHA completo do commit compilado ("" se desconhecido)
	CommitShort string `json:"commit_short"` // 7 primeiros caracteres
	CommitTime  string `json:"commit_time"`  // data do commit (RFC3339)
	Dirty       bool   `json:"dirty"`        // havia mudanças sem commit ao compilar
	BuiltAt     string `json:"built_at"`     // quando o arquivo do binário foi gerado (RFC3339, melhor esforço)
	StartedAt   string `json:"started_at"`   // quando este processo subiu
	GoVersion   string `json:"go_version"`
	// FrontendBundle é o JS principal que o frontend embutido neste binário referencia (index-XXXX.js).
	// O navegador compara com o que ele carregou: se difere, a página é de um build anterior.
	FrontendBundle string `json:"frontend_bundle,omitempty"`
}

var (
	started = time.Now()
	once    sync.Once
	cached  Info
)

// Get devolve o Info deste processo (calculado uma vez).
func Get() Info {
	once.Do(func() {
		bi, _ := debug.ReadBuildInfo()
		cached = FromBuildInfo(bi, updater.Version)
		cached.StartedAt = started.UTC().Format(time.RFC3339)
		if exe, err := os.Executable(); err == nil {
			if st, err := os.Stat(exe); err == nil {
				cached.BuiltAt = st.ModTime().UTC().Format(time.RFC3339)
			}
		}
	})
	return cached
}

// FromBuildInfo extrai o Info de um *debug.BuildInfo (separado de Get para poder ser testado).
func FromBuildInfo(bi *debug.BuildInfo, version string) Info {
	if version == "" {
		version = "dev"
	}
	in := Info{Version: version}
	if bi == nil {
		return in
	}
	in.GoVersion = bi.GoVersion
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			in.Commit = s.Value
		case "vcs.time":
			in.CommitTime = s.Value
		case "vcs.modified":
			in.Dirty = s.Value == "true"
		}
	}
	in.CommitShort = in.Commit
	if len(in.CommitShort) > 7 {
		in.CommitShort = in.CommitShort[:7]
	}
	return in
}

// Label é a forma curta para logs e telas: "1.3.49 · 4133705 (com mudanças sem commit)".
func (i Info) Label() string {
	s := i.Version
	if i.CommitShort != "" {
		s += " · " + i.CommitShort
		if i.Dirty {
			s += "*"
		}
	}
	return s
}

var bundleRe = regexp.MustCompile(`/assets/(index-[A-Za-z0-9_-]+\.js)`)

// FrontendBundle acha, no index.html do frontend embutido, o nome do bundle principal.
// Devolve "" se o index.html não existir ou não referenciar nenhum.
func FrontendBundle(fsys fs.FS) string {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return ""
	}
	if m := bundleRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}
