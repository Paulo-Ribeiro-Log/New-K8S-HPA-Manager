import { useEffect, useRef, useState } from "react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2, Gauge, Play, XCircle, AlertTriangle } from "lucide-react";
import { ProtectedAction } from "@/components/rbac";
import { useClusters } from "@/hooks/useAPI";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { LatencyTestResult, LatencyTestSSEEvent, LatencyTestStats } from "@/lib/api/types";

interface HistoryEntry {
  id: string;
  timestamp: string;
  cluster: string;
  namespace: string;
  url: string;
  stats: LatencyTestStats;
  errorCount: number;
  totalRequests: number;
}

// Extrai a porta de serviço do primeiro item de `ports` (formato "80:8080/TCP" do backend) —
// só um atalho pra pré-preencher a URL, o campo continua editável depois.
function firstServicePort(ports: string[]): string {
  if (!ports || ports.length === 0) return "";
  return ports[0].split(/[:/]/)[0] || "";
}

function StatCard({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className={`rounded-md border p-3 ${highlight ? "border-primary/40 bg-primary/5" : "border-border"}`}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold font-mono">{value}</div>
    </div>
  );
}

export default function LatencyTestTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [selectedService, setSelectedService] = useState("");
  const [url, setUrl] = useState("");
  const [requests, setRequests] = useState(20);
  const [timeoutMs, setTimeoutMs] = useState(3000);

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState(0);
  const [phaseMessage, setPhaseMessage] = useState("");
  const [result, setResult] = useState<LatencyTestResult | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const esRef = useRef<EventSource | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-latency-test", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const { data: services = [] } = useQuery({
    queryKey: ["services-latency-test", cluster, namespace],
    queryFn: () => apiClient.getServices(cluster, [namespace]),
    enabled: !!cluster && !!namespace,
  });

  const canRun = !!cluster && !!namespace && !!url.trim() && !isRunning;

  const handleServiceChange = (name: string) => {
    setSelectedService(name);
    const svc = services.find((s) => s.name === name);
    if (svc) {
      const port = firstServicePort(svc.ports);
      const portSuffix = port ? `:${port}` : "";
      setUrl(`http://${svc.name}.${svc.namespace}.svc.cluster.local${portSuffix}`);
    }
  };

  const runTest = async () => {
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando teste de latência...");
    setIsRunning(true);
    try {
      const { session_id } = await apiClient.runLatencyTest({
        cluster,
        namespace,
        url: url.trim(),
        requests,
        timeout_ms: timeoutMs,
      });
      setSessionId(session_id);
    } catch (err) {
      setIsRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o teste");
    }
  };

  const cancelTest = async () => {
    if (!sessionId) return;
    try {
      await apiClient.cancelLatencyTest(sessionId);
    } catch {
      /* ignore — o pod é limpo no servidor de qualquer forma */
    }
    esRef.current?.close();
    setIsRunning(false);
    setPhaseMessage("Teste cancelado.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getLatencyTestStreamURL(sessionId));
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: LatencyTestSSEEvent = JSON.parse(e.data);
        setPhaseMessage(event.message);
        setProgress(event.progress);

        if (event.type === "complete" && event.result) {
          setResult(event.result);
          setIsRunning(false);
          setHistory((prev) => [
            {
              id: sessionId,
              timestamp: event.timestamp,
              cluster,
              namespace,
              url: url.trim(),
              stats: event.result!.stats,
              errorCount: event.result!.error_count,
              totalRequests: event.result!.total_requests,
            },
            ...prev,
          ]);
        }
        if (event.type === "error") {
          setRunError(event.error || event.message);
          setIsRunning(false);
        }
      } catch {
        /* ignore evento malformado */
      }
    };

    es.onerror = () => {
      es.close();
      setIsRunning(false);
    };

    return () => {
      es.close();
      esRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const chartData = result?.samples.map((ms, i) => ({ n: i + 1, ms: Number(ms.toFixed(1)) })) ?? [];

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-wrap items-end gap-3">
        <div className="min-w-[220px]">
          <ClusterSelectorForTab
            selectedCluster={cluster}
            onClusterChange={(v) => {
              setCluster(v);
              setNamespace("");
              setSelectedService("");
              setUrl("");
              setResult(null);
            }}
            clusters={clusters.map((c) => c.context)}
            tabLabel="Teste de Latência"
            clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
          />
        </div>

        <div className="min-w-[180px]">
          <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
          <Select
            value={namespace}
            onValueChange={(v) => {
              setNamespace(v);
              setSelectedService("");
            }}
            disabled={!cluster}
          >
            <SelectTrigger>
              <SelectValue placeholder="Selecione o namespace" />
            </SelectTrigger>
            <SelectContent>
              {namespaces.map((ns) => (
                <SelectItem key={ns.name} value={ns.name}>
                  {ns.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="min-w-[200px]">
          <label className="text-xs text-muted-foreground block mb-1">Service (atalho, opcional)</label>
          <Select value={selectedService} onValueChange={handleServiceChange} disabled={!namespace}>
            <SelectTrigger>
              <SelectValue placeholder="Preencher URL a partir de um Service" />
            </SelectTrigger>
            <SelectContent>
              {services.map((s) => (
                <SelectItem key={s.name} value={s.name}>
                  {s.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="min-w-[280px] flex-1">
          <label className="text-xs text-muted-foreground block mb-1">URL alvo</label>
          <Input
            placeholder="http://meu-service.meu-namespace.svc.cluster.local:8080/health"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
        </div>

        <div className="w-24">
          <label className="text-xs text-muted-foreground block mb-1">Requisições</label>
          <Input
            type="number"
            min={1}
            max={200}
            value={requests}
            onChange={(e) => setRequests(Math.min(200, Math.max(1, Number(e.target.value) || 1)))}
          />
        </div>

        <div className="w-28">
          <label className="text-xs text-muted-foreground block mb-1">Timeout (ms)</label>
          <Input
            type="number"
            min={100}
            max={10000}
            value={timeoutMs}
            onChange={(e) => setTimeoutMs(Math.min(10000, Math.max(100, Number(e.target.value) || 100)))}
          />
        </div>

        <ProtectedAction>
          {!isRunning ? (
            <Button onClick={runTest} disabled={!canRun}>
              <Play className="w-4 h-4 mr-2" />
              Executar Teste
            </Button>
          ) : (
            <Button variant="destructive" onClick={cancelTest}>
              <XCircle className="w-4 h-4 mr-2" />
              Cancelar
            </Button>
          )}
        </ProtectedAction>
      </div>

      <div className="p-6 flex flex-col gap-4">
        {isRunning && (
          <div className="rounded-md border border-border p-4">
            <div className="flex items-center gap-2 text-sm mb-2">
              <Loader2 className="w-4 h-4 animate-spin text-primary" />
              {phaseMessage}
            </div>
            <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all duration-300"
                style={{ width: `${Math.round(progress * 100)}%` }}
              />
            </div>
          </div>
        )}

        {runError && (
          <div className="rounded-md border p-3 text-sm flex items-center gap-2 border-red-500/40 bg-red-500/10 text-red-500">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            {runError}
          </div>
        )}

        {!isRunning && !result && !runError && (
          <div className="text-sm text-muted-foreground flex items-center gap-2">
            <Gauge className="w-4 h-4" />
            Selecione cluster, namespace e a URL alvo (ou escolha um Service pra pré-preencher) e
            clique em "Executar Teste" — mede a latência real agora, a partir de um pod efêmero
            dentro do próprio namespace.
          </div>
        )}

        {result && (
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-3 md:grid-cols-6 gap-3">
              <StatCard label="Mínimo" value={`${result.stats.min_ms.toFixed(1)}ms`} />
              <StatCard label="Média" value={`${result.stats.avg_ms.toFixed(1)}ms`} />
              <StatCard label="Mediana" value={`${result.stats.median_ms.toFixed(1)}ms`} />
              <StatCard label="P95" value={`${result.stats.p95_ms.toFixed(1)}ms`} highlight />
              <StatCard label="P99" value={`${result.stats.p99_ms.toFixed(1)}ms`} highlight />
              <StatCard label="Máximo" value={`${result.stats.max_ms.toFixed(1)}ms`} />
            </div>

            <div className="text-sm text-muted-foreground">
              {result.total_requests - result.error_count}/{result.total_requests} requisições
              bem-sucedidas
              {result.error_count > 0 && (
                <Badge variant="outline" className="ml-2 bg-red-500/10 text-red-500 border-red-500/30">
                  {result.error_count} erro(s)/timeout
                </Badge>
              )}
            </div>

            {chartData.length > 0 && (
              <div className="h-56 border border-border rounded-md p-2">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" opacity={0.2} />
                    <XAxis dataKey="n" tick={{ fontSize: 11 }} label={{ value: "requisição", position: "insideBottom", offset: -2, fontSize: 11 }} />
                    <YAxis tick={{ fontSize: 11 }} label={{ value: "ms", angle: -90, position: "insideLeft", fontSize: 11 }} />
                    <Tooltip formatter={(v: number) => [`${v}ms`, "latência"]} labelFormatter={(n) => `requisição ${n}`} />
                    <Bar dataKey="ms" fill="hsl(var(--primary))" radius={[2, 2, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>
        )}

        {history.length > 0 && (
          <div className="mt-2">
            <div className="text-xs font-medium text-muted-foreground mb-1.5">
              Testes anteriores nesta sessão
            </div>
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground">
                  <th className="pb-2 font-medium">Hora</th>
                  <th className="pb-2 font-medium">Cluster/Namespace</th>
                  <th className="pb-2 font-medium">URL</th>
                  <th className="pb-2 font-medium">P95</th>
                  <th className="pb-2 font-medium">P99</th>
                  <th className="pb-2 font-medium">Erros</th>
                </tr>
              </thead>
              <tbody>
                {history.map((h) => (
                  <tr key={h.id} className="border-b border-border last:border-0">
                    <td className="py-1.5 pr-4 text-xs text-muted-foreground whitespace-nowrap">
                      {new Date(h.timestamp).toLocaleTimeString("pt-BR")}
                    </td>
                    <td className="py-1.5 pr-4 font-mono text-xs">{h.cluster}/{h.namespace}</td>
                    <td className="py-1.5 pr-4 font-mono text-xs truncate max-w-xs">{h.url}</td>
                    <td className="py-1.5 pr-4 font-mono text-xs">{h.stats.p95_ms.toFixed(1)}ms</td>
                    <td className="py-1.5 pr-4 font-mono text-xs">{h.stats.p99_ms.toFixed(1)}ms</td>
                    <td className="py-1.5 text-xs">{h.errorCount}/{h.totalRequests}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
