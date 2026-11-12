import { useState } from "react";
import { CalendarClock, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { eventLabel, type SavedEvent } from "@/lib/metricsEvents";

/**
 * Eventos nomeados: janelas de tempo salvas (ex.: "Stress test 11/11 14h-16h"). Clicar num evento aplica a
 * janela; "Salvar janela atual" guarda a janela que está nos campos de início e fim.
 */
export function MetricsEventPicker({
  events, current, onApply, onSave, onRemove, disabled,
}: {
  events: SavedEvent[];
  /** Janela atual em ISO, ou nulo quando os campos estão inválidos. */
  current: { start: string; end: string } | null;
  onApply: (start: string, end: string) => void;
  onSave: (name: string, start: string, end: string) => boolean;
  onRemove: (name: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={disabled} title="Eventos nomeados: janelas de tempo salvas (ex.: stress test)">
          <CalendarClock className="h-3.5 w-3.5" />Eventos{events.length > 0 ? ` (${events.length})` : ""}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[26rem] p-2 space-y-2" align="start">
        {events.length === 0 ? (
          <div className="text-xs text-muted-foreground px-1">Nenhum evento salvo. Ajuste a janela em <b>Personalizado</b> (início e fim), dê um nome e salve.</div>
        ) : (
          <ul className="max-h-60 overflow-y-auto space-y-0.5">
            {events.map((e) => (
              <li key={e.name} className="flex items-center gap-1 rounded px-1 py-0.5 hover:bg-muted/50">
                <button className="flex-1 text-left text-xs" title="Aplicar esta janela" onClick={() => { onApply(e.start, e.end); setOpen(false); }}>
                  {eventLabel(e)}
                </button>
                <button className="text-muted-foreground hover:text-red-400" title={`Apagar o evento ${e.name}`} onClick={() => onRemove(e.name)}>
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex items-center gap-1 border-t border-border pt-2">
          <Input className="h-7 text-xs" placeholder="Nome do evento" value={name} onChange={(e) => setName(e.target.value)} />
          <Button size="icon" variant="outline" className="h-7 w-7 flex-shrink-0" disabled={!name.trim() || !current} title={current ? "Salvar a janela atual como evento" : "A janela atual é inválida"} onClick={() => current && onSave(name, current.start, current.end) && setName("")}>
            <Save className="h-3.5 w-3.5" />
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
