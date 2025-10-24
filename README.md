# 🎯 k8s-hpa-manager

> **Gerenciador terminal interativo para Kubernetes HPAs e Azure AKS Node Pools**

Uma ferramenta TUI (Terminal User Interface) poderosa para gerenciar Horizontal Pod Autoscalers e Node Pools do Azure AKS, construída com Go e Bubble Tea.

<div align="center">

### 🎯 Desenvolvido para SRE Logística
**Criado para facilitar operações de scaling de ambientes durante stress tests**

Permite gerenciar HPAs e Node Pools de forma interativa e segura, com sessões reutilizáveis, execução sequencial de node pools e validação de conectividade integrada.

</div>

---

[![CI](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/actions/workflows/ci.yml/badge.svg)](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/actions/workflows/ci.yml)
[![Release](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/actions/workflows/release.yml/badge.svg)](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/actions/workflows/release.yml)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/Paulo-Ribeiro-Log/Scale_HPA)](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/latest)
[![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

---

## 📖 Índice

- [Funcionalidades](#-funcionalidades)
- [Instalação](#-instalação)
- [Quick Start](#-quick-start)
- [Comandos CLI](#-comandos-cli)
- [Controles de Teclado](#%EF%B8%8F-controles-de-teclado)
- [Sistema de Sessões](#-sistema-de-sessões)
- [Node Pools Azure AKS](#-node-pools-azure-aks)
- [Features Avançadas](#-features-avançadas)
- [Desenvolvimento](#%EF%B8%8F-desenvolvimento)
- [Troubleshooting](#-troubleshooting)

---

## 🌟 Funcionalidades

### 🎯 Gerenciamento Kubernetes HPA

| Feature | Descrição |
|---------|-----------|
| **Auto-descoberta** | Descobre clusters `akspriv-*` automaticamente do kubeconfig |
| **Multi-namespace** | Selecione múltiplos namespaces simultaneamente |
| **Edição em Lote** | Modifique múltiplos HPAs de uma vez (Ctrl+U) |
| **Rollout Integration** | Trigger automático de rollouts após mudanças |
| **Resource Management** | Edite CPU/Memory requests/limits dos Deployments |
| **Sistema de Sessões** | Salve configurações para revisão e aplicação posterior |

### ☁️ Gerenciamento Azure AKS

| Feature | Descrição |
|---------|-----------|
| **Node Pools** | Gerencie count, min/max nodes, autoscaling (Ctrl+N) |
| **Execução Sequencial** | Execute 2 node pools em sequência (F12) para stress tests |
| **Autenticação Transparente** | Azure AD + CLI integrados com fallback automático |
| **Async Operations** | Interface não-bloqueante durante aplicações |
| **Auto-discovery** | `k8s-hpa-manager autodiscover` para 26+ clusters |

### 🎨 Interface & UX

| Feature | Descrição |
|---------|-----------|
| **Interface Responsiva** | Adapta-se ao tamanho do terminal (80x24 mínimo) |
| **Multi-tab** | Até 10 tabs simultâneas (Alt+1-9, Alt+0) |
| **Progress Bars** | Estilo Rich Python com cores dinâmicas |
| **Status Container** | Feedback em tempo real de operações |
| **Modais de Confirmação** | Previne aplicações acidentais (Ctrl+D/U) |
| **Help Contextual** | Tecla `?` com scroll navegável |

### 🚀 Features Avançadas (2025)

#### TUI (Terminal Interface)
- ✅ **CronJob Management** (F9) - Enable/disable com status visual
- ✅ **Prometheus Stack** (F8) - Gerenciamento de recursos do stack Prometheus
- ✅ **VPN Validation** - Verifica conectividade K8s antes de operações
- ✅ **Versionamento Automático** - Sistema de updates via GitHub Releases
- ✅ **Sessões Mistas** - Combine HPAs + Node Pools (Ctrl+M)
- ✅ **Navegação Sequencial** - Ctrl+←/→ entre tabs com wrap-around
- ✅ **Log Detalhado** - Todas alterações mostradas (antes → depois)

#### 🌐 Web Interface (Outubro 2025)
- ✅ **Interface Web Completa** - React + TypeScript + shadcn/ui
- ✅ **Dashboard Moderno** - Grid 2x2 com métricas reais (CPU/Memory allocation)
- ✅ **HPAs/Node Pools/CronJobs** - CRUD completo com editores funcionais
- ✅ **Prometheus Stack** - Resource management + **Rollout individual**
- ✅ **Sistema de Sessões** - Save/Load/Rename/Delete/Edit (compatível TUI)
- ✅ **Staging Area** - Preview de alterações antes de aplicar
- ✅ **Aplicar Agora** - Botões para aplicação individual (HPAs e Node Pools)
- ✅ **Snapshot de Cluster** - Captura estado atual para rollback
- ✅ **Heartbeat System** - Auto-shutdown em 20min de inatividade
- ✅ **Standalone Binary** - Frontend embedado (não precisa Node.js em runtime)

---

## 🚀 Instalação

### ⚡ Instalação em 1 Comando (Recomendado)

```bash
# Clone, compile e instala automaticamente
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

**O que este comando faz:**
- ✅ Verifica requisitos (Go, Git, kubectl, Azure CLI)
- ✅ Clona o repositório automaticamente
- ✅ Compila com injeção de versão
- ✅ Instala em `/usr/local/bin/k8s-hpa-manager`
- ✅ Copia scripts utilitários para `~/.k8s-hpa-manager/scripts/`
- ✅ Cria atalho `k8s-hpa-web` para servidor web
- ✅ Testa instalação automaticamente

📚 **Guia completo:** [INSTALL_GUIDE.md](INSTALL_GUIDE.md) | [QUICK_INSTALL.md](QUICK_INSTALL.md)

---

### 📋 Pré-requisitos

#### Obrigatórios
- **Go 1.23+** - Para compilação
- **Git** - Para clonar repositório
- **kubectl** - Cliente Kubernetes configurado

#### Opcionais
- **Azure CLI** - Para operações de Node Pools AKS
- **Terminal colorido** - Para melhor visualização (TUI)

---

### 🔄 Outras Formas de Instalação

<details>
<summary><b>📦 Download de Release (Quando Disponível)</b></summary>

```bash
# Download do binário pré-compilado
wget https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/download/v1.1.0/k8s-hpa-manager-linux-amd64

# Instalar
chmod +x k8s-hpa-manager-linux-amd64
sudo mv k8s-hpa-manager-linux-amd64 /usr/local/bin/k8s-hpa-manager

# Verificar
k8s-hpa-manager version
```

</details>

<details>
<summary><b>🔨 Instalação Manual (Clone Local)</b></summary>

```bash
# Clone o repositório
git clone https://github.com/Paulo-Ribeiro-Log/Scale_HPA.git
cd Scale_HPA

# Método 1: Com script de instalação
./install.sh

# Método 2: Manual
make build
sudo cp build/k8s-hpa-manager /usr/local/bin/
sudo chmod +x /usr/local/bin/k8s-hpa-manager

# Verificar
k8s-hpa-manager version
```

</details>

---

### 🔄 Sistema de Atualizações

A aplicação verifica automaticamente por updates **1x por dia** ao iniciar.

#### Verificar Manualmente

```bash
# Ver versão atual e verificar updates
k8s-hpa-manager version

# Output se houver update disponível:
# 🆕 Nova versão disponível: 1.1.0 → 1.2.0
# 📦 Download: https://github.com/.../v1.2.0
```

#### Atualizar Automaticamente

```bash
# Script de auto-update (copia durante instalação)
~/.k8s-hpa-manager/scripts/auto-update.sh

# Ou com auto-confirmação (para scripts/cron)
~/.k8s-hpa-manager/scripts/auto-update.sh --yes

# Apenas verificar sem instalar
~/.k8s-hpa-manager/scripts/auto-update.sh --check

# Simular atualização (teste)
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run
```

📚 **Documentação completa:** [UPDATE_BEHAVIOR.md](UPDATE_BEHAVIOR.md) | [AUTO_UPDATE_EXAMPLES.md](AUTO_UPDATE_EXAMPLES.md)

---

### 🗑️ Desinstalação

```bash
# Script automatizado (com opção de preservar dados)
~/.k8s-hpa-manager/scripts/uninstall.sh

# Manual
sudo rm /usr/local/bin/k8s-hpa-manager
sudo rm /usr/local/bin/k8s-hpa-web  # Se criado
rm -rf ~/.k8s-hpa-manager/           # Remover dados (opcional)
```

---

## 🎮 Quick Start

### 1️⃣ Primeiro Uso - Auto-descoberta de Clusters

```bash
# Auto-descobre clusters do kubeconfig e configura node pools
k8s-hpa-manager autodiscover

# Inicia a aplicação
k8s-hpa-manager
```

### 2️⃣ Workflow Básico - Scaling HPAs

```bash
k8s-hpa-manager

# 1. Selecione um cluster → ENTER
# 2. Selecione namespaces → SPACE (múltiplos)
# 3. ENTER → Carrega HPAs
# 4. Selecione HPAs → SPACE
# 5. ENTER → Edite valores
# 6. Ctrl+S → Salve sessão
# 7. Ctrl+D → Aplica HPA individual OU Ctrl+U → Aplica todos
```

### 3️⃣ Workflow Avançado - Node Pools

```bash
k8s-hpa-manager

# 1. Ctrl+N → Abre gerenciamento de node pools
# 2. Selecione pools → SPACE
# 3. ENTER → Edite (count, min/max, autoscaling)
# 4. F12 → Marca para execução sequencial (*1, *2)
# 5. Ctrl+D/U → Aplica (com confirmação)
# ✅ *1 executa → *2 inicia automaticamente
```

### 4️⃣ Web Interface - Modo Browser

```bash
# Iniciar servidor web (background por padrão)
k8s-hpa-manager web

# Ou foreground para ver logs
k8s-hpa-manager web -f

# Custom port
k8s-hpa-manager web --port 8080

# Acesse no browser
# http://localhost:8080
# Token: poc-token-123 (padrão POC)

# Features disponíveis:
# - Dashboard com métricas reais do cluster
# - Edição de HPAs com botão "Aplicar Agora"
# - Edição de Node Pools com botão "Aplicar Agora"
# - Rollout individual de recursos Prometheus
# - Sistema de sessões (save/load/edit/delete)
# - Staging area com preview de alterações
```

---

## 📟 Comandos CLI

### Comandos Principais

```bash
# Iniciar interface interativa (TUI)
k8s-hpa-manager

# Iniciar interface web (background por padrão)
k8s-hpa-manager web

# Iniciar interface web em foreground (ver logs)
k8s-hpa-manager web -f
k8s-hpa-manager web --foreground

# Interface web com porta customizada
k8s-hpa-manager web --port 8080

# Mostrar versão e verificar updates
k8s-hpa-manager version

# Auto-descobrir clusters do kubeconfig
k8s-hpa-manager autodiscover

# Modo demo (mostra status sem executar)
k8s-hpa-manager --demo

# Debug mode
k8s-hpa-manager --debug

# Desabilitar verificação de updates
k8s-hpa-manager --check-updates=false
```

### Scripts Utilitários

Após instalação via `install-from-github.sh`, os scripts ficam em `~/.k8s-hpa-manager/scripts/`:

```bash
# Gerenciar servidor web (via atalho)
k8s-hpa-web start          # Iniciar servidor
k8s-hpa-web stop           # Parar servidor
k8s-hpa-web status         # Ver status
k8s-hpa-web logs           # Ver logs em tempo real
k8s-hpa-web restart        # Reiniciar

# Auto-update (verificar e atualizar)
~/.k8s-hpa-manager/scripts/auto-update.sh          # Interativo
~/.k8s-hpa-manager/scripts/auto-update.sh --yes    # Auto-confirmar
~/.k8s-hpa-manager/scripts/auto-update.sh --check  # Apenas verificar
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run # Simular

# Desinstalar
~/.k8s-hpa-manager/scripts/uninstall.sh

# Backup/Restore (para desenvolvimento)
~/.k8s-hpa-manager/scripts/backup.sh "descrição"
~/.k8s-hpa-manager/scripts/restore.sh

# Rebuild web interface (para desenvolvimento)
~/.k8s-hpa-manager/scripts/rebuild-web.sh -b

# Custom kubeconfig
k8s-hpa-manager --kubeconfig /path/to/config

# Desabilitar verificação de updates
k8s-hpa-manager --check-updates=false
```

### Comandos de Desenvolvimento

```bash
# Build TUI
make build                    # → ./build/k8s-hpa-manager
make build-all                # Multi-platform builds

# Build Web Interface
make web-install              # Instalar dependências frontend (primeira vez)
make web-build                # Build frontend → internal/web/static/
make build-web                # Build completo (frontend + backend)
./rebuild-web.sh -b           # Rebuild completo (limpa cache)

# Run
make run                      # Build + run TUI
make run-dev                  # Run TUI com debug
make web-dev                  # Dev server frontend (hot reload)

# Test
make test                     # Run tests
make test-coverage            # Coverage report (coverage.html)

# Release
make version                  # Show detected version
make release                  # Build para todas plataformas
```

---

## ⌨️ Controles de Teclado

### Navegação Global

| Tecla | Ação |
|-------|------|
| `↑↓` ou `k j` | Navegar listas (vi-keys) |
| `←→` ou `h l` | Navegar horizontalmente |
| `Tab` | Alternar entre painéis |
| `Space` | Selecionar/deselecionar item |
| `Enter` | Confirmar seleção ou editar |
| `ESC` | Voltar/cancelar (preserva contexto) |
| `?` | Help contextual com scroll |
| `F4` | Sair da aplicação |

### Clusters & Sessões

| Tecla | Ação |
|-------|------|
| `F5` ou `R` | Reload lista de clusters |
| `Ctrl+L` | Carregar sessão salva |
| `Ctrl+S` | Salvar sessão (funciona SEM modificações para rollback) |
| `Ctrl+M` | Criar sessão mista (HPAs + Node Pools) |

### HPAs

| Tecla | Ação |
|-------|------|
| `Ctrl+D` | Aplicar HPA individual (mostra contador ●) |
| `Ctrl+U` | Aplicar todos HPAs em lote |
| `Space` (edit) | Toggle rollout (Deployment/DaemonSet/StatefulSet) |
| `↑↓` | Navegar campos no modo edição |

### Node Pools Azure

| Tecla | Ação |
|-------|------|
| `Ctrl+N` | Acessar gerenciamento de node pools |
| `Ctrl+D/U` | Aplicar mudanças em node pools |
| `Space` (edit) | Toggle autoscaling |
| `F12` | Marcar para execução sequencial (max 2) |

### Features Especiais

| Tecla | Ação |
|-------|------|
| `F8` | Prometheus Stack Management |
| `F9` | CronJob Management |
| `S` (namespaces) | Toggle namespaces de sistema |
| `Shift+↑↓` | Scroll em painéis responsivos |

### Multi-tab Navigation

| Tecla | Ação |
|-------|------|
| `Ctrl+T` | Nova tab (max 10) |
| `Ctrl+W` | Fechar tab atual (não fecha a última) |
| `Alt+1-9` | Ir para tab 1-9 |
| `Alt+0` | Ir para tab 10 |
| `Ctrl+→` | Próxima tab (wrap-around) |
| `Ctrl+←` | Tab anterior (wrap-around) |

---

## 💾 Sistema de Sessões

### Comportamento

As sessões funcionam como **"estados salvos para revisão"**:

1. **Ctrl+S** → Salva estado atual (cluster + namespaces + HPAs/Node Pools modificados)
2. **Ctrl+L** → Restaura estado para **revisão** (NÃO aplica automaticamente!)
3. **Revisar/Editar** → Ajuste valores se necessário
4. **Ctrl+D/U** → Aplique quando pronto (com confirmação modal)

> 💡 **Vantagem**: Você pode carregar uma sessão, revisar mudanças, ajustar rollouts e depois aplicar com segurança.

### Estrutura de Pastas

```
~/.k8s-hpa-manager/sessions/
├── HPA-Upscale/          # Sessões de scale up de HPAs
├── HPA-Downscale/        # Sessões de scale down de HPAs
├── Node-Upscale/         # Sessões de scale up de node pools
├── Node-Downscale/       # Sessões de scale down de node pools
└── Mixed/                # Sessões combinando HPAs + Node Pools
```

### Templates de Nomenclatura

#### Variáveis Disponíveis

| Variável | Descrição | Exemplo |
|----------|-----------|---------|
| `{action}` | Ação customizada | `upscale`, `emergency` |
| `{cluster}` | Nome do cluster | `akspriv-prod-east` |
| `{env}` | Ambiente | `dev`, `prod`, `staging` |
| `{timestamp}` | Data/hora completa | `19-09-24_14:23:45` |
| `{date}` | Data | `19-09-24` |
| `{time}` | Hora | `14:23:45` |
| `{user}` | Usuário do sistema | `admin` |
| `{hpa_count}` | Número de HPAs | `15` |

#### Templates Predefinidos

```bash
# Action + Cluster + Timestamp
{action}_{cluster}_{timestamp}
# Resultado: "upscale_akspriv-prod_19-09-24_14:23:45"

# Environment + Date
{env}_{date}
# Resultado: "prod_19-09-24"

# Quick Save
Quick-save_{timestamp}
# Resultado: "Quick-save_19-09-24_14:23:45"
```

### Rollback Manual

```bash
# Crie snapshot SEM modificar nada:
k8s-hpa-manager
# 1. Ctrl+L → Carrega sessão com estado atual
# 2. Ctrl+S imediatamente (SEM modificar!)
# 3. Nomear: "rollback-producao-2025-01-10"
# ✅ Sessão de backup pronta para rollback futuro
```

---

## ☁️ Node Pools Azure AKS

### Auto-discovery

```bash
# Extrai resource groups e subscriptions do kubeconfig
k8s-hpa-manager autodiscover

# Gera/atualiza ~/.k8s-hpa-manager/clusters-config.json
# Escalável para 26+ clusters sem configuração manual
```

### Workflow de Edição

```bash
k8s-hpa-manager

# Ctrl+N → Abre node pools
# Edite campos:
#   - Node Count (manual mode)
#   - Min Nodes / Max Nodes (autoscaling mode)
#   - Autoscaling Enabled (Space para toggle)
# Ctrl+S → Salva sessão
# Ctrl+D/U → Aplica (com confirmação modal)
```

### Execução Sequencial (Stress Tests)

Útil para testar capacidade durante scale down/up controlado:

```bash
k8s-hpa-manager

# 1. Ctrl+N → Node pools
# 2. F12 em monitoring-1 → Marca como *1
# 3. F12 em monitoring-2 → Marca como *2
# 4. Edite valores (ex: *1 → 0 nodes, *2 → scale up)
# 5. Ctrl+D/U → Inicia execução
# ✅ Interface permanece responsiva
# ✅ *1 executa → StatusContainer mostra progresso
# ✅ *1 completa → *2 inicia automaticamente
# ✅ Multi-tasking: edite HPAs enquanto pools executam
```

**Workflow Assíncrono:**
- `🔍 Verificando conectividade VPN com Azure...`
- `✅ VPN conectada - Azure acessível`
- `🔄 *1: Aplicando...` → `✅ *1: Completado`
- `🚀 Iniciando automaticamente *2` → `✅ *2: Completado`

---

## 🎨 Features Avançadas

### CronJob Management (F9)

```bash
k8s-hpa-manager

# Na seleção de namespaces → F9
# Recursos:
#   - Status visual: 🟢 Ativo, 🔴 Suspenso, 🟡 Falhou, 🔵 Executando
#   - Schedule description: "0 2 * * * - executa todo dia às 2:00 AM"
#   - Enable/disable: Enter → Space (toggle suspend)
#   - Apply: Ctrl+D (individual) ou Ctrl+U (batch)
# ESC → Volta para namespaces (preserva estado)
```

### Prometheus Stack Management (F8)

```bash
k8s-hpa-manager

# Na seleção de namespaces → F8
# Features:
#   - Métricas assíncronas (não bloqueia UI)
#   - Exibição dual:
#     * Lista: "CPU: 1 (uso: 264m)/2 | MEM: 8Gi (uso: 3918Mi)/12Gi"
#     * Edit: "CPU Request: 1", "Memory Request: 8Gi"
#   - Auto-scroll: Item selecionado sempre visível
#   - Refresh: 300ms durante coleta de métricas
# ESC → Volta para namespaces (preserva estado)
```

### Validação VPN On-Demand

A aplicação valida conectividade VPN antes de operações críticas:

```bash
# Pontos de validação:
#   - Startup (discoverClusters)
#   - Load namespaces
#   - Load HPAs
#   - Apply operations (Ctrl+D/U)

# Feedback no StatusContainer:
#   🔍 Validando conectividade VPN...
#   ✅ VPN conectada - Kubernetes acessível
#
#   # Ou se falhar:
#   ❌ VPN desconectada - Kubernetes inacessível
#   💡 SOLUÇÃO: Conecte-se à VPN e tente novamente (F5)
```

### Versionamento Automático

```bash
# Verificação em background (1x/dia)
k8s-hpa-manager

# Notificação no StatusContainer (após 3s):
# 🆕 Nova versão disponível: 1.5.0 → 1.6.0
# 📦 Download: https://github.com/.../v1.6.0
# 💡 Execute 'k8s-hpa-manager version'

# Manual
k8s-hpa-manager version
# Output:
#   k8s-hpa-manager versão 1.6.0
#   Verificando updates...
#   ✅ Você está usando a versão mais recente!
```

### Log Detalhado (Antes → Depois)

Todas alterações exibidas no StatusContainer:

```
⚙️ Aplicando HPA: ingress-nginx/nginx-ingress-controller
  📝 Min Replicas: 1 → 2
  📝 Max Replicas: 8 → 12
  📝 CPU Target: 60% → 70%
  🔧 CPU Request: 50m → 100m
  🔧 Memory Request: 90Mi → 180Mi
✅ HPA aplicado: ingress-nginx/nginx-ingress-controller
```

---

## 🛠️ Desenvolvimento

### Estrutura do Projeto

```
k8s-hpa-manager/
├── cmd/                           # CLI entry points
│   ├── root.go                    # Main command + Azure auth
│   ├── version.go                 # Version command
│   ├── autodiscover.go            # Cluster auto-discovery
│   └── k8s-teste/                 # Layout testing tools
├── internal/
│   ├── tui/                       # Terminal UI (Bubble Tea)
│   │   ├── app.go                 # Main orchestrator
│   │   ├── handlers.go            # Event handlers
│   │   ├── views.go               # UI rendering
│   │   ├── message.go             # Bubble Tea messages
│   │   ├── resource_*.go          # HPA/Node Pool management
│   │   ├── cronjob_*.go           # CronJob management
│   │   ├── components/            # Reusable UI components
│   │   └── layout/                # Layout management
│   ├── models/
│   │   └── types.go               # Domain model & app state
│   ├── kubernetes/
│   │   └── client.go              # K8s client wrapper
│   ├── azure/
│   │   └── auth.go                # Azure SDK auth
│   ├── session/
│   │   └── manager.go             # Session persistence
│   ├── config/
│   │   └── kubeconfig.go          # Cluster discovery
│   ├── updater/                   # Versioning system
│   │   ├── version.go
│   │   ├── github.go
│   │   └── checker.go
│   └── ui/                        # UI utilities
│       ├── progress.go
│       ├── logs.go
│       └── status_panel.go
├── build/                         # Build artifacts
├── backups/                       # Code backups (via backup.sh)
├── install.sh                     # Automated installer
├── uninstall.sh                   # Automated uninstaller
├── backup.sh                      # Create backups
├── restore.sh                     # Restore backups
├── Makefile
└── go.mod
```

### Tech Stack

#### Backend (Go)
| Tecnologia | Versão | Uso |
|------------|--------|-----|
| **Go** | 1.23+ (toolchain 1.24.7) | Linguagem principal |
| **Bubble Tea** | v0.24.2 | TUI framework |
| **Lipgloss** | v1.1.0 | Styling e layout |
| **Cobra** | v1.10.1 | CLI commands |
| **client-go** | v0.31.4 | Kubernetes client oficial |
| **Azure SDK** | Latest | Azure AKS management |
| **Gin** | v1.9+ | Web framework (REST API) |

#### Frontend (Web Interface)
| Tecnologia | Versão | Uso |
|------------|--------|-----|
| **React** | 18.3 | UI framework |
| **TypeScript** | 5.8 | Type safety |
| **Vite** | 5.4 | Build tool + dev server |
| **Tailwind CSS** | 3.4 | Styling |
| **shadcn/ui** | Latest | Component library |
| **React Query** | (TanStack) | State management |
| **React Router** | Latest | Navegação |
| **Lucide React** | Latest | Ícones |

### Padrões de Código

**State Management:**
- Todo estado da aplicação em `AppModel` (`internal/models/types.go`)
- State transitions via `AppState` enum
- Bubble Tea messages para operações assíncronas

**Edição de Texto:**
- Centralizada em `internal/tui/text_input.go`
- Cursor inteligente com overlay
- Unicode-safe (sempre usar `[]rune`)

**Error Handling:**
- Propagação adequada (não usar panics)
- Mensagens no StatusContainer
- ESC sempre retorna ao contexto anterior

---

## 🔧 Troubleshooting

### Problemas Comuns

#### Instalação

| Problema | Solução |
|----------|---------|
| **"Go not found"** | Instale Go 1.23+ de [golang.org/dl](https://golang.org/dl/) |
| **Permission denied** | Use `sudo` para instalar em `/usr/local/bin/` |
| **Binary not found** | Reinicie terminal ou adicione `/usr/local/bin` ao PATH |

#### Conectividade

| Problema | Solução |
|----------|---------|
| **Cluster offline** | Execute `kubectl cluster-info --context=<cluster>` |
| **VPN desconectada** | Conecte VPN e pressione F5 para reload |
| **HPAs não carregam** | Verifique RBAC e toggle namespaces sistema (tecla `S`) |
| **Azure timeout** | Valide `az login` e subscription ativa |

#### Interface

| Problema | Solução |
|----------|---------|
| **Help muito grande** | Use ↑↓ ou PgUp/PgDn para scroll |
| **Texto minúsculo** | Interface adapta-se ao terminal (use Ctrl+0 para tamanho normal) |
| **Erro sem saída** | Use ESC para voltar (preserva contexto) |

#### Node Pools

| Problema | Solução |
|----------|---------|
| **Node pools não carregam** | Execute `k8s-hpa-manager autodiscover` |
| **"clusters-config.json not found"** | Execute autodiscover para gerar o arquivo |
| **Azure auth failed** | Execute `az login` manualmente |

#### Interface Web

| Problema | Solução |
|----------|---------|
| **Frontend não carrega** | Execute `make web-build` antes de `make build` |
| **"Frontend not found"** | Rode `make build-web` para build completo |
| **Mudanças não aparecem** | Use `./rebuild-web.sh -b` para limpar cache |
| **Dropdown não visível** | Hard refresh no browser (Ctrl+Shift+R) |
| **API retorna 404** | Verifique se servidor está rodando em background |
| **Heartbeat falha** | Servidor desligou por inatividade (20min), reinicie |

### Debug Mode

```bash
# Ativa logging detalhado
k8s-hpa-manager --debug

# Logs exibidos no terminal incluem:
#   - Estado da aplicação (AppState transitions)
#   - Mensagens Bubble Tea
#   - Operações Kubernetes (API calls)
#   - Azure authentication flow
```

### Backup e Restore

```bash
# Criar backup antes de modificações
./backup.sh "descrição do backup"

# Listar backups disponíveis
./restore.sh

# Restaurar backup específico
./restore.sh backup_20251001_122526
```

- Mantém 10 backups mais recentes
- Metadados inclusos (git commit, data, usuário)

---

## 📊 Exemplos Práticos

### 🚨 Cenário 1: Scale Up Emergencial

```bash
k8s-hpa-manager

# 1. Selecionar cluster produção → ENTER
# 2. Selecionar namespaces críticos → SPACE (múltiplos)
# 3. ENTER → Carregar HPAs
# 4. Selecionar HPAs → SPACE, ENTER para editar
# 5. Aumentar max_replicas de 10 → 20
# 6. SPACE → Ativar rollout
# 7. Ctrl+S → Salvar como "emergency-scale-2025-10-15"
# 8. Ctrl+U → ENTER (confirmar modal)
# ✅ Todas mudanças aplicadas + rollouts executados
```

### 🛍️ Cenário 2: Preparação Black Friday

```bash
k8s-hpa-manager

# DIA 1 - Preparação (semana antes)
# 1. Editar HPAs para valores black friday
# 2. Ctrl+S → "blackfriday-2025-config"
# 3. ESC → Sai sem aplicar

# DIA 2 - Black Friday (dia do evento)
# 1. Ctrl+L → Carregar "blackfriday-2025-config"
# 2. Revisar valores (já estão configurados!)
# 3. Ctrl+U → ENTER (aplicar tudo)
# ✅ Rollouts automáticos executados
```

### 🔄 Cenário 3: Rollback Rápido

```bash
# ANTES do incident (criar backup preventivo)
k8s-hpa-manager
# 1. Ctrl+L → Carregar estado atual
# 2. Ctrl+S imediatamente (SEM modificar)
# 3. Nomear: "backup-pre-change-2025-10-15"

# DURANTE incident (rollback)
k8s-hpa-manager
# 1. Ctrl+L → "backup-pre-change-2025-10-15"
# 2. Revisar valores originais
# 3. Ctrl+U → ENTER (aplicar)
# ✅ Rollback completo em segundos
```

### ⚡ Cenário 4: Stress Test com Node Pools

```bash
k8s-hpa-manager autodiscover  # Se primeira vez

k8s-hpa-manager
# 1. Ctrl+N → Node pools
# 2. F12 em monitoring-1 → *1 (primeiro)
# 3. F12 em monitoring-2 → *2 (segundo)
# 4. Edit *1: Node Count = 0 (scale down total)
# 5. Edit *2: Node Count = 5 (scale up)
# 6. Ctrl+U → ENTER
# ✅ *1 executa (scale down)
# ✅ Sistema monitora → *2 inicia automaticamente
# ✅ Interface livre para editar HPAs durante execução
```

### 🌐 Cenário 5: Gerenciamento via Web Interface

```bash
# Iniciar servidor web
k8s-hpa-manager web

# Acessar no browser: http://localhost:8080

# WORKFLOW 1: Editar HPAs e aplicar
# 1. Selecionar cluster no dropdown
# 2. Aba "HPAs" → Click no HPA desejado
# 3. Editar valores (min/max replicas, targets, resources)
# 4. Click "Aplicar Agora" → HPA atualizado imediatamente
# ✅ Ou click "Salvar (Staging)" para aplicar múltiplos depois

# WORKFLOW 2: Rollout de Prometheus Stack
# 1. Aba "Prometheus" → Lista de recursos
# 2. Click "Rollout" no deployment/statefulset/daemonset
# 3. Aguardar 2s → Lista atualiza automaticamente
# ✅ Rollout executado sem interromper serviço

# WORKFLOW 3: Node Pools com aplicação individual
# 1. Aba "Node Pools" → Click no pool desejado
# 2. Editor abre no painel direito
# 3. Ajustar node count, autoscaling, min/max
# 4. Click "Aplicar Agora" → Azure CLI executa em background
# ✅ Alteração aplicada sem staging

# WORKFLOW 4: Snapshot para Rollback
# 1. Click "Salvar Sessão"
# 2. Modo "Capturar Snapshot" (sem modificações pendentes)
# 3. Pasta: "Rollback"
# 4. Nome: "pre-deploy-2025-10-23"
# 5. Click "Capturar Snapshot"
# ✅ Estado atual do cluster salvo para rollback futuro

# WORKFLOW 5: Editar sessão salva
# 1. Click "Load Session" → Escolher pasta
# 2. Click menu (⋮) → "Editar Conteúdo"
# 3. Tabs "HPAs" / "Node Pools" → Click para expandir
# 4. Modificar valores incorretos
# 5. Click "Salvar Alterações"
# ✅ Sessão atualizada (arquivo JSON modificado)
```

---

## 🤝 Contribuição

Contribuições são bem-vindas! Por favor:

1. Fork o projeto
2. Crie uma branch: `git checkout -b feature/nova-funcionalidade`
3. Commit: `git commit -m 'feat: adiciona nova funcionalidade'`
4. Push: `git push origin feature/nova-funcionalidade`
5. Abra um Pull Request

### Reportando Bugs

Ao abrir uma issue, inclua:

- **Versão**: `k8s-hpa-manager version`
- **Go version**: `go version`
- **OS**: `uname -a`
- **Logs**: Output com `--debug`
- **Steps to reproduce**: Como reproduzir o problema

---

## 📝 Licença

Este projeto está sob a licença MIT. Veja [LICENSE](LICENSE) para detalhes.

---

## 📚 Documentação Adicional

### Interface Web
- **Docs/README_WEB.md** - Documentação completa da interface web
- **Docs/WEB_INTERFACE_DESIGN.md** - Arquitetura e design system
- **Docs/WEB_SESSIONS_PLAN.md** - Sistema de sessões (planejamento)
- **internal/web/frontend/README.md** - Guia do desenvolvedor frontend

### CLAUDE.md
- **CLAUDE.md** - Instruções para Claude Code (contexto completo do projeto)

---

## 📞 Suporte

### Precisa de Ajuda?

1. **Help Contextual**: Pressione `?` na aplicação TUI
2. **Docs Web**: Consulte `Docs/README_WEB.md` para interface web
3. **Troubleshooting**: Consulte seção acima
4. **Debug Mode**: Execute com `--debug`
5. **Issues**: [Abra uma issue](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/issues)

---

<div align="center">

**🎯 Desenvolvido para simplificar o gerenciamento de HPAs e Node Pools**

⚡ **TUI rápida e intuitiva** | 🌐 **Interface Web moderna** | 💾 **Sessões que preservam seu trabalho**

🚀 **Rollouts individuais** | 📊 **Dashboard com métricas reais** | 🔄 **Snapshot para rollback**

[![⭐ Star no GitHub](https://img.shields.io/github/stars/Paulo-Ribeiro-Log/Scale_HPA?style=social)](https://github.com/Paulo-Ribeiro-Log/Scale_HPA)

</div>
