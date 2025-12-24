import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Lightbulb, CheckCircle2, Settings } from "lucide-react";

export function AIQuickStartGuide() {
  return (
    <Card className="bg-gradient-to-br from-blue-50 to-indigo-50 dark:from-slate-800 dark:to-slate-900 border-blue-200 dark:border-blue-800">
      <CardContent className="p-6">
        <div className="flex items-start gap-4">
          <div className="p-3 bg-blue-500 rounded-xl shadow-lg">
            <Lightbulb className="w-6 h-6 text-white" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-semibold mb-2">Como usar AI Diagnostics</h3>
            <div className="space-y-3 text-sm">
              <div className="flex items-start gap-2">
                <CheckCircle2 className="w-4 h-4 text-green-500 mt-0.5 flex-shrink-0" />
                <p>
                  Vá para a aba <strong>Pods</strong>, selecione um pod e clique em{" "}
                  <Badge variant="outline" className="mx-1">
                    Analisar com AI
                  </Badge>
                </p>
              </div>
              <div className="flex items-start gap-2">
                <CheckCircle2 className="w-4 h-4 text-green-500 mt-0.5 flex-shrink-0" />
                <p>
                  Recursos suportados:{" "}
                  <Badge variant="secondary" className="mr-1">
                    Pod
                  </Badge>
                  <Badge variant="secondary" className="mr-1">
                    Deployment
                  </Badge>
                  <Badge variant="secondary" className="mr-1">
                    HPA
                  </Badge>
                  <Badge variant="secondary">Node</Badge>
                </p>
              </div>
              <div className="flex items-start gap-2">
                <Settings className="w-4 h-4 text-blue-500 mt-0.5 flex-shrink-0" />
                <p>
                  Configure suas API keys na aba <strong>Configurações</strong> para usar
                  providers externos (Gemini, OpenAI, Claude, Copilot)
                </p>
              </div>
              <div className="flex items-start gap-2">
                <CheckCircle2 className="w-4 h-4 text-green-500 mt-0.5 flex-shrink-0" />
                <p>
                  Se nenhum provider externo for configurado, o sistema usará{" "}
                  <strong>Ollama</strong> localmente (se disponível)
                </p>
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
