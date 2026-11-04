package dynatrace

import (
	"strings"
	"testing"
)

func TestNormalizeBaseURL_Validas(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://nyr48864.live.dynatrace.com", "https://nyr48864.live.dynatrace.com"},
		{"https://nyr48864.live.dynatrace.com/", "https://nyr48864.live.dynatrace.com"},
		{"https://nyr48864.live.dynatrace.com/api/v2", "https://nyr48864.live.dynatrace.com"},
		{"https://nyr48864.live.dynatrace.com/api/v2/", "https://nyr48864.live.dynatrace.com"},
		{"  https://nyr48864.live.dynatrace.com \n", "https://nyr48864.live.dynatrace.com"},
		// sem esquema, mas com cara de endereço: assume https
		{"nyr48864.live.dynatrace.com", "https://nyr48864.live.dynatrace.com"},
		{"nyr48864.live.dynatrace.com/api/v2", "https://nyr48864.live.dynatrace.com"},
		// UI web vira API
		{"https://abc123.apps.dynatrace.com", "https://abc123.live.dynatrace.com"},
		// Managed: o caminho do ambiente é preservado
		{"https://dt.empresa.com.br/e/1a2b3c", "https://dt.empresa.com.br/e/1a2b3c"},
		{"dt.empresa.com.br:9999", "https://dt.empresa.com.br:9999"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"localhost", "https://localhost"},
	}
	for _, c := range cases {
		got, err := NormalizeBaseURL(c.in)
		if err != nil {
			t.Errorf("NormalizeBaseURL(%q) deu erro: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, quer %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeBaseURL_Invalidas(t *testing.T) {
	cases := []struct{ in, errContains string }{
		{"", "vazia"},
		{"   ", "vazia"},
		{"casas-bahia", `"casas-bahia" não é um endereço`}, // o caso do erro "unsupported protocol scheme"
		{"casas-bahia/api/v2", "não é um endereço"},
		{"meu-tenant", "https://<id>.live.dynatrace.com"},
		{"ftp://dt.empresa.com", "esquema"},
		{"https://", "inválida"},
	}
	for _, c := range cases {
		got, err := NormalizeBaseURL(c.in)
		if err == nil {
			t.Errorf("NormalizeBaseURL(%q) = %q, deveria dar erro", c.in, got)
			continue
		}
		if !strings.Contains(err.Error(), c.errContains) {
			t.Errorf("NormalizeBaseURL(%q): erro %q não contém %q", c.in, err.Error(), c.errContains)
		}
	}
}

// Reproduz o erro relatado: com a URL "casas-bahia" o cliente nem é criado, e a causa fica clara
// (antes ele era criado e a primeira chamada falhava com "unsupported protocol scheme").
func TestNewClient_RecusaNomeSoltoNoLugarDaURL(t *testing.T) {
	c, err := NewClient("casas-bahia", "token")
	if err == nil {
		t.Fatalf("esperava erro, veio cliente com baseURL=%q", c.BaseURL())
	}
	if !strings.Contains(err.Error(), "casas-bahia") || !strings.Contains(err.Error(), "live.dynatrace.com") {
		t.Errorf("a mensagem deveria citar o valor e dar um exemplo de URL: %v", err)
	}
}

func TestNewClient_URLNormalizadaNaRequisicao(t *testing.T) {
	c, err := NewClient("nyr48864.live.dynatrace.com/api/v2/", "token")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://nyr48864.live.dynatrace.com" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}
