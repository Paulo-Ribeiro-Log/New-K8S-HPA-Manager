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

- [x] **`switchContext` global** — adicionado `isSwitchingContext` com disable+placeholder "Alternando..." no Select durante a troca (`HPATab.tsx`, `NodePoolTab.tsx`)
- [x] **Token POC hardcoded** — `"token"` → `"auth_token"`, fallback `"poc-token-123"` → `""` em todas as 4 ocorrências (`src/hooks/useNotifications.ts`)
- [x] **`useSSE` reconecta a cada render** — callbacks movidos para refs (`onEventRef`, `onErrorRef`, `onCompleteRef`); removidos das deps do `useEffect` (`src/hooks/useSSE.ts`)
- [x] **`useSSE` acumula eventos sem limite** — array limitado a `MAX_EVENTS = 200`; entradas antigas descartadas automaticamente (`src/hooks/useSSE.ts`)
- [ ] **7 hooks duplicados em `useAPI.ts`** — `useConfigMaps`, `useSecrets`, `useDeployments`, `useDaemonSets`, `useStatefulSets`, `useIngresses`, `usePods` são quase idênticos; `useIngresses`/`usePods` sem auto-refresh (inconsistência) ⏸ *adiado*
- [x] **Código morto** — removidos `useCronJobsOld`, `usePrometheusOld` e `ApplyAllModal_old.tsx`
- [ ] **Padrão de resposta HTTP inconsistente** — campo de erro ora `"error"`, ora `"message"`, ora `"error.message"` entre handlers ⏸ *adiado — requer testes completos*

---

## P3 — Baixo (performance e limpeza)

- [x] **N+1 queries no HPA List** — substituído loop de namespace+query por `ListHPAs(ctx, "")` + filtro pós-query via `IsSystemNamespace` exportado (`internal/web/handlers/hpas.go`, `internal/kubernetes/client.go`)
- [x] **7 polling intervals simultâneos** — adicionado `if (document.visibilityState !== "visible") return` nos 5 hooks com setInterval (ConfigMaps, Secrets, Deployments, DaemonSets, StatefulSets) (`src/hooks/useAPI.ts`)
- [x] **Logging excessivo em `Info`** — `GetHPAMetrics` alterado de `log.Info()` para `log.Debug()` (`internal/monitoring/engine/monitoring_v2.go`)

---

## Funcionalidades Novas (Backlog)

- [ ] **Undo/Rollback de operações** — history tracker já salva before/after; adicionar botão "Reverter" na tela de histórico
- [ ] **Health Check agendado** — execuções automáticas com resultados no SQLite e notificações proativas
- [ ] **Diff cross-cluster de HPAs/NodePools** — comparar configuração HLG vs PRD antes de deployments
- [ ] **Rate limiting na API** — middleware por usuário/operação para evitar sobrecarga acidental
