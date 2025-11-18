import { useEffect, useMemo, useState, useCallback } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, PanelLeftClose, PanelLeftOpen, FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, Lock, Unlock } from "lucide-react";
import { toast } from "sonner";
import * as yaml from "js-yaml";

import type {
  Namespace,
  SecretSummary,
  SecretManifest,
} from "@/lib/api/types";
import { useSecrets } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";

interface SecretsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const SecretsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: SecretsTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedSecret, setSelectedSecret] = useState<SecretSummary | null>(null);
  const [manifest, setManifest] = useState<SecretManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [showLabels, setShowLabels] = useState(true);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [isDecoded, setIsDecoded] = useState(false);
  const [editorFullScreen, setEditorFullScreen] = useState(false);

  // Undo/Redo history
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const filteredNamespaces = useMemo(() => {
    if (!namespaces || !Array.isArray(namespaces)) return [];
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) {
      onNamespaceChange("");
    }
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { secrets, loading, error, refetch } = useSecrets(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar Secrets", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedSecret(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(true);
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
    setIsDecoded(false);
  }, [cluster, selectedNamespace]);

  const filteredSecrets = useMemo(() => {
    let filtered = secrets;
    
    // Filtrar secrets de sistema quando Sistema está desligado
    if (!showSystemNamespaces) {
      filtered = filtered.filter((secret) => 
        !secret.name.includes("sh.helm.release.") && 
        !secret.name.startsWith("default-") &&
        !secret.name.includes("alertmanager") &&
        !secret.name.includes("prometheus")
      );
    }
    
    // Aplicar busca por query
    if (!searchQuery) return filtered;
    const query = searchQuery.toLowerCase();
    return filtered.filter((cm) => {
      return (
        cm.name.toLowerCase().includes(query) ||
        cm.namespace.toLowerCase().includes(query) ||
        Object.entries(cm.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [secrets, searchQuery, showSystemNamespaces]);

  const handleSelectSecret = async (summary: SecretSummary) => {
    setSelectedSecret(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getSecret(
        summary.cluster,
        summary.namespace,
        summary.name
      );
      setManifest(detail);
      const initialYaml = detail.yaml || "";
      setEditorValue(initialYaml);
      setOriginalYaml(initialYaml);
      setShowLabels(true);
      setViewMode("editor");
      // Inicializar histórico com valor inicial
      setHistory([initialYaml]);
      setHistoryIndex(0);
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // Atualizar histórico quando o editor muda
  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);

    // Adicionar ao histórico (remover itens futuros se estamos no meio do histórico)
    setHistoryIndex((currentIndex) => {
      setHistory((prev) => {
        const newHistory = prev.slice(0, currentIndex + 1);
        newHistory.push(value);
        // Limitar histórico a 50 itens
        if (newHistory.length > 50) {
          newHistory.shift();
          return newHistory;
        }
        return newHistory;
      });
      return Math.min(currentIndex + 1, 49);
    });
  }, []);

  // Undo
  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  // Redo
  const handleRedo = useCallback(() => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  const refreshSecrets = () => {
    if (!cluster) return;
    refetch();
  };

  const refreshManifest = () => {
    if (selectedSecret) {
      handleSelectSecret(selectedSecret);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && !hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setViewMode(mode);
  };

  const handleToggleDecode = () => {
    // Guard: Verificar se há um Secret selecionado e se o editor tem conteúdo
    if (!selectedSecret) {
      toast.warning("Selecione um Secret primeiro");
      return;
    }

    if (!editorValue || editorValue.trim() === '') {
      toast.warning("Editor vazio - carregue um Secret primeiro");
      return;
    }

    try {
      const yamlObj = yaml.load(editorValue) as any;

      if (!yamlObj || !yamlObj.data) {
        toast.error("YAML inválido ou sem seção 'data'");
        return;
      }

      // Preservar formatação original alterando apenas os valores
      let newYaml = editorValue;

      if (isDecoded) {
        // RE-ENCODE: texto plano → Base64
        for (const [key, value] of Object.entries(yamlObj.data)) {
          if (typeof value === 'string') {
            const encoded = btoa(value);
            // Substituir apenas o valor deste campo específico, mantendo formatação
            const regex = new RegExp(`(\\s+${key}:\\s+)(?:"[^"]*"|'[^']*'|[^\\n]+)`, 'g');
            newYaml = newYaml.replace(regex, `$1${encoded}`);
          }
        }
        toast.success("Valores re-encodificados para Base64");
      } else {
        // DECODE: Base64 → texto plano
        for (const [key, value] of Object.entries(yamlObj.data)) {
          if (typeof value === 'string') {
            try {
              const decoded = atob(value);
              // Substituir apenas o valor deste campo específico, mantendo formatação
              const regex = new RegExp(`(\\s+${key}:\\s+)(?:"[^"]*"|'[^']*'|[^\\n]+)`, 'g');
              newYaml = newYaml.replace(regex, `$1${decoded}`);
            } catch {
              // Se não for Base64 válido, mantém original
            }
          }
        }
        toast.success("Valores decodificados para texto plano");
      }

      setEditorValue(newYaml);
      setIsDecoded(!isDecoded);
    } catch (err) {
      toast.error("Erro ao processar YAML", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const handleShowDiffModal = async (fullscreen = false) => {
    if (!selectedSecret) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setIsDiffLoading(true);
    try {
      const diffResponse = await apiClient.diffSecret(originalYaml, editorValue, selectedSecret?.name);
      const unifiedDiff = diffResponse.unifiedDiff || "";
      const html = diff2html(unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffFullScreen(fullscreen);
      setDiffModalOpen(true);
    } catch (error) {
      toast.error("Erro ao gerar diff visual", {
        description: error instanceof Error ? error.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleDiffModalChange = (open: boolean) => {
    setDiffModalOpen(open);
    if (!open) {
      setDiffFullScreen(false);
    }
  };

  const toggleDiffFullScreen = () => {
    setDiffFullScreen((prev) => !prev);
  };

  const handleValidate = async () => {
    if (!selectedSecret) return;
    setIsValidating(true);
    try {
      await apiClient.validateSecret({
        cluster: selectedSecret.cluster,
        namespace: selectedSecret.namespace,
        yaml: editorValue,
        fieldManager: "web-configmap-editor",
      });
      toast.success("Dry-run bem-sucedido", {
        description: `${selectedSecret.namespace}/${selectedSecret.name}`,
      });
    } catch (err) {
      toast.error("Dry-run falhou", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleApply = async () => {
    if (!selectedSecret) return;

    // VALIDAÇÃO: Previne aplicação com valores decodificados
    if (isDecoded) {
      toast.error("Não é possível aplicar com valores decodificados", {
        description: "Clique no botão 'Encoded' para re-encodificar antes de aplicar",
      });
      return;
    }

    setIsApplying(true);
    try {
      await apiClient.applySecret(
        selectedSecret.cluster,
        selectedSecret.namespace,
        selectedSecret.name,
        {
          yaml: editorValue,
          fieldManager: "web-secret-editor",
          dryRun: false,
        }
      );
      toast.success("Secret aplicado", {
        description: `${selectedSecret.namespace}/${selectedSecret.name}`,
      });
      refreshManifest();
    } catch (err) {
      toast.error("Falha ao aplicar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsApplying(false);
    }
  };

  const openApplyConfirm = () => {
    if (!selectedSecret) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para aplicar");
      return;
    }
    setApplyConfirmOpen(true);
  };

  const confirmApplyChanges = async () => {
    setApplyConfirmOpen(false);
    await handleApply();
  };

  const namespaceSelector = (
    <Select
      value={selectedNamespace || "__all__"}
      onValueChange={(value) => onNamespaceChange(value === "__all__" ? "" : value)}
      disabled={!cluster || filteredNamespaces.length === 0}
    >
      <SelectTrigger className="w-[140px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="__all__">Todos</SelectItem>
        {filteredNamespaces.map((ns) => (
          <SelectItem key={ns.name} value={ns.name}>
            {ns.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {namespaceSelector}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Mostrar namespaces e secrets de sistema (incluindo Helm)" : "Ocultar namespaces e secrets de sistema (incluindo Helm)"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
      </Button>
      <Button variant="outline" size="sm" onClick={refreshSecrets} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
      </Button>
    </div>
  );

  const rightTitleAction = (
    <Button
      variant="outline"
      size="sm"
      onClick={refreshManifest}
      disabled={!selectedSecret || manifestLoading}
    >
      <RefreshCcw className="w-4 h-4 mr-2" />
      Recarregar YAML
    </Button>
  );

  const renderSecretList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Secrets
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando Secrets...
        </div>
      );
    }

    if (filteredSecrets.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {secrets.length === 0
            ? "Nenhum Secret encontrado"
            : "Nenhum Secret corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredSecrets.map((cm) => {
          const isSelected =
            selectedSecret?.name === cm.name &&
            selectedSecret?.namespace === cm.namespace;
          return (
            <button
              key={`${cm.cluster}-${cm.namespace}-${cm.name}`}
              onClick={() => handleSelectSecret(cm)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="font-semibold text-sm">{cm.name}</div>
              <div className="text-xs text-muted-foreground">{cm.namespace}</div>
              <div className="text-[11px] text-muted-foreground mt-1">
                {cm.dataKeys.length} {cm.dataKeys.length === 1 ? 'key' : 'keys'}
              </div>
            </button>
          );
        })}
      </div>
    );
  };

  const hasChanges = editorValue !== originalYaml;

  const renderManifestPanel = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar Secrets
        </div>
      );
    }

    if (!selectedSecret) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um Secret para visualizar o manifesto
        </div>
      );
    }

    const updatedAt = selectedSecret.updatedAt
      ? new Date(selectedSecret.updatedAt).toLocaleString()
      : "--";

    return (
      <div className="space-y-3">
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedSecret.cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
            <span className="font-medium">{selectedSecret.namespace}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">ResourceVersion</span>
            <span className="font-mono">{selectedSecret.resourceVersion || "--"}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Atualizado</span>
            <span className="font-medium">{updatedAt}</span>
          </div>
          {selectedSecret.labels && Object.keys(selectedSecret.labels).length > 0 && (
            <div className="flex flex-col">
              <button
                type="button"
                onClick={() => setShowLabels((prev) => !prev)}
                className="flex items-center gap-1 text-muted-foreground uppercase mb-0.5 hover:text-foreground"
              >
                {showLabels ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                <span>Labels</span>
              </button>
              {showLabels && (
                <div className="flex flex-wrap gap-1 mt-1">
                  {Object.entries(selectedSecret.labels).map(([key, value]) => (
                    <span
                      key={`${key}-${value}`}
                      className="px-1.5 py-0.5 bg-secondary/60 rounded text-[10px] font-mono"
                    >
                      {key}={value}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        <div className="space-y-3">
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Manifesto YAML</p>
              <div className="flex items-center gap-2">
                {manifestLoading && (
                  <span className="text-xs text-muted-foreground">Carregando...</span>
                )}
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleUndo}
                    disabled={!canUndo}
                    className={`px-2 py-1 text-xs font-medium ${
                      canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Desfazer (Ctrl+Z)"
                  >
                    <Undo2 className="w-3.5 h-3.5" />
                  </button>
                  <button
                    type="button"
                    onClick={handleRedo}
                    disabled={!canRedo}
                    className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                      canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Refazer (Ctrl+Y)"
                  >
                    <Redo2 className="w-3.5 h-3.5" />
                  </button>
                </div>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={() => handleToggleView("editor")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    }`}
                  >
                    Editor
                  </button>
                  <button
                    type="button"
                    onClick={() => handleToggleView("diff")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                    disabled={!hasChanges}
                  >
                    Diff
                  </button>
                </div>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleToggleDecode}
                    className={`px-3 py-1 text-xs font-medium flex items-center gap-1.5 ${
                      isDecoded
                        ? "bg-yellow-500/20 text-yellow-600 dark:text-yellow-400 border-yellow-500/30"
                        : "bg-green-500/20 text-green-600 dark:text-green-400 border-green-500/30"
                    }`}
                    title={isDecoded ? "Re-encodificar para Base64" : "Decodificar para texto plano"}
                  >
                    {isDecoded ? (
                      <>
                        <Unlock className="w-3.5 h-3.5" />
                        Decoded
                      </>
                    ) : (
                      <>
                        <Lock className="w-3.5 h-3.5" />
                        Encoded
                      </>
                    )}
                  </button>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditorFullScreen(true)}
                  title="Abrir editor em tela cheia"
                  disabled={!selectedSecret}
                >
                  <Maximize2 className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
            {viewMode === "editor" && (
              <MonacoYamlEditor
                value={editorValue}
                onChange={handleEditorChange}
                height={520}
              />
            )}
            {viewMode === "diff" && (
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalYaml}
                value={editorValue}
                height={520}
                readOnly
              />
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal(false)}
              disabled={!selectedSecret || !hasChanges || isDiffLoading}
            >
              {isDiffLoading ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <FileDiff className="w-4 h-4 mr-2" />
              )}
              Visualizar diff
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal(true)}
              disabled={!selectedSecret || !hasChanges || isDiffLoading}
              className="gap-2"
              title="Abrir diff ocupando toda a tela"
            >
              <Maximize2 className="w-4 h-4" />
              Tela cheia
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleValidate}
              disabled={!selectedSecret || isValidating}
            >
              <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={openApplyConfirm}
              disabled={!selectedSecret || isApplying || !hasChanges}
            >
              <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
            </Button>
          </div>

        </div>
      </div>
    );
  };

  const leftContent = (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        {searchQuery && (
          <button
            type="button"
            onClick={() => setSearchQuery("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            aria-label="Limpar busca"
          >
            ×
          </button>
        )}
        <Input
          placeholder="Buscar por nome ou label..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderSecretList()}
    </div>
  );

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Secrets" : "Ocultar painel de Secrets"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

  const renderDiffDialog = () => {
    const dialogSizeClass = diffFullScreen
      ? "w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none"
      : "max-w-6xl max-h-[85vh]";
    const scrollAreaHeight = diffFullScreen ? "h-[calc(100vh-8rem)]" : "h-[calc(85vh-8rem)]";

    return (
      <Dialog open={diffModalOpen} onOpenChange={handleDiffModalChange}>
        <DialogContent className={`bg-background border-border ${dialogSizeClass}`}>
          <DialogHeader className="border-b border-border pb-4 pr-12">
            <div className="flex items-start justify-between gap-4">
              <div>
                <DialogTitle className="text-xl font-semibold text-primary">
                  Diff Visual
                </DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground">
                  Comparação lado a lado entre o YAML original e a versão editada
                  {selectedSecret && ` • ${selectedSecret.namespace}/${selectedSecret.name}`}
                </DialogDescription>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={toggleDiffFullScreen}
                title={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                aria-label={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                className="gap-2"
              >
                {diffFullScreen ? (
                  <>
                    <Minimize2 className="w-4 h-4" />
                    <span>Sair de tela cheia</span>
                  </>
                ) : (
                  <>
                    <Maximize2 className="w-4 h-4" />
                    <span>Tela cheia</span>
                  </>
                )}
              </Button>
            </div>
          </DialogHeader>
          <ScrollArea className={`${scrollAreaHeight} w-full`}>
            <div className="p-4">
              {diffHtml ? (
                <div className="diff2html-dark" dangerouslySetInnerHTML={{ __html: diffHtml }} />
              ) : (
                <div className="flex items-center justify-center h-32 text-muted-foreground">
                  <p>Nenhum diff disponível.</p>
                </div>
              )}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    );
  };

  const renderEditorFullScreen = () => {
    if (!selectedSecret) return null;

    const handleCancel = () => {
      // Restaurar o YAML original, descartando alterações
      setEditorValue(originalYaml);
      setIsDecoded(false);
      setViewMode("editor");
      setEditorFullScreen(false);
      toast.info("Alterações descartadas");
    };

    // Keyboard shortcuts
    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 'z' && !e.shiftKey) {
          e.preventDefault();
          handleUndo();
        } else if ((e.key === 'y') || (e.key === 'z' && e.shiftKey)) {
          e.preventDefault();
          handleRedo();
        } else if (e.key === 's') {
          e.preventDefault();
          if (hasChanges && !isApplying) {
            openApplyConfirm();
            setEditorFullScreen(false);
          }
        }
      } else if (e.key === 'Escape') {
        handleCancel();
      }
    };

    return (
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent 
          className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0"
          onKeyDown={handleKeyDown}
        >
          <div className="h-full flex flex-col">
            <DialogHeader className="border-b border-border px-6 py-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <DialogTitle className="text-xl font-semibold text-primary">
                    Editor YAML - Tela Cheia
                  </DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground">
                    {selectedSecret.namespace}/{selectedSecret.name}
                  </DialogDescription>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={handleUndo}
                      disabled={!canUndo}
                      className={`px-2 py-1 text-xs font-medium ${
                        canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Desfazer (Ctrl+Z)"
                    >
                      <Undo2 className="w-3.5 h-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={handleRedo}
                      disabled={!canRedo}
                      className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                        canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Refazer (Ctrl+Y)"
                    >
                      <Redo2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={() => handleToggleView("editor")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      }`}
                    >
                      Editor
                    </button>
                    <button
                      type="button"
                      onClick={() => handleToggleView("diff")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                      disabled={!hasChanges}
                    >
                      Diff
                    </button>
                  </div>
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={handleToggleDecode}
                      className={`px-3 py-1 text-xs font-medium flex items-center gap-1.5 ${
                        isDecoded
                          ? "bg-yellow-500/20 text-yellow-600 dark:text-yellow-400 border-yellow-500/30"
                          : "bg-green-500/20 text-green-600 dark:text-green-400 border-green-500/30"
                      }`}
                      title={isDecoded ? "Re-encodificar para Base64" : "Decodificar para texto plano"}
                    >
                      {isDecoded ? (
                        <>
                          <Unlock className="w-3.5 h-3.5" />
                          Decoded
                        </>
                      ) : (
                        <>
                          <Lock className="w-3.5 h-3.5" />
                          Encoded
                        </>
                      )}
                    </button>
                  </div>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={handleValidate}
                    disabled={!selectedSecret || isValidating}
                  >
                    {isValidating ? (
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <CheckCircle2 className="w-4 h-4 mr-2" />
                    )}
                    Dry-run
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleCancel}
                    title="Descartar alterações e sair (Esc)"
                  >
                    Cancelar
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => {
                      openApplyConfirm();
                      setEditorFullScreen(false);
                    }}
                    disabled={!selectedSecret || isApplying || !hasChanges}
                  >
                    <TriangleAlert className="w-4 h-4 mr-2" />
                    Aplicar
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditorFullScreen(false)}
                    title="Minimizar tela cheia (Esc)"
                  >
                    <Minimize2 className="w-4 h-4" />
                  </Button>
                </div>
              </div>
            </DialogHeader>
            <div className="flex-1 p-4">
              {viewMode === "editor" && (
                <MonacoYamlEditor
                  value={editorValue}
                  onChange={handleEditorChange}
                  height="calc(100vh - 140px)"
                />
              )}
              {viewMode === "diff" && (
                <MonacoYamlEditor
                  mode="diff"
                  originalValue={originalYaml}
                  value={editorValue}
                  height="calc(100vh - 140px)"
                  readOnly
                />
              )}
            </div>
          </div>
        </DialogContent>
      </Dialog>
    );
  };

  const renderApplyConfirmDialog = () => {
    if (!selectedSecret) return null;

    return (
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent className="max-w-md bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">
              Confirmar aplicação
            </DialogTitle>
            <DialogDescription>
              Essa ação vai aplicar o Secret diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {selectedSecret.cluster}</p>
              <p><span className="text-muted-foreground">Namespace:</span> {selectedSecret.namespace}</p>
              <p><span className="text-muted-foreground">Secret:</span> {selectedSecret.name}</p>
            </div>
            <p className="text-muted-foreground">
              Certifique-se de que o diff foi revisado antes de confirmar. Esta operação não possui rollback automático.
            </p>
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Button
              variant="ghost"
              onClick={() => setApplyConfirmOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={confirmApplyChanges}
              disabled={isApplying}
            >
              {isApplying ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <TriangleAlert className="w-4 h-4 mr-2" />
              )}
              Confirmar
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  };

  if (isSidebarCollapsed) {
    return (
      <>
        <div className="p-4 h-full">
          <div className="grid grid-cols-1 h-full">
            <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
              <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
                <div className="flex items-center gap-3 flex-wrap">
                  {collapseButton}
                  <p className="text-base font-semibold text-primary">Visualização</p>
                </div>
                {rightTitleAction}
              </div>
              <div className="flex-1 overflow-auto min-h-0">
                {renderManifestPanel()}
              </div>
            </div>
          </div>
        </div>

        {renderDiffDialog()}
        {renderEditorFullScreen()}
        {renderApplyConfirmDialog()}
      </>
    );
  }

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Secrets",
          titleAction: (
            <div className="flex items-center gap-2">
              {namespaceSelector}
              <Button
                variant={showSystemNamespaces ? "secondary" : "outline"}
                size="sm"
                onClick={onToggleSystemNamespaces}
                title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
              >
                {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
              </Button>
              <Button variant="outline" size="sm" onClick={refreshSecrets} disabled={!cluster || loading}>
                <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
              </Button>
              {collapseButton}
            </div>
          ),
          content: leftContent,
        }}
        rightPanel={{
          title: "Visualização",
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {renderDiffDialog()}
      {renderEditorFullScreen()}
      {renderApplyConfirmDialog()}
    </>
  );
};
