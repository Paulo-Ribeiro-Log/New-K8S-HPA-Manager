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

## Fase 1 — Backend: runner do teste (pod efêmero + exec + parsing) ✅ CONCLUÍDA

**Arquivo:** `internal/web/handlers/latency_test_tool.go` ← CRIADO

> **Nota (bug real pego durante a implementação)**: o nome original planejado era
> `latency_test.go` — Go trata QUALQUER arquivo terminado em `_test.go` como arquivo de teste
> (excluído do build normal, só compilado via `go test`). `go build` não acusa erro nenhum, o
> arquivo simplesmente não entra no binário — silencioso e fácil de não perceber. Renomeado pra
> `latency_test_tool.go`. Confirmar com `go list -f '{{.GoFiles}}' ./pacote/` (deve aparecer ali,
> não em `{{.TestGoFiles}}`) sempre que um nome de arquivo novo tiver "test" no meio.

- [x] `createTestPod(ctx, clientset, namespace) (podName string, cleanup func(), error)` —
  gera nome único (`latency-test-<random>`), labels (`app=latency-test-tool`,
  `created-by=k8s-hpa-manager`), `ActiveDeadlineSeconds: 300`, requests/limits pequenos; `cleanup()`
  deleta o pod (chamar sempre via `defer`)
- [x] `waitPodRunning(ctx, clientset, namespace, podName, timeout)` — poll simples até `Status.Phase == Running`
- [x] `runLatencyProbe(ctx, clientset, restConfig, namespace, podName, url string, count, timeoutMs int) (samples []float64, errorCount int, err error)`
  — monta o script bash com `count` chamadas de `curl -o /dev/null -s -w "%{time_total}\n" --max-time <timeoutMs/1000>s <url>`
  (um único `sh -c`, não N execs), roda via `execCmdInPod` (reaproveitado de
  `nodepools_conntrack.go` sem mudanças — ainda não precisou extrair pra helper compartilhado,
  reavaliar se um 3º arquivo passar a precisar), parseia stdout linha a linha
  (`strconv.ParseFloat`), linhas que não parseiam (`ERR` do curl em timeout/erro de conexão) contam
  em `errorCount` e não entram em `samples`
- [x] `computeLatencyStats(samples []float64) LatencyTestStats` — min/avg/median/p95/p99/max via
  `latencyPercentile` (nearest-rank, generaliza o `percentile95` hardcoded de
  `internal/monitoring/nodepoolpredictions/cost_analyzer.go` pra qualquer p — aquele é privado ao
  pacote e não dava pra importar direto)
- [x] `LatencyTestResult{Samples []float64, Stats LatencyTestStats, ErrorCount int, TotalRequests int}`

`go build ./internal/web/handlers/...` limpo. Nada ainda chama essas funções (isso é a Fase 2) —
por design, é só o runner isolado.

---

## Fase 2 — Backend: endpoint SSE + rotas ✅ CONCLUÍDA

**Arquivo:** `internal/web/handlers/latency_test_tool.go` ← MODIFICADO (mesmo arquivo da Fase 1)
**Arquivo:** `internal/web/server.go` ← MODIFICADO
**Arquivo:** `internal/web/sse/progress.go` ← MODIFICADO (campo novo, ver nota abaixo)

- [x] `POST /api/v1/latency-test/run` (`LatencyTestHandler.Run`) — recebe
  `{cluster, namespace, url, requests, timeout_ms}`, aplica default (20 requisições / 3000ms) e
  teto (200 requisições / 10000ms) quando ausente ou acima do limite, retorna `session_id`
- [x] `GET /api/v1/latency-test/stream/:sessionId` (`LatencyTestHandler.Stream`) — SSE via
  `ProgressTracker` já existente (`handlers.GetProgressTracker()`, mesma instância global do
  Command Runner/Health Check), replay de eventos perdidos na reconexão, mesmo padrão exato de
  `CommandRunnerHandler.Stream`
- [x] `POST /api/v1/latency-test/cancel/:sessionId` (`LatencyTestHandler.Cancel`) — cancela o
  `context.Context` da execução; o cleanup do pod roda de qualquer forma porque `createTestPod`
  usa `context.Background()` próprio pro delete, não o ctx cancelado
- [x] Registrado em `server.go` atrás de `rbacMiddleware.RequireSREGroup()`; rota de stream usa
  `middleware.WebSocketJWTAuthMiddleware` direto no `s.router` (fora do grupo `api`) — mesma razão
  do Command Runner: `EventSource` do browser não manda header `Authorization`, token vem por
  query param
- [x] `LatencyTestHandler.logHistory` grava no `HistoryTracker` (`action: "latency_test"`) no fim
  da execução (sucesso ou falha) — construído direto como `history.HistoryEntry{}` (não via
  `CreateHistoryEntry(c, ...)`) porque o log acontece dentro da goroutine em background, depois
  que o handler HTTP já retornou e `*gin.Context` não está mais disponível; `UserInfo` é capturado
  ainda de forma síncrona em `Run` (via `GetUserInfoForHistory(c)`) e passado pra goroutine

**Nota**: `ProgressEvent` (`internal/web/sse/progress.go`) ganhou o campo
`Result interface{} \`json:"result,omitempty"\`` — o struct não tinha um jeito de carregar um
payload estruturado (só `Details string`), e o resultado final do teste
(`LatencyTestResult{Samples, Stats, ErrorCount, TotalRequests}`) precisa chegar inteiro no evento
`"complete"`. Campo genérico, não quebra nenhum uso existente (`omitempty`).

`go build ./...`, `go vet ./...` e `gofmt -l` limpos.

---

## Fase 3 — Frontend: nova ferramenta "Teste de Latência" ✅ CONCLUÍDA

**Arquivo:** `internal/web/frontend/src/components/LatencyTestTab.tsx` ← CRIADO
**Arquivo:** `internal/web/frontend/src/components/ToolsMenu.tsx` ← MODIFICADO (`toolsTabs` array, ícone `Gauge`)
**Arquivo:** `internal/web/frontend/src/pages/Index.tsx` ← MODIFICADO (import + case `"latency-test"`)
**Arquivo:** `internal/web/frontend/src/lib/api/types.ts` ← MODIFICADO (`RunLatencyTestRequest`/`Response`, `LatencyTestStats`, `LatencyTestResult`, `LatencyTestSSEEvent`)
**Arquivo:** `internal/web/frontend/src/lib/api/client.ts` ← MODIFICADO (`runLatencyTest`, `getLatencyTestStreamURL`, `cancelLatencyTest`)

- [x] Formulário: `ClusterSelectorForTab` + `Select` de namespace (`apiClient.getNamespaces`) +
  `Select` de Service opcional (`apiClient.getServices`, atalho que preenche a URL como
  `http://<service>.<namespace>.svc.cluster.local:<porta>` a partir do primeiro item de `ports`) +
  campo de URL editável + nº de requisições (1-200) + timeout (100-10000ms) — componente
  autocontido, sem props (mesmo padrão do `AccessCheckTab`, não usa o cluster global do `Index.tsx`)
- [x] Botão "Executar Teste" vira "Cancelar" (variant destructive) enquanto `isRunning` — troca de
  botão em vez de só desabilitar, deixa mais óbvio que dá pra cancelar
- [x] SSE via `EventSource` cru, mesmo padrão exato do `CommandRunnerTab.tsx` (não existe hook
  genérico de SSE no projeto — cada tab implementa a própria conexão inline)
- [x] Progresso ao vivo: barra de progresso (`div` com width % baseado em `event.progress`) +
  mensagem da fase atual (`event.message`, já vem pronta do backend: "Criando pod de
  teste...", "Aguardando pod ficar pronto...", "Executando N requisições...")
- [x] Resultado final: 6 `StatCard` (min/avg/mediana/p95/p99/max, p95/p99 com destaque visual) +
  contagem de sucesso/erro + `BarChart` do Recharts com a latência de cada requisição individual
- [x] Botão "Cancelar" chama `apiClient.cancelLatencyTest(sessionId)` e fecha o `EventSource` local
  (o pod é limpo no servidor de qualquer forma, não depende dessa chamada ter sucesso)
- [x] Histórico da sessão: array em estado local (`useState`, não persiste — reinicia ao dar F5),
  tabela simples com cluster/namespace/URL/P95/P99/erros de cada teste anterior

`npx tsc --noEmit` (0 erros) e `./rebuild-web.sh -b` (build de produção) limpos. `npm run lint`
não roda neste ambiente — binário do eslint em `node_modules/.bin/eslint` está zerado/sem permissão
de execução (problema pré-existente do ambiente, não desta feature); typecheck é o gate que importa
aqui e passou limpo.

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
