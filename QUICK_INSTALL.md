# ⚡ Instalação Rápida - K8s HPA Manager

## 🚀 Instalação em 1 Comando

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

**Ou download e execute:**

```bash
wget https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh
chmod +x install-from-github.sh
./install-from-github.sh
```

---

## ✅ O que será instalado:

- ✅ Binário `k8s-hpa-manager` em `/usr/local/bin/`
- ✅ Scripts utilitários em `~/.k8s-hpa-manager/scripts/`:
  - `web-server.sh` - Gerenciar servidor web
  - `uninstall.sh` - Desinstalar aplicação
  - `backup.sh` / `restore.sh` - Backup/restore do código
- ✅ Atalho `k8s-hpa-web` para gerenciar servidor web
- ✅ Verificação automática de requisitos

---

## 📋 Requisitos:

- **Go 1.23+**
- **Git**
- **kubectl** (configurado)
- **Azure CLI** (opcional - para Node Pools)

---

## 🎯 Uso Rápido:

### TUI (Terminal Interface)
```bash
k8s-hpa-manager
```

### Web Interface
```bash
k8s-hpa-web start              # Iniciar servidor
# Acesse: http://localhost:8080
k8s-hpa-web stop               # Parar servidor
```

### Auto-descobrir Clusters
```bash
k8s-hpa-manager autodiscover
```

---

## 📚 Documentação Completa:

- **Guia de Instalação**: [INSTALL_GUIDE.md](INSTALL_GUIDE.md)
- **Documentação Técnica**: [CLAUDE.md](CLAUDE.md)
- **Releases**: [GitHub Releases](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases)

---

## 🗑️ Desinstalar:

```bash
~/.k8s-hpa-manager/scripts/uninstall.sh
```

---

**Pronto para começar!** 🎉
