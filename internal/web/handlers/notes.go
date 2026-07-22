package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
)

// NotesHandler gerencia anotações em Markdown escopadas por cluster+aba.
type NotesHandler struct {
	store *storage.NotesStore
}

// NewNotesHandler cria o handler.
func NewNotesHandler(store *storage.NotesStore) *NotesHandler {
	return &NotesHandler{store: store}
}

func notesUserEmail(c *gin.Context) string {
	email, _ := c.Get("user_email")
	s := fmt.Sprintf("%v", email)
	if s == "" || s == "<nil>" {
		return "default"
	}
	return s
}

// List — GET /api/v1/notes?cluster=&tab=
func (h *NotesHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	tab := c.Query("tab")
	if cluster == "" || tab == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster e tab são obrigatórios"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusOK, []storage.Note{})
		return
	}
	notes, err := h.store.List(cluster, tab)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, notes)
}

// Create — POST /api/v1/notes  (body: {cluster, tab, content})
func (h *NotesHandler) Create(c *gin.Context) {
	var body struct {
		Cluster string `json:"cluster" binding:"required"`
		Tab     string `json:"tab" binding:"required"`
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	id, err := h.store.Save(storage.Note{
		Cluster:   body.Cluster,
		Tab:       body.Tab,
		Content:   body.Content,
		UserEmail: notesUserEmail(c),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Update — PUT /api/v1/notes/:id  (body: {content}) — só o autor pode editar
func (h *NotesHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	var body struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	existing, err := h.store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "nota não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing.UserEmail != notesUserEmail(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas o autor pode editar esta nota"})
		return
	}
	if err := h.store.Update(id, body.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete — DELETE /api/v1/notes/:id — só o autor pode excluir
func (h *NotesHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	existing, err := h.store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "nota não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing.UserEmail != notesUserEmail(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas o autor pode excluir esta nota"})
		return
	}
	if err := h.store.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
