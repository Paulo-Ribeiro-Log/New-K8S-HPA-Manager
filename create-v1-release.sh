#!/bin/bash
# Script para criar release v1.0.0 do New-K8S-HPA-Manager
# Autor: Paulo Ribeiro
# Data: 2025-01-12

set -e

echo "🚀 Criando Release v1.0.0 do New-K8S-HPA-Manager..."
echo ""

# Verificar se gh está autenticado
if ! gh auth status &>/dev/null; then
    echo "⚠️  GitHub CLI não está autenticado"
    echo "Execute: gh auth login"
    exit 1
fi

# Verificar se a tag existe
if ! git tag | grep -q "^v1.0.0$"; then
    echo "📌 Criando tag v1.0.0..."
    git tag -a v1.0.0 -m "Release inicial v1.0.0"
    git push origin v1.0.0
else
    echo "✅ Tag v1.0.0 já existe"
fi

# Compilar binário
echo "🔨 Compilando binário..."
make build

# Renomear binário para new-k8s-hpa
echo "📦 Preparando asset new-k8s-hpa..."
cp build/new-k8s-hpa build/new-k8s-hpa
chmod +x build/new-k8s-hpa

# Criar release notes
RELEASE_NOTES=$(cat <<'EOF'
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
# Download direto do binário
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.0/new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/

# Ou via script de instalação
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

## 💻 Tech Stack

**Backend**: Go 1.23+, Bubble Tea, client-go v0.31, Azure SDK
**Frontend**: React 18.3, TypeScript 5.8, Vite 5.4, shadcn/ui, Tailwind CSS

## 📦 Assets

- `new-k8s-hpa`: Binário executável Linux AMD64 (63 MB)

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
```

---

**Changelog completo**: Migração do repositório anterior com todas as features implementadas até novembro/2025.
EOF
)

# Criar release no GitHub
echo "🎉 Criando release no GitHub..."
gh release create v1.0.0 \
  --repo Paulo-Ribeiro-Log/New-K8S-HPA-Manager \
  --title "v1.0.0 - Release Inicial" \
  --notes "$RELEASE_NOTES" \
  build/new-k8s-hpa

echo ""
echo "✅ Release v1.0.0 criada com sucesso!"
echo "🔗 https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.0"
