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
import type { AnalysisResult, ResourceType } from "@/types/ai";
import { Clock, Trash2, Eye, RefreshCw, Search } from "lucide-react";
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

  // Filtrar histórico
  const filteredHistory = useMemo(() => {
    // Garantir que history é sempre um array
    if (!Array.isArray(history)) {
      return [];
    }

    return history.filter((item) => {
      // Verificações de segurança para campos opcionais
      const resourceName = item.resourceName || "";
      const namespace = item.namespace || "";
      const cluster = item.cluster || "";
      const resourceType = item.resourceType || "";
      const provider = item.provider || "";

      const matchesSearch =
        searchQuery === "" ||
        resourceName.toLowerCase().includes(searchQuery.toLowerCase()) ||
        namespace.toLowerCase().includes(searchQuery.toLowerCase()) ||
        cluster.toLowerCase().includes(searchQuery.toLowerCase());

      const matchesResourceType =
        resourceTypeFilter === "all" || resourceType === resourceTypeFilter;

      const matchesProvider =
        providerFilter === "all" || provider === providerFilter;

      return matchesSearch && matchesResourceType && matchesProvider;
    });
  }, [history, searchQuery, resourceTypeFilter, providerFilter]);

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
    const date = new Date(dateString);
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
            <CardTitle>📜 Histórico de Análises</CardTitle>
            <CardDescription>
              {filteredHistory.length} análise(s) encontrada(s)
            </CardDescription>
          </div>
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

        {/* Filtros */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-2 mt-4">
          <div className="relative">
            <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Buscar por nome, namespace, cluster..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-8"
            />
          </div>

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
        </div>
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
              {filteredHistory.map((item) => (
                <div
                  key={item.id}
                  className="border rounded-lg p-3 hover:bg-muted/50 cursor-pointer transition-colors"
                  onClick={() => onViewAnalysis(item)}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 space-y-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <Badge variant="outline">{item.resourceType}</Badge>
                        <Badge variant="secondary">{item.provider}</Badge>
                        <span className="text-sm font-medium">
                          {item.resourceName}
                        </span>
                      </div>

                      <div className="text-xs text-muted-foreground">
                        {item.cluster}/{item.namespace}
                      </div>

                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {formatDate(item.analyzedAt)}
                        </span>
                        {item.responseTime && (
                          <span>⚡ {item.responseTime.toFixed(1)}s</span>
                        )}
                        {item.suggestions && (
                          <span>💡 {item.suggestions.length} sugestões</span>
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
                        onClick={(e) => handleDelete(item.id, item.resourceName, e)}
                        className="text-red-500 hover:text-red-600 hover:bg-red-50"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
