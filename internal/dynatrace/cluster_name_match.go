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
// ambiente (cobertos por clusterNameEnvGroups) nem provider/plataforma/ARN comuns na convenção de
// nomes de cluster deste app (ex: "arn:aws:eks:REGION:ACCOUNT:cluster/NAME" — contexts EKS sem
// alias amigável configurado usam o ARN completo como identificador).
var clusterNameGenericTokens = map[string]bool{
	"aks": true, "eks": true, "gke": true, "akspriv": true, "admin": true, "agents": true,
	"arn": true, "aws": true, "cluster": true,
}

// isNumericToken reporta se o token é só dígitos — nunca ajuda a identificar um cluster (ex: o
// account ID ou a subrede de uma ARN AWS) e nunca vai bater com nada no Dynatrace.
func isNumericToken(tok string) bool {
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(tok) > 0
}

// tokenizeClusterName separa um nome de cluster/entidade em tokens minúsculos por separador comum
// (hífen, underscore, ponto, barra, dois-pontos) — mesmo conjunto de separadores usado em nomes de
// cluster e em displayName de entidades Dynatrace neste tenant. Inclui ":" porque ARNs AWS usam
// esse separador (arn:aws:eks:us-east-1:123456789:cluster/nome) — sem isso, tokens de ARN saíam
// grudados uns nos outros e nunca combinavam com nada.
func tokenizeClusterName(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
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
		if clusterNameGenericTokens[tok] || isNumericToken(tok) {
			continue
		}
		out = append(out, tok)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// eksARNClusterPrefix identifica um context de cluster EKS registrado pelo ARN completo (sem
// alias amigável configurado) — formato "arn:aws:eks:REGION:ACCOUNT:cluster/NOME".
const eksARNClusterPrefix = "arn:aws:eks:"

// NormalizeClusterName resolve o nome de cluster usado internamente pelo app pro nome que a
// correlação com o Dynatrace deve usar. Dois ajustes, ambos no-op para nomes que já não precisam
// deles (seguro chamar sempre, em qualquer provider):
//
//  1. ARN completo de EKS (contexts sem alias amigável configurado, ex:
//     "arn:aws:eks:us-east-1:400894646268:cluster/asaplog-production") → nome curto
//     ("asaplog-production"). Sem isso, nem o nome exato nem o fallback fuzzy (ver
//     fuzzyResolveEntityIDByName) tinham chance de achar a entidade certa — bug real confirmado:
//     "asaplog-production" (nome curto, testado manualmente) resolvia corretamente contra o
//     Dynatrace, mas o app de verdade usa o ARN completo como identificador de cluster
//     (`selectedCluster` no frontend vem de `cluster.context`, não `cluster.name`), fazendo a
//     correlação real sempre falhar mesmo com a lógica de fallback fuzzy já corrigida.
//  2. Sufixo "-admin" (convenção AKS) removido — mesmo tratamento que outros ~30 call sites do
//     app já fazem pra outras integrações externas (Azure resource names, etc.).
func NormalizeClusterName(cluster string) string {
	name := cluster
	if strings.HasPrefix(name, eksARNClusterPrefix) {
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
	}
	return strings.TrimSuffix(name, "-admin")
}
