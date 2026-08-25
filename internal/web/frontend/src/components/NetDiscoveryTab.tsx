import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
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
import { Loader2, Play, XCircle, AlertTriangle, Route, Copy, Check, History, ChevronDown, ChevronUp, FileDown, ListChecks, KeyRound, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { ptBR } from "date-fns/locale";
import { ProtectedAction } from "@/components/rbac";
import NetDiscoveryGraph from "@/components/NetDiscoveryGraph";
import { CertificateSourcePickerModal } from "@/components/CertificateSourcePickerModal";
import { useClusters } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { exportNetDiscoveryPDF } from "@/lib/netDiscoveryPdfExport";
import { DOCKER_FIX_BY_REASON } from "@/lib/dockerFixSnippets";
import type { NetDiscoveryHistoryEntry, NetDiscoveryHop, NetDiscoveryResult, NetDiscoverySSEEvent } from "@/lib/api/types";

type NetDiscoveryMode = "pod" | "local";

// osLabel — versão compacta (texto puro, pro banner de histórico) do mesmo veredito já mostrado
// como badge no resultado ao vivo (ver bloco `result.fingerprint.os_guess` mais abaixo) — mesmo
// princípio de nunca afirmar certeza (sempre "provável"/"?", nunca um nome seco).
function osLabel(guess?: string): string {
  if (guess === "linux") return "🐧 Linux provável";
  if (guess === "windows") return "🪟 Windows provável";
  return "❓ SO não identificado";
}

// cloudBadgeClass — mesma paleta PROVIDER_COLORS já usada em LatencyTopologyGraph.tsx (aws=laranja,
// gcp=verde, azure=azul) — Azure adicionado na Fase 5, item P3 do roadmap.
function cloudBadgeClass(provider: string): string {
  if (provider === "aws") return "border-orange-500/40 text-orange-600 dark:text-orange-400";
  if (provider === "azure") return "border-blue-500/40 text-blue-600 dark:text-blue-400";
  return "border-green-500/40 text-green-600 dark:text-green-400"; // gcp
}

// "Descoberta de Rede" — Fase 1 (IP-ROUTE-DISCOVERY-PLAN.md): traceroute básico + grafo ao vivo,
// sem nenhuma camada de enriquecimento ainda (DNS reverso/ASN/nuvem/cross-reference K8s/
// fingerprint de SO chegam nas Fases 2-4). Mesmo padrão dual pod/local já usado pelo Teste de
// Kafka/Banco de Dados/Latência — nunca um mecanismo novo.
export default function NetDiscoveryTab() {
  const { clusters } = useClusters();
  const [target, setTarget] = useState("");
  // probePort — porta TCP da sonda do tcptraceroute, string vazia = usa o default do backend (443).
  // Bug real corrigido: 443 fixo nunca alcança um alvo Windows atrás de cofre PAM (Delinea etc.),
  // que tipicamente só tem 3389/445/5985/5986 abertos — dado ao usuário o controle explícito em
  // vez de tentar adivinhar automaticamente uma topologia de rede que este código não observa.
  const [probePort, setProbePort] = useState("");
  // probeTimeoutSec — segundos de espera por resposta de CADA salto, string vazia = default do
  // backend (2s), máximo 8. Pedido explícito do usuário: descartar rede lenta/alta latência antes
  // de aceitar que um alvo atrás de cofre PAM é bloqueado de verdade (visto ao vivo: 21 saltos em
  // silêncio total mesmo trocando a porta — sinal de bloqueio de firewall, não de porta errada).
  const [probeTimeoutSec, setProbeTimeoutSec] = useState("");
  // Certificado de cliente (mTLS) — pedido explícito do usuário depois de perguntar se ter o
  // certificado "que já existe nesses clusters/servidores" ajudaria a descoberta: útil quando o
  // alvo exige certificado de cliente — o ganho confirmado ao vivo é destravar a checagem HTTP
  // (Server:/status), não necessariamente o certificado TLS em si (que na maioria dos
  // terminadores já é lido mesmo sem cert de cliente — ver net_discovery_fingerprint.go). Seção
  // colapsada por padrão (caso raro) — os dois campos NUNCA são persistidos em localStorage nem
  // em nenhum outro lugar, só vivem em memória durante esta sessão do formulário; ver
  // ClientCertPEM/ClientKeyPEM em net_discovery.go.
  const [mtlsExpanded, setMtlsExpanded] = useState(false);
  const [clientCertPEM, setClientCertPEM] = useState("");
  const [clientKeyPEM, setClientKeyPEM] = useState("");
  const mtlsConfigured = clientCertPEM.trim() !== "" && clientKeyPEM.trim() !== "";
  // Picker reaproveitado (Backup Apartado / Extraído de PFX / Rollback) — pedido explícito do
  // usuário depois de perguntar se um certificado já existente ajudaria a descoberta: evita
  // copiar/colar manual quando o certificado de cliente já está salvo em qualquer um dos 3
  // repositórios que a aba Certificados TLS já mantém. defaultTab="manual" porque esta aba não
  // edita nenhum Secret K8s real — a aba "Rollback" do picker (escopada por cluster/namespace/
  // secretName) ficaria sempre vazia aqui, então não faz sentido como aba inicial.
  const [certPickerOpen, setCertPickerOpen] = useState(false);
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

  // Fase 5, item P4 — lote de múltiplos alvos. Decisão de design (ver IP-ROUTE-DISCOVERY-PLAN.md
  // seção 10): fila SEQUENCIAL — reaproveita o MESMO painel de resultado (grafo/tabela/fingerprint,
  // `result`/`hops`/`running`/`progress` acima) pra mostrar sempre "o alvo atualmente ativo na
  // fila", sem duplicar nenhuma renderização — só uma faixa compacta de status acima mostra o
  // progresso de TODOS os alvos do lote. `batchQueueRef` (não só state) evita o bug de stale
  // closure já documentado nesta app (CLAUDE.md, Code Editor) — o handler de SSE abaixo lê sempre
  // o valor mais recente da fila, nunca um capturado no momento em que o efeito foi criado.
  const [inputMode, setInputMode] = useState<"single" | "batch">("single");
  const [batchTargetsText, setBatchTargetsText] = useState("");
  const [batchQueue, setBatchQueue] = useState<{ target: string; sessionId: string }[]>([]);
  const batchQueueRef = useRef<{ target: string; sessionId: string }[]>([]);
  const [batchId, setBatchId] = useState<string | null>(null);
  const [batchStatuses, setBatchStatuses] = useState<Record<string, "queued" | "running" | "done" | "error">>({});
  const [batchSummaries, setBatchSummaries] = useState<Record<string, { reached: boolean; osGuess?: string }>>({});

  // Fase 5 — Histórico de Descobertas: mostra "última busca: ..." pro alvo digitado ANTES mesmo
  // do usuário clicar "Traçar rota" (resolve a dor observada ao vivo nesta sessão — reinvestigar o
  // mesmo host atrás de um cofre PAM do zero, em conversas diferentes). Debounce de 400ms (mesmo
  // padrão já usado noutras buscas desta app) evita 1 request por tecla digitada.
  const [debouncedTarget, setDebouncedTarget] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedTarget(target.trim()), 400);
    return () => clearTimeout(t);
  }, [target]);

  const { data: historyData } = useQuery({
    queryKey: ["net-discovery-history", debouncedTarget],
    queryFn: () => apiClient.getNetDiscoveryHistory(debouncedTarget),
    enabled: debouncedTarget.length > 2,
  });
  const historyEntries: NetDiscoveryHistoryEntry[] = historyData?.entries ?? [];
  const [historyExpanded, setHistoryExpanded] = useState(false);

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

  const probePortNum = probePort.trim() ? Number(probePort.trim()) : undefined;
  const probePortValid = probePortNum === undefined || (Number.isInteger(probePortNum) && probePortNum >= 1 && probePortNum <= 65535);

  const probeTimeoutSecNum = probeTimeoutSec.trim() ? Number(probeTimeoutSec.trim()) : undefined;
  const probeTimeoutSecValid = probeTimeoutSecNum === undefined || (Number.isInteger(probeTimeoutSecNum) && probeTimeoutSecNum >= 1 && probeTimeoutSecNum <= 8);

  const batchTargetsParsed = batchTargetsText.split("\n").map((t) => t.trim()).filter(Boolean);
  const batchTargetsValid = batchTargetsParsed.length > 0 && batchTargetsParsed.length <= 10;

  // mTLS: os dois campos precisam vir juntos, ou nenhum dos dois — mesma checagem que o backend
  // já faz (INVALID_CLIENT_CERT), espelhada aqui pra não gastar um round-trip com algo trivial de
  // checar no cliente.
  const mtlsPairValid = (clientCertPEM.trim() !== "") === (clientKeyPEM.trim() !== "");

  const canRun =
    !running &&
    probePortValid &&
    probeTimeoutSecValid &&
    mtlsPairValid &&
    (mode === "local" ? dockerReady : !!cluster && !!namespace) &&
    (inputMode === "single" ? !!target.trim() : batchTargetsValid);

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
        probe_port: probePortNum,
        probe_timeout_sec: probeTimeoutSecNum,
        client_cert_pem: mtlsConfigured ? clientCertPEM : undefined,
        client_key_pem: mtlsConfigured ? clientKeyPEM : undefined,
      });
      setSessionId(session_id);
    } catch (err) {
      setRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar a descoberta");
    }
  };

  // runBatch — Fase 5/P4. Reaproveita o MESMO estado `sessionId`/`hops`/`result`/`progress` já
  // usado pelo modo single (nunca duplicado) — o handler de SSE mais abaixo detecta que está num
  // lote (via `batchQueueRef`) e encadeia pro próximo alvo sozinho quando um termina.
  const runBatch = async () => {
    setHops([]);
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando lote...");
    setRunning(true);
    setBatchStatuses({});
    setBatchSummaries({});
    setBatchQueue([]);
    batchQueueRef.current = [];
    setBatchId(null);
    try {
      const resp = await apiClient.runNetDiscoveryBatch({
        targets: batchTargetsParsed,
        mode,
        cluster: mode === "pod" ? cluster : undefined,
        namespace: mode === "pod" ? namespace : undefined,
        probe_port: probePortNum,
        probe_timeout_sec: probeTimeoutSecNum,
        client_cert_pem: mtlsConfigured ? clientCertPEM : undefined,
        client_key_pem: mtlsConfigured ? clientKeyPEM : undefined,
      });
      const queue = resp.targets.map((t, i) => ({ target: t, sessionId: resp.session_ids[i] }));
      batchQueueRef.current = queue;
      setBatchQueue(queue);
      setBatchId(resp.batch_id);
      const initialStatuses: Record<string, "queued" | "running"> = {};
      queue.forEach((q, i) => { initialStatuses[q.target] = i === 0 ? "running" : "queued"; });
      setBatchStatuses(initialStatuses);
      setSessionId(queue[0].sessionId);
    } catch (err) {
      setRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o lote");
    }
  };

  const cancel = async () => {
    const cancelTarget = batchId ?? sessionId;
    if (!cancelTarget) return;
    try {
      await apiClient.cancelNetDiscovery(cancelTarget);
    } catch {
      /* ignore — o pod/container é limpo no servidor de qualquer forma */
    }
    esRef.current?.close();
    setRunning(false);
    setBatchId(null);
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

          // Fase 5/P4 — lote: se este sessionId pertence a uma fila em andamento (batchQueueRef,
          // não `batchQueue` — evita stale closure, mesmo cuidado já documentado nesta app pra
          // callback assíncrono disparado por API de terceiro), encadeia pro próximo alvo em vez
          // de encerrar. `finalResult` já dá o resumo (alcançado/SO) pra faixa de status.
          const queue = batchQueueRef.current;
          const idx = queue.findIndex((q) => q.sessionId === sessionId);
          if (idx !== -1) {
            const finishedTarget = queue[idx].target;
            setBatchStatuses((prev) => ({ ...prev, [finishedTarget]: "done" }));
            setBatchSummaries((prev) => ({
              ...prev,
              [finishedTarget]: { reached: finalResult.reached, osGuess: finalResult.fingerprint?.os_guess },
            }));
            if (idx + 1 < queue.length) {
              const next = queue[idx + 1];
              setBatchStatuses((prev) => ({ ...prev, [next.target]: "running" }));
              setHops([]);
              setResult(null);
              setProgress(0);
              setPhaseMessage(`Iniciando ${next.target}...`);
              setSessionId(next.sessionId);
              return; // lote continua — não encerra running ainda
            }
          }
          setRunning(false);
        }
        if (event.type === "error") {
          // Mesmo encadeamento do "complete" acima — um alvo com erro no meio do lote não para
          // tudo, só marca esse alvo como erro na faixa de status e segue pro próximo.
          const queue = batchQueueRef.current;
          const idx = queue.findIndex((q) => q.sessionId === sessionId);
          if (idx !== -1) {
            const failedTarget = queue[idx].target;
            setBatchStatuses((prev) => ({ ...prev, [failedTarget]: "error" }));
            if (idx + 1 < queue.length) {
              const next = queue[idx + 1];
              setBatchStatuses((prev) => ({ ...prev, [next.target]: "running" }));
              setHops([]);
              setResult(null);
              setProgress(0);
              setPhaseMessage(`Iniciando ${next.target}...`);
              setSessionId(next.sessionId);
              return; // lote continua — erro já registrado na faixa de status, sem poluir runError
            }
          }
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
        {/* Fase 5/P4 — toggle Alvo único / Lote. Trocar de modo com um lote em andamento não é
            permitido (disabled={running}) — evita perder o encadeamento de sessões a meio caminho. */}
        <div className="flex items-center gap-1.5">
          <ListChecks className="w-3.5 h-3.5 text-muted-foreground" />
          <div className="flex rounded-md border border-border overflow-hidden">
            {(["single", "batch"] as const).map((m) => (
              <button
                key={m}
                type="button"
                disabled={running}
                onClick={() => setInputMode(m)}
                className={`text-xs px-2.5 py-1 ${inputMode === m ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"} disabled:opacity-50`}
              >
                {m === "single" ? "Alvo único" : "Lote (até 10)"}
              </button>
            ))}
          </div>
        </div>

        <div className="flex flex-wrap items-end gap-3">
          {inputMode === "single" ? (
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
          ) : (
            <div className="min-w-[280px] flex-1">
              <label className="text-xs text-muted-foreground block mb-1">
                IPs ou hostnames (um por linha, até 10) — {batchTargetsParsed.length}/10
              </label>
              <Textarea
                placeholder={"8.8.8.8\nservidor1.dominio.com\nservidor2.dominio.com"}
                value={batchTargetsText}
                onChange={(e) => setBatchTargetsText(e.target.value)}
                disabled={running}
                rows={3}
                className="font-mono text-xs"
              />
            </div>
          )}

          <div className="w-28">
            <label className="text-xs text-muted-foreground block mb-1">Porta da sonda</label>
            <Input
              type="number"
              min={1}
              max={65535}
              placeholder="443"
              value={probePort}
              onChange={(e) => setProbePort(e.target.value)}
              disabled={running}
              className={!probePortValid ? "border-destructive" : undefined}
              onKeyDown={(e) => { if (e.key === "Enter" && canRun) run(); }}
            />
          </div>

          <div className="w-32">
            <label className="text-xs text-muted-foreground block mb-1">Timeout/salto (s)</label>
            <Input
              type="number"
              min={1}
              max={8}
              placeholder="2"
              value={probeTimeoutSec}
              onChange={(e) => setProbeTimeoutSec(e.target.value)}
              disabled={running}
              className={!probeTimeoutSecValid ? "border-destructive" : undefined}
              onKeyDown={(e) => { if (e.key === "Enter" && canRun) run(); }}
            />
          </div>

          {!running ? (
            <ProtectedAction>
              <Button onClick={inputMode === "single" ? run : runBatch} disabled={!canRun}>
                <Play className="w-4 h-4 mr-1.5" />
                {inputMode === "single" ? "Traçar rota" : "Iniciar Lote"}
              </Button>
            </ProtectedAction>
          ) : (
            <Button variant="destructive" onClick={cancel}>
              <XCircle className="w-4 h-4 mr-1.5" />
              Cancelar
            </Button>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2 -mt-1">
          <span className="text-[10px] text-muted-foreground">
            Padrão 443 (web), timeout 2s/salto. Alvo sem resposta? Tente:
          </span>
          {[
            { port: "3389", label: "3389 · RDP/Windows" },
            { port: "22", label: "22 · SSH" },
            { port: "445", label: "445 · SMB" },
          ].map((p) => (
            <button
              key={p.port}
              type="button"
              disabled={running}
              onClick={() => setProbePort(p.port)}
              className="text-[10px] px-1.5 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-foreground/40 disabled:opacity-50"
            >
              {p.label}
            </button>
          ))}
          <span className="text-[10px] text-muted-foreground">·</span>
          {/* Ainda sem resposta com a porta certa? Rede lenta/alta latência (ex: VPN até um cofre
              PAM) pode precisar de mais tempo por salto antes de considerar bloqueado de verdade —
              pedido explícito do usuário, achado ao vivo: 21 saltos em silêncio total mesmo
              trocando de porta (sinal de firewall dropando tudo, não de porta errada — mas o
              timeout maior descarta de vez a hipótese de rede lenta antes de aceitar isso). */}
          {["5", "8"].map((t) => (
            <button
              key={t}
              type="button"
              disabled={running}
              onClick={() => setProbeTimeoutSec(t)}
              className="text-[10px] px-1.5 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-foreground/40 disabled:opacity-50"
            >
              {t}s/salto
            </button>
          ))}
        </div>

        {/* Certificado de cliente (mTLS) — colapsado por padrão (caso raro), pedido explícito do
            usuário depois de perguntar se ter o certificado "que já existe nesses
            clusters/servidores" ajudaria a descoberta. Útil só quando o alvo exige certificado
            de cliente e rejeita TLS anônimo por completo (ver netDiscoveryFingerprintScript). Os
            dois campos nunca são persistidos em localStorage nem enviados a nenhum outro lugar —
            só vão no corpo do POST /net-discovery/run(-batch) desta execução. */}
        <div className="rounded border border-border bg-background/60 text-xs">
          <button
            type="button"
            onClick={() => setMtlsExpanded((v) => !v)}
            className="flex items-center gap-2 w-full text-left px-3 py-2"
          >
            <KeyRound className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
            <span className="text-muted-foreground">Certificado de cliente (mTLS)</span>
            {mtlsConfigured && (
              <Badge variant="outline" className="text-[10px] py-0 border-emerald-500/40 text-emerald-600 dark:text-emerald-400">
                configurado
              </Badge>
            )}
            <span className="text-[10px] text-muted-foreground ml-auto">opcional — só se o alvo exigir</span>
            {mtlsExpanded ? <ChevronUp className="w-3.5 h-3.5 flex-shrink-0" /> : <ChevronDown className="w-3.5 h-3.5 flex-shrink-0" />}
          </button>
          {mtlsExpanded && (
            <div className="px-3 pb-3 space-y-2">
              <div className="flex items-start justify-between gap-2">
                <p className="text-[10px] text-muted-foreground">
                  Só faz diferença quando o servidor exige certificado de cliente — o ganho confirmado
                  é destravar a checagem HTTP (Server:/status), já que o certificado TLS em si costuma
                  ser lido mesmo sem apresentar um. Nunca salvo — vale só para esta execução.
                </p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-6 text-[10px] px-2 flex-shrink-0"
                  disabled={running}
                  onClick={() => setCertPickerOpen(true)}
                >
                  <History className="w-3 h-3 mr-1" />
                  Certificado salvo...
                </Button>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                <div>
                  <label className="text-xs text-muted-foreground block mb-1">Certificado (PEM)</label>
                  <Textarea
                    placeholder="-----BEGIN CERTIFICATE-----"
                    value={clientCertPEM}
                    onChange={(e) => setClientCertPEM(e.target.value)}
                    disabled={running}
                    rows={4}
                    className="font-mono text-[10px]"
                  />
                </div>
                <div>
                  <label className="text-xs text-muted-foreground block mb-1">Chave privada (PEM)</label>
                  <Textarea
                    placeholder="-----BEGIN PRIVATE KEY-----"
                    value={clientKeyPEM}
                    onChange={(e) => setClientKeyPEM(e.target.value)}
                    disabled={running}
                    rows={4}
                    className="font-mono text-[10px]"
                  />
                </div>
              </div>
              {(clientCertPEM.trim() !== "") !== (clientKeyPEM.trim() !== "") && (
                <p className="text-[10px] text-destructive">Certificado e chave precisam vir juntos, ou nenhum dos dois.</p>
              )}
            </div>
          )}
        </div>

        {historyEntries.length > 0 && (() => {
          const last = historyEntries[0];
          return (
            <div className="rounded border border-border bg-background/60 px-3 py-2 text-xs">
              <button
                type="button"
                onClick={() => setHistoryExpanded((v) => !v)}
                className="flex items-center gap-2 w-full text-left"
              >
                <History className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                <span className="text-muted-foreground">
                  Última busca: <span className="text-foreground">{new Date(last.created_at).toLocaleString("pt-BR")}</span>
                  {" — "}
                  {last.reached ? (
                    <span className="text-emerald-600 dark:text-emerald-400">alcançado</span>
                  ) : (
                    <span className="text-amber-600 dark:text-amber-400">não alcançado</span>
                  )}
                  {" — "}
                  {osLabel(last.result?.fingerprint?.os_guess)}
                  {historyEntries.length > 1 && ` (+${historyEntries.length - 1} busca${historyEntries.length > 2 ? "s" : ""} anterior${historyEntries.length > 2 ? "es" : ""})`}
                </span>
                {historyExpanded ? (
                  <ChevronUp className="w-3.5 h-3.5 text-muted-foreground ml-auto flex-shrink-0" />
                ) : (
                  <ChevronDown className="w-3.5 h-3.5 text-muted-foreground ml-auto flex-shrink-0" />
                )}
              </button>

              {historyExpanded && (
                <div className="mt-2 flex flex-col gap-1.5 border-t border-border pt-2">
                  {historyEntries.map((entry, i) => (
                    <div key={i} className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-muted-foreground">
                      <span className="text-foreground">{new Date(entry.created_at).toLocaleString("pt-BR")}</span>
                      <span>·</span>
                      <span>modo {entry.mode === "local" ? "direto" : "cluster"}</span>
                      <span>·</span>
                      <span>{entry.hops_count} saltos</span>
                      <span>·</span>
                      <span>{entry.reached ? "alcançado" : "não alcançado"}</span>
                      <span>·</span>
                      <span>IP: {entry.target_ip}</span>
                      {entry.result && (
                        <button
                          type="button"
                          onClick={() => exportNetDiscoveryPDF({ result: entry.result!, mode: entry.mode, generatedAt: entry.created_at })}
                          className="inline-flex items-center gap-1 text-foreground hover:underline"
                        >
                          <FileDown className="w-3 h-3" />
                          Exportar PDF
                        </button>
                      )}
                      {entry.result?.fingerprint?.os_confidence && (
                        <span className="w-full text-[11px] italic">{entry.result.fingerprint.os_confidence}</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })()}

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

        {/* Fase 5/P4 — faixa de status do lote. O painel de resultado abaixo (grafo/tabela/
            fingerprint) sempre reflete o alvo ATUALMENTE ativo (via `hops`/`result`/`running`
            reaproveitados) — esta faixa é só o panorama de todos os alvos, sem re-renderizar o
            resultado completo de cada um. */}
        {batchQueue.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            {batchQueue.map(({ target: t }) => {
              const status = batchStatuses[t] ?? "queued";
              const summary = batchSummaries[t];
              const icon =
                status === "running" ? <Loader2 className="w-3 h-3 animate-spin" /> :
                status === "done" ? <Check className="w-3 h-3 text-emerald-500" /> :
                status === "error" ? <XCircle className="w-3 h-3 text-destructive" /> :
                <span className="w-3 h-3 rounded-full border border-muted-foreground/40 inline-block" />;
              return (
                <span
                  key={t}
                  title={summary ? `${summary.reached ? "Alcançado" : "Não alcançado"}${summary.osGuess ? ` · ${osLabel(summary.osGuess)}` : ""}` : status}
                  className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-mono ${
                    status === "running" ? "border-primary/40 bg-primary/5" :
                    status === "error" ? "border-destructive/40 bg-destructive/5" :
                    status === "done" ? "border-emerald-500/30" : "border-border text-muted-foreground"
                  }`}
                >
                  {icon}
                  {t}
                </span>
              );
            })}
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
              {result.fingerprint.client_cert_used && (
                <Badge variant="outline" className="text-[10px] py-0 border-cyan-500/40 text-cyan-600 dark:text-cyan-400" title="Um certificado de cliente foi apresentado durante o handshake TLS desta descoberta — não confirma sucesso por si só, veja o Certificado TLS abaixo.">
                  <KeyRound className="w-3 h-3 mr-1" />mTLS apresentado
                </Badge>
              )}
            </div>
            {result.fingerprint.os_confidence && (
              <p className="text-xs text-muted-foreground">{result.fingerprint.os_confidence}</p>
            )}
            {result.fingerprint.probed_host && (
              <div className="flex items-start gap-1.5 rounded border border-amber-500/30 bg-amber-500/5 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
                <AlertTriangle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                <span>
                  HTTP/certificado abaixo foram checados usando o hostname{" "}
                  <span className="font-mono">{result.fingerprint.probed_host}</span>
                  {!result.target_resolved && " (descoberto via DNS reverso, não digitado por você)"} —
                  este IP pode responder de forma diferente pra outros hostnames que também apontam pra ele
                  (comum em ingress compartilhado).
                </span>
              </div>
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
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={copyResult}
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                  {copied ? "Copiado" : "Copiar"}
                </button>
                <button
                  type="button"
                  onClick={() => exportNetDiscoveryPDF({ result, mode })}
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  <FileDown className="w-3 h-3" />
                  Exportar PDF
                </button>
              </div>
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
                            className={`text-[10px] py-0 ${
                              h.internal_ref.from_cache
                                ? "border-dashed border-amber-500/50 text-amber-600 dark:text-amber-400"
                                : "border-purple-500/40 text-purple-600 dark:text-purple-400"
                            }`}
                            title={`Cluster: ${h.internal_ref.cluster}${h.internal_ref.namespace ? ` · Namespace: ${h.internal_ref.namespace}` : ""}${h.internal_ref.pod_name ? ` · Pod: ${h.internal_ref.pod_name}` : ""} — ${
                              h.internal_ref.from_cache
                                ? `de um cache de ${formatDistanceToNow(new Date(h.internal_ref.matched_at), { locale: ptBR })} atrás — pode estar desatualizado, o IP pode ter mudado de dono desde então`
                                : "confirmado ao vivo nesta própria busca"
                            }`}
                          >
                            {h.internal_ref.from_cache && <Clock className="w-2.5 h-2.5 mr-1" />}
                            K8s: {h.internal_ref.name}
                          </Badge>
                        )}
                        {h.cloud_match && (
                          <Badge variant="outline" className={`text-[10px] py-0 ${cloudBadgeClass(h.cloud_match)}`}>
                            {h.cloud_match.toUpperCase()}{h.cloud_region ? ` · ${h.cloud_region}` : ""}
                          </Badge>
                        )}
                        {h.asn && (
                          <span className="font-mono text-muted-foreground" title={h.asn_org || ""}>
                            AS{h.asn}{h.asn_org ? ` — ${h.asn_org}` : ""}
                          </span>
                        )}
                        {h.reverse_dns && <span className="text-muted-foreground break-all" title={h.reverse_dns}>{h.reverse_dns}</span>}
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

      {/* Picker reaproveitado da aba Certificados TLS — abre direto em "Backup Apartado"
          (defaultTab="manual"), já que esta aba não edita nenhum Secret K8s real, então a aba
          "Rollback" do picker (escopada por cluster/namespace/secretName) ficaria sempre vazia
          aqui. onSelect popula os dois campos de mTLS de uma vez, como se tivesse colado à mão. */}
      <CertificateSourcePickerModal
        open={certPickerOpen}
        onOpenChange={setCertPickerOpen}
        cluster=""
        namespace=""
        secretName=""
        defaultTab="manual"
        onSelect={(crt, key) => {
          setClientCertPEM(crt);
          setClientKeyPEM(key);
        }}
      />
    </div>
  );
}
