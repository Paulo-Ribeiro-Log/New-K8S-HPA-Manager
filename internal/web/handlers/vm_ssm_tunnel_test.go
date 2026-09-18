package handlers

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWaitTunnelReady_RetornaAssimQueSinalDePrDeAparece — bug real corrigido: a versão anterior
// (waitLocalPortOpen) considerava o túnel pronto assim que a porta local aceitava TCP, mas o
// plugin `session-manager-plugin` abre esse listener ANTES do canal SSM ponta a ponta terminar de
// se estabelecer — conectar nessa janela derrubava a conexão com EOF no meio do handshake SSH
// ("erro no handshake SSH com ssm-tunnel-...: ssh: handshake failed: EOF", relatado pelo usuário).
// Este teste confirma que waitTunnelReady só retorna sucesso quando a saída real do `aws ssm`
// contém a linha de prontidão documentada da AWS ("Waiting for connections..."), nunca antes.
func TestWaitTunnelReady_RetornaAssimQueSinalDeProntidaoAparece(t *testing.T) {
	buf := &syncBuffer{}
	_, _ = buf.Write([]byte("Starting session with SessionId: abc\n"))
	_, _ = buf.Write([]byte("Port 12345 opened for sessionId abc.\n"))

	done := make(chan error, 1)
	go func() {
		done <- waitTunnelReady(context.Background(), 12345, buf, 2*time.Second)
	}()

	// Ainda sem a linha de prontidão — waitTunnelReady não deve retornar ainda.
	select {
	case err := <-done:
		t.Fatalf("waitTunnelReady retornou (err=%v) antes da linha de prontidão aparecer", err)
	case <-time.After(400 * time.Millisecond):
		// esperado: continua esperando
	}

	_, _ = buf.Write([]byte("Waiting for connections...\n"))

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitTunnelReady deveria ter retornado nil após a linha de prontidão, veio: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitTunnelReady nunca retornou mesmo depois da linha de prontidão aparecer")
	}
}

func TestWaitTunnelReady_TimeoutSeSinalNuncaAparece(t *testing.T) {
	buf := &syncBuffer{}
	_, _ = buf.Write([]byte("Starting session with SessionId: abc\nPort 12345 opened for sessionId abc.\n"))

	err := waitTunnelReady(context.Background(), 12345, buf, 300*time.Millisecond)
	if err == nil {
		t.Fatal("esperado erro de timeout — a linha de prontidão nunca apareceu")
	}
}

func TestWaitTunnelReady_RespeitaCancelamentoDeContexto(t *testing.T) {
	buf := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := waitTunnelReady(ctx, 12345, buf, 5*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperado erro — contexto já cancelado")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitTunnelReady deveria ter retornado quase imediatamente com contexto já cancelado, levou %v", elapsed)
	}
}

// TestSyncBuffer_SemRaceEmEscritaConcorrente — motivo real de existir (ver comentário de
// syncBuffer): bytes.Buffer cru não é seguro pra leitura (String()) concorrente com escrita
// (Write(), como o pacote exec faz internamente ao copiar do pipe do subprocesso) — rodar este
// teste com -race confirma que não há data race entre as duas operações.
func TestSyncBuffer_SemRaceEmEscritaConcorrente(t *testing.T) {
	buf := &syncBuffer{}
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = buf.Write([]byte("x"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = buf.String()
		}
	}()
	wg.Wait()
}
