import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Server, Activity, AlertTriangle, CheckCircle2, Package, Terminal, Copy, Tag, Tags } from "lucide-react";
import type { NodeDetailsResponse } from "@/lib/api/types";
import { format } from "date-fns";
import { toast } from "sonner";

interface NodeDetailsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  nodeDetails: NodeDetailsResponse | null;
  loading: boolean;
}

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
  loading,
}: NodeDetailsModalProps) {
  if (!nodeDetails) return null;

  const { node, pods, events, kubectl_describe } = nodeDetails;

  const formatBytes = (bytes: number) => {
    if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GiB`;
    if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(2)} MiB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(2)} KiB`;
    return `${bytes} B`;
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[1400px] max-h-[95vh] overflow-hidden">
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

                  {/* Taints */}
                  <div className="p-4 border rounded-lg">
                    <h4 className="text-xs font-medium mb-2 flex items-center gap-2">
                      <AlertTriangle className="w-3 h-3" />
                      Taints {node.taints && node.taints.length > 0 && `(${node.taints.length})`}
                    </h4>
                    {node.taints && node.taints.length > 0 ? (
                      <div className="space-y-2 max-h-40 overflow-y-auto">
                        {node.taints.map((taint, index) => (
                          <div key={index} className="p-2 border rounded text-xs space-y-1">
                            <div className="flex items-center justify-between gap-1">
                              <Badge variant="secondary" className="font-mono text-[10px] px-1 py-0">
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
                              <div className="text-muted-foreground">= {taint.value}</div>
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
                      <div className="text-xs text-muted-foreground">No taints</div>
                    )}
                  </div>

                  {/* Labels */}
                  <div className="p-4 border rounded-lg col-span-2">
                    <h4 className="text-xs font-medium mb-2 flex items-center gap-2">
                      <Tags className="w-3 h-3" />
                      Labels {node.labels && `(${Object.keys(node.labels).length})`}
                    </h4>
                    {node.labels && Object.keys(node.labels).length > 0 ? (
                      <div className="grid grid-cols-2 gap-2 max-h-40 overflow-y-auto">
                        {Object.entries(node.labels).slice(0, 10).map(([key, value]) => (
                          <div key={key} className="p-1.5 border rounded flex items-center justify-between gap-1 text-xs">
                            <div className="flex items-center gap-1 flex-1 min-w-0">
                              <Badge variant="outline" className="font-mono text-[10px] px-1 py-0 shrink-0">
                                {key.split('/').pop()}
                              </Badge>
                              <span className="text-muted-foreground truncate text-[10px]">{value}</span>
                            </div>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-4 w-4 p-0 shrink-0"
                              onClick={() => {
                                navigator.clipboard.writeText(`${key}=${value}`);
                                toast.success(`Label copiado!`);
                              }}
                            >
                              <Copy className="h-2.5 w-2.5" />
                            </Button>
                          </div>
                        ))}
                        {Object.keys(node.labels).length > 10 && (
                          <div className="col-span-2 text-center text-xs text-muted-foreground">
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
                pods.map((pod, index) => (
                  <div key={index} className="p-4 border rounded-lg">
                    <div className="flex items-start justify-between mb-2">
                      <div>
                        <h4 className="text-sm font-medium flex items-center gap-2">
                          <Package className="w-4 h-4" />
                          {pod.name}
                        </h4>
                        <p className="text-xs text-muted-foreground">Namespace: {pod.namespace}</p>
                      </div>
                      <Badge variant={pod.phase === "Running" ? "default" : "secondary"}>
                        {pod.phase}
                      </Badge>
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-xs mt-3">
                      <div>
                        <span className="text-muted-foreground">CPU Request:</span>{" "}
                        <span className="font-mono">{pod.cpu_request || "N/A"}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">CPU Limit:</span>{" "}
                        <span className="font-mono">{pod.cpu_limit || "N/A"}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Memory Request:</span>{" "}
                        <span className="font-mono">{pod.memory_request || "N/A"}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Memory Limit:</span>{" "}
                        <span className="font-mono">{pod.memory_limit || "N/A"}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Restarts:</span>{" "}
                        <Badge variant={pod.restart_count > 3 ? "destructive" : "secondary"}>
                          {pod.restart_count}
                        </Badge>
                      </div>
                    </div>
                  </div>
                ))
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
