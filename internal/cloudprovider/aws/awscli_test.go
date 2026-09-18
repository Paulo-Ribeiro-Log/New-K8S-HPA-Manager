package aws

import (
	"context"
	"testing"
	"time"
)

// TestProfileSemaphore_SerializaAcessoMesmoProfile confirma que duas chamadas concorrentes pro
// mesmo profile nunca detém o lock ao mesmo tempo — mesmo racional do bug real corrigido em
// awscli.go (contenção da AWS CLI v2 no cache SQLite de sessão SSO, session.db, quando múltiplas
// invocações compartilham profile).
func TestProfileSemaphore_SerializaAcessoMesmoProfile(t *testing.T) {
	sem := newProfileSemaphore()

	if err := sem.Lock(context.Background()); err != nil {
		t.Fatalf("1º Lock falhou: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		if err := sem.Lock(context.Background()); err != nil {
			return
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("2ª goroutine conseguiu o lock enquanto a 1ª ainda o segurava — não está serializando")
	case <-time.After(100 * time.Millisecond):
		// esperado: 2ª goroutine continua bloqueada
	}

	sem.Unlock()

	select {
	case <-acquired:
		// esperado: 2ª goroutine destrava assim que o lock é liberado
	case <-time.After(2 * time.Second):
		t.Fatal("2ª goroutine nunca conseguiu o lock depois do Unlock()")
	}
}

// TestProfileSemaphore_RespeitaCancelamentoDeContexto — bug real corrigido na 1ª versão desta
// correção (achado validando ao vivo, antes de considerar pronto): usar um *sync.Mutex cru fazia
// uma chamada cujo contexto já tinha expirado continuar bloqueada esperando o lock mesmo assim,
// só falhando DEPOIS de finalmente conseguir o lock — desperdiçando a fila de espera inteira com
// trabalho que já tinha nascido morto. Este teste trava esse comportamento: uma chamada com
// contexto cancelado nunca deve continuar bloqueada além do cancelamento.
func TestProfileSemaphore_RespeitaCancelamentoDeContexto(t *testing.T) {
	sem := newProfileSemaphore()
	if err := sem.Lock(context.Background()); err != nil {
		t.Fatalf("1º Lock falhou: %v", err)
	}
	defer sem.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sem.Lock(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Lock deveria ter falhado — o lock já está retido por outra chamada e o contexto expira antes de liberar")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Lock ficou bloqueado por %v mesmo com contexto de 50ms — não está respeitando o cancelamento", elapsed)
	}
}

// TestAwsCLIProfileLock_MesmoProfileMesmoSemaforo confirma que dois profiles DIFERENTES nunca
// competem entre si (só chamadas pro MESMO profile devem serializar) — evita uma regressão onde
// todo mundo cai num único lock global, serializando à toa chamadas que nem compartilham recurso.
func TestAwsCLIProfileLock_MesmoProfileMesmoSemaforo(t *testing.T) {
	// Limpa o mapa global entre execuções de teste (evita interferência de outros testes do
	// mesmo pacote rodando em paralelo/sequência sobre o mesmo estado package-level).
	awsCLIProfileLocksMu.Lock()
	awsCLIProfileLocks = map[string]profileSemaphore{}
	awsCLIProfileLocksMu.Unlock()

	a1 := awsCLIProfileLock("perfil-a")
	a2 := awsCLIProfileLock("perfil-a")
	b1 := awsCLIProfileLock("perfil-b")

	if a1 != a2 {
		t.Fatal("duas chamadas pro mesmo profile deveriam reaproveitar o MESMO semáforo")
	}
	if a1 == b1 {
		t.Fatal("profiles diferentes nunca deveriam compartilhar o mesmo semáforo")
	}
}
