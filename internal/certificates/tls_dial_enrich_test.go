package certificates

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildTLSDialResult_TodosHostsAtuais(t *testing.T) {
	results := []tlsDialResult{
		{Host: "a.example.com", SerialDec: "123", IssuerCN: "Sectigo RSA OV CA", NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		{Host: "b.example.com", SerialDec: "123", IssuerCN: "Sectigo RSA OV CA", NotAfter: time.Now().Add(90 * 24 * time.Hour)},
	}

	result := buildTLSDialResult(results, "123", "")

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if result.Method != "tls-dial" {
		t.Errorf("Method = %q, esperado %q", result.Method, "tls-dial")
	}
	if result.TotalReplicasFound != 2 {
		t.Errorf("TotalReplicasFound = %d, esperado 2", result.TotalReplicasFound)
	}
	if result.ReplicasCurrent != 2 {
		t.Errorf("ReplicasCurrent = %d, esperado 2", result.ReplicasCurrent)
	}
	if len(result.ReplicasStale) != 0 {
		t.Errorf("esperava 0 hosts stale, obtive %v", result.ReplicasStale)
	}
}

func TestBuildTLSDialResult_AlgunsStale(t *testing.T) {
	results := []tlsDialResult{
		{Host: "a.example.com", SerialDec: "123"},
		{Host: "b.example.com", SerialDec: "999"},
		{Host: "c.example.com", SerialDec: "999"},
	}

	result := buildTLSDialResult(results, "123", "")

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if result.TotalReplicasFound != 3 {
		t.Errorf("TotalReplicasFound = %d, esperado 3", result.TotalReplicasFound)
	}
	if result.ReplicasCurrent != 1 {
		t.Errorf("ReplicasCurrent = %d, esperado 1", result.ReplicasCurrent)
	}
	if len(result.ReplicasStale) != 2 {
		t.Fatalf("esperava 2 hosts stale, obtive %d: %v", len(result.ReplicasStale), result.ReplicasStale)
	}
	if len(result.Notes) == 0 {
		t.Error("esperava uma nota avisando sobre propagação incompleta")
	}
}

func TestBuildTLSDialResult_TodosComErro(t *testing.T) {
	results := []tlsDialResult{
		{Host: "a.example.com", Err: errors.New("connection refused")},
		{Host: "b.example.com", Err: errors.New("timeout")},
	}

	result := buildTLSDialResult(results, "123", "")

	if result.Checked {
		t.Error("esperava Checked=false quando todos os hosts falham")
	}
	if result.Method != "tls-dial" {
		t.Errorf("Method = %q, esperado %q", result.Method, "tls-dial")
	}
	if len(result.Notes) < 2 {
		t.Errorf("esperava notas explicando os erros por host, obtive %v", result.Notes)
	}
}

func TestBuildTLSDialResult_EmissorDiferente_ClassificaComoCamadaExterna(t *testing.T) {
	// Achado real: cluster EKS onde o Secret tem cert Sectigo mas o host público serve um
	// certificado de emissor completamente diferente (DigiCert) — não é uma versão antiga do
	// mesmo certificado, é uma camada externa (CDN/WAF/proxy) na frente do cluster.
	results := []tlsDialResult{
		{Host: "api.example.com", SerialDec: "999", IssuerCN: "DigiCert Global G3 TLS ECC SHA384 2020 CA1"},
	}

	result := buildTLSDialResult(results, "123", "Sectigo RSA Organization Validation Secure Server CA")

	if len(result.ReplicasStale) != 0 {
		t.Errorf("esperava 0 em ReplicasStale (emissor diferente não é propagação atrasada), obteve %v", result.ReplicasStale)
	}
	if len(result.PossibleExternalLayer) != 1 || result.PossibleExternalLayer[0] != "api.example.com" {
		t.Errorf("esperava PossibleExternalLayer=[api.example.com], obteve %v", result.PossibleExternalLayer)
	}
	foundNote := false
	for _, n := range result.Notes {
		if strings.Contains(n, "camada externa") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("esperava nota mencionando 'camada externa', obteve %v", result.Notes)
	}
}

// TestBuildTLSDialResult_FakeIngressCert_ClassificaSeparadoDeCamadaExterna valida o achado real:
// um host que responde com o certificado autoassinado padrão do ingress-nginx tem emissor
// "diferente do esperado" (mesmo sinal bruto de PossibleExternalLayer), mas a causa real é mais
// específica — o host não bate com nenhum Ingress válido — e precisa cair no próprio balde
// DefaultFakeCert, não ser confundido com "camada externa (CDN/WAF)".
func TestBuildTLSDialResult_FakeIngressCert_ClassificaSeparadoDeCamadaExterna(t *testing.T) {
	results := []tlsDialResult{
		{Host: "sem-ingress.example.com", SerialDec: "999", IssuerCN: "Kubernetes Ingress Controller Fake Certificate", IsDefaultFakeCert: true},
	}

	result := buildTLSDialResult(results, "123", "Sectigo RSA Organization Validation Secure Server CA")

	if len(result.DefaultFakeCert) != 1 || result.DefaultFakeCert[0] != "sem-ingress.example.com" {
		t.Errorf("esperava DefaultFakeCert=[sem-ingress.example.com], obteve %v", result.DefaultFakeCert)
	}
	if len(result.PossibleExternalLayer) != 0 {
		t.Errorf("esperava 0 em PossibleExternalLayer (deve cair só em DefaultFakeCert), obteve %v", result.PossibleExternalLayer)
	}
	if len(result.ReplicasStale) != 0 {
		t.Errorf("esperava 0 em ReplicasStale, obteve %v", result.ReplicasStale)
	}
	foundNote := false
	for _, n := range result.Notes {
		if strings.Contains(n, "certificado autoassinado PADRÃO do ingress-nginx") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("esperava nota mencionando o certificado fake padrão, obteve %v", result.Notes)
	}
}

func TestBuildTLSDialResult_MesmoEmissor_ClassificaComoStaleGenuino(t *testing.T) {
	results := []tlsDialResult{
		{Host: "api.example.com", SerialDec: "999", IssuerCN: "Sectigo RSA Organization Validation Secure Server CA"},
	}

	result := buildTLSDialResult(results, "123", "Sectigo RSA Organization Validation Secure Server CA")

	if len(result.PossibleExternalLayer) != 0 {
		t.Errorf("esperava 0 em PossibleExternalLayer (mesmo emissor = propagação genuína), obteve %v", result.PossibleExternalLayer)
	}
	if len(result.ReplicasStale) != 1 || result.ReplicasStale[0] != "api.example.com" {
		t.Errorf("esperava ReplicasStale=[api.example.com], obteve %v", result.ReplicasStale)
	}
}

func TestBuildTLSDialResult_LeafIssuerCNVazio_CaiParaStaleConservador(t *testing.T) {
	// Sem o emissor esperado pra comparar (ex: falha ao parsear o PEM), não afirma "camada
	// externa" — comportamento conservador, cai no balde antigo (ReplicasStale).
	results := []tlsDialResult{
		{Host: "api.example.com", SerialDec: "999", IssuerCN: "DigiCert Global G3 TLS ECC SHA384 2020 CA1"},
	}

	result := buildTLSDialResult(results, "123", "")

	if len(result.PossibleExternalLayer) != 0 {
		t.Errorf("esperava 0 em PossibleExternalLayer sem leafIssuerCN pra comparar, obteve %v", result.PossibleExternalLayer)
	}
	if len(result.ReplicasStale) != 1 {
		t.Errorf("esperava 1 em ReplicasStale, obteve %v", result.ReplicasStale)
	}
}

func TestEnrichWithTLSDial_HostsVazio(t *testing.T) {
	result := EnrichWithTLSDial(context.Background(), nil, "123", "")

	if result.Checked {
		t.Error("esperava Checked=false para lista de hosts vazia")
	}
	if result.Method != "tls-dial" {
		t.Errorf("Method = %q, esperado %q", result.Method, "tls-dial")
	}
}

func TestEnrichWithTLSDial_OrquestracaoParalela(t *testing.T) {
	original := dialHostForCertFn
	defer func() { dialHostForCertFn = original }()

	dialHostForCertFn = func(ctx context.Context, host string) tlsDialResult {
		if host == "stale.example.com" {
			return tlsDialResult{Host: host, SerialDec: "999"}
		}
		return tlsDialResult{Host: host, SerialDec: "123"}
	}

	result := EnrichWithTLSDial(context.Background(), []string{"a.example.com", "stale.example.com"}, "123", "")

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if result.TotalReplicasFound != 2 {
		t.Errorf("TotalReplicasFound = %d, esperado 2", result.TotalReplicasFound)
	}
	if result.ReplicasCurrent != 1 {
		t.Errorf("ReplicasCurrent = %d, esperado 1", result.ReplicasCurrent)
	}
	if len(result.ReplicasStale) != 1 || result.ReplicasStale[0] != "stale.example.com" {
		t.Errorf("ReplicasStale = %v, esperado [stale.example.com]", result.ReplicasStale)
	}
}
