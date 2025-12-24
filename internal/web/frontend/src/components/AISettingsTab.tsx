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
  has_copilot: boolean;
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
  const [copilotKey, setCopilotKey] = useState("");
  const [copilotEndpoint, setCopilotEndpoint] = useState("");
  const [copilotDeployment, setCopilotDeployment] = useState("");
  const [preferredProvider, setPreferredProvider] = useState("ollama");

  // Visibility state
  const [showGemini, setShowGemini] = useState(false);
  const [showOpenAI, setShowOpenAI] = useState(false);
  const [showClaude, setShowClaude] = useState(false);
  const [showCopilot, setShowCopilot] = useState(false);

  // Validation results
  const [geminiValid, setGeminiValid] = useState<boolean | null>(null);
  const [openaiValid, setOpenaiValid] = useState<boolean | null>(null);
  const [claudeValid, setClaudeValid] = useState<boolean | null>(null);
  const [copilotValid, setCopilotValid] = useState<boolean | null>(null);

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

  const validateToken = async (provider: string, apiKey: string, endpoint?: string, deployment?: string) => {
    if (!apiKey) return;

    // Para Copilot, endpoint é obrigatório
    if (provider === "copilot" && !endpoint) {
      toast({
        title: "⚠️ Atenção",
        description: "Endpoint do Azure OpenAI é obrigatório para Copilot",
        variant: "destructive",
      });
      return;
    }

    setValidating(provider);

    try {
      const response = await apiClient.validateAIToken(provider, apiKey, endpoint, deployment);

      if (provider === "gemini") setGeminiValid(response.valid);
      if (provider === "openai") setOpenaiValid(response.valid);
      if (provider === "claude") setClaudeValid(response.valid);
      if (provider === "copilot") setCopilotValid(response.valid);

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
      if (provider === "copilot") setCopilotValid(false);

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
    // Validar que pelo menos um token foi fornecido (permitir Ollama sem tokens)
    // if (!geminiKey && !openaiKey && !claudeKey && !copilotKey) {
    //   toast({
    //     title: "⚠️ Atenção",
    //     description: "Você precisa configurar pelo menos um token AI (ou usar Ollama local)",
    //     variant: "destructive",
    //   });
    //   return;
    // }

    setSaving(true);

    try {
      await apiClient.saveAITokens({
        gemini_api_key: geminiKey,
        openai_api_key: openaiKey,
        claude_api_key: claudeKey,
        copilot_api_key: copilotKey,
        copilot_endpoint: copilotEndpoint,
        copilot_deployment: copilotDeployment,
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
      setCopilotKey("");
      setCopilotEndpoint("");
      setCopilotDeployment("");

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
      setCopilotKey("");
      setCopilotEndpoint("");
      setCopilotDeployment("");
      setGeminiValid(null);
      setOpenaiValid(null);
      setClaudeValid(null);
      setCopilotValid(null);

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
                <Badge variant={tokenStatus.has_copilot ? "default" : "outline"}>
                  {tokenStatus.has_copilot ? <CheckCircle2 className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                  Copilot {tokenStatus.has_copilot ? "Configurado" : "Não Configurado"}
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
              OpenAI API Key
              {openaiValid === true && <CheckCircle2 className="h-4 w-4 text-green-500" />}
              {openaiValid === false && <XCircle className="h-4 w-4 text-red-500" />}
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="openai-key"
                  type={showOpenAI ? "text" : "password"}
                  placeholder="sk-..."
                  value={openaiKey}
                  onChange={(e) => {
                    setOpenaiKey(e.target.value);
                    setOpenaiValid(null);
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="absolute right-0 top-0 h-full px-3"
                  onClick={() => setShowOpenAI(!showOpenAI)}
                >
                  {showOpenAI ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => validateToken("openai", openaiKey)}
                disabled={!openaiKey || validating === "openai"}
              >
                {validating === "openai" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  "Validar"
                )}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Obtenha sua chave em:{" "}
              <a
                href="https://platform.openai.com/api-keys"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:underline"
              >
                https://platform.openai.com/api-keys
              </a>
            </p>
          </div>

          {/* Claude API Key */}
          <div className="space-y-2">
            <Label htmlFor="claude-key" className="flex items-center gap-2">
              <Key className="h-4 w-4" />
              Claude API Key
              {claudeValid === true && <CheckCircle2 className="h-4 w-4 text-green-500" />}
              {claudeValid === false && <XCircle className="h-4 w-4 text-red-500" />}
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="claude-key"
                  type={showClaude ? "text" : "password"}
                  placeholder="sk-ant-api03-..."
                  value={claudeKey}
                  onChange={(e) => {
                    setClaudeKey(e.target.value);
                    setClaudeValid(null);
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="absolute right-0 top-0 h-full px-3"
                  onClick={() => setShowClaude(!showClaude)}
                >
                  {showClaude ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => validateToken("claude", claudeKey)}
                disabled={!claudeKey || validating === "claude"}
              >
                {validating === "claude" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  "Validar"
                )}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Obtenha sua chave em:{" "}
              <a
                href="https://console.anthropic.com/settings/keys"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:underline"
              >
                https://console.anthropic.com/settings/keys
              </a>
            </p>
          </div>

          {/* Microsoft Copilot API Key (Azure OpenAI) */}
          <div className="space-y-2">
            <Label htmlFor="copilot-key" className="flex items-center gap-2">
              <Key className="h-4 w-4" />
              Microsoft Copilot API Key (Azure OpenAI)
              {copilotValid === true && <CheckCircle2 className="h-4 w-4 text-green-500" />}
              {copilotValid === false && <XCircle className="h-4 w-4 text-red-500" />}
            </Label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  id="copilot-key"
                  type={showCopilot ? "text" : "password"}
                  placeholder="Azure OpenAI API Key..."
                  value={copilotKey}
                  onChange={(e) => {
                    setCopilotKey(e.target.value);
                    setCopilotValid(null);
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="absolute right-0 top-0 h-full px-3"
                  onClick={() => setShowCopilot(!showCopilot)}
                >
                  {showCopilot ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => validateToken("copilot", copilotKey, copilotEndpoint, copilotDeployment)}
                disabled={!copilotKey || !copilotEndpoint || validating === "copilot"}
              >
                {validating === "copilot" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  "Validar"
                )}
              </Button>
            </div>

            {/* Copilot Endpoint */}
            <div className="space-y-1 pt-2">
              <Label htmlFor="copilot-endpoint" className="text-xs">Endpoint (Azure OpenAI Resource)</Label>
              <Input
                id="copilot-endpoint"
                type="text"
                placeholder="https://your-resource.openai.azure.com"
                value={copilotEndpoint}
                onChange={(e) => {
                  setCopilotEndpoint(e.target.value);
                  setCopilotValid(null);
                }}
                className="text-sm"
              />
            </div>

            {/* Copilot Deployment */}
            <div className="space-y-1">
              <Label htmlFor="copilot-deployment" className="text-xs">Deployment Name</Label>
              <Input
                id="copilot-deployment"
                type="text"
                placeholder="gpt-4o (opcional, padrão: gpt-4o)"
                value={copilotDeployment}
                onChange={(e) => {
                  setCopilotDeployment(e.target.value);
                  setCopilotValid(null);
                }}
                className="text-sm"
              />
            </div>

            <p className="text-xs text-muted-foreground">
              Configure seu Azure OpenAI resource em:{" "}
              <a
                href="https://portal.azure.com/#view/Microsoft_Azure_ProjectOxford/CognitiveServicesHub/~/OpenAI"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:underline"
              >
                Azure Portal - OpenAI
              </a>
            </p>
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
                <SelectItem value="ollama">Ollama (Local - Grátis - Recomendado)</SelectItem>
                <SelectItem value="gemini">Gemini (Grátis)</SelectItem>
                <SelectItem value="openai">OpenAI GPT-4o-mini (Pago - Barato)</SelectItem>
                <SelectItem value="claude">Claude (Pago - Alta Qualidade)</SelectItem>
                <SelectItem value="copilot">Microsoft Copilot (Azure OpenAI - Pago)</SelectItem>
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

            {(tokenStatus?.has_gemini || tokenStatus?.has_openai || tokenStatus?.has_claude || tokenStatus?.has_copilot) && (
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
