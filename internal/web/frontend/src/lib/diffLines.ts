// Numeração das linhas de um patch unificado (o "patch" que a API do GitHub devolve por arquivo).
// Lê os cabeçalhos "@@ -a,b +c,d @@" e conta as linhas antigas e novas, como na aba "Files changed".

export type DiffLineKind = "hunk" | "add" | "del" | "ctx" | "meta";

export interface DiffLine {
  text: string;
  kind: DiffLineKind;
  /** Número da linha no arquivo antigo (ausente em adição, cabeçalho e meta). */
  oldNo?: number;
  /** Número da linha no arquivo novo (ausente em remoção, cabeçalho e meta). */
  newNo?: number;
}

const HUNK_RE = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

export function parsePatch(patch: string): DiffLine[] {
  if (!patch) return [];
  const out: DiffLine[] = [];
  let oldNo = 0;
  let newNo = 0;
  let inHunk = false;
  for (const text of patch.split("\n")) {
    const m = HUNK_RE.exec(text);
    if (m) {
      oldNo = parseInt(m[1], 10);
      newNo = parseInt(m[2], 10);
      inHunk = true;
      out.push({ text, kind: "hunk" });
    } else if (!inHunk || text.startsWith("\\")) {
      out.push({ text, kind: "meta" }); // "\ No newline at end of file" e o que vier antes do 1º hunk
    } else if (text.startsWith("+")) {
      out.push({ text, kind: "add", newNo: newNo++ });
    } else if (text.startsWith("-")) {
      out.push({ text, kind: "del", oldNo: oldNo++ });
    } else {
      out.push({ text, kind: "ctx", oldNo: oldNo++, newNo: newNo++ });
    }
  }
  return out;
}
