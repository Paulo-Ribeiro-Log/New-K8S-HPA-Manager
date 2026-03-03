import { useEffect, useMemo, useState, useCallback, useRef } from "react";
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Search,
  RefreshCcw,
  Eye,
  EyeOff,
  CheckCircle2,
  TriangleAlert,
  FileDiff,
  Loader2,
  Undo2,
  Redo2,
  Maximize2,
  Minimize2,
  X,
  FileText,
  MoreVertical,
  Trash2,
  SplitSquareHorizontal,
  ChevronsUpDown,
  Check,
  Info,
} from "lucide-react";
import { toast } from "sonner";

import type { Namespace, APIResourceInfo, GenericResourceSummary, GenericResourceManifest } from "@/lib/api/types";
import { useAPIResources, useGenericResources } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface ResourceExplorerTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

// Agrupa recursos: first built-in, then CRDs
function groupResources(resources: APIResourceInfo[]) {
  const builtIn: APIResourceInfo[] = [];
  const crds: APIResourceInfo[] = [];
  for (const r of resources) {
    if (!r.group || r.group === "") {
      builtIn.push(r);
    } else {
      crds.push(r);
    }
  }
  return { builtIn, crds };
}

function resourceLabel(r: APIResourceInfo) {
  return r.group ? `${r.kind} · ${r.group}` : r.kind;
}

export const ResourceExplorerTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: ResourceExplorerTabProps) => {
  // ── Resource type selection (combobox) ─────────────────────────────────
  const [comboOpen, setComboOpen] = useState(false);
  const [selectedResource, setSelectedResource] = useState<APIResourceInfo | null>(null);

  // ── Item list state ─────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedItem, setSelectedItem] = useState<GenericResourceSummary | null>(null);

  // ── Editor state ────────────────────────────────────────────────────────
  const [manifest, setManifest] = useState<GenericResourceManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");

  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const historyCache = useRef<Map<string, { history: string[]; index: number }>>(new Map());
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  // ── Data hooks ─────────────────────────────────────────────────────────
  const { resources, loading: resourcesLoading, error: resourcesError } = useAPIResources(cluster);

  const namespaceFilter = useMemo(() => {
    if (!selectedResource?.namespaced) return undefined;
    return selectedNamespace || undefined;
  }, [selectedResource, selectedNamespace]);

  const {
    items,
    loading: itemsLoading,
    error: itemsError,
    refetch: refetchItems,
  } = useGenericResources(
    cluster,
    selectedResource?.name,
    selectedResource?.group,
    namespaceFilter,
  );

  // ── Filtered namespaces ────────────────────────────────────────────────
  const filteredNamespaces = useMemo(() => {
    if (!namespaces) return [];
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) onNamespaceChange("");
  }, [filteredNamespaces]);

  // ── Grouped resources for combobox ─────────────────────────────────────
  const { builtIn, crds } = useMemo(() => groupResources(resources), [resources]);

  // ── Reset on cluster change ────────────────────────────────────────────
  useEffect(() => {
    setSelectedResource(null);
    setSelectedItem(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster]);

  // ── Reset selected item when resource type changes ─────────────────────
  useEffect(() => {
    setSelectedItem(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
  }, [selectedResource]);

  useEffect(() => {
    if (resourcesError) toast.error("Erro ao carregar tipos de recursos", { description: resourcesError });
  }, [resourcesError]);

  useEffect(() => {
    if (itemsError) toast.error("Erro ao listar recursos", { description: itemsError });
  }, [itemsError]);

  // ── Filtered items ─────────────────────────────────────────────────────
  const filteredItems = useMemo(() => {
    if (!items) return [];
    if (!searchQuery) return items;
    const q = searchQuery.toLowerCase();
    return items.filter(
      (item) =>
        item.name.toLowerCase().includes(q) ||
        item.namespace.toLowerCase().includes(q)
    );
  }, [items, searchQuery]);

  // ── Select item → load manifest ────────────────────────────────────────
  const handleSelectItem = async (item: GenericResourceSummary) => {
    if (selectedItem && history.length > 0) {
      const key = `${item.namespace}/${item.name}`;
      historyCache.current.set(key, { history: [...history], index: historyIndex });
    }
    setSelectedItem(item);
    setManifestLoading(true);
    setManifest(null);
    try {
      const detail = await apiClient.getGenericResourceYAML(
        cluster,
        item.namespace,
        selectedResource!.name,
        item.name,
        selectedResource?.group,
      );
      setManifest(detail);
      const yaml = detail.yaml || "";
      setOriginalYaml(yaml);
      setViewMode("editor");
      const key = `${item.namespace}/${item.name}`;
      const cached = historyCache.current.get(key);
      if (cached) {
        setHistory(cached.history);
        setHistoryIndex(cached.index);
        setEditorValue(cached.history[cached.index]);
      } else {
        setHistory([yaml]);
        setHistoryIndex(0);
        setEditorValue(yaml);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // ── Editor helpers ─────────────────────────────────────────────────────
  const historyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const addToHistory = useCallback(
    (value: string) => {
      setHistory((prev) => {
        const newHistory = prev.slice(0, historyIndex + 1);
        if (newHistory[newHistory.length - 1] === value) return prev;
        const updated = [...newHistory, value].slice(-50);
        setHistoryIndex(updated.length - 1);
        return updated;
      });
    },
    [historyIndex],
  );

  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
    if (historyTimer.current) clearTimeout(historyTimer.current);
    historyTimer.current = setTimeout(() => addToHistory(value), 1000);
  }, [addToHistory]);

  const handleUndo = useCallback(() => {
    if (historyIndex <= 0) return;
    const newIndex = historyIndex - 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
  }, [history, historyIndex]);

  const handleRedo = useCallback(() => {
    if (historyIndex >= history.length - 1) return;
    const newIndex = historyIndex + 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
  }, [history, historyIndex]);

  const refreshManifest = async () => {
    if (!selectedItem || !selectedResource) return;
    setManifestLoading(true);
    try {
      const detail = await apiClient.getGenericResourceYAML(
        cluster,
        selectedItem.namespace,
        selectedResource.name,
        selectedItem.name,
        selectedResource.group,
      );
      setManifest(detail);
      const yaml = detail.yaml || "";
      setOriginalYaml(yaml);
      setEditorValue(yaml);
      setHistory([yaml]);
      setHistoryIndex(0);
      toast.success("Manifesto recarregado do cluster");
    } catch (err) {
      toast.error("Erro ao recarregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // ── Validate (dry-run) ─────────────────────────────────────────────────
  const handleValidate = async () => {
    if (!selectedItem || !selectedResource) return;
    setIsValidating(true);
    try {
      await apiClient.validateGenericResource(cluster, selectedItem.namespace, editorValue);
      toast.success("YAML válido", { description: "Validação dry-run bem-sucedida" });
    } catch (err) {
      toast.error("Erro de validação", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setIsValidating(false);
    }
  };

  // ── Diff ───────────────────────────────────────────────────────────────
  const handleShowDiff = async () => {
    if (!selectedItem) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffGenericResource(
        originalYaml,
        editorValue,
        `${selectedItem.namespace}/${selectedItem.name}`,
      );
      const html = diff2html(result.unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffModalOpen(true);
    } catch (err) {
      toast.error("Erro ao gerar diff", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  // ── Apply ──────────────────────────────────────────────────────────────
  const handleApply = async () => {
    if (!selectedItem || !selectedResource) return;
    setIsApplying(true);
    try {
      await apiClient.applyGenericResource(
        cluster,
        selectedItem.namespace,
        selectedResource.name,
        selectedItem.name,
        { yaml: editorValue, dryRun: false, force: true },
      );
      toast.success("Recurso aplicado com sucesso");
      setOriginalYaml(editorValue);
      addToHistory(editorValue);
      setApplyConfirmOpen(false);
      refetchItems();
    } catch (err) {
      setApplyConfirmOpen(false);
      const msg = err instanceof Error ? err.message : String(err);
      setErrorTitle("Erro ao aplicar recurso");
      setErrorMessage(msg);
      setErrorDialogOpen(true);
    } finally {
      setIsApplying(false);
    }
  };

  // ── Describe ───────────────────────────────────────────────────────────
  const handleDescribe = async () => {
    if (!selectedItem || !selectedResource) return;
    setDescribeLoading(true);
    setDescribeModalOpen(true);
    try {
      const result = await apiClient.describeGenericResource(
        cluster,
        selectedItem.namespace,
        selectedResource.name,
        selectedItem.name,
      );
      setDescribeContent(result.describe);
    } catch (err) {
      setDescribeContent(`Erro: ${err instanceof Error ? err.message : "Erro desconhecido"}`);
    } finally {
      setDescribeLoading(false);
    }
  };

  // ── Delete ─────────────────────────────────────────────────────────────
  const handleDelete = async () => {
    if (!selectedItem || !selectedResource) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteGenericResource(
        cluster,
        selectedItem.namespace,
        selectedResource.name,
        selectedItem.name,
        selectedResource.group,
      );
      toast.success(`${selectedItem.name} deletado com sucesso`);
      setDeleteConfirmOpen(false);
      setSelectedItem(null);
      setManifest(null);
      setEditorValue("");
      refetchItems();
    } catch (err) {
      toast.error("Erro ao deletar recurso", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  const hasChanges = editorValue !== originalYaml;
  void manifest;

  // ── Toolbar shared between normal and fullscreen editors ────────────────
  const editorToolbar = (
    <div className="flex items-center gap-1">
      <Button
        variant="outline" size="sm"
        onClick={handleUndo}
        disabled={historyIndex <= 0}
        title="Desfazer"
      >
        <Undo2 className="w-4 h-4" />
      </Button>
      <Button
        variant="outline" size="sm"
        onClick={handleRedo}
        disabled={historyIndex >= history.length - 1}
        title="Refazer"
      >
        <Redo2 className="w-4 h-4" />
      </Button>
      <Button
        variant={viewMode === "diff" ? "secondary" : "outline"}
        size="sm"
        onClick={() => setViewMode(viewMode === "editor" ? "diff" : "editor")}
        title="Alternar Editor/Diff"
      >
        <SplitSquareHorizontal className="w-4 h-4" />
      </Button>
      <Button
        variant="outline" size="sm"
        onClick={handleShowDiff}
        disabled={isDiffLoading || !hasChanges}
        title="Visualizar diff"
      >
        {isDiffLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <FileDiff className="w-4 h-4" />}
      </Button>
      <Button
        variant="outline" size="sm"
        onClick={handleValidate}
        disabled={isValidating}
        title="Validar (dry-run)"
      >
        {isValidating
          ? <Loader2 className="w-4 h-4 animate-spin" />
          : <CheckCircle2 className="w-4 h-4" />}
      </Button>
      <Button
        variant="outline" size="sm"
        onClick={refreshManifest}
        disabled={!selectedItem || manifestLoading}
        title="Recarregar YAML do cluster"
      >
        {manifestLoading
          ? <Loader2 className="w-4 h-4 animate-spin" />
          : <RefreshCcw className="w-4 h-4" />}
      </Button>
      <Button
        variant="outline" size="sm"
        onClick={() => setEditorValue(originalYaml)}
        disabled={!hasChanges}
        title="Cancelar alterações"
      >
        <X className="w-4 h-4" />
        <span className="ml-1 hidden sm:inline">Cancelar</span>
      </Button>
      <Button
        variant={hasChanges ? "default" : "outline"}
        size="sm"
        onClick={() => setApplyConfirmOpen(true)}
        disabled={!hasChanges || isApplying}
        title="Aplicar"
      >
        {isApplying
          ? <Loader2 className="w-4 h-4 animate-spin" />
          : <TriangleAlert className="w-4 h-4" />}
        <span className="ml-1 hidden sm:inline">Aplicar</span>
      </Button>
    </div>
  );

  // ── Left panel ─────────────────────────────────────────────────────────
  const leftTitleAction = (
    <div className="flex items-center gap-2">
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4 mr-1" /> : <EyeOff className="w-4 h-4 mr-1" />}
        Sistema
      </Button>
      <Button variant="outline" size="sm" onClick={refetchItems} disabled={itemsLoading || !selectedResource}>
        <RefreshCcw className={`w-4 h-4 mr-1 ${itemsLoading ? "animate-spin" : ""}`} />
        Atualizar
      </Button>
    </div>
  );

  const leftContent = (
    <div className="space-y-3">
      {/* Combobox: resource type selector */}
      <Popover open={comboOpen} onOpenChange={setComboOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={comboOpen}
            className="w-full justify-between h-8 text-sm font-normal"
          >
            {selectedResource ? resourceLabel(selectedResource) : "Selecionar tipo de recurso..."}
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[320px] p-0" align="start">
          <Command>
            <CommandInput placeholder="Buscar tipo (ex: Pod, ExternalSecret)..." />
            <CommandList>
              {resourcesLoading ? (
                <div className="flex justify-center py-4">
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
              ) : (
                <>
                  <CommandEmpty>Nenhum tipo encontrado.</CommandEmpty>
                  {builtIn.length > 0 && (
                    <CommandGroup heading="Recursos Built-in">
                      {builtIn.map((r) => (
                        <CommandItem
                          key={`${r.name}.${r.group}`}
                          value={resourceLabel(r)}
                          onSelect={() => {
                            setSelectedResource(r);
                            setComboOpen(false);
                          }}
                        >
                          <Check
                            className={`mr-2 h-4 w-4 ${
                              selectedResource?.name === r.name && selectedResource?.group === r.group
                                ? "opacity-100"
                                : "opacity-0"
                            }`}
                          />
                          {r.kind}
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  )}
                  {crds.length > 0 && (
                    <CommandGroup heading="CRDs">
                      {crds.map((r) => (
                        <CommandItem
                          key={`${r.name}.${r.group}`}
                          value={resourceLabel(r)}
                          onSelect={() => {
                            setSelectedResource(r);
                            setComboOpen(false);
                          }}
                        >
                          <Check
                            className={`mr-2 h-4 w-4 ${
                              selectedResource?.name === r.name && selectedResource?.group === r.group
                                ? "opacity-100"
                                : "opacity-0"
                            }`}
                          />
                          {resourceLabel(r)}
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  )}
                </>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      {/* Namespace selector (only for namespaced resources) */}
      {selectedResource?.namespaced && (
        <Select
          value={selectedNamespace || "__all__"}
          onValueChange={(v) => onNamespaceChange(v === "__all__" ? "" : v)}
        >
          <SelectTrigger className="h-8 w-full">
            <SelectValue placeholder="Todos os namespaces" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">Todos os namespaces</SelectItem>
            {filteredNamespaces.map((ns) => (
              <SelectItem key={ns.name} value={ns.name}>{ns.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      {/* Search */}
      {selectedResource && (
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder={`Buscar ${selectedResource.kind}...`}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-8 h-8"
          />
          {searchQuery && (
            <Button
              variant="ghost" size="icon"
              className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6"
              onClick={() => setSearchQuery("")}
            >
              <X className="h-3 w-3" />
            </Button>
          )}
        </div>
      )}

      {/* Item list */}
      {!selectedResource ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center text-muted-foreground">
          <Search className="h-10 w-10 opacity-30" />
          <p className="text-sm font-medium">Selecione um tipo de recurso</p>
          <p className="text-xs opacity-70">Use o seletor acima para buscar qualquer recurso Kubernetes (built-in ou CRD)</p>
        </div>
      ) : itemsLoading ? (
        <div className="flex justify-center py-6">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : itemsError ? (
        <div className="flex flex-col items-center gap-2 py-6 text-center text-muted-foreground">
          <Info className="h-8 w-8 opacity-50" />
          <p className="text-sm font-medium">Erro ao listar recursos</p>
          <p className="text-xs">{itemsError}</p>
        </div>
      ) : filteredItems.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-6 text-muted-foreground">
          <Search className="h-8 w-8 opacity-40" />
          <p className="text-sm">Nenhum {selectedResource.kind} encontrado</p>
        </div>
      ) : (
        <>
          <div className="text-xs text-muted-foreground">
            {filteredItems.length} recurso(s) encontrado(s)
          </div>
          <div className="space-y-1">
            {filteredItems.map((item) => {
              const isSelected =
                selectedItem?.name === item.name &&
                selectedItem?.namespace === item.namespace;
              return (
                <div
                  key={`${item.namespace}/${item.name}`}
                  className={`p-2 rounded-md cursor-pointer transition-colors ${
                    isSelected ? "bg-accent" : "hover:bg-muted"
                  }`}
                  onClick={() => handleSelectItem(item)}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <span className="font-medium text-sm truncate block">{item.name}</span>
                      {item.namespace && (
                        <span className="text-xs text-muted-foreground">{item.namespace}</span>
                      )}
                    </div>
                    <div className="flex flex-col items-end gap-1 shrink-0">
                      {item.age && (
                        <span className="text-xs text-muted-foreground">{item.age}</span>
                      )}
                      {item.additionalColumns?.status && (
                        <span className="px-1.5 py-0.5 rounded text-xs bg-blue-500/20 text-blue-400 border border-blue-500/30">
                          {item.additionalColumns.status}
                        </span>
                      )}
                      {item.additionalColumns?.phase && (
                        <span className="px-1.5 py-0.5 rounded text-xs bg-purple-500/20 text-purple-400 border border-purple-500/30">
                          {item.additionalColumns.phase}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );

  // ── Right panel ────────────────────────────────────────────────────────
  const rightTitleAction = selectedItem ? (
    <div className="flex items-center gap-2">
      {editorToolbar}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm">
            <MoreVertical className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={handleDescribe}>
            <FileText className="h-4 w-4 mr-2" />
            Describe
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <ProtectedAction>
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onClick={() => setDeleteConfirmOpen(true)}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Deletar
            </DropdownMenuItem>
          </ProtectedAction>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button variant="outline" size="sm" onClick={() => setEditorFullScreen(true)}>
        <Maximize2 className="h-4 w-4" />
      </Button>
    </div>
  ) : null;

  const rightContent = !selectedItem ? (
    <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground">
      <Search className="h-12 w-12 opacity-20" />
      <p className="text-sm">Selecione um recurso para visualizar o manifesto</p>
    </div>
  ) : manifestLoading ? (
    <div className="flex justify-center items-center h-full">
      <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
    </div>
  ) : (
    <div className="flex flex-col h-full gap-2">
      {/* Header */}
      <div className="flex items-center gap-2 flex-wrap">
        <span className="font-semibold text-sm">{selectedItem.name}</span>
        {selectedItem.namespace && (
          <span className="px-1.5 py-0.5 rounded text-xs bg-secondary text-secondary-foreground">
            {selectedItem.namespace}
          </span>
        )}
        {selectedItem.kind && (
          <span className="px-1.5 py-0.5 rounded text-xs bg-blue-500/20 text-blue-400 border border-blue-500/30">
            {selectedItem.kind}
          </span>
        )}
        {selectedItem.apiVersion && (
          <span className="text-xs text-muted-foreground">{selectedItem.apiVersion}</span>
        )}
        {selectedItem.age && (
          <span className="text-xs text-muted-foreground ml-auto">Age: {selectedItem.age}</span>
        )}
      </div>

      {/* Editor */}
      <MonacoYamlEditor
        value={editorValue}
        onChange={handleEditorChange}
        originalValue={originalYaml}
        mode={viewMode}
        height={520}
      />
    </div>
  );

  // ── Render ─────────────────────────────────────────────────────────────
  return (
    <>
      <SplitView
        leftPanel={{
          title: "Recursos",
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedItem ? `${selectedResource?.kind}: ${selectedItem.name}` : "Visualização",
          titleAction: rightTitleAction ?? undefined,
          content: rightContent,
        }}
      />

      {/* Diff modal */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className={diffFullScreen ? "max-w-[98vw] w-[98vw] max-h-[98vh]" : "max-w-4xl max-h-[85vh]"}>
          <DialogHeader>
            <DialogTitle className="flex items-center justify-between">
              Diff — {selectedItem?.name}
              <Button variant="ghost" size="sm" onClick={() => setDiffFullScreen(!diffFullScreen)}>
                {diffFullScreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
              </Button>
            </DialogTitle>
          </DialogHeader>
          <ScrollArea className="max-h-[70vh]">
            <div
              className="diff2html-container text-xs"
              dangerouslySetInnerHTML={{ __html: diffHtml }}
            />
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Apply confirm modal */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Apply</DialogTitle>
            <DialogDescription>
              Aplicar as alterações em <strong>{selectedItem?.name}</strong>
              {selectedItem?.namespace ? ` (namespace: ${selectedItem.namespace})` : ""}?
              Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setApplyConfirmOpen(false)}>Cancelar</Button>
            <Button onClick={handleApply} disabled={isApplying}>
              {isApplying && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Aplicar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Describe modal */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh]">
          <DialogHeader>
            <DialogTitle>kubectl describe — {selectedItem?.name}</DialogTitle>
          </DialogHeader>
          {describeLoading ? (
            <div className="flex justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin" />
            </div>
          ) : (
            <ScrollArea className="max-h-[65vh]">
              <pre className="text-xs font-mono whitespace-pre-wrap break-words">{describeContent}</pre>
            </ScrollArea>
          )}
        </DialogContent>
      </Dialog>

      {/* Delete confirm modal */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Delete</DialogTitle>
            <DialogDescription>
              Deletar <strong>{selectedItem?.name}</strong>
              {selectedItem?.namespace ? ` do namespace ${selectedItem.namespace}` : ""}?
              Esta ação é irreversível.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)}>Cancelar</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
              {isDeleting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Deletar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Error dialog */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{errorTitle}</DialogTitle>
            <DialogDescription asChild>
              <ScrollArea className="max-h-48">
                <pre className="text-xs font-mono whitespace-pre-wrap break-words text-destructive">{errorMessage}</pre>
              </ScrollArea>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button onClick={() => setErrorDialogOpen(false)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Fullscreen editor modal */}
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent className="max-w-[98vw] w-[98vw] max-h-[98vh] h-[98vh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center justify-between">
              <span>{selectedResource?.kind}: {selectedItem?.name}</span>
              <div className="flex items-center gap-2">
                {editorToolbar}
                <Button variant="ghost" size="sm" onClick={() => setEditorFullScreen(false)}>
                  <Minimize2 className="h-4 w-4" />
                </Button>
              </div>
            </DialogTitle>
          </DialogHeader>
          <div className="flex-1 min-h-0 overflow-hidden">
            <MonacoYamlEditor
              value={editorValue}
              onChange={handleEditorChange}
              originalValue={originalYaml}
              mode={viewMode}
              height="calc(95vh - 140px)"
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
};
