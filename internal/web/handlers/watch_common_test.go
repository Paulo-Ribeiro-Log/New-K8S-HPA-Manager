package handlers

import (
	"context"
	"testing"
	"time"

	"k8s-hpa-manager/internal/web/sse"
)

func newTestWatchSession(grace time.Duration) *watchSession {
	s := newWatchSession(sse.NewProgressTracker())
	s.grace = grace
	return s
}

// startBlocking inicia uma sessão cujo "informer" só termina quando o contexto é cancelado.
func startBlocking(s *watchSession) (string, <-chan struct{}) {
	done := make(chan struct{})
	id := s.start(func(ctx context.Context, _ string) {
		<-ctx.Done()
		close(done)
	})
	return id, done
}

func waitDone(t *testing.T, done <-chan struct{}, within time.Duration) bool {
	t.Helper()
	select {
	case <-done:
		return true
	case <-time.After(within):
		return false
	}
}

func TestWatchSessionCancelaAposDesconexaoDoStream(t *testing.T) {
	s := newTestWatchSession(50 * time.Millisecond)
	id, done := startBlocking(s)
	s.attach(id)
	s.detach(id)
	if !waitDone(t, done, 2*time.Second) {
		t.Fatal("watch órfão deveria ser cancelado depois do grace")
	}
	// o forget roda logo depois que o "informer" retorna: espera um instante em vez de checar na mesma hora
	limpo := false
	for i := 0; i < 100 && !limpo; i++ {
		s.mu.Lock()
		_, temEstado := s.lastSeen[id]
		s.mu.Unlock()
		_, temCancel := s.cancelFuncs.Load(id)
		limpo = !temEstado && !temCancel
		if !limpo {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !limpo {
		t.Error("estado da sessão deveria ter sido limpo")
	}
}

func TestWatchSessionReconexaoDentroDoGraceMantemOWatch(t *testing.T) {
	s := newTestWatchSession(150 * time.Millisecond)
	id, done := startBlocking(s)
	s.attach(id)
	s.detach(id)
	time.Sleep(50 * time.Millisecond)
	s.attach(id) // EventSource reconectou
	if waitDone(t, done, 400*time.Millisecond) {
		t.Fatal("com stream conectado o watch não pode ser cancelado")
	}
	s.detach(id)
	if !waitDone(t, done, 2*time.Second) {
		t.Fatal("depois de desconectar de vez, deveria cancelar")
	}
}

func TestWatchSessionSemNenhumStreamTambemEOrfa(t *testing.T) {
	s := newTestWatchSession(50 * time.Millisecond)
	_, done := startBlocking(s)
	if !waitDone(t, done, 2*time.Second) {
		t.Fatal("sessão que nunca recebeu stream deveria ser cancelada")
	}
}

func TestWatchSessionStreamAtivoNuncaCancela(t *testing.T) {
	s := newTestWatchSession(30 * time.Millisecond)
	id, done := startBlocking(s)
	s.attach(id)
	if waitDone(t, done, 300*time.Millisecond) {
		t.Fatal("stream conectado: não pode cancelar")
	}
	s.detach(id)
	waitDone(t, done, 2*time.Second)
}
