package dynatrace

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"
)

// ─── Tipos do contexto de um problem ─────────────────────────────────────────

// EvidenceItem evidência Davis AI de um problem (causa detectada automaticamente).
type EvidenceItem struct {
	EvidenceType string    `json:"evidenceType"` // EVENT | METRIC | TRANSACTIONAL | LOG | MAINTENANCE_WINDOW
	DisplayName  string    `json:"displayName"`  // ex: "Response time degradation"
	EntityID     string    `json:"entityId"`
	EntityName   string    `json:"entityName"`
	EntityType   string    `json:"entityType"`
	RootCause    bool      `json:"rootCause"`
	StartTime    time.Time `json:"startTime"`
}

// TopoRelation serviço relacionado à entidade raiz (upstream ou downstream).
type TopoRelation struct {
	EntityID   string `json:"entityId"`
	EntityName string `json:"entityName"`
	EntityType string `json:"entityType"`
	Direction  string `json:"direction"` // "outgoing" | "incoming"
}

// TraceEntry trace HTTP simplificado (distributed tracing).
type TraceEntry struct {
	TraceID    string    `json:"traceId"`
	StartTime  time.Time `json:"startTime"`
	DurationMs float64   `json:"durationMs"` // microseconds → ms
	Name       string    `json:"name"`       // ex: "/oauth/token"
	HTTPMethod string    `json:"httpMethod,omitempty"`
	HTTPStatus int       `json:"httpStatus,omitempty"`
	HasError   bool      `json:"hasError"`
}

// ContextEventEntry evento relacionado ao problem (simplificado para o frontend).
type ContextEventEntry struct {
	EventID    string    `json:"eventId"`
	EventType  string    `json:"eventType"` // ex: "PERFORMANCE_EVENT"
	Title      string    `json:"title"`
	StartTime  time.Time `json:"startTime"`
	EntityID   string    `json:"entityId"`
	EntityName string    `json:"entityName"`
}

// ProblemContext contexto rico de um problem: evidências Davis, eventos, topologia e traces.
type ProblemContext struct {
	ProblemID    string              `json:"problemId"`
	TimeFrom     time.Time           `json:"timeFrom"`
	TimeTo       time.Time           `json:"timeTo"`
	ProblemStart time.Time           `json:"problemStart"`
	Evidence     []EvidenceItem      `json:"evidence"`
	Events       []ContextEventEntry `json:"events"`
	Topology     []TopoRelation      `json:"topology"`
	Traces       []TraceEntry        `json:"traces"`
	TracesError  string              `json:"tracesError,omitempty"`  // erro ao buscar traces (ex: 403 sem permissão)
}

// ─── Evidências Davis AI ──────────────────────────────────────────────────────

// evidenceDetailsRaw usa int64 para evitar incompatibilidade com time.Time.
type evidenceDetailsRaw struct {
	TotalCount int           `json:"totalCount"`
	Details    []evidenceRaw `json:"details"`
}

type evidenceRaw struct {
	EvidenceType string     `json:"evidenceType"`
	DisplayName  string     `json:"displayName"`
	Entity       EntityStub `json:"entity"`
	RootCause    bool       `json:"rootCause"`
	StartTime    int64      `json:"startTime"` // epoch ms
}

func (c *Client) getEvidenceDetails(ctx context.Context, problemID string) []EvidenceItem {
	var raw struct {
		EvidenceDetails *evidenceDetailsRaw `json:"evidenceDetails"`
	}
	params := url.Values{"fields": {"+evidenceDetails"}}
	if err := c.get(ctx, "problems/"+problemID, params, &raw); err != nil || raw.EvidenceDetails == nil {
		return []EvidenceItem{}
	}

	items := make([]EvidenceItem, 0, len(raw.EvidenceDetails.Details))
	for _, ev := range raw.EvidenceDetails.Details {
		items = append(items, EvidenceItem{
			EvidenceType: ev.EvidenceType,
			DisplayName:  ev.DisplayName,
			EntityID:     ev.Entity.EntityID.ID,
			EntityName:   ev.Entity.BestName(),
			EntityType:   ev.Entity.EntityID.Type,
			RootCause:    ev.RootCause,
			StartTime:    time.UnixMilli(ev.StartTime),
		})
	}
	return items
}

// ─── Topologia ────────────────────────────────────────────────────────────────

func (c *Client) getEntityTopology(entity *Entity) []TopoRelation {
	seen := map[string]bool{}
	var relations []TopoRelation

	add := func(stub EntityStub, dir string) {
		if seen[stub.EntityID.ID] {
			return
		}
		seen[stub.EntityID.ID] = true
		relations = append(relations, TopoRelation{
			EntityID:   stub.EntityID.ID,
			EntityName: stub.BestName(),
			EntityType: stub.EntityID.Type,
			Direction:  dir,
		})
	}

	// toRelationships.calls = entidades que ESTA entidade chama (downstream / outgoing)
	for _, stub := range entity.ToRelationships["calls"] {
		add(stub, "outgoing")
	}
	// fromRelationships.calledBy = entidades que chamam ESTA entidade (upstream / incoming)
	for _, stub := range entity.FromRelationships["calledBy"] {
		add(stub, "incoming")
	}
	return relations
}

// ─── Distributed Traces ───────────────────────────────────────────────────────

// tracesResponseRaw resposta da API /api/v2/distributed-tracing/traces
type tracesResponseRaw struct {
	TotalCount int `json:"totalCount"`
	Traces     []struct {
		TraceID   string `json:"traceId"`
		StartTime string `json:"startTime"` // ISO-8601 na v2
		Duration  int64  `json:"duration"`  // microseconds
		// Spans inclui o root span (primeiro item geralmente é o entry point)
		Spans []struct {
			Name       string            `json:"name"`
			Kind       string            `json:"kind"` // SERVER | CLIENT | PRODUCER | CONSUMER
			Attributes map[string]string `json:"attributes"`
		} `json:"spans"`
	} `json:"traces"`
}

// getServiceTraces busca distributed traces do serviço raiz.
// Endpoint: /api/v2/distributed-tracing/traces (requer escopo DataExport ou CaptureRequestData).
// Retorna os traces e um erro descritivo se falhar (403, 404, etc.).
func (c *Client) getServiceTraces(ctx context.Context, entityID, from, to string) ([]TraceEntry, string) {
	// A API v2 usa query string com filter, não entitySelector
	params := url.Values{
		"from":     {from},
		"to":       {to},
		"pageSize": {"50"},
		// Filtra pelo entity ID do serviço de entrada
		"query": {fmt.Sprintf(`dt.entity.service="%s"`, entityID)},
	}

	var raw tracesResponseRaw
	if err := c.get(ctx, "distributed-tracing/traces", params, &raw); err != nil {
		return []TraceEntry{}, err.Error()
	}

	entries := make([]TraceEntry, 0, len(raw.Traces))
	for _, t := range raw.Traces {
		startTime, _ := time.Parse(time.RFC3339, t.StartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}

		entry := TraceEntry{
			TraceID:    t.TraceID,
			StartTime:  startTime,
			DurationMs: float64(t.Duration) / 1000.0,
		}

		// Procura o span SERVER ou o primeiro span para extrair nome/HTTP
		for _, span := range t.Spans {
			if span.Kind == "SERVER" || entry.Name == "" {
				entry.Name = span.Name
				if v, ok := span.Attributes["http.method"]; ok {
					entry.HTTPMethod = v
				}
				if v, ok := span.Attributes["http.status_code"]; ok {
					var code int
					fmt.Sscanf(v, "%d", &code)
					entry.HTTPStatus = code
					entry.HasError = code >= 400
				}
				if span.Kind == "SERVER" {
					break
				}
			}
		}

		if entry.Name != "" {
			entries = append(entries, entry)
		}
	}
	return entries, ""
}

// ─── GetProblemContext — API pública ──────────────────────────────────────────

// GetProblemContext coleta evidências Davis, eventos, topologia e traces em paralelo.
func (c *Client) GetProblemContext(ctx context.Context, problem *Problem) *ProblemContext {
	from := problem.StartTime.Add(-10 * time.Minute)
	to := time.Now()
	if problem.EndTime != nil {
		if candidate := problem.EndTime.Add(15 * time.Minute); candidate.Before(to) {
			to = candidate
		}
	}
	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)

	result := &ProblemContext{
		ProblemID:    problem.ProblemID,
		TimeFrom:     from,
		TimeTo:       to,
		ProblemStart: problem.StartTime,
		Evidence:     []EvidenceItem{},
		Events:       []ContextEventEntry{},
		Topology:     []TopoRelation{},
		Traces:       []TraceEntry{},
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// 1. Evidências Davis AI
	wg.Add(1)
	go func() {
		defer wg.Done()
		items := c.getEvidenceDetails(ctx, problem.ProblemID)
		mu.Lock()
		result.Evidence = items
		mu.Unlock()
	}()

	// 2. Eventos de todas as entidades afetadas (desduplicados, ordenados desc)
	wg.Add(1)
	go func() {
		defer wg.Done()
		seen := map[string]bool{}
		var all []ContextEventEntry
		for _, stub := range problem.AffectedEntities {
			evs, err := c.GetEntityEvents(ctx, stub.EntityID.ID, from, to)
			if err != nil {
				continue
			}
			for _, ev := range evs {
				if !seen[ev.EventID] {
					seen[ev.EventID] = true
					all = append(all, ContextEventEntry{
						EventID:    ev.EventID,
						EventType:  ev.EventType,
						Title:      ev.Title,
						StartTime:  ev.StartTime,
						EntityID:   ev.EntityID,
						EntityName: ev.EntityName,
					})
				}
			}
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].StartTime.After(all[j].StartTime)
		})
		mu.Lock()
		result.Events = all
		mu.Unlock()
	}()

	// 3. Topologia — da entidade raiz (ou primeiro serviço afetado)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var entityID string
		if problem.RootCauseEntity != nil {
			entityID = problem.RootCauseEntity.EntityID.ID
		} else {
			for _, s := range problem.AffectedEntities {
				if s.EntityID.Type == "SERVICE" {
					entityID = s.EntityID.ID
					break
				}
			}
		}
		if entityID == "" {
			return
		}
		entity, err := c.GetEntity(ctx, entityID)
		if err != nil {
			return
		}
		mu.Lock()
		result.Topology = c.getEntityTopology(entity)
		mu.Unlock()
	}()

	// 4. Traces HTTP — para o serviço raiz (requer traces.read)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var entityID string
		if problem.RootCauseEntity != nil && problem.RootCauseEntity.EntityID.Type == "SERVICE" {
			entityID = problem.RootCauseEntity.EntityID.ID
		} else {
			for _, s := range problem.AffectedEntities {
				if s.EntityID.Type == "SERVICE" {
					entityID = s.EntityID.ID
					break
				}
			}
		}
		if entityID == "" {
			return
		}
		traces, tracesErr := c.getServiceTraces(ctx, entityID, fromStr, toStr)
		mu.Lock()
		result.Traces = traces
		result.TracesError = tracesErr
		mu.Unlock()
	}()

	wg.Wait()
	return result
}
