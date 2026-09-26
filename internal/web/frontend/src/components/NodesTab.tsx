import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertTriangle,
  ArrowLeft,
  Ban,
  BarChart3,
  ChevronRight,
  CheckCircle2,
  Copy,
  FileText,
  Loader2,
  MoreVertical,
  Package,
  PlayCircle,
  RefreshCcw,
  Search,
  ServerCog,
  ShieldAlert,
  Trash2,
  X,
  XCircle,
} from "lucide-react";
import { apiClient } from "@/lib/api/client";
import type { ClusterNodeSummary, Namespace, NodeDrainEvent, NodeDrainOptions } from "@/lib/api/types";
import { DeploymentsTab } from "@/components/DeploymentsTab";
import ErrorBoundary from "@/components/ErrorBoundary";
import { SplitView } from "@/components/SplitView";
import { ResourceYamlPanel } from "@/components/ResourceYamlPanel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ProtectedAction } from "@/components/rbac";

// Aba Nodes (Workloads): todos os nodes do cluster no padrão da aba Namespaces — lista à
// esquerda com menu de 3 pontos (cordon/uncordon, drain, describe, delete); à direita, com node
// selecionado, detalhes + YAML editável (ResourceYamlPanel); sem seleção, navegação por node
// (nodes → namespaces com pods no node → deployments do node → pods do node, NodeWorkloadsNavigator).
// A visão geral dos nodes fica num modal.
// Backend: /api/v1/cluster-nodes (handlers/cluster_nodes.go).

type StatusFilter = "all" | "ready" | "notready" | "cordoned";

const fmtCores = (m: number) => (m / 1000).toFixed(m >= 10000 ? 0 : 1);
const fmtGiB = (b: number) => (b / 1024 ** 3).toFixed(1);
const pct = (used: number, total: number) => (used >= 0 && total > 0 ? Math.round((used / total) * 100) : null);

function UsageBar({ label, value, detail }: { label: string; value: number | null; detail?: string }) {
  const color = value === null ? "bg-muted" : value >= 90 ? "bg-red-500" : value >= 75 ? "bg-amber-500" : "bg-primary";
  return (
    <div className="min-w-0 flex-1" title={detail}>
      <div className="flex items-center justify-between text-[10px] text-muted-foreground mb-0.5">
        <span>{label}</span>
        <span className="font-mono text-foreground/80">{value === null ? "—" : `${value}%`}</span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div className={`h-full ${color}`} style={{ width: `${Math.min(value ?? 0, 100)}%` }} />
      </div>
    </div>
  );
}

function StatusBadges({ node }: { node: ClusterNodeSummary }) {
  return (
    <div className="flex items-center gap-1 flex-wrap">
      <Badge
        variant="outline"
        className={`text-[10px] ${
          node.status === "Ready"
            ? "border-green-500/40 text-green-600 dark:text-green-400"
            : "border-red-500/40 text-red-600 dark:text-red-400"
        }`}
      >
        {node.status}
      </Badge>
      {node.unschedulable && (
        <Badge variant="outline" className="text-[10px] border-amber-500/40 text-amber-600 dark:text-amber-400">
          SchedulingDisabled
        </Badge>
      )}
      {node.pressures.map(p => (
        <Badge key={p} variant="outline" className="text-[10px] border-red-500/40 text-red-600 dark:text-red-400">
          {p}
        </Badge>
      ))}
    </div>
  );
}

interface NodesTabProps {
  cluster: string;
  namespaces: Namespace[];
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const NodesTab = ({ cluster, namespaces, showSystemNamespaces, onToggleSystemNamespaces }: NodesTabProps) => {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [poolFilter, setPoolFilter] = useState("");
  const [sortBy, setSortBy] = useState<"name" | "cpu" | "mem" | "pods">("name");
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [yamlReloadToken, setYamlReloadToken] = useState(0);
  const [busyNode, setBusyNode] = useState<string | null>(null);
  const [describe, setDescribe] = useState<{ node: string; content: string; loading: boolean } | null>(null);
  const [drainNodes, setDrainNodes] = useState<string[] | null>(null);
  const [deleteNode, setDeleteNode] = useState<string | null>(null);
  const [overviewOpen, setOverviewOpen] = useState(false);
  // Seleção múltipla para cordon/uncordon em lote (independente do node aberto à direita).
  const [checked, setChecked] = useState<Set<string>>(new Set());

  const nodesQuery = useQuery({
    queryKey: ["cluster-nodes", cluster],
    queryFn: () => apiClient.getClusterNodes(cluster),
    enabled: !!cluster,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
  const permsQuery = useQuery({
    queryKey: ["cluster-nodes-perms", cluster],
    queryFn: () => apiClient.getClusterNodePermissions(cluster),
    enabled: !!cluster,
    staleTime: 5 * 60_000,
  });
  // Enquanto a checagem não volta, libera — o backend aplica o RBAC de qualquer forma.
  const perms = permsQuery.data ?? { canPatch: true, canDelete: true, canEvict: true };

  const nodes = useMemo(() => nodesQuery.data ?? [], [nodesQuery.data]);
  const selected = nodes.find(n => n.name === selectedName) ?? null;
  const pools = useMemo(() => [...new Set(nodes.map(n => n.nodePool).filter(Boolean) as string[])].sort(), [nodes]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const list = nodes.filter(n => {
      if (poolFilter && n.nodePool !== poolFilter) return false;
      if (statusFilter === "ready" && n.status !== "Ready") return false;
      if (statusFilter === "notready" && n.status === "Ready") return false;
      if (statusFilter === "cordoned" && !n.unschedulable) return false;
      if (!q) return true;
      return [n.name, n.nodePool, n.zone, n.instanceType, n.internalIP, n.kubeletVersion]
        .some(v => v?.toLowerCase().includes(q));
    });
    const usage = (n: ClusterNodeSummary) =>
      sortBy === "cpu" ? pct(n.cpuUsageMillis, n.cpuAllocatableMillis) ?? -1
        : sortBy === "mem" ? pct(n.memUsageBytes, n.memAllocatableBytes) ?? -1
        : n.podsCount;
    return sortBy === "name" ? list : [...list].sort((a, b) => usage(b) - usage(a));
  }, [nodes, search, statusFilter, poolFilter, sortBy]);

  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ["cluster-nodes", cluster] });
  }, [queryClient, cluster]);

  const setSchedulable = async (name: string, schedulable: boolean) => {
    setBusyNode(name);
    try {
      await apiClient.setClusterNodeSchedulable(cluster, name, schedulable);
      toast.success(schedulable ? `Uncordon: ${name} volta a receber pods` : `Cordon: ${name} não recebe novos pods`);
      refresh();
      setYamlReloadToken(t => t + 1);
    } catch (err) {
      toast.error(schedulable ? "Falha no uncordon" : "Falha no cordon", { description: err instanceof Error ? err.message : String(err) });
    } finally {
      setBusyNode(null);
    }
  };

  const openDescribe = async (name: string) => {
    setDescribe({ node: name, content: "", loading: true });
    try {
      const r = await apiClient.describeClusterNode(cluster, name);
      setDescribe({ node: name, content: r.describe, loading: false });
    } catch (err) {
      setDescribe({ node: name, content: `Erro: ${err instanceof Error ? err.message : String(err)}`, loading: false });
    }
  };

  const loadYaml = useCallback(async () => (await apiClient.getClusterNode(cluster, selectedName!)).yaml, [cluster, selectedName]);
  const applyYaml = useCallback(
    (yamlContent: string, dryRun: boolean) => apiClient.applyClusterNode(cluster, selectedName!, { yaml: yamlContent, dryRun, force: true }),
    [cluster, selectedName],
  );

  const actionsMenu = (node: ClusterNodeSummary, triggerClassName = "h-7 w-7 p-0") => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className={triggerClassName} onClick={e => e.stopPropagation()} title="Ações do node">
          {busyNode === node.name ? <Loader2 className="w-4 h-4 animate-spin" /> : <MoreVertical className="w-4 h-4" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onClick={e => e.stopPropagation()}>
        <ProtectedAction showWarning={false}>
          {node.unschedulable ? (
            <DropdownMenuItem disabled={!perms.canPatch} onClick={() => setSchedulable(node.name, true)}>
              <PlayCircle className="w-4 h-4 mr-2" />
              Uncordon
              {!perms.canPatch && <span className="ml-2 text-[10px] text-muted-foreground">(sem permissão)</span>}
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem disabled={!perms.canPatch} onClick={() => setSchedulable(node.name, false)}>
              <Ban className="w-4 h-4 mr-2" />
              Cordon
              {!perms.canPatch && <span className="ml-2 text-[10px] text-muted-foreground">(sem permissão)</span>}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem disabled={!perms.canPatch || !perms.canEvict} onClick={() => setDrainNodes([node.name])}>
            <ServerCog className="w-4 h-4 mr-2" />
            Drain…
            {(!perms.canPatch || !perms.canEvict) && <span className="ml-2 text-[10px] text-muted-foreground">(sem permissão)</span>}
          </DropdownMenuItem>
        </ProtectedAction>
        <DropdownMenuItem onClick={() => openDescribe(node.name)}>
          <FileText className="w-4 h-4 mr-2" />
          Describe
        </DropdownMenuItem>
        <ProtectedAction showWarning={false}>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={!perms.canDelete}
            onClick={() => setDeleteNode(node.name)}
            className="text-destructive focus:text-destructive"
          >
            <Trash2 className="w-4 h-4 mr-2" />
            Deletar node
            {!perms.canDelete && <span className="ml-2 text-[10px] text-muted-foreground">(sem permissão)</span>}
          </DropdownMenuItem>
        </ProtectedAction>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  // ── Painel esquerdo ──
  const leftTitleAction = (
    <div className="flex items-center gap-2">
      {selected && (
        <button
          onClick={() => setSelectedName(null)}
          className="flex items-center justify-center w-7 h-7 rounded-full bg-primary text-primary-foreground shadow-sm hover:bg-primary/85 active:bg-primary/70 transition-colors flex-shrink-0"
          title="Desmarcar node e voltar para a visão geral"
        >
          <X className="w-4 h-4" />
        </button>
      )}
      <Button variant="outline" size="sm" onClick={refresh} disabled={!cluster || nodesQuery.isFetching}>
        {nodesQuery.isFetching ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <RefreshCcw className="w-4 h-4 mr-2" />}
        Atualizar
      </Button>
    </div>
  );

  const statusCount = (f: StatusFilter) =>
    f === "all" ? nodes.length
      : f === "ready" ? nodes.filter(n => n.status === "Ready").length
      : f === "notready" ? nodes.filter(n => n.status !== "Ready").length
      : nodes.filter(n => n.unschedulable).length;

  useEffect(() => setChecked(new Set()), [cluster]);
  // Nodes que sumiram da listagem saem da seleção.
  const checkedNodes = useMemo(() => nodes.filter(n => checked.has(n.name)), [nodes, checked]);
  const toggleChecked = (name: string, on: boolean) =>
    setChecked(prev => {
      const next = new Set(prev);
      if (on) next.add(name);
      else next.delete(name);
      return next;
    });
  const toggleMany = (names: string[], on: boolean) =>
    setChecked(prev => {
      const next = new Set(prev);
      names.forEach(n => (on ? next.add(n) : next.delete(n)));
      return next;
    });

  // Cordon/uncordon em lote (usado pela barra de lote das duas listas).
  const applySchedulableBatch = async (names: string[], schedulable: boolean) => {
    try {
      const r = await apiClient.setClusterNodesSchedulableBatch(cluster, names, schedulable);
      const okCount = r.results.filter(x => x.ok).length;
      if (r.failed === 0) {
        toast.success(`${schedulable ? "Uncordon" : "Cordon"} aplicado em ${okCount} node(s)`);
        setChecked(new Set());
      } else {
        const failures = r.results.filter(x => !x.ok);
        toast.error(`${okCount} ok, ${r.failed} falha(s)`, {
          description: failures.slice(0, 5).map(f => `${f.node}: ${f.error}`).join("\n") + (failures.length > 5 ? `\n+${failures.length - 5}` : ""),
        });
        // Mantém selecionados só os que falharam, para tentar de novo.
        setChecked(new Set(failures.map(f => f.node)));
      }
    } catch (err) {
      toast.error("Falha no lote", { description: err instanceof Error ? err.message : String(err) });
    } finally {
      refresh();
      setYamlReloadToken(t => t + 1);
    }
  };

  const bulkBar = (
    <NodeBulkBar
      selected={checkedNodes}
      allNodes={nodes}
      canPatch={perms.canPatch}
      canEvict={perms.canEvict}
      onClear={() => setChecked(new Set())}
      onApplySchedulable={applySchedulableBatch}
      onDrain={names => setDrainNodes(names)}
    />
  );

  const selectAllRow = (list: ClusterNodeSummary[], label: string) => {
    const all = list.length > 0 && list.every(n => checked.has(n.name));
    const some = list.some(n => checked.has(n.name));
    return (
      <label className="flex items-center gap-2 text-xs cursor-pointer select-none px-1">
        <Checkbox
          checked={all}
          data-state={some && !all ? "indeterminate" : undefined}
          onCheckedChange={v => toggleMany(list.map(n => n.name), v === true)}
          className="w-3.5 h-3.5 rounded-full"
          disabled={list.length === 0}
        />
        {label} ({list.length})
      </label>
    );
  };

  const leftContent = (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input placeholder="Buscar node, pool, zona, IP, versão..." value={search} onChange={e => setSearch(e.target.value)} className="pl-10 pr-8" />
        {search && (
          <button type="button" onClick={() => setSearch("")} className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground" aria-label="Limpar busca">
            ×
          </button>
        )}
      </div>

      <div className="flex items-center gap-1.5 flex-wrap">
        {([
          ["all", "Todos"],
          ["ready", "Ready"],
          ["notready", "NotReady"],
          ["cordoned", "Cordoned"],
        ] as [StatusFilter, string][]).map(([f, label]) => (
          <Button key={f} size="sm" variant={statusFilter === f ? "default" : "outline"} className="h-7 text-xs px-2" onClick={() => setStatusFilter(f)}>
            {label} <span className="ml-1 opacity-70">{statusCount(f)}</span>
          </Button>
        ))}
      </div>

      <div className="flex items-center gap-2">
        <select
          value={poolFilter}
          onChange={e => setPoolFilter(e.target.value)}
          className="flex-1 min-w-0 text-xs bg-background border border-border/60 rounded-md px-2 h-8"
        >
          <option value="">Todos os node pools</option>
          {pools.map(p => <option key={p} value={p}>{p}</option>)}
        </select>
        <select
          value={sortBy}
          onChange={e => setSortBy(e.target.value as typeof sortBy)}
          className="text-xs bg-background border border-border/60 rounded-md px-2 h-8"
          title="Ordenação"
        >
          <option value="name">Nome</option>
          <option value="cpu">Maior CPU</option>
          <option value="mem">Maior memória</option>
          <option value="pods">Mais pods</option>
        </select>
      </div>

      {cluster && filtered.length > 0 && selectAllRow(filtered, poolFilter ? `Todos do pool ${poolFilter}` : "Todos os filtrados")}

      {!cluster ? (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">Selecione um cluster para listar os Nodes</div>
      ) : nodesQuery.isLoading ? (
        <div className="flex items-center justify-center h-64"><Loader2 className="w-6 h-6 animate-spin" /></div>
      ) : nodesQuery.error ? (
        <div className="text-sm text-red-400 p-3">{nodesQuery.error instanceof Error ? nodesQuery.error.message : String(nodesQuery.error)}</div>
      ) : filtered.length === 0 ? (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {nodes.length === 0 ? "Nenhum node encontrado" : "Nenhum node corresponde aos filtros"}
        </div>
      ) : (
        <div className="space-y-2">
          {filtered.map(node => {
            const isSelected = node.name === selectedName;
            return (
              <div
                key={node.name}
                role="button"
                tabIndex={0}
                onClick={() => setSelectedName(node.name)}
                onKeyDown={e => e.key === "Enter" && setSelectedName(node.name)}
                className={`w-full text-left p-3 rounded-lg border transition-colors cursor-pointer ${
                  isSelected ? "border-primary bg-primary/10" : "border-border/60 hover:border-primary/40"
                }`}
              >
                <div className="flex items-start gap-2">
                  <span className="pt-0.5" onClick={e => e.stopPropagation()} onKeyDown={e => e.stopPropagation()}>
                    <Checkbox
                      checked={checked.has(node.name)}
                      onCheckedChange={v => toggleChecked(node.name, v === true)}
                      className="w-3.5 h-3.5 rounded-full"
                      aria-label={`Selecionar ${node.name} para ação em lote`}
                    />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="font-semibold text-sm truncate" title={node.name}>{node.name}</div>
                    <div className="text-[11px] text-muted-foreground truncate">
                      {[node.nodePool, node.zone, node.instanceType].filter(Boolean).join(" · ") || "—"}
                    </div>
                  </div>
                  {actionsMenu(node)}
                </div>
                <div className="mt-2"><StatusBadges node={node} /></div>
                <div className="mt-2 flex items-end gap-3">
                  <UsageBar label="CPU" value={pct(node.cpuUsageMillis, node.cpuAllocatableMillis)} />
                  <UsageBar label="MEM" value={pct(node.memUsageBytes, node.memAllocatableBytes)} />
                  <div className="text-[10px] text-muted-foreground whitespace-nowrap text-right">
                    <div><span className="font-mono text-foreground/80">{node.podsCount}</span>/{node.podsCapacity} pods</div>
                    <div>{node.age}</div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {checkedNodes.length > 0 && <div className="sticky bottom-0 z-10 -mx-1">{bulkBar}</div>}
    </div>
  );

  // ── Painel direito ──
  const renderOverview = () => {
    if (!cluster || nodes.length === 0) {
      return <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">Nenhum node carregado</div>;
    }
    const byPool = pools.map(p => {
      const list = nodes.filter(n => n.nodePool === p);
      const sum = (f: (n: ClusterNodeSummary) => number) => list.reduce((acc, n) => acc + Math.max(f(n), 0), 0);
      const hasMetrics = list.every(n => n.cpuUsageMillis >= 0);
      return {
        pool: p,
        total: list.length,
        ready: list.filter(n => n.status === "Ready").length,
        cordoned: list.filter(n => n.unschedulable).length,
        cpu: hasMetrics ? pct(sum(n => n.cpuUsageMillis), sum(n => n.cpuAllocatableMillis)) : null,
        mem: hasMetrics ? pct(sum(n => n.memUsageBytes), sum(n => n.memAllocatableBytes)) : null,
        pods: sum(n => n.podsCount),
      };
    });
    const versions = [...new Set(nodes.map(n => n.kubeletVersion))].sort();
    const cards: [string, number, string][] = [
      ["Nodes", nodes.length, ""],
      ["Ready", statusCount("ready"), "text-green-600 dark:text-green-400"],
      ["NotReady", statusCount("notready"), statusCount("notready") > 0 ? "text-red-600 dark:text-red-400" : ""],
      ["Cordoned", statusCount("cordoned"), statusCount("cordoned") > 0 ? "text-amber-600 dark:text-amber-400" : ""],
    ];
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {cards.map(([label, value, cls]) => (
            <div key={label} className="rounded-lg border border-border/60 p-3">
              <div className="text-xs text-muted-foreground">{label}</div>
              <div className={`text-2xl font-semibold ${cls}`}>{value}</div>
            </div>
          ))}
        </div>
        <div className="rounded-lg border border-border/60">
          <div className="px-3 py-2 border-b text-sm font-medium">Por node pool</div>
          <table className="w-full text-xs">
            <thead className="text-muted-foreground">
              <tr className="border-b">
                <th className="text-left font-medium px-3 py-2">Pool</th>
                <th className="text-right font-medium px-3 py-2">Nodes</th>
                <th className="text-right font-medium px-3 py-2">Ready</th>
                <th className="text-right font-medium px-3 py-2">Cordoned</th>
                <th className="text-right font-medium px-3 py-2">CPU</th>
                <th className="text-right font-medium px-3 py-2">Memória</th>
                <th className="text-right font-medium px-3 py-2">Pods</th>
              </tr>
            </thead>
            <tbody>
              {byPool.map(r => (
                <tr key={r.pool} className="border-b last:border-0 hover:bg-muted/40 cursor-pointer" onClick={() => { setPoolFilter(r.pool); setOverviewOpen(false); }} title="Filtrar a lista por este pool">
                  <td className="px-3 py-2 font-mono">{r.pool}</td>
                  <td className="px-3 py-2 text-right">{r.total}</td>
                  <td className="px-3 py-2 text-right">{r.ready}</td>
                  <td className={`px-3 py-2 text-right ${r.cordoned ? "text-amber-600 dark:text-amber-400" : ""}`}>{r.cordoned}</td>
                  <td className="px-3 py-2 text-right font-mono">{r.cpu === null ? "—" : `${r.cpu}%`}</td>
                  <td className="px-3 py-2 text-right font-mono">{r.mem === null ? "—" : `${r.mem}%`}</td>
                  <td className="px-3 py-2 text-right">{r.pods}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="text-xs text-muted-foreground">
          Versões do kubelet: {versions.map(v => <Badge key={v} variant="secondary" className="ml-1 text-[10px] font-mono">{v}</Badge>)}
        </div>
      </div>
    );
  };

  const renderDetails = (node: ClusterNodeSummary) => {
    const cpu = pct(node.cpuUsageMillis, node.cpuAllocatableMillis);
    const mem = pct(node.memUsageBytes, node.memAllocatableBytes);
    const fields: [string, string | undefined][] = [
      ["Node pool", node.nodePool],
      ["Roles", node.roles.join(", ") || undefined],
      ["Zona", node.zone],
      ["Tipo", node.instanceType],
      ["IP interno", node.internalIP],
      ["Kubelet", node.kubeletVersion],
      ["Runtime", node.containerRuntime],
      ["SO", node.osImage],
      ["Idade", node.age],
    ];
    return (
      <div className="space-y-4">
        <div className="space-y-3 border-b border-border/50 pb-3">
          <StatusBadges node={node} />
          <div className="grid grid-cols-2 md:grid-cols-3 gap-x-4 gap-y-2 text-xs">
            {fields.filter(([, v]) => v).map(([label, value]) => (
              <div key={label} className="min-w-0">
                <div className="text-muted-foreground uppercase text-[10px]">{label}</div>
                <div className="font-medium truncate" title={value}>{value}</div>
              </div>
            ))}
          </div>
          <div className="flex items-end gap-4">
            <UsageBar label={`CPU ${node.cpuUsageMillis >= 0 ? fmtCores(node.cpuUsageMillis) : "?"} / ${fmtCores(node.cpuAllocatableMillis)} cores`} value={cpu} />
            <UsageBar label={`Memória ${node.memUsageBytes >= 0 ? fmtGiB(node.memUsageBytes) : "?"} / ${fmtGiB(node.memAllocatableBytes)} GiB`} value={mem} />
            <UsageBar label={`Pods ${node.podsCount} / ${node.podsCapacity}`} value={pct(node.podsCount, node.podsCapacity)} />
          </div>
          {node.cpuUsageMillis < 0 && <p className="text-[11px] text-muted-foreground">Uso de CPU/memória indisponível (Metrics Server não respondeu).</p>}
          {node.taints.length > 0 && (
            <div>
              <div className="text-muted-foreground uppercase text-[10px] mb-1">Taints</div>
              <div className="flex flex-wrap gap-1">
                {node.taints.map(t => <Badge key={t} variant="secondary" className="text-[10px] font-mono">{t}</Badge>)}
              </div>
            </div>
          )}
        </div>
        <ResourceYamlPanel
          kind="Node"
          name={node.name}
          cluster={cluster}
          loadYaml={loadYaml}
          applyYaml={applyYaml}
          canApply={perms.canPatch}
          onApplied={refresh}
          reloadToken={yamlReloadToken}
        />
      </div>
    );
  };

  const rightTitleAction = selected ? (
    <div className="flex items-center gap-2">
      <Button variant="outline" size="sm" onClick={() => openDescribe(selected.name)}>
        <FileText className="w-4 h-4 mr-1" />
        Describe
      </Button>
      {actionsMenu(selected, "h-8 w-8 p-0")}
    </div>
  ) : (
    <Button variant="outline" size="sm" onClick={() => setOverviewOpen(true)} disabled={!cluster}>
      <BarChart3 className="w-4 h-4 mr-1" />
      Visão geral dos nodes
    </Button>
  );

  return (
    <>
      <SplitView
        leftPanel={{ title: `Nodes (${filtered.length})`, titleAction: leftTitleAction, content: leftContent }}
        rightPanel={{
          title: selected ? selected.name : "Workloads por node",
          titleAction: rightTitleAction,
          content: selected ? (
            renderDetails(selected)
          ) : (
            <NodeWorkloadsNavigator
              cluster={cluster}
              nodes={nodes}
              checked={checked}
              onToggle={toggleChecked}
              selectAllRow={selectAllRow}
              bulkBar={checkedNodes.length > 0 ? bulkBar : null}
              namespaces={namespaces}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={onToggleSystemNamespaces}
            />
          ),
        }}
      />

      {/* Visão geral dos nodes (totais e por node pool) — como o "Visão geral do cluster" da aba Namespaces */}
      <Dialog open={overviewOpen} onOpenChange={setOverviewOpen}>
        <DialogContent className="max-w-5xl max-h-[90vh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center gap-2">
              <BarChart3 className="w-5 h-5" />
              Visão geral dos nodes
            </DialogTitle>
            <DialogDescription className="flex items-center justify-between gap-2">
              <span>{cluster}</span>
              <Button variant="outline" size="sm" onClick={refresh} disabled={!cluster || nodesQuery.isFetching}>
                {nodesQuery.isFetching ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <RefreshCcw className="w-4 h-4 mr-1" />}
                Atualizar
              </Button>
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-auto min-h-0">{renderOverview()}</div>
        </DialogContent>
      </Dialog>

      {/* Describe */}
      <Dialog open={!!describe} onOpenChange={open => !open && setDescribe(null)}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {describe?.node}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">Node: {describe?.node} • Cluster: {cluster}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="h-[70vh]">
            {describe?.loading ? (
              <div className="flex items-center justify-center py-8"><Loader2 className="w-6 h-6 animate-spin" /></div>
            ) : (
              <pre className="text-xs font-mono bg-muted p-4 rounded whitespace-pre-wrap">{describe?.content}</pre>
            )}
          </ScrollArea>
          <DialogFooter className="gap-2">
            <Button variant="outline" size="sm" onClick={() => describe && openDescribe(describe.node)} disabled={describe?.loading}>
              <RefreshCcw className={`w-3 h-3 mr-1 ${describe?.loading ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
            <Button variant="outline" size="sm" onClick={() => describe && navigator.clipboard.writeText(describe.content)} disabled={!describe?.content}>
              <Copy className="w-3 h-3 mr-1" />
              Copiar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {drainNodes && (
        <DrainDialog
          cluster={cluster}
          nodes={drainNodes}
          onClose={() => setDrainNodes(null)}
          onFinished={() => { refresh(); setYamlReloadToken(t => t + 1); }}
        />
      )}

      {deleteNode && (
        <DeleteNodeDialog
          cluster={cluster}
          node={deleteNode}
          onClose={() => setDeleteNode(null)}
          onDeleted={() => {
            if (selectedName === deleteNode) setSelectedName(null);
            refresh();
          }}
        />
      )}
    </>
  );
};

// ─── Navegação por node (painel direito sem seleção) ─────────────────────────

function NodeWorkloadsNavigator({
  cluster,
  nodes,
  checked,
  onToggle,
  selectAllRow,
  bulkBar,
  namespaces,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: {
  cluster: string;
  nodes: ClusterNodeSummary[];
  checked: Set<string>;
  onToggle: (name: string, on: boolean) => void;
  selectAllRow: (list: ClusterNodeSummary[], label: string) => React.ReactNode;
  bulkBar: React.ReactNode;
  namespaces: Namespace[];
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}) {
  const [node, setNode] = useState("");
  const [namespace, setNamespace] = useState("");
  const [search, setSearch] = useState("");
  // DeploymentsTab chama onNamespaceChange("") se o namespace sumir — volta para a lista.
  const handleNamespaceChange = useCallback((ns: string) => setNamespace(ns), []);

  useEffect(() => {
    setNode("");
    setNamespace("");
  }, [cluster]);
  useEffect(() => setSearch(""), [node]);

  const workloadsQuery = useQuery({
    queryKey: ["cluster-node-workloads", cluster, node],
    queryFn: () => apiClient.getClusterNodeWorkloads(cluster, node),
    enabled: !!cluster && !!node,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
  const systemNs = useMemo(() => new Set(namespaces.filter(n => n.isSystem).map(n => n.name)), [namespaces]);
  const current = workloadsQuery.data?.find(w => w.namespace === namespace);
  const nodeFilter = useMemo(() => ({ node, deployments: current?.deployments ?? [] }), [node, current]);

  const q = search.trim().toLowerCase();
  const row = "w-full flex items-center justify-between gap-2 px-3 py-2 text-left text-sm transition-colors";
  const searchBox = (placeholder: string) => (
    <div className="relative flex-shrink-0">
      <Search className="absolute left-2 top-2.5 w-4 h-4 text-muted-foreground" />
      <Input placeholder={placeholder} value={search} onChange={e => setSearch(e.target.value)} className="pl-8" />
    </div>
  );

  if (!cluster) {
    return <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">Selecione um cluster para listar os Nodes</div>;
  }

  // Nível 3: deployments do namespace com pods no node (e, no drill-down, só os pods do node).
  if (node && namespace) {
    return (
      <ErrorBoundary componentName="Nodes › Deployments">
        <DeploymentsTab
          cluster={cluster}
          namespaces={namespaces}
          selectedNamespace={namespace}
          onNamespaceChange={handleNamespaceChange}
          showSystemNamespaces={showSystemNamespaces}
          onToggleSystemNamespaces={onToggleSystemNamespaces}
          stateScope="nodes-workloads"
          embedded={{ onBack: () => setNamespace(""), backLabel: `Namespaces em ${node}` }}
          nodeFilter={nodeFilter}
        />
      </ErrorBoundary>
    );
  }

  // Nível 2: namespaces com pods no node.
  if (node) {
    const all = workloadsQuery.data ?? [];
    const hiddenSystem = showSystemNamespaces ? 0 : all.filter(w => systemNs.has(w.namespace)).length;
    const list = all.filter(w => (showSystemNamespaces || !systemNs.has(w.namespace)) && (!q || w.namespace.toLowerCase().includes(q)));
    return (
      <div className="flex flex-col h-full min-h-0 gap-2">
        <div className="flex items-center gap-2 flex-shrink-0 min-w-0">
          <Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => setNode("")}>
            <ArrowLeft className="w-4 h-4 mr-1" />
            Nodes
          </Button>
          <span className="text-sm font-mono truncate" title={node}>{node}</span>
          {workloadsQuery.isFetching && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
        </div>
        {searchBox("Filtrar namespaces...")}
        <p className="text-xs text-muted-foreground flex-shrink-0">
          {list.length} namespace(s) com pods neste node — clique para ver os deployments
          {hiddenSystem > 0 && ` · ${hiddenSystem} de sistema ocultos`}
        </p>
        <div className="flex-1 overflow-auto min-h-0 border border-border/50 rounded-md divide-y divide-border/50">
          {workloadsQuery.isLoading ? (
            <div className="flex items-center justify-center h-32"><Loader2 className="w-5 h-5 animate-spin" /></div>
          ) : workloadsQuery.error ? (
            <div className="p-3 text-sm text-red-400">{workloadsQuery.error instanceof Error ? workloadsQuery.error.message : String(workloadsQuery.error)}</div>
          ) : list.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">Nenhum namespace com pods neste node</div>
          ) : (
            list.map(w => {
              const clickable = w.deployments.length > 0;
              return (
                <button
                  key={w.namespace}
                  disabled={!clickable}
                  onClick={() => setNamespace(w.namespace)}
                  title={clickable ? undefined : "Os pods deste namespace no node não pertencem a Deployments (DaemonSet, StatefulSet, Job...)"}
                  className={`${row} ${clickable ? "hover:bg-primary/10" : "opacity-60 cursor-default"}`}
                >
                  <span className="flex items-center gap-2 min-w-0">
                    <Package className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                    <span className="font-medium truncate">{w.namespace}</span>
                    {systemNs.has(w.namespace) && (
                      <span className="px-1.5 py-0.5 bg-yellow-500/20 text-yellow-300 rounded text-[10px] flex-shrink-0">Sistema</span>
                    )}
                  </span>
                  <span className="flex items-center gap-2 flex-shrink-0 text-[11px] text-muted-foreground">
                    <span>{w.pods} pod(s)</span>
                    <span>· {w.deployments.length} deployment(s)</span>
                    {w.otherPods > 0 && <span>· {w.otherPods} outro(s)</span>}
                    {clickable && <ChevronRight className="w-4 h-4" />}
                  </span>
                </button>
              );
            })
          )}
        </div>
      </div>
    );
  }

  // Nível 1: nodes.
  const list = nodes.filter(n => !q || [n.name, n.nodePool, n.zone].some(v => v?.toLowerCase().includes(q)));
  return (
    <div className="flex flex-col h-full min-h-0 gap-2">
      {searchBox("Filtrar nodes...")}
      <div className="flex items-center justify-between gap-2 flex-shrink-0">
        {selectAllRow(list, q ? "Todos os filtrados" : "Todos")}
        <p className="text-xs text-muted-foreground">clique no node para ver os namespaces com pods nele</p>
      </div>
      <div className="flex-1 overflow-auto min-h-0 border border-border/50 rounded-md divide-y divide-border/50">
        {list.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">Nenhum node corresponde à busca</div>
        ) : (
          list.map(n => (
            <button key={n.name} onClick={() => setNode(n.name)} className={`${row} hover:bg-primary/10 ${checked.has(n.name) ? "bg-primary/5" : ""}`}>
              <span className="flex items-center gap-2 min-w-0">
                {/* Checkbox — clique faz toggle sem abrir o node */}
                <span className="flex items-center" onClick={e => { e.stopPropagation(); onToggle(n.name, !checked.has(n.name)); }}>
                  <Checkbox checked={checked.has(n.name)} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
                </span>
                <span className={`h-2 w-2 rounded-full flex-shrink-0 ${n.status === "Ready" ? "bg-green-500" : "bg-red-500"}`} title={n.status} />
                <span className="font-medium truncate" title={n.name}>{n.name}</span>
                {n.unschedulable && (
                  <span className="px-1.5 py-0.5 bg-amber-500/20 text-amber-500 rounded text-[10px] flex-shrink-0">cordoned</span>
                )}
              </span>
              <span className="flex items-center gap-2 flex-shrink-0 text-[11px] text-muted-foreground">
                {n.nodePool && <span className="font-mono">{n.nodePool}</span>}
                <span>· {n.podsCount} pod(s)</span>
                <ChevronRight className="w-4 h-4" />
              </span>
            </button>
          ))
        )}
      </div>
      {bulkBar && <div className="flex-shrink-0">{bulkBar}</div>}
    </div>
  );
}

// ─── Barra de ações em lote (mesmo molde da DeploymentMonitorTable) ──────────

function NodeBulkBar({
  selected,
  allNodes,
  canPatch,
  canEvict,
  onClear,
  onApplySchedulable,
  onDrain,
}: {
  selected: ClusterNodeSummary[];
  allNodes: ClusterNodeSummary[];
  canPatch: boolean;
  canEvict: boolean;
  onClear: () => void;
  onApplySchedulable: (names: string[], schedulable: boolean) => Promise<void>;
  onDrain: (names: string[]) => void;
}) {
  const [action, setAction] = useState<"cordon" | "uncordon" | null>(null);
  const [processing, setProcessing] = useState(false);

  const toCordon = selected.filter(n => !n.unschedulable);
  const toUncordon = selected.filter(n => n.unschedulable);
  const targets = action === "cordon" ? toCordon : action === "uncordon" ? toUncordon : [];
  const skipped = selected.length - targets.length;

  // Pools que ficariam sem nenhum node Ready aceitando pods depois do cordon.
  const poolsLeftEmpty = useMemo(() => {
    if (action !== "cordon") return [];
    const names = new Set(toCordon.map(n => n.name));
    return [...new Set(toCordon.map(n => n.nodePool ?? ""))].filter(pool =>
      !allNodes.some(n => (n.nodePool ?? "") === pool && n.status === "Ready" && !n.unschedulable && !names.has(n.name)),
    );
  }, [action, toCordon, allNodes]);

  const confirm = async () => {
    if (!action || targets.length === 0) return;
    setProcessing(true);
    try {
      await onApplySchedulable(targets.map(n => n.name), action === "uncordon");
    } finally {
      setProcessing(false);
      setAction(null);
    }
  };

  const noPerm = "Sem permissão no cluster para esta ação";
  return (
    <div className="border-t border-border bg-background">
      {action ? (
        <div className={`flex items-center gap-2 px-3 py-2 text-xs ${action === "cordon" ? "bg-amber-500/10 border-b border-amber-500/30" : "bg-green-500/10 border-b border-green-500/30"}`}>
          <span className="flex-1 min-w-0">
            {action === "cordon" ? `Cordon em ${targets.length} node(s)? Deixam de receber novos pods.` : `Uncordon em ${targets.length} node(s)? Voltam a receber pods.`}
            {skipped > 0 && <span className="text-muted-foreground"> {skipped} já {action === "cordon" ? "em cordon" : "aceitando pods"}, fora do lote.</span>}
            {poolsLeftEmpty.length > 0 && (
              <span className="block text-amber-600 dark:text-amber-400">
                <AlertTriangle className="w-3 h-3 inline mr-1 -mt-0.5" />
                {poolsLeftEmpty.map(p => p || "(sem pool)").join(", ")} ficará(ão) sem node Ready aceitando pods — novos pods ficarão Pending.
              </span>
            )}
          </span>
          <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setAction(null)} disabled={processing}>
            Cancelar
          </Button>
          <Button
            size="sm"
            className={`h-6 px-3 text-xs gap-1 text-white ${action === "cordon" ? "bg-amber-600 hover:bg-amber-700" : "bg-green-600 hover:bg-green-700"}`}
            onClick={confirm}
            disabled={processing || targets.length === 0}
          >
            {processing ? <Loader2 className="w-3 h-3 animate-spin" /> : "Confirmar"}
          </Button>
        </div>
      ) : (
        <div className="flex items-center gap-2 px-3 py-1.5 bg-muted/30 flex-wrap">
          <Button variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground" onClick={onClear}>
            Desmarcar tudo
          </Button>
          <span className="text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{selected.length}</span> node(s) selecionado(s)
          </span>
          <div className="flex-1" />
          <ProtectedAction showWarning={false}>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs text-amber-500 border-amber-500/40 hover:bg-amber-500/10 hover:border-amber-500 gap-1"
              onClick={() => setAction("cordon")}
              disabled={!canPatch || toCordon.length === 0}
              title={canPatch ? "Cordon nos nodes selecionados" : noPerm}
            >
              <Ban className="w-3 h-3" />
              Cordon ({toCordon.length})
            </Button>
          </ProtectedAction>
          <ProtectedAction showWarning={false}>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs text-green-600 dark:text-green-400 border-green-500/40 hover:bg-green-500/10 hover:border-green-500 gap-1"
              onClick={() => setAction("uncordon")}
              disabled={!canPatch || toUncordon.length === 0}
              title={canPatch ? "Uncordon nos nodes selecionados" : noPerm}
            >
              <PlayCircle className="w-3 h-3" />
              Uncordon ({toUncordon.length})
            </Button>
          </ProtectedAction>
          <ProtectedAction showWarning={false}>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs text-destructive border-destructive/40 hover:bg-destructive/10 hover:border-destructive gap-1"
              onClick={() => onDrain(selected.map(n => n.name))}
              disabled={!canPatch || !canEvict}
              title={canPatch && canEvict ? "Drain nos nodes selecionados, um de cada vez" : noPerm}
            >
              <ServerCog className="w-3 h-3" />
              Drain ({selected.length})
            </Button>
          </ProtectedAction>
        </div>
      )}
    </div>
  );
}

// ─── Drain ────────────────────────────────────────────────────────────────────

const DEFAULT_DRAIN_OPTIONS: NodeDrainOptions = {
  ignore_daemonsets: true,
  delete_emptydir_data: true,
  force: false,
  grace_period: 30,
  timeout: "5m",
  disable_eviction: false,
};

type DrainPhase = "config" | "running" | "done" | "error" | "cancelled";

// Linha do log do drain: o evento e o node a que pertence (drain em lote).
type DrainLogEntry = NodeDrainEvent & { node: string };

// Drain de um ou mais nodes, um de cada vez (como `kubectl drain n1 n2 ...`), parando no primeiro
// que falhar — os seguintes não são tocados.
function DrainDialog({ cluster, nodes, onClose, onFinished }: { cluster: string; nodes: string[]; onClose: () => void; onFinished: () => void }) {
  const [opts, setOpts] = useState<NodeDrainOptions>(DEFAULT_DRAIN_OPTIONS);
  const [phase, setPhase] = useState<DrainPhase>("config");
  const [events, setEvents] = useState<DrainLogEntry[]>([]);
  const [progress, setProgress] = useState({ evicted: 0, total: 0 });
  const [current, setCurrent] = useState(0); // índice do node em andamento
  const [doneCount, setDoneCount] = useState(0);
  const abortRef = useRef<AbortController | null>(null);
  const multi = nodes.length > 1;

  const addEvent = (node: string, e: NodeDrainEvent) => {
    setProgress({ evicted: e.evicted, total: e.total || 0 });
    setEvents(prev => {
      // "blocked" repetido do mesmo pod substitui o anterior (não enche o log a cada 5s).
      const last = prev[prev.length - 1];
      if (e.type === "blocked" && last?.type === "blocked" && last.pod === e.pod && last.namespace === e.namespace) {
        return [...prev.slice(0, -1), { ...e, node }];
      }
      return [...prev, { ...e, node }].slice(-500);
    });
  };

  const start = async () => {
    setPhase("running");
    setEvents([]);
    setDoneCount(0);
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    let finished = 0;
    try {
      for (let i = 0; i < nodes.length; i++) {
        const node = nodes[i];
        setCurrent(i);
        setProgress({ evicted: 0, total: 0 });
        try {
          const last = await apiClient.drainClusterNode(cluster, node, opts, e => addEvent(node, e), ctrl.signal);
          if (last?.type === "error") {
            setPhase("error");
            toast.error(`Drain de ${node} falhou${multi ? ` — ${nodes.length - i - 1} node(s) restante(s) não foram drenados` : ""}`, { description: last.message });
            return;
          }
        } catch (err) {
          if (ctrl.signal.aborted) {
            setPhase("cancelled");
          } else {
            setPhase("error");
            addEvent(node, { type: "error", message: err instanceof Error ? err.message : String(err), evicted: 0, total: 0 });
          }
          return;
        }
        finished++;
        setDoneCount(finished);
      }
      setPhase("done");
      toast.success(multi ? `Drain concluído em ${nodes.length} nodes` : `Drain de ${nodes[0]} concluído`);
    } finally {
      abortRef.current = null;
      onFinished();
    }
  };

  const running = phase === "running";
  const percent = progress.total > 0 ? Math.round((progress.evicted / progress.total) * 100) : phase === "done" ? 100 : 0;

  const optionRow = (key: keyof NodeDrainOptions, label: string, hint: string, danger = false) => (
    <div className="flex items-start justify-between gap-3 py-1.5">
      <div>
        <Label className={`text-sm ${danger ? "text-amber-600 dark:text-amber-400" : ""}`}>{label}</Label>
        <p className="text-[11px] text-muted-foreground">{hint}</p>
      </div>
      <Switch checked={opts[key] as boolean} onCheckedChange={v => setOpts(o => ({ ...o, [key]: v }))} />
    </div>
  );

  const eventLine = (e: DrainLogEntry, i: number, all: DrainLogEntry[]) => {
    const pod = e.pod ? `${e.namespace}/${e.pod}` : "";
    const [Icon, cls, text] =
      e.type === "evicted" ? [CheckCircle2, "text-green-500", `${pod} removido`]
        : e.type === "evicting" ? [Loader2, "text-muted-foreground", `removendo ${pod}…`]
        : e.type === "blocked" ? [ShieldAlert, "text-amber-500", `${pod}: ${e.message}`]
        : e.type === "error" ? [XCircle, "text-red-500", e.message]
        : e.type === "done" ? [CheckCircle2, "text-green-500", e.message]
        : [PlayCircle, "text-primary", e.message];
    const newNode = multi && (i === 0 || all[i - 1].node !== e.node);
    return (
      <div key={i}>
        {newNode && <div className="text-[11px] font-semibold font-mono text-foreground/80 pt-1.5 pb-0.5 border-t first:border-t-0">{e.node}</div>}
        <div className="flex items-start gap-2 text-xs py-0.5">
          <Icon className={`w-3.5 h-3.5 mt-0.5 shrink-0 ${cls}`} />
          <span className="font-mono break-all">{text}</span>
        </div>
      </div>
    );
  };

  const statusLabel =
    phase === "running" ? (multi ? `Node ${current + 1} de ${nodes.length}: ${nodes[current]}` : "Em andamento…")
      : phase === "done" ? "Concluído"
      : phase === "cancelled" ? "Cancelado"
      : `Falhou${multi ? ` em ${nodes[current]}` : ""}`;

  return (
    <Dialog open onOpenChange={open => !open && !running && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ServerCog className="w-5 h-5" />
            {multi ? `Drain de ${nodes.length} nodes` : "Drain do node"}
          </DialogTitle>
          <DialogDescription className="font-mono">{multi ? `${cluster}` : `${nodes[0]} • ${cluster}`}</DialogDescription>
        </DialogHeader>

        {phase === "config" ? (
          <div className="space-y-2">
            {multi && (
              <ScrollArea className="max-h-28 rounded-md border">
                <ul className="p-2 space-y-0.5">
                  {nodes.map((n, i) => (
                    <li key={n} className="text-xs font-mono truncate" title={n}>{i + 1}. {n}</li>
                  ))}
                </ul>
              </ScrollArea>
            )}
            <p className="text-xs text-muted-foreground">
              Como o <span className="font-mono">kubectl drain</span>: {multi ? "cada node, um de cada vez," : "o node"} é marcado como
              unschedulable (cordon) e os pods são removidos via Eviction API, respeitando PodDisruptionBudgets (nova tentativa a cada 5s
              até o timeout). Mirror/static pods são ignorados.{multi && " Se um node falhar, os seguintes não são drenados."}
            </p>
            <div className="divide-y rounded-md border px-3">
              {optionRow("ignore_daemonsets", "Ignorar DaemonSets", "Pods de DaemonSet continuam no node (o controller os recriaria de qualquer forma).")}
              {optionRow("delete_emptydir_data", "Apagar dados emptyDir", "Permite remover pods com volumes emptyDir — os dados desses volumes se perdem.")}
              {optionRow("force", "Force", "Remove também pods sem controller (Deployment/StatefulSet/Job) — eles NÃO serão recriados.", true)}
              {optionRow("disable_eviction", "Ignorar PodDisruptionBudgets", "Usa DELETE direto em vez da Eviction API — pode derrubar todas as réplicas de uma vez.", true)}
            </div>
            <div className="grid grid-cols-2 gap-3 pt-1">
              <div>
                <Label className="text-xs">Grace period (s)</Label>
                <Input type="number" min={0} value={opts.grace_period} onChange={e => setOpts(o => ({ ...o, grace_period: Math.max(0, Number(e.target.value) || 0) }))} className="h-8" />
              </div>
              <div>
                <Label className="text-xs">Timeout</Label>
                <Input value={opts.timeout} onChange={e => setOpts(o => ({ ...o, timeout: e.target.value }))} placeholder="5m" className="h-8 font-mono" />
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-2 text-xs">
                <span className="text-muted-foreground truncate">{statusLabel}</span>
                <span className="font-mono flex-shrink-0">{progress.evicted}/{progress.total} pods</span>
              </div>
              <Progress value={percent} className="h-2" />
              {multi && (
                <div className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                  <span>Nodes concluídos</span>
                  <span className="font-mono">{doneCount}/{nodes.length}</span>
                </div>
              )}
            </div>
            <ScrollArea className="h-64 rounded-md border p-2">
              {events.map((e, i) => eventLine(e, i, events))}
              {running && events.length === 0 && <div className="text-xs text-muted-foreground">Iniciando…</div>}
            </ScrollArea>
            {phase === "cancelled" && (
              <p className="text-xs text-amber-600 dark:text-amber-400 flex items-start gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                Drain cancelado. Os pods já removidos continuam removidos e os nodes já processados continuam em cordon (use Uncordon para liberar).
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          {phase === "config" && (
            <>
              <Button variant="outline" onClick={onClose}>Cancelar</Button>
              <Button variant="destructive" onClick={start}>
                <ServerCog className="w-4 h-4 mr-2" />
                {multi ? `Iniciar drain (${nodes.length})` : "Iniciar drain"}
              </Button>
            </>
          )}
          {running && (
            <Button variant="outline" onClick={() => abortRef.current?.abort()}>
              <X className="w-4 h-4 mr-2" />
              Cancelar drain
            </Button>
          )}
          {!running && phase !== "config" && <Button onClick={onClose}>Fechar</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── Delete ───────────────────────────────────────────────────────────────────

function DeleteNodeDialog({ cluster, node, onClose, onDeleted }: { cluster: string; node: string; onClose: () => void; onDeleted: () => void }) {
  const [confirmText, setConfirmText] = useState("");
  const [deleting, setDeleting] = useState(false);

  const doDelete = async () => {
    setDeleting(true);
    try {
      await apiClient.deleteClusterNode(cluster, node);
      toast.success(`Node ${node} removido da API do cluster`);
      onDeleted();
      onClose();
    } catch (err) {
      toast.error("Erro ao deletar node", { description: err instanceof Error ? err.message : String(err) });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open onOpenChange={open => !open && !deleting && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-destructive">
            <Trash2 className="w-5 h-5" />
            Deletar node
          </DialogTitle>
          <DialogDescription className="font-mono">{node} • {cluster}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 text-sm">
          <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs space-y-1.5">
            <p className="font-semibold text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
              <AlertTriangle className="w-3.5 h-3.5" />
              Isso remove só o objeto Node da API (igual kubectl/k9s)
            </p>
            <p className="text-muted-foreground">
              A VM continua existindo e sendo cobrada na cloud, e o kubelet pode registrar o node de novo. Os pods dele são removidos da API
              sem respeitar PodDisruptionBudgets — faça <span className="font-mono">drain</span> antes. Para remover a VM, reduza o node pool
              ou apague a instância pela cloud.
            </p>
          </div>
          <div>
            <Label className="text-xs">Digite o nome do node para confirmar</Label>
            <Input value={confirmText} onChange={e => setConfirmText(e.target.value)} placeholder={node} className="font-mono h-8 mt-1" autoFocus />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={deleting}>Cancelar</Button>
          <Button variant="destructive" onClick={doDelete} disabled={confirmText !== node || deleting}>
            {deleting ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Trash2 className="w-4 h-4 mr-2" />}
            Deletar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
