import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Brain } from "lucide-react";

export function AIDiagnosticsTab() {
  return (
    <div className="space-y-6 p-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Brain className="h-6 w-6" />
            AI-Powered Diagnostics
          </CardTitle>
          <CardDescription>
            Sistema de diagnósticos AI em desenvolvimento
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Esta funcionalidade está sendo implementada. Em breve você poderá analisar
              recursos Kubernetes usando IA (Gemini ou Ollama).
            </p>

            <div className="rounded-lg border p-4 bg-muted/50">
              <h3 className="text-sm font-semibold mb-2">🚀 Recursos Planejados:</h3>
              <ul className="text-sm text-muted-foreground space-y-1 list-disc list-inside">
                <li>Análise de Pods com CrashLoopBackOff</li>
                <li>Diagnóstico de HPAs maxed out</li>
                <li>Análise de Nodes com problemas</li>
                <li>Sugestões de ações corretivas</li>
                <li>Histórico de análises</li>
              </ul>
            </div>

            <div className="text-xs text-muted-foreground">
              <strong>Status:</strong> Backend 100% completo | Frontend em desenvolvimento
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
