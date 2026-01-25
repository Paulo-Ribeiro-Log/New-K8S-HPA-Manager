# 🔮 Plano de Implementação: Análise Preditiva de Deployments

**Data de Criação**: 02 de Janeiro de 2026  
**Versão**: 1.0  
**Status**: 📋 Planejamento

---

## 📋 Visão Geral

Implementar sistema de análise preditiva individual para Deployments, integrando dados históricos do Prometheus com análise de IA para prever necessidades de recursos, identificar riscos e sugerir otimizações considerando padrões de carga temporal e capacidade de infraestrutura.

---

## 🎯 Objetivos

### Primários
1. **Análise Temporal Contextual**: Comparar métricas atuais com histórico do mesmo período (hora do dia, dia da semana)
2. **Previsão de Capacidade**: Identificar necessidade de escalonamento antes que problemas ocorram
3. **Otimização de Recursos**: Sugerir ajustes de limites e requisições baseado em padrões reais
4. **Análise de Distribuição**: Avaliar saúde da distribuição de réplicas entre nodes

### Secundários
1. Identificar anomalias em padrões de consumo
2. Detectar recursos subutilizados ou superalocados
3. Prever impacto de escalonamento na infraestrutura
4. Sugerir janelas ideais para manutenção

---

## 🏗️ Arquitetura da Solução

```
┌─────────────────────────────────────────────────────────────┐
│                    Frontend (React/TypeScript)               │
├─────────────────────────────────────────────────────────────┤
│  DeploymentsTab.tsx                                          │
│  ├─ Botão "Análise Preditiva" (header)                      │
│  └─ Modal/Panel de Resultados                               │
│      ├─ Gráficos temporais (Chart.js/Recharts)              │
│      ├─ Sugestões da IA                                      │
│      ├─ Score de saúde                                       │
│      └─ Recomendações acionáveis                            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Backend API (Go)                          │
├─────────────────────────────────────────────────────────────┤
│  /api/v1/predictions/deployment                              │
│  └─ DeploymentPredictionHandler                             │
│      ├─ Valida parâmetros (cluster, namespace, name)        │
│      ├─ Orquestra coleta de dados                           │
│      └─ Retorna análise consolidada                         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Módulo de Análise Preditiva (Go)               │
├─────────────────────────────────────────────────────────────┤
│  internal/predictions/                                       │
│  ├─ analyzer.go           (Orquestrador principal)          │
│  ├─ prometheus_collector.go (Coleta métricas históricas)    │
│  ├─ temporal_comparator.go  (Análise temporal)              │
│  ├─ capacity_evaluator.go   (Avaliação de capacidade)       │
│  ├─ node_distributor.go     (Análise de distribuição)       │
│  └─ ai_integrator.go        (Integração com IA)             │
└─────────────────────────────────────────────────────────────┘
                    │                    │
                    ▼                    ▼
        ┌───────────────────┐  ┌──────────────────┐
        │   Prometheus      │  │   AI Provider    │
        │   PromQL Queries  │  │   (Claude/GPT)   │
        └───────────────────┘  └──────────────────┘
```

---

## 📊 Métricas a Coletar do Prometheus

### 1. **Métricas de Recursos (Deployment)**

#### CPU
```promql
# Uso atual
rate(container_cpu_usage_seconds_total{namespace="X",pod=~"deployment-.*"}[5m])

# Histórico (últimos 7 dias, mesmo horário ±1h)
rate(container_cpu_usage_seconds_total{namespace="X",pod=~"deployment-.*"}[5m]) offset 7d

# Histórico (últimos 30 dias, mesmo horário)
rate(container_cpu_usage_seconds_total{namespace="X",pod=~"deployment-.*"}[5m]) offset 30d

# Requests e Limits
kube_pod_container_resource_requests{resource="cpu"}
kube_pod_container_resource_limits{resource="cpu"}
```

#### Memória
```promql
# Uso atual
container_memory_working_set_bytes{namespace="X",pod=~"deployment-.*"}

# Histórico
container_memory_working_set_bytes{namespace="X",pod=~"deployment-.*"} offset 7d

# Requests e Limits
kube_pod_container_resource_requests{resource="memory"}
kube_pod_container_resource_limits{resource="memory"}

# Memory pressure
container_memory_usage_bytes / container_spec_memory_limit_bytes > 0.8
```

#### Rede
```promql
# Tráfego de entrada
rate(container_network_receive_bytes_total{namespace="X",pod=~"deployment-.*"}[5m])

# Tráfego de saída
rate(container_network_transmit_bytes_total{namespace="X",pod=~"deployment-.*"}[5m])

# Latência (se disponível via Istio)
histogram_quantile(0.95, rate(istio_request_duration_milliseconds_bucket[5m]))
```

### 2. **Métricas de Réplicas**

```promql
# Número atual de réplicas
kube_deployment_status_replicas{namespace="X",deployment="Y"}

# Réplicas disponíveis
kube_deployment_status_replicas_available{namespace="X",deployment="Y"}

# Réplicas prontas
kube_deployment_status_replicas_ready{namespace="X",deployment="Y"}

# Histórico de escalonamento
changes(kube_deployment_status_replicas[7d])

# HPA (se configurado)
kube_horizontalpodautoscaler_status_current_replicas
kube_horizontalpodautoscaler_status_desired_replicas
```

### 3. **Métricas de Nodes**

```promql
# Capacidade total de CPU dos nodes
sum(kube_node_status_capacity{resource="cpu"})

# CPU alocável
sum(kube_node_status_allocatable{resource="cpu"})

# CPU já requisitada
sum(kube_pod_container_resource_requests{resource="cpu"})

# Memória disponível
sum(kube_node_status_allocatable{resource="memory"})

# Pods por node
count(kube_pod_info) by (node)

# Nodes onde o deployment está
kube_pod_info{namespace="X",pod=~"deployment-.*"}
```

### 4. **Métricas de Saúde**

```promql
# Restarts de containers
rate(kube_pod_container_status_restarts_total{namespace="X"}[1h])

# Status de pods
kube_pod_status_phase{namespace="X",pod=~"deployment-.*"}

# Tempo de uptime
time() - kube_pod_start_time{namespace="X"}

# OOMKilled
kube_pod_container_status_terminated_reason{reason="OOMKilled"}
```

### 5. **Métricas de Performance**

```promql
# Request rate
rate(http_requests_total{namespace="X"}[5m])

# Error rate
rate(http_requests_total{namespace="X",status=~"5.."}[5m])

# Latência P95
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
```

---

## 🤖 Integração com IA

### Prompt Template para Análise

```text
Você é um especialista sênior em análise de infraestrutura Kubernetes e otimização de recursos.

## CONTEXTO DO DEPLOYMENT
- Nome: {deployment_name}
- Namespace: {namespace}
- Cluster: {cluster}
- Hora atual: {current_time} ({day_of_week})
- Réplicas atuais: {current_replicas}

## MÉTRICAS ATUAIS (últimos 5 minutos)
- CPU média: {cpu_avg}m / Request: {cpu_request}m / Limit: {cpu_limit}m
- CPU pico: {cpu_peak}m
- Memória média: {mem_avg}Mi / Request: {mem_request}Mi / Limit: {mem_limit}Mi
- Memória pico: {mem_peak}Mi
- Network RX: {net_rx} MB/s
- Network TX: {net_tx} MB/s
- Request rate: {req_rate} req/s
- Error rate: {error_rate}%
- Latência P95: {p95_latency}ms
- Restarts (última hora): {restarts}

## DADOS HISTÓRICOS (mesmo período - 7 dias atrás)
- CPU média: {cpu_avg_7d}m (Δ: {cpu_delta_7d}%)
- Memória média: {mem_avg_7d}Mi (Δ: {mem_delta_7d}%)
- Réplicas: {replicas_7d} (Δ: {replicas_delta_7d})
- Request rate: {req_rate_7d} req/s (Δ: {req_delta_7d}%)

## DADOS HISTÓRICOS (mesmo período - 30 dias atrás)
- CPU média: {cpu_avg_30d}m (Δ: {cpu_delta_30d}%)
- Memória média: {mem_avg_30d}Mi (Δ: {mem_delta_30d}%)
- Réplicas: {replicas_30d}

## PADRÃO TEMPORAL (última semana)
- Horário de pico: {peak_hours}
- CPU média no horário de pico: {peak_cpu}m
- CPU média fora de pico: {offpeak_cpu}m
- Variação semanal: {weekly_pattern}

## DISTRIBUIÇÃO EM NODES E ANÁLISE DE VM SIZING
- Nodes utilizados: {nodes_used}
- Distribuição atual: {node_distribution}
  * Node-1: {pods_count} pods, CPU: {node_cpu_usage}/{node_cpu_capacity}, Mem: {node_mem_usage}/{node_mem_capacity}
  * Node-2: {pods_count} pods, CPU: {node_cpu_usage}/{node_cpu_capacity}, Mem: {node_mem_usage}/{node_mem_capacity}
- Nodes disponíveis no cluster: {total_nodes}
- VM Sizing por node:
  * Instance type predominante: {instance_type} (ex: t3.large, m5.xlarge)
  * CPU por VM: {cpu_per_vm} cores
  * Memória por VM: {mem_per_vm} GB
  * Pods max por node: {max_pods_per_node}
- Capacidade Total do Cluster:
  * CPU total disponível: {total_cpu_available} cores
  * CPU já alocada: {total_cpu_allocated} cores ({cpu_utilization}%)
  * Memória disponível: {total_mem_available} GB
  * Memória alocada: {total_mem_allocated} GB ({mem_utilization}%)
- Análise de Bin-Packing:
  * Fator de empacotamento atual: {bin_packing_efficiency}%
  * Desperdício estimado: {wasted_resources}
  * Fragmentação de recursos: {fragmentation_level}
- Pod Placement Analysis:
  * Anti-affinity rules: {anti_affinity_rules}
  * Topology spread constraints: {topology_constraints}
  * Node selector/taints: {node_selectors}

## APLICAÇÕES CONCORRENTES (top 5 consumidores de recursos)
{competing_apps_list}

## EVENTOS RECENTES
{recent_events}

## ANÁLISE SOLICITADA

Por favor, forneça uma análise preditiva detalhada incluindo:

1. **SCORE DE SAÚDE** (0-100): Avalie a saúde geral do deployment
   - Justifique o score

2. **ANÁLISE DE TENDÊNCIAS**:
   - Identifique tendências de crescimento/decrescimento
   - Compare com padrão semanal e mensal
   - Detecte anomalias

3. **PREDIÇÕES TEMPORAIS** (CRÍTICO):
   - **Próximas 4 horas**: O que é provável acontecer no curto prazo
   - **Próximas 24 horas**: Eventos previstos para o dia
   - **Próximos 7 dias**: Tendências de médio prazo
   - Para cada predição:
     * Evento previsto (ex: "OOMKill", "CPU throttling", "Pod eviction")
     * Probabilidade (0-100%)
     * Severidade (low/medium/high/critical)
     * Timestamp estimado
     * Confiança na predição (0-100%)

4. **ANÁLISE DE CAUSA RAIZ** (RCA):
   - Identifique possíveis causas dos problemas atuais ou futuros:
     * Configuração inadequada (requests/limits)
     * Aumento de carga não planejado
     * Memory leak
     * CPU inefficiency
     * Problema de rede/latência
     * Dependências externas lentas
     * Saturação de nodes
   - Para cada causa identificada:
     * Evidências observadas nas métricas
     * Nível de certeza (0-100%)
     * Sugestões de investigação adicional

5. **ANÁLISE DE IMPACTO** (Cenários "E se..."):
   - **Se nada for feito**:
     * Impacto em usuários (downtime, latência, errors)
     * Impacto em outras aplicações
     * Timeline até falha crítica
   - **Se aplicar as otimizações sugeridas**:
     * Melhorias esperadas
     * Riscos da mudança
     * Estimativa de tempo de implementação

6. **PREVISÃO DE CAPACIDADE E ANÁLISE DE VM SIZING**:
   - Esse deployment pode escalar com a infraestrutura atual?
   - Quantas réplicas adicionais são suportadas CONSIDERANDO:
     * Recursos disponíveis por node (CPU, Memória)
     * Limites de pods por node (kubelet max-pods)
     * Fragmentação de recursos entre nodes
     * Requisições vs Limites configurados
   - Análise por Node:
     * Quantas réplicas cabem em cada node atual?
     * Qual node está mais saturado?
     * Qual node tem mais capacidade livre?
   - Impacto no Cluster:
     * Será necessário adicionar novos nodes? Quantos?
     * Qual o sizing ideal para novos nodes?
     * Impacto em outras aplicações (bin-packing)
   - Timeline de Saturação:
     * Quando atingiremos 80% de capacidade?
     * Quando atingiremos 100% de capacidade?
     * Baseado no crescimento atual, quando precisaremos escalar a infra?

7. **OTIMIZAÇÕES RECOMENDADAS**:
   - Ajustes em requests/limits
   - Número ideal de réplicas para este horário
   - Redistribuição entre nodes (se necessário)
   - Configurações de HPA

8. **RISCOS IDENTIFICADOS**:
   - Riscos imediatos (próximas horas)
   - Riscos de médio prazo (próximos dias)
   - Gargalos de infraestrutura
   - Riscos cascata (impacto em outras apps)

9. **AÇÕES PRIORITÁRIAS** (máximo 5):
   - Liste ações ordenadas por prioridade
   - Cada ação deve ser específica e acionável
   - Inclua urgência e esforço estimado

10. **JANELA IDEAL PARA MANUTENÇÃO**:
    - Baseado no padrão de carga, quando seria ideal fazer updates?

11. **SUMÁRIO EXECUTIVO** (para relatório):
    - Resumo em 3-4 parágrafos para gestores não-técnicos
    - Destaque problemas críticos e impactos técnicos

Formato da resposta: JSON estruturado
```

### Estrutura de Resposta da IA

```json
{
  "health_score": 85,
  "health_justification": "Deployment está saudável mas próximo dos limites de CPU...",
  "trends": {
    "cpu": {
      "direction": "growing",
      "rate": "+15% week-over-week",
      "forecast_7d": "950m",
      "anomalies": []
    },
    "memory": {
      "direction": "stable",
      "rate": "+2% week-over-week",
      "forecast_7d": "1.2Gi",
      "anomalies": ["spike detected on 2025-12-30 14:00"]
    },
    "replicas": {
      "current_pattern": "stable",
      "recommended_baseline": 3,
      "peak_hours_recommended": 5
    }
  },
  "predictions": {
    "short_term": [
      {
        "timeframe": "next_4_hours",
        "event": "CPU throttling during peak traffic",
        "probability": 75,
        "severity": "medium",
        "estimated_time": "2026-01-02T18:00:00Z",
        "confidence": 85,
        "description": "Baseado no padrão de tráfego, CPU deve atingir 95% de uso às 18h",
        "impact": "Response time +200ms, possível degradação de serviço"
      },
      {
        "timeframe": "next_4_hours",
        "event": "HPA scale-up trigger",
        "probability": 80,
        "severity": "low",
        "estimated_time": "2026-01-02T17:30:00Z",
        "confidence": 90,
        "description": "HPA deve adicionar 2 réplicas durante horário de pico",
        "impact": "Melhoria na performance, uso adicional de recursos"
      }
    ],
    "medium_term": [
      {
        "timeframe": "next_24_hours",
        "event": "Memory leak causing gradual increase",
        "probability": 60,
        "severity": "high",
        "estimated_time": "2026-01-03T02:00:00Z",
        "confidence": 70,
        "description": "Padrão de crescimento linear de memória sugere leak, atingirá limit em 24h",
        "impact": "OOMKill esperado, reinício de pods, perda de sessões"
      }
    ],
    "long_term": [
      {
        "timeframe": "next_7_days",
        "event": "Node capacity exhaustion",
        "probability": 45,
        "severity": "critical",
        "estimated_time": "2026-01-09T00:00:00Z",
        "confidence": 65,
        "description": "Com crescimento atual (+15%/semana), nodes não suportarão demanda em 7 dias",
        "impact": "Impossibilidade de escalar, downtime em picos, impacto em outras apps"
      },
      {
        "timeframe": "next_7_days",
        "event": "Persistent storage near capacity",
        "probability": 30,
        "severity": "high",
        "estimated_time": "2026-01-08T00:00:00Z",
        "confidence": 50,
        "description": "Se logs continuarem crescendo no ritmo atual",
        "impact": "Write failures, data loss risk"
      }
    ]
  },
  "root_cause_analysis": {
    "identified_causes": [
      {
        "cause": "Inadequate memory limits",
        "category": "configuration",
        "certainty": 90,
        "evidence": [
          "Memory usage at 95% of limit",
          "OOMKill events in last 48h",
          "Working set consistently exceeds request"
        ],
        "investigation_steps": [
          "Review application memory profile",
          "Check for memory leaks with profiling tools",
          "Analyze heap dumps from recent OOMKills"
        ]
      },
      {
        "cause": "Traffic spike pattern not accounted in HPA",
        "category": "scaling_configuration",
        "certainty": 85,
        "evidence": [
          "CPU spikes every day 17:00-19:00",
          "HPA reacts 5min late (cooldown period)",
          "Request queue builds up during ramp-up"
        ],
        "investigation_steps": [
          "Review HPA metrics and thresholds",
          "Consider predictive scaling",
          "Implement scheduled scaling for known patterns"
        ]
      },
      {
        "cause": "Possible memory leak in application code",
        "category": "application_issue",
        "certainty": 70,
        "evidence": [
          "Linear memory growth over 7 days",
          "Memory not released after load decreases",
          "Restarts temporarily fix the issue"
        ],
        "investigation_steps": [
          "Enable memory profiling",
          "Review recent code changes",
          "Check for unclosed connections or goroutine leaks"
        ]
      },
      {
        "cause": "Database connection pool exhaustion",
        "category": "dependency",
        "certainty": 55,
        "evidence": [
          "Latency spikes correlate with connection errors in logs",
          "Error rate increases during high load"
        ],
        "investigation_steps": [
          "Review database connection pool settings",
          "Monitor database connection metrics",
          "Check for connection leak in application"
        ]
      }
    ],
    "primary_cause": "Inadequate memory limits",
    "contributing_factors": [
      "Suboptimal HPA configuration",
      "Potential application memory leak"
    ]
  },
  "impact_analysis": {
    "if_no_action": {
      "user_impact": {
        "downtime_probability": 70,
        "expected_downtime": "2-4 hours within next 7 days",
        "degraded_performance": "Response time +300ms during peaks",
        "error_rate_increase": "+5% during peak hours",
        "affected_users": "~10,000 users during peak (40% of total)"
      },
      "infrastructure_impact": {
        "cascading_failures": [
          "frontend-app will experience increased latency",
          "api-gateway may throttle requests",
          "redis-cache may hit memory limits from retry storms"
        ],
        "node_pressure": "2 out of 5 nodes at 90%+ capacity",
        "cluster_stability": "medium risk of node evictions"
      },
      "timeline_to_failure": {
        "critical_failure_eta": "5-7 days with 70% confidence",
        "first_user_impact_eta": "18-24 hours with 80% confidence",
        "service_degradation_eta": "4-6 hours with 85% confidence"
      }
    },
    "if_optimizations_applied": {
      "improvements": {
        "stability_increase": "+40% (fewer restarts, better resource fit)",
        "performance_gain": "P95 latency -150ms",
        "resource_efficiency": "Better node utilization, reduced waste",
        "user_experience": "Consistent response times, <1% error rate"
      },
      "implementation_risks": {
        "risk_level": "low",
        "potential_issues": [
          "Brief downtime during rolling update (~30s per pod)",
          "Memory increase may trigger node pressure temporarily"
        ],analysis": {
      "current_distribution": {
        "node-1": {"pods": 3, "cpu_available": "500m", "mem_available": "2Gi", "can_fit": 1},
        "node-2": {"pods": 2, "cpu_available": "1000m", "mem_available": "3Gi", "can_fit": 2},
        "node-3": {"pods": 4, "cpu_available": "200m", "mem_available": "1Gi", "can_fit": 0}
      },
      "most_saturated_node": "node-3 (95% CPU, 90% Memory)",
      "best_candidate_node": "node-2 (50% CPU, 60% Memory)",
      "total_capacity_per_node": "6 pods max (considering 800m CPU request per pod)"
    },
    "vm_sizing_recommendations": {
      "current_instance_type": "t3.large (2 vCPU, 8GB RAM)",
      "recommended_for_scaling": "t3.xlarge (4 vCPU, 16GB RAM)",
      "reason": "Current nodes reaching capacity, larger instances provide better pod density",
      "cost_efficiency": "Better utilization with fewer, larger nodes vs many small nodes"
    },
    "scaling_timeline": {
      "reach_80_percent": "3 days at current growth rate",
      "reach_100_percent": "7 days at current growth rate",
      "new_nodes_needed_date": "2026-01-09 (7 days)",
      "recommended_action_date": "2026-01-05 (3 days) - provision before saturation"
    },
    "node_impact": "Would require 3 more nodes to be fully utilized OR 2 larger nodes (t3.xlarge)",
    "competing_apps_consideration": "app-backend is primary competitor for CPU resources",
    "bin_packing_efficiency": "Current: 68%, After optimization: 82% (better resource utilization)
      },
      "implementation_estimate": {
        "effort": "2 hours engineering time",
        "complexity": "low",
        "testing_required": "Recommended to validate in staging first"
      }
    }
  },
  "capacity_forecast": {
    "can_scale": true,
    "max_additional_replicas": 12,
    "limiting_factor": "CPU availability on nodes",
    "node_impact": "Would require 3 more nodes to be fully utilized",
    "competing_apps_consideration": "app-backend is primary competitor for CPU resources"
  },
  "optimizations": [
    {
      "type": "resource_limits",
      "priority": "high",
      "current": "cpu: 1000m, memory: 1Gi",
      "recommended": "cpu: 800m, memory: 1.2Gi",
      "reason": "CPU is overprovisioned, memory needs headroom",
      "expected_impact": "Better bin packing, reduce memory pressure"
    },
    {
      "type": "hpa_configuration",
      "priority": "high",
      "recommended": {
        "min_replicas": 3,
        "max_replicas": 8,
        "target_cpu": 70,
        "target_memory": 75
      },
      "reason": "Current HPA too aggressive causing thrashing"
    }
  ],
  "risks": {
    "immediate": [
      {
        "severity": "medium",
        "description": "CPU approaching 90% during peak hours",
        "probability": 0.7,
        "mitigation": "Add 2 replicas or increase CPU limit"
      }
    ],
    "medium_term": [
      {
        "severity": "high",
        "description": "Memory trend suggests OOMKill risk in 5-7 days",
        "probability": 0.6,
        "mitigation": "Investigate memory leak, increase limit to 1.5Gi"
      }
    ]
  },
  "priority_actions": [
    {
      "action": "Increase memory request to 800Mi and limit to 1.5Gi",
      "priority": 1,
      "reason": "Prevent predicted OOMKill",
      "effort": "low",
      "risk": "low"
    },
    {
      "action": "Configure HPA with min=3, max=8, targetCPU=70%",
      "priority": 2,
      "reason": "Automate scaling for peak hours",
      "effort": "low",
      "risk": "low"
    },
    {
      "action": "Reduce CPU request to 600m",
      "priority": 3,
      "reason": "Better node utilization",
      "effort": "low",
      "risk": "medium"
    }
  ],
  "maintenance_window": {
    "recommended_day": "Tuesday",
    "recommended_time": "03:00-05:00 UTC",
    "reason": "Lowest traffic period based on 30-day analysis",
    "average_load": "20% of peak"
  },
  "executive_summary": {
    "overview": "O deployment está operacional mas enfrenta problemas de capacidade que resultarão em falhas dentro de 5-7 dias se não tratados. Causa primária identificada: limites de memória inadequados com possível memory leak na aplicação.",
    "critical_findings": [
      "Memória crescendo linearmente e atingirá limite em 24h (70% probabilidade de OOMKill)",
      "CPU throttling durante horário de pico afetando 40% dos usuários",
      "Infraestrutura próxima da capacidade máxima, impossibilitando escalonamento adicional"
    ],
    "business_impact": "Downtime previsto de 2-4 horas nas próximas 72 horas se não houver intervenção. SLA em risco com impacto direto na disponibilidade do serviço.",
    "recommended_action": "URGENTE: Aumentar limites de memória e investigar memory leak. Ações podem ser aplicadas em janela de manutenção (Terça 03:00-05:00) com risco baixo.",
    "priority": "HIGH",
    "urgency": "Action required within 24 hours"
  }
}
```

---

## 🔧 Implementação Técnica

### Fase 1: Backend - Coleta de Dados (Semana 1)

#### 1.1 Estrutura de Dados

**Arquivo**: `internal/predictions/types.go`

```go
package predictions

import "time"

// PredictionRequest representa uma solicitação de análise preditiva
type PredictionRequest struct {
    Cluster    string
    Namespace  string
    Deployment string
    TimeRange  string // Ex: "7d", "30d"
}

// DeploymentMetrics contém todas as métricas coletadas
type DeploymentMetrics struct {
    Current    MetricSnapshot
    Historical []HistoricalSnapshot
    Nodes      NodeMetrics
    Competing  []CompetingApp
}

// MetricSnapshot representa métricas em um momento específico
type MetricSnapshot struct {
    Timestamp      time.Time
    CPUAvg         float64 // millicores
    CPUPeak        float64
    CPURequest     float64
    CPULimit       float64
    MemAvg         float64 // bytes
    MemPeak        float64
    MemRequest     float64
    MemLimit       float64
    NetworkRX      float64 // bytes/sec
    NetworkTX      float64
    RequestRate    float64 // req/sec
    ErrorRate      float64 // percentage
    LatencyP95     float64 // milliseconds
    Replicas       int32
    ReplicasReady  int32
    Restarts       int32
}

// HistoricalSnapshot contém dados históricos com contexto temporal
type HistoricalSnapshot struct {
    MetricSnapshot
    DaysAgo    int
    DayOfWeek  string
    HourOfDay  int
}

// NodeMetrics contém informações sobre nodes
type NodeMetrics struct {
    NodesUsed          []string
    TotalNodesAvailable int
    Distribution       map[string]int // node -> replica count
    TotalCPUCapacity   float64
    TotalCPUAllocated  float64
    TotalMemCapacity   float64
    TotalMemAllocated  float64
}

// CompetingApp representa aplicações que competem por recursos
type CompetingApp struct {
    Name       string
    Namespace  string
    CPUUsage   float64
    MemUsage   float64
    Replicas   int32
}

// PredictionResult contém o resultado da análise preditiva
type PredictionResult struct {
   Predição temporal
type Prediction struct {
    Timeframe     string  // "next_4_hours", "next_24_hours", "next_7_days"
    Event         string
    Probability   int     // 0-100
    Severity      string  // "low", "medium", "high", "critical"
    EstimatedTime time.Time
    Confidence    int     // 0-100
    Description   string
    Impact        string
}

type PredictionsAnalysis struct {
    ShortTerm  []Prediction // próximas 4h
    MediumTerm []Prediction // próximas 24h
    LongTerm   []Prediction // próximos 7d
}

// Análise de causa raiz
type RootCause struct {
    Cause              string
    Category           string   // "configuration", "application_issue", "dependency", etc.
    Certainty          int      // 0-100
    Evidence           []string
    InvestigationSteps []string
}

type RootCauseAnalysis struct {
    IdentifiedCauses    []RootCause
    PrimaryCause        string
    ContributingFactors []string
}

// Análise de impacto
type ImpactAnalysis struct {
    IfNoAction            NoActionImpact
    IfOptimizationsApplied OptimizationsImpact
}

type NoActionImpact struct {
    UserImpact           UserImpact
    InfrastructureImpact InfraImpact
    TimelineToFailure    FailureTimeline
}

type UserImpact struct {
    DowntimeProbability  int
    ExpectedDowntime     string
    DegradedPerformance  string
    ErrorRateIncrease    string
    AffectedUsers        string
}

type InfraImpact struct {
    CascadingFailures []string
    NodePressure      string
    ClusterStability  string
}

type FailureTimeline struct {
    CriticalFailureETA      string
    FirstUserImpactETA      string
    ServiceDegradationETA   string
}

type OptimizationsImpact struct {
    Improvements           Improvements
    ImplementationRisks    ImplementationRisks
    ImplementationEstimate ImplementationEstimate
}

type ImplementationEstimate struct {
    Effort          string
    Complexity      string
    TestingRequired string
}

type Improvements struct {
    StabilityIncrease  string
    PerformanceGain    string
    ResourceEfficiency string
    UserExperience     string
}

type ImplementationRisks struct {
    RiskLevel      string
    PotentialIssues []string
    Mitigation     string
}

// Sumário executivo
type ExecutiveSummary struct {
    Overview         string
    CriticalFindings []string
    BusinessImpact   string
    RecommendedAction string
    Priority         string // "LOW", "MEDIUM", "HIGH", "CRITICAL"
    Urgency          string
}

// Resultado completo atualizado
type PredictionResult struct {
    HealthScore         int
    HealthJustification string
    Trends              TrendsAnalysis
    Predictions         PredictionsAnalysis      // NOVO
    RootCauseAnalysis   RootCauseAnalysis        // NOVO
    ImpactAnalysis      ImpactAnalysis           // NOVO
    CapacityForecast    CapacityAnalysis
    Optimizations       []Optimization
    Risks               RiskAnalysis
    PriorityActions     []Action
    MaintenanceWindow   MaintenanceWindow
    ExecutiveSummary    ExecutiveSummary         // NOVO
    RawMetrics          DeploymentMetrics
    GeneratedAt         time.Time
}
    HealthJustification string
    Trends              TrendsAnalysis
    CapacityForecast    CapacityAnalysis
    Optimizations       []Optimization
    Risks               RiskAnalysis
    PriorityActions     []Action
    MaintenanceWindow   MaintenanceWindow
    RawMetrics          DeploymentMetrics
    GeneratedAt         time.Time
}

// ... outros tipos seguindo a estrutura JSON da IA
```

#### 1.2 Coletor Prometheus

**Arquivo**: `internal/predictions/prometheus_collector.go`

```go
package predictions

import (
    "context"
    "fmt"
    "time"
    promapi "github.com/prometheus/client_golang/api"
    promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

type PrometheusCollector struct {
    client promv1.API
}

func NewPrometheusCollector(prometheusURL string) (*PrometheusCollector, error) {
    client, err := promapi.NewClient(promapi.Config{Address: prometheusURL})
    if err != nil {
        return nil, err
    }
    return &PrometheusCollector{
        client: promv1.NewAPI(client),
    }, nil
}

func (pc *PrometheusCollector) CollectCurrentMetrics(ctx context.Context, req PredictionRequest) (*MetricSnapshot, error) {
    // Implementar queries Prometheus para métricas atuais
    // Retornar MetricSnapshot preenchido
}

func (pc *PrometheusCollector) CollectHistoricalMetrics(ctx context.Context, req PredictionRequest, daysAgo int) (*HistoricalSnapshot, error) {
    // Implementar queries com offset (ex: offset 7d)
    // Considerar hora do dia para comparação justa
}

func (pc *PrometheusCollector) CollectNodeMetrics(ctx context.Context, cluster string) (*NodeMetrics, error) {
    // Coletar informações sobre nodes e capacidade
}

func (pc *PrometheusCollector) CollectCompetingApps(ctx context.Context, cluster string, topN int) ([]CompetingApp, error) {
    // Identificar top N aplicações por consumo de recursos
}
```

#### 1.3 Analisador Principal

**Arquivo**: `internal/predictions/analyzer.go`

```go
package predictions

import (
    "context"
    "fmt"
)

type Analyzer struct {
    promCollector *PrometheusCollector
    aiIntegrator  *AIIntegrator
    k8sClient     *kubernetes.Clientset
}

func NewAnalyzer(promURL string, aiConfig AIConfig) (*Analyzer, error) {
    promCollector, err := NewPrometheusCollector(promURL)
    if err != nil {
        return nil, err
    }
    
    aiIntegrator, err := NewAIIntegrator(aiConfig)
    if err != nil {
        return nil, err
    }
    
    return &Analyzer{
        promCollector: promCollector,
        aiIntegrator:  aiIntegrator,
    }, nil
}

func (a *Analyzer) AnalyzeDeployment(ctx context.Context, req PredictionRequest) (*PredictionResult, error) {
    // 1. Coletar métricas atuais
    current, err := a.promCollector.CollectCurrentMetrics(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("failed to collect current metrics: %w", err)
    }
    
    // 2. Coletar histórico (7d, 30d)
    historical := []HistoricalSnapshot{}
    for _, days := range []int{7, 30} {
        hist, err := a.promCollector.CollectHistoricalMetrics(ctx, req, days)
        if err != nil {
            // Log warning mas continue
            continue
        }
        historical = append(historical, *hist)
    }
    
    // 3. Coletar métricas de nodes
    nodeMetrics, err := a.promCollector.CollectNodeMetrics(ctx, req.Cluster)
    if err != nil {
        return nil, fmt.Errorf("failed to collect node metrics: %w", err)
    }
    
    // 4. Coletar aplicações concorrentes
    competing, err := a.promCollector.CollectCompetingApps(ctx, req.Cluster, 5)
    if err != nil {
        // Não crítico, continue sem
        competing = []CompetingApp{}
    }
    
    // 5. Montar estrutura de métricas completa
    metrics := DeploymentMetrics{
        Current:    *current,
        Historical: historical,
        Nodes:      *nodeMetrics,
        Competing:  competing,
    }
    
    // 6. Enviar para IA para análise
    result, err := a.aiIntegrator.AnalyzeMetrics(ctx, req, metrics)
    if err != nil {
        return nil, fmt.Errorf("AI analysis failed: %w", err)
    }
    
    result.RawMetrics = metrics
    result.GeneratedAt = time.Now()
    
    return result, nil
}
```

#### 1.4 API Handler

**Arquivo**: `internal/web/handlers/prediction_handler.go`

```go
package handlers

import (
    "encoding/json"
    "net/http"
    "github.com/gorilla/mux"
    "your-project/internal/predictions"
)

type PredictionHandler struct {
    analyzer *predictions.Analyzer
}

func NewPredictionHandler(analyzer *predictions.Analyzer) *PredictionHandler {
    return &PredictionHandler{analyzer: analyzer}
}

func (h *PredictionHandler) AnalyzeDeployment(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    cluster := vars["cluster"]
    namespace := vars["namespace"]
    deployment := vars["deployment"]
    
    req := predictions.PredictionRequest{
        Cluster:    cluster,
        Namespace:  namespace,
        Deployment: deployment,
        TimeRange:  "30d", // padrão
    }
    
    result, err := h.analyzer.AnalyzeDeployment(r.Context(), req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}

// Registrar rota
// router.HandleFunc("/api/v1/clusters/{cluster}/namespaces/{namespace}/deployments/{deployment}/predict", handler.AnalyzeDeployment).Methods("POST")
```

### Fase 2: Frontend - Interface (Semana 2)

#### 2.1 Componente de Análise Preditiva

**Arquivo**: `internal/web/frontend/src/components/PredictiveAnalysisModal.tsx`

```tsx
import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Brain, TrendingUp, AlertTriangle, CheckCircle2, Loader2 } from "lucide-react";
import { Line } from 'react-chartjs-2';
import type { PredictionResult } from "@/lib/api/types";

interface PredictiveAnalysisModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  deployment: string;
}

export const PredictiveAnalysisModal = ({
  open,
  onOpenChange,
  cluster,
  namespace,
  deployment,
}: PredictiveAnalysisModalProps) => {
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<PredictionResult | null>(null);
  
  const runAnalysis = async () => {
    setLoading(true);
    try {
      const response = await fetch(
        `/api/v1/clusters/${cluster}/namespaces/${namespace}/deployments/${deployment}/predict`,
        { method: 'POST' }
      );
      const data = await response.json();
      setResult(data);
    } catch (error) {
      toast.error("Erro ao executar análise preditiva");
    } finally {
      setLoading(false);
    }
  };
  
  // Auto-executar ao abrir
  useEffect(() => {
    if (open) {
      runAnalysis();
    }
  }, [open]);
  
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-7xl max-h-[95vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Brain className="w-5 h-5" />
            Análise Preditiva - {deployment}
          </DialogTitle>
        </DialogHeader>
        
        {loading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-8 h-8 animate-spin" />
            <span className="ml-3">Analisando métricas e consultando IA...</span>
          </div>
        )}
        
        {result && (
          <div className="space-y-6">
            {/* Botões de Ação */}
            <div className="flex justify-end gap-2 mb-4">
              <Button variant="outline" size="sm" onClick={() => exportReport('markdown')}>
                <FileText className="w-4 h-4 mr-2" />
                Exportar Markdown
              </Button>
              <Button variant="outline" size="sm" onClick={() => exportReport('pdf')}>
                <FileDown className="w-4 h-4 mr-2" />
                Exportar PDF
              </Button>
              <Button variant="outline" size="sm" onClick={() => exportReport('json')}>
                <Code className="w-4 h-4 mr-2" />
                Exportar JSON
              </Button>
            </div>

            {/* Executive Summary (para gestores) */}
            <ExecutiveSummaryCard summary={result.executive_summary} />
            
            {/* Health Score */}
            <HealthScoreCard score={result.health_score} justification={result.health_justification} />
            
            {/* NOVO: Timeline de Predições */}
            <PredictionsTimelineSection predictions={result.predictions} />
            
            {/* NOVO: Análise de Causa Raiz */}
            <RootCauseSection analysis={result.root_cause_analysis} />
            
            {/* NOVO: Análise de Impacto */}
            <ImpactAnalysisSection impact={result.impact_analysis} />
            
            {/* Trends */}
            <TrendsSection trends={result.trends} />
            
            {/* Capacity Forecast */}
            <CapacitySection forecast={result.capacity_forecast} />
            
            {/* Risks */}
            <RisksSection risks={result.risks} />
            
            {/* Priority Actions */}
            <ActionsSection actions={result.priority_actions} />
            
            {/* Optimizations */}
            <OptimizationsSection optimizations={result.optimizations} />
            
            {/* Maintenance Window */}
            <MaintenanceSection window={result.maintenance_window} />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};
```

#### 2.2 Integração no DeploymentsTab

**Modificação em**: `internal/web/frontend/src/components/DeploymentsTab.tsx`

```tsx
// Adicionar estado
const [predictiveAnalysisOpen, setPredictiveAnalysisOpen] = useState(false);

// Modificar rightTitleAction
const rightTitleAction = (
  <div className="flex gap-2">
    {selectedDeployment && isDeploymentProblematic(selectedDeployment) && (
      <AITriggerButton
        resourceType="Deployment"
        cluster={cluster}
        namespace={selectedDeployment.namespace}
        resourceName={selectedDeployment.name}
        size="sm"
        variant="outline"
      />
    )}
    {selectedDeployment && (
      <Button
        variant="secondary"
        size="sm"
        onClick={() => setPredictiveAnalysisOpen(true)}
      >
        <Brain className="w-4 h-4 mr-2" />
        Análise Preditiva
      </Button>
    )}
    <Button
      variant="outline"
      size="sm"
      onClick={refreshManifest}
      disabled={!selectedDeployment || manifestLoading}
    >
      <RefreshCcw className="w-4 h-4 mr-2" />
      Recarregar YAML
    </Button>
  </div>
);

// Adicionar modal no render
<PredictiveAnalysisModal
  open={predictiveAnalysisOpen}
  onOpenChange={setPredictiveAnalysisOpen}
  cluster={cluster}
  namespace={selectedDeployment?.namespace || ""}
  deployment={selectedDeployment?.name || ""}
/>
```

---

## 📅 Cronograma de Implementação

### Sprint 1 (Semana 1): Backend Core
- [ ] Criar estrutura de tipos (`types.go`)
- [ ] Implementar `PrometheusCollector` com queries básicas
- [ ] Implementar coleta de métricas atuais
- [ ] Implementar coleta de métricas históricas com offset
- [ ] Testes unitários do coletor

### Sprint 2 (Semana 1-2): Backend - Node Analysis
- [ ] Implementar coleta de métricas de nodes
- [ ] Implementar identificação de aplicações concorrentes
- [ ] Implementar análise de distribuição
- [ ] Testes de integração com Prometheus

### Sprint 3 (Semana 2): Backend - AI Integration
- [ ] Implementar `AIIntegrator` com template de prompt
- [ ] Criar parser de resposta JSON da IA
- [ ] Implementar fallback em caso de erro da IA
- [ ] Handler HTTP completo
- [ ] Documentação da API

### Sprint 4 (Semana 2-3): Frontend - UI Components
- [ ] Criar `PredictiveAnalysisModal.tsx`
- [ ] Implementar `HealthScoreCard` component
- [ ] Implementar `PredictionsTimeline` component (timeline visual de eventos futuros)
- [ ] Implementar `RootCauseSection` component (análise de causas)
- [ ] Implementar `ImpactAnalysisSection` component (cenários "e se...")
- [ ] Implementar gráficos de tendências (CPU, Memória)
- [ ] Implementar seção de capacidade
- [ ] Implementar seção de riscos

### Sprint 5 (Semana 3): Frontend - Actions & Reports
- [ ] Implementar seção de ações prioritárias
- [ ] Implementar seção de otimizações
- [ ] Implementar `ExecutiveSummaryCard` (sumário para gestores)
- [ ] Implementar exportação de relatório (PDF, Markdown, JSON)
- [ ] Adicionar gráficos comparativos (atual vs histórico)
- [ ] Integrar botão no DeploymentsTab
- [ ] Testes E2E

### Sprint 6 (Semana 4): Polish & Optimization
- [ ] Cache de resultados (5 minutos)
- [ ] Loading states e error handling
- [ ] Otimização de queries Prometheus
- [ ] Documentação de usuário
- [ ] Review de código e QA

---

## � Sistema de Relatórios

### Formatos Suportados

#### 1. **Markdown Report** (para documentação)

```markdown
# Análise Preditiva: frontend-api
**Cluster**: production-us-east-1  
**Namespace**: default  
**Gerado em**: 02/01/2026 15:30:00 UTC  
**Score de Saúde**: 85/100 ⚠️

---

## 📊 Sumário Executivo

O deployment está operacional mas enfrenta problemas de capacidade que resultarão 
em falhas dentro de 5-7 dias se não tratados...

**Prioridade**: HIGH  
**Urgência**: Ação necessária em 24 horas

---

## 🔮 Predições

### Próximas 4 Horas
- ⚠️ **CPU throttling** (75% probabilidade, severidade média)
  - Estimado para: 02/01/2026 18:00
  - Impacto: Response time +200ms
  
### Próximas 24 Horas
- 🚨 **OOMKill** (60% probabilidade, severidade alta)
  - Estimado para: 03/01/2026 02:00
  - Impacto: Reinício de pods, perda de sessões

---

## 🔍 Análise de Causa Raiz

### Causa Primária (90% certeza)
**Limites de memória inadequados**

**Evidências:**
- Memory usage at 95% of limit
- OOMKill events in last 48h
- Working set consistently exceeds request

**Investigação Recomendada:**
1. Review application memory profile
2. Check for memory leaks with profiling tools
3. Analyze heap dumps from recent OOMKills

---
 de Predição**: 
   - Predições de curto prazo (4h) com 80%+ de acurácia
   - Predições de médio prazo (24h) com 70%+ de acurácia
   - Causa raiz identificada corretamente em 75%+ dos casos
3. **Acionabilidade**: IA fornece pelo menos 3 recomendações acionáveis com impacto mensurável
4. **Usabilidade**: 
   - Modal intuitivo com gráficos claros
   - Relatório compreensível para não-técnicos
   - Exportação em < 3 segundos
5. **Confiabilidade**: 95% de uptime da feature
6. **Adoção**: 
   - Utilizada em 50% dos deployments problemáticos
   - Relatórios compartilhados em 30% das análises
7. **Impacto Mensurável**:
   - Redução de 40% em incidentes previsíveis
   - Redução de 30% em MTTR (Mean Time To Resolution)
- **Downtime previsto**: 2-4 horas nas próximas 72h (70% probabilidade)
- **Usuários afetados**: ~10,000 durante pico (40% do total)
- **Impacto no SLA**: Risco de violação dos acordos de nível de serviço

### Se Otimizações Forem Aplicadas
- **Melhoria de estabilidade**: +40%
- **Ganho de performance**: P95 latency -150ms
- **Eficiência de recursos**: Melhor utilização de nodes e redução de desperdício

---

## ✅ Ações Prioritárias

1. **Aumentar memory request para 800Mi e limit para 1.5Gi** (URGENTE)
   - Prioridade: 1
   - Esforço: Baixo (2 horas)
   - Risco: Baixo
   
2. **Configurar HPA com min=3, max=8, targetCPU=70%**
   - Prioridade: 2
   - Esforço: Baixo
   
...

---

## 📈 Métricas Históricas

[Gráficos e dados detalhados]

---

**Relatório gerado por K8s HPA Manager - Análise Preditiva v1.0**
```

#### 2. **PDF Report** (para stakeholders)

- Formatação profissional com logo
- Gráficos incorporados
- Sumário executivo destacado
- Código de cores para severidade
- Gerado via biblioteca `go-pdf` ou `wkhtmltopdf`

#### 3. **JSON Export** (para integração)

```json
{
  "report_metadata": {
    "deployment": "frontend-api",
    "cluster": "production-us-east-1",
    "generated_at": "2026-01-02T15:30:00Z",
    "version": "1.0"
  },
  "analysis": { ... },
  "exportable": true
}
```

### Implementação de Exportação

**Backend Handler**:

```go
// internal/predictions/report_generator.go
type ReportGenerator struct {
    result *PredictionResult
}

func (rg *ReportGenerator) GenerateMarkdown() (string, error) {
    // Template Markdown
}

func (rg *ReportGenerator) GeneratePDF() ([]byte, error) {
    // Gerar PDF usando wkhtmltopdf ou go-pdf
}

func (rg *ReportGenerator) GenerateJSON() ([]byte, error) {
    return json.MarshalIndent(rg.result, "", "  ")
}
```

**Frontend**:

```tsx
const exportReport = async (format: 'markdown' | 'pdf' | 'json') => {
  const response = await fetch(
    `/api/v1/clusters/${cluster}/namespaces/${namespace}/deployments/${deployment}/predict/export?format=${format}`
  );
  
  const blob = await response.blob();
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `prediction-${deployment}-${Date.now()}.${format}`;
  a.click();
};
```

---

## 🔐 Considerações de Segurança e Sanitização

### Segurança Geral
1. **Autenticação**: Reutilizar sistema RBAC existente
2. **Rate Limiting**: Máximo 1 análise por deployment a cada 5 minutos
3. **Validação**: Validar todos os inputs (cluster, namespace, deployment)
4. **Logs**: Registrar todas as análises para auditoria

### 🔒 Sanitização de Dados Sensíveis (CRÍTICO)

**IMPORTANTE**: Todos os dados coletados devem passar pelo sistema **Sanitizer** antes de serem:
- Enviados para IA
- Exibidos na interface
- Exportados em relatórios
- Armazenados em cache/logs

#### Dados que DEVEM ser Sanitizados:

1. **Informações de Identificação**:
   - Nomes de clusters (mascarar para "cluster-xxx")
   - Nomes de namespaces sensíveis (produção, staging)
   - IPs internos e ranges de rede
   - Nomes de nodes (mascarar identificadores únicos)
   - Nomes de VMs e instance IDs

2. **Métricas e Valores**:
   - URLs de serviços externos
   - Connection strings
   - Tokens ou secrets acidentalmente expostos em variáveis de ambiente
   - Credenciais em annotations ou labels

3. **Logs e Eventos**:
   - Stack traces com paths internos
   - Mensagens de erro com informações sensíveis
   - Nomes de usuários ou emails

4. **Relatórios Exportados**:
   - Todos os dados devem ser sanitizados antes da exportação
   - PDF, Markdown e JSON devem conter apenas dados mascarados

#### Implementação da Sanitização:

**No Backend (Go)**:

```go
import "your-project/internal/sanitizer"

// Antes de enviar para IA
func (a *Analyzer) AnalyzeDeployment(ctx context.Context, req PredictionRequest) (*PredictionResult, error) {
    // 1. Coletar métricas
    metrics := a.collectMetrics(ctx, req)
    
    // 2. SANITIZAR dados antes de enviar para IA
    sanitizedMetrics := sanitizer.SanitizeMetrics(metrics)
    
    // 3. Enviar para IA
    result := a.aiIntegrator.AnalyzeMetrics(ctx, req, sanitizedMetrics)
    
    // 4. SANITIZAR resposta da IA (pode conter dados refletidos)
    sanitizedResult := sanitizer.SanitizeResult(result)
    
    return sanitizedResult, nil
}

// Antes de exportar relatórios
func (rg *ReportGenerator) GenerateMarkdown() (string, error) {
    // Sanitizar todos os campos antes de gerar relatório
    sanitizedResult := sanitizer.SanitizeForExport(rg.result)
    return rg.templateMarkdown(sanitizedResult)
}
```

**No Frontend (TypeScript)**:

```tsx
import { sanitizeForDisplay } from '@/lib/sanitizer';

// Antes de exibir na UI
const displayMetrics = sanitizeForDisplay(result.raw_metrics);

// Antes de exportar
const exportReport = async (format: string) => {
    const sanitizedData = sanitizeForExport(result);
    // ... proceder com exportação
};
```

#### Configuração do Sanitizer:

```yaml
# config/sanitizer.yaml
sanitization:
  enabled: true
  mode: strict  # strict, moderate, permissive
  
  patterns:
    # IPs
    - pattern: '\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b'
      replacement: '***.***.***.***.***'
      
    # Cluster names (preservar prefixo)
    - pattern: 'cluster-(prod|staging|dev)-[a-z0-9]+'
      replacement: 'cluster-$1-***'
      
    # Node names / VM IDs
    - pattern: '(node|ip|vm|instance)-[a-z0-9-]+'
      replacement: '$1-***'
      
    # Email addresses
    - pattern: '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}'
      replacement: '***@***'
      
    # Secrets/Tokens
    - pattern: '(token|secret|password|key)[:=]\s*["\']?[^\s"\',}]+'
      replacement: '$1=***REDACTED***'
  
  preserve_structure: true  # Manter estrutura dos dados
  audit_log: true          # Logar todas as sanitizações
```

#### Checklist de Sanitização:

- [ ] Integrar chamadas ao Sanitizer em todos os pontos de coleta
- [ ] Sanitizar antes de enviar para IA
- [ ] Sanitizar resposta da IA
- [ ] Sanitizar antes de exibir na UI
- [ ] Sanitizar antes de exportar relatórios
- [ ] Sanitizar logs e audit trails
- [ ] Testar com dados reais para validar eficácia
- [ ] Documentar padrões de sanitização para o time

#### Exemplo de Dados Sanitizados:

**Antes**:
```json
{
  "cluster": "production-us-east-1-kubernetes-v1.28",
  "node": "ip-10-0-1-45.ec2.internal",
  "namespace": "payment-processing",
  "event": "Failed to connect to database at mysql://admin:pass123@10.0.2.100:3306"
}
```

**Depois**:
```json
{
  "cluster": "cluster-prod-***",
  "node": "node-***",
  "namespace": "namespace-***",
  "event": "Failed to connect to database at ***://***:***@***.***.***.***.***:***"
}
```

### Benefícios da Sanitização:

1. ✅ **Conformidade**: Atende requisitos de segurança e compliance
2. ✅ **Proteção de Dados**: Impede vazamento de informações sensíveis
3. ✅ **Segurança da IA**: Evita exposição de dados em prompts enviados para IA externa
4. ✅ **Auditoria**: Facilita compartilhamento de relatórios sem riscos
5. ✅ **Confiança**: Permite uso seguro da funcionalidade em ambientes críticos

---

## 📊 Queries Prometheus - Cheat Sheet

### Comparação Temporal (mesmo horário, dias atrás)

```promql
# CPU atual
rate(container_cpu_usage_seconds_total{namespace="default",pod=~"nginx-.*"}[5m])

# CPU há 7 dias (mesmo horário)
rate(container_cpu_usage_seconds_total{namespace="default",pod=~"nginx-.*"}[5m]) offset 7d

# CPU há 30 dias (mesmo horário)
rate(container_cpu_usage_seconds_total{namespace="default",pod=~"nginx-.*"}[5m]) offset 30d
```

### Range Vector para Tendências

```promql
# CPU nas últimas 24h (pontos a cada 5m)
rate(container_cpu_usage_seconds_total{namespace="default",pod=~"nginx-.*"}[5m])[24h:5m]
```

### Agregação por Horário

```promql
# Média de CPU por hora do dia (última semana)
avg_over_time(
  rate(container_cpu_usage_seconds_total{namespace="default",pod=~"nginx-.*"}[5m])[7d:1h]
) by (hour)
```

### Capacidade de Node

```promql
# CPU disponível = alocável - já requisitado
sum(kube_node_status_allocatable{resource="cpu"}) 
- 
sum(kube_pod_container_resource_requests{resource="cpu"})
```

---

## 🎯 Critérios de Sucesso

1. **Performance**: Análise completa em < 10 segundos
2. **Precisão**: IA fornece pelo menos 3 recomendações acionáveis
3. **Usabilidade**: Modal intuitivo com gráficos claros
4. **Confiabilidade**: 95% de uptime da feature
5. **Adoção**: Utilizada em 50% dos deployments problemáticos

---

## 🔄 Melhorias Futuras (v2.0)

1. **Machine Learning Local**: Treinar modelo próprio com histórico
2. **Alertas Proativos**: Notificar quando análise detectar riscos críticos
3. **Análise em Lote**: Analisar múltiplos deployments simultaneamente
4. **Comparação**: Comparar deployment com peers do mesmo namespace
5. **Recomendações Aplicáveis**: Botão para aplicar otimizações automaticamente
6. **Dashboard Preditivo**: Visão consolidada de todos os deployments
7. **Validação de Predições**: Sistema de feedback para melhorar acurácia
8. **Relatórios Agendados**: Enviar relatórios semanais automáticos
9. **Simulador de Cenários**: "E se eu aumentar para X réplicas?"
10. **Integração com Incident Management**: Criar tickets automaticamente para riscos críticos

---

## 📚 Referências Técnicas

- [Prometheus Query Examples](https://prometheus.io/docs/prometheus/latest/querying/examples/)
- [Kubernetes Metrics](https://kubernetes.io/docs/concepts/cluster-administration/system-metrics/)
- [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics/tree/main/docs)
- [Best Practices for Resource Management](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)

---

## 📝 Notas de Implementação

1. **Prometheus URL**: Deve ser configurável via env var ou config
2. **AI Provider**: Suportar Claude e GPT-4 com fallback
3. **Timeout**: Análise deve ter timeout de 30s
4. **Cache**: Implementar cache Redis opcional para resultados
5. **Testes**: Mockar Prometheus e IA para testes unitários

---

**Documento vivo** - Atualizar conforme progresso da implementação.
