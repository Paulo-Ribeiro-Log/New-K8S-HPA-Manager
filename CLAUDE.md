# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/k8s-hpa-manager` para executar a aplicação.
**IMPORTANTE**: Interface **totalmente responsiva** - adapta-se a qualquer tamanho de terminal (recomendado: 80x24 ou maior).

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

### 📚 Histórico e Referências
11. [📜 Histórico de Correções](docs/history/CHANGELOG.md) - Correções e refatorações principais
12. [🔍 Análise Cordon/Drain](ANALISE_NODEPOOL_CORDON_DRAIN.md) - Análise detalhada do sistema

### 🚀 Otimizações
13. [⚡ Autodiscover Optimization](docs/optimization/AUTODISCOVER_OPTIMIZATION.md) - Paralelização da busca de subscriptions

---

## 📌 Quick Reference

### Comandos Mais Usados

```bash
# Build e Run
make build                    # Compilar backend Go
make web-build                # Build frontend
./build/k8s-hpa-manager web   # Iniciar servidor web

# Development
make run-dev                  # TUI com debug
make web-dev                  # Frontend dev server
make test                     # Rodar testes

# Installation (Release - Estável)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Installation (Main - Desenvolvimento)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash

# Updates
k8s-hpa-manager version       # Verificar versão
~/.k8s-hpa-manager/scripts/auto-update.sh  # Auto-update
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
│   │   └── sse/              # Server-Sent Events
│   ├── kubernetes/           # K8s client wrapper
│   ├── azure/                # Azure SDK auth
│   └── models/               # Data structures
├── docs/                     # Documentação modular
└── build/                    # Build artifacts
```

### Tech Stack (Quick Reference)

| Categoria | Tecnologia |
|-----------|------------|
| **Backend** | Go 1.23+, Bubble Tea, client-go, Azure SDK |
| **Frontend** | React 18.3, TypeScript 5.8, Vite 5.4 |
| **UI** | shadcn/ui, Tailwind CSS 3.4 |
| **Arquitetura** | MVC, SSE (Server-Sent Events) |

### Features Principais

✅ **TUI Completo** - Interface terminal responsiva com progress bars
✅ **Web Interface** - React/TypeScript com 99% das features
✅ **HPAs & Node Pools** - CRUD completo com staging area
✅ **Cordon/Drain** - Sistema de evacuação com progress em tempo real (SSE)
✅ **Sessions** - Save/load/edit com compatibilidade TUI ↔ Web
✅ **Monitoring V2** - Sistema sem port-forwards, acesso direto Prometheus
✅ **Auto-Updates** - Sistema automático de detecção e instalação
✅ **Autodiscover Otimizado** - Busca paralela de subscriptions (10x mais rápido)
✅ **Navegação Inteligente** - Navegação direta de Monitoramento → HPA Editor com 1 clique
✅ **Métricas Corrigidas** - CPU/Memória agora calculam média correta (0-100%)

---

## 🎯 Para Novos Chats do Claude

**Context Template Rápido:**
```
Projeto: Kubernetes HPA + Azure AKS Node Pool Manager
Versão: v1.0.11 (Nov 2025)
Tech: Go 1.23 + Bubble Tea (TUI) + React 18.3 (Web)
Build: make build && make web-build
Binary: ./build/k8s-hpa-manager

Recent Updates:
- Navegação inteligente: Monitoramento → HPA Editor com 1 clique (cluster/namespace/HPA automático)
- Ícones de monitoramento atualizam instantaneamente ao adicionar HPA
- Métricas Prometheus corrigidas: CPU/Memória agora usam avg() ao invés de sum() (0-100%)
- Autodiscover otimizado com busca paralela de subscriptions (10x mais rápido)
```

**Ver documentação completa:**
- [Quick Start](docs/guides/QUICK_START.md) - Estado atual e features
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md) - Comandos essenciais
- [Architecture](docs/architecture/OVERVIEW.md) - Estrutura técnica

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

**Happy coding!** 🚀
