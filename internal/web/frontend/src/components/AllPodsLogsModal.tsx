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

// Throttle de render: linhas chegam via SSE uma por vez (potencialmente dezenas/segundo com vários
// pods barulhentos) — sem agrupar, cada linha viraria um re-render do React, travando a UI. Acumula
// num ref e libera pro state a cada FLUSH_INTERVAL_MS.
const FLUSH_INTERVAL_MS = 150;

// Ring buffer — "sem cache cheio": mantém só as últimas MAX_LINES no state, descarta as mais
// antigas. Independente do bufferSize do replay buffer do broker SSE (já limitado no backend).
const MAX_LINES = 2000;

// Distância (px) do fundo do scroll a partir da qual ainda consideramos "colado no fim" —
// pequena folga pra não perder o autoscroll por causa de 1-2px de arredondamento.
const AUTOSCROLL_THRESHOLD_PX = 48;

// Paleta fixa de cores por pod, cíclica por índice — cor estável durante toda a sessão do modal
// (mesmo pod, mesma cor). Cor do PREFIXO [pod-name] — o conteúdo da linha em si continua colorido
// por severidade (logLineColor), igual ao visualizador single-pod.
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
  content: string;
}

// stripTimestampPrefix remove o prefixo RFC3339Nano que o backend inclui (PodLogOptions.Timestamps
// = true, equivalente a `kubectl logs --timestamps`) — usado só pra exibição, sem reordenar: as
// linhas já chegam na ordem real de emissão via streaming (Follow=true), então não há necessidade
// de parsear o timestamp pra ordenação como na versão anterior (polling/snapshot).
function stripTimestampPrefix(raw: string): string {
  const spaceIdx = raw.indexOf(" ");
  if (spaceIdx === -1) return raw;
  if (Number.isNaN(Date.parse(raw.slice(0, spaceIdx)))) return raw;
  return raw.slice(spaceIdx + 1);
}

// AllPodsLogsModal mostra os logs de vários pods intercalados por tempo real num único stream,
// prefixados por pod — estilo `kubectl logs -l app=x --prefix` / `stern`. Reaproveita os MESMOS
// recursos do visualizador single-pod (LogsViewer, dentro de PodQuickViewModal.tsx): filtro por
// nível (ERR/WARN/INFO/DEBUG), busca com destaque, coloração de linha por severidade, copiar,
// inspetor de JSON — nada reduzido, só adaptado pra várias origens ao mesmo tempo (prefixo [pod]).
//
// Streaming real via SSE (Follow=true, mesmo `kubectl logs -f`) — internal/web/handlers/
// pods_logs_stream.go abre uma goroutine por pod, cada uma publicando linha-a-linha no mesmo
// sessionID do broker SSE já usado por Command Runner/Health Check/etc. Diferente da primeira
// versão (polling: buscava um snapshot a cada 5s, substituía o buffer inteiro e reordenava — daí o
// sintoma de "carrega, trava, repete"), aqui a conexão fica aberta e cada linha nova chega e é
// apensada ao buffer existente, sem re-fetch nem re-sort.
export function AllPodsLogsModal({ open, onClose, cluster, pods }: Props) {
  const effectivePods = useMemo(() => pods.slice(0, MAX_PODS), [pods]);
  const truncated = pods.length > MAX_PODS;

  const [lines, setLines] = useState<MergedLine[]>([]);
  const [loading, setLoading] = useState(false);
  // autoRefresh agora controla a CONEXÃO SSE (não um poll): desligado = stream fechado, buffer já
  // recebido continua visível; ligado = (re)abre um streaming novo do zero.
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [tailLines, setTailLines] = useState("100");
  const [logLevelFilter, setLogLevelFilter] = useState<Set<LogLevel>>(new Set());
  const [search, setSearch] = useState("");
  const [copied, setCopied] = useState(false);
  const jsonInspector = useJsonInspector();

  // Cor por pod estável durante a sessão do modal, independente da ordem de chegada das linhas
  // (cada pod sempre pisca a mesma cor, mesmo padrão da versão anterior).
  const podColorByName = useMemo(() => {
    const map = new Map<string, string>();
    effectivePods.forEach((pod, idx) => map.set(pod.name, POD_COLORS[idx % POD_COLORS.length]));
    return map;
  }, [effectivePods]);

  const esRef = useRef<EventSource | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const bufferRef = useRef<MergedLine[]>([]);
  const flushTimerRef = useRef<number | null>(null);

  const flushBuffer = useCallback(() => {
    flushTimerRef.current = null;
    if (bufferRef.current.length === 0) return;
    const incoming = bufferRef.current;
    bufferRef.current = [];
    setLines((prev) => {
      const next = prev.length > 0 ? prev.concat(incoming) : incoming;
      return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
    });
  }, []);

  const scheduleFlush = useCallback(() => {
    if (flushTimerRef.current !== null) return;
    flushTimerRef.current = window.setTimeout(flushBuffer, FLUSH_INTERVAL_MS);
  }, [flushBuffer]);

  // closeStream fecha a conexão SSE atual e avisa o backend pra cancelar as goroutines de streaming
  // daquela sessão (libera o Follow=true de cada pod em vez de deixá-lo pendurado no servidor).
  const closeStream = useCallback(() => {
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    if (sessionIdRef.current) {
      apiClient.cancelPodLogsStreamAll(sessionIdRef.current).catch(() => { /* best-effort */ });
      sessionIdRef.current = null;
    }
    if (flushTimerRef.current !== null) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    bufferRef.current = [];
  }, []);

  const openStream = useCallback(async () => {
    closeStream();
    if (effectivePods.length === 0) {
      setLines([]);
      return;
    }
    setLines([]);
    setLoading(true);
    try {
      const podsReq = effectivePods.map((pod) => ({
        namespace: pod.namespace,
        name: pod.name,
        // Container "normal" (não init/ephemeral) — mesma heurística já usada em
        // PodLogsPanel.tsx/ContainersTab.tsx.
        container: pod.containers.find((c) => c.type === "container")?.name,
      }));
      const { session_id } = await apiClient.startPodLogsStreamAll(cluster, podsReq, Number(tailLines));
      sessionIdRef.current = session_id;

      const es = new EventSource(apiClient.getPodLogsStreamAllURL(session_id));
      esRef.current = es;

      es.onopen = () => setLoading(false);

      es.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          if (event.type === "complete") return; // todas as goroutines terminaram (ex: cancelado)
          const result = event.result;
          if (!result?.pod) return;
          const content = result.error
            ? `[erro ao ler logs deste pod] ${result.error}`
            : stripTimestampPrefix(result.line ?? "");
          bufferRef.current.push({
            podLabel: result.pod,
            colorClass: podColorByName.get(result.pod) ?? POD_COLORS[0],
            content,
          });
          scheduleFlush();
        } catch {
          /* ignora evento malformado — não derruba a conexão por uma linha ruim */
        }
      };

      es.onerror = () => setLoading(false);
    } catch {
      setLoading(false);
    }
  }, [cluster, effectivePods, tailLines, podColorByName, scheduleFlush, closeStream]);

  useEffect(() => {
    if (open && autoRefresh) {
      openStream();
    } else {
      closeStream();
    }
    return closeStream;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, autoRefresh, cluster, tailLines, effectivePods]);

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

  // Autoscroll condicional — gruda no fim a cada novo lote de linhas, EXCETO se o usuário rolou
  // pra cima manualmente (mesmo comportamento de um log viewer ao vivo tipo k9s/stern). Um scroll
  // handler marca stickToBottomRef=false assim que o usuário se afasta do fim; volta a "colar"
  // só quando ele rola de volta pra perto do fim por conta própria.
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);

  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickToBottomRef.current = distanceFromBottom <= AUTOSCROLL_THRESHOLD_PX;
  }, []);

  useEffect(() => {
    if (!stickToBottomRef.current) return;
    const el = scrollContainerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [filteredLines]);

  // Reabrir o modal (ou trocar de pods/tail) deve voltar a colar no fim — sem isso, uma sessão
  // anterior rolada pra cima "vazaria" o estado pro próximo streaming.
  useEffect(() => {
    if (open) stickToBottomRef.current = true;
  }, [open]);

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
            title={autoRefresh ? "Streaming ao vivo ligado — clique pra pausar" : "Streaming pausado — clique pra retomar"}
          >
            <RefreshCw className={`w-3 h-3 ${autoRefresh && !loading ? "animate-spin" : ""}`} />
            Auto
          </Button>

          <Button
            size="sm" variant="outline" className="h-7 text-xs gap-1"
            // Se o streaming estiver pausado (Auto desligado), religar via setAutoRefresh dispara o
            // efeito que já chama openStream sozinho — chamar openStream aqui também duplicaria a
            // conexão. Só quando já está ligado é que este botão precisa forçar o reconnect direto.
            onClick={() => { if (!autoRefresh) setAutoRefresh(true); else openStream(); }}
            disabled={loading}
            title="Fecha e reabre o streaming do zero"
          >
            {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
            Reconectar
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
        <div
          ref={scrollContainerRef}
          onScroll={handleScroll}
          className="flex-1 min-h-0 overflow-auto bg-black/50"
          onMouseUp={jsonInspector.handleMouseUp}
        >
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
