package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Fontes de plan/output do View Plan. Atlantis é só uma delas: qualquer coisa que o GitHub
// guarde para o PR e pareça saída de plan (Terraform/OpenTofu) ou de um job de plan/diff/preview
// entra aqui.
//
//	atlantis — comentário "Ran Plan for ..." (code_editor_plan.go)
//	comment  — comentário de outro bot/workflow com saída de plan num bloco de código
//	check    — saída (summary/text) de um check run do commit do PR
//	workflow — log de um job do GitHub Actions cujo nome indica plan/diff/preview
const (
	planKindAtlantis = "atlantis"
	planKindComment  = "comment"
	planKindCheck    = "check"
	planKindWorkflow = "workflow"

	maxCommentSources = 5
	maxWorkflowJobs   = 6
	maxJobLogBytes    = 6 << 20
	maxFallbackLines  = 300
)

// PlanSource é um conjunto de plans de uma mesma origem.
type PlanSource struct {
	Kind      string        `json:"kind"`
	Label     string        `json:"label"`
	URL       string        `json:"url"`
	Author    string        `json:"author"`
	CreatedAt string        `json:"created_at"`
	Comments  int           `json:"comments"` // > 1 quando o plan do Atlantis veio dividido em vários comentários
	Projects  []PlanProject `json:"projects"`
}

// PlanAvailability diz se há algo para mostrar no View Plan do PR da branch.
type PlanAvailability struct {
	Available bool    `json:"available"`
	Branch    string  `json:"branch"`
	PR        *PlanPR `json:"pr"`
	// Reason quando !Available: "no_pr" ou "no_output".
	Reason   string `json:"reason,omitempty"`
	Comments int    `json:"comments"` // comentários com plan (Atlantis e outros)
	Checks   int    `json:"checks"`   // check runs com saída de plan
	Jobs     int    `json:"jobs"`     // jobs do Actions de plan/diff/preview
}

type ghCheckRun struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	HTMLURL     string `json:"html_url"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	App         struct {
		Slug string `json:"slug"`
	} `json:"app"`
	Output struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	} `json:"output"`
}

var (
	// Marcadores de saída de plan do Terraform/OpenTofu.
	planMarkerRe = regexp.MustCompile(`(?i)(terraform|opentofu|tofu) (will perform the following actions|used the selected providers)` +
		`|(?m)^\s*Plan: \d+ to add, \d+ to change, \d+ to destroy` +
		`|No changes\. Your infrastructure matches` +
		`|(?m)^\s*Changes to Outputs:`)
	planStartRe   = regexp.MustCompile(`(?i)(terraform|opentofu|tofu) (used the selected providers|will perform the following actions)|^No changes\. Your infrastructure matches`)
	planEndRe     = regexp.MustCompile(`^(Plan: \d+ to add|No changes\.)`)
	planErrorRe   = regexp.MustCompile(`(?m)^(│ )?Error: `)
	planJobNameRe = regexp.MustCompile(`(?i)(^|[^a-z])(plan|diff|preview)([^a-z]|$)`)
	logStampRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[0-9:.]+Z ?`)
	ansiRe        = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	htmlTagRe     = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
)

func looksLikePlan(s string) bool { return planMarkerRe.MatchString(s) }

// ─── Montagem das fontes ────────────────────────────────────────────────────

// gatherPlanSources reúne todas as fontes do PR. Falha de comentários é erro; falha de check
// runs ou de logs vira aviso (a tela mostra o que conseguiu).
func gatherPlanSources(ctx context.Context, token, owner, repo string, pr PlanPR) ([]PlanSource, []string, error) {
	comments, err := listIssueComments(ctx, token, owner, repo, pr.Number)
	if err != nil {
		return nil, nil, err
	}
	warnings := []string{}
	var checks []ghCheckRun
	if pr.HeadSHA != "" {
		if checks, err = listCheckRuns(ctx, token, owner, repo, pr.HeadSHA); err != nil {
			warnings = append(warnings, "Check runs indisponíveis: "+err.Error())
		}
	}

	var sources []PlanSource
	if atl := extractAtlantisPlan(comments); atl != nil {
		sources = append(sources, PlanSource{
			Kind: planKindAtlantis, Label: "Atlantis", URL: atl.CommentURL, Author: atl.Author,
			CreatedAt: atl.CreatedAt, Comments: atl.Comments, Projects: atl.Projects,
		})
	}
	sources = append(sources, extractCommentPlans(comments)...)

	wf, wfWarn := workflowSources(ctx, token, owner, repo, planJobs(checks))
	sources = append(sources, wf...)
	warnings = append(warnings, wfWarn...)
	sources = append(sources, checkRunSources(checks)...)
	sortPlanSources(sources)
	return sources, warnings, nil
}

// checkPlanAvailability conta o que existe sem baixar logs: comentários com plan, check runs
// com saída de plan e jobs de plan/diff/preview do Actions.
func checkPlanAvailability(ctx context.Context, token, owner, repo string, pr PlanPR) (PlanAvailability, error) {
	av := PlanAvailability{PR: &pr}
	comments, err := listIssueComments(ctx, token, owner, repo, pr.Number)
	if err != nil {
		return av, err
	}
	if extractAtlantisPlan(comments) != nil {
		av.Comments++
	}
	av.Comments += len(extractCommentPlans(comments))
	if pr.HeadSHA != "" {
		// Sem check runs (permissão, falha): segue só com os comentários.
		if checks, err := listCheckRuns(ctx, token, owner, repo, pr.HeadSHA); err == nil {
			av.Checks = len(checkRunSources(checks))
			av.Jobs = len(planJobs(checks))
		}
	}
	av.Available = av.Comments+av.Checks+av.Jobs > 0
	if !av.Available {
		av.Reason = "no_output"
	}
	return av, nil
}

// ─── Comentários genéricos ──────────────────────────────────────────────────

// extractCommentPlans acha comentários (que não são do Atlantis) com saída de plan em bloco de
// código, do mais novo para o mais antigo, um por autor+título.
func extractCommentPlans(comments []ghIssueComment) []PlanSource {
	var out []PlanSource
	seen := map[string]bool{}
	for i := len(comments) - 1; i >= 0 && len(out) < maxCommentSources; i-- {
		cm := comments[i]
		body := strings.TrimSpace(normalizeNewlines(cm.Body))
		if strings.HasPrefix(body, "Ran Plan for") || planContinuationRe.MatchString(body) {
			continue // já tratado como Atlantis
		}
		title := commentTitle(body)
		projects := commentPlanProjects(body, title)
		if len(projects) == 0 {
			continue
		}
		k := cm.User.Login + "|" + title
		if seen[k] {
			continue
		}
		seen[k] = true
		label := "Comentário de " + cm.User.Login
		if title != "" {
			label += " — " + title
		}
		out = append(out, PlanSource{
			Kind: planKindComment, Label: label, URL: cm.HTMLURL, Author: cm.User.Login,
			CreatedAt: cm.CreatedAt, Comments: 1, Projects: projects,
		})
	}
	return out
}

// commentPlanProjects devolve um projeto por bloco de código que pareça plan; sem nenhum bloco de
// código, o corpo inteiro vale se tiver marcador de plan.
func commentPlanProjects(body, title string) []PlanProject {
	var projects []PlanProject
	hasFence := false
	for rest := body; ; {
		content, after, ok := firstFence(rest)
		if !ok {
			break
		}
		hasFence = true
		if looksLikePlan(content) {
			projects = append(projects, PlanProject{Content: content})
		}
		rest = after
	}
	if len(projects) == 0 && !hasFence && looksLikePlan(body) {
		projects = append(projects, PlanProject{Content: body})
	}
	for i := range projects {
		projects[i].Label = title
		if len(projects) > 1 {
			projects[i].Label = fmt.Sprintf("%s (bloco %d)", title, i+1)
		}
		if projects[i].Label == "" {
			projects[i].Label = "plan"
		}
		fillPlanSummary(&projects[i])
	}
	return projects
}

// commentTitle é a primeira linha útil do comentário, sem markdown/HTML, curta.
func commentTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(htmlTagRe.ReplaceAllString(line, ""))
		t = strings.TrimSpace(strings.Trim(strings.TrimLeft(t, "# "), "`"))
		if t == "" || strings.HasPrefix(t, "```") {
			continue
		}
		if r := []rune(t); len(r) > 80 {
			t = string(r[:80]) + "…"
		}
		return t
	}
	return ""
}

// fillPlanSummary preenche o resumo ("Plan: ...") e marca erro quando há "Error:" sem resumo.
func fillPlanSummary(p *PlanProject) {
	if m := planSummaryRe.FindAllStringSubmatch(p.Content, -1); len(m) > 0 {
		p.Summary = strings.TrimSpace(m[len(m)-1][1])
		return
	}
	p.Error = planErrorRe.MatchString(p.Content)
}

// ─── Check runs ─────────────────────────────────────────────────────────────

func listCheckRuns(ctx context.Context, token, owner, repo, sha string) ([]ghCheckRun, error) {
	var all []ghCheckRun
	for page := 1; page <= 3; page++ {
		var resp struct {
			CheckRuns []ghCheckRun `json:"check_runs"`
		}
		u := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs?per_page=100&filter=latest&page=%d", githubAPIBase, owner, repo, sha, page)
		if err := githubGetJSON(ctx, token, u, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.CheckRuns...)
		if len(resp.CheckRuns) < 100 {
			break
		}
	}
	return all, nil
}

// checkRunSources: check runs cuja saída (summary/text) tem plan.
func checkRunSources(checks []ghCheckRun) []PlanSource {
	var out []PlanSource
	for _, c := range checks {
		text := c.Output.Summary + "\n" + c.Output.Text
		if !looksLikePlan(text) {
			continue
		}
		content := strings.TrimSpace(text)
		if inner, _, ok := firstFence(content); ok && looksLikePlan(inner) {
			content = inner
		}
		p := PlanProject{Label: c.Name, Content: content}
		fillPlanSummary(&p)
		at := c.CompletedAt
		if at == "" {
			at = c.StartedAt
		}
		out = append(out, PlanSource{
			Kind: planKindCheck, Label: "Check: " + c.Name, URL: c.HTMLURL, Author: c.App.Slug,
			CreatedAt: at, Comments: 1, Projects: []PlanProject{p},
		})
	}
	return out
}

// ─── Jobs do GitHub Actions ─────────────────────────────────────────────────

// planJobs: jobs do Actions (no GitHub Actions, o ID do check run é o ID do job) com nome de
// plan/diff/preview que rodaram (pulado ou cancelado não tem saída útil).
func planJobs(checks []ghCheckRun) []ghCheckRun {
	var out []ghCheckRun
	for _, c := range checks {
		if c.App.Slug != "github-actions" || !planJobNameRe.MatchString(c.Name) {
			continue
		}
		if c.Conclusion == "skipped" || c.Conclusion == "cancelled" || c.Status == "queued" {
			continue
		}
		out = append(out, c)
		if len(out) == maxWorkflowJobs {
			break
		}
	}
	return out
}

// workflowSources baixa o log de cada job (em paralelo) e extrai os plans. Log indisponível
// (expirado, sem permissão actions:read) vira aviso.
func workflowSources(ctx context.Context, token, owner, repo string, jobs []ghCheckRun) ([]PlanSource, []string) {
	type result struct {
		src  *PlanSource
		warn string
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job ghCheckRun) {
			defer wg.Done()
			raw, err := fetchJobLog(ctx, token, owner, repo, job.ID)
			if err != nil {
				results[i].warn = fmt.Sprintf("Log do job %q indisponível: %s", job.Name, err.Error())
				return
			}
			projects := extractPlansFromLog(job.Name, logLines(raw))
			if len(projects) == 0 {
				return
			}
			at := job.CompletedAt
			if at == "" {
				at = job.StartedAt
			}
			results[i].src = &PlanSource{
				Kind: planKindWorkflow, Label: "Actions: " + job.Name, URL: job.HTMLURL,
				Author: "GitHub Actions", CreatedAt: at, Comments: 1, Projects: projects,
			}
		}(i, job)
	}
	wg.Wait()

	var out []PlanSource
	var warns []string
	for _, r := range results {
		if r.src != nil {
			out = append(out, *r.src)
		}
		if r.warn != "" {
			warns = append(warns, r.warn)
		}
	}
	return out, warns
}

func fetchJobLog(ctx context.Context, token, owner, repo string, jobID int64) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", githubAPIBase, owner, repo, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// O GitHub responde 302 para uma URL assinada de outro domínio; o Go não repassa o
	// Authorization nesse redirecionamento.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("falha ao contatar GitHub API: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusGone, http.StatusNotFound:
		return "", fmt.Errorf("logs expirados ou removidos")
	case http.StatusForbidden, http.StatusUnauthorized:
		return "", fmt.Errorf("o PAT não tem permissão de leitura do Actions (actions:read)")
	default:
		return "", fmt.Errorf("GitHub API retornou %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJobLogBytes))
	return string(body), err
}

// logLines remove timestamps e cores ANSI do log do Actions.
func logLines(raw string) []string {
	lines := strings.Split(normalizeNewlines(raw), "\n")
	for i, l := range lines {
		l = logStampRe.ReplaceAllString(l, "")
		lines[i] = strings.TrimRight(ansiRe.ReplaceAllString(l, ""), " \t\r")
	}
	return lines
}

func isLogMeta(l string) bool {
	return strings.HasPrefix(l, "##[") || strings.HasPrefix(l, "Post job cleanup")
}

// extractPlansFromLog acha as saídas de plan de um log de job: do cabeçalho do plan até o
// resumo ("Plan: ..." / "No changes.") mais o bloco "Changes to Outputs". Sem plan, tenta o
// primeiro erro do Terraform; sem isso, devolve o fim do log (job de diff/preview de outra
// ferramenta).
func extractPlansFromLog(job string, lines []string) []PlanProject {
	var projects []PlanProject
	for i := 0; i < len(lines); i++ {
		if !planStartRe.MatchString(lines[i]) {
			continue
		}
		end := i
		for k := i; k < len(lines); k++ {
			if strings.HasPrefix(lines[k], "##[group]") || strings.HasPrefix(lines[k], "Post job cleanup") {
				end = k - 1
				break
			}
			end = k
			if planEndRe.MatchString(lines[k]) {
				break
			}
		}
		if strings.HasPrefix(lines[end], "No changes.") {
			for n := 0; n < 3 && end+1 < len(lines) && lines[end+1] != "" && !isLogMeta(lines[end+1]); n++ {
				end++
			}
		} else {
			for n := 0; n < 300 && end+1 < len(lines); n++ {
				next := lines[end+1]
				if isLogMeta(next) || !(next == "" || next[0] == ' ' || next[0] == '\t' || strings.HasPrefix(next, "Changes to Outputs:")) {
					break
				}
				end++
			}
		}
		projects = append(projects, PlanProject{Content: strings.TrimRight(strings.Join(lines[i:end+1], "\n"), "\n ")})
		i = end
	}

	if len(projects) == 0 {
		if p, ok := firstLogError(lines); ok {
			projects = append(projects, p)
		}
	}
	if len(projects) == 0 {
		if tail := logTail(lines); tail != "" {
			projects = append(projects, PlanProject{Content: tail})
		}
	}
	for i := range projects {
		projects[i].Label = job
		if len(projects) > 1 {
			projects[i].Label = fmt.Sprintf("%s #%d", job, i+1)
		}
		fillPlanSummary(&projects[i])
	}
	return projects
}

// firstLogError devolve o primeiro "Error:" do Terraform com o contexto que o segue.
func firstLogError(lines []string) (PlanProject, bool) {
	for i, l := range lines {
		if !planErrorRe.MatchString(l) {
			continue
		}
		start := i
		if start > 0 && strings.HasPrefix(lines[start-1], "╷") {
			start--
		}
		end := i
		for end+1 < len(lines) && end-i < 60 && !isLogMeta(lines[end+1]) {
			end++
			if strings.HasPrefix(lines[end], "╵") {
				break
			}
		}
		return PlanProject{Content: strings.Join(lines[start:end+1], "\n"), Error: true}, true
	}
	return PlanProject{}, false
}

// logTail devolve as últimas linhas úteis do log (sem metadados do runner).
func logTail(lines []string) string {
	var kept []string
	for _, l := range lines {
		if l == "" || isLogMeta(l) {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) > maxFallbackLines {
		kept = kept[len(kept)-maxFallbackLines:]
	}
	return strings.Join(kept, "\n")
}

// sortPlanSources põe Atlantis, comentários, Actions e checks nessa ordem (estável).
func sortPlanSources(s []PlanSource) {
	rank := map[string]int{planKindAtlantis: 0, planKindComment: 1, planKindWorkflow: 2, planKindCheck: 3}
	sort.SliceStable(s, func(i, j int) bool { return rank[s[i].Kind] < rank[s[j].Kind] })
}
