#!/usr/bin/env node
// Confere a ajuda contra as abas do app.
//
//   node scripts/check-help-coverage.mjs            lista o que falta (aviso, sai com 0)
//   node scripts/check-help-coverage.mjs --strict   falta de página também reprova (sai com 1)
//
// Sempre reprova (independe do --strict), por ser defeito e não lacuna:
//   - entrada no registro sem arquivo help/pages/<id>.md
//   - arquivo .md sem entrada no registro
//   - link interno [texto](ajuda:<id>#<seção>) para página ou seção que não existe
//
// Páginas com id `geral-*` explicam um padrão compartilhado (não são de uma aba) e ficam de fora
// da conferência de cobertura.
//
// Ids de aba vêm dos menus (ToolsMenu, WorkloadMenu) mais as abas principais abaixo, que não
// estão em menu nenhum.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const src = join(root, "src");
const strict = process.argv.includes("--strict");

// Abas principais que o Index renderiza sem passar por ToolsMenu/WorkloadMenu.
const CORE_TABS = ["dashboard", "hpas", "nodepools", "staging", "explorer", "code-editor"];

const idsIn = (file, re) => [...readFileSync(join(src, file), "utf8").matchAll(re)].map((m) => m[1]);

const tabIds = new Set([
  ...CORE_TABS,
  ...idsIn("components/ToolsMenu.tsx", /\bid:\s*"([a-z0-9-]+)"\s*,\s*label:/g),
  ...idsIn("components/WorkloadMenu.tsx", /\bid:\s*"([a-z0-9-]+)"\s*,\s*label:/g),
]);

// Entradas do registro: `id: "x"` seguido de `title:` (ignora a definição de tipo `id: string`).
const registryIds = new Set(idsIn("help/helpRegistry.ts", /\bid:\s*"([a-z0-9-]+)"\s*,\s*title:/g));

const pagesDir = join(src, "help", "pages");
const pageIds = new Set(existsSync(pagesDir) ? readdirSync(pagesDir).filter((f) => f.endsWith(".md")).map((f) => f.slice(0, -3)) : []);

const missing = [...tabIds].filter((id) => !registryIds.has(id)).sort();
const registryWithoutPage = [...registryIds].filter((id) => !pageIds.has(id)).sort();
const pageWithoutRegistry = [...pageIds].filter((id) => !registryIds.has(id)).sort();
const unknownInRegistry = [...registryIds].filter((id) => !tabIds.has(id) && !id.startsWith("geral-")).sort();

// Mesma regra de âncora de helpSearch.ts (slugify/plainHeadingText): manter igual.
const slugify = (text) =>
  text
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*_~]/g, "")
    .trim()
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");

function headingIds(markdown) {
  const ids = new Set();
  let inFence = false;
  for (const line of markdown.split("\n")) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    else if (!inFence) {
      const m = /^(#{2,3})\s+(.+?)\s*#*\s*$/.exec(line);
      if (m) ids.add(slugify(m[2]));
    }
  }
  return ids;
}

const brokenLinks = [];
const pageHeadings = new Map([...pageIds].map((id) => [id, headingIds(readFileSync(join(pagesDir, `${id}.md`), "utf8"))]));
for (const id of pageIds) {
  const text = readFileSync(join(pagesDir, `${id}.md`), "utf8");
  for (const m of text.matchAll(/\]\(ajuda:([a-z0-9-]+)(?:#([a-z0-9-]+))?\)/g)) {
    const [, target, anchor] = m;
    if (!pageIds.has(target)) brokenLinks.push(`${id}.md aponta para a página inexistente "${target}"`);
    else if (anchor && !pageHeadings.get(target).has(anchor)) brokenLinks.push(`${id}.md aponta para a seção inexistente "${target}#${anchor}"`);
  }
}

const inCI = process.env.GITHUB_ACTIONS === "true";
const warn = (msg) => console.log(inCI ? `::warning title=Ajuda::${msg}` : `aviso: ${msg}`);
const fail = (msg) => console.log(inCI ? `::error title=Ajuda::${msg}` : `ERRO: ${msg}`);

const covered = [...tabIds].filter((id) => registryIds.has(id) && pageIds.has(id)).length;
console.log(`Ajuda: ${covered} de ${tabIds.size} abas com página (${Math.round((covered / tabIds.size) * 100)}%).`);

let defects = 0;
for (const id of registryWithoutPage) {
  fail(`"${id}" está no registro, mas falta src/help/pages/${id}.md`);
  defects++;
}
for (const id of pageWithoutRegistry) {
  fail(`src/help/pages/${id}.md existe, mas "${id}" não está em src/help/helpRegistry.ts`);
  defects++;
}
for (const msg of brokenLinks) {
  fail(`link quebrado: ${msg}`);
  defects++;
}
for (const id of unknownInRegistry) {
  warn(`"${id}" está no registro, mas não é id de nenhuma aba (renomeada ou removida?)`);
}
if (missing.length > 0) {
  const msg = `${missing.length} aba(s) sem ajuda: ${missing.join(", ")}`;
  strict ? (fail(msg), defects++) : warn(msg);
}

if (defects > 0) process.exit(1);
