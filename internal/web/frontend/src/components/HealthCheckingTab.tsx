import { useState, useEffect, useMemo } from "react";
import { SplitView } from "@/components/SplitView";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Activity,
  Play,
  Loader2,
  RefreshCcw,
  Server,
} from "lucide-react";
import { toast } from "sonner";
import { useHealthChecking } from "@/hooks/useHealthChecking";
import type { HealthCheckRequest } from "@/types/healthcheck";
import { HealthCheckProgressModal } from "@/components/HealthCheckProgressModal";
import { HealthCheckResultsPanel } from "@/components/HealthCheckResultsPanel";
import { useClusters, useNamespaces } from "@/hooks/useAPI";
import { ProtectedAction } from "@/components/rbac";

interface HealthCheckingTabProps {
  // Componente independente - não recebe contexto do Dashboard
}

export const HealthCheckingTab = (props: HealthCheckingTabProps) => {
  // Buscar TODOS os clusters disponíveis (independente do contexto)
  const { clusters: allClusters, loading: clustersLoading } = useClusters();

  // Health Check State
  const {
    isRunning,
    sessionId,
    clusterSessions,
    results,
    runHealthCheck,
    reset,
  } = useHealthChecking();

  // Configuration State
  const [environment, setEnvironment] = useState<string>("");
  const [selectedClusters, setSelectedClusters] = useState<string[]>([]);
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [checkDeployments, setCheckDeployments] = useState(true);
  const [checkServices, setCheckServices] = useState(true);
  const [checkConfigs, setCheckConfigs] = useState(true);
  const [timeout, setTimeout] = useState(10);

  // Modal state
  const [showProgressModal, setShowProgressModal] = useState(false);

  // Debug logs - executa após estado estar inicializado
  useEffect(() => {
    console.log("[HealthCheckingTab] State updated:", {
      allClustersCount: allClusters?.length || 0,
      allClusters: allClusters?.map(c => ({ name: c.name, context: c.context })),
      selectedClusters,
    });
  }, [allClusters, selectedClusters]);

  // Auto-select clusters when environment changes
  useEffect(() => {
    if (!environment || !allClusters) return;

    const clustersToSelect = allClusters
      .filter((c) => {
        const name = c.name.toLowerCase();
        if (environment === "prod") {
          return name.includes("prod") || name.includes("prd");
        }
        if (environment === "hlg") {
          return name.includes("hlg") || name.includes("homolog") || name.includes("staging");
        }
        if (environment === "all") {
          return true;
        }
        return false;
      })
      .map((c) => c.context);  // ✅ Usar context (com -admin) ao invés de name

    setSelectedClusters(clustersToSelect);
  }, [environment, allClusters]);

  // Handle run health check
  const handleRun = async () => {
    console.log("[HealthCheckingTab] handleRun called");
    console.log("[HealthCheckingTab] selectedClusters:", selectedClusters);
    console.log("[HealthCheckingTab] checkDeployments:", checkDeployments);
    console.log("[HealthCheckingTab] checkServices:", checkServices);
    console.log("[HealthCheckingTab] checkConfigs:", checkConfigs);

    // Validação
    if (selectedClusters.length === 0) {
      console.error("[HealthCheckingTab] No clusters selected");
      toast.error("Selecione pelo menos um cluster");
      return;
    }

    if (!checkDeployments && !checkServices && !checkConfigs) {
      console.error("[HealthCheckingTab] No check types selected");
      toast.error("Selecione pelo menos um tipo de check");
      return;
    }

    // Parse namespaces do input
    const namespaces = parseNamespaces();
    console.log("[HealthCheckingTab] Parsed namespaces:", namespaces);

    // Construir request
    const request: HealthCheckRequest = {
      environment: environment || undefined,
      clusters: selectedClusters,
      namespaces: namespaces.length > 0 ? namespaces : undefined,
      check_deployments: checkDeployments,
      check_services: checkServices,
      check_configs: checkConfigs,
      timeout: timeout,
    };

    console.log("[HealthCheckingTab] Sending request:", request);

    // Executar
    const newSessionId = await runHealthCheck(request);

    console.log("[HealthCheckingTab] Received sessionId:", newSessionId);

    if (newSessionId) {
      setShowProgressModal(true);
      console.log("[HealthCheckingTab] Progress modal opened");
    } else {
      console.error("[HealthCheckingTab] No sessionId returned");
    }
  };

  // Toggle cluster selection
  const toggleCluster = (clusterContext: string) => {
    setSelectedClusters((prev) =>
      prev.includes(clusterContext)
        ? prev.filter((c) => c !== clusterContext)
        : [...prev, clusterContext]
    );
    // Limpar filtro de ambiente quando seleção manual
    setEnvironment("");
  };

  // Select all/none clusters
  const selectAllClusters = () => {
    setSelectedClusters(allClusters.map((c) => c.context));  // ✅ Usar context
    setEnvironment("");
  };

  const clearClusters = () => {
    setSelectedClusters([]);
    setEnvironment("");
  };

  // Parse namespace input ao executar (separado por vírgula)
  const parseNamespaces = (): string[] => {
    if (!namespaceInput.trim()) return [];
    return namespaceInput.split(',').map(ns => ns.trim()).filter(ns => ns.length > 0);
  };

  // Filter clusters (search)
  const [clusterSearchQuery, setClusterSearchQuery] = useState("");
  const filteredClusters = useMemo(() => {
    if (!clusterSearchQuery) return allClusters;
    return allClusters.filter((c) =>
      c.name.toLowerCase().includes(clusterSearchQuery.toLowerCase())
    );
  }, [allClusters, clusterSearchQuery]);

  // Namespace input (opcional - usuário pode digitar namespaces separados por vírgula)
  const [namespaceInput, setNamespaceInput] = useState("");

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Health Check Configuration",
          content: (
            <ScrollArea className="h-full">
              <div className="space-y-4">
                {/* Cluster Selection */}
                <Card>
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div>
                        <CardTitle className="text-sm font-medium">Clusters</CardTitle>
                        <CardDescription className="text-xs">
                          {selectedClusters.length === 0
                            ? "Selecione clusters para testar"
                            : `${selectedClusters.length} cluster(s) selecionado(s)`}
                        </CardDescription>
                      </div>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={selectAllClusters}
                          className="h-7 text-xs"
                        >
                          Todos
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={clearClusters}
                          className="h-7 text-xs"
                        >
                          Limpar
                        </Button>
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-2">
                      <Input
                        placeholder="Buscar clusters..."
                        value={clusterSearchQuery}
                        onChange={(e) => setClusterSearchQuery(e.target.value)}
                        className="h-8"
                      />
                      <ScrollArea className="h-48">
                        <div className="space-y-2">
                          {clustersLoading ? (
                            <div className="text-center py-4 text-sm text-muted-foreground">
                              <Loader2 className="h-4 w-4 animate-spin inline mr-2" />
                              Carregando clusters...
                            </div>
                          ) : filteredClusters.length === 0 ? (
                            <div className="text-center py-4 text-sm text-muted-foreground">
                              Nenhum cluster encontrado
                            </div>
                          ) : (
                            filteredClusters.map((c) => (
                              <div key={c.context} className="flex items-center gap-2">
                                <Checkbox
                                  id={`cluster-${c.context}`}
                                  checked={selectedClusters.includes(c.context)}
                                  onCheckedChange={() => toggleCluster(c.context)}
                                />
                                <Label
                                  htmlFor={`cluster-${c.context}`}
                                  className="text-sm cursor-pointer font-mono flex-1"
                                >
                                  {c.name}
                                </Label>
                              </div>
                            ))
                          )}
                        </div>
                      </ScrollArea>
                    </div>
                  </CardContent>
                </Card>

                {/* Environment Filter (Optional) */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Filtro de Ambiente (Opcional)</CardTitle>
                    <CardDescription className="text-xs">
                      Seleciona automaticamente clusters por ambiente (prod/hlg). Desativa seleção manual.
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-2">
                      <Select value={environment || undefined} onValueChange={setEnvironment}>
                        <SelectTrigger>
                          <SelectValue placeholder="Nenhum (apenas cluster atual)" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="prod">Produção</SelectItem>
                          <SelectItem value="hlg">Homologação</SelectItem>
                          <SelectItem value="all">Todos os ambientes</SelectItem>
                        </SelectContent>
                      </Select>
                      {environment && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setEnvironment("")}
                          className="h-7 text-xs w-full"
                        >
                          Limpar filtro
                        </Button>
                      )}
                    </div>
                  </CardContent>
                </Card>

                {/* Namespace Input */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Namespaces (Opcional)</CardTitle>
                    <CardDescription className="text-xs">
                      Deixe vazio para testar todos os namespaces. Separe múltiplos namespaces por vírgula.
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <Input
                      placeholder="Ex: default, kube-system, production"
                      value={namespaceInput}
                      onChange={(e) => setNamespaceInput(e.target.value)}
                      className="h-8"
                      disabled={selectedClusters.length === 0}
                    />
                    {namespaceInput && (
                      <p className="text-xs text-muted-foreground mt-2">
                        {parseNamespaces().length} namespace(s): {parseNamespaces().join(", ")}
                      </p>
                    )}
                  </CardContent>
                </Card>

                {/* Check Options */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Tipos de Verificação</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-deployments"
                        checked={checkDeployments}
                        onCheckedChange={(checked) => setCheckDeployments(checked as boolean)}
                      />
                      <Label htmlFor="check-deployments" className="text-sm cursor-pointer">
                        Verificar Deployments
                      </Label>
                    </div>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-services"
                        checked={checkServices}
                        onCheckedChange={(checked) => setCheckServices(checked as boolean)}
                      />
                      <Label htmlFor="check-services" className="text-sm cursor-pointer">
                        Testar Serviços Externos
                      </Label>
                    </div>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-configs"
                        checked={checkConfigs}
                        onCheckedChange={(checked) => setCheckConfigs(checked as boolean)}
                      />
                      <Label htmlFor="check-configs" className="text-sm cursor-pointer">
                        Validar ConfigMaps/Secrets
                      </Label>
                    </div>
                  </CardContent>
                </Card>

                {/* Timeout */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Timeout</CardTitle>
                    <CardDescription className="text-xs">
                      Timeout por check em segundos (máx: 120s)
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <Input
                      type="number"
                      min={5}
                      max={120}
                      value={timeout}
                      onChange={(e) => setTimeout(parseInt(e.target.value) || 10)}
                      className="h-8"
                    />
                  </CardContent>
                </Card>

                {/* Run Button */}
                <ProtectedAction
                  action="run_health_check"
                  fallback={
                    <Button className="w-full" disabled>
                      <Play className="mr-2 h-4 w-4" />
                      Executar Health Check (Apenas SRE)
                    </Button>
                  }
                >
                  <Button
                    className="w-full"
                    onClick={handleRun}
                    disabled={isRunning || selectedClusters.length === 0}
                  >
                    {isRunning ? (
                      <>
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        Executando...
                      </>
                    ) : (
                      <>
                        <Play className="mr-2 h-4 w-4" />
                        Executar Health Check
                        {selectedClusters.length > 0 && ` (${selectedClusters.length} cluster${selectedClusters.length > 1 ? 's' : ''})`}
                      </>
                    )}
                  </Button>
                </ProtectedAction>
              </div>
            </ScrollArea>
          ),
        }}
        rightPanel={{
          title: "Resultados",
          titleAction: selectedClusters.length > 0 ? (
            <Badge variant="secondary" className="gap-1">
              <Server className="h-3 w-3" />
              {selectedClusters.length} cluster{selectedClusters.length > 1 ? 's' : ''}
            </Badge>
          ) : undefined,
          content: (
            <ScrollArea className="h-full">
              {!isRunning && results.length === 0 && (
                <div className="flex flex-col items-center justify-center h-full text-center p-8">
                  <Activity className="h-16 w-16 text-muted-foreground opacity-50 mb-4" />
                  <h3 className="text-lg font-semibold mb-2">
                    Configure e execute um health check
                  </h3>
                  <p className="text-sm text-muted-foreground">
                    Selecione as opções no painel esquerdo e clique em "Executar Health Check"
                  </p>
                </div>
              )}

              {results.length > 0 && (
                <HealthCheckResultsPanel results={results} />
              )}
            </ScrollArea>
          ),
        }}
      />

      {/* Progress Modal */}
      {sessionId && showProgressModal && (
        <HealthCheckProgressModal
          sessionId={sessionId}
          clusterSessions={clusterSessions}
          open={showProgressModal}
          onOpenChange={(open) => {
            setShowProgressModal(open);
            // Quando modal fechar, fazer reset
            if (!open) {
              reset();
            }
          }}
          onComplete={(result) => {
            // ✅ NÃO fechar modal automaticamente - deixar usuário ver resultados
            // O modal mostrará botão "Fechar" quando completo
            toast.success("Health check concluído!");
          }}
        />
      )}
    </>
  );
};
