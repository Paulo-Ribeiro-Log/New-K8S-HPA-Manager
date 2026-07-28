// Helpers de gráfico compartilhados entre componentes com comparação D-1/D-2/D-3 — extraído de
// ConntrackTab.tsx (padrão já validado em produção) pra ser reaproveitado por
// DeploymentBehaviorChart.tsx sem duplicar a lógica.

// Dias disponíveis para comparação (mesmo horário, N dias atrás).
export const COMPARE_DAYS = [1, 2, 3] as const;
export const COMPARE_COLORS: Record<number, string> = { 1: "#f97316", 2: "#a855f7", 3: "#64748b" };
export const COMPARE_LABELS: Record<number, string> = { 1: "D-1", 2: "D-2", 3: "D-3" };

export function compareColorForOffset(offset: number): string {
  return COMPARE_COLORS[offset] ?? "#94a3b8";
}

export function compareLabelForOffset(offset: number): string {
  return COMPARE_LABELS[offset] ?? `D-${offset}`;
}

// Resolve cor/label de uma série de comparação a partir do dataKey (convenção "{prefix}D{offset}",
// ex: "pctD1"/"cpuD2") — usado em formatters custom de ChartTooltip pra não depender de
// item.payload.fill: no ChartTooltipContent do shadcn, todas as séries de um mesmo ponto
// compartilham o mesmo objeto `payload` (a linha do chartData), então um `fill` fixo (ex: da barra
// "hoje") vazaria pras linhas de comparação também.
export function compareColorForDataKey(dataKey: string): string {
  const offset = Number(dataKey.match(/D(\d+)$/)?.[1]);
  return compareColorForOffset(offset);
}

export function compareLabelForDataKey(dataKey: string): string {
  const offset = Number(dataKey.match(/D(\d+)$/)?.[1]);
  return compareLabelForOffset(offset);
}

// Reduz uma série de pontos a no máximo `maxSamples` amostras (48 por padrão) — mesma lógica pra
// todas as séries comparadas num gráfico, garante que o índice i de séries diferentes corresponda
// ao mesmo horário relativo (não decima por timestamp absoluto).
export function decimate<T>(points: T[], maxSamples = 48): T[] {
  if (!points.length) return [];
  const step = Math.ceil(points.length / maxSamples);
  return points.filter((_, i) => i % step === 0);
}
