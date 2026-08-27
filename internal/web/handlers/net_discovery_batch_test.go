package handlers

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeBatchTargets_TrimsAndSkipsEmpty(t *testing.T) {
	got := normalizeBatchTargets([]string{" 8.8.8.8 ", "", "   ", "example.com"}, false)
	want := []string{"8.8.8.8", "example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNormalizeBatchTargets_DedupeCaseInsensitivePreservesOrder cobre o achado documentado no
// comentário da função: "Foo.com" e "foo.com" não podem virar dois alvos diferentes no mesmo
// lote — a PRIMEIRA grafia encontrada é a que sobrevive, ordem de entrada preservada.
func TestNormalizeBatchTargets_DedupeCaseInsensitivePreservesOrder(t *testing.T) {
	got := normalizeBatchTargets([]string{"Example.com", "8.8.8.8", "example.COM", "1.1.1.1"}, false)
	want := []string{"Example.com", "8.8.8.8", "1.1.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeBatchTargets_EmptyInputReturnsEmptyNotNil(t *testing.T) {
	got := normalizeBatchTargets(nil, false)
	if got == nil {
		t.Fatal("esperava slice vazio, não nil")
	}
	if len(got) != 0 {
		t.Errorf("esperava 0 alvos, veio %d", len(got))
	}
}

// TestNormalizeBatchTargets_AllowDuplicatesKeepsRepeatedTarget — Fase C do roadmap de maturidade
// profissional (modo de monitoramento contínuo): com allowDuplicates=true, o mesmo alvo repetido N
// vezes deve sobreviver TODAS as N vezes, na ordem — é o mecanismo que viabiliza "monitorar" como
// "lote com o mesmo alvo repetido", sem endpoint novo.
func TestNormalizeBatchTargets_AllowDuplicatesKeepsRepeatedTarget(t *testing.T) {
	got := normalizeBatchTargets([]string{"8.8.8.8", "8.8.8.8", "8.8.8.8"}, true)
	want := []string{"8.8.8.8", "8.8.8.8", "8.8.8.8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNormalizeBatchTargets_AllowDuplicatesStillTrimsAndSkipsEmpty garante que allowDuplicates=true
// não desliga as outras normalizações (trim/vazio) — só a dedupe.
func TestNormalizeBatchTargets_AllowDuplicatesStillTrimsAndSkipsEmpty(t *testing.T) {
	got := normalizeBatchTargets([]string{" 8.8.8.8 ", "", "  ", " 8.8.8.8 "}, true)
	want := []string{"8.8.8.8", "8.8.8.8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNormalizeConcurrency — Fase G do roadmap de maturidade profissional (paralelismo opcional).
func TestNormalizeConcurrency(t *testing.T) {
	if n, errCode, _ := normalizeConcurrency(0); errCode != "" || n != 1 {
		t.Errorf("normalizeConcurrency(0) = (%d, %q), want (1, \"\") — default sequencial", n, errCode)
	}
	if n, errCode, _ := normalizeConcurrency(3); errCode != "" || n != 3 {
		t.Errorf("normalizeConcurrency(3) = (%d, %q), want (3, \"\")", n, errCode)
	}
	if _, errCode, _ := normalizeConcurrency(-1); errCode != "INVALID_CONCURRENCY" {
		t.Errorf("normalizeConcurrency(-1) errCode = %q, want INVALID_CONCURRENCY", errCode)
	}
	if _, errCode, _ := normalizeConcurrency(netDiscoveryBatchConcurrencyMax + 1); errCode != "INVALID_CONCURRENCY" {
		t.Errorf("normalizeConcurrency(%d) errCode = %q, want INVALID_CONCURRENCY", netDiscoveryBatchConcurrencyMax+1, errCode)
	}
}

// TestValidateConcurrencyWithMonitor_RejectsParallelMonitoring — Fase G: monitoramento é uma série
// temporal, paralelismo não faz sentido semântico combinado com ele.
func TestValidateConcurrencyWithMonitor_RejectsParallelMonitoring(t *testing.T) {
	if errCode, _ := validateConcurrencyWithMonitor(true, 3); errCode != "INVALID_CONCURRENCY" {
		t.Errorf("validateConcurrencyWithMonitor(true, 3) errCode = %q, want INVALID_CONCURRENCY", errCode)
	}
	if errCode, _ := validateConcurrencyWithMonitor(true, 1); errCode != "" {
		t.Errorf("validateConcurrencyWithMonitor(true, 1) errCode = %q, want vazio (sequencial é compatível com monitoramento)", errCode)
	}
	if errCode, _ := validateConcurrencyWithMonitor(false, 3); errCode != "" {
		t.Errorf("validateConcurrencyWithMonitor(false, 3) errCode = %q, want vazio (lote normal paralelo é válido)", errCode)
	}
}

// TestNormalizeMonitorInterval — Fase C do roadmap de maturidade profissional.
func TestNormalizeMonitorInterval(t *testing.T) {
	if sec, errCode, _ := normalizeMonitorInterval(0); errCode != "" || sec != 0 {
		t.Errorf("normalizeMonitorInterval(0) = (%d, %q), want (0, \"\")", sec, errCode)
	}
	if sec, errCode, _ := normalizeMonitorInterval(15); errCode != "" || sec != 15 {
		t.Errorf("normalizeMonitorInterval(15) = (%d, %q), want (15, \"\")", sec, errCode)
	}
	if _, errCode, _ := normalizeMonitorInterval(-1); errCode != "INVALID_INTERVAL" {
		t.Errorf("normalizeMonitorInterval(-1) errCode = %q, want INVALID_INTERVAL", errCode)
	}
	if _, errCode, _ := normalizeMonitorInterval(netDiscoveryMonitorIntervalMaxSec + 1); errCode != "INVALID_INTERVAL" {
		t.Errorf("normalizeMonitorInterval(%d) errCode = %q, want INVALID_INTERVAL", netDiscoveryMonitorIntervalMaxSec+1, errCode)
	}
}

// TestComputeOverallTimeout_BatchWorstCaseCappedByBatchLimit cobre o cálculo do teto do LOTE
// inteiro (RunBatch) — soma do pior caso individual, capado por netDiscoveryBatchOverallTimeoutCap.
// Não testa RunBatch via HTTP (mesma convenção deste pacote — handlers são finos, testados via a
// lógica pura que delegam pra); só confirma a fórmula usada dentro dele.
func TestComputeOverallTimeout_BatchWorstCaseCappedByBatchLimit(t *testing.T) {
	numTargets := netDiscoveryBatchMaxTargets // pior caso: lote cheio
	perTarget := computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec, netDiscoveryProbeCountMax)
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
	documentedWorstCase := time.Duration(netDiscoveryBatchMaxTargets) * computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec, netDiscoveryProbeCountMax)
	if netDiscoveryBatchOverallTimeoutCap < documentedWorstCase {
		t.Errorf("netDiscoveryBatchOverallTimeoutCap (%v) é menor que o pior caso documentado (%d alvos × timeout máximo = %v) — alvos no fim de um lote cheio podem nunca chegar a rodar",
			netDiscoveryBatchOverallTimeoutCap, netDiscoveryBatchMaxTargets, documentedWorstCase)
	}
}
