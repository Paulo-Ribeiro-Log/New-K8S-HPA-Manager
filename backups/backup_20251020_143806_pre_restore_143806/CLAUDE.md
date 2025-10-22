# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).

**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/k8s-hpa-manager` para executar a aplicação.

## 💬 Continuing Development in New Chat Sessions

When starting a new Claude Code chat session to continue development of this project, provide this context:

**Project Summary**: Terminal-based Kubernetes HPA and Azure AKS Node Pool management tool built with Go and Bubble Tea TUI framework. Features async rollout progress tracking with Rich Python-style progress bars, integrated status panel, session management, and unified HPA/node pool operations.

**Key Recent Features**:
- **Rich Python-Style Progress Bars**: Real-time progress tracking com barras elegantes (━ preenchido, ╌ vazio) e cores dinâmicas (vermelho→laranja→amarelo→verde)
- **Integrated Status Panel**: Unified "📊 Status e Informações" panel (140x15) com mensagens, progress bars e scroll
- **Bottom-Up Display**: Novos itens aparecem sempre na última linha (leitura de baixo para cima)
- **Complete Rollout Lifecycle**: Mensagens de início, progress bar em tempo real, e mensagens de conclusão (sucesso ✅ ou falha ❌ com motivo)
- **Multiple Rollout Types**: Support for Deployment, DaemonSet, and StatefulSet rollouts com toggles individuais
- **Thread-Safe Operations**: Mutex-protected rollout progress updates with auto-cleanup após 3 segundos
- **Visual Application Indicators**: ● symbols showing HPA application count with timestamps

**Current State (Janeiro 2025)**: A aplicação possui layout completo com execução sequencial de node pools para stress tests, rollouts detalhados de HPA (Deployment/DaemonSet/StatefulSet), correções críticas de bugs de parsing, funcionalidade de CronJob management, e sistema de status container com quebra de linhas inteligente.

## 🆕 Melhorias Recentes (Janeiro 2025)

### 🔍 Auto-Descoberta Inteligente de Clusters (NOVO - Outubro 2025)
- **Descoberta Automática**: Comando `k8s-hpa-manager autodiscover` extrai automaticamente resource groups e subscriptions
- **Extração do Kubeconfig**: Lê resource group direto do campo `user` (formato: `clusterAdmin_{RG}_{CLUSTER}`)
- **Integração Azure CLI**: Descobre subscription via Azure CLI automaticamente
- **Escalável**: Ideal para ambientes com 26, 70+ clusters - não precisa mais configurar manualmente
- **Atualização Inteligente**: Mescla com configurações existentes, preserva dados customizados
- **Workflow Simplificado**:
  1. Adicionar clusters ao kubeconfig: `az aks get-credentials --name CLUSTER --resource-group RG`
  2. Executar auto-descoberta: `k8s-hpa-manager autodiscover`
  3. Arquivo `~/.k8s-hpa-manager/clusters-config.json` gerado/atualizado automaticamente
  4. Node Pools prontos para uso na aplicação
- **Casos de Uso**:
  - Configuração inicial de múltiplos clusters
  - Onboarding de novos analistas (não precisa configurar manualmente)
  - Atualizar após mudanças de subscriptions
  - Regenerar configuração após rotação de credenciais

### 💾 Salvamento Manual de Sessões para Rollback (NOVO - Janeiro 2025)
- **Salvar Sem Modificações**: Ctrl+S agora permite salvar sessões a qualquer momento, mesmo sem alterações
- **Sessões de Rollback**: Crie backups de configurações atuais para restauração rápida em caso de problemas
- **Funciona em Todos os Modos**:
  - HPAs: Salve sessões de HPAs sem modificações (Ctrl+S na tela HPAs Selecionados)
  - Node Pools: Salve sessões de Node Pools sem modificações (Ctrl+S na tela Node Pools Selecionados)
  - Sessões Mistas: Salve sessões combinadas (HPAs + Node Pools) sem modificações
- **Workflow Típico**:
  1. Carregar sessão existente (Ctrl+L)
  2. Pressionar Ctrl+S imediatamente (sem modificações)
  3. Dar nome como "rollback-producao-2025-01-10"
  4. Sessão de backup criada e pronta para uso futuro
- **Casos de Uso**:
  - Criar snapshots antes de mudanças críticas em produção
  - Versionar configurações (v1, v2, v3)
  - Duplicar sessões para testes em diferentes ambientes
  - Manter histórico de configurações conhecidas e estáveis

### 🔄 Execução Sequencial de Node Pools (NOVO)
- **Sistema de Marcação**: Ctrl+M marca até 2 node pools para execução sequencial
- **Indicadores Visuais**: `*1`, `*2` mostram ordem de execução no painel
- **Execução Automática**: Primeiro pool manual, segundo inicia automaticamente após conclusão
- **Stress Tests**: Ideal para cenários monitoring-1 → 0 nodes, monitoring-2 → scale up
- **Persistência Completa**: Dados de sequência salvos e restaurados em sessões
- **Logs Detalhados**: Debug tracking completo do processo sequencial

### 🎯 CronJob Management (NOVO)
- **Acesso via F9**: Gerenciamento completo de CronJobs do cluster selecionado
- **Operações CRUD**: Habilitar/desabilitar suspend status dos CronJobs
- **Status em Tempo Real**: 🟢 Ativo, 🔴 Suspenso, 🟡 Falhou, 🔵 Executando
- **Informações Detalhadas**: Schedule, última execução, job template
- **Seleção Múltipla**: Ctrl+D individual, Ctrl+U batch operations
- **Interface Responsiva**: Auto-scroll e navegação inteligente

### 🎨 Sistema de Progress Bars Rich Python (NOVO)
- **Integração Completa**: Progress bars totalmente funcionais no Status Container com estilo Rich Python
- **Caracteres Finos**: Usa ━ (preenchido) e ╌ (vazio) para barras elegantes de altura única
- **Cores Dinâmicas**: Sistema de cores baseado no progresso:
  - 0-24%: 🔴 Vermelho (#EF4444) - Inicial/crítico
  - 25-49%: 🟠 Laranja (#F59E0B) - Progredindo
  - 50-74%: 🟡 Amarelo (#EAB308) - Meio caminho
  - 75-99%: 🟢 Verde claro (#84CC16) - Quase lá
  - 100%: ✅ Verde completo (#10B981) - Concluído
- **Largura Compacta**: 30 caracteres de barra + porcentagem + título = ~130 colunas (cabe em 1 linha)
- **Bottom-Up**: Novos itens sempre aparecem na última linha do container
- **Lifecycle Completo**:
  - Início: `ℹ️ rollout: 🚀 Iniciando rollout deployment para nginx-ingress-controller/ingress-nginx`
  - Progresso: `━━━━━━━━━╌╌╌╌╌╌╌╌╌╌ 25% 🔄 Aplicando mudanças...`
  - Sucesso: `✅ rollout: ✅ Rollout deployment concluído: nginx-ingress-controller/ingress-nginx`
  - Falha: `❌ rollout: ❌ Rollout deployment falhou: nginx-ingress-controller/ingress-nginx - [motivo]`
- **Auto-cleanup**: Progress bars removidas automaticamente 3 segundos após conclusão
- **Scrollable**: Container de status com scroll para histórico completo

### 🔧 Correções Críticas de Bugs
- **MinReplicas Parsing**: Corrigido bug que mostrava endereços de memória (824643808920) em vez de valores reais
- **Função getIntValue()**: Readicionada para tratar ponteiros *int32 com segurança
- **Rollouts em Sessões**: DaemonSet e StatefulSet rollouts agora salvos e restaurados corretamente
- **Exibição Detalhada**: Painel HPAs Selecionados mostra todos os 3 tipos de rollout
- **Espaçamento CronJob**: Corrigido alinhamento do painel Status e Informações
- **Container Status Auto-contido**: Implementado sistema de quebra de linhas inteligente
- **Seleção de Texto Terminal**: Removido `tea.WithMouseCellMotion()` para permitir seleção normal de texto
- **Dimensões Status Panel**: Mantidas dimensões fixas 140x15 conforme especificação original
- **Variation Selectors em Emojis**: Corrigido bug crítico de alinhamento causado por caracteres invisíveis (U+FE0F) que vinham junto com emojis, adicionando 2 colunas extras. Implementada função `removeVariationSelectors()` que remove esses caracteres antes da renderização, garantindo exatamente 140 colunas visuais
- **Progress Bar Integration**: Sistema completo de progress bars integrado ao StatusContainer com interface unificada

### Sistema de Interface Limpo
- **Layout sem moldura**: Interface limpa e direta focada no conteúdo
- **Seleção por cor azul**: Sistema visual consistente com fundo azul (#00ADD8) para itens selecionados
- **Scroll universal**: Funcionalidade de scroll implementada em todos os painéis que precisam
- **Auto-scroll inteligente**: Item selecionado sempre permanece visível durante navegação
- **Limites de terminal**: Mínimo 120x35 para garantir boa experiência de uso

### Node Pool Autoscaling Corrigido
- **Aplicação de Autoscaling**: Corrigida função `updateNodePoolViaAzureCLI` para aplicar mudanças no `AutoscalingEnabled`
- **Comandos Azure CLI**:
  - `--enable-cluster-autoscaler` quando habilitando autoscaling
  - `--disable-cluster-autoscaler` quando desabilitando autoscaling
  - `--update-cluster-autoscaler` quando atualizando min/max existente
- **Exibição de Progresso**: Corrigida para mostrar operação correta ([manual], [autoscale], [scale])

### Layout Responsivo de Node Pools
- **Parâmetros Idênticos aos HPAs**: Node Pools agora têm exatamente os mesmos parâmetros responsivos que HPAs
- **Scroll Inteligente**: Funcionalidade completa de scroll com Shift+Up/Down e mouse wheel
- **Indicadores de Aplicação**: Sistema ● para mostrar quantas vezes Node Pool foi aplicado
- **Auto-scroll**: Item selecionado sempre permanece visível

### Campos e Funcionalidades Adicionados
- **NodePoolSelectedScrollOffset**: Campo de scroll para Node Pools no modelo
- **AppliedCount**: Contador de aplicações para Node Pools (igual aos HPAs)
- **Funções de Scroll**: `calculateSelectedNodePoolLinePosition()`, `adjustNodePoolScrollToKeepItemVisible()`

## Development Commands

### Building and Running
- `make build` - Build the binary to `build/k8s-hpa-manager`
- `make build-all` - Build for multiple platforms (Linux, macOS, Windows)
- `make run` - Build and run the application
- `make run-dev` - Run in development mode with debug logging (`go run . --debug`)

### Installation and Distribution
- `./install.sh` - **Automated installer script** - builds and installs globally to `/usr/local/bin/`
- `./uninstall.sh` - **Automated uninstaller script** - removes binary and optionally session data
- After installation: `k8s-hpa-manager` command available globally from any directory

### Testing and Quality
- `make test` - Run all tests with verbose output (`go test -v ./...`)
- `make test-coverage` - Run tests with coverage report, generates `coverage.out` and `coverage.html`

### Dependencies
- No dedicated `make deps` target - use `go mod download` or `go mod tidy` directly

### Cluster Auto-Discovery (NOVO)
- `k8s-hpa-manager autodiscover` - **Auto-descobre todos os clusters** do kubeconfig
  - Extrai resource groups automaticamente do campo `user`
  - Descobre subscriptions via Azure CLI
  - Gera/atualiza `~/.k8s-hpa-manager/clusters-config.json`
  - Escalável para 26, 70+ clusters
  - Uso: `k8s-hpa-manager autodiscover`
  - Help: `k8s-hpa-manager autodiscover --help`

### Backup and Restore (NOVO)
- `./backup.sh [descrição]` - **Criar backup** do código antes de modificações
  - Backup automático com timestamp
  - Metadados inclusos (git commit, data, usuário)
  - Mantém os 10 backups mais recentes automaticamente
  - Exemplo: `./backup.sh "antes_da_feature_X"`
- `./restore.sh [nome_backup]` - **Restaurar backup** anterior
  - Lista backups disponíveis se executado sem argumentos
  - Cria backup do estado atual antes de restaurar
  - Recompila automaticamente após restauração
  - Confirmação de segurança antes de sobrescrever
  - Exemplo: `./restore.sh backup_20251001_125658_before_autodiscovery_feature`

### Direct Execution

**After Global Installation:**
- `k8s-hpa-manager` - Run with default kubeconfig from any directory
- `k8s-hpa-manager --kubeconfig /path/to/config` - Use custom kubeconfig
- `k8s-hpa-manager --debug` - Enable debug logging
- `k8s-hpa-manager --help` - Show help and available options

**Local Development (before installation):**
- `./build/k8s-hpa-manager` - Run with default kubeconfig
- `./build/k8s-hpa-manager --kubeconfig /path/to/config` - Use custom kubeconfig
- `./build/k8s-hpa-manager --debug` - Enable debug logging

## Architecture Overview

This is a terminal-based Kubernetes HPA (Horizontal Pod Autoscaler) and Azure AKS Node Pool management tool built with Go, using the Bubble Tea TUI framework. The application provides an interactive interface for managing HPAs across multiple Kubernetes clusters and AKS node pools with integrated Azure CLI support.

### Core Components

**TUI Layer** (`internal/tui/`):
- `app.go` - Main application orchestrator using Bubble Tea framework with centralized text editing methods
- `views.go` - UI rendering and layout logic with intelligent cursor display
- `handlers.go` - User input and event handling using centralized text input system
- `message.go` - Bubble Tea message definitions for state management
- `text_input.go` - **NEW**: Centralized text input module with intelligent cursor that shows overlaid characters
- `resource_handlers.go` - Specialized handlers for HPA and node pool resource editing
- `resource_views.go` - Resource-specific view rendering

**Business Logic** (`internal/`):
- `kubernetes/client.go` - Kubernetes API client wrapper for HPA operations
- `config/kubeconfig.go` - Kubeconfig discovery and cluster connection management
- `session/manager.go` - Session persistence and template-based naming system
- `models/types.go` - Complete domain model definitions and application state
- `azure/auth.go` - Azure SDK authentication manager with transparent login flow

**Entry Points**:
- `main.go` - Application bootstrap
- `cmd/root.go` - Cobra CLI command definitions and flag handling

### Key Features

#### Kubernetes HPA Management
1. **Cluster Discovery**: Automatically discovers clusters with `akspriv-*` naming pattern
2. **Single Cluster Selection**: Select one cluster at a time with automatic context entry
3. **Multiple Namespace Selection**: Select multiple namespaces with visual indicators and toggle functionality
4. **System Namespace Filtering**: Automatically filters out system namespaces with `S` key toggle option
5. **Async HPA Counting**: Fast namespace loading with background HPA counting for better performance
6. **Multi-HPA Selection**: Select multiple HPAs within chosen namespaces for batch operations
7. **Live Editing**: Modify HPA min/max replica values, CPU/memory targets with real-time validation
8. **Rollout Integration**: Toggle rollout execution per HPA with Space key in edit mode

#### Azure AKS Node Pool Management
9. **Node Pool Discovery**: Automatically loads node pools from Azure AKS clusters using `clusters-config.json`
10. **Azure Authentication**: Transparent Azure CLI authentication with browser and device code fallback
11. **Subscription Management**: Automatic Azure subscription configuration from cluster config
12. **Node Pool Editing**: Modify node count, min/max node count, and autoscaler settings
13. **Real-time Application**: Apply node pool changes via Azure CLI with progress feedback
14. **Filtered JSON Output**: Clean, relevant output from Azure CLI operations

#### Session & Workflow Management
15. **Unified Session Management**: Save/restore HPA and node pool configurations
16. **Mixed Sessions**: Combine HPA and node pool modifications in single sessions (Ctrl+M)
17. **Session Type Detection**: Automatic detection of HPA vs node pool vs mixed sessions
18. **State-Preserving Session Loading**: Load sessions to review/edit before applying changes
19. **Template-based Naming**: Session naming with variables like `{action}_{cluster}_{timestamp}`
20. **Session Name Display**: Persistent display of loaded session name across all interface screens
21. **Rollout State Persistence**: HPA rollout toggle settings are saved and restored with sessions
22. **Manual Rollback Sessions**: Save sessions without modifications (Ctrl+S) for creating backups and rollback points

#### Progress Tracking & Status Management
22. **Async Rollout Progress Bars**: Real-time progress tracking for deployment/daemonset/statefulset rollouts with Rich Python-style Unicode bars (━ and ╌)
23. **Integrated Status Panel**: Unified "📊 Status e Informações" panel displaying success/error messages, progress bars, and rollout status
24. **Multiple Rollout Support**: Simultaneous execution of deployment, daemonset, and statefulset rollouts with individual progress tracking
25. **Thread-Safe Progress Updates**: Mutex-protected rollout progress management with auto-cleanup after completion
26. **Visual Application Indicators**: ● symbols with counters showing HPA application history and timestamps

#### User Interface & Experience
27. **Comprehensive Help System**: Context-sensitive help accessible via `?` key with scroll navigation
28. **Cluster Connectivity Testing**: Real-time cluster connection status with visual indicators
29. **Individual & Batch Operations**: Apply changes individually (Ctrl+D) or in batch (Ctrl+U)
30. **Error Recovery**: ESC key allows returning from error states instead of forced exit
31. **Multi-panel Interface**: TAB navigation between HPAs and node pools in mixed sessions, with namespace-grouped HPA display
32. **Non-Blocking Status**: Messages and progress bars appear in dedicated panel without interrupting workflow
33. **Automatic Subscription Switching**: Auto-configures Azure subscription when switching clusters
34. **Token Expiration Handling**: Automatic re-authentication when Azure tokens expire

### Data Flow

The application follows a state-driven architecture:
1. `AppModel` in `models/types.go` maintains complete application state
2. State transitions are managed through `AppState` enum (Cluster Selection → Session Selection → Namespace Selection → HPA Management → Editing → Help)
3. Multi-selection flow: One Cluster → Multiple Namespaces → Multiple HPAs → Individual Editing
4. Bubble Tea messages coordinate between UI interactions and business logic
5. Kubernetes operations are abstracted through the `Client` wrapper with per-cluster client management
6. Session system preserves state and allows restoration without immediate application

### Session System

Sessions use template-based naming with variables like `{action}_{cluster}_{timestamp}` and are persisted as JSON in `~/.k8s-hpa-manager/sessions/`. The system supports rollback capabilities by maintaining original HPA values.

### Dependencies

#### Core Framework
- **Bubble Tea**: TUI framework for interactive terminal interfaces
- **Lipgloss**: Styling and layout for terminal UI with custom border title implementation
- **Cobra**: CLI command framework
- Go 1.23+ required for the project (toolchain 1.24.7)

#### Kubernetes Integration
- **client-go**: Official Kubernetes Go client library for HPA management

#### Azure Integration
- **Azure SDK for Go**: `github.com/Azure/azure-sdk-for-go/sdk/azcore` and `azidentity` for authentication
- **Azure Container Service SDK**: `armcontainerservice` for AKS node pool management
- **Azure CLI**: External dependency for node pool operations (automatic installation verification)

### Custom Border Implementation

Since the current Lipgloss version (1.1.0) doesn't include native BorderTitle support, the application implements a custom `renderPanelWithTitle()` function that:

- **Auto-calculates panel width** based on content and title requirements
- **Draws custom Unicode borders** (╭─╮│╰╯) with integrated titles
- **Handles Unicode characters safely** using rune conversion for proper character boundaries
- **Applies selective coloring** to border elements while preserving content styling
- **Centers titles dynamically** within the top border with proper padding

**Panel Examples:**
```
╭─────── Clusters Kubernetes ───────╮
│  ✅ akspriv-dev-central          │
│  ⏳ akspriv-prod-east            │
│  ❌ akspriv-test-west            │
╰───────────────────────────────────╯

╭──── Namespaces Disponíveis ────╮
│  🎯 api-services (5 HPAs)      │
│  ✓ web-frontend (2 HPAs)       │
│  ❌ database (0 HPAs)          │
╰─────────────────────────────────╯
```

## Installation Guide

### Automated Installation (Recommended)

The project includes automated installation scripts for easy deployment:

**Quick Installation:**
```bash
# Clone or navigate to project directory
cd path/to/k8s-hpa-manager

# Run the installer
./install.sh
```

**What the installer does:**
- ✅ Verifies Go installation and project structure
- ✅ Builds the binary using make (or go build as fallback)
- ✅ Installs to `/usr/local/bin/` for global access
- ✅ Sets proper execution permissions
- ✅ Tests the installation
- ✅ Provides usage instructions

**After installation, use from anywhere:**
```bash
k8s-hpa-manager              # Start the application
k8s-hpa-manager --help       # Show help
k8s-hpa-manager --debug      # Debug mode
```

### Manual Installation

If you prefer manual installation:

```bash
# Build the binary
make build

# Copy to system path (requires sudo)
sudo cp build/k8s-hpa-manager /usr/local/bin/
sudo chmod +x /usr/local/bin/k8s-hpa-manager

# Verify installation
k8s-hpa-manager --help
```

### Uninstallation

To remove the application:

```bash
# Run the uninstaller
./uninstall.sh

# Or manually remove
sudo rm /usr/local/bin/k8s-hpa-manager
rm -rf ~/.k8s-hpa-manager  # Optional: remove session data
```

## User Interface & Controls

### Navigation Controls
- **Arrow Keys / vi-keys (hjkl)**: Navigate through lists and menus
- **Tab**: Switch between panels in multi-panel views
- **Space**: Select/deselect items (namespaces, HPAs)
- **Enter**: Confirm selection or edit item
- **ESC**: Go back/cancel current operation or clear error messages
- **F4**: Exit application
- **?**: Show comprehensive help screen with scroll navigation

### Cluster & Session Management
- **Ctrl+L**: Load saved session from cluster selection screen
- **F5/R**: Reload cluster list in cluster selection screen
- **Ctrl+S**: Save current session from HPA/node pool selection screen (funciona MESMO SEM MODIFICAÇÕES - perfeito para criar sessões de rollback)
- **Ctrl+M**: Create mixed session (HPAs + node pools combined)
- **💾 Rollback Workflow**:
  1. Carregar sessão existente (Ctrl+L)
  2. Pressionar Ctrl+S imediatamente (sem fazer modificações)
  3. Nomear como "rollback-[ambiente]-[data]"
  4. Sessão de backup criada para uso futuro

### HPA Operations
- **Ctrl+D**: Apply individual selected HPA (works multiple times, shows ● counter)
- **Ctrl+U**: Apply all selected HPAs in batch (works multiple times, shows ● counters)
- **Space** (in edit mode): Toggle rollout options (Deployment/DaemonSet/StatefulSet)
- **↑↓**: Navigate through all rollout fields in HPA editing interface

### Node Pool Operations
- **Ctrl+N**: Access node pool management for selected cluster
- **Ctrl+D/Ctrl+U**: Apply node pool changes via Azure CLI
- **Space**: Select/deselect node pools for modification
- **Enter**: Edit node pool parameters (node count, min/max, autoscaling toggle)
- **Space** (in edit mode): Toggle autoscaling enable/disable with dynamic field visibility
- **Ctrl+M**: Mark/unmark node pool for sequential execution (stress tests)

### Node Pool Sequential Execution (NOVO)
- **Ctrl+M**: Mark up to 2 node pools for sequential execution (shows *1, *2)
- **Visual Indicators**: `*1` first pool (manual execution), `*2` second pool (auto execution)
- **Manual First**: Execute first pool manually with Ctrl+D/Ctrl+U (e.g., scale to 0)
- **Auto Second**: System automatically starts second pool when first completes
- **Session Persistence**: Sequential markings saved and restored with sessions
- **Stress Test Workflow**: Perfect for monitoring-1 → 0 nodes, monitoring-2 → scale up

### Mixed Session Operations
- **Ctrl+M**: Start mixed session workflow
- **Tab**: Switch between HPA and node pool panels
- **Ctrl+S**: Save combined HPA and node pool modifications
- **Ctrl+D/Ctrl+U**: Apply all changes (HPAs + node pools) in unified operation

### Progress Tracking & Status
- **📊 Status Panel**: Integrated panel shows success messages, errors, and progress bars without blocking workflow
- **Real-time Progress**: Async rollout progress bars update automatically during deployment/daemonset/statefulset operations
- **Auto-cleanup**: Status messages clear after 5 seconds, progress bars removed after rollout completion

### Scroll Controls (Janeiro 2025)
- **Shift+Up/Down**: Scroll em painéis responsivos (HPAs Selecionados, Node Pools Selecionados, Status)
- **Mouse Wheel**: Scroll alternativo para painéis responsivos
- **Auto-scroll Inteligente**: Item selecionado sempre permanece visível durante navegação
- **Indicadores de Scroll**: `[5-15/45]` mostram posição atual e total quando scroll está ativo
- **Scroll Context-Aware**: Controles funcionam apenas no painel ativo atual
- **Multiple Applications**: ● indicators track how many times each HPA was applied in current session

### CronJob Management (F9)
- **F9** (from cluster selection): Access CronJob management for selected cluster
- **↑↓ / k j**: Navigate through CronJobs list
- **Space**: Select/deselect CronJob for batch operations
- **Enter**: Edit individual CronJob (enable/disable suspend status)
- **Ctrl+D**: Apply changes to selected CronJobs individually
- **Ctrl+U**: Apply changes to all selected CronJobs in batch
- **Ctrl+S** (in edit mode): Save changes and return to list
- **ESC**: Return to cluster selection or cancel editing
- **Real-time Status Reading**: Displays current schedule, last execution time, status, and job template
- **Schedule Description**: Converts cron expressions to human-readable format (e.g., "0 2 * * * - executa todo dia às 2:00 AM")
- **Status Indicators**: 🟢 Active, 🔴 Suspended, 🟡 Failed, 🔵 Running
- **Automatic Refresh**: Status updates after applying changes

### Execução Sequencial de Node Pools (NOVO)
- **Ctrl+M** (em Node Pools Selecionados): Marcar node pools para execução sequencial (máximo 2)
- **Indicadores visuais**: Node pools marcados mostram `*1` (primeiro) e `*2` (segundo)
- **Execução manual**: Primeiro node pool deve ser executado manualmente com Ctrl+D/Ctrl+U
- **Execução automática**: Segundo node pool inicia automaticamente quando primeiro completar
- **Persistência**: Marcações sequenciais são salvas e restauradas em sessões
- **Status tracking**: pending → executing → completed → failed
- **Uso em stress tests**: Permite escalar primeiro pool para 0 (liberar recursos) e depois iniciar segundo pool automaticamente

### Correções Críticas de Bugs (NOVO)
- **MinReplicas corrigido**: HPAs não mostram mais endereços de memória (ex: 824643808920), agora exibem valores corretos
- **Rollout completo**: HPAs Selecionados agora exibe status detalhado: "Rollout: Deployment:✅ DaemonSet:❌ StatefulSet:✅"
- **Sessões HPA**: DaemonSet e StatefulSet rollouts agora são corretamente salvos E restaurados ao carregar sessões
- **Resolução mínima**: Terminal limitado a mínimo 188x45 para garantir que todos elementos permaneçam visíveis
- **ESC navigation**: Tecla ESC funciona corretamente em todas as telas (CronJob, Session folders, etc.)
- **Spacing CronJob**: Telas de edição de CronJob agora têm espaçamento correto

### Special Features
- **S** (in namespace selection): Toggle system namespace visibility
- **F9** (from cluster selection): Access CronJob management for selected cluster
- **Help Screen Navigation**: ↑↓/kj for line scroll, PgUp/PgDn for page scroll, Home/End for extremes

## Recent Improvements & Bug Fixes

### UI/UX Improvements & Bug Fixes (Latest - September 2024)
- **Progress Bar System Overhaul**: Complete redesign of progress tracking for node pools
  - **Fixed duplicated progress bars**: Automatic cleanup of previous operations before starting new ones
  - **Smooth progress progression**: Progress now flows 5% → 15% → 25% → 30%-90% → 95% → 100%
  - **Enhanced granularity**: Multiple progress steps during Azure CLI command execution
  - **Faster cleanup**: Progress bars auto-remove after 5 seconds (reduced from 30 seconds)
  - **Visual improvements**: Changed "1-2 nodes" display to "1→2 nodes" for clarity
- **Node Pool Screen Refresh Fix**: Resolved screen fragment issues in node pool editing
  - Added missing status panel to node pool editing screen
  - All error/success messages now display properly with automatic cleanup
  - Fixed screen refresh to prevent UI element fragments
  - Implemented automatic message clearing after 5 seconds with screen refresh
- **Universal Message Auto-Clear**: Enhanced message management across all screens
  - All success/error messages now auto-clear after 5 seconds
  - Automatic screen refresh when messages are cleared to prevent artifacts
  - Fixed missing `clearStatusMessages()` calls in session operations
  - Consistent message handling for node pools, sessions, and HPA operations

### Cluster Management Enhancements (September 2024)
- **Reload Functionality**: Added cluster reload capability in cluster selection screen
  - `F5` or `R` key reloads the cluster list
  - Automatically resets selection to first cluster
  - Shows loading state during cluster discovery
  - Useful for refreshing cluster status or discovering new clusters
  - Updated help documentation and UI guidance

### HPA Application System & Rollout Enhancements (December 2024)
- **New Rollout Options**: Added DaemonSet and StatefulSet rollout toggles to HPA editing
  - Three rollout toggles now available: Deployment, DaemonSet, StatefulSet
  - All rollout options are saved in session JSON for complete state persistence
  - Space key toggles any rollout option during HPA editing
  - All rollout settings are preserved when loading sessions
- **Application Indicators**: Subtle visual feedback system for applied configurations
  - `●` indicator appears after successful application (Ctrl+D or Ctrl+U)
  - Counter shows multiple applications: `●2`, `●3`, etc. for repeated applications
  - Counters reset automatically on new session or cluster change
  - Perfect for tracking rollouts and debugging repeated applications
- **Fixed Ctrl+U Batch Application**: Corrected critical bug preventing batch HPA application
  - Ctrl+U now properly applies all selected HPAs regardless of modification status
  - Ctrl+D also applies individual HPAs without requiring modification flag
  - Enables multiple rollout executions and configuration re-applications
  - Supports debugging workflows and repeated configuration enforcement
- **Enhanced Navigation**: Fixed navigation to reach new DaemonSet and StatefulSet rollout fields
  - All rollout toggles now accessible via arrow key navigation in HPA editing
  - Consistent Space key behavior across all rollout options
  - Proper field exclusion from Enter key (rollouts use Space, not Enter)
- **HPA Organization**: Implemented namespace grouping in Selected HPAs panel
  - HPAs automatically grouped by namespace with visual separators: `───────── namespace ─────────`
  - Eliminates namespace redundancy - shown only in group headers
  - Maintains all functionality: navigation, selection, indicators, and editing
  - Significantly improves visual organization for multi-namespace selections

### Node Pool Safety & Session Management Fixes (December 2024)
- **Critical Safety Fix**: Completely removed dangerous `scale_method` field that could cause catastrophic node deletion
  - Replaced with safe `AutoscalingEnabled` boolean toggle (defaults to `true`)
  - When `false`: Shows only editable "Node Count" field for manual scaling
  - When `true`: Shows all autoscaling fields (Min/Max/Current node counts)
  - Dynamic UI: Fields appear/disappear instantly based on autoscaling state
- **Session Saving System Unified**: Fixed critical bug where node pool sessions weren't saving properly
  - Removed obsolete `saveNodePoolSession()` function that lacked folder support and metadata
  - Unified all session saving to use single `saveSession()` function with complete feature set
  - Now supports folder organization (Node-Upscale, Node-Downscale) with full metadata generation
- **Navigation Flow Fixes**: Corrected session saving navigation to return to appropriate screens
  - Node pool sessions now correctly return to "Node Pools Selected" screen (not HPAs)
  - System intelligently detects session type and routes to correct interface
  - Preserves workflow continuity for editing multiple node pools and saving multiple sessions
- **Enhanced Session Persistence**: Node pool sessions now include complete cluster context and metadata
  - Proper `clusters_affected` tracking for multi-cluster environments
  - Complete rollback data preservation with original values
  - Session type detection for proper loading and restoration

### HPA Editing Panel Fixes (December 2024)
- **Fixed Dual Panel System**: HPA editing now correctly displays two side-by-side panels
  - **Left Panel**: Min/Max replicas, CPU/Memory targets, Rollout settings
  - **Right Panel**: Deployment CPU/Memory requests and limits
- **TAB Navigation Fixed**: TAB now properly switches between HPA main panel and deployment resources panel
- **Deployment Resource Loading**: Fixed critical bug where deployment resources weren't loaded
  - Added `EnrichHPAWithDeploymentResources` call to `ListHPAs` function
  - Now automatically loads CPU Request/Limit and Memory Request/Limit from associated deployment
- **Complete Panel Navigation**:
  - ↑↓ navigation within each panel with circular wrapping
  - Visual indicators show which panel is active (blue border)
  - All deployment resource fields are now editable
- **Error Handling**: Graceful handling when deployment resources can't be loaded (shows warning, continues with other HPAs)

### Text Input System Refactoring (December 2024)
- **Centralized Text Input Architecture**: Complete refactoring of all text input handling into a single, reusable module (`text_input.go`)
- **Intelligent Cursor System**: Revolutionary cursor that **shows the character being overlaid** instead of hiding it
  - Visual modes: Colored highlighting (primary) and bracket delimiters (fallback)
  - Example: Editing "hello" → "hel**[l]**o" (shows the 'l' being overlaid with yellow background)
  - Smart space handling: Spaces shown as "·" or "▓" for visibility
- **Eliminated Code Duplication**: Removed ~200+ lines of duplicate text editing logic scattered across multiple files
- **Enhanced Keyboard Navigation**: Consistent behavior across ALL text inputs with advanced shortcuts:
  - `Ctrl+U` - Clear from cursor to beginning
  - `Ctrl+K` - Clear from cursor to end
  - `Home/End` - Jump to beginning/end
  - `Left/Right` - Precise cursor navigation with visual feedback
- **Universal Application**: All text inputs now use the same robust system:
  - Session naming, HPA value editing, Node pool parameters, Session renaming, Field editing
- **Professional UX**: Text editing experience now comparable to modern editors with precise visual feedback

### Session Management & Authentication Enhancements
- **Session Name Display**: Persistent visual indicator showing the currently loaded session name across all interface screens
- **Rollout State Persistence**: HPA rollout toggle settings are now properly saved and restored with session files
- **Automatic Azure Token Refresh**: Intelligent detection and automatic re-authentication when Azure CLI tokens expire
- **Subscription Auto-Switching**: Automatic Azure subscription configuration when switching between clusters with different subscriptions
- **Enhanced Error Recovery**: Improved retry logic for Azure authentication failures with tenant-specific login

### Azure AKS Node Pool Management
- **Full Node Pool Integration**: Complete CRUD operations for AKS node pools via Azure CLI
- **Transparent Azure Authentication**: Uses Azure SDK with browser login + device code fallback
- **Cluster Configuration Integration**: Automatic detection and loading from `clusters-config.json`
- **Subscription Management**: Automatic Azure subscription configuration per cluster
- **Progress Feedback**: Real-time progress indicators during Azure CLI operations
- **Filtered Output**: Clean, relevant JSON output filtering from Azure CLI responses
- **Mixed Sessions**: Unified sessions combining HPA and node pool modifications (Ctrl+M)

### Session System Enhancements
- **Session Type Detection**: Automatic detection of HPA vs node pool vs mixed sessions
- **Unified Session Loading**: Load any session type with appropriate state restoration
- **Mixed Session Workflow**: Complete interface for managing HPAs and node pools together
- **Template Variable Expansion**: Enhanced session naming with cluster, environment, and timestamp variables

### Azure Authentication Improvements
- **SDK-based Authentication**: Replaced `az login` with native Azure SDK authentication
- **Multiple Auth Methods**: Interactive browser flow with device code fallback
- **No More Subscription Lists**: Eliminates unwanted Azure subscription selection prompts
- **Transparent Login Flow**: Seamless authentication without user interruption

### Session Loading Enhancement
- Sessions now restore application state for review/editing instead of immediate application
- Maintains cluster context, namespace selections, and HPA modifications
- Automatic Kubernetes client setup for session cluster

### Individual Application Fix
- Fixed bug where Ctrl+D would prevent subsequent individual applications
- Now properly tracks which HPAs have been applied vs. still modified
- Ctrl+U continues to work after individual applications

### Error Handling Improvement
- ESC key now allows returning from error states instead of forced application exit
- Preserves user context and selections when recovering from errors

### Help System
- Comprehensive scrollable help accessible via ? key from any screen
- Context-aware navigation instructions
- Visual indicator reference guide

### Interface Polish
- **Custom Border Titles**: All panels now feature integrated titles in borders (like Python's Rich library)
- **Auto-sizing Panels**: Borders automatically adjust to content width for perfect alignment
- **Unicode-safe Rendering**: Proper handling of border characters (╭─╮│╰╯) with consistent coloring
- Real-time cluster connectivity testing with visual status indicators
- HPA count display for namespaces
- Modified state indicators (✨) for changed HPAs
- Rollout status display in HPA lists

## Important Implementation Notes

### System Namespaces Filter
The application automatically filters out system namespaces including: `kube-system`, `istio-system`, `cert-manager`, `gatekeeper-system`, `monitoring`, `prometheus`, `grafana`, `flux-system`, `argocd`, and many others. Full list in `internal/kubernetes/client.go:systemNamespaces`.

### Cluster Pattern Matching
Only discovers clusters with names starting with `akspriv-*` pattern from kubeconfig contexts.

### Session Storage
Sessions are stored as JSON files in `~/.k8s-hpa-manager/sessions/` with organized subfolders:
- `HPA-Upscale/` - Sessions for scaling up HPA resources
- `HPA-Downscale/` - Sessions for scaling down HPA resources
- `Node-Upscale/` - Sessions for scaling up AKS node pools
- `Node-Downscale/` - Sessions for scaling down AKS node pools

The system includes automatic cleanup of old autosave files (keeps only 5 most recent).

### Template Variables
Session naming supports these template variables:
- `{action}` - Custom action name
- `{cluster}` - Primary cluster name
- `{env}` - Environment extracted from cluster name (dev/prod/staging/test)
- `{timestamp}` - Format: dd-mm-yy_hh:mm:ss
- `{date}` - Format: dd-mm-yy
- `{time}` - Format: hh:mm:ss
- `{user}` - Current system user
- `{hpa_count}` - Number of HPAs in the session

## Troubleshooting

### Common Issues

**Installation Issues:**
- **"Go not found"**: Install Go from https://golang.org/dl/ and ensure it's in PATH
- **Permission denied during installation**: Script requires sudo for `/usr/local/bin/` access
- **Binary not found after installation**: Restart terminal or check that `/usr/local/bin/` is in PATH
- **"k8s-hpa-manager: command not found"**: Run `which k8s-hpa-manager` to verify installation location

**Cluster shows as offline/error status:**
- Verify cluster connectivity: `kubectl cluster-info --context=<cluster-name>`
- Check kubeconfig file permissions and validity
- Ensure cluster credentials haven't expired

**"Client not found for cluster" error:**
- This was a known bug fixed in recent versions
- If still occurring, restart application and try again
- Verify cluster name matches exactly in kubeconfig

**HPAs not loading for namespace:**
- Check if namespace has any HPAs: `kubectl get hpa -n <namespace>`
- Verify RBAC permissions for HPA resources
- System namespaces are filtered by default - use 'S' to toggle

**Session loading doesn't apply changes:**
- This is intentional behavior - sessions restore state for review
- Use Ctrl+D (individual) or Ctrl+U (batch) to apply after loading
- Check that cluster context is properly loaded

**Help screen too large for terminal:**
- Use ↑↓ or k/j keys to scroll through help content
- PgUp/PgDn for faster navigation
- Home/End to jump to beginning/end

### Error Recovery
- **ESC key**: Returns from any error state while preserving context
- **F4**: Force exit application
- **?**: Access help from any screen for guidance

### Performance Tips
- System namespace filtering improves loading speed
- Background HPA counting reduces perceived wait times
- Session system preserves work between application runs

## Recent Enhancements & New Features (2025)

### 📐 Layout Responsivo e Espaçamento Universal (Janeiro 2025)

A aplicação agora possui **layout completamente unificado** com espaçamento consistente:

#### **Limitação de Resolução Terminal:**
- **42 linhas x 185 colunas**: Limitação implementada para garantir que todos os elementos permaneçam sempre visíveis
- **Função `applyTerminalSizeLimit()`**: Intercepta redimensionamento do terminal automaticamente
- **Layout controlado**: Elimina problemas de elementos desaparecendo ou ficando cortados
- **UX consistente**: Mesma experiência independente do tamanho do terminal do usuário

#### **Espaçamento Dinâmico Universal:**
- **Posição fixa do Status Panel**: Mantém mesma posição em TODAS as telas (Lista HPAs, Lista Node Pools, Edição HPA, Edição Node Pool)
- **Cálculo inteligente**: `calculateEditingPanelSpacing()` para telas de edição, funções específicas para listas
- **Referência única**: Todos os cálculos baseados em 20 linhas de referência para consistência
- **Transições suaves**: Status panel nunca "pula" ao navegar entre telas

#### **Padronização de Painéis:**
- **Tamanho padrão**: Todos os painéis fixos agora usam 70x18 (largura x altura)
- **Responsividade preservada**: Painéis que precisam de scroll mantêm funcionalidade dinâmica
- **Consistência visual**: Layout uniforme em todas as funcionalidades

#### **Node Pool Responsivo Completo:**
- **Parâmetros idênticos aos HPAs**: Largura baseada no conteúdo, altura máxima 35 linhas
- **Scroll inteligente**: Shift+Up/Down e mouse wheel funcionam perfeitamente
- **Auto-scroll**: Item selecionado sempre permanece visível
- **Indicadores visuais**: `[5-15/45]` e contadores de aplicação `●2`, `●3`

### 🔧 Node Pool Autoscaling Totalmente Corrigido (Janeiro 2025)

Funcionalidade de autoscaling para Node Pools agora **100% funcional**:

#### **Implementação de Comandos Azure CLI:**
- **Habilitar autoscaling**: `az aks nodepool update --enable-cluster-autoscaler`
- **Desabilitar autoscaling**: `az aks nodepool update --disable-cluster-autoscaler`
- **Atualizar min/max**: `az aks nodepool update --update-cluster-autoscaler`

#### **Exibição de Progresso Corrigida:**
- **[manual]**: Quando desabilitando autoscaling + definindo nodes fixos
- **[autoscale]**: Quando habilitando autoscaling + definindo min/max
- **[scale]**: Quando alterando node count manualmente
- **Valores corretos**: Mostra `0→1 nodes` em vez de `1→2 nodes` incorreto

#### **AppliedCount para Node Pools:**
- **Indicadores visuais**: Sistema `●` igual aos HPAs
- **Contador de aplicações**: Rastreia quantas vezes Node Pool foi aplicado
- **Persistência**: Contador mantido durante sessão

### 🎯 Advanced Text Editing with Cursor Navigation

The application now features **professional-grade text editing** capabilities across all input fields:

#### **Cursor Navigation:**
- **←/→ (h/l)**: Move cursor character by character
- **Home/Ctrl+A**: Jump to beginning of text
- **End/Ctrl+E**: Jump to end of text
- **↑/↓**: Quick navigation to start/end (context-dependent)

#### **Precise Text Editing:**
- **Backspace**: Delete character before cursor
- **Delete**: Delete character at cursor position
- **Ctrl+U**: Delete from cursor to beginning of line
- **Ctrl+K**: Delete from cursor to end of line
- **Ctrl+W**: Delete previous word
- **Character insertion**: Type anywhere in text, characters insert at cursor position

#### **Visual Cursor Indicator:**
- The **█** symbol shows exactly where you're editing
- Cursor position is maintained during navigation
- Real-time visual feedback during text operations

#### **Practical Example:**
To fix "1278Gb" → "128Gb":
1. Enter edit mode (press Enter on field)
2. Use → to navigate to the "7"
3. Press Delete to remove only the "7"
4. Result: "128Gb" ✅

### 🔧 Enhanced Session Management

#### **Session Renaming with Cursor:**
- **Trigger**: Press `Ctrl+N` or `F2` on any saved session
- **Full cursor editing**: Navigate and edit session names precisely
- **Visual feedback**: Live cursor display during editing
- **Subfolder support**: Works seamlessly with HPA-Upscale/HPA-Downscale/Node-Upscale/Node-Downscale folders

#### **Session Organization:**
- **Subfolders**: HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale for better organization
- **Deletion**: `Ctrl+R` with confirmation dialog
- **Template naming**: Support for variables like `{action}_{cluster}_{timestamp}`
- **Mixed sessions**: Combine HPA and node pool modifications (`Ctrl+M`)

### 🛠️ Technical Implementation Details

#### **Text Editing Architecture:**
- **Unified helper functions**: `handleTextEditingKeys()`, `insertCursorInText()`, `validateCursorPosition()`
- **Unicode-safe**: Proper handling of multi-byte characters using Go's `[]rune`
- **Context-aware**: Callbacks system for save/cancel operations
- **Applied everywhere**: HPA fields, Node Pool fields, Session names, Cluster resources

#### **Error Handling Improvements:**
- **Compilation fixes**: Resolved function closure bugs, duplicate declarations
- **Method corrections**: Fixed undefined methods (`applyHPAChange` → `applyHPAChanges`)
- **Syntax validation**: All text editing functions properly validated

## Continuing Development in Future Chats

### 🚀 Context for Next Claude Sessions

When continuing development of this project in future chats, provide this context:

#### **Project Overview:**
This is a **terminal-based Kubernetes HPA and Azure AKS Node Pool management tool** built with:
- **Language**: Go 1.23+ (toolchain 1.24.7)
- **TUI Framework**: Bubble Tea + Lipgloss
- **Architecture**: MVC pattern with `internal/tui/` (views, handlers, app), `internal/models/`, `internal/session/`
- **Key Features**: Multi-cluster HPA management, Azure node pools, session persistence, subfolder organization

#### **Recent Major Work Completed:**
1. **Advanced text editing with cursor navigation** - fully implemented across all modules
2. **Session renaming functionality** - working with visual cursor feedback
3. **Compilation error fixes** - all syntax and method errors resolved
4. **Enhanced help system** - updated with new text editing features

#### **Current State:**
- ✅ **Compiles successfully**: `make build` works without errors
- ✅ **Text editing**: Professional-grade editing in all input fields
- ✅ **Session management**: Full CRUD with subfolders and renaming
- ✅ **Help system**: Comprehensive documentation accessible via `?` key

### 🔧 Common Development Commands

**For any future Claude sessions working on this project:**

```bash
# Build and test
make build                    # Build to ./build/k8s-hpa-manager
make run                      # Build and run
make run-dev                  # Run with debug logging
make test                     # Run tests with verbose output

# Installation
./install.sh                  # Install globally to /usr/local/bin/
./uninstall.sh               # Remove installation

# After installation
k8s-hpa-manager              # Run from anywhere
k8s-hpa-manager --debug      # Debug mode
k8s-hpa-manager --help       # Show help
```

### 📁 Key File Structure

```
├── cmd/root.go              # CLI entry point & flags
├── main.go                  # Application bootstrap
├── internal/
│   ├── tui/
│   │   ├── app.go          # Main Bubble Tea application + text editing helpers
│   │   ├── handlers.go     # Input handling for all states
│   │   ├── views.go        # UI rendering & layout
│   │   ├── message.go      # Bubble Tea messages
│   │   └── resource_*      # Cluster resource management
│   ├── models/types.go     # All data structures & app state
│   ├── session/manager.go  # Session persistence with subfolder support
│   ├── kubernetes/client.go # K8s API wrapper
│   └── config/kubeconfig.go # Cluster discovery
```

### 🎯 Potential Next Features & Improvements

#### **High Priority:**
1. **Validation system**: Add field validation for CPU/memory formats, replica ranges
2. **Undo/Redo**: Implement undo functionality for text editing operations
3. **Search/Filter**: Add search functionality within HPA/namespace lists
4. **Batch operations**: Multi-select with space bar for bulk operations
5. **Export functionality**: Export sessions to YAML/JSON for backup

#### **Medium Priority:**
6. **Configuration management**: User-configurable templates and default values
7. **Metrics integration**: Show current CPU/memory usage alongside targets
8. **History tracking**: Track all changes with timestamps for audit
9. **Plugin system**: Support for custom validation rules or transforms
10. **Multi-tenancy**: Support for different user profiles/contexts

#### **Advanced Features:**
11. **Git integration**: Track configuration changes in git repositories
12. **Notification system**: Alerts for failed operations or state changes
13. **Web dashboard**: Optional web interface for remote management
14. **API integration**: RESTful API for external tool integration
15. **Monitoring dashboards**: Integration with Prometheus/Grafana for visual metrics

### 🐛 Known Technical Debt & Improvements

#### **Code Quality:**
- **Error handling**: Some async operations could use better error propagation
- **Testing**: Unit test coverage could be expanded for text editing functions
- **Documentation**: Inline code documentation for complex functions
- **Performance**: Large cluster lists could benefit from virtualization

#### **UI/UX:**
- **Responsive design**: Better handling of very small terminal windows
- **Color themes**: Support for different color schemes/accessibility
- **Keyboard shortcuts**: More intuitive shortcuts for power users
- **Status indicators**: More detailed progress indicators for long operations

### 💡 Development Best Practices for This Project

#### **When Adding New Features:**
1. **Follow MVC pattern**: Views in `views.go`, logic in `handlers.go`, state in `models/types.go`
2. **Use text editing helpers**: Leverage `handleTextEditingKeys()` for any text input
3. **Add to help system**: Update help in `renderHelp()` function
4. **Test compilation**: Always run `make build` after changes
5. **Update CLAUDE.md**: Document new features for future developers

#### **Code Style:**
- **Error handling**: Use proper error propagation, don't panic
- **State management**: All UI state should be in `AppModel` struct
- **Async operations**: Use Bubble Tea commands for long-running operations
- **Unicode safety**: Always use `[]rune` for text manipulation
- **Logging**: Use `a.debugLog()` method for debug output

#### **Common Gotchas:**
- **Function closures**: Make sure functions that aren't closed properly (missing `}`)
- **Bubble Tea returns**: Always return both `tea.Model` and `tea.Cmd`
- **Text editing**: Remember to initialize `CursorPosition` when starting text editing
- **Session persistence**: Use folder-aware functions for session operations
- **Azure auth**: Handle token expiration gracefully

This project is **well-architected** and ready for continued development. The text editing system provides a **solid foundation** for any input-heavy features, and the session management system provides excellent **state persistence** capabilities.

---

## 🔄 Continuing Development in New Claude Chats

### **Essential Context for New Conversations**

When starting a new chat with Claude to continue this project, provide this essential context:

#### **Project Overview:**
```
This is a Kubernetes HPA (Horizontal Pod Autoscaler) and Azure AKS Node Pool management tool built in Go with Bubble Tea TUI framework. The application provides an interactive terminal interface for managing HPAs across multiple clusters and node pools with session persistence.

Key components:
- Go 1.23+ (toolchain 1.24.7) with Bubble Tea TUI framework
- Kubernetes client-go for HPA operations
- Azure SDK for node pool management
- Centralized text input system with intelligent cursor
- Session management with folder organization
- Built binary: ./build/k8s-hpa-manager
```

#### **Current State (Updated December 2024):**
```
Recent major improvements completed:
1. ✅ Fixed HPA dual panel system - both panels now display correctly
2. ✅ Fixed TAB navigation between HPA main panel and deployment resources panel
3. ✅ Fixed deployment resource loading - CPU/Memory values now load automatically
4. ✅ Centralized text input architecture (text_input.go)
5. ✅ Intelligent cursor that shows overlaid characters with visual feedback
6. ✅ Session folder organization (HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale)
7. ✅ Eliminated duplicate text editing logic across codebase
8. ✅ Professional text editing UX with advanced keyboard shortcuts

HPA editing now fully functional with dual panel system and deployment resource editing.
All text inputs use unified system with intelligent cursor overlay.
```

#### **File Structure Reference:**
```
internal/tui/
├── app.go - Main orchestrator with centralized text methods
├── text_input.go - Centralized text input module (NEW)
├── handlers.go - Event handling using centralized system
├── views.go - UI rendering with intelligent cursor display
├── resource_*.go - Resource-specific handlers/views
└── message.go - Bubble Tea messages

Key session directories:
~/.k8s-hpa-manager/sessions/
├── HPA-Upscale/
├── HPA-Downscale/
├── Node-Upscale/
└── Node-Downscale/
```

### **Quick Start Commands for Development:**
```bash
# Build and test
make build

# Run with debug logging
./build/k8s-hpa-manager --debug

# Install globally
./install.sh

# Check session storage
ls -la ~/.k8s-hpa-manager/sessions/
```

### **Current Architecture Strengths:**
- ✅ **Text Input**: Centralized, intelligent cursor, zero duplication
- ✅ **Session Management**: Folder organization, template naming, state persistence
- ✅ **Error Handling**: ESC recovery, graceful degradation
- ✅ **Azure Integration**: Token refresh, subscription switching
- ✅ **UI/UX**: Professional editing experience, visual feedback

### **Ready for Next Steps:**
The codebase is now in excellent shape for:
1. **New Features**: Well-structured for easy extension
2. **Bug Fixes**: Centralized systems make fixes apply universally
3. **UI Improvements**: Consistent text editing foundation
4. **Performance**: Clean architecture for optimizations

### **Quick Issue Resolution:**
- **HPA panel issues**: Check `renderHPAMainPanel()` and `renderHPAResourcePanel()` in `views.go`
- **Deployment resources not loading**: Verify `EnrichHPAWithDeploymentResources` is called in `ListHPAs` function
- **TAB navigation problems**: Check `handleHPAEditingKeys()` and ensure `ActivePanel` is initialized correctly
- **Text editing bugs**: Check `text_input.go` first
- **Cursor problems**: Use `RenderWithCursor()` method
- **Session issues**: Verify folder structure in `session/manager.go`
- **Compilation errors**: Run `make build` after any changes

### **Recent Technical Fixes:**
- **File**: `internal/kubernetes/client.go` line 241 - Added deployment resource enrichment
- **File**: `internal/tui/handlers.go` line 1025 - Fixed ActivePanel initialization
- **File**: `internal/tui/text_input.go` - Complete cursor overlay system
- **Functions**: `navigateMainPanelUp/Down` and `navigateResourcePanelUp/Down` for panel navigation

---

## 🎯 **Template Atualizado Para Novos Chats (Dec 2024)**

```
Estou trabalhando em um projeto Kubernetes HPA + Azure AKS Node Pool management tool em Go com Bubble Tea TUI.

Estado atual (Dec 2024):
✅ Sistema completo de edição de HPA com dois painéis funcionando perfeitamente
✅ TAB alterna entre painel HPA (replicas/targets) e painel recursos (CPU/Memory)
✅ Carregamento automático de recursos do deployment (CPU/Memory requests/limits)
✅ Sistema de entrada de texto centralizado com cursor inteligente que mostra caracteres sobrepostos
✅ Organização de sessões em pastas (HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale)
✅ Arquitetura limpa sem duplicação, UX profissional

Correções críticas mais recentes:
- Fixed: HPA dual panel system - ambos painéis agora exibem corretamente
- Fixed: Deployment resource loading - CPU/Memory values carregam automaticamente
- Fixed: TAB navigation entre painéis funciona perfeitamente
- Fixed: ActivePanel initialization (internal/tui/handlers.go:1025)
- Fixed: EnrichHPAWithDeploymentResources call (internal/kubernetes/client.go:241)

Build: make build
Binary: ./build/k8s-hpa-manager

IMPORTANTE: Leia o CLAUDE.md completo para arquitetura detalhada.

[Descreva seu objetivo específico aqui]
```

**Happy coding!** 🚀
- to memorize "sempre compile o build em ./build/ "