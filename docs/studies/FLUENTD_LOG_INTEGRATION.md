# 📋 Estudo: Integração com FluentD para Análise de Logs

**Data:** 20 de novembro de 2025
**Versão:** 1.0
**Status:** Estudo Técnico - Complementar ao Alertmanager Integration

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 📋 Sumário Executivo

Este documento apresenta um estudo sobre integração com FluentD/Fluentbit para análise de logs dos recursos gerenciados (HPAs, Node Pools, CronJobs, Pods), complementando a [integração com Alertmanager](ALERTMANAGER_HPA_INTEGRATION.md) baseada em métricas.

### 🎯 Objetivos

1. **Correlação Logs ↔ Recursos**: Associar logs de pods/containers aos HPAs e Node Pools correspondentes
2. **Detecção de Padrões**: Identificar problemas através de análise de logs (OOMKilled, erros de aplicação, crashes)
3. **Root Cause Analysis**: Facilitar investigação de incidentes com logs contextualizados
4. **Timeline Unificada**: Combinar logs + métricas + alertas em uma visão temporal única
5. **Log Aggregation**: Agregar logs de múltiplos pods de um HPA em uma view consolidada

---

## 🏗️ Arquitetura Proposta

### Componentes

```
┌─────────────────────────────────────────────────────────────────┐
│                     k8s-hpa-manager                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────────┐      ┌──────────────────┐                │
│  │  FluentD Client  │──────│  Log Aggregator  │                │
│  └──────────────────┘      └──────────────────┘                │
│           │                          │                           │
│           │                          │                           │
│           ▼                          ▼                           │
│  ┌──────────────────┐      ┌──────────────────┐                │
│  │  Log Correlator  │──────│  Pattern Matcher │                │
│  └──────────────────┘      └──────────────────┘                │
│           │                          │                           │
│           │                          │                           │
│           ▼                          ▼                           │
│  ┌─────────────────────────────────────────┐                   │
│  │         Log Event Tracker               │                   │
│  │    (integrado com History Tracker)      │                   │
│  └─────────────────────────────────────────┘                   │
│                      │                                           │
│                      ▼                                           │
│           ┌──────────────────┐                                  │
│           │   REST API       │                                  │
│           └──────────────────┘                                  │
└───────────────────┬───────────────────────────────────────────┘
                    │
                    ▼
        ┌───────────────────────┐
        │   Frontend (React)    │
        │   - LogsPanel         │
        │   - LogTimeline       │
        │   - ErrorPatterns     │
        └───────────────────────┘

External Services:
┌──────────────┐       ┌──────────────┐       ┌──────────────┐
│   FluentD    │       │ Elasticsearch│       │   Loki       │
│  (collector) │───────│   (storage)  │───────│  (storage)   │
└──────────────┘       └──────────────┘       └──────────────┘
```

---

## 🔍 Tipos de Logs Relevantes

### 1. **Logs de Aplicação (Application Logs)**

#### OOMKilled Detection
```json
{
  "timestamp": "2025-11-20T10:30:45Z",
  "level": "ERROR",
  "message": "Container killed due to OOM",
  "kubernetes": {
    "pod_name": "myapp-7d9f8c-xyz",
    "namespace": "production",
    "container_name": "app",
    "labels": {
      "app": "myapp"
    }
  },
  "reason": "OOMKilled",
  "exit_code": 137
}
```

**Correlação**:
- Identificar HPA associado via label `app=myapp`
- Verificar memory limits vs usage
- Sugerir aumento de `resources.limits.memory`

#### Crash Detection
```json
{
  "timestamp": "2025-11-20T10:31:00Z",
  "level": "FATAL",
  "message": "panic: runtime error: invalid memory address",
  "stack_trace": "goroutine 1 [running]:\nmain.main()\n...",
  "kubernetes": {
    "pod_name": "api-server-abc123",
    "namespace": "default"
  }
}
```

**Correlação**:
- Identificar padrão de crashes frequentes
- Alertar sobre CrashLoopBackOff antes do Kubernetes
- Log detalhado de stack trace para debug

---

### 2. **Logs de Infraestrutura (Kubernetes Events)**

#### Image Pull Errors
```json
{
  "type": "Warning",
  "reason": "Failed",
  "message": "Failed to pull image \"myregistry.io/app:v2.1\": rpc error: code = Unknown desc = Error response from daemon: pull access denied",
  "involvedObject": {
    "kind": "Pod",
    "name": "myapp-deployment-xyz",
    "namespace": "production"
  },
  "source": "kubelet"
}
```

#### Node Events
```json
{
  "type": "Warning",
  "reason": "NodeNotReady",
  "message": "Node aks-nodepool1-12345 is not ready: kubelet stopped posting node status",
  "involvedObject": {
    "kind": "Node",
    "name": "aks-nodepool1-12345"
  }
}
```

**Correlação com Node Pools**:
- Identificar node pool afetado
- Correlacionar com drain/cordon operations
- Alertar sobre nodes problemáticos

---

### 3. **Logs de HPA (Scaling Events)**

```json
{
  "type": "Normal",
  "reason": "ScaledUpReplicas",
  "message": "Scaled up deployment/myapp from 5 to 8 replicas due to cpu utilization (75% > 70%)",
  "involvedObject": {
    "kind": "HorizontalPodAutoscaler",
    "name": "myapp-hpa",
    "namespace": "production"
  }
}
```

**Análise**:
- Timeline de eventos de scaling
- Correlação com métricas de CPU/memória
- Detecção de scaling thrashing (scale up/down rápido)

---

### 4. **Logs de CronJobs**

```json
{
  "timestamp": "2025-11-20T02:00:00Z",
  "level": "ERROR",
  "message": "CronJob execution failed",
  "cronjob": "backup-database",
  "namespace": "production",
  "exit_code": 1,
  "duration": "45s",
  "error": "Connection timeout to database"
}
```

**Correlação**:
- Histórico de sucessos/falhas
- Duração média de execução
- Alertas sobre falhas consecutivas

---

## 📊 Padrões de Erro Detectáveis

### Pattern Matching Rules

| Padrão | Severidade | Ação |
|--------|------------|------|
| `OOMKilled` OR `exit_code: 137` | 🔴 Critical | Sugerir aumento memory limits |
| `panic:` AND `runtime error` | 🔴 Critical | Log stack trace, alertar dev team |
| `ImagePullBackOff` OR `ErrImagePull` | 🔴 Critical | Verificar registry credentials |
| `CrashLoopBackOff` | 🔴 Critical | Analisar últimos 10 logs do container |
| `Connection refused` (>5x em 1min) | 🟡 Warning | Verificar dependências (DB, APIs) |
| `Timeout` (>10x em 5min) | 🟡 Warning | Possível lentidão de rede/serviço |
| `rate limit exceeded` | 🟡 Warning | Possível necessidade de scale up |
| `disk pressure` OR `no space left` | 🔴 Critical | Node precisa limpeza ou expansão |
| Scaling thrashing (>5 scales em 10min) | 🟡 Warning | HPA mal configurado |

---

## 🛠️ Implementação Técnica

### 1. FluentD Client (Go)

```go
package fluentd

import (
    "context"
    "encoding/json"
    "net/http"
    "time"
)

// FluentD pode usar Elasticsearch ou Loki como backend
type Backend string

const (
    BackendElasticsearch Backend = "elasticsearch"
    BackendLoki         Backend = "loki"
)

type Client struct {
    endpoint string
    backend  Backend
    client   *http.Client
}

type LogQuery struct {
    Namespace    string
    PodName      string
    ContainerName string
    Labels       map[string]string
    StartTime    time.Time
    EndTime      time.Time
    Limit        int
    Level        string // ERROR, WARN, INFO, DEBUG
    Pattern      string // Regex pattern to match
}

type LogEntry struct {
    Timestamp   time.Time         `json:"timestamp"`
    Level       string            `json:"level"`
    Message     string            `json:"message"`
    Kubernetes  KubernetesContext `json:"kubernetes"`
    ExtraFields map[string]interface{} `json:"extra,omitempty"`
}

type KubernetesContext struct {
    PodName       string            `json:"pod_name"`
    Namespace     string            `json:"namespace"`
    ContainerName string            `json:"container_name"`
    Labels        map[string]string `json:"labels"`
    NodeName      string            `json:"node_name"`
}

// NewClient cria novo cliente FluentD
func NewClient(endpoint string, backend Backend) *Client {
    return &Client{
        endpoint: endpoint,
        backend:  backend,
        client: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
}

// QueryLogs busca logs do backend
func (c *Client) QueryLogs(ctx context.Context, query LogQuery) ([]LogEntry, error) {
    switch c.backend {
    case BackendElasticsearch:
        return c.queryElasticsearch(ctx, query)
    case BackendLoki:
        return c.queryLoki(ctx, query)
    default:
        return nil, fmt.Errorf("unsupported backend: %s", c.backend)
    }
}

// queryElasticsearch consulta Elasticsearch
func (c *Client) queryElasticsearch(ctx context.Context, query LogQuery) ([]LogEntry, error) {
    // Elasticsearch Query DSL
    esQuery := map[string]interface{}{
        "query": map[string]interface{}{
            "bool": map[string]interface{}{
                "must": []map[string]interface{}{
                    {
                        "range": map[string]interface{}{
                            "@timestamp": map[string]interface{}{
                                "gte": query.StartTime.Format(time.RFC3339),
                                "lte": query.EndTime.Format(time.RFC3339),
                            },
                        },
                    },
                },
            },
        },
        "size": query.Limit,
        "sort": []map[string]string{
            {"@timestamp": "desc"},
        },
    }

    // Adicionar filtros
    must := esQuery["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"].([]map[string]interface{})

    if query.Namespace != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "kubernetes.namespace": query.Namespace,
            },
        })
    }

    if query.PodName != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "kubernetes.pod_name": query.PodName,
            },
        })
    }

    if query.Level != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "level": query.Level,
            },
        })
    }

    if query.Pattern != "" {
        must = append(must, map[string]interface{}{
            "regexp": map[string]string{
                "message": query.Pattern,
            },
        })
    }

    // Enviar request
    body, _ := json.Marshal(esQuery)
    req, _ := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/_search", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    // Parse response
    var result struct {
        Hits struct {
            Hits []struct {
                Source LogEntry `json:"_source"`
            } `json:"hits"`
        } `json:"hits"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    logs := make([]LogEntry, len(result.Hits.Hits))
    for i, hit := range result.Hits.Hits {
        logs[i] = hit.Source
    }

    return logs, nil
}

// queryLoki consulta Loki
func (c *Client) queryLoki(ctx context.Context, query LogQuery) ([]LogEntry, error) {
    // LogQL query
    logql := "{namespace=\"" + query.Namespace + "\""

    if query.PodName != "" {
        logql += ",pod=\"" + query.PodName + "\""
    }

    if query.ContainerName != "" {
        logql += ",container=\"" + query.ContainerName + "\""
    }

    for k, v := range query.Labels {
        logql += fmt.Sprintf(",%s=\"%s\"", k, v)
    }

    logql += "}"

    if query.Pattern != "" {
        logql += " |~ `" + query.Pattern + "`"
    }

    if query.Level != "" {
        logql += " | json | level=\"" + query.Level + "\""
    }

    // Query range
    params := url.Values{}
    params.Add("query", logql)
    params.Add("start", fmt.Sprintf("%d", query.StartTime.UnixNano()))
    params.Add("end", fmt.Sprintf("%d", query.EndTime.UnixNano()))
    params.Add("limit", fmt.Sprintf("%d", query.Limit))

    req, _ := http.NewRequestWithContext(ctx, "GET", c.endpoint+"/loki/api/v1/query_range?"+params.Encode(), nil)
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    // Parse Loki response
    var result struct {
        Data struct {
            Result []struct {
                Stream map[string]string `json:"stream"`
                Values [][]string        `json:"values"`
            } `json:"result"`
        } `json:"data"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    // Converter para LogEntry
    logs := []LogEntry{}
    for _, stream := range result.Data.Result {
        for _, value := range stream.Values {
            timestamp, _ := strconv.ParseInt(value[0], 10, 64)
            var entry LogEntry
            if err := json.Unmarshal([]byte(value[1]), &entry); err == nil {
                entry.Timestamp = time.Unix(0, timestamp)
                logs = append(logs, entry)
            }
        }
    }

    return logs, nil
}

// GetRecentErrors busca erros recentes para um HPA
func (c *Client) GetRecentErrors(ctx context.Context, namespace, hpaName string, since time.Duration) ([]LogEntry, error) {
    // Buscar labels do HPA para correlacionar pods
    // Assumindo que HPA tem labels que matcham os pods
    query := LogQuery{
        Namespace: namespace,
        Labels: map[string]string{
            "app": hpaName, // ou outro label comum
        },
        StartTime: time.Now().Add(-since),
        EndTime:   time.Now(),
        Level:     "ERROR",
        Limit:     100,
    }

    return c.QueryLogs(ctx, query)
}
```

---

### 2. Log Correlator

```go
package logs

import (
    "context"
    "k8s-hpa-manager/internal/models"
    "k8s-hpa-manager/internal/fluentd"
)

type Correlator struct {
    fluentdClient *fluentd.Client
}

type HPALogContext struct {
    HPA         *models.HPA
    RecentLogs  []fluentd.LogEntry
    ErrorCount  int
    OOMCount    int
    CrashCount  int
    Patterns    []DetectedPattern
}

type DetectedPattern struct {
    Pattern     string
    Occurrences int
    FirstSeen   time.Time
    LastSeen    time.Time
    Severity    string
    Suggestion  string
}

func (c *Correlator) GetHPALogContext(ctx context.Context, hpa *models.HPA, lookback time.Duration) (*HPALogContext, error) {
    // Buscar logs recentes
    logs, err := c.fluentdClient.GetRecentErrors(ctx, hpa.Namespace, hpa.Name, lookback)
    if err != nil {
        return nil, err
    }

    context := &HPALogContext{
        HPA:        hpa,
        RecentLogs: logs,
    }

    // Analisar padrões
    for _, log := range logs {
        // OOMKilled detection
        if strings.Contains(log.Message, "OOMKilled") ||
           (log.ExtraFields["exit_code"] != nil && log.ExtraFields["exit_code"].(float64) == 137) {
            context.OOMCount++
        }

        // Crash detection
        if strings.Contains(log.Message, "panic:") || strings.Contains(log.Message, "FATAL") {
            context.CrashCount++
        }

        context.ErrorCount++
    }

    // Detectar padrões recorrentes
    context.Patterns = c.detectPatterns(logs)

    return context, nil
}

func (c *Correlator) detectPatterns(logs []fluentd.LogEntry) []DetectedPattern {
    patternMap := make(map[string]*DetectedPattern)

    patterns := []struct {
        Name       string
        Regex      *regexp.Regexp
        Severity   string
        Suggestion string
    }{
        {
            Name:       "OOMKilled",
            Regex:      regexp.MustCompile(`OOMKilled|exit.*137`),
            Severity:   "critical",
            Suggestion: "Aumentar memory limits do container",
        },
        {
            Name:       "Connection Refused",
            Regex:      regexp.MustCompile(`connection refused|cannot connect`),
            Severity:   "warning",
            Suggestion: "Verificar dependências (DB, APIs, serviços)",
        },
        {
            Name:       "Timeout",
            Regex:      regexp.MustCompile(`timeout|timed out`),
            Severity:   "warning",
            Suggestion: "Possível lentidão de rede ou serviço. Verificar latência.",
        },
        {
            Name:       "Disk Pressure",
            Regex:      regexp.MustCompile(`disk pressure|no space left`),
            Severity:   "critical",
            Suggestion: "Node precisa limpeza de disco ou expansão de storage",
        },
    }

    for _, log := range logs {
        for _, p := range patterns {
            if p.Regex.MatchString(log.Message) {
                if _, exists := patternMap[p.Name]; !exists {
                    patternMap[p.Name] = &DetectedPattern{
                        Pattern:     p.Name,
                        Severity:    p.Severity,
                        Suggestion:  p.Suggestion,
                        FirstSeen:   log.Timestamp,
                        Occurrences: 0,
                    }
                }
                patternMap[p.Name].Occurrences++
                patternMap[p.Name].LastSeen = log.Timestamp
            }
        }
    }

    result := make([]DetectedPattern, 0, len(patternMap))
    for _, p := range patternMap {
        result = append(result, *p)
    }

    // Ordenar por occurrences
    sort.Slice(result, func(i, j int) bool {
        return result[i].Occurrences > result[j].Occurrences
    })

    return result
}
```

---

### 3. REST API Endpoints

```go
// internal/web/handlers/logs.go
package handlers

import (
    "github.com/gin-gonic/gin"
    "k8s-hpa-manager/internal/logs"
    "time"
)

type LogsHandler struct {
    correlator *logs.Correlator
}

func NewLogsHandler(c *logs.Correlator) *LogsHandler {
    return &LogsHandler{correlator: c}
}

// GET /api/v1/logs/hpa/:cluster/:namespace/:name
func (h *LogsHandler) GetHPALogs(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    name := c.Param("name")

    // Parse lookback (default: 1h)
    lookback := 1 * time.Hour
    if lb := c.Query("lookback"); lb != "" {
        if d, err := time.ParseDuration(lb); err == nil {
            lookback = d
        }
    }

    // Buscar HPA
    hpa, err := h.getHPA(cluster, namespace, name)
    if err != nil {
        c.JSON(404, gin.H{"error": "HPA not found"})
        return
    }

    // Obter contexto de logs
    context, err := h.correlator.GetHPALogContext(c.Request.Context(), hpa, lookback)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{
        "success": true,
        "data": gin.H{
            "hpa":         hpa,
            "logs":        context.RecentLogs,
            "error_count": context.ErrorCount,
            "oom_count":   context.OOMCount,
            "crash_count": context.CrashCount,
            "patterns":    context.Patterns,
        },
    })
}

// GET /api/v1/logs/nodepool/:cluster/:nodepool
func (h *LogsHandler) GetNodePoolLogs(c *gin.Context) {
    cluster := c.Param("cluster")
    nodepool := c.Param("nodepool")

    // Similar ao HPA, mas para node pool
    // Buscar logs de todos os nodes do pool
    // ...
}

// GET /api/v1/logs/cronjob/:cluster/:namespace/:name
func (h *LogsHandler) GetCronJobLogs(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    name := c.Param("name")

    // Buscar execuções recentes do CronJob
    // Incluir logs de sucessos e falhas
    // ...
}
```

---

## 🎨 Frontend Components

### 1. LogsPanel Component

```typescript
// internal/web/frontend/src/components/LogsPanel.tsx
import { useState, useEffect } from 'react';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Badge } from '@/components/ui/badge';
import { Terminal, AlertCircle, Zap } from 'lucide-react';

interface LogEntry {
  timestamp: string;
  level: 'ERROR' | 'WARN' | 'INFO' | 'DEBUG';
  message: string;
  kubernetes: {
    pod_name: string;
    container_name: string;
  };
}

interface DetectedPattern {
  pattern: string;
  occurrences: number;
  severity: 'critical' | 'warning' | 'info';
  suggestion: string;
  first_seen: string;
  last_seen: string;
}

interface LogsPanelProps {
  cluster: string;
  namespace: string;
  hpaName: string;
  lookback?: string; // "1h", "6h", "24h"
}

export const LogsPanel = ({ cluster, namespace, hpaName, lookback = "1h" }: LogsPanelProps) => {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [patterns, setPatterns] = useState<DetectedPattern[]>([]);
  const [stats, setStats] = useState({ errorCount: 0, oomCount: 0, crashCount: 0 });
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchLogs = async () => {
      try {
        const res = await fetch(
          `/api/v1/logs/hpa/${cluster}/${namespace}/${hpaName}?lookback=${lookback}`
        );
        const data = await res.json();

        if (data.success) {
          setLogs(data.data.logs || []);
          setPatterns(data.data.patterns || []);
          setStats({
            errorCount: data.data.error_count,
            oomCount: data.data.oom_count,
            crashCount: data.data.crash_count
          });
        }
      } catch (error) {
        console.error('Error fetching logs:', error);
      } finally {
        setLoading(false);
      }
    };

    fetchLogs();
    const interval = setInterval(fetchLogs, 30000); // Refresh cada 30s
    return () => clearInterval(interval);
  }, [cluster, namespace, hpaName, lookback]);

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'ERROR': return 'text-red-600 bg-red-50';
      case 'WARN': return 'text-yellow-600 bg-yellow-50';
      case 'INFO': return 'text-blue-600 bg-blue-50';
      default: return 'text-gray-600 bg-gray-50';
    }
  };

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical': return 'bg-red-100 text-red-800';
      case 'warning': return 'bg-yellow-100 text-yellow-800';
      default: return 'bg-blue-100 text-blue-800';
    }
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="p-8 text-center text-muted-foreground">
          Carregando logs...
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {/* Stats Summary */}
      <div className="grid grid-cols-3 gap-3">
        <Card>
          <CardContent className="p-4">
            <div className="text-xs text-muted-foreground mb-1">Erros</div>
            <div className="text-2xl font-bold text-red-600">{stats.errorCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="text-xs text-muted-foreground mb-1">OOMKilled</div>
            <div className="text-2xl font-bold text-orange-600">{stats.oomCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="text-xs text-muted-foreground mb-1">Crashes</div>
            <div className="text-2xl font-bold text-purple-600">{stats.crashCount}</div>
          </CardContent>
        </Card>
      </div>

      {/* Detected Patterns */}
      {patterns.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm flex items-center gap-2">
              <Zap className="w-4 h-4" />
              Padrões Detectados
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {patterns.map((pattern, idx) => (
              <div key={idx} className="p-3 border rounded-lg bg-muted/30">
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <AlertCircle className={`w-4 h-4 ${
                      pattern.severity === 'critical' ? 'text-red-600' : 'text-yellow-600'
                    }`} />
                    <span className="font-semibold text-sm">{pattern.pattern}</span>
                  </div>
                  <Badge className={getSeverityColor(pattern.severity)}>
                    {pattern.occurrences}x
                  </Badge>
                </div>
                <div className="text-xs text-muted-foreground mb-2">
                  {new Date(pattern.first_seen).toLocaleString('pt-BR')} → {' '}
                  {new Date(pattern.last_seen).toLocaleString('pt-BR')}
                </div>
                <div className="text-xs bg-blue-50 dark:bg-blue-950/20 p-2 rounded border-l-2 border-blue-500">
                  💡 <strong>Sugestão:</strong> {pattern.suggestion}
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {/* Logs List */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm flex items-center gap-2">
            <Terminal className="w-4 h-4" />
            Logs Recentes ({logs.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ScrollArea className="h-[400px]">
            {logs.length === 0 ? (
              <div className="text-center text-muted-foreground py-8">
                Nenhum log encontrado no período selecionado
              </div>
            ) : (
              <div className="space-y-2">
                {logs.map((log, idx) => (
                  <div key={idx} className="p-3 border rounded-lg hover:bg-muted/50 transition-colors">
                    <div className="flex items-start gap-3">
                      <Badge className={`${getLevelColor(log.level)} text-xs px-2`}>
                        {log.level}
                      </Badge>
                      <div className="flex-1 min-w-0">
                        <div className="text-xs text-muted-foreground mb-1">
                          {new Date(log.timestamp).toLocaleString('pt-BR')} • {' '}
                          <span className="font-mono">{log.kubernetes.pod_name}</span>
                          {log.kubernetes.container_name && (
                            <> / {log.kubernetes.container_name}</>
                          )}
                        </div>
                        <div className="text-sm font-mono break-all">
                          {log.message}
                        </div>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </ScrollArea>
        </CardContent>
      </Card>
    </div>
  );
};
```

---

### 2. Integração no HPAEditor

```typescript
// Adicionar aba "Logs" no HPAEditor
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { LogsPanel } from './LogsPanel';

export const HPAEditor = ({ hpa, cluster }) => {
  return (
    <Tabs defaultValue="config">
      <TabsList>
        <TabsTrigger value="config">Configuração</TabsTrigger>
        <TabsTrigger value="metrics">Métricas</TabsTrigger>
        <TabsTrigger value="alerts">Alertas</TabsTrigger>
        <TabsTrigger value="logs">Logs</TabsTrigger> {/* NOVO */}
      </TabsList>

      <TabsContent value="config">
        {/* Configuração HPA existente */}
      </TabsContent>

      <TabsContent value="metrics">
        {/* Métricas existentes */}
      </TabsContent>

      <TabsContent value="alerts">
        {/* Alertas do Alertmanager */}
      </TabsContent>

      <TabsContent value="logs">
        <LogsPanel
          cluster={cluster}
          namespace={hpa.namespace}
          hpaName={hpa.name}
          lookback="6h"
        />
      </TabsContent>
    </Tabs>
  );
};
```

---

### 3. Timeline Unificada (Logs + Métricas + Alertas)

```typescript
// components/UnifiedTimeline.tsx
interface TimelineEvent {
  timestamp: string;
  type: 'log' | 'metric' | 'alert' | 'scaling';
  severity: 'critical' | 'warning' | 'info';
  title: string;
  description: string;
  details?: any;
}

export const UnifiedTimeline = ({ cluster, namespace, hpaName }) => {
  const [events, setEvents] = useState<TimelineEvent[]>([]);

  useEffect(() => {
    // Buscar e combinar:
    // 1. Logs (FluentD)
    // 2. Alertas (Alertmanager)
    // 3. Eventos de scaling (K8s events)
    // 4. Métricas anômalas (Prometheus)

    const fetchAllEvents = async () => {
      const [logs, alerts, metrics] = await Promise.all([
        fetch(`/api/v1/logs/hpa/${cluster}/${namespace}/${hpaName}`),
        fetch(`/api/v1/alerts/hpa/${cluster}/${namespace}/${hpaName}`),
        fetch(`/api/v1/metrics/anomalies/${cluster}/${namespace}/${hpaName}`)
      ]);

      // Combinar e ordenar por timestamp
      const combined = [
        ...logs.data.logs.map(l => ({
          timestamp: l.timestamp,
          type: 'log',
          severity: l.level === 'ERROR' ? 'critical' : 'info',
          title: l.message.substring(0, 50),
          description: l.message,
          details: l
        })),
        ...alerts.data.alerts.map(a => ({
          timestamp: a.startsAt,
          type: 'alert',
          severity: a.labels.severity,
          title: a.labels.alertname,
          description: a.annotations.summary,
          details: a
        })),
        // ...metrics
      ].sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));

      setEvents(combined);
    };

    fetchAllEvents();
  }, [cluster, namespace, hpaName]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Timeline Unificada</CardTitle>
      </CardHeader>
      <CardContent>
        <ScrollArea className="h-[600px]">
          <div className="relative">
            {/* Linha vertical da timeline */}
            <div className="absolute left-4 top-0 bottom-0 w-0.5 bg-border" />

            <div className="space-y-4 pl-12">
              {events.map((event, idx) => (
                <div key={idx} className="relative">
                  {/* Marcador da timeline */}
                  <div className={`absolute -left-8 w-3 h-3 rounded-full ${
                    event.severity === 'critical' ? 'bg-red-500' :
                    event.severity === 'warning' ? 'bg-yellow-500' :
                    'bg-blue-500'
                  }`} />

                  <div className="border rounded-lg p-4 bg-card">
                    <div className="flex items-start justify-between mb-2">
                      <div className="flex items-center gap-2">
                        <Badge variant={
                          event.type === 'log' ? 'default' :
                          event.type === 'alert' ? 'destructive' :
                          'secondary'
                        }>
                          {event.type}
                        </Badge>
                        <span className="font-semibold text-sm">{event.title}</span>
                      </div>
                      <span className="text-xs text-muted-foreground">
                        {new Date(event.timestamp).toLocaleString('pt-BR')}
                      </span>
                    </div>
                    <p className="text-sm text-muted-foreground">{event.description}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
};
```

---

## 🔗 Integração com Alertmanager

### Correlação: Alertas (Métricas) ↔ Logs (Eventos)

```go
// Combinar alertas do Alertmanager com logs do FluentD
type IncidentAnalysis struct {
    Alert          Alert
    RelatedLogs    []LogEntry
    RootCause      string
    Recommendation string
}

func AnalyzeIncident(alert Alert, fluentdClient *fluentd.Client) (*IncidentAnalysis, error) {
    // Buscar logs no período do alerta
    logs, err := fluentdClient.QueryLogs(context.Background(), fluentd.LogQuery{
        Namespace: alert.Labels["namespace"],
        Labels:    alert.Labels,
        StartTime: alert.StartsAt.Add(-5 * time.Minute), // 5min antes
        EndTime:   time.Now(),
        Level:     "ERROR",
        Limit:     50,
    })

    if err != nil {
        return nil, err
    }

    analysis := &IncidentAnalysis{
        Alert:       alert,
        RelatedLogs: logs,
    }

    // Análise inteligente
    switch alert.Labels["alertname"] {
    case "HPAMaxedOut":
        // Verificar se há OOMKilled nos logs
        for _, log := range logs {
            if strings.Contains(log.Message, "OOMKilled") {
                analysis.RootCause = "Pods sendo OOMKilled devido a falta de memória"
                analysis.Recommendation = "Aumentar memory limits E maxReplicas do HPA"
                return analysis, nil
            }
        }
        analysis.RootCause = "HPA atingiu limite de replicas devido a alta demanda"
        analysis.Recommendation = "Aumentar maxReplicas do HPA"

    case "OOMKilled":
        analysis.RootCause = "Container excedeu memory limit"
        analysis.Recommendation = "Aumentar resources.limits.memory em 50%"

    // ... outros alertas
    }

    return analysis, nil
}
```

---

## 📊 Casos de Uso

### Caso 1: Investigação de OOMKilled

**Fluxo**:
1. Usuário vê alerta "OOMKilled" no HPATab
2. Clica no HPA → abre HPAEditor → aba "Logs"
3. LogsPanel mostra:
   - Padrão detectado: "OOMKilled" (15 ocorrências na última hora)
   - Sugestão: "Aumentar memory limits de 512Mi para 768Mi"
   - Logs detalhados com stack traces
4. Usuário pode:
   - Ver timeline completa (quando começou)
   - Aplicar sugestão (aumentar memory limits)
   - Marcar como investigado no History Tracker

### Caso 2: Debug de CrashLoopBackOff

**Fluxo**:
1. Alerta: "CrashLoopBackOff" no pod X
2. LogsPanel correlaciona e mostra:
   - Últimos 10 logs antes do crash
   - Stack trace do panic
   - Padrão: "Connection refused to database:5432"
3. Root Cause: Database indisponível
4. Ação: Verificar conectividade com DB, não é problema do HPA

### Caso 3: Análise de Scaling Thrashing

**Fluxo**:
1. HPA escalando up/down rapidamente (>5x em 10min)
2. Timeline Unificada mostra:
   - 10:00 - Scale up (3 → 5) - CPU 80%
   - 10:02 - Scale down (5 → 3) - CPU 40%
   - 10:04 - Scale up (3 → 5) - CPU 85%
   - ...
3. Logs mostram: "Connection pool exhausted" durante picos
4. Root Cause: HPA reagindo a latência de rede, não CPU real
5. Recomendação: Ajustar `behavior.scaleDown.stabilizationWindowSeconds`

---

## ⚙️ Configuração do FluentD

### FluentD DaemonSet (Kubernetes)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentd-config
  namespace: logging
data:
  fluent.conf: |
    <source>
      @type tail
      path /var/log/containers/*.log
      pos_file /var/log/fluentd-containers.log.pos
      tag kubernetes.*
      read_from_head true
      <parse>
        @type json
        time_format %Y-%m-%dT%H:%M:%S.%NZ
      </parse>
    </source>

    # Enrich com metadados Kubernetes
    <filter kubernetes.**>
      @type kubernetes_metadata
      @id filter_kube_metadata
    </filter>

    # Detectar OOMKilled
    <filter kubernetes.**>
      @type grep
      <regexp>
        key log
        pattern /(OOMKilled|exit code 137)/
      </regexp>
      <record>
        severity critical
        event_type oomkilled
      </record>
    </filter>

    # Output para Elasticsearch
    <match kubernetes.**>
      @type elasticsearch
      host elasticsearch.logging.svc.cluster.local
      port 9200
      logstash_format true
      logstash_prefix k8s
      include_tag_key true
      type_name _doc
      <buffer>
        @type file
        path /var/log/fluentd-buffers/kubernetes.system.buffer
        flush_mode interval
        flush_interval 5s
      </buffer>
    </match>
```

---

## 🚀 Roadmap de Implementação

### Sprint 1: Foundation
- [ ] Implementar FluentD Client (Elasticsearch)
- [ ] Suporte a Loki (opcional)
- [ ] Testes unitários

### Sprint 2: Correlation
- [ ] Implementar Log Correlator
- [ ] Pattern Matcher com regex
- [ ] Integração com History Tracker

### Sprint 3: API
- [ ] REST endpoints para logs
- [ ] Filtros e queries avançadas
- [ ] Paginação

### Sprint 4: Frontend
- [ ] LogsPanel component
- [ ] Integração no HPAEditor
- [ ] Stats e padrões visuais

### Sprint 5: Timeline
- [ ] UnifiedTimeline component
- [ ] Combinar logs + alertas + métricas
- [ ] Visualização temporal

### Sprint 6: Advanced Analysis
- [ ] Incident Analysis (correlação alerta ↔ logs)
- [ ] Root Cause suggestions
- [ ] Export de relatórios

---

## 🧪 Testes

### Testes de Integração

```go
func TestFluentDClient_QueryLogs(t *testing.T) {
    // Mock Elasticsearch server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        response := `{
            "hits": {
                "hits": [
                    {
                        "_source": {
                            "timestamp": "2025-11-20T10:30:00Z",
                            "level": "ERROR",
                            "message": "OOMKilled",
                            "kubernetes": {
                                "namespace": "production",
                                "pod_name": "app-xyz"
                            }
                        }
                    }
                ]
            }
        }`
        w.Write([]byte(response))
    }))
    defer server.Close()

    client := fluentd.NewClient(server.URL, fluentd.BackendElasticsearch)

    logs, err := client.QueryLogs(context.Background(), fluentd.LogQuery{
        Namespace: "production",
        Level:     "ERROR",
        Limit:     10,
        StartTime: time.Now().Add(-1 * time.Hour),
        EndTime:   time.Now(),
    })

    assert.NoError(t, err)
    assert.Len(t, logs, 1)
    assert.Equal(t, "OOMKilled", logs[0].Message)
}
```

---

## 📚 Benefícios da Integração FluentD

### 1. **Observabilidade Completa**
- ✅ Métricas (Prometheus) + Logs (FluentD) + Alertas (Alertmanager)
- ✅ Visão 360° de cada recurso (HPA, Node Pool, CronJob)

### 2. **Root Cause Analysis**
- ✅ Correlação automática alerta → logs
- ✅ Stack traces e contexto completo
- ✅ Detecção de padrões recorrentes

### 3. **Proatividade**
- ✅ Detectar OOMKilled antes de virar CrashLoopBackOff
- ✅ Identificar problemas de conectividade
- ✅ Alertar sobre degradação de performance

### 4. **Developer Experience**
- ✅ Todos os logs em um só lugar (não precisa kubectl logs)
- ✅ Interface visual amigável
- ✅ Timeline unificada para investigação

---

## 🔗 Integração com Sistema Existente

### Compatibilidade com Alertmanager Integration

| Feature | Alertmanager | FluentD | Combinado |
|---------|--------------|---------|-----------|
| **Detecção** | Baseado em métricas | Baseado em logs | 🚀 Dupla detecção |
| **OOMKilled** | ⚠️ Após evento | ✅ Em tempo real | ✅ Detecção imediata |
| **Root Cause** | ❌ Limitado | ✅ Stack traces | ✅ Análise completa |
| **Histórico** | 🟡 Curto prazo | ✅ Longo prazo | ✅ Timeline completa |
| **Actionable** | ✅ Sugestões | 🟡 Diagnóstico | ✅ Ação + Contexto |

**Exemplo de uso conjunto**:
```
1. Prometheus detecta: HighCPUUsage (métrica)
2. Alertmanager dispara alerta
3. FluentD mostra logs: "Connection pool exhausted" (causa raiz)
4. Sistema sugere: Aumentar connection pool, não CPU limits
```

---

## 📝 Próximos Passos

1. ✅ **Estudo concluído** - Revisão pendente
2. ⏳ Aprovação da arquitetura
3. ⏳ Decisão: Elasticsearch ou Loki como backend?
4. ⏳ Configuração do FluentD no cluster de testes
5. ⏳ Início da implementação (Sprint 1)

---

## 📖 Referências

- [FluentD Documentation](https://docs.fluentd.org/)
- [Fluentbit vs FluentD](https://docs.fluentbit.io/manual/about/fluentd-and-fluent-bit)
- [Elasticsearch Query DSL](https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl.html)
- [Loki LogQL](https://grafana.com/docs/loki/latest/logql/)
- [Kubernetes Logging Architecture](https://kubernetes.io/docs/concepts/cluster-administration/logging/)

---

**Autor**: Claude Code
**Revisão**: Pendente
**Aprovação**: Pendente
