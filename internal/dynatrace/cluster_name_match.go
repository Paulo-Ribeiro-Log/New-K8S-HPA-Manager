package dynatrace

import (
	"sort"
	"strings"
)

// clusterNameEnvGroups mapeia tokens de ambiente equivalentes para um grupo canônico — usado pra
// validar que uma correlação fuzzy por palavra-chave (ver fuzzyResolveEntityIDByName) não mistura
// clusters de ambientes diferentes. Ex: "asaplog-preprod" não deve casar com a entidade Dynatrace
// de "asaplog-production" só porque os dois nomes contêm "asaplog".
var clusterNameEnvGroups = map[string]string{
	"prd": "prd", "prod": "prd", "production": "prd",
	"hlg": "hlg", "hml": "hlg", "staging": "hlg", "stg": "hlg",
	"preprod": "preprod",
	"dev":     "dev", "development": "dev",
	"sit": "sit",
	"uat": "uat",
}

// clusterNameGenericTokens são tokens que não ajudam a distinguir um cluster de outro — nem
// ambiente (cobertos por clusterNameEnvGroups) nem provider/plataforma comuns na convenção de
// nomes de cluster deste app.
var clusterNameGenericTokens = map[string]bool{
	"aks": true, "eks": true, "gke": true, "akspriv": true, "admin": true, "agents": true,
}

// tokenizeClusterName separa um nome de cluster/entidade em tokens minúsculos por separador comum
// (hífen, underscore, ponto, barra) — mesmo conjunto de separadores usado em nomes de cluster e em
// displayName de entidades Dynatrace neste tenant.
func tokenizeClusterName(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/'
	})
}

// extractClusterEnvToken retorna o grupo de ambiente canônico do nome (ver clusterNameEnvGroups),
// ou "" se nenhum token reconhecido for encontrado.
func extractClusterEnvToken(name string) string {
	for _, tok := range tokenizeClusterName(name) {
		if group, ok := clusterNameEnvGroups[tok]; ok {
			return group
		}
	}
	return ""
}

// extractClusterDistinctiveTokens extrai tokens do nome do cluster que ajudam a identificá-lo de
// forma única, ignorando marcadores de ambiente e prefixos de provider/plataforma genéricos.
// Ordenados do mais longo pro mais curto — o token mais longo tende a ser o mais específico
// (menor chance de casar com clusters de produtos/squads diferentes).
func extractClusterDistinctiveTokens(name string) []string {
	var out []string
	for _, tok := range tokenizeClusterName(name) {
		if len(tok) < 3 {
			continue
		}
		if _, isEnv := clusterNameEnvGroups[tok]; isEnv {
			continue
		}
		if clusterNameGenericTokens[tok] {
			continue
		}
		out = append(out, tok)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}
