# 🔍 Plano de Integração com ELK Stack

**Data:** 2025-12-04
**Versão:** v1.4.0+ (proposta)
**Objetivo:** Integrar Elasticsearch, Logstash, Kibana para logs avançados e análise

---

## 📊 Análise: É uma Boa Ideia?

### ✅ **SIM! Faz muito sentido para sua aplicação**

**Por quê:**

1. **Contexto Kubernetes** - Você já gerencia clusters K8s, o ELK é padrão para logging
2. **Troubleshooting Avançado** - Logs de pods/containers agregados e pesquisáveis
3. **Correlação de Eventos** - Relacionar mudanças de HPA com logs de aplicação
4. **Auditoria Completa** - Rastreabilidade de todas operações + logs externos
5. **Análise de Performance** - Identificar causas raiz de problemas via logs

---

## 🎯 Casos de Uso Reais

### 1. **Troubleshooting de Alertas**
```
Cenário: Alerta "HPA CPU > 80%"

Workflow Atual:
1. Ver alerta no Prometheus
2. Abrir Kibana manualmente
3. Buscar logs do pod
4. Copiar/colar filtros manualmente

Workflow com Integração:
1. Clicar no alerta
2. Ver métricas + logs do pod no mesmo lugar
3. Filtros automáticos (namespace, pod, time range)
```

### 2. **Análise de Mudanças**
```
Cenário: Após aplicar upscale, validar se funcionou

Workflow Atual:
1. Aplicar mudança de HPA
2. Esperar alguns minutos
3. Ir para Kibana manualmente
4. Buscar logs de erro

Workflow com Integração:
1. Aplicar mudança
2. Modal "Analisando impacto..."
3. Gráfico de métricas + logs de erro lado a lado
4. ✅ "Sem erros detectados" ou ⚠️ "3 erros encontrados"
```

### 3. **Root Cause Analysis**
```
Cenário: Pod crashando repetidamente

Workflow Atual:
1. Ver restart count alto
2. kubectl logs pod-name --previous
3. Copiar erros e analisar

Workflow com Integração:
1. Ver restart count alto
2. Botão "Ver Crash Logs"
3. Últimos 10 crashes com stacktraces
4. Padrões detectados automaticamente
```

---

## 🏗️ Arquitetura Proposta

### Visão Geral

```
┌──────────────────────────────────────────────────┐
│  new-k8s-hpa (Frontend React)                   │
│                                                  │
│  ┌────────────────────────────────────────────┐ │
│  │  Componente: LogsViewer                    │ │
│  │  • Query builder visual                    │ │
│  │  • Timeline de eventos                     │ │
│  │  │  • Syntax highlighting                   │ │
│  └────────────────────────────────────────────┘ │
│                     │                            │
│                     │ HTTP                       │
│                     ▼                            │
│  ┌────────────────────────────────────────────┐ │
│  │  Backend Go (handlers/logs.go)            │ │
│  │  • Proxy para Elasticsearch                │ │
│  │  • Query builder (DSL)                     │ │
│  │  • Cache de queries frequentes             │ │
│  └────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
                     │
                     │ HTTP/HTTPS
                     ▼
┌──────────────────────────────────────────────────┐
│  Elasticsearch                                   │
│  • Índices: k8s-logs-*                          │
│  • Retention: 30 dias (configurável)            │
│  • Aggregations para análises                   │
└──────────────────────────────────────────────────┘
                     ▲
                     │
┌──────────────────────────────────────────────────┐
│  Kubernetes Cluster                              │
│  • Fluentd/Filebeat shipping logs               │
│  • Pods de todas as aplicações                  │
└──────────────────────────────────────────────────┘
```

---

## 💻 Implementação Técnica

### Backend: Elasticsearch Client (Go)

```go
// internal/logs/elasticsearch.go
package logs

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/elastic/go-elasticsearch/v8"
    "github.com/elastic/go-elasticsearch/v8/esapi"
)

type ElasticsearchClient struct {
    client *elasticsearch.Client
    index  string
}

// NewElasticsearchClient cria um novo cliente
func NewElasticsearchClient(addresses []string, index string) (*ElasticsearchClient, error) {
    cfg := elasticsearch.Config{
        Addresses: addresses,
        // Se usar autenticação:
        // Username: os.Getenv("ES_USERNAME"),
        // Password: os.Getenv("ES_PASSWORD"),
    }

    client, err := elasticsearch.NewClient(cfg)
    if err != nil {
        return nil, err
    }

    return &ElasticsearchClient{
        client: client,
        index:  index,
    }, nil
}

// QueryLogs busca logs com filtros
func (es *ElasticsearchClient) QueryLogs(ctx context.Context, query LogQuery) (*LogResult, error) {
    // Construir query DSL
    body := map[string]interface{}{
        "query": map[string]interface{}{
            "bool": map[string]interface{}{
                "must": buildMustClauses(query),
                "filter": buildFilterClauses(query),
            },
        },
        "sort": []map[string]interface{}{
            {"@timestamp": map[string]string{"order": "desc"}},
        },
        "size": query.Size,
        "from": query.From,
    }

    // Adicionar highlight para termos de busca
    if query.SearchTerm != "" {
        body["highlight"] = map[string]interface{}{
            "fields": map[string]interface{}{
                "message": map[string]interface{}{},
                "log":     map[string]interface{}{},
            },
            "pre_tags":  []string{"<mark>"},
            "post_tags": []string{"</mark>"},
        }
    }

    bodyJSON, _ := json.Marshal(body)

    // Executar query
    req := esapi.SearchRequest{
        Index: []string{es.index},
        Body:  strings.NewReader(string(bodyJSON)),
    }

    res, err := req.Do(ctx, es.client)
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()

    if res.IsError() {
        return nil, fmt.Errorf("elasticsearch error: %s", res.Status())
    }

    // Parse resultado
    var result LogResult
    if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
        return nil, err
    }

    return &result, nil
}

// buildMustClauses constrói cláusulas obrigatórias
func buildMustClauses(query LogQuery) []map[string]interface{} {
    must := []map[string]interface{}{}

    // Busca por termo
    if query.SearchTerm != "" {
        must = append(must, map[string]interface{}{
            "multi_match": map[string]interface{}{
                "query":  query.SearchTerm,
                "fields": []string{"message", "log", "kubernetes.container_name"},
                "type":   "phrase_prefix",
            },
        })
    }

    // Namespace
    if query.Namespace != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "kubernetes.namespace_name.keyword": query.Namespace,
            },
        })
    }

    // Pod name
    if query.PodName != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "kubernetes.pod_name.keyword": query.PodName,
            },
        })
    }

    // Container
    if query.ContainerName != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "kubernetes.container_name.keyword": query.ContainerName,
            },
        })
    }

    // Log level
    if query.Level != "" {
        must = append(must, map[string]interface{}{
            "term": map[string]string{
                "level.keyword": query.Level,
            },
        })
    }

    return must
}

// buildFilterClauses constrói filtros de tempo
func buildFilterClauses(query LogQuery) []map[string]interface{} {
    filter := []map[string]interface{}{}

    // Time range
    if !query.StartTime.IsZero() || !query.EndTime.IsZero() {
        rangeFilter := map[string]interface{}{
            "range": map[string]interface{}{
                "@timestamp": map[string]interface{}{},
            },
        }

        if !query.StartTime.IsZero() {
            rangeFilter["range"].(map[string]interface{})["@timestamp"].(map[string]interface{})["gte"] = query.StartTime.Format(time.RFC3339)
        }
        if !query.EndTime.IsZero() {
            rangeFilter["range"].(map[string]interface{})["@timestamp"].(map[string]interface{})["lte"] = query.EndTime.Format(time.RFC3339)
        }

        filter = append(filter, rangeFilter)
    }

    return filter
}

// GetLogPatterns analisa logs para encontrar padrões comuns (erros, warnings)
func (es *ElasticsearchClient) GetLogPatterns(ctx context.Context, query LogQuery) (*PatternResult, error) {
    body := map[string]interface{}{
        "query": map[string]interface{}{
            "bool": map[string]interface{}{
                "must":   buildMustClauses(query),
                "filter": buildFilterClauses(query),
            },
        },
        "size": 0, // Não queremos hits, apenas aggregations
        "aggs": map[string]interface{}{
            "error_patterns": map[string]interface{}{
                "terms": map[string]interface{}{
                    "field": "message.keyword",
                    "size":  10,
                    "include": ".*error.*|.*failed.*|.*exception.*",
                },
            },
            "log_levels": map[string]interface{}{
                "terms": map[string]interface{}{
                    "field": "level.keyword",
                },
            },
        },
    }

    bodyJSON, _ := json.Marshal(body)

    req := esapi.SearchRequest{
        Index: []string{es.index},
        Body:  strings.NewReader(string(bodyJSON)),
    }

    res, err := req.Do(ctx, es.client)
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()

    var result PatternResult
    if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
        return nil, err
    }

    return &result, nil
}
```

---

### Backend: API Handlers

```go
// internal/web/handlers/logs.go
package handlers

import (
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    "k8s-hpa-manager/internal/logs"
)

type LogsHandler struct {
    esClient *logs.ElasticsearchClient
}

func NewLogsHandler(esClient *logs.ElasticsearchClient) *LogsHandler {
    return &LogsHandler{esClient: esClient}
}

// QueryLogs - GET /api/v1/logs/query
func (h *LogsHandler) QueryLogs(c *gin.Context) {
    var req logs.LogQuery

    // Parse query params
    req.Namespace = c.Query("namespace")
    req.PodName = c.Query("pod")
    req.ContainerName = c.Query("container")
    req.Level = c.Query("level")
    req.SearchTerm = c.Query("q")
    req.Size = parseIntOrDefault(c.Query("size"), 100)
    req.From = parseIntOrDefault(c.Query("from"), 0)

    // Parse time range
    if startStr := c.Query("start"); startStr != "" {
        if t, err := time.Parse(time.RFC3339, startStr); err == nil {
            req.StartTime = t
        }
    }
    if endStr := c.Query("end"); endStr != "" {
        if t, err := time.Parse(time.RFC3339, endStr); err == nil {
            req.EndTime = t
        }
    }

    // Executar query
    result, err := h.esClient.QueryLogs(c.Request.Context(), req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, result)
}

// GetPodLogs - GET /api/v1/logs/pods/:cluster/:namespace/:pod
func (h *LogsHandler) GetPodLogs(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    pod := c.Param("pod")

    // Query específica para o pod
    query := logs.LogQuery{
        Cluster:   cluster,
        Namespace: namespace,
        PodName:   pod,
        Size:      500,
        StartTime: time.Now().Add(-1 * time.Hour), // Última hora
    }

    result, err := h.esClient.QueryLogs(c.Request.Context(), query)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, result)
}

// GetErrorPatterns - GET /api/v1/logs/patterns
func (h *LogsHandler) GetErrorPatterns(c *gin.Context) {
    var req logs.LogQuery

    req.Namespace = c.Query("namespace")
    req.Level = "ERROR" // Apenas erros
    req.StartTime = time.Now().Add(-24 * time.Hour) // Últimas 24h

    patterns, err := h.esClient.GetLogPatterns(c.Request.Context(), req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, patterns)
}

// StreamLogs - GET /api/v1/logs/stream (SSE)
func (h *LogsHandler) StreamLogs(c *gin.Context) {
    // Implementar streaming de logs em tempo real
    // Similar ao sistema SSE já existente para Cordon/Drain
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    // TODO: Implementar scroll query do Elasticsearch
    // para streaming contínuo de logs
}
```

---

### Frontend: Componente LogsViewer

```typescript
// internal/web/frontend/src/components/LogsViewer.tsx
import { useState, useEffect } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Search, Filter, Download, RefreshCw } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { format } from "date-fns";

interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
  kubernetes: {
    namespace: string;
    pod_name: string;
    container_name: string;
  };
}

interface LogsViewerProps {
  cluster?: string;
  namespace?: string;
  podName?: string;
  containerName?: string;
}

export const LogsViewer = ({ cluster, namespace, podName, containerName }: LogsViewerProps) => {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [levelFilter, setLevelFilter] = useState<string>("ALL");
  const [timeRange, setTimeRange] = useState<string>("1h");

  // Fetch logs
  const fetchLogs = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (namespace) params.append("namespace", namespace);
      if (podName) params.append("pod", podName);
      if (containerName) params.append("container", containerName);
      if (searchTerm) params.append("q", searchTerm);
      if (levelFilter !== "ALL") params.append("level", levelFilter);

      // Calcular time range
      const end = new Date();
      const start = new Date(end.getTime() - parseTimeRange(timeRange));
      params.append("start", start.toISOString());
      params.append("end", end.toISOString());

      const response = await apiClient.get(`/api/v1/logs/query?${params}`);
      setLogs(response.data.hits.hits.map((hit: any) => hit._source));
    } catch (error) {
      console.error("Error fetching logs:", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchLogs();
  }, [namespace, podName, containerName, levelFilter, timeRange]);

  return (
    <div className="flex flex-col h-full">
      {/* Filtros */}
      <div className="flex gap-2 p-4 border-b">
        <div className="flex-1">
          <Input
            placeholder="Buscar nos logs..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && fetchLogs()}
            icon={<Search className="w-4 h-4" />}
          />
        </div>

        <Select value={levelFilter} onValueChange={setLevelFilter}>
          <option value="ALL">Todos os níveis</option>
          <option value="ERROR">ERROR</option>
          <option value="WARN">WARN</option>
          <option value="INFO">INFO</option>
          <option value="DEBUG">DEBUG</option>
        </Select>

        <Select value={timeRange} onValueChange={setTimeRange}>
          <option value="5m">Últimos 5 min</option>
          <option value="15m">Últimos 15 min</option>
          <option value="1h">Última 1 hora</option>
          <option value="6h">Últimas 6 horas</option>
          <option value="24h">Últimas 24 horas</option>
        </Select>

        <Button onClick={fetchLogs} disabled={loading}>
          <RefreshCw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
        </Button>

        <Button variant="outline" onClick={() => exportLogs(logs)}>
          <Download className="w-4 h-4" />
        </Button>
      </div>

      {/* Lista de logs */}
      <div className="flex-1 overflow-auto p-4 bg-black/5 dark:bg-black/20 font-mono text-sm">
        {logs.length === 0 ? (
          <div className="text-center text-muted-foreground py-8">
            Nenhum log encontrado
          </div>
        ) : (
          logs.map((log, idx) => (
            <LogLine key={idx} log={log} searchTerm={searchTerm} />
          ))
        )}
      </div>

      {/* Footer com stats */}
      <div className="p-2 border-t text-sm text-muted-foreground flex gap-4">
        <span>{logs.length} logs carregados</span>
        <span>•</span>
        <span>Período: {timeRange}</span>
      </div>
    </div>
  );
};

// Componente para uma linha de log
const LogLine = ({ log, searchTerm }: { log: LogEntry; searchTerm: string }) => {
  const levelColors = {
    ERROR: "text-red-500",
    WARN: "text-yellow-500",
    INFO: "text-blue-500",
    DEBUG: "text-gray-500",
  };

  const highlightText = (text: string, term: string) => {
    if (!term) return text;
    const parts = text.split(new RegExp(`(${term})`, "gi"));
    return parts.map((part, i) =>
      part.toLowerCase() === term.toLowerCase() ?
        <mark key={i} className="bg-yellow-300 dark:bg-yellow-600">{part}</mark> : part
    );
  };

  return (
    <div className="hover:bg-accent/50 px-2 py-1 rounded">
      <span className="text-muted-foreground mr-2">
        {format(new Date(log.timestamp), "HH:mm:ss.SSS")}
      </span>
      <Badge variant="outline" className={levelColors[log.level] || ""}>
        {log.level}
      </Badge>
      <span className="text-muted-foreground ml-2 mr-2">
        [{log.kubernetes.pod_name}/{log.kubernetes.container_name}]
      </span>
      <span>{highlightText(log.message, searchTerm)}</span>
    </div>
  );
};

// Helper para exportar logs
const exportLogs = (logs: LogEntry[]) => {
  const content = logs.map(log =>
    `[${log.timestamp}] [${log.level}] [${log.kubernetes.pod_name}] ${log.message}`
  ).join("\n");

  const blob = new Blob([content], { type: "text/plain" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `logs-${Date.now()}.txt`;
  a.click();
};

// Helper para parsear time range
const parseTimeRange = (range: string): number => {
  const value = parseInt(range);
  const unit = range.slice(-1);

  switch (unit) {
    case "m": return value * 60 * 1000;
    case "h": return value * 60 * 60 * 1000;
    default: return 60 * 60 * 1000; // default 1h
  }
};
```

---

## 🎨 UI/UX: Integração com Abas Existentes

### 1. **Tab "Pods" - Botão "View Logs"**

```typescript
// Em cada PodListItem, adicionar botão:
<Button
  variant="outline"
  size="sm"
  onClick={() => openLogsModal(pod)}
>
  📜 Logs
</Button>

// Modal de logs abre com filtros pré-aplicados:
<LogsViewer
  cluster={pod.cluster}
  namespace={pod.namespace}
  podName={pod.name}
/>
```

### 2. **Monitoring - Correlação de Logs com Métricas**

```typescript
// Em MonitoringPage, adicionar painel de logs abaixo do gráfico:
<div className="grid grid-cols-2 gap-4">
  <div>
    <h3>Métricas</h3>
    <MetricsChart hpa={selectedHPA} />
  </div>
  <div>
    <h3>Logs Recentes</h3>
    <LogsViewer
      namespace={selectedHPA.namespace}
      podName={selectedHPA.name}
      timeRange="1h"
      levelFilter="ERROR"
    />
  </div>
</div>
```

### 3. **Alertas - Logs Contextuais**

```typescript
// Em AlertsDialog, adicionar seção de logs:
<AlertsDialog alert={alert}>
  <Tabs>
    <Tab label="Detalhes">...</Tab>
    <Tab label="Logs">
      <LogsViewer
        namespace={alert.namespace}
        podName={alert.pod_name}
        timeRange="30m"
        searchTerm="error|exception|failed"
      />
    </Tab>
  </Tabs>
</AlertsDialog>
```

---

## 📦 Configuração

### Variáveis de Ambiente

```bash
# .env
ELASTICSEARCH_URL=https://elasticsearch.company.com:9200
ELASTICSEARCH_USERNAME=k8s-hpa-manager
ELASTICSEARCH_PASSWORD=secret
ELASTICSEARCH_INDEX_PATTERN=k8s-logs-*
ELASTICSEARCH_TIMEOUT=30s

# Se usar Kibana para visualizações avançadas
KIBANA_URL=https://kibana.company.com
```

### Inicialização do Cliente

```go
// cmd/root.go ou internal/web/server.go
import "k8s-hpa-manager/internal/logs"

func initElasticsearch() *logs.ElasticsearchClient {
    addresses := []string{os.Getenv("ELASTICSEARCH_URL")}
    index := os.Getenv("ELASTICSEARCH_INDEX_PATTERN")

    client, err := logs.NewElasticsearchClient(addresses, index)
    if err != nil {
        log.Fatalf("Failed to initialize Elasticsearch: %v", err)
    }

    return client
}
```

---

## 🚀 Plano de Implementação

### Fase 1: Backend (3-4 dias)
- ✅ Criar `internal/logs/elasticsearch.go`
- ✅ Implementar `ElasticsearchClient` com query builder
- ✅ Criar handlers `/api/v1/logs/*`
- ✅ Adicionar testes unitários
- ✅ Documentar API com exemplos

### Fase 2: Frontend Básico (2-3 dias)
- ✅ Criar componente `<LogsViewer>`
- ✅ Implementar filtros (namespace, pod, level, time range)
- ✅ Syntax highlighting de logs
- ✅ Export de logs (txt/json)

### Fase 3: Integrações (2-3 dias)
- ✅ Adicionar botão "View Logs" em Pods
- ✅ Correlação de logs em Monitoring
- ✅ Logs contextuais em Alertas
- ✅ Quick link para Kibana (externo)

### Fase 4: Features Avançadas (3-4 dias)
- ✅ Log patterns detection (erros comuns)
- ✅ Streaming de logs em tempo real (SSE)
- ✅ Filtros avançados (regex, exclude, etc.)
- ✅ Bookmarks de queries frequentes

---

## 📊 Métricas de Sucesso

**Antes da integração:**
- Tempo médio de troubleshooting: ~15-20 minutos
- Troca de ferramentas: 3-5 vezes (new-k8s-hpa → Kibana → Grafana)

**Após integração:**
- Tempo médio de troubleshooting: ~5-8 minutos (-60%)
- Troca de ferramentas: 0-1 vez
- Correlação automática de eventos: ✅

---

## ⚠️ Considerações Importantes

### Performance
- ✅ **Cache de queries** - Evitar queries repetidas ao ES
- ✅ **Paginação** - Limitar resultados (100-500 logs por vez)
- ✅ **Índices otimizados** - Usar data streams do Elasticsearch
- ✅ **Timeout adequado** - 30s para queries complexas

### Segurança
- ✅ **Autenticação** - Usar credenciais read-only para ES
- ✅ **RBAC** - Respeitar permissões de namespace do usuário
- ✅ **Logs sensíveis** - Não expor secrets/passwords em logs
- ✅ **Rate limiting** - Prevenir abuse de queries

### Custos
- ⚠️ **Storage** - Logs consomem bastante espaço
- ⚠️ **Queries** - Queries pesadas podem impactar ES
- ✅ **Retention** - Configurar retenção adequada (30-90 dias)

---

## 🎁 Bonus: Features Futuras

### 1. **Log Anomaly Detection**
- ML para detectar padrões anormais em logs
- Alertas proativos antes de falhas

### 2. **Log Correlation com Traces**
- Integrar com Jaeger/Zipkin
- Trace ID → Logs relacionados

### 3. **Log Replay**
- "Replay" de logs de incidentes passados
- Análise post-mortem facilitada

### 4. **Chat Ops**
- Comandos Slack para buscar logs
- Notificações de erros críticos

---

## ✅ Recomendação Final

**SIM, integre com ELK Stack!**

**Prioridade:** 🥇 **ALTA** (v1.5.0)

**Motivos:**
1. ✅ Complementa perfeitamente as funcionalidades atuais
2. ✅ Reduz tempo de troubleshooting significativamente
3. ✅ Unifica ferramentas (menos context switching)
4. ✅ Implementação relativamente simples (10-12 dias)
5. ✅ Alto valor percebido pelos usuários

**Começar por:**
1. Fase 1 (Backend) - Base sólida
2. Fase 2 (LogsViewer básico) - MVP funcional
3. Fase 3 (Integrações) - Quick wins visíveis

---

**Quer que eu comece a implementação? Por qual fase começamos?** 🚀
