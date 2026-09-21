import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Cpu, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

// Desempenho de CPU medido por SKU/node pool — ver internal/finops/vm_perf.go e
// internal/web/handlers/finops_perf.go. O nome do SKU não fixa o processador (a mesma série roda em
// gerações diferentes de Xeon, e mesmo com o mesmo processador a série pode expor conjuntos
// diferentes de instruções), então a UI mostra o que foi MEDIDO no ambiente, nunca um número de tabela.

export interface PerfComparison {
  relative?: number; // alternativa ÷ atual, por thread (código genérico)
  level: "faster" | "equivalent" | "slightly_slower" | "slower" | "unknown";
  source: "measured" | "same_series" | "unknown";
  cpu_models?: string[];
  noisy?: boolean;
  // "pool": o SKU atual foi medido nos nodes deste pool; "fleet": só em outros clusters (o
  // processador deste pool pode ser outro — o SKU não fixa o processador).
  current_scope?: "pool" | "fleet";
  crypto_relative?: number; // RSA-2048/s da alternativa ÷ do atual
  crypto_level?: "faster" | "equivalent" | "slower";
  lost_features?: string[];
  note: string;
}

export interface PerfInfo {
  score: number;
  nodes: number;
  cpu_models: string[];
  rsa_score?: number;
  features?: string[];
  noisy?: boolean;
  source: string;
  scope?: "pool" | "fleet";
}

interface PerfNode {
  cluster: string;
  node_name: string;
  node_pool: string;
  sku: string;
  cpu_model: string;
  py_score: number;
  py_spread: number;
  rsa_sign_per_sec: number;
  cpu_features?: string;
  measured_at: string;
}

interface PerfSKUSummary {
  sku: string;
  score: number;
  nodes: number;
  clusters: number;
  cpu_models: string[];
  noisy?: boolean;
  rsa_score?: number;
  features?: string[];
  rel_to_best: number;
}

interface PerfRunResult {
  node_name: string;
  node_pool: string;
  sku: string;
  cpu_model?: string;
  py_score?: number;
  py_spread?: number;
  rsa_sign_per_sec?: number;
  noisy?: boolean;
  error?: string;
}

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

/** "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz" → "Xeon Platinum 8370C @ 2.80GHz". */
function shortCPU(model: string): string {
  return model
    .replace(/\(R\)|\(TM\)/g, "")
    .replace(/^Intel\s+/, "")
    .replace(/\s+CPU\s+@/, " @")
    .replace(/\s+/g, " ")
    .trim();
}

const LEVEL_CLS: Record<string, string> = {
  faster: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
  equivalent: "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
  slightly_slower: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400",
  slower: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
};

/** Etiquetas de desempenho de uma alternativa de SKU (CPU geral por thread + criptografia). */
export function PerfBadge({ perf }: { perf?: PerfComparison }) {
  if (!perf) return null;
  if (perf.level === "unknown") {
    return (
      <p className="text-[10px] text-muted-foreground italic" title={perf.note}>
        Desempenho de CPU: sem medição — use "Medir desempenho de CPU"
      </p>
    );
  }
  const cryptoCls = perf.crypto_level === "slower" ? LEVEL_CLS.slower : LEVEL_CLS.faster;
  return (
    <div className="space-y-0.5">
      <div className="flex items-center gap-1.5 flex-wrap text-[10px]">
        <Cpu className="h-3 w-3 text-muted-foreground" />
        <span className={`px-1.5 py-0.5 rounded-full font-medium ${LEVEL_CLS[perf.level]}`} title={perf.note}>
          CPU por thread ×{(perf.relative ?? 0).toFixed(2)}
        </span>
        {perf.crypto_level && perf.crypto_level !== "equivalent" && (
          <span
            className={`px-1.5 py-0.5 rounded-full font-medium ${cryptoCls}`}
            title={`Criptografia (RSA-2048): ×${(perf.crypto_relative ?? 0).toFixed(2)} do SKU atual${
              perf.lost_features?.length ? ` — não expõe: ${perf.lost_features.join(", ")}` : ""
            }`}
          >
            TLS/RSA ×{(perf.crypto_relative ?? 0).toFixed(2)}
          </span>
        )}
        {perf.source === "same_series" && <span className="text-muted-foreground">(inferido da mesma série)</span>}
        {perf.current_scope === "fleet" && (
          <span className="text-amber-600" title="O SKU atual foi medido só em outros clusters, não neste pool — o processador aqui pode ser outro. Meça este pool.">
            atual medido em outro cluster
          </span>
        )}
        {perf.noisy && (
          <span className="text-amber-600 flex items-center gap-0.5" title="Medição instável — repita">
            <AlertTriangle className="h-3 w-3" /> ruidosa
          </span>
        )}
      </div>
      {perf.crypto_level === "slower" && (perf.lost_features?.length ?? 0) > 0 && (
        <p className="text-[10px] text-red-600 dark:text-red-400">
          Não expõe {perf.lost_features!.join(", ")} — terminação TLS/criptografia mais lenta nesta série.
        </p>
      )}
    </div>
  );
}

/** Linha "CPU medida" do SKU atual do pool (ou aviso de que nunca foi medido). */
export function CurrentPerfLine({ perf }: { perf?: PerfInfo }) {
  if (!perf) {
    return <p className="text-[10px] text-muted-foreground italic">CPU não medida</p>;
  }
  const fleet = perf.scope === "fleet";
  return (
    <p
      className={`text-[10px] ${fleet ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"}`}
      title={`${perf.cpu_models.join(" / ")} · ${perf.nodes} node(s) medido(s)${perf.features?.length ? ` · extensões: ${perf.features.join(", ")}` : ""}${
        fleet ? " — medido em OUTROS clusters/pools com este SKU, não neste pool: o processador pode ser outro. Use \"Medir desempenho de CPU\"." : ""
      }`}
    >
      {fleet ? "Medido só em outros clusters" : "Medido neste pool"}: {perf.cpu_models.length ? shortCPU(perf.cpu_models[0]) : "CPU desconhecida"}
      {perf.cpu_models.length > 1 ? ` (+${perf.cpu_models.length - 1})` : ""} · {perf.score.toFixed(1)} pts
      {perf.rsa_score ? ` · RSA ${Math.round(perf.rsa_score)}/s` : ""}
      {perf.source === "same_series" ? " · inferido da série" : ""}
    </p>
  );
}

export function PerfBenchmarkButton({ cluster, pools }: { cluster: string; pools: { name: string; sku: string }[] }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [namespace, setNamespace] = useState("default");
  const [perPool, setPerPool] = useState("2");
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastRun, setLastRun] = useState<PerfRunResult[] | null>(null);

  const stored = useQuery<{ nodes: PerfNode[]; sku_summary: PerfSKUSummary[] }>({
    queryKey: ["finops-perf-benchmark", cluster],
    queryFn: async () => {
      const r = await fetch(`/api/v1/finops/perf-benchmark?cluster=${encodeURIComponent(cluster)}`, { headers: authHeaders() });
      if (!r.ok) throw new Error(`Erro ${r.status}`);
      return r.json();
    },
    enabled: open && !!cluster,
    staleTime: 30 * 1000,
  });

  const isSelected = (name: string) => selected[name] ?? true; // padrão: todos os pools
  const chosen = pools.filter((p) => isSelected(p.name)).map((p) => p.name);
  const nodeEstimate = chosen.length * Number(perPool);

  const run = async () => {
    setRunning(true);
    setError(null);
    setLastRun(null);
    try {
      const r = await fetch("/api/v1/finops/perf-benchmark", {
        method: "POST",
        headers: { ...authHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify({ cluster, namespace: namespace.trim() || "default", node_pools: chosen, nodes_per_pool: Number(perPool) }),
      });
      const body = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error((body as { error?: string }).error ?? `Erro ${r.status}`);
      setLastRun((body as { results: PerfRunResult[] }).results);
      // O comparativo nas sugestões é montado na LEITURA — recarrega pra as etiquetas aparecerem.
      await queryClient.invalidateQueries({ queryKey: ["finops-rightsizing", cluster] });
      await queryClient.invalidateQueries({ queryKey: ["finops-perf-benchmark", cluster] });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} disabled={pools.length === 0}
        title={pools.length === 0 ? "Analise o cluster primeiro para listar os node pools" : undefined}>
        <Cpu className="h-4 w-4 mr-1.5" /> Medir desempenho de CPU
      </Button>

      <Dialog open={open} onOpenChange={(v) => !running && setOpen(v)}>
        <DialogContent className="max-w-3xl max-h-[85vh] overflow-y-auto overflow-x-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><Cpu className="h-4 w-4" /> Desempenho de CPU por node pool</DialogTitle>
          </DialogHeader>

          <div className="space-y-4 text-sm">
            <p className="text-xs text-muted-foreground">
              O nome do SKU <strong>não</strong> garante o processador nem as instruções de CPU que a VM enxerga (a documentação da Microsoft
              lista várias gerações de Xeon por série). Esta medição roda um benchmark curto (~20 s) de CPU por thread nos nodes e guarda o resultado —
              o comparativo "SKU atual × alternativa" das sugestões passa a mostrar o que foi <strong>medido</strong>. Não mede rede, disco nem
              latência de aplicação.
            </p>

            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription className="text-xs">
                Cria <strong>1 pod efêmero por node amostrado</strong> (250m de CPU pedidos, sem privilégios, imagem <code>nicolaka/netshoot</code> ~500&nbsp;MB
                baixada no node se ainda não existir), removido ao final. Nodes cordonados, em reparo ou com pressão de disco/memória são ignorados.
                Um node sem CPU livre não é medido e aparece com o motivo.
              </AlertDescription>
            </Alert>

            <div className="grid grid-cols-2 gap-3">
              <label className="space-y-1 text-xs">
                <span className="text-muted-foreground">Namespace do pod de medição</span>
                <Input value={namespace} onChange={(e) => setNamespace(e.target.value)} placeholder="default" disabled={running} />
              </label>
              <label className="space-y-1 text-xs">
                <span className="text-muted-foreground">Nodes por pool</span>
                <Select value={perPool} onValueChange={setPerPool} disabled={running}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {["1", "2", "3", "4"].map((n) => <SelectItem key={n} value={n}>{n}</SelectItem>)}
                  </SelectContent>
                </Select>
              </label>
            </div>

            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">Node pools</p>
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                {pools.map((p) => (
                  <label key={p.name} className="flex items-center gap-1.5 text-xs cursor-pointer">
                    <Checkbox checked={isSelected(p.name)} disabled={running}
                      onCheckedChange={(v) => setSelected((s) => ({ ...s, [p.name]: v === true }))} />
                    <span className="font-mono">{p.name}</span>
                    <span className="text-muted-foreground">{p.sku}</span>
                  </label>
                ))}
              </div>
            </div>

            <div className="flex items-center gap-3">
              <Button size="sm" onClick={run} disabled={running || chosen.length === 0 || nodeEstimate > 24}>
                {running ? <Loader2 className="h-4 w-4 mr-1.5 animate-spin" /> : <Cpu className="h-4 w-4 mr-1.5" />}
                {running ? "Medindo…" : `Medir até ${nodeEstimate} node(s)`}
              </Button>
              {running && <span className="text-xs text-muted-foreground">Pode levar alguns minutos (o primeiro pull da imagem em cada node é o mais lento).</span>}
              {nodeEstimate > 24 && <span className="text-xs text-red-600">Máximo de 24 nodes por execução — reduza pools ou nodes por pool.</span>}
            </div>

            {error && <Alert variant="destructive"><AlertDescription className="text-xs">{error}</AlertDescription></Alert>}

            {lastRun && (
              <div className="space-y-1">
                <p className="text-xs font-semibold">Resultado desta execução</p>
                <div className="border rounded-md overflow-x-auto">
                  <table className="w-full text-[11px]">
                    <thead className="bg-muted/40 text-muted-foreground">
                      <tr><th className="text-left p-1.5">Pool</th><th className="text-left p-1.5">SKU</th><th className="text-left p-1.5">Processador</th>
                        <th className="text-right p-1.5">CPU (pts)</th><th className="text-right p-1.5">RSA/s</th></tr>
                    </thead>
                    <tbody>
                      {lastRun.map((r) => (
                        <tr key={r.node_name} className="border-t">
                          <td className="p-1.5 font-mono" title={r.node_name}>{r.node_pool}</td>
                          <td className="p-1.5 font-mono">{r.sku}</td>
                          {r.error ? (
                            <td colSpan={3} className="p-1.5 text-red-600 dark:text-red-400 break-words" title={r.error}>✗ {r.error}</td>
                          ) : (
                            <>
                              <td className="p-1.5">{shortCPU(r.cpu_model ?? "")}</td>
                              <td className="p-1.5 text-right">
                                {r.py_score?.toFixed(1)}
                                {r.noisy && <span className="text-amber-600" title={`Instável: ${((r.py_spread ?? 0) * 100).toFixed(0)}% de variação entre amostras`}> ⚠</span>}
                              </td>
                              <td className="p-1.5 text-right">{Math.round(r.rsa_sign_per_sec ?? 0)}</td>
                            </>
                          )}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            <div className="space-y-1">
              <p className="text-xs font-semibold">Desempenho medido por SKU (todos os clusters)</p>
              {stored.isLoading ? (
                <p className="text-xs text-muted-foreground">Carregando…</p>
              ) : (stored.data?.sku_summary.length ?? 0) === 0 ? (
                <p className="text-xs text-muted-foreground">Nenhuma medição ainda.</p>
              ) : (
                <div className="border rounded-md overflow-x-auto">
                  <table className="w-full text-[11px]">
                    <thead className="bg-muted/40 text-muted-foreground">
                      <tr><th className="text-left p-1.5">SKU</th><th className="text-left p-1.5 w-40">CPU por thread</th>
                        <th className="text-right p-1.5">RSA/s</th><th className="text-left p-1.5">Processador</th>
                        <th className="text-right p-1.5">Nodes</th></tr>
                    </thead>
                    <tbody>
                      {stored.data!.sku_summary.map((s) => (
                        <tr key={s.sku} className="border-t align-top">
                          <td className="p-1.5 font-mono">{s.sku}</td>
                          <td className="p-1.5">
                            <div className="flex items-center gap-1.5">
                              <div className="h-1.5 flex-1 rounded bg-muted overflow-hidden">
                                <div className="h-full bg-blue-500" style={{ width: `${Math.min(100, s.rel_to_best * 100)}%` }} />
                              </div>
                              <span className="w-10 text-right">{s.score.toFixed(1)}</span>
                            </div>
                            {s.noisy && <span className="text-[10px] text-amber-600">medição ruidosa</span>}
                          </td>
                          <td className="p-1.5 text-right">{s.rsa_score ? Math.round(s.rsa_score) : "—"}</td>
                          <td className="p-1.5" title={s.features?.length ? `Extensões: ${s.features.join(", ")}` : undefined}>
                            {s.cpu_models.map(shortCPU).join(" / ") || "—"}
                          </td>
                          <td className="p-1.5 text-right" title={`${s.clusters} cluster(s)`}>{s.nodes}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <p className="text-[10px] text-muted-foreground">
                "pts" = iterações/s de um loop de inteiros em Python (melhor de 5 amostras, mediana entre os nodes) — só faz sentido comparando
                SKUs entre si. Passe o mouse no processador para ver as extensões de CPU exibidas à VM.
              </p>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
