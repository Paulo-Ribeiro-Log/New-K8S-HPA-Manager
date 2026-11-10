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
	if prs == nil {
		prs = []OpenPR{}
	}
	c.JSON(http.StatusOK, gin.H{"prs": prs})
}
