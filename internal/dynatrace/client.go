package dynatrace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// entityCacheEntry guarda o resultado enriquecido de uma entidade Dynatrace com timestamp.
type entityCacheEntry struct {
	stub     EntityStub
	cachedAt time.Time
}

// entityCache é o cache global em memória para evitar chamadas repetidas à API de entidades.
// Chave: entityID (string). Valor: entityCacheEntry.
var entityCache sync.Map

const entityCacheTTL = 5 * time.Minute

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

// GetOpenProblems retorna problems com paginação automática.
// status: "OPEN" (padrão), "CLOSED", ou "ALL".
// filter (opcional): filtra por management zone ou tag (prefixo "tag:").
// from/to (opcional): intervalo ISO 8601 para busca histórica (ex: "now-7d", "2026-03-01T00:00:00Z").
// Limite de segurança: 200 problems por chamada.
func (c *Client) GetOpenProblems(ctx context.Context, filter, status, from, to string) (*ProblemsResult, error) {
	if status == "" {
		status = "OPEN"
	}
	status = strings.ToUpper(status)

	var selector string
	if status != "ALL" {
		selector = fmt.Sprintf(`status("%s")`, status)
	}

	if filter != "" {
		prefix := ""
		if selector != "" {
			prefix = ","
		}
		if strings.HasPrefix(filter, "tag:") {
			selector += prefix + fmt.Sprintf(`tag("%s")`, strings.TrimPrefix(filter, "tag:"))
		} else {
			selector += prefix + fmt.Sprintf(`managementZones("%s")`, filter)
		}
	}

	// A API DT usa now-2h como janela padrão quando "from" não é especificado.
	// Para CLOSED/ALL sem from explícito, ampliamos para now-4h para incluir
	// problems fechados recentemente (até 4h atrás).
	if from == "" && status != "OPEN" {
		from = "now-4h"
	}

	const maxProblems = 200
	const pageSize = 10 // API Dynatrace limita pageSize=10 quando fields está presente

	allProblems := make([]Problem, 0, pageSize)
	var totalCount int
	nextPageKey := ""

	for {
		var result struct {
			Problems    []problemRaw `json:"problems"`
			TotalCount  int          `json:"totalCount"`
			NextPageKey string       `json:"nextPageKey,omitempty"`
		}

		var params url.Values
		if nextPageKey == "" {
			// Primeira página: parâmetros completos
			params = url.Values{
				"fields":   {"+affectedEntities,+impactedEntities,+rootCauseEntity,+managementZones"},
				"pageSize": {fmt.Sprintf("%d", pageSize)},
			}
			if selector != "" {
				params["problemSelector"] = []string{selector}
			}
			// Intervalo de tempo para busca histórica (ex: problems encerrados num período)
			if from != "" {
				params["from"] = []string{from}
			}
			if to != "" {
				params["to"] = []string{to}
			}
		} else {
			// Páginas seguintes: apenas nextPageKey (os demais parâmetros são codificados nele)
			params = url.Values{"nextPageKey": {nextPageKey}}
		}

		if err := c.get(ctx, "problems", params, &result); err != nil {
			if len(allProblems) > 0 {
				// Retornar o que já coletamos em caso de erro numa página intermediária
				break
			}
			return nil, err
		}

		totalCount = result.TotalCount
		for _, raw := range result.Problems {
			allProblems = append(allProblems, raw.toModel())
		}

		if result.NextPageKey == "" || len(allProblems) >= maxProblems {
			break
		}
		nextPageKey = result.NextPageKey
	}

	return &ProblemsResult{Problems: allProblems, TotalCount: totalCount}, nil
}

// GetManagementZones retorna as management zones (alert profiles) do ambiente Dynatrace.
// Tenta 3 estratégias em cascata, da mais completa para a mais básica:
//
//  1. Settings API (builtin:management-zones) — lista TODAS as zones configuradas.
//     Requer scope "Read settings" (settings.read). Mais completa.
//
//  2. Entities API (type(MANAGEMENT_ZONE)) — management zones são entidades DT.
//     Requer apenas "Read entities" (entities.read). Quase sempre disponível.
//
//  3. Extrai zones dos últimos 30 dias de problems — requer só "Read problems".
//     Menos completa (só zones com ocorrências recentes), mas sempre funciona.
func (c *Client) GetManagementZones(ctx context.Context) ([]ManagementZone, error) {
	// ── Estratégia 1: Settings API ─────────────────────────────────────────────
	var settingsResult struct {
		Items []struct {
			ObjectID string `json:"objectId"`
			Value    struct {
				Name string `json:"name"`
			} `json:"value"`
		} `json:"items"`
	}
	if err := c.get(ctx, "settings/objects", url.Values{
		"schemaIds": {"builtin:management-zones"},
		"pageSize":  {"500"},
		"scope":     {"environment"},
	}, &settingsResult); err == nil && len(settingsResult.Items) > 0 {
		zones := make([]ManagementZone, 0, len(settingsResult.Items))
		for _, item := range settingsResult.Items {
			if item.Value.Name != "" {
				zones = append(zones, ManagementZone{ID: item.ObjectID, Name: item.Value.Name})
			}
		}
		sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })
		return zones, nil
	}

	// ── Estratégia 2: Entities API ─────────────────────────────────────────────
	var entResult struct {
		Entities []struct {
			EntityID    string `json:"entityId"`
			DisplayName string `json:"displayName"`
		} `json:"entities"`
	}
	if err := c.get(ctx, "entities", url.Values{
		"entitySelector": {"type(MANAGEMENT_ZONE)"},
		"pageSize":       {"500"},
		"fields":         {"displayName"},
	}, &entResult); err == nil && len(entResult.Entities) > 0 {
		zones := make([]ManagementZone, 0, len(entResult.Entities))
		for _, e := range entResult.Entities {
			if e.DisplayName != "" {
				zones = append(zones, ManagementZone{ID: e.EntityID, Name: e.DisplayName})
			}
		}
		sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })
		return zones, nil
	}

	// ── Estratégia 3: Extrai dos últimos 30 dias de problems ───────────────────
	// Sem problemSelector + from=now-30d → DT retorna OPEN e CLOSED no período.
	var probResult struct {
		Problems []struct {
			ManagementZones []ManagementZone `json:"managementZones"`
		} `json:"problems"`
	}
	if err := c.get(ctx, "problems", url.Values{
		"fields":   {"+managementZones"},
		"pageSize": {"50"},
		"from":     {"now-30d"},
	}, &probResult); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	zones := make([]ManagementZone, 0)
	for _, p := range probResult.Problems {
		for _, mz := range p.ManagementZones {
			if mz.Name != "" {
				if _, ok := seen[mz.ID]; !ok {
					seen[mz.ID] = struct{}{}
					zones = append(zones, mz)
				}
			}
		}
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })
	return zones, nil
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

// enrichFromEntity preenche DisplayName, K8s básico e DTLabels ricos a partir de uma entidade.
func enrichFromEntity(stub EntityStub, entity *Entity) EntityStub {
	if entity.DisplayName != "" {
		stub.DisplayName = entity.DisplayName
	} else if stub.Name != "" {
		stub.DisplayName = stub.Name
	}
	corr := entity.ExtractK8sCorrelation()
	if corr != nil {
		stub.K8sCluster = corr.Cluster
		stub.K8sNamespace = corr.Namespace
		stub.K8sWorkload = corr.Workload
		stub.K8sPodName = corr.PodName
	}
	// Tags ricas (squad, journey, versão, GitHub repo, etc.)
	stub.Labels = entity.ExtractDTLabels()
	// Fallback de namespace via Labels se K8s não extraiu
	if stub.K8sNamespace == "" && stub.Labels != nil && stub.Labels.Namespace != "" {
		stub.K8sNamespace = stub.Labels.Namespace
	}
	// Relações de topologia — chain de chamadas (call chain para o VRP)
	for _, rel := range entity.ToRelationships["calls"] {
		stub.CallsTo = append(stub.CallsTo, rel.EntityID)
	}
	for _, rel := range entity.FromRelationships["calledBy"] {
		stub.CalledBy = append(stub.CalledBy, rel.EntityID)
	}
	return stub
}

// enrichConcurrency limita quantas chamadas GetEntity simultâneas EnrichEntitiesWithK8s dispara —
// mesmo padrão/valor de BatchQueryMetrics (metrics.go) nesta mesma package.
const enrichConcurrency = 10

// EnrichEntitiesWithK8s busca as entidades afetadas e extrai correlação K8s + DTLabels.
// Usa cache em memória (TTL 5 min) para evitar chamadas repetidas à API por entityID.
//
// Entidades com cache-hit são resolvidas na hora (sem rede); as com cache-miss disparam GetEntity
// em paralelo (até enrichConcurrency por vez), não mais uma de cada vez. Bug real de performance
// corrigido: entidades vindas de Problems (AffectedEntities/ImpactedEntities — GetOpenProblems,
// investigação profunda, etc.) nunca passam por listEntitiesBySelector antes, então chegam aqui
// SEMPRE com cache frio; um problem com dezenas de entidades pagava dezenas de round-trips HTTP
// sequenciais (algumas centenas de ms cada), tornando telas de investigação/análise de problem
// visivelmente lentas sem necessidade — cada GetEntity é independente, nada impede paralelizar.
func (c *Client) EnrichEntitiesWithK8s(ctx context.Context, stubs []EntityStub) []EntityStub {
	enriched := make([]EntityStub, len(stubs))

	type pending struct {
		idx  int
		stub EntityStub
	}
	var toFetch []pending
	for i, stub := range stubs {
		if raw, ok := entityCache.Load(stub.EntityID.ID); ok {
			entry := raw.(entityCacheEntry)
			if time.Since(entry.cachedAt) < entityCacheTTL {
				enriched[i] = entry.stub
				continue
			}
			entityCache.Delete(stub.EntityID.ID) // entrada expirada
		}
		toFetch = append(toFetch, pending{idx: i, stub: stub})
	}

	if len(toFetch) == 0 {
		return enriched
	}

	sem := make(chan struct{}, enrichConcurrency)
	var wg sync.WaitGroup
	for _, p := range toFetch {
		wg.Add(1)
		go func(p pending) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			stub := p.stub
			entity, err := c.GetEntity(ctx, stub.EntityID.ID)
			if err != nil {
				if stub.DisplayName == "" && stub.Name != "" {
					stub.DisplayName = stub.Name
				}
				enriched[p.idx] = stub
				return
			}
			result := enrichFromEntity(stub, entity)
			entityCache.Store(stub.EntityID.ID, entityCacheEntry{stub: result, cachedAt: time.Now()})
			enriched[p.idx] = result
		}(p)
	}
	wg.Wait()

	return enriched
}

// nameSearchSelectors gera múltiplas variantes de entitySelector para um nome de workload K8s.
// Nomes K8s usam hifens ("tms-embarcador") enquanto DT pode usar espaços ("TMS Embarcador")
// ou nomes completamente diferentes. As variantes cobrem os casos mais comuns:
//
//  1. contains com o nome original:        entityName.contains("tms-embarcador")
//  2. contains com espaços no lugar de -:  entityName.contains("tms embarcador")
//  3. AND dos 2 tokens mais longos (≥4):   entityName.contains("embarcador"),entityName.contains("tms")
//
// A variante 3 é a mais poderosa: encontra "TMS Embarcador", "tms-embarcador-api",
// "Embarcador TMS Service" etc. Tokens muito curtos (<4 chars) são ignorados para evitar
// falsos positivos (ex: "tms" com 3 chars pode ser omitido se a outra parte é suficiente).
func nameSearchSelectors(name string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	// 1. contains com nome original (hifenado)
	add(fmt.Sprintf(`entityName.contains("%s")`, name))

	// 2. troca hifens por espaços
	withSpaces := strings.ReplaceAll(name, "-", " ")
	if withSpaces != name {
		add(fmt.Sprintf(`entityName.contains("%s")`, withSpaces))
	}

	// 3. AND dos tokens mais específicos (split por "-", ordena por tamanho desc)
	parts := strings.Split(name, "-")
	if len(parts) >= 2 {
		// ordenar por comprimento decrescente para pegar os tokens mais específicos
		sorted := make([]string, len(parts))
		copy(sorted, parts)
		for i := 0; i < len(sorted)-1; i++ {
			for j := i + 1; j < len(sorted); j++ {
				if len(sorted[j]) > len(sorted[i]) {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		// pegar os 2 tokens mais longos com ao menos 3 caracteres
		var tokens []string
		for _, t := range sorted {
			if len(t) >= 3 {
				tokens = append(tokens, t)
				if len(tokens) == 2 {
					break
				}
			}
		}
		if len(tokens) == 2 {
			add(fmt.Sprintf(`entityName.contains("%s"),entityName.contains("%s")`, tokens[0], tokens[1]))
		} else if len(tokens) == 1 {
			// apenas 1 token longo — usa sozinho com contains
			add(fmt.Sprintf(`entityName.contains("%s")`, tokens[0]))
		}
	}

	return out
}

// SearchEntitiesByName busca entidades DT pelo nome de workloads K8s usando múltiplas estratégias.
// Para cada nome gera variantes de busca (contains com hifens, com espaços, tokens AND) para
// cobrir os diferentes padrões de nomenclatura entre K8s e Dynatrace.
// Ex: "tms-embarcador" encontra "TMS Embarcador", "tms-embarcador-api", "Embarcador TMS".
// Faz chamadas paralelas por variante, deduplica por entityID.
func (c *Client) SearchEntitiesByName(ctx context.Context, names []string) []EntityStub {
	if len(names) == 0 {
		return nil
	}
	if len(names) > 15 {
		names = names[:15]
	}

	// Gerar todas as variantes de selector para todos os nomes
	var selectors []string
	for _, name := range names {
		selectors = append(selectors, nameSearchSelectors(name)...)
	}

	type chanResult struct {
		stubs []EntityStub
	}
	ch := make(chan chanResult, len(selectors))
	var wg sync.WaitGroup

	for _, sel := range selectors {
		wg.Add(1)
		go func(entitySelector string) {
			defer wg.Done()

			params := url.Values{
				"entitySelector": {entitySelector},
				"fields":         {"+tags,+properties"},
				"pageSize":       {"10"},
			}

			var resp struct {
				Entities []Entity `json:"entities"`
			}
			if err := c.get(ctx, "entities", params, &resp); err != nil {
				ch <- chanResult{}
				return
			}

			stubs := make([]EntityStub, 0, len(resp.Entities))
			for _, e := range resp.Entities {
				stub := EntityStub{
					EntityID:    EntityID{ID: e.EntityID, Type: e.Type},
					DisplayName: e.DisplayName,
				}
				enriched := enrichFromEntity(stub, &e)
				entityCache.Store(enriched.EntityID.ID, entityCacheEntry{stub: enriched, cachedAt: time.Now()})
				stubs = append(stubs, enriched)
			}
			ch <- chanResult{stubs: stubs}
		}(sel)
	}

	go func() { wg.Wait(); close(ch) }()

	// Deduplicar por entityID
	seen := make(map[string]struct{})
	var all []EntityStub
	for r := range ch {
		for _, s := range r.stubs {
			if _, ok := seen[s.EntityID.ID]; !ok {
				seen[s.EntityID.ID] = struct{}{}
				all = append(all, s)
			}
		}
	}
	return all
}

// listEntitiesBySelectorMaxPages limita quantas páginas de 500 entidades listEntitiesBySelector
// segue via nextPageKey — 50 páginas = até 25.000 entidades. Sem isso, clusters com muito churn de
// CLOUD_APPLICATION_INSTANCE (CronJobs frequentes acumulam uma entidade por execução — Dynatrace
// não parece expirar essas entidades rápido) estouram o teto e voltam a perder resultado: 20
// páginas (10.000) pareciam generosas o bastante pro maior cluster AKS da frota (~4.561
// PROCESS_GROUP_INSTANCE em akspriv-oferta-prd, 103 nós), mas o cluster EKS asaplog-production
// tem 10.804 CLOUD_APPLICATION_INSTANCE reais — confirmado truncando ~800 entidades com o teto
// antigo. Ainda limitado (não ilimitado) pra não arriscar loop infinito caso a API do Dynatrace
// devolva nextPageKey indefinidamente por algum bug.
const listEntitiesBySelectorMaxPages = 50

// listEntitiesBySelector busca entidades com um entitySelector arbitrário e retorna EntityStubs
// enriquecidos. Segue nextPageKey até listEntitiesBySelectorMaxPages páginas — sem isso, clusters
// grandes (ex: akspriv-oferta-prd, 4.561 PROCESS_GROUP_INSTANCE) tinham 500 entidades retornadas e
// o resto descartado silenciosamente, fazendo pods do MESMO deployment aparecerem alguns
// "monitorados" (calharam de estar nos primeiros 500, ordem arbitrária da API) e outros "não
// monitorados" mesmo sendo igualmente instrumentados pelo OneAgent — bug real confirmado
// comparando kubectl (pods rodando há semanas, 0 restarts) contra a API do Dynatrace (entidade
// PROCESS_GROUP_INSTANCE correspondente inexistente em QUALQUER página, não só ausente da
// primeira). Grava no cache entityCache a cada página.
func (c *Client) listEntitiesBySelector(ctx context.Context, entitySelector string) ([]EntityStub, error) {
	var stubs []EntityStub
	nextPageKey := ""

	for page := 0; page < listEntitiesBySelectorMaxPages; page++ {
		var params url.Values
		if nextPageKey == "" {
			params = url.Values{
				"entitySelector": {entitySelector},
				"fields":         {"+tags,+properties"},
				"pageSize":       {"500"},
			}
		} else {
			// Páginas seguintes: apenas nextPageKey (mesmo padrão de GetOpenProblems acima —
			// os demais parâmetros já estão codificados nele pela própria API).
			params = url.Values{"nextPageKey": {nextPageKey}}
		}

		var resp struct {
			Entities    []Entity `json:"entities"`
			NextPageKey string   `json:"nextPageKey,omitempty"`
		}
		if err := c.get(ctx, "entities", params, &resp); err != nil {
			if page == 0 {
				return nil, err
			}
			// Falha numa página intermediária — retorna o que já foi coletado em vez de
			// descartar tudo (degradação graciosa, mesmo princípio de outras checagens
			// best-effort do app).
			break
		}

		for _, e := range resp.Entities {
			stub := EntityStub{
				EntityID:    EntityID{ID: e.EntityID, Type: e.Type},
				DisplayName: e.DisplayName,
			}
			enriched := enrichFromEntity(stub, &e)
			entityCache.Store(enriched.EntityID.ID, entityCacheEntry{stub: enriched, cachedAt: time.Now()})
			stubs = append(stubs, enriched)
		}

		nextPageKey = resp.NextPageKey
		if nextPageKey == "" {
			break
		}
	}

	return stubs, nil
}

// clusterEntityCache guarda o entityId da entidade KUBERNETES_CLUSTER resolvida por nome —
// praticamente nunca muda (o cluster não é recriado no Dynatrace), TTL bem mais longo que
// entityCache. Guarda também string vazia = "não encontrado" pra não bater na API repetidamente
// por um cluster que o Dynatrace realmente não conhece.
var clusterEntityCache sync.Map

const clusterEntityCacheTTL = 30 * time.Minute

type clusterEntityCacheEntry struct {
	entityID string // vazio = confirmado "não encontrado"
	cachedAt time.Time
}

// relationshipByEntityType mapeia o tipo de entidade pro nome da relação topológica que a liga a
// um KUBERNETES_CLUSTER — confirmado empiricamente via /api/v2/entityTypes/<tipo> (toRelationships)
// contra um tenant real, já que a documentação pública do Dynatrace não lista isso de forma óbvia.
var relationshipByEntityType = map[string]string{
	"CLOUD_APPLICATION":          "isClusterOfCa",
	"CLOUD_APPLICATION_INSTANCE": "isClusterOfCai",
	"SERVICE":                    "isClusterOfService",
}

// fuzzyResolveEntityIDByName tenta achar uma entidade do tipo entityType cujo displayName contenha
// o token mais distintivo de clusterName, exigindo que o token de ambiente do candidato seja
// compatível com o do cluster original — evita que a correlação fuzzy misture clusters de
// ambientes diferentes que só coincidem no nome do produto/squad (ex: "asaplog-preprod" vs
// "asaplog-production", ambos contêm "asaplog", mas são ambientes diferentes).
//
// Usado só como fallback, quando a busca por nome exato (entityName("<clusterName>")) não
// encontra nada — necessário porque nem todo cluster segue a convenção "nome do context ==
// displayName da entidade Dynatrace". Confirmado contra um tenant real: o cluster EKS
// "asaplog-production" tem sua entidade KUBERNETES_CLUSTER nomeada "eks-asaplog-prd" no
// Dynatrace — escolhido manualmente por quem criou o DynaKube, sem relação determinística com o
// nome real do cluster (diferente do sufixo "-admin" do AKS, que é só um strip fixo).
//
// Retorna "" (não encontrado) em vez de arriscar uma correlação errada sempre que o resultado é
// ambíguo (múltiplos candidatos compatíveis) ou quando o nome original não tem nenhum marcador de
// ambiente reconhecível (não dá pra desambiguar com segurança nesse caso).
func (c *Client) fuzzyResolveEntityIDByName(ctx context.Context, entityType, clusterName string) (string, error) {
	tokens := extractClusterDistinctiveTokens(clusterName)
	if len(tokens) == 0 {
		return "", nil
	}

	selector := fmt.Sprintf(`type("%s"),entityName.contains("%s")`, entityType, tokens[0])
	stubs, err := c.listEntitiesBySelector(ctx, selector)
	if err != nil || len(stubs) == 0 {
		return "", err
	}

	originalEnv := extractClusterEnvToken(clusterName)
	if originalEnv == "" {
		return "", nil
	}

	match := ""
	for _, stub := range stubs {
		if extractClusterEnvToken(stub.DisplayName) != originalEnv {
			continue
		}
		if match != "" {
			// mais de um candidato compatível — ambíguo, não arrisca.
			return "", nil
		}
		match = stub.EntityID.ID
	}
	return match, nil
}

// resolveKubernetesClusterEntityID resolve o entityId da entidade KUBERNETES_CLUSTER cujo
// displayName bate com clusterName. Cacheado — ver clusterEntityCache. Fallback fuzzy (ver
// fuzzyResolveEntityIDByName) só é tentado quando o nome exato não encontra nada — não altera o
// comportamento pra nenhum cluster que já resolvia corretamente antes.
func (c *Client) resolveKubernetesClusterEntityID(ctx context.Context, clusterName string) (string, error) {
	if raw, ok := clusterEntityCache.Load(clusterName); ok {
		entry := raw.(clusterEntityCacheEntry)
		if time.Since(entry.cachedAt) < clusterEntityCacheTTL {
			return entry.entityID, nil
		}
		clusterEntityCache.Delete(clusterName)
	}

	selector := fmt.Sprintf(`type("KUBERNETES_CLUSTER"),entityName("%s")`, clusterName)
	stubs, err := c.listEntitiesBySelector(ctx, selector)
	if err != nil {
		return "", err
	}
	entityID := ""
	if len(stubs) > 0 {
		entityID = stubs[0].EntityID.ID
	} else if fuzzyID, ferr := c.fuzzyResolveEntityIDByName(ctx, "KUBERNETES_CLUSTER", clusterName); ferr == nil {
		entityID = fuzzyID
	}
	clusterEntityCache.Store(clusterName, clusterEntityCacheEntry{entityID: entityID, cachedAt: time.Now()})
	return entityID, nil
}

// ListEntitiesByCluster lista entidades de um tipo instrumentadas pelo OneAgent em um cluster.
//
// Estratégia primária: resolver a entidade KUBERNETES_CLUSTER pelo nome e navegar a relação
// topológica direta até o tipo pedido (toRelationships.isClusterOfCa/Cai/Service). Confirmado
// empiricamente contra um tenant real que CLOUD_APPLICATION e CLOUD_APPLICATION_INSTANCE NÃO
// carregam tags (`"tags": []` sempre, mesmo em entidades ativamente monitoradas) — os seletores
// por tag abaixo NUNCA encontram nada pra esses 2 tipos, é preciso usar a relação. SERVICE tem
// tags, mas com chave `k8s.cluster.name` (não `kubernetes.cluster.name` como a tentativa 3
// assumia) — bug real também corrigido aqui.
//
// Fallback (defesa em profundidade, caso a entidade KUBERNETES_CLUSTER não exista/não resolva —
// ex: versão antiga de OneAgent, cluster fora do Kubernetes API Monitoring):
//  1. tag("dt.host_group.id:<cluster>")
//  2. tag("k8s.cluster.name:<cluster>")
//
// listEntitiesBySelector pagina internamente (até listEntitiesBySelectorMaxPages), então clusters
// grandes (milhares de entidades) não perdem resultado.
func (c *Client) ListEntitiesByCluster(ctx context.Context, clusterName, entityType string) ([]EntityStub, error) {
	if clusterName == "" || entityType == "" {
		return nil, fmt.Errorf("clusterName e entityType são obrigatórios")
	}

	// Estratégia primária: relação topológica direta com a entidade KUBERNETES_CLUSTER.
	if relationship, ok := relationshipByEntityType[entityType]; ok {
		clusterEntityID, err := c.resolveKubernetesClusterEntityID(ctx, clusterName)
		if err == nil && clusterEntityID != "" {
			selector := fmt.Sprintf(`type("%s"),toRelationships.%s(entityId("%s"))`, entityType, relationship, clusterEntityID)
			stubs, serr := c.listEntitiesBySelector(ctx, selector)
			if serr == nil && len(stubs) > 0 {
				return stubs, nil
			}
		}
	}

	// Fallback 1: tag dt.host_group.id (usada por HOST/PROCESS_GROUP em alguns tenants — mantida
	// por retrocompatibilidade, mas não confirmada como funcional pros tipos acima neste tenant).
	primarySelector := fmt.Sprintf(`type("%s"),tag("dt.host_group.id:%s")`, entityType, clusterName)
	stubs, err := c.listEntitiesBySelector(ctx, primarySelector)
	if err != nil {
		stubs = nil // falha não fatal — continua tentativas
	}
	if len(stubs) > 0 {
		return stubs, nil
	}

	// Fallback 2: tag k8s.cluster.name propagada pelo OneAgent (chave confirmada empiricamente —
	// a antiga "kubernetes.cluster.name" nunca bateu com nada, era só um typo do nome real da tag).
	tag2Selector := fmt.Sprintf(`type("%s"),tag("k8s.cluster.name:%s")`, entityType, clusterName)
	stubs, err = c.listEntitiesBySelector(ctx, tag2Selector)
	if err != nil {
		return nil, fmt.Errorf("ListEntitiesByCluster (todas as tentativas falharam): %w", err)
	}

	return stubs, nil
}

// hostGroupEntityCache guarda o entityId da entidade HOST_GROUP resolvida por nome — mesmo
// espírito de clusterEntityCache, TTL longo (praticamente nunca muda).
var hostGroupEntityCache sync.Map

// resolveHostGroupEntityID resolve o entityId da entidade HOST_GROUP cujo displayName bate com
// clusterName. HOST_GROUP é o nível de correlação certo pra clusters em modo OneAgent
// "classicFullStack" — confirmado empiricamente que displayName == hostGroup configurado no
// DynaKube (spec.oneAgent.classicFullStack.hostGroup), mesmo valor usado como nome do cluster em
// todo o resto do app.
func (c *Client) resolveHostGroupEntityID(ctx context.Context, clusterName string) (string, error) {
	if raw, ok := hostGroupEntityCache.Load(clusterName); ok {
		entry := raw.(clusterEntityCacheEntry)
		if time.Since(entry.cachedAt) < clusterEntityCacheTTL {
			return entry.entityID, nil
		}
		hostGroupEntityCache.Delete(clusterName)
	}

	selector := fmt.Sprintf(`type("HOST_GROUP"),entityName("%s")`, clusterName)
	stubs, err := c.listEntitiesBySelector(ctx, selector)
	if err != nil {
		return "", err
	}
	entityID := ""
	if len(stubs) > 0 {
		entityID = stubs[0].EntityID.ID
	} else if fuzzyID, ferr := c.fuzzyResolveEntityIDByName(ctx, "HOST_GROUP", clusterName); ferr == nil {
		entityID = fuzzyID
	}
	hostGroupEntityCache.Store(clusterName, clusterEntityCacheEntry{entityID: entityID, cachedAt: time.Now()})
	return entityID, nil
}

// ListProcessGroupInstancesByHostGroup lista as PROCESS_GROUP_INSTANCE (uma por processo/container
// monitorado) de um cluster em modo OneAgent "classicFullStack" — a maioria real da frota (AKS
// deste app), confirmado via `kubectl get dynakube -o yaml` contra vários clusters reais. Esse
// modo NÃO cria KUBERNETES_CLUSTER/CLOUD_APPLICATION/CLOUD_APPLICATION_INSTANCE (só existem em
// clusters com "Kubernetes API Monitoring"/Cloud Native Full Stack habilitado — ver
// ListEntitiesByCluster) — a correlação por pod só é possível via PROCESS_GROUP_INSTANCE,
// navegando a relação topológica isHostGroupOf até a entidade HOST_GROUP do cluster.
// Namespace/PodName vêm de properties.metadata (KUBERNETES_NAMESPACE/KUBERNETES_FULL_POD_NAME),
// não de tags — ver ExtractK8sCorrelation em models.go.
func (c *Client) ListProcessGroupInstancesByHostGroup(ctx context.Context, clusterName string) ([]EntityStub, error) {
	hostGroupID, err := c.resolveHostGroupEntityID(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	if hostGroupID == "" {
		return nil, nil // cluster não tem HOST_GROUP no Dynatrace — não é erro, só não monitorado
	}
	selector := fmt.Sprintf(`type("PROCESS_GROUP_INSTANCE"),toRelationships.isHostGroupOf(entityId("%s"))`, hostGroupID)
	return c.listEntitiesBySelector(ctx, selector)
}

// GetOpenProblemsForEntity retorna problems OPEN que afetam uma entidade específica.
// Usado na busca reversa: entity ID → problems ativos.
func (c *Client) GetOpenProblemsForEntity(ctx context.Context, entityID string) ([]Problem, error) {
	params := url.Values{
		"problemSelector": {fmt.Sprintf(`status("OPEN"),entityId("%s")`, entityID)},
		"fields":          {"+affectedEntities,+impactedEntities,+rootCauseEntity"},
		"pageSize":        {"5"}, // poucas entidades → poucos problems esperados
	}

	var result struct {
		Problems []problemRaw `json:"problems"`
	}
	if err := c.get(ctx, "problems", params, &result); err != nil {
		return nil, err
	}

	problems := make([]Problem, 0, len(result.Problems))
	for _, raw := range result.Problems {
		problems = append(problems, raw.toModel())
	}
	return problems, nil
}

// GetProblemsForEntityInWindow retorna problems (abertos OU fechados) que afetam uma entidade
// específica dentro de uma janela de tempo — diferente de GetOpenProblemsForEntity, que só cobre
// status("OPEN") e ignora completamente a janela. Usado pelo overlay de problems do gráfico de
// comportamento do Deployment (DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md, Fase 2): um problem já fechado
// (ex: um pico de CPU resolvido há 2h) ainda é relevante pra explicar o comportamento histórico
// exibido no gráfico, então o seletor aqui não filtra por status — mesma convenção epoch-ms de
// from/to já usada em GetEntityEvents. pageSize=10 (não 20) — confirmado empiricamente contra a
// API real que ela rejeita com 400 "Page size can't be larger than 10 if the fields parameter is
// set", mesma restrição já documentada em GetOpenProblems/GetOpenProblemsForEntity acima.
func (c *Client) GetProblemsForEntityInWindow(ctx context.Context, entityID string, from, to time.Time) ([]Problem, error) {
	params := url.Values{
		"problemSelector": {fmt.Sprintf(`entityId("%s")`, entityID)},
		"from":            {fmt.Sprintf("%d", from.UnixMilli())},
		"to":              {fmt.Sprintf("%d", to.UnixMilli())},
		"fields":          {"+affectedEntities,+impactedEntities,+rootCauseEntity"},
		"pageSize":        {"10"},
	}

	var result struct {
		Problems []problemRaw `json:"problems"`
	}
	if err := c.get(ctx, "problems", params, &result); err != nil {
		return nil, err
	}

	problems := make([]Problem, 0, len(result.Problems))
	for _, raw := range result.Problems {
		problems = append(problems, raw.toModel())
	}
	return problems, nil
}

// getProblemsForEntitiesInWindowMaxEntities limita quantos entityIDs GetProblemsForEntitiesInWindow
// consulta em paralelo — um Deployment com muitas réplicas (ex: 35, visto em cluster real) resolve
// pra um PROCESS_GROUP_INSTANCE por pod (ver ResolveEntityIDsForWorkload); sem teto, um workload
// grande dispararia dezenas de chamadas simultâneas à API do Dynatrace só pra montar o overlay de
// um único gráfico. 10 é generoso o bastante pra achar os problems reais sem explodir chamadas.
const getProblemsForEntitiesInWindowMaxEntities = 10

// GetProblemsForEntitiesInWindow busca problems de múltiplas entidades em paralelo e mescla o
// resultado, deduplicando por ProblemID — necessário porque ResolveEntityIDsForWorkload pode
// retornar uma entidade por CLOUD_APPLICATION (Cloud Native Full Stack) OU várias
// PROCESS_GROUP_INSTANCE, uma por réplica (classicFullStack, a maioria da frota AKS real). Um
// mesmo problem geralmente afeta várias réplicas ao mesmo tempo — sem dedupe apareceria repetido
// no overlay.
func (c *Client) GetProblemsForEntitiesInWindow(ctx context.Context, entityIDs []string, from, to time.Time) ([]Problem, error) {
	if len(entityIDs) > getProblemsForEntitiesInWindowMaxEntities {
		entityIDs = entityIDs[:getProblemsForEntitiesInWindowMaxEntities]
	}

	type entityResult struct {
		problems []Problem
		err      error
	}
	results := make([]entityResult, len(entityIDs))
	var wg sync.WaitGroup
	wg.Add(len(entityIDs))
	for i, id := range entityIDs {
		go func(i int, id string) {
			defer wg.Done()
			problems, err := c.GetProblemsForEntityInWindow(ctx, id, from, to)
			results[i] = entityResult{problems: problems, err: err}
		}(i, id)
	}
	wg.Wait()

	problemLists := make([][]Problem, 0, len(results))
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		problemLists = append(problemLists, r.problems)
	}

	merged := mergeProblemsDedup(problemLists)
	// Só propaga erro se NENHUMA entidade retornou problems e todas falharam — uma falha parcial
	// (ex: 1 de 5 réplicas com erro transitório) não deve esconder os problems que as outras
	// acharam com sucesso.
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// mergeProblemsDedup mescla várias listas de problems (uma por entidade consultada) num único
// slice, deduplicando por ProblemID — extraído em função pura pra ser testável sem precisar de um
// cliente Dynatrace real (a paralelização em si não tem lógica de negócio pra testar).
func mergeProblemsDedup(problemLists [][]Problem) []Problem {
	seen := make(map[string]bool)
	var merged []Problem
	for _, problems := range problemLists {
		for _, p := range problems {
			if seen[p.ProblemID] {
				continue
			}
			seen[p.ProblemID] = true
			merged = append(merged, p)
		}
	}
	return merged
}

// EnrichStub busca detalhes de uma única entidade e preenche DisplayName, K8s e DTLabels.
func (c *Client) EnrichStub(ctx context.Context, stub EntityStub) EntityStub {
	entity, err := c.GetEntity(ctx, stub.EntityID.ID)
	if err != nil {
		if stub.DisplayName == "" && stub.Name != "" {
			stub.DisplayName = stub.Name
		}
		return stub
	}
	return enrichFromEntity(stub, entity)
}

// ─── raw structs para parsing (timestamps em milissegundos no JSON) ────────────

type problemRaw struct {
	ProblemID        string           `json:"problemId"`
	DisplayID        string           `json:"displayId"`
	Title            string           `json:"title"`
	Status           string           `json:"status"`
	SeverityLevel    string           `json:"severityLevel"`
	ImpactLevel      string           `json:"impactLevel"`
	StartTime        int64            `json:"startTime"`
	EndTime          int64            `json:"endTime,omitempty"`
	AffectedEntities []EntityStub     `json:"affectedEntities"`
	ImpactedEntities []EntityStub     `json:"impactedEntities"`
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
