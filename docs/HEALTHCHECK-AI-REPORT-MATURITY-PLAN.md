# Plano: Maturidade do Relatório de IA do Health Check

← ✅ CONCLUÍDA — Fases 1-4 implementadas

## Problema

O usuário mostrou um exemplo de relatório de incidente gerado pela Davis AI do Dynatrace (cluster
EKS, período de 48h) e quer que os relatórios de IA do Health Check desta ferramenta cheguem no
mesmo nível. As características do exemplo que definem essa "maturidade":

1. Problemas numerados por severidade (🔴/🟠/🟡), cada um com: **citação verbatim** do evento K8s
   bruto, deployment/pods afetados, explicação em bullets do que o evento técnico significa, e um
   **link causal explícito** para o sintoma percebido pelo usuário final (ex: "isso causa 'reset de
   ambiente'").
2. Tabela de utilização por nó (pods ativos / capacidade máxima / % utilização) com uma inferência
   diagnóstica embaixo (ex: "baixa ocupação de pods mas CPU/memória já esgotados → requests mal
   configurados").
3. Distinção entre problema **agudo** (começou hoje, N eventos na última hora) e **crônico**
   (milhares de ocorrências, primeira ocorrência há meses, "não resolvido há quase 3 meses").
4. Problemas DAVIS ativos citados por ID (`P-26073420`) com dias em aberto.
5. Tabela-resumo final: Severidade | Problema | Aplicação Afetada | Status.
6. Ações recomendadas em 3 baldes de urgência: **Imediato (hoje)** / **Curto prazo (esta semana)** /
   **Monitoramento contínuo**.

## Diagnóstico do estado atual (levantado nesta rodada)

A maior parte dos ingredientes já existe — o gap está concentrado em wiring e no prompt, não em
coletar dado novo do zero. Acervo confirmado por leitura direta do código (não achismo):

- **Prompt raso**: `buildCorrelatedItemPrompt`/`buildBatchCorrelatedPrompt`
  (`internal/web/handlers/healthcheck.go:737` e `:846`) pedem só 4-5 bullets genéricos (causa raiz,
  impacto, ações, prevenção) — sem exigir citação verbatim, sem tabela de nós, sem os 3 baldes de
  urgência, sem estrutura por severidade.
- **Eventos já têm `Count`/`FirstTimestamp`/`LastTimestamp`** (`EventHealth`,
  `internal/healthcheck/event_checker.go:16`), mas `Correlate()` (`correlator.go:132`) descarta
  esses campos ao montar `CorrelatedK8sIssue` — só propaga `Message`/`Severity`. O struct
  `CorrelatedK8sIssue` (`models.go:222`) nem tem campos pra isso hoje.
- **`NodeChecker` está completamente órfão.** `internal/healthcheck/node_checker.go` tem
  `CheckAll()` (linha 74) totalmente implementado e testado (`node_checker_test.go`), calculando
  exatamente a tabela do exemplo (`PodUtilization`, `Capacity`, `Allocatable`, `Allocated` —
  `models.go`/`node_checker.go:15-45`). **Mas nada no `Orchestrator` o instancia ou chama** —
  `NewOrchestrator` (`orchestrator.go:41`) monta `deploymentChecker`/`serviceChecker`/`configChecker`/
  `eventChecker`/`hpaChecker`/`pvChecker`/`dynatraceChecker`/`oneAgentChecker`, mas não
  `nodeChecker`. `HealthCheckResult` (`models.go:156`) nem tem um campo `NodeResults`. Ou seja: a
  tabela de nós do exemplo não é "dado computado mas não exposto" — é uma feature pronta que nunca
  roda em produção.
- **Métricas Dynatrace de CPU/memória são buscadas e descartadas.** `dynatrace_checker.go`,
  `enrichWithContext` (linha ~297) chama `client.GetEntityMetricsForProblem` (retorna séries
  `node_cpu`/`node_pods` pra `KUBERNETES_NODE` e `cpu_milli`/`memory_mb` pra `CLOUD_APPLICATION`,
  conforme `internal/dynatrace/metrics.go:110`), mas o switch que monta `MetricsSummary`
  (`dynatrace_checker.go` linhas ~389-406) só extrai `error_rate`/`response_p90`/`response_p99`/
  `throughput` — os demais `series.Key` caem no default e são perdidos. Esse enriquecimento também
  só roda para problemas de severidade `AVAILABILITY`/`ERROR` (linha ~366: `if sev != "AVAILABILITY"
  && sev != "ERROR" { continue }`), pulando `PERFORMANCE`.
- **Não existe comparação request-vs-uso-real no Health Check.** `deployment_checker.go` coleta
  `ContainerResources` (requests/limits, `models.go:325`) mas só verifica presença/igualdade, nunca
  compara contra consumo real. A lógica pronta pra isso já existe em dois lugares — só não em Health
  Check:
  - `internal/finops/prometheus_enricher.go`: `queryContainerMetric` (linha 194) + `verdictFromPrometheus`
    (linha 316) — regra pronta: P95 ≥ 95% do request = `oom_risk`; excesso > 15% = `superprovisioned`.
  - `internal/monitoring/nodepoolpredictions/`: mesmo conceito em nível de nó.
- **Sem aba "Relatório" dedicada.** O resultado de `AnalyzeCorrelated`/`AnalyzeCorrelatedBatch` é
  consumido como texto solto em `HealthCheckResultsPanel.tsx` — não há o equivalente da
  `RelatorioTab` do FinOps (tabela-resumo por severidade, gráfico, achados priorizados).

## Decisão de escopo — como o histórico de longo prazo será construído

O K8s Events API por padrão só retém eventos por poucas horas (`--event-ttl` do apiserver); o
`Count`/`FirstTimestamp` de um `EventHealth` sozinho **não** reproduz "1.961 ocorrências desde
28/04" do exemplo — quando o objeto de evento expira e é recriado, o contador reseta a partir de 1.

**Decisão confirmada com o usuário**: persistir snapshots de eventos no SQLite já existente
(`HealthCheckStorage`, `internal/healthcheck/storage.go`) e agregar entre execuções históricas —
reaproveita a infraestrutura de storage já em produção, sem depender de nada externo. Fica preciso
a partir do momento em que a agregação começar a rodar (não retroage a histórico anterior à feature).

**Nuance de implementação a resolver na Fase 1** (documentada aqui, não resolvida ainda): o `Count`
de um `EventHealth` já é cumulativo *dentro do ciclo de vida do objeto de evento atual* no
apiserver. Somar `Count` ingenuamente a cada execução de Health Check gera overcounting (cada
execução recontaria o mesmo ciclo). A agregação correta precisa comparar o `Count` da execução atual
com o valor conhecido da execução anterior para o mesmo evento (mesma chave `namespace + involved_kind
+ involved_name + reason`): se `Count` atual < `Count` anterior conhecido, o objeto de evento foi
reciclado (novo ciclo) → soma o `Count` anterior ao total acumulado antes de substituir; se `Count`
atual ≥ anterior, é o mesmo ciclo → apenas atualiza o valor conhecido (não soma). `total_acumulado =
soma dos Count de cada ciclo fechado + Count do ciclo atual`. `first_seen_ever` é o mínimo de todos
os `FirstTimestamp` observados desde que a agregação começou a rodar.

## Fases

### Fase 1 — Wiring de dados já coletados + heurística crônico/agudo + prompt estruturado ✅
*(menor esforço, maior ganho imediato — não depende das Fases 2/3)*

**Status: implementado como desenhado**, com um ajuste na fórmula de acumulação descoberto pelos
testes (não estava especificado com essa precisão no design original): `cumulative_count`
representa "soma de todos os ciclos já FECHADOS + Count do ciclo atual (ainda vivo)". No mesmo
ciclo (`currentCount >= lastKnown`), substitui a contribuição antiga do ciclo atual pela nova
(`cumulative - lastKnown + currentCount`); num ciclo reciclado (`currentCount < lastKnown`), o
`lastKnown` do ciclo fechado já está contabilizado em `cumulative` — só soma a contribuição do novo
ciclo por cima (`cumulative + currentCount`). A primeira versão escrita (`cumulative + lastKnown`
no reset, sem tratar o caso "mesmo ciclo") dava resultado errado nos dois cenários — pego pelos
testes `TestEventChronicity_SameCycleUpdate`/`TestEventChronicity_CycleReset` antes de qualquer uso
real. Validado via `go test ./internal/healthcheck/... -race` (7 testes novos cobrindo primeira
observação, mesmo ciclo, reciclagem de ciclo, os dois limiares de crônico, caso agudo e isolamento
entre chaves distintas) + `go build ./...`.

**Validado ponta a ponta contra o cluster real `akspriv-envvias-hlg-admin`** (2 execuções de Health
Check reais, ~4min de intervalo, via `POST /api/v1/healthcheck/run`): a tabela
`health_check_event_history` populou corretamente a partir de eventos reais (`BackOff` num pod em
crashloop, `FailedGetResourceMetric` num HPA) e a acumulação entre as duas execuções bateu com o
esperado — `Count` do apiserver subiu (724→747 e 2102→2122, mesmo ciclo) e `cumulative_count`
acompanhou corretamente, com `first_seen_ever` mantendo a âncora original. Também confirmado: eventos
sintéticos do Kyverno (`PolicyViolation`, `Count=0`) não quebram a lógica (ficam estáveis em
`cumulative_count=0`).

**Não validado ponta a ponta**: a correlação com Dynatrace e, por consequência, a saída real dos
prompts reescritos — o ambiente de teste não tinha credenciais Dynatrace configuradas para o usuário
logado (`GET /api/v1/dynatrace/config` retornou `enabled:false`), então `CorrelatedItems` ficou
sempre vazio nesse cluster (sem problem DT, `Correlate()` não gera itens). Além disso, os únicos
eventos com `Count`/histórico real observados eram de `InvolvedKind` `Pod`/`HorizontalPodAutoscaler`
— fora do escopo de correlação atual (`Correlate()` só processa eventos `InvolvedKind == "Deployment"`,
limitação pré-existente, não introduzida nesta fase). Pra compensar, `buildCorrelatedItemPrompt`/
`buildBatchCorrelatedPrompt` foram exercitados em `internal/web/handlers/healthcheck_prompt_test.go`
com um `CorrelatedHealthItem` que combina a mensagem de evento REAL capturada do cluster (o
`BackOff`/crashloop, `Count=747`) com um problem Dynatrace sintético — a saída impressa confirma
visualmente a estrutura esperada (emoji 🔴, citação verbatim, marcação de crônico, DAVIS ID + dias em
aberto, métricas `cpu_milli`/`memory_mb` não mais descartadas, e os 3 baldes de ação). Validação
completa da correlação DT fica pendente de um cluster com Dynatrace configurado.

1. **Nova tabela `health_check_event_history`** em `storage.go` (chave: `cluster, namespace,
   involved_kind, involved_name, reason`), colunas `cumulative_count INTEGER`, `last_known_count
   INTEGER`, `first_seen_ever TIMESTAMP`, `last_seen TIMESTAMP`, `updated_at TIMESTAMP`. Nova função
   `UpsertEventHistory(ctx, cluster, namespace, event EventHealth) error`, chamada por `Save()` pra
   cada item de `EventResults`, aplicando a lógica de ciclo descrita acima.
2. Nova função `GetEventChronicity(ctx, cluster, namespace, involvedKind, involvedName, reason
   string) (*EventChronicity, error)` — retorna `{CumulativeCount int64, FirstSeenEver time.Time,
   IsChronic bool}`. Heurística de classificação (constantes, ajustáveis): `IsChronic = CumulativeCount
   >= 500 || time.Since(FirstSeenEver) >= 30*24h`.
3. `CorrelatedK8sIssue` (`models.go:222`) ganha campos `Count int32`, `FirstTimestamp time.Time`,
   `Chronicity *EventChronicity` (nil quando não é evento, ou quando storage indisponível — falha
   aberta, nunca bloqueia o Health Check por causa disso).
4. `addK8sIssue` em `correlator.go` (linha 132, ramo de eventos) passa a propagar `Count`/
   `FirstTimestamp` e consultar `GetEventChronicity`.
5. Corrigir `dynatrace_checker.go`: o switch de `MetricsSummary` passa a preservar `cpu_milli`/
   `memory_mb`/`node_cpu`/`node_pods` (chaves adicionais no `map[string]float64`, prefixadas pra não
   colidir, ex: `"metric_cpu_milli"`); remover o filtro `sev != "AVAILABILITY" && sev != "ERROR"` ou
   ampliá-lo pra incluir `PERFORMANCE`.
6. Reescrever `buildCorrelatedItemPrompt`/`buildBatchCorrelatedPrompt` pra exigir explicitamente do
   modelo: emoji de severidade por item, citação verbatim de `issue.Message`, frase de link causal
   pro sintoma do usuário, se `Chronicity.IsChronic` → citar "problema crônico, N ocorrências desde
   DATA", DAVIS problem ID + dias em aberto (`time.Since(p.StartTime)`), e ações finais divididas em
   `## Imediato (hoje)` / `## Curto prazo (esta semana)` / `## Monitoramento contínuo`.

### Fase 2 — Wiring do `NodeChecker` órfão ✅

**Status: implementado, com duas mudanças em relação ao design original e um bug real pego durante
a validação contra cluster real.**

- Além dos itens 1-4 do design original, foi necessário adicionar `CheckNodes bool`/`TimeoutNodes
  int`/`GetTimeoutNodes()` em `HealthCheckRequest` (não estava explícito no design) — sem uma flag
  de request, o `NodeChecker` nunca seria efetivamente chamado, já que todos os outros checkers são
  condicionados a uma flag equivalente.
- **Desvio consciente do "sem UI nova" do design original**: foi adicionado um checkbox "Capacidade
  dos Nós" em `HealthCheckingTab.tsx` (mesmo padrão dos outros 6 checkboxes de tipo de check). Sem
  isso, `check_nodes` nunca seria `true` a partir da UI real — o backend ficaria tecnicamente pronto
  mas inacessível pela aplicação, só testável via API direta. "Sem UI nova" no design original queria
  dizer "sem uma visualização dedicada da tabela de nós" (isso continua correto — não foi
  implementada), não "sem nenhum controle para habilitar o check".
- `buildCorrelatedItemPrompt`/`buildBatchCorrelatedPrompt` e os handlers `AnalyzeCorrelated`/
  `AnalyzeCorrelatedBatch` ganharam um parâmetro `nodes []healthcheck.NodeHealth` opcional — o
  frontend (`HealthCheckResultsPanel.tsx`) passa `result.node_results` ao chamar a análise AI.
- **Bug real encontrado e corrigido durante a validação contra cluster**: `GetHistory()` em
  `storage.go` não deserializava `NodeResults` do `extra_json` — só `Get()` (busca por ID único)
  tinha essa linha. A causa: a edição anterior (`replace_all`) buscou o texto
  `result.PVCResults = extra.PVCResults\n\tresult.DynatraceResults = ...` mas o bloco de `GetHistory()`
  está uma indentação mais profunda (dentro de um `for rows.Next()`), então o texto não bateu e só um
  dos dois lugares foi atualizado. Isso passou pelos testes unitários da Fase 1 porque nenhum teste
  cobria `Save()`+`GetHistory()` ponta a ponta — só foi percebido rodando de verdade contra
  `akspriv-ofertalogistica-hlg-admin` (`check_nodes=true` retornava `node_results: []` mesmo com os
  logs de progresso mostrando "6 nó(s) com avisos de capacidade" durante a execução). Corrigido, e
  adicionado `TestSaveAndGetHistory_RoundTripsAllExtraFields` (`storage_event_history_test.go`) que
  testa `Save()`+`Get()`+`GetHistory()` juntos especificamente para prevenir essa classe de bug
  (divergência entre os dois pontos de leitura do `extra_json`) se acontecer de novo com outro campo.

**Validado ponta a ponta contra `akspriv-ofertalogistica-hlg-admin`** (52 namespaces, cluster real,
`check_nodes=true`): 13 nós retornados, 6 com `status=warning` — CPU entre 90-100% enquanto a
ocupação de pods está em 15-45% do limite. **Esse é exatamente o padrão do relatório de exemplo que
motivou a Fase 2** ("baixa ocupação de pods mas CPU/memória já esgotados → requests mal
configurados"), confirmado com dado real, não sintético.

1. `Orchestrator` (`orchestrator.go:41`) ganha `nodeChecker *NodeChecker`, instanciado em
   `NewOrchestrator` (`NewNodeChecker()`).
2. `executeClusterCheck` (linha 245) ganha uma etapa `nodeResults := o.nodeChecker.CheckAll(ctx,
   client, req.GetTimeoutNodes())` — mesmo padrão de progress callback/SSE dos outros checkers
   (`deploymentCallback` etc., ver linha ~355 como referência direta).
3. `HealthCheckResult` ganha `NodeResults []NodeHealth`; `extraResultFields` (`storage.go:126`) ganha
   o mesmo campo pra persistir no `extra_json`; `calculateSummary` (`orchestrator.go:1041`) passa a
   contar `GetNodeCriticalCount`/`GetNodeWarningCount` (já existem, `node_checker.go:324`/`:335`, só
   não são chamados por ninguém hoje).
4. Novo helper `buildNodeUtilizationSection(nodes []healthcheck.NodeHealth) string` em
   `handlers/healthcheck.go`, chamado pelos dois prompt builders — monta a tabela markdown
   (nó/pods ativos/capacidade/% utilização) igual ao exemplo.
5. Frontend: `NodeHealth` exposto no tipo TS de `HealthCheckResult`; sem UI nova nesta fase (só
   alimenta o prompt) — ver Fase 4 pra exibição visual dedicada.

### Fase 3 — Comparação request vs. uso real via Prometheus ✅

**Status: implementado, com escopo maior que o design original** — a validação descobriu que
`DeploymentHealth` **já tinha** `CPUUsagePercent`/`MemoryUsagePercent` computados ao vivo via
metrics-server (`enrichWithMetrics`, `deployment_checker.go`), mas — igual ao `NodeChecker` na Fase
2 — completamente descartados (não influenciavam `Status`/`Message`, não estavam no tipo TS do
frontend, não chegavam no correlator nem no prompt). Perguntado ao usuário se a Fase 3 devia só
conectar esse dado já existente ou também adicionar o histórico via Prometheus — resposta: os dois.

**O que foi feito:**
- Novo `internal/healthcheck/resource_enricher.go` (`ResourceEnricher`, independente do pacote
  `finops` — reimplementa os mesmos limiares de `verdictFromPrometheus`, não importa o tipo
  `FinOpsWorkload`). Diferente do enricher do FinOps (que constrói um mapa pod→workload global pra
  cobrir o cluster inteiro numa passada só), o Health Check já lista os pods exatos de cada
  deployment via label selector (`deployment_checker.go`) — a query Prometheus usa esses nomes de
  pod diretamente (`pod=~"pod1|pod2|..."`), sem precisar de join com `kube_pod_labels` nem
  heurística de nome.
- `DeploymentChecker.Check`/`CheckAll` ganham um parâmetro `resourceEnricher *ResourceEnricher`
  (nilable, único call site em `orchestrator.go`) — chamado logo após `enrichWithMetrics`, dentro de
  `enrichWithResourceHistory`. Roda pra todos os deployments verificados quando o enricher está
  disponível (mesmo comportamento incondicional do `enrichWithMetrics` já existente — não gateado
  por status, pra manter consistência arquitetural com o vizinho).
- `deploymentResourceBaseline` extraído de dentro de `enrichWithMetrics` pra função compartilhada
  (soma request, com fallback pra limit, de todos os containers) — usado tanto pelo comparativo ao
  vivo quanto pelo histórico, evita duplicar a mesma regra.
- Nova flag `check_resource_history`/`CheckResourceHistory` + checkbox "Uso Real vs. Request
  (Prometheus)" no `HealthCheckingTab.tsx` — mesma lição da Fase 2: sem controle explícito na UI, o
  backend fica pronto mas inacessível. `ResourceEnricher` só é criado quando a flag está ligada E
  `discovery.IsEndpointAvailable(promURL)` confirma que o Prometheus do cluster responde (mesmo
  padrão de auto-descoberta do FinOps) — se não resolver, segue sem erro, só sem o dado extra.
- `correlator.go` propaga `ResourceVerdict`/`CPUUsagePercent`/`MemoryUsagePercent` pros issues de
  `ResourceKind == "Deployment"`; `resourceVerdictLine` nos prompt builders cita o veredicto só
  quando é `oom_risk` ou `superprovisioned` (nunca `"ok"` ou vazio — não vale a pena gerar ruído no
  relatório pra um veredicto neutro).
- Janela de histórico fixa em 48h (matching o "últimas 48h" do relatório de exemplo) — não
  configurável nesta rodada, mantém o escopo pequeno.

**Validado com dado real** contra `akspriv-ofertalogistica-hlg-admin` (namespaces
`oferta-estoque-1p-api-externas-hlg`/`oferta-estoque-1p-workers-hlg`/
`regionalizacao-estoqueregionalizado-api-externas-hlg`, Prometheus confirmado alcançável via probe
HTTP antes do teste): 17 de 18 deployments retornaram `resource_verdict="superprovisioned"` com CPU
P95 na casa de frações de millicore e memória real na faixa de dezenas/centenas de MB — consistente
com um ambiente HLG (staging) praticamente ocioso. Testes unitários cobrem `verdictFromP95` (tabela
com os mesmos 6 cenários do FinOps) e `deploymentResourceBaseline` (request vs. fallback pra limit,
múltiplos containers, sem resources configurados) sem depender de rede.

### Fase 3 (texto original do design, mantido como referência)

1. Novo `internal/healthcheck/prometheus_enricher.go`, adaptando o padrão de
   `internal/finops/prometheus_enricher.go` (`queryContainerMetric`/`verdictFromPrometheus`,
   mesmos thresholds: P95 ≥ 95% do request = `oom_risk`, excesso > 15% = `superprovisioned`) — reaproveita
   `ContainerResources` já coletado por `deployment_checker.go` em vez de recalcular requests.
2. Etapa opcional no orchestrator, condicionada a Prometheus configurado pro cluster (mesmo padrão
   de detecção usado no FinOps — se a URL não resolve, pula sem erro). Resultado anexado como
   `ResourceVerdict string` em `DeploymentHealth` (ou um novo campo correlacionado, a decidir na
   implementação conforme o que for menos invasivo no struct existente).
3. Prompt builders passam a citar o veredicto quando presente, habilitando a inferência "requests mal
   configurados" do exemplo (baixa ocupação de pods + CPU/memória esgotados → requests sem `limits`
   reais, ou request muito abaixo do uso real).

### Fase 4 — Aba "Relatório" dedicada ✅

**Status: implementado.** Novo componente `internal/web/frontend/src/components/HealthReportTab.tsx`
(padrão de arquivo separado já usado por `HealthCheckDTTab.tsx`), plugado como 10ª aba
("Relatório", `ListChecks`) em `HealthCheckResultsPanel.tsx` (`grid-cols-9` → `grid-cols-10`), na
última posição — mesma convenção do FinOps (`RelatorioTab` também é a última das 7 abas lá).

**Conteúdo**, em ordem:
1. Cabeçalho: cluster, período (`started_at`–`finished_at`), badges de contagem por severidade.
2. Tabela-resumo priorizada por severidade (Severidade | Recurso | Mensagem | Contexto), com badge
   "Crônico desde DATA" (issues de evento com `chronicity.is_chronic`) e badge de veredicto de
   recursos ("risco OOM/throttling" / "superprovisionado") quando presentes.
3. Tabela de utilização dos nós (só aparece quando `node_results` não está vazio — depende de
   `check_nodes` ter sido marcado na Fase 2), com a mesma dica de "baixa utilização de pods + CPU
   alta → requests mal configurados" do relatório de exemplo original.

**Fonte dos dados da tabela-resumo**: prioriza `result.correlated_items` (já vem com severidade
final calculada e crônico/agudo) quando não-vazio; sem Dynatrace configurado (`correlated_items`
sempre vazio, mesma limitação de todas as fases anteriores), cai num fallback client-side que monta
a lista a partir de `deployment_results`/`event_results`/`hpa_results`/`pvc_results` filtrando só
os não-healthy — não tem crônico/agudo nesse caminho (só vem de `CorrelatedK8sIssue`), mas tem o
veredicto de recursos (`resource_verdict` já vive direto em `DeploymentHealth`, Fase 3).

**Validado**: `tsc --noEmit` + `eslint` limpos; sanity-check manual (não visual) contra dois
resultados reais salvos no SQLite durante as validações das Fases 2/3 — conferido que os campos que
o componente lê (`node.allocated.pods`, `node.pod_utilization_percent`, `deployment.status`,
`deployment.resource_verdict` etc.) batem exatamente com o JSON real devolvido pela API (13 nós
reais, 48 deployments não-healthy num resultado com 180 deployments totais). **Não clicado num
browser de verdade** — sem ferramenta de automação de browser neste ambiente em nenhuma das 4
fases desta sessão.

## Fora de escopo

- Implementação em si — este documento só planeja; cada fase é aprovada e implementada
  separadamente, sem depender de fechar as 4 de uma vez.
- Retroagir a agregação de crônico/agudo a histórico anterior à Fase 1 — só conta a partir de quando
  `UpsertEventHistory` começar a rodar.
- Suporte a clusters sem Prometheus configurado pra Fase 3 — nesse caso o veredicto de uso real
  simplesmente não aparece no relatório (degrada graciosamente, sem erro pro usuário).
- Alterar o pipeline de coleta OneAgent/DAVIS Problems em si — Fase 1 só para de descartar dado que
  `GetEntityMetricsForProblem` já retorna, não muda como/quando ele é chamado além do filtro de
  severidade.

## Mapa de arquivos (previsto)

| Arquivo | O quê |
|---|---|
| `internal/healthcheck/storage.go` | Nova tabela `health_check_event_history`, `UpsertEventHistory`, `GetEventChronicity` |
| `internal/healthcheck/models.go` | `CorrelatedK8sIssue` ganha `Count`/`FirstTimestamp`/`Chronicity`; `HealthCheckResult` ganha `NodeResults` |
| `internal/healthcheck/correlator.go` | `addK8sIssue` propaga Count/FirstTimestamp/Chronicity pros eventos |
| `internal/healthcheck/dynatrace_checker.go` | Switch de `MetricsSummary` preserva CPU/memória; filtro de severidade ampliado |
| `internal/healthcheck/orchestrator.go` | `nodeChecker` instanciado e chamado em `executeClusterCheck`; `calculateSummary` conta nós críticos/warning |
| `internal/healthcheck/resource_enricher.go` (novo, Fase 3) | `ResourceEnricher`, `EnrichDeployment`, `verdictFromP95` — comparação P95 vs. request, independente do pacote `finops` |
| `internal/healthcheck/deployment_checker.go` | `deploymentResourceBaseline` extraído/compartilhado; `enrichWithResourceHistory`; `Check`/`CheckAll` ganham parâmetro `resourceEnricher` |
| `internal/web/handlers/healthcheck.go` | `buildCorrelatedItemPrompt`/`buildBatchCorrelatedPrompt` reescritos; novo `buildNodeUtilizationSection`; novo `resourceVerdictLine` |
| `internal/web/frontend/src/types/healthcheck.ts` | `NodeHealth`/`NodeResources`/`AffectedPod`/`EventChronicity` novos; `CorrelatedK8sIssue` ganha `count`/`first_timestamp`/`chronicity`/`resource_verdict`/`cpu_usage_percent`/`memory_usage_percent`; `DeploymentHealth` ganha `cpu_p95_millis`/`memory_p95_bytes`/`resource_verdict`; `HealthCheckRequest`/`HealthCheckResult` ganham `check_nodes`/`node_results`/`check_resource_history` |
| `internal/web/frontend/src/lib/api/client.ts` | `analyzeCorrelatedItem`/`analyzeCorrelatedBatch` ganham parâmetro `nodes` opcional |
| `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx` | `nodes` repassado de `result.node_results` até `CorrelatedTab`/`CorrelatedItemCard`/chamadas de análise AI. Sem view dedicada da tabela de nós ainda (fica pra Fase 4) |
| `internal/web/frontend/src/components/HealthCheckingTab.tsx` | Checkboxes "Capacidade dos Nós" (`check_nodes`) e "Uso Real vs. Request (Prometheus)" (`check_resource_history`) — necessários pra tornar as Fases 2/3 alcançáveis pela UI, não previstos no design original |

## Validação planejada

Fases 1-3 são backend-heavy: `go test ./internal/healthcheck/... -race` cobrindo a lógica de ciclo
do `UpsertEventHistory` (caso mais arriscado — testar reset de `Count` explicitamente com casos
tabulares) e `verdictFromPrometheus`/thresholds adaptados. Teste manual via `./build/new-k8s-hpa web
-f` contra um cluster real rodando Health Check correlacionado repetidas vezes pra observar a
agregação de crônico/agudo evoluir entre execuções. Fase 4 segue o mandato do CLAUDE.md de testar UI
no browser antes de reportar concluído.
