package handlers

import (
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

var yamlDocSeparator = regexp.MustCompile(`(?m)^---[ \t]*$`)

// looksLikeRenderedManifest diz se o texto parece um manifest renderizado do Kubernetes (um ou mais
// documentos com apiVersion e kind) em vez de um values.yaml.
//
// O Helm não distingue: se alguém mandar o manifest como values, o `helm upgrade` aceita, o release
// volta aos padrões do chart e fica com chaves inúteis por cima. Já aconteceu: a aba Manifest do
// frontend reaproveitava o modal da aba Valores para "aplicar". A tela foi corrigida; esta checagem
// protege a API contra clientes antigos (bundle em cache no navegador) e usos diretos.
//
// Um values legítimo quase nunca tem `apiVersion` e `kind` juntos na raiz.
func looksLikeRenderedManifest(valuesYAML string) bool {
	if strings.TrimSpace(valuesYAML) == "" {
		return false
	}
	for _, doc := range yamlDocSeparator.Split(valuesYAML, -1) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			continue
		}
		_, hasAPI := m["apiVersion"]
		_, hasKind := m["kind"]
		if hasAPI && hasKind {
			return true
		}
	}
	return false
}

const helmManifestAsValuesMsg = "o conteúdo enviado parece um manifest renderizado do Kubernetes (apiVersion e kind), não um values.yaml. " +
	"Enviá-lo como values faria o release voltar aos padrões do chart. Edite os values do release (aba Valores) em vez do manifest."
