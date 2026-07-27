import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Loader2,
  RefreshCw,
  Search,
  X,
  ScrollText,
  AlertTriangle,
  Copy,
  Check,
  Braces,
} from "lucide-react";
import type { PodSummary } from "@/lib/api/types";
import { apiClient } from "@/lib/api/client";
import {
  escapeRegExp,
  highlightMatches,
  getLogLevel,
  logLineColor,
  LOG_LEVEL_CONFIG,
} from "@/components/PodQuickViewModal";
import type { LogLevel } from "@/components/PodQuickViewModal";
import { useJsonInspector } from "@/hooks/useJsonInspector";
import { JsonInspectorModal, JsonFloatingButton } from "@/components/JsonInspectorModal";

interface Props {
  open: boolean;
  onClose: () => void;
  cluster: string;
  // pods deve ser a lista JÁ FILTRADA (o array "filtered" de PodMonitorTable), não a lista bruta
  // — o botão que abre esse modal mostra logs de "todos os pods listados", respeitando
  // busca/filtros ativos, não literalmente todos os pods do cluster/namespace.
  pods: PodSummary[];
}

// MAX_PODS é um teto de segurança — sem isso, um filtro amplo (ex: sem nenhum filtro aplicado)
// dispararia dezenas/centenas de fetches paralelos a cada ciclo de refresh, sobrecarregando tanto
// o servidor da app quanto a API do cluster. Mesmo espírito de outros tetos já existentes no
// projeto (ex: dbRedisScanCap).
const MAX_PODS = 40;
const REFRESH_INTERVAL_MS = 5000; // mesmo intervalo do auto-refresh do LogsViewer single-pod

// Paleta fixa de cores por pod, cíclica por índice — cor estável entre refreshes (mesmo pod,
// mesma cor), mesmo depois de a lista intercalada reordenar por timestamp a cada fetch. Cor do
// PREFIXO [pod-name] — o conteúdo da linha em si continua colorido por severidade
// (logLineColor), igual ao visualizador single-pod.
const POD_COLORS = [
  "text-blue-300",
  "text-emerald-300",
  "text-amber-300",
  "text-fuchsia-300",
  "text-cyan-300",
  "text-orange-300",
  "text-lime-300",
  "text-pink-300",
  "text-indigo-300",
  "text-teal-300",
];

interface MergedLine {
  podLabel: string;
  colorClass: string;
  // ts: epoch ms, ou null quando a linha não tinha um timestamp parseável no início (cai no fim
  // da ordenação, mantendo a ordem relativa entre si — nunca quebra o parse das demais linhas).
  ts: number | null;
  content: string;
}

// parseTimestampedLine separa o prefixo RFC3339Nano (produzido pelo backend quando
// getPodLogs(..., timestamps=true) é usado — equivalente a `kubectl logs --timestamps`) do
// resto da linha. Linhas sem timestamp parseável no início (raro) ficam com ts=null.
function parseTimestampedLine(raw: string): { ts: number | null; content: string } {
  const spaceIdx = raw.indexOf(" ");
  if (spaceIdx === -1) return { ts: null, content: raw };
  const parsed = Date.parse(raw.slice(0, spaceIdx));
  if (Number.isNaN(parsed)) return { ts: null, content: raw };
  return { ts: parsed, content: raw.slice(spaceIdx + 1) };
}

// AllPodsLogsModal mostra os logs de vários pods intercalados por tempo real num único stream,
// prefixados por pod — estilo `kubectl logs -l app=x --prefix` / `stern`. Reaproveita os MESMOS
// recursos do visualizador single-pod (LogsViewer, dentro de PodQuickViewModal.tsx): filtro por
// nível (ERR/WARN/INFO/DEBUG), busca com destaque, coloração de linha por severidade, auto-refresh
// a cada 5s, copiar, inspetor de JSON — nada reduzido, só adaptado pra várias origens ao mesmo
// tempo (prefixo [pod] + merge cronológico entre pods, que não existe no caso single-pod).
//
// Não existe streaming nem endpoint de logs agregados no backend: é polling (mesmo padrão de
// PodLogsPanel.tsx/LogsViewer) fazendo N chamadas paralelas de getPodLogs (uma por pod, primeiro
// container "normal" de cada) a cada ciclo, remontando o stream intercalado do zero — sem append
// incremental.
export function AllPodsLogsModal({ open, onClose, cluster, pods }: Props) {
  const effectivePods = useMemo(() => pods.slice(0, MAX_PODS), [pods]);
  const truncated = pods.length > MAX_PODS;

  const [lines, setLines] = useState<MergedLine[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [tailLines, setTailLines] = useState("100");
  const [logLevelFilter, setLogLevelFilter] = useState<Set<LogLevel>>(new Set());
  const [search, setSearch] = useState("");
  const [copied, setCopied] = useState(false);
  const jsonInspector = useJsonInspector();

  const fetchAll = useCallback(async () => {
    if (effectivePods.length === 0) {
      setLines([]);
      return;
    }
    setLoading(true);
    try {
      const results = await Promise.allSettled(
        effectivePods.map(async (pod, idx) => {
          // Container "normal" (não init/ephemeral) — mesma heurística de "primeiro container"
          // já usada em PodLogsPanel.tsx/ContainersTab.tsx. Sem seletor de container por pod
          // nesta versão (limitação aceita — ver plano).
          const container = pod.containers.find((c) => c.type === "container")?.name;
          const res = await apiClient.getPodLogs(cluster, pod.namespace, pod.name, container, Number(tailLines), false, true);
          return { pod, idx, raw: res.logs };
        })
      );

      const merged: MergedLine[] = [];
      for (const r of results) {
        if (r.status !== "fulfilled") continue; // pod pode ter sido removido entre o clique e o fetch — ignora, não derruba os demais
        const { pod, idx, raw } = r.value;
        const colorClass = POD_COLORS[idx % POD_COLORS.length];
        for (const rawLine of raw.split("\n")) {
          if (!rawLine) continue;
          const { ts, content } = parseTimestampedLine(rawLine);
          merged.push({ podLabel: pod.name, colorClass, ts, content });
        }
      }
      // Linhas sem timestamp (ts=null) vão pro fim, mantendo ordem relativa entre si (sort estável).
      merged.sort((a, b) => {
        if (a.ts === null && b.ts === null) return 0;
        if (a.ts === null) return 1;
        if (b.ts === null) return -1;
        return a.ts - b.ts;
      });
      setLines(merged);
    } finally {
      setLoading(false);
    }
  }, [cluster, effectivePods, tailLines]);

  useEffect(() => {
    if (open) fetchAll();
  }, [open, fetchAll]);

  useEffect(() => {
    if (!open || !autoRefresh) return;
    const id = setInterval(fetchAll, REFRESH_INTERVAL_MS);
    return () => clearInterval(id);
  }, [open, autoRefresh, fetchAll]);

  const toggleLevelFilter = (level: LogLevel) => {
    setLogLevelFilter((prev) => {
      const next = new Set(prev);
      if (next.has(level)) next.delete(level);
      else next.add(level);
      return next;
    });
  };

  const clearFilters = () => {
    setLogLevelFilter(new Set());
    setSearch("");
  };

  // Mesma composição de filtro do filterLogLines single-pod: nível (se algum estiver ativo) E
  // busca por texto (se preenchida) — só que aqui aplicado às linhas já intercaladas/mescladas.
  const filteredLines = useMemo(() => {
    let result = lines;
    if (logLevelFilter.size > 0) {
      result = result.filter((l) => {
        const level = getLogLevel(l.content);
        return level !== null && logLevelFilter.has(level);
      });
    }
    const q = search.trim().toLowerCase();
    if (q) {
      result = result.filter((l) => l.content.toLowerCase().includes(q) || l.podLabel.toLowerCase().includes(q));
    }
    return result;
  }, [lines, logLevelFilter, search]);

  const handleCopy = useCallback(() => {
    const text = filteredLines.map((l) => `[${l.podLabel}] ${l.content}`).join("\n");
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [filteredLines]);

  // Resize da janela do modal — mesmo padrão já validado em PodQuickViewModal.tsx/
  // JsonInspectorModal.tsx (não extraído em hook compartilhado: replicar o bloco pequeno é mais
  // seguro aqui do que alterar os outros dois componentes já em produção sem necessidade).
  const [modalSize, setModalSize] = useState({ width: 1000, height: 700 });
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
        width: resizeDir.current !== "s" ? Math.max(560, prev.width + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(400, prev.height + dy) : prev.height,
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

  const startResize = (dir: "se" | "e" | "s") => (e: ReactMouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    resizing.current = true;
    resizeDir.current = dir;
    lastResizePos.current = { x: e.clientX, y: e.clientY };
    document.body.style.cursor = `${dir}-resize`;
    document.body.style.userSelect = "none";
  };

  const hasActiveFilters = logLevelFilter.size > 0 || search.trim().length > 0;

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
        className="flex flex-col p-0 gap-0 overflow-hidden"
      >
        <DialogHeader className="px-4 pt-4 pb-2 flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <ScrollText className="w-4 h-4" />
            Logs de {effectivePods.length} pod(s)
            {loading && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
          </DialogTitle>
        </DialogHeader>

        {truncated && (
          <div className="mx-4 mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-400 flex items-center gap-1.5 flex-shrink-0">
            <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
            Mostrando os primeiros {MAX_PODS} de {pods.length} pods filtrados — refine a busca pra ver os demais.
          </div>
        )}

        {/* Linha 1 — mesma composição do LogsViewer single-pod (menos o seletor de container,
            que não faz sentido aqui: cada pod já usa o seu primeiro container automaticamente). */}
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0">
          <Select value={tailLines} onValueChange={setTailLines}>
            <SelectTrigger className="h-7 text-xs w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {["100", "500", "1000", "5000"].map((n) => (
                <SelectItem key={n} value={n} className="text-xs">{n} linhas/pod</SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Button
            size="sm" variant={autoRefresh ? "default" : "outline"}
            className="h-7 text-xs gap-1"
            onClick={() => setAutoRefresh((v) => !v)}
          >
            <RefreshCw className={`w-3 h-3 ${autoRefresh ? "animate-spin" : ""}`} />
            Auto
          </Button>

          <Button
            size="sm" variant="outline" className="h-7 text-xs"
            onClick={fetchAll} disabled={loading}
          >
            {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
          </Button>

          <div className="flex-1" />

          <Button
            size="sm" variant="ghost" className="h-7 text-xs gap-1"
            onClick={handleCopy} disabled={filteredLines.length === 0}
          >
            {copied ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
            {copied ? "Copiado" : "Copiar"}
          </Button>
          <Button
            size="sm" variant="ghost"
            className="h-7 text-xs gap-1 text-blue-400 hover:bg-blue-400/10"
            onClick={jsonInspector.openInspector}
            title="Selecione texto no log para inspecionar JSON"
          >
            <Braces className="w-3 h-3" />
            JSON
          </Button>
        </div>

        {/* Linha 2 — filtro de nível + busca, mesma UI do LogsViewer single-pod */}
        <div className="flex items-center gap-2 px-4 py-1.5 border-b border-border/50 flex-shrink-0 bg-muted/10">
          {(["error", "warn", "info", "debug"] as LogLevel[]).map((level) => {
            const cfg = LOG_LEVEL_CONFIG[level];
            const active = logLevelFilter.has(level);
            return (
              <button
                key={level}
                onClick={() => toggleLevelFilter(level)}
                className={`h-6 px-2 rounded text-[10px] font-mono font-medium border transition-colors ${active ? cfg.active : cfg.inactive}`}
              >
                {cfg.label}
              </button>
            );
          })}
          <div className="w-px h-4 bg-border/50 mx-0.5" />
          <div className="relative flex-1 max-w-56">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground pointer-events-none" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Buscar nos logs..."
              className="w-full h-6 pl-6 pr-6 text-[11px] bg-background border border-border rounded focus:outline-none focus:border-primary font-mono"
            />
            {search && (
              <button
                onClick={() => setSearch("")}
                className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="w-3 h-3" />
              </button>
            )}
          </div>
          {hasActiveFilters && (
            <span className="text-[10px] text-muted-foreground ml-auto font-mono">
              {filteredLines.length} linha{filteredLines.length !== 1 ? "s" : ""}
            </span>
          )}
          {hasActiveFilters && (
            <button
              onClick={clearFilters}
              className="text-[10px] text-muted-foreground hover:text-foreground flex items-center gap-0.5"
            >
              <X className="w-3 h-3" /> Limpar
            </button>
          )}
        </div>

        {/* Área de scroll de logs — mesmo fundo/estilo do LogsViewer single-pod */}
        <div className="flex-1 min-h-0 overflow-auto bg-black/50" onMouseUp={jsonInspector.handleMouseUp}>
          <div className="p-3 font-mono text-xs leading-5">
            {loading && lines.length === 0 ? (
              <div className="text-muted-foreground flex items-center gap-2">
                <Loader2 className="w-3 h-3 animate-spin" /> Carregando logs...
              </div>
            ) : filteredLines.length > 0 ? (
              filteredLines.map((l, i) => (
                <div key={i} className="whitespace-pre-wrap break-all flex gap-1.5">
                  <span className={`shrink-0 font-semibold ${l.colorClass}`}>[{l.podLabel}]</span>
                  <span className={`min-w-0 ${logLineColor(l.content)}`}>{highlightMatches(l.content, search)}</span>
                </div>
              ))
            ) : (
              <span className="text-muted-foreground">Nenhuma linha encontrada.</span>
            )}
          </div>
        </div>

        {jsonInspector.floatingPos && (
          <JsonFloatingButton pos={jsonInspector.floatingPos} onClick={jsonInspector.openInspector} />
        )}
        <JsonInspectorModal
          open={jsonInspector.open}
          onClose={() => jsonInspector.setOpen(false)}
          initialText={jsonInspector.text}
        />

        {/* Handles de resize — mesmo padrão de PodQuickViewModal.tsx */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={startResize("e")}
        />
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={startResize("s")}
        />
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={startResize("se")}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
            <path d="M9 5 L9 9 L5 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
          </svg>
        </div>
      </DialogContent>
    </Dialog>
  );
}
