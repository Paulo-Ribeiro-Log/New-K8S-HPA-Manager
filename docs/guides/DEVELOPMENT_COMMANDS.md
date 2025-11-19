# 🔧 Development Commands

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Terminal Requirements (TUI)

**✅ Interface Totalmente Responsiva**

A aplicação usa **EXATAMENTE o tamanho do seu terminal** - sem forçar dimensões artificiais:

- **Adapta-se ao terminal**: Usa suas dimensões reais (ex: 80x24, 120x30, etc)
- **Texto legível**: Não precisa zoom out - mantenha Ctrl+0 (tamanho normal)
- **Otimizada para produção**: Layout compacto, operação segura sem erros visuais
- **Sem limites artificiais**: Removido forçamento de 188x45 que causava texto minúsculo

**Como funciona:**
1. Aplicação detecta tamanho real do terminal
2. Ajusta painéis automaticamente (60x12 base)
3. Status panel compacto (80x10)
4. Context box inline (cluster | sessão)
5. Scroll quando necessário

**Validação VPN e Azure:**
- **VPN Check**: Usa `kubectl cluster-info` para validar conectividade K8s real
- **Validação on-demand**: Testa VPN em início, namespaces, HPAs e timeouts
- **Azure timeout**: 5 segundos para evitar travamentos DNS
- **Mensagens claras**: Exibidas no StatusContainer com soluções (F5 para retry)

## Installation and Updates

```bash
# Instalação completa em 1 comando (clone + build + install)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# O que faz:
# - Clona repositório
# - Compila com injeção de versão
# - Instala em /usr/local/bin/
# - Copia scripts utilitários para ~/.k8s-hpa-manager/scripts/
# - Cria atalho k8s-hpa-web

# Sistema de updates automático
k8s-hpa-manager version       # Verificar versão e updates disponíveis
~/.k8s-hpa-manager/scripts/auto-update.sh             # Auto-update interativo
~/.k8s-hpa-manager/scripts/auto-update.sh --yes       # Auto-update sem confirmação
~/.k8s-hpa-manager/scripts/auto-update.sh --check     # Apenas verificar
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run   # Simular

# Scripts utilitários instalados
k8s-hpa-web start/stop/status/logs/restart            # Gerenciar servidor web
~/.k8s-hpa-manager/scripts/uninstall.sh              # Desinstalar
~/.k8s-hpa-manager/scripts/backup.sh                 # Backup (dev)
~/.k8s-hpa-manager/scripts/restore.sh                # Restore (dev)
```

📚 **Documentação:**
- `INSTALL_GUIDE.md` - Guia completo de instalação
- `UPDATE_BEHAVIOR.md` - Como funciona o sistema de updates
- `AUTO_UPDATE_EXAMPLES.md` - Exemplos de uso do auto-update

## Creating GitHub Releases

```bash
# 1. Configurar GitHub token (apenas primeira vez)
./setup-github-token.sh

# 2. Criar RELEASE_NOTES_vX.X.X.md com descrição da versão

# 3. Compilar binários para todas as plataformas
make release

# 4. Criar release no GitHub (método recomendado - script genérico)
./create-release.sh 1.0.5

# Ou deixar o script detectar versão via git tag
git tag v1.0.5
./create-release.sh

# Script específico de versão (se existir)
./create-release-v1.0.4.sh
```

**O script `create-release.sh`**:
- ✅ Busca token automaticamente em múltiplas localizações
- ✅ Funciona para qualquer versão (genérico e reutilizável)
- ✅ Detecta versão via git tag ou argumento
- ✅ Verifica se binários existem antes de criar release
- ✅ Pede confirmação antes de publicar
- ✅ Cria release no GitHub com release notes
- ✅ Faz upload automático dos 4 binários (Linux, macOS Intel/ARM, Windows)

📚 **Documentação de releases:**
- `GITHUB_TOKEN_SETUP.md` - Guia completo de configuração de token
- `.env.example` - Template para configuração de token

## Building and Running (TUI)

```bash
# Build TUI
make build                    # Build to ./build/k8s-hpa-manager (version auto-detected)
make build-all                # Build for multiple platforms (Linux, macOS, Windows)
make run                      # Build and run
make run-dev                  # Run with debug logging (go run . --debug)
make version                  # Show detected version from git tags
make release                  # Build for all platforms (Linux, macOS amd64/arm64, Windows)
```

## Building and Running (Web Interface)

```bash
# Frontend development
make web-install              # Install frontend dependencies (npm install)
make web-dev                  # Start Vite dev server (port 5173)
                              # Backend: ./build/k8s-hpa-manager web --port 8080 (terminal 2)

# Production build
make web-build                # Build frontend → internal/web/static/
make build-web                # Build completo (frontend + Go binary com embed)

# Run web server
./build/k8s-hpa-manager web              # Background mode (default)
./build/k8s-hpa-manager web -f           # Foreground mode
./build/k8s-hpa-manager web --port 8080  # Custom port

# IMPORTANTE: Rebuild obrigatório
./rebuild-web.sh -b           # Script recomendado (evita cache issues)
```

## Testing

```bash
make test                     # Run all tests with verbose output
make test-coverage            # Run tests with coverage (generates coverage.html)
```

## Safe Deploy (Deploy Seguro)

**Script automatizado para deploy seguro de dev2 → main com validações completas:**

```bash
./safe-deploy.sh              # Deploy completo (interativo com confirmações)
./safe-deploy.sh --dry-run    # Simular deploy sem executar (teste)
./safe-deploy.sh --yes        # Deploy automático sem confirmações
./safe-deploy.sh --skip-tests # Pular execução de testes (não recomendado)
./safe-deploy.sh --skip-build # Pular build (não recomendado)
./safe-deploy.sh --help       # Ver todas as opções
```

**O que o script faz:**
1. ✅ **Validações iniciais**: Working tree limpo, branches existem
2. ✅ **Testes**: Executa `make test` (pode pular com --skip-tests)
3. ✅ **Build**: Compila TUI e Web (pode pular com --skip-build)
4. ✅ **Backup**: Cria branch de backup automático (backup-TIMESTAMP-pre-deploy)
5. ✅ **Merge**: dev2 → main com detecção de conflitos
6. ✅ **Sync**: Rebase com origin/main
7. ✅ **Tags**: Opção de atualizar tags (ex: v1.2.0)
8. ✅ **Push**: Branch main e tags para GitHub
9. ✅ **Sync dev2**: Opção de sincronizar dev2 com main após deploy

**Workflow recomendado:**
```bash
# 1. Testar primeiro (dry-run)
./safe-deploy.sh --dry-run

# 2. Deploy real após validar
./safe-deploy.sh

# 3. Ou deploy automático (CI/CD)
./safe-deploy.sh --yes
```

**Vantagens:**
- 🛡️ Previne quebra da branch main
- 🔄 Backup automático antes de qualquer alteração
- ✅ Validações completas (testes, build, working tree)
- 📊 Resumo claro do que será feito
- 🎯 Modo dry-run para testes seguros

**Nota:** O script `safe-deploy.sh` está no `.gitignore` e não é versionado (uso local apenas).

## Installation

```bash
./install.sh                  # Automated installer → /usr/local/bin/
./uninstall.sh                # Uninstaller (optionally removes session data)

# After installation
k8s-hpa-manager               # Run TUI from anywhere
k8s-hpa-manager web           # Run web interface
k8s-hpa-manager --debug       # Debug mode
k8s-hpa-manager --help        # Show help
```

## Cluster Auto-Discovery

```bash
k8s-hpa-manager autodiscover  # Auto-descobre clusters do kubeconfig
```
- Extrai resource groups do campo `user` (formato: `clusterAdmin_{RG}_{CLUSTER}`)
- Descobre subscriptions via Azure CLI
- Gera/atualiza `~/.k8s-hpa-manager/clusters-config.json`
- Escalável para 26, 70+ clusters

**Workflow:**
1. `az aks get-credentials --name CLUSTER --resource-group RG`
2. `k8s-hpa-manager autodiscover`
3. Node Pools prontos para uso (TUI e Web)

## Backup and Restore

```bash
./backup.sh "descrição"       # Criar backup antes de modificações
./restore.sh                  # Listar backups disponíveis
./restore.sh backup_name      # Restaurar backup específico
```
- Mantém os 10 backups mais recentes automaticamente
- Metadados inclusos (git commit, data, usuário)
