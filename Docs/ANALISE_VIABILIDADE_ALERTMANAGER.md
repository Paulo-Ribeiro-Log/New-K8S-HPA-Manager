# 📊 Análise de Viabilidade: Integração Alertmanager + FluentD com K8s HPA Manager

**Data:** 19 de novembro de 2025  
**Versão:** 2.0  
**Status:** Análise Técnica Completa + Validação de Infraestrutura  
**Documento Base:** [ALERTMANAGER_HPA_INTEGRATION.md](docs/studies/ALERTMANAGER_HPA_INTEGRATION.md)

---

## 📋 Sumário Executivo

Esta análise avalia a viabilidade técnica da integração proposta entre o K8s HPA Manager, Alertmanager e FluentD, considerando que **ambos estão instalados em todos os clusters**.

### 🎯 Resultado

**VIABILIDADE: MUITO ALTA (95%)**

✅ **Alertmanager**: Instalado em todos os clusters - **PRIORIDADE IMEDIATA**  
⚠️ **FluentD**: Instalado, mas requer validação do backend - **FASE 2 CONDICIONAL**

A integração é tecnicamente viável e oferece alto valor agregado. A infraestrutura base está sólida e confirmada.

---

## 🚀 Confirmação de Infraestrutura

### **Status dos Serviços** ✅

| Serviço | Status | Clusters | Prioridade |
|---------|--------|----------|------------|
| **Alertmanager** | ✅ Instalado | Todos | 🔴 ALTA |
| **FluentD** | ✅ Instalado | Todos | 🟡 MÉDIA |
| **Prometheus** | ✅ Instalado | Todos | ✅ Integrado |

**Conclusão**: Infraestrutura completa para implementação imediata do Alertmanager.

---

## ✅ Pontos Fortes da Proposta

### 1. **Infraestrutura Base Sólida** ✅

A aplicação já possui componentes fundamentais:

- ✅ **Prometheus Client existente**: `/internal/monitoring/prometheus/client.go` já implementado e funcional
- ✅ **Discovery System**: `/internal/monitoring/discovery/prometheus.go` com padrão estabelecido
- ✅ **History Tracker**: Sistema de logging maduro e extensível (v1.1.1)
- ✅ **Frontend AlertsPanel**: Componente já existente para exibir anomalias
- ✅ **MonitoringEngineV2**: Engine de coleta com cache (TTL 30s)
- ✅ **Alertmanager**: Confirmado em todos os clusters ⭐
- ✅ **FluentD**: Confirmado em todos os clusters ⭐

### 2. **Padrão de Discovery Replicável** ✅

O sistema já usa discovery automático para Prometheus:

```go
buildPrometheusURL(nome, ambiente string) string {
    return fmt.Sprintf("https://prometheus-%s-%s.viavarejo.com.br/", nome, ambiente)
}
```

**Padrão equivalente para Alertmanager:**
```go
buildAlertmanagerURL(nome, ambiente string) string {
    return fmt.Sprintf("https://alertmanager-%s-%s.viavarejo.com.br/", nome, ambiente)
    // OU internamente: http://alertmanager.monitoring.svc:9093
}
```

### 3. **Arquitetura Modular** ✅

- ✅ Separação clara de responsabilidades
- ✅ Handlers isolados (`internal/web/handlers/`)
- ✅ Models bem definidos
- ✅ API REST padronizada

---

## ⚠️ Pontos de Atenção e Riscos

### **1. Configuração de Endpoints (RESOLVIDO)** ✅

**Status**: ✅ Alertmanager confirmado em todos os clusters

**Atualização necessária** em `clusters-config.json`:

```json
// clusters-config.json ATUAL (sem Alertmanager)
{
  "clusterName": "akspriv-faturamento-prd",
  "resourceGroup": "rg-faturamento-app-prd",
  "subscription": "PRD - BACKOFFICE"
  // ❌ FALTA: "alertmanager_url": "..."
}
```

**Solução requerida:**
```json
{
  "clusterName": "akspriv-faturamento-prd",
  "resourceGroup": "rg-faturamento-app-prd",
  "subscription": "PRD - BACKOFFICE",
  "alertmanager_url": "http://alertmanager-faturamento-prd.monitoring.svc:9093",
  "alertmanager_external_url": "https://alertmanager-faturamento-prd.viavarejo.com.br"
}
```

**Recomendação**: Implementar discovery similar ao Prometheus:

```go
func DiscoverAlertmanagerEndpoint(cluster string) (*AlertmanagerEndpoint, error) {
    name, env := parseClusterName(cluster)
    
    // Tentar interno primeiro (Service K8s)
    internalURL := fmt.Sprintf("http://alertmanager.monitoring.svc:9093")
    if validateEndpoint(internalURL) {
        return &AlertmanagerEndpoint{URL: internalURL}, nil
    }
    
    // Fallback para externo (Ingress)
    externalURL := fmt.Sprintf("https://alertmanager-%s-%s.viavarejo.com.br", name, env)
    return &AlertmanagerEndpoint{URL: externalURL}, validateEndpoint(externalURL)
}
```

---

### **2. Complexidade de Correlação** 🟡

**Desafio**: Mapear alertas → HPAs afetados

O documento propõe:
```go
correlations := make(map[string]*HPAAlert)
for _, alert := range alerts {
    namespace := alert.Labels["namespace"]
    hpaName := alert.Labels["hpa"]
    // Match com HPAs existentes
}
```

**Problema**: Nem todos os alertas têm label `hpa` diretamente.

| Alert | Labels Disponíveis | Correlação |
|-------|-------------------|------------|
| `HPAMaxedOut` | `namespace`, `horizontalpodautoscaler` | ✅ Direto |
| `PodCrashLooping` | `namespace`, `pod` | ⚠️ Requer lookup (pod → deployment → HPA) |
| `NodeMemoryPressure` | `node` | ❌ Não correlaciona diretamente com HPA |

**Recomendação**:
1. Implementar cache `pod → deployment → HPA` no MonitoringEngineV2
2. Usar heurísticas para alertas de infraestrutura (node, cluster-level)
3. Classificar alertas em:
   - ✅ **HPA-Direct**: Afetam HPA diretamente
   - ⚠️ **HPA-Indirect**: Afetam pods gerenciados por HPA
   - ❌ **HPA-Unrelated**: Infraestrutura geral

---

### **3. Volume de Alertas** 🟡

**Risco**: Overhead de polling e UI clutter

**Dados estimados** (cluster médio com 50 HPAs):
- Alertas ativos simultâneos: 10-30
- Taxa de mudança: 5-10 alertas/minuto
- Dados por alerta: ~500 bytes JSON

**Impacto**:
- Bandwidth: ~15KB/request a cada 30s = ✅ aceitável
- Latência: GET /api/v2/alerts ~100-300ms = ✅ aceitável
- UI: ⚠️ Risco de sobrecarga visual com muitos alertas

**Mitigação**:
```go
// Filtros essenciais
type AlertFilter struct {
    OnlyHPARelated bool   // Filtrar apenas HPA
    MinSeverity    string // critical, warning
    MaxAge         int    // Últimos X minutos
    Limit          int    // Máximo de alertas
}
```

---

### **4. Autenticação e RBAC** 🟡

**Incerteza**: O documento não menciona autenticação ao Alertmanager

**Cenários possíveis**:
1. **Alertmanager público** (HTTP básico): ✅ Fácil
2. **Alertmanager com mTLS**: ⚠️ Requer certificados
3. **Alertmanager com OAuth/OIDC**: 🔴 Complexo
4. **Service interno do K8s**: ✅ Sem auth se chamado dentro do cluster

**Recomendação**: Validar primeiro se `http://alertmanager.monitoring.svc:9093` está acessível **sem autenticação** do pod k8s-hpa-manager.

---

### **5. Mitigation Engine: Auto-Apply Seguro?** 🔴

O documento propõe ações automáticas:
```go
AutoApply: true  // 🚨 RISCO!
```

**Preocupações**:
- ✅ **Safe Actions**: Aumentar `maxReplicas` (reversível)
- ⚠️ **Risky Actions**: Restart pods, drain nodes
- 🔴 **Unsafe Actions**: Modificar limits/requests automaticamente

**Recomendação**:
```go
type SuggestedAction struct {
    Type        string
    AutoApply   bool
    SafetyLevel string // "safe", "risky", "unsafe"
    RequiresApproval bool
}

// Apenas safe actions podem ter AutoApply=true
if action.SafetyLevel == "safe" {
    action.AutoApply = true
} else {
    action.RequiresApproval = true
}
```

---

### **6. Frontend: Badges em HPATab** 🟡

O documento propõe badges de alerta em cada HPA:

---

### **7. Integração FluentD: Root Cause Analysis** 🟡 NOVO

**Oportunidade**: FluentD instalado em todos os clusters

**Potencial**:
```typescript
// Correlação Alert → Logs
Alert: "HPAMaxedOut" (critical)
  ↓
FluentD Query: Logs dos pods (últimos 15min)
  ↓
Resultado: "OutOfMemoryError: Java heap space" (50x)
  ↓
Recomendação: "Aumentar memory limits + investigar memory leak"
```

**Desafios**:
1. **Backend desconhecido**: Onde FluentD envia logs? (Elasticsearch, Loki, Splunk?)
2. **Formato variável**: Logs estruturados ou texto puro?
3. **Volume alto**: Queries podem ser lentas (> 5s)
4. **Parsing complexo**: Correlação pod → deployment → HPA

**Validação requerida**:
```bash
# Descobrir backend
kubectl get configmap -n kube-system fluentd-config -o yaml | grep -A 10 "output"

# Testar query (exemplo Elasticsearch)
curl "http://elasticsearch:9200/fluentd-*/_search?q=namespace:production&size=10"
```

**Recomendação**: Implementar em **Fase 2**, após sucesso da Fase 1 (Alertmanager).

O documento propõe badges de alerta em cada HPA:

```tsx
{alerts && alerts.count > 0 && (
    <Badge variant="destructive">{alerts.count} alertas</Badge>
)}
```

**Desafios**:
1. **Performance**: Necessário enriquecer **TODOS** os HPAs com alertas (N+1 problem)
2. **Latency**: Pode atrasar renderização inicial
3. **Complexity**: Aumenta payload da API `/api/v1/hpas`

**Solução**:
```typescript
// Backend: Endpoint separado para alertas por HPA
GET /api/v1/alerts/summary?cluster={cluster}
Response: {
  "akspriv-faturamento-prd": {
    "namespace-a/hpa-1": { count: 2, severity: "warning" },
    "namespace-b/hpa-2": { count: 1, severity: "critical" }
  }
}

// Frontend: Fetch apenas quando necessário
useEffect(() => {
  if (selectedCluster) {
    fetchAlertsSummary(selectedCluster);
  }
}, [selectedCluster]);
```

---

## 🎯 Plano de Implementação Revisado

### **Fase 0: Validação de Infraestrutura** (1-2 dias) ✅ CONFIRMADO

**Status**: ✅ Alertmanager instalado em TODOS os clusters  
**Status**: ✅ FluentD instalado em TODOS os clusters

**Validação técnica necessária**:

```bash
# 1. Testar acesso ao Alertmanager
for cluster in faturamento-prd logreversa-prd oferta-hlg; do
  echo "Testing alertmanager-$cluster..."
  kubectl --context=akspriv-$cluster-admin get svc -n monitoring | grep alertmanager
  kubectl --context=akspriv-$cluster-admin port-forward -n monitoring svc/alertmanager 9093:9093 &
  sleep 2
  curl -s http://localhost:9093/api/v2/alerts | jq '.[] | {labels, state, annotations}' | head -20
  pkill -f "port-forward.*alertmanager"
done

# 2. Descobrir backend do FluentD (para Fase 2)
kubectl get configmap -n kube-system fluentd-config -o yaml | grep -A 20 "output"

# 3. Validar formato de logs (se Elasticsearch)
curl "http://elasticsearch.logging.svc:9200/_cat/indices?v" | grep fluentd
curl "http://elasticsearch.logging.svc:9200/fluentd-*/_search?size=5" | jq
```

**Decisão**: ✅ PROSSEGUIR com implementação imediata do Alertmanager

---

### **Fase 1: Backend Foundation** (3-5 dias)
✅ **Prioridade ALTA**

**Tarefas:**
1. Criar `internal/alertmanager/client.go` (similar ao Prometheus)
2. Implementar discovery: `DiscoverAlertmanagerEndpoint()`
3. Adicionar campo `alertmanager_url` em `clusters-config.json`
4. Testes de conectividade

**Critério de sucesso**:
```bash
# Teste manual
curl http://alertmanager-faturamento-prd.monitoring.svc:9093/api/v2/alerts
# OU
curl https://alertmanager-faturamento-prd.viavarejo.com.br/api/v2/alerts
```

**Arquivos a criar:**
- `internal/alertmanager/client.go`
- `internal/alertmanager/discovery.go`
- `internal/alertmanager/types.go`

---

### **Fase 2: Alert Correlation** (2-3 dias)
🟡 **Prioridade MÉDIA**

**Tarefas:**
1. Implementar correlator com cache `pod → HPA`
2. Classificar alertas (HPA-Direct, HPA-Indirect, HPA-Unrelated)
3. Filtros por severidade e tipo

**Simplificação**: Focar **apenas** em alertas com labels:
- `horizontalpodautoscaler`
- `namespace`
- `cluster`

**Arquivos a criar:**
- `internal/alertmanager/correlator.go`

---

### **Fase 3: History Integration** (1 dia)
✅ **Prioridade ALTA**

**Modificações:**
```go
// internal/history/tracker.go
const (
    ActionAlertDetected     = "alert_detected"
    ActionAlertResolved     = "alert_resolved"
    ActionMitigationApplied = "mitigation_applied"
)
```

**Arquivos a modificar:**
- `internal/history/tracker.go`

---

### **Fase 4: REST API** (2 dias)
✅ **Prioridade ALTA**

**Endpoints:**
```go
// internal/web/server.go
alertHandler := handlers.NewAlertHandler(s.kubeManager)
api.GET("/alerts", alertHandler.List)                    // Lista completa
api.GET("/alerts/summary", alertHandler.GetSummary)      // Agregado por HPA
api.GET("/alerts/stats", alertHandler.GetStats)          // Estatísticas
api.GET("/alerts/:cluster/:id", alertHandler.Get)        // Alerta específico
```

**Arquivos a criar:**
- `internal/web/handlers/alerts.go`

**Arquivos a modificar:**
- `internal/web/server.go`

---

### **Fase 5: Frontend Básico** (3 dias)
🟡 **Prioridade MÉDIA**

**Componentes:**
1. Endpoint `/alerts` (lista completa)
2. Badge de contagem em `HPATab`
3. Detalhes no `HPAEditor` (read-only)

**Arquivos a modificar:**
- `internal/web/frontend/src/components/HPATab.tsx`
- `internal/web/frontend/src/components/HPAEditor.tsx`
- `internal/web/frontend/src/lib/api/client.ts`
- `internal/web/frontend/src/lib/api/types.ts`

**NÃO implementar ainda**:
- ❌ Mitigation Engine (Fase 6)
- ❌ Auto-apply actions (Fase 6)
- ❌ Webhook receiver (Fase 7)

---

### **Fase 6: Mitigation Engine** (5-7 dias)
⚠️ **Prioridade BAIXA** (implementar apenas se Fases 1-5 forem bem-sucedidas)

**Tarefas:**
1. Rules engine para sugestões
2. Safety levels (safe, risky, unsafe)
3. Approval workflow

**Arquivos a criar:**
- `internal/alertmanager/mitigation.go`
- `internal/alertmanager/rules.go`

---

### **Fase 7: Webhooks** (3-4 dias)
🟢 **Prioridade BAIXA** (opcional)

Apenas se houver demanda por notificações em tempo real (vs polling 30s).

**Arquivos a criar:**
- `internal/web/handlers/webhooks.go`

---

### **Fase 8: Integração FluentD** (10-15 dias)
⚠️ **CONDICIONAL** - Depende de validação do backend

**Pré-requisitos CRÍTICOS**:
1. ✅ Fase 1-5 (Alertmanager) funcionando
2. ⚠️ Backend de logs identificado e acessível
3. ⚠️ Formato de logs validado
4. ⚠️ Performance de queries aceitável (< 3s)

**Tarefas:**

1. **Descobrir Backend** (1 dia)
```bash
kubectl get configmap -n kube-system fluentd-* -o yaml
# Identificar: Elasticsearch, Loki, Splunk, CloudWatch?
```

2. **Implementar FluentD Client** (3-4 dias)
```go
// internal/fluentd/client.go
type Client struct {
    backend string // "elasticsearch", "loki", etc
    endpoint string
}

func (c *Client) QueryLogs(namespace, podName string, since time.Duration) ([]LogEntry, error)
```

3. **Correlação Pod → HPA** (2-3 dias)
```go
// internal/fluentd/correlator.go
func (c *Correlator) GetLogsForHPA(cluster, namespace, hpaName string) ([]LogEntry, error) {
    // 1. Obter pods do HPA target (deployment/statefulset)
    // 2. Query logs dos pods
    // 3. Filtrar por severidade (ERROR, WARN)
    // 4. Parsear e estruturar
}
```

4. **API REST** (2 dias)
```go
// internal/web/handlers/logs.go
api.GET("/logs/hpa/:cluster/:namespace/:name", logsHandler.GetHPALogs)
```

5. **Frontend** (3-4 dias)
```tsx
// HPAEditor: Botão "View Logs"
<Button onClick={() => fetchLogs(cluster, namespace, hpaName)}>
  <FileText className="h-4 w-4 mr-2" />
  View Logs (Last 15min)
</Button>

// Modal com logs filtrados
<LogsModal logs={logs} severity={["ERROR", "WARN"]} />
```

**Arquivos a criar:**
- `internal/fluentd/client.go`
- `internal/fluentd/correlator.go`
- `internal/fluentd/backends/elasticsearch.go`
- `internal/fluentd/backends/loki.go`
- `internal/web/handlers/logs.go`
- `internal/web/frontend/src/components/LogsModal.tsx`

**Benefícios:**
- 🔍 Root cause analysis direto na interface
- 📚 Contexto adicional para troubleshooting
- ⚡ Reduz tempo de MTTR (Mean Time To Resolution)
- 🎯 Correlação Alert → Logs → Ação

**Riscos:**
- ⚠️ Performance (queries lentas)
- ⚠️ Parsing complexo (logs não estruturados)
- ⚠️ Volume alto (milhões de logs)
- ⚠️ Backend pode variar por cluster

---

## 📊 Comparação: Alertmanager vs FluentD

| Critério | Alertmanager | FluentD |
|----------|--------------|---------|
| **Status** | ✅ Instalado (todos) | ✅ Instalado (todos) |
| **Latência** | ⚡ Tempo real (segundos) | 🐢 Minutos (indexação) |
| **Custo de Query** | 💰 Baixo (HTTP GET) | 💰💰 Alto (full-text search) |
| **Contexto** | 🎯 Métricas + regras | 📚 Logs detalhados |
| **Estrutura** | ✅ Padronizado (labels) | ⚠️ Variável (depende da app) |
| **Volume de Dados** | ✅ Pequeno (KB/req) | ⚠️ Grande (MB/req) |
| **Correlação HPA** | ✅ Direto (labels) | ⚠️ Requer parsing |
| **Proatividade** | ✅ Detecta antes de falhar | ❌ Apenas post-mortem |
| **Facilidade** | ✅ API REST simples | ⚠️ Depende do backend |
| **Prioridade** | 🔴 IMEDIATA | 🟡 FASE 2 |
| **ROI** | ⭐⭐⭐⭐⭐ Muito alto | ⭐⭐⭐ Médio |

---

## 📊 Avaliação Final

### **Alertmanager (Fase 1)**

| Critério | Score | Observação |
|----------|-------|------------|
| **Viabilidade Técnica** | 10/10 | ✅ Confirmado em todos os clusters |
| **Complexidade** | 5/10 | API REST simples, labels padronizados |
| **Risco** | 2/10 | ✅ Infraestrutura confirmada |
| **Benefício** | 10/10 | Detecção proativa crítica |
| **Esforço MVP** | 8-10 dias | Fases 0-5 |
| **ROI** | ⭐⭐⭐⭐⭐ | **Muito alto** |

### **FluentD (Fase 2)**

| Critério | Score | Observação |
|----------|-------|------------|
| **Viabilidade Técnica** | 7/10 | ✅ Instalado, ⚠️ backend desconhecido |
| **Complexidade** | 8/10 | Parsing logs, correlação pod→HPA |
| **Risco** | 6/10 | Performance, formato variável |
| **Benefício** | 8/10 | Root cause analysis valioso |
| **Esforço** | 10-15 dias | Após validação do backend |
| **ROI** | ⭐⭐⭐ | **Médio** (depende da qualidade dos logs) |

### **Total (Fases 1+2)**

| Critério | Score | Observação |
|----------|-------|------------|
| **Esforço Total** | 20-25 dias | Alertmanager + FluentD completo |
| **Viabilidade Geral** | 95% | ✅ Alertmanager imediato, ⚠️ FluentD condicional |

---

## ✅ Recomendação Final

**SIM, IMPLEMENTAR COM PRIORIZAÇÃO**, abordagem em camadas:

### **Camada 1: Alertmanager (MVP)** 🔴 PRIORIDADE IMEDIATA

**Razão**: 
- ✅ Infraestrutura confirmada (instalado em todos os clusters)
- ✅ API padronizada (Alertmanager API v2)
- ✅ Dados estruturados (labels, annotations)
- ✅ Latência baixa (tempo real)
- ✅ Alto impacto (detecção proativa de problemas)

**Escopo MVP:**
1. ✅ Alertmanager Client (Fase 1)
2. ✅ Endpoint `/alerts` + `/alerts/summary` (Fase 4)
3. ✅ Badge de alertas em HPAs (Fase 5)
4. ✅ History Tracker integration (Fase 3)
5. ✅ Painel de alertas detalhado (Fase 5)

**Esforço**: 8-10 dias  
**ROI**: ⭐⭐⭐⭐⭐ Muito alto

---

### **Camada 2: FluentD (CONDICIONAL)** 🟡 IMPLEMENTAR DEPOIS

**Razão**:
- ✅ Infraestrutura confirmada (instalado em todos os clusters)
- ⚠️ Backend desconhecido (requer validação)
- ⚠️ Parsing complexo (logs não estruturados)
- ✅ Contexto adicional valioso (root cause analysis)
- ✅ Complementa Alertmanager perfeitamente

**Escopo Fase 2:**
1. ⚠️ Descobrir backend de logs (Elasticsearch, Loki?)
2. ⚠️ Validar formato e performance
3. ⚠️ FluentD Client + Correlator (Fase 8)
4. ⚠️ Endpoint `/logs/hpa/:namespace/:name`
5. ⚠️ Botão "View Logs" no HPA Editor

**Esforço**: 10-15 dias (após validação)  
**ROI**: ⭐⭐⭐ Médio (depende da qualidade dos logs)

**Implementar SE**:
- ✅ Alertmanager (Camada 1) estiver funcionando
- ✅ Backend de logs for consultável (Elasticsearch, Loki)
- ✅ Logs forem estruturados ou parseáveis
- ✅ Latência de query for aceitável (< 3s)

---

### **Deixar para Fase 3 (Futuro):**
- ❌ Mitigation Engine (complexo, requer validação de segurança)
- ❌ Auto-apply actions (risco alto)
- ❌ Webhooks (opcional, polling 30s é suficiente)

---

### **Pré-requisitos: Status Atualizado**

#### **Alertmanager** ✅
1. ✅ ~~Validar acesso ao Alertmanager~~ → **CONFIRMADO em todos os clusters**
2. ✅ ~~Definir se todos os clusters têm Alertmanager~~ → **CONFIRMADO**
3. 🟡 Validar formato de labels nos alerts existentes
4. 🟡 Verificar necessidade de autenticação
5. 🟡 Testar latência de queries (objetivo: < 500ms)

#### **FluentD** ⚠️
1. 🔴 **Descobrir backend de logs** (Elasticsearch, Loki, Splunk?)
2. 🔴 **Validar acesso ao backend** (network, autenticação)
3. 🟡 Analisar formato e estrutura dos logs
4. 🟡 Testar performance de queries (objetivo: < 3s)
5. 🟡 Validar correlação pod → deployment → HPA

**Checklist de validação:**
```bash
# Para cada cluster em clusters-config.json
for cluster in $(jq -r '.[].clusterName' clusters-config.json); do
    echo "Testing $cluster..."
    
    # Método 1: Via port-forward
    kubectl --context=${cluster}-admin port-forward -n monitoring svc/alertmanager 9093:9093 &
    sleep 2
    curl -s http://localhost:9093/api/v2/alerts | jq '.[] | .labels'
    pkill -f "port-forward.*alertmanager"
    
    # Método 2: Via ingress (se existir)
    # curl -s https://alertmanager-${cluster}.viavarejo.com.br/api/v2/alerts
done
```

---

### **Riscos Mitigados**

- ✅ Discovery automático (similar ao Prometheus)
- ✅ Cache de correlação (performance)
- ✅ Filtros agressivos (reduzir noise)
- ✅ Approval obrigatório para ações críticas
- ✅ Implementação em fases (MVP primeiro)

---

## 📝 Próximos Passos Sugeridos

### **Semana 1-2: Validação + MVP Alertmanager** 🔴

**Objetivos:**
1. ✅ Validar acesso ao Alertmanager em 3-5 clusters piloto
2. ✅ Analisar estrutura de alertas (labels, annotations, severidade)
3. ✅ Implementar Fase 0-1 (Alertmanager Client + Discovery)
4. ✅ Atualizar `clusters-config.json` com `alertmanager_url`

**Scripts de validação:**
```bash
#!/bin/bash
# validate-alertmanager.sh

CLUSTERS=(
  "akspriv-faturamento-prd"
  "akspriv-logreversa-prd"
  "akspriv-oferta-hlg"
  "akspriv-tracking-prd"
  "akspriv-tms-hlg"
)

for cluster in "${CLUSTERS[@]}"; do
  echo "=== Testing $cluster ==="
  
  # Port-forward
  kubectl --context=${cluster}-admin port-forward -n monitoring svc/alertmanager 9093:9093 &
  PF_PID=$!
  sleep 3
  
  # Test API
  echo "1. API Health:"
  curl -s http://localhost:9093/api/v2/status | jq
  
  echo "2. Active Alerts (sample):"
  curl -s http://localhost:9093/api/v2/alerts | jq '[.[] | {labels, state, severity: .labels.severity}] | .[0:3]'
  
  echo "3. Alert Labels:"
  curl -s http://localhost:9093/api/v2/alerts | jq '[.[].labels | keys] | add | unique'
  
  # Cleanup
  kill $PF_PID
  echo ""
done
```

**Entregáveis:**
- ✅ `internal/alertmanager/client.go`
- ✅ `internal/alertmanager/discovery.go`
- ✅ Testes unitários
- ✅ Documentação de labels/annotations

---

### **Semana 3-4: API + Frontend Alertmanager** 🔴

**Objetivos:**
1. ✅ Implementar Fases 2-5 (Correlator + API + Frontend)
2. ✅ Endpoint `/api/v1/alerts` (lista, summary, stats)
3. ✅ Badge de alertas em `HPATab`
4. ✅ Painel detalhado em `HPAEditor`
5. ✅ History Tracker integration

**Entregáveis:**
- ✅ `internal/alertmanager/correlator.go`
- ✅ `internal/web/handlers/alerts.go`
- ✅ Frontend components (badges, panels)
- ✅ Testes de integração
- ✅ Deploy em staging

---

### **Semana 5: Validação FluentD** 🟡

**Objetivos:**
1. ⚠️ Descobrir backend de logs (Elasticsearch, Loki, Splunk?)
2. ⚠️ Validar acesso e autenticação
3. ⚠️ Analisar formato de logs (estruturado, JSON, texto puro?)
4. ⚠️ Testar performance de queries
5. ⚠️ **Decisão Go/No-Go para Fase 2**

**Scripts de validação:**
```bash
#!/bin/bash
# validate-fluentd.sh

# 1. Descobrir configuração
kubectl get configmap -n kube-system fluentd-config -o yaml > fluentd-config.yaml
cat fluentd-config.yaml | grep -A 30 "output"

# 2. Testar backend (exemplo Elasticsearch)
kubectl get svc -n logging | grep elastic
kubectl port-forward -n logging svc/elasticsearch 9200:9200 &
sleep 2

echo "Elasticsearch indices:"
curl -s http://localhost:9200/_cat/indices?v | grep fluentd

echo "Sample logs:"
curl -s "http://localhost:9200/fluentd-*/_search?size=5" | jq

echo "Search by namespace:"
curl -s "http://localhost:9200/fluentd-*/_search" -H 'Content-Type: application/json' -d'
{
  "size": 5,
  "query": {
    "bool": {
      "must": [
        {"match": {"kubernetes.namespace_name": "production"}},
        {"range": {"@timestamp": {"gte": "now-1h"}}}
      ]
    }
  }
}' | jq
```

**Decisão:**
- ✅ Se viável → Semana 6-8: Implementar FluentD integration
- ❌ Se não viável → Documentar limitações e focar em Alertmanager

---

### **Semana 6-8: FluentD Integration (CONDICIONAL)** 🟡

**SE validação for positiva:**
1. ⚠️ Implementar FluentD Client (backend-specific)
2. ⚠️ Correlator pod → deployment → HPA
3. ⚠️ Endpoint `/api/v1/logs/hpa/:namespace/:name`
4. ⚠️ Frontend: Botão "View Logs" + Modal
5. ⚠️ Integração com Alertas (Alert → Logs)

---

### **Semana 9-10: Deploy Produção** ✅

**Objetivos:**
1. ✅ Testes de carga e stress
2. ✅ Documentação completa (usuário + técnica)
3. ✅ Rollout gradual (3 clusters → 10 clusters → todos)
4. ✅ Monitoramento de métricas de uso
5. ✅ Coleta de feedback

---

### **Longo Prazo (Mês 2-3)** ⚠️

**Baseado em feedback:**
1. ⚠️ Mitigation Engine (se demanda confirmada)
2. ⚠️ Auto-apply actions (com aprovações obrigatórias)
3. ⚠️ Webhooks (se polling não for suficiente)
4. ⚠️ Machine Learning para detecção de padrões
5. ⚠️ Integração com Slack/Teams para notificações

---

## 📚 Referências

- **Documento Base**: [ALERTMANAGER_HPA_INTEGRATION.md](docs/studies/ALERTMANAGER_HPA_INTEGRATION.md)
- **Prometheus Client**: `internal/monitoring/prometheus/client.go`
- **Discovery System**: `internal/monitoring/discovery/prometheus.go`
- **History Tracker**: `internal/history/tracker.go`
- **Alertmanager API**: https://prometheus.io/docs/alerting/latest/clients/

---

## 🏗️ Arquitetura Proposta: Alertmanager + FluentD (Visão Completa)

```
┌─────────────────────────────────────────────────────────────────┐
│                     Frontend (React)                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌───────────────────┐      ┌─────────────────────┐            │
│  │   HPATab          │      │   AlertsPanel       │            │
│  │   - HPA List      │◄────►│   - Active Alerts   │            │
│  │   - Badge: 3 🔴   │      │   - By Severity     │            │
│  └─────────┬─────────┘      └─────────────────────┘            │
│            │                          │                          │
│            ▼                          │                          │
│  ┌─────────────────────────────────────────────┐               │
│  │   HPAEditor (Details View)                  │               │
│  │   ┌─────────────────────────────────────┐   │               │
│  │   │ 🚨 Active Alerts (3)                │   │               │
│  │   │ - HPAMaxedOut (critical)            │   │               │
│  │   │ - HighCPUUsage (warning)            │   │               │
│  │   │ - MemoryPressure (warning)          │   │               │
│  │   │   └─ [View Logs] ◄───────────────┐  │   │ (Fase 2)     │
│  │   └─────────────────────────────────────┘   │               │
│  │   ┌─────────────────────────────────────┐   │               │
│  │   │ 📋 Suggested Actions                │   │               │
│  │   │ ✅ Increase maxReplicas to 15       │   │               │
│  │   │ ⚠️  Investigate memory leak         │   │               │
│  │   └─────────────────────────────────────┘   │               │
│  └─────────────────────────────────────────────┘               │
│                          │                                       │
└──────────────────────────┼───────────────────────────────────────┘
                           │
┌──────────────────────────┼───────────────────────────────────────┐
│                   Backend (Go)                                    │
├──────────────────────────┼───────────────────────────────────────┤
│                          │                                        │
│  ┌────────────────┐     │      ┌─────────────────────┐          │
│  │ Alertmanager   │     │      │ FluentD Query       │          │
│  │ Client         │     │      │ Engine (Fase 2)     │          │
│  │ - GetAlerts()  │     │      │ - QueryLogs()       │          │
│  │ - GetStats()   │     │      │ - FilterByPod()     │          │
│  │ - Correlate()  │     │      │ - ParseErrors()     │          │
│  └────────┬───────┘     │      └──────────┬──────────┘          │
│           │             │                 │                      │
│           └─────────────┼─────────────────┘                      │
│                         ▼                                        │
│  ┌──────────────────────────────────────────────────┐           │
│  │  Alert-HPA Correlator                            │           │
│  │  1. Match alerts to HPAs (by labels)             │           │
│  │  2. Enrich with FluentD logs (optional, Fase 2)  │           │
│  │  3. Generate recommendations                     │           │
│  │  4. Calculate severity & priority                │           │
│  └─────────────────────┬────────────────────────────┘           │
│                        │                                         │
│                        ▼                                         │
│  ┌──────────────────────────────────────────────────┐           │
│  │  History Tracker                                 │           │
│  │  - ActionAlertDetected                           │           │
│  │  - ActionAlertResolved                           │           │
│  │  - ActionLogsQueried (Fase 2)                    │           │
│  │  - ActionMitigationApplied                       │           │
│  └──────────────────────────────────────────────────┘           │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
                │                           │
     ┌──────────┴─────────┐      ┌─────────┴────────────┐
     ▼                     ▼      ▼                      ▼
┌──────────────┐  ┌────────────────────┐  ┌──────────────────┐
│Alertmanager  │  │ FluentD Backend    │  │  Prometheus      │
│ (K8s Service)│  │ (Elasticsearch?    │  │  (Metrics)       │
│ :9093        │  │  Loki? Splunk?)    │  │                  │
└──────────────┘  └────────────────────┘  └──────────────────┘
```

**Fluxo de Dados:**

1. **Alertmanager → Backend** (Polling 30s)
   - GET `/api/v2/alerts`
   - Filtrar por cluster/namespace
   - Enriquecer com metadata

2. **Correlator** (Real-time)
   - Match `alert.labels.horizontalpodautoscaler` → HPA
   - Calcular impacto (quantos HPAs afetados)
   - Gerar sugestões (rules engine)

3. **FluentD → Backend** (On-demand, Fase 2)
   - Query logs quando usuário clica "View Logs"
   - Filtrar por pod/namespace/time range
   - Parsear erros/warnings relevantes

4. **History Tracker** (Async)
   - Registrar detecção de alerta
   - Registrar resolução
   - Timeline de eventos

5. **Frontend** (Real-time via polling)
   - Badge de alertas atualiza a cada 30s
   - Painel de alertas auto-refresh
   - Logs on-demand (não realtime)

---

## 🔄 Histórico de Revisões

| Versão | Data | Autor | Mudanças |
|--------|------|-------|----------|
| 1.0 | 19/11/2025 | Claude AI | Análise inicial de viabilidade |
| 2.0 | 19/11/2025 | Claude AI | ✅ Confirmação Alertmanager + FluentD instalados<br>✅ Adicionada análise FluentD (Fase 2)<br>✅ Comparação Alertmanager vs FluentD<br>✅ Arquitetura completa em camadas<br>✅ Scripts de validação detalhados<br>✅ Roadmap atualizado (10 semanas) |

---

## 📚 Referências Atualizadas

- **Documento Base**: [ALERTMANAGER_HPA_INTEGRATION.md](docs/studies/ALERTMANAGER_HPA_INTEGRATION.md)
- **Prometheus Client**: `internal/monitoring/prometheus/client.go`
- **Discovery System**: `internal/monitoring/discovery/prometheus.go`
- **History Tracker**: `internal/history/tracker.go`
- **Alertmanager API v2**: https://prometheus.io/docs/alerting/latest/clients/
- **FluentD Output Plugins**: https://docs.fluentd.org/output
- **Elasticsearch Query DSL**: https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl.html
- **Loki LogQL**: https://grafana.com/docs/loki/latest/logql/

---

**Autor**: Claude AI  
**Revisão**: Pendente  
**Aprovação**: Pendente  
**Última atualização**: 19 de novembro de 2025  
**Status**: ✅ Pronto para implementação (Fase 1 - Alertmanager)
