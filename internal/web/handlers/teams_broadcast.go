package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"k8s-hpa-manager/internal/teams"
)

type BroadcastChat struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
}

type BroadcastChatsResponse struct {
	Query      string          `json:"query,omitempty"`
	SearchedAt string          `json:"searched_at,omitempty"`
	Count      int             `json:"count"`
	Chats      []BroadcastChat `json:"chats"`
}

type SendBroadcastRequest struct {
	ThreadIDs []string `json:"thread_ids"`
	Markdown  string   `json:"markdown"`
}

type BroadcastTemplate struct {
	Filename  string `json:"filename"`
	UpdatedAt string `json:"updated_at"`
	Size      int64  `json:"size"`
}

type SaveTemplateRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type TeamsBroadcastHandler struct {
	logger   *zerolog.Logger
	scanning bool
	scanMu   sync.Mutex
}

func NewTeamsBroadcastHandler(logger *zerolog.Logger) *TeamsBroadcastHandler {
	return &TeamsBroadcastHandler{logger: logger}
}

// normalizeSearch remove acentos e converte para minúsculas para busca tolerante.
func normalizeSearch(s string) string {
	t := transform.Chain(norm.NFD, transform.RemoveFunc(func(r rune) bool {
		return unicode.Is(unicode.Mn, r)
	}))
	result, _, _ := transform.String(t, strings.ToLower(s))
	return result
}

func broadcastTemplatesDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".k8s-hpa-manager", "broadcast-templates")
	return dir, os.MkdirAll(dir, 0700)
}

// safeFilename rejeita nomes com path traversal ou caracteres inválidos.
func safeFilename(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") ||
		strings.Contains(name, "..") || strings.ContainsAny(name, "\x00") {
		return false
	}
	return true
}

// GetChats lê todos os arquivos teams-chats-*.json e mescla os resultados.
func (h *TeamsBroadcastHandler) GetChats(c *gin.Context) {
	query := normalizeSearch(c.Query("q"))
	homeDir, _ := os.UserHomeDir()
	baseDir := filepath.Join(homeDir, ".k8s-hpa-manager")

	files, _ := filepath.Glob(filepath.Join(baseDir, "teams-chats-*.json"))

	seen := map[string]bool{}
	var allChats []BroadcastChat

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var resp BroadcastChatsResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		for _, ch := range resp.Chats {
			if seen[ch.ID] {
				continue
			}
			seen[ch.ID] = true
			allChats = append(allChats, ch)
		}
	}

	result := make([]BroadcastChat, 0, len(allChats))
	for _, ch := range allChats {
		if query == "" || strings.Contains(normalizeSearch(ch.DisplayName), query) {
			result = append(result, ch)
		}
	}

	c.JSON(http.StatusOK, BroadcastChatsResponse{Count: len(result), Chats: result})
}

// ScanChats lança o Chrome e escaneia TODAS as conversas do IndexedDB do Teams.
func (h *TeamsBroadcastHandler) ScanChats(c *gin.Context) {
	h.scanMu.Lock()
	if h.scanning {
		h.scanMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "scan já em andamento"})
		return
	}
	h.scanning = true
	h.scanMu.Unlock()
	defer func() {
		h.scanMu.Lock()
		h.scanning = false
		h.scanMu.Unlock()
	}()

	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")
	outputPath := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-chats-all.json")

	h.logger.Info().Msg("[Broadcast] Iniciando scan de todos os chats do Teams...")

	chats, err := teams.ScanConversations(sessionDir, outputPath, h.logger)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": len(chats), "path": outputPath})
}

// ListTemplates retorna todos os arquivos salvos em broadcast-templates/.
func (h *TeamsBroadcastHandler) ListTemplates(c *gin.Context) {
	dir, err := broadcastTemplatesDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	templates := make([]BroadcastTemplate, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		t := BroadcastTemplate{Filename: e.Name()}
		if info != nil {
			t.Size = info.Size()
			t.UpdatedAt = info.ModTime().Format(time.RFC3339)
		}
		templates = append(templates, t)
	}

	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// SaveTemplate salva (cria ou sobrescreve) um arquivo em broadcast-templates/.
func (h *TeamsBroadcastHandler) SaveTemplate(c *gin.Context) {
	var req SaveTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !safeFilename(req.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de arquivo inválido"})
		return
	}

	dir, err := broadcastTemplatesDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	path := filepath.Join(dir, req.Filename)
	if err := os.WriteFile(path, []byte(req.Content), 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info().Str("file", req.Filename).Msg("[Broadcast] Template salvo")
	c.JSON(http.StatusOK, gin.H{"filename": req.Filename})
}

// GetTemplate retorna o conteúdo de um arquivo salvo.
func (h *TeamsBroadcastHandler) GetTemplate(c *gin.Context) {
	filename := c.Param("filename")
	if !safeFilename(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de arquivo inválido"})
		return
	}

	dir, err := broadcastTemplatesDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "arquivo não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"filename": filename, "content": string(data)})
}

// DeleteTemplate remove um arquivo salvo.
func (h *TeamsBroadcastHandler) DeleteTemplate(c *gin.Context) {
	filename := c.Param("filename")
	if !safeFilename(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de arquivo inválido"})
		return
	}

	dir, err := broadcastTemplatesDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := os.Remove(filepath.Join(dir, filename)); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "arquivo não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info().Str("file", filename).Msg("[Broadcast] Template removido")
	c.JSON(http.StatusOK, gin.H{"deleted": filename})
}

// Send envia uma mensagem markdown para os chats selecionados.
func (h *TeamsBroadcastHandler) Send(c *gin.Context) {
	var req SendBroadcastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.ThreadIDs) == 0 || req.Markdown == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_ids e markdown são obrigatórios"})
		return
	}
	h.logger.Info().Int("chats", len(req.ThreadIDs)).Msg("[Broadcast] Solicitação de envio")
	c.JSON(http.StatusNotImplemented, gin.H{"error": "envio via browser ainda não implementado"})
}
