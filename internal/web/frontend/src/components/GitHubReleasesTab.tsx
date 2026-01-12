import { useState } from "react";
import { SplitView } from "@/components/SplitView";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Info,
  Search,
  GitCompare,
  Database,
  Package,
  Users,
  FileText,
  Clock,
  GitBranch,
  AlertCircle
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";

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

export const GitHubReleasesTab = () => {
  const [releaseName, setReleaseName] = useState("");
  const [newTag, setNewTag] = useState("");
  const [showAllFiles, setShowAllFiles] = useState(false);
  const [searchTriggered, setSearchTriggered] = useState(false);

  // Query para buscar e comparar
  const { data, isLoading, error, refetch } = useQuery<ComparisonResult>({
    queryKey: ['github-compare', releaseName, newTag],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/github/compare?release=${encodeURIComponent(releaseName)}&new_tag=${encodeURIComponent(newTag)}`,
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
    enabled: searchTriggered && !!releaseName && !!newTag,
  });

  const handleSearch = () => {
    if (releaseName && newTag) {
      setSearchTriggered(true);
      refetch();
    }
  };

  // Filtrar arquivos por extensão
  const filteredFiles = data?.files.filter(file => {
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
    <SplitView
      leftPanel={{
        title: "Buscar Release",
        content: (
          <div className="h-full flex flex-col space-y-4 p-4">
            <p className="text-sm text-muted-foreground">
              Compare versões de releases do GitHub
            </p>

            {/* Formulário de busca */}
            <div className="space-y-4">
              <div>
                <Label htmlFor="release-name">Nome da Release</Label>
                <Input
                  id="release-name"
                  placeholder="Ex: vv-retira-geolocalizacao"
                  value={releaseName}
                  onChange={(e) => setReleaseName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Nome do repositório GitHub
                </p>
              </div>

              <div>
                <Label htmlFor="new-tag">Tag da Nova Release</Label>
                <Input
                  id="new-tag"
                  placeholder="Ex: 2.5.8-1"
                  value={newTag}
                  onChange={(e) => setNewTag(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Versão que deseja comparar com produção
                </p>
              </div>

              <Button
                onClick={handleSearch}
                disabled={!releaseName || !newTag || isLoading}
                className="w-full"
              >
                <Search className="h-4 w-4 mr-2" />
                {isLoading ? 'Buscando...' : 'Buscar e Comparar'}
              </Button>
            </div>

            {/* Informações de produção */}
            {data?.production_deployment && (
              <>
                <Separator />
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm flex items-center gap-2">
                      <Database className="h-4 w-4" />
                      Versão em Produção
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2 text-xs">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Cluster:</span>
                      <span className="font-mono">{data.production_deployment.cluster}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Namespace:</span>
                      <span className="font-mono">{data.production_deployment.namespace}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Deployment:</span>
                      <span className="font-medium">{data.production_deployment.deployment_name}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Versão Atual:</span>
                      <Badge variant="secondary" className="font-mono">
                        {data.production_deployment.version}
                      </Badge>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Squad:</span>
                      <span>{data.production_deployment.squad || '-'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">CHG:</span>
                      <span className="font-mono">{data.production_deployment.servicenow_task || '-'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Age:</span>
                      <span>{data.production_deployment.age}</span>
                    </div>
                  </CardContent>
                </Card>
              </>
            )}

            {/* Alerta informativo */}
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Como funciona</AlertTitle>
              <AlertDescription className="text-xs space-y-2">
                <ol className="list-decimal list-inside space-y-1 text-muted-foreground">
                  <li>Digite o nome da release e a nova tag</li>
                  <li>Sistema busca versão em produção na base</li>
                  <li>Compara versão atual → nova no GitHub</li>
                  <li>Exibe commits e arquivos alterados</li>
                </ol>
              </AlertDescription>
            </Alert>
          </div>
        ),
      }}
      rightPanel={{
        title: "Comparação",
        titleAction: data && (
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <GitCompare className="h-4 w-4" />
              <span>{data.total_commits} commit(s)</span>
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

            {isLoading && (
              <div className="flex-1 flex items-center justify-center">
                <div className="text-center space-y-4">
                  <Search className="h-8 w-8 animate-pulse mx-auto text-muted-foreground" />
                  <p className="text-sm text-muted-foreground">Buscando e comparando versões...</p>
                </div>
              </div>
            )}

            {error && (
              <div className="flex-1 flex items-center justify-center p-8">
                <Alert variant="destructive">
                  <AlertCircle className="h-4 w-4" />
                  <AlertTitle>Erro ao comparar releases</AlertTitle>
                  <AlertDescription className="text-xs">
                    {error instanceof Error ? error.message : 'Erro desconhecido'}
                  </AlertDescription>
                </Alert>
              </div>
            )}

            {!isLoading && !error && data && (
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
                        <Badge variant="outline">{data.base_tag}</Badge>
                        <span className="text-muted-foreground">→</span>
                        <Badge variant="default">{data.head_tag}</Badge>
                      </div>
                      <a
                        href={data.compare_url}
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
                  {data.commits && data.commits.length > 0 && (
                    <Card>
                      <CardHeader>
                        <CardTitle className="text-sm">
                          Commits ({data.commits.length})
                        </CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-3">
                        {data.commits.map((commit) => (
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

                  {filteredFiles.length === 0 && data.files.length > 0 && !showAllFiles && (
                    <Alert>
                      <Info className="h-4 w-4" />
                      <AlertTitle>Nenhum arquivo filtrado encontrado</AlertTitle>
                      <AlertDescription className="text-xs">
                        Nenhum arquivo .yml, .yaml, .md ou Dockerfile foi alterado.
                        Ative "Mostrar todos" para ver os {data.files.length} arquivo(s) alterados.
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
  );
};
