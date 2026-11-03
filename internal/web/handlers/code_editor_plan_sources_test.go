package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tfPlan = "Terraform used the selected providers to generate the following execution plan.\n" +
	"  # aws_s3_bucket.x will be created\n  + resource \"aws_s3_bucket\" \"x\" {}\n\n" +
	"Plan: 1 to add, 0 to change, 0 to destroy.\n"

func tfFence(s string) string { return "```terraform\n" + s + "```" }

func TestLooksLikePlan(t *testing.T) {
	for _, s := range []string{tfPlan, "Plan: 3 to add, 1 to change, 0 to destroy.", "No changes. Your infrastructure matches the configuration.", "OpenTofu will perform the following actions:"} {
		if !looksLikePlan(s) {
			t.Errorf("deveria reconhecer: %q", s)
		}
	}
	for _, s := range []string{"lgtm", "atlantis plan", "terraform fmt passou"} {
		if looksLikePlan(s) {
			t.Errorf("não deveria reconhecer: %q", s)
		}
	}
}

func TestExtractCommentPlans(t *testing.T) {
	body := "#### Terraform Plan 📖`success`\n<details><summary>Show Plan</summary>\n\n" + tfFence(tfPlan) + "\n\n</details>"
	comments := []ghIssueComment{
		{Body: "lgtm"},
		{Body: "Ran Plan for dir: `x` workspace: `default`\n\n" + tfFence(tfPlan)}, // Atlantis: ignorado aqui
		{Body: body, HTMLURL: "u1", User: struct {
			Login string `json:"login"`
		}{"bot"}},
	}
	got := extractCommentPlans(comments)
	if len(got) != 1 {
		t.Fatalf("fontes = %d", len(got))
	}
	s := got[0]
	if s.Kind != planKindComment || s.Author != "bot" || s.URL != "u1" || len(s.Projects) != 1 {
		t.Fatalf("fonte: %+v", s)
	}
	if s.Projects[0].Summary != "Plan: 1 to add, 0 to change, 0 to destroy." || !strings.Contains(s.Projects[0].Label, "Terraform Plan") {
		t.Errorf("projeto: %+v", s.Projects[0])
	}
	if strings.Contains(s.Projects[0].Content, "```") {
		t.Error("o conteúdo não deve trazer a cerca do bloco")
	}
}

func TestExtractCommentPlansSemBlocoDeCodigo(t *testing.T) {
	got := extractCommentPlans([]ghIssueComment{{Body: "Resultado:\n" + tfPlan}})
	if len(got) != 1 || got[0].Projects[0].Summary == "" {
		t.Errorf("plan em texto puro deveria valer: %+v", got)
	}
}

func TestExtractCommentPlansDeduplicaPorAutorETitulo(t *testing.T) {
	mk := func(url string) ghIssueComment {
		c := ghIssueComment{Body: "#### Terraform Plan\n" + tfFence(tfPlan), HTMLURL: url}
		c.User.Login = "bot"
		return c
	}
	got := extractCommentPlans([]ghIssueComment{mk("antigo"), mk("novo")})
	if len(got) != 1 || got[0].URL != "novo" {
		t.Errorf("deveria ficar só o mais novo: %+v", got)
	}
}

func TestCheckRunSourcesEPlanJobs(t *testing.T) {
	var plan, build, skipped ghCheckRun
	plan.ID, plan.Name, plan.App.Slug = 1, "terraform plan (prod)", "github-actions"
	plan.Conclusion = "success"
	build.ID, build.Name, build.App.Slug = 2, "build", "github-actions"
	skipped.ID, skipped.Name, skipped.App.Slug, skipped.Conclusion = 3, "plan", "github-actions", "skipped"
	var out ghCheckRun
	out.ID, out.Name, out.App.Slug = 4, "spacelift/stack", "spacelift"
	out.Output.Summary = tfFence(tfPlan)

	jobs := planJobs([]ghCheckRun{plan, build, skipped, out})
	if len(jobs) != 1 || jobs[0].ID != 1 {
		t.Errorf("jobs de plan: %+v", jobs)
	}
	srcs := checkRunSources([]ghCheckRun{plan, build, out})
	if len(srcs) != 1 || srcs[0].Kind != planKindCheck || srcs[0].Projects[0].Summary == "" {
		t.Errorf("checks: %+v", srcs)
	}
}

func TestExtractPlansFromLog(t *testing.T) {
	raw := strings.Join([]string{
		"2026-01-01T10:00:00.1234567Z ##[group]Run terraform plan",
		"2026-01-01T10:00:00.1234567Z terraform plan -no-color",
		"2026-01-01T10:00:00.1234567Z ##[endgroup]",
		"2026-01-01T10:00:01.0000000Z aws_s3_bucket.x: Refreshing state... [id=x]",
		"2026-01-01T10:00:02.0000000Z \x1b[1mTerraform used the selected providers to generate the following execution plan.\x1b[0m",
		"2026-01-01T10:00:02.0000000Z   # aws_s3_bucket.x will be created",
		"2026-01-01T10:00:02.0000000Z   + resource \"aws_s3_bucket\" \"x\" {}",
		"2026-01-01T10:00:02.0000000Z ",
		"2026-01-01T10:00:02.0000000Z Plan: 1 to add, 0 to change, 0 to destroy.",
		"2026-01-01T10:00:02.0000000Z ",
		"2026-01-01T10:00:02.0000000Z Changes to Outputs:",
		"2026-01-01T10:00:02.0000000Z   + arn = (known after apply)",
		"2026-01-01T10:00:03.0000000Z ##[group]Run next step",
		"2026-01-01T10:00:03.0000000Z echo done",
	}, "\n")
	got := extractPlansFromLog("plan", logLines(raw))
	if len(got) != 1 {
		t.Fatalf("projetos = %d: %+v", len(got), got)
	}
	p := got[0]
	if p.Label != "plan" || p.Summary != "Plan: 1 to add, 0 to change, 0 to destroy." {
		t.Errorf("projeto: %+v", p)
	}
	if strings.Contains(p.Content, "Refreshing state") || strings.Contains(p.Content, "##[") || strings.Contains(p.Content, "\x1b") {
		t.Errorf("o conteúdo deve ser só o plan, sem metadados nem ANSI:\n%s", p.Content)
	}
	if !strings.HasPrefix(p.Content, "Terraform used the selected providers") || !strings.Contains(p.Content, "arn = (known after apply)") {
		t.Errorf("conteúdo incompleto:\n%s", p.Content)
	}
}

func TestExtractPlansFromLogMultiplosENoChanges(t *testing.T) {
	raw := "No changes. Your infrastructure matches the configuration.\n\nTerraform has compared\n\n##[group]Run x\n" + tfPlan
	got := extractPlansFromLog("tf", logLines(raw))
	if len(got) != 2 || got[0].Label != "tf #1" || got[1].Label != "tf #2" {
		t.Fatalf("projetos: %+v", got)
	}
	if !strings.HasPrefix(got[0].Summary, "No changes.") {
		t.Errorf("resumo: %q", got[0].Summary)
	}
}

func TestExtractPlansFromLogErroEFallback(t *testing.T) {
	errLog := "init ok\n╷\n│ Error: Invalid provider\n│ \n│ details\n╵\n##[error]Process completed with exit code 1."
	got := extractPlansFromLog("plan", logLines(errLog))
	if len(got) != 1 || !got[0].Error || !strings.Contains(got[0].Content, "Invalid provider") {
		t.Errorf("erro: %+v", got)
	}

	other := "##[group]Run helm diff upgrade\nhelm diff\n##[endgroup]\nspec.replicas: 2 -> 3\n"
	got = extractPlansFromLog("helm diff", logLines(other))
	if len(got) != 1 || !strings.Contains(got[0].Content, "spec.replicas: 2 -> 3") || strings.Contains(got[0].Content, "##[") {
		t.Errorf("fallback do fim do log: %+v", got)
	}
}

// Servidor GitHub falso para o fluxo completo.
func fakeGitHub(t *testing.T, mux map[string]func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := mux[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	old := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = old; srv.Close() })
}

func jsonHandler(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

func TestGatherPlanSourcesEAvailability(t *testing.T) {
	jobLog := "2026-01-01T10:00:02Z " + strings.ReplaceAll(tfPlan, "\n", "\n2026-01-01T10:00:02Z ")
	fakeGitHub(t, map[string]func(http.ResponseWriter, *http.Request){
		"/repos/o/r/issues/7/comments": jsonHandler(`[{"body":"lgtm","user":{"login":"dev"}}]`),
		"/repos/o/r/commits/abc/check-runs": jsonHandler(`{"check_runs":[
			{"id":11,"name":"terraform plan","status":"completed","conclusion":"success","html_url":"http://job","app":{"slug":"github-actions"}},
			{"id":12,"name":"lint","status":"completed","conclusion":"success","app":{"slug":"github-actions"}}]}`),
		"/repos/o/r/actions/jobs/11/logs": func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, jobLog) },
	})
	pr := PlanPR{Number: 7, HeadSHA: "abc"}

	sources, warnings, err := gatherPlanSources(context.Background(), "tok", "o", "r", pr)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	if len(sources) != 1 || sources[0].Kind != planKindWorkflow || sources[0].Projects[0].Summary == "" {
		t.Fatalf("fontes: %+v", sources)
	}

	av, err := checkPlanAvailability(context.Background(), "tok", "o", "r", pr)
	if err != nil || !av.Available || av.Jobs != 1 || av.Comments != 0 {
		t.Errorf("disponibilidade: %+v err=%v", av, err)
	}
}

func TestAvailabilityNadaParaMostrar(t *testing.T) {
	fakeGitHub(t, map[string]func(http.ResponseWriter, *http.Request){
		"/repos/o/r/issues/7/comments":      jsonHandler(`[{"body":"atlantis plan","user":{"login":"dev"}}]`),
		"/repos/o/r/commits/abc/check-runs": jsonHandler(`{"check_runs":[{"id":1,"name":"build","app":{"slug":"github-actions"}}]}`),
	})
	av, err := checkPlanAvailability(context.Background(), "tok", "o", "r", PlanPR{Number: 7, HeadSHA: "abc"})
	if err != nil || av.Available || av.Reason != "no_output" {
		t.Errorf("sem plan o botão deve ficar desabilitado: %+v err=%v", av, err)
	}
}

func TestGatherLogIndisponivelViraAviso(t *testing.T) {
	fakeGitHub(t, map[string]func(http.ResponseWriter, *http.Request){
		"/repos/o/r/issues/7/comments":      jsonHandler(`[]`),
		"/repos/o/r/commits/abc/check-runs": jsonHandler(`{"check_runs":[{"id":11,"name":"plan","status":"completed","conclusion":"success","app":{"slug":"github-actions"}}]}`),
		"/repos/o/r/actions/jobs/11/logs":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGone) },
	})
	sources, warnings, err := gatherPlanSources(context.Background(), "tok", "o", "r", PlanPR{Number: 7, HeadSHA: "abc"})
	if err != nil || len(sources) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "expirados") {
		t.Errorf("sources=%v warnings=%v err=%v", sources, warnings, err)
	}
}

func TestAvailabilityChecksIndisponiveisUsaComentarios(t *testing.T) {
	body := "Ran Plan for dir: `x` workspace: `default`\n\n<details><summary>Show Output</summary>\n\n```diff\n" + tfPlan + "```\n</details>"
	b, _ := jsonMarshalString([]map[string]any{{"body": body, "user": map[string]string{"login": "atlantis"}}})
	fakeGitHub(t, map[string]func(http.ResponseWriter, *http.Request){
		"/repos/o/r/issues/7/comments": jsonHandler(b),
		// check-runs ausente → 404: a disponibilidade segue só com os comentários
	})
	av, err := checkPlanAvailability(context.Background(), "tok", "o", "r", PlanPR{Number: 7, HeadSHA: "abc"})
	if err != nil || !av.Available || av.Comments != 1 {
		t.Errorf("disponibilidade: %+v err=%v", av, err)
	}
}

func jsonMarshalString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
