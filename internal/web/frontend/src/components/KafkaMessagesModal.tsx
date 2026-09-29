import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Braces, Copy, Search, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { snippetAround, termsRegex } from "@/lib/kafkaHighlight";
import type { KafkaMessage } from "@/lib/api/types";

// Destaque âmbar suave: legível no claro e no escuro, sem gritar como o amarelo puro.
const MARK_CLASS =
  "rounded-[3px] px-0.5 bg-amber-200/80 text-amber-950 ring-1 ring-inset ring-amber-400/60 dark:bg-amber-400/25 dark:text-amber-100 dark:ring-amber-300/40";

/** Texto com os termos destacados (sem diferenciar maiúsculas). */
export function Highlight({ text, terms }: { text: string; terms: string[] }) {
  const re = useMemo(() => termsRegex(terms), [terms]);
  if (!re || !text) return <>{text}</>;
  const parts = text.split(re);
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <mark key={i} className={MARK_CLASS}>{part}</mark>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
    </>
  );
}

function formatJson(text: string): string | null {
  const t = text.trim();
  if (!t.startsWith("{") && !t.startsWith("[")) return null;
  try {
    return JSON.stringify(JSON.parse(t), null, 2);
  } catch {
    return null;
  }
}

function matches(m: KafkaMessage, term: string) {
  const q = term.trim().toLowerCase();
  if (!q) return true;
  return m.payload.toLowerCase().includes(q) || (m.key ?? "").toLowerCase().includes(q);
}

export default function KafkaMessagesModal({
  open,
  onOpenChange,
  messages,
  topic,
  serverFilter,
  initialIndex,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  messages: KafkaMessage[];
  topic: string;
  /** Texto usado na busca do scan (backend) — destacado junto com a busca local. */
  serverFilter: string;
  initialIndex: number;
}) {
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState(0);
  const [prettyJson, setPrettyJson] = useState(false);
  const detailRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  // Cada abertura começa na mensagem clicada, sem busca local.
  useEffect(() => {
    if (open) {
      setSearch("");
      setSelected(initialIndex);
    }
  }, [open, initialIndex]);

  const filtered = useMemo(
    () => messages.map((m, i) => ({ m, i })).filter(({ m }) => matches(m, search)),
    [messages, search]
  );
  const terms = useMemo(() => [serverFilter, search], [serverFilter, search]);

  // Mantém a seleção dentro do resultado da busca local.
  const current = filtered.find((f) => f.i === selected) ?? filtered[0];
  const currentPos = current ? filtered.indexOf(current) : -1;

  const payloadShown = current
    ? prettyJson
      ? formatJson(current.m.payload) ?? current.m.payload
      : current.m.payload
    : "";
  const canPretty = current ? formatJson(current.m.payload) !== null : false;

  // Leva a primeira ocorrência para o centro do painel de detalhe ao trocar de mensagem/busca.
  useEffect(() => {
    const el = detailRef.current;
    if (!el) return;
    const mark = el.querySelector("mark");
    if (mark) mark.scrollIntoView({ block: "center" });
    else el.scrollTop = 0;
  }, [current?.i, search, serverFilter, prettyJson]);

  const move = (delta: number) => {
    if (filtered.length === 0) return;
    const next = filtered[Math.min(filtered.length - 1, Math.max(0, currentPos + delta))];
    setSelected(next.i);
    listRef.current?.querySelector<HTMLElement>(`[data-idx="${next.i}"]`)?.scrollIntoView({ block: "nearest" });
  };

  const copy = (text: string, label: string) =>
    navigator.clipboard
      .writeText(text)
      .then(() => toast.success(`${label} copiado!`))
      .catch(() => toast.error("Não foi possível copiar"));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Altura FIXA (não só max-h): os dois painéis usam min-h-0 + overflow-y-auto, que só rolam com altura definida. */}
      <DialogContent className="max-w-6xl h-[85vh] flex flex-col gap-3 overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-2">
            Mensagens do tópico <span className="font-mono text-sm">{topic}</span>
            {serverFilter && (
              <Badge variant="outline" className="font-normal">
                busca no scan: <mark className={cn(MARK_CLASS, "ml-1")}>{serverFilter}</mark>
              </Badge>
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="flex items-center gap-2 shrink-0">
          <div className="relative flex-1 max-w-md">
            <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
            <Input
              autoFocus
              spellCheck={false}
              placeholder="Buscar texto nos resultados (key ou payload)..."
              className="pl-8 pr-8 font-mono text-xs"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") { e.preventDefault(); move(1); }
                if (e.key === "ArrowUp") { e.preventDefault(); move(-1); }
              }}
            />
            {search && (
              <button
                type="button"
                onClick={() => setSearch("")}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                title="Limpar busca"
              >
                <X className="w-4 h-4" />
              </button>
            )}
          </div>
          <span className="text-xs text-muted-foreground">
            {search ? `${filtered.length} de ${messages.length}` : `${messages.length} mensagem(ns)`} · ↑↓ navega
          </span>
        </div>

        <div className="flex-1 min-h-0 flex gap-3 overflow-hidden">
          {/* Lista */}
          <div ref={listRef} className="w-80 shrink-0 min-h-0 overflow-y-auto rounded-md border border-border">
            {filtered.length === 0 ? (
              <p className="p-3 text-xs text-muted-foreground">Nenhuma mensagem contém "{search}".</p>
            ) : (
              filtered.map(({ m, i }) => (
                <button
                  key={i}
                  data-idx={i}
                  type="button"
                  onClick={() => setSelected(i)}
                  className={cn(
                    "w-full text-left px-2.5 py-2 border-b border-border/60 flex flex-col gap-0.5",
                    current?.i === i ? "bg-primary/10" : "hover:bg-muted/50"
                  )}
                >
                  <span className="flex items-center gap-2 text-[10px] text-muted-foreground font-mono">
                    p{m.partition}@{m.offset}
                    {m.timestamp_ms ? <span>{new Date(m.timestamp_ms).toLocaleString("pt-BR")}</span> : null}
                    {m.binary && <span className="text-amber-600 dark:text-amber-400">binário</span>}
                  </span>
                  {m.key && (
                    <span className="text-[10px] font-mono text-muted-foreground truncate">
                      key: <Highlight text={m.key} terms={terms} />
                    </span>
                  )}
                  <span className="text-xs font-mono line-clamp-2 break-all">
                    <Highlight text={snippetAround(m.payload, terms)} terms={terms} />
                  </span>
                </button>
              ))
            )}
          </div>

          {/* Detalhe */}
          <div className="flex-1 min-w-0 min-h-0 flex flex-col gap-2">
            {current ? (
              <>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground shrink-0">
                  <span>Partição: <span className="font-mono text-foreground">{current.m.partition}</span></span>
                  <span>Offset: <span className="font-mono text-foreground">{current.m.offset}</span></span>
                  {current.m.timestamp_ms ? (
                    <span>Timestamp: <span className="font-mono text-foreground">{new Date(current.m.timestamp_ms).toLocaleString("pt-BR")}</span></span>
                  ) : null}
                  <div className="ml-auto flex items-center gap-2">
                    {canPretty && (
                      <Button
                        type="button"
                        variant={prettyJson ? "secondary" : "outline"}
                        size="sm"
                        className="h-7 gap-1.5"
                        onClick={() => setPrettyJson((v) => !v)}
                        title="Indentar o JSON do payload (a busca continua valendo sobre o texto exibido)"
                      >
                        <Braces className="w-3.5 h-3.5" />
                        {prettyJson ? "JSON original" : "Formatar JSON"}
                      </Button>
                    )}
                    <Button type="button" variant="outline" size="sm" className="h-7 gap-1.5" onClick={() => copy(current.m.payload, "Mensagem")}>
                      <Copy className="w-3.5 h-3.5" />
                      Copiar mensagem
                    </Button>
                  </div>
                </div>

                <div ref={detailRef} className="flex-1 min-h-0 overflow-y-auto flex flex-col gap-3 pr-1">
                  {current.m.binary && (
                    <div className="text-xs rounded-md border border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400 p-2">
                      Payload/key contém bytes que não são UTF-8 válido (dados binários — protobuf/Avro, ou um tópico
                      interno do Kafka). O kcat substitui esses bytes por "�", então o texto abaixo não reflete os bytes
                      originais com exatidão.
                    </div>
                  )}
                  {current.m.key && (
                    <div>
                      <div className="text-xs text-muted-foreground mb-1">Key</div>
                      <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-md border border-border bg-muted/30 p-2">
                        <Highlight text={current.m.key} terms={terms} />
                      </pre>
                    </div>
                  )}
                  <div>
                    <div className="text-xs text-muted-foreground mb-1">Payload</div>
                    <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-md border border-border bg-muted/30 p-2">
                      <Highlight text={payloadShown} terms={terms} />
                    </pre>
                  </div>
                </div>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">Nenhuma mensagem selecionada.</p>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
