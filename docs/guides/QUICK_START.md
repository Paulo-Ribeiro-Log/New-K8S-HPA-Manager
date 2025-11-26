# 🚀 Quick Start Para Novos Chats

## Project Summary
**Terminal-based Kubernetes HPA and Azure AKS Node Pool management tool** built with Go and Bubble Tea TUI framework. Features async rollout progress tracking with Rich Python-style progress bars, integrated status panel, session management, and unified HPA/node pool operations.

**NOVO (Outubro 2025)**: Interface web completa (React/TypeScript) com compatibilidade 100% TUI para sessões.

## Estado Atual (Novembro 2025)

**Versão Atual:** v1.0.7 (Release: 26 de novembro de 2025)
**GitHub Release:** https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.7

**TUI (Terminal Interface):**
- ✅ Interface responsiva (adapta-se ao tamanho real do terminal - mínimo 80x24)
- ✅ Execução sequencial de node pools para stress tests (F12)
- ✅ Rollouts detalhados de HPA (Deployment/DaemonSet/StatefulSet)
- ✅ CronJob management (F9) e Prometheus Stack (F8)
- ✅ Status container compacto (80x10) com progress bars Rich Python
- ✅ Auto-descoberta de clusters via `k8s-hpa-manager autodiscover`
- ✅ Validação VPN on-demand (verifica conectividade K8s antes de operações críticas)
- ✅ Modais de confirmação (Ctrl+D/Ctrl+U exigem confirmação)
- ✅ Log detalhado de alterações (antes → depois) no StatusContainer
- ✅ Sistema de Logs completo (F3) - visualizador com scroll, copiar, limpar
- ✅ Race condition corrigida (Mutex RWLock para testes paralelos de cluster)
- ✅ **Sistema de updates automático** - Detecção 1x por dia com notificação

**Web Interface:**
- ✅ Interface web completa (99% funcional)
- ✅ HPAs, Node Pools, CronJobs e Prometheus Stack implementados
- ✅ Dashboard com **gauge de dois anéis** mostrando Capacity vs Allocatable - v1.3.3
- ✅ **Métricas precisas** idênticas ao K9s (uso de Allocatable ao invés de Capacity) - v1.3.3
- ✅ Sistema de sessões completo (save/load/rename/delete/edit)
- ✅ Staging area com preview de alterações
- ✅ Snapshot de cluster para rollback
- ✅ Sistema de heartbeat e auto-shutdown (20min inatividade)
- ✅ ApplyAllModal com progress tracking e rollout simulation
- ✅ **Rollout individual para Prometheus Stack** (Deployment/StatefulSet/DaemonSet) - Outubro 2025
- ✅ **Aplicar Agora para Node Pools** - Aplicação individual sem staging - Outubro 2025
- ✅ **Campo de busca inteligente** - HPAs (nome/namespace) e Node Pools (nome/cluster) - v1.2.1
- ✅ **Modal de edição inline** - Edição completa de HPAs no ApplyAllModal - v1.2.1
- ✅ **Sistema de eventos** - Refetch sem reload para estabilidade - v1.2.1
- ✅ **Sistema de Log Viewer** - Modal com captura em tempo real, auto-refresh, exportar CSV - v1.2.1
- ✅ **Toggle de Namespaces de Sistema** - Exibe/oculta namespaces de sistema (kube-system, monitoring, etc.) - Outubro 2025
- ✅ **Combobox de Cluster no Header** - Busca integrada com filtro em tempo real, keyboard navigation - v1.3.2
- ✅ **Redesign CronJobs e Prometheus Pages** - SplitView layout, auto-refresh, controles compactos - v1.3.4
- ✅ **Redesign Staging Page** - SplitView layout (2/5 + 3/5), busca integrada, editor inline - v1.3.7
- ✅ **Load Session Modal Simplificado** - Removido "Apply Directly", scroll independente por painel - v1.3.8
- ✅ **Edição Inline de Node Pools no ApplyAllModal** - Menu ⋮ com opções "Editar Conteúdo" e "Remover da Lista" - v1.3.9
- ✅ **Editor não fecha após salvar** - Correção em StagingPanel para HPAs e Node Pools - v1.3.9
- ✅ **Página de Monitoring HPA-Watchdog** - Sidebar retrátil, integração com engine de monitoramento, métricas em tempo real - Novembro 2025
- ✅ **Refatoração RotatingCollector** - Sistema de monitoramento simplificado, redução de 850 → 450 linhas, baseline automático de 3 dias - 07 nov 2025
- ✅ **Aba ConfigMaps (Monaco Editor)** - Listagem completa com filtro por namespace, edição YAML com monaco-yaml, diff, dry-run e apply direto via backend Go; cards de estatísticas são ocultados apenas nesta aba para maximizar o espaço útil - Nov 2025
- ✅ **Diff visual com Diff2HTML** - Modal dedicado (side-by-side) usando tema VS Code dark, nomes reais de arquivos e mesma paleta do Monaco; backend gera unified diff via `difflib` - Nov 2025
- ✅ **Melhorias de UX na aba ConfigMaps** - Toggle de Labels, botão para recolher o painel de ConfigMaps e botões "X" de limpeza em todos os campos de busca (HPAs, Node Pools, etc.) para liberar espaço no editor - Nov 2025
- ✅ **Diff Visual com tela cheia + confirmação de Apply** - Botão "Tela cheia" (e toggle no cabeçalho) no modal de diff e diálogo de confirmação antes de aplicar YAML diretamente no cluster - Nov 2025
- ✅ **Linhas de referência confiáveis no gráfico de réplicas** - Min/Max passam a considerar o snapshot válido mais recente, evitando referência 0/0 causada por dados antigos - Nov 2025
- ✅ **Select compacto na aba Monitoramento** - Quando a sidebar está recolhida, o nome do HPA vira um select com todos os recursos monitorados para troca rápida sem reabrir o painel - Nov 2025
- ✅ **Cordon/Drain Config para Node Pools** - Sistema completo de evacuação de nodes antes de aplicar mudanças, com modal de configuração (grace period, timeout, force delete, etc.) integrado ao fluxo de Sequential Execution - 15 nov 2025
- ✅ **Progress Bar em Tempo Real via SSE** - Sistema completo de feedback visual durante operações Cordon/Drain usando Server-Sent Events; progress bar com gradiente de cores por fase (Azul: CORDON 0-20%, Laranja: DRAIN 20-80%, Roxo: AZURE 80-95%, Verde: COMPLETE 100%); ícones animados; detalhes em tempo real (node name, pods count, timestamps) - 18 nov 2025
- ✅ **Destaque visual sutil em cards de métricas** - Background azul escuro (blue-950/30) com borda (blue-800/40) em cards de CPU e latência P95/P99 para melhor diferenciação visual do fundo dos gráficos - 16 nov 2025
- ✅ **AlertHealthBadge com Dialog de Alertas** - Badge de health/alertas nos HPAListItems agora sempre clicável, abrindo modal AlertsDialog com detalhes completos dos alertas do Prometheus; wrapper div com stopPropagation previne propagação de clique para o Card pai - 26 nov 2025
- ✅ **Validação VPN Otimizada** - ValidateVPNConnectivity agora testa TODOS os contextos kubectl disponíveis (não apenas -prd/-hlg), timeout aumentado para 10s, múltiplos indicadores de sucesso, redução de falsos negativos - 26 nov 2025

**Sistema de Monitoramento V2 (Novembro 2025):**
- ✅ **MonitoringEngineV2 - Arquitetura sem Port-Forwards** - Sistema completo de monitoramento com acesso direto aos endpoints Prometheus (HTTPS público) sem necessidade de port-forwards ou VPN - 15 nov 2025
- ✅ **Discovery Automático de Endpoints** - Auto-descoberta de URLs Prometheus baseada em nomes de clusters (pattern: `https://prometheus-{nome}-{env}.viavarejo.com.br/`) - 15 nov 2025
- ✅ **Cliente Prometheus Nativo** - HTTP client com suporte a SSL self-signed, queries instantâneas e range queries para métricas históricas - 15 nov 2025
- ✅ **Cache em Memória com TTL** - Sistema de cache inteligente (1h TTL) com cleanup automático, substituindo SQLite - 15 nov 2025
- ✅ **API REST V2** - Endpoints `/api/v1/monitoring/v2/*` para métricas em tempo real, status da engine e gerenciamento de cache - 15 nov 2025
- ✅ **Escalabilidade Ilimitada** - Suporte a qualquer número de clusters simultaneamente (vs. limite de 2 clusters na V1) - 15 nov 2025
- ✅ **Redução de Código** - Deletados ~2537 linhas de código legado (port-forwards, baseline, rotating collectors) - 15 nov 2025
- ✅ **27 Testes Unitários** - 100% de cobertura nos novos componentes (discovery, client, cache, engine) - 15 nov 2025

**Sistema de Releases (v1.0.6 - Novembro 2025):**
- ✅ **Sistema de Releases Automatizado** - Script `create-release.sh` com upload automático de binários para GitHub (Linux amd64, macOS Intel/ARM) - 15 nov 2025
- ✅ **Documentação Completa de Releases** - `INSTRUCTIONS_RELEASE.md` com guia passo a passo para criar releases - 15 nov 2025
- ✅ **Suporte Windows via WSL2** - `WINDOWS_SUPPORT.md` com instruções detalhadas de instalação via Windows Subsystem for Linux - 15 nov 2025
- ✅ **Versionamento Semântico** - Injeção de versão via git tags durante build (`make release`) - 15 nov 2025

## Tech Stack
- **Language**: Go 1.23+ (toolchain 1.24.7)
- **TUI Framework**: Bubble Tea v0.24.2 + Lipgloss v1.1.0
- **K8s Client**: client-go v0.31.4 (official)
- **Azure SDK**: azcore v1.19.1, azidentity v1.12.0
- **Web Frontend**: React 18.3 + TypeScript 5.8 + Vite 5.4
- **Web UI**: shadcn/ui (Radix UI) + Tailwind CSS 3.4
- **Architecture**: MVC pattern com state-driven UI
