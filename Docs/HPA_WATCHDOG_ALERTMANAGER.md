# HPA Watchdog - Integração com Alertmanager

## 🎯 Visão Geral

**MUDANÇA DE PARADIGMA**: Com Alertmanager disponível, o HPA Watchdog pode evoluir de "detector de anomalias" para **"dashboard inteligente de alertas existentes"** + detector complementar.

## 🏗️ Arquitetura Atualizada (3 Camadas)

```
┌─────────────────────────────────────────────────────────────────────┐
│                         HPA Watchdog                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────┐  ┌───────────────┐  ┌─────────────────────────┐ │
│  │  K8s API     │  │  Prometheus   │  │  Alertmanager API       │ │
│  │  (Config)    │  │  (Metrics)    │  │  (Existing Alerts)      │ │
│  └──────┬───────┘  └───────┬───────┘  └───────────┬─────────────┘ │
│         │                   │                      │                │
│         └──────────┬────────┴──────────────────────┘                │
│                    ▼                                                 │
│         ┌────────────────────────────┐                              │
│         │   Unified Collector        │                              │
│         │  • HPA config (K8s)        │                              │
│         │  • Metrics (Prometheus)    │                              │
│         │  • Alerts (Alertmanager)   │                              │
│         └────────────┬───────────────┘                              │
│                      ▼                                               │
│         ┌────────────────────────────┐                              │
│         │  Alert Aggregator          │                              │
│         │  • Sync Alertmanager       │                              │
│         │  • Detect new anomalies    │                              │
│         │  • Enrich context          │                              │
│         │  • Deduplicate             │                              │
│         └────────────┬───────────────┘                              │
│                      ▼                                               │
│         ┌────────────────────────────┐                              │
│         │  Unified Alert Dashboard   │                              │
│         │  • Alertmanager alerts     │                              │
│         │  • Watchdog alerts         │                              │
│         │  • Correlation view        │                              │
│         └────────────────────────────┘                              │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 🔄 Dois Modos de Operação

### Modo 1: Dashboard de Alertas (Principal)

**Objetivo**: Visualizar alertas **EXISTENTES** do Prometheus/Alertmanager de forma centralizada e contextualizada.

```
Alertmanager → HPA Watchdog TUI → Enriquecimento com Métricas
```

**Workflow:**
1. Watchdog sincroniza alertas do Alertmanager via API
2. Filtra apenas alertas relacionados a HPAs
3. Enriquece com contexto (métricas, histórico, correlação)
4. Exibe em TUI com visualização rica

**Vantagens:**
- ✅ Aproveita regras Prometheus já configuradas
- ✅ Não duplica lógica de alertas
- ✅ Centraliza visualização de múltiplos clusters
- ✅ Adiciona contexto rico (gráficos, histórico, correlação)

### Modo 2: Detector Complementar (Secundário)

**Objetivo**: Detectar anomalias que **NÃO** estão cobertas por regras Prometheus.

```
Prometheus Metrics → Watchdog Analyzer → Alertas Customizados
```

**Casos de Uso:**
- Padrões complexos não cobertos por PromQL simples
- Análise temporal avançada (ex: oscilação de réplicas)
- Correlação entre múltiplos HPAs
- Machine learning para predição (futuro)

---

## 🔌 Alertmanager API

### Endpoints Úteis

```go
// Base URL
alertmanagerURL := "http://alertmanager.monitoring.svc:9093"

// 1. Listar alertas ativos
GET /api/v2/alerts

// 2. Listar alertas silenciados
GET /api/v2/silences

// 3. Criar silêncio (acknowledge)
POST /api/v2/silences

// 4. Deletar silêncio
DELETE /api/v2/silence/{id}

// 5. Status geral
GET /api/v2/status
```

### Estrutura de Alerta (Alertmanager)

```go
type AlertmanagerAlert struct {
    Annotations  map[string]string `json:"annotations"`
    EndsAt       time.Time         `json:"endsAt"`
    StartsAt     time.Time         `json:"startsAt"`
    UpdatedAt    time.Time         `json:"updatedAt"`
    Fingerprint  string            `json:"fingerprint"`
    Receivers    []Receiver        `json:"receivers"`
    Status       AlertStatus       `json:"status"`
    Labels       map[string]string `json:"labels"`
    GeneratorURL string            `json:"generatorURL"`
}

type AlertStatus struct {
    State       string   `json:"state"` // "active", "suppressed", "unprocessed"
    SilencedBy  []string `json:"silencedBy"`
    InhibitedBy []string `json:"inhibitedBy"`
}

// Exemplo de alerta HPA-related
{
  "labels": {
    "alertname": "HPAMaxedOut",
    "namespace": "api-gateway",
    "horizontalpodautoscaler": "nginx",
    "severity": "warning"
  },
  "annotations": {
    "summary": "HPA nginx has been running at max replicas for 15m",
    "description": "HPA nginx in namespace api-gateway has been at max replicas (10) for more than 15 minutes."
  },
  "startsAt": "2025-10-23T14:30:00Z",
  "status": {
    "state": "active"
  }
}
```

---

## 💡 Casos de Uso Poderosos

### 1. Dashboard Centralizado Multi-Cluster

**Problema**: Você tem 5 clusters de produção, cada um com Prometheus/Alertmanager. Difícil visualizar todos os alertas HPA em um só lugar.

**Solução com Watchdog:**
```
HPA Watchdog
├─ akspriv-prod-east (Alertmanager)
│  ├─ 🔴 HPAMaxedOut: api-gateway/nginx
│  └─ ⚠️  HPAScalingSlowly: payments/api
├─ akspriv-prod-west (Alertmanager)
│  ├─ 🔴 HPACPUHigh: frontend/web
│  └─ ℹ️  HPAConfigChanged: backend/service
├─ akspriv-qa (Alertmanager)
│  └─ (sem alertas)
└─ ...

Total: 4 alertas ativos em 3 clusters
```

**Interface TUI:**
```
╔═ HPA Alerts Dashboard (Multi-Cluster) ════════════════════════════╗
║                                                                    ║
║  📊 Summary: 4 active alerts across 3 clusters                    ║
║                                                                    ║
║  🔴 CRITICAL (1)  ⚠️  WARNING (2)  ℹ️  INFO (1)                   ║
║                                                                    ║
║  ┌─ akspriv-prod-east (2 alerts) ──────────────────────────────┐║
║  │ 🔴 HPAMaxedOut | 14:30:22 | api-gateway/nginx               │║
║  │    At max replicas (10) for 15 minutes                       │║
║  │    [Enriquecer] [Silenciar] [Details]                        │║
║  │                                                               │║
║  │ ⚠️  HPAScalingSlowly | 14:25:10 | payments/api               │║
║  │    Scaling too slowly, target not met                        │║
║  │    [Enriquecer] [Silenciar] [Details]                        │║
║  └───────────────────────────────────────────────────────────────┘║
║                                                                    ║
║  ┌─ akspriv-prod-west (2 alerts) ───────────────────────────────┐║
║  │ 🔴 HPACPUHigh | 14:32:45 | frontend/web                      │║
║  │    CPU usage above 90% for 10 minutes                        │║
║  │    [Enriquecer] [Silenciar] [Details]                        │║
║  │                                                               │║
║  │ ℹ️  HPAConfigChanged | 14:20:00 | backend/service            │║
║  │    Max replicas changed from 20 to 30                        │║
║  │    [Acknowledge]                                              │║
║  └───────────────────────────────────────────────────────────────┘║
╚════════════════════════════════════════════════════════════════════╝
[Enter] Enrich with metrics  [S] Silence  [A] Acknowledge  [ESC] Back
```

### 2. Enriquecimento de Contexto

**Problema**: Alerta do Alertmanager tem informação básica. Falta contexto rico.

**Solução:**

**Alerta Alertmanager (básico):**
```
🔴 HPAMaxedOut
HPA nginx has been at max replicas (10) for 15 minutes
```

**Enriquecido pelo Watchdog:**
```
╔═ Alert Details (Enriched) ════════════════════════════════════════╗
║                                                                    ║
║  🔴 HPAMaxedOut (Alertmanager)                                    ║
║  📊 HPA: api-gateway/nginx                                        ║
║  ⏰ Started: 14:30:22 (15 minutes ago)                            ║
║                                                                    ║
║  📈 Current State (Prometheus):                                   ║
║     • Replicas: 10/10 (100% capacity)                            ║
║     • CPU: 88% (target: 70%)                                     ║
║     • Memory: 75% (target: 80%)                                  ║
║     • Request rate: 650 req/s                                    ║
║     • Error rate: 0.8% (✅ healthy)                              ║
║     • P95 latency: 320ms (✅ healthy)                            ║
║                                                                    ║
║  📊 CPU History (15 min):                                         ║
║   100%│                                                           ║
║    90%│                         ╭────────                         ║
║    80%│                   ╭─────╯                                ║
║    70%│             ╭─────╯                                      ║
║    60%├─────────────                                             ║
║       14:15    14:20    14:25    14:30    14:35                 ║
║                                                                    ║
║  💡 Análise (Watchdog):                                           ║
║     • HPA está funcionando corretamente                          ║
║     • CPU sustentado acima do target → scaling esperado          ║
║     • Request rate estável (não é spike temporário)              ║
║     • AÇÃO RECOMENDADA: Aumentar max_replicas para 15           ║
║                                                                    ║
║  🔗 Links:                                                        ║
║     • Prometheus: http://prometheus.../graph?g0.expr=...         ║
║     • Grafana: http://grafana.../d/hpa-dashboard/...             ║
║     • K8s: kubectl get hpa nginx -n api-gateway                  ║
║                                                                    ║
║  [S] Silence for 1h  [I] Increase max_replicas  [ESC] Back       ║
╚════════════════════════════════════════════════════════════════════╝
```

### 3. Silenciar Alertas via Watchdog

**Workflow:**
```
1. Usuário vê alerta no Watchdog TUI
2. Pressiona [S] para silenciar
3. Modal aparece:
   ┌─ Silence Alert ──────────────────────┐
   │ Duration: [1h ▼]                     │
   │ Comment: [Planned maintenance     ] │
   │ Creator: [paulo@loggi.com        ] │
   │                                      │
   │ [Confirm]  [Cancel]                  │
   └──────────────────────────────────────┘
4. Watchdog chama Alertmanager API para criar silence
5. Alerta some da lista ativa (vai para "Silenced")
```

### 4. Correlação de Alertas

**Cenário**: Múltiplos alertas relacionados ao mesmo incident.

**Watchdog detecta:**
```
╔═ Correlated Alerts ════════════════════════════════════════════════╗
║                                                                     ║
║  🔥 INCIDENT DETECTED: High Load in api-gateway cluster            ║
║                                                                     ║
║  🔴 Primary Alert (Root Cause):                                    ║
║     HPACPUHigh @ api-gateway/nginx (14:30:22)                     ║
║     CPU 95% for 10 minutes                                        ║
║                                                                     ║
║  ⚠️  Related Alerts (Symptoms):                                    ║
║     • HPAMaxedOut @ api-gateway/nginx (14:32:10)                  ║
║       At max replicas, cannot scale further                       ║
║                                                                     ║
║     • HighErrorRate @ api-gateway/nginx (14:33:45)                ║
║       Error rate 8% (baseline: 0.5%)                              ║
║                                                                     ║
║     • HighLatency @ api-gateway/nginx (14:34:20)                  ║
║       P95 latency 2.5s (baseline: 150ms)                          ║
║                                                                     ║
║  💡 Root Cause Analysis (Watchdog):                                ║
║     1. Traffic spike: 850 req/s (↑300% baseline)                  ║
║     2. HPA scaled to max (10 replicas)                            ║
║     3. Insufficient capacity → CPU 95% → errors + latency         ║
║                                                                     ║
║  🚨 Recommended Actions:                                           ║
║     1. URGENT: Increase max_replicas from 10 to 20                ║
║     2. Monitor traffic source (DDoS? Marketing campaign?)         ║
║     3. Check upstream services for cascading failures             ║
║                                                                     ║
║  [Execute Action 1]  [Silence All]  [ESC] Back                    ║
╚═════════════════════════════════════════════════════════════════════╝
```

---

## 🔧 Implementação

### Alertmanager Client (Go)

```go
package alertmanager

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

type Client struct {
    baseURL    string
    httpClient *http.Client
}

func NewClient(baseURL string) *Client {
    return &Client{
        baseURL:    baseURL,
        httpClient: &http.Client{Timeout: 10 * time.Second},
    }
}

// Alert representa um alerta do Alertmanager
type Alert struct {
    Annotations  map[string]string `json:"annotations"`
    EndsAt       time.Time         `json:"endsAt"`
    StartsAt     time.Time         `json:"startsAt"`
    UpdatedAt    time.Time         `json:"updatedAt"`
    Fingerprint  string            `json:"fingerprint"`
    Status       AlertStatus       `json:"status"`
    Labels       map[string]string `json:"labels"`
    GeneratorURL string            `json:"generatorURL"`
}

type AlertStatus struct {
    State       string   `json:"state"` // "active", "suppressed", "unprocessed"
    SilencedBy  []string `json:"silencedBy"`
    InhibitedBy []string `json:"inhibitedBy"`
}

// GetAlerts retorna todos os alertas ativos
func (c *Client) GetAlerts(ctx context.Context) ([]Alert, error) {
    url := fmt.Sprintf("%s/api/v2/alerts", c.baseURL)

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("alertmanager returned status %d", resp.StatusCode)
    }

    var alerts []Alert
    if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
        return nil, err
    }

    return alerts, nil
}

// GetHPAAlerts filtra apenas alertas relacionados a HPAs
func (c *Client) GetHPAAlerts(ctx context.Context) ([]Alert, error) {
    alerts, err := c.GetAlerts(ctx)
    if err != nil {
        return nil, err
    }

    var hpaAlerts []Alert
    for _, alert := range alerts {
        // Filtrar por labels que indicam HPAs
        if _, hasHPA := alert.Labels["horizontalpodautoscaler"]; hasHPA {
            hpaAlerts = append(hpaAlerts, alert)
        } else if _, hasNamespace := alert.Labels["namespace"]; hasNamespace {
            // Checar se alertname contém "HPA"
            if alertname, ok := alert.Labels["alertname"]; ok {
                if containsHPA(alertname) {
                    hpaAlerts = append(hpaAlerts, alert)
                }
            }
        }
    }

    return hpaAlerts, nil
}

// Silence cria um silenciamento no Alertmanager
type Silence struct {
    ID        string            `json:"id,omitempty"`
    Matchers  []Matcher         `json:"matchers"`
    StartsAt  time.Time         `json:"startsAt"`
    EndsAt    time.Time         `json:"endsAt"`
    CreatedBy string            `json:"createdBy"`
    Comment   string            `json:"comment"`
}

type Matcher struct {
    Name    string `json:"name"`
    Value   string `json:"value"`
    IsRegex bool   `json:"isRegex"`
}

func (c *Client) CreateSilence(ctx context.Context, silence Silence) (string, error) {
    url := fmt.Sprintf("%s/api/v2/silences", c.baseURL)

    payload, err := json.Marshal(silence)
    if err != nil {
        return "", err
    }

    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("failed to create silence: status %d", resp.StatusCode)
    }

    var result struct {
        SilenceID string `json:"silenceID"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    return result.SilenceID, nil
}

// Helper function
func containsHPA(s string) bool {
    return strings.Contains(strings.ToLower(s), "hpa")
}
```

### Unified Alert Model

```go
package models

import "time"

// UnifiedAlert combina alertas de Alertmanager + Watchdog
type UnifiedAlert struct {
    ID          string
    Source      AlertSource    // Alertmanager ou Watchdog
    Severity    AlertSeverity  // Critical, Warning, Info
    Type        AnomalyType

    // Core Info
    Cluster     string
    Namespace   string
    HPAName     string
    Timestamp   time.Time

    // Message
    Summary     string
    Description string

    // Alertmanager specific
    Fingerprint  string
    GeneratorURL string
    Status       string // "active", "suppressed"
    SilencedBy   []string

    // Enrichment (from Prometheus + Watchdog)
    Snapshot     *HPASnapshot        // Estado atual do HPA
    Context      *AlertContext       // Contexto adicional
    Correlation  []string            // IDs de alertas correlacionados

    // Actions
    Acknowledged bool
    AckedAt      *time.Time
    AckedBy      string
}

type AlertSource int

const (
    AlertSourceAlertmanager AlertSource = iota
    AlertSourceWatchdog
)

type AlertContext struct {
    // Métricas adicionais
    RequestRate    float64
    ErrorRate      float64
    P95Latency     float64

    // Análise
    Trend          string // "increasing", "decreasing", "stable"
    PredictedState string // "will_max_out", "will_stabilize"

    // Links
    PrometheusURL  string
    GrafanaURL     string
    KubectlCommand string
}
```

---

## ⚙️ Configuração Atualizada

### watchdog.yaml (com Alertmanager)

```yaml
monitoring:
  scan_interval_seconds: 30
  history_retention_minutes: 5

  # Prometheus Integration
  prometheus:
    enabled: true
    auto_discover: true
    fallback_to_metrics_server: true

  # Alertmanager Integration (NOVO)
  alertmanager:
    enabled: true                    # Habilita integração
    auto_discover: true              # Descobre endpoint via K8s Service
    sync_interval_seconds: 30        # Intervalo de sincronização

    # Endpoints por cluster (se auto_discover=false)
    endpoints:
      akspriv-prod-east: "http://alertmanager.monitoring.svc:9093"
      akspriv-prod-west: "http://alertmanager.monitoring.svc:9093"

    # Filtros
    filters:
      only_hpa_related: true         # Filtrar apenas alertas HPA
      exclude_silenced: false        # Mostrar alertas silenciados?
      min_severity: "warning"        # Ignorar alertas "info"

    # Auto-discovery patterns
    discovery_patterns:
      - "alertmanager.monitoring.svc:9093"
      - "alertmanager-operated.monitoring.svc:9093"
      - "kube-prometheus-stack-alertmanager.monitoring.svc:9093"

alerts:
  # Alert sources priority
  # Se mesmo alerta existe em Alertmanager E Watchdog, qual mostrar?
  source_priority: ["alertmanager", "watchdog"]

  # Deduplication
  deduplicate: true                  # Remover alertas duplicados
  dedupe_window_minutes: 5           # Janela de deduplicação

  # Correlation
  auto_correlate: true               # Agrupar alertas relacionados
  correlation_window_minutes: 10     # Janela de correlação

thresholds:
  # ... (thresholds existentes)
```

---

## 🎨 Interface TUI Atualizada

### Dashboard com Alertmanager

```
╔═ HPA Watchdog v1.0.0 ════════════════════════════════════════════════╗
║  Clusters: 5  │  HPAs: 142  │  Alerts: 8 (4 Alertmanager, 4 Watchdog)║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                       ║
║  📊 ALERT SOURCES                                                     ║
║  ┌─ Alertmanager (4 alerts) ──────────────────────────────────────┐ ║
║  │ 🔴 CRITICAL: 1  ⚠️  WARNING: 2  ℹ️  INFO: 1                     │ ║
║  │ Last sync: 14:35:45 (15s ago)                                   │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                       ║
║  ┌─ Watchdog (4 alerts) ───────────────────────────────────────────┐ ║
║  │ 🔴 CRITICAL: 2  ⚠️  WARNING: 2                                   │ ║
║  │ Detected: 4 anomalies not covered by Prometheus rules           │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                       ║
║  🔥 ACTIVE ALERTS (8) ────────────────────────────────────────────── ║
║                                                                       ║
║  🔴 [AM] HPAMaxedOut | 14:30 | akspriv-prod-east/api-gateway/nginx  ║
║      At max replicas (10) for 15 minutes                             ║
║      [E] Enrich  [S] Silence  [→] Details                            ║
║                                                                       ║
║  🔴 [WD] ReplicaOscillation | 14:32 | akspriv-qa/payments/api       ║
║      Replicas changing rapidly (6 changes in 5 min)                  ║
║      [A] Acknowledge  [→] Details                                    ║
║                                                                       ║
║  ⚠️  [AM] HPAScalingSlowly | 14:25 | akspriv-prod-east/payments/api ║
║      Scaling too slowly, target not met                              ║
║      [E] Enrich  [S] Silence                                         ║
║                                                                       ║
║  ... (5 more alerts)                                                 ║
║                                                                       ║
║  [↑↓] Navigate  [E] Enrich  [S] Silence  [A] Ack  [C] Correlate     ║
╚═══════════════════════════════════════════════════════════════════════╝

Legend: [AM] = Alertmanager  [WD] = Watchdog
```

---

## 🚀 Vantagens da Arquitetura com Alertmanager

### ✅ O Que Você Ganha

1. **Dashboard Centralizado Multi-Cluster**
   - Visualiza alertas de 5+ clusters em uma TUI
   - Não precisa abrir 5 Alertmanagers diferentes

2. **Enriquecimento de Contexto**
   - Alertas Alertmanager básicos
   - Watchdog adiciona: gráficos, histórico, métricas correlacionadas

3. **Gestão de Silenciamentos**
   - Silenciar alertas diretamente da TUI
   - Watchdog chama API Alertmanager

4. **Correlação Inteligente**
   - Detecta alertas relacionados ao mesmo incident
   - Agrupa por root cause

5. **Complementaridade**
   - Alertmanager: Alertas de regras Prometheus (cobertura ampla)
   - Watchdog: Análises complexas não cobertas por PromQL simples

6. **Não Duplica Lógica**
   - Reutiliza regras Prometheus existentes
   - Watchdog foca em visualização e análise avançada

---

## 📦 Dependências Adicionais

```bash
# Alertmanager não tem client oficial Go
# Usar HTTP direto ou client comunitário
go get github.com/prometheus/alertmanager/api/v2/client
```

---

## 🎯 Decisão Final ATUALIZADA

### ✅ RECOMENDAÇÃO: Arquitetura de 3 Camadas

```
1. K8s API         → Configuração e estado
2. Prometheus      → Métricas e histórico
3. Alertmanager    → Alertas existentes (fonte PRIMÁRIA)
```

**Papéis:**
- **Alertmanager**: Fonte principal de alertas (regras Prometheus)
- **Watchdog**: Dashboard centralizado + detector complementar + enriquecimento

**Modo de Operação:**
- 70% do tempo: Visualizar alertas do Alertmanager
- 30% do tempo: Detectar anomalias complementares (não cobertas por Prometheus)

---

## 🚀 Impacto no Roadmap

### Fase 1 (MVP) - Atualizada

- [x] Core monitoring (K8s + Prometheus)
- [ ] **Alertmanager client** (NOVO)
- [ ] **Alert aggregator** (NOVO - unifica Alertmanager + Watchdog)
- [ ] TUI básico com dashboard de alertas
- [ ] Config system

### Fase 2 - Atualizada

- [ ] **Silence management via TUI** (NOVO)
- [ ] **Alert correlation** (NOVO)
- [ ] Enhanced UI com gráficos
- [ ] Advanced analysis (detector complementar)

---

**Conclusão**: Com Alertmanager, o HPA Watchdog se torna uma **"Control Tower"** para alertas HPA em múltiplos clusters! 🚀🎯
