import { useEffect, useRef, useState } from "react";
import { ArrowUp, FileText, Folder, FolderGit2, FolderOpen, HardDrive, Home, Loader2, Search } from "lucide-react";
import { apiClient, type CodeEditorBrowseResult, type CodeEditorRepo } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

// "Abrir pasta" do Code Editor: navega pelas pastas da máquina onde o servidor roda (no WSL,
// inclusive as do Windows em /mnt/c) e registra a escolhida como item do editor — sem clonar.
// O campo aceita caminho Linux, ~/..., C:\... e \\wsl$\... (convertidos no backend).
// Também lista os arquivos da pasta: duplo clique num arquivo abre a pasta dele já com o
// arquivo aberto no editor.

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
// Busca por trechos: o texto vira termos (separados por espaço) e cada um precisa aparecer no
// nome, em qualquer ordem. Sem diferenciar maiúsculas e acentos; um termo também casa ignorando
// os separadores - _ . ("iacsre" acha "casas-bahia-iac-srelogistica").
const normalize = (s: string) => s.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase();
const compact = (s: string) => s.replace(/[-_.\s]+/g, "");

function searchTerms(filter: string): string[] {
  return normalize(filter).split(/\s+/).filter(Boolean);
}

function matchesTerms(name: string, terms: string[]): boolean {
  if (terms.length === 0) return true;
  const n = normalize(name);
  const c = compact(n);
  return terms.every((t) => n.includes(t) || c.includes(compact(t)));
}

// Destaca no nome os trechos que casam literalmente (o casamento "sem separadores" não é marcado).
function HighlightName({ name, terms }: { name: string; terms: string[] }) {
  if (terms.length === 0) return <>{name}</>;
  const n = normalize(name); // NFD + remoção de acentos preserva os índices só sem acento composto
  const marks = new Array<boolean>(name.length).fill(false);
  if (n.length === name.length) {
    for (const t of terms) {
      for (let i = n.indexOf(t); i !== -1; i = n.indexOf(t, i + 1)) marks.fill(true, i, i + t.length);
    }
  }
  const parts: React.ReactNode[] = [];
  let start = 0;
  for (let i = 1; i <= name.length; i++) {
    if (i === name.length || marks[i] !== marks[start]) {
      const text = name.slice(start, i);
      parts.push(
        marks[start] ? (
          <mark key={start} className="bg-yellow-300/70 dark:bg-yellow-500/50 text-inherit rounded-sm">
            {text}
          </mark>
        ) : (
          text
        )
      );
      start = i;
    }
  }
  return <>{parts}</>;
}

// Autocomplete do campo de caminho: o que vem depois da última / (ou \\) é o trecho buscado
// dentro da pasta antes dela. Sem barra nenhuma, busca na pasta que está aberta no modal.
function splitPath(value: string): { parent: string | null; partial: string } {
  const i = Math.max(value.lastIndexOf("/"), value.lastIndexOf("\\"));
  if (i === -1) return { parent: null, partial: value };
  return { parent: value.slice(0, i + 1) || "/", partial: value.slice(i + 1) };
}

const MAX_SUGGESTIONS = 50;

type Suggestion = { name: string; path: string; kind: "dir" | "file"; is_git?: boolean; dir: string };

export function CodeEditorOpenFolderDialog({
  open,
  onClose,
  onOpened,
}: {
  open: boolean;
  onClose: () => void;
  // file: caminho relativo à pasta aberta, quando o usuário escolheu um arquivo
  onOpened: (repo: CodeEditorRepo, file?: string) => void;
}) {
  const [input, setInput] = useState("");
  const [result, setResult] = useState<CodeEditorBrowseResult | null>(null);
  const [showHidden, setShowHidden] = useState(false);
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(false);
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState("");
  // Autocomplete do campo de caminho.
  const [suggestOpen, setSuggestOpen] = useState(false);
  const [suggestions, setSuggestions] = useState<Suggestion[]>([]);
  const [activeIdx, setActiveIdx] = useState(0);
  const [suggestTerms, setSuggestTerms] = useState<string[]>([]);
  const listingCache = useRef(new Map<string, CodeEditorBrowseResult>());
  const inputRef = useRef<HTMLInputElement>(null);

  async function browse(path: string, hidden = showHidden) {
    setLoading(true);
    setError("");
    try {
      const r = await apiClient.codeEditorBrowse(path, hidden);
      setResult(r);
      setInput(r.path);
      setFilter("");
      listingCache.current.set(`${r.path}|${hidden}`, r);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (open) {
      setResult(null);
      setError("");
      setSuggestOpen(false);
      listingCache.current.clear();
      browse(localStorage.getItem("ce_last_open_folder_parent") ?? "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  async function openFolder(path: string, file?: string) {
    setOpening(true);
    setError("");
    try {
      const repo = await apiClient.codeEditorOpenFolder(path);
      const parent = repo.local_path.split("/").slice(0, -1).join("/") || "/";
      localStorage.setItem("ce_last_open_folder_parent", parent);
      onOpened(repo, file);
      onClose();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setOpening(false);
    }
  }

  // Sugestões: lista a pasta-pai do que foi digitado (com cache) e filtra pelo trecho final.
  useEffect(() => {
    if (!open || !suggestOpen) return;
    const { parent, partial } = splitPath(input);
    const dir = parent ?? result?.path;
    if (!dir) return;
    const t = searchTerms(partial);
    let cancelled = false;
    const timer = setTimeout(async () => {
      const key = `${dir}|${showHidden}`;
      let listing = listingCache.current.get(key);
      if (!listing) {
        try {
          listing = await apiClient.codeEditorBrowse(dir, showHidden);
          listingCache.current.set(key, listing);
        } catch {
          if (!cancelled) setSuggestions([]); // pasta inexistente enquanto digita: só não sugere
          return;
        }
      }
      if (cancelled) return;
      const first = t[0] ?? "";
      const byPrefix = (a: Suggestion, b: Suggestion) =>
        Number(!normalize(a.name).startsWith(first)) - Number(!normalize(b.name).startsWith(first));
      const dirs: Suggestion[] = listing.dirs
        .filter((d) => matchesTerms(d.name, t))
        .map((d) => ({ name: d.name, path: d.path, kind: "dir", is_git: d.is_git, dir: listing!.path }));
      const files: Suggestion[] = listing.files
        .filter((x) => matchesTerms(x.name, t))
        .map((x) => ({ name: x.name, path: x.path, kind: "file", dir: listing!.path }));
      setSuggestions([...dirs.sort(byPrefix), ...files.sort(byPrefix)].slice(0, MAX_SUGGESTIONS));
      setSuggestTerms(t);
      setActiveIdx(0);
    }, 120);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [input, suggestOpen, open, showHidden, result?.path]);

  async function acceptSuggestion(sg: Suggestion) {
    if (sg.kind === "file") {
      setSuggestOpen(false);
      openFolder(sg.dir, sg.name);
      return;
    }
    await browse(sg.path);
    // Continua completando dentro da pasta escolhida.
    setInput(sg.path.endsWith("/") ? sg.path : `${sg.path}/`);
    setSuggestOpen(true);
    inputRef.current?.focus();
  }

  function onPathKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    const hasList = suggestOpen && suggestions.length > 0;
    if (e.key === "ArrowDown" && hasList) {
      e.preventDefault();
      setActiveIdx((i) => (i + 1) % suggestions.length);
    } else if (e.key === "ArrowUp" && hasList) {
      e.preventDefault();
      setActiveIdx((i) => (i - 1 + suggestions.length) % suggestions.length);
    } else if (e.key === "Tab" && hasList) {
      e.preventDefault();
      acceptSuggestion(suggestions[activeIdx]);
    } else if (e.key === "Enter" && hasList && splitPath(input).partial.trim() !== "") {
      // Com trecho digitado, Enter completa; com o caminho terminando em /, Enter navega (form).
      e.preventDefault();
      acceptSuggestion(suggestions[activeIdx]);
    }
  }

  const terms = searchTerms(filter);
  const q = terms.length > 0;
  const dirs = (result?.dirs ?? []).filter((d) => matchesTerms(d.name, terms));
  const files = (result?.files ?? []).filter((f) => matchesTerms(f.name, terms));

  return (
    <Dialog open={open} onOpenChange={(v) => !v && !opening && onClose()}>
      <DialogContent
        className="max-w-2xl"
        onEscapeKeyDown={(e) => {
          // Com a lista do autocomplete aberta, Esc fecha só a lista.
          if (suggestOpen && suggestions.length > 0) {
            e.preventDefault();
            setSuggestOpen(false);
          }
        }}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="w-4 h-4" />
            Abrir pasta
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3 min-w-0">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setSuggestOpen(false);
              browse(input);
            }}
          >
            <div className="relative flex-1 min-w-0">
              <Input
                ref={inputRef}
                value={input}
                onChange={(e) => {
                  setInput(e.target.value);
                  setSuggestOpen(true);
                }}
                onKeyDown={onPathKeyDown}
                onBlur={() => setSuggestOpen(false)}
                placeholder={String.raw`Digite para completar: /home/usuario/scr  ·  ~/repos/iac prd  ·  C:\Users\usuario`}
                className="font-mono text-xs h-8"
                autoComplete="off"
                spellCheck={false}
              />
              {suggestOpen && suggestions.length > 0 && (
                <div className="absolute left-0 right-0 top-full mt-1 z-50 max-h-64 overflow-y-auto rounded-md border bg-popover shadow-lg p-1">
                  {suggestions.map((sg, i) => (
                    <button
                      key={sg.path}
                      type="button"
                      // mousedown + preventDefault: escolhe antes do blur do input fechar a lista
                      onMouseDown={(e) => {
                        e.preventDefault();
                        acceptSuggestion(sg);
                      }}
                      onMouseEnter={() => setActiveIdx(i)}
                      className={`w-full flex items-center gap-2 px-2 py-1 text-xs rounded text-left ${i === activeIdx ? "bg-muted" : ""}`}
                    >
                      {sg.kind === "file" ? (
                        <FileText className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                      ) : sg.is_git ? (
                        <FolderGit2 className="w-3.5 h-3.5 text-orange-400 flex-shrink-0" />
                      ) : (
                        <Folder className="w-3.5 h-3.5 text-blue-400 flex-shrink-0" />
                      )}
                      <span className="truncate font-mono">
                        <HighlightName name={sg.name} terms={suggestTerms} />
                        {sg.kind === "dir" ? "/" : ""}
                      </span>
                    </button>
                  ))}
                  <p className="px-2 pt-1 text-[10px] text-muted-foreground border-t mt-1">
                    ↑/↓ escolhe · Tab ou Enter completa · arquivo abre a pasta com ele · Esc fecha
                  </p>
                </div>
              )}
            </div>
            <Button type="submit" variant="outline" size="sm" className="h-8" disabled={loading}>
              Ir
            </Button>
          </form>

          <div className="flex items-center gap-1.5 flex-wrap">
            {result?.shortcuts.map((s) => (
              <Button key={s.path} variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => browse(s.path)}>
                {s.label === "Home" ? <Home className="w-3 h-3" /> : <HardDrive className="w-3 h-3" />}
                {s.label}
              </Button>
            ))}
            <label className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
              <input
                type="checkbox"
                checked={showHidden}
                onChange={(e) => {
                  setShowHidden(e.target.checked);
                  if (result) browse(result.path, e.target.checked);
                }}
              />
              Mostrar ocultas
            </label>
          </div>

          <div className="border rounded-md">
            <div className="flex items-center gap-2 px-2 py-1.5 border-b bg-muted/30">
              <Button
                variant="ghost"
                size="sm"
                className="h-6 w-6 p-0"
                title="Pasta acima"
                disabled={!result?.parent || loading}
                onClick={() => result?.parent && browse(result.parent)}
              >
                <ArrowUp className="w-3.5 h-3.5" />
              </Button>
              <span className="font-mono text-xs truncate flex-1">{result?.path ?? "…"}</span>
              {loading && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
            </div>
            {result && (
              <div className="flex items-center gap-2 px-2 py-1 border-b">
                <Search className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                <input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Buscar por trechos do nome nesta pasta (ex.: iac prd)..."
                  className="flex-1 bg-transparent text-xs py-0.5 focus:outline-none placeholder:text-muted-foreground"
                />
                <span className="text-[10px] text-muted-foreground flex-shrink-0">
                  {dirs.length} pasta{dirs.length === 1 ? "" : "s"} · {files.length} arquivo{files.length === 1 ? "" : "s"}
                </span>
              </div>
            )}
            <ScrollArea className="h-72">
              <div className="p-1">
                {result && dirs.length === 0 && files.length === 0 && (
                  <p className="text-xs text-muted-foreground text-center py-6">
                    {q ? "Nada encontrado nesta pasta" : "Pasta vazia"}
                  </p>
                )}
                {dirs.map((d) => (
                  <button
                    key={d.path}
                    className="w-full flex items-center gap-2 px-2 py-1 text-xs rounded hover:bg-muted/60 text-left"
                    onClick={() => browse(d.path)}
                    onDoubleClick={() => openFolder(d.path)}
                    title="Clique para entrar · duplo clique para abrir"
                  >
                    {d.is_git ? (
                      <FolderGit2 className="w-3.5 h-3.5 text-orange-400 flex-shrink-0" />
                    ) : (
                      <Folder className="w-3.5 h-3.5 text-blue-400 flex-shrink-0" />
                    )}
                    <span className="truncate">
                      <HighlightName name={d.name} terms={terms} />
                    </span>
                  </button>
                ))}
                {files.map((f) => (
                  <button
                    key={f.path}
                    className="w-full flex items-center gap-2 px-2 py-1 text-xs rounded hover:bg-muted/60 text-left disabled:opacity-50"
                    disabled={opening}
                    onDoubleClick={() => result && openFolder(result.path, f.name)}
                    title="Duplo clique para abrir esta pasta no editor com o arquivo aberto"
                  >
                    <FileText className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                    <span className="truncate flex-1">
                      <HighlightName name={f.name} terms={terms} />
                    </span>
                    <span className="text-[10px] text-muted-foreground flex-shrink-0">{formatSize(f.size)}</span>
                  </button>
                ))}
              </div>
            </ScrollArea>
          </div>

          {error && <p className="text-xs text-red-400">{error}</p>}
          <p className="text-[11px] text-muted-foreground">
            Os arquivos são editados no lugar, sem cópia. Pastas do Windows ficam em <span className="font-mono">/mnt/c/…</span> — você
            também pode colar o caminho no formato <span className="font-mono">C:\…</span>. Fechar a pasta no editor não apaga nada.
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={opening}>
            Cancelar
          </Button>
          <Button onClick={() => result && openFolder(result.path)} disabled={!result || opening || loading}>
            {opening ? <Loader2 className="w-3 h-3 animate-spin mr-1" /> : <FolderOpen className="w-3.5 h-3.5 mr-1" />}
            Abrir esta pasta
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
