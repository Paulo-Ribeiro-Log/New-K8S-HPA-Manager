package handlers

import (
	"strings"
	"testing"
)

func newESItem(name, targetName string) externalSecretListItem {
	item := externalSecretListItem{}
	item.Metadata.Name = name
	item.Spec.Target.Name = targetName
	return item
}

// TestPickExternalSecretCandidate_MatchesByTargetName cobre o caso real confirmado ao vivo
// (cluster akspriv-entregamais-prd-admin, namespace entrega-mais-prd): o ExternalSecret não
// segue a convenção fixa "sre-tools-external-secrets-<namespace>", mas seu spec.target.name bate
// exatamente com o Secret selecionado na UI.
func TestPickExternalSecretCandidate_MatchesByTargetName(t *testing.T) {
	items := []externalSecretListItem{
		newESItem("akv-entregamais-prd-entrega-mais-prd", "akv-entrega-mais-prd"),
	}
	got, err := pickExternalSecretCandidate(items, "entrega-mais-prd", "akv-entrega-mais-prd")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != "akv-entregamais-prd-entrega-mais-prd" {
		t.Errorf("got = %q, want %q", got, "akv-entregamais-prd-entrega-mais-prd")
	}
}

// TestPickExternalSecretCandidate_SingleItemWithoutSecretName cobre o chamador antigo (sem
// secretName informado) contra um namespace com um único ExternalSecret — não há ambiguidade
// possível, então deve escolher esse mesmo sem nenhum critério de nome.
func TestPickExternalSecretCandidate_SingleItemWithoutSecretName(t *testing.T) {
	items := []externalSecretListItem{
		newESItem("meu-external-secret", "algum-secret"),
	}
	got, err := pickExternalSecretCandidate(items, "ns", "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != "meu-external-secret" {
		t.Errorf("got = %q, want %q", got, "meu-external-secret")
	}
}

// TestPickExternalSecretCandidate_MultipleWithoutMatch garante que, na ambiguidade real (mais de
// um ExternalSecret e nenhum bate por target.name), o erro é retornado listando os candidatos —
// nunca escolhe um nome ao acaso (evitaria anotar o recurso errado silenciosamente).
func TestPickExternalSecretCandidate_MultipleWithoutMatch(t *testing.T) {
	items := []externalSecretListItem{
		newESItem("es-a", "secret-a"),
		newESItem("es-b", "secret-b"),
	}
	_, err := pickExternalSecretCandidate(items, "ns", "secret-c")
	if err == nil {
		t.Fatal("esperava erro de ambiguidade, got nil")
	}
	if !strings.Contains(err.Error(), "es-a") || !strings.Contains(err.Error(), "es-b") {
		t.Errorf("mensagem de erro deveria listar os candidatos, got: %v", err)
	}
}

// TestPickExternalSecretCandidate_MultipleWithMatch confirma que, mesmo com múltiplos
// ExternalSecrets no namespace, o critério por target.name resolve a ambiguidade corretamente
// (escolhe o certo, não o primeiro da lista).
func TestPickExternalSecretCandidate_MultipleWithMatch(t *testing.T) {
	items := []externalSecretListItem{
		newESItem("es-a", "secret-a"),
		newESItem("es-b", "secret-b"),
	}
	got, err := pickExternalSecretCandidate(items, "ns", "secret-b")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != "es-b" {
		t.Errorf("got = %q, want %q", got, "es-b")
	}
}

// TestPickExternalSecretCandidate_NoItems cobre o namespace sem nenhum ExternalSecret — erro
// claro, nunca um pânico por índice fora de faixa.
func TestPickExternalSecretCandidate_NoItems(t *testing.T) {
	_, err := pickExternalSecretCandidate(nil, "ns", "secret")
	if err == nil {
		t.Fatal("esperava erro para lista vazia, got nil")
	}
}
