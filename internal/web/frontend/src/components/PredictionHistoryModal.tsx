import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Calendar, Search, Loader2, History, TrendingUp, ChevronLeft, ChevronRight, Eye, X } from "lucide-react";
import { toast } from "sonner";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";

interface PredictionHistoryModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster?: string;
  namespace?: string;
  deployment?: string;
  onSelectRecord?: (record: any) => void;
}

interface PredictionRecord {
  id: string;
  cluster: string;
  namespace: string;
  deployment: string;
  health_score: number;
  risk_level: string;
  executive_summary: any;
  predictions: any[];
  recommendations: any[];
  raw_metrics: any;
  provider: string;
  model: string;
  duration_ms: number;
  user_email: string;
  analyzed_at: string;
  created_at: string;
}

interface HistoryResponse {
  records: PredictionRecord[];
  total: number;
  limit: number;
  offset: number;
}

export const PredictionHistoryModal = ({
  open,
  onOpenChange,
  cluster,
  namespace,
  deployment,
  onSelectRecord,
}: PredictionHistoryModalProps) => {
  const [loading, setLoading] = useState(false);
  const [records, setRecords] = useState<PredictionRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize] = useState(20);
  
  // Filtros
  const [filterCluster, setFilterCluster] = useState(cluster || "");
  const [filterNamespace, setFilterNamespace] = useState(namespace || "");
  const [filterDeployment, setFilterDeployment] = useState(deployment || "");
  const [filterRiskLevel, setFilterRiskLevel] = useState("");
  const [filterStartDate, setFilterStartDate] = useState("");
  const [filterEndDate, setFilterEndDate] = useState("");

  const [selectedRecord, setSelectedRecord] = useState<PredictionRecord | null>(null);
  const [viewDetailsOpen, setViewDetailsOpen] = useState(false);

  // Carregar histórico
  const loadHistory = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filterCluster) params.append("cluster", filterCluster);
      if (filterNamespace) params.append("namespace", filterNamespace);
      if (filterDeployment) params.append("deployment", filterDeployment);
      if (filterRiskLevel) params.append("risk_level", filterRiskLevel);
      if (filterStartDate) params.append("start_date", filterStartDate);
      if (filterEndDate) params.append("end_date", filterEndDate);
      params.append("limit", pageSize.toString());
      params.append("offset", ((currentPage - 1) * pageSize).toString());

      const response = await fetch(`/api/v1/predictions/history?${params}`, {
        headers: {
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
      });

      if (!response.ok) throw new Error("Falha ao carregar histórico");

      const data: HistoryResponse = await response.json();
      setRecords(data.records || []);
      setTotal(data.total || 0);
    } catch (error) {
      console.error("Erro ao carregar histórico:", error);
      toast.error("Erro ao carregar histórico de análises");
    } finally {
      setLoading(false);
    }
  };

  // Carregar quando abrir o modal ou mudar filtros
  useEffect(() => {
    if (open) {
      loadHistory();
    }
  }, [open, currentPage, filterCluster, filterNamespace, filterDeployment, filterRiskLevel, filterStartDate, filterEndDate]);

  // Reset filtros
  const handleResetFilters = () => {
    setFilterCluster(cluster || "");
    setFilterNamespace(namespace || "");
    setFilterDeployment(deployment || "");
    setFilterRiskLevel("");
    setFilterStartDate("");
    setFilterEndDate("");
    setCurrentPage(1);
  };

  // Aplicar filtros
  const handleApplyFilters = () => {
    setCurrentPage(1);
    loadHistory();
  };

  // Ver detalhes de um registro
  const handleViewDetails = (record: PredictionRecord) => {
    setSelectedRecord(record);
    setViewDetailsOpen(true);
  };

  // Usar análise no modal principal
  const handleUseAnalysis = (record: PredictionRecord) => {
    if (onSelectRecord) {
      // Reconstituir estrutura completa do raw_metrics se necessário
      let rawMetrics = record.raw_metrics;
      
      // Se raw_metrics não tem a estrutura esperada, criar estrutura padrão
      if (!rawMetrics?.current || !rawMetrics?.trends || !rawMetrics?.node_metrics) {
        console.warn("raw_metrics com estrutura incompleta, criando estrutura padrão");
        rawMetrics = {
          current: {
            cpu_usage_avg: 0,
            cpu_usage_p95: 0,
            memory_usage_avg: 0,
            memory_usage_p95: 0,
            cpu_requests: 0,
            cpu_limits: 0,
            memory_requests: 0,
            memory_limits: 0,
            replicas_desired: 0,
            replicas_available: 0,
            replicas_unavailable: 0,
          },
          day_3_ago: {
            cpu_usage_avg: 0,
            cpu_usage_p95: 0,
            memory_usage_avg: 0,
            memory_usage_p95: 0,
          },
          day_7_ago: {
            cpu_usage_avg: 0,
            cpu_usage_p95: 0,
            memory_usage_avg: 0,
            memory_usage_p95: 0,
          },
          day_10_ago: {
            cpu_usage_avg: 0,
            cpu_usage_p95: 0,
            memory_usage_avg: 0,
            memory_usage_p95: 0,
          },
          day_14_ago: {
            cpu_usage_avg: 0,
            cpu_usage_p95: 0,
            memory_usage_avg: 0,
            memory_usage_p95: 0,
          },
          trends: {
            cpu_change_7d_percent: 0,
            memory_change_7d_percent: 0,
            cpu_change_14d_percent: 0,
            memory_change_14d_percent: 0,
            replicas_stability_score: 100,
          },
          node_metrics: {
            total_capacity: {
              cpu_total_cores: 0,
              cpu_allocatable_cores: 0,
              cpu_utilization_percent: 0,
              memory_total_bytes: 0,
              memory_allocatable_bytes: 0,
              memory_utilization_percent: 0,
            },
            headroom: {
              cpu_available_cores: 0,
              memory_available_bytes: 0,
              cpu_headroom_percent: 0,
              memory_headroom_percent: 0,
            },
          },
          competing_apps: [],
          ...rawMetrics, // Merge com o que vier do banco
        };
      }

      onSelectRecord({
        request_id: record.id,
        cluster: record.cluster,
        namespace: record.namespace,
        deployment: record.deployment,
        health_score: {
          overall: record.health_score,
          category: record.health_score >= 75 ? "healthy" : record.health_score >= 50 ? "warning" : "critical",
          breakdown: {
            availability: 0,
            performance: 0,
            stability: 0,
            efficiency: 0,
          }
        },
        executive_summary: record.executive_summary,
        predictions: record.predictions,
        recommendations: record.recommendations,
        raw_metrics: rawMetrics,
        analyzed_at: record.analyzed_at,
        duration_ms: record.duration_ms,
      });
    }
    onOpenChange(false);
  };

  const getRiskLevelColor = (level: string) => {
    switch (level) {
      case "critical":
        return "bg-red-500/20 text-red-400 border-red-500/50";
      case "high":
        return "bg-orange-500/20 text-orange-400 border-orange-500/50";
      case "medium":
        return "bg-yellow-500/20 text-yellow-400 border-yellow-500/50";
      case "low":
        return "bg-green-500/20 text-green-400 border-green-500/50";
      default:
        return "bg-blue-500/20 text-blue-400 border-blue-500/50";
    }
  };

  const getHealthScoreColor = (score: number) => {
    if (score >= 75) return "text-green-500";
    if (score >= 50) return "text-yellow-500";
    return "text-red-500";
  };

  const totalPages = Math.ceil(total / pageSize);

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent 
          className="max-w-7xl h-[90vh] flex flex-col p-0"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b">
            <DialogTitle className="flex items-center gap-2">
              <History className="w-5 h-5 text-blue-500" />
              Histórico de Análises Preditivas
            </DialogTitle>
            <DialogDescription>
              Consulte análises anteriores realizadas nos deployments
            </DialogDescription>
          </DialogHeader>

          {/* Filtros */}
          <div className="flex-shrink-0 px-6 py-4 border-b bg-muted/30">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-3">
              <Input
                placeholder="Cluster"
                value={filterCluster}
                onChange={(e) => setFilterCluster(e.target.value)}
                className="text-sm"
              />
              <Input
                placeholder="Namespace"
                value={filterNamespace}
                onChange={(e) => setFilterNamespace(e.target.value)}
                className="text-sm"
              />
              <Input
                placeholder="Deployment"
                value={filterDeployment}
                onChange={(e) => setFilterDeployment(e.target.value)}
                className="text-sm"
              />
              <Select value={filterRiskLevel || undefined} onValueChange={(value) => setFilterRiskLevel(value)}>
                <SelectTrigger className="text-sm">
                  <SelectValue placeholder="Todos os Risk Levels" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="critical">Critical</SelectItem>
                  <SelectItem value="high">High</SelectItem>
                  <SelectItem value="medium">Medium</SelectItem>
                  <SelectItem value="low">Low</SelectItem>
                </SelectContent>
              </Select>
              <Input
                type="date"
                placeholder="Data Início"
                value={filterStartDate}
                onChange={(e) => setFilterStartDate(e.target.value)}
                className="text-sm"
              />
              <Input
                type="date"
                placeholder="Data Fim"
                value={filterEndDate}
                onChange={(e) => setFilterEndDate(e.target.value)}
                className="text-sm"
              />
            </div>
            <div className="flex gap-2 mt-3">
              <Button size="sm" onClick={handleApplyFilters} disabled={loading}>
                <Search className="w-4 h-4 mr-2" />
                Buscar
              </Button>
              <Button size="sm" variant="outline" onClick={handleResetFilters}>
                <X className="w-4 h-4 mr-2" />
                Limpar Filtros
              </Button>
            </div>
          </div>

          {/* Lista de Registros */}
          <ScrollArea className="flex-1 px-6">
            <div className="py-4">
              {loading ? (
                <div className="flex items-center justify-center py-20">
                  <Loader2 className="w-8 h-8 animate-spin text-primary" />
                  <span className="ml-3 text-muted-foreground">Carregando histórico...</span>
                </div>
              ) : records.length === 0 ? (
                <div className="text-center py-20 text-muted-foreground">
                  <History className="w-12 h-12 mx-auto mb-4 opacity-50" />
                  <p>Nenhum registro encontrado</p>
                </div>
              ) : (
                <div className="space-y-3">
                  {records.map((record) => (
                    <div
                      key={record.id}
                      className="bg-gradient-card border border-border/50 rounded-lg p-4 hover:border-primary/50 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-4">
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-3 mb-2">
                            <h4 className="font-semibold text-sm truncate">
                              {record.deployment}
                            </h4>
                            <span className={`px-2 py-0.5 rounded text-xs font-semibold border ${getRiskLevelColor(record.risk_level)}`}>
                              {record.risk_level}
                            </span>
                            <span className={`text-2xl font-bold ${getHealthScoreColor(record.health_score)}`}>
                              {record.health_score}
                            </span>
                          </div>
                          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                            <span>📍 {record.cluster} / {record.namespace}</span>
                            <span>🤖 {record.provider} ({record.model})</span>
                            <span>⏱️ {record.duration_ms}ms</span>
                            <span>📅 {format(new Date(record.analyzed_at), "dd/MM/yyyy HH:mm", { locale: ptBR })}</span>
                          </div>
                          {record.executive_summary?.current_state && (
                            <p className="text-sm mt-2 line-clamp-2 text-muted-foreground">
                              {record.executive_summary.current_state}
                            </p>
                          )}
                        </div>
                        <div className="flex gap-2">
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleViewDetails(record)}
                          >
                            <Eye className="w-4 h-4 mr-1" />
                            Detalhes
                          </Button>
                          <Button
                            size="sm"
                            onClick={() => handleUseAnalysis(record)}
                          >
                            <TrendingUp className="w-4 h-4 mr-1" />
                            Usar
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </ScrollArea>

          {/* Paginação */}
          {!loading && records.length > 0 && (
            <div className="flex-shrink-0 px-6 py-4 border-t flex items-center justify-between">
              <div className="text-sm text-muted-foreground">
                Mostrando {((currentPage - 1) * pageSize) + 1} a {Math.min(currentPage * pageSize, total)} de {total} registros
              </div>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
                  disabled={currentPage === 1}
                >
                  <ChevronLeft className="w-4 h-4" />
                  Anterior
                </Button>
                <div className="flex items-center gap-2 px-3">
                  <span className="text-sm">
                    Página {currentPage} de {totalPages}
                  </span>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setCurrentPage((p) => Math.min(totalPages, p + 1))}
                  disabled={currentPage >= totalPages}
                >
                  Próxima
                  <ChevronRight className="w-4 h-4" />
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Modal de Detalhes */}
      <Dialog open={viewDetailsOpen} onOpenChange={setViewDetailsOpen}>
        <DialogContent 
          className="max-w-4xl h-[80vh] flex flex-col p-0"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b">
            <DialogTitle>Detalhes da Análise</DialogTitle>
            <DialogDescription>
              {selectedRecord && `${selectedRecord.deployment} - ${format(new Date(selectedRecord.analyzed_at), "dd/MM/yyyy HH:mm", { locale: ptBR })}`}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="flex-1 px-6">
            {selectedRecord && (
              <div className="py-4 space-y-4">
                {/* Health Score */}
                <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                  <h3 className="font-semibold mb-2">Health Score</h3>
                  <div className={`text-4xl font-bold ${getHealthScoreColor(selectedRecord.health_score)}`}>
                    {selectedRecord.health_score}/100
                  </div>
                </div>

                {/* Executive Summary */}
                <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                  <h3 className="font-semibold mb-2">Resumo Executivo</h3>
                  <p className="text-sm mb-2">{selectedRecord.executive_summary?.current_state}</p>
                  <div className="mb-2">
                    <span className="text-xs font-semibold text-muted-foreground">Nível de Risco: </span>
                    <span className={`px-2 py-1 rounded text-xs font-semibold border ${getRiskLevelColor(selectedRecord.risk_level)}`}>
                      {selectedRecord.risk_level}
                    </span>
                  </div>
                  {selectedRecord.executive_summary?.key_findings && (
                    <div>
                      <span className="text-xs font-semibold text-muted-foreground">Principais Descobertas:</span>
                      <ul className="list-disc list-inside text-sm mt-1 space-y-1">
                        {selectedRecord.executive_summary.key_findings.map((finding: string, idx: number) => (
                          <li key={idx}>{finding}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>

                {/* Predictions */}
                {selectedRecord.predictions && (
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold mb-2">Previsões</h3>
                    <div className="space-y-3">
                      {/* Se predictions é um objeto com short_term, medium_term, long_term */}
                      {!Array.isArray(selectedRecord.predictions) && typeof selectedRecord.predictions === 'object' && (
                        <>
                          {/* Short Term */}
                          {Array.isArray((selectedRecord.predictions as any).short_term) && (selectedRecord.predictions as any).short_term.length > 0 && (
                            <div>
                              <div className="font-semibold text-xs text-muted-foreground mb-1">Curto Prazo (1-7 dias)</div>
                              <div className="space-y-1">
                                {(selectedRecord.predictions as any).short_term.map((pred: any, idx: number) => (
                                  <div key={idx} className="text-sm pl-3 border-l-2 border-blue-500">
                                    <div className="font-medium text-blue-400">{pred.timeframe} - {pred.event}</div>
                                    <div className="text-muted-foreground">{pred.impact}</div>
                                    {pred.probability && <div className="text-xs text-muted-foreground">Probabilidade: {(pred.probability * 100).toFixed(0)}%</div>}
                                    {pred.severity && <div className="text-xs text-yellow-400">Severidade: {pred.severity}</div>}
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}
                          
                          {/* Medium Term */}
                          {Array.isArray((selectedRecord.predictions as any).medium_term) && (selectedRecord.predictions as any).medium_term.length > 0 && (
                            <div>
                              <div className="font-semibold text-xs text-muted-foreground mb-1">Médio Prazo (1-4 semanas)</div>
                              <div className="space-y-1">
                                {(selectedRecord.predictions as any).medium_term.map((pred: any, idx: number) => (
                                  <div key={idx} className="text-sm pl-3 border-l-2 border-yellow-500">
                                    <div className="font-medium text-yellow-400">{pred.timeframe} - {pred.event}</div>
                                    <div className="text-muted-foreground">{pred.impact}</div>
                                    {pred.probability && <div className="text-xs text-muted-foreground">Probabilidade: {(pred.probability * 100).toFixed(0)}%</div>}
                                    {pred.severity && <div className="text-xs text-yellow-400">Severidade: {pred.severity}</div>}
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}
                          
                          {/* Long Term */}
                          {Array.isArray((selectedRecord.predictions as any).long_term) && (selectedRecord.predictions as any).long_term.length > 0 && (
                            <div>
                              <div className="font-semibold text-xs text-muted-foreground mb-1">Longo Prazo (1-3 meses)</div>
                              <div className="space-y-1">
                                {(selectedRecord.predictions as any).long_term.map((pred: any, idx: number) => (
                                  <div key={idx} className="text-sm pl-3 border-l-2 border-orange-500">
                                    <div className="font-medium text-orange-400">{pred.timeframe} - {pred.event}</div>
                                    <div className="text-muted-foreground">{pred.impact}</div>
                                    {pred.probability && <div className="text-xs text-muted-foreground">Probabilidade: {(pred.probability * 100).toFixed(0)}%</div>}
                                    {pred.severity && <div className="text-xs text-orange-400">Severidade: {pred.severity}</div>}
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}
                        </>
                      )}
                      
                      {/* Fallback: se predictions é um array simples */}
                      {Array.isArray(selectedRecord.predictions) && selectedRecord.predictions.length > 0 && (
                        <div className="space-y-2">
                          {selectedRecord.predictions.map((pred: any, idx: number) => (
                            <div key={idx} className="text-sm pl-3 border-l-2 border-blue-500">
                              <div className="font-medium text-blue-400">{pred.timeframe} - {pred.event || pred.description}</div>
                              <div className="text-muted-foreground">{pred.impact || pred.description}</div>
                              {pred.probability && <div className="text-xs text-muted-foreground">Probabilidade: {(pred.probability * 100).toFixed(0)}%</div>}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Recommendations */}
                {Array.isArray(selectedRecord.recommendations) && selectedRecord.recommendations.length > 0 && (
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold mb-2">Recomendações</h3>
                    <div className="space-y-2">
                      {selectedRecord.recommendations.map((rec: any, idx: number) => (
                        <div key={idx} className="text-sm border-l-2 border-green-500 pl-3 py-1">
                          <div className="font-medium text-green-400">{rec.title}</div>
                          <div className="text-muted-foreground mt-0.5">{rec.description}</div>
                          {rec.priority && (
                            <span className={`inline-block mt-1 px-2 py-0.5 rounded text-xs font-semibold ${
                              rec.priority === 1 ? 'bg-red-500/20 text-red-400' :
                              rec.priority === 2 ? 'bg-yellow-500/20 text-yellow-400' :
                              'bg-blue-500/20 text-blue-400'
                            }`}>
                              Prioridade: {rec.priority}
                            </span>
                          )}
                          {rec.expected_impact && (
                            <div className="text-xs text-muted-foreground mt-1">
                              Impacto: {rec.expected_impact}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Metadata */}
                <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                  <h3 className="font-semibold mb-2">Metadados</h3>
                  <div className="grid grid-cols-2 gap-2 text-sm">
                    <div><span className="text-muted-foreground">Cluster:</span> {selectedRecord.cluster}</div>
                    <div><span className="text-muted-foreground">Namespace:</span> {selectedRecord.namespace}</div>
                    <div><span className="text-muted-foreground">Provider:</span> {selectedRecord.provider}</div>
                    <div><span className="text-muted-foreground">Model:</span> {selectedRecord.model}</div>
                    <div><span className="text-muted-foreground">Duração:</span> {selectedRecord.duration_ms}ms</div>
                    <div><span className="text-muted-foreground">Usuário:</span> {selectedRecord.user_email}</div>
                  </div>
                </div>
              </div>
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  );
};
