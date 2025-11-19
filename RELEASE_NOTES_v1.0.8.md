# Release Notes - v1.0.8

**Data de lançamento:** 17 de novembro de 2025

## 🎯 Destaques

Esta release traz correções importantes no instalador, interface web e sistema de testes.

---

## 🔧 Correções

### Instalador Automático
- ✅ **Instalador agora busca versão latest automaticamente**
  - Removida versão hardcoded v1.0.6
  - Implementado fetch automático via GitHub API
  - Fallback para v1.0.7 se API falhar
  - Feedback visual da versão detectada

### Interface Web (Frontend)

#### ConfigMapsTab.tsx
- ✅ **Corrigida race condition no sistema de Undo/Redo**
  - Histórico agora usa índice atual ao invés de closure stale
  - Callback handleEditorChange sem dependências (performance)

- ✅ **Corrigida configuração diff2html**
  - Removidas propriedades inexistentes (`inputFormat`, `highlight`)
  - Configuração simplificada e funcional

- ✅ **Corrigidos tipos TypeScript em onClick handlers**
  - Assinatura da função ajustada para boolean simples
  - Type-safety melhorada em todos os botões

#### SecretsTab.tsx
- ✅ **Mesmas correções aplicadas**
  - Race condition no Undo/Redo corrigida
  - Configuração diff2html simplificada
  - Tipos onClick corrigidos

### Sistema de Monitoramento (Backend)

- ✅ **Isolado código legado do monitor**
  - Diretório `internal/monitoring/monitor/` renomeado para `monitor.legacy/`
  - Previne erros de compilação com pacote `analyzer` inexistente
  - Código preservado para referência futura

- ✅ **Corrigidos testes de Prometheus URL discovery**
  - Testes agora usam URLs corretas com prefixo `akspriv-`
  - Pattern correto: `https://prometheus-akspriv-{nome}-{env}.viavarejo.com.br/`
  - 100% de cobertura nos testes unitários

---

## 📦 Binários Disponíveis

- **Linux AMD64**: `new-k8s-hpa-linux-amd64`
- **macOS Intel (AMD64)**: `new-k8s-hpa-darwin-amd64`
- **macOS Apple Silicon (ARM64)**: `new-k8s-hpa-darwin-arm64`

---

## 🚀 Instalação

### Instalação Automática (Recomendado)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

**Agora busca automaticamente a versão latest (v1.0.8)!**

### Download Manual

Baixe o binário adequado para sua plataforma nos assets abaixo.

---

## 🔄 Upgrade de v1.0.7

```bash
# Via script de auto-update
~/.k8s-hpa-manager/scripts/auto-update.sh

# Ou reinstalação completa
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

---

## 📝 Commits Incluídos

1. **2694030** - fix: alterar instalador para buscar versão latest automaticamente do GitHub
2. **f7b2a2f** - fix: corrigir erros TypeScript em ConfigMapsTab e SecretsTab
3. **60a9a56** - fix: corrigir erros de compilação e testes

---

## 🐛 Problemas Conhecidos

Nenhum problema conhecido nesta release.

---

## 📚 Documentação

- **Instalação**: [INSTALL_GUIDE.md](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/blob/main/INSTALL_GUIDE.md)
- **Sistema de Updates**: [UPDATE_BEHAVIOR.md](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/blob/main/UPDATE_BEHAVIOR.md)
- **Guia de Desenvolvimento**: [CLAUDE.md](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/blob/main/CLAUDE.md)

---

**Versão anterior:** v1.0.7
**Versão atual:** v1.0.8
**Próxima versão planejada:** TBD
