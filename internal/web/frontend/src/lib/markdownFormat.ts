// Ferramentas de formatação do Code Editor para arquivos .md: lógica pura sobre o texto e a seleção (offsets).
// Devolve a edição mínima (trocar [from,to) por insert) e a nova seleção, para o Monaco aplicar com executeEdits
// (o Ctrl+Z do editor desfaz).

export type FormatAction =
  | "bold" | "italic" | "strike" | "code"
  | "h1" | "h2" | "h3" | "ul" | "ol" | "task" | "quote"
  | "link" | "break" | "codeblock" | "table" | "hr" | "br" | "check"
  | `color:${string}`;

export interface FormatEdit {
  from: number;
  to: number;
  insert: string;
  /** Nova seleção, em offsets do texto DEPOIS da edição. */
  selStart: number;
  selEnd: number;
}

const WRAP: Record<string, [string, string]> = { bold: ["**", "**"], italic: ["*", "*"], strike: ["~~", "~~"], code: ["`", "`"] };
const PREFIX: Record<string, string> = { h1: "# ", h2: "## ", h3: "### ", ul: "- ", task: "- [ ] ", quote: "> " };
const COLOR_RE = /^(#[0-9a-f]{3,8}|[a-z]{3,20})$/i;

function wrapSelection(text: string, s: number, e: number, open: string, close: string, placeholder: string): FormatEdit {
  // já embrulhado por fora da seleção → desembrulha
  if (s >= open.length && text.slice(s - open.length, s) === open && text.slice(e, e + close.length) === close) {
    return { from: s - open.length, to: e + close.length, insert: text.slice(s, e), selStart: s - open.length, selEnd: e - open.length };
  }
  const sel = text.slice(s, e) || placeholder;
  const ins = open + sel + close;
  return { from: s, to: e, insert: ins, selStart: s + open.length, selEnd: s + open.length + sel.length };
}

/** Início da linha de `pos` e fim da linha de `pos` (sem o \n). */
function lineBounds(text: string, from: number, to: number): [number, number] {
  const a = text.lastIndexOf("\n", from - 1) + 1;
  const nl = text.indexOf("\n", to > from ? to - 1 : to);
  return [a, nl === -1 ? text.length : nl];
}

function prefixLines(text: string, s: number, e: number, action: string): FormatEdit {
  const [a, b] = lineBounds(text, s, e);
  const lines = text.slice(a, b).split("\n");
  const HEAD = /^#{1,6} /;
  const LIST = /^(\s*)(?:[-*+] \[[ xX]\] |[-*+] |\d+[.)] )/;
  let out: string[];
  if (action === "ol") {
    const all = lines.every((l) => /^\d+[.)] /.test(l));
    out = lines.map((l, i) => (all ? l.replace(/^\d+[.)] /, "") : `${i + 1}. ${l.replace(LIST, "$1").replace(HEAD, "")}`));
  } else {
    const p = PREFIX[action];
    const all = lines.every((l) => l.startsWith(p));
    const isHead = action.startsWith("h");
    out = lines.map((l) => {
      if (all) return l.slice(p.length);
      const base = isHead ? l.replace(HEAD, "") : action === "quote" ? l : l.replace(LIST, "$1").replace(HEAD, "");
      return p + base;
    });
  }
  const insert = out.join("\n");
  return { from: a, to: b, insert, selStart: a, selEnd: a + insert.length };
}

export function applyFormat(text: string, s: number, e: number, action: FormatAction): FormatEdit {
  if (action in WRAP) return wrapSelection(text, s, e, WRAP[action][0], WRAP[action][1], "texto");
  if (action in PREFIX || action === "ol") return prefixLines(text, s, e, action);
  if (action.startsWith("color:")) {
    const c = action.slice(6);
    if (!COLOR_RE.test(c)) return { from: s, to: s, insert: "", selStart: s, selEnd: e };
    return wrapSelection(text, s, e, `<span style="color:${c}">`, "</span>", "texto");
  }
  if (action === "link") {
    const sel = text.slice(s, e) || "texto";
    const url = "https://";
    const ins = `[${sel}](${url})`;
    const us = s + sel.length + 3;
    return { from: s, to: e, insert: ins, selStart: us, selEnd: us + url.length };
  }
  if (action === "codeblock") {
    const sel = text.slice(s, e);
    const ins = "```\n" + sel + "\n```";
    return { from: s, to: e, insert: ins, selStart: s + 4, selEnd: s + 4 + sel.length };
  }
  if (action === "table") {
    const t = "| Coluna 1 | Coluna 2 | Coluna 3 |\n| --- | --- | --- |\n|  |  |  |\n|  |  |  |\n";
    const [, b] = lineBounds(text, e, e);
    const pre = b < text.length || text.length > 0 ? "\n\n" : "";
    return { from: b, to: b, insert: pre + t, selStart: b + pre.length + 2, selEnd: b + pre.length + 10 };
  }
  if (action === "hr") {
    const [, b] = lineBounds(text, e, e);
    return { from: b, to: b, insert: "\n\n---\n", selStart: b + 6, selEnd: b + 6 };
  }
  if (action === "break") return { from: s, to: e, insert: "  \n", selStart: s + 3, selEnd: s + 3 };
  if (action === "br") return { from: s, to: e, insert: "<br>", selStart: s + 4, selEnd: s + 4 };
  // check: ☐ ↔ ☑ sobre a seleção, ou insere ☐
  const ch = text.slice(s, e);
  const ins = ch === "☐" ? "☑" : ch === "☑" ? "☐" : "☐ ";
  return { from: s, to: e, insert: ins, selStart: s + ins.length, selEnd: s + ins.length };
}

/** Aplica a edição ao texto (usado nos testes; o Monaco aplica por executeEdits). */
export function applyEdit(text: string, ed: FormatEdit): string {
  return text.slice(0, ed.from) + ed.insert + text.slice(ed.to);
}
