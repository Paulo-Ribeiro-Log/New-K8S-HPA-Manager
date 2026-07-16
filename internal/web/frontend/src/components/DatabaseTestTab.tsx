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
  Database,
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
  Copy,
} from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { ProtectedAction } from "@/components/rbac";
import { useClusters } from "@/hooks/useAPI";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { formatBytes } from "@/lib/monitorUtils";
import { toast } from "sonner";
import type { DBTestResult, DBTestSSEEvent, DBStageStatus, DBEngine, DBAuthMode, DBExecutionMode, DBBrowseObject } from "@/lib/api/types";

// Passo a passo de instalação do Docker Engine — cobre Ubuntu e WSL2 (ambiente alvo principal
// desta app, ver CLAUDE.md). Script oficial da Docker (get.docker.com) funciona nos dois; o passo
// 3 é o que mais varia — WSL2 normalmente não sobe serviços sozinho no boot (sem systemd por
// padrão), por isso o `service docker start` como primeira opção + systemctl como alternativa.
const DOCKER_INSTALL_SNIPPET = `# 1. Instala o Docker Engine (script oficial — cobre Ubuntu e WSL2)
curl -fsSL https://get.docker.com | sudo sh

# 2. Permite rodar docker sem sudo (evita pedir senha em cada teste)
sudo usermod -aG docker $USER
newgrp docker   # ou feche e reabra o terminal/WSL

# 3. Inicia o serviço — WSL2 normalmente não sobe serviços sozinho no boot
sudo service docker start
# alternativa em distros com systemd habilitado no WSL2:
# sudo systemctl enable --now docker`;

// "address_pool_exhausted": dockerd chega a tentar subir mas crasha ao criar a rede bridge padrão
// com "all predefined address pools have been fully subnetted" — causa mais comum é VPN
// corporativa com split-tunnel "route everything", que anuncia rota pras faixas 10.0.0.0/8 +
// 172.16.0.0/12 + 192.168.0.0/16 inteiras (exatamente onde o Docker tenta alocar por padrão).
// Fix: apontar o Docker pra uma faixa fora do que a VPN cobre — 100.64.0.0/10 (RFC 6598, CGNAT,
// raramente roteada por VPN corporativa).
const DOCKER_ADDRESS_POOL_FIX_SNIPPET = `# O Docker não conseguiu criar a rede padrão porque a VPN corporativa está
# roteando toda a faixa 10.0.0.0/8 + 172.16.0.0/12 + 192.168.0.0/16 — não
# sobra bloco livre nessas faixas privadas clássicas. Fix: usar uma faixa
# fora do que a VPN cobre.
sudo tee /etc/docker/daemon.json > /dev/null <<'JSON'
{
  "default-address-pools": [
    { "base": "100.64.0.0/10", "size": 24 }
  ]
}
JSON
sudo systemctl reset-failed docker.service
sudo systemctl restart docker`;

// Escolhe título + snippet conforme DBDockerStatus.reason — cada causa tem um fix diferente,
// mostrar sempre "instale o Docker" quando ele já está instalado/rodando seria confuso (ver
// db_test_docker.go no backend, mesma classificação).
const DOCKER_FIX_BY_REASON: Record<string, { title: string; snippet: string }> = {
  not_installed: { title: "Docker não está instalado neste servidor", snippet: DOCKER_INSTALL_SNIPPET },
  permission_denied: { title: "Usuário do servidor sem permissão para o Docker", snippet: DOCKER_INSTALL_SNIPPET },
  address_pool_exhausted: { title: "Docker não conseguiu criar a rede padrão (conflito com VPN)", snippet: DOCKER_ADDRESS_POOL_FIX_SNIPPET },
  daemon_unreachable: { title: "Docker instalado, mas o daemon não respondeu", snippet: DOCKER_INSTALL_SNIPPET },
};

// Combobox com busca embutida no mesmo popover — mesmo padrão de KafkaTestTab.tsx/
// ClusterSelectorForTab.tsx (evita o bug do <Select> do Radix fechar o dropdown ao focar um
// campo de busca externo). Local a este arquivo porque é usado só aqui (Namespace/Deployment).
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
          title={value || undefined}
        >
          <span className="truncate">{value || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      {/* min-w garante que o dropdown nunca fique mais estreito que o botão, mas w-auto +
          max-w permite crescer além disso pra caber nomes longos de deployment/namespace/
          database — sem isso o dropdown ficava preso exatamente na largura (às vezes
          pequena) do trigger, cortando nomes que o próprio botão já truncava. */}
      <PopoverContent className="min-w-[--radix-popover-trigger-width] w-auto max-w-[26rem] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  title={opt}
                  onSelect={() => {
                    onChange(opt === value ? "" : opt);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4 shrink-0", value === opt ? "opacity-100" : "opacity-0")} />
                  <span className="truncate">{opt}</span>
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

type StatsSortKey = "name" | "count" | "size_bytes" | "storage_size_bytes";

// BrowseStatsTable é a tabela de estatísticas ordenável — mesmo espírito do "All Stats" do
// MongoDB Compass (Collection/Count/Size/Storage Size), reaproveitada por 3 níveis diferentes:
// tabelas Postgres/MySQL e collections Mongo (Count+Size+StorageSize, via catálogo — ver
// db_test_tool.go groupColumnsToTablesWithStats), bancos lógicos Redis (só Count, via `INFO
// keyspace` — Redis não tem conceito de tamanho por banco) e chaves Redis (Tipo+Size via `MEMORY
// USAGE`, sem Count/StorageSize — uma chave não é um container). As colunas exibidas são
// controladas pelos `show*` — cada nível passa só o que faz sentido pra ele. Objetos sem o campo
// numérico correspondente (ex: falha do $collStats numa view específica) mostram "—" em vez de
// "0 B"/"0".
function BrowseStatsTable({
  objects,
  nameLabel,
  showType = false,
  showCount = true,
  showSize = true,
  showStorageSize = true,
  sortKey,
  sortDir,
  onSort,
}: {
  objects: DBBrowseObject[];
  nameLabel: string;
  showType?: boolean;
  showCount?: boolean;
  showSize?: boolean;
  showStorageSize?: boolean;
  sortKey: StatsSortKey;
  sortDir: "asc" | "desc";
  onSort: (key: StatsSortKey) => void;
}) {
  const sorted = [...objects].sort((a, b) => {
    const dir = sortDir === "asc" ? 1 : -1;
    if (sortKey === "name") return a.name.localeCompare(b.name) * dir;
    return ((a[sortKey] ?? 0) - (b[sortKey] ?? 0)) * dir;
  });

  const sortIcon = (key: StatsSortKey) =>
    sortKey !== key ? (
      <ArrowUpDown className="w-3 h-3 opacity-40" />
    ) : sortDir === "asc" ? (
      <ChevronUp className="w-3 h-3" />
    ) : (
      <ChevronDown className="w-3 h-3" />
    );

  const sortableHeader = (key: StatsSortKey, label: string, align: "left" | "right" = "left") => (
    <TableHead className={align === "right" ? "text-right" : ""}>
      <button
        type="button"
        onClick={() => onSort(key)}
        className={cn(
          "inline-flex items-center gap-1 text-xs font-medium hover:text-foreground",
          align === "right" && "flex-row-reverse",
        )}
      >
        {label}
        {sortIcon(key)}
      </button>
    </TableHead>
  );

  return (
    <div className="max-h-72 overflow-auto border border-border rounded-md">
      <Table>
        <TableHeader className="sticky top-0 bg-background">
          <TableRow>
            {sortableHeader("name", nameLabel)}
            {showType && <TableHead>Tipo</TableHead>}
            {showCount && sortableHeader("count", "Count", "right")}
            {showSize && sortableHeader("size_bytes", "Size", "right")}
            {showStorageSize && sortableHeader("storage_size_bytes", "Storage Size", "right")}
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map((obj, i) => (
            <TableRow key={i}>
              <TableCell className="py-1.5 font-mono text-xs">{obj.name}</TableCell>
              {showType && (
                <TableCell className="py-1.5">
                  {obj.type && <Badge variant="outline" className="text-[9px] px-1 py-0">{obj.type}</Badge>}
                </TableCell>
              )}
              {showCount && (
                <TableCell className="py-1.5 text-right text-xs font-mono">
                  {obj.count !== undefined ? obj.count.toLocaleString("pt-BR") : "—"}
                </TableCell>
              )}
              {showSize && (
                <TableCell className="py-1.5 text-right text-xs font-mono">
                  {obj.size_bytes !== undefined ? formatBytes(obj.size_bytes) : "—"}
                </TableCell>
              )}
              {showStorageSize && (
                <TableCell className="py-1.5 text-right text-xs font-mono">
                  {obj.storage_size_bytes !== undefined ? formatBytes(obj.storage_size_bytes) : "—"}
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

// Deriva os badges de TCP/DNS, Autenticação (só se mode !== "none") e TLS (só se useTLS) a partir
// da classificação única do estágio de conectividade (ver db_test_tool.go).
//
// "unknown_failed" (regex de erro do engine não reconheceu a saída — ex: docker não instalado no
// servidor, connection string malformada, erro de infra antes do cliente rodar) não permite saber
// em qual sub-estágio a falha ocorreu. Sem esse `known` guard, qualquer status diferente de
// "tcp_failed"/"auth_failed" caía no `: "ok"` do fallback — TCP/DNS e Autenticação apareciam com
// check verde mesmo quando o teste falhou de forma não classificada, contradizendo a mensagem de
// erro ao lado. Aqui, todos os sub-estágios viram "skipped" (cinza/desconhecido) nesse caso.
function deriveConnectivityBadges(status: DBStageStatus, authMode: DBAuthMode, useTLS: boolean) {
  const badges: { label: string; status: "ok" | "failed" | "skipped" }[] = [];
  const known = status === "ok" || status === "tcp_failed" || status === "auth_failed" || status === "tls_failed";

  badges.push({ label: "TCP/DNS", status: !known ? "skipped" : status === "tcp_failed" ? "failed" : "ok" });
  if (authMode !== "none") {
    badges.push({
      label: "Autenticação",
      status: !known ? "skipped" : status === "tcp_failed" ? "skipped" : status === "auth_failed" ? "failed" : "ok",
    });
  }
  if (useTLS) {
    badges.push({
      label: "TLS",
      status: !known
        ? "skipped"
        : status === "tcp_failed" || status === "auth_failed"
        ? "skipped"
        : status === "tls_failed"
        ? "failed"
        : "ok",
    });
  }
  return badges;
}

const ENGINE_OPTIONS: { value: DBEngine; label: string; defaultPort: number }[] = [
  { value: "postgres", label: "PostgreSQL", defaultPort: 5432 },
  { value: "mysql", label: "MySQL/MariaDB", defaultPort: 3306 },
  { value: "mongodb", label: "MongoDB", defaultPort: 27017 },
  { value: "redis", label: "Redis", defaultPort: 6379 },
];

export default function DatabaseTestTab() {
  const { clusters } = useClusters();
  const queryClient = useQueryClient();
  // executionMode="pod" (default): ephemeral container anexado a um pod real do Deployment,
  // reflete NetworkPolicy/Istio. "local": roda direto no host do servidor, sem tocar o cluster —
  // útil quando o banco é alcançável direto da rede do servidor (VPN, endpoint público) e não faz
  // sentido simular a identidade de rede de um workload específico. Roda a mesma imagem do engine
  // via Docker local (docker run --rm) — requer Docker instalado no servidor, não os clientes
  // nativos (ver DOCKER_INSTALL_SNIPPET / painel de pré-checagem abaixo).
  const [executionMode, setExecutionMode] = useState<DBExecutionMode>("pod");
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [deployment, setDeployment] = useState("");

  const [engine, setEngine] = useState<DBEngine>("postgres");
  const [host, setHost] = useState("");
  const [port, setPort] = useState(5432);

  const [authMode, setAuthMode] = useState<DBAuthMode>("none");
  const [useTLS, setUseTLS] = useState(false);
  const [skipTLSVerify, setSkipTLSVerify] = useState(false);
  const [database, setDatabase] = useState("");
  const [credSource, setCredSource] = useState<"manual" | "secret">("manual");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [secretNamespace, setSecretNamespace] = useState("");
  const [secretName, setSecretName] = useState("");
  const [usernameKey, setUsernameKey] = useState("username");
  const [passwordKey, setPasswordKey] = useState("password");
  const [secretBase64Decode, setSecretBase64Decode] = useState(false);
  const [connectionString, setConnectionString] = useState("");
  const [csSource, setCsSource] = useState<"manual" | "configmap" | "secret">("manual");
  const [csRefNamespace, setCsRefNamespace] = useState("");
  const [csRefName, setCsRefName] = useState("");
  const [csRefKey, setCsRefKey] = useState("connectionString");

  const [hostSource, setHostSource] = useState<"manual" | "configmap">("manual");
  const [hostConfigMapNamespace, setHostConfigMapNamespace] = useState("");
  const [hostConfigMapName, setHostConfigMapName] = useState("");
  const [hostKey, setHostKey] = useState("host");
  const [portKey, setPortKey] = useState("port");

  const [browseEnabled, setBrowseEnabled] = useState(false);
  // redisKeyPattern filtra o SCAN...MATCH do estágio de navegação — só usado quando engine="redis".
  const [redisKeyPattern, setRedisKeyPattern] = useState("");
  const [timeoutMs, setTimeoutMs] = useState(5000);

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState(0);
  const [phaseMessage, setPhaseMessage] = useState("");
  const [result, setResult] = useState<DBTestResult | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [rawOutputOpen, setRawOutputOpen] = useState(false);
  // Ordenação da tabela de estatísticas (tabelas Postgres/MySQL, collections Mongo) — mesmo
  // espírito do "All Stats" do MongoDB Compass (coluna Size ordenável).
  const [statsSortKey, setStatsSortKey] = useState<"name" | "count" | "size_bytes" | "storage_size_bytes">("storage_size_bytes");
  const [statsSortDir, setStatsSortDir] = useState<"asc" | "desc">("desc");
  const esRef = useRef<EventSource | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-db-test", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  // Pré-checagem de Docker — só relevante no modo "local" (Direto do servidor). staleTime curto:
  // usuário pode instalar o Docker e clicar em "Verificar novamente" segundos depois.
  const { data: dockerStatus } = useQuery({
    queryKey: ["db-test-docker-status"],
    queryFn: () => apiClient.getDBTestDockerStatus(),
    enabled: executionMode === "local",
    staleTime: 15_000,
  });
  const dockerReady = executionMode !== "local" || !!(dockerStatus?.installed && dockerStatus?.daemon_running);

  const { data: deployments = [] } = useQuery({
    queryKey: ["deployments-db-test", cluster, namespace],
    queryFn: () => apiClient.getDeployments(cluster, [namespace]),
    enabled: !!cluster && !!namespace,
  });

  const usingHostConfigMap = authMode !== "connstring" && hostSource === "configmap";
  const usingCredSecret = authMode === "userpass" && credSource === "secret";
  const effectiveSecretNamespace = secretNamespace || namespace;
  const effectiveConfigMapNamespace = hostConfigMapNamespace || namespace;

  // Scan de Secrets/ConfigMaps por nome — mesmo padrão de busca já usado pra cluster/namespace/
  // deployment (SearchableSelect), em vez de digitar o nome do recurso às cegas.
  const { data: secrets = [] } = useQuery({
    queryKey: ["secrets-db-test", cluster, effectiveSecretNamespace],
    queryFn: () => apiClient.getSecrets(cluster, [effectiveSecretNamespace]),
    enabled: !!cluster && !!effectiveSecretNamespace && usingCredSecret,
  });

  const { data: configmaps = [] } = useQuery({
    queryKey: ["configmaps-db-test", cluster, effectiveConfigMapNamespace],
    queryFn: () => apiClient.getConfigMaps(cluster, [effectiveConfigMapNamespace]),
    enabled: !!cluster && !!effectiveConfigMapNamespace && usingHostConfigMap,
  });

  // dataKeys do Secret/ConfigMap selecionado — popula os selects de "qual chave é usuário/senha/
  // host/porta" com os nomes reais em vez do usuário adivinhar/digitar.
  const selectedSecretKeys = secrets.find((s) => s.name === secretName)?.dataKeys ?? [];
  const selectedConfigMapKeys = configmaps.find((cm) => cm.name === hostConfigMapName)?.dataKeys ?? [];

  // Connection string completa lida de um Secret ou ConfigMap (comum ter a credencial já embutida
  // na URI) — mesmo padrão de scan por nome, mas com fonte selecionável entre os dois tipos.
  const effectiveCsNamespace = csRefNamespace || namespace;

  const { data: csSecrets = [] } = useQuery({
    queryKey: ["secrets-db-test-cs", cluster, effectiveCsNamespace],
    queryFn: () => apiClient.getSecrets(cluster, [effectiveCsNamespace]),
    enabled: !!cluster && !!effectiveCsNamespace && authMode === "connstring" && csSource === "secret",
  });

  const { data: csConfigMaps = [] } = useQuery({
    queryKey: ["configmaps-db-test-cs", cluster, effectiveCsNamespace],
    queryFn: () => apiClient.getConfigMaps(cluster, [effectiveCsNamespace]),
    enabled: !!cluster && !!effectiveCsNamespace && authMode === "connstring" && csSource === "configmap",
  });

  const selectedCsKeys =
    csSource === "secret"
      ? csSecrets.find((s) => s.name === csRefName)?.dataKeys ?? []
      : csConfigMaps.find((cm) => cm.name === csRefName)?.dataKeys ?? [];

  // Cluster só é obrigatório no modo "pod" (resolve o Deployment/pod/ephemeral container) ou
  // quando alguma referência de Secret/ConfigMap é usada (precisa da API do K8s pra ler, mesmo
  // que o teste em si rode local). Namespace/Deployment só existem no modo "pod".
  const usesK8sRef = usingHostConfigMap || usingCredSecret || (authMode === "connstring" && csSource !== "manual");
  const needsCluster = executionMode === "pod" || usesK8sRef;

  const canRun =
    (!needsCluster || !!cluster) &&
    (executionMode !== "pod" || (!!namespace && !!deployment)) &&
    dockerReady &&
    !isRunning &&
    (authMode === "connstring"
      ? csSource === "manual"
        ? !!connectionString.trim()
        : !!csRefName.trim()
      : usingHostConfigMap
      ? !!hostConfigMapName.trim()
      : !!host.trim() && port > 0);

  const runTest = async () => {
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando teste de banco de dados...");
    setIsRunning(true);
    try {
      const { session_id } = await apiClient.runDBTest({
        execution_mode: executionMode,
        cluster,
        namespace,
        deployment,
        engine,
        host: authMode === "connstring" || usingHostConfigMap ? "" : host.trim(),
        port: authMode === "connstring" || usingHostConfigMap ? 0 : port,
        ...(usingHostConfigMap
          ? {
              host_configmap_ref: {
                namespace: hostConfigMapNamespace || namespace,
                name: hostConfigMapName,
                host_key: hostKey || "host",
                port_key: portKey || "port",
              },
            }
          : {}),
        auth: {
          mode: authMode,
          use_tls: authMode === "connstring" ? false : useTLS,
          skip_tls_verify: authMode === "connstring" ? false : skipTLSVerify,
          database: authMode === "connstring" ? undefined : database || undefined,
          ...(authMode === "userpass"
            ? credSource === "manual"
              ? { username, password }
              : {
                  secret_ref: {
                    namespace: secretNamespace || namespace,
                    name: secretName,
                    username_key: usernameKey || "username",
                    password_key: passwordKey || "password",
                    base64_decode: secretBase64Decode,
                  },
                }
            : {}),
          ...(authMode === "connstring"
            ? csSource === "manual"
              ? { connection_string: connectionString.trim() }
              : {
                  connstring_ref: {
                    kind: csSource,
                    namespace: csRefNamespace || namespace,
                    name: csRefName,
                    key: csRefKey || "connectionString",
                  },
                }
            : {}),
        },
        browse: browseEnabled,
        ...(engine === "redis" && redisKeyPattern.trim() ? { redis_key_pattern: redisKeyPattern.trim() } : {}),
        timeout_ms: timeoutMs,
      });
      setSessionId(session_id);
    } catch (err) {
      setIsRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o teste");
    }
  };

  const cancelTest = async () => {
    if (!sessionId) return;
    try {
      await apiClient.cancelDBTest(sessionId);
    } catch {
      /* ignore */
    }
    esRef.current?.close();
    setIsRunning(false);
    setPhaseMessage("Teste cancelado.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getDBTestStreamURL(sessionId));
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: DBTestSSEEvent = JSON.parse(e.data);
        setPhaseMessage(event.message);
        setProgress(event.progress);

        if (event.type === "complete" && event.result) {
          setResult(event.result);
          setIsRunning(false);
          // Falha de conectividade: abre a "Saída bruta" automaticamente — do contrário o painel
          // fica colapsado e o usuário não vê o motivo real sem saber que precisa clicar nele.
          if (event.result.connectivity.status !== "ok") {
            setRawOutputOpen(true);
          }
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

  // O nível listado pelo Explorar dados varia por engine E por ter ou não um Database informado
  // (ex: Postgres sem Database lista "database"; com Database, desce um nível pra "table" — ver
  // db_test_tool.go). object_type vem do próprio resultado, não é fixo por engine.
  const browseObjectLabels: Record<string, string> = {
    database: "Databases",
    table: "Tabelas",
    collection: "Collections",
    key: "Chaves",
  };
  const browseObjectLabel = (objectType?: string) => (objectType && browseObjectLabels[objectType]) || "Objetos";

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
              <RadioGroupItem value="pod" id="exec-pod" />
              <label htmlFor="exec-pod" className="text-sm cursor-pointer">Via Pod do Cluster</label>
            </div>
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="local" id="exec-local" />
              <label htmlFor="exec-local" className="text-sm cursor-pointer">Direto do servidor (terminal local)</label>
            </div>
          </RadioGroup>
          {executionMode === "local" && (
            <span className="text-[10px] text-amber-600 dark:text-amber-400">
              Roda a mesma imagem do modo Pod num container Docker local no servidor (`docker run --rm`) — requer Docker instalado lá, não os clientes nativos. Não reflete NetworkPolicy/Istio do cluster.
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
              onClick={() => queryClient.invalidateQueries({ queryKey: ["db-test-docker-status"] })}
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
                tabLabel="Teste de Banco de Dados"
                clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
              />
            </div>
          )}

          {executionMode === "pod" && (
            <>
              <div className="min-w-[260px]">
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

              <div className="min-w-[320px]">
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

          <div className="w-44">
            <label className="text-xs text-muted-foreground block mb-1">Engine</label>
            <Select
              value={engine}
              onValueChange={(v) => {
                const eng = v as DBEngine;
                setEngine(eng);
                setPort(ENGINE_OPTIONS.find((o) => o.value === eng)?.defaultPort ?? 0);
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ENGINE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
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
        <div className="w-full">
          <RadioGroup value={authMode} onValueChange={(v) => setAuthMode(v as DBAuthMode)} className="flex items-center gap-4">
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="none" id="auth-none" />
              <label htmlFor="auth-none" className="text-sm cursor-pointer">Sem autenticação</label>
            </div>
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="userpass" id="auth-userpass" />
              <label htmlFor="auth-userpass" className="text-sm cursor-pointer">Usuário e senha</label>
            </div>
            <div className="flex items-center gap-1.5">
              <RadioGroupItem value="connstring" id="auth-connstring" />
              <label htmlFor="auth-connstring" className="text-sm cursor-pointer">Connection string</label>
            </div>
          </RadioGroup>
        </div>

        {authMode === "connstring" ? (
          <div className="flex flex-wrap items-end gap-3">
            <div className="w-full">
              <RadioGroup value={csSource} onValueChange={(v) => setCsSource(v as typeof csSource)} className="flex items-center gap-4">
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="manual" id="cs-manual" />
                  <label htmlFor="cs-manual" className="text-sm cursor-pointer">Digitar manualmente</label>
                </div>
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="configmap" id="cs-configmap" />
                  <label htmlFor="cs-configmap" className="text-sm cursor-pointer">Ler de um ConfigMap do K8s</label>
                </div>
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="secret" id="cs-secret" />
                  <label htmlFor="cs-secret" className="text-sm cursor-pointer">Ler de um Secret do K8s</label>
                </div>
              </RadioGroup>
            </div>

            {csSource === "manual" ? (
              <div className="w-full max-w-2xl">
                <label className="text-xs text-muted-foreground block mb-1">Connection string completa</label>
                <Input
                  placeholder={
                    engine === "postgres" ? "postgresql://user:pass@host:5432/db?sslmode=require" :
                    engine === "mysql" ? "mysql://user:pass@host:3306/db" :
                    engine === "mongodb" ? "mongodb+srv://user:pass@cluster.mongodb.net/db" :
                    "redis://:pass@host:6379/0"
                  }
                  value={connectionString}
                  onChange={(e) => setConnectionString(e.target.value)}
                />
              </div>
            ) : (
              <>
                <div className="w-44">
                  <label className="text-xs text-muted-foreground block mb-1">
                    Namespace do {csSource === "secret" ? "Secret" : "ConfigMap"}
                  </label>
                  <SearchableSelect
                    value={csRefNamespace}
                    onChange={(v) => { setCsRefNamespace(v); setCsRefName(""); }}
                    options={namespaces.map((ns) => ns.name)}
                    placeholder={namespace || "(mesmo do teste)"}
                    searchPlaceholder="Buscar namespace..."
                    emptyMessage="Nenhum namespace encontrado."
                  />
                </div>
                <div className="w-56">
                  <label className="text-xs text-muted-foreground block mb-1">
                    Nome do {csSource === "secret" ? "Secret" : "ConfigMap"}
                  </label>
                  <SearchableSelect
                    value={csRefName}
                    onChange={setCsRefName}
                    options={csSource === "secret" ? csSecrets.map((r) => r.name) : csConfigMaps.map((r) => r.name)}
                    placeholder={`Selecione o ${csSource === "secret" ? "Secret" : "ConfigMap"}`}
                    searchPlaceholder="Buscar..."
                    emptyMessage="Nenhum recurso encontrado."
                    disabled={!effectiveCsNamespace}
                  />
                </div>
                <div className="w-44">
                  <label className="text-xs text-muted-foreground block mb-1">Chave (connection string)</label>
                  <SearchableSelect
                    value={csRefKey}
                    onChange={setCsRefKey}
                    options={selectedCsKeys}
                    placeholder="connectionString"
                    searchPlaceholder="Buscar chave..."
                    emptyMessage={`Selecione o ${csSource === "secret" ? "Secret" : "ConfigMap"} primeiro.`}
                    disabled={!csRefName}
                  />
                </div>
              </>
            )}
          </div>
        ) : (
          <div className="flex flex-wrap items-end gap-3">
            <div className="w-full">
              <RadioGroup value={hostSource} onValueChange={(v) => setHostSource(v as typeof hostSource)} className="flex items-center gap-4">
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="manual" id="host-manual" />
                  <label htmlFor="host-manual" className="text-sm cursor-pointer">Digitar host/porta manualmente</label>
                </div>
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="configmap" id="host-configmap" />
                  <label htmlFor="host-configmap" className="text-sm cursor-pointer">Ler de um ConfigMap do K8s</label>
                </div>
              </RadioGroup>
            </div>

            {hostSource === "manual" ? (
              <>
                <div className="w-72">
                  <label className="text-xs text-muted-foreground block mb-1">Host</label>
                  <Input placeholder="ex: my-postgres.database.azure.com" value={host} onChange={(e) => setHost(e.target.value)} title={host || undefined} />
                </div>
                <div className="w-28">
                  <label className="text-xs text-muted-foreground block mb-1">Porta</label>
                  <Input type="number" value={port} onChange={(e) => setPort(Number(e.target.value) || 0)} />
                </div>
              </>
            ) : (
              <>
                <div className="min-w-[220px]">
                  <label className="text-xs text-muted-foreground block mb-1">Namespace do ConfigMap</label>
                  <SearchableSelect
                    value={hostConfigMapNamespace}
                    onChange={(v) => { setHostConfigMapNamespace(v); setHostConfigMapName(""); }}
                    options={namespaces.map((ns) => ns.name)}
                    placeholder={namespace || "(mesmo do teste)"}
                    searchPlaceholder="Buscar namespace..."
                    emptyMessage="Nenhum namespace encontrado."
                  />
                </div>
                <div className="min-w-[280px]">
                  <label className="text-xs text-muted-foreground block mb-1">Nome do ConfigMap</label>
                  <SearchableSelect
                    value={hostConfigMapName}
                    onChange={setHostConfigMapName}
                    options={configmaps.map((cm) => cm.name)}
                    placeholder="Selecione o ConfigMap"
                    searchPlaceholder="Buscar ConfigMap..."
                    emptyMessage="Nenhum ConfigMap encontrado."
                    disabled={!effectiveConfigMapNamespace}
                  />
                </div>
                <div className="min-w-[180px]">
                  <label className="text-xs text-muted-foreground block mb-1">Chave host</label>
                  <SearchableSelect
                    value={hostKey}
                    onChange={setHostKey}
                    options={selectedConfigMapKeys}
                    placeholder="host"
                    searchPlaceholder="Buscar chave..."
                    emptyMessage="Selecione o ConfigMap primeiro."
                    disabled={!hostConfigMapName}
                  />
                </div>
                <div className="min-w-[180px]">
                  <label className="text-xs text-muted-foreground block mb-1">Chave porta</label>
                  <SearchableSelect
                    value={portKey}
                    onChange={setPortKey}
                    options={selectedConfigMapKeys}
                    placeholder="port"
                    searchPlaceholder="Buscar chave..."
                    emptyMessage="Selecione o ConfigMap primeiro."
                    disabled={!hostConfigMapName}
                  />
                </div>
              </>
            )}

            <div className="w-72">
              <label className="text-xs text-muted-foreground block mb-1">
                {engine === "redis" ? "Índice do banco (0-15, opcional)" : "Database (opcional)"}
              </label>
              <Input
                type={engine === "redis" ? "number" : "text"}
                min={engine === "redis" ? 0 : undefined}
                max={engine === "redis" ? 15 : undefined}
                placeholder={engine === "redis" ? "0" : undefined}
                value={database}
                onChange={(e) => setDatabase(e.target.value)}
                title={database || undefined}
              />
              {engine !== "redis" && browseEnabled && (
                <p className="text-[10px] text-muted-foreground mt-1">
                  {database.trim()
                    ? `Explorar dados vai listar ${engine === "mongodb" ? "collections" : "tabelas"} de "${database.trim()}".`
                    : `Vazio: Explorar dados lista só os databases. Preencha pra descer e listar ${engine === "mongodb" ? "collections" : "tabelas"}.`}
                </p>
              )}
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

            {authMode === "userpass" && (
              <>
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
                      <Input value={username} onChange={(e) => setUsername(e.target.value)} />
                    </div>
                    <div className="w-56">
                      <label className="text-xs text-muted-foreground block mb-1">Senha</label>
                      <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
                    </div>
                  </>
                ) : (
                  <>
                    <div className="w-44">
                      <label className="text-xs text-muted-foreground block mb-1">Namespace do Secret</label>
                      <SearchableSelect
                        value={secretNamespace}
                        onChange={(v) => { setSecretNamespace(v); setSecretName(""); }}
                        options={namespaces.map((ns) => ns.name)}
                        placeholder={namespace || "(mesmo do teste)"}
                        searchPlaceholder="Buscar namespace..."
                        emptyMessage="Nenhum namespace encontrado."
                      />
                    </div>
                    <div className="w-56">
                      <label className="text-xs text-muted-foreground block mb-1">Nome do Secret</label>
                      <SearchableSelect
                        value={secretName}
                        onChange={setSecretName}
                        options={secrets.map((s) => s.name)}
                        placeholder="Selecione o Secret"
                        searchPlaceholder="Buscar Secret..."
                        emptyMessage="Nenhum Secret encontrado."
                        disabled={!effectiveSecretNamespace}
                      />
                    </div>
                    <div className="w-40">
                      <label className="text-xs text-muted-foreground block mb-1">Chave usuário</label>
                      <SearchableSelect
                        value={usernameKey}
                        onChange={setUsernameKey}
                        options={selectedSecretKeys}
                        placeholder="username"
                        searchPlaceholder="Buscar chave..."
                        emptyMessage="Selecione o Secret primeiro."
                        disabled={!secretName}
                      />
                    </div>
                    <div className="w-40">
                      <label className="text-xs text-muted-foreground block mb-1">Chave senha</label>
                      <SearchableSelect
                        value={passwordKey}
                        onChange={setPasswordKey}
                        options={selectedSecretKeys}
                        placeholder="password"
                        searchPlaceholder="Buscar chave..."
                        emptyMessage="Selecione o Secret primeiro."
                        disabled={!secretName}
                      />
                    </div>
                    <div className="w-full flex items-center gap-2">
                      <Checkbox checked={secretBase64Decode} onCheckedChange={(v) => setSecretBase64Decode(!!v)} id="db-secret-b64" />
                      <label htmlFor="db-secret-b64" className="text-xs text-muted-foreground cursor-pointer max-w-md">
                        Valores no Secret estão em Base64 (decodificar antes de autenticar — comum em secrets sincronizados de Azure Key Vault via external-secrets)
                      </label>
                    </div>
                  </>
                )}
              </>
            )}
          </div>
        )}

        <div className="flex items-center gap-2">
          <Switch checked={browseEnabled} onCheckedChange={setBrowseEnabled} id="browse-toggle" />
          <label htmlFor="browse-toggle" className="text-sm font-medium cursor-pointer">
            Explorar dados (só leitura — lista databases/tabelas/collections/chaves)
          </label>
        </div>

        {browseEnabled && engine === "redis" && (
          <div className="w-64 pl-8">
            <label className="text-xs text-muted-foreground block mb-1">Padrão de chaves (opcional)</label>
            <Input
              placeholder="ex: sessao:*, cache:usuario:*"
              value={redisKeyPattern}
              onChange={(e) => setRedisKeyPattern(e.target.value)}
            />
            <p className="text-[10px] text-muted-foreground mt-1">
              Filtra o SCAN por um padrão glob (MATCH) — vazio lista sem filtro. Redis não indexa
              chaves por nome/schema como os outros engines, só por esse tipo de padrão.
            </p>
          </div>
        )}
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
            <Database className="w-4 h-4" />
            {executionMode === "pod"
              ? 'Selecione cluster, namespace, o Deployment de onde o teste deve partir, o engine e o host/porta (ou uma connection string) e clique em "Executar Teste" — anexa um container efêmero com o cliente do banco (psql/mysql/mongosh/redis-cli) num pod já rodando desse Deployment, refletindo a identidade de rede real dele (NetworkPolicy/Istio avaliados por label/service account do pod).'
              : 'Selecione o engine e o host/porta (ou uma connection string) e clique em "Executar Teste" — roda a mesma imagem do modo Pod num container Docker local no servidor da aplicação, sem tocar o cluster. Útil quando o banco é alcançável direto da rede do servidor; não reflete NetworkPolicy/Istio de nenhum workload específico.'}
          </div>
        )}

        {result && (
          <div className="flex flex-col gap-4">
            <div className="text-xs text-muted-foreground space-y-0.5">
              {result.target_pod ? (
                <>
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
                </>
              ) : (
                <div>Testado direto do servidor da aplicação (container Docker local, sem tocar o cluster).</div>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {deriveConnectivityBadges(result.connectivity.status, authMode, authMode !== "connstring" && useTLS).map((b) => (
                <StageBadge key={b.label} label={b.label} status={b.status} />
              ))}
              {browseEnabled && (
                <StageBadge
                  label="Explorar dados"
                  status={
                    result.browse.status === "ok"
                      ? "ok"
                      : result.browse.status === "skipped"
                      ? "skipped"
                      : "failed"
                  }
                />
              )}
            </div>

            {result.connectivity.status === "auth_failed" ? (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 flex flex-col gap-2">
                <div className="text-sm text-amber-700 dark:text-amber-400">{result.connectivity.message}</div>
                {authMode === "none" && (
                  <Button size="sm" variant="outline" className="w-fit" onClick={() => setAuthMode("userpass")}>
                    Configurar usuário e senha
                  </Button>
                )}
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">{result.connectivity.message}</div>
            )}

            {browseEnabled && (
              <div className="flex flex-col gap-2">
                <div className="text-sm text-muted-foreground">
                  {result.browse.message}
                  {result.browse.object_type && ` (${browseObjectLabel(result.browse.object_type)})`}
                </div>
                {result.browse.truncated && (
                  <div className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
                    <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                    Amostra limitada — pode haver mais {browseObjectLabel(result.browse.object_type).toLowerCase()} além dos listados.
                  </div>
                )}
                {result.browse.objects && result.browse.objects.length > 0 && (() => {
                  // Tabelas/collections (todo engine) e — só pro Redis, que não tem esses dois
                  // níveis nos outros 3 engines — bancos lógicos (Count via INFO keyspace) e
                  // chaves (Tipo+Size via MEMORY USAGE) ganham a tabela ordenável. Postgres/MySQL/
                  // Mongo sem Database informado continuam na lista simples (sem estatística de
                  // tamanho por database, exceto o sizeOnDisk do Mongo já embutido no Detail).
                  const ot = result.browse.object_type;
                  const statsConfig =
                    ot === "table"
                      ? { nameLabel: "Tabela", showType: false, showCount: true, showSize: true, showStorageSize: true }
                      : ot === "collection"
                      ? { nameLabel: "Collection", showType: false, showCount: true, showSize: true, showStorageSize: true }
                      : engine === "redis" && ot === "database"
                      ? { nameLabel: "Banco", showType: false, showCount: true, showSize: false, showStorageSize: false }
                      : engine === "redis" && ot === "key"
                      ? { nameLabel: "Chave", showType: true, showCount: false, showSize: true, showStorageSize: false }
                      : null;

                  if (statsConfig) {
                    return (
                      <BrowseStatsTable
                        objects={result.browse.objects}
                        {...statsConfig}
                        sortKey={statsSortKey}
                        sortDir={statsSortDir}
                        onSort={(key) => {
                          if (key === statsSortKey) {
                            setStatsSortDir((d) => (d === "asc" ? "desc" : "asc"));
                          } else {
                            setStatsSortKey(key);
                            setStatsSortDir("desc");
                          }
                        }}
                      />
                    );
                  }

                  return (
                    <ScrollArea className="max-h-72 border border-border rounded-md">
                      <div className="divide-y divide-border">
                        {result.browse.objects.map((obj, i) => (
                          <div key={i} className="px-2.5 py-1.5 flex items-start gap-2">
                            <span className="text-xs font-mono shrink-0">{obj.name}</span>
                            {obj.type && (
                              <Badge variant="outline" className="text-[9px] px-1 py-0 shrink-0">{obj.type}</Badge>
                            )}
                            {obj.detail && (
                              <span className="text-[10px] font-mono text-muted-foreground truncate min-w-0">{obj.detail}</span>
                            )}
                          </div>
                        ))}
                      </div>
                    </ScrollArea>
                  );
                })()}
              </div>
            )}

            <Collapsible open={rawOutputOpen} onOpenChange={setRawOutputOpen}>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" size="sm" className="w-fit gap-1 text-xs text-muted-foreground">
                  {rawOutputOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  Saída bruta
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1.5 rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap overflow-x-auto">
                  {result.connectivity.raw_output || "(sem saída)"}
                  {browseEnabled && result.browse.raw_output && (
                    <>
                      {"\n\n--- explorar dados ---\n"}
                      {result.browse.raw_output}
                    </>
                  )}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          </div>
        )}
      </div>
    </div>
  );
}
