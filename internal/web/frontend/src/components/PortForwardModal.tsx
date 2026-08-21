import { useEffect, useMemo, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Network,
  Loader2,
  Play,
  Square,
  Copy,
  ExternalLink,
  ChevronsUpDown,
  Check,
  ArrowUp,
  ArrowDown,
  Info,
  AlertTriangle,
  Globe,
  Lock,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { useClusters } from "@/hooks/useAPI";
import { usePortForwardSessions, useStartPortForward, useStopPortForward } from "@/hooks/usePortForward";
import { formatBytes } from "@/lib/monitorUtils";
import { cn } from "@/lib/utils";
import { ProtectedAction } from "@/components/rbac";
import type { PortForwardSession, PortForwardStatus } from "@/lib/api/types";

// Sugestões de porta comuns — puramente UX (o backend aceita qualquer porta 1-65535, não valida
// contra esta lista). Junto com as portas reais declaradas pelos containers do pod
// (getPortForwardPodPorts), formam o popover de "portas sugeridas" no formulário.
const COMMON_PORTS: { port: number; label: string }[] = [
  { port: 80, label: "HTTP" },
  { port: 8080, label: "HTTP (alt)" },
  { port: 443, label: "HTTPS" },
  { port: 3000, label: "Node/Dev" },
  { port: 5000, label: "HTTP (alt)" },
  { port: 9090, label: "Prometheus" },
  { port: 5432, label: "PostgreSQL" },
  { port: 3306, label: "MySQL/MariaDB" },
  { port: 6379, label: "Redis" },
  { port: 27017, label: "MongoDB" },
  { port: 9092, label: "Kafka" },
  { port: 9200, label: "Elasticsearch" },
];

interface PortForwardModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Pré-preenche o formulário quando aberto a partir do contexto de um pod específico (ex:
   *  PodQuickViewModal) — sem isso, abre com o seletor cluster→namespace→pod vazio. */
  initialCluster?: string;
  initialNamespace?: string;
  initialPod?: string;
  initialWorkload?: string;
}

function SimpleSearchableSelect({
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

function statusBadge(status: PortForwardStatus) {
  switch (status) {
    case "running":
      return <Badge className="gap-1 bg-green-500/10 text-green-500 border-green-500/30" variant="outline">
        <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" /> Ativo
      </Badge>;
    case "starting":
      return <Badge className="gap-1 bg-blue-500/10 text-blue-400 border-blue-500/30" variant="outline">
        <Loader2 className="h-3 w-3 animate-spin" /> Iniciando
      </Badge>;
    case "reconnecting":
      return <Badge className="gap-1 bg-amber-500/10 text-amber-500 border-amber-500/30" variant="outline">
        <Loader2 className="h-3 w-3 animate-spin" /> Reconectando
      </Badge>;
    case "error":
      return <Badge className="gap-1 bg-red-500/10 text-red-500 border-red-500/30" variant="outline">
        <AlertTriangle className="h-3 w-3" /> Erro
      </Badge>;
    case "stopped":
      return <Badge className="gap-1 bg-muted text-muted-foreground border-border" variant="outline">Encerrado</Badge>;
  }
}

function formatUptime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 0) return "0s";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

function formatRelative(iso?: string): string {
  if (!iso) return "—";
  const ms = Date.now() - new Date(iso).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 5) return "agora";
  if (s < 60) return `há ${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `há ${m}min`;
  const h = Math.floor(m / 60);
  return `há ${h}h`;
}

function connectHost(session: PortForwardSession): string {
  if (session.bind_address === "127.0.0.1") return "127.0.0.1";
  return window.location.hostname;
}

/**
 * PortForwardModal — gerenciador completo de sessões de port-forward pra pods K8s. Túnel real
 * (mesmo mecanismo SPDY de `kubectl port-forward`) aberto pelo próprio servidor, com porta
 * local/bind address escolhidos pelo usuário, estatísticas ao vivo (bytes/conexões) e lista
 * global (todas as sessões, de todos os usuários — mesma transparência de outras ferramentas
 * server-side desta app). Ver PORT-FORWARD-PLAN.md.
 *
 * Dois pontos de entrada montam este componente: um genérico (botão "Port Forward" na barra de
 * abas, sem pré-preenchimento) e um contextual (PodQuickViewModal, pré-preenchendo cluster/
 * namespace/pod do pod sendo visto) — cada um com sua própria instância local de `open`, mas
 * compartilhando a mesma lista de sessões (globais no backend).
 */
export function PortForwardModal({
  open,
  onOpenChange,
  initialCluster,
  initialNamespace,
  initialPod,
  initialWorkload,
}: PortForwardModalProps) {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState(initialCluster || "");
  const [namespace, setNamespace] = useState(initialNamespace || "");
  const [pod, setPod] = useState(initialPod || "");
  const [remotePort, setRemotePort] = useState("");
  const [localPort, setLocalPort] = useState("");
  const [bindAll, setBindAll] = useState(true);
  const [label, setLabel] = useState("");
  const [portPopoverOpen, setPortPopoverOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    setCluster(initialCluster || "");
    setNamespace(initialNamespace || "");
    setPod(initialPod || "");
    setRemotePort("");
    setLocalPort("");
    setLabel("");
  }, [open, initialCluster, initialNamespace, initialPod]);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["pf-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: open && !!cluster,
  });

  const { data: pods = [] } = useQuery({
    queryKey: ["pf-pods", cluster, namespace],
    queryFn: () => apiClient.getPods(cluster, namespace ? [namespace] : undefined),
    enabled: open && !!cluster && !!namespace,
  });

  const { data: podPortsResp } = useQuery({
    queryKey: ["pf-pod-ports", cluster, namespace, pod],
    queryFn: () => apiClient.getPortForwardPodPorts(cluster, namespace, pod),
    enabled: open && !!cluster && !!namespace && !!pod,
  });
  const containerPorts = podPortsResp?.ports ?? [];

  const { data: sessions = [], isLoading: sessionsLoading, isError: sessionsError, refetch: refetchSessions } =
    usePortForwardSessions(open);
  const startMutation = useStartPortForward();
  const stopMutation = useStopPortForward();

  const selectedPod = useMemo(() => pods.find((p) => p.name === pod), [pods, pod]);

  const canStart = !!cluster && !!namespace && !!pod && !!remotePort && Number(remotePort) > 0 && Number(remotePort) <= 65535;

  const handleStart = async () => {
    if (!canStart) return;
    try {
      await startMutation.mutateAsync({
        cluster,
        namespace,
        pod,
        workload: initialWorkload,
        remote_port: Number(remotePort),
        local_port: localPort ? Number(localPort) : undefined,
        bind_address: bindAll ? "0.0.0.0" : "127.0.0.1",
        label: label.trim() || undefined,
      });
      toast.success(`Port-forward iniciado para ${namespace}/${pod}:${remotePort}`);
      setRemotePort("");
      setLocalPort("");
      setLabel("");
    } catch (e) {
      toast.error("Erro ao iniciar port-forward", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    }
  };

  const handleStop = async (session: PortForwardSession) => {
    try {
      await stopMutation.mutateAsync(session.id);
      toast.success(`Port-forward de ${session.namespace}/${session.pod} encerrado`);
    } catch (e) {
      toast.error("Erro ao encerrar port-forward", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    }
  };

  const handleCopy = (session: PortForwardSession) => {
    const text = `${connectHost(session)}:${session.local_port}`;
    navigator.clipboard.writeText(text).then(() => toast.success(`Copiado: ${text}`));
  };

  const activeSessions = sessions.filter((s) => s.status === "running" || s.status === "starting" || s.status === "reconnecting");
  const inactiveSessions = sessions.filter((s) => s.status === "error" || s.status === "stopped");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl h-[85vh] flex flex-col overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Network className="h-4 w-4" />
            Port Forward
          </DialogTitle>
          <DialogDescription>
            Abre um túnel real (mesmo mecanismo do <code>kubectl port-forward</code>) do servidor
            até um pod — a sessão fica ativa até você encerrar, ficar ociosa por muito tempo ou
            atingir o tempo máximo de {"8h"}.
          </DialogDescription>
        </DialogHeader>

        {/* Formulário de criação — sempre visível no topo, altura fixa */}
        <div className="flex-shrink-0 space-y-3 border rounded-lg p-3 bg-muted/20">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <div>
              <Label className="text-xs">Cluster</Label>
              <div className="mt-1">
                <SimpleSearchableSelect
                  value={cluster}
                  onChange={(v) => {
                    setCluster(v);
                    setNamespace("");
                    setPod("");
                  }}
                  options={clusters.map((c) => c.context)}
                  placeholder="Selecione..."
                  searchPlaceholder="Buscar cluster..."
                  emptyMessage="Nenhum cluster encontrado."
                />
              </div>
            </div>
            <div>
              <Label className="text-xs">Namespace</Label>
              <div className="mt-1">
                <SimpleSearchableSelect
                  value={namespace}
                  onChange={(v) => {
                    setNamespace(v);
                    setPod("");
                  }}
                  options={namespaces.map((n) => n.name)}
                  placeholder="Selecione..."
                  searchPlaceholder="Buscar namespace..."
                  emptyMessage="Nenhum namespace encontrado."
                  disabled={!cluster}
                />
              </div>
            </div>
            <div>
              <Label className="text-xs">Pod</Label>
              <div className="mt-1">
                <SimpleSearchableSelect
                  value={pod}
                  onChange={setPod}
                  options={pods.map((p) => p.name)}
                  placeholder="Selecione..."
                  searchPlaceholder="Buscar pod..."
                  emptyMessage="Nenhum pod encontrado."
                  disabled={!namespace}
                />
              </div>
            </div>
          </div>

          {selectedPod && selectedPod.phase !== "Running" && (
            <p className="text-[11px] text-amber-500 flex items-center gap-1">
              <AlertTriangle className="h-3 w-3" /> Este pod não está Running (fase: {selectedPod.phase}) — o túnel provavelmente vai falhar ao iniciar.
            </p>
          )}

          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 items-end">
            <div>
              <Label className="text-xs">Porta remota (no pod)</Label>
              <Popover open={portPopoverOpen} onOpenChange={setPortPopoverOpen}>
                <PopoverTrigger asChild>
                  <Input
                    className="mt-1 h-9 text-xs"
                    type="number"
                    min={1}
                    max={65535}
                    placeholder="ex: 8080"
                    value={remotePort}
                    onChange={(e) => setRemotePort(e.target.value)}
                    onFocus={() => setPortPopoverOpen(true)}
                  />
                </PopoverTrigger>
                <PopoverContent className="w-64 p-2" align="start" onOpenAutoFocus={(e) => e.preventDefault()}>
                  {containerPorts.length > 0 && (
                    <div className="mb-2">
                      <p className="text-[10px] text-muted-foreground mb-1 uppercase tracking-wide">Portas do pod</p>
                      <div className="flex flex-wrap gap-1">
                        {containerPorts.map((p, i) => (
                          <button
                            key={`${p.container}-${p.port}-${i}`}
                            className="text-[11px] px-2 py-1 rounded border border-border/60 hover:bg-accent transition-colors"
                            onClick={() => { setRemotePort(String(p.port)); setPortPopoverOpen(false); }}
                          >
                            {p.port}{p.name ? ` (${p.name})` : ""} <span className="text-muted-foreground">· {p.container}</span>
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                  <p className="text-[10px] text-muted-foreground mb-1 uppercase tracking-wide">Comuns</p>
                  <div className="flex flex-wrap gap-1">
                    {COMMON_PORTS.map((p) => (
                      <button
                        key={p.port}
                        className="text-[11px] px-2 py-1 rounded border border-border/60 hover:bg-accent transition-colors"
                        onClick={() => { setRemotePort(String(p.port)); setPortPopoverOpen(false); }}
                      >
                        {p.port} <span className="text-muted-foreground">{p.label}</span>
                      </button>
                    ))}
                  </div>
                </PopoverContent>
              </Popover>
            </div>
            <div>
              <Label className="text-xs">Porta local</Label>
              <Input
                className="mt-1 h-9 text-xs"
                type="number"
                min={1}
                max={65535}
                placeholder="Automática"
                value={localPort}
                onChange={(e) => setLocalPort(e.target.value)}
              />
            </div>
            <div className="col-span-2 sm:col-span-1">
              <Label className="text-xs">Rótulo (opcional)</Label>
              <Input
                className="mt-1 h-9 text-xs"
                placeholder="ex: banco local"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
            </div>
            <div className="col-span-2 sm:col-span-1 flex items-end">
              <ProtectedAction>
                <Button size="sm" className="w-full h-9" onClick={handleStart} disabled={!canStart || startMutation.isPending}>
                  {startMutation.isPending ? <Loader2 className="h-4 w-4 mr-1.5 animate-spin" /> : <Play className="h-4 w-4 mr-1.5" />}
                  Iniciar
                </Button>
              </ProtectedAction>
            </div>
          </div>

          <button
            type="button"
            className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
            onClick={() => setBindAll((v) => !v)}
          >
            {bindAll ? <Globe className="h-3 w-3" /> : <Lock className="h-3 w-3" />}
            {bindAll
              ? "Acessível pela rede (0.0.0.0) — qualquer host que alcance este servidor pode conectar durante a sessão"
              : "Somente nesta máquina (127.0.0.1) — clique para tornar acessível pela rede"}
          </button>
        </div>

        {/* Lista de sessões */}
        <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
          <div className="flex items-center justify-between flex-shrink-0 py-1.5">
            <p className="text-xs font-medium text-muted-foreground">
              Sessões ({sessions.length}) {sessionsLoading && <Loader2 className="inline h-3 w-3 ml-1 animate-spin" />}
            </p>
            {sessionsError && (
              <Button variant="ghost" size="sm" className="h-6 text-[11px] text-amber-500" onClick={() => refetchSessions()}>
                Falha ao atualizar — tentar de novo
              </Button>
            )}
          </div>
          <ScrollArea className="flex-1 min-h-0 pr-3">
            {sessions.length === 0 && !sessionsLoading ? (
              <p className="text-sm text-muted-foreground text-center py-8">
                Nenhuma sessão de port-forward ativa. Preencha o formulário acima e clique em "Iniciar".
              </p>
            ) : (
              <div className="space-y-2 pb-2">
                {[...activeSessions, ...inactiveSessions].map((s) => (
                  <div key={s.id} className="rounded border border-border/60 p-2.5 text-xs space-y-1.5">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 space-y-0.5">
                        <div className="flex items-center gap-1.5 flex-wrap">
                          {statusBadge(s.status)}
                          <span className="font-medium truncate">{s.namespace}/{s.pod}</span>
                          {s.workload && <span className="text-muted-foreground">({s.workload})</span>}
                          {s.label && <Badge variant="secondary" className="text-[10px]">{s.label}</Badge>}
                        </div>
                        <p className="text-muted-foreground">{s.cluster}</p>
                        {s.status === "error" && s.error && (
                          <p className="text-red-400 flex items-start gap-1 mt-1">
                            <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" /> {s.error}
                          </p>
                        )}
                        {s.status === "reconnecting" && s.error && (
                          <p className="text-amber-500 flex items-start gap-1 mt-1">
                            <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" /> {s.error}
                          </p>
                        )}
                        {s.status === "stopped" && s.error && (
                          <p className="text-muted-foreground flex items-start gap-1 mt-1">
                            <Info className="h-3 w-3 flex-shrink-0 mt-0.5" /> {s.error}
                          </p>
                        )}
                      </div>
                      <div className="flex items-center gap-1 flex-shrink-0">
                        {(s.status === "running" || s.status === "starting" || s.status === "reconnecting") && (
                          <ProtectedAction>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-7 w-7 text-destructive hover:text-destructive"
                              title="Encerrar"
                              disabled={stopMutation.isPending}
                              onClick={() => handleStop(s)}
                            >
                              <Square className="h-3.5 w-3.5" />
                            </Button>
                          </ProtectedAction>
                        )}
                      </div>
                    </div>

                    {(s.status === "running" || s.status === "starting" || s.status === "reconnecting") && (
                      <>
                        <div className="flex items-center gap-2 flex-wrap">
                          <code className="px-1.5 py-0.5 rounded bg-muted text-foreground">
                            {connectHost(s)}:{s.local_port}
                          </code>
                          <span className="text-muted-foreground">→ porta {s.remote_port} no pod</span>
                          <Button variant="ghost" size="icon" className="h-6 w-6" title="Copiar host:porta" onClick={() => handleCopy(s)}>
                            <Copy className="h-3 w-3" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            title="Abrir no navegador (assume HTTP — pode não funcionar para outros protocolos)"
                            onClick={() => window.open(`http://${connectHost(s)}:${s.local_port}`, "_blank")}
                          >
                            <ExternalLink className="h-3 w-3" />
                          </Button>
                        </div>
                        <div className="flex items-center gap-3 text-muted-foreground flex-wrap">
                          <span title="Conexões ativas / total">🔌 {s.connections_active} ativa(s) / {s.connections_total} total</span>
                          <span className="flex items-center gap-0.5" title="Enviado (cliente → pod)">
                            <ArrowUp className="h-3 w-3" /> {formatBytes(s.bytes_sent)}
                          </span>
                          <span className="flex items-center gap-0.5" title="Recebido (pod → cliente)">
                            <ArrowDown className="h-3 w-3" /> {formatBytes(s.bytes_received)}
                          </span>
                          <span title="Tempo desde que a sessão foi criada">⏱ {formatUptime(s.created_at)}</span>
                          <span title="Última conexão aceita">Última atividade: {formatRelative(s.last_activity)}</span>
                          {s.created_by && <span title="Quem iniciou">por {s.created_by}</span>}
                        </div>
                      </>
                    )}
                  </div>
                ))}
              </div>
            )}
          </ScrollArea>
        </div>
      </DialogContent>
    </Dialog>
  );
}
