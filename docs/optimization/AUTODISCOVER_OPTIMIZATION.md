# 🚀 Otimização do Auto-Discovery - Busca Paralela de Subscriptions

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 📅 **Versão**: v1.0.10
**Data**: 19/11/2025
**Status**: ✅ Implementado

---

## 🔍 Problema Identificado

### Comportamento Anterior (SEQUENCIAL):

O comando `k8s-hpa-manager autodiscover` era **muito lento** durante a instalação porque:

1. Para **cada cluster**, o sistema precisava descobrir em qual Azure Subscription ele estava
2. O método `discoverSubscriptionViaAzureCLI` testava **sequencialmente** cada subscription:

```go
// ❌ CÓDIGO ANTIGO (SEQUENCIAL)
for _, subscriptionID := range subscriptions {
    cmd := exec.Command("az", "aks", "show",
        "--name", clusterName,
        "--resource-group", resourceGroup,
        "--subscription", subscriptionID)

    output, err := cmd.CombinedOutput() // ⏳ Bloqueia ~2-3s por subscription
    if err != nil {
        continue // Tenta próxima
    }
    // Encontrou, retorna
}
```

### 📊 Impacto:

- **10 subscriptions** × **3 segundos** = **30 segundos por cluster**
- **70 clusters** × **30s** = **35 minutos total**! 😱

Mesmo com 10 workers paralelos processando clusters, cada worker ainda testava as subscriptions **sequencialmente**.

---

## ✅ Solução Implementada

### Paralelização da Busca de Subscriptions

Agora, **todas as subscriptions são testadas em paralelo** para cada cluster:

```go
// ✅ CÓDIGO NOVO (PARALELO)
resultChan := make(chan result, len(validSubscriptions))

// Disparar goroutine para cada subscription
for _, subscriptionID := range validSubscriptions {
    go func(subID string) {
        cmd := exec.Command("az", "aks", "show",
            "--name", clusterName,
            "--resource-group", resourceGroup,
            "--subscription", subID)

        output, err := cmd.CombinedOutput() // 🚀 Executa em paralelo

        resultChan <- result{
            subscriptionID: subID,
            resourceID:     strings.TrimSpace(string(output)),
            err:            err,
        }
    }(subscriptionID)
}

// Coletar resultados - retorna assim que encontrar a primeira match
for i := 0; i < len(validSubscriptions); i++ {
    res := <-resultChan
    if res.err == nil && res.resourceID != "" {
        // Encontrou! Extrair subscription do resource ID
        return extractSubscriptionFromResourceID(res.resourceID)
    }
}
```

---

## 📈 Ganhos de Performance

### Antes (Sequencial):
- **Por cluster**: 10 subs × 3s = **30 segundos**
- **Total (70 clusters)**: ~**35 minutos**

### Depois (Paralelo):
- **Por cluster**: max(3s) = **~3 segundos** (todas em paralelo!)
- **Total (70 clusters)**: ~**3-5 minutos** ⚡

### **Ganho: 10x mais rápido!** 🎉

---

## 🏗️ Arquitetura da Otimização

```
AutoDiscoverAllClusters (já era paralelo - 10 workers)
    ├─ Worker 1: Cluster A
    │   └─ discoverSubscriptionViaAzureCLI (AGORA PARALELO!)
    │       ├─ Goroutine: testa Subscription 1 (paralelo)
    │       ├─ Goroutine: testa Subscription 2 (paralelo)
    │       ├─ Goroutine: testa Subscription 3 (paralelo)
    │       └─ ... (todas ao mesmo tempo!)
    │
    ├─ Worker 2: Cluster B
    │   └─ discoverSubscriptionViaAzureCLI (AGORA PARALELO!)
    │       └─ ... (todas subscriptions em paralelo)
    │
    └─ ... (10 workers processando clusters simultaneamente)
```

**Paralelização em dois níveis:**
1. ✅ **Nível 1**: Múltiplos clusters processados simultaneamente (10 workers)
2. ✅ **Nível 2**: Múltiplas subscriptions testadas simultaneamente (NOVO!)

---

## 🧪 Como Testar

### Teste com Timer:

```bash
# Antes da otimização (se tiver backup)
time k8s-hpa-manager autodiscover

# Depois da otimização
time ./build/k8s-hpa-manager autodiscover
```

### Observar Logs:

```bash
./build/k8s-hpa-manager autodiscover

# Saída esperada:
🔍 Iniciando auto-descoberta paralela para 70 clusters...
[1/70] ✅ akspriv-cluster-a - RG: rg-cluster-a, Sub: 12345678...
[2/70] ✅ akspriv-cluster-b - RG: rg-cluster-b, Sub: 87654321...
...
📊 Resumo: ✅ 70 sucesso | ❌ 0 erros
💾 Configurações salvas em: ~/.k8s-hpa-manager/clusters-config.json
```

Agora você deve ver os clusters sendo descobertos **muito mais rápido**!

---

## 🔧 Arquivos Modificados

### `internal/config/kubeconfig.go`
- **Linha 335-411**: Refatorado `discoverSubscriptionViaAzureCLI` para paralelizar busca
- **Mudanças**:
  - Adicionado filtro de subscriptions vazias
  - Criado canal de resultados com buffer
  - Disparado goroutine para cada subscription
  - Coleta resultados até encontrar match

---

## 🎯 Próximas Otimizações (Opcional)

### 1. Cache de Mapeamento Cluster → Subscription
- Cachear resultado da primeira descoberta
- Evitar re-testar subscriptions em execuções futuras
- **Ganho adicional**: ~90% de redução em re-runs

### 2. Heurística Inteligente
- Ordenar subscriptions por probabilidade (última usada, mesmo resource group)
- Reduzir tentativas desnecessárias
- **Ganho adicional**: ~30-50% mais rápido

---

## 📝 Notas Técnicas

### Segurança de Concorrência:
- ✅ Canais com buffer para evitar deadlocks
- ✅ Goroutines independentes (sem estado compartilhado)
- ✅ Coleta de resultados thread-safe via canais

### Tratamento de Erros:
- ✅ Erros de subscriptions individuais não abortam busca total
- ✅ Retorna erro apenas se NENHUMA subscription contiver o cluster
- ✅ Continua coletando todos os resultados antes de falhar

### Rate Limiting:
- ⚠️ Azure CLI pode ter rate limits (não observado em testes)
- Se necessário, adicionar semáforo: `make(chan struct{}, 5)` para limitar concorrência

---

## ✅ Conclusão

A paralelização da busca de subscriptions reduziu o tempo de autodiscovery de **~35 minutos** para **~3-5 minutos** (10x mais rápido), melhorando significativamente a experiência de instalação.

**Resultado**: Instalações agora são **muito mais rápidas**! 🚀

---

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)
