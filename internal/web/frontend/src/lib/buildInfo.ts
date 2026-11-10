// Qual build está rodando: o servidor informa o seu (GET /health → build) e o navegador sabe qual bundle
// do frontend ele carregou. Se os dois diferem, a página é de um build anterior (cache do navegador, ou o
// servidor foi reiniciado com outro binário) e basta recarregar. Funções puras, testadas em buildInfo.test.ts.

export interface ServerBuild {
  version: string;
  commit?: string;
  commit_short?: string;
  commit_time?: string;
  dirty?: boolean; // havia mudanças sem commit ao compilar
  built_at?: string; // quando o binário foi gerado
  started_at?: string; // quando o processo subiu
  go_version?: string;
  frontend_bundle?: string; // JS principal que o frontend embutido no binário referencia
}

const BUNDLE_RE = /\/assets\/(index-[A-Za-z0-9_-]+\.js)(?:[?#].*)?$/;

// loadedBundleName acha, entre os `src` dos <script> da página, o bundle principal (index-XXXX.js).
// Devolve "" quando não há (ex.: `vite dev`, que serve módulos soltos) — nesse caso nada é comparado.
export function loadedBundleName(scriptSrcs: Iterable<string>): string {
  for (const src of scriptSrcs) {
    const m = BUNDLE_RE.exec(src);
    if (m) return m[1];
  }
  return "";
}

// isFrontendStale diz se a página carregada é de um build diferente do que o servidor embute.
// Sem um dos dois nomes (dev, servidor antigo sem esse campo) não dá para afirmar: não é "desatualizada".
export function isFrontendStale(loaded: string, server?: string): boolean {
  return !!loaded && !!server && loaded !== server;
}

// buildLabel é o texto curto do selo: "4133705" ou "4133705*" quando havia mudanças sem commit.
export function buildLabel(b?: ServerBuild | null): string {
  if (!b) return "";
  const c = b.commit_short || "";
  if (!c) return b.version && b.version !== "dev" ? b.version : "dev";
  return b.dirty ? `${c}*` : c;
}

const fmt = (iso?: string) => {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString("pt-BR");
};

// describeBuild é o texto completo (tooltip), uma informação por linha.
export function describeBuild(b?: ServerBuild | null, loaded = ""): string {
  if (!b) return "Build do servidor indisponível";
  const lines = [`Versão: ${b.version || "dev"}`];
  if (b.commit_short) lines.push(`Commit: ${b.commit_short}${b.dirty ? " (com mudanças sem commit)" : ""}${b.commit_time ? ` — ${fmt(b.commit_time)}` : ""}`);
  if (b.built_at) lines.push(`Binário gerado em: ${fmt(b.built_at)}`);
  if (b.started_at) lines.push(`Servidor iniciado em: ${fmt(b.started_at)}`);
  if (b.frontend_bundle) lines.push(`Frontend do servidor: ${b.frontend_bundle}`);
  if (loaded) lines.push(`Frontend desta página: ${loaded}`);
  return lines.join("\n");
}
