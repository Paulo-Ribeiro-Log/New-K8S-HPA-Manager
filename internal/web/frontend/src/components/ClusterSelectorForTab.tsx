import { useState } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Loader2, ChevronsUpDown, Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { cloudProviderBadge } from "@/hooks/useCloudProvider";
import { isProdClusterName, isHlgClusterName } from "@/lib/clusterSafety";

interface ClusterSelectorForTabProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
  tabLabel: string;
  isLoading?: boolean;
  /** Mapa de context → cloud_provider para exibir badges */
  clusterProviders?: Record<string, string>;
}

// Combobox com busca embutida no mesmo popover (padrão já usado em Header.tsx) —
// evita o bug do <Select> do Radix fechar o dropdown ao focar um campo de busca externo.
export const ClusterSelectorForTab = ({
  selectedCluster,
  onClusterChange,
  clusters,
  tabLabel,
  isLoading = false,
  clusterProviders,
}: ClusterSelectorForTabProps) => {
  const [open, setOpen] = useState(false);
  // Mesma correção de segurança do combobox de cluster do Header.tsx (reaproveitando
  // isProdClusterName/isHlgClusterName, mesma fonte única já usada nesta app) — analista
  // sobrecarregado não deveria selecionar PRD por engano numa ferramenta de aba específica.
  // isProdClusterName é uma detecção AMPLA (qualquer "produ*" no nome, não só o sufixo "-prd"
  // exato) — pedido explícito do usuário pra cobrir clusters como um EKS "asaplog-production".
  const [envFilter, setEnvFilter] = useState<"all" | "hlg" | "prd">("all");
  const filteredClusters = clusters.filter((c) => {
    if (envFilter === "all") return true;
    if (envFilter === "prd") return isProdClusterName(c);
    return isHlgClusterName(c);
  });
  const selectedIsProd = isProdClusterName(selectedCluster);

  return (
    <div className="px-6 py-3 bg-muted/30 border-b">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-muted-foreground">
            {tabLabel} - Cluster Context:
          </span>
          {isLoading && (
            <Loader2 className="w-4 h-4 animate-spin text-primary" />
          )}
        </div>

        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              role="combobox"
              aria-expanded={open}
              disabled={isLoading}
              className="w-[280px] justify-between"
            >
              <span className={cn("truncate", selectedIsProd && "text-amber-600 dark:text-amber-400 font-semibold")}>
                {selectedCluster || "Selecione ou busque um cluster..."}
              </span>
              <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[280px] p-0">
            {/* Filtro Todos/HLG/PRD — mesmo padrão do combobox principal (Header.tsx). */}
            <div className="flex items-center gap-1 px-2 pt-2 pb-1.5 border-b border-border">
              {(["all", "hlg", "prd"] as const).map((f) => (
                <button
                  key={f}
                  type="button"
                  onClick={() => setEnvFilter(f)}
                  className={cn(
                    "text-xs px-2 py-1 rounded",
                    envFilter === f
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted"
                  )}
                >
                  {f === "all" ? "Todos" : f.toUpperCase()}
                </button>
              ))}
            </div>
            <Command>
              <CommandInput placeholder="Buscar cluster..." />
              <CommandList>
                <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                <CommandGroup>
                  {filteredClusters.map((cluster) => {
                    const badge = clusterProviders
                      ? cloudProviderBadge(clusterProviders[cluster])
                      : null;
                    const isProd = isProdClusterName(cluster);
                    return (
                      <CommandItem
                        key={cluster}
                        value={cluster}
                        onSelect={() => {
                          onClusterChange(cluster === selectedCluster ? "" : cluster);
                          setOpen(false);
                        }}
                      >
                        <Check
                          className={cn(
                            "mr-2 h-4 w-4",
                            selectedCluster === cluster ? "opacity-100" : "opacity-0"
                          )}
                        />
                        {badge && (
                          <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded mr-1.5 ${badge.className}`}>
                            {badge.label}
                          </span>
                        )}
                        <span className={isProd ? "text-amber-600 dark:text-amber-400 font-medium" : undefined}>
                          {cluster}
                        </span>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
};
