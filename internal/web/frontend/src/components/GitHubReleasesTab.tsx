import { useState } from "react";
import { SplitView } from "@/components/SplitView";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Info, RefreshCcw, Database, GitBranch, Package, Clock, Users, FileText } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";

interface DeploymentRecord {
  id: number;
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  image_tag: string;
  full_image: string;
  replicas_current: number;
  replicas_desired: number;
  app_name: string;
  status: string;
  squad: string;
  servicenow_task: string;
  first_seen: string;
  last_seen: string;
  last_health_check: string;
  age: string;
}

interface DeploymentsRegistryResponse {
  deployments: DeploymentRecord[];
  total: number;
  valid_versions: number;
  invalid_versions: number;
  filters_applied: {
    cluster: string;
    namespace: string;
    only_valid_versions: boolean;
  };
}

// GitHubReleasesTab NÃO depende do cluster selecionado
// Releases são do GitHub, não específicas de cluster/namespace
export const GitHubReleasesTab = () => {
  const [clusterFilter, setClusterFilter] = useState("");
  const [namespaceFilter, setNamespaceFilter] = useState("");
  const [onlyValidVersions, setOnlyValidVersions] = useState(true);

  // Query para buscar deployments do registry
  const { data, isLoading, error, refetch } = useQuery<DeploymentsRegistryResponse>({
    queryKey: ['github-deployments-registry', clusterFilter, namespaceFilter, onlyValidVersions],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (clusterFilter) params.append('cluster', clusterFilter);
      if (namespaceFilter) params.append('namespace', namespaceFilter);
      if (onlyValidVersions) params.append('only_valid_versions', 'true');

      const response = await fetch(`/api/v1/github/deployments/registry?${params}`);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${await response.text()}`);
      }
      return response.json();
    },
    refetchInterval: 60000, // Atualizar a cada 1 minuto
  });

  return (
    <SplitView
      leftPanel={{
        title: "Configuração",
        titleAction: (
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            disabled={isLoading}
          >
            <RefreshCcw className={`h-4 w-4 mr-2 ${isLoading ? 'animate-spin' : ''}`} />
            Atualizar
          </Button>
        ),
        content: (
          <div className="h-full flex flex-col space-y-4 p-4">
            <p className="text-sm text-muted-foreground">
              Base de conhecimento de deployments dos clusters Kubernetes
            </p>

            {/* Filtros */}
            <div className="space-y-4">
              <div>
                <Label htmlFor="cluster-filter">Filtrar por Cluster</Label>
                <Input
                  id="cluster-filter"
                  placeholder="Ex: abastecimento-hlg"
                  value={clusterFilter}
                  onChange={(e) => setClusterFilter(e.target.value)}
                />
              </div>

              <div>
                <Label htmlFor="namespace-filter">Filtrar por Namespace</Label>
                <Input
                  id="namespace-filter"
                  placeholder="Ex: default"
                  value={namespaceFilter}
                  onChange={(e) => setNamespaceFilter(e.target.value)}
                />
              </div>

              <div className="flex items-center space-x-2">
                <Checkbox
                  id="valid-versions"
                  checked={onlyValidVersions}
                  onCheckedChange={(checked) => setOnlyValidVersions(checked as boolean)}
                />
                <Label
                  htmlFor="valid-versions"
                  className="text-sm font-normal cursor-pointer"
                >
                  Apenas versões válidas (x.x.x-x)
                </Label>
              </div>
            </div>

            {/* Estatísticas */}
            {data && (
              <div className="space-y-2 pt-4 border-t">
                <h3 className="text-sm font-medium">Estatísticas</h3>
                <div className="grid grid-cols-2 gap-2">
                  <div className="bg-muted p-3 rounded-lg">
                    <div className="text-2xl font-bold">{data.total}</div>
                    <div className="text-xs text-muted-foreground">Total</div>
                  </div>
                  <div className="bg-green-50 p-3 rounded-lg border border-green-200">
                    <div className="text-2xl font-bold text-green-700">{data.valid_versions}</div>
                    <div className="text-xs text-green-600">Versões Válidas</div>
                  </div>
                </div>
              </div>
            )}

            {/* Alerta informativo */}
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Como Popular a Base</AlertTitle>
              <AlertDescription className="text-xs space-y-2">
                <div className="mt-2 space-y-1">
                  <p className="font-medium">Executar Health Check</p>
                  <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                    <li>Acesse a aba <strong>Health Checking</strong></li>
                    <li>Selecione os clusters desejados</li>
                    <li>Marque apenas <strong>"Deployments"</strong></li>
                    <li>Execute o Health Check</li>
                  </ul>
                </div>
              </AlertDescription>
            </Alert>

            {/* Informação sobre labels necessários */}
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Labels Necessários</AlertTitle>
              <AlertDescription className="text-xs">
                <p className="mb-2">Os deployments devem ter:</p>
                <ul className="list-disc list-inside space-y-1 font-mono text-xs">
                  <li>app.kubernetes.io/name</li>
                  <li>app.kubernetes.io/version</li>
                  <li>devops.k8s.io/component-squad</li>
                  <li>devops.k8s.io/servicenow-task-number</li>
                </ul>
              </AlertDescription>
            </Alert>
          </div>
        ),
      }}
      rightPanel={{
        title: "Deployments do Registry",
        titleAction: data && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Database className="h-4 w-4" />
            <span>{data.deployments.length} deployment(s)</span>
          </div>
        ),
        content: (
          <div className="h-full flex flex-col">
            {isLoading && (
              <div className="flex-1 flex items-center justify-center">
                <div className="text-center space-y-4">
                  <RefreshCcw className="h-8 w-8 animate-spin mx-auto text-muted-foreground" />
                  <p className="text-sm text-muted-foreground">Carregando deployments...</p>
                </div>
              </div>
            )}

            {error && (
              <div className="flex-1 flex items-center justify-center p-8">
                <Alert variant="destructive">
                  <AlertTitle>Erro ao carregar deployments</AlertTitle>
                  <AlertDescription className="text-xs">
                    {error instanceof Error ? error.message : 'Erro desconhecido'}
                  </AlertDescription>
                </Alert>
              </div>
            )}

            {!isLoading && !error && data && data.deployments.length === 0 && (
              <div className="flex-1 flex items-center justify-center text-center space-y-4 p-8">
                <div>
                  <Database className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
                  <p className="text-muted-foreground">
                    Nenhum deployment encontrado
                  </p>
                  <p className="text-xs text-muted-foreground mt-2">
                    Execute o Health Check para popular a base de dados
                  </p>
                </div>
              </div>
            )}

            {!isLoading && !error && data && data.deployments.length > 0 && (
              <ScrollArea className="flex-1">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-[150px]">
                        <div className="flex items-center gap-2">
                          <Database className="h-4 w-4" />
                          Cluster
                        </div>
                      </TableHead>
                      <TableHead className="w-[120px]">
                        <div className="flex items-center gap-2">
                          <Package className="h-4 w-4" />
                          Namespace
                        </div>
                      </TableHead>
                      <TableHead className="w-[180px]">
                        <div className="flex items-center gap-2">
                          <GitBranch className="h-4 w-4" />
                          Deployment
                        </div>
                      </TableHead>
                      <TableHead className="w-[100px]">Versão</TableHead>
                      <TableHead className="w-[80px]">
                        <div className="flex items-center gap-2">
                          <Clock className="h-4 w-4" />
                          Age
                        </div>
                      </TableHead>
                      <TableHead className="w-[120px]">
                        <div className="flex items-center gap-2">
                          <Users className="h-4 w-4" />
                          Squad
                        </div>
                      </TableHead>
                      <TableHead className="w-[120px]">
                        <div className="flex items-center gap-2">
                          <FileText className="h-4 w-4" />
                          CHG
                        </div>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.deployments.map((deployment) => (
                      <TableRow key={deployment.id}>
                        <TableCell className="font-mono text-xs">
                          {deployment.cluster}
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {deployment.namespace}
                        </TableCell>
                        <TableCell className="font-medium">
                          {deployment.deployment_name}
                        </TableCell>
                        <TableCell>
                          {deployment.version ? (
                            <Badge variant="secondary" className="font-mono text-xs">
                              {deployment.version}
                            </Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">N/A</span>
                          )}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {deployment.age}
                        </TableCell>
                        <TableCell className="text-xs">
                          {deployment.squad || (
                            <span className="text-muted-foreground">-</span>
                          )}
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {deployment.servicenow_task || (
                            <span className="text-muted-foreground">-</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}
          </div>
        ),
      }}
    />
  );
};
