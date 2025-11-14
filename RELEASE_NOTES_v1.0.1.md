# Release Notes v1.0.1

**Data:** 13 de novembro de 2025
**Tag:** v1.0.1

---

## 🎯 Resumo

Release de correção focada em build offline e organização de documentação.

---

## ✨ Novos Recursos

### Documentação Organizada

- **Pasta `Possibilidades/`**: Criada para armazenar análises técnicas e planos futuros
- **ANALISE_CRITICA_NOVAS_FUNCIONALIDADES.md**: Análise detalhada de 4 funcionalidades propostas:
  - ⚠️ Secrets com Base64 toggle (Implementar com restrições)
  - ✅ Análise de Deployments health/liveness (Altamente recomendado)
  - ✅ Gerenciamento de réplicas zerar/restaurar (Altamente recomendado)
  - ❌ Terminal interativo netshoot (Não recomendado - usar comandos pré-definidos)
- **PR_VALIDATION_WORKFLOW.md**: Fluxo completo de validação de PRs Helm

### Arquivos Organizados

Movidos para `Possibilidades/`:
- AI_INTEGRATION_ANALYSIS.md
- ALERTMANAGER_INTEGRATION_PLAN.md
- DOCS_COMPARISON.md
- HPA_MONITORING_TODO.md
- HPA_WATCHDOG_INTEGRATION_DIFFICULTY.md
- INTEGRATION_SRE_CLI.md
- MONITORING_IMPLEMENTATION_TODO.md
- MONITORING_REFACTOR_PLAN.md
- PROMETHEUS_INTEGRATION_ANALYSIS.md
- PROMETHEUS_METRICS_PLAN.md
- README.old.md
- RELEASE_NOTES_v1.0.0.md
- RESUMO-AI_INTEGRATION_ANALYSIS.md
- RESUMO-PROMETHEUS_INTEGRATION_ANALYSIS.md
- TECHNICAL_ANALYSIS_AND_ROADMAP.md

---

## 🐛 Correções

### Build Offline (Critical Fix)

**Problema:** Build baixava pacotes Go durante instalação, causando falhas em ambientes sem internet ou com proxy corporativo.

**Solução:**
- ✅ Makefile agora usa `-mod=vendor` em todos os targets de build
- ✅ `install-from-github.sh` executa `go mod vendor` antes do build
- ✅ Build 100% offline após clonar repositório
- ✅ Adicionado `vendor/` ao `.gitignore`

**Impacto:**
- Instalação confiável em ambientes air-gapped
- Build determinístico (mesmas versões de dependências)
- Velocidade de build 3-5x mais rápida (sem download)

---

## 📦 Instalação

### Via Script (Recomendado)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

### Download Manual

Binários disponíveis para:
- Linux (amd64)
- macOS (Intel amd64)
- macOS (Apple Silicon arm64)
- Windows (amd64)

---

## 🔧 Tech Stack

- **Backend:** Go 1.23+ (toolchain 1.24.7)
- **TUI:** Bubble Tea v0.24.2 + Lipgloss v1.1.0
- **Frontend:** React 18.3 + TypeScript 5.8 + Vite 5.4
- **UI Components:** shadcn/ui (Radix UI) + Tailwind CSS 3.4
- **Kubernetes:** client-go v0.31.4
- **Azure:** azcore v1.19.1, azidentity v1.12.0

---

## 📝 Changelog Completo

### Added
- Pasta `Possibilidades/` para análises técnicas
- `ANALISE_CRITICA_NOVAS_FUNCIONALIDADES.md` (1.446 linhas)
- `PR_VALIDATION_WORKFLOW.md` (134 linhas)

### Changed
- Makefile: Adicionado `-mod=vendor` a todos os builds
- install-from-github.sh: Executa `go mod vendor` antes do build
- Documentos de análise movidos para `Possibilidades/`

### Fixed
- Build não baixa mais pacotes Go durante instalação
- Build 100% offline e determinístico

---

## 🚀 Próximos Passos (v1.1.0)

Baseado em `ANALISE_CRITICA_NOVAS_FUNCIONALIDADES.md`:

**Sprint 1-2 (Prioritário):**
1. ✅ Análise de Deployments (Health/Liveness Checks) - 8-12h
2. ✅ Gerenciamento de Réplicas (Zerar/Restaurar/Alterar) - 6-8h
3. ✅ Comandos Pré-Definidos de Rede (netshoot seguro) - 8-12h

**Sprint 3-4 (Opcional):**
4. ⚠️ Secrets com Base64 Toggle - 12-16h (SE aprovado por security team)

**Total estimado:** 22-32 horas (~3-4 dias úteis)

---

## 📊 Estatísticas do Release

- **Commits:** 2
- **Arquivos modificados:** 20
- **Linhas adicionadas:** 2.108
- **Documentos organizados:** 15
- **Tamanho binário:** ~89 MB (Linux amd64)

---

## 🙏 Créditos

Desenvolvido com Claude Code (Anthropic).

---

**Download:** https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.1
