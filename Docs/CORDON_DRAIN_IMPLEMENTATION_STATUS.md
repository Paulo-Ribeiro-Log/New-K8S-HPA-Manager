# Status de Implementação: Cordon/Drain Node Pools

## ✅ Fases Concluídas

### ✅ Fase 1: Backend - Structs e Models

**Arquivo**: `internal/models/types.go`

**Implementado**:
- ✅ `NodePoolChanges` struct (autoscaling, nodeCount, minNodes, maxNodes)
- ✅ `DrainOptions` struct completa (todas as flags kubectl drain)
- ✅ Novos campos no `NodePool`:
  - `PreDrainChanges` - Mudanças ANTES do drain (scale UP destino)
  - `PostDrainChanges` - Mudanças DEPOIS do drain (scale DOWN origem)
  - `CordonEnabled`, `DrainEnabled`, `DrainOptions`
  - `CordonStatus`, `DrainStatus`, `NodesInPool`
- ✅ `DefaultDrainOptions()` - Configuração segura padrão
- ✅ `AggressiveDrainOptions()` - Configuração para emergências (Black Friday)

---

### ✅ Fase 2: Backend - Funções Kubernetes Client

**Arquivo**: `internal/kubernetes/client.go`

**Implementado**:
- ✅ `GetNodesInNodePool()` - Lista nodes por label agentpool
- ✅ `CordonNode()` - Marca node como unschedulable
- ✅ `UncordonNode()` - Marca node como schedulable
- ✅ `DrainNode()` - Remove pods com todas as opções kubectl drain
- ✅ `evictPod()` - Evict pod usando Eviction API ou DELETE
- ✅ `waitForPodsDeleted()` - Aguarda pods serem deletados com timeout
- ✅ `IsNodeDrained()` - Verifica se node está sem pods
- ✅ `ValidateDrainOptions()` - Valida todas as opções
- ✅ `ValidateTimeout()` - Valida formato de timeout (5m, 300s, 1h)
- ✅ `ValidatePodSelector()` - Valida label selector
- ✅ Helpers: `isDaemonSetPod()`, `hasController()`, `parseDuration()`

**Import adicionado**: `policyv1 "k8s.io/api/policy/v1"` para Eviction API

---

### ✅ Fase 3: TUI - Modal de Configuração

**Arquivo**: `internal/tui/components/sequence_config_modal.go`

**Implementado**:
- ✅ `SequenceConfigModal` struct completo
- ✅ `NewSequenceConfigModal()` - Cria modal com defaults
- ✅ `Render()` - Renderização ASCII com lipgloss
- ✅ `HandleKey()` - Navegação por teclado (Tab, Space, Enter, A)
- ✅ `ToDrainOptions()` - Converte configuração para DrainOptions
- ✅ Checkboxes para todas as opções (cordon, drain, flags)
- ✅ Campos de texto para grace period e timeout
- ✅ Accordion para opções avançadas (tecla 'A')
- ✅ Preview do fluxo de execução (5 fases)

**Features**:
- Navegação: Tab/Shift+Tab, Space (toggle), Enter (confirmar)
- Accordion: A (expandir/recolher avançadas)
- Validação inline
- Preview detalhado do fluxo

---

### ✅ Fase 4: Web - Componente React

**Arquivo**: `internal/web/frontend/src/components/NodePoolSequencingModal.tsx`

**Implementado**:
- ✅ `NodePoolSequencingModal` componente completo
- ✅ Interface `NodePoolSequence`, `DrainOptions`, `SequenceConfig`
- ✅ Checkboxes para todas as opções (shadcn/ui)
- ✅ Inputs para grace period, timeout e opções avançadas
- ✅ Accordion para opções avançadas (Button toggle)
- ✅ Validação inline de campos (regex timeout, range checks)
- ✅ Preview do fluxo de execução com badges (1️⃣-5️⃣)
- ✅ Alert de tempo estimado e aviso de downtime
- ✅ Erros de validação exibidos em Alert (destructive variant)

**Features**:
- Dialog com overflow-y-auto (max-h-90vh)
- Grid layout para campos (grace period e timeout)
- Badge de sequência (*1, *2)
- Footer com 3 botões (Cancelar, Validar, Executar)

---

## 🔄 Próximas Etapas (Fase 5)

### Integração com Execução Sequencial

**Arquivos a modificar**:

1. **TUI** (`internal/tui/app.go`):
   - Adicionar handler para abrir modal (ex: tecla 'C' quando node pools marcados)
   - Integrar modal com `executeSequentialNodePools()`
   - Implementar execução em fases:
     1. PRE-DRAIN: Aplicar `PreDrainChanges`
     2. AGUARDAR: Esperar nodes Ready (30s)
     3. CORDON: Cordon nodes origem
     4. DRAIN: Drain nodes origem → destino
     5. POST-DRAIN: Aplicar `PostDrainChanges`

2. **Web Backend** (`internal/web/handlers/nodepools.go`):
   - Endpoint `POST /api/v1/nodepools/sequence/execute`
   - Aceitar `SequenceConfig` no body
   - Validar configuração
   - Executar sequenciamento com progress tracking

3. **Web Frontend** (`internal/web/frontend/src/pages/Index.tsx`):
   - Integrar `NodePoolSequencingModal` na página de Node Pools
   - Botão "Configure Sequencing" quando 2 node pools marcados (*1 e *2)
   - Callback `onConfirm` chama API endpoint
   - Progress modal durante execução

---

## 📋 Checklist de Implementação

### Backend
- [x] Structs em `models/types.go`
- [x] Funções em `kubernetes/client.go`
- [x] Validações de DrainOptions
- [ ] Handler web `POST /api/v1/nodepools/sequence/execute`
- [ ] Lógica de execução sequencial com cordon/drain

### TUI
- [x] Modal de configuração (`sequence_config_modal.go`)
- [ ] Handler para abrir modal (tecla 'C')
- [ ] Integração com execução sequencial
- [ ] Progress tracking durante execução

### Web
- [x] Componente React (`NodePoolSequencingModal.tsx`)
- [ ] Integração com página Index.tsx
- [ ] Botão "Configure Sequencing"
- [ ] Callback para API
- [ ] Progress modal durante execução

---

## 🎯 Fluxo Correto de Execução

```
FASE 1: PRE-DRAIN
├─ Aplicar PreDrainChanges no destino (monitoring-bf)
│  └─ Autoscaling=ON, Min=1, Max=3
├─ Aguardar 30s para nodes ficarem Ready
└─ Logs: "✅ Destino pronto para receber pods"

FASE 2: CORDON
├─ GetNodesInNodePool(origem)
├─ Para cada node: CordonNode(nodeName)
└─ Logs: "✅ 3 nodes cordoned"

FASE 3: DRAIN
├─ Para cada node cordoned:
│  ├─ DrainNode(nodeName, drainOptions)
│  ├─ Flags aplicadas: --ignore-daemonsets, --delete-emptydir-data
│  ├─ Grace period: 30s
│  ├─ Timeout: 5m
│  └─ IsNodeDrained(nodeName) == true
└─ Logs: "✅ 3 nodes drained (15 pods migrados)"

FASE 4: POST-DRAIN
├─ Aplicar PostDrainChanges na origem (monitoring)
│  └─ Autoscaling=OFF, NodeCount=0
└─ Logs: "✅ Origem desligada"

FASE 5: FINALIZAÇÃO
├─ UncordonNode(destino) - se necessário
└─ Logs: "✅ Sequenciamento concluído (tempo: 7m12s)"
```

---

## 🧪 Testes Necessários

1. **Validações**:
   - [ ] Timeout format válido (5m, 300s, 1h)
   - [ ] Grace period >= 0
   - [ ] Chunk size >= 1
   - [ ] Drain requer Cordon habilitado

2. **Execução**:
   - [ ] Cordon funciona (nodes marcados como unschedulable)
   - [ ] Drain funciona (pods migrados corretamente)
   - [ ] Flags aplicadas corretamente (--ignore-daemonsets, etc.)
   - [ ] Timeout respeitado
   - [ ] Dry-run não executa alterações

3. **Edge Cases**:
   - [ ] Node sem pods (drain imediato)
   - [ ] Pods com PDBs (respeita Eviction API)
   - [ ] Pods standalone (--force necessário)
   - [ ] DaemonSets (--ignore-daemonsets)

---

## 📝 Documentação

- [x] `NODEPOOL_CORDON_DRAIN_PLAN.md` - Plano completo original
- [x] `NODEPOOL_CORDON_DRAIN_UI_DESIGN.md` - Design de UI/UX
- [x] `CORDON_DRAIN_IMPLEMENTATION_STATUS.md` - Este arquivo

---

**Última atualização:** 14 de novembro de 2025
**Status:** Fases 1-4 concluídas ✅ | Fase 5 em andamento 🔄
