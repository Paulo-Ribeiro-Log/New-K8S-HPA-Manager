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
}

func newWatchSession(tracker *sse.ProgressTracker) *watchSession {
	return &watchSession{tracker: tracker}
}

// start gera um sessionID, registra o cancelFunc e roda `run` em background — `run` deve
// bloquear até o contexto ser cancelado (ex: informer.Run(ctx.Done())) e NÃO precisa publicar o
// evento "complete" nem limpar cancelFuncs sozinho, start() já faz isso ao `run` retornar (mesmo
// padrão de pods_watch.go's runWatch).
func (s *watchSession) start(run func(ctx context.Context, sessionID string)) string {
	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFuncs.Store(sessionID, cancel)

	go func() {
		run(ctx, sessionID)
		// Contexto cancelado (aba fechada, ou Cancel() explícito) — sinaliza fim pro handler de
		// Stream fechar a conexão HTTP em vez de ficar pendurado.
		s.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID: sessionID, Type: "complete", Phase: "completed", Timestamp: time.Now(),
		})
		s.cancelFuncs.Delete(sessionID)
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
		s.cancelFuncs.Delete(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}
