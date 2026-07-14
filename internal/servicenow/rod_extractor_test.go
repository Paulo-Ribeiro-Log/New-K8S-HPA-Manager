package servicenow

import (
	"testing"

	"github.com/rs/zerolog"
)

// Mesmo caso do parser_test.go, mas contra o parseDescription do RodExtractor —
// é esse método que roda de fato em produção (client.go/ExtractFromDescription é
// caminho secundário). Ver realChgDescription em parser_test.go.
func TestRodExtractor_ParseDescription_RepoFromURLDoRepositorio(t *testing.T) {
	logger := zerolog.Nop()
	r := NewRodExtractor(&logger)

	data := r.parseDescription(realChgDescription)

	if data.GitHubRepo != "processamento-contrato" {
		t.Errorf("GitHubRepo = %q, esperado %q (não deve usar o valor de Aplicação com sufixo -b2c)", data.GitHubRepo, "processamento-contrato")
	}
	if data.Application != "processamento-contrato-b2c" {
		t.Errorf("Application = %q, esperado %q", data.Application, "processamento-contrato-b2c")
	}
}

func TestRodExtractor_ParseDescription_AlphanumericVersionTag(t *testing.T) {
	logger := zerolog.Nop()
	r := NewRodExtractor(&logger)

	description := "* Aplicação(ões): outro-app.\n" +
		"* Versão: choic-4437_cnpj_v6-1.\n"

	data := r.parseDescription(description)

	if data.Version != "choic-4437_cnpj_v6-1" {
		t.Errorf("Version = %q, esperado %q (tag alfanumérica não deve ser truncada/corrompida)", data.Version, "choic-4437_cnpj_v6-1")
	}
}

func TestRodExtractor_ParseDescription_RepoFromLegacyFormat(t *testing.T) {
	logger := zerolog.Nop()
	r := NewRodExtractor(&logger)

	description := "* Aplicação(ões): tms-sync-1p-order-management-acl.\n" +
		"* Repositório: github.com/casas-bahia/tms-sync-1p-order-management-acl.git.\n"

	data := r.parseDescription(description)

	if data.GitHubRepo != "tms-sync-1p-order-management-acl" {
		t.Errorf("GitHubRepo = %q, esperado %q (formato antigo '* Repositório:' deve continuar funcionando)", data.GitHubRepo, "tms-sync-1p-order-management-acl")
	}
}
