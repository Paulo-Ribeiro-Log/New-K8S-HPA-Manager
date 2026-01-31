package sse

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ProgressEvent representa um evento de progresso enviado via SSE
type ProgressEvent struct {
	ID        string    `json:"id"`         // ID da operação (ex: "nodepool-prod-pool-1234")
	Type      string    `json:"type"`       // "cordon" | "drain" | "azure" | "complete" | "error" | "init" | "deployments" | "services" | "configs" | "summary"
	Phase     string    `json:"phase"`      // "started" | "in_progress" | "completed" | "failed"
	Message   string    `json:"message"`    // Mensagem descritiva
	Progress  float64   `json:"progress"`   // 0.0 - 1.0
	Details   string    `json:"details"`    // Detalhes adicionais
	Status    string    `json:"status"`     // "healthy" | "warning" | "critical" | "unknown" (para health checking)
	Timestamp time.Time `json:"timestamp"`  // Timestamp do evento
	NodeName  string    `json:"node_name"`  // Nome do node sendo processado (se aplicável)
	PodsCount int       `json:"pods_count"` // Quantidade de pods (se aplicável)
	Error     string    `json:"error"`      // Mensagem de erro (se Type == "error")
}

// Client representa um cliente SSE conectado
type Client struct {
	ID       string
	Channel  chan ProgressEvent
	IsClosed bool
	mu       sync.Mutex
}

// NewClient cria um novo cliente SSE
func NewClient(id string) *Client {
	return &Client{
		ID:       id,
		Channel:  make(chan ProgressEvent, 500), // Buffer de 500 eventos (suporta múltiplos clusters simultâneos)
		IsClosed: false,
	}
}

// Send envia um evento para o cliente
func (c *Client) Send(event ProgressEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.IsClosed {
		return
	}

	select {
	case c.Channel <- event:
		// Evento enviado com sucesso
	default:
		// Canal cheio, descartar evento mais antigo (não bloquear)
		// ⚠️ CRÍTICO: Log quando evento for descartado - pode causar travamento no frontend!
		log.Warn().
			Str("client_id", c.ID).
			Str("event_type", event.Type).
			Str("event_phase", event.Phase).
			Float64("progress", event.Progress).
			Int("buffer_size", cap(c.Channel)).
			Msg("[SSE] Canal cheio! Evento descartado - frontend pode travar se for 'complete'")
	}
}

// Close fecha o canal do cliente
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsClosed {
		c.IsClosed = true
		close(c.Channel)
	}
}

// ReplayBuffer armazena eventos para replay quando cliente reconecta
type ReplayBuffer struct {
	events    []ProgressEvent
	maxEvents int
	mu        sync.RWMutex
}

// NewReplayBuffer cria um novo buffer com tamanho máximo
func NewReplayBuffer(maxEvents int) *ReplayBuffer {
	return &ReplayBuffer{
		events:    make([]ProgressEvent, 0, maxEvents),
		maxEvents: maxEvents,
	}
}

// Add adiciona um evento ao buffer (FIFO, remove mais antigo se cheio)
func (rb *ReplayBuffer) Add(event ProgressEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if len(rb.events) >= rb.maxEvents {
		// Remover evento mais antigo (primeiro)
		rb.events = rb.events[1:]
	}
	rb.events = append(rb.events, event)
}

// GetAll retorna cópia de todos os eventos no buffer
func (rb *ReplayBuffer) GetAll() []ProgressEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Retornar cópia para evitar race conditions
	result := make([]ProgressEvent, len(rb.events))
	copy(result, rb.events)
	return result
}

// Clear limpa todos os eventos do buffer
func (rb *ReplayBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.events = rb.events[:0]
}

// ProgressTracker gerencia múltiplos clientes SSE e rastreia progresso de operações
type ProgressTracker struct {
	clients       map[string]*Client
	replayBuffers map[string]*ReplayBuffer // Buffer por sessão
	mu            sync.RWMutex
	bufferSize    int // Tamanho máximo do buffer (default: 100)
}

// NewProgressTracker cria um novo tracker de progresso
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		clients:       make(map[string]*Client),
		replayBuffers: make(map[string]*ReplayBuffer),
		bufferSize:    100, // Últimos 100 eventos por sessão
	}
}

// AddClient registra um novo cliente SSE
func (pt *ProgressTracker) AddClient(client *Client) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.clients[client.ID] = client
}

// RemoveClient remove um cliente SSE
func (pt *ProgressTracker) RemoveClient(clientID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if client, exists := pt.clients[clientID]; exists {
		client.Close()
		delete(pt.clients, clientID)
	}
}

// GetClient retorna um cliente pelo ID
func (pt *ProgressTracker) GetClient(clientID string) (*Client, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	client, exists := pt.clients[clientID]
	return client, exists
}

// Broadcast envia um evento para todos os clientes
func (pt *ProgressTracker) Broadcast(event ProgressEvent) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	for _, client := range pt.clients {
		client.Send(event)
	}
}

// SendToClient envia um evento para um cliente específico
// Também salva no replay buffer para permitir reconexão sem perda de eventos
func (pt *ProgressTracker) SendToClient(clientID string, event ProgressEvent) {
	// ✅ SEMPRE salvar no replay buffer (mesmo se cliente não conectou ainda)
	pt.addToReplayBuffer(clientID, event)

	client, exists := pt.GetClient(clientID)
	if exists {
		client.Send(event)
	} else {
		// Cliente não encontrado - evento salvo no buffer para replay posterior
		log.Debug().
			Str("client_id", clientID).
			Str("event_type", event.Type).
			Float64("progress", event.Progress).
			Msg("[SSE] Cliente não conectado - evento salvo no replay buffer")
	}

	// Limpar buffer se evento é de conclusão
	if event.Type == "complete" || event.Type == "error" {
		// Agendar limpeza após 5 minutos (tempo para cliente buscar histórico)
		go func() {
			time.Sleep(5 * time.Minute)
			pt.ClearReplayBuffer(clientID)
		}()
	}
}

// addToReplayBuffer adiciona evento ao buffer da sessão
func (pt *ProgressTracker) addToReplayBuffer(sessionID string, event ProgressEvent) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	buffer, exists := pt.replayBuffers[sessionID]
	if !exists {
		buffer = NewReplayBuffer(pt.bufferSize)
		pt.replayBuffers[sessionID] = buffer
	}
	buffer.Add(event)
}

// GetReplayEvents retorna todos os eventos do buffer para uma sessão
func (pt *ProgressTracker) GetReplayEvents(sessionID string) []ProgressEvent {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	buffer, exists := pt.replayBuffers[sessionID]
	if !exists {
		return []ProgressEvent{}
	}
	return buffer.GetAll()
}

// ClearReplayBuffer limpa o buffer de uma sessão
func (pt *ProgressTracker) ClearReplayBuffer(sessionID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if buffer, exists := pt.replayBuffers[sessionID]; exists {
		buffer.Clear()
		delete(pt.replayBuffers, sessionID)
		log.Debug().Str("session_id", sessionID).Msg("[SSE] Replay buffer limpo")
	}
}

// ProgressReporter é uma interface helper para enviar eventos de progresso
type ProgressReporter struct {
	OperationID string
	Tracker     *ProgressTracker
}

// NewProgressReporter cria um novo reporter
func NewProgressReporter(operationID string, tracker *ProgressTracker) *ProgressReporter {
	return &ProgressReporter{
		OperationID: operationID,
		Tracker:     tracker,
	}
}

// SendCordonStarted envia evento de início do CORDON
func (pr *ProgressReporter) SendCordonStarted(totalNodes int) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "cordon",
		Phase:     "started",
		Message:   fmt.Sprintf("Iniciando CORDON de %d nodes...", totalNodes),
		Progress:  0.0,
		Timestamp: time.Now(),
	})
}

// SendCordonProgress envia progresso do CORDON
func (pr *ProgressReporter) SendCordonProgress(nodeName string, current, total int) {
	progress := float64(current) / float64(total)
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "cordon",
		Phase:     "in_progress",
		Message:   fmt.Sprintf("CORDON: %d/%d nodes", current, total),
		Progress:  progress * 0.2, // CORDON = 0-20% do progresso total
		Details:   fmt.Sprintf("Node: %s", nodeName),
		Timestamp: time.Now(),
		NodeName:  nodeName,
	})
}

// SendCordonCompleted envia evento de conclusão do CORDON
func (pr *ProgressReporter) SendCordonCompleted(totalNodes int) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "cordon",
		Phase:     "completed",
		Message:   fmt.Sprintf("✅ CORDON concluído: %d nodes marcados como unschedulable", totalNodes),
		Progress:  0.2, // 20%
		Timestamp: time.Now(),
	})
}

// SendDrainStarted envia evento de início do DRAIN
func (pr *ProgressReporter) SendDrainStarted(totalNodes int) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "drain",
		Phase:     "started",
		Message:   fmt.Sprintf("Iniciando DRAIN de %d nodes...", totalNodes),
		Progress:  0.2, // Começa após CORDON (20%)
		Timestamp: time.Now(),
	})
}

// SendDrainProgress envia progresso do DRAIN
func (pr *ProgressReporter) SendDrainProgress(nodeName string, current, total, podsCount int) {
	// DRAIN = 20-80% do progresso total (60% do espaço)
	nodeProgress := float64(current) / float64(total)
	progress := 0.2 + (nodeProgress * 0.6)

	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "drain",
		Phase:     "in_progress",
		Message:   fmt.Sprintf("DRAIN: %d/%d nodes", current, total),
		Progress:  progress,
		Details:   fmt.Sprintf("Node: %s | Pods evacuados: %d", nodeName, podsCount),
		Timestamp: time.Now(),
		NodeName:  nodeName,
		PodsCount: podsCount,
	})
}

// SendDrainCompleted envia evento de conclusão do DRAIN
func (pr *ProgressReporter) SendDrainCompleted(totalNodes, totalPodsEvicted int) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "drain",
		Phase:     "completed",
		Message:   fmt.Sprintf("✅ DRAIN concluído: %d nodes evacuados, %d pods realocados", totalNodes, totalPodsEvicted),
		Progress:  0.8, // 80%
		Timestamp: time.Now(),
		PodsCount: totalPodsEvicted,
	})
}

// SendAzureStarted envia evento de início da operação Azure
func (pr *ProgressReporter) SendAzureStarted() {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "azure",
		Phase:     "started",
		Message:   "Aplicando mudanças via Azure CLI...",
		Progress:  0.8, // Começa após DRAIN (80%)
		Timestamp: time.Now(),
	})
}

// SendAzureProgress envia progresso da operação Azure
func (pr *ProgressReporter) SendAzureProgress(message string) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "azure",
		Phase:     "in_progress",
		Message:   message,
		Progress:  0.9, // 90%
		Timestamp: time.Now(),
	})
}

// SendAzureCompleted envia evento de conclusão da operação Azure
func (pr *ProgressReporter) SendAzureCompleted() {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "azure",
		Phase:     "completed",
		Message:   "✅ Mudanças aplicadas no Azure",
		Progress:  0.95, // 95%
		Timestamp: time.Now(),
	})
}

// SendComplete envia evento de conclusão total da operação
func (pr *ProgressReporter) SendComplete() {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "complete",
		Phase:     "completed",
		Message:   "✅ Operação concluída com sucesso",
		Progress:  1.0, // 100%
		Timestamp: time.Now(),
	})
}

// SendError envia evento de erro
func (pr *ProgressReporter) SendError(errorType, errorMessage string) {
	pr.send(ProgressEvent{
		ID:        pr.OperationID,
		Type:      "error",
		Phase:     "failed",
		Message:   fmt.Sprintf("❌ Erro durante %s", errorType),
		Error:     errorMessage,
		Timestamp: time.Now(),
	})
}

// send é um helper interno para enviar eventos
func (pr *ProgressReporter) send(event ProgressEvent) {
	pr.Tracker.Broadcast(event)
}

// FormatSSE formata um ProgressEvent como mensagem SSE
func FormatSSE(event ProgressEvent) string {
	data, err := json.Marshal(event)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("data: %s\n\n", string(data))
}
