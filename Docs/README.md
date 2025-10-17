# 📚 Documentação - k8s-hpa-manager

Este diretório contém toda a documentação do projeto k8s-hpa-manager.

---

## 🌐 Interface Web (POC) - 85% Completa

**Status:** ✅ Backend funcional | Frontend funcional | Sistema de validação implementado

### Documentos Principais (Leia nesta ordem)

1. **[README_WEB.md](./README_WEB.md)** ⭐ **COMECE AQUI**
   - Índice completo da documentação web
   - Quick start e primeiros passos
   - Overview da arquitetura

2. **[WEB_POC_STATUS.md](./WEB_POC_STATUS.md)** 📊 **STATUS ATUAL**
   - Progresso detalhado (85% completo)
   - Features implementadas vs pendentes
   - Checklist de tarefas

3. **[WEB_INTERFACE_DESIGN.md](./WEB_INTERFACE_DESIGN.md)** 🏗️ **ARQUITETURA**
   - Design completo da arquitetura
   - Estrutura de arquivos
   - API endpoints e contratos

4. **[WEB_VALIDATION_SYSTEM.md](./WEB_VALIDATION_SYSTEM.md)** 🔒 **VALIDAÇÃO**
   - Sistema de validação Azure AD + VPN
   - Cache thread-safe (5 min TTL)
   - Timeout configurável (5s)

5. **[WEB_NODEPOOLS_IMPLEMENTED.md](./WEB_NODEPOOLS_IMPLEMENTED.md)** 🖥️ **NODE POOLS**
   - Implementação completa do endpoint
   - Integração Azure CLI
   - Frontend com grid responsivo

### Documentos de Sessões Anteriores

- **[CONTINUE_AQUI.md](./CONTINUE_AQUI.md)** - Guia de continuidade
- **[RESUMO_SESSAO.md](./RESUMO_SESSAO.md)** - Resumo de sessão
- **[START_HERE.txt](./START_HERE.txt)** - Ponto de partida
- **[GIT_COMMIT_MESSAGE.txt](./GIT_COMMIT_MESSAGE.txt)** - Template de commit

### Documentos de Bug Fixes

- **[WEB_BUGFIX_ROUND2.md](./WEB_BUGFIX_ROUND2.md)** - Correções round 2
- **[WEB_BUG_FIX.md](./WEB_BUG_FIX.md)** - Correções iniciais
- **[WEB_FINAL_FIX.md](./WEB_FINAL_FIX.md)** - Correções finais

### Documentos de Planejamento

- **[WEB_OFFICIAL_PLAN.md](./WEB_OFFICIAL_PLAN.md)** - Plano oficial
- **[WEB_NEW_DESIGN.md](./WEB_NEW_DESIGN.md)** - Novo design
- **[WEB_IMPLEMENTATION_STATUS.md](./WEB_IMPLEMENTATION_STATUS.md)** - Status de implementação
- **[WEB_POC_TEST_RESULTS.md](./WEB_POC_TEST_RESULTS.md)** - Resultados de testes

### Outros Documentos

- **[INSTRUCOES_USUARIO.md](./INSTRUCOES_USUARIO.md)** - Instruções para usuário

---

## 🖥️ Interface TUI (Terminal)

Documentação da interface TUI principal está no arquivo raiz:
- **[../CLAUDE.md](../CLAUDE.md)** - Documentação completa do projeto

### Documentos de Features TUI

- **[BUG_FIX_MINREPLICAS.md](./BUG_FIX_MINREPLICAS.md)** - Correção MinReplicas
- **[DEBUG_TERMINAL_SIZE.md](./DEBUG_TERMINAL_SIZE.md)** - Debug tamanho terminal
- **[HPA_ROLLOUT_SESSION_FIX.md](./HPA_ROLLOUT_SESSION_FIX.md)** - Fix rollout em sessões
- **[LAYOUT_TEST_README.md](./LAYOUT_TEST_README.md)** - Testes de layout
- **[MOUSE_SCROLL_FEATURES.md](./MOUSE_SCROLL_FEATURES.md)** - Features de scroll
- **[NODEPOOL_SEQUENTIAL_EXECUTION.md](./NODEPOOL_SEQUENTIAL_EXECUTION.md)** - Execução sequencial
- **[ROLLOUT_DISPLAY_FIX.md](./ROLLOUT_DISPLAY_FIX.md)** - Fix display de rollout

---

## 📊 Quick Stats

### Interface Web POC
- **Progresso:** 85% completo
- **Backend:** ✅ Clusters, Namespaces, HPAs, Node Pools
- **Frontend:** ✅ Login, Dashboard, Edição, Grid Node Pools
- **Validação:** ✅ Azure AD + VPN com cache
- **Pendente:** CronJobs (20min), Rollouts (40min), Sessions (1h)

### Interface TUI
- **Status:** ✅ 100% funcional
- **Features:** HPAs, Node Pools, CronJobs, Prometheus, Rollouts
- **Sessões:** Save/Load com templates
- **Logs:** Sistema completo (F3)
- **Validação:** Azure AD + VPN on-demand

---

## 🚀 Quick Start

### Interface Web
```bash
# Build
go build -o ./build/k8s-hpa-manager .

# Iniciar modo web
./build/k8s-hpa-manager web --port 8080

# Acessar
# Browser: http://localhost:8080
# Token: poc-token-123
```

### Interface TUI
```bash
# Build
make build

# Executar
./build/k8s-hpa-manager

# Ou instalar globalmente
./install.sh
k8s-hpa-manager
```

---

## 📝 Contribuindo

Para adicionar nova documentação:

1. Crie o arquivo em `Docs/`
2. Adicione link neste README
3. Atualize `CLAUDE.md` se relevante
4. Commit com mensagem descritiva

---

## 🔗 Links Úteis

- **Repositório:** [GitHub](https://github.com/seu-usuario/k8s-hpa-manager)
- **Issues:** [GitHub Issues](https://github.com/seu-usuario/k8s-hpa-manager/issues)
- **Documentação Principal:** [CLAUDE.md](../CLAUDE.md)

---

**Última atualização:** 16 de Outubro de 2025
**Versão POC Web:** 85% completa
**Versão TUI:** 1.0.0
