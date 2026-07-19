import { useEffect, useMemo, useState, useCallback, useRef } from "react";
import { SplitView } from "@/components/SplitView";
import { useRevealOnKeyChange } from "@/hooks/useRevealOnKeyChange";
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
  FileText,
  Route,
  Trash2,
  AlertCircle,
  Copy,
  Plus,
  ChevronLeft,
  MoreVertical,
  X,
  SplitSquareHorizontal,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import yaml from "js-yaml";

import type { Namespace, GatewaySummary, GatewayManifest } from "@/lib/api/types";
import { useGateways } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { setHistoryCacheEntry } from "@/lib/historyCache";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac";

const GATEWAY_KINDS = [
  { value: "gateway", label: "Gateway" },
  { value: "httproute", label: "HTTPRoute" },
  { value: "grpcroute", label: "GRPCRoute" },
  { value: "tcproute", label: "TCPRoute" },
  { value: "gatewayclass", label: "GatewayClass" },
] as const;

const GATEWAY_TEMPLATES: Record<string, string> = {
  gateway: `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: my-gateway
  namespace: default
spec:
  gatewayClassName: nginx
  listeners:
    - name: http
      port: 80
      protocol: HTTP
`,
  httproute: `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: my-route
  namespace: default
spec:
  parentRefs:
    - name: my-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: my-service
          port: 80
`,
  grpcroute: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GRPCRoute
metadata:
  name: my-grpcroute
  namespace: default
spec:
  parentRefs:
    - name: my-gateway
  rules:
    - backendRefs:
        - name: my-grpc-service
          port: 50051
`,
  tcproute: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: my-tcproute
  namespace: default
spec:
  parentRefs:
    - name: my-gateway
  rules:
    - backendRefs:
        - name: my-tcp-service
          port: 9000
`,
  gatewayclass: `apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: my-gateway-class
spec:
  controllerName: example.com/foo
`,
};

interface GatewayTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onOpenCompare?: (initial: { type: "gateway"; namespace: string; name: string }) => void;
}

export const GatewayTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onOpenCompare,
}: GatewayTabProps) => {
  const [selectedKind, setSelectedKind] = useState<string>("gateway");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedGateway, setSelectedGateway] = useState<GatewaySummary | null>(null);
  const leftListRef = useRef<HTMLDivElement>(null);
  useRevealOnKeyChange(
    leftListRef,
    selectedGateway ? `${selectedGateway.cluster}-${selectedGateway.namespace}-${selectedGateway.name}` : null
  );
  const [manifest, setManifest] = useState<GatewayManifest | null>(null);
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
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [createYaml, setCreateYaml] = useState("");
  const [isCreating, setIsCreating] = useState(false);
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const historyCache = useRef<Map<string, { history: string[]; index: number }>>(new Map());
  const [editHistory, setEditHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const isClusterScoped = selectedKind === "gatewayclass";
  const effectiveNamespace = isClusterScoped ? "" : selectedNamespace;

  const { gateways, loading, error, refetch } = useGateways(
    cluster,
    effectiveNamespace || undefined,
    selectedKind
  );

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  const filteredGateways = useMemo(() => {
    if (!searchQuery) return gateways;
    const q = searchQuery.toLowerCase();
    return gateways.filter(
      (g) =>
        g.name.toLowerCase().includes(q) ||
        (g.namespace || "").toLowerCase().includes(q)
    );
  }, [gateways, searchQuery]);

  const kindLabel = GATEWAY_KINDS.find((k) => k.value === selectedKind)?.label ?? selectedKind;
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < editHistory.length - 1;
  const hasChanges = editorValue !== originalYaml && !!manifest;

  useEffect(() => {
    setSelectedGateway(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setViewMode("editor");
    setEditHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedKind, selectedNamespace]);

  const loadManifest = async (gw: GatewaySummary) => {
    if (selectedGateway && editHistory.length > 0) {
      const cacheKey = `${cluster}/${selectedGateway.namespace}/${selectedGateway.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...editHistory], index: historyIndex });
    }
    setSelectedGateway(gw);
    setManifestLoading(true);
    setManifest(null);
    try {
      const m = await apiClient.getGateway(cluster, gw.namespace, gw.kind || selectedKind, gw.name);
      setManifest(m);
      const initialYaml = m.yaml || "";
      setOriginalYaml(initialYaml);
      setViewMode("editor");

      const cacheKey = `${cluster}/${gw.namespace}/${gw.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setEditHistory(cached.history);
        setHistoryIndex(cached.index);
        if (cached.index >= 0 && cached.index < cached.history.length) {
          setEditorValue(cached.history[cached.index]);
        }
      } else {
        setEditHistory([initialYaml]);
        setHistoryIndex(0);
        setEditorValue(initialYaml);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
  }, []);

  const handleUndo = () => {
    if (!canUndo) return;
    const idx = historyIndex - 1;
    setHistoryIndex(idx);
    setEditorValue(editHistory[idx]);
  };

  const handleRedo = () => {
    if (!canRedo) return;
    const idx = historyIndex + 1;
    setHistoryIndex(idx);
    setEditorValue(editHistory[idx]);
  };

  const handleCancel = () => {
    if (!selectedGateway) return;
    setEditorValue(originalYaml);
    setEditHistory([originalYaml]);
    setHistoryIndex(0);
    const cacheKey = `${cluster}/${selectedGateway.namespace}/${selectedGateway.name}`;
    historyCache.current.delete(cacheKey);
  };

  const handleClearSelection = () => {
    setSelectedGateway(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setViewMode("editor");
    setEditHistory([]);
    setHistoryIndex(-1);
  };

  const refreshManifest = async () => {
    if (!selectedGateway) return;
    await loadManifest(selectedGateway);
  };

  const showError = (title: string, message: string) => {
    setErrorTitle(title);
    setErrorMessage(message);
    setErrorDialogOpen(true);
  };

  const handleValidate = async () => {
    if (!selectedGateway) return;
    setIsValidating(true);
    try {
      await apiClient.validateGateway({
        cluster,
        namespace: selectedGateway.namespace,
        yaml: editorValue,
        kind: selectedGateway.kind || selectedKind,
      });
      toast.success("Manifesto válido (dry-run OK)");
    } catch (err) {
      showError("Erro de Validação", err instanceof Error ? err.message : String(err));
    } finally {
      setIsValidating(false);
    }
  };

  const handleShowDiff = async () => {
    if (!selectedGateway) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffGateway(
        originalYaml,
        editorValue,
        `${selectedGateway.name}.yaml`
      );
      if (!result.hasChanges) {
        toast.info("Sem alterações");
        return;
      }
      setDiffHtml(
        diff2html(result.unifiedDiff, {
          drawFileList: false,
          matching: "lines",
          outputFormat: "side-by-side",
        })
      );
      setDiffModalOpen(true);
    } catch (err) {
      toast.error("Erro ao calcular diff: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleApply = async () => {
    if (!selectedGateway) return;
    setIsApplying(true);
    try {
      await apiClient.applyGateway(
        cluster,
        selectedGateway.namespace,
        selectedGateway.kind || selectedKind,
        selectedGateway.name,
        { yaml: editorValue }
      );
      toast.success(`${selectedGateway.kind || selectedKind}/${selectedGateway.name} aplicado`);
      setOriginalYaml(editorValue);
      setApplyConfirmOpen(false);
      refetch();
    } catch (err) {
      showError("Erro ao Aplicar", err instanceof Error ? err.message : String(err));
      setApplyConfirmOpen(false);
    } finally {
      setIsApplying(false);
    }
  };

  const handleDescribe = async () => {
    if (!selectedGateway) return;
    setDescribeLoading(true);
    setDescribeModalOpen(true);
    try {
      const result = await apiClient.describeGateway(
        cluster,
        selectedGateway.namespace,
        selectedGateway.kind || selectedKind,
        selectedGateway.name
      );
      setDescribeContent(result.describe);
    } catch (err) {
      setDescribeContent("Erro: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setDescribeLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!selectedGateway) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteGateway(
        cluster,
        selectedGateway.namespace,
        selectedGateway.kind || selectedKind,
        selectedGateway.name
      );
      toast.success(`${selectedGateway.name} deletado`);
      setDeleteConfirmOpen(false);
      handleClearSelection();
      refetch();
    } catch (err) {
      showError("Erro ao Deletar", err instanceof Error ? err.message : String(err));
      setDeleteConfirmOpen(false);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleCreate = async (dryRun = false) => {
    setIsCreating(true);
    try {
      const parsed = yaml.load(createYaml) as Record<string, unknown>;
      const kind = ((parsed?.kind as string) || selectedKind).toLowerCase();
      const ns =
        ((parsed?.metadata as Record<string, unknown>)?.namespace as string) ||
        effectiveNamespace ||
        "default";
      await apiClient.createGateway(cluster, ns, kind, { yaml: createYaml, dryRun });
      if (dryRun) {
        toast.success("Dry-run OK — manifesto válido");
      } else {
        toast.success("Recurso criado com sucesso");
        setCreateModalOpen(false);
        setCreateYaml("");
        refetch();
      }
    } catch (err) {
      showError(
        dryRun ? "Erro de Validação" : "Erro ao Criar",
        err instanceof Error ? err.message : String(err)
      );
    } finally {
      setIsCreating(false);
    }
  };

  // ── Painel esquerdo ──────────────────────────────────────────────────────

  const namespaceSelector = (
    <Select
      value={selectedNamespace || "__all__"}
      onValueChange={(value) => onNamespaceChange(value === "__all__" ? "" : value)}
      disabled={!cluster || filteredNamespaces.length === 0 || isClusterScoped}
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
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
        disabled={isClusterScoped}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={() => refetch()}
        disabled={!cluster || loading}
        title="Atualizar lista"
      >
        <RefreshCcw className="w-4 h-4" />
      </Button>
    </div>
  );

  const leftContent = (
    <div className="space-y-3" ref={leftListRef}>
      {selectedGateway && (
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary font-medium">
          <Route className="w-3.5 h-3.5 flex-shrink-0" />
          <span className="truncate flex-1">
            {selectedGateway.namespace ? `${selectedGateway.namespace}/` : ""}{selectedGateway.name}
          </span>
          <button
            onClick={handleClearSelection}
            className="flex-shrink-0 hover:text-foreground transition-colors"
            title="Voltar para lista"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Kind selector */}
      <Select value={selectedKind} onValueChange={(v) => setSelectedKind(v)}>
        <SelectTrigger className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {GATEWAY_KINDS.map((k) => (
            <SelectItem key={k.value} value={k.value}>
              {k.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Search */}
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
          placeholder={`Buscar ${kindLabel}...`}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {/* List */}
      {loading && (
        <div className="flex items-center justify-center py-8 text-muted-foreground text-sm gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          Carregando...
        </div>
      )}
      {!loading && error && (
        <div className="p-4 text-xs text-amber-400 flex items-start gap-2">
          <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}
      {!loading && !error && filteredGateways.length === 0 && (
        <div className="flex items-center justify-center py-8 text-muted-foreground text-sm">
          {(gateways ?? []).length === 0
            ? `Nenhum ${kindLabel} encontrado`
            : `Nenhum ${kindLabel} corresponde à busca`}
        </div>
      )}
      {!loading && !error && filteredGateways.map((gw) => {
        const isSelected =
          selectedGateway?.name === gw.name &&
          selectedGateway?.namespace === gw.namespace;
        const itemKey = `${gw.cluster}-${gw.namespace}-${gw.name}`;
        return (
          <button
            key={itemKey}
            data-item-key={itemKey}
            onClick={() => loadManifest(gw)}
            className={`w-full text-left p-3 rounded-lg border transition-colors ${
              isSelected
                ? "border-primary bg-primary/10 text-primary-foreground"
                : "border-border/60 hover:border-primary/40"
            }`}
          >
            <div className="font-semibold text-sm flex items-center gap-2">
              <Route className="w-4 h-4" />
              {gw.name}
            </div>
            {gw.namespace && !isClusterScoped && (
              <div className="text-xs text-muted-foreground">{gw.namespace}</div>
            )}
            {gw.addresses && gw.addresses.length > 0 && (
              <div className="text-[11px] text-muted-foreground mt-1">
                <span className="font-semibold">Endereços:</span> {gw.addresses.join(", ")}
              </div>
            )}
          </button>
        );
      })}
    </div>
  );

  // ── Painel direito ───────────────────────────────────────────────────────

  const rightTitlePrefix = selectedGateway ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  const rightTitleAction = (
    <div className="flex items-center gap-2">
      {selectedGateway && onOpenCompare && selectedGateway.kind?.toLowerCase() === "gateway" && (
        <Button
          variant="ghost"
          size="sm"
          title="Abrir em Edição Lado a Lado"
          onClick={() => onOpenCompare({ type: "gateway", namespace: selectedGateway.namespace, name: selectedGateway.name })}
        >
          <SplitSquareHorizontal className="w-4 h-4" />
        </Button>
      )}
      <ProtectedAction>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setCreateYaml(GATEWAY_TEMPLATES[selectedKind] || GATEWAY_TEMPLATES.gateway);
            setCreateModalOpen(true);
          }}
          disabled={!cluster}
          title={`Criar novo ${kindLabel}`}
        >
          <Plus className="w-4 h-4 mr-2" /> Criar
        </Button>
      </ProtectedAction>
      {selectedGateway && (
        <Button
          variant="outline"
          size="sm"
          onClick={handleDescribe}
          disabled={describeLoading}
        >
          {describeLoading ? (
            <Loader2 className="w-4 h-4 mr-1 animate-spin" />
          ) : (
            <FileText className="w-4 h-4 mr-1" />
          )}
          Describe
        </Button>
      )}
      <Button
        variant="outline"
        size="sm"
        onClick={refreshManifest}
        disabled={!selectedGateway || manifestLoading}
      >
        <RefreshCcw className="w-4 h-4 mr-2" />
        Recarregar YAML
      </Button>
      {selectedGateway && (
        <ProtectedAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={manifestLoading}>
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => {
                  navigator.clipboard.writeText(editorValue);
                  toast.success("YAML copiado");
                }}
              >
                <Copy className="w-4 h-4 mr-2" />
                Copiar YAML
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => setDeleteConfirmOpen(true)}
                disabled={isDeleting}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Deletar {kindLabel}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ProtectedAction>
      )}
    </div>
  );

  const renderOverviewTable = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar recursos Gateway API
        </div>
      );
    }
    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-4 w-4 animate-spin" />
          Carregando {kindLabel}s...
        </div>
      );
    }
    if (filteredGateways.length === 0) {
      return (
        <div className="flex flex-col items-center justify-center h-64 text-muted-foreground gap-3">
          <Route className="h-10 w-10 opacity-20" />
          <p className="text-sm">Nenhum {kindLabel} encontrado</p>
          <p className="text-xs text-muted-foreground/60 text-center max-w-xs leading-relaxed">
            Gateway API (<code>gateway.networking.k8s.io</code>) pode não estar instalada neste cluster.
            Verifique com: <code>kubectl get crd gateways.gateway.networking.k8s.io</code>
          </p>
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {/* header row */}
        <div className="flex items-center gap-2 text-xs text-muted-foreground pb-1 border-b border-border/50">
          <span className="font-medium">{filteredGateways.length} {kindLabel}(s)</span>
        </div>

        {/* table */}
        <div className="w-full overflow-x-auto">
          <table className="w-full text-xs border-collapse">
            <thead>
              <tr className="text-left text-muted-foreground uppercase text-[10px] tracking-wide">
                <th className="pb-2 pr-4 font-medium">Nome</th>
                {!isClusterScoped && <th className="pb-2 pr-4 font-medium">Namespace</th>}
                <th className="pb-2 pr-4 font-medium">Endereços</th>
                <th className="pb-2 w-8" />
              </tr>
            </thead>
            <tbody>
              {filteredGateways.map((gw) => (
                <tr
                  key={`${gw.cluster}-${gw.namespace}-${gw.name}`}
                  className="border-t border-border/30 hover:bg-accent/40 cursor-pointer transition-colors group"
                  onClick={() => loadManifest(gw)}
                >
                  <td className="py-2 pr-4">
                    <div className="flex items-center gap-1.5 font-medium">
                      <Route className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                      {gw.name}
                    </div>
                  </td>
                  {!isClusterScoped && (
                    <td className="py-2 pr-4 text-muted-foreground">{gw.namespace || "—"}</td>
                  )}
                  <td className="py-2 pr-4 text-muted-foreground">
                    {gw.addresses && gw.addresses.length > 0 ? gw.addresses.join(", ") : "—"}
                  </td>
                  <td className="py-2 text-right">
                    <button
                      className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-accent transition-all"
                      title={`Editar ${gw.name}`}
                      onClick={(e) => { e.stopPropagation(); loadManifest(gw); }}
                    >
                      <FileText className="h-3.5 w-3.5 text-muted-foreground" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  };

  const renderManifestPanel = () => {
    if (!selectedGateway) {
      return renderOverviewTable();
    }

    return (
      <div className="space-y-3">
        {/* Metadata header */}
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedGateway.cluster || cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Kind</span>
            <span className="font-mono text-primary">{selectedGateway.kind || kindLabel}</span>
          </div>
          {!isClusterScoped && selectedGateway.namespace && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
              <span className="font-medium">{selectedGateway.namespace}</span>
            </div>
          )}
          {selectedGateway.addresses && selectedGateway.addresses.length > 0 && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Endereços</span>
              <span className="font-medium">{selectedGateway.addresses.join(", ")}</span>
            </div>
          )}
        </div>

        {/* Editor section */}
        <div className="space-y-3">
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Manifesto YAML</p>
              <div className="flex items-center gap-2">
                {manifestLoading && (
                  <span className="text-xs text-muted-foreground">Carregando...</span>
                )}
                {/* Undo/Redo */}
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleUndo}
                    disabled={!canUndo}
                    className={`px-2 py-1 text-xs font-medium ${
                      canUndo
                        ? "bg-background text-muted-foreground hover:bg-secondary"
                        : "bg-background text-muted-foreground/30 cursor-not-allowed"
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
                      canRedo
                        ? "bg-background text-muted-foreground hover:bg-secondary"
                        : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Refazer (Ctrl+Y)"
                  >
                    <Redo2 className="w-3.5 h-3.5" />
                  </button>
                </div>
                {/* Editor/Diff toggle */}
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={() => setViewMode("editor")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "editor"
                        ? "bg-primary text-white"
                        : "bg-background text-muted-foreground"
                    }`}
                  >
                    Editor
                  </button>
                  <button
                    type="button"
                    onClick={() => setViewMode("diff")}
                    disabled={!hasChanges}
                    className={`px-3 py-1 text-xs font-medium border-l border-border/50 ${
                      viewMode === "diff"
                        ? "bg-primary text-white"
                        : "bg-background text-muted-foreground"
                    } ${!hasChanges ? "opacity-50 cursor-not-allowed" : ""}`}
                  >
                    Diff
                  </button>
                </div>
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

          {/* Action buttons */}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleShowDiff}
              disabled={!hasChanges || isDiffLoading}
            >
              {isDiffLoading ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <FileDiff className="w-4 h-4 mr-2" />
              )}
              Visualizar diff
            </Button>
            <ProtectedAction>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleValidate}
                disabled={isValidating}
              >
                {isValidating ? (
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                ) : (
                  <CheckCircle2 className="w-4 h-4 mr-2" />
                )}
                Validar (Dry-run)
              </Button>
            </ProtectedAction>
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={!hasChanges}
            >
              <X className="w-4 h-4 mr-2" /> Cancelar
            </Button>
            <ProtectedAction>
              <Button
                variant="default"
                size="sm"
                onClick={() => setApplyConfirmOpen(true)}
                disabled={isApplying || !hasChanges}
              >
                <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
              </Button>
            </ProtectedAction>
          </div>
        </div>
      </div>
    );
  };

  return (
    <>
      <SplitView
        leftPanel={{
          title: `${kindLabel}s`,
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedGateway ? selectedGateway.name : "Editor",
          titlePrefix: rightTitlePrefix,
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {/* Apply confirm */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Aplicação</DialogTitle>
            <DialogDescription>
              Aplicar alterações em{" "}
              <strong>
                {selectedGateway?.kind || selectedKind}/{selectedGateway?.name}
              </strong>{" "}
              no cluster <strong>{cluster}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setApplyConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button onClick={handleApply} disabled={isApplying}>
              {isApplying && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
              Aplicar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirm */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Deleção</DialogTitle>
            <DialogDescription>
              <strong className="text-destructive">Deletar permanentemente</strong>{" "}
              {selectedGateway?.kind || selectedKind}/{selectedGateway?.name}?
              Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
              {isDeleting && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
              Deletar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Describe modal */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-3xl max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>kubectl describe — {selectedGateway?.name}</DialogTitle>
          </DialogHeader>
          <ScrollArea className="flex-1 min-h-0">
            {describeLoading ? (
              <div className="flex items-center justify-center p-8 gap-2 text-muted-foreground text-xs">
                <Loader2 className="h-4 w-4 animate-spin" />
                Carregando...
              </div>
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap p-4">{describeContent}</pre>
            )}
          </ScrollArea>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(describeContent);
                toast.success("Copiado");
              }}
            >
              <Copy className="h-3.5 w-3.5 mr-1.5" />
              Copiar
            </Button>
            <Button variant="outline" size="sm" onClick={() => setDescribeModalOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Diff modal */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent
          className={
            diffFullScreen
              ? "max-w-full w-screen h-screen rounded-none flex flex-col"
              : "max-w-5xl max-h-[85vh] flex flex-col"
          }
        >
          <DialogHeader className="flex-shrink-0 flex flex-row items-center justify-between">
            <div>
              <DialogTitle>Diff Visual</DialogTitle>
              <DialogDescription>
                Comparação lado a lado — {selectedGateway?.namespace}/{selectedGateway?.name}
              </DialogDescription>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDiffFullScreen((v) => !v)}
              className="gap-2"
            >
              {diffFullScreen ? (
                <>
                  <Minimize2 className="h-4 w-4" />
                  Sair de tela cheia
                </>
              ) : (
                <>
                  <Maximize2 className="h-4 w-4" />
                  Tela cheia
                </>
              )}
            </Button>
          </DialogHeader>
          <ScrollArea className="flex-1 min-h-0">
            <div className="p-4">
              <div
                className="diff2html-dark text-xs [&_.d2h-wrapper]:text-xs"
                dangerouslySetInnerHTML={{ __html: diffHtml }}
              />
            </div>
          </ScrollArea>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setDiffModalOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create modal */}
      <Dialog open={createModalOpen} onOpenChange={setCreateModalOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Novo {kindLabel}</DialogTitle>
            <DialogDescription>
              Edite o YAML e clique em Criar para aplicar no cluster{" "}
              <strong>{cluster}</strong>.
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 min-h-0">
            <MonacoYamlEditor
              value={createYaml}
              onChange={setCreateYaml}
              height={400}
              readOnly={false}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateModalOpen(false)}>
              Cancelar
            </Button>
            <Button
              variant="outline"
              onClick={() => handleCreate(true)}
              disabled={isCreating}
            >
              {isCreating ? (
                <Loader2 className="h-4 w-4 animate-spin mr-1" />
              ) : (
                <CheckCircle2 className="h-4 w-4 mr-1" />
              )}
              Dry-run
            </Button>
            <ProtectedAction>
              <Button onClick={() => handleCreate(false)} disabled={isCreating}>
                {isCreating && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
                Criar
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Error dialog */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[70vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <AlertCircle className="h-5 w-5" />
              {errorTitle}
            </DialogTitle>
          </DialogHeader>
          <ScrollArea className="flex-1 min-h-0">
            <pre className="text-xs font-mono whitespace-pre-wrap p-2 text-destructive/90">
              {errorMessage}
            </pre>
          </ScrollArea>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(errorMessage);
                toast.success("Erro copiado");
              }}
            >
              <Copy className="h-3.5 w-3.5 mr-1.5" />
              Copiar
            </Button>
            <Button variant="outline" size="sm" onClick={() => setErrorDialogOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
