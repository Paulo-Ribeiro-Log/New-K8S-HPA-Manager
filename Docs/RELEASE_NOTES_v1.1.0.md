# Release v1.1.0

## 🎉 Novidades Principais

### 🌐 Interface Web Completa
- **Nova interface web React/TypeScript** com servidor backend integrado
- Dashboard com métricas reais de cluster
- Gerenciamento completo de HPAs e Node Pools via browser
- Sistema de sessões compatível com TUI
- Auto-shutdown inteligente (20min de inatividade)

### 🐛 Correções Importantes
- **Race condition corrigida** no sistema de testes de cluster
- Correção do memory state entre telas de Node Pools e HPAs
- Melhorias na estabilidade geral

### ⚡ Melhorias de Performance
- Sequenciamento otimizado de operações em node pools
- Sistema de logs melhorado (.gitignore atualizado)

## 📦 Como Usar

### TUI (Terminal Interface)
```bash
k8s-hpa-manager
```

### Web Interface
```bash
k8s-hpa-manager web
# Acesse: http://localhost:8080
```

## 🔧 Requisitos
- Go 1.23+
- Kubernetes cluster configurado
- Azure CLI (para operações de node pools)
- kubectl

## 📚 Documentação
Ver `CLAUDE.md` para documentação completa e guia de desenvolvimento.

---

**Full Changelog**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/compare/v1.0.0...v1.1.0
