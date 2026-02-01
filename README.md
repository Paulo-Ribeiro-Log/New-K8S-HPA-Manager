# New K8s HPA Manager

**Ferramenta completa de gerenciamento de HPA (Horizontal Pod Autoscaler) e Node Pools do Kubernetes/Azure AKS com interface Web.**

[![Release](https://img.shields.io/github/v/release/Paulo-Ribeiro-Log/New-K8S-HPA-Manager?style=flat-square)](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

---

## 🎯 Visão Geral

**New K8s HPA Manager** é uma solução robusta para gerenciar recursos Kubernetes em larga escala, oferecendo duas interfaces complementares:

---

## ✨ Principais Funcionalidades

### 📊 Gerenciamento de Recursos
- **HPAs**: Edição em lote de Min/Max Replicas, Targets (CPU/Memory), Resources (Request/Limit)
- **Node Pools (AKS)**: Controle de autoscaling, node count e limites
- **CronJobs**: Suspend/Resume de cronjobs
- **Prometheus Stack**: Gerenciamento de recursos e rollouts

### 💾 Sistema de Sessões
- **Save/Load/Rename/Delete**: Sessões compatíveis entre TUI e Web
- **Snapshots de Cluster**: Captura estado atual para rollback
- **Templates**: Nomenclatura padronizada (Upscale/Downscale/Rollback)
- **History Tracking**: Rastreamento completo de alterações

### 📡 Monitoramento (HPA-Watchdog)
- **Métricas em Tempo Real**: Integração com Prometheus
- **Baseline de 3 dias**: Coleta histórica para análise
- **Gráficos Interativos**: CPU, Memory, Replicas com comparação D-1
- **Detecção de Anomalias**: Sistema inteligente de alertas

### 📝 ConfigMaps
- **Editor YAML**: Monaco Editor com syntax highlighting
- **Diff Visual**: Side-by-side com tema VS Code Dark
- **Dry-run e Apply**: Validação e aplicação segura
- **Filtros Avançados**: Por namespace, labels e data keys

---

## 🚀 Instalação Rápida

### Método 1: Instalação Automática (Recomendado)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

**O que o instalador faz:**
- ✅ Verifica requisitos (Go, Git, kubectl, Azure CLI)
- ✅ Clona repositório
- ✅ Compila binário com versão injetada
- ✅ Instala em `/usr/local/bin/new-k8s-hpa`
- ✅ Copia scripts utilitários para `~/.new-k8s-hpa/scripts/`
- ✅ Cria atalho `new-k8s-hpa-web` para servidor web

### Método 2: Download Direto de Binários Pré-Compilados

**Linux (amd64)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.3.1/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

**macOS (Intel)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.3.1/new-k8s-hpa-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

**macOS (Apple Silicon M1/M2)**
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.3.1/new-k8s-hpa-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

**Windows**
⚠️ **Windows não é suportado via binários pré-compilados.**

Use **WSL2** (Windows Subsystem for Linux) para funcionalidade completa:
```bash
# Dentro do WSL2 (Ubuntu)
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.3.1/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

📖 Ver [WINDOWS_SUPPORT.md](WINDOWS_SUPPORT.md) para instruções detalhadas de instalação WSL2

---

## 💻 Tech Stack

| Categoria | Tecnologias |
|-----------|-------------|
| **Backend** | Go 1.24+, client-go v0.34, Azure SDK |
| **Kubernetes** | client-go v0.31.4 (official) |
| **Azure** | azcore v1.19.1, azidentity v1.12.0, Azure CLI |
| **Frontend** | React 18.3, TypeScript 5.8, Vite 5.4 |
| **UI Components** | shadcn/ui (Radix UI), Tailwind CSS 3.4 |
| **Web Server** | Gin v1.11.0 com heartbeat/auto-shutdown (20min) |

---

## 🛠️ Uso

### Interface TUI

```bash
# Iniciar TUI
new-k8s-hpa

# Outros comandos
new-k8s-hpa --debug          # Debug mode
new-k8s-hpa autodiscover     # Auto-descobrir clusters
new-k8s-hpa version          # Ver versão
new-k8s-hpa --help           # Ajuda completa
```

**Atalhos do TUI:**
- `F1` - Help
- `F3` - Log Viewer
- `F5` - Reload clusters
- `F8` - Prometheus Stack
- `F9` - CronJobs
- `F12` - Stress Test (Node Pools)
- `Ctrl+S` - Save Session
- `Ctrl+L` - Load Session
- `ESC` - Voltar/Cancelar

### Interface Web

```bash
# Método 1: Atalho (se instalado via script)
new-k8s-hpa-web start        # Iniciar servidor (porta 8080)
new-k8s-hpa-web stop         # Parar servidor
new-k8s-hpa-web status       # Ver status
new-k8s-hpa-web logs         # Logs em tempo real

# Método 2: Comando direto
new-k8s-hpa web              # Background mode (default)
new-k8s-hpa web -f           # Foreground mode
new-k8s-hpa web --port 9000  # Custom port
```

**Acesso:**
```
http://localhost:8080
```

**Auto-shutdown:** Servidor desliga automaticamente após 20 minutos de inatividade.

---

## 📋 Requisitos

### Obrigatórios
- **Go 1.24+** (para compilação)
- **kubectl** configurado com acesso aos clusters
- **Git** (para clone do repositório)

### Opcionais
- **Azure CLI** (necessário para operações com Node Pools)
- **VPN** (se clusters requerem VPN)

---

## ⚙️ Configuração Inicial

```bash
# 1. Configurar kubeconfig
export KUBECONFIG=~/.kube/config

# 2. Login Azure (para Node Pools)
az login

# 3. Auto-descobrir clusters
new-k8s-hpa autodiscover

# 4. Iniciar aplicação
new-k8s-hpa
```

---

## 📚 Documentação

- **[CLAUDE.md](CLAUDE.md)** - Guia completo de desenvolvimento
- **[INSTRUCTIONS_RELEASE.md](INSTRUCTIONS_RELEASE.md)** - Como criar releases com binários pré-compilados
- **[WINDOWS_SUPPORT.md](WINDOWS_SUPPORT.md)** - Limitações Windows e instalação via WSL2
- **[GitHub Releases](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)** - Release notes e downloads

---

## 🔧 Scripts Utilitários

Após instalação via script, os seguintes utilitários ficam disponíveis em `~/.new-k8s-hpa/scripts/`:

| Script | Descrição |
|--------|-----------|
| `web-server.sh` | Gerenciar servidor web (start/stop/status/logs) |
| `uninstall.sh` | Desinstalar aplicação |
| `auto-update.sh` | Sistema de auto-update |
| `backup.sh` | Backup do código fonte |
| `restore.sh` | Restaurar backup |
| `rebuild-web.sh` | Rebuild interface web |

---

## 📦 Releases

### v1.3.1 (2025-12-03) - **Versão Atual**
- 🐛 **Correções Críticas**
  - ✅ Corrigido erro de conflito ao aplicar edições em ConfigMaps
  - ✅ Melhorias na estabilidade do editor de ConfigMaps

- 🎨 **UI/UX Improvements**
  - ✅ Gráfico de Memória: Linha corrente usa cor azul ao invés de roxo
  - ✅ ConfigMaps/Secrets/Deployments: Labels iniciam recolhidos por padrão
  - ✅ Campo "Versão" exibido quando disponível (app.kubernetes.io/version)
  - ✅ Node Pools: Botão de refresh no painel "Available Node Pools"
  - ✅ VM Disk Specs: Informações de performance de disco (IOPS, Throughput)

- 📊 **Monitoramento e Alertas**
  - ✅ Card de Cluster Contextual: Adapta-se ao contexto da aba
  - ✅ Comparação Histórica D-1/D-2/D-3: Linha lilás nos gráficos
  - ✅ Sistema de Alertas completo com filtro por período de tempo
  - ✅ Navegação bidirecional: HPAs ↔ Monitoramento com 1 clique

- ⚙️ **Tech Stack**
  - ✅ Go 1.24+, client-go v0.34, Azure SDK
  - ✅ React 18.3, TypeScript 5.8, Vite 5.4

[Ver todas as releases](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)

---

## 🤝 Contribuindo

Contribuições são bem-vindas! Por favor:

1. Fork o repositório
2. Crie uma branch para sua feature (`git checkout -b feature/nova-feature`)
3. Commit suas mudanças (`git commit -m 'feat: adicionar nova feature'`)
4. Push para a branch (`git push origin feature/nova-feature`)
5. Abra um Pull Request

**Convenções de commit:**
- `feat:` - Nova funcionalidade
- `fix:` - Correção de bug
- `docs:` - Documentação
- `chore:` - Manutenção/refatoração
- `style:` - Formatação de código

---

## 📄 Licença

[MIT License](LICENSE) - sinta-se livre para usar em projetos pessoais e comerciais.

---

## 👨‍💻 Autor

**Paulo Ribeiro**
- GitHub: [@Paulo-Ribeiro-Log](https://github.com/Paulo-Ribeiro-Log)

---

## 🌟 Agradecimentos

Projeto baseado no [Scale_HPA](https://github.com/Paulo-Ribeiro-Log/Scale_HPA) com melhorias significativas e nova arquitetura.

---

## 🚀 Quick Links

- [📦 Releases](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases)
- [🐛 Issues](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues)
- [📖 Wiki](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/wiki)
- [💬 Discussions](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/discussions)

---

<div align="center">

**⭐ Se este projeto foi útil, considere dar uma estrela!**

</div>
