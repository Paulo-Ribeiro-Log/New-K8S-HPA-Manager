import { useState, useEffect } from "react";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Trophy, Cpu, HardDrive, Package, RefreshCw, Activity } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import type { NamespaceMetrics, TopNamespacesResponse } from "@/lib/api/types";

interface TopNamespacesCardProps {
  cluster: string;
}

type MetricTab = "cpu" | "memory" | "pods";

export const TopNamespacesCard = ({ cluster }: TopNamespacesCardProps) => {
  const [data, setData] = useState<TopNamespacesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedTab, setSelectedTab] = useState<MetricTab>("cpu");
  const [refreshTrigger, setRefreshTrigger] = useState(0); // ✅ Trigger para forçar refresh manual

  useEffect(() => {
    // ✅ FIX: Limpar dados ao trocar cluster
    setData(null);
    setError(null);
    setLoading(true);

    // ✅ FIX: AbortController para cancelar fetch pendente ao trocar cluster
    const abortController = new AbortController();
    let isMounted = true;
    const intervalId = Math.random().toString(36).substring(7); // ID único do intervalo

    const fetchMetrics = async () => {
      if (!cluster) {
        setLoading(false);
        return;
      }

      console.log(`[TopNamespacesCard:${intervalId}] Fetching metrics for cluster: ${cluster}`);
      try {
        setError(null);
        const response = await apiClient.getNamespaceMetrics(cluster, 5);

        // ✅ Só atualiza estado se componente ainda estiver montado
        if (isMounted) {
          setData(response);
        }
      } catch (err) {
        // Ignora erros de abort (quando cluster muda)
        if (err instanceof Error && err.name === 'AbortError') {
          console.log(`[TopNamespacesCard:${intervalId}] Fetch aborted for cluster: ${cluster}`);
          return;
        }

        console.error(`[TopNamespacesCard:${intervalId}] Error fetching metrics:`, err);
        if (isMounted) {
          setError(err instanceof Error ? err.message : "Failed to fetch namespace metrics");
        }
      } finally {
        if (isMounted) {
          setLoading(false);
        }
      }
    };

    fetchMetrics();

    // Auto-refresh a cada 60 segundos (agora usa cluster correto)
    console.log(`[TopNamespacesCard:${intervalId}] Creating interval for cluster: ${cluster}`);
    const interval = setInterval(() => {
      // ✅ Verifica se não foi abortado antes de fazer novo fetch
      if (!abortController.signal.aborted && isMounted) {
        console.log(`[TopNamespacesCard:${intervalId}] Interval tick for cluster: ${cluster}`);
        fetchMetrics();
      }
    }, 60000);

    return () => {
      console.log(`[TopNamespacesCard:${intervalId}] CLEANUP for cluster: ${cluster} (clearing interval #${interval})`);
      isMounted = false;
      abortController.abort(); // ✅ Cancela qualquer fetch pendente
      clearInterval(interval);
    };
  }, [cluster, refreshTrigger]); // ✅ Incluir refreshTrigger nas dependências

  if (loading) {
    return (
      <Card className="p-4 bg-gradient-card border-border/50">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-12 h-12 bg-slate-200 dark:bg-slate-700 rounded-xl animate-pulse"></div>
          <div>
            <div className="h-6 bg-slate-200 dark:bg-slate-700 rounded w-64 animate-pulse mb-2"></div>
            <div className="h-4 bg-slate-200 dark:bg-slate-700 rounded w-48 animate-pulse"></div>
          </div>
        </div>
        <div className="space-y-4">
          {[1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="space-y-2">
              <div className="h-4 bg-slate-200 dark:bg-slate-700 rounded w-full animate-pulse"></div>
              <div className="h-2 bg-slate-200 dark:bg-slate-700 rounded w-full animate-pulse"></div>
            </div>
          ))}
        </div>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="p-4 bg-gradient-card border-border/50">
        <div className="flex items-center gap-2 mb-3">
          <div className="p-2 bg-gradient-to-r from-yellow-500 to-orange-600 rounded-lg shadow-md opacity-50">
            <Trophy className="w-5 h-5 text-white" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-slate-800 dark:text-slate-100">
              Top 5 Namespaces
            </h3>
            <p className="text-xs text-slate-600 dark:text-slate-400">
              Dados indisponíveis
            </p>
          </div>
        </div>
        <div className="flex items-center justify-center py-6">
          <div className="text-center max-w-xs">
            <div className="w-10 h-10 bg-yellow-100 dark:bg-yellow-900/30 rounded-full flex items-center justify-center mx-auto mb-2">
              <Activity className="w-5 h-5 text-yellow-600 dark:text-yellow-400" />
            </div>
            <p className="text-xs text-slate-600 dark:text-slate-400 mb-1">
              Prometheus inacessível
            </p>
            <p className="text-xs text-slate-500 dark:text-slate-500 truncate" title={error}>
              Timeout ao conectar
            </p>
          </div>
        </div>
      </Card>
    );
  }

  if (!data) {
    return null;
  }

  const renderMetricBar = (metric: NamespaceMetrics, type: MetricTab) => {
    const value =
      type === "cpu"
        ? metric.cpu_request_millis
        : type === "memory"
        ? metric.memory_request_gb
        : metric.pod_count;

    const percent =
      type === "cpu"
        ? metric.cpu_percent_of_cluster
        : type === "memory"
        ? metric.memory_percent_of_cluster
        : metric.pod_percent_of_cluster;

    const color =
      percent > 75
        ? "bg-red-500"
        : percent > 50
        ? "bg-yellow-500"
        : "bg-green-500";

    const displayValue =
      type === "cpu"
        ? `${value.toLocaleString()}m`
        : type === "memory"
        ? `${value.toFixed(1)} GB`
        : `${value} pods`;

    return (
      <div key={metric.namespace} className="mb-2">
        <div className="flex justify-between text-xs mb-1">
          <span className="font-medium text-slate-700 dark:text-slate-300 truncate max-w-[180px]">
            {metric.namespace}
          </span>
          <span className="text-slate-600 dark:text-slate-400 text-xs">
            {displayValue}
            <span className="ml-1.5 text-slate-400 dark:text-slate-500">
              ({percent.toFixed(0)}%)
            </span>
          </span>
        </div>
        <Progress value={percent} className={`h-1.5 ${color}`} />
      </div>
    );
  };

  const getCurrentMetrics = (): NamespaceMetrics[] => {
    switch (selectedTab) {
      case "cpu":
        return data.top_cpu;
      case "memory":
        return data.top_memory;
      case "pods":
        return data.top_pods;
    }
  };

  const getOthers = (): NamespaceMetrics => {
    switch (selectedTab) {
      case "cpu":
        return data.cpu_others;
      case "memory":
        return data.memory_others;
      case "pods":
        return data.pods_others;
    }
  };

  return (
    <Card className="p-4 bg-gradient-card border-border/50">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <div className="p-2 bg-gradient-to-r from-yellow-500 to-orange-600 rounded-lg shadow-md">
            <Trophy className="w-5 h-5 text-white" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-slate-800 dark:text-slate-100">
              Top 5 Namespaces por Consumo
            </h3>
            <p className="text-xs text-slate-600 dark:text-slate-400">
              {data.total_namespaces} namespaces total
            </p>
          </div>
        </div>
        <button
          onClick={() => setRefreshTrigger((prev) => prev + 1)}
          className="p-1.5 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg transition-colors"
          title="Atualizar métricas"
        >
          <RefreshCw className="w-4 h-4 text-slate-600 dark:text-slate-400" />
        </button>
      </div>

      <div className="flex gap-2 mb-4">
        <button
          onClick={() => setSelectedTab("cpu")}
          className={`flex-1 py-1.5 px-3 rounded-lg transition-all duration-200 text-sm ${
            selectedTab === "cpu"
              ? "bg-gradient-to-r from-blue-500 to-blue-600 text-white shadow-md"
              : "bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-600"
          }`}
        >
          <Cpu className="inline w-3.5 h-3.5 mr-1.5" />
          CPU
        </button>
        <button
          onClick={() => setSelectedTab("memory")}
          className={`flex-1 py-1.5 px-3 rounded-lg transition-all duration-200 text-sm ${
            selectedTab === "memory"
              ? "bg-gradient-to-r from-purple-500 to-purple-600 text-white shadow-md"
              : "bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-600"
          }`}
        >
          <HardDrive className="inline w-3.5 h-3.5 mr-1.5" />
          Memória
        </button>
        <button
          onClick={() => setSelectedTab("pods")}
          className={`flex-1 py-1.5 px-3 rounded-lg transition-all duration-200 text-sm ${
            selectedTab === "pods"
              ? "bg-gradient-to-r from-green-500 to-green-600 text-white shadow-md"
              : "bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-600"
          }`}
        >
          <Package className="inline w-3.5 h-3.5 mr-1.5" />
          Pods
        </button>
      </div>

      <div className="space-y-2.5">
        {getCurrentMetrics().map((metric) => renderMetricBar(metric, selectedTab))}

        {/* Outros namespaces */}
        <div className="mt-3 pt-3 border-t border-slate-200 dark:border-slate-700">
          {renderMetricBar(getOthers(), selectedTab)}
        </div>
      </div>
    </Card>
  );
};
