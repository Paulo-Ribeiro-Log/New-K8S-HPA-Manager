import { useState, useEffect, useRef, useCallback } from "react";
import Editor, { OnMount } from "@monaco-editor/react";
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
} from "@/lib/api/client";

const API_BASE = "/api/v1";

// Mapeia extensão para linguagem do Monaco
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

// Cor de status git
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

// ─── FileTree ──────────────────────────────────────────────────────────────

interface FileTreeNodeProps {
  node: CodeEditorFileNode;
  selectedPath: string;
  onSelect: (node: CodeEditorFileNode) => void;
  modifiedPaths: Set<string>;
  level: number;
}

function FileTreeNode({ node, selectedPath, onSelect, modifiedPaths, level }: FileTreeNodeProps) {
  const [open, setOpen] = useState(level < 1);

  const isSelected = selectedPath === node.path;
  const isModified = modifiedPaths.has(node.path);

  if (node.type === "dir") {
    return (
      <div>
        <button
          onClick={() => setOpen(o => !o)}
          className={`w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded hover:bg-muted/50 text-left`}
          style={{ paddingLeft: `${level * 12 + 4}px` }}
        >
          {open ? <ChevronDown className="w-3 h-3 flex-shrink-0 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 flex-shrink-0 text-muted-foreground" />}
          {open ? <FolderOpen className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" /> : <Folder className="w-3.5 h-3.5 flex-shrink-0 text-blue-400" />}
          <span className="truncate text-foreground/80">{node.name}</span>
        </button>
        {open && node.children?.map(child => (
          <FileTreeNode key={child.path} node={child} selectedPath={selectedPath} onSelect={onSelect} modifiedPaths={modifiedPaths} level={level + 1} />
        ))}
      </div>
    );
  }

  return (
    <button
      onClick={() => onSelect(node)}
      className={`w-full flex items-center gap-1 px-1 py-0.5 text-xs rounded text-left hover:bg-muted/50 ${isSelected ? "bg-accent text-accent-foreground" : ""}`}
      style={{ paddingLeft: `${level * 12 + 4}px` }}
    >
      <span className="w-3 h-3 flex-shrink-0" />
      <File className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
      <span className={`truncate flex-1 ${isModified ? "text-yellow-400" : "text-foreground/80"}`}>{node.name}</span>
      {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400 flex-shrink-0" />}
    </button>
  );
}

// ─── CloneDialog ───────────────────────────────────────────────────────────

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
        const dataLine = evt.split("\n").find(l => l.startsWith("data:"));
        if (!dataLine) continue;
        try {
          const d = JSON.parse(dataLine.slice(5));
          if (d.message) setLogs(l => [...l, d.message]);
          if (d.done) {
            if (d.error) setError(d.error);
            else doneId = d.id;
          }
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

// ─── CommitDialog ──────────────────────────────────────────────────────────

interface CommitDialogProps {
  open: boolean;
  repoId: string;
  status: CodeEditorGitStatus | null;
  onClose: () => void;
  onDone: () => void;
}

function CommitDialog({ open, repoId, status, onClose, onDone }: CommitDialogProps) {
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function doCommit() {
    if (!message.trim()) { setError("Mensagem é obrigatória"); return; }
    setLoading(true); setError("");
    try {
      await apiClient.codeEditorCommit(repoId, message.trim());
      setMessage(""); onDone();
    } catch (e: any) { setError(e.message || "Erro ao commitar"); }
    finally { setLoading(false); }
  }

  const changedFiles = status?.files ?? [];

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><GitCommit className="w-4 h-4" />Novo Commit</DialogTitle></DialogHeader>
        <div className="space-y-3">
          {changedFiles.length > 0 && (
            <div>
              <p className="text-xs text-muted-foreground mb-1">Arquivos alterados ({changedFiles.length}):</p>
              <ScrollArea className="h-28 border border-border/40 rounded p-2">
                {changedFiles.map(f => (
                  <div key={f.path} className="flex items-center gap-2 text-xs py-0.5">
                    <span className={`font-bold w-4 text-center ${statusColor(f.status)}`}>{statusLabel(f.status)}</span>
                    <span className="font-mono text-foreground/80 truncate">{f.path}</span>
                  </div>
                ))}
              </ScrollArea>
            </div>
          )}
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Mensagem</label>
            <Input placeholder="feat: descrição da mudança" value={message} onChange={e => setMessage(e.target.value)} disabled={loading} onKeyDown={e => e.key === "Enter" && doCommit()} />
          </div>
          {error && <div className="text-red-400 text-xs flex items-center gap-1"><AlertCircle className="w-3 h-3" />{error}</div>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>Cancelar</Button>
          <Button onClick={doCommit} disabled={loading || !message.trim()}>
            {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <GitCommit className="w-3 h-3 mr-1" />}
            Commitar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── BranchDialog ──────────────────────────────────────────────────────────

interface BranchDialogProps {
  open: boolean;
  repoId: string;
  currentBranch: string;
  onClose: () => void;
  onDone: () => void;
}

function BranchDialog({ open, repoId, currentBranch: cur, onClose, onDone }: BranchDialogProps) {
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function doCreate() {
    if (!name.trim()) { setError("Nome é obrigatório"); return; }
    setLoading(true); setError("");
    try {
      await apiClient.codeEditorCreateBranch(repoId, name.trim(), cur);
      onDone();
    } catch (e: any) { setError(e.message || "Erro ao criar branch"); }
    finally { setLoading(false); }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && !loading && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><GitBranch className="w-4 h-4" />Criar Branch</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">A partir de: <span className="font-mono text-foreground">{cur}</span></p>
          <Input placeholder="feature/nova-funcionalidade" value={name} onChange={e => setName(e.target.value)} disabled={loading} onKeyDown={e => e.key === "Enter" && doCreate()} />
          {error && <div className="text-red-400 text-xs">{error}</div>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>Cancelar</Button>
          <Button onClick={doCreate} disabled={loading || !name.trim()}>
            {loading ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : null} Criar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── SseDialog (Pull/Push) ─────────────────────────────────────────────────

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
            if (d.done) {
              if (d.error) setError(d.error);
              else setSuccess(true);
            }
          } catch (_) {}
        }
      }
      setRunning(false);
    })().catch(e => { setError(e.message); setRunning(false); });
  }, [open]);

  useEffect(() => { if (logsRef.current) logsRef.current.scrollTop = logsRef.current.scrollHeight; }, [logs]);

  return (
    <Dialog open={open} onOpenChange={v => { if (!v && !running) { onClose(); } }}>
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

// ─── Main CodeEditorTab ────────────────────────────────────────────────────

export function CodeEditorTab() {
  const [repos, setRepos] = useState<CodeEditorRepo[]>([]);
  const [selectedRepo, setSelectedRepo] = useState<CodeEditorRepo | null>(null);
  const [tree, setTree] = useState<CodeEditorFileNode[]>([]);
  const [selectedFile, setSelectedFile] = useState<CodeEditorFileNode | null>(null);
  const [fileContent, setFileContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [status, setStatus] = useState<CodeEditorGitStatus | null>(null);
  const [branches, setBranches] = useState<CodeEditorBranches | null>(null);
  const [log, setLog] = useState<CodeEditorLogEntry[]>([]);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<string[]>([]);

  // Panels state
  const [sidePanel, setSidePanel] = useState<"files" | "git" | "log">("files");

  // Dialogs
  const [showClone, setShowClone] = useState(false);
  const [showCommit, setShowCommit] = useState(false);
  const [showBranch, setShowBranch] = useState(false);
  const [sseDialog, setSseDialog] = useState<{ title: string; endpoint: string; body?: object } | null>(null);
  const [branchDropdown, setBranchDropdown] = useState(false);

  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const branchRef = useRef<HTMLDivElement>(null);

  const isModified = fileContent !== savedContent;

  // Fecha dropdown de branch ao clicar fora
  useEffect(() => {
    const handler = (e: MouseEvent) => { if (branchRef.current && !branchRef.current.contains(e.target as Node)) setBranchDropdown(false); };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  async function loadRepos() {
    try { setRepos(await apiClient.codeEditorListRepos()); } catch (_) {}
  }

  useEffect(() => { loadRepos(); }, []);

  async function selectRepo(repo: CodeEditorRepo) {
    setSelectedRepo(repo);
    setSelectedFile(null);
    setFileContent("");
    setSavedContent("");
    setSearchQuery("");
    setSearchResults([]);
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
    setSelectedFile(node);
    try {
      const { content } = await apiClient.codeEditorReadFile(selectedRepo.id, node.path);
      setFileContent(content);
      setSavedContent(content);
    } catch (e: any) {
      setFileContent(`// Erro ao carregar: ${e.message}`);
      setSavedContent("");
    }
  }

  async function saveFile() {
    if (!selectedRepo || !selectedFile || !isModified) return;
    try {
      await apiClient.codeEditorWriteFile(selectedRepo.id, selectedFile.path, fileContent);
      setSavedContent(fileContent);
      await loadStatus(selectedRepo.id);
    } catch (e: any) {
      alert("Erro ao salvar: " + e.message);
    }
  }

  async function handleCheckout(branch: string) {
    if (!selectedRepo) return;
    setBranchDropdown(false);
    try {
      await apiClient.codeEditorCheckoutBranch(selectedRepo.id, branch);
      await Promise.all([loadStatus(selectedRepo.id), loadBranches(selectedRepo.id), loadTree(selectedRepo.id)]);
      setSelectedFile(null); setFileContent(""); setSavedContent("");
    } catch (e: any) { alert("Erro: " + e.message); }
  }

  async function handleSearch() {
    if (!selectedRepo || !searchQuery.trim()) return;
    try {
      const r = await apiClient.codeEditorSearchFiles(selectedRepo.id, searchQuery);
      setSearchResults(r.matches ?? []);
    } catch (_) {}
  }

  async function deleteRepo(id: string) {
    if (!confirm(`Remover repositório "${id}" localmente? Os dados no GitHub não serão afetados.`)) return;
    try {
      await apiClient.codeEditorDeleteRepo(id);
      if (selectedRepo?.id === id) { setSelectedRepo(null); setTree([]); setSelectedFile(null); setFileContent(""); setSavedContent(""); setStatus(null); setBranches(null); setLog([]); }
      await loadRepos();
    } catch (e: any) { alert("Erro: " + e.message); }
  }

  const handleEditorMount: OnMount = (editor) => {
    editorRef.current = editor;
    editor.addCommand(
      // Ctrl+S
      (2048 | 49),
      () => saveFile(),
    );
  };

  const modifiedPaths = new Set((status?.files ?? []).map(f => f.path));

  // Todos os branches para o seletor
  const allBranches = branches ? [...(branches.local ?? []), ...(branches.remote ?? []).filter(r => !branches.local.includes(r.replace("origin/", "")))] : [];

  return (
    <div className="flex flex-col h-full min-h-0 bg-background">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border/50 flex-shrink-0 bg-card/30">
        <FolderGit2 className="w-4 h-4 text-blue-400" />
        <span className="text-sm font-medium">Editor de Código</span>

        {/* Repo selector */}
        {repos.length > 0 && (
          <select
            className="ml-2 text-xs bg-muted border border-border/50 rounded px-2 py-1 text-foreground"
            value={selectedRepo?.id ?? ""}
            onChange={e => { const r = repos.find(x => x.id === e.target.value); if (r) selectRepo(r); }}
          >
            <option value="">Selecionar repositório...</option>
            {repos.map(r => (
              <option key={r.id} value={r.id}>{r.owner}/{r.repo}</option>
            ))}
          </select>
        )}

        {/* Branch selector */}
        {selectedRepo && branches && (
          <div ref={branchRef} className="relative">
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => setBranchDropdown(d => !d)}>
              <GitBranch className="w-3 h-3" />
              {branches.current}
              <ChevronDown className="w-3 h-3" />
            </Button>
            {branchDropdown && (
              <div className="absolute top-full left-0 mt-1 z-50 bg-popover border border-border rounded shadow-lg min-w-48 max-h-64 overflow-y-auto">
                <div className="px-2 py-1 text-xs text-muted-foreground border-b border-border/50 font-medium">Locais</div>
                {(branches.local ?? []).map(b => (
                  <button key={b} className={`w-full text-left px-3 py-1.5 text-xs hover:bg-muted ${b === branches.current ? "text-blue-400 font-medium" : ""}`} onClick={() => handleCheckout(b)}>
                    {b === branches.current && "✓ "}{b}
                  </button>
                ))}
                {(branches.remote ?? []).length > 0 && <>
                  <div className="px-2 py-1 text-xs text-muted-foreground border-t border-b border-border/50 font-medium">Remotos</div>
                  {(branches.remote ?? []).map(b => (
                    <button key={b} className="w-full text-left px-3 py-1.5 text-xs hover:bg-muted text-muted-foreground" onClick={() => handleCheckout(b)}>{b}</button>
                  ))}
                </>}
              </div>
            )}
          </div>
        )}

        <div className="flex-1" />

        {/* Actions */}
        {selectedRepo && (
          <>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => setSseDialog({ title: "Git Pull", endpoint: `/code-editor/repos/${selectedRepo.id}/pull` })}>
              <Download className="w-3 h-3" />Pull
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => setShowCommit(true)} disabled={modifiedPaths.size === 0}>
              <GitCommit className="w-3 h-3" />Commit
              {modifiedPaths.size > 0 && <span className="bg-yellow-500 text-black text-[10px] px-1 rounded-full">{modifiedPaths.size}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => setSseDialog({ title: "Git Push", endpoint: `/code-editor/repos/${selectedRepo.id}/push` })}>
              <Upload className="w-3 h-3" />Push
              {status?.ahead && status.ahead !== "0" && <span className="bg-blue-500 text-white text-[10px] px-1 rounded-full">{status.ahead}</span>}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => setShowBranch(true)}>
              <Plus className="w-3 h-3" />Branch
            </Button>
          </>
        )}

        <Button size="sm" className="h-6 text-xs gap-1" onClick={() => setShowClone(true)}>
          <GitPullRequest className="w-3 h-3" />Clonar
        </Button>
      </div>

      {/* Body */}
      <div className="flex flex-1 min-h-0">
        {/* Sidebar */}
        <div className="w-56 flex-shrink-0 border-r border-border/50 flex flex-col min-h-0">
          {/* Sidebar tabs */}
          <div className="flex border-b border-border/50 flex-shrink-0">
            {(["files", "git", "log"] as const).map(panel => (
              <button key={panel} onClick={() => setSidePanel(panel)} className={`flex-1 py-1.5 text-xs font-medium transition-colors ${sidePanel === panel ? "text-foreground border-b-2 border-primary" : "text-muted-foreground hover:text-foreground"}`}>
                {panel === "files" ? "Arquivos" : panel === "git" ? `Git${modifiedPaths.size > 0 ? ` (${modifiedPaths.size})` : ""}` : "Log"}
              </button>
            ))}
          </div>

          {/* Sidebar content */}
          <div className="flex-1 min-h-0 overflow-hidden flex flex-col">
            {sidePanel === "files" && (
              <>
                {/* Search */}
                {selectedRepo && (
                  <div className="px-2 py-1.5 flex gap-1 flex-shrink-0 border-b border-border/30">
                    <Input className="h-6 text-xs" placeholder="Buscar arquivo..." value={searchQuery} onChange={e => setSearchQuery(e.target.value)} onKeyDown={e => e.key === "Enter" && handleSearch()} />
                    <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={handleSearch}><Search className="w-3 h-3" /></Button>
                  </div>
                )}
                <ScrollArea className="flex-1">
                  {/* Repos list if none selected */}
                  {!selectedRepo && (
                    <div className="p-2 space-y-1">
                      {repos.length === 0 ? (
                        <p className="text-xs text-muted-foreground text-center py-4">Nenhum repositório clonado.<br />Clique em "Clonar" para começar.</p>
                      ) : repos.map(r => (
                        <div key={r.id} className="group flex items-center gap-1 rounded hover:bg-muted/50 px-1 py-1">
                          <button className="flex-1 text-left text-xs truncate" onClick={() => selectRepo(r)}>
                            <span className="font-medium">{r.owner}/{r.repo}</span>
                            <span className="text-muted-foreground block text-[10px]">{r.current_branch}</span>
                          </button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0 opacity-0 group-hover:opacity-100" onClick={() => deleteRepo(r.id)}><Trash2 className="w-3 h-3 text-red-400" /></Button>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Search results */}
                  {selectedRepo && searchQuery && searchResults.length > 0 && (
                    <div className="p-1 space-y-0.5">
                      {searchResults.slice(0, 50).map(path => (
                        <button key={path} className="w-full text-left px-2 py-0.5 text-xs hover:bg-muted/50 rounded font-mono truncate text-foreground/80" onClick={() => openFile({ name: path.split("/").pop() ?? path, path, type: "file" })}>
                          {path}
                        </button>
                      ))}
                      <div className="flex justify-end px-1"><Button variant="ghost" size="sm" className="h-5 text-[10px]" onClick={() => { setSearchQuery(""); setSearchResults([]); }}>Limpar</Button></div>
                    </div>
                  )}

                  {/* File tree */}
                  {selectedRepo && !searchQuery && (
                    <div className="p-1">
                      <div className="flex items-center justify-between px-1 mb-1">
                        <span className="text-xs text-muted-foreground font-medium">{selectedRepo.owner}/{selectedRepo.repo}</span>
                        <div className="flex gap-1">
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => loadTree(selectedRepo.id)}><RefreshCw className="w-3 h-3" /></Button>
                          <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => { setSelectedRepo(null); setTree([]); }}><X className="w-3 h-3" /></Button>
                        </div>
                      </div>
                      {tree.map(node => <FileTreeNode key={node.path} node={node} selectedPath={selectedFile?.path ?? ""} onSelect={openFile} modifiedPaths={modifiedPaths} level={0} />)}
                    </div>
                  )}
                </ScrollArea>
              </>
            )}

            {sidePanel === "git" && selectedRepo && (
              <ScrollArea className="flex-1">
                <div className="p-2 space-y-3">
                  {status && (
                    <div>
                      <p className="text-xs font-medium text-muted-foreground mb-1">Alterações ({status.files.length})</p>
                      {status.files.length === 0 ? (
                        <p className="text-xs text-muted-foreground">Árvore limpa</p>
                      ) : status.files.map(f => (
                        <div key={f.path} className="flex items-center gap-1.5 py-0.5">
                          <span className={`text-xs font-bold w-4 text-center ${statusColor(f.status)}`}>{statusLabel(f.status)}</span>
                          <button className="text-xs font-mono truncate hover:text-foreground text-foreground/70 flex-1 text-left" onClick={() => openFile({ name: f.path.split("/").pop() ?? f.path, path: f.path, type: "file" })}>{f.path}</button>
                        </div>
                      ))}
                    </div>
                  )}
                  <div className="flex gap-1">
                    <Button variant="outline" size="sm" className="flex-1 h-6 text-xs" onClick={() => loadStatus(selectedRepo.id)}><RefreshCw className="w-3 h-3 mr-1" />Atualizar</Button>
                    <Button size="sm" className="flex-1 h-6 text-xs" onClick={() => setShowCommit(true)} disabled={modifiedPaths.size === 0}><GitCommit className="w-3 h-3 mr-1" />Commit</Button>
                  </div>
                </div>
              </ScrollArea>
            )}

            {sidePanel === "log" && selectedRepo && (
              <ScrollArea className="flex-1">
                <div className="p-2 space-y-2">
                  <Button variant="ghost" size="sm" className="w-full h-6 text-xs" onClick={() => loadLog(selectedRepo.id)}><RefreshCw className="w-3 h-3 mr-1" />Atualizar</Button>
                  {log.map(entry => (
                    <div key={entry.hash} className="border-b border-border/30 pb-2">
                      <p className="text-xs text-foreground/90 leading-tight">{entry.message}</p>
                      <div className="flex items-center gap-1 mt-0.5">
                        <Clock className="w-2.5 h-2.5 text-muted-foreground" />
                        <span className="text-[10px] text-muted-foreground">{entry.when}</span>
                        <span className="text-[10px] text-muted-foreground">· {entry.author}</span>
                      </div>
                      <span className="font-mono text-[10px] text-muted-foreground/60">{entry.hash.slice(0, 7)}</span>
                    </div>
                  ))}
                  {log.length === 0 && <p className="text-xs text-muted-foreground text-center py-4">Sem commits</p>}
                </div>
              </ScrollArea>
            )}
          </div>
        </div>

        {/* Editor area */}
        <div className="flex-1 flex flex-col min-h-0 min-w-0">
          {selectedFile ? (
            <>
              {/* File tab bar */}
              <div className="flex items-center gap-2 px-3 py-1.5 border-b border-border/50 flex-shrink-0 bg-card/20">
                <File className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-xs font-mono text-foreground/80">{selectedFile.path}</span>
                {isModified && <span className="w-1.5 h-1.5 rounded-full bg-yellow-400" title="Modificado (não salvo)" />}
                <div className="flex-1" />
                <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={saveFile} disabled={!isModified}>
                  <CheckCircle2 className="w-3 h-3" />Salvar (Ctrl+S)
                </Button>
              </div>
              {/* Monaco */}
              <div className="flex-1 min-h-0">
                <Editor
                  height="100%"
                  language={extToLanguage(selectedFile.name)}
                  value={fileContent}
                  theme="vs-dark"
                  onMount={handleEditorMount}
                  onChange={v => setFileContent(v ?? "")}
                  options={{
                    automaticLayout: true,
                    minimap: { enabled: false },
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
              <div className="text-center space-y-2">
                <FolderGit2 className="w-12 h-12 mx-auto opacity-20" />
                {selectedRepo ? (
                  <p className="text-sm">Selecione um arquivo na árvore para editar</p>
                ) : (
                  <>
                    <p className="text-sm">Nenhum repositório selecionado</p>
                    <Button size="sm" onClick={() => setShowClone(true)}><GitPullRequest className="w-3.5 h-3.5 mr-1.5" />Clonar repositório</Button>
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Dialogs */}
      <CloneDialog open={showClone} onClose={() => setShowClone(false)} onDone={async id => { await loadRepos(); const r = repos.find(x => x.id === id); if (r) selectRepo(r); else { const fresh = await apiClient.codeEditorListRepos(); const found = fresh.find(x => x.id === id); if (found) { setRepos(fresh); selectRepo(found); } } }} />

      {selectedRepo && (
        <>
          <CommitDialog open={showCommit} repoId={selectedRepo.id} status={status} onClose={() => setShowCommit(false)} onDone={async () => { setShowCommit(false); await Promise.all([loadStatus(selectedRepo.id), loadLog(selectedRepo.id)]); }} />
          <BranchDialog open={showBranch} repoId={selectedRepo.id} currentBranch={branches?.current ?? ""} onClose={() => setShowBranch(false)} onDone={async () => { setShowBranch(false); await Promise.all([loadBranches(selectedRepo.id), loadStatus(selectedRepo.id)]); }} />
          {sseDialog && (
            <SseDialog open={true} title={sseDialog.title} endpoint={sseDialog.endpoint} body={sseDialog.body} onClose={() => setSseDialog(null)} onDone={async () => { setSseDialog(null); await Promise.all([loadStatus(selectedRepo.id), loadBranches(selectedRepo.id), loadLog(selectedRepo.id)]); }} />
          )}
        </>
      )}
    </div>
  );
}
