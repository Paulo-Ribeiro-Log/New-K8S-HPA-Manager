package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// View Plan — lê (somente leitura) os plans/outputs que o GitHub guarda para o PR da branch atual
// do repo. As fontes (code_editor_plan_sources.go) são: comentários do Atlantis, comentários de
// outros bots/workflows com saída de plan, saídas de check runs e logs dos jobs do GitHub Actions
// que rodam plan/diff/preview.
//
// O Atlantis publica cada projeto num bloco <details><summary>Show Output</summary> com um ```diff
// dentro; plans maiores que o limite de comentário do GitHub são quebrados em vários comentários
// seguidos ("Continued plan output from previous comment."), que são reemendados aqui para o texto
// voltar a ficar inteiro.

// githubAPIBase é trocável nos testes.
var githubAPIBase = "https://api.github.com"

// PlanPR é um PR da branch atual.
type PlanPR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Base      string `json:"base"`
	State     string `json:"state"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
	HeadSHA   string `json:"head_sha"`
}

// PlanProject é a saída de plan de um projeto/dir do Atlantis.
type PlanProject struct {
	Label   string `json:"label"`
	Content string `json:"content"`
	Summary string `json:"summary"`
	Error   bool   `json:"error"`
}

// AtlantisPlan é o último plan comentado no PR.
type AtlantisPlan struct {
	CommentURL string        `json:"comment_url"`
	Author     string        `json:"author"`
	CreatedAt  string        `json:"created_at"`
	Comments   int           `json:"comments"` // > 1 quando o plan foi dividido em vários comentários
	Projects   []PlanProject `json:"projects"`
}

type ghIssueComment struct {
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

var (
	planHeadingRe      = regexp.MustCompile(`(?m)^### \d+\.\s+(.*)$`)
	planFenceOpenRe    = regexp.MustCompile("(?m)^```[A-Za-z0-9_-]*[ \t]*\n")
	planSummaryRe      = regexp.MustCompile(`(?m)^.*(Plan: \d+ to .*|No changes\..*)$`)
	planContinuationRe = regexp.MustCompile(`(?i)^continued plan output from previous comment`)
)

// planTarget identifica o repo/branch/token usados nas chamadas ao GitHub.
type planTarget struct {
	owner, repo, branch, token string
}

// repoAccess é o que as rotas de PR precisam do repositório local: owner/repo e o PAT.
type repoAccess struct {
	owner, repo, token string
}

// resolveRepoAccess valida o repo e acha owner/repo e o PAT. Em falha, já responde ao cliente e
// devolve ok=false.
func (h *CodeEditorHandler) resolveRepoAccess(c *gin.Context) (repoAccess, bool) {
	dir := filepath.Join(h.reposBase, c.Param("id"))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repositório não encontrado"})
		return repoAccess{}, false
	}
	owner, repo := ownerRepo(dir)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "não foi possível determinar owner/repo do repositório"})
		return repoAccess{}, false
	}
	token := h.resolveProfileToken(c, c.Query("profile_id"))
	if token == "" {
		token = h.resolveActiveProfileToken(c)
	}
	if token == "" {
		token = h.getToken(c)
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "PAT GitHub não configurado — configure em GitHub Releases → perfil"})
		return repoAccess{}, false
	}
	return repoAccess{owner: owner, repo: repo, token: token}, true
}

var prBranchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

// validPRBranch aceita só nomes de branch comuns (letras, números, . _ - /): o nome vai para a
// consulta ao GitHub e nunca para um comando.
func validPRBranch(b string) bool {
	return prBranchRe.MatchString(b) && !strings.Contains(b, "..") && !strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".lock")
}

// resolvePlanTarget acha repo, branch e PAT. A branch é a pedida em ?branch= (qualquer branch com PR,
// sem precisar de checkout) ou, sem ela, a branch atual do repositório local. Em falha, já responde
// ao cliente e devolve ok=false.
func (h *CodeEditorHandler) resolvePlanTarget(c *gin.Context) (planTarget, bool) {
	acc, ok := h.resolveRepoAccess(c)
	if !ok {
		return planTarget{}, false
	}
	branch := strings.TrimSpace(c.Query("branch"))
	if branch != "" {
		if !validPRBranch(branch) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "nome de branch inválido"})
			return planTarget{}, false
		}
	} else {
		branch = currentBranch(filepath.Join(h.reposBase, c.Param("id")))
		if branch == "" || branch == "HEAD" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "repositório não está numa branch (HEAD destacado)"})
			return planTarget{}, false
		}
	}
	return planTarget{owner: acc.owner, repo: acc.repo, branch: branch, token: acc.token}, true
}

// pickPR escolhe o PR pedido (?pr=<n>) ou, sem ele, o primeiro da lista (aberto mais recente).
func pickPR(prs []PlanPR, requested string) PlanPR {
	selected := prs[0]
	if n, _ := strconv.Atoi(requested); n > 0 {
		for _, p := range prs {
			if p.Number == n {
				selected = p
			}
		}
	}
	return selected
}

// GetPRPlan — GET /api/v1/code-editor/repos/:id/pr/plan?pr=<n>&profile_id=<id>
// Sem `pr`, usa o PR aberto mais recente da branch atual (ou o mais recente fechado/mergeado).
// Devolve todas as fontes de plan/output achadas no GitHub para o PR.
func (h *CodeEditorHandler) GetPRPlan(c *gin.Context) {
	t, ok := h.resolvePlanTarget(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	prs, err := listBranchPRs(ctx, t.token, t.owner, t.repo, t.branch)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"branch": t.branch, "prs": prs, "pr": nil, "sources": []PlanSource{}, "warnings": []string{}}
	if len(prs) == 0 {
		c.JSON(http.StatusOK, resp)
		return
	}

	selected := pickPR(prs, c.Query("pr"))
	resp["pr"] = selected

	sources, warnings, err := gatherPlanSources(ctx, t.token, t.owner, t.repo, selected)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	resp["sources"] = sources
	resp["warnings"] = warnings
	c.JSON(http.StatusOK, resp)
}

// GetPRPlanAvailability — GET /api/v1/code-editor/repos/:id/pr/plan/available?profile_id=<id>
// Resposta barata (PRs, comentários e check runs; sem baixar logs) usada para habilitar ou
// desabilitar o botão View Plan.
func (h *CodeEditorHandler) GetPRPlanAvailability(c *gin.Context) {
	t, ok := h.resolvePlanTarget(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	prs, err := listBranchPRs(ctx, t.token, t.owner, t.repo, t.branch)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if len(prs) == 0 {
		c.JSON(http.StatusOK, PlanAvailability{Branch: t.branch, Reason: "no_pr"})
		return
	}
	av, err := checkPlanAvailability(ctx, t.token, t.owner, t.repo, prs[0])
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	av.Branch = t.branch
	c.JSON(http.StatusOK, av)
}

// listBranchPRs lista os PRs cuja head é a branch, abertos primeiro e depois por atualização.
func listBranchPRs(ctx context.Context, token, owner, repo, branch string) ([]PlanPR, error) {
	q := url.Values{}
	q.Set("head", owner+":"+branch)
	q.Set("state", "all")
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	q.Set("per_page", "30")
	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		HTMLURL   string `json:"html_url"`
		UpdatedAt string `json:"updated_at"`
		MergedAt  string `json:"merged_at"`
		Base      struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := githubGetJSON(ctx, token, fmt.Sprintf("%s/repos/%s/%s/pulls?%s", githubAPIBase, owner, repo, q.Encode()), &raw); err != nil {
		return nil, err
	}
	var open, other []PlanPR
	for _, r := range raw {
		p := PlanPR{Number: r.Number, Title: r.Title, Base: r.Base.Ref, State: r.State, URL: r.HTMLURL, UpdatedAt: r.UpdatedAt, HeadSHA: r.Head.SHA}
		if r.State == "open" {
			open = append(open, p)
			continue
		}
		if r.MergedAt != "" {
			p.State = "merged"
		}
		other = append(other, p)
	}
	return append(open, other...), nil
}

// listIssueComments busca todos os comentários do PR (conversation), paginando.
func listIssueComments(ctx context.Context, token, owner, repo string, number int) ([]ghIssueComment, error) {
	var all []ghIssueComment
	for page := 1; page <= 30; page++ {
		var batch []ghIssueComment
		u := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", githubAPIBase, owner, repo, number, page)
		if err := githubGetJSON(ctx, token, u, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func githubGetJSON(ctx context.Context, token, apiURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao contatar GitHub API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &ghErr)
		if ghErr.Message == "" {
			ghErr.Message = fmt.Sprintf("GitHub API retornou %d", resp.StatusCode)
		}
		return fmt.Errorf("GitHub: %s", ghErr.Message)
	}
	return json.Unmarshal(body, out)
}

// extractAtlantisPlan acha o último comentário "Ran Plan for ..." (mais as continuações
// que vêm logo depois dele) e devolve a saída de cada projeto. nil se não houver plan.
func extractAtlantisPlan(comments []ghIssueComment) *AtlantisPlan {
	start := -1
	for i := len(comments) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(comments[i].Body), "Ran Plan for") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}

	first := comments[start]
	plan := &AtlantisPlan{CommentURL: first.HTMLURL, Author: first.User.Login, CreatedAt: first.CreatedAt, Comments: 1}
	plan.Projects = parsePlanSections(normalizeNewlines(first.Body))

	for _, cm := range comments[start+1:] {
		body := strings.TrimSpace(normalizeNewlines(cm.Body))
		if !planContinuationRe.MatchString(body) {
			break
		}
		plan.Comments++
		// O primeiro bloco de código da continuação é o resto do último projeto, cortado
		// exatamente no ponto da divisão — concatena sem separador para reconstituir a linha.
		content, rest, ok := firstFence(body)
		if ok && len(plan.Projects) > 0 {
			plan.Projects[len(plan.Projects)-1].Content += content
		}
		plan.Projects = append(plan.Projects, parsePlanSections(rest)...)
	}

	for i := range plan.Projects {
		p := &plan.Projects[i]
		if m := planSummaryRe.FindAllStringSubmatch(p.Content, -1); len(m) > 0 {
			p.Summary = strings.TrimSpace(m[len(m)-1][1])
		}
	}
	return plan
}

// parsePlanSections quebra um comentário do Atlantis em projetos: com vários projetos
// cada um vem sob "### N. dir: `x` workspace: `y`"; com um só, o rótulo está na 1ª linha.
func parsePlanSections(body string) []PlanProject {
	headings := planHeadingRe.FindAllStringSubmatchIndex(body, -1)
	if len(headings) == 0 {
		content, _, ok := firstFence(body)
		if !ok {
			return nil
		}
		label := strings.TrimSpace(strings.SplitN(strings.TrimSpace(body), "\n", 2)[0])
		label = strings.TrimSuffix(strings.TrimPrefix(label, "Ran Plan for "), ":")
		return []PlanProject{newPlanProject(label, body, content)}
	}

	var out []PlanProject
	for i, hd := range headings {
		end := len(body)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		section := body[hd[1]:end]
		content, _, ok := firstFence(section)
		if !ok {
			continue
		}
		out = append(out, newPlanProject(body[hd[2]:hd[3]], section, content))
	}
	return out
}

func newPlanProject(label, section, content string) PlanProject {
	return PlanProject{
		Label:   strings.TrimSpace(strings.ReplaceAll(label, "`", "")),
		Content: content,
		Error:   strings.Contains(section, "Plan Error") || strings.Contains(section, "Plan Failed"),
	}
}

// firstFence devolve o conteúdo do primeiro bloco ``` (sem a linha de abertura e sem o "\n"
// antes do fechamento) e o texto depois dele. Bloco sem fechamento vai até o fim.
func firstFence(s string) (content, rest string, ok bool) {
	loc := planFenceOpenRe.FindStringIndex(s)
	if loc == nil {
		return "", s, false
	}
	after := s[loc[1]:]
	for off := 0; ; {
		idx := strings.Index(after[off:], "\n```")
		if idx < 0 {
			return after, "", true
		}
		pos := off + idx
		tail := after[pos+4:]
		if tail == "" || tail[0] == '\n' || tail[0] == ' ' || tail[0] == '\t' {
			return after[:pos], tail, true
		}
		off = pos + 4
	}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
