# 📊 Estudo: Integração HPAs com Alertmanager para Mitigação de Falhas

**Data:** 19 de novembro de 2025
**Versão:** 1.0
**Status:** Estudo Técnico

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 📋 Sumário Executivo

Este documento apresenta um estudo detalhado sobre a integração da aba HPAs (painel "Available HPAs") com o Alertmanager do Prometheus, com foco em identificação proativa de alertas e registro de eventos para mitigação de falhas.

### 🎯 Objetivos

1. **Identificação Proativa**: Detectar problemas antes que causem indisponibilidade
2. **Correlação Inteligente**: Associar alertas do Alertmanager aos HPAs específicos
3. **Mitigação Automatizada**: Sugerir ou aplicar ações corretivas baseadas em alertas
4. **Rastreabilidade**: Registrar eventos de alerta e ações tomadas no History Tracker

---

## 🏗️ Arquitetura Atual

### Componentes Existentes

#### 1. **Prometheus Integration**
- **Localização**: `internal/monitoring/prometheus/client.go`
- **Funcionalidade**: Client wrapper para Prometheus API
- **Status**: ✅ Implementado
- **Features**:
  - Query e QueryRange PromQL
  - Lazy connection
  - Teste de conectividade

#### 2. **Monitoring V2**
- **Localização**: `internal/monitoring/engine/monitoring_v2.go`
- **Funcionalidade**: Engine de monitoramento de métricas
- **Status**: ✅ Implementado
- **Features**:
  - Coleta de métricas de CPU/Memória
  - Detecção de anomalias
  - Análise de comportamento

#### 3. **Alerts Panel**
- **Localização**: `internal/web/frontend/src/components/AlertsPanel.tsx`
- **Funcionalidade**: Interface visual para anomalias
- **Status**: ✅ Implementado
- **Features**:
  - Exibição de anomalias por severidade
  - Filtros por cluster
  - Badges visuais (low, medium, high, critical)

#### 4. **HPA Tab**
- **Localização**: `internal/web/frontend/src/components/HPATab.tsx`
- **Funcionalidade**: Interface de gerenciamento de HPAs
- **Status**: ✅ Implementado
- **Features**:
  - Listagem de HPAs por cluster/namespace
  - Editor de HPAs
  - Seleção de cluster/namespace

#### 5. **History Tracker**
- **Localização**: `internal/history/tracker.go`
- **Funcionalidade**: Audit log de operações
- **Status**: ✅ Implementado (v1.1.1)
- **Features**:
  - Rastreamento de ações (Cordon/Drain/Rollouts)
  - Persistência em JSON
  - Filtros e estatísticas

---

## 🔍 Gap Analysis

### O Que Falta para Integração Completa

| Componente | Status | Prioridade |
|------------|--------|------------|
| **Alertmanager Client** | ❌ Não implementado | 🔴 Alta |
| **Alert-HPA Correlation** | ❌ Não implementado | 🔴 Alta |
| **Alert Event Logger** | ❌ Não implementado | 🟡 Média |
| **Mitigation Actions** | ❌ Não implementado | 🟡 Média |
| **Alert Visualization in HPAs** | ❌ Não implementado | 🔴 Alta |
| **Webhook Receiver** | ❌ Não implementado | 🟢 Baixa |

---

## 📊 Tipos de Alertas Relevantes para HPAs

### Resumo Geral de Alertas

| Categoria | Alert | Severidade | Auto-Mitigável |
|-----------|-------|------------|----------------|
| **Recursos** | KubePodCrashLooping | 🔴 Critical | ❌ |
| **Recursos** | HPAMaxedOut | 🟡 Warning | ✅ |
| **Recursos** | HPAScaleCapability | 🔴 Critical | ❌ |
| **Performance** | HighCPUUsage | 🟡 Warning | ✅ |
| **Performance** | MemoryPressure | 🔴 Critical | ✅ |
| **Disponibilidade** | PodNotReady | 🔴 Critical | ❌ |
| **Disponibilidade** | PodsPending | 🔴 Critical | ⚠️ Parcial |
| **Memória** | OOMKilled | 🔴 Critical | ✅ |
| **Memória** | PodMemoryLimitReached | 🟡 Warning | ✅ |
| **Estado** | ImagePullBackOff | 🔴 Critical | ❌ |
| **Estado** | CrashLoopBackOff | 🔴 Critical | ⚠️ Parcial |
| **Estado** | ContainerRestarting | 🟡 Warning | ❌ |
| **Cluster** | NodeDiskPressure | 🔴 Critical | ❌ |
| **Cluster** | NodeMemoryPressure | 🔴 Critical | ❌ |
| **Cluster** | NodeNotReady | 🔴 Critical | ❌ |
| **Node Pool** | NodePoolMaxedOut | 🔴 Critical | ✅ |
| **Node Pool** | NodePoolNearMaxCapacity | 🟡 Warning | ⚠️ Parcial |
| **Node Pool** | NodePoolMinCountReached | ℹ️ Info | ✅ |
| **Node Pool** | NodePoolScalingBlocked | 🔴 Critical | ❌ |
| **Node Pool** | NodePoolUnderutilized | ℹ️ Info | ⚠️ Parcial |
| **Node Pool** | NodePoolRapidScaling | 🟡 Warning | ❌ |
| **Node Pool** | NodePoolCostThreshold | 🟡 Warning | ❌ |
| **HPA** | HPATargetMissing | 🔴 Critical | ❌ |
| **HPA** | HPAMetricsUnavailable | 🔴 Critical | ❌ |

**Total de Alertas: 24**

**Legenda:**
- ✅ **Auto-Mitigável**: Ação pode ser aplicada automaticamente com segurança
- ⚠️ **Parcial**: Algumas ações podem ser automatizadas, outras requerem intervenção manual
- ❌ **Manual**: Requer análise e intervenção humana

**Por Categoria:**
- Recursos: 3 alertas
- Performance: 2 alertas
- Disponibilidade: 2 alertas
- Memória: 2 alertas
- Estado: 3 alertas
- Cluster: 3 alertas
- **Node Pool: 7 alertas** ⭐
- HPA: 2 alertas

---

### 1. **Alerts de Recursos (Resource Alerts)**

#### **KubePodCrashLooping**
- **Descrição**: Pod em loop de crash
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar logs do pod
  - Rollback se deployment recente
  - Aumentar limites de recursos

```promql
ALERT KubePodCrashLooping
  IF rate(kube_pod_container_status_restarts_total[15m]) > 0
  FOR 5m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Pod {{ $labels.namespace }}/{{ $labels.pod }} está crashando"
  }
```

#### **HPAMaxedOut**
- **Descrição**: HPA atingiu limite máximo de réplicas
- **Severidade**: 🟡 Warning
- **Ação Sugerida**:
  - Aumentar maxReplicas
  - Investigar causa da alta demanda
  - Revisar targetCPUUtilizationPercentage

```promql
ALERT HPAMaxedOut
  IF kube_hpa_status_current_replicas >= kube_hpa_spec_max_replicas
  FOR 10m
  LABELS { severity = "warning" }
  ANNOTATIONS {
    summary = "HPA {{ $labels.namespace }}/{{ $labels.hpa }} atingiu limite máximo"
  }
```

#### **HPAScaleCapability**
- **Descrição**: HPA não consegue escalar
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar metrics-server
  - Validar targetRef existe
  - Checar RBAC permissions

```promql
ALERT HPAScaleCapability
  IF kube_hpa_status_condition{condition="ScalingActive",status="false"} == 1
  FOR 5m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "HPA {{ $labels.namespace }}/{{ $labels.hpa }} não consegue escalar"
  }
```

### 2. **Alerts de Performance**

#### **HighCPUUsage**
- **Descrição**: CPU alta sustentada
- **Severidade**: 🟡 Warning
- **Ação Sugerida**:
  - Aumentar CPU requests/limits
  - Escalar horizontalmente
  - Otimizar código/queries

```promql
ALERT HighCPUUsage
  IF rate(container_cpu_usage_seconds_total[5m]) > 0.8
  FOR 15m
  LABELS { severity = "warning" }
```

#### **MemoryPressure**
- **Descrição**: Pressão de memória alta
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Aumentar memory limits
  - Investigar memory leaks
  - Restart pods se necessário

```promql
ALERT MemoryPressure
  IF container_memory_working_set_bytes / container_spec_memory_limit_bytes > 0.9
  FOR 5m
  LABELS { severity = "critical" }
```

### 3. **Alerts de Disponibilidade**

#### **PodNotReady**
- **Descrição**: Pods não estão ready
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar readiness probes
  - Checar logs de startup
  - Validar dependências

```promql
ALERT PodNotReady
  IF kube_pod_status_phase{phase!~"Running|Succeeded"} > 0
  FOR 10m
  LABELS { severity = "critical" }
```

#### **PodsPending**
- **Descrição**: Pods em estado Pending (não conseguem ser agendados)
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar recursos disponíveis no cluster
  - Checar node selectors/affinity rules
  - Validar PVCs (se houver)
  - Aumentar nodes no cluster
  - Revisar resource requests (podem estar muito altos)

```promql
ALERT PodsPending
  IF kube_pod_status_phase{phase="Pending"} > 0
  FOR 5m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Pod {{ $labels.namespace }}/{{ $labels.pod }} está Pending há 5min",
    description = "Pod não consegue ser agendado. Verifique recursos do cluster."
  }
```

**Causas Comuns:**
- Recursos insuficientes (CPU/Memory)
- Node selector não encontra nodes compatíveis
- Taints/Tolerations incompatíveis
- PersistentVolumeClaim pendente
- ImagePullBackOff (imagem não disponível)

---

### 4. **Alerts de Memória Críticos**

#### **OOMKilled**
- **Descrição**: Container foi morto por falta de memória (Out Of Memory)
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - **IMEDIATO**: Aumentar memory limits
  - Analisar memory usage patterns
  - Investigar memory leaks
  - Otimizar código/queries
  - Considerar vertical pod autoscaling

```promql
ALERT ContainerOOMKilled
  IF rate(kube_pod_container_status_restarts_total{reason="OOMKilled"}[15m]) > 0
  FOR 1m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Container {{ $labels.namespace }}/{{ $labels.pod }}/{{ $labels.container }} foi OOMKilled",
    description = "Container excedeu memory limit. Aumentar limits ou investigar memory leak."
  }
```

**Informações Adicionais:**
- Memory limit atual vs usage
- Histórico de memory usage (últimas 24h)
- Recomendação de novo limit (usage máximo + 20% buffer)
- Link para logs/métricas

**Exemplo de Mitigação Automática:**
```yaml
# Estado atual
memory:
  limit: "512Mi"
  usage_max: "510Mi"  # 99.6% do limit!

# Ação sugerida
memory:
  limit: "768Mi"  # +50% do atual
  requests: "512Mi"  # Garantir reserva
```

#### **PodMemoryLimitReached**
- **Descrição**: Pod está próximo do memory limit (risco de OOMKilled)
- **Severidade**: 🟡 Warning (preventivo)
- **Ação Sugerida**:
  - Aumentar memory limits antes de OOMKill
  - Análise proativa de tendências
  - Alertar equipe de desenvolvimento

```promql
ALERT PodMemoryLimitReached
  IF (container_memory_working_set_bytes / container_spec_memory_limit_bytes) > 0.95
  FOR 10m
  LABELS { severity = "warning" }
  ANNOTATIONS {
    summary = "Pod {{ $labels.namespace }}/{{ $labels.pod }} usando >95% do memory limit",
    description = "Risco de OOMKilled. Aumentar limits preventivamente."
  }
```

---

### 5. **Alerts de Estado de Pods**

#### **ImagePullBackOff**
- **Descrição**: Kubernetes não consegue baixar a imagem do container
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar se imagem existe no registry
  - Validar image pull secrets
  - Checar conectividade com registry
  - Corrigir tag da imagem

```promql
ALERT ImagePullBackOff
  IF kube_pod_container_status_waiting_reason{reason="ImagePullBackOff"} > 0
  FOR 5m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Pod {{ $labels.namespace }}/{{ $labels.pod }} com ImagePullBackOff",
    description = "Imagem {{ $labels.image }} não pode ser baixada."
  }
```

#### **CrashLoopBackOff**
- **Descrição**: Pod está em loop de crash e restart
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Analisar logs do container
  - Verificar liveness/readiness probes
  - Validar variáveis de ambiente
  - Rollback se deployment recente
  - Aumentar resources se OOMKilled

```promql
ALERT CrashLoopBackOff
  IF kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"} > 0
  FOR 5m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Pod {{ $labels.namespace }}/{{ $labels.pod }} em CrashLoopBackOff",
    description = "Container reiniciando continuamente. Checar logs."
  }
```

#### **ContainerRestarting**
- **Descrição**: Container reiniciando frequentemente
- **Severidade**: 🟡 Warning
- **Ação Sugerida**:
  - Investigar causa dos restarts
  - Checar logs de erro
  - Validar health checks
  - Considerar aumentar resources

```promql
ALERT ContainerRestarting
  IF rate(kube_pod_container_status_restarts_total[15m]) > 0
  FOR 5m
  LABELS { severity = "warning" }
  ANNOTATIONS {
    summary = "Container {{ $labels.namespace }}/{{ $labels.pod }}/{{ $labels.container }} reiniciando",
    description = "{{ $value }} restarts nos últimos 15min"
  }
```

---

### 6. **Alerts de Recursos do Cluster**

#### **NodeDiskPressure**
- **Descrição**: Node com pressão de disco
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Limpar logs antigos
  - Remover imagens não utilizadas
  - Verificar persistent volumes
  - Adicionar storage ao node

```promql
ALERT NodeDiskPressure
  IF kube_node_status_condition{condition="DiskPressure",status="true"} > 0
  FOR 5m
  LABELS { severity = "critical" }
```

#### **NodeMemoryPressure**
- **Descrição**: Node com pressão de memória
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Escalar cluster (adicionar nodes)
  - Drenar pods do node
  - Revisar memory requests dos pods

```promql
ALERT NodeMemoryPressure
  IF kube_node_status_condition{condition="MemoryPressure",status="true"} > 0
  FOR 5m
  LABELS { severity = "critical" }
```

#### **NodeNotReady**
- **Descrição**: Node não está pronto para receber workloads
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Investigar health do node
  - Verificar kubelet status
  - Checar conectividade de rede
  - Considerar replace do node

```promql
ALERT NodeNotReady
  IF kube_node_status_condition{condition="Ready",status!="true"} > 0
  FOR 5m
  LABELS { severity = "critical" }
```

---

### 8. **Alerts de Node Pool Scaling (Azure AKS)** ⭐ NOVO

#### **NodePoolMaxedOut**
- **Descrição**: Node Pool atingiu o limite máximo de nodes configurado
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - **IMEDIATO**: Aumentar node_count_max do Node Pool
  - Analisar se é pico temporário ou crescimento sustentado
  - Considerar otimização de resources dos pods
  - Avaliar vertical scaling antes de horizontal

```promql
ALERT NodePoolMaxedOut
  IF (
    count(kube_node_info{node=~".*nodepool.*"})
    >=
    on(nodepool) group_left()
    azure_nodepool_max_count
  )
  FOR 10m
  LABELS { severity = "critical", component = "nodepool" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} atingiu limite máximo de nodes",
    description = "Current: {{ $value }} nodes, Max: {{ $labels.max_count }}. Aumentar node_count_max."
  }
```

**Informações Adicionais:**
- Node count atual vs max configurado
- Tendência de crescimento (últimas 24h)
- Utilização média de recursos dos nodes
- Custo estimado de aumentar max_count
- Histórico de scaling events

**Exemplo de Contexto:**
```yaml
# Estado atual
nodepool: agentpool-prod
node_count: 10
node_count_max: 10  # ⚠️ MAXED OUT!
node_count_min: 3

# Análise
pods_pending: 15
avg_cpu_usage: 85%
avg_memory_usage: 78%
trend: crescimento sustentado

# Ação sugerida
new_node_count_max: 15  # +50%
estimated_cost: +$450/mês
```

#### **NodePoolNearMaxCapacity**
- **Descrição**: Node Pool próximo do limite máximo (>90%)
- **Severidade**: 🟡 Warning (preventivo)
- **Ação Sugerida**:
  - Planejar aumento de node_count_max
  - Monitorar tendência de crescimento
  - Preparar aprovação de budget

```promql
ALERT NodePoolNearMaxCapacity
  IF (
    (count(kube_node_info{node=~".*nodepool.*"}) / azure_nodepool_max_count) > 0.9
  )
  FOR 15m
  LABELS { severity = "warning", component = "nodepool" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} usando >90% da capacidade máxima",
    description = "{{ $value }}% de {{ $labels.max_count }} nodes. Planejar aumento."
  }
```

#### **NodePoolMinCountReached**
- **Descrição**: Node Pool está no mínimo de nodes (scale down bloqueado)
- **Severidade**: ℹ️ Info
- **Ação Sugerida**:
  - Verificar se min_count está adequado
  - Considerar reduzir se workload diminuiu
  - Otimizar custos

```promql
ALERT NodePoolMinCountReached
  IF (
    count(kube_node_info{node=~".*nodepool.*"})
    <=
    on(nodepool) group_left()
    azure_nodepool_min_count
  )
  FOR 30m
  LABELS { severity = "info", component = "nodepool" }
```

#### **NodePoolScalingBlocked**
- **Descrição**: Autoscaler não consegue adicionar nodes (quota excedida, limite regional, etc)
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar quotas do Azure
  - Checar limites regionais
  - Validar service principal permissions
  - Considerar multi-region deployment

```promql
ALERT NodePoolScalingBlocked
  IF (
    rate(cluster_autoscaler_failed_scale_ups_total[10m]) > 0
    AND
    (count(kube_node_info) >= azure_nodepool_max_count)
  )
  FOR 5m
  LABELS { severity = "critical", component = "nodepool" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} não consegue escalar",
    description = "Autoscaler falhando. Verificar quotas Azure e limites."
  }
```

#### **NodePoolUnderutilized**
- **Descrição**: Node Pool com baixa utilização (oportunidade de reduzir custos)
- **Severidade**: ℹ️ Info
- **Ação Sugerida**:
  - Reduzir node_count_max se tendência de queda
  - Consolidar workloads
  - Ajustar min_count para economizar

```promql
ALERT NodePoolUnderutilized
  IF (
    avg(rate(node_cpu_seconds_total{mode="idle"}[15m])) > 0.7
    AND
    avg(node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) > 0.5
  )
  FOR 1h
  LABELS { severity = "info", component = "nodepool" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} subutilizado",
    description = "CPU: {{ $value }}% idle. Considerar reduzir nodes."
  }
```

#### **NodePoolRapidScaling**
- **Descrição**: Node Pool escalando muito rapidamente (potencial problema)
- **Severidade**: 🟡 Warning
- **Ação Sugerida**:
  - Investigar causa do scaling rápido
  - Verificar se há memory leak ou runaway process
  - Validar HPA configurations
  - Considerar rate limiting

```promql
ALERT NodePoolRapidScaling
  IF (
    rate(kube_node_created[10m]) > 0.5  # Mais de 1 node a cada 2min
  )
  FOR 5m
  LABELS { severity = "warning", component = "nodepool" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} escalando muito rápido",
    description = "{{ $value }} nodes/min. Investigar causa."
  }
```

#### **NodePoolCostThreshold**
- **Descrição**: Custo estimado do Node Pool excedeu threshold configurado
- **Severidade**: 🟡 Warning
- **Ação Sugerida**:
  - Revisar aprovação de budget
  - Otimizar resource requests
  - Considerar spot instances
  - Avaliar instance types mais econômicos

```promql
ALERT NodePoolCostThreshold
  IF (
    (count(kube_node_info{node=~".*nodepool.*"}) * azure_node_hourly_cost) > azure_cost_threshold
  )
  FOR 30m
  LABELS { severity = "warning", component = "nodepool", cost = "true" }
  ANNOTATIONS {
    summary = "Node Pool {{ $labels.nodepool }} excedeu threshold de custo",
    description = "Custo atual: ${{ $value }}/hora. Threshold: ${{ $labels.threshold }}/hora"
  }
```

---

### 7. **Alerts de HPA Específicos**

#### **HPATargetMissing**
- **Descrição**: HPA não encontra o target (Deployment/StatefulSet)
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar se target existe
  - Validar nome do targetRef
  - Checar namespace correto

```promql
ALERT HPATargetMissing
  IF kube_hpa_status_condition{condition="AbleToScale",status="false"} > 0
  FOR 5m
  LABELS { severity = "critical" }
```

#### **HPAMetricsUnavailable**
- **Descrição**: HPA não consegue obter métricas (metrics-server down)
- **Severidade**: 🔴 Critical
- **Ação Sugerida**:
  - Verificar metrics-server status
  - Validar RBAC permissions
  - Checar conectividade API

```promql
ALERT HPAMetricsUnavailable
  IF kube_hpa_status_condition{condition="ScalingActive",status="false"} > 0
  FOR 5m
  LABELS { severity = "critical" }
```

---

## 🏗️ Arquitetura Proposta

### Diagrama de Componentes

```
┌─────────────────────────────────────────────────────────────┐
│                      Frontend (React)                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────────────┐      ┌────────────────────────┐   │
│  │   HPATab Component   │      │   AlertsPanel          │   │
│  │                      │      │                        │   │
│  │  - Lista HPAs        │◄────►│  - Anomalias           │   │
│  │  - Editor            │      │  - Alertas             │   │
│  │  - Badge de Alertas  │      │  - Severidade          │   │
│  └──────────────────────┘      └────────────────────────┘   │
│           │                              │                   │
│           └──────────────┬───────────────┘                   │
│                          ▼                                   │
│                  API REST Handlers                           │
└─────────────────────────────────────────────────────────────┘
                           │
┌─────────────────────────────────────────────────────────────┐
│                    Backend (Go)                              │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────────┐   ┌──────────────────┐                 │
│  │ Alert Manager   │   │ Alert-HPA        │                 │
│  │ Client          │──►│ Correlator       │                 │
│  │                 │   │                  │                 │
│  │ - Query Alerts  │   │ - Match labels   │                 │
│  │ - Parse Rules   │   │ - Find HPAs      │                 │
│  └─────────────────┘   └──────────────────┘                 │
│           │                       │                          │
│           │                       ▼                          │
│           │            ┌─────────────────────┐              │
│           │            │  Mitigation Engine  │              │
│           │            │                     │              │
│           │            │  - Suggest Actions  │              │
│           │            │  - Auto-remediate   │              │
│           │            └─────────────────────┘              │
│           │                       │                          │
│           └───────────┬───────────┘                          │
│                       ▼                                      │
│            ┌────────────────────┐                            │
│            │  History Tracker   │                            │
│            │                    │                            │
│            │  - Log Alert       │                            │
│            │  - Log Action      │                            │
│            └────────────────────┘                            │
│                                                               │
└─────────────────────────────────────────────────────────────┘
                       │
                       ▼
            ┌────────────────────┐
            │   Alertmanager     │
            │   (External)       │
            └────────────────────┘
```

### Fluxo de Dados

```
1. Alertmanager → Alert Manager Client
   - Query active alerts
   - Filter by namespace/labels

2. Alert-HPA Correlator → HPA Matching
   - Parse alert labels
   - Find affected HPAs
   - Calculate severity

3. Mitigation Engine → Suggested Actions
   - Analyze alert type
   - Current HPA state
   - Historical patterns
   - Suggest remediation

4. History Tracker → Event Logging
   - Log alert occurrence
   - Log actions taken
   - Store timeline

5. Frontend → Visualization
   - Display alerts per HPA
   - Show suggestions
   - Action buttons
```

---

## 💻 Implementação Proposta

### Fase 1: Alertmanager Client (Backend)

**Arquivo**: `internal/alertmanager/client.go`

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
	endpoint string
	client   *http.Client
}

type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Status      string            `json:"status"`
	Receivers   []string          `json:"receivers"`
}

// NewClient cria cliente do Alertmanager
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetActiveAlerts retorna alertas ativos
func (c *Client) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]Alert, error) {
	// Construir query string com filtros
	// Ex: ?filter={namespace="production",severity="critical"}

	resp, err := c.client.Get(c.endpoint + "/api/v2/alerts")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var alerts []Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, err
	}

	// Filtrar alerts ativos
	var activeAlerts []Alert
	for _, alert := range alerts {
		if alert.Status == "firing" {
			activeAlerts = append(activeAlerts, alert)
		}
	}

	return activeAlerts, nil
}
```

### Fase 2: Alert-HPA e NodePool Correlator

**Arquivo**: `internal/alertmanager/correlator.go`

```go
package alertmanager

import (
	"k8s-hpa-manager/internal/models"
)

type HPAAlert struct {
	HPA         *models.HPA
	Alerts      []Alert
	Severity    string
	Suggestions []string
}

type NodePoolAlert struct {
	NodePool    *models.NodePool
	Alerts      []Alert
	Severity    string
	Suggestions []string
}

type Correlator struct {
	hpaStore models.HPAStore
}

// CorrelateHPAAlerts associa alertas aos HPAs afetados
func (c *Correlator) CorrelateHPAAlerts(alerts []Alert, hpas []models.HPA) []HPAAlert {
	correlations := make(map[string]*HPAAlert)

	for _, alert := range alerts {
		// Extrair namespace e hpa name dos labels
		namespace := alert.Labels["namespace"]
		hpaName := alert.Labels["hpa"]

		// Encontrar HPA correspondente
		for _, hpa := range hpas {
			key := fmt.Sprintf("%s/%s", hpa.Namespace, hpa.Name)

			if hpa.Namespace == namespace {
				if correlations[key] == nil {
					correlations[key] = &HPAAlert{
						HPA:    &hpa,
						Alerts: []Alert{},
					}
				}
				correlations[key].Alerts = append(correlations[key].Alerts, alert)

				// Calcular severidade máxima
				if alert.Labels["severity"] == "critical" {
					correlations[key].Severity = "critical"
				}
			}
		}
	}

	// Converter map para slice
	var result []HPAAlert
	for _, correlation := range correlations {
		result = append(result, *correlation)
	}

	return result
}
```

### Fase 3: Mitigation Engine

**Arquivo**: `internal/alertmanager/mitigation.go`

```go
package alertmanager

type SuggestedAction struct {
	Type        string
	Description string
	Priority    int
	AutoApply   bool
	Params      map[string]interface{}
}

type MitigationEngine struct {
	rules map[string]func(Alert, *models.HPA) []SuggestedAction
}

// NewMitigationEngine cria engine de mitigação
func NewMitigationEngine() *MitigationEngine {
	engine := &MitigationEngine{
		rules: make(map[string]func(Alert, *models.HPA) []SuggestedAction),
	}

	// Registrar regras de mitigação
	engine.registerRules()
	return engine
}

func (e *MitigationEngine) registerRules() {
	// Regra: HPA Maxed Out
	e.rules["HPAMaxedOut"] = func(alert Alert, hpa *models.HPA) []SuggestedAction {
		return []SuggestedAction{
			{
				Type:        "increase_max_replicas",
				Description: fmt.Sprintf("Aumentar maxReplicas de %d para %d", hpa.MaxReplicas, hpa.MaxReplicas+5),
				Priority:    1,
				AutoApply:   false,
				Params: map[string]interface{}{
					"max_replicas": hpa.MaxReplicas + 5,
				},
			},
			{
				Type:        "review_target_utilization",
				Description: "Revisar targetCPUUtilizationPercentage (atualmente em 80%)",
				Priority:    2,
				AutoApply:   false,
			},
		}
	}

	// Regra: High CPU Usage
	e.rules["HighCPUUsage"] = func(alert Alert, hpa *models.HPA) []SuggestedAction {
		return []SuggestedAction{
			{
				Type:        "trigger_rollout",
				Description: "Reiniciar pods (rollout restart)",
				Priority:    1,
				AutoApply:   false,
			},
			{
				Type:        "scale_up",
				Description: "Escalar para o máximo de réplicas temporariamente",
				Priority:    2,
				AutoApply:   true, // Pode ser automatizado
				Params: map[string]interface{}{
					"replicas": hpa.MaxReplicas,
				},
			},
		}
	}
}

// GetSuggestions retorna sugestões de ações baseadas nos alertas
func (e *MitigationEngine) GetSuggestions(alert Alert, hpa *models.HPA) []SuggestedAction {
	alertName := alert.Labels["alertname"]

	if rule, exists := e.rules[alertName]; exists {
		return rule(alert, hpa)
	}

	// Sugestão genérica
	return []SuggestedAction{
		{
			Type:        "investigate",
			Description: "Investigar logs e métricas do HPA",
			Priority:    1,
			AutoApply:   false,
		},
	}
}
```

### Fase 4: History Tracker Integration

**Arquivo**: `internal/history/tracker.go` (adição)

```go
// Novas Actions para alertas
const (
	ActionAlertDetected      = "alert_detected"
	ActionAlertResolved      = "alert_resolved"
	ActionMitigationApplied  = "mitigation_applied"
	ActionMitigationSkipped  = "mitigation_skipped"
)

// LogAlert registra detecção de alerta
func (ht *HistoryTracker) LogAlert(alert Alert, hpa *models.HPA, severity string) error {
	return ht.Log(HistoryEntry{
		Action:   ActionAlertDetected,
		Resource: fmt.Sprintf("%s/%s", hpa.Namespace, hpa.Name),
		Cluster:  hpa.Cluster,
		Status:   StatusSuccess,
		Before: map[string]interface{}{
			"alert_name":   alert.Labels["alertname"],
			"severity":     severity,
			"started_at":   alert.StartsAt,
		},
		After: map[string]interface{}{
			"suggestions_count": len(suggestions),
		},
	})
}
```

### Fase 5: Frontend Integration

**Arquivo**: `internal/web/frontend/src/components/HPAListItem.tsx` (modificação)

```tsx
interface HPAListItemProps {
	// ... props existentes
	alerts?: AlertSummary;
}

export const HPAListItem = ({ name, namespace, currentReplicas, minReplicas, maxReplicas, alerts, ...props }: HPAListItemProps) => {
	return (
		<div className="...">
			{/* Conteúdo existente */}

			{/* Badge de alertas */}
			{alerts && alerts.count > 0 && (
				<div className="flex items-center gap-1">
					<AlertTriangle className={`w-4 h-4 ${alerts.severity === 'critical' ? 'text-red-500' : 'text-yellow-500'}`} />
					<span className="text-xs font-medium">{alerts.count} alerta{alerts.count > 1 ? 's' : ''}</span>
				</div>
			)}
		</div>
	);
};
```

**Arquivo**: `internal/web/frontend/src/components/HPAEditor.tsx` (adição de painel)

```tsx
{/* Painel de Alertas Ativos */}
{hpaAlerts && hpaAlerts.length > 0 && (
	<Card className="border-l-4 border-l-red-500">
		<CardHeader>
			<CardTitle className="flex items-center gap-2">
				<AlertTriangle className="w-5 h-5 text-red-500" />
				Alertas Ativos ({hpaAlerts.length})
			</CardTitle>
		</CardHeader>
		<CardContent>
			{hpaAlerts.map((alert, idx) => (
				<div key={idx} className="mb-4 p-3 bg-red-50 rounded">
					<div className="font-semibold text-red-700">{alert.labels.alertname}</div>
					<div className="text-sm text-gray-600">{alert.annotations.summary}</div>

					{/* Sugestões de mitigação */}
					{alert.suggestions && alert.suggestions.length > 0 && (
						<div className="mt-2">
							<div className="text-xs font-medium mb-1">Ações Sugeridas:</div>
							{alert.suggestions.map((suggestion, sidx) => (
								<Button
									key={sidx}
									variant="outline"
									size="sm"
									className="mr-2 mb-1"
									onClick={() => handleApplySuggestion(suggestion)}
								>
									{suggestion.description}
								</Button>
							))}
						</div>
					)}
				</div>
			))}
		</CardContent>
	</Card>
)}
```

---

## 📝 Endpoints da API

### Backend REST API

```go
// GET /api/v1/alertmanager/alerts?cluster={cluster}
// Retorna todos os alertas ativos

// GET /api/v1/alertmanager/alerts/hpa/{namespace}/{name}?cluster={cluster}
// Retorna alertas específicos de um HPA

// GET /api/v1/alertmanager/correlations?cluster={cluster}
// Retorna correlação entre alertas e HPAs

// POST /api/v1/alertmanager/mitigation/{namespace}/{name}
// Aplica ação de mitigação sugerida
{
  "alert_id": "uuid",
  "action_type": "increase_max_replicas",
  "params": {
    "max_replicas": 15
  }
}
```

---

## 🎨 Visualização de Alertas de Node Pool na Interface

### Interface de Node Pools com Alertas

**Arquivo**: `internal/web/frontend/src/components/NodePoolListItem.tsx` (modificação proposta)

```tsx
interface NodePoolListItemProps {
	name: string;
	nodeCount: number;
	nodeCountMin: number;
	nodeCountMax: number;
	vmSize: string;
	alerts?: NodePoolAlertSummary;  // ⭐ NOVO
	onClick: () => void;
}

interface NodePoolAlertSummary {
	count: number;
	severity: 'critical' | 'warning' | 'info';
	types: string[];  // ['NodePoolMaxedOut', 'NodePoolCostThreshold']
}

export const NodePoolListItem = ({ name, nodeCount, nodeCountMin, nodeCountMax, vmSize, alerts, onClick }: NodePoolListItemProps) => {
	// Calcular utilização
	const utilizationPercent = ((nodeCount - nodeCountMin) / (nodeCountMax - nodeCountMin)) * 100;

	return (
		<div className="p-3 border rounded hover:bg-muted/50 cursor-pointer" onClick={onClick}>
			<div className="flex items-center justify-between">
				<div>
					<div className="font-semibold">{name}</div>
					<div className="text-sm text-muted-foreground">{vmSize}</div>
				</div>

				{/* Badge de Alertas */}
				{alerts && alerts.count > 0 && (
					<div className={`flex items-center gap-1 px-2 py-1 rounded ${
						alerts.severity === 'critical'
							? 'bg-red-100 text-red-700'
							: alerts.severity === 'warning'
							? 'bg-yellow-100 text-yellow-700'
							: 'bg-blue-100 text-blue-700'
					}`}>
						<AlertTriangle className="w-4 h-4" />
						<span className="text-xs font-medium">{alerts.count}</span>
					</div>
				)}
			</div>

			{/* Barra de Utilização com indicadores */}
			<div className="mt-2">
				<div className="flex items-center justify-between text-xs mb-1">
					<span>Nodes: {nodeCount} / {nodeCountMax}</span>
					<span className={
						nodeCount >= nodeCountMax
							? 'text-red-600 font-bold'
							: nodeCount >= nodeCountMax * 0.9
							? 'text-yellow-600 font-semibold'
							: 'text-muted-foreground'
					}>
						{utilizationPercent.toFixed(0)}%
					</span>
				</div>
				<div className="w-full bg-gray-200 rounded-full h-2">
					<div
						className={`h-2 rounded-full transition-all ${
							nodeCount >= nodeCountMax
								? 'bg-red-500'  // Maxed out
								: nodeCount >= nodeCountMax * 0.9
								? 'bg-yellow-500'  // Near max
								: 'bg-green-500'  // Normal
						}`}
						style={{ width: `${Math.min(utilizationPercent, 100)}%` }}
					/>
				</div>
			</div>

			{/* Resumo de alertas */}
			{alerts && alerts.count > 0 && (
				<div className="mt-2 text-xs text-muted-foreground">
					{alerts.types.slice(0, 2).join(', ')}
					{alerts.types.length > 2 && ` +${alerts.types.length - 2}`}
				</div>
			)}
		</div>
	);
};
```

### Painel de Alertas no Node Pool Editor

**Arquivo**: `internal/web/frontend/src/components/NodePoolEditor.tsx` (adição)

```tsx
{/* Painel de Alertas do Node Pool */}
{nodePoolAlerts && nodePoolAlerts.length > 0 && (
	<Card className="border-l-4 border-l-red-500 mb-4">
		<CardHeader>
			<CardTitle className="flex items-center gap-2 text-base">
				<AlertTriangle className="w-5 h-5 text-red-500" />
				Alertas do Node Pool ({nodePoolAlerts.length})
			</CardTitle>
		</CardHeader>
		<CardContent className="space-y-3">
			{nodePoolAlerts.map((alert, idx) => (
				<div key={idx} className={`p-3 rounded ${
					alert.severity === 'critical' ? 'bg-red-50' : 'bg-yellow-50'
				}`}>
					{/* Alert: NodePoolMaxedOut */}
					{alert.labels.alertname === 'NodePoolMaxedOut' && (
						<>
							<div className="flex items-start justify-between">
								<div>
									<div className="font-semibold text-red-700">
										Node Pool no Limite Máximo
									</div>
									<div className="text-sm text-gray-600 mt-1">
										{nodePool.node_count} de {nodePool.node_count_max} nodes utilizados
									</div>
								</div>
								<Badge variant="destructive">Critical</Badge>
							</div>

							{/* Informações Contextuais */}
							<div className="mt-3 p-2 bg-white/50 rounded text-xs">
								<div className="grid grid-cols-2 gap-2">
									<div>
										<span className="text-muted-foreground">Pods Pending:</span>
										<span className="font-semibold ml-1">{alert.context.pods_pending || 0}</span>
									</div>
									<div>
										<span className="text-muted-foreground">CPU Avg:</span>
										<span className="font-semibold ml-1">{alert.context.avg_cpu_usage}%</span>
									</div>
								</div>
							</div>

							{/* Ações Sugeridas */}
							<div className="mt-3">
								<div className="text-xs font-medium mb-2">Ações Sugeridas:</div>
								<div className="flex flex-wrap gap-2">
									<Button
										variant="destructive"
										size="sm"
										onClick={() => handleIncreaseMaxCount(alert.suggestion.new_max_count)}
									>
										<TrendingUp className="w-3 h-3 mr-1" />
										Aumentar para {alert.suggestion.new_max_count} nodes
									</Button>
									<Button
										variant="outline"
										size="sm"
										onClick={() => handleViewMetrics()}
									>
										<BarChart className="w-3 h-3 mr-1" />
										Analisar Métricas
									</Button>
									<Button
										variant="outline"
										size="sm"
										onClick={() => handleViewHistory()}
									>
										<History className="w-3 h-3 mr-1" />
										Ver Histórico
									</Button>
								</div>
							</div>

							{/* Estimativa de Custo */}
							{alert.suggestion.estimated_cost && (
								<div className="mt-2 p-2 bg-blue-50 rounded text-xs">
									<span className="text-blue-700">
										💰 Custo estimado: +${alert.suggestion.estimated_cost}/mês
									</span>
								</div>
							)}
						</>
					)}

					{/* Alert: NodePoolNearMaxCapacity */}
					{alert.labels.alertname === 'NodePoolNearMaxCapacity' && (
						<>
							<div className="flex items-start justify-between">
								<div>
									<div className="font-semibold text-yellow-700">
										Próximo do Limite Máximo
									</div>
									<div className="text-sm text-gray-600 mt-1">
										{((nodePool.node_count / nodePool.node_count_max) * 100).toFixed(0)}% de capacidade utilizada
									</div>
								</div>
								<Badge variant="secondary">Warning</Badge>
							</div>

							<div className="mt-2 text-xs text-gray-600">
								Planejar aumento preventivo do node_count_max para evitar problemas futuros.
							</div>
						</>
					)}

					{/* Alert: NodePoolCostThreshold */}
					{alert.labels.alertname === 'NodePoolCostThreshold' && (
						<>
							<div className="flex items-start justify-between">
								<div>
									<div className="font-semibold text-yellow-700">
										Threshold de Custo Excedido
									</div>
									<div className="text-sm text-gray-600 mt-1">
										Custo atual: ${alert.context.current_cost}/hora
									</div>
								</div>
								<Badge variant="secondary">Cost Alert</Badge>
							</div>

							<div className="mt-3">
								<div className="text-xs font-medium mb-2">Opções de Otimização:</div>
								<div className="flex flex-wrap gap-2">
									<Button variant="outline" size="sm">
										Revisar Resource Requests
									</Button>
									<Button variant="outline" size="sm">
										Considerar Spot Instances
									</Button>
								</div>
							</div>
						</>
					)}
				</div>
			))}
		</CardContent>
	</Card>
)}
```

### Dashboard de Correlação HPA ↔ Node Pool

**Arquivo**: `internal/web/frontend/src/components/CapacityDashboard.tsx` (novo)

```tsx
interface CapacityDashboardProps {
	cluster: string;
}

export const CapacityDashboard = ({ cluster }: CapacityDashboardProps) => {
	const { hpaAlerts } = useHPAAlerts(cluster);
	const { nodePoolAlerts } = useNodePoolAlerts(cluster);

	return (
		<div className="grid grid-cols-2 gap-4">
			{/* Coluna HPAs */}
			<Card>
				<CardHeader>
					<CardTitle>HPAs com Alertas</CardTitle>
				</CardHeader>
				<CardContent>
					{hpaAlerts.map(hpa => (
						<div key={hpa.name} className="mb-2">
							<div className="font-semibold">{hpa.name}</div>
							<div className="text-sm">
								{hpa.alerts.map(a => a.labels.alertname).join(', ')}
							</div>
						</div>
					))}
				</CardContent>
			</Card>

			{/* Coluna Node Pools */}
			<Card>
				<CardHeader>
					<CardTitle>Node Pools com Alertas</CardTitle>
				</CardHeader>
				<CardContent>
					{nodePoolAlerts.map(np => (
						<div key={np.name} className="mb-2">
							<div className="font-semibold">{np.name}</div>
							<div className="text-sm text-red-600">
								{np.node_count}/{np.node_count_max} nodes
							</div>
							<div className="text-xs">
								{np.alerts.map(a => a.labels.alertname).join(', ')}
							</div>
						</div>
					))}
				</CardContent>
			</Card>
		</div>
	);
};
```

---

## 🔄 Fluxo de Uso

### Cenário 1: Detecção Proativa (HPA)

```
1. Usuário acessa aba HPAs
2. Sistema busca alertas ativos do Alertmanager
3. Correlator associa alertas aos HPAs
4. Interface exibe badges de alerta em cada HPA
5. Usuário clica no HPA com alerta
6. Editor mostra detalhes do alerta e sugestões
7. Usuário aplica mitigação sugerida
8. History Tracker registra ação
```

### Cenário 2: Resposta Automatizada

```
1. Alertmanager detecta "HPAMaxedOut"
2. Webhook notifica o k8s-hpa-manager
3. Correlator identifica HPA afetado
4. Mitigation Engine avalia ação
5. Se AutoApply=true: aplica automaticamente
6. History Tracker registra evento
7. Frontend atualiza status via SSE
```

---

## 📊 Benefícios Esperados

### 1. **Proatividade**
- ✅ Detectar problemas antes de indisponibilidade
- ✅ Reduzir tempo de resposta a incidentes
- ✅ Visibility de saúde dos HPAs em tempo real

### 2. **Automação**
- ✅ Sugestões inteligentes de mitigação
- ✅ Opção de aplicação automática (AutoApply)
- ✅ Redução de trabalho manual

### 3. **Rastreabilidade**
- ✅ Histórico completo de alertas
- ✅ Registro de ações tomadas
- ✅ Análise de padrões de falha

### 4. **Experiência do Usuário**
- ✅ Interface visual clara
- ✅ Context-aware (alertas por HPA)
- ✅ Ações com um clique

---

## ⚙️ Configuração Requerida

### Alertmanager Configuration

```yaml
route:
  receiver: 'k8s-hpa-manager-webhook'
  routes:
    - match:
        severity: critical
      receiver: 'k8s-hpa-manager-webhook'
      continue: true

receivers:
  - name: 'k8s-hpa-manager-webhook'
    webhook_configs:
      - url: 'http://k8s-hpa-manager:8080/api/v1/alertmanager/webhook'
        send_resolved: true
```

### Prometheus Rules (Exemplos)

```yaml
groups:
  - name: hpa_alerts
    interval: 30s
    rules:
      - alert: HPAMaxedOut
        expr: kube_hpa_status_current_replicas >= kube_hpa_spec_max_replicas
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "HPA {{ $labels.namespace }}/{{ $labels.hpa }} atingiu limite máximo"
          description: "Considere aumentar maxReplicas"

      - alert: HPAScaleCapability
        expr: kube_hpa_status_condition{condition="ScalingActive",status="false"} == 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "HPA {{ $labels.namespace }}/{{ $labels.hpa }} não consegue escalar"
```

---

## 🚀 Roadmap de Implementação

### Fase 1: Foundation (Sprint 1)
- [ ] Criar Alertmanager Client
- [ ] Implementar GetActiveAlerts
- [ ] Testes unitários

### Fase 2: Correlation (Sprint 2)
- [ ] Implementar Alert-HPA Correlator
- [ ] Criar lógica de matching
- [ ] Testes de correlação

### Fase 3: Mitigation (Sprint 3)
- [ ] Criar Mitigation Engine
- [ ] Registrar regras básicas
- [ ] Implementar sugestões

### Fase 4: Integration (Sprint 4)
- [ ] Integrar com History Tracker
- [ ] Criar endpoints REST API
- [ ] Documentar API

### Fase 5: Frontend (Sprint 5)
- [ ] Adicionar badges de alerta em HPAs
- [ ] Painel de alertas no editor
- [ ] Botões de ação de mitigação

### Fase 6: Automation (Sprint 6)
- [ ] Implementar webhook receiver
- [ ] Ações automáticas (AutoApply)
- [ ] Testes E2E

---

## 🧪 Testes Requeridos

### Testes Unitários
- Alert Manager Client
- Correlator logic
- Mitigation rules
- History logging

### Testes de Integração
- Alertmanager → Client → Correlator
- Frontend → Backend → Alertmanager
- Webhook flow

### Testes E2E
- Cenário completo de alerta → mitigação
- Auto-remediation flow
- History timeline

---

## 📚 Referências

- [Alertmanager API Docs](https://prometheus.io/docs/alerting/latest/clients/)
- [Prometheus Alerting Rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
- [Kubernetes HPA Metrics](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
- [Best Practices for Alerting](https://prometheus.io/docs/practices/alerting/)

---

## 📝 Notas Finais

Este estudo fornece uma base sólida para implementação da integração com Alertmanager. A arquitetura proposta é:

- ✅ **Extensível**: Novas regras de mitigação podem ser adicionadas facilmente
- ✅ **Modular**: Componentes independentes e testáveis
- ✅ **Escalável**: Suporta múltiplos clusters e namespaces
- ✅ **User-friendly**: Interface visual intuitiva

**Próximos passos**: Aprovação do estudo e início da Fase 1 de implementação.

---

**Autor**: Claude Code
**Revisão**: Pendente
**Aprovação**: Pendente
