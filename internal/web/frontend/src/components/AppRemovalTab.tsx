import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronsUpDown, Loader2, PackageX, ScanSearch, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { apiClient } from "@/lib/api/client";
import { useClusters } from "@/hooks/useAPI";
import { cn } from "@/lib/utils";
import type {
  AppRemovalCandidate,
  AppRemovalItem,
  AppRemovalOrigin,
  AppRemovalScanResult,
} from "@/lib/api/types";

const ORIGIN_LABEL: Record<AppRemovalOrigin, string> = {
  helm: "Helm",
  kustomize: "Kustomize",
  "kustomize-provavel": "Kustomize (provável)",
  manual: "Manual",
};

function SearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  emptyMessage,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  placeholder: string;
  searchPlaceholder: string;
  emptyMessage: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const selected = options.find((o) => o.value === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">{selected?.label || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt.value}
                  value={opt.label}
                  onSelect={() => {
                    onChange(opt.value === value ? "" : opt.value);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === opt.value ? "opacity-100" : "opacity-0")} />
                  {opt.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function candidateKey(c: AppRemovalCandidate) {
  return `${c.kind}/${c.name}`;
}

function ItemRow({ item }: { item: AppRemovalItem }) {
  return (
    <tr className="border-t align-top">
      <td className="px-3 py-2 font-mono text-xs">{item.name}</td>
      <td className="px-3 py-2">
        <Badge variant="outline" className="text-[10px]">
          {ORIGIN_LABEL[item.origin]}
        </Badge>
      </td>
      <td className="px-3 py-2 text-xs text-muted-foreground">
        {item.reason}
        {item.used_by && item.used_by.length > 0 && (
          <div className="mt-1 text-amber-600 dark:text-amber-400">
            Também usado por: {item.used_by.join(", ")}
          </div>
        )}
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap gap-1">
          {item.shared && <Badge variant="outline" className="text-[10px] border-amber-500 text-amber-600">Compartilhado</Badge>}
          {item.data_risk && <Badge variant="outline" className="text-[10px] border-red-500 text-red-600">Dados</Badge>}
          {item.removed_by_helm && <Badge variant="outline" className="text-[10px]">Removido pelo Helm</Badge>}
        </div>
      </td>
      <td className="px-3 py-2 text-xs whitespace-nowrap">
        {item.default_selected ? (
          <span className="text-green-600 dark:text-green-400">Remover</span>
        ) : (
          <span className="text-muted-foreground">Manter</span>
        )}
      </td>
    </tr>
  );
}

/**
 * AppRemovalTab — "Remoção de Aplicação" (Tools menu). Fase 1: só leitura. Descobre tudo o que
 * pertence a uma aplicação (workloads, Services, Ingress, config, dados, RBAC), indica o
 * gerenciador (Helm / Kustomize provável / manual) e a seleção sugerida. Nada é removido.
 */
export default function AppRemovalTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [target, setTarget] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState<string | null>(null);
  const [result, setResult] = useState<AppRemovalScanResult | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["app-removal-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const { data: candidatesResp, isLoading: loadingCandidates, isError: candidatesError } = useQuery({
    queryKey: ["app-removal-candidates", cluster, namespace],
    queryFn: () => apiClient.listAppRemovalCandidates(cluster, namespace),
    enabled: !!cluster && !!namespace,
  });
  const candidates = candidatesResp?.candidates ?? [];
  const targetCandidate = candidates.find((c) => candidateKey(c) === target);

  const resetResult = () => {
    setResult(null);
    setScanError(null);
  };

  const handleScan = async () => {
    if (!targetCandidate) return;
    setScanning(true);
    resetResult();
    try {
      setResult(await apiClient.scanAppRemoval(cluster, namespace, targetCandidate.name));
    } catch (e) {
      setScanError(e instanceof Error ? e.message : "Erro desconhecido");
    } finally {
      setScanning(false);
    }
  };

  const groups = result
    ? result.items.reduce<{ kind: string; items: AppRemovalItem[] }[]>((acc, item) => {
        const last = acc[acc.length - 1];
        if (last && last.kind === item.kind) last.items.push(item);
        else acc.push({ kind: item.kind, items: [item] });
        return acc;
      }, [])
    : [];

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-4">
      <div className="flex items-center gap-2">
        <PackageX className="h-5 w-5 text-primary" />
        <h1 className="text-lg font-semibold">Remoção de Aplicação</h1>
      </div>
      <p className="text-sm text-muted-foreground">
        Descobre tudo o que pertence a uma aplicação no namespace (workloads, Services, Ingress,
        config, dados e RBAC) e se ela é gerenciada por Helm ou Kustomize. Nesta etapa a ferramenta
        só lê o cluster: nada é removido.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div>
          <Label className="text-xs">Cluster</Label>
          <div className="mt-1">
            <SearchableSelect
              value={cluster}
              onChange={(v) => {
                setCluster(v);
                setNamespace("");
                setTarget("");
                resetResult();
              }}
              options={clusters.map((c) => ({ value: c.context, label: c.context }))}
              placeholder="Selecione..."
              searchPlaceholder="Buscar cluster..."
              emptyMessage="Nenhum cluster encontrado."
            />
          </div>
        </div>
        <div>
          <Label className="text-xs">Namespace</Label>
          <div className="mt-1">
            <SearchableSelect
              value={namespace}
              onChange={(v) => {
                setNamespace(v);
                setTarget("");
                resetResult();
              }}
              options={namespaces.map((n) => ({ value: n.name, label: n.name }))}
              placeholder="Selecione..."
              searchPlaceholder="Buscar namespace..."
              emptyMessage="Nenhum namespace encontrado."
              disabled={!cluster}
            />
          </div>
        </div>
        <div>
          <Label className="text-xs">Aplicação</Label>
          <div className="mt-1">
            <SearchableSelect
              value={target}
              onChange={(v) => {
                setTarget(v);
                resetResult();
              }}
              options={candidates.map((c) => ({
                value: candidateKey(c),
                label: c.kind === "HelmRelease" ? `${c.name} (release Helm)` : `${c.name} (${c.kind})`,
              }))}
              placeholder={loadingCandidates ? "Carregando..." : "Selecione..."}
              searchPlaceholder="Buscar aplicação..."
              emptyMessage="Nenhum workload encontrado."
              disabled={!namespace || loadingCandidates}
            />
          </div>
        </div>
      </div>

      {candidatesError && (
        <Alert variant="destructive">
          <AlertTitle>Falha ao listar aplicações</AlertTitle>
          <AlertDescription>Verifique o acesso ao cluster e tente novamente.</AlertDescription>
        </Alert>
      )}

      <Button onClick={handleScan} disabled={!targetCandidate || scanning}>
        {scanning ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <ScanSearch className="h-4 w-4 mr-2" />}
        Escanear dependências
      </Button>

      {scanError && (
        <Alert variant="destructive">
          <AlertTitle>Falha no scan</AlertTitle>
          <AlertDescription>{scanError}</AlertDescription>
        </Alert>
      )}

      {result && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium">{result.name}</span>
            <Badge variant="outline">{ORIGIN_LABEL[result.origin]}</Badge>
            {result.helm && (
              <span className="text-muted-foreground">
                release <code>{result.helm.release}</code> · {result.helm.revisions} revisões
              </span>
            )}
            <span className="text-muted-foreground">· {result.items.length} itens</span>
          </div>

          {result.warnings.map((w) => (
            <Alert key={w}>
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>{w}</AlertDescription>
            </Alert>
          ))}

          {groups.map((g) => (
            <div key={g.kind} className="rounded-md border overflow-hidden">
              <div className="bg-muted px-3 py-1.5 text-xs font-semibold">
                {g.kind} <span className="text-muted-foreground font-normal">({g.items.length})</span>
              </div>
              <table className="w-full text-left">
                <thead className="sr-only">
                  <tr>
                    <th>Nome</th>
                    <th>Origem</th>
                    <th>Motivo</th>
                    <th>Avisos</th>
                    <th>Sugestão</th>
                  </tr>
                </thead>
                <tbody>
                  {g.items.map((item) => (
                    <ItemRow key={`${item.kind}/${item.name}`} item={item} />
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
