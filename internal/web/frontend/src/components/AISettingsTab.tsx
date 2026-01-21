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
  gemini_model?: string;
  has_openai: boolean;
  openai_model?: string;
  has_claude: boolean;
  claude_model?: string;
  has_copilot: boolean;
  copilot_endpoint?: string;
  copilot_deployment?: string;
  ollama_model?: string;
  preferred_provider: string;
  updated_at?: string;
}

interface ModelInfo {
  id: string;
  name: string;
  description?: string;
  is_default: boolean;
}

interface ModelsResponse {
  provider: string;
  models: ModelInfo[];
}

export function AISettingsTab() {
  const { toast } = useToast();
  const [tokenStatus, setTokenStatus] = useState<TokenStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState<string | null>(null);

  // Form state
  const [aiEmail, setAiEmail] = useState(""); // Email para identificar configurações AI (independente do Azure AD)
  const [geminiKey, setGeminiKey] = useState("");
  const [geminiModel, setGeminiModel] = useState("");
  const [openaiKey, setOpenaiKey] = useState("");
  const [openaiModel, setOpenaiModel] = useState("");
  const [claudeKey, setClaudeKey] = useState("");
  const [claudeModel, setClaudeModel] = useState("");
  const [copilotKey, setCopilotKey] = useState("");
  const [copilotEndpoint, setCopilotEndpoint] = useState("");
  const [copilotDeployment, setCopilotDeployment] = useState("");
  const [ollamaModel, setOllamaModel] = useState("");
  const [preferredProvider, setPreferredProvider] = useState("ollama");

  // Available models per provider
  const [geminiModels, setGeminiModels] = useState<ModelInfo[]>([]);
  const [claudeModels, setClaudeModels] = useState<ModelInfo[]>([]);
  const [openaiModels, setOpenaiModels] = useState<ModelInfo[]>([]);
  const [ollamaModels, setOllamaModels] = useState<ModelInfo[]>([]);
  const [copilotModels, setCopilotModels] = useState<ModelInfo[]>([]);

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
    // Tentar carregar ai_email do localStorage
    const savedEmail = localStorage.getItem("ai_email");
    if (savedEmail) {
      setAiEmail(savedEmail);
    }

    loadTokenStatus();
    loadAvailableModels();
  }, []);

  const loadTokenStatus = async () => {
    setLoading(true);
    try {
      const response = await apiClient.getAITokens();
      console.log("[AISettingsTab] Configurações carregadas:", {
        ai_email: response.ai_email || "(não configurado)",
        preferred_provider: response.preferred_provider,
        has_gemini: response.has_gemini,
        has_claude: response.has_claude,
        has_openai: response.has_openai,
      });

      setTokenStatus(response);
      setPreferredProvider(response.preferred_provider || "ollama");

      // Carregar email AI (se existir) e sincronizar com localStorage
      if (response.ai_email) {
        setAiEmail(response.ai_email);
        // Sincronizar localStorage com backend (fonte de verdade)
        const currentLocalEmail = localStorage.getItem("ai_email");
        if (currentLocalEmail !== response.ai_email) {
          localStorage.setItem("ai_email", response.ai_email);
          console.log("[AISettingsTab] localStorage sincronizado com backend:", response.ai_email);
        }
      }

      // Carregar modelos salvos
      if (response.gemini_model) setGeminiModel(response.gemini_model);
      if (response.claude_model) setClaudeModel(response.claude_model);
      if (response.openai_model) setOpenaiModel(response.openai_model);
      if (response.ollama_model) setOllamaModel(response.ollama_model);
      if (response.copilot_endpoint) setCopilotEndpoint(response.copilot_endpoint);
      if (response.copilot_deployment) setCopilotDeployment(response.copilot_deployment);
    } catch (error) {
      console.error("Failed to load token status:", error);
      toast({
        title: "Erro ao carregar configurações",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
    } finally {
      setLoading(false);
    }
  };

  const loadAvailableModels = async () => {
    try {
      // Carregar modelos de todos os providers em paralelo
      const [gemini, claude, openai, ollama, copilot] = await Promise.all([
        apiClient.getAvailableModels("gemini"),
        apiClient.getAvailableModels("claude"),
        apiClient.getAvailableModels("openai"),
        apiClient.getAvailableModels("ollama"),
        apiClient.getAvailableModels("copilot"),
      ]);

      setGeminiModels(gemini.models);
      setClaudeModels(claude.models);
      setOpenaiModels(openai.models);
      setOllamaModels(ollama.models);
      setCopilotModels(copilot.models);

      // Definir modelos padrão se não houver selecionado
      if (!geminiModel && gemini.models.length > 0) {
        const defaultModel = gemini.models.find(m => m.is_default);
        if (defaultModel) setGeminiModel(defaultModel.id);
      }
      if (!claudeModel && claude.models.length > 0) {
        const defaultModel = claude.models.find(m => m.is_default);
        if (defaultModel) setClaudeModel(defaultModel.id);
      }
      if (!openaiModel && openai.models.length > 0) {
        const defaultModel = openai.models.find(m => m.is_default);
        if (defaultModel) setOpenaiModel(defaultModel.id);
      }
      if (!ollamaModel && ollama.models.length > 0) {
        const defaultModel = ollama.models.find(m => m.is_default);
        if (defaultModel) setOllamaModel(defaultModel.id);
      }
      if (!copilotDeployment && copilot.models.length > 0) {
        const defaultModel = copilot.models.find(m => m.is_default);
        if (defaultModel) setCopilotDeployment(defaultModel.id);
      }
    } catch (error) {
      console.error("Failed to load available models:", error);
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
          title: "✅ Formato válido",
          description: `${provider} API key tem formato correto. Será testada na primeira análise.`,
        });
      } else {
        toast({
          title: "❌ Formato inválido",
          description: response.error || "Token não tem formato válido",
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
    // Validar que ai_email está preenchido
    if (!aiEmail || aiEmail.trim() === "") {
      toast({
        title: "⚠️ Email obrigatório",
        description: "Por favor, preencha seu email para identificar suas configurações AI",
        variant: "destructive",
      });
      return;
    }

    // Validar formato de email básico
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(aiEmail)) {
      toast({
        title: "⚠️ Email inválido",
        description: "Por favor, insira um email válido (ex: seu.email@exemplo.com)",
        variant: "destructive",
      });
      return;
    }

    setSaving(true);

    try {
      // Preparar payload - enviar apenas campos preenchidos
      const payload: any = {
        ai_email: aiEmail.trim(), // Email para identificar configurações (independente do Azure AD)
        preferred_provider: preferredProvider,
      };

      // API Keys - enviar apenas se foram preenchidas (não vazias)
      if (geminiKey) payload.gemini_api_key = geminiKey;
      if (openaiKey) payload.openai_api_key = openaiKey;
      if (claudeKey) payload.claude_api_key = claudeKey;
      if (copilotKey) payload.copilot_api_key = copilotKey;

      // Modelos - SEMPRE enviar (permitem trocar modelo sem re-inserir API key)
      if (geminiModel) payload.gemini_model = geminiModel;
      if (openaiModel) payload.openai_model = openaiModel;
      if (claudeModel) payload.claude_model = claudeModel;
      if (ollamaModel) payload.ollama_model = ollamaModel;

      // Copilot configs - SEMPRE enviar (não são sensíveis)
      if (copilotEndpoint) payload.copilot_endpoint = copilotEndpoint;
      if (copilotDeployment) payload.copilot_deployment = copilotDeployment;

      console.log("[AISettingsTab] Salvando configurações:", {
        ai_email: payload.ai_email,
        preferred_provider: payload.preferred_provider,
        gemini_api_key: payload.gemini_api_key ? "***" : undefined,
        openai_api_key: payload.openai_api_key ? "***" : undefined,
        claude_api_key: payload.claude_api_key ? "***" : undefined,
        copilot_api_key: payload.copilot_api_key ? "***" : undefined,
      });

      await apiClient.saveAITokens(payload);

      // Armazenar ai_email no localStorage para uso futuro
      console.log("[AISettingsTab] Salvando ai_email no localStorage:", aiEmail.trim());
      localStorage.setItem("ai_email", aiEmail.trim());

      toast({
        title: "✅ Configurações salvas",
        description: "Seus tokens AI foram salvos com sucesso",
      });

      // Notificar todos os componentes que usam useAIDiagnostics para atualizarem
      // seus estados locais. Resolve o problema de cache onde componentes mantinham
      // o status antigo após salvar novas configurações.
      console.log("[AISettingsTab] Disparando evento 'ai-settings-updated' para atualizar status...");
      window.dispatchEvent(new CustomEvent("ai-settings-updated"));
      console.log("[AISettingsTab] Evento disparado - outros componentes devem atualizar agora");

      // Limpar apenas campos de senha (API keys) após salvar
      // Manter endpoint e deployment pois são necessários para validação/uso posterior
      setGeminiKey("");
      setOpenaiKey("");
      setClaudeKey("");
      setCopilotKey("");

      // Recarregar status (apenas indicadores has_gemini, has_claude, etc)
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
      await apiClient.deleteAITokens(aiEmail);

      toast({
        title: "✅ Tokens removidos",
        description: "Suas configurações AI foram removidas",
      });

      // Notificar todos os componentes que usam useAIDiagnostics
      window.dispatchEvent(new CustomEvent("ai-settings-updated"));
      console.log("[AISettingsTab] Evento 'ai-settings-updated' disparado após remoção de tokens");

      // Limpar localStorage
      localStorage.removeItem("ai_email");

      // Limpar form
      setAiEmail("");
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
          {/* Aviso Importante sobre Validação */}
          <div className="rounded-lg border border-yellow-200 dark:border-yellow-800 bg-yellow-50 dark:bg-yellow-950 p-4">
            <div className="flex items-start gap-3">
              <div className="text-yellow-600 dark:text-yellow-400 mt-0.5">⚠️</div>
              <div className="space-y-1">
                <p className="text-sm font-medium text-yellow-900 dark:text-yellow-100">
                  Importante: Validação de Formato Apenas
                </p>
                <p className="text-xs text-yellow-800 dark:text-yellow-200">
                  O botão "Validar Formato" verifica apenas se a chave tem formato correto, <strong>sem fazer chamadas à API</strong>.
                  A validação real (e consumo de quota) acontece apenas quando você usar o provider em uma análise real.
                  Isso previne consumo desnecessário de quota da sua API.
                </p>
              </div>
            </div>
          </div>

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

          {/* AI Email (identificação das configurações - independente do Azure AD) */}
          <div className="space-y-2">
            <Label htmlFor="ai-email" className="flex items-center gap-2">
              Seu Email (para AI)
            </Label>
            <Input
              id="ai-email"
              type="email"
              placeholder="seu.email@exemplo.com"
              value={aiEmail}
              onChange={(e) => setAiEmail(e.target.value)}
              className="font-mono text-sm"
            />
            <p className="text-xs text-muted-foreground">
              Este email identifica suas configurações AI (não precisa ser o mesmo do Azure AD).
              Todas as suas API keys e preferências ficam vinculadas a este email.
            </p>
          </div>

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
                  "Validar Formato"
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

            {/* Seleção de Modelo Gemini */}
            <div className="space-y-2">
              <Label htmlFor="gemini-model">Modelo Gemini</Label>
              <Select value={geminiModel} onValueChange={setGeminiModel}>
                <SelectTrigger id="gemini-model">
                  <SelectValue placeholder="Selecione o modelo" />
                </SelectTrigger>
                <SelectContent>
                  {geminiModels.map((model) => (
                    <SelectItem key={model.id} value={model.id}>
                      {model.name} {model.is_default && "(Padrão)"} {model.description && `- ${model.description}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
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
                  "Validar Formato"
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

            {/* Seleção de Modelo OpenAI */}
            <div className="space-y-2">
              <Label htmlFor="openai-model">Modelo OpenAI</Label>
              <Select value={openaiModel} onValueChange={setOpenaiModel}>
                <SelectTrigger id="openai-model">
                  <SelectValue placeholder="Selecione o modelo" />
                </SelectTrigger>
                <SelectContent>
                  {openaiModels.map((model) => (
                    <SelectItem key={model.id} value={model.id}>
                      {model.name} {model.is_default && "(Padrão)"} {model.description && `- ${model.description}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
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
                  "Validar Formato"
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

            {/* Seleção de Modelo Claude */}
            <div className="space-y-2">
              <Label htmlFor="claude-model">Modelo Claude</Label>
              <Select value={claudeModel} onValueChange={setClaudeModel}>
                <SelectTrigger id="claude-model">
                  <SelectValue placeholder="Selecione o modelo" />
                </SelectTrigger>
                <SelectContent>
                  {claudeModels.map((model) => (
                    <SelectItem key={model.id} value={model.id}>
                      {model.name} {model.is_default && "(Padrão)"} {model.description && `- ${model.description}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
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
                  "Validar Formato"
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

            {/* Copilot Deployment - Select Dropdown */}
            <div className="space-y-1">
              <Label htmlFor="copilot-deployment" className="text-xs">Deployment / Modelo</Label>
              <Select value={copilotDeployment} onValueChange={setCopilotDeployment}>
                <SelectTrigger id="copilot-deployment" className="text-sm">
                  <SelectValue placeholder="Selecione o deployment" />
                </SelectTrigger>
                <SelectContent>
                  {copilotModels.map((model) => (
                    <SelectItem key={model.id} value={model.id} className="text-sm">
                      {model.name} {model.is_default && "(Padrão)"} {model.description && `- ${model.description}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
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

          {/* Ollama Model Selection */}
          <div className="space-y-2">
            <Label htmlFor="ollama-model" className="flex items-center gap-2">
              Modelo Ollama (Local)
            </Label>
            <Select value={ollamaModel} onValueChange={setOllamaModel}>
              <SelectTrigger id="ollama-model">
                <SelectValue placeholder="Selecione o modelo Ollama" />
              </SelectTrigger>
              <SelectContent>
                {ollamaModels.map((model) => (
                  <SelectItem key={model.id} value={model.id}>
                    {model.name} {model.is_default && "(Padrão)"} {model.description && `- ${model.description}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Ollama roda localmente (grátis). Instale em:{" "}
              <a
                href="https://ollama.com/download"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:underline"
              >
                https://ollama.com/download
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
