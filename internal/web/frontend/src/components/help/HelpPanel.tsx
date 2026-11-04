import { useEffect, useMemo, useRef, useState } from "react";
import { BookOpen, HelpCircle, Loader2, Search, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { loadHelpPage } from "@/help/helpContent";
import {
  HELP_ENTRIES,
  HELP_IMPACT_LABEL,
  findHelpEntry,
  type HelpEntry,
  type HelpImpact,
} from "@/help/helpRegistry";
import { extractHeadings, filterEntries, groupEntries } from "@/help/helpSearch";
import { HelpMarkdown } from "./HelpMarkdown";
import { useHelp } from "./helpContext";

// Painel de ajuda fixo à direita, abaixo do cabeçalho. Não é modal: dá para ler o passo a passo e
// usar a aba ao mesmo tempo. Fica sob os diálogos (z-40 contra z-50).

const IMPACT_STYLE: Record<HelpImpact, string> = {
  leitura: "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  escrita: "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400",
  destrutiva: "border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-400",
};

function ImpactBadge({ impact }: { impact: HelpImpact }) {
  return (
    <Badge variant="outline" className={`text-[10px] font-medium ${IMPACT_STYLE[impact]}`}>
      {HELP_IMPACT_LABEL[impact]}
    </Badge>
  );
}

function IndexView({ onPick }: { onPick: (id: string) => void }) {
  const [query, setQuery] = useState("");
  const groups = useMemo(() => groupEntries(filterEntries(HELP_ENTRIES, query)), [query]);
  return (
    <div className="space-y-3 p-3">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
        <Input
          autoFocus
          className="h-8 pl-7 text-xs"
          placeholder="Buscar na ajuda..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {groups.length === 0 ? (
        <p className="px-1 text-xs text-muted-foreground">Nenhuma página encontrada para "{query}".</p>
      ) : (
        groups.map((g) => (
          <section key={g.group}>
            <h3 className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{g.group}</h3>
            <div className="space-y-0.5">
              {g.items.map((e) => (
                <button
                  key={e.id}
                  onClick={() => onPick(e.id)}
                  className="w-full rounded-md px-2 py-1.5 text-left hover:bg-muted/50"
                >
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-medium text-foreground">{e.title}</span>
                    <ImpactBadge impact={e.impact} />
                  </div>
                  <p className="mt-0.5 text-[11px] leading-snug text-muted-foreground">{e.summary}</p>
                </button>
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  );
}

function PageView({ entry, anchor }: { entry: HelpEntry; anchor: string | null }) {
  const [markdown, setMarkdown] = useState<string | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing">("loading");
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    setState("loading");
    setMarkdown(null);
    loadHelpPage(entry.id)
      .then((md) => {
        if (cancelled) return;
        setMarkdown(md);
        setState(md === null ? "missing" : "ready");
      })
      .catch(() => !cancelled && setState("missing"));
    return () => {
      cancelled = true;
    };
  }, [entry.id]);

  const headings = useMemo(() => (markdown ? extractHeadings(markdown).filter((h) => h.level === 2) : []), [markdown]);

  // Rola até a seção pedida (openHelp(id, "secao")) ou volta ao topo ao trocar de página.
  useEffect(() => {
    if (state !== "ready") return;
    const target = anchor ? bodyRef.current?.querySelector<HTMLElement>(`#${CSS.escape(anchor)}`) : null;
    if (target) target.scrollIntoView({ block: "start" });
    else bodyRef.current?.scrollTo({ top: 0 });
  }, [state, anchor, entry.id]);

  return (
    <div ref={bodyRef} className="h-full overflow-y-auto p-3">
      <div className="mb-3">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-base font-semibold text-foreground">{entry.title}</h2>
          <ImpactBadge impact={entry.impact} />
        </div>
        <p className="mt-1 text-xs text-muted-foreground">{entry.summary}</p>
      </div>

      {state === "loading" && (
        <div className="flex items-center gap-2 py-6 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Carregando...
        </div>
      )}
      {state === "missing" && (
        <p className="rounded-md border border-border/50 bg-muted/30 p-3 text-xs text-muted-foreground">
          Não foi possível carregar esta página de ajuda.
        </p>
      )}
      {state === "ready" && markdown && (
        <>
          {headings.length > 1 && (
            <nav className="mb-3 rounded-md border border-border/50 bg-muted/30 p-2.5" aria-label="Nesta página">
              <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Nesta página</p>
              <ul className="space-y-0.5">
                {headings.map((h) => (
                  <li key={h.id}>
                    <button
                      className="text-left text-xs text-primary hover:underline"
                      onClick={() =>
                        bodyRef.current?.querySelector<HTMLElement>(`#${CSS.escape(h.id)}`)?.scrollIntoView({ block: "start", behavior: "smooth" })
                      }
                    >
                      {h.text}
                    </button>
                  </li>
                ))}
              </ul>
            </nav>
          )}
          <HelpMarkdown source={markdown} />
        </>
      )}
    </div>
  );
}

export function HelpPanel() {
  const { open, view, activeTabId, pageOverride, anchor, openHelp, closeHelp, showIndex } = useHelp();
  if (!open) return null;

  const currentId = pageOverride ?? activeTabId;
  const entry = findHelpEntry(currentId);

  return (
    <aside
      className="fixed bottom-0 right-0 top-16 z-40 flex w-[440px] max-w-[95vw] flex-col border-l border-border bg-background shadow-2xl"
      aria-label="Ajuda"
    >
      <div className="flex flex-shrink-0 items-center gap-2 border-b border-border/50 px-3 py-2">
        <HelpCircle className="h-4 w-4 text-primary" />
        <span className="text-sm font-semibold">Ajuda</span>
        <div className="flex-1" />
        <Button
          variant={view === "index" ? "secondary" : "ghost"}
          size="sm"
          className="h-7 gap-1 px-2 text-xs"
          onClick={showIndex}
          title="Todas as páginas de ajuda"
        >
          <BookOpen className="h-3.5 w-3.5" />
          Índice
        </Button>
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={closeHelp} title="Fechar a ajuda" aria-label="Fechar a ajuda">
          <X className="h-4 w-4" />
        </Button>
      </div>

      <div className="min-h-0 flex-1">
        {view === "index" ? (
          <div className="h-full overflow-y-auto">
            <IndexView onPick={(id) => openHelp(id)} />
          </div>
        ) : entry ? (
          <PageView entry={entry} anchor={anchor} />
        ) : (
          <div className="space-y-3 p-4">
            <p className="text-sm text-foreground">Ainda não há ajuda para esta aba.</p>
            <p className="text-xs text-muted-foreground">
              A ajuda está sendo escrita aba por aba. Veja as páginas que já existem no índice.
            </p>
            <Button variant="outline" size="sm" className="h-8 gap-1 text-xs" onClick={showIndex}>
              <BookOpen className="h-3.5 w-3.5" />
              Abrir o índice
            </Button>
          </div>
        )}
      </div>
    </aside>
  );
}
