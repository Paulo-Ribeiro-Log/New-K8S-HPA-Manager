# Plano: Multi-Cloud (AKS + EKS) com Sugestão de VPN

Criado em: 2026-03-30

---

## Objetivo

Adicionar suporte a clusters **AWS EKS** na aplicação, detectando automaticamente o cloud provider de cada cluster e **sugerindo ao usuário a troca de VPN** quando necessário — sem gerenciar, configurar ou executar nenhuma conexão VPN pela aplicação.

---

## Premissas

- A aplicação **não conecta nem desconecta** VPN
- A aplicação **detecta** qual cloud o cluster pertence e **avisa** o usuário
- O usuário troca a VPN manualmente (cliente VPN da empresa)
- Zero configuração de VPN dentro da aplicação
- Kubeconfig EKS já configurado pelo usuário via `aws eks update-kubeconfig --profile X`

---

## Como Detectar o Cloud Provider

### Auto-detect via URL do API server no kubeconfig

| Cloud | Padrão da URL | Exemplo |
|---|---|---|
| **Azure AKS** | `*.azmk8s.io` | `https://akspriv-abc.hcp.brazilsouth.azmk8s.io` |
| **AWS EKS** | `*.eks.amazonaws.com` | `https://ABC123.gr7.us-east-1.eks.amazonaws.com` |

**Por que URL e não nome do contexto?**
Nomes de contexto são livres e arbitrários. A URL é gerada pelo cloud provider — fonte de verdade mais confiável, sem exigir convenção de nomenclatura.

**Fallback:** campo opcional `cloud_provider` no `clusters-config.json` para casos onde a URL não seja conclusiva.

---

## Autenticação AWS — Como Funciona Transparentemente

O kubeconfig gerado pelo `aws eks update-kubeconfig` embute um exec plugin:

```yaml
users:
- name: arn:aws:eks:us-east-1:123:cluster/meu-eks
  user:
    exec:
      command: aws
      args: [eks, get-token, --cluster-name, meu-eks, --region, us-east-1, --profile, meu-perfil]
```

O `client-go` (já usado pela aplicação) **chama esse exec plugin automaticamente**. Portanto:

- `getClient()` em `kubeconfig.go` **já funciona para EKS sem nenhuma modificação**
- O perfil AWS está embutido no kubeconfig — a aplicação não precisa conhecê-lo para chamadas K8s
- O campo `AwsProfile` em `ClusterConfig` é útil apenas para operações AWS CLI (Node Groups, `aws sts get-caller-identity`)

---

## Arquitetura: Provider Abstraction Layer

O acoplamento atual com Azure CLI está em duas áreas:

| Área | Acoplamento |
|---|---|
| **Node Pools tab** | `az aks nodepool list/scale/update/operation-abort`, `az account set/show`, `ValidateAzureAuth()` |
| **FinOps tab** | `AzurePricer` → `https://prices.azure.com/api/retail/prices` com SKU Azure (`Standard_D4s_v3`) |

**O que já é cloud-agnostic e não muda:**
- Todo `client-go` (K8s API) — funciona em AKS e EKS
- Cordon/Drain (kubectl via K8s API)
- HPAs, ConfigMaps, Deployments, Secrets, etc.
- FinOps Calculator (lógica de alocação de custo)
- Scanner de node pools — usa label `node.kubernetes.io/instance-type` que **existe igual no EKS** (retorna `m5.xlarge` ao invés de `Standard_D4s_v3`)

### Estrutura proposta

```
internal/
└── cloudprovider/
    ├── interface.go          ← interfaces comuns
    ├── azure/
    │   ├── nodegroup.go      ← az aks nodepool ... (código atual migrado)
    │   ├── pricer.go         ← AzurePricer migrada para cá
    │   └── auth.go           ← ValidateAzureAuth()
    └── aws/
        ├── nodegroup.go      ← aws eks list-nodegroups/update-nodegroup-config
        ├── pricer.go         ← AWS Pricing API
        └── auth.go           ← aws sts get-caller-identity --profile
```

### Interface Node Group

```go
// internal/cloudprovider/interface.go

type NodeGroup struct {
    Name        string
    NodeCount   int
    MinCount    int
    MaxCount    int
    VMSize      string  // "Standard_D4s_v3" (AKS) ou "m5.xlarge" (EKS)
    Mode        string  // "System"/"User" (AKS) ou "" (EKS)
    Autoscaling bool
    Status      string
}

type NodeGroupProvider interface {
    ListNodeGroups(ctx context.Context, cluster string) ([]NodeGroup, error)
    ScaleNodeGroup(ctx context.Context, cluster, group string, count int) error
    SetAutoscaling(ctx context.Context, cluster, group string, min, max int) error
    AbortOperation(ctx context.Context, cluster, group string) error // EKS: ErrNotSupported
    ValidateAuth(ctx context.Context) error
}

type CloudPricer interface {
    GetPrice(ctx context.Context, vmSize, region string) (float64, error)
    GetVMSpecs(vmSize string) (cpuCores int, memGB int)
}
```

### Equivalência de comandos AKS → EKS

| Operação | Azure CLI | AWS CLI |
|---|---|---|
| Listar node groups | `az aks nodepool list --rg X --cluster-name C` | `aws eks list-nodegroups --cluster-name C` + `describe-nodegroup` por cada |
| Escalar (desired) | `az aks nodepool scale --node-count N` | `aws eks update-nodegroup-config --scaling-config desiredSize=N` |
| Autoscaling min/max | `az aks nodepool update --enable-cluster-autoscaler --min N --max M` | `aws eks update-nodegroup-config --scaling-config minSize=N,maxSize=M` |
| Abort operação | `az aks nodepool operation-abort` | ❌ não existe — botão escondido para EKS |
| Auth check | `az account show` | `aws sts get-caller-identity --profile P` |

### FinOps: troca cirúrgica no Calculator

```go
// Antes (acoplado):
type Calculator struct {
    pricer   *AzurePricer
    exchange *ExchangeRateProvider
}

// Depois (genérico):
type Calculator struct {
    pricer   CloudPricer   // AzurePricer ou AWSPricer
    exchange *ExchangeRateProvider
}
```

O `NodePoolRegistryEntry.VMSize` já armazena o valor correto para ambos os clouds (vem do label K8s `node.kubernetes.io/instance-type`) — **o scanner não muda**.

O handler escolhe o provider pelo `cloud_provider` do cluster:

```go
provider, err := h.kubeManager.GetNodeGroupProvider(cluster)
// Retorna AzureNodeGroupProvider ou AWSNodeGroupProvider conforme cloud_provider
pools, err := provider.ListNodeGroups(ctx, cluster)
```

---

## Fluxo de Uso — Sugestão de VPN

```
Usuário seleciona cluster X
        │
        ▼
App detecta CloudProvider de X via URL do kubeconfig
        │
        ├─► "aks" — cloud atual = aks → continua normalmente
        │
        └─► "eks" — cloud atual = aks → exibe banner/toast:
                    ┌──────────────────────────────────────────┐
                    │ ⚠️  Este cluster é AWS EKS                │
                    │  Verifique se sua VPN AWS está ativa      │
                    │  antes de continuar.          [OK]        │
                    └──────────────────────────────────────────┘
```

O "cloud atual" é inferido pelo último cluster selecionado com sucesso. Ao iniciar a aplicação, não há cloud ativo — o aviso aparece sempre que o cloud mudar.

---

## Checklist de Implementação

### Fase 0 — Provider Abstraction Layer *(pré-requisito de tudo)*
- [x] Criar `internal/cloudprovider/interface.go` com `NodeGroupProvider`
- [x] Migrar código `az aks nodepool` de `nodepools.go` para `internal/cloudprovider/azure/nodegroup.go`
- [x] `AzurePricer` implementa `CloudPricer` (interface em `internal/finops/pricer.go`); `AzurePricer` não foi movida de `azure_pricing.go` mas satisfaz a interface
- [x] `ValidateAzureAuth()` delegada via `AzureNodeGroupProvider.ValidateAuth()` (sem arquivo `auth.go` separado)
- [x] Criar esqueleto `internal/cloudprovider/aws/` (stubs que retornam `ErrNotSupported`)
- [x] `Calculator` em `internal/finops/calculator.go` passa a receber `CloudPricer` ao invés de `*AzurePricer`
- [x] `KubeConfigManager.GetNodeGroupProvider(cluster)` retorna o provider correto
- [x] Handlers `List`, `Update`, `Abort`, `ApplySequential`, `executeSequenceAsync` usam provider (removido código `az` CLI direto e `validators.ValidateAzureAuth`)
- [x] `SetAutoscaling` com fallback `--update-cluster-autoscaler` → `--enable-cluster-autoscaler`
- [x] Bug corrigido: `h.applyNodePoolChanges` passava pool name como cluster name para `az aks`

### Fase 1 — Detecção de Cloud Provider
- [x] Criar `internal/config/cloud_provider.go` com `DetectCloudProvider(serverURL)`, `ExtractRegionFromEKSURL()` e `extractAKSRegion()`
- [x] `DiscoverClusters()` — sem filtro `akspriv-`, popula `CloudProvider` e `Region` via URL do API server
- [x] `ValidateConfig()` — aceita qualquer cluster com contexto válido (sem restrição `akspriv-*`)
- [x] Adicionar `CloudProvider` e `Region` ao tipo `models.Cluster`
- [x] `ClusterConfig` — campos opcionais `AwsRegion` e `AwsProfile` (retrocompatível)
- [x] `GetNodeGroupProvider()` — usa cloud provider detectado por URL para escolher Azure ou AWS provider
- [x] Expor `cloud_provider` e `region` no endpoint `GET /api/v1/clusters`
- [x] Testes unitários para `DetectCloudProvider`, `ExtractRegionFromEKSURL`, `extractAKSRegion`

### Fase 2 — UX de Sugestão de VPN
- [x] `cloud_provider` adicionado ao tipo `Cluster` em `types.ts`
- [x] Hook `useCloudProvider()` — detecta mudança de cloud, gerencia `sessionStorage` para "primeira vez"
- [x] Badge `[AKS]`/`[EKS]` no Header (combobox) e em `ClusterSelectorForTab`
- [x] `VpnWarningDialog` — dialog modal na primeira troca de cloud por sessão
- [x] Toast de aviso nas trocas subsequentes de cloud na mesma sessão
- [x] `clusterProviders` map passado do `Index.tsx` para `Header` e disponível para outros selects

### Fase 3 — Cloud-Specific UX
- [ ] Esconder aba "Node Pools" quando `cloud_provider === "eks"`
- [ ] Pular `ValidateAzureAuth` para clusters EKS
- [ ] Esconder botão "Abort" no Node Pools quando `cloud_provider === "eks"`

### Fase 4 — AWS Node Groups (implementação real)
- [ ] Implementar `AWSNodeGroupProvider.ListNodeGroups` via `aws eks list-nodegroups` + `describe-nodegroup`
- [ ] Implementar `AWSNodeGroupProvider.ScaleNodeGroup` via `aws eks update-nodegroup-config`
- [ ] Implementar `AWSNodeGroupProvider.SetAutoscaling` via `aws eks update-nodegroup-config`
- [ ] Implementar `AWSNodeGroupProvider.ValidateAuth` via `aws sts get-caller-identity --profile`
- [ ] Exibir mensagem orientativa em falha de autenticação EKS

### Fase 5 — AWS Pricer (FinOps para EKS)
- [ ] Implementar `AWSPricer` consultando AWS Pricing API com cache SQLite (mesmo padrão do `AzurePricer`)
- [ ] Mapear tipos EC2 (`m5.xlarge`, `t3.medium`, etc.) para vCPU/RAM no `vmSpecs`
- [ ] FinOps report funcional para clusters EKS

---

## O que NÃO está no escopo

- ❌ Conectar/desconectar VPN pela aplicação
- ❌ Armazenar credenciais ou perfis VPN
- ❌ Gerenciar `~/.aws/credentials` ou tokens Azure
- ❌ Suporte a GKE ou outros clouds (por ora)
- ❌ Chamar AWS SDK Go — usar `aws` CLI (mesmo padrão que AKS usa `az` CLI)
