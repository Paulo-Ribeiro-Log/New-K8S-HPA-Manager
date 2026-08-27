import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Loader2, Play, XCircle, AlertTriangle, Route, Copy, Check, History, ChevronDown, ChevronUp, FileDown, FileJson, FileSpreadsheet, StickyNote, ListChecks, KeyRound, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { ptBR } from "date-fns/locale";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import NetDiscoveryGraph from "@/components/NetDiscoveryGraph";
import { CertificateSourcePickerModal } from "@/components/CertificateSourcePickerModal";
import { useClusters } from "@/hooks/useAPI";
import { useCreateNote, GENERAL_NOTES_CLUSTER, GENERAL_NOTES_TAB } from "@/hooks/useNotes";
import { apiClient } from "@/lib/api/client";
import { exportNetDiscoveryPDF } from "@/lib/netDiscoveryPdfExport";
import { exportNetDiscoveryCSV, exportNetDiscoveryJSON, buildNoteMarkdown } from "@/lib/netDiscoveryExport";
import { diffRoutes, formatRouteDiffSummary } from "@/lib/netDiscoveryRouteDiff";
import { aggregateMonitorRounds } from "@/lib/netDiscoveryMonitor";
import { DOCKER_FIX_BY_REASON } from "@/lib/dockerFixSnippets";
import type { NetDiscoveryHistoryEntry, NetDiscoveryHop, NetDiscoveryResult, NetDiscoverySSEEvent } from "@/lib/api/types";

type NetDiscoveryMode = "pod" | "local";

// netDiscoveryBatchConcurrency — Fase G (roadmap de maturidade profissional): quantos alvos rodam
// ao mesmo tempo quando o usuário liga "Paralelizar" no modo lote. Fixo em 3 (não configurável na
// UI) — mesmo teto máximo já validado no backend (netDiscoveryBatchConcurrencyMax) — escopo
// reduzido confirmado com o usuário: um único checkbox liga/desliga, sem campo numérico extra.
const netDiscoveryBatchConcurrency = 3;

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

// formatLatencyCell — Fase A (múltiplas sondas por salto): mostra a faixa min–avg–max quando há
// mais de 1 amostra recebida com valores diferentes (jitter real observável), mantendo o formato
// simples "X.X ms" de antes desta fase quando só há 1 amostra (ou min==max, sem jitter pra mostrar).
function formatLatencyCell(h: NetDiscoveryHop): string {
  if (h.rtt_ms == null) return "—";
  if (h.rtt_min_ms != null && h.rtt_max_ms != null && h.rtt_max_ms > h.rtt_min_ms) {
    return `${h.rtt_min_ms.toFixed(1)}–${h.rtt_ms.toFixed(1)}–${h.rtt_max_ms.toFixed(1)} ms`;
  }
  return `${h.rtt_ms.toFixed(1)} ms`;
}

// lossBadgeClass — vermelho pra perda total (100%, hop nunca respondeu — já coberto por
// timed_out, mas ainda vale sinalizar), âmbar pra perda parcial (instabilidade real: hop respondeu
// mas nem sempre), sem badge nenhum quando não há perda (0% é o caso comum, não vale poluir a UI).
function lossBadgeClass(lossPct: number): string {
  if (lossPct >= 100) return "border-destructive/50 text-destructive";
  return "border-amber-500/50 text-amber-600 dark:text-amber-400";
}

// "Descoberta de Rede" — Fase 1 (IP-ROUTE-DISCOVERY-PLAN.md): traceroute básico + grafo ao vivo,
// sem nenhuma camada de enriquecimento ainda (DNS reverso/ASN/nuvem/cross-reference K8s/
// fingerprint de SO chegam nas Fases 2-4). Mesmo padrão dual pod/local já usado pelo Teste de
// Kafka/Banco de Dados/Latência — nunca um mecanismo novo.
export default function NetDiscoveryTab() {
  const { clusters } = useClusters();
  const queryClient = useQueryClient();
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
  // probeCount — Fase A (roadmap de maturidade profissional): quantas sondas TCP por salto, string
  // vazia = default do backend (3), máximo 3. Com 1 sonda só (valor original desta feature) não dá
  // pra distinguir "hop lento" de "hop com perda intermitente de pacote" — mais de 1 amostra
  // permite calcular perda% e faixa de latência (min/max) por salto, não só uma média solta.
  const [probeCount, setProbeCount] = useState("");
  // extraPorts — Fase D (roadmap de maturidade profissional): portas extras (separadas por
  // vírgula) pra verificar no fingerprint do destino, além das ~18 portas curadas checadas por
  // padrão — útil pra troubleshooting de uma aplicação específica (ex: 8081, 9000).
  const [extraPorts, setExtraPorts] = useState("");
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
  // Fase C (roadmap de maturidade profissional) — modo "monitor": 3ª opção além de single/batch,
  // reaproveitando 100% o MESMO mecanismo de lote (runBatch/RunBatch) com allow_duplicate_targets
  // (o mesmo alvo repetido N vezes) — nunca um endpoint novo, "monitorar" é literalmente "lote com
  // o mesmo alvo repetido".
  const [inputMode, setInputMode] = useState<"single" | "batch" | "monitor">("single");
  const [batchTargetsText, setBatchTargetsText] = useState("");
  // monitorRounds/monitorIntervalSec — só usados quando inputMode==="monitor".
  const [monitorRounds, setMonitorRounds] = useState("3");
  const [monitorIntervalSec, setMonitorIntervalSec] = useState("0");
  const [batchQueue, setBatchQueue] = useState<{ target: string; sessionId: string }[]>([]);
  const batchQueueRef = useRef<{ target: string; sessionId: string }[]>([]);
  const [batchId, setBatchId] = useState<string | null>(null);
  // batchStatuses/batchSummaries — Achado real: no modo monitor todos os itens da fila têm o MESMO
  // `target` (alvo repetido N vezes) — chavear por target (como o lote normal fazia) colapsaria
  // todas as rodadas numa única entrada. Chaveado por `sessionId` (sempre único, mesmo no modo
  // monitor) desde esta fase — generaliza corretamente os dois modos sem duas implementações.
  const [batchStatuses, setBatchStatuses] = useState<Record<string, "queued" | "running" | "done" | "error">>({});
  const [batchSummaries, setBatchSummaries] = useState<Record<string, { reached: boolean; osGuess?: string }>>({});
  // isMonitorRun/batchRoundResults — só populados no modo monitor: batchRoundResults acumula o
  // NetDiscoveryResult COMPLETO de cada rodada, na ordem de conclusão (== ordem das rodadas, já que
  // a fila é estritamente sequencial) — necessário pra agregação (aggregateMonitorRounds), que
  // precisa da rota/loss% inteiros de cada rodada, não só o resumo reached/osGuess já guardado em
  // batchSummaries.
  const [isMonitorRun, setIsMonitorRun] = useState(false);
  const [batchRoundResults, setBatchRoundResults] = useState<NetDiscoveryResult[]>([]);

  // Fase G (roadmap de maturidade profissional) — paralelismo opcional no modo lote, escopo
  // reduzido confirmado com o usuário: até 3 alvos rodando ao mesmo tempo, SEM grafo/tabela ao
  // vivo por item (só a faixa de status compacta já existente) — a UI de "um alvo ativo" (`hops`/
  // `result`/`progress`) nunca é usada no caminho paralelo. `parallelStreamsRef` gerencia N
  // EventSource simultâneas (uma por alvo), fora do mecanismo de encadeamento sequencial
  // (`batchQueueRef`/`useEffect([sessionId])`) usado por single/batch-sequencial/monitor.
  const [parallelBatch, setParallelBatch] = useState(false);
  const parallelStreamsRef = useRef<Map<string, EventSource>>(new Map());
  const [parallelResults, setParallelResults] = useState<Record<string, NetDiscoveryResult>>({});
  const [expandedParallelItem, setExpandedParallelItem] = useState<string | null>(null);

  // Fecha qualquer stream paralela remanescente se o componente desmontar no meio de um lote
  // paralelo (troca de aba — ver achado colateral documentado no CLAUDE.md desta feature).
  useEffect(() => {
    return () => {
      parallelStreamsRef.current.forEach((es) => es.close());
    };
  }, []);

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

  // Fase B (roadmap de maturidade profissional) — diff de rota entre execuções: baselineHopsRef
  // captura o último estado CONHECIDO (historyEntries[0], antes desta execução) no instante em que
  // `run()` dispara — não lido direto de `historyEntries` no render, porque a query de histórico
  // é invalidada assim que a execução termina (ver "complete" no efeito de SSE abaixo, pra manter
  // o banner "Última busca" e o histórico expandido atualizados) e, sem esse snapshot capturado
  // antecipadamente, uma 2ª execução consecutiva do mesmo alvo compararia contra si mesma. Só
  // relevante no modo single (batch/monitor têm seus próprios alvos/mecanismos de agregação) —
  // zerado em runBatch() pra nunca mostrar um diff equivocado durante lote/monitoramento.
  const baselineHopsRef = useRef<NetDiscoveryHop[] | null>(null);
  const liveRouteDiff = result && baselineHopsRef.current ? diffRoutes(result.hops, baselineHopsRef.current) : null;

  // Fase E — "Salvar como Nota" (roadmap de maturidade profissional): reaproveita a feature de
  // Notas já existente nesta app. Escopo: modo pod com cluster selecionado usa esse cluster + a
  // aba "net-discovery" (mesmo `activeTab` desta ferramenta no ToolsMenu, convenção "escopado por
  // cluster+aba" já usada pelas demais Notas); modo local (sem cluster real associado à
  // investigação) usa os "Lembretes gerais" (GENERAL_NOTES_CLUSTER/GENERAL_NOTES_TAB, já
  // exportados por useNotes.ts) — a investigação não pertence a nenhum cluster específico nesse
  // caso. useCreateNote(cluster, tab) fecha sobre os argumentos recebidos NESTE render, então
  // recalcular os dois a cada render mantém o destino sempre correto conforme o usuário alterna
  // modo/cluster no formulário.
  const noteScopeCluster = mode === "pod" && cluster ? cluster : GENERAL_NOTES_CLUSTER;
  const noteScopeTab = mode === "pod" && cluster ? "net-discovery" : GENERAL_NOTES_TAB;
  const createNote = useCreateNote(noteScopeCluster, noteScopeTab);

  const saveResultAsNote = () => {
    if (!result) return;
    createNote.mutate(buildNoteMarkdown(result, mode), {
      onSuccess: () => toast.success("Descoberta salva como Nota"),
      onError: (err) => toast.error(err instanceof Error ? err.message : "Falha ao salvar a nota"),
    });
  };

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

  const probeCountNum = probeCount.trim() ? Number(probeCount.trim()) : undefined;
  const probeCountValid = probeCountNum === undefined || (Number.isInteger(probeCountNum) && probeCountNum >= 1 && probeCountNum <= 3);

  // extraPorts — parse de "8081, 9000" pra [8081, 9000], tolerante a espaços/vírgulas extras.
  // Inválido (não-número, fora de 1-65535, ou mais de 10) desabilita o botão de rodar, mesmo
  // padrão dos demais campos de sonda desta aba — validação espelhada da do backend
  // (validateExtraPorts) só pra evitar um round-trip com algo trivial de checar no cliente.
  const extraPortsRaw = extraPorts.split(",").map((p) => p.trim()).filter(Boolean);
  const extraPortsNums = extraPortsRaw.map((p) => Number(p));
  const extraPortsValid =
    extraPortsRaw.length <= 10 &&
    extraPortsNums.every((n) => Number.isInteger(n) && n >= 1 && n <= 65535);

  const batchTargetsParsed = batchTargetsText.split("\n").map((t) => t.trim()).filter(Boolean);
  const batchTargetsValid = batchTargetsParsed.length > 0 && batchTargetsParsed.length <= 10;

  // monitorRounds/monitorIntervalSec — Fase C. Rounds: 2-10 (mesmo teto de alvos por lote,
  // netDiscoveryBatchMaxTargets no backend — reaproveitado, não duplicado). Interval: 0-60s
  // (netDiscoveryMonitorIntervalMaxSec no backend).
  const monitorRoundsNum = Number(monitorRounds.trim());
  const monitorRoundsValid = Number.isInteger(monitorRoundsNum) && monitorRoundsNum >= 2 && monitorRoundsNum <= 10;
  const monitorIntervalNum = monitorIntervalSec.trim() ? Number(monitorIntervalSec.trim()) : 0;
  const monitorIntervalValid = Number.isInteger(monitorIntervalNum) && monitorIntervalNum >= 0 && monitorIntervalNum <= 60;

  // mTLS: os dois campos precisam vir juntos, ou nenhum dos dois — mesma checagem que o backend
  // já faz (INVALID_CLIENT_CERT), espelhada aqui pra não gastar um round-trip com algo trivial de
  // checar no cliente.
  const mtlsPairValid = (clientCertPEM.trim() !== "") === (clientKeyPEM.trim() !== "");

  const canRun =
    !running &&
    probePortValid &&
    probeTimeoutSecValid &&
    probeCountValid &&
    extraPortsValid &&
    mtlsPairValid &&
    (mode === "local" ? dockerReady : !!cluster && !!namespace) &&
    (inputMode === "single"
      ? !!target.trim()
      : inputMode === "monitor"
      ? !!target.trim() && monitorRoundsValid && monitorIntervalValid
      : batchTargetsValid);

  const run = async () => {
    setHops([]);
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando...");
    setRunning(true);
    // Fase B — captura o baseline ANTES de disparar a requisição (ver comentário completo em
    // baselineHopsRef acima).
    baselineHopsRef.current = historyEntries[0]?.result?.hops ?? null;
    // Achado real de code review: run() (modo single) não limpava estado deixado por um LOTE
    // anterior — a faixa de status do lote ficava vazando visualmente pra uma busca única
    // seguinte, e um batchId desatualizado (já apagado no servidor) podia fazer cancel() mandar
    // o ID errado, deixando a busca única real sem ser cancelada de verdade.
    setBatchId(null);
    setBatchQueue([]);
    batchQueueRef.current = [];
    setBatchStatuses({});
    setBatchSummaries({});
    setIsMonitorRun(false);
    setBatchRoundResults([]);
    // Fase G — fecha qualquer stream paralela remanescente de um lote anterior.
    parallelStreamsRef.current.forEach((es) => es.close());
    parallelStreamsRef.current = new Map();
    setParallelResults({});
    setExpandedParallelItem(null);
    try {
      const { session_id } = await apiClient.runNetDiscovery({
        target: target.trim(),
        mode,
        cluster: mode === "pod" ? cluster : undefined,
        namespace: mode === "pod" ? namespace : undefined,
        probe_port: probePortNum,
        probe_timeout_sec: probeTimeoutSecNum,
        probe_count: probeCountNum,
        extra_ports: extraPortsNums.length > 0 ? extraPortsNums : undefined,
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
    // Fase B — o diff de rota ao vivo (baselineHopsRef) só faz sentido no modo single, onde
    // `debouncedTarget`/historyEntries seguem o campo de alvo único; zerado aqui pra nunca mostrar
    // um diff equivocado durante lote/monitoramento (que têm seus próprios alvos/mecanismos).
    baselineHopsRef.current = null;
    setBatchStatuses({});
    setBatchSummaries({});
    setBatchQueue([]);
    batchQueueRef.current = [];
    setBatchId(null);
    setIsMonitorRun(false);
    setBatchRoundResults([]);
    parallelStreamsRef.current.forEach((es) => es.close());
    parallelStreamsRef.current = new Map();
    setParallelResults({});
    setExpandedParallelItem(null);
    try {
      const resp = await apiClient.runNetDiscoveryBatch({
        targets: batchTargetsParsed,
        mode,
        cluster: mode === "pod" ? cluster : undefined,
        namespace: mode === "pod" ? namespace : undefined,
        probe_port: probePortNum,
        probe_timeout_sec: probeTimeoutSecNum,
        probe_count: probeCountNum,
        extra_ports: extraPortsNums.length > 0 ? extraPortsNums : undefined,
        client_cert_pem: mtlsConfigured ? clientCertPEM : undefined,
        client_key_pem: mtlsConfigured ? clientKeyPEM : undefined,
        concurrency: parallelBatch ? netDiscoveryBatchConcurrency : undefined,
      });
      const queue = resp.targets.map((t, i) => ({ target: t, sessionId: resp.session_ids[i] }));
      batchQueueRef.current = queue;
      setBatchQueue(queue);
      setBatchId(resp.batch_id);
      // Chaveado por sessionId (único mesmo se dois alvos repetissem o mesmo texto — não deveria
      // acontecer no lote normal, que dedupe, mas mantém o mesmo critério do modo monitor abaixo).
      const initialStatuses: Record<string, "queued" | "running"> = {};
      queue.forEach((q, i) => { initialStatuses[q.sessionId] = i === 0 ? "running" : "queued"; });
      setBatchStatuses(initialStatuses);
      if (parallelBatch) {
        // Fase G — caminho paralelo: abre TODAS as streams de uma vez (o backend já as executa
        // concorrentemente), sem passar por `sessionId`/o encadeamento sequencial existente. Nunca
        // popula `hops`/`result` (sem grafo/tabela ao vivo por item, escopo reduzido confirmado).
        setPhaseMessage(`Executando ${queue.length} alvos em paralelo...`);
        startParallelStreams(queue);
      } else {
        setSessionId(queue[0].sessionId);
      }
    } catch (err) {
      setRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o lote");
    }
  };

  // startParallelStreams — Fase G: abre uma EventSource por alvo (todas de uma vez, já que o
  // backend já os executa concorrentemente — não há "vez" de cada um pra esperar). Cada stream só
  // atualiza status/resultado daquele item específico (`batchStatuses`/`parallelResults`), nunca o
  // estado "alvo ativo único" (`hops`/`result`/`progress`) usado pelos outros modos — por isso não
  // há grafo/tabela ao vivo por item neste modo (escopo reduzido confirmado com o usuário).
  const startParallelStreams = (queue: { target: string; sessionId: string }[]) => {
    let pending = queue.length;
    let completed = 0;
    const total = queue.length;
    const finishOne = () => {
      pending -= 1;
      completed += 1;
      // Barra de progresso reaproveitada (mesmo elemento visual do modo sequencial) — aqui reflete
      // % de alvos concluídos (com ou sem erro), já que não há um "salto atual" único pra basear o
      // progresso nesse modo.
      setProgress(completed / total);
      if (pending <= 0) {
        setRunning(false);
        setBatchId(null);
      }
    };
    queue.forEach((q) => {
      const es = new EventSource(apiClient.getNetDiscoveryStreamURL(q.sessionId));
      parallelStreamsRef.current.set(q.sessionId, es);
      let terminalReceived = false;
      es.onmessage = (e) => {
        try {
          const event: NetDiscoverySSEEvent = JSON.parse(e.data);
          if (event.type === "complete" && event.result) {
            terminalReceived = true;
            const finalResult = event.result as NetDiscoveryResult;
            setBatchStatuses((prev) => ({ ...prev, [q.sessionId]: "done" }));
            setBatchSummaries((prev) => ({
              ...prev,
              [q.sessionId]: { reached: finalResult.reached, osGuess: finalResult.fingerprint?.os_guess },
            }));
            setParallelResults((prev) => ({ ...prev, [q.sessionId]: finalResult }));
            es.close();
            parallelStreamsRef.current.delete(q.sessionId);
            finishOne();
          }
          if (event.type === "error") {
            terminalReceived = true;
            setBatchStatuses((prev) => ({ ...prev, [q.sessionId]: "error" }));
            es.close();
            parallelStreamsRef.current.delete(q.sessionId);
            finishOne();
          }
        } catch {
          /* ignore evento malformado */
        }
      };
      // Mesmo achado real já documentado no useEffect single-stream (es.onerror mais abaixo): o
      // encerramento normal da conexão após "complete"/"error" comumente dispara onerror também —
      // só reage de verdade (conta como pendência resolvida) se a conexão caiu ANTES de qualquer
      // evento terminal, senão o `finishOne()` já rodou dentro de onmessage.
      es.onerror = () => {
        es.close();
        if (parallelStreamsRef.current.has(q.sessionId)) {
          parallelStreamsRef.current.delete(q.sessionId);
        }
        if (!terminalReceived) {
          setBatchStatuses((prev) => ({ ...prev, [q.sessionId]: "error" }));
          finishOne();
        }
      };
    });
  };

  // runMonitor — Fase C (roadmap de maturidade profissional): reaproveita o MESMO endpoint de lote
  // (apiClient.runNetDiscoveryBatch) com allow_duplicate_targets=true e um único alvo repetido
  // `monitorRoundsNum` vezes — "monitorar" é literalmente "lote com o mesmo alvo repetido N vezes"
  // (mesma decisão de design do próprio lote: reaproveitar runDiscovery/RunBatch sem modificar
  // nada, zero risco de lógica nova no backend). O encadeamento SSE (batchQueueRef, useEffect
  // abaixo) já funciona sem nenhuma mudança — só a agregação final (MonitorSummaryPanel) é nova.
  const runMonitor = async () => {
    setHops([]);
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando monitoramento...");
    setRunning(true);
    baselineHopsRef.current = null;
    setBatchStatuses({});
    setBatchSummaries({});
    setBatchQueue([]);
    batchQueueRef.current = [];
    setBatchId(null);
    setIsMonitorRun(true);
    setBatchRoundResults([]);
    // Fase G — monitoramento nunca é paralelo (ver validateConcurrencyWithMonitor no backend), mas
    // ainda limpa qualquer stream paralela remanescente de um lote anterior.
    parallelStreamsRef.current.forEach((es) => es.close());
    parallelStreamsRef.current = new Map();
    setParallelResults({});
    setExpandedParallelItem(null);
    try {
      const trimmedTarget = target.trim();
      const resp = await apiClient.runNetDiscoveryBatch({
        targets: Array(monitorRoundsNum).fill(trimmedTarget),
        mode,
        cluster: mode === "pod" ? cluster : undefined,
        namespace: mode === "pod" ? namespace : undefined,
        probe_port: probePortNum,
        probe_timeout_sec: probeTimeoutSecNum,
        probe_count: probeCountNum,
        extra_ports: extraPortsNums.length > 0 ? extraPortsNums : undefined,
        client_cert_pem: mtlsConfigured ? clientCertPEM : undefined,
        client_key_pem: mtlsConfigured ? clientKeyPEM : undefined,
        allow_duplicate_targets: true,
        interval_sec: monitorIntervalNum,
      });
      const queue = resp.targets.map((t, i) => ({ target: t, sessionId: resp.session_ids[i] }));
      batchQueueRef.current = queue;
      setBatchQueue(queue);
      setBatchId(resp.batch_id);
      const initialStatuses: Record<string, "queued" | "running"> = {};
      queue.forEach((q, i) => { initialStatuses[q.sessionId] = i === 0 ? "running" : "queued"; });
      setBatchStatuses(initialStatuses);
      setSessionId(queue[0].sessionId);
    } catch (err) {
      setRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o monitoramento");
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
    // Fase G — fecha todas as streams paralelas em andamento, se houver (cancelTarget=batchId já
    // derrubou o contexto compartilhado no backend; aqui só limpa as conexões abertas no cliente).
    parallelStreamsRef.current.forEach((es) => es.close());
    parallelStreamsRef.current = new Map();
    setRunning(false);
    setBatchId(null);
    setPhaseMessage("Descoberta cancelada.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getNetDiscoveryStreamURL(sessionId));
    esRef.current = es;

    // Achado real de code review — contexto completo no comentário de es.onerror mais abaixo:
    // marcada true assim que este onmessage processa um evento terminal ("complete"/"error") pra
    // ESTA sessão, seja encadeando pro próximo alvo do lote ou encerrando de vez. Variável local
    // (não ref/state) — cada execução deste efeito (uma por sessionId) tem a sua própria, correta
    // por construção.
    let terminalReceived = false;

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
          terminalReceived = true;
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
            // Chaveado por sessionId (ver comentário em batchStatuses/batchSummaries acima) — no
            // modo monitor, `target` se repete em TODA rodada, então chavear por ele colapsaria
            // todas as rodadas numa única entrada.
            setBatchStatuses((prev) => ({ ...prev, [sessionId]: "done" }));
            setBatchSummaries((prev) => ({
              ...prev,
              [sessionId]: { reached: finalResult.reached, osGuess: finalResult.fingerprint?.os_guess },
            }));
            // Fase C — acumula o resultado COMPLETO desta rodada, na ordem de conclusão (== ordem
            // das rodadas, fila estritamente sequencial) — usado só pela agregação do modo
            // monitor (aggregateMonitorRounds), sem custo pro lote normal (só mais um array).
            setBatchRoundResults((prev) => [...prev, finalResult]);
            if (idx + 1 < queue.length) {
              const next = queue[idx + 1];
              setBatchStatuses((prev) => ({ ...prev, [next.sessionId]: "running" }));
              setHops([]);
              setResult(null);
              setProgress(0);
              setPhaseMessage(`Iniciando ${next.target}...`);
              setSessionId(next.sessionId);
              return; // lote continua — não encerra running ainda
            }
          }
          setRunning(false);
          // Achado real de code review: batchId nunca era zerado quando o lote terminava
          // NATURALMENTE (só em runBatch() ao iniciar um novo, e em cancel()) — um cancel()
          // subsequente (numa busca única iniciada depois, se run() não limpasse — ver acima)
          // podia mandar esse ID morto pro backend em vez do sessionId real.
          setBatchId(null);
          // Fase B — só invalida (refresca) o Histórico quando esta foi uma execução single-mode
          // de verdade (idx === -1, nunca fez parte de uma fila de lote/monitor) — garante que o
          // banner "Última busca"/histórico expandido reflitam esta execução recém-concluída sem
          // precisar trocar de aba/recarregar, e sem invalidar uma query de alvo errado durante
          // lote (onde `target`/debouncedTarget não correspondem ao alvo que de fato rodou).
          if (idx === -1) {
            queryClient.invalidateQueries({ queryKey: ["net-discovery-history", debouncedTarget] });
          }
        }
        if (event.type === "error") {
          terminalReceived = true;
          // Mesmo encadeamento do "complete" acima — um alvo com erro no meio do lote não para
          // tudo, só marca esse alvo como erro na faixa de status e segue pro próximo.
          const queue = batchQueueRef.current;
          const idx = queue.findIndex((q) => q.sessionId === sessionId);
          if (idx !== -1) {
            setBatchStatuses((prev) => ({ ...prev, [sessionId]: "error" }));
            if (idx + 1 < queue.length) {
              const next = queue[idx + 1];
              setBatchStatuses((prev) => ({ ...prev, [next.sessionId]: "running" }));
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
          setBatchId(null); // mesmo motivo do "complete" acima — lote terminou (com erro) naturalmente
        }
      } catch {
        /* ignore evento malformado */
      }
    };

    // Achado real de code review: es.onerror sempre chamava setRunning(false) incondicionalmente
    // — mas quando o servidor termina a resposta HTTP após um "complete"/"error" (Stream() retorna
    // em net_discovery.go), o browser comumente dispara EventSource.onerror por causa disso (o
    // encerramento normal da conexão, não uma falha de rede real), às vezes ANTES/JUNTO do cleanup
    // do efeito React que fecharia este `es` de propósito. Em modo lote, o handler onmessage acima
    // já tinha avançado sessionId pro PRÓXIMO alvo (setSessionId(next.sessionId), sem tocar
    // running) — esse onerror tardio do stream ANTIGO então forçava running=false no meio do
    // lote, fazendo a UI parecer "ociosa/concluída" enquanto o backend (ainda segurando o lock
    // runningUsers) seguia processando os alvos restantes; qualquer nova busca iniciada pelo
    // usuário nesse meio-tempo batia em 409 DISCOVERY_ALREADY_RUNNING sem explicação visível.
    // A flag `terminalReceived` (declarada acima, setada dentro de onmessage) resolve o
    // encadeamento: se onerror disparar DEPOIS de um evento terminal já processado, é só o
    // fechamento normal da conexão — ignora (o estado já foi decidido pelo onmessage,
    // running/batchId já estão corretos, seja encadeado pro próximo alvo ou finalizado). Só reage
    // de verdade (encerra) quando a conexão cai ANTES de qualquer evento terminal ter chegado —
    // aí sim é uma falha de rede genuína.
    es.onerror = () => {
      es.close();
      if (terminalReceived) return; // conexão fechou depois de já sabermos o resultado — normal
      setRunning(false);
      setBatchId(null);
    };

    return () => {
      es.close();
      esRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const copyResult = () => {
    if (!result) return;
    const lines = result.hops.map((h) => {
      if (h.timed_out) return `${h.index}  * * *`;
      const lossSuffix = h.loss_pct ? `  (perda ${h.loss_pct.toFixed(0)}%)` : "";
      return `${h.index}  ${h.ip}  ${formatLatencyCell(h)}${lossSuffix}`;
    });
    navigator.clipboard.writeText(
      `Rota até ${result.target_input} (${result.target_ip}):\n${lines.join("\n")}`
    );
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-col gap-3">
        {/* Fase 5/P4 (single/batch) + Fase C (monitor) — toggle de 3 modos. Trocar de modo com uma
            descoberta em andamento não é permitido (disabled={running}) — evita perder o
            encadeamento de sessões a meio caminho. */}
        <div className="flex items-center gap-1.5">
          <ListChecks className="w-3.5 h-3.5 text-muted-foreground" />
          <div className="flex rounded-md border border-border overflow-hidden">
            {(["single", "batch", "monitor"] as const).map((m) => (
              <button
                key={m}
                type="button"
                disabled={running}
                onClick={() => setInputMode(m)}
                className={`text-xs px-2.5 py-1 ${inputMode === m ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"} disabled:opacity-50`}
              >
                {m === "single" ? "Alvo único" : m === "batch" ? "Lote (até 10)" : "Monitorar (repetir)"}
              </button>
            ))}
          </div>
        </div>

        <div className="flex flex-wrap items-end gap-3">
          {inputMode === "batch" ? (
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
          ) : (
            <div className="min-w-[280px] flex-1">
              <label className="text-xs text-muted-foreground block mb-1">IP ou hostname</label>
              <Input
                placeholder="ex: 8.8.8.8 ou servidor.dominio.com"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                disabled={running}
                onKeyDown={(e) => {
                  if (e.key !== "Enter" || !canRun) return;
                  if (inputMode === "monitor") runMonitor(); else run();
                }}
              />
            </div>
          )}

          {/* Fase G — paralelismo opcional, só no modo lote (não no monitor — série temporal, ver
              validateConcurrencyWithMonitor no backend). Escopo reduzido confirmado com o usuário:
              sem grafo/tabela ao vivo por item durante o paralelismo, só a faixa de status. */}
          {inputMode === "batch" && (
            <div className="flex items-center gap-2 pb-2">
              <Checkbox
                checked={parallelBatch}
                onCheckedChange={(v) => setParallelBatch(!!v)}
                disabled={running}
                id="net-discovery-parallel"
              />
              <label htmlFor="net-discovery-parallel" className="text-xs text-muted-foreground cursor-pointer max-w-[220px]">
                Paralelizar (até {netDiscoveryBatchConcurrency} simultâneos, sem grafo ao vivo por item)
              </label>
            </div>
          )}

          {/* Fase C — Rodadas/Intervalo, só no modo monitor. */}
          {inputMode === "monitor" && (
            <>
              <div className="w-24">
                <label className="text-xs text-muted-foreground block mb-1">Rodadas</label>
                <Input
                  type="number"
                  min={2}
                  max={10}
                  value={monitorRounds}
                  onChange={(e) => setMonitorRounds(e.target.value)}
                  disabled={running}
                  className={!monitorRoundsValid ? "border-destructive" : undefined}
                />
              </div>
              <div className="w-32">
                <label className="text-xs text-muted-foreground block mb-1">Intervalo (s)</label>
                <Input
                  type="number"
                  min={0}
                  max={60}
                  placeholder="0"
                  value={monitorIntervalSec}
                  onChange={(e) => setMonitorIntervalSec(e.target.value)}
                  disabled={running}
                  className={!monitorIntervalValid ? "border-destructive" : undefined}
                />
              </div>
            </>
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

          <div className="w-28">
            <label className="text-xs text-muted-foreground block mb-1">Sondas/salto</label>
            <Input
              type="number"
              min={1}
              max={3}
              placeholder="3"
              value={probeCount}
              onChange={(e) => setProbeCount(e.target.value)}
              disabled={running}
              className={!probeCountValid ? "border-destructive" : undefined}
              onKeyDown={(e) => { if (e.key === "Enter" && canRun) run(); }}
            />
          </div>

          {!running ? (
            <ProtectedAction>
              <Button
                onClick={inputMode === "single" ? run : inputMode === "monitor" ? runMonitor : runBatch}
                disabled={!canRun}
              >
                <Play className="w-4 h-4 mr-1.5" />
                {inputMode === "single" ? "Traçar rota" : inputMode === "monitor" ? "Iniciar Monitoramento" : "Iniciar Lote"}
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
            Padrão 443 (web), timeout 2s/salto, 3 sondas/salto (perda%/latência por amostragem). Alvo sem resposta? Tente:
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

        {/* Portas extras do fingerprint — Fase D (roadmap de maturidade profissional): além das
            ~18 portas curadas checadas por padrão, deixa o usuário pedir portas específicas de uma
            aplicação sob investigação (ex: 8081, 9000) sem precisar editar código. */}
        <div className="flex items-center gap-2 -mt-1">
          <label className="text-[10px] text-muted-foreground whitespace-nowrap">Portas extras do fingerprint:</label>
          <Input
            placeholder="ex: 8081, 9000"
            value={extraPorts}
            onChange={(e) => setExtraPorts(e.target.value)}
            disabled={running}
            className={`h-6 text-[10px] max-w-[220px] ${!extraPortsValid ? "border-destructive" : ""}`}
          />
          {!extraPortsValid && (
            <span className="text-[10px] text-destructive">até 10 portas, cada uma entre 1 e 65535</span>
          )}
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
                  {historyEntries.map((entry, i) => {
                    // Fase B — compara com a entrada SEGUINTE do array (mais antiga, já que
                    // historyEntries vem ordenado mais-recente-primeiro) pra marcar "rota mudou"
                    // entre uma execução e a imediatamente anterior a ela.
                    const olderEntry = historyEntries[i + 1];
                    const entryDiff = entry.result && olderEntry?.result
                      ? diffRoutes(entry.result.hops, olderEntry.result.hops)
                      : null;
                    return (
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
                      {entryDiff?.changed && (
                        <Badge
                          variant="outline"
                          className="text-[10px] py-0 border-amber-500/40 text-amber-600 dark:text-amber-400"
                          title={formatRouteDiffSummary(entryDiff)}
                        >
                          rota mudou
                        </Badge>
                      )}
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
                    );
                  })}
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
            {batchQueue.map(({ target: t, sessionId: qsid }, i) => {
              // Chaveado por sessionId, não por target — no modo monitor, `t` se repete em TODA
              // rodada (ver comentário completo em batchStatuses/batchSummaries acima).
              const status = batchStatuses[qsid] ?? "queued";
              const summary = batchSummaries[qsid];
              const icon =
                status === "running" ? <Loader2 className="w-3 h-3 animate-spin" /> :
                status === "done" ? <Check className="w-3 h-3 text-emerald-500" /> :
                status === "error" ? <XCircle className="w-3 h-3 text-destructive" /> :
                <span className="w-3 h-3 rounded-full border border-muted-foreground/40 inline-block" />;
              // Fase G — no modo paralelo, um item com resultado já disponível pode ser expandido
              // ("ver detalhes") — é o único jeito de inspecionar a rota daquele alvo, já que não
              // há grafo/tabela ao vivo por item nesse modo (escopo reduzido confirmado).
              const expandable = parallelBatch && !!parallelResults[qsid];
              return (
                <span
                  key={qsid}
                  onClick={expandable ? () => setExpandedParallelItem((prev) => (prev === qsid ? null : qsid)) : undefined}
                  title={
                    expandable
                      ? "Clique para ver a rota completa"
                      : summary
                      ? `${summary.reached ? "Alcançado" : "Não alcançado"}${summary.osGuess ? ` · ${osLabel(summary.osGuess)}` : ""}`
                      : status
                  }
                  className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-mono ${expandable ? "cursor-pointer hover:border-foreground/40" : ""} ${
                    status === "running" ? "border-primary/40 bg-primary/5" :
                    status === "error" ? "border-destructive/40 bg-destructive/5" :
                    status === "done" ? (expandedParallelItem === qsid ? "border-emerald-500/60 bg-emerald-500/10" : "border-emerald-500/30") : "border-border text-muted-foreground"
                  }`}
                >
                  {icon}
                  {t}
                  {/* Modo monitor: mesmo alvo em toda rodada — número da rodada distingue as bolhas */}
                  {isMonitorRun && <span className="text-muted-foreground">#{i + 1}</span>}
                </span>
              );
            })}
          </div>
        )}

        {/* Fase G — tabela de saltos do item paralelo expandido (ver expandable acima). Reaproveita
            o mesmo formato compacto de linha da tabela principal, sem fingerprint/grafo. */}
        {parallelBatch && expandedParallelItem && parallelResults[expandedParallelItem] && (
          <ParallelItemResultTable result={parallelResults[expandedParallelItem]} />
        )}

        {/* Fase C — resumo de agregação do monitoramento, só quando TODAS as rodadas concluíram
            (batchRoundResults.length === batchQueue.length garante isso — nunca aparece parcial,
            inclusive se o usuário cancelar no meio). */}
        {isMonitorRun && batchQueue.length > 0 && batchRoundResults.length === batchQueue.length && (
          <MonitorSummaryPanel results={batchRoundResults} />
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

        {/* Fase B — diff de rota: sinaliza quando a rota atual diverge da última execução
            registrada ANTES desta busca (ver baselineHopsRef) — indício de reroteamento/failover/
            mudança de infraestrutura desde a última vez que este alvo foi investigado. */}
        {result && liveRouteDiff?.changed && (
          <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400">
            <History className="w-4 h-4 mt-0.5 shrink-0" />
            <span>Rota mudou em relação à última busca registrada — {formatRouteDiffSummary(liveRouteDiff)}.</span>
          </div>
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
                {/* Fase E (roadmap de maturidade profissional) — CSV/JSON pra levar o dado bruto
                    pra outra ferramenta, e "Salvar como Nota" reaproveitando a feature de Notas
                    já existente (ver noteScopeCluster/noteScopeTab acima). */}
                <button
                  type="button"
                  onClick={() => exportNetDiscoveryCSV(result)}
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  <FileSpreadsheet className="w-3 h-3" />
                  CSV
                </button>
                <button
                  type="button"
                  onClick={() => exportNetDiscoveryJSON(result)}
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  <FileJson className="w-3 h-3" />
                  JSON
                </button>
                <button
                  type="button"
                  onClick={saveResultAsNote}
                  disabled={createNote.isPending}
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
                >
                  <StickyNote className="w-3 h-3" />
                  {createNote.isPending ? "Salvando..." : "Salvar como Nota"}
                </button>
              </div>
            </div>
            <table className="w-full text-xs">
              <thead>
                <tr className="text-muted-foreground">
                  <th className="text-left font-medium px-2.5 py-1.5 w-12">#</th>
                  <th className="text-left font-medium px-2.5 py-1.5">IP</th>
                  <th className="text-left font-medium px-2.5 py-1.5">Latência</th>
                  <th className="text-left font-medium px-2.5 py-1.5">Perda</th>
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
                    <td className="px-2.5 py-1.5 font-mono">{formatLatencyCell(h)}</td>
                    <td className="px-2.5 py-1.5 font-mono">
                      {h.loss_pct ? (
                        <Badge
                          variant="outline"
                          className={`text-[10px] py-0 ${lossBadgeClass(h.loss_pct)}`}
                          title={h.probes_sent ? `${h.probes_received ?? 0}/${h.probes_sent} sondas responderam` : undefined}
                        >
                          {h.loss_pct.toFixed(0)}%
                        </Badge>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
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

// ParallelItemResultTable — Fase G (roadmap de maturidade profissional): tabela de saltos de UM
// item do lote paralelo, expandida sob demanda ("ver detalhes") — a única forma de inspecionar a
// rota de um alvo nesse modo, já que não há grafo/tabela ao vivo por item (escopo reduzido). Mesmo
// formato de coluna da tabela principal (Latência via formatLatencyCell, Perda via lossBadgeClass),
// sem fingerprint (não é o foco do resumo em lote) nem grafo Cytoscape (custo de renderização N
// instâncias simultâneas não vale a pena pra uma inspeção pontual).
function ParallelItemResultTable({ result }: { result: NetDiscoveryResult }) {
  return (
    <div className="rounded-md border border-border overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 bg-muted/40 border-b border-border text-sm">
        <Route className="w-4 h-4 text-muted-foreground" />
        <span className="font-mono">{result.target_input}</span>
        {result.target_resolved && <span className="text-xs text-muted-foreground">→ {result.target_ip}</span>}
        <Badge variant="outline" className={result.reached ? "border-emerald-500/40 text-emerald-600 dark:text-emerald-400" : "border-amber-500/40 text-amber-600 dark:text-amber-400"}>
          {result.reached ? "destino alcançado" : "destino não confirmado"}
        </Badge>
      </div>
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground">
            <th className="text-left font-medium px-2.5 py-1.5 w-12">#</th>
            <th className="text-left font-medium px-2.5 py-1.5">IP</th>
            <th className="text-left font-medium px-2.5 py-1.5">Latência</th>
            <th className="text-left font-medium px-2.5 py-1.5">Perda</th>
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
              <td className="px-2.5 py-1.5 font-mono">{formatLatencyCell(h)}</td>
              <td className="px-2.5 py-1.5 font-mono">
                {h.loss_pct ? (
                  <Badge variant="outline" className={`text-[10px] py-0 ${lossBadgeClass(h.loss_pct)}`}>{h.loss_pct.toFixed(0)}%</Badge>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// MonitorSummaryPanel — Fase C (roadmap de maturidade profissional). Renderizado só quando todas
// as rodadas do monitoramento concluíram (ver o call site acima) — agrega os NetDiscoveryResult
// completos de cada rodada via aggregateMonitorRounds (lib/netDiscoveryMonitor.ts), sem nenhuma
// chamada nova ao backend.
function MonitorSummaryPanel({ results }: { results: NetDiscoveryResult[] }) {
  const { hopAggregates, routeDiff } = aggregateMonitorRounds(results);
  const reachedCount = results.filter((r) => r.reached).length;

  return (
    <div className="rounded-md border border-border p-3 flex flex-col gap-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <ListChecks className="w-4 h-4 text-muted-foreground" />
        Resumo do monitoramento ({results.length} rodadas)
      </div>
      <div className="text-xs text-muted-foreground">
        Destino alcançado em {reachedCount}/{results.length} rodadas.
      </div>
      {routeDiff.changed ? (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">
          <AlertTriangle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
          <span>Rota mudou durante o monitoramento — {formatRouteDiffSummary(routeDiff)}.</span>
        </div>
      ) : (
        <div className="text-xs text-emerald-600 dark:text-emerald-400">Rota estável durante todo o monitoramento.</div>
      )}
      <table className="w-full text-xs mt-1">
        <thead>
          <tr className="text-muted-foreground">
            <th className="text-left font-medium px-2 py-1">Salto</th>
            <th className="text-left font-medium px-2 py-1">Respondeu</th>
            <th className="text-left font-medium px-2 py-1">Perda média</th>
          </tr>
        </thead>
        <tbody>
          {hopAggregates.map((h, i) => (
            <tr key={h.index} className={i > 0 ? "border-t border-border" : ""}>
              <td className="px-2 py-1 font-mono">{h.index}</td>
              <td className="px-2 py-1 font-mono">
                {h.respondedRounds}/{h.totalRounds}
                {h.respondedRounds > 0 && h.respondedRounds < h.totalRounds && (
                  <span className="ml-1.5 text-amber-600 dark:text-amber-400" title="Este salto não respondeu em todas as rodadas — instabilidade intermitente">
                    instável
                  </span>
                )}
              </td>
              <td className="px-2 py-1 font-mono">{h.avgLossPct > 0 ? `${h.avgLossPct.toFixed(0)}%` : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
