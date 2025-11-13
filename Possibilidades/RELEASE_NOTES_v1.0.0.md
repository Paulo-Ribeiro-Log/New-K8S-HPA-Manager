# K8s HPA Manager v1.0.0

## 🎉 Release Inicial do Novo Repositório

Esta é a primeira release oficial do **New-K8S-HPA-Manager**, uma ferramenta completa de gerenciamento de HPA e Node Pools do Kubernetes/Azure AKS.

## ✨ Principais Funcionalidades

### Gerenciamento de Recursos
- 📊 **HPAs**: Edição em lote de Min/Max Replicas, Targets (CPU/Memory), Resources (Request/Limit)
- 🔧 **Node Pools (AKS)**: Controle de autoscaling, node count e limites
- ⏰ **CronJobs**: Suspend/Resume de cronjobs
- 📈 **Prometheus Stack**: Gerenciamento de recursos e rollouts

### Sistema de Sessões
- 💾 **Save/Load/Rename/Delete**: Sessões compatíveis entre TUI e Web
- 📸 **Snapshots de Cluster**: Captura estado atual para rollback
- 🏷️ **Templates**: Nomenclatura padronizada (Upscale/Downscale/Rollback)
- 📝 **History Tracking**: Rastreamento completo de alterações

### Monitoramento (HPA-Watchdog)
- 📡 **Métricas em Tempo Real**: Integração com Prometheus
- 🔍 **Baseline de 3 dias**: Coleta histórica para análise
- 📊 **Gráficos Interativos**: CPU, Memory, Replicas com comparação D-1
- 🚨 **Detecção de Anomalias**: Sistema inteligente de alertas

### ConfigMaps
- 📝 **Editor YAML**: Monaco Editor com syntax highlighting
- 🔀 **Diff Visual**: Side-by-side com tema VS Code Dark
- ✅ **Dry-run e Apply**: Validação e aplicação segura
- 🔍 **Filtros Avançados**: Por namespace, labels e data keys

## 🚀 Instalação Rápida

```bash
# Método 1: Instalação automática (recomendado)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Método 2: Download direto do binário
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.0/new-k8s-hpa-v1.0.0
chmod +x new-k8s-hpa-v1.0.0
sudo mv new-k8s-hpa-v1.0.0 /usr/local/bin/new-k8s-hpa
```

## 💻 Tech Stack

**Backend**: Go 1.23+, Bubble Tea, client-go v0.31, Azure SDK
**Frontend**: React 18.3, TypeScript 5.8, Vite 5.4, shadcn/ui, Tailwind CSS
**Kubernetes**: client-go official, Azure CLI integration
**Web Server**: Gin HTTP com heartbeat/auto-shutdown (20min)

## 📦 Assets

- `new-k8s-hpa-v1.0.0`: Binário executável Linux AMD64 (89 MB)

## 🛠️ Uso

### Interface TUI
```bash
new-k8s-hpa                  # Abrir TUI
new-k8s-hpa --debug          # Debug mode
new-k8s-hpa autodiscover     # Auto-descobrir clusters
```

### Interface Web
```bash
new-k8s-hpa web              # Background mode
new-k8s-hpa web -f           # Foreground mode
new-k8s-hpa web --port 8080  # Custom port
```

## 📋 Requisitos

- Go 1.23+ (para compilação)
- kubectl configurado
- Azure CLI (para Node Pools)
- Acesso a clusters Kubernetes

## 📚 Documentação

- [Guia de Desenvolvimento](CLAUDE.md)
- [Sistema de Updates](UPDATE_BEHAVIOR.md)
- [Guia de Instalação](INSTALL_GUIDE.md)

---

**Changelog completo**: Migração do repositório Scale_HPA com todas as features implementadas até novembro/2025.

**Full Changelog**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/commits/v1.0.0
