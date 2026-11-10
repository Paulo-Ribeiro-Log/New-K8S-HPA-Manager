// Selo "PR #n" no padrão do GitHub: a COR é o estado (aberto verde, rascunho cinza, mergeado roxo,
// fechado vermelho); checks, revisão e conflito viram indicadores à parte. Lógica pura (testada).

export interface PRStatusFields {
  state?: string; // open | closed | merged (ausente = open)
  draft: boolean;
  checks?: string; // success | failure | pending | none
  review?: string; // approved | changes_requested | review_required | none
  mergeable?: string; // ok | conflict | unknown
}

export type PRTone = "green" | "gray" | "purple" | "red";

export interface PRIndicators {
  checks?: "success" | "failure" | "pending";
  review?: "approved" | "changes_requested";
  conflict: boolean;
}

export interface PRBadge {
  tone: PRTone;
  /** Fechado e mergeado já não aceitam mudança: o selo fica esmaecido. */
  dimmed: boolean;
  indicators: PRIndicators;
  details: string[]; // uma linha por item, para o card
}

const TONE_CLASS: Record<PRTone, string> = {
  green: "border-emerald-500/40 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20",
  gray: "border-slate-500/40 bg-slate-500/10 text-slate-300 hover:bg-slate-500/20",
  purple: "border-purple-500/40 bg-purple-500/10 text-purple-300 hover:bg-purple-500/20",
  red: "border-red-500/40 bg-red-500/10 text-red-300 hover:bg-red-500/20",
};

export function prToneClass(b: Pick<PRBadge, "tone" | "dimmed">): string {
  return TONE_CLASS[b.tone] + (b.dimmed ? " opacity-60 hover:opacity-100" : "");
}

const CHECKS_LABEL = { failure: "Checks falhando", pending: "Checks em andamento", success: "Checks passaram" } as const;

export function prBadge(p: PRStatusFields): PRBadge {
  const state = p.state || "open";
  if (state === "merged") return { tone: "purple", dimmed: true, indicators: { conflict: false }, details: ["Mergeado"] };
  if (state === "closed") return { tone: "red", dimmed: true, indicators: { conflict: false }, details: ["Fechado sem merge"] };

  const indicators: PRIndicators = { conflict: p.mergeable === "conflict" };
  const details: string[] = [p.draft ? "Rascunho" : "Aberto"];
  if (indicators.conflict) details.push("Conflito com a branch de destino");
  if (p.checks === "failure" || p.checks === "pending" || p.checks === "success") {
    indicators.checks = p.checks;
    details.push(CHECKS_LABEL[p.checks]);
  }
  if (p.review === "approved" || p.review === "changes_requested") {
    indicators.review = p.review;
    details.push(p.review === "approved" ? "Aprovado" : "Alterações solicitadas");
  } else if (p.review === "review_required") {
    details.push("Aguardando revisão");
  }
  return { tone: p.draft ? "gray" : "green", dimmed: false, indicators, details };
}
