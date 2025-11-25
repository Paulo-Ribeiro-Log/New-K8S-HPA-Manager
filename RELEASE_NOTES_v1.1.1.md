# 🚀 Release v1.1.1 - Audit Log e CronJob Editor Aprimorado

## ✨ Novidades

### 📊 Sistema de Audit Log para Operações de Infraestrutura

**Rastreabilidade completa de operações críticas:**

- ✅ **Audit log de Cordon/Drain** - Todas as operações em node pools são registradas
- ✅ **Audit log de Rollouts** - Deployments, DaemonSets e StatefulSets auditados
- ✅ **Histórico persistente** - JSON files organizados por mês
- ✅ **Endpoint especializado** - `/api/v1/history/cordon-drain` com filtros e estatísticas
- ✅ **Rastreamento de duração** - Tempo de execução de cada operação
- ✅ **Status detalhado** - Success/Failed com mensagens de erro

**Novas Actions registradas:**
- `ActionCordonNode` - Node marcado como unschedulable
- `ActionDrainNode` - Pods evacuados do node
- `ActionNodePoolSequence` - Sequência completa de evacuação
- `ActionRolloutDeployment` - Rollout de Deployment executado
- `ActionRolloutDaemonSet` - Rollout de DaemonSet executado
- `ActionRolloutStatefulSet` - Rollout de StatefulSet executado

### 🕒 CronJob Editor Aprimorado

**Interface intuitiva com parser de cron:**

- ✅ **Descrições legíveis** - "0 5 * * *" → "Todos os dias às 05:00"
- ✅ **Editor visual** - Edição de schedule via interface web
- ✅ **Validação em tempo real** - Feedback instantâneo (verde/vermelho)
- ✅ **Preview ao vivo** - Veja a descrição enquanto digita
- ✅ **Guia integrado** - Ranges válidos exibidos no editor
- ✅ **Suporte completo** - Ranges, listas, steps (*/)

**Exemplos de transformações:**
- `0 5 * * *` → "Todos os dias às 05:00"
- `30 14 * * *` → "Todos os dias às 14:30"
- `0 9 * * 1` → "Todas as Segundas às 09:00"
- `*/15 * * * *` → "A cada 15 minutos"

## 🔧 Melhorias

- **Backend:** Endpoint de CronJob agora aceita `schedule` e `suspend`
- **Frontend:** Parser TypeScript completo para expressões cron
- **Documentação:** CLAUDE.md atualizado com caminho correto do binário
- **Interface:** Simplificada removendo elementos visuais excessivos

## 📦 Arquivos Modificados

### Backend
- `internal/history/tracker.go` - 6 novas action constants
- `internal/kubernetes/client.go` - Audit log em rollouts (+90 linhas)
- `internal/config/kubeconfig.go` - Propagação historyTracker (+30 linhas)
- `internal/web/handlers/nodepools.go` - Audit log cordon/drain (+86 linhas)
- `internal/web/handlers/history.go` - Endpoint especializado (+111 linhas)
- `internal/web/handlers/cronjobs.go` - Suporte a schedule

### Frontend
- `internal/web/frontend/src/lib/cronParser.ts` - Novo utilitário (+250 linhas)
- `internal/web/frontend/src/components/CronJobEditor.tsx` - Interface aprimorada
- `internal/web/frontend/src/hooks/useAPI.ts` - Tipo expandido

### Documentação
- `docs/history/CHANGELOG.md` - Documentação completa das features
- `CLAUDE.md` - Atualizado para v1.1.1

## 🐛 Correções

- Corrigido caminho do binário: `./build/new-k8s-hpa` (anteriormente incorreto)

## 📝 Commits desta Release

- `9111a79` - feat(audit): sistema completo de audit log para infraestrutura
- `7ced27b` - feat(web): interface aprimorada para CronJob Schedule Editor
- `417d9b3` - refactor(web): simplifica interface removendo grid visual
- `535ad19` - docs: corrige caminho do binário
- `20302cd` - chore: atualiza versão para v1.1.1

## 🔗 Instalação

### Via Script (Recomendado)
```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

### Build Manual
```bash
git clone https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager.git
cd New-K8S-HPA-Manager/Scale_HPA
make build
make web-build
./build/new-k8s-hpa web
```

## 📊 Estatísticas

- **+562 linhas** de código adicionadas (frontend)
- **+493 linhas** de código adicionadas (backend audit log)
- **6 novas actions** de audit log
- **1 novo utilitário** completo (cronParser)
- **2 endpoints** modificados/criados

---

**Full Changelog**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/compare/v1.0.10...v1.1.1
