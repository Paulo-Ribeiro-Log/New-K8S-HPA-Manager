import { useEffect, useRef, useState, type ComponentProps } from "react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
  Server,
  Users,
  MemoryStick,
  Target,
  Clock,
  Eye,
  EyeOff,
  X,
  Zap,
  ArrowDownUp,
  Timer,
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
import type { DBTestResult, DBTestSSEEvent, DBStageStatus, DBEngine, DBAuthMode, DBExecutionMode, DBBrowseObject, DBPreviewResponse, RedisServerInfo, RedisInfoSection, AzureRedisTierInfo } from "@/lib/api/types";
import { DOCKER_FIX_BY_REASON } from "@/lib/dockerFixSnippets";

// ClearableInput envolve o <Input> do shadcn com um botão "Limpar" (X, some quando o campo está
// vazio) e, quando `isPassword`, também um botão de mostrar/ocultar senha (Eye/EyeOff) — usado em
// todos os campos de texto digitados manualmente nesta aba (host, connection string, usuário,
// senha, padrão de chaves, database). `type` é passthrough (ex: "number" pro índice de banco do
// Redis) — `isPassword` sempre tem precedência sobre `type` explícito.
function ClearableInput({
  value,
  onChange,
  isPassword = false,
  type = "text",
  className,
  ...inputProps
}: {
  value: string;
  onChange: (v: string) => void;
  isPassword?: boolean;
} & Omit<ComponentProps<typeof Input>, "value" | "onChange">) {
  const [revealed, setRevealed] = useState(false);
  const effectiveType = isPassword ? (revealed ? "text" : "password") : type;
  return (
    <div className="relative">
      <Input
        {...inputProps}
        type={effectiveType}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={cn(isPassword ? "pr-14" : "pr-7", className)}
      />
      <div className="absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5">
        {isPassword && (
          <button
            type="button"
            tabIndex={-1}
            onClick={() => setRevealed((v) => !v)}
            className="p-1 text-muted-foreground hover:text-foreground rounded"
            title={revealed ? "Ocultar senha" : "Mostrar senha"}
          >
            {revealed ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
          </button>
        )}
        {value && (
          <button
            type="button"
            tabIndex={-1}
            onClick={() => onChange("")}
            className="p-1 text-muted-foreground hover:text-foreground rounded"
            title="Limpar"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

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

// RedisServerInfoCard mostra as estatísticas do próprio servidor Redis (versão, memória,
// clientes conectados, hit rate) — vem junto do nível "database" (topo) da navegação, mesma
// chamada de INFO que já lista os bancos 0-15 (ver parseRedisServerInfo no backend). hit_rate_pct
// == -1 significa "sem dados ainda" (servidor recém-iniciado, hits e misses zerados) — mostrado
// como "—" em vez de "0%", que seria enganoso (parece taxa de acerto ruim, não ausência de dado).
// formatMs formata milissegundos com casas decimais suficientes pra latências sub-milissegundo
// (comuns no Redis, ex: 0.02ms) não virarem "0.0ms" e parecerem zero.
function formatMs(ms: number): string {
  return `${ms.toFixed(ms < 1 ? 3 : 2)}ms`;
}

function RedisServerInfoCard({ info }: { info: RedisServerInfo }) {
  const tiles: { icon: typeof Server; label: string; value: string; title?: string }[] = [
    { icon: Server, label: "Versão", value: info.version ? `${info.version}${info.mode ? ` (${info.mode})` : ""}` : "—" },
    { icon: Users, label: "Clientes conectados", value: String(info.connected_clients) },
    { icon: MemoryStick, label: "Memória usada", value: info.used_memory_human || "—" },
    {
      icon: Target,
      label: "Hit rate",
      value: info.hit_rate_pct < 0 ? "—" : `${info.hit_rate_pct.toFixed(1)}% (${info.keyspace_hits}/${info.keyspace_hits + info.keyspace_misses})`,
    },
    { icon: Zap, label: "Throughput atual", value: `${info.instantaneous_ops_per_sec} ops/s` },
    {
      icon: Timer,
      label: "Latência média",
      value: info.avg_latency_ms < 0 ? "—" : formatMs(info.avg_latency_ms),
      title: info.avg_latency_ms < 0
        ? undefined
        : info.slowest_command
        ? `Latência real (soma de usec / soma de calls de todos os comandos). Comando mais lento: ${info.slowest_command} (${formatMs(info.slowest_command_latency_ms)}/chamada)`
        : "Latência real (soma de usec / soma de calls de todos os comandos)",
    },
    {
      icon: ArrowDownUp,
      label: "Leitura / Escrita",
      value: info.read_pct < 0 ? "—" : `${info.read_pct.toFixed(1)}% / ${info.write_pct.toFixed(1)}%`,
      title: info.read_pct < 0
        ? undefined
        : `${info.total_reads_processed} leituras / ${info.total_writes_processed} escritas processadas (acumulado desde o start do servidor)`,
    },
    { icon: Clock, label: "Uptime", value: `${info.uptime_days}d` },
  ];
  return (
    <div className="rounded-md border border-border bg-muted/20 p-3">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-2">
        {info.role && <Badge variant="outline" className="text-[9px] px-1 py-0">{info.role}</Badge>}
        Estatísticas do servidor
      </div>
      <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-8 gap-3">
        {tiles.map(({ icon: Icon, label, value, title }) => (
          <div key={label} className="flex items-start gap-1.5 min-w-0">
            <Icon className="w-3.5 h-3.5 shrink-0 mt-0.5 text-muted-foreground" />
            <div className="min-w-0">
              <div className="text-[10px] text-muted-foreground truncate">{label}</div>
              <div className="text-xs font-mono truncate" title={title ?? value}>{value}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// REDIS_INFO_TAB_GROUPS agrupa as seções de `redis-cli INFO` por assunto — cada aba junta seções
// relacionadas (ex: Server+Clients+CPU+Memory numa aba só, "Servidor") em vez de uma aba por
// seção, economizando espaço quando o modal precisa mostrar as ~10 seções do INFO de uma vez.
// Nomes em minúsculo pra casar com section.name (que vem exatamente como o cabeçalho "# Nome" do
// redis-cli) via comparação case-insensitive.
const REDIS_INFO_TAB_GROUPS: { label: string; sectionNames: string[] }[] = [
  { label: "Servidor", sectionNames: ["server", "clients", "cpu", "memory"] },
  { label: "Persistência", sectionNames: ["persistence"] },
  { label: "Estatísticas", sectionNames: ["stats", "commandstats", "latencystats", "errorstats"] },
  { label: "Replicação", sectionNames: ["replication"] },
  { label: "Cluster", sectionNames: ["cluster"] },
  { label: "Keyspace", sectionNames: ["keyspace"] },
];

const AZURE_TIER_TAB_LABEL = "Tier (Azure)";

// AzureTierTabPanel mostra o tier/SKU do Azure Cache for Redis dentro da aba dedicada de
// RedisInfoTabs — mesmo conteúdo que antes ficava sempre visível acima da saída bruta, agora só
// aparece quando o usuário abre essa aba (evita competir por espaço com as seções do INFO).
function AzureTierTabPanel({ info }: { info: AzureRedisTierInfo }) {
  if (!info.found) {
    return <div className="text-xs text-amber-700 dark:text-amber-400">{info.error}</div>;
  }
  const rows: { label: string; value?: string | number }[] = [
    { label: "Tier", value: info.tier_label },
    { label: "Subscription", value: info.subscription },
    { label: "Resource Group", value: info.resource_group },
    { label: "Região", value: info.location },
    { label: "Shards", value: info.shard_count || undefined },
    { label: "Versão Redis", value: info.redis_version },
  ];
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1.5 font-mono text-xs">
      {rows
        .filter((r) => r.value !== undefined && r.value !== "")
        .map((r) => (
          <div key={r.label} className="flex gap-1.5 min-w-0">
            <span className="text-muted-foreground shrink-0">{r.label}:</span>
            <span className="truncate" title={String(r.value)}>{r.value}</span>
          </div>
        ))}
    </div>
  );
}

// RedisInfoTabs formata a saída de `redis-cli INFO` (já parseada em seções pelo backend, ver
// RedisInfoSection) como abas agrupadas por assunto em vez de um texto corrido — pedido depois
// que a versão anterior (Dialog com só um <pre> de texto puro) ficou difícil de vasculhar pra
// achar um campo específico. `azureTier` opcional adiciona a aba de tier — só quando a busca foi
// tentada (found ou error preenchido; ausência de ambos significa "nem parece Azure", omite a
// aba).
function RedisInfoTabs({
  sections,
  azureTier,
}: {
  sections: RedisInfoSection[];
  azureTier?: AzureRedisTierInfo;
}) {
  const groups = REDIS_INFO_TAB_GROUPS.map((g) => ({
    label: g.label,
    sections: sections.filter((s) => g.sectionNames.includes(s.name.toLowerCase())),
  })).filter((g) => g.sections.length > 0);

  const showTierTab = !!azureTier && (azureTier.found || !!azureTier.error);
  const tabLabels = [...groups.map((g) => g.label), ...(showTierTab ? [AZURE_TIER_TAB_LABEL] : [])];

  const [activeTab, setActiveTab] = useState<string>("");
  const effectiveTab = tabLabels.includes(activeTab) ? activeTab : tabLabels[0];

  if (tabLabels.length === 0) return null;

  return (
    <div className="flex-1 min-h-0 flex flex-col border border-border rounded-md">
      <div className="flex border-b border-border gap-1 px-2 flex-wrap flex-shrink-0">
        {tabLabels.map((label) => (
          <button
            key={label}
            type="button"
            onClick={() => setActiveTab(label)}
            className={cn(
              "px-2.5 py-1.5 text-xs font-medium transition-colors border-b-2 -mb-px",
              effectiveTab === label ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto p-3">
        {effectiveTab === AZURE_TIER_TAB_LABEL && azureTier ? (
          <AzureTierTabPanel info={azureTier} />
        ) : (
          groups
            .find((g) => g.label === effectiveTab)
            ?.sections.map((section) => (
              <div key={section.name} className="mb-4 last:mb-0">
                <div className="text-xs font-semibold text-muted-foreground mb-1.5"># {section.name}</div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1 font-mono text-xs">
                  {section.fields.map((f) => (
                    <div key={f.key} className="flex gap-1.5 min-w-0">
                      <span className="text-muted-foreground shrink-0">{f.key}:</span>
                      <span className="truncate" title={f.value}>{f.value}</span>
                    </div>
                  ))}
                </div>
              </div>
            ))
        )}
      </div>
    </div>
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
  onRowClick,
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
  // onRowClick, quando presente, abre a amostra de dados reais daquele objeto (ver
  // PreviewModal) — só faz sentido em níveis "folha" (tabela/collection/chave), nunca no nível
  // "database" (não dá pra rodar SELECT/find num banco inteiro).
  onRowClick?: (name: string) => void;
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
            <TableRow
              key={i}
              className={onRowClick ? "cursor-pointer hover:bg-muted/50" : undefined}
              onClick={onRowClick ? () => onRowClick(obj.name) : undefined}
              title={onRowClick ? `Ver amostra de dados de "${obj.name}"` : undefined}
            >
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

// parseRedisCliLikeString reconhece dois formatos que NÃO são URI (por isso rejeitados pelo
// backend, ver isValidRedisConnString/redisConnStringHint em db_test_tool.go), mas que usuários
// colam no campo "Connection string" esperando que funcionem — ambos vêm do jeito que a própria
// Azure Portal descreve o Redis Cache pra teste manual, sem usuário (só Access Key), diferente dos
// outros 3 engines:
//   1. O comando redis-cli inteiro: `redis-cli -p 10000 -h host -a "chave" --tls`
//   2. O atalho "host:porta --tls" (sem prefixo `redis-cli`, só os dados de conexão)
// Retorna null quando não reconhece nenhum dos dois formatos — nesse caso a UI não mostra o botão
// de preenchimento automático, só a mensagem genérica de connection string inválida.
function parseRedisCliLikeString(raw: string): { host: string; port: number; password?: string; tls: boolean } | null {
  const trimmed = raw.trim();
  if (!trimmed || /^rediss?:\/\//i.test(trimmed)) return null;

  const pick = (m: RegExpMatchArray | null) => (m ? m[2] ?? m[3] ?? m[4] : undefined);
  const quotedOrBare = '("([^"]+)"|\'([^\']+)\'|(\\S+))';

  const hostMatch = trimmed.match(new RegExp(`-h\\s+${quotedOrBare}`));
  const portMatch = trimmed.match(new RegExp(`(?:-p|--port)\\s+${quotedOrBare}`));
  const passMatch = trimmed.match(new RegExp(`-a\\s+${quotedOrBare}`));

  let host = pick(hostMatch);
  let portStr = pick(portMatch);

  if (!host) {
    // Atalho "host:porta" — sem flags de redis-cli, só os dados de conexão + --tls opcional.
    const shortMatch = trimmed.match(/^([a-zA-Z0-9.-]+):(\d+)\b/);
    if (shortMatch) {
      host = shortMatch[1];
      portStr = shortMatch[2];
    }
  }

  if (!host || !portStr) return null;
  const port = parseInt(portStr, 10);
  if (!Number.isFinite(port) || port <= 0) return null;

  return { host, port, password: pick(passMatch), tls: /(^|\s)--tls(\s|$)/.test(trimmed) };
}

const ENGINE_OPTIONS: { value: DBEngine; label: string; defaultPort: number }[] = [
  { value: "postgres", label: "PostgreSQL", defaultPort: 5432 },
  { value: "mysql", label: "MySQL/MariaDB", defaultPort: 3306 },
  { value: "mongodb", label: "MongoDB", defaultPort: 27017 },
  { value: "redis", label: "Redis", defaultPort: 6379 },
  { value: "sqlserver", label: "SQL Server (Azure SQL)", defaultPort: 1433 },
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
  // authMechanism só existe pro Mongo em modo "userpass" — embutido na URI construída pelo
  // backend (authMechanism=...). Vazio deixa o mongosh negociar automaticamente (padrão de
  // sempre); alguns MongoDB gerenciados/mais antigos exigem escolher explicitamente.
  const [authMechanism, setAuthMechanism] = useState<"" | "SCRAM-SHA-1" | "SCRAM-SHA-256">("");
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
  const [showRawFallback, setShowRawFallback] = useState(false);
  // Ordenação da tabela de estatísticas (tabelas Postgres/MySQL, collections Mongo) — mesmo
  // espírito do "All Stats" do MongoDB Compass (coluna Size ordenável).
  const [statsSortKey, setStatsSortKey] = useState<"name" | "count" | "size_bytes" | "storage_size_bytes">("storage_size_bytes");
  const [statsSortDir, setStatsSortDir] = useState<"asc" | "desc">("desc");

  // Amostra de dados reais (Preview) — clicar numa linha da tabela de estatísticas (tabela/
  // collection/chave) abre uma consulta pontual, síncrona, só leitura (SELECT/find/GET com
  // limite) contra aquele objeto específico. Ação à parte do teste principal, mesmo padrão da
  // Visão geral de tópicos do Teste de Kafka.
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewData, setPreviewData] = useState<DBPreviewResponse | null>(null);
  const [previewObject, setPreviewObject] = useState("");
  // previewSelectedIndex controla qual linha/documento está aberto no painel de detalhes
  // (layout mestre-detalhe: lista compacta à esquerda, conteúdo completo sem scroll horizontal
  // à direita — evita a tabela larga forçar scroll lateral e quebrar o foco de leitura,
  // principalmente com documentos Mongo de _id composto/aninhado).
  const [previewSelectedIndex, setPreviewSelectedIndex] = useState(0);
  // Paginação + ordenação — offset 0-based (LIMIT/OFFSET ou skip/limit no backend); sortColumn
  // vazio = ordem "natural" do banco (não garantida entre páginas, mas é o padrão até o usuário
  // escolher uma coluna). Resetados pra 0/vazio sempre que um objeto NOVO é aberto (paginação de
  // uma tabela/collection diferente não faz sentido continuar de onde a anterior parou).
  const [previewOffset, setPreviewOffset] = useState(0);
  const [previewSortColumn, setPreviewSortColumn] = useState("");
  const [previewSortDir, setPreviewSortDir] = useState<"asc" | "desc">("asc");
  const previewPageSize = 20;

  // fetchPreviewPage é a chamada de API compartilhada — abrir um objeto novo (openPreview),
  // trocar de página e mudar a ordenação todos passam pelos mesmos parâmetros de conexão, só
  // variando object/offset/sortColumn/sortDir.
  const fetchPreviewPage = async (objectName: string, offset: number, sortColumn: string, sortDir: "asc" | "desc") => {
    setPreviewLoading(true);
    setPreviewError(null);
    try {
      const data = await apiClient.previewDBTestObject({
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
          database: database || undefined,
          ...(engine === "mongodb" && authMode === "userpass" && authMechanism ? { auth_mechanism: authMechanism } : {}),
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
        browse: true,
        database: database || "",
        object: objectName,
        limit: previewPageSize,
        offset,
        sort_column: sortColumn || undefined,
        sort_dir: sortDir,
        timeout_ms: timeoutMs,
      });
      setPreviewData(data);
      setPreviewSelectedIndex(0);
      if (data.status === "failed") {
        setPreviewError(data.message || "Falha ao buscar amostra de dados");
      }
    } catch (err) {
      setPreviewError(err instanceof Error ? err.message : "Falha ao buscar amostra de dados");
    } finally {
      setPreviewLoading(false);
    }
  };

  const openPreview = (objectName: string) => {
    setPreviewObject(objectName);
    setPreviewOpen(true);
    setPreviewOffset(0);
    setPreviewSortColumn("");
    setPreviewSortDir("asc");
    setPreviewData(null);
    fetchPreviewPage(objectName, 0, "", "asc");
  };

  const previewCanGoPrev = previewOffset > 0 && !previewLoading;
  const previewCanGoNext = !!previewData?.has_more && !previewLoading;

  const previewGoToPage = (direction: "prev" | "next") => {
    const newOffset = direction === "next" ? previewOffset + previewPageSize : Math.max(0, previewOffset - previewPageSize);
    if (direction === "next" && !previewCanGoNext) return;
    if (direction === "prev" && !previewCanGoPrev) return;
    setPreviewOffset(newOffset);
    fetchPreviewPage(previewObject, newOffset, previewSortColumn, previewSortDir);
  };

  const previewChangeSort = (column: string) => {
    setPreviewSortColumn(column);
    setPreviewOffset(0);
    fetchPreviewPage(previewObject, 0, column, previewSortDir);
  };

  const previewToggleSortDir = () => {
    const newDir = previewSortDir === "asc" ? "desc" : "asc";
    setPreviewSortDir(newDir);
    setPreviewOffset(0);
    fetchPreviewPage(previewObject, 0, previewSortColumn, newDir);
  };

  // Navegação por teclado dentro do modal de amostra: ↑/↓ trocam o documento/linha selecionado
  // na lista à esquerda, ←/→ trocam de página. Só ativo com o modal aberto; ignora quando o foco
  // está num campo de texto/select (evita capturar as setas de um Select nativo aberto, por
  // exemplo). preventDefault nas 4 teclas evita rolar a página de fundo.
  useEffect(() => {
    if (!previewOpen) return;
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target?.isContentEditable) return;
      switch (e.key) {
        case "ArrowDown":
          if (!previewData?.rows || previewData.rows.length === 0) return;
          e.preventDefault();
          setPreviewSelectedIndex((i) => Math.min(i + 1, previewData.rows!.length - 1));
          break;
        case "ArrowUp":
          if (!previewData?.rows || previewData.rows.length === 0) return;
          e.preventDefault();
          setPreviewSelectedIndex((i) => Math.max(i - 1, 0));
          break;
        // ArrowRight/ArrowLeft (paginação) não dependem de `rows` estruturado — funcionam também
        // no fallback de texto puro do Redis (list/zset paginam de verdade mesmo sem tabela).
        case "ArrowRight":
          e.preventDefault();
          previewGoToPage("next");
          break;
        case "ArrowLeft":
          e.preventDefault();
          previewGoToPage("prev");
          break;
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [previewOpen, previewData, previewOffset, previewSortColumn, previewSortDir, previewObject]);

  // Colunas da tabela de amostra = união de todas as chaves de todas as linhas, na ordem de
  // primeira aparição — Mongo pode ter documentos com campos diferentes entre si (schema-less),
  // Postgres/MySQL sempre têm as mesmas colunas em todas as linhas.
  const previewColumns = (() => {
    const cols: string[] = [];
    const seen = new Set<string>();
    for (const row of previewData?.rows ?? []) {
      for (const key of Object.keys(row)) {
        if (!seen.has(key)) {
          seen.add(key);
          cols.push(key);
        }
      }
    }
    return cols;
  })();

  const formatPreviewCell = (value: unknown): string => {
    if (value === null || value === undefined) return "";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
  };

  // Rótulo compacto da lista à esquerda do painel mestre-detalhe — usa _id quando existe (Mongo),
  // senão a primeira coluna, senão só o número da linha. Trunca pra nunca forçar quebra/scroll
  // horizontal na lista (o valor completo aparece sempre no painel de detalhes à direita).
  const previewRowLabel = (row: Record<string, unknown>, index: number): string => {
    const raw = "_id" in row ? formatPreviewCell(row["_id"]) : previewColumns[0] ? formatPreviewCell(row[previewColumns[0]]) : "";
    const trimmed = raw.trim();
    if (!trimmed) return `Linha ${index + 1}`;
    return trimmed.length > 48 ? trimmed.slice(0, 48) + "…" : trimmed;
  };
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

  // Tier/SKU do Azure Cache for Redis — só faz sentido buscar quando o modal de saída bruta está
  // aberto (lazy, evita chamada a cada digitação de host) pro engine Redis com host preenchido
  // (modo connstring não expõe um campo de host separado — ver DatabaseTestTab.tsx). staleTime
  // igual ao TTL de cache do backend (30min): tier de um cache Azure raramente muda.
  const { data: azureRedisTier } = useQuery({
    queryKey: ["db-test-redis-azure-tier", host],
    queryFn: () => apiClient.getRedisAzureTier(host),
    enabled: rawOutputOpen && engine === "redis" && authMode !== "connstring" && !!host.trim(),
    staleTime: 30 * 60_000,
  });

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
          // database controla o nível do Explorar dados (lista bancos vs. desce pra tabelas/
          // collections) independente do modo de auth — inclusive em "connstring": o Mongo, por
          // exemplo, usa isso via getSiblingDB(nome) pra trocar de banco DENTRO do mesmo cluster/
          // replica set autenticado pela connection string (comum quando o connstring só tem
          // authSource=admin, sem apontar pra um banco de trabalho específico).
          database: database || undefined,
          ...(engine === "mongodb" && authMode === "userpass" && authMechanism
            ? { auth_mechanism: authMechanism }
            : {}),
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
                <ClearableInput
                  placeholder={
                    engine === "postgres" ? "postgresql://user:pass@host:5432/db?sslmode=require" :
                    engine === "mysql" ? "mysql://user:pass@host:3306/db" :
                    engine === "mongodb" ? "mongodb+srv://user:pass@cluster.mongodb.net/db" :
                    engine === "sqlserver" ? "sqlserver://user:pass@host:1433/db" :
                    "redis://:pass@host:6379/0"
                  }
                  value={connectionString}
                  onChange={setConnectionString}
                />
                {engine === "redis" && (() => {
                  const parsed = parseRedisCliLikeString(connectionString);
                  if (!parsed) return null;
                  return (
                    <div className="mt-1.5 flex items-center gap-2 text-xs text-amber-700 dark:text-amber-400 flex-wrap">
                      <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                      <span>
                        Isso parece um comando redis-cli (host/porta/senha/TLS), não uma connection string —
                        o Redis Cache da Azure (e a maioria dos Redis self-hosted) não usa usuário, só host+porta+senha.
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-6 px-2 text-xs shrink-0"
                        onClick={() => {
                          setAuthMode("userpass");
                          setCredSource("manual");
                          setHostSource("manual");
                          setHost(parsed.host);
                          setPort(parsed.port);
                          setUsername("");
                          setPassword(parsed.password ?? "");
                          setUseTLS(parsed.tls);
                          setConnectionString("");
                        }}
                      >
                        Preencher campos automaticamente
                      </Button>
                    </div>
                  );
                })()}
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
                  <ClearableInput placeholder="ex: my-postgres.database.azure.com" value={host} onChange={setHost} title={host || undefined} />
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

                {engine === "mongodb" && (
                  <div className="w-56">
                    <label className="text-xs text-muted-foreground block mb-1">Mecanismo de autenticação</label>
                    <Select value={authMechanism || "auto"} onValueChange={(v) => setAuthMechanism(v === "auto" ? "" : (v as "SCRAM-SHA-1" | "SCRAM-SHA-256"))}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">Automático (padrão)</SelectItem>
                        <SelectItem value="SCRAM-SHA-1">SCRAM-SHA-1</SelectItem>
                        <SelectItem value="SCRAM-SHA-256">SCRAM-SHA-256</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                )}

                {credSource === "manual" ? (
                  <>
                    <div className="w-56">
                      <label className="text-xs text-muted-foreground block mb-1">
                        Usuário{engine === "redis" && " (opcional)"}
                      </label>
                      <ClearableInput
                        value={username}
                        onChange={setUsername}
                        placeholder={engine === "redis" ? "deixe em branco pra Access Key" : undefined}
                      />
                      {engine === "redis" && (
                        <p className="text-[10px] text-muted-foreground mt-1 max-w-56">
                          Redis não tem usuário (exceto ACL do Redis 6+) — deixe em branco e preencha só a senha
                          (Access Key do Azure Cache, AUTH de outros providers, etc.).
                        </p>
                      )}
                    </div>
                    <div className="w-56">
                      <label className="text-xs text-muted-foreground block mb-1">
                        {engine === "redis" ? "Senha / Access Key" : "Senha"}
                      </label>
                      <ClearableInput isPassword value={password} onChange={setPassword} />
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

        {/* Fora dos dois branches de auth (connstring vs host/port) de propósito: o nível do
            Explorar dados depende disso independente de como a conexão é feita. Se a connection
            string já tiver um banco no path, o backend usa ele automaticamente quando este campo
            fica vazio (ver resolveDBEffectiveDatabase) — preencher aqui só é necessário pra
            sobrescrever esse banco ou quando a connection string não tem nenhum (comum em
            replica sets Mongo autenticados só com authSource=admin). */}
        <div className="w-72">
          <label className="text-xs text-muted-foreground block mb-1">
            {engine === "redis" ? "Índice do banco (0-15, opcional)" : "Database (opcional)"}
          </label>
          <ClearableInput
            type={engine === "redis" ? "number" : "text"}
            min={engine === "redis" ? 0 : undefined}
            max={engine === "redis" ? 15 : undefined}
            placeholder={engine === "redis" ? "0" : undefined}
            value={database}
            onChange={setDatabase}
            title={database || undefined}
          />
          {engine !== "redis" && browseEnabled && (
            <p className="text-[10px] text-muted-foreground mt-1">
              {database.trim()
                ? `Explorar dados vai listar ${engine === "mongodb" ? "collections" : "tabelas"} de "${database.trim()}".`
                : authMode === "connstring"
                ? `Vazio: usa o banco já embutido na connection string, se houver. Preencha só se quiser sobrescrever ou a connection string não tiver um banco.`
                : `Vazio: Explorar dados lista só os databases. Preencha pra descer e listar ${engine === "mongodb" ? "collections" : "tabelas"}.`}
            </p>
          )}
        </div>

        <div className="flex items-center gap-2">
          <Switch checked={browseEnabled} onCheckedChange={setBrowseEnabled} id="browse-toggle" />
          <label htmlFor="browse-toggle" className="text-sm font-medium cursor-pointer">
            Explorar dados (só leitura — lista databases/tabelas/collections/chaves)
          </label>
        </div>

        {browseEnabled && engine === "redis" && (
          <div className="w-64 pl-8">
            <label className="text-xs text-muted-foreground block mb-1">Padrão de chaves (opcional)</label>
            <ClearableInput
              placeholder="ex: sessao:*, cache:usuario:*"
              value={redisKeyPattern}
              onChange={setRedisKeyPattern}
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
                {engine === "redis" && result.browse.redis_server_info && (
                  <RedisServerInfoCard info={result.browse.redis_server_info} />
                )}
                {result.browse.database && (
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
                    <Database className="w-3.5 h-3.5 shrink-0" />
                    <span>{result.browse.database}</span>
                    <ChevronRight className="w-3 h-3 shrink-0 opacity-50" />
                    <span>{browseObjectLabel(result.browse.object_type)}</span>
                  </div>
                )}
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
                    // Preview (amostra de dados reais) só faz sentido em níveis "folha" — uma
                    // tabela/collection ou, no Redis, uma chave específica. Nunca no nível
                    // "database" do Redis (INFO keyspace não aponta pra um objeto navegável).
                    const canPreview = ot === "table" || ot === "collection" || (engine === "redis" && ot === "key");
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
                        onRowClick={canPreview ? openPreview : undefined}
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

            <Button
              variant="ghost"
              size="sm"
              className="w-fit gap-1 text-xs text-muted-foreground"
              onClick={() => setRawOutputOpen(true)}
            >
              <ChevronRight className="h-3 w-3" />
              Ver saída bruta
            </Button>

            <Dialog open={rawOutputOpen} onOpenChange={setRawOutputOpen}>
              <DialogContent className="max-w-3xl h-[80vh] flex flex-col overflow-hidden">
                <DialogHeader>
                  <DialogTitle>Saída bruta</DialogTitle>
                </DialogHeader>

                {engine === "redis" && browseEnabled && result.browse.redis_info_sections?.length ? (
                  <>
                    <RedisInfoTabs
                      sections={result.browse.redis_info_sections}
                      azureTier={authMode !== "connstring" && host.trim() ? azureRedisTier : undefined}
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      className="w-fit gap-1 text-xs text-muted-foreground shrink-0"
                      onClick={() => setShowRawFallback((v) => !v)}
                    >
                      {showRawFallback ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      Ver texto bruto original (não formatado)
                    </Button>
                    {showRawFallback && (
                      <pre className="max-h-40 overflow-y-auto rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap shrink-0">
                        {result.connectivity.raw_output || "(sem saída)"}
                        {result.browse.raw_output && (
                          <>
                            {"\n\n--- explorar dados ---\n"}
                            {result.browse.raw_output}
                          </>
                        )}
                      </pre>
                    )}
                  </>
                ) : (
                  <pre className="flex-1 min-h-0 overflow-y-auto rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap">
                    {result.connectivity.raw_output || "(sem saída)"}
                    {browseEnabled && result.browse.raw_output && (
                      <>
                        {"\n\n--- explorar dados ---\n"}
                        {result.browse.raw_output}
                      </>
                    )}
                  </pre>
                )}
              </DialogContent>
            </Dialog>
          </div>
        )}
      </div>

      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className="max-w-[57.6rem] max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm flex items-center gap-1.5">
              {result?.browse.database && (
                <>
                  <span className="text-muted-foreground">{result.browse.database}</span>
                  <ChevronRight className="w-3 h-3 shrink-0 opacity-50" />
                </>
              )}
              {previewObject}
            </DialogTitle>
          </DialogHeader>
          {previewLoading ? (
            <div className="flex-1 flex items-center justify-center gap-2 text-sm text-muted-foreground py-10">
              <Loader2 className="w-4 h-4 animate-spin" />
              Buscando amostra de dados...
            </div>
          ) : previewError && (!previewData || !previewData.rows?.length) && !previewData?.raw_output ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 text-sm text-muted-foreground py-10">
              <AlertTriangle className="w-5 h-5" />
              {previewError}
              <Button size="sm" variant="outline" onClick={() => openPreview(previewObject)}>Tentar de novo</Button>
            </div>
          ) : previewData ? (
            <div className="flex-1 min-h-0 flex flex-col gap-2 overflow-hidden">
              <div className="text-xs text-muted-foreground shrink-0">{previewData.message}</div>
              {previewData.rows !== undefined && (
                <div className="flex items-center justify-between gap-2 shrink-0 flex-wrap">
                  <div className="flex items-center gap-1.5">
                    <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Ordenar por</span>
                    <Select value={previewSortColumn || "__none__"} onValueChange={(v) => previewChangeSort(v === "__none__" ? "" : v)}>
                      <SelectTrigger className="h-7 w-40 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__">(sem ordenação)</SelectItem>
                        {previewColumns.map((col) => (
                          <SelectItem key={col} value={col}>{col}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 px-2 gap-1 text-xs"
                      disabled={!previewSortColumn}
                      onClick={previewToggleSortDir}
                      title={previewSortDir === "asc" ? "Crescente" : "Decrescente"}
                    >
                      {previewSortDir === "asc" ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                      {previewSortDir === "asc" ? "Asc" : "Desc"}
                    </Button>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" disabled={!previewCanGoPrev} onClick={() => previewGoToPage("prev")} title="Página anterior (←)">
                      <ChevronRight className="w-3 h-3 rotate-180" />
                      Anterior
                    </Button>
                    <span className="text-[10px] text-muted-foreground font-mono">Pág. {Math.floor(previewOffset / previewPageSize) + 1}</span>
                    <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" disabled={!previewCanGoNext} onClick={() => previewGoToPage("next")} title="Próxima página (→)">
                      Próxima
                      <ChevronRight className="w-3 h-3" />
                    </Button>
                  </div>
                  <span className="text-[10px] text-muted-foreground w-full text-right">
                    Navegação: ↑↓ troca de linha, ←→ troca de página
                  </span>
                </div>
              )}
              {previewData.rows && previewData.rows.length > 0 ? (() => {
                const selected = previewData.rows[Math.min(previewSelectedIndex, previewData.rows.length - 1)];
                return (
                  <div className="flex-1 min-h-0 flex gap-2 overflow-hidden">
                    {/* Lista à esquerda — compacta, um rótulo por linha/documento, nunca quebra
                        nem faz scroll horizontal (valores longos são truncados com "…"). */}
                    <div className="w-48 shrink-0 min-h-0 overflow-y-auto border border-border rounded-md">
                      {previewData.rows.map((row, i) => (
                        <button
                          key={i}
                          type="button"
                          onClick={() => setPreviewSelectedIndex(i)}
                          className={cn(
                            "w-full text-left px-2 py-1.5 text-xs font-mono border-b border-border/50 truncate block",
                            i === previewSelectedIndex ? "bg-primary/10 text-foreground" : "hover:bg-muted/50 text-muted-foreground",
                          )}
                          title={previewRowLabel(row, i)}
                        >
                          {previewRowLabel(row, i)}
                        </button>
                      ))}
                    </div>
                    {/* Detalhes à direita — todos os campos da linha/documento selecionado,
                        empilhados verticalmente com quebra de linha (whitespace-pre-wrap
                        break-words em rótulo E valor) em vez de colunas lado a lado — elimina o
                        scroll horizontal que a tabela larga forçava e garante que campos longos
                        (ex: _class, descricaoLinha) fiquem completamente visíveis. overflow-y-
                        auto direto (em vez do ScrollArea do shadcn) — mais previsível dentro
                        dessa cadeia de flex aninhado. */}
                    <div className="flex-1 min-h-0 flex flex-col border border-border rounded-md overflow-hidden">
                      <div className="flex items-center justify-between px-2 py-1 border-b border-border shrink-0">
                        <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Detalhes</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 gap-1 text-xs"
                          onClick={() => {
                            navigator.clipboard.writeText(JSON.stringify(selected, null, 2));
                            toast.success("Dados copiados!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                          Copiar
                        </Button>
                      </div>
                      <div className="flex-1 min-h-0 overflow-y-auto">
                        <div className="p-3 flex flex-col gap-2.5">
                          {previewColumns.map((col) => (
                            <div key={col} className="flex flex-col gap-0.5">
                              <span className="text-[10px] uppercase tracking-wide text-muted-foreground font-medium break-words">{col}</span>
                              <span className="text-xs font-mono whitespace-pre-wrap break-words">
                                {formatPreviewCell(selected[col]) || "—"}
                              </span>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })() : previewData.raw_output ? (
                <div className="flex-1 min-h-0 flex flex-col gap-2 overflow-hidden">
                  {/* Redis: sem tabela estruturada (tipos de chave incompatíveis entre si), mas
                      list/zset têm paginação real por índice (LRANGE/ZRANGE) — mesmos botões
                      Anterior/Próxima do modo tabela, sem o seletor de ordenação (Redis não tem
                      conceito de coluna). string/hash/set/stream sempre trazem tudo de uma vez,
                      então "Próxima" nunca habilita pra esses tipos (has_more calculado no
                      backend por tipo, ver parseRedisPreviewMeta). */}
                  <div className="flex items-center justify-between gap-2 shrink-0">
                    <div className="flex items-center gap-1.5">
                      <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" disabled={!previewCanGoPrev} onClick={() => previewGoToPage("prev")} title="Página anterior (←)">
                        <ChevronRight className="w-3 h-3 rotate-180" />
                        Anterior
                      </Button>
                      <span className="text-[10px] text-muted-foreground font-mono">Pág. {Math.floor(previewOffset / previewPageSize) + 1}</span>
                      <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" disabled={!previewCanGoNext} onClick={() => previewGoToPage("next")} title="Próxima página (→)">
                        Próxima
                        <ChevronRight className="w-3 h-3" />
                      </Button>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 gap-1 text-xs"
                      onClick={() => {
                        navigator.clipboard.writeText(previewData.raw_output || "");
                        toast.success("Dados copiados!");
                      }}
                    >
                      <Copy className="h-3 w-3" />
                      Copiar
                    </Button>
                  </div>
                  <ScrollArea className="flex-1 min-h-0 rounded-md border border-border bg-muted/30">
                    <pre className="text-xs font-mono whitespace-pre-wrap break-all p-2">{previewData.raw_output}</pre>
                  </ScrollArea>
                </div>
              ) : (
                <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground py-10">
                  Nenhum dado encontrado.
                </div>
              )}
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
