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

// EnrichEntitiesWithK8s busca as entidades afetadas e extrai correlação K8s + DTLabels.
// Usa cache em memória (TTL 5 min) para evitar chamadas repetidas à API por entityID.
func (c *Client) EnrichEntitiesWithK8s(ctx context.Context, stubs []EntityStub) []EntityStub {
	enriched := make([]EntityStub, 0, len(stubs))
	for _, stub := range stubs {
		// Verificar cache antes de chamar API
		if raw, ok := entityCache.Load(stub.EntityID.ID); ok {
			entry := raw.(entityCacheEntry)
			if time.Since(entry.cachedAt) < entityCacheTTL {
				enriched = append(enriched, entry.stub)
				continue
			}
			entityCache.Delete(stub.EntityID.ID) // entrada expirada
		}

		entity, err := c.GetEntity(ctx, stub.EntityID.ID)
		if err != nil {
			if stub.DisplayName == "" && stub.Name != "" {
				stub.DisplayName = stub.Name
			}
			enriched = append(enriched, stub)
			continue
		}
		result := enrichFromEntity(stub, entity)
		entityCache.Store(stub.EntityID.ID, entityCacheEntry{stub: result, cachedAt: time.Now()})
		enriched = append(enriched, result)
	}
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

// listEntitiesBySelector busca entidades com um entitySelector arbitrário e retorna EntityStubs enriquecidos.
// Usa pageSize=500 (limite prático por cluster). Grava no cache entityCache.
func (c *Client) listEntitiesBySelector(ctx context.Context, entitySelector string) ([]EntityStub, error) {
	params := url.Values{
		"entitySelector": {entitySelector},
		"fields":         {"+tags,+properties"},
		"pageSize":       {"500"},
	}
	var resp struct {
		Entities []Entity `json:"entities"`
	}
	if err := c.get(ctx, "entities", params, &resp); err != nil {
		return nil, err
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

// resolveKubernetesClusterEntityID resolve o entityId da entidade KUBERNETES_CLUSTER cujo
// displayName bate com clusterName. Cacheado — ver clusterEntityCache.
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
// Retorna até 500 entidades (limite prático por cluster, sem paginação).
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
