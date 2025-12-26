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
  PanelLeftClose,
  PanelLeftOpen,
  CheckCircle2,
  AlertCircle,
  XCircle,
  Clock,
} from "lucide-react";
import { toast } from "sonner";
import { useHealthChecking } from "@/hooks/useHealthChecking";
import type { HealthCheckRequest } from "@/types/healthcheck";
import { HealthCheckProgressModal } from "@/components/HealthCheckProgressModal";
import { HealthCheckResultsPanel } from "@/components/HealthCheckResultsPanel";
import type { Namespace } from "@/lib/api/types";
import { ProtectedAction } from "@/components/rbac";

interface HealthCheckingTabProps {
  cluster: string;
  namespaces: Namespace[];
  onRefresh: () => void;
}

export const HealthCheckingTab = ({
  cluster,
  namespaces,
  onRefresh,
}: HealthCheckingTabProps) => {
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);

  // Health Check State
  const {
    isRunning,
    sessionId,
    results,
    runHealthCheck,
    reset,
  } = useHealthChecking();

  // Configuration State
  const [environment, setEnvironment] = useState<string>("");
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [checkDeployments, setCheckDeployments] = useState(true);
  const [checkServices, setCheckServices] = useState(true);
  const [checkConfigs, setCheckConfigs] = useState(true);
  const [timeout, setTimeout] = useState(10);

  // Modal state
  const [showProgressModal, setShowProgressModal] = useState(false);

  // Handle run health check
  const handleRun = async () => {
    // Validação
    if (!cluster) {
      toast.error("Selecione um cluster primeiro");
      return;
    }

    if (!checkDeployments && !checkServices && !checkConfigs) {
      toast.error("Selecione pelo menos um tipo de check");
      return;
    }

    // Construir request
    const request: HealthCheckRequest = {
      environment: environment || undefined,
      clusters: environment ? undefined : [cluster],
      namespaces: selectedNamespaces.length > 0 ? selectedNamespaces : undefined,
      check_deployments: checkDeployments,
      check_services: checkServices,
      check_configs: checkConfigs,
      timeout: timeout,
    };

    // Executar
    const newSessionId = await runHealthCheck(request);

    if (newSessionId) {
      setShowProgressModal(true);
    }
  };

  // Toggle namespace selection
  const toggleNamespace = (namespace: string) => {
    setSelectedNamespaces((prev) =>
      prev.includes(namespace)
        ? prev.filter((ns) => ns !== namespace)
        : [...prev, namespace]
    );
  };

  // Select all/none namespaces
  const selectAllNamespaces = () => {
    setSelectedNamespaces(namespaces.map((ns) => ns.name));
  };

  const clearNamespaces = () => {
    setSelectedNamespaces([]);
  };

  // Filter namespaces (search)
  const [searchQuery, setSearchQuery] = useState("");
  const filteredNamespaces = useMemo(() => {
    if (!searchQuery) return namespaces;
    return namespaces.filter((ns) =>
      ns.name.toLowerCase().includes(searchQuery.toLowerCase())
    );
  }, [namespaces, searchQuery]);

  return (
    <>
      <SplitView
        leftPanel={
          <div className="h-full flex flex-col">
            {/* Header */}
            <div className="flex items-center justify-between p-4 border-b">
              <div className="flex items-center gap-2">
                <Activity className="h-5 w-5 text-blue-600" />
                <h2 className="text-lg font-semibold">Health Check Configuration</h2>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setIsSidebarCollapsed(!isSidebarCollapsed)}
              >
                {isSidebarCollapsed ? (
                  <PanelLeftOpen className="h-4 w-4" />
                ) : (
                  <PanelLeftClose className="h-4 w-4" />
                )}
              </Button>
            </div>

            <ScrollArea className="flex-1">
              <div className="p-4 space-y-6">
                {/* Cluster Info */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Cluster Selecionado</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <Badge variant="outline" className="font-mono">
                      {cluster || "Nenhum cluster selecionado"}
                    </Badge>
                  </CardContent>
                </Card>

                {/* Environment Filter (Optional) */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Filtro de Ambiente (Opcional)</CardTitle>
                    <CardDescription className="text-xs">
                      Deixe vazio para usar apenas o cluster atual
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <Select value={environment} onValueChange={setEnvironment}>
                      <SelectTrigger>
                        <SelectValue placeholder="Selecione ambiente (opcional)" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="">Nenhum (apenas cluster atual)</SelectItem>
                        <SelectItem value="prod">Produção</SelectItem>
                        <SelectItem value="hlg">Homologação</SelectItem>
                        <SelectItem value="all">Todos os ambientes</SelectItem>
                      </SelectContent>
                    </Select>
                  </CardContent>
                </Card>

                {/* Namespace Selection */}
                <Card>
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div>
                        <CardTitle className="text-sm font-medium">Namespaces</CardTitle>
                        <CardDescription className="text-xs">
                          {selectedNamespaces.length === 0
                            ? "Todos os namespaces"
                            : `${selectedNamespaces.length} selecionados`}
                        </CardDescription>
                      </div>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={selectAllNamespaces}
                          className="h-7 text-xs"
                        >
                          Todos
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={clearNamespaces}
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
                        placeholder="Buscar namespaces..."
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        className="h-8"
                      />
                      <ScrollArea className="h-48">
                        <div className="space-y-2">
                          {filteredNamespaces.map((ns) => (
                            <div key={ns.name} className="flex items-center gap-2">
                              <Checkbox
                                id={`ns-${ns.name}`}
                                checked={selectedNamespaces.includes(ns.name)}
                                onCheckedChange={() => toggleNamespace(ns.name)}
                              />
                              <Label
                                htmlFor={`ns-${ns.name}`}
                                className="text-sm cursor-pointer"
                              >
                                {ns.name}
                              </Label>
                            </div>
                          ))}
                        </div>
                      </ScrollArea>
                    </div>
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
                    disabled={isRunning || !cluster}
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
                      </>
                    )}
                  </Button>
                </ProtectedAction>
              </div>
            </ScrollArea>
          </div>
        }
        rightPanel={
          <div className="h-full flex flex-col">
            {/* Header */}
            <div className="flex items-center justify-between p-4 border-b">
              <h2 className="text-lg font-semibold">Resultados</h2>
              <Button variant="ghost" size="sm" onClick={onRefresh}>
                <RefreshCcw className="h-4 w-4" />
              </Button>
            </div>

            {/* Content */}
            <ScrollArea className="flex-1">
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
          </div>
        }
        isLeftCollapsed={isSidebarCollapsed}
        onLeftCollapse={setIsSidebarCollapsed}
      />

      {/* Progress Modal */}
      {sessionId && showProgressModal && (
        <HealthCheckProgressModal
          sessionId={sessionId}
          open={showProgressModal}
          onOpenChange={setShowProgressModal}
          onComplete={(result) => {
            setShowProgressModal(false);
            reset();
            toast.success("Health check concluído!");
          }}
        />
      )}
    </>
  );
};
