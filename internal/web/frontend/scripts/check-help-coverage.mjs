#!/usr/bin/env node
// Confere a ajuda contra as abas do app.
//
//   node scripts/check-help-coverage.mjs            lista o que falta (aviso, sai com 0)
//   node scripts/check-help-coverage.mjs --strict   falta de página também reprova (sai com 1)
//
// Sempre reprova (independe do --strict), por ser defeito e não lacuna:
//   - entrada no registro sem arquivo help/pages/<id>.md
//   - arquivo .md sem entrada no registro
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
const unknownInRegistry = [...registryIds].filter((id) => !tabIds.has(id)).sort();

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
for (const id of unknownInRegistry) {
  warn(`"${id}" está no registro, mas não é id de nenhuma aba (renomeada ou removida?)`);
}
if (missing.length > 0) {
  const msg = `${missing.length} aba(s) sem ajuda: ${missing.join(", ")}`;
  strict ? (fail(msg), defects++) : warn(msg);
}

if (defects > 0) process.exit(1);
