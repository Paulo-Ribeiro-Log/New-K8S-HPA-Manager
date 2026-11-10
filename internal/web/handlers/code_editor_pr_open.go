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

const openPRsMaxPages = 5 // até 500 PRs abertos

// listOpenPRs lista os PRs abertos (paginando). PRs de forks ficam de fora: a branch deles não
// existe neste repositório, e o nome poderia colidir com uma branch daqui.
func listOpenPRs(ctx context.Context, token, owner, repo string) ([]OpenPR, error) {
	var out []OpenPR
	full := owner + "/" + repo
	for page := 1; page <= openPRsMaxPages; page++ {
		var batch []struct {
			Number    int    `json:"number"`
			Title     string `json:"title"`
			HTMLURL   string `json:"html_url"`
			Draft     bool   `json:"draft"`
			UpdatedAt string `json:"updated_at"`
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
		u := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&sort=updated&direction=desc&per_page=100&page=%d", githubAPIBase, owner, repo, page)
		if err := githubGetJSON(ctx, token, u, &batch); err != nil {
			return nil, err
		}
		for _, p := range batch {
			if p.Head.Repo == nil || !strings.EqualFold(p.Head.Repo.FullName, full) {
				continue // fork (ou repositório de origem apagado)
			}
			out = append(out, OpenPR{Branch: p.Head.Ref, Number: p.Number, Title: p.Title, URL: p.HTMLURL,
				Base: p.Base.Ref, Draft: p.Draft, Author: p.User.Login, UpdatedAt: p.UpdatedAt})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// ListOpenPRs — GET /api/v1/code-editor/repos/:id/pr/open?profile_id=<id>
// Só leitura. Devolve os PRs abertos do repositório, com a branch de origem de cada um.
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
