import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Loader2, Play, XCircle, AlertTriangle, Route, Copy, Check } from "lucide-react";
import { ProtectedAction } from "@/components/rbac";
import NetDiscoveryGraph from "@/components/NetDiscoveryGraph";
import { useClusters } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { DOCKER_FIX_BY_REASON } from "@/lib/dockerFixSnippets";
import type { NetDiscoveryHop, NetDiscoveryResult, NetDiscoverySSEEvent } from "@/lib/api/types";

type NetDiscoveryMode = "pod" | "local";

// "Descoberta de Rede" — Fase 1 (IP-ROUTE-DISCOVERY-PLAN.md): traceroute básico + grafo ao vivo,
// sem nenhuma camada de enriquecimento ainda (DNS reverso/ASN/nuvem/cross-reference K8s/
// fingerprint de SO chegam nas Fases 2-4). Mesmo padrão dual pod/local já usado pelo Teste de
// Kafka/Banco de Dados/Latência — nunca um mecanismo novo.
export default function NetDiscoveryTab() {
  const { clusters } = useClusters();
  const [target, setTarget] = useState("");
  const [mode, setMode] = useState<NetDiscoveryMode>("local"); // local = default (ver seção 4.1 do plano: é a rede que o host do backend já enxerga, inclusive infra remota de terceiros)
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState(0);
  const [phaseMessage, setPhaseMessage] = useState("");
  const [hops, setHops] = useState<NetDiscoveryHop[]>([]);
  const [result, setResult] = useState<NetDiscoveryResult | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const esRef = useRef<EventSource | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-net-discovery", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster && mode === "pod",
  });

  const { data: dockerStatus } = useQuery({
    queryKey: ["net-discovery-docker-status"],
    queryFn: () => apiClient.getNetDiscoveryDockerStatus(),
    enabled: mode === "local",
    refetchInterval: mode === "local" ? 15000 : false,
  });
  const dockerReady = mode !== "local" || !!(dockerStatus?.installed && dockerStatus?.daemon_running);

  const canRun =
    !!target.trim() &&
    !running &&
    (mode === "local" ? dockerReady : !!cluster && !!namespace);

  const run = async () => {
    setHops([]);
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando...");
    setRunning(true);
    try {
      const { session_id } = await apiClient.runNetDiscovery({
        target: target.trim(),
        mode,
        cluster: mode === "pod" ? cluster : undefined,
        namespace: mode === "pod" ? namespace : undefined,
      });
      setSessionId(session_id);
    } catch (err) {
      setRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar a descoberta");
    }
  };

  const cancel = async () => {
    if (!sessionId) return;
    try {
      await apiClient.cancelNetDiscovery(sessionId);
    } catch {
      /* ignore — o pod/container é limpo no servidor de qualquer forma */
    }
    esRef.current?.close();
    setRunning(false);
    setPhaseMessage("Descoberta cancelada.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getNetDiscoveryStreamURL(sessionId));
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: NetDiscoverySSEEvent = JSON.parse(e.data);
        setPhaseMessage(event.message);
        setProgress(event.progress);

        if (event.type === "hop" && event.result) {
          const hop = event.result as NetDiscoveryHop;
          setHops((prev) => [...prev, hop]);
        }
        if (event.type === "complete" && event.result) {
          const finalResult = event.result as NetDiscoveryResult;
          setResult(finalResult);
          // Lista final é a fonte de verdade (cobre o caso raro de um evento "hop" ter se perdido
          // no meio do stream) — NetDiscoveryGraph só adiciona a diferença, nunca duplica.
          setHops(finalResult.hops);
          setRunning(false);
        }
        if (event.type === "error") {
          setRunError(event.error || event.message);
          setRunning(false);
        }
      } catch {
        /* ignore evento malformado */
      }
    };

    es.onerror = () => {
      es.close();
      setRunning(false);
    };

    return () => {
      es.close();
      esRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const copyResult = () => {
    if (!result) return;
    const lines = result.hops.map((h) =>
      h.timed_out ? `${h.index}  * * *` : `${h.index}  ${h.ip}  ${h.rtt_ms?.toFixed(1) ?? "?"} ms`
    );
    navigator.clipboard.writeText(
      `Rota até ${result.target_input} (${result.target_ip}):\n${lines.join("\n")}`
    );
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-col gap-3">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-[280px] flex-1">
            <label className="text-xs text-muted-foreground block mb-1">IP ou hostname</label>
            <Input
              placeholder="ex: 8.8.8.8 ou servidor.dominio.com"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              disabled={running}
              onKeyDown={(e) => { if (e.key === "Enter" && canRun) run(); }}
            />
          </div>

          {!running ? (
            <ProtectedAction>
              <Button onClick={run} disabled={!canRun}>
                <Play className="w-4 h-4 mr-1.5" />
                Traçar rota
              </Button>
            </ProtectedAction>
          ) : (
            <Button variant="destructive" onClick={cancel}>
              <XCircle className="w-4 h-4 mr-1.5" />
              Cancelar
            </Button>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-4">
          <RadioGroup
            value={mode}
            onValueChange={(v) => { setMode(v as NetDiscoveryMode); setResult(null); setHops([]); }}
            className="flex items-center gap-4"
          >
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="local" id="net-discovery-local" disabled={running} />
              <label htmlFor="net-discovery-local" className="text-sm cursor-pointer">Direto do servidor</label>
            </div>
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="pod" id="net-discovery-pod" disabled={running} />
              <label htmlFor="net-discovery-pod" className="text-sm cursor-pointer">A partir de um Cluster</label>
            </div>
          </RadioGroup>
          <span className="text-[10px] text-muted-foreground">
            {mode === "local"
              ? "Roda no host do servidor — mesma rede/VPN corporativa já usada pra alcançar clusters e infraestrutura remota."
              : "Roda de dentro do cluster escolhido — útil pra diagnosticar conectividade a partir da perspectiva daquele cluster especificamente."}
          </span>
        </div>

        {mode === "local" && dockerStatus && !dockerReady && (() => {
          const fix = DOCKER_FIX_BY_REASON[dockerStatus.reason ?? "daemon_unreachable"] ?? DOCKER_FIX_BY_REASON.daemon_unreachable;
          return (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 flex flex-col gap-2">
              <div className="text-sm text-amber-700 dark:text-amber-400">
                <span className="font-semibold">{fix.title}</span>
                {dockerStatus.error && <span> — {dockerStatus.error}</span>}
              </div>
              <pre className="rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap overflow-x-auto">
                {fix.snippet}
              </pre>
            </div>
          );
        })()}

        {mode === "pod" && (
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-[220px]">
              <ClusterSelectorForTab
                selectedCluster={cluster}
                onClusterChange={(v) => { setCluster(v); setNamespace(""); setResult(null); setHops([]); }}
                clusters={clusters.map((c) => c.context)}
                tabLabel="Descoberta de Rede"
                clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
              />
            </div>
            <div className="min-w-[180px]">
              <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
              <Select value={namespace} onValueChange={setNamespace} disabled={!cluster || running}>
                <SelectTrigger>
                  <SelectValue placeholder="Selecione o namespace" />
                </SelectTrigger>
                <SelectContent>
                  {namespaces.map((ns) => (
                    <SelectItem key={ns.name} value={ns.name}>{ns.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        )}
      </div>

      <div className="p-6 flex flex-col gap-4">
        {(running || phaseMessage) && (
          <div className="flex items-center gap-2 text-sm">
            {running && <Loader2 className="w-4 h-4 animate-spin text-primary" />}
            <span className="text-muted-foreground">{phaseMessage}</span>
            {running && (
              <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden max-w-xs">
                <div className="h-full bg-primary transition-all" style={{ width: `${progress * 100}%` }} />
              </div>
            )}
          </div>
        )}

        {runError && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <span>{runError}</span>
          </div>
        )}

        {(hops.length > 0 || running) && (
          <NetDiscoveryGraph hops={hops} running={running} />
        )}

        {/* Fingerprint do destino (Fase 2) — SEMPRE com a explicação da heurística junto do
            veredito (os_confidence), nunca só "Linux"/"Windows" seco — mesmo princípio de
            fraseologia neutra já usado no resto da app pra inferências (nunca afirmar como fato
            o que é heurística). */}
        {result?.fingerprint && (
          <div className="rounded-md border border-border p-3 flex flex-col gap-2">
            <div className="flex items-center gap-2 flex-wrap">
              {result.fingerprint.os_guess === "linux" && (
                <Badge variant="outline" className="border-emerald-500/40 text-emerald-600 dark:text-emerald-400">🐧 Linux provável</Badge>
              )}
              {result.fingerprint.os_guess === "windows" && (
                <Badge variant="outline" className="border-blue-500/40 text-blue-600 dark:text-blue-400">🪟 Windows provável</Badge>
              )}
              {!result.fingerprint.os_guess && (
                <Badge variant="outline" className="border-border text-muted-foreground">❓ SO não identificado</Badge>
              )}
              {result.fingerprint.is_web_server && (
                <Badge variant="outline" className="border-purple-500/40 text-purple-600 dark:text-purple-400">🌐 Servidor Web</Badge>
              )}
              {result.fingerprint.ttl != null && (
                <span className="text-xs text-muted-foreground font-mono">TTL {result.fingerprint.ttl}</span>
              )}
            </div>
            {result.fingerprint.os_confidence && (
              <p className="text-xs text-muted-foreground">{result.fingerprint.os_confidence}</p>
            )}
            {result.fingerprint.open_ports && result.fingerprint.open_ports.length > 0 && (
              <div className="flex items-center gap-1.5 flex-wrap">
                <span className="text-xs text-muted-foreground">Portas abertas:</span>
                {result.fingerprint.open_ports.map((p) => (
                  <Badge key={p} variant="outline" className="text-[10px] py-0 font-mono">{p}</Badge>
                ))}
              </div>
            )}
            {result.fingerprint.http_server && (
              <div className="text-xs">
                <span className="text-muted-foreground">Header Server:</span>{" "}
                <span className="font-mono">{result.fingerprint.http_server}</span>
              </div>
            )}
            {result.fingerprint.tls_subject && (
              <div className="text-xs">
                <span className="text-muted-foreground">Certificado TLS:</span>{" "}
                <span className="font-mono">{result.fingerprint.tls_subject}</span>
                {result.fingerprint.tls_issuer && (
                  <span className="text-muted-foreground"> (emitido por {result.fingerprint.tls_issuer})</span>
                )}
              </div>
            )}
          </div>
        )}

        {result && (
          <div className="rounded-md border border-border overflow-hidden">
            <div className="flex items-center justify-between gap-2 px-3 py-2 bg-muted/40 border-b border-border">
              <div className="flex items-center gap-2 text-sm">
                <Route className="w-4 h-4 text-muted-foreground" />
                <span className="font-mono">{result.target_input}</span>
                {result.target_resolved && (
                  <span className="text-xs text-muted-foreground">→ {result.target_ip}</span>
                )}
                <Badge variant="outline" className={result.reached ? "border-emerald-500/40 text-emerald-600 dark:text-emerald-400" : "border-amber-500/40 text-amber-600 dark:text-amber-400"}>
                  {result.reached ? "destino alcançado" : "destino não confirmado"}
                </Badge>
              </div>
              <button
                type="button"
                onClick={copyResult}
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                {copied ? "Copiado" : "Copiar"}
              </button>
            </div>
            <table className="w-full text-xs">
              <thead>
                <tr className="text-muted-foreground">
                  <th className="text-left font-medium px-2.5 py-1.5 w-12">#</th>
                  <th className="text-left font-medium px-2.5 py-1.5">IP</th>
                  <th className="text-left font-medium px-2.5 py-1.5">Latência</th>
                  <th className="text-left font-medium px-2.5 py-1.5">Contexto (DNS/ASN/nuvem)</th>
                </tr>
              </thead>
              <tbody>
                {result.hops.map((h) => (
                  <tr key={h.index} className={h.index > 1 ? "border-t border-border" : ""}>
                    <td className="px-2.5 py-1.5 font-mono">{h.index}</td>
                    <td className="px-2.5 py-1.5 font-mono">
                      {h.timed_out ? <span className="text-muted-foreground">* * *</span> : h.ip}
                      {h.is_target && <Badge variant="outline" className="ml-2 text-[10px] py-0 border-emerald-500/40 text-emerald-600 dark:text-emerald-400">destino</Badge>}
                    </td>
                    <td className="px-2.5 py-1.5 font-mono">{h.rtt_ms ? `${h.rtt_ms.toFixed(1)} ms` : "—"}</td>
                    <td className="px-2.5 py-1.5">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {h.internal_ref && (
                          <Badge
                            variant="outline"
                            className="text-[10px] py-0 border-purple-500/40 text-purple-600 dark:text-purple-400"
                            title={`Cluster: ${h.internal_ref.cluster}${h.internal_ref.namespace ? ` · Namespace: ${h.internal_ref.namespace}` : ""}${h.internal_ref.pod_name ? ` · Pod: ${h.internal_ref.pod_name}` : ""}`}
                          >
                            K8s: {h.internal_ref.name}
                          </Badge>
                        )}
                        {h.cloud_match && (
                          <Badge
                            variant="outline"
                            className={`text-[10px] py-0 ${h.cloud_match === "aws" ? "border-orange-500/40 text-orange-600 dark:text-orange-400" : "border-green-500/40 text-green-600 dark:text-green-400"}`}
                          >
                            {h.cloud_match.toUpperCase()}{h.cloud_region ? ` · ${h.cloud_region}` : ""}
                          </Badge>
                        )}
                        {h.asn && (
                          <span className="font-mono text-muted-foreground" title={h.asn_org || ""}>
                            AS{h.asn}{h.asn_org ? ` — ${h.asn_org}` : ""}
                          </span>
                        )}
                        {h.reverse_dns && <span className="text-muted-foreground truncate max-w-[220px]" title={h.reverse_dns}>{h.reverse_dns}</span>}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!result.reached && (
              <div className="px-3 py-2 text-xs text-muted-foreground border-t border-border">
                Alguns saltos podem legitimamente não responder (firewall/NAT no caminho) — não
                confirma nem descarta que o destino esteja alcançável, só que o traceroute não
                completou dentro do limite de saltos.
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
