// Carrega o texto de uma página de ajuda sob demanda: cada .md vira um chunk à parte, então o
// bundle principal não cresce com as páginas.
const pages = import.meta.glob("./pages/*.md", { query: "?raw", import: "default" }) as Record<
  string,
  () => Promise<string>
>;

/** Markdown da página `id`, ou null se ela ainda não existe. */
export async function loadHelpPage(id: string): Promise<string | null> {
  const load = pages[`./pages/${id}.md`];
  return load ? load() : null;
}

/** Ids que têm arquivo em help/pages. */
export function helpPageIds(): string[] {
  return Object.keys(pages).map((p) => p.replace("./pages/", "").replace(/\.md$/, ""));
}
