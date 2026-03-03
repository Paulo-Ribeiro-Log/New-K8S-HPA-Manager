import { useState, useEffect } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, TrendingUp, Clock, ChevronRight } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface NodePoolPredictionHistoryModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster?: string;
  nodepool?: string;
  onSelectRecord?: (record: any) => void;
}

interface HistoryRecord {
  id: string;
  cluster: string;
  nodepool_name: string;
  health_score: number;
  risk_level: string;
  provider: string;
  model: string;
  duration_ms: number;
  user_email: string;
  analyzed_at: string;
  created_at: string;
  full_result?: string;
}

function riskColor(level: string) {
  const m: Record<string, string> = {
    low: "bg-green-500/20 text-green-500 border-green-500/30",
    medium: "bg-yellow-500/20 text-yellow-500 border-yellow-500/30",
    high: "bg-orange-500/20 text-orange-500 border-orange-500/30",
    critical: "bg-red-500/20 text-red-500 border-red-500/30",
  };
  return m[level] ?? m.medium;
}

function healthColor(score: number) {
  if (score >= 75) return "text-green-500";
  if (score >= 50) return "text-yellow-500";
  return "text-red-500";
}

export function NodePoolPredictionHistoryModal({
  open,
  onOpenChange,
  cluster,
  nodepool,
  onSelectRecord,
}: NodePoolPredictionHistoryModalProps) {
  const [records, setRecords] = useState<HistoryRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const limit = 20;

  const loadRecords = async (pageNum = 0) => {
    setLoading(true);
    try {
      const res = await apiClient.getNodePoolPredictionHistory({
        cluster,
        nodepool,
        limit,
        offset: pageNum * limit,
      });
      setRecords(res.records ?? []);
      setTotal(res.total ?? 0);
      setPage(pageNum);
    } catch (err) {
      toast.error("Erro ao carregar histórico", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) loadRecords(0);
  }, [open, cluster, nodepool]);

  const handleSelectRecord = (record: HistoryRecord) => {
    if (!onSelectRecord) return;
    // full_result é o JSON completo do NodePoolPredictionResult retornado pelo backend
    if (record.full_result) {
      try {
        const parsed = JSON.parse(record.full_result);
        onSelectRecord(parsed);
        onOpenChange(false);
        return;
      } catch {
        // fallback: passa o record metadata
      }
    }
    onSelectRecord(record);
    onOpenChange(false);
  };

  const totalPages = Math.ceil(total / limit);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-w-3xl h-[80vh] flex flex-col p-0"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b">
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <Clock className="w-5 h-5 text-primary" />
                Histórico de Análises — {nodepool ?? "todos os node pools"}
              </DialogTitle>
              <DialogDescription>
                {cluster ? `Cluster: ${cluster}` : "Todos os clusters"}
                {total > 0 && ` · ${total} análise${total !== 1 ? "s" : ""}`}
              </DialogDescription>
            </div>
            <Button variant="outline" size="sm" onClick={() => loadRecords(0)} disabled={loading}>
              {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : "Atualizar"}
            </Button>
          </div>
        </DialogHeader>

        <ScrollArea className="flex-1 px-6">
          <div className="py-4 space-y-2">
            {loading && records.length === 0 && (
              <div className="flex items-center justify-center py-16">
                <Loader2 className="w-6 h-6 animate-spin text-primary" />
                <span className="ml-3 text-muted-foreground text-sm">Carregando histórico...</span>
              </div>
            )}

            {!loading && records.length === 0 && (
              <div className="text-center py-16 text-muted-foreground">
                <TrendingUp className="w-10 h-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">Nenhuma análise encontrada.</p>
                <p className="text-xs mt-1">Execute uma análise preditiva para registrá-la aqui.</p>
              </div>
            )}

            {records.map((rec) => (
              <div
                key={rec.id}
                className="bg-gradient-card border border-border/50 rounded-lg p-4 hover:border-primary/40 transition-colors"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <span className="font-semibold text-sm truncate">{rec.nodepool_name}</span>
                      <Badge variant="outline" className="text-xs">{rec.cluster}</Badge>
                      <Badge className={`text-xs ${riskColor(rec.risk_level)}`}>{rec.risk_level}</Badge>
                    </div>
                    <div className="flex items-center gap-4 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <Clock className="w-3 h-3" />
                        {new Date(rec.analyzed_at).toLocaleString("pt-BR")}
                      </span>
                      {rec.provider && (
                        <span className="flex items-center gap-1">
                          <TrendingUp className="w-3 h-3" />
                          {rec.provider}{rec.model ? ` · ${rec.model}` : ""}
                        </span>
                      )}
                      {rec.user_email && (
                        <span className="truncate max-w-[160px]">{rec.user_email}</span>
                      )}
                      <span>{(rec.duration_ms / 1000).toFixed(1)}s</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 flex-shrink-0">
                    <div className="text-center">
                      <div className={`text-2xl font-bold ${healthColor(rec.health_score)}`}>
                        {rec.health_score}
                      </div>
                      <div className="text-xs text-muted-foreground">score</div>
                    </div>
                    {onSelectRecord && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleSelectRecord(rec)}
                        className="text-primary hover:text-primary"
                      >
                        <ChevronRight className="w-4 h-4" />
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </ScrollArea>

        {/* Paginação */}
        {totalPages > 1 && (
          <div className="flex-shrink-0 flex items-center justify-between px-6 py-3 border-t text-sm">
            <span className="text-muted-foreground">
              Página {page + 1} de {totalPages}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page === 0 || loading}
                onClick={() => loadRecords(page - 1)}
              >
                Anterior
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={page >= totalPages - 1 || loading}
                onClick={() => loadRecords(page + 1)}
              >
                Próxima
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export default NodePoolPredictionHistoryModal;
