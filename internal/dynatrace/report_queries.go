package dynatrace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Consultas do relatório de métricas (internal/monitoring/report): resolver um Deployment até os
// serviços de ENTRADA do Dynatrace e ler as séries deles. Somente leitura.
//
// O nome do serviço no Dynatrace NÃO identifica a aplicação (há dezenas de entidades com o mesmo
// nome, de bancos, threads em segundo plano e outros ambientes). O elo confiável é o PROCESS_GROUP,
// cujo displayName segue "<tecnologia> - <deployment> - <namespace>"; os serviços vêm de
// toRelationships.runsOn (confirmado contra o frete-hub real).

// ErrNoProcessGroup: nenhum process group do Dynatrace casa com o Deployment/namespace.
var ErrNoProcessGroup = errors.New("nenhum process group do Dynatrace casa com este Deployment e namespace")

// ServiceRef é um serviço de entrada resolvido.
type ServiceRef struct {
	ID   string
	Name string
	Type string // WEB_REQUEST_SERVICE, WEB_SERVICE, RPC_SERVICE
}

// entryServiceTypes: só estes recebem tráfego de clientes. DATABASE_SERVICE, consumidores de fila e
// "Requests executed in background threads" ficam de fora.
var entryServiceTypes = map[string]bool{"WEB_REQUEST_SERVICE": true, "WEB_SERVICE": true, "RPC_SERVICE": true}

const backgroundThreadsMarker = "requests executed in background threads"

// k8sNameRe: nomes de Deployment/namespace (DNS-1123). Vai para um entitySelector, então nada de
// aspas nem caracteres especiais.
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// ResolveEntryServices acha os serviços de entrada do Deployment. Dois caminhos, nesta ordem:
//
//  1. nome do process group `<tecnologia> - <deployment> - <namespace>` (1 consulta, barato; vale para a
//     maioria, mas é convenção de nomeação e NÃO vale para todos — o frete-b2c, com 70 réplicas
//     monitoradas, não segue);
//  2. metadados Kubernetes dos PROCESS_GROUP_INSTANCE do cluster (KUBERNETES_NAMESPACE e
//     KUBERNETES_BASE_POD_NAME, derivados do nome do pod pelo próprio OneAgent), o mesmo elo que o app
//     já usa no overlay de problems e na cobertura. Mais pesado (lista os processos do cluster), por
//     isso cacheado por cluster.
//
// Devolve também notas (como foi resolvido, serviços ignorados). Erro ErrNoProcessGroup quando nenhum
// dos dois acha o Deployment; a mensagem lista o que existe, para o usuário ver como ele aparece.
func (c *Client) ResolveEntryServices(ctx context.Context, cluster, namespace, workload string) ([]ServiceRef, []string, error) {
	namespace, workload = strings.ToLower(namespace), strings.ToLower(workload)
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(workload) {
		return nil, nil, fmt.Errorf("namespace ou deployment com nome inválido")
	}
	lk, err := c.lookupByPGName(ctx, namespace, workload)
	if err != nil {
		return nil, nil, err
	}
	ids, via := lk.ids, "nome do process group"
	found := lk.found
	var nsWorkloads []string
	var k8sNote string
	if !found {
		kids, wls, kvia, kfound, kerr := c.lookupByK8s(ctx, NormalizeClusterName(cluster), namespace, workload)
		nsWorkloads = wls
		switch {
		case kerr != nil:
			k8sNote = "metadados do Kubernetes indisponíveis: " + kerr.Error()
		case kfound:
			ids, found, via = kids, true, kvia
		}
	}
	if !found {
		return nil, nil, c.noProcessGroupError(ctx, lk.candidates, namespace, workload, nsWorkloads, k8sNote)
	}
	svcs, skipNotes, err := c.classifyEntryServices(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	return svcs, append([]string{"Deployment resolvido por " + via}, skipNotes...), nil
}

// pgNameLookup é o resultado do caminho 1.
type pgNameLookup struct {
	ids        []string
	found      bool     // algum PG casou o nome exato
	candidates []Entity // PGs que só contêm o nome (para a pista do erro)
}

func (c *Client) lookupByPGName(ctx context.Context, namespace, workload string) (pgNameLookup, error) {
	selector := fmt.Sprintf(`type("PROCESS_GROUP"),entityName.contains("%s")`, workload)
	pgs, err := c.listRawEntities(ctx, selector, "properties,toRelationships")
	if err != nil {
		return pgNameLookup{}, err
	}
	// nome exato "<tecnologia> - <deployment> - <namespace>": um contains pegaria também
	// "frete-hub-regionalizado", que é outro Deployment
	suffix := " - " + workload + " - " + namespace
	out := pgNameLookup{}
	seen := map[string]bool{}
	for _, pg := range pgs {
		if !strings.HasSuffix(strings.ToLower(pg.DisplayName), suffix) {
			out.candidates = append(out.candidates, pg)
			continue
		}
		out.found = true
		for _, s := range pg.ToRelationships["runsOn"] {
			if id := s.EntityID.ID; id != "" && !seen[id] {
				seen[id] = true
				out.ids = append(out.ids, id)
			}
		}
	}
	return out, nil
}

// classifyEntryServices busca os serviços e fica só com os pontos de entrada.
func (c *Client) classifyEntryServices(ctx context.Context, ids []string) ([]ServiceRef, []string, error) {
	var out []ServiceRef
	var skipped []string
	for i := 0; i < len(ids); i += 50 {
		end := i + 50
		if end > len(ids) {
			end = len(ids)
		}
		quoted := make([]string, 0, end-i)
		for _, id := range ids[i:end] {
			quoted = append(quoted, `"`+id+`"`)
		}
		svcs, err := c.listRawEntities(ctx, "entityId("+strings.Join(quoted, ",")+")", "properties")
		if err != nil {
			return nil, nil, err
		}
		for _, s := range svcs {
			typ, _ := s.Properties["serviceType"].(string)
			if !entryServiceTypes[typ] || strings.Contains(strings.ToLower(s.DisplayName), backgroundThreadsMarker) {
				skipped = append(skipped, fmt.Sprintf("%s (%s)", s.DisplayName, typ))
				continue
			}
			out = append(out, ServiceRef{ID: s.EntityID, Name: s.DisplayName, Type: typ})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	var notes []string
	if len(skipped) > 0 {
		sort.Strings(skipped)
		notes = append(notes, "serviços ignorados (não são ponto de entrada): "+strings.Join(skipped, "; "))
	}
	return out, notes, nil
}

// ── caminho 2: metadados Kubernetes dos processos do cluster ──────────────────────────────────────

// k8sProc é um processo monitorado (PROCESS_GROUP_INSTANCE) reduzido ao que o relatório usa.
type k8sProc struct {
	Namespace, Workload, PGID string
	ServiceIDs                []string
	// Tags são os valores das tags do processo (ou a chave, se não têm valor), em minúsculas: o
	// fallback de busca por tag compara o nome do Deployment com elas.
	Tags []string
}

type k8sProcEntry struct {
	procs []k8sProc
	at    time.Time
}

const k8sProcTTL = 10 * time.Minute

var (
	k8sProcCache  sync.Map // "<baseURL>|<cluster>" -> k8sProcEntry
	k8sProcFlight singleflight.Group
)

// clusterProcesses lista (uma vez por cluster a cada 10 min, mesmo com vários itens em paralelo) os
// processos do cluster com namespace, Deployment e serviços. cluster sem HOST_GROUP -> nil, sem erro.
func (c *Client) clusterProcesses(ctx context.Context, cluster string) ([]k8sProc, error) {
	key := c.baseURL + "|" + cluster
	if raw, ok := k8sProcCache.Load(key); ok {
		if e := raw.(k8sProcEntry); time.Since(e.at) < k8sProcTTL {
			return e.procs, nil
		}
	}
	v, err, _ := k8sProcFlight.Do(key, func() (interface{}, error) {
		hostGroupID, err := c.resolveHostGroupEntityID(ctx, cluster)
		if err != nil {
			return nil, err
		}
		if hostGroupID == "" {
			return []k8sProc(nil), nil // cluster sem HOST_GROUP: não monitorado desta forma
		}
		selector := fmt.Sprintf(`type("PROCESS_GROUP_INSTANCE"),toRelationships.isHostGroupOf(entityId("%s"))`, hostGroupID)
		pgis, err := c.listRawEntities(ctx, selector, "+tags,+properties,+fromRelationships,+toRelationships")
		if err != nil {
			return nil, err
		}
		var procs []k8sProc
		for i := range pgis {
			e := &pgis[i]
			corr := e.ExtractK8sCorrelation()
			if corr == nil || corr.Namespace == "" || corr.Workload == "" {
				continue
			}
			p := k8sProc{Namespace: corr.Namespace, Workload: corr.Workload, PGID: firstRelationshipID(e.FromRelationships["isInstanceOf"]), Tags: tagValues(e.Tags)}
			for _, s := range e.ToRelationships["runsOnProcessGroupInstance"] {
				if id := s.EntityID.ID; id != "" {
					p.ServiceIDs = append(p.ServiceIDs, id)
				}
			}
			procs = append(procs, p)
		}
		k8sProcCache.Store(key, k8sProcEntry{procs: procs, at: time.Now()})
		return procs, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]k8sProc), nil
}

// workloadMatches compara o nome do Deployment vindo do Dynatrace com o pedido. O OneAgent às vezes
// generaliza dígitos do nome com "*" (achado real: o Deployment frete-b2c aparece como "frete-b*c"),
// então um "*" no nome do Dynatrace casa com uma sequência de dígitos. Sem "*", é igualdade exata
// (sem diferenciar maiúsculas).
func workloadMatches(dtName, workload string) bool {
	if !strings.Contains(dtName, "*") {
		return strings.EqualFold(dtName, workload)
	}
	parts := strings.Split(strings.ToLower(dtName), "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	re, err := regexp.Compile("^" + strings.Join(parts, "[0-9]+") + "$")
	return err == nil && re.MatchString(strings.ToLower(workload))
}

// tagValues devolve o valor de cada tag (ou a chave, quando a tag não tem valor), em minúsculas e sem
// repetição, limitado a 30.
func tagValues(tags []Tag) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		v := strings.ToLower(strings.TrimSpace(t.Value))
		if v == "" {
			v = strings.ToLower(strings.TrimSpace(t.Key))
		}
		if v != "" && !seen[v] && len(out) < 30 {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// lookupByK8s resolve o Deployment pelos processos do cluster. Devolve os ids de serviço (dos
// processos e dos process groups deles), os Deployments vistos no namespace (para a pista do erro) e
// se o Deployment foi achado.
func (c *Client) lookupByK8s(ctx context.Context, cluster, namespace, workload string) (ids []string, nsWorkloads []string, via string, found bool, err error) {
	procs, err := c.clusterProcesses(ctx, cluster)
	if err != nil {
		return nil, nil, "", false, err
	}
	const viaMeta = "metadados Kubernetes dos processos (KUBERNETES_BASE_POD_NAME)"
	const viaTag = "tag do Dynatrace nos processos do namespace (pode agrupar mais de um Deployment: confira os serviços listados)"

	wlSeen := map[string]bool{}
	for _, p := range procs {
		if strings.EqualFold(p.Namespace, namespace) && !wlSeen[strings.ToLower(p.Workload)] {
			wlSeen[strings.ToLower(p.Workload)] = true
			nsWorkloads = append(nsWorkloads, p.Workload)
		}
	}
	sort.Strings(nsWorkloads)

	// 1º pelos metadados (o nome do Deployment que o OneAgent deriva do pod); só se nenhum processo casar,
	// 2º pelas tags (um valor de tag igual ao nome do Deployment, no mesmo namespace)
	match := func(p k8sProc, byTag bool) bool {
		if !strings.EqualFold(p.Namespace, namespace) {
			return false
		}
		if !byTag {
			return workloadMatches(p.Workload, workload)
		}
		for _, t := range p.Tags {
			if workloadMatches(t, workload) {
				return true
			}
		}
		return false
	}
	var picked []k8sProc
	for _, byTag := range []bool{false, true} {
		for _, p := range procs {
			if match(p, byTag) {
				picked = append(picked, p)
			}
		}
		if len(picked) > 0 {
			via = viaMeta
			if byTag {
				via = viaTag
			}
			break
		}
	}
	if len(picked) == 0 {
		return nil, nsWorkloads, "", false, nil
	}

	svcSeen, pgSeen := map[string]bool{}, map[string]bool{}
	var pgIDs []string
	for _, p := range picked {
		for _, id := range p.ServiceIDs {
			if !svcSeen[id] {
				svcSeen[id] = true
				ids = append(ids, id)
			}
		}
		if p.PGID != "" && !pgSeen[p.PGID] {
			pgSeen[p.PGID] = true
			pgIDs = append(pgIDs, p.PGID)
		}
	}
	// os serviços também podem estar ligados ao process group (não só aos processos)
	for i := 0; i < len(pgIDs); i += 50 {
		end := i + 50
		if end > len(pgIDs) {
			end = len(pgIDs)
		}
		quoted := make([]string, 0, end-i)
		for _, id := range pgIDs[i:end] {
			quoted = append(quoted, `"`+id+`"`)
		}
		pgs, perr := c.listRawEntities(ctx, "entityId("+strings.Join(quoted, ",")+")", "toRelationships")
		if perr != nil {
			return nil, nsWorkloads, via, true, perr
		}
		for _, pg := range pgs {
			for _, s := range pg.ToRelationships["runsOn"] {
				if id := s.EntityID.ID; id != "" && !svcSeen[id] {
					svcSeen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	return ids, nsWorkloads, via, true, nil
}

// noProcessGroupError monta o erro "sem process group" com a pista do que existe no Dynatrace, para o
// usuário ver como a aplicação aparece lá: PGs com o nome do Deployment (que não casaram o padrão
// exato) ou, se não houver, PGs do mesmo namespace.
func (c *Client) noProcessGroupError(ctx context.Context, byName []Entity, namespace, workload string, nsWorkloads []string, k8sNote string) error {
	limit := func(in []string, max int) []string {
		sort.Strings(in)
		if len(in) > max {
			in = append(in[:max], fmt.Sprintf("… (+%d)", len(in)-max))
		}
		return in
	}
	var parts []string
	if len(byName) > 0 {
		var n []string
		for _, e := range byName {
			n = append(n, e.DisplayName)
		}
		parts = append(parts, fmt.Sprintf("process groups com %q no nome: %s", workload, strings.Join(limit(n, 10), " | ")))
	}
	if len(nsWorkloads) > 0 {
		parts = append(parts, fmt.Sprintf("Deployments com processo monitorado no namespace %s (metadados Kubernetes): %s", namespace, strings.Join(limit(nsWorkloads, 60), " | ")))
	}
	if len(parts) == 0 {
		// nada com o nome do Deployment e sem processos: lista os PGs do namespace (nome termina em " - <namespace>")
		if all, err := c.listRawEntities(ctx, fmt.Sprintf(`type("PROCESS_GROUP"),entityName.contains("%s")`, namespace), "properties"); err == nil {
			suffix := " - " + namespace
			var n []string
			for _, e := range all {
				if strings.HasSuffix(strings.ToLower(e.DisplayName), suffix) {
					n = append(n, e.DisplayName)
				}
			}
			if len(n) > 0 {
				parts = append(parts, fmt.Sprintf("nenhum process group tem %q no nome, os do namespace %s são: %s", workload, namespace, strings.Join(limit(n, 60), " | ")))
			}
		}
	}
	if k8sNote != "" {
		parts = append(parts, k8sNote)
	}
	if len(parts) == 0 {
		return ErrNoProcessGroup
	}
	return fmt.Errorf("%w; %s", ErrNoProcessGroup, strings.Join(parts, "; "))
}

// NullableSeries é uma série do Dynatrace: Values[i] é NaN quando o ponto veio nulo (sem dado —
// nunca confundir com zero).
type NullableSeries struct {
	Times  []int64 // epoch em ms
	Values []float64
}

// QueryServiceSeries lê uma métrica por serviço (dimensão dt.entity.service) na janela e na
// resolução pedidas (ex.: "1m"). selector é o metricSelector SEM o splitBy (ele é acrescentado).
// scale multiplica os valores (ex.: 0.001 para microssegundos → milissegundos).
func (c *Client) QueryServiceSeries(ctx context.Context, selector string, serviceIDs []string, from, to time.Time, resolution string, scale float64) (map[string]NullableSeries, error) {
	out := map[string]NullableSeries{}
	if len(serviceIDs) == 0 {
		return out, nil
	}
	quoted := make([]string, len(serviceIDs))
	for i, id := range serviceIDs {
		quoted[i] = `"` + id + `"`
	}
	// splitBy ANTES da agregação: `:splitBy("dt.entity.service"):sum`; o percentil entra no selector
	params := url.Values{
		"metricSelector": {selector},
		"entitySelector": {"entityId(" + strings.Join(quoted, ",") + ")"},
		"from":           {strconv.FormatInt(from.UnixMilli(), 10)},
		"to":             {strconv.FormatInt(to.UnixMilli(), 10)},
		"resolution":     {resolution},
	}
	var resp struct {
		Result []struct {
			Data []struct {
				DimensionMap map[string]string `json:"dimensionMap"`
				Timestamps   []int64           `json:"timestamps"`
				Values       []*float64        `json:"values"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.get(ctx, "metrics/query", params, &resp); err != nil {
		return nil, err
	}
	if scale == 0 {
		scale = 1
	}
	for _, r := range resp.Result {
		for _, d := range r.Data {
			id := d.DimensionMap["dt.entity.service"]
			if id == "" {
				continue
			}
			s := NullableSeries{Times: d.Timestamps, Values: make([]float64, len(d.Values))}
			for i, v := range d.Values {
				if v == nil {
					s.Values[i] = math.NaN()
				} else {
					s.Values[i] = *v * scale
				}
			}
			out[id] = s
		}
	}
	return out, nil
}
