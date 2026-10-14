import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

// Visão "Formatado" do corpo JSON do Cliente HTTP: árvore recolhível com rótulos legíveis
// (prazoEntrega → "Prazo entrega"), Sim/Não nos booleanos e busca que filtra e destaca.

type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

// camelCase / snake_case / kebab-case → "Primeira maiúscula e resto em minúsculas".
export function humanizeKey(key: string): string {
  const words = key
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .trim()
    .split(/\s+/)
    .map((w, i) => (i > 0 && w.length > 1 && w === w.toUpperCase() ? w : w.toLowerCase()));
  const s = words.join(" ");
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : key;
}

const isContainer = (v: Json): v is Json[] | { [key: string]: Json } => v !== null && typeof v === "object";

function primitiveText(v: Json): string {
  if (v === null) return "null";
  if (typeof v === "boolean") return v ? "Sim" : "Não";
  return String(v);
}

// Rótulo extra de um item de lista: o primeiro campo "identificador" que for primitivo.
const HINT_KEYS = ["nome", "name", "descricao", "description", "titulo", "title", "tipo", "type", "id", "key", "codigo"];
function itemHint(v: Json): string {
  if (!isContainer(v) || Array.isArray(v)) return "";
  for (const k of HINT_KEYS) {
    const val = v[k];
    if (val !== undefined && !isContainer(val)) return primitiveText(val);
    if (isContainer(val) && !Array.isArray(val)) {
      const inner = itemHint(val);
      if (inner) return inner;
    }
  }
  return "";
}

function textMatches(text: string, q: string): boolean {
  return text.toLowerCase().includes(q);
}

// O nó (pela chave ou pelo valor) ou algum descendente casa com a busca.
function subtreeMatches(label: string, rawKey: string, v: Json, q: string): boolean {
  if (textMatches(label, q) || textMatches(rawKey, q)) return true;
  if (!isContainer(v)) return textMatches(primitiveText(v), q) || textMatches(String(v), q);
  return Object.entries(v).some(([k, child]) =>
    subtreeMatches(Array.isArray(v) ? `Item ${Number(k) + 1}` : humanizeKey(k), k, child, q)
  );
}

export function Highlight({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>;
  const lower = text.toLowerCase();
  const parts: React.ReactNode[] = [];
  let from = 0;
  let i = lower.indexOf(query, from);
  while (i !== -1) {
    if (i > from) parts.push(text.slice(from, i));
    parts.push(
      <mark key={i} className="bg-yellow-300/70 dark:bg-yellow-500/50 text-inherit rounded-sm">
        {text.slice(i, i + query.length)}
      </mark>
    );
    from = i + query.length;
    i = lower.indexOf(query, from);
  }
  if (from < text.length) parts.push(text.slice(from));
  return <>{parts}</>;
}

function ValueView({ v, query }: { v: Json; query: string }) {
  if (v === null) return <span className="text-muted-foreground italic">vazio (null)</span>;
  if (typeof v === "boolean")
    return (
      <span
        className={cn(
          "px-1.5 rounded text-[11px] font-medium",
          v ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" : "bg-red-500/10 text-red-600 dark:text-red-400"
        )}
      >
        <Highlight text={v ? "Sim" : "Não"} query={query} />
      </span>
    );
  if (typeof v === "number")
    return (
      <span className="font-mono text-blue-600 dark:text-blue-400">
        <Highlight text={String(v)} query={query} />
      </span>
    );
  if (v === "") return <span className="text-muted-foreground italic">texto vazio</span>;
  return (
    <span className="break-all">
      <Highlight text={String(v)} query={query} />
    </span>
  );
}

interface NodeProps {
  label: string;
  rawKey: string;
  hint?: string;
  value: Json;
  path: string;
  depth: number;
  query: string;
  // Com busca, um ancestral cuja chave casou mostra a subárvore inteira.
  showAll: boolean;
  open: Record<string, boolean>;
  toggle: (path: string, current: boolean) => void;
}

function JsonNode({ label, rawKey, hint, value, path, depth, query, showAll, open, toggle }: NodeProps) {
  const selfKeyMatch = !!query && (textMatches(label, query) || textMatches(rawKey, query));
  if (query && !showAll && !subtreeMatches(label, rawKey, value, query)) return null;

  const keyView = (
    <span className="text-muted-foreground" title={rawKey}>
      <Highlight text={label} query={query} />
    </span>
  );

  if (!isContainer(value)) {
    return (
      <div className="flex gap-2 py-0.5 pl-5 text-xs leading-5">
        <span className="shrink-0 min-w-[10rem]">{keyView}</span>
        <ValueView v={value} query={query} />
      </div>
    );
  }

  const entries = Object.entries(value);
  const isArray = Array.isArray(value);
  // Sem busca: aberto até o 4º nível (ou como o usuário deixou). Com busca: tudo que sobrou aberto.
  const isOpen = query ? true : open[path] ?? depth < 4;
  const summary = isArray
    ? `${entries.length} ${entries.length === 1 ? "item" : "itens"}`
    : `${entries.length} ${entries.length === 1 ? "campo" : "campos"}`;

  return (
    <div className="text-xs leading-5">
      <button
        type="button"
        onClick={() => toggle(path, isOpen)}
        className="flex items-center gap-1 py-0.5 w-full text-left hover:bg-muted/50 rounded"
      >
        {isOpen ? <ChevronDown className="w-4 h-4 shrink-0" /> : <ChevronRight className="w-4 h-4 shrink-0" />}
        <span className="font-semibold text-foreground" title={rawKey}>
          <Highlight text={label} query={query} />
        </span>
        {hint && (
          <span className="text-foreground/80">
            — <Highlight text={hint} query={query} />
          </span>
        )}
        <span className="text-muted-foreground">({summary})</span>
      </button>
      {isOpen && (
        <div className="ml-2 pl-3 border-l border-border">
          {entries.length === 0 && <div className="pl-5 py-0.5 text-muted-foreground italic">vazio</div>}
          {entries.map(([k, child]) => (
            <JsonNode
              key={k}
              label={isArray ? `Item ${Number(k) + 1}` : humanizeKey(k)}
              rawKey={k}
              hint={isArray ? itemHint(child) : undefined}
              value={child}
              path={`${path}/${k}`}
              depth={depth + 1}
              query={query}
              showAll={showAll || selfKeyMatch}
              open={open}
              toggle={toggle}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function HttpClientJsonView({ data, query }: { data: unknown; query: string }) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const toggle = (path: string, current: boolean) => setOpen((o) => ({ ...o, [path]: !current }));
  const q = query.trim().toLowerCase();
  const root = data as Json;

  if (!isContainer(root)) {
    return (
      <div className="p-3 text-xs">
        <ValueView v={root} query={q} />
      </div>
    );
  }
  const entries = Object.entries(root);
  const isArray = Array.isArray(root);
  const visible = !q || entries.some(([k, child]) => subtreeMatches(isArray ? `Item ${Number(k) + 1}` : humanizeKey(k), k, child, q));

  return (
    <div className="p-3">
      {!visible && <p className="text-xs text-muted-foreground">Nenhum trecho encontrado para "{query.trim()}".</p>}
      {entries.map(([k, child]) => (
        <JsonNode
          key={k}
          label={isArray ? `Item ${Number(k) + 1}` : humanizeKey(k)}
          rawKey={k}
          hint={isArray ? itemHint(child) : undefined}
          value={child}
          path={k}
          depth={0}
          query={q}
          showAll={false}
          open={open}
          toggle={toggle}
        />
      ))}
    </div>
  );
}
