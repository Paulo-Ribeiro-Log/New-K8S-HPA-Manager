# 🕸️ Integração Istio/Kiali - Planejamento Completo

**Data**: 2025-12-09
**Versão**: 1.0
**Status**: 📋 Planejamento (Aguardando implementação)

---

## 🎯 Objetivo

Integrar o **Service Mesh do Istio** e o **dashboard Kiali** ao K8s HPA Manager para visualizar:
- Grafo de dependências entre services
- Métricas de tráfego (request rate, latency, errors)
- Status de saúde dos services
- Traffic flow entre aplicações

---

## 🏗️ Arquitetura Istio + Kiali

### O que é Istio?
**Istio** é um service mesh que adiciona observabilidade, segurança e controle de tráfego para microservices Kubernetes através de sidecars Envoy.

### O que é Kiali?
**Kiali** é o dashboard oficial do Istio que visualiza o service mesh através de:
- Grafo de topologia (service graph)
- Métricas de tráfego (via Prometheus)
- Traces distribuídos (via Jaeger)
- Configurações Istio (VirtualServices, DestinationRules, etc)

### Como Funcionam Juntos?
```
┌──────────────────────────────────────────────────────────┐
│                     Kubernetes Cluster                   │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌─────────────┐        ┌─────────────┐                │
│  │   Pod A     │────────│   Pod B     │                │
│  │ [App] [Envoy]       │ [App] [Envoy]                │
│  └─────────────┘        └─────────────┘                │
│         │                       │                        │
│         └───────┬───────────────┘                        │
│                 ↓                                        │
│         ┌───────────────┐                                │
│         │  Istio Pilot  │ (Control Plane)               │
│         └───────┬───────┘                                │
│                 ↓                                        │
│         ┌───────────────┐                                │
│         │  Prometheus   │ (Coleta métricas Envoy)       │
│         └───────┬───────┘                                │
│                 ↓                                        │
│         ┌───────────────┐                                │
│         │    Kiali      │ (API + Dashboard)             │
│         └───────┬───────┘                                │
│                 ↓                                        │
│         ┌───────────────┐                                │
│         │ K8s HPA Mgr   │ (Nossa aplicação)             │
│         └───────────────┘                                │
└──────────────────────────────────────────────────────────┘
```

---

## 📊 Métricas do Istio

### 1. Request Metrics (HTTP)
Coletadas pelo Envoy sidecar de cada pod:

```promql
istio_requests_total{
  reporter="destination",
  source_workload="frontend",
  source_namespace="production",
  destination_workload="backend-api",
  destination_namespace="production",
  response_code="200",
  connection_security_policy="mutual_tls"
}
```

**Labels importantes:**
- `source_workload` - Service que fez o request
- `destination_workload` - Service que recebeu o request
- `response_code` - HTTP status code
- `connection_security_policy` - mTLS habilitado?

### 2. Request Duration (Latency)
```promql
istio_request_duration_milliseconds_bucket{
  reporter="destination",
  source_workload="frontend",
  destination_workload="backend-api"
}
```

**Histograms para P50, P95, P99:**
```promql
histogram_quantile(0.50, sum(rate(istio_request_duration_milliseconds_bucket[5m])) by (le, destination_workload))
histogram_quantile(0.95, ...)
histogram_quantile(0.99, ...)
```

### 3. Request/Response Size
```promql
istio_request_bytes_sum
istio_response_bytes_sum
```

### 4. TCP Metrics (para non-HTTP traffic)
```promql
istio_tcp_sent_bytes_total
istio_tcp_received_bytes_total
istio_tcp_connections_opened_total
istio_tcp_connections_closed_total
```

---

## 🌐 API do Kiali

### Endpoints Principais

#### 1. **Service Graph**
```http
GET /api/namespaces/{namespace}/graph
```

**Query Parameters:**
- `duration` - Período de tempo (ex: `60s`, `5m`, `1h`)
- `graphType` - Tipo de grafo: `workload`, `app`, `versionedApp`, `service`
- `includeIdleEdges` - Incluir edges sem tráfego recente

**Response Example:**
```json
{
  "timestamp": 1733759841,
  "duration": 60,
  "graphType": "workload",
  "elements": {
    "nodes": [
      {
        "data": {
          "id": "workload_production_frontend_v1",
          "nodeType": "workload",
          "namespace": "production",
          "workload": "frontend",
          "app": "frontend",
          "version": "v1",
          "traffic": [
            {
              "protocol": "http",
              "rates": {
                "httpIn": "150.5",
                "httpOut": "150.5"
              }
            }
          ],
          "health": {
            "workloadStatus": {
              "name": "frontend",
              "desiredReplicas": 3,
              "currentReplicas": 3,
              "availableReplicas": 3
            },
            "requests": {
              "errorRatio": 0.002,
              "inboundErrorRatio": 0.002,
              "outboundErrorRatio": 0.0
            }
          }
        }
      }
    ],
    "edges": [
      {
        "data": {
          "id": "edge_frontend_backend",
          "source": "workload_production_frontend_v1",
          "target": "workload_production_backend-api_v1",
          "traffic": {
            "protocol": "http",
            "rates": {
              "http": "150.5",
              "httpPercentReq": "100.0"
            },
            "responses": {
              "200": {
                "flags": {},
                "hosts": {}
              }
            }
          }
        }
      }
    ]
  }
}
```

#### 2. **Workload Metrics**
```http
GET /api/namespaces/{namespace}/workloads/{workload}/metrics
```

**Query Parameters:**
- `direction` - `inbound` ou `outbound`
- `duration` - Período de tempo
- `step` - Intervalo entre pontos (ex: `15s`)
- `rateInterval` - Janela para cálculo de rate (ex: `1m`)
- `filters[]` - Métricas desejadas: `request_count`, `request_duration`, `request_error_count`

**Response Example:**
```json
{
  "request_count": {
    "matrix": [
      {
        "metric": {
          "reporter": "destination",
          "response_code": "200"
        },
        "values": [
          [1733759600, "150.5"],
          [1733759615, "152.3"],
          [1733759630, "148.7"]
        ]
      }
    ]
  },
  "request_duration": {
    "matrix": [
      {
        "metric": {
          "reporter": "destination"
        },
        "values": [
          [1733759600, "0.025"],
          [1733759615, "0.028"],
          [1733759630, "0.023"]
        ]
      }
    ]
  }
}
```

#### 3. **Namespace Health**
```http
GET /api/namespaces/{namespace}/health
```

**Response Example:**
```json
{
  "namespace": "production",
  "workloadStatuses": [
    {
      "name": "frontend",
      "desiredReplicas": 3,
      "currentReplicas": 3,
      "availableReplicas": 3,
      "syncedProxies": 3
    },
    {
      "name": "backend-api",
      "desiredReplicas": 5,
      "currentReplicas": 5,
      "availableReplicas": 4,
      "syncedProxies": 5
    }
  ],
  "requests": {
    "inbound": {
      "http": {
        "200": 95.5,
        "500": 4.5
      }
    },
    "outbound": {
      "http": {
        "200": 98.2,
        "404": 1.8
      }
    },
    "healthAnnotations": {}
  }
}
```

#### 4. **Istio Config Validation**
```http
GET /api/namespaces/{namespace}/istio
```

Lista VirtualServices, DestinationRules, Gateways, etc com validação.

---

## 🔧 Implementação Backend (Go)

### 1. Estrutura de Diretórios

```
internal/
├── istio/
│   ├── client.go          # Cliente HTTP para API do Kiali
│   ├── types.go           # Structs para response da API
│   └── graph.go           # Parsing do grafo
└── web/
    └── handlers/
        └── istio.go       # Handlers HTTP para frontend
```

### 2. Cliente Kiali (`internal/istio/client.go`)

```go
package istio

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type KialiClient struct {
    baseURL    string
    httpClient *http.Client
    token      string // Se autenticação for necessária
}

// NewKialiClient cria cliente para API do Kiali
func NewKialiClient(baseURL string, token string) *KialiClient {
    return &KialiClient{
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
        token: token,
    }
}

// GetServiceGraph retorna grafo de services de um namespace
func (c *KialiClient) GetServiceGraph(ctx context.Context, namespace string, duration string, graphType string) (*GraphResponse, error) {
    url := fmt.Sprintf("%s/api/namespaces/%s/graph?duration=%s&graphType=%s",
        c.baseURL, namespace, duration, graphType)

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    // Adicionar token se necessário
    if c.token != "" {
        req.Header.Set("Authorization", "Bearer "+c.token)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to call Kiali API: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("Kiali API returned status %d: %s", resp.StatusCode, string(body))
    }

    var graph GraphResponse
    if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    return &graph, nil
}

// GetWorkloadMetrics retorna métricas de um workload
func (c *KialiClient) GetWorkloadMetrics(ctx context.Context, namespace, workload string, params MetricsParams) (*MetricsResponse, error) {
    url := fmt.Sprintf("%s/api/namespaces/%s/workloads/%s/metrics?duration=%s&step=%s&direction=%s&filters[]=%s",
        c.baseURL, namespace, workload, params.Duration, params.Step, params.Direction, params.Filters)

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    if c.token != "" {
        req.Header.Set("Authorization", "Bearer "+c.token)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("Kiali API returned status %d: %s", resp.StatusCode, string(body))
    }

    var metrics MetricsResponse
    if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
        return nil, err
    }

    return &metrics, nil
}

// GetNamespaceHealth retorna status de saúde do namespace
func (c *KialiClient) GetNamespaceHealth(ctx context.Context, namespace string) (*NamespaceHealth, error) {
    url := fmt.Sprintf("%s/api/namespaces/%s/health", c.baseURL, namespace)

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    if c.token != "" {
        req.Header.Set("Authorization", "Bearer "+c.token)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("Kiali API returned status %d: %s", resp.StatusCode, string(body))
    }

    var health NamespaceHealth
    if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
        return nil, err
    }

    return &health, nil
}
```

### 3. Types (`internal/istio/types.go`)

```go
package istio

// GraphResponse representa o grafo de services retornado pelo Kiali
type GraphResponse struct {
    Timestamp int64        `json:"timestamp"`
    Duration  int          `json:"duration"`
    GraphType string       `json:"graphType"`
    Elements  GraphElements `json:"elements"`
}

type GraphElements struct {
    Nodes []*GraphNode `json:"nodes"`
    Edges []*GraphEdge `json:"edges"`
}

type GraphNode struct {
    Data NodeData `json:"data"`
}

type NodeData struct {
    ID        string      `json:"id"`
    NodeType  string      `json:"nodeType"`  // "workload", "app", "service"
    Namespace string      `json:"namespace"`
    Workload  string      `json:"workload"`
    App       string      `json:"app"`
    Version   string      `json:"version"`
    Traffic   []Traffic   `json:"traffic"`
    Health    *NodeHealth `json:"health,omitempty"`
}

type Traffic struct {
    Protocol string           `json:"protocol"`
    Rates    map[string]string `json:"rates"` // httpIn, httpOut, tcpIn, tcpOut
}

type NodeHealth struct {
    WorkloadStatus WorkloadStatus `json:"workloadStatus"`
    Requests       RequestHealth  `json:"requests"`
}

type WorkloadStatus struct {
    Name              string `json:"name"`
    DesiredReplicas   int    `json:"desiredReplicas"`
    CurrentReplicas   int    `json:"currentReplicas"`
    AvailableReplicas int    `json:"availableReplicas"`
}

type RequestHealth struct {
    ErrorRatio         float64 `json:"errorRatio"`
    InboundErrorRatio  float64 `json:"inboundErrorRatio"`
    OutboundErrorRatio float64 `json:"outboundErrorRatio"`
}

type GraphEdge struct {
    Data EdgeData `json:"data"`
}

type EdgeData struct {
    ID      string       `json:"id"`
    Source  string       `json:"source"`
    Target  string       `json:"target"`
    Traffic EdgeTraffic  `json:"traffic"`
}

type EdgeTraffic struct {
    Protocol  string                     `json:"protocol"`
    Rates     map[string]string          `json:"rates"`     // http, tcp
    Responses map[string]ResponseDetails `json:"responses"` // "200", "500", etc
}

type ResponseDetails struct {
    Flags map[string]interface{} `json:"flags"`
    Hosts map[string]interface{} `json:"hosts"`
}

// MetricsParams parâmetros para consulta de métricas
type MetricsParams struct {
    Duration string   // "60s", "5m", "1h"
    Step     string   // "15s"
    Direction string  // "inbound", "outbound"
    Filters  string   // "request_count,request_duration,request_error_count"
}

// MetricsResponse métricas retornadas pelo Kiali
type MetricsResponse struct {
    RequestCount    *MetricMatrix `json:"request_count,omitempty"`
    RequestDuration *MetricMatrix `json:"request_duration,omitempty"`
    RequestErrors   *MetricMatrix `json:"request_error_count,omitempty"`
}

type MetricMatrix struct {
    Matrix []MetricSeries `json:"matrix"`
}

type MetricSeries struct {
    Metric map[string]string `json:"metric"`
    Values [][]interface{}   `json:"values"` // [timestamp, value]
}

// NamespaceHealth status de saúde do namespace
type NamespaceHealth struct {
    Namespace        string            `json:"namespace"`
    WorkloadStatuses []WorkloadStatus  `json:"workloadStatuses"`
    Requests         NamespaceRequests `json:"requests"`
}

type NamespaceRequests struct {
    Inbound  RequestStats `json:"inbound"`
    Outbound RequestStats `json:"outbound"`
}

type RequestStats struct {
    HTTP map[string]float64 `json:"http"` // "200": 95.5, "500": 4.5
}
```

### 4. Handlers (`internal/web/handlers/istio.go`)

```go
package handlers

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/rs/zerolog/log"

    "k8s-hpa-manager/internal/istio"
)

type IstioHandler struct {
    kialiClient *istio.KialiClient
}

// NewIstioHandler cria handler para endpoints Istio/Kiali
func NewIstioHandler(kialiBaseURL string, kialiToken string) *IstioHandler {
    return &IstioHandler{
        kialiClient: istio.NewKialiClient(kialiBaseURL, kialiToken),
    }
}

// GetServiceGraph retorna grafo de services do namespace
// GET /api/v1/istio/graph/:cluster/:namespace?duration=60s&graphType=workload
func (h *IstioHandler) GetServiceGraph(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    duration := c.DefaultQuery("duration", "60s")
    graphType := c.DefaultQuery("graphType", "workload")

    // Remove sufixo -admin
    normalizedCluster := strings.TrimSuffix(cluster, "-admin")

    log.Info().
        Str("cluster", normalizedCluster).
        Str("namespace", namespace).
        Str("duration", duration).
        Str("graphType", graphType).
        Msg("Buscando service graph do Kiali")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    graph, err := h.kialiClient.GetServiceGraph(ctx, namespace, duration, graphType)
    if err != nil {
        log.Error().
            Err(err).
            Str("cluster", normalizedCluster).
            Str("namespace", namespace).
            Msg("Erro ao buscar service graph do Kiali")

        c.JSON(500, gin.H{
            "error": fmt.Sprintf("Failed to fetch service graph: %v", err),
        })
        return
    }

    log.Info().
        Str("cluster", normalizedCluster).
        Str("namespace", namespace).
        Int("nodes", len(graph.Elements.Nodes)).
        Int("edges", len(graph.Elements.Edges)).
        Msg("Service graph obtido com sucesso")

    c.JSON(200, graph)
}

// GetWorkloadMetrics retorna métricas de um workload
// GET /api/v1/istio/metrics/:cluster/:namespace/:workload?duration=60s&direction=inbound
func (h *IstioHandler) GetWorkloadMetrics(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    workload := c.Param("workload")

    params := istio.MetricsParams{
        Duration:  c.DefaultQuery("duration", "60s"),
        Step:      c.DefaultQuery("step", "15s"),
        Direction: c.DefaultQuery("direction", "inbound"),
        Filters:   "request_count,request_duration,request_error_count",
    }

    normalizedCluster := strings.TrimSuffix(cluster, "-admin")

    log.Info().
        Str("cluster", normalizedCluster).
        Str("namespace", namespace).
        Str("workload", workload).
        Msg("Buscando métricas do workload")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    metrics, err := h.kialiClient.GetWorkloadMetrics(ctx, namespace, workload, params)
    if err != nil {
        log.Error().
            Err(err).
            Str("workload", workload).
            Msg("Erro ao buscar métricas do workload")

        c.JSON(500, gin.H{
            "error": fmt.Sprintf("Failed to fetch workload metrics: %v", err),
        })
        return
    }

    c.JSON(200, metrics)
}

// GetNamespaceHealth retorna status de saúde do namespace
// GET /api/v1/istio/health/:cluster/:namespace
func (h *IstioHandler) GetNamespaceHealth(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")

    normalizedCluster := strings.TrimSuffix(cluster, "-admin")

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    health, err := h.kialiClient.GetNamespaceHealth(ctx, namespace)
    if err != nil {
        log.Error().
            Err(err).
            Str("namespace", namespace).
            Msg("Erro ao buscar health do namespace")

        c.JSON(500, gin.H{
            "error": fmt.Sprintf("Failed to fetch namespace health: %v", err),
        })
        return
    }

    c.JSON(200, health)
}
```

### 5. Registrar Rotas (`internal/web/server.go`)

```go
// Istio/Kiali endpoints
istioHandler := handlers.NewIstioHandler(
    os.Getenv("KIALI_URL"), // Ex: http://kiali.istio-system:20001
    os.Getenv("KIALI_TOKEN"),
)

api.GET("/istio/graph/:cluster/:namespace", istioHandler.GetServiceGraph)
api.GET("/istio/metrics/:cluster/:namespace/:workload", istioHandler.GetWorkloadMetrics)
api.GET("/istio/health/:cluster/:namespace", istioHandler.GetNamespaceHealth)
```

---

## 🎨 Implementação Frontend (React/TypeScript)

### 1. API Client (`internal/web/frontend/src/lib/api/client.ts`)

```typescript
// Adicionar ao apiClient existente

// Service Graph
async getServiceGraph(
  cluster: string,
  namespace: string,
  duration: string = "60s",
  graphType: string = "workload"
): Promise<ServiceGraph> {
  const response = await this.request(
    `/istio/graph/${cluster}/${namespace}?duration=${duration}&graphType=${graphType}`
  );
  return response;
}

// Workload Metrics
async getWorkloadMetrics(
  cluster: string,
  namespace: string,
  workload: string,
  duration: string = "60s",
  direction: string = "inbound"
): Promise<WorkloadMetrics> {
  const response = await this.request(
    `/istio/metrics/${cluster}/${namespace}/${workload}?duration=${duration}&direction=${direction}`
  );
  return response;
}

// Namespace Health
async getNamespaceHealth(
  cluster: string,
  namespace: string
): Promise<NamespaceHealth> {
  const response = await this.request(`/istio/health/${cluster}/${namespace}`);
  return response;
}
```

### 2. Types (`internal/web/frontend/src/lib/api/types.ts`)

```typescript
// Service Graph Types
export interface ServiceGraph {
  timestamp: number;
  duration: number;
  graphType: string;
  elements: {
    nodes: GraphNode[];
    edges: GraphEdge[];
  };
}

export interface GraphNode {
  data: {
    id: string;
    nodeType: string;
    namespace: string;
    workload: string;
    app: string;
    version: string;
    traffic: {
      protocol: string;
      rates: Record<string, string>;
    }[];
    health?: {
      workloadStatus: {
        name: string;
        desiredReplicas: number;
        currentReplicas: number;
        availableReplicas: number;
      };
      requests: {
        errorRatio: number;
        inboundErrorRatio: number;
        outboundErrorRatio: number;
      };
    };
  };
}

export interface GraphEdge {
  data: {
    id: string;
    source: string;
    target: string;
    traffic: {
      protocol: string;
      rates: Record<string, string>;
      responses: Record<string, any>;
    };
  };
}

// Workload Metrics Types
export interface WorkloadMetrics {
  request_count?: {
    matrix: MetricSeries[];
  };
  request_duration?: {
    matrix: MetricSeries[];
  };
  request_error_count?: {
    matrix: MetricSeries[];
  };
}

export interface MetricSeries {
  metric: Record<string, string>;
  values: [number, string][]; // [timestamp, value]
}

// Namespace Health Types
export interface NamespaceHealth {
  namespace: string;
  workloadStatuses: {
    name: string;
    desiredReplicas: number;
    currentReplicas: number;
    availableReplicas: number;
  }[];
  requests: {
    inbound: {
      http: Record<string, number>;
    };
    outbound: {
      http: Record<string, number>;
    };
  };
}
```

### 3. Custom Hook (`internal/web/frontend/src/hooks/useServiceMesh.ts`)

```typescript
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { ServiceGraph, WorkloadMetrics, NamespaceHealth } from "@/lib/api/types";

export const useServiceGraph = (
  cluster: string,
  namespace: string,
  duration: string = "60s",
  graphType: string = "workload"
) => {
  return useQuery<ServiceGraph>({
    queryKey: ["serviceGraph", cluster, namespace, duration, graphType],
    queryFn: () => apiClient.getServiceGraph(cluster, namespace, duration, graphType),
    enabled: !!cluster && !!namespace,
    refetchInterval: 15000, // Refresh a cada 15s
  });
};

export const useWorkloadMetrics = (
  cluster: string,
  namespace: string,
  workload: string,
  duration: string = "60s",
  direction: string = "inbound"
) => {
  return useQuery<WorkloadMetrics>({
    queryKey: ["workloadMetrics", cluster, namespace, workload, duration, direction],
    queryFn: () => apiClient.getWorkloadMetrics(cluster, namespace, workload, duration, direction),
    enabled: !!cluster && !!namespace && !!workload,
    refetchInterval: 30000, // Refresh a cada 30s
  });
};

export const useNamespaceHealth = (cluster: string, namespace: string) => {
  return useQuery<NamespaceHealth>({
    queryKey: ["namespaceHealth", cluster, namespace],
    queryFn: () => apiClient.getNamespaceHealth(cluster, namespace),
    enabled: !!cluster && !!namespace,
    refetchInterval: 30000,
  });
};
```

### 4. Componente Principal (`internal/web/frontend/src/components/ServiceMeshTab.tsx`)

```typescript
import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { RefreshCcw, Loader2 } from "lucide-react";
import { useServiceGraph, useNamespaceHealth } from "@/hooks/useServiceMesh";
import { ServiceMeshGraph } from "@/components/ServiceMeshGraph";
import { TrafficMetricsCards } from "@/components/TrafficMetricsCards";
import { WorkloadList } from "@/components/WorkloadList";

interface ServiceMeshTabProps {
  cluster: string;
  namespaces: string[];
}

export const ServiceMeshTab = ({ cluster, namespaces }: ServiceMeshTabProps) => {
  const [selectedNamespace, setSelectedNamespace] = useState<string>("");
  const [duration, setDuration] = useState<string>("60s");
  const [graphType, setGraphType] = useState<string>("workload");

  const { data: graph, isLoading: graphLoading, refetch: refetchGraph } = useServiceGraph(
    cluster,
    selectedNamespace,
    duration,
    graphType
  );

  const { data: health, isLoading: healthLoading } = useNamespaceHealth(
    cluster,
    selectedNamespace
  );

  return (
    <div className="flex flex-col gap-4 p-6">
      {/* Header com Controles */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">Service Mesh (Istio)</h2>
          <p className="text-muted-foreground">
            Visualize o grafo de dependências e métricas de tráfego
          </p>
        </div>

        <div className="flex items-center gap-2">
          {/* Seletor de Namespace */}
          <Select value={selectedNamespace} onValueChange={setSelectedNamespace}>
            <SelectTrigger className="w-64">
              <SelectValue placeholder="Selecione um namespace" />
            </SelectTrigger>
            <SelectContent>
              {namespaces.map((ns) => (
                <SelectItem key={ns} value={ns}>
                  {ns}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Seletor de Duração */}
          <Select value={duration} onValueChange={setDuration}>
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="60s">1 min</SelectItem>
              <SelectItem value="300s">5 min</SelectItem>
              <SelectItem value="900s">15 min</SelectItem>
              <SelectItem value="3600s">1 hora</SelectItem>
            </SelectContent>
          </Select>

          {/* Seletor de Tipo de Grafo */}
          <Select value={graphType} onValueChange={setGraphType}>
            <SelectTrigger className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="workload">Workload</SelectItem>
              <SelectItem value="app">App</SelectItem>
              <SelectItem value="service">Service</SelectItem>
            </SelectContent>
          </Select>

          {/* Botão Refresh */}
          <Button
            variant="outline"
            size="icon"
            onClick={() => refetchGraph()}
            disabled={graphLoading}
          >
            {graphLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCcw className="h-4 w-4" />
            )}
          </Button>
        </div>
      </div>

      {/* Conteúdo Principal */}
      {!selectedNamespace ? (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            Selecione um namespace para visualizar o service mesh
          </CardContent>
        </Card>
      ) : graphLoading || healthLoading ? (
        <Card>
          <CardContent className="py-12 flex items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
          </CardContent>
        </Card>
      ) : (
        <>
          {/* Cards de Métricas Gerais */}
          <TrafficMetricsCards graph={graph} health={health} />

          {/* Grafo de Service Mesh */}
          <ServiceMeshGraph graph={graph} />

          {/* Lista de Workloads com Métricas */}
          <WorkloadList
            cluster={cluster}
            namespace={selectedNamespace}
            workloads={graph?.elements.nodes || []}
          />
        </>
      )}
    </div>
  );
};
```

### 5. Componente de Grafo (`ServiceMeshGraph.tsx`)

Usar biblioteca **React Flow** ou **D3.js** para renderizar grafo interativo.

**Exemplo com React Flow:**

```typescript
import ReactFlow, {
  Node,
  Edge,
  Background,
  Controls,
  MiniMap
} from "reactflow";
import "reactflow/dist/style.css";

interface ServiceMeshGraphProps {
  graph: ServiceGraph;
}

export const ServiceMeshGraph = ({ graph }: ServiceMeshGraphProps) => {
  if (!graph) return null;

  // Converter nodes do Kiali para formato do React Flow
  const nodes: Node[] = graph.elements.nodes.map((node, index) => ({
    id: node.data.id,
    type: "custom",
    position: { x: index * 200, y: index * 100 }, // Layout básico
    data: {
      label: node.data.workload,
      namespace: node.data.namespace,
      health: node.data.health,
      traffic: node.data.traffic,
    },
  }));

  // Converter edges do Kiali para formato do React Flow
  const edges: Edge[] = graph.elements.edges.map((edge) => ({
    id: edge.data.id,
    source: edge.data.source,
    target: edge.data.target,
    label: `${edge.data.traffic.rates.http || "0"} req/s`,
    animated: true,
    style: {
      stroke: getEdgeColor(edge.data.traffic),
    },
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle>Service Graph</CardTitle>
        <CardDescription>
          Visualização de dependências entre services
        </CardDescription>
      </CardHeader>
      <CardContent className="h-[600px]">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          fitView
        >
          <Background />
          <Controls />
          <MiniMap />
        </ReactFlow>
      </CardContent>
    </Card>
  );
};

// Helper para determinar cor do edge baseado em erro rate
function getEdgeColor(traffic: any): string {
  const errorRatio = calculateErrorRatio(traffic.responses);

  if (errorRatio > 0.05) return "#ef4444"; // Vermelho (>5% errors)
  if (errorRatio > 0.01) return "#f97316"; // Laranja (>1% errors)
  return "#10b981"; // Verde (< 1% errors)
}
```

---

## 📦 Dependências

### Backend (Go)
```bash
# Já incluídas no projeto
net/http - Cliente HTTP nativo
encoding/json - Parsing JSON
context - Timeout e cancelamento
```

### Frontend (React/TypeScript)
```bash
npm install reactflow
npm install @tanstack/react-query  # Já instalado
npm install recharts              # Já instalado
```

---

## 🔐 Configuração e Segurança

### 1. Variáveis de Ambiente

```bash
# .env ou docker-compose.yml
KIALI_URL=http://kiali.istio-system:20001
KIALI_TOKEN=your-token-here  # Opcional se autenticação estiver desabilitada
```

### 2. Acesso ao Kiali

**Opção 1: Port-Forward (Dev)**
```bash
kubectl port-forward -n istio-system svc/kiali 20001:20001
```

**Opção 2: Service ClusterIP (Prod)**
```go
// Acessar via DNS interno do cluster
kialiURL := "http://kiali.istio-system.svc.cluster.local:20001"
```

**Opção 3: Ingress (Prod com auth)**
```yaml
# kiali-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kiali
  namespace: istio-system
  annotations:
    nginx.ingress.kubernetes.io/auth-type: basic
spec:
  rules:
  - host: kiali.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: kiali
            port:
              number: 20001
```

### 3. Autenticação

Se Kiali requer autenticação, obter token:

```bash
# Token do ServiceAccount do Kiali
kubectl get secret -n istio-system \
  $(kubectl get sa kiali-service-account -n istio-system -o jsonpath='{.secrets[0].name}') \
  -o jsonpath='{.data.token}' | base64 -d
```

---

## 🧪 Testes

### 1. Testar Cliente Kiali

```bash
# Health check
curl http://kiali.istio-system:20001/api/namespaces/production/health

# Service graph
curl "http://kiali.istio-system:20001/api/namespaces/production/graph?duration=60s&graphType=workload"
```

### 2. Testar Backend

```bash
# Service graph
curl http://localhost:8080/api/v1/istio/graph/cluster-name/production?duration=60s

# Workload metrics
curl http://localhost:8080/api/v1/istio/metrics/cluster-name/production/frontend?duration=300s

# Namespace health
curl http://localhost:8080/api/v1/istio/health/cluster-name/production
```

---

## 📊 Exemplo de Visualização Final

### Dashboard Service Mesh

```
┌─────────────────────────────────────────────────────────────┐
│ Service Mesh (Istio) - Namespace: production               │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
│ │Requests │  │Success  │  │ Errors  │  │Latency  │        │
│ │ 1.2k/s  │  │ 98.5%   │  │  1.5%   │  │  25ms   │        │
│ └─────────┘  └─────────┘  └─────────┘  └─────────┘        │
│                                                             │
│ ┌───────────────────────────────────────────────────────┐  │
│ │                  Service Graph                        │  │
│ │                                                       │  │
│ │     ┌──────────┐                                     │  │
│ │     │ Frontend │                                     │  │
│ │     └────┬─────┘                                     │  │
│ │          │ 150 req/s                                 │  │
│ │          │ 99.5% success                             │  │
│ │          ↓                                           │  │
│ │     ┌──────────┐                                     │  │
│ │     │Backend   │                                     │  │
│ │     │  API     │                                     │  │
│ │     └────┬─────┘                                     │  │
│ │          ├─→ Database (50 req/s)                     │  │
│ │          ├─→ Cache (80 req/s)                        │  │
│ │          └─→ External API (20 req/s)                 │  │
│ │                                                       │  │
│ └───────────────────────────────────────────────────────┘  │
│                                                             │
│ ┌───────────────────────────────────────────────────────┐  │
│ │ Workloads                                             │  │
│ ├─────────────┬──────────┬──────────┬─────────┬────────┤  │
│ │ Name        │Replicas  │Req/s     │Success  │Latency │  │
│ ├─────────────┼──────────┼──────────┼─────────┼────────┤  │
│ │ frontend    │ 3/3      │ 150.5    │ 99.5%   │ 12ms   │  │
│ │ backend-api │ 5/5      │ 150.5    │ 98.2%   │ 28ms   │  │
│ │ cache       │ 2/2      │ 80.3     │ 100%    │ 5ms    │  │
│ └─────────────┴──────────┴──────────┴─────────┴────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## ✅ Checklist de Implementação

### Backend
- [ ] Criar `internal/istio/client.go`
- [ ] Criar `internal/istio/types.go`
- [ ] Criar `internal/istio/graph.go`
- [ ] Criar `internal/web/handlers/istio.go`
- [ ] Registrar rotas em `internal/web/server.go`
- [ ] Adicionar variáveis de ambiente (KIALI_URL, KIALI_TOKEN)
- [ ] Testar endpoints com curl

### Frontend
- [ ] Adicionar types em `types.ts`
- [ ] Adicionar métodos no `apiClient`
- [ ] Criar hook `useServiceMesh.ts`
- [ ] Instalar `reactflow` via npm
- [ ] Criar `ServiceMeshTab.tsx`
- [ ] Criar `ServiceMeshGraph.tsx` (React Flow)
- [ ] Criar `TrafficMetricsCards.tsx`
- [ ] Criar `WorkloadList.tsx`
- [ ] Adicionar aba "Service Mesh" ao menu Workload ou criar aba separada
- [ ] Testar navegação e visualização

### Integração
- [ ] Configurar acesso ao Kiali (port-forward ou service URL)
- [ ] Testar autenticação (se necessário)
- [ ] Validar métricas retornadas
- [ ] Ajustar layout do grafo (auto-layout com dagre ou force-directed)
- [ ] Adicionar refresh automático (15s-30s)
- [ ] Implementar filtros (por namespace, workload, timerange)
- [ ] Adicionar tooltips no grafo
- [ ] Implementar click em node → mostrar detalhes

---

## 📚 Referências

- **Kiali API Documentation**: https://kiali.io/docs/api/
- **Istio Metrics**: https://istio.io/latest/docs/reference/config/metrics/
- **React Flow**: https://reactflow.dev/
- **Envoy Proxy Metrics**: https://www.envoyproxy.io/docs/envoy/latest/operations/stats_overview

---

## 🚀 Próximos Passos

1. **Revisar documentação** e validar requisitos
2. **Configurar acesso ao Kiali** (port-forward ou service URL)
3. **Testar API do Kiali** manualmente (curl)
4. **Implementar backend** (client + handlers)
5. **Implementar frontend** (components + hooks)
6. **Integrar na interface** (nova aba ou menu Workload)
7. **Testar end-to-end**
8. **Documentar uso** (adicionar ao CLAUDE.md)

---

**Documentação criada! Pronto para iniciar implementação quando aprovado.** ✅

---

[⬅️ Voltar ao CLAUDE.md principal](../../CLAUDE.md)
