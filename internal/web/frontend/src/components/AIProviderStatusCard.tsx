import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Brain, RefreshCw, AlertCircle } from "lucide-react";
import type { ProviderStatus } from "@/types/ai";

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
            {error && !isAvailable && (
              <div className="flex items-start gap-2 mt-2 p-2 rounded-md bg-destructive/10 border border-destructive/20">
                <AlertCircle className="h-4 w-4 text-destructive mt-0.5 flex-shrink-0" />
                <p className="text-xs text-destructive">{error}</p>
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
