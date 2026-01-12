import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Play, X, CheckCircle2, AlertCircle, Loader2 } from "lucide-react";
import { useClusters } from "@/hooks/useAPI";
import { useMutation, useQueryClient } from "@tanstack/react-query";

interface DeploymentScanModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface ScanProgress {
  cluster: string;
  status: "pending" | "running" | "completed" | "error";
  deploymentsFound: number;
  message?: string;
}

export const DeploymentScanModal = ({ open, onOpenChange }: DeploymentScanModalProps) => {
  const { clusters } = useClusters();
  const queryClient = useQueryClient();
  const [selectedClusters, setSelectedClusters] = useState<string[]>([]);
  const [scanProgress, setScanProgress] = useState<ScanProgress[]>([]);
  const [isScanning, setIsScanning] = useState(false);

  // ✅ Filtrar apenas clusters de produção (com "-prd" no nome)
  const productionClusters = clusters.filter(c =>
    c.context.toLowerCase().includes('-prd')
  );

  const scanMutation = useMutation({
    mutationFn: async (clusters: string[]) => {
      setIsScanning(true);
      setScanProgress(clusters.map(c => ({ cluster: c, status: "pending", deploymentsFound: 0 })));

      // Simular scan sequencial (backend real faria paralelo)
      for (const cluster of clusters) {
        // Atualizar status para running
        setScanProgress(prev => prev.map(p =>
          p.cluster === cluster ? { ...p, status: "running" } : p
        ));

        try {
          // Chamar endpoint de scan para este cluster
          const response = await fetch(`/api/v1/github/deployments/scan?cluster=${cluster}`, {
            method: "POST",
            headers: {
              'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`
            }
          });

          if (!response.ok) {
            throw new Error(`Falha ao escanear ${cluster}`);
          }

          const result = await response.json();

          // Atualizar com sucesso
          setScanProgress(prev => prev.map(p =>
            p.cluster === cluster
              ? { ...p, status: "completed", deploymentsFound: result.deployments_found || 0 }
              : p
          ));
        } catch (error) {
          // Atualizar com erro
          setScanProgress(prev => prev.map(p =>
            p.cluster === cluster
              ? { ...p, status: "error", message: error instanceof Error ? error.message : "Erro desconhecido" }
              : p
          ));
        }
      }

      setIsScanning(false);
    }
  });

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedClusters(productionClusters.map(c => c.context));
    } else {
      setSelectedClusters([]);
    }
  };

  const handleClusterToggle = (clusterName: string, checked: boolean) => {
    if (checked) {
      setSelectedClusters(prev => [...prev, clusterName]);
    } else {
      setSelectedClusters(prev => prev.filter(c => c !== clusterName));
    }
  };

  const handleStartScan = () => {
    if (selectedClusters.length > 0) {
      scanMutation.mutate(selectedClusters);
    }
  };

  const handleClose = () => {
    if (!isScanning) {
      onOpenChange(false);
      // Invalidar cache após scan
      queryClient.invalidateQueries({ queryKey: ['github-deployments-registry'] });
      queryClient.invalidateQueries({ queryKey: ['github-deployments-search'] });
    }
  };

  const completedCount = scanProgress.filter(p => p.status === "completed").length;
  const totalCount = scanProgress.length;
  const progressPercentage = totalCount > 0 ? (completedCount / totalCount) * 100 : 0;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-2xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle>Scan de Deployments</DialogTitle>
          <DialogDescription>
            Escaneia deployments nos clusters selecionados e popula a base de dados
          </DialogDescription>
        </DialogHeader>

        {!isScanning && scanProgress.length === 0 && (
          <div className="space-y-4">
            {/* Seleção de clusters */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Selecione os Clusters</Label>
                <Button
                  variant="link"
                  size="sm"
                  onClick={() => handleSelectAll(selectedClusters.length !== productionClusters.length)}
                >
                  {selectedClusters.length === productionClusters.length ? "Desmarcar todos" : "Selecionar todos"}
                </Button>
              </div>

              <ScrollArea className="h-[300px] border rounded-md p-4">
                <div className="space-y-3">
                  {productionClusters.length === 0 ? (
                    <div className="text-sm text-muted-foreground text-center py-8">
                      Nenhum cluster de produção (-prd) disponível
                    </div>
                  ) : (
                    productionClusters.map((cluster) => (
                      <div key={cluster.context} className="flex items-center space-x-2">
                        <Checkbox
                          id={cluster.context}
                          checked={selectedClusters.includes(cluster.context)}
                          onCheckedChange={(checked) =>
                            handleClusterToggle(cluster.context, checked as boolean)
                          }
                        />
                        <Label
                          htmlFor={cluster.context}
                          className="text-sm font-normal cursor-pointer flex-1"
                        >
                          {cluster.context}
                        </Label>
                      </div>
                    ))
                  )}
                </div>
              </ScrollArea>
            </div>

            {/* Alerta informativo */}
            <Alert>
              <AlertDescription className="text-xs">
                O scan irá:
                <ul className="list-disc list-inside mt-2 space-y-1">
                  <li>Buscar todos os deployments nos clusters selecionados</li>
                  <li>Extrair: nome, namespace, versão, squad, CHG</li>
                  <li>Salvar na base SQLite (~/.k8s-hpa-manager/deployment-registry.db)</li>
                  <li>Atualizar registros existentes com novos dados</li>
                </ul>
              </AlertDescription>
            </Alert>

            {/* Botões */}
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={handleClose}>
                Cancelar
              </Button>
              <Button
                onClick={handleStartScan}
                disabled={selectedClusters.length === 0}
              >
                <Play className="h-4 w-4 mr-2" />
                Iniciar Scan ({selectedClusters.length} cluster(s))
              </Button>
            </div>
          </div>
        )}

        {/* Progresso do scan */}
        {(isScanning || scanProgress.length > 0) && (
          <div className="space-y-4">
            {/* Barra de progresso global */}
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span>Progresso do Scan</span>
                <span className="text-muted-foreground">
                  {completedCount} / {totalCount} clusters
                </span>
              </div>
              <Progress value={progressPercentage} className="h-2" />
            </div>

            {/* Lista de clusters em progresso */}
            <ScrollArea className="h-[350px] border rounded-md p-4">
              <div className="space-y-3">
                {scanProgress.map((progress) => (
                  <div
                    key={progress.cluster}
                    className="flex items-start justify-between p-3 border rounded"
                  >
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        {progress.status === "pending" && (
                          <Badge variant="secondary">Aguardando</Badge>
                        )}
                        {progress.status === "running" && (
                          <Badge className="bg-blue-500">
                            <Loader2 className="h-3 w-3 mr-1 animate-spin" />
                            Escaneando
                          </Badge>
                        )}
                        {progress.status === "completed" && (
                          <Badge className="bg-green-500">
                            <CheckCircle2 className="h-3 w-3 mr-1" />
                            Concluído
                          </Badge>
                        )}
                        {progress.status === "error" && (
                          <Badge variant="destructive">
                            <AlertCircle className="h-3 w-3 mr-1" />
                            Erro
                          </Badge>
                        )}
                        <span className="font-mono text-sm">{progress.cluster}</span>
                      </div>

                      {progress.status === "completed" && (
                        <p className="text-xs text-muted-foreground mt-1">
                          {progress.deploymentsFound} deployment(s) encontrados
                        </p>
                      )}

                      {progress.status === "error" && progress.message && (
                        <p className="text-xs text-red-600 mt-1">{progress.message}</p>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </ScrollArea>

            {/* Botão de fechar */}
            {!isScanning && (
              <div className="flex justify-end">
                <Button onClick={handleClose}>
                  <CheckCircle2 className="h-4 w-4 mr-2" />
                  Concluir
                </Button>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};
