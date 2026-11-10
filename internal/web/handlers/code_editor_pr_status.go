package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Status de merge do PR — antes de criar (prévia local: commits à frente/atrás e conflitos entre a
// branch e o destino) e depois de criado (o que o GitHub calcula: mergeable/mergeable_state).

// PRPreview é a prévia local de merge de head em base.
type PRPreview struct {
	BaseRef       string   `json:"base_ref"` // ref usada como destino (origin/<base> quando existe)
	Ahead         int      `json:"ahead"`    // commits de head que o destino não tem
	Behind        int      `json:"behind"`   // commits do destino que head não tem
	Checked       bool     `json:"checked"`  // false: não deu para simular o merge
	Conflicts     bool     `json:"conflicts"`
	ConflictFiles []string `json:"conflict_files"`
}

// resolveBaseRef prefere origin/<base> (o que o PR vai usar) e cai para a branch local.
func resolveBaseRef(dir, base string) string {
	if _, err := runGit(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		return "origin/" + base
	}
	return base
}

// parseMergeTreeNew lê `git merge-tree --write-tree --name-only` (git ≥ 2.38): na primeira linha o
// tree, depois os arquivos em conflito até a linha em branco (as mensagens vêm depois).
func parseMergeTreeNew(out string) []string {
	files := []string{}
	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		files = append(files, lines[i])
	}
	return files
}

// parseMergeTreeLegacy lê a saída do `git merge-tree <base> <a> <b>` antigo: há conflito quando
// aparece um marcador "+<<<<<<<"; os arquivos vêm das linhas "  our ..." dos blocos
// "changed in both" / "added in both".
func parseMergeTreeLegacy(out string) (bool, []string) {
	files := []string{}
	conflict := false
	inBoth := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "changed in both"), strings.HasPrefix(l, "added in both"):
			inBoth = true
		case inBoth && strings.HasPrefix(l, "  our"):
			if f := strings.Fields(l); len(f) >= 4 {
				files = append(files, f[3])
			}
			inBoth = false
		case strings.HasPrefix(l, "+<<<<<<<"):
			conflict = true
		}
	}
	return conflict, files
}

// simulateMerge diz se head e base conflitam, sem tocar no working tree.
func simulateMerge(dir, base, head string) (checked, conflicts bool, files []string) {
	out, err := runGit(dir, "merge-tree", "--write-tree", "--name-only", base, head)
	if err == nil {
		return true, false, []string{}
	}
	// git ≥ 2.38 sai com 1 e lista os arquivos em conflito; o antigo recusa a opção e imprime o uso
	if !strings.HasPrefix(out, "usage:") && !strings.Contains(out, "unknown option") && !strings.HasPrefix(out, "fatal:") {
		return true, true, parseMergeTreeNew(out)
	}
	// git antigo (sem --write-tree)
	mb, err := runGit(dir, "merge-base", base, head)
	if err != nil {
		return false, false, []string{}
	}
	old, err := runGit(dir, "merge-tree", mb, base, head)
	if err != nil {
		return false, false, []string{}
	}
	conflict, files := parseMergeTreeLegacy(old)
	return true, conflict, files
}

// GetPRPreview — GET /api/v1/code-editor/repos/:id/pr/preview?head=<branch>&base=<branch>
func (h *CodeEditorHandler) GetPRPreview(c *gin.Context) {
	dir := h.repoDir(c.Param("id"))
	head, base := strings.TrimSpace(c.Query("head")), strings.TrimSpace(c.Query("base"))
	if !validGitRev(head) || !validGitRev(base) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "head e base são obrigatórios"})
		return
	}
	baseRef := resolveBaseRef(dir, base)
	p := PRPreview{BaseRef: baseRef, ConflictFiles: []string{}}
	if out, err := runGit(dir, "rev-list", "--count", baseRef+".."+head); err == nil {
		p.Ahead, _ = strconv.Atoi(out)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	if out, err := runGit(dir, "rev-list", "--count", head+".."+baseRef); err == nil {
		p.Behind, _ = strconv.Atoi(out)
	}
	p.Checked, p.Conflicts, p.ConflictFiles = simulateMerge(dir, baseRef, head)
	c.JSON(http.StatusOK, p)
}

// PRMergeStatus é o que o GitHub informa sobre a possibilidade de merge do PR.
type PRMergeStatus struct {
	Number         int    `json:"number"`
	State          string `json:"state"`           // open | closed | merged
	Mergeable      *bool  `json:"mergeable"`       // nil: o GitHub ainda está calculando
	MergeableState string `json:"mergeable_state"` // clean | dirty | blocked | behind | unstable | draft | has_hooks | unknown
	Draft          bool   `json:"draft"`
	// MergeBlockReason: por que NÃO dá para mergear agora ("" = pode). Calculado a partir do estado acima.
	MergeBlockReason string `json:"merge_block_reason,omitempty"`
}

// mergeBlockReason diz por que o PR não pode ser mergeado agora ("" quando pode). Só deixa passar o que
// o GitHub chama de clean (tudo verde), has_hooks (verde, com hooks) e behind (verde, só atrasado em
// relação ao destino). Os checks e as aprovações exigidos pela proteção da branch entram em "blocked".
func mergeBlockReason(st PRMergeStatus) string {
	switch {
	case st.State == "merged":
		return "o PR já foi mergeado"
	case st.State != "open":
		return "o PR está fechado: reabra-o para mergear"
	case st.Draft || st.MergeableState == "draft":
		return "o PR é um rascunho"
	}
	switch st.MergeableState {
	case "clean", "has_hooks", "behind":
		return ""
	case "dirty":
		return "o PR tem conflito com a branch de destino"
	case "unstable":
		return "há checks falhando"
	case "blocked":
		return "o GitHub bloqueia o merge: checks obrigatórios, aprovações ou regra de proteção da branch pendentes"
	}
	return "o GitHub ainda está calculando se dá para mergear: tente de novo em alguns segundos"
}

// GetPRMergeStatus — GET /api/v1/code-editor/repos/:id/pr/status?pr=<n>&profile_id=<id>
// O GitHub calcula `mergeable` de forma assíncrona: logo após criar o PR vem nulo; o cliente repete.
func (h *CodeEditorHandler) GetPRMergeStatus(c *gin.Context) {
	n, _ := strconv.Atoi(c.Query("pr"))
	if n <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr é obrigatório"})
		return
	}
	t, ok := h.resolveRepoAccess(c) // não depende da branch atual: serve a qualquer PR do repositório
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	st, err := fetchPRMergeStatus(ctx, t.token, t.owner, t.repo, n)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func fetchPRMergeStatus(ctx context.Context, token, owner, repo string, number int) (PRMergeStatus, error) {
	var raw struct {
		State          string `json:"state"`
		Merged         bool   `json:"merged"`
		Draft          bool   `json:"draft"`
		Mergeable      *bool  `json:"mergeable"`
		MergeableState string `json:"mergeable_state"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBase, owner, repo, number)
	if err := githubGetJSON(ctx, token, u, &raw); err != nil {
		return PRMergeStatus{}, err
	}
	st := PRMergeStatus{Number: number, State: raw.State, Mergeable: raw.Mergeable, MergeableState: raw.MergeableState, Draft: raw.Draft}
	if raw.Merged {
		st.State = "merged"
	}
	st.MergeBlockReason = mergeBlockReason(st)
	return st, nil
}
