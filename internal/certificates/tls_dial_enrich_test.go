package certificates

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBuildTLSDialResult_TodosHostsAtuais(t *testing.T) {
	results := []tlsDialResult{
		{Host: "a.example.com", SerialDec: "123", IssuerCN: "Sectigo RSA OV CA", NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		{Host: "b.example.com", SerialDec: "123", IssuerCN: "Sectigo RSA OV CA", NotAfter: time.Now().Add(90 * 24 * time.Hour)},
	}

	result := buildTLSDialResult(results, "123")

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

	result := buildTLSDialResult(results, "123")

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

	result := buildTLSDialResult(results, "123")

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

func TestEnrichWithTLSDial_HostsVazio(t *testing.T) {
	result := EnrichWithTLSDial(context.Background(), nil, "123")

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

	result := EnrichWithTLSDial(context.Background(), []string{"a.example.com", "stale.example.com"}, "123")

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
