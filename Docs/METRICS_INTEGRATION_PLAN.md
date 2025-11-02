# Metrics Integration - Plano de Implementação

**Data:** 02 de novembro de 2025
**Versão:** 1.0
**Status:** Proposta

---

## 📊 Objetivo

Integrar métricas em tempo real do Kubernetes Metrics Server para exibir uso REAL de CPU/Memory ao lado dos targets configurados nos HPAs.

---

## 🎯 Benefícios

1. **Visibilidade completa**: Ver uso real vs target configurado
2. **Validação de HPAs**: Verificar se HPA está funcionando corretamente
3. **Troubleshooting**: Identificar HPAs que não estão scaling
4. **Decisões informadas**: Ajustar targets baseado em uso real
5. **Alertas visuais**: Destacar HPAs que estão acima/abaixo do target

---

## 🏗️ Arquitetura

### Backend (Go)

#### 1. Cliente Metrics Server

**Arquivo**: `internal/kubernetes/metrics.go` (NOVO)

```go
package kubernetes

import (
    "context"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
    metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// MetricsClient encapsula acesso ao Metrics Server
type MetricsClient struct {
    client *metricsclientset.Clientset
}

// NewMetricsClient cria um cliente para o Metrics Server
func NewMetricsClient(restConfig *rest.Config) (*MetricsClient, error) {
    metricsClient, err := metricsclientset.NewForConfig(restConfig)
    if err != nil {
        return nil, err
    }
    return &MetricsClient{client: metricsClient}, nil
}

// GetPodMetrics retorna métricas de todos os pods de um deployment
func (m *MetricsClient) GetPodMetrics(ctx context.Context, namespace, deploymentName string) (*PodMetrics, error) {
    // 1. Buscar pods do deployment via label selector
    // 2. Obter métricas de cada pod
    // 3. Agregar CPU/Memory total e média

    podMetricsList, err := m.client.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("app=%s", deploymentName),
    })

    if err != nil {
        return nil, err
    }

    // Calcular agregações
    var totalCPU, totalMemory int64
    var avgCPU, avgMemory int64
    podCount := len(podMetricsList.Items)

    for _, podMetrics := range podMetricsList.Items {
        for _, container := range podMetrics.Containers {
            cpu := container.Usage.Cpu().MilliValue()  // CPU em milicores
            memory := container.Usage.Memory().Value() // Memory em bytes

            totalCPU += cpu
            totalMemory += memory
        }
    }

    if podCount > 0 {
        avgCPU = totalCPU / int64(podCount)
        avgMemory = totalMemory / int64(podCount)
    }

    return &PodMetrics{
        TotalCPU:    totalCPU,
        TotalMemory: totalMemory,
        AvgCPU:      avgCPU,
        AvgMemory:   avgMemory,
        PodCount:    podCount,
    }, nil
}
```

#### 2. Modelo de Dados

**Arquivo**: `internal/models/types.go` (modificar HPA struct)

```go
type HPA struct {
    // ... campos existentes

    // Métricas em tempo real (NOVO)
    CurrentCPUUsage      *int64  `json:"current_cpu_usage,omitempty"`       // CPU atual em milicores
    CurrentMemoryUsage   *int64  `json:"current_memory_usage,omitempty"`    // Memory atual em bytes
    CurrentCPUPercent    *int    `json:"current_cpu_percent,omitempty"`     // % em relação ao request
    CurrentMemoryPercent *int    `json:"current_memory_percent,omitempty"`  // % em relação ao request
    MetricsAvailable     bool    `json:"metrics_available"`                 // Se métricas estão disponíveis
    MetricsError         *string `json:"metrics_error,omitempty"`           // Erro ao buscar métricas
    LastMetricsUpdate    *string `json:"last_metrics_update,omitempty"`     // Timestamp da última atualização
}

type PodMetrics struct {
    TotalCPU    int64 `json:"total_cpu"`     // CPU total em milicores
    TotalMemory int64 `json:"total_memory"`  // Memory total em bytes
    AvgCPU      int64 `json:"avg_cpu"`       // CPU média por pod
    AvgMemory   int64 `json:"avg_memory"`    // Memory média por pod
    PodCount    int   `json:"pod_count"`     // Número de pods
}
```

#### 3. Enriquecimento de HPAs com Métricas

**Arquivo**: `internal/kubernetes/client.go` (adicionar função)

```go
// EnrichHPAWithMetrics adiciona métricas em tempo real ao HPA
func (c *Client) EnrichHPAWithMetrics(ctx context.Context, hpa *models.HPA, metricsClient *MetricsClient) error {
    if metricsClient == nil {
        hpa.MetricsAvailable = false
        return nil
    }

    // Obter nome do deployment do HPA
    deploymentName := hpa.ScaleTargetRef.Name

    // Buscar métricas dos pods
    metrics, err := metricsClient.GetPodMetrics(ctx, hpa.Namespace, deploymentName)
    if err != nil {
        errMsg := err.Error()
        hpa.MetricsError = &errMsg
        hpa.MetricsAvailable = false
        return nil // Não falhar, apenas marcar como indisponível
    }

    hpa.MetricsAvailable = true
    hpa.CurrentCPUUsage = &metrics.AvgCPU
    hpa.CurrentMemoryUsage = &metrics.AvgMemory

    // Calcular % em relação ao request configurado
    if hpa.TargetCPURequest != "" {
        cpuRequest := parseResourceValue(hpa.TargetCPURequest) // Ex: "500m" → 500
        if cpuRequest > 0 {
            cpuPercent := int((metrics.AvgCPU * 100) / cpuRequest)
            hpa.CurrentCPUPercent = &cpuPercent
        }
    }

    if hpa.TargetMemoryRequest != "" {
        memRequest := parseResourceValue(hpa.TargetMemoryRequest) // Ex: "512Mi" → bytes
        if memRequest > 0 {
            memPercent := int((metrics.AvgMemory * 100) / memRequest)
            hpa.CurrentMemoryPercent = &memPercent
        }
    }

    now := time.Now().Format(time.RFC3339)
    hpa.LastMetricsUpdate = &now

    return nil
}
```

#### 4. Handler API

**Arquivo**: `internal/web/handlers/hpas.go` (modificar List)

```go
func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    namespace := c.Query("namespace")
    includeMetrics := c.Query("include_metrics") == "true" // Opcional

    // ... código existente para buscar HPAs

    if includeMetrics {
        // Criar cliente de métricas
        metricsClient, err := kubeclient.NewMetricsClient(client)
        if err != nil {
            // Log erro mas não falhar a requisição
            fmt.Printf("⚠️ Metrics Server não disponível: %v\n", err)
        } else {
            // Enriquecer cada HPA com métricas
            for i := range allHPAs {
                _ = kubeClient.EnrichHPAWithMetrics(c.Request.Context(), &allHPAs[i], metricsClient)
            }
        }
    }

    c.JSON(200, gin.H{
        "success": true,
        "data":    allHPAs,
        "count":   len(allHPAs),
    })
}
```

---

### Frontend (React/TypeScript)

#### 1. Componente de Métricas

**Arquivo**: `internal/web/frontend/src/components/MetricsBadge.tsx` (NOVO)

```typescript
import { Badge } from "@/components/ui/badge";
import { TrendingUp, TrendingDown, Minus } from "lucide-react";

interface MetricsBadgeProps {
  current?: number;      // Valor atual (%)
  target?: number;       // Target configurado (%)
  available: boolean;    // Se métricas estão disponíveis
  error?: string;        // Erro ao buscar métricas
}

export const MetricsBadge = ({ current, target, available, error }: MetricsBadgeProps) => {
  if (!available) {
    return (
      <Badge variant="outline" className="text-gray-400">
        N/A {error && <span title={error}>⚠️</span>}
      </Badge>
    );
  }

  if (current === undefined || target === undefined) {
    return <Badge variant="outline">-</Badge>;
  }

  // Calcular variação do target
  const diff = current - target;
  const absDiff = Math.abs(diff);

  // Cores baseadas na variação
  let variant: "default" | "success" | "warning" | "destructive" = "default";
  let Icon = Minus;

  if (diff > 20) {
    variant = "destructive"; // Muito acima do target
    Icon = TrendingUp;
  } else if (diff > 5) {
    variant = "warning"; // Acima do target
    Icon = TrendingUp;
  } else if (diff < -20) {
    variant = "warning"; // Muito abaixo do target
    Icon = TrendingDown;
  } else if (diff < -5) {
    variant = "default"; // Abaixo do target
    Icon = TrendingDown;
  } else {
    variant = "success"; // Dentro do target ±5%
    Icon = Minus;
  }

  return (
    <Badge variant={variant} className="flex items-center gap-1">
      <Icon className="w-3 h-3" />
      {current}% (target: {target}%)
      {absDiff > 5 && <span className="ml-1 text-xs">±{absDiff}%</span>}
    </Badge>
  );
};
```

#### 2. Integração no HPACard

**Arquivo**: `internal/web/frontend/src/pages/Index.tsx` (modificar)

```typescript
import { MetricsBadge } from "@/components/MetricsBadge";

// No fetch de HPAs, adicionar query param
const { data: hpasData } = useQuery({
  queryKey: ["hpas", selectedCluster, selectedNamespaces, includeMetrics],
  queryFn: () => apiClient.getHPAs(selectedCluster, selectedNamespaces, {
    includeMetrics: true  // <-- NOVO
  }),
  enabled: !!selectedCluster && selectedNamespaces.length > 0,
});

// No render do card de HPA
<div className="grid grid-cols-2 gap-2 text-sm">
  <div>
    <span className="text-muted-foreground">Min/Max Replicas:</span>
    <span className="ml-2 font-medium">{hpa.min_replicas}/{hpa.max_replicas}</span>
  </div>

  {/* NOVO: CPU Metrics */}
  {hpa.target_cpu && (
    <div>
      <span className="text-muted-foreground">CPU:</span>
      <MetricsBadge
        current={hpa.current_cpu_percent}
        target={hpa.target_cpu}
        available={hpa.metrics_available}
        error={hpa.metrics_error}
      />
    </div>
  )}

  {/* NOVO: Memory Metrics */}
  {hpa.target_memory && (
    <div>
      <span className="text-muted-foreground">Memory:</span>
      <MetricsBadge
        current={hpa.current_memory_percent}
        target={hpa.target_memory}
        available={hpa.metrics_available}
        error={hpa.metrics_error}
      />
    </div>
  )}
</div>

{/* NOVO: Última atualização */}
{hpa.last_metrics_update && (
  <div className="text-xs text-muted-foreground mt-2">
    Última atualização: {new Date(hpa.last_metrics_update).toLocaleString()}
  </div>
)}
```

#### 3. Auto-refresh de Métricas

**Arquivo**: `internal/web/frontend/src/pages/Index.tsx`

```typescript
// Auto-refresh a cada 30 segundos quando métricas estão habilitadas
const { data: hpasData } = useQuery({
  queryKey: ["hpas", selectedCluster, selectedNamespaces, includeMetrics],
  queryFn: () => apiClient.getHPAs(selectedCluster, selectedNamespaces, {
    includeMetrics: true
  }),
  enabled: !!selectedCluster && selectedNamespaces.length > 0,
  refetchInterval: includeMetrics ? 30000 : false, // 30 segundos
});

// Toggle para habilitar/desabilitar métricas
const [includeMetrics, setIncludeMetrics] = useState(false);

<Switch
  checked={includeMetrics}
  onCheckedChange={setIncludeMetrics}
  label="Mostrar métricas em tempo real"
/>
```

---

## 🔧 Dependências

### Go Modules

```bash
# Adicionar ao go.mod
go get k8s.io/metrics@v0.31.4
```

### Requisitos de Cluster

1. **Metrics Server instalado** no cluster K8s:
   ```bash
   kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
   ```

2. **RBAC permissions** para ler métricas:
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: metrics-reader
   rules:
   - apiGroups: ["metrics.k8s.io"]
     resources: ["pods", "nodes"]
     verbs: ["get", "list"]
   ```

---

## 📊 UI/UX Mockup

```
┌─────────────────────────────────────────────────────┐
│ HPA: my-app-hpa                    [⋮]              │
├─────────────────────────────────────────────────────┤
│ Min/Max Replicas: 2/10                              │
│ CPU:        [🔴 95%] (target: 80%) +15%            │
│             ^^^^^^^^ Vermelho - acima do target     │
│ Memory:     [🟢 72%] (target: 75%) -3%             │
│             ^^^^^^^^ Verde - dentro do target       │
│                                                     │
│ Última atualização: 02/11/2025 15:32:45           │
└─────────────────────────────────────────────────────┘
```

**Cores:**
- 🟢 Verde: Dentro do target (±5%)
- 🟡 Amarelo: Fora do target (±5-20%)
- 🔴 Vermelho: Muito fora do target (>20%)
- ⚪ Cinza: Métricas indisponíveis

---

## 🚀 Implementação em Fases

### Fase 1: Backend Básico (1-2 dias)
- [ ] Criar `MetricsClient` em `internal/kubernetes/metrics.go`
- [ ] Adicionar campos no modelo `HPA`
- [ ] Implementar `EnrichHPAWithMetrics()`
- [ ] Modificar handler `List()` com `include_metrics` param
- [ ] Testar com Metrics Server real

### Fase 2: Frontend Básico (1 dia)
- [ ] Criar `MetricsBadge` component
- [ ] Integrar no HPA card
- [ ] Adicionar toggle de métricas
- [ ] Testar exibição e cores

### Fase 3: Auto-refresh (1 dia)
- [ ] Implementar refetch interval
- [ ] Adicionar loading states
- [ ] Otimizar performance (cache)

### Fase 4: Features Avançadas (2-3 dias)
- [ ] Gráficos de tendência (Recharts)
- [ ] Alertas visuais quando muito fora do target
- [ ] Histórico de métricas (opcional)
- [ ] Export de métricas

---

## ⚠️ Considerações

### Performance
- **Cache**: Cachear métricas por 10-30 segundos no backend
- **Opcional**: Parâmetro `include_metrics` evita overhead quando não necessário
- **Lazy loading**: Buscar métricas apenas quando tab de HPAs está visível

### Erros Comuns
- **Metrics Server não instalado**: Mostrar mensagem clara no UI
- **RBAC insuficiente**: Log erro e marcar como indisponível
- **Timeout**: Timeout de 5 segundos para evitar travamentos

### Escalabilidade
- Para 100+ HPAs: Considerar paginação ou busca sob demanda
- Implementar debounce no auto-refresh

---

## 📝 Testes

### Unitários
```go
func TestEnrichHPAWithMetrics(t *testing.T) {
    // Mock MetricsClient
    // Testar cálculo de percentuais
    // Testar erro handling
}
```

### Integração
```bash
# 1. Criar deployment de teste
kubectl create deployment test-app --image=nginx --replicas=3

# 2. Criar HPA
kubectl autoscale deployment test-app --cpu-percent=80 --min=2 --max=10

# 3. Gerar carga
kubectl run -it --rm load-generator --image=busybox /bin/sh
# while true; do wget -q -O- http://test-app; done

# 4. Verificar métricas na API
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/hpas?cluster=test&namespace=default&include_metrics=true"
```

---

## 🎯 Resultado Esperado

Após implementação, o usuário terá:

1. **Visibilidade completa** do uso real vs configurado
2. **Identificação rápida** de HPAs problemáticos (cores)
3. **Decisões informadas** sobre ajustes de targets
4. **Troubleshooting facilitado** com métricas em tempo real
5. **Auto-refresh** mantém dados atualizados

---

## 📚 Referências

- [Kubernetes Metrics API](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/)
- [Metrics Server](https://github.com/kubernetes-sigs/metrics-server)
- [Go client-go metrics](https://pkg.go.dev/k8s.io/metrics)

---

**Autor**: Claude Code
**Última atualização**: 02 de novembro de 2025
