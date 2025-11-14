# Release v1.0.4 - Autodiscover Automático e Notificação de VPN

## 🎯 Destaques

### ✨ Instalação Simplificada
- **Autodiscover automático** durante instalação
- Clusters configurados automaticamente após `install-from-github.sh`
- Não é mais necessário executar `new-k8s-hpa autodiscover` manualmente

### 🚨 Notificação Clara de VPN Desconectada
- **Banner vermelho persistente** quando VPN está desconectada
- Substituído toast temporário por alerta visual destacado
- Verificação automática de VPN em cada mudança de aba
- Botão "Tentar Novamente" para revalidar conexão
- Instruções claras: conectar VPN, executar autodiscover, verificar kubectl

### 📂 Padronização de Diretório
- Diretório de dados alterado de `~/.new-k8s-hpa/` para `~/.k8s-hpa-manager/`
- Alinhado com nome oficial da aplicação
- **Nome do executável permanece**: `new-k8s-hpa` (não mudou!)

## 🐛 Correções Críticas

### ✅ Node Pools Não Carregavam em Instalação Nova
- **Problema**: Aplicação instalada em novo computador não carregava Node Pools
- **Causa**: `clusters-config.json` não era criado automaticamente
- **Solução**: Instalador executa autodiscover automaticamente

### ✅ Notificação de VPN Passava Despercebida
- **Problema**: Toast de VPN desconectada desaparecia em 10 segundos
- **Causa**: Usuário não percebia a desconexão e operações falhavam silenciosamente
- **Solução**: Banner persistente vermelho com instruções claras

## 📦 Instalação

### Linux (amd64)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.4/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.4/new-k8s-hpa-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Apple Silicon)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.4/new-k8s-hpa-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### Windows (amd64)
```powershell
# Download do binário
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.4/new-k8s-hpa-windows-amd64.exe -o new-k8s-hpa.exe
# Adicionar ao PATH manualmente
```

### Instalação via Script (Recomendado)
```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

## 🚀 Como Usar

### Interface TUI
```bash
new-k8s-hpa                      # Iniciar TUI
new-k8s-hpa autodiscover         # Auto-descobrir clusters (automático na instalação)
new-k8s-hpa version              # Ver versão e verificar updates
```

### Interface Web
```bash
new-k8s-hpa web                  # Background mode (porta 8080)
new-k8s-hpa web -f               # Foreground mode
new-k8s-hpa web --port 9000      # Custom port

# Ou via script utilitário
new-k8s-hpa-web start            # Iniciar servidor
new-k8s-hpa-web stop             # Parar servidor
new-k8s-hpa-web status           # Ver status
new-k8s-hpa-web logs             # Ver logs em tempo real
```

## 🔄 Atualização de v1.0.3

### Migração de Diretório de Dados

Se você já tem v1.0.3 instalada, migre o diretório de dados:

```bash
# Opção 1: Migração manual (preserva tudo)
mv ~/.new-k8s-hpa ~/.k8s-hpa-manager

# Opção 2: Deixar criar novo (perde sessões antigas)
new-k8s-hpa autodiscover
# Copiar sessões manualmente se necessário:
cp -r ~/.new-k8s-hpa/sessions ~/.k8s-hpa-manager/
```

### Auto-update
```bash
# Opção 1: Auto-update interativo
~/.k8s-hpa-manager/scripts/auto-update.sh

# Opção 2: Auto-update automático
~/.k8s-hpa-manager/scripts/auto-update.sh --yes

# Opção 3: Reinstalação completa
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

## 📋 Changelog Completo

### Features
- Autodiscover automático durante instalação (`install-from-github.sh`)
- Banner de VPN persistente e destacado (vermelho)
- Verificação de VPN em cada mudança de aba
- Hook `useVPNStatus` para monitoramento contínuo
- Script de diagnóstico (`diagnostico.sh`) para troubleshooting
- Componente `VPNWarningBanner` com botão "Tentar Novamente"

### Bug Fixes
- Node Pools agora carregam corretamente em instalação nova
- Notificação de VPN não passa mais despercebida
- Diretório de dados padronizado (`.k8s-hpa-manager`)

### Refactoring
- Substituído `.new-k8s-hpa/` por `.k8s-hpa-manager/` em todo código
- 21 arquivos atualizados (Go + Shell scripts)
- Documentação atualizada

## 🛠️ Ferramentas de Diagnóstico

### Script de Diagnóstico
```bash
# Verifica tudo: binário, clusters-config, kubeconfig, Azure CLI, servidor web, assets
./diagnostico.sh
```

**O que verifica**:
- ✅ Binário instalado e versão
- ✅ `~/.k8s-hpa-manager/clusters-config.json` existe
- ✅ Kubeconfig configurado (`~/.kube/config`)
- ✅ Azure CLI instalado e autenticado
- ✅ Servidor web rodando
- ✅ Endpoints `/api/v1/clusters` e `/api/v1/nodepools` funcionando
- ✅ Assets embarcados no binário

## 📊 Tamanhos dos Binários

| Plataforma | Tamanho |
|------------|---------|
| Linux amd64 | ~92 MB |
| macOS amd64 (Intel) | ~91 MB |
| macOS arm64 (Apple Silicon) | ~89 MB |
| Windows amd64 | ~91 MB |

## 🔗 Links Úteis

- **Repositório**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager
- **Issues**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues
- **Documentação**: Ver CLAUDE.md no repositório

---

**Changelog completo**: Autodiscover automático, banner de VPN persistente, e padronização de diretório de dados.
