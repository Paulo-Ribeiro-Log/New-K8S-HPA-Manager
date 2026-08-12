import { useMemo, useState } from "react";
import type { SecretDataRecord } from "@/lib/api/types";
import { Eye, EyeOff, Copy, RefreshCcw, KeyRound, AlertCircle, Loader2, ListFilter, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

// SERVIÇO | CLUSTER | NAMESPACE | FONTE — mesmo padrão de grid+resize de PodMonitorTable.tsx/
// SecretMonitorTable.tsx (útil pra manter a mesma UX de filtro-na-coluna+arraste já usada nas
// demais listagens da aplicação, em vez de uma barra de <Select> solta acima da tabela).
const INITIAL_WIDTHS = [420, 170, 150, 220];

const KIND_LABELS: Record<string, string> = { secret: "Secret", configmap: "ConfigMap" };

// Mesmo ColumnFilter (Popover + checkbox multi-select) já duplicado em cada *MonitorTable.tsx
// (não há versão compartilhada no projeto) — `formatOption` permite exibir um rótulo diferente do
// valor usado internamente pro filtro (ex: "secret" → "Secret", cluster sem sufixo "-admin").
function ColumnFilter({
  label,
  options,
  selected,
  onChange,
  formatOption,
}: {
  label: string;
  options: string[];
  selected: Set<string>;
  onChange: (v: Set<string>) => void;
  formatOption?: (opt: string) => string;
}) {
  const active = selected.size > 0;
  const toggle = (val: string) => {
    const next = new Set(selected);
    if (next.has(val)) next.delete(val);
    else next.add(val);
    onChange(next);
  };
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className={`flex items-center gap-0.5 uppercase hover:text-foreground transition-colors ${active ? "text-primary" : "text-muted-foreground"}`}
          title={`Filtrar por ${label}`}
        >
          {label}
          <ListFilter className="w-2.5 h-2.5 ml-0.5" />
          {active && (
            <span className="ml-0.5 bg-primary text-primary-foreground rounded-full text-[9px] w-3.5 h-3.5 flex items-center justify-center font-bold">
              {selected.size}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-max min-w-[180px] max-w-[360px] p-2" align="start">
        <div className="flex items-center justify-between gap-4 mb-2">
          <span className="text-xs font-medium whitespace-nowrap">{label}</span>
          {active && (
            <button onClick={() => onChange(new Set())} className="text-xs text-muted-foreground hover:text-foreground whitespace-nowrap">
              Limpar
            </button>
          )}
        </div>
        <ScrollArea className="max-h-72">
          <div className="space-y-1">
            {options.map((opt) => (
              <label key={opt} className="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-muted/50 text-xs">
                <Checkbox checked={selected.has(opt)} onCheckedChange={() => toggle(opt)} className="w-3.5 h-3.5 rounded-full flex-shrink-0" />
                <span className="whitespace-nowrap truncate" title={opt}>{formatOption ? formatOption(opt) : opt}</span>
                {selected.has(opt) && <Check className="w-3 h-3 text-primary flex-shrink-0" />}
              </label>
            ))}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  );
}

interface SecretDataResultsTableProps {
  items: SecretDataRecord[];
  loading: boolean;
  // Distingue "nunca buscou" de "buscou e não achou nada" — diferente das outras *MonitorTable.tsx
  // (que sempre têm uma lista base vinda de List(), aqui não há resultado nenhum antes da 1ª busca).
  hasSearched: boolean;
  query: string;
  onResyncClick: (cluster: string, namespace: string, resourceName: string) => void;
}

export const SecretDataResultsTable = ({ items, loading, hasSearched, query, onResyncClick }: SecretDataResultsTableProps) => {
  const [clusterFilter, setClusterFilter] = useState<Set<string>>(new Set());
  const [namespaceFilter, setNamespaceFilter] = useState<Set<string>>(new Set());
  const [kindFilter, setKindFilter] = useState<Set<string>>(new Set());
  const [revealedRows, setRevealedRows] = useState<Set<string>>(new Set());
  const [revealedAsBase64, setRevealedAsBase64] = useState<Set<string>>(new Set());
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);

  const rowKeyOf = (rec: SecretDataRecord) =>
    `${rec.resource_kind}-${rec.cluster}-${rec.namespace}-${rec.resource_name}-${rec.data_key}`;

  const uniqueClusters = useMemo(() => {
    const s = new Set<string>();
    items.forEach((r) => s.add(r.cluster));
    return Array.from(s).sort();
  }, [items]);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    items.forEach((r) => s.add(r.namespace));
    return Array.from(s).sort();
  }, [items]);

  const uniqueKinds = useMemo(() => {
    const s = new Set<string>();
    items.forEach((r) => s.add(r.resource_kind));
    return Array.from(s).sort();
  }, [items]);

  const hasFilters = clusterFilter.size > 0 || namespaceFilter.size > 0 || kindFilter.size > 0;

  const filtered = useMemo(() => {
    let result = items;
    if (clusterFilter.size > 0) result = result.filter((r) => clusterFilter.has(r.cluster));
    if (namespaceFilter.size > 0) result = result.filter((r) => namespaceFilter.has(r.namespace));
    if (kindFilter.size > 0) result = result.filter((r) => kindFilter.has(r.resource_kind));
    return result;
  }, [items, clusterFilter, namespaceFilter, kindFilter]);

  const toggleRevealed = (key: string) => {
    setRevealedRows((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleBase64 = (key: string) => {
    setRevealedAsBase64((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // Copia o valor exibido (decodificado ou base64, conforme o toggle da linha) — complementa a
  // quebra de linha (break-all) da célula: com valores longos quebrados em várias linhas, copiar
  // manualmente com o mouse é inconveniente.
  const copyValue = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success("Valor copiado para a área de transferência");
    } catch {
      toast.error("Não foi possível copiar o valor");
    }
  };

  return (
    <div className="flex flex-col h-full border border-border rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border bg-muted/30 flex-shrink-0">
        <span className="text-xs font-medium text-muted-foreground truncate">
          {hasSearched && (
            <>
              {filtered.length} resultado(s){hasFilters ? " (filtrado)" : ""}
              {query ? ` para "${query}"` : ""}
            </>
          )}
        </span>
        {loading && <Loader2 className="w-3 h-3 animate-spin text-muted-foreground flex-shrink-0" />}
      </div>

      {/* Cabeçalho das colunas + linhas */}
      <div className="flex-1 overflow-auto">
        <div
          className="sticky top-0 z-10 grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20"
          style={{ gridTemplateColumns: gridTemplate }}
        >
          <span className="relative flex items-center overflow-hidden pr-4 text-muted-foreground uppercase">
            Serviço
            <ResizeHandle onResize={(d) => resize(0, d)} />
          </span>
          <span className="relative flex items-center overflow-hidden pr-4">
            <ColumnFilter
              label="CLUSTER"
              options={uniqueClusters}
              selected={clusterFilter}
              onChange={setClusterFilter}
              formatOption={(c) => c.replace(/-admin$/, "")}
            />
            <ResizeHandle onResize={(d) => resize(1, d)} />
          </span>
          <span className="relative flex items-center overflow-hidden pr-4">
            <ColumnFilter label="NAMESPACE" options={uniqueNamespaces} selected={namespaceFilter} onChange={setNamespaceFilter} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative flex items-center overflow-hidden">
            <ColumnFilter
              label="FONTE"
              options={uniqueKinds}
              selected={kindFilter}
              onChange={setKindFilter}
              formatOption={(k) => KIND_LABELS[k] || k}
            />
          </span>
        </div>

        {!hasSearched ? (
          <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <KeyRound className="h-12 w-12 mb-4 opacity-50" />
            <p className="text-sm">Nenhuma busca realizada</p>
            <p className="text-xs mt-1">Digite um termo e clique em buscar</p>
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <AlertCircle className="h-12 w-12 mb-4 opacity-50" />
            <p className="text-sm">Nenhum resultado encontrado</p>
            <p className="text-xs mt-1">"{query}" não foi encontrado no índice de Secrets/ConfigMaps</p>
          </div>
        ) : filtered.length === 0 ? (
          <div className="text-muted-foreground text-xs text-center py-6">
            Nenhum resultado para os filtros aplicados
          </div>
        ) : (
          filtered.map((rec) => {
            const key = rowKeyOf(rec);
            const revealed = revealedRows.has(key);
            const showBase64 = revealedAsBase64.has(key);
            const displayValue = rec.is_binary
              ? rec.value_base64
              : (showBase64 ? rec.value_base64 : rec.value_decoded) || rec.value_base64;

            return (
              <div
                key={key}
                className="grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs text-foreground"
                style={{ gridTemplateColumns: gridTemplate }}
              >
                <div className="flex flex-col gap-1 min-w-0 pr-2">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-semibold truncate" title={rec.data_key}>{rec.data_key}</span>
                    <button
                      type="button"
                      onClick={() => toggleRevealed(key)}
                      className="text-muted-foreground hover:text-foreground shrink-0"
                      title={revealed ? "Ocultar valor" : "Revelar valor"}
                    >
                      {revealed ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                    </button>
                  </div>
                  {revealed ? (
                    <div className="flex items-start gap-1 min-w-0">
                      <span
                        className="break-all max-w-md text-xs text-muted-foreground"
                        title={rec.truncated ? "Valor truncado no armazenamento (>8KB) — não é o valor completo" : undefined}
                      >
                        {displayValue}
                        {rec.truncated ? "…" : ""}
                      </span>
                      <button
                        type="button"
                        onClick={() => copyValue(displayValue)}
                        className="text-muted-foreground hover:text-foreground shrink-0"
                        title="Copiar valor"
                      >
                        <Copy className="h-3 w-3" />
                      </button>
                      {!rec.is_binary && (
                        <button
                          type="button"
                          onClick={() => toggleBase64(key)}
                          className="text-[10px] text-muted-foreground underline hover:text-foreground shrink-0"
                        >
                          {showBase64 ? "decodificado" : "base64"}
                        </button>
                      )}
                    </div>
                  ) : (
                    <span className="text-xs text-muted-foreground select-none">••••••••</span>
                  )}
                </div>
                <span className="truncate self-start pt-0.5" title={rec.cluster}>{rec.cluster.replace(/-admin$/, "")}</span>
                <span className="truncate self-start pt-0.5" title={rec.namespace}>{rec.namespace}</span>
                <div className="flex items-center gap-1.5 min-w-0 self-start pt-0.5">
                  <span className="truncate text-muted-foreground" title={`${rec.resource_kind}: ${rec.resource_name}`}>
                    {rec.resource_kind}: {rec.resource_name}
                  </span>
                  {rec.resource_kind === "secret" && rec.resource_name.toLowerCase().includes("akv") && (
                    <ProtectedAction>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-5 px-1.5 gap-1 text-[10px] border-blue-500/40 text-blue-500 hover:bg-blue-500/10 hover:text-blue-400 shrink-0"
                        title="Força o external-secrets a ressincronizar com o Azure Key Vault"
                        onClick={() => onResyncClick(rec.cluster, rec.namespace, rec.resource_name)}
                      >
                        <RefreshCcw className="h-2.5 w-2.5" />
                        Resync
                      </Button>
                    </ProtectedAction>
                  )}
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
