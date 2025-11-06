// API Client - connects to Go backend

import type {
  Cluster,
  ClusterInfo,
  Namespace,
  HPA,
  NodePool,
  CronJob,
  PrometheusResource,
  ValidationStatus,
  APIError,
  APIResponse,
  Session,
  SessionFolder,
  SessionTemplate,
  MonitoringStatus,
  HPAMetrics,
  Anomalies,
  HPAHealth,
} from "./types";

const API_BASE_URL = "/api/v1";

class APIClient {
  private token: string | null = null;

  constructor() {
    // Load token from localStorage
    this.token = localStorage.getItem("auth_token") || null;
  }

  setToken(token: string) {
    this.token = token;
    localStorage.setItem("auth_token", token);
  }

  clearToken() {
    this.token = null;
    localStorage.removeItem("auth_token");
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: HeadersInit = {
      "Content-Type": "application/json",
      ...options.headers,
    };

    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch(`${API_BASE_URL}${endpoint}`, {
      ...options,
      headers,
    });

    if (!response.ok) {
      const error: APIError = await response.json().catch(() => ({
        error: `HTTP ${response.status}: ${response.statusText}`,
      }));
      throw new Error(error.error || `Request failed: ${response.status}`);
    }

    return response.json();
  }

  // Clusters
  async getClusters(): Promise<Cluster[]> {
    const response = await this.request<APIResponse<Cluster[]>>("/clusters");
    return response.data || [];
  }

  async testCluster(clusterName: string): Promise<{ online: boolean }> {
    return this.request(`/clusters/${encodeURIComponent(clusterName)}/test`);
  }

  async switchContext(context: string): Promise<{ success: boolean; message: string }> {
    return this.request("/clusters/switch-context", {
      method: "POST",
      body: JSON.stringify({ context }),
    });
  }

  async getClusterInfo(cluster?: string): Promise<ClusterInfo> {
    const url = cluster ? `/clusters/info?cluster=${encodeURIComponent(cluster)}` : '/clusters/info';
    const response = await this.request(url, { method: 'GET' }) as { success: boolean; data: ClusterInfo };
    return response.data;
  }

  // Namespaces
  async getNamespaces(cluster?: string): Promise<Namespace[]> {
    const query = cluster ? `?cluster=${encodeURIComponent(cluster)}` : "";
    const response = await this.request<APIResponse<Namespace[]>>(
      `/namespaces${query}`
    );
    return response.data || [];
  }

  // HPAs
  async getHPAs(cluster?: string, namespace?: string, bypassCache: boolean = false, showSystem: boolean = false): Promise<HPA[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespace) params.append("namespace", namespace);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());
    const query = params.toString() ? `?${params.toString()}` : "";

    const response = await this.request<APIResponse<HPA[]>>(`/hpas${query}`, {
      headers: bypassCache
        ? {
            "Cache-Control": "no-cache",
            Pragma: "no-cache",
          }
        : {},
    });
    return response.data || [];
  }

  async getHPA(
    cluster: string,
    namespace: string,
    name: string,
    bypassCache: boolean = false
  ): Promise<HPA> {
    const params = new URLSearchParams();
    if (bypassCache) params.append("_t", Date.now().toString());
    const query = params.toString()
      ? `?${params.toString()}`
      : "";

    const response = await this.request<APIResponse<HPA>>(
      `/hpas/${encodeURIComponent(cluster)}/${encodeURIComponent(
        namespace
      )}/${encodeURIComponent(name)}${query}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    if (!response.data) {
      throw new Error("HPA not found");
    }
    return response.data;
  }

  async updateHPA(
    cluster: string,
    namespace: string,
    name: string,
    hpa: Partial<HPA>
  ): Promise<HPA> {
    const response = await this.request<APIResponse<HPA>>(
      `/hpas/${encodeURIComponent(cluster)}/${encodeURIComponent(
        namespace
      )}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(hpa),
        headers: {
          "Content-Type": "application/json",
          "Cache-Control": "no-cache",
          Pragma: "no-cache",
        },
      }
    );
    if (!response.data) {
      throw new Error("HPA update did not return data");
    }
    return response.data;
  }

  // Node Pools
  async getNodePools(cluster?: string): Promise<NodePool[]> {
    const query = cluster ? `?cluster=${encodeURIComponent(cluster)}` : "";
    const response = await this.request<APIResponse<NodePool[]>>(
      `/nodepools${query}`
    );
    return response.data || [];
  }

  async updateNodePool(
    cluster: string,
    resourceGroup: string,
    name: string,
    updates: {
      node_count?: number;
      min_node_count?: number;
      max_node_count?: number;
      autoscaling_enabled?: boolean;
    }
  ): Promise<NodePool> {
    return this.request(
      `/nodepools/${encodeURIComponent(cluster)}/${encodeURIComponent(
        resourceGroup
      )}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(updates),
      }
    );
  }

  async applyNodePoolsSequential(
    nodePools: NodePool[]
  ): Promise<{ success: boolean; message: string }> {
    return this.request("/nodepools/apply-sequential", {
      method: "POST",
      body: JSON.stringify({ nodePools }),
    });
  }

  // CronJobs
  async getCronJobs(cluster?: string): Promise<CronJob[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    const query = params.toString() ? `?${params.toString()}` : "";

    const response = await this.request<APIResponse<CronJob[]>>(
      `/cronjobs${query}`
    );
    return response.data || [];
  }

  async updateCronJob(
    cluster: string,
    namespace: string,
    name: string,
    cronJob: Partial<CronJob>
  ): Promise<CronJob> {
    return this.request(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(
        namespace
      )}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(cronJob),
      }
    );
  }

  // Prometheus Stack
  async getPrometheusResources(
    cluster?: string
  ): Promise<PrometheusResource[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    const query = params.toString() ? `?${params.toString()}` : "";

    const response = await this.request<APIResponse<PrometheusResource[]>>(
      `/prometheus${query}`
    );
    return response.data || [];
  }

  async updatePrometheusResource(
    cluster: string,
    namespace: string,
    type: string,
    name: string,
    resource: Partial<PrometheusResource>
  ): Promise<PrometheusResource> {
    return this.request(
      `/prometheus/${encodeURIComponent(cluster)}/${encodeURIComponent(
        namespace
      )}/${encodeURIComponent(type)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(resource),
      }
    );
  }

  // Sessions
  async getSessions(): Promise<Session[]> {
    const response = await this.request<{ sessions: Session[]; count: number }>(
      "/sessions"
    );
    return response.sessions;
  }

  async getSessionFolders(): Promise<SessionFolder[]> {
    const response = await this.request<{ folders: SessionFolder[] }>(
      "/sessions/folders"
    );
    return response.folders;
  }

  async getSessionsInFolder(folder: string): Promise<Session[]> {
    const response = await this.request<{ sessions: Session[]; count: number }>(
      `/sessions/folders/${folder}`
    );
    return response.sessions;
  }

  async getSession(name: string, folder?: string): Promise<Session> {
    const params = folder ? `?folder=${encodeURIComponent(folder)}` : "";
    return this.request<Session>(
      `/sessions/${encodeURIComponent(name)}${params}`
    );
  }

  async saveSession(sessionData: {
    name: string;
    folder: string;
    description?: string;
    template: string;
    changes: any[];
    node_pool_changes: any[];
  }): Promise<{ message: string; session_name: string; folder: string }> {
    return this.request<{
      message: string;
      session_name: string;
      folder: string;
    }>("/sessions", {
      method: "POST",
      body: JSON.stringify(sessionData),
    });
  }

  async deleteSession(
    name: string,
    folder?: string
  ): Promise<{ message: string; session_name: string }> {
    const params = folder ? `?folder=${encodeURIComponent(folder)}` : "";
    return this.request<{ message: string; session_name: string }>(
      `/sessions/${encodeURIComponent(name)}${params}`,
      {
        method: "DELETE",
      }
    );
  }

  async getSessionTemplates(): Promise<SessionTemplate[]> {
    const response = await this.request<{ templates: SessionTemplate[] }>(
      "/sessions/templates"
    );
    return response.templates;
  }

  // Validation (VPN + Azure CLI)
  async validateEnvironment(): Promise<ValidationStatus> {
    return this.request("/validate");
  }

  // VPN Status Check
  async checkVPNStatus(): Promise<{
    connected: boolean;
    message: string;
    timestamp: number;
  }> {
    return this.request("/vpn/status");
  }

  // Monitoring Endpoints
  async getMonitoringStatus(): Promise<MonitoringStatus> {
    return this.request<MonitoringStatus>("/monitoring/status");
  }

  async getHPAMetrics(
    cluster: string,
    namespace: string,
    hpaName: string,
    duration: string = "5m"
  ): Promise<HPAMetrics> {
    const params = new URLSearchParams({ duration });
    return this.request<HPAMetrics>(
      `/monitoring/metrics/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(hpaName)}?${params}`
    );
  }

  async getAnomalies(
    cluster?: string,
    severity?: string
  ): Promise<Anomalies> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (severity) params.append("severity", severity);

    const queryString = params.toString();
    return this.request<Anomalies>(
      `/monitoring/anomalies${queryString ? `?${queryString}` : ""}`
    );
  }

  async getHPAHealth(
    cluster: string,
    namespace: string,
    hpaName: string
  ): Promise<HPAHealth> {
    return this.request<HPAHealth>(
      `/monitoring/health/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(hpaName)}`
    );
  }

  async startMonitoring(): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(
      "/monitoring/start",
      {
        method: "POST",
      }
    );
  }

  async stopMonitoring(): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(
      "/monitoring/stop",
      {
        method: "POST",
      }
    );
  }

  async addHPAToMonitoring(
    cluster: string,
    namespace: string,
    hpa: string
  ): Promise<{ status: string; message: string; target?: any }> {
    return this.request<{ status: string; message: string; target?: any }>(
      "/monitoring/hpa",
      {
        method: "POST",
        body: JSON.stringify({
          cluster,
          namespace,
          hpa,
        }),
      }
    );
  }
}

// Singleton instance
export const apiClient = new APIClient();

// Export for convenience
export default apiClient;
