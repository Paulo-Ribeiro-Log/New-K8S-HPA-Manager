package finops

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Showback (FINOPS-REVISAO-PLAN.md, Fase 3): custo por namespace e dono, por mês.

// namespaceOwnerLabels: labels de namespace aceitos como "dono", na ordem de preferência.
var namespaceOwnerLabels = []string{
	"owner", "team", "squad", "time", "responsavel", "dono",
	"cost-center", "costcenter", "centro-de-custo",
	"app.kubernetes.io/part-of", "jornada", "journey",
}

// ownerFromLabels devolve o dono e o label usado ("" quando nenhum label conhecido existe).
func ownerFromLabels(labels map[string]string) (owner, label string) {
	lower := make(map[string]string, len(labels))
	for k, v := range labels {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range namespaceOwnerLabels {
		if v := strings.TrimSpace(lower[k]); v != "" {
			return v, k
		}
	}
	return "", ""
}

// attachNamespaceOwners preenche Owner/OwnerLabel pelos labels dos namespaces. Best-effort: sem
// permissão de listar namespaces, o showback agrupa só por namespace.
func attachNamespaceOwners(ctx context.Context, client kubernetes.Interface, namespaces []FinOpsNamespace) {
	if client == nil || len(namespaces) == 0 {
		return
	}
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	labels := make(map[string]map[string]string, len(list.Items))
	for _, ns := range list.Items {
		labels[ns.Name] = ns.Labels
	}
	for i := range namespaces {
		namespaces[i].Owner, namespaces[i].OwnerLabel = ownerFromLabels(labels[namespaces[i].Namespace])
	}
}

// poolFactors: fator efetivo ÷ tabela por pool (1 sem cobertura) e se o pool está sob compromisso
// (reserva/Savings Plan/Spot) — mesma regra de capacity.ts.
func poolFactors(r *FinOpsReport) (factor map[string]float64, committed map[string]bool) {
	factor, committed = map[string]float64{}, map[string]bool{}
	for _, p := range r.NodePools {
		f := 1.0
		if eff := effectiveMonthlyBRL(p.Coverage); eff > 0 && p.MonthlyCostBRL > 0 {
			f = eff / p.MonthlyCostBRL
		}
		factor[p.Name] = f
		committed[p.Name] = isCommitted(p.Coverage)
	}
	return factor, committed
}

// UnallocatedNamespace é a linha do custo de nodes que nenhum workload ocupa (ociosa, sistema,
// DaemonSets fora do relatório) — sem ela, a soma do showback não fecha com o custo do cluster.
const UnallocatedNamespace = "(não alocado)"

// ShowbackRow é o custo mensal de um namespace.
type ShowbackRow struct {
	Cluster    string  `json:"cluster"`
	Namespace  string  `json:"namespace"`
	Owner      string  `json:"owner,omitempty"`
	OwnerLabel string  `json:"owner_label,omitempty"`
	Workloads  int     `json:"workloads"`
	ComputeBRL float64 `json:"compute_brl"` // custo efetivo do compute dos workloads
	StorageBRL float64 `json:"storage_brl"` // PVCs do namespace (tabela)
	WasteBRL   float64 `json:"waste_brl"`   // request acima do uso (efetivo, limitado ao custo)
	TotalBRL   float64 `json:"total_brl"`
}

// BuildShowback monta o showback do relatório (já com cobertura e isenções aplicadas).
func BuildShowback(r *FinOpsReport) []ShowbackRow {
	factor, _ := poolFactors(r)
	f := func(pool string) float64 {
		if v, ok := factor[pool]; ok {
			return v
		}
		return 1
	}
	rows := map[string]*ShowbackRow{}
	get := func(ns string) *ShowbackRow {
		if rows[ns] == nil {
			rows[ns] = &ShowbackRow{Cluster: r.Cluster, Namespace: ns}
		}
		return rows[ns]
	}
	for _, n := range r.Namespaces {
		row := get(n.Namespace)
		row.Owner, row.OwnerLabel = n.Owner, n.OwnerLabel
	}
	allocated := 0.0
	for _, w := range r.Workloads {
		row := get(w.Namespace)
		row.Workloads++
		c := w.CostShareBRL * f(w.NodePool)
		row.ComputeBRL += c
		allocated += c
		row.WasteBRL += math.Min(w.WasteBRL, math.Max(w.CostShareBRL, 0)) * f(w.NodePool)
	}
	osBRL, _, pvcByNS := diskCosts(r)
	for ns, v := range pvcByNS {
		get(ns).StorageBRL += v
	}
	compute := 0.0
	for _, p := range r.NodePools {
		compute += p.MonthlyCostBRL * f(p.Name)
	}
	if rest := compute + osBRL - allocated; rest >= 1 {
		get(UnallocatedNamespace).ComputeBRL = rest
	}
	out := make([]ShowbackRow, 0, len(rows))
	for _, row := range rows {
		row.ComputeBRL, row.StorageBRL, row.WasteBRL = round2(row.ComputeBRL), round2(row.StorageBRL), round2(row.WasteBRL)
		row.TotalBRL = round2(row.ComputeBRL + row.StorageBRL)
		if row.TotalBRL > 0 || row.Workloads > 0 {
			out = append(out, *row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalBRL > out[j].TotalBRL })
	return out
}

// ShowbackSnapshot é a foto do showback de um dia (ritmo mensal do último scan daquele dia).
type ShowbackSnapshot struct {
	Day  time.Time // meia-noite local do dia
	Rows []ShowbackRow
}

// AccrueShowback soma o custo dia a dia do mês: cada dia, de from até until (inclusive), usa a foto
// mais recente até ele, ao ritmo de 12/365 do valor mensal. Dias antes da primeira foto não entram
// (devolvidos em daysCovered). snaps precisa estar em ordem de dia.
func AccrueShowback(snaps []ShowbackSnapshot, from, until time.Time) (rows []ShowbackRow, daysCovered int) {
	if len(snaps) == 0 || until.Before(from) {
		return nil, 0
	}
	const perDay = 12.0 / 365
	acc := map[string]*ShowbackRow{}
	i := -1
	for d := from; !d.After(until); d = d.AddDate(0, 0, 1) {
		for i+1 < len(snaps) && !snaps[i+1].Day.After(d) {
			i++
		}
		if i < 0 {
			continue
		}
		daysCovered++
		for _, r := range snaps[i].Rows {
			k := r.Cluster + "/" + r.Namespace
			a := acc[k]
			if a == nil {
				a = &ShowbackRow{Cluster: r.Cluster, Namespace: r.Namespace}
				acc[k] = a
			}
			a.Owner, a.OwnerLabel, a.Workloads = r.Owner, r.OwnerLabel, r.Workloads
			a.ComputeBRL += r.ComputeBRL * perDay
			a.StorageBRL += r.StorageBRL * perDay
			a.WasteBRL += r.WasteBRL * perDay
			a.TotalBRL += r.TotalBRL * perDay
		}
	}
	for _, a := range acc {
		a.ComputeBRL, a.StorageBRL, a.WasteBRL, a.TotalBRL = round2(a.ComputeBRL), round2(a.StorageBRL), round2(a.WasteBRL), round2(a.TotalBRL)
		rows = append(rows, *a)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TotalBRL > rows[j].TotalBRL })
	return rows, daysCovered
}
