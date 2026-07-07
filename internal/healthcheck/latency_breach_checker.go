package healthcheck

import (
	"context"
	"sort"
	"sync"
	"time"

	"k8s-hpa-manager/internal/monitoring/latencylookup"
)

// latencyBreachThresholdMs é o limite de P95 (ms) acima do qual um workload conta como "breach"
// de latência pra fins de escalada de severidade — constante fácil de ajustar depois de ver dado
// real (mesmo padrão de thresholds fixos já usado em outras partes do projeto, ex: cor de aresta
// do grafo de topologia do Teste de Latência).
//
// Deliberadamente DIFERENTE de actionrules.DefaultThresholds().LatencyP95CritMs (5000ms) — não é
// duplicação a corrigir (ver DYNATRACE-DIAGNOSTICS-PLAN.md Fase 1, item 1.5). Este threshold
// responde "isso CORROBORA um DT Problem que já está aberto?" (barra mais sensível, porque já há
// outro sinal independente apontando problema); o threshold do actionrules responde "isso SOZINHO
// já é motivo de alerta?" (barra mais alta, é o único sinal). Não trocar por
// actionrules.DefaultThresholds() sem reconfirmar com o usuário — já foi decidido explicitamente
// que 500ms é o valor certo pra esse caso de uso específico.
const latencyBreachThresholdMs = 500.0

// latencyBreachConcurrency limita quantos lookups de latência rodam em paralelo — mesmo espírito
// dos semáforos já usados em scans de frota (nodepool, access-check), evitando martelar
// Prometheus/Dynatrace com N requisições simultâneas quando há muitos workloads com DT Problem.
const latencyBreachConcurrency = 6

// latencyBreachTimeout é o teto pro enriquecimento inteiro (todos os workloads) — se estourar,
// os itens ainda sem resposta ficam sem LatencySource (best-effort, nunca bloqueia o Health Check).
const latencyBreachTimeout = 30 * time.Second

// latencyFetchFunc é o tipo de latencylookup.Fetch — parametrizado só pra permitir injetar um
// fake determinístico em teste (a lógica de filtro/escalada/reordenação é testável sem rede; o
// fetch real em si não é, por bater em Prometheus/Dynatrace de verdade).
type latencyFetchFunc func(ctx context.Context, dtURL, dtToken, cluster, namespace, workload string) latencylookup.Result

// EnrichWithLatencyBreach cruza breach de latência (P95 acima de latencyBreachThresholdMs, via
// Dynatrace/Prometheus — latencylookup.Fetch) com DT Problems já abertos no mesmo workload,
// escalando FinalSeverity pra Critical quando os dois batem (mesmo espírito da escalada K8s↔DT já
// feita em Correlate). Ver LATENCY-METRICS-PLAN.md Fase 7.
//
// Só verifica latência de workloads que JÁ têm len(DTProblems) > 0 — cruzar com "DT Problems
// abertos no mesmo workload" é literalmente o que a Fase 7 pede, e isso limita o custo de N
// lookups extras por scan ao número de workloads com problema DT ativo (tipicamente pequeno), não
// ao total de workloads do cluster. Roda só quando dtURL/dtToken não são vazios (o mesmo gate de
// req.CheckDynatrace que já habilita a correlação em si).
func EnrichWithLatencyBreach(ctx context.Context, items []CorrelatedHealthItem, cluster, dtURL, dtToken string) []CorrelatedHealthItem {
	return enrichWithLatencyBreach(ctx, items, cluster, dtURL, dtToken, latencylookup.Fetch)
}

func enrichWithLatencyBreach(ctx context.Context, items []CorrelatedHealthItem, cluster, dtURL, dtToken string, fetch latencyFetchFunc) []CorrelatedHealthItem {
	type target struct {
		idx int
	}
	var targets []target
	for i, item := range items {
		if len(item.DTProblems) > 0 {
			targets = append(targets, target{idx: i})
		}
	}
	if len(targets) == 0 {
		return items
	}

	ctx, cancel := context.WithTimeout(ctx, latencyBreachTimeout)
	defer cancel()

	sem := make(chan struct{}, latencyBreachConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			item := items[idx]
			res := fetch(ctx, dtURL, dtToken, cluster, item.Namespace, item.WorkloadName)
			if res.Source == "" {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			items[idx].LatencyP95Ms = res.P95Ms
			items[idx].LatencyP99Ms = res.P99Ms
			items[idx].LatencySource = res.Source
			items[idx].LatencyBreach = res.P95Ms > latencyBreachThresholdMs
			if items[idx].LatencyBreach && items[idx].FinalSeverity.Weight() < SeverityCritical.Weight() {
				items[idx].FinalSeverity = SeverityCritical
			}
		}(t.idx)
	}
	wg.Wait()

	// Re-ordenar: FinalSeverity pode ter mudado pra alguns itens (mesmo critério de Correlate).
	sort.Slice(items, func(i, j int) bool {
		if items[i].Correlated != items[j].Correlated {
			return items[i].Correlated
		}
		return items[i].FinalSeverity.Weight() > items[j].FinalSeverity.Weight()
	})

	return items
}
