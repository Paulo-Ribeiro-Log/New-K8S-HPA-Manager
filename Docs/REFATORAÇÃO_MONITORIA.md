# 🔄 Refatoração do Sistema de Monitoramento

**Data:** 15 de novembro de 2025
**Objetivo:** Migrar de port-forwards + baseline SQLite → Endpoint Prometheus público
**Redução estimada:** 850 linhas → ~300 linhas (~60% menos código)

---

## ✅ RESULTADOS DOS TESTES - Endpoint Prometheus

**Endpoint testado:** `https://prometheus-akspriv-logreversa-prd.viavarejo.com.br/`

### **DESCOBERTA CRÍTICA: SEM AUTENTICAÇÃO NECESSÁRIA! 🎉**

Ao contrário da suposição inicial, o endpoint **NÃO requer Azure AD authentication**. O endpoint é totalmente aberto e acessível sem tokens.

**Implicações:**
- ✅ Implementação **muito mais simples** (sem Azure AD token handling)
- ✅ Apenas precisa lidar com certificado SSL auto-assinado (`InsecureSkipVerify: true`)
- ✅ Redução adicional de código (elimina todo gerenciamento de tokens)

### **Métricas Coletadas com Sucesso:**

#### **1. Métricas básicas de HPA (namespace: ingress-nginx, HPA: nginx-ingress-controller):**
```bash
✅ Current Replicas:  3
✅ Min Replicas:      3
✅ Max Replicas:      20
✅ Desired Replicas:  3
✅ Target CPU:        60%
```

#### **2. Métricas de uso atual:**
```bash
✅ CPU Usage:     1.42% (vs Request)
✅ Memory Usage:  114.03% (vs Request)
```

#### **3. Dados históricos (query_range - última hora):**
```bash
✅ 61 data points coletados (step: 1 minuto)
✅ Endpoint /api/v1/query_range funcional
```

### **Queries PromQL Testadas:**

```promql
# Réplicas atuais
kube_horizontalpodautoscaler_status_current_replicas{namespace="ingress-nginx",horizontalpodautoscaler="nginx-ingress-controller"}

# Min/Max replicas
kube_horizontalpodautoscaler_spec_min_replicas{...}
kube_horizontalpodautoscaler_spec_max_replicas{...}

# Target CPU
kube_horizontalpodautoscaler_spec_target_metric{metric_name="cpu"}

# CPU usage % (vs Request)
sum(rate(container_cpu_usage_seconds_total{namespace="ingress-nginx",pod=~"nginx-ingress-controller.*"}[1m])) /
sum(kube_pod_container_resource_requests{namespace="ingress-nginx",pod=~"nginx-ingress-controller.*",resource="cpu"}) * 100

# Memory usage % (vs Request)
sum(container_memory_working_set_bytes{namespace="ingress-nginx",pod=~"nginx-ingress-controller.*"}) /
sum(kube_pod_container_resource_requests{namespace="ingress-nginx",pod=~"nginx-ingress-controller.*",resource="memory"}) * 100
```

### **Conclusões dos Testes:**

| Item | Status | Observação |
|------|--------|------------|
| **Autenticação** | ❌ Não necessária | Endpoint completamente aberto |
| **SSL Certificate** | ⚠️ Auto-assinado | Requer `InsecureSkipVerify: true` |
| **Métricas HPA** | ✅ Completas | Min, Max, Current, Desired, Target |
| **Métricas de uso** | ✅ Disponíveis | CPU e Memory % vs Request |
| **Dados históricos** | ✅ Funcionais | query_range retorna dados corretamente |
| **Escalabilidade** | ✅ Ilimitada | Sem limitação de clusters simultâneos |

---

## 📋 Índice

1. [Motivação](#-motivação)
2. [Comparação: Antes vs Depois](#-comparação-antes-vs-depois)
3. [Descoberta do Endpoint Prometheus](#-descoberta-do-endpoint-prometheus)
4. [Arquitetura Nova](#-arquitetura-nova)
5. [Plano de Implementação (6 Fases)](#-plano-de-implementação-6-fases)
6. [Arquivos que serão DELETADOS](#-arquivos-que-serão-deletados)
7. [Arquivos que serão CRIADOS](#-arquivos-que-serão-criados)
8. [Arquivos que serão MODIFICADOS](#-arquivos-que-serão-modificados)
9. [Testes de Validação](#-testes-de-validação)
10. [Rollback Plan](#-rollback-plan)

---

## 🎯 Motivação

### **Problema Atual**

Sistema de monitoramento usa port-forwards temporários para cada cluster, com baseline obrigatório de 3 dias (4320 pontos) coletado antecipadamente:

**Limitações:**
- ❌ Escalabilidade limitada (2 clusters simultâneos - portas 55555/55556)
- ❌ Setup demorado (3-5 minutos para baseline de 3 dias)
- ❌ Dependência de kubectl + VPN
- ❌ Port-forwards criados/destruídos constantemente
- ❌ Sistema de slots temporais complexo
- ❌ Baseline SQLite persistente (4320 registros por HPA)
- ❌ ~850 linhas de código complexo

**Arquivos envolvidos (legado):**
```
internal/monitoring/
├── portforward/
│   └── portforward.go           # Port-forward manager (2 portas)
├── timeslot/
│   └── timeslot.go              # Sistema de rotação de slots
├── baseline/
│   ├── queue.go                 # Fila de baseline
│   ├── worker.go                # Workers de baseline
│   └── scheduler.go             # Scheduler de rescan
├── collector/
│   └── rotating.go              # Collector com rotação de portas
└── storage/
    └── persistence.go           # SQLite para baseline + snapshots
```

### **Solução Proposta**

Prometheus expõe endpoint público **SEM AUTENTICAÇÃO** (descoberta em 15/nov/2025):

**URL Pattern:**
```
https://prometheus-<cluster>-<ambiente>.viavarejo.com.br/
```

**⚠️ IMPORTANTE:** Endpoint é **completamente aberto** - não requer Azure AD tokens ou qualquer autenticação. Apenas SSL auto-assinado (requer `InsecureSkipVerify: true`).

**Exemplos:**
```
https://prometheus-faturamento-hlg.viavarejo.com.br/
https://prometheus-checkout-prod.viavarejo.com.br/
https://prometheus-pagamento-dev.viavarejo.com.br/
```

**Benefícios:**
- ✅ Escalabilidade ilimitada (N clusters simultâneos)
- ✅ Setup instantâneo (0 segundos)
- ✅ Sem dependência de kubectl/VPN
- ✅ Queries sob demanda (6h, 1d, 3d, 7d, 30d)
- ✅ Cache em memória (não SQLite)
- ✅ **SEM autenticação** (endpoint aberto - descoberto em 15/nov/2025)
- ✅ ~250 linhas de código (~70% menos - sem código de auth)

---

## 📊 Comparação: Antes vs Depois

| Aspecto | ANTES (Port-Forward) | DEPOIS (Endpoint Público) |
|---------|---------------------|---------------------------|
| **Setup inicial** | 3-5 minutos (baseline 3 dias) | 0 segundos (instantâneo) |
| **Escalabilidade** | 2 clusters simultâneos | Ilimitado (N clusters) |
| **Dependências** | kubectl, VPN, port-forward | **Nenhuma** (HTTP/HTTPS simples) |
| **Resiliência** | Falha se VPN cair | Resiliente (endpoint público) |
| **Histórico** | Fixo (3 dias no SQLite) | Flexível (1h-30d sob demanda) |
| **Performance** | Scan a cada 30s com port-forward | Query instantânea HTTP/HTTPS |
| **Complexidade** | ~850 linhas de código | **~250 linhas** (70% menos) |
| **Persistência** | SQLite (4320 registros/HPA) | Cache em memória (TTL 1h) |
| **Manutenção** | Alta (slots, portas, baseline) | Baixa (HTTP simples) |
| **Autenticação** | Nenhuma (localhost) | **Nenhuma** (endpoint aberto) ⚠️ |
| **SSL Certificate** | N/A | Auto-assinado (InsecureSkipVerify) |

---

## 🔍 Descoberta do Endpoint Prometheus

### **Pattern de URL**

```
https://prometheus-<nome-cluster>-<ambiente>.viavarejo.com.br/
```

**Mapeamento cluster → URL:**

| Cluster Kubernetes | Ambiente | URL Prometheus |
|-------------------|----------|----------------|
| `akspriv-faturamento-hlg-admin` | `hlg` | `https://prometheus-faturamento-hlg.viavarejo.com.br/` |
| `akspriv-checkout-prod-admin` | `prod` | `https://prometheus-checkout-prod.viavarejo.com.br/` |
| `akspriv-pagamento-dev-admin` | `dev` | `https://prometheus-pagamento-dev.viavarejo.com.br/` |

**Extração automática:**
```go
// Entrada: "akspriv-faturamento-hlg-admin"
// Saída:
//   Nome: "faturamento"
//   Ambiente: "hlg"
//   URL: "https://prometheus-faturamento-hlg.viavarejo.com.br/"

func parseClusterName(cluster string) (nome, ambiente string) {
    // Remove prefixo "akspriv-" e sufixo "-admin"
    clean := strings.TrimPrefix(cluster, "akspriv-")
    clean = strings.TrimSuffix(clean, "-admin")

    // Split por "-" e pega última parte como ambiente
    parts := strings.Split(clean, "-")
    ambiente = parts[len(parts)-1] // "hlg", "dev", "prod"
    nome = strings.Join(parts[:len(parts)-1], "-") // "faturamento", "checkout"

    return nome, ambiente
}
```

### **Autenticação Azure AD**

O mesmo token usado para autenticação AKS funciona para Prometheus (SSO):

```go
// Token já existe no sistema atual:
// internal/azure/auth.go → GetAzureCredential()

// Reutilizar token:
credential, _ := azidentity.NewDefaultAzureCredential(nil)
token, _ := credential.GetToken(ctx, policy.TokenRequestOptions{
    Scopes: []string{"https://management.azure.com/.default"},
})

// Usar em requests HTTP:
req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.Token))
```

---

## 🏗️ Arquitetura Nova

### **Componentes Principais**

```
internal/monitoring/
├── discovery/
│   └── prometheus.go          # Detecção automática de endpoint
├── client/
│   ├── prometheus_client.go   # Cliente HTTP com Azure AD
│   └── query_builder.go       # Builder de queries PromQL
├── cache/
│   └── memory_cache.go        # Cache em memória (TTL 1h)
└── engine/
    └── engine.go              # Engine simplificado (sem slots/baseline)
```

### **Fluxo de Dados**

```
┌─────────────────────────────────────────────────────────┐
│ NOVO FLUXO (Endpoint Público):                          │
├─────────────────────────────────────────────────────────┤
│ 1. Usuário clica "Monitorar HPA"                        │
│    ↓                                                     │
│ 2. Sistema detecta URL do Prometheus                    │
│    • parseClusterName("akspriv-faturamento-hlg-admin")  │
│    • Gera: https://prometheus-faturamento-hlg.viavarejo.com.br/│
│    ↓                                                     │
│ 3. Valida endpoint (GET /api/v1/status/config)          │
│    • Headers: Authorization: Bearer <azure-ad-token>    │
│    • Se OK: Endpoint público disponível                 │
│    • Se FAIL: Fallback para port-forward (legado)       │
│    ↓                                                     │
│ 4. Usuário seleciona período (UI):                      │
│    • "Últimas 6 horas" → range: 6h                      │
│    • "Últimas 24 horas" → range: 1d                     │
│    • "Últimos 3 dias" → range: 3d                       │
│    ↓                                                     │
│ 5. Backend faz query_range SOB DEMANDA:                 │
│    GET /api/v1/query_range?query=...&start=...&end=...  │
│    • Não coleta antecipadamente                         │
│    • Retorna apenas dados do período selecionado        │
│    ↓                                                     │
│ 6. Cache em memória (opcional):                         │
│    • TTL: 1 hora                                        │
│    • Evita re-query se período já foi consultado        │
│    ↓                                                     │
│ 7. Frontend exibe gráficos                              │
│    • Recharts com dados do período                      │
│    • Auto-refresh a cada 30s (query instantânea)        │
└─────────────────────────────────────────────────────────┘
```

---

## 🛠️ Plano de Implementação (6 Fases)

### **Fase 1: Discovery + Validação** ✅

**Objetivo:** Detectar automaticamente endpoint Prometheus público e validar conectividade.

**Arquivos CRIADOS:**
```
internal/monitoring/discovery/
└── prometheus.go              # NOVO - Detecção de endpoint
```

**Código:**
```go
// internal/monitoring/discovery/prometheus.go
package discovery

type PrometheusEndpoint struct {
    Cluster     string // "akspriv-faturamento-hlg-admin"
    Name        string // "faturamento"
    Environment string // "hlg", "dev", "prod"
    URL         string // "https://prometheus-faturamento-hlg.viavarejo.com.br/"
    AuthType    string // "azure-ad" ou "port-forward"
    Available   bool   // Endpoint acessível?
}

// DiscoverEndpoint detecta automaticamente endpoint do Prometheus
func DiscoverEndpoint(cluster string, azureToken string) (*PrometheusEndpoint, error) {
    name, env := parseClusterName(cluster)

    endpoint := &PrometheusEndpoint{
        Cluster:     cluster,
        Name:        name,
        Environment: env,
        URL:         buildPrometheusURL(name, env),
        AuthType:    "azure-ad",
    }

    // Validar se endpoint está acessível
    if err := validateEndpoint(endpoint, azureToken); err != nil {
        log.Warn().Err(err).Msg("Endpoint público não acessível, usando port-forward")
        return createPortForwardEndpoint(cluster), nil
    }

    endpoint.Available = true
    return endpoint, nil
}

func parseClusterName(cluster string) (name, env string) {
    // "akspriv-faturamento-hlg-admin" → ("faturamento", "hlg")
    clean := strings.TrimPrefix(cluster, "akspriv-")
    clean = strings.TrimSuffix(clean, "-admin")

    parts := strings.Split(clean, "-")
    env = parts[len(parts)-1]
    name = strings.Join(parts[:len(parts)-1], "-")

    return name, env
}

func buildPrometheusURL(name, env string) string {
    return fmt.Sprintf("https://prometheus-%s-%s.viavarejo.com.br", name, env)
}

func validateEndpoint(endpoint *PrometheusEndpoint, token string) error {
    // GET /api/v1/status/config
    url := fmt.Sprintf("%s/api/v1/status/config", endpoint.URL)

    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
    req.Header.Set("Accept", "application/json")

    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("endpoint não acessível: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return fmt.Errorf("endpoint retornou status %d", resp.StatusCode)
    }

    return nil
}
```

**Testes:**
```bash
go test ./internal/monitoring/discovery -v
```

**Critérios de sucesso:**
- ✅ Parse correto de nomes de clusters (70+ clusters)
- ✅ URL gerada corretamente para todos os ambientes (dev/hlg/prod)
- ✅ Validação de endpoint funciona com token Azure AD
- ✅ Fallback para port-forward quando endpoint não disponível

---

### **Fase 2: Cliente Prometheus com Azure AD** ✅

**Objetivo:** Cliente HTTP para Prometheus com autenticação Azure AD.

**Arquivos CRIADOS:**
```
internal/monitoring/client/
├── prometheus_client.go       # NOVO - Cliente HTTP
└── query_builder.go           # NOVO - Builder de queries PromQL
```

**Código:**
```go
// internal/monitoring/client/prometheus_client.go
package client

type PrometheusClient struct {
    endpoint   *discovery.PrometheusEndpoint
    httpClient *http.Client
    azureToken string
}

func NewPrometheusClient(endpoint *discovery.PrometheusEndpoint, azureToken string) *PrometheusClient {
    return &PrometheusClient{
        endpoint:   endpoint,
        azureToken: azureToken,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
            Transport: &http.Transport{
                TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
            },
        },
    }
}

// QueryRange executa query temporal (range)
func (c *PrometheusClient) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryResult, error) {
    url := fmt.Sprintf("%s/api/v1/query_range", c.endpoint.URL)

    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.azureToken))
    req.Header.Set("Accept", "application/json")

    q := req.URL.Query()
    q.Add("query", query)
    q.Add("start", fmt.Sprintf("%d", start.Unix()))
    q.Add("end", fmt.Sprintf("%d", end.Unix()))
    q.Add("step", step.String())
    req.URL.RawQuery = q.Encode()

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("query falhou: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("prometheus retornou %d: %s", resp.StatusCode, body)
    }

    var result QueryResult
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("falha ao decodificar resposta: %w", err)
    }

    return &result, nil
}

// Query executa query instantânea (estado atual)
func (c *PrometheusClient) Query(ctx context.Context, query string) (*QueryResult, error) {
    url := fmt.Sprintf("%s/api/v1/query", c.endpoint.URL)

    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.azureToken))
    req.Header.Set("Accept", "application/json")

    q := req.URL.Query()
    q.Add("query", query)
    req.URL.RawQuery = q.Encode()

    resp, err := c.httpClient.Do(req)
    // ... similar ao QueryRange
}
```

**Query Builder:**
```go
// internal/monitoring/client/query_builder.go
package client

func BuildReplicasQuery(namespace, hpaName string) string {
    return fmt.Sprintf(
        `kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
        namespace, hpaName,
    )
}

func BuildCPUQuery(namespace, deployment string) string {
    return fmt.Sprintf(
        `sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`,
        namespace, deployment, namespace, deployment,
    )
}

func BuildMemoryQuery(namespace, deployment string) string {
    return fmt.Sprintf(
        `sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`,
        namespace, deployment, namespace, deployment,
    )
}
```

**Testes:**
```bash
go test ./internal/monitoring/client -v
```

**Critérios de sucesso:**
- ✅ Query range retorna dados corretos (verificar timestamps)
- ✅ Query instantânea retorna estado atual
- ✅ Autenticação Azure AD funciona (token válido)
- ✅ Timeout de 30s funciona corretamente
- ✅ Erros HTTP são tratados adequadamente

---

### **Fase 3: Cache em Memória** ✅

**Objetivo:** Cache em memória para evitar re-queries desnecessárias.

**Arquivos CRIADOS:**
```
internal/monitoring/cache/
└── memory_cache.go            # NOVO - Cache em memória
```

**Código:**
```go
// internal/monitoring/cache/memory_cache.go
package cache

type MemoryCache struct {
    data map[string]*CacheEntry
    mu   sync.RWMutex
    ttl  time.Duration
}

type CacheEntry struct {
    Data      interface{}
    ExpiresAt time.Time
}

func NewMemoryCache(ttl time.Duration) *MemoryCache {
    cache := &MemoryCache{
        data: make(map[string]*CacheEntry),
        ttl:  ttl,
    }

    // Cleanup routine (remove entradas expiradas a cada 5 minutos)
    go cache.cleanupLoop()

    return cache
}

func (c *MemoryCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    entry, exists := c.data[key]
    if !exists {
        return nil, false
    }

    if time.Now().After(entry.ExpiresAt) {
        return nil, false // Expirado
    }

    return entry.Data, true
}

func (c *MemoryCache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.data[key] = &CacheEntry{
        Data:      value,
        ExpiresAt: time.Now().Add(c.ttl),
    }
}

func (c *MemoryCache) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        c.cleanup()
    }
}

func (c *MemoryCache) cleanup() {
    c.mu.Lock()
    defer c.mu.Unlock()

    now := time.Now()
    for key, entry := range c.data {
        if now.After(entry.ExpiresAt) {
            delete(c.data, key)
        }
    }
}

// Gerar chave de cache
func CacheKey(cluster, namespace, hpaName, period string) string {
    return fmt.Sprintf("%s/%s/%s/%s", cluster, namespace, hpaName, period)
}
```

**Testes:**
```bash
go test ./internal/monitoring/cache -v
```

**Critérios de sucesso:**
- ✅ Cache armazena e recupera dados corretamente
- ✅ TTL funciona (dados expiram após 1 hora)
- ✅ Cleanup remove entradas expiradas
- ✅ Thread-safe (RWMutex)

---

### **Fase 4: Refatoração do Engine** ✅

**Objetivo:** Simplificar engine removendo slots, baseline e port-forwards.

**Arquivos MODIFICADOS:**
```
internal/monitoring/engine/
└── engine.go                  # MODIFICADO - Simplificado
```

**Código (ANTES - 850 linhas):**
```go
// COMPLEXO: Slots, port-forwards, baseline, workers
type ScanEngine struct {
    portForwardManager *portforward.PortForwardManager
    timeSlotManager    *timeslot.TimeSlotManager
    baselineQueue      *baseline.BaselineQueue
    baselineWorker1    *baseline.BaselineWorker
    baselineWorker2    *baseline.BaselineWorker
    baselineScheduler  *baseline.BaselineScheduler
    persistence        *storage.Persistence // SQLite
    // ... muitos outros campos
}
```

**Código (DEPOIS - ~300 linhas):**
```go
// SIMPLES: Apenas discovery + client + cache
type MonitoringEngine struct {
    discovery *discovery.PrometheusDiscovery
    cache     *cache.MemoryCache
    clients   map[string]*client.PrometheusClient // cluster → client
    mu        sync.RWMutex
}

func NewMonitoringEngine() *MonitoringEngine {
    return &MonitoringEngine{
        discovery: discovery.New(),
        cache:     cache.NewMemoryCache(1 * time.Hour),
        clients:   make(map[string]*client.PrometheusClient),
    }
}

func (e *MonitoringEngine) AddHPA(cluster, namespace, hpaName string) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    // Descobrir endpoint (ou usar cache)
    endpoint, err := e.discovery.DiscoverEndpoint(cluster, azureToken)
    if err != nil {
        return err
    }

    // Criar cliente (se ainda não existe)
    if _, exists := e.clients[cluster]; !exists {
        e.clients[cluster] = client.NewPrometheusClient(endpoint, azureToken)
    }

    return nil
}

func (e *MonitoringEngine) GetMetrics(cluster, namespace, hpaName, period string) (*Metrics, error) {
    // Verificar cache
    cacheKey := cache.CacheKey(cluster, namespace, hpaName, period)
    if cached, ok := e.cache.Get(cacheKey); ok {
        return cached.(*Metrics), nil
    }

    // Buscar do Prometheus
    client := e.clients[cluster]

    start, end, step := calculateTimeRange(period)
    query := client.BuildReplicasQuery(namespace, hpaName)

    result, err := client.QueryRange(ctx, query, start, end, step)
    if err != nil {
        return nil, err
    }

    metrics := parseMetrics(result)

    // Cachear
    e.cache.Set(cacheKey, metrics)

    return metrics, nil
}
```

**Critérios de sucesso:**
- ✅ Engine simplificado funciona com endpoint público
- ✅ Fallback para port-forward funciona (clusters sem endpoint)
- ✅ Cache reduz queries repetidas
- ✅ Código reduzido de 850 → ~300 linhas

---

### **Fase 5: Backend API + Frontend UI** ✅

**Objetivo:** Expor API REST e UI para seleção de períodos.

**Arquivos CRIADOS:**
```
internal/web/handlers/
└── monitoring_v2.go           # NOVO - Handler com períodos selecionáveis

internal/web/frontend/src/components/
└── PeriodSelector.tsx         # NOVO - Seletor de período
```

**Backend:**
```go
// internal/web/handlers/monitoring_v2.go
package handlers

// GET /api/v1/monitoring/hpa/:cluster/:namespace/:name/metrics?period=6h
func (h *Handler) GetHPAMetrics(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    name := c.Param("name")
    period := c.DefaultQuery("period", "6h") // "6h", "1d", "3d", "7d", "30d"

    metrics, err := h.monitoringEngine.GetMetrics(cluster, namespace, name, period)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, metrics)
}
```

**Frontend:**
```typescript
// PeriodSelector.tsx
const periods = [
  { label: "Últimas 6 horas", value: "6h" },
  { label: "Últimas 24 horas", value: "1d" },
  { label: "Últimos 3 dias", value: "3d" },
  { label: "Últimos 7 dias", value: "7d" },
  { label: "Últimos 30 dias", value: "30d" },
];

function PeriodSelector({ onChange }) {
  const [period, setPeriod] = useState("6h");

  const handleChange = (value: string) => {
    setPeriod(value);
    onChange(value);
  };

  return (
    <Select value={period} onValueChange={handleChange}>
      {periods.map(p => (
        <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
      ))}
    </Select>
  );
}
```

**Critérios de sucesso:**
- ✅ API retorna métricas para períodos selecionados
- ✅ Frontend exibe gráficos corretos para cada período
- ✅ Auto-refresh funciona (30s)
- ✅ Cache reduz latência

---

### **Fase 6: Limpeza e Deprecação** ✅

**Objetivo:** Deletar código legado e atualizar documentação.

**Ações:**

1. **Deletar arquivos obsoletos:**
   ```bash
   rm -rf internal/monitoring/portforward/
   rm -rf internal/monitoring/timeslot/
   rm -rf internal/monitoring/baseline/
   rm -rf internal/monitoring/collector/rotating.go
   rm -f internal/monitoring/storage/persistence.go
   ```

2. **Atualizar CLAUDE.md:**
   - Remover seções sobre port-forwards e baseline
   - Adicionar seção sobre endpoint público
   - Atualizar exemplos de código

3. **Atualizar README.md:**
   - Documentar nova arquitetura
   - Adicionar instruções de configuração

4. **Criar migration guide:**
   - Arquivo: `MONITORING_MIGRATION.md`
   - Explicar diferenças entre v1 (port-forward) e v2 (endpoint)

5. **Testes de regressão:**
   ```bash
   go test ./... -v
   make build-web
   ```

**Critérios de sucesso:**
- ✅ Nenhum arquivo órfão no repositório
- ✅ Testes passam (100% green)
- ✅ Documentação atualizada
- ✅ Build funciona sem erros

---

## 🗑️ Arquivos que serão DELETADOS

```
internal/monitoring/
├── portforward/
│   └── portforward.go                    # ~250 linhas
├── timeslot/
│   └── timeslot.go                       # ~220 linhas
├── baseline/
│   ├── queue.go                          # ~150 linhas
│   ├── worker.go                         # ~200 linhas
│   └── scheduler.go                      # ~180 linhas
├── collector/
│   └── rotating.go                       # ~600 linhas
└── storage/
    └── persistence.go (baseline methods) # ~300 linhas

TOTAL: ~1900 linhas deletadas
```

---

## 📝 Arquivos que serão CRIADOS

```
internal/monitoring/
├── discovery/
│   └── prometheus.go                     # ~200 linhas
├── client/
│   ├── prometheus_client.go              # ~150 linhas
│   └── query_builder.go                  # ~100 linhas
└── cache/
    └── memory_cache.go                   # ~150 linhas

internal/web/handlers/
└── monitoring_v2.go                      # ~200 linhas

internal/web/frontend/src/components/
└── PeriodSelector.tsx                    # ~80 linhas

TOTAL: ~880 linhas criadas
```

**Redução líquida:** 1900 - 880 = **1020 linhas deletadas** (~54% menos código)

---

## 🔧 Arquivos que serão MODIFICADOS

```
internal/monitoring/engine/
└── engine.go                             # 850 → 300 linhas (~65% redução)

internal/web/frontend/src/pages/
└── MonitoringPage.tsx                    # Adicionar PeriodSelector

internal/web/server.go                    # Adicionar rota /api/v1/monitoring/hpa/:cluster/:namespace/:name/metrics

CLAUDE.md                                 # Atualizar seção de monitoramento
README.md                                 # Atualizar arquitetura
```

---

## ✅ Testes de Validação

### **Fase 1: Discovery**
```bash
# Teste unitário
go test ./internal/monitoring/discovery -v

# Teste manual
curl -H "Authorization: Bearer $AZURE_TOKEN" \
  https://prometheus-faturamento-hlg.viavarejo.com.br/api/v1/status/config
```

### **Fase 2: Cliente**
```bash
# Teste unitário
go test ./internal/monitoring/client -v

# Teste manual - Query range
curl -H "Authorization: Bearer $AZURE_TOKEN" \
  "https://prometheus-faturamento-hlg.viavarejo.com.br/api/v1/query_range?query=kube_horizontalpodautoscaler_status_current_replicas{namespace='ecommerce',horizontalpodautoscaler='checkout-api'}&start=1700000000&end=1700043600&step=60s"
```

### **Fase 3: Cache**
```bash
# Teste unitário
go test ./internal/monitoring/cache -v

# Teste manual - Verificar TTL
# 1. Requisitar métricas
# 2. Aguardar 30s
# 3. Requisitar novamente (deve vir do cache)
# 4. Aguardar 1h
# 5. Requisitar novamente (deve buscar do Prometheus)
```

### **Fase 4: Engine**
```bash
# Teste de integração
go test ./internal/monitoring/engine -v

# Teste E2E
# 1. Adicionar HPA ao monitoramento
# 2. Requisitar métricas (6h, 1d, 3d)
# 3. Verificar dados corretos
```

### **Fase 5: API + UI**
```bash
# Build frontend
make web-build

# Teste manual
# 1. Abrir http://localhost:8080
# 2. Ir para página de Monitoring
# 3. Adicionar HPA
# 4. Selecionar diferentes períodos (6h, 1d, 3d)
# 5. Verificar gráficos corretos
```

### **Fase 6: Testes de Regressão**
```bash
# Testes completos
go test ./... -v

# Build completo
make build-web
make build

# Verificar binário
./build/new-k8s-hpa version
./build/new-k8s-hpa web -f
```

---

## 🔄 Rollback Plan

### **Se algo der errado:**

1. **Reverter commits:**
   ```bash
   git log --oneline # Encontrar hash do commit ANTES da refatoração
   git revert <hash>..HEAD
   ```

2. **Restaurar arquivos deletados:**
   ```bash
   git checkout <hash-anterior> -- internal/monitoring/portforward/
   git checkout <hash-anterior> -- internal/monitoring/timeslot/
   git checkout <hash-anterior> -- internal/monitoring/baseline/
   git checkout <hash-anterior> -- internal/monitoring/collector/rotating.go
   ```

3. **Rebuild:**
   ```bash
   make build-web
   make build
   ```

4. **Testar sistema legado:**
   ```bash
   go test ./internal/monitoring/... -v
   ```

### **Backup antes de começar:**

```bash
# Criar backup completo antes da refatoração
./backup.sh "pre-monitoring-refactoring-$(date +%Y%m%d)"
```

---

## 📚 Referências

- **Prometheus API:** https://prometheus.io/docs/prometheus/latest/querying/api/
- **Azure AD Authentication:** https://learn.microsoft.com/en-us/azure/active-directory/develop/v2-oauth2-auth-code-flow
- **PromQL Guide:** https://prometheus.io/docs/prometheus/latest/querying/basics/

---

## 📅 Timeline Estimado

| Fase | Descrição | Tempo Estimado | Status |
|------|-----------|----------------|--------|
| 1 | Discovery + Validação | 2-3 horas | ⏳ Pendente |
| 2 | Cliente Prometheus | 2-3 horas | ⏳ Pendente |
| 3 | Cache em Memória | 1-2 horas | ⏳ Pendente |
| 4 | Refatoração Engine | 3-4 horas | ⏳ Pendente |
| 5 | Backend API + Frontend | 2-3 horas | ⏳ Pendente |
| 6 | Limpeza + Testes | 1-2 horas | ⏳ Pendente |
| **TOTAL** | | **11-17 horas** | |

---

## ✅ Checklist Final

- [ ] Fase 1: Discovery implementado e testado
- [ ] Fase 2: Cliente Prometheus funcionando
- [ ] Fase 3: Cache em memória funcionando
- [ ] Fase 4: Engine simplificado (850 → 300 linhas)
- [ ] Fase 5: API + UI com seletor de períodos
- [ ] Fase 6: Arquivos legados deletados
- [ ] Documentação atualizada (CLAUDE.md, README.md)
- [ ] Testes passando (100% green)
- [ ] Build funcionando sem erros
- [ ] Backup criado antes da refatoração
- [ ] Rollback plan testado

---

**🎯 Meta Final:** Sistema de monitoramento 60% mais simples, instantâneo e escalável! 🚀
