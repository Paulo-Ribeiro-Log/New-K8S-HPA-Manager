import { useState } from "react";
import { Flag, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { MARKER_NAME_MAX, markerTime, type Marker } from "@/lib/metricsMarkers";

/**
 * "Marcar agora": guarda um instante com um nome (ex.: "Subi o limite do HPA") que aparece como linha vertical
 * nos gráficos do modal e no resumo da sessão. Enter marca; a lista mostra os mais recentes e apaga.
 */
export function MetricsMarkerButton({ markers, onAdd, onRemove }: { markers: Marker[]; onAdd: (name: string) => boolean; onRemove: (id: string) => void }) {
  const [name, setName] = useState("");
  const submit = () => {
    if (onAdd(name)) setName("");
  };
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" title="Marcar o instante de agora com um nome; aparece nos gráficos do modal e no resumo da sessão">
          <Flag className="h-3.5 w-3.5" />Marcar{markers.length > 0 ? ` (${markers.length})` : ""}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[24rem] space-y-2 p-3 text-xs">
        <div className="flex items-center gap-1">
          <Input
            className="h-7 text-xs"
            placeholder="O que aconteceu agora? (ex.: subi o limite do HPA)"
            maxLength={MARKER_NAME_MAX}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
          <Button size="sm" className="h-7 text-xs flex-shrink-0" disabled={!name.trim()} onClick={submit}>Marcar agora</Button>
        </div>
        <div className="text-[10px] text-muted-foreground">Grava a hora de agora. O marcador aparece como linha vertical nos gráficos do modal (quando a hora cai dentro do período) e no resumo da sessão.</div>
        {markers.length > 0 && (
          <ul className="max-h-48 space-y-0.5 overflow-y-auto border-t border-border pt-2">
            {markers.slice(0, 30).map((m) => (
              <li key={m.id} className="flex items-center gap-2 rounded px-1 py-0.5 hover:bg-muted/50">
                <span className="w-20 flex-shrink-0 tabular-nums text-muted-foreground">{markerTime(m.at)}</span>
                <span className="min-w-0 flex-1 truncate" title={m.name}>{m.name}</span>
                <button type="button" className="text-muted-foreground hover:text-red-400" title="Apagar este marcador" onClick={() => onRemove(m.id)}>
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}
