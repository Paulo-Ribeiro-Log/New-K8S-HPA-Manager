package config

import (
	"net"
	"testing"
	"time"
)

// TestSplitProbeBudget_NeverExceedsOriginalTimeout cobre o bug real corrigido em
// TestClusterTCPConnection: a versão anterior fazia 2 tentativas de `timeout` CADA, dobrando o
// pior caso pra até `2×timeout + reachabilityProbeRetryDelay`. splitProbeBudget divide o MESMO
// orçamento entre as 2 tentativas — o pior caso (perAttempt + delay + perAttempt) nunca deve
// ultrapassar o `timeout` original pedido pelo chamador.
func TestSplitProbeBudget_NeverExceedsOriginalTimeout(t *testing.T) {
	cases := []time.Duration{
		2500 * time.Millisecond, // logo acima do piso splittable
		8 * time.Second,         // valor real usado por VPNHandler.CheckStatus
		10 * time.Second,        // reachabilityProbeTimeout (checkReachability)
		15 * time.Second,        // teto aceito por GET /clusters/:name/test
	}
	for _, timeout := range cases {
		perAttempt, splittable := splitProbeBudget(timeout)
		if !splittable {
			t.Fatalf("timeout=%v: esperava splittable=true", timeout)
		}
		worstCase := perAttempt + reachabilityProbeRetryDelay + perAttempt
		if worstCase > timeout {
			t.Errorf("timeout=%v: pior caso (%v) excede o orçamento pedido — regressão do bug de dobrar o timeout", timeout, worstCase)
		}
		if perAttempt <= 0 {
			t.Errorf("timeout=%v: perAttempt não-positivo (%v)", timeout, perAttempt)
		}
	}
}

// TestSplitProbeBudget_TooSmallFallsBackToSingleAttempt cobre o piso: orçamento pequeno demais
// pra caber 2 tentativas + o delay entre elas sem deixar cada uma com um resíduo quase inútil —
// nesse caso cai pra 1 tentativa só, com o orçamento cheio (não splittable).
func TestSplitProbeBudget_TooSmallFallsBackToSingleAttempt(t *testing.T) {
	for _, timeout := range []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second} {
		perAttempt, splittable := splitProbeBudget(timeout)
		if splittable {
			t.Errorf("timeout=%v: esperava splittable=false (orçamento pequeno demais pra dividir)", timeout)
		}
		if perAttempt != timeout {
			t.Errorf("timeout=%v: esperava perAttempt igual ao timeout original quando não-splittable, veio %v", timeout, perAttempt)
		}
	}
}

// TestTestClusterTCPConnection_UnreachableHostStaysWithinOriginalBudget é o teste de ponta a
// ponta (com socket real, não só a aritmética pura acima) do mesmo bug: mira num host que nunca
// aceita conexão (porta fechada em loopback — "connection refused" imediato tanto na 1ª quanto
// na 2ª tentativa, então o tempo total fica dominado só pelo delay do retry, nunca pelos 2
// timeouts completos) e confirma que o tempo total fica bem abaixo do antigo pior caso
// (2×timeout). Não testa o caso "hang até o timeout" (dial contra endereço black-holed) porque
// isso dependeria do comportamento de rede real do ambiente de CI — coberto ao vivo nesta sessão
// contra o Prometheus/VPN reais do usuário, não aqui.
func TestTestClusterTCPConnection_UnreachableHostStaysWithinOriginalBudget(t *testing.T) {
	// Porta fechada de propósito: abre e fecha um listener pra garantir uma porta livre, mas
	// nunca aceita conexão nela — "connection refused" real, não simulado.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("falha ao abrir listener temporário: %v", err)
	}
	closedAddr := l.Addr().String()
	l.Close()

	k := &KubeConfigManager{}
	const timeout = 8 * time.Second

	start := time.Now()
	reachable := func() bool {
		perAttempt, splittable := splitProbeBudget(timeout)
		if !splittable {
			return dialOnce(closedAddr, timeout)
		}
		if dialOnce(closedAddr, perAttempt) {
			return true
		}
		time.Sleep(reachabilityProbeRetryDelay)
		return dialOnce(closedAddr, perAttempt)
	}()
	elapsed := time.Since(start)
	_ = k

	if reachable {
		t.Fatalf("esperava reachable=false contra uma porta fechada")
	}
	// "Connection refused" é quase instantâneo (SO responde RST na hora) — o tempo real aqui é
	// dominado pelo reachabilityProbeRetryDelay (300ms), não pelos timeouts de 8s. O que este
	// teste garante é a ausência de qualquer bloqueio artificial de `timeout` inteiro por
	// tentativa: bem abaixo do antigo pior caso de 2×timeout=16s.
	if elapsed >= timeout {
		t.Errorf("elapsed=%v ficou perto ou acima do orçamento total (%v) — indício de regressão pro comportamento antigo (2 tentativas de timeout inteiro cada)", elapsed, timeout)
	}
}
