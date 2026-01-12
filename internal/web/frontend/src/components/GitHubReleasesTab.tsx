import { useState } from "react";
import * as React from "react";
import { SplitView } from "@/components/SplitView";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Info,
  Search,
  GitCompare,
  Database,
  Users,
  FileText,
  Clock,
  GitBranch,
  AlertCircle,
  Settings,
  Play,
  Key
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { TokenConfigModal } from "./TokenConfigModal";
import { DeploymentScanModal } from "./DeploymentScanModal";

interface ProductionDeployment {
  id: number;
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  squad: string;
  servicenow_task: string;
  last_seen: string;
  age: string;
}

interface GitHubCommit {
  sha: string;
  message: string;
  author: string;
  date: string;
  url: string;
}

interface GitHubFile {
  filename: string;
  status: string;
  additions: number;
  deletions: number;
  changes: number;
  patch?: string;
}

interface ComparisonResult {
  production_deployment: ProductionDeployment;
  base_tag: string;
  head_tag: string;
  commits: GitHubCommit[];
  files: GitHubFile[];
  total_commits: number;
  total_files: number;
  repository_url: string;
  compare_url: string;
}

interface DeploymentRecord {
  id: number;
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  squad: string;
  servicenow_task: string;
  last_seen: string;
  age: string;
}

export const GitHubReleasesTab = () => {
  const [deploymentName, setDeploymentName] = useState("");  // Nome do deployment na base
  const [githubRepo, setGithubRepo] = useState("");           // Nome do repositório no GitHub
  const [productionTag, setProductionTag] = useState("");     // Tag atual em produção
  const [newTag, setNewTag] = useState("");                   // Tag da nova release
  const [showAllFiles, setShowAllFiles] = useState(false);
  const [searchTriggered, setSearchTriggered] = useState(false);
  const [showTokenModal, setShowTokenModal] = useState(false);
  const [showScanModal, setShowScanModal] = useState(false);

  // ✅ Query para buscar versão em produção assim que digitar o nome do deployment
  const { data: productionData, isLoading: isLoadingProduction, error: productionError } = useQuery<{
    deployment: string;
    namespace: string;
    cluster: string;
    version: string;
    image: string;
    status: string;
    created_at: string;      // ✅ Data de criação real do deployment
    last_seen: string;       // Data do último scan
    squad: string;
    servicenow_task: string;
  }>({
    queryKey: ['github-production-version', deploymentName],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/github/deployments/production?app_name=${encodeURIComponent(deploymentName)}`,
        {
          headers: {
            'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`
          }
        }
      );

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      return response.json();
    },
    enabled: deploymentName.length >= 3, // Só buscar se tiver pelo menos 3 caracteres
  });

  // ✅ Query para buscar deployments na base (busca geral)
  const { data: searchResults, isLoading: isSearching } = useQuery<{ deployments: DeploymentRecord[] }>({
    queryKey: ['github-deployments-all'],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/github/deployments/registry?only_valid_versions=true`,
        {
          headers: {
            'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`
          }
        }
      );

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      return response.json();
    },
    staleTime: 60000, // Cache por 1 minuto
  });

  // Query para comparar releases no GitHub
  const { data: comparisonData, isLoading: isComparing, error: compareError, refetch: refetchComparison } = useQuery<ComparisonResult>({
    queryKey: ['github-compare', githubRepo, productionTag, newTag],
    queryFn: async () => {
      // Construir URL de comparação no formato: viavarejo-internal/<repo>/compare/<base>...<head>
      const owner = 'viavarejo-internal';
      const base = productionTag.replace(/^v/, ''); // Remove 'v' se existir
      const head = newTag.replace(/^v/, '');
      
      const response = await fetch(
        `/api/v1/github/repos/${owner}/${githubRepo}/compare/${base}...${head}`,
        {
          headers: {
            'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`
          }
        }
      );

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`HTTP ${response.status}: ${errorText}`);
      }

      return response.json();
    },
    enabled: searchTriggered && !!githubRepo && !!productionTag && !!newTag,
  });

  const handleSearch = () => {
    if (githubRepo && productionTag && newTag) {
      setSearchTriggered(true);
      refetchComparison();
    }
  };

  // Calcular age do deployment
  const calculateAge = (dateString: string): string => {
    if (!dateString) return '-';

    const date = new Date(dateString);
    const now = new Date();

    // ✅ Validar se data é válida (não é 1970-01-01, não é inválida, não é futura)
    if (isNaN(date.getTime()) || date.getFullYear() < 2020 || date > now) {
      return '-';
    }

    const diffMs = now.getTime() - date.getTime();
    const totalDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    // ✅ Formatar com anos, meses e dias
    if (totalDays >= 365) {
      const years = Math.floor(totalDays / 365);
      const remainingDays = totalDays % 365;
      const months = Math.floor(remainingDays / 30);
      if (months > 0) {
        return `${years}a ${months}m`;
      }
      return `${years}a`;
    }

    if (totalDays >= 30) {
      const months = Math.floor(totalDays / 30);
      const days = totalDays % 30;
      if (days > 0) {
        return `${months}m ${days}d`;
      }
      return `${months}m`;
    }

    if (totalDays > 0) {
      const hours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
      if (hours > 0) {
        return `${totalDays}d ${hours}h`;
      }
      return `${totalDays}d`;
    }

    const hours = Math.floor(diffMs / (1000 * 60 * 60));
    if (hours > 0) {
      const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
      if (minutes > 0) {
        return `${hours}h ${minutes}m`;
      }
      return `${hours}h`;
    }

    const minutes = Math.floor(diffMs / (1000 * 60));
    return `${minutes}m`;
  };

  // Função para buscar dados (refetch)
  const handleRefetch = () => {
    if (deploymentName.length >= 3) {
      window.location.reload(); // Recarrega a página para buscar dados atualizados
    }
  };

  // Auto-preencher tag em produção quando encontrar o deployment
  React.useEffect(() => {
    if (productionData?.version) {
      setProductionTag(productionData.version);
    }
  }, [productionData]);

  // Filtrar arquivos por extensão
  const filteredFiles = comparisonData?.files.filter(file => {
    if (showAllFiles) return true;

    const filename = file.filename.toLowerCase();
    return (
      filename.endsWith('.yml') ||
      filename.endsWith('.yaml') ||
      filename.endsWith('.md') ||
      filename.includes('dockerfile')
    );
  }) || [];

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Configuração",
          titleAction: (
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleRefetch}
                disabled={deploymentName.length < 3}
                title="Recarregar dados da base"
              >
                <Search className="h-4 w-4 mr-2" />
                Refetch
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowTokenModal(true)}
              >
                <Key className="h-4 w-4 mr-2" />
                Token
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowScanModal(true)}
              >
                <Play className="h-4 w-4 mr-2" />
                Scan
              </Button>
            </div>
          ),
          content: (
            <div className="h-full flex flex-col space-y-4 p-4">
              {/* Campo 1: Nome do Deployment na Base */}
              <div className="space-y-2">
                <Label htmlFor="deployment-name">Nome do Deployment (Base de Dados)</Label>
                <Input
                  id="deployment-name"
                  placeholder="Ex: vv-geolocalizacao-api"
                  value={deploymentName}
                  onChange={(e) => setDeploymentName(e.target.value)}
                />
                {isLoadingProduction && deploymentName.length >= 3 && (
                  <p className="text-xs text-muted-foreground flex items-center gap-2">
                    <Search className="h-3 w-3 animate-pulse" />
                    Buscando na base de dados...
                  </p>
                )}
                {productionError && deploymentName.length >= 3 && (
                  <Alert variant="destructive" className="py-2">
                    <AlertCircle className="h-4 w-4" />
                    <AlertTitle className="text-sm">Deployment não encontrado</AlertTitle>
                    <AlertDescription className="text-xs">
                      Não foi encontrado deployment "{deploymentName}" em produção.
                      Execute o Scan primeiro.
                    </AlertDescription>
                  </Alert>
                )}
              </div>

              {/* ✅ Painel de Informações da Release em Produção */}
              {productionData && (
                <>
                  <Card className="border-green-500/50 bg-green-50 dark:bg-green-950/40">
                    <CardHeader className="pb-3">
                      <CardTitle className="text-sm flex items-center gap-2 text-green-700 dark:text-green-400">
                        <Database className="h-4 w-4" />
                        Release em Produção
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 text-xs">
                      <div className="grid grid-cols-2 gap-x-4 gap-y-2">
                        <div>
                          <span className="text-muted-foreground">Cluster:</span>
                          <p className="font-mono font-medium mt-0.5 text-foreground">{productionData.cluster}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Namespace:</span>
                          <p className="font-mono font-medium mt-0.5 text-foreground">{productionData.namespace}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Deployment:</span>
                          <p className="font-medium mt-0.5 text-foreground">{productionData.deployment}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Versão Atual:</span>
                          <Badge variant="secondary" className="font-mono mt-0.5">
                            {productionData.version}
                          </Badge>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Squad:</span>
                          <p className="mt-0.5 text-foreground">{productionData.squad || '-'}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">ServiceNow CHG:</span>
                          <p className="font-mono mt-0.5 text-foreground">{productionData.servicenow_task || '-'}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Criado em:</span>
                          <p className="mt-0.5 text-[10px] text-foreground">
                            {productionData.created_at ? new Date(productionData.created_at).toLocaleString('pt-BR', {
                              day: '2-digit',
                              month: '2-digit',
                              year: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit'
                            }) : '-'}
                          </p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Age (Tempo de Vida):</span>
                          <Badge variant="outline" className="mt-0.5">
                            {productionData.created_at ? calculateAge(productionData.created_at) : '-'}
                          </Badge>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Último Scan:</span>
                          <p className="mt-0.5 text-[10px] text-foreground">
                            {new Date(productionData.last_seen).toLocaleString('pt-BR', {
                              day: '2-digit',
                              month: '2-digit',
                              year: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit'
                            })}
                          </p>
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                  <Separator />
                </>
              )}

              {/* Campo 2: Nome do Repositório GitHub */}
              <div>
                <Label htmlFor="github-repo">Repositório GitHub</Label>
                <Input
                  id="github-repo"
                  placeholder="Ex: vv-retira-geolocalizacao"
                  value={githubRepo}
                  onChange={(e) => setGithubRepo(e.target.value)}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Nome do repositório em github.com/viavarejo-internal/
                </p>
              </div>

              <Separator />

              {/* Campo 3: Tag em Produção (auto-preenchido) */}
              <div>
                <Label htmlFor="production-tag">Tag em Produção</Label>
                <Input
                  id="production-tag"
                  placeholder="Ex: 2.5.5-2"
                  value={productionTag}
                  onChange={(e) => setProductionTag(e.target.value)}
                />
                {productionData?.version && (
                  <p className="text-xs text-muted-foreground mt-1">
                    ✅ Auto-preenchido com a versão encontrada
                  </p>
                )}
              </div>

              {/* Campo 4: Tag da Nova Release */}
              <div>
                <Label htmlFor="new-tag">Tag da Nova Release</Label>
                <Input
                  id="new-tag"
                  placeholder="Ex: 2.5.8-1"
                  value={newTag}
                  onChange={(e) => setNewTag(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                />
              </div>

              {/* Botão Comparar */}
              <Button
                onClick={handleSearch}
                disabled={!githubRepo || !productionTag || !newTag || isComparing}
                className="w-full"
              >
                <GitCompare className="h-4 w-4 mr-2" />
                {isComparing ? 'Comparando...' : 'Comparar com GitHub'}
              </Button>

              {/* Alerta informativo */}
              <Alert>
                <Info className="h-4 w-4" />
                <AlertTitle>Como usar</AlertTitle>
                <AlertDescription className="text-xs space-y-2">
                  <ol className="list-decimal list-inside space-y-1 text-muted-foreground">
                    <li>Configure seu token GitHub (botão Token)</li>
                    <li>Execute Scan dos clusters (botão Scan)</li>
                    <li>Digite o nome do deployment (ex: vv-geolocalizacao-api)</li>
                    <li>Digite o nome do repositório GitHub (ex: vv-retira-geolocalizacao)</li>
                    <li>Confirme/ajuste a tag em produção</li>
                    <li>Digite a nova tag e clique em Comparar</li>
                  </ol>
                </AlertDescription>
              </Alert>
            </div>
          ),
        }}
        rightPanel={{
          title: "Comparação",
          titleAction: comparisonData && (
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <GitCompare className="h-4 w-4" />
                <span>{comparisonData.total_commits} commit(s)</span>
                <span className="text-muted-foreground">•</span>
                <span>{filteredFiles.length} arquivo(s)</span>
              </div>
              <div className="flex items-center gap-2">
                <Label htmlFor="show-all" className="text-xs cursor-pointer">
                  Mostrar todos
                </Label>
                <Switch
                  id="show-all"
                  checked={showAllFiles}
                  onCheckedChange={setShowAllFiles}
                />
              </div>
            </div>
          ),
          content: (
            <div className="h-full flex flex-col">
              {!searchTriggered && (
                <div className="flex-1 flex items-center justify-center text-center space-y-4 p-8">
                  <div>
                    <GitCompare className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
                    <p className="text-muted-foreground">
                      Digite o nome da release e a nova tag para comparar
                    </p>
                  </div>
                </div>
              )}

              {isComparing && (
                <div className="flex-1 flex items-center justify-center">
                  <div className="text-center space-y-4">
                    <Search className="h-8 w-8 animate-pulse mx-auto text-muted-foreground" />
                    <p className="text-sm text-muted-foreground">Buscando e comparando versões...</p>
                  </div>
                </div>
              )}

              {compareError && (
                <div className="flex-1 flex items-center justify-center p-8">
                  <Alert variant="destructive">
                    <AlertCircle className="h-4 w-4" />
                    <AlertTitle>Erro ao comparar releases</AlertTitle>
                    <AlertDescription className="text-xs">
                      {compareError instanceof Error ? compareError.message : 'Erro desconhecido'}
                    </AlertDescription>
                  </Alert>
                </div>
              )}

              {!isComparing && !compareError && comparisonData && (
                <ScrollArea className="flex-1 p-4">
                  <div className="space-y-6">
                    {/* Informação de comparação */}
                    <Card>
                      <CardHeader className="pb-3">
                        <CardTitle className="text-sm flex items-center gap-2">
                          <GitBranch className="h-4 w-4" />
                          Comparando Versões
                        </CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-2">
                        <div className="flex items-center justify-between text-sm">
                          <Badge variant="outline">{comparisonData.base_tag}</Badge>
                          <span className="text-muted-foreground">→</span>
                          <Badge variant="default">{comparisonData.head_tag}</Badge>
                        </div>
                        <a
                          href={comparisonData.compare_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-xs text-blue-600 hover:underline flex items-center gap-1"
                        >
                          <GitCompare className="h-3 w-3" />
                          Ver no GitHub
                        </a>
                      </CardContent>
                    </Card>

                    {/* Commits */}
                    {comparisonData.commits && comparisonData.commits.length > 0 && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm">
                            Commits ({comparisonData.commits.length})
                          </CardTitle>
                        </CardHeader>
                        <CardContent className="space-y-3">
                          {comparisonData.commits.map((commit) => (
                            <div key={commit.sha} className="border-l-2 border-muted pl-3 py-1">
                              <div className="flex items-start justify-between gap-2">
                                <div className="flex-1">
                                  <p className="text-sm font-medium leading-tight">
                                    {commit.message.split('\n')[0]}
                                  </p>
                                  <p className="text-xs text-muted-foreground mt-1">
                                    {commit.author} • {new Date(commit.date).toLocaleString('pt-BR')}
                                  </p>
                                </div>
                                <code className="text-xs bg-muted px-2 py-1 rounded">
                                  {commit.sha.substring(0, 7)}
                                </code>
                              </div>
                            </div>
                          ))}
                        </CardContent>
                      </Card>
                    )}

                    {/* Arquivos alterados */}
                    {filteredFiles.length > 0 && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm flex items-center justify-between">
                            <span>Arquivos Alterados ({filteredFiles.length})</span>
                            {!showAllFiles && (
                              <Badge variant="secondary" className="text-xs">
                                .yml, .yaml, .md, Dockerfile
                              </Badge>
                            )}
                          </CardTitle>
                        </CardHeader>
                        <CardContent className="space-y-2">
                          {filteredFiles.map((file, index) => (
                            <div
                              key={index}
                              className="flex items-center justify-between p-2 rounded border"
                            >
                              <div className="flex-1">
                                <p className="text-sm font-mono">{file.filename}</p>
                                <div className="flex items-center gap-3 mt-1 text-xs">
                                  <Badge
                                    variant={
                                      file.status === 'added' ? 'default' :
                                      file.status === 'removed' ? 'destructive' :
                                      'secondary'
                                    }
                                    className="text-xs"
                                  >
                                    {file.status}
                                  </Badge>
                                  <span className="text-green-600">+{file.additions}</span>
                                  <span className="text-red-600">-{file.deletions}</span>
                                </div>
                              </div>
                            </div>
                          ))}
                        </CardContent>
                      </Card>
                    )}

                    {filteredFiles.length === 0 && comparisonData.files.length > 0 && !showAllFiles && (
                      <Alert>
                        <Info className="h-4 w-4" />
                        <AlertTitle>Nenhum arquivo filtrado encontrado</AlertTitle>
                        <AlertDescription className="text-xs">
                          Nenhum arquivo .yml, .yaml, .md ou Dockerfile foi alterado.
                          Ative "Mostrar todos" para ver os {comparisonData.files.length} arquivo(s) alterados.
                        </AlertDescription>
                      </Alert>
                    )}
                  </div>
                </ScrollArea>
              )}
            </div>
          ),
        }}
      />

      {/* Modal de configuração de token */}
      <TokenConfigModal open={showTokenModal} onOpenChange={setShowTokenModal} />

      {/* Modal de scan de deployments */}
      <DeploymentScanModal open={showScanModal} onOpenChange={setShowScanModal} />
    </>
  );
};
