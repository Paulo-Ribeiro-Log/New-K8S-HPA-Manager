import { useEffect, useMemo, useState } from "react";
import Editor from "@monaco-editor/react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Copy, Download, Loader2, FileArchive, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { PodArchiveCandidate, PodArchiveEntry } from "@/lib/api/types";
import { formatBytes } from "@/lib/monitorUtils";

// PodArchiveExtractModal — extrator genérico de arquivos empacotados dentro de um .jar/.war/.zip
// num container de pod. Motivado por um caso real: aplicações Spring Boot desta empresa (chart
// convair-helm) frequentemente não expõem o application.yml via ConfigMap — o arquivo vem
// compilado dentro do próprio jar (BOOT-INF/classes/application.yaml). Deliberadamente genérico
// (não hardcoded pra "application.yml") — qualquer entrada de texto de qualquer .jar/.war/.zip
// encontrado no pod pode ser visualizada aqui. Ver internal/web/handlers/pod_archive_extract.go.

interface PodArchiveExtractModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  podName: string;
  containers: string[];
}

// monacoLanguageForEntry deriva a linguagem do Monaco a partir da extensão do nome da entrada —
// mesmo padrão do CodeEditorTab.tsx (detecção por extensão, sem MonacoYamlEditor.tsx, que força
// language="yaml" fixo e não serve aqui: o conteúdo pode ser properties/xml/json/texto puro).
function monacoLanguageForEntry(name: string): string {
  const lower = name.toLowerCase();
  if (lower.endsWith(".yml") || lower.endsWith(".yaml")) return "yaml";
  if (lower.endsWith(".json")) return "json";
  if (lower.endsWith(".xml") || lower.endsWith(".wsdl") || lower.endsWith(".xsd")) return "xml";
  if (lower.endsWith(".properties")) return "ini";
  if (lower.endsWith(".sql")) return "sql";
  if (lower.endsWith(".sh")) return "shell";
  if (lower.endsWith(".md")) return "markdown";
  if (lower.endsWith(".manifest") || lower === "manifest.mf" || lower.endsWith("/manifest.mf")) return "ini";
  return "plaintext";
}

function entryDisplayName(path: string): string {
  const parts = path.split("/");
  return parts[parts.length - 1] || path;
}

export function PodArchiveExtractModal({
  open,
  onOpenChange,
  cluster,
  namespace,
  podName,
  containers,
}: PodArchiveExtractModalProps) {
  const [container, setContainer] = useState(containers[0] ?? "");
  const [archives, setArchives] = useState<PodArchiveCandidate[]>([]);
  const [archivesLoading, setArchivesLoading] = useState(false);
  const [archivesError, setArchivesError] = useState<string | null>(null);
  const [selectedArchive, setSelectedArchive] = useState<string>("");

  const [entries, setEntries] = useState<PodArchiveEntry[]>([]);
  const [entriesLoading, setEntriesLoading] = useState(false);
  const [entriesError, setEntriesError] = useState<string | null>(null);
  const [entrySearch, setEntrySearch] = useState("");
  const [selectedEntry, setSelectedEntry] = useState<string>("");

  const [content, setContent] = useState<string>("");
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setContainer(containers[0] ?? "");
    setArchives([]);
    setSelectedArchive("");
    setEntries([]);
    setSelectedEntry("");
    setEntrySearch("");
    setContent("");
    setArchivesError(null);
    setEntriesError(null);
    setContentError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, podName, namespace, cluster]);

  const loadArchives = async (targetContainer: string) => {
    if (!targetContainer) return;
    setArchivesLoading(true);
    setArchivesError(null);
    setSelectedArchive("");
    setEntries([]);
    setSelectedEntry("");
    setContent("");
    try {
      const res = await apiClient.getPodArchives(cluster, namespace, podName, targetContainer);
      setArchives(res.archives || []);
      if ((res.archives || []).length > 0) {
        setSelectedArchive(res.archives[0].path);
      }
    } catch (e) {
      setArchivesError(e instanceof Error ? e.message : String(e));
    } finally {
      setArchivesLoading(false);
    }
  };

  useEffect(() => {
    if (open && container) {
      loadArchives(container);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, container]);

  useEffect(() => {
    if (!selectedArchive || !container) {
      setEntries([]);
      return;
    }
    let cancelled = false;
    setEntriesLoading(true);
    setEntriesError(null);
    setSelectedEntry("");
    setContent("");
    apiClient
      .getPodArchiveEntries(cluster, namespace, podName, container, selectedArchive)
      .then((res) => {
        if (cancelled) return;
        setEntries(res.entries || []);
      })
      .catch((e) => {
        if (cancelled) return;
        setEntriesError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setEntriesLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedArchive]);

  const filteredEntries = useMemo(() => {
    if (!entrySearch.trim()) return entries;
    const q = entrySearch.toLowerCase();
    return entries.filter((e) => e.name.toLowerCase().includes(q));
  }, [entries, entrySearch]);

  const handleSelectEntry = async (entryName: string) => {
    setSelectedEntry(entryName);
    setContentLoading(true);
    setContentError(null);
    setContent("");
    try {
      const res = await apiClient.getPodArchiveContent(cluster, namespace, podName, container, selectedArchive, entryName);
      setContent(res.content);
    } catch (e) {
      setContentError(e instanceof Error ? e.message : String(e));
    } finally {
      setContentLoading(false);
    }
  };

  const handleCopy = () => {
    if (!content) return;
    navigator.clipboard.writeText(content);
    toast.success("Conteúdo copiado");
  };

  const handleDownload = () => {
    if (!content || !selectedEntry) return;
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = entryDisplayName(selectedEntry);
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl h-[80vh] flex flex-col overflow-hidden">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <FileArchive className="w-5 h-5" />
            Extrair de .jar/.war/.zip
          </DialogTitle>
          <DialogDescription>
            Visualiza qualquer arquivo de texto empacotado dentro de um .jar/.war/.zip do container — útil pra
            aplicações (ex: Spring Boot) cujo <code>application.yml</code> vem compilado no jar, sem ConfigMap.
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-end gap-3 flex-shrink-0">
          {containers.length > 1 && (
            <div className="w-52">
              <label className="text-xs text-muted-foreground block mb-1">Container</label>
              <Select value={container} onValueChange={setContainer}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {containers.map((c) => (
                    <SelectItem key={c} value={c}>{c}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="flex-1 min-w-0">
            <label className="text-xs text-muted-foreground block mb-1">
              Arquivo (.jar/.war/.zip) — {archivesLoading ? "buscando..." : `${archives.length} encontrado(s)`}
            </label>
            <Select value={selectedArchive} onValueChange={setSelectedArchive} disabled={archivesLoading || archives.length === 0}>
              <SelectTrigger>
                <SelectValue placeholder={archivesLoading ? "Buscando arquivos no container..." : "Nenhum arquivo encontrado"} />
              </SelectTrigger>
              <SelectContent>
                {archives.map((a) => (
                  <SelectItem key={a.path} value={a.path}>
                    {a.path} {a.size_bytes >= 0 ? `(${formatBytes(a.size_bytes)})` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Button variant="outline" size="icon" onClick={() => loadArchives(container)} disabled={archivesLoading} title="Buscar arquivos novamente">
            {archivesLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
          </Button>
        </div>

        {archivesError && (
          <p className="text-xs text-destructive flex-shrink-0">{archivesError}</p>
        )}

        <div className="flex-1 min-h-0 flex gap-3">
          <div className="w-72 flex-shrink-0 flex flex-col border border-border rounded-md overflow-hidden">
            <div className="p-2 border-b border-border flex-shrink-0">
              <Input
                placeholder="Buscar entrada..."
                value={entrySearch}
                onChange={(e) => setEntrySearch(e.target.value)}
                className="h-8 text-sm"
                disabled={entries.length === 0}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                {entriesLoading ? "listando..." : `${filteredEntries.length} de ${entries.length} entrada(s)`}
              </p>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto">
              {entriesError && <p className="text-xs text-destructive p-2">{entriesError}</p>}
              {!entriesLoading && !entriesError && filteredEntries.length === 0 && entries.length > 0 && (
                <p className="text-xs text-muted-foreground p-2">Nenhuma entrada bate com a busca.</p>
              )}
              {filteredEntries.map((entry) => (
                <button
                  key={entry.name}
                  onClick={() => handleSelectEntry(entry.name)}
                  className={`w-full text-left px-2 py-1.5 text-xs border-b border-border/50 hover:bg-muted truncate ${
                    selectedEntry === entry.name ? "bg-muted font-medium" : ""
                  }`}
                  title={entry.name}
                >
                  {entry.name}
                  {entry.size_bytes >= 0 && (
                    <span className="text-muted-foreground ml-1">({formatBytes(entry.size_bytes)})</span>
                  )}
                </button>
              ))}
            </div>
          </div>

          <div className="flex-1 min-w-0 flex flex-col border border-border rounded-md overflow-hidden">
            <div className="p-2 border-b border-border flex items-center justify-between flex-shrink-0">
              <span className="text-xs text-muted-foreground truncate">
                {selectedEntry || "Selecione uma entrada à esquerda para visualizar o conteúdo"}
              </span>
              <div className="flex gap-1 flex-shrink-0">
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={handleCopy} disabled={!content} title="Copiar conteúdo">
                  <Copy className="w-3.5 h-3.5" />
                </Button>
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={handleDownload} disabled={!content} title="Baixar arquivo">
                  <Download className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
            <div className="flex-1 min-h-0">
              {contentLoading && (
                <div className="h-full flex items-center justify-center text-muted-foreground text-sm gap-2">
                  <Loader2 className="w-4 h-4 animate-spin" /> Extraindo...
                </div>
              )}
              {contentError && !contentLoading && (
                <div className="h-full flex items-center justify-center text-destructive text-sm p-4 text-center">
                  {contentError}
                </div>
              )}
              {!contentLoading && !contentError && selectedEntry && (
                <Editor
                  key={selectedEntry}
                  language={monacoLanguageForEntry(selectedEntry)}
                  value={content}
                  height="100%"
                  options={{ readOnly: true, minimap: { enabled: false }, fontSize: 13, wordWrap: "on" }}
                />
              )}
              {!contentLoading && !contentError && !selectedEntry && (
                <div className="h-full flex items-center justify-center text-muted-foreground text-sm">
                  Nenhuma entrada selecionada
                </div>
              )}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
