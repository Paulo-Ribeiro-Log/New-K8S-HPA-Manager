import { useState, useEffect } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useToast } from "@/components/ui/use-toast";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Key, Eye, EyeOff, CheckCircle2, XCircle, Loader2, Shield, Trash2 } from "lucide-react";
import { apiClient } from "@/lib/api/client";

interface TokenStatus {
  has_gemini: boolean;
  has_openai: boolean;
  has_claude: boolean;
  preferred_provider: string;
  updated_at?: string;
}

export function AISettingsTab() {
  const { toast } = useToast();
  const [tokenStatus, setTokenStatus] = useState<TokenStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState<string | null>(null);

  // Form state
  const [geminiKey, setGeminiKey] = useState("");
  const [openaiKey, setOpenaiKey] = useState("");
  const [claudeKey, setClaudeKey] = useState("");
  const [preferredProvider, setPreferredProvider] = useState("gemini");

  // Visibility state
  const [showGemini, setShowGemini] = useState(false);
  const [showOpenAI, setShowOpenAI] = useState(false);
  const [showClaude, setShowClaude] = useState(false);

  // Validation results
  const [geminiValid, setGeminiValid] = useState<boolean | null>(null);
  const [openaiValid, setOpenaiValid] = useState<boolean | null>(null);
  const [claudeValid, setClaudeValid] = useState<boolean | null>(null);

  useEffect(() => {
    loadTokenStatus();
  }, []);

  const loadTokenStatus = async () => {
    setLoading(true);
    try {
      const response = await apiClient.getAITokens();
      setTokenStatus(response);
      setPreferredProvider(response.preferred_provider || "gemini");
    } catch (error) {
      console.error("Failed to load token status:", error);
      toast({
        title: "❌ Erro ao carregar configurações",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
    } finally {
      setLoading(false);
    }
  };

  const validateToken = async (provider: string, apiKey: string) => {
    if (!apiKey) return;

    setValidating(provider);

    try {
      const response = await apiClient.validateAIToken(provider, apiKey);

      if (provider === "gemini") setGeminiValid(response.valid);
      if (provider === "openai") setOpenaiValid(response.valid);
      if (provider === "claude") setClaudeValid(response.valid);

      if (response.valid) {
        toast({
          title: "✅ Token válido",
          description: `${provider} API key validada com sucesso`,
        });
      } else {
        toast({
          title: "❌ Token inválido",
          description: response.error || "Token não é válido",
          variant: "destructive",
        });
      }
    } catch (error) {
      if (provider === "gemini") setGeminiValid(false);
      if (provider === "openai") setOpenaiValid(false);
      if (provider === "claude") setClaudeValid(false);

      toast({
        title: "❌ Erro na validação",
        description: error instanceof Error ? error.message : "Erro ao validar token",
        variant: "destructive",
      });
    } finally {
      setValidating(null);
    }
  };

  const handleSave = async () => {
    // Validar que pelo menos um token foi fornecido
    if (!geminiKey && !openaiKey && !claudeKey) {
      toast({
        title: "⚠️ Atenção",
        description: "Você precisa configurar pelo menos um token AI",
        variant: "destructive",
      });
      return;
    }

    setSaving(true);

    try {
      await apiClient.saveAITokens({
        gemini_api_key: geminiKey,
        openai_api_key: openaiKey,
        claude_api_key: claudeKey,
        preferred_provider: preferredProvider,
      });

      toast({
        title: "✅ Configurações salvas",
        description: "Seus tokens AI foram salvos com sucesso",
      });

      // Limpar campos de senha após salvar
      setGeminiKey("");
      setOpenaiKey("");
      setClaudeKey("");

      // Recarregar status
      await loadTokenStatus();
    } catch (error) {
      toast({
        title: "❌ Erro ao salvar",
        description: error instanceof Error ? error.message : "Erro ao salvar tokens",
        variant: "destructive",
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!window.confirm("Tem certeza que deseja remover todas as suas configurações AI?")) {
      return;
    }

    try {
      await apiClient.deleteAITokens();

      toast({
        title: "✅ Tokens removidos",
        description: "Suas configurações AI foram removidas",
      });

      // Limpar form
      setGeminiKey("");
      setOpenaiKey("");
      setClaudeKey("");
      setGeminiValid(null);
      setOpenaiValid(null);
      setClaudeValid(null);

      await loadTokenStatus();
    } catch (error) {
      toast({
        title: "❌ Erro ao remover",
        description: error instanceof Error ? error.message : "Erro ao remover tokens",
        variant: "destructive",
      });
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <Loader2 className="h-8 w-8 animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-6 w-6" />
            Configurações de AI Providers
          </CardTitle>
          <CardDescription>
            Configure suas próprias API keys para usar AI Diagnostics. Seus tokens são armazenados de forma segura e
            privada.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Status Atual */}
          {tokenStatus && (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Status Atual</Label>
              <div className="flex gap-2 flex-wrap">
                <Badge variant={tokenStatus.has_gemini ? "default" : "outline"}>
                  {tokenStatus.has_gemini ? <CheckCircle2 className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                  Gemini {tokenStatus.has_gemini ? "Configurado" : "Não Configurado"}
                </Badge>
                <Badge variant={tokenStatus.has_openai ? "default" : "outline"}>
                  {tokenStatus.has_openai ? <CheckCircle2 className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                  OpenAI {tokenStatus.has_openai ? "Configurado" : "Não Configurado"}
                </Badge>
                <Badge variant={tokenStatus.has_claude ? "default" : "outline"}>
                  {tokenStatus.has_claude ? <CheckCircle2 className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                  Claude {tokenStatus.has_claude ? "Configurado" : "Não Configurado"}
                </Badge>
              </div>
              {tokenStatus.updated_at && (
                <p className="text-xs text-muted-foreground mt-1">
                  Última atualização: {new Date(tokenStatus.updated_at).toLocaleString("pt-BR")}
                </p>
              )}
            </div>
          )}

          <Separator />

          {/* Gemini API Key */}
          <div className="space-y-2">
            <Label htmlFor="gemini-key" className="flex items-center gap-2">
              <Key className="h-4 w-4" />
              Gemini API Key
              {geminiValid === true && <CheckCircle2 className="h-4 w-4 text-green-500" />}
              {geminiValid === false && <XCircle className="h-4 w-4 text-red-500" />}
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="gemini-key"
                  type={showGemini ? "text" : "password"}
                  placeholder="AIza..."
                  value={geminiKey}
                  onChange={(e) => {
                    setGeminiKey(e.target.value);
                    setGeminiValid(null);
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="absolute right-0 top-0 h-full px-3"
                  onClick={() => setShowGemini(!showGemini)}
                >
                  {showGemini ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => validateToken("gemini", geminiKey)}
                disabled={!geminiKey || validating === "gemini"}
              >
                {validating === "gemini" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  "Validar"
                )}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Obtenha sua chave em:{" "}
              <a
                href="https://aistudio.google.com/app/apikey"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:underline"
              >
                https://aistudio.google.com/app/apikey
              </a>
            </p>
          </div>

          {/* OpenAI API Key */}
          <div className="space-y-2">
            <Label htmlFor="openai-key" className="flex items-center gap-2">
              <Key className="h-4 w-4" />
              OpenAI API Key (Em breve)
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="openai-key"
                  type={showOpenAI ? "text" : "password"}
                  placeholder="sk-..."
                  value={openaiKey}
                  onChange={(e) => setOpenaiKey(e.target.value)}
                  disabled
                />
              </div>
              <Button size="sm" variant="outline" disabled>
                Validar
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">OpenAI integration coming soon</p>
          </div>

          {/* Claude API Key */}
          <div className="space-y-2">
            <Label htmlFor="claude-key" className="flex items-center gap-2">
              <Key className="h-4 w-4" />
              Claude API Key (Em breve)
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="claude-key"
                  type={showClaude ? "text" : "password"}
                  placeholder="sk-ant-..."
                  value={claudeKey}
                  onChange={(e) => setClaudeKey(e.target.value)}
                  disabled
                />
              </div>
              <Button size="sm" variant="outline" disabled>
                Validar
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">Claude integration coming soon</p>
          </div>

          <Separator />

          {/* Provider Preferido */}
          <div className="space-y-2">
            <Label htmlFor="preferred-provider">Provider Preferido</Label>
            <Select value={preferredProvider} onValueChange={setPreferredProvider}>
              <SelectTrigger>
                <SelectValue placeholder="Selecione o provider padrão" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="gemini">Gemini (Recomendado)</SelectItem>
                <SelectItem value="openai" disabled>
                  OpenAI (Em breve)
                </SelectItem>
                <SelectItem value="claude" disabled>
                  Claude (Em breve)
                </SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Provider que será usado por padrão para análises AI
            </p>
          </div>

          {/* Botões de Ação */}
          <div className="flex gap-2 pt-4">
            <Button onClick={handleSave} disabled={saving}>
              {saving ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Salvando...
                </>
              ) : (
                "Salvar Configurações"
              )}
            </Button>

            {(tokenStatus?.has_gemini || tokenStatus?.has_openai || tokenStatus?.has_claude) && (
              <Button variant="destructive" onClick={handleDelete}>
                <Trash2 className="h-4 w-4 mr-2" />
                Remover Todos os Tokens
              </Button>
            )}
          </div>

          {/* Aviso de Segurança */}
          <div className="bg-muted p-4 rounded-lg space-y-2">
            <p className="text-sm font-medium flex items-center gap-2">
              <Shield className="h-4 w-4" />
              Segurança e Privacidade
            </p>
            <ul className="text-xs text-muted-foreground space-y-1 ml-6 list-disc">
              <li>Seus tokens são armazenados de forma segura e criptografada</li>
              <li>Cada usuário tem seus próprios tokens (não são compartilhados)</li>
              <li>Tokens nunca são exibidos ou transmitidos para outros usuários</li>
              <li>Você pode remover seus tokens a qualquer momento</li>
            </ul>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
