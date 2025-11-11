import { useEffect, useMemo, useState } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { toast } from "sonner";

import type {
  Namespace,
  ConfigMapSummary,
  ConfigMapManifest,
} from "@/lib/api/types";
import { useConfigMaps } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";

interface ConfigMapsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const ConfigMapsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: ConfigMapsTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedConfigMap, setSelectedConfigMap] = useState<ConfigMapSummary | null>(null);
  const [manifest, setManifest] = useState<ConfigMapManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [showLabels, setShowLabels] = useState(true);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { configMaps, loading, error, refetch } = useConfigMaps(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar ConfigMaps", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedConfigMap(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(true);
    setViewMode("editor");
  }, [cluster, selectedNamespace]);

  const filteredConfigMaps = useMemo(() => {
    if (!searchQuery) return configMaps;
    const query = searchQuery.toLowerCase();
    return configMaps.filter((cm) => {
      return (
        cm.name.toLowerCase().includes(query) ||
        cm.namespace.toLowerCase().includes(query) ||
        Object.entries(cm.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [configMaps, searchQuery]);

  const handleSelectConfigMap = async (summary: ConfigMapSummary) => {
    setSelectedConfigMap(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getConfigMap(
        summary.cluster,
        summary.namespace,
        summary.name
      );
      setManifest(detail);
      setEditorValue(detail.yaml || "");
      setOriginalYaml(detail.yaml || "");
      setShowLabels(true);
      setViewMode("editor");
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const refreshConfigMaps = () => {
    if (!cluster) return;
    refetch();
  };

  const refreshManifest = () => {
    if (selectedConfigMap) {
      handleSelectConfigMap(selectedConfigMap);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && !hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setViewMode(mode);
  };

  const handleValidate = async () => {
    if (!selectedConfigMap) return;
    setIsValidating(true);
    try {
      await apiClient.validateConfigMap({
        cluster: selectedConfigMap.cluster,
        namespace: selectedConfigMap.namespace,
        yaml: editorValue,
        fieldManager: "web-configmap-editor",
      });
      toast.success("Dry-run bem-sucedido", {
        description: `${selectedConfigMap.namespace}/${selectedConfigMap.name}`,
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
    if (!selectedConfigMap) return;
    const confirmed = window.confirm(
      `Aplicar ConfigMap ${selectedConfigMap.namespace}/${selectedConfigMap.name}?`
    );
    if (!confirmed) return;

    setIsApplying(true);
    try {
      await apiClient.applyConfigMap(
        selectedConfigMap.cluster,
        selectedConfigMap.namespace,
        selectedConfigMap.name,
        {
          yaml: editorValue,
          fieldManager: "web-configmap-editor",
          dryRun: false,
        }
      );
      toast.success("ConfigMap aplicado", {
        description: `${selectedConfigMap.namespace}/${selectedConfigMap.name}`,
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

  const leftTitleAction = (
    <>
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
      </Button>
      <Button variant="outline" size="sm" onClick={refreshConfigMaps} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
      </Button>
    </>
  );

  const rightTitleAction = (
    <Button
      variant="outline"
      size="sm"
      onClick={refreshManifest}
      disabled={!selectedConfigMap || manifestLoading}
    >
      <RefreshCcw className="w-4 h-4 mr-2" />
      Recarregar YAML
    </Button>
  );

  const renderConfigMapList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar ConfigMaps
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando ConfigMaps...
        </div>
      );
    }

    if (filteredConfigMaps.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {configMaps.length === 0
            ? "Nenhum ConfigMap encontrado"
            : "Nenhum ConfigMap corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredConfigMaps.map((cm) => {
          const isSelected =
            selectedConfigMap?.name === cm.name &&
            selectedConfigMap?.namespace === cm.namespace;
          return (
            <button
              key={`${cm.cluster}-${cm.namespace}-${cm.name}`}
              onClick={() => handleSelectConfigMap(cm)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="font-semibold text-sm">{cm.name}</div>
              <div className="text-xs text-muted-foreground">{cm.namespace}</div>
              <div className="text-[11px] text-muted-foreground mt-1">
                {cm.dataKeys.length} keys • {cm.binaryKeys.length} binárias
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
          Selecione um cluster para visualizar ConfigMaps
        </div>
      );
    }

    if (!selectedConfigMap) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um ConfigMap para visualizar o manifesto
        </div>
      );
    }

    const updatedAt = selectedConfigMap.updatedAt
      ? new Date(selectedConfigMap.updatedAt).toLocaleString()
      : "--";

    return (
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <p className="text-xs text-muted-foreground uppercase">Cluster</p>
            <p className="font-medium break-all">{selectedConfigMap.cluster}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground uppercase">Namespace</p>
            <p className="font-medium break-all">{selectedConfigMap.namespace}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground uppercase">ResourceVersion</p>
            <p className="font-mono text-xs">{selectedConfigMap.resourceVersion || "--"}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground uppercase">Atualizado</p>
            <p>{updatedAt}</p>
          </div>
        </div>

        {selectedConfigMap.labels && Object.keys(selectedConfigMap.labels).length > 0 && (
          <div className="text-xs">
            <button
              type="button"
              onClick={() => setShowLabels((prev) => !prev)}
              className="flex items-center gap-2 text-left text-muted-foreground mb-1"
            >
              {showLabels ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
              <span>Labels</span>
            </button>
            {showLabels && (
              <div className="flex flex-wrap gap-2">
                {Object.entries(selectedConfigMap.labels).map(([key, value]) => (
                  <span
                    key={`${key}-${value}`}
                    className="px-2 py-1 bg-secondary/60 rounded-md font-mono"
                  >
                    {key}={value}
                  </span>
                ))}
              </div>
            )}
          </div>
        )}

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
              </div>
            </div>
            {viewMode === "editor" && (
              <MonacoYamlEditor
                value={editorValue}
                onChange={setEditorValue}
                height={360}
              />
            )}
            {viewMode === "diff" && (
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalYaml}
                value={editorValue}
                height={360}
                readOnly
              />
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={handleValidate}
              disabled={!selectedConfigMap || isValidating}
            >
              <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={handleApply}
              disabled={!selectedConfigMap || isApplying || !hasChanges}
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
              <Input
                placeholder="Buscar por nome ou label..."
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                className="pl-10"
              />
            </div>

            {renderConfigMapList()}
          </div>
  );

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de ConfigMaps" : "Ocultar painel de ConfigMaps"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

  if (isSidebarCollapsed) {
    return (
      <div className="p-4 h-full">
        <div className="grid grid-cols-1 h-full">
          <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
            <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
              <div className="flex items-center gap-2">
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
    );
  }

  return (
    <SplitView
      leftPanel={{
        title: "ConfigMaps",
        titleAction: (
          <div className="flex items-center gap-2">
            {collapseButton}
            <Button
              variant={showSystemNamespaces ? "secondary" : "outline"}
              size="sm"
              onClick={onToggleSystemNamespaces}
              title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
            >
              {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
            </Button>
            <Button variant="outline" size="sm" onClick={refreshConfigMaps} disabled={!cluster || loading}>
              <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
            </Button>
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
  );
};
