import { useEffect, useRef, useState } from "react";
import { AlertCircle, AlertTriangle, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { usePrometheusHealthAlerts } from "@/hooks/usePrometheusHealth";

interface PrometheusHealthAlertsProps {
  cluster: string;
  /** Leva à aba Prometheus (diagnóstico do WAL). */
  onOpenPrometheus: () => void;
}

const DISMISS_KEY = "prometheus_health_dismissed";

const readDismissed = (): string[] => {
  try {
    return JSON.parse(sessionStorage.getItem(DISMISS_KEY) || "[]");
  } catch {
    return [];
  }
};

// Alertas de integridade do Prometheus do cluster ativo: fora do ar e limpeza interrompida (críticos, não
// dispensáveis) e reincidência do problema de WAL (aviso, dispensável na sessão).
export function PrometheusHealthAlerts({ cluster, onOpenPrometheus }: PrometheusHealthAlertsProps) {
  const { items } = usePrometheusHealthAlerts(cluster);
  const [dismissed, setDismissed] = useState<string[]>(readDismissed);
  const toasted = useRef(new Set<string>());

  const keyOf = (i: { namespace: string; name: string; state: string }) => `${cluster}|${i.namespace}/${i.name}|${i.state}`;

  // Um aviso (toast) por problema novo na sessão: não repete a cada consulta de 60 s.
  useEffect(() => {
    for (const i of items) {
      const key = keyOf(i);
      if (toasted.current.has(key)) continue;
      toasted.current.add(key);
      const title = `Prometheus ${i.namespace}/${i.name}`;
      if (i.severity === "critical") toast.error(title, { description: i.summary });
      else toast.warning(title, { description: i.summary });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, cluster]);

  const dismiss = (key: string) => {
    const next = [...dismissed, key];
    setDismissed(next);
    try {
      sessionStorage.setItem(DISMISS_KEY, JSON.stringify(next));
    } catch {
      /* sem sessionStorage: o aviso volta na próxima sessão */
    }
  };

  const visible = items.filter((i) => i.severity === "critical" || !dismissed.includes(keyOf(i)));
  if (visible.length === 0) return null;

  return (
    <div className="space-y-2 px-6 py-2 flex-shrink-0" data-testid="prometheus-health-alerts">
      {visible.map((i) => {
        const critical = i.severity === "critical";
        const Icon = critical ? AlertCircle : AlertTriangle;
        return (
          <div
            key={keyOf(i)}
            className={cn(
              "flex items-start gap-3 rounded-md border p-3 text-sm",
              critical
                ? "border-red-500/50 bg-red-500/10 text-red-800 dark:text-red-300"
                : "border-amber-500/50 bg-amber-500/10 text-amber-800 dark:text-amber-300"
            )}
          >
            <Icon className="w-4 h-4 mt-0.5 shrink-0" />
            <div className="flex-1 min-w-0">
              <p className="font-semibold">
                {critical ? "Prometheus com problema" : "Problema recorrente no Prometheus"} — {i.namespace}/{i.name}
              </p>
              <p className="text-xs mt-0.5">{i.summary}</p>
            </div>
            <Button size="sm" variant="outline" onClick={onOpenPrometheus} className="shrink-0">
              Abrir diagnóstico do WAL
            </Button>
            {!critical && (
              <button
                type="button"
                onClick={() => dismiss(keyOf(i))}
                className="shrink-0 opacity-70 hover:opacity-100"
                aria-label="Dispensar aviso nesta sessão"
                title="Dispensar nesta sessão"
              >
                <X className="w-4 h-4" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
