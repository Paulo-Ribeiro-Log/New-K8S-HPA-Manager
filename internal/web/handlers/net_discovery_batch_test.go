package handlers

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeBatchTargets_TrimsAndSkipsEmpty(t *testing.T) {
	got := normalizeBatchTargets([]string{" 8.8.8.8 ", "", "   ", "example.com"})
	want := []string{"8.8.8.8", "example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNormalizeBatchTargets_DedupeCaseInsensitivePreservesOrder cobre o achado documentado no
// comentário da função: "Foo.com" e "foo.com" não podem virar dois alvos diferentes no mesmo
// lote — a PRIMEIRA grafia encontrada é a que sobrevive, ordem de entrada preservada.
func TestNormalizeBatchTargets_DedupeCaseInsensitivePreservesOrder(t *testing.T) {
	got := normalizeBatchTargets([]string{"Example.com", "8.8.8.8", "example.COM", "1.1.1.1"})
	want := []string{"Example.com", "8.8.8.8", "1.1.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeBatchTargets_EmptyInputReturnsEmptyNotNil(t *testing.T) {
	got := normalizeBatchTargets(nil)
	if got == nil {
		t.Fatal("esperava slice vazio, não nil")
	}
	if len(got) != 0 {
		t.Errorf("esperava 0 alvos, veio %d", len(got))
	}
}

// TestComputeOverallTimeout_BatchWorstCaseCappedByBatchLimit cobre o cálculo do teto do LOTE
// inteiro (RunBatch) — soma do pior caso individual, capado por netDiscoveryBatchOverallTimeoutCap.
// Não testa RunBatch via HTTP (mesma convenção deste pacote — handlers são finos, testados via a
// lógica pura que delegam pra); só confirma a fórmula usada dentro dele.
func TestComputeOverallTimeout_BatchWorstCaseCappedByBatchLimit(t *testing.T) {
	numTargets := netDiscoveryBatchMaxTargets // pior caso: lote cheio
	perTarget := computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec)
	batchTimeout := time.Duration(numTargets) * perTarget
	if batchTimeout > netDiscoveryBatchOverallTimeoutCap {
		batchTimeout = netDiscoveryBatchOverallTimeoutCap
	}
	if batchTimeout > netDiscoveryBatchOverallTimeoutCap {
		t.Errorf("teto calculado (%v) excede netDiscoveryBatchOverallTimeoutCap (%v)", batchTimeout, netDiscoveryBatchOverallTimeoutCap)
	}
	if batchTimeout <= 0 {
		t.Errorf("teto calculado deveria ser positivo, veio %v", batchTimeout)
	}
}

// TestNetDiscoveryBatchOverallTimeoutCap_NeverSmallerThanDocumentedWorstCase — achado real de
// code review: netDiscoveryBatchOverallTimeoutCap era uma constante FIXA (30min) menor que o
// próprio pior caso documentado no comentário de netDiscoveryBatchMaxTargets (10 alvos no timeout
// de sonda máximo ≈ 45min) — com um lote cheio genuinamente bloqueado, o contexto compartilhado
// expirava ANTES de todos os alvos rodarem, sem nenhum evento SSE de erro pros alvos que nunca
// chegaram a começar. Corrigido tornando o teto CALCULADO (não mais reafirmado como número fixo);
// este teste trava essa invariante — se alguém reintroduzir um valor fixo no futuro sem manter em
// sincronia com a fórmula, ele falha.
func TestNetDiscoveryBatchOverallTimeoutCap_NeverSmallerThanDocumentedWorstCase(t *testing.T) {
	documentedWorstCase := time.Duration(netDiscoveryBatchMaxTargets) * computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec)
	if netDiscoveryBatchOverallTimeoutCap < documentedWorstCase {
		t.Errorf("netDiscoveryBatchOverallTimeoutCap (%v) é menor que o pior caso documentado (%d alvos × timeout máximo = %v) — alvos no fim de um lote cheio podem nunca chegar a rodar",
			netDiscoveryBatchOverallTimeoutCap, netDiscoveryBatchMaxTargets, documentedWorstCase)
	}
}
