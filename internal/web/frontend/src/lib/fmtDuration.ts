// Duração em milissegundos na unidade que cabe no valor: "30 µs", "4,2 ms", "268 ms", "1,3 s", "42 s".
// Evita "0.268s", que obriga a converter de cabeça. Lógica pura (testada em fmtDuration.test.ts).

const n = (v: number, maxDecimals: number) => v.toLocaleString("pt-BR", { maximumFractionDigits: maxDecimals });

/** Entrada em ms. Valor inválido ou negativo devolve "" (quem chama decide o texto de "sem dado"). */
export function fmtDurationMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms === 0) return "0 ms";
  if (ms < 1) {
    const us = Math.round(ms * 1000);
    return us >= 1000 ? "1 ms" : `${Math.max(1, us)} µs`; // 0,0004 ms ainda é 1 µs, não "0 µs"
  }
  if (ms < 10) return `${n(ms, 1)} ms`;
  if (ms < 1000) {
    const r = Math.round(ms);
    return r >= 1000 ? "1 s" : `${r} ms`; // 999,6 ms vira "1 s", não "1000 ms"
  }
  const s = ms / 1000;
  if (s < 10) return `${n(s, 2)} s`;
  if (s < 100) return `${n(s, 1)} s`;
  return `${n(s, 0)} s`;
}
