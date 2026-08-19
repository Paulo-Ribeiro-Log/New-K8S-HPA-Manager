package spinnaker

import (
	"strings"
	"testing"
)

// TestStage_FailureLog_TopLevelException reproduz o formato real de falha confirmado ao vivo
// (squad "Inteligência Comercial", application "pricing", stage deployManifest com timeout):
// exceção no nível raiz do context, com stackTrace Java completo.
func TestStage_FailureLog_TopLevelException(t *testing.T) {
	stage := Stage{
		Name:   "Patch (Manifest)",
		Status: "TERMINAL",
		Context: []byte(`{
			"exception": {
				"exceptionType": "TimeoutException",
				"operation": "waitForManifestToStabilize",
				"details": {
					"error": "Unexpected Task Failure",
					"errors": ["Stage Patch (Manifest) timed out after 30 minutes"],
					"stackTrace": "com.netflix.spinnaker.orca.exceptions.TimeoutException: ..."
				}
			}
		}`),
	}

	log := stage.FailureLog()
	if !strings.Contains(log, "TimeoutException") {
		t.Errorf("log não contém o tipo da exceção: %q", log)
	}
	if !strings.Contains(log, "waitForManifestToStabilize") {
		t.Errorf("log não contém a operação: %q", log)
	}
	if !strings.Contains(log, "Unexpected Task Failure") {
		t.Errorf("log não contém o erro: %q", log)
	}
	if !strings.Contains(log, "Stage Patch (Manifest) timed out after 30 minutes") {
		t.Errorf("log não contém a lista de errors: %q", log)
	}
	if !strings.Contains(log, "Stack trace:") {
		t.Errorf("log não contém a stack trace: %q", log)
	}
}

// TestStage_FailureLog_KatoTasks reproduz o outro formato real confirmado ao vivo (squad "SRE
// Logística", ambiente HLG, stage runJobManifest sem conseguir alcançar o cluster via DNS):
// falha dentro de "kato.tasks", com history passo-a-passo + exception com a causa real.
func TestStage_FailureLog_KatoTasks(t *testing.T) {
	stage := Stage{
		Name:   "xl-release-callback",
		Status: "TERMINAL",
		Context: []byte(`{
			"kato.tasks": [
				{
					"history": [
						{"phase": "ORCHESTRATION", "status": "Initializing Orchestration Task"},
						{"phase": "RUN_KUBERNETES_JOB", "status": "Running Kubernetes job..."}
					],
					"exception": {
						"message": "Deploy failed for manifest: job xlrelease-callback. Error: Unable to connect to the server: dial tcp: lookup akspriv-tracking-hlg on 10.2.0.10:53: no such host",
						"operation": "KubernetesRunJobOperation"
					}
				}
			]
		}`),
	}

	log := stage.FailureLog()
	if !strings.Contains(log, "KubernetesRunJobOperation") {
		t.Errorf("log não contém a operação: %q", log)
	}
	if !strings.Contains(log, "no such host") {
		t.Errorf("log não contém a mensagem de erro real: %q", log)
	}
	if !strings.Contains(log, "Running Kubernetes job...") {
		t.Errorf("log não contém o histórico passo-a-passo: %q", log)
	}
}

// TestStage_FailureLog_SemFalha garante que um stage bem-sucedido (sem exception/kato.tasks)
// devolve string vazia — o frontend usa isso pra decidir se mostra o botão "Ver log".
func TestStage_FailureLog_SemFalha(t *testing.T) {
	cases := []Stage{
		{Name: "deploy-helm", Status: "SUCCEEDED", Context: []byte(`{"account":"x","cloudProvider":"kubernetes"}`)},
		{Name: "vazio", Status: "SKIPPED"},
		{Name: "json-invalido", Status: "TERMINAL", Context: []byte(`not-json`)},
		{Name: "sem-status", Context: []byte(`{"exception":{"details":{"error":"x"}}}`)},
	}
	for _, s := range cases {
		if got := s.FailureLog(); got != "" {
			t.Errorf("FailureLog() de %q = %q, esperava vazio", s.Name, got)
		}
	}
}

// TestStage_FailureLog_KatoTasksHistorySemExceptionNaoConta — bug real corrigido: achado ao
// vivo (2 deployments reais, squad tracking, ambas execuções SUCCEEDED) que "kato.tasks[].history"
// está presente em TODA tarefa Kubernetes, sucesso incluso — não é sinal de falha sozinho. Sem
// esse fix, um stage SUCCEEDED aparecia com "Ver log" na UI, mostrando só a narrativa normal de
// deploy como se fosse uma falha.
func TestStage_FailureLog_KatoTasksHistorySemExceptionNaoConta(t *testing.T) {
	stage := Stage{
		Name:   "deploy-helm",
		Status: "SUCCEEDED",
		Context: []byte(`{
			"kato.tasks": [
				{
					"history": [
						{"phase": "ORCHESTRATION", "status": "Initializing Orchestration Task"},
						{"phase": "RUN_KUBERNETES_JOB", "status": "Running Kubernetes job..."}
					]
				}
			]
		}`),
	}
	if got := stage.FailureLog(); got != "" {
		t.Errorf("FailureLog() = %q, esperava vazio (history sem exception, stage SUCCEEDED)", got)
	}
}

// TestStage_FailureLog_TruncaLogsGigantes garante o teto de segurança (maxStageLogChars).
func TestStage_FailureLog_TruncaLogsGigantes(t *testing.T) {
	hugeStackTrace := strings.Repeat("x", maxStageLogChars*2)
	stage := Stage{
		Status:  "TERMINAL",
		Context: []byte(`{"exception":{"details":{"stackTrace":"` + hugeStackTrace + `"}}}`),
	}
	log := stage.FailureLog()
	if len(log) > maxStageLogChars+50 { // pequena margem pro sufixo "...[truncado]..."
		t.Errorf("log não foi truncado: %d chars", len(log))
	}
	if !strings.HasSuffix(log, "...[truncado]...") {
		t.Errorf("log truncado não termina com o marcador esperado")
	}
}
