# Release v1.2.0

## 🎉 Novidades Principais

### 🚀 Sistema Completo de Instalação e Updates
- **Instalação em 1 comando** via curl (clone + build + install automático)
- **Auto-update script** com flags `--yes` e `--dry-run`
- **Verificação automática de updates** 1x por dia
- **Notificações no TUI** quando houver nova versão
- **Scripts utilitários** copiados automaticamente (`k8s-hpa-web`, `auto-update`, `uninstall`)
- **Atalho `k8s-hpa-web`** para gerenciar servidor web facilmente

### 📚 Documentação Completa
- **INSTALL_GUIDE.md** - Guia completo de instalação
- **UPDATE_BEHAVIOR.md** - Como funciona o sistema de updates
- **AUTO_UPDATE_EXAMPLES.md** - Exemplos práticos (cron, scripts, CI/CD)
- **QUICK_INSTALL.md** - Instalação rápida
- **README.md** atualizado com nova seção de instalação

### 🔄 Sistema de Versionamento
- Versão injetada automaticamente via git tags
- Comparação semântica de versões (MAJOR.MINOR.PATCH)
- Cache de verificação (24 horas)
- Suporte a GitHub token para rate limiting

## 🆕 Novas Funcionalidades

### Scripts de Instalação
- ✅ `install-from-github.sh` - Instalador completo
  - Verifica requisitos (Go, Git, kubectl, Azure CLI)
  - Clona repositório automaticamente
  - Compila com injeção de versão
  - Instala em `/usr/local/bin/`
  - Copia scripts utilitários
  - Testa instalação

### Auto-Update
- ✅ `auto-update.sh` - Script de atualização automática
  - `--yes` - Auto-confirmar (sem perguntar)
  - `--dry-run` - Simular sem executar
  - `--check` - Apenas verificar status
  - `--force` - Forçar reinstalação

### Comandos
```bash
# Instalação
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Verificar updates
k8s-hpa-manager version

# Auto-update
~/.k8s-hpa-manager/scripts/auto-update.sh
~/.k8s-hpa-manager/scripts/auto-update.sh --yes      # Automação
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run  # Teste

# Web server
k8s-hpa-web start/stop/status/logs/restart
```

## 🔧 Melhorias

- ✅ Sistema de updates totalmente automático
- ✅ Notificações no TUI (StatusContainer)
- ✅ Cache de verificação (evita spam de requisições)
- ✅ Scripts utilitários sempre disponíveis
- ✅ Fácil gerenciamento do servidor web
- ✅ Desinstalação limpa

## 📦 Instalação

### Método 1: Instalação Automática (Recomendado)
```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

### Método 2: Download de Binário
```bash
# Download
wget https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/download/v1.2.0/k8s-hpa-manager-linux-amd64

# Instalar
chmod +x k8s-hpa-manager-linux-amd64
sudo mv k8s-hpa-manager-linux-amd64 /usr/local/bin/k8s-hpa-manager
```

## 🔄 Atualização de v1.1.0

Se você está usando v1.1.0:

```bash
# Verificar se há update
k8s-hpa-manager version

# Opção 1: Re-executar instalador
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Opção 2: Auto-update (se instalado via script)
~/.k8s-hpa-manager/scripts/auto-update.sh
```

## 🐛 Correções

- Documentação atualizada e expandida
- README.md reorganizado com instalação em destaque
- CLAUDE.md atualizado com seção de instalação/updates

## 📝 Notas Técnicas

### Versionamento
- Versão injetada via `-ldflags` durante build
- Detecção automática via `git describe --tags`
- Verificação via GitHub API (`/repos/.../releases/latest`)

### Cache
- Localização: `~/.k8s-hpa-manager/.update-check`
- Validade: 24 horas
- Forçar nova verificação: `rm ~/.k8s-hpa-manager/.update-check`

### Scripts Utilitários
- `k8s-hpa-web` - Atalho para `web-server.sh`
- `auto-update.sh` - Sistema de atualização
- `uninstall.sh` - Desinstalação
- `backup.sh` / `restore.sh` - Backup/restore (dev)
- `rebuild-web.sh` - Rebuild web interface

## 🔗 Links

- **GitHub**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA
- **Documentação**: Ver `CLAUDE.md` no repositório
- **Guia de Instalação**: `INSTALL_GUIDE.md`
- **Sistema de Updates**: `UPDATE_BEHAVIOR.md`

---

**Full Changelog**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/compare/v1.1.0...v1.2.0
