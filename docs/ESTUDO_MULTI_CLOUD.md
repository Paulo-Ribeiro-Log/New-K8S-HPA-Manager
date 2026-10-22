# Estudo: Expansão Multi-Cloud (AKS + EKS)

> Elaborado em 20/03/2026 — Branch `integracao-dyna`

---

## Contexto

O projeto hoje é **~85% genérico Kubernetes** e **~15% específico de Azure/AKS**.
A expansão para AWS EKS requer:
- Suporte a New Relic (em vez de Dynatrace)
- Gerenciamento de 2 VPNs distintas (Azure VPN ↔ AWS VPN)
- Abstração de Node Pool para Auto Scaling Groups (EKS)
- Abstração de RBAC (Azure AD → IAM)

---

## 1. O que já funciona com EKS sem mudanças (85%)

| Componente | Status |
|---|---|
| HPA management (Deployment, StatefulSet, scaling) | ✅ portável |
| Deployments / StatefulSets / DaemonSets / CronJobs CRUD | ✅ portável |
| ConfigMaps, Secrets, Services, Ingresses, PersistentVolumes | ✅ portável |
| Health Check (K8s status, events, HPAs, PVCs) | ✅ portável |
| AI Diagnostics (Ollama, Claude, Gemini, OpenAI, Copilot) | ✅ portável |
| Resource Explorer (via Discovery API — qualquer CRD) | ✅ portável |
| Command Runner, WebSocket terminal | ✅ portável |
| Monitoring via Prometheus | ✅ portável |
| Audit trail, SSE, UI React | ✅ portável |

---

## 2. O que NÃO funciona com EKS (15% Azure-only)

### 2.1 Node Pool Management
- Todo via `az aks nodepool ...` — `exec.Command("az")` em ~15 lugares
- EKS usa **Auto Scaling Groups (ASG)** via AWS CLI/SDK
- Lógica de enable/disable autoscaler antes de escalar é Azure-específica
- Azure rejeita `az aks nodepool scale` se autoscaling habilitado → orquestração especial

### 2.2 Azure AD RBAC
- `internal/rbac/azure_ad.go` chama `az ad user get-member-groups`
- Grupo SRE hardcoded: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
- EKS usa IAM + aws-auth ConfigMap ou EKS Access Entries

### 2.3 Azure VM Specs
- `internal/monitoring/predictions/azure_vm_specs.go` — ~150 VM sizes Azure hardcoded
- EKS precisa de EC2 instance types (m5.xlarge, c5.2xlarge, etc.)
- Estrutura reutilizável, conteúdo precisa de equivalente AWS

### 2.4 Quirks de kubeconfig AKS
- Sufixo `-admin` nos context names (`akspriv-xxx-admin`)
- Regex `aks-<nodepool>-XXXXXXXX-vmssXXXX` para parsear node names no OneAgent scanner
- Discovery fixo em `akspriv-*` — clusters EKS nunca seriam descobertos

---

## 3. Observabilidade: Dynatrace (AKS) → New Relic (EKS)

### 3.1 Mapeamento de funcionalidades

| Funcionalidade atual (DT) | Equivalente New Relic | API NR |
|---|---|---|
| `GetOpenProblems` | Issues/Alerts abertos | NerdGraph `aiIssues` |
| `ListEntitiesByCluster` | Entity search por cluster | NerdGraph `entitySearch` com `tags.clusterName` |
| `SearchEntitiesByName` | Entity search por nome | NerdGraph `entitySearch` com `name LIKE` |
| `BatchQueryMetrics` (error rate, P90, restarts) | NRQL queries | NerdGraph `nrql` |
| `EnrichEntitiesWithK8s` | Entity tags K8s | NerdGraph `entity.tags` |
| `GetProblemContext` (Davis AI) | Anomaly detection | NerdGraph `aiIssues` com `incidentEntities` |
| OneAgent threshold scan | NR Agent K8s samples | NRQL `K8sDeploymentSample`, `K8sPodSample` |

### 3.2 Vantagens do New Relic para EKS

- **NerdGraph (GraphQL)**: endpoint único `https://api.newrelic.com/graphql`
- **Entity types K8s nativos**: `KUBERNETES_CLUSTER`, `KUBERNETES_DEPLOYMENT`, `KUBERNETES_POD`, `KUBERNETES_CONTAINER`, `KUBERNETES_NODE`
- Sem necessidade de correlacionar via `host_group` — `clusterName` vem direto no atributo
- **NRQL** para métricas (parecido com SQL):

```sql
SELECT
  filter(count(*), WHERE error IS true) / count(*) * 100 AS error_rate,
  percentile(duration, 90) AS p90_ms,
  sum(restartCount) AS pod_restarts
FROM K8sContainerSample, Transaction
WHERE clusterName = 'meu-cluster-eks'
AND namespaceName = 'meu-namespace'
SINCE 60 minutes ago
```

```graphql
{
  actor {
    entitySearch(query: "type = 'KUBERNETES_DEPLOYMENT' AND tags.clusterName = 'meu-cluster-eks'") {
      results {
        entities {
          guid
          name
          tags { key values }
        }
      }
    }
  }
}
```

### 3.3 Abstração proposta: `ObservabilityProvider`

```go
// internal/observability/provider.go
type ObservabilityProvider interface {
    GetOpenIssues(ctx context.Context, filter IssueFilter) ([]Issue, error)
    ListClusterEntities(ctx context.Context, cluster string) ([]Entity, error)
    SearchEntitiesByName(ctx context.Context, names []string) ([]Entity, error)
    GetEntityMetrics(ctx context.Context, entityIDs []string, windowMin int) (map[string]Metrics, error)
}

// internal/observability/dynatrace/provider.go  ← move o código atual de internal/dynatrace/
// internal/observability/newrelic/provider.go   ← novo
```

O `HealthCheckResultsPanel.tsx` **não muda** — dados chegam no mesmo formato normalizado.

---

## 4. VPN Switching (2 VPNs distintas)

### 4.1 Problema atual

- Discovery fixo em `akspriv-*` — clusters EKS nunca descobertos
- `GET /api/v1/vpn/status` verifica `kubectl cluster-info` sem saber *qual* cloud
- Cache de clients K8s (TTL 30min) — ao trocar VPN, clients do provider anterior ficam como "zumbis" até expirar
- Health Check tenta alcançar clusters inacessíveis → timeout desnecessário

### 4.2 Configuração de providers

```yaml
# ~/.kube/hpa-manager-providers.yaml
providers:
  azure:
    pattern: "akspriv-*"
    vpn_probe: "https://management.azure.com"    # HEAD em 2s
    observability: dynatrace

  aws:
    pattern: "eks-*"                              # naming dos clusters EKS
    vpn_probe: "https://eks.us-east-1.amazonaws.com"
    observability: newrelic
```

### 4.3 VPN Detection por provider

```go
// internal/cloud/vpn_detector.go
type VPNStatus struct {
    Provider string        // "azure" | "aws"
    Active   bool
    Latency  time.Duration
}

// HEAD request com timeout 2s — se conecta, VPN está ativa
func ProbeVPN(ctx context.Context, endpoint string) bool { ... }
```

### 4.4 Invalidação de cache ao trocar VPN

```go
func (k *KubeConfigManager) InvalidateProviderClients(provider string) {
    k.clientMutex.Lock()
    defer k.clientMutex.Unlock()
    for name := range k.clientCache {
        if k.clusterProvider[name] == provider {
            delete(k.clientCache, name)
        }
    }
}
```

### 4.5 UI proposta

```
┌─ Clusters ──────────────────────────────────────┐
│                                                  │
│  ☁️ Azure (VPN ativa)            ● conectado     │
│  ├── akspriv-busca-prd           ✓               │
│  ├── akspriv-adanalytics-prd     ✓               │
│  └── akspriv-logistica-hlg       ✓               │
│                                                  │
│  ☁️ AWS (VPN inativa)            ○ offline       │
│  ├── eks-payments-prd            ✗ inacessível   │
│  └── eks-checkout-prd            ✗ inacessível   │
│                                                  │
└──────────────────────────────────────────────────┘
```

Health Check pula clusters `vpn_offline` com mensagem clara em vez de timeout.

---

## 5. Abstração de Node Pool: AKS → EKS

### 5.1 Interface proposta

```go
// internal/cloud/provider.go
type CloudProvider interface {
    ListNodePools(ctx context.Context, cluster string) ([]NodePool, error)
    ScaleNodePool(ctx context.Context, cluster, pool string, count int, opts ScaleOptions) error
    GetInstanceSpecs(instanceType string) (*InstanceSpecs, error)
    ParseNodeName(nodeName string) (poolName string, ok bool)
    ValidateAuth(ctx context.Context) error
}

type AKSProvider struct{} // az aks nodepool ...
type EKSProvider struct{} // aws eks describe-nodegroup + ASG API
```

### 5.2 Diferenças AKS vs EKS

| Aspecto | AKS | EKS |
|---|---|---|
| Conceito | Node Pool | Node Group (ASG) |
| CLI | `az aks nodepool scale` | `aws eks update-nodegroup-config` |
| Autoscaling | enable/disable explícito antes de scale | configurado no ASG diretamente |
| Node name pattern | `aks-<pool>-XXXXXXXX-vmssXXXX` | `ip-10-0-1-23.us-east-1.compute.internal` |
| Pool tag no node | Parte do nome | Label `eks.amazonaws.com/nodegroup` |
| VM/Instance specs | Azure VM sizes (Standard_D*) | EC2 instance types (m5.*, c5.*, etc.) |

### 5.3 RBAC

| Aspecto | AKS (atual) | EKS (proposto) |
|---|---|---|
| Auth provider | Azure AD (`az ad user get-member-groups`) | IAM + aws-auth ConfigMap |
| Grupo SRE | `VV_CLOUD_SRE` (Azure AD group) | IAM group ou K8s RBAC group |
| Token refresh | `az account get-access-token` | `aws eks get-token` |

---

## 6. Resumo de esforço por componente

| Componente | AKS+DT (atual) | EKS+NR (novo) | Reuso |
|---|---|---|---|
| HPA/Deploy/Health Check | ✅ pronto | ✅ sem mudança | 100% |
| Observability client | `internal/dynatrace/` | `internal/newrelic/` | interface comum |
| Node pool management | `az aks nodepool` | `aws eks` + ASG | 0% — reescrever |
| RBAC | Azure AD | IAM/aws-auth | 0% — reescrever |
| VM/Instance specs | Azure VM sizes | EC2 instance types | estrutura reutilizável |
| Discovery de clusters | `akspriv-*` hardcoded | config por provider | refatorar |
| VPN detection | único check genérico | probe por provider | refatorar |
| Cache de clients K8s | TTL 30min global | invalidação por provider | adicionar |
| UI React | 100% genérica | 100% genérica | 100% |

---

## 7. Ordem de implementação sugerida

1. **Config de providers** (`hpa-manager-providers.yaml` + loader) — desbloqueia todo o resto
2. **VPN detection por provider** — UI já mostra status correto
3. **Discovery multi-provider** — clusters EKS aparecem na lista
4. **New Relic client** (`internal/observability/newrelic/`) — paridade com DT
5. **Abstração `ObservabilityProvider`** — orquestrador usa interface, não DT direto
6. **EKS Node Group management** — `EKSProvider` implementando `CloudProvider`
7. **IAM RBAC** — substituir Azure AD por provider de auth configurável
8. **EC2 instance specs** — completar predições para EKS
