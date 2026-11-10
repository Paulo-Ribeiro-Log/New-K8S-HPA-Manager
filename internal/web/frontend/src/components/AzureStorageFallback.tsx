import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Cloud, FolderOpen, HardDrive, Loader2, Search, Square, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";
import type { AzureAccountContents, AzureLifecycleRule, AzureStorageHit, AzureStorageSearchResult } from "@/lib/api/types";
import { clearStored, contentsKey, hitKey, loadStored, mergeSearchResults, saveStored, searchKey } from "@/lib/azureStorageSearchStore";

// Plano B da busca: quando o nome não aparece em nenhum volume/dependência dos clusters, procura o
// storage (conta, disco, container, file share) pelas subscriptions dos clusters. Só leitura, a
// pedido (botão) — nunca dispara sozinho, porque cada busca faz várias chamadas ao Azure.
//
// O que foi achado PERSISTE (memória + localStorage, ver azureStorageSearchStore): não some ao trocar
// de aba nem quando uma busca seguinte falha, e cada busca nova se mescla ao resultado anterior.

const TYPE_LABEL: Record<AzureStorageHit["type"], string> = {
  storage_account: "Storage account",
  disk: "Disco",
  container: "Container (blob)",
  file_share: "File share",
};

const VALID_QUERY = /^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$/;

function ruleText(r: AzureLifecycleRule): string {
  const parts: string[] = [];
  if (r.blob_types?.length) parts.push(`tipos: ${r.blob_types.join(", ")}`);
  parts.push(r.prefixes?.length ? `prefixos: ${r.prefixes.join(", ")}` : "todos os blobs");
  parts.push(r.actions.length ? r.actions.join("; ") : "sem ações");
  return parts.join(" · ");
}

function Lifecycle({ hit }: { hit: AzureStorageHit }) {
  const lc = hit.lifecycle;
  if (!lc) return null;
  if (lc.error) {
    return <div className="text-[11px] text-amber-300">Ciclo de vida: não foi possível ler ({lc.error})</div>;
  }
  if (!lc.has_policy || lc.rules.length === 0) {
    return (
      <div className="text-[11px] text-amber-300">
        Ciclo de vida: {lc.has_policy ? "política sem regras" : "nenhuma regra"} — nenhum blob é movido de camada nem expira sozinho.
      </div>
    );
  }
  return (
    <div className="text-[11px] space-y-0.5">
      <div className="text-muted-foreground">Ciclo de vida ({lc.rules.length} regra{lc.rules.length > 1 ? "s" : ""}):</div>
      {lc.rules.map((r) => (
        <div key={r.name} className={r.enabled ? "" : "opacity-60"}>
          <span className="font-medium">{r.name}</span>
          {!r.enabled && <span className="text-amber-300"> (desativada)</span>}
          <span className={r.deletes && r.enabled ? "text-red-300" : "text-muted-foreground"}> — {ruleText(r)}</span>
        </div>
      ))}
    </div>
  );
}

const fmtDate = (iso?: string) => {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString("pt-BR");
};

// Containers e file shares de UMA conta, lidos sem refazer a busca.
function AccountContents({ c }: { c: AzureAccountContents }) {
  return (
    <div className="rounded border border-border/50 bg-muted/20 p-2 space-y-1.5 text-[11px]">
      <div className="text-muted-foreground">
        Lido em {new Date(c.queried_at).toLocaleString("pt-BR")}
        {c.truncated && " · lista truncada"}
      </div>
      {(c.warnings ?? []).map((w, i) => (
        <div key={i} className="flex items-start gap-1.5 text-amber-300"><AlertTriangle className="w-3 h-3 shrink-0 mt-px" /><span>{w}</span></div>
      ))}

      <div>
        <div className="font-medium">Containers ({c.containers?.length ?? 0})</div>
        {(c.containers ?? []).length === 0 ? (
          <div className="text-muted-foreground">Nenhum container.</div>
        ) : (
          <div className="max-h-48 overflow-auto">
            <table className="w-full">
              <tbody>
                {c.containers.map((ct) => (
                  <tr key={ct.name} className="border-b border-border/20 align-top">
                    <td className="py-0.5 pr-3 font-mono break-all">{ct.name}</td>
                    <td className="py-0.5 pr-3 whitespace-nowrap">
                      {ct.public_access && ct.public_access !== "None"
                        ? <span className="text-red-300" title="Leitura anônima habilitada neste container">acesso público: {ct.public_access}</span>
                        : <span className="text-muted-foreground">privado</span>}
                    </td>
                    <td className="py-0.5 pr-3 whitespace-nowrap text-muted-foreground">
                      {ct.immutable && <span className="text-sky-300 mr-2" title="Política de imutabilidade (WORM)">imutável</span>}
                      {ct.deleted && <span className="text-amber-300 mr-2">apagado (soft delete)</span>}
                      {ct.lease_state && ct.lease_state !== "Available" ? `lease: ${ct.lease_state}` : ""}
                    </td>
                    <td className="py-0.5 text-right whitespace-nowrap text-muted-foreground">{fmtDate(ct.last_modified)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div>
        <div className="font-medium">File shares ({c.shares?.length ?? 0})</div>
        {(c.shares ?? []).length === 0 ? (
          <div className="text-muted-foreground">Nenhum file share.</div>
        ) : (
          <div className="max-h-40 overflow-auto">
            <table className="w-full">
              <tbody>
                {c.shares.map((sh) => (
                  <tr key={sh.name} className="border-b border-border/20 align-top">
                    <td className="py-0.5 pr-3 font-mono break-all">{sh.name}</td>
                    <td className="py-0.5 pr-3 text-muted-foreground">{[sh.protocol, sh.tier, sh.quota_gb ? `${sh.quota_gb} GiB` : ""].filter(Boolean).join(" · ")}</td>
                    <td className="py-0.5 text-right whitespace-nowrap text-muted-foreground">{fmtDate(sh.last_modified)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function HitCard({ hit, cluster, contents, contentsBusy, contentsError, onLoadContents }: {
  hit: AzureStorageHit;
  cluster?: string;
  contents?: AzureAccountContents;
  contentsBusy: boolean;
  contentsError: string;
  onLoadContents: (h: AzureStorageHit) => void;
}) {
  const where = hit.subscription_name || hit.subscription_id;
  const owners = hit.clusters?.length ? hit.clusters.join(", ") : "—";
  return (
    <div className="rounded-md border border-border/60 bg-card p-3 space-y-1.5">
      <div className="flex items-center gap-2 flex-wrap">
        <Badge variant="outline" className="text-[10px]">{TYPE_LABEL[hit.type]}</Badge>
        <span className="font-mono text-sm break-all">{hit.name}</span>
        {hit.aks_managed && <Badge variant="outline" className="text-[10px] border-sky-500/40 text-sky-300" title="Resource group de infraestrutura do AKS (MC_…)">RG do AKS</Badge>}
      </div>

      <div className="flex items-start gap-1.5 text-[11px] text-amber-300">
        <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-px" />
        {!hit.clusters?.length ? (
          <span>
            Não pertence a nenhum cluster conhecido: está na subscription <b>{where}</b>, que <b>nenhum cluster usa</b>.
          </span>
        ) : (
          <span>
            Não pertence {cluster ? <>ao cluster <b>{cluster}</b></> : "a nenhum cluster conhecido"}, mas está na mesma subscription: <b>{where}</b>
            {!cluster && <> (usada por {owners})</>}.
          </span>
        )}
      </div>

      <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-[11px]">
        <span className="text-muted-foreground">Resource group</span><span className="font-mono break-all">{hit.resource_group || "—"}</span>
        {hit.location && <><span className="text-muted-foreground">Região</span><span>{hit.location}</span></>}
        {hit.account && <><span className="text-muted-foreground">Conta</span><span className="font-mono">{hit.account}</span></>}
        {hit.kind && <><span className="text-muted-foreground">Kind</span><span>{hit.kind}</span></>}
        {hit.sku && <><span className="text-muted-foreground">SKU</span><span>{hit.sku}</span></>}
        {hit.tier && <><span className="text-muted-foreground">Tier</span><span>{hit.tier}</span></>}
        {hit.size_gb && <><span className="text-muted-foreground">Tamanho</span><span>{hit.size_gb} GiB</span></>}
        {hit.state && <><span className="text-muted-foreground">Estado</span><span>{hit.state}{hit.attached_to ? ` (em ${hit.attached_to})` : ""}</span></>}
        <span className="text-muted-foreground">Subscription</span><span className="font-mono break-all">{hit.subscription_id}</span>
      </div>

      <Lifecycle hit={hit} />

      {hit.type === "storage_account" && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-2 flex-wrap">
            <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={contentsBusy} onClick={() => onLoadContents(hit)}
              title="Lista os containers e file shares desta conta (2 chamadas, só desta conta; não refaz a busca)">
              {contentsBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <FolderOpen className="w-3.5 h-3.5" />}
              {contentsBusy ? "Lendo a conta…" : contents ? "Atualizar containers e file shares" : "Ver containers e file shares desta conta"}
            </Button>
            {contentsError && <span className="text-red-400 text-[11px]">{contentsError}</span>}
          </div>
          {contents
            ? <AccountContents c={contents} />
            : hit.containers && hit.containers.length > 0 && (
                <div className="text-[11px]">
                  <span className="text-muted-foreground">Containers (primeiros): </span>
                  <span className="font-mono">{hit.containers.join(", ")}</span>
                </div>
              )}
        </div>
      )}
    </div>
  );
}

export function AzureStorageFallback({ query, cluster }: { query: string; cluster?: string }) {
  const q = query.trim();
  const key = searchKey(cluster, q);
  const [result, setResult] = useState<AzureStorageSearchResult | null>(null);
  const [contents, setContents] = useState<Record<string, AzureAccountContents>>({});
  const [savedAt, setSavedAt] = useState("");
  const [busy, setBusy] = useState<"base" | "all" | "deep" | null>(null);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [error, setError] = useState("");
  const [loadingContents, setLoadingContents] = useState<Set<string>>(new Set());
  const [contentsError, setContentsError] = useState<Record<string, string>>({});
  // Estado mais recente, para os laços assíncronos (a varredura de containers vai uma subscription por vez).
  const cur = useRef<{ result: AzureStorageSearchResult | null; contents: Record<string, AzureAccountContents> }>({ result: null, contents: {} });
  const stopRef = useRef(false);
  const keyRef = useRef(key);

  // Mudou a busca ou o cluster: carrega o que foi guardado para ela (se houver) em vez de descartar.
  useEffect(() => {
    keyRef.current = key;
    stopRef.current = true; // uma varredura em andamento de outra busca não pode gravar nesta
    const s = loadStored(key);
    cur.current = { result: s?.result ?? null, contents: s?.contents ?? {} };
    setResult(s?.result ?? null);
    setContents(s?.contents ?? {});
    setSavedAt(s?.savedAt ?? "");
    setError("");
    setProgress(null);
    setBusy(null);
    setContentsError({});
  }, [key]);

  // Aplica e guarda um novo estado — sempre mesclado ao anterior, nunca substituindo.
  const commit = useCallback((forKey: string, res: AzureStorageSearchResult | null, cts: Record<string, AzureAccountContents>) => {
    if (forKey !== keyRef.current) return; // a tela já mudou de busca
    cur.current = { result: res, contents: cts };
    setResult(res);
    setContents(cts);
    if (res) setSavedAt(saveStored(forKey, { result: res, contents: cts }).savedAt);
  }, []);

  if (!VALID_QUERY.test(q)) return null;

  // Busca nas subscriptions dos clusters (ou em todas, com `all`). Falha não apaga o que já havia.
  const run = async (opts: { all: boolean }) => {
    const forKey = key;
    setBusy(opts.all ? "all" : "base");
    setError("");
    try {
      const next = await apiClient.searchAzureStorage(q, { cluster, all: opts.all });
      commit(forKey, mergeSearchResults(cur.current.result, next), cur.current.contents);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Falha ao buscar na subscription");
    } finally {
      setBusy(null);
    }
  };

  // Procura o nome dentro dos containers e file shares das contas, UMA subscription por vez (cada chamada
  // cabe no prazo, uma falha não perde as demais e o resultado vai aparecendo). Só as subscriptions onde
  // o Azure listou contas; nada do que já foi achado é apagado.
  const runDeep = async () => {
    const forKey = key;
    const subs = cur.current.result?.subscriptions ?? [];
    const scope = subs.filter((s) => s.accounts_listed > 0);
    const todo = scope.length ? scope : subs;
    if (todo.length === 0) return;
    stopRef.current = false;
    setBusy("deep");
    setError("");
    setProgress({ done: 0, total: todo.length });
    let next = 0;
    let done = 0;
    const worker = async () => {
      while (!stopRef.current) {
        const i = next++;
        if (i >= todo.length) return;
        const s = todo[i];
        let partial: AzureStorageSearchResult;
        try {
          partial = await apiClient.searchAzureStorage(q, { cluster, deep: true, all: cur.current.result?.all, subscription: s.id, disks: false });
        } catch (e: unknown) {
          partial = { query: q, deep: true, all: false, subscriptions: [], results: [], truncated: false, queried_at: new Date().toISOString(),
            warnings: [`${s.name || s.id}: ${e instanceof Error ? e.message : "falha na varredura"}`] };
        }
        commit(forKey, mergeSearchResults(cur.current.result, partial), cur.current.contents);
        done++;
        setProgress({ done, total: todo.length });
      }
    };
    await Promise.all([worker(), worker()]);
    if (forKey === keyRef.current) {
      setBusy(null);
      setProgress(null);
    }
  };

  // Containers e file shares de UMA conta já achada: só ela, sem refazer a busca.
  const loadContents = async (hit: AzureStorageHit) => {
    const forKey = key;
    const ck = contentsKey(hit);
    setLoadingContents((prev) => new Set(prev).add(ck));
    setContentsError((prev) => ({ ...prev, [ck]: "" }));
    try {
      const c = await apiClient.getAzureAccountContents(hit.subscription_id, hit.resource_group, hit.name);
      commit(forKey, cur.current.result, { ...cur.current.contents, [ck]: c });
    } catch (e: unknown) {
      setContentsError((prev) => ({ ...prev, [ck]: e instanceof Error ? e.message : "Falha ao ler a conta" }));
    } finally {
      setLoadingContents((prev) => { const n = new Set(prev); n.delete(ck); return n; });
    }
  };

  const forget = () => {
    stopRef.current = true;
    clearStored(key);
    cur.current = { result: null, contents: {} };
    setResult(null);
    setContents({});
    setSavedAt("");
    setError("");
    setProgress(null);
    setBusy(null);
  };

  const working = busy !== null;
  const deepButton = (variant: "outline" | "ghost") =>
    busy === "deep" ? (
      <div className="inline-flex items-center gap-2">
        <span className="inline-flex items-center gap-1.5 text-muted-foreground">
          <Loader2 className="w-3.5 h-3.5 animate-spin" />
          Varrendo contas… {progress ? `${progress.done}/${progress.total} subscriptions` : ""}
        </span>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => { stopRef.current = true; }}>
          <Square className="w-3 h-3" />Parar
        </Button>
      </div>
    ) : (
      <Button size="sm" variant={variant} className="h-7 text-xs gap-1.5" disabled={working || !result?.subscriptions?.length} onClick={runDeep}
        title="Procura o nome dentro dos containers e file shares de todas as contas, uma subscription por vez (uma chamada por conta; pode levar alguns minutos). O que já foi achado fica.">
        <Search className="w-3.5 h-3.5" />
        Procurar o nome dentro de containers e file shares das contas
      </Button>
    );

  return (
    <div className="mt-3 rounded-md border border-dashed border-border/70 p-3 space-y-2.5 text-xs">
      <div className="flex items-center gap-2 flex-wrap">
        <Cloud className="w-4 h-4 text-muted-foreground" />
        <span className="text-muted-foreground">
          "<span className="font-mono">{q}</span>" não foi encontrado {cluster ? "neste cluster" : "nos clusters"}. Procurar na subscription (Azure)?
        </span>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={working} onClick={() => run({ all: false })}>
          {busy === "base" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
          {busy === "base" ? "Buscando…" : result ? "Buscar de novo" : "Buscar na subscription"}
        </Button>
      </div>

      {error && <div className="text-red-400 whitespace-pre-wrap">{error}</div>}

      {result && (
        <div className="space-y-2">
          <div className="flex items-center gap-2 flex-wrap text-muted-foreground">
            <span>
              {result.results.length} resultado(s) em {result.subscriptions.length} subscription(s)
              {result.subscriptions.length === 1 && result.subscriptions[0].name ? ` (${result.subscriptions[0].name})` : ""}
              {result.deep ? " — incluindo containers e file shares" : ""}.
              {result.truncated && " Lista truncada: refine o nome."}
            </span>
            <span className="text-[10px]" title="O resultado fica guardado neste navegador e não some ao trocar de aba">
              Guardado em {savedAt ? new Date(savedAt).toLocaleString("pt-BR") : "—"}
            </span>
            <div className="flex-1" />
            <Button size="sm" variant="ghost" className="h-6 text-[11px] gap-1 text-muted-foreground" disabled={working} onClick={forget}
              title="Esquece este resultado guardado (não apaga nada no Azure)">
              <Trash2 className="w-3 h-3" />Limpar resultado guardado
            </Button>
          </div>

          <details className="text-[11px] text-muted-foreground">
            <summary className="cursor-pointer select-none">Subscriptions consultadas e o que o Azure listou</summary>
            <ul className="mt-1 space-y-0.5">
              {(result.subscriptions ?? []).map((s) => (
                <li key={s.id}>
                  <span className="text-foreground">{s.name || s.id}</span>
                  {(s.clusters ?? []).length === 0 && <span className="text-sky-300"> (sem cluster)</span>}
                  : {s.accounts_listed} conta(s) e {s.disks_listed} disco(s) listados
                </li>
              ))}
            </ul>
          </details>

          {(result.warnings ?? []).map((w, i) => (
            <div key={i} className="flex items-start gap-1.5 text-amber-300"><AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-px" /><span>{w}</span></div>
          ))}

          {result.results.length === 0 ? (
            <div className="flex items-center gap-2 flex-wrap text-muted-foreground">
              <HardDrive className="w-4 h-4" />
              <span>Nenhuma conta ou disco com esse nome{result.deep ? ", nem container ou file share" : ""}.</span>
              {!result.all && (
                <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={working} onClick={() => run({ all: true })}
                  title="Inclui as subscriptions que nenhum cluster usa, mas o az login do servidor enxerga">
                  {busy === "all" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Cloud className="w-3.5 h-3.5" />}
                  {busy === "all" ? "Consultando subscriptions…" : "Buscar em todas as subscriptions do az login"}
                </Button>
              )}
              {deepButton("outline")}
            </div>
          ) : (
            <div className="space-y-2">
              {result.results.map((h) => {
                const ck = contentsKey(h);
                return (
                  <HitCard key={hitKey(h)} hit={h} cluster={cluster} contents={contents[ck]}
                    contentsBusy={loadingContents.has(ck)} contentsError={contentsError[ck] ?? ""} onLoadContents={loadContents} />
                );
              })}
              <div className="flex items-center gap-2 flex-wrap">
                {!result.all && (
                  <Button size="sm" variant="ghost" className="h-7 text-xs gap-1.5" disabled={working} onClick={() => run({ all: true })}
                    title="Inclui as subscriptions que nenhum cluster usa, mas o az login do servidor enxerga">
                    {busy === "all" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Cloud className="w-3.5 h-3.5" />}
                    {busy === "all" ? "Consultando subscriptions…" : "Buscar também em todas as subscriptions do az login"}
                  </Button>
                )}
                {deepButton("ghost")}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
