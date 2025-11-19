# Release v1.0.5 - Sistema de Releases Automatizado e Correções de UX

## 🎯 Destaques

### 🚀 Sistema de Releases Automatizado
- **Script genérico `create-release.sh`** para criar releases de qualquer versão
- **Configuração segura de GitHub token** via `setup-github-token.sh`
- **Documentação completa** em `GITHUB_TOKEN_SETUP.md`
- **Proteção automática** de tokens no `.gitignore`
- Workflow simplificado: 1 comando para publicar release completa

### 🔧 Correção: Auto-Open Navegador
- **Removida verificação incorreta** de "browser already open"
- Navegador agora **sempre abre automaticamente** ao iniciar servidor
- Respeita flag `--no-browser` quando especificada
- Sistema de heartbeat mantido intacto (auto-shutdown em 20min)

## 🐛 Correções

### ✅ Navegador Não Abria Automaticamente
- **Problema**: Script verificava se QUALQUER navegador estava aberto no sistema, mas não se estava acessando `localhost:<porta>`
- **Causa**: Função `isPageAlreadyOpen()` retornava true mesmo sem navegador na URL correta
- **Solução**: Removida verificação condicional - navegador sempre abre ao iniciar servidor

## ✨ Novas Features

### 📦 Sistema Completo de Releases
**Scripts criados:**
- `setup-github-token.sh` - Configuração interativa de token GitHub
  - Validação de formato
  - Teste automático com GitHub API
  - Permissões seguras (600)

- `create-release.sh` - Script genérico reutilizável
  - Busca token em múltiplas localizações (`.env`, `github_token.txt`, `secrets.sh`)
  - Detecta versão via git tag ou argumento
  - Verifica existência de binários
  - Pede confirmação antes de publicar
  - Upload automático de 4 binários

- `.env.example` - Template de configuração

**Documentação:**
- `GITHUB_TOKEN_SETUP.md` - Guia completo
  - Método automatizado (script)
  - Método manual
  - Boas práticas de segurança
  - Troubleshooting
  - Renovação de token expirado

**Proteção de segurança:**
- `.gitignore` atualizado para proteger:
  - `.env`, `.env.local`, `.env.*.local`
  - `*.token`
  - `github_token.txt`
  - `secrets.sh`

## 📦 Instalação

### Linux (amd64)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Apple Silicon)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### Windows (amd64)
```powershell
# Download do binário
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-windows-amd64.exe -o new-k8s-hpa.exe
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
new-k8s-hpa web --no-browser     # Sem auto-open

# Ou via script utilitário
new-k8s-hpa-web start            # Iniciar servidor
new-k8s-hpa-web stop             # Parar servidor
new-k8s-hpa-web status           # Ver status
new-k8s-hpa-web logs             # Ver logs em tempo real
```

## 🔄 Atualização de v1.0.4

Não há breaking changes. Atualização direta:

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
- Sistema completo de releases automatizado
- Script `setup-github-token.sh` para configuração segura
- Script `create-release.sh` genérico e reutilizável
- Documentação completa em `GITHUB_TOKEN_SETUP.md`
- Template `.env.example` para configuração
- Proteção de tokens no `.gitignore`

### Bug Fixes
- Navegador agora abre automaticamente ao iniciar servidor web
- Removida verificação incorreta de "browser already open"
- Correção em `cmd/web.go` - função `isPageAlreadyOpen()` removida

### Documentation
- `GITHUB_TOKEN_SETUP.md` - Guia completo de token GitHub
- `CLAUDE.md` atualizado com seção "Creating GitHub Releases"

## 🛠️ Para Desenvolvedores

### Criar Futuras Releases
```bash
# 1. Configurar token (apenas primeira vez)
./setup-github-token.sh

# 2. Criar RELEASE_NOTES_vX.X.X.md

# 3. Compilar binários
make release

# 4. Criar release
./create-release.sh 1.0.6
# ou deixar detectar versão via git tag
git tag v1.0.6
./create-release.sh
```

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

**Changelog completo**: Sistema de releases automatizado e correção de auto-open navegador.
