package dynatrace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client cliente HTTP para a Dynatrace Environment API v2
type Client struct {
	baseURL    string // ex: https://nyr48864.live.dynatrace.com
	apiToken   string // Api-Token individual ou service account
	httpClient *http.Client
}

// NewClient cria um cliente Dynatrace.
// baseURL e apiToken podem ser vazios — fallback para env vars DT_API_URL e DT_API_TOKEN.
func NewClient(baseURL, apiToken string) (*Client, error) {
	if baseURL == "" {
		baseURL = os.Getenv("DT_API_URL")
	}
	if apiToken == "" {
		apiToken = os.Getenv("DT_API_TOKEN")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("Dynatrace URL não configurada (configure em AI Settings ou via env DT_API_URL)")
	}
	if apiToken == "" {
		return nil, fmt.Errorf("Dynatrace API token não configurado (configure em AI Settings ou via env DT_API_TOKEN)")
	}
	// Normalizar URL — remover trailing slash e sufixo /api/v2 se já vier na URL
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/api/v2")

	// Corrigir domínio: apps.dynatrace.com é a UI web, a API usa live.dynatrace.com
	// Ex: https://abc123.apps.dynatrace.com → https://abc123.live.dynatrace.com
	baseURL = strings.ReplaceAll(baseURL, ".apps.dynatrace.com", ".live.dynatrace.com")

	return &Client{
		baseURL:  baseURL,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// BaseURL retorna a URL base configurada (sem token)
func (c *Client) BaseURL() string {
	return c.baseURL
}

// get executa GET na Dynatrace API v2 e decodifica JSON na struct destino
func (c *Client) get(ctx context.Context, path string, params url.Values, dest interface{}) error {
	apiURL := c.baseURL + "/api/v2/" + strings.TrimPrefix(path, "/")
	if len(params) > 0 {
		apiURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("erro ao criar requisição: %w", err)
	}
	req.Header.Set("Authorization", "Api-Token "+c.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro ao conectar ao Dynatrace: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("erro ao ler resposta: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 401:
			return fmt.Errorf("Dynatrace: token inválido ou expirado (401) — URL: %s", apiURL)
		case 403:
			return fmt.Errorf("Dynatrace: token sem permissão (403) — URL: %s — body: %s", apiURL, string(body))
		case 404:
			return fmt.Errorf("Dynatrace: endpoint não encontrado (404) — URL: %s", apiURL)
		default:
			return fmt.Errorf("Dynatrace API error (status %d) — URL: %s — body: %s", resp.StatusCode, apiURL, string(body))
		}
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("erro ao parsear resposta Dynatrace: %w", err)
	}
	return nil
}

// TestConnection verifica se a conexão e o token estão funcionando.
// Retorna a latência em ms.
func (c *Client) TestConnection(ctx context.Context) (int64, error) {
	start := time.Now()
	var result struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "metrics", url.Values{"pageSize": {"1"}}, &result); err != nil {
		// Tentar endpoint alternativo mais leve
		var envResult map[string]interface{}
		if err2 := c.get(ctx, "settings/schemas", url.Values{"pageSize": {"1"}}, &envResult); err2 != nil {
			return 0, err
		}
	}
	return time.Since(start).Milliseconds(), nil
}

// ProblemsResult resultado paginado da busca de problems
type ProblemsResult struct {
	Problems   []Problem
	TotalCount int // total real no Dynatrace (pode ser maior que len(Problems))
}

// GetOpenProblems retorna todos os problems com status OPEN.
// filter (opcional): filtra por management zone — ex: "SRE-LOGISTICA".
// Prefixo "tag:" força filtro por entity tag: "tag:minha-tag".
func (c *Client) GetOpenProblems(ctx context.Context, filter string) (*ProblemsResult, error) {
	selector := `status("OPEN")`
	if filter != "" {
		if strings.HasPrefix(filter, "tag:") {
			selector += fmt.Sprintf(`,tag("%s")`, strings.TrimPrefix(filter, "tag:"))
		} else {
			// Default: management zone
			selector += fmt.Sprintf(`,managementZones("%s")`, filter)
		}
	}
	params := url.Values{
		"problemSelector": {selector},
		"fields":          {"+affectedEntities,+impactedEntities,+rootCauseEntity,+managementZones"},
		"pageSize":        {"10"}, // máximo permitido com fields enriquecidos
	}

	var result struct {
		Problems    []problemRaw `json:"problems"`
		TotalCount  int          `json:"totalCount"`
		NextPageKey string       `json:"nextPageKey,omitempty"`
	}

	if err := c.get(ctx, "problems", params, &result); err != nil {
		return nil, err
	}

	problems := make([]Problem, 0, len(result.Problems))
	for _, raw := range result.Problems {
		problems = append(problems, raw.toModel())
	}
	return &ProblemsResult{Problems: problems, TotalCount: result.TotalCount}, nil
}

// GetProblem retorna detalhes completos de um problem pelo ID.
func (c *Client) GetProblem(ctx context.Context, problemID string) (*Problem, error) {
	params := url.Values{
		"fields": {"+affectedEntities,+impactedEntities,+rootCauseEntity,+managementZones"},
	}

	var raw problemRaw
	if err := c.get(ctx, "problems/"+problemID, params, &raw); err != nil {
		return nil, err
	}
	p := raw.toModel()
	return &p, nil
}

// GetEntity retorna uma entidade pelo ID com tags e relações.
func (c *Client) GetEntity(ctx context.Context, entityID string) (*Entity, error) {
	params := url.Values{
		"fields": {"+tags,+properties,+toRelationships.calls,+fromRelationships.calledBy"},
	}

	var entity Entity
	if err := c.get(ctx, "entities/"+entityID, params, &entity); err != nil {
		return nil, err
	}
	return &entity, nil
}

// GetEntityMetrics retorna métricas de uma entidade no período do problema.
func (c *Client) GetEntityMetrics(ctx context.Context, entityID, metricSelector, from, to string) (*MetricData, error) {
	if from == "" {
		from = "now-1h"
	}
	if to == "" {
		to = "now"
	}

	params := url.Values{
		"metricSelector": {metricSelector},
		"entitySelector": {fmt.Sprintf("entityId(\"%s\")", entityID)},
		"from":           {from},
		"to":             {to},
		"resolution":     {"1m"},
	}

	var result struct {
		Resolution string       `json:"resolution"`
		Result     []MetricData `json:"result"`
	}

	if err := c.get(ctx, "metrics/query", params, &result); err != nil {
		return nil, err
	}

	if len(result.Result) == 0 {
		return nil, fmt.Errorf("nenhuma métrica encontrada para %s", metricSelector)
	}
	return &result.Result[0], nil
}

// GetEntityEvents retorna eventos de uma entidade em um período.
func (c *Client) GetEntityEvents(ctx context.Context, entityID string, from, to time.Time) ([]Event, error) {
	params := url.Values{
		"entitySelector": {fmt.Sprintf("entityId(\"%s\")", entityID)},
		"from":           {fmt.Sprintf("%d", from.UnixMilli())},
		"to":             {fmt.Sprintf("%d", to.UnixMilli())},
		"pageSize":       {"20"},
	}

	var result struct {
		Events     []eventRaw `json:"events"`
		TotalCount int        `json:"totalCount"`
	}

	if err := c.get(ctx, "events", params, &result); err != nil {
		return nil, err
	}

	events := make([]Event, 0, len(result.Events))
	for _, raw := range result.Events {
		events = append(events, raw.toModel())
	}
	return events, nil
}

// EnrichEntitiesWithK8s busca as entidades afetadas e extrai correlação K8s.
// Retorna as EntityStubs com os campos K8s preenchidos.
func (c *Client) EnrichEntitiesWithK8s(ctx context.Context, stubs []EntityStub) []EntityStub {
	enriched := make([]EntityStub, 0, len(stubs))
	for _, stub := range stubs {
		entity, err := c.GetEntity(ctx, stub.EntityID.ID)
		if err != nil {
			enriched = append(enriched, stub)
			continue
		}
		// Preencher displayName da entidade real (a stub pode vir sem nome)
		if entity.DisplayName != "" {
			stub.DisplayName = entity.DisplayName
		}
		corr := entity.ExtractK8sCorrelation()
		if corr != nil {
			stub.K8sCluster = corr.Cluster
			stub.K8sNamespace = corr.Namespace
			stub.K8sWorkload = corr.Workload
		}
		enriched = append(enriched, stub)
	}
	return enriched
}

// ─── raw structs para parsing (timestamps em milissegundos no JSON) ────────────

type problemRaw struct {
	ProblemID        string       `json:"problemId"`
	DisplayID        string       `json:"displayId"`
	Title            string       `json:"title"`
	Status           string       `json:"status"`
	SeverityLevel    string       `json:"severityLevel"`
	ImpactLevel      string       `json:"impactLevel"`
	StartTime        int64        `json:"startTime"`
	EndTime          int64        `json:"endTime,omitempty"`
	AffectedEntities []EntityStub `json:"affectedEntities"`
	ImpactedEntities []EntityStub `json:"impactedEntities"`
	RootCauseEntity  *EntityStub      `json:"rootCauseEntity,omitempty"`
	ManagementZones  []ManagementZone `json:"managementZones,omitempty"`
	// EvidenceDetails omitido: StartTime vem como int64 (ms), incompatível com time.Time.
}

func (r *problemRaw) toModel() Problem {
	p := Problem{
		ProblemID:        r.ProblemID,
		DisplayID:        r.DisplayID,
		Title:            r.Title,
		Status:           r.Status,
		SeverityLevel:    r.SeverityLevel,
		ImpactLevel:      r.ImpactLevel,
		StartTime:        time.UnixMilli(r.StartTime),
		AffectedEntities: r.AffectedEntities,
		ImpactedEntities: r.ImpactedEntities,
		RootCauseEntity:  r.RootCauseEntity,
		ManagementZones:  r.ManagementZones,
	}
	if r.EndTime > 0 {
		t := time.UnixMilli(r.EndTime)
		p.EndTime = &t
	}
	return p
}

type eventRaw struct {
	EventID    string            `json:"eventId"`
	EventType  string            `json:"eventType"`
	Title      string            `json:"title"`
	StartTime  int64             `json:"startTime"`
	EndTime    int64             `json:"endTime,omitempty"`
	EntityID   string            `json:"entityId"`
	EntityName string            `json:"entityName,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

func (r *eventRaw) toModel() Event {
	e := Event{
		EventID:    r.EventID,
		EventType:  r.EventType,
		Title:      r.Title,
		StartTime:  time.UnixMilli(r.StartTime),
		EntityID:   r.EntityID,
		EntityName: r.EntityName,
		Properties: r.Properties,
	}
	if r.EndTime > 0 {
		t := time.UnixMilli(r.EndTime)
		e.EndTime = &t
	}
	return e
}
