package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Azure Resource Graph: consulta KQL sobre o inventário de várias subscriptions numa chamada só
// (POST /providers/Microsoft.ResourceGraph/resources). Mesmo token de management.azure.com do
// ARMClient (az CLI autenticado).

const (
	armResourceGraphAPIVersion = "2022-10-01"
	argPageSize                = 1000
	argMaxSubsPerQuery         = 1000
)

// ResourceGraphQuery executa a consulta KQL nas subscriptions informadas (em lotes de até 1000,
// limite da API) e devolve todas as linhas (paginação por $skipToken). Cada linha é um objeto JSON.
func (c *ARMClient) ResourceGraphQuery(ctx context.Context, subscriptions []string, query string) ([]json.RawMessage, error) {
	if len(subscriptions) == 0 {
		return nil, fmt.Errorf("resource graph: nenhuma subscription para consultar")
	}
	url := fmt.Sprintf("%s/providers/Microsoft.ResourceGraph/resources?api-version=%s", armBaseURL, armResourceGraphAPIVersion)

	var rows []json.RawMessage
	for start := 0; start < len(subscriptions); start += argMaxSubsPerQuery {
		chunk := subscriptions[start:min(start+argMaxSubsPerQuery, len(subscriptions))]
		skipToken := ""
		for page := 0; page < armMaxPages; page++ {
			options := map[string]any{"$top": argPageSize, "resultFormat": "objectArray"}
			if skipToken != "" {
				options["$skipToken"] = skipToken
			}
			body, err := c.PostJSON(ctx, url, map[string]any{"subscriptions": chunk, "query": query, "options": options})
			if err != nil {
				return nil, fmt.Errorf("resource graph: %w", err)
			}
			var resp struct {
				Data      []json.RawMessage `json:"data"`
				SkipToken string            `json:"$skipToken"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				return nil, fmt.Errorf("resource graph: resposta inválida: %w", err)
			}
			rows = append(rows, resp.Data...)
			if resp.SkipToken == "" {
				break
			}
			skipToken = resp.SkipToken
		}
	}
	return rows, nil
}

// kqlList monta uma lista literal KQL — ('a', 'b') — escapando cada valor. Lista vazia vira um
// valor que nunca casa, para `in`/`in~` continuarem válidos.
func kqlList(values []string) string {
	if len(values) == 0 {
		return "('__nenhum__')"
	}
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `'`, `\'`)
		quoted = append(quoted, "'"+v+"'")
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}
