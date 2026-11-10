// Cor e texto do selo "PR #n" conforme o status do PR no GitHub. Lógica pura (testada).

export interface PRStatusFields {
  draft: boolean;
  checks?: string; // success | failure | pending | none
  review?: string; // approved | changes_requested | review_required | none
  mergeable?: string; // ok | conflict | unknown
}

export type PRTone = "red" | "orange" | "gray" | "amber" | "green" | "blue";

export interface PRBadge {
  tone: PRTone;
  label: string; // resumo curto do que pesa mais
  details: string[]; // uma linha por item, para o tooltip
}

const TONE_CLASS: Record<PRTone, string> = {
  red: "border-red-500/40 bg-red-500/10 text-red-300 hover:bg-red-500/20",
  orange: "border-orange-500/40 bg-orange-500/10 text-orange-300 hover:bg-orange-500/20",
  gray: "border-slate-500/40 bg-slate-500/10 text-slate-300 hover:bg-slate-500/20",
  amber: "border-amber-500/40 bg-amber-500/10 text-amber-300 hover:bg-amber-500/20",
  green: "border-emerald-500/30 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20",
  blue: "border-sky-500/30 bg-sky-500/10 text-sky-300 hover:bg-sky-500/20",
};

export function prToneClass(t: PRTone): string {
  return TONE_CLASS[t];
}

/** Prioridade: conflito/checks falhando > alterações pedidas > rascunho > checks rodando > aprovado/ok > só aberto. */
export function prBadge(p: PRStatusFields): PRBadge {
  const details: string[] = [];
  if (p.draft) details.push("Rascunho");
  if (p.mergeable === "conflict") details.push("Conflito com a branch de destino");
  if (p.checks === "failure") details.push("Checks falhando");
  else if (p.checks === "pending") details.push("Checks em andamento");
  else if (p.checks === "success") details.push("Checks passaram");
  if (p.review === "approved") details.push("Aprovado");
  else if (p.review === "changes_requested") details.push("Alterações solicitadas");
  else if (p.review === "review_required") details.push("Aguardando revisão");

  let tone: PRTone;
  let label: string;
  if (p.mergeable === "conflict") [tone, label] = ["red", "conflito"];
  else if (p.checks === "failure") [tone, label] = ["red", "checks falhando"];
  else if (p.review === "changes_requested") [tone, label] = ["orange", "alterações pedidas"];
  else if (p.draft) [tone, label] = ["gray", "rascunho"];
  else if (p.checks === "pending") [tone, label] = ["amber", "checks rodando"];
  else if (p.review === "approved") [tone, label] = ["green", "aprovado"];
  else if (p.review === "review_required") [tone, label] = ["blue", "aguarda revisão"];
  else if (p.checks === "success") [tone, label] = ["green", "checks ok"];
  else [tone, label] = ["blue", ""];
  return { tone, label, details };
}
