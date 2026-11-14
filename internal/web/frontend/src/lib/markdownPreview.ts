// Apoio ao preview Markdown do Code Editor: HTML mínimo e seguro (<br>, cor), limpeza dos escapes que o
// export do Confluence põe e alternância de checkbox direto no texto. Sem dependência (lógica pura, testada).

type MNode = { type: string; value?: string; children?: MNode[]; data?: Record<string, unknown>; position?: { start: { line: number; column: number } } };

const COLOR_RE = /^(#[0-9a-f]{3,8}|[a-z]{3,20}|rgb\(\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}\s*\))$/i;

/** Cor de uma tag `<span style="color:red">` ou `<font color="#c00">`; vazio se não houver ou não for segura. */
export function colorFromTag(attrs: string): string {
  const m = /color\s*[:=]\s*["']?\s*([^;"'>]+?)\s*(?:[;"'>]|$)/i.exec(attrs);
  const c = m?.[1]?.trim() ?? "";
  return COLOR_RE.test(c) ? c : "";
}

const CHECKS = /[☐☑☒]/;

function splitChecks(n: MNode): MNode[] {
  const text = n.value ?? "";
  if (!CHECKS.test(text) || !n.position) return [n];
  let line = n.position.start.line;
  let col = n.position.start.column;
  const out: MNode[] = [];
  let buf = "";
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === "\n") { line++; col = 1; buf += ch; continue; }
    if (CHECKS.test(ch)) {
      if (buf) out.push({ type: "text", value: buf });
      buf = "";
      out.push({
        type: "mdCheck",
        data: { hName: "span", hProperties: { "data-check-line": line, "data-check-col": col, className: "md-check" } },
        children: [{ type: "text", value: ch }],
      });
    } else buf += ch;
    col++;
  }
  if (buf) out.push({ type: "text", value: buf });
  return out;
}

function walk(children: MNode[]): MNode[] {
  const out: MNode[] = [];
  const stack: { color: string; kids: MNode[] }[] = [];
  const push = (n: MNode) => (stack.length ? stack[stack.length - 1].kids : out).push(n);
  for (const n of children) {
    if (n.type === "html") {
      const v = (n.value ?? "").trim();
      if (/^<br\s*\/?>$/i.test(v)) { push({ type: "break" }); continue; }
      const open = /^<(span|font)\b([^>]*)>$/i.exec(v);
      if (open) { stack.push({ color: colorFromTag(open[2]), kids: [] }); continue; }
      if (/^<\/(span|font)>$/i.test(v) && stack.length) {
        const f = stack.pop()!;
        if (f.color) push({ type: "mdColor", data: { hName: "span", hProperties: { style: `color:${f.color}` } }, children: f.kids });
        else f.kids.forEach(push);
        continue;
      }
      continue; // outro HTML: continua ignorado, como antes
    }
    if (n.type === "text") { splitChecks(n).forEach(push); continue; }
    if (n.children && n.type !== "code" && n.type !== "inlineCode") n.children = walk(n.children);
    push(n);
  }
  while (stack.length) { const f = stack.pop()!; f.kids.forEach(push); } // tag aberta sem fechar: mantém o texto
  return out;
}

/** Plugin remark: HTML mínimo (br, span/font com cor) e ☐/☑ clicáveis. */
export function remarkMdExtras() {
  return (tree: MNode) => { if (tree.children) tree.children = walk(tree.children); };
}

function softToBreaks(children: MNode[]): MNode[] {
  const out: MNode[] = [];
  for (const n of children) {
    if (n.type === "text" && (n.value ?? "").includes("\n")) {
      (n.value ?? "").split("\n").forEach((part, i) => {
        if (i > 0) out.push({ type: "break" });
        if (part) out.push({ type: "text", value: part });
      });
    } else {
      if (n.children && n.type !== "code" && n.type !== "inlineCode") n.children = softToBreaks(n.children);
      out.push(n);
    }
  }
  return out;
}

/** Plugin remark opcional: o Enter simples dentro de um parágrafo vira quebra de linha (como nos comentários do GitHub). */
export function remarkSoftBreaks() {
  return (tree: MNode) => { if (tree.children) tree.children = softToBreaks(tree.children); };
}

/** Remove as barras de escape do export do Confluence (`\*\*`, `\<br\>`, `\``), exceto `\|` e blocos de código. */
export function cleanConfluenceEscapes(text: string): string {
  let fence = false;
  return text.split("\n").map((line) => {
    if (/^\s*(```|~~~)/.test(line)) { fence = !fence; return line; }
    if (fence) return line;
    return line.replace(/\\([*_`<>[\]()#+!~.-])/g, "$1");
  }).join("\n");
}

/** Negrito com espaço dentro dos marcadores (`**Texto **`, `** Texto**`, inclusive espaço não separável) não é
 * negrito no CommonMark: os espaços saem de dentro e vão para fora (`**Texto** `). */
export function fixLooseBold(line: string): string {
  return line.replace(/\*\*(\s*)([^*\n]*?[^*\s])(\s*)\*\*/g, (m, a: string, body: string, c: string) =>
    a || c ? `${a ? " " : ""}**${body}**${c ? " " : ""}` : m);
}

/** Correção do Markdown vindo do Confluence: escapes e negrito com espaço dentro. Blocos de código ficam como estão. */
export function fixMarkdown(text: string): string {
  let fence = false;
  return cleanConfluenceEscapes(text).split("\n").map((line) => {
    if (/^\s*(```|~~~)/.test(line)) { fence = !fence; return line; }
    return fence ? line : fixLooseBold(line);
  }).join("\n");
}

/** Há o que corrigir (escapes do Confluence ou negrito com espaço dentro)? */
export function hasConfluenceEscapes(text: string): boolean {
  return fixMarkdown(text) !== text;
}

/** Alterna ☐↔☑ na linha/coluna (1-based) do texto; devolve o texto igual se a posição não tiver o caractere. */
export function toggleCheckAt(text: string, line: number, col: number): string {
  const lines = text.split("\n");
  const l = lines[line - 1];
  if (l === undefined) return text;
  const ch = l[col - 1];
  const next = ch === "☐" ? "☑" : ch === "☑" || ch === "☒" ? "☐" : "";
  if (!next) return text;
  lines[line - 1] = l.slice(0, col - 1) + next + l.slice(col);
  return lines.join("\n");
}

/** Alterna `- [ ]`↔`- [x]` na linha (1-based). */
export function toggleTaskAt(text: string, line: number): string {
  const lines = text.split("\n");
  const l = lines[line - 1];
  if (l === undefined) return text;
  const r = l.replace(/^(\s*(?:[-*+]|\d+[.)])\s+\[)([ xX])(\])/, (_m, a, c, b) => a + (c === " " ? "x" : " ") + b);
  if (r === l) return text;
  lines[line - 1] = r;
  return lines.join("\n");
}
