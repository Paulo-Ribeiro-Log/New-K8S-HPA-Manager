package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Diff do PR — arquivos alterados e o patch de cada um, lidos (somente leitura) da API do GitHub
// para o PR da branch atual. O GitHub omite `patch` em arquivo binário ou muito grande.

// PRDiffFile é um arquivo alterado pelo PR.
type PRDiffFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename,omitempty"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch,omitempty"`
}

const prDiffMaxPages = 30 // a API do GitHub limita a 3000 arquivos por PR

// listPRFiles busca todos os arquivos do PR, paginando.
func listPRFiles(ctx context.Context, token, owner, repo string, number int) ([]PRDiffFile, error) {
	var all []PRDiffFile
	for page := 1; page <= prDiffMaxPages; page++ {
		var batch []PRDiffFile
		u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", githubAPIBase, owner, repo, number, page)
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

// GetPRDiff — GET /api/v1/code-editor/repos/:id/pr/diff?pr=<n>&profile_id=<id>
// Sem `pr`, usa o PR aberto mais recente da branch atual.
func (h *CodeEditorHandler) GetPRDiff(c *gin.Context) {
	t, ok := h.resolvePlanTarget(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	prs, err := listBranchPRs(ctx, t.token, t.owner, t.repo, t.branch)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if len(prs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "a branch " + t.branch + " não tem Pull Request"})
		return
	}
	selected := pickPR(prs, c.Query("pr"))
	files, err := listPRFiles(ctx, t.token, t.owner, t.repo, selected.Number)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pr": selected, "files": files})
}
