import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Server, Activity, AlertTriangle, CheckCircle2, Package, Terminal } from "lucide-react";
import type { NodeDetailsResponse } from "@/lib/api/types";
import { format } from "date-fns";

interface NodeDetailsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  nodeDetails: NodeDetailsResponse | null;
  loading: boolean;
}

const StatusBadge = ({ status }: { status: string }) => {
  const getVariant = () => {
    if (status === "Ready") return "success";
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
      <DialogContent className="max-w-6xl max-h-[90vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Server className="w-5 h-5" />
            {node.name}
            <StatusBadge status={node.status} />
          </DialogTitle>
          <DialogDescription>
            Node Pool: {node.node_pool_name} | Cluster: {node.cluster_name}
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="overview" className="w-full">
          <TabsList className="grid w-full grid-cols-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="pods">Pods ({node.pods_total})</TabsTrigger>
            <TabsTrigger value="events">Events ({events.length})</TabsTrigger>
            <TabsTrigger value="describe">kubectl describe</TabsTrigger>
          </TabsList>

          <ScrollArea className="max-h-[calc(90vh-180px)] mt-4">
            {/* Overview Tab */}
            <TabsContent value="overview" className="space-y-6 mt-0 pr-4">
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
                </div>
              </div>

              <Separator />

              {/* Conditions */}
              {node.conditions && node.conditions.length > 0 && (
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
                              <CheckCircle2 className="w-4 h-4 text-success" />
                            ) : (
                              <AlertTriangle className="w-4 h-4 text-warning" />
                            )}
                            <span className="font-medium text-sm">{condition.type}</span>
                          </div>
                          <Badge variant={condition.status === "True" ? "success" : "secondary"} className="text-xs">
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
              )}
            </TabsContent>

            {/* Pods Tab */}
            <TabsContent value="pods" className="space-y-3 mt-0 pr-4">
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
                      <Badge variant={pod.phase === "Running" ? "success" : "secondary"}>
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
            <TabsContent value="events" className="space-y-3 mt-0 pr-4">
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
            <TabsContent value="describe" className="mt-0 pr-4">
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
