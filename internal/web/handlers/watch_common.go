package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"k8s-hpa-manager/internal/web/sse"
)

// watchSession é o esqueleto compartilhado (sessão + Stream/Cancel SSE) por trás de todo Watch
// desta app (Pods, Deployments, HPAs) — extraído de pods_watch.go (o piloto original) quando a
// mesma infraestrutura passou a ser reaproveitada por outros recursos. `Stream`/`Cancel` são
// genéricos de verdade (o sessionID já é um UUID globalmente único, sem relação com o tipo de
// recurso) — por isso uma ÚNICA instância compartilhada atende as rotas dos 3 recursos, só o
// `Start` de cada um precisa ser específico (monta o cache.ListWatch e o conversor certos).
type watchSession struct {
	tracker     *sse.ProgressTracker
	cancelFuncs sync.Map // sessionID -> context.CancelFunc

	// Controle de watch órfão: o browser fecha a aba, dá F5 ou perde a rede sem avisar (o cancelamento do frontend
	// no unmount não roda), e o Informer ficaria vivo até o servidor reiniciar, guardando todos os objetos em memória
	// e mantendo uma conexão aberta com a API do cluster.
	grace    time.Duration
	mu       sync.Mutex
	streams  map[string]int       // conexões SSE ativas por sessão
	lastSeen map[string]time.Time // última vez que a sessão foi criada/conectada/desconectada
}

// watchOrphanGrace é quanto tempo uma sessão pode ficar sem nenhuma conexão SSE antes de ser cancelada. Cobre a
// reconexão automática do EventSource (alguns segundos) e o intervalo entre o POST de início e o GET do stream.
const watchOrphanGrace = 30 * time.Second

func newWatchSession(tracker *sse.ProgressTracker) *watchSession {
	return &watchSession{
		tracker:  tracker,
		grace:    watchOrphanGrace,
		streams:  map[string]int{},
		lastSeen: map[string]time.Time{},
	}
}

// attach/detach contam as conexões SSE de uma sessão; ao zerar, agenda a checagem de órfão.
func (s *watchSession) attach(sessionID string) {
	s.mu.Lock()
	s.streams[sessionID]++
	s.lastSeen[sessionID] = time.Now()
	s.mu.Unlock()
}

func (s *watchSession) detach(sessionID string) {
	s.mu.Lock()
	if s.streams[sessionID] > 0 {
		s.streams[sessionID]--
	}
	s.lastSeen[sessionID] = time.Now()
	s.mu.Unlock()
	time.AfterFunc(s.grace, func() { s.cancelIfOrphan(sessionID) })
}

// cancelIfOrphan cancela o watch se a sessão está sem conexão SSE há pelo menos `grace`. Se alguém reconectou (ou a
// última desconexão foi mais recente), não faz nada: existe outra checagem agendada pela desconexão mais nova.
func (s *watchSession) cancelIfOrphan(sessionID string) {
	s.mu.Lock()
	seen, known := s.lastSeen[sessionID]
	orphan := known && s.streams[sessionID] == 0 && time.Since(seen) >= s.grace
	s.mu.Unlock()
	if !orphan {
		return
	}
	if val, ok := s.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
	}
}

// forget apaga o estado da sessão quando ela termina.
func (s *watchSession) forget(sessionID string) {
	s.cancelFuncs.Delete(sessionID)
	s.mu.Lock()
	delete(s.streams, sessionID)
	delete(s.lastSeen, sessionID)
	s.mu.Unlock()
}

// start gera um sessionID, registra o cancelFunc e roda `run` em background — `run` deve
// bloquear até o contexto ser cancelado (ex: informer.Run(ctx.Done())) e NÃO precisa publicar o
// evento "complete" nem limpar cancelFuncs sozinho, start() já faz isso ao `run` retornar (mesmo
// padrão de pods_watch.go's runWatch).
func (s *watchSession) start(run func(ctx context.Context, sessionID string)) string {
	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFuncs.Store(sessionID, cancel)
	s.mu.Lock()
	s.lastSeen[sessionID] = time.Now()
	s.mu.Unlock()
	// Sessão que nunca recebeu um stream (cliente caiu entre o POST e o GET) também é órfã.
	time.AfterFunc(s.grace, func() { s.cancelIfOrphan(sessionID) })

	go func() {
		run(ctx, sessionID)
		// Contexto cancelado (aba fechada, ou Cancel() explícito) — sinaliza fim pro handler de
		// Stream fechar a conexão HTTP em vez de ficar pendurado.
		s.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID: sessionID, Type: "complete", Phase: "completed", Timestamp: time.Now(),
		})
		s.forget(sessionID)
	}()

	return sessionID
}

// Stream conecta o cliente ao fluxo SSE de um Watch em andamento — idêntico pros 3 recursos, o
// loop nunca para sozinho em eventos "<recurso>_*" (só em "complete"/"error"), já que o Watch é
// contínuo por natureza.
func (s *watchSession) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range s.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	s.tracker.AddClient(client)
	defer s.tracker.RemoveClient(sessionID)
	s.attach(sessionID)
	defer s.detach(sessionID)

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

// Cancel para o Watch de uma sessão — cancela o contexto compartilhado pelo Informer, que
// desbloqueia sozinho o `run` (ex: informer.Run(ctx.Done())) passado em start().
func (s *watchSession) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	if val, ok := s.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
		s.forget(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}
