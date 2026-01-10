# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Versão atual oficial: **v1.3.1** (GitHub release). Tags locais v1.3.2+ são do projeto antigo e devem ser ignoradas.
**IMPORTANTE**: Ao fazer alterações no frontend (React/TypeScript), sempre rebuild com `./rebuild-web.sh -b` E fazer hard refresh no navegador (Ctrl+Shift+R).
**IMPORTANTE**: Data de hoje: **08 de janeiro de 2026** - usar esta data ao documentar mudanças.

---

## 📑 Índice / Table of Contents

### 📘 Guias Essenciais
1. [🚀 Quick Start](docs/guides/QUICK_START.md) - Estado atual do projeto, features e tech stack
2. [🔧 Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md) - Comandos de desenvolvimento, build e deploy
3. [🏗️ Architecture Overview](docs/architecture/OVERVIEW.md) - Estrutura de diretórios e componentes
4. [🌐 Interface Web](docs/guides/WEB_INTERFACE.md) - React/TypeScript, features e workflows
5. [⚠️ Common Pitfalls](docs/guides/COMMON_PITFALLS.md) - Erros comuns e soluções
6. [🧪 Testing Strategy](docs/guides/TESTING.md) - Estratégias de teste e validação
7. [🔧 Troubleshooting](docs/guides/TROUBLESHOOTING.md) - Resolução de problemas comuns
8. [🚀 Continuing Development](docs/guides/CONTINUING_DEV.md) - Context templates e best practices
9. [⚡ Async Optimization Plan](docs/guides/ASYNC_OPTIMIZATION_PLAN.md) - Plano de otimização assíncrona da auto descoberta
10. [📦 Installation Scripts](docs/guides/INSTALLATION_SCRIPTS.md) - Scripts de instalação (release vs main)
11. [🔔 Windows Notifications](docs/guides/WINDOWS_NOTIFICATIONS.md) - Sistema de notificações via PowerShell/WSL2
12. [🔒 RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md) - Controle de acesso baseado em grupos do Azure AD

### 📚 Histórico e Referências
11. [📜 Histórico de Correções](docs/history/CHANGELOG.md) - Correções e refatorações principais
12. [🔍 Análise Cordon/Drain](ANALISE_NODEPOOL_CORDON_DRAIN.md) - Análise detalhada do sistema

### 🚀 Otimizações
13. [⚡ Autodiscover Optimization](docs/optimization/AUTODISCOVER_OPTIMIZATION.md) - Paralelização da busca de subscriptions

### 📋 Roadmaps e Estudos
14. [🔐 Estudo SSO Azure AD](ESTUDO_SSO_AZURE_AD.md) - Análise técnica de SSO OAuth 2.0/OIDC
15. [🛣️ Roteiro Implementação SSO](ROTEIRO_IMPLEMENTACAO_SSO.md) - **Roadmap completo para servidor centralizado** (6 dias)
16. [📧 Implementação user_email History](IMPLEMENTACAO_USER_EMAIL_HISTORY.md) - User tracking no History Tracker (v1.3.6)

### 🤖 AI Diagnostics (✅ Produção desde 23/12/2025)
17. [🧠 Plano AI Diagnostics](PLANO_AI_DIAGNOSTICS.md) - **Plano completo de implementação** (8 dias)
18. [📊 Progresso AI Diagnostics](PROGRESSO_AI_DIAGNOSTICS.md) - **Status: 100% Completo** (v1.3.6)

### ⎈ Helm Tab (✅ Produção desde 08/01/2026)
19. [🎯 Plano Aba Helm](PLANO_ABA_HELM.md) - **Implementação completa da aba Helm** (v1.3.7+)
20. [🔧 Plano de Correção Helm](PLANO_CORRECAO_HELM.md) - **Correções críticas da aba Helm** (busca dinâmica, React Query, SSE progress)
21. [📋 Sumário de Correções Helm](SUMARIO_CORRECOES_HELM.md) - **Resumo executivo das 5 fases de correção**
22. [✏️ Correções Manifest Tab](CORRECOES_MANIFEST_TAB.md) - **Detalhamento da refatoração do editor Manifest**

### 📋 Health Checking (✅ Produção desde 28/12/2025)
23. [📊 Implementação Logs Persistentes](IMPLEMENTACAO_LOGS_PERSISTENTES.md) - **Sistema de persistência e visualização de logs históricos** (v1.3.7+)

---

## 📌 Quick Reference

### Comandos Mais Usados

```bash
# Build e Run
make build                    # Compilar backend Go (detecta versão via git tags)
make web-build                # Build frontend (npm run build + copy para static/)
./build/new-k8s-hpa web       # Iniciar servidor web (porta 8080)

# Development
make run-dev                  # TUI com debug (go run . --debug)
make web-dev                  # Frontend dev server (Vite HMR - porta 5173)
make build-web                # Build completo (frontend + backend)
./rebuild-web.sh -b           # ⭐ RECOMENDADO - Evita cache issues

# Testing
make test                     # Rodar todos os testes
make test-coverage            # Testes com coverage (gera coverage.html)
go test -v ./internal/config -race  # Testar race conditions específicas
go test -run TestGetClient    # Rodar teste específico
go test -v ./internal/rbac -race    # Testar RBAC com race detector
go test -v ./internal/ai            # Testar AI Diagnostics
go test -v ./internal/sanitizer     # Testar sanitização de logs
./testes/test-rbac.sh         # Suite completa RBAC (40+ cenários)

# Debug Web
tail -f /tmp/k8s-hpa-manager-web-*.log  # Logs do servidor (background mode)
./build/new-k8s-hpa web -f    # Foreground mode (logs no terminal)
./build/new-k8s-hpa web --ad  # ⚠️  EMERGÊNCIA: Bypass RBAC (flag oculta, sem documentação)

# Installation (Release - Estável)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Installation (Main - Desenvolvimento)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash

# Updates
new-k8s-hpa version           # Verificar versão e updates disponíveis
~/.k8s-hpa-manager/scripts/auto-update.sh  # Auto-update interativo

# Release
make release                  # Build multi-plataforma (Linux, macOS Intel/ARM)
./create-v1-release.sh        # Criar release no GitHub com binários

# Scripts Utilitários
./web-server.sh start|stop|status|logs  # Gerenciar servidor web
./rebuild-web.sh -b                     # Rebuild frontend + backend (limpa cache)
./backup.sh                             # Backup automático do código
./restore.sh                            # Restaurar último backup
./uninstall.sh                          # Desinstalar completamente
./diagnostico.sh                        # Diagnóstico de problemas comuns
```

### Estrutura do Projeto

```
k8s-hpa-manager/
├── cmd/                      # CLI commands (Cobra)
├── internal/
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   ├── web/                  # Web Interface (React/TS)
│   │   ├── frontend/         # React SPA
│   │   ├── handlers/         # Go REST API
│   │   ├── sse/              # Server-Sent Events
│   │   └── middleware/       # RBAC, CORS, etc.
│   ├── kubernetes/           # K8s client wrapper
│   ├── azure/                # Azure SDK auth
│   ├── models/               # Data structures
│   ├── config/               # Kubeconfig, cache
│   ├── session/              # Session management
│   ├── monitoring/           # Prometheus/metrics
│   ├── rbac/                 # Azure AD RBAC
│   ├── ai/                   # AI Diagnostics (Ollama/Claude)
│   ├── collectors/           # Context collectors
│   ├── sanitizer/            # Sanitização de logs
│   ├── storage/              # SQLite persistence
│   ├── history/              # History tracker
│   ├── updater/              # Auto-update system
│   ├── validation/           # Input validation
│   └── logs/                 # Logging utilities
├── docs/                     # Documentação modular
├── build/                    # Build artifacts
├── vendor/                   # Go modules vendored
└── scripts/                  # Utility scripts
```

**Módulos Go Principais:**
- `k8s-hpa-manager` (root) - Módulo principal (Go 1.24.0)
- Dependências chave: k8s.io/client-go v0.34.1, gin v1.11.0, zerolog v1.34.0
- Build com vendor mode: `go build -mod=vendor`

### ⚠️ Comandos Críticos - NÃO Esquecer

**Antes de Commitar Código:**
```bash
# 1. SEMPRE rodar testes com race detector
go test -v ./internal/... -race

# 2. SEMPRE verificar se build funciona
make build

# 3. Se alterou frontend, SEMPRE rebuild
./rebuild-web.sh -b

# 4. Verificar formatting Go
go fmt ./...

# 5. Verificar se vendored modules estão atualizados
go mod vendor
```

**Antes de Fazer PR/Release:**
```bash
# 1. Rodar suite completa de testes
make test-coverage

# 2. Testar RBAC (se alterou permissões)
./testes/test-rbac.sh

# 3. Verificar versão será injetada corretamente
make version

# 4. Build multi-plataforma (smoke test)
make release
```

**Após Fazer Mudanças no Frontend:**
```bash
# 1. SEMPRE rebuild com script
./rebuild-web.sh -b

# 2. SEMPRE fazer hard refresh no navegador
# Ctrl+Shift+R (Linux/Windows) ou Cmd+Shift+R (macOS)

# 3. Verificar assets foram gerados
ls -lh internal/web/static/assets/ | grep -E "\.(js|css)$"

# 4. Verificar referências no index.html
grep -E "index-.*\.(js|css)" internal/web/static/index.html
```

### Tech Stack (Quick Reference)

| Categoria | Tecnologia |
|-----------|------------|
| **Backend** | Go 1.24.0+, client-go v0.34.1, Azure SDK v1.19.1 |
| **Frontend** | React 18.3.1, TypeScript 5.8.3, Vite 5.4.21 |
| **UI** | shadcn/ui (Radix UI), Tailwind CSS 3.4.17 |
| **Editor** | Monaco Editor 0.52.2, xterm.js 5.3.0 |
| **Gráficos** | Recharts 2.15.4, Cytoscape 3.33.1 |
| **Web Server** | Gin 1.11.0, CORS, SSE (Server-Sent Events) |
| **Arquitetura** | MVC, SSE, WebSocket (Terminal), React Query |

### Features Principais

✅ **TUI Completo** - Interface terminal responsiva com progress bars
✅ **Web Interface** - React/TypeScript com 99% das features
✅ **HPAs & Node Pools** - CRUD completo com staging area
✅ **Cordon/Drain** - Sistema de evacuação com progress em tempo real (SSE)
✅ **Audit Log** - Rastreabilidade completa de operações Cordon/Drain e Rollouts com histórico persistente
✅ **CronJob Editor Aprimorado** - Interface visual com parser de cron, descrições legíveis e validação em tempo real
✅ **Sessions** - Save/load/edit com compatibilidade TUI ↔ Web
✅ **Monitoring V2** - Sistema sem port-forwards, acesso direto Prometheus
✅ **Auto-Updates** - Sistema automático de detecção e instalação
✅ **Autodiscover Otimizado** - Busca paralela de subscriptions (10x mais rápido)
✅ **Navegação Inteligente** - Navegação bidirecional: HPAs ↔ Monitoramento com 1 clique
✅ **Métricas Corrigidas** - CPU/Memória agora calculam média correta (0-100%) com avg() ao invés de sum()
✅ **VM Specs Display** - Exibe vCPUs e memória de cada VM Size no Node Pool Editor
✅ **Notificações In-App Clicáveis** - Sistema de notificações web com navegação contextual para AlertsDialog
✅ **AlertsDialog Aprimorado** - Card de contexto destacado com extração inteligente de HPA/Pod/Container/Deployment
✅ **Sistema de Alertas Completo** - Exibe TODOS os alertas ativos (sem filtros restritivos) com filtro por período de tempo
✅ **Alertas Filtrados por Tempo** - Botão de alertas no painel "Análise de Métricas" respeita seletor de tempo (5min-24h)
✅ **Gráfico de Réplicas Corrigido** - Backend agora retorna `replicas_current` corretamente (era `replicas`)
✅ **Comparação Histórica D-1/D-2/D-3** - Linha lilás nos gráficos permite comparar métricas atuais com 1, 2 ou 3 dias atrás
  - Select dropdown nos modais expandidos para escolher período de comparação
  - Backend: parâmetro `days_offset` (1-3) com validação e cálculo dinâmico de offset
  - Frontend: refetch automático ao trocar período, labels descritivas nos selects
  - Linha com opacidade 0.2-0.3 e nome dinâmico (ex: "CPU D-2 (48h atrás)")
✅ **Card de Cluster Contextual** - Card de estatísticas adapta-se ao contexto da aba
  - Dashboard: exibe total de clusters disponíveis no kubeconfig
  - Outras abas: mostra contexto selecionado + versão do Kubernetes
  - Usa truncate com tooltip para nomes longos, mantém consistência visual
✅ **UI/UX Improvements (v1.3.1)** - Melhorias na interface e usabilidade
  - Gráfico de Memória: Linha corrente agora usa cor azul (#3b82f6) ao invés de roxo, evitando confusão com linha D-1
  - ConfigMaps/Secrets/Deployments: Labels iniciam recolhidos, campo "Versão" exibido quando disponível (app.kubernetes.io/version)
  - Node Pools: Botão de refresh adicionado no painel "Available Node Pools" para atualizar dados do Azure AKS
  - VM Disk Specs: Exibe informações de performance de disco (Temp Disk, Max Disks, IOPS, Throughput) no Node Pool Editor
✅ **Dashboard por Namespace (v1.3.2)** - Visibilidade granular de consumo de recursos
  - Card "Top 5 Namespaces por Consumo" no Dashboard principal
  - Grid 3 colunas: CPU Usage | Memory Usage | Top 5 Namespaces (lado a lado)
  - Tabs interativas: CPU (millicores), Memory (GB), Pods (count)
  - Barras de progresso coloridas: verde (0-50%), amarelo (50-75%), vermelho (75-100%)
  - Seção "Outros" com soma de namespaces fora do Top 5
  - Auto-refresh a cada 60 segundos + botão manual de refresh
  - Backend: Queries Prometheus agregadas por namespace (`sum by (namespace)`)
  - Endpoint: `GET /api/v1/namespaces/:cluster/metrics?limit=5`
  - Graceful degradation: estado de erro elegante quando Prometheus inacessível
✅ **Aba Namespaces (v1.3.4)** - Gerenciamento completo de Namespaces Kubernetes
  - **Overview Top 5**: Quando nenhum namespace selecionado, exibe 3 gráficos de pizza lado a lado
    - Top 5 Namespaces por CPU (millicores)
    - Top 5 Namespaces por Memória (GB)
    - Top 5 Namespaces por Pods (count)
    - Seção "Outros" agregando namespaces fora do Top 5
  - **Detalhes do Namespace**: Ao selecionar namespace, exibe painel com:
    - Metadados: Nome, Cluster, Status (Phase do namespace), Age (formato compacto: 2d5h, 30m, 45s)
    - Monaco YAML Editor (tema VS Code) com **edição completa e persistente**
    - **Sistema de edição avançado** (copiado de ConfigMaps):
      - ✅ Undo/Redo com histórico de 50 versões (cache persistente por namespace)
      - ✅ View Mode toggle: Editor | Diff (side-by-side)
      - ✅ Validação Dry-run (testa YAML antes de aplicar)
      - ✅ Visualizar diff com diff2html (modal compacto + fullscreen)
      - ✅ Apply com confirmação (exibe mudanças detectadas)
      - ✅ Cancelar (restaura YAML original)
      - ✅ Botões duplicados em: painel normal + modal fullscreen
    - Botões de ação: Describe, Visualizar diff, Expandir Editor, Dry-run, Cancelar, Aplicar
    - Dropdown menu (⋮) com opção Delete (cor vermelha)
    - Modal fullscreen do editor com toolbar completa de edição
  - **Criar Namespace**: Botão no header do painel "Visualização" (à direita)
    - Modal com campo de texto para nome do namespace
    - Validação: nome obrigatório, cluster selecionado
    - Auto-refresh da lista após criação bem-sucedida
  - **Backend completo**:
    - `GET /namespaces/:cluster/:name` - Obter manifesto YAML
    - `GET /namespaces/:cluster/:name/describe` - kubectl describe
    - `POST /namespaces/:cluster` - Criar namespace
    - `PUT /namespaces/:cluster/:name` - **Aplicar YAML editado** (com suporte a dry-run)
    - `DELETE /namespaces/:cluster/:name` - Deletar namespace
    - `GET /namespaces/:cluster/metrics?limit=5` - Top 5 métricas
  - **Componente**: `NamespacesTab.tsx` (1240+ linhas, dual-mode: overview vs details)
  - **Correções**: Hook `useNamespaces` agora retorna `refetch` corretamente conectado ao botão Atualizar
  - **Imports**: diff (createTwoFilesPatch), diff2html (html), js-yaml (load), ícones Undo2/Redo2/CheckCircle2/TriangleAlert/FileDiff
✅ **Aba Pods (v1.3.3)** - Gerenciamento completo de Pods Kubernetes
  - Listagem de pods com filtros por namespace, search, system pods
  - Status visual com badges coloridos (Running/Pending/Failed/CrashLoopBackOff)
  - Indicadores de restart count com alertas visuais (>3 restarts)
  - Informações de containers: estado, imagem, restarts, ready status
  - Labels colapsáveis por pod
  - Modal de detalhes com 2 tabs: YAML Manifest + Logs
  - Logs de containers individuais (tail 500 linhas) com **syntax highlighting**
  - Botão "Delete Pod" com confirmação
  - Auto-refresh a cada 30 segundos
  - Backend: Endpoints já existiam em `internal/web/handlers/pods.go`
  - API: `GET /pods?cluster=...&namespaces=...`, `GET /pods/:cluster/:namespace/:name`, `DELETE /pods/:cluster/:namespace/:name`, `GET /pods/:cluster/:namespace/:name/logs`
  - Componente: `PodsPanel.tsx` (estilo ConfigMaps/Secrets/Deployments)
✅ **Aba Containers (v1.3.3)** - Visualização dedicada de containers
  - Painel esquerdo: Lista de pods + containers (tree view)
  - Painel direito: Logs + Detalhes do container selecionado
  - Botão "Labels" movido para painel direito (Container Logs & Details)
  - Auto-refresh de logs a cada 3 segundos (opcional)
  - Download de logs como arquivo .txt
  - Componente: `ContainersTab.tsx`
✅ **UX Improvements - Pods/Containers (v1.3.3)**
  - **Métricas inline**: Layout horizontal "RESTARTS: 3  CPU/R: 800m  CPU/L: 1" (ao invés de grid vertical)
  - **Badge de versão**: Extrai versão da imagem do container (ex: nginx:1.21.0 → "1.21.0")
  - **Log highlighting com estilos inline**: Colorização automática de logs
    - ❌ ERROR/FATAL/EXCEPTION: Vermelho (#f87171) com fundo e borda vermelha
    - ⚠️ WARN/WARNING: Amarelo/Laranja (#fbbf24) com fundo e borda
    - ℹ️ INFO: Azul claro (#60a5fa)
    - 🔍 DEBUG/TRACE: Roxo (#c084fc) com fonte menor
    - ✅ SUCCESS/COMPLETED: Verde (#4ade80)
    - 🔴 HTTP 4xx/5xx: Laranja (#fb923c) com fundo
    - HTTP 2xx/3xx: Verde claro (#86efac)
  - **YAML Fullscreen Modal**: Botão "Expandir" no painel YAML (estilo ConfigMaps) com Monaco Editor
  - **kubectl describe**: Botão "Describe" em todas as abas (Pods, ConfigMaps, Secrets, Deployments)
    - Backend: Executa `kubectl describe {resourceType} {name}` via `internal/kubernetes/client.go:ExecuteKubectlDescribe()`
    - API: `GET /api/v1/{resource}/:cluster/:namespace/:name/describe`
    - Handlers: `configmaps.go`, `secrets.go`, `deployments.go`, `pods.go`
    - Frontend: Modal com ScrollArea exibindo output completo do kubectl describe
  - **Header Compacto no Painel de Detalhes (v1.3.6 - 23/12/2025)**:
    - Gauges de CPU/Memory isolados com `position: absolute` (não mais afetam altura do header)
    - Namespace e versão transformados em badges `variant="secondary"`
    - Badges de status (Running, container count) agrupados na mesma linha
    - Estrutura otimizada:
      - Linha 1: Nome do pod
      - Linha 2: Badge NS + Badge versão + Badge status + Badge container count
      - Linha 3: Node, IP, Age, Restarts
      - Linha 4: CPU/R, CPU/L, MEM/R, MEM/L
    - Redução de ~40% na altura do header sem perda de informações
✅ **RBAC com Azure AD (v1.3.5+)** - Controle de acesso baseado em grupos do Azure AD
  - **Grupo SRE**: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
  - **Backend**: Módulo `internal/rbac/azure_ad.go` com verificação via Azure CLI (`az ad user get-member-groups`)
  - **Middleware**: `internal/web/middleware/rbac.go` protege rotas sensíveis (POST, PUT, DELETE)
  - **Frontend**: Hook `useUserPermissions()` + componente `<ProtectedAction>` para ocultar/desabilitar botões
  - **Cache**: TTL de 1 hora para permissões, invalidação manual via endpoint `/permissions/refresh`
  - **Recursos Protegidos**: Apply (HPAs, Node Pools, ConfigMaps, Namespaces), Delete (todos os recursos), Cordon/Drain
  - **Recursos Públicos**: Visualização (GET), Staging area, Sessions, Monitoramento, Logs
  - **Badge de Status**: Componente `<SREBadge>` exibe status SRE no header com popover de grupos
  - **Testes**: Suite completa (`./testes/test-rbac.sh`) + testes Go unitários (`go test ./internal/rbac`)
  - **Documentação**: [RBAC_AZURE_AD_IMPLEMENTATION.md](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md) + [RBAC_SUMMARY.md](docs/guides/RBAC_SUMMARY.md)
✅ **AI Diagnostics (v1.3.6 - Produção)** - Análise inteligente de problemas Kubernetes com IA
  - **Status**: ✅ Backend 100% | ✅ Frontend 100% | ✅ Integração completa
  - **Providers**: Ollama local (llama3.2:3b padrão, compatível com modelos <3B) + Claude (via API key)
  - **Modelo Recomendado**: llama3.2:3b (2GB RAM, rápido, eficiente para análise de logs)
  - **Constraint**: Sistema com 6.1GB RAM disponível - modelos >3B causam timeout
  - **Recursos Suportados**: Pods, Deployments, HPAs, Nodes
  - **Backend Completo (5 módulos, 20 arquivos, ~2.500 linhas)**:
    - ✅ `internal/sanitizer/` - **Sanitização inteligente e seletiva** (v1.3.6 - 24/12/2025)
      - **IPs NÃO são mascarados**: `192.168.1.100` → `192.168.1.100`
      - **Emails NÃO são mascarados**: `usuario@example.com` → `usuario@example.com`
      - **Certificados mascarados** (formato `[tipo:nome]`):
        - `certificado-tls.cert` → `[cert:certificado-tls]`
        - `app-private.key` → `[key:app-private]`
      - **Connection strings** (senha mascarada - 4 primeiros + 3 últimos):
        - `user:s6Yxbn1I9i98GHIJcJdc@host` → `user:s6Yx*************Jdc@host`
        - `mongodb://user:MyP@ss@host:27017/` → `mongodb://user:MyP@****ss@host:27017/`
      - **Base64 >30 chars** (3 primeiros + 3/4 últimos, mantém "=" se existir):
        - `MDFhghthghthghthghthghthghthghtTRk4=` → `MDF*****************************Rk4=`
      - Stack traces preservados integralmente para legibilidade
    - ✅ `internal/collectors/` - Coleta de contexto incluindo **logs anteriores** (últimas 30 linhas antes do crash)
    - ✅ `internal/storage/` - SQLite + histórico persistente (./build/ai_diagnostics.db)
    - ✅ `internal/ai/` - Providers (Ollama/Claude) + Analyzer + Prompts especializados
    - ✅ `internal/web/handlers/ai_diagnostics.go` - 6 endpoints REST API
  - **API REST**: `/api/v1/ai/analyze`, `/api/v1/ai/history`, `/api/v1/ai/status`, `/api/v1/ai/stats`
  - **Frontend React**:
    - ✅ Botão "Analisar com AI" integrado ao **painel de detalhes de Pods** (lado direito)
    - ✅ Modal de análise com markdown formatado, badges de prioridade, sugestões expandíveis
    - ✅ Indicador de provider/modelo em uso (Ollama llama3.2:3b ou Claude)
    - ✅ UX otimizada: botão apenas em painel de detalhes (não na lista de cards)
  - **Funcionalidades**:
    - Análise de problemas (CrashLoopBackOff, maxed out HPAs, node pressure, etc)
    - Extração automática de sugestões + comandos kubectl
    - Inferência de prioridade (critical/high/medium/low)
    - Histórico completo com filtros (cluster, namespace, resource, data, provider)
    - **Sanitização inteligente e seletiva** (atualizada 24/12/2025):
      - IPs e emails **não são mascarados** (análise completa de conectividade)
      - Certificados mascarados formato `[tipo:nome]`: `app.cert` → `[cert:app]`, `app.key` → `[key:app]`
      - Senhas em connection strings: 4 primeiros + 3 últimos chars visíveis (suporta `@` interno)
      - Base64 >30 chars: 3 primeiros + 3/4 últimos (mantém "=" se existir)
      - Stack traces preservados integralmente para legibilidade
    - **Análise de crash logs**: Envia últimas 30 linhas antes do restart (logs anteriores)
  - **Inicialização**: `./build/k8s-hpa-manager web --ai-provider ollama --ollama-model llama3.2:3b`
  - **Limitações Conhecidas**:
    - llama3.2:3b tem capacidade limitada (3B parâmetros) - análises menos profundas que Claude/Gemini
    - Modelos maiores (qwen2.5:14b, deepseek-r1:7b) causam timeout ou falha por falta de RAM
    - Para melhor qualidade: usar Claude (pago) ou Gemini (grátis com API key)
  - **⚠️ IMPORTANTE - Qualidade de Análise (24/12/2025)**:
    - **llama3.2:3b**: Análises SUPERFICIAIS - identifica sintomas mas não investiga causas profundas
      - Exemplo ruim: "Timeout MongoDB → restart deployment" ❌ (workaround, não solução)
    - **Claude API (Recomendado)**: Análises PROFUNDAS com investigação de causa raiz
      - Exemplo bom: "Timeout MongoDB → verificar ConfigMap connection string → validar Secret credenciais → testar conectividade → verificar DNS" ✅
    - **Comando para Claude**: `./build/k8s-hpa-manager web --ai-provider claude`
    - **Variável de ambiente**: `export ANTHROPIC_API_KEY=sk-ant-...`
    - **Melhorias no prompt (24/12/2025)**:
      - Template agora FORÇA análise profunda de causa raiz (não apenas sintomas)
      - Instruções específicas para timeouts de conexão (DB, API, services)
      - Comandos de investigação (kubectl get configmap/secret/service, nslookup, nc)
      - Evita workarounds temporários (restart sem investigar causa)
  - **Documentação**: [PLANO_AI_DIAGNOSTICS.md](PLANO_AI_DIAGNOSTICS.md) | [PROGRESSO_AI_DIAGNOSTICS.md](PROGRESSO_AI_DIAGNOSTICS.md)
✅ **Análise Preditiva (v1.3.8+ - Produção desde 04/01/2026)** - Sistema completo de análise preditiva de deployments
✅ **Health Checking Aprimorado (v1.3.9)** - Melhorias de precisão, métricas e resiliência
  - Seletores de pods agora usam `LabelSelectorAsSelector`, respeitando `matchLabels` e `matchExpressions`
  - Deployments sem `spec.replicas` deixam de causar panic (assume 1 réplica e registra aviso)
  - Coleta de métricas via Metrics Server/Prometheus para preencher `CPUUsagePercent` e `MemoryUsagePercent`
  - Reaproveitamento da listagem inicial de deployments, reduzindo chamadas redundantes à API Kubernetes
  - Timeouts configuráveis propagados para listagem de deployments/pods, coleta de métricas e análise individual
  - Eventos SSE sem emojis; modal de progresso exibe mensagens textuais "Crítico/Aviso" alinhadas ao novo guia de UI
  - **Status**: ✅ Backend 100% | ✅ Frontend 100% | ✅ IA Integration 100% | ✅ Exportação PDF/MD 100%
  - **Funcionalidades Principais**:
    - ✅ **Métricas Temporais**: Coleta de snapshots em 5 pontos (atual, D-3, D-7, D-10, D-14)
    - ✅ **Análise de Tendências**: CPU/Memory/ErrorRate/Latency com detecção automática de direção
    - ✅ **Health Score 0-100**: Breakdown por componente (Availability 30%, Performance 30%, Stability 25%, Efficiency 15%)
    - ✅ **Previsões com IA**: Short/Medium/Long term predictions com severidade e probabilidade
    - ✅ **Root Cause Analysis**: Análise de causa raiz com evidências e remediação
    - ✅ **Análise de Capacidade para Crescimento**: Cálculo realista de max réplicas
      - Min/Max/Current nodes do node pool
      - Aplicações concorrentes com réplicas e consumo per-replica
      - 3 cenários: nodes atuais, max nodes, remover concorrentes
      - Identificação de bottleneck resource (CPU/Memory)
    - ✅ **Recomendações Priorizadas**: Sistema 1-5 com estimativa de implementação e ganho de eficiência
    - ✅ **Relatórios Profissionais**: PDF e Markdown sem emojis (jsPDF compatible)
    - ✅ **Modal Completo**: Todas informações dos relatórios também no modal (DADOS ANALISADOS + ANÁLISE DE CRESCIMENTO)
    - ✅ **Histórico Persistente**: SQLite com filtros avançados e busca
  - **Queries Prometheus Corrigidas** (Bug Crítico Resolvido):
    - ❌ ANTES: `avg() by (pod)` retornava vetor → queryScalar() pegava v[0] → 0.00
    - ✅ DEPOIS: `sum()` com filtros `container!="",container!="POD"` → escalar único
    - ✅ Compatibilidade v1.x/v2.x: `kube_node_status_capacity_cpu_cores or kube_node_status_capacity{resource="cpu"}`
  - **Arquivos Principais**:
    - `internal/monitoring/predictions/collector.go` (~960 linhas) - Coleta de métricas e capacidade
    - `internal/monitoring/predictions/queries.go` - Queries Prometheus corrigidas
    - `internal/monitoring/predictions/models.go` - Estruturas GrowthCapacityAnalysis
    - `internal/web/handlers/predictions.go` - API REST + geração de relatórios MD
    - `internal/web/frontend/src/components/DeploymentsTab.tsx` (~2800 linhas) - Modal completo
    - `internal/storage/predictions_store.go` - Persistência SQLite
  - **API REST**:
    - `POST /api/v1/predictions/analyze` - Iniciar análise
    - `GET /api/v1/predictions/report/:id/markdown` - Exportar MD
    - `GET /api/v1/predictions/history` - Histórico com filtros
  - **Documentação**: [PREDICTIVE_ANALYSIS_FEATURES.md](PREDICTIVE_ANALYSIS_FEATURES.md)

✅ **Health Checking (v1.3.7+ - Produção desde 28/12/2025)** - Sistema completo de verificação de saúde de clusters
  - **Status**: ✅ Backend 100% | ✅ Frontend 100% | ✅ SSE Progress 100% | ✅ **Multi-Cluster com Tabs** 100% | ✅ **Logs Persistentes** 100%
  - **Funcionalidades**:
    - ✅ **Multi-Cluster com Tabs (NOVO - 28/12/2025)**: Execução paralela em múltiplos clusters com tabs independentes
      - Worker pool paralelo no backend (sessionID único por cluster: `baseSessionID-clusterName`)
      - Tabs component (shadcn/ui) com ícone de status (✅ completo, ❌ falhou)
      - Progresso independente por cluster via SSE (só activeTab conecta)
      - Contador global: "Health Check em Progresso (2/3)" no header
      - Caso especial: 1 cluster apenas - não mostra tabs (UX otimizada)
    - ✅ **Verificação de Deployments**: Status de réplicas, containers crash, image pull errors, probes (liveness/readiness)
    - ✅ **Verificação de Services**: Testes de conectividade (MongoDB, Redis, PostgreSQL, Kafka, EventHub, HTTP) - *placeholder*
    - ✅ **Verificação de Configs**: Validação de ConfigMaps/Secrets - *placeholder*
    - ✅ **Logs Persistentes (NOVO - 31/12/2025)**: Sistema completo de persistência e visualização de logs históricos
      - **Backend SQLite**: Eventos salvos automaticamente na tabela `health_check_events` (./build/health_check.db)
      - **Persistência Automática**: Cada evento SSE é salvo no banco via `orchestrator.go:publishProgress()`
      - **Badges Clicáveis**: 4 badges (Healthy, Warning, Critical, Total) com contadores visuais
      - **Visualização de Histórico**: Clique nos badges abre modal com logs completos do banco
      - **Modo Visualização**: Modal desabilita SSE e exibe apenas eventos pré-carregados
      - **API REST**: `GET /api/v1/healthcheck/events/:sessionId` retorna eventos persistidos
      - **Frontend**: Handler `handleShowProgress()` busca eventos e passa para modal via `preloadedEvents`
      - **Arquivos**: `storage.go` (tabela + CRUD), `orchestrator.go` (auto-save), `handlers/healthcheck.go` (endpoint)
      - **Documentação**: [IMPLEMENTACAO_LOGS_PERSISTENTES.md](IMPLEMENTACAO_LOGS_PERSISTENTES.md)
    - ✅ **Progresso Dinâmico**: Barra de progresso adapta-se aos checks selecionados (0-100% para 1 check, divide proporcionalmente para múltiplos)
    - ✅ **Eventos Detalhados**: Log em tempo real com detalhes de cada problema (máximo 10 críticos + 10 warnings)
    - ✅ **Filtros de Status**: Healthy, Warning, Critical com contadores ao vivo
  - **Backend (5 arquivos, ~800 linhas)**:
    - `internal/healthcheck/models.go` - Estruturas de dados (HealthStatus, DeploymentHealth, ServiceHealth, ConfigHealth)
    - `internal/healthcheck/orchestrator.go` - Orquestrador principal com worker pool paralelo
      - `executeMultiClusterCheck()` - Worker pool com sessionID único por cluster
      - `GetClusterSessionMapping()` - Retorna map: cluster -> sessionID
      - `ResolveClusters()` - Público (resolve clusters por environment ou lista manual)
    - `internal/healthcheck/deployment_checker.go` - Valida Deployments (réplicas, crashes, probes)
    - `internal/healthcheck/service_checker.go` - Testa conectividade de serviços externos (*placeholder*)
    - `internal/healthcheck/config_checker.go` - Valida ConfigMaps/Secrets (*placeholder*)
    - `internal/web/handlers/healthcheck.go` - REST API + SSE endpoints
      - `Run()` retorna `cluster_sessions: Record<string, string>` na response
  - **Frontend React**:
    - `HealthCheckingTab.tsx` - Tab principal com configuração + execução (multi-select já existia)
    - `HealthCheckProgressModal.tsx` - **REFATORADO para Tabs** (500 linhas)
      - Componente filho: `ClusterTabContent` (gerencia SSE progress de cada cluster)
      - Tabs component com ícones de status nos triggers (Server + CheckCircle2/XCircle)
      - Enabled prop evita múltiplas conexões SSE simultâneas (só activeTab conecta)
      - Estado global: completedClusters, failedClusters (Sets)
    - Hook `useHealthCheckProgress()` - Gerencia conexão SSE e eventos
    - Hook `useHealthChecking()` - Agora retorna `clusterSessions` além de `sessionId`
  - **API REST**:
    - `POST /api/v1/healthcheck/run` - Inicia health check
      - Request: `{clusters: string[], check_deployments: bool, ...}`
      - Response: `{session_id: string, cluster_sessions: Record<string, string>}`
      - Exemplo: `cluster_sessions: {"cluster-a": "uuid-cluster-a", "cluster-b": "uuid-cluster-b"}`
    - `GET /api/v1/healthcheck/progress?session={id}` - SSE stream de progresso (por cluster)
    - `GET /api/v1/healthcheck/{id}` - Busca resultado completo
  - **Progresso Dinâmico**:
    - Checks habilitados determinam faixas de progresso (5-95%)
    - 1 check selecionado: 0-100% dedicado (ex: só Deployments)
    - 3 checks selecionados: divide em 30% cada (Deployments 5-35%, Services 35-65%, Configs 65-95%)
    - Progresso incremental dentro de cada fase (atual/total)
  - **Limitações Conhecidas**:
    - Service Checker e Config Checker são *placeholders* (retornam listas vazias)
    - Máximo 10 eventos críticos + 10 warnings exibidos no log (evita sobrecarga UI)
    - Delay de 50ms entre eventos detalhados (processamento suave)
  - **UI/UX**:
    - Textos com quebra de linha forçada (`word-break`, `overflow-wrap`, `whitespace: normal`)
    - ScrollArea com `overflow-hidden` para evitar texto vazar fora do modal
    - Badges coloridos (verde/amarelo/vermelho) para status visual
    - Mensagens detalhadas: "❌ Deployment namespace/nome: mensagem do erro"
    - **Modal de Progresso**: Largura aumentada para 1280px (max-w-7xl) - 43% maior que anterior (896px)
      - Evita scroll confuso ao analisar 4+ clusters simultaneamente
      - Melhor visualização de tabs e progresso paralelo
  - **Filtros de Histórico (NOVO - 02/01/2026)**:
    - ✅ **Date Picker com Range**: Seleção de período customizável no modal de histórico
      - Componente: react-day-picker com `mode="range"` (Calendar shadcn/ui)
      - Suporta seleção de data única (only "from") ou range completo (from → to)
      - Dual calendar (2 meses side-by-side) para fácil seleção
      - Integrado com filtros existentes (Hoje, Última Semana, Último Mês)
      - Desabilita datas futuras (`disabled={(date) => date > new Date()}`)
      - Formato visual: "DD MMM YYYY" (ex: "25 Dez 2025")
    - Arquivo: `HealthCheckHistoryModal.tsx` (linhas 40-55, 180-215)
  - **Sistema de Exportação de Relatórios (NOVO - 02/01/2026)**:
    - ✅ **3 Formatos Profissionais**: PDF, Markdown (.md), CSV
    - ✅ **Dados Exportados**:
      - Warnings e Criticals: Detalhes completos (fase, descrição, severidade)
      - Healthies: Apenas mencionados (totais por cluster)
      - Metadados: Data da análise, duração, total de checks
    - ✅ **Design Profissional** (sem emojis):
      - **PDF**: Cabeçalho azul (RGB 41,128,185), tabelas com jsPDF-autoTable
        - Sumário executivo com métricas agregadas
        - Detalhes por cluster com color-coding (warnings=laranja, critical=vermelho, healthy=verde)
        - Filename: `health-check-report-YYYY-MM-DD.pdf` (data da análise)
      - **Markdown**: Estrutura com tabelas e blockquotes
        - Headers em CAPS (SUMÁRIO EXECUTIVO, ANÁLISE DETALHADA)
        - Tabelas markdown para métricas e alertas
        - Filename: `health-check-report-YYYY-MM-DD.md`
      - **CSV**: Formato tabular para Excel/BI
        - Headers: Data_Analise, Cluster, Status, Duracao_ms, Total_Checks, Healthy, Warnings, Critical, Tipo_Alerta, Fase, Mensagem
        - Escape de aspas duplas e quebras de linha
        - Filename: `health-check-report-YYYY-MM-DD.csv`
    - ✅ **Botão de Exportação**:
      - Painel "Resultados": Exporta análise atual (após execução)
      - Modal de Histórico: Exporta resultados filtrados (por período/cluster)
      - Modal de seleção de formato com preview de resumo
    - ✅ **Timestamp Correto**: Usa data real da análise (`started_at`), não data atual
      - Fix crítico: Exportações agora refletem quando análise foi executada
      - Cálculo: `Math.max(...timestamps)` para pegar análise mais recente
    - Arquivos:
      - `lib/reportGenerator.ts` - Geração de PDF/Markdown/CSV (440 linhas)
      - `ExportReportModal.tsx` - Seleção de formato (187 linhas)
      - `HealthCheckingTab.tsx` - Botão no painel Resultados
      - `HealthCheckHistoryModal.tsx` - Botão no header do histórico
    - Dependências: jspdf 2.5.2, jspdf-autotable 3.8.4, react-day-picker 9.4.3
  - **Correções Críticas (03/01/2026)**:
    - ✅ **Fix UTF-8 em PDF**: Emojis (⚠️, ✅, ❌) quebravam encoding do jsPDF
      - Solução: Função `removeEmojis()` com Unicode ranges específicos
      - Remove apenas emojis, preserva texto completo (português, acentos, etc)
      - Aplicado em: modal de visualização + todas as exportações (PDF/MD/CSV)
      - Regex: `[\u{1F300}-\u{1F9FF}]`, `[\u{2600}-\u{26FF}]`, `[\u{2700}-\u{27BF}]`, `\uFE0F`
    - ✅ **Fix Crítico - Filtros Aplicados Após Salvar**: Alertas da whitelist apareciam no banco/exportações
      - **Root Cause**: `publishProgress()` salvava eventos ANTES de aplicar filtros (orchestrator.go:553)
      - **Solução**: Aplicar filtros nos callbacks ANTES de chamar `publishProgress()`
      - **Modificações**: Callbacks de deployments (176-189), services (224-235), configs (259-270)
      - **Resultado**: Eventos filtrados NÃO são publicados via SSE, NÃO salvos no banco, NÃO aparecem em exportações
      - **Arquivos**: `internal/healthcheck/orchestrator.go`, `HealthCheckAlertsExportModal.tsx`, `HealthCheckAlertsReport.tsx`

---

## 🎯 Para Novos Chats do Claude

**Context Template Rápido:**
```
Projeto: Kubernetes HPA + Azure AKS Node Pool Manager

Repositório: git@github.com:Paulo-Ribeiro-Log/New-K8S-HPA-Manager.git
Branch Principal: new-k8s-hpa-dev (desenvolvimento contínuo)
Versão Atual: v1.3.1 (última release estável)
Tech: Go 1.24.0+ + React 18.3.1 + TypeScript 5.8.3
Build: make build && make web-build
Binary: ./build/new-k8s-hpa
Servidor Web: ./build/new-k8s-hpa web (porta 8080)

Recent Updates (v1.3.9 - 05/01/2026):
- **Refatoração Crítica: Cálculo de Crescimento de Réplicas (Growth Analysis)**:
  - **Problema**: Cálculo incorreto usando capacidade total do cluster ao invés de cálculo per-node
    - Ignorava que aplicações concorrentes também escalam proporcionalmente com nodes
    - Não considerava overhead de sistema (kubelet, kube-system) → estimativas irreais
  - **Solução**: Refatoração completa da função `calculateGrowthAnalysis()` (`collector.go:1222-1473`)
    - ✅ **Cálculo per-node**: CPU/Memory calculados por VM individual, não cluster-wide
    - ✅ **Safety Margin 15%**: Reserva automática para kubelet e pods do sistema (`const safetyMargin = 0.85`)
    - ✅ **Competing Apps Scaling**: Aplicações concorrentes escalam proporcionalmente ao número de nodes
      - Cenário 1 (current nodes): usa valores atuais de competing apps
      - Cenário 2 (max nodes): aplica `scaleFactor = maxNodes/currentNodes` para competing apps
    - ✅ **Logging Detalhado**: 30+ linhas de log explicando cada passo do cálculo
  - **Fórmula Corrigida**:
    ```go
    // ANTES (INCORRETO)
    maxReplicas = totalClusterCPU / appCPUPerReplica

    // DEPOIS (CORRETO)
    usableCPUPerNode := cpuPerVM * 0.85 // 15% overhead
    availableCPUPerNode := usableCPUPerNode - competingCPUPerNode
    maxReplicasPerNode := availableCPUPerNode / appCPUPerReplica
    maxReplicasTotal := maxReplicasPerNode * numberOfNodes
    ```
  - **Impacto**: Estimativas de capacidade agora são realistas e consideram limitações de infraestrutura
  - Arquivo: `internal/monitoring/predictions/collector.go` (linhas 1222-1473)

- **Menu de Operações em Deployments (3-dot menu)**:
  - **Feature**: Menu dropdown com operações destrutivas protegidas por RBAC
  - **Backend (Go)**:
    - ✅ Endpoint `DELETE /deployments/:cluster/:namespace/:name` - Deletar deployment
    - ✅ Endpoint `POST /deployments/:cluster/:namespace/:name/restart` - Rollout restart
    - ✅ RBAC: Ambos endpoints protegidos com `rbacMiddleware.RequireSREGroup()`
    - ✅ Handlers: `deployments.go:Delete()` (416-495), `RolloutRestart()` (497-536)
    - ✅ Client methods: `client.go:DeleteDeployment()` (1732-1740), `RolloutRestartDeployment()` (1727-1730)
  - **Frontend (React/TypeScript)**:
    - ✅ Dropdown menu com ícone `MoreVertical` (3 pontos) no header do painel "Visualização"
    - ✅ 2 opções: **Rollout Restart** (ícone RotateCw) + **Deletar Deployment** (ícone Trash2, cor vermelha)
    - ✅ Modais de confirmação para ambas operações com detalhes do deployment
    - ✅ Loading states: `isDeleting`, `isRestarting` com spinners e botões desabilitados
    - ✅ Toast notifications: Sucesso/erro com descrição detalhada
    - ✅ Auto-refresh: Lista de deployments atualizada após delete, manifest recarregado após restart
    - ✅ Protected actions: Menu completo encapsulado em `<ProtectedAction requiredGroup="SRE">`
  - **Segurança**: Operações destrutivas disponíveis apenas para usuários do grupo SRE do Azure AD
  - Arquivos modificados:
    - Backend: `deployments.go`, `client.go`, `server.go` (rotas 509-510)
    - Frontend: `DeploymentsTab.tsx` (imports, state, handlers, modals em ~100 linhas adicionadas)

- **Fix Crítico: Timestamps Incorretos na Análise Preditiva**:
  - **Problema**: Previsões mostravam datas futuras irreais (ex: aplicação de 2024 com previsão para 2026)
    - Root Cause: Timestamps eram calculados pela IA usando `time.Now()` ao invés do timestamp real das métricas
    - Timestamps não eram validados nem calculados pelo backend, dependendo da IA
  - **Solução**: Implementação de cálculo determinístico de timestamps no backend
    - ✅ **Novo campo**: Adicionado `Timestamp *time.Time` ao struct `Prediction` (`models.go:277`)
    - ✅ **Função de enriquecimento**: `enrichPredictionsWithTimestamps()` no analyzer (`analyzer.go:542-596`)
      - Calcula timestamps baseados em `metrics.Current.Timestamp` (timestamp real das métricas)
      - Suporta múltiplos formatos: "4h", "24h", "7d", "próximas 4 horas", "curto prazo", etc
      - Parsing automático de formatos dinâmicos ("Xh", "Xd")
    - ✅ **Integração**: Enriquecimento automático após análise da IA (`analyzer.go:96`)
      - `a.enrichPredictionsWithTimestamps(&result.Predictions, metrics.Current.Timestamp)`
      - Predictions agora têm timestamps precisos: `baseTimestamp + timeframe_offset`
  - **Lógica Correta**:
    ```go
    // Short-term (4h): metrics.Current.Timestamp + 4 horas
    // Medium-term (24h): metrics.Current.Timestamp + 24 horas
    // Long-term (7d): metrics.Current.Timestamp + 7 dias
    ```
  - **Impacto**: Previsões agora exibem timestamps corretos relativos ao momento da coleta das métricas
  - Arquivos modificados:
    - `internal/monitoring/predictions/models.go` (linha 277)
    - `internal/monitoring/predictions/analyzer.go` (linhas 96, 542-596)

Recent Updates (v1.3.8 - 04/01/2026):
- **Análise Preditiva - Sistema Completo em Produção**:
  - **Problema Resolvido**: Métricas retornando 0.00 (CPU/Memory)
    - Root Cause: Queries `avg() by (pod)` retornavam vetor, `queryScalar()` pegava apenas v[0]
    - Solução: Mudança para `sum()` com filtros `container!="",container!="POD"` → escalar único
  - **Compatibilidade Prometheus v1.x/v2.x**:
    - Queries com OR operator: `kube_node_status_capacity_cpu_cores or kube_node_status_capacity{resource="cpu"}`
    - Fallback automático entre formatos de métricas
  - **Análise de Capacidade para Crescimento Horizontal**:
    - Implementação completa com GrowthCapacityAnalysis
    - Min/Max/Current nodes do node pool (Azure AKS)
    - Aplicações concorrentes com réplicas e consumo per-replica
    - 3 cenários de escalabilidade: nodes atuais, max nodes, remover concorrentes
    - Identificação automática de bottleneck resource (CPU vs Memory)
  - **Modal Enriquecido**: Adicionadas seções "DADOS ANALISADOS" e "ANÁLISE DE CRESCIMENTO"
    - Grid de métricas de réplicas (4 cards)
    - Consumo de recursos com trends coloridos
    - Capacidade do cluster (total, utilização, nodes)
    - Node Pool configuration (min/max/current)
    - Tabela de aplicações concorrentes (scrollable, 7 colunas)
    - Tabela de cenários de escalabilidade (3 rows)
    - Recomendação final destacada com max réplicas sugeridas
  - **Relatórios Profissionais**: Exportação PDF e Markdown sem emojis (jsPDF compatible)
    - Seção completa de análise de crescimento com tabelas formatadas
    - Filename: `predicao_{deployment}_{timestamp}.{pdf|md}`
  - **Health Score Detalhado**: 0-100 com breakdown (Availability/Performance/Stability/Efficiency)
  - **IA Integration**: Previsões short/medium/long term + Root Cause Analysis
  - **Histórico Persistente**: SQLite (predictions.db) com filtros avançados
  - Arquivos: `collector.go` (~960 linhas), `DeploymentsTab.tsx` (~2800 linhas), `queries.go` (queries corrigidas)
  - Documentação: [PREDICTIVE_ANALYSIS_FEATURES.md](PREDICTIVE_ANALYSIS_FEATURES.md)

Recent Updates (v1.3.7 - 03/01/2026):
- **Fix Crítico: Requisições ao Cluster Antigo Após Troca de Contexto**:
  - **Problema**: Ao trocar de cluster (ex: abastecimento-hlg → faturamento-prd), React Query continuava fazendo requisições ao cluster antigo
  - **Root Cause**: Hook `refetchInterval` de `useQuery` recebe `query` como `undefined` na primeira renderização
  - **Solução**: Adicionado guard `if (!query) return 30000` nos 3 hooks de alertas antes de acessar `query.queryKey`
  - **Arquivos Modificados**: `internal/web/frontend/src/hooks/useAlerts.ts`
    - `useHPAAlerts()` - linhas 57-68
    - `useNodePoolAlerts()` - linhas 128-139
    - `useAlertSummary()` - linhas 155-166
  - **Fix Aplicado**:
    ```typescript
    refetchInterval: (data, query) => {
      // ✅ FIX: Guard contra query undefined na primeira renderização
      if (!query) return 30000;

      // ✅ FIX: Desabilitar refetch se query key não corresponde ao cluster atual
      const queryCluster = query.queryKey[1];
      if (queryCluster !== cluster) {
        console.log(`[useHPAAlerts] Disabling refetch for old cluster: ${queryCluster} (current: ${cluster})`);
        return false;
      }
      return 30000;
    }
    ```
  - **Impacto**: Elimina requisições espúrias ao cluster antigo, melhora performance e evita erros 500

- **Erros do Monaco Editor (Documentação)**:
  - **Erros comuns em aba anônima** (INOFENSIVOS - podem ser ignorados):
    - "Tracking Prevention blocked access to storage" - localStorage bloqueado
    - "Could not create web worker(s)" - Web Workers bloqueados, fallback para main thread
    - "Cannot use 'in' operator to search for 'then' in undefined" - Workers retornam undefined
    - "Missing requestHandler or method: doValidation/getFoldingRanges/getCodeAction" - Métodos de workers não disponíveis
  - **Funcionalidade NÃO afetada**: Editor YAML, syntax highlighting básico, autocomplete e edição funcionam normalmente
  - **Apenas se preocupar se**: Editor não aparecer, não conseguir digitar YAML, ou Apply não funcionar
  - **Solução**: Nenhuma necessária - Monaco tem fallbacks automáticos para modo síncrono

Recent Updates (v1.3.6 - 24/12/2025):
- **AI Diagnostics em Produção**: Sistema completo integrado ao servidor web
  - Frontend: Botão "Analisar com AI" no painel de detalhes de Pods
  - Backend: Integração com Ollama (llama3.2:3b) + Claude API
  - **Sanitização inteligente e seletiva** (v1.3.6 - 24/12/2025):
    - IPs e emails NÃO são mascarados (visíveis para análise completa)
    - Certificados formato `[tipo:nome]`: `app.cert` → `[cert:app]`, `app.key` → `[key:app]`
    - Connection strings (senha 4+3 chars, suporta `@` interno): `user:MyP@ss@host` → `user:MyP@****ss@host`
    - Base64 >30 chars (3+4 chars, mantém "="): `MDFhghthghthghthghthghthghthghtTRk4=` → `MDF*****************************Rk4=`
    - Stack traces preservados integralmente para legibilidade
  - **Análise de crash logs**: Sistema envia últimas 30 linhas ANTES do restart
    - Prompt destaca seção "LOGS ANTERIORES (ANTES DO CRASH)"
    - AI foca no erro real que causou o crashloop
  - **UI otimizada**: Botão apenas no painel de detalhes (removido da lista de Pods)
  - **Constraint RAM**: Sistema tem 6.1GB RAM disponível
    - Modelos >3B (qwen2.5:14b, deepseek-r1:7b) causam timeout ou falha
    - llama3.2:3b (2GB) é o maior modelo viável localmente
  - **⚠️ Melhorias no Prompt (24/12/2025)**:
    - Template refatorado para FORÇAR análise profunda de causa raiz
    - llama3.2:3b tem limitações graves (análises superficiais, workarounds ruins)
    - **RECOMENDAÇÃO**: Usar Claude API para análises complexas (`--ai-provider claude`)
    - Novo prompt inclui: análise multi-hipótese, comandos de investigação, evita workarounds
  - Comando Ollama: `./build/k8s-hpa-manager web --ai-provider ollama --ollama-model llama3.2:3b`
  - Comando Claude (Recomendado): `./build/k8s-hpa-manager web --ai-provider claude` (requer `ANTHROPIC_API_KEY`)
  - Arquivos chave:
    - `internal/sanitizer/sanitizer.go` (sanitização inteligente - v1.3.6 24/12/2025)
    - `internal/sanitizer/patterns.go` (regras: IPs/emails NÃO mascarados, certificados, connection strings, base64)
    - `internal/ai/prompts.go` (linhas 347-425 - template melhorado com análise profunda)
    - `internal/web/frontend/src/components/PodsPanel.tsx` (UI sem botão na lista)

Recent Updates (v1.3.5):
- **RBAC com Azure AD**: Sistema completo de controle de acesso
  - Frontend: 10 componentes protegidos (44 botões com `<ProtectedAction>`)
  - Backend: Middleware RBAC protegendo 20+ rotas (POST/PUT/DELETE)
  - Hook `useUserPermissions()` com cache de 1 hora
  - Flag oculta `--ad` para bypass em emergências (sem documentação)
  - Badge SRE no header, teste completo em `./testes/test-rbac.sh`
  - Módulo: `internal/rbac/azure_ad.go`, `internal/web/middleware/rbac.go`
  - Grupo: VV_CLOUD_SRE (eb865ea5-2672-49be-abc8-74c248c556b0)

Recent Updates (v1.3.4):
- Aba Namespaces: Gerenciamento completo com overview Top 5 (CPU/Memory/Pods) + **Sistema de edição avançado**
  - Dual-mode: Overview (charts) quando nenhum namespace selecionado, detalhes com Monaco Editor quando selecionado
  - CRUD completo: Criar, Visualizar, Editar YAML (com undo/redo/diff/dry-run/apply), Deletar namespaces
  - **Edição avançada** (copiado de ConfigMaps): Undo/Redo (50 versões), Diff side-by-side, Dry-run, Apply com confirmação
  - Botões duplicados em painel normal e modal fullscreen
  - Botão "Criar Namespace" no header do painel "Visualização"
  - kubectl describe integrado, modal fullscreen para edição com toolbar completa
  - **Lista de Deployments**: Botão colapsável (estilo Labels do ConfigMaps) exibindo deployments do namespace
    - Mostra ícone Package + nome + réplicas (ready/total)
    - Carregamento automático ao selecionar namespace
    - Endpoint: `GET /api/v1/deployments?cluster=...&namespaces=...`
    - Estado inicial: recolhido, click para expandir/colapsar
  - Backend: endpoints POST/GET/**PUT**/DELETE em `/namespaces/:cluster`
  - Handler Apply: `internal/web/handlers/namespaces.go:Apply()` com suporte a dry-run
  - Kubernetes client: `internal/kubernetes/client.go:ApplyNamespace()` reutilizado
  - API client: `internal/web/frontend/src/lib/api/client.ts:applyNamespace()` adicionado
  - Component: `NamespacesTab.tsx` (1250+ linhas com toda lógica de edição + deployments)

Recent Updates (v1.3.3):
- Aba Pods/Containers: Gerenciamento completo de Pods e Containers Kubernetes
  - Listagem com filtros (namespace, search, system pods)
  - Métricas inline, badge de versão extraído da imagem
  - Log highlighting com cores (ERROR vermelho, WARN amarelo, INFO azul, etc)
  - YAML fullscreen modal + kubectl describe integrado
- kubectl describe: Implementado em todas as abas (Pods, ConfigMaps, Secrets, Deployments)
  - Backend: `ExecuteKubectlDescribe()` em `internal/kubernetes/client.go`
  - API: `GET /api/v1/{resource}/:cluster/:namespace/:name/describe`

- Dashboard por Namespace (v1.3.2): Card "Top 5 Namespaces por Consumo"
  - Grid 3 colunas: CPU | Memory | Top 5 NS
  - Tabs: CPU (millicores), Memory (GB), Pods (count)
  - Prometheus agregado: sum by (namespace)
```

**Ver documentação completa:**
- [Quick Start](docs/guides/QUICK_START.md) - Estado atual e features
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md) - Comandos essenciais
- [Architecture](docs/architecture/OVERVIEW.md) - Estrutura técnica

---

## 🏗️ Conceitos de Arquitetura Críticos

### Padrão de Concorrência (Thread-Safety)
**SEMPRE usar sync.RWMutex para acesso a recursos compartilhados:**

```go
// internal/config/kubeconfig.go - Cliente K8s por cluster
var (
    clientCache  = make(map[string]*kubernetes.Clientset)
    clientMutex  sync.RWMutex  // CRÍTICO: protege criação de clients
)

// Padrão: Double-check locking
clientMutex.RLock()
if client, exists := clientCache[cluster]; exists {
    clientMutex.RUnlock()
    return client, nil
}
clientMutex.RUnlock()

// Upgrade para write lock apenas quando necessário
clientMutex.Lock()
defer clientMutex.Unlock()
// ... criação do client
```

**Bubble Tea - NUNCA usar goroutines diretas:**
```go
// ❌ ERRADO - Race condition
go func() {
    result := applyHPA()
}()

// ✅ CORRETO - Retornar tea.Cmd
return func() tea.Msg {
    err := applyHPA()
    return HPAAppliedMsg{err: err}
}
```

### Sistema de Estado (AppModel)
**`internal/models/types.go` é a ÚNICA fonte de verdade:**

```go
type AppModel struct {
    State              AppState           // Máquina de estados
    Clusters           []string           // Clusters disponíveis
    SelectedCluster    int                // Índice do cluster selecionado
    Namespaces         []string           // Namespaces do cluster
    SelectedNamespaces map[string]bool    // Multi-seleção
    HPAs               []HPAInfo          // HPAs carregados
    NodePools          []NodePoolInfo     // Node pools do cluster
    StagingChanges     []interface{}      // Staging area (pré-apply)
    // ... 40+ campos para estado completo da aplicação
}

// Transições de estado válidas
type AppState int
const (
    StateClusterSelection    // Seleção de cluster
    StateSessionSelection    // Load/Save sessão
    StateNamespaceSelection  // Seleção de namespaces
    StateResourceSelection   // HPAs/NodePools
    StateEditing            // Edição de recursos
    StateHelp               // Tela de ajuda
)
```

**NUNCA criar estado local em handlers ou views** - sempre modificar AppModel e retornar mensagem.

### Sistema de Sessões (Compatibilidade TUI ↔ Web)
**Formato JSON compartilhado** (`internal/session/manager.go`):

```json
{
  "id": "uuid-v4",
  "name": "session-name",
  "folder": "HPA-Upscale|HPA-Downscale|Node-Upscale|Node-Downscale|Mixed",
  "created_at": "2025-11-26T10:00:00Z",
  "clusters_affected": ["cluster1", "cluster2"],  // Auto-calculado
  "namespaces_affected": ["ns1", "ns2"],         // Auto-calculado
  "hpas": [...],
  "node_pools": [...]
}
```

**Diretórios organizados por tipo:**
```
~/.k8s-hpa-manager/sessions/
├── HPA-Upscale/
├── HPA-Downscale/
├── Node-Upscale/
├── Node-Downscale/
└── Mixed/
```

### Sistema SSE (Server-Sent Events)
**Progress tracking em tempo real** (`internal/web/sse/progress.go`):

```go
// Broker gerencia múltiplos clients conectados
type Broker struct {
    clients    map[chan string]bool
    newClients chan chan string
    defunctClients chan chan string
    messages   chan string
}

// Uso: Cordon/Drain com updates em tempo real
func (h *Handler) CordonDrain(c *gin.Context) {
    // 1. Criar canal SSE
    messageChan := make(chan string)
    h.sseBroker.NewClients <- messageChan

    // 2. Executar operação em goroutine
    go func() {
        for _, node := range nodes {
            h.sseBroker.Publish(fmt.Sprintf("Cordoning %s...", node))
            // ... operação
            h.sseBroker.Publish(fmt.Sprintf("✅ Done: %s", node))
        }
    }()

    // 3. Stream events para client
    c.Stream(func(w io.Writer) bool {
        if msg, ok := <-messageChan; ok {
            c.SSEvent("progress", msg)
            return true
        }
        return false
    })
}
```

### Staging Area (Pré-visualização de mudanças)
**React Context + Go Backend:**

```typescript
// internal/web/frontend/src/contexts/StagingContext.tsx
interface StagingContextType {
  stagedHPAs: HPAWithChanges[]
  stagedNodePools: NodePoolWithChanges[]
  addHPAToStaging: (hpa: HPAInfo, changes: HPAChanges) => void
  removeHPAFromStaging: (id: string) => void
  applyAll: () => Promise<void>  // Rollout com SSE progress
}

// Fluxo: Edit → Stage → Preview → Apply All
// 1. Usuário edita HPA (modal inline)
// 2. addHPAToStaging() adiciona à staging area
// 3. ApplyAllModal mostra diff antes/depois
// 4. applyAll() executa rollout com progress tracking
```

---

## 🧩 Padrões de Desenvolvimento do Projeto

### Sistema de Versionamento
**Versão injetada em tempo de build** via linker flags:

```bash
# Makefile detecta versão automaticamente
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
LDFLAGS := -X main.version=$(VERSION)

# Build com versão
go build -ldflags "$(LDFLAGS)" -o build/new-k8s-hpa
```

**NUNCA hardcodear versão no código** - usar `main.version` (injetado via build).

### Estrutura de Handlers HTTP (Backend)
**Padrão Gin + Dependency Injection:**

```go
// internal/web/handlers/example.go
type ExampleHandler struct {
    clientCache *cache.ClientCache  // Shared K8s clients
    logger      *zerolog.Logger
}

func NewExampleHandler(cc *cache.ClientCache, logger *zerolog.Logger) *ExampleHandler {
    return &ExampleHandler{clientCache: cc, logger: logger}
}

// Route registration em internal/web/routes.go
func SetupRoutes(router *gin.Engine, handlers ...) {
    v1 := router.Group("/api/v1")
    v1.GET("/resource/:cluster/:namespace", handler.GetResource)
}
```

**NUNCA** criar clientes K8s diretamente - sempre reutilizar do cache.

### Frontend - API Client Pattern
**Centralizar chamadas HTTP** (`internal/web/frontend/src/lib/api/client.ts`):

```typescript
// ✅ CORRETO
export const apiClient = {
  async getHPAs(cluster: string, namespaces: string[]): Promise<HPAInfo[]> {
    const params = new URLSearchParams({ cluster })
    namespaces.forEach(ns => params.append('namespaces', ns))

    const response = await fetch(`/api/v1/hpas?${params}`)
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    return response.json()
  }
}

// ❌ ERRADO - Não fazer fetch direto em componentes
```

### WebSocket Pattern (Terminal)
**Protocolo JSON** (`internal/web/handlers/websocket_shell.go`):

```typescript
// Frontend envia comandos via JSON
ws.send(JSON.stringify({ type: "input", data: "ls -la\n" }))
ws.send(JSON.stringify({ type: "resize", rows: 50, cols: 120 }))

// Backend responde com output base64
{ type: "output", data: "base64-encoded-terminal-output" }
```

**SEMPRE** usar `event.preventDefault()` em handlers de teclado para evitar duplicação.

### Logging Pattern (Backend)
**Structured logging com zerolog:**

```go
// SEMPRE passar logger via DI, nunca global
logger.Info().
    Str("cluster", cluster).
    Str("namespace", namespace).
    Str("hpa", hpaName).
    Msg("Applying HPA changes")

// Errors com stack trace
logger.Error().
    Err(err).
    Str("operation", "apply-hpa").
    Msg("Failed to apply HPA")
```

### React Query Pattern (Frontend)
**Cacheable API calls** com TanStack React Query:

```typescript
// hooks/useHPAs.ts
export const useHPAs = (cluster: string, namespaces: string[]) => {
  return useQuery({
    queryKey: ['hpas', cluster, namespaces],
    queryFn: () => apiClient.getHPAs(cluster, namespaces),
    staleTime: 30000,  // 30 segundos
    enabled: !!cluster && namespaces.length > 0
  })
}
```

**SEMPRE** usar `queryKey` único para invalidação de cache.

---

## ⚙️ Peculiaridades Técnicas Importantes

### Azure CLI - Ordem de Operações para Node Pools

**CRÍTICO**: Azure CLI rejeita `az aks nodepool scale` se autoscaling estiver habilitado.

**Ordem correta para 4 cenários** (`internal/tui/app.go:buildNodePoolCommands()`):

```go
// Cenário 1: Apenas MinCount/MaxCount (autoscaling já habilitado)
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y

// Cenário 2: Apenas Node Count (desabilitar autoscaling)
// 1. az aks nodepool update --disable-cluster-autoscaler
// 2. az aks nodepool scale --node-count N

// Cenário 3: MinCount/MaxCount + Node Count
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y
// 2. az aks nodepool update --disable-cluster-autoscaler
// 3. az aks nodepool scale --node-count N
// 4. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y

// Cenário 4: Apenas habilitar autoscaling (sem mudanças)
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y
```

### Validação VPN e Conectividade K8s

**Validação on-demand** - não assume que VPN está conectada:

```go
// internal/config/kubeconfig.go
func ValidateClusterConnection(cluster string) error {
    // Timeout de 5 segundos para evitar travamento
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Usa kubectl cluster-info (mais confiável que ping)
    cmd := exec.CommandContext(ctx, "kubectl", "cluster-info",
                               "--context", cluster)

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("VPN desconectada ou cluster inacessível")
    }
    return nil
}
```

**Pontos de validação:**
1. Início da aplicação (todos os clusters)
2. Ao carregar namespaces (cluster selecionado)
3. Ao carregar HPAs (cluster selecionado)
4. Timeout em operações Azure CLI (5s)

### Suffix `-admin` em Cluster Names

**Problema**: Sessions salvam sem `-admin`, mas kubeconfig contexts têm `-admin`.

**Soluções implementadas:**

```typescript
// Web: internal/web/frontend/src/contexts/StagingContext.tsx
const findClusterInConfig = (sessionCluster: string) => {
  // Remove -admin para matching
  return clusters.find(c =>
    c.name === sessionCluster ||
    c.name === `${sessionCluster}-admin`
  )
}

// Load session adiciona -admin automaticamente
const loadFromSession = (session: Session) => {
  session.hpas.forEach(hpa => {
    hpa.cluster = ensureAdminSuffix(hpa.cluster)  // akspriv-prod → akspriv-prod-admin
  })
}
```

### Web Interface - Hard Refresh Obrigatório

**Após `./rebuild-web.sh -b`, SEMPRE fazer hard refresh:**

```bash
# 1. Build
./rebuild-web.sh -b

# 2. Browser - Ctrl+Shift+R (Linux/Windows) ou Cmd+Shift+R (macOS)
# Isso limpa cache JavaScript e carrega novos assets
```

**Por que?** Vite gera nomes de arquivo com hash (ex: `index-abc123.js`), mas navegadores podem cachear `index.html` antigo.

### Bubble Tea - Texto Unicode-Safe

**SEMPRE usar `[]rune` ao invés de strings:**

```go
// ❌ ERRADO - Quebra com emojis
text := "Hello 👋"
text[0] = 'h'  // Panic ou corrupção

// ✅ CORRETO
runes := []rune("Hello 👋")
runes[0] = 'h'
text = string(runes)
```

**Cursor position também usa runes:**
```go
// internal/tui/text_input.go
type TextInput struct {
    value  []rune  // Texto como runes
    cursor int     // Posição do cursor em runes (não bytes)
}
```

---

## 📖 Navegação

Toda a documentação detalhada foi modularizada em arquivos separados na pasta `docs/`:

- **`docs/guides/`** - Guias práticos de desenvolvimento
- **`docs/architecture/`** - Documentação de arquitetura
- **`docs/history/`** - Histórico de mudanças e correções

Cada arquivo contém um link "Voltar ao CLAUDE.md principal" no topo para fácil navegação.

---

## 🔗 Links Externos

- **GitHub**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager
- **Latest Release**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/latest
- **Análise Cordon/Drain**: [ANALISE_NODEPOOL_CORDON_DRAIN.md](ANALISE_NODEPOOL_CORDON_DRAIN.md)
- **Plano Otimização Async**: [PLANO_OTIMIZACAO_ASYNC.md](PLANO_OTIMIZACAO_ASYNC.md)

---

## 🔄 Últimas Atualizações (12/12/2025)

### Service Mesh - Kiali Integration

#### Seletor Traffic Implementado
- **Traffic Accordion** com 3 protocolos:
  - **gRPC**: Requests (padrão), Received Messages, Sent Messages, Total Messages
  - **HTTP**: Requests (única opção)
  - **TCP**: Sent Bytes (padrão), Received Bytes, Total Bytes
- Accordion múltiplo (Traffic + Display podem abrir simultaneamente)
- Interface idêntica ao Kiali original

#### Sistema de Cores Dinâmico para Erros
**Problema Resolvido**: Chaves duplicadas `line-color` e `target-arrow-color` faziam apenas primeira definição (estática) ser usada.

**Solução**: Removidas definições estáticas, mantidas apenas funções dinâmicas:

```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
'line-color': function(ele: any) {
  const errorRate = parseFloat(ele.data('errorRate') || '0');
  
  // PRIORIDADE: Erros sempre aparecem primeiro
  if (errorRate >= 5) return '#ef4444';    // Vermelho (>=5%)
  if (errorRate >= 1) return '#f97316';    // Laranja (1-5%)
  if (errorRate >= 0.1) return '#eab308';  // Amarelo (0.1-1%)
  if (errorRate > 0) return '#fca5a5';     // Vermelho claro (>0%)
  
  // Sem erros: tráfego e protocolo
  if (rate > 100) return '#10b981';        // Verde (alto tráfego)
  if (protocol === 'http') return '#3b82f6'; // Azul (HTTP)
  return '#9ca3af';                        // Cinza (padrão)
}
```

#### Cálculo de Error Rate pelo Backend
**Implementação**: Backend agora calcula `errorRate` analisando `responses` do Kiali:

```go
// internal/web/handlers/servicemesh.go
// Se Kiali retorna apenas códigos de erro (4xx/5xx) com count 0.00,
// considera 100% erro (todas requisições falhando)
if errorRequests == 0 && totalRequests == 0 {
    hasErrorCodes := false
    for code := range edge.Data.Traffic.Responses {
        if len(code) > 0 && (code[0] == '4' || code[0] == '5') {
            hasErrorCodes = true
            break
        }
    }
    if hasErrorCodes {
        simpleEdge.ErrorRate = 100.0  // 🔴 Linha vermelha
    }
}
```

#### Correção do Refresh
**Problema**: Refresh removia todos elementos e recriava, causando perda de nodes.

**Solução**: Método `updateGraphDataFromResponse()` que:
- Atualiza apenas **dados** de elementos existentes
- Adiciona **novos** nodes/edges
- Remove **apenas** elementos que desapareceram
- Mantém **posições** dos nodes existentes
- Layout apenas para nodes novos

```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
const loadServiceGraphSilent = async () => {
  const data = await apiClient.getServiceGraph(...)
  
  if (cyInstance.current) {
    updateGraphDataFromResponse(data);  // ✅ Não destrói gráfico
  } else {
    setGraphData(data);  // Cria novo
  }
}
```

**Labels de Erro**: Agora exibem `X.X% err` inline com métricas de tráfego.

#### Otimização de Espaço dos Controles
**Problema**: Selects e botão "Atualizar" ocupavam muito espaço, reduzindo área do gráfico.

**Solução**: Controles compactos com larguras fixas:

```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
// Layout flexbox com larguras máximas definidas
<CardContent className="space-y-2 py-3">
  <div className="flex flex-wrap items-end gap-2">
    {/* Namespace: 130-160px */}
    <div className="min-w-[130px] max-w-[160px]">
      <SelectTrigger className="h-8 text-sm">
    
    {/* Duração: 100-120px */}
    <div className="min-w-[100px] max-w-[120px]">
      <SelectTrigger className="h-8 text-sm">
    
    {/* Tipo de Grafo: 100-120px */}
    <div className="min-w-[100px] max-w-[120px]">
      <SelectTrigger className="h-8 text-sm">
    
    {/* Botão Atualizar: 90-100px */}
    <Button className="h-8 text-xs px-3 min-w-[90px] max-w-[100px]">
```

**Melhorias**:
- Labels: `text-xs` (antes `text-sm`), margem reduzida `mb-1` (antes `mb-2`)
- Espaçamento: `gap-2` (antes `gap-3`), `py-3` (antes padrão)
- Altura: `h-8` em todos os controles (compacto e uniforme)
- Larguras: Fixadas com `min-w-` e `max-w-` (antes esticavam com `flex-1`)
- Ícones: `h-3 w-3` (antes `h-4 w-4`)

**Resultado**: ~40% menos espaço vertical, liberando mais área para visualização do gráfico.

#### Modo Fullscreen Melhorado
**Problema**: Controles Traffic e Display não estavam acessíveis no modo fullscreen.

**Solução**: 
- Popovers Traffic e Display duplicados no header do fullscreen
- Botão Settings (engrenagem) removido (não tinha funcionalidade real)
- Estado `showFullscreenControls` removido (não mais necessário)

**Implementação**:
```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
{isFullscreen && (
  <div className="flex items-center gap-2">
    {/* Popover Traffic */}
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <Filter className="h-4 w-4" />
        </Button>
      </PopoverTrigger>
      {/* ... mesmo conteúdo do modo normal */}
    </Popover>
    
    {/* Popover Display */}
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <Eye className="h-4 w-4" />
        </Button>
      </PopoverTrigger>
      {/* ... mesmo conteúdo do modo normal */}
    </Popover>
  </div>
)}
```

#### Sistema de Badges do Istio
**Funcionalidades Implementadas**:
- **Missing Sidecars** (⚠️): Detecta pods sem `istio-proxy` container
- **Security - mTLS** (🔒): Verifica PeerAuthentication STRICT (placeholder)
- **Virtual Services** (🔀): Detecta VirtualServices do Istio (placeholder)

**Backend** (`internal/web/handlers/servicemesh.go`):
```go
// SimplifiedNode com novos campos
type SimplifiedNode struct {
    // ... campos existentes
    HasSidecar        bool `json:"hasSidecar"`        // ⚠️
    HasVirtualService bool `json:"hasVirtualService"` // 🔀
    MtlsEnabled       bool `json:"mtlsEnabled"`       // 🔒
}

// Detecção de sidecar (FUNCIONAL)
func (h *ServiceMeshHandler) checkSidecar(ctx context.Context, clientset kubernetes.Interface, namespace, workloadName string) bool {
    pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("app=%s", workloadName),
    })
    if err != nil || len(pods.Items) == 0 {
        return false
    }
    
    for _, container := range pods.Items[0].Spec.Containers {
        if container.Name == "istio-proxy" {
            return true
        }
    }
    return false
}

// Detecção de mTLS (PLACEHOLDER - requer dynamic client)
func (h *ServiceMeshHandler) checkMTLS(ctx context.Context, clientset kubernetes.Interface, namespace string) bool {
    // TODO: Implementar com dynamic client para acessar PeerAuthentication do Istio
    return false
}

// Detecção de VirtualService (PLACEHOLDER - requer dynamic client)
func (h *ServiceMeshHandler) checkVirtualService(ctx context.Context, clientset kubernetes.Interface, namespace, serviceName string) bool {
    // TODO: Implementar com dynamic client para Istio CRDs
    return false
}
```

**Frontend** (`internal/web/frontend/src/components/ServiceMeshGraph.tsx`):
```typescript
// Display Options
const [displayOptions, setDisplayOptions] = useState({
  show: {
    // ... existentes
    trafficAnimation: false,
  },
  showBadges: {
    missingSidecars: false,  // ⚠️
    security: false,          // 🔒
    virtualServices: false,   // 🔀
  },
  // ...
});

// Renderização no label do node
'label': function(ele: any) {
  let label = workload || 'unknown';
  
  // Badges visuais
  const badges = [];
  if (displayOptions.showBadges.missingSidecars && hasSidecar === false) {
    badges.push('⚠️'); // Missing sidecar
  }
  if (displayOptions.showBadges.virtualServices && hasVirtualService) {
    badges.push('🔀'); // Virtual Service
  }
  if (displayOptions.showBadges.security && mtlsEnabled) {
    badges.push('🔒'); // mTLS
  }
  
  if (badges.length > 0) {
    label += '\n' + badges.join(' ');
  }
  
  return label;
}
```

**Display Popover**: Seção "Show Badges" adicionada:
```typescript
<div className="space-y-2">
  <div className="text-xs font-medium text-muted-foreground mb-2">Show Badges</div>
  <div className="flex items-center justify-between">
    <Label htmlFor="missing-sidecars">Missing Sidecars</Label>
    <Checkbox
      id="missing-sidecars"
      checked={displayOptions.showBadges.missingSidecars}
      onCheckedChange={(checked) => {
        setDisplayOptions(prev => ({
          ...prev,
          showBadges: { ...prev.showBadges, missingSidecars: checked as boolean }
        }));
      }}
    />
  </div>
  {/* Security (mTLS) e Virtual Services similar */}
</div>
```

**Status Atual**:
- ✅ **Missing Sidecars**: Totalmente funcional (detecta `istio-proxy` container)
- ⚠️ **mTLS Security**: Placeholder (retorna `false` - TODO: dynamic client)
- ⚠️ **Virtual Services**: Placeholder (retorna `false` - TODO: dynamic client)
- ✅ **Frontend**: Totalmente implementado com checkboxes e renderização de emojis

**TODOs**:
- Implementar `checkMTLS()` com dynamic client para acessar `PeerAuthentication` do Istio
- Implementar `checkVirtualService()` com dynamic client para acessar CRDs do Istio

#### Detecção de Clusters sem Istio
**Problema**: Muitos clusters não têm Istio instalado, causando erros e confusão.

**Solução**: Mensagem informativa quando Istio/Kiali não está disponível.

**Detecção Automática**:
```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
const loadNamespaces = async () => {
  try {
    const response = await apiClient.getServiceMeshNamespaces(filters.cluster);
    
    // Backend retorna null quando Kiali não está acessível
    if (!response.namespaces || response.namespaces.length === 0) {
      setIstioNotAvailable(true);
      toast.error('Istio/Kiali não está disponível neste cluster');
      return;
    }
    
    setIstioNotAvailable(false);
    setNamespaces(response.namespaces);
  } catch (error) {
    // Detectar erros específicos do Istio
    if (errorMsg.includes('kiali') || errorMsg.includes('503')) {
      setIstioNotAvailable(true);
    }
  }
}
```

**Mensagem Visual**:
```tsx
{istioNotAvailable ? (
  <div className="w-full h-full flex items-center justify-center">
    <div className="text-center space-y-4">
      <div className="text-6xl">🚫</div>
      <h3 className="text-xl font-semibold">
        Istio não disponível
      </h3>
      <p className="text-sm text-muted-foreground">
        O Istio Service Mesh não está instalado ou não está acessível neste cluster.
      </p>
      <p className="text-xs text-muted-foreground">
        Para visualizar a topologia do Service Mesh, é necessário ter o Istio e o Kiali instalados no cluster <strong>{cluster}</strong>.
      </p>
      <Button onClick={() => loadServiceGraph(false)}>
        <RefreshCw className="mr-2 h-4 w-4" /> Tentar novamente
      </Button>
    </div>
  </div>
) : (
  // Grafo normal
)}
```

**Triggers de Detecção**:
1. `response.namespaces === null` ou vazio
2. Erro com status 503 (Service Unavailable)
3. Mensagem contendo: "kiali", "istio", "service unavailable"
4. Connection refused / not found

**UX**:
- Mensagem aparece em modo normal e fullscreen
- Controles de zoom ocultos quando Istio não disponível
- Botão "Tentar novamente" para reconectar
- Toast notification informando o problema

---

---

## 🐛 Correções Recentes (Dezembro 2025)

### ✅ Fix: Bug de Duplicação do Caractere "ç" no Terminal (21/12/2025)

**Commit**: `6d4f010`

**Contexto**:
O componente `PodTerminal` é usado nas seguintes abas da interface web:
- **Aba Pods** (`PodsPanel.tsx`) - Shell em containers de pods
- **Aba Namespaces** (`NamespacesTab.tsx`) - Shell em pods do namespace
- Suporta dois modos:
  - Shell normal (exec direto no container)
  - Ephemeral Debug (container de debug temporário)

**Problema**: 
- Ao digitar "ç" no terminal (shell de pods/namespaces), o caractere era duplicado ("çç")
- Ao tentar deletar, apenas um dos "ç" era removido, o outro permanecia
- Isso tornava impossível usar corretamente códigos com caracteres especiais ABNT2
- Bug afetava ambas as abas (Pods e Namespaces) pois compartilham o mesmo componente

**Causa Raiz**:
- O código estava usando `terminal.write(char)` para escrever diretamente no terminal
- O xterm.js processava o evento novamente, duplicando o caractere
- O delete só removia a representação visual, não o caractere real no buffer

**Solução Implementada**:
```typescript
// ANTES (INCORRETO)
if (event.code === "Semicolon" && !event.ctrlKey && !event.altKey) {
  const char = event.shiftKey ? "Ç" : "ç";
  terminal.write(char);  // ❌ Escrita direta causa duplicação
  return false;
}

// DEPOIS (CORRETO)
if (event.code === "Semicolon" && !event.ctrlKey && !event.altKey) {
  event.preventDefault();  // ✅ Bloqueia processamento padrão
  const char = event.shiftKey ? "Ç" : "ç";
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "input", data: char }));  // ✅ Envia via WebSocket
  }
  return false;
}
```

**Arquivos Modificados**:
- `internal/web/frontend/src/components/PodTerminal.tsx` (componente compartilhado)

**Onde a Correção se Aplica**:
- ✅ Aba **Pods** - Shell em qualquer container de pod
- ✅ Aba **Namespaces** - Shell em pods listados no namespace
- ✅ Modo **Shell Normal** (exec direto)
- ✅ Modo **Ephemeral Debug** (container debug temporário)

**Teclas ABNT2 Corrigidas**:
- ✅ `ç/Ç` - Semicolon key
- ✅ `~/`` - Quote key (til/crase)
- ✅ `´` - BracketLeft (acento agudo)
- ✅ `[/{` - BracketRight (colchetes)
- ✅ `]/}` - Backslash (colchetes fechamento)

---

### ✅ Fix: Mensagens do Banner de Clusters Inacessíveis (21/12/2025)

**Commit**: `9c3b594`

**Problema**:
- Banner assumia que o problema era sempre "VPN Desconectada"
- Não considerava o caso de VPN conectada mas clusters desligados
- Usuários ficavam confusos quando a VPN estava OK mas clusters offline

**Solução**:
```tsx
// ANTES
<h3>VPN Desconectada - Kubernetes Inacessível</h3>
<p>Não foi possível conectar aos clusters Kubernetes. 
   Verifique se você está conectado à VPN corporativa.</p>

// DEPOIS
<h3>Clusters Kubernetes Inacessíveis</h3>
<p>Não foi possível conectar aos clusters Kubernetes. 
   Isso pode ocorrer se a VPN estiver desconectada ou 
   os clusters estiverem desligados.</p>
```

**Instruções Reorganizadas**:
1. ✅ Verifique a conexão VPN e clique em "Tentar Novamente"
2. ✅ Confirme se os clusters estão ligados e acessíveis *(NOVO)*
3. ✅ Clique em "Auto-Descobrir Clusters" ou execute comando
4. ✅ Verifique se o kubectl está configurado corretamente

**Arquivos Modificados**:
- `internal/web/frontend/src/components/VPNWarningBanner.tsx`

**Benefícios**:
- Mensagem mais precisa e menos assumptiva
- Cobre ambos os cenários: VPN OFF ou clusters desligados
- Melhor experiência do usuário (UX) com troubleshooting mais claro

---

---

## 🔍 Troubleshooting - Problemas Comuns

### Frontend não atualiza após mudanças
**Sintoma**: Alterações no código React/TypeScript não aparecem no navegador.

**Solução**:
```bash
# 1. Rebuild completo (limpa cache do Vite)
./rebuild-web.sh -b

# 2. Hard refresh no navegador
# Linux/Windows: Ctrl+Shift+R
# macOS: Cmd+Shift+R

# 3. Se persistir, limpar cache manualmente
rm -rf internal/web/frontend/node_modules/.vite
rm -rf internal/web/frontend/dist
make web-build
```

### Terminal duplica caracteres especiais (ç, ~, etc)
**Sintoma**: Ao digitar "ç" no terminal de pods, aparece "çç".

**Causa**: Handler de teclado processando evento duas vezes (xterm.js + browser).

**Solução** (já corrigida em `PodTerminal.tsx`):
- ✅ Usar `event.preventDefault()` ANTES de enviar via WebSocket
- ✅ Mapear códigos físicos (Semicolon, Quote, etc) para caracteres ABNT2
- ❌ NUNCA usar `terminal.write()` diretamente em key handlers

### Clusters aparecem como inacessíveis
**Sintoma**: Banner "VPN Desconectada" mesmo com VPN conectada.

**Causas possíveis**:
1. VPN realmente desconectada
2. Clusters desligados (shutdown programado)
3. Timeout de validação (5s padrão)

**Diagnóstico**:
```bash
# Validar conexão manualmente
kubectl cluster-info --context <cluster-name>

# Aumentar timeout (se clusters lentos)
# Ver internal/config/kubeconfig.go:ValidateClusterConnection()
```

### Istio/Kiali não disponível
**Sintoma**: Service Mesh Graph exibe mensagem "Istio não disponível".

**Diagnóstico**:
```bash
# Verificar se Kiali está instalado
kubectl get svc -n istio-system kiali

# Testar acesso direto
./scripts/diagnose-kiali-503.sh

# Configurar autenticação anônima (se necessário)
./scripts/configure-kiali-anonymous.sh
```

### Race conditions em testes
**Sintoma**: `go test -race` reporta race conditions.

**Áreas críticas**:
- `clientCache` em `internal/config/kubeconfig.go` - sempre usar `sync.RWMutex`
- Bubble Tea - NUNCA usar goroutines diretas, sempre `tea.Cmd`

**Teste específico**:
```bash
go test -v ./internal/config -race
```

### Build falha com "version not found"
**Sintoma**: `make build` falha ao detectar versão.

**Solução**:
```bash
# Git tags locais corrompidos - limpar
git fetch --tags --prune

# Forçar versão manualmente
VERSION=v1.3.1 make build
```

### AI Diagnostics não funciona
**Sintoma**: Botão "Analisar com AI" não responde ou retorna erro.

**Diagnóstico**:
```bash
# 1. Verificar se Ollama está rodando (local)
curl http://localhost:11434/api/tags

# 2. Verificar modelo instalado
ollama list | grep llama3.2:3b

# 3. Testar modelo manualmente
ollama run llama3.2:3b "teste"

# 4. Verificar logs do backend
tail -f /tmp/k8s-hpa-manager-web-*.log | grep -i "ai\|ollama\|claude"
```

**Soluções**:
- Se Ollama não está instalado: `curl -fsSL https://ollama.com/install.sh | sh`
- Se modelo não existe: `ollama pull llama3.2:3b`
- Se RAM insuficiente (<6GB): usar modelo menor ou Claude API
- Para Claude API: configurar `ANTHROPIC_API_KEY` e iniciar com `--ai-provider claude`

---

## 🔄 Fluxo de Desenvolvimento

### Desenvolvimento Local (TUI)
```bash
# 1. Fazer mudanças no código Go
# 2. Testar localmente
make run-dev                     # TUI com debug mode

# 3. Testar com race detector
go test -v ./internal/... -race

# 4. Build para produção
make build
./build/new-k8s-hpa             # Testar binário compilado
```

### Desenvolvimento Web (Frontend)
```bash
# 1. Fazer mudanças em React/TypeScript
cd internal/web/frontend

# 2. Dev mode com hot reload (Vite)
npm run dev                      # Frontend na porta 5173
# Em outro terminal: ./build/new-k8s-hpa web -f

# 3. Build para produção
cd ../../..                      # Voltar para raiz
./rebuild-web.sh -b             # Build completo + limpa cache
# IMPORTANTE: Hard refresh no navegador (Ctrl+Shift+R)

# 4. Verificar assets gerados
ls -lh internal/web/static/assets/
```

### Workflow de Release
```bash
# 1. Atualizar versão
git tag v1.3.2
git push origin v1.3.2

# 2. Build multi-plataforma
make release                     # Gera binários em build/release/

# 3. Criar GitHub release
./create-v1-release.sh           # Upload automático para GitHub
```

---

**Happy coding!** 🚀

---

## 📝 Histórico de Sessões Recentes

### Sessão 08/01/2026 - Refinamento Aba Helm
**Contexto**: Melhorias de UX e organização da interface Helm
**Alterações**:
- ✅ Corrigido bug: namespace não estava sendo passado ao selecionar release
- ✅ Reorganizado menu de ações em dropdown (⋮) no header de detalhes
- ✅ Movido Install, Upgrade, Rollback, Uninstall para menu de 3 pontos
- ✅ Mantido Export Values como botão independente
- ✅ Implementado filtro de namespaces de sistema (igual aba Namespaces)
- ✅ Adicionado botão "Sistema" (Eye/EyeOff) para toggle no header
- ✅ Select de namespaces movido para header e tornado dinâmico
- ✅ Namespaces carregados automaticamente dos releases disponíveis
- ✅ Filtro aplicado em releases e select (oculta kube-system, istio, etc)
- ✅ Auto-reset de namespace selecionado quando se torna indisponível
- ✅ Removidos cards de estatísticas (Contexto, Namespaces, HPAs, etc) da aba Helm
- ✅ Implementado MonacoYamlEditor nas abas Values e Manifest
- ✅ Habilitada edição no Monaco (substituindo `<pre>` tags read-only)
- ✅ Edição funcional: Values (Raw editável, Renderizado read-only), Manifest (editável)

**Arquivos Modificados**:
- `internal/web/frontend/src/components/HelmTab.tsx` - Filtros e select dinâmico
- `internal/web/frontend/src/components/HelmReleaseList.tsx` - Filtro de releases
- `internal/web/frontend/src/components/HelmReleaseDetails.tsx` - Menu dropdown e Monaco editor
- `internal/web/frontend/src/store/helmStore.tsx` - Estado de namespace
- `internal/web/frontend/src/pages/Index.tsx` - Ocultar cards na aba Helm
- `PLANO_ABA_HELM.md` - Atualização de progresso e critérios de aceite

**Pendências Identificadas**:
- [ ] Implementar botões Apply/Validate para aplicar edições do Monaco
- [ ] Validação YAML inline no editor
- [ ] Modal de confirmação antes de aplicar mudanças
- [ ] Diff visual (original vs editado) antes de upgrade
- [ ] Feedback de progresso ao aplicar mudanças

---
