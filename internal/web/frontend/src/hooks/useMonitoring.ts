// Custom React hooks for Monitoring operations

import { useState, useEffect, useCallback } from "react";
import { apiClient } from "@/lib/api/client";
import type {
  MonitoringStatus,
  HPAMetrics,
  Anomalies,
  HPAHealth,
  Anomaly,
} from "@/lib/api/types";

/**
 * Hook para obter status do monitoring engine
 */
export function useMonitoringStatus() {
  const [status, setStatus] = useState<MonitoringStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getMonitoringStatus();
      setStatus(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch monitoring status"
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  return { status, loading, error, refetch: fetchStatus };
}

/**
 * Hook para obter métricas históricas de um HPA
 */
export function useHPAMetrics(
  cluster?: string,
  namespace?: string,
  hpaName?: string,
  duration: string = "5m"
) {
  const [metrics, setMetrics] = useState<HPAMetrics | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchMetrics = useCallback(
    async (customDuration?: string) => {
      if (!cluster || !namespace || !hpaName) {
        setMetrics(null);
        return;
      }

      try {
        setLoading(true);
        setError(null);
        const data = await apiClient.getHPAMetrics(
          cluster,
          namespace,
          hpaName,
          customDuration || duration
        );
        setMetrics(data);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch HPA metrics"
        );
      } finally {
        setLoading(false);
      }
    },
    [cluster, namespace, hpaName, duration]
  );

  useEffect(() => {
    fetchMetrics();
  }, [fetchMetrics]);

  return { metrics, loading, error, refetch: fetchMetrics };
}

/**
 * Hook para obter anomalias detectadas
 */
export function useAnomalies(cluster?: string, severity?: string) {
  const [anomalies, setAnomalies] = useState<Anomaly[]>([]);
  const [count, setCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchAnomalies = useCallback(
    async (customCluster?: string, customSeverity?: string) => {
      try {
        setLoading(true);
        setError(null);
        const data = await apiClient.getAnomalies(
          customCluster || cluster,
          customSeverity || severity
        );
        setAnomalies(data.anomalies);
        setCount(data.count);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch anomalies"
        );
      } finally {
        setLoading(false);
      }
    },
    [cluster, severity]
  );

  useEffect(() => {
    fetchAnomalies();
  }, [fetchAnomalies]);

  return { anomalies, count, loading, error, refetch: fetchAnomalies };
}

/**
 * Hook para obter health status de um HPA
 */
export function useHPAHealth(
  cluster?: string,
  namespace?: string,
  hpaName?: string
) {
  const [health, setHealth] = useState<HPAHealth | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchHealth = useCallback(async () => {
    if (!cluster || !namespace || !hpaName) {
      setHealth(null);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getHPAHealth(cluster, namespace, hpaName);
      setHealth(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch HPA health"
      );
    } finally {
      setLoading(false);
    }
  }, [cluster, namespace, hpaName]);

  useEffect(() => {
    fetchHealth();
  }, [fetchHealth]);

  return { health, loading, error, refetch: fetchHealth };
}

/**
 * Hook para controlar o monitoring engine (start/stop)
 */
export function useMonitoringControl() {
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const startMonitoring = async () => {
    try {
      setStarting(true);
      setError(null);
      const result = await apiClient.startMonitoring();
      return result;
    } catch (err) {
      const errorMsg =
        err instanceof Error ? err.message : "Failed to start monitoring";
      setError(errorMsg);
      throw new Error(errorMsg);
    } finally {
      setStarting(false);
    }
  };

  const stopMonitoring = async () => {
    try {
      setStopping(true);
      setError(null);
      const result = await apiClient.stopMonitoring();
      return result;
    } catch (err) {
      const errorMsg =
        err instanceof Error ? err.message : "Failed to stop monitoring";
      setError(errorMsg);
      throw new Error(errorMsg);
    } finally {
      setStopping(false);
    }
  };

  return {
    startMonitoring,
    stopMonitoring,
    starting,
    stopping,
    error,
  };
}

/**
 * Hook composto para monitoramento completo de um HPA
 * Combina métricas, health e anomalias em um único hook
 */
export function useHPAMonitoring(
  cluster?: string,
  namespace?: string,
  hpaName?: string,
  duration: string = "5m"
) {
  const { metrics, loading: metricsLoading, error: metricsError, refetch: refetchMetrics } = useHPAMetrics(
    cluster,
    namespace,
    hpaName,
    duration
  );

  const { health, loading: healthLoading, error: healthError, refetch: refetchHealth } = useHPAHealth(
    cluster,
    namespace,
    hpaName
  );

  const refetchAll = useCallback(async () => {
    await Promise.all([refetchMetrics(), refetchHealth()]);
  }, [refetchMetrics, refetchHealth]);

  return {
    metrics,
    health,
    loading: metricsLoading || healthLoading,
    error: metricsError || healthError,
    refetch: refetchAll,
  };
}
