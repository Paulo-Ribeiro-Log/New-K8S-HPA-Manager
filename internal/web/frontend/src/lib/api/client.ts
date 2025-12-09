// API Client - connects to Go backend

import type {
  Cluster,
  ClusterInfo,
  Namespace,
  NamespaceManifest,
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
  ConfigMapSummary,
  ConfigMapManifest,
  ConfigMapDiffResult,
  ConfigMapValidateResult,
  ConfigMapApplyResult,
  SecretSummary,
  SecretManifest,
  SecretDiffResult,
  SecretValidateResult,
  SecretApplyResult,
  IngressSummary,
  IngressManifest,
  IngressDiffResult,
  IngressValidateResult,
  IngressApplyResult,
  DeploymentSummary,
  DeploymentManifest,
  DeploymentDiffResult,
  DeploymentValidateResult,
  DeploymentApplyResult,
  PodSummary,
  PodManifest,
  VersionInfo,
  SequenceExecuteRequest,
  TopNamespacesResponse,
  NamespaceMetrics,
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
      const rawError = await response.json().catch(() => ({
        error: `HTTP ${response.status}: ${response.statusText}`,
      }));

      let message: string | undefined;
      if (typeof rawError?.error === "string") {
        message = rawError.error;
      } else if (rawError?.error?.message) {
        message = rawError.error.message;
      } else if (rawError?.message) {
        message = rawError.message;
      } else if (rawError) {
        try {
          message = JSON.stringify(rawError);
        } catch {
          message = undefined;
        }
      }

      throw new Error(message || `Request failed: ${response.status}`);
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

  async getNamespaceMetrics(cluster: string, limit: number = 5): Promise<TopNamespacesResponse> {
    const response = await this.request<APIResponse<TopNamespacesResponse>>(
      `/namespaces/${encodeURIComponent(cluster)}/metrics?limit=${limit}`
    );
    return response.data;
  }

  async getNamespace(cluster: string, name: string): Promise<NamespaceManifest> {
    const response = await this.request<APIResponse<NamespaceManifest>>(
      `/namespaces/${encodeURIComponent(cluster)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("Namespace not found");
    }
    return response.data;
  }

  async describeNamespace(cluster: string, name: string): Promise<{ describe: string }> {
    return await this.request<{ describe: string }>(
      `/namespaces/${encodeURIComponent(cluster)}/${encodeURIComponent(name)}/describe`
    );
  }

  async deleteNamespace(cluster: string, name: string): Promise<{ success: boolean; message: string }> {
    return await this.request<{ success: boolean; message: string }>(
      `/namespaces/${encodeURIComponent(cluster)}/${encodeURIComponent(name)}`,
      { method: "DELETE" }
    );
  }

  async createNamespace(cluster: string, name: string): Promise<{ success: boolean; message: string }> {
    return await this.request<{ success: boolean; message: string }>(
      `/namespaces/${encodeURIComponent(cluster)}`,
      { 
        method: "POST",
        body: JSON.stringify({ name })
      }
    );
  }

  async applyNamespace(
    cluster: string,
    name: string,
    payload: { yaml: string; fieldManager: string; dryRun: boolean }
  ): Promise<{ success: boolean; message: string }> {
    return await this.request<{ success: boolean; message: string }>(
      `/namespaces/${encodeURIComponent(cluster)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    );
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

  // ConfigMaps
  async getConfigMaps(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<ConfigMapSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<ConfigMapSummary[]>>(
      `/configmaps${query}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    return response.data || [];
  }

  async getConfigMap(cluster: string, namespace: string, name: string): Promise<ConfigMapManifest> {
    const response = await this.request<APIResponse<ConfigMapManifest>>(
      `/configmaps/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("ConfigMap not found");
    }
    return response.data;
  }

  async diffConfigMap(originalYaml: string, updatedYaml: string, fileName?: string): Promise<ConfigMapDiffResult> {
    const response = await this.request<APIResponse<ConfigMapDiffResult>>(
      `/configmaps/diff`,
      {
        method: "POST",
        body: JSON.stringify({ originalYaml, updatedYaml, fileName }),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateConfigMap(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<ConfigMapValidateResult> {
    const response = await this.request<APIResponse<ConfigMapValidateResult>>(
      `/configmaps/validate`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Validação sem retorno");
    }
    return response.data;
  }

  async applyConfigMap(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean }
  ): Promise<ConfigMapApplyResult> {
    const response = await this.request<APIResponse<ConfigMapApplyResult>>(
      `/configmaps/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(body),
      }
    );
    if (!response.data) {
      throw new Error("Aplicação sem retorno");
    }
    return response.data;
  }

  async describeConfigMap(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/configmaps/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  // Secrets API Methods
  async getSecrets(cluster: string, namespaces?: string[], showSystem?: boolean, bypassCache: boolean = false): Promise<SecretSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (showSystem !== undefined) {
      params.append("showSystem", showSystem.toString());
    }
    if (bypassCache) params.append("_t", Date.now().toString());

    const response = await this.request<APIResponse<SecretSummary[]>>(
      `/secrets?${params.toString()}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    return response.data || [];
  }

  async getSecret(cluster: string, namespace: string, name: string): Promise<SecretManifest> {
    const response = await this.request<APIResponse<SecretManifest>>(
      `/secrets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("Secret not found");
    }
    return response.data;
  }

  async diffSecret(originalYaml: string, updatedYaml: string, fileName?: string): Promise<SecretDiffResult> {
    const response = await this.request<APIResponse<SecretDiffResult>>(
      `/secrets/diff`,
      {
        method: "POST",
        body: JSON.stringify({ originalYaml, updatedYaml, fileName }),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateSecret(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<SecretValidateResult> {
    const response = await this.request<APIResponse<SecretValidateResult>>(
      `/secrets/validate`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Validação sem retorno");
    }
    return response.data;
  }

  async applySecret(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean }
  ): Promise<SecretApplyResult> {
    const response = await this.request<APIResponse<SecretApplyResult>>(
      `/secrets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(body),
      }
    );
    if (!response.data) {
      throw new Error("Aplicação sem retorno");
    }
    return response.data;
  }

  async describeSecret(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/secrets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  async createSecret(
    cluster: string,
    namespace: string,
    body: { yaml: string; fieldManager?: string }
  ): Promise<SecretApplyResult> {
    const response = await this.request<APIResponse<SecretApplyResult>>(
      `/secrets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}`,
      {
        method: "POST",
        body: JSON.stringify(body),
      }
    );
    if (!response.data) {
      throw new Error("Criação sem retorno");
    }
    return response.data;
  }

  // Deployments API Methods
  async getDeployments(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<DeploymentSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<DeploymentSummary[]>>(
      `/deployments${query}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    return response.data || [];
  }

  async getDeployment(cluster: string, namespace: string, name: string): Promise<DeploymentManifest> {
    const response = await this.request<APIResponse<DeploymentManifest>>(
      `/deployments/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("Deployment not found");
    }
    return response.data;
  }

  async diffDeployment(originalYaml: string, updatedYaml: string, fileName?: string): Promise<DeploymentDiffResult> {
    const response = await this.request<APIResponse<DeploymentDiffResult>>(
      `/deployments/diff`,
      {
        method: "POST",
        body: JSON.stringify({ originalYaml, updatedYaml, fileName }),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateDeployment(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<DeploymentValidateResult> {
    const response = await this.request<APIResponse<DeploymentValidateResult>>(
      `/deployments/validate`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Validação sem retorno");
    }
    return response.data;
  }

  async applyDeployment(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean }
  ): Promise<DeploymentApplyResult> {
    const response = await this.request<APIResponse<DeploymentApplyResult>>(
      `/deployments/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(body),
      }
    );
    if (!response.data) {
      throw new Error("Aplicação sem retorno");
    }
    return response.data;
  }

  async describeDeployment(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/deployments/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  // Ingress API Methods
  async getIngresses(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<IngressSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<IngressSummary[]>>(
      `/ingresses${query}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    return response.data || [];
  }

  async getIngress(cluster: string, namespace: string, name: string): Promise<IngressManifest> {
    const response = await this.request<APIResponse<IngressManifest>>(
      `/ingresses/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("Ingress not found");
    }
    return response.data;
  }

  async diffIngress(originalYaml: string, updatedYaml: string, fileName?: string): Promise<IngressDiffResult> {
    const response = await this.request<APIResponse<IngressDiffResult>>(
      `/ingresses/diff`,
      {
        method: "POST",
        body: JSON.stringify({ originalYaml, updatedYaml, fileName }),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateIngress(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<IngressValidateResult> {
    const response = await this.request<APIResponse<IngressValidateResult>>(
      `/ingresses/validate`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Validação sem retorno");
    }
    return response.data;
  }

  async applyIngress(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean }
  ): Promise<IngressApplyResult> {
    const response = await this.request<APIResponse<IngressApplyResult>>(
      `/ingresses/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(body),
      }
    );
    if (!response.data) {
      throw new Error("Aplicação sem retorno");
    }
    return response.data;
  }

  async describeIngress(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/ingresses/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  // Pods/Containers API Methods
  async getPods(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<PodSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<PodSummary[]>>(
      `/pods${query}`,
      {
        headers: bypassCache
          ? {
              "Cache-Control": "no-cache",
              Pragma: "no-cache",
            }
          : {},
      }
    );
    return response.data || [];
  }

  async getPod(cluster: string, namespace: string, name: string): Promise<PodManifest> {
    const response = await this.request<APIResponse<PodManifest>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("Pod not found");
    }
    return response.data;
  }

  async deletePod(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "DELETE",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async restartPod(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string; hasOwner: boolean; ownerKind?: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string; hasOwner: boolean; ownerKind?: string }>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`,
      {
        method: "POST",
      }
    );
    return response.data || { success: false, message: "Unknown error", hasOwner: false };
  }

  async getPodLogs(
    cluster: string,
    namespace: string,
    podName: string,
    containerName?: string,
    tailLines?: number
  ): Promise<{ logs: string }> {
    const params = new URLSearchParams();
    if (containerName) params.append("container", containerName);
    if (tailLines) params.append("tail", tailLines.toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<{ logs: string }>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(podName)}/logs${query}`
    );
    return response.data || { logs: "" };
  }

  async describePod(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
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

  async getNodePoolDiskMetrics(cluster: string, nodePoolName?: string): Promise<{ success: boolean; data: any[] }> {
    let query = `?cluster=${encodeURIComponent(cluster)}`;
    if (nodePoolName) {
      query += `&nodepool=${encodeURIComponent(nodePoolName)}`;
    }
    const response = await this.request<{ success: boolean; data: any[] }>(
      `/nodepools/disk-metrics${query}`
    );
    return response;
  }

  async getStorageOverview(cluster: string): Promise<{ success: boolean; data: any }> {
    const response = await this.request<{ success: boolean; data: any }>(
      `/nodepools/storage-overview?cluster=${encodeURIComponent(cluster)}`
    );
    return response;
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
    },
    cordonDrainConfig?: {
      cordonEnabled: boolean;
      drainEnabled: boolean;
      gracePeriod: number;
      timeout: number;
      forceDelete: boolean;
      ignoreDaemonSets: boolean;
      deleteEmptyDir: boolean;
      chunkSize: number;
    }
  ): Promise<NodePool> {
    const payload = cordonDrainConfig
      ? {
          ...updates,
          cordon_drain_config: {
            cordon_enabled: cordonDrainConfig.cordonEnabled,
            drain_enabled: cordonDrainConfig.drainEnabled,
            grace_period: cordonDrainConfig.gracePeriod,
            timeout: cordonDrainConfig.timeout,
            force_delete: cordonDrainConfig.forceDelete,
            ignore_daemonsets: cordonDrainConfig.ignoreDaemonSets,
            delete_emptydir: cordonDrainConfig.deleteEmptyDir,
            chunk_size: cordonDrainConfig.chunkSize,
          },
        }
      : updates;

    return this.request(
      `/nodepools/${encodeURIComponent(cluster)}/${encodeURIComponent(
        resourceGroup
      )}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(payload),
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
    duration: string = "5m",
    daysOffset: number = 1
  ): Promise<HPAMetrics> {
    const params = new URLSearchParams({ 
      duration,
      days_offset: daysOffset.toString()
    });
    return this.request<HPAMetrics>(
      `/monitoring/v2/metrics/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(hpaName)}?${params}`
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

  // Sincronizar lista completa de HPAs monitorados (reconciliação)
  async syncMonitoredHPAs(
    hpas: Array<{ cluster: string; namespace: string; hpa: string }>
  ): Promise<{ status: string; added: number; removed: number; total: number }> {
    return this.request<{ status: string; added: number; removed: number; total: number }>(
      "/monitoring/sync",
      {
        method: "POST",
        body: JSON.stringify({ hpas }),
      }
    );
  }

  // Version Info
  async getVersion(): Promise<VersionInfo> {
    const response = await fetch("/api/v1/version");
    if (!response.ok) {
      throw new Error("Failed to fetch version");
    }
    return response.json();
  }

  // Node Pool Sequence Execution
  async executeNodePoolSequence(
    request: SequenceExecuteRequest
  ): Promise<{ success: boolean; message: string; data?: any }> {
    const headers: HeadersInit = {
      "Content-Type": "application/json",
    };

    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch("/api/v1/nodepools/sequence/execute", {
      method: "POST",
      headers,
      body: JSON.stringify(request),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error?.message || "Failed to execute node pool sequence"
      );
    }

    return data;
  }

  // Alerts - Prometheus Integration
  async getHPAAlerts(cluster: string): Promise<any> {
    return this.request(`/alerts/hpa?cluster=${cluster}`);
  }

  async getHPAAlertsByNamespace(cluster: string, namespace: string): Promise<any> {
    return this.request(`/alerts/hpa/namespace?cluster=${cluster}&namespace=${namespace}`);
  }

  async getNodePoolAlerts(cluster: string): Promise<any> {
    return this.request(`/alerts/nodepool?cluster=${cluster}`);
  }

  async getAlertSummary(cluster: string): Promise<any> {
    return this.request(`/alerts/summary?cluster=${cluster}`);
  }

  async getAllAlerts(cluster: string): Promise<any> {
    return this.request(`/alerts?cluster=${cluster}`);
  }
}

// Singleton instance
export const apiClient = new APIClient();

// Export for convenience
export default apiClient;
