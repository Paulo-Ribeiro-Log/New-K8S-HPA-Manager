package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// PRs abertos (criados e ainda não mergeados) do repositório, por branch — usados pelo painel de
// branches para mostrar o Diff e o Plan de qualquer branch com PR, sem precisar de checkout.

// OpenPR é um PR aberto cuja branch de origem está no próprio repositório.
type OpenPR struct {
	Branch    string `json:"branch"`
	State     string `json:"state"` // open | closed | merged
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Base      string `json:"base"`
	Draft     bool   `json:"draft"`
	Author    string `json:"author,omitempty"`
	UpdatedAt string `json:"updated_at"`
	// Status no GitHub (vazio quando não deu para consultar): checks = success|failure|pending|none;
	// review = approved|changes_requested|review_required|none; mergeable = ok|conflict|unknown.
	Checks    string `json:"checks,omitempty"`
	Review    string `json:"review,omitempty"`
	Mergeable string `json:"mergeable,omitempty"`
}

const openPRsMaxPages = 5  // até 500 PRs abertos
const closedPRsLimit = 100 // só os 100 fechados mexidos há menos tempo: o selo cinza é para branches ainda existentes

// listPRs lista PRs no estado pedido (open|closed), paginando até maxPages. PRs de forks ficam de fora:
// a branch deles não existe neste repositório, e o nome poderia colidir com uma branch daqui.
func listPRs(ctx context.Context, token, owner, repo, state string, maxPages int) ([]OpenPR, error) {
	var out []OpenPR
	full := owner + "/" + repo
	for page := 1; page <= maxPages; page++ {
		var batch []struct {
			Number    int    `json:"number"`
			Title     string `json:"title"`
			HTMLURL   string `json:"html_url"`
			Draft     bool   `json:"draft"`
			UpdatedAt string `json:"updated_at"`
			MergedAt  string `json:"merged_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Head struct {
				Ref  string `json:"ref"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
		}
		u := fmt.Sprintf("%s/repos/%s/%s/pulls?state=%s&sort=updated&direction=desc&per_page=100&page=%d", githubAPIBase, owner, repo, state, page)
		if err := githubGetJSON(ctx, token, u, &batch); err != nil {
			return nil, err
		}
		for _, p := range batch {
			if p.Head.Repo == nil || !strings.EqualFold(p.Head.Repo.FullName, full) {
				continue // fork (ou repositório de origem apagado)
			}
			st := state
			if p.MergedAt != "" {
				st = "merged"
			}
			out = append(out, OpenPR{Branch: p.Head.Ref, State: st, Number: p.Number, Title: p.Title, URL: p.HTMLURL,
				Base: p.Base.Ref, Draft: p.Draft, Author: p.User.Login, UpdatedAt: p.UpdatedAt})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// listOpenPRs lista os PRs abertos.
func listOpenPRs(ctx context.Context, token, owner, repo string) ([]OpenPR, error) {
	return listPRs(ctx, token, owner, repo, "open", openPRsMaxPages)
}

// listClosedPRs lista os PRs fechados (inclui os mergeados, com State "merged") mais recentes.
func listClosedPRs(ctx context.Context, token, owner, repo string) ([]OpenPR, error) {
	prs, err := listPRs(ctx, token, owner, repo, "closed", 1)
	if len(prs) > closedPRsLimit {
		prs = prs[:closedPRsLimit]
	}
	return prs, err
}

// ListOpenPRs — GET /api/v1/code-editor/repos/:id/pr/open?profile_id=<id>
// Só leitura. Devolve os PRs abertos do repositório (depois os fechados/mergeados mais recentes),
// com a branch de origem e o state de cada um.
func (h *CodeEditorHandler) ListOpenPRs(c *gin.Context) {
	acc, ok := h.resolveRepoAccess(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	prs, err := listOpenPRs(ctx, acc.token, acc.owner, acc.repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	enrichOpenPRs(ctx, acc.token, acc.owner, acc.repo, prs) // melhor esforço: sem status, o selo segue só com o número
	// Fechados/mergeados vêm depois dos abertos (o cliente fica com o primeiro de cada branch). Melhor
	// esforço: se falhar, os abertos continuam valendo.
	if closed, cerr := listClosedPRs(ctx, acc.token, acc.owner, acc.repo); cerr == nil {
		prs = append(prs, closed...)
	}
	if prs == nil {
		prs = []OpenPR{}
	}
	c.JSON(http.StatusOK, gin.H{"prs": prs})
}

const openPRStatusQuery = `query($o:String!,$r:String!){repository(owner:$o,name:$r){pullRequests(states:OPEN,first:100,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{number mergeable reviewDecision commits(last:1){nodes{commit{statusCheckRollup{state}}}}}}}}`

// enrichOpenPRs preenche checks, revisão e mergeable de cada PR com UMA consulta GraphQL (só leitura).
// Qualquer falha (token sem acesso ao GraphQL, limite) deixa os campos vazios: o selo continua valendo.
func enrichOpenPRs(ctx context.Context, token, owner, repo string, prs []OpenPR) {
	var resp struct {
		Data struct {
			Repository struct {
				PullRequests struct {
					Nodes []struct {
						Number         int    `json:"number"`
						Mergeable      string `json:"mergeable"`
						ReviewDecision string `json:"reviewDecision"`
						Commits        struct {
							Nodes []struct {
								Commit struct {
									StatusCheckRollup *struct {
										State string `json:"state"`
									} `json:"statusCheckRollup"`
								} `json:"commit"`
							} `json:"nodes"`
						} `json:"commits"`
					} `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
	}
	payload := map[string]any{"query": openPRStatusQuery, "variables": map[string]string{"o": owner, "r": repo}}
	if err := githubPostJSON(ctx, token, githubAPIBase+"/graphql", payload, &resp); err != nil {
		return
	}
	byNum := map[int]int{}
	for i := range prs {
		byNum[prs[i].Number] = i
	}
	for _, n := range resp.Data.Repository.PullRequests.Nodes {
		i, ok := byNum[n.Number]
		if !ok {
			continue
		}
		prs[i].Checks = "none"
		if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			prs[i].Checks = checksFromRollup(n.Commits.Nodes[0].Commit.StatusCheckRollup.State)
		}
		prs[i].Review = strings.ToLower(n.ReviewDecision)
		if prs[i].Review == "" {
			prs[i].Review = "none"
		}
		switch n.Mergeable {
		case "MERGEABLE":
			prs[i].Mergeable = "ok"
		case "CONFLICTING":
			prs[i].Mergeable = "conflict"
		default:
			prs[i].Mergeable = "unknown"
		}
	}
}

func checksFromRollup(state string) string {
	switch state {
	case "SUCCESS":
		return "success"
	case "FAILURE", "ERROR":
		return "failure"
	case "PENDING", "EXPECTED":
		return "pending"
	}
	return "none"
}

type setPRStateRequest struct {
	PR    int    `json:"pr"`
	State string `json:"state"` // closed | open
}

// SetPRState — POST /api/v1/code-editor/repos/:id/pr/state?profile_id=<id>
// Body: { "pr", "state": "closed"|"open" }. Fecha ou reabre o PR no GitHub, em nome do PAT do perfil.
// PR já mergeado não reabre.
func (h *CodeEditorHandler) SetPRState(c *gin.Context) {
	var req setPRStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PR <= 0 || (req.State != "closed" && req.State != "open") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr é obrigatório e state deve ser closed ou open"})
		return
	}
	acc, ok := h.resolveRepoAccess(c)
	if !ok {
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	resource := fmt.Sprintf("%s/%s#%d", acc.owner, acc.repo, req.PR)
	after := map[string]interface{}{"repo_id": c.Param("id"), "number": req.PR, "state": req.State}
	fail := func(status int, msg string) {
		logOperation(c, h.historyTracker, "code_editor_pr_state", resource, "", nil, after, start, fmt.Errorf("%s", msg))
		c.JSON(status, gin.H{"error": msg})
	}

	pr, err := getPRDetail(ctx, acc.token, acc.owner, acc.repo, req.PR)
	if err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}
	cur := prState(pr)
	switch {
	case cur == "merged":
		fail(http.StatusConflict, "o PR já foi mergeado: não dá para fechar nem reabrir")
		return
	case cur == req.State:
		c.JSON(http.StatusOK, gin.H{"state": cur}) // já está como pedido
		return
	}
	var out struct {
		State string `json:"state"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBase, acc.owner, acc.repo, req.PR)
	if err := githubSendJSON(ctx, http.MethodPatch, acc.token, u, map[string]string{"state": req.State}, &out); err != nil {
		msg := err.Error()
		status := http.StatusBadGateway
		if ge, ok := err.(*ghAPIError); ok {
			status = ge.Status
			if status == http.StatusForbidden && strings.Contains(msg, "Resource not accessible") {
				msg = "O PAT deste perfil não tem permissão para fechar/reabrir PRs (precisa de 'Pull requests: Read and write'). " + msg
			}
			if status < 400 || status > 599 {
				status = http.StatusBadGateway
			}
		}
		fail(status, msg)
		return
	}
	after["before_state"] = cur
	logOperation(c, h.historyTracker, "code_editor_pr_state", resource, "", map[string]interface{}{"state": cur}, after, start, nil)
	c.JSON(http.StatusOK, gin.H{"state": out.State})
}
