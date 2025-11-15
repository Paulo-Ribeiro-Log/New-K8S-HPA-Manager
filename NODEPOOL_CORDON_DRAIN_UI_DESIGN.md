# Node Pool Cordon/Drain - UI/UX Design

## Visão Geral

Modal completo para configuração de operações de Cordon e Drain em Node Pools durante sequenciamento.

---

## Kubectl Drain - Flags Disponíveis

### Flags Essenciais (Mais Usadas)

| Flag | Descrição | Default | Comum? |
|------|-----------|---------|--------|
| `--ignore-daemonsets` | Ignora DaemonSets durante drain | false | ✅ SIM |
| `--delete-emptydir-data` | Deleta pods com volumes emptyDir | false | ✅ SIM |
| `--force` | Força remoção de pods não gerenciados por controller | false | ⚠️ Cuidado |
| `--grace-period` | Período de graça antes de forçar terminação (segundos) | 30 | ✅ SIM |
| `--timeout` | Timeout total da operação de drain (ex: 5m) | 0 (sem limite) | ✅ SIM |

### Flags Avançadas (Menos Comuns)

| Flag | Descrição | Default | Comum? |
|------|-----------|---------|--------|
| `--disable-eviction` | Usa DELETE ao invés de eviction API | false | Raro |
| `--skip-wait-for-delete-timeout` | Timeout para aguardar pod deletion (segundos) | 0 | Raro |
| `--pod-selector` | Label selector para filtrar pods | "" | Médio |
| `--dry-run` | Simula operação sem executar | false | Dev/Test |
| `--chunk-size` | Quantos nodes drenar em paralelo | 1 | Avançado |

---

## Design do Modal - TUI (Terminal)

### Layout Proposto

```
╔════════════════════════════════════════════════════════════════════╗
║              Node Pool Sequencing - Configuração                  ║
╠════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║ 📋 Node Pools Selecionados:                                       ║
║   *1: monitoring       (autoscaling → manual, count → 0)          ║
║   *2: monitoring-bf    (manual → autoscaling, min=1, max=3)       ║
║                                                                    ║
║ ──────────────────────────────────────────────────────────────── ║
║                                                                    ║
║ ⚙️  Operações de Transição:                                       ║
║                                                                    ║
║   [✓] Habilitar Cordon                                            ║
║       └─ Marca nodes como unschedulable antes do drain            ║
║                                                                    ║
║   [✓] Habilitar Drain                                             ║
║       └─ Remove pods gracefully e os migra para destino           ║
║                                                                    ║
║ ──────────────────────────────────────────────────────────────── ║
║                                                                    ║
║ 🔧 Opções de Drain (kubectl drain flags):                         ║
║                                                                    ║
║   ESSENCIAIS:                                                     ║
║   [✓] --ignore-daemonsets                                         ║
║       Ignora DaemonSets (recomendado)                             ║
║                                                                    ║
║   [✓] --delete-emptydir-data                                      ║
║       Permite deletar pods com volumes emptyDir                   ║
║                                                                    ║
║   [ ] --force                                                     ║
║       ⚠️  Força remoção de pods standalone (use com cuidado!)    ║
║                                                                    ║
║   Grace Period: [30____] segundos                                 ║
║       Tempo de espera antes de forçar terminação                  ║
║                                                                    ║
║   Timeout: [5m____] (ex: 5m, 300s, 10m)                          ║
║       Timeout total da operação                                   ║
║                                                                    ║
║   AVANÇADAS (pressione 'A' para expandir):                        ║
║   ▶ Mostrar opções avançadas...                                   ║
║                                                                    ║
║ ──────────────────────────────────────────────────────────────── ║
║                                                                    ║
║ 📊 Fluxo de Execução:                                             ║
║                                                                    ║
║   1️⃣  FASE PRE-DRAIN                                              ║
║       Ajustar monitoring-bf (destino) para receber pods           ║
║       → Min=1, Max=3, Autoscaling=ON                              ║
║                                                                    ║
║   2️⃣  AGUARDAR NODES READY (30s)                                  ║
║       Aguardar nodes do destino ficarem Ready                     ║
║                                                                    ║
║   3️⃣  CORDON                                                       ║
║       Marcar nodes do monitoring (origem) como unschedulable      ║
║                                                                    ║
║   4️⃣  DRAIN                                                        ║
║       Migrar pods de monitoring → monitoring-bf                   ║
║       Com flags: --ignore-daemonsets --delete-emptydir-data       ║
║                                                                    ║
║   5️⃣  FASE POST-DRAIN                                             ║
║       Ajustar monitoring (origem) para desligar                   ║
║       → Autoscaling=OFF, NodeCount=0                              ║
║                                                                    ║
║ ──────────────────────────────────────────────────────────────── ║
║                                                                    ║
║  [Cancelar (Esc)]   [Validar (Ctrl+V)]   [Executar (Enter)]     ║
║                                                                    ║
╚════════════════════════════════════════════════════════════════════╝
```

### Opções Avançadas (Expandidas com 'A')

```
║   AVANÇADAS:                                                      ║
║   ▼ Mostrar opções avançadas                                      ║
║                                                                    ║
║   [ ] --disable-eviction                                          ║
║       Usa DELETE ao invés de Eviction API (não respeita PDBs)    ║
║                                                                    ║
║   Skip Wait Timeout: [20____] segundos                            ║
║       Timeout para aguardar deleção de pods                       ║
║                                                                    ║
║   Pod Selector: [_________________________________]                ║
║       Label selector (ex: app=nginx,tier!=frontend)              ║
║                                                                    ║
║   [ ] --dry-run                                                   ║
║       Simular operação sem executar                               ║
║                                                                    ║
║   Chunk Size: [1____] nodes                                       ║
║       Quantos nodes drenar em paralelo                            ║
```

---

## Design do Modal - Web Interface

### Layout Proposto (React Component)

```typescript
// NodePoolSequencingModal.tsx

interface Props {
  nodePools: NodePoolSequence[];  // *1 e *2
  onConfirm: (config: SequenceConfig) => void;
  onCancel: () => void;
}

interface SequenceConfig {
  cordonEnabled: boolean;
  drainEnabled: boolean;
  drainOptions: DrainOptions;
}

interface DrainOptions {
  // Essenciais
  ignoreDaemonsets: boolean;
  deleteEmptyDirData: boolean;
  force: boolean;
  gracePeriod: number;      // segundos
  timeout: string;          // "5m", "300s", etc.

  // Avançadas
  disableEviction: boolean;
  skipWaitForDeleteTimeout: number;  // segundos
  podSelector: string;      // label selector
  dryRun: boolean;
  chunkSize: number;
}
```

### Visual Mockup (HTML/CSS)

```html
<div class="modal">
  <!-- Header -->
  <div class="modal-header">
    <h2>⚙️ Node Pool Sequencing - Configuração</h2>
    <button class="close-btn">×</button>
  </div>

  <!-- Body -->
  <div class="modal-body">
    <!-- Node Pools Selecionados -->
    <section class="section">
      <h3>📋 Node Pools Selecionados</h3>
      <div class="node-pool-card">
        <span class="sequence-badge">*1</span>
        <span class="name">monitoring</span>
        <span class="changes">autoscaling → manual, count → 0</span>
      </div>
      <div class="node-pool-card">
        <span class="sequence-badge">*2</span>
        <span class="name">monitoring-bf</span>
        <span class="changes">manual → autoscaling, min=1, max=3</span>
      </div>
    </section>

    <hr />

    <!-- Operações de Transição -->
    <section class="section">
      <h3>⚙️ Operações de Transição</h3>

      <label class="checkbox-label">
        <input type="checkbox" checked />
        <span class="label-text">Habilitar Cordon</span>
        <small class="help-text">
          Marca nodes como unschedulable antes do drain
        </small>
      </label>

      <label class="checkbox-label">
        <input type="checkbox" checked />
        <span class="label-text">Habilitar Drain</span>
        <small class="help-text">
          Remove pods gracefully e os migra para destino
        </small>
      </label>
    </section>

    <hr />

    <!-- Opções de Drain -->
    <section class="section">
      <h3>🔧 Opções de Drain</h3>

      <!-- Essenciais -->
      <div class="subsection">
        <h4>Essenciais</h4>

        <label class="checkbox-label">
          <input type="checkbox" checked />
          <span class="label-text">--ignore-daemonsets</span>
          <small class="help-text">Ignora DaemonSets (recomendado)</small>
        </label>

        <label class="checkbox-label">
          <input type="checkbox" checked />
          <span class="label-text">--delete-emptydir-data</span>
          <small class="help-text">
            Permite deletar pods com volumes emptyDir
          </small>
        </label>

        <label class="checkbox-label warning">
          <input type="checkbox" />
          <span class="label-text">--force</span>
          <small class="help-text">
            ⚠️ Força remoção de pods standalone (use com cuidado!)
          </small>
        </label>

        <div class="input-group">
          <label>Grace Period</label>
          <input type="number" value="30" min="0" />
          <span class="unit">segundos</span>
          <small class="help-text">
            Tempo de espera antes de forçar terminação
          </small>
        </div>

        <div class="input-group">
          <label>Timeout</label>
          <input type="text" value="5m" placeholder="5m, 300s, 10m" />
          <small class="help-text">Timeout total da operação</small>
        </div>
      </div>

      <!-- Avançadas (Accordion) -->
      <details class="accordion">
        <summary>Avançadas (clique para expandir)</summary>

        <label class="checkbox-label">
          <input type="checkbox" />
          <span class="label-text">--disable-eviction</span>
          <small class="help-text">
            Usa DELETE ao invés de Eviction API (não respeita PDBs)
          </small>
        </label>

        <div class="input-group">
          <label>Skip Wait Timeout</label>
          <input type="number" value="20" min="0" />
          <span class="unit">segundos</span>
          <small class="help-text">
            Timeout para aguardar deleção de pods
          </small>
        </div>

        <div class="input-group">
          <label>Pod Selector</label>
          <input
            type="text"
            placeholder="app=nginx,tier!=frontend"
            value=""
          />
          <small class="help-text">
            Label selector para filtrar pods
          </small>
        </div>

        <label class="checkbox-label">
          <input type="checkbox" />
          <span class="label-text">--dry-run</span>
          <small class="help-text">Simular operação sem executar</small>
        </label>

        <div class="input-group">
          <label>Chunk Size</label>
          <input type="number" value="1" min="1" />
          <span class="unit">nodes</span>
          <small class="help-text">
            Quantos nodes drenar em paralelo
          </small>
        </div>
      </details>
    </section>

    <hr />

    <!-- Fluxo de Execução (Preview) -->
    <section class="section">
      <h3>📊 Fluxo de Execução</h3>

      <div class="execution-flow">
        <div class="step">
          <span class="step-number">1️⃣</span>
          <div class="step-content">
            <strong>FASE PRE-DRAIN</strong>
            <p>
              Ajustar monitoring-bf (destino) para receber pods
              <br />→ Min=1, Max=3, Autoscaling=ON
            </p>
          </div>
        </div>

        <div class="step">
          <span class="step-number">2️⃣</span>
          <div class="step-content">
            <strong>AGUARDAR NODES READY (30s)</strong>
            <p>Aguardar nodes do destino ficarem Ready</p>
          </div>
        </div>

        <div class="step">
          <span class="step-number">3️⃣</span>
          <div class="step-content">
            <strong>CORDON</strong>
            <p>Marcar nodes do monitoring (origem) como unschedulable</p>
          </div>
        </div>

        <div class="step">
          <span class="step-number">4️⃣</span>
          <div class="step-content">
            <strong>DRAIN</strong>
            <p>
              Migrar pods de monitoring → monitoring-bf
              <br />Com flags: --ignore-daemonsets --delete-emptydir-data
            </p>
          </div>
        </div>

        <div class="step">
          <span class="step-number">5️⃣</span>
          <div class="step-content">
            <strong>FASE POST-DRAIN</strong>
            <p>
              Ajustar monitoring (origem) para desligar
              <br />→ Autoscaling=OFF, NodeCount=0
            </p>
          </div>
        </div>
      </div>
    </section>
  </div>

  <!-- Footer -->
  <div class="modal-footer">
    <button class="btn btn-secondary" onclick="onCancel()">
      Cancelar
    </button>
    <button class="btn btn-warning" onclick="onValidate()">
      🔍 Validar Configuração
    </button>
    <button class="btn btn-primary" onclick="onConfirm()">
      ✅ Executar Sequenciamento
    </button>
  </div>
</div>
```

---

## Defaults Recomendados

### Configuração Segura (Padrão)

```go
type DrainOptions struct {
    // Essenciais - RECOMENDADOS
    IgnoreDaemonsets:     true,   // ✅ Sempre ignorar DaemonSets
    DeleteEmptyDirData:   true,   // ✅ Permitir volumes emptyDir
    Force:                false,  // ❌ NÃO forçar por padrão (perigoso)
    GracePeriod:          30,     // 30 segundos (padrão K8s)
    Timeout:              "5m",   // 5 minutos (suficiente para maioria)

    // Avançadas - DESABILITADAS
    DisableEviction:      false,  // Respeitar PDBs
    SkipWaitForDeleteTimeout: 20, // 20 segundos
    PodSelector:          "",     // Sem filtro
    DryRun:               false,  // Executar de verdade
    ChunkSize:            1,      // 1 node por vez (seguro)
}
```

### Configuração Agressiva (Black Friday - Downtime Mínimo)

```go
type DrainOptions struct {
    IgnoreDaemonsets:     true,
    DeleteEmptyDirData:   true,
    Force:                true,   // ⚠️ Forçar remoção
    GracePeriod:          10,     // Reduzir para 10s
    Timeout:              "2m",   // Timeout agressivo

    DisableEviction:      false,  // Ainda respeitar PDBs
    SkipWaitForDeleteTimeout: 10,
    PodSelector:          "",
    DryRun:               false,
    ChunkSize:            2,      // Drenar 2 nodes em paralelo
}
```

---

## Validações e Feedback

### Validações Necessárias

1. **Pelo menos 2 node pools selecionados** (*1 e *2)
2. **Se Drain habilitado, Cordon deve estar habilitado também**
   - Não faz sentido drenar sem cordon primeiro
3. **Grace Period ≥ 0**
4. **Timeout válido** (regex: `^\d+[smh]$`)
5. **Chunk Size ≥ 1**
6. **Pod Selector válido** (se fornecido - label selector syntax)

### Mensagens de Erro

```
❌ Erro: Drain requer Cordon habilitado
💡 Habilite o Cordon antes de ativar o Drain

❌ Erro: Timeout inválido
💡 Use formato: 5m, 300s, 1h

❌ Erro: Apenas 1 node pool selecionado
💡 Selecione pelo menos 2 node pools (*1 e *2)

⚠️  Aviso: --force pode remover pods standalone
💡 Use apenas se souber o que está fazendo
```

### Confirmação Antes de Executar

```
╔═══════════════════════════════════════════════════════════════╗
║                  ⚠️  CONFIRMAR EXECUÇÃO                       ║
╠═══════════════════════════════════════════════════════════════╣
║                                                               ║
║  Você está prestes a executar o sequenciamento:              ║
║                                                               ║
║  *1: monitoring       → Cordon + Drain → Desligar            ║
║  *2: monitoring-bf    → Ligar → Receber pods                 ║
║                                                               ║
║  Opções de Drain:                                            ║
║    • --ignore-daemonsets                                     ║
║    • --delete-emptydir-data                                  ║
║    • --grace-period=30                                       ║
║    • --timeout=5m                                            ║
║                                                               ║
║  ⏱️  Tempo estimado: ~7 minutos                              ║
║                                                               ║
║  ⚠️  Esta operação pode causar downtime temporário se        ║
║     o destino não tiver capacidade suficiente.               ║
║                                                               ║
║  Deseja continuar?                                           ║
║                                                               ║
║  [Não (Esc)]                    [Sim, Executar (Enter)]     ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
```

---

## Fluxo de Interação - TUI

### Teclado (Navegação)

| Tecla | Ação |
|-------|------|
| `Tab` | Próximo campo |
| `Shift+Tab` | Campo anterior |
| `Space` | Toggle checkbox |
| `Enter` | Confirmar valor / Executar |
| `Esc` | Cancelar |
| `A` | Expandir/Recolher opções avançadas |
| `Ctrl+V` | Validar configuração |
| `?` | Mostrar ajuda inline |

### Estados Visuais

**Checkbox Marcado:**
```
[✓] --ignore-daemonsets
```

**Checkbox Desmarcado:**
```
[ ] --force
```

**Campo com Erro:**
```
Timeout: [xxx___] ❌ Formato inválido
         └─ Use: 5m, 300s, 1h
```

**Campo Válido:**
```
Timeout: [5m____] ✅
```

---

## Fluxo de Interação - Web

### Estados Visuais (CSS Classes)

```css
/* Checkbox padrão */
.checkbox-label {
  padding: 12px;
  border: 1px solid #e0e0e0;
  border-radius: 4px;
  cursor: pointer;
}

.checkbox-label:hover {
  background-color: #f5f5f5;
}

/* Checkbox marcado */
.checkbox-label input:checked + .label-text {
  font-weight: bold;
  color: #1976d2;
}

/* Aviso (--force) */
.checkbox-label.warning {
  border-color: #ff9800;
  background-color: #fff3e0;
}

.checkbox-label.warning .help-text {
  color: #e65100;
}

/* Input com erro */
.input-group.error input {
  border-color: #d32f2f;
}

.input-group.error .help-text {
  color: #d32f2f;
}

/* Input válido */
.input-group.valid input {
  border-color: #388e3c;
}
```

### Loading State (Durante Execução)

```html
<div class="modal-overlay loading">
  <div class="loading-content">
    <div class="spinner"></div>
    <h3>Executando Sequenciamento...</h3>

    <div class="progress-steps">
      <div class="step completed">
        ✅ FASE PRE-DRAIN - monitoring-bf ajustado
      </div>
      <div class="step in-progress">
        ⏳ AGUARDANDO NODES READY (12s restantes)
      </div>
      <div class="step pending">
        CORDON - monitoring
      </div>
      <div class="step pending">
        DRAIN - monitoring → monitoring-bf
      </div>
      <div class="step pending">
        FASE POST-DRAIN - monitoring desligado
      </div>
    </div>

    <button class="btn btn-danger" onclick="cancelExecution()">
      ⛔ Cancelar Execução
    </button>
  </div>
</div>
```

---

## Estrutura de Dados (Backend)

### Go Structs Atualizados

```go
// internal/models/types.go

type NodePoolSequenceConfig struct {
    NodePools []NodePoolWithSequence `json:"node_pools"`

    // Opções de operação
    CordonEnabled bool `json:"cordon_enabled"`
    DrainEnabled  bool `json:"drain_enabled"`

    // Opções de drain
    DrainOptions DrainOptions `json:"drain_options"`
}

type DrainOptions struct {
    // Essenciais
    IgnoreDaemonsets   bool   `json:"ignore_daemonsets"`
    DeleteEmptyDirData bool   `json:"delete_emptydir_data"`
    Force              bool   `json:"force"`
    GracePeriod        int    `json:"grace_period"`        // segundos
    Timeout            string `json:"timeout"`             // "5m", "300s"

    // Avançadas
    DisableEviction          bool   `json:"disable_eviction"`
    SkipWaitForDeleteTimeout int    `json:"skip_wait_timeout"`  // segundos
    PodSelector              string `json:"pod_selector"`
    DryRun                   bool   `json:"dry_run"`
    ChunkSize                int    `json:"chunk_size"`
}

// Defaults
func DefaultDrainOptions() DrainOptions {
    return DrainOptions{
        IgnoreDaemonsets:         true,
        DeleteEmptyDirData:       true,
        Force:                    false,
        GracePeriod:              30,
        Timeout:                  "5m",
        DisableEviction:          false,
        SkipWaitForDeleteTimeout: 20,
        PodSelector:              "",
        DryRun:                   false,
        ChunkSize:                1,
    }
}
```

---

## Implementação - Ordem de Tarefas

### Fase 1: Backend (Go)
1. ✅ Atualizar structs em `internal/models/types.go`
2. ✅ Adicionar `DrainOptions` com defaults
3. ✅ Criar funções de validação em `internal/kubernetes/client.go`:
   - `ValidateDrainOptions(opts DrainOptions) error`
   - `ValidateTimeout(timeout string) error`
   - `ValidatePodSelector(selector string) error`

### Fase 2: TUI (Terminal)
1. ✅ Criar modal de configuração em `internal/tui/components/sequence_config_modal.go`
2. ✅ Integrar com `handlers.go` (tecla para abrir modal)
3. ✅ Adicionar validações inline
4. ✅ Conectar com execução sequencial

### Fase 3: Web (React)
1. ✅ Criar componente `NodePoolSequencingModal.tsx`
2. ✅ Criar componente `DrainOptionsForm.tsx` (reutilizável)
3. ✅ Integrar com `Index.tsx`
4. ✅ Adicionar API endpoint `POST /api/v1/nodepools/sequence/config`

### Fase 4: Testes
1. ✅ Testar validações (inputs inválidos)
2. ✅ Testar fluxo completo (mock de execução)
3. ✅ Testar dry-run
4. ✅ Testar com clusters reais (homologação)

---

## Próximos Passos Imediatos

1. **Revisar este documento** com o usuário
2. **Aprovar design do modal** (TUI e Web)
3. **Definir defaults finais** (seguro vs agressivo)
4. **Implementar Fase 1** (Backend - structs e validações)

---

## Perguntas Pendentes

1. **Timeout default**: 5 minutos é suficiente? Ou prefere 10 minutos?
2. **Chunk size**: Permitir drenar múltiplos nodes em paralelo? Ou sempre 1 por vez?
3. **Force flag**: Deve ter confirmação extra antes de habilitar?
4. **Pod selector**: Deve ter sugestões de labels comuns (ex: app=prometheus)?
5. **Dry-run**: Deve mostrar preview do que SERIA executado?

---

**Autor:** Claude Code
**Data:** 14 de novembro de 2025
**Status:** 🟡 Aguardando aprovação do usuário
