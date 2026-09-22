import { useEffect, useMemo, useRef, useState } from "react";
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
import { Copy, Download, Loader2, FileCode, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { PodArchiveCandidate, PodArchiveEntry } from "@/lib/api/types";
import { formatBytes } from "@/lib/monitorUtils";

// PodConfigFinderModal — buscador de arquivos de configuração num container de pod. Cobre dois
// casos reais, cada um com Kind diferente (ver PodArchiveCandidate.kind):
//   - Kind="archive": apps Spring Boot desta empresa (chart convair-helm) que não expõem o
//     application.yml via ConfigMap — o arquivo vem compilado dentro do próprio jar
//     (BOOT-INF/classes/application.yaml). Precisa listar entradas antes de extrair uma.
//   - Kind="file": apps .NET, cujo appsettings.json/web.config normalmente é arquivo SOLTO
//     (Dockerfile COPY), nunca empacotado — lido direto, sem passo de listar entradas.
// Deliberadamente genérico por nome/extensão (não hardcoded pra um framework só). Ver
// internal/web/handlers/pod_config_finder.go.

interface PodConfigFinderModalProps {
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

// ResizeDivider — arrasta a borda entre a lista de entradas e o editor. Mesmo padrão (sem
// componente compartilhado — cada tela duplica sua própria cópia pequena, ver CommandRunnerTab.tsx/
// CodeEditorTab.tsx) já usado no resto da app.
function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => {
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
  }, [onDrag]);

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

export function PodConfigFinderModal({
  open,
  onOpenChange,
  cluster,
  namespace,
  podName,
  containers,
}: PodConfigFinderModalProps) {
  const [container, setContainer] = useState(containers[0] ?? "");
  const [candidates, setCandidates] = useState<PodArchiveCandidate[]>([]);
  const [archivesLoading, setArchivesLoading] = useState(false);
  const [archivesError, setArchivesError] = useState<string | null>(null);
  const [selectedArchive, setSelectedArchive] = useState<string>("");
  const selectedCandidate = useMemo(
    () => candidates.find((cand) => cand.path === selectedArchive),
    [candidates, selectedArchive]
  );

  const [entries, setEntries] = useState<PodArchiveEntry[]>([]);
  const [entriesLoading, setEntriesLoading] = useState(false);
  const [entriesError, setEntriesError] = useState<string | null>(null);
  const [entrySearch, setEntrySearch] = useState("");
  const [selectedEntry, setSelectedEntry] = useState<string>("");

  const [content, setContent] = useState<string>("");
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);

  // Modal redimensionável (bordas direita/inferior/canto) — mesmo padrão de PodQuickViewModal.tsx.
  const [modalSize, setModalSize] = useState({ width: 1024, height: 640 });
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize((prev) => ({
        width: resizeDir.current !== "s" ? Math.max(640, prev.width + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(420, prev.height + dy) : prev.height,
      }));
    };
    const onUp = () => {
      if (!resizing.current) return;
      resizing.current = false;
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

  // Painel esquerdo (lista de entradas) redimensionável via ResizeDivider.
  const [leftPanelWidth, setLeftPanelWidth] = useState(288);

  useEffect(() => {
    if (!open) return;
    setContainer(containers[0] ?? "");
    setCandidates([]);
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
      const res = await apiClient.getPodConfigCandidates(cluster, namespace, podName, targetContainer);
      setCandidates(res.candidates || []);
      if ((res.candidates || []).length > 0) {
        setSelectedArchive(res.candidates[0].path);
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

  // Kind="file" (.NET appsettings.json etc): não existe "listar entradas" de verdade — trata o
  // próprio arquivo como entrada única e já carrega o conteúdo direto (getPodConfigFileContent).
  // Kind="archive" (Spring Boot etc): fluxo original — lista entradas do pacote, usuário escolhe.
  useEffect(() => {
    if (!selectedArchive || !container || !selectedCandidate) {
      setEntries([]);
      return;
    }
    if (selectedCandidate.kind === "file") {
      const name = entryDisplayName(selectedCandidate.path);
      setEntriesError(null);
      setEntriesLoading(false);
      setEntries([{ name, size_bytes: selectedCandidate.size_bytes }]);
      handleSelectEntry(name);
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
  }, [selectedArchive, selectedCandidate]);

  // BUG REAL corrigido — relatado ao vivo: colar um nome exato copiado de outro lugar (ex:
  // "BOOT-INF/classes/application.yaml" copiado da própria lista) não retornava resultado nenhum.
  // Causa: só o CHECK de "está vazio" usava `.trim()` — a comparação em si usava `entrySearch`
  // cru, então um espaço/quebra de linha invisível colado junto (comum ao copiar texto de uma
  // lista/tabela renderizada) nunca batia com `includes()`. Corrigido normalizando o termo (trim +
  // barra invertida → normal, cobre copy-paste vindo de um path exibido em estilo Windows) ANTES
  // de comparar, não só antes de decidir se o filtro está "ativo".
  const filteredEntries = useMemo(() => {
    const q = entrySearch.trim().toLowerCase().replace(/\\/g, "/");
    if (!q) return entries;
    return entries.filter((e) => e.name.toLowerCase().includes(q));
  }, [entries, entrySearch]);

  const handleSelectEntry = async (entryName: string) => {
    setSelectedEntry(entryName);
    setContentLoading(true);
    setContentError(null);
    setContent("");
    try {
      const res = selectedCandidate?.kind === "file"
        ? await apiClient.getPodConfigFileContent(cluster, namespace, podName, container, selectedCandidate.path)
        : await apiClient.getPodArchiveContent(cluster, namespace, podName, container, selectedArchive, entryName);
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
      <DialogContent
        className="flex flex-col overflow-hidden"
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
      >
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <FileCode className="w-5 h-5" />
            Buscar arquivo de configuração
          </DialogTitle>
          <DialogDescription>
            Localiza e mostra arquivos de config do container, soltos ou empacotados — útil pra apps .NET cujo{" "}
            <code>appsettings.json</code>/<code>web.config</code> é arquivo solto na imagem, ou apps Java/Spring cujo{" "}
            <code>application.yml</code> vem compilado dentro do próprio <code>.jar/.war/.zip</code>, sem ConfigMap.
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
              Arquivo de config — {archivesLoading ? "buscando..." : `${candidates.length} encontrado(s)`}
            </label>
            <Select value={selectedArchive} onValueChange={setSelectedArchive} disabled={archivesLoading || candidates.length === 0}>
              <SelectTrigger>
                <SelectValue placeholder={archivesLoading ? "Buscando arquivos no container..." : "Nenhum arquivo encontrado"} />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((cand) => (
                  <SelectItem key={cand.path} value={cand.path}>
                    [{cand.kind === "file" ? "solto" : "pacote"}] {cand.path} {cand.size_bytes >= 0 ? `(${formatBytes(cand.size_bytes)})` : ""}
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

        <div className="flex-1 min-h-0 flex">
          <div
            className="flex-shrink-0 flex flex-col border border-border rounded-md overflow-hidden"
            style={{ width: leftPanelWidth }}
          >
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

          <ResizeDivider onDrag={(d) => setLeftPanelWidth((w) => Math.max(180, Math.min(600, w + d)))} />

          <div className="flex-1 min-w-0 flex flex-col border border-border rounded-md overflow-hidden ml-3">
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
                  <Loader2 className="w-4 h-4 animate-spin" /> Carregando...
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
                  theme="vs-dark"
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

        {/* Handles de resize do modal — mesmo padrão de PodQuickViewModal.tsx */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "e";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "e-resize";
            document.body.style.userSelect = "none";
          }}
        />
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "s";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "s-resize";
            document.body.style.userSelect = "none";
          }}
        />
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "se";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "se-resize";
            document.body.style.userSelect = "none";
          }}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
          </svg>
        </div>
      </DialogContent>
    </Dialog>
  );
}
