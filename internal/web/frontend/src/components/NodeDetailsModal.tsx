import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Server, Activity, AlertTriangle, CheckCircle2, Package, Copy, Tag, Tags, Cpu, HardDrive } from "lucide-react";
import type { NodeDetailsResponse, PodOnNode } from "@/lib/api/types";
import { format } from "date-fns";
import { toast } from "sonner";
import { getVMSpecs, formatVMSpecs, formatDiskSpecs } from "@/lib/azure-vm-specs";

interface NodeDetailsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  nodeDetails: NodeDetailsResponse | null;
  loading?: boolean;
  vmSize?: string;
}

// Gauge semicircular SVG para exibir % de uso de CPU/Memória por pod
const ResourceGauge = ({
  pct,
  label,
  dimmed = false,
}: {
  pct: number;
  label: string;
  dimmed?: boolean;
}) => {
  const clamped = Math.min(100, Math.max(0, pct));
  const r = 22;
  const cx = 28;
  const cy = 28;
  const startX = cx - r;
  const startY = cy;
  const angleRad = (clamped / 100) * Math.PI;
  const endX = cx + r * Math.cos(Math.PI - angleRad);
  const endY = cy - r * Math.sin(angleRad);
  // largeArc SEMPRE 0: o arco preenchido é sempre ≤ 180° (semicírculo),
  // usar 1 acima de 50% faz o SVG traçar o caminho pelo lado de baixo (fora do viewBox).
  const largeArc = 0;

  const color =
    clamped >= 90 ? "#ef4444" : clamped >= 70 ? "#f97316" : clamped >= 50 ? "#eab308" : "#22c55e";

  return (
    <div className={`flex flex-col items-center gap-0.5 min-w-[52px] flex-shrink-0${dimmed ? " opacity-30" : ""}`}>
      <svg viewBox="0 0 56 32" className="w-14">
        {/* Track */}
        <path
          d={`M ${startX} ${startY} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`}
          fill="none"
          stroke="hsl(var(--muted))"
          strokeWidth="5"
          strokeLinecap="round"
        />
        {/* Fill */}
        {clamped > 0 && (
          <path
            d={`M ${startX} ${startY} A ${r} ${r} 0 ${largeArc} 1 ${endX} ${endY}`}
            fill="none"
            stroke={color}
            strokeWidth="5"
            strokeLinecap="round"
          />
        )}
        {/* % texto */}
        <text
          x={cx}
          y={cy - 3}
          textAnchor="middle"
          fontSize="9"
          fontWeight="700"
          fill={color}
        >
          {clamped.toFixed(0)}%
        </text>
      </svg>
      {/* Label fora do SVG — legível */}
      <span className="text-[10px] font-semibold text-muted-foreground leading-none">{label}</span>
    </div>
  );
};

const StatusBadge = ({ status }: { status: string }) => {
  const getVariant = () => {
    if (status === "Ready") return "default";
    if (status === "NotReady") return "destructive";
    return "secondary";
  };

  return (
    <Badge variant={getVariant() as any} className="ml-2">
      {status}
    </Badge>
  );
};

export default function NodeDetailsModal({
  open,
  onOpenChange,
  nodeDetails,
  vmSize,
}: NodeDetailsModalProps) {
  if (!nodeDetails) return null;

  const { node, pods, events, kubectl_describe } = nodeDetails;

  // Debug: Track node data
  console.log('🎯 [NodeDetailsModal] Rendering node:', node.name);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-w-[1400px] max-h-[95vh] overflow-hidden"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Server className="w-5 h-5" />
            {node.name}
            <StatusBadge status={node.status} />
          </DialogTitle>
          <DialogDescription className="space-y-2 mt-2">
            <div className="flex items-center gap-2 flex-wrap text-sm">
              <span className="text-muted-foreground">Node Pool:</span>
              <span className="font-medium">{node.node_pool_name}</span>
            </div>

            {/* Cluster Info */}
            <div className="flex items-center gap-3 flex-wrap text-sm">
              {/* Cluster Name */}
              <div className="flex items-center gap-1">
                <span className="text-muted-foreground">{node.cluster_name}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-4 w-4 p-0"
                  onClick={() => {
                    navigator.clipboard.writeText(node.cluster_name);
                    toast.success("Cluster name copiado!");
                  }}
                >
                  <Copy className="h-3 w-3" />
                </Button>
              </div>

              <span className="text-muted-foreground">•</span>

              {/* Resource Group */}
              <div className="flex items-center gap-1">
                <span className="text-muted-foreground">{node.resource_group}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-4 w-4 p-0"
                  onClick={() => {
                    navigator.clipboard.writeText(node.resource_group);
                    toast.success("Resource Group copiado!");
                  }}
                >
                  <Copy className="h-3 w-3" />
                </Button>
              </div>

              {node.subscription && (
                <>
                  <span className="text-muted-foreground">•</span>

                  {/* Subscription */}
                  <div className="flex items-center gap-1">
                    {node.subscription_name ? (
                      <>
                        <span className="text-muted-foreground">{node.subscription_name}</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-4 w-4 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(node.subscription_name || '');
                            toast.success("Subscription name copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                        <span className="text-xs text-muted-foreground/60 font-mono">({node.subscription})</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-4 w-4 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(node.subscription);
                            toast.success("Subscription ID copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </>
                    ) : (
                      <>
                        <span className="text-muted-foreground font-mono">{node.subscription}</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-4 w-4 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(node.subscription);
                            toast.success("Subscription ID copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </>
                    )}
                  </div>
                </>
              )}

              {/* Cluster Tags */}
              {node.cluster_tags && Object.keys(node.cluster_tags).length > 0 && (
                <Popover>
                  <PopoverTrigger asChild>
                    <Button variant="outline" size="sm" className="h-5 gap-1 px-2">
                      <Tag className="h-3 w-3" />
                      <span className="text-xs">Tags ({Object.keys(node.cluster_tags).length})</span>
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-80">
                    <div className="space-y-2">
                      <h4 className="font-semibold text-sm">Cluster Tags</h4>
                      <Separator />
                      <div className="space-y-1 max-h-60 overflow-y-auto">
                        {Object.entries(node.cluster_tags).map(([key, value]) => (
                          <div key={key} className="flex items-center justify-between gap-2 text-xs">
                            <div className="flex items-start gap-2 flex-1 min-w-0">
                              <Badge variant="secondary" className="font-mono text-xs shrink-0">
                                {key}
                              </Badge>
                              <span className="text-muted-foreground break-all">{value}</span>
                            </div>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-5 w-5 p-0 shrink-0"
                              onClick={() => {
                                navigator.clipboard.writeText(`${key}=${value}`);
                                toast.success(`Tag ${key} copiada!`);
                              }}
                            >
                              <Copy className="h-3 w-3" />
                            </Button>
                          </div>
                        ))}
                      </div>
                    </div>
                  </PopoverContent>
                </Popover>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="overview" className="w-full h-full flex flex-col">
          <TabsList className="grid w-full grid-cols-4 flex-shrink-0">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="pods">Pods ({node.pods_total})</TabsTrigger>
            <TabsTrigger value="events">Events ({events.length})</TabsTrigger>
            <TabsTrigger value="describe">kubectl describe</TabsTrigger>
          </TabsList>

          <ScrollArea className="h-[calc(95vh-220px)] mt-4 pr-4">
            {/* Overview Tab */}
            <TabsContent value="overview" className="space-y-6 mt-0">
              {/* Basic Info */}
              <div>
                <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                  <Server className="w-4 h-4" />
                  Basic Information
                </h3>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <span className="text-muted-foreground">Hostname:</span>{" "}
                    <span className="font-mono">{node.hostname}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Internal IP:</span>{" "}
                    <span className="font-mono">{node.internal_ip}</span>
                  </div>
                  {node.external_ip && (
                    <div>
                      <span className="text-muted-foreground">External IP:</span>{" "}
                      <span className="font-mono">{node.external_ip}</span>
                    </div>
                  )}
                  <div>
                    <span className="text-muted-foreground">Kubernetes Version:</span>{" "}
                    <span className="font-mono">{node.kubernetes_version}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Age:</span>{" "}
                    <span>{node.age}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Created At:</span>{" "}
                    <span>{format(new Date(node.created_at), "yyyy-MM-dd HH:mm:ss")}</span>
                  </div>
                </div>
              </div>

              <Separator />

              {/* VM Configuration */}
              {vmSize && (() => {
                const vmSpecs = getVMSpecs(vmSize);
                const specsFormatted = formatVMSpecs(vmSize);
                const diskSpecsFormatted = formatDiskSpecs(vmSize);
                return (
                  <>
                    <div>
                      <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                        <Cpu className="w-4 h-4" />
                        VM Configuration
                      </h3>
                      <div className="grid grid-cols-2 gap-3 text-sm">
                        <div>
                          <span className="text-muted-foreground">VM Size:</span>{" "}
                          <span className="font-mono font-medium">{vmSize}</span>
                        </div>
                        {vmSpecs && (
                          <>
                            <div>
                              <span className="text-muted-foreground">vCPUs:</span>{" "}
                              <span className="font-medium">{vmSpecs.vCPUs}</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Memory:</span>{" "}
                              <span className="font-medium">{vmSpecs.memoryGiB} GiB</span>
                            </div>
                            {vmSpecs.family && (
                              <div>
                                <span className="text-muted-foreground">Family:</span>{" "}
                                <span className="font-medium">{vmSpecs.family}</span>
                              </div>
                            )}
                          </>
                        )}
                      </div>
                      {vmSpecs?.description && (
                        <p className="text-xs text-muted-foreground mt-2">{vmSpecs.description}</p>
                      )}
                      {specsFormatted && (
                        <p className="text-sm font-medium text-primary mt-2">{specsFormatted}</p>
                      )}
                      {diskSpecsFormatted && (
                        <div className="mt-3 p-3 bg-muted/50 rounded-lg flex items-start gap-2 text-sm">
                          <HardDrive className="w-4 h-4 text-muted-foreground mt-0.5 flex-shrink-0" />
                          <div>
                            <p className="text-xs text-muted-foreground mb-1">Disk Performance</p>
                            <p className="text-xs font-medium">{diskSpecsFormatted}</p>
                          </div>
                        </div>
                      )}
                    </div>
                    <Separator />
                  </>
                );
              })()}

              {/* Resources */}
              <div>
                <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                  <Activity className="w-4 h-4" />
                  Resources
                </h3>
                <div className="grid grid-cols-2 gap-4">
                  {/* CPU */}
                  <div className="p-4 border rounded-lg">
                    <h4 className="text-xs font-medium mb-2">CPU</h4>
                    <div className="space-y-1 text-sm">
                      <div>
                        <span className="text-muted-foreground">Capacity:</span>{" "}
                        <span className="font-mono">{node.cpu_capacity}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Allocatable:</span>{" "}
                        <span className="font-mono">{node.cpu_allocatable}</span>
                      </div>
                      {node.cpu_used && (
                        <>
                          <div>
                            <span className="text-muted-foreground">Used:</span>{" "}
                            <span className="font-mono">{node.cpu_used}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Usage:</span>{" "}
                            <Badge variant={node.cpu_usage_percent > 80 ? "destructive" : "secondary"}>
                              {node.cpu_usage_percent.toFixed(1)}%
                            </Badge>
                          </div>
                        </>
                      )}
                    </div>
                  </div>

                  {/* Memory */}
                  <div className="p-4 border rounded-lg">
                    <h4 className="text-xs font-medium mb-2">Memory</h4>
                    <div className="space-y-1 text-sm">
                      <div>
                        <span className="text-muted-foreground">Capacity:</span>{" "}
                        <span className="font-mono">{node.memory_capacity}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Allocatable:</span>{" "}
                        <span className="font-mono">{node.memory_allocatable}</span>
                      </div>
                      {node.memory_used && (
                        <>
                          <div>
                            <span className="text-muted-foreground">Used:</span>{" "}
                            <span className="font-mono">{node.memory_used}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Usage:</span>{" "}
                            <Badge variant={node.memory_usage_percent > 80 ? "destructive" : "secondary"}>
                              {node.memory_usage_percent.toFixed(1)}%
                            </Badge>
                          </div>
                        </>
                      )}
                    </div>
                  </div>

                  {/* Pods */}
                  <div className="p-4 border rounded-lg">
                    <h4 className="text-xs font-medium mb-2">Pods</h4>
                    <div className="space-y-1 text-sm">
                      <div>
                        <span className="text-muted-foreground">Capacity:</span>{" "}
                        <span className="font-mono">{node.pods_capacity}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Allocatable:</span>{" "}
                        <span className="font-mono">{node.pods_allocatable}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Running:</span>{" "}
                        <span className="font-mono">{node.pods_running}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Total:</span>{" "}
                        <span className="font-mono">{node.pods_total}</span>
                      </div>
                    </div>
                  </div>

                  {/* Taints & Labels */}
                  <div className="p-4 border rounded-lg">
                    {/* Taints */}
                    <h4 className="text-xs font-medium mb-2 flex items-center gap-2">
                      <AlertTriangle className="w-3 h-3" />
                      Taints {node.taints && node.taints.length > 0 && `(${node.taints.length})`}
                    </h4>
                    {node.taints && node.taints.length > 0 ? (
                      <div className="space-y-2 max-h-32 overflow-y-auto mb-4">
                        {node.taints.map((taint, index) => (
                          <div key={index} className="p-2 border rounded text-xs space-y-1">
                            <div className="flex items-center justify-between gap-1">
                              <Badge
                                variant="secondary"
                                className="font-mono text-[10px] px-1 py-0 truncate max-w-[150px]"
                                title={taint.key}
                              >
                                {taint.key}
                              </Badge>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-4 w-4 p-0"
                                onClick={() => {
                                  const taintStr = taint.value
                                    ? `${taint.key}=${taint.value}:${taint.effect}`
                                    : `${taint.key}:${taint.effect}`;
                                  navigator.clipboard.writeText(taintStr);
                                  toast.success("Taint copiado!");
                                }}
                              >
                                <Copy className="h-2.5 w-2.5" />
                              </Button>
                            </div>
                            {taint.value && (
                              <div className="text-muted-foreground truncate" title={taint.value}>= {taint.value}</div>
                            )}
                            <Badge variant={
                              taint.effect === "NoSchedule" ? "destructive" :
                              taint.effect === "NoExecute" ? "destructive" : "secondary"
                            } className="text-[10px] px-1 py-0">
                              {taint.effect}
                            </Badge>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="text-xs text-muted-foreground mb-4">No taints</div>
                    )}

                    {/* Labels */}
                    <h4 className="text-xs font-medium mb-2 flex items-center gap-2">
                      <Tags className="w-3 h-3" />
                      Labels {node.labels && `(${Object.keys(node.labels).length})`}
                    </h4>
                    {node.labels && Object.keys(node.labels).length > 0 ? (
                      <div className="space-y-2 max-h-32 overflow-y-auto">
                        {Object.entries(node.labels).slice(0, 10).map(([key, value]) => (
                          <div key={key} className="p-1.5 border rounded flex items-center justify-between gap-1 text-xs">
                            <div className="flex items-center gap-1 flex-1 min-w-0">
                              <Badge
                                variant="outline"
                                className="font-mono text-[10px] px-1 py-0 shrink-0 max-w-[100px] truncate"
                                title={key}
                              >
                                {key.split('/').pop()}
                              </Badge>
                              <span
                                className="text-muted-foreground truncate text-[10px]"
                                title={value}
                              >
                                {value}
                              </span>
                            </div>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-4 w-4 p-0 shrink-0"
                              onClick={() => {
                                // IMPORTANTE: Copia valor COMPLETO (não truncado)
                                navigator.clipboard.writeText(`${key}=${value}`);
                                toast.success(`Label copiado!`);
                              }}
                            >
                              <Copy className="h-2.5 w-2.5" />
                            </Button>
                          </div>
                        ))}
                        {Object.keys(node.labels).length > 10 && (
                          <div className="text-center text-xs text-muted-foreground">
                            +{Object.keys(node.labels).length - 10} mais labels...
                          </div>
                        )}
                      </div>
                    ) : (
                      <div className="text-xs text-muted-foreground">No labels</div>
                    )}
                  </div>
                </div>
              </div>

              <Separator />

              {/* Conditions */}
              {node.conditions && node.conditions.length > 0 && (
                <>
                  <div>
                    <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                      <AlertTriangle className="w-4 h-4" />
                      Conditions
                    </h3>
                    <div className="space-y-2">
                      {node.conditions.map((condition, index) => (
                        <div key={index} className="p-3 border rounded-lg">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              {condition.status === "True" ? (
                                <CheckCircle2 className="w-4 h-4 text-green-600" />
                              ) : (
                                <AlertTriangle className="w-4 h-4 text-yellow-600" />
                              )}
                              <span className="font-medium text-sm">{condition.type}</span>
                            </div>
                            <Badge variant={condition.status === "True" ? "default" : "secondary"} className="text-xs">
                              {condition.status}
                            </Badge>
                          </div>
                          {condition.message && (
                            <p className="text-xs text-muted-foreground mt-2">{condition.message}</p>
                          )}
                          {condition.reason && (
                            <p className="text-xs text-muted-foreground mt-1">Reason: {condition.reason}</p>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>

                  <Separator />
                </>
              )}
            </TabsContent>

            {/* Pods Tab */}
            <TabsContent value="pods" className="space-y-3 mt-0">
              {pods.length === 0 ? (
                <div className="text-center text-muted-foreground py-8">
                  No pods running on this node
                </div>
              ) : (
                pods.map((pod: PodOnNode, index) => {
                  const hasMetrics = pod.cpu_usage !== undefined || pod.memory_usage !== undefined;
                  // Fallback: usa request vs limit quando não há métricas reais
                  const cpuPct = pod.cpu_usage_pct ?? 0;
                  const memPct = pod.memory_usage_pct ?? 0;

                  return (
                    <div key={index} className="p-4 border rounded-lg">
                      {/* Header: nome + namespace + fase + restarts */}
                      <div className="flex items-start justify-between mb-3">
                        <div className="flex-1 min-w-0">
                          <h4 className="text-sm font-medium flex items-center gap-2 truncate">
                            <Package className="w-4 h-4 flex-shrink-0" />
                            <span className="truncate" title={pod.name}>{pod.name}</span>
                          </h4>
                          <p className="text-xs text-muted-foreground mt-0.5">{pod.namespace}</p>
                        </div>
                        <div className="flex items-center gap-2 flex-shrink-0 ml-2">
                          <Badge variant={pod.restart_count > 3 ? "destructive" : "secondary"} className="text-xs">
                            {pod.restart_count} restart{pod.restart_count !== 1 ? "s" : ""}
                          </Badge>
                          <Badge variant={pod.phase === "Running" ? "default" : "secondary"} className="text-xs">
                            {pod.phase}
                          </Badge>
                        </div>
                      </div>

                      {/* Resources: CPU e Memory em dois painéis lado a lado */}
                      <div className="grid grid-cols-2 gap-2 mt-1">
                        {/* CPU */}
                        <div className="flex items-center justify-center gap-5 p-3 bg-muted/30 rounded-lg">
                          <ResourceGauge pct={cpuPct} label="CPU" dimmed={!hasMetrics} />
                          <div className="text-xs space-y-1">
                            {hasMetrics && pod.cpu_usage && (
                              <div>
                                <span className="text-muted-foreground">Uso: </span>
                                <span className="font-mono font-semibold">{pod.cpu_usage}</span>
                              </div>
                            )}
                            <div>
                              <span className="text-muted-foreground">Req: </span>
                              <span className="font-mono">{pod.cpu_request || "—"}</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Lim: </span>
                              <span className="font-mono">{pod.cpu_limit || "—"}</span>
                            </div>
                          </div>
                        </div>

                        {/* Memory */}
                        <div className="flex items-center justify-center gap-5 p-3 bg-muted/30 rounded-lg">
                          <ResourceGauge pct={memPct} label="MEM" dimmed={!hasMetrics} />
                          <div className="text-xs space-y-1">
                            {hasMetrics && pod.memory_usage && (
                              <div>
                                <span className="text-muted-foreground">Uso: </span>
                                <span className="font-mono font-semibold">{pod.memory_usage}</span>
                              </div>
                            )}
                            <div>
                              <span className="text-muted-foreground">Req: </span>
                              <span className="font-mono">{pod.memory_request || "—"}</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Lim: </span>
                              <span className="font-mono">{pod.memory_limit || "—"}</span>
                            </div>
                          </div>
                        </div>
                      </div>

                      {!hasMetrics && (
                        <p className="text-[10px] text-muted-foreground/50 mt-1.5 text-center">
                          Metrics Server indisponível — exibindo requests/limits
                        </p>
                      )}
                    </div>
                  );
                })
              )}
            </TabsContent>

            {/* Events Tab */}
            <TabsContent value="events" className="space-y-3 mt-0">
              {events.length === 0 ? (
                <div className="text-center text-muted-foreground py-8">
                  No recent events
                </div>
              ) : (
                events.map((event, index) => (
                  <div key={index} className="p-4 border rounded-lg">
                    <div className="flex items-start justify-between mb-2">
                      <h4 className="text-sm font-medium">{event.reason}</h4>
                      <Badge variant={event.type === "Warning" ? "destructive" : "secondary"}>
                        {event.type}
                      </Badge>
                    </div>
                    <p className="text-sm text-muted-foreground mb-2">{event.message}</p>
                    <div className="flex items-center gap-4 text-xs text-muted-foreground">
                      <span>Count: {event.count}</span>
                      <span>First: {format(new Date(event.first_timestamp), "yyyy-MM-dd HH:mm:ss")}</span>
                      <span>Last: {format(new Date(event.last_timestamp), "yyyy-MM-dd HH:mm:ss")}</span>
                    </div>
                    {event.source_component && (
                      <p className="text-xs text-muted-foreground mt-1">
                        Source: {event.source_component}
                      </p>
                    )}
                  </div>
                ))
              )}
            </TabsContent>

            {/* kubectl describe Tab */}
            <TabsContent value="describe" className="mt-0">
              {kubectl_describe ? (
                <div className="p-4 border rounded-lg bg-muted/30">
                  <pre className="text-xs font-mono whitespace-pre-wrap overflow-x-auto">
                    {kubectl_describe}
                  </pre>
                </div>
              ) : (
                <div className="text-center text-muted-foreground py-8">
                  kubectl describe output not available
                </div>
              )}
            </TabsContent>
          </ScrollArea>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
