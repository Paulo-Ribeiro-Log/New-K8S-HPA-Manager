# 🚀 Plano de Otimização - Auto Descoberta Assíncrona

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 📅 **Versão**: v1.0.9 (Planejamento)
**Data**: 19/11/2025
**Status**: 📋 Planejamento

---

## 🔍 **Análise dos Gargalos Atuais**

### 1. **`AutoDiscoverAllClusters` (internal/config/kubeconfig.go:386)**
✅ **JÁ É ASSÍNCRONO** com semáforo (10 workers paralelos)
- Processa múltiplos clusters simultaneamente
- Usa goroutines com canal de resultados
- **Não precisa de otimização**

### 2. **`DiscoverClusterResources` (internal/kubernetes/client.go:1244)**
❌ **TOTALMENTE SÍNCRONO** - Principal gargalo!
- Lista namespaces sequencialmente
- Para cada namespace, lista:
  - Deployments (sequencial)
  - StatefulSets (sequencial)
  - DaemonSets (sequencial)
- **Problema**: Com 70+ namespaces e centenas de recursos, fica MUITO lento
- **Tempo estimado**: 30-60 segundos

### 3. **`fetchMetricsAsync` (internal/tui/app.go:4138)**
⚠️ **PSEUDO-ASSÍNCRONO** - Roda em goroutine mas é sequencial internamente
- Itera recursos um por um
- Cada `GetPodMetrics` é síncrono (kubectl top)
- **Problema**: Se há 100+ recursos, demora muito
- **Tempo estimado**: 20-40 segundos

---

## 🎯 **Estratégias de Otimização**

### **Fase 1: Tornar `DiscoverClusterResources` Assíncrono** ⭐
**Impacto: ALTO** | **Complexidade: MÉDIA**

**Estratégia**: Worker Pool com goroutines
1. Listar todos os namespaces (rápido)
2. Criar worker pool (10-20 workers)
3. Cada worker processa 1 namespace:
   - Lista Deployments, StatefulSets, DaemonSets em PARALELO (3 goroutines)
   - Envia resultados para canal agregador
4. Coletar e consolidar resultados

**Ganho esperado**: 5-10x mais rápido (30-60s → 5-10s)

**Mudanças necessárias:**
- Refatorar `DiscoverClusterResources` para usar worker pool
- Adicionar goroutines para listar cada tipo de recurso por namespace
- Manter interface compatível (retornar `[]models.ClusterResource`)

---

### **Fase 2: Otimizar `fetchMetricsAsync`** ⭐⭐
**Impacto: ALTO** | **Complexidade: MÉDIA**

**Estratégia**: Paralelizar busca de métricas
1. Dividir recursos em lotes (batches de 10-20)
2. Criar goroutines para buscar métricas em paralelo
3. Usar canal para atualizar progressivamente a UI
4. Rate limiting (evitar sobrecarregar kubectl/API)

**Ganho esperado**: 3-5x mais rápido (20-40s → 5-10s)

**Mudanças necessárias:**
- Converter loop sequencial em worker pool
- Adicionar rate limiting (semáforo)
- Enviar updates progressivos via tea.Msg

---

### **Fase 3: Cache Inteligente** ⭐
**Impacto: MÉDIO** | **Complexidade: BAIXA**

**Estratégia**: Cache com TTL
1. Cachear resultados de DiscoverClusterResources (5 minutos)
2. Cachear métricas (30-60 segundos)
3. Invalidar cache ao fazer mudanças

**Ganho esperado**: Reduz chamadas repetidas em 80%

---

### **Fase 4: Progressive Loading (UX)** ⭐⭐⭐
**Impacto: ALTO (percepção do usuário)** | **Complexidade: MÉDIA**

**Estratégia**: Mostrar resultados progressivamente
1. Retornar recursos conforme descobertos (não esperar tudo)
2. Atualizar lista em tempo real via tea.Msg
3. Mostrar progress bar/spinner durante descoberta
4. Permitir interação enquanto descobre

**Ganho esperado**: Usuário vê primeiros resultados em 1-2s (vs 60s bloqueado)

---

## 📊 **Implementação Proposta**

### **Prioridade 1 (CRÍTICA):**
1. ✅ Refatorar `DiscoverClusterResources` para worker pool assíncrono
2. ✅ Adicionar progressive loading na UI (mostrar recursos conforme descobertos)

### **Prioridade 2 (IMPORTANTE):**
3. ✅ Otimizar `fetchMetricsAsync` com paralelização
4. ✅ Adicionar progress indicators na TUI/Web

### **Prioridade 3 (OPCIONAL):**
5. ⏸️ Implementar cache com TTL
6. ⏸️ Rate limiting inteligente

---

## 🛠️ **Exemplo de Implementação - Fase 1**

### **Nova função `DiscoverClusterResources` (assíncrona)**

```go
// internal/kubernetes/client.go

func (c *Client) DiscoverClusterResources(showSystemResources bool, prometheusOnly bool, logFunc func(string, ...interface{})) ([]models.ClusterResource, error) {
    // 1. Listar namespaces (rápido)
    namespaces, err := c.clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
    if err != nil {
        return nil, err
    }

    // Filtrar namespaces de sistema se necessário
    var targetNamespaces []string
    for _, ns := range namespaces.Items {
        if !showSystemResources && isSystemNamespace(ns.Name) {
            continue
        }
        targetNamespaces = append(targetNamespaces, ns.Name)
    }

    logFunc("📊 Total de namespaces a processar: %d", len(targetNamespaces))

    // 2. Canal para resultados
    type result struct {
        resources []models.ClusterResource
        namespace string
        err       error
    }
    resultChan := make(chan result, len(targetNamespaces))

    // 3. Worker pool (10 workers para namespaces)
    semaphore := make(chan struct{}, 10)

    // 4. Processar namespaces em paralelo
    for _, ns := range targetNamespaces {
        go func(namespace string) {
            semaphore <- struct{}{}
            defer func() { <-semaphore }()

            resources := c.discoverNamespaceResources(namespace, prometheusOnly, logFunc)
            resultChan <- result{
                resources: resources,
                namespace: namespace,
            }
        }(ns)
    }

    // 5. Coletar resultados
    var allResources []models.ClusterResource
    for i := 0; i < len(targetNamespaces); i++ {
        res := <-resultChan
        allResources = append(allResources, res.resources...)
        logFunc("✅ Namespace %s processado: %d recursos", res.namespace, len(res.resources))
    }

    logFunc("📊 Total de recursos descobertos: %d", len(allResources))
    return allResources, nil
}

// Função auxiliar para processar um namespace (com 3 goroutines internas)
func (c *Client) discoverNamespaceResources(namespace string, prometheusOnly bool, logFunc func(string, ...interface{})) []models.ClusterResource {
    var resources []models.ClusterResource
    var mu sync.Mutex // Proteger append concorrente

    var wg sync.WaitGroup
    wg.Add(3)

    // Descobrir Deployments (paralelo)
    go func() {
        defer wg.Done()
        deployments, err := c.clientset.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{})
        if err != nil {
            return
        }
        for _, deployment := range deployments.Items {
            resource := c.createResourceFromDeployment(&deployment)
            if !prometheusOnly || isPrometheusRelated(resource.Name, resource.Namespace) {
                mu.Lock()
                resources = append(resources, resource)
                mu.Unlock()
            }
        }
    }()

    // Descobrir StatefulSets (paralelo)
    go func() {
        defer wg.Done()
        statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(context.Background(), metav1.ListOptions{})
        if err != nil {
            return
        }
        for _, sts := range statefulSets.Items {
            resource := c.createResourceFromStatefulSet(&sts)
            if !prometheusOnly || isPrometheusRelated(resource.Name, resource.Namespace) {
                mu.Lock()
                resources = append(resources, resource)
                mu.Unlock()
            }
        }
    }()

    // Descobrir DaemonSets (paralelo)
    go func() {
        defer wg.Done()
        daemonSets, err := c.clientset.AppsV1().DaemonSets(namespace).List(context.Background(), metav1.ListOptions{})
        if err != nil {
            return
        }
        for _, ds := range daemonSets.Items {
            resource := c.createResourceFromDaemonSet(&ds)
            if !prometheusOnly || isPrometheusRelated(resource.Name, resource.Namespace) {
                mu.Lock()
                resources = append(resources, resource)
                mu.Unlock()
            }
        }
    }()

    wg.Wait()
    return resources
}
```

---

## 🛠️ **Exemplo de Implementação - Fase 2**

### **Nova função `fetchMetricsAsync` (paralela)**

```go
// internal/tui/app.go

func (a *App) fetchMetricsAsync() {
    if a.model.SelectedCluster == nil {
        return
    }
    contextName := a.model.SelectedCluster.Context

    defer func() {
        a.model.FetchingMetrics = false
        a.debugLog("[DEBUG fetchMetricsAsync] Coleta de métricas concluída")
    }()

    if len(a.model.ClusterResources) == 0 {
        a.debugLog("[DEBUG fetchMetricsAsync] Nenhum recurso para coletar métricas")
        return
    }

    // Worker pool para buscar métricas em paralelo
    semaphore := make(chan struct{}, 10) // Máximo 10 kubectl top simultâneos
    var wg sync.WaitGroup

    for i := range a.model.ClusterResources {
        wg.Add(1)
        go func(index int) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()

            // Verificar se índice ainda é válido
            if index >= len(a.model.ClusterResources) {
                return
            }

            resource := &a.model.ClusterResources[index]

            // Buscar métricas via kubectl top
            cpuUsage, memUsage := a.kubeManager.GetPodMetrics(
                contextName,
                resource.Namespace,
                resource.Name,
                resource.WorkloadType,
            )

            // Atualizar campos de exibição
            if cpuUsage != "-" || memUsage != "-" {
                if cpuUsage != "-" {
                    if resource.CurrentCPURequest == "-" {
                        resource.DisplayCPURequest = fmt.Sprintf("- (uso: %s)", cpuUsage)
                    } else {
                        resource.DisplayCPURequest = fmt.Sprintf("%s (uso: %s)", resource.CurrentCPURequest, cpuUsage)
                    }
                } else {
                    resource.DisplayCPURequest = resource.CurrentCPURequest
                }

                if memUsage != "-" {
                    if resource.CurrentMemoryRequest == "-" {
                        resource.DisplayMemoryRequest = fmt.Sprintf("- (uso: %s)", memUsage)
                    } else {
                        resource.DisplayMemoryRequest = fmt.Sprintf("%s (uso: %s)", resource.CurrentMemoryRequest, memUsage)
                    }
                } else {
                    resource.DisplayMemoryRequest = resource.CurrentMemoryRequest
                }

                a.debugLog("[DEBUG fetchMetricsAsync] Atualizado %s/%s - CPU: %s, MEM: %s",
                    resource.Namespace, resource.Name, cpuUsage, memUsage)
            }
        }(i)
    }

    wg.Wait()
}
```

---

## 📈 **Ganhos Esperados**

| Otimização | Ganho de Performance | Tempo Atual | Tempo Esperado |
|------------|---------------------|-------------|----------------|
| **DiscoverClusterResources** (worker pool) | **5-10x** | 30-60s | 5-10s |
| **fetchMetricsAsync** (paralelo) | **3-5x** | 20-40s | 5-10s |
| **Progressive Loading** (UX) | **10x percepção** | 60s bloqueado | 2s primeiros resultados |
| **Cache** (chamadas repetidas) | **∞** | 60s | Instantâneo |

### **Total: De ~60-100s → ~10-20s** ⚡

---

## 🎬 **Próximos Passos**

1. **Implementar Fase 1** (DiscoverClusterResources assíncrono)
2. **Implementar Fase 2** (fetchMetricsAsync paralelo)
3. **Implementar Fase 4** (Progressive loading na UI)
4. **Testes de performance** (comparar antes/depois)
5. **Implementar Fase 3** (cache, se necessário)

---

## 📝 **Notas de Implementação**

### **Segurança de Concorrência**
- Usar `sync.Mutex` para proteger acesso a slices compartilhados
- Usar `sync.WaitGroup` para sincronizar goroutines
- Canais com buffer para evitar deadlocks

### **Tratamento de Erros**
- Não abortar toda a descoberta se um namespace falhar
- Logar erros individuais mas continuar processamento
- Retornar recursos parciais em caso de erro

### **Rate Limiting**
- Semáforo com 10 workers para namespaces
- Semáforo com 10 workers para métricas
- Evitar sobrecarregar API do Kubernetes

---

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)
