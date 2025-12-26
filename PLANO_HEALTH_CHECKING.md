# Plano de Implementação: Health Checking Completo para Clusters AKS

**Data Início:** 25/12/2025
**Data Atualização:** 26/12/2025
**Versão:** v1.4.0
**Estimativa:** 5 dias de implementação
**Status:** 🟢 Dia 1 Completo (Backend - Deployment Checker)

---

## 📋 Visão Geral

Sistema completo de health checking multi-cluster que valida conectividade de TODOS os serviços em clusters AKS:
- **Multi-cluster paralelo**: Testa 1 ou mais clusters simultaneamente (mínimo 2 workers)
- **Filtros de ambiente**: Seleção por prod/hlg/all ou manual (checkboxes individuais)
- **Deployments saudáveis**: Status de réplicas, crashes, image pull errors
- **Conectividade de serviços externos**: MongoDB, Redis, Kafka, PostgreSQL, EventHub, HTTP
- **Validação de ConfigMaps/Secrets**: Existência, chaves obrigatórias, formato
- **Interface pragmática**: Progress em tempo real (SSE), sugestões de troubleshooting, histórico

---

## 🏛️ Decisões de Arquitetura (26/12/2025)

### Multi-Cluster Paralelo
- **Problema original**: Plano inicial considerava apenas 1 cluster por vez
- **Solução implementada**: Worker pool para processar múltiplos clusters em paralelo
- **Comportamento**:
  - 1 cluster: execução sequencial (sem pool)
  - 2+ clusters: worker pool com mínimo 2 workers, máximo `min(NumCPU, total_clusters)`
  - Exemplo: 5 clusters em máquina 8-core → 5 workers (todos em paralelo)

### Filtros de Ambiente
- **Requisito**: Testar apenas clusters de produção, ou hlg, ou todos
- **Implementação**: Dois modos de seleção (mutuamente exclusivos)
  1. **Filtro por ambiente** (`environment: "prod"/"hlg"/"all"`)
     - Detecta ambiente automaticamente pelo nome do cluster
     - Padrões: `*prod*`, `*prd*` → "prod" | `*hlg*`, `*homolog*`, `*staging*` → "hlg"
  2. **Seleção manual** (`clusters: ["cluster1", "cluster2"]`)
     - Checkboxes individuais para escolher clusters específicos
     - Útil para testar clusters críticos específicos

### Progress Tracking via SSE
- **Integração**: Reutiliza `internal/web/sse/ProgressTracker` existente
- **Eventos**: Publicados em tempo real para cada cluster sendo processado
- **Campos**: `sessionID`, `cluster`, `phase`, `message`, `progress` (0-100%), `status`

---

## 🎯 Objetivos

### Funcionalidades Principais

1. **Health Check de Deployments**
   - Status de réplicas (ready/total)
   - Containers em CrashLoopBackOff
   - Image pull errors
   - Recursos (CPU/Memory) próximos do limite

2. **Health Check de Serviços Externos**
   - MongoDB (connection string test)
   - Redis (PING command)
   - PostgreSQL (connection test)
   - Kafka (broker connectivity)
   - EventHub (connection string validation)
   - RabbitMQ/AMQP (connection test)
   - HTTP APIs (endpoint ping)

3. **Validação de Configurações**
   - ConfigMaps referenciados existem
   - Secrets referenciados existem
   - Connection strings têm formato válido
   - Credenciais não vazias

4. **Interface Pragmática**
   - Status visual (✅ Healthy, ⚠️ Warning, ❌ Critical)
   - Progress bar em tempo real (SSE)
   - Sugestões de ações para problemas detectados
   - Histórico de health checks
   - Export para JSON/CSV

---

## 🏗️ Arquitetura Proposta

### Backend (Go)

```
internal/
├── healthcheck/
│   ├── models.go              # Estruturas de dados
│   ├── orchestrator.go        # Orquestrador principal
│   ├── deployment_checker.go  # Valida deployments
│   ├── service_checker.go     # Testa serviços externos
│   ├── config_checker.go      # Valida ConfigMaps/Secrets
│   ├── connectivity.go        # Testes de conectividade
│   ├── analyzers/
│   │   ├── mongodb.go         # MongoDB health checker
│   │   ├── redis.go           # Redis health checker
│   │   ├── postgres.go        # PostgreSQL health checker
│   │   ├── kafka.go           # Kafka health checker
│   │   ├── eventhub.go        # EventHub health checker
│   │   └── http.go            # HTTP endpoint checker
│   └── storage.go             # Persistência de histórico
│
├── web/
│   ├── handlers/
│   │   └── healthcheck.go     # REST API endpoints
│   └── routes.go              # Registro de rotas
```

### Frontend (React/TypeScript)

```
internal/web/frontend/src/
├── components/
│   ├── HealthCheckingTab.tsx             # Container principal (40KB)
│   ├── HealthCheckCard.tsx               # Card de resultado (2KB)
│   ├── HealthCheckResultsPanel.tsx       # Painel de resultados (12KB)
│   ├── HealthCheckProgressModal.tsx      # Modal SSE progress (10KB)
│   ├── HealthCheckConfigDialog.tsx       # Configuração de checks (8KB)
│   ├── HealthCheckResourceSelector.tsx   # Seletor de recursos (6KB)
│   ├── HealthCheckSuggestions.tsx        # Sugestões de ações (6KB)
│   └── HealthCheckHistory.tsx            # Histórico (8KB)
│
├── hooks/
│   ├── useHealthChecking.ts              # Lógica principal (60 linhas)
│   ├── useHealthCheckProgress.ts         # SSE handling (80 linhas)
│   └── useHealthCheckHistory.ts          # Histórico (40 linhas)
│
├── lib/api/
│   └── client.ts                         # Adicionar métodos health check
│
└── types/
    └── healthcheck.ts                    # Tipos TypeScript
```

---

## 📊 Modelos de Dados

### Backend (Go)

```go
// Tipos de recursos suportados
type ResourceType string
const (
    ResourceDeployment ResourceType = "Deployment"
    ResourceService    ResourceType = "Service"
    ResourceConfigMap  ResourceType = "ConfigMap"
    ResourceSecret     ResourceType = "Secret"
)

// Tipos de serviços externos
type ServiceType string
const (
    ServiceMongoDB   ServiceType = "MongoDB"
    ServiceRedis     ServiceType = "Redis"
    ServicePostgres  ServiceType = "PostgreSQL"
    ServiceKafka     ServiceType = "Kafka"
    ServiceEventHub  ServiceType = "EventHub"
    ServiceRabbitMQ  ServiceType = "RabbitMQ"
    ServiceHTTP      ServiceType = "HTTP"
)

// Status de health
type HealthStatus string
const (
    StatusHealthy  HealthStatus = "healthy"
    StatusWarning  HealthStatus = "warning"
    StatusCritical HealthStatus = "critical"
    StatusUnknown  HealthStatus = "unknown"
)

// Request de health check
type HealthCheckRequest struct {
    // Modo 1: Filtro por ambiente (prod, hlg, all)
    Environment string `json:"environment,omitempty"` // "prod", "hlg", "all" - se preenchido, ignora Clusters

    // Modo 2: Seleção manual de clusters (checkboxes individuais)
    Clusters []string `json:"clusters,omitempty"` // Se preenchido, ignora Environment

    Namespaces []string `json:"namespaces"` // Vazio = todos

    // Opções de verificação
    CheckDeployments bool `json:"check_deployments"`
    CheckServices    bool `json:"check_services"`
    CheckConfigs     bool `json:"check_configs"`

    // Timeout por check (segundos)
    Timeout int `json:"timeout"` // Padrão: 10s

    // Paralelismo (apenas para múltiplos clusters)
    // Se Clusters > 1: mínimo 2 workers, máximo = NumCPU ou total de clusters
    MaxParallel int `json:"max_parallel"` // Padrão: min(NumCPU, len(Clusters))
}

// Resultado de health check
type HealthCheckResult struct {
    ID         string       `json:"id"`          // UUID
    Cluster    string       `json:"cluster"`
    Namespace  string       `json:"namespace"`
    StartedAt  time.Time    `json:"started_at"`
    FinishedAt time.Time    `json:"finished_at"`
    Duration   int64        `json:"duration_ms"` // Milissegundos

    // Resultados
    DeploymentResults []DeploymentHealth `json:"deployment_results"`
    ServiceResults    []ServiceHealth    `json:"service_results"`
    ConfigResults     []ConfigHealth     `json:"config_results"`

    // Resumo
    TotalChecks   int          `json:"total_checks"`
    HealthyCount  int          `json:"healthy_count"`
    WarningCount  int          `json:"warning_count"`
    CriticalCount int          `json:"critical_count"`
    OverallStatus HealthStatus `json:"overall_status"`
}

// Health de Deployment
type DeploymentHealth struct {
    Name      string       `json:"name"`
    Namespace string       `json:"namespace"`
    Status    HealthStatus `json:"status"`

    // Detalhes
    ReplicasReady    int32 `json:"replicas_ready"`
    ReplicasDesired  int32 `json:"replicas_desired"`
    ContainersCrash  int32 `json:"containers_crash"`
    ImagePullErrors  int32 `json:"image_pull_errors"`

    // Recursos
    CPUUsagePercent    float64 `json:"cpu_usage_percent"`    // 0-100
    MemoryUsagePercent float64 `json:"memory_usage_percent"` // 0-100

    // Mensagem
    Message      string   `json:"message"`
    Suggestions  []string `json:"suggestions"`
    CheckedAt    time.Time `json:"checked_at"`
}

// Health de Serviço Externo
type ServiceHealth struct {
    Name         string       `json:"name"`
    Namespace    string       `json:"namespace"`
    ServiceType  ServiceType  `json:"service_type"`
    Status       HealthStatus `json:"status"`

    // Conectividade
    Reachable       bool   `json:"reachable"`
    LatencyMs       int64  `json:"latency_ms"`
    ConnectionError string `json:"connection_error,omitempty"`

    // Detalhes específicos
    Details map[string]interface{} `json:"details,omitempty"`

    // Fonte (ConfigMap/Secret)
    ConfigSource string `json:"config_source"` // "configmap:my-config" ou "secret:my-secret"

    // Mensagem
    Message     string    `json:"message"`
    Suggestions []string  `json:"suggestions"`
    CheckedAt   time.Time `json:"checked_at"`
}

// Health de ConfigMap/Secret
type ConfigHealth struct {
    Name         string       `json:"name"`
    Namespace    string       `json:"namespace"`
    ResourceType ResourceType `json:"resource_type"` // ConfigMap ou Secret
    Status       HealthStatus `json:"status"`

    // Validações
    Exists           bool     `json:"exists"`
    HasRequiredKeys  bool     `json:"has_required_keys"`
    MissingKeys      []string `json:"missing_keys,omitempty"`
    InvalidValues    []string `json:"invalid_values,omitempty"`

    // Mensagem
    Message     string    `json:"message"`
    Suggestions []string  `json:"suggestions"`
    CheckedAt   time.Time `json:"checked_at"`
}

// Progress de health check (SSE)
type HealthCheckProgress struct {
    SessionID string       `json:"session_id"`
    Phase     string       `json:"phase"` // "deployments", "services", "configs"
    Message   string       `json:"message"`
    Progress  int          `json:"progress"` // 0-100
    Status    HealthStatus `json:"status"`
    Timestamp time.Time    `json:"timestamp"`
}
```

### Frontend (TypeScript)

```typescript
// Tipos TypeScript (mirror do backend)
type ResourceType = "Deployment" | "Service" | "ConfigMap" | "Secret";
type ServiceType = "MongoDB" | "Redis" | "PostgreSQL" | "Kafka" | "EventHub" | "RabbitMQ" | "HTTP";
type HealthStatus = "healthy" | "warning" | "critical" | "unknown";

interface HealthCheckRequest {
  cluster: string;
  namespaces: string[];
  check_deployments: boolean;
  check_services: boolean;
  check_configs: boolean;
  timeout: number;
}

interface HealthCheckResult {
  id: string;
  cluster: string;
  namespace: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;

  deployment_results: DeploymentHealth[];
  service_results: ServiceHealth[];
  config_results: ConfigHealth[];

  total_checks: number;
  healthy_count: number;
  warning_count: number;
  critical_count: number;
  overall_status: HealthStatus;
}

interface DeploymentHealth {
  name: string;
  namespace: string;
  status: HealthStatus;
  replicas_ready: number;
  replicas_desired: number;
  containers_crash: number;
  image_pull_errors: number;
  cpu_usage_percent: number;
  memory_usage_percent: number;
  message: string;
  suggestions: string[];
  checked_at: string;
}

interface ServiceHealth {
  name: string;
  namespace: string;
  service_type: ServiceType;
  status: HealthStatus;
  reachable: boolean;
  latency_ms: number;
  connection_error?: string;
  details?: Record<string, any>;
  config_source: string;
  message: string;
  suggestions: string[];
  checked_at: string;
}

interface ConfigHealth {
  name: string;
  namespace: string;
  resource_type: ResourceType;
  status: HealthStatus;
  exists: boolean;
  has_required_keys: boolean;
  missing_keys?: string[];
  invalid_values?: string[];
  message: string;
  suggestions: string[];
  checked_at: string;
}

interface HealthCheckProgress {
  session_id: string;
  phase: string;
  message: string;
  progress: number;
  status: HealthStatus;
  timestamp: string;
}
```

---

## 🔧 Implementação Backend

### 1. Orquestrador (orchestrator.go)

**Responsabilidade:** Coordenar todos os health checks

```go
type Orchestrator struct {
    kubeManager       *config.KubeConfigManager
    deploymentChecker *DeploymentChecker
    serviceChecker    *ServiceChecker
    configChecker     *ConfigChecker
    storage           *HealthCheckStorage
    sseBroker         *sse.Broker
}

func (o *Orchestrator) ExecuteHealthCheck(ctx context.Context, req HealthCheckRequest) (*HealthCheckResult, error) {
    // 1. Criar sessão
    sessionID := uuid.New().String()
    result := &HealthCheckResult{
        ID:        sessionID,
        Cluster:   req.Cluster,
        StartedAt: time.Now(),
    }

    // 2. Obter cliente Kubernetes
    client, err := o.kubeManager.GetClient(req.Cluster)
    if err != nil {
        return nil, fmt.Errorf("failed to get client: %w", err)
    }

    // 3. Determinar namespaces
    namespaces := req.Namespaces
    if len(namespaces) == 0 {
        namespaces, err = getAllNamespaces(ctx, client)
        if err != nil {
            return nil, err
        }
    }

    // 4. Executar checks em paralelo
    var wg sync.WaitGroup
    var mu sync.Mutex

    totalPhases := 0
    if req.CheckDeployments { totalPhases++ }
    if req.CheckServices { totalPhases++ }
    if req.CheckConfigs { totalPhases++ }
    currentPhase := 0

    // 4.1 Check Deployments
    if req.CheckDeployments {
        wg.Add(1)
        go func() {
            defer wg.Done()
            o.publishProgress(sessionID, "deployments", "Verificando deployments...", currentPhase*100/totalPhases)

            deploymentResults := o.deploymentChecker.CheckAll(ctx, client, namespaces, req.Timeout)

            mu.Lock()
            result.DeploymentResults = deploymentResults
            mu.Unlock()

            currentPhase++
        }()
    }

    // 4.2 Check Services
    if req.CheckServices {
        wg.Add(1)
        go func() {
            defer wg.Done()
            o.publishProgress(sessionID, "services", "Testando conectividade de serviços...", currentPhase*100/totalPhases)

            serviceResults := o.serviceChecker.CheckAll(ctx, client, namespaces, req.Timeout)

            mu.Lock()
            result.ServiceResults = serviceResults
            mu.Unlock()

            currentPhase++
        }()
    }

    // 4.3 Check Configs
    if req.CheckConfigs {
        wg.Add(1)
        go func() {
            defer wg.Done()
            o.publishProgress(sessionID, "configs", "Validando ConfigMaps e Secrets...", currentPhase*100/totalPhases)

            configResults := o.configChecker.CheckAll(ctx, client, namespaces)

            mu.Lock()
            result.ConfigResults = configResults
            mu.Unlock()

            currentPhase++
        }()
    }

    // 5. Aguardar conclusão
    wg.Wait()

    // 6. Calcular resumo
    result.FinishedAt = time.Now()
    result.Duration = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
    o.calculateSummary(result)

    // 7. Publicar conclusão
    o.publishProgress(sessionID, "complete", "Health check concluído", 100)

    // 8. Salvar no histórico
    if err := o.storage.Save(ctx, result); err != nil {
        log.Error().Err(err).Msg("Failed to save health check result")
    }

    return result, nil
}

func (o *Orchestrator) calculateSummary(result *HealthCheckResult) {
    statusCount := map[HealthStatus]int{
        StatusHealthy:  0,
        StatusWarning:  0,
        StatusCritical: 0,
    }

    // Contar deployments
    for _, d := range result.DeploymentResults {
        statusCount[d.Status]++
    }

    // Contar services
    for _, s := range result.ServiceResults {
        statusCount[s.Status]++
    }

    // Contar configs
    for _, c := range result.ConfigResults {
        statusCount[c.Status]++
    }

    result.TotalChecks = len(result.DeploymentResults) + len(result.ServiceResults) + len(result.ConfigResults)
    result.HealthyCount = statusCount[StatusHealthy]
    result.WarningCount = statusCount[StatusWarning]
    result.CriticalCount = statusCount[StatusCritical]

    // Determinar status geral
    if result.CriticalCount > 0 {
        result.OverallStatus = StatusCritical
    } else if result.WarningCount > 0 {
        result.OverallStatus = StatusWarning
    } else {
        result.OverallStatus = StatusHealthy
    }
}
```

### 2. Deployment Checker (deployment_checker.go)

**Responsabilidade:** Validar saúde de Deployments

```go
type DeploymentChecker struct{}

func (c *DeploymentChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int) []DeploymentHealth {
    results := []DeploymentHealth{}

    for _, ns := range namespaces {
        deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
        if err != nil {
            log.Error().Err(err).Str("namespace", ns).Msg("Failed to list deployments")
            continue
        }

        for _, d := range deployments.Items {
            health := c.Check(ctx, client, ns, d.Name, timeout)
            results = append(results, health)
        }
    }

    return results
}

func (c *DeploymentChecker) Check(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout int) DeploymentHealth {
    deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return DeploymentHealth{
            Name:      name,
            Namespace: namespace,
            Status:    StatusCritical,
            Message:   fmt.Sprintf("Failed to get deployment: %v", err),
            CheckedAt: time.Now(),
        }
    }

    health := DeploymentHealth{
        Name:            name,
        Namespace:       namespace,
        ReplicasReady:   deployment.Status.ReadyReplicas,
        ReplicasDesired: *deployment.Spec.Replicas,
        CheckedAt:       time.Now(),
    }

    // Verificar réplicas
    if health.ReplicasReady == 0 && health.ReplicasDesired > 0 {
        health.Status = StatusCritical
        health.Message = "Nenhuma réplica está pronta"
        health.Suggestions = append(health.Suggestions, "kubectl describe deployment "+name+" -n "+namespace)
        health.Suggestions = append(health.Suggestions, "Verificar eventos do deployment")
        return health
    }

    if health.ReplicasReady < health.ReplicasDesired {
        health.Status = StatusWarning
        health.Message = fmt.Sprintf("Apenas %d/%d réplicas prontas", health.ReplicasReady, health.ReplicasDesired)
        health.Suggestions = append(health.Suggestions, "kubectl get pods -n "+namespace+" -l app="+name)
        health.Suggestions = append(health.Suggestions, "Verificar logs dos pods")
    }

    // Buscar pods do deployment
    pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("app=%s", name),
    })
    if err != nil {
        log.Error().Err(err).Msg("Failed to list pods")
    } else {
        // Contar containers em crash
        crashCount := 0
        imagePullErrors := 0

        for _, pod := range pods.Items {
            for _, cs := range pod.Status.ContainerStatuses {
                if cs.State.Waiting != nil {
                    if cs.State.Waiting.Reason == "CrashLoopBackOff" ||
                       cs.State.Waiting.Reason == "Error" {
                        crashCount++
                    }
                    if cs.State.Waiting.Reason == "ImagePullBackOff" ||
                       cs.State.Waiting.Reason == "ErrImagePull" {
                        imagePullErrors++
                    }
                }
            }
        }

        health.ContainersCrash = int32(crashCount)
        health.ImagePullErrors = int32(imagePullErrors)

        if crashCount > 0 {
            health.Status = StatusCritical
            health.Message = fmt.Sprintf("%d containers em CrashLoopBackOff", crashCount)
            health.Suggestions = append(health.Suggestions, "kubectl logs <pod-name> -n "+namespace+" --previous")
            health.Suggestions = append(health.Suggestions, "Analisar logs anteriores ao crash")
        }

        if imagePullErrors > 0 {
            health.Status = StatusCritical
            health.Message = fmt.Sprintf("%d erros ao puxar imagens", imagePullErrors)
            health.Suggestions = append(health.Suggestions, "Verificar se a imagem existe no registry")
            health.Suggestions = append(health.Suggestions, "Validar ImagePullSecrets")
        }
    }

    // Se nenhum problema detectado
    if health.Status == "" {
        health.Status = StatusHealthy
        health.Message = "Deployment saudável"
    }

    return health
}
```

### 3. Service Checker (service_checker.go)

**Responsabilidade:** Testar conectividade de serviços externos

```go
type ServiceChecker struct {
    analyzers map[ServiceType]ServiceAnalyzer
}

type ServiceAnalyzer interface {
    Check(ctx context.Context, connectionString string, timeout int) (bool, int64, error)
    GetDetails(ctx context.Context, connectionString string) (map[string]interface{}, error)
}

func NewServiceChecker() *ServiceChecker {
    return &ServiceChecker{
        analyzers: map[ServiceType]ServiceAnalyzer{
            ServiceMongoDB:  &MongoDBAnalyzer{},
            ServiceRedis:    &RedisAnalyzer{},
            ServicePostgres: &PostgresAnalyzer{},
            ServiceKafka:    &KafkaAnalyzer{},
            ServiceEventHub: &EventHubAnalyzer{},
            ServiceRabbitMQ: &RabbitMQAnalyzer{},
            ServiceHTTP:     &HTTPAnalyzer{},
        },
    }
}

func (c *ServiceChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int) []ServiceHealth {
    results := []ServiceHealth{}

    for _, ns := range namespaces {
        // Buscar ConfigMaps e Secrets com connection strings
        configMaps, _ := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
        secrets, _ := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})

        // Extrair connection strings de ConfigMaps
        for _, cm := range configMaps.Items {
            for key, value := range cm.Data {
                serviceType, connStr := c.detectServiceType(value)
                if serviceType != "" {
                    health := c.Check(ctx, ns, cm.Name, key, serviceType, connStr, timeout)
                    health.ConfigSource = fmt.Sprintf("configmap:%s/%s", cm.Name, key)
                    results = append(results, health)
                }
            }
        }

        // Extrair connection strings de Secrets
        for _, secret := range secrets.Items {
            for key, value := range secret.Data {
                valueStr := string(value)
                serviceType, connStr := c.detectServiceType(valueStr)
                if serviceType != "" {
                    health := c.Check(ctx, ns, secret.Name, key, serviceType, connStr, timeout)
                    health.ConfigSource = fmt.Sprintf("secret:%s/%s", secret.Name, key)
                    results = append(results, health)
                }
            }
        }
    }

    return results
}

func (c *ServiceChecker) detectServiceType(value string) (ServiceType, string) {
    // Detectar tipo de serviço por padrões de connection string

    // MongoDB: mongodb://... ou mongodb+srv://...
    if strings.HasPrefix(value, "mongodb://") || strings.HasPrefix(value, "mongodb+srv://") {
        return ServiceMongoDB, value
    }

    // Redis: redis://...
    if strings.HasPrefix(value, "redis://") {
        return ServiceRedis, value
    }

    // PostgreSQL: postgresql://... ou postgres://...
    if strings.HasPrefix(value, "postgresql://") || strings.HasPrefix(value, "postgres://") {
        return ServicePostgres, value
    }

    // Kafka: broker:port (formato simples)
    if matched, _ := regexp.MatchString(`^[a-zA-Z0-9\-\.]+:\d+$`, value); matched {
        return ServiceKafka, value
    }

    // EventHub: Endpoint=sb://...
    if strings.Contains(value, "Endpoint=sb://") {
        return ServiceEventHub, value
    }

    // RabbitMQ: amqp://...
    if strings.HasPrefix(value, "amqp://") {
        return ServiceRabbitMQ, value
    }

    // HTTP: http://... ou https://...
    if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
        return ServiceHTTP, value
    }

    return "", ""
}

func (c *ServiceChecker) Check(ctx context.Context, namespace, resourceName, key string, serviceType ServiceType, connStr string, timeout int) ServiceHealth {
    health := ServiceHealth{
        Name:        fmt.Sprintf("%s/%s", resourceName, key),
        Namespace:   namespace,
        ServiceType: serviceType,
        CheckedAt:   time.Now(),
    }

    analyzer, exists := c.analyzers[serviceType]
    if !exists {
        health.Status = StatusUnknown
        health.Message = fmt.Sprintf("Analyzer não implementado para %s", serviceType)
        return health
    }

    // Executar check com timeout
    timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
    defer cancel()

    start := time.Now()
    reachable, latency, err := analyzer.Check(timeoutCtx, connStr, timeout)
    elapsed := time.Since(start).Milliseconds()

    health.Reachable = reachable
    health.LatencyMs = latency

    if err != nil {
        health.Status = StatusCritical
        health.ConnectionError = err.Error()
        health.Message = fmt.Sprintf("Falha ao conectar: %v", err)
        health.Suggestions = append(health.Suggestions, "Verificar se o serviço está acessível")
        health.Suggestions = append(health.Suggestions, "Validar connection string no ConfigMap/Secret")
        health.Suggestions = append(health.Suggestions, "Testar conectividade com kubectl exec")
        return health
    }

    // Obter detalhes adicionais
    details, _ := analyzer.GetDetails(timeoutCtx, connStr)
    health.Details = details

    // Determinar status baseado em latência
    if latency > 1000 {
        health.Status = StatusWarning
        health.Message = fmt.Sprintf("Conectado, mas latência alta (%dms)", latency)
        health.Suggestions = append(health.Suggestions, "Investigar performance da rede")
    } else {
        health.Status = StatusHealthy
        health.Message = fmt.Sprintf("Conectado com sucesso (latência: %dms)", latency)
    }

    return health
}
```

### 4. Analyzers (analyzers/)

#### MongoDB Analyzer (mongodb.go)

```go
type MongoDBAnalyzer struct{}

func (a *MongoDBAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
    start := time.Now()

    clientOptions := options.Client().ApplyURI(connStr).
        SetServerSelectionTimeout(time.Duration(timeout) * time.Second)

    client, err := mongo.Connect(ctx, clientOptions)
    if err != nil {
        return false, 0, fmt.Errorf("failed to connect: %w", err)
    }
    defer client.Disconnect(ctx)

    // Ping para validar conectividade
    err = client.Ping(ctx, nil)
    if err != nil {
        return false, 0, fmt.Errorf("ping failed: %w", err)
    }

    latency := time.Since(start).Milliseconds()
    return true, latency, nil
}

func (a *MongoDBAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
    clientOptions := options.Client().ApplyURI(connStr)
    client, err := mongo.Connect(ctx, clientOptions)
    if err != nil {
        return nil, err
    }
    defer client.Disconnect(ctx)

    // Obter informações do servidor
    var result bson.M
    err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&result)
    if err != nil {
        return nil, err
    }

    return map[string]interface{}{
        "version":     result["version"],
        "uptime":      result["uptime"],
        "connections": result["connections"],
    }, nil
}
```

#### Redis Analyzer (redis.go)

```go
type RedisAnalyzer struct{}

func (a *RedisAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
    start := time.Now()

    opt, err := redis.ParseURL(connStr)
    if err != nil {
        return false, 0, fmt.Errorf("invalid connection string: %w", err)
    }

    opt.DialTimeout = time.Duration(timeout) * time.Second
    opt.ReadTimeout = time.Duration(timeout) * time.Second

    client := redis.NewClient(opt)
    defer client.Close()

    // PING command
    pong, err := client.Ping(ctx).Result()
    if err != nil {
        return false, 0, fmt.Errorf("PING failed: %w", err)
    }

    if pong != "PONG" {
        return false, 0, fmt.Errorf("unexpected PING response: %s", pong)
    }

    latency := time.Since(start).Milliseconds()
    return true, latency, nil
}

func (a *RedisAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
    opt, err := redis.ParseURL(connStr)
    if err != nil {
        return nil, err
    }

    client := redis.NewClient(opt)
    defer client.Close()

    // INFO command
    info, err := client.Info(ctx).Result()
    if err != nil {
        return nil, err
    }

    // Parse INFO response (key:value pairs)
    details := make(map[string]interface{})
    lines := strings.Split(info, "\r\n")
    for _, line := range lines {
        if strings.Contains(line, ":") {
            parts := strings.SplitN(line, ":", 2)
            details[parts[0]] = parts[1]
        }
    }

    return details, nil
}
```

#### PostgreSQL Analyzer (postgres.go)

```go
type PostgresAnalyzer struct{}

func (a *PostgresAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
    start := time.Now()

    connConfig, err := pgx.ParseConfig(connStr)
    if err != nil {
        return false, 0, fmt.Errorf("invalid connection string: %w", err)
    }

    connConfig.ConnectTimeout = time.Duration(timeout) * time.Second

    conn, err := pgx.ConnectConfig(ctx, connConfig)
    if err != nil {
        return false, 0, fmt.Errorf("failed to connect: %w", err)
    }
    defer conn.Close(ctx)

    // Simple query test
    var result int
    err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
    if err != nil {
        return false, 0, fmt.Errorf("query failed: %w", err)
    }

    latency := time.Since(start).Milliseconds()
    return true, latency, nil
}

func (a *PostgresAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
    conn, err := pgx.Connect(ctx, connStr)
    if err != nil {
        return nil, err
    }
    defer conn.Close(ctx)

    var version string
    err = conn.QueryRow(ctx, "SELECT version()").Scan(&version)
    if err != nil {
        return nil, err
    }

    var dbSize int64
    err = conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&dbSize)
    if err != nil {
        dbSize = 0
    }

    return map[string]interface{}{
        "version":     version,
        "database_size_bytes": dbSize,
    }, nil
}
```

#### HTTP Analyzer (http.go)

```go
type HTTPAnalyzer struct{}

func (a *HTTPAnalyzer) Check(ctx context.Context, endpoint string, timeout int) (bool, int64, error) {
    start := time.Now()

    client := &http.Client{
        Timeout: time.Duration(timeout) * time.Second,
    }

    req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
    if err != nil {
        return false, 0, fmt.Errorf("failed to create request: %w", err)
    }

    resp, err := client.Do(req)
    if err != nil {
        return false, 0, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    latency := time.Since(start).Milliseconds()

    if resp.StatusCode >= 400 {
        return false, latency, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
    }

    return true, latency, nil
}

func (a *HTTPAnalyzer) GetDetails(ctx context.Context, endpoint string) (map[string]interface{}, error) {
    resp, err := http.Get(endpoint)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    return map[string]interface{}{
        "status_code":    resp.StatusCode,
        "content_type":   resp.Header.Get("Content-Type"),
        "content_length": resp.ContentLength,
    }, nil
}
```

### 5. REST API Handlers (handlers/healthcheck.go)

```go
type HealthCheckHandler struct {
    orchestrator *healthcheck.Orchestrator
    sseBroker    *sse.Broker
}

func NewHealthCheckHandler(orch *healthcheck.Orchestrator, broker *sse.Broker) *HealthCheckHandler {
    return &HealthCheckHandler{
        orchestrator: orch,
        sseBroker:    broker,
    }
}

// POST /api/v1/healthcheck/run
func (h *HealthCheckHandler) Run(c *gin.Context) {
    var req healthcheck.HealthCheckRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "success": false,
            "error":   gin.H{"message": err.Error()},
        })
        return
    }

    // Executar em goroutine (long-running)
    go func() {
        result, err := h.orchestrator.ExecuteHealthCheck(context.Background(), req)
        if err != nil {
            log.Error().Err(err).Msg("Health check failed")
        }
    }()

    c.JSON(http.StatusAccepted, gin.H{
        "success": true,
        "message": "Health check iniciado",
    })
}

// GET /api/v1/healthcheck/progress?session={id} (SSE)
func (h *HealthCheckHandler) Progress(c *gin.Context) {
    sessionID := c.Query("session")
    if sessionID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "session ID required"})
        return
    }

    messageChan := make(chan string)
    h.sseBroker.NewClients <- messageChan
    defer func() {
        h.sseBroker.DefunctClients <- messageChan
    }()

    c.Stream(func(w io.Writer) bool {
        if msg, ok := <-messageChan; ok {
            c.SSEvent("progress", msg)
            return true
        }
        return false
    })
}

// GET /api/v1/healthcheck/history?cluster=x&namespace=y
func (h *HealthCheckHandler) History(c *gin.Context) {
    cluster := c.Query("cluster")
    namespace := c.Query("namespace")

    results, err := h.orchestrator.GetHistory(context.Background(), cluster, namespace)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error":   gin.H{"message": err.Error()},
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    results,
        "count":   len(results),
    })
}
```

---

## 🎨 Implementação Frontend

### 1. HealthCheckingTab.tsx (Container Principal)

**Layout:** SplitView (esquerda: config + trigger, direita: resultados)

```typescript
export const HealthCheckingTab: React.FC = () => {
  const { cluster } = useClusterContext();
  const [config, setConfig] = useState<HealthCheckRequest>({
    cluster: cluster || "",
    namespaces: [],
    check_deployments: true,
    check_services: true,
    check_configs: true,
    timeout: 10,
  });

  const [isRunning, setIsRunning] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [results, setResults] = useState<HealthCheckResult | null>(null);

  const handleRun = async () => {
    try {
      setIsRunning(true);
      const response = await apiClient.runHealthCheck(config);
      setSessionId(response.session_id);
      toast.success("Health check iniciado");
    } catch (error) {
      toast.error("Erro ao iniciar health check");
      setIsRunning(false);
    }
  };

  return (
    <div className="flex h-full">
      {/* Painel Esquerdo: Configuração */}
      <div className="w-1/3 border-r p-4 space-y-4">
        <h2>Health Check Configuration</h2>

        {/* Cluster Selector */}
        <Select value={config.cluster} onValueChange={(v) => setConfig({...config, cluster: v})}>
          <SelectTrigger>
            <SelectValue placeholder="Selecione o cluster" />
          </SelectTrigger>
          {/* ... */}
        </Select>

        {/* Namespace Multi-Select */}
        <MultiSelect
          values={config.namespaces}
          onChange={(ns) => setConfig({...config, namespaces: ns})}
        />

        {/* Check Options */}
        <div className="space-y-2">
          <Checkbox
            checked={config.check_deployments}
            onCheckedChange={(c) => setConfig({...config, check_deployments: c as boolean})}
          />
          <Label>Verificar Deployments</Label>
        </div>

        {/* Timeout Input */}
        <Input
          type="number"
          value={config.timeout}
          onChange={(e) => setConfig({...config, timeout: parseInt(e.target.value)})}
          label="Timeout (segundos)"
        />

        {/* Run Button */}
        <Button onClick={handleRun} disabled={isRunning || !config.cluster}>
          {isRunning ? <Loader2 className="animate-spin" /> : <Play />}
          Executar Health Check
        </Button>
      </div>

      {/* Painel Direito: Resultados */}
      <div className="flex-1 p-4">
        {isRunning && sessionId && (
          <HealthCheckProgressModal sessionId={sessionId} onComplete={setResults} />
        )}

        {results && (
          <HealthCheckResultsPanel results={results} />
        )}

        {!isRunning && !results && (
          <div className="text-center text-muted-foreground">
            <Activity className="opacity-50" />
            <p>Configure e execute um health check</p>
          </div>
        )}
      </div>
    </div>
  );
};
```

### 2. HealthCheckProgressModal.tsx (SSE Progress)

```typescript
interface Props {
  sessionId: string;
  onComplete: (results: HealthCheckResult) => void;
}

export const HealthCheckProgressModal: React.FC<Props> = ({ sessionId, onComplete }) => {
  const [events, setEvents] = useState<HealthCheckProgress[]>([]);
  const [progress, setProgress] = useState(0);
  const [isComplete, setIsComplete] = useState(false);

  useEffect(() => {
    const eventSource = new EventSource(`/api/v1/healthcheck/progress?session=${sessionId}`);

    eventSource.addEventListener("progress", (e) => {
      const event: HealthCheckProgress = JSON.parse(e.data);
      setEvents(prev => [...prev, event]);
      setProgress(event.progress);
    });

    eventSource.addEventListener("complete", (e) => {
      const result: HealthCheckResult = JSON.parse(e.data);
      onComplete(result);
      setIsComplete(true);
      eventSource.close();
    });

    eventSource.onerror = () => {
      toast.error("Erro na conexão SSE");
      eventSource.close();
    };

    return () => eventSource.close();
  }, [sessionId]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Health Check em Progresso</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <Progress value={progress} />

        <ScrollArea className="h-96">
          {events.map((event, i) => (
            <div key={i} className="flex items-center gap-2 py-1">
              {event.status === "healthy" && <CheckCircle2 className="text-green-600" />}
              {event.status === "warning" && <AlertCircle className="text-yellow-600" />}
              {event.status === "critical" && <XCircle className="text-red-600" />}
              <span className="text-sm">{event.message}</span>
            </div>
          ))}
        </ScrollArea>
      </CardContent>
    </Card>
  );
};
```

### 3. HealthCheckResultsPanel.tsx (Resultados)

```typescript
interface Props {
  results: HealthCheckResult;
}

export const HealthCheckResultsPanel: React.FC<Props> = ({ results }) => {
  return (
    <div className="space-y-4">
      {/* Resumo */}
      <div className="grid grid-cols-4 gap-4">
        <StatsCard
          icon={CheckCircle2}
          value={results.healthy_count}
          label="Healthy"
          className="border-green-200"
        />
        <StatsCard
          icon={AlertCircle}
          value={results.warning_count}
          label="Warnings"
          className="border-yellow-200"
        />
        <StatsCard
          icon={XCircle}
          value={results.critical_count}
          label="Critical"
          className="border-red-200"
        />
        <StatsCard
          icon={Activity}
          value={results.total_checks}
          label="Total Checks"
        />
      </div>

      {/* Tabs: Deployments | Services | Configs */}
      <Tabs defaultValue="deployments">
        <TabsList>
          <TabsTrigger value="deployments">Deployments ({results.deployment_results.length})</TabsTrigger>
          <TabsTrigger value="services">Services ({results.service_results.length})</TabsTrigger>
          <TabsTrigger value="configs">Configs ({results.config_results.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="deployments">
          <div className="space-y-2">
            {results.deployment_results.map((d, i) => (
              <HealthCheckCard key={i} health={d} type="deployment" />
            ))}
          </div>
        </TabsContent>

        {/* Similar para services e configs */}
      </Tabs>
    </div>
  );
};
```

### 4. HealthCheckCard.tsx (Card de Resultado)

```typescript
interface Props {
  health: DeploymentHealth | ServiceHealth | ConfigHealth;
  type: "deployment" | "service" | "config";
}

export const HealthCheckCard: React.FC<Props> = ({ health, type }) => {
  const statusColors = {
    healthy: "border-green-200 bg-green-50",
    warning: "border-yellow-200 bg-yellow-50",
    critical: "border-red-200 bg-red-50",
    unknown: "border-gray-200 bg-gray-50",
  };

  return (
    <Card className={statusColors[health.status]}>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base">{health.name}</CardTitle>
          <Badge variant={health.status === "healthy" ? "default" : "destructive"}>
            {health.status.toUpperCase()}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-2">
        <p className="text-sm">{health.message}</p>

        {/* Deployment-specific info */}
        {type === "deployment" && (
          <div className="text-xs text-muted-foreground">
            Réplicas: {health.replicas_ready}/{health.replicas_desired}
          </div>
        )}

        {/* Service-specific info */}
        {type === "service" && (
          <div className="text-xs text-muted-foreground">
            Latência: {health.latency_ms}ms | Tipo: {health.service_type}
          </div>
        )}

        {/* Suggestions */}
        {health.suggestions && health.suggestions.length > 0 && (
          <Accordion type="single" collapsible>
            <AccordionItem value="suggestions">
              <AccordionTrigger className="text-sm">
                Sugestões ({health.suggestions.length})
              </AccordionTrigger>
              <AccordionContent>
                <ul className="list-disc list-inside space-y-1">
                  {health.suggestions.map((s, i) => (
                    <li key={i} className="text-xs">{s}</li>
                  ))}
                </ul>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        )}
      </CardContent>
    </Card>
  );
};
```

---

## 📝 REST API Endpoints

```
POST   /api/v1/healthcheck/run
       Body: HealthCheckRequest
       Response: { session_id: string }

GET    /api/v1/healthcheck/progress?session={id}
       SSE stream de HealthCheckProgress

GET    /api/v1/healthcheck/history?cluster=x&namespace=y
       Response: { data: HealthCheckResult[] }

GET    /api/v1/healthcheck/:id
       Response: { data: HealthCheckResult }

DELETE /api/v1/healthcheck/:id
       Response: { success: boolean }
```

---

## 🗂️ Estrutura de Banco de Dados (SQLite)

```sql
CREATE TABLE health_check_results (
    id TEXT PRIMARY KEY,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP NOT NULL,
    duration_ms INTEGER NOT NULL,

    -- Resumo
    total_checks INTEGER NOT NULL,
    healthy_count INTEGER NOT NULL,
    warning_count INTEGER NOT NULL,
    critical_count INTEGER NOT NULL,
    overall_status TEXT NOT NULL,

    -- JSON blob com resultados detalhados
    deployment_results TEXT,  -- JSON array
    service_results TEXT,     -- JSON array
    config_results TEXT,      -- JSON array

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_cluster ON health_check_results(cluster);
CREATE INDEX idx_namespace ON health_check_results(namespace);
CREATE INDEX idx_started_at ON health_check_results(started_at DESC);
```

---

## 🔄 Fluxo Completo de Execução

### Seleção de Clusters (Dois Modos)

**Modo 1: Filtro por Ambiente**
1. **Frontend:** Usuário seleciona ambiente (prod/hlg/all)
2. **Backend:** `resolveClusters()` detecta ambiente de cada cluster (padrões: *prod*, *hlg*, *staging*)
3. **Backend:** Retorna lista filtrada de clusters

**Modo 2: Seleção Manual**
1. **Frontend:** Usuário marca checkboxes de clusters específicos
2. **Backend:** Recebe array de clusters selecionados diretamente

### Execução Multi-Cluster

3. **Frontend:** Click em "Executar" → `POST /api/v1/healthcheck/run`
4. **Backend:** Cria sessão UUID, resolve clusters (environment filter ou manual)
5. **Backend:** Determina paralelismo:
   - 1 cluster → execução sequencial (sem worker pool)
   - 2+ clusters → worker pool com `min(2, min(NumCPU, total_clusters))` workers
6. **Backend:** Para cada cluster (em paralelo se 2+ clusters):
   - DeploymentChecker valida deployments
   - ServiceChecker testa conectividade de serviços
   - ConfigChecker valida ConfigMaps/Secrets
7. **Backend:** Publica progresso via SSE com identificação do cluster (`[cluster-name] Verificando deployments...`)
8. **Frontend:** Abre EventSource, escuta eventos SSE por cluster
9. **Frontend:** Atualiza Progress bar + Event log em tempo real (agrupa por cluster)
10. **Backend:** Calcula resumo por cluster, salva no SQLite
11. **Backend:** Publica evento "complete" com resultados agregados
12. **Frontend:** Exibe resultados em HealthCheckResultsPanel (tabs por cluster)
13. **Frontend:** Permite export JSON/CSV, filtrar por status e cluster

---

## 📦 Dependências Go Necessárias

```bash
go get go.mongodb.org/mongo-driver/mongo
go get github.com/go-redis/redis/v8
go get github.com/jackc/pgx/v5
go get github.com/segmentio/kafka-go
go get github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs
go get github.com/streadway/amqp
```

---

## ✅ Checklist de Implementação

### Backend (5 dias)

**Dia 1: Estrutura + Deployment Checker** ✅ **COMPLETO** (26/12/2025)
- [x] Criar `internal/healthcheck/models.go` (153 linhas - com Environment e Clusters array)
- [x] Criar `internal/healthcheck/orchestrator.go` (466 linhas - worker pool multi-cluster)
- [x] Criar `internal/healthcheck/deployment_checker.go` (149 linhas)
- [x] Criar `internal/healthcheck/service_checker.go` (stub - 21 linhas)
- [x] Criar `internal/healthcheck/config_checker.go` (stub - 18 linhas)
- [x] Criar `internal/healthcheck/storage.go` (251 linhas - SQLite)
- [x] Criar `internal/healthcheck/deployment_checker_test.go` (192 linhas - 7 testes passando)
- [x] Atualizar dependências (k8s.io/client-go v0.35.0, fake client)
- [x] Commit: "feat: adiciona health check de clusters com filtros de ambiente" (commit 7db7daf)

**Dia 2: Service Checkers (Analyzers)** 🔄 **EM ANDAMENTO**
- [ ] Implementar `service_checker.go:CheckAll()` - detectar connection strings em ConfigMaps/Secrets
- [ ] Criar `internal/healthcheck/analyzers/mongodb.go` (ping + serverStatus)
- [ ] Criar `internal/healthcheck/analyzers/redis.go` (PING + INFO command)
- [ ] Criar `internal/healthcheck/analyzers/postgres.go` (connection test + version)
- [ ] Criar `internal/healthcheck/analyzers/kafka.go` (broker connectivity)
- [ ] Criar `internal/healthcheck/analyzers/eventhub.go` (Azure EventHub connection)
- [ ] Criar `internal/healthcheck/analyzers/http.go` (HTTP GET + status code check)
- [ ] Testes unitários service checkers (mocks de serviços externos)
- [ ] Commit: "feat: adiciona health check de serviços externos"

**Dia 3: Config Checker + REST API**
- [ ] Implementar `config_checker.go:CheckAll()` - validação de ConfigMaps/Secrets
- [ ] Criar `internal/web/handlers/healthcheck.go` (6 endpoints REST)
- [ ] Registrar rotas em `internal/web/routes.go` ou `server.go`
- [ ] Integração com `ProgressTracker` (SSE já reutilizado no orchestrator)
- [ ] Testes integração API (health check end-to-end)
- [ ] Commit: "feat: adiciona API REST de health checking"

### Frontend (1-2 dias)

**Dia 4: Componentes React**
- [ ] Criar `HealthCheckingTab.tsx`
- [ ] Criar `HealthCheckProgressModal.tsx`
- [ ] Criar `HealthCheckResultsPanel.tsx`
- [ ] Criar `HealthCheckCard.tsx`
- [ ] Criar `HealthCheckConfigDialog.tsx`
- [ ] Hook `useHealthChecking.ts`
- [ ] Hook `useHealthCheckProgress.ts`
- [ ] Tipos TypeScript em `types/healthcheck.ts`
- [ ] Commit: "feat: adiciona interface de health checking"

**Dia 5: Refinamentos + Export + AI Integration**
- [ ] Adicionar filtros por status
- [ ] Export JSON/CSV/PDF
- [ ] Botão "Analisar com AI" em items críticos
- [ ] Histórico de health checks
- [ ] Testes end-to-end
- [ ] Commit: "feat: adiciona histórico, export e integração AI"

---

## 🎨 UI/UX Highlights

### Estados Visuais

**Idle State:**
```
┌─────────────────────────────────────┐
│ Configure e execute um health check │
│         [Activity Icon]              │
└─────────────────────────────────────┘
```

**Running State (Cluster Único):**
```
┌─────────────────────────────────────┐
│ Health Check em Progresso           │
│ Cluster: akspriv-prod               │
│ [===========>          ] 45%        │
│                                     │
│ ✅ Deployments verificados          │
│ ⏳ Testando serviços externos...    │
│ ⏸️  Aguardando validação configs    │
└─────────────────────────────────────┘
```

**Running State (Multi-Cluster - 3 workers):**
```
┌─────────────────────────────────────┐
│ Health Check Multi-Cluster          │
│ Ambiente: Produção (5 clusters)     │
│ [============>         ] 60%        │
│                                     │
│ 🟢 [akspriv-prod]                   │
│    ✅ Deployments OK | ✅ Services OK│
│                                     │
│ 🟡 [akspriv-prod-2]                 │
│    ⏳ Testando serviços externos... │
│                                     │
│ 🔵 [akspriv-prod-3]                 │
│    ⏳ Verificando deployments...    │
│                                     │
│ ⏸️  [akspriv-prod-4] - Na fila      │
│ ⏸️  [akspriv-prod-5] - Na fila      │
└─────────────────────────────────────┘
```

**Results State (Cluster Único):**
```
┌─────────────────────────────────────┐
│ Health Check: akspriv-prod          │
│ [✅ 42 Healthy] [⚠️ 5 Warning]      │
│ [❌ 2 Critical] [📊 49 Total]       │
│                                     │
│ Tabs: Deployments | Services | Cfg │
│                                     │
│ 🟢 api-gateway   [HEALTHY]          │
│    Réplicas: 3/3                    │
│                                     │
│ 🟡 auth-service  [WARNING]          │
│    Réplicas: 2/3                    │
│    > Sugestões (3)                  │
│                                     │
│ 🔴 payment-svc   [CRITICAL]         │
│    MongoDB connection failed        │
│    > Sugestões (5)                  │
└─────────────────────────────────────┘
```

**Results State (Multi-Cluster - Agregado):**
```
┌─────────────────────────────────────┐
│ Health Check Multi-Cluster          │
│ Ambiente: Produção (5 clusters)     │
│ [✅ 187 Healthy] [⚠️ 23 Warning]    │
│ [❌ 8 Critical] [📊 218 Total]      │
│                                     │
│ Filtros: [Cluster ▼] [Status ▼]    │
│                                     │
│ Tabs: Visão Geral | Por Cluster    │
│                                     │
│ ┌─ akspriv-prod (49 checks) ──────┐│
│ │ ✅ 42 | ⚠️ 5 | ❌ 2              ││
│ └───────────────────────────────────┘│
│ ┌─ akspriv-prod-2 (43 checks) ────┐│
│ │ ✅ 38 | ⚠️ 4 | ❌ 1              ││
│ └───────────────────────────────────┘│
│ ┌─ akspriv-prod-3 (44 checks) ────┐│
│ │ ✅ 40 | ⚠️ 4 | ❌ 0              ││
│ └───────────────────────────────────┘│
│                                     │
│ Click em um cluster para ver        │
│ detalhes (Deployments/Services/Cfg) │
└─────────────────────────────────────┘
```

---

## 📚 Referências no Código Existente

**Reutilizar padrões de:**
- `internal/collectors/` - Coleta de contexto diagnóstico
- `internal/web/handlers/ai_diagnostics.go` - SSE progress tracking
- `internal/storage/ai_history_store.go` - Persistência SQLite
- `SequenceProgressModal.tsx` - Modal de progresso SSE
- `ConfigMapsTab.tsx` - Layout SplitView
- `ApplyAllModal.tsx` - Pré-visualização de mudanças

---

## ✅ Decisões do Usuário

1. **Escopo:** Implementação completa (deployments + serviços + configs)
2. **Execução:** Apenas on-demand (usuário clica "Executar")
3. **Integração AI:** Manual (botão "Analisar com AI" em cada item crítico)
4. **Export:** CSV, JSON, PDF

---

## 📤 Export de Resultados (CSV, JSON, PDF)

### CSV Export

```go
func (h *HealthCheckHandler) ExportCSV(c *gin.Context) {
    resultID := c.Param("id")
    result, err := h.orchestrator.GetResult(ctx, resultID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    var buf bytes.Buffer
    writer := csv.NewWriter(&buf)

    // Header
    writer.Write([]string{"Type", "Name", "Namespace", "Status", "Message", "Suggestions"})

    // Deployments
    for _, d := range result.DeploymentResults {
        writer.Write([]string{
            "Deployment",
            d.Name,
            d.Namespace,
            string(d.Status),
            d.Message,
            strings.Join(d.Suggestions, "; "),
        })
    }

    // Services
    for _, s := range result.ServiceResults {
        writer.Write([]string{
            string(s.ServiceType),
            s.Name,
            s.Namespace,
            string(s.Status),
            s.Message,
            strings.Join(s.Suggestions, "; "),
        })
    }

    writer.Flush()

    c.Header("Content-Type", "text/csv")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=health-check-%s.csv", resultID))
    c.Data(200, "text/csv", buf.Bytes())
}
```

### JSON Export

```go
func (h *HealthCheckHandler) ExportJSON(c *gin.Context) {
    resultID := c.Param("id")
    result, err := h.orchestrator.GetResult(ctx, resultID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=health-check-%s.json", resultID))
    c.JSON(200, result)
}
```

### PDF Export

```go
import "github.com/jung-kurt/gofpdf"

func (h *HealthCheckHandler) ExportPDF(c *gin.Context) {
    resultID := c.Param("id")
    result, err := h.orchestrator.GetResult(ctx, resultID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    pdf := gofpdf.New("P", "mm", "A4", "")
    pdf.AddPage()
    pdf.SetFont("Arial", "B", 16)

    // Título
    pdf.Cell(40, 10, fmt.Sprintf("Health Check Report - %s", result.Cluster))
    pdf.Ln(12)

    // Resumo
    pdf.SetFont("Arial", "", 12)
    pdf.Cell(40, 10, fmt.Sprintf("Status Geral: %s", result.OverallStatus))
    pdf.Ln(8)
    pdf.Cell(40, 10, fmt.Sprintf("Total Checks: %d", result.TotalChecks))
    pdf.Ln(8)
    pdf.Cell(40, 10, fmt.Sprintf("Healthy: %d | Warning: %d | Critical: %d",
        result.HealthyCount, result.WarningCount, result.CriticalCount))
    pdf.Ln(12)

    // Tabela de resultados
    pdf.SetFont("Arial", "B", 10)
    pdf.Cell(50, 7, "Recurso")
    pdf.Cell(30, 7, "Status")
    pdf.Cell(100, 7, "Mensagem")
    pdf.Ln(8)

    pdf.SetFont("Arial", "", 9)
    for _, d := range result.DeploymentResults {
        pdf.Cell(50, 6, d.Name)
        pdf.Cell(30, 6, string(d.Status))
        pdf.Cell(100, 6, d.Message)
        pdf.Ln(6)
    }

    var buf bytes.Buffer
    err = pdf.Output(&buf)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.Header("Content-Type", "application/pdf")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=health-check-%s.pdf", resultID))
    c.Data(200, "application/pdf", buf.Bytes())
}
```

### Frontend Export Buttons

```typescript
<DropdownMenu>
  <DropdownMenuTrigger asChild>
    <Button variant="outline">
      <Download className="mr-2 h-4 w-4" />
      Exportar
    </Button>
  </DropdownMenuTrigger>
  <DropdownMenuContent>
    <DropdownMenuItem onClick={() => handleExport("json")}>
      <FileJson className="mr-2 h-4 w-4" />
      JSON
    </DropdownMenuItem>
    <DropdownMenuItem onClick={() => handleExport("csv")}>
      <FileSpreadsheet className="mr-2 h-4 w-4" />
      CSV
    </DropdownMenuItem>
    <DropdownMenuItem onClick={() => handleExport("pdf")}>
      <FileText className="mr-2 h-4 w-4" />
      PDF
    </DropdownMenuItem>
  </DropdownMenuContent>
</DropdownMenu>

const handleExport = async (format: "json" | "csv" | "pdf") => {
  const url = `/api/v1/healthcheck/${results.id}/export/${format}`;
  const response = await fetch(url);
  const blob = await response.blob();

  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = `health-check-${results.id}.${format}`;
  link.click();

  toast.success(`Exportado com sucesso (${format.toUpperCase()})`);
};
```

---

## 🤖 Integração Manual com AI Diagnostics

### Backend: Endpoint de Análise Individual

```go
// POST /api/v1/healthcheck/:id/analyze/:type/:name
func (h *HealthCheckHandler) AnalyzeItem(c *gin.Context) {
    resultID := c.Param("id")
    itemType := c.Param("type")   // "deployment" | "service" | "config"
    itemName := c.Param("name")

    // Buscar resultado
    result, err := h.orchestrator.GetResult(ctx, resultID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // Encontrar item específico
    var item interface{}
    switch itemType {
    case "deployment":
        for _, d := range result.DeploymentResults {
            if d.Name == itemName {
                item = d
                break
            }
        }
    case "service":
        for _, s := range result.ServiceResults {
            if s.Name == itemName {
                item = s
                break
            }
        }
    }

    if item == nil {
        c.JSON(404, gin.H{"error": "Item not found"})
        return
    }

    // Construir contexto para AI
    contextReq := collectors.ContextRequest{
        ResourceType:    itemType,
        Cluster:         result.Cluster,
        Namespace:       result.Namespace,
        ResourceName:    itemName,
        IncludeLogs:     true,
        IncludeDescribe: true,
    }

    diagCtx, err := h.contextBuilder.BuildContext(ctx, contextReq)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // Executar análise AI
    analysis, err := h.aiAnalyzer.Analyze(ctx, diagCtx)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{
        "success":  true,
        "analysis": analysis,
    })
}
```

### Frontend: Botão "Analisar com AI" em Items Críticos

```typescript
// HealthCheckCard.tsx
export const HealthCheckCard: React.FC<Props> = ({ health, type, resultId }) => {
  const [analyzing, setAnalyzing] = useState(false);
  const [analysis, setAnalysis] = useState<AIAnalysis | null>(null);

  const handleAnalyze = async () => {
    setAnalyzing(true);
    try {
      const response = await apiClient.analyzeHealthCheckItem(resultId, type, health.name);
      setAnalysis(response.analysis);
      toast.success("Análise de AI concluída");
    } catch (error) {
      toast.error("Erro ao analisar com AI");
    } finally {
      setAnalyzing(false);
    }
  };

  return (
    <Card className={statusColors[health.status]}>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>{health.name}</CardTitle>
          <div className="flex items-center gap-2">
            <Badge>{health.status}</Badge>

            {/* Mostrar botão AI apenas para status critical/warning */}
            {(health.status === "critical" || health.status === "warning") && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleAnalyze}
                disabled={analyzing}
              >
                {analyzing ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Brain className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>
        </div>
      </CardHeader>

      <CardContent>
        {/* Mensagem e sugestões */}
        <p>{health.message}</p>

        {/* Análise AI (se disponível) */}
        {analysis && (
          <Alert className="mt-4">
            <Brain className="h-4 w-4" />
            <AlertTitle>Análise de IA</AlertTitle>
            <AlertDescription>
              <ReactMarkdown>{analysis.analysis}</ReactMarkdown>
            </AlertDescription>
          </Alert>
        )}

        {/* Sugestões originais */}
        {health.suggestions && health.suggestions.length > 0 && (
          <Accordion type="single" collapsible>
            <AccordionItem value="suggestions">
              <AccordionTrigger>Sugestões ({health.suggestions.length})</AccordionTrigger>
              <AccordionContent>
                <ul>
                  {health.suggestions.map((s, i) => <li key={i}>{s}</li>)}
                </ul>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        )}
      </CardContent>
    </Card>
  );
};
```

---

## 🚀 Próximos Passos

Após aprovação deste plano:

1. **Fase 1:** Backend (Dias 1-3)
   - Implementar estrutura base + deployment checker
   - Adicionar service checkers (MongoDB, Redis, Kafka, PostgreSQL, EventHub, HTTP)
   - Criar API REST + SSE

2. **Fase 2:** Frontend (Dias 4-5)
   - Implementar componentes React
   - Integrar com API backend
   - Export CSV/JSON/PDF
   - Botão "Analisar com AI" em items críticos
   - Testes end-to-end

---

**Estimativa Total:** 3-4 dias de desenvolvimento
**Arquivos a Criar:** ~25 arquivos (15 backend Go, 10 frontend React/TS)
**Linhas de Código:** ~5.000 linhas totais

Este plano fornece uma base sólida e escalável para health checking completo de clusters AKS, com interface pragmática e sugestões acionáveis.
