package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseMergeTreeNew(t *testing.T) {
	out := "abc123\na.txt\nb/c.txt\n\nAuto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt"
	if got := parseMergeTreeNew(out); !reflect.DeepEqual(got, []string{"a.txt", "b/c.txt"}) {
		t.Fatalf("veio %v", got)
	}
}

func TestSimulateMergeERevList(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) } //nolint:errcheck
	g := func(args ...string) string {
		out, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	g("init", "-b", "main")
	write("a.txt", "base\n")
	g("add", ".")
	g("commit", "-m", "base")

	g("checkout", "-b", "limpa")
	write("novo.txt", "x\n")
	g("add", ".")
	g("commit", "-m", "novo")

	g("checkout", "main")
	g("checkout", "-b", "conflita")
	write("a.txt", "feat\n")
	g("commit", "-am", "feat")
	g("checkout", "main")
	write("a.txt", "main\n")
	g("commit", "-am", "main muda")

	if checked, conflicts, _ := simulateMerge(dir, "main", "limpa"); !checked || conflicts {
		t.Fatalf("limpa: checked=%v conflicts=%v", checked, conflicts)
	}
	checked, conflicts, files := simulateMerge(dir, "main", "conflita")
	if !checked || !conflicts || !reflect.DeepEqual(files, []string{"a.txt"}) {
		t.Fatalf("conflita: checked=%v conflicts=%v files=%v", checked, conflicts, files)
	}
	if out, _ := runGit(dir, "rev-list", "--count", "main..conflita"); out != "1" {
		t.Fatalf("ahead = %q", out)
	}
}

func TestFetchPRMergeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"state": "closed", "merged": true, "mergeable": nil, "mergeable_state": "unknown"}) //nolint:errcheck
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()

	st, err := fetchPRMergeStatus(context.Background(), "tok", "o", "r", 3)
	if err != nil || st.State != "merged" || st.Mergeable != nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}
