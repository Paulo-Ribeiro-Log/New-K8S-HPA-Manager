# Latência de Aplicações — Teste sob Demanda + Prometheus/Dynatrace

Checklist de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

**Contexto e decisão principal (mudou após discussão)**: a entrega prioritária **não** é um
dashboard passivo de latência histórica — é uma ferramenta de **teste ativo sob demanda**: o
usuário escolhe uma aplicação (cluster/namespace/service), aperta um botão "Executar Teste", e a
ferramenta dispara requisições reais NAQUELE momento e devolve a latência medida na hora (min/avg/
p50/p95/p99/max, erros). Dashboards históricos via Prometheus/Dynatrace continuam no plano, mas
como **contexto complementar** exibido ao lado do resultado do teste (ex: "este teste: 42ms |
histórico P95 via DT: 55ms"), não como a feature principal.

**Por que teste ativo, não só leitura de série histórica**: séries do Prometheus/DT mostram o que
JÁ aconteceu (últimos 5min/1h/1d) — não respondem "e agora, nesse exato momento, qual a latência?".
Isso importa pra validar uma mudança recente (deploy, scale, mudança de config) sem esperar o
próximo scrape, e pra diagnosticar durante um incidente sem depender de instrumentação prévia do
app (funciona mesmo em apps sem métricas HTTP expostas).

---

## Decisão de arquitetura — como o teste roda de fato

**Onde a requisição parte**: de DENTRO do cluster, não do servidor da ferramenta — testar a
latência real "app → app" (o caminho que importa) exige estar na mesma rede/DNS interno
(`<service>.<namespace>.svc.cluster.local`), não atravessar VPN/internet a partir de onde a
ferramenta roda.

**Mecanismo escolhido — pod efêmero + exec (reaproveita padrão já existente)**:
1. Cria um Pod curto (`image: curlimages/curl`, `command: ["sleep", "300"]`,
   `ActiveDeadlineSeconds: 300` como cinto de segurança) no **mesmo namespace** do alvo — respeita
   NetworkPolicy do namespace, já que o pod nasce "de dentro".
2. Espera o pod ficar `Running` (poll simples, mesmo padrão de espera já usado em operações de
   scale/rollout no `kubernetes/client.go`).
3. Executa dentro dele um loop de `curl` via **exec** — reaproveita **`execCmdInPod`**
   (`internal/web/handlers/nodepools_conntrack.go:226`), que já é genérico (clientset, restConfig,
   namespace, podName, container, cmd) e não depende de hostNetwork — foi só usado até hoje com pod
   hostNetwork (pro caso do conntrack), mas serve pra qualquer pod.
4. `curl -o /dev/null -s -w "%{time_total}\n" <url>` repetido N vezes num script bash — 1 exec só
   (não N execs), parseia cada linha (segundos → ms) no backend.
5. Deleta o pod no fim (`defer`, sempre) — cinto de segurança adicional:
   `ActiveDeadlineSeconds` garante que o K8s mata o pod sozinho mesmo se o processo do servidor
   morrer no meio do teste. Considerar também uma varredura periódica (igual em espírito à limpeza
   de zumbis do SSE, `sseCleanupInterval` em `progress.go`) que deleta pods de teste órfãos por
   label + idade, como rede de segurança extra.

**Alvo do teste**: campo de URL livre (`http://<service>.<namespace>.svc.cluster.local:<porta>/<path>`),
com um seletor de Service (lista já existente na aba Services) como atalho pra pré-preencher —
não force o usuário a montar a URL manualmente, mas não trave o teste a "só serviços conhecidos"
(permite testar Ingress externo, outro namespace, etc.).

**Progresso em tempo real**: SSE, mesmo broker já usado em Cordon/Drain/Health Check/Helm/Command
Runner (`internal/web/sse/progress.go` — `ProgressTracker`/`ProgressReporter`). Eventos: "pod
criado", "aguardando pod ready", "executando N requisições...", resultado final.

**Guardrails (não negociável, evita virar ferramenta de DoS acidental)**:
- Atrás de `RequireSREGroup()` (mesmo padrão do Command Runner)
- Máximo de requisições por teste configurável mas com teto hardcoded (ex: 200)
- Requests/limits pequenos no pod efêmero (50m CPU / 64Mi mem)
- Um teste por vez por usuário (lock simples), evita empilhar pods de teste
- Toda execução logada no `HistoryTracker` (`action: "latency_test"`) — é uma ação que gera carga
  real no cluster alvo, vale trilha de auditoria como outras operações sensíveis

---

## Fase 1 — Backend: runner do teste (pod efêmero + exec + parsing)

**Arquivo:** `internal/web/handlers/latency_test.go` ← CRIAR

- [ ] `createTestPod(ctx, clientset, namespace) (podName string, cleanup func(), error)` —
  gera nome único (`latency-test-<random>`), labels (`app=latency-test-tool`,
  `created-by=k8s-hpa-manager`), `ActiveDeadlineSeconds: 300`, requests/limits pequenos; `cleanup()`
  deleta o pod (chamar sempre via `defer`)
- [ ] `waitPodRunning(ctx, clientset, namespace, podName, timeout)` — poll simples até `Status.Phase == Running`
- [ ] `runLatencyProbe(ctx, clientset, restConfig, namespace, podName, url string, count int, timeoutMs int) ([]float64, error)`
  — monta o script bash com `count` chamadas de `curl -o /dev/null -s -w "%{time_total}\n" --max-time <timeoutMs/1000>s <url>`,
  roda via `execCmdInPod` (reaproveitado, mover pra um helper compartilhado se for usado fora de
  `nodepools_conntrack.go` — avaliar extrair pra `internal/web/handlers/pod_exec_helpers.go` se
  outros arquivos também passarem a precisar), parseia stdout linha a linha (`strconv.ParseFloat`),
  linhas que não parseiam (timeout/erro do curl) contam como falha
- [ ] `computeLatencyStats(samples []float64) LatencyTestStats` — min/avg/median/p95/p99/max
  (mesma lógica de percentil já usada em algum lugar do FinOps/predictions — reaproveitar se
  existir função de percentil genérica, senão implementar `sort.Float64s` + índice)
- [ ] `LatencyTestResult{Samples []float64, Stats LatencyTestStats, ErrorCount int, TotalRequests int}`

---

## Fase 2 — Backend: endpoint SSE + rotas

**Arquivo:** `internal/web/handlers/latency_test.go` ← MODIFICAR (mesmo arquivo da Fase 1)
**Arquivo:** `internal/web/server.go` ← MODIFICAR

- [ ] `POST /api/v1/latency-test/run` — recebe `{cluster, namespace, url, requests, timeout_ms}`,
  valida teto de `requests` (ex: máx 200) e `timeout_ms` (ex: máx 10000), retorna `session_id`
  (mesmo padrão de outras operações SSE: endpoint de start retorna ID, cliente conecta no stream)
- [ ] `GET /api/v1/latency-test/stream/:sessionId` — SSE, eventos de progresso via
  `ProgressTracker`/`ProgressReporter` (criar pod → aguardando ready → executando → resultado final
  ou erro)
- [ ] `POST /api/v1/latency-test/cancel/:sessionId` — cancela e força cleanup do pod (usuário fecha
  o modal no meio do teste) — mesmo padrão de cancelamento já usado no Command Runner
  (`CommandRunnerHandler.Cancel`)
- [ ] Registrar rotas em `server.go`, atrás de `rbacMiddleware.RequireSREGroup()`
- [ ] `CreateHistoryEntry(c, "latency_test", url, cluster, status, nil, {namespace, requests, stats}, duration, errMsg)`
  no fim da execução (sucesso ou falha)

---

## Fase 3 — Frontend: nova ferramenta "Teste de Latência"

**Arquivo:** `internal/web/frontend/src/components/LatencyTestTab.tsx` ← CRIAR
**Arquivo:** `internal/web/frontend/src/components/ToolsMenu.tsx` ← MODIFICAR (`toolsTabs` array)
**Arquivo:** `internal/web/frontend/src/pages/Index.tsx` ← MODIFICAR (import + case em `renderTabContent`)

- [ ] Formulário: `ClusterSelectorForTab` (cluster) + seletor de namespace + seletor de Service
  (opcional, atalho — reaproveita lista já buscada pela aba Services) + campo de URL (editável,
  pré-preenchido se um Service foi escolhido) + nº de requisições (default 20, máx 200) + timeout
  por requisição (default 3000ms)
- [ ] Botão "Executar Teste" — desabilitado enquanto uma execução já está em andamento (lock de "um
  teste por vez" refletido na UI, não só no backend)
- [ ] Conexão SSE ao clicar — mesmo padrão de `EventSource`/hook já usado em Cordon/Drain ou Command
  Runner (`useCommandRunnerStream` ou equivalente — reaproveitar hook se existir um genérico de SSE)
- [ ] Progresso ao vivo: "Criando pod de teste...", "Aguardando pod ficar pronto...", "Executando
  requisição N de M..."
- [ ] Resultado final: cards com min/avg/mediana/p95/p99/max + contagem de erros; mini
  histograma/sparkline das amostras individuais (Recharts, reaproveitando padrão de chart já usado
  em outras abas)
- [ ] Botão "Cancelar" durante a execução → chama `POST /latency-test/cancel/:sessionId`
- [ ] Histórico de testes anteriores dessa sessão (lista simples na mesma tela, não precisa
  persistir em banco na v1 — já fica no `HistoryTracker` genérico se quiser consultar depois)

---

## Fase 4 — Segurança e limites (não pular)

- [ ] Teto hardcoded de requisições por teste no backend (não confiar só na validação do frontend)
- [ ] Lock "um teste por vez por usuário" (mapa em memória `map[userEmail]bool` protegido por mutex,
  ou reaproveitar algum semáforo já existente se o padrão se repetir em outro lugar)
- [ ] `ActiveDeadlineSeconds` no pod + `defer cleanup()` + tratamento de erro que **sempre** tenta
  deletar o pod mesmo se o teste falhar no meio
- [ ] Varredura periódica de pods órfãos (`app=latency-test-tool` + idade > X min) — proteção contra
  o caso de o processo do servidor morrer entre a criação do pod e o cleanup
- [ ] `RequireSREGroup()` no backend + `ProtectedAction` (sem `allowed` — é ação de infraestrutura,
  não RBAC K8s do analista) no frontend

---

## Fase 5 (complementar) — Contexto histórico via Prometheus/Dynatrace

Mantido do plano original — mostrado **ao lado** do resultado do teste ativo (Fases 1-4), não
substitui. Só implementar depois das Fases 1-4 estarem validadas.

**Descoberta da pesquisa anterior (ainda vale)**: já existe código de latência via Prometheus pronto
e não utilizado — `internal/monitoring/prometheus/client.go` tem `GetP95Latency`/`GetP99Latency`/
`GetRequestRate`/`GetErrorRate` (+ variantes `*History`) via `histogram_quantile`, só chamado por um
coletor confirmadamente morto (`internal/monitoring/monitor.legacy`, não importado por nada fora de
si mesmo). Do lado Dynatrace, `internal/dynatrace/metrics.go` já define os 4 percentis de latência
(`builtin:service.response.time:percentile(50/90/95/99)`) em `serviceMetricDefs`, hoje só buscados
no contexto de investigação de Problem (`GetEntityMetricsForProblem`).

- [ ] Portar `GetP95Latency`/`GetP99Latency`/`GetRequestRate`/`GetErrorRate` de
  `internal/monitoring/prometheus/client.go` pro client ativo (`internal/monitoring/client/prometheus_client.go`)
  — validar nome real da métrica Istio (`istio_request_duration_milliseconds_bucket`?) antes de
  assumir, cluster já roda Istio/Kiali
- [ ] `internal/dynatrace/latency_metrics.go` ← CRIAR — `GetWorkloadLatency(ctx, windowDays)`
  reaproveitando `serviceMetricDefs`; validar se `response_p50..p99` (entidades `SERVICE`) aceita
  `splitBy` direto por `k8s.workload.name` ou precisa de `ExtractK8sCorrelation` primeiro
- [ ] No resultado do teste ativo (Fase 3), buscar em paralelo (best-effort, não bloqueia o teste)
  o histórico P95/P99 do mesmo alvo via DT (primário) ou Prometheus (fallback) e exibir como
  badge/linha de comparação — mesmo padrão `MetricsSource` (`"dynatrace"`/`"prometheus"`/`""`) já
  validado no FinOps

---

## Fase 6 (opcional/depois) — Aba fleet-wide / SLO tracking

Só entrar nessa fase depois das Fases 1-5 validadas em produção — é a parte "descoberta de
problema" (visão agregada "pior P95 de toda a frota"), não "confirmação pontual" (que já é resolvida
pelo teste ativo das Fases 1-4).

- [ ] `internal/web/frontend/src/components/LatencyTab.tsx` ← CRIAR
- [ ] Entry em `ToolsMenu.tsx` (`toolsTabs` array, mesmo padrão de `dynatrace`/`finops`)
- [ ] Case em `Index.tsx` `renderTabContent()` (import + `ErrorBoundary` wrapper, mesmo padrão)
- [ ] Backend: `GET /api/v1/latency/fleet-scan?namespace=&threshold_ms=` — varre workloads
  (paralelo, semáforo, como `access_check_scan.go` faz para clusters) retornando os que ultrapassam
  o threshold de P95/P99 configurado, usando os dados históricos DT/Prometheus da Fase 5 (não dispara
  testes ativos em massa — isso seria abusivo, é leitura de série histórica)
- [ ] Atualizar `CLAUDE.md` (linha do `ToolsMenu`) e este arquivo com o resultado

---

## Fase 7 (opcional/futuro) — Correlação com Health Check

- [ ] Reaproveitar o padrão de `internal/healthcheck/correlator.go` (`workloadKey`, dois mapas,
  união, escalada de severidade): cruzar breach de latência (P95/P99 acima do threshold, via dados
  históricos da Fase 5) com DT Problems abertos no mesmo workload → severidade escalada quando os
  dois batem
- [ ] Nova categoria em `CorrelatedHealthItem` ou aba própria dentro do Health Check

---

## Status e próximos passos

⚠️ Nenhuma fase iniciada — este documento é só o plano.

Validar antes de codar a Fase 1:
- Confirmar que os clusters alvo permitem `pods/exec` pro service account do servidor (mesma
  permissão já usada por `execCmdInPod` no conntrack — se aquele feature funciona hoje, essa
  permissão já existe)
- Definir a imagem do pod de teste (`curlimages/curl` é pequena e teria acesso de pull garantido?
  confirmar se há um registry interno/mirror obrigatório por política da empresa antes de assumir
  Docker Hub público)
- Decidir o teto exato de requisições/timeout (proposta: 200 requisições, 10s de timeout por
  requisição, ambos configuráveis pelo usuário até esse teto)
