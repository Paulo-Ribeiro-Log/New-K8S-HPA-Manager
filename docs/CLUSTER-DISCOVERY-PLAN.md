# Cluster Discovery — Paralelismo + Config EKS Separado

Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

## Diagnóstico

O código já tem paralelismo, mas com gargalos:

```
AutoDiscoverAllClusters → semáforo cap. 3
  └→ discoverSubscriptionViaAzureCLI (por cluster)
       ├─ az account list          ← chamado N vezes (uma por cluster!)
       └─ az aks show              ← M chamadas, semáforo cap. 5
            → para cada subscription até achar o cluster
```

**Gargalo 1 — `az account list` redundante:** cada cluster chama individualmente, overhead de ~1-2s de startup do Azure CLI por execução. Com 30 clusters isso é 30 chamadas desnecessárias.

**Gargalo 2 — semáforo conservador:** cap. 3 clusters simultâneos × cap. 5 subscriptions = 15 processos `az`. Pode ir para 10×15 = 150 com segurança.

**Gargalo 3 — EKS passa pelo fluxo AKS:** `AutoDiscoverClusterConfig` tenta `extractResourceGroupFromKubeconfig` e `discoverSubscriptionViaAzureCLI` para clusters EKS — falha em formato diferente, sem fallback útil. EKS precisa de `aws eks list-clusters`, não de `az aks show`.

**Gargalo 4 — config misturado:** `clusters-config.json` mistura campos AKS (`resourceGroup`, `subscription`) com campos EKS (`awsRegion`, `awsProfile`) na mesma struct, sem separação por provider.

---

## Fases de implementação

---

### Fase 1 — Separação de structs e arquivo EKS ✅ CONCLUÍDA

**Arquivos modificados/criados:**
- `internal/config/kubeconfig.go` — separar `ClusterConfig` (AKS puro) da struct híbrida atual
- `internal/config/eks_config.go` ← CRIAR — `EKSClusterConfig` + load/save dedicado

**O que muda na struct:**

```go
// ClusterConfig — AKS only (sem campos AWS)
type ClusterConfig struct {
    Name           string `json:"clusterName"`
    ResourceGroup  string `json:"resourceGroup"`
    Subscription   string `json:"subscription"`
    SubscriptionID string `json:"subscriptionId,omitempty"`
}

// EKSClusterConfig — EKS only (sem campos Azure)
type EKSClusterConfig struct {
    Name       string   `json:"clusterName"`
    AwsRegion  string   `json:"awsRegion"`
    AwsProfile string   `json:"awsProfile"`
    AccountID  string   `json:"awsAccountId,omitempty"`
    VpcID      string   `json:"vpcId,omitempty"`
    NodeGroups []string `json:"nodeGroups,omitempty"`
}
```

**Arquivo EKS:** `~/.k8s-hpa-manager/eks-clusters-config.json`

**Métodos novos em `eks_config.go`:**
- `LoadEKSClustersFromConfig() []EKSClusterConfig`
- `SaveEKSClusterConfigs(configs []EKSClusterConfig, logFunc func(string)) error`

**Impacto em `GetNodeGroupProvider`** (`kubeconfig.go:664`):
- Ao resolver config de um cluster EKS, ler de `eks-clusters-config.json` em vez de `clusters-config.json`
- Manter retrocompatibilidade: se cluster EKS estiver ainda no `clusters-config.json` (migração), usar campos `awsRegion`/`awsProfile` de lá como fallback

- [x] Remover `AwsRegion`/`AwsProfile` de `ClusterConfig`
- [x] Criar `EKSClusterConfig` em `internal/config/eks_config.go`
- [x] Criar `LoadEKSClustersFromConfig()` e `SaveEKSClusterConfigs()`
- [x] Atualizar `GetNodeGroupProvider` para ler EKS config do arquivo correto
- [x] Compilar e verificar que nenhuma referência quebrou

---

### Fase 2 — EKS Discovery (`eks_discovery.go`) ✅ CONCLUÍDA

**Arquivo:** `internal/config/eks_discovery.go` ← CRIAR

**Estratégia:** Uma chamada `aws eks list-clusters` por profile+região já retorna todos os clusters — sem iterar subscription por subscription como o AKS precisa fazer.

```
AutoDiscoverEKSClusters(logFunc):
  1. aws configure list-profiles  →  lista todos os profiles
  2. para cada profile (paralelo, semáforo cap. 5):
       para cada região (us-east-1, sa-east-1, us-east-2 — configurável):
         aws eks list-clusters --profile X --region Y
           └→ para cada cluster retornado:
                aws eks describe-cluster --name Z → extrai VPC, nodegroups, ARN, accountId
  3. Retorna []EKSClusterConfig
```

Regions default a escanear: `["us-east-1", "us-east-2", "sa-east-1", "us-west-2"]`

- [x] Criar `internal/config/eks_discovery.go`
- [x] Função `AutoDiscoverEKSClusters(logFunc func(string)) ([]EKSClusterConfig, []error)`
- [x] Função `listEKSProfiles(ctx) ([]string, error)`
- [x] Função `discoverEKSClustersInRegion(ctx, profile, region string) ([]EKSClusterConfig, error)`
- [x] Timeout global: 5 minutos; timeout por comando `aws`: 30s
- [x] Semáforo: cap. 5 por profile×região simultâneos

---

### Fase 3 — Otimização AKS (subscriptions pré-carregadas) ✅ CONCLUÍDA

**Arquivo:** `internal/config/kubeconfig.go`

**Mudança principal:** `az account list` chamado **uma única vez** antes do loop de clusters, resultado compartilhado via argumento.

```go
// Antes (N chamadas, uma por cluster):
func (k *KubeConfigManager) discoverSubscriptionViaAzureCLI(clusterName, resourceGroup string) (string, string, error) {
    cmd := exec.Command("az", "account", "list", ...)  // ← chamado para CADA cluster
    ...
}

// Depois (1 chamada compartilhada):
func (k *KubeConfigManager) AutoDiscoverAllClusters(logFunc func(string)) ([]ClusterConfig, []error) {
    subscriptions := k.loadAllAzureSubscriptions()  // ← 1 vez só
    // passa subscriptions para cada goroutine
    ...
}

func (k *KubeConfigManager) discoverSubscriptionViaAzureCLI(
    clusterName, resourceGroup string,
    subscriptions []string,  // ← recebe pronto, não busca mais
) (string, string, error) { ... }
```

**Ajustes de semáforo:**

| Parâmetro | Atual | Novo |
|-----------|-------|------|
| Clusters AKS simultâneos | 3 | 10 |
| Subscriptions por cluster | 5 | 15 |
| Timeout global autodiscover | 5 min | 10 min |
| Timeout por `az` call | 30s | 20s (fail-fast) |

- [x] Extrair `loadAllAzureSubscriptions(ctx) []string` — com timeout 30s
- [x] Atualizar assinatura de `discoverSubscriptionViaAzureCLI` para receber `[]string`
- [x] Ajustar semáforos: clusters 3→10, subscriptions 5→15, timeout global 5min→10min, por cmd 30s→20s
- [x] Passar contexto pai para subcomandos AKS (cancelamento em cascata)

---

### Fase 4 — Handler unificado AKS + EKS ✅ CONCLUÍDA

**Arquivo:** `internal/web/handlers/autodiscover.go`

Discovery AKS e EKS rodam em paralelo via duas goroutines independentes. SSE emite progresso de ambos com prefixo `[AKS]` / `[EKS]`.

```go
// Fase 2: processar AKS e EKS em paralelo
var wgProviders sync.WaitGroup
wgProviders.Add(2)

go func() {
    defer wgProviders.Done()
    aksConfigs, aksErrors = manager.AutoDiscoverAllClusters(func(msg string) {
        progressChan <- AutoDiscoverProgress{Phase: "processing", Message: "[AKS] " + msg, ...}
    })
}()

go func() {
    defer wgProviders.Done()
    eksConfigs, eksErrors = manager.AutoDiscoverEKSClusters(func(msg string) {
        progressChan <- AutoDiscoverProgress{Phase: "processing", Message: "[EKS] " + msg, ...}
    })
}()

wgProviders.Wait()
```

**Salvar em arquivos separados:**
- `SaveClusterConfigs(aksConfigs, ...)` → `clusters-config.json`
- `SaveEKSClusterConfigs(eksConfigs, ...)` → `eks-clusters-config.json`

**Atualizar `AutoDiscoverProgress`:** adicionar campo `Provider string` (`"aks"` | `"eks"` | `""`) para o frontend diferenciar visualmente.

- [x] Atualizar `AutoDiscoverProgress` com campo `Provider`
- [x] Rodar AKS e EKS em goroutines paralelas no handler
- [x] Salvar em arquivos separados após conclusão de cada provider
- [x] Atualizar `HandleAutoDiscoverSync` da mesma forma

---

### Fase 5 — Frontend ✅ CONCLUÍDA

**Arquivo:** `internal/web/frontend/src/components/` (modal/tab de autodiscover)

- [x] Cards de status separados por provider (AKS azul / EKS laranja) com contadores individuais
- [x] Badge colorido em cada linha de log: AKS=azul, EKS=laranja
- [x] Prefixos `[AKS]`/`[EKS]` removidos do texto (substituídos pelo badge visual)
- [x] Card de total com soma AKS+EKS após conclusão
- [x] Botão "Executar Novamente" sem precisar fechar o modal
- [x] Auto-scroll nos logs durante a descoberta

---

## Arquivos a criar/modificar

```
internal/config/kubeconfig.go                ← Fase 1 (struct) + Fase 3 (semáforos, pré-load)
internal/config/eks_config.go                ← CRIAR — Fase 1 (EKSClusterConfig, load/save)
internal/config/eks_discovery.go             ← CRIAR — Fase 2 (AutoDiscoverEKSClusters)
internal/web/handlers/autodiscover.go        ← Fase 4 (handler paralelo)
internal/web/frontend/src/.../autodiscover   ← Fase 5 (UI)
```

## Retrocompatibilidade

- `clusters-config.json` existente com campos `awsRegion`/`awsProfile` → `GetNodeGroupProvider` usa como fallback até o usuário rodar a nova descoberta
- Nenhuma API pública quebra: `ClusterConfig` remove campos opcionais `omitempty`, quem recebia JSON com esses campos simplesmente para de recebê-los
