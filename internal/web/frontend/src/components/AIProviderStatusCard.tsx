import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Brain, RefreshCw, AlertCircle, Settings } from "lucide-react";
import type { ProviderStatus } from "@/types/ai";
import { useNavigate } from "react-router-dom";

interface AIProviderStatusCardProps {
  providerStatus: ProviderStatus | null;
  isLoading: boolean;
  onRefresh: () => void;
}

export function AIProviderStatusCard({
  providerStatus,
  isLoading,
  onRefresh,
}: AIProviderStatusCardProps) {
  const navigate = useNavigate();
  // Format relative time
  const getRelativeTime = (timestamp?: string) => {
    if (!timestamp) return "Nunca";
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffSecs = Math.floor(diffMs / 1000);

    if (diffSecs < 30) return "Agora";
    if (diffMins < 1) return `${diffSecs}s atrás`;
    if (diffMins < 60) return `${diffMins}min atrás`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours}h atrás`;
    const diffDays = Math.floor(diffHours / 24);
    return `${diffDays}d atrás`;
  };

  const isAvailable = providerStatus?.available ?? false;
  const provider = providerStatus?.provider || "Desconhecido";
  const model = providerStatus?.model || "N/A";
  const error = providerStatus?.error;
  const lastCheck = providerStatus?.lastCheck;
  const totalCalls = providerStatus?.total_calls || 0;
  const successfulCalls = providerStatus?.successful_calls || 0;
  const failedCalls = providerStatus?.failed_calls || 0;
  
  // Detectar se é erro de quota
  const isQuotaError = error && (
    error.toLowerCase().includes("quota") || 
    error.toLowerCase().includes("429") ||
    error.toLowerCase().includes("limit reached")
  );

  return (
    <Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60">
      <CardContent className="p-6">
        <div className="flex items-center gap-4">
          {/* Ícone com gradiente dinâmico */}
          <div
            className={`p-4 rounded-xl shadow-lg ${
              isAvailable
                ? "bg-gradient-to-r from-green-500 to-emerald-600"
                : "bg-gradient-to-r from-red-500 to-rose-600"
            }`}
          >
            <Brain className="w-8 h-8 text-white" />
          </div>

          {/* Info */}
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1">
              <h3 className="text-lg font-semibold">AI Provider Status</h3>
              <Badge variant={isAvailable ? "default" : "destructive"}>
                {isAvailable ? "Online" : "Offline"}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground">
              {provider} - {model}
            </p>
            {lastCheck && (
              <p className="text-xs text-muted-foreground mt-1">
                Última verificação: {getRelativeTime(lastCheck)}
              </p>
            )}
            
            {/* Contador de chamadas à AI */}
            {totalCalls > 0 && (
              <div className="flex items-center gap-3 mt-2 text-xs">
                <div className="flex items-center gap-1">
                  <span className="font-medium text-slate-700 dark:text-slate-300">Total:</span>
                  <span className="font-semibold text-blue-600 dark:text-blue-400">{totalCalls}</span>
                </div>
                <div className="flex items-center gap-1">
                  <span className="font-medium text-slate-700 dark:text-slate-300">Sucesso:</span>
                  <span className="font-semibold text-green-600 dark:text-green-400">{successfulCalls}</span>
                </div>
                {failedCalls > 0 && (
                  <div className="flex items-center gap-1">
                    <span className="font-medium text-slate-700 dark:text-slate-300">Falhas:</span>
                    <span className="font-semibold text-red-600 dark:text-red-400">{failedCalls}</span>
                  </div>
                )}
              </div>
            )}
            
            {error && (
              <div className="flex items-start gap-2 mt-2 p-3 rounded-md bg-destructive/10 border border-destructive/20">
                <AlertCircle className="h-4 w-4 text-destructive mt-0.5 flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-xs text-destructive font-medium break-words">{error}</p>
                  {isQuotaError && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="mt-2 h-7 text-xs"
                      onClick={() => navigate('/settings?tab=ai')}
                    >
                      <Settings className="h-3 w-3 mr-1" />
                      Trocar Provider
                    </Button>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* Botão refresh */}
          <Button
            variant="outline"
            size="icon"
            onClick={onRefresh}
            disabled={isLoading}
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
