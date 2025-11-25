# 🎯 Alertas Selecionados para HPAs e Node Pools

**Data:** 25 de novembro de 2025
**Status:** ✅ Definição Completa
**Fonte:** Prometheus API `/api/v1/alerts` e `/api/v1/rules`

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 📋 Sumário Executivo

Após análise do AlertManager e Prometheus, identificamos **8 alertas críticos** para integração com HPAs e **0 alertas específicos** para Node Pools (ainda não configurados).

### 🎯 Decisão Final

**Para HPAs**: Integrar 8 alertas existentes
**Para Node Pools**: Alertas customizados não existem - **DECISÃO PENDENTE**

---

## ✅ Alertas para HPAs (8 selecionados)

### 1. **KubeHpaMaxedOut** 🔴
**Severidade**: Warning
**Descrição**: HPA atingiu o número máximo de réplicas configurado

**Query PromQL**:
```promql
kube_horizontalpodautoscaler_status_current_replicas == kube_horizontalpodautoscaler_spec_max_replicas
```

**Ação Sugerida**:
- ✅ **Auto-Mitigável**: SIM
- 🔧 **Ação**: Aumentar `spec.maxReplicas` do HPA
- 📊 **Prioridade**: ALTA

**Exemplo de Mitigação**:
```yaml
# Antes
maxReplicas: 10

# Depois (sugestão: +50%)
maxReplicas: 15
```

---

### 2. **KubeHpaReplicasMismatch** ⚠️
**Severidade**: Warning
**Descrição**: HPA não consegue atingir o número desejado de réplicas

**Query PromQL**:
```promql
(kube_horizontalpodautoscaler_status_desired_replicas != kube_horizontalpodautoscaler_status_current_replicas)
AND (kube_horizontalpodautoscaler_status_current_replicas > kube_horizontalpodautoscaler_spec_min_replicas)
AND (kube_horizontalpodautoscaler_status_current_replicas < kube_horizontalpodautoscaler_spec_max_replicas)
AND changes(kube_horizontalpodautoscaler_status_current_replicas[15m]) == 0
```

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Verificar recursos do Node Pool (pode estar cheio)
- 📊 **Prioridade**: ALTA
- 🔗 **Correlação**: Pode indicar problema no Node Pool

**Possíveis Causas**:
1. Node Pool sem capacidade (maxed out)
2. PodDisruptionBudget bloqueando scale
3. Insufficient resources (CPU/Memory)

---

### 3. **CPUThrottlingHigh** ℹ️
**Severidade**: Info
**Descrição**: CPU throttling acima de 25% (pod limitado por CPU limit)

**Query PromQL**:
```promql
sum by (container, pod, namespace) (increase(container_cpu_cfs_throttled_periods_total{container!=""}[5m]))
/ sum by (container, pod, namespace) (increase(container_cpu_cfs_periods_total[5m]))
> 0.25
```

**Ação Sugerida**:
- ✅ **Auto-Mitigável**: SIM
- 🔧 **Ação**: Aumentar `resources.limits.cpu` ou `resources.requests.cpu`
- 📊 **Prioridade**: MÉDIA

**Exemplo de Mitigação**:
```yaml
# Opção 1: Aumentar CPU limit
resources:
  limits:
    cpu: "2000m"  # era 1000m
  requests:
    cpu: "500m"

# Opção 2: Scale horizontal (aumentar replicas)
# Se HPA já está maxed out → aumentar maxReplicas
```

---

### 4. **Eventos OOMKilled** 🔴
**Severidade**: Critical
**Descrição**: Container foi morto por falta de memória (Out Of Memory)

**Query PromQL**:
```promql
sum by (namespace, reason) (kube_pod_container_status_last_terminated_reason{reason=~"OOMKilled"}) > 0
OR sum by (namespace, reason) (kube_pod_container_status_waiting_reason{reason=~"OOMKilled"}) > 0
```

**Ação Sugerida**:
- ✅ **Auto-Mitigável**: SIM
- 🔧 **Ação**: Aumentar `resources.limits.memory`
- 📊 **Prioridade**: CRÍTICA

**Exemplo de Mitigação**:
```yaml
resources:
  limits:
    memory: "2Gi"  # era 1Gi
  requests:
    memory: "1Gi"
```

---

### 5. **Eventos CrashLoopBackOff** 🔴
**Severidade**: Critical
**Descrição**: Container está em loop de crash contínuo

**Query PromQL**:
```promql
sum by (namespace, reason) (kube_pod_container_status_last_terminated_reason{reason=~"CrashLoopBackOff"}) > 0
OR sum by (namespace, reason) (kube_pod_container_status_waiting_reason{reason=~"CrashLoopBackOff"}) > 0
```

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Requer análise de logs (problema de aplicação)
- 📊 **Prioridade**: CRÍTICA
- 📝 **Registro**: Apenas alertar no History Tracker

**Investigação Necessária**:
- Verificar logs do pod: `kubectl logs <pod> --previous`
- Verificar eventos: `kubectl describe pod <pod>`

---

### 6. **KubePodCrashLooping** ⚠️
**Severidade**: Warning
**Descrição**: Pod em estado CrashLoopBackOff há mais de 5 minutos

**Query PromQL**:
```promql
max_over_time(kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"}[5m]) >= 1
```

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Similar a CrashLoopBackOff (análise de logs)
- 📊 **Prioridade**: ALTA

---

### 7. **KubePodNotReady** ⚠️
**Severidade**: Warning
**Descrição**: Pod em estado Pending, Unknown ou Failed

**Query PromQL**:
```promql
sum by (namespace, pod, cluster) (
  max by (namespace, pod, cluster) (
    kube_pod_status_phase{phase=~"Pending|Unknown|Failed"}
  ) * on (namespace, pod, cluster) group_left (owner_kind)
  topk by (namespace, pod, cluster) (
    1, max by (namespace, pod, owner_kind, cluster) (
      kube_pod_owner{owner_kind!="Job"}
    )
  )
) > 0
```

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Depende do motivo (resources, node pressure, etc.)
- 📊 **Prioridade**: ALTA

**Possíveis Causas**:
- Insufficient CPU/Memory
- Node Pool sem capacidade
- PodDisruptionBudget
- ImagePullBackOff

---

### 8. **KubeDeploymentReplicasMismatch** ⚠️
**Severidade**: Warning
**Descrição**: Deployment não consegue manter o número desejado de réplicas

**Query PromQL**:
```promql
(kube_deployment_spec_replicas > kube_deployment_status_replicas_available)
AND (changes(kube_deployment_status_replicas_updated[10m]) == 0)
```

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Verificar se é problema de recursos ou Node Pool
- 📊 **Prioridade**: ALTA
- 🔗 **Correlação**: Similar a KubeHpaReplicasMismatch

---

## ✅ Alertas para Node Pools (11 selecionados)

### 🎯 Decisão: Opção A - Alertas Node-Level Existentes

Usaremos alertas Node-level do Kubernetes que já existem e são relevantes para capacidade e saúde dos Node Pools.

---

### 🔴 Alertas CRÍTICOS (6 alertas)

#### 1. **KubeNodeNotReady** 🔴
**Severidade**: Critical (esperado)
**Descrição**: Node não está em estado Ready

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Investigar node (pode estar com problema de rede, kubelet, etc.)
- 📊 **Prioridade**: CRÍTICA
- 🔗 **Impacto Node Pool**: Node indisponível reduz capacidade do pool

---

#### 2. **KubeNodeUnreachable** 🔴
**Severidade**: Critical (esperado)
**Descrição**: Node não está acessível pela control plane

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Problema de rede ou node totalmente offline
- 📊 **Prioridade**: CRÍTICA
- 🔗 **Impacto Node Pool**: Similar a NodeNotReady

---

#### 3. **KubeletDown** 🔴
**Severidade**: Warning/Critical (esperado)
**Descrição**: Kubelet do node parou de responder

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Reiniciar node ou investigar kubelet
- 📊 **Prioridade**: CRÍTICA

---

#### 4. **KubeletTooManyPods** ⚠️
**Severidade**: Info/Warning (esperado)
**Descrição**: Node está executando muitos pods (próximo ao limite)

**Ação Sugerida**:
- ✅ **Auto-Mitigável**: SIM
- 🔧 **Ação**: Aumentar `count` ou `max_count` do Node Pool
- 📊 **Prioridade**: ALTA
- 💡 **Trigger**: Principal alerta para auto-scaling de Node Pool!

**Possível Ação Automática**:
```yaml
# Se Node Pool está com todos os nodes em "TooManyPods"
# → Aumentar count do Node Pool
current_count: 3
suggested_count: 5  # +2 nodes
```

---

#### 5. **NodeFilesystemAlmostOutOfSpace** ⚠️
**Severidade**: Warning
**Descrição**: Disco do node está quase cheio (>80% usado)

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Limpar imagens antigas ou aumentar disk size
- 📊 **Prioridade**: ALTA
- 🔗 **Impacto Node Pool**: Pode impedir scheduling de novos pods

---

#### 6. **NodeFilesystemSpaceFillingUp** ⚠️
**Severidade**: Warning
**Descrição**: Disco do node enchendo rapidamente

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Similar a AlmostOutOfSpace mas preditivo
- 📊 **Prioridade**: MÉDIA

---

### 🟡 Alertas IMPORTANTES (5 alertas)

#### 7. **KubeNodeReadinessFlapping** ⚠️
**Severidade**: Warning
**Descrição**: Node alternando entre Ready/NotReady (instável)

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Investigar instabilidade (rede, kubelet, resources)
- 📊 **Prioridade**: MÉDIA

---

#### 8. **KubeletPlegDurationHigh** ℹ️
**Severidade**: Warning
**Descrição**: Kubelet demorando muito para listar pods (PLEG)

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Node sob pressão, considerar scale do Node Pool
- 📊 **Prioridade**: MÉDIA
- 🔗 **Correlação**: Pode indicar necessidade de mais nodes

---

#### 9. **KubeletPodStartUpLatencyHigh** ℹ️
**Severidade**: Warning
**Descrição**: Pods demorando muito para iniciar no node

**Ação Sugerida**:
- ⚠️ **Auto-Mitigável**: PARCIAL
- 🔧 **Ação**: Node sobrecarregado, considerar scale
- 📊 **Prioridade**: MÉDIA

---

#### 10. **NodeHighNumberConntrackEntriesUsed** ℹ️
**Severidade**: Warning
**Descrição**: Node com muitas conexões de rede abertas

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Investigar aplicações com muitas conexões
- 📊 **Prioridade**: BAIXA

---

#### 11. **NodeNetworkInterfaceFlapping** ⚠️
**Severidade**: Warning
**Descrição**: Interface de rede do node instável

**Ação Sugerida**:
- ❌ **Auto-Mitigável**: NÃO
- 🔧 **Ação**: Problema de rede ou hardware
- 📊 **Prioridade**: MÉDIA

---

## 📊 Resumo por Prioridade

### 🔴 CRÍTICA (9 alertas)

**HPAs:**
1. **Eventos OOMKilled** - Memory limit insuficiente
2. **Eventos CrashLoopBackOff** - Aplicação com erro

**Node Pools:**
3. **KubeNodeNotReady** - Node não disponível
4. **KubeNodeUnreachable** - Node offline
5. **KubeletDown** - Kubelet parou

### 🟡 ALTA (8 alertas)

**HPAs:**
1. **KubeHpaMaxedOut** - HPA no limite
2. **KubeHpaReplicasMismatch** - HPA não consegue escalar
3. **KubePodCrashLooping** - Pod crashando
4. **KubePodNotReady** - Pod não está pronto
5. **KubeDeploymentReplicasMismatch** - Deployment instável

**Node Pools:**
6. **KubeletTooManyPods** - Node com muitos pods ⭐ (trigger para scale)
7. **NodeFilesystemAlmostOutOfSpace** - Disco cheio

### 🔵 MÉDIA (2 alertas)

**HPAs:**
1. **CPUThrottlingHigh** - CPU limitada

**Node Pools:**
2. **NodeFilesystemSpaceFillingUp** - Disco enchendo
3. **KubeNodeReadinessFlapping** - Node instável
4. **KubeletPlegDurationHigh** - Node sob pressão
5. **KubeletPodStartUpLatencyHigh** - Latência alta
6. **NodeNetworkInterfaceFlapping** - Rede instável

### 📊 Total Selecionado
- **HPAs**: 8 alertas
- **Node Pools**: 11 alertas
- **TOTAL**: 19 alertas

---

## 🎯 Plano de Implementação

### Fase 1: Cliente Base (Imediato) ✅
1. **Cliente Go** para buscar alertas via Prometheus API (`/api/v1/alerts`)
2. **Modelo de dados** para alertas
3. **Handler Web** para expor alertas via REST API

### Fase 2: Integração HPAs 🚧
Implementar integração com os **8 alertas de HPA**:

1. **Correlação** alerta → HPA (via `namespace` + `deployment`/`statefulset`)
2. **Badge visual** na lista de HPAs (🔴 critical, 🟡 warning, ℹ️ info)
3. **Painel de alertas** por HPA no frontend
4. **Sugestões de ação** automáticas para alertas auto-mitigáveis
5. **Registro no History Tracker** quando ações são tomadas

### Fase 3: Integração Node Pools 🚧
Implementar integração com os **11 alertas Node-level**:

1. **Correlação** alerta → Node Pool (via `node` label)
2. **Badge visual** na lista de Node Pools
3. **Painel de alertas** por Node Pool
4. **Sugestões de ação** para **KubeletTooManyPods** (principal trigger)
5. **Auto-scaling**: Sugerir aumento de `count` quando múltiplos nodes com TooManyPods

---

## 🔗 Próximas Etapas

### ✅ Decisões Tomadas
- [x] Alertas para HPAs: **8 alertas selecionados**
- [x] Alertas para Node Pools: **Opção A - 11 alertas Node-level**

### 🚀 Implementação

1. **Implementar cliente** Go para Prometheus API `/api/v1/alerts`
2. **Criar modelo de dados** para alertas (Alert struct)
3. **Handler Web** para expor alertas via REST API
4. **Integrar com frontend** React (badges e painéis)
5. **Adicionar ações** de mitigação sugeridas
6. **Registrar ações** no History Tracker

---

## 📚 Referências

- **Prometheus API**: https://prometheus.io/docs/prometheus/latest/querying/api/
- **Kube-State-Metrics**: https://github.com/kubernetes/kube-state-metrics/tree/main/docs
- **PrometheusRule CRD**: https://prometheus-operator.dev/docs/operator/api/#monitoring.coreos.com/v1.PrometheusRule

---

**Status**: ✅ Alertas HPA Definidos | ✅ Alertas Node Pool Definidos (Node-level)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
