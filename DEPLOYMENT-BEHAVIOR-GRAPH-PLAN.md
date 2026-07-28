# Plano: Gráfico de Comportamento do Deployment + Indicador Dynatrace na aba Pods

Status: ✅ Fase 0 concluída (branch `feat/pods-dynatrace-monitoring-status`, mergeada com `main` local). Fases 1-3 ainda não iniciadas.

## Contexto

A aba Pods hoje mostra o dono do pod só como texto solto (`ownerWorkload`, ex. "Deployment/checkout-api") — não há nenhum jeito de visualizar o comportamento histórico daquele Deployment (réplicas ao longo do tempo, uso de CPU/mem, restarts) a partir dali. Levantamento no app inteiro confirmou: `DeploymentsTab.tsx` já tem "Análise Preditiva" (score computado on-demand, sem gráfico) e "Histórico de Análises" (`PredictionHistoryModal.tsx`, lista textual de runs passados, sem gráfico) — **não existe hoje nenhum gráfico histórico por deployment individual em lugar nenhum do app**. É uma lacuna real, não uma feature subutilizada.

O objetivo é dar ao analista, direto do quick-view de um pod, uma visão "o que aconteceu com este Deployment nas últimas N horas" — réplicas escalando, pico de CPU/mem, restarts, e (Fase 2) se o Dynatrace abriu algum problem nesse intervalo. Serve tanto para troubleshooting reativo ("por que este pod reiniciou?") quanto para dar contexto de comportamento normal (comparação D-1/D-2/D-3, mesmo padrão já validado em produção no Conntrack Viewer).

Pedido adicional, mesma leva de trabalho: indicador visual por pod (painéis esquerdo e direito da aba Pods) mostrando se aquele pod está sendo monitorado pelo Dynatrace — ver Fase 0.

**Fonte de dados do gráfico de comportamento: Prometheus como fonte primária (mais barata, universal — funciona em AKS/EKS/GKE), Dynatrace como FALLBACK real de série temporal quando o cluster não tem Prometheus instalado** (não só como anotação — nem todo cluster tem Prometheus instalado, isso é ausência estrutural em parte da frota, não só "temporariamente fora do ar"). Dynatrace já tem o catálogo de métricas necessário para cobrir esse caso: entity type `CLOUD_APPLICATION`/`CLOUD_APPLICATION_INSTANCE` mapeia para `k8sWorkloadMetricDefs` (`internal/dynatrace/metrics.go:71-78`) — `pods_running`, `pods_ready_pct`, `pod_restarts`, `cpu_milli`, `cpu_throttle`, `memory_mb` — cobertura quase 1:1 com o que o gráfico precisa, via `getMetricsBatch(ctx, entityID, defs, from, to, resolution)` (`internal/dynatrace/metrics.go:195`), que já retorna série temporal completa. Dynatrace só serve como fallback em clusters AKS (única cloud com correlação DT neste app) — em EKS/GKE sem Prometheus, não há fonte disponível, e o gráfico mostra estado vazio explícito, não um erro.

Nenhum SQLite novo de série contínua: seria reimplementar o que Prometheus/Dynatrace já fazem — este é um gráfico de diagnóstico (janela de horas/dias), não de capacity planning de longo prazo (caso que já justificou SQLite em `snat_history_store.go`/conntrack).

---

## Fase 0 — Indicador de status de monitoramento Dynatrace por pod

Independente do gráfico de comportamento (Fases 1-3) — pode ser feita em paralelo ou antes.

Três estados visuais nas duas listas de pods (painel esquerdo — cards; painel direito — `PodMonitorTable`):
- 🟢 **Monitorado**: `CheckCircle2` (lucide-react) verde — pod tem entidade Dynatrace correspondente.
- 🟡 **Não monitorado (warning)**: `AlertTriangle` âmbar — DT configurado/disponível pro cluster, mas este pod não tem entidade (OneAgent não injetado ou não descoberto).
- ⛔ **Sem monitoramento (proibido)**: `Ban` cinza — DT não se aplica a este cluster (não é AKS, ou sem credenciais DT no servidor).

### Checklist

- [x] `internal/dynatrace/models.go`: campo `K8sPodName string` adicionado em `EntityStub`
- [x] `internal/dynatrace/client.go`: `enrichFromEntity` propaga `stub.K8sPodName = corr.PodName`
- [x] `internal/dynatrace/pod_monitoring.go` (novo): `ListMonitoredPods` — `ListEntitiesByCluster(ctx, clusterName, "CLOUD_APPLICATION_INSTANCE")` + `EnrichEntitiesWithK8s`, set `"namespace/podname"`, cache em memória TTL 2min (`sync.Map`, mesmo padrão do `entityCache` já existente em `client.go`)
- [x] `internal/web/handlers/pods_dynatrace_status.go` (novo arquivo — `pods.go` já tinha 2000+ linhas): `GetDynatraceStatus` → `GET /api/v1/pods/:cluster/dynatrace-status?ai_email=`, resposta `{cluster_supported, monitored: string[]}`. `cluster_supported` = AKS (`config.DetectCloudProvider` via `kubeManager.GetServerURL`) + cliente DT resolvido (`dynatraceClientForPods`, mesma resolução de credenciais de `DynatraceHandler.clientForUser` — tokens do usuário via `UserTokensStore`, fallback env vars). Falha ao consultar DT → mesma resposta de `cluster_supported=false`, nunca 5xx. `PodHandler` ganhou o campo `tokensStore *storage.UserTokensStore` (`NewPodHandler` com novo parâmetro — call site em `server.go` atualizado: `handlers.NewPodHandler(s.kubeManager, s.historyTracker, s.aiTokensStore)`)
- [x] Rota registrada em `internal/web/server.go` (grupo `pods`, ao lado de `/browse`)
- [x] `internal/web/frontend/src/hooks/useAPI.ts`: `useDynatracePodStatus(cluster, aiEmail?)` → `{ clusterSupported, monitoredKeys: Set<string>, hasLoaded }` — `hasLoaded` foi adicionado além do previsto no plano original, pra distinguir "ainda não sabemos" (não renderiza ícone) de "sabemos que não é suportado" (ícone de proibido); sem isso o ícone piscaria "proibido" por um instante em todo cluster suportado. Poll a cada 3min
- [x] `internal/web/frontend/src/components/DynatraceStatusIcon.tsx` (novo): `DynatraceStatusIcon` (ícone+tooltip via `title`, padrão nativo já usado em `PodMonitorTable.tsx`, sem shadcn `Tooltip`) + helper `resolveDynatraceStatus(clusterSupported, monitoredKeys, podKey)`
- [x] `internal/web/frontend/src/components/PodsPanel.tsx`: ícone no card do painel esquerdo (`useUserProfile()` fornece o `ai_email`), condicionado a `dtHasLoaded`
- [x] `internal/web/frontend/src/components/PodMonitorTable.tsx`: nova coluna FIXA "DT" (24px, sem `ResizeHandle`, inserida entre `dot` e `READY` — `INITIAL_WIDTHS` e todos os índices de `resize()` de READY em diante deslocados em +1). Recebe `dtClusterSupported`/`dtMonitoredKeys`/`dtHasLoaded` como props do painel pai (`PodsPanel.tsx`) em vez de rodar o próprio polling — evita duplicar a chamada entre os dois painéis
- [x] Verificação automatizada: `go build ./...`, `go vet ./internal/dynatrace/... ./internal/web/handlers/...`, `tsc --noEmit`, `npm run lint` (sem novos erros — só 1 warning `react-refresh/only-export-components` em `DynatraceStatusIcon.tsx` por exportar `resolveDynatraceStatus` junto do componente, mesmo padrão já tolerado em outros arquivos do repo)
- [x] Teste manual no navegador — validado contra um cluster k3s real (não-AKS, via Docker): `GET /dynatrace-status` retorna `cluster_supported:false`, ícone `Ban` (cinza) aparece corretamente nos dois painéis (card esquerdo e coluna da `PodMonitorTable`), sem erros de console. **Caminho `cluster_supported=false` confirmado; os estados "monitorado" (verde) e "não monitorado" (âmbar) ainda não foram validados contra um cluster AKS real com Dynatrace configurado** — este ambiente de sandbox não tem acesso a nenhum cluster AKS real nem credenciais Dynatrace; validar no ambiente do usuário antes de considerar 100% coberto.

---

## Fase 1 — MVP do gráfico de comportamento (Prometheus + fallback Dynatrace)

### Backend

- [ ] `internal/monitoring/client/prometheus_client.go`: adicionar 3 funções novas (**não mexer em `GetHPAHistoricalMetrics`** — ver justificativa abaixo)
  ```go
  func (c *PrometheusClient) GetDeploymentHistoricalMetrics(ctx context.Context, namespace, deployment string, duration, step time.Duration) (map[string]*QueryRangeResult, error)
  func (c *PrometheusClient) GetDeploymentHistoricalMetricsWithOffset(ctx context.Context, namespace, deployment string, duration, step, offset time.Duration) (map[string]*QueryRangeResult, error)
  func (c *PrometheusClient) deploymentHistoricalMetricsRange(ctx context.Context, namespace, deployment string, start, end time.Time, step time.Duration) (map[string]*QueryRangeResult, error) // corpo compartilhado
  ```
  Queries (via `run(key, query)`, best-effort por chave):

  | key | PromQL |
  |---|---|
  | `replicas_desired` | `kube_deployment_spec_replicas{namespace="%s",deployment="%s"}` |
  | `replicas_current` | `kube_deployment_status_replicas{namespace="%s",deployment="%s"}` |
  | `replicas_ready` | `kube_deployment_status_replicas_ready{namespace="%s",deployment="%s"}` |
  | `replicas_updated` | `kube_deployment_status_replicas_updated{namespace="%s",deployment="%s"}` |
  | `replicas_unavailable` | `kube_deployment_status_replicas_unavailable{namespace="%s",deployment="%s"}` |
  | `cpu` | `avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[1m]) / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s-.*",resource="cpu"}) * 100` |
  | `memory` | mesmo padrão, `container_memory_working_set_bytes` / `resource="memory"` |
  | `restarts` | `increase(kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s-.*"}[step])` — **usar `increase()`**, não o contador cru (dá "restarts neste intervalo", não total acumulado) |

  `cpu_request`/`cpu_limit`/`memory_request`/`memory_limit`: instant `Query()`, não `QueryRange()` (mudam raramente, economiza 4 séries range). Usar `pod=~"%s-.*"` (com hífen — mais preciso que o `%s.*` de `GetHPAHistoricalMetrics`, evita match espúrio tipo `foo` casando `foobar-xyz`).

  **Por que função nova em vez de adaptar `GetHPAHistoricalMetrics`**: 4 das 8 queries dela dependem de `kube_horizontalpodautoscaler_*` indexado pelo NOME DO HPA — não existe se o Deployment não tiver HPA, e reaproveitar passando `deployment` no lugar de `hpaName` retornaria vazio sempre que os nomes divergirem (bug sutil). Ela já está em produção (tela HPA) — não mexer.

- [ ] Detecção de HPA (`has_hpa`): via K8s API (`h.kubeManager.GetClient(cluster)` → `AutoscalingV2().HorizontalPodAutoscalers(namespace).List()`, checar `scaleTargetRef.name == deployment`), não PromQL
- [ ] Scale events: `func deriveScaleEvents(points []DeploymentBehaviorPoint) []DeploymentScaleEvent` — diff sequencial de `replicas_desired`, no backend (zero queries extra, mantém frontend "burro")
- [ ] Criar `internal/web/handlers/deployment_behavior.go` com os tipos de resposta e `func (h *DeploymentHandler) GetDeploymentBehavior(c *gin.Context)`:
  ```go
  type DeploymentBehaviorPoint struct {
      Timestamp int64 `json:"ts"`
      ReplicasDesired, ReplicasCurrent, ReplicasReady, ReplicasUpdated, ReplicasUnavailable float64
      CPUUsagePct, MemoryUsagePct, Restarts float64
  }
  type DeploymentScaleEvent struct {
      Timestamp int64 `json:"ts"`
      FromReplicas, ToReplicas float64
  }
  type DeploymentBehaviorResponse struct {
      Cluster, Namespace, Deployment string
      Hours, StepMinutes int
      OffsetDays []int
      Points []DeploymentBehaviorPoint
      ComparePoints map[int][]DeploymentBehaviorPoint `json:"compare_points,omitempty"` // chave = offset em dias
      ScaleEvents []DeploymentScaleEvent
      HasHPA, PrometheusAvailable bool
      Source string `json:"source"` // "prometheus" | "dynatrace" | "none" — cores já convencionadas no FinOps (DT=azul, Prometheus=laranja)
      Error string `json:"error,omitempty"`
      DynatraceProblems []DTProblemMarker `json:"dynatrace_problems,omitempty"` // vazio até a Fase 2
  }
  ```
  Rota: `GET /api/v1/deployments/:cluster/:namespace/:name/behavior?hours=6&step=5&offset_days=1,2,3`, registrada em `server.go` no grupo `deployments` já existente (perto de `deployments.GET("/:cluster/:namespace/:name/describe", ...)`)

- [ ] Precedência de fonte no handler: tenta Prometheus (`h.getPromClient(cluster)`) primeiro; se falhar por qualquer motivo (não instalado ou fora do ar — mesmo tratamento), tenta Dynatrace só se AKS + credenciais DT configuradas (reaproveitar checagem da Fase 0). Nenhuma das duas → `source:"none"`, `points:[]`, HTTP 200, sem erro
- [ ] Caminho Dynatrace: `ResolveEntityForWorkload` → `client.getMetricsBatch(ctx, entityID, k8sWorkloadMetricDefs, from, to, resolution)` → mapear `pods_running`/`pods_ready_pct`/`pod_restarts`/`cpu_milli`/`memory_mb` pro shape comum. `replicas_desired/updated/unavailable` sem equivalente DT — ficam vazios, scale-events some nesse caminho (limitação documentada, não simular)
- [ ] Criar `internal/dynatrace/workload_resolver.go`:
  ```go
  // ResolveEntityForWorkload resolve o entityID Dynatrace (CLOUD_APPLICATION) de um Deployment,
  // compondo ListEntitiesByCluster + EnrichEntitiesWithK8s + filtro por K8sNamespace/K8sWorkload.
  // Cacheado em memória por (cluster,namespace,deployment). Compartilhado com o overlay de
  // problems da Fase 2 — não duplicar.
  func (c *Client) ResolveEntityForWorkload(ctx context.Context, clusterName, namespace, deploymentName string) (entityID string, found bool, err error)
  ```
- [ ] `internal/web/handlers/deployments.go`: `DeploymentHandler` ganha cache de client Prometheus por cluster (mesmo padrão de `NodePoolHandler.getPromClient`, `nodepools.go:104`) — considerar extrair um helper compartilhado `getOrCreatePromClient(...)` pros dois handlers em vez de colar uma 3ª cópia
- [ ] Timeout do handler: 45s (vs 30s do conntrack — mais queries). Paralelizar as chamadas dentro de `deploymentHistoricalMetricsRange` com goroutines + `sync.WaitGroup`

### Frontend

- [ ] Criar `internal/web/frontend/src/lib/chartHelpers.ts`: extrair `decimate`, `COMPARE_COLORS`/`COMPARE_LABELS` e resolvers de cor/label por `dataKey`, hoje só em `ConntrackTab.tsx:73-74` — reaproveitado pelos dois componentes
- [ ] Criar `internal/web/frontend/src/components/DeploymentBehaviorChart.tsx` (componente próprio, não adaptação do `HistoryChart` do Conntrack — este precisa de painéis empilhados com escalas diferentes):
  1. Réplicas (linhas desired/current/ready) + scale events como `ReferenceLine x={ts}` verticais
  2. CPU% + Mem% (linhas 0-100%, `ReferenceLine` em 80%/95%)
  3. Restarts (barras)

  Comparação D-1/D-2/D-3 opt-in (toggle, nunca automático — evita 36 queries no caso comum). Badge de fonte (azul DT / laranja Prometheus). `source:"none"` → estado vazio explícito, nunca erro genérico
- [ ] `internal/web/frontend/src/lib/api/types.ts`: `DeploymentBehaviorPoint`, `DeploymentScaleEvent`, `DeploymentBehaviorResponse`
- [ ] `internal/web/frontend/src/lib/api/client.ts`: `getDeploymentBehavior(cluster, namespace, name, params)`
- [ ] `internal/web/frontend/src/components/PodQuickViewModal.tsx`: 5ª aba manual (tupla linha 843 `["details","logs","previous-logs","same-image"]` → `[...,"behavior"]`, label "Comportamento"; bloco `flex-1 min-h-0 overflow-y-auto`, **nunca shadcn `<Tabs>`**). Nome do deployment via `workloadSearchTerm` (linha 671). Checar `pod.ownerWorkload?.startsWith("Deployment/")` antes de habilitar — senão, estado desabilitado "Disponível apenas para Deployments"

### Verificação Fase 1
- [ ] `go build ./...`, `go vet ./internal/monitoring/client/... ./internal/dynatrace/... ./internal/web/handlers/...`, `tsc --noEmit`
- [ ] Teste manual: cluster AKS+Prometheus (com e sem HPA, D-1/D-2/D-3 em janela 24h, sem timeout)
- [ ] Teste manual: cluster AKS sem Prometheus + DT configurado (fallback funcionando, `source:"dynatrace"`, sem scale-events)
- [ ] Teste manual: cluster sem nenhuma fonte (`source:"none"`, estado vazio, sem 5xx)
- [ ] Teste manual: Prometheus configurado mas fora do ar (mesmo tratamento de "não instalado")

---

## Fase 2 — Overlay de problems do Dynatrace (aditivo, independente da fonte)

A resolução de entity (`ResolveEntityForWorkload`) já é infra da Fase 1 — aqui só falta buscar e sobrepor os *problems*. Útil mesmo quando a série veio do Prometheus (cluster pode ter as duas fontes ao mesmo tempo).

- [ ] `internal/dynatrace/`: nova variante `GetProblemsForEntityInWindow(ctx, entityID, from, to time.Time) ([]Problem, error)` — `GetOpenProblemsForEntity` hoje só cobre `status("OPEN")` hardcoded; esta cobre também fechados dentro da janela (API Problems v2 suporta `timeFrom`/`timeTo` sem filtro de status)
- [ ] `DeploymentBehaviorResponse.DynatraceProblems` populado só quando AKS + DT configurado + entity resolvido (reaproveita `ResolveEntityForWorkload` da Fase 1); `omitempty` em qualquer outro caso
- [ ] Frontend: `ReferenceArea` (Recharts) colorida por severidade, sobreposta ao painel de réplicas/CPU em `DeploymentBehaviorChart.tsx`

---

## Fase 3 — Reexposição em `DeploymentsTab.tsx`

Sem trabalho de backend adicional (endpoint já genérico por `cluster/namespace/name`).

- [ ] Reaproveitar `DeploymentBehaviorChart.tsx` tal qual
- [ ] Botão "Comportamento" ao lado de "Análise Preditiva"/"Histórico de Análises" já existentes em `DeploymentsTab.tsx`
- [ ] Decidir UX na hora: modal dedicado (tipo `PredictionHistoryModal.tsx`) vs. painel inline

---

## Referência rápida de arquivos

**Criar**: `internal/dynatrace/pod_monitoring.go`, `internal/web/frontend/src/components/DynatraceStatusIcon.tsx`, `internal/web/handlers/deployment_behavior.go`, `internal/dynatrace/workload_resolver.go`, `internal/web/frontend/src/components/DeploymentBehaviorChart.tsx`, `internal/web/frontend/src/lib/chartHelpers.ts`

**Modificar**: `internal/dynatrace/models.go`, `internal/dynatrace/client.go`, `internal/web/handlers/pods.go`, `internal/web/frontend/src/hooks/useAPI.ts`, `internal/web/frontend/src/components/PodsPanel.tsx`, `internal/web/frontend/src/components/PodMonitorTable.tsx`, `internal/monitoring/client/prometheus_client.go`, `internal/web/handlers/deployments.go`, `internal/web/handlers/nodepools.go` (opcional, extração de helper), `internal/web/server.go`, `internal/web/frontend/src/lib/api/types.ts`, `internal/web/frontend/src/lib/api/client.ts`, `internal/web/frontend/src/components/PodQuickViewModal.tsx`, `internal/web/frontend/src/components/ConntrackTab.tsx` (se extrairmos `chartHelpers.ts`)

## Referências de padrão já validadas em produção (reaproveitar, não reinventar)
- Histórico + comparação D-1/D-2/D-3: `internal/web/handlers/nodepools_conntrack.go` + `internal/web/frontend/src/components/ConntrackTab.tsx` (`HistoryChart`)
- Cache de client Prometheus por cluster: `internal/web/handlers/nodepools.go:104` (`getPromClient`)
- Resolução de credenciais Dynatrace do servidor: `internal/web/handlers/dynatrace.go:59-67`
- Precedência de fonte de métricas (DT/Prometheus) com campo `source`: `internal/finops/calculator.go` (`BuildReport`, campo `MetricsSource`)
- Regra de tabs manuais (nunca shadcn `<Tabs>` em contexto de altura flexível): ver `CLAUDE.md`, seção "shadcn Tabs em Modais com Altura Fixa"
