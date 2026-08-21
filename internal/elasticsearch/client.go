// Package elasticsearch é um client HTTP mínimo para o Elasticsearch desta empresa — usado hoje
// só como fonte de triagem do Health Check (HEALTHCHECK-TRIAGE-MODE-PLAN.md Fase 3): contar erros
// de log por namespace numa janela curta de tempo. Acesso direto ao Elasticsearch (Basic Auth,
// sem proxy Kibana — confirmado com o usuário em 2026-08-20, ver seção 4 item 5 do plano). Não é
// um client genérico de propósito geral — só o suficiente pra essa consulta específica.
package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Convenções assumidas, não confirmadas contra um índice real desta empresa ainda (usuário optou
// explicitamente por "assumir a convenção padrão e ajustar depois" em vez de bloquear o código
// até confirmar manualmente — ver HEALTHCHECK-TRIAGE-MODE-PLAN.md Fase 3). Se a fonte nunca achar
// nada mesmo com erros reais no índice, o primeiro lugar a checar é aqui.
const (
	// DefaultTimestampField é praticamente universal em instalações Elasticsearch/Kibana
	// (convenção do próprio ecossistema, não específica desta empresa) — risco baixo.
	DefaultTimestampField = "@timestamp"
	// DefaultLevelField casa com o formato de log já documentado nesta app pro pipeline
	// FluentD+EventHub (ver seção "JSON Inspector" do CLAUDE.md): {"time":...,"level":"INFO",...}.
	DefaultLevelField = "level"
	// DefaultNamespaceField é a convenção mais comum do fluent-plugin-kubernetes_metadata_filter
	// — a que o usuário confirmou assumir (pipeline real é Fluentd, ver seção 4 item 5 do plano).
	DefaultNamespaceField = "kubernetes.namespace_name"
	// DefaultClusterField não foi confirmado com o usuário — é uma suposição adicional necessária
	// pra não misturar logs de clusters diferentes na mesma contagem (esta app gerencia ~26
	// clusters, um índice/pipeline compartilhado é o caso comum). Inspirado no script de
	// referência da seção 0 do plano, que usava "cluster_name" pra esse mesmo propósito. Falha
	// segura se o campo não existir: o filtro por termo não acha nada, resultado vem vazio
	// (Available=true, Namespaces=[]) — nunca mistura dados de cluster errado.
	DefaultClusterField = "cluster_name"
	// DefaultIndexPattern busca em todos os índices — funciona, mas pode ser lento num cluster ES
	// grande. Fortemente recomendado configurar algo mais específico (ex: "k8s-logs-*") nas
	// credenciais do usuário.
	DefaultIndexPattern = "*"
	// DefaultTimeWindow é o mesmo padrão sugerido pelo script de referência da seção 0 do plano.
	DefaultTimeWindow = "now-15m"

	maxNamespaceBuckets = 200
)

// errorLevelValues — valores de "level" considerados erro. Cobre variação de capitalização entre
// pipelines diferentes (mesma preocupação de robustez já vista noutras integrações desta app).
var errorLevelValues = []string{"error", "Error", "ERROR", "fatal", "Fatal", "FATAL"}

// Client é o client HTTP mínimo pro Elasticsearch.
type Client struct {
	baseURL      string
	username     string
	password     string
	indexPattern string
	httpClient   *http.Client
}

// NewClient cria o client. indexPattern vazio usa DefaultIndexPattern.
func NewClient(baseURL, username, password, indexPattern string) *Client {
	if indexPattern == "" {
		indexPattern = DefaultIndexPattern
	}
	return &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		username:     username,
		password:     password,
		indexPattern: indexPattern,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				// Mesmo padrão de outros clients HTTP internos desta app (Dynatrace/Prometheus) —
				// certificados internos frequentemente não batem numa CA pública confiável.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// esSearchResponse é só o subconjunto do corpo de resposta do Elasticsearch que interessa aqui.
type esSearchResponse struct {
	Aggregations struct {
		ByNamespace struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int    `json:"doc_count"`
			} `json:"buckets"`
		} `json:"by_namespace"`
	} `json:"aggregations"`
}

// NamespaceErrorCounts busca, dentro da janela de tempo informada (formato relativo Elasticsearch,
// ex: "now-15m") e filtrado pelo cluster informado, a contagem de logs de erro por namespace.
//
// cluster vazio desativa o filtro por cluster (não recomendado em produção — mistura dados de
// todos os clusters que compartilham o mesmo índice/pipeline). Retorna mapa vazio (não erro)
// quando a query funciona mas não acha nada — erro só em falha real de rede/parse, pra distinguir
// "sem erro nenhum" de "não consegui perguntar" (mesmo espírito do campo Available na interface
// TargetSource que consome isto, ver internal/healthcheck/target_source_elasticsearch.go).
func (c *Client) NamespaceErrorCounts(ctx context.Context, cluster, timeWindow string) (map[string]int, error) {
	if timeWindow == "" {
		timeWindow = DefaultTimeWindow
	}

	must := []map[string]interface{}{
		{
			"range": map[string]interface{}{
				DefaultTimestampField: map[string]interface{}{
					"gte": timeWindow,
					"lte": "now",
				},
			},
		},
		{
			"terms": map[string]interface{}{
				DefaultLevelField: errorLevelValues,
			},
		},
	}
	if cluster != "" {
		must = append(must, map[string]interface{}{
			"term": map[string]interface{}{DefaultClusterField: cluster},
		})
	}

	query := map[string]interface{}{
		"size": 0,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": must,
			},
		},
		"aggs": map[string]interface{}{
			"by_namespace": map[string]interface{}{
				"terms": map[string]interface{}{
					"field": DefaultNamespaceField,
					"size":  maxNamespaceBuckets,
				},
			},
		},
	}

	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	url := fmt.Sprintf("%s/%s/_search", c.baseURL, c.indexPattern)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach elasticsearch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("elasticsearch returned HTTP %d", resp.StatusCode)
	}

	var result esSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode elasticsearch response: %w", err)
	}

	counts := make(map[string]int, len(result.Aggregations.ByNamespace.Buckets))
	for _, b := range result.Aggregations.ByNamespace.Buckets {
		counts[b.Key] = b.DocCount
	}
	return counts, nil
}

// TestConnection faz uma checagem mínima de conectividade/autenticação (GET / — endpoint raiz do
// Elasticsearch, sempre disponível e barato) sem depender de nenhuma convenção de campo/índice.
func (c *Client) TestConnection(ctx context.Context) (latencyMs int64, err error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to reach elasticsearch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("elasticsearch returned HTTP %d (verifique usuário/senha)", resp.StatusCode)
	}

	return time.Since(start).Milliseconds(), nil
}

// BaseURL retorna a URL configurada (sem credenciais) — usado só pra exibição/log.
func (c *Client) BaseURL() string {
	return c.baseURL
}
