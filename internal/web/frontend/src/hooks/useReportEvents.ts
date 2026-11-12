import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { EVENTS_KEY, mergeEvents, parseEvents, removeEvent, upsertEvent, type SavedEvent } from "@/lib/metricsEvents";

function loadLocal(): SavedEvent[] {
  try {
    return parseEvents(localStorage.getItem(EVENTS_KEY));
  } catch {
    return [];
  }
}
function saveLocal(list: SavedEvent[]) {
  try {
    localStorage.setItem(EVENTS_KEY, JSON.stringify(list));
  } catch {
    /* sem armazenamento: segue só com o servidor */
  }
}

/**
 * Eventos nomeados do Relatório de Métricas. O servidor é a fonte (por usuário, sobrevive a reinício e a
 * troca de navegador); o localStorage é só cache. Ao abrir, junta os dois sem perder evento.
 */
export function useReportEvents() {
  const [events, setEvents] = useState<SavedEvent[]>(loadLocal);

  useEffect(() => {
    let cancelled = false;
    apiClient
      .metricsReportEventsGet()
      .then((r) => {
        if (cancelled) return;
        const server = parseEvents(r.events);
        setEvents((local) => {
          const merged = mergeEvents(server, local);
          saveLocal(merged);
          if (merged.length !== server.length) apiClient.metricsReportEventsPut(merged).catch(() => {});
          return merged;
        });
      })
      .catch(() => {
        /* servidor indisponível: segue com o cache do navegador */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const persist = useCallback((next: SavedEvent[]) => {
    setEvents(next);
    saveLocal(next);
    apiClient.metricsReportEventsPut(next).catch(() => toast.error("Não consegui gravar os eventos no servidor (ficaram só neste navegador)"));
  }, []);

  const save = useCallback((name: string, start: string, end: string) => {
    const next = upsertEvent(events, name, start, end);
    if (next === events) {
      toast.error("Informe um nome e uma janela válida (fim depois do início)");
      return false;
    }
    persist(next);
    toast.success(`Evento "${name.trim()}" salvo`);
    return true;
  }, [events, persist]);

  const remove = useCallback((name: string) => persist(removeEvent(events, name)), [events, persist]);

  return { events, save, remove };
}
