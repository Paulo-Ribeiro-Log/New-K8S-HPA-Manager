package finops

import (
	"strings"

	"k8s-hpa-manager/internal/models"
)

// envAliases normaliza os marcadores de ambiente usados em nomes de cluster/RG e em tags.
var envAliases = map[string]string{
	"prd": "prd", "prod": "prd", "production": "prd", "producao": "prd",
	"hlg": "hlg", "hml": "hlg", "homolog": "hlg", "homologacao": "hlg",
	"dev": "dev", "desenv": "dev", "development": "dev",
	"stg": "stg", "staging": "stg",
	"qa": "qa", "uat": "uat", "sit": "sit",
}

// EnvironmentOf devolve o ambiente normalizado ("prd", "hlg", "dev"...) presente em um nome de
// cluster/RG ou valor de tag — o ÚLTIMO marcador reconhecido, que é onde a convenção põe o sufixo
// de ambiente (aks-x-prd, rg-x-data-hlg, MC_rg-x-app-hlg_akspriv-x-hlg_brazilsouth). "" quando
// não há marcador (não dá para afirmar o ambiente).
func EnvironmentOf(name string) string {
	env := ""
	for _, tok := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == ' '
	}) {
		if e, ok := envAliases[tok]; ok {
			env = e
		}
	}
	return env
}

// FilterScopeByEnvironment mantém só os RGs do ambiente env (o do cluster analisado): ambiente
// pela tag de ambiente do RG ou, sem ela, pelo nome. RG com a tag de jornada mas sem ambiente
// identificável fica de fora (pode ser de PRD ou de HLG); RG que veio de um cluster do escopo
// (app/node/dados) já é do ambiente certo, porque os clusters foram filtrados antes. env vazio
// (ambiente do cluster desconhecido) não filtra nada.
func FilterScopeByEnvironment(rgs []models.ScopedResourceGroup, env string) (kept, excluded []models.ScopedResourceGroup) {
	for _, rg := range rgs {
		e := EnvironmentOf(rg.EnvTag)
		if e == "" {
			e = EnvironmentOf(rg.ResourceGroup)
		}
		rg.Environment = e
		switch {
		case env == "" || e == env:
			kept = append(kept, rg)
		case e == "" && rg.Source != "tag":
			kept = append(kept, rg)
		case e == "":
			rg.ExcludedReason = "ambiente não identificável (sem tag de ambiente nem marcador no nome)"
			excluded = append(excluded, rg)
		default:
			rg.ExcludedReason = "ambiente " + e + " (a análise é de " + env + ")"
			excluded = append(excluded, rg)
		}
	}
	return kept, excluded
}
