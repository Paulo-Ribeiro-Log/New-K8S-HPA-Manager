package teams

import (
	"testing"
)

func TestParseDOMMessages(t *testing.T) {
	msgs := []string{
		"[GERAL] [AVISO][SRE APPROVAL] REALIZAR O PR... by Mr. ViaBot",
		"[AVISO][SRE APPROVAL] REALIZAR O PREENCHIMENTO DO FORMULÁRIO PARA APROVAR UMA MUDANÇA CONTÍNUA",
		"CHG0454511",
		"https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093",
		"[GERAL] [AVISO][SRE APPROVAL] REALIZAR O PR... by Mr. ViaBot",
		"[AVISO][SRE APPROVAL] REALIZAR O PREENCHIMENTO DO FORMULÁRIO PARA APROVAR UMA MUDANÇA CONTÍNUA",
		"CHG0454652",
		"https://devstartcd.via.com.br/sre-approval/form/eea9bbc3-8be3-424b-a5f0-8a0fe91252c7",
		// duplicate — deve ser ignorado
		"CHG0454511",
		"https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093",
	}

	items := ParseDOMMessages(msgs)

	if len(items) != 2 {
		t.Fatalf("esperado 2 itens, got %d", len(items))
	}
	if items[0].CHG != "CHG0454511" {
		t.Errorf("CHG[0] = %q", items[0].CHG)
	}
	if items[1].CHG != "CHG0454652" {
		t.Errorf("CHG[1] = %q", items[1].CHG)
	}
	if items[0].ApprovalURL != "https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093" {
		t.Errorf("URL[0] = %q", items[0].ApprovalURL)
	}
}

func TestParseDOMMessages_NoURL(t *testing.T) {
	// CHG sem URL adjacente não deve ser incluído
	msgs := []string{"CHG0454511", "outra coisa qualquer"}
	items := ParseDOMMessages(msgs)
	if len(items) != 0 {
		t.Fatalf("esperado 0 itens, got %d", len(items))
	}
}

func TestParseDOMMessages_WithDescription(t *testing.T) {
	msgs := []string{
		"SRE Approval - CHG0454511 by Mr. ViaBot",
		"Nome e versão da aplicação: [Logística Abastecimento] supply-neogrid-integration-job - 0.0.4-27",
		"CHG0454511",
		"https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093",
	}
	items := ParseDOMMessages(msgs)
	if len(items) != 1 {
		t.Fatalf("esperado 1 item, got %d", len(items))
	}
	want := "[Logística Abastecimento] supply-neogrid-integration-job - 0.0.4-27"
	if items[0].Description != want {
		t.Errorf("Description = %q, want %q", items[0].Description, want)
	}
}
