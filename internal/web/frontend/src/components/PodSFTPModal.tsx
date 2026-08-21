import { useCallback, useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  FolderOpen,
  Folder,
  File,
  FolderPlus,
  Upload,
  Download,
  Pencil,
  Trash2,
  Loader2,
  ChevronRight,
  Home,
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { formatBytes } from "@/lib/monitorUtils";

interface SFTPFileEntry {
  name: string;
  path: string;
  size: number;
  is_dir: boolean;
  mod_time: string;
  mode: string;
}

interface PodSFTPModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  podName: string;
  containers: string[];
}

function authToken(): string {
  return localStorage.getItem("auth_token") ?? "";
}

async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) };
  const token = authToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const resp = await fetch(url, { ...init, headers });
  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    try {
      const body = await resp.json();
      msg = body?.error?.message || body?.error || msg;
    } catch {
      // corpo não era JSON — mantém a mensagem genérica
    }
    throw new Error(msg);
  }
  return resp;
}

function formatModTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("pt-BR");
  } catch {
    return iso;
  }
}

/**
 * PodSFTPModal — navegador de arquivos completo contra um pod, usando o servidor SFTP embutido
 * (internal/podsftp — protocolo SFTP real rodando in-process, sem expor porta de rede nenhuma;
 * ver SFTP-FILE-BROWSER-PLAN.md). Substitui "Transferir Arquivos" (PodFileTransferModal.tsx,
 * download-only) — pedido explícito do usuário: "servidor SFTP de verdade, mas com interface em
 * nossa aplicação" (sem precisar de WinSCP/FileZilla externo).
 */
export function PodSFTPModal({ open, onOpenChange, cluster, namespace, podName, containers }: PodSFTPModalProps) {
  const [container, setContainer] = useState(containers[0] ?? "");
  const [path, setPath] = useState("/");
  const [entries, setEntries] = useState<SFTPFileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [creatingFolder, setCreatingFolder] = useState(false);

  const [renaming, setRenaming] = useState<SFTPFileEntry | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [savingRename, setSavingRename] = useState(false);

  const [deleting, setDeleting] = useState<SFTPFileEntry | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const basePath = `/api/v1/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(podName)}/sftp`;

  const load = useCallback(async (targetPath: string) => {
    if (!container) return;
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ container, path: targetPath });
      const resp = await apiFetch(`${basePath}/list?${params.toString()}`);
      const data = await resp.json();
      const sorted: SFTPFileEntry[] = (data.entries ?? []).sort((a: SFTPFileEntry, b: SFTPFileEntry) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
        return a.name.localeCompare(b.name);
      });
      setEntries(sorted);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Erro ao listar diretório");
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, [basePath, container]);

  // BUG REAL CORRIGIDO — este efeito dependia de `containers` (o array inteiro), mas esse prop
  // é passado como `selectedPod.containers.map(c => c.name)` em PodsPanel.tsx — uma referência de
  // array NOVA a cada re-render do painel (inclusive os re-renders periódicos do polling da lista
  // de pods, sem nenhuma ação do usuário). Como um array recriado via .map() nunca é
  // referencialmente igual ao anterior mesmo com conteúdo idêntico, o efeito reexecutava a cada
  // poll, resetando `container` de volta pro primeiro (ex: "istio-proxy") e `path` de volta pra
  // "/" — mesmo com o usuário já tendo trocado de container ou navegado pra uma subpasta.
  // Corrigido dependendo só de `open` (primitivo estável): a lista de containers de um pod nunca
  // muda de verdade durante a mesma sessão do modal, então não há necessidade de reagir a ela.
  useEffect(() => {
    if (!open) return;
    setContainer(containers[0] ?? "");
    setPath("/");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (open && container) load(path);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, container, path]);

  const navigateTo = (p: string) => setPath(p || "/");

  const breadcrumbSegments = path === "/" ? [] : path.split("/").filter(Boolean);

  const handleEntryClick = (entry: SFTPFileEntry) => {
    if (entry.is_dir) navigateTo(entry.path);
  };

  const handleUploadClick = () => fileInputRef.current?.click();

  const handleFileSelected = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // permite selecionar o mesmo arquivo de novo depois
    if (!file) return;

    setUploading(true);
    try {
      const remotePath = path === "/" ? `/${file.name}` : `${path}/${file.name}`;
      const params = new URLSearchParams({ container, path: remotePath });
      const form = new FormData();
      form.append("file", file);
      await apiFetch(`${basePath}/upload?${params.toString()}`, { method: "POST", body: form });
      toast.success(`Enviado: ${file.name}`);
      load(path);
    } catch (e) {
      toast.error("Erro ao enviar arquivo", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setUploading(false);
    }
  };

  const handleDownload = async (entry: SFTPFileEntry) => {
    try {
      const params = new URLSearchParams({ container, path: entry.path });
      const resp = await apiFetch(`${basePath}/download?${params.toString()}`);
      const blob = await resp.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = entry.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);
    } catch (e) {
      toast.error("Erro ao baixar arquivo", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    }
  };

  const handleCreateFolder = async () => {
    if (!newFolderName.trim()) return;
    setCreatingFolder(true);
    try {
      const folderPath = path === "/" ? `/${newFolderName.trim()}` : `${path}/${newFolderName.trim()}`;
      const params = new URLSearchParams({ container });
      await apiFetch(`${basePath}/mkdir?${params.toString()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: folderPath }),
      });
      toast.success(`Pasta criada: ${newFolderName.trim()}`);
      setNewFolderOpen(false);
      setNewFolderName("");
      load(path);
    } catch (e) {
      toast.error("Erro ao criar pasta", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setCreatingFolder(false);
    }
  };

  const openRename = (entry: SFTPFileEntry) => {
    setRenaming(entry);
    setRenameValue(entry.name);
  };

  const handleRename = async () => {
    if (!renaming || !renameValue.trim() || renameValue.trim() === renaming.name) {
      setRenaming(null);
      return;
    }
    setSavingRename(true);
    try {
      const dir = path === "/" ? "" : path;
      const newPath = `${dir}/${renameValue.trim()}`;
      const params = new URLSearchParams({ container });
      await apiFetch(`${basePath}/rename?${params.toString()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ old_path: renaming.path, new_path: newPath }),
      });
      toast.success(`Renomeado para: ${renameValue.trim()}`);
      setRenaming(null);
      load(path);
    } catch (e) {
      toast.error("Erro ao renomear", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setSavingRename(false);
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleting) return;
    setConfirmingDelete(true);
    try {
      const params = new URLSearchParams({ container, path: deleting.path, is_dir: String(deleting.is_dir) });
      await apiFetch(`${basePath}/remove?${params.toString()}`, { method: "DELETE" });
      toast.success(`Removido: ${deleting.name}`);
      setDeleting(null);
      load(path);
    } catch (e) {
      toast.error("Erro ao remover", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setConfirmingDelete(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-3xl h-[80vh] flex flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <FolderOpen className="h-4 w-4" />
              Arquivos — {namespace}/{podName}
            </DialogTitle>
            <DialogDescription>
              Navegue, envie e baixe arquivos direto do pod via SFTP — sem precisar de nenhum cliente externo.
            </DialogDescription>
          </DialogHeader>

          <div className="flex-shrink-0 flex items-center gap-2 flex-wrap">
            {containers.length > 1 && (
              <Select value={container} onValueChange={(v) => setContainer(v)}>
                <SelectTrigger className="h-8 w-40 text-xs flex-shrink-0">
                  <SelectValue placeholder="Container" />
                </SelectTrigger>
                <SelectContent>
                  {containers.map((c) => (
                    <SelectItem key={c} value={c} className="text-xs">{c}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}

            <div className="flex items-center gap-1 text-xs flex-1 min-w-0 overflow-x-auto whitespace-nowrap">
              <button className="p-1 rounded hover:bg-accent flex-shrink-0" onClick={() => navigateTo("/")} title="Raiz">
                <Home className="h-3.5 w-3.5" />
              </button>
              {breadcrumbSegments.map((seg, i) => {
                const segPath = "/" + breadcrumbSegments.slice(0, i + 1).join("/");
                return (
                  <span key={segPath} className="flex items-center gap-1 flex-shrink-0">
                    <ChevronRight className="h-3 w-3 text-muted-foreground" />
                    <button className="hover:underline" onClick={() => navigateTo(segPath)}>{seg}</button>
                  </span>
                );
              })}
            </div>

            <Button variant="ghost" size="icon" className="h-8 w-8 flex-shrink-0" onClick={() => load(path)} title="Atualizar" disabled={loading}>
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>

            <ProtectedAction>
              <Button variant="outline" size="sm" className="h-8 text-xs gap-1 flex-shrink-0" onClick={() => setNewFolderOpen(true)}>
                <FolderPlus className="h-3.5 w-3.5" /> Nova pasta
              </Button>
            </ProtectedAction>
            <ProtectedAction>
              <Button variant="outline" size="sm" className="h-8 text-xs gap-1 flex-shrink-0" onClick={handleUploadClick} disabled={uploading}>
                {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                Enviar
              </Button>
            </ProtectedAction>
            <input ref={fileInputRef} type="file" className="hidden" onChange={handleFileSelected} />
          </div>

          <div className="flex-1 min-h-0 overflow-hidden border rounded-lg">
            {error ? (
              <div className="flex flex-col items-center justify-center h-full gap-2 text-center px-4">
                <p className="text-sm text-destructive">{error}</p>
                <Button variant="outline" size="sm" onClick={() => load(path)}>Tentar de novo</Button>
              </div>
            ) : loading && entries.length === 0 ? (
              <div className="flex items-center justify-center h-full gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
              </div>
            ) : entries.length === 0 ? (
              <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                Pasta vazia.
              </div>
            ) : (
              <ScrollArea className="h-full">
                <div className="divide-y divide-border/50">
                  {entries.map((entry) => (
                    <div
                      key={entry.path}
                      className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent/50 transition-colors group"
                    >
                      <button
                        className="flex items-center gap-2 flex-1 min-w-0 text-left"
                        onClick={() => handleEntryClick(entry)}
                        disabled={!entry.is_dir}
                      >
                        {entry.is_dir ? (
                          <Folder className="h-4 w-4 text-blue-400 flex-shrink-0" />
                        ) : (
                          <File className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                        )}
                        <span className={`truncate ${entry.is_dir ? "font-medium" : ""}`}>{entry.name}</span>
                      </button>
                      <span className="text-xs text-muted-foreground w-20 text-right flex-shrink-0">
                        {entry.is_dir ? "" : formatBytes(entry.size)}
                      </span>
                      <span className="text-xs text-muted-foreground w-36 text-right flex-shrink-0 hidden sm:block">
                        {formatModTime(entry.mod_time)}
                      </span>
                      <div className="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                        {!entry.is_dir && (
                          <Button variant="ghost" size="icon" className="h-7 w-7" title="Baixar" onClick={() => handleDownload(entry)}>
                            <Download className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <ProtectedAction>
                          <Button variant="ghost" size="icon" className="h-7 w-7" title="Renomear" onClick={() => openRename(entry)}>
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                        </ProtectedAction>
                        <ProtectedAction>
                          <Button
                            variant="ghost" size="icon"
                            className="h-7 w-7 text-destructive hover:text-destructive"
                            title="Excluir"
                            onClick={() => setDeleting(entry)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </ProtectedAction>
                      </div>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* Nova pasta */}
      <Dialog open={newFolderOpen} onOpenChange={(v) => { if (!creatingFolder) setNewFolderOpen(v); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Nova pasta</DialogTitle>
          </DialogHeader>
          <Input
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            placeholder="nome-da-pasta"
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter") handleCreateFolder(); }}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" size="sm" onClick={() => setNewFolderOpen(false)} disabled={creatingFolder}>Cancelar</Button>
            <Button size="sm" onClick={handleCreateFolder} disabled={!newFolderName.trim() || creatingFolder}>
              {creatingFolder && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
              Criar
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Renomear */}
      <Dialog open={!!renaming} onOpenChange={(v) => { if (!v && !savingRename) setRenaming(null); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Renomear</DialogTitle>
          </DialogHeader>
          <Input
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter") handleRename(); }}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" size="sm" onClick={() => setRenaming(null)} disabled={savingRename}>Cancelar</Button>
            <Button size="sm" onClick={handleRename} disabled={!renameValue.trim() || savingRename}>
              {savingRename && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
              Salvar
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Confirmação de exclusão */}
      <AlertDialog open={!!deleting} onOpenChange={(v) => { if (!v && !confirmingDelete) setDeleting(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar Exclusão</AlertDialogTitle>
            <AlertDialogDescription>
              Tem certeza que deseja excluir {deleting?.is_dir ? "a pasta" : "o arquivo"}{" "}
              <strong>{deleting?.name}</strong>
              {deleting?.is_dir && " e todo o seu conteúdo"}? Esta ação não pode ser desfeita.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={confirmingDelete}>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmDelete} disabled={confirmingDelete} className="bg-destructive hover:bg-destructive/90">
              {confirmingDelete && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Excluir
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
