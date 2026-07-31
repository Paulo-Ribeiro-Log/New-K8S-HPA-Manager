package certificates

import (
	"context"
	"errors"
	"testing"
)

func TestAnalyzeBackendTLSLogs_LinhaComErroEHost(t *testing.T) {
	logs := map[string]string{
		"ingress-nginx/pod-1": `2026/07/31 10:00:00 [info] normal request
2026/07/31 10:00:05 [error] 123#123: *456 SSL_do_handshake() failed (SSL: error:...) while SSL handshaking to upstream, host: "api.example.com"
2026/07/31 10:00:10 [info] outra linha normal`,
	}

	result := analyzeBackendTLSLogs(logs, []string{"api.example.com"})

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if len(result.Signals) != 1 {
		t.Fatalf("esperava 1 signal, obteve %d: %v", len(result.Signals), result.Signals)
	}
	if len(result.ControllerPods) != 1 || result.ControllerPods[0] != "ingress-nginx/pod-1" {
		t.Errorf("ControllerPods = %v, esperado [ingress-nginx/pod-1]", result.ControllerPods)
	}
}

func TestAnalyzeBackendTLSLogs_ErroMasHostDiferente_NaoConta(t *testing.T) {
	logs := map[string]string{
		"ingress-nginx/pod-1": `2026/07/31 10:00:05 [error] SSL_do_handshake() failed while SSL handshaking to upstream, host: "other.example.com"`,
	}

	result := analyzeBackendTLSLogs(logs, []string{"api.example.com"})

	if len(result.Signals) != 0 {
		t.Errorf("esperava 0 signals (host diferente), obteve %v", result.Signals)
	}
	if len(result.Notes) == 0 {
		t.Error("esperava nota explicando que nenhum sinal foi encontrado")
	}
}

func TestAnalyzeBackendTLSLogs_LogLimpo(t *testing.T) {
	logs := map[string]string{
		"ingress-nginx/pod-1": `2026/07/31 10:00:00 [info] tudo normal, host: "api.example.com"`,
	}

	result := analyzeBackendTLSLogs(logs, []string{"api.example.com"})

	if !result.Checked {
		t.Error("esperava Checked=true mesmo sem sinais")
	}
	if len(result.Signals) != 0 {
		t.Errorf("esperava 0 signals, obteve %v", result.Signals)
	}
	if len(result.Notes) == 0 {
		t.Error("esperava nota explicando ausência de sinal (nunca 'confirmado OK')")
	}
}

func TestAnalyzeBackendTLSLogs_X509UnknownAuthority(t *testing.T) {
	logs := map[string]string{
		"ingress-nginx/pod-2": `2026/07/31 10:00:05 [error] upstream SSL certificate does not verify for host "api.example.com": x509: certificate signed by unknown authority`,
	}

	result := analyzeBackendTLSLogs(logs, []string{"api.example.com"})

	if len(result.Signals) != 1 {
		t.Fatalf("esperava 1 signal, obteve %d: %v", len(result.Signals), result.Signals)
	}
}

func TestCheckBackendTLSWithFetcher_OrquestracaoComErroDeLeitura(t *testing.T) {
	pods := []PodRef{
		{Namespace: "ingress-nginx", Name: "pod-1"},
		{Namespace: "ingress-nginx", Name: "pod-2"},
	}

	fetcher := func(ctx context.Context, pod PodRef) (string, error) {
		if pod.Name == "pod-1" {
			return "", errors.New("timeout lendo log")
		}
		return `[error] SSL_do_handshake() failed while SSL handshaking to upstream, host: "api.example.com"`, nil
	}

	result := checkBackendTLSWithFetcher(context.Background(), pods, []string{"api.example.com"}, fetcher)

	if !result.Checked {
		t.Fatal("esperava Checked=true (pelo menos 1 pod respondeu)")
	}
	if len(result.Signals) != 1 {
		t.Errorf("esperava 1 signal do pod-2, obteve %v", result.Signals)
	}
	foundReadError := false
	for _, n := range result.Notes {
		if n == "ingress-nginx/pod-1: timeout lendo log" {
			foundReadError = true
		}
	}
	if !foundReadError {
		t.Errorf("esperava nota sobre erro de leitura do pod-1, obteve %v", result.Notes)
	}
}

func TestLineMentionsAnyHost(t *testing.T) {
	if !lineMentionsAnyHost(`host: "API.Example.com"`, []string{"api.example.com"}) {
		t.Error("esperava match case-insensitive")
	}
	if lineMentionsAnyHost(`host: "other.example.com"`, []string{"api.example.com"}) {
		t.Error("nao esperava match")
	}
}
