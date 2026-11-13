import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { DEFAULT_THRESHOLDS, THRESHOLDS_KEY, isDefaultThresholds, normalizeThresholds, type Thresholds } from "@/lib/metricsThresholds";

function loadLocal(): Thresholds {
  try {
    return normalizeThresholds(localStorage.getItem(THRESHOLDS_KEY));
  } catch {
    return { ...DEFAULT_THRESHOLDS };
  }
}
function saveLocal(t: Thresholds) {
  try {
    localStorage.setItem(THRESHOLDS_KEY, JSON.stringify(t));
  } catch {
    /* sem armazenamento: segue só com o servidor */
  }
}

/**
 * Limites de alerta do Tempo real. O servidor é a fonte (por usuário, sobrevive a reinício e a troca de
 * navegador); o localStorage é só cache. Ao abrir, o que o servidor tem vale; se ele não tem nada e o navegador
 * tem limites próprios, eles sobem para o servidor.
 */
export function useLiveThresholds() {
  const [thresholds, setThresholds] = useState<Thresholds>(loadLocal);

  useEffect(() => {
    let cancelled = false;
    apiClient
      .metricsLiveThresholdsGet()
      .then((r) => {
        if (cancelled) return;
        const list = Array.isArray(r.thresholds) ? r.thresholds : [];
        if (list.length > 0) {
          const t = normalizeThresholds(list);
          setThresholds(t);
          saveLocal(t);
        } else {
          const local = loadLocal();
          if (!isDefaultThresholds(local)) apiClient.metricsLiveThresholdsPut([local]).catch(() => {});
        }
      })
      .catch(() => {
        /* servidor indisponível: segue com o cache do navegador */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const save = useCallback((next: Thresholds) => {
    const t = normalizeThresholds(next);
    setThresholds(t);
    saveLocal(t);
    apiClient.metricsLiveThresholdsPut([t]).catch(() => toast.error("Não consegui gravar os limites no servidor (ficaram só neste navegador)"));
  }, []);

  const reset = useCallback(() => save({ ...DEFAULT_THRESHOLDS }), [save]);

  return { thresholds, save, reset };
}
