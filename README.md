# New K8s HPA Manager

**Ferramenta completa de gerenciamento de recursos Kubernetes e Azure AKS com interface Web.**

[![Release](https://img.shields.io/github/v/release/Paulo-Ribeiro-Log/New-K8S-HPA-Manager?style=flat-square)](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

---

## Visão Geral

**New K8s HPA Manager** é uma solução para gerenciar recursos Kubernetes em larga escala, com suporte a múltiplos clusters (AKS/EKS/GKE) e análise preditiva com IA. Interface **Web** (Go/Gin API + React/TypeScript SPA).

**Última release estável:** `v1.3.40`

---

## Funcionalidades

### Workloads Kubernetes

| Recurso | Funcionalidades |
|---------|----------------|
| **HPAs** | Edição em lote de Min/Max Replicas, Targets CPU/Memory, Resources Request/Limit, staging area com preview |
| **Node Pools (AKS)** | Controle de autoscaling, node count, min/max, sequenciamento de operações, **Conntrack Viewer** |
| **Deployments** | Monaco YAML Editor, Rollout Restart, Delete, histórico de undo/redo, diff side-by-side |
| **DaemonSets** | Monaco YAML Editor, Rollout Restart, Delete, dry-run, apply com confirmação |
| **StatefulSets** | Monaco YAML Editor, Rollout Restart, Delete, dry-run, apply com confirmação |
| **VPAs** | Monaco YAML Editor, badge de updateMode (Auto/Off/Initial/Recreate), recomendações |
| **CronJobs** | Suspend/Resume, editor visual com parser de cron e descrições legíveis |
| **Namespaces** | CRUD completo, Monaco Editor, undo/redo, diff, dry-run, lista de deployments por namespace |
| **ConfigMaps** | Monaco YAML Editor, diff side-by-side, dry-run, apply, kubectl describe |
| **Secrets** | Monaco YAML Editor, diff, dry-run, apply |
| **Services** | Monaco YAML Editor, criar, editar, deletar |
| **Pods** | Listagem com filtros, métricas inline (atual / % do limit), logs com syntax highlighting, delete, modal de detalhes |
| **Containers** | Tree view de pods/containers, logs com auto-refresh, download |
| **Ingress** | Visualização e edição |
| **Events** | Monitoramento de eventos do cluster |

### Conntrack Viewer (Node Pools)

- **Snapshot atual** via `exec` em pod com `hostNetwork:true` (sem agente no node)
- **Histórico 24h via Prometheus**: BarChart comparando comportamento histórico vs snapshot atual
- **Recomendação automática de capacidade** por nó: OK / Monitorar tendência / Spike ativo / Aumentar limite
- Métricas: `nf_conntrack_count`, `nf_conntrack_max`, `nf_conntrack_buckets`, usage %
- Fallback gracioso quando Prometheus não está disponível

### Operações Avançadas

- **Cordon/Drain**: Evacuação de nodes com progresso em tempo real (SSE), sequenciamento e rollback
- **Terminal de Pods**: Shell interativo via WebSocket (xterm.js), suporte a teclado ABNT2
- **Transferência de Arquivos**: File browser completo nos pods, download de arquivos/diretórios, batch download (tar.gz)
- **Staging Area**: Preview de mudanças antes de aplicar, apply em lote com SSE progress

### Monitoramento e Alertas

- **Métricas em Tempo Real**: Integração direta com Prometheus (sem port-forwards)
- **Dashboard por Namespace**: Top 5 namespaces por CPU, Memória e Pods
- **Gráficos Interativos**: CPU, Memory, Replicas com comparação histórica D-1/D-2/D-3
- **Alertas Ativos**: Todos os alertas Prometheus com filtro por período (5min–24h)
- **Notificações In-App**: Sistema de notificações clicáveis com navegação contextual

### Monitor Tables (Painel Direito)

- Colunas redimensionáveis com drag handles em todas as tabelas de workloads
- Seleção múltipla com ações em lote (Restart, Delete, Kill)
- CPU/MEM: exibe `valor atual / % do limit` + `request / limit` na segunda linha
- Cor da linha reflete status do recurso (verde=Running, cinza=Completed, laranja=Error)
- Ordenação e filtro por coluna

### Análise Preditiva com IA

- **AI Diagnostics**: Análise inteligente de problemas em Pods com Ollama (local) ou Claude API
  - Providers: `llama3.2:3b` (local, 2GB RAM) ou Claude via API key
  - Sanitização seletiva: certificados e connection strings mascarados, IPs/emails visíveis
  - Histórico persistente em SQLite com filtros avançados

- **Análise Preditiva de Deployments**: Health Score 0-100, tendências, previsões curto/médio/longo prazo
  - Análise de capacidade de crescimento horizontal com 3 cenários
  - Análise de custo com over-provisioning e right-sizing P95
  - Exportação de relatórios PDF e Markdown

- **Análise Preditiva de Node Pools**: Health Score com 5 componentes ponderados
  - Métricas de conntrack por node, bin packing, eventos do autoscaler
  - Custo Azure (USD + BRL com cotação automática), idle waste, right-sizing
  - Relatórios PDF (9 seções) e Markdown (12 seções)

### Health Checking

- Execução paralela em múltiplos clusters com tabs independentes
- Verificação de Deployments: réplicas, CrashLoopBackOff, image pull errors, probes, QoS class
- Verificação de Nodes: conditions, capacidade vs alocação, pods afetados
- Cross-reference de ConfigMaps/Secrets com recursos do cluster
- Progresso em tempo real via SSE, histórico persistente em SQLite
- Exportação de relatórios em PDF, Markdown e CSV

### Helm

- Listagem de releases com filtro dinâmico por namespace e busca
- Monaco Editor nas abas Values e Manifest com undo/redo, diff, dry-run
- Apply com SSE streaming de progresso (0-100% com fases)
- Rollback, Uninstall, Export Values

### Service Mesh (Istio/Kiali)

- Topologia de serviços com Cytoscape.js
- Sistema de cores dinâmico por error rate e tráfego
- Badges: Missing Sidecars, Virtual Services, mTLS
- Modo fullscreen com controles de Traffic e Display

### Integração ServiceNow

- Importação de incidentes e CIs via ServiceNow API
- **Autenticação SAML/SSO via browser automation** (go-rod, Chromium embutido gerenciado pela própria aplicação — sem depender de Chrome do sistema): sessão persistida em disco, reaproveitada entre extrações
- Importação de CHGs de aprovação SRE a partir de mensagens do Microsoft Teams (Mr.ViaBot)

### Infraestrutura e Segurança

- **RBAC com Azure AD**: Controle de acesso baseado em grupos (VV_CLOUD_SRE)
  - Operações destrutivas protegidas no backend e frontend
  - Cache de permissões com TTL de 1 hora

- **Audit Log**: Rastreabilidade completa de operações Cordon/Drain e Rollouts
- **Sessions**: Save/Load/Edit de sessões de cluster/namespace
- **Auto-Discover**: Busca paralela de clusters AKS/EKS/GKE
- **Auto-Update**: Detecção e instalação automática de novas versões
- **Certificates**: Visualização de certificados TLS do cluster
- **Dependencies**: Mapa de dependências entre serviços

---

## Instalação

### Método 1: Release estável (Recomendado)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

### Método 2: Binários pré-compilados

Substitua `vX.Y.Z` pela [última release](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases) (atualmente `v1.3.40`).

**Linux (amd64)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/vX.Y.Z/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

**macOS (Intel)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/vX.Y.Z/new-k8s-hpa-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa && sudo mv new-k8s-hpa /usr/local/bin/
```

**macOS (Apple Silicon)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/vX.Y.Z/new-k8s-hpa-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa && sudo mv new-k8s-hpa /usr/local/bin/
```

**Windows**: Use WSL2 com o binário Linux.

---

## Uso

```bash
new-k8s-hpa                  # Sem subcomando = mesmo que `web` (inicia em background, porta 8080)
new-k8s-hpa web              # Inicia em background (porta 8080)
new-k8s-hpa web -f           # Foreground mode (logs no terminal)
new-k8s-hpa web --port 9000  # Porta customizada

# Com AI Diagnostics (Ollama local)
new-k8s-hpa web --ai-provider ollama --ollama-model llama3.2:3b

# Com AI Diagnostics (Claude API)
export ANTHROPIC_API_KEY=sk-ant-...
new-k8s-hpa web --ai-provider claude

new-k8s-hpa autodiscover     # Auto-descobrir clusters AKS/EKS/GKE
new-k8s-hpa version          # Ver versão e updates disponíveis
```

Acesse: `http://localhost:8080`

---

## Requisitos

| Obrigatório | Opcional |
|-------------|----------|
| Go 1.25+ (compilação) | Azure CLI / AWS CLI / gcloud (Node Pools AKS/EKS/GKE) |
| kubectl configurado | Prometheus (métricas + Conntrack histórico) |
| Git | Ollama local ou API key Claude/Gemini/OpenAI (AI Diagnostics) |
| | Kiali/Istio (Service Mesh) |
| | Google Chrome/Chromium no WSL2 (Teams — usa o Chrome do sistema; ServiceNow roda 100% no Chromium embutido do go-rod, não depende disso) |

---

## Configuração Inicial

```bash
# Configurar kubeconfig
export KUBECONFIG=~/.kube/config

# Login Azure (para Node Pools)
az login

# Auto-descobrir clusters
new-k8s-hpa autodiscover

# Iniciar servidor web
new-k8s-hpa web
```

---

## Tech Stack

| Categoria | Tecnologia |
|-----------|------------|
| **Backend** | Go 1.25.0, Gin 1.11.0, client-go v0.34.1 |
| **Azure** | azcore v1.19.1, azidentity v1.12.0, Azure CLI |
| **Frontend** | React 18.3.1, TypeScript 5.8.3, Vite 5.4.21 |
| **UI** | shadcn/ui (Radix UI), Tailwind CSS 3.4.17, Recharts |
| **Editor** | Monaco Editor 0.52.2 |
| **Terminal** | xterm.js 5.3.0 (WebSocket) |
| **Gráficos** | Recharts 2.15.4, Cytoscape 3.33.1 |
| **Persistência** | SQLite (histórico AI, predictions, health check) |
| **Streaming** | SSE (Server-Sent Events) |

---

## Documentação

- [CLAUDE.md](CLAUDE.md) — Guia completo de desenvolvimento e arquitetura
- [docs/guides/](docs/guides/) — Guias específicos (RBAC, Web Interface, Troubleshooting, etc.)
- [GitHub Releases](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases) — Release notes e downloads

---

## Links

- [Releases](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)
- [Issues](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues)

---

## Autor

**Paulo Ribeiro** · [@Paulo-Ribeiro-Log](https://github.com/Paulo-Ribeiro-Log)

---

## Licença

[MIT License](LICENSE)
