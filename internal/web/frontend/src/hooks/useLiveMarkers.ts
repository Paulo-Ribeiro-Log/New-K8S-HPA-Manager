import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { MARKERS_KEY, addMarker, mergeMarkers, parseMarkers, removeMarker, type Marker } from "@/lib/metricsMarkers";

function loadLocal(): Marker[] {
  try {
    return parseMarkers(localStorage.getItem(MARKERS_KEY));
  } catch {
    return [];
  }
}
function saveLocal(list: Marker[]) {
  try {
    localStorage.setItem(MARKERS_KEY, JSON.stringify(list));
  } catch {
    /* sem armazenamento: segue só com o servidor */
  }
}

/**
 * Marcadores pontuais do Tempo real. O servidor é a fonte (por usuário, sobrevive a reinício e a troca de
 * navegador); o localStorage é só cache. Ao abrir, junta os dois sem perder marcador.
 */
export function useLiveMarkers() {
  const [markers, setMarkers] = useState<Marker[]>(loadLocal);

  useEffect(() => {
    let cancelled = false;
    apiClient
      .metricsLiveMarkersGet()
      .then((r) => {
        if (cancelled) return;
        const server = parseMarkers(r.markers);
        setMarkers((local) => {
          const merged = mergeMarkers(server, local);
          saveLocal(merged);
          if (merged.length !== server.length) apiClient.metricsLiveMarkersPut(merged).catch(() => {});
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

  const persist = useCallback((next: Marker[]) => {
    setMarkers(next);
    saveLocal(next);
    apiClient.metricsLiveMarkersPut(next).catch(() => toast.error("Não consegui gravar os marcadores no servidor (ficaram só neste navegador)"));
  }, []);

  /** Marca AGORA com o nome dado. Devolve false com nome vazio. */
  const add = useCallback((name: string) => {
    const next = addMarker(markers, name);
    if (next === markers) return false;
    persist(next);
    return true;
  }, [markers, persist]);

  const remove = useCallback((id: string) => persist(removeMarker(markers, id)), [markers, persist]);

  return { markers, add, remove };
}
