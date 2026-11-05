package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Dono dos clones do Code Editor.
//
// Os clones ficam numa pasta única do servidor (~/.k8s-hpa-manager/repos), visível a todos os
// usuários. Excluir um clone faz RemoveAll, que apagaria também as mudanças ainda não commitadas
// de quem o está usando. Por isso o e-mail de quem clonou fica registrado num arquivo à parte e só
// essa pessoa exclui o clone. Clones antigos, sem dono registrado, continuam excluíveis por
// qualquer um (compatibilidade). A identidade vem do JWT; no modo de token estático ela é a do `az`
// do servidor (igual para todos), então a checagem não distingue pessoas nesse modo.
//
// "Abrir pasta" não entra aqui: a exclusão remove só o link, nunca os arquivos do usuário.

var repoOwnersMu sync.Mutex

func (h *CodeEditorHandler) repoOwnersPath() string {
	return filepath.Join(filepath.Dir(h.reposBase), "code-editor-repo-owners.json")
}

// loadRepoOwners lê o mapa id -> e-mail. Arquivo ausente ou inválido vira mapa vazio.
func (h *CodeEditorHandler) loadRepoOwners() map[string]string {
	owners := map[string]string{}
	if b, err := os.ReadFile(h.repoOwnersPath()); err == nil {
		_ = json.Unmarshal(b, &owners)
	}
	if owners == nil {
		owners = map[string]string{}
	}
	return owners
}

func (h *CodeEditorHandler) saveRepoOwners(owners map[string]string) error {
	b, err := json.MarshalIndent(owners, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.repoOwnersPath()), 0700); err != nil {
		return err
	}
	return os.WriteFile(h.repoOwnersPath(), b, 0600)
}

// repoOwner devolve o e-mail de quem clonou o repositório ("" se não houver registro).
func (h *CodeEditorHandler) repoOwner(id string) string {
	repoOwnersMu.Lock()
	defer repoOwnersMu.Unlock()
	return h.loadRepoOwners()[id]
}

// setRepoOwner registra quem clonou o repositório. E-mail vazio não registra nada.
func (h *CodeEditorHandler) setRepoOwner(id, email string) {
	if email == "" {
		return
	}
	repoOwnersMu.Lock()
	defer repoOwnersMu.Unlock()
	owners := h.loadRepoOwners()
	owners[id] = email
	_ = h.saveRepoOwners(owners)
}

func (h *CodeEditorHandler) clearRepoOwner(id string) {
	repoOwnersMu.Lock()
	defer repoOwnersMu.Unlock()
	owners := h.loadRepoOwners()
	if _, ok := owners[id]; !ok {
		return
	}
	delete(owners, id)
	_ = h.saveRepoOwners(owners)
}

// canDeleteRepo diz se o usuário pode excluir o clone: sem dono registrado, qualquer um; com dono,
// só ele (sem diferenciar maiúsculas).
func canDeleteRepo(owner, caller string) bool {
	return owner == "" || strings.EqualFold(owner, caller)
}

// deleteClonedRepo exclui um clone (não vale para "Abrir pasta"), respeitando o dono, e registra a
// operação no Change History. Devolve o status HTTP e a mensagem de erro ("" em caso de sucesso).
func (h *CodeEditorHandler) deleteClonedRepo(c *gin.Context, id string) (status int, errMsg string) {
	start := time.Now()
	owner := h.repoOwner(id)
	caller := GetUserInfoForHistory(c).Email
	if !canDeleteRepo(owner, caller) {
		err := os.ErrPermission
		logOperation(c, h.historyTracker, "code_editor_delete_repo", id, "", nil,
			map[string]interface{}{"owner": owner, "denied": true}, start, err)
		return 403, "este repositório foi clonado por " + owner + "; só essa pessoa pode excluí-lo"
	}
	err := os.RemoveAll(h.repoDir(id))
	logOperation(c, h.historyTracker, "code_editor_delete_repo", id, "", nil,
		map[string]interface{}{"owner": owner}, start, err)
	if err != nil {
		return 500, err.Error()
	}
	h.clearRepoOwner(id)
	return 200, ""
}
