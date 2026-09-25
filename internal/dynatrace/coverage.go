package dynatrace

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cobertura de Deep Monitoring por namespace — tradução para a API clássica v2 (Api-Token dt0c01)
// da DQL do dashboard de cobertura (smartscapeNodes "PROCESS" → join SERVICE runs_on → join
// ONEAGENT). A DQL exige Grail + platform token (dt0s16), que o perfil do usuário não tem; aqui:
//
//	PROCESS (smartscape)            → PROCESS_GROUP_INSTANCE do host group do cluster
//	dt.process_group.detected_name  → PROCESS_GROUP.properties.detectedName (fromRelationships.isInstanceOf)
//	references[runs_on.host]        → HOST (fromRelationships.isProcessOf)
//	SERVICE runs_on PROCESS         → SERVICE (toRelationships.runsOnProcessGroupInstance)
//	agentTechnologyType             → SERVICE.properties.agentTechnologyType
//	ONEAGENT dt.agent.module.version→ HOST.properties (ver hostAgentVersion)
//
// Semântica mantida da DQL: o join com SERVICE é inner — processo sem serviço não entra nas linhas
// (contado à parte em ProcessesWithoutService, para não sumir em silêncio).

// CoverageRow é uma linha do relatório — mesmas colunas da DQL.
type CoverageRow struct {
	Namespace            string `json:"namespace"`
	ServiceName          string `json:"service_name"`
	Technology           string `json:"technology"`
	OneAgentVersion      string `json:"oneagent_version"`
	DeepMonitoringStatus string `json:"deep_monitoring_status"`
	HostCount            int    `json:"host_count"`
	PodCount             int    `json:"pod_count"`
}

// CoverageReport é o retorno de GetDeepMonitoringCoverage.
type CoverageReport struct {
	Cluster                 string        `json:"cluster"`
	HostGroupFound          bool          `json:"host_group_found"`
	Rows                    []CoverageRow `json:"rows"`
	ProcessesWithoutService int           `json:"processes_without_service"`
	GeneratedAt             time.Time     `json:"generated_at"`
}

const (
	CoverageStatusActive     = "Ativo"
	CoverageStatusUnresolved = "Nao resolvido"
)

// coverageExcludedNamespacePrefixes = os `not matchesValue(k8s_namespace, "<x>*")` da DQL.
var coverageExcludedNamespacePrefixes = []string{"kube-system", "dynatrace", "istio-system", "calico-system"}

// PodCoverageProcess é um processo monitorado dentro de um pod, usado no tooltip do ícone DT
// das listagens de pods. Status "Sem servico" = processo sem serviço detectado (fica fora da
// tabela da Cobertura, mesma semântica de inner join da DQL, mas aparece aqui).
type PodCoverageProcess struct {
	ProcessName          string `json:"process_name"`
	Technology           string `json:"technology"`
	DeepMonitoringStatus string `json:"deep_monitoring_status"`
}

// PodCoverage é o detalhe de deep monitoring de um pod (chave "namespace/pod").
type PodCoverage struct {
	OneAgentVersion string               `json:"oneagent_version"`
	Processes       []PodCoverageProcess `json:"processes"`
}

const CoverageStatusNoService = "Sem servico"

var coverageCache sync.Map // chave: baseURL|cluster → coverageCacheEntry

const coverageCacheTTL = 5 * time.Minute

type coverageCacheEntry struct {
	report   *CoverageReport
	pods     map[string]*PodCoverage
	cachedAt time.Time
}

// GetDeepMonitoringCoverage monta o relatório de cobertura do cluster (nome já normalizado —
// NormalizeClusterName — igual ao host group do DynaKube). refresh ignora o cache de 5 min.
func (c *Client) GetDeepMonitoringCoverage(ctx context.Context, clusterName string, refresh bool) (*CoverageReport, error) {
	entry, err := c.coverage(ctx, clusterName, refresh)
	if err != nil {
		return nil, err
	}
	return entry.report, nil
}

// GetPodCoverage devolve o detalhe por pod ("namespace/pod") da mesma coleta — e do mesmo cache —
// de GetDeepMonitoringCoverage. Inclui todos os namespaces (a exclusão de kube-system etc. vale só
// para o relatório). hostGroupFound=false → cluster sem OneAgent clássico (mapa vazio).
func (c *Client) GetPodCoverage(ctx context.Context, clusterName string) (pods map[string]*PodCoverage, hostGroupFound bool, err error) {
	entry, err := c.coverage(ctx, clusterName, false)
	if err != nil {
		return nil, false, err
	}
	return entry.pods, entry.report.HostGroupFound, nil
}

// coverageProcess é um PROCESS_GROUP_INSTANCE já resolvido (nome, versão, tecnologias).
type coverageProcess struct {
	id, namespace, podName, hostID, name, version string
	techs                                         []string // uma por serviço; "" = serviço sem agentTechnologyType
}

func (c *Client) coverage(ctx context.Context, clusterName string, refresh bool) (coverageCacheEntry, error) {
	cacheKey := c.baseURL + "|" + clusterName
	if !refresh {
		if raw, ok := coverageCache.Load(cacheKey); ok {
			entry := raw.(coverageCacheEntry)
			if time.Since(entry.cachedAt) < coverageCacheTTL {
				return entry, nil
			}
		}
	}

	procs, hostGroupFound, err := c.collectCoverageProcesses(ctx, clusterName)
	if err != nil {
		return coverageCacheEntry{}, err
	}
	entry := coverageCacheEntry{
		report:   aggregateCoverage(clusterName, hostGroupFound, procs),
		pods:     podCoverageMap(procs),
		cachedAt: time.Now(),
	}
	coverageCache.Store(cacheKey, entry)
	return entry, nil
}

func (c *Client) collectCoverageProcesses(ctx context.Context, clusterName string) ([]coverageProcess, bool, error) {
	hostGroupID, err := c.resolveHostGroupEntityID(ctx, clusterName)
	if err != nil {
		return nil, false, err
	}
	if hostGroupID == "" {
		return nil, false, nil // cluster sem HOST_GROUP no Dynatrace — não monitorado, não é erro
	}

	selector := fmt.Sprintf(`type("PROCESS_GROUP_INSTANCE"),toRelationships.isHostGroupOf(entityId("%s"))`, hostGroupID)
	pgis, err := c.listRawEntities(ctx, selector, "+tags,+properties,+fromRelationships,+toRelationships")
	if err != nil {
		return nil, true, err
	}

	type rawProc struct {
		coverageProcess
		pgID       string
		serviceIDs []string
	}
	var raws []rawProc
	hostIDs, pgIDs, serviceIDs := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for i := range pgis {
		e := &pgis[i]
		corr := e.ExtractK8sCorrelation()
		if corr == nil || corr.Namespace == "" {
			continue
		}
		p := rawProc{
			coverageProcess: coverageProcess{
				id:        e.EntityID,
				namespace: corr.Namespace,
				podName:   corr.PodName,
				hostID:    firstRelationshipID(e.FromRelationships["isProcessOf"]),
			},
			pgID: firstRelationshipID(e.FromRelationships["isInstanceOf"]),
		}
		for _, s := range e.ToRelationships["runsOnProcessGroupInstance"] {
			p.serviceIDs = append(p.serviceIDs, s.EntityID.ID)
			serviceIDs[s.EntityID.ID] = struct{}{}
		}
		if p.hostID != "" {
			hostIDs[p.hostID] = struct{}{}
		}
		if p.pgID != "" {
			pgIDs[p.pgID] = struct{}{}
		}
		raws = append(raws, p)
	}

	hosts, err := c.getEntitiesByIDs(ctx, keysOf(hostIDs))
	if err != nil {
		return nil, true, err
	}
	groups, err := c.getEntitiesByIDs(ctx, keysOf(pgIDs))
	if err != nil {
		return nil, true, err
	}
	services, err := c.getEntitiesByIDs(ctx, keysOf(serviceIDs))
	if err != nil {
		return nil, true, err
	}

	procs := make([]coverageProcess, 0, len(raws))
	for _, r := range raws {
		p := r.coverageProcess
		p.name = p.id
		if pg, ok := groups[r.pgID]; ok {
			p.name = propString(pg, "detectedName")
			if p.name == "" {
				p.name = pg.DisplayName
			}
		}
		if h, ok := hosts[p.hostID]; ok {
			p.version = hostAgentVersion(h)
		}
		for _, sid := range r.serviceIDs {
			tech := ""
			if svc, ok := services[sid]; ok {
				tech = propString(svc, "agentTechnologyType")
			}
			p.techs = append(p.techs, tech)
		}
		procs = append(procs, p)
	}
	return procs, true, nil
}

func coverageStatus(tech string) string {
	if tech != "" {
		return CoverageStatusActive
	}
	return CoverageStatusUnresolved
}

// aggregateCoverage = o summarize/sort da DQL, sobre os processos fora dos namespaces excluídos.
func aggregateCoverage(clusterName string, hostGroupFound bool, procs []coverageProcess) *CoverageReport {
	report := &CoverageReport{Cluster: clusterName, HostGroupFound: hostGroupFound, Rows: []CoverageRow{}, GeneratedAt: time.Now()}

	type rowKey struct{ ns, name, tech, version, status string }
	type rowSets struct{ hosts, procs map[string]struct{} }
	agg := map[rowKey]*rowSets{}
	for _, p := range procs {
		if isExcludedCoverageNamespace(p.namespace) {
			continue
		}
		if len(p.techs) == 0 {
			report.ProcessesWithoutService++
			continue
		}
		for _, tech := range p.techs {
			k := rowKey{p.namespace, p.name, tech, p.version, coverageStatus(tech)}
			sets := agg[k]
			if sets == nil {
				sets = &rowSets{hosts: map[string]struct{}{}, procs: map[string]struct{}{}}
				agg[k] = sets
			}
			if p.hostID != "" {
				sets.hosts[p.hostID] = struct{}{}
			}
			sets.procs[p.id] = struct{}{}
		}
	}

	for k, sets := range agg {
		report.Rows = append(report.Rows, CoverageRow{
			Namespace:            k.ns,
			ServiceName:          k.name,
			Technology:           k.tech,
			OneAgentVersion:      k.version,
			DeepMonitoringStatus: k.status,
			HostCount:            len(sets.hosts),
			PodCount:             len(sets.procs),
		})
	}
	// sort k8s_namespace asc, technology asc, process_name asc
	sort.Slice(report.Rows, func(i, j int) bool {
		a, b := report.Rows[i], report.Rows[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Technology != b.Technology {
			return a.Technology < b.Technology
		}
		if a.ServiceName != b.ServiceName {
			return a.ServiceName < b.ServiceName
		}
		return a.OneAgentVersion < b.OneAgentVersion
	})
	return report
}

// podCoverageMap agrupa os processos por pod ("namespace/pod", mesma chave de ListMonitoredPods),
// sem duplicar processo+tecnologia (um processo com N serviços da mesma tecnologia vira 1 linha).
func podCoverageMap(procs []coverageProcess) map[string]*PodCoverage {
	pods := map[string]*PodCoverage{}
	for _, p := range procs {
		if p.podName == "" {
			continue
		}
		key := p.namespace + "/" + p.podName
		pc := pods[key]
		if pc == nil {
			pc = &PodCoverage{}
			pods[key] = pc
		}
		if pc.OneAgentVersion == "" {
			pc.OneAgentVersion = p.version
		}
		if len(p.techs) == 0 {
			pc.Processes = appendUniqueProcess(pc.Processes, PodCoverageProcess{ProcessName: p.name, DeepMonitoringStatus: CoverageStatusNoService})
			continue
		}
		for _, tech := range p.techs {
			pc.Processes = appendUniqueProcess(pc.Processes, PodCoverageProcess{ProcessName: p.name, Technology: tech, DeepMonitoringStatus: coverageStatus(tech)})
		}
	}
	for _, pc := range pods {
		sort.Slice(pc.Processes, func(i, j int) bool {
			a, b := pc.Processes[i], pc.Processes[j]
			if a.ProcessName != b.ProcessName {
				return a.ProcessName < b.ProcessName
			}
			return a.Technology < b.Technology
		})
	}
	return pods
}

func appendUniqueProcess(list []PodCoverageProcess, p PodCoverageProcess) []PodCoverageProcess {
	for _, existing := range list {
		if existing == p {
			return list
		}
	}
	return append(list, p)
}

// coverageIDBatchSize/coverageIDConcurrency: entitySelector entityId("a","b",...) com 100 IDs
// (~3 KB de URL) e no máximo 4 requisições simultâneas — clusters grandes têm milhares de PGIs,
// mas hosts/process groups/serviços distintos ficam na casa das centenas.
const (
	coverageIDBatchSize   = 100
	coverageIDConcurrency = 4
)

// getEntitiesByIDs busca entidades (só properties) por ID, em lotes paralelos. Lote que falha é
// descartado (as linhas afetadas saem com versão/tecnologia vazias); só é erro se TODOS falharem.
func (c *Client) getEntitiesByIDs(ctx context.Context, ids []string) (map[string]*Entity, error) {
	out := map[string]*Entity{}
	if len(ids) == 0 {
		return out, nil
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		failed   int
		batches  int
	)
	sem := make(chan struct{}, coverageIDConcurrency)
	for start := 0; start < len(ids); start += coverageIDBatchSize {
		end := min(start+coverageIDBatchSize, len(ids))
		quoted := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			quoted = append(quoted, fmt.Sprintf("%q", id))
		}
		selector := "entityId(" + strings.Join(quoted, ",") + ")"
		batches++

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			entities, err := c.listRawEntities(ctx, selector, "+properties")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for i := range entities {
				out[entities[i].EntityID] = &entities[i]
			}
		}()
	}
	wg.Wait()

	if failed == batches {
		return nil, firstErr
	}
	return out, nil
}

// hostAgentVersion lê a versão do OneAgent de uma entidade HOST. O nome/formato exato da property
// na API clássica não foi validado contra o tenant (a DQL usa dt.agent.module.version do nó
// ONEAGENT) — tenta as chaves conhecidas, em string ou no formato {major,minor,revision}.
func hostAgentVersion(h *Entity) string {
	for _, key := range []string{"agentVersion", "installerVersion", "oneAgentVersion"} {
		switch v := h.Properties[key].(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]interface{}:
			major, okMaj := v["major"].(float64)
			minor, okMin := v["minor"].(float64)
			if okMaj && okMin {
				rev, _ := v["revision"].(float64)
				return fmt.Sprintf("%d.%d.%d", int(major), int(minor), int(rev))
			}
		}
	}
	return ""
}

func isExcludedCoverageNamespace(ns string) bool {
	lower := strings.ToLower(ns)
	for _, p := range coverageExcludedNamespacePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func firstRelationshipID(rels []EntityStub) string {
	if len(rels) == 0 {
		return ""
	}
	return rels[0].EntityID.ID
}

func propString(e *Entity, key string) string {
	s, _ := e.Properties[key].(string)
	return s
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
