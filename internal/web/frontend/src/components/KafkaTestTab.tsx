import { useEffect, useRef, useState } from "react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Loader2,
  Waypoints,
  Play,
  XCircle,
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  ChevronUp,
  ArrowUpDown,
  Check,
  CheckCircle2,
  Search,
  Table2,
  Copy,
} from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes } from "@/lib/monitorUtils";
import { cn } from "@/lib/utils";
import { ProtectedAction } from "@/components/rbac";
import { useClusters } from "@/hooks/useAPI";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import { DOCKER_FIX_BY_REASON } from "@/lib/dockerFixSnippets";
import type { KafkaTestResult, KafkaTestSSEEvent, KafkaStageStatus, KafkaMessage, TopicsOverviewResponse, DBExecutionMode } from "@/lib/api/types";

// Combobox com busca embutida no mesmo popover — mesmo padrão de ClusterSelectorForTab.tsx
// (evita o bug do <Select> do Radix fechar o dropdown ao focar um campo de busca externo).
// Local a este arquivo porque é usado só aqui (Namespace/Deployment); ver ClusterSelectorForTab
// pro combobox de cluster equivalente, já usado em várias abas.
function SearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  emptyMessage,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: string[];
  placeholder: string;
  searchPlaceholder: string;
  emptyMessage: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">{value || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  onSelect={() => {
                    onChange(opt === value ? "" : opt);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === opt ? "opacity-100" : "opacity-0")} />
                  {opt}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function StageBadge({ label, status }: { label: string; status: "ok" | "failed" | "skipped" }) {
  const meta = {
    ok: { color: "bg-green-500/10 text-green-500 border-green-500/30", icon: <CheckCircle2 className="w-3 h-3" /> },
    failed: { color: "bg-red-500/10 text-red-500 border-red-500/30", icon: <XCircle className="w-3 h-3" /> },
    skipped: { color: "bg-muted text-muted-foreground border-border", icon: null },
  }[status];
  return (
    <Badge variant="outline" className={`gap-1 ${meta.color}`}>
      {meta.icon}
      {label}
    </Badge>
  );
}

// Deriva os badges de TCP/DNS, Autenticação (só se SASL configurado) e Protocolo Kafka a partir
// da classificação única do estágio de conectividade (ver kafka_test_tool.go).
function deriveConnectivityBadges(status: KafkaStageStatus, saslEnabled: boolean) {
  const badges: { label: string; status: "ok" | "failed" | "skipped" }[] = [];
  badges.push({ label: "TCP/DNS", status: status === "tcp_failed" ? "failed" : "ok" });
  if (saslEnabled) {
    badges.push({
      label: "Autenticação",
      status: status === "tcp_failed" ? "skipped" : status === "auth_failed" ? "failed" : "ok",
    });
  }
  badges.push({ label: "Protocolo Kafka", status: status === "ok" ? "ok" : "failed" });
  return badges;
}

export default function KafkaTestTab() {
  const queryClient = useQueryClient();
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [deployment, setDeployment] = useState("");
  const [broker, setBroker] = useState("");

  // executionMode="pod" (default): ephemeral container anexado a um pod real do Deployment,
  // reflete NetworkPolicy/Istio. "local": container Docker local no servidor da aplicação — útil
  // quando o broker é alcançável direto da rede do servidor (VPN, endpoint público) e não faz
  // sentido refletir a identidade de rede de um pod específico. Mesmo campo/padrão do Teste de
  // Banco de Dados (DatabaseTestTab.tsx) — requer Docker instalado no servidor, não clientes
  // nativos (ver DOCKER_FIX_BY_REASON / painel de pré-checagem abaixo).
  const [executionMode, setExecutionMode] = useState<DBExecutionMode>("pod");

  const { data: dockerStatus } = useQuery({
    queryKey: ["kafka-test-docker-status"],
    queryFn: () => apiClient.getKafkaTestDockerStatus(),
    enabled: executionMode === "local",
  });
  const dockerReady = executionMode !== "local" || !!(dockerStatus?.installed && dockerStatus?.daemon_running);

  const [saslEnabled, setSaslEnabled] = useState(false);
  const [mechanism, setMechanism] = useState<"PLAIN" | "SCRAM-SHA-256" | "SCRAM-SHA-512">("PLAIN");
  const [useTLS, setUseTLS] = useState(false);
  const [skipTLSVerify, setSkipTLSVerify] = useState(false);
  const [credSource, setCredSource] = useState<"manual" | "secret">("manual");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [secretNamespace, setSecretNamespace] = useState("");
  const [secretName, setSecretName] = useState("");
  const [usernameKey, setUsernameKey] = useState("username");
  const [passwordKey, setPasswordKey] = useState("password");
  const [secretBase64Decode, setSecretBase64Decode] = useState(false);

  const [produceConsumeEnabled, setProduceConsumeEnabled] = useState(false);
  const [topic, setTopic] = useState("");
  const [confirmProduce, setConfirmProduce] = useState(false);

  const [viewTopicEnabled, setViewTopicEnabled] = useState(false);
  const [viewMaxMessages, setViewMaxMessages] = useState(10);

  const [countOffsetsEnabled, setCountOffsetsEnabled] = useState(false);

  const [timeoutMs, setTimeoutMs] = useState(5000);

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState(0);
  const [phaseMessage, setPhaseMessage] = useState("");
  const [result, setResult] = useState<KafkaTestResult | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [rawOutputOpen, setRawOutputOpen] = useState(false);
  const [selectedMessage, setSelectedMessage] = useState<KafkaMessage | null>(null);
  const esRef = useRef<EventSource | null>(null);

  // Busca de tópicos (popover de seleção) — lista sob demanda via kcat -L, não em toda digitação:
  // cada busca abre um ephemeral container real no pod, então é uma ação explícita do usuário.
  const [topicSearchOpen, setTopicSearchOpen] = useState(false);
  const [topicOptions, setTopicOptions] = useState<string[]>([]);
  const [topicSearchLoading, setTopicSearchLoading] = useState(false);
  const [topicSearchError, setTopicSearchError] = useState<string | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-kafka-test", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const { data: deployments = [] } = useQuery({
    queryKey: ["deployments-kafka-test", cluster, namespace],
    queryFn: () => apiClient.getDeployments(cluster, [namespace]),
    enabled: !!cluster && !!namespace,
  });

  const needsTopic = produceConsumeEnabled || viewTopicEnabled || countOffsetsEnabled;

  // Cluster só é obrigatório no modo "pod" (resolve o Deployment/pod/ephemeral container) ou
  // quando a credencial SASL vem de um Secret do K8s (precisa da API do K8s pra ler, mesmo que o
  // teste em si rode local). Namespace/Deployment só existem no modo "pod". Mesmo raciocínio de
  // needsCluster/usesK8sRef em DatabaseTestTab.tsx.
  const usesK8sRef = saslEnabled && credSource === "secret";
  const needsCluster = executionMode === "pod" || usesK8sRef;

  const canRun =
    (!needsCluster || !!cluster) &&
    (executionMode !== "pod" || (!!namespace && !!deployment)) &&
    dockerReady &&
    !!broker.trim() &&
    !isRunning &&
    (!needsTopic || !!topic.trim()) &&
    (!produceConsumeEnabled || confirmProduce);

  // Reaproveitado pelo teste completo e pela busca de tópicos — mesma config SASL nos dois casos.
  const buildSaslPayload = () =>
    saslEnabled
      ? {
          mechanism,
          use_tls: useTLS,
          skip_tls_verify: skipTLSVerify,
          ...(credSource === "manual"
            ? { username, password }
            : {
                secret_ref: {
                  namespace: secretNamespace || namespace,
                  name: secretName,
                  username_key: usernameKey || "username",
                  password_key: passwordKey || "password",
                  base64_decode: secretBase64Decode,
                },
              }),
        }
      : undefined;

  const runTest = async () => {
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando teste de Kafka...");
    setIsRunning(true);
    try {
      const { session_id } = await apiClient.runKafkaTest({
        execution_mode: executionMode,
        cluster,
        namespace,
        deployment,
        broker: broker.trim(),
        sasl: buildSaslPayload(),
        produce_consume: produceConsumeEnabled,
        topic: needsTopic ? topic.trim() : undefined,
        confirm_produce: produceConsumeEnabled ? confirmProduce : false,
        view_topic: viewTopicEnabled,
        view_max_messages: viewTopicEnabled ? viewMaxMessages : undefined,
        count_offsets: countOffsetsEnabled,
        timeout_ms: timeoutMs,
      });
      setSessionId(session_id);
    } catch (err) {
      setIsRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o teste");
    }
  };

  const canSearchTopics =
    (!needsCluster || !!cluster) &&
    (executionMode !== "pod" || (!!namespace && !!deployment)) &&
    dockerReady &&
    !!broker.trim();

  const searchTopics = async () => {
    if (!canSearchTopics) return;
    setTopicSearchLoading(true);
    setTopicSearchError(null);
    try {
      const { topics, raw_output } = await apiClient.listKafkaTopics({
        execution_mode: executionMode,
        cluster,
        namespace,
        deployment,
        broker: broker.trim(),
        sasl: buildSaslPayload(),
        timeout_ms: timeoutMs,
      });
      setTopicOptions(topics);
      if (topics.length === 0) {
        setTopicSearchError(raw_output ? "Nenhum tópico encontrado — ver saída bruta" : "Nenhum tópico encontrado no broker");
      }
    } catch (err) {
      setTopicOptions([]);
      setTopicSearchError(err instanceof Error ? err.message : "Falha ao buscar tópicos");
    } finally {
      setTopicSearchLoading(false);
    }
  };

  const openTopicSearch = () => {
    setTopicSearchOpen(true);
    if (topicOptions.length === 0 && !topicSearchLoading) {
      searchTopics();
    }
  };

  // Visão geral de tópicos (Partições + ~Mensagens) — mesmo espírito do "All Stats" do MongoDB
  // Compass no Teste de Banco de Dados, adaptado ao que o kcat expõe pro Kafka (sem tamanho em
  // disco, que exigiria os scripts nativos do Kafka/JMX). Ação síncrona à parte do teste
  // principal, mesmo padrão da busca de tópicos (searchTopics) — não passa por needsTopic.
  const [topicsOverviewOpen, setTopicsOverviewOpen] = useState(false);
  const [topicsOverviewLoading, setTopicsOverviewLoading] = useState(false);
  const [topicsOverviewError, setTopicsOverviewError] = useState<string | null>(null);
  const [topicsOverviewData, setTopicsOverviewData] = useState<TopicsOverviewResponse | null>(null);
  const [overviewSortKey, setOverviewSortKey] = useState<"topic" | "partitions" | "message_count" | "disk_bytes">("message_count");
  const [overviewSortDir, setOverviewSortDir] = useState<"asc" | "desc">("desc");

  const loadTopicsOverview = async () => {
    if (!canSearchTopics) return;
    setTopicsOverviewOpen(true);
    setTopicsOverviewLoading(true);
    setTopicsOverviewError(null);
    try {
      const data = await apiClient.kafkaTopicsOverview({
        execution_mode: executionMode,
        cluster,
        namespace,
        deployment,
        broker: broker.trim(),
        sasl: buildSaslPayload(),
        timeout_ms: timeoutMs,
      });
      setTopicsOverviewData(data);
      if (data.topics.length === 0) {
        setTopicsOverviewError(data.raw_output ? "Nenhum tópico encontrado — ver saída bruta" : "Nenhum tópico encontrado no broker");
      }
    } catch (err) {
      setTopicsOverviewData(null);
      setTopicsOverviewError(err instanceof Error ? err.message : "Falha ao buscar a visão geral de tópicos");
    } finally {
      setTopicsOverviewLoading(false);
    }
  };

  const sortedOverviewTopics = [...(topicsOverviewData?.topics ?? [])].sort((a, b) => {
    const dir = overviewSortDir === "asc" ? 1 : -1;
    if (overviewSortKey === "topic") return a.topic.localeCompare(b.topic) * dir;
    return (a[overviewSortKey] - b[overviewSortKey]) * dir;
  });

  const overviewSortIcon = (key: typeof overviewSortKey) =>
    overviewSortKey !== key ? (
      <ArrowUpDown className="w-3 h-3 opacity-40" />
    ) : overviewSortDir === "asc" ? (
      <ChevronUp className="w-3 h-3" />
    ) : (
      <ChevronDown className="w-3 h-3" />
    );

  const sortableOverviewHeader = (key: typeof overviewSortKey, label: string, align: "left" | "right" = "left") => (
    <TableHead className={align === "right" ? "text-right" : ""}>
      <button
        type="button"
        onClick={() => {
          if (key === overviewSortKey) setOverviewSortDir((d) => (d === "asc" ? "desc" : "asc"));
          else {
            setOverviewSortKey(key);
            setOverviewSortDir("desc");
          }
        }}
        className={cn(
          "inline-flex items-center gap-1 text-xs font-medium hover:text-foreground",
          align === "right" && "flex-row-reverse",
        )}
      >
        {label}
        {overviewSortIcon(key)}
      </button>
    </TableHead>
  );

  const cancelTest = async () => {
    if (!sessionId) return;
    try {
      await apiClient.cancelKafkaTest(sessionId);
    } catch {
      /* ignore — o pod é limpo no servidor de qualquer forma */
    }
    esRef.current?.close();
    setIsRunning(false);
    setPhaseMessage("Teste cancelado.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getKafkaTestStreamURL(sessionId));
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: KafkaTestSSEEvent = JSON.parse(e.data);
        setPhaseMessage(event.message);
        setProgress(event.progress);

        if (event.type === "complete" && event.result) {
          setResult(event.result);
          setIsRunning(false);
        }
        if (event.type === "error") {
          setRunError(event.error || event.message);
          setIsRunning(false);
        }
      } catch {
        /* ignore evento malformado */
      }
    };

    es.onerror = () => {
      es.close();
      setIsRunning(false);
    };

    return () => {
      es.close();
      esRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-4">
          <RadioGroup
            value={executionMode}
            onValueChange={(v) => { setExecutionMode(v as DBExecutionMode); setResult(null); }}
            className="flex items-center gap-4"
          >
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="pod" id="kafka-exec-pod" />
              <label htmlFor="kafka-exec-pod" className="text-sm cursor-pointer">Via Pod do Cluster</label>
            </div>
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="local" id="kafka-exec-local" />
              <label htmlFor="kafka-exec-local" className="text-sm cursor-pointer">Direto do servidor (terminal local)</label>
            </div>
          </RadioGroup>
          {executionMode === "local" && (
            <span className="text-[10px] text-amber-600 dark:text-amber-400">
              Roda a mesma imagem do modo Pod (kcat) num container Docker local no servidor (`docker run --rm`) — requer Docker instalado lá. Não reflete NetworkPolicy/Istio do cluster.
            </span>
          )}
        </div>

        {executionMode === "local" && dockerStatus && !dockerReady && (() => {
          const fix = DOCKER_FIX_BY_REASON[dockerStatus.reason ?? "daemon_unreachable"] ?? DOCKER_FIX_BY_REASON.daemon_unreachable;
          return (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 flex flex-col gap-2">
            <div className="text-sm text-amber-700 dark:text-amber-400">
              <span className="font-semibold">{fix.title}</span>
              {dockerStatus.error && <span> — {dockerStatus.error}</span>}
            </div>
            <div className="relative">
              <pre className="rounded-md border border-border bg-muted/30 p-3 pr-10 text-[11px] font-mono whitespace-pre-wrap overflow-x-auto">
                {fix.snippet}
              </pre>
              <Button
                variant="ghost"
                size="icon"
                className="absolute top-1.5 right-1.5 h-6 w-6"
                onClick={() => {
                  navigator.clipboard.writeText(fix.snippet);
                  toast.success("Comando copiado!");
                }}
              >
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
            <Button
              size="sm"
              variant="outline"
              className="w-fit"
              onClick={() => queryClient.invalidateQueries({ queryKey: ["kafka-test-docker-status"] })}
            >
              Verificar novamente
            </Button>
          </div>
          );
        })()}

        <div className="flex flex-wrap items-end gap-3">
        {needsCluster && (
        <div className="min-w-[220px]">
          <ClusterSelectorForTab
            selectedCluster={cluster}
            onClusterChange={(v) => {
              setCluster(v);
              setNamespace("");
              setDeployment("");
              setResult(null);
            }}
            clusters={clusters.map((c) => c.context)}
            tabLabel="Teste Kafka"
            clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
          />
        </div>
        )}

        {executionMode === "pod" && (
        <>
        <div className="min-w-[200px]">
          <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
          <SearchableSelect
            value={namespace}
            onChange={(v) => { setNamespace(v); setDeployment(""); }}
            options={namespaces.map((ns) => ns.name)}
            placeholder="Selecione o namespace"
            searchPlaceholder="Buscar namespace..."
            emptyMessage="Nenhum namespace encontrado."
            disabled={!cluster}
          />
        </div>

        <div className="min-w-[240px]">
          <label className="text-xs text-muted-foreground block mb-1">Deployment (de onde o teste parte)</label>
          <SearchableSelect
            value={deployment}
            onChange={setDeployment}
            options={deployments.map((d) => d.name)}
            placeholder="Selecione o deployment"
            searchPlaceholder="Buscar deployment..."
            emptyMessage="Nenhum deployment encontrado."
            disabled={!namespace}
          />
        </div>
        </>
        )}

        <div className="min-w-[280px] flex-1">
          <label className="text-xs text-muted-foreground block mb-1">Broker (host:porta)</label>
          <Input
            placeholder="kfk-mht-prd01.com:9098"
            value={broker}
            onChange={(e) => setBroker(e.target.value)}
          />
        </div>

        <div className="w-28">
          <label className="text-xs text-muted-foreground block mb-1">Timeout (ms)</label>
          <Input
            type="number"
            min={100}
            max={15000}
            value={timeoutMs}
            onChange={(e) => setTimeoutMs(Math.min(15000, Math.max(100, Number(e.target.value) || 100)))}
          />
        </div>

        <ProtectedAction>
          {!isRunning ? (
            <Button onClick={runTest} disabled={!canRun}>
              <Play className="w-4 h-4 mr-2" />
              Executar Teste
            </Button>
          ) : (
            <Button variant="destructive" onClick={cancelTest}>
              <XCircle className="w-4 h-4 mr-2" />
              Cancelar
            </Button>
          )}
        </ProtectedAction>
        </div>
      </div>

      <div className="px-6 py-3 border-b border-border flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <Switch checked={saslEnabled} onCheckedChange={setSaslEnabled} id="sasl-toggle" />
          <label htmlFor="sasl-toggle" className="text-sm font-medium cursor-pointer">
            Autenticação SASL
          </label>
        </div>

        {saslEnabled && (
          <div className="flex flex-wrap items-end gap-3 pl-8">
            <div className="w-44">
              <label className="text-xs text-muted-foreground block mb-1">Mecanismo</label>
              <Select value={mechanism} onValueChange={(v) => setMechanism(v as typeof mechanism)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="PLAIN">PLAIN</SelectItem>
                  <SelectItem value="SCRAM-SHA-256">SCRAM-SHA-256</SelectItem>
                  <SelectItem value="SCRAM-SHA-512">SCRAM-SHA-512</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <Switch checked={useTLS} onCheckedChange={setUseTLS} id="tls-toggle" />
              <label htmlFor="tls-toggle" className="text-sm cursor-pointer">Usar TLS</label>
            </div>

            {useTLS && (
              <div className="flex items-center gap-2">
                <Checkbox checked={skipTLSVerify} onCheckedChange={(v) => setSkipTLSVerify(!!v)} id="skip-tls" />
                <label htmlFor="skip-tls" className="text-sm text-muted-foreground cursor-pointer">
                  Ignorar verificação de certificado (não recomendado)
                </label>
              </div>
            )}

            <div className="w-full">
              <RadioGroup value={credSource} onValueChange={(v) => setCredSource(v as typeof credSource)} className="flex items-center gap-4">
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="manual" id="cred-manual" />
                  <label htmlFor="cred-manual" className="text-sm cursor-pointer">Digitar manualmente</label>
                </div>
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="secret" id="cred-secret" />
                  <label htmlFor="cred-secret" className="text-sm cursor-pointer">Ler de um Secret do K8s</label>
                </div>
              </RadioGroup>
            </div>

            {credSource === "manual" ? (
              <>
                <div className="w-56">
                  <label className="text-xs text-muted-foreground block mb-1">Usuário</label>
                  <Input placeholder="ex: $ConnectionString (Azure Event Hub)" value={username} onChange={(e) => setUsername(e.target.value)} />
                </div>
                <div className="w-56">
                  <label className="text-xs text-muted-foreground block mb-1">Senha</label>
                  <Input placeholder="ex: connection string completa (Event Hub)" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
                </div>
              </>
            ) : (
              <>
                <div className="w-44">
                  <label className="text-xs text-muted-foreground block mb-1">Namespace do Secret</label>
                  <Input
                    placeholder={namespace || "(mesmo do teste)"}
                    value={secretNamespace}
                    onChange={(e) => setSecretNamespace(e.target.value)}
                  />
                </div>
                <div className="w-48">
                  <label className="text-xs text-muted-foreground block mb-1">Nome do Secret</label>
                  <Input value={secretName} onChange={(e) => setSecretName(e.target.value)} />
                </div>
                <div className="w-36">
                  <label className="text-xs text-muted-foreground block mb-1">Chave usuário</label>
                  <Input value={usernameKey} onChange={(e) => setUsernameKey(e.target.value)} />
                </div>
                <div className="w-36">
                  <label className="text-xs text-muted-foreground block mb-1">Chave senha</label>
                  <Input value={passwordKey} onChange={(e) => setPasswordKey(e.target.value)} />
                </div>
                <div className="w-full flex items-center gap-2">
                  <Checkbox checked={secretBase64Decode} onCheckedChange={(v) => setSecretBase64Decode(!!v)} id="secret-b64" />
                  <label htmlFor="secret-b64" className="text-xs text-muted-foreground cursor-pointer max-w-md">
                    Valores no Secret estão em Base64 (decodificar antes de autenticar — comum em secrets sincronizados de Azure Key Vault via external-secrets)
                  </label>
                </div>
              </>
            )}
          </div>
        )}

        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="gap-1.5"
            disabled={!canSearchTopics}
            title={canSearchTopics ? "Lista todos os tópicos do broker com partições e ~mensagens retidas" : "Preencha cluster, namespace, deployment e broker primeiro"}
            onClick={loadTopicsOverview}
          >
            <Table2 className="w-4 h-4" />
            Visão geral de tópicos
          </Button>
        </div>

        {needsTopic && (
          <div className="w-72">
            <label className="text-xs text-muted-foreground block mb-1">Tópico</label>
            <div className="flex gap-1.5">
              <Input placeholder="meu-topico" value={topic} onChange={(e) => setTopic(e.target.value)} />
              <Popover open={topicSearchOpen} onOpenChange={setTopicSearchOpen}>
                <PopoverTrigger asChild>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    disabled={!canSearchTopics}
                    title={canSearchTopics ? "Buscar tópicos no broker" : "Preencha cluster, namespace, deployment e broker primeiro"}
                    onClick={openTopicSearch}
                  >
                    <Search className="h-4 w-4" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-72 p-0" align="end">
                  <Command shouldFilter={!topicSearchLoading}>
                    <CommandInput placeholder="Filtrar tópicos..." />
                    <CommandList>
                      {topicSearchLoading ? (
                        <div className="py-6 flex items-center justify-center gap-2 text-sm text-muted-foreground">
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Buscando tópicos no broker...
                        </div>
                      ) : topicSearchError ? (
                        <div className="p-3 text-xs text-muted-foreground flex flex-col gap-2">
                          <span>{topicSearchError}</span>
                          <Button size="sm" variant="outline" onClick={searchTopics}>Tentar de novo</Button>
                        </div>
                      ) : (
                        <>
                          <CommandEmpty>Nenhum tópico encontrado.</CommandEmpty>
                          <CommandGroup>
                            {topicOptions.map((t) => (
                              <CommandItem
                                key={t}
                                value={t}
                                onSelect={() => {
                                  setTopic(t);
                                  setTopicSearchOpen(false);
                                }}
                              >
                                <Check className={cn("mr-2 h-4 w-4", topic === t ? "opacity-100" : "opacity-0")} />
                                <span className="truncate">{t}</span>
                              </CommandItem>
                            ))}
                          </CommandGroup>
                        </>
                      )}
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
            </div>
            <div className="text-[10px] text-muted-foreground mt-1">
              A busca anexa um container efêmero real no pod pra listar os tópicos visíveis dessa identidade de rede — pode digitar direto se já souber o nome.
            </div>
          </div>
        )}

        <div className="flex items-center gap-2">
          <Switch checked={produceConsumeEnabled} onCheckedChange={setProduceConsumeEnabled} id="pc-toggle" />
          <label htmlFor="pc-toggle" className="text-sm font-medium cursor-pointer">
            Produzir e consumir mensagem de teste
          </label>
        </div>

        {produceConsumeEnabled && (
          <div className="flex items-center gap-2 pl-8">
            <Checkbox checked={confirmProduce} onCheckedChange={(v) => setConfirmProduce(!!v)} id="confirm-produce" />
            <label htmlFor="confirm-produce" className="text-sm text-amber-600 dark:text-amber-400 cursor-pointer max-w-md">
              Entendo que isso publica uma mensagem real neste tópico
            </label>
          </div>
        )}

        <div className="flex items-center gap-2">
          <Switch checked={viewTopicEnabled} onCheckedChange={setViewTopicEnabled} id="view-toggle" />
          <label htmlFor="view-toggle" className="text-sm font-medium cursor-pointer">
            Visualizar mensagens existentes (só leitura)
          </label>
        </div>

        {viewTopicEnabled && (
          <div className="flex items-end gap-3 pl-8">
            <div className="w-32">
              <label className="text-xs text-muted-foreground block mb-1">Últimas N mensagens</label>
              <Input
                type="number"
                min={1}
                max={50}
                value={viewMaxMessages}
                onChange={(e) => setViewMaxMessages(Math.min(50, Math.max(1, Number(e.target.value) || 1)))}
              />
            </div>
          </div>
        )}

        <div className="flex items-center gap-2">
          <Switch checked={countOffsetsEnabled} onCheckedChange={setCountOffsetsEnabled} id="count-offsets-toggle" />
          <label htmlFor="count-offsets-toggle" className="text-sm font-medium cursor-pointer">
            Contar mensagens por offset (só leitura)
          </label>
        </div>
      </div>

      <div className="p-6 flex flex-col gap-4">
        {isRunning && (
          <div className="rounded-md border border-border p-4">
            <div className="flex items-center gap-2 text-sm mb-2">
              <Loader2 className="w-4 h-4 animate-spin text-primary" />
              {phaseMessage}
            </div>
            <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all duration-300"
                style={{ width: `${Math.round(progress * 100)}%` }}
              />
            </div>
          </div>
        )}

        {runError && (
          <div className="rounded-md border p-3 text-sm flex items-center gap-2 border-red-500/40 bg-red-500/10 text-red-500">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            {runError}
          </div>
        )}

        {!isRunning && !result && !runError && (
          <div className="text-sm text-muted-foreground flex items-center gap-2">
            <Waypoints className="w-4 h-4" />
            Selecione cluster, namespace, o Deployment de onde o teste deve partir e o broker
            (host:porta) e clique em "Executar Teste" — anexa um container efêmero num pod já
            rodando desse Deployment, refletindo a identidade de rede real dele
            (NetworkPolicy/Istio avaliados por label/service account do pod).
          </div>
        )}

        {result && (
          <div className="flex flex-col gap-4">
            <div className="text-xs text-muted-foreground space-y-0.5">
              <div>
                Testado a partir do pod <span className="font-mono">{result.target_pod}</span> — container efêmero{" "}
                <span className="font-mono">{result.ephemeral_container}</span>
              </div>
              <div>
                Ephemeral containers não podem ser removidos via API do K8s — o processo se encerra sozinho após 5min,
                mas continua listado no pod até ele reiniciar. Pra conferir o estado dele:{" "}
                <code className="bg-muted px-1 py-0.5 rounded text-[10px]">
                  {`kubectl get pod ${result.target_pod} -n ${namespace} -o jsonpath='{.status.ephemeralContainerStatuses}'`}
                </code>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {deriveConnectivityBadges(result.connectivity.status, saslEnabled).map((b) => (
                <StageBadge key={b.label} label={b.label} status={b.status} />
              ))}
              {produceConsumeEnabled && (
                <StageBadge
                  label="Produzir/Consumir"
                  status={
                    result.produce_consume.status === "ok"
                      ? "ok"
                      : result.produce_consume.status === "skipped"
                      ? "skipped"
                      : "failed"
                  }
                />
              )}
              {countOffsetsEnabled && (
                <StageBadge
                  label="Offsets"
                  status={
                    result.offset_count.status === "ok"
                      ? "ok"
                      : result.offset_count.status === "skipped"
                      ? "skipped"
                      : "failed"
                  }
                />
              )}
              {viewTopicEnabled && (
                <StageBadge
                  label="Visualizar"
                  status={
                    result.view_topic.status === "ok"
                      ? "ok"
                      : result.view_topic.status === "skipped"
                      ? "skipped"
                      : "failed"
                  }
                />
              )}
            </div>

            {result.connectivity.status === "auth_failed" ? (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 flex flex-col gap-2">
                <div className="text-sm text-amber-700 dark:text-amber-400">{result.connectivity.message}</div>
                <div className="flex flex-wrap gap-2">
                  {!saslEnabled && (
                    <Button size="sm" variant="outline" onClick={() => setSaslEnabled(true)}>
                      Ligar Autenticação SASL
                    </Button>
                  )}
                  {result.connectivity.suggested_mechanism && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setSaslEnabled(true);
                        const first = result.connectivity.suggested_mechanism!.split(/[,\s]+/)[0] as typeof mechanism;
                        if (first === "PLAIN" || first === "SCRAM-SHA-256" || first === "SCRAM-SHA-512") {
                          setMechanism(first);
                        }
                      }}
                    >
                      Usar mecanismo sugerido ({result.connectivity.suggested_mechanism})
                    </Button>
                  )}
                </div>
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">{result.connectivity.message}</div>
            )}

            {(result.connectivity.broker_count || result.connectivity.topic_count) && (
              <div className="flex items-center gap-3 text-sm">
                {!!result.connectivity.broker_count && (
                  <Badge variant="outline">{result.connectivity.broker_count} broker(s)</Badge>
                )}
                {!!result.connectivity.topic_count && (
                  <Badge variant="outline">{result.connectivity.topic_count} tópico(s)</Badge>
                )}
              </div>
            )}

            {produceConsumeEnabled && (
              <div className="text-sm text-muted-foreground">
                {result.produce_consume.message}
                {result.produce_consume.round_trip_ms !== undefined && (
                  <span className="font-mono ml-2">({result.produce_consume.round_trip_ms}ms round-trip aprox.)</span>
                )}
              </div>
            )}

            {countOffsetsEnabled && (
              <div className="flex flex-col gap-2">
                <div className="text-sm text-muted-foreground">
                  {result.offset_count.message}
                  {result.offset_count.total_messages !== undefined && result.offset_count.status === "ok" && (
                    <span className="font-mono ml-2 text-foreground">({result.offset_count.total_messages.toLocaleString("pt-BR")} no total)</span>
                  )}
                </div>
                {result.offset_count.partitions && result.offset_count.partitions.length > 0 && (
                  <div className="border border-border rounded-md overflow-hidden">
                    <table className="w-full text-xs">
                      <thead className="bg-muted/50">
                        <tr>
                          <th className="text-left px-2.5 py-1.5 font-medium text-muted-foreground">Partição</th>
                          <th className="text-right px-2.5 py-1.5 font-medium text-muted-foreground">Offset inicial</th>
                          <th className="text-right px-2.5 py-1.5 font-medium text-muted-foreground">Offset final</th>
                          <th className="text-right px-2.5 py-1.5 font-medium text-muted-foreground">Mensagens retidas</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border">
                        {result.offset_count.partitions.map((p) => (
                          <tr key={p.partition}>
                            <td className="px-2.5 py-1.5 font-mono">{p.partition}</td>
                            <td className="px-2.5 py-1.5 font-mono text-right">{p.earliest.toLocaleString("pt-BR")}</td>
                            <td className="px-2.5 py-1.5 font-mono text-right">{p.latest.toLocaleString("pt-BR")}</td>
                            <td className="px-2.5 py-1.5 font-mono text-right">{p.count.toLocaleString("pt-BR")}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}

            {viewTopicEnabled && (
              <div className="flex flex-col gap-2">
                <div className="text-sm text-muted-foreground">{result.view_topic.message}</div>
                {result.view_topic.messages && result.view_topic.messages.length > 0 && (
                  <div className="border border-border rounded-md divide-y divide-border overflow-hidden">
                    {result.view_topic.messages.map((m, i) => (
                      <button
                        key={i}
                        type="button"
                        onClick={() => setSelectedMessage(m)}
                        className="w-full text-left px-2.5 py-1.5 hover:bg-muted/50 transition-colors flex items-center gap-3"
                      >
                        <span className="text-[10px] text-muted-foreground shrink-0">
                          p{m.partition}@{m.offset}
                        </span>
                        {m.binary && (
                          <Badge variant="outline" className="text-[9px] px-1 py-0 shrink-0 bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30">
                            binário
                          </Badge>
                        )}
                        {m.timestamp_ms ? (
                          <span className="text-[10px] text-muted-foreground shrink-0 font-mono">
                            {new Date(m.timestamp_ms).toLocaleString("pt-BR")}
                          </span>
                        ) : null}
                        {m.key && (
                          <span className="text-[10px] font-mono text-muted-foreground shrink-0 truncate max-w-[100px]">
                            key: {m.key}
                          </span>
                        )}
                        <span className="text-xs font-mono truncate flex-1 min-w-0">{m.payload}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}

            <Collapsible open={rawOutputOpen} onOpenChange={setRawOutputOpen}>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" size="sm" className="w-fit gap-1 text-xs text-muted-foreground">
                  {rawOutputOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  Saída bruta do kcat
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1.5 rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap overflow-x-auto">
                  {result.connectivity.raw_output || "(sem saída)"}
                  {produceConsumeEnabled && result.produce_consume.raw_output && (
                    <>
                      {"\n\n--- produce/consume ---\n"}
                      {result.produce_consume.raw_output}
                    </>
                  )}
                  {countOffsetsEnabled && result.offset_count.raw_output && (
                    <>
                      {"\n\n--- contagem de offsets ---\n"}
                      {result.offset_count.raw_output}
                    </>
                  )}
                  {viewTopicEnabled && result.view_topic.raw_output && (
                    <>
                      {"\n\n--- visualizar tópico ---\n"}
                      {result.view_topic.raw_output}
                    </>
                  )}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          </div>
        )}
      </div>

      <Dialog open={!!selectedMessage} onOpenChange={(open) => !open && setSelectedMessage(null)}>
        <DialogContent className="max-w-2xl max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Mensagem do tópico</DialogTitle>
          </DialogHeader>
          {selectedMessage && (
            <div className="flex flex-col gap-3 min-h-0 flex-1">
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                <span>Partição: <span className="font-mono text-foreground">{selectedMessage.partition}</span></span>
                <span>Offset: <span className="font-mono text-foreground">{selectedMessage.offset}</span></span>
                {selectedMessage.timestamp_ms ? (
                  <span>Timestamp: <span className="font-mono text-foreground">{new Date(selectedMessage.timestamp_ms).toLocaleString("pt-BR")}</span></span>
                ) : null}
              </div>
              {selectedMessage.binary && (
                <div className="text-xs rounded-md border border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400 p-2">
                  Payload/key contém bytes que não são UTF-8 válido (dados binários — protobuf/Avro,
                  ou um tópico interno do Kafka como <span className="font-mono">__consumer_offsets</span>).
                  O kcat já substitui esses bytes por "�" antes de entregar o resultado — o texto abaixo
                  não reflete os bytes originais com exatidão.
                </div>
              )}
              {selectedMessage.key && (
                <div>
                  <div className="text-xs text-muted-foreground mb-1">Key</div>
                  <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-md border border-border bg-muted/30 p-2">
                    {selectedMessage.key}
                  </pre>
                </div>
              )}
              <div className="flex flex-col min-h-0 flex-1">
                <div className="text-xs text-muted-foreground mb-1">Payload</div>
                <ScrollArea className="flex-1 min-h-0 rounded-md border border-border bg-muted/30">
                  <pre className="text-xs font-mono whitespace-pre-wrap break-all p-2">{selectedMessage.payload}</pre>
                </ScrollArea>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={topicsOverviewOpen} onOpenChange={setTopicsOverviewOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Visão geral de tópicos</DialogTitle>
          </DialogHeader>
          {topicsOverviewLoading ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground py-10">
              <div className="flex items-center gap-2">
                <Loader2 className="w-4 h-4 animate-spin" />
                Consultando partições e offsets de todos os tópicos...
              </div>
              {executionMode === "local" && (
                <span className="text-[11px] text-center max-w-sm">
                  Modo local também calcula o tamanho em disco via imagem completa do Kafka — pode demorar mais na primeira vez (pull da imagem + JVM subindo).
                </span>
              )}
            </div>
          ) : topicsOverviewError && (!topicsOverviewData || topicsOverviewData.topics.length === 0) ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 text-sm text-muted-foreground py-10">
              <AlertTriangle className="w-5 h-5" />
              {topicsOverviewError}
              <Button size="sm" variant="outline" onClick={loadTopicsOverview}>Tentar de novo</Button>
            </div>
          ) : topicsOverviewData ? (
            <div className="flex-1 min-h-0 flex flex-col gap-2 overflow-hidden">
              {topicsOverviewData.truncated && (
                <div className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1.5 shrink-0">
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                  Muitos tópicos no broker — a contagem de ~mensagens foi limitada aos primeiros (ordem alfabética); os demais aparecem com "—" nessa coluna.
                </div>
              )}
              {executionMode === "local" && topicsOverviewData.disk_usage_warning && (
                <div className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1.5 shrink-0">
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                  {topicsOverviewData.disk_usage_warning}
                </div>
              )}
              <div className="flex-1 overflow-auto border border-border rounded-md">
                <Table>
                  <TableHeader className="sticky top-0 bg-background">
                    <TableRow>
                      {sortableOverviewHeader("topic", "Tópico")}
                      {sortableOverviewHeader("partitions", "Partições", "right")}
                      {sortableOverviewHeader("message_count", "~Mensagens", "right")}
                      {executionMode === "local" && sortableOverviewHeader("disk_bytes", "Disco", "right")}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sortedOverviewTopics.map((t) => (
                      <TableRow key={t.topic}>
                        <TableCell className="py-1.5 font-mono text-xs truncate max-w-[280px]" title={t.topic}>{t.topic}</TableCell>
                        <TableCell className="py-1.5 text-right text-xs font-mono">{t.partitions}</TableCell>
                        <TableCell className="py-1.5 text-right text-xs font-mono">
                          {t.message_count < 0 ? "—" : t.message_count.toLocaleString("pt-BR")}
                        </TableCell>
                        {executionMode === "local" && (
                          <TableCell className="py-1.5 text-right text-xs font-mono">
                            {t.disk_bytes < 0 ? "—" : formatBytes(t.disk_bytes)}
                          </TableCell>
                        )}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
