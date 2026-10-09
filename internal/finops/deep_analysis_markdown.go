package finops

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RenderDeepAnalysisMarkdown gera o relatório Markdown da Deep Analysis de um pool (F5), no mesmo
// formato da análise manual que originou a feature: resumo, alocação, nodes, DaemonSets, workloads,
// HPAs, simulação de VMs, avisos e metodologia.
func RenderDeepAnalysisMarkdown(a PoolDeepAnalysis) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&b, format, args...) }
	ov, al, ds := a.Overview, a.Allocation, a.DaemonSets

	w("# Deep Analysis do node pool `%s` (%s)\n\n", a.Pool, a.Cluster)
	w("| | |\n|---|---|\n")
	w("| **Cluster** | `%s`%s |\n", a.Cluster, mdIfStr(ov.Region != "", " ("+ov.Region+")"))
	w("| **Node pool** | `%s`: %d nodes `%s` (%d vCPU / %d GB%s), %s%s, `maxPods` %d |\n",
		a.Pool, ov.Nodes, ov.VMSize, ov.VCPU, ov.MemGB, mdIfStr(ov.CPU != "", ", "+ov.CPU), deepPriorityLabel(ov.Priority),
		mdIfStr(ov.OSDisk != "", ", disco de SO "+ov.OSDisk), ov.MaxPods)
	if len(ov.Zones) > 0 {
		w("| **Zonas** | %s |\n", strings.Join(ov.Zones, ", "))
	}
	w("| **Pods em execução** | %d |\n", ov.PodsRunning)
	if ov.PriceUSDHour > 0 {
		w("| **Custo estimado** | R$ %s/mês (US$ %s/h por node, câmbio %s) |\n", mdBrl(ov.MonthlyCostBRL), mdNum(ov.PriceUSDHour, 3), mdNum(ov.ExchangeRate, 2))
	}
	w("| **Gerado em** | %s |\n", a.GeneratedAt.Format("02/01/2006 15:04"))
	w("| **Fontes** | %s |\n\n", deepSources(a))

	if !a.History.Available {
		w("> **Sem histórico:** os números de uso são uma foto do momento (metrics-server), não o pico. Rode \"Analisar\" no FinOps para ter P95 e picos históricos antes de aplicar mudanças.\n\n")
	}

	// 1. Resumo
	w("## 1. Resumo\n\n")
	if len(a.Findings) == 0 {
		w("Nenhum problema relevante encontrado.\n\n")
	}
	for i, f := range a.Findings {
		w("%d. **%s %s.** %s\n", i+1, mdSeverityIcon(f.Severity), strings.TrimSuffix(f.Title, "."), f.Detail)
	}
	w("\n")

	// 2. Alocação
	w("## 2. Alocação do pool\n\n")
	w("%s\n\n", al.Explanation)
	w("| | Alocável | Requests | Limits | Uso ao vivo (node) | Uso P95 (Σ workloads) |\n|---|---|---|---|---|---|\n")
	w("| **CPU** | %s | %s (%.0f%%) | %s (%.0f%%) | %s | %s |\n", mdCores(al.CPU.Allocatable),
		mdCores(al.CPU.Requests), al.CPU.RequestPct, mdCores(al.CPU.Limits), al.CPU.LimitPct,
		mdUsageCell(al.CPU.HasLive, mdCores(al.CPU.UsageLive), al.CPU.UsageLivePct), mdUsageCell(al.CPU.HasP95, mdCores(al.CPU.UsageP95), al.CPU.UsageP95Pct))
	w("| **Memória** | %s | %s (%.0f%%) | %s (%.0f%%) | %s | %s |\n\n", mdGib(al.Mem.Allocatable),
		mdGib(al.Mem.Requests), al.Mem.RequestPct, mdGib(al.Mem.Limits), al.Mem.LimitPct,
		mdUsageCell(al.Mem.HasLive, mdGib(al.Mem.UsageLive), al.Mem.UsageLivePct), mdUsageCell(al.Mem.HasP95, mdGib(al.Mem.UsageP95), al.Mem.UsageP95Pct))
	if al.CPU.HasLive {
		w("Nodes com memória em uso ≥ 90%%: **%d** (≥ 95%%: **%d**). Nodes com CPU ≥ 90%%: %d.\n\n", al.NodesMemAbove90, al.NodesMemAbove95, al.NodesCPUAbove90)
	}

	// 3. Nodes
	if len(a.Nodes) > 0 {
		w("## 3. Nodes (mais carregados em memória)\n\n")
		w("| Node | Zona | Pods | CPU req | CPU uso | Mem req | Mem uso | Mem limits |\n|---|---|---|---|---|---|---|---|\n")
		for _, n := range mdFirstNodes(a.Nodes, 15) {
			w("| `%s` | %s | %d | %.0f%% | %s | %.0f%% | %s | %.0f%% |\n", n.Name, mdDash(n.Zone), n.Pods, n.CPUReqPct,
				mdPctOrDash(n.HasUsage, n.CPUUsagePct), n.MemReqPct, mdPctOrDash(n.HasUsage, n.MemUsagePct), n.MemLimPct)
		}
		if len(a.Nodes) > 15 {
			w("\n_+ %d nodes._\n", len(a.Nodes)-15)
		}
		w("\n")
	}

	// 4. DaemonSets
	w("## 4. DaemonSets (custo fixo por node)\n\n")
	w("Por node, os DaemonSets reservam **%.0fm de CPU e %.0f Mi**", ds.PerNodeCPUReqMillis, ds.PerNodeMemReqMi)
	if ds.HasUsage {
		w(" e usam **%.0fm de CPU e %.0f Mi**", ds.PerNodeCPUUsageMillis, ds.PerNodeMemUsageMi)
	}
	w(": **%.0f%% da memória** e %.0f%% da CPU alocáveis de cada node", ds.MemOverheadPct, ds.CPUOverheadPct)
	if ds.MonthlyCostBRL > 0 {
		w(" (≈ R$ %s/mês no pool)", mdBrl(ds.MonthlyCostBRL))
	}
	w(".\n\n")
	if len(ds.Items) > 0 {
		w("| DaemonSet | CPU req | CPU uso méd/máx | Mem req | Mem uso méd/máx | Observação |\n|---|---|---|---|---|---|\n")
		for _, d := range ds.Items {
			w("| %s/%s | %s | %s | %s | %s | %s |\n", d.Namespace, d.Name, mdMillis(d.CPUReqMillis),
				mdAvgMaxCell(d.HasUsage, d.CPUUsageAvg, d.CPUUsageMax, "m"), mdMi(d.MemReqMi),
				mdAvgMaxCell(d.HasUsage, d.MemUsageAvgMi, d.MemUsageMaxMi, " Mi"), mdFlagsLabel(d.Flags))
		}
		w("\n")
	}

	// 5. Workloads
	w("## 5. Workloads do pool\n\n")
	if len(a.Workloads) == 0 {
		w("Nenhum workload (além de DaemonSets) no pool.\n\n")
	} else {
		w("Valores por pod. Uso = P95 histórico quando disponível, senão o uso instantâneo (máximo entre os pods).\n\n")
		w("| Workload | Pods | CPU req → rec | CPU uso | Mem req → rec | Mem uso | Mem limit → rec | %% CPU req do pool | Observações |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, x := range a.Workloads {
			w("| %s/%s | %d | %s → %s | %s | %s → %s | %s | %s → %s | %s%% | %s |\n",
				x.Namespace, x.Workload, x.PodsOnPool,
				mdMillis(x.CPUReqMillis), mdRecCell(x.CPURecMillis, "m"), mdWorkloadUsage(x, true),
				mdMi(x.MemReqMi), mdRecCell(x.MemRecMi, " Mi"), mdWorkloadUsage(x, false),
				mdMi(x.MemLimMi), mdRecCell(x.MemLimitRecMi, " Mi"), mdNum(x.CPURequestSharePct, 1), mdFlagsLabel(x.Flags))
		}
		w("\n")
	}

	// 6. HPAs
	var hpas []DeepWorkloadRow
	for _, x := range a.Workloads {
		if x.HPA != nil {
			hpas = append(hpas, x)
		}
	}
	if len(hpas) > 0 {
		w("## 6. HPAs\n\n")
		w("| Workload | Métricas (alvo) | Min / Max / Atual | Estado | Histórico | Projeção com o request recomendado |\n|---|---|---|---|---|---|\n")
		for _, x := range hpas {
			h := x.HPA
			hist := "—"
			if h.AvgReplicas > 0 || h.ScaleEvents > 0 {
				hist = fmt.Sprintf("méd %.0f, %d–%d, %d eventos", h.AvgReplicas, h.MinObserved, h.MaxObserved, h.ScaleEvents)
			}
			w("| %s | %s | %d / %d / %d | %s | %s | %s |\n", x.Workload, mdDash(hpaMetricsSummary(h)), h.Min, h.Max, h.Current,
				hpaStateLabel(h.State), hist, projectionsLabel(h.Projections))
		}
		w("\n")
	}

	// 7. Throttling
	var thr []DeepWorkloadRow
	for _, x := range a.Workloads {
		if x.HasThrottle && x.ThrottleP95Pct >= 1 {
			thr = append(thr, x)
		}
	}
	if a.ThrottleWindowDays > 0 || len(thr) > 0 {
		w("## 7. Throttling de CPU\n\n")
		if len(thr) == 0 {
			w("Nenhum workload com throttling relevante na janela de %d dias.\n\n", a.ThrottleWindowDays)
		} else {
			w("%% dos períodos do CFS em que o container foi congelado pelo CPU limit (P95 em %d dias). Acima de %.0f%%, impacta latência.\n\n", a.ThrottleWindowDays, deepThrottleWarnPct)
			w("| Workload | CPU limit | Throttling P95 | Atual | Limit sugerido |\n|---|---|---|---|---|\n")
			for _, x := range thr {
				w("| %s | %s | %.0f%% | %.0f%% | %s |\n", x.Workload, mdMillis(x.CPULimMillis), x.ThrottleP95Pct, x.ThrottleCurrentPct, mdRecCell(x.CPULimitRecMillis, "m"))
			}
			w("\n")
		}
	}

	// 8. Simulação
	if len(a.Simulation) > 0 {
		w("## 8. Simulação de VMs\n\n")
		w("Nodes necessários por SKU, com %.0f%% de ocupação-alvo, descontando o custo fixo de DaemonSets, mínimo de %d nodes (ou nº de zonas). "+
			"**Requests atuais** = só troca a VM; **recomendados** = depois do ajuste de requests.", a.Headroom*100, deepMinNodes)
		if a.SKUCatalogStatus != "" {
			w(" Catálogo de SKUs da região: %s.", mdCatalogLabel(a.SKUCatalogStatus))
		}
		w("\n\n")
		w("| VM | vCPU / GB | CPU | Alocável/node | Requests atuais | Requests recomendados | Economia/mês | Observações |\n|---|---|---|---|---|---|---|---|\n")
		for _, s := range a.Simulation {
			name := "`" + s.VMSize + "`"
			if s.IsCurrent {
				name += " **(atual)**"
			}
			cpu := mdDash(s.CPU)
			if !s.SMT {
				cpu += " (sem SMT)"
			}
			w("| %s | %d / %d | %s | %s / %s%s | %s | %s | %s | %s |\n", name, s.VCPU, s.MemGB, cpu,
				mdCores(s.AllocCPUMillis), mdGib(s.AllocMemMi), mdIfStr(s.AllocEstimated, " (est.)"),
				mdSimCell(s.Feasible, s.NodesCurrentReq, s.CostCurrentReqBRL, s.LimitingCurrentReq, s.PriceUSDHour),
				mdSimCell(s.Feasible, s.NodesRecommended, s.CostRecommendedBRL, s.LimitingRecommended, s.PriceUSDHour),
				mdSavingsCell(s), mdDash(strings.Join(s.Notes, " ")))
		}
		w("\n")
	}

	if len(a.Warnings) > 0 {
		w("## Avisos da coleta\n\n")
		for _, x := range a.Warnings {
			w("- %s\n", x)
		}
		w("\n")
	}

	w("## Metodologia\n\n")
	w("- **Request recomendado:** CPU = P95 × 1,2 (mínimo 50m); memória = max(P95, uso atual) × 1,2; limit de memória = max(P95, pico) × 1,3. Em workloads com HPA por utilização, o request é elevado para a utilização projetada não passar do alvo.\n")
	w("- **Alocável de SKUs candidatas:** fórmula de reserva da AKS (Kubernetes ≥ 1.29): CPU 60m/100m/140m + 10m por core acima de 4; memória min(20 MB × maxPods + 50 MB, 25%%) + 100 Mi de eviction. A SKU atual usa o alocável medido nos nodes.\n")
	w("- **Uso P95 (Σ workloads):** soma dos P95 por pod dos workloads com histórico no relatório, mais o uso ao vivo dos DaemonSets (o histórico de um DaemonSet é o pior node do cluster inteiro, não deste pool). Não inclui o consumo do sistema do node (o uso ao vivo inclui). A soma é conservadora, porque os picos de pods diferentes não acontecem ao mesmo tempo.\n")
	w("- **Preços:** tabela sob demanda (Azure Retail Prices). Reservas, Savings Plan e Spot não estão refletidos.\n")
	return b.String()
}

func mdIfStr(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func mdDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// brl formata um valor em reais no padrão brasileiro (1.234,56).
func mdBrl(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := fmt.Sprintf("%.2f", v)
	intPart, dec := s[:len(s)-3], s[len(s)-2:]
	var groups []string
	for len(intPart) > 3 {
		groups = append([]string{intPart[len(intPart)-3:]}, groups...)
		intPart = intPart[:len(intPart)-3]
	}
	groups = append([]string{intPart}, groups...)
	out := strings.Join(groups, ".") + "," + dec
	if neg {
		return "-" + out
	}
	return out
}

func mdCores(m float64) string { return mdNum(m/1000, 1) + " cores" }
func mdGib(mi float64) string  { return mdNum(mi/1024, 1) + " GiB" }

// mdNum formata com vírgula decimal (pt-BR).
func mdNum(v float64, decimals int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', decimals, 64), ".", ",", 1)
}
func mdMillis(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0fm", v)
}
func mdMi(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f Mi", v)
}

func mdUsageCell(has bool, val string, p float64) string {
	if !has {
		return "—"
	}
	return fmt.Sprintf("%s (%.0f%%)", val, p)
}

func mdPctOrDash(has bool, p float64) string {
	if !has {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", p)
}

func mdAvgMaxCell(has bool, avg, max float64, unit string) string {
	if !has {
		return "—"
	}
	return fmt.Sprintf("%.0f / %.0f%s", avg, max, unit)
}

func mdRecCell(v float64, unit string) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("**%.0f%s**", v, unit)
}

func mdWorkloadUsage(x DeepWorkloadRow, cpu bool) string {
	switch x.UsageBasis {
	case "p95":
		if cpu {
			return fmt.Sprintf("P95 %.0fm", x.CPUP95Millis)
		}
		return fmt.Sprintf("P95 %.0f Mi (%.0f%% do req)", x.MemP95Mi, x.MemUsageVsRequestPct)
	case "live":
		if cpu {
			return fmt.Sprintf("%.0f / %.0fm", x.CPUUsageAvgMillis, x.CPUUsageMaxMillis)
		}
		return fmt.Sprintf("%.0f / %.0f Mi (%.0f%% do req)", x.MemUsageAvgMi, x.MemUsageMaxMi, x.MemUsageVsRequestPct)
	}
	return "—"
}

func mdFirstNodes(n []DeepNodeRow, k int) []DeepNodeRow {
	if len(n) <= k {
		return n
	}
	return n[:k]
}

var deepFlagLabels = map[string]string{
	"no_requests":          "sem requests",
	"no_mem_request":       "sem request de memória",
	"no_mem_limit":         "sem limit de memória",
	"mem_under_requested":  "**uso de memória acima do request**",
	"cpu_under_requested":  "uso de CPU acima do request",
	"cpu_over_requested":   "CPU request ≥ 4× o uso",
	"high_mem_limit_ratio": "limit de memória > 2× o request",
	"hpa_pinned_max":       "HPA no máximo",
	"hpa_pinned_min":       "HPA no mínimo",
	"hpa_memory_metric":    "HPA por memória",
	"rec_adjusted_for_hpa": "rec. ajustado ao HPA",
	"cpu_throttled":        "**throttling de CPU**",
}

func mdFlagsLabel(flags []string) string {
	var out []string
	for _, f := range flags {
		if l, ok := deepFlagLabels[f]; ok {
			out = append(out, l)
		} else {
			out = append(out, f)
		}
	}
	return mdDash(strings.Join(out, "; "))
}

func mdSeverityIcon(s string) string {
	switch s {
	case "critical":
		return "🔴"
	case "warning":
		return "🟡"
	}
	return "🔵"
}

func deepPriorityLabel(p string) string {
	if p == "spot" {
		return "Spot"
	}
	return "Regular"
}

func hpaStateLabel(s string) string {
	switch s {
	case "fixed":
		return "fixo (min = max)"
	case "pinned_min":
		return "preso no mínimo"
	case "pinned_max":
		return "**preso no máximo**"
	}
	return "escalando"
}

func projectionsLabel(ps []DeepHPAProjection) string {
	var out []string
	for _, p := range ps {
		res := p.Resource
		if res == "memory" {
			res = "memória"
		}
		s := fmt.Sprintf("%s: %.0f%% → %.0f%% (alvo %d%%)", res, p.CurrentPct, p.ProjectedPct, p.Target)
		if p.AdjustedForHPA {
			s += fmt.Sprintf(", ajustado (o puro daria %.0f%%)", p.PlainProjectedPct)
		}
		out = append(out, s)
	}
	return mdDash(strings.Join(out, "; "))
}

func deepSources(a PoolDeepAnalysis) string {
	var s []string
	if a.MetricsLive {
		s = append(s, "metrics-server (ao vivo)")
	}
	if a.History.Available {
		h := "relatório FinOps em cache"
		if a.History.GeneratedAt != nil {
			h += " de " + a.History.GeneratedAt.Format("02/01/2006 15:04")
		}
		if a.History.WindowDays > 0 {
			h += fmt.Sprintf(" (P95 de %d dias)", a.History.WindowDays)
		}
		h += fmt.Sprintf(", %d/%d workloads com histórico", a.History.Covered, a.History.Total)
		s = append(s, h)
	}
	if a.ThrottleWindowDays > 0 {
		s = append(s, fmt.Sprintf("Prometheus (throttling, %d dias)", a.ThrottleWindowDays))
	}
	return mdDash(strings.Join(s, "; "))
}

var deepLimitingLabels = map[string]string{"cpu": "CPU", "memory": "memória", "pods": "pods", "min_nodes": "mínimo"}

func mdSimCell(feasible bool, nodes int, cost float64, limiting string, price float64) string {
	if !feasible || nodes == 0 {
		return "não comporta"
	}
	s := fmt.Sprintf("%d nodes", nodes)
	if price > 0 {
		s += fmt.Sprintf(", R$ %s", mdBrl(cost))
	}
	if l := deepLimitingLabels[limiting]; l != "" {
		s += " (" + l + ")"
	}
	return s
}

func mdSavingsCell(s DeepSimulation) string {
	if !s.Feasible || s.PriceUSDHour <= 0 {
		return "—"
	}
	if s.SavingsRecommendedBRL >= 0 {
		return "R$ " + mdBrl(s.SavingsRecommendedBRL)
	}
	return "+R$ " + mdBrl(-s.SavingsRecommendedBRL) + " (custo maior)"
}

func mdCatalogLabel(st string) string {
	switch st {
	case SKUCatalogFresh:
		return "verificado"
	case SKUCatalogStale:
		return "verificado (cache vencido, atualizando)"
	case SKUCatalogLoading:
		return "carregando — disponibilidade não verificada"
	}
	return "indisponível — disponibilidade não verificada"
}
