package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPRFilesPagina(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/o/r/pulls/7/files" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("requisição inesperada: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		n := 100
		if r.URL.Query().Get("page") == "2" {
			n = 3
		}
		files := make([]PRDiffFile, n)
		for i := range files {
			files[i] = PRDiffFile{Filename: fmt.Sprintf("f%d", i), Status: "modified", Additions: 1, Patch: "@@ -1 +1 @@\n-a\n+b"}
		}
		json.NewEncoder(w).Encode(files) //nolint:errcheck
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()

	files, err := listPRFiles(context.Background(), "tok", "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 103 || calls != 2 {
		t.Fatalf("esperava 103 arquivos em 2 chamadas, veio %d em %d", len(files), calls)
	}
}
