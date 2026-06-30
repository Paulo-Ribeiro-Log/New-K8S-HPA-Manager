import { useEffect, useMemo, useState, useRef } from "react";
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
}

export const GatewayTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: GatewayTabProps) => {
  const [selectedKind, setSelectedKind] = useState<string>("gateway");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedGateway, setSelectedGateway] = useState<GatewaySummary | null>(null);
  const [manifest, setManifest] = useState<GatewayManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
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
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [createYaml, setCreateYaml] = useState("");
  const [isCreating, setIsCreating] = useState(false);
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const historyCache = useRef<Map<string, { history: string[]; index: number }>>(new Map());
  const [editHistory, setEditHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  // GatewayClass é cluster-scoped — sem namespace
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

  const kindLabel =
    GATEWAY_KINDS.find((k) => k.value === selectedKind)?.label ?? selectedKind;

  useEffect(() => {
    setSelectedGateway(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
  }, [cluster, selectedKind, selectedNamespace]);

  const loadManifest = async (gw: GatewaySummary) => {
    setSelectedGateway(gw);
    setManifestLoading(true);
    try {
      const m = await apiClient.getGateway(
        cluster,
        gw.namespace,
        gw.kind || selectedKind,
        gw.name
      );
      setManifest(m);
      setEditorValue(m.yaml);
      setOriginalYaml(m.yaml);

      const cacheKey = `${cluster}/${gw.namespace}/${gw.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setEditHistory(cached.history);
        setHistoryIndex(cached.index);
      } else {
        setEditHistory([m.yaml]);
        setHistoryIndex(0);
      }
    } catch (err) {
      toast.error(
        "Erro ao carregar manifesto: " +
          (err instanceof Error ? err.message : String(err))
      );
    } finally {
      setManifestLoading(false);
    }
  };

  const handleEditorChange = (value: string) => {
    setEditorValue(value);
    if (!selectedGateway) return;
    const cacheKey = `${cluster}/${selectedGateway.namespace}/${selectedGateway.name}`;
    const newHistory = [...editHistory.slice(0, historyIndex + 1), value].slice(-50);
    const newIndex = newHistory.length - 1;
    setEditHistory(newHistory);
    setHistoryIndex(newIndex);
    setHistoryCacheEntry(historyCache.current, cacheKey, { history: newHistory, index: newIndex });
  };

  const handleUndo = () => {
    if (historyIndex <= 0) return;
    const idx = historyIndex - 1;
    setHistoryIndex(idx);
    setEditorValue(editHistory[idx]);
  };

  const handleRedo = () => {
    if (historyIndex >= editHistory.length - 1) return;
    const idx = historyIndex + 1;
    setHistoryIndex(idx);
    setEditorValue(editHistory[idx]);
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
      showError(
        "Erro de Validação",
        err instanceof Error ? err.message : String(err)
      );
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
      toast.error(
        "Erro ao calcular diff: " +
          (err instanceof Error ? err.message : String(err))
      );
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
      toast.success(
        `${selectedGateway.kind || selectedKind}/${selectedGateway.name} aplicado`
      );
      setOriginalYaml(editorValue);
      setApplyConfirmOpen(false);
      refetch();
    } catch (err) {
      showError(
        "Erro ao Aplicar",
        err instanceof Error ? err.message : String(err)
      );
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
      setDescribeContent(
        "Erro: " + (err instanceof Error ? err.message : String(err))
      );
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
      setSelectedGateway(null);
      setManifest(null);
      setEditorValue("");
      refetch();
    } catch (err) {
      showError(
        "Erro ao Deletar",
        err instanceof Error ? err.message : String(err)
      );
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
      await apiClient.createGateway(cluster, ns, kind, {
        yaml: createYaml,
        dryRun,
      });
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

  const hasChanges = editorValue !== originalYaml && !!manifest;

  // ── Left panel: toolbar + list ──────────────────────────────────────────
  const leftTitleAction = (
    <div className="flex items-center gap-1 flex-wrap">
      <Select value={selectedKind} onValueChange={(v) => setSelectedKind(v)}>
        <SelectTrigger className="h-6 w-32 text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {GATEWAY_KINDS.map((k) => (
            <SelectItem key={k.value} value={k.value} className="text-xs">
              {k.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {!isClusterScoped && (
        <Select
          value={selectedNamespace || "__all__"}
          onValueChange={(v) => onNamespaceChange(v === "__all__" ? "" : v)}
        >
          <SelectTrigger className="h-6 w-36 text-xs">
            <SelectValue placeholder="Namespace" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__" className="text-xs">Todos</SelectItem>
            {filteredNamespaces.map((ns) => (
              <SelectItem key={ns.name} value={ns.name} className="text-xs">
                {ns.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      <div className="relative">
        <Search className="absolute left-1.5 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
        <Input
          className="h-6 pl-6 text-xs w-36"
          placeholder="Filtrar..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
      </div>

      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={() => refetch()}
        title="Atualizar"
      >
        <RefreshCcw className="h-3 w-3" />
      </Button>

      {!isClusterScoped && (
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={onToggleSystemNamespaces}
          title={showSystemNamespaces ? "Ocultar system" : "Mostrar system"}
        >
          {showSystemNamespaces ? (
            <EyeOff className="h-3 w-3" />
          ) : (
            <Eye className="h-3 w-3" />
          )}
        </Button>
      )}

      <ProtectedAction>
        <Button
          size="sm"
          className="h-6 text-xs gap-1"
          onClick={() => {
            setCreateYaml(
              GATEWAY_TEMPLATES[selectedKind] || GATEWAY_TEMPLATES.gateway
            );
            setCreateModalOpen(true);
          }}
        >
          <Plus className="h-3 w-3" />
          Novo
        </Button>
      </ProtectedAction>
    </div>
  );

  const leftContent = (
    <div className="space-y-0">
      {loading && (
        <div className="flex items-center justify-center py-8 text-muted-foreground text-xs gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          Carregando...
        </div>
      )}
      {error && (
        <div className="p-4 text-xs text-amber-400 flex items-start gap-2">
          <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}
      {!loading && !error && filteredGateways.length === 0 && (
        <div className="py-8 px-4 text-center space-y-2">
          <Route className="h-6 w-6 text-muted-foreground/40 mx-auto" />
          <p className="text-xs text-muted-foreground">
            Nenhum {kindLabel} encontrado
          </p>
          <p className="text-[10px] text-muted-foreground/60 leading-relaxed">
            Gateway API (<code>gateway.networking.k8s.io</code>) pode não estar instalada
            neste cluster. Verifique com:{" "}
            <code className="text-[10px]">kubectl get crd gateways.gateway.networking.k8s.io</code>
          </p>
        </div>
      )}
      {filteredGateways.map((gw) => {
        const isSelected =
          selectedGateway?.name === gw.name &&
          selectedGateway?.namespace === gw.namespace;
        return (
          <button
            key={`${gw.namespace}/${gw.name}`}
            className={`w-full text-left px-2 py-2 rounded hover:bg-accent/50 transition-colors ${
              isSelected ? "bg-accent" : ""
            }`}
            onClick={() => loadManifest(gw)}
          >
            <div className="flex items-center gap-2 min-w-0">
              <Route className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
              <span className="text-xs font-medium truncate">{gw.name}</span>
            </div>
            {gw.namespace && !isClusterScoped && (
              <div className="text-[10px] text-muted-foreground mt-0.5 ml-5">
                {gw.namespace}
              </div>
            )}
            {gw.addresses && gw.addresses.length > 0 && (
              <div className="text-[10px] text-blue-400 mt-0.5 ml-5 truncate">
                {gw.addresses.join(", ")}
              </div>
            )}
          </button>
        );
      })}
    </div>
  );

  // ── Right panel: editor ──────────────────────────────────────────────────
  const rightTitleSuffix = selectedGateway ? (
    <span className="text-xs text-muted-foreground font-normal">
      {selectedGateway.namespace && !isClusterScoped
        ? `(${selectedGateway.namespace})`
        : ""}
      {hasChanges && (
        <span
          className="ml-2 inline-block w-1.5 h-1.5 rounded-full bg-amber-400"
          title="Alterações não aplicadas"
        />
      )}
    </span>
  ) : null;

  const rightTitleAction = selectedGateway ? (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={handleUndo}
        disabled={historyIndex <= 0}
        title="Desfazer"
      >
        <Undo2 className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={handleRedo}
        disabled={historyIndex >= editHistory.length - 1}
        title="Refazer"
      >
        <Redo2 className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={handleShowDiff}
        disabled={!hasChanges || isDiffLoading}
        title="Ver diff"
      >
        {isDiffLoading ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <FileDiff className="h-3 w-3" />
        )}
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={handleDescribe}
        title="kubectl describe"
      >
        <FileText className="h-3 w-3" />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="h-6 w-6" title="Mais ações">
            <Copy className="h-3 w-3" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => {
              navigator.clipboard.writeText(editorValue);
              toast.success("YAML copiado");
            }}
          >
            <Copy className="h-3.5 w-3.5 mr-2" />
            Copiar YAML
          </DropdownMenuItem>
          <DropdownMenuItem
            className="text-destructive"
            onClick={() => setDeleteConfirmOpen(true)}
          >
            <Trash2 className="h-3.5 w-3.5 mr-2" />
            Deletar
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={() => setEditorFullScreen((v) => !v)}
        title={editorFullScreen ? "Sair da tela cheia" : "Tela cheia"}
      >
        {editorFullScreen ? (
          <Minimize2 className="h-3 w-3" />
        ) : (
          <Maximize2 className="h-3 w-3" />
        )}
      </Button>
    </div>
  ) : null;

  // apply bar height in px (shown only when hasChanges)
  const applyBarHeight = 38;

  const rightContent = selectedGateway ? (
    // Usamos position:relative + absolute inset-0 para que Monaco
    // receba altura real independente do overflow-auto do SplitView
    <div className="relative w-full h-full">
      {/* Apply bar — posicionado no topo, empurra o editor para baixo */}
      {hasChanges && (
        <div
          className="absolute top-0 left-0 right-0 z-10 flex items-center gap-2 px-2 rounded bg-amber-500/10 border-b border-amber-500/30"
          style={{ height: applyBarHeight }}
        >
          <TriangleAlert className="h-3.5 w-3.5 text-amber-400 flex-shrink-0" />
          <span className="text-xs text-amber-300 flex-1">Alterações pendentes</span>
          <ProtectedAction>
            <Button
              size="sm"
              variant="outline"
              className="h-6 text-xs border-amber-500/50 text-amber-300 hover:bg-amber-500/20"
              onClick={handleValidate}
              disabled={isValidating}
            >
              {isValidating ? (
                <Loader2 className="h-3 w-3 animate-spin mr-1" />
              ) : (
                <CheckCircle2 className="h-3 w-3 mr-1" />
              )}
              Validar
            </Button>
          </ProtectedAction>
          <ProtectedAction>
            <Button
              size="sm"
              className="h-6 text-xs bg-amber-600 hover:bg-amber-700"
              onClick={() => setApplyConfirmOpen(true)}
              disabled={isApplying}
            >
              {isApplying && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
              Aplicar
            </Button>
          </ProtectedAction>
        </div>
      )}

      {/* Editor — ocupa o espaço restante abaixo da apply bar */}
      {editorFullScreen ? (
        <div className="fixed inset-0 z-50 bg-background">
          <MonacoYamlEditor
            value={editorValue}
            onChange={handleEditorChange}
            height="100%"
            readOnly={false}
          />
        </div>
      ) : manifestLoading ? (
        <div
          className="absolute inset-0 flex items-center justify-center gap-2 text-muted-foreground text-xs"
          style={{ top: hasChanges ? applyBarHeight : 0 }}
        >
          <Loader2 className="h-4 w-4 animate-spin" />
          Carregando...
        </div>
      ) : (
        <div
          className="absolute left-0 right-0 bottom-0"
          style={{ top: hasChanges ? applyBarHeight : 0 }}
        >
          <MonacoYamlEditor
            value={editorValue}
            onChange={handleEditorChange}
            height="100%"
            readOnly={false}
          />
        </div>
      )}
    </div>
  ) : (
    <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
      <Route className="h-8 w-8 opacity-30" />
      <span className="text-xs">Selecione um {kindLabel} para editar</span>
    </div>
  );

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
          titleSuffix: rightTitleSuffix,
          titleAction: rightTitleAction,
          content: rightContent,
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
              {isApplying && (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              )}
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
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={isDeleting}
            >
              {isDeleting && (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              )}
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
              <pre className="text-xs font-mono whitespace-pre-wrap p-4">
                {describeContent}
              </pre>
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
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDescribeModalOpen(false)}
            >
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
            <DialogTitle>Diff — {selectedGateway?.name}</DialogTitle>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setDiffFullScreen((v) => !v)}
              className="h-7 w-7"
            >
              {diffFullScreen ? (
                <Minimize2 className="h-4 w-4" />
              ) : (
                <Maximize2 className="h-4 w-4" />
              )}
            </Button>
          </DialogHeader>
          <ScrollArea className="flex-1 min-h-0">
            <div
              className="text-xs [&_.d2h-wrapper]:text-xs"
              dangerouslySetInnerHTML={{ __html: diffHtml }}
            />
          </ScrollArea>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDiffModalOpen(false)}
            >
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
          <div className="flex-1 min-h-0 h-96">
            <MonacoYamlEditor
              value={createYaml}
              onChange={setCreateYaml}
              height="100%"
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
                {isCreating && (
                  <Loader2 className="h-4 w-4 animate-spin mr-1" />
                )}
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
            <Button
              variant="outline"
              size="sm"
              onClick={() => setErrorDialogOpen(false)}
            >
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
