import { useEffect, useState } from "react";
import { AlertTriangle, Cloud, HardDrive, Loader2, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";
import type { AzureLifecycleRule, AzureStorageHit, AzureStorageSearchResult } from "@/lib/api/types";

// Plano B da busca: quando o nome não aparece em nenhum volume/dependência dos clusters, procura o
// storage (conta, disco, container, file share) pelas subscriptions dos clusters. Só leitura, a
// pedido (botão) — nunca dispara sozinho, porque cada busca faz várias chamadas ao Azure.

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

function HitCard({ hit, cluster }: { hit: AzureStorageHit; cluster?: string }) {
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

      {hit.containers && hit.containers.length > 0 && (
        <div className="text-[11px]">
          <span className="text-muted-foreground">Containers: </span>
          <span className="font-mono">{hit.containers.join(", ")}</span>
        </div>
      )}
      <Lifecycle hit={hit} />
    </div>
  );
}

export function AzureStorageFallback({ query, cluster }: { query: string; cluster?: string }) {
  const q = query.trim();
  const [loading, setLoading] = useState(false);
  const [deepLoading, setDeepLoading] = useState(false);
  const [allLoading, setAllLoading] = useState(false);
  const [result, setResult] = useState<AzureStorageSearchResult | null>(null);
  const [error, setError] = useState("");

  // outra busca/cluster: descarta o resultado anterior
  useEffect(() => {
    setResult(null);
    setError("");
  }, [q, cluster]);

  if (!VALID_QUERY.test(q)) return null;

  // deep/all mantêm o escopo da busca anterior: "também em containers" não esquece o "em todas".
  const run = async (opts: { deep?: boolean; all?: boolean }) => {
    const deep = opts.deep ?? result?.deep ?? false;
    const all = opts.all ?? result?.all ?? false;
    const setBusy = opts.all ? setAllLoading : opts.deep ? setDeepLoading : setLoading;
    setBusy(true);
    setError("");
    try {
      setResult(await apiClient.searchAzureStorage(q, { cluster, deep, all }));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Falha ao buscar na subscription");
    } finally {
      setBusy(false);
    }
  };
  const busy = loading || deepLoading || allLoading;

  return (
    <div className="mt-3 rounded-md border border-dashed border-border/70 p-3 space-y-2.5 text-xs">
      <div className="flex items-center gap-2 flex-wrap">
        <Cloud className="w-4 h-4 text-muted-foreground" />
        <span className="text-muted-foreground">
          "<span className="font-mono">{q}</span>" não foi encontrado {cluster ? "neste cluster" : "nos clusters"}. Procurar na subscription (Azure)?
        </span>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={busy} onClick={() => run({ deep: false, all: false })}>
          {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
          {loading ? "Buscando…" : "Buscar na subscription"}
        </Button>
      </div>

      {error && <div className="text-red-400 whitespace-pre-wrap">{error}</div>}

      {result && (
        <div className="space-y-2">
          <div className="text-muted-foreground">
            {result.results.length} resultado(s) em {result.subscriptions.length} subscription(s)
            {result.subscriptions.length === 1 && result.subscriptions[0].name ? ` (${result.subscriptions[0].name})` : ""}
            {result.deep ? " — incluindo containers e file shares" : ""}.
            {result.truncated && " Lista truncada: refine o nome."}
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
                <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={busy} onClick={() => run({ all: true })}
                  title="Inclui as subscriptions que nenhum cluster usa, mas o az login do servidor enxerga">
                  {allLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Cloud className="w-3.5 h-3.5" />}
                  {allLoading ? "Consultando subscriptions…" : "Buscar em todas as subscriptions do az login"}
                </Button>
              )}
              {!result.deep && (
                <Button size="sm" variant="outline" className="h-7 text-xs gap-1.5" disabled={busy} onClick={() => run({ deep: true })}
                  title="Lista os containers e file shares das contas (uma chamada por conta; pode levar um minuto)">
                  {deepLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
                  {deepLoading ? "Varrendo contas…" : "Buscar também em containers e file shares"}
                </Button>
              )}
            </div>
          ) : (
            <div className="space-y-2">
              {result.results.map((h) => <HitCard key={`${h.subscription_id}/${h.type}/${h.account ?? ""}/${h.name}`} hit={h} cluster={cluster} />)}
              {!result.deep && (
                <Button size="sm" variant="ghost" className="h-7 text-xs gap-1.5" disabled={busy} onClick={() => run({ deep: true })}
                  title="Lista os containers e file shares das contas (uma chamada por conta; pode levar um minuto)">
                  {deepLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
                  {deepLoading ? "Varrendo contas…" : "Buscar também em containers e file shares"}
                </Button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
