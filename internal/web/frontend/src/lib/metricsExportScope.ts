// Escopo da exportação do Relatório: o que vai para o Markdown, o CSV e o PDF quando o operador só quer um
// cluster (ou alguns namespaces) do que foi coletado. Lógica pura (testada em metricsExportScope.test.ts).
// Não toca na tabela da tela nem no que foi coletado: só filtra as linhas antes de gerar o documento.
import type { ReportResult, ReportRow } from "./metricsReport";

export interface ScopeNamespace {
  namespace: string;
  count: number;
}
export interface ScopeCluster {
  cluster: string;
  namespaces: ScopeNamespace[];
  count: number;
}

const cmp = (a: string, b: string) => a.localeCompare(b, "pt-BR");

/** Chave de um (cluster, namespace). JSON evita colisão com qualquer caractere que apareça nos nomes. */
export const scopeKey = (cluster: string, namespace: string): string => JSON.stringify([cluster, namespace]);

/** Clusters e namespaces do que foi coletado, com a quantidade de aplicações de cada um, em ordem. */
export function scopeTree(rows: ReportRow[]): ScopeCluster[] {
  const byCluster = new Map<string, Map<string, number>>();
  for (const r of rows) {
    const ns = byCluster.get(r.cluster) ?? new Map<string, number>();
    ns.set(r.namespace, (ns.get(r.namespace) ?? 0) + 1);
    byCluster.set(r.cluster, ns);
  }
  return [...byCluster.entries()]
    .sort(([a], [b]) => cmp(a, b))
    .map(([cluster, ns]) => {
      const namespaces = [...ns.entries()].sort(([a], [b]) => cmp(a, b)).map(([namespace, count]) => ({ namespace, count }));
      return { cluster, namespaces, count: namespaces.reduce((n, x) => n + x.count, 0) };
    });
}

/** Tudo marcado: o ponto de partida (e o que vale quando o operador não mexe em nada). */
export function allKeys(tree: ScopeCluster[]): Set<string> {
  return new Set(tree.flatMap((c) => c.namespaces.map((n) => scopeKey(c.cluster, n.namespace))));
}

export function clusterState(tree: ScopeCluster[], selected: Set<string>, cluster: string): "all" | "some" | "none" {
  const c = tree.find((x) => x.cluster === cluster);
  if (!c || c.namespaces.length === 0) return "none";
  const n = c.namespaces.filter((x) => selected.has(scopeKey(cluster, x.namespace))).length;
  return n === 0 ? "none" : n === c.namespaces.length ? "all" : "some";
}

/** Marca ou desmarca o cluster inteiro (todos os namespaces dele). Devolve outro conjunto. */
export function toggleCluster(tree: ScopeCluster[], selected: Set<string>, cluster: string, on: boolean): Set<string> {
  const next = new Set(selected);
  for (const n of tree.find((c) => c.cluster === cluster)?.namespaces ?? []) {
    if (on) next.add(scopeKey(cluster, n.namespace));
    else next.delete(scopeKey(cluster, n.namespace));
  }
  return next;
}

export function toggleNamespace(selected: Set<string>, cluster: string, namespace: string, on: boolean): Set<string> {
  const next = new Set(selected);
  if (on) next.add(scopeKey(cluster, namespace));
  else next.delete(scopeKey(cluster, namespace));
  return next;
}

export interface ScopeSummary {
  /** Falta alguma coisa do que foi coletado. */
  partial: boolean;
  clusters: number;
  totalClusters: number;
  apps: number;
  totalApps: number;
  /** "2 de 5 clusters, 14 de 38 aplicações: cluster-a (todos); cluster-b (ns1, ns2)". */
  note: string;
}

export function describeScope(tree: ScopeCluster[], selected: Set<string>): ScopeSummary {
  const totalApps = tree.reduce((n, c) => n + c.count, 0);
  let apps = 0;
  const parts: string[] = [];
  let clusters = 0;
  for (const c of tree) {
    const chosen = c.namespaces.filter((n) => selected.has(scopeKey(c.cluster, n.namespace)));
    if (chosen.length === 0) continue;
    clusters += 1;
    apps += chosen.reduce((n, x) => n + x.count, 0);
    parts.push(chosen.length === c.namespaces.length ? `${c.cluster} (todos os namespaces)` : `${c.cluster} (${chosen.map((n) => n.namespace).join(", ")})`);
  }
  const partial = apps !== totalApps;
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
  return {
    partial, clusters, totalClusters: tree.length, apps, totalApps,
    note: `${clusters} de ${plural(tree.length, "cluster", "clusters")}, ${apps} de ${plural(totalApps, "aplicação", "aplicações")}: ${parts.join("; ")}`,
  };
}

/** O relatório só com o escopo marcado. Parcial leva `scope_note`, que os documentos mostram no cabeçalho. */
export function filterByScope(result: ReportResult, selected: Set<string>): ReportResult {
  const tree = scopeTree(result.rows);
  const sum = describeScope(tree, selected);
  if (!sum.partial) return result;
  return {
    ...result,
    rows: result.rows.filter((r) => selected.has(scopeKey(r.cluster, r.namespace))),
    scope_note: sum.note,
  };
}

/** Pedaço do nome do arquivo: vazio com tudo marcado; o nome do cluster quando é um só inteiro; senão "parcial". */
export function scopeSlug(tree: ScopeCluster[], selected: Set<string>, slug: (s: string) => string): string {
  const sum = describeScope(tree, selected);
  if (!sum.partial) return "";
  if (sum.clusters === 1) {
    const only = tree.find((c) => clusterState(tree, selected, c.cluster) !== "none")!;
    const s = slug(only.cluster);
    return clusterState(tree, selected, only.cluster) === "all" ? s : `${s}-parcial`;
  }
  return "parcial";
}
