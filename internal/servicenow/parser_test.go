package servicenow

import "testing"

// Texto real de uma CHG (campo "Motivo da mudança") — reproduz o bug em que o regex só
// reconhecia "* Repositório:", não "* URL do Repositório:" (formato atual do template),
// caindo no fallback errado (Application/Projeto, que tem sufixo "-b2c" que não existe
// no repositório GitHub real).
const realChgDescription = `Opções de Janelas disponíveis para implementação em Produção:
    - Padrão: 00:00:00 - 08:00:00 (Dias Disponíveis: Segunda a Quinta-feira)

Objetivo/Motivo da Mudança:
    * Mensagens de commits sem a issue do Jira referenciadas:
        * PRS-4156 - Ajuste lógica de filial entrega para Transit point e EAD

Informações úteis sobre o repositório/projeto e da esteira de CD:
    * Squad(s): Frete Hub.
    * Produto: Gestão de Contratos de Frete.
    * Projeto: processamento-contrato-b2c.
    * Aplicação(ões): processamento-contrato-b2c.
    * Branch no GitHub: release/4.0.4.
    * Versão: 4.0.4-3.
    * Nome do Repositório: processamento-contrato
    * URL do Repositório: github.com/casas-bahia/processamento-contrato.git
    * Titulo da release no XL-Release: [Frete Hub] processamento-contrato-b2c - 4.0.4-3.
    * Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/abc
    * Severidade: 1
`

func TestExtractFromDescription_RepoFromURLDoRepositorio(t *testing.T) {
	data := ExtractFromDescription(realChgDescription)

	if data.GitHubRepo != "processamento-contrato" {
		t.Errorf("GitHubRepo = %q, esperado %q (não deve usar o valor de Aplicação/Projeto com sufixo -b2c)", data.GitHubRepo, "processamento-contrato")
	}
	if data.Application != "processamento-contrato-b2c" {
		t.Errorf("Application = %q, esperado %q", data.Application, "processamento-contrato-b2c")
	}
}

func TestExtractFromDescription_AlphanumericVersion(t *testing.T) {
	data := ExtractFromDescription(realChgDescription)

	if data.Version != "4.0.4-3" {
		t.Errorf("Version = %q, esperado %q", data.Version, "4.0.4-3")
	}
}

func TestExtractFromDescription_AlphanumericVersionTag(t *testing.T) {
	description := "* Aplicação(ões): outro-app.\n" +
		"* Versão: choic-4437_cnpj_v6-1.\n"

	data := ExtractFromDescription(description)

	if data.Version != "choic-4437_cnpj_v6-1" {
		t.Errorf("Version = %q, esperado %q (tag alfanumérica não deve ser truncada/corrompida)", data.Version, "choic-4437_cnpj_v6-1")
	}
}

func TestExtractFromDescription_RepoFromLegacyFormat(t *testing.T) {
	description := "* Aplicação(ões): tms-sync-1p-order-management-acl.\n" +
		"* Repositório: github.com/casas-bahia/tms-sync-1p-order-management-acl.git.\n"

	data := ExtractFromDescription(description)

	if data.GitHubRepo != "tms-sync-1p-order-management-acl" {
		t.Errorf("GitHubRepo = %q, esperado %q (formato antigo '* Repositório:' deve continuar funcionando)", data.GitHubRepo, "tms-sync-1p-order-management-acl")
	}
}
