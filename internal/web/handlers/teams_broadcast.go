package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"k8s-hpa-manager/internal/teams"
	"k8s-hpa-manager/internal/web/sse"
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
	SessionID string   `json:"session_id"` // UUID gerado pelo frontend para o stream SSE
	ThreadIDs []string `json:"thread_ids"`
	Markdown  string   `json:"markdown"`
	HTML      string   `json:"html"` // conteúdo já renderizado do painel de preview
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
	tracker  *sse.ProgressTracker
	scanning bool
	scanMu   sync.Mutex
	sending  bool
	sendMu   sync.Mutex
}

func NewTeamsBroadcastHandler(logger *zerolog.Logger) *TeamsBroadcastHandler {
	return &TeamsBroadcastHandler{
		logger:  logger,
		tracker: GetProgressTracker(),
	}
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

// Send inicia o envio em lote de forma assíncrona e retorna 202 imediatamente.
// O progresso é publicado via SSE no stream identificado por session_id.
func (h *TeamsBroadcastHandler) Send(c *gin.Context) {
	var req SendBroadcastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id é obrigatório"})
		return
	}
	if len(req.ThreadIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_ids é obrigatório"})
		return
	}

	h.sendMu.Lock()
	if h.sending {
		h.sendMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "envio já em andamento"})
		return
	}
	h.sending = true
	h.sendMu.Unlock()

	htmlContent := req.HTML
	if htmlContent == "" {
		if req.Markdown == "" {
			h.sendMu.Lock()
			h.sending = false
			h.sendMu.Unlock()
			c.JSON(http.StatusBadRequest, gin.H{"error": "html ou markdown é obrigatório"})
			return
		}
		htmlContent = markdownToTeamsHTML(req.Markdown)
	}

	sessionID := req.SessionID
	total := len(req.ThreadIDs)
	homeDir, _ := os.UserHomeDir()
	teamsSessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	h.logger.Info().Str("session", sessionID).Int("total", total).Msg("[Broadcast] Iniciando envio assíncrono")

	// Publicar evento inicial para o cliente SSE saber que começou.
	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:       sessionID,
		Type:     "broadcast_start",
		Phase:    "started",
		Message:  "Iniciando envio...",
		Progress: 0,
	})

	go func() {
		defer func() {
			h.sendMu.Lock()
			h.sending = false
			h.sendMu.Unlock()
		}()

		sent := 0
		failed := 0
		var allResults []teams.SendResult

		onProgress := func(r teams.SendResult) {
			allResults = append(allResults, r)
			if r.OK {
				sent++
			} else {
				failed++
			}
			done := sent + failed
			progress := float64(done) / float64(total)

			// Serializar result para Details
			raw, _ := json.Marshal(r)

			h.tracker.SendToClient(sessionID, sse.ProgressEvent{
				ID:       sessionID,
				Type:     "broadcast_progress",
				Phase:    "in_progress",
				Message:  r.ThreadID,
				Progress: progress,
				Details:  string(raw),
				Error:    r.Error,
			})
		}

		results, err := teams.SendBatch(teamsSessionDir, req.ThreadIDs, htmlContent, onProgress, h.logger)
		if err != nil {
			h.logger.Error().Err(err).Msg("[Broadcast] Erro no envio")
			h.tracker.SendToClient(sessionID, sse.ProgressEvent{
				ID:      sessionID,
				Type:    "error",
				Phase:   "failed",
				Message: err.Error(),
				Error:   err.Error(),
			})
			return
		}

		// Calcular totais finais a partir dos resultados completos.
		sent, failed = 0, 0
		for _, r := range results {
			if r.OK {
				sent++
			} else {
				failed++
			}
		}

		raw, _ := json.Marshal(gin.H{"sent": sent, "failed": failed, "results": results})
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:       sessionID,
			Type:     "complete",
			Phase:    "completed",
			Message:  "Envio concluído",
			Progress: 1.0,
			Details:  string(raw),
		})
		h.logger.Info().Int("sent", sent).Int("failed", failed).Msg("[Broadcast] Envio concluído")
	}()

	c.JSON(http.StatusAccepted, gin.H{"session_id": sessionID, "total": total})
}

// StreamSend abre o stream SSE de progresso do envio em lote.
// GET /api/v1/teams/broadcast/send/stream/:sessionId
func (h *TeamsBroadcastHandler) StreamSend(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId obrigatório"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Replay de eventos já publicados (ex: cliente conectou depois do início).
	for _, evt := range h.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(event)); err != nil {
				return
			}
			c.Writer.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

// markdownToTeamsHTML converte markdown simples para HTML aceito pelo Teams.
// Suporta: negrito, itálico, código, quebras de linha, cabeçalhos e listas.
func markdownToTeamsHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	inList := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")

		// Cabeçalho
		if strings.HasPrefix(trimmed, "### ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			sb.WriteString("<p><b>" + htmlEscape(trimmed[4:]) + "</b></p>")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			sb.WriteString("<p><b>" + htmlEscape(trimmed[3:]) + "</b></p>")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			sb.WriteString("<p><b>" + htmlEscape(trimmed[2:]) + "</b></p>")
			continue
		}

		// Item de lista
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if !inList {
				sb.WriteString("<ul>")
				inList = true
			}
			content := applyInlineMarkdown(trimmed[2:])
			sb.WriteString("<li>" + content + "</li>")
			continue
		}

		// Linha vazia
		if trimmed == "" {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			continue
		}

		// Parágrafo normal
		if inList {
			sb.WriteString("</ul>")
			inList = false
		}
		sb.WriteString("<p>" + applyInlineMarkdown(trimmed) + "</p>")
	}

	if inList {
		sb.WriteString("</ul>")
	}
	return sb.String()
}

var (
	reBold1   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBold2   = regexp.MustCompile(`__(.+?)__`)
	reItalic1 = regexp.MustCompile(`\*(.+?)\*`)
	reItalic2 = regexp.MustCompile(`_(.+?)_`)
	reCode    = regexp.MustCompile("`(.+?)`")
)

func applyInlineMarkdown(s string) string {
	s = reBold1.ReplaceAllString(s, "<b>$1</b>")
	s = reBold2.ReplaceAllString(s, "<b>$1</b>")
	s = reItalic1.ReplaceAllString(s, "<i>$1</i>")
	s = reItalic2.ReplaceAllString(s, "<i>$1</i>")
	s = reCode.ReplaceAllString(s, "<code>$1</code>")
	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
