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

func TestParseDOMMessages_SafeLinks(t *testing.T) {
	// URL embalada em Safe Links — comum em ambientes corporativos Microsoft
	safeURL := "https://na01.safelinks.protection.outlook.com/?url=https%3A%2F%2Fdevstartcd.via.com.br%2Fsre-approval%2Fform%2Fbc29e187-b998-439c-bd9c-edb64d2f5093&data=05%7C01%7C"
	msgs := []string{
		"CHG0454511",
		safeURL,
	}
	items := ParseDOMMessages(msgs)
	if len(items) != 1 {
		t.Fatalf("esperado 1 item (Safe Links), got %d", len(items))
	}
	want := "https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093"
	if items[0].ApprovalURL != want {
		t.Errorf("ApprovalURL Safe Links = %q, want %q", items[0].ApprovalURL, want)
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

func TestParseRawMessages_PostedAt(t *testing.T) {
	msgs := []RawMessage{
		{Text: "CHG0454511", PostedAt: "2026-07-08T14:32:05.123Z"},
		{Text: "https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093"},
	}
	items := ParseRawMessages(msgs)
	if len(items) != 1 {
		t.Fatalf("esperado 1 item, got %d", len(items))
	}
	if items[0].PostedAt == nil {
		t.Fatal("PostedAt não deveria ser nil")
	}
	want := "2026-07-08T14:32:05.123Z"
	if got := items[0].PostedAt.Format("2006-01-02T15:04:05.000Z"); got != want {
		t.Errorf("PostedAt = %q, want %q", got, want)
	}
}

func TestParseRawMessages_PostedAtMissing(t *testing.T) {
	// Sem datetime capturado (ex: fallback de leaf-node) — PostedAt deve ficar nil, não travar.
	msgs := []RawMessage{
		{Text: "CHG0454511"},
		{Text: "https://devstartcd.via.com.br/sre-approval/form/bc29e187-b998-439c-bd9c-edb64d2f5093"},
	}
	items := ParseRawMessages(msgs)
	if len(items) != 1 {
		t.Fatalf("esperado 1 item, got %d", len(items))
	}
	if items[0].PostedAt != nil {
		t.Errorf("PostedAt deveria ser nil, got %v", items[0].PostedAt)
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
