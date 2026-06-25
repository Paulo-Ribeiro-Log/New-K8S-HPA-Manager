import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import Editor, { DiffEditor, OnMount } from "@monaco-editor/react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Terminal as XTerm } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";
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
  Map as MapIcon,
  PackageMinus,
  PackagePlus,
  Tag,
  ChevronsUpDown,
  Terminal,
  Cherry,
  Type,
  History,
  Replace,
  User,
  Key,
  BookOpen,
  GitCompare,
  Columns2,
  ShieldAlert,
  Copy,
  ExternalLink,
  WrapText,
  Locate,
  Layers,
  Play,
  FlaskConical,
  ServerCrash,
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
  type CodeEditorBlameLine,
  type CodeEditorFileLogEntry,
  type CodeEditorReplaceRequest,
  type CodeEditorReplaceMatch,
  type GitHubEditorProfile,
} from "@/lib/api/client";

const API_BASE = "/api/v1";

// ─── Markdown components ──────────────────────────────────────────────────────

const MD_COMPONENTS: Components = {
  h1: ({ children }) => (
    <h1 className="text-2xl font-black text-slate-100 mt-6 mb-3 pb-1 border-b border-slate-700 leading-tight first:mt-0">
      {children}
    </h1>
  ),
  h2: ({ children }) => (
    <h2 className="text-xl font-bold text-blue-300 mt-5 mb-2 leading-tight">{children}</h2>
  ),
  h3: ({ children }) => (
    <h3 className="text-base font-semibold text-emerald-300 mt-4 mb-1.5 leading-tight">{children}</h3>
  ),
  h4: ({ children }) => (
    <h4 className="text-sm font-semibold text-amber-300 mt-3 mb-1 leading-tight">{children}</h4>
  ),
  h5: ({ children }) => (
    <h5 className="text-sm font-medium text-pink-300 mt-2 mb-1 leading-tight">{children}</h5>
  ),
  h6: ({ children }) => (
    <h6 className="text-xs font-medium text-pink-300 mt-2 mb-1 leading-tight">{children}</h6>
  ),
  p: ({ children }) => (
    <p className="text-sm text-slate-300 mb-3 leading-relaxed">{children}</p>
  ),
  strong: ({ children }) => <strong className="font-bold text-slate-100">{children}</strong>,
  em: ({ children }) => <em className="italic text-slate-200">{children}</em>,
  del: ({ children }) => <del className="line-through text-slate-400">{children}</del>,
  a: ({ href, children }) => (
    <a href={href} className="text-sky-400 underline hover:text-sky-300" target="_blank" rel="noreferrer">
      {children}
    </a>
  ),
  blockquote: ({ children }) => (
    <blockquote className="pl-4 border-l-2 border-slate-600 text-slate-400 italic my-3">
      {children}
    </blockquote>
  ),
  ul: ({ children }) => (
    <ul className="list-disc list-inside text-sm text-slate-300 mb-3 space-y-1 pl-2">{children}</ul>
  ),
  ol: ({ children }) => (
    <ol className="list-decimal list-inside text-sm text-slate-300 mb-3 space-y-1 pl-2">{children}</ol>
  ),
  li: ({ children }) => <li className="text-slate-300">{children}</li>,
  code: ({ className, children, ...rest }) => {
    if (className) {
      return (
        <pre className="bg-slate-900 border border-slate-700 rounded-md p-3 overflow-x-auto my-3">
          <code className="text-xs text-green-300 font-mono">{children}</code>
        </pre>
      );
    }
    return (
      <code className="bg-slate-800 text-green-300 text-xs font-mono px-1 py-0.5 rounded" {...rest}>
        {children}
      </code>
    );
  },
  pre: ({ children }) => <>{children}</>,
  hr: () => <hr className="border-slate-700 my-4" />,
  table: ({ children }) => (
    <div className="overflow-x-auto my-3">
      <table className="w-full border-collapse text-xs">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-slate-800">{children}</thead>,
  tbody: ({ children }) => <tbody>{children}</tbody>,
  tr: ({ children }) => (
    <tr className="border-b border-slate-700 even:bg-slate-900/30">{children}</tr>
  ),
  th: ({ children }) => (
    <th className="border border-slate-700 px-3 py-1.5 text-left text-slate-200 font-semibold">
      {children}
    </th>
  ),
  td: ({ children }) => (
    <td className="border border-slate-700 px-3 py-1.5 text-slate-300">{children}</td>
  ),
};

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
  if (s === "??" || s === "? ") return "U";
  if (s.startsWith("R")) return "R";
  return s.trim()[0] ?? "~";
}

// VSCode-like: badge letter shown in the file tree
function scmBadge(xy: string): string {
  if (xy === "??" || xy.startsWith("?")) return "U";
  const x = xy[0], y = xy[1];
  if (x === "A") return "A";
  if (x === "D" || y === "D") return "D";
  if (x === "R") return "R";
  if (x === "C") return "C";
  return "M";
}

function scmColor(xy: string): string {
  if (xy === "??" || xy.startsWith("?")) return "text-green-400"; // untracked
  if (xy.includes("A")) return "text-green-400";                   // added
  if (xy.includes("D")) return "text-red-400";                     // deleted
  if (xy.startsWith("R")) return "text-orange-400";                // renamed
  return "text-yellow-400";                                        // modified
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

// ─── ResizeDivider (horizontal — sidebar) ──────────────────────────────────

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

// ─── HResizeDivider (vertical — terminal) ──────────────────────────────────

function HResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastY = useRef(0);

  const onMove = useCallback((e: MouseEvent) => {
    if (!dragging.current) return;
    onDrag(e.clientY - lastY.current);
    lastY.current = e.clientY;
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
      className="h-2 flex-shrink-0 bg-border/60 hover:bg-primary/70 active:bg-primary cursor-row-resize transition-colors flex items-center justify-center group"
      onMouseDown={(e) => {
        dragging.current = true;
        lastY.current = e.clientY;
        document.body.style.cursor = "row-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    >
      <div className="flex gap-0.5 opacity-50 group-hover:opacity-100 transition-opacity">
        <span className="w-4 h-0.5 rounded-full bg-current" />
        <span className="w-4 h-0.5 rounded-full bg-current" />
        <span className="w-4 h-0.5 rounded-full bg-current" />
      </div>
    </div>
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
  gitFileStatus?: Map<string, string>;
  level: number;
  onDelete: (node: CodeEditorFileNode) => void;
  onRename: (node: CodeEditorFileNode) => void;
  onHistory?: (node: CodeEditorFileNode) => void;
  onUpload?: (dirPath: string, files: FileList) => void;
  onDirFocus?: (path: string) => void;
  onCreate?: (parentPath: string, mode: "file" | "dir") => void;
  onMove?: (from: string, toDir: string) => void;
  onClipboardOp?: (path: string, op: "cut" | "copy") => void;
  onPaste?: (toDir: string) => void;
  cutPath?: string;
  onContextMenu?: (e: React.MouseEvent, node: CodeEditorFileNode) => void;
  revealPath?: string | null;
}

function treeNavKeyDown(e: React.KeyboardEvent<HTMLElement>, onEnter: () => void) {
  if (e.key === "ArrowDown") {
    e.preventDefault();
    const items = Array.from(document.querySelectorAll<HTMLElement>("[data-tree-item]"));
    const idx = items.indexOf(e.currentTarget);
    items[idx + 1]?.focus();
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    const items = Array.from(document.querySelectorAll<HTMLElement>("[data-tree-item]"));
    const idx = items.indexOf(e.currentTarget);
    items[idx - 1]?.focus();
  } else if (e.key === "Enter") {
    e.preventDefault();
    onEnter();
  }
}

function FileTreeNode({ node, selectedPath, onSelect, modifiedPaths, gitFileStatus, level, onDelete, onRename, onHistory, onUpload, onDirFocus, onCreate, onMove, onClipboardOp, onPaste, cutPath, onContextMenu, revealPath }: FileTreeNodeProps) {
  const [open, setOpen] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const isSelected = selectedPath === node.path;
  const isModified = modifiedPaths.has(node.path);
  const fileXY = gitFileStatus?.get(node.path);

  if (node.type === "dir") {
    return (
      <div>
        <div
          data-tree-item="true"
          tabIndex={0}
          className={`w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded hover:bg-muted/50 text-left group cursor-pointer transition-colors focus:outline-none focus:ring-1 focus:ring-primary/40 ${dragOver ? "ring-2 ring-primary bg-primary/10" : ""}`}
          style={{ paddingLeft: `${level * 12 + 4}px` }}
          onClick={() => { setOpen(o => !o); onDirFocus?.(node.path); }}
          onContextMenu={e => { e.preventDefault(); e.stopPropagation(); onContextMenu?.(e, node); }}
          onKeyDown={e => {
            if ((e.ctrlKey || e.metaKey) && e.key === "v") { e.preventDefault(); onPaste?.(node.path); }
            else if (e.key === "ArrowRight") { e.preventDefault(); if (!open) { setOpen(true); onDirFocus?.(node.path); } }
            else if (e.key === "ArrowLeft") { e.preventDefault(); if (open) setOpen(false); }
            else treeNavKeyDown(e, () => { setOpen(o => !o); onDirFocus?.(node.path); });
          }}
          onDragOver={e => { e.preventDefault(); setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={e => {
            e.preventDefault();
            setDragOver(false);
            const moveFrom = e.dataTransfer.getData("application/x-tree-node");
            if (moveFrom) {
              onMove?.(moveFrom, node.path);
            } else if (e.dataTransfer.files.length > 0 && onUpload) {
              onUpload(node.path, e.dataTransfer.files);
            }
          }}
        >
          {open ? <ChevronDown className="w-3 h-3 flex-shrink-0 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 flex-shrink-0 text-muted-foreground" />}
          {open ? <FolderOpen className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" /> : <Folder className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" />}
          <span className="truncate text-foreground/80 flex-1">{node.name}</span>
          {/* dir change indicator */}
          {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400/60 flex-shrink-0" />}
          {onCreate && (
            <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
              <button onClick={e => { e.stopPropagation(); onDirFocus?.(node.path); onCreate(node.path, "file"); }}
                className="p-0.5 rounded hover:bg-muted" title="Novo arquivo aqui">
                <FilePlus className="w-2.5 h-2.5 text-muted-foreground hover:text-foreground" />
              </button>
              <button onClick={e => { e.stopPropagation(); onDirFocus?.(node.path); onCreate(node.path, "dir"); }}
                className="p-0.5 rounded hover:bg-muted" title="Nova pasta aqui">
                <FolderPlus className="w-2.5 h-2.5 text-muted-foreground hover:text-foreground" />
              </button>
            </div>
          )}
        </div>
        {open && node.children?.map(child => (
          <FileTreeNode key={child.path} node={child} selectedPath={selectedPath} onSelect={onSelect}
            modifiedPaths={modifiedPaths} gitFileStatus={gitFileStatus} level={level + 1} onDelete={onDelete} onRename={onRename}
            onHistory={onHistory} onUpload={onUpload} onDirFocus={onDirFocus} onCreate={onCreate}
            onMove={onMove} onClipboardOp={onClipboardOp} onPaste={onPaste} cutPath={cutPath}
            onContextMenu={onContextMenu} revealPath={revealPath} />
        ))}
      </div>
    );
  }

  const isCut = cutPath === node.path;
  const isRevealed = revealPath === node.path;
  return (
    <div
      data-tree-item="true"
      {...(isRevealed ? { "data-reveal-path": "true" } : {})}
      tabIndex={0}
      draggable
      onDragStart={e => {
        e.dataTransfer.setData("application/x-tree-node", node.path);
        e.dataTransfer.effectAllowed = "move";
      }}
      className={`w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded text-left hover:bg-muted/50 group focus:outline-none focus:ring-1 focus:ring-primary/40 cursor-grab active:cursor-grabbing ${isSelected ? "bg-accent text-accent-foreground" : ""} ${isCut ? "opacity-40" : ""} ${isRevealed ? "ring-2 ring-yellow-400/70 bg-yellow-400/10" : ""}`}
      style={{ paddingLeft: `${level * 12 + 4}px` }}
      onContextMenu={e => { e.preventDefault(); e.stopPropagation(); onContextMenu?.(e, node); }}
      onKeyDown={e => {
        if ((e.ctrlKey || e.metaKey) && e.key === "c") { e.preventDefault(); onClipboardOp?.(node.path, "copy"); }
        else if ((e.ctrlKey || e.metaKey) && e.key === "x") { e.preventDefault(); onClipboardOp?.(node.path, "cut"); }
        else treeNavKeyDown(e, () => onSelect(node));
      }}
    >
      <button className="flex items-center gap-1 flex-1 min-w-0" onClick={() => onSelect(node)}>
        <span className="w-3 h-3 flex-shrink-0" />
        <File className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
        <span className={`truncate flex-1 ${fileXY ? scmColor(fileXY) : "text-foreground/80"}`}>{node.name}</span>
        {fileXY && (
          <span className={`text-[10px] font-bold leading-none flex-shrink-0 ${scmColor(fileXY)}`}>{scmBadge(fileXY)}</span>
        )}
      </button>
      <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
        {onHistory && (
          <button onClick={e => { e.stopPropagation(); onHistory(node); }} className="p-0.5 rounded hover:bg-muted" title="Histórico do arquivo">
            <History className="w-2.5 h-2.5 text-muted-foreground hover:text-foreground" />
          </button>
        )}
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
  const [profiles, setProfiles] = useState<GitHubProfile[]>([]);
  const [selectedProfileId, setSelectedProfileId] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [cloning, setCloning] = useState(false);
  const [error, setError] = useState("");
  const [existingId, setExistingId] = useState("");
  const [success, setSuccess] = useState(false);
  const logsRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      const fresh = loadProfiles();
      setProfiles(fresh);
      const defaultId = localStorage.getItem("ce_default_profile") ?? "";
      setSelectedProfileId(defaultId);
      setUrl(""); setBranch(""); setLogs([]); setError(""); setExistingId(""); setSuccess(false);
    }
  }, [open]);

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
    setError(""); setExistingId(""); setSuccess(false); setLogs([]); setCloning(true);
    const authToken = localStorage.getItem("auth_token") || "";
    const profileToken = profiles.find(p => p.id === selectedProfileId)?.token;
    let res: Response;
    try {
      res = await fetch(`${API_BASE}/code-editor/clone`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}) },
        body: JSON.stringify({ ...parsed, branch, ...(profileToken ? { token: profileToken } : {}) }),
      });
    } catch (e) {
      setError("Falha de conexão com o servidor.");
      setCloning(false);
      return;
    }

    // Trata respostas de erro pré-clone (não são SSE)
    if (!res.ok) {
      setCloning(false);
      try {
        const data = await res.json();
        if (res.status === 409) {
          setExistingId(data.id ?? "");
          setError("Repositório já clonado localmente.");
        } else if (res.status === 400) {
          setError(data.error || "Requisição inválida.");
        } else {
          setError(data.error || `Erro ao clonar (HTTP ${res.status}).`);
        }
      } catch {
        setError(`Erro ao clonar (HTTP ${res.status}).`);
      }
      return;
    }

    if (!res.body) { setError("Sem resposta do servidor."); setCloning(false); return; }
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
          if (d.done) {
            if (d.error) setError(d.error);
            else { doneId = d.id; setSuccess(true); setLogs(l => [...l, "✅ Clone concluído com sucesso!"]); }
          }
        } catch (_) {}
      }
    }
    setCloning(false);
    if (doneId) { setTimeout(() => { onDone(doneId); onClose(); }, 1200); }
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
          {profiles.length > 0 && (
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Perfil GitHub (autenticação)</label>
              <select
                className="w-full text-xs bg-muted border border-border/50 rounded px-2 py-1.5 text-foreground"
                value={selectedProfileId}
                onChange={e => setSelectedProfileId(e.target.value)}
                disabled={cloning}
              >
                <option value="">— usar credential helper do sistema —</option>
                {profiles.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>
          )}
          {logs.length > 0 && (
            <div ref={logsRef} className="bg-black/50 rounded p-2 h-32 overflow-y-auto font-mono text-xs space-y-0.5">
              {logs.map((l, i) => (
                <div key={i} className={l.startsWith("✅") ? "text-emerald-400" : "text-green-300/80"}>{l}</div>
              ))}
            </div>
          )}
          {success && !cloning && (
            <div className="flex items-center gap-2 text-emerald-400 text-xs">
              <CheckCircle2 className="w-3 h-3 flex-shrink-0" />
              Repositório clonado com sucesso! Abrindo...
            </div>
          )}
          {error && (
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-red-400 text-xs">
                <AlertCircle className="w-3 h-3 flex-shrink-0" />{error}
              </div>
              {existingId && (
                <Button size="sm" variant="outline" className="text-xs h-7 w-full"
                  onClick={() => { onDone(existingId); onClose(); }}>
                  Abrir repositório existente
                </Button>
              )}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={cloning}>Cancelar</Button>
          <Button onClick={doClone} disabled={cloning || !url || success}>
            {cloning ? <><Loader2 className="w-3 h-3 animate-spin mr-1" />Clonando...</> : "Clonar"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── CreatePRModal ──────────────────────────────────────────────────────────

interface CreatePRModalProps {
  open: boolean;
  onClose: () => void;
  repoId: string;
  head: string;        // branch atual (source)
  branches: string[];  // branches disponíveis para base
}

function CreatePRModal({ open, onClose, repoId, head, branches }: CreatePRModalProps) {
  const defaultBase = branches.includes("main") ? "main" : branches.includes("master") ? "master" : branches[0] ?? "main";
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [base, setBase] = useState(defaultBase);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{ number: number; url: string } | null>(null);

  useEffect(() => {
    if (open) {
      setTitle(head.replace(/[-_/]/g, " ").replace(/\b\w/g, c => c.toUpperCase()));
      setBody("");
      setBase(branches.includes("main") ? "main" : branches.includes("master") ? "master" : branches[0] ?? "main");
      setError("");
      setResult(null);
    }
  }, [open, head, branches]);

  async function submit() {
    if (!title.trim()) { setError("Título obrigatório"); return; }
    setLoading(true); setError("");
    try {
      const pr = await apiClient.codeEditorCreatePR(repoId, title, body, head, base);
      setResult({ number: pr.number, url: pr.url });
    } catch (e: any) {
      setError(e?.message || "Erro ao criar Pull Request");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <GitPullRequest className="w-4 h-4" />Criar Pull Request
          </DialogTitle>
        </DialogHeader>

        {result ? (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-emerald-400 text-sm">
              <CheckCircle2 className="w-4 h-4 flex-shrink-0" />
              PR #{result.number} criado com sucesso!
            </div>
            <Button className="w-full" onClick={() => window.open(result.url, "_blank")}>
              <ExternalLink className="w-3 h-3 mr-2" />Abrir no GitHub
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Branch origem → destino</label>
              <div className="flex items-center gap-2 text-xs bg-muted rounded px-2 py-1.5 font-mono">
                <span className="text-blue-400">{head}</span>
                <span className="text-muted-foreground">→</span>
                <select
                  className="bg-transparent border-none outline-none text-foreground flex-1"
                  value={base}
                  onChange={e => setBase(e.target.value)}
                  disabled={loading}
                >
                  {branches.filter(b => b !== head).map(b => (
                    <option key={b} value={b}>{b}</option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Título *</label>
              <Input
                placeholder="Descreva a mudança em uma linha"
                value={title}
                onChange={e => setTitle(e.target.value)}
                disabled={loading}
                onKeyDown={e => e.key === "Enter" && submit()}
              />
            </div>
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Descrição (opcional)</label>
              <textarea
                className="w-full text-xs bg-muted border border-border/50 rounded px-2 py-1.5 text-foreground resize-none h-24 outline-none focus:ring-1 focus:ring-ring"
                placeholder="O que essa mudança faz? Por quê? Como testar?"
                value={body}
                onChange={e => setBody(e.target.value)}
                disabled={loading}
              />
            </div>
            {error && (
              <div className="flex items-center gap-2 text-red-400 text-xs">
                <AlertCircle className="w-3 h-3 flex-shrink-0" />{error}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            {result ? "Fechar" : "Cancelar"}
          </Button>
          {!result && (
            <Button onClick={submit} disabled={loading || !title.trim()}>
              {loading ? <><Loader2 className="w-3 h-3 animate-spin mr-1" />Criando...</> : "Criar PR"}
            </Button>
          )}
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
  onPush?: () => void;
  onRefresh?: () => void;
}

function CommitDialog({ open, repoId, status, onClose, onDone, onPush, onRefresh }: CommitDialogProps) {
  const [message, setMessage] = useState("");
  const [amend, setAmend] = useState(false);
  const [loading, setLoading] = useState(false);
  const [unstaging, setUnstaging] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [gitOutput, setGitOutput] = useState("");

  useEffect(() => {
    if (open) { setMessage(""); setError(""); setGitOutput(""); setAmend(false); }
  }, [open]);

  async function doCommit(andPush = false) {
    if (!amend && !message.trim()) { setError("Mensagem é obrigatória"); return; }
    setLoading(true); setError(""); setGitOutput("");
    try {
      const result = await apiClient.codeEditorCommit(repoId, message.trim(), undefined, amend);
      setGitOutput(result.message || "Commit realizado.");
      setTimeout(() => {
        onDone();
        if (andPush && onPush) onPush();
      }, 800);
    } catch (e: any) {
      setError(e.message || "Erro ao commitar");
    } finally {
      setLoading(false);
    }
  }

  async function doUnstage(path: string) {
    setUnstaging(path);
    try {
      await apiClient.codeEditorUnstage(repoId, [path]);
      onRefresh?.();
    } catch (e: any) {
      setError(e.message || "Erro ao remover arquivo");
    } finally {
      setUnstaging(null);
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

        {/* File list — único elemento com scroll, altura fixa */}
        {changedFiles.length > 0 && !gitOutput && (
          <div>
            <p className="text-xs text-muted-foreground mb-1">
              Arquivos em staging ({changedFiles.length}) — passe o mouse para remover:
            </p>
            <ScrollArea className="h-28 border border-border/40 rounded">
              <div className="p-2 space-y-0.5">
                {changedFiles.map(f => (
                  <div key={f.path} className="flex items-center gap-2 text-xs py-0.5 group">
                    <span className={`font-bold w-4 text-center flex-shrink-0 ${scmColor(f.status)}`}>{scmBadge(f.status)}</span>
                    <span className="font-mono text-foreground/80 truncate flex-1">{f.path}</span>
                    <button
                      onClick={() => doUnstage(f.path)}
                      disabled={!!unstaging || loading}
                      title="Remover do staging (git restore --staged)"
                      className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-red-400 transition-opacity flex-shrink-0 w-4 h-4 flex items-center justify-center rounded"
                    >
                      {unstaging === f.path ? <Loader2 className="w-3 h-3 animate-spin" /> : <X className="w-3 h-3" />}
                    </button>
                  </div>
                ))}
              </div>
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

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            {gitOutput ? "Fechar" : "Cancelar"}
          </Button>
          {!gitOutput && (
            <>
              <Button variant="outline" onClick={() => doCommit(false)} disabled={loading || (!amend && !message.trim())}>
                {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitCommit className="w-3 h-3 mr-1" />}
                {amend ? "Emendar" : "Commitar"}
              </Button>
              {!amend && onPush && (
                <Button onClick={() => doCommit(true)} disabled={loading || !message.trim()}>
                  {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <Upload className="w-3 h-3 mr-1" />}
                  Commitar e Fazer Push
                </Button>
              )}
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── Token store (localStorage) ──────────────────────────────────────────────

interface GitHubProfile { id: string; name: string; token: string; active?: boolean }

const PROFILES_KEY = "ce_github_profiles";
const REPO_PROFILE_KEY = "ce_repo_profile";

function loadProfiles(): GitHubProfile[] {
  try { return JSON.parse(localStorage.getItem(PROFILES_KEY) || "[]"); } catch { return []; }
}
function saveProfiles(p: GitHubProfile[]) { localStorage.setItem(PROFILES_KEY, JSON.stringify(p)); }
function loadRepoProfile(): Record<string, string> {
  try { return JSON.parse(localStorage.getItem(REPO_PROFILE_KEY) || "{}"); } catch { return {}; }
}
function saveRepoProfile(m: Record<string, string>) { localStorage.setItem(REPO_PROFILE_KEY, JSON.stringify(m)); }

// ─── ProfileSwitcher ─────────────────────────────────────────────────────────
// Gerencia contas GitHub diretamente no dropdown — sem precisar abrir outro modal.

interface ProfileSwitcherProps {
  repoId: string;
  repoProfileMap: Record<string, string>;
  onSwitch: (repoId: string, profileId: string) => void;
}

function ProfileSwitcher({ repoId, repoProfileMap, onSwitch }: ProfileSwitcherProps) {
  const [open, setOpen] = useState(false);
  const [profiles, setProfiles] = useState<GitHubProfile[]>([]);
  const [newName, setNewName] = useState("");
  const [newToken, setNewToken] = useState("");
  const [showNewToken, setShowNewToken] = useState(false);
  const btnRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const nameInputRef = useRef<HTMLInputElement>(null);

  const activeId = repoId
    ? (repoProfileMap[repoId] ?? "")
    : (localStorage.getItem("ce_default_profile") ?? "");

  const activeProfile = profiles.find(p => p.id === activeId);

  function openMenu() {
    setProfiles(loadProfiles());
    setOpen(true);
    setTimeout(() => nameInputRef.current?.focus(), 50);
  }

  function handleSwitch(profileId: string) {
    onSwitch(repoId, profileId);
    setOpen(false);
  }

  function handleAdd() {
    const name = newName.trim();
    const token = newToken.trim();
    if (!name || !token) return;
    const updated = [...loadProfiles(), { id: crypto.randomUUID(), name, token }];
    saveProfiles(updated);
    setProfiles(updated);
    // Seleciona automaticamente o perfil recém-adicionado
    const newId = updated[updated.length - 1].id;
    onSwitch(repoId, newId);
    setNewName("");
    setNewToken("");
    setShowNewToken(false);
  }

  function handleDelete(id: string) {
    const updated = loadProfiles().filter(p => p.id !== id);
    saveProfiles(updated);
    // Remove associações ao perfil deletado
    const map = loadRepoProfile();
    Object.keys(map).forEach(k => { if (map[k] === id) delete map[k]; });
    saveRepoProfile(map);
    if (localStorage.getItem("ce_default_profile") === id) localStorage.removeItem("ce_default_profile");
    setProfiles(updated);
    if (activeId === id) onSwitch(repoId, "");
  }

  // Fecha ao clicar fora
  useEffect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node) &&
          btnRef.current && !btnRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [open]);

  return (
    <div className="relative">
      <button
        ref={btnRef}
        onClick={openMenu}
        className="flex items-center gap-1.5 text-xs bg-muted/60 border border-border/50 rounded px-2 py-1 hover:bg-muted text-foreground/80 transition-colors"
        title="Gerenciar contas GitHub"
      >
        <Key className="w-3 h-3 text-primary flex-shrink-0" />
        <span className="max-w-28 truncate">
          {activeProfile ? activeProfile.name : <span className="opacity-60">Conta GitHub</span>}
        </span>
        <ChevronsUpDown className="w-3 h-3 opacity-50 flex-shrink-0" />
      </button>

      {open && (
        <div
          ref={menuRef}
          className="absolute top-full left-0 mt-1 z-50 w-72 bg-popover border border-border rounded shadow-xl"
        >
          {/* Cabeçalho */}
          <div className="px-3 py-2 border-b border-border/50">
            <p className="text-[11px] font-semibold text-foreground">Contas GitHub</p>
            <p className="text-[10px] text-muted-foreground">
              {repoId ? "Conta usada para este repositório" : "Conta padrão (clone)"}
            </p>
          </div>

          {/* Lista de perfis */}
          <div className="py-1">
            {/* Sem perfil */}
            <button
              className={`w-full text-left text-xs px-3 py-2 hover:bg-muted transition-colors flex items-center gap-2 ${activeId === "" ? "bg-muted/50" : ""}`}
              onClick={() => handleSwitch("")}
            >
              <div className={`w-3 h-3 flex-shrink-0 ${activeId === "" ? "text-primary" : "opacity-0"}`}>
                <CheckCircle2 className="w-3 h-3" />
              </div>
              <span className={`flex-1 ${activeId === "" ? "font-medium text-foreground" : "text-muted-foreground"}`}>
                Sem conta (credential helper)
              </span>
            </button>

            {/* Perfis configurados */}
            {profiles.map(p => (
              <div
                key={p.id}
                className={`flex items-center gap-1 px-3 py-1.5 hover:bg-muted group transition-colors ${activeId === p.id ? "bg-muted/50" : ""}`}
              >
                <button
                  className="flex items-center gap-2 flex-1 text-left min-w-0"
                  onClick={() => handleSwitch(p.id)}
                >
                  <div className={`w-3 h-3 flex-shrink-0 ${activeId === p.id ? "text-primary" : "opacity-0"}`}>
                    <CheckCircle2 className="w-3 h-3" />
                  </div>
                  <div className="min-w-0">
                    <p className={`text-xs truncate ${activeId === p.id ? "font-semibold text-foreground" : "text-foreground/80"}`}>{p.name}</p>
                    <p className="text-[10px] text-muted-foreground font-mono">
                      {p.token.length > 8 ? p.token.slice(0, 8) + "••••" + p.token.slice(-4) : "••••••••"}
                    </p>
                  </div>
                </button>
                <button
                  onClick={() => handleDelete(p.id)}
                  className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive transition-all p-0.5 rounded flex-shrink-0"
                  title="Remover conta"
                >
                  <Trash2 className="w-3 h-3" />
                </button>
              </div>
            ))}

            {profiles.length === 0 && (
              <p className="px-3 py-1.5 text-[11px] text-muted-foreground italic">
                Nenhuma conta configurada ainda.
              </p>
            )}
          </div>

          {/* Adicionar nova conta */}
          <div className="border-t border-border/50 px-3 py-2.5 space-y-2">
            <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wide">Adicionar conta</p>
            <input
              ref={nameInputRef}
              className="w-full text-xs bg-background border border-border/60 rounded px-2 py-1.5 text-foreground placeholder:text-muted-foreground focus:outline-none focus:border-primary/60"
              placeholder="Nome (ex: Pessoal, Empresa)"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === "Enter" && document.getElementById("ps-token-input")?.focus()}
            />
            <div className="flex gap-1.5">
              <div className="relative flex-1">
                <input
                  id="ps-token-input"
                  type={showNewToken ? "text" : "password"}
                  className="w-full text-xs bg-background border border-border/60 rounded px-2 py-1.5 pr-7 font-mono text-foreground placeholder:text-muted-foreground focus:outline-none focus:border-primary/60"
                  placeholder="ghp_... ou github_pat_..."
                  value={newToken}
                  onChange={e => setNewToken(e.target.value)}
                  onKeyDown={e => e.key === "Enter" && handleAdd()}
                />
                <button
                  type="button"
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setShowNewToken(v => !v)}
                  tabIndex={-1}
                >
                  {showNewToken
                    ? <Eye className="w-3 h-3" />
                    : <Eye className="w-3 h-3 opacity-50" />}
                </button>
              </div>
              <button
                className="text-xs bg-primary text-primary-foreground rounded px-2.5 py-1 hover:bg-primary/90 disabled:opacity-40 disabled:cursor-not-allowed transition-colors font-medium flex-shrink-0"
                disabled={!newName.trim() || !newToken.trim()}
                onClick={handleAdd}
              >
                Adicionar
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── GitHubTokenDialog ───────────────────────────────────────────────────────

function GitHubTokenDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [profiles, setProfiles] = useState<GitHubProfile[]>([]);
  const [activeId, setActiveId] = useState("");
  const [newName, setNewName] = useState("");
  const [newToken, setNewToken] = useState("");
  const [showNewToken, setShowNewToken] = useState(false);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setNewName(""); setNewToken(""); setError(""); setShowNewToken(false);
    // Carregar do servidor para garantir sincronização entre abas/sessões
    apiClient.codeEditorGetGitHubProfiles().then(r => {
      const srv = r.profiles;
      if (srv.length > 0) {
        const local: GitHubProfile[] = srv.map(p => ({ id: p.id, name: p.name, token: p.token, active: p.active }));
        setProfiles(local);
        saveProfiles(local);
        const aid = srv.find(p => p.active)?.id ?? localStorage.getItem("ce_default_profile") ?? "";
        setActiveId(aid);
        if (aid) localStorage.setItem("ce_default_profile", aid);
      } else {
        const local = loadProfiles();
        setProfiles(local);
        setActiveId(localStorage.getItem("ce_default_profile") ?? "");
      }
    }).catch(() => {
      const local = loadProfiles();
      setProfiles(local);
      setActiveId(localStorage.getItem("ce_default_profile") ?? "");
    });
  }, [open]);

  async function persistProfiles(updated: GitHubProfile[], newActiveId: string) {
    const toSave: GitHubEditorProfile[] = updated.map(p => ({
      id: p.id, name: p.name, token: p.token, active: p.id === newActiveId,
    }));
    saveProfiles(updated);
    if (newActiveId) localStorage.setItem("ce_default_profile", newActiveId);
    else localStorage.removeItem("ce_default_profile");
    try { await apiClient.codeEditorSaveGitHubProfiles(toSave); } catch (_) { /* silencioso */ }
  }

  async function handleAdd() {
    if (!newName.trim()) { setError("Nome é obrigatório"); return; }
    if (!newToken.trim()) { setError("Token é obrigatório"); return; }
    if (profiles.some(p => p.name === newName.trim())) { setError("Já existe um perfil com esse nome"); return; }
    setSaving(true);
    const newProfile: GitHubProfile = { id: crypto.randomUUID(), name: newName.trim(), token: newToken.trim() };
    const updated = [...profiles, newProfile];
    // Auto-seleciona se for o primeiro perfil ou se não há padrão definido
    const newActiveId = activeId || newProfile.id;
    await persistProfiles(updated, newActiveId);
    setProfiles(updated);
    setActiveId(newActiveId);
    setNewName(""); setNewToken(""); setError(""); setShowNewToken(false);
    setSaving(false);
  }

  async function handleSetDefault(id: string) {
    setActiveId(id);
    await persistProfiles(profiles, id);
  }

  async function handleDelete(id: string) {
    const updated = profiles.filter(p => p.id !== id);
    const newActiveId = id === activeId ? (updated[0]?.id ?? "") : activeId;
    const map = loadRepoProfile();
    Object.keys(map).forEach(k => { if (map[k] === id) delete map[k]; });
    saveRepoProfile(map);
    await persistProfiles(updated, newActiveId);
    setProfiles(updated);
    setActiveId(newActiveId);
  }

  function maskedToken(t: string) {
    if (t.length <= 8) return "••••••••";
    return t.slice(0, 4) + "••••" + t.slice(-4);
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Key className="w-4 h-4" />Perfis GitHub
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          {/* Lista de perfis existentes */}
          {profiles.length > 0 ? (
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">
                Clique em <CheckCircle2 className="inline w-3 h-3 mx-0.5" /> para definir o perfil padrão (usado em pull/push)
              </p>
              {profiles.map(p => (
                <div key={p.id} className={`flex items-center gap-2 rounded border px-3 py-2 transition-colors ${
                  p.id === activeId ? "border-primary/50 bg-primary/5" : "border-border/50 bg-muted/30"
                }`}>
                  <button
                    onClick={() => p.id !== activeId && handleSetDefault(p.id)}
                    title={p.id === activeId ? "Perfil padrão" : "Definir como padrão"}
                    className={`flex-shrink-0 transition-colors ${p.id === activeId ? "text-primary cursor-default" : "text-muted-foreground hover:text-primary cursor-pointer"}`}
                  >
                    <CheckCircle2 className="w-4 h-4" />
                  </button>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium">{p.name}</span>
                    {p.id === activeId && <span className="ml-2 text-[10px] text-primary font-semibold uppercase tracking-wide">padrão</span>}
                    <span className="ml-2 text-xs text-muted-foreground font-mono">{maskedToken(p.token)}</span>
                  </div>
                  <button onClick={() => handleDelete(p.id)} className="flex-shrink-0 text-muted-foreground hover:text-red-400 transition-colors">
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground text-center py-2">Nenhum perfil configurado</p>
          )}

          {/* Adicionar novo */}
          <div className="border border-border/50 rounded p-3 space-y-2">
            <p className="text-xs text-muted-foreground font-medium">Adicionar perfil</p>
            <Input placeholder="Nome (ex: Pessoal, Trabalho)" value={newName} onChange={e => setNewName(e.target.value)} className="h-7 text-xs"
              onKeyDown={e => e.key === "Enter" && document.getElementById("ghd-token-input")?.focus()} />
            <div className="relative">
              <Input id="ghd-token-input" type={showNewToken ? "text" : "password"} placeholder="ghp_... ou github_pat_..."
                value={newToken} onChange={e => setNewToken(e.target.value)} className="h-7 text-xs pr-8"
                onKeyDown={e => e.key === "Enter" && handleAdd()} />
              <button type="button" onClick={() => setShowNewToken(v => !v)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                <Eye className="w-3.5 h-3.5" />
              </button>
            </div>
            <p className="text-[10px] text-muted-foreground">GitHub → Settings → Developer settings → Personal access tokens → Scope: <code>repo</code></p>
            {error && <p className="text-xs text-red-400">{error}</p>}
            <Button size="sm" onClick={handleAdd} disabled={!newName.trim() || !newToken.trim() || saving} className="w-full h-7 text-xs">
              {saving ? <Loader2 className="w-3 h-3 mr-1 animate-spin" /> : <Plus className="w-3 h-3 mr-1" />}
              Adicionar Perfil
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Fechar</Button>
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
  onConflict: (files: string[]) => void;
}

function MergeDialog({ open, repoId, currentBranch, branches, onClose, onDone, onConflict }: MergeDialogProps) {
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
      const result = await apiClient.codeEditorMerge(repoId, target, noFf) as { message: string; has_conflicts?: boolean };
      if (result.has_conflicts) {
        // Busca lista de arquivos conflitantes e abre o resolver
        const c = await apiClient.codeEditorGetConflicts(repoId);
        onClose();
        onConflict(c.files);
        return;
      }
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
  basePath?: string;
  onClose: () => void;
  onDone: (path: string) => void;
}

function CreateFileDialog({ open, mode, repoId, basePath, onClose, onDone }: CreateFileDialogProps) {
  const [path, setPath] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setPath(basePath ? basePath + "/" : "");
      setError("");
    }
  }, [open, basePath]);

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
  onBranchDiff: () => void;
}

function BranchesPanel({ branches, onRefresh, onCheckout, onCreateBranch, onMerge, onBranchDiff }: BranchesPanelProps) {
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
          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={onBranchDiff} title="Comparar dois branches">
            <GitCompare className="w-3 h-3" />
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
  const [treeLoading, setTreeLoading] = useState(false);

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
  const [sidePanel, setSidePanel] = useState<"files" | "branches" | "git" | "log" | "replace" | "k8s" | "source-control">("files");
  const [sidebarWidth, setSidebarWidth] = useState(() => {
    const saved = localStorage.getItem("ce_sidebar_width");
    return saved ? parseInt(saved) : 224;
  });

  // Editor options
  const [showMinimap, setShowMinimap] = useState(false);

  // Stash state (feedback)
  const [stashLoading, setStashLoading] = useState<"push" | "pop" | null>(null);

  // Source Control panel state
  const [scmCommitMsg, setScmCommitMsg] = useState("");
  const [scmLoading, setScmLoading] = useState(false);

  // Clipboard (Ctrl+C/X/V na tree)
  const [clipboard, setClipboard] = useState<{ path: string; op: "cut" | "copy" } | null>(null);

  // Dialogs
  const [showClone, setShowClone] = useState(false);
  const [showGitHubToken, setShowGitHubToken] = useState(false);
  // Retorna o token da conta GitHub ativa.
  // Fallback em cascata: perfil padrão → perfil com active=true → único perfil existente.
  function activeToken(): string | undefined {
    const profiles = loadProfiles();
    if (profiles.length === 0) return undefined;
    const pid = localStorage.getItem("ce_default_profile") ?? "";
    return profiles.find(p => p.id === pid)?.token
      ?? profiles.find(p => p.active)?.token
      ?? (profiles.length === 1 ? profiles[0].token : undefined);
  }

  const [showCommit, setShowCommit] = useState(false);
  const [showBranch, setShowBranch] = useState(false);
  const [showMerge, setShowMerge] = useState(false);
  const [sseDialog, setSseDialog] = useState<{ title: string; endpoint: string; body?: object } | null>(null);
  const [focusedDirPath, setFocusedDirPath] = useState("");
  const [createDialog, setCreateDialog] = useState<{ mode: "file" | "dir"; basePath: string } | null>(null);

  function getCreateBasePath(): string {
    if (focusedDirPath) return focusedDirPath;
    if (activeTab) {
      const parts = activeTab.node.path.split("/");
      parts.pop();
      return parts.join("/");
    }
    return "";
  }
  const [renameNode, setRenameNode] = useState<CodeEditorFileNode | null>(null);
  const [diffFile, setDiffFile] = useState<string | null>(null);

  // Tags e log sub-abas
  const [tags, setTags] = useState<{ name: string; date: string; commit: string }[]>([]);
  const [logTab, setLogTab] = useState<"commits" | "tags">("commits");
  const [showCreateTag, setShowCreateTag] = useState<{ hash: string } | null>(null);

  // Terminal integrado
  const [showTerminal, setShowTerminal] = useState(false);
  const [terminalHeight, setTerminalHeight] = useState(() => {
    const saved = localStorage.getItem("ce_terminal_height");
    return saved ? Math.max(80, Math.min(600, parseInt(saved, 10))) : 240;
  });
  const [terminalFont, setTerminalFont] = useState(
    () => localStorage.getItem("ce_terminal_font") ?? ""
  );
  const [terminalTabs, setTerminalTabs] = useState<{id: number; label: string}[]>([{id: 1, label: "Terminal 1"}]);
  const [activeTerminalId, setActiveTerminalId] = useState(1);
  const terminalCounter = useRef(1);

  function addTerminalTab() {
    const id = ++terminalCounter.current;
    setTerminalTabs(prev => [...prev, { id, label: `Terminal ${id}` }]);
    setActiveTerminalId(id);
  }

  function closeTerminalTab(closedId: number) {
    setTerminalTabs(prev => {
      const next = prev.filter(t => t.id !== closedId);
      if (next.length === 0) {
        setShowTerminal(false);
        const newId = ++terminalCounter.current;
        setActiveTerminalId(newId);
        return [{ id: newId, label: "Terminal 1" }];
      }
      setActiveTerminalId(curr => curr === closedId ? next[next.length - 1].id : curr);
      return next;
    });
  }

  // Fase 4: Blame
  const [showBlame, setShowBlame] = useState(false);
  const [blameLines, setBlameLines] = useState<CodeEditorBlameLine[]>([]);
  const [cursorLine, setCursorLine] = useState(1);
  const [cursorCol, setCursorCol] = useState(1);
  const [autoSave, setAutoSave] = useState(() => localStorage.getItem("ce_autosave") === "true");
  const autoSaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [formatOnSave, setFormatOnSave] = useState(() => localStorage.getItem("ce_format_on_save") === "true");
  const [showQuickOpen, setShowQuickOpen] = useState(false);
  const [quickOpenQuery, setQuickOpenQuery] = useState("");
  const [quickOpenIdx, setQuickOpenIdx] = useState(0);
  const [fontSize, setFontSize] = useState(() => parseInt(localStorage.getItem("ce_font_size") ?? "13", 10));
  const [wordWrap, setWordWrap] = useState(() => localStorage.getItem("ce_word_wrap") !== "off");
  const [revealPath, setRevealPath] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; node: CodeEditorFileNode } | null>(null);
  const blameDecorationsRef = useRef<MonacoEditorNS.editor.IEditorDecorationsCollection | null>(null);
  const blameMap = useMemo(() => {
    const m = new Map<number, CodeEditorBlameLine>();
    blameLines.forEach(b => m.set(b.line, b));
    return m;
  }, [blameLines]);

  // Fase 4: Histórico de arquivo
  const [fileHistoryNode, setFileHistoryNode] = useState<CodeEditorFileNode | null>(null);

  // Fase 5: Conflitos, branch diff, markdown preview
  const [showConflictResolver, setShowConflictResolver] = useState(false);
  const [conflictFiles, setConflictFiles] = useState<string[]>([]);
  const [showBranchDiff, setShowBranchDiff] = useState(false);
  const [showCreatePR, setShowCreatePR] = useState(false);
  const [showMarkdownPreview, setShowMarkdownPreview] = useState(false);
  const [markdownPreviewWidth, setMarkdownPreviewWidth] = useState(() => {
    const saved = localStorage.getItem("ce_md_preview_width");
    return saved ? Math.max(200, parseInt(saved, 10)) : 480;
  });

  // Fase 4: Replace
  const [replaceQuery, setReplaceQuery] = useState("");
  const [replaceWith, setReplaceWith] = useState("");
  const [replaceRegex, setReplaceRegex] = useState(false);
  const [replaceGlob, setReplaceGlob] = useState("");
  const [replaceLoading, setReplaceLoading] = useState(false);
  const [replaceMatches, setReplaceMatches] = useState<CodeEditorReplaceMatch[]>([]);
  const [replaceModified, setReplaceModified] = useState(0);
  const [replaceApplied, setReplaceApplied] = useState(false);
  const [replaceError, setReplaceError] = useState("");

  // K8s integration (Fase 9)
  const [k8sContexts, setK8sContexts] = useState<string[]>([]);
  const [k8sCluster, setK8sCluster] = useState(() => localStorage.getItem("ce_k8s_cluster") ?? "");
  const [k8sOutput, setK8sOutput] = useState<{ text: string; kind: "info" | "ok" | "err" | "warn" }[]>([]);
  const [k8sRunning, setK8sRunning] = useState<"diff" | "dry-run" | "apply" | "get" | null>(null);
  const k8sOutputRef = useRef<HTMLDivElement>(null);

  // Confirm dialog (substitui confirm() nativo)
  const [confirmState, setConfirmState] = useState<{
    message: string;
    onConfirm: () => void;
    onCancel: () => void;
  } | null>(null);

  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const saveFileRef = useRef<() => void>(() => {});
  const openFileRef = useRef<(node: CodeEditorFileNode) => Promise<void>>(async () => {});
  const pendingNavigationRef = useRef<{ line: number; col: number } | null>(null);
  const editorRowRef = useRef<HTMLDivElement>(null);
  const lspVersionRef = useRef<number>(0);
  const lspProviderDisposables = useRef<MonacoEditorNS.IDisposable[]>([]);
  const { toasts, addToast } = useToasts();

  // showConfirm — substitui window.confirm() por dialog React
  function showConfirm(message: string): Promise<boolean> {
    return new Promise(resolve => {
      setConfirmState({
        message,
        onConfirm: () => { setConfirmState(null); resolve(true); },
        onCancel:  () => { setConfirmState(null); resolve(false); },
      });
    });
  }

  const activeTab = openTabs[activeTabIdx] ?? null;
  const isModified = activeTab ? activeTab.currentContent !== activeTab.savedContent : false;
  const modifiedPaths = new Set((status?.files ?? []).map(f => f.path));
  const gitFileStatusMap = new Map((status?.files ?? []).map(f => [f.path, f.status]));

  // ── persist sidebar width ──
  useEffect(() => {
    localStorage.setItem("ce_sidebar_width", String(sidebarWidth));
  }, [sidebarWidth]);

  // ── LSP: aplica navegação pendente após troca de aba (go-to-definition cross-file) ──
  useEffect(() => {
    if (!pendingNavigationRef.current || !activeTab) return;
    const { line, col } = pendingNavigationRef.current;
    pendingNavigationRef.current = null;
    // requestAnimationFrame garante que Monaco já atualizou o modelo com o novo conteúdo
    requestAnimationFrame(() => {
      editorRef.current?.revealLineInCenter(line);
      editorRef.current?.setPosition({ lineNumber: line, column: col });
      editorRef.current?.focus();
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab?.node.path]);

  // ── LSP: atualiza vars globais e faz polling de diagnósticos ──
  useEffect(() => {
    if (!activeTab) return;
    lspActivate(activeTab.repoId, activeTab.node.path);
    lspVersionRef.current += 1;

    const lang = extToLanguage(activeTab.node.name);
    if (lang !== "go" && lang !== "python") return;

    const repoId = activeTab.repoId;
    const filePath = activeTab.node.path;
    let alive = true;
    const poll = async () => {
      if (!alive) return;
      try {
        const result = await apiClient.lspDiagnostics(repoId, lang, filePath);
        if (!alive) return;
        const applyFn = (window as any).__lspApplyDiagnostics;
        if (applyFn && editorRef.current) {
          const model = editorRef.current.getModel();
          const owner = lang === "python" ? "pyright" : "gopls";
          if (model) applyFn(model, result.diagnostics ?? [], owner);
        }
      } catch { /* silencioso */ }
    };
    poll();
    const interval = setInterval(poll, 2500);
    return () => { alive = false; clearInterval(interval); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTabIdx, activeTab?.node.path]);

  // ── persist selected repo ──
  useEffect(() => {
    if (selectedRepo) {
      localStorage.setItem("ce_last_repo", selectedRepo.id);
    }
  }, [selectedRepo?.id]);

  // ── carregamento inicial ──
  useEffect(() => {
    loadRepos();
    // Sincroniza perfis do servidor para o localStorage (cache para activeToken())
    apiClient.codeEditorGetGitHubProfiles().then(r => {
      if (r.profiles.length > 0) {
        localStorage.setItem("ce_github_profiles", JSON.stringify(r.profiles));
        const active = r.profiles.find(p => p.active);
        if (active) localStorage.setItem("ce_default_profile", active.id);
      }
    }).catch(() => { /* silencioso */ });
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
    await Promise.all([loadTree(repo.id), loadStatus(repo.id), loadBranches(repo.id), loadLog(repo.id), loadTags(repo.id)]);
  }

  async function loadTree(id: string) {
    setTreeLoading(true);
    try { setTree(await apiClient.codeEditorGetFileTree(id)); } catch (_) {}
    setTreeLoading(false);
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
    // Atualiza diretório em foco para o pai do arquivo aberto
    const parentDir = node.path.includes("/") ? node.path.split("/").slice(0, -1).join("/") : "";
    setFocusedDirPath(parentDir);
    // Já aberto? Ativar a aba existente
    const existingIdx = openTabs.findIndex(t => t.repoId === repoId && t.node.path === node.path);
    if (existingIdx >= 0) {
      setActiveTabIdx(existingIdx);
      lspActivate(repoId, node.path);
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
      // Inicia LSP para arquivos Go e Python
      const lang = extToLanguage(node.name);
      if (lang === "go" || lang === "python") {
        lspVersionRef.current = 1;
        apiClient.lspOpen(repoId, lang, node.path, content).catch(() => {});
      }
      lspActivate(repoId, node.path);
    } catch (e: any) {
      addToast("error", "Erro ao abrir: " + e.message);
    }
  }

  // Atualiza vars globais usadas pelos providers Monaco
  function lspActivate(repoId: string, filePath: string) {
    (window as any).__lspActiveRepoId = repoId;
    (window as any).__lspActiveFilePath = filePath;
  }

  async function closeTab(idx: number) {
    const tab = openTabs[idx];
    if (tab && tab.currentContent !== tab.savedContent) {
      if (!await showConfirm(`"${tab.node.name}" tem mudanças não salvas. Fechar mesmo assim?`)) return;
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
    if (autoSave) {
      if (autoSaveTimerRef.current) clearTimeout(autoSaveTimerRef.current);
      autoSaveTimerRef.current = setTimeout(() => saveFileRef.current(), 1500);
    }
    // Notifica LSP da mudança (Go/Python)
    if (activeTab) {
      const lang = extToLanguage(activeTab.node.name);
      if (lang === "go" || lang === "python") {
        lspVersionRef.current += 1;
        apiClient.lspChange(activeTab.repoId, lang, activeTab.node.path, value, lspVersionRef.current).catch(() => {});
      }
    }
  }

  async function saveFile() {
    if (!activeTab || !isModified) return;
    if (activeTab.node.path.startsWith("__k8s_virtual__/")) return; // recurso virtual — não salva em disco
    const tabRepoId = activeTab.repoId;
    const tabPath = activeTab.node.path;
    const tabName = activeTab.node.name;
    const contentSnapshot = activeTab.currentContent;
    const tabIdx = activeTabIdx;
    try {
      let contentToSave = contentSnapshot;
      let didFormat = false;
      if (formatOnSave) {
        const lang = extToLanguage(tabName);
        if (["go", "typescript", "javascript", "python", "json"].includes(lang)) {
          try {
            const r = await apiClient.codeEditorFormatFile(tabRepoId, tabPath, contentSnapshot);
            contentToSave = r.content;
            didFormat = true;
          } catch { /* format falhou — salva como está */ }
        }
      }
      await apiClient.codeEditorWriteFile(tabRepoId, tabPath, contentToSave);
      if (didFormat) {
        const model = editorRef.current?.getModel();
        if (model && model.getValue() === contentSnapshot) {
          const pos = editorRef.current?.getPosition();
          model.setValue(contentToSave);
          if (pos) editorRef.current?.setPosition(pos);
        }
      }
      setOpenTabs(prev => prev.map((t, i) =>
        i === tabIdx && t.node.path === tabPath
          ? { ...t, currentContent: contentToSave, savedContent: contentToSave }
          : t
      ));
      await loadStatus(tabRepoId);
      addToast("success", `Salvo${didFormat ? " e formatado" : ""}: ${tabName}`);
    } catch (e: any) {
      addToast("error", "Erro ao salvar: " + e.message);
    }
  }

  async function handleCheckout(branch: string) {
    if (!selectedRepo) return;
    // Confirmar se há mudanças não salvas em alguma aba
    const hasUnsaved = openTabs.some(t => t.currentContent !== t.savedContent);
    if (hasUnsaved && !await showConfirm("Há arquivos com mudanças não salvas. Alternar branch vai descartá-las. Continuar?")) {
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

  // Ctrl+P Quick Open — lista de arquivos filtrada
  const quickOpenFiles = useMemo(() => {
    const allFiles = flattenTree(tree);
    if (!quickOpenQuery.trim()) return allFiles;
    const qk = quickOpenQuery.toLowerCase();
    return allFiles.filter(f =>
      f.name.toLowerCase().includes(qk) || f.path.toLowerCase().includes(qk)
    );
  }, [tree, quickOpenQuery]);

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
    if (!await showConfirm(`Deletar "${node.path}"?`)) return;
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

  async function handleClipboardPaste(toDir: string) {
    if (!clipboard || !selectedRepo) return;
    const filename = clipboard.path.split("/").pop()!;
    const to = toDir ? toDir + "/" + filename : filename;
    if (clipboard.path === to) return;
    const verb = clipboard.op === "cut" ? "Mover" : "Copiar";
    if (!await showConfirm(`${verb}\n"${clipboard.path}"\n→ "${to}"?`)) return;
    try {
      if (clipboard.op === "cut") {
        await apiClient.codeEditorRenameFile(selectedRepo.id, clipboard.path, to);
        setOpenTabs(prev => prev.map(t =>
          t.node.path === clipboard.path
            ? { ...t, node: { ...t.node, path: to, name: filename } }
            : t
        ));
        setClipboard(null);
      } else {
        await apiClient.codeEditorCopyFile(selectedRepo.id, clipboard.path, to);
        // mantém clipboard para permitir colar múltiplas cópias
      }
      await loadTree(selectedRepo.id);
      await loadStatus(selectedRepo.id);
      addToast("success", `${clipboard.op === "cut" ? "Movido" : "Copiado"}: ${filename}`);
    } catch (e: any) {
      addToast("error", e.message || `Erro ao ${clipboard.op === "cut" ? "mover" : "copiar"}`);
    }
  }

  async function handleMoveFile(from: string, toDir: string) {
    if (!selectedRepo) return;
    const filename = from.split("/").pop()!;
    const to = toDir ? toDir + "/" + filename : filename;
    if (from === to) return;
    if (!await showConfirm(`Mover\n"${from}"\n→ "${to}"?`)) return;
    try {
      await apiClient.codeEditorRenameFile(selectedRepo.id, from, to);
      setOpenTabs(prev => prev.map(t =>
        t.node.path === from
          ? { ...t, node: { ...t.node, path: to, name: filename } }
          : t
      ));
      await loadTree(selectedRepo.id);
      await loadStatus(selectedRepo.id);
      addToast("success", `Movido: ${filename}`);
    } catch (e: any) {
      addToast("error", e.message || "Erro ao mover");
    }
  }

  async function handleResetFile(filePath: string) {
    if (!selectedRepo) return;
    if (!await showConfirm(`Descartar mudanças em "${filePath}"?`)) return;
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

  // ── Source Control helpers ──────────────────────────────────────────────────
  const scmStagedFiles   = (status?.files ?? []).filter(f => f.status[0] !== ' ' && f.status[0] !== '?');
  const scmUnstagedFiles = (status?.files ?? []).filter(f => f.status !== '??' && f.status[1] !== ' ' && f.status[1] !== '?');
  const scmUntrackedFiles = (status?.files ?? []).filter(f => f.status === '??' || f.status === '? ');

  async function handleScmStage(paths: string[]) {
    if (!selectedRepo) return;
    try { await apiClient.codeEditorStageFiles(selectedRepo.id, paths); }
    catch (e: any) { addToast("error", e.message || "Erro ao adicionar ao staging"); }
    await loadStatus(selectedRepo.id);
  }
  async function handleScmUnstage(paths: string[]) {
    if (!selectedRepo) return;
    try { await apiClient.codeEditorUnstage(selectedRepo.id, paths); }
    catch (e: any) { addToast("error", e.message || "Erro ao remover do staging"); }
    await loadStatus(selectedRepo.id);
  }
  async function handleScmCommit(andPush: boolean) {
    if (!selectedRepo || !scmCommitMsg.trim()) return;
    setScmLoading(true);
    try {
      await apiClient.codeEditorCommit(selectedRepo.id, scmCommitMsg.trim());
      setScmCommitMsg("");
      addToast("success", "Commit criado com sucesso");
      await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id)]);
      if (andPush) setSseDialog({ title: "Git Push", endpoint: `/code-editor/repos/${selectedRepo.id}/push`, body: activeToken() ? { token: activeToken() } : undefined });
    } catch (e: any) { addToast("error", e.message || "Erro ao commitar"); }
    setScmLoading(false);
  }

  async function deleteRepo(id: string) {
    if (!await showConfirm(`Remover repositório "${id}" localmente?`)) return;
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

  async function handleCherryPick(hash: string) {
    if (!selectedRepo) return;
    if (!await showConfirm(`Aplicar commit ${hash.slice(0, 7)} no branch atual?`)) return;
    try {
      const r = await apiClient.codeEditorCherryPick(selectedRepo.id, hash);
      addToast("success", "Cherry-pick aplicado");
      await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id)]);
      return r.message;
    } catch (e: any) {
      addToast("error", e.message || "Erro no cherry-pick");
    }
  }

  async function loadTags(id: string) {
    try {
      const r = await apiClient.codeEditorListTags(id);
      setTags(r.tags ?? []);
    } catch (_) {}
  }

  async function handleCreateTag(name: string, hash: string, message?: string) {
    if (!selectedRepo) return;
    try {
      await apiClient.codeEditorCreateTag(selectedRepo.id, name, hash, message);
      addToast("success", `Tag ${name} criada`);
      await loadTags(selectedRepo.id);
    } catch (e: any) {
      addToast("error", e.message || "Erro ao criar tag");
    }
  }

  async function handleDeleteTag(name: string) {
    if (!selectedRepo) return;
    if (!await showConfirm(`Deletar tag "${name}"?`)) return;
    try {
      await apiClient.codeEditorDeleteTag(selectedRepo.id, name);
      addToast("success", `Tag ${name} removida`);
      await loadTags(selectedRepo.id);
    } catch (e: any) {
      addToast("error", e.message || "Erro ao deletar tag");
    }
  }

  // ── Blame: mostra anotação apenas na linha do cursor ──
  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    if (blameDecorationsRef.current) {
      blameDecorationsRef.current.clear();
      blameDecorationsRef.current = null;
    }
    if (!showBlame || blameMap.size === 0) return;

    const b = blameMap.get(cursorLine);
    if (!b) return;

    const styleId = "blame-inline-style";
    if (!document.getElementById(styleId)) {
      const style = document.createElement("style");
      style.id = styleId;
      style.textContent = `.blame-inline { color: #6b7280; font-size: 11px; font-style: italic; pointer-events: none; }`;
      document.head.appendChild(style);
    }

    blameDecorationsRef.current = editor.createDecorationsCollection([{
      range: { startLineNumber: b.line, startColumn: 1, endLineNumber: b.line, endColumn: 999999 },
      options: {
        after: {
          content: `    ${b.author} · ${b.date} · ${b.short}`,
          inlineClassName: "blame-inline",
        },
        isWholeLine: false,
      },
    }]);
  }, [showBlame, blameMap, cursorLine]);

  async function loadBlame() {
    if (!selectedRepo || !activeTab) return;
    if (showBlame) {
      // Desativar blame
      setShowBlame(false);
      setBlameLines([]);
      return;
    }
    // Ativar blame
    setShowBlame(true);
    try {
      const r = await apiClient.codeEditorGetBlame(selectedRepo.id, activeTab.node.path);
      setBlameLines(r.lines ?? []);
    } catch (_) {
      setBlameLines([]);
    }
  }

  async function handleUpload(dirPath: string, files: FileList) {
    if (!selectedRepo) return;
    try {
      const r = await apiClient.codeEditorUploadFiles(selectedRepo.id, dirPath, files);
      if (r.created?.length > 0) {
        addToast("success", `${r.created.length} arquivo(s) enviado(s)`);
        await loadTree(selectedRepo.id);
        await loadStatus(selectedRepo.id);
      }
    } catch (e: any) {
      addToast("error", e.message || "Erro no upload");
    }
  }

  async function handleReplace(dryRun: boolean) {
    if (!selectedRepo || !replaceQuery.trim()) return;
    setReplaceLoading(true);
    setReplaceError("");
    try {
      const req: CodeEditorReplaceRequest = {
        query: replaceQuery,
        replacement: replaceWith,
        is_regex: replaceRegex,
        glob: replaceGlob,
        dry_run: dryRun,
      };
      const r = await apiClient.codeEditorReplaceInFiles(selectedRepo.id, req);
      setReplaceMatches(r.matches ?? []);
      setReplaceModified(r.modified_files);
      setReplaceApplied(r.applied);
      if (!dryRun && r.applied) {
        addToast("success", `Substituição aplicada em ${r.modified_files} arquivo(s)`);
        await loadStatus(selectedRepo.id);
      }
    } catch (e: any) {
      setReplaceError(e.message || "Erro na substituição");
    } finally {
      setReplaceLoading(false);
    }
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

  // ── K8s integration (Fase 9) ──────────────────────────────────────────────

  // Detecta manifests K8s no arquivo ativo
  const k8sManifest = useMemo(() => {
    const content = activeTab?.currentContent ?? "";
    if (!content.includes("apiVersion:") || !content.includes("kind:")) return null;
    const kind = content.match(/^kind:\s*(\S+)/m)?.[1] ?? "";
    const name = content.match(/^  name:\s*(\S+)/m)?.[1] ?? content.match(/^name:\s*(\S+)/m)?.[1] ?? "";
    const ns = content.match(/^  namespace:\s*(\S+)/m)?.[1] ?? content.match(/^namespace:\s*(\S+)/m)?.[1] ?? "";
    if (!kind) return null;
    return { kind, name, namespace: ns };
  }, [activeTab?.currentContent]);

  // Carrega contexts quando o painel K8s é aberto
  useEffect(() => {
    if (sidePanel !== "k8s" || k8sContexts.length > 0) return;
    apiClient.k8sListContexts()
      .then(r => {
        setK8sContexts(r.contexts ?? []);
        if (!k8sCluster && r.contexts?.length) {
          const saved = localStorage.getItem("ce_k8s_cluster") ?? "";
          setK8sCluster(saved && r.contexts.includes(saved) ? saved : r.contexts[0]);
        }
      })
      .catch(() => {});
  }, [sidePanel]);

  // Auto-scroll ao adicionar linhas de output
  useEffect(() => {
    if (k8sOutputRef.current) k8sOutputRef.current.scrollTop = k8sOutputRef.current.scrollHeight;
  }, [k8sOutput]);

  function classifyK8sLine(line: string): "ok" | "err" | "warn" | "info" {
    const low = line.toLowerCase();
    if (low.includes("error") || low.includes("failed") || low.includes("invalid")) return "err";
    if (low.includes("warning") || low.includes("warn")) return "warn";
    if (low.includes("created") || low.includes("configured") || low.includes("unchanged") || low.includes("applied") || low.includes("serverdryr")) return "ok";
    return "info";
  }

  async function runK8sSSE(action: "diff" | "dry-run" | "apply") {
    if (!selectedRepo || !k8sCluster || k8sRunning) return;
    const content = activeTab?.currentContent ?? "";
    if (!content.trim()) return;
    setK8sRunning(action);
    setK8sOutput([]);
    const token = localStorage.getItem("auth_token") || "";
    const endpoint = `${API_BASE}/code-editor/repos/${selectedRepo.id}/k8s/${action}`;
    try {
      const res = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        body: JSON.stringify({ cluster: k8sCluster, content }),
      });
      if (!res.body) { setK8sOutput([{ text: "Sem resposta do servidor", kind: "err" }]); return; }
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
            if (d.line !== undefined) setK8sOutput(o => [...o, { text: d.line, kind: classifyK8sLine(d.line) }]);
            if (d.done && d.error) setK8sOutput(o => [...o, { text: `Erro: ${d.error}`, kind: "err" }]);
          } catch (_) {}
        }
      }
    } catch (e: any) {
      setK8sOutput(o => [...o, { text: `Erro: ${e.message}`, kind: "err" }]);
    } finally {
      setK8sRunning(null);
    }
  }

  async function runK8sGet() {
    if (!selectedRepo || !k8sCluster || k8sRunning || !k8sManifest?.kind || !k8sManifest?.name) return;
    setK8sRunning("get");
    setK8sOutput([]);
    try {
      const r = await apiClient.k8sGetResource(
        selectedRepo.id, k8sCluster, k8sManifest.kind, k8sManifest.name, k8sManifest.namespace,
      );
      // Abre o conteúdo em nova aba no editor como arquivo virtual
      const virtualNode: CodeEditorFileNode = {
        name: `${k8sManifest.kind}-${k8sManifest.name}-cluster.yaml`,
        path: `__k8s_virtual__/${k8sManifest.kind}-${k8sManifest.name}.yaml`,
        type: "file",
      };
      const existingIdx = openTabs.findIndex(t => t.node.path === virtualNode.path);
      if (existingIdx >= 0) {
        setOpenTabs(prev => prev.map((t, i) => i === existingIdx
          ? { ...t, currentContent: r.content, savedContent: r.content, initialContent: r.content }
          : t));
        setActiveTabIdx(existingIdx);
      } else {
        const newTab: OpenTab = { node: virtualNode, initialContent: r.content, currentContent: r.content, savedContent: r.content, repoId: selectedRepo.id };
        setOpenTabs(prev => [...prev, newTab]);
        setActiveTabIdx(openTabs.length);
      }
      addToast("success", `${k8sManifest.kind}/${k8sManifest.name} carregado`);
    } catch (e: any) {
      setK8sOutput([{ text: `Erro: ${e.message}`, kind: "err" }]);
    } finally {
      setK8sRunning(null);
    }
  }

  // Mantém ref atualizado para evitar stale closure no addCommand do Monaco
  useEffect(() => { saveFileRef.current = saveFile; });
  useEffect(() => { openFileRef.current = openFile; });

  // Sincroniza fontSize/wordWrap com Monaco sem recriar o editor
  useEffect(() => {
    editorRef.current?.updateOptions({ fontSize, wordWrap: wordWrap ? "on" : "off" });
  }, [fontSize, wordWrap]);

  // Fecha context menu ao clicar fora
  useEffect(() => {
    if (!contextMenu) return;
    const close = () => setContextMenu(null);
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [contextMenu]);

  // Scroll para o arquivo revelado na tree
  useEffect(() => {
    if (!revealPath) return;
    const el = document.querySelector("[data-reveal-path]");
    if (el) el.scrollIntoView({ block: "center", behavior: "smooth" });
    const t = setTimeout(() => setRevealPath(null), 1500);
    return () => clearTimeout(t);
  }, [revealPath]);

  // Ctrl+P global (quando o Monaco não está focado)
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.ctrlKey && !e.altKey && !e.shiftKey && e.key === "p" && selectedRepo) {
        e.preventDefault();
        setShowQuickOpen(true);
        setQuickOpenQuery("");
        setQuickOpenIdx(0);
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [selectedRepo]);

  const handleEditorMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    editor.addCommand(2048 | 49, () => saveFileRef.current()); // Ctrl+S
    editor.addCommand(512 | 1024 | 36, () => formatFile()); // Shift+Alt+F
    editor.addCommand(2048 | 46, () => { setShowQuickOpen(true); setQuickOpenQuery(""); setQuickOpenIdx(0); }); // Ctrl+P
    editor.onDidChangeCursorPosition(e => {
      setCursorLine(e.position.lineNumber);
      setCursorCol(e.position.column);
    });

    // ── TypeScript/JavaScript — worker built-in do Monaco ──────────────────
    // Configura uma única vez (flag global para evitar reconfiguração)
    if (!(window as any).__monacoTSConfigured) {
      (window as any).__monacoTSConfigured = true;
      const ts = monacoInstance.languages.typescript;

      const compilerOpts = {
        target: ts.ScriptTarget.ESNext,
        moduleResolution: ts.ModuleResolutionKind.NodeJs,
        module: ts.ModuleKind.ESNext,
        jsx: ts.JsxEmit.ReactJSX,
        allowJs: true,
        allowSyntheticDefaultImports: true,
        esModuleInterop: true,
        strict: false,
        noImplicitAny: false,
        skipLibCheck: true,
      };
      const diagOpts = {
        noSemanticValidation: false,
        noSyntaxValidation: false,
        onlyVisible: true,
      };
      ts.typescriptDefaults.setCompilerOptions(compilerOpts);
      ts.typescriptDefaults.setDiagnosticsOptions(diagOpts);
      ts.javascriptDefaults.setCompilerOptions({ ...compilerOpts, checkJs: false });
      ts.javascriptDefaults.setDiagnosticsOptions({ noSemanticValidation: true, noSyntaxValidation: false });
    }

    // ── Go-to-definition cross-file: intercepta abertura de URIs lspdef:// ──
    // Monaco standalone não sabe abrir arquivos externos; substituímos o
    // openCodeEditor do serviço interno para usar nosso sistema de abas.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const editorSvc = (editor as any)._codeEditorService;
    if (editorSvc && typeof editorSvc.openCodeEditor === 'function' && !(window as any).__lspDefHandlerRegistered) {
      (window as any).__lspDefHandlerRegistered = true;
      editorSvc.openCodeEditor = async (input: any) => {
        const uri = input?.resource;
        if (!uri || uri.scheme !== 'lspdef') return null;

        // uri.path = "/<repoId>/<relative/path/to/file>"
        const parts = (uri.path as string).replace(/^\//, '').split('/');
        const [repoId, ...fileParts] = parts;
        const filePath = fileParts.join('/');
        const line: number = input.options?.selection?.startLineNumber ?? 1;
        const col: number  = input.options?.selection?.startColumn  ?? 1;

        const activeFilePath = (window as any).__lspActiveFilePath as string | undefined;
        if (filePath === activeFilePath) {
          // Mesmo arquivo — navega direto
          editorRef.current?.revealLineInCenter(line);
          editorRef.current?.setPosition({ lineNumber: line, column: col });
          editorRef.current?.focus();
        } else {
          // Arquivo diferente — abre em nova aba e navega após mount
          pendingNavigationRef.current = { line, col };
          const fileName = filePath.split('/').pop() ?? filePath;
          await openFileRef.current({ path: filePath, name: fileName, type: 'file', children: [] });
        }
        return null;
      };
    }

    // ── Go via gopls — providers registrados uma vez por sessão ──────────────
    if (!(window as any).__monacoGoLSPRegistered) {
      (window as any).__monacoGoLSPRegistered = true;

      // mapa LSP kind → Monaco kind
      const lspKindToMonaco = (k: number): MonacoEditorNS.languages.CompletionItemKind => {
        const m = monacoInstance.languages.CompletionItemKind;
        const map: Record<number, MonacoEditorNS.languages.CompletionItemKind> = {
          1: m.Text, 2: m.Method, 3: m.Function, 4: m.Constructor, 5: m.Field,
          6: m.Variable, 7: m.Class, 8: m.Interface, 9: m.Module, 10: m.Property,
          12: m.Value, 13: m.Enum, 14: m.Keyword, 15: m.Snippet,
          16: m.Color, 17: m.File, 18: m.Reference, 22: m.TypeParameter,
        };
        return map[k] ?? m.Text;
      };

      const lspSevToMonaco = (sev: number): MonacoEditorNS.MarkerSeverity => {
        if (sev === 1) return monacoInstance.MarkerSeverity.Error;
        if (sev === 2) return monacoInstance.MarkerSeverity.Warning;
        if (sev === 3) return monacoInstance.MarkerSeverity.Info;
        return monacoInstance.MarkerSeverity.Hint;
      };

      // Completion provider
      const compDisp = monacoInstance.languages.registerCompletionItemProvider("go", {
        triggerCharacters: [".", "(", " ", "\t"],
        provideCompletionItems: async (model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return { suggestions: [] };
          try {
            const result = await apiClient.lspComplete(
              repoId, "go", filePath, model.getValue(),
              position.lineNumber - 1, position.column - 1,
              lspVersionRef.current
            );
            const suggestions = (result.items ?? []).map(item => ({
              label: item.label,
              kind: lspKindToMonaco(item.kind),
              detail: item.detail,
              documentation: item.documentation ? { value: item.documentation } : undefined,
              insertText: item.insertText ?? item.label,
              range: {
                startLineNumber: position.lineNumber,
                endLineNumber: position.lineNumber,
                startColumn: position.column,
                endColumn: position.column,
              },
            } as MonacoEditorNS.languages.CompletionItem));
            return { suggestions };
          } catch { return { suggestions: [] }; }
        },
      });

      // Hover provider
      const hoverDisp = monacoInstance.languages.registerHoverProvider("go", {
        provideHover: async (model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return null;
          try {
            const result = await apiClient.lspHover(
              repoId, "go", filePath, model.getValue(),
              position.lineNumber - 1, position.column - 1,
              lspVersionRef.current
            );
            if (!result?.contents) return null;
            return {
              contents: [{ value: "```go\n" + result.contents + "\n```" }],
              range: result.range ? {
                startLineNumber: result.range.start.line + 1,
                startColumn: result.range.start.character + 1,
                endLineNumber: result.range.end.line + 1,
                endColumn: result.range.end.character + 1,
              } : undefined,
            };
          } catch { return null; }
        },
      });

      // Definition provider
      const defDisp = monacoInstance.languages.registerDefinitionProvider("go", {
        provideDefinition: async (_model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return null;
          try {
            const result = await apiClient.lspDefinition(
              repoId, "go", filePath,
              position.lineNumber - 1, position.column - 1
            );
            if (!result?.locations?.length) return null;
            return result.locations.map(loc => ({
              uri: monacoInstance.Uri.from({ scheme: 'lspdef', path: `/${repoId}/${loc.path}` }),
              range: {
                startLineNumber: loc.range.start.line + 1,
                startColumn: loc.range.start.character + 1,
                endLineNumber: loc.range.end.line + 1,
                endColumn: loc.range.end.character + 1,
              },
            }));
          } catch { return null; }
        },
      });

      // guarda disposables para limpar se necessário
      lspProviderDisposables.current = [compDisp, hoverDisp, defDisp];

      // expõe helper de diagnósticos globalmente (genérico por owner/source)
      (window as any).__lspApplyDiagnostics = (
        model: MonacoEditorNS.editor.ITextModel,
        diagnostics: Array<{ range: { start: { line: number; character: number }; end: { line: number; character: number } }; severity: number; message: string; source?: string }>,
        owner = "lsp"
      ) => {
        const markers = diagnostics.map(d => ({
          startLineNumber: d.range.start.line + 1,
          startColumn:  d.range.start.character + 1,
          endLineNumber: d.range.end.line + 1,
          endColumn: d.range.end.character + 1,
          severity: lspSevToMonaco(d.severity),
          message: d.message,
          source: d.source ?? owner,
        } as MonacoEditorNS.editor.IMarkerData));
        monacoInstance.editor.setModelMarkers(model, owner, markers);
      };
    }

    // ── Python via pyright — providers registrados uma vez por sessão ────────
    if (!(window as any).__monacoPyLSPRegistered) {
      (window as any).__monacoPyLSPRegistered = true;

      const lspKindToMonaco = (k: number): MonacoEditorNS.languages.CompletionItemKind => {
        const m = monacoInstance.languages.CompletionItemKind;
        const map: Record<number, MonacoEditorNS.languages.CompletionItemKind> = {
          1: m.Text, 2: m.Method, 3: m.Function, 4: m.Constructor, 5: m.Field,
          6: m.Variable, 7: m.Class, 8: m.Interface, 9: m.Module, 10: m.Property,
          12: m.Value, 13: m.Enum, 14: m.Keyword, 15: m.Snippet,
          16: m.Color, 17: m.File, 18: m.Reference, 22: m.TypeParameter,
        };
        return map[k] ?? m.Text;
      };

      monacoInstance.languages.registerCompletionItemProvider("python", {
        triggerCharacters: [".", "(", " ", "\t", "["],
        provideCompletionItems: async (model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return { suggestions: [] };
          try {
            const result = await apiClient.lspComplete(
              repoId, "python", filePath, model.getValue(),
              position.lineNumber - 1, position.column - 1,
              lspVersionRef.current
            );
            const suggestions = (result.items ?? []).map(item => ({
              label: item.label,
              kind: lspKindToMonaco(item.kind),
              detail: item.detail,
              documentation: item.documentation ? { value: item.documentation } : undefined,
              insertText: item.insertText ?? item.label,
              range: {
                startLineNumber: position.lineNumber,
                endLineNumber: position.lineNumber,
                startColumn: position.column,
                endColumn: position.column,
              },
            } as MonacoEditorNS.languages.CompletionItem));
            return { suggestions };
          } catch { return { suggestions: [] }; }
        },
      });

      monacoInstance.languages.registerHoverProvider("python", {
        provideHover: async (model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return null;
          try {
            const result = await apiClient.lspHover(
              repoId, "python", filePath, model.getValue(),
              position.lineNumber - 1, position.column - 1,
              lspVersionRef.current
            );
            if (!result?.contents) return null;
            return {
              contents: [{ value: result.contents }],
              range: result.range ? {
                startLineNumber: result.range.start.line + 1,
                startColumn: result.range.start.character + 1,
                endLineNumber: result.range.end.line + 1,
                endColumn: result.range.end.character + 1,
              } : undefined,
            };
          } catch { return null; }
        },
      });

      monacoInstance.languages.registerDefinitionProvider("python", {
        provideDefinition: async (_model, position) => {
          const repoId = (window as any).__lspActiveRepoId as string | undefined;
          const filePath = (window as any).__lspActiveFilePath as string | undefined;
          if (!repoId || !filePath) return null;
          try {
            const result = await apiClient.lspDefinition(
              repoId, "python", filePath,
              position.lineNumber - 1, position.column - 1
            );
            if (!result?.locations?.length) return null;
            return result.locations.map(loc => ({
              uri: monacoInstance.Uri.from({ scheme: 'lspdef', path: `/${repoId}/${loc.path}` }),
              range: {
                startLineNumber: loc.range.start.line + 1,
                startColumn: loc.range.start.character + 1,
                endLineNumber: loc.range.end.line + 1,
                endColumn: loc.range.end.character + 1,
              },
            }));
          } catch { return null; }
        },
      });
    }
  };

  const sidePanels = [
    { id: "files" as const, label: "Arquivos" },
    { id: "source-control" as const, label: `Source Control${modifiedPaths.size > 0 ? ` (${modifiedPaths.size})` : ""}` },
    { id: "branches" as const, label: `Branches${branches ? ` (${branches.local.length})` : ""}` },
    { id: "git" as const, label: `Git` },
    { id: "log" as const, label: "Log" },
    { id: "replace" as const, label: "Replace" },
    { id: "k8s" as const, label: "K8s" },
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
              onClick={() => setSseDialog({ title: "Git Pull", endpoint: `/code-editor/repos/${selectedRepo.id}/pull`, body: activeToken() ? { token: activeToken() } : undefined })}>
              <Download className="w-3 h-3" />Pull
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => { setSidePanel("git"); setShowCommit(true); }}
              disabled={modifiedPaths.size === 0}>
              <GitCommit className="w-3 h-3" />Commit
              {modifiedPaths.size > 0 && <span className="bg-yellow-500 text-black text-[10px] px-1 rounded-full">{modifiedPaths.size}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => setSseDialog({ title: "Git Push", endpoint: `/code-editor/repos/${selectedRepo.id}/push`, body: activeToken() ? { token: activeToken() } : undefined })}>
              <Upload className="w-3 h-3" />Push
              {status?.ahead && status.ahead !== "0" && <span className="bg-blue-500 text-white text-[10px] px-1 rounded-full">{status.ahead}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1"
              onClick={() => { setSidePanel("branches"); setShowBranch(true); }}>
              <Plus className="w-3 h-3" />Branch
            </Button>
            {branches?.current && branches.current !== "main" && branches.current !== "master" && (
              <Button variant="ghost" size="sm" className="h-6 text-xs gap-1"
                title={`Criar Pull Request: ${selectedRepo.owner}/${selectedRepo.repo}`}
                onClick={() => setShowCreatePR(true)}>
                <GitPullRequest className="w-3 h-3" />PR
              </Button>
            )}
          </>
        )}

        {selectedRepo && (
          <Button variant={showTerminal ? "default" : "ghost"} size="sm" className="h-6 text-xs gap-1"
            title="Terminal integrado" onClick={() => setShowTerminal(v => !v)}>
            <Terminal className="w-3 h-3" />Terminal
          </Button>
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
          <div className="flex items-center border-b border-border/50 flex-shrink-0">
            <div className="flex overflow-x-auto flex-1 min-w-0">
              {sidePanels.map(p => (
                <button key={p.id} onClick={() => setSidePanel(p.id)}
                  className={`flex-shrink-0 px-2 py-1.5 text-[11px] font-medium transition-colors whitespace-nowrap ${sidePanel === p.id ? "text-foreground border-b-2 border-primary" : "text-muted-foreground hover:text-foreground"}`}>
                  {p.label}
                </button>
              ))}
            </div>
            {/* Botões de ação da tree — visíveis apenas no painel Arquivos com repo aberto */}
            {sidePanel === "files" && selectedRepo && !grepMode && (
              <div className="flex gap-0.5 px-1 flex-shrink-0">
                <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Novo arquivo"
                  onClick={() => setCreateDialog({ mode: "file", basePath: getCreateBasePath() })}>
                  <FilePlus className="w-3 h-3" />
                </Button>
                <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Nova pasta"
                  onClick={() => setCreateDialog({ mode: "dir", basePath: getCreateBasePath() })}>
                  <FolderPlus className="w-3 h-3" />
                </Button>
                <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Atualizar árvore"
                  onClick={() => { loadTree(selectedRepo.id); loadStatus(selectedRepo.id); }}>
                  <RefreshCw className={`w-3 h-3 ${treeLoading ? "animate-spin" : ""}`} />
                </Button>
                <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Fechar repositório"
                  onClick={() => { setSelectedRepo(null); setTree([]); setOpenTabs([]); setActiveTabIdx(0); }}>
                  <X className="w-3 h-3" />
                </Button>
              </div>
            )}
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
                          <button className="flex-1 text-left text-xs truncate min-w-0" onClick={() => selectRepo(r)}>
                            <span className="font-medium">{r.owner}/{r.repo}</span>
                            <div className="flex items-center gap-1.5 mt-0.5">
                              <span className="text-muted-foreground text-[10px]">{r.current_branch}</span>
                              {r.size && <span className="text-[10px] text-muted-foreground/60 font-mono">{r.size}</span>}
                            </div>
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
                      <div
                        className="min-h-8"
                        onDragOver={e => e.preventDefault()}
                        onDrop={e => {
                          e.preventDefault();
                          const moveFrom = e.dataTransfer.getData("application/x-tree-node");
                          if (moveFrom) {
                            handleMoveFile(moveFrom, "");
                          } else if (e.dataTransfer.files.length > 0) {
                            handleUpload("", e.dataTransfer.files);
                          }
                        }}
                        onKeyDown={e => {
                          if ((e.ctrlKey || e.metaKey) && e.key === "v") { e.preventDefault(); handleClipboardPaste(""); }
                          else if (e.key === "Escape") { setClipboard(null); }
                        }}
                      >
                        {clipboard && (
                          <div className="mx-1 mb-1 px-2 py-1 rounded bg-muted/40 border border-dashed border-muted-foreground/30 flex items-center gap-1.5">
                            <span className="text-[10px] text-muted-foreground">{clipboard.op === "cut" ? "✂" : "⎘"}</span>
                            <span className="text-[10px] text-foreground/60 truncate flex-1">{clipboard.path.split("/").pop()}</span>
                            <button onClick={() => setClipboard(null)} className="text-[10px] text-muted-foreground hover:text-foreground">✕</button>
                          </div>
                        )}
                        {tree.map(node => (
                          <FileTreeNode key={node.path} node={node} selectedPath={activeTab?.node.path ?? ""}
                            onSelect={openFile} modifiedPaths={modifiedPaths} gitFileStatus={gitFileStatusMap} level={0}
                            onDelete={handleDeleteFile} onRename={n => setRenameNode(n)}
                            onHistory={n => setFileHistoryNode(n)}
                            onUpload={handleUpload}
                            onDirFocus={setFocusedDirPath}
                            onCreate={(parentPath, mode) => setCreateDialog({ mode, basePath: parentPath })}
                            onMove={handleMoveFile}
                            onClipboardOp={(path, op) => setClipboard({ path, op })}
                            onPaste={handleClipboardPaste}
                            cutPath={clipboard?.op === "cut" ? clipboard.path : undefined}
                            onContextMenu={(e, n) => setContextMenu({ x: e.clientX, y: e.clientY, node: n })}
                            revealPath={revealPath} />
                        ))}
                      </div>
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
                onBranchDiff={() => setShowBranchDiff(true)}
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

            {/* ── Painel: Source Control ── */}
            {sidePanel === "source-control" && (
              <div className="flex flex-col h-full">
                {/* Commit input + actions */}
                <div className="p-2 border-b border-border/30 flex-shrink-0 space-y-1.5">
                  <Input
                    className="h-7 text-xs"
                    placeholder={scmStagedFiles.length > 0 ? "Mensagem de commit..." : "Faça staging de arquivos primeiro"}
                    value={scmCommitMsg}
                    onChange={e => setScmCommitMsg(e.target.value)}
                    onKeyDown={e => e.key === "Enter" && scmCommitMsg.trim() && scmStagedFiles.length > 0 && handleScmCommit(false)}
                    disabled={scmStagedFiles.length === 0}
                  />
                  {scmStagedFiles.length > 0 && (
                    <div className="flex gap-1">
                      <Button size="sm" variant="outline" className="flex-1 h-6 text-[11px]"
                        onClick={() => handleScmCommit(false)} disabled={!scmCommitMsg.trim() || scmLoading}>
                        <GitCommit className="w-3 h-3 mr-1" />Commit
                      </Button>
                      <Button size="sm" className="flex-1 h-6 text-[11px]"
                        onClick={() => handleScmCommit(true)} disabled={!scmCommitMsg.trim() || scmLoading}>
                        <Upload className="w-3 h-3 mr-1" />+ Push
                      </Button>
                    </div>
                  )}
                </div>

                <ScrollArea className="flex-1">
                  <div className="p-1">

                    {/* Staged Changes */}
                    {scmStagedFiles.length > 0 && (
                      <div className="mb-1">
                        <div className="flex items-center justify-between px-1 py-1">
                          <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
                            Staged Changes ({scmStagedFiles.length})
                          </span>
                          <button onClick={() => handleScmUnstage(scmStagedFiles.map(f => f.path))}
                            title="Remover todos do staging"
                            className="text-[10px] text-muted-foreground hover:text-foreground px-1 rounded hover:bg-muted">
                            − tudo
                          </button>
                        </div>
                        {scmStagedFiles.map(f => (
                          <div key={`s-${f.path}`} className="flex items-center gap-1 px-1 py-0.5 rounded hover:bg-muted/40 group text-xs">
                            <span className={`font-bold w-4 text-center flex-shrink-0 ${scmColor(f.status)}`}>{scmBadge(f.status)}</span>
                            <button className="font-mono truncate flex-1 text-left text-foreground/70 hover:text-foreground min-w-0"
                              title={f.path}
                              onClick={() => openFile({ name: f.path.split("/").pop()!, path: f.path, type: "file" })}>
                              {f.path.split("/").pop()}
                            </button>
                            <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
                              <button onClick={() => setDiffFile(f.path)} title="Ver diff" className="p-0.5 rounded hover:bg-muted">
                                <Eye className="w-2.5 h-2.5 text-muted-foreground" />
                              </button>
                              <button onClick={() => handleScmUnstage([f.path])} title="Remover do staging" className="p-0.5 rounded hover:bg-muted">
                                <X className="w-2.5 h-2.5 text-muted-foreground hover:text-red-400" />
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Changes (unstaged) */}
                    {scmUnstagedFiles.length > 0 && (
                      <div className="mb-1">
                        <div className="flex items-center justify-between px-1 py-1">
                          <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
                            Changes ({scmUnstagedFiles.length})
                          </span>
                          <button onClick={() => handleScmStage(scmUnstagedFiles.map(f => f.path))}
                            title="Adicionar todos ao staging"
                            className="text-[10px] text-muted-foreground hover:text-foreground px-1 rounded hover:bg-muted">
                            + tudo
                          </button>
                        </div>
                        {scmUnstagedFiles.map(f => (
                          <div key={`u-${f.path}`} className="flex items-center gap-1 px-1 py-0.5 rounded hover:bg-muted/40 group text-xs">
                            <span className={`font-bold w-4 text-center flex-shrink-0 ${scmColor(f.status)}`}>
                              {f.status[1] === "M" ? "M" : f.status[1] === "D" ? "D" : scmBadge(f.status)}
                            </span>
                            <button className="font-mono truncate flex-1 text-left text-foreground/70 hover:text-foreground min-w-0"
                              title={f.path}
                              onClick={() => openFile({ name: f.path.split("/").pop()!, path: f.path, type: "file" })}>
                              {f.path.split("/").pop()}
                            </button>
                            <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
                              <button onClick={() => setDiffFile(f.path)} title="Ver diff" className="p-0.5 rounded hover:bg-muted">
                                <Eye className="w-2.5 h-2.5 text-muted-foreground" />
                              </button>
                              <button onClick={() => handleScmStage([f.path])} title="Adicionar ao staging" className="p-0.5 rounded hover:bg-muted">
                                <Plus className="w-2.5 h-2.5 text-muted-foreground hover:text-green-400" />
                              </button>
                              <button onClick={() => handleResetFile(f.path)} title="Descartar mudanças" className="p-0.5 rounded hover:bg-muted">
                                <RotateCcw className="w-2.5 h-2.5 text-red-400/70 hover:text-red-400" />
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Untracked */}
                    {scmUntrackedFiles.length > 0 && (
                      <div className="mb-1">
                        <div className="flex items-center justify-between px-1 py-1">
                          <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
                            Untracked ({scmUntrackedFiles.length})
                          </span>
                          <button onClick={() => handleScmStage(scmUntrackedFiles.map(f => f.path))}
                            title="Adicionar todos ao staging"
                            className="text-[10px] text-muted-foreground hover:text-foreground px-1 rounded hover:bg-muted">
                            + tudo
                          </button>
                        </div>
                        {scmUntrackedFiles.map(f => (
                          <div key={`t-${f.path}`} className="flex items-center gap-1 px-1 py-0.5 rounded hover:bg-muted/40 group text-xs">
                            <span className="font-bold w-4 text-center flex-shrink-0 text-green-400">U</span>
                            <span className="font-mono truncate flex-1 text-foreground/70 min-w-0" title={f.path}>
                              {f.path.split("/").pop()}
                            </span>
                            <div className="hidden group-hover:flex gap-0.5 flex-shrink-0">
                              <button onClick={() => handleScmStage([f.path])} title="Adicionar ao staging" className="p-0.5 rounded hover:bg-muted">
                                <Plus className="w-2.5 h-2.5 text-muted-foreground hover:text-green-400" />
                              </button>
                              <button onClick={() => handleResetFile(f.path)} title="Deletar arquivo" className="p-0.5 rounded hover:bg-muted">
                                <Trash2 className="w-2.5 h-2.5 text-red-400/70 hover:text-red-400" />
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Empty states */}
                    {!selectedRepo && <p className="text-xs text-muted-foreground text-center py-6">Selecione um repositório</p>}
                    {selectedRepo && status && status.files.length === 0 && (
                      <p className="text-xs text-muted-foreground text-center py-6">✓ Árvore limpa</p>
                    )}
                  </div>
                </ScrollArea>

                {selectedRepo && (
                  <div className="flex gap-1 p-2 border-t border-border/30 flex-shrink-0">
                    <Button variant="outline" size="sm" className="flex-1 h-6 text-xs"
                      onClick={() => loadStatus(selectedRepo.id)}>
                      <RefreshCw className="w-3 h-3 mr-1" />Atualizar
                    </Button>
                    {(scmUnstagedFiles.length > 0 || scmUntrackedFiles.length > 0) && (
                      <Button size="sm" className="h-6 text-xs px-2"
                        onClick={() => handleScmStage([...scmUnstagedFiles, ...scmUntrackedFiles].map(f => f.path))}>
                        <Plus className="w-3 h-3 mr-1" />Stage All
                      </Button>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* ── Painel: Log ── */}
            {sidePanel === "log" && (
              <div className="flex flex-col h-full">
                {/* Sub-abas: Commits | Tags */}
                <div className="flex border-b border-border/30 flex-shrink-0">
                  {(["commits", "tags"] as const).map(t => (
                    <button key={t} onClick={() => setLogTab(t)}
                      className={`flex-1 py-1 text-[10px] font-medium transition-colors flex items-center justify-center gap-1 ${logTab === t ? "text-foreground border-b-2 border-primary" : "text-muted-foreground hover:text-foreground"}`}>
                      {t === "commits" ? <><GitCommit className="w-3 h-3" />Commits</> : <><Tag className="w-3 h-3" />Tags ({tags.length})</>}
                    </button>
                  ))}
                </div>
                <ScrollArea className="flex-1">
                  {logTab === "commits" && (
                          <div className="p-2 space-y-2">
                            {selectedRepo && (
                              <Button variant="ghost" size="sm" className="w-full h-6 text-xs" onClick={() => loadLog(selectedRepo.id)}>
                                <RefreshCw className="w-3 h-3 mr-1" />Atualizar
                              </Button>
                            )}
                            {log.map(entry => (
                              <div key={entry.hash} className="border-b border-border/30 pb-2 group">
                                <p className="text-xs text-foreground/90 leading-tight">{entry.message}</p>
                                <div className="flex items-center gap-1 mt-0.5">
                                  <Clock className="w-2.5 h-2.5 text-muted-foreground" />
                                  <span className="text-[10px] text-muted-foreground">{entry.when} · {entry.author}</span>
                                </div>
                                <div className="flex items-center gap-1 mt-0.5">
                                  <span className="font-mono text-[10px] text-muted-foreground/60">{entry.hash.slice(0, 7)}</span>
                                  <div className="flex gap-1 opacity-0 group-hover:opacity-100 ml-1">
                                    <button title="Cherry-pick para branch atual"
                                      className="text-[10px] text-primary hover:underline flex items-center gap-0.5"
                                      onClick={() => handleCherryPick(entry.hash)}>
                                      <Cherry className="w-2.5 h-2.5" />pick
                                    </button>
                                    <button title="Criar tag neste commit"
                                      className="text-[10px] text-muted-foreground hover:text-foreground flex items-center gap-0.5"
                                      onClick={() => setShowCreateTag({ hash: entry.hash })}>
                                      <Tag className="w-2.5 h-2.5" />tag
                                    </button>
                                  </div>
                                </div>
                              </div>
                            ))}
                            {log.length === 0 && <p className="text-xs text-muted-foreground text-center py-4">Sem commits</p>}
                          </div>
                        )}
                        {logTab === "tags" && (
                          <div className="p-2 space-y-1">
                            {selectedRepo && (
                              <div className="flex gap-1 mb-2">
                                <Button variant="ghost" size="sm" className="flex-1 h-6 text-xs" onClick={() => loadTags(selectedRepo.id)}>
                                  <RefreshCw className="w-3 h-3 mr-1" />Atualizar
                                </Button>
                                <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={() => setShowCreateTag({ hash: "" })}>
                                  <Plus className="w-3 h-3 mr-1" />Nova tag
                                </Button>
                              </div>
                            )}
                            {tags.map(tag => (
                              <div key={tag.name} className="group flex items-start justify-between border-b border-border/20 pb-1">
                                <div>
                                  <p className="text-xs font-medium text-foreground/90">{tag.name}</p>
                                  <p className="text-[10px] text-muted-foreground">{tag.date} {tag.commit && `· ${tag.commit}`}</p>
                                </div>
                                <Button variant="ghost" size="sm" className="h-5 w-5 p-0 opacity-0 group-hover:opacity-100 flex-shrink-0"
                                  onClick={() => handleDeleteTag(tag.name)}>
                                  <Trash2 className="w-3 h-3 text-red-400" />
                                </Button>
                              </div>
                            ))}
                            {tags.length === 0 && <p className="text-xs text-muted-foreground text-center py-4">Sem tags</p>}
                          </div>
                        )}
                  </ScrollArea>
              </div>
            )}

            {/* ── Painel: Replace ── */}
            {sidePanel === "replace" && (
              <div className="flex flex-col h-full">
                <ScrollArea className="flex-1">
                  <div className="p-2 space-y-2">
                    <p className="text-xs font-medium text-muted-foreground">Find & Replace Global</p>
                    <div>
                      <label className="text-[10px] text-muted-foreground">Buscar</label>
                      <Input className="h-6 text-xs mt-0.5 font-mono" placeholder="texto ou regex..."
                        value={replaceQuery} onChange={e => setReplaceQuery(e.target.value)}
                        onKeyDown={e => e.key === "Enter" && handleReplace(true)} />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground">Substituir por</label>
                      <Input className="h-6 text-xs mt-0.5 font-mono" placeholder="substituição..."
                        value={replaceWith} onChange={e => setReplaceWith(e.target.value)} />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground">Glob (ex: *.go)</label>
                      <Input className="h-6 text-xs mt-0.5 font-mono" placeholder="*.go, *.ts..."
                        value={replaceGlob} onChange={e => setReplaceGlob(e.target.value)} />
                    </div>
                    <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                      <input type="checkbox" checked={replaceRegex} onChange={e => setReplaceRegex(e.target.checked)} className="w-3 h-3" />
                      Regex
                    </label>
                    {replaceError && <p className="text-xs text-red-400">{replaceError}</p>}
                    <div className="flex gap-1">
                      <Button size="sm" variant="outline" className="flex-1 h-6 text-xs"
                        onClick={() => { setReplaceMatches([]); handleReplace(true); }}
                        disabled={replaceLoading || !replaceQuery.trim() || !selectedRepo}>
                        {replaceLoading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <Search className="w-3 h-3 mr-1" />}
                        Buscar
                      </Button>
                    </div>
                    {replaceMatches.length > 0 && !replaceApplied && (
                      <Button size="sm" className="w-full h-6 text-xs"
                        onClick={() => handleReplace(false)}
                        disabled={replaceLoading}>
                        <Replace className="w-3 h-3 mr-1" />
                        Substituir tudo ({replaceModified} arquivo{replaceModified !== 1 ? "s" : ""})
                      </Button>
                    )}
                    {replaceApplied && (
                      <p className="text-xs text-green-400">✓ Aplicado em {replaceModified} arquivo(s)</p>
                    )}
                    {replaceMatches.length > 0 && (
                      <div className="space-y-1 mt-1">
                        <p className="text-[10px] text-muted-foreground">{replaceMatches.length} ocorrência(s)</p>
                        {Object.entries(
                          replaceMatches.reduce((acc, m) => {
                            (acc[m.file] = acc[m.file] || []).push(m);
                            return acc;
                          }, {} as Record<string, typeof replaceMatches>)
                        ).map(([file, fileMatches]) => (
                          <div key={file} className="border border-border/30 rounded p-1">
                            <p className="text-[10px] font-mono text-primary truncate mb-1">{file}</p>
                            {fileMatches.map((m, i) => (
                              <div key={i} className="text-[10px] font-mono border-b border-border/20 pb-0.5 mb-0.5">
                                <span className="text-muted-foreground mr-1">L{m.line}</span>
                                <span className="text-red-400/70 line-through">{m.before.trim()}</span>
                                <span className="mx-1 text-muted-foreground">→</span>
                                <span className="text-green-400">{m.after.trim()}</span>
                              </div>
                            ))}
                          </div>
                        ))}
                      </div>
                    )}
                    {replaceMatches.length === 0 && !replaceLoading && replaceQuery && (
                      <p className="text-xs text-muted-foreground italic text-center py-2">Nenhuma ocorrência</p>
                    )}
                  </div>
                </ScrollArea>
              </div>
            )}

            {/* ── Painel K8s (Fase 9) ── */}
            {sidePanel === "k8s" && (
              <div className="flex flex-col h-full min-h-0">
                <div className="p-2 space-y-2 flex-shrink-0">
                  <p className="text-xs font-medium text-muted-foreground flex items-center gap-1">
                    <Layers className="w-3 h-3" /> Integração Kubernetes
                  </p>

                  {/* Cluster selector */}
                  <div>
                    <label className="text-[10px] text-muted-foreground">Cluster (context)</label>
                    <select
                      className="w-full mt-0.5 text-xs bg-muted border border-border/50 rounded px-2 py-1 text-foreground"
                      value={k8sCluster}
                      onChange={e => { setK8sCluster(e.target.value); localStorage.setItem("ce_k8s_cluster", e.target.value); }}
                    >
                      {k8sContexts.length === 0 && <option value="">Carregando...</option>}
                      {k8sContexts.map(ctx => <option key={ctx} value={ctx}>{ctx}</option>)}
                    </select>
                  </div>

                  {/* Manifest detectado */}
                  {k8sManifest ? (
                    <div className="bg-muted/40 border border-border/30 rounded p-1.5 text-[10px] font-mono text-foreground/80">
                      <span className="text-primary font-semibold">{k8sManifest.kind}</span>
                      {k8sManifest.name && <span> / {k8sManifest.name}</span>}
                      {k8sManifest.namespace && <span className="text-muted-foreground"> ({k8sManifest.namespace})</span>}
                    </div>
                  ) : (
                    <p className="text-[10px] text-muted-foreground italic">Nenhum manifest K8s detectado no arquivo ativo</p>
                  )}

                  {/* Ações */}
                  <div className="grid grid-cols-2 gap-1">
                    <Button
                      size="sm" variant="outline" className="h-7 text-xs gap-1"
                      disabled={!k8sCluster || !selectedRepo || !!k8sRunning}
                      onClick={() => runK8sSSE("diff")}
                    >
                      {k8sRunning === "diff" ? <Loader2 className="w-3 h-3 animate-spin" /> : <GitCompare className="w-3 h-3" />}
                      Diff
                    </Button>
                    <Button
                      size="sm" variant="outline" className="h-7 text-xs gap-1"
                      disabled={!k8sCluster || !selectedRepo || !!k8sRunning}
                      onClick={() => runK8sSSE("dry-run")}
                    >
                      {k8sRunning === "dry-run" ? <Loader2 className="w-3 h-3 animate-spin" /> : <FlaskConical className="w-3 h-3" />}
                      Dry Run
                    </Button>
                    <Button
                      size="sm" variant="default" className="h-7 text-xs gap-1 col-span-2"
                      disabled={!k8sCluster || !selectedRepo || !!k8sRunning}
                      onClick={async () => { if (await showConfirm(`Aplicar o manifest no cluster "${k8sCluster}"?`)) runK8sSSE("apply"); }}
                    >
                      {k8sRunning === "apply" ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}
                      Apply
                    </Button>
                    <Button
                      size="sm" variant="outline" className="h-7 text-xs gap-1 col-span-2"
                      disabled={!k8sCluster || !selectedRepo || !!k8sRunning || !k8sManifest?.name}
                      onClick={() => runK8sGet()}
                    >
                      {k8sRunning === "get" ? <Loader2 className="w-3 h-3 animate-spin" /> : <ServerCrash className="w-3 h-3" />}
                      Get recurso atual
                    </Button>
                  </div>

                  {/* Botão limpar output */}
                  {k8sOutput.length > 0 && (
                    <button
                      onClick={() => setK8sOutput([])}
                      className="text-[10px] text-muted-foreground hover:text-foreground flex items-center gap-1"
                    >
                      <X className="w-2.5 h-2.5" /> Limpar output
                    </button>
                  )}
                </div>

                {/* Output panel */}
                <div
                  ref={k8sOutputRef}
                  className="flex-1 min-h-0 overflow-y-auto border-t border-border/30 bg-black/20 p-2"
                >
                  {k8sOutput.length === 0 && !k8sRunning && (
                    <p className="text-[10px] text-muted-foreground italic text-center mt-4">Output aparecerá aqui</p>
                  )}
                  {k8sRunning && k8sOutput.length === 0 && (
                    <p className="text-[10px] text-muted-foreground italic text-center mt-4 flex items-center justify-center gap-1">
                      <Loader2 className="w-3 h-3 animate-spin" /> Executando...
                    </p>
                  )}
                  {k8sOutput.map((line, i) => (
                    <div key={i} className={`text-[11px] font-mono leading-5 whitespace-pre-wrap break-all ${
                      line.kind === "err" ? "text-red-400" :
                      line.kind === "ok"  ? "text-green-400" :
                      line.kind === "warn" ? "text-yellow-400" :
                      "text-foreground/80"
                    }`}>{line.text || " "}</div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>

        <ResizeDivider onDrag={d => setSidebarWidth(w => Math.max(160, Math.min(520, w + d)))} />

        {/* ── Área do editor ── */}
        <div className="flex-1 flex flex-col min-h-0 min-w-0 relative">
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
                {/* Breadcrumb */}
                <div className="flex items-center flex-1 min-w-0 overflow-hidden text-xs font-mono text-muted-foreground">
                  {activeTab.node.path.split("/").map((seg, i, arr) => {
                    const isLast = i === arr.length - 1;
                    return (
                      <span key={i} className="flex items-center flex-shrink-0">
                        {i > 0 && <span className="opacity-30 mx-0.5 select-none">/</span>}
                        {isLast ? (
                          <span className="text-foreground/80 truncate max-w-[200px]">{seg}</span>
                        ) : (
                          <button
                            className="hover:text-foreground hover:underline underline-offset-2 truncate max-w-[120px]"
                            onClick={() => { setSidePanel("files"); setRevealPath(activeTab.node.path); }}
                            title={arr.slice(0, i + 1).join("/")}
                          >{seg}</button>
                        )}
                      </span>
                    );
                  })}
                </div>
                {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400 flex-shrink-0" title="Não salvo" />}
                <div className="flex gap-1 flex-shrink-0">
                  <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Copiar caminho"
                    onClick={() => navigator.clipboard.writeText(activeTab.node.path).then(() => addToast("success", "Caminho copiado"))}>
                    <Copy className="w-3 h-3" />
                  </Button>
                  <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title="Revelar na tree"
                    onClick={() => { setSidePanel("files"); setRevealPath(activeTab.node.path); }}>
                    <Locate className="w-3 h-3" />
                  </Button>
                  {activeTab.node.name.endsWith(".md") && (
                    <Button variant={showMarkdownPreview ? "default" : "ghost"} size="sm" className="h-5 w-5 p-0"
                      title={showMarkdownPreview ? "Ocultar preview Markdown" : "Preview Markdown"}
                      onClick={() => setShowMarkdownPreview(v => !v)}>
                      <BookOpen className="w-3 h-3" />
                    </Button>
                  )}
                  <Button variant={showBlame ? "default" : "ghost"} size="sm" className="h-5 w-5 p-0" title="Git Blame inline"
                    onClick={loadBlame}>
                    <User className="w-3 h-3" />
                  </Button>
                  <Button variant="ghost" size="sm" className="h-5 w-5 p-0" title={showMinimap ? "Ocultar minimap" : "Mostrar minimap"}
                    onClick={() => setShowMinimap(m => !m)}>
                    <MapIcon className="w-3 h-3" />
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
              <div ref={editorRowRef} className="flex-1 min-h-0 flex flex-row">
                <div className="flex-1 min-h-0 min-w-0">
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
                      fontSize: fontSize,
                      lineHeight: Math.round(fontSize * 1.55),
                      fontFamily: "'Cascadia Code','Fira Code','Consolas','Courier New',monospace",
                      fontLigatures: true,
                      wordWrap: wordWrap ? "on" : "off",
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
                {showMarkdownPreview && activeTab.node.name.endsWith(".md") && (
                  <>
                    <ResizeDivider onDrag={d => setMarkdownPreviewWidth(w => {
                      const maxW = Math.floor((editorRowRef.current?.offsetWidth ?? 1400) * 0.70);
                      const next = Math.max(200, Math.min(maxW, w - d));
                      localStorage.setItem("ce_md_preview_width", String(next));
                      return next;
                    })} />
                    <div className="flex-shrink-0 overflow-y-auto bg-slate-950 p-5" style={{ width: markdownPreviewWidth }}>
                      <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
                        {activeTab.currentContent}
                      </ReactMarkdown>
                    </div>
                  </>
                )}
              </div>
              {/* ── Status Bar ── */}
              <div className="flex items-center gap-3 px-3 h-5 bg-[#007acc] text-white text-[10px] flex-shrink-0 select-none">
                <span className="font-mono">Ln {cursorLine}, Col {cursorCol}</span>
                <span className="opacity-40">|</span>
                <span>{extToLanguage(activeTab.node.name)}</span>
                <span className="opacity-40">|</span>
                <span>UTF-8</span>
                <div className="flex-1" />
                {/* Font size */}
                <button onClick={() => { const n = Math.max(10, fontSize - 1); setFontSize(n); localStorage.setItem("ce_font_size", String(n)); }}
                  className="px-1 rounded hover:bg-white/20 leading-none" title="Diminuir fonte">−</button>
                <span className="font-mono w-7 text-center">{fontSize}px</span>
                <button onClick={() => { const n = Math.min(24, fontSize + 1); setFontSize(n); localStorage.setItem("ce_font_size", String(n)); }}
                  className="px-1 rounded hover:bg-white/20 leading-none" title="Aumentar fonte">+</button>
                {/* Word wrap */}
                <button
                  onClick={() => { const n = !wordWrap; setWordWrap(n); localStorage.setItem("ce_word_wrap", n ? "on" : "off"); }}
                  className={`flex items-center gap-1 px-1.5 rounded hover:bg-white/20 transition-colors ${wordWrap ? "" : "opacity-50"}`}
                  title={wordWrap ? "Word wrap ativado — clique para desativar" : "Word wrap desativado — clique para ativar"}
                >
                  <WrapText className="w-3 h-3" />Wrap
                </button>
                <button
                  onClick={() => { const n = !autoSave; setAutoSave(n); localStorage.setItem("ce_autosave", String(n)); }}
                  className={`flex items-center gap-1 px-1.5 rounded hover:bg-white/20 transition-colors ${autoSave ? "" : "opacity-50"}`}
                  title={autoSave ? "Auto-save ativado — clique para desativar" : "Auto-save desativado — clique para ativar"}
                >
                  <span className={`w-1.5 h-1.5 rounded-full ${autoSave ? "bg-green-300" : "bg-white/30"}`} />
                  Auto-save
                </button>
                <button
                  onClick={() => { const n = !formatOnSave; setFormatOnSave(n); localStorage.setItem("ce_format_on_save", String(n)); }}
                  className={`flex items-center gap-1 px-1.5 rounded hover:bg-white/20 transition-colors ${formatOnSave ? "" : "opacity-50"}`}
                  title={formatOnSave ? "Formatar ao salvar ativado — clique para desativar" : "Formatar ao salvar desativado — clique para ativar"}
                >
                  <span className={`w-1.5 h-1.5 rounded-full ${formatOnSave ? "bg-green-300" : "bg-white/30"}`} />
                  Fmt ao salvar
                </button>
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
          {/* ── Terminal integrado (multi-abas) ── */}
          {showTerminal && selectedRepo && (
            <>
              <HResizeDivider onDrag={delta => {
                setTerminalHeight(h => {
                  const next = Math.max(80, Math.min(600, h - delta));
                  localStorage.setItem("ce_terminal_height", String(next));
                  return next;
                });
              }} />
              <div className="flex flex-col flex-shrink-0" style={{ height: terminalHeight }}>
                {/* Barra de abas */}
                <div className="flex items-center bg-[#2d2d2d] border-b border-white/10 flex-shrink-0 overflow-x-auto">
                  {terminalTabs.map(tab => (
                    <div
                      key={tab.id}
                      className={`flex items-center gap-1.5 px-3 py-1.5 text-xs cursor-pointer border-r border-white/10 flex-shrink-0 select-none transition-colors ${
                        activeTerminalId === tab.id
                          ? "bg-[#1e1e1e] text-white"
                          : "text-white/40 hover:text-white/70 hover:bg-[#252525]"
                      }`}
                      onClick={() => setActiveTerminalId(tab.id)}
                    >
                      <Terminal className="w-3 h-3 flex-shrink-0" />
                      <span>{tab.label}</span>
                      <button
                        className="ml-0.5 opacity-40 hover:opacity-100 transition-opacity"
                        onClick={e => { e.stopPropagation(); closeTerminalTab(tab.id); }}
                        title="Fechar terminal"
                      >
                        <X className="w-2.5 h-2.5" />
                      </button>
                    </div>
                  ))}
                  <button
                    onClick={addTerminalTab}
                    className="px-2 py-1.5 text-white/40 hover:text-white/70 flex-shrink-0 transition-colors"
                    title="Novo terminal"
                  >
                    <Plus className="w-3 h-3" />
                  </button>
                </div>
                {/* Instâncias de terminal */}
                {terminalTabs.map(tab => (
                  <div
                    key={tab.id}
                    style={{ display: activeTerminalId === tab.id ? "flex" : "none", flex: 1, flexDirection: "column", minHeight: 0 }}
                  >
                    <RepoTerminal
                      repoId={selectedRepo.id}
                      repoName={`${selectedRepo.owner}/${selectedRepo.repo}`}
                      height={terminalHeight - 28}
                      font={terminalFont}
                      visible={activeTerminalId === tab.id}
                      onFontChange={f => {
                        setTerminalFont(f);
                        localStorage.setItem("ce_terminal_font", f);
                      }}
                      onClose={() => closeTerminalTab(tab.id)}
                    />
                  </div>
                ))}
              </div>
            </>
          )}

          {/* ── Context menu da tree ── */}
          {contextMenu && (
            <div
              className="fixed z-[200] bg-popover border border-border rounded-md shadow-lg py-1 min-w-[168px]"
              style={{ top: contextMenu.y, left: contextMenu.x }}
              onMouseDown={e => e.stopPropagation()}
            >
              {contextMenu.node.type === "file" ? (
                <>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { openFile(contextMenu.node); setContextMenu(null); }}>
                    <File className="w-3 h-3" />Abrir
                  </button>
                  <div className="border-t border-border/40 my-1" />
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setRenameNode(contextMenu.node); setContextMenu(null); }}>
                    <Pencil className="w-3 h-3" />Renomear
                  </button>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2 text-red-400"
                    onClick={() => { handleDeleteFile(contextMenu.node); setContextMenu(null); }}>
                    <Trash2 className="w-3 h-3" />Deletar
                  </button>
                  <div className="border-t border-border/40 my-1" />
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { navigator.clipboard.writeText(contextMenu.node.path).then(() => addToast("success", "Caminho copiado")); setContextMenu(null); }}>
                    <Copy className="w-3 h-3" />Copiar caminho
                  </button>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setSidePanel("files"); setRevealPath(contextMenu.node.path); setContextMenu(null); }}>
                    <Locate className="w-3 h-3" />Revelar na tree
                  </button>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setFileHistoryNode(contextMenu.node); setContextMenu(null); }}>
                    <History className="w-3 h-3" />Histórico
                  </button>
                </>
              ) : (
                <>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setFocusedDirPath(contextMenu.node.path); setCreateDialog({ mode: "file", basePath: contextMenu.node.path }); setContextMenu(null); }}>
                    <FilePlus className="w-3 h-3" />Novo arquivo aqui
                  </button>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setFocusedDirPath(contextMenu.node.path); setCreateDialog({ mode: "dir", basePath: contextMenu.node.path }); setContextMenu(null); }}>
                    <FolderPlus className="w-3 h-3" />Nova pasta aqui
                  </button>
                  <div className="border-t border-border/40 my-1" />
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { setRenameNode(contextMenu.node); setContextMenu(null); }}>
                    <Pencil className="w-3 h-3" />Renomear
                  </button>
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2 text-red-400"
                    onClick={() => { handleDeleteFile(contextMenu.node); setContextMenu(null); }}>
                    <Trash2 className="w-3 h-3" />Deletar
                  </button>
                  <div className="border-t border-border/40 my-1" />
                  <button className="w-full text-left px-3 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground flex items-center gap-2"
                    onClick={() => { navigator.clipboard.writeText(contextMenu.node.path).then(() => addToast("success", "Caminho copiado")); setContextMenu(null); }}>
                    <Copy className="w-3 h-3" />Copiar caminho
                  </button>
                </>
              )}
            </div>
          )}

          {/* ── Quick Open (Ctrl+P) ── */}
          {showQuickOpen && selectedRepo && (
            <div
              className="absolute inset-0 z-50 flex flex-col items-center pt-14"
              style={{ background: "rgba(0,0,0,0.55)" }}
              onClick={e => { if (e.target === e.currentTarget) setShowQuickOpen(false); }}
            >
              <div className="w-[560px] max-w-[90%] bg-[#1e1e1e] border border-[#454545] rounded-md shadow-2xl overflow-hidden">
                <div className="relative border-b border-[#454545]">
                  <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-[#aaa]" />
                  <input
                    autoFocus
                    className="w-full bg-transparent text-[13px] text-white py-2 pl-9 pr-3 outline-none placeholder:text-[#666]"
                    placeholder="Abrir arquivo... (Esc para fechar)"
                    value={quickOpenQuery}
                    onChange={e => { setQuickOpenQuery(e.target.value); setQuickOpenIdx(0); }}
                    onKeyDown={e => {
                      if (e.key === "Escape") setShowQuickOpen(false);
                      else if (e.key === "ArrowDown") { e.preventDefault(); setQuickOpenIdx(i => Math.min(i + 1, quickOpenFiles.length - 1)); }
                      else if (e.key === "ArrowUp") { e.preventDefault(); setQuickOpenIdx(i => Math.max(0, i - 1)); }
                      else if (e.key === "Enter") {
                        const f = quickOpenFiles[quickOpenIdx];
                        if (f) { openFile(f); setShowQuickOpen(false); }
                      }
                    }}
                  />
                </div>
                <div className="max-h-[320px] overflow-y-auto">
                  {quickOpenFiles.slice(0, 50).map((f, i) => (
                    <div
                      key={f.path}
                      className={`flex items-center gap-2 px-3 py-1.5 cursor-pointer ${
                        i === quickOpenIdx ? "bg-[#094771] text-white" : "text-[#ccc] hover:bg-[#2a2d2e]"
                      }`}
                      onClick={() => { openFile(f); setShowQuickOpen(false); }}
                      onMouseEnter={() => setQuickOpenIdx(i)}
                    >
                      <File className="w-3.5 h-3.5 flex-shrink-0 opacity-60" />
                      <span className="text-[12px] truncate">{f.name}</span>
                      <span className="ml-auto text-[10px] text-[#888] truncate max-w-[200px]">
                        {f.path.includes("/") ? f.path.substring(0, f.path.lastIndexOf("/")) : ""}
                      </span>
                    </div>
                  ))}
                  {quickOpenFiles.length === 0 && (
                    <p className="text-center text-[#888] text-[12px] py-4">Nenhum arquivo encontrado</p>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* ── CreateTag dialog ── */}
      {showCreateTag !== null && (
        <CreateTagDialog
          defaultHash={showCreateTag.hash}
          onClose={() => setShowCreateTag(null)}
          onCreate={handleCreateTag}
        />
      )}

      {/* ── Confirm dialog ── */}
      {confirmState && (
        <Dialog open onOpenChange={v => { if (!v) confirmState.onCancel(); }}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Confirmar</DialogTitle>
            </DialogHeader>
            <p className="text-sm text-muted-foreground whitespace-pre-line">{confirmState.message}</p>
            <DialogFooter>
              <Button variant="ghost" size="sm" onClick={confirmState.onCancel}>Cancelar</Button>
              <Button variant="destructive" size="sm" onClick={confirmState.onConfirm}>Confirmar</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {/* ── Dialogs ── */}
      <GitHubTokenDialog open={showGitHubToken} onClose={() => setShowGitHubToken(false)} />

      <CloneDialog
        open={showClone}
        onClose={() => setShowClone(false)}
        onDone={async (id) => {
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
            onPush={() => setSseDialog({ title: "Git Push", endpoint: `/code-editor/repos/${selectedRepo.id}/push`, body: activeToken() ? { token: activeToken() } : undefined })}
            onRefresh={() => loadStatus(selectedRepo.id)}
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
            onConflict={(files) => {
              setConflictFiles(files);
              setShowConflictResolver(true);
            }}
          />

          <CreateFileDialog
            open={!!createDialog}
            mode={createDialog?.mode ?? "file"}
            repoId={selectedRepo.id}
            basePath={createDialog?.basePath}
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

      {/* ── Fase 4: Histórico de arquivo ── */}
      {fileHistoryNode && selectedRepo && (
        <FileHistoryModal
          repoId={selectedRepo.id}
          node={fileHistoryNode}
          onClose={() => setFileHistoryNode(null)}
        />
      )}

      {/* ── Fase 5: Resolver conflitos ── */}
      {showConflictResolver && selectedRepo && (
        <ConflictResolverModal
          repoId={selectedRepo.id}
          files={conflictFiles}
          onClose={() => setShowConflictResolver(false)}
          onDone={async () => {
            setShowConflictResolver(false);
            setOpenTabs([]);
            setActiveTabIdx(0);
            await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id), loadTree(selectedRepo.id)]);
            addToast("success", "Merge concluído após resolução de conflitos");
          }}
          onAbort={async () => {
            setShowConflictResolver(false);
            await Promise.all([loadStatus(selectedRepo.id), loadTree(selectedRepo.id)]);
            addToast("success", "Merge abortado");
          }}
        />
      )}

      {/* ── Fase 5: Diff entre branches ── */}
      {showBranchDiff && selectedRepo && (
        <BranchDiffModal
          repoId={selectedRepo.id}
          branches={branches}
          onClose={() => setShowBranchDiff(false)}
        />
      )}

      {/* ── Criar Pull Request ── */}
      {showCreatePR && selectedRepo && branches?.current && (
        <CreatePRModal
          open={showCreatePR}
          onClose={() => setShowCreatePR(false)}
          repoId={selectedRepo.id}
          head={branches.current}
          branches={[
            ...(branches.local ?? []),
            ...(branches.remote ?? []).map((r: string) => r.replace(/^origin\//, "")),
          ].filter((b, i, a) => b !== branches.current && a.indexOf(b) === i)}
        />
      )}

      <ToastContainer toasts={toasts} />
    </div>
  );
}

// ─── FileHistoryModal ────────────────────────────────────────────────────────

interface FileHistoryModalProps {
  repoId: string;
  node: CodeEditorFileNode;
  onClose: () => void;
}

function FileHistoryModal({ repoId, node, onClose }: FileHistoryModalProps) {
  const [entries, setEntries] = useState<CodeEditorFileLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [diffCommit, setDiffCommit] = useState<CodeEditorFileLogEntry | null>(null);

  useEffect(() => {
    apiClient.codeEditorGetFileLog(repoId, node.path)
      .then(r => setEntries(r.entries ?? []))
      .catch(() => setEntries([]))
      .finally(() => setLoading(false));
  }, [repoId, node.path]);

  return (
    <>
      <Dialog open onOpenChange={onClose}>
        <DialogContent className="max-w-lg max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm">
              <History className="w-4 h-4" />
              Histórico: {node.name}
            </DialogTitle>
          </DialogHeader>
          <div className="flex-1 min-h-0 overflow-y-auto">
            {loading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
              </div>
            ) : entries.length === 0 ? (
              <p className="text-xs text-muted-foreground text-center py-8">Sem histórico</p>
            ) : (
              <div className="space-y-1 p-2">
                {entries.map(e => (
                  <div key={e.hash} className="group flex items-start gap-2 border-b border-border/20 pb-2">
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-foreground/90 leading-tight truncate">{e.message}</p>
                      <div className="flex items-center gap-1 mt-0.5">
                        <span className="font-mono text-[10px] text-muted-foreground/60">{e.hash.slice(0, 7)}</span>
                        <span className="text-[10px] text-muted-foreground">· {e.author} · {e.date}</span>
                      </div>
                    </div>
                    <button
                      className="flex-shrink-0 text-[10px] text-primary hover:underline opacity-0 group-hover:opacity-100 flex items-center gap-0.5"
                      onClick={() => setDiffCommit(e)}
                      title="Ver diff neste commit"
                    >
                      <Eye className="w-2.5 h-2.5" />diff
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={onClose}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {diffCommit && (
        <CommitDiffModal
          repoId={repoId}
          filePath={node.path}
          commit={diffCommit}
          onClose={() => setDiffCommit(null)}
        />
      )}
    </>
  );
}

// ─── CommitDiffModal ─────────────────────────────────────────────────────────

interface CommitDiffModalProps {
  repoId: string;
  filePath: string;
  commit: CodeEditorFileLogEntry;
  onClose: () => void;
}

function CommitDiffModal({ repoId, filePath, commit, onClose }: CommitDiffModalProps) {
  const [original, setOriginal] = useState("");
  const [modified, setModified] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        const [cur, prev] = await Promise.all([
          apiClient.codeEditorGetFileAtCommit(repoId, commit.hash, filePath),
          apiClient.codeEditorGetFileAtCommit(repoId, commit.hash + "^", filePath).catch(() => ({ content: "" })),
        ]);
        setModified(cur.content);
        setOriginal(prev.content);
      } catch (_) {
        setOriginal("");
        setModified("");
      } finally {
        setLoading(false);
      }
    }
    load();
  }, [repoId, commit.hash, filePath]);

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-5xl h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-sm font-mono flex items-center gap-2">
            <GitCommit className="w-4 h-4 flex-shrink-0" />
            {commit.hash.slice(0, 7)} · {commit.message}
          </DialogTitle>
        </DialogHeader>
        <div className="flex-1 min-h-0">
          {loading ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <DiffEditor
              height="100%"
              original={original}
              modified={modified}
              theme="vs-dark"
              options={{ readOnly: true, automaticLayout: true, minimap: { enabled: false } }}
            />
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>Fechar</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── RepoTerminal ────────────────────────────────────────────────────────────

interface RepoTerminalProps {
  repoId: string;
  repoName: string;
  height: number;
  font: string;
  visible?: boolean;
  onFontChange: (font: string) => void;
  onClose: () => void;
}

function RepoTerminal({ repoId, repoName, height, font, visible, onFontChange, onClose }: RepoTerminalProps) {
  const divRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [fonts, setFonts] = useState<string[]>([]);
  const [showFontPicker, setShowFontPicker] = useState(false);

  useEffect(() => {
    if (!divRef.current) return;
    const fontFamily = font
      ? `'${font}','Cascadia Code','Fira Code','Consolas',monospace`
      : "'Cascadia Code','Fira Code','Consolas',monospace";
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily,
      convertEol: true,
      scrollback: 5000,
      theme: { background: "#1e1e1e", foreground: "#d4d4d4", cursor: "#ffffff" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(divRef.current);
    fit.fit();
    xtermRef.current = term;
    fitRef.current = fit;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const token = localStorage.getItem("auth_token") ?? "";
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/api/v1/code-editor/repos/${repoId}/terminal?token=${encodeURIComponent(token)}`
    );
    wsRef.current = ws;
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    };
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === "output") {
          // Uint8Array → xterm.js decodifica UTF-8 corretamente (multibyte zsh/emoji)
          // atob() retorna binary string onde cada char é um byte — passar como string
          // quebra sequências multibyte porque xterm trata chars como codepoints Unicode
          const binary = atob(msg.data);
          const bytes = new Uint8Array(binary.length);
          for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
          term.write(bytes);
        }
      } catch { term.write(e.data); }
    };
    ws.onclose = () => term.writeln("\r\n\x1b[2m[sessão encerrada]\x1b[0m");

    term.onData(data => {
      if (ws.readyState === WebSocket.OPEN) {
        // Encode UTF-8 → base64 sem quebrar chars multibyte (btoa() falha com chars > 255)
        const encoded = btoa(unescape(encodeURIComponent(data)));
        ws.send(JSON.stringify({ type: "input", data: encoded }));
      }
    });

    const handleResize = () => {
      fit.fit();
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    };
    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      ws.close();
      term.dispose();
    };
  }, [repoId]);

  // Refit quando o painel é redimensionado verticalmente
  useEffect(() => {
    if (!fitRef.current || !xtermRef.current) return;
    // rAF para deixar o DOM atualizar o height antes do fit
    const id = requestAnimationFrame(() => {
      fitRef.current?.fit();
      const ws = wsRef.current;
      const term = xtermRef.current;
      if (ws?.readyState === WebSocket.OPEN && term) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    });
    return () => cancelAnimationFrame(id);
  }, [height]);

  // Refit ao tornar-se visível (troca de aba)
  useEffect(() => {
    if (!visible) return;
    const id = requestAnimationFrame(() => {
      fitRef.current?.fit();
      const ws = wsRef.current;
      const term = xtermRef.current;
      if (ws?.readyState === WebSocket.OPEN && term) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    });
    return () => cancelAnimationFrame(id);
  }, [visible]);

  // Atualiza fonte do xterm dinamicamente sem recriar o terminal
  useEffect(() => {
    const term = xtermRef.current;
    const fit = fitRef.current;
    if (!term) return;
    const fontFamily = font
      ? `'${font}','Cascadia Code','Fira Code','Consolas',monospace`
      : "'Cascadia Code','Fira Code','Consolas',monospace";
    term.options.fontFamily = fontFamily;
    requestAnimationFrame(() => {
      fit?.fit();
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    });
  }, [font]);

  // Carrega lista de fontes ao abrir o picker
  function handleOpenFontPicker() {
    setShowFontPicker(v => {
      if (!v && fonts.length === 0) {
        apiClient.codeEditorListFonts().then(r => setFonts(r.fonts)).catch(() => {});
      }
      return !v;
    });
  }

  return (
    <div className="flex flex-col bg-[#1e1e1e]" style={{ height }}>
      <div className="flex items-center justify-between px-3 py-1 border-b border-white/10 flex-shrink-0 gap-2">
        <span className="text-xs text-white/60 font-mono flex items-center gap-1 min-w-0 truncate">
          <Terminal className="w-3 h-3 flex-shrink-0" />{repoName}
        </span>
        <div className="flex items-center gap-1 flex-shrink-0">
          {/* Seletor de fonte */}
          <div className="relative">
            <button
              onClick={handleOpenFontPicker}
              title="Fonte do terminal"
              className="text-white/40 hover:text-white/80 flex items-center gap-1 text-xs px-1.5 py-0.5 rounded hover:bg-white/10 transition-colors"
            >
              <Type className="w-3 h-3" />
              {font ? <span className="max-w-[120px] truncate">{font}</span> : <span className="text-white/30">fonte</span>}
            </button>
            {showFontPicker && (
              <div className="absolute bottom-full right-0 mb-1 w-56 bg-[#252526] border border-white/10 rounded shadow-xl z-50 overflow-hidden">
                <div className="px-2 pt-2 pb-1 border-b border-white/10">
                  <p className="text-[10px] text-white/40 uppercase tracking-wide">Fonte do terminal</p>
                </div>
                <div className="max-h-52 overflow-y-auto">
                  <button
                    className={`w-full text-left px-3 py-1.5 text-xs hover:bg-white/10 transition-colors ${font === "" ? "text-primary" : "text-white/60"}`}
                    onClick={() => { onFontChange(""); setShowFontPicker(false); }}
                  >
                    Padrão (Cascadia Code / Fira Code)
                  </button>
                  {fonts.length === 0 && (
                    <p className="px-3 py-2 text-xs text-white/30 italic">carregando…</p>
                  )}
                  {fonts.map(f => (
                    <button
                      key={f}
                      className={`w-full text-left px-3 py-1.5 text-xs hover:bg-white/10 transition-colors truncate ${font === f ? "text-primary" : "text-white/70"}`}
                      style={{ fontFamily: `'${f}', monospace` }}
                      onClick={() => { onFontChange(f); setShowFontPicker(false); }}
                    >
                      {f}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
          <button onClick={onClose} className="text-white/40 hover:text-white/80">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
      <div ref={divRef} className="flex-1 min-h-0 px-1" />
    </div>
  );
}

// ─── CreateTagDialog ─────────────────────────────────────────────────────────

interface CreateTagDialogProps {
  defaultHash: string;
  onClose: () => void;
  onCreate: (name: string, hash: string, message?: string) => Promise<void>;
}

function CreateTagDialog({ defaultHash, onClose, onCreate }: CreateTagDialogProps) {
  const [name, setName] = useState("");
  const [hash, setHash] = useState(defaultHash);
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);

  async function submit() {
    if (!name.trim()) return;
    setLoading(true);
    try {
      await onCreate(name.trim(), hash.trim(), message.trim() || undefined);
      onClose();
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-sm">
        <DialogHeader><DialogTitle>Criar Tag</DialogTitle></DialogHeader>
        <div className="space-y-3 py-2">
          <div>
            <label className="text-xs text-muted-foreground">Nome da tag *</label>
            <Input className="h-7 text-xs mt-1" placeholder="v1.0.0" value={name}
              onChange={e => setName(e.target.value)} autoFocus />
          </div>
          <div>
            <label className="text-xs text-muted-foreground">Commit (hash ou vazio para HEAD)</label>
            <Input className="h-7 text-xs font-mono mt-1" placeholder="HEAD" value={hash}
              onChange={e => setHash(e.target.value)} />
          </div>
          <div>
            <label className="text-xs text-muted-foreground">Mensagem (opcional — cria tag anotada)</label>
            <Input className="h-7 text-xs mt-1" placeholder="Release v1.0.0" value={message}
              onChange={e => setMessage(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>Cancelar</Button>
          <Button size="sm" onClick={submit} disabled={loading || !name.trim()}>
            {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <Tag className="w-3 h-3 mr-1" />}
            Criar tag
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── ConflictResolverModal (Fase 5) ──────────────────────────────────────────

interface ConflictBlock {
  index: number;
  before: string;   // texto antes do bloco conflito
  ours: string;
  theirs: string;
  label: string;    // nome do branch vindo
  after: string;    // texto depois (só para reconstrução)
  choice: "ours" | "theirs" | "manual" | null;
  manual: string;
}

function parseConflicts(content: string): { blocks: ConflictBlock[]; hasConflict: boolean } {
  const lines = content.split("\n");
  const blocks: ConflictBlock[] = [];
  let idx = 0;
  let i = 0;
  while (i < lines.length) {
    const startIdx = lines.findIndex((l, j) => j >= i && l.startsWith("<<<<<<<"));
    if (startIdx === -1) break;
    const sepIdx = lines.findIndex((l, j) => j > startIdx && l.startsWith("======="));
    const endIdx = lines.findIndex((l, j) => j > (sepIdx >= 0 ? sepIdx : startIdx) && l.startsWith(">>>>>>>"));
    if (sepIdx === -1 || endIdx === -1) break;
    const label = lines[endIdx].replace(/^>>>>>>>/, "").trim();
    blocks.push({
      index: idx++,
      before: lines.slice(0, startIdx).join("\n"),
      ours: lines.slice(startIdx + 1, sepIdx).join("\n"),
      theirs: lines.slice(sepIdx + 1, endIdx).join("\n"),
      label,
      after: lines.slice(endIdx + 1).join("\n"),
      choice: null,
      manual: "",
    });
    i = endIdx + 1;
  }
  return { blocks, hasConflict: blocks.length > 0 };
}

function rebuildContent(original: string, blocks: ConflictBlock[]): string {
  if (blocks.length === 0) return original;
  // Reconstrói o arquivo substituindo cada bloco de conflito pela escolha
  let content = original;
  // Substitui de trás para frente para manter índices de linha
  const lines = content.split("\n");
  const positions: { start: number; end: number; replacement: string }[] = [];
  let scanIdx = 0;
  for (const b of blocks) {
    const startLine = lines.findIndex((l, j) => j >= scanIdx && l.startsWith("<<<<<<<"));
    if (startLine === -1) continue;
    const sepLine = lines.findIndex((l, j) => j > startLine && l.startsWith("======="));
    const endLine = lines.findIndex((l, j) => j > sepLine && l.startsWith(">>>>>>>"));
    if (sepLine === -1 || endLine === -1) continue;
    let replacement: string;
    if (b.choice === "ours") replacement = b.ours;
    else if (b.choice === "theirs") replacement = b.theirs;
    else replacement = b.manual || b.ours;
    positions.push({ start: startLine, end: endLine, replacement });
    scanIdx = endLine + 1;
  }
  // Aplica de trás para frente
  for (let i = positions.length - 1; i >= 0; i--) {
    const { start, end, replacement } = positions[i];
    lines.splice(start, end - start + 1, ...replacement.split("\n"));
  }
  return lines.join("\n");
}

interface ConflictResolverModalProps {
  repoId: string;
  files: string[];
  onClose: () => void;
  onDone: () => void;
  onAbort: () => void;
}

function ConflictResolverModal({ repoId, files, onClose, onDone, onAbort }: ConflictResolverModalProps) {
  const [selectedFile, setSelectedFile] = useState(files[0] ?? "");
  const [fileContent, setFileContent] = useState("");
  const [blocks, setBlocks] = useState<ConflictBlock[]>([]);
  const [loading, setLoading] = useState(false);
  const [resolvedFiles, setResolvedFiles] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [aborting, setAborting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!selectedFile) return;
    setLoading(true);
    apiClient.codeEditorReadFile(repoId, selectedFile)
      .then(r => {
        setFileContent(r.content);
        const { blocks: parsed } = parseConflicts(r.content);
        setBlocks(parsed);
      })
      .catch(() => setBlocks([]))
      .finally(() => setLoading(false));
  }, [repoId, selectedFile]);

  function setChoice(index: number, choice: "ours" | "theirs") {
    setBlocks(prev => prev.map(b => b.index === index ? { ...b, choice, manual: choice === "ours" ? b.ours : b.theirs } : b));
  }

  function setManual(index: number, text: string) {
    setBlocks(prev => prev.map(b => b.index === index ? { ...b, choice: "manual", manual: text } : b));
  }

  const allResolved = blocks.length > 0 && blocks.every(b => b.choice !== null);

  async function saveFile() {
    setSaving(true); setError("");
    try {
      const resolved = rebuildContent(fileContent, blocks);
      await apiClient.codeEditorResolveConflict(repoId, selectedFile, resolved);
      setResolvedFiles(prev => new Set([...prev, selectedFile]));
      // Avança para o próximo arquivo com conflito
      const next = files.find(f => f !== selectedFile && !resolvedFiles.has(f));
      if (next) setSelectedFile(next);
    } catch (e: any) {
      setError(e.message || "Erro ao salvar");
    } finally {
      setSaving(false);
    }
  }

  async function commitMerge() {
    setCommitting(true); setError("");
    try {
      await apiClient.codeEditorCommitMerge(repoId);
      onDone();
    } catch (e: any) {
      setError(e.message || "Erro ao fazer commit");
    } finally {
      setCommitting(false);
    }
  }

  async function abortMerge() {
    setAborting(true);
    try {
      await apiClient.codeEditorAbortMerge(repoId);
      onAbort();
    } catch (e: any) {
      setError(e.message || "Erro ao abortar");
    } finally {
      setAborting(false);
    }
  }

  const allFilesResolved = files.every(f => resolvedFiles.has(f));

  return (
    <div className="fixed inset-0 z-50 bg-background flex flex-col">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-2 border-b border-border/50 flex-shrink-0 bg-card/20">
        <ShieldAlert className="w-4 h-4 text-yellow-400 flex-shrink-0" />
        <span className="text-sm font-medium">Resolver Conflitos de Merge</span>
        <span className="text-xs text-muted-foreground">({resolvedFiles.size}/{files.length} arquivos resolvidos)</span>
        <div className="flex-1" />
        {error && <span className="text-xs text-red-400">{error}</span>}
        <Button variant="ghost" size="sm" className="h-6 text-xs text-red-400 hover:text-red-300"
          onClick={abortMerge} disabled={aborting || committing}>
          {aborting ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : null}
          Abortar Merge
        </Button>
        {allFilesResolved && (
          <Button size="sm" className="h-6 text-xs bg-green-700 hover:bg-green-600"
            onClick={commitMerge} disabled={committing}>
            {committing ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitCommit className="w-3 h-3 mr-1" />}
            Commit do Merge
          </Button>
        )}
        <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={onClose}>
          <X className="w-3.5 h-3.5" />
        </Button>
      </div>

      <div className="flex flex-1 min-h-0">
        {/* Lista de arquivos */}
        <div className="w-56 flex-shrink-0 border-r border-border/50 flex flex-col">
          <p className="text-[10px] text-muted-foreground uppercase tracking-wide px-3 py-2 border-b border-border/30">Arquivos com conflito</p>
          <ScrollArea className="flex-1">
            {files.map(f => {
              const done = resolvedFiles.has(f);
              return (
                <button key={f} onClick={() => setSelectedFile(f)}
                  className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 hover:bg-muted/50 transition-colors ${selectedFile === f ? "bg-muted/80 text-foreground" : "text-muted-foreground"}`}>
                  {done
                    ? <CheckCircle2 className="w-3 h-3 text-green-400 flex-shrink-0" />
                    : <AlertCircle className="w-3 h-3 text-yellow-400 flex-shrink-0" />}
                  <span className="truncate font-mono">{f}</span>
                </button>
              );
            })}
          </ScrollArea>
        </div>

        {/* Editor de conflitos */}
        <div className="flex-1 flex flex-col min-h-0">
          {loading ? (
            <div className="flex-1 flex items-center justify-center">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : blocks.length === 0 ? (
            <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
              Nenhum marcador de conflito encontrado neste arquivo.
            </div>
          ) : (
            <>
              <ScrollArea className="flex-1">
                <div className="p-4 space-y-4">
                  {blocks.map(b => (
                    <div key={b.index} className="border border-border/50 rounded overflow-hidden">
                      <div className="flex items-center justify-between bg-muted/30 px-3 py-1.5 border-b border-border/30">
                        <span className="text-xs font-medium">Conflito #{b.index + 1}</span>
                        <div className="flex gap-1">
                          <Button size="sm" variant={b.choice === "ours" ? "default" : "outline"}
                            className="h-5 text-[10px] px-2"
                            onClick={() => setChoice(b.index, "ours")}>
                            Aceitar Atual (HEAD)
                          </Button>
                          <Button size="sm" variant={b.choice === "theirs" ? "default" : "outline"}
                            className="h-5 text-[10px] px-2"
                            onClick={() => setChoice(b.index, "theirs")}>
                            Aceitar Vindo ({b.label || "branch"})
                          </Button>
                        </div>
                      </div>
                      {/* Ours */}
                      <div className="bg-blue-950/20 border-b border-border/20 p-3">
                        <p className="text-[10px] text-blue-400 font-medium mb-1">HEAD (atual)</p>
                        <pre className="font-mono text-xs text-blue-200/80 whitespace-pre-wrap">{b.ours || <em className="text-muted-foreground">vazio</em>}</pre>
                      </div>
                      {/* Theirs */}
                      <div className="bg-orange-950/20 p-3">
                        <p className="text-[10px] text-orange-400 font-medium mb-1">{b.label || "Vindo"}</p>
                        <pre className="font-mono text-xs text-orange-200/80 whitespace-pre-wrap">{b.theirs || <em className="text-muted-foreground">vazio</em>}</pre>
                      </div>
                      {/* Edição manual */}
                      {b.choice !== null && (
                        <div className="bg-muted/10 border-t border-border/20 p-3">
                          <p className="text-[10px] text-muted-foreground mb-1">Resultado (editável)</p>
                          <textarea
                            className="w-full font-mono text-xs bg-background border border-border/40 rounded p-2 resize-none focus:outline-none focus:ring-1 focus:ring-primary/50"
                            rows={Math.max(3, (b.manual || "").split("\n").length + 1)}
                            value={b.manual}
                            onChange={e => setManual(b.index, e.target.value)}
                          />
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </ScrollArea>
              <div className="flex items-center justify-between px-4 py-2 border-t border-border/30 flex-shrink-0">
                <span className="text-xs text-muted-foreground">
                  {blocks.filter(b => b.choice !== null).length}/{blocks.length} blocos resolvidos
                </span>
                <Button size="sm" className="h-6 text-xs" onClick={saveFile}
                  disabled={!allResolved || saving || resolvedFiles.has(selectedFile)}>
                  {saving ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <CheckCircle2 className="w-3 h-3 mr-1" />}
                  {resolvedFiles.has(selectedFile) ? "Salvo" : "Salvar e Marcar Resolvido"}
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── BranchDiffModal (Fase 5) ─────────────────────────────────────────────────

interface BranchDiffModalProps {
  repoId: string;
  branches: CodeEditorBranches | null;
  onClose: () => void;
}

function BranchDiffModal({ repoId, branches, onClose }: BranchDiffModalProps) {
  const allBranches = useMemo(() => {
    const local = branches?.local ?? [];
    const remote = (branches?.remote ?? []).filter(r => !r.includes("->"));
    return [...new Set([...(branches?.current ? [branches.current] : []), ...local, ...remote])];
  }, [branches]);

  const [from, setFrom] = useState(branches?.current ?? "");
  const [to, setTo] = useState("");
  const [diff, setDiff] = useState("");
  const [filesSummary, setFilesSummary] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function compare() {
    if (!from || !to) { setError("Selecione os dois branches"); return; }
    setLoading(true); setError(""); setDiff(""); setFilesSummary("");
    try {
      const r = await apiClient.codeEditorGetBranchDiff(repoId, from, to);
      setDiff(r.diff);
      setFilesSummary(r.files);
    } catch (e: any) {
      setError(e.message || "Erro ao comparar");
    } finally {
      setLoading(false);
    }
  }

  // Colore linhas do diff unificado
  function renderDiffLine(line: string, idx: number) {
    if (line.startsWith("+++") || line.startsWith("---")) {
      return <div key={idx} className="text-muted-foreground font-mono text-xs whitespace-pre">{line}</div>;
    }
    if (line.startsWith("@@")) {
      return <div key={idx} className="text-blue-400 font-mono text-xs whitespace-pre bg-blue-950/20 px-1">{line}</div>;
    }
    if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("new file") || line.startsWith("deleted file")) {
      return <div key={idx} className="text-primary font-mono text-xs whitespace-pre font-medium border-t border-border/20 mt-2 pt-2">{line}</div>;
    }
    if (line.startsWith("+")) {
      return <div key={idx} className="text-green-400 font-mono text-xs whitespace-pre bg-green-950/20">{line}</div>;
    }
    if (line.startsWith("-")) {
      return <div key={idx} className="text-red-400 font-mono text-xs whitespace-pre bg-red-950/20">{line}</div>;
    }
    return <div key={idx} className="text-foreground/70 font-mono text-xs whitespace-pre">{line}</div>;
  }

  return (
    <div className="fixed inset-0 z-50 bg-background flex flex-col">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-2 border-b border-border/50 flex-shrink-0 bg-card/20">
        <Columns2 className="w-4 h-4 text-primary flex-shrink-0" />
        <span className="text-sm font-medium">Comparar Branches</span>
        <div className="flex-1" />
        <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={onClose}>
          <X className="w-3.5 h-3.5" />
        </Button>
      </div>

      {/* Seletores */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 flex-shrink-0 bg-card/10">
        <select className="text-xs bg-muted border border-border/50 rounded px-2 py-1 text-foreground"
          value={from} onChange={e => setFrom(e.target.value)}>
          <option value="">De (base)...</option>
          {allBranches.map(b => <option key={b} value={b}>{b}</option>)}
        </select>
        <ArrowRightLeft className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
        <select className="text-xs bg-muted border border-border/50 rounded px-2 py-1 text-foreground"
          value={to} onChange={e => setTo(e.target.value)}>
          <option value="">Para (comparar)...</option>
          {allBranches.map(b => <option key={b} value={b}>{b}</option>)}
        </select>
        <Button size="sm" className="h-6 text-xs" onClick={compare} disabled={loading || !from || !to}>
          {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitCompare className="w-3 h-3 mr-1" />}
          Comparar
        </Button>
        {error && <span className="text-xs text-red-400">{error}</span>}
        {filesSummary && !loading && (
          <span className="text-xs text-muted-foreground ml-2">
            {filesSummary.split("\n").filter(Boolean).length} arquivo(s) alterado(s)
          </span>
        )}
      </div>

      {/* Diff */}
      <div className="flex flex-1 min-h-0">
        {/* Lista de arquivos */}
        {filesSummary && (
          <div className="w-56 flex-shrink-0 border-r border-border/50">
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide px-3 py-2 border-b border-border/30">Arquivos</p>
            <ScrollArea className="h-full">
              {filesSummary.split("\n").filter(Boolean).map((line, i) => {
                const [status, ...rest] = line.split("\t");
                const fname = rest.join("\t");
                const color = status === "A" ? "text-green-400" : status === "D" ? "text-red-400" : status === "M" ? "text-yellow-400" : "text-muted-foreground";
                return (
                  <div key={i} className="flex items-center gap-2 px-3 py-1.5 text-xs border-b border-border/10">
                    <span className={`font-bold flex-shrink-0 ${color}`}>{status}</span>
                    <span className="font-mono truncate text-foreground/70">{fname}</span>
                  </div>
                );
              })}
            </ScrollArea>
          </div>
        )}

        {/* Diff colorizado */}
        <ScrollArea className="flex-1 bg-[#1e1e1e]">
          {loading ? (
            <div className="flex items-center justify-center h-32">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : diff ? (
            <div className="p-4">
              {diff.split("\n").map((line, i) => renderDiffLine(line, i))}
            </div>
          ) : (
            <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
              {from && to ? "Clique em Comparar para ver o diff" : "Selecione dois branches para comparar"}
            </div>
          )}
        </ScrollArea>
      </div>
    </div>
  );
}
