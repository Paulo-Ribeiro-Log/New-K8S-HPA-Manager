// Funções puras da ajuda (sem React): normalização, busca no índice e âncoras dos títulos.
import { HELP_GROUP_ORDER, type HelpEntry, type HelpGroup } from "./helpRegistry";

/** Minúsculas e sem acentos, para a busca ignorar maiúsculas e "ç/ã". */
export function normalize(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "");
}

/** Texto do título sem a marcação do Markdown (`código`, **negrito**, [link](url)). */
export function plainHeadingText(text: string): string {
  return text
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*_~]/g, "")
    .trim();
}

/** Âncora estável de um título: "Como usar (passo a passo)" -> "como-usar-passo-a-passo". */
export function slugify(text: string): string {
  return normalize(plainHeadingText(text))
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export interface HelpHeading {
  level: 2 | 3;
  text: string;
  id: string;
}

/** Títulos ## e ### do Markdown (fora de blocos de código), para o "Nesta página". */
export function extractHeadings(markdown: string): HelpHeading[] {
  const out: HelpHeading[] = [];
  let inFence = false;
  for (const line of markdown.split("\n")) {
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = /^(#{2,3})\s+(.+?)\s*#*\s*$/.exec(line);
    if (!m) continue;
    const text = plainHeadingText(m[2]);
    out.push({ level: m[1].length as 2 | 3, text, id: slugify(text) });
  }
  return out;
}

/** Entradas que contêm todos os termos da busca (título, resumo, grupo ou palavras-chave). */
export function filterEntries(entries: HelpEntry[], query: string): HelpEntry[] {
  const terms = normalize(query).split(/\s+/).filter(Boolean);
  if (terms.length === 0) return entries;
  return entries.filter((e) => {
    const haystack = normalize([e.title, e.summary, e.group, ...(e.keywords ?? [])].join(" "));
    return terms.every((t) => haystack.includes(t));
  });
}

/** Agrupa na ordem de HELP_GROUP_ORDER, com os itens de cada grupo em ordem alfabética. */
export function groupEntries(entries: HelpEntry[]): { group: HelpGroup; items: HelpEntry[] }[] {
  return HELP_GROUP_ORDER.map((group) => ({
    group,
    items: entries.filter((e) => e.group === group).sort((a, b) => a.title.localeCompare(b.title, "pt-BR")),
  })).filter((g) => g.items.length > 0);
}

/** Link interno de uma página de ajuda para outra: `ajuda:<id>` ou `ajuda:<id>#<seção>`. */
export function parseHelpLink(href: string | undefined): { id: string; anchor: string | null } | null {
  if (!href || !href.startsWith("ajuda:")) return null;
  const [id, anchor] = href.slice("ajuda:".length).split("#");
  return id ? { id, anchor: anchor || null } : null;
}
