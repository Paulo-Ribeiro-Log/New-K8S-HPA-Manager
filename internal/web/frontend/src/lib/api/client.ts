// API Client - connects to Go backend

import type {
  Cluster,
  ClusterInfo,
  Namespace,
  NamespaceManifest,
  HPA,
  NodePool,
  NodesListResponse,
  NodeDetailsResponse,
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
  StatefulSetSummary,
  StatefulSetManifest,
  StatefulSetDiffResult,
  StatefulSetValidateResult,
  StatefulSetApplyResult,
  DaemonSetSummary,
  DaemonSetManifest,
  DaemonSetDiffResult,
  DaemonSetValidateResult,
  DaemonSetApplyResult,
  PodSummary,
  PodManifest,
  EventSummary,
  ResourceQuotaSummary,
  NetworkPolicySummary,
  ServiceSummary,
  ServiceManifest,
  ServiceDiffResult,
  ServiceValidateResult,
  ServiceApplyResult,
  VPASummary,
  VPAManifest,
  VPADiffResult,
  VPAValidateResult,
  VPAApplyResult,
  PodsSummary,
  VersionInfo,
  SequenceExecuteRequest,
  TopNamespacesResponse,
  NamespaceMetrics,
  GitHubReposConfig,
  GitHubReleasesResponse,
  GitHubComparison,
  DeploymentSearchResponse,
  ProductionDeploymentResponse,
  AllVersionsResponse,
  TokenStatusResponse,
  SaveTokenRequest,
  SaveTokenResponse,
  ServiceNowImportResponse,
  ServiceNowParseResponse,
  ServiceNowPlaywrightResponse,
  PlaywrightStatusResponse,
  ServiceNowBrowserConfig,
  ServiceNowBatchItem,
  ServiceNowBatchResponse,
  APIResourceInfo,
  GenericResourceSummary,
  GenericResourceManifest,
  ExplorerDiffResult,
  ExplorerApplyResult,
  AWXStatus,
  AWXCertificate,
  AWXJobLaunch,
  BatchPodMetrics,
  NodePoolRegistryEntry,
  NodePoolLookupResult,
  ConntrackResponse,
  ConntrackNodeHistoryResponse,
  K8sNamespacePermissions,
} from "./types";

import type {
  AnalyzeRequest,
  AnalysisResult,
  ProviderStatus,
  AIStats,
  HistoryFilter,
} from "@/types/ai";

import type {
  HealthCheckRequest,
  HealthCheckRunResponse,
  HealthCheckHistoryResponse,
  HealthCheckStatsResponse,
  HealthCheckGetResponse,
  HealthCheckDeleteResponse,
  HealthCheckEventsResponse,
} from "@/types/healthcheck";

const API_BASE_URL = "/api/v1";

class APIClient {
  private token: string | null = null;
  private gitHubEmail: string | null = null;

  constructor() {
    // Load token from localStorage
    this.token = localStorage.getItem("auth_token") || null;
    this.gitHubEmail = localStorage.getItem("github_email") || null;
  }

  setToken(token: string) {
    this.token = token;
    localStorage.setItem("auth_token", token);
  }

  clearToken() {
    this.token = null;
    localStorage.removeItem("auth_token");
  }

  /** Tenta login via JWT no backend. Retorna os dados do token emitido. */
  async login(): Promise<{ token: string; email: string; isSRE: boolean; expiresAt: string }> {
    const res = await fetch(`${API_BASE_URL}/auth/login`, { method: "POST" });
    const data = await res.json();
    if (!res.ok) throw Object.assign(new Error(data.error || "Login failed"), { status: res.status, code: data.code });
    this.setToken(data.token);
    return { token: data.token, email: data.email, isSRE: data.is_sre, expiresAt: data.expires_at };
  }

  /** Logout stateless — descarta token localmente. */
  async logout(): Promise<void> {
    try {
      await fetch(`${API_BASE_URL}/auth/logout`, {
        method: "POST",
        headers: this.token ? { Authorization: `Bearer ${this.token}` } : {},
      });
    } finally {
      this.clearToken();
    }
  }

  /**
   * Verifica se o token armazenado é um JWT expirado.
   * Retorna false para tokens não-JWT (backward compat com token estático).
   */
  isTokenExpired(): boolean {
    const token = this.token;
    if (!token) return false;
    const parts = token.split(".");
    if (parts.length !== 3) return false; // não é JWT
    try {
      const payload = JSON.parse(atob(parts[1]));
      if (!payload.exp) return false;
      return Date.now() / 1000 > payload.exp;
    } catch {
      return false;
    }
  }

  /**
   * Retorna true quando o JWT expira em menos de 60 minutos.
   * Permite refresh proativo antes de expirar — evita que o analista
   * precise refazer az login durante o dia de trabalho.
   */
  private isTokenNearExpiry(): boolean {
    const token = this.token;
    if (!token) return false;
    const parts = token.split(".");
    if (parts.length !== 3) return false;
    try {
      const payload = JSON.parse(atob(parts[1]));
      if (!payload.exp) return false;
      const oneHourFromNow = Date.now() / 1000 + 3600;
      return oneHourFromNow > payload.exp;
    } catch {
      return false;
    }
  }

  /** Decodifica claims do JWT localmente (sem verificar assinatura). */
  getTokenClaims(): { email?: string; isSRE?: boolean } | null {
    const token = this.token;
    if (!token) return null;
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    try {
      const payload = JSON.parse(atob(parts[1]));
      return { email: payload.email, isSRE: payload.is_sre };
    } catch {
      return null;
    }
  }

  setGitHubEmail(email: string) {
    console.log('🔧 [APIClient] Setting GitHub email:', email);
    this.gitHubEmail = email;
    try {
      localStorage.setItem("github_email", email);
      console.log('✅ [APIClient] GitHub email saved to localStorage');
    } catch (error) {
      console.error('❌ [APIClient] Failed to save GitHub email to localStorage:', error);
    }
  }

  clearGitHubEmail() {
    this.gitHubEmail = null;
    localStorage.removeItem("github_email");
  }

  getGitHubOrg(): string {
    return localStorage.getItem("github_org") || "casas-bahia";
  }

  setGitHubOrg(org: string) {
    localStorage.setItem("github_org", org);
  }

  /** Tenta renovar o JWT antes de fazer a requisição. Limpa token se refresh falhar. */
  private async tryRefreshToken(): Promise<void> {
    if (!this.token) return;
    try {
      const res = await fetch(`${API_BASE_URL}/auth/refresh`, {
        method: "POST",
        headers: { Authorization: `Bearer ${this.token}` },
      });
      if (!res.ok) { this.clearToken(); return; }
      const data = await res.json();
      if (data.token) this.setToken(data.token);
    } catch {
      // Sem conectividade — não limpa token, tenta a requisição original
    }
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    // Auto-refresh: renova proativamente quando falta < 1h para expirar
    // ou quando já expirou (backend aceita grace period de 24h no refresh)
    if (this.isTokenExpired() || this.isTokenNearExpiry()) {
      await this.tryRefreshToken();
      // Se token foi limpo (refresh falhou após 24h de grace), força re-login
      if (!this.token) {
        window.dispatchEvent(new CustomEvent("jwt-expired"));
        throw new Error("Sessão expirada. Faça login novamente.");
      }
    }

    // ✅ CRÍTICO: Recarregar gitHubEmail do localStorage antes de cada requisição
    this.gitHubEmail = localStorage.getItem("github_email") || null;

    // Debug para rotas GitHub
    if (endpoint.includes('/github/')) {
      console.log('🔍 [APIClient] Request to:', endpoint);
      console.log('📧 [APIClient] GitHub email from localStorage:', this.gitHubEmail);
    }

    // IMPORTANTE: Construir headers COMPLETO antes de passar para fetch
    const requestHeaders: Record<string, string> = {
      "Content-Type": "application/json",
    };

    // Copiar headers do options (se existirem)
    if (options.headers) {
      const optHeaders = options.headers as Record<string, string>;
      Object.keys(optHeaders).forEach(key => {
        requestHeaders[key] = optHeaders[key];
      });
    }

    // Adicionar Authorization se existir
    if (this.token) {
      requestHeaders["Authorization"] = `Bearer ${this.token}`;
    }

    // Adicionar X-GitHub-Email se existir (DEVE ser adicionado AQUI, ANTES do fetch)
    if (this.gitHubEmail) {
      requestHeaders["X-GitHub-Email"] = this.gitHubEmail;
      if (endpoint.includes('/github/')) {
        console.log('✅ [APIClient] Adding X-GitHub-Email header:', this.gitHubEmail);
        console.log('🔧 [APIClient] All headers:', requestHeaders);
      }
    } else if (endpoint.includes('/github/')) {
      console.warn('⚠️ [APIClient] No GitHub email found - header NOT added');
    }

    const response = await fetch(`${API_BASE_URL}${endpoint}`, {
      ...options,
      headers: requestHeaders, // Usar o objeto completo
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

      // Detectar token AWS SSO expirado ou falha do exec provider EKS e disparar evento global
      const isAwsSsoError = message && (
        message.includes("aws sso login") ||
        message.includes("token AWS expirado") ||
        message.includes("SSO expirado") ||
        message.includes("credenciais AWS incompletas") ||
        message.includes("NoCredentialProviders") ||
        // Erros do exec credential provider do client-go (aws eks get-token retornou erro)
        message.includes("exec plugin: execute command") ||
        message.includes("couldn't get token: exec plugin") ||
        message.includes("exec format error") ||
        // AWS CLI: perfil sem token SSO (exit 255 = aws cli retornou erro)
        (message.includes("exit status 255") && message.includes("eks"))
      );
      if (isAwsSsoError) {
        const profileMatch = message!.match(/--profile\s+(\S+)/);
        const profile = profileMatch?.[1] ?? "";
        window.dispatchEvent(new CustomEvent("aws-sso-token-expired", { detail: { profile } }));
      }

      // Token rejeitado pelo servidor (inválido, expirado ou formato errado)
      // Força re-login para que o usuário obtenha um JWT válido
      if (response.status === 401) {
        const code = rawError?.error?.code ?? rawError?.code ?? "";
        if (code === "INVALID_TOKEN" || code === "TOKEN_EXPIRED" || code === "UNAUTHORIZED") {
          this.clearToken();
          window.dispatchEvent(new CustomEvent("jwt-expired"));
        }
      }

      // K8s RBAC negou a operação — erro amigável sem stack trace
      if (response.status === 403) {
        const friendly = "Permissão negada pelo K8s RBAC. Você não tem acesso de escrita neste namespace.";
        throw Object.assign(new Error(message || friendly), { status: 403, code: "K8S_FORBIDDEN" });
      }

      throw new Error(message || `Request failed: ${response.status}`);
    }

    return response.json();
  }

  // Generic HTTP methods (used by hooks like useUserPermissions)
  async get<T = any>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: 'GET' });
  }

  async post<T = any>(endpoint: string, data?: any): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'POST',
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async delete<T = any>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: 'DELETE' });
  }

  // Permissões K8s reais via SelfSubjectRulesReview
  async getK8sPermissions(cluster: string, namespace: string): Promise<K8sNamespacePermissions> {
    return this.request<K8sNamespacePermissions>(
      `/permissions/k8s?cluster=${encodeURIComponent(cluster)}&namespace=${encodeURIComponent(namespace)}`
    );
  }

  // Clusters
  async getClusters(): Promise<Cluster[]> {
    const response = await this.request<APIResponse<Cluster[]>>("/clusters");
    return response.data || [];
  }

  async testCluster(clusterName: string, timeoutSec?: number): Promise<{ data: { cluster: string; status: string; reachable: boolean } }> {
    const url = `/clusters/${encodeURIComponent(clusterName)}/test${timeoutSec ? `?timeout=${timeoutSec}` : ""}`;
    return this.request(url);
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
  // Sempre busca TODOS os namespaces (incluindo sistema) com isSystem marcado
  // A filtragem é feita no frontend para permitir toggle dinâmico
  async getNamespaces(cluster?: string): Promise<Namespace[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    // Sempre pedir todos os namespaces para ter isSystem disponível
    params.append("showSystem", "true");
    const query = params.toString() ? `?${params.toString()}` : "";
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

  async createNamespace(
    cluster: string,
    name: string,
    isSpotInstance: boolean = false,
    annotations?: Record<string, string>,
    labels?: Record<string, string>
  ): Promise<{ success: boolean; message: string }> {
    return await this.request<{ success: boolean; message: string }>(
      `/namespaces/${encodeURIComponent(cluster)}`,
      {
        method: "POST",
        body: JSON.stringify({ name, isSpotInstance, annotations, labels })
      }
    );
  }

  async applyNamespace(
    cluster: string,
    name: string,
    payload: { yaml: string; fieldManager: string; dryRun: boolean; force?: boolean }
  ): Promise<{ success: boolean; message: string }> {
    return await this.request<{ success: boolean; message: string }>(
      `/namespaces/${encodeURIComponent(cluster)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    );
  }

  async applyPod(
    cluster: string,
    namespace: string,
    name: string,
    payload: { yaml: string; fieldManager: string; dryRun: boolean; force?: boolean }
  ): Promise<{ success: boolean; message: string; data?: any }> {
    return await this.request<{ success: boolean; message: string; data?: any }>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    );
  }

  async getPodMetrics(
    cluster: string,
    namespace: string,
    name: string
  ): Promise<{ 
    success: boolean; 
    data?: {
      available: boolean;
      cpu?: {
        current: number;
        request: number;
        limit: number;
        percent: number;
        unit: string;
      };
      memory?: {
        current: number;
        request: number;
        limit: number;
        percent: number;
        unit: string;
      };
      message?: string;
    };
  }> {
    try {
      return await this.request<{ 
        success: boolean; 
        data: {
          available: boolean;
          cpu?: any;
          memory?: any;
          message?: string;
        };
      }>(
        `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/metrics`
      );
    } catch (error: any) {
      return {
        success: false,
        data: {
          available: false,
          message: "Metrics not available",
        },
      };
    }
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
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
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
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
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
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
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

  // Deployments Batch Operations
  async batchDeleteDeployments(cluster: string, deployments: Array<{ namespace: string; name: string }>): Promise<{
    results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
    total: number;
    success_count: number;
    failed_count: number;
  }> {
    const response = await this.request<APIResponse<{
      results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
      total: number;
      success_count: number;
      failed_count: number;
    }>>(
      `/deployments/${encodeURIComponent(cluster)}/batch/delete`,
      {
        method: "POST",
        body: JSON.stringify({ deployments }),
      }
    );
    return response.data || { results: [], total: 0, success_count: 0, failed_count: 0 };
  }

  async batchRestartDeployments(cluster: string, deployments: Array<{ namespace: string; name: string }>): Promise<{
    results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
    total: number;
    success_count: number;
    failed_count: number;
  }> {
    const response = await this.request<APIResponse<{
      results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
      total: number;
      success_count: number;
      failed_count: number;
    }>>(
      `/deployments/${encodeURIComponent(cluster)}/batch/restart`,
      {
        method: "POST",
        body: JSON.stringify({ deployments }),
      }
    );
    return response.data || { results: [], total: 0, success_count: 0, failed_count: 0 };
  }

  async scaleDeployment(cluster: string, namespace: string, name: string, replicas: number): Promise<{ success: boolean; message: string; replicas: number }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string; replicas: number }>>(
      `/deployments/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`,
      {
        method: "POST",
        body: JSON.stringify({ replicas }),
      }
    );
    return response.data || { success: false, message: "Sem resposta", replicas };
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
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
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

  // StatefulSets API Methods
  async getStatefulSets(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<StatefulSetSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<StatefulSetSummary[]>>(
      `/statefulsets${query}`,
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

  async getStatefulSet(cluster: string, namespace: string, name: string): Promise<StatefulSetManifest> {
    const response = await this.request<APIResponse<StatefulSetManifest>>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("StatefulSet not found");
    }
    return response.data;
  }

  async diffStatefulSet(payload: { originalYaml: string; updatedYaml: string; fileName?: string }): Promise<StatefulSetDiffResult> {
    const response = await this.request<APIResponse<StatefulSetDiffResult>>(
      `/statefulsets/diff`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateStatefulSet(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<StatefulSetValidateResult> {
    const response = await this.request<APIResponse<StatefulSetValidateResult>>(
      `/statefulsets/validate`,
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

  async applyStatefulSet(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
  ): Promise<StatefulSetApplyResult> {
    const response = await this.request<APIResponse<StatefulSetApplyResult>>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
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

  async describeStatefulSet(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  async deleteStatefulSet(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "DELETE",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async restartStatefulSet(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`,
      {
        method: "POST",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async scaleStatefulSet(cluster: string, namespace: string, name: string, replicas: number): Promise<{ success: boolean; message: string; replicas: number }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string; replicas: number }>>(
      `/statefulsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`,
      {
        method: "POST",
        body: JSON.stringify({ replicas }),
      }
    );
    return response.data || { success: false, message: "Unknown error", replicas: 0 };
  }

  // DaemonSets API Methods
  async getDaemonSets(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<DaemonSetSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<DaemonSetSummary[]>>(
      `/daemonsets${query}`,
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

  async getDaemonSet(cluster: string, namespace: string, name: string): Promise<DaemonSetManifest> {
    const response = await this.request<APIResponse<DaemonSetManifest>>(
      `/daemonsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) {
      throw new Error("DaemonSet not found");
    }
    return response.data;
  }

  async diffDaemonSet(payload: { originalYaml: string; updatedYaml: string; fileName?: string }): Promise<DaemonSetDiffResult> {
    const response = await this.request<APIResponse<DaemonSetDiffResult>>(
      `/daemonsets/diff`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    if (!response.data) {
      throw new Error("Diff response inválida");
    }
    return response.data;
  }

  async validateDaemonSet(payload: {
    cluster: string;
    namespace: string;
    yaml: string;
    fieldManager?: string;
  }): Promise<DaemonSetValidateResult> {
    const response = await this.request<APIResponse<DaemonSetValidateResult>>(
      `/daemonsets/validate`,
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

  async applyDaemonSet(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; fieldManager?: string; dryRun?: boolean; force?: boolean }
  ): Promise<DaemonSetApplyResult> {
    const response = await this.request<APIResponse<DaemonSetApplyResult>>(
      `/daemonsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
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

  async describeDaemonSet(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/daemonsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  async deleteDaemonSet(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/daemonsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "DELETE",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async restartDaemonSet(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/daemonsets/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`,
      {
        method: "POST",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
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

  async killPod(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/kill`,
      {
        method: "POST",
      }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  // Batch Operations
  async batchDeletePods(cluster: string, pods: Array<{ namespace: string; name: string }>): Promise<{
    results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
    total: number;
    success_count: number;
    failed_count: number;
  }> {
    const response = await this.request<APIResponse<{
      results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
      total: number;
      success_count: number;
      failed_count: number;
    }>>(
      `/pods/${encodeURIComponent(cluster)}/batch/delete`,
      {
        method: "POST",
        body: JSON.stringify({ pods }),
      }
    );
    return response.data || { results: [], total: 0, success_count: 0, failed_count: 0 };
  }

  async batchKillPods(cluster: string, pods: Array<{ namespace: string; name: string }>): Promise<{
    results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
    total: number;
    success_count: number;
    failed_count: number;
  }> {
    const response = await this.request<APIResponse<{
      results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
      total: number;
      success_count: number;
      failed_count: number;
    }>>(
      `/pods/${encodeURIComponent(cluster)}/batch/kill`,
      {
        method: "POST",
        body: JSON.stringify({ pods }),
      }
    );
    return response.data || { results: [], total: 0, success_count: 0, failed_count: 0 };
  }

  async batchRestartPods(cluster: string, pods: Array<{ namespace: string; name: string }>): Promise<{
    results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
    total: number;
    success_count: number;
    failed_count: number;
  }> {
    const response = await this.request<APIResponse<{
      results: Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }>;
      total: number;
      success_count: number;
      failed_count: number;
    }>>(
      `/pods/${encodeURIComponent(cluster)}/batch/restart`,
      {
        method: "POST",
        body: JSON.stringify({ pods }),
      }
    );
    return response.data || { results: [], total: 0, success_count: 0, failed_count: 0 };
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

  async getBatchPodMetrics(cluster: string, namespace: string): Promise<BatchPodMetrics> {
    try {
      const params = new URLSearchParams({ cluster, namespace });
      const result = await this.request<{ success: boolean; data: BatchPodMetrics }>(`/pods/metrics?${params}`);
      return result.data;
    } catch {
      return { available: false, pods: {} };
    }
  }

  async describePod(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  // Events API Methods
  async getEvents(
    cluster?: string,
    namespaces?: string[],
    search?: string,
    type?: "Normal" | "Warning",
    showSystem: boolean = false,
    bypassCache: boolean = false
  ): Promise<EventSummary[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces && namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (search) params.append("search", search);
    if (type) params.append("type", type);
    if (showSystem) params.append("showSystem", "true");
    if (bypassCache) params.append("_t", Date.now().toString());

    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<{ events: EventSummary[]; count: number }>>(
      `/events${query}`
    );
    return response.data?.events || [];
  }

  async getResourceQuotas(
    cluster: string,
    namespaces: string[]
  ): Promise<ResourceQuotaSummary[]> {
    const params = new URLSearchParams({ cluster });
    if (namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    const response = await this.request<APIResponse<{ quotas: ResourceQuotaSummary[]; count: number }>>(
      `/resource-quotas?${params}`
    );
    return response.data?.quotas || [];
  }

  async getNetworkPolicies(
    cluster: string,
    namespaces: string[]
  ): Promise<NetworkPolicySummary[]> {
    const params = new URLSearchParams({ cluster });
    if (namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    const response = await this.request<APIResponse<{ policies: NetworkPolicySummary[]; count: number }>>(
      `/network-policies?${params}`
    );
    return response.data?.policies || [];
  }

  async getServices(
    cluster: string,
    namespaces: string[],
    showSystem: boolean = false
  ): Promise<ServiceSummary[]> {
    const params = new URLSearchParams({ cluster });
    if (namespaces.length > 0) {
      params.append("namespaces", namespaces.join(","));
    }
    if (showSystem) params.append("showSystem", "true");
    const response = await this.request<APIResponse<{ services: ServiceSummary[]; count: number }>>(
      `/services?${params}`
    );
    return response.data?.services || [];
  }

  async getServiceManifest(cluster: string, namespace: string, name: string): Promise<ServiceManifest> {
    const response = await this.request<APIResponse<ServiceManifest>>(
      `/services/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) throw new Error("Service not found");
    return response.data;
  }

  async diffService(payload: { originalYaml: string; updatedYaml: string; fileName?: string }): Promise<ServiceDiffResult> {
    const response = await this.request<APIResponse<ServiceDiffResult>>(
      `/services/diff`,
      { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Diff response inválida");
    return response.data;
  }

  async validateService(payload: { cluster: string; namespace: string; name?: string; yaml: string }): Promise<ServiceValidateResult> {
    const response = await this.request<APIResponse<ServiceValidateResult>>(
      `/services/validate`,
      { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Validação sem retorno");
    return response.data;
  }

  async applyService(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; dryRun?: boolean; force?: boolean }
  ): Promise<ServiceApplyResult> {
    const response = await this.request<APIResponse<ServiceApplyResult>>(
      `/services/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: "PUT", body: JSON.stringify(body) }
    );
    if (!response.data) throw new Error("Aplicação sem retorno");
    return response.data;
  }

  async createService(cluster: string, namespace: string, yaml: string): Promise<{ name: string; namespace: string; cluster: string }> {
    const response = await this.request<APIResponse<{ name: string; namespace: string; cluster: string }>>(
      `/services/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}`,
      { method: "POST", body: JSON.stringify({ yaml }) }
    );
    if (!response.data) throw new Error("Criação sem retorno");
    return response.data;
  }

  async deleteService(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/services/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: "DELETE" }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async describeService(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/services/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  // VPAs API Methods
  async getVPAs(
    cluster: string,
    namespaces?: string[],
    showSystem: boolean = false
  ): Promise<{ data: VPASummary[]; crdNotInstalled?: boolean }> {
    const params = new URLSearchParams({ cluster });
    if (namespaces && namespaces.length > 0) params.append("namespaces", namespaces.join(","));
    if (showSystem) params.append("showSystem", "true");
    const response = await this.request<{ success: boolean; data: VPASummary[]; count: number; crdNotInstalled?: boolean }>(
      `/vpas?${params}`
    );
    return { data: response.data || [], crdNotInstalled: response.crdNotInstalled };
  }

  async getVPAManifest(cluster: string, namespace: string, name: string): Promise<VPAManifest> {
    const response = await this.request<APIResponse<VPAManifest>>(
      `/vpas/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) throw new Error("VPA not found");
    return response.data;
  }

  async diffVPA(payload: { originalYaml: string; updatedYaml: string; fileName?: string }): Promise<VPADiffResult> {
    const response = await this.request<APIResponse<VPADiffResult>>(
      `/vpas/diff`,
      { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Diff response inválida");
    return response.data;
  }

  async validateVPA(payload: { cluster: string; namespace: string; yaml: string }): Promise<VPAValidateResult> {
    const response = await this.request<APIResponse<VPAValidateResult>>(
      `/vpas/validate`,
      { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Validação sem retorno");
    return response.data;
  }

  async applyVPA(
    cluster: string,
    namespace: string,
    name: string,
    body: { yaml: string; dryRun?: boolean; force?: boolean }
  ): Promise<VPAApplyResult> {
    const response = await this.request<APIResponse<VPAApplyResult>>(
      `/vpas/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: "PUT", body: JSON.stringify(body) }
    );
    if (!response.data) throw new Error("Aplicação sem retorno");
    return response.data;
  }

  async deleteVPA(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/vpas/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: "DELETE" }
    );
    return response.data || { success: false, message: "Unknown error" };
  }

  async describeVPA(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/vpas/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  async getPodsSummary(cluster: string, namespace: string): Promise<PodsSummary> {
    const response = await this.request<APIResponse<PodsSummary>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/summary`
    );
    return response.data as PodsSummary;
  }

  async createDebugPod(cluster: string, namespace: string, name: string): Promise<{ success: boolean; message: string }> {
    const response = await this.request<APIResponse<{ success: boolean; message: string }>>(
      `/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/debug`,
      {
        method: "POST",
        body: JSON.stringify({ name }),
        headers: {
          "Content-Type": "application/json",
        },
      }
    );
    return response.data as { success: boolean; message: string };
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
  async getNodePools(cluster?: string): Promise<{ pools: NodePool[]; notSupported: boolean; message?: string }> {
    const query = cluster ? `?cluster=${encodeURIComponent(cluster)}` : "";
    const response = await this.request<APIResponse<NodePool[]> & { not_supported?: boolean; message?: string }>(
      `/nodepools${query}`
    );
    return {
      pools: response.data || [],
      notSupported: response.not_supported ?? false,
      message: response.message,
    };
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

  async getRemovedNodes(cluster: string, pool: string): Promise<{
    removed_nodes: Array<{
      name: string;
      removed_at: string;
      reason: string;
      source: string;
      details: string;
    }>;
  }> {
    return this.request(
      `/nodepools/removed-nodes?cluster=${encodeURIComponent(cluster)}&pool=${encodeURIComponent(pool)}`
    );
  }

  async getNodeEvents(cluster: string, node: string): Promise<{
    events: Array<{
      type: string;
      reason: string;
      age: string;
      count: number;
      from: string;
      message: string;
      timestamp: string;
    }>;
  }> {
    return this.request(
      `/nodepools/node-events?cluster=${encodeURIComponent(cluster)}&node=${encodeURIComponent(node)}`
    );
  }

  async getPendingWorkloads(cluster: string, aiEmail?: string): Promise<import("@/lib/api/types").PendingWorkloadsResponse> {
    const params = new URLSearchParams({ cluster });
    if (aiEmail) params.set("ai_email", aiEmail);
    return this.request(`/nodepools/pending-workloads?${params}`);
  }

  async getNodeResources(cluster: string, nodepool: string): Promise<import("@/lib/api/types").NodeResourcesResponse> {
    return this.request(`/nodepools/node-resources?cluster=${encodeURIComponent(cluster)}&nodepool=${encodeURIComponent(nodepool)}`);
  }

  async getAutoscalerStatus(cluster: string): Promise<import("@/lib/api/types").AutoscalerStatus> {
    return this.request(`/nodepools/autoscaler-status?cluster=${encodeURIComponent(cluster)}`);
  }

  async getNodeDiskStats(cluster: string, nodepool: string): Promise<import("@/lib/api/types").NodeDiskStatsResponse> {
    return this.request(`/nodepools/node-disk-stats?cluster=${encodeURIComponent(cluster)}&nodepool=${encodeURIComponent(nodepool)}`);
  }

  async getConntrackStats(cluster: string, nodepool: string): Promise<ConntrackResponse> {
    return this.request<ConntrackResponse>(
      `/nodepools/conntrack?cluster=${encodeURIComponent(cluster)}&nodepool=${encodeURIComponent(nodepool)}`
    );
  }

  async getConntrackNodeHistory(
    cluster: string,
    node: string,
    hours = 6,
    step = 5,
  ): Promise<ConntrackNodeHistoryResponse> {
    return this.request<ConntrackNodeHistoryResponse>(
      `/nodepools/conntrack/history?cluster=${encodeURIComponent(cluster)}&node=${encodeURIComponent(node)}&hours=${hours}&step=${step}`
    );
  }

  async getStorageOverview(cluster: string): Promise<{ success: boolean; data: any }> {
    const response = await this.request<{ success: boolean; data: any }>(
      `/nodepools/storage-overview?cluster=${encodeURIComponent(cluster)}`
    );
    return response;
  }

  async getNodesInNodePool(cluster: string, nodePoolName: string): Promise<NodesListResponse> {
    const response = await this.request<APIResponse<NodesListResponse>>(
      `/nodes/${encodeURIComponent(cluster)}/${encodeURIComponent(nodePoolName)}`
    );
    return response.data || { nodes: [], count: 0, node_pool_name: nodePoolName, cluster };
  }

  async getNodePoolAzureInfo(cluster: string, nodePoolName: string): Promise<{
    cluster_tags: Record<string, string>;
    subscription_name: string;
    resource_group: string;
    subscription: string;
  }> {
    const response = await this.request<APIResponse<any>>(
      `/nodes/${encodeURIComponent(cluster)}/${encodeURIComponent(nodePoolName)}/azure-info`
    );
    return response.data || { cluster_tags: {}, subscription_name: "", resource_group: "", subscription: "" };
  }

  async getNodeDetails(cluster: string, nodePoolName: string, nodeName: string): Promise<NodeDetailsResponse> {
    // Add cache-busting timestamp to ensure fresh data
    const cacheBuster = Date.now();
    const response = await this.request<APIResponse<NodeDetailsResponse>>(
      `/nodes/${encodeURIComponent(cluster)}/${encodeURIComponent(nodePoolName)}/${encodeURIComponent(nodeName)}?_t=${cacheBuster}`
    );
    if (!response.data) {
      throw new Error('No data returned from server');
    }
    return response.data;
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
    },
    signal?: AbortSignal
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
        signal,
      }
    );
  }

  async abortNodePoolOperation(cluster: string, resourceGroup: string, name: string): Promise<{ success: boolean; message: string }> {
    return this.request(
      `/nodepools/${encodeURIComponent(cluster)}/${encodeURIComponent(resourceGroup)}/${encodeURIComponent(name)}/abort`,
      { method: "POST" }
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
  async getCronJobs(cluster?: string, namespaces?: string[]): Promise<CronJob[]> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespaces?.length) namespaces.forEach(ns => params.append("namespaces", ns));
    const query = params.toString() ? `?${params.toString()}` : "";
    const response = await this.request<APIResponse<CronJob[]>>(`/cronjobs${query}`);
    return response.data || [];
  }

  async getCronJobManifest(cluster: string, namespace: string, name: string): Promise<{ yaml: string }> {
    const response = await this.request<APIResponse<{ yaml: string; cluster: string; namespace: string; name: string }>>(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`
    );
    if (!response.data) throw new Error("Manifesto sem retorno");
    return response.data;
  }

  async applyCronJob(
    cluster: string, namespace: string, name: string,
    body: { yaml: string; dryRun?: boolean }
  ): Promise<{ name: string; namespace: string; cluster: string; resourceVersion?: string; dryRun?: boolean }> {
    const response = await this.request<APIResponse<{ name: string; namespace: string; cluster: string; resourceVersion?: string; dryRun?: boolean }>>(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/yaml`,
      { method: "PUT", body: JSON.stringify(body) }
    );
    if (!response.data) throw new Error("Aplicação sem retorno");
    return response.data;
  }

  async describeCronJob(cluster: string, namespace: string, name: string): Promise<{ describe: string }> {
    const response = await this.request<{ describe: string; cluster: string; namespace: string; name: string }>(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/describe`
    );
    return response;
  }

  async diffCronJob(payload: { originalYaml: string; updatedYaml: string; fileName: string }): Promise<{ unifiedDiff: string; hasChanges: boolean }> {
    const response = await this.request<APIResponse<{ unifiedDiff: string; hasChanges: boolean }>>(
      "/cronjobs/diff", { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Diff sem retorno");
    return response.data;
  }

  async validateCronJob(payload: { cluster: string; namespace: string; name: string; yaml: string }): Promise<{ valid: boolean }> {
    const response = await this.request<APIResponse<{ valid: boolean }>>(
      "/cronjobs/validate", { method: "POST", body: JSON.stringify(payload) }
    );
    if (!response.data) throw new Error("Validação sem retorno");
    return response.data;
  }

  async triggerCronJob(cluster: string, namespace: string, name: string): Promise<{ jobName: string; namespace: string; cluster: string }> {
    const response = await this.request<APIResponse<{ jobName: string; namespace: string; cluster: string }>>(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/trigger`,
      { method: "POST", body: "{}" }
    );
    if (!response.data) throw new Error("Trigger sem retorno");
    return response.data;
  }

  async getJobTemplate(cluster: string, namespace: string, name: string): Promise<{ yaml: string }> {
    return this.request<{ yaml: string }>(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/job-template`
    );
  }

  async createJob(cluster: string, namespace: string, yamlContent: string, dryRun = false): Promise<{ name: string; namespace: string; dry_run: boolean }> {
    const response = await this.request<APIResponse<{ name: string; namespace: string; dry_run: boolean }>>(
      "/jobs",
      {
        method: "POST",
        body: JSON.stringify({ cluster, namespace, yaml: yamlContent, dry_run: dryRun }),
      }
    );
    if (!response.success) throw new Error((response as unknown as { error: string }).error || "Erro ao criar Job");
    return response.data!;
  }

  async createCronJob(cluster: string, namespace: string, yamlContent: string, dryRun = false): Promise<{ name: string; namespace: string; schedule: string; dry_run: boolean }> {
    const response = await this.request<APIResponse<{ name: string; namespace: string; schedule: string; dry_run: boolean }>>(
      "/cronjobs/new",
      {
        method: "POST",
        body: JSON.stringify({ cluster, namespace, yaml: yamlContent, dry_run: dryRun }),
      }
    );
    if (!response.success) throw new Error((response as unknown as { error: string }).error || "Erro ao criar CronJob");
    return response.data!;
  }

  async updateCronJob(
    cluster: string,
    namespace: string,
    name: string,
    cronJob: Partial<CronJob>
  ): Promise<CronJob> {
    return this.request(
      `/cronjobs/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: "PUT", body: JSON.stringify(cronJob) }
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

  // Service Mesh - Istio/Kiali Integration
  async getServiceGraph(
    cluster: string,
    namespace: string | string[],  // Aceita string ou array
    duration: string = '5m',
    graphType: string = 'workload',
    options?: {
      injectServiceNodes?: boolean;
      includeIdleEdges?: boolean;
      includeIdleNodes?: boolean;
      appenders?: string;
    }
  ): Promise<import('@/types/servicemesh').ServiceGraphResponse> {
    // Converter array para string separada por vírgulas
    const namespaceParam = Array.isArray(namespace) ? namespace.join(',') : namespace;
    let url = `/servicemesh/graph?cluster=${cluster}&namespace=${namespaceParam}&duration=${duration}&graphType=${graphType}`;
    
    if (options?.injectServiceNodes !== undefined) {
      url += `&injectServiceNodes=${options.injectServiceNodes}`;
    }
    if (options?.includeIdleEdges !== undefined) {
      url += `&includeIdleEdges=${options.includeIdleEdges}`;
    }
    if (options?.includeIdleNodes !== undefined) {
      url += `&includeIdleNodes=${options.includeIdleNodes}`;
    }
    if (options?.appenders) {
      url += `&appenders=${encodeURIComponent(options.appenders)}`;
    }
    
    return this.request(url);
  }

  async getServiceMeshNamespaces(
    cluster: string
  ): Promise<import('@/types/servicemesh').ServiceMeshNamespace> {
    return this.request(`/servicemesh/namespaces?cluster=${cluster}`);
  }

  async getServiceMeshMetrics(
    cluster: string,
    namespace: string
  ): Promise<import('@/types/servicemesh').ServiceMeshMetrics> {
    return this.request(`/servicemesh/metrics?cluster=${cluster}&namespace=${namespace}`);
  }

  // =============================================================================
  // AI Diagnostics Methods
  // =============================================================================

  /**
   * Get AI provider status
   */
  async getAIProviderStatus(aiEmail?: string): Promise<ProviderStatus> {
    const params = aiEmail ? `?ai_email=${encodeURIComponent(aiEmail)}` : "";
    return this.request(`/ai/status${params}`);
  }

  /**
   * Analyze a Kubernetes resource with AI
   */
  async analyzeResource(request: AnalyzeRequest, signal?: AbortSignal): Promise<AnalysisResult> {
    return this.request(`/ai/analyze`, {
      method: "POST",
      body: JSON.stringify(request),
      signal, // Pass signal to fetch for cancellation
    });
  }

  /**
   * Get AI analysis history with optional filters
   */
  async getAIHistory(filters?: HistoryFilter): Promise<AnalysisResult[]> {
    let url = `/ai/history`;
    const params = new URLSearchParams();

    if (filters) {
      if (filters.cluster) params.append("cluster", filters.cluster);
      if (filters.namespace) params.append("namespace", filters.namespace);
      if (filters.resourceType) params.append("resourceType", filters.resourceType);
      if (filters.provider) params.append("provider", filters.provider);
      if (filters.limit !== undefined) params.append("limit", filters.limit.toString());
      if (filters.offset !== undefined) params.append("offset", filters.offset.toString());
    }

    const queryString = params.toString();
    if (queryString) {
      url += `?${queryString}`;
    }

    const response = await this.request<{ records: AnalysisResult[]; total: number }>(url);
    
    // Backend retorna { records: [], total: 0 }, mas precisamos apenas do array
    return Array.isArray(response) ? response : (response.records || []);
  }

  /**
   * Get a specific analysis by ID
   */
  async getAnalysisById(id: string): Promise<AnalysisResult> {
    return this.request(`/ai/history/${id}`);
  }

  /**
   * Get AI statistics
   */
  async getAIStats(): Promise<AIStats> {
    return this.request(`/ai/stats`);
  }

  /**
   * Delete an analysis from history
   */
  async deleteAnalysis(id: string): Promise<void> {
    return this.request(`/ai/history/${id}`, {
      method: "DELETE",
    });
  }

  /**
   * Get user's AI tokens status
   */
  async startGoogleDeviceAuth(): Promise<{
    device_code: string;
    user_code: string;
    verification_url: string;
    expires_in: number;
    interval: number;
  }> {
    return this.request(`/ai/tokens/google-auth/start`, { method: "POST", body: "{}" });
  }

  async pollGoogleDeviceAuth(deviceCode: string, aiEmail: string): Promise<{
    status: "pending" | "authenticated" | "error";
    access_token?: string;
    error?: string;
  }> {
    return this.request(`/ai/tokens/google-auth/poll`, {
      method: "POST",
      body: JSON.stringify({ device_code: deviceCode, ai_email: aiEmail }),
    });
  }

  // ─── gcloud install + auth flow ───────────────────────────────────────────
  async startGoogleInstallAuth(aiEmail?: string, baseUrl?: string, wifPoolProvider?: string, wifClientID?: string): Promise<{ session_id: string; auth_url?: string; status?: string; is_wif?: boolean }> {
    return this.request(`/ai/tokens/google-auth/install/start`, {
      method: "POST",
      body: JSON.stringify({
        ai_email: aiEmail || "",
        base_url: baseUrl || window.location.origin,
        wif_pool_provider: wifPoolProvider || "",
        wif_client_id: wifClientID || "",
      }),
    });
  }

  async getGoogleAuthStatus(sessionId: string): Promise<{
    status: "waiting_browser" | "authenticated" | "error";
    auth_url?: string;
    error?: string;
  }> {
    return this.request(`/ai/tokens/google-auth/install/status?session_id=${encodeURIComponent(sessionId)}`);
  }

  async submitGoogleAuthCode(sessionId: string, code: string, aiEmail: string): Promise<{ status: string }> {
    return this.request(`/ai/tokens/google-auth/install/code`, {
      method: "POST",
      body: JSON.stringify({ session_id: sessionId, code, ai_email: aiEmail }),
    });
  }

  async getAITokens(): Promise<{
    ai_email?: string;
    has_gemini: boolean;
    gemini_model?: string;
    gemini_auth_mode?: string;
    gemini_vertex_project?: string;
    gemini_vertex_location?: string;
    gemini_wif_login_url?: string;
    gemini_wif_client_id?: string;
    gemini_agentspace_engine_id?: string;
    has_gemini_service_account: boolean;
    has_gemini_refresh_token: boolean;
    has_openai: boolean;
    openai_model?: string;
    has_claude: boolean;
    claude_model?: string;
    has_copilot: boolean;
    copilot_endpoint?: string;
    copilot_deployment?: string;
    ollama_model?: string;
    preferred_provider: string;
    has_dynatrace?: boolean;
    dynatrace_url?: string;
    dynatrace_tag_filter?: string;
    updated_at?: string;
  }> {
    // IMPORTANTE: Enviar ai_email para buscar configurações corretas do usuário
    const aiEmail = localStorage.getItem("ai_email");
    if (aiEmail) {
      return this.request(`/ai/tokens?ai_email=${encodeURIComponent(aiEmail)}`);
    }
    return this.request(`/ai/tokens`);
  }

  /**
   * Save user's AI tokens
   */
  async saveAITokens(tokens: {
    ai_email: string; // Email para identificar configurações (independente do Azure AD)
    gemini_api_key?: string;
    gemini_model?: string;
    gemini_auth_mode?: string;
    gemini_vertex_project?: string;
    gemini_vertex_location?: string;
    gemini_service_account_json?: string;
    gemini_wif_login_url?: string;
    gemini_wif_client_id?: string;
    gemini_agentspace_engine_id?: string;
    openai_api_key?: string;
    openai_model?: string;
    claude_api_key?: string;
    claude_model?: string;
    copilot_api_key?: string;
    copilot_endpoint?: string;
    copilot_deployment?: string;
    ollama_model?: string;
    preferred_provider: string;
  }): Promise<{ success: boolean; message: string }> {
    return this.request(`/ai/tokens`, {
      method: "POST",
      body: JSON.stringify(tokens),
    });
  }

  /**
   * Delete user's AI tokens
   */
  async deleteAITokens(aiEmail: string): Promise<{ success: boolean; message: string }> {
    return this.request(`/ai/tokens?ai_email=${encodeURIComponent(aiEmail)}`, {
      method: "DELETE",
    });
  }

  /**
   * Validate an AI token
   */
  async validateAIToken(
    provider: string,
    apiKey: string,
    endpoint?: string,
    deployment?: string,
    vertexProject?: string,
    vertexLocation?: string,
    serviceAccountJSON?: string,
    aiEmail?: string
  ): Promise<{ valid: boolean; error?: string; message?: string }> {
    const body: any = { provider, api_key: apiKey };
    if (endpoint) body.endpoint = endpoint;
    if (deployment) body.deployment = deployment;
    if (vertexProject) body.vertex_project = vertexProject;
    if (vertexLocation) body.vertex_location = vertexLocation;
    if (serviceAccountJSON) body.service_account_json = serviceAccountJSON;
    if (aiEmail) body.ai_email = aiEmail;

    return this.request(`/ai/tokens/validate`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  /**
   * Get available models for a specific AI provider
   */
  async getAvailableModels(provider: string, mode?: string): Promise<{
    provider: string;
    models: Array<{
      id: string;
      name: string;
      description?: string;
      is_default: boolean;
    }>;
  }> {
    const params = mode ? `provider=${provider}&mode=${mode}` : `provider=${provider}`;
    return this.request(`/ai/models?${params}`);
  }

  /**
   * Export AI analysis as PDF
   */
  async exportAIPDF(analysisId: string): Promise<Blob> {
    const headers: HeadersInit = {
      "Content-Type": "application/pdf",
    };

    // Add auth token if available
    const token = localStorage.getItem("auth_token");
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const response = await fetch(`/api/v1/ai/report/${analysisId}/pdf`, {
      method: "GET",
      headers,
    });

    if (!response.ok) {
      throw new Error(`Failed to export PDF: ${response.statusText}`);
    }

    return response.blob();
  }

  /**
   * Export AI analysis as Markdown
   */
  async exportAIMarkdown(analysisId: string): Promise<Blob> {
    const headers: HeadersInit = {
      "Content-Type": "text/markdown",
    };

    // Add auth token if available
    const token = localStorage.getItem("auth_token");
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const response = await fetch(`/api/v1/ai/report/${analysisId}/markdown`, {
      method: "GET",
      headers,
    });

    if (!response.ok) {
      throw new Error(`Failed to export Markdown: ${response.statusText}`);
    }

    return response.blob();
  }

  /**
   * Export AI analysis as CSV
   */
  async exportAICSV(analysisId: string): Promise<Blob> {
    const headers: HeadersInit = {
      "Content-Type": "text/csv",
    };

    // Add auth token if available
    const token = localStorage.getItem("auth_token");
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const response = await fetch(`/api/v1/ai/report/${analysisId}/csv`, {
      method: "GET",
      headers,
    });

    if (!response.ok) {
      throw new Error(`Failed to export CSV: ${response.statusText}`);
    }

    return response.blob();
  }

  // ========================
  // Health Checking Methods
  // ========================

  /**
   * Run health check on specified clusters
   * POST /api/v1/healthcheck/run
   */
  async runHealthCheck(request: HealthCheckRequest): Promise<HealthCheckRunResponse> {
    return this.request(`/healthcheck/run`, {
      method: "POST",
      body: JSON.stringify(request),
    });
  }

  /**
   * Cancel health check
   * DELETE /api/v1/healthcheck/cancel/:sessionId
   */
  async cancelHealthCheck(sessionId: string): Promise<{ success: boolean; message: string }> {
    return this.request(`/healthcheck/cancel/${sessionId}`, {
      method: "DELETE",
    });
  }

  /**
   * Get health check history
   * GET /api/v1/healthcheck/history?cluster=x&namespace=y
   */
  async getHealthCheckHistory(
    cluster?: string,
    namespace?: string
  ): Promise<HealthCheckHistoryResponse> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (namespace) params.append("namespace", namespace);

    const query = params.toString();
    return this.request(`/healthcheck/history${query ? `?${query}` : ""}`);
  }

  /**
   * Get health check statistics
   * GET /api/v1/healthcheck/stats?cluster=x&days=7
   */
  async getHealthCheckStats(
    cluster?: string,
    days?: number
  ): Promise<HealthCheckStatsResponse> {
    const params = new URLSearchParams();
    if (cluster) params.append("cluster", cluster);
    if (days) params.append("days", days.toString());

    const query = params.toString();
    return this.request(`/healthcheck/stats${query ? `?${query}` : ""}`);
  }

  /**
   * Get specific health check result by ID
   * GET /api/v1/healthcheck/:id
   */
  async getHealthCheckResult(id: string): Promise<HealthCheckGetResponse> {
    return this.request(`/healthcheck/${id}`);
  }

  /**
   * Delete health check result
   * DELETE /api/v1/healthcheck/:id
   */
  async deleteHealthCheckResult(id: string): Promise<HealthCheckDeleteResponse> {
    return this.request(`/healthcheck/${id}`, {
      method: "DELETE",
    });
  }

  /**
   * Get health check events (logs persistidos) by session ID
   * GET /api/v1/healthcheck/events/:sessionId
   */
  async getHealthCheckEvents(sessionId: string): Promise<HealthCheckEventsResponse> {
    return this.request(`/healthcheck/events/${sessionId}`);
  }

  /**
   * Análise AI de item correlacionado K8s ↔ Dynatrace
   * POST /api/v1/healthcheck/correlated/analyze
   */
  async analyzeCorrelatedItem(
    item: import("../../types/healthcheck").CorrelatedHealthItem,
    aiEmail: string
  ): Promise<{ success: boolean; analysis: string; analyzed_at: string }> {
    return this.request("/healthcheck/correlated/analyze", {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail, item }),
    });
  }

  /**
   * Análise AI consolidada de múltiplos itens correlacionados K8s ↔ Dynatrace
   * POST /api/v1/healthcheck/correlated/analyze-batch
   */
  async analyzeCorrelatedBatch(
    items: import("../../types/healthcheck").CorrelatedHealthItem[],
    aiEmail: string
  ): Promise<{ success: boolean; analysis: string; item_count: number; analyzed_at: string }> {
    return this.request("/healthcheck/correlated/analyze-batch", {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail, items }),
    });
  }

  /**
   * Análise AI de um sinal OneAgent individual
   * POST /api/v1/healthcheck/oneagent/analyze
   */
  async analyzeOneAgentSignal(
    signal: import("../../types/healthcheck").OneAgentSignal,
    aiEmail: string
  ): Promise<{ analysis: string }> {
    return this.request("/healthcheck/oneagent/analyze", {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail, signal }),
    });
  }

  // ===== HEALTH CHECK FILTERS =====

  /**
   * Get all filter rules
   * GET /api/v1/filters
   */
  async getFilters(): Promise<any> {
    return this.request("/filters");
  }

  /**
   * Get available filter categories
   * GET /api/v1/filters/categories
   */
  async getFilterCategories(): Promise<any> {
    return this.request("/filters/categories");
  }

  /**
   * Add new filter rule
   * POST /api/v1/filters
   */
  async addFilterRule(rule: any): Promise<any> {
    return this.request("/filters", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(rule),
    });
  }

  /**
   * Remove filter rule
   * DELETE /api/v1/filters/:id
   */
  async removeFilterRule(id: string): Promise<any> {
    return this.request(`/filters/${id}`, {
      method: "DELETE",
    });
  }

  // ========================================
  // GitHub Releases API
  // ========================================

  /**
   * Get configured GitHub repositories
   * GET /api/v1/github/repos
   */
  async getGitHubRepos(): Promise<GitHubReposConfig> {
    return this.request("/github/repos");
  }

  /**
   * Get releases from a GitHub repository
   * GET /api/v1/github/repos/:owner/:repo/releases
   */
  async getGitHubReleases(
    owner: string,
    repo: string
  ): Promise<GitHubReleasesResponse> {
    return this.request(`/github/repos/${owner}/${repo}/releases`);
  }

  /**
   * Compare two GitHub releases (tags)
   * GET /api/v1/github/repos/:owner/:repo/compare/:base...:head
   */
  async compareGitHubReleases(
    owner: string,
    repo: string,
    base: string,
    head: string
  ): Promise<GitHubComparison> {
    return this.request(
      `/github/repos/${owner}/${repo}/compare/${base}...${head}`
    );
  }

  /**
   * Search deployments by app name
   * GET /api/v1/github/deployments/search?app_name=X
   */
  async searchDeployments(appName: string): Promise<DeploymentSearchResponse> {
    const params = new URLSearchParams({ app_name: appName });
    return this.request(`/github/deployments/search?${params}`);
  }

  /**
   * Get production deployment version
   * GET /api/v1/github/deployments/production?app_name=X
   */
  async getProductionDeployment(
    appName: string
  ): Promise<ProductionDeploymentResponse> {
    const params = new URLSearchParams({ app_name: appName });
    return this.request(`/github/deployments/production?${params}`);
  }

  /**
   * Get all versions of an app
   * GET /api/v1/github/deployments/all-versions?app_name=X
   */
  async getAllVersions(appName: string): Promise<AllVersionsResponse> {
    const params = new URLSearchParams({ app_name: appName });
    return this.request(`/github/deployments/all-versions?${params}`);
  }

  /**
   * Get GitHub token status for current user
   * GET /api/v1/github/token/status
   */
  async getGitHubTokenStatus(): Promise<TokenStatusResponse> {
    return this.request("/github/token/status");
  }

  /**
   * Save GitHub token for current user
   * POST /api/v1/github/token
   */
  async saveGitHubToken(token: string, email: string): Promise<SaveTokenResponse> {
    const response = await this.request<SaveTokenResponse>("/github/token", {
      method: "POST",
      body: JSON.stringify({ token, email } as SaveTokenRequest),
    });
    
    // Store email in localStorage for future requests
    this.setGitHubEmail(email);
    
    return response;
  }

  /**
   * Delete GitHub token for current user
   * DELETE /api/v1/github/token
   */
  async deleteGitHubToken(): Promise<{ success: boolean; message: string }> {
    const response = await this.request<{ success: boolean; message: string }>("/github/token", {
      method: "DELETE",
    });

    // Clear email from localStorage
    this.clearGitHubEmail();

    return response;
  }

  /**
   * POST /api/v1/github/commit-file
   * Faz commit de um arquivo em um repositório GitHub a partir da URL de pasta.
   */
  async commitFileToGitHub(params: {
    owner: string;
    repo: string;
    branch: string;
    basePath: string;
    filename: string;
    content: string;
    message: string;
  }): Promise<{ success: boolean; file_url: string; commit_url: string; created: boolean }> {
    return this.request("/github/commit-file", {
      method: "POST",
      body: JSON.stringify({
        owner: params.owner,
        repo: params.repo,
        branch: params.branch,
        base_path: params.basePath,
        filename: params.filename,
        content: params.content,
        message: params.message,
      }),
    });
  }

  // ==================== ServiceNow Integration ====================

  /**
   * Import CHG data from ServiceNow URL via HTTP scraping
   * POST /api/v1/servicenow/import
   */
  async importServiceNowCHG(url: string): Promise<ServiceNowImportResponse> {
    return this.request("/servicenow/import", {
      method: "POST",
      body: JSON.stringify({ url }),
    });
  }

  /**
   * Parse CHG description text manually
   * POST /api/v1/servicenow/parse
   */
  async parseServiceNowDescription(description: string): Promise<ServiceNowParseResponse> {
    return this.request("/servicenow/parse", {
      method: "POST",
      body: JSON.stringify({ description }),
    });
  }

  /**
   * Extract sys_id from ServiceNow URL
   * GET /api/v1/servicenow/extract-sysid?url=...
   */
  async extractServiceNowSysID(url: string): Promise<{ success: boolean; sys_id?: string; error?: string }> {
    const params = new URLSearchParams({ url });
    return this.request(`/servicenow/extract-sysid?${params}`);
  }

  /**
   * Extract CHG data using Playwright (browser automation with Azure AD SSO)
   * POST /api/v1/servicenow/extract-playwright
   */
  async extractServiceNowWithPlaywright(url: string): Promise<ServiceNowPlaywrightResponse> {
    return this.request("/servicenow/extract-playwright", {
      method: "POST",
      body: JSON.stringify({ url }),
    });
  }

  /**
   * Extract multiple CHGs sequentially in a single HTTP request — avoids concurrent Rod serialization timeouts.
   * POST /api/v1/servicenow/parse-batch
   */
  async parseServiceNowBatch(items: ServiceNowBatchItem[]): Promise<ServiceNowBatchResponse> {
    return this.request("/servicenow/parse-batch", {
      method: "POST",
      body: JSON.stringify({ items }),
    });
  }

  /**
   * Get Playwright configuration status
   * GET /api/v1/servicenow/playwright-status
   */
  async getPlaywrightStatus(): Promise<PlaywrightStatusResponse> {
    return this.request("/servicenow/playwright-status");
  }

  /**
   * GET /api/v1/servicenow/browser-config
   */
  async getServiceNowBrowserConfig(): Promise<ServiceNowBrowserConfig> {
    return this.request("/servicenow/browser-config");
  }

  /**
   * POST /api/v1/servicenow/browser-config
   */
  async setServiceNowBrowserConfig(
    forceWindowsBrowser: boolean,
    windowsSessionDir?: string
  ): Promise<ServiceNowBrowserConfig> {
    return this.request("/servicenow/browser-config", {
      method: "POST",
      body: JSON.stringify({
        force_windows_browser: forceWindowsBrowser,
        windows_session_dir: windowsSessionDir ?? "",
      }),
    });
  }

  // ==================== NodePool Predictive Analysis ====================

  /**
   * Analyze a node pool with predictive AI
   * POST /api/v1/nodepoolpredictions/analyze
   */
  async analyzeNodePool(cluster: string, nodepool: string): Promise<any> {
    return this.request("/nodepoolpredictions/analyze", {
      method: "POST",
      body: JSON.stringify({ cluster, nodepool_name: nodepool }),
    });
  }

  /**
   * Get node pool prediction history
   * GET /api/v1/nodepoolpredictions/history
   */
  async getNodePoolPredictionHistory(filters?: {
    cluster?: string;
    nodepool?: string;
    limit?: number;
    offset?: number;
  }): Promise<any> {
    const params = new URLSearchParams();
    if (filters?.cluster) params.set("cluster", filters.cluster);
    if (filters?.nodepool) params.set("nodepool", filters.nodepool);
    if (filters?.limit) params.set("limit", String(filters.limit));
    if (filters?.offset) params.set("offset", String(filters.offset));
    const qs = params.toString();
    return this.request(`/nodepoolpredictions/history${qs ? `?${qs}` : ""}`);
  }

  /**
   * Download node pool prediction markdown report
   * GET /api/v1/nodepoolpredictions/report/:id/markdown
   */
  async getNodePoolPredictionReport(id: string): Promise<string> {
    const response = await fetch(`/api/v1/nodepoolpredictions/report/${id}/markdown`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
      },
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.text();
  }

  // ==================== Resource Explorer ====================

  /**
   * Lista todos os tipos de recursos disponíveis no cluster (built-in + CRDs)
   * GET /api/v1/explorer/api-resources?cluster=X
   */
  async getAPIResources(cluster: string): Promise<APIResourceInfo[]> {
    const params = new URLSearchParams({ cluster });
    const response = await this.request<{ success: boolean; data: APIResourceInfo[]; count: number }>(
      `/explorer/api-resources?${params}`
    );
    return response.data || [];
  }

  /**
   * Lista recursos de um tipo específico
   * GET /api/v1/explorer/items?cluster=X&resource=Y&group=Z&namespace=W
   */
  async listGenericResources(
    cluster: string,
    resource: string,
    group: string,
    namespace?: string
  ): Promise<GenericResourceSummary[]> {
    const params = new URLSearchParams({ cluster, resource });
    if (group) params.set("group", group);
    if (namespace) params.set("namespace", namespace);
    const response = await this.request<{ success: boolean; data: GenericResourceSummary[]; count: number }>(
      `/explorer/items?${params}`
    );
    return response.data || [];
  }

  /**
   * Retorna o YAML completo de um recurso específico
   * GET /api/v1/explorer/:cluster/:namespace/:resource/:name
   */
  async getGenericResourceYAML(
    cluster: string,
    namespace: string,
    resource: string,
    name: string,
    group?: string
  ): Promise<GenericResourceManifest> {
    const params = new URLSearchParams();
    if (group) params.set("group", group);
    const qs = params.toString();
    const response = await this.request<{ success: boolean; data: GenericResourceManifest }>(
      `/explorer/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(resource)}/${encodeURIComponent(name)}${qs ? `?${qs}` : ""}`
    );
    if (!response.data) throw new Error("Resource not found");
    return response.data;
  }

  /**
   * Aplica um manifesto YAML (qualquer tipo de recurso)
   * PUT /api/v1/explorer/:cluster/:namespace/:resource/:name
   */
  async applyGenericResource(
    cluster: string,
    namespace: string,
    resource: string,
    name: string,
    body: { yaml: string; dryRun?: boolean; force?: boolean },
    group?: string
  ): Promise<ExplorerApplyResult> {
    const params = new URLSearchParams();
    if (group) params.set("group", group);
    const qs = params.toString();
    const response = await this.request<{ success: boolean; data: ExplorerApplyResult }>(
      `/explorer/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(resource)}/${encodeURIComponent(name)}${qs ? `?${qs}` : ""}`,
      { method: "PUT", body: JSON.stringify(body) }
    );
    if (!response.data) throw new Error("Apply failed");
    return response.data;
  }

  /**
   * Deleta qualquer recurso Kubernetes
   * DELETE /api/v1/explorer/:cluster/:namespace/:resource/:name
   */
  async deleteGenericResource(
    cluster: string,
    namespace: string,
    resource: string,
    name: string,
    group?: string
  ): Promise<{ success: boolean; message: string }> {
    const params = new URLSearchParams();
    if (group) params.set("group", group);
    const qs = params.toString();
    return this.request(
      `/explorer/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(resource)}/${encodeURIComponent(name)}${qs ? `?${qs}` : ""}`,
      { method: "DELETE" }
    );
  }

  /**
   * Gera diff unificado entre dois YAMLs
   * POST /api/v1/explorer/diff
   */
  async diffGenericResource(
    original: string,
    updated: string,
    fileName?: string
  ): Promise<ExplorerDiffResult> {
    const response = await this.request<{ success: boolean; data: ExplorerDiffResult }>(
      "/explorer/diff",
      { method: "POST", body: JSON.stringify({ originalYaml: original, updatedYaml: updated, fileName }) }
    );
    if (!response.data) throw new Error("Diff failed");
    return response.data;
  }

  /**
   * Valida um manifesto via dry-run
   * POST /api/v1/explorer/validate
   */
  async validateGenericResource(
    cluster: string,
    namespace: string,
    yaml: string
  ): Promise<{ valid: boolean }> {
    const response = await this.request<{ success: boolean; data: { valid: boolean } }>(
      "/explorer/validate",
      { method: "POST", body: JSON.stringify({ cluster, namespace, yaml }) }
    );
    if (!response.data) throw new Error("Validation failed");
    return response.data;
  }

  /**
   * Retorna output do kubectl describe para qualquer recurso
   * GET /api/v1/explorer/:cluster/:namespace/:resource/:name/describe
   */
  async describeGenericResource(
    cluster: string,
    namespace: string,
    resource: string,
    name: string
  ): Promise<{ describe: string }> {
    return this.request(
      `/explorer/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(resource)}/${encodeURIComponent(name)}/describe`
    );
  }

  // ── AWX Integration ────────────────────────────────────────────────────────

  /** Verifica se o AWX está configurado e acessível */
  async getAWXStatus(): Promise<AWXStatus> {
    return this.request<AWXStatus>("/awx/status");
  }

  /** Lista credenciais/certificados disponíveis no AWX */
  async getAWXCertificates(): Promise<AWXCertificate[]> {
    const data = await this.request<{ certificates: AWXCertificate[] }>("/awx/certificates");
    return data.certificates ?? [];
  }

  /** Busca as opções de cert_tls do survey do job template AWX (ex: template 25 ou 26) */
  async getAWXTemplateSurvey(templateId: number): Promise<string[]> {
    const data = await this.request<{ choices: string[] }>(`/awx/templates/${templateId}/survey`);
    return data.choices ?? [];
  }

  /** Lança job no AWX (template 25 = instalar, 26 = atualizar) */
  async launchAWXCertJob(payload: {
    template_id: number;
    app_name: string;
    subs_env: string;
    cluster_resource_group_name: string;
    namespace: string;
    cert_tls: string;
  }): Promise<AWXJobLaunch> {
    return this.request<AWXJobLaunch>("/awx/jobs/launch", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  }

  /** Retorna a URL do SSE stream de logs de um job AWX */
  getAWXJobStreamURL(jobId: number): string {
    return `/api/v1/awx/jobs/${jobId}/stream`;
  }

  /** Retorna resource_group e subs_env do cluster a partir do clusters-config.json */
  async getAWXClusterInfo(cluster: string): Promise<{ resource_group: string; subs_env: string; subscription: string }> {
    return this.request(`/awx/cluster-info?cluster=${encodeURIComponent(cluster)}`);
  }

  /** Salva credenciais AWX — modo manual (username + password) ou via Perfil SSO */
  async saveAWXCredentials(
    baseURL: string,
    opts: { username: string; password: string } | { useSSOProfile: true; loginIdentifier: "email" | "matricula" }
  ): Promise<void> {
    const body = "useSSOProfile" in opts
      ? { base_url: baseURL, use_sso_profile: true, login_identifier: opts.loginIdentifier }
      : { base_url: baseURL, username: opts.username, password: opts.password, use_sso_profile: false };
    await this.request<void>("/awx/credentials", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  /** Remove credenciais AWX salvas */
  async deleteAWXCredentials(): Promise<void> {
    await this.request<void>("/awx/credentials", { method: "DELETE" });
  }

  // ─── SSO Profile ───────────────────────────────────────────────────────────

  /** Busca perfil SSO corporativo (email + matrícula, sem senha) */
  async getSSOProfile(): Promise<import("./types").SSOProfile> {
    return this.request("/sso/profile");
  }

  /** Salva perfil SSO corporativo */
  async saveSSOProfile(email: string, matricula: string, password: string): Promise<void> {
    await this.request<void>("/sso/profile", {
      method: "PUT",
      body: JSON.stringify({ email, matricula, password }),
    });
  }

  /** Remove perfil SSO corporativo */
  async deleteSSOProfile(): Promise<void> {
    await this.request<void>("/sso/profile", { method: "DELETE" });
  }

  // ─── Command Runner ────────────────────────────────────────────────────────

  /** Inicia execução em lote e retorna session_id para streaming SSE */
  async executeCommand(req: import("./types").ExecuteCommandRequest): Promise<import("./types").ExecuteCommandResponse> {
    return this.request<import("./types").ExecuteCommandResponse>("/command-runner/execute", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  /** URL do SSE stream de uma execução */
  getCommandRunnerStreamURL(sessionId: string): string {
    const token = localStorage.getItem("auth_token");
    return `/api/v1/command-runner/stream/${sessionId}?token=${encodeURIComponent(token)}`;
  }

  /** Gera um comando kubectl/shell via AI a partir de uma descrição */
  async generateCommand(req: import("./types").GenerateCommandRequest): Promise<import("./types").GenerateCommandResponse> {
    return this.request<import("./types").GenerateCommandResponse>("/command-runner/generate", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  /** Força a parada de uma execução em andamento (mata os processos no servidor) */
  async cancelCommand(sessionId: string): Promise<void> {
    await this.request<void>(`/command-runner/session/${encodeURIComponent(sessionId)}`, {
      method: "DELETE",
    });
  }

  // ===== DYNATRACE =====

  async getDynatraceConfig(aiEmail: string): Promise<{
    base_url: string;
    has_token: boolean;
    enabled: boolean;
    tag_filter: string;
  }> {
    return this.request(`/dynatrace/config?ai_email=${encodeURIComponent(aiEmail)}`);
  }

  async testDynatraceConnection(aiEmail: string): Promise<{
    success: boolean;
    latency_ms?: number;
    base_url?: string;
    error?: string;
  }> {
    return this.request("/dynatrace/test", {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail }),
    });
  }

  async getDynatraceManagementZones(aiEmail: string): Promise<{
    zones: Array<{ id: string; name: string }>;
  }> {
    return this.request(`/dynatrace/management-zones?ai_email=${encodeURIComponent(aiEmail)}`);
  }

  async getDynatraceProblems(
    aiEmail: string,
    filter?: string,
    status?: string,
    from?: string,
    to?: string,
  ): Promise<{
    problems: import("../../types/healthcheck").DynatraceProblem[];
    total: number;
    fetched_at: string;
    ui_base_url?: string;
    dt_not_configured?: boolean;
    message?: string;
  }> {
    let url = `/dynatrace/problems?ai_email=${encodeURIComponent(aiEmail)}`;
    if (filter) url += `&filter=${encodeURIComponent(filter)}`;
    if (status) url += `&status=${encodeURIComponent(status)}`;
    if (from) url += `&from=${encodeURIComponent(from)}`;
    if (to) url += `&to=${encodeURIComponent(to)}`;
    return this.request(url);
  }

  async getDynatraceProblem(problemId: string, aiEmail: string): Promise<any> {
    return this.request(`/dynatrace/problems/${encodeURIComponent(problemId)}?ai_email=${encodeURIComponent(aiEmail)}`);
  }

  async getDynatraceProblemMetrics(problemId: string, aiEmail: string): Promise<import("../../types/healthcheck").DTProblemMetrics> {
    return this.request(`/dynatrace/problems/${encodeURIComponent(problemId)}/metrics?ai_email=${encodeURIComponent(aiEmail)}`);
  }

  async getDynatraceProblemContext(problemId: string, aiEmail: string): Promise<import("../../types/healthcheck").DTProblemContext> {
    return this.request(`/dynatrace/problems/${encodeURIComponent(problemId)}/context?ai_email=${encodeURIComponent(aiEmail)}`);
  }

  // ─── NodePool Registry (correlação Dynatrace aks-<pool>-vmss*) ──────────────

  async getNodePoolRegistry(cluster?: string): Promise<{ entries: NodePoolRegistryEntry[]; total: number }> {
    const params = cluster ? `?cluster=${encodeURIComponent(cluster)}` : "";
    return this.request(`/nodepools/registry${params}`);
  }

  async lookupNodePoolByEntityName(entityName: string): Promise<NodePoolLookupResult> {
    return this.request(`/nodepools/registry/lookup?name=${encodeURIComponent(entityName)}`);
  }

  async scanNodePoolRegistry(cluster?: string): Promise<{ results: any[]; total_inserted: number; scanned_at: string }> {
    const params = cluster ? `?cluster=${encodeURIComponent(cluster)}` : "";
    return this.request(`/nodepools/registry/scan${params}`, { method: "POST" });
  }

  async analyzeDynatraceProblem(problemId: string, aiEmail: string, signal?: AbortSignal): Promise<{
    problem_id: string;
    title: string;
    severity: string;
    analysis: string;
    action_items: Array<{
      urgency: string;
      app_section: string;
      workload?: string;
      namespace?: string;
      cluster?: string;
      action: string;
      reason: string;
    }>;
    analyzed_at: string;
  }> {
    return this.request(`/dynatrace/problems/${encodeURIComponent(problemId)}/analyze`, {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail }),
      signal,
    });
  }

  async investigateDynatraceProblem(problemId: string, aiEmail: string, signal?: AbortSignal): Promise<{
    problem_id: string;
    problem_title: string;
    root_cause_entity_name?: string;
    root_cause_entity_type?: string;
    identified_cluster?: string;
    identified_node_pool?: string;
    identified_namespace?: string;
    identified_workload?: string;
    health_check_result?: any;
    health_check_error?: string;
    dependencies?: Array<{
      service_name: string;
      service_type: string;
      deployment?: string;
      namespace: string;
      cluster: string;
    }>;
    ai_analysis?: string;
    ai_error?: string;
    investigated_at: string;
  }> {
    return this.request(`/dynatrace/problems/${encodeURIComponent(problemId)}/investigate`, {
      method: "POST",
      body: JSON.stringify({ ai_email: aiEmail }),
      signal,
    });
  }
  // ─── AWS SSO Auth ─────────────────────────────────────────────────────────

  async checkAwsSsoStatus(profile: string): Promise<{ profile: string; valid: boolean; login_in_progress: boolean }> {
    return this.request(`/aws/auth/status?profile=${encodeURIComponent(profile)}`);
  }

  async startAwsSsoLogin(profile: string): Promise<{
    profile: string;
    url?: string;
    user_code?: string;
    already_valid?: boolean;
    message?: string;
  }> {
    return this.request("/aws/auth/login", { method: "POST", body: JSON.stringify({ profile }) });
  }

  async pollAwsSsoLogin(profile: string): Promise<{ profile: string; done: boolean; success?: boolean; url?: string; user_code?: string }> {
    return this.request(`/aws/auth/poll?profile=${encodeURIComponent(profile)}`);
  }

  async listAwsSsoConfigs(): Promise<{ profiles: Record<string, { sso: { start_url: string; region: string; account_id: string; role_name: string }; region: string }> }> {
    return this.request("/aws/config");
  }

  async saveAwsSsoConfig(profile: string, sso: { start_url: string; region: string; account_id: string; role_name: string }, region: string): Promise<{ profile: string; message: string }> {
    return this.request("/aws/config", { method: "POST", body: JSON.stringify({ profile, sso, region }) });
  }

  async deleteAwsSsoConfig(profile: string): Promise<{ profile: string; message: string }> {
    return this.request(`/aws/config/${encodeURIComponent(profile)}`, { method: "DELETE" });
  }

  // ==================== Teams Integration ====================

  async getTeamsApprovalsToday(): Promise<{
    success: boolean;
    items: Array<{ chg: string; approval_url: string; extracted_at: string }>;
    last_updated: string | null;
    needs_refresh: boolean;
    refreshing: boolean;
  }> {
    return this.request("/teams/approvals/today");
  }

  async refreshTeamsApprovals(): Promise<{
    success: boolean;
    items: Array<{ chg: string; approval_url: string; extracted_at: string }>;
    last_updated: string | null;
    error?: string;
  }> {
    return this.request("/teams/approvals/refresh", { method: "POST" });
  }

  async searchTeamsCHG(chg: string): Promise<{
    found: boolean;
    item?: { chg: string; servicenow_url?: string; approval_url: string; description?: string; extracted_at: string };
  }> {
    return this.request(`/teams/approvals/search?chg=${encodeURIComponent(chg.toUpperCase())}`);
  }

  async searchTeamsByName(q: string): Promise<{
    found: boolean;
    count: number;
    items?: { chg: string; servicenow_url?: string; approval_url: string; description?: string; extracted_at: string }[];
  }> {
    return this.request(`/teams/approvals/search?q=${encodeURIComponent(q)}`);
  }

  // ==================== Teams Broadcast ====================

  async getBroadcastChats(q?: string): Promise<{
    query?: string;
    searched_at?: string;
    count: number;
    chats: { id: string; display_name: string; source: string }[];
  }> {
    const qs = q ? `?q=${encodeURIComponent(q)}` : "";
    return this.request(`/teams/broadcast/chats${qs}`);
  }

  async scanBroadcastChats(): Promise<{ count: number; path: string; error?: string }> {
    return this.request("/teams/broadcast/chats/scan", { method: "POST" });
  }

  async listBroadcastTemplates(): Promise<{ templates: { filename: string; updated_at: string; size: number }[] }> {
    return this.request("/teams/broadcast/templates");
  }

  async saveBroadcastTemplate(filename: string, content: string): Promise<{ filename: string }> {
    return this.request("/teams/broadcast/templates", {
      method: "POST",
      body: JSON.stringify({ filename, content }),
    });
  }

  async getBroadcastTemplate(filename: string): Promise<{ filename: string; content: string }> {
    return this.request(`/teams/broadcast/templates/${encodeURIComponent(filename)}`);
  }

  async deleteBroadcastTemplate(filename: string): Promise<{ deleted: string }> {
    return this.request(`/teams/broadcast/templates/${encodeURIComponent(filename)}`, { method: "DELETE" });
  }

  async sendBroadcastMessage(payload: {
    session_id: string;
    thread_ids: string[];
    markdown: string;
    html?: string;
  }): Promise<{ session_id?: string; total?: number; error?: string }> {
    return this.request("/teams/broadcast/send", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  // ==================== SRE Approval ====================

  async getSreApprovalInfo(approvalUrl: string): Promise<{
    success: boolean;
    approval_info?: {
      id: string;
      change_number: string;
      title: string;
      application: string;
      version: string;
      squad_owner: string;
      status: string;
      is_finalized: boolean;
      approver_email?: string;
      approver_squad?: string;
    };
    error?: string;
  }> {
    return this.request(`/sre-approval/info?url=${encodeURIComponent(approvalUrl)}`);
  }

  async sreApprove(approvalUrl: string, approverEmail?: string): Promise<{
    success: boolean;
    message?: string;
    error?: string;
    already_finalized?: boolean;
    approver_email?: string;
  }> {
    return this.request("/sre-approval/approve", {
      method: "POST",
      body: JSON.stringify({ approval_url: approvalUrl, approver_email: approverEmail || "" }),
    });
  }

  async getSreCurrentUser(): Promise<{ success: boolean; email: string; error?: string }> {
    return this.request("/sre-approval/current-user");
  }
}

// Singleton instance
export const apiClient = new APIClient();

// Export for convenience
export default apiClient;
