import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import type { AnalysisResult, ResourceType } from "@/types/ai";
import { Clock, Trash2, Eye, RefreshCw, Search, Filter, ChevronDown, Calendar, X } from "lucide-react";
import { useState, useMemo } from "react";
import { useToast } from "@/components/ui/use-toast";

interface AIHistoryPanelProps {
  history: AnalysisResult[];
  isLoading: boolean;
  onRefresh: () => void;
  onViewAnalysis: (analysis: AnalysisResult) => void;
  onDeleteAnalysis: (id: string) => void;
}

export function AIHistoryPanel({
  history,
  isLoading,
  onRefresh,
  onViewAnalysis,
  onDeleteAnalysis,
}: AIHistoryPanelProps) {
  const { toast } = useToast();
  const [searchQuery, setSearchQuery] = useState("");
  const [resourceTypeFilter, setResourceTypeFilter] = useState<ResourceType | "all">("all");
  const [providerFilter, setProviderFilter] = useState<"all" | "gemini" | "ollama" | "claude" | "openai" | "copilot">("all");

  // Novos filtros avançados
  const [clusterFilter, setClusterFilter] = useState<string>("all");
  const [namespaceFilter, setNamespaceFilter] = useState<string>("all");
  const [severityFilter, setSeverityFilter] = useState<"all" | "critical" | "high" | "medium" | "low">("all");
  const [dateFilter, setDateFilter] = useState<"all" | "today" | "week" | "month">("all");
  const [isFiltersOpen, setIsFiltersOpen] = useState(true);

  // Extrair valores únicos para os filtros
  const uniqueClusters = useMemo(() => {
    if (!Array.isArray(history)) return [];
    const clusters = history
      .map((item) => item.cluster)
      .filter((cluster): cluster is string => !!cluster);
    return Array.from(new Set(clusters)).sort();
  }, [history]);

  const uniqueNamespaces = useMemo(() => {
    if (!Array.isArray(history)) return [];
    const namespaces = history
      .map((item) => item.namespace)
      .filter((namespace): namespace is string => !!namespace);
    return Array.from(new Set(namespaces)).sort();
  }, [history]);

  // Filtrar histórico com todos os filtros
  const filteredHistory = useMemo(() => {
    // Garantir que history é sempre um array
    if (!Array.isArray(history)) {
      return [];
    }

    return history.filter((item) => {
      // Verificações de segurança para campos opcionais
      const resourceName = item.resource_name || "";
      const namespace = item.namespace || "";
      const cluster = item.cluster || "";
      const resourceType = item.resource_type || "";
      const provider = item.provider || "";
      const severity = item.executive_summary?.severity || "";
      const analyzedAt = item.analyzed_at;

      // Filtro de busca
      const matchesSearch =
        searchQuery === "" ||
        resourceName.toLowerCase().includes(searchQuery.toLowerCase()) ||
        namespace.toLowerCase().includes(searchQuery.toLowerCase()) ||
        cluster.toLowerCase().includes(searchQuery.toLowerCase());

      // Filtro de tipo de recurso
      const matchesResourceType =
        resourceTypeFilter === "all" || resourceType === resourceTypeFilter;

      // Filtro de provider
      const matchesProvider =
        providerFilter === "all" || provider === providerFilter;

      // Filtro de cluster
      const matchesCluster = clusterFilter === "all" || cluster === clusterFilter;

      // Filtro de namespace
      const matchesNamespace =
        namespaceFilter === "all" || namespace === namespaceFilter;

      // Filtro de severity (se estruturado disponível)
      const matchesSeverity =
        severityFilter === "all" || severity === severityFilter;

      // Filtro de data
      let matchesDate = true;
      if (dateFilter !== "all" && analyzedAt) {
        const itemDate = new Date(analyzedAt);
        const now = new Date();
        const diffMs = now.getTime() - itemDate.getTime();
        const diffDays = diffMs / (1000 * 60 * 60 * 24);

        if (dateFilter === "today") {
          matchesDate = diffDays < 1;
        } else if (dateFilter === "week") {
          matchesDate = diffDays < 7;
        } else if (dateFilter === "month") {
          matchesDate = diffDays < 30;
        }
      }

      return (
        matchesSearch &&
        matchesResourceType &&
        matchesProvider &&
        matchesCluster &&
        matchesNamespace &&
        matchesSeverity &&
        matchesDate
      );
    });
  }, [
    history,
    searchQuery,
    resourceTypeFilter,
    providerFilter,
    clusterFilter,
    namespaceFilter,
    severityFilter,
    dateFilter,
  ]);

  // Função para limpar todos os filtros
  const clearAllFilters = () => {
    setSearchQuery("");
    setResourceTypeFilter("all");
    setProviderFilter("all");
    setClusterFilter("all");
    setNamespaceFilter("all");
    setSeverityFilter("all");
    setDateFilter("all");
    toast({
      title: "Filtros limpos",
      description: "Todos os filtros foram removidos",
    });
  };

  // Contador de filtros ativos
  const activeFiltersCount = useMemo(() => {
    let count = 0;
    if (searchQuery !== "") count++;
    if (resourceTypeFilter !== "all") count++;
    if (providerFilter !== "all") count++;
    if (clusterFilter !== "all") count++;
    if (namespaceFilter !== "all") count++;
    if (severityFilter !== "all") count++;
    if (dateFilter !== "all") count++;
    return count;
  }, [
    searchQuery,
    resourceTypeFilter,
    providerFilter,
    clusterFilter,
    namespaceFilter,
    severityFilter,
    dateFilter,
  ]);

  const handleDelete = async (id: string, resourceName: string, e: React.MouseEvent) => {
    e.stopPropagation();

    const confirmed = window.confirm(
      `Deseja realmente deletar a análise de "${resourceName}"?`
    );

    if (confirmed) {
      onDeleteAnalysis(id);
    }
  };

  const formatDate = (dateString: string) => {
    if (!dateString) return "data desconhecida";

    const date = new Date(dateString);

    // Validar se a data é válida
    if (isNaN(date.getTime())) return "data inválida";

    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return "agora";
    if (diffMins < 60) return `${diffMins}min atrás`;
    if (diffHours < 24) return `${diffHours}h atrás`;
    if (diffDays < 7) return `${diffDays}d atrás`;

    return date.toLocaleDateString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              Histórico de Análises
              {activeFiltersCount > 0 && (
                <Badge variant="secondary" className="text-xs">
                  {activeFiltersCount} filtro(s)
                </Badge>
              )}
            </CardTitle>
            <CardDescription>
              {filteredHistory.length} de {history?.length || 0} análise(s)
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {activeFiltersCount > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={clearAllFilters}
                className="text-xs"
              >
                <X className="h-3 w-3 mr-1" />
                Limpar
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={onRefresh}
              disabled={isLoading}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
          </div>
        </div>

        {/* Filtros Avançados - Collapsible */}
        <Collapsible
          open={isFiltersOpen}
          onOpenChange={setIsFiltersOpen}
          className="mt-4"
        >
          <CollapsibleTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-between p-2 h-auto hover:bg-muted/50"
            >
              <div className="flex items-center gap-2">
                <Filter className="h-4 w-4" />
                <span className="text-sm font-medium">Filtros Avançados</span>
              </div>
              <ChevronDown
                className={`h-4 w-4 transition-transform ${
                  isFiltersOpen ? "rotate-180" : ""
                }`}
              />
            </Button>
          </CollapsibleTrigger>

          <CollapsibleContent className="space-y-3 mt-3">
            {/* Linha 1: Busca */}
            <div className="relative">
              <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Buscar por nome, namespace, cluster..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-8"
              />
            </div>

            {/* Linha 2: Resource Type + Provider + Severity */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <Select
                value={resourceTypeFilter}
                onValueChange={(value) => setResourceTypeFilter(value as ResourceType | "all")}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Tipo de recurso" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos os tipos</SelectItem>
                  <SelectItem value="Pod">Pod</SelectItem>
                  <SelectItem value="Deployment">Deployment</SelectItem>
                  <SelectItem value="HPA">HPA</SelectItem>
                  <SelectItem value="Node">Node</SelectItem>
                </SelectContent>
              </Select>

              <Select
                value={providerFilter}
                onValueChange={(value) => setProviderFilter(value as "all" | "gemini" | "ollama" | "claude" | "openai" | "copilot")}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Provider AI" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos providers</SelectItem>
                  <SelectItem value="ollama">Ollama</SelectItem>
                  <SelectItem value="gemini">Gemini</SelectItem>
                  <SelectItem value="claude">Claude</SelectItem>
                  <SelectItem value="openai">OpenAI</SelectItem>
                  <SelectItem value="copilot">GitHub Copilot</SelectItem>
                </SelectContent>
              </Select>

              <Select
                value={severityFilter}
                onValueChange={(value) => setSeverityFilter(value as "all" | "critical" | "high" | "medium" | "low")}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Severidade" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todas severidades</SelectItem>
                  <SelectItem value="critical">Critical</SelectItem>
                  <SelectItem value="high">High</SelectItem>
                  <SelectItem value="medium">Medium</SelectItem>
                  <SelectItem value="low">Low</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Linha 3: Cluster + Namespace + Data */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <Select value={clusterFilter} onValueChange={setClusterFilter}>
                <SelectTrigger>
                  <SelectValue placeholder="Cluster" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos clusters</SelectItem>
                  {uniqueClusters.map((cluster) => (
                    <SelectItem key={cluster} value={cluster}>
                      {cluster}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Select value={namespaceFilter} onValueChange={setNamespaceFilter}>
                <SelectTrigger>
                  <SelectValue placeholder="Namespace" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos namespaces</SelectItem>
                  {uniqueNamespaces.map((namespace) => (
                    <SelectItem key={namespace} value={namespace}>
                      {namespace}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Select value={dateFilter} onValueChange={setDateFilter}>
                <SelectTrigger>
                  <SelectValue placeholder="Período" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">
                    <div className="flex items-center gap-2">
                      <Calendar className="h-4 w-4" />
                      Todas as datas
                    </div>
                  </SelectItem>
                  <SelectItem value="today">Hoje</SelectItem>
                  <SelectItem value="week">Última semana</SelectItem>
                  <SelectItem value="month">Último mês</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </CardHeader>

      <CardContent>
        <ScrollArea className="h-[400px] w-full pr-4">
          {isLoading && filteredHistory.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-muted-foreground">
              <RefreshCw className="h-6 w-6 animate-spin mr-2" />
              Carregando histórico...
            </div>
          ) : filteredHistory.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-muted-foreground">
              📭 Nenhuma análise encontrada
            </div>
          ) : (
            <div className="space-y-2">
              {filteredHistory.map((item) => {
                // Extrair severity se disponível
                const severity = item.executive_summary?.severity;
                const getSeverityBadge = (sev: string | undefined) => {
                  if (!sev) return null;

                  const variants: Record<string, { variant: "default" | "destructive" | "outline" | "secondary"; className: string }> = {
                    critical: { variant: "destructive" as const, className: "bg-red-600 text-white" },
                    high: { variant: "default" as const, className: "bg-orange-500 text-white" },
                    medium: { variant: "default" as const, className: "bg-yellow-500 text-white" },
                    low: { variant: "default" as const, className: "bg-blue-500 text-white" },
                  };

                  const config = variants[sev.toLowerCase()] || variants.low;
                  return (
                    <Badge variant={config.variant} className={`${config.className} text-xs uppercase`}>
                      {sev}
                    </Badge>
                  );
                };

                return (
                  <div
                    key={item.id}
                    className="border rounded-lg p-3 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={() => onViewAnalysis(item)}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex-1 space-y-1">
                        <div className="flex items-center gap-2 flex-wrap">
                          <Badge variant="outline">
                            {item.resource_type || "Unknown"}
                          </Badge>
                          <Badge variant="secondary">
                            {item.provider || "Unknown"}
                          </Badge>
                          {severity && getSeverityBadge(severity)}
                          <span className="text-sm font-medium">
                            {item.resource_name || "sem nome"}
                          </span>
                        </div>

                        <div className="text-xs text-muted-foreground">
                          {item.cluster || "?"}/{item.namespace || "?"}
                        </div>

                        <div className="flex items-center gap-3 text-xs text-muted-foreground">
                          <span className="flex items-center gap-1">
                            <Clock className="h-3 w-3" />
                            {formatDate(item.analyzed_at)}
                          </span>
                          {item.response_time && (
                            <span className="flex items-center gap-1">
                              <span className="text-green-600">⚡</span>
                              {item.response_time.toFixed(1)}s
                            </span>
                          )}
                          {item.suggestions && item.suggestions.length > 0 && (
                            <span className="flex items-center gap-1">
                              <span className="text-blue-600">💡</span>
                              {item.suggestions.length} sugestões
                            </span>
                          )}
                          {item.recommendations && item.recommendations.length > 0 && (
                            <span className="flex items-center gap-1">
                              <span className="text-purple-600">🎯</span>
                              {item.recommendations.length} ações
                            </span>
                          )}
                        </div>
                      </div>

                      <div className="flex items-center gap-1 flex-shrink-0">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => onViewAnalysis(item)}
                        >
                          <Eye className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => handleDelete(item.id, item.resource_name, e)}
                          className="text-red-500 hover:text-red-600 hover:bg-red-50"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
