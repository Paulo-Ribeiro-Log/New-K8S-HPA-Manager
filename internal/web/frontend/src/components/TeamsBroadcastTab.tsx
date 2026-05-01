import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import Editor, { BeforeMount, OnMount } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Components } from "react-markdown";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Bold, Italic, Strikethrough, Code, Link2, Image, List,
  ListOrdered, Quote, Minus, Table2, Eye, Code2,
  RefreshCw, X, MessageSquarePlus, ScanSearch, Type, Users, Copy, Check,
  Save, FolderOpen, Trash2, FileText, FilePlus,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

// ── Tipos ─────────────────────────────────────────────────────────────────────

interface BroadcastChat {
  id: string;
  display_name: string;
  source: string;
}

interface SendResult {
  thread_id: string;
  ok: boolean;
  status: number;
  error?: string;
}

// ── Monaco theme ──────────────────────────────────────────────────────────────

const beforeMount: BeforeMount = (monaco) => {
  monaco.editor.defineTheme("markdown-dark", {
    base: "vs-dark",
    inherit: true,
    rules: [
      { token: "markup.heading.1.markdown", foreground: "F1F5F9", fontStyle: "bold" },
      { token: "markup.heading.2.markdown", foreground: "93C5FD", fontStyle: "bold" },
      { token: "markup.heading.3.markdown", foreground: "6EE7B7", fontStyle: "bold" },
      { token: "markup.heading.4.markdown", foreground: "FCD34D", fontStyle: "bold" },
      { token: "markup.heading.5.markdown", foreground: "F9A8D4", fontStyle: "bold" },
      { token: "markup.heading.6.markdown", foreground: "F9A8D4", fontStyle: "bold" },
      { token: "punctuation.definition.heading.markdown", foreground: "64748B" },
      { token: "markup.bold.markdown", fontStyle: "bold", foreground: "E2E8F0" },
      { token: "markup.italic.markdown", fontStyle: "italic", foreground: "CBD5E1" },
      { token: "markup.strikethrough.markdown", fontStyle: "strikethrough", foreground: "94A3B8" },
      { token: "markup.inline.raw.string.markdown", foreground: "86EFAC" },
      { token: "markup.fenced_code.block.markdown", foreground: "86EFAC" },
      { token: "fenced_code.block.language", foreground: "A5B4FC" },
      { token: "string.other.link.title.markdown", foreground: "7DD3FC" },
      { token: "markup.underline.link.markdown", foreground: "38BDF8" },
      { token: "punctuation.definition.list.begin.markdown", foreground: "94A3B8" },
      { token: "markup.quote.markdown", foreground: "94A3B8", fontStyle: "italic" },
      { token: "punctuation.separator.table.markdown", foreground: "64748B" },
    ],
    colors: { "editor.background": "#0D1117" },
  });
};

// ── ReactMarkdown components com estilos distintos ────────────────────────────

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

// ── Toolbar ───────────────────────────────────────────────────────────────────

type FormatAction = {
  label: string;
  icon?: React.ReactNode;
  title: string;
  action: (selected: string) => { text: string };
};

const TOOLBAR: (FormatAction | "sep")[] = [
  { label: "H1", title: "Título 1", action: (s) => ({ text: `# ${s || "Título"}` }) },
  { label: "H2", title: "Título 2", action: (s) => ({ text: `## ${s || "Título"}` }) },
  { label: "H3", title: "Título 3", action: (s) => ({ text: `### ${s || "Título"}` }) },
  "sep",
  { label: "", icon: <Bold className="w-3.5 h-3.5" />, title: "Negrito", action: (s) => ({ text: `**${s || "texto"}**` }) },
  { label: "", icon: <Italic className="w-3.5 h-3.5" />, title: "Itálico", action: (s) => ({ text: `*${s || "texto"}*` }) },
  { label: "", icon: <Strikethrough className="w-3.5 h-3.5" />, title: "Tachado", action: (s) => ({ text: `~~${s || "texto"}~~` }) },
  "sep",
  { label: "", icon: <Code className="w-3.5 h-3.5" />, title: "Código inline", action: (s) => ({ text: `\`${s || "código"}\`` }) },
  { label: "", icon: <Code2 className="w-3.5 h-3.5" />, title: "Bloco de código", action: (s) => ({ text: `\`\`\`\n${s || "código"}\n\`\`\`` }) },
  "sep",
  { label: "", icon: <Link2 className="w-3.5 h-3.5" />, title: "Link", action: (s) => ({ text: `[${s || "texto"}](url)` }) },
  { label: "", icon: <Image className="w-3.5 h-3.5" />, title: "Imagem", action: (s) => ({ text: `![${s || "alt"}](url)` }) },
  "sep",
  { label: "", icon: <List className="w-3.5 h-3.5" />, title: "Lista", action: (s) => ({ text: s ? s.split("\n").map(l => `- ${l}`).join("\n") : "- item" }) },
  { label: "", icon: <ListOrdered className="w-3.5 h-3.5" />, title: "Lista numerada", action: (s) => ({ text: s ? s.split("\n").map((l, i) => `${i + 1}. ${l}`).join("\n") : "1. item" }) },
  { label: "", icon: <Quote className="w-3.5 h-3.5" />, title: "Citação", action: (s) => ({ text: `> ${s || "citação"}` }) },
  "sep",
  { label: "", icon: <Table2 className="w-3.5 h-3.5" />, title: "Tabela", action: () => ({ text: "| Coluna 1 | Coluna 2 | Coluna 3 |\n|----------|----------|----------|\n| Valor 1  | Valor 2  | Valor 3  |" }) },
  { label: "", icon: <Minus className="w-3.5 h-3.5" />, title: "Divisor", action: () => ({ text: "\n---\n" }) },
];

// ── Modal de seleção de chats ─────────────────────────────────────────────────

function ChatSelectorModal({
  open,
  onClose,
  selected,
  onToggle,
  onSelectMany,
}: {
  open: boolean;
  onClose: () => void;
  selected: Map<string, BroadcastChat>;
  onToggle: (chat: BroadcastChat) => void;
  onSelectMany: (chats: BroadcastChat[], select: boolean) => void;
}) {
  const [query, setQuery] = useState("");
  const [allChats, setAllChats] = useState<BroadcastChat[]>([]);
  const [loading, setLoading] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [scanMsg, setScanMsg] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const loadAll = useCallback(async () => {
    setLoading(true);
    try {
      const data = await apiClient.getBroadcastChats();
      setAllChats(data.chats ?? []);
    } catch {
      setAllChats([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    loadAll();
  }, [open, loadAll]);

  const handleScan = async () => {
    setScanning(true);
    setScanMsg(null);
    try {
      const res = await apiClient.scanBroadcastChats();
      setScanMsg(`Scan concluído: ${res.count} chats encontrados`);
      await loadAll();
    } catch (e: unknown) {
      setScanMsg(e instanceof Error ? e.message : "Erro ao escanear");
    } finally {
      setScanning(false);
    }
  };

  // Remove acentos e normaliza para busca: "Análise" → "analise"
  const accentFold = (s: string) =>
    (s ?? "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase().trim();

  const q = accentFold(query);
  const sortedChats = useMemo(() => {
    const filtered = q
      ? allChats.filter(c => accentFold(c.display_name).includes(q))
      : allChats;
    return [...filtered].sort((a, b) =>
      (selected.has(a.id) ? 0 : 1) - (selected.has(b.id) ? 0 : 1)
    );
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, allChats, selected]);

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        className="max-w-lg w-full flex flex-col"
        style={{ maxHeight: "80vh" }}
        onOpenAutoFocus={(e) => { e.preventDefault(); inputRef.current?.focus(); }}
      >
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <Users className="w-4 h-4" />
            Selecionar destinatários
            {selected.size > 0 && (
              <Badge variant="secondary" className="text-[10px] h-4 px-1.5">
                {selected.size} selecionado{selected.size !== 1 ? "s" : ""}
              </Badge>
            )}
          </DialogTitle>
        </DialogHeader>

        {/* Busca + scan */}
        <div className="flex gap-2 flex-shrink-0">
          <input
            ref={inputRef}
            type="text"
            placeholder="Buscar chat por nome..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="flex h-8 w-full rounded-md border border-input bg-background px-3 py-1 text-xs ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          />
          <Button
            variant="outline"
            size="sm"
            className="h-8 px-3 text-xs gap-1.5 flex-shrink-0"
            onClick={handleScan}
            disabled={scanning}
            title="Abre o Chrome com a sessão do Teams e escaneia todos os chats via IndexedDB (~30s)"
          >
            {scanning
              ? <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              : <ScanSearch className="w-3.5 h-3.5" />}
            {scanning ? "Escaneando..." : "Escanear Teams"}
          </Button>
        </div>

        {scanMsg && (
          <p className="text-[11px] text-muted-foreground flex-shrink-0">{scanMsg}</p>
        )}

        {/* Lista de chats */}
        <div className="flex-1 overflow-y-auto min-h-0 border border-border rounded-md">
          {loading && (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="w-4 h-4 animate-spin text-muted-foreground" />
            </div>
          )}
          {!loading && sortedChats.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-8">
              {q
                ? `Nenhum chat encontrado para "${q}"`
                : "Cache vazio — clique em \"Escanear Teams\" para carregar os chats"}
            </p>
          )}
          {!loading && sortedChats.map((chat) => {
            const isSel = selected.has(chat.id);
            return (
              <label
                key={chat.id}
                className={`
                  flex items-center gap-3 px-3 py-2 cursor-pointer text-xs border-b border-border/40
                  last:border-0 hover:bg-muted/40 transition-colors
                  ${isSel ? "bg-primary/5 text-foreground" : "text-muted-foreground"}
                `}
              >
                <input
                  type="checkbox"
                  className="w-3.5 h-3.5 accent-primary flex-shrink-0"
                  checked={isSel}
                  onChange={() => onToggle(chat)}
                />
                <span className="truncate leading-tight flex-1">{chat.display_name}</span>
                {isSel && <span className="text-[10px] text-primary flex-shrink-0">✓</span>}
              </label>
            );
          })}
        </div>

        {/* Rodapé */}
        <div className="flex items-center justify-between flex-shrink-0 pt-1 gap-2">
          <span className="text-[11px] text-muted-foreground flex-shrink-0">
            {allChats.length > 0
              ? `${sortedChats.length} de ${allChats.length} chat${allChats.length !== 1 ? "s" : ""}`
              : ""}
          </span>
          {sortedChats.length > 0 && (() => {
            const anyVisibleSelected = sortedChats.some(c => selected.has(c.id));
            return (
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs flex-shrink-0"
                onClick={() => onSelectMany(sortedChats, !anyVisibleSelected)}
              >
                {anyVisibleSelected ? "Desmarcar tudo" : "Selecionar tudo"}
              </Button>
            );
          })()}
          <Button size="sm" className="h-8 text-xs flex-shrink-0" onClick={onClose}>
            Confirmar seleção
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Modal de templates ────────────────────────────────────────────────────────

const EXTENSIONS = [".md", ".txt"] as const;
type Ext = typeof EXTENSIONS[number];

function TemplatesModal({
  open,
  onClose,
  currentContent,
  onLoad,
}: {
  open: boolean;
  onClose: () => void;
  currentContent: string;
  onLoad: (content: string, filename: string) => void;
}) {
  const [templates, setTemplates] = useState<{ filename: string; updated_at: string; size: number }[]>([]);
  const [loading, setLoading] = useState(false);
  const [saveName, setSaveName] = useState("");
  const [saveExt, setSaveExt] = useState<Ext>(".md");
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const loadList = useCallback(async () => {
    setLoading(true);
    try {
      const data = await apiClient.listBroadcastTemplates();
      setTemplates(data.templates ?? []);
    } catch {
      setTemplates([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) { setSaveName(""); setSaveMsg(null); loadList(); }
  }, [open, loadList]);

  const handleSave = async () => {
    const name = saveName.trim();
    if (!name) return;
    const filename = name.endsWith(saveExt) ? name : name + saveExt;
    setSaving(true);
    setSaveMsg(null);
    try {
      await apiClient.saveBroadcastTemplate(filename, currentContent);
      setSaveMsg({ ok: true, text: `Salvo como "${filename}"` });
      setSaveName("");
      loadList();
    } catch (e: unknown) {
      setSaveMsg({ ok: false, text: e instanceof Error ? e.message : "Erro ao salvar" });
    } finally {
      setSaving(false);
    }
  };

  const handleLoad = async (filename: string) => {
    try {
      const data = await apiClient.getBroadcastTemplate(filename);
      onLoad(data.content, filename);
      onClose();
    } catch {
      // silencioso — arquivo pode ter sido removido externamente
    }
  };

  const handleDelete = async (filename: string) => {
    setDeleting(filename);
    try {
      await apiClient.deleteBroadcastTemplate(filename);
      setTemplates((prev) => prev.filter((t) => t.filename !== filename));
    } catch {
      // silencioso
    } finally {
      setDeleting(null);
    }
  };

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    return `${(bytes / 1024).toFixed(1)} KB`;
  };

  const formatDate = (iso: string) => {
    try {
      return new Date(iso).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" });
    } catch { return iso; }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-lg w-full flex flex-col" style={{ maxHeight: "80vh" }}>
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <FolderOpen className="w-4 h-4" />
            Arquivos salvos
          </DialogTitle>
        </DialogHeader>

        {/* ── Salvar conteúdo atual ── */}
        <div className="flex-shrink-0 border border-border rounded-md p-3 flex flex-col gap-2 bg-muted/20">
          <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wide">
            Salvar conteúdo atual
          </span>
          <div className="flex gap-2">
            <Input
              className="h-8 text-xs flex-1"
              placeholder="Nome do arquivo..."
              value={saveName}
              onChange={(e) => setSaveName(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") handleSave(); }}
            />
            <select
              value={saveExt}
              onChange={(e) => setSaveExt(e.target.value as Ext)}
              className="h-8 rounded-md border border-input bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            >
              {EXTENSIONS.map((ext) => (
                <option key={ext} value={ext}>{ext}</option>
              ))}
            </select>
            <Button
              size="sm"
              className="h-8 text-xs gap-1.5 flex-shrink-0"
              onClick={handleSave}
              disabled={!saveName.trim() || saving}
            >
              {saving ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
              Salvar
            </Button>
          </div>
          {saveMsg && (
            <p className={`text-[11px] ${saveMsg.ok ? "text-green-400" : "text-destructive"}`}>
              {saveMsg.ok ? "✅" : "❌"} {saveMsg.text}
            </p>
          )}
        </div>

        {/* ── Lista de arquivos ── */}
        <div className="flex-1 overflow-y-auto min-h-0 border border-border rounded-md">
          {loading && (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="w-4 h-4 animate-spin text-muted-foreground" />
            </div>
          )}
          {!loading && templates.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-8">
              Nenhum arquivo salvo ainda
            </p>
          )}
          {!loading && templates.map((t) => (
            <div
              key={t.filename}
              className="flex items-center gap-3 px-3 py-2.5 border-b border-border/40 last:border-0 hover:bg-muted/30 transition-colors group"
            >
              <FileText className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-xs font-medium text-foreground truncate">{t.filename}</p>
                <p className="text-[10px] text-muted-foreground">
                  {formatDate(t.updated_at)} · {formatSize(t.size)}
                </p>
              </div>
              <div className="flex gap-1 flex-shrink-0">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[10px] gap-1 opacity-0 group-hover:opacity-100 transition-opacity"
                  onClick={() => handleLoad(t.filename)}
                >
                  <FolderOpen className="w-3 h-3" />
                  Abrir
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[10px] text-destructive hover:text-destructive opacity-0 group-hover:opacity-100 transition-opacity"
                  onClick={() => handleDelete(t.filename)}
                  disabled={deleting === t.filename}
                >
                  {deleting === t.filename
                    ? <RefreshCw className="w-3 h-3 animate-spin" />
                    : <Trash2 className="w-3 h-3" />}
                </Button>
              </div>
            </div>
          ))}
        </div>

        <div className="flex justify-between items-center flex-shrink-0 pt-1">
          <span className="text-[11px] text-muted-foreground">
            {templates.length > 0 ? `${templates.length} arquivo${templates.length !== 1 ? "s" : ""}` : ""}
          </span>
          <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onClose}>
            Fechar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Modal Salvar Como ─────────────────────────────────────────────────────────

function SaveAsModal({
  open,
  initialName,
  onClose,
  onSave,
}: {
  open: boolean;
  initialName: string;
  onClose: () => void;
  onSave: (filename: string) => Promise<void>;
}) {
  const [name, setName] = useState(initialName);
  const [ext, setExt] = useState<Ext>(".md");
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      // Preenche com o nome atual sem extensão
      const base = initialName.replace(/\.(md|txt)$/, "");
      setName(base || "");
      setErr(null);
      setTimeout(() => inputRef.current?.select(), 50);
    }
  }, [open, initialName]);

  const doSave = async () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    const filename = trimmed.endsWith(ext) ? trimmed : trimmed + ext;
    setSaving(true);
    setErr(null);
    try {
      await onSave(filename);
      onClose();
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "Erro ao salvar");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-sm w-full">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm">
            <FilePlus className="w-4 h-4" />
            Salvar como
          </DialogTitle>
        </DialogHeader>
        <div className="flex gap-2 mt-1">
          <Input
            ref={inputRef}
            className="h-8 text-xs flex-1"
            placeholder="Nome do arquivo..."
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") doSave(); if (e.key === "Escape") onClose(); }}
          />
          <select
            value={ext}
            onChange={(e) => setExt(e.target.value as Ext)}
            className="h-8 rounded-md border border-input bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          >
            {EXTENSIONS.map((e) => <option key={e} value={e}>{e}</option>)}
          </select>
        </div>
        {err && <p className="text-[11px] text-destructive">{err}</p>}
        <div className="flex justify-end gap-2 mt-2">
          <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onClose}>
            Cancelar
          </Button>
          <Button size="sm" className="h-8 text-xs gap-1.5" onClick={doSave} disabled={!name.trim() || saving}>
            {saving ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            Salvar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ── Template padrão ───────────────────────────────────────────────────────────

const DEFAULT_MARKDOWN = `# Título da Mensagem

Descreva o conteúdo principal aqui.

## Contexto

- Ponto importante 1
- Ponto importante 2

## Detalhes

| Campo    | Valor    | Status  |
|----------|----------|---------|
| Item 1   | Dado A   | ✅ OK   |
| Item 2   | Dado B   | ⚠️ Alerta |

> **Nota:** informação relevante aqui.
`;

// ── Componente principal ──────────────────────────────────────────────────────

export const TeamsBroadcastTab = () => {
  const [content, setContent] = useState(DEFAULT_MARKDOWN);
  const [isPlainText, setIsPlainText] = useState(false);
  const [selected, setSelected] = useState<Map<string, BroadcastChat>>(new Map());
  const [modalOpen, setModalOpen] = useState(false);
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const [saveAsOpen, setSaveAsOpen] = useState(false);
  const [currentFilename, setCurrentFilename] = useState<string | null>(null);
  const [isDirty, setIsDirty] = useState(false);
  const [sending, setSending] = useState(false);
  const [sendProgress, setSendProgress] = useState<{ done: number; total: number } | null>(null);
  const [sendStatus, setSendStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [copied, setCopied] = useState(false);

  // Refs para evitar stale closure no atalho Ctrl+S do Monaco
  const currentFilenameRef = useRef<string | null>(null);
  const contentRef = useRef(content);
  useEffect(() => { currentFilenameRef.current = currentFilename; }, [currentFilename]);
  useEffect(() => { contentRef.current = content; }, [content]);

  // Divisor editor ↔ preview
  const [editorPct, setEditorPct] = useState(50);
  const dragging = useRef(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<MonacoNS.editor.IStandaloneCodeEditor | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const saveToFilename = useCallback(async (filename: string) => {
    await apiClient.saveBroadcastTemplate(filename, contentRef.current);
    setCurrentFilename(filename);
    setIsDirty(false);
    toast.success(`Salvo: ${filename}`);
  }, []);

  const handleSaveRef = useRef<() => void>(null!);

  const handleSave = useCallback(() => {
    const fname = currentFilenameRef.current;
    if (!fname) {
      setSaveAsOpen(true);
      return;
    }
    saveToFilename(fname).catch(() => toast.error("Erro ao salvar"));
  }, [saveToFilename]);

  useEffect(() => { handleSaveRef.current = handleSave; }, [handleSave]);

  const handleMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS, () => {
      handleSaveRef.current?.();
    });
  };

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const pct = ((e.clientX - rect.left) / rect.width) * 100;
      setEditorPct(Math.max(25, Math.min(75, pct)));
    };
    const onUp = () => {
      if (!dragging.current) return;
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
  }, []);

  const applyFormat = (action: FormatAction) => {
    const editor = editorRef.current;
    if (!editor || isPlainText) return;
    const selection = editor.getSelection();
    const model = editor.getModel();
    if (!selection || !model) return;
    const selectedText = model.getValueInRange(selection);
    const { text } = action.action(selectedText);
    editor.executeEdits("toolbar", [{ range: selection, text }]);
    editor.focus();
  };

  const toggleChat = (chat: BroadcastChat) =>
    setSelected((prev) => {
      const next = new Map(prev);
      next.has(chat.id) ? next.delete(chat.id) : next.set(chat.id, chat);
      return next;
    });

  const selectManyChats = (chats: BroadcastChat[], select: boolean) =>
    setSelected((prev) => {
      const next = new Map(prev);
      chats.forEach(c => select ? next.set(c.id, c) : next.delete(c.id));
      return next;
    });

  const removeChat = (id: string) =>
    setSelected((prev) => { const n = new Map(prev); n.delete(id); return n; });

  const handleCopy = async () => {
    const html = previewRef.current?.innerHTML ?? "";
    const plain = previewRef.current?.innerText ?? content;
    try {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/html": new Blob([html], { type: "text/html" }),
          "text/plain": new Blob([plain], { type: "text/plain" }),
        }),
      ]);
    } catch {
      // fallback: copia só o texto sem formatação
      try {
        await navigator.clipboard.writeText(plain);
      } catch {
        const el = document.createElement("textarea");
        el.value = plain;
        document.body.appendChild(el);
        el.select();
        document.execCommand("copy");
        document.body.removeChild(el);
      }
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const [sendResults, setSendResults] = useState<Map<string, SendResult>>(new Map());
  const sseRef = useRef<EventSource | null>(null);

  const handleSend = async () => {
    if (selected.size === 0 || !content.trim()) return;

    const sessionId = crypto.randomUUID();
    const total = selected.size;
    const htmlContent = previewRef.current?.innerHTML ?? "";

    setSending(true);
    setSendStatus(null);
    setSendProgress({ done: 0, total });
    setSendResults(new Map());

    // Fechar SSE anterior se existir.
    sseRef.current?.close();

    // Conectar ao stream SSE antes de disparar o POST.
    const token = localStorage.getItem("auth_token") ?? "";
    const sse = new EventSource(
      `/api/v1/teams/broadcast/send/stream/${sessionId}?token=${encodeURIComponent(token)}`
    );
    sseRef.current = sse;

    sse.addEventListener("message", (e) => {
      try {
        const evt = JSON.parse(e.data) as {
          type: string; phase: string; message: string;
          progress: number; details: string; error: string;
        };

        if (evt.type === "broadcast_progress" && evt.details) {
          const r = JSON.parse(evt.details) as SendResult;
          setSendResults(prev => new Map(prev).set(r.thread_id, r));
          setSendProgress({ done: Math.round(evt.progress * total), total });
        }

        if (evt.type === "complete" && evt.details) {
          const final = JSON.parse(evt.details) as { sent: number; failed: number; results: SendResult[] };
          setSendStatus({
            ok: final.failed === 0,
            msg: final.failed === 0
              ? `Enviado para ${final.sent} chat(s)`
              : `${final.sent} enviados, ${final.failed} falharam`,
          });
          setSendProgress(null);
          setSending(false);
          sse.close();
        }

        if (evt.type === "error") {
          setSendStatus({ ok: false, msg: evt.error || evt.message });
          setSendProgress(null);
          setSending(false);
          sse.close();
        }
      } catch { /* ignorar eventos mal formados */ }
    });

    sse.onerror = () => {
      // SSE pode desconectar ao fechar o stream — ignorar se já concluído.
    };

    try {
      await apiClient.sendBroadcastMessage({
        session_id: sessionId,
        thread_ids: Array.from(selected.keys()),
        markdown: content,
        html: htmlContent,
      });
      // 202 retornado — aguardar eventos SSE para concluir.
    } catch (e: unknown) {
      setSendStatus({ ok: false, msg: e instanceof Error ? e.message : String(e) });
      setSendProgress(null);
      setSending(false);
      sse.close();
    }
  };

  const selectedList = Array.from(selected.values());

  return (
    <div className="flex flex-col h-full overflow-hidden bg-background">

      {/* ── Toolbar ── */}
      <div className="flex items-center gap-0.5 px-3 py-1.5 border-b border-border flex-shrink-0 bg-card flex-wrap">
        {!isPlainText && TOOLBAR.map((item, i) => {
          if (item === "sep") return <div key={`sep-${i}`} className="w-px h-4 bg-border/60 mx-1" />;
          return (
            <button
              key={item.title}
              title={item.title}
              onClick={() => applyFormat(item)}
              className={`
                inline-flex items-center justify-center gap-1 px-2 py-1 rounded text-xs
                text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors
                ${item.label ? "font-semibold min-w-[28px]" : "w-7 h-7"}
              `}
            >
              {item.icon}
              {item.label && <span>{item.label}</span>}
            </button>
          );
        })}

        <div className="flex-1" />

        {/* Indicador do arquivo atual */}
        {currentFilename && (
          <span className="text-[11px] font-mono text-muted-foreground max-w-[140px] truncate flex items-center gap-1" title={currentFilename}>
            {isDirty && <span className="text-amber-400 text-[13px] leading-none">●</span>}
            {currentFilename}
          </span>
        )}

        {/* Salvar (Ctrl+S) */}
        <button
          onClick={handleSave}
          title={currentFilename ? `Salvar ${currentFilename} (Ctrl+S)` : "Salvar como... (Ctrl+S)"}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors"
        >
          <Save className="w-3.5 h-3.5" />
          Salvar
        </button>

        {/* Salvar como */}
        <button
          onClick={() => setSaveAsOpen(true)}
          title="Salvar como novo arquivo..."
          className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors"
        >
          <FilePlus className="w-3.5 h-3.5" />
          Salvar como
        </button>

        <div className="w-px h-4 bg-border/60 mx-0.5" />

        <button
          onClick={() => setTemplatesOpen(true)}
          title="Abrir arquivo salvo"
          className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors"
        >
          <FolderOpen className="w-3.5 h-3.5" />
          Abrir
        </button>

        <div className="w-px h-4 bg-border/60 mx-1" />

        <button
          onClick={() => setIsPlainText((v) => !v)}
          title={isPlainText ? "Alternar para Markdown" : "Alternar para Texto Simples"}
          className={`
            inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium transition-colors
            ${isPlainText
              ? "bg-primary/15 text-primary border border-primary/30"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/60"
            }
          `}
        >
          {isPlainText
            ? <><Type className="w-3.5 h-3.5" />Texto simples</>
            : <><Code2 className="w-3.5 h-3.5" />Markdown</>}
        </button>
      </div>

      {/* ── Editor + Preview ── */}
      <div ref={containerRef} className="flex flex-1 min-h-0 overflow-hidden">

        {/* Editor */}
        <div
          className="flex flex-col min-h-0 overflow-hidden border-r border-border"
          style={{ width: `${editorPct}%` }}
        >
          <div className="flex items-center gap-2 px-3 py-1 border-b border-border/40 flex-shrink-0 bg-[#0D1117]">
            {isPlainText
              ? <Type className="w-3 h-3 text-muted-foreground" />
              : <Code2 className="w-3 h-3 text-muted-foreground" />}
            <span className="text-[11px] text-muted-foreground">
              {isPlainText ? "Texto simples" : "Markdown"}
            </span>
          </div>
          <div className="flex-1 min-h-0">
            <Editor
              height="100%"
              language={isPlainText ? "plaintext" : "markdown"}
              value={content}
              onChange={(v) => { setContent(v ?? ""); setIsDirty(true); }}
              theme="markdown-dark"
              beforeMount={beforeMount}
              onMount={handleMount}
              options={{
                minimap: { enabled: false },
                fontSize: 13,
                lineHeight: 22,
                wordWrap: "on",
                lineNumbers: "off",
                scrollBeyondLastLine: false,
                padding: { top: 12, bottom: 12 },
                renderLineHighlight: "none",
                overviewRulerLanes: 0,
                folding: false,
              }}
            />
          </div>
        </div>

        {/* Divisor */}
        <div
          className="w-1 flex-shrink-0 bg-border/40 hover:bg-primary/50 cursor-col-resize transition-colors"
          onMouseDown={(e) => {
            dragging.current = true;
            document.body.style.cursor = "col-resize";
            document.body.style.userSelect = "none";
            e.preventDefault();
          }}
        />

        {/* Preview */}
        <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
          <div className="flex items-center gap-2 px-3 py-1 border-b border-border/40 flex-shrink-0 bg-card">
            <Eye className="w-3 h-3 text-muted-foreground" />
            <span className="text-[11px] text-muted-foreground flex-1">Visualização</span>
            <button
              onClick={handleCopy}
              title="Copiar conteúdo"
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors"
            >
              {copied
                ? <><Check className="w-3 h-3 text-green-400" /><span className="text-green-400">Copiado!</span></>
                : <><Copy className="w-3 h-3" />Copiar</>}
            </button>
          </div>
          <div ref={previewRef} className="flex-1 overflow-y-auto p-5">
            {isPlainText ? (
              <pre className="text-sm text-slate-300 font-mono whitespace-pre-wrap leading-relaxed">
                {content}
              </pre>
            ) : (
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
                {content}
              </ReactMarkdown>
            )}
          </div>
        </div>
      </div>

      {/* ── Barra inferior ── */}
      <div className="flex-shrink-0 border-t border-border bg-card/80 px-3 py-2 flex items-center gap-2 min-h-0">

        {/* Botão destinatários */}
        <Button
          variant="outline"
          size="sm"
          className="h-8 text-xs gap-1.5 flex-shrink-0"
          onClick={() => setModalOpen(true)}
        >
          <Users className="w-3.5 h-3.5" />
          {selectedList.length > 0
            ? `${selectedList.length} destinatário${selectedList.length !== 1 ? "s" : ""}`
            : "Destinatários"}
        </Button>

        {/* Chips selecionados */}
        <div className="flex-1 flex flex-wrap gap-1 min-w-0 overflow-hidden">
          {selectedList.length === 0 && (
            <span className="text-xs text-muted-foreground self-center">
              Nenhum destinatário selecionado
            </span>
          )}
          {selectedList.map((chat) => (
            <Badge
              key={chat.id}
              variant="secondary"
              className="text-[10px] gap-1 pr-1 pl-2 h-5 cursor-default"
            >
              <span className="max-w-[160px] truncate">{chat.display_name}</span>
              <button
                onClick={() => removeChat(chat.id)}
                className="ml-0.5 rounded hover:bg-muted/60 p-0.5 flex-shrink-0"
              >
                <X className="w-2.5 h-2.5" />
              </button>
            </Badge>
          ))}
        </div>

        {/* Progress bar + Status + Enviar */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {sendProgress && (
            <div className="flex items-center gap-1.5 min-w-[120px]">
              <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                <div
                  className="h-full bg-primary transition-all duration-300 rounded-full"
                  style={{ width: `${(sendProgress.done / sendProgress.total) * 100}%` }}
                />
              </div>
              <span className="text-[10px] text-muted-foreground whitespace-nowrap">
                {sendProgress.done}/{sendProgress.total}
              </span>
            </div>
          )}
          {sendStatus && (
            <span className={`text-xs ${sendStatus.ok ? "text-green-400" : "text-destructive"}`}>
              {sendStatus.ok ? "✅" : "❌"} {sendStatus.msg}
            </span>
          )}
          <Button
            onClick={handleSend}
            disabled={selected.size === 0 || !content.trim() || sending}
            size="sm"
            className="h-8 text-xs gap-1.5"
          >
            {sending
              ? <RefreshCw className="w-3 h-3 animate-spin" />
              : <MessageSquarePlus className="w-3 h-3" />}
            {sending
              ? "Enviando..."
              : selected.size > 0
                ? `Enviar para ${selected.size}`
                : "Enviar"}
          </Button>
        </div>
      </div>

      {/* Resultados por destinatário — atualizado em tempo real via SSE */}
      {sendResults.size > 0 && (
        <div className="flex-shrink-0 border-t border-border bg-card/50 px-3 py-2 max-h-40 overflow-y-auto">
          <div className="text-[10px] text-muted-foreground mb-1 font-medium uppercase tracking-wide">
            Resultado do envio
          </div>
          <div className="flex flex-wrap gap-1.5">
            {Array.from(selected.values()).map((chat) => {
              const r = sendResults.get(chat.id);
              return (
                <span
                  key={chat.id}
                  title={r ? (r.error || (r.ok ? "Enviado" : `HTTP ${r.status}`)) : "Aguardando..."}
                  className={`inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border transition-colors ${
                    !r
                      ? "bg-muted/30 border-border text-muted-foreground"
                      : r.ok
                      ? "bg-green-950/40 border-green-800/50 text-green-400"
                      : "bg-red-950/40 border-red-800/50 text-red-400"
                  }`}
                >
                  {!r ? "⋯" : r.ok ? "✓" : "✗"}
                  {" "}
                  <span className="max-w-[120px] truncate">{chat.display_name}</span>
                  {r && !r.ok && r.status > 0 && <span className="opacity-60">{r.status}</span>}
                </span>
              );
            })}
          </div>
        </div>
      )}

      {/* ── Modal de destinatários ── */}
      <ChatSelectorModal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        selected={selected}
        onToggle={toggleChat}
        onSelectMany={selectManyChats}
      />

      {/* ── Modal de templates (abrir/gerenciar) ── */}
      <TemplatesModal
        open={templatesOpen}
        onClose={() => setTemplatesOpen(false)}
        currentContent={content}
        onLoad={(text, filename) => {
          setContent(text);
          setCurrentFilename(filename);
          setIsDirty(false);
        }}
      />

      {/* ── Modal Salvar Como ── */}
      <SaveAsModal
        open={saveAsOpen}
        initialName={currentFilename ?? ""}
        onClose={() => setSaveAsOpen(false)}
        onSave={saveToFilename}
      />
    </div>
  );
};
