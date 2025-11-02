# Plano de Integração: Alertmanager

**Documento:** Arquitetura e implementação da integração com Alertmanager do Prometheus Stack
**Data:** 02 de novembro de 2025
**Versão:** 1.0
**Referência:** Sistema complementar ao [METRICS_INTEGRATION_PLAN.md](Docs/METRICS_INTEGRATION_PLAN.md)

---

## 📋 Índice

1. [Visão Geral](#-visão-geral)
2. [Objetivos](#-objetivos)
3. [Arquitetura](#-arquitetura)
4. [Implementação Backend](#-implementação-backend)
5. [Implementação Frontend](#-implementação-frontend)
6. [Regras de Alertas Recomendadas](#-regras-de-alertas-recomendadas)
7. [Sistema de Recomendações](#-sistema-de-recomendações)
8. [Integração com History Tracker](#-integração-com-history-tracker)
9. [Fases de Implementação](#-fases-de-implementação)
10. [Testes](#-testes)
11. [Segurança e Performance](#-segurança-e-performance)

---

## 🎯 Visão Geral

O **Alertmanager** é o componente do Prometheus Stack responsável por gerenciar alertas. Esta integração adiciona **inteligência proativa** ao k8s-hpa-manager, permitindo:

- **Monitoramento contínuo** de HPAs e Node Pools
- **Alertas em tempo real** quando limites são atingidos
- **Recomendações automáticas** baseadas em métricas e alertas
- **Correlação** entre alertas e mudanças aplicadas (History Tracker)

### Diferença: Metrics vs Alertmanager

| Feature | Metrics Integration | Alertmanager Integration |
|---------|---------------------|--------------------------|
| **Foco** | Estado atual (snapshot) | Tendências e anomalias |
| **Quando** | On-demand (usuário abre UI) | Contínuo (background) |
| **Dados** | CPU/Memory atual vs target | Alertas de threshold, duração |
| **Ação** | Informativo (badges) | Proativo (notificações + recomendações) |
| **Exemplo** | "CPU: 85% (target 70%)" | "⚠️ HPA at max replicas for 10min" |

**Juntos:** Fornecem visão completa (estado atual + histórico de comportamento).

---

## 🎯 Objetivos

### Primários
1. ✅ Exibir alertas ativos do Prometheus na interface web
2. ✅ Gerar recomendações automáticas baseadas em alertas
3. ✅ Correlacionar alertas com mudanças no History Tracker
4. ✅ Notificar usuário de forma não intrusiva (badge count, toast)

### Secundários
5. ✅ Permitir filtros (severity, cluster, resource)
6. ✅ Auto-refresh de alertas (polling 30s)
7. ✅ Deep link: Alerta → HPA Editor (ação rápida)
8. ✅ Exportar alertas para CSV/JSON

---

## 🏗️ Arquitetura

### Diagrama de Componentes

```
┌────────────────────────────────────────────────────────────────┐
│                    Frontend (React/TypeScript)                  │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐  ┌─────────────────┐  ┌──────────────────┐  │
│  │ AlertPanel   │  │ Recommendations │  │ HistoryViewer    │  │
│  │              │  │ Widget          │  │ (updated)        │  │
│  │ - Badge (🔔) │  │                 │  │                  │  │
│  │ - List       │  │ - Smart suggest │  │ - Alert badge    │  │
│  │ - Filters    │  │ - One-click     │  │ - Correlation    │  │
│  └──────┬───────┘  └────────┬────────┘  └────────┬─────────┘  │
│         │                   │                     │             │
└─────────┼───────────────────┼─────────────────────┼─────────────┘
          │                   │                     │
┌─────────▼───────────────────▼─────────────────────▼─────────────┐
│                     Backend (Go API)                             │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  /api/v1/alerts                      - List alerts (GET)        │
│  /api/v1/alerts/:id                  - Get alert details (GET)  │
│  /api/v1/alerts/stats                - Statistics (GET)         │
│  /api/v1/recommendations             - Get recommendations      │
│  /api/v1/history (updated)           - Include alert context    │
│                                                                  │
│  ┌─────────────────────┐   ┌───────────────────────────┐       │
│  │ AlertmanagerClient  │   │ RecommendationEngine      │       │
│  │ - GetAlerts()       │   │ - Analyze()               │       │
│  │ - GetAlertByID()    │   │ - GenerateSuggestions()   │       │
│  │ - GetStats()        │   │ - ApplyRecommendation()   │       │
│  └──────────┬──────────┘   └─────────────┬─────────────┘       │
│             │                            │                      │
└─────────────┼────────────────────────────┼──────────────────────┘
              │                            │
      ┌───────▼────────┐          ┌────────▼──────────┐
      │  Alertmanager  │          │  Metrics Server   │
      │  REST API      │          │  (K8s)            │
      │  (Prometheus)  │          └───────────────────┘
      └────────────────┘
```

### Fluxo de Dados

**1. Polling de Alertas (Frontend → Backend → Alertmanager)**
```
Frontend (AlertPanel)
  ↓ [HTTP GET /api/v1/alerts?cluster=prod&severity=warning]
Backend (AlertHandler)
  ↓ [HTTP GET http://alertmanager-prod.monitoring.svc.cluster.local:9093/api/v2/alerts]
Alertmanager API
  ↓ [JSON Response: Array<Alert>]
Backend (parse + enrich)
  ↓ [JSON Response to Frontend]
Frontend (render badges, list, toast)
```

**2. Geração de Recomendações (Backend)**
```
AlertmanagerClient.GetAlerts()
  ↓
RecommendationEngine.Analyze(alerts)
  ↓ [Rules engine: if alert == "HPAAtMaxReplicas" for 10min → suggest increase maxReplicas]
  ↓
MetricsClient.GetCurrentMetrics() [opcional: confirmar com dados reais]
  ↓
GenerateSuggestions()
  ↓ [Array<Recommendation>]
Frontend (RecommendationsWidget)
```

**3. Correlação com History (Save → History Tracker)**
```
User applies change (HPA update)
  ↓
Check active alerts for this resource
  ↓
HistoryEntry.triggered_by_alert = "HPAAtMaxReplicas" (if exists)
  ↓
Save to ~/.k8s-hpa-manager/history/
  ↓
HistoryViewer shows badge: "🔔 Applied due to alert"
```

---

## 🔧 Implementação Backend

### 1. Alertmanager Client (`internal/prometheus/alertmanager.go`)

```go
package prometheus

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "time"
)

// Alert representa um alerta do Alertmanager
type Alert struct {
    ID          string            `json:"id"`
    Labels      map[string]string `json:"labels"`
    Annotations map[string]string `json:"annotations"`
    State       string            `json:"state"` // "firing", "pending", "inactive"
    ActiveAt    time.Time         `json:"activeAt"`
    EndsAt      time.Time         `json:"endsAt"`
    Value       string            `json:"value"`

    // Campos derivados para UI
    Severity    string `json:"severity"`    // warning, critical, info
    Resource    string `json:"resource"`    // namespace/hpa-name
    Cluster     string `json:"cluster"`
    Summary     string `json:"summary"`
    Description string `json:"description"`
}

type AlertStats struct {
    Total    int `json:"total"`
    Firing   int `json:"firing"`
    Pending  int `json:"pending"`
    Critical int `json:"critical"`
    Warning  int `json:"warning"`
    Info     int `json:"info"`
}

type AlertmanagerClient struct {
    baseURL string
    client  *http.Client
}

func NewAlertmanagerClient(baseURL string) *AlertmanagerClient {
    return &AlertmanagerClient{
        baseURL: baseURL,
        client:  &http.Client{Timeout: 10 * time.Second},
    }
}

// GetAlerts busca alertas com filtros opcionais
func (c *AlertmanagerClient) GetAlerts(ctx context.Context, filters map[string]string) ([]Alert, error) {
    filterStr := buildFilterString(filters)
    url := fmt.Sprintf("%s/api/v2/alerts?filter=%s", c.baseURL, url.QueryEscape(filterStr))

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch alerts: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("alertmanager returned status %d", resp.StatusCode)
    }

    var rawAlerts []Alert
    if err := json.NewDecoder(resp.Body).Decode(&rawAlerts); err != nil {
        return nil, fmt.Errorf("failed to decode alerts: %w", err)
    }

    // Enriquecer alertas com campos derivados
    alerts := make([]Alert, 0, len(rawAlerts))
    for _, alert := range rawAlerts {
        enrichAlert(&alert)
        alerts = append(alerts, alert)
    }

    return alerts, nil
}

// GetAlertByID busca um alerta específico por ID
func (c *AlertmanagerClient) GetAlertByID(ctx context.Context, id string) (*Alert, error) {
    alerts, err := c.GetAlerts(ctx, nil)
    if err != nil {
        return nil, err
    }

    for _, alert := range alerts {
        if alert.ID == id {
            return &alert, nil
        }
    }

    return nil, fmt.Errorf("alert %s not found", id)
}

// GetStats retorna estatísticas de alertas
func (c *AlertmanagerClient) GetStats(ctx context.Context, cluster string) (*AlertStats, error) {
    filters := map[string]string{}
    if cluster != "" {
        filters["cluster"] = cluster
    }

    alerts, err := c.GetAlerts(ctx, filters)
    if err != nil {
        return nil, err
    }

    stats := &AlertStats{}
    for _, alert := range alerts {
        stats.Total++

        switch alert.State {
        case "firing":
            stats.Firing++
        case "pending":
            stats.Pending++
        }

        switch alert.Severity {
        case "critical":
            stats.Critical++
        case "warning":
            stats.Warning++
        case "info":
            stats.Info++
        }
    }

    return stats, nil
}

// enrichAlert adiciona campos derivados ao alerta
func enrichAlert(alert *Alert) {
    // Extrair severity dos labels
    if sev, ok := alert.Labels["severity"]; ok {
        alert.Severity = sev
    }

    // Extrair cluster
    if cluster, ok := alert.Labels["cluster"]; ok {
        alert.Cluster = cluster
    }

    // Construir resource (namespace/name)
    if ns, ok := alert.Labels["namespace"]; ok {
        if name, ok := alert.Labels["horizontalpodautoscaler"]; ok {
            alert.Resource = fmt.Sprintf("%s/%s", ns, name)
        } else if name, ok := alert.Labels["pod"]; ok {
            alert.Resource = fmt.Sprintf("%s/%s", ns, name)
        }
    }

    // Extrair summary e description das annotations
    if summary, ok := alert.Annotations["summary"]; ok {
        alert.Summary = summary
    }
    if desc, ok := alert.Annotations["description"]; ok {
        alert.Description = desc
    }

    // Gerar ID se não existir (hash dos labels)
    if alert.ID == "" {
        alert.ID = generateAlertID(alert.Labels)
    }
}

// buildFilterString constrói string de filtro para Alertmanager API
func buildFilterString(filters map[string]string) string {
    if len(filters) == 0 {
        return ""
    }

    var parts []string
    for k, v := range filters {
        parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
    }

    return fmt.Sprintf("{%s}", join(parts, ","))
}

// generateAlertID gera ID único para alerta baseado em labels
func generateAlertID(labels map[string]string) string {
    // Implementação simplificada - usar hash MD5 dos labels
    return fmt.Sprintf("alert-%d", hashLabels(labels))
}
```

### 2. Alert Handler (`internal/web/handlers/alerts.go`)

```go
package handlers

import (
    "fmt"
    "net/http"

    "k8s-hpa-manager/internal/config"
    "k8s-hpa-manager/internal/prometheus"

    "github.com/gin-gonic/gin"
)

type AlertHandler struct {
    kubeManager *config.KubeConfigManager
}

func NewAlertHandler(km *config.KubeConfigManager) *AlertHandler {
    return &AlertHandler{
        kubeManager: km,
    }
}

// List retorna alertas filtrados
func (h *AlertHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")      // Obrigatório
    severity := c.Query("severity")    // Opcional: critical, warning, info
    state := c.Query("state")          // Opcional: firing, pending
    resource := c.Query("resource")    // Opcional: namespace/hpa-name

    if cluster == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "MISSING_PARAMETER",
                "message": "Parameter 'cluster' is required",
            },
        })
        return
    }

    // Obter URL do Alertmanager para o cluster
    alertmanagerURL, err := h.getAlertmanagerURL(cluster)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "CONFIG_ERROR",
                "message": fmt.Sprintf("Failed to get Alertmanager URL: %v", err),
            },
        })
        return
    }

    client := prometheus.NewAlertmanagerClient(alertmanagerURL)

    // Construir filtros
    filters := map[string]string{
        "cluster": cluster,
    }
    if severity != "" {
        filters["severity"] = severity
    }
    if state != "" {
        filters["alertstate"] = state
    }

    // Buscar alertas
    alerts, err := client.GetAlerts(c.Request.Context(), filters)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "FETCH_ERROR",
                "message": fmt.Sprintf("Failed to fetch alerts: %v", err),
            },
        })
        return
    }

    // Filtro adicional por resource (backend)
    if resource != "" {
        filtered := []prometheus.Alert{}
        for _, alert := range alerts {
            if alert.Resource == resource {
                filtered = append(filtered, alert)
            }
        }
        alerts = filtered
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    alerts,
        "count":   len(alerts),
    })
}

// Get retorna detalhes de um alerta específico
func (h *AlertHandler) Get(c *gin.Context) {
    cluster := c.Param("cluster")
    alertID := c.Param("id")

    alertmanagerURL, err := h.getAlertmanagerURL(cluster)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "CONFIG_ERROR",
                "message": fmt.Sprintf("Failed to get Alertmanager URL: %v", err),
            },
        })
        return
    }

    client := prometheus.NewAlertmanagerClient(alertmanagerURL)

    alert, err := client.GetAlertByID(c.Request.Context(), alertID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "NOT_FOUND",
                "message": fmt.Sprintf("Alert not found: %v", err),
            },
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    alert,
    })
}

// GetStats retorna estatísticas de alertas
func (h *AlertHandler) GetStats(c *gin.Context) {
    cluster := c.Query("cluster")

    if cluster == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "MISSING_PARAMETER",
                "message": "Parameter 'cluster' is required",
            },
        })
        return
    }

    alertmanagerURL, err := h.getAlertmanagerURL(cluster)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "CONFIG_ERROR",
                "message": fmt.Sprintf("Failed to get Alertmanager URL: %v", err),
            },
        })
        return
    }

    client := prometheus.NewAlertmanagerClient(alertmanagerURL)

    stats, err := client.GetStats(c.Request.Context(), cluster)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "FETCH_ERROR",
                "message": fmt.Sprintf("Failed to fetch stats: %v", err),
            },
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    stats,
    })
}

// getAlertmanagerURL retorna a URL do Alertmanager para o cluster
func (h *AlertHandler) getAlertmanagerURL(cluster string) (string, error) {
    // Padrão: http://alertmanager-{cluster}.monitoring.svc.cluster.local:9093
    // Configurável via clusters-config.json (campo "alertmanager_url")

    // TODO: Implementar leitura de clusters-config.json
    // Por enquanto, retornar URL padrão
    return fmt.Sprintf("http://alertmanager-%s.monitoring.svc.cluster.local:9093", cluster), nil
}
```

### 3. Rotas no Server (`internal/web/server.go`)

```go
// Adicionar no método setupRoutes()

func (s *Server) setupRoutes() {
    // ... rotas existentes ...

    // Alertmanager routes
    alertHandler := handlers.NewAlertHandler(s.kubeManager)
    api.GET("/alerts", alertHandler.List)                      // Lista alertas
    api.GET("/alerts/stats", alertHandler.GetStats)            // Estatísticas
    api.GET("/alerts/:cluster/:id", alertHandler.Get)          // Alerta específico
}
```

---

## 🎨 Implementação Frontend

### 1. Alert Panel Component (`internal/web/frontend/src/components/AlertPanel.tsx`)

```typescript
import React, { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Bell, AlertTriangle, Info, RefreshCw, ExternalLink } from "lucide-react";
import { apiClient } from "@/lib/api/client";

interface Alert {
  id: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  state: "firing" | "pending" | "inactive";
  activeAt: string;
  severity: string;
  resource: string;
  cluster: string;
  summary: string;
  description: string;
}

interface AlertStats {
  total: number;
  firing: number;
  pending: number;
  critical: number;
  warning: number;
  info: number;
}

interface AlertPanelProps {
  cluster: string;
  onAlertClick?: (alert: Alert) => void; // Deep link para HPA Editor
}

export function AlertPanel({ cluster, onAlertClick }: AlertPanelProps) {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [stats, setStats] = useState<AlertStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [filter, setFilter] = useState<"all" | "critical" | "warning" | "info">("all");
  const [autoRefresh, setAutoRefresh] = useState(true);

  // Fetch alerts
  const fetchAlerts = async () => {
    setLoading(true);
    try {
      const token = localStorage.getItem("auth_token") || "poc-token-123";

      // Fetch alerts
      const severityFilter = filter !== "all" ? `&severity=${filter}` : "";
      const alertsRes = await fetch(
        `/api/v1/alerts?cluster=${cluster}${severityFilter}`,
        { headers: { Authorization: `Bearer ${token}` } }
      );
      const alertsData = await alertsRes.json();

      if (alertsData.success) {
        setAlerts(alertsData.data);
      }

      // Fetch stats
      const statsRes = await fetch(
        `/api/v1/alerts/stats?cluster=${cluster}`,
        { headers: { Authorization: `Bearer ${token}` } }
      );
      const statsData = await statsRes.json();

      if (statsData.success) {
        setStats(statsData.data);
      }
    } catch (error) {
      console.error("Failed to fetch alerts:", error);
    } finally {
      setLoading(false);
    }
  };

  // Initial fetch
  useEffect(() => {
    fetchAlerts();
  }, [cluster, filter]);

  // Auto-refresh every 30s
  useEffect(() => {
    if (!autoRefresh) return;

    const interval = setInterval(() => {
      fetchAlerts();
    }, 30000);

    return () => clearInterval(interval);
  }, [autoRefresh, cluster, filter]);

  // Severity badge
  const getSeverityBadge = (severity: string) => {
    const variants: Record<string, { color: string; icon: React.ReactNode }> = {
      critical: { color: "bg-red-500", icon: <AlertTriangle className="w-3 h-3" /> },
      warning: { color: "bg-yellow-500", icon: <AlertTriangle className="w-3 h-3" /> },
      info: { color: "bg-blue-500", icon: <Info className="w-3 h-3" /> },
    };

    const variant = variants[severity] || variants.info;

    return (
      <Badge className={`${variant.color} text-white`}>
        {variant.icon}
        <span className="ml-1">{severity}</span>
      </Badge>
    );
  };

  // Time ago helper
  const timeAgo = (timestamp: string) => {
    const now = new Date();
    const then = new Date(timestamp);
    const diffMs = now.getTime() - then.getTime();
    const diffMins = Math.floor(diffMs / 60000);

    if (diffMins < 60) return `${diffMins}m ago`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours}h ago`;
    const diffDays = Math.floor(diffHours / 24);
    return `${diffDays}d ago`;
  };

  return (
    <Card className="w-full">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bell className="w-5 h-5" />
            <CardTitle>Active Alerts</CardTitle>
            {stats && (
              <Badge variant="secondary">
                {stats.total} total
              </Badge>
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* Auto-refresh toggle */}
            <Button
              variant={autoRefresh ? "default" : "outline"}
              size="sm"
              onClick={() => setAutoRefresh(!autoRefresh)}
              title={autoRefresh ? "Auto-refresh ON" : "Auto-refresh OFF"}
            >
              <RefreshCw className={`w-4 h-4 ${autoRefresh ? "animate-spin" : ""}`} />
            </Button>

            {/* Manual refresh */}
            <Button
              variant="outline"
              size="sm"
              onClick={fetchAlerts}
              disabled={loading}
            >
              Refresh
            </Button>
          </div>
        </div>

        {/* Stats badges */}
        {stats && (
          <div className="flex gap-2 mt-2">
            <Badge
              variant="outline"
              className="cursor-pointer hover:bg-red-100"
              onClick={() => setFilter("critical")}
            >
              🔴 {stats.critical} Critical
            </Badge>
            <Badge
              variant="outline"
              className="cursor-pointer hover:bg-yellow-100"
              onClick={() => setFilter("warning")}
            >
              ⚠️ {stats.warning} Warning
            </Badge>
            <Badge
              variant="outline"
              className="cursor-pointer hover:bg-blue-100"
              onClick={() => setFilter("info")}
            >
              ℹ️ {stats.info} Info
            </Badge>
            <Badge
              variant="outline"
              className="cursor-pointer hover:bg-gray-100"
              onClick={() => setFilter("all")}
            >
              📊 All
            </Badge>
          </div>
        )}
      </CardHeader>

      <CardContent>
        {loading && alerts.length === 0 ? (
          <div className="text-center text-gray-500 py-4">Loading alerts...</div>
        ) : alerts.length === 0 ? (
          <div className="text-center text-gray-500 py-4">
            ✅ No active alerts for this cluster
          </div>
        ) : (
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {alerts.map((alert) => (
              <div
                key={alert.id}
                className="border rounded-lg p-3 hover:bg-gray-50 cursor-pointer transition-colors"
                onClick={() => onAlertClick?.(alert)}
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      {getSeverityBadge(alert.severity)}
                      <span className="font-semibold">{alert.labels.alertname}</span>
                      <Badge variant="outline" className="text-xs">
                        {alert.state}
                      </Badge>
                    </div>

                    <p className="text-sm text-gray-700 mb-1">
                      {alert.summary}
                    </p>

                    {alert.resource && (
                      <p className="text-xs text-gray-500">
                        📦 {alert.resource}
                      </p>
                    )}

                    <p className="text-xs text-gray-400 mt-1">
                      {timeAgo(alert.activeAt)}
                    </p>
                  </div>

                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      onAlertClick?.(alert);
                    }}
                  >
                    <ExternalLink className="w-4 h-4" />
                  </Button>
                </div>

                {alert.description && (
                  <p className="text-xs text-gray-600 mt-2 border-t pt-2">
                    💡 {alert.description}
                  </p>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
```

### 2. Integração no Dashboard (`internal/web/frontend/src/pages/Index.tsx`)

```typescript
// Adicionar import
import { AlertPanel } from "@/components/AlertPanel";

// Dentro do component Index:
const [showAlerts, setShowAlerts] = useState(false);

// Adicionar no layout (abaixo do Dashboard ou como aba separada)
{showAlerts && (
  <AlertPanel
    cluster={selectedCluster}
    onAlertClick={(alert) => {
      // Deep link: Abrir HPA Editor se alerta for de HPA
      if (alert.resource) {
        const [namespace, hpaName] = alert.resource.split("/");
        // TODO: Implementar abertura do HPAEditor com esses dados
        console.log("Open HPA Editor:", { namespace, hpaName });
      }
    }}
  />
)}
```

### 3. Badge no Header (`internal/web/frontend/src/components/Header.tsx`)

```typescript
// Adicionar badge de alertas ativos no header
import { Bell } from "lucide-react";

const [alertCount, setAlertCount] = useState(0);

// Fetch alert count
useEffect(() => {
  const fetchAlertCount = async () => {
    const token = localStorage.getItem("auth_token") || "poc-token-123";
    const res = await fetch(`/api/v1/alerts/stats?cluster=${selectedCluster}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    const data = await res.json();
    if (data.success) {
      setAlertCount(data.data.firing + data.data.pending);
    }
  };

  fetchAlertCount();
  const interval = setInterval(fetchAlertCount, 30000);
  return () => clearInterval(interval);
}, [selectedCluster]);

// Render badge
<Button
  variant="secondary"
  size="sm"
  className="relative"
  onClick={() => setShowAlerts(!showAlerts)}
  title="View Alerts"
>
  <Bell className="w-4 h-4" />
  {alertCount > 0 && (
    <Badge className="absolute -top-1 -right-1 bg-red-500 text-white text-xs px-1">
      {alertCount}
    </Badge>
  )}
</Button>
```

---

## 🚨 Regras de Alertas Recomendadas

### Arquivo de Configuração Prometheus (`prometheus-rules.yaml`)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-hpa-rules
  namespace: monitoring
data:
  hpa-rules.yml: |
    groups:
      - name: hpa_scaling_alerts
        interval: 30s
        rules:
          # Alerta: HPA atingiu maxReplicas
          - alert: HPAAtMaxReplicas
            expr: |
              kube_horizontalpodautoscaler_status_current_replicas >= kube_horizontalpodautoscaler_spec_max_replicas
            for: 5m
            labels:
              severity: warning
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} at max replicas"
              description: "HPA has been at max replicas ({{ $value }}) for 5+ minutes. Consider increasing maxReplicas."

          # Alerta: HPA atingiu minReplicas
          - alert: HPAAtMinReplicas
            expr: |
              kube_horizontalpodautoscaler_status_current_replicas <= kube_horizontalpodautoscaler_spec_min_replicas
            for: 30m
            labels:
              severity: info
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} at min replicas"
              description: "HPA has been at min replicas ({{ $value }}) for 30+ minutes. Consider lowering minReplicas to save resources."

          # Alerta: CPU consistentemente acima do target
          - alert: HPACPUAboveTarget
            expr: |
              (
                sum by (namespace, horizontalpodautoscaler) (
                  rate(container_cpu_usage_seconds_total{pod=~".*"}[5m])
                )
                /
                sum by (namespace, horizontalpodautoscaler) (
                  kube_horizontalpodautoscaler_spec_target_metric{metric_name="cpu"}
                )
              ) > 1.2
            for: 10m
            labels:
              severity: warning
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} CPU 20% above target"
              description: "CPU usage has been 20%+ above target for 10 minutes. Current: {{ $value | humanizePercentage }}. Consider adjusting target CPU or maxReplicas."

          # Alerta: CPU consistentemente abaixo do target
          - alert: HPACPUBelowTarget
            expr: |
              (
                sum by (namespace, horizontalpodautoscaler) (
                  rate(container_cpu_usage_seconds_total{pod=~".*"}[5m])
                )
                /
                sum by (namespace, horizontalpodautoscaler) (
                  kube_horizontalpodautoscaler_spec_target_metric{metric_name="cpu"}
                )
              ) < 0.5
            for: 30m
            labels:
              severity: info
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} CPU 50% below target"
              description: "CPU usage has been 50%+ below target for 30 minutes. Current: {{ $value | humanizePercentage }}. Consider lowering target CPU to optimize costs."

          # Alerta: Memory consistentemente acima do target
          - alert: HPAMemoryAboveTarget
            expr: |
              (
                sum by (namespace, horizontalpodautoscaler) (
                  container_memory_working_set_bytes{pod=~".*"}
                )
                /
                sum by (namespace, horizontalpodautoscaler) (
                  kube_horizontalpodautoscaler_spec_target_metric{metric_name="memory"}
                )
              ) > 1.2
            for: 10m
            labels:
              severity: warning
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} Memory 20% above target"
              description: "Memory usage has been 20%+ above target for 10 minutes. Consider adjusting target Memory or maxReplicas."

          # Alerta: HPA não consegue escalar (throttling)
          - alert: HPAScalingThrottled
            expr: |
              increase(kube_horizontalpodautoscaler_status_condition{condition="ScalingLimited",status="true"}[5m]) > 0
            for: 5m
            labels:
              severity: critical
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} scaling throttled"
              description: "HPA is unable to scale. Check resource quotas, node capacity, and HPA configuration."

          # Alerta: HPA com erro de métricas
          - alert: HPAMetricsUnavailable
            expr: |
              kube_horizontalpodautoscaler_status_condition{condition="ScalingActive",status="false"} == 1
            for: 5m
            labels:
              severity: warning
              component: hpa
            annotations:
              summary: "HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} metrics unavailable"
              description: "HPA cannot read metrics. Check Metrics Server and target resource."

      - name: nodepool_alerts
        interval: 30s
        rules:
          # Alerta: Node Pool com alta utilização
          - alert: NodePoolHighUtilization
            expr: |
              (
                sum by (cluster, nodepool) (kube_node_status_allocatable{resource="cpu"})
                -
                sum by (cluster, nodepool) (kube_node_status_capacity{resource="cpu"} - kube_node_status_allocatable{resource="cpu"})
              ) / sum by (cluster, nodepool) (kube_node_status_capacity{resource="cpu"}) > 0.85
            for: 5m
            labels:
              severity: warning
              component: nodepool
            annotations:
              summary: "Node Pool {{ $labels.nodepool }} high CPU utilization"
              description: "Node Pool is at {{ $value | humanizePercentage }} CPU capacity. Consider scaling up."

          # Alerta: Node Pool próximo ao limite de nodes
          - alert: NodePoolNearMaxNodes
            expr: |
              (
                sum by (cluster, nodepool) (kube_node_info{node=~".*"})
                /
                max by (cluster, nodepool) (azure_nodepool_max_nodes)
              ) > 0.9
            for: 5m
            labels:
              severity: warning
              component: nodepool
            annotations:
              summary: "Node Pool {{ $labels.nodepool }} near max nodes"
              description: "Node Pool has {{ $value | humanizePercentage }} of max nodes. Consider increasing max nodes."
```

### Instalação das Regras

```bash
# Aplicar ConfigMap
kubectl apply -f prometheus-rules.yaml

# Recarregar configuração do Prometheus
kubectl -n monitoring exec prometheus-0 -- kill -HUP 1

# Verificar regras carregadas
kubectl -n monitoring exec prometheus-0 -- promtool check config /etc/prometheus/prometheus.yml
```

---

## 💡 Sistema de Recomendações

### Recommendation Engine (`internal/prometheus/recommendations.go`)

```go
package prometheus

import (
    "context"
    "fmt"
    "time"

    "k8s-hpa-manager/internal/models"
)

type RecommendationType string

const (
    RecommendationScaleUp        RecommendationType = "scale_up"
    RecommendationScaleDown      RecommendationType = "scale_down"
    RecommendationAdjustTarget   RecommendationType = "adjust_target"
    RecommendationEnableAutosc   RecommendationType = "enable_autoscaling"
    RecommendationDisableAutosc  RecommendationType = "disable_autoscaling"
)

type Recommendation struct {
    ID          string                 `json:"id"`
    Type        RecommendationType     `json:"type"`
    Severity    string                 `json:"severity"` // critical, warning, info
    Resource    string                 `json:"resource"` // namespace/hpa-name
    Cluster     string                 `json:"cluster"`
    Reason      string                 `json:"reason"`
    BasedOnAlert string                `json:"based_on_alert,omitempty"`
    SuggestedAction SuggestedAction    `json:"suggested_action"`
    CreatedAt   time.Time              `json:"created_at"`
}

type SuggestedAction struct {
    Field         string      `json:"field"`          // maxReplicas, targetCPU, etc
    CurrentValue  interface{} `json:"current_value"`
    SuggestedValue interface{} `json:"suggested_value"`
    EstimatedImpact string    `json:"estimated_impact"` // Descrição do impacto
}

type RecommendationEngine struct {
    alertClient   *AlertmanagerClient
    metricsClient *MetricsClient // Do METRICS_INTEGRATION_PLAN.md
}

func NewRecommendationEngine(alertClient *AlertmanagerClient, metricsClient *MetricsClient) *RecommendationEngine {
    return &RecommendationEngine{
        alertClient:   alertClient,
        metricsClient: metricsClient,
    }
}

// GenerateRecommendations analisa alertas e métricas para gerar recomendações
func (e *RecommendationEngine) GenerateRecommendations(ctx context.Context, cluster string) ([]Recommendation, error) {
    // Buscar alertas ativos
    alerts, err := e.alertClient.GetAlerts(ctx, map[string]string{
        "cluster": cluster,
        "state":   "firing",
    })
    if err != nil {
        return nil, err
    }

    recommendations := []Recommendation{}

    for _, alert := range alerts {
        recs := e.analyzeAlert(alert)
        recommendations = append(recommendations, recs...)
    }

    return recommendations, nil
}

// analyzeAlert analisa um alerta e retorna recomendações
func (e *RecommendationEngine) analyzeAlert(alert Alert) []Recommendation {
    recommendations := []Recommendation{}

    switch alert.Labels["alertname"] {
    case "HPAAtMaxReplicas":
        // Recomendar aumento de maxReplicas
        recommendations = append(recommendations, Recommendation{
            ID:       fmt.Sprintf("rec-%s-%d", alert.ID, time.Now().Unix()),
            Type:     RecommendationScaleUp,
            Severity: "warning",
            Resource: alert.Resource,
            Cluster:  alert.Cluster,
            Reason:   "HPA has been at max replicas for extended period",
            BasedOnAlert: alert.Labels["alertname"],
            SuggestedAction: SuggestedAction{
                Field:         "maxReplicas",
                CurrentValue:  alert.Labels["current_max"], // Extrair do alerta
                SuggestedValue: calculateNewMax(alert.Labels["current_max"]),
                EstimatedImpact: "Allows HPA to scale beyond current limit, improving responsiveness under high load",
            },
            CreatedAt: time.Now(),
        })

    case "HPACPUAboveTarget":
        // Recomendar ajuste de target CPU ou aumento de maxReplicas
        variance := parseVariance(alert.Value)

        if variance > 30 { // Mais de 30% acima do target
            recommendations = append(recommendations, Recommendation{
                ID:       fmt.Sprintf("rec-%s-%d", alert.ID, time.Now().Unix()),
                Type:     RecommendationAdjustTarget,
                Severity: "warning",
                Resource: alert.Resource,
                Cluster:  alert.Cluster,
                Reason:   fmt.Sprintf("CPU consistently %.0f%% above target", variance),
                BasedOnAlert: alert.Labels["alertname"],
                SuggestedAction: SuggestedAction{
                    Field:         "targetCPU",
                    CurrentValue:  alert.Labels["target_cpu"],
                    SuggestedValue: calculateNewTargetCPU(alert.Labels["target_cpu"], variance),
                    EstimatedImpact: "Reduces scaling threshold, allowing HPA to scale earlier",
                },
                CreatedAt: time.Now(),
            })
        }

    case "HPACPUBelowTarget":
        // Recomendar redução de target CPU ou minReplicas
        recommendations = append(recommendations, Recommendation{
            ID:       fmt.Sprintf("rec-%s-%d", alert.ID, time.Now().Unix()),
            Type:     RecommendationScaleDown,
            Severity: "info",
            Resource: alert.Resource,
            Cluster:  alert.Cluster,
            Reason:   "CPU consistently below target - potential cost savings",
            BasedOnAlert: alert.Labels["alertname"],
            SuggestedAction: SuggestedAction{
                Field:         "targetCPU",
                CurrentValue:  alert.Labels["target_cpu"],
                SuggestedValue: calculateOptimalTargetCPU(alert.Labels["target_cpu"]),
                EstimatedImpact: "Optimizes resource usage and reduces costs",
            },
            CreatedAt: time.Now(),
        })

    case "NodePoolHighUtilization":
        // Recomendar escalonamento de node pool
        recommendations = append(recommendations, Recommendation{
            ID:       fmt.Sprintf("rec-%s-%d", alert.ID, time.Now().Unix()),
            Type:     RecommendationScaleUp,
            Severity: "warning",
            Resource: alert.Labels["nodepool"],
            Cluster:  alert.Cluster,
            Reason:   "Node Pool nearing capacity",
            BasedOnAlert: alert.Labels["alertname"],
            SuggestedAction: SuggestedAction{
                Field:         "node_count",
                CurrentValue:  alert.Labels["current_nodes"],
                SuggestedValue: calculateNewNodeCount(alert.Labels["current_nodes"]),
                EstimatedImpact: "Increases cluster capacity, prevents pod scheduling failures",
            },
            CreatedAt: time.Now(),
        })
    }

    return recommendations
}

// Helper functions
func calculateNewMax(currentMax string) int {
    // Parse currentMax e aumentar 20-30%
    // Implementação simplificada
    return 0
}

func parseVariance(value string) float64 {
    // Parse variance from alert value
    return 0
}

func calculateNewTargetCPU(current string, variance float64) int {
    // Calcular novo target baseado em variance
    return 0
}

func calculateOptimalTargetCPU(current string) int {
    // Calcular target otimizado
    return 0
}

func calculateNewNodeCount(current string) int {
    // Calcular novo node count
    return 0
}
```

### Recommendations Handler (`internal/web/handlers/recommendations.go`)

```go
package handlers

import (
    "net/http"

    "k8s-hpa-manager/internal/config"
    "k8s-hpa-manager/internal/prometheus"

    "github.com/gin-gonic/gin"
)

type RecommendationHandler struct {
    kubeManager *config.KubeConfigManager
    engine      *prometheus.RecommendationEngine
}

func NewRecommendationHandler(km *config.KubeConfigManager, engine *prometheus.RecommendationEngine) *RecommendationHandler {
    return &RecommendationHandler{
        kubeManager: km,
        engine:      engine,
    }
}

// List retorna recomendações para um cluster
func (h *RecommendationHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")

    if cluster == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "MISSING_PARAMETER",
                "message": "Parameter 'cluster' is required",
            },
        })
        return
    }

    recommendations, err := h.engine.GenerateRecommendations(c.Request.Context(), cluster)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "success": false,
            "error": gin.H{
                "code":    "GENERATION_ERROR",
                "message": err.Error(),
            },
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    recommendations,
        "count":   len(recommendations),
    })
}
```

### Recommendations Widget Frontend (`internal/web/frontend/src/components/RecommendationsWidget.tsx`)

```typescript
import React, { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Lightbulb, Check, X } from "lucide-react";

interface Recommendation {
  id: string;
  type: string;
  severity: string;
  resource: string;
  cluster: string;
  reason: string;
  based_on_alert?: string;
  suggested_action: {
    field: string;
    current_value: any;
    suggested_value: any;
    estimated_impact: string;
  };
  created_at: string;
}

interface RecommendationsWidgetProps {
  cluster: string;
  onApply: (rec: Recommendation) => void;
}

export function RecommendationsWidget({ cluster, onApply }: RecommendationsWidgetProps) {
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [loading, setLoading] = useState(false);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());

  const fetchRecommendations = async () => {
    setLoading(true);
    try {
      const token = localStorage.getItem("auth_token") || "poc-token-123";
      const res = await fetch(`/api/v1/recommendations?cluster=${cluster}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      const data = await res.json();

      if (data.success) {
        setRecommendations(data.data);
      }
    } catch (error) {
      console.error("Failed to fetch recommendations:", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchRecommendations();
    const interval = setInterval(fetchRecommendations, 60000); // Refresh every 60s
    return () => clearInterval(interval);
  }, [cluster]);

  const visibleRecommendations = recommendations.filter(
    (rec) => !dismissed.has(rec.id)
  );

  return (
    <Card className="w-full">
      <CardHeader>
        <div className="flex items-center gap-2">
          <Lightbulb className="w-5 h-5 text-yellow-500" />
          <CardTitle>Smart Recommendations</CardTitle>
          {visibleRecommendations.length > 0 && (
            <Badge variant="secondary">{visibleRecommendations.length}</Badge>
          )}
        </div>
      </CardHeader>

      <CardContent>
        {loading && recommendations.length === 0 ? (
          <div className="text-center text-gray-500 py-4">
            Analyzing alerts and metrics...
          </div>
        ) : visibleRecommendations.length === 0 ? (
          <div className="text-center text-gray-500 py-4">
            ✅ No recommendations at this time
          </div>
        ) : (
          <div className="space-y-3">
            {visibleRecommendations.map((rec) => (
              <div
                key={rec.id}
                className="border rounded-lg p-4 bg-gradient-to-r from-yellow-50 to-white"
              >
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <Badge
                      variant={rec.severity === "critical" ? "destructive" : "secondary"}
                    >
                      {rec.severity}
                    </Badge>
                    <span className="font-semibold text-sm">{rec.resource}</span>
                  </div>

                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setDismissed((prev) => new Set(prev).add(rec.id))}
                  >
                    <X className="w-4 h-4" />
                  </Button>
                </div>

                <p className="text-sm text-gray-700 mb-2">
                  📊 {rec.reason}
                </p>

                {rec.based_on_alert && (
                  <p className="text-xs text-gray-500 mb-2">
                    🔔 Based on alert: <strong>{rec.based_on_alert}</strong>
                  </p>
                )}

                <div className="bg-white border rounded p-3 mb-2">
                  <p className="text-xs text-gray-600 mb-1">
                    <strong>Suggested Action:</strong>
                  </p>
                  <p className="text-sm">
                    Change <strong>{rec.suggested_action.field}</strong> from{" "}
                    <span className="text-red-600">{rec.suggested_action.current_value}</span>{" "}
                    to{" "}
                    <span className="text-green-600">{rec.suggested_action.suggested_value}</span>
                  </p>
                  <p className="text-xs text-gray-500 mt-1">
                    💡 Impact: {rec.suggested_action.estimated_impact}
                  </p>
                </div>

                <div className="flex gap-2">
                  <Button
                    size="sm"
                    className="bg-green-500 hover:bg-green-600 text-white"
                    onClick={() => onApply(rec)}
                  >
                    <Check className="w-4 h-4 mr-1" />
                    Apply
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setDismissed((prev) => new Set(prev).add(rec.id))}
                  >
                    Dismiss
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
```

---

## 📊 Integração com History Tracker

### Atualização do HistoryEntry (`internal/history/tracker.go`)

```go
type HistoryEntry struct {
    ID           string                 `json:"id"`
    Timestamp    string                 `json:"timestamp"`
    Action       string                 `json:"action"`
    Resource     string                 `json:"resource"`
    Cluster      string                 `json:"cluster"`
    Before       map[string]interface{} `json:"before"`
    After        map[string]interface{} `json:"after"`
    Status       string                 `json:"status"`
    ErrorMsg     string                 `json:"error_msg,omitempty"`
    Duration     int64                  `json:"duration_ms"`
    SessionName  string                 `json:"session_name,omitempty"`

    // NOVO: Campos de correlação com alertas
    TriggeredByAlert string `json:"triggered_by_alert,omitempty"` // Nome do alerta
    AlertFiredAt     string `json:"alert_fired_at,omitempty"`     // Timestamp do alerta
    RecommendationID string `json:"recommendation_id,omitempty"`   // ID da recomendação aplicada
}
```

### Captura de Contexto no Handler (`internal/web/handlers/hpas.go`)

```go
// No método Update(), adicionar antes de aplicar mudanças:

// Verificar se há alertas ativos para este recurso
alertmanagerURL, err := h.getAlertmanagerURL(cluster)
if err == nil {
    alertClient := prometheus.NewAlertmanagerClient(alertmanagerURL)
    alerts, err := alertClient.GetAlerts(c.Request.Context(), map[string]string{
        "cluster": cluster,
        "namespace": namespace,
        "horizontalpodautoscaler": name,
    })

    if err == nil && len(alerts) > 0 {
        // Usar o primeiro alerta firing como contexto
        for _, alert := range alerts {
            if alert.State == "firing" {
                // Adicionar contexto ao history entry (mais tarde)
                triggeredByAlert = alert.Labels["alertname"]
                alertFiredAt = alert.ActiveAt.Format(time.RFC3339)
                break
            }
        }
    }
}

// ... aplicar mudanças ...

// No Log() do history tracker:
h.historyTracker.Log(history.HistoryEntry{
    Action:           history.ActionUpdateHPA,
    Resource:         fmt.Sprintf("%s/%s", namespace, name),
    Cluster:          cluster,
    Before:           beforeState,
    After:            afterState,
    Status:           history.StatusSuccess,
    Duration:         duration,
    TriggeredByAlert: triggeredByAlert,  // NOVO
    AlertFiredAt:     alertFiredAt,      // NOVO
})
```

### Badge no HistoryViewer Frontend (`internal/web/frontend/src/components/HistoryViewer.tsx`)

```typescript
// Adicionar badge de alerta ao lado do resource name

{entry.triggered_by_alert && (
  <Badge variant="outline" className="ml-2 bg-yellow-50 text-yellow-700 border-yellow-300">
    🔔 Alert: {entry.triggered_by_alert}
  </Badge>
)}

// Adicionar no expandedEntry details:
{entry.triggered_by_alert && (
  <div className="bg-yellow-50 border border-yellow-200 rounded p-2 mt-2">
    <p className="text-xs text-yellow-800">
      <strong>🔔 Applied in response to alert:</strong> {entry.triggered_by_alert}
    </p>
    {entry.alert_fired_at && (
      <p className="text-xs text-yellow-600">
        Alert fired at: {new Date(entry.alert_fired_at).toLocaleString()}
      </p>
    )}
  </div>
)}
```

---

## 📅 Fases de Implementação

### Fase 1: Backend Alertmanager Client (Estimativa: 3-4 horas)

**Objetivos:**
- ✅ Criar `AlertmanagerClient` em Go
- ✅ Implementar handler `/api/v1/alerts`
- ✅ Testes com Alertmanager real

**Tarefas:**
1. Criar `internal/prometheus/alertmanager.go`
2. Implementar `GetAlerts()`, `GetAlertByID()`, `GetStats()`
3. Criar `internal/web/handlers/alerts.go`
4. Adicionar rotas no `server.go`
5. Testar com Alertmanager de desenvolvimento

**Critérios de Aceite:**
- API retorna alertas do Alertmanager corretamente
- Filtros funcionam (cluster, severity, state)
- Erros tratados graciosamente

---

### Fase 2: Frontend AlertPanel (Estimativa: 4-5 horas)

**Objetivos:**
- ✅ Criar `AlertPanel` component
- ✅ Auto-refresh (polling 30s)
- ✅ Badge de alertas ativos no header

**Tarefas:**
1. Criar `internal/web/frontend/src/components/AlertPanel.tsx`
2. Integrar no `Index.tsx` (Dashboard ou aba separada)
3. Adicionar badge no `Header.tsx` com contagem de alertas
4. Implementar filtros de severity
5. Adicionar deep link: Click em alerta → Abrir HPA Editor

**Critérios de Aceite:**
- Alertas exibidos em tempo real
- Auto-refresh funciona sem travar UI
- Badge atualiza contagem corretamente
- Deep link abre HPA Editor com dados corretos

---

### Fase 3: Sistema de Recomendações (Estimativa: 6-8 horas)

**Objetivos:**
- ✅ Implementar `RecommendationEngine`
- ✅ Handler `/api/v1/recommendations`
- ✅ Widget de recomendações no frontend

**Tarefas:**
1. Criar `internal/prometheus/recommendations.go`
2. Implementar lógica de análise de alertas
3. Criar handler `/api/v1/recommendations`
4. Criar `RecommendationsWidget.tsx`
5. Integrar widget no Dashboard
6. Implementar ação "Apply" (aplicar recomendação com um clique)

**Critérios de Aceite:**
- Recomendações geradas automaticamente baseadas em alertas
- UI exibe recomendações com sugestões claras
- Botão "Apply" aplica mudança e registra no History
- Recomendações podem ser dismissed

---

### Fase 4: Integração com History Tracker (Estimativa: 3-4 horas)

**Objetivos:**
- ✅ Adicionar campos de alerta no `HistoryEntry`
- ✅ Capturar contexto de alerta ao aplicar mudanças
- ✅ Badge visual no `HistoryViewer`

**Tarefas:**
1. Atualizar struct `HistoryEntry` com campos de alerta
2. Modificar handlers (HPA, Node Pool) para capturar alertas ativos
3. Atualizar `HistoryViewer.tsx` com badge de alerta
4. Adicionar filtro "Applied due to alerts" no History
5. Testar correlação temporal (alerta → mudança → resolução)

**Critérios de Aceite:**
- History entries mostram alertas relacionados
- Badge "🔔 Applied due to alert" visível
- Filtro funciona corretamente
- Timeline completa: alerta fired → mudança aplicada → alerta resolved

---

### Fase 5: Regras de Alertas e Documentação (Estimativa: 2-3 horas)

**Objetivos:**
- ✅ Criar ConfigMap com regras de alerta
- ✅ Documentar instalação e troubleshooting
- ✅ Testes end-to-end

**Tarefas:**
1. Criar `prometheus-rules.yaml` com regras recomendadas
2. Documentar instalação no cluster
3. Criar guia de troubleshooting
4. Testes end-to-end: Disparar alerta → Gerar recomendação → Aplicar → Verificar History
5. Atualizar `CLAUDE.md` com referência a este plano

**Critérios de Aceite:**
- Regras instaladas e funcionando no Prometheus
- Alertas disparam corretamente
- Sistema completo funciona end-to-end
- Documentação clara e completa

---

## 🧪 Testes

### Testes Unitários (Backend)

```go
// internal/prometheus/alertmanager_test.go

func TestAlertmanagerClient_GetAlerts(t *testing.T) {
    // Mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        alerts := []Alert{
            {
                ID:    "alert-1",
                State: "firing",
                Labels: map[string]string{
                    "alertname": "HPAAtMaxReplicas",
                    "severity":  "warning",
                },
            },
        }
        json.NewEncoder(w).Encode(alerts)
    }))
    defer server.Close()

    client := NewAlertmanagerClient(server.URL)
    alerts, err := client.GetAlerts(context.Background(), nil)

    assert.NoError(t, err)
    assert.Len(t, alerts, 1)
    assert.Equal(t, "HPAAtMaxReplicas", alerts[0].Labels["alertname"])
}

func TestRecommendationEngine_Analyze(t *testing.T) {
    engine := NewRecommendationEngine(nil, nil)

    alert := Alert{
        ID:    "alert-1",
        State: "firing",
        Labels: map[string]string{
            "alertname":  "HPAAtMaxReplicas",
            "cluster":    "prod",
            "namespace":  "default",
            "horizontalpodautoscaler": "api-service",
        },
        Resource: "default/api-service",
        Cluster:  "prod",
    }

    recs := engine.analyzeAlert(alert)

    assert.NotEmpty(t, recs)
    assert.Equal(t, RecommendationScaleUp, recs[0].Type)
    assert.Equal(t, "maxReplicas", recs[0].SuggestedAction.Field)
}
```

### Testes de Integração

```bash
# 1. Disparar alerta manualmente (stress test)
kubectl run stress --image=polinux/stress --restart=Never -- stress --cpu 4

# 2. Verificar alerta no Alertmanager
curl http://alertmanager-prod.monitoring.svc.cluster.local:9093/api/v2/alerts | jq

# 3. Verificar alerta na API
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/alerts?cluster=prod" | jq

# 4. Verificar recomendações geradas
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/recommendations?cluster=prod" | jq

# 5. Aplicar recomendação via UI e verificar History
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/history" | jq '.data[] | select(.triggered_by_alert != null)'
```

### Testes End-to-End

**Cenário 1: HPA at Max Replicas**
1. Deploy app com HPA (maxReplicas=3)
2. Gerar carga (CPU > 80%)
3. Aguardar HPA escalar para 3 réplicas
4. Aguardar 5 minutos (alerta "HPAAtMaxReplicas" dispara)
5. Verificar alerta no AlertPanel (frontend)
6. Verificar recomendação gerada ("Increase maxReplicas to 5")
7. Clicar "Apply" na recomendação
8. Verificar HPA atualizado para maxReplicas=5
9. Verificar History entry com badge "🔔 Applied due to alert"
10. Verificar alerta resolvido após 5min

**Cenário 2: CPU Below Target**
1. Deploy app com HPA (targetCPU=70%)
2. Carga baixa (CPU < 35%)
3. Aguardar 30 minutos (alerta "HPACPUBelowTarget" dispara)
4. Verificar recomendação ("Lower targetCPU to 50%")
5. Aplicar recomendação
6. Verificar HPA atualizado
7. Verificar History

---

## 🔒 Segurança e Performance

### Segurança

**1. Autenticação ao Alertmanager**
```go
// Se Alertmanager requer autenticação
client := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            // Configuração TLS se necessário
        },
    },
    Timeout: 10 * time.Second,
}

req.Header.Set("Authorization", "Bearer "+token)
```

**2. Rate Limiting**
```go
// Limitar requisições ao Alertmanager (max 10 req/s)
rateLimiter := rate.NewLimiter(10, 1)

func (c *AlertmanagerClient) GetAlerts(ctx context.Context, filters map[string]string) ([]Alert, error) {
    if err := rateLimiter.Wait(ctx); err != nil {
        return nil, err
    }
    // ... fetch alerts
}
```

**3. Validação de Input**
```go
// Validar filtros antes de construir query
func validateFilters(filters map[string]string) error {
    allowedKeys := map[string]bool{
        "cluster":   true,
        "severity":  true,
        "alertname": true,
        "namespace": true,
    }

    for key := range filters {
        if !allowedKeys[key] {
            return fmt.Errorf("invalid filter key: %s", key)
        }
    }

    return nil
}
```

### Performance

**1. Caching de Alertas**
```go
type AlertCache struct {
    cache map[string]cachedAlerts
    mu    sync.RWMutex
    ttl   time.Duration
}

type cachedAlerts struct {
    alerts    []Alert
    expiresAt time.Time
}

func (c *AlertCache) Get(key string) ([]Alert, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    cached, exists := c.cache[key]
    if !exists || time.Now().After(cached.expiresAt) {
        return nil, false
    }

    return cached.alerts, true
}

func (c *AlertCache) Set(key string, alerts []Alert) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.cache[key] = cachedAlerts{
        alerts:    alerts,
        expiresAt: time.Now().Add(c.ttl),
    }
}
```

**2. Polling Inteligente no Frontend**
```typescript
// Apenas fazer polling se componente visível
useEffect(() => {
    if (!isVisible) return; // Skip polling se componente não visível

    const interval = setInterval(fetchAlerts, 30000);
    return () => clearInterval(interval);
}, [isVisible]);

// Usar IntersectionObserver para detectar visibilidade
const [isVisible, setIsVisible] = useState(false);
const ref = useRef<HTMLDivElement>(null);

useEffect(() => {
    const observer = new IntersectionObserver(
        ([entry]) => setIsVisible(entry.isIntersecting),
        { threshold: 0.1 }
    );

    if (ref.current) observer.observe(ref.current);

    return () => observer.disconnect();
}, []);
```

**3. Lazy Loading de Recomendações**
```typescript
// Apenas buscar recomendações se houver alertas
useEffect(() => {
    if (alertCount === 0) {
        setRecommendations([]);
        return;
    }

    fetchRecommendations();
}, [alertCount]);
```

---

## 📚 Referências

- **Alertmanager API**: https://prometheus.io/docs/alerting/latest/alertmanager/
- **Prometheus Recording Rules**: https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/
- **Kubernetes HPA Metrics**: https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/
- **METRICS_INTEGRATION_PLAN.md**: Plano complementar para integração de métricas
- **CLAUDE.md**: Documentação principal do projeto

---

## ✅ Checklist de Implementação

### Backend
- [ ] Criar `internal/prometheus/alertmanager.go` (AlertmanagerClient)
- [ ] Criar `internal/prometheus/recommendations.go` (RecommendationEngine)
- [ ] Criar `internal/web/handlers/alerts.go` (AlertHandler)
- [ ] Criar `internal/web/handlers/recommendations.go` (RecommendationHandler)
- [ ] Adicionar rotas no `server.go`
- [ ] Atualizar `HistoryEntry` com campos de alerta
- [ ] Modificar handlers HPA/Node Pool para capturar alertas
- [ ] Adicionar testes unitários
- [ ] Adicionar testes de integração

### Frontend
- [ ] Criar `AlertPanel.tsx` (lista de alertas)
- [ ] Criar `RecommendationsWidget.tsx` (recomendações)
- [ ] Adicionar badge de alertas no `Header.tsx`
- [ ] Integrar AlertPanel no `Index.tsx`
- [ ] Integrar RecommendationsWidget no `Index.tsx`
- [ ] Atualizar `HistoryViewer.tsx` com badge de alerta
- [ ] Implementar deep link (alerta → HPA Editor)
- [ ] Implementar ação "Apply" em recomendações
- [ ] Adicionar filtro "Applied due to alerts" no History

### Infraestrutura
- [ ] Criar `prometheus-rules.yaml` (ConfigMap)
- [ ] Instalar regras no cluster
- [ ] Validar regras com `promtool`
- [ ] Configurar Alertmanager (se necessário)
- [ ] Atualizar `clusters-config.json` com URLs do Alertmanager

### Documentação
- [ ] Atualizar `CLAUDE.md` com referência a este plano
- [ ] Criar guia de troubleshooting
- [ ] Documentar instalação das regras
- [ ] Criar exemplos de uso
- [ ] Adicionar entry no histórico de correções (quando concluído)

---

**Última atualização:** 02 de novembro de 2025
**Status:** Plano aprovado, aguardando implementação
**Próximo passo:** Fase 1 - Backend Alertmanager Client
