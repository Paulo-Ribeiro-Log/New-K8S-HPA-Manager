import { useState, useEffect, useCallback, useRef } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { ProtectedAction } from "@/components/rbac";
import { apiClient } from "@/lib/api/client";
import { isPodImmutableFieldsError, POD_IMMUTABLE_FIELDS_HINT } from "@/lib/podErrors";
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
  ChevronsUpDown,
  Check,
} from "lucide-react";
import { toast } from "sonner";
import type { Cluster } from "@/lib/api/types";

// ─── Tipos ────────────────────────────────────────────────────────────────────

export type ResourceType =
  | "deployment"
  | "configmap"
  | "secret"
  | "daemonset"
  | "statefulset"
  | "ingress"
  | "namespace"
  | "pod"
  | "gateway";

export interface CompareInitial {
  type: ResourceType;
  namespace: string;
  name: string;
}

interface ResourceCompareModalProps {
  open: boolean;
  onClose: () => void;
  cluster: string;
  clusters: Cluster[];
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
  gateway:     "Gateway",
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
    case "gateway":     return (await apiClient.getGateway(cluster, namespace, "gateway", name)).yaml;
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
    case "gateway":     return (await apiClient.getGateways(cluster, namespace, "gateway", true)).map(g => g.name);
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
    case "gateway":     resp = await apiClient.diffGateway(originalYaml, updatedYaml, name); break;
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
    case "gateway":     await apiClient.validateGateway({ cluster, namespace, yaml: yamlContent, kind: "gateway" }); break;
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
    case "gateway":     await apiClient.applyGateway(cluster, namespace, "gateway", name, body); break;
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

// PanelSnapshot — resumo do estado ATUAL de um ResourceEditPanel, reportado pro modal pai
// (ResourceCompareModal) via onStateChange. Existe pra viabilizar o "Diff Esquerdo × Direito"
// (botão ao lado do switch "Clusters diferentes"): os dois painéis são instâncias TOTALMENTE
// independentes (cada um com seu próprio useState de tipo/namespace/nome/YAML) — sem isso, o
// componente pai não tem como saber se os dois lados estão com o MESMO tipo de recurso carregado,
// nem acesso ao conteúdo YAML de cada um pra montar o diff.
export interface PanelSnapshot {
  resourceType: ResourceType | "";
  namespace: string;
  resourceName: string;
  yaml: string; // editorValue — reflete edições ao vivo no painel, não só o YAML original carregado
  loaded: boolean;
}

interface ResourceEditPanelProps {
  label: "Esquerdo" | "Direito";
  cluster: string;
  initial?: CompareInitial;
  editorHeight: string;
  onStateChange?: (snapshot: PanelSnapshot) => void;
}

function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => {
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [onDrag]);

  return (
    <div
      className="w-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-col-resize transition-colors"
      onMouseDown={(e) => {
        dragging.current = true;
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    />
  );
}

function ResourceEditPanel({ label, cluster, initial, editorHeight, onStateChange }: ResourceEditPanelProps) {
  // Namespaces do cluster deste painel (buscados internamente)
  const [namespaces,       setNamespaces]       = useState<string[]>([]);
  const [nsLoading,        setNsLoading]        = useState(false);

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

  // ── Busca namespaces do próprio cluster ──────────────────────────────────
  useEffect(() => {
    if (!cluster) return;
    let cancelled = false;
    setNsLoading(true);
    apiClient.getNamespaces(cluster)
      .then(list => { if (!cancelled) setNamespaces(list.map(n => n.name)); })
      .catch(() => { if (!cancelled) setNamespaces([]); })
      .finally(() => { if (!cancelled) setNsLoading(false); });
    return () => { cancelled = true; };
  }, [cluster]);

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

  // Reporta o estado atual pro modal pai (ver PanelSnapshot) — habilita o botão "Diff Esquerdo ×
  // Direito" só quando os dois painéis têm o MESMO tipo de recurso carregado.
  useEffect(() => {
    onStateChange?.({ resourceType, namespace, resourceName, yaml: editorValue, loaded: resourceLoaded });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resourceType, namespace, resourceName, editorValue, resourceLoaded]);

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
              <Label className="text-xs text-muted-foreground">
                Namespace
                {nsLoading && <Loader2 className="w-3 h-3 inline ml-1 animate-spin" />}
              </Label>
              <Select value={namespace} onValueChange={handleNamespaceChange} disabled={!resourceType || nsLoading}>
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

              {isPodImmutableFieldsError(errorMessage) && (
                <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-semibold text-blue-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Isso não é um conflito de field manager — é uma regra do próprio Kubernetes:
                    um Pod já criado só permite alterar a imagem do container (e poucos outros campos).
                    {" "}{POD_IMMUTABLE_FIELDS_HINT}
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

export function ResourceCompareModal({ open, onClose, cluster, clusters: allClusters, initialLeft }: ResourceCompareModalProps) {
  // Cross-cluster
  const [crossCluster, setCrossCluster]               = useState(false);
  const [rightCluster, setRightCluster]               = useState(cluster);
  const [isRightContextSwitching, setIsRightCtxSwitch] = useState(false);
  const [clusterComboOpen, setClusterComboOpen]        = useState(false);

  // Largura do painel esquerdo (resize dinâmico)
  const [leftWidth, setLeftWidth] = useState<number | null>(null);

  // Diff entre os dois painéis (Esquerdo × Direito) — pedido explícito do usuário: habilitar um
  // diff direto entre as duas janelas quando o TIPO de recurso selecionado é o mesmo nos dois
  // lados (ex: os dois com Deployment carregado, mesmo que sejam nomes/namespaces/clusters
  // diferentes — é justamente o caso de uso, comparar a mesma peça em dois lugares). Cada painel
  // reporta seu estado (PanelSnapshot) via onStateChange sempre que tipo/namespace/nome/conteúdo
  // mudam — os dois React.useState abaixo guardam o snapshot mais recente de cada lado.
  const [leftPanelState,  setLeftPanelState]  = useState<PanelSnapshot | null>(null);
  const [rightPanelState, setRightPanelState] = useState<PanelSnapshot | null>(null);
  const [panelsDiffOpen,  setPanelsDiffOpen]  = useState(false);
  const canDiffPanels = !!(
    leftPanelState?.loaded && rightPanelState?.loaded &&
    leftPanelState.resourceType && leftPanelState.resourceType === rightPanelState.resourceType
  );

  // Altura dos editores dentro do modal fullscreen
  const EDITOR_HEIGHT = "calc(100vh - 310px)";

  // Sincronizar rightCluster quando switch está OFF ou cluster esquerdo muda
  useEffect(() => {
    if (!crossCluster) setRightCluster(cluster);
  }, [cluster, crossCluster]);

  // Troca o contexto do painel direito (igual ao handleClusterChange do Index.tsx)
  const handleRightClusterChange = async (newCluster: string) => {
    if (newCluster === rightCluster) return;
    setIsRightCtxSwitch(true);
    try {
      await apiClient.switchContext(newCluster);
      setRightCluster(newCluster);
      toast.success(`Painel direito: contexto alterado para ${newCluster}`);
    } catch (err) {
      toast.error("Erro ao alterar contexto do painel direito", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsRightCtxSwitch(false);
    }
  };

  const handleClose = () => {
    setCrossCluster(false);
    setRightCluster(cluster);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={val => { if (!val) handleClose(); }}>
      <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0 bg-background border-border flex flex-col [&>button:last-child]:hidden">

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
                <Popover open={clusterComboOpen} onOpenChange={setClusterComboOpen}>
                  <PopoverTrigger asChild>
                    <Button
                      variant="outline"
                      role="combobox"
                      aria-expanded={clusterComboOpen}
                      disabled={isRightContextSwitching}
                      className="h-7 text-xs w-64 font-mono justify-between px-2"
                    >
                      {isRightContextSwitching ? (
                        <span className="flex items-center gap-1.5">
                          <Loader2 className="w-3 h-3 animate-spin" />
                          Trocando contexto...
                        </span>
                      ) : (
                        <span className="truncate">
                          {rightCluster || "Selecione cluster direito..."}
                        </span>
                      )}
                      <ChevronsUpDown className="ml-1 h-3 w-3 shrink-0 opacity-50" />
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-80 p-0" align="start">
                    <Command>
                      <CommandInput placeholder="Buscar cluster..." className="text-xs h-8" />
                      <CommandList>
                        <CommandEmpty className="text-xs py-3 text-center text-muted-foreground">
                          Nenhum cluster encontrado.
                        </CommandEmpty>
                        <CommandGroup>
                          {allClusters.map(c => (
                            <CommandItem
                              key={c.context}
                              value={c.context}
                              onSelect={(val) => {
                                handleRightClusterChange(val);
                                setClusterComboOpen(false);
                              }}
                              className="text-xs font-mono"
                            >
                              <Check
                                className={`mr-2 h-3 w-3 shrink-0 ${rightCluster === c.context ? "opacity-100" : "opacity-0"}`}
                              />
                              <span className="truncate">{c.context}</span>
                              {c.context === cluster && (
                                <span className="ml-auto text-muted-foreground text-xs shrink-0">(esquerdo)</span>
                              )}
                            </CommandItem>
                          ))}
                        </CommandGroup>
                      </CommandList>
                    </Command>
                  </PopoverContent>
                </Popover>
              )}
            </div>

            {/* Diff Esquerdo × Direito — só habilitado quando os dois painéis têm o MESMO tipo
                de recurso carregado (ResourceEditPanel.onStateChange mantém leftPanelState/
                rightPanelState sempre atualizados). */}
            <div className="flex items-center gap-2 ml-2 pl-4 border-l border-border/50">
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                disabled={!canDiffPanels}
                onClick={() => setPanelsDiffOpen(true)}
                title={canDiffPanels
                  ? "Comparar o conteúdo dos dois painéis"
                  : "Selecione o mesmo tipo de recurso, já carregado, nos dois painéis"}
              >
                <FileDiff className="w-3.5 h-3.5 mr-1" />
                Diff Esquerdo × Direito
              </Button>
            </div>
          </div>
          <Button variant="ghost" size="sm" onClick={handleClose} title="Fechar">
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Dois painéis independentes */}
        <div className="flex-1 min-h-0 flex flex-row overflow-hidden">
          {/* Painel Esquerdo */}
          <div
            className="flex flex-col h-full overflow-hidden flex-shrink-0"
            style={{ width: leftWidth !== null ? leftWidth : "50%" }}
          >
            <div className="shrink-0 px-4 pt-3 pb-1 border-b border-border/40 bg-muted/10 flex items-center gap-2">
              <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Painel Esquerdo</span>
              <Badge variant="secondary" className="text-xs font-mono">{cluster}</Badge>
            </div>
            <div className="flex-1 min-h-0 p-4 overflow-hidden">
              <ResourceEditPanel
                key={`left-${open}`}
                label="Esquerdo"
                cluster={cluster}
                initial={initialLeft}
                editorHeight={EDITOR_HEIGHT}
                onStateChange={setLeftPanelState}
              />
            </div>
          </div>

          <ResizeDivider
            onDrag={(d) =>
              setLeftWidth((w) => {
                const current = w ?? window.innerWidth / 2;
                return Math.max(300, Math.min(window.innerWidth - 300, current + d));
              })
            }
          />

          {/* Painel Direito */}
          <div className="flex flex-col h-full overflow-hidden flex-1 min-w-0">
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
                editorHeight={EDITOR_HEIGHT}
                onStateChange={setRightPanelState}
              />
            </div>
          </div>
        </div>

        {/* ── Diff Esquerdo × Direito ─────────────────────── */}
        <Dialog open={panelsDiffOpen} onOpenChange={setPanelsDiffOpen}>
          <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none bg-background border-border flex flex-col">
            <DialogHeader className="border-b border-border pb-4 pr-12 shrink-0">
              <DialogTitle className="text-xl font-semibold text-primary">
                Diff — Painel Esquerdo × Painel Direito
              </DialogTitle>
              <DialogDescription className="text-sm text-muted-foreground">
                {leftPanelState?.resourceType && RESOURCE_LABELS[leftPanelState.resourceType as ResourceType]}
                {" • "}
                Esquerdo: {leftPanelState?.namespace ? `${leftPanelState.namespace}/` : ""}{leftPanelState?.resourceName} ({cluster})
                {" vs "}
                Direito: {rightPanelState?.namespace ? `${rightPanelState.namespace}/` : ""}{rightPanelState?.resourceName} ({crossCluster ? rightCluster : cluster})
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 min-h-0 p-4">
              {leftPanelState && rightPanelState && (
                <MonacoYamlEditor
                  mode="diff"
                  originalValue={leftPanelState.yaml}
                  value={rightPanelState.yaml}
                  height="calc(100vh - 180px)"
                  readOnly
                />
              )}
            </div>
          </DialogContent>
        </Dialog>

      </DialogContent>
    </Dialog>
  );
}
