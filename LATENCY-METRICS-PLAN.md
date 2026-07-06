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

## Fase 4 — Segurança e limites (não pular) ✅ CONCLUÍDA

- [x] Teto hardcoded de requisições por teste no backend (`latencyTestMaxRequests = 200`,
  `latencyTestMaxTimeoutMs = 10000` em `Run` — já feito na Fase 2, o frontend não é a única
  barreira)
- [x] Lock "um teste por vez por usuário" — `LatencyTestHandler.runningUsers sync.Map`
  (`userEmail -> struct{}`), `LoadOrStore` em `Run` retorna `409 TEST_ALREADY_RUNNING` se já
  houver um teste rodando pro mesmo e-mail; liberado via `defer h.runningUsers.Delete(lockKey)`
  na goroutine que chama `runTest` (cobre sucesso, falha e cancelamento — todos passam por ali)
- [x] `ActiveDeadlineSeconds` + `defer cleanup()` — já garantido desde a Fase 1/2: `defer cleanup()`
  roda logo após `createTestPod` retornar com sucesso, antes de qualquer outra chamada bloqueante,
  então cobre todo erro subsequente (`waitPodRunning`, `runLatencyProbe`) automaticamente
- [x] Varredura periódica de pods órfãos — `LatencyTestHandler.seenClusters sync.Map` (populado em
  `Run`, só clusters onde este handler já rodou algo) + `sweepOrphanPods()` (goroutine iniciada em
  `NewLatencyTestHandler`, tick de 5min) + `sweepClusterOrphans()` (lista pods
  `app=latency-test-tool` cluster-wide, deleta os com mais de 10min — bem acima do
  `ActiveDeadlineSeconds` de 5min, então se ainda existe depois disso é porque o K8s não aplicou o
  deadline, cenário raro mas coberto)
- [x] `RequireSREGroup()` no backend (já feito na Fase 2) + `ProtectedAction` (sem `allowed`) no
  frontend envolvendo o botão Executar Teste/Cancelar — mesmo padrão exato do
  `CommandRunnerTab.tsx` (botão único que alterna entre as duas ações, ambas dentro do mesmo
  `<ProtectedAction>`)

`go build ./...`, `go vet ./...`, `gofmt -l` e `tsc --noEmit` limpos. `./rebuild-web.sh -b` ok.

---

## Fase 5 (complementar) — Contexto histórico via Prometheus/Dynatrace ✅ CONCLUÍDA E VALIDADA contra Prometheus/DT reais

Mostrado **ao lado** do resultado do teste ativo (Fases 1-4), não substitui.

**Desvio consciente do que estava escrito aqui antes**: o item original dizia "portar
`GetP95Latency`/... pro client ativo (`internal/monitoring/client/prometheus_client.go`)" —
isso fazia sentido quando a feature ainda seria parte do dashboard do Monitoring V2. Depois do
pivô pro teste ativo sob demanda, essa costura deixou de fazer sentido: o contexto histórico só
precisa ser consultado UMA VEZ, junto do teste, não integrado ao motor de monitoramento contínuo.
Em vez de portar o código, `internal/web/handlers/latency_history.go` (novo) importa
`internal/monitoring/prometheus` **diretamente como biblioteca** (o pacote já compila e já é usado
por `internal/monitoring/predictions`/`nodepoolpredictions` — não está morto, só não tinha nenhum
consumidor de latência ativo) e chama `NewClient`/`GetP95Latency`/`GetP99Latency` como estão, sem
copiar nada.

- [x] `internal/dynatrace/latency_metrics.go` ← CRIADO — `GetWorkloadLatency(ctx, windowDays)` e
  `GetSingleWorkloadLatency(ctx, namespace, workload, windowDays)`, reaproveitando
  `queryWorkloadBatch` (o mesmo helper privado que `GetAllWorkloadMetrics` do FinOps já usa) com
  `builtin:service.response.time` (percentile 95/99) em vez de CPU/mem; conversão µs→ms via
  `responseTimeUsToMs = 0.001` (mesmo fator de `serviceMetricDefs`)
- [x] `internal/web/handlers/latency_history.go` ← CRIADO — `fetchHistoricalLatencyContext`
  (DT primeiro, Prometheus como fallback, `discovery.GetPrometheusURL(cluster)` +
  `promclient.NewClient` do pacote citado acima) e `guessServiceNameFromURL` (heurística: primeiro
  label DNS do host da URL — ex: `http://minha-app.ns.svc.cluster.local:8080` → `minha-app`)
- [x] `runTest` (`latency_test_tool.go`) dispara a busca histórica em goroutine própria logo no
  início (contexto/timeout PRÓPRIOS de 15s, independentes do `ctx` cancelável do teste), e espera
  no máximo 2s por ela antes de montar o resultado final — nunca atrasa o teste ativo além disso.
  `LatencyTestResult.Historical LatencyHistoricalContext` (campo novo) carrega o resultado (ou
  `MetricsSource: ""` se nada respondeu a tempo/existir dado)
- [x] Frontend (`LatencyTestTab.tsx`) exibe uma linha "Contexto histórico" com badge DT (azul)
  ou Prom (âmbar) + P95/P99 históricos, só quando `metrics_source` não é vazio — mesmo padrão de
  cor já usado no FinOps

**✅ Validado contra Prometheus/DT reais de um cluster AKS (`akspriv-abastecimento-hlg-admin`),
testando o fluxo completo no navegador — 3 bugs reais encontrados e corrigidos:**

1. **Prometheus: métricas erradas.** `GetP95Latency`/`GetP99Latency` usavam
   `http_request_duration_seconds_bucket` e `nginx_ingress_controller_request_duration_seconds_bucket`
   — confirmado via `curl` direto no Prometheus real que **nenhuma das duas existe** (zero séries em
   todo o cluster; o cluster roda Istio, não NGINX puro). Corrigido: adicionada
   `istio_request_duration_milliseconds_bucket` como primeira tentativa, labels reais confirmados
   contra a API (`destination_service_name`/`destination_service_namespace`/`reporter="destination"`),
   mantendo nginx-ingress/genérica como fallback legado (`buildLatencyQueryCandidates` em
   `internal/monitoring/prometheus/client.go`). Também: janela do `rate()` de 5m → 30m — serviços de
   baixo tráfego (comum em HLG) têm buracos de vários minutos sem request, 5m caía com frequência
   numa janela vazia mesmo com o serviço tendo tráfego "seguido" (ex: ~1 req/min já é insuficiente
   pra garantir amostra em toda janela de 5min).
2. **Prometheus: "lazy connection" nunca era ativada.** O client em `client.go` exige
   `TestConnection(ctx)` explícito antes de `Query()`/`QueryRange()` aceitarem rodar
   (`connected=true`) — `fetchHistoricalLatencyContext` criava o client e chamava
   `GetP95Latency`/`GetP99Latency` direto, sem nunca chamar `TestConnection()` primeiro. Toda
   consulta falhava com `"prometheus client not connected"`, mesmo com o Prometheus 100% acessível.
3. **Zero silencioso tratado como sucesso.** Quando nenhuma query candidata achava valor válido
   (`histogram_quantile` sobre janela sem amostra recente devolve `NaN`, não erro), a função antiga
   devolvia `(0, nil)` — o zero virava "sucesso" pro chamador, e o `omitempty` no JSON de
   `LatencyHistoricalContext` escondia o campo, fazendo o frontend não mostrar nada sem indicar por
   quê. Corrigido: `queryLatencyPercentile` devolve erro explícito quando nenhuma candidata acha dado
   (`internal/monitoring/prometheus/client.go`), e `omitempty` removido de `P95Ms`/`P99Ms`
   (`latency_history.go`) — um zero real de latência agora é distinguível de "sem dado".

Também corrigido `guessServiceNameFromURL` pra aceitar host sem esquema (`"meu-host.com"` sem
`http://` na frente — `url.Parse` sem esquema trata a string toda como Path, não Host, e
`u.Hostname()` volta vazio).

**Dynatrace: suposição #1 do parágrafo original estava certa, e a correção FOI implementada** (não
ficou só como fallback seguro): confirmado direto contra `GET /api/v2/metrics/builtin:service.response.time`
que a única dimensão real dessa métrica é `dt.entity.service` (tipo `ENTITY`) — `SERVICE` nunca
carregou `k8s.namespace.name`/`k8s.workload.name`. A query antiga sempre falhava com erro 400
explícito da API (`"The dimension key k8s.namespace.name has been referenced, but the metric has
no such key"`). Reescrito `internal/dynatrace/latency_metrics.go`: `findServiceEntityID` busca a
entidade `SERVICE` por nome (`entityName.contains`, mesma heurística de `guessServiceNameFromURL`)
e `queryServicePercentile` consulta a métrica filtrada por `entitySelector=entityId(...)` (sem
`splitBy`, já que filtra por 1 entidade). `GetWorkloadLatency` (batch por `splitBy` k8s) removido —
sem uso depois da reescrita. Validado com teste manual direto contra a API real: entidade
`WEB_REQUEST_SERVICE` de aplicação encontrada por nome, latência real retornada
(P95=715ms/P99=1072ms).

**Achado à parte, não é bug**: os clusters/namespaces do domínio usado pra testar (`abastecimento-hlg`/
`abastecimento-prd`) não têm **nenhuma** cobertura desse tenant Dynatrace (zero entidades
`CLOUD_APPLICATION` neles) — o DT configurado neste projeto monitora outros domínios (onboarding,
categoria, etc.). Não dá pra validar o caminho DT ponta a ponta via navegador nesse domínio por
faltar workload monitorado, não por falha de código; nesses testes o contexto histórico
correntemente vem do Prometheus.

`go build ./...`, `go vet ./...`, `gofmt -l` limpos. Testado ponta a ponta no navegador (Playwright)
contra cluster AKS real: contexto histórico aparece com dado real do Prometheus
(`P95 91.7ms · P99 98.3ms`) condizente com o teste ativo (66.4ms/66.5ms) no mesmo alvo.

---

## Fase 6 (opcional/depois) — Multi-protocolo (ICMP/HTTP/HTTPS), alvos multi-cloud e topologia visual

Só entrar nessa fase depois das Fases 1-5 validadas em produção. Reescrita depois de discutir com o
usuário duas ideias novas: (1) suportar ping ICMP além de HTTP/HTTPS, com alvos multi-região tipo
`gcping.com` (GCP) e equivalentes de Azure/AWS; (2) desenhar o resultado como um grafo de topologia,
igual ao já existente na aba Service Mesh (Cytoscape.js, cor por severidade) — em vez da tabela
simples "pior P95 da frota" que estava planejada antes. A tabela por si só não respondia à pergunta
"qual é o caminho de rede e onde ele está lento", só "quais workloads estão lentos".

### 6.1 — Protocolo selecionável no probe (ICMP/HTTP/HTTPS) ✅ CONCLUÍDA (ICMP não validado)

**Arquivo:** `internal/web/handlers/latency_test_tool.go` ← MODIFICADO

- [x] Imagem do pod efêmero trocada de `curlimages/curl` para `nicolaka/netshoot:v0.12` (tag
  fixada de propósito, não `latest`) — **tag não confirmada contra o registry real** neste
  ambiente (sem acesso à internet), validar/atualizar antes de confiar em produção
- [x] `RunLatencyTestRequest.Protocol string` (`"http"` | `"https"` | `"icmp"`, default `"http"`),
  validado em `Run` (`latencyValidProtocols`, 400 se valor desconhecido)
- [x] `runLatencyProbe` virou dispatcher protocol-aware, dividido em `runHTTPProbe` (lógica
  antiga, sem mudança) e `runICMPProbe` (novo):
  - ICMP: `ping -c <count> -W <timeoutSec> -i 0.2 <host>; true` — **uma única invocação** (o
    `ping` já faz N pings sozinho); `-i 0.2` (200ms entre pacotes, o mínimo permitido sem
    privilégio) acelera o teste; `; true` força o shell a sair 0 mesmo com 100% de perda, senão
    `execCmdInPod` descartaria o stdout inteiro (mesmo motivo do `|| echo ERR` no probe HTTP)
  - Regex `time=([0-9.]+)\s*ms` extrai cada amostra da saída do ping
  - `normalizeICMPTarget()` — defesa em profundidade: se vier uma URL completa mesmo em modo
    ICMP, extrai só o host
- [x] **Risco real, documentado no código e não contornável de forma genérica**: pod sem
  privilégios não abre socket ICMP raw sem `CAP_NET_RAW`, e clusters com Pod Security
  `restricted` proíbem explicitamente qualquer capability adicionada. Decisão: **não pedir a
  capability** (evita quebrar a criação do pod em clusters restritos) — se o `ping` falhar por
  permissão, o stderr real (via `execCmdInPod`) aparece na mensagem de erro devolvida ao usuário,
  nunca cai silenciosamente pra outro protocolo
- [x] Frontend (`LatencyTestTab.tsx`): `Select` de Protocolo (HTTP/HTTPS/ICMP); campo de alvo troca
  label ("URL alvo" ↔ "Host alvo") e placeholder conforme o protocolo; troca de protocolo ajusta o
  valor já digitado (`handleProtocolChange` — soma/remove schema); atalho de Service respeita o
  protocolo atual ao pré-preencher; mensagens de progresso, resultado e eixo do gráfico usam
  "pacotes"/"pacote" em vez de "requisições"/"requisição" quando o protocolo é ICMP; nova coluna
  "Protocolo" na tabela de histórico da sessão

`go build ./...`, `go vet ./...`, `gofmt -l` e `tsc --noEmit` limpos. `./rebuild-web.sh -b` ok,
servidor sobe e responde 200.

**Não testado contra cluster real** (sem VPN/cluster neste ambiente) — dois pontos específicos
merecem atenção na primeira execução real: (1) se a tag `netshoot:v0.12` existe e faz pull sem
erro; (2) se o `ping` funciona sem `CAP_NET_RAW` no(s) cluster(s) da organização (depende do
sysctl `net.ipv4.ping_group_range` de cada nó — comum em GKE/EKS/AKS permitir por padrão, mas não
confirmado aqui).

### 6.2 — Alvos multi-cloud (GCP/AWS/Azure), estilo `gcping.com` ✅ CONCLUÍDA (só AWS populado)

**Arquivo:** `internal/web/handlers/latency_cloud_targets.go` ← CRIADO

**Decisão consciente ao implementar**: não populamos GCP nem Azure. Ao revisar a ideia original
("copiar o método do gcping.com"), ficou claro que o `gcping.com` mede contra **serviços de
demonstração que o próprio autor deployou** em cada região (Cloud Run/App Engine dele) — não são
endpoints públicos genéricos do GCP. Gerar tráfego de teste repetido contra infraestrutura de
terceiros que não é nossa (e sem necessidade real) não é apropriado, então não replicamos isso.
AWS é diferente: `s3.<região>.amazonaws.com` é convenção **oficial e documentada** da própria AWS
(todo endpoint regional de S3 responde nesse padrão, autenticado ou não) — não um palpite nem
dependência de terceiro, por isso só essa lista foi populada com confiança.

- [x] `CloudRegionTarget{Provider, Region, Label, Host, Protocol}` — lista curada e pequena, 5
  regiões AWS (sa-east-1 São Paulo primeiro, por relevância pra organização brasileira; depois
  us-east-1, us-west-2, eu-west-1, ap-southeast-1)
- [x] GCP e Azure **deliberadamente vazios**, com o motivo documentado em comentário no código
  (ver acima) — preencher exige decisão consciente depois (provisionar recurso "canário" próprio
  por região, ou usar um recurso de teste já existente da organização); não é algo pra resolver
  com um palpite de hostname
- [x] Não implementamos a derivação dinâmica "só regiões onde a organização tem cluster" via
  `loadClusterConfig()`/EKS/GKE — lista estática pequena já cobre o caso de uso por ora; fica como
  possível refinamento futuro, não bloqueante
- [x] `GET /api/v1/latency-test/cloud-targets` (`LatencyTestHandler.GetCloudTargets`) — sem
  `RequireSREGroup()` (é config estática, mesmo critério de outros endpoints read-only)
- [x] Frontend: `Select` "Alvo rápido: região de nuvem" ao lado do seletor de Service —
  mutuamente exclusivos (escolher um limpa o outro), pré-preenche `url` + `protocol` de acordo
  com o alvo escolhido

`go build ./...`, `go vet ./...`, `gofmt -l` e `tsc --noEmit` limpos. `./rebuild-web.sh -b` ok,
servidor sobe e responde 200; endpoint `/latency-test/cloud-targets` registrado e acessível
(não testado com auth válida neste ambiente — sem token JWT real pra exercitar via curl).

### 6.3 — Persistência leve dos resultados (necessária pro grafo ter o que mostrar) ✅ CONCLUÍDA

**Arquivo:** `internal/storage/latency_test_history_store.go` ← CRIADO
**Arquivo:** `internal/web/server.go` ← MODIFICADO (init do store + campo no `Server`)
**Arquivo:** `internal/web/handlers/latency_test_tool.go` ← MODIFICADO (`LatencyTestHandler.testHistory` + dual-write em `logHistory`)

- [x] SQLite (`~/.k8s-hpa-manager/latency_test_history.db`), mesmo padrão de
  `snat_history_store.go` (WAL mode, `_busy_timeout=5000`, `MaxOpenConns(3)`): tabela com
  `cluster, namespace, target, protocol, p95_ms, p99_ms, error_count, total_requests, tested_at,
  tested_by`
- [x] Retenção de 30 dias (não 90 como o SNAT — dado mais efêmero/diagnóstico). Diferença
  proposital do padrão SNAT: `Save` aqui NUNCA deduplica por janela de tempo — cada teste é uma
  ação explícita do usuário (botão "Executar Teste"), não um snapshot periódico automático, então
  todo teste concluído vira um registro
- [x] `LatencyTestHandler.logHistory` grava aqui TAMBÉM (dual-write, best-effort — erro de
  persistência nunca afeta a resposta já enviada ao usuário) — só quando `result != nil` (teste
  rodou até o fim; uma falha antes de gerar amostra nenhuma não tem P95/P99 pra registrar, o
  `HistoryTracker` já cobre isso como auditoria). Os dois writes (`HistoryTracker` e
  `LatencyTestHistoryStore`) agora são independentes um do outro — antes, `logHistory` inteiro
  retornava cedo se `historyTracker` fosse nil, o que teria pulado o novo store também
- [x] `GetRecent(limit int) ([]LatencyTestRecord, error)` — todos os clusters/alvos, mais
  recentes primeiro; usado pelo endpoint de topologia da 6.4

`go build ./...`, `go vet ./...`, `gofmt -l` limpos. `go test ./internal/storage/...` passa.
Testado de verdade nesta sessão (não só compilação): subi o servidor
(`./build/new-k8s-hpa web -f`) e confirmei `~/.k8s-hpa-manager/latency_test_history.db` sendo
criado com sucesso, log "✅ Latency Test History Store inicializado", sem erros.

### 6.4 — Grafo de topologia (reaproveitando o motor do Service Mesh) ✅ CONCLUÍDA

**Arquivo:** `internal/web/handlers/latency_topology.go` ← CRIADO
**Arquivo:** `internal/web/frontend/src/components/LatencyTopologyGraph.tsx` ← CRIADO
**Arquivo:** `internal/web/frontend/src/components/LatencyTestTab.tsx` ← MODIFICADO (toggle de aba)

- [x] `GET /api/v1/latency-test/topology` (`LatencyTestHandler.GetTopology`) — lê
  `testHistory.GetRecent(500)`, mantém só a PRIMEIRA ocorrência de cada par cluster→alvo (records
  já vêm mais-recente-primeiro, então a primeira ocorrência já É a mais recente), monta nós
  (cluster com provider via `kubeManager.DiscoverClusters()`; alvo com `targetNodeKind()` —
  `"cloud_target"` se bater com a lista curada da 6.2, senão `"service_target"` genérico) e
  arestas. `normalizeICMPTarget()` (já existente da 6.1) normaliza o alvo pra host puro — assim
  `http://x` e `https://x` do mesmo host viram o MESMO nó, não dois
- [x] Frontend (`LatencyTopologyGraph.tsx`) reaproveita a lib Cytoscape.js já usada em
  `ServiceMeshGraph.tsx` (mesmo padrão de `style` com função por elemento), mas como componente
  NOVO e enxuto — o de Service Mesh tem 2400+ linhas com filtros/badges/fullscreen que não fazem
  sentido pro nosso caso (poucos nós, sem necessidade de filtro)
  - Cor do nó: mesma família de cor dos badges AKS/EKS/GKE já usados em `useCloudProvider.ts`
    (azul=Azure/AKS, laranja=AWS/EKS, verde=GCP/GKE) — aplicada tanto a nós de cluster quanto a
    nós de alvo de nuvem, pra manter a linguagem visual consistente
  - Forma do nó: círculo (cluster, default), retângulo arredondado (`cloud_target`), losango
    (`service_target`)
  - Cor da aresta por P95: verde <100ms, amarelo 100-300ms, vermelho >300ms (thresholds como
    constantes fáceis de ajustar depois de ver dado real)
  - Clique na aresta abre painel com P95/P99/protocolo/erros/quando foi testado (mais simples que
    tooltip — reaproveita padrão de "seleção abre card" já visto em outras telas)
  - Sem teste ainda pra um par = sem aresta (backend já filtra isso, frontend só desenha o que
    vier)
- [x] Toggle "Teste"/"Topologia" dentro do `LatencyTestTab.tsx` — mesmo padrão de abas manuais
  (`div`+estado, nunca shadcn `<Tabs>`) já documentado no CLAUDE.md pra evitar o bug de
  `flex-1 min-h-0` quebrado

`go build ./...`, `go vet ./...`, `gofmt -l` e `tsc --noEmit` limpos. `./rebuild-web.sh -b`
(build de produção via Vite, mais rigoroso que `tsc` pra JSX malformado) passou — servidor sobe e
responde 200.

Com isso, **toda a Fase 6 está concluída**. Falta só a Fase 7 (opcional/futuro).

### Ordem de implementação sugerida

6.1 (protocolo) é independente e menor — dá pra fazer sozinha. 6.2 (alvos cloud) depende de 6.1
(sem protocolo ICMP, teste multi-cloud ainda funciona só em HTTP/HTTPS, então não é bloqueante,
mas fica mais completo junto). 6.3 e 6.4 são inseparáveis (o grafo não existe sem os dados
persistidos). Pode-se implementar 6.1+6.2 numa sessão e 6.3+6.4 em outra.

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
