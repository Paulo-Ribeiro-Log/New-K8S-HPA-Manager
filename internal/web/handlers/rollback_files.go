package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/rollbackfiles"
)

// ─── Biblioteca de arquivos de rollback ────────────────────────────────────────────────────────
//
// Pedido explícito do usuário depois de usar o Modo Nexus do Rollback de Deployment: (1) baixar
// (persistir no servidor) artefatos encontrados no Nexus, (2) um "Rollback Manual" que lista/
// seleciona arquivos dessa pasta gerenciada OU de qualquer outro diretório do servidor ("arquivos
// que já temos salvo de outras ações de rollback"), e (3) editar esses YAMLs via Monaco. Ver
// internal/rollbackfiles/store.go pra estrutura em disco e justificativas de segurança.

// RollbackFilesHandler expõe a pasta gerenciada de artefatos de rollback + navegação/leitura/
// escrita em diretórios externos arbitrários do servidor.
type RollbackFilesHandler struct {
	store *rollbackfiles.Store
}

// NewRollbackFilesHandler cria o handler — falha só se ~/.k8s-hpa-manager/rollback-artifacts/ não
// puder ser criada (permissão, disco cheio, etc.).
func NewRollbackFilesHandler() (*RollbackFilesHandler, error) {
	store, err := rollbackfiles.NewStore()
	if err != nil {
		return nil, err
	}
	return &RollbackFilesHandler{store: store}, nil
}

// List — GET /api/v1/rollback-files
func (h *RollbackFilesHandler) List(c *gin.Context) {
	entries, err := h.store.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"files": entries, "baseDir": h.store.BaseDir()}})
}

type rollbackFileSaveRequest struct {
	Name    string `json:"name" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// Save — POST /api/v1/rollback-files
// Usado pelo botão "Baixar" do Modo Nexus (o conteúdo já vem baixado do Nexus, aqui só persiste
// na pasta gerenciada) e por um eventual "Salvar como" manual.
func (h *RollbackFilesHandler) Save(c *gin.Context) {
	var req rollbackFileSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	entry, err := h.store.Save(req.Name, []byte(req.Content))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("SAVE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entry})
}

// Read — GET /api/v1/rollback-files/:name
func (h *RollbackFilesHandler) Read(c *gin.Context) {
	name := c.Param("name")
	content, err := h.store.Read(name)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse("READ_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"content": string(content)}})
}

type rollbackFileWriteRequest struct {
	Content string `json:"content" binding:"required"`
}

// Write — PUT /api/v1/rollback-files/:name
// Sobrescreve o conteúdo de um arquivo já salvo — usado pelo "Salvar" da edição via Monaco.
func (h *RollbackFilesHandler) Write(c *gin.Context) {
	name := c.Param("name")
	var req rollbackFileWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if err := h.store.Write(name, []byte(req.Content)); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("WRITE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Delete — DELETE /api/v1/rollback-files/:name
func (h *RollbackFilesHandler) Delete(c *gin.Context) {
	name := c.Param("name")
	if err := h.store.Delete(name); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("DELETE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Browse — GET /api/v1/rollback-files/browse?dir=/caminho/absoluto
// Lista arquivos .yaml/.yml de um diretório ARBITRÁRIO do servidor — "arquivos que já temos salvo
// de outras ações de rollback".
func (h *RollbackFilesHandler) Browse(c *gin.Context) {
	dir := strings.TrimSpace(c.Query("dir"))
	if dir == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "parâmetro dir é obrigatório"))
		return
	}
	entries, err := rollbackfiles.BrowseDirectory(dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("BROWSE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"files": entries}})
}

// ReadExternal — GET /api/v1/rollback-files/external?path=/caminho/absoluto/arquivo.yaml
func (h *RollbackFilesHandler) ReadExternal(c *gin.Context) {
	path := strings.TrimSpace(c.Query("path"))
	if path == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "parâmetro path é obrigatório"))
		return
	}
	content, err := rollbackfiles.ReadFileAtPath(path)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse("READ_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"content": string(content)}})
}

type rollbackFileWriteExternalRequest struct {
	Path    string `json:"path" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// WriteExternal — PUT /api/v1/rollback-files/external
func (h *RollbackFilesHandler) WriteExternal(c *gin.Context) {
	var req rollbackFileWriteExternalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if err := rollbackfiles.WriteFileAtPath(req.Path, []byte(req.Content)); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("WRITE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
