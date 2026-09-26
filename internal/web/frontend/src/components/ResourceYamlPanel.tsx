import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as yaml from "js-yaml";
import { createTwoFilesPatch } from "diff";
import { html } from "diff2html";
import { toast } from "sonner";
import {
  AlertCircle,
  CheckCircle2,
  Copy,
  FileDiff,
  Loader2,
  Maximize2,
  Minimize2,
  Redo2,
  RefreshCcw,
  TriangleAlert,
  Undo2,
  X,
} from "lucide-react";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ProtectedAction } from "@/components/rbac";

// Editor YAML de um recurso do cluster com as mesmas ferramentas da aba Namespaces (Monaco com
// schema K8s, desfazer/refazer, editor/diff, tela cheia, diff lado a lado, dry-run, cancelar,
// aplicar com confirmação das mudanças e modal de erro). Extraído para não repetir ~600 linhas
// por aba — hoje usado pela aba Nodes.

interface ResourceYamlPanelProps {
  kind: string; // "Node"
  name: string;
  cluster: string;
  loadYaml: () => Promise<string>;
  applyYaml: (yamlContent: string, dryRun: boolean) => Promise<void>;
  /** RBAC do cluster: false desabilita dry-run/aplicar com explicação. */
  canApply?: boolean;
  onApplied?: () => void;
  /** Muda quando o recurso é alterado por fora (ex: cordon) — recarrega o YAML se não houver edição. */
  reloadToken?: unknown;
  editorHeight?: number;
}

const HISTORY_LIMIT = 50;

type Change = { path: string; before: string; after: string };

function compactChanges(originalYaml: string, editedYaml: string): Change[] {
  try {
    const changes: Change[] = [];
    const fmt = (v: unknown, missing: string) =>
      v === undefined ? missing : v === null ? "null" : v === "" ? '""' : typeof v === "object" ? JSON.stringify(v, null, 2) : String(v);
    type Obj = Record<string, unknown> | undefined;
    const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
    const walk = (a: Obj, b: Obj, path: string) => {
      for (const key of new Set([...Object.keys(a || {}), ...Object.keys(b || {})])) {
        const p = path ? `${path}.${key}` : key;
        const va = a?.[key];
        const vb = b?.[key];
        if (va === vb) continue;
        if (isObj(va) && isObj(vb)) walk(va, vb, p);
        else if (JSON.stringify(va) !== JSON.stringify(vb)) changes.push({ path: p, before: fmt(va, "(não existe)"), after: fmt(vb, "(removido)") });
      }
    };
    walk(yaml.load(originalYaml) as Obj, yaml.load(editedYaml) as Obj, "");
    return changes;
  } catch {
    return [];
  }
}

export function ResourceYamlPanel({
  kind,
  name,
  cluster,
  loadYaml,
  applyYaml,
  canApply = true,
  onApplied,
  reloadToken,
  editorHeight = 450,
}: ResourceYamlPanelProps) {
  const [loading, setLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [fullScreen, setFullScreen] = useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [error, setError] = useState<{ title: string; message: string } | null>(null);
  const historyTimer = useRef<ReturnType<typeof setTimeout>>();

  const hasChanges = editorValue !== originalYaml;
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;
  const applyDisabledReason = canApply ? undefined : `Sem permissão no cluster para alterar ${kind}`;

  const resetTo = (content: string) => {
    setEditorValue(content);
    setOriginalYaml(content);
    setHistory([content]);
    setHistoryIndex(0);
    setViewMode("editor");
  };

  const load = useCallback(
    async (notify = false) => {
      setLoading(true);
      try {
        resetTo(await loadYaml());
        if (notify) toast.success("YAML recarregado do cluster");
      } catch (err) {
        toast.error(`Erro ao carregar o YAML do ${kind}`, { description: err instanceof Error ? err.message : String(err) });
      } finally {
        setLoading(false);
      }
    },
    [loadYaml, kind],
  );

  // Troca de recurso: carrega do zero.
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, name]);

  // Alteração externa (cordon/uncordon): recarrega só se não houver edição em andamento.
  const editingRef = useRef(false);
  editingRef.current = hasChanges;
  useEffect(() => {
    if (reloadToken === undefined || editingRef.current) return;
    load();
  }, [reloadToken]); // eslint-disable-line react-hooks/exhaustive-deps

  // Histórico de desfazer/refazer: um passo por pausa de digitação (600ms).
  const handleChange = (value: string | undefined) => {
    const next = value ?? "";
    setEditorValue(next);
    clearTimeout(historyTimer.current);
    historyTimer.current = setTimeout(() => {
      setHistory(prev => {
        const base = prev.slice(0, historyIndexRef.current + 1);
        if (base[base.length - 1] === next) return prev;
        const updated = [...base, next].slice(-HISTORY_LIMIT);
        setHistoryIndex(updated.length - 1);
        return updated;
      });
    }, 600);
  };
  const historyIndexRef = useRef(historyIndex);
  historyIndexRef.current = historyIndex;
  useEffect(() => () => clearTimeout(historyTimer.current), []);

  const undo = () => {
    if (!canUndo) return;
    clearTimeout(historyTimer.current);
    setHistoryIndex(historyIndex - 1);
    setEditorValue(history[historyIndex - 1]);
  };
  const redo = () => {
    if (!canRedo) return;
    setHistoryIndex(historyIndex + 1);
    setEditorValue(history[historyIndex + 1]);
  };

  const cancel = () => {
    resetTo(originalYaml);
    setFullScreen(false);
    toast.info("Alterações descartadas");
  };

  const validate = async () => {
    setIsValidating(true);
    try {
      await applyYaml(editorValue, true);
      toast.success("Validação bem-sucedida (dry-run)", { description: "O YAML está válido e pode ser aplicado" });
    } catch (err) {
      toast.error("Falha na validação", { description: err instanceof Error ? err.message : String(err) });
    } finally {
      setIsValidating(false);
    }
  };

  const apply = async () => {
    setConfirmOpen(false);
    setIsApplying(true);
    try {
      await applyYaml(editorValue, false);
      toast.success(`${kind} aplicado`, { description: name });
      await load();
      onApplied?.();
    } catch (err) {
      setError({ title: `Falha ao aplicar ${kind} ${name}`, message: err instanceof Error ? err.message : String(err) });
      toast.error(`Falha ao aplicar ${kind}`, { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  const openDiff = (full: boolean) => {
    const patch = createTwoFilesPatch(`${name} (original)`, `${name} (editado)`, originalYaml, editorValue, "", "");
    setDiffHtml(html(patch, { drawFileList: false, matching: "lines", outputFormat: "side-by-side" }));
    setDiffFullScreen(full);
    setDiffOpen(true);
  };

  const changes = useMemo(() => (confirmOpen ? compactChanges(originalYaml, editorValue) : []), [confirmOpen, originalYaml, editorValue]);

  const toolbar = (
    <div className="flex items-center gap-2 flex-wrap">
      <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
        <button type="button" onClick={undo} disabled={!canUndo} title="Desfazer (Ctrl+Z)"
          className={`px-2 py-1 text-xs ${canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"}`}>
          <Undo2 className="w-3.5 h-3.5" />
        </button>
        <button type="button" onClick={redo} disabled={!canRedo} title="Refazer (Ctrl+Y)"
          className={`px-2 py-1 text-xs border-l border-border/50 ${canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"}`}>
          <Redo2 className="w-3.5 h-3.5" />
        </button>
      </div>
      <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
        <button type="button" onClick={() => setViewMode("editor")}
          className={`px-3 py-1 text-xs font-medium ${viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"}`}>
          Editor
        </button>
        <button type="button" onClick={() => setViewMode("diff")} disabled={!hasChanges}
          className={`px-3 py-1 text-xs font-medium ${viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"} ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}>
          Diff
        </button>
      </div>
    </div>
  );

  const editor = (height: number | string) =>
    viewMode === "editor" ? (
      <MonacoYamlEditor value={editorValue} onChange={handleChange} height={height} readOnly={loading} />
    ) : (
      <MonacoYamlEditor mode="diff" originalValue={originalYaml} value={editorValue} height={height} readOnly />
    );

  const validateButton = (label: string) => (
    <ProtectedAction>
      <Button variant="secondary" size="sm" onClick={validate} disabled={isValidating || !canApply} title={applyDisabledReason}>
        {isValidating ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-2" />}
        {label}
      </Button>
    </ProtectedAction>
  );

  const applyButton = (onClick: () => void) => (
    <ProtectedAction>
      <Button variant="default" size="sm" onClick={onClick} disabled={isApplying || !hasChanges || !canApply} title={applyDisabledReason}>
        {isApplying ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <TriangleAlert className="w-4 h-4 mr-2" />}
        Aplicar
      </Button>
    </ProtectedAction>
  );

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <p className="text-sm font-medium flex items-center gap-2">
          Manifesto YAML
          {hasChanges && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400" title="Alterações não aplicadas" />}
          {loading && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
        </p>
        <div className="flex items-center gap-2 flex-wrap">
          {toolbar}
          <Button variant="outline" size="sm" onClick={() => { navigator.clipboard.writeText(editorValue); toast.success("YAML copiado"); }} title="Copiar YAML">
            <Copy className="w-3.5 h-3.5" />
          </Button>
          <Button variant="outline" size="sm" onClick={() => load(true)} disabled={loading} title="Recarregar do cluster (descarta edições)">
            <RefreshCcw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <Button variant="outline" size="sm" onClick={() => setFullScreen(true)} title="Abrir editor em tela cheia">
            <Maximize2 className="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>

      {editor(editorHeight)}

      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => openDiff(false)} disabled={!hasChanges}>
          <FileDiff className="w-4 h-4 mr-2" />
          Visualizar diff
        </Button>
        <Button variant="outline" size="sm" onClick={() => openDiff(true)} disabled={!hasChanges} className="gap-2" title="Abrir diff ocupando toda a tela">
          <Maximize2 className="w-4 h-4" />
          Tela cheia
        </Button>
        {validateButton("Validar (Dry-run)")}
        <Button variant="outline" size="sm" onClick={cancel} disabled={!hasChanges}>
          <X className="w-4 h-4 mr-2" />
          Cancelar
        </Button>
        {applyButton(() => setConfirmOpen(true))}
      </div>

      {/* Editor em tela cheia */}
      <Dialog open={fullScreen} onOpenChange={setFullScreen}>
        <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0">
          <div className="h-full flex flex-col">
            <DialogHeader className="border-b border-border px-6 py-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <DialogTitle className="text-xl font-semibold text-primary">Editor YAML - Tela Cheia</DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground">
                    {kind} {name} • {cluster}
                  </DialogDescription>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  {toolbar}
                  {validateButton("Dry-run")}
                  <Button variant="outline" size="sm" onClick={cancel} title="Descartar alterações e sair">
                    Cancelar
                  </Button>
                  {applyButton(() => { setFullScreen(false); setConfirmOpen(true); })}
                  <Button variant="ghost" size="sm" onClick={() => setFullScreen(false)} title="Minimizar tela cheia (Esc)">
                    <Minimize2 className="w-4 h-4" />
                  </Button>
                </div>
              </div>
            </DialogHeader>
            <div className="flex-1 p-4">{editor("calc(100vh - 140px)")}</div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Diff lado a lado */}
      <Dialog open={diffOpen} onOpenChange={setDiffOpen}>
        <DialogContent className={diffFullScreen ? "w-screen h-screen max-w-none max-h-none rounded-none" : "max-w-6xl max-h-[90vh]"}>
          <DialogHeader>
            <DialogTitle>Comparação de Alterações (Diff)</DialogTitle>
            <DialogDescription>
              {kind} {name} • {cluster}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className={diffFullScreen ? "h-[calc(100vh-120px)] w-full" : "h-[70vh] w-full"}>
            <div className="diff-content" dangerouslySetInnerHTML={{ __html: diffHtml }} />
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Confirmação de apply com as mudanças detectadas */}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent className="max-w-4xl max-h-[90vh] bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">Confirmar aplicação</DialogTitle>
            <DialogDescription>Essa ação vai aplicar o {kind} diretamente no cluster selecionado.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {cluster}</p>
              <p><span className="text-muted-foreground">{kind}:</span> {name}</p>
            </div>
            {changes.length > 0 && (
              <div className="space-y-2">
                <p className="font-semibold text-sm">Mudanças detectadas ({changes.length}):</p>
                <div className="max-h-[400px] overflow-y-auto space-y-2 border rounded-lg p-3 bg-muted/10">
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
            <p className="text-muted-foreground">Esta operação não possui rollback automático. Confirme que as mudanças estão corretas.</p>
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>Cancelar</Button>
            <ProtectedAction>
              <Button variant="destructive" onClick={apply} disabled={isApplying}>
                {isApplying ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <TriangleAlert className="w-4 h-4 mr-2" />}
                Confirmar
              </Button>
            </ProtectedAction>
          </div>
        </DialogContent>
      </Dialog>

      {/* Erro de apply */}
      <Dialog open={!!error} onOpenChange={open => !open && setError(null)}>
        <DialogContent className="max-w-3xl max-h-[85vh] bg-background border-destructive/50 border-2">
          <DialogHeader className="border-b border-destructive/30 pb-4">
            <div className="flex items-start gap-3">
              <div className="shrink-0 w-10 h-10 rounded-full bg-destructive/10 flex items-center justify-center">
                <AlertCircle className="w-6 h-6 text-destructive" />
              </div>
              <div className="flex-1 min-w-0">
                <DialogTitle className="text-destructive text-lg font-semibold">{error?.title}</DialogTitle>
                <DialogDescription className="text-muted-foreground text-sm mt-1">
                  Detalhes técnicos do erro abaixo. Campos de status são somente leitura e conflitos de field manager aparecem aqui.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="bg-destructive/5 border border-destructive/20 rounded-lg p-4">
              <pre className="text-xs font-mono text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">{error?.message}</pre>
            </div>
          </ScrollArea>
          <DialogFooter className="border-t border-border/50 pt-4">
            <Button variant="outline" onClick={() => setError(null)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
