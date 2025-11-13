# Release v1.0.3 - Correções de Repositório e Versão no Header

## 🎯 Destaques

### ✨ Novo Sistema de Versionamento
- **Versão exibida no header** da interface web (abaixo do título em letras menores)
- **Indicador de update disponível** - Badge amarelo "Update" quando nova versão está disponível
- Link direto para página de download da release no GitHub
- Verificação automática de updates via GitHub API

### 🐛 Correções Críticas
- **✅ Repositório corrigido** - Todos os scripts e sistema de updates agora apontam para `Paulo-Ribeiro-Log/New-K8S-HPA-Manager`
- **✅ Node Pools funcionando** - Interface web agora carrega Node Pools corretamente
- **✅ Build offline** - Sistema de vendor/ completamente funcional (97MB de dependências versionadas)

## 📦 Instalação

### Linux (amd64)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.3/k8s-hpa-manager-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.3/k8s-hpa-manager-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Apple Silicon)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.3/k8s-hpa-manager-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### Windows (amd64)
```powershell
# Download do binário
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.3/k8s-hpa-manager-windows-amd64.exe -o new-k8s-hpa.exe
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
new-k8s-hpa autodiscover         # Auto-descobrir clusters
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

## 🔄 Atualização

Se você já tem uma versão instalada:

```bash
# Opção 1: Auto-update
~/.new-k8s-hpa/scripts/auto-update.sh --yes

# Opção 2: Reinstalação completa
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

## 📋 Changelog Completo

### Features
- Sistema de versionamento no header da interface web
- Badge de update disponível (amarelo) quando nova versão publicada
- Endpoint GET /api/v1/version (sem autenticação)
- Link direto para download da release

### Bug Fixes
- Corrigido RepoName em `internal/updater/version.go` (Scale_HPA → New-K8S-HPA-Manager)
- Corrigido URL em `create_release.sh`
- Corrigido changelog em `create-v1-release.sh`
- Sistema de updates agora busca no repositório correto

### Melhorias
- Build offline 100% funcional (vendor/ versionado)
- Interface web carrega Node Pools corretamente
- Frontend rebuilded com assets atualizados

## 📊 Tamanhos dos Binários

| Plataforma | Tamanho |
|------------|---------|
| Linux amd64 | ~90 MB |
| macOS amd64 (Intel) | ~90 MB |
| macOS arm64 (Apple Silicon) | ~87 MB |
| Windows amd64 | ~90 MB |

## 🔗 Links Úteis

- **Repositório**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager
- **Issues**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues
- **Documentação**: Ver CLAUDE.md no repositório

---

**Changelog completo**: Migração do repositório anterior com correções críticas de versionamento e sistema de updates.
