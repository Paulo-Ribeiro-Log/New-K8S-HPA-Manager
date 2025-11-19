# Plano: Cordon/Drain para Sequenciamento de Node Pools

## 🎯 Objetivo

Adicionar opções de **cordon** e **drain** no sequenciamento de node pools para permitir transição segura entre node pools de Prometheus (ex: normal → black friday) sem downtime.

## 📋 Cenário de Uso

**Caso Prometheus Stack:**
- **Node Pool Normal**: `prometheus-np-small` (uso diário, VMs menores)
- **Node Pool Black Friday**: `prometheus-np-large` (eventos, VMs maiores, 2x recursos)
- **Restrição**: Apenas 1 node pool ativo por vez
- **Requisito**: Transição sem perda de dados ou downtime

## 🔄 Fluxo Atual (TUI)

```
1. Usuário seleciona 2 node pools
2. Marca primeiro como *1 (F12)
3. Marca segundo como *2 (F12)
4. Ctrl+U aplica o primeiro
5. Após primeiro completar → segundo executa automaticamente
```

**Problema**: Não faz cordon/drain, pods podem ser interrompidos abruptamente.

## ✨ Fluxo Proposto

### Sequência Segura com Cordon/Drain:

```
┌─────────────────────────────────────────────────────────┐
│ FASE 1: Scale Up do Segundo Node Pool (Black Friday)   │
└─────────────────────────────────────────────────────────┘
  ✅ Aplicar mudanças no segundo node pool (ex: min=3, max=10)
  ⏳ Aguardar nodes ficarem Ready (kubectl get nodes)

┌─────────────────────────────────────────────────────────┐
│ FASE 2: Cordon do Primeiro Node Pool (Normal)          │
└─────────────────────────────────────────────────────────┘
  ✅ kubectl cordon <node-1>
  ✅ kubectl cordon <node-2>
  ✅ ...
  ℹ️  Nodes marcados como unschedulable (pods novos vão para BF)

┌─────────────────────────────────────────────────────────┐
│ FASE 3: Drain do Primeiro Node Pool                    │
└─────────────────────────────────────────────────────────┘
  ✅ kubectl drain <node-1> --ignore-daemonsets --delete-emptydir-data --force
  ✅ kubectl drain <node-2> --ignore-daemonsets --delete-emptydir-data --force
  ✅ ...
  ℹ️  Pods migrados gracefully para node pool BF
  ⏳ Aguardar todos os pods serem movidos

┌─────────────────────────────────────────────────────────┐
│ FASE 4: Scale Down do Primeiro Node Pool               │
└─────────────────────────────────────────────────────────┘
  ✅ Aplicar mudanças no primeiro node pool (ex: min=0, max=0)
  ℹ️  Nodes vazios são desligados
```

## 🏗️ Arquitetura da Solução

### 1. Modelo de Dados (NodePool)

```go
// internal/models/types.go
type NodePool struct {
    // ... campos existentes ...

    // NOVO: Opções de sequenciamento avançado
    SequenceOrder     int    // 1, 2 (já existe)
    SequenceStatus    string // pending, executing, completed (já existe)

    // NOVO: Configurações de cordon/drain
    CordonEnabled     bool   // Se deve fazer cordon antes de scale down
    DrainEnabled      bool   // Se deve fazer drain antes de scale down
    DrainTimeout      int    // Timeout em segundos (padrão: 300s)
    DrainGracePeriod  int    // Grace period em segundos (padrão: 30s)

    // NOVO: Status de operações
    CordonStatus      string // idle, cordoning, cordoned, failed
    DrainStatus       string // idle, draining, drained, failed
    NodesInNodePool   []string // Lista de nodes deste node pool
}
```

### 2. Funções Kubernetes (internal/kubernetes/client.go)

```go
// NOVO: Listar nodes de um node pool específico
func (c *Client) GetNodesInNodePool(ctx context.Context, nodePoolName string) ([]string, error) {
    // kubectl get nodes -l agentpool=<nodePoolName> -o name
    nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("agentpool=%s", nodePoolName),
    })

    var nodeNames []string
    for _, node := range nodes.Items {
        nodeNames = append(nodeNames, node.Name)
    }
    return nodeNames, nil
}

// NOVO: Cordon de um node
func (c *Client) CordonNode(ctx context.Context, nodeName string) error {
    // kubectl cordon <node>
    node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
    if err != nil {
        return err
    }

    node.Spec.Unschedulable = true
    _, err = c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
    return err
}

// NOVO: Drain de um node
func (c *Client) DrainNode(ctx context.Context, nodeName string, timeout, gracePeriod int) error {
    // kubectl drain <node> --ignore-daemonsets --delete-emptydir-data --force --timeout=300s --grace-period=30

    // Implementação usando eviction API
    pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
    })

    for _, pod := range pods.Items {
        // Skip DaemonSets
        if isDaemonSetPod(pod) {
            continue
        }

        // Evict pod
        eviction := &policyv1.Eviction{
            ObjectMeta: metav1.ObjectMeta{
                Name:      pod.Name,
                Namespace: pod.Namespace,
            },
            DeleteOptions: &metav1.DeleteOptions{
                GracePeriodSeconds: int64Ptr(int64(gracePeriod)),
            },
        }

        err := c.clientset.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
        if err != nil {
            return err
        }
    }

    return nil
}

// NOVO: Verificar se todos os pods foram drenados
func (c *Client) IsNodeDrained(ctx context.Context, nodeName string) (bool, error) {
    pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
    })

    for _, pod := range pods.Items {
        if !isDaemonSetPod(pod) && !isEmptyDirOnlyPod(pod) {
            return false, nil // Ainda há pods non-daemonset
        }
    }

    return true, nil
}
```

### 3. Lógica de Sequenciamento Avançado (internal/tui/app.go)

```go
// MODIFICAR: applyNodePoolChanges para incluir cordon/drain
func (a *App) applyNodePoolChanges(nodePools []models.NodePool) tea.Cmd {
    return func() tea.Msg {
        ctx := context.Background()

        for _, pool := range nodePools {
            // Determinar se é scale up ou scale down
            isScaleDown := pool.NewNodeCount < pool.CurrentNodeCount ||
                           (pool.NewAutoscaling && pool.NewMaxNodes < pool.CurrentNodeCount)

            // Se é scale down E tem cordon/drain habilitado
            if isScaleDown && (pool.CordonEnabled || pool.DrainEnabled) {
                // FASE 1: Cordon
                if pool.CordonEnabled {
                    if err := a.cordonNodePool(ctx, &pool); err != nil {
                        return sequentialNodePoolCompletedMsg{
                            NodePoolName: pool.Name,
                            Order:        pool.SequenceOrder,
                            Success:      false,
                            Error:        fmt.Sprintf("Cordon failed: %v", err),
                        }
                    }
                }

                // FASE 2: Drain
                if pool.DrainEnabled {
                    if err := a.drainNodePool(ctx, &pool); err != nil {
                        return sequentialNodePoolCompletedMsg{
                            NodePoolName: pool.Name,
                            Order:        pool.SequenceOrder,
                            Success:      false,
                            Error:        fmt.Sprintf("Drain failed: %v", err),
                        }
                    }
                }
            }

            // FASE 3: Aplicar mudanças no node pool (Azure CLI)
            if err := a.executeNodePoolUpdate(ctx, &pool); err != nil {
                return sequentialNodePoolCompletedMsg{
                    NodePoolName: pool.Name,
                    Order:        pool.SequenceOrder,
                    Success:      false,
                    Error:        fmt.Sprintf("Update failed: %v", err),
                }
            }
        }

        return sequentialNodePoolCompletedMsg{
            NodePoolName: nodePools[0].Name,
            Order:        nodePools[0].SequenceOrder,
            Success:      true,
        }
    }
}

// NOVO: Cordon de todos os nodes de um node pool
func (a *App) cordonNodePool(ctx context.Context, pool *models.NodePool) error {
    a.debugLog("🔒 Cordoning nodes in node pool %s...", pool.Name)
    pool.CordonStatus = "cordoning"

    // Obter client K8s
    client, err := getClient(pool.Cluster)
    if err != nil {
        return err
    }

    // Listar nodes
    nodes, err := client.GetNodesInNodePool(ctx, pool.Name)
    if err != nil {
        return err
    }

    pool.NodesInNodePool = nodes

    // Cordon cada node
    for _, nodeName := range nodes {
        if err := client.CordonNode(ctx, nodeName); err != nil {
            pool.CordonStatus = "failed"
            return fmt.Errorf("failed to cordon %s: %w", nodeName, err)
        }
        a.debugLog("  ✅ Cordoned: %s", nodeName)
    }

    pool.CordonStatus = "cordoned"
    a.debugLog("✅ All nodes cordoned in %s", pool.Name)
    return nil
}

// NOVO: Drain de todos os nodes de um node pool
func (a *App) drainNodePool(ctx context.Context, pool *models.NodePool) error {
    a.debugLog("💧 Draining nodes in node pool %s...", pool.Name)
    pool.DrainStatus = "draining"

    client, err := getClient(pool.Cluster)
    if err != nil {
        return err
    }

    // Drain cada node
    for _, nodeName := range pool.NodesInNodePool {
        a.debugLog("  💧 Draining: %s", nodeName)

        if err := client.DrainNode(ctx, nodeName, pool.DrainTimeout, pool.DrainGracePeriod); err != nil {
            pool.DrainStatus = "failed"
            return fmt.Errorf("failed to drain %s: %w", nodeName, err)
        }

        // Aguardar node ser drenado completamente
        if err := a.waitForNodeDrained(ctx, client, nodeName, pool.DrainTimeout); err != nil {
            pool.DrainStatus = "failed"
            return fmt.Errorf("timeout waiting for %s to drain: %w", nodeName, err)
        }

        a.debugLog("  ✅ Drained: %s", nodeName)
    }

    pool.DrainStatus = "drained"
    a.debugLog("✅ All nodes drained in %s", pool.Name)
    return nil
}

// NOVO: Aguardar node ser drenado
func (a *App) waitForNodeDrained(ctx context.Context, client *kubernetes.Client, nodeName string, timeout int) error {
    deadline := time.Now().Add(time.Duration(timeout) * time.Second)

    for time.Now().Before(deadline) {
        drained, err := client.IsNodeDrained(ctx, nodeName)
        if err != nil {
            return err
        }

        if drained {
            return nil
        }

        time.Sleep(5 * time.Second) // Check a cada 5s
    }

    return fmt.Errorf("timeout after %ds", timeout)
}
```

### 4. Interface de Usuário (TUI)

**Modal de Configuração de Sequenciamento:**

```
┌─────────────────────────────────────────────────────────────────┐
│ Configuração de Sequenciamento - Node Pool: prometheus-np-small│
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Ordem de Execução:  (*) Primeiro (*1)  ( ) Segundo (*2)       │
│                                                                 │
│ ┌─ Opções Avançadas ────────────────────────────────────────┐  │
│ │                                                            │  │
│ │ [✓] Habilitar Cordon antes de scale down                 │  │
│ │     └─ Marca nodes como unschedulable                     │  │
│ │                                                            │  │
│ │ [✓] Habilitar Drain antes de scale down                  │  │
│ │     ├─ Timeout:        [300] segundos                     │  │
│ │     ├─ Grace Period:   [ 30] segundos                     │  │
│ │     ├─ Ignore DaemonSets: [✓]                            │  │
│ │     └─ Force delete EmptyDir: [✓]                        │  │
│ │                                                            │  │
│ └────────────────────────────────────────────────────────────┘  │
│                                                                 │
│ ⚠️  Recomendado para transições de node pools do Prometheus    │
│                                                                 │
│          [Cancelar]              [Salvar]                       │
└─────────────────────────────────────────────────────────────────┘
```

**Indicadores de Status durante Execução:**

```
┌─ Execução Sequencial ──────────────────────────────────────────┐
│                                                                 │
│ *1 prometheus-np-small                                         │
│    [████████████████████████████] 100%  ✅ Completed           │
│    └─ 🔒 Cordoned (3 nodes) → 💧 Drained → 📉 Scaled down     │
│                                                                 │
│ *2 prometheus-np-large                                         │
│    [████████████░░░░░░░░░░░░░░░░] 65%   ⏳ Draining...        │
│    └─ 📈 Scaled up → 🔒 Cordoning...                           │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 5. Interface Web

**NodePoolEditor.tsx - Adicionar Toggle:**

```typescript
// Novo estado
const [cordonEnabled, setCordonEnabled] = useState(false);
const [drainEnabled, setDrainEnabled] = useState(false);
const [drainTimeout, setDrainTimeout] = useState(300);
const [drainGracePeriod, setDrainGracePeriod] = useState(30);

// UI
<div className="space-y-3">
  <div className="flex items-center space-x-2">
    <Checkbox
      id="cordon"
      checked={cordonEnabled}
      onCheckedChange={setCordonEnabled}
    />
    <Label htmlFor="cordon">
      Habilitar Cordon antes de scale down
    </Label>
  </div>

  <div className="flex items-center space-x-2">
    <Checkbox
      id="drain"
      checked={drainEnabled}
      onCheckedChange={setDrainEnabled}
    />
    <Label htmlFor="drain">
      Habilitar Drain antes de scale down
    </Label>
  </div>

  {drainEnabled && (
    <div className="ml-6 space-y-2">
      <div className="grid grid-cols-2 gap-2">
        <div>
          <Label>Timeout (segundos)</Label>
          <Input
            type="number"
            value={drainTimeout}
            onChange={(e) => setDrainTimeout(parseInt(e.target.value))}
          />
        </div>
        <div>
          <Label>Grace Period (segundos)</Label>
          <Input
            type="number"
            value={drainGracePeriod}
            onChange={(e) => setDrainGracePeriod(parseInt(e.target.value))}
          />
        </div>
      </div>
    </div>
  )}
</div>
```

## 📝 Checklist de Implementação

### Backend (Go)

- [ ] 1. Adicionar campos cordon/drain no modelo NodePool (`internal/models/types.go`)
- [ ] 2. Implementar `GetNodesInNodePool()` (`internal/kubernetes/client.go`)
- [ ] 3. Implementar `CordonNode()` (`internal/kubernetes/client.go`)
- [ ] 4. Implementar `DrainNode()` (`internal/kubernetes/client.go`)
- [ ] 5. Implementar `IsNodeDrained()` (`internal/kubernetes/client.go`)
- [ ] 6. Modificar `applyNodePoolChanges()` com lógica cordon/drain (`internal/tui/app.go`)
- [ ] 7. Adicionar `cordonNodePool()` (`internal/tui/app.go`)
- [ ] 8. Adicionar `drainNodePool()` (`internal/tui/app.go`)
- [ ] 9. Adicionar `waitForNodeDrained()` (`internal/tui/app.go`)
- [ ] 10. Atualizar mensagens de progresso para incluir status cordon/drain

### Frontend Web (TypeScript/React)

- [ ] 11. Adicionar campos cordon/drain em `types.ts`
- [ ] 12. Adicionar toggles no `NodePoolEditor.tsx`
- [ ] 13. Atualizar `ApplyAllModal.tsx` para mostrar operações cordon/drain
- [ ] 14. Adicionar indicadores de progresso para cordon/drain
- [ ] 15. Atualizar sessões para salvar configurações cordon/drain

### Interface TUI

- [ ] 16. Criar modal de configuração de sequenciamento (`internal/tui/views.go`)
- [ ] 17. Adicionar handlers de teclado para abrir modal
- [ ] 18. Atualizar `renderHelp()` com novas teclas
- [ ] 19. Atualizar indicadores de status visual

### Testes

- [ ] 20. Testar cordon/drain com cluster de teste
- [ ] 21. Testar timeout e grace period
- [ ] 22. Testar com DaemonSets (devem ser ignorados)
- [ ] 23. Testar transição Prometheus normal → black friday
- [ ] 24. Verificar que pods não têm downtime

### Documentação

- [ ] 25. Atualizar CLAUDE.md com novo fluxo
- [ ] 26. Criar guia de uso para transições de Prometheus
- [ ] 27. Documentar flags de cordon/drain

## 🎯 Workflow Completo - Transição Prometheus

**Cenário**: Black Friday chegando, precisa aumentar recursos do Prometheus.

### Preparação (Via TUI ou Web):

```bash
1. Acessar Node Pools
2. Selecionar prometheus-np-large (Black Friday)
3. Configurar:
   - Min Nodes: 0 → 3
   - Max Nodes: 3 → 10
   - Autoscaling: Habilitado
4. Marcar como *1 (primeiro a executar)
5. Configurar opções avançadas:
   - ☐ Cordon (não precisa, é scale UP)
   - ☐ Drain (não precisa, é scale UP)

6. Selecionar prometheus-np-small (Normal)
7. Configurar:
   - Min Nodes: 3 → 0
   - Max Nodes: 10 → 0
8. Marcar como *2 (segundo a executar)
9. Configurar opções avançadas:
   - ✓ Cordon (marcar nodes como unschedulable)
   - ✓ Drain (mover pods gracefully)
   - Timeout: 300s
   - Grace Period: 30s

10. Ctrl+U - Aplicar
```

### Execução Automática:

```
[FASE 1] *1 prometheus-np-large
  ⏳ Scaling up: min=3, max=10
  ⏳ Aguardando nodes ficarem Ready...
  ✅ 3 nodes prontos (node-bf-1, node-bf-2, node-bf-3)
  ✅ Prometheus pods agendados nos novos nodes
  ✅ *1 COMPLETED

[FASE 2] *2 prometheus-np-small (automático)
  🔒 Cordoning nodes...
     ✅ node-small-1 cordoned
     ✅ node-small-2 cordoned
     ✅ node-small-3 cordoned
  💧 Draining nodes...
     ⏳ Draining node-small-1 (timeout: 300s)
        └─ Evicting prometheus-server-0 (grace: 30s)
        └─ Evicting prometheus-alertmanager-0 (grace: 30s)
     ✅ node-small-1 drained (45s)
     ⏳ Draining node-small-2...
     ✅ node-small-2 drained (38s)
     ⏳ Draining node-small-3...
     ✅ node-small-3 drained (42s)
  📉 Scaling down: min=0, max=0
  ⏳ Aguardando nodes serem removidos...
  ✅ All nodes removed
  ✅ *2 COMPLETED

✅ TRANSIÇÃO CONCLUÍDA SEM DOWNTIME
```

## 🚨 Casos de Erro e Tratamento

### 1. Timeout no Drain

```
Cenário: Pod com PDB muito restritivo não consegue ser evictado

Tratamento:
- Logar warning após 80% do timeout
- Sugerir verificar PodDisruptionBudgets
- Permitir force delete após timeout (opcional, configurável)
```

### 2. Node Pool de Destino Sem Capacidade

```
Cenário: Node pool BF não tem nodes suficientes para receber pods

Tratamento:
- Verificar nodes Ready antes de iniciar drain
- Aguardar autoscaler criar mais nodes se necessário
- Falhar com mensagem clara se capacidade insuficiente
```

### 3. Rollback de Emergência

```
Cenário: Algo dá errado durante transição

Solução:
- Botão "Cancelar e Reverter" durante execução
- Uncordon dos nodes que foram cordoned
- Re-scale do node pool original
```

## 💡 Melhorias Futuras

1. **Pre-flight checks**: Verificar capacidade antes de iniciar
2. **Dry-run mode**: Simular transição sem executar
3. **Health checks**: Verificar pods healthy após migração
4. **Notificações**: Alertas via webhook quando transição completa
5. **Templates**: Salvar configurações de transição (ex: "Transição Black Friday")

---

**Este plano está pronto para implementação incremental.**
