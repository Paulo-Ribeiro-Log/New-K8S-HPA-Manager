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
	// HTML — campo legado (conteúdo renderizado do painel de preview via innerHTML). Bug real
	// corrigido (relatado ao vivo, 4ª rodada da mesma investigação de "espaçamento entre linhas
	// ignorado ao enviar" — as 3 rodadas anteriores corrigiram markdownToTeamsHTML sem nenhum
	// efeito visível, porque essa função NUNCA rodava de verdade): o frontend sempre populava
	// este campo com `previewRef.current.innerHTML` e nunca o deixava vazio, então o branch que
	// chama `markdownToTeamsHTML` (abaixo) nunca era alcançado — todas as correções anteriores
	// eram código morto do ponto de vista da UI real. E o HTML gerado pelo preview (react-
	// markdown/remark-gfm, CommonMark padrão) sofre uma limitação estrutural: markdown padrão
	// **não tem como representar** "várias linhas em branco" como algo diferente de "uma linha
	// em branco" — múltiplas linhas em branco consecutivas sempre colapsam pra uma única quebra
	// de parágrafo, não importa quantas existiam no texto original. Ou seja, mesmo com
	// `markdownToTeamsHTML` corrigido, o HTML do preview já tinha perdido essa informação antes
	// de chegar aqui. Corrigido no frontend: `handleSend` para de mandar este campo (fica vazio
	// por padrão agora) — o envio passa a sempre reconstruir o HTML a partir de `Markdown`/
	// `IsPlainText` aqui no backend, que processa cada linha literalmente (não via parser
	// CommonMark), preservando a contagem exata de linhas em branco. Mantido no struct só por
	// compatibilidade — se vier preenchido (cliente antigo/futuro uso manual da API), ainda tem
	// prioridade, mas a UI atual nunca mais o envia.
	HTML string `json:"html"`
	// IsPlainText — replica o toggle "Texto simples"/"Markdown" do editor (`isPlainText` no
	// frontend). Quando true, `Markdown` é tratado como texto literal (escapado, sem interpretar
	// `*`/`_`/`` ` ``/etc. como sintaxe) — via plainTextToTeamsHTML, não markdownToTeamsHTML.
	IsPlainText bool `json:"is_plain_text"`
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

type FetchMessageRequest struct {
	Link string `json:"link"`
}

type TeamsBroadcastHandler struct {
	logger      *zerolog.Logger
	tracker     *sse.ProgressTracker
	scanning    bool
	scanMu      sync.Mutex
	sending     bool
	sendMu      sync.Mutex
	fetchingMsg bool
	fetchMsgMu  sync.Mutex
	deleting    bool
	deleteMu    sync.Mutex
}

// DeleteMessagesTarget identifica uma mensagem já enviada a apagar — thread_id + o message_id
// REAL atribuído pelo servidor do Teams (retornado em SendResult.MessageID no momento do envio,
// nunca o clientmessageid usado só pra compor a mensagem).
type DeleteMessagesTarget struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
}

type DeleteMessagesRequest struct {
	Targets []DeleteMessagesTarget `json:"targets"`
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

// DeleteMessages apaga uma ou mais mensagens já enviadas pelo broadcast (ex: mensagem mal
// formatada enviada por engano). Síncrono — diferente de Send, o volume aqui é sempre pequeno
// (os destinatários de UM envio já concluído), não precisa de progresso via SSE.
func (h *TeamsBroadcastHandler) DeleteMessages(c *gin.Context) {
	var req DeleteMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Targets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targets é obrigatório"})
		return
	}
	for _, t := range req.Targets {
		if strings.TrimSpace(t.ThreadID) == "" || strings.TrimSpace(t.MessageID) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cada target precisa de thread_id e message_id"})
			return
		}
	}

	h.deleteMu.Lock()
	if h.deleting {
		h.deleteMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "outra exclusão já está em andamento"})
		return
	}
	h.deleting = true
	h.deleteMu.Unlock()
	defer func() {
		h.deleteMu.Lock()
		h.deleting = false
		h.deleteMu.Unlock()
	}()

	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	targets := make([]teams.DeleteTarget, len(req.Targets))
	for i, t := range req.Targets {
		targets[i] = teams.DeleteTarget{ThreadID: t.ThreadID, MessageID: t.MessageID}
	}

	h.logger.Info().Int("targets", len(targets)).Msg("[Broadcast] Apagando mensagens...")

	results, err := teams.DeleteMessages(sessionDir, targets, h.logger)
	if err != nil {
		h.logger.Error().Err(err).Msg("[Broadcast] Erro ao apagar mensagens")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deleted := 0
	for _, r := range results {
		if r.OK {
			deleted++
		}
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted, "failed": len(results) - deleted, "results": results})
}

// FetchMessage carrega o texto de uma mensagem específica do Teams a partir do link de
// "Copiar link" (ex: https://teams.microsoft.com/l/message/<threadId>/<messageId>?context=...).
// Útil para reaproveitar o conteúdo de uma mensagem já publicada como ponto de partida do
// broadcast, sem precisar copiar/colar manualmente do Teams (perde formatação/quebras de linha).
func (h *TeamsBroadcastHandler) FetchMessage(c *gin.Context) {
	var req FetchMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Link) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "link é obrigatório"})
		return
	}

	h.fetchMsgMu.Lock()
	if h.fetchingMsg {
		h.fetchMsgMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "outro carregamento de mensagem já está em andamento"})
		return
	}
	h.fetchingMsg = true
	h.fetchMsgMu.Unlock()
	defer func() {
		h.fetchMsgMu.Lock()
		h.fetchingMsg = false
		h.fetchMsgMu.Unlock()
	}()

	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	h.logger.Info().Str("link", req.Link).Msg("[Broadcast] Carregando mensagem via link do Teams...")

	msg, err := teams.FetchMessageByLink(sessionDir, req.Link, h.logger)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, msg)
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

	// req.HTML só tem prioridade se vier preenchido explicitamente (compatibilidade — a UI atual
	// nunca mais envia esse campo, ver comentário no struct SendBroadcastRequest). Caminho normal:
	// reconstrói o HTML aqui a partir de Markdown/IsPlainText, preservando a contagem exata de
	// linhas em branco (o que o HTML de preview via CommonMark não consegue fazer).
	htmlContent := req.HTML
	if htmlContent == "" {
		if req.Markdown == "" {
			h.sendMu.Lock()
			h.sending = false
			h.sendMu.Unlock()
			c.JSON(http.StatusBadRequest, gin.H{"error": "html ou markdown é obrigatório"})
			return
		}
		if req.IsPlainText {
			htmlContent = plainTextToTeamsHTML(req.Markdown)
		} else {
			htmlContent = markdownToTeamsHTML(req.Markdown)
		}
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
// Suporta: negrito, itálico, tachado, código, links, quebras de linha, cabeçalhos, listas e
// citações.
//
// Bug real corrigido (relatado ao vivo: "os espaços entre linhas ao enviar o arquivo continua
// sendo ignorado e fica mal formatado na mensagem final") — a versão original tratava CADA linha
// não-vazia como um `<p>` isolado e **descartava por completo** qualquer linha em branco.
//
// Bug real corrigido — 2ª rodada (relatado ao vivo: "o texto tem espaços de 2 linhas entre elas,
// mas o espaço exibido na mensagem entregue é de apenas uma linha") — a 1ª correção emitia um
// `<p>&nbsp;</p>` por linha em branco, e assumia que `<p>`s sucessivos já tinham uma margem
// própria dando o espaçamento "de base" (só emitindo `&nbsp;` pras linhas ALÉM da primeira).
//
// Bug real corrigido — 3ª rodada (relatado ao vivo comparando o texto extraído com o que
// efetivamente chegou entregue no Teams: espaçamento simples nunca aparecia, só o "extra"):
// a suposição da 2ª rodada — de que dois `<p>` vizinhos têm margem suficiente pra criar
// separação visível sozinhos — é FALSA no client de mensagens do Teams, que zera (ou deixa bem
// próximo de zero) a margem entre parágrafos consecutivos pra manter mensagens compactas.
// Resultado prático: toda linha em branco do texto original (não só a partir da 2ª) precisava de
// representação explícita, e a versão anterior só cobria "linha em branco extra além da primeira"
// — a separação "de base" nunca existia de verdade para começo.
//
// Reescrito com um modelo mais simples e sem depender de NENHUMA suposição sobre margem de `<p>`
// do Teams: o corpo de texto corrido (parágrafos + linhas em branco) vira uma lista de
// "segmentos" (`bodySegments`) unidos por `<br>` dentro de UM ÚNICO `<p>` — cada linha de
// conteúdo é um segmento; cada linha em branco é um segmento VAZIO. Como `<br>` sempre força uma
// quebra de linha de verdade (não depende de margem/CSS de bloco), o número de `<br>` entre dois
// segmentos de conteúdo reproduz exatamente o número de linhas em branco que existiam entre eles
// no original: 1 linha em branco → 2 `<br>` seguidos (fecha a linha de cima, abre e fecha a linha
// vazia); 2 linhas em branco → 3 `<br>` seguidos; e assim por diante — sem precisar de nenhum
// caso especial "-1" ou de `&nbsp;` decorativo. Só elementos genuinamente estruturais (cabeçalho,
// lista, citação) continuam em blocos `<p>`/`<ul>` próprios, o que é esperado ficarem visualmente
// destacados do corpo.
func markdownToTeamsHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	inList := false

	// bodySegments acumula o corpo de texto corrido até um elemento estrutural interromper —
	// ver comentário da função acima sobre como isso reproduz o número exato de linhas em branco.
	var bodySegments []string
	flushBody := func() {
		// Remove segmentos vazios do INÍCIO/FIM do bloco antes de unir — evitam um <br> solto e
		// sem sentido visual no começo/fim do <p> (ex: a mensagem inteira sempre termina com uma
		// linha em branco "estrutural", sobra do \n final que htmlToMarkdown sempre adiciona;
		// sem isso, TODA mensagem enviada ganhava um <br> supérfluo bem no final). Segmentos
		// vazios NO MEIO do bloco são preservados — são eles que representam linhas em branco de
		// verdade entre parágrafos (ver comentário da função acima).
		start, end := 0, len(bodySegments)
		for start < end && bodySegments[start] == "" {
			start++
		}
		for end > start && bodySegments[end-1] == "" {
			end--
		}
		segs := bodySegments[start:end]
		bodySegments = nil
		if len(segs) == 0 {
			return
		}
		sb.WriteString("<p>" + strings.Join(segs, "<br>") + "</p>")
	}
	closeList := func() {
		if inList {
			sb.WriteString("</ul>")
			inList = false
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimRight(line, " \t")

		// Linha vazia — vira um segmento vazio no corpo em andamento (ver comentário da função).
		// Ignorada se nada foi acumulado ainda (linhas em branco soltas antes do primeiro texto
		// real, ou logo após um bloco estrutural, não precisam virar espaço visível).
		if trimmed == "" {
			if len(bodySegments) > 0 {
				bodySegments = append(bodySegments, "")
			}
			continue
		}

		// Cabeçalho — flushBody() aqui (e em citação/lista abaixo) é necessário mesmo sem linha
		// vazia entre um parágrafo e o próximo bloco (ex: "texto\n# Título", sem separador): sem
		// isso o corpo acumulado ficaria pendurado no buffer e sairia DEPOIS do cabeçalho no HTML
		// final, embora viesse antes no texto original — ordem trocada.
		if strings.HasPrefix(trimmed, "### ") {
			flushBody()
			closeList()
			sb.WriteString("<p><b>" + htmlEscape(trimmed[4:]) + "</b></p>")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			flushBody()
			closeList()
			sb.WriteString("<p><b>" + htmlEscape(trimmed[3:]) + "</b></p>")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			flushBody()
			closeList()
			sb.WriteString("<p><b>" + htmlEscape(trimmed[2:]) + "</b></p>")
			continue
		}

		// Citação (produzida por htmlToMarkdown a partir de <blockquote> ao carregar)
		if strings.HasPrefix(trimmed, "> ") {
			flushBody()
			closeList()
			sb.WriteString("<p>" + applyInlineMarkdown(trimmed[2:]) + "</p>")
			continue
		}

		// Item de lista
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flushBody()
			if !inList {
				sb.WriteString("<ul>")
				inList = true
			}
			content := applyInlineMarkdown(trimmed[2:])
			sb.WriteString("<li>" + content + "</li>")
			continue
		}

		// Tabela Markdown (GFM): uma linha com "|" imediatamente seguida por uma linha
		// separadora (só traços/dois-pontos/pipes, ex: "|---|:---:|---|") é sinal inequívoco de
		// tabela — mesma regra do GFM que o preview em react-markdown/remark-gfm já usa.
		//
		// Bug real corrigido (relatado ao vivo: mensagem inteira chegando "sem formatação", com
		// o exemplo mais visível sendo uma tabela ilegível, só os caracteres "|"/"-" crus dentro
		// de um parágrafo): esta função nunca implementou tabelas — antes desta correção, cada
		// linha de uma tabela caía no caminho de "corpo normal" abaixo e virava texto literal.
		// Confirmado que o Teams aceita <table> de verdade no conteúdo da mensagem — é o mesmo
		// elemento que o botão "Inserir tabela" da própria barra de formatação do Teams gera ao
		// compor manualmente (diferente da limitação de mensagens de bot via Bot Framework/
		// Connector API, que não é o protocolo usado aqui — ver comentário do payload em
		// internal/teams/sender.go).
		if strings.Contains(trimmed, "|") && i+1 < len(lines) && reTableSepRow.MatchString(strings.TrimSpace(lines[i+1])) {
			flushBody()
			closeList()
			header := splitTableRow(trimmed)
			i += 2 // pula a linha de cabeçalho (já lida) e a linha separadora
			var rows [][]string
			for i < len(lines) {
				rowLine := strings.TrimRight(lines[i], " \t")
				if strings.TrimSpace(rowLine) == "" || !strings.Contains(rowLine, "|") {
					break
				}
				rows = append(rows, splitTableRow(rowLine))
				i++
			}
			i-- // compensa o i++ do for — a próxima iteração precisa reprocessar a linha em que paramos
			sb.WriteString(buildTeamsTableHTML(header, rows))
			continue
		}

		// Linha de corpo normal — acumula; linhas de conteúdo E linhas em branco se intercalam
		// no mesmo buffer, unidas por <br> ao fechar (ver comentário da função).
		closeList()
		bodySegments = append(bodySegments, applyInlineMarkdown(trimmed))
	}

	closeList()
	flushBody()
	return sb.String()
}

// plainTextToTeamsHTML converte texto literal (modo "Texto simples" do editor, sem interpretar
// nenhuma sintaxe Markdown) pra HTML aceito pelo Teams. Cada linha vira um segmento HTML-escapado,
// unido por `<br>` — mesmo modelo de `markdownToTeamsHTML` (preserva a contagem exata de linhas
// em branco), mas sem nenhuma das checagens de cabeçalho/lista/citação/negrito/etc., já que texto
// simples deve chegar exatamente como foi digitado (um `*` ou `#` aqui é só um caractere literal,
// nunca sintaxe).
func plainTextToTeamsHTML(text string) string {
	lines := strings.Split(text, "\n")
	segs := make([]string, len(lines))
	for i, line := range lines {
		segs[i] = htmlEscape(line)
	}
	// Mesmo trim de segmentos vazios do início/fim que flushBody faz — evita um <br> sobrando no
	// começo/fim vindo só de uma quebra de linha "estrutural" (o \n final do texto, por exemplo).
	start, end := 0, len(segs)
	for start < end && segs[start] == "" {
		start++
	}
	for end > start && segs[end-1] == "" {
		end--
	}
	segs = segs[start:end]
	if len(segs) == 0 {
		return ""
	}
	return "<p>" + strings.Join(segs, "<br>") + "</p>"
}

var (
	reBold1   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBold2   = regexp.MustCompile(`__(.+?)__`)
	reItalic1 = regexp.MustCompile(`\*(.+?)\*`)
	reItalic2 = regexp.MustCompile(`_(.+?)_`)
	reStrike  = regexp.MustCompile(`~~(.+?)~~`)
	reCode    = regexp.MustCompile("`(.+?)`")
	// reLink casa a sintaxe de link do Markdown ([texto](url)) — produzida por htmlToMarkdown a
	// partir de <a href> ao carregar uma mensagem via link (ver internal/teams/message_fetch.go).
	// Sem isso, um link carregado do Teams voltava como texto literal "[texto](url)" ao reenviar
	// em vez de um link clicável de verdade — parte do mesmo bug de "formatação alterada".
	reLink = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
	// reTableSepRow casa a linha separadora de uma tabela GFM (ex: "|---|:---:|---|",
	// "---|---" ou "-|-|-") — só traços/dois-pontos/pipes/espaços, com pelo menos um traço.
	// Ver comentário de detecção de tabela em markdownToTeamsHTML.
	reTableSepRow = regexp.MustCompile(`^\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?$`)
)

// splitTableRow separa uma linha de tabela Markdown em células: remove os pipes externos
// (opcionais em GFM) e respeita "\|" como pipe escapado dentro de uma célula.
func splitTableRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	const escPipe = "\x00ESCPIPE\x00"
	s = strings.ReplaceAll(s, `\|`, escPipe)
	parts := strings.Split(s, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.ReplaceAll(strings.TrimSpace(p), escPipe, "|")
	}
	return cells
}

// buildTeamsTableHTML monta um <table> HTML real a partir do cabeçalho e das linhas de dados já
// separados em células. Linhas com menos colunas que o cabeçalho preenchem as células faltantes
// com string vazia em vez de descartar a linha inteira — mais tolerante a tabelas digitadas à
// mão com contagem de colunas inconsistente entre linhas.
func buildTeamsTableHTML(header []string, rows [][]string) string {
	var sb strings.Builder
	sb.WriteString("<table><tr>")
	for _, h := range header {
		sb.WriteString("<th>" + applyInlineMarkdown(h) + "</th>")
	}
	sb.WriteString("</tr>")
	for _, row := range rows {
		sb.WriteString("<tr>")
		for i := range header {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			sb.WriteString("<td>" + applyInlineMarkdown(cell) + "</td>")
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</table>")
	return sb.String()
}

func applyInlineMarkdown(s string) string {
	s = reBold1.ReplaceAllString(s, "<b>$1</b>")
	s = reBold2.ReplaceAllString(s, "<b>$1</b>")
	s = reItalic1.ReplaceAllString(s, "<i>$1</i>")
	s = reItalic2.ReplaceAllString(s, "<i>$1</i>")
	s = reStrike.ReplaceAllString(s, "<s>$1</s>")
	s = reCode.ReplaceAllString(s, "<code>$1</code>")
	s = reLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
