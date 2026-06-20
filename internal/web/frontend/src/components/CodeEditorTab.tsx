import { useState, useEffect, useRef, useCallback } from "react";
import Editor, { DiffEditor, OnMount } from "@monaco-editor/react";
import type * as MonacoEditorNS from "monaco-editor";
import {
  GitBranch,
  GitCommit,
  GitPullRequest,
  Upload,
  Download,
  FolderGit2,
  ChevronRight,
  ChevronDown,
  File,
  Folder,
  FolderOpen,
  Plus,
  Trash2,
  RefreshCw,
  Search,
  X,
  AlertCircle,
  CheckCircle2,
  Clock,
  Loader2,
  ArrowRightLeft,
  Pencil,
  FolderPlus,
  FilePlus,
  GitMerge,
  RotateCcw,
  Eye,
  Map,
  PackageMinus,
  PackagePlus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  apiClient,
  type CodeEditorRepo,
  type CodeEditorFileNode,
  type CodeEditorGitStatus,
  type CodeEditorBranches,
  type CodeEditorLogEntry,
  type CodeEditorGrepMatch,
} from "@/lib/api/client";

const API_BASE = "/api/v1";

// ─── helpers ───────────────────────────────────────────────────────────────

function extToLanguage(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase() ?? "";
  const map: Record<string, string> = {
    go: "go", ts: "typescript", tsx: "typescript", js: "javascript",
    jsx: "javascript", py: "python", yaml: "yaml", yml: "yaml",
    json: "json", md: "markdown", sh: "shell", bash: "shell",
    dockerfile: "dockerfile", tf: "hcl", hcl: "hcl", toml: "ini",
    xml: "xml", html: "html", css: "css", scss: "scss",
    sql: "sql", rs: "rust", java: "java", kt: "kotlin",
    rb: "ruby", php: "php", c: "c", cpp: "cpp", h: "c",
  };
  if (filename.toLowerCase() === "dockerfile") return "dockerfile";
  if (filename.toLowerCase() === "makefile") return "makefile";
  return map[ext] ?? "plaintext";
}

function statusColor(s: string): string {
  if (s.includes("M")) return "text-yellow-400";
  if (s.includes("A")) return "text-green-400";
  if (s.includes("D")) return "text-red-400";
  if (s.includes("?")) return "text-muted-foreground";
  return "text-blue-400";
}

function statusLabel(s: string): string {
  if (s === "M " || s === " M" || s === "MM") return "M";
  if (s === "A " || s === "AM") return "A";
  if (s === "D " || s === " D") return "D";
  if (s === "??" || s === "? ") return "?";
  if (s.startsWith("R")) return "R";
  return s.trim()[0] ?? "~";
}

// ─── Multi-tab state ────────────────────────────────────────────────────────

interface OpenTab {
  node: CodeEditorFileNode;
  initialContent: string;
  currentContent: string;
  savedContent: string;
  repoId: string;
}

// ─── Toast ─────────────────────────────────────────────────────────────────

interface Toast {
  id: number;
  type: "success" | "error";
  message: string;
}

function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const counter = useRef(0);

  function addToast(type: Toast["type"], message: string) {
    const id = ++counter.current;
    setToasts(t => [...t, { id, type, message }]);
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 4000);
  }

  return { toasts, addToast };
}

function ToastContainer({ toasts }: { toasts: Toast[] }) {
  if (toasts.length === 0) return null;
  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
      {toasts.map(t => (
        <div key={t.id} className={`flex items-center gap-2 px-3 py-2 rounded shadow-lg text-xs font-medium animate-in slide-in-from-right-4 ${t.type === "success" ? "bg-green-900/90 text-green-200 border border-green-700" : "bg-red-900/90 text-red-200 border border-red-700"}`}>
          {t.type === "success" ? <CheckCircle2 className="w-3.5 h-3.5 flex-shrink-0" /> : <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />}
          <span className="max-w-xs truncate">{t.message}</span>
        </div>
      ))}
    </div>
  );
}

// ─── ResizeDivider ─────────────────────────────────────────────────────────

function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  const onMove = useCallback((e: MouseEvent) => {
    if (!dragging.current) return;
    onDrag(e.clientX - lastX.current);
    lastX.current = e.clientX;
  }, [onDrag]);

  const onUp = useCallback(() => {
    dragging.current = false;
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
  }, []);

  useEffect(() => {
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [onMove, onUp]);

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

// ─── helpers ────────────────────────────────────────────────────────────────

function flattenTree(nodes: CodeEditorFileNode[]): CodeEditorFileNode[] {
  const result: CodeEditorFileNode[] = [];
  function walk(ns: CodeEditorFileNode[]) {
    for (const n of ns) {
      if (n.type === "file") result.push(n);
      if (n.children) walk(n.children);
    }
  }
  walk(nodes);
  return result;
}

// ─── FileTreeNode ───────────────────────────────────────────────────────────

interface FileTreeNodeProps {
  node: CodeEditorFileNode;
  selectedPath: string;
  onSelect: (node: CodeEditorFileNode) => void;
  modifiedPaths: Set<string>;
  level: number;
  onDelete: (node: CodeEditorFileNode) => void;
  onRename: (node: CodeEditorFileNode) => void;
}

function FileTreeNode({ node, selectedPath, onSelect, modifiedPaths, level, onDelete, onRename }: FileTreeNodeProps) {
  const [open, setOpen] = useState(false);
  const isSelected = selectedPath === node.path;
  const isModified = modifiedPaths.has(node.path);

  if (node.type === "dir") {
    return (
      <div>
        <div
          className="w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded hover:bg-muted/50 text-left group cursor-pointer"
          style={{ paddingLeft: `${level * 12 + 4}px` }}
          onClick={() => setOpen(o => !o)}
        >
          {open ? <ChevronDown className="w-3 h-3 flex-shrink-0 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 flex-shrink-0 text-muted-foreground" />}
          {open ? <FolderOpen className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" /> : <Folder className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" />}
          <span className="truncate text-foreground/80 flex-1">{node.name}</span>
        </div>
        {open && node.children?.map(child => (
          <FileTreeNode key={child.path} node={child} selectedPath={selectedPath} onSelect={onSelect}
            modifiedPaths={modifiedPaths} level={level + 1} onDelete={onDelete} onRename={onRename} />
        ))}
      </div>
    );
  }

  return (
    <div
      className={`w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded text-left hover:bg-muted/50 group ${isSelected ? "bg-accent text-accent-foreground" : ""}`}
      style={{ paddingLeft: `${level * 12 + 4}px` }}
    >
      <button className="flex items-center gap-1 flex-1 min-w-0" onClick={() => onSelect(node)}>
        <span className="w-3 h-3 flex-shrink-0" />
        <File className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
        <span className={`truncate flex-1 ${isModified ? "text-yellow-400" : "text-foreground/80"}`}>{node.name}</span>
        {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400 flex-shrink-0" />}
      </button>
      <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
        <button onClick={e => { e.stopPropagation(); onRename(node); }} className="p-0.5 rounded hover:bg-muted" title="Renomear">
          <Pencil className="w-2.5 h-2.5 text-muted-foreground hover:text-foreground" />
        </button>
        <button onClick={e => { e.stopPropagation(); onDelete(node); }} className="p-0.5 rounded hover:bg-muted" title="Deletar">
          <Trash2 className="w-2.5 h-2.5 text-red-400/70 hover:text-red-400" />
        </button>
      </div>
    </div>
  );
}

// ─── CloneDialog ────────────────────────────────────────────────────────────

interface CloneDialogProps {
  open: boolean;
  onClose: () => void;
  onDone: (id: string) => void;
}

function CloneDialog({ open, onClose, onDone }: CloneDialogProps) {
  const [url, setUrl] = useState("");
  const [branch, setBranch] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [cloning, setCloning] = useState(false);
  const [error, setError] = useState("");
  const logsRef = useRef<HTMLDivElement>(null);

  function parseGitHubUrl(input: string): { owner: string; repo: string } | null {
    const match = input.match(/github\.com[/:]([\w.-]+)\/([\w.-]+?)(?:\.git)?(?:\/|$)/);
    if (match) return { owner: match[1], repo: match[2] };
    const parts = input.split("/").filter(Boolean);
    if (parts.length >= 2) return { owner: parts[parts.length - 2], repo: parts[parts.length - 1].replace(/\.git$/, "") };
    return null;
  }

  async function doClone() {
    const parsed = parseGitHubUrl(url);
    if (!parsed) { setError("URL inválida. Use https://github.com/owner/repo"); return; }
    setError(""); setLogs([]); setCloning(true);
    const token = localStorage.getItem("auth_token") || "";
    const res = await fetch(`${API_BASE}/code-editor/clone`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      body: JSON.stringify({ ...parsed, branch }),
    });
    if (!res.body) { setError("Sem resposta SSE"); setCloning(false); return; }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = "";
    let doneId = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const events = buf.split("\n\n");
      buf = events.pop() ?? "";
      for (const evt of events) {
        const dl = evt.split("\n").find(l => l.startsWith("data:"));
        if (!dl) continue;
        try {
          const d = JSON.parse(dl.slice(5));
          if (d.message) setLogs(l => [...l, d.message]);
          if (d.done) { if (d.error) setError(d.error); else doneId = d.id; }
        } catch (_) {}
      }
    }
    setCloning(false);
    if (doneId) { setTimeout(() => { onDone(doneId); onClose(); }, 800); }
  }

  useEffect(() => { if (logsRef.current) logsRef.current.scrollTop = logsRef.current.scrollHeight; }, [logs]);

  return (
    <Dialog open={open} onOpenChange={v => !v && !cloning && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><FolderGit2 className="w-4 h-4" />Clonar Repositório</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">URL do GitHub</label>
            <Input placeholder="https://github.com/owner/repo" value={url} onChange={e => setUrl(e.target.value)} disabled={cloning} />
          </div>
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Branch (opcional)</label>
            <Input placeholder="main" value={branch} onChange={e => setBranch(e.target.value)} disabled={cloning} />
          </div>
          {logs.length > 0 && (
            <div ref={logsRef} className="bg-black/50 rounded p-2 h-32 overflow-y-auto font-mono text-xs space-y-0.5">
              {logs.map((l, i) => <div key={i} className="text-green-300/80">{l}</div>)}
            </div>
          )}
          {error && <div className="flex items-center gap-2 text-red-400 text-xs"><AlertCircle className="w-3 h-3" />{error}</div>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={cloning}>Cancelar</Button>
          <Button onClick={doClone} disabled={cloning || !url}>
            {cloning ? <><Loader2 className="w-3 h-3 animate-spin mr-1" />Clonando...</> : "Clonar"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── CommitDialog ───────────────────────────────────────────────────────────

interface CommitDialogProps {
  open: boolean;
  repoId: string;
  status: CodeEditorGitStatus | null;
  onClose: () => void;
  onDone: () => void;
}

function CommitDialog({ open, repoId, status, onClose, onDone }: CommitDialogProps) {
  const [message, setMessage] = useState("");
  const [amend, setAmend] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [gitOutput, setGitOutput] = useState("");

  useEffect(() => {
    if (open) { setMessage(""); setError(""); setGitOutput(""); setAmend(false); }
  }, [open]);

  async function doCommit() {
    if (!amend && !message.trim()) { setError("Mensagem é obrigatória"); return; }
    setLoading(true); setError(""); setGitOutput("");
    try {
      const result = await apiClient.codeEditorCommit(repoId, message.trim(), undefined, amend);
      setGitOutput(result.message || "Commit realizado.");
      setTimeout(() => { onDone(); }, 1500);
    } catch (e: any) {
      setError(e.message || "Erro ao commitar");
    } finally {
      setLoading(false);
    }
  }

  const changedFiles = status?.files ?? [];

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <GitCommit className="w-4 h-4" />{amend ? "Emendar Último Commit" : "Novo Commit"}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          {changedFiles.length > 0 && !gitOutput && (
            <div>
              <p className="text-xs text-muted-foreground mb-1">Arquivos ({changedFiles.length}):</p>
              <ScrollArea className="h-24 border border-border/40 rounded p-2">
                {changedFiles.map(f => (
                  <div key={f.path} className="flex items-center gap-2 text-xs py-0.5">
                    <span className={`font-bold w-4 text-center ${statusColor(f.status)}`}>{statusLabel(f.status)}</span>
                    <span className="font-mono text-foreground/80 truncate">{f.path}</span>
                  </div>
                ))}
              </ScrollArea>
            </div>
          )}

          {!gitOutput && (
            <>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Mensagem do commit</label>
                <Input
                  placeholder={amend ? "Deixe vazio para manter a mensagem atual" : "feat: descrição da mudança"}
                  value={message}
                  onChange={e => setMessage(e.target.value)}
                  disabled={loading}
                  onKeyDown={e => e.key === "Enter" && !e.shiftKey && doCommit()}
                  autoFocus
                />
                <p className="text-[10px] text-muted-foreground mt-1">Sugestões: feat: · fix: · docs: · refactor: · chore:</p>
              </div>
              <label className="flex items-center gap-2 cursor-pointer">
                <input type="checkbox" checked={amend} onChange={e => setAmend(e.target.checked)} className="rounded" />
                <span className="text-xs text-muted-foreground">Emendar último commit (--amend)</span>
              </label>
            </>
          )}

          {gitOutput && (
            <div className="bg-black/40 border border-green-800/50 rounded p-3">
              <div className="flex items-center gap-2 mb-2">
                <CheckCircle2 className="w-4 h-4 text-green-400" />
                <span className="text-xs font-medium text-green-400">Commit realizado!</span>
              </div>
              <pre className="font-mono text-xs text-green-300/80 whitespace-pre-wrap">{gitOutput}</pre>
            </div>
          )}

          {error && (
            <div className="bg-red-950/30 border border-red-800/50 rounded p-2 flex items-start gap-2">
              <AlertCircle className="w-3.5 h-3.5 text-red-400 mt-0.5 flex-shrink-0" />
              <pre className="text-xs text-red-300 whitespace-pre-wrap">{error}</pre>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            {gitOutput ? "Fechar" : "Cancelar"}
          </Button>
          {!gitOutput && (
            <Button onClick={doCommit} disabled={loading || (!amend && !message.trim())}>
              {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitCommit className="w-3 h-3 mr-1" />}
              {amend ? "Emendar" : "Commitar"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── BranchDialog ───────────────────────────────────────────────────────────

interface BranchDialogProps {
  open: boolean;
  repoId: string;
  currentBranch: string;
  onClose: () => void;
  onDone: (newBranch: string) => void;
}

function BranchDialog({ open, repoId, currentBranch: cur, onClose, onDone }: BranchDialogProps) {
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [gitOutput, setGitOutput] = useState("");
  const [createdBranch, setCreatedBranch] = useState("");

  useEffect(() => {
    if (open) { setName(""); setError(""); setGitOutput(""); setCreatedBranch(""); }
  }, [open]);

  async function doCreate() {
    if (!name.trim()) { setError("Nome é obrigatório"); return; }
    setLoading(true); setError(""); setGitOutput("");
    try {
      const result = await apiClient.codeEditorCreateBranch(repoId, name.trim(), cur);
      setGitOutput(result.message || `Branch '${result.branch}' criado.`);
      setCreatedBranch(result.branch);
      setTimeout(() => { onDone(result.branch); }, 1500);
    } catch (e: any) {
      setError(e.message || "Erro ao criar branch");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><GitBranch className="w-4 h-4" />Criar Novo Branch</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            A partir de: <span className="font-mono text-foreground bg-muted px-1 py-0.5 rounded">{cur}</span>
          </p>

          {!gitOutput && (
            <Input
              placeholder="feature/nova-funcionalidade"
              value={name}
              onChange={e => setName(e.target.value)}
              disabled={loading}
              onKeyDown={e => e.key === "Enter" && doCreate()}
              autoFocus
            />
          )}

          {gitOutput && (
            <div className="bg-black/40 border border-green-800/50 rounded p-3">
              <div className="flex items-center gap-2 mb-2">
                <CheckCircle2 className="w-4 h-4 text-green-400" />
                <span className="text-xs font-medium text-green-400">Branch criado e ativo!</span>
              </div>
              <pre className="font-mono text-xs text-green-300/80 whitespace-pre-wrap">{gitOutput}</pre>
              {createdBranch && (
                <div className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                  <GitBranch className="w-3 h-3" />
                  <span className="font-mono text-foreground">{createdBranch}</span>
                </div>
              )}
            </div>
          )}

          {error && (
            <div className="bg-red-950/30 border border-red-800/50 rounded p-2 flex items-start gap-2">
              <AlertCircle className="w-3.5 h-3.5 text-red-400 mt-0.5 flex-shrink-0" />
              <pre className="text-xs text-red-300 whitespace-pre-wrap">{error}</pre>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            {gitOutput ? "Fechar" : "Cancelar"}
          </Button>
          {!gitOutput && (
            <Button onClick={doCreate} disabled={loading || !name.trim()}>
              {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitBranch className="w-3 h-3 mr-1" />}
              Criar e Alternar
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── MergeDialog ───────────────────────────────────────────────────────────

interface MergeDialogProps {
  open: boolean;
  repoId: string;
  currentBranch: string;
  branches: CodeEditorBranches | null;
  onClose: () => void;
  onDone: () => void;
}

function MergeDialog({ open, repoId, currentBranch, branches, onClose, onDone }: MergeDialogProps) {
  const [target, setTarget] = useState("");
  const [noFf, setNoFf] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [gitOutput, setGitOutput] = useState("");

  useEffect(() => { if (open) { setTarget(""); setError(""); setGitOutput(""); } }, [open]);

  const allBranches = [...(branches?.local ?? []), ...(branches?.remote ?? [])].filter(b => b !== currentBranch && !b.includes("->"));

  async function doMerge() {
    if (!target) { setError("Selecione um branch"); return; }
    setLoading(true); setError(""); setGitOutput("");
    try {
      const result = await apiClient.codeEditorMerge(repoId, target, noFf);
      setGitOutput(result.message);
      setTimeout(() => { onDone(); }, 1500);
    } catch (e: any) {
      setError(e.message || "Erro ao fazer merge");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><GitMerge className="w-4 h-4" />Merge em {currentBranch}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          {!gitOutput && (
            <>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Branch de origem</label>
                <select className="w-full text-xs bg-muted border border-border/50 rounded px-2 py-1.5 text-foreground"
                  value={target} onChange={e => setTarget(e.target.value)}>
                  <option value="">Selecionar branch...</option>
                  {allBranches.map(b => <option key={b} value={b}>{b}</option>)}
                </select>
              </div>
              <label className="flex items-center gap-2 cursor-pointer">
                <input type="checkbox" checked={noFf} onChange={e => setNoFf(e.target.checked)} className="rounded" />
                <span className="text-xs text-muted-foreground">--no-ff (sempre criar merge commit)</span>
              </label>
            </>
          )}

          {gitOutput && (
            <div className="bg-black/40 border border-green-800/50 rounded p-3">
              <CheckCircle2 className="w-4 h-4 text-green-400 mb-1" />
              <pre className="font-mono text-xs text-green-300/80 whitespace-pre-wrap">{gitOutput}</pre>
            </div>
          )}
          {error && (
            <div className="bg-red-950/30 border border-red-800/50 rounded p-2 flex items-start gap-2">
              <AlertCircle className="w-3.5 h-3.5 text-red-400 mt-0.5 flex-shrink-0" />
              <pre className="text-xs text-red-300 whitespace-pre-wrap">{error}</pre>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>{gitOutput ? "Fechar" : "Cancelar"}</Button>
          {!gitOutput && (
            <Button onClick={doMerge} disabled={loading || !target}>
              {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitMerge className="w-3 h-3 mr-1" />}
              Fazer Merge
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── CreateFileDialog ───────────────────────────────────────────────────────

interface CreateFileDialogProps {
  open: boolean;
  mode: "file" | "dir";
  repoId: string;
  onClose: () => void;
  onDone: (path: string) => void;
}

function CreateFileDialog({ open, mode, repoId, onClose, onDone }: CreateFileDialogProps) {
  const [path, setPath] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { if (open) { setPath(""); setError(""); } }, [open]);

  async function doCreate() {
    if (!path.trim()) { setError("Caminho é obrigatório"); return; }
    setLoading(true); setError("");
    try {
      if (mode === "file") {
        await apiClient.codeEditorCreateFile(repoId, path.trim());
      } else {
        await apiClient.codeEditorCreateDir(repoId, path.trim());
      }
      onDone(path.trim());
      onClose();
    } catch (e: any) {
      setError(e.message || "Erro ao criar");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {mode === "file" ? <FilePlus className="w-4 h-4" /> : <FolderPlus className="w-4 h-4" />}
            {mode === "file" ? "Criar Arquivo" : "Criar Pasta"}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Caminho (relativo ao repositório)</label>
            <Input
              placeholder={mode === "file" ? "src/utils/helper.go" : "src/utils"}
              value={path}
              onChange={e => setPath(e.target.value)}
              disabled={loading}
              onKeyDown={e => e.key === "Enter" && doCreate()}
              autoFocus
            />
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>Cancelar</Button>
          <Button onClick={doCreate} disabled={loading || !path.trim()}>
            {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : null}
            Criar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── RenameDialog ───────────────────────────────────────────────────────────

interface RenameDialogProps {
  open: boolean;
  repoId: string;
  node: CodeEditorFileNode | null;
  onClose: () => void;
  onDone: (from: string, to: string) => void;
}

function RenameDialog({ open, repoId, node, onClose, onDone }: RenameDialogProps) {
  const [newPath, setNewPath] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { if (open && node) { setNewPath(node.path); setError(""); } }, [open, node]);

  async function doRename() {
    if (!newPath.trim() || !node) return;
    setLoading(true); setError("");
    try {
      await apiClient.codeEditorRenameFile(repoId, node.path, newPath.trim());
      onDone(node.path, newPath.trim());
      onClose();
    } catch (e: any) {
      setError(e.message || "Erro ao renomear");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Pencil className="w-4 h-4" />Renomear</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground font-mono bg-muted px-2 py-1 rounded">{node?.path}</p>
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Novo caminho</label>
            <Input value={newPath} onChange={e => setNewPath(e.target.value)} disabled={loading}
              onKeyDown={e => e.key === "Enter" && doRename()} autoFocus />
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>Cancelar</Button>
          <Button onClick={doRename} disabled={loading || !newPath.trim()}>
            {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : null}Renomear
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── DiffModal ──────────────────────────────────────────────────────────────

interface DiffModalProps {
  open: boolean;
  repoId: string;
  filePath: string;
  currentContent: string;
  onClose: () => void;
}

function DiffModal({ open, repoId, filePath, currentContent, onClose }: DiffModalProps) {
  const [original, setOriginal] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open || !filePath) return;
    setLoading(true);
    apiClient.codeEditorGetOriginal(repoId, filePath)
      .then(r => setOriginal(r.content))
      .catch(() => setOriginal(""))
      .finally(() => setLoading(false));
  }, [open, repoId, filePath]);

  return (
    <Dialog open={open} onOpenChange={v => !v && onClose()}>
      <DialogContent className="max-w-5xl h-[80vh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <Eye className="w-4 h-4" />Diff — {filePath}
            <span className="text-xs text-muted-foreground font-normal ml-2">HEAD ← Atual</span>
          </DialogTitle>
        </DialogHeader>
        <div className="flex-1 min-h-0">
          {loading ? (
            <div className="flex items-center justify-center h-full"><Loader2 className="w-5 h-5 animate-spin text-muted-foreground" /></div>
          ) : (
            <DiffEditor
              original={original}
              modified={currentContent}
              language={extToLanguage(filePath.split("/").pop() ?? filePath)}
              theme="vs-dark"
              options={{
                readOnly: true,
                renderSideBySide: true,
                minimap: { enabled: false },
                fontSize: 12,
                scrollBeyondLastLine: false,
              }}
            />
          )}
        </div>
        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={onClose}>Fechar</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── SseDialog (Pull / Push) ────────────────────────────────────────────────

interface SseDialogProps {
  open: boolean;
  title: string;
  endpoint: string;
  body?: object;
  onClose: () => void;
  onDone: () => void;
}

function SseDialog({ open, title, endpoint, body, onClose, onDone }: SseDialogProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  const logsRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) { setLogs([]); setError(""); setSuccess(false); return; }
    setRunning(true);
    const token = localStorage.getItem("auth_token") || "";
    (async () => {
      const res = await fetch(`${API_BASE}${endpoint}`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        body: body ? JSON.stringify(body) : undefined,
      });
      if (!res.body) { setError("Sem resposta"); setRunning(false); return; }
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const events = buf.split("\n\n");
        buf = events.pop() ?? "";
        for (const evt of events) {
          const dl = evt.split("\n").find(l => l.startsWith("data:"));
          if (!dl) continue;
          try {
            const d = JSON.parse(dl.slice(5));
            if (d.message) setLogs(l => [...l, d.message]);
            if (d.done) { if (d.error) setError(d.error); else setSuccess(true); }
          } catch (_) {}
        }
      }
      setRunning(false);
    })().catch(e => { setError(e.message); setRunning(false); });
  }, [open]);

  useEffect(() => { if (logsRef.current) logsRef.current.scrollTop = logsRef.current.scrollHeight; }, [logs]);

  return (
    <Dialog open={open} onOpenChange={v => { if (!v && !running) onClose(); }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {running && <Loader2 className="w-4 h-4 animate-spin" />}
            {success && <CheckCircle2 className="w-4 h-4 text-green-400" />}
            {error && <AlertCircle className="w-4 h-4 text-red-400" />}
            {title}
          </DialogTitle>
        </DialogHeader>
        <div ref={logsRef} className="bg-black/50 rounded p-2 h-48 overflow-y-auto font-mono text-xs space-y-0.5">
          {logs.map((l, i) => <div key={i} className="text-green-300/80">{l}</div>)}
          {error && <div className="text-red-400">{error}</div>}
        </div>
        <DialogFooter>
          <Button onClick={() => { onClose(); if (success) onDone(); }} disabled={running}>
            {running ? "Aguarde..." : "Fechar"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── BranchesPanel (sidebar) ────────────────────────────────────────────────

interface BranchesPanelProps {
  repoId: string;
  branches: CodeEditorBranches | null;
  onRefresh: () => void;
  onCheckout: (branch: string) => Promise<void>;
  onCreateBranch: () => void;
  onMerge: () => void;
}

function BranchesPanel({ branches, onRefresh, onCheckout, onCreateBranch, onMerge }: BranchesPanelProps) {
  const [checkingOut, setCheckingOut] = useState("");
  const [filter, setFilter] = useState("");

  async function handleCheckout(branch: string) {
    if (branches?.current === branch) return;
    setCheckingOut(branch);
    await onCheckout(branch);
    setCheckingOut("");
  }

  const q = filter.toLowerCase();
  const matchBranch = (b: string) => !q || b.toLowerCase().includes(q);

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-2 py-1.5 border-b border-border/30 flex-shrink-0">
        <span className="text-xs text-muted-foreground font-medium">Branches</span>
        <div className="flex gap-1">
          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={onMerge} title="Merge">
            <GitMerge className="w-3 h-3" />
          </Button>
          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={onRefresh} title="Atualizar">
            <RefreshCw className="w-3 h-3" />
          </Button>
          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={onCreateBranch} title="Novo branch">
            <Plus className="w-3 h-3" />
          </Button>
        </div>
      </div>
      <div className="px-2 py-1.5 border-b border-border/20 flex-shrink-0">
        <div className="relative">
          <Search className="w-3 h-3 absolute left-1.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
          <input
            className="w-full pl-5 pr-2 py-0.5 text-xs bg-muted/50 border border-border/40 rounded focus:outline-none focus:ring-1 focus:ring-primary/50 text-foreground placeholder:text-muted-foreground"
            placeholder="Filtrar branches..."
            value={filter}
            onChange={e => setFilter(e.target.value)}
          />
          {filter && (
            <button onClick={() => setFilter("")} className="absolute right-1 top-1/2 -translate-y-1/2">
              <X className="w-3 h-3 text-muted-foreground hover:text-foreground" />
            </button>
          )}
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-2 space-y-3">
          {branches?.current && matchBranch(branches.current) && (
            <div>
              <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 px-1">Atual</p>
              <div className="flex items-center gap-2 px-2 py-1.5 rounded bg-primary/10 border border-primary/20">
                <GitBranch className="w-3 h-3 text-primary flex-shrink-0" />
                <span className="text-xs font-medium text-primary truncate">{branches.current}</span>
              </div>
            </div>
          )}

          {(branches?.local?.length ?? 0) > 0 && (
            <div>
              <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 px-1">Locais</p>
              <div className="space-y-0.5">
                {branches!.local.filter(b => b !== branches?.current).filter(matchBranch).map(b => (
                  <button key={b} onClick={() => handleCheckout(b)} disabled={checkingOut !== ""}
                    className="w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs hover:bg-muted/60 text-left group">
                    <GitBranch className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                    <span className="truncate flex-1 text-foreground/80">{b}</span>
                    {checkingOut === b
                      ? <Loader2 className="w-3 h-3 animate-spin text-muted-foreground" />
                      : <ArrowRightLeft className="w-3 h-3 text-muted-foreground opacity-0 group-hover:opacity-100" />}
                  </button>
                ))}
              </div>
            </div>
          )}

          {(branches?.remote?.length ?? 0) > 0 && (
            <div>
              <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 px-1">Remotos</p>
              <div className="space-y-0.5">
                {(branches!.remote ?? [])
                  .filter(r => !r.includes("->"))
                  .filter(r => !branches!.local.includes(r.replace("origin/", "")))
                  .filter(matchBranch)
                  .map(b => (
                    <button key={b} onClick={() => handleCheckout(b)} disabled={checkingOut !== ""}
                      className="w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs hover:bg-muted/60 text-left group">
                      <GitBranch className="w-3 h-3 text-muted-foreground/60 flex-shrink-0" />
                      <span className="truncate flex-1 text-muted-foreground">{b}</span>
                      {checkingOut === b
                        ? <Loader2 className="w-3 h-3 animate-spin text-muted-foreground" />
                        : <Download className="w-3 h-3 text-muted-foreground opacity-0 group-hover:opacity-100" />}
                    </button>
                  ))}
              </div>
            </div>
          )}

          {!branches && (
            <p className="text-xs text-muted-foreground text-center py-4">Selecione um repositório</p>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

// ─── Main CodeEditorTab ─────────────────────────────────────────────────────

export function CodeEditorTab() {
  const [repos, setRepos] = useState<CodeEditorRepo[]>([]);
  const [selectedRepo, setSelectedRepo] = useState<CodeEditorRepo | null>(null);
  const [tree, setTree] = useState<CodeEditorFileNode[]>([]);

  // Multi-tab state
  const [openTabs, setOpenTabs] = useState<OpenTab[]>([]);
  const [activeTabIdx, setActiveTabIdx] = useState(0);

  const [status, setStatus] = useState<CodeEditorGitStatus | null>(null);
  const [branches, setBranches] = useState<CodeEditorBranches | null>(null);
  const [log, setLog] = useState<CodeEditorLogEntry[]>([]);

  // Search / grep
  const [searchQuery, setSearchQuery] = useState("");
  const [grepMode, setGrepMode] = useState(false);
  const [grepResults, setGrepResults] = useState<CodeEditorGrepMatch[]>([]);

  // Sidebar
  const [sidePanel, setSidePanel] = useState<"files" | "branches" | "git" | "log">("files");
  const [sidebarWidth, setSidebarWidth] = useState(() => {
    const saved = localStorage.getItem("ce_sidebar_width");
    return saved ? parseInt(saved) : 224;
  });

  // Editor options
  const [showMinimap, setShowMinimap] = useState(false);

  // Stash state (feedback)
  const [stashLoading, setStashLoading] = useState<"push" | "pop" | null>(null);

  // Dialogs
  const [showClone, setShowClone] = useState(false);
  const [showCommit, setShowCommit] = useState(false);
  const [showBranch, setShowBranch] = useState(false);
  const [showMerge, setShowMerge] = useState(false);
  const [sseDialog, setSseDialog] = useState<{ title: string; endpoint: string; body?: object } | null>(null);
  const [createDialog, setCreateDialog] = useState<{ mode: "file" | "dir" } | null>(null);
  const [renameNode, setRenameNode] = useState<CodeEditorFileNode | null>(null);
  const [diffFile, setDiffFile] = useState<string | null>(null);

  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const { toasts, addToast } = useToasts();

  const activeTab = openTabs[activeTabIdx] ?? null;
  const isModified = activeTab ? activeTab.currentContent !== activeTab.savedContent : false;
  const modifiedPaths = new Set((status?.files ?? []).map(f => f.path));

  // ── persist sidebar width ──
  useEffect(() => {
    localStorage.setItem("ce_sidebar_width", String(sidebarWidth));
  }, [sidebarWidth]);

  // ── persist selected repo ──
  useEffect(() => {
    if (selectedRepo) {
      localStorage.setItem("ce_last_repo", selectedRepo.id);
    }
  }, [selectedRepo?.id]);

  // ── carregamento inicial ──
  useEffect(() => {
    loadRepos();
  }, []);

  async function loadRepos() {
    try {
      const fresh = await apiClient.codeEditorListRepos();
      setRepos(fresh);
      // Restaurar último repo usado
      const lastId = localStorage.getItem("ce_last_repo");
      if (lastId) {
        const found = fresh.find(r => r.id === lastId);
        if (found) selectRepo(found);
      }
    } catch (_) {}
  }

  async function selectRepo(repo: CodeEditorRepo) {
    setSelectedRepo(repo);
    setOpenTabs([]);
    setActiveTabIdx(0);
    setSearchQuery("");
    setGrepResults([]); setGrepMode(false);
    await Promise.all([loadTree(repo.id), loadStatus(repo.id), loadBranches(repo.id), loadLog(repo.id)]);
  }

  async function loadTree(id: string) {
    try { setTree(await apiClient.codeEditorGetFileTree(id)); } catch (_) {}
  }

  async function loadStatus(id: string) {
    try { setStatus(await apiClient.codeEditorGetStatus(id)); } catch (_) {}
  }

  async function loadBranches(id: string) {
    try { setBranches(await apiClient.codeEditorGetBranches(id)); } catch (_) {}
  }

  async function loadLog(id: string) {
    try { setLog(await apiClient.codeEditorGetLog(id)); } catch (_) {}
  }

  async function openFile(node: CodeEditorFileNode) {
    if (!selectedRepo) return;
    const repoId = selectedRepo.id;
    // Já aberto? Ativar a aba existente
    const existingIdx = openTabs.findIndex(t => t.repoId === repoId && t.node.path === node.path);
    if (existingIdx >= 0) {
      setActiveTabIdx(existingIdx);
      return;
    }
    try {
      const { content } = await apiClient.codeEditorReadFile(repoId, node.path);
      const newTab: OpenTab = { node, initialContent: content, currentContent: content, savedContent: content, repoId };
      setOpenTabs(prev => {
        const updated = [...prev, newTab];
        setActiveTabIdx(updated.length - 1);
        return updated;
      });
    } catch (e: any) {
      addToast("error", "Erro ao abrir: " + e.message);
    }
  }

  function closeTab(idx: number) {
    const tab = openTabs[idx];
    if (tab && tab.currentContent !== tab.savedContent) {
      if (!confirm(`"${tab.node.name}" tem mudanças não salvas. Fechar mesmo assim?`)) return;
    }
    setOpenTabs(prev => {
      const updated = prev.filter((_, i) => i !== idx);
      if (activeTabIdx >= updated.length) {
        setActiveTabIdx(Math.max(0, updated.length - 1));
      } else if (idx < activeTabIdx) {
        setActiveTabIdx(activeTabIdx - 1);
      }
      return updated;
    });
  }

  function updateTabContent(value: string | undefined) {
    if (value === undefined) return;
    setOpenTabs(prev => prev.map((t, i) => i === activeTabIdx ? { ...t, currentContent: value } : t));
  }

  async function saveFile() {
    if (!activeTab || !isModified) return;
    try {
      await apiClient.codeEditorWriteFile(activeTab.repoId, activeTab.node.path, activeTab.currentContent);
      setOpenTabs(prev => prev.map((t, i) => i === activeTabIdx ? { ...t, savedContent: t.currentContent } : t));
      await loadStatus(activeTab.repoId);
      addToast("success", `Salvo: ${activeTab.node.name}`);
    } catch (e: any) {
      addToast("error", "Erro ao salvar: " + e.message);
    }
  }

  async function handleCheckout(branch: string) {
    if (!selectedRepo) return;
    // Confirmar se há mudanças não salvas em alguma aba
    const hasUnsaved = openTabs.some(t => t.currentContent !== t.savedContent);
    if (hasUnsaved && !confirm("Há arquivos com mudanças não salvas. Alternar branch vai descartá-las. Continuar?")) {
      throw new Error("cancelado");
    }
    try {
      const result = await apiClient.codeEditorCheckoutBranch(selectedRepo.id, branch);
      addToast("success", `Alternado para: ${result.branch}`);
      setOpenTabs([]);
      setActiveTabIdx(0);
      await Promise.all([loadStatus(selectedRepo.id), loadBranches(selectedRepo.id), loadTree(selectedRepo.id)]);
    } catch (e: any) {
      if (e.message !== "cancelado") {
        addToast("error", e.message || "Erro ao alternar branch");
      }
      throw e;
    }
  }

  // Busca por nome: client-side em tempo real (sem API)
  const q = searchQuery.toLowerCase().trim();
  const fileMatches: CodeEditorFileNode[] = selectedRepo && q && !grepMode
    ? flattenTree(tree).filter(f => f.path.toLowerCase().includes(q))
    : [];

  // Busca por conteúdo (grep): requer Enter
  async function handleGrepSearch() {
    if (!selectedRepo || !searchQuery.trim() || !grepMode) return;
    try {
      const r = await apiClient.codeEditorGrepFiles(selectedRepo.id, searchQuery);
      setGrepResults(r.matches ?? []);
    } catch (_) {}
  }

  async function handleDeleteFile(node: CodeEditorFileNode) {
    if (!selectedRepo) return;
    if (!confirm(`Deletar "${node.path}"?`)) return;
    try {
      await apiClient.codeEditorDeleteFile(selectedRepo.id, node.path);
      // Fechar a aba se estiver aberta
      const tabIdx = openTabs.findIndex(t => t.node.path === node.path);
      if (tabIdx >= 0) closeTab(tabIdx);
      await loadTree(selectedRepo.id);
      await loadStatus(selectedRepo.id);
      addToast("success", `Deletado: ${node.name}`);
    } catch (e: any) {
      addToast("error", e.message || "Erro ao deletar");
    }
  }

  async function handleRenameFile(from: string, to: string) {
    if (!selectedRepo) return;
    // Atualiza a aba se estiver aberta
    const tabIdx = openTabs.findIndex(t => t.node.path === from);
    if (tabIdx >= 0) {
      setOpenTabs(prev => prev.map((t, i) => i === tabIdx ? {
        ...t,
        node: { ...t.node, path: to, name: to.split("/").pop() ?? to },
      } : t));
    }
    await loadTree(selectedRepo.id);
    await loadStatus(selectedRepo.id);
    addToast("success", `Renomeado: ${to}`);
  }

  async function handleResetFile(filePath: string) {
    if (!selectedRepo) return;
    if (!confirm(`Descartar mudanças em "${filePath}"?`)) return;
    try {
      await apiClient.codeEditorResetFile(selectedRepo.id, filePath);
      // Recarrega a aba se estiver aberta
      const tabIdx = openTabs.findIndex(t => t.node.path === filePath);
      if (tabIdx >= 0) {
        const { content } = await apiClient.codeEditorReadFile(selectedRepo.id, filePath);
        setOpenTabs(prev => prev.map((t, i) => i === tabIdx ? {
          ...t, initialContent: content, currentContent: content, savedContent: content,
        } : t));
      }
      await loadStatus(selectedRepo.id);
      addToast("success", `Revertido: ${filePath}`);
    } catch (e: any) {
      addToast("error", e.message || "Erro ao reverter");
    }
  }

  async function handleStash(action: "push" | "pop") {
    if (!selectedRepo) return;
    setStashLoading(action);
    try {
      if (action === "push") {
        const r = await apiClient.codeEditorStash(selectedRepo.id);
        setOpenTabs([]);
        setActiveTabIdx(0);
        addToast("success", r.message || "Stash criado");
      } else {
        const r = await apiClient.codeEditorStashPop(selectedRepo.id);
        addToast("success", r.message || "Stash aplicado");
      }
      await Promise.all([loadStatus(selectedRepo.id), loadTree(selectedRepo.id)]);
    } catch (e: any) {
      addToast("error", e.message || `Erro no stash ${action}`);
    } finally {
      setStashLoading(null);
    }
  }

  async function deleteRepo(id: string) {
    if (!confirm(`Remover repositório "${id}" localmente?`)) return;
    try {
      await apiClient.codeEditorDeleteRepo(id);
      if (selectedRepo?.id === id) {
        setSelectedRepo(null); setTree([]); setOpenTabs([]); setActiveTabIdx(0);
        setStatus(null); setBranches(null); setLog([]);
        localStorage.removeItem("ce_last_repo");
      }
      await loadRepos();
      addToast("success", `Repositório ${id} removido`);
    } catch (e: any) { addToast("error", e.message); }
  }

  const [formatting, setFormatting] = useState(false);

  async function formatFile() {
    if (!selectedRepo || !activeTab) return;
    setFormatting(true);
    try {
      const r = await apiClient.codeEditorFormatFile(
        selectedRepo.id,
        activeTab.node.path,
        activeTab.currentContent,
      );
      updateTabContent(r.content);
      addToast("success", "Formatado com sucesso");
    } catch (e: any) {
      addToast("error", e.message || "Erro ao formatar");
    } finally {
      setFormatting(false);
    }
  }

  const handleEditorMount: OnMount = (editor) => {
    editorRef.current = editor;
    editor.addCommand(2048 | 49, () => saveFile()); // Ctrl+S
    editor.addCommand(512 | 1024 | 36, () => formatFile()); // Shift+Alt+F
  };

  const sidePanels = [
    { id: "files" as const, label: "Arquivos" },
    { id: "branches" as const, label: `Branches${branches ? ` (${branches.local.length})` : ""}` },
    { id: "git" as const, label: `Git${modifiedPaths.size > 0 ? ` (${modifiedPaths.size})` : ""}` },
    { id: "log" as const, label: "Log" },
  ];

  return (
    <div className="flex flex-col h-full min-h-0 bg-background">
      {/* ── Header ── */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border/50 flex-shrink-0 bg-card/30">
        <FolderGit2 className="w-4 h-4 text-blue-400 flex-shrink-0" />
        <span className="text-sm font-medium">Editor de Código</span>

        {repos.length > 0 && (
          <select
            className="ml-2 text-xs bg-muted border border-border/50 rounded px-2 py-1 text-foreground max-w-48"
            value={selectedRepo?.id ?? ""}
            onChange={e => { const r = repos.find(x => x.id === e.target.value); if (r) selectRepo(r); }}
          >
            <option value="">Selecionar repositório...</option>
            {repos.map(r => <option key={r.id} value={r.id}>{r.owner}/{r.repo}</option>)}
          </select>
        )}

        {selectedRepo && branches?.current && (
          <button
            onClick={() => setSidePanel("branches")}
            className="flex items-center gap-1 text-xs bg-muted/60 border border-border/50 rounded px-2 py-1 hover:bg-muted text-foreground/80"
            title="Ver todos os branches"
          >
            <GitBranch className="w-3 h-3 text-primary" />
            <span className="font-mono">{branches.current}</span>
          </button>
        )}

        <div className="flex-1" />

        {/* Ações Git */}
        {selectedRepo && (
          <>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => setSseDialog({ title: "Git Pull", endpoint: `/code-editor/repos/${selectedRepo.id}/pull` })}>
              <Download className="w-3 h-3" />Pull
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => { setSidePanel("git"); setShowCommit(true); }}
              disabled={modifiedPaths.size === 0}>
              <GitCommit className="w-3 h-3" />Commit
              {modifiedPaths.size > 0 && <span className="bg-yellow-500 text-black text-[10px] px-1 rounded-full">{modifiedPaths.size}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => setSseDialog({ title: "Git Push", endpoint: `/code-editor/repos/${selectedRepo.id}/push` })}>
              <Upload className="w-3 h-3" />Push
              {status?.ahead && status.ahead !== "0" && <span className="bg-blue-500 text-white text-[10px] px-1 rounded-full">{status.ahead}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => { setSidePanel("branches"); setShowBranch(true); }}>
              <Plus className="w-3 h-3" />Branch
            </Button>
          </>
        )}

        <Button size="sm" className="h-6 text-xs gap-1" onClick={() => setShowClone(true)}>
          <GitPullRequest className="w-3 h-3" />Clonar
        </Button>
      </div>

      {/* ── Body ── */}
      <div className="flex flex-1 min-h-0">
        {/* Sidebar */}
        <div className="flex-shrink-0 flex flex-col min-h-0 overflow-hidden" style={{ width: sidebarWidth }}>
          {/* Tabs da sidebar */}
          <div className="flex border-b border-border/50 flex-shrink-0 overflow-x-auto">
            {sidePanels.map(p => (
              <button key={p.id} onClick={() => setSidePanel(p.id)}
                className={`flex-shrink-0 px-2 py-1.5 text-[11px] font-medium transition-colors whitespace-nowrap ${sidePanel === p.id ? "text-foreground border-b-2 border-primary" : "text-muted-foreground hover:text-foreground"}`}>
                {p.label}
              </button>
            ))}
          </div>

          {/* Conteúdo da sidebar */}
          <div className="flex-1 min-h-0 overflow-hidden flex flex-col">

            {/* ── Painel: Arquivos ── */}
            {sidePanel === "files" && (
              <>
                {selectedRepo && (
                  <div className="px-2 py-1.5 flex gap-1 flex-shrink-0 border-b border-border/30">
                    <Input
                      className="h-6 text-xs"
                      placeholder={grepMode ? "Buscar no conteúdo... (Enter)" : "Filtrar por nome..."}
                      value={searchQuery}
                      onChange={e => { setSearchQuery(e.target.value); if (grepMode) setGrepResults([]); }}
                      onKeyDown={e => e.key === "Enter" && handleGrepSearch()}
                    />
                    {searchQuery && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0 flex-shrink-0"
                        title="Limpar busca"
                        onClick={() => { setSearchQuery(""); setGrepResults([]); }}
                      >
                        <X className="w-3 h-3" />
                      </Button>
                    )}
                    <Button
                      variant={grepMode ? "default" : "ghost"}
                      size="sm"
                      className="h-6 w-6 p-0 flex-shrink-0"
                      title={grepMode ? "Modo: conteúdo (clique para nome)" : "Modo: nome (clique para conteúdo)"}
                      onClick={() => { setGrepMode(m => !m); setSearchQuery(""); setGrepResults([]); }}
                    >
                      <Search className="w-3 h-3" />
                    </Button>
                  </div>
                )}
                <ScrollArea className="flex-1">
                  {!selectedRepo && (
                    <div className="p-2 space-y-1">
                      {repos.length === 0 ? (
                        <p className="text-xs text-muted-foreground text-center py-4">Nenhum repo clonado.<br />Clique em "Clonar".</p>
                      ) : repos.map(r => (
                        <div key={r.id} className="group flex items-center gap-1 rounded hover:bg-muted/50 px-1 py-1">
                          <button className="flex-1 text-left text-xs truncate" onClick={() => selectRepo(r)}>
                            <span className="font-medium">{r.owner}/{r.repo}</span>
                            <span className="text-muted-foreground block text-[10px]">{r.current_branch}</span>
                          </button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0 opacity-0 group-hover:opacity-100" onClick={() => deleteRepo(r.id)}>
                            <Trash2 className="w-3 h-3 text-red-400" />
                          </Button>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Resultados grep (conteúdo) */}
                  {selectedRepo && grepMode && (
                    <div className="p-1 space-y-0.5">
                      {grepResults.length === 0 && searchQuery && (
                        <p className="text-[10px] text-muted-foreground px-2 py-2">Pressione Enter para buscar no conteúdo</p>
                      )}
                      {grepResults.slice(0, 100).map((m, i) => (
                        <button key={i} className="w-full text-left px-2 py-1 text-xs hover:bg-muted/50 rounded"
                          onClick={() => openFile({ name: m.file.split("/").pop() ?? m.file, path: m.file, type: "file" })}>
                          <div className="font-mono text-foreground/80 truncate">{m.file}:{m.line}</div>
                          <div className="text-muted-foreground truncate text-[10px]">{m.content}</div>
                        </button>
                      ))}
                    </div>
                  )}

                  {/* Resultados busca por nome — client-side, tempo real */}
                  {selectedRepo && !grepMode && q && (
                    <div className="p-1 space-y-0.5">
                      {fileMatches.length === 0 ? (
                        <p className="text-xs text-muted-foreground px-2 py-2">Nenhum arquivo encontrado</p>
                      ) : fileMatches.map(f => {
                        // Destaca a parte que bate com a query
                        const lower = f.path.toLowerCase();
                        const idx = lower.indexOf(q);
                        return (
                          <button key={f.path} className="w-full text-left px-2 py-0.5 text-xs hover:bg-muted/50 rounded font-mono"
                            onClick={() => openFile(f)}>
                            {idx >= 0 ? (
                              <span>
                                <span className="text-foreground/60">{f.path.slice(0, idx)}</span>
                                <span className="text-yellow-300 font-semibold">{f.path.slice(idx, idx + q.length)}</span>
                                <span className="text-foreground/60">{f.path.slice(idx + q.length)}</span>
                              </span>
                            ) : (
                              <span className="text-foreground/80 truncate">{f.path}</span>
                            )}
                          </button>
                        );
                      })}
                      <p className="text-[10px] text-muted-foreground px-2">{fileMatches.length} resultado{fileMatches.length !== 1 ? "s" : ""}</p>
                    </div>
                  )}

                  {/* Árvore de arquivos — sempre visível quando não há filtro de nome ativo */}
                  {selectedRepo && !grepMode && (
                    <div className="p-1" style={{ display: q ? "none" : undefined }}>
                      <div className="flex items-center justify-between px-1 mb-1">
                        <span className="text-xs text-muted-foreground font-medium truncate">{selectedRepo.owner}/{selectedRepo.repo}</span>
                        <div className="flex gap-1 flex-shrink-0">
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => setCreateDialog({ mode: "file" })} title="Novo arquivo"><FilePlus className="w-3 h-3" /></Button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => setCreateDialog({ mode: "dir" })} title="Nova pasta"><FolderPlus className="w-3 h-3" /></Button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => loadTree(selectedRepo.id)}><RefreshCw className="w-3 h-3" /></Button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => { setSelectedRepo(null); setTree([]); setOpenTabs([]); setActiveTabIdx(0); }}><X className="w-3 h-3" /></Button>
                        </div>
                      </div>
                      {tree.map(node => (
                        <FileTreeNode key={node.path} node={node} selectedPath={activeTab?.node.path ?? ""}
                          onSelect={openFile} modifiedPaths={modifiedPaths} level={0}
                          onDelete={handleDeleteFile} onRename={n => setRenameNode(n)} />
                      ))}
                    </div>
                  )}
                </ScrollArea>
              </>
            )}

            {/* ── Painel: Branches ── */}
            {sidePanel === "branches" && (
              <BranchesPanel
                repoId={selectedRepo?.id ?? ""}
                branches={selectedRepo ? branches : null}
                onRefresh={() => selectedRepo && loadBranches(selectedRepo.id)}
                onCheckout={handleCheckout}
                onCreateBranch={() => setShowBranch(true)}
                onMerge={() => setShowMerge(true)}
              />
            )}

            {/* ── Painel: Git ── */}
            {sidePanel === "git" && (
              <div className="flex flex-col h-full">
                <ScrollArea className="flex-1">
                  <div className="p-2 space-y-3">
                    {selectedRepo && status ? (
                      <>
                        <div>
                          <p className="text-xs font-medium text-muted-foreground mb-1">
                            Alterações ({status.files.length})
                          </p>
                          {status.files.length === 0 ? (
                            <p className="text-xs text-muted-foreground italic">Árvore limpa</p>
                          ) : status.files.map(f => (
                            <div key={f.path} className="flex items-center gap-1 py-0.5 group">
                              <span className={`text-xs font-bold w-4 text-center flex-shrink-0 ${statusColor(f.status)}`}>{statusLabel(f.status)}</span>
                              <button className="text-xs font-mono truncate hover:text-foreground text-foreground/70 flex-1 text-left min-w-0"
                                onClick={() => openFile({ name: f.path.split("/").pop() ?? f.path, path: f.path, type: "file" })}>
                                {f.path}
                              </button>
                              <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
                                <button onClick={() => setDiffFile(f.path)} title="Ver diff" className="p-0.5 rounded hover:bg-muted">
                                  <Eye className="w-2.5 h-2.5 text-muted-foreground hover:text-foreground" />
                                </button>
                                <button onClick={() => handleResetFile(f.path)} title="Descartar mudanças" className="p-0.5 rounded hover:bg-muted">
                                  <RotateCcw className="w-2.5 h-2.5 text-red-400/70 hover:text-red-400" />
                                </button>
                              </div>
                            </div>
                          ))}
                        </div>
                        {(status.ahead !== "0" || status.behind !== "0") && (
                          <div className="text-xs text-muted-foreground border border-border/30 rounded px-2 py-1">
                            {status.ahead !== "0" && <span className="text-blue-400">↑{status.ahead} à frente </span>}
                            {status.behind !== "0" && <span className="text-yellow-400">↓{status.behind} atrás</span>}
                          </div>
                        )}
                        {/* Stash */}
                        <div className="border-t border-border/30 pt-2 flex gap-1">
                          <Button variant="ghost" size="sm" className="flex-1 h-6 text-[11px]" onClick={() => handleStash("push")}
                            disabled={stashLoading !== null || status.files.length === 0}>
                            {stashLoading === "push" ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <PackageMinus className="w-3 h-3 mr-1" />}
                            Stash
                          </Button>
                          <Button variant="ghost" size="sm" className="flex-1 h-6 text-[11px]" onClick={() => handleStash("pop")}
                            disabled={stashLoading !== null}>
                            {stashLoading === "pop" ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <PackagePlus className="w-3 h-3 mr-1" />}
                            Pop
                          </Button>
                        </div>
                      </>
                    ) : (
                      <p className="text-xs text-muted-foreground text-center py-4">Selecione um repositório</p>
                    )}
                  </div>
                </ScrollArea>
                {selectedRepo && (
                  <div className="flex gap-1 p-2 border-t border-border/30 flex-shrink-0">
                    <Button variant="outline" size="sm" className="flex-1 h-6 text-xs" onClick={() => loadStatus(selectedRepo.id)}>
                      <RefreshCw className="w-3 h-3 mr-1" />Atualizar
                    </Button>
                    <Button size="sm" className="flex-1 h-6 text-xs" onClick={() => setShowCommit(true)} disabled={modifiedPaths.size === 0}>
                      <GitCommit className="w-3 h-3 mr-1" />Commit
                    </Button>
                  </div>
                )}
              </div>
            )}

            {/* ── Painel: Log ── */}
            {sidePanel === "log" && (
              <div className="flex flex-col h-full">
                <ScrollArea className="flex-1">
                  <div className="p-2 space-y-2">
                    {selectedRepo && (
                      <Button variant="ghost" size="sm" className="w-full h-6 text-xs" onClick={() => loadLog(selectedRepo.id)}>
                        <RefreshCw className="w-3 h-3 mr-1" />Atualizar
                      </Button>
                    )}
                    {log.map(entry => (
                      <div key={entry.hash} className="border-b border-border/30 pb-2">
                        <p className="text-xs text-foreground/90 leading-tight">{entry.message}</p>
                        <div className="flex items-center gap-1 mt-0.5">
                          <Clock className="w-2.5 h-2.5 text-muted-foreground" />
                          <span className="text-[10px] text-muted-foreground">{entry.when} · {entry.author}</span>
                        </div>
                        <span className="font-mono text-[10px] text-muted-foreground/60">{entry.hash.slice(0, 7)}</span>
                      </div>
                    ))}
                    {log.length === 0 && <p className="text-xs text-muted-foreground text-center py-4">Sem commits</p>}
                  </div>
                </ScrollArea>
              </div>
            )}
          </div>
        </div>

        <ResizeDivider onDrag={d => setSidebarWidth(w => Math.max(160, Math.min(520, w + d)))} />

        {/* ── Área do editor ── */}
        <div className="flex-1 flex flex-col min-h-0 min-w-0">
          {/* Barra de abas */}
          {openTabs.length > 0 && (
            <div className="flex items-center border-b border-border/50 flex-shrink-0 bg-card/10 overflow-x-auto">
              {openTabs.map((tab, idx) => {
                const unsaved = tab.currentContent !== tab.savedContent;
                return (
                  <div
                    key={`${tab.repoId}/${tab.node.path}`}
                    className={`flex items-center gap-1.5 px-3 py-1.5 text-xs border-r border-border/30 cursor-pointer flex-shrink-0 group ${idx === activeTabIdx ? "bg-background border-b-2 border-b-primary text-foreground" : "text-muted-foreground hover:text-foreground hover:bg-muted/30"}`}
                    onClick={() => setActiveTabIdx(idx)}
                    style={{ borderBottom: idx === activeTabIdx ? "2px solid hsl(var(--primary))" : undefined }}
                  >
                    <File className="w-3 h-3 flex-shrink-0" />
                    <span className="truncate max-w-32">{tab.node.name}</span>
                    {unsaved && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400 flex-shrink-0" />}
                    <button
                      onClick={e => { e.stopPropagation(); closeTab(idx); }}
                      className="w-3.5 h-3.5 rounded hover:bg-muted flex items-center justify-center opacity-0 group-hover:opacity-100 flex-shrink-0"
                    >
                      <X className="w-2.5 h-2.5" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          {activeTab ? (
            <>
              {/* Barra do arquivo ativo */}
              <div className="flex items-center gap-2 px-3 py-1 border-b border-border/50 flex-shrink-0 bg-card/20">
                <span className="text-xs font-mono text-muted-foreground truncate flex-1">{activeTab.node.path}</span>
                {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400 flex-shrink-0" title="Não salvo" />}
                <div className="flex gap-1 flex-shrink-0">
                  <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title={showMinimap ? "Ocultar minimap" : "Mostrar minimap"}
                    onClick={() => setShowMinimap(m => !m)}>
                    <Map className="w-3 h-3" />
                  </Button>
                  <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Ver diff"
                    onClick={() => setDiffFile(activeTab.node.path)}>
                    <Eye className="w-3 h-3" />
                  </Button>
                  <Button variant="ghost" size="sm" className="h-6 text-xs gap-1 px-2" title="Formatar arquivo (Shift+Alt+F)" onClick={formatFile} disabled={formatting}>
                    {formatting ? <RefreshCw className="w-3 h-3 animate-spin" /> : <span className="font-mono font-bold text-[11px]">Fmt</span>}
                  </Button>
                  <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={saveFile} disabled={!isModified}>
                    <CheckCircle2 className="w-3 h-3" />Salvar <span className="text-muted-foreground text-[10px]">Ctrl+S</span>
                  </Button>
                </div>
              </div>
              <div className="flex-1 min-h-0">
                <Editor
                  height="100%"
                  language={extToLanguage(activeTab.node.name)}
                  value={activeTab.currentContent}
                  theme="vs-dark"
                  onMount={handleEditorMount}
                  onChange={updateTabContent}
                  options={{
                    automaticLayout: true,
                    minimap: { enabled: showMinimap },
                    fontSize: 13,
                    lineHeight: 20,
                    fontFamily: "'Cascadia Code','Fira Code','Consolas','Courier New',monospace",
                    fontLigatures: true,
                    wordWrap: "on",
                    tabSize: 2,
                    scrollBeyondLastLine: false,
                    smoothScrolling: true,
                    cursorSmoothCaretAnimation: "on",
                    renderLineHighlight: "all",
                    scrollbar: { vertical: "visible", horizontal: "visible", verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
                    suggest: { showIcons: true, showSnippets: true },
                    quickSuggestions: true,
                  }}
                />
              </div>
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-muted-foreground">
              <div className="text-center space-y-3">
                <FolderGit2 className="w-12 h-12 mx-auto opacity-20" />
                {selectedRepo ? (
                  <p className="text-sm">Selecione um arquivo na aba "Arquivos"</p>
                ) : (
                  <>
                    <p className="text-sm">Nenhum repositório selecionado</p>
                    <Button size="sm" onClick={() => setShowClone(true)}>
                      <GitPullRequest className="w-3.5 h-3.5 mr-1.5" />Clonar repositório
                    </Button>
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* ── Dialogs ── */}
      <CloneDialog
        open={showClone}
        onClose={() => setShowClone(false)}
        onDone={async id => {
          const fresh = await apiClient.codeEditorListRepos();
          setRepos(fresh);
          const found = fresh.find(x => x.id === id);
          if (found) selectRepo(found);
        }}
      />

      {selectedRepo && (
        <>
          <CommitDialog
            open={showCommit}
            repoId={selectedRepo.id}
            status={status}
            onClose={() => setShowCommit(false)}
            onDone={async () => {
              setShowCommit(false);
              await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id)]);
              addToast("success", "Commit criado com sucesso");
            }}
          />

          <BranchDialog
            open={showBranch}
            repoId={selectedRepo.id}
            currentBranch={branches?.current ?? ""}
            onClose={() => setShowBranch(false)}
            onDone={async (newBranch) => {
              setShowBranch(false);
              setOpenTabs([]);
              setActiveTabIdx(0);
              await Promise.all([loadBranches(selectedRepo.id), loadStatus(selectedRepo.id), loadTree(selectedRepo.id)]);
              addToast("success", `Agora em: ${newBranch}`);
            }}
          />

          <MergeDialog
            open={showMerge}
            repoId={selectedRepo.id}
            currentBranch={branches?.current ?? ""}
            branches={branches}
            onClose={() => setShowMerge(false)}
            onDone={async () => {
              setShowMerge(false);
              setOpenTabs([]);
              setActiveTabIdx(0);
              await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id), loadTree(selectedRepo.id)]);
              addToast("success", "Merge concluído");
            }}
          />

          <CreateFileDialog
            open={!!createDialog}
            mode={createDialog?.mode ?? "file"}
            repoId={selectedRepo.id}
            onClose={() => setCreateDialog(null)}
            onDone={async (path) => {
              await loadTree(selectedRepo.id);
              if (createDialog?.mode === "file") {
                openFile({ name: path.split("/").pop() ?? path, path, type: "file" });
              }
              addToast("success", `Criado: ${path}`);
            }}
          />

          <RenameDialog
            open={!!renameNode}
            repoId={selectedRepo.id}
            node={renameNode}
            onClose={() => setRenameNode(null)}
            onDone={handleRenameFile}
          />

          {diffFile && (
            <DiffModal
              open={true}
              repoId={selectedRepo.id}
              filePath={diffFile}
              currentContent={openTabs.find(t => t.node.path === diffFile)?.currentContent ?? ""}
              onClose={() => setDiffFile(null)}
            />
          )}

          {sseDialog && (
            <SseDialog
              open={true}
              title={sseDialog.title}
              endpoint={sseDialog.endpoint}
              body={sseDialog.body}
              onClose={() => setSseDialog(null)}
              onDone={async () => {
                setSseDialog(null);
                await Promise.all([loadStatus(selectedRepo.id), loadBranches(selectedRepo.id), loadLog(selectedRepo.id)]);
              }}
            />
          )}
        </>
      )}

      <ToastContainer toasts={toasts} />
    </div>
  );
}
