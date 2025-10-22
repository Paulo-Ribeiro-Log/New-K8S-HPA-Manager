import { Card } from "@/components/ui/card";
import { Activity, Server, Cpu, HardDrive, Monitor, Package } from "lucide-react";
import { useEffect, useState } from "react";
import { apiClient } from "@/lib/api/client";
import { ClusterInfo } from "@/lib/api/types";
import { MetricsGauge } from "@/components/MetricsGauge";



interface DashboardChartsProps {
  selectedCluster?: string;
}

export const DashboardCharts = ({ selectedCluster }: DashboardChartsProps) => {
  const [clusterInfo, setClusterInfo] = useState<ClusterInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchClusterInfo = async () => {
    try {
      setLoading(true);
      setError(null);
      const info = await apiClient.getClusterInfo(selectedCluster);
      setClusterInfo(info);
    } catch (err) {
      console.error('Error fetching cluster info:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch cluster info');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchClusterInfo();
    
    // Atualizar a cada 30 segundos
    const interval = setInterval(fetchClusterInfo, 30000);
    
    return () => clearInterval(interval);
  }, [selectedCluster]); // Reagir quando o cluster selecionado mudar

  if (loading) {
    return (
      <div className="p-6 h-full bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800">
        <div className="mb-8">
          <div className="h-8 bg-slate-200 dark:bg-slate-700 rounded-lg w-64 animate-pulse mb-2"></div>
          <div className="h-4 bg-slate-200 dark:bg-slate-700 rounded-lg w-96 animate-pulse"></div>
        </div>

        <Card className="mb-6 bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60 dark:border-slate-700/60 shadow-lg">
          <div className="p-6">
            <div className="flex items-center gap-3 mb-6">
              <div className="w-12 h-12 bg-slate-200 dark:bg-slate-700 rounded-xl animate-pulse"></div>
              <div>
                <div className="h-6 bg-slate-200 dark:bg-slate-700 rounded w-48 animate-pulse mb-2"></div>
                <div className="h-4 bg-slate-200 dark:bg-slate-700 rounded w-64 animate-pulse"></div>
              </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
              {[1, 2, 3, 4, 5].map((i) => (
                <div key={i} className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40">
                  <div className="h-4 bg-slate-200 dark:bg-slate-600 rounded w-20 animate-pulse mb-2"></div>
                  <div className="h-5 bg-slate-200 dark:bg-slate-600 rounded w-full animate-pulse"></div>
                </div>
              ))}
            </div>
          </div>
        </Card>

        <Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60 dark:border-slate-700/60 shadow-lg">
          <div className="p-6">
            <div className="flex items-center gap-3 mb-6">
              <div className="w-12 h-12 bg-slate-200 dark:bg-slate-700 rounded-xl animate-pulse"></div>
              <div>
                <div className="h-6 bg-slate-200 dark:bg-slate-700 rounded w-40 animate-pulse mb-2"></div>
                <div className="h-4 bg-slate-200 dark:bg-slate-700 rounded w-56 animate-pulse"></div>
              </div>
            </div>
            <div className="flex justify-center items-center gap-12">
              {[1, 2].map((i) => (
                <div key={i} className="p-6 rounded-2xl border bg-slate-50 dark:bg-slate-700/50 border-slate-200 dark:border-slate-600 shadow-sm">
                  <div className="flex flex-col items-center space-y-4">
                    <div className="w-32 h-32 bg-slate-200 dark:bg-slate-600 rounded-full animate-pulse"></div>
                    <div className="text-center">
                      <div className="h-4 bg-slate-200 dark:bg-slate-600 rounded w-20 animate-pulse mb-2"></div>
                      <div className="h-3 bg-slate-200 dark:bg-slate-600 rounded w-16 animate-pulse"></div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-6 h-full bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800 flex items-center justify-center">
        <Card className="bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm border border-red-200/60 dark:border-red-700/60 shadow-xl max-w-md w-full">
          <div className="p-8 text-center">
            <div className="w-16 h-16 bg-red-100 dark:bg-red-900/30 rounded-full flex items-center justify-center mx-auto mb-4">
              <Monitor className="w-8 h-8 text-red-500" />
            </div>
            <h3 className="text-lg font-semibold text-slate-800 dark:text-slate-100 mb-2">
              Erro ao Carregar Dashboard
            </h3>
            <p className="text-sm text-slate-600 dark:text-slate-400 mb-1">
              Não foi possível obter as informações do cluster
            </p>
            <p className="text-xs text-red-600 dark:text-red-400 font-mono bg-red-50 dark:bg-red-900/20 p-2 rounded border border-red-200 dark:border-red-800 mb-6">
              {error}
            </p>
            <button 
              onClick={fetchClusterInfo}
              className="inline-flex items-center gap-2 px-4 py-2 bg-gradient-to-r from-blue-500 to-purple-600 text-white rounded-lg text-sm font-medium hover:from-blue-600 hover:to-purple-700 transition-all duration-200 shadow-lg hover:shadow-xl"
            >
              <Activity className="w-4 h-4" />
              Tentar Novamente
            </button>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="p-6 h-full bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800 animate-in fade-in duration-500 overflow-y-auto">
      {/* Header Section */}
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-slate-800 dark:text-slate-100 mb-2">
          Dashboard do Cluster
        </h1>
        <p className="text-slate-600 dark:text-slate-400">
          Monitoramento em tempo real dos recursos do Kubernetes
        </p>
      </div>

      {/* Cluster Info Card */}
      <Card className="mb-6 bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60 dark:border-slate-700/60 shadow-lg hover:shadow-xl transition-all duration-300">
        <div className="p-6">
          <div className="flex items-center gap-3 mb-6">
            <div className="p-3 bg-gradient-to-r from-blue-500 to-purple-600 rounded-xl shadow-lg">
              <Server className="w-6 h-6 text-white" />
            </div>
            <div>
              <h2 className="text-xl font-semibold text-slate-800 dark:text-slate-100">
                Informações do Cluster
              </h2>
              <p className="text-sm text-slate-600 dark:text-slate-400">
                Detalhes de configuração e status
              </p>
            </div>
          </div>
          
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors duration-200">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></div>
                <span className="text-sm font-medium text-slate-600 dark:text-slate-400">Cluster</span>
              </div>
              <p className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200 break-all">
                {clusterInfo?.cluster}
              </p>
            </div>
            
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors duration-200">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
                <span className="text-sm font-medium text-slate-600 dark:text-slate-400">Contexto</span>
              </div>
              <p className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200 break-all">
                {clusterInfo?.context}
              </p>
            </div>
            
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors duration-200">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 bg-purple-500 rounded-full animate-pulse"></div>
                <span className="text-sm font-medium text-slate-600 dark:text-slate-400">Kubernetes Version</span>
              </div>
              <p className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200">
                {clusterInfo?.kubernetesVersion}
              </p>
            </div>
            
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors duration-200">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></div>
                <span className="text-sm font-medium text-slate-600 dark:text-slate-400">Nodes</span>
              </div>
              <p className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200">
                {clusterInfo?.nodeCount || 0}
              </p>
            </div>
            
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 border border-slate-200/60 dark:border-slate-600/40 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors duration-200">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
                <span className="text-sm font-medium text-slate-600 dark:text-slate-400">Pods</span>
              </div>
              <p className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200">
                {clusterInfo?.podCount || 0}
              </p>
            </div>
          </div>
          

        </div>
      </Card>

      {/* Resources Usage Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <MetricsGauge
          icon={Cpu}
          label="CPU Usage"
          value={clusterInfo?.cpuUsagePercent || 0}
          unit="%"
          warningThreshold={70}
          dangerThreshold={90}
        />
        
        <MetricsGauge
          icon={HardDrive}
          label="Memory Usage"
          value={clusterInfo?.memoryUsagePercent || 0}
          unit="%"
          warningThreshold={70}
          dangerThreshold={90}
        />
      </div>
    </div>
  );
};
