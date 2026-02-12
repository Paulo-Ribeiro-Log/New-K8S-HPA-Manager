import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Server, RefreshCw, Eye } from "lucide-react";
import { useNodes, useNodeDetails } from "@/hooks/useNodes";
import NodeDetailsModal from "./NodeDetailsModal";
import type { NodeInfo } from "@/lib/api/types";

interface NodeDetailsTabProps {
  cluster: string;
  nodePoolName: string;
}

const StatusBadge = ({ status }: { status: string }) => {
  const getVariant = () => {
    if (status === "Ready") return "success";
    if (status === "NotReady") return "destructive";
    return "secondary";
  };

  return (
    <Badge variant={getVariant() as any}>
      {status}
    </Badge>
  );
};

export default function NodeDetailsTab({ cluster, nodePoolName }: NodeDetailsTabProps) {
  const { nodes, loading, error, refetch } = useNodes(cluster, nodePoolName);
  const [selectedNode, setSelectedNode] = useState<string | null>(null);
  const [showModal, setShowModal] = useState(false);

  const { nodeDetails, loading: loadingDetails } = useNodeDetails(
    cluster,
    nodePoolName,
    selectedNode || ""
  );

  const handleViewDetails = (nodeName: string) => {
    setSelectedNode(nodeName);
    setShowModal(true);
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedNode(null);
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-12">
          <div className="flex items-center gap-2 text-muted-foreground">
            <RefreshCw className="w-4 h-4 animate-spin" />
            Loading nodes...
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card>
        <CardContent className="py-12">
          <div className="text-center text-destructive">
            <p className="mb-4">Error loading nodes: {error}</p>
            <Button onClick={() => refetch()} variant="outline" size="sm">
              <RefreshCw className="w-4 h-4 mr-2" />
              Retry
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Server className="w-5 h-5" />
              Nodes - {nodePoolName}
            </CardTitle>
            <CardDescription>
              {nodes.length} node{nodes.length !== 1 ? "s" : ""} in this pool
            </CardDescription>
          </div>
          <Button onClick={() => refetch()} variant="outline" size="sm">
            <RefreshCw className="w-4 h-4 mr-2" />
            Atualizar
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {nodes.length === 0 ? (
          <div className="text-center text-muted-foreground py-8">
            No nodes found in this pool
          </div>
        ) : (
          <div className="border rounded-lg overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>CPU</TableHead>
                  <TableHead>Memory</TableHead>
                  <TableHead>Pods</TableHead>
                  <TableHead>Age</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((node: NodeInfo) => (
                  <TableRow key={node.name} className="hover:bg-muted/50 cursor-pointer">
                    <TableCell className="font-medium font-mono text-sm">
                      {node.name}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={node.status} />
                      {node.unschedulable && (
                        <Badge variant="secondary" className="ml-2">
                          Cordoned
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {node.cpu_used ? (
                        <div className="space-y-1">
                          <div className="text-sm font-mono">
                            {node.cpu_used} / {node.cpu_allocatable}
                          </div>
                          <Badge
                            variant={node.cpu_usage_percent > 80 ? "destructive" : "secondary"}
                            className="text-xs"
                          >
                            {node.cpu_usage_percent.toFixed(1)}%
                          </Badge>
                        </div>
                      ) : (
                        <span className="text-muted-foreground text-sm">N/A</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {node.memory_used ? (
                        <div className="space-y-1">
                          <div className="text-sm font-mono">
                            {node.memory_used} / {node.memory_allocatable}
                          </div>
                          <Badge
                            variant={node.memory_usage_percent > 80 ? "destructive" : "secondary"}
                            className="text-xs"
                          >
                            {node.memory_usage_percent.toFixed(1)}%
                          </Badge>
                        </div>
                      ) : (
                        <span className="text-muted-foreground text-sm">N/A</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="text-sm font-mono">
                        {node.pods_running} / {node.pods_total}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        of {node.pods_capacity} max
                      </div>
                    </TableCell>
                    <TableCell className="text-sm">{node.age}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        onClick={() => handleViewDetails(node.name)}
                        variant="ghost"
                        size="sm"
                      >
                        <Eye className="w-4 h-4 mr-2" />
                        View Details
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      {/* Node Details Modal */}
      <NodeDetailsModal
        open={showModal}
        onOpenChange={handleCloseModal}
        nodeDetails={nodeDetails}
        loading={loadingDetails}
      />
    </Card>
  );
}
