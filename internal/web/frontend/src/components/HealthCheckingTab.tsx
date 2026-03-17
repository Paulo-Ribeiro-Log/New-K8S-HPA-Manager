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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
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
  Filter,
  FilterX,
  Settings,
  Clock,
  Download,
  CheckCircle2,
  BarChart3,
} from "lucide-react";
import { toast } from "sonner";
import { useHealthChecking } from "@/hooks/useHealthChecking";
import type { HealthCheckRequest } from "@/types/healthcheck";
import { HealthCheckProgressModal } from "@/components/HealthCheckProgressModal";
import { HealthCheckResultsPanel, type SelectedAlert } from "@/components/HealthCheckResultsPanel";
import { FiltersManagementModal } from "@/components/FiltersManagementModal";
import { HealthCheckHistoryModal } from "@/components/HealthCheckHistoryModal";
import { ExportReportModal } from "@/components/ExportReportModal";
import { HealthCheckMetricsDashboard } from "@/components/HealthCheckMetricsDashboard";
import { useClusters, useNamespaces } from "@/hooks/useAPI";
import { ProtectedAction } from "@/components/rbac";
import { useFilters } from "@/hooks/useFilters";
import { apiClient } from "@/lib/api/client";
import type { HealthCheckResult } from "@/types/healthcheck";

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
    markCompleted,
    markCancelled,
  } = useHealthChecking();

  // Filters hook
  const { addRule, fetchRules } = useFilters();

  // Configuration State
  const [environment, setEnvironment] = useState<string>("");
  const [selectedClusters, setSelectedClusters] = useState<string[]>([]);
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [checkDeployments, setCheckDeployments] = useState(true);
  const [checkServices, setCheckServices] = useState(true);
  const [checkConfigs, setCheckConfigs] = useState(true);
  const [checkEvents, setCheckEvents] = useState(false); // Verificar eventos K8s (desabilitado por padrão)
  const [checkHPAs, setCheckHPAs] = useState(true);      // Verificar HPAs (habilitado por padrão)
  const [checkPVCs, setCheckPVCs] = useState(true);      // Verificar PVCs (habilitado por padrão)
  const [checkDynatrace, setCheckDynatrace] = useState(false); // Verificar problems Dynatrace
  const [timeout, setTimeout] = useState(30); // Timeout geral (fallback)
  const [showAdvancedTimeouts, setShowAdvancedTimeouts] = useState(false);
  const [timeoutDeployments, setTimeoutDeployments] = useState(60); // Padrão: 60s
  const [timeoutServices, setTimeoutServices] = useState(45);      // Padrão: 45s
  const [timeoutConfigs, setTimeoutConfigs] = useState(30);        // Padrão: 30s
  const [timeoutEvents, setTimeoutEvents] = useState(30);          // Padrão: 30s
  const [timeoutHPAs, setTimeoutHPAs] = useState(45);              // Padrão: 45s
  const [timeoutPVCs, setTimeoutPVCs] = useState(30);              // Padrão: 30s
  const [applyFilters, setApplyFilters] = useState(true); // ✅ Filtrar falsos positivos por padrão

  // Modal state
  const [showProgressModal, setShowProgressModal] = useState(false);
  const [showFiltersModal, setShowFiltersModal] = useState(false);
  const [showHistoryModal, setShowHistoryModal] = useState(false);
  const [showExportModal, setShowExportModal] = useState(false);

  // Tab state para painel direito
  const [rightPanelTab, setRightPanelTab] = useState<"results" | "metrics">("results");
  const [preloadedLogs, setPreloadedLogs] = useState<{
    cluster: string;
    sessionId: string;
    events: any[];
    result: HealthCheckResult; // ✅ Incluir resultado completo para exibir badges
  } | null>(null);

  // Handle adding alerts to whitelist
  const handleAddToWhitelist = async (alerts: SelectedAlert[]) => {
    if (alerts.length === 0) {
      toast.error("Nenhum alerta selecionado");
      return;
    }

    toast.loading(`Adicionando ${alerts.length} alerta(s) à whitelist...`, { id: "add-whitelist" });

    let successCount = 0;
    let failureCount = 0;

    for (const alert of alerts) {
      const rule = {
        type: "exact" as const,
        resource_type: alert.resource_type,
        namespace: alert.namespace,
        name: alert.name,
        reason: `Auto-adicionado via Health Check: ${alert.message}`,
      };

      const success = await addRule(rule);
      if (success) {
        successCount++;
      } else {
        failureCount++;
      }
    }

    toast.dismiss("add-whitelist");

    if (failureCount === 0) {
      toast.success(`✅ ${successCount} alerta(s) adicionado(s) à whitelist com sucesso!`);
    } else {
      toast.warning(`⚠️ ${successCount} adicionado(s), ${failureCount} falharam`);
    }

    // Atualizar lista de filtros no modal (se abrir depois)
    await fetchRules();

    // Abrir modal para visualizar os filtros adicionados
    setShowFiltersModal(true);
  };

  // Handle viewing logs from completed health check
  const handleShowProgress = async (cluster: string, result: HealthCheckResult) => {
    try {
      // O result.id já é o sessionID correto para aquele cluster
      // (formato: baseSessionID-clusterName)
      console.log(`[HealthCheckingTab] Buscando logs para cluster: ${cluster}, sessionID: ${result.id}`);
      console.log(`[HealthCheckingTab] Resultado recebido do badge:`, result);

      // Buscar eventos salvos no banco de dados
      const response = await apiClient.getHealthCheckEvents(result.id);

      console.log(`[HealthCheckingTab] Response de eventos recebida:`, response);

      if (response.success && response.events) {
        // Se não há eventos, mostrar mensagem
        if (response.events.length === 0) {
          toast.warning(`Nenhum log encontrado para o cluster ${cluster}`);
          return;
        }

        // Formatar eventos para o formato esperado pelo modal
        const formattedEvents = response.events.map((event: any) => ({
          id: event.id,
          type: event.type,
          phase: event.phase,
          message: event.message,
          progress: event.progress,
          status: event.status,
          timestamp: new Date(event.timestamp),
        }));

        console.log(`[HealthCheckingTab] ${formattedEvents.length} eventos formatados`);

        // ✅ Armazenar logs pré-carregados COM o resultado completo
        setPreloadedLogs({
          cluster,
          sessionId: result.id,
          events: formattedEvents,
          result, // ✅ Incluir resultado completo (já vem do badge com todos os counts)
        });

        console.log(`[HealthCheckingTab] PreloadedLogs setado com resultado completo:`, {
          cluster,
          sessionId: result.id,
          eventsCount: formattedEvents.length,
          resultCounts: {
            total: result.total_checks,
            healthy: result.healthy_count,
            warning: result.warning_count,
            critical: result.critical_count,
          },
        });

        // Abrir modal em modo visualização
        setShowProgressModal(true);
        toast.success(`${formattedEvents.length} eventos carregados para ${cluster}`);
      } else {
        toast.error("Erro ao carregar logs: resposta inválida do servidor");
      }
    } catch (error) {
      console.error("[HealthCheckingTab] Failed to fetch logs:", error);
      toast.error(`Erro ao carregar logs: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    }
  };

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

    if (!checkDeployments && !checkServices && !checkConfigs && !checkEvents && !checkHPAs && !checkPVCs && !checkDynatrace) {
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
      check_events: checkEvents,
      check_hpas: checkHPAs,
      check_pvcs: checkPVCs,
      check_dynatrace: checkDynatrace,
      timeout: timeout,
      // Timeouts específicos (se modo avançado habilitado)
      timeout_deployments: showAdvancedTimeouts ? timeoutDeployments : undefined,
      timeout_services: showAdvancedTimeouts ? timeoutServices : undefined,
      timeout_configs: showAdvancedTimeouts ? timeoutConfigs : undefined,
      timeout_events: showAdvancedTimeouts ? timeoutEvents : undefined,
      timeout_hpas: showAdvancedTimeouts ? timeoutHPAs : undefined,
      timeout_pvcs: showAdvancedTimeouts ? timeoutPVCs : undefined,
      apply_filters: applyFilters, // ✅ Aplicar filtros de falsos positivos
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
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-events"
                        checked={checkEvents}
                        onCheckedChange={(checked) => setCheckEvents(checked as boolean)}
                      />
                      <Label htmlFor="check-events" className="text-sm cursor-pointer">
                        Verificar Eventos K8s
                      </Label>
                      <Badge variant="outline" className="text-xs">
                        FailedScheduling, OOM, etc.
                      </Badge>
                    </div>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-hpas"
                        checked={checkHPAs}
                        onCheckedChange={(checked) => setCheckHPAs(checked as boolean)}
                      />
                      <Label htmlFor="check-hpas" className="text-sm cursor-pointer">
                        Validar HPAs
                      </Label>
                      <Badge variant="outline" className="text-xs">
                        min=max, métricas, scaling
                      </Badge>
                    </div>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-pvcs"
                        checked={checkPVCs}
                        onCheckedChange={(checked) => setCheckPVCs(checked as boolean)}
                      />
                      <Label htmlFor="check-pvcs" className="text-sm cursor-pointer">
                        Validar PVCs
                      </Label>
                      <Badge variant="outline" className="text-xs">
                        status, StorageClass, bindings
                      </Badge>
                    </div>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="check-dynatrace"
                        checked={checkDynatrace}
                        onCheckedChange={(checked) => setCheckDynatrace(checked as boolean)}
                      />
                      <Label htmlFor="check-dynatrace" className="text-sm cursor-pointer">
                        Problems Dynatrace
                      </Label>
                      <Badge variant="outline" className="text-xs bg-purple-50 dark:bg-purple-950/20 text-purple-700 dark:text-purple-400 border-purple-300">
                        requer token DT configurado
                      </Badge>
                    </div>
                  </CardContent>
                </Card>

                {/* Filtros de Falsos Positivos */}
                <Card>
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div>
                        <CardTitle className="text-sm font-medium">Filtros Inteligentes</CardTitle>
                        <CardDescription className="text-xs">
                          {applyFilters
                            ? "Filtrando falsos positivos conhecidos"
                            : "Exibindo TODOS os resultados (pode conter falsos positivos)"}
                        </CardDescription>
                      </div>
                      <Button
                        variant={applyFilters ? "default" : "outline"}
                        size="sm"
                        onClick={() => setApplyFilters(!applyFilters)}
                        className="h-8 gap-2"
                      >
                        {applyFilters ? (
                          <>
                            <Filter className="h-4 w-4" />
                            Filtros Ativos
                          </>
                        ) : (
                          <>
                            <FilterX className="h-4 w-4" />
                            Sem Filtros
                          </>
                        )}
                      </Button>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-2">
                      <div className="text-xs text-muted-foreground space-y-1">
                        <p>
                          <strong>Filtros incluem:</strong>
                        </p>
                        <ul className="list-disc list-inside space-y-0.5 ml-2">
                          <li>ConfigMaps vazios conhecidos (ex: ingress-controller-leader)</li>
                          <li>Secrets de sistema sem dados (ex: service account tokens)</li>
                          <li>Recursos internos do Kubernetes (kube-system)</li>
                        </ul>
                      </div>

                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowFiltersModal(true)}
                        className="w-full gap-2"
                      >
                        <Settings className="h-4 w-4" />
                        Gerenciar Filtros
                      </Button>
                    </div>
                  </CardContent>
                </Card>

                {/* Timeout */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Timeout</CardTitle>
                    <CardDescription className="text-xs">
                      {showAdvancedTimeouts
                        ? "Timeouts específicos por tipo de check"
                        : "Timeout geral em segundos (máx: 120s)"}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {!showAdvancedTimeouts ? (
                      <Input
                        type="number"
                        min={5}
                        max={120}
                        value={timeout}
                        onChange={(e) => setTimeout(parseInt(e.target.value) || 30)}
                        className="h-8"
                      />
                    ) : (
                      <div className="space-y-2">
                        <div className="flex items-center gap-2">
                          <Label className="text-xs w-24">Deployments:</Label>
                          <Input
                            type="number"
                            min={10}
                            max={300}
                            value={timeoutDeployments}
                            onChange={(e) => setTimeoutDeployments(parseInt(e.target.value) || 60)}
                            className="h-7 text-xs"
                          />
                          <span className="text-xs text-muted-foreground">s</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Label className="text-xs w-24">Services:</Label>
                          <Input
                            type="number"
                            min={10}
                            max={300}
                            value={timeoutServices}
                            onChange={(e) => setTimeoutServices(parseInt(e.target.value) || 45)}
                            className="h-7 text-xs"
                          />
                          <span className="text-xs text-muted-foreground">s</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Label className="text-xs w-24">Configs:</Label>
                          <Input
                            type="number"
                            min={10}
                            max={300}
                            value={timeoutConfigs}
                            onChange={(e) => setTimeoutConfigs(parseInt(e.target.value) || 30)}
                            className="h-7 text-xs"
                          />
                          <span className="text-xs text-muted-foreground">s</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Label className="text-xs w-24">Eventos:</Label>
                          <Input
                            type="number"
                            min={10}
                            max={300}
                            value={timeoutEvents}
                            onChange={(e) => setTimeoutEvents(parseInt(e.target.value) || 30)}
                            className="h-7 text-xs"
                            disabled={!checkEvents}
                          />
                          <span className="text-xs text-muted-foreground">s</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Label className="text-xs w-24">HPAs:</Label>
                          <Input
                            type="number"
                            min={10}
                            max={300}
                            value={timeoutHPAs}
                            onChange={(e) => setTimeoutHPAs(parseInt(e.target.value) || 45)}
                            className="h-7 text-xs"
                            disabled={!checkHPAs}
                          />
                          <span className="text-xs text-muted-foreground">s</span>
                        </div>
                      </div>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setShowAdvancedTimeouts(!showAdvancedTimeouts)}
                      className="w-full text-xs h-7"
                    >
                      {showAdvancedTimeouts ? "Usar timeout único" : "Configurar por tipo"}
                    </Button>
                  </CardContent>
                </Card>

                {/* Run Button */}
                <ProtectedAction
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
          title: "",
          content: (
            <Tabs value={rightPanelTab} onValueChange={(v) => setRightPanelTab(v as "results" | "metrics")} className="h-full flex flex-col">
              {/* Tabs Header com ações */}
              <div className="flex items-center justify-between px-4 pt-2 pb-0 border-b">
                <TabsList className="h-9">
                  <TabsTrigger value="results" className="text-xs gap-1.5">
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    Resultados
                    {results.length > 0 && (
                      <Badge variant="secondary" className="ml-1 h-4 px-1 text-[10px]">
                        {results.length}
                      </Badge>
                    )}
                  </TabsTrigger>
                  <TabsTrigger value="metrics" className="text-xs gap-1.5">
                    <BarChart3 className="h-3.5 w-3.5" />
                    Métricas
                  </TabsTrigger>
                </TabsList>

                {/* Ações do painel (só aparecem na aba Resultados) */}
                {rightPanelTab === "results" && (
                  <div className="flex items-center gap-2">
                    {selectedClusters.length > 0 && (
                      <Badge variant="secondary" className="gap-1">
                        <Server className="h-3 w-3" />
                        {selectedClusters.length} cluster{selectedClusters.length > 1 ? 's' : ''}
                      </Badge>
                    )}
                    {/* Botão Ver Progresso - Apenas quando está executando */}
                    {isRunning && sessionId && (
                      <Button
                        variant="default"
                        size="sm"
                        onClick={() => setShowProgressModal(true)}
                        className="h-7 animate-pulse"
                      >
                        <Activity className="mr-1.5 h-3.5 w-3.5" />
                        <span className="text-xs">Ver Progresso</span>
                      </Button>
                    )}
                    {/* Botão Exportar Relatório - Aparece quando há resultados */}
                    {results.length > 0 && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowExportModal(true)}
                        className="h-7"
                      >
                        <Download className="mr-1.5 h-3.5 w-3.5" />
                        <span className="text-xs">Exportar</span>
                      </Button>
                    )}
                    {/* Botão Histórico - Sempre disponível */}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setShowHistoryModal(true)}
                      className="h-7"
                    >
                      <Clock className="mr-1.5 h-3.5 w-3.5" />
                      <span className="text-xs">Histórico</span>
                    </Button>
                  </div>
                )}
              </div>

              {/* Tab Content: Resultados */}
              <TabsContent value="results" className="flex-1 m-0 overflow-hidden">
                <ScrollArea className="h-full">
                  <HealthCheckResultsPanel
                    results={results}
                    isRunning={isRunning}
                    runningClusters={selectedClusters}
                    onShowProgress={handleShowProgress}
                    onAddToWhitelist={handleAddToWhitelist}
                  />
                </ScrollArea>
              </TabsContent>

              {/* Tab Content: Métricas Dashboard */}
              <TabsContent value="metrics" className="flex-1 m-0 overflow-hidden">
                <ScrollArea className="h-full">
                  <HealthCheckMetricsDashboard />
                </ScrollArea>
              </TabsContent>
            </Tabs>
          ),
        }}
      />

      {/* Progress Modal */}
      {(sessionId || preloadedLogs) && showProgressModal && (
        <HealthCheckProgressModal
          sessionId={preloadedLogs?.sessionId || sessionId}
          clusterSessions={preloadedLogs ? { [preloadedLogs.cluster]: preloadedLogs.sessionId } : clusterSessions}
          open={showProgressModal}
          preloadedEvents={preloadedLogs?.events}
          preloadedResult={preloadedLogs?.result} // ✅ Passar resultado completo pré-carregado
          viewMode={!!preloadedLogs} // viewMode apenas quando tem preloadedLogs (visualização de histórico)
          onOpenChange={(open) => {
            // Permitir fechar apenas se não está rodando ou se usuário confirmar
            if (!open && isRunning && !preloadedLogs) {
              const confirmed = window.confirm(
                "Análises ainda em andamento. Tem certeza que deseja fechar? " +
                "Você pode reabrir o progresso clicando em 'Ver Progresso' no painel de Resultados."
              );
              if (!confirmed) {
                return; // Impede fechar
              }
            }
            setShowProgressModal(open);
            // Limpar logs pré-carregados ao fechar
            if (!open) {
              setPreloadedLogs(null);
            }
            // NÃO fazer reset - manter resultados no painel
          }}
          onComplete={(result) => {
            // Armazenar resultado no painel
            markCompleted(result);
            toast.success(`Health check concluído para cluster: ${result.cluster}`);
          }}
          onCancel={markCancelled}
        />
      )}

      {/* Filters Management Modal */}
      <FiltersManagementModal
        open={showFiltersModal}
        onOpenChange={setShowFiltersModal}
      />

      {/* History Modal */}
      <HealthCheckHistoryModal
        open={showHistoryModal}
        onOpenChange={setShowHistoryModal}
        onSelectHistory={(result) => handleShowProgress(result.cluster, result)}
      />

      {/* Export Report Modal */}
      <ExportReportModal
        open={showExportModal}
        onOpenChange={setShowExportModal}
        results={results.reduce((acc, result) => {
          acc[result.cluster] = result;
          return acc;
        }, {} as Record<string, HealthCheckResult>)}
      />
    </>
  );
};
