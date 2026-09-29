// Comentários de linha/bloco no estilo VS Code para todos os editores Monaco da aplicação
// (instalados globalmente via lib/monacoEditor.tsx → installCommentActions).
//
// O Monaco só sabe comentar linguagens com language configuration registrada, e o
// Code Editor manda muita coisa como "plaintext" (.env, .conf, .lua, .cs, …). Por isso
// resolvemos a sintaxe pelo nome do arquivo com uma tabela própria e fazemos as edições
// nós mesmos. Linguagem sem comentário de bloco (YAML, shell, …) → o "bloco" vira
// comentário de linha em todas as linhas selecionadas.
import type * as MonacoEditorNS from "monaco-editor";
import { toast } from "sonner";

type Monaco = typeof MonacoEditorNS;
type CodeEditor = MonacoEditorNS.editor.IStandaloneCodeEditor;

export interface CommentTokens {
  line?: string;
  block?: [string, string];
}

const C_LIKE: CommentTokens = { line: "//", block: ["/*", "*/"] };
const HASH: CommentTokens = { line: "#" };
const MARKUP: CommentTokens = { block: ["<!--", "-->"] };

// Extensão (sem ponto) ou nome de arquivo inteiro em minúsculas.
const BY_NAME: Record<string, CommentTokens> = {
  // C-like
  go: C_LIKE, ts: C_LIKE, tsx: C_LIKE, mts: C_LIKE, cts: C_LIKE, js: C_LIKE, jsx: C_LIKE,
  mjs: C_LIKE, cjs: C_LIKE, java: C_LIKE, kt: C_LIKE, kts: C_LIKE, scala: C_LIKE, sc: C_LIKE,
  groovy: C_LIKE, gradle: C_LIKE, jenkinsfile: C_LIKE, c: C_LIKE, h: C_LIKE, cpp: C_LIKE,
  cc: C_LIKE, cxx: C_LIKE, hpp: C_LIKE, hh: C_LIKE, hxx: C_LIKE, m: C_LIKE, mm: C_LIKE,
  cs: C_LIKE, fs: { line: "//", block: ["(*", "*)"] }, rs: C_LIKE, swift: C_LIKE, dart: C_LIKE,
  php: C_LIKE, proto: C_LIKE, sol: C_LIKE, zig: { line: "//" }, v: C_LIKE, d: C_LIKE,
  json: C_LIKE, jsonc: C_LIKE, json5: C_LIKE, scss: C_LIKE, less: C_LIKE, styl: C_LIKE,
  css: { block: ["/*", "*/"] }, graphql: HASH, gql: HASH, prisma: C_LIKE, hlsl: C_LIKE,
  glsl: C_LIKE, cue: { line: "//" }, jsonnet: C_LIKE, libsonnet: C_LIKE, bicep: C_LIKE,
  // Hash
  py: { line: "#", block: ['"""', '"""'] }, pyi: { line: "#", block: ['"""', '"""'] },
  sh: HASH, bash: HASH, zsh: HASH, ksh: HASH, fish: HASH, yaml: HASH, yml: HASH,
  rb: HASH, rake: HASH, gemfile: HASH, rakefile: HASH, pl: HASH, pm: HASH, r: HASH,
  dockerfile: HASH, containerfile: HASH, makefile: HASH, mk: HASH, toml: HASH, conf: HASH,
  cfg: HASH, properties: HASH, env: HASH, ".env": HASH, gitignore: HASH, ".gitignore": HASH,
  dockerignore: HASH, ".dockerignore": HASH, gitattributes: HASH, ".gitattributes": HASH,
  editorconfig: HASH, ".editorconfig": HASH, helmignore: HASH, ".helmignore": HASH,
  tf: { line: "#", block: ["/*", "*/"] }, tfvars: { line: "#", block: ["/*", "*/"] },
  hcl: { line: "#", block: ["/*", "*/"] }, nomad: { line: "#", block: ["/*", "*/"] },
  ps1: { line: "#", block: ["<#", "#>"] }, psm1: { line: "#", block: ["<#", "#>"] },
  psd1: { line: "#", block: ["<#", "#>"] }, nix: { line: "#", block: ["/*", "*/"] },
  cmake: HASH, "cmakelists.txt": HASH, ex: HASH, exs: HASH, jl: { line: "#", block: ["#=", "=#"] },
  awk: HASH, tcl: HASH, nim: { line: "#", block: ["#[", "]#"] }, cr: HASH, coffee: { line: "#", block: ["###", "###"] },
  procfile: HASH, "requirements.txt": HASH, pp: HASH, jinja: { block: ["{#", "#}"] },
  j2: { block: ["{#", "#}"] }, tpl: { block: ["{{/*", "*/}}"] }, gotmpl: { block: ["{{/*", "*/}}"] },
  // Double dash
  sql: { line: "--", block: ["/*", "*/"] }, lua: { line: "--", block: ["--[[", "]]"] },
  hs: { line: "--", block: ["{-", "-}"] }, elm: { line: "--", block: ["{-", "-}"] },
  ada: { line: "--" }, adb: { line: "--" }, ads: { line: "--" }, vhd: { line: "--" },
  vhdl: { line: "--" }, purs: { line: "--", block: ["{-", "-}"] },
  // Markup
  html: MARKUP, htm: MARKUP, xhtml: MARKUP, xml: MARKUP, svg: MARKUP, xsd: MARKUP, xsl: MARKUP,
  xaml: MARKUP, csproj: MARKUP, vbproj: MARKUP, fsproj: MARKUP, props: MARKUP, targets: MARKUP,
  config: MARKUP, pom: MARKUP, plist: MARKUP, vue: MARKUP, svelte: MARKUP, md: MARKUP,
  markdown: MARKUP, mdx: { block: ["{/*", "*/}"] }, razor: { block: ["@*", "*@"] }, cshtml: { block: ["@*", "*@"] },
  // Outros
  ini: { line: ";" }, reg: { line: ";" }, asm: { line: ";" }, s: { line: "#" },
  lisp: { line: ";;", block: ["#|", "|#"] }, el: { line: ";;" }, clj: { line: ";;" },
  cljs: { line: ";;" }, edn: { line: ";;" }, scm: { line: ";;", block: ["#|", "|#"] },
  tex: { line: "%" }, sty: { line: "%" }, cls: { line: "%" }, bib: { line: "%" },
  erl: { line: "%" }, hrl: { line: "%" }, matlab: { line: "%", block: ["%{", "%}"] },
  ml: { block: ["(*", "*)"] }, mli: { block: ["(*", "*)"] }, pas: { line: "//", block: ["{", "}"] },
  vb: { line: "'" }, vbs: { line: "'" }, bas: { line: "'" }, bat: { line: "REM" }, cmd: { line: "REM" },
  vim: { line: '"' }, vimrc: { line: '"' }, ".vimrc": { line: '"' },
  f90: { line: "!" }, f95: { line: "!" }, f: { line: "!" },
};

// Fallback pelo languageId do Monaco (arquivo sem extensão reconhecida).
const BY_LANGUAGE: Record<string, CommentTokens> = {
  typescript: C_LIKE, javascript: C_LIKE, go: C_LIKE, java: C_LIKE, kotlin: C_LIKE, csharp: C_LIKE,
  c: C_LIKE, cpp: C_LIKE, rust: C_LIKE, swift: C_LIKE, php: C_LIKE, scss: C_LIKE, less: C_LIKE,
  json: C_LIKE, css: { block: ["/*", "*/"] }, python: BY_NAME.py, yaml: HASH, shell: HASH,
  dockerfile: HASH, makefile: HASH, ruby: HASH, perl: HASH, r: HASH, hcl: BY_NAME.hcl,
  ini: { line: "#" }, sql: BY_NAME.sql, lua: BY_NAME.lua, html: MARKUP, xml: MARKUP,
  markdown: MARKUP, powershell: BY_NAME.ps1, bat: BY_NAME.bat, vb: BY_NAME.vb,
  mysql: BY_NAME.sql, pgsql: BY_NAME.sql, redis: HASH, scala: C_LIKE, dart: C_LIKE,
  "objective-c": C_LIKE, fsharp: BY_NAME.fs, graphql: HASH, proto: C_LIKE, sol: C_LIKE,
  clojure: BY_NAME.clj, elixir: HASH, julia: BY_NAME.jl, coffeescript: BY_NAME.coffee,
  pascal: BY_NAME.pas, tcl: HASH, razor: BY_NAME.razor, handlebars: { block: ["{{!--", "--}}"] },
  twig: { block: ["{#", "#}"] }, liquid: { block: ["{% comment %}", "{% endcomment %}"] },
  bicep: C_LIKE, apex: C_LIKE, azcli: HASH, cypher: { line: "//" }, kusto: { line: "//" },
  "freemarker2": { block: ["<#--", "-->"] }, pug: { line: "//-" }, sb: { line: "'" },
  mips: { line: "#" }, systemverilog: C_LIKE, verilog: C_LIKE, typespec: C_LIKE, wgsl: C_LIKE,
  csp: C_LIKE, st: { block: ["(*", "*)"] }, m3: { block: ["(*", "*)"] }, qsharp: { line: "//" },
  abap: { line: '"' }, cameligo: C_LIKE, pascaligo: C_LIKE, flow9: C_LIKE, ecl: C_LIKE,
  lexon: { line: "COMMENT" }, postiats: C_LIKE, powerquery: C_LIKE, restructuredtext: { line: ".." },
  sparql: HASH, scheme: BY_NAME.scm, pla: HASH, aes: C_LIKE,
};

export function getCommentTokens(fileName: string, languageId?: string): CommentTokens | null {
  const lower = fileName.toLowerCase();
  if (BY_NAME[lower]) return BY_NAME[lower];
  if (lower.startsWith("dockerfile") || lower.startsWith("containerfile")) return HASH;
  if (lower.startsWith(".env")) return HASH;
  const dot = lower.lastIndexOf(".");
  if (dot >= 0 && BY_NAME[lower.slice(dot + 1)]) return BY_NAME[lower.slice(dot + 1)];
  if (languageId && BY_LANGUAGE[languageId]) return BY_LANGUAGE[languageId];
  return null;
}

type LineMode = "toggle" | "add" | "remove";

// Linhas cobertas pela seleção (seleção que termina na coluna 1 não inclui essa linha — igual ao VS Code).
function selectedLines(sel: MonacoEditorNS.Selection): [number, number] {
  let end = sel.endLineNumber;
  if (end > sel.startLineNumber && sel.endColumn === 1) end--;
  return [sel.startLineNumber, end];
}

function leadingWs(s: string): number {
  return s.length - s.trimStart().length;
}

function lineCommentEdits(
  monaco: Monaco, model: MonacoEditorNS.editor.ITextModel, sel: MonacoEditorNS.Selection,
  tokens: CommentTokens, mode: LineMode,
): MonacoEditorNS.editor.IIdentifiedSingleEditOperation[] {
  const [start, end] = selectedLines(sel);
  const lines: { n: number; text: string }[] = [];
  for (let n = start; n <= end; n++) {
    const text = model.getLineContent(n);
    if (text.trim()) lines.push({ n, text });
  }
  if (!lines.length) return [];

  const isCommented = (t: string) => {
    const s = t.trimStart();
    if (tokens.line) return s.startsWith(tokens.line);
    const [o, c] = tokens.block!;
    return s.startsWith(o) && t.trimEnd().endsWith(c);
  };
  const remove = mode === "remove" || (mode === "toggle" && lines.every(l => isCommented(l.text)));
  const edits: MonacoEditorNS.editor.IIdentifiedSingleEditOperation[] = [];

  if (remove) {
    for (const { n, text } of lines) {
      if (!isCommented(text)) continue;
      const col = leadingWs(text) + 1;
      if (tokens.line) {
        let len = tokens.line.length;
        if (text[col - 1 + len] === " ") len++;
        edits.push({ range: new monaco.Range(n, col, n, col + len), text: "" });
      } else {
        const [o, c] = tokens.block!;
        const body = text.trim();
        let inner = body.slice(o.length, body.length - c.length);
        if (inner.startsWith(" ")) inner = inner.slice(1);
        if (inner.endsWith(" ")) inner = inner.slice(0, -1);
        edits.push({ range: new monaco.Range(n, col, n, text.trimEnd().length + 1), text: inner });
      }
    }
    return edits;
  }

  // Adiciona na menor indentação do bloco, como o VS Code.
  const indent = Math.min(...lines.map(l => leadingWs(l.text)));
  for (const { n, text } of lines) {
    if (tokens.line) {
      edits.push({ range: new monaco.Range(n, indent + 1, n, indent + 1), text: tokens.line + " " });
    } else {
      const [o, c] = tokens.block!;
      const endCol = text.trimEnd().length + 1;
      edits.push({ range: new monaco.Range(n, endCol, n, endCol), text: " " + c });
      edits.push({ range: new monaco.Range(n, indent + 1, n, indent + 1), text: o + " " });
    }
  }
  return edits;
}

function blockCommentEdits(
  monaco: Monaco, model: MonacoEditorNS.editor.ITextModel, sel: MonacoEditorNS.Selection,
  tokens: CommentTokens,
): MonacoEditorNS.editor.IIdentifiedSingleEditOperation[] {
  if (!tokens.block) return lineCommentEdits(monaco, model, sel, tokens, "toggle");
  const [o, c] = tokens.block;

  // Seleção vazia → bloco sobre o conteúdo da linha do cursor.
  let range: MonacoEditorNS.IRange = sel;
  if (sel.isEmpty()) {
    const text = model.getLineContent(sel.startLineNumber);
    if (!text.trim()) return [];
    range = new monaco.Range(sel.startLineNumber, leadingWs(text) + 1, sel.startLineNumber, text.trimEnd().length + 1);
  }

  // Ignora espaços nas bordas (seleção de linhas inteiras).
  const trimmed = (r: MonacoEditorNS.IRange) => {
    const full = model.getValueInRange(r);
    const startOff = model.getOffsetAt({ lineNumber: r.startLineNumber, column: r.startColumn }) + leadingWs(full);
    const endOff = model.getOffsetAt({ lineNumber: r.endLineNumber, column: r.endColumn }) - (full.length - full.trimEnd().length);
    return { body: full.trim(), s: model.getPositionAt(startOff), e: model.getPositionAt(Math.max(startOff, endOff)) };
  };
  const unwrap = ({ body, s, e }: ReturnType<typeof trimmed>) => {
    if (!(body.startsWith(o) && body.endsWith(c) && body.length >= o.length + c.length)) return null;
    let inner = body.slice(o.length, body.length - c.length);
    if (inner.startsWith(" ")) inner = inner.slice(1);
    if (inner.endsWith(" ")) inner = inner.slice(0, -1);
    return [{ range: new monaco.Range(s.lineNumber, s.column, e.lineNumber, e.column), text: inner }];
  };

  const sel0 = trimmed(range);
  if (!sel0.body) return [];
  // Já comentado: a seleção em si, ou as linhas inteiras que ela cobre (seleção parcial dentro do bloco).
  const [l1, l2] = selectedLines(sel);
  const lines = trimmed(new monaco.Range(l1, 1, l2, model.getLineMaxColumn(l2)));
  const removed = unwrap(sel0) ?? unwrap(lines);
  if (removed) return removed;

  const { s, e } = sel0;
  return [
    { range: new monaco.Range(s.lineNumber, s.column, s.lineNumber, s.column), text: o + " " },
    { range: new monaco.Range(e.lineNumber, e.column, e.lineNumber, e.column), text: " " + c },
  ];
}

// Nome do arquivo por editor. Padrão: basename da URI do model (prop `path` do <Editor>);
// o Code Editor, que reaproveita um model só entre abas, informa via setCommentFileName.
const fileNameGetters = new WeakMap<object, () => string>();

export function setCommentFileName(editor: CodeEditor, getFileName: () => string) {
  fileNameGetters.set(editor, getFileName);
}

function fileNameOf(editor: CodeEditor, model: MonacoEditorNS.editor.ITextModel): string {
  const getter = fileNameGetters.get(editor);
  if (getter) return getter();
  return model.uri.path.split("/").pop() ?? "";
}

function run(
  editor: CodeEditor, monaco: Monaco,
  build: (model: MonacoEditorNS.editor.ITextModel, sel: MonacoEditorNS.Selection, tokens: CommentTokens) => MonacoEditorNS.editor.IIdentifiedSingleEditOperation[],
) {
  const model = editor.getModel();
  if (!model || editor.getOption(monaco.editor.EditorOption.readOnly)) return;
  const tokens = getCommentTokens(fileNameOf(editor, model), model.getLanguageId());
  if (!tokens) {
    toast.error(`Sintaxe de comentário desconhecida para "${model.getLanguageId()}"`);
    return;
  }
  // Multi-cursor na mesma linha geraria edições duplicadas (e sobrepostas) — deduplica.
  const seen = new Set<string>();
  const edits = (editor.getSelections() ?? []).flatMap(sel => build(model, sel, tokens)).filter(ed => {
    const r = ed.range;
    const key = `${r.startLineNumber}:${r.startColumn}:${r.endLineNumber}:${r.endColumn}:${ed.text}`;
    return !seen.has(key) && !!seen.add(key);
  });
  if (!edits.length) return;
  editor.pushUndoStop();
  editor.executeEdits("code-comments", edits);
  editor.pushUndoStop();
}

export const BLOCK_COMMENT_ACTION_ID = "hpa.comments.toggleBlock";

// Some do menu e desativa os atalhos enquanto o editor está readOnly (ex: lado original do diff).
const EDITABLE = "!editorReadonly";

// Registra as ações com os atalhos do VS Code e entradas no menu de contexto.
// Ações registradas via addAction sobrepõem os atalhos padrão do Monaco.
function registerCommentActions(editor: CodeEditor, monaco: Monaco) {
  const { KeyMod, KeyCode } = monaco;
  const chordK = (k: number) => KeyMod.chord(KeyMod.CtrlCmd | KeyCode.KeyK, KeyMod.CtrlCmd | k);
  const lineAction = (id: string, label: string, mode: LineMode, keybindings: number[], order: number) =>
    editor.addAction({
      id, label, keybindings, precondition: EDITABLE, contextMenuGroupId: "1_modification", contextMenuOrder: order,
      run: ed => run(ed as CodeEditor, monaco, (model, sel, tokens) => lineCommentEdits(monaco, model, sel, tokens, mode)),
    });

  editor.addAction({
    id: BLOCK_COMMENT_ACTION_ID,
    label: "Comentar/descomentar bloco",
    keybindings: [KeyMod.Shift | KeyMod.Alt | KeyCode.KeyA],
    precondition: EDITABLE,
    contextMenuGroupId: "1_modification",
    contextMenuOrder: 10,
    run: ed => run(ed as CodeEditor, monaco, (model, sel, tokens) => blockCommentEdits(monaco, model, sel, tokens)),
  });
  lineAction("hpa.comments.toggleLine", "Comentar/descomentar linha(s)", "toggle",
    // ABNT_C1 = tecla "/" do teclado ABNT2 (ao lado do Shift direito); o KeyCode.Slash é a
    // posição do "/" no layout US, que no ABNT2 é a tecla ";".
    [KeyMod.CtrlCmd | KeyCode.Slash, KeyMod.CtrlCmd | KeyCode.ABNT_C1, KeyMod.CtrlCmd | KeyCode.NumpadDivide], 11);
  lineAction("hpa.comments.addLine", "Adicionar comentário de linha", "add", [chordK(KeyCode.KeyC)], 12);
  lineAction("hpa.comments.removeLine", "Remover comentário de linha", "remove", [chordK(KeyCode.KeyU)], 13);
}

// Instala as ações em todo editor criado a partir de agora (inclusive os dois lados de um
// DiffEditor; o lado readOnly ignora a ação). Idempotente — chamado no beforeMount do wrapper.
const installed = new WeakSet<object>();
export function installCommentActions(monaco: Monaco) {
  if (installed.has(monaco)) return;
  installed.add(monaco);
  monaco.editor.onDidCreateEditor(created => {
    // O evento dispara dentro do construtor, antes do StandaloneCodeEditor terminar de
    // montar o keybinding service — adia para depois do construtor.
    queueMicrotask(() => {
      const editor = created as CodeEditor;
      if (typeof editor.addAction !== "function") return; // editores embutidos (peek view)
      try { registerCommentActions(editor, monaco); } catch (e) { console.warn("codeComments:", e); }
    });
  });
}
