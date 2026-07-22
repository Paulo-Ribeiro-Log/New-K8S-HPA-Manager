package dynatrace

import (
	"context"
	"fmt"
	"net/url"
)

// LogSearchResult é o retorno (parcial) de GET /api/v2/logs/search — só os campos usados pelo
// diagnóstico de ingestão (ver TestLogIngestion_Integration). O schema completo da API tem mais
// campos; não modelados aqui de propósito, já que esse endpoint não faz parte do fluxo funcional
// principal da aplicação (Problems/Entities/Métricas), só de uma checagem manual pontual.
type LogSearchResult struct {
	Results      []map[string]interface{} `json:"results"`
	NextSliceKey string                    `json:"nextSliceKey,omitempty"`
}

// SearchRecentLogs consulta GET /api/v2/logs/search (API clássica v2, Log Monitoring) ordenado
// por mais recente. query aceita a sintaxe de busca de log da Dynatrace (ex:
// `k8s.namespace.name=="meu-namespace"`); vazio busca sem filtro. Usado só para diagnosticar se
// o OneAgent/Log Module está de fato ingerindo logs no tenant — retornar sem erro e
// len(Results)==0 não confirma "desligado" (pode ser query sem match), só ausência de erro.
func (c *Client) SearchRecentLogs(ctx context.Context, query string, limit int) (*LogSearchResult, error) {
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	params.Set("sort", "-timestamp")

	var result LogSearchResult
	if err := c.get(ctx, "logs/search", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
