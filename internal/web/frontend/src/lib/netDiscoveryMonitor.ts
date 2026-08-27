import type { NetDiscoveryResult } from "./api/types";
import { diffRoutes, type RouteDiffResult } from "./netDiscoveryRouteDiff";

// ─── "Descoberta de Rede" — Fase C do roadmap de maturidade profissional
// (IP-ROUTE-DISCOVERY-PLAN.md): agregação do modo de monitoramento contínuo (mesmo alvo, N rodadas
// sequenciais, reaproveitando o mecanismo de lote já existente — ver runMonitor em
// NetDiscoveryTab.tsx). 100% frontend, opera sobre os NetDiscoveryResult completos de cada rodada
// já recebidos via SSE (batchRoundResults) — sem chamada nova ao backend.

export interface MonitorHopAggregate {
  index: number;
  respondedRounds: number;
  totalRounds: number;
  avgLossPct: number;
}

export interface MonitorAggregateResult {
  hopAggregates: MonitorHopAggregate[];
  // routeDiff — 1ª rodada vs ÚLTIMA rodada (não rodada-a-rodada) — responde "a rota no início do
  // monitoramento é a mesma de agora", suficiente pra sinalizar reroteamento/failover durante a
  // janela observada sem precisar de N-1 diffs intermediários.
  routeDiff: RouteDiffResult;
}

// aggregateMonitorRounds agrega por POSIÇÃO de salto (0-based, mesmo critério de diffRoutes) — não
// por `index` reportado (que pode divergir entre rodadas se o número de saltos varia).
export function aggregateMonitorRounds(results: NetDiscoveryResult[]): MonitorAggregateResult {
  const maxHops = results.reduce((max, r) => Math.max(max, r.hops.length), 0);
  const hopAggregates: MonitorHopAggregate[] = [];

  for (let pos = 0; pos < maxHops; pos++) {
    let respondedRounds = 0;
    let lossSum = 0;
    let lossSamples = 0;
    let displayIndex = pos + 1;
    for (const r of results) {
      const hop = r.hops[pos];
      if (!hop) continue;
      displayIndex = hop.index;
      if (!hop.timed_out && hop.ip) {
        respondedRounds++;
      }
      if (hop.loss_pct != null) {
        lossSum += hop.loss_pct;
        lossSamples++;
      }
    }
    hopAggregates.push({
      index: displayIndex,
      respondedRounds,
      totalRounds: results.length,
      avgLossPct: lossSamples > 0 ? lossSum / lossSamples : 0,
    });
  }

  const routeDiff: RouteDiffResult =
    results.length >= 2
      ? diffRoutes(results[results.length - 1].hops, results[0].hops)
      : { changed: false, changes: [] };

  return { hopAggregates, routeDiff };
}
