import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ChevronsUpDown } from "lucide-react";
import { cn } from "@/lib/utils";

interface ClusterJourneyComboboxProps {
  journeyOptions: string[];
  selectedJourneys: Set<string>;
  onToggleJourney: (value: string) => void;
  /** "header" = combobox principal (fundo colorido do Header.tsx, texto claro);
   *  "light" = dentro de uma aba (ClusterSelectorForTab.tsx, fundo claro/muted). */
  variant?: "header" | "light";
}

// Combobox próprio pro filtro de jornada (tag Azure "jornada") — separado do combobox de
// seleção de cluster, sentado ao lado dele. Passou por 2 rodadas antes desta: primeiro os
// checkboxes viviam dentro do popover do próprio combobox de cluster (ficou espremido numa
// largura fixa de 280-400px); depois uma tentativa de empilhar verticalmente ao lado do botão
// (ainda ocupava espaço fixo no header). Pedido final do usuário: um combobox à parte, só
// abrindo quando clicado — some do layout quando fechado, igual ao combobox de cluster ao lado.
// Só existe quando há 2+ valores distintos de jornada; com 0 ou 1, retorna null.
export const ClusterJourneyCombobox = ({
  journeyOptions,
  selectedJourneys,
  onToggleJourney,
  variant = "light",
}: ClusterJourneyComboboxProps) => {
  const [open, setOpen] = useState(false);

  if (journeyOptions.length < 2) return null;

  const activeCount = selectedJourneys.size;
  // Nenhum ou todos marcados = "Todas as jornadas" (sem filtro ativo) — mesma regra já
  // combinada com o usuário pro comportamento do filtro em si.
  const isFiltering = activeCount > 0 && activeCount < journeyOptions.length;
  const label = isFiltering
    ? activeCount === 1
      ? Array.from(selectedJourneys)[0]
      : `${activeCount} jornadas`
    : "Todas as jornadas";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          size="sm"
          className={cn(
            "h-9 justify-between gap-1 text-xs w-[150px]",
            variant === "header" &&
              cn(
                "bg-white/20 border-white/30 text-white hover:bg-white/25 hover:text-white",
                isFiltering && "bg-white/30 border-white/50 font-semibold"
              ),
            variant === "light" && isFiltering && "border-primary text-primary font-semibold"
          )}
        >
          <span className="truncate">{label}</span>
          <ChevronsUpDown className="h-3.5 w-3.5 opacity-60 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[180px] p-1.5" align="start">
        <div className="flex flex-col gap-0.5">
          {journeyOptions.map((journey) => {
            const checked = selectedJourneys.has(journey);
            return (
              <label
                key={journey}
                className="flex items-center gap-2 text-xs px-1.5 py-1 rounded cursor-pointer hover:bg-muted"
              >
                <Checkbox
                  checked={checked}
                  onCheckedChange={() => onToggleJourney(journey)}
                  className="w-3.5 h-3.5"
                />
                <span className={cn(checked && "font-medium text-foreground")}>{journey}</span>
              </label>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
};
