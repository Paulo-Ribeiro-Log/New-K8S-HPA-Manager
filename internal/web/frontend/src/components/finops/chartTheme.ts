// Paleta e estilo compartilhados pelos gráficos do FinOps (fora de charts.tsx para esse arquivo
// exportar só componentes).
// Paleta da decomposição de custo (mesma no Relatório, no Node Pools e no PDF).
export const COST_COLORS = {
  productive: "#10b981", // em uso
  waste: "#f59e0b",      // request acima do necessário
  idle: "#f43f5e",       // capacidade ociosa
  osDisk: "#38bdf8",
  pvc: "#818cf8",
  orphan: "#e879f9",
} as const;

export const AXIS_TICK = { fontSize: 10, fill: "hsl(var(--muted-foreground))" };
