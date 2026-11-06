package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Apagar e renomear branches do Code Editor, no clone local e (opcionalmente) no origin.

// protectedBranches nunca são apagadas nem renomeadas no remoto por aqui.
var protectedBranches = map[string]bool{"main": true, "master": true}

// branchExists diz se a ref existe (refs/heads/<n> ou refs/remotes/origin/<n>).
func branchExists(dir, ref string) bool {
	_, err := runGit(dir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// validBranchName usa o próprio git (`check-ref-format --branch`), que também rejeita "-x".
func validBranchName(dir, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	_, err := runGit(dir, "check-ref-format", "--branch", name)
	return err == nil
}

// remoteGit roda um comando git contra o origin com o PAT do usuário (perfil explícito, depois o
// ativo, depois o legado — a mesma ordem do CreatePR).
func (h *CodeEditorHandler) remoteGit(c *gin.Context, dir, profileID string, args ...string) (string, error) {
	token := h.resolveProfileToken(c, profileID)
	if token == "" {
		token = h.resolveActiveProfileToken(c)
	}
	if token == "" {
		token = h.getToken(c)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	cmd, cleanup := gitCmdWithToken(ctx, dir, token, args...)
	defer cleanup()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// RenameBranch — POST /api/v1/code-editor/repos/:id/branch/rename
// Body: { "old": "...", "new": "...", "rename_remote": false, "profile_id": "" }
// Renomeia a branch local (inclusive a atual). Com rename_remote e a branch existindo no origin,
// publica o nome novo, apaga o antigo e religa o upstream. Se o remoto falhar, a renomeação local
// fica feita e a resposta traz `warning`.
func (h *CodeEditorHandler) RenameBranch(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Old          string `json:"old"`
		New          string `json:"new"`
		RenameRemote bool   `json:"rename_remote"`
		ProfileID    string `json:"profile_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Old == "" || req.New == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "old e new são obrigatórios"})
		return
	}
	req.Old, req.New = strings.TrimSpace(req.Old), strings.TrimSpace(req.New)
	if !validBranchName(dir, req.Old) || !validBranchName(dir, req.New) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de branch inválido"})
		return
	}
	if req.Old == req.New {
		c.JSON(http.StatusBadRequest, gin.H{"error": "o nome novo é igual ao atual"})
		return
	}
	if !branchExists(dir, "refs/heads/"+req.Old) {
		c.JSON(http.StatusNotFound, gin.H{"error": "a branch local " + req.Old + " não existe"})
		return
	}
	if branchExists(dir, "refs/heads/"+req.New) {
		c.JSON(http.StatusConflict, gin.H{"error": "já existe uma branch local chamada " + req.New})
		return
	}
	onRemote := branchExists(dir, "refs/remotes/origin/"+req.Old)
	if req.RenameRemote && onRemote && protectedBranches[req.Old] {
		c.JSON(http.StatusForbidden, gin.H{"error": "a branch " + req.Old + " não pode ser renomeada no remoto por aqui"})
		return
	}

	start := time.Now()
	var outcome error
	defer func() {
		logOperation(c, h.historyTracker, "code_editor_rename_branch", id+": "+req.Old+" → "+req.New, "", nil,
			map[string]interface{}{"repo_id": id, "old": req.Old, "new": req.New, "remote": req.RenameRemote && onRemote}, start, outcome)
	}()

	if out, err := runGit(dir, "branch", "-m", req.Old, req.New); err != nil {
		outcome = fmt.Errorf("%s", out)
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}

	resp := gin.H{"old": req.Old, "new": req.New, "current": currentBranch(dir), "remote_renamed": false}
	if req.RenameRemote && onRemote {
		if out, err := h.remoteGit(c, dir, req.ProfileID, "push", "-u", "origin", req.New); err != nil {
			outcome = fmt.Errorf("remoto: %s", out)
			resp["warning"] = "Renomeada só localmente — não foi possível publicar o nome novo no origin: " + out
			c.JSON(http.StatusOK, resp)
			return
		}
		if out, err := h.remoteGit(c, dir, req.ProfileID, "push", "origin", "--delete", req.Old); err != nil {
			outcome = fmt.Errorf("remoto: %s", out)
			resp["warning"] = "O nome novo foi publicado, mas não foi possível apagar " + req.Old + " do origin: " + out
			c.JSON(http.StatusOK, resp)
			return
		}
		resp["remote_renamed"] = true
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteBranch — POST /api/v1/code-editor/repos/:id/branch/delete
// Body: { "name": "...", "scope": "local"|"remote"|"both", "force": false, "profile_id": "" }
// `local` usa `git branch -d` (só apaga o que já foi mergeado; force → -D). A branch atual e as
// protegidas (main/master) não são apagadas no remoto.
func (h *CodeEditorHandler) DeleteBranch(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Name      string `json:"name"`
		Scope     string `json:"scope"`
		Force     bool   `json:"force"`
		ProfileID string `json:"profile_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Scope != "local" && req.Scope != "remote" && req.Scope != "both" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope deve ser local, remote ou both"})
		return
	}
	if !validBranchName(dir, req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de branch inválido"})
		return
	}
	doLocal := req.Scope == "local" || req.Scope == "both"
	doRemote := req.Scope == "remote" || req.Scope == "both"

	if doLocal && currentBranch(dir) == req.Name {
		c.JSON(http.StatusConflict, gin.H{"error": "não dá para apagar a branch atual — troque de branch antes"})
		return
	}
	if doRemote && protectedBranches[req.Name] {
		c.JSON(http.StatusForbidden, gin.H{"error": "a branch " + req.Name + " não pode ser apagada no remoto por aqui"})
		return
	}
	if doLocal && !branchExists(dir, "refs/heads/"+req.Name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "a branch local " + req.Name + " não existe"})
		return
	}

	start := time.Now()
	var outcome error
	defer func() {
		logOperation(c, h.historyTracker, "code_editor_delete_branch", id+": "+req.Name, "", nil,
			map[string]interface{}{"repo_id": id, "branch": req.Name, "scope": req.Scope, "force": req.Force}, start, outcome)
	}()

	if doLocal {
		flag := "-d"
		if req.Force {
			flag = "-D"
		}
		if out, err := runGit(dir, "branch", flag, req.Name); err != nil {
			outcome = fmt.Errorf("%s", out)
			if strings.Contains(out, "not fully merged") {
				c.JSON(http.StatusConflict, gin.H{"error": out, "error_type": "not_merged"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": out})
			return
		}
	}
	if doRemote {
		if out, err := h.remoteGit(c, dir, req.ProfileID, "push", "origin", "--delete", req.Name); err != nil {
			outcome = fmt.Errorf("remoto: %s", out)
			msg := out
			if doLocal {
				msg = "A branch local foi apagada, mas não foi possível apagar do origin: " + out
			}
			c.JSON(http.StatusBadGateway, gin.H{"error": msg})
			return
		}
		runGit(dir, "branch", "-dr", "origin/"+req.Name) //nolint:errcheck // limpa a ref remota local já apagada
	}
	c.JSON(http.StatusOK, gin.H{"branch": req.Name, "scope": req.Scope})
}
