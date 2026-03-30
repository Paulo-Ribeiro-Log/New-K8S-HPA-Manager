# Plano de Correções e Melhorias

Gerado em: 2026-03-29

---

## P0 — Crítico (bugs com impacto direto em produção)

- [x] **Cache key inconsistente** — `GetHPAMetrics` usa `"hpa_metrics:%s"` mas `ClearCacheForHPA` usa `"hpa:%s"` → invalidação nunca funciona (`internal/monitoring/engine/monitoring_v2.go`)
- [x] **Goroutine leak no MemoryCache** — `cleanupLoop` roda indefinidamente sem canal de parada; cada `NewMemoryCache` vaza uma goroutine (`internal/monitoring/cache/memory_cache.go`)
- [x] **`exec.Command` sem timeout no NodePool** — `az aks nodepool operation-abort` e `az account set` sem `CommandContext`/timeout; handler HTTP pode travar indefinidamente (`internal/web/handlers/nodepools.go:76-81, 222`)

---

## P1 — Alto (bugs que afetam UX de forma perceptível)

- [x] **MonitoringEngine não é restartável** — `Stop()` fecha `stopCh`/cancela `ctx` sem reiniciar; chamada a `Start()` após `Stop()` falha (`internal/monitoring/engine/monitoring_v2.go:83-110`)
- [x] **`context.Background()` em handlers** — operações continuam após cliente desconectar; usar `c.Request.Context()` (`nodepools.go:403,629`, `cronjobs.go:78,373,392`, `prometheus.go:87,105,123,210,484`)
- [x] **`isRunning` nunca reseta em sucesso** — botão "Run" do Health Check fica desabilitado permanentemente (`src/hooks/useHealthChecking.ts:26-59`)
- [x] **Toast vazio no NodePoolTab** — `toast.warning()` e `toast.info()` sem mensagem (`src/components/NodePoolTab.tsx:121,125`)

---

## P2 — Médio (UX e manutenibilidade)

- [ ] **`switchContext` global** — troca de cluster muda contexto kubectl para todos os usuários simultaneamente (`HPATab.tsx:72-81`, `NodePoolTab.tsx:130-139`)
- [ ] **Token POC hardcoded** — fallback `"poc-token-123"` + chave localStorage errada `"token"` vs `"auth_token"` (`src/hooks/useNotifications.ts:36`)
- [ ] **`useSSE` reconecta a cada render** — callbacks instáveis nas dependências do `useEffect` causam reconexão e perda de eventos (`src/hooks/useSSE.ts:108`)
- [ ] **`useSSE` acumula eventos sem limite** — array cresce indefinidamente em operações longas (`src/hooks/useSSE.ts:68`)
- [ ] **7 hooks duplicados em `useAPI.ts`** — `useConfigMaps`, `useSecrets`, `useDeployments`, `useDaemonSets`, `useStatefulSets`, `useIngresses`, `usePods` são quase idênticos; `useIngresses`/`usePods` sem auto-refresh (inconsistência)
- [ ] **Código morto** — `useCronJobsOld`, `usePrometheusOld` (`useAPI.ts:646-751`) e `ApplyAllModal_old.tsx` nunca importados
- [ ] **Padrão de resposta HTTP inconsistente** — campo de erro ora `"error"`, ora `"message"`, ora `"error.message"` entre handlers

---

## P3 — Baixo (performance e limpeza)

- [ ] **N+1 queries no HPA List** — lista namespaces + query por namespace; usar `ListHPAs(ctx, "")` para buscar tudo de uma vez (`internal/web/handlers/hpas.go:70-92`)
- [ ] **7 polling intervals simultâneos** — cada hook cria próprio `setInterval`; adicionar visibility-based polling (`document.visibilityState`) (`src/hooks/useAPI.ts`)
- [ ] **Logging excessivo em `Info`** — `GetHPAMetrics` loga em Info a cada chamada; mudar para `Debug` (`internal/monitoring/engine/monitoring_v2.go:153`)

---

## Funcionalidades Novas (Backlog)

- [ ] **Undo/Rollback de operações** — history tracker já salva before/after; adicionar botão "Reverter" na tela de histórico
- [ ] **Health Check agendado** — execuções automáticas com resultados no SQLite e notificações proativas
- [ ] **Diff cross-cluster de HPAs/NodePools** — comparar configuração HLG vs PRD antes de deployments
- [ ] **Rate limiting na API** — middleware por usuário/operação para evitar sobrecarga acidental
