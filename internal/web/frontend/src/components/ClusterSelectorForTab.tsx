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
              {selectedCluster || "Selecione ou busque um cluster..."}
              <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[280px] p-0">
            <Command>
              <CommandInput placeholder="Buscar cluster..." />
              <CommandList>
                <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                <CommandGroup>
                  {clusters.map((cluster) => {
                    const badge = clusterProviders
                      ? cloudProviderBadge(clusterProviders[cluster])
                      : null;
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
                        {cluster}
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
