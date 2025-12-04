import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { HardDrive, AlertTriangle, CheckCircle2, Info, Server, Database, Layers } from "lucide-react";
import type { NodePoolDiskMetrics } from "@/hooks/useNodePoolDiskMetrics";
import type { StorageOverview } from "@/lib/api/storage-types";
import { useEffect, useState } from "react";
import { apiClient } from "@/lib/api/client";

interface NodePoolDiskDetailsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  diskMetrics: NodePoolDiskMetrics | null;
  loading: boolean;
  vmSize?: string;
  cluster?: string;
}

// Componente de Gauge circular
const CircularGauge = ({ 
  value, 
  label, 
  size = 120,
  strokeWidth = 10,
  showPercentage = true 
}: { 
  value: number; 
  label: string; 
  size?: number;
  strokeWidth?: number;
  showPercentage?: boolean;
}) => {
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (value / 100) * circumference;
  
  const getColor = () => {
    if (value > 80) return "#ef4444"; // red
    if (value > 60) return "#f59e0b"; // amber
    return "#10b981"; // green
  };

  return (
    <div className="flex flex-col items-center gap-2">
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} className="transform -rotate-90">
          {/* Background circle */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke="currentColor"
            strokeWidth={strokeWidth}
            className="text-muted"
            opacity={0.2}
          />
          {/* Progress circle */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={getColor()}
            strokeWidth={strokeWidth}
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            strokeLinecap="round"
            className="transition-all duration-500"
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-2xl font-bold">{value.toFixed(1)}</span>
          {showPercentage && <span className="text-xs text-muted-foreground">%</span>}
        </div>
      </div>
      <p className="text-xs text-center text-muted-foreground max-w-[120px]">{label}</p>
    </div>
  );
};

export default function NodePoolDiskDetailsModal({
  open,
  onOpenChange,
  diskMetrics,
  loading,
  vmSize,
  cluster
}: NodePoolDiskDetailsModalProps) {
  const [storageOverview, setStorageOverview] = useState<StorageOverview | null>(null);
  const [loadingStorage, setLoadingStorage] = useState(false);

  useEffect(() => {
    if (open && cluster) {
      setLoadingStorage(true);
      apiClient.getStorageOverview(cluster)
        .then(response => {
          if (response.success && response.data) {
            setStorageOverview(response.data);
          }
        })
        .catch(error => {
          console.error("Failed to load storage overview:", error);
        })
        .finally(() => {
          setLoadingStorage(false);
        });
    }
  }, [open, cluster]);

  if (!diskMetrics) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <HardDrive className="w-5 h-5" />
            Disk Details - {diskMetrics.node_pool_name}
          </DialogTitle>
          <DialogDescription>
            Informações detalhadas de disco para todos os nodes do pool
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="nodes" className="w-full">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="nodes">Node Disks</TabsTrigger>
            <TabsTrigger value="storage">Storage Classes</TabsTrigger>
            <TabsTrigger value="pvcs">PVCs</TabsTrigger>
          </TabsList>

          <ScrollArea className="max-h-[calc(90vh-180px)] mt-4">
            <TabsContent value="nodes" className="space-y-6 mt-0">
            {/* Summary Section */}
            <div className="pr-4">
              <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                <Info className="w-4 h-4" />
                Pool Summary
              </h3>
              <div className="grid grid-cols-3 gap-4">
                <CircularGauge 
                  value={diskMetrics.usage_percent} 
                  label="Disk Usage"
                />
                <div className="flex flex-col justify-center space-y-2">
                  <div className="text-sm">
                    <span className="text-muted-foreground">Total:</span>
                    <span className="ml-2 font-medium">{(diskMetrics.total_bytes / (1024**3)).toFixed(1)} GiB</span>
                  </div>
                  <div className="text-sm">
                    <span className="text-muted-foreground">Used:</span>
                    <span className="ml-2 font-medium">{(diskMetrics.used_bytes / (1024**3)).toFixed(1)} GiB</span>
                  </div>
                  <div className="text-sm">
                    <span className="text-muted-foreground">Available:</span>
                    <span className="ml-2 font-medium text-green-600">{(diskMetrics.available_bytes / (1024**3)).toFixed(1)} GiB</span>
                  </div>
                </div>
                <div className="flex flex-col justify-center space-y-2">
                  <div className="text-sm">
                    <span className="text-muted-foreground">Nodes:</span>
                    <span className="ml-2 font-medium">{diskMetrics.node_count}</span>
                  </div>
                  {vmSize && (
                    <div className="text-sm">
                      <span className="text-muted-foreground">VM Size:</span>
                      <span className="ml-2 font-medium">{vmSize}</span>
                    </div>
                  )}
                </div>
              </div>
            </div>

            <Separator className="my-4" />

            {/* Individual Nodes */}
            {diskMetrics.nodes && diskMetrics.nodes.length > 0 && (
              <div>
                <h3 className="text-sm font-semibold mb-3 flex items-center gap-2 pr-4">
                  <Server className="w-4 h-4" />
                  Individual Nodes ({diskMetrics.nodes.length})
                </h3>
                <ScrollArea className="h-[350px] pr-4">
                  <div className="space-y-4">
                  {diskMetrics.nodes.map((node, idx) => (
                    <div key={idx} className="border rounded-lg p-4 bg-card">
                      <div className="flex items-start justify-between mb-3">
                        <div className="flex-1">
                          <div className="flex items-center gap-2 mb-1">
                            <h4 className="font-medium text-sm">{node.node_name}</h4>
                            <Badge 
                              variant={node.is_ephemeral ? "default" : "secondary"}
                              className="text-xs"
                            >
                              {node.is_ephemeral ? "Ephemeral" : "Managed Disk"}
                            </Badge>
                          </div>
                          <p className="text-xs text-muted-foreground">{node.disk_type}</p>
                        </div>
                        {node.usage_percent > 80 ? (
                          <AlertTriangle className="w-5 h-5 text-destructive" />
                        ) : (
                          <CheckCircle2 className="w-5 h-5 text-green-600" />
                        )}
                      </div>

                      <div className="grid grid-cols-4 gap-4">
                        <CircularGauge 
                          value={node.usage_percent} 
                          label="Usage"
                          size={100}
                          strokeWidth={8}
                        />
                        <div className="col-span-3 flex flex-col justify-center space-y-2">
                          <div className="grid grid-cols-2 gap-2 text-xs">
                            <div>
                              <span className="text-muted-foreground">Total Size:</span>
                              <span className="ml-2 font-medium">{(node.total_bytes / (1024**3)).toFixed(2)} GiB</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Used:</span>
                              <span className="ml-2 font-medium">{(node.used_bytes / (1024**3)).toFixed(2)} GiB</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Available:</span>
                              <span className="ml-2 font-medium text-green-600">{(node.available_bytes / (1024**3)).toFixed(2)} GiB</span>
                            </div>
                            <div>
                              <span className="text-muted-foreground">Type:</span>
                              <span className="ml-2 font-medium">{node.is_ephemeral ? "Ephemeral" : "Persistent"}</span>
                            </div>
                          </div>
                        </div>
                      </div>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </div>
            )}
            </TabsContent>

            {/* Storage Classes Tab */}
            <TabsContent value="storage" className="space-y-4 mt-0">
              {loadingStorage ? (
                <div className="flex items-center justify-center py-8">
                  <p className="text-sm text-muted-foreground">Loading storage classes...</p>
                </div>
              ) : storageOverview && storageOverview.storage_classes.length > 0 ? (
                <>
                  <div className="grid grid-cols-4 gap-3 mb-4 pr-4">
                    <div className="text-center p-3 bg-card border rounded-lg">
                      <p className="text-2xl font-bold">{storageOverview.storage_classes.length}</p>
                      <p className="text-xs text-muted-foreground">Storage Classes</p>
                    </div>
                    <div className="text-center p-3 bg-card border rounded-lg">
                      <p className="text-2xl font-bold">{storageOverview.total_pvs}</p>
                      <p className="text-xs text-muted-foreground">Total PVs</p>
                    </div>
                    <div className="text-center p-3 bg-card border rounded-lg">
                      <p className="text-2xl font-bold">{(storageOverview.total_capacity_bytes / (1024**3)).toFixed(0)} GiB</p>
                      <p className="text-xs text-muted-foreground">Total Capacity</p>
                    </div>
                    {storageOverview.used_capacity_bytes > 0 && (
                      <div className="text-center p-3 bg-card border rounded-lg">
                        <p className="text-2xl font-bold">{(storageOverview.used_capacity_bytes / (1024**3)).toFixed(1)} GiB</p>
                        <p className="text-xs text-muted-foreground">Used Capacity</p>
                      </div>
                    )}
                  </div>

                  <ScrollArea className="h-[400px] pr-4">
                    <div className="space-y-3">
                      {storageOverview.storage_classes.map((sc, idx) => (
                      <div key={idx} className="border rounded-lg p-4 bg-card">
                        <div className="flex items-start justify-between mb-3">
                          <div className="flex-1">
                            <h4 className="font-medium text-sm flex items-center gap-2">
                              <Layers className="w-4 h-4" />
                              {sc.name}
                            </h4>
                            <p className="text-xs text-muted-foreground mt-1">{sc.provisioner}</p>
                            {sc.allow_expansion && (
                              <Badge variant="secondary" className="text-xs mt-2">Expandable</Badge>
                            )}
                          </div>
                          {sc.used_capacity_bytes !== undefined && sc.used_capacity_bytes > 0 && (
                            <div className="ml-4">
                              <CircularGauge 
                                value={sc.usage_percentage || 0} 
                                label="" 
                                size={70}
                                strokeWidth={6}
                              />
                            </div>
                          )}
                        </div>
                        
                        <div className="grid grid-cols-2 gap-3 text-xs">
                          <div>
                            <span className="text-muted-foreground">Reclaim Policy:</span>
                            <span className="ml-2 font-medium">{sc.reclaim_policy || 'N/A'}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Binding Mode:</span>
                            <span className="ml-2 font-medium">{sc.volume_bind_mode || 'N/A'}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">PV Count:</span>
                            <span className="ml-2 font-medium">{sc.pv_count}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Total Capacity:</span>
                            <span className="ml-2 font-medium">{(sc.total_capacity_bytes / (1024**3)).toFixed(1)} GiB</span>
                          </div>
                          {sc.used_capacity_bytes !== undefined && sc.used_capacity_bytes > 0 && (
                            <>
                              <div>
                                <span className="text-muted-foreground">Used:</span>
                                <span className="ml-2 font-medium">{(sc.used_capacity_bytes / (1024**3)).toFixed(1)} GiB</span>
                              </div>
                              <div>
                                <span className="text-muted-foreground">Usage:</span>
                                <span className="ml-2 font-medium">{sc.usage_percentage?.toFixed(1)}%</span>
                              </div>
                            </>
                          )}
                        </div>

                        {sc.parameters && Object.keys(sc.parameters).length > 0 && (
                          <div className="mt-3 pt-3 border-t">
                            <p className="text-xs font-medium text-muted-foreground mb-2">Parameters:</p>
                            <div className="grid grid-cols-2 gap-2">
                              {Object.entries(sc.parameters).map(([key, value]) => (
                                <div key={key} className="text-xs">
                                  <span className="text-muted-foreground">{key}:</span>
                                  <span className="ml-1 font-mono text-[11px]">{value}</span>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                </>
              ) : (
                <div className="flex items-center justify-center py-8">
                  <p className="text-sm text-muted-foreground">No storage classes found</p>
                </div>
              )}
            </TabsContent>

            {/* PVCs Tab */}
            <TabsContent value="pvcs" className="space-y-4 mt-0">
              {loadingStorage ? (
                <div className="flex items-center justify-center py-8">
                  <p className="text-sm text-muted-foreground">Loading PVCs...</p>
                </div>
              ) : storageOverview && storageOverview.pvcs.length > 0 ? (
                <>
                  <div className="flex items-center justify-between mb-4 pr-4">
                    <p className="text-sm font-medium">
                      Total PVCs: {storageOverview.total_pvcs}
                    </p>
                  </div>

                  <ScrollArea className="h-[450px] pr-4">
                    <div className="space-y-2">
                      {storageOverview.pvcs.map((pvc, idx) => (
                        <div key={idx} className="border rounded-lg p-3 bg-card hover:bg-accent/50 transition-colors">
                          <div className="flex items-start justify-between mb-3">
                            <div className="flex-1">
                              <div className="flex items-center gap-2">
                                <Database className="w-4 h-4" />
                                <h4 className="font-medium text-sm">{pvc.name}</h4>
                                <Badge 
                                  variant={pvc.status === "Bound" ? "default" : "secondary"}
                                  className="text-xs"
                                >
                                  {pvc.status}
                                </Badge>
                              </div>
                              <p className="text-xs text-muted-foreground mt-1">
                                Namespace: {pvc.namespace}
                              </p>
                            </div>
                            {pvc.used_bytes !== undefined && pvc.used_bytes > 0 && (
                              <div className="ml-4">
                                <CircularGauge 
                                  value={pvc.usage_percentage || 0} 
                                  label="" 
                                  size={70}
                                  strokeWidth={6}
                                />
                              </div>
                            )}
                          </div>
                        
                        <div className="grid grid-cols-3 gap-2 text-xs">
                          <div>
                            <span className="text-muted-foreground">Storage Class:</span>
                            <span className="ml-1 font-medium">{pvc.storage_class || 'default'}</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Capacity:</span>
                            <span className="ml-1 font-medium">{(pvc.capacity_bytes / (1024**3)).toFixed(2)} GiB</span>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Access:</span>
                            <span className="ml-1 font-medium text-[11px]">{pvc.access_modes.join(', ')}</span>
                          </div>
                          {pvc.used_bytes !== undefined && pvc.used_bytes > 0 && (
                            <>
                              <div>
                                <span className="text-muted-foreground">Used:</span>
                                <span className="ml-1 font-medium">{(pvc.used_bytes / (1024**3)).toFixed(2)} GiB</span>
                              </div>
                              <div>
                                <span className="text-muted-foreground">Available:</span>
                                <span className="ml-1 font-medium">{(pvc.available_bytes! / (1024**3)).toFixed(2)} GiB</span>
                              </div>
                              <div>
                                <span className="text-muted-foreground">Usage:</span>
                                <span className="ml-1 font-medium">{pvc.usage_percentage?.toFixed(1)}%</span>
                              </div>
                            </>
                          )}
                        </div>

                        {pvc.node_name && (
                          <div className="mt-2 pt-2 border-t text-xs">
                            <span className="text-muted-foreground">Node:</span>
                            <span className="ml-2 font-medium">{pvc.node_name}</span>
                          </div>
                        )}
                      </div>
                    ))}
                    </div>
                  </ScrollArea>
                </>
              ) : (
                <div className="flex items-center justify-center py-8">
                  <p className="text-sm text-muted-foreground">No PVCs found</p>
                </div>
              )}
            </TabsContent>
          </ScrollArea>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
