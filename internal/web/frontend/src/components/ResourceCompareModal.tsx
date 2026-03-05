import { useState, useEffect, useCallback, useRef } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { ProtectedAction } from "@/components/rbac";
import { apiClient } from "@/lib/api/client";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import yaml from "js-yaml";
import {
  X,
  SplitSquareHorizontal,
  Loader2,
  AlertCircle,
  RefreshCcw,
  Undo2,
  Redo2,
  Maximize2,
  Minimize2,
  FileDiff,
  CheckCircle2,
  TriangleAlert,
  ChevronRight,
} from "lucide-react";
import { toast } from "sonner";
import { useClusters } from "@/hooks/useAPI";

// ─── Tipos ────────────────────────────────────────────────────────────────────

export type ResourceType =
  | "deployment"
  | "configmap"
  | "secret"
  | "daemonset"
  | "statefulset"
  | "ingress"
  | "namespace"
  | "pod";

export interface CompareInitial {
  type: ResourceType;
  namespace: string;
  name: string;
}

interface ResourceCompareModalProps {
  open: boolean;
  onClose: () => void;
  cluster: string;
  initialLeft?: CompareInitial;
}

// ─── Constantes ───────────────────────────────────────────────────────────────

const RESOURCE_LABELS: Record<ResourceType, string> = {
  deployment:  "Deployment",
  configmap:   "ConfigMap",
  secret:      "Secret",
  daemonset:   "DaemonSet",
  statefulset: "StatefulSet",
  ingress:     "Ingress",
  namespace:   "Namespace",
  pod:         "Pod",
};

const RESOURCE_TYPES = Object.entries(RESOURCE_LABELS) as [ResourceType, string][];
const CLUSTER_SCOPED: ResourceType[] = ["namespace"];
const FIELD_MANAGER = "web-resource-editor";

// ─── Fetchers genéricos ───────────────────────────────────────────────────────

async function fetchYaml(type: ResourceType, cluster: string, namespace: string, name: string): Promise<string> {
  switch (type) {
    case "deployment":  return (await apiClient.getDeployment(cluster, namespace, name)).yaml;
    case "configmap":   return (await apiClient.getConfigMap(cluster, namespace, name)).yaml;
    case "secret":      return (await apiClient.getSecret(cluster, namespace, name)).yaml;
    case "daemonset":   return (await apiClient.getDaemonSet(cluster, namespace, name)).yaml;
    case "statefulset": return (await apiClient.getStatefulSet(cluster, namespace, name)).yaml;
    case "ingress":     return (await apiClient.getIngress(cluster, namespace, name)).yaml;
    case "namespace":   return (await apiClient.getNamespace(cluster, name)).yaml;
    case "pod":         return (await apiClient.getPod(cluster, namespace, name)).yaml;
  }
}

async function fetchList(type: ResourceType, cluster: string, namespace: string): Promise<string[]> {
  switch (type) {
    case "deployment":  return (await apiClient.getDeployments(cluster, namespace ? [namespace] : [], "", true)).map(d => d.name);
    case "configmap":   return (await apiClient.getConfigMaps(cluster, namespace ? [namespace] : [], "", true)).map(c => c.name);
    case "secret":      return (await apiClient.getSecrets(cluster, namespace ? [namespace] : [], true)).map(s => s.name);
    case "daemonset":   return (await apiClient.getDaemonSets(cluster, namespace ? [namespace] : [], "", true)).map(d => d.name);
    case "statefulset": return (await apiClient.getStatefulSets(cluster, namespace ? [namespace] : [], "", true)).map(s => s.name);
    case "ingress":     return (await apiClient.getIngresses(cluster, namespace ? [namespace] : [], "", true)).map(i => i.name);
    case "namespace":   return (await apiClient.getNamespaces(cluster)).map(n => n.name);
    case "pod":         return (await apiClient.getPods(cluster, namespace ? [namespace] : [], "", true)).map(p => p.name);
    default: return [];
  }
}

async function diffResource(
  type: ResourceType,
  originalYaml: string,
  updatedYaml: string,
  name: string
): Promise<string> {
  let resp: { unifiedDiff?: string } | null = null;
  switch (type) {
    case "deployment":  resp = await apiClient.diffDeployment(originalYaml, updatedYaml, name); break;
    case "configmap":   resp = await apiClient.diffConfigMap(originalYaml, updatedYaml, name); break;
    case "secret":      resp = await apiClient.diffSecret(originalYaml, updatedYaml, name); break;
    case "ingress":     resp = await apiClient.diffIngress(originalYaml, updatedYaml, name); break;
    case "daemonset":   resp = await apiClient.diffDaemonSet({ originalYaml, updatedYaml, fileName: name }); break;
    case "statefulset": resp = await apiClient.diffStatefulSet({ originalYaml, updatedYaml, fileName: name }); break;
    default: return "";
  }
  return resp?.unifiedDiff ?? "";
}

async function validateResource(
  type: ResourceType,
  cluster: string,
  namespace: string,
  yamlContent: string
): Promise<void> {
  const payload = { cluster, namespace, yaml: yamlContent, fieldManager: FIELD_MANAGER };
  switch (type) {
    case "deployment":  await apiClient.validateDeployment(payload); break;
    case "configmap":   await apiClient.validateConfigMap(payload); break;
    case "ingress":     await apiClient.validateIngress(payload); break;
    case "daemonset":   await apiClient.validateDaemonSet(payload); break;
    case "statefulset": await apiClient.validateStatefulSet(payload); break;
    default:
      throw new Error(`Dry-run não disponível para ${RESOURCE_LABELS[type]}`);
  }
}

async function applyResource(
  type: ResourceType,
  cluster: string,
  namespace: string,
  name: string,
  yamlContent: string,
  force = true
): Promise<void> {
  const body = { yaml: yamlContent, fieldManager: FIELD_MANAGER, dryRun: false, force };
  switch (type) {
    case "deployment":  await apiClient.applyDeployment(cluster, namespace, name, body); break;
    case "configmap":   await apiClient.applyConfigMap(cluster, namespace, name, body); break;
    case "secret":      await apiClient.applySecret(cluster, namespace, name, body); break;
    case "ingress":     await apiClient.applyIngress(cluster, namespace, name, body); break;
    case "daemonset":   await apiClient.applyDaemonSet(cluster, namespace, name, body); break;
    case "statefulset": await apiClient.applyStatefulSet(cluster, namespace, name, body); break;
    case "namespace":   await apiClient.applyNamespace(cluster, name, { yaml: yamlContent, fieldManager: FIELD_MANAGER, dryRun: false, force }); break;
    case "pod":         await apiClient.applyPod(cluster, namespace, name, { yaml: yamlContent, fieldManager: FIELD_MANAGER, dryRun: false, force }); break;
  }
}

// ─── Utilitário diff ─────────────────────────────────────────────────────────

function generateCompactDiff(originalYaml: string, updatedYaml: string) {
  try {
    const obj1 = yaml.load(originalYaml) as Record<string, unknown>;
    const obj2 = yaml.load(updatedYaml) as Record<string, unknown>;
    const changes: Array<{ path: string; before: string; after: string }> = [];

    const compare = (a: unknown, b: unknown, path: string) => {
      const keys = new Set([...Object.keys(a as object || {}), ...Object.keys(b as object || {})]);
      for (const key of keys) {
        const cur = `${path}${path ? "." : ""}${key}`;
        const v1 = (a as Record<string, unknown>)?.[key];
        const v2 = (b as Record<string, unknown>)?.[key];
        if (v1 === v2) continue;
        if (typeof v1 === "object" && typeof v2 === "object" && v1 && v2 && !Array.isArray(v1) && !Array.isArray(v2)) {
          compare(v1, v2, cur);
        } else if (JSON.stringify(v1) !== JSON.stringify(v2)) {
          changes.push({
            path: cur,
            before: v1 === undefined ? "(não existe)" : typeof v1 === "object" ? JSON.stringify(v1, null, 2) : String(v1),
            after:  v2 === undefined ? "(removido)"  : typeof v2 === "object" ? JSON.stringify(v2, null, 2) : String(v2),
          });
        }
      }
    };

    compare(obj1, obj2, "");
    return changes;
  } catch {
    return [];
  }
}

// ─── Painel de edição independente ───────────────────────────────────────────

interface ResourceEditPanelProps {
  label: "Esquerdo" | "Direito";
  cluster: string;
  namespaces: string[];
  initial?: CompareInitial;
  editorHeight: string;
}

function ResourceEditPanel({ label, cluster, namespaces, initial, editorHeight }: ResourceEditPanelProps) {
  // Seleção de recurso
  const [resourceType, setResourceType] = useState<ResourceType | "">(initial?.type ?? "");
  const [namespace,    setNamespace]    = useState(initial?.namespace ?? "");
  const [resourceName, setResourceName] = useState(initial?.name ?? "");
  const [nameList,     setNameList]     = useState<string[]>([]);
  const [nameListLoading, setNameListLoading] = useState(false);

  // Conteúdo
  const [originalYaml, setOriginalYaml] = useState("");
  const [editorValue,  setEditorValue]  = useState("");
  const [yamlLoading,  setYamlLoading]  = useState(false);
  const [yamlError,    setYamlError]    = useState("");

  // Undo / Redo
  const historyRef = useRef<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < historyRef.current.length - 1;

  // Estados de toolbar
  const [viewMode,       setViewMode]       = useState<"editor" | "diff">("editor");
  const [isValidating,   setIsValidating]   = useState(false);
  const [isApplying,     setIsApplying]     = useState(false);
  const [isDiffLoading,  setIsDiffLoading]  = useState(false);

  // Modais
  const [diffModalOpen,    setDiffModalOpen]    = useState(false);
  const [diffHtml,         setDiffHtml]          = useState("");
  const [diffFullscreen,   setDiffFullscreen]   = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);

  // Error Dialog para exibir erros de apply de forma mais proeminente
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle,      setErrorTitle]      = useState("");
  const [errorMessage,    setErrorMessage]    = useState("");

  const isClusterScoped = resourceType ? CLUSTER_SCOPED.includes(resourceType) : false;
  const hasChanges = editorValue !== originalYaml && !!originalYaml;

  // ── Helpers de histórico ──────────────────────────────────────────────────

  const initHistory = useCallback((value: string) => {
    historyRef.current = [value];
    setHistoryIndex(0);
    setEditorValue(value);
  }, []);

  const addToHistory = useCallback((value: string) => {
    setHistoryIndex(cur => {
      const newHist = historyRef.current.slice(0, cur + 1);
      if (newHist[newHist.length - 1] !== value) {
        newHist.push(value);
        if (newHist.length > 50) newHist.shift();
      }
      historyRef.current = newHist;
      return newHist.length - 1;
    });
  }, []);

  const handleUndo = useCallback(() => {
    if (!canUndo) return;
    const idx = historyIndex - 1;
    setHistoryIndex(idx);
    setEditorValue(historyRef.current[idx]);
  }, [canUndo, historyIndex]);

  const handleRedo = useCallback(() => {
    if (!canRedo) return;
    const idx = historyIndex + 1;
    setHistoryIndex(idx);
    setEditorValue(historyRef.current[idx]);
  }, [canRedo, historyIndex]);

  // ── Carrega lista de nomes ────────────────────────────────────────────────

  useEffect(() => {
    if (!resourceType) return;
    if (!isClusterScoped && !namespace) return;

    setNameList([]);
    setNameListLoading(true);
    setResourceName("");
    setEditorValue("");
    setOriginalYaml("");
    setYamlError("");

    fetchList(resourceType, cluster, namespace)
      .then(list => setNameList(list))
      .catch(() => {/* silencioso */})
      .finally(() => setNameListLoading(false));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resourceType, namespace, cluster]);

  // ── Carrega YAML ─────────────────────────────────────────────────────────

  useEffect(() => {
    if (!resourceType || !resourceName) return;
    if (!isClusterScoped && !namespace) return;

    setYamlLoading(true);
    setYamlError("");
    setViewMode("editor");

    fetchYaml(resourceType, cluster, namespace, resourceName)
      .then(y => {
        setOriginalYaml(y);
        initHistory(y);
      })
      .catch(err => setYamlError(err?.message ?? "Erro ao carregar YAML"))
      .finally(() => setYamlLoading(false));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resourceType, namespace, resourceName, cluster]);

  // ── Aplica initial quando o painel abre ─────────────────────────────────

  useEffect(() => {
    if (initial) {
      setResourceType(initial.type);
      setNamespace(initial.namespace);
      setResourceName(initial.name);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── Handlers ─────────────────────────────────────────────────────────────

  const handleEditorChange = useCallback((val: string) => setEditorValue(val), []);

  const handleTypeChange = (val: string) => {
    setResourceType(val as ResourceType);
    setNamespace("");
    setResourceName("");
    setNameList([]);
    setOriginalYaml("");
    setEditorValue("");
    setYamlError("");
    historyRef.current = [];
    setHistoryIndex(-1);
    setViewMode("editor");
  };

  const handleNamespaceChange = (val: string) => {
    setNamespace(val);
    setResourceName("");
    setOriginalYaml("");
    setEditorValue("");
    setYamlError("");
    historyRef.current = [];
    setHistoryIndex(-1);
    setViewMode("editor");
  };

  const handleNameChange = (val: string) => {
    setResourceName(val);
    setOriginalYaml("");
    setEditorValue("");
    setYamlError("");
    historyRef.current = [];
    setHistoryIndex(-1);
    setViewMode("editor");
  };

  const handleCancel = () => {
    setEditorValue(originalYaml);
    initHistory(originalYaml);
    setViewMode("editor");
    toast.info("Alterações descartadas");
  };

  const handleReload = async () => {
    if (!resourceType || !resourceName) return;
    setYamlLoading(true);
    try {
      const y = await fetchYaml(resourceType, cluster, namespace, resourceName);
      setOriginalYaml(y);
      initHistory(y);
      setViewMode("editor");
      toast.success("YAML recarregado");
    } catch (err) {
      toast.error("Falha ao recarregar", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setYamlLoading(false);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && !hasChanges) { toast.info("Nenhuma alteração para comparar"); return; }
    setViewMode(mode);
  };

  const handleShowDiffModal = async (fullscreen = false) => {
    if (!resourceType || !hasChanges) return;
    setIsDiffLoading(true);
    try {
      const unified = await diffResource(resourceType, originalYaml, editorValue, resourceName);
      const html = diff2html(unified, { drawFileList: false, matching: "lines", outputFormat: "side-by-side" });
      setDiffHtml(html);
      setDiffFullscreen(fullscreen);
      setDiffModalOpen(true);
    } catch (err) {
      toast.error("Erro ao gerar diff", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleValidate = async () => {
    if (!resourceType || !resourceName) return;
    setIsValidating(true);
    try {
      await validateResource(resourceType, cluster, namespace, editorValue);
      toast.success("Dry-run bem-sucedido", { description: `${namespace ? namespace + "/" : ""}${resourceName}` });
    } catch (err) {
      toast.error("Dry-run falhou", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setIsValidating(false);
    }
  };

  const handleApply = async () => {
    if (!resourceType || !resourceName) return;
    setIsApplying(true);
    setApplyConfirmOpen(false);
    try {
      await applyResource(resourceType, cluster, namespace, resourceName, editorValue, true);
      toast.success(`${RESOURCE_LABELS[resourceType]} aplicado`, {
        description: `${namespace ? namespace + "/" : ""}${resourceName}`,
      });
      // Recarregar após aplicar
      const y = await fetchYaml(resourceType, cluster, namespace, resourceName);
      setOriginalYaml(y);
      initHistory(y);
      setViewMode("editor");
    } catch (err) {
      // Exibir erro em modal dedicado ao invés de toast
      const errorMsg = err instanceof Error ? err.message : String(err);
      const resourceFullName = `${RESOURCE_LABELS[resourceType]} ${namespace ? namespace + "/" : ""}${resourceName}`;

      setErrorTitle(`Falha ao aplicar ${resourceFullName}`);
      setErrorMessage(errorMsg);
      setErrorDialogOpen(true);

      // Toast como fallback visual rápido
      toast.error("Falha ao aplicar recurso", { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  // ── Keyboard handler ─────────────────────────────────────────────────────

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey) {
      if (e.key === "z" && !e.shiftKey) { e.preventDefault(); handleUndo(); }
      else if (e.key === "y" || (e.key === "z" && e.shiftKey)) { e.preventDefault(); handleRedo(); }
      else if (e.key === "s") {
        e.preventDefault();
        if (hasChanges) {
          addToHistory(editorValue);
          toast.success("Checkpoint salvo localmente");
        }
      }
    }
  }, [handleUndo, handleRedo, addToHistory, editorValue, hasChanges]);

  // ── Render ────────────────────────────────────────────────────────────────

  const resourceLoaded = !!originalYaml;

  // Diff modal
  const diffDialogSizeClass = diffFullscreen
    ? "w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none"
    : "max-w-5xl max-h-[85vh]";

  const changes = applyConfirmOpen ? generateCompactDiff(originalYaml, editorValue) : [];

  return (
    <div className="flex flex-col h-full gap-2 min-w-0" onKeyDown={handleKeyDown} tabIndex={-1}>

      {/* ── Seleção de recurso ─────────────────────────────── */}
      <div className="flex items-end gap-2 flex-wrap shrink-0">
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Tipo</Label>
          <Select value={resourceType} onValueChange={handleTypeChange}>
            <SelectTrigger className="h-8 text-xs w-[130px]">
              <SelectValue placeholder="Tipo..." />
            </SelectTrigger>
            <SelectContent>
              {RESOURCE_TYPES.map(([val, lbl]) => (
                <SelectItem key={val} value={val} className="text-xs">{lbl}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {!isClusterScoped && (
          <>
            <ChevronRight className="w-3 h-3 text-muted-foreground mb-1 shrink-0" />
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Namespace</Label>
              <Select value={namespace} onValueChange={handleNamespaceChange} disabled={!resourceType}>
                <SelectTrigger className="h-8 text-xs w-[140px]">
                  <SelectValue placeholder="Namespace..." />
                </SelectTrigger>
                <SelectContent>
                  {namespaces.map(ns => (
                    <SelectItem key={ns} value={ns} className="text-xs">{ns}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </>
        )}

        <ChevronRight className="w-3 h-3 text-muted-foreground mb-1 shrink-0" />
        <div className="flex flex-col gap-1 flex-1 min-w-[150px]">
          <Label className="text-xs text-muted-foreground">
            Recurso
            {nameListLoading && <Loader2 className="w-3 h-3 inline ml-1 animate-spin" />}
          </Label>
          <Select
            value={resourceName}
            onValueChange={handleNameChange}
            disabled={!resourceType || (!isClusterScoped && !namespace) || nameListLoading}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue placeholder={nameList.length === 0 && !nameListLoading ? "Selecione tipo/ns..." : "Nome..."} />
            </SelectTrigger>
            <SelectContent>
              {nameList.map(n => (
                <SelectItem key={n} value={n} className="text-xs font-mono">{n}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {resourceLoaded && (
          <Button variant="ghost" size="sm" className="mb-0.5" onClick={handleReload} disabled={yamlLoading} title="Recarregar YAML do cluster">
            <RefreshCcw className="w-3.5 h-3.5" />
          </Button>
        )}
      </div>

      {/* Badge do recurso selecionado */}
      {resourceName && resourceType && (
        <div className="flex items-center gap-2 shrink-0">
          <Badge variant="secondary" className="text-xs font-mono truncate max-w-xs">
            {RESOURCE_LABELS[resourceType]}/{namespace ? `${namespace}/` : ""}{resourceName}
          </Badge>
          {hasChanges && <Badge variant="outline" className="text-xs text-amber-400 border-amber-400/40">Editado</Badge>}
        </div>
      )}

      {/* ── Toolbar do editor ──────────────────────────────── */}
      {resourceLoaded && (
        <div className="flex items-center justify-between shrink-0">
          <div className="flex items-center gap-2">
            {/* Undo/Redo */}
            <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
              <button
                type="button"
                onClick={handleUndo}
                disabled={!canUndo}
                className={`px-2 py-1 text-xs ${canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"}`}
                title="Desfazer (Ctrl+Z)"
              >
                <Undo2 className="w-3.5 h-3.5" />
              </button>
              <button
                type="button"
                onClick={handleRedo}
                disabled={!canRedo}
                className={`px-2 py-1 text-xs border-l border-border/50 ${canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"}`}
                title="Refazer (Ctrl+Y)"
              >
                <Redo2 className="w-3.5 h-3.5" />
              </button>
            </div>
            {/* Editor / Diff toggle */}
            <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
              <button
                type="button"
                onClick={() => handleToggleView("editor")}
                className={`px-3 py-1 text-xs font-medium ${viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"}`}
              >
                Editor
              </button>
              <button
                type="button"
                onClick={() => handleToggleView("diff")}
                disabled={!hasChanges}
                className={`px-3 py-1 text-xs font-medium ${viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"} ${!hasChanges ? "opacity-40 cursor-not-allowed" : ""}`}
              >
                Diff
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Editor ────────────────────────────────────────── */}
      <div className="flex-1 min-h-0">
        {yamlLoading ? (
          <div className="flex items-center justify-center border border-border/60 rounded-lg h-full">
            <div className="flex flex-col items-center gap-2 text-muted-foreground">
              <Loader2 className="w-6 h-6 animate-spin" />
              <span className="text-sm">Carregando YAML...</span>
            </div>
          </div>
        ) : yamlError ? (
          <div className="flex items-center justify-center border border-red-500/40 rounded-lg h-full bg-red-500/5">
            <div className="flex flex-col items-center gap-2 text-red-400">
              <AlertCircle className="w-6 h-6" />
              <span className="text-sm text-center px-4">{yamlError}</span>
            </div>
          </div>
        ) : !resourceLoaded ? (
          <div className="flex items-center justify-center border border-border/30 rounded-lg h-full border-dashed">
            <div className="flex flex-col items-center gap-2 text-muted-foreground/50">
              <SplitSquareHorizontal className="w-8 h-8" />
              <span className="text-sm">Selecione tipo, namespace e recurso</span>
            </div>
          </div>
        ) : viewMode === "diff" ? (
          <MonacoYamlEditor mode="diff" originalValue={originalYaml} value={editorValue} height={editorHeight} readOnly />
        ) : (
          <MonacoYamlEditor value={editorValue} onChange={handleEditorChange} height={editorHeight} />
        )}
      </div>

      {/* ── Botões de ação ────────────────────────────────── */}
      {resourceLoaded && (
        <div className="flex flex-wrap items-center gap-2 shrink-0 pt-1 border-t border-border/30">
          <Button variant="outline" size="sm"
            onClick={() => handleShowDiffModal(false)}
            disabled={!hasChanges || isDiffLoading}
          >
            {isDiffLoading ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <FileDiff className="w-4 h-4 mr-1" />}
            Visualizar diff
          </Button>
          <ProtectedAction>
            <Button variant="secondary" size="sm" onClick={handleValidate} disabled={isValidating}>
              {isValidating ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-1" />}
              Dry-run
            </Button>
          </ProtectedAction>
          <Button variant="outline" size="sm" onClick={handleCancel} disabled={!hasChanges}>
            <X className="w-4 h-4 mr-1" /> Cancelar
          </Button>
          <ProtectedAction>
            <Button variant="default" size="sm"
              onClick={() => setApplyConfirmOpen(true)}
              disabled={!hasChanges || isApplying}
            >
              {isApplying ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <TriangleAlert className="w-4 h-4 mr-1" />}
              Aplicar
            </Button>
          </ProtectedAction>
        </div>
      )}

      {/* ── Modal de diff de mudanças ────────────────────── */}
      <Dialog open={diffModalOpen} onOpenChange={open => { setDiffModalOpen(open); if (!open) setDiffFullscreen(false); }}>
        <DialogContent className={`bg-background border-border ${diffFullscreen ? "w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none" : "max-w-5xl max-h-[85vh]"}`}>
          <DialogHeader className="border-b border-border pb-4 pr-12">
            <div className="flex items-start justify-between gap-4">
              <div>
                <DialogTitle className="text-xl font-semibold text-primary">Diff Visual — Painel {label}</DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground">
                  Original vs editado • {RESOURCE_LABELS[resourceType as ResourceType]}/{namespace ? `${namespace}/` : ""}{resourceName}
                </DialogDescription>
              </div>
              <Button variant="outline" size="sm" onClick={() => setDiffFullscreen(f => !f)}>
                {diffFullscreen ? <><Minimize2 className="w-4 h-4 mr-1" />Sair tela cheia</> : <><Maximize2 className="w-4 h-4 mr-1" />Tela cheia</>}
              </Button>
            </div>
          </DialogHeader>
          <ScrollArea className={diffFullscreen ? "h-[calc(100vh-8rem)]" : "h-[calc(85vh-8rem)]"}>
            <div className="p-4">
              {diffHtml
                ? <div className="diff2html-dark" dangerouslySetInnerHTML={{ __html: diffHtml }} />
                : <p className="text-muted-foreground text-center py-8">Nenhum diff disponível.</p>
              }
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* ── Modal de confirmação de apply ───────────────── */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent className="max-w-4xl max-h-[90vh] bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">Confirmar aplicação</DialogTitle>
            <DialogDescription>
              Aplica o {resourceType && RESOURCE_LABELS[resourceType]} diretamente no cluster.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {cluster}</p>
              {namespace && <p><span className="text-muted-foreground">Namespace:</span> {namespace}</p>}
              <p><span className="text-muted-foreground">Recurso:</span> {resourceName}</p>
            </div>
            {changes.length > 0 && (
              <div className="space-y-2">
                <p className="font-semibold text-sm">Mudanças detectadas ({changes.length}):</p>
                <div className="max-h-[300px] overflow-y-auto space-y-2 border rounded-lg p-3 bg-muted/10">
                  {changes.map((c, i) => (
                    <div key={i} className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">{c.path}</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-red-300">{c.before}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-green-300">{c.after}</pre>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <p className="text-muted-foreground text-xs">Esta operação não possui rollback automático.</p>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setApplyConfirmOpen(false)}>Cancelar</Button>
            <ProtectedAction>
              <Button variant="default" onClick={handleApply} disabled={isApplying}>
                {isApplying ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <TriangleAlert className="w-4 h-4 mr-2" />}
                Confirmar e Aplicar
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Modal de erro (destaque para analistas) ───────── */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh] bg-background border-destructive/50 border-2">
          <DialogHeader className="border-b border-destructive/30 pb-4">
            <div className="flex items-start gap-3">
              <div className="shrink-0 w-10 h-10 rounded-full bg-destructive/10 flex items-center justify-center">
                <AlertCircle className="w-6 h-6 text-destructive" />
              </div>
              <div className="flex-1 min-w-0">
                <DialogTitle className="text-destructive text-lg font-semibold">
                  {errorTitle}
                </DialogTitle>
                <DialogDescription className="text-muted-foreground text-sm mt-1">
                  Detalhes técnicos do erro abaixo. Verifique conflitos de field managers ou validação de YAML.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="space-y-4">
              {/* Mensagem de erro formatada */}
              <div className="bg-destructive/5 border border-destructive/20 rounded-lg p-4">
                <div className="flex items-center gap-2 mb-3">
                  <TriangleAlert className="w-4 h-4 text-destructive" />
                  <span className="text-sm font-semibold text-destructive">Erro Detalhado</span>
                </div>
                <pre className="text-xs font-mono text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">
                  {errorMessage}
                </pre>
              </div>

              {/* Dicas de resolução (se erro contiver palavras-chave conhecidas) */}
              {errorMessage.includes("conflicts with") && (
                <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-semibold text-blue-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Este erro indica conflito de <strong>field manager</strong> (Server-Side Apply).
                    O recurso foi previamente aplicado com <code className="bg-muted px-1 rounded">kubectl apply</code> (client-side)
                    e agora está sendo aplicado via SSA com field manager diferente.
                  </p>
                  <p className="text-xs text-muted-foreground mt-2 leading-relaxed">
                    <strong>Ação recomendada:</strong> O backend já utiliza <code className="bg-muted px-1 rounded">--force=true</code>.
                    Se o erro persistir, verifique se há anotações <code className="bg-muted px-1 rounded">kubectl.kubernetes.io/*</code>
                    que precisam ser removidas manualmente do YAML antes de aplicar.
                  </p>
                </div>
              )}

              {errorMessage.includes("illegal base64") && (
                <div className="bg-yellow-500/5 border border-yellow-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-yellow-400" />
                    <span className="text-sm font-semibold text-yellow-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Erro de validação de base64 no campo <code className="bg-muted px-1 rounded">data</code> do Secret.
                    Verifique se os valores não foram editados manualmente ou se há caracteres inválidos (espaços, quebras de linha).
                  </p>
                </div>
              )}
            </div>
          </ScrollArea>

          <DialogFooter className="border-t border-border/50 pt-4">
            <Button variant="outline" onClick={() => setErrorDialogOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─── Modal principal ──────────────────────────────────────────────────────────

export function ResourceCompareModal({ open, onClose, cluster, initialLeft }: ResourceCompareModalProps) {
  const [namespaces, setNamespaces] = useState<string[]>([]);

  // Cross-cluster
  const { clusters: allClusters } = useClusters();
  const [crossCluster, setCrossCluster]       = useState(false);
  const [rightCluster, setRightCluster]       = useState(cluster);
  const [rightNamespaces, setRightNamespaces] = useState<string[]>([]);

  // Altura dos editores dentro do modal fullscreen
  const EDITOR_HEIGHT = "calc(100vh - 310px)";

  // Carrega namespaces do cluster esquerdo ao abrir
  useEffect(() => {
    if (!open || !cluster) return;
    apiClient.getNamespaces(cluster)
      .then(list => setNamespaces(list.map(n => n.name)))
      .catch(() => {});
  }, [open, cluster]);

  // Sincronizar rightCluster quando switch está OFF ou cluster esquerdo muda
  useEffect(() => {
    if (!crossCluster) setRightCluster(cluster);
  }, [cluster, crossCluster]);

  // Carregar namespaces do cluster direito
  useEffect(() => {
    if (!open || !rightCluster) return;
    if (!crossCluster) { setRightNamespaces([]); return; }
    apiClient.getNamespaces(rightCluster)
      .then(list => setRightNamespaces(list.map(n => n.name)))
      .catch(() => setRightNamespaces([]));
  }, [open, rightCluster, crossCluster]);

  const handleClose = () => {
    setCrossCluster(false);
    setRightCluster(cluster);
    setRightNamespaces([]);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={val => { if (!val) handleClose(); }}>
      <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0 bg-background border-border flex flex-col">

        {/* Header */}
        <div className="flex-none flex items-center justify-between px-6 py-3 border-b border-border bg-muted/30">
          <div className="flex items-center gap-3 flex-wrap">
            <SplitSquareHorizontal className="w-5 h-5 text-primary shrink-0" />
            <h2 className="text-base font-semibold shrink-0">Edição Lado a Lado</h2>
            {cluster && <Badge variant="outline" className="text-xs font-mono">{cluster}</Badge>}

            {/* Switch cross-cluster */}
            <div className="flex items-center gap-2 ml-2 pl-4 border-l border-border/50">
              <Switch
                id="cross-cluster"
                checked={crossCluster}
                onCheckedChange={(val) => {
                  setCrossCluster(val);
                  if (!val) setRightCluster(cluster);
                }}
              />
              <Label htmlFor="cross-cluster" className="text-xs cursor-pointer text-muted-foreground whitespace-nowrap">
                Clusters diferentes
              </Label>
              {crossCluster && (
                <Select value={rightCluster} onValueChange={setRightCluster}>
                  <SelectTrigger className="h-7 text-xs w-60 font-mono">
                    <SelectValue placeholder="Selecione cluster direito..." />
                  </SelectTrigger>
                  <SelectContent>
                    {allClusters.map(c => (
                      <SelectItem key={c.name} value={c.name} className="text-xs font-mono">
                        {c.name}
                        {c.name === cluster && (
                          <span className="ml-2 text-muted-foreground text-xs">(esquerdo)</span>
                        )}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
          </div>
          <Button variant="ghost" size="sm" onClick={handleClose} title="Fechar">
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Dois painéis independentes */}
        <div className="flex-1 min-h-0 grid grid-cols-2">
          {/* Painel Esquerdo */}
          <div className="flex flex-col h-full overflow-hidden border-r border-border">
            <div className="shrink-0 px-4 pt-3 pb-1 border-b border-border/40 bg-muted/10 flex items-center gap-2">
              <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Painel Esquerdo</span>
              <Badge variant="secondary" className="text-xs font-mono">{cluster}</Badge>
            </div>
            <div className="flex-1 min-h-0 p-4 overflow-hidden">
              <ResourceEditPanel
                key={`left-${open}`}
                label="Esquerdo"
                cluster={cluster}
                namespaces={namespaces}
                initial={initialLeft}
                editorHeight={EDITOR_HEIGHT}
              />
            </div>
          </div>

          {/* Painel Direito */}
          <div className="flex flex-col h-full overflow-hidden">
            <div className="shrink-0 px-4 pt-3 pb-1 border-b border-border/40 bg-muted/10 flex items-center gap-2">
              <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Painel Direito</span>
              <Badge
                variant="secondary"
                className={`text-xs font-mono ${crossCluster && rightCluster !== cluster ? "border border-amber-400/40 text-amber-400" : ""}`}
              >
                {crossCluster ? rightCluster : cluster}
              </Badge>
              {crossCluster && rightCluster !== cluster && (
                <Badge variant="outline" className="text-xs text-amber-400 border-amber-400/40">
                  cluster diferente
                </Badge>
              )}
            </div>
            <div className="flex-1 min-h-0 p-4 overflow-hidden">
              <ResourceEditPanel
                key={`right-${open}-${crossCluster ? rightCluster : cluster}`}
                label="Direito"
                cluster={crossCluster ? rightCluster : cluster}
                namespaces={crossCluster ? rightNamespaces : namespaces}
                editorHeight={EDITOR_HEIGHT}
              />
            </div>
          </div>
        </div>

      </DialogContent>
    </Dialog>
  );
}
