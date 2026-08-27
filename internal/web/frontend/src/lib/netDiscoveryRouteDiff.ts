import type { NetDiscoveryHop } from "./api/types";

// ─── "Descoberta de Rede" — Fase B do roadmap de maturidade profissional
// (IP-ROUTE-DISCOVERY-PLAN.md): diff de rota entre execuções. 100% frontend, sem mudança de
// backend — o Histórico de Descobertas (Fase 5/P1) já traz os hops completos das últimas execuções
// conhecidas (NetDiscoveryHistoryEntry.result), então comparar duas listas de hops é só lógica pura
// sobre dado já em mãos. Detecta reroteamento/failover/mudança de infraestrutura entre uma busca e
// a anterior — hoje o histórico só LISTAVA execuções passadas, nunca comparava uma com a outra.

export interface RouteDiffChange {
  index: number;
  from?: string; // undefined = hop não existia nesta posição na rota anterior (* * * ou rota mais curta)
  to?: string; // undefined = hop não existe nesta posição na rota atual
}

export interface RouteDiffResult {
  changed: boolean;
  changes: RouteDiffChange[];
}

// hopIdentity — o que identifica um hop pra fins de diff: o IP resolvido, ou undefined quando o
// salto não respondeu ("* * *"). Loss%/RTT/jitter (Fase A) são DELIBERADAMENTE ignorados aqui —
// variação de latência/perda entre duas execuções não é "mudança de rota", é variação de
// desempenho da mesma rota; só a IDENTIDADE do hop (qual IP responde naquele índice) importa pro
// diff.
function hopIdentity(hop: NetDiscoveryHop | undefined): string | undefined {
  if (!hop || hop.timed_out || !hop.ip) return undefined;
  return hop.ip;
}

// diffRoutes compara duas listas de hops (mesmo alvo, execuções diferentes) por ÍNDICE de salto —
// não por conteúdo textual da rota inteira — pra apontar EXATAMENTE qual salto mudou, não só "a
// rota é diferente". `current` é a execução mais recente, `previous` a mais antiga (a ordem importa
// só pro sentido de from/to nas mensagens, a detecção de mudança é simétrica).
export function diffRoutes(current: NetDiscoveryHop[], previous: NetDiscoveryHop[]): RouteDiffResult {
  const maxLen = Math.max(current.length, previous.length);
  const changes: RouteDiffChange[] = [];

  for (let i = 0; i < maxLen; i++) {
    // hops nem sempre vêm ordenados/contíguos por `index` de forma confiável entre execuções
    // diferentes (número de saltos pode variar) — usa a POSIÇÃO na lista (0-based), convertendo
    // pro índice 1-based de exibição (mesmo padrão já usado no resto desta aba).
    const curHop = current[i];
    const prevHop = previous[i];
    const curIP = hopIdentity(curHop);
    const prevIP = hopIdentity(prevHop);

    if (curIP !== prevIP) {
      changes.push({
        index: curHop?.index ?? prevHop?.index ?? i + 1,
        from: prevIP,
        to: curIP,
      });
    }
  }

  return { changed: changes.length > 0, changes };
}

// formatRouteDiffSummary — texto curto pro banner/tag da UI, listando até `maxItems` mudanças e
// resumindo o resto ("+N outras") em vez de estourar a tela com uma rota inteira reescrita.
export function formatRouteDiffSummary(diff: RouteDiffResult, maxItems = 3): string {
  if (!diff.changed) return "";
  const shown = diff.changes.slice(0, maxItems).map((c) => {
    const from = c.from ?? "* * *";
    const to = c.to ?? "* * *";
    return `salto ${c.index} era ${from}, agora é ${to}`;
  });
  const extra = diff.changes.length - shown.length;
  return extra > 0 ? `${shown.join("; ")}; +${extra} outra${extra > 1 ? "s" : ""}` : shown.join("; ");
}
