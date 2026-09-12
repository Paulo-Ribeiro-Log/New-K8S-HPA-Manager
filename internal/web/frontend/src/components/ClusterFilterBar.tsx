import { cn } from "@/lib/utils";
import type { EnvFilter } from "@/hooks/useClusterEnvFilter";

interface ClusterFilterBarProps {
  envFilter: EnvFilter;
  onEnvFilterChange: (value: EnvFilter) => void;
}

// Barra Todos/HLG/PRD compartilhada por Header.tsx e ClusterSelectorForTab.tsx — antes cada um
// duplicava o mesmo JSX. O filtro de jornada (tag Azure "jornada") vive num componente à parte,
// ClusterJourneyFilter.tsx, renderizado FORA do popover, ao lado do botão do combobox — dentro
// do popover (largura fixa de 280-400px) os checkboxes de jornada ficavam espremidos/quebrando
// linha junto com estes botões, pedido explícito do usuário pra mover pra fora.
export const ClusterFilterBar = ({ envFilter, onEnvFilterChange }: ClusterFilterBarProps) => {
  return (
    <div className="flex items-center gap-1 px-2 pt-2 pb-1.5 border-b border-border">
      {(["all", "hlg", "prd"] as const).map((f) => (
        <button
          key={f}
          type="button"
          onClick={() => onEnvFilterChange(f)}
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
  );
};
