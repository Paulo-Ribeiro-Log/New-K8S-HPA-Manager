package handlers

import (
	"fmt"
	"sort"
	"strings"
)

// Política de ciclo de vida (lifecycle management) de uma conta de armazenamento: as regras que
// movem blobs para camadas mais baratas (Cool/Cold/Archive) ou os excluem depois de N dias.
// Lida do ARM (GET .../managementPolicies/default); só leitura.

// LifecycleRule é uma regra da política, já resumida em texto.
type LifecycleRule struct {
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	BlobTypes []string `json:"blob_types,omitempty"`
	Prefixes  []string `json:"prefixes,omitempty"`
	Actions   []string `json:"actions"`
	Deletes   bool     `json:"deletes"` // alguma ação exclui dados
}

// Escopos e ações, na ordem em que aparecem na tela.
var lifecycleScopes = []struct{ key, label string }{
	{"baseBlob", "blob"},
	{"snapshot", "snapshot"},
	{"version", "versão"},
}

var lifecycleActions = []struct{ key, label string }{
	{"tierToCool", "mover para Cool"},
	{"tierToCold", "mover para Cold"},
	{"tierToArchive", "mover para Archive"},
	{"delete", "excluir"},
}

// Condição de tempo de cada ação: campo do ARM -> texto.
var lifecycleBases = []struct{ key, label string }{
	{"daysAfterModificationGreaterThan", "da última modificação"},
	{"daysAfterLastAccessTimeGreaterThan", "do último acesso"},
	{"daysAfterCreationGreaterThan", "da criação"},
	{"daysAfterLastTierChangeGreaterThan", "da última mudança de camada"},
}

// parseLifecycleRules lê properties.policy.rules do managementPolicies/default.
func parseLifecycleRules(policy map[string]any) []LifecycleRule {
	raw, _ := dig(policy, "properties", "policy", "rules").([]any)
	rules := make([]LifecycleRule, 0, len(raw))
	for _, r := range raw {
		rule := LifecycleRule{
			Name:    digStr(r, "name"),
			Enabled: digStr(r, "enabled") != "false", // ausente = ativa
			Actions: []string{},
		}
		if bt, ok := dig(r, "definition", "filters", "blobTypes").([]any); ok {
			for _, b := range bt {
				if s, ok := b.(string); ok {
					rule.BlobTypes = append(rule.BlobTypes, s)
				}
			}
		}
		if pm, ok := dig(r, "definition", "filters", "prefixMatch").([]any); ok {
			for _, p := range pm {
				if s, ok := p.(string); ok {
					rule.Prefixes = append(rule.Prefixes, s)
				}
			}
		}
		for _, sc := range lifecycleScopes {
			for _, ac := range lifecycleActions {
				node := dig(r, "definition", "actions", sc.key, ac.key)
				if node == nil {
					continue
				}
				when := lifecycleWhen(node)
				if when == "" {
					continue
				}
				rule.Actions = append(rule.Actions, fmt.Sprintf("%s: %s %s", sc.label, ac.label, when))
				if ac.key == "delete" {
					rule.Deletes = true
				}
			}
		}
		rules = append(rules, rule)
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
	return rules
}

// lifecycleWhen descreve a condição de tempo de uma ação: "após 30 dia(s) da última modificação".
func lifecycleWhen(node any) string {
	var parts []string
	for _, b := range lifecycleBases {
		if v := digStr(node, b.key); v != "" {
			parts = append(parts, fmt.Sprintf("após %s dia(s) %s", v, b.label))
		}
	}
	return strings.Join(parts, " e ")
}

// lifecycleText é a linha única que a tela mostra para uma regra.
func lifecycleText(r LifecycleRule) string {
	var parts []string
	if len(r.BlobTypes) > 0 {
		parts = append(parts, "tipos: "+strings.Join(r.BlobTypes, ", "))
	}
	if len(r.Prefixes) > 0 {
		parts = append(parts, "prefixos: "+strings.Join(r.Prefixes, ", "))
	} else {
		parts = append(parts, "todos os blobs")
	}
	if len(r.Actions) == 0 {
		parts = append(parts, "sem ações")
	} else {
		parts = append(parts, strings.Join(r.Actions, "; "))
	}
	return strings.Join(parts, " · ")
}

// lifecycleNotFound reconhece "a conta não tem política" (não é erro: é a ausência de regras).
func lifecycleNotFound(err error) bool {
	m := err.Error()
	return strings.Contains(m, "ManagementPolicyNotFound") || strings.Contains(m, "ResourceNotFound") || strings.Contains(m, "was not found")
}

// lifecycleSection acrescenta a seção "Ciclo de vida" aos detalhes.
func lifecycleSection(policy map[string]any, err error, d *AzStorageDetails) {
	if err != nil {
		if lifecycleNotFound(err) {
			d.Sections = append(d.Sections, section("Ciclo de vida (regras)",
				AzField{Label: "Política", Value: "nenhuma regra configurada", Warn: true}))
			d.Notes = append(d.Notes, "Ciclo de vida: sem política — nenhum blob é movido para camadas mais baratas nem expira sozinho.")
			return
		}
		d.Warnings = append(d.Warnings, "Ciclo de vida: "+err.Error())
		return
	}
	rules := parseLifecycleRules(policy)
	if len(rules) == 0 {
		d.Sections = append(d.Sections, section("Ciclo de vida (regras)",
			AzField{Label: "Política", Value: "existe, mas sem regras", Warn: true}))
		return
	}
	fields := make([]AzField, 0, len(rules))
	for _, r := range rules {
		label := r.Name
		if !r.Enabled {
			label += " (desativada)"
		}
		fields = append(fields, AzField{Label: label, Value: lifecycleText(r), Warn: !r.Enabled})
		if r.Enabled && r.Deletes {
			var del []string
			for _, a := range r.Actions {
				if strings.Contains(a, "excluir") {
					del = append(del, a)
				}
			}
			d.Notes = append(d.Notes, fmt.Sprintf("Ciclo de vida: a regra %q exclui dados automaticamente (%s).", r.Name, strings.Join(del, "; ")))
		}
	}
	d.Sections = append(d.Sections, section("Ciclo de vida (regras)", fields...))
}
