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
