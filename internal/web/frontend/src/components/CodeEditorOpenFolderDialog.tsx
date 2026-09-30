import { useEffect, useState } from "react";
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

  async function browse(path: string, hidden = showHidden) {
    setLoading(true);
    setError("");
    try {
      const r = await apiClient.codeEditorBrowse(path, hidden);
      setResult(r);
      setInput(r.path);
      setFilter("");
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

  const q = filter.trim().toLowerCase();
  const dirs = (result?.dirs ?? []).filter((d) => !q || d.name.toLowerCase().includes(q));
  const files = (result?.files ?? []).filter((f) => !q || f.name.toLowerCase().includes(q));

  return (
    <Dialog open={open} onOpenChange={(v) => !v && !opening && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="w-4 h-4" />
            Abrir pasta
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              browse(input);
            }}
          >
            <Input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={String.raw`/home/usuario/scripts  ·  ~/scripts  ·  C:\Users\usuario\scripts`}
              className="font-mono text-xs h-8"
            />
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
                  placeholder="Buscar pastas e arquivos nesta pasta..."
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
                    <span className="truncate">{d.name}</span>
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
                    <span className="truncate flex-1">{f.name}</span>
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
