// Limites de alerta do Tempo real, configuráveis por usuário. Lógica pura (testada em
// metricsThresholds.test.ts); nenhuma chamada de rede nem DOM. Os valores padrão são os que eram fixos no
// código antes de existir esta configuração, então quem não mexe em nada vê o mesmo de sempre.

export interface Thresholds {
  /** Latência do maior percentil marcado (ms): acima de latWarnMs é ALERTA, acima de latRedMs é ERRO. */
  latWarnMs: number;
  latRedMs: number;
  /** Erros 5xx por segundo agora: acima disto é ERRO (0 = qualquer 5xx). */
  err5Rate: number;
  /** Erros 4xx por segundo agora: acima disto é ALERTA (0 = qualquer 4xx). */
  err4Rate: number;
  /** CPU e memória, % do limit. */
  usageWarn: number;
  usageRed: number;
  /** Throttling de CPU, % dos períodos limitados. */
  throttleWarn: number;
  throttleRed: number;
  /** Pacotes descartados + erros de rede por segundo: acima disto é ALERTA. */
  netWarn: number;
  /** Restarts em 15 min: acima disto é ALERTA (OOMKilled continua sendo ERRO). */
  restartsWarn: number;
  /** ALERTA quando as réplicas desejadas chegam ao teto do HPA. */
  hpaCeiling: boolean;
  /** ALERTA com pods pendentes ou não prontos. */
  pods: boolean;
}

export const DEFAULT_THRESHOLDS: Thresholds = {
  latWarnMs: 500, latRedMs: 1000,
  err5Rate: 0, err4Rate: 0,
  usageWarn: 70, usageRed: 85,
  throttleWarn: 10, throttleRed: 25,
  netWarn: 0.5,
  restartsWarn: 0,
  hpaCeiling: true,
  pods: true,
};

export const THRESHOLDS_KEY = "metrics_live_thresholds";

const NUM_KEYS = ["latWarnMs", "latRedMs", "err5Rate", "err4Rate", "usageWarn", "usageRed", "throttleWarn", "throttleRed", "netWarn", "restartsWarn"] as const;
const BOOL_KEYS = ["hpaCeiling", "pods"] as const;
/** Pares (alerta, erro): alerta não pode passar de erro. */
const PAIRS: [keyof Thresholds, keyof Thresholds][] = [["latWarnMs", "latRedMs"], ["usageWarn", "usageRed"], ["throttleWarn", "throttleRed"]];
/** Teto de cada campo, para barrar digitação absurda (um "0 a 100%" que vira 1000000). */
const MAX: Partial<Record<keyof Thresholds, number>> = { latWarnMs: 3_600_000, latRedMs: 3_600_000, usageWarn: 1000, usageRed: 1000, throttleWarn: 100, throttleRed: 100, err5Rate: 1_000_000, err4Rate: 1_000_000, netWarn: 1_000_000, restartsWarn: 1_000_000 };

/**
 * Aceita o que veio do servidor ou do navegador (objeto, texto JSON ou lista de um objeto) e devolve limites
 * sempre completos e coerentes: campo ausente ou inválido volta ao padrão, e um par com alerta acima do erro
 * volta ao padrão inteiro.
 */
export function normalizeThresholds(raw: unknown): Thresholds {
  let v: unknown = raw;
  if (typeof v === "string") {
    try {
      v = JSON.parse(v);
    } catch {
      return { ...DEFAULT_THRESHOLDS };
    }
  }
  if (Array.isArray(v)) v = v[0];
  const o = (v && typeof v === "object" ? v : {}) as Record<string, unknown>;
  const out: Thresholds = { ...DEFAULT_THRESHOLDS };
  for (const k of NUM_KEYS) {
    const x = o[k];
    if (typeof x === "number" && Number.isFinite(x) && x >= 0 && x <= (MAX[k] ?? Infinity)) out[k] = x;
  }
  for (const k of BOOL_KEYS) if (typeof o[k] === "boolean") out[k] = o[k] as boolean;
  for (const [w, r] of PAIRS) {
    if ((out[w] as number) > (out[r] as number)) {
      (out[w] as number) = DEFAULT_THRESHOLDS[w] as number;
      (out[r] as number) = DEFAULT_THRESHOLDS[r] as number;
    }
  }
  return out;
}

const LABEL: Record<(typeof NUM_KEYS)[number], string> = {
  latWarnMs: "Latência (alerta)", latRedMs: "Latência (erro)", err5Rate: "Erros 5xx", err4Rate: "Erros 4xx",
  usageWarn: "CPU e memória (alerta)", usageRed: "CPU e memória (erro)", throttleWarn: "Throttling (alerta)", throttleRed: "Throttling (erro)",
  netWarn: "Rede", restartsWarn: "Restarts",
};

/** Mensagens para o formulário; vazio = tudo certo. */
export function validateThresholds(t: Thresholds): string[] {
  const errs: string[] = [];
  for (const k of NUM_KEYS) {
    const x = t[k];
    if (typeof x !== "number" || !Number.isFinite(x) || x < 0) errs.push(`${LABEL[k]}: informe um número maior ou igual a zero.`);
    else if (x > (MAX[k] ?? Infinity)) errs.push(`${LABEL[k]}: o máximo é ${(MAX[k] as number).toLocaleString("pt-BR")}.`);
  }
  if (errs.length) return errs;
  if (t.latWarnMs > t.latRedMs) errs.push("Latência: o limite de alerta não pode ser maior que o de erro.");
  if (t.usageWarn > t.usageRed) errs.push("CPU e memória: o limite de alerta não pode ser maior que o de erro.");
  if (t.throttleWarn > t.throttleRed) errs.push("Throttling: o limite de alerta não pode ser maior que o de erro.");
  return errs;
}

export function isDefaultThresholds(t: Thresholds): boolean {
  return [...NUM_KEYS, ...BOOL_KEYS].every((k) => t[k] === DEFAULT_THRESHOLDS[k]);
}

/** Resumo curto dos que diferem do padrão, para a tela de Opções ("Latência (alerta): 300"). Vazio = padrão. */
export function changedThresholds(t: Thresholds): string[] {
  const out: string[] = [];
  for (const k of NUM_KEYS) if (t[k] !== DEFAULT_THRESHOLDS[k]) out.push(`${LABEL[k]}: ${t[k].toLocaleString("pt-BR")}`);
  if (t.hpaCeiling !== DEFAULT_THRESHOLDS.hpaCeiling) out.push(`Teto do HPA: ${t.hpaCeiling ? "alerta" : "ignorado"}`);
  if (t.pods !== DEFAULT_THRESHOLDS.pods) out.push(`Pods pendentes: ${t.pods ? "alerta" : "ignorados"}`);
  return out;
}
