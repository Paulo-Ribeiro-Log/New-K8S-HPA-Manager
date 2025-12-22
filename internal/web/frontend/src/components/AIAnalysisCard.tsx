import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import type { AnalysisResult, Suggestion } from "@/types/ai";
import { Copy, Check, AlertTriangle, Info, Zap, Wrench } from "lucide-react";
import { useState } from "react";
import { useToast } from "@/components/ui/use-toast";
import ReactMarkdown from "react-markdown";

interface AIAnalysisCardProps {
  analysis: AnalysisResult;
  onClose?: () => void;
}

const priorityColors = {
  critical: "bg-red-500 hover:bg-red-600",
  high: "bg-orange-500 hover:bg-orange-600",
  medium: "bg-yellow-500 hover:bg-yellow-600",
  low: "bg-blue-500 hover:bg-blue-600",
};

const priorityIcons = {
  critical: AlertTriangle,
  high: AlertTriangle,
  medium: Info,
  low: Info,
};

const suggestionTypeIcons = {
  investigate: Info,
  fix: Wrench,
  optimize: Zap,
  scale: Zap,
};

const SuggestionCard = ({ suggestion }: { suggestion: Suggestion }) => {
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);
  const Icon = suggestionTypeIcons[suggestion.type];
  const PriorityIcon = priorityIcons[suggestion.priority];

  const handleCopy = async () => {
    if (!suggestion.command) return;

    try {
      await navigator.clipboard.writeText(suggestion.command);
      setCopied(true);
      toast({
        title: "✅ Comando copiado",
        description: "O comando foi copiado para a área de transferência",
      });
      setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      toast({
        title: "❌ Erro ao copiar",
        description: "Não foi possível copiar o comando",
        variant: "destructive",
      });
    }
  };

  return (
    <div className="border rounded-lg p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2 flex-1">
          <Icon className="h-5 w-5 mt-0.5 text-blue-500 flex-shrink-0" />
          <div className="space-y-1 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <Badge variant="outline" className="capitalize">
                {suggestion.type}
              </Badge>
              <Badge className={`${priorityColors[suggestion.priority]} text-white`}>
                <PriorityIcon className="h-3 w-3 mr-1" />
                {suggestion.priority}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground">{suggestion.description}</p>
            {suggestion.command && (
              <div className="mt-2">
                <div className="flex items-start gap-2">
                  <code className="flex-1 bg-muted p-2 rounded text-xs font-mono break-all">
                    $ {suggestion.command}
                  </code>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleCopy}
                    className="flex-shrink-0"
                  >
                    {copied ? (
                      <Check className="h-4 w-4" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export function AIAnalysisCard({ analysis, onClose }: AIAnalysisCardProps) {
  const formattedDate = new Date(analysis.analyzedAt).toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });

  return (
    <Card className="w-full">
      <CardHeader>
        <div className="flex items-start justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2">
              🤖 AI Diagnostics
              <Badge variant="outline">{analysis.provider}</Badge>
            </CardTitle>
            <CardDescription>
              {analysis.resourceType}: {analysis.cluster}/{analysis.namespace}/{analysis.resourceName}
            </CardDescription>
          </div>
          {onClose && (
            <Button variant="ghost" size="sm" onClick={onClose}>
              ✕
            </Button>
          )}
        </div>
        <div className="flex items-center gap-4 text-xs text-muted-foreground mt-2">
          <span>📅 {formattedDate}</span>
          {analysis.responseTime && (
            <span>⚡ {analysis.responseTime.toFixed(2)}s</span>
          )}
          {analysis.tokensUsed && (
            <span>🎯 {analysis.tokensUsed} tokens</span>
          )}
          {analysis.model && (
            <span>🧠 {analysis.model}</span>
          )}
        </div>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* Análise principal */}
        <div>
          <h3 className="text-sm font-semibold mb-2">📊 Análise</h3>
          <ScrollArea className="h-[300px] w-full rounded border p-4 bg-muted/50">
            <div className="prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown>{analysis.analysis}</ReactMarkdown>
            </div>
          </ScrollArea>
        </div>

        {/* Sugestões */}
        {analysis.suggestions && analysis.suggestions.length > 0 && (
          <>
            <Separator />
            <div>
              <h3 className="text-sm font-semibold mb-3">💡 Sugestões de Ação</h3>
              <div className="space-y-3">
                {analysis.suggestions.map((suggestion, index) => (
                  <SuggestionCard key={index} suggestion={suggestion} />
                ))}
              </div>
            </div>
          </>
        )}

        {/* Metadata adicional */}
        {analysis.userEmail && (
          <>
            <Separator />
            <div className="text-xs text-muted-foreground">
              Analisado por: {analysis.userEmail}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
