# Plano: Modo Triagem no Health Check (sinaliza primeiro, investiga depois)

**Status:** 🟢 Fases 1, 2 e 4 implementadas e **validadas ao vivo (2026-08-20) contra um cluster
real** (`akspriv-abastecimento-hlg`, via API direta/curl com JWT real — ver "Validação ao vivo" ao
final da Fase 1) — Modo Triagem funciona ponta a ponta de verdade: escopo resolvido via
Dynatrace/Prometheus reais, deployment/HPA checkers de fato confinados a esse escopo, e
supressão de ruído (Fase 4) confirmada reduzindo o escopo na prática. Um bug real foi encontrado e
corrigido nessa validação (`triage_summary` não sobrevivia ao Save/Get — commit `1d9d25df`). Só a
UI React em si não foi clicada num navegador ainda (risco residual baixo — ver nota "O que ainda
não foi clicado"). **Fase 3 (Elasticsearch) implementada em 2026-08-20** — convenção de campo
assumida por decisão explícita do usuário ("assumir e ajustar depois"), validada só via
`httptest` (5 testes) e build/type-check — **não confirmada contra um índice ELK real** (sessão
Azure AD expirou no meio da implementação, sem JWT novo pro resto da sessão — ver "Validação até
agora" da Fase 3 pro próximo passo exato). Fases 5 (Zabbix) e 6 (granularidade por workload) ainda
não iniciadas. Fases 3/4 registradas em 2026-08-20 a partir da revisão de um script Python de
referência de outra ferramenta interna — ver seção 0.
**Origem:** conversa sobre `ZABBIX-INTEGRATION-PLAN.md` levou o usuário a propor repensar a
arquitetura do Health Check: hoje ele varre o cluster ponto a ponto e devolve muita informação
de erro/postura; a proposta é buscar primeiro em ferramentas de monitoramento (Dynatrace/
Grafana/Zabbix) por problemas já sinalizados, e só then investigar a fundo os
clusters/namespaces apontados por elas — uma busca mais "de fora pra dentro" em vez de varredura
cega.
Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` (seção "Health Checking") +
`ZABBIX-INTEGRATION-PLAN.md` (a integração Zabbix é uma fonte deste plano, não pré-requisito).

---

## 0. Decisões já confirmadas com o usuário (não reabrir sem motivo)

1. **Não substituir o Health Check atual — manter os dois modos.** Varredura Completa (hoje)
   continua existindo: boa parte do valor do Health Check é achar problema de **postura/higiene**
   (sem probe, QoS errado, sem PDB, ConfigMap órfão, capacidade de nó apertada) que nenhuma
   ferramenta de monitoramento algum dia vai alertar sozinha — elas alertam sintoma (latência,
   erro, CPU/mem, host down), não manifesto malformado. Se o Health Check virasse 100% "só
   investiga o que já foi sinalizado", perderia exatamente esse tipo de achado proativo.
2. **"Grafana" não é uma integração nova** — confirmado com o usuário: os alertas vistos no
   Grafana desta empresa são os mesmos alertas nativos do Prometheus, já com cliente Go pronto
   nesta aplicação (`internal/monitoring/alerts`, ver seção 1.3) — nunca ligado ao Health Check,
   só usado hoje na aba de Alertas/Dashboard. Nenhuma API do Grafana em si precisa ser integrada.
3. **Zabbix não bloqueia.** As fontes já integradas (Dynatrace + Prometheus) são suficientes pra
   implementar o Modo Triagem inteiro hoje. Zabbix entra depois, como mais uma fonte plugável,
   quando `ZABBIX-INTEGRATION-PLAN.md` tiver pelo menos a Fase 1 pronta — sem precisar reabrir a
   arquitetura deste plano (ver seção 3).

**Origem dos itens de 2026-08-20 (Fases 3/4 abaixo)**: usuário compartilhou um script Python de
referência de outra ferramenta interna (SRE multi-squad, não deste repositório) que já resolve
correlação multi-fonte de forma parecida — revisão comparativa registrada aqui, sem portar nada
literalmente (arquitetura/stack diferentes). Dessa revisão, 2 ideias genuinamente novas (não
cobertas por nada existente nesta app, confirmado lendo o código) entraram no backlog deste plano:
Elasticsearch/ELK como fonte de erro de log (Fase 3) e ignore-lists configuráveis por nome de
sinal externo (Fase 4). Um 3º item (painel "farol" por fonte) não é uma fase própria — é só a
seção 2.4 (UI da Fase 2) ficando mais específica sobre o que já é possível hoje com
`TriageSummary.Sources`, que a Fase 1 já produz.

---

## 1. Como o Health Check funciona hoje (baseline verificado no código)

Lido diretamente de `internal/healthcheck/orchestrator.go` e dos checkers em
`internal/healthcheck/*_checker.go` — não é descrição de memória.

### 1.1. Namespaces são resolvidos uma vez, checkers rodam sobre a lista inteira

```go
// orchestrator.go — RunForCluster (resumido)
namespaces := req.Namespaces
if len(namespaces) == 0 {
    namespaces, err = getAllNamespaces(ctx, client) // TODOS os namespaces do cluster
}
// ... resourceEnricher, spinnakerEnricher resolvidos uma vez por cluster ...
if req.CheckDeployments {
    go func() {
        deploymentResults := o.deploymentChecker.CheckAll(ctx, client, metricsClient,
            namespaces, timeout, cluster, registry, resourceEnricher, spinnakerEnricher, callback)
    }()
}
// mesmo padrão para Services, Configs, Events, HPAs, PVCs, Nodes — cada um recebe a MESMA
// lista `namespaces` e varre tudo dentro dela, sem nenhum filtro de "só o que importa"
```

**Achado-chave**: `namespaces []string` já é a interface de escopo de **todo** checker
(`CheckAll(ctx, client, ..., namespaces, ...)`). Isso é o ponto de alavancagem pro Modo Triagem —
não é preciso mudar a assinatura de nenhum checker pra restringir o que é varrido, só é preciso
calcular uma lista de namespaces menor antes de chamá-los (ver seção 2.1).

### 1.2. Dynatrace/Spinnaker hoje só enriquecem depois, nunca reduzem escopo

`CheckDynatrace` roda **em paralelo** com os demais checks (não antes), e seu resultado só é
cruzado depois via `Correlate()` (`internal/healthcheck/correlator.go`) — mapeia K8s↔DT por
workload e escala severidade quando os dois lados confirmam problema, mas a varredura K8s já
tinha acontecido inteira antes disso. `SpinnakerEnricher` segue o mesmo princípio (enriquece
`DeploymentHealth`, nunca decide o que é varrido). **Nenhuma fonte externa hoje influencia o que
é buscado no cluster** — só o que é exibido/priorizado depois.

### 1.3. Fontes de sinal externo já integradas nesta aplicação

| Fonte | Cliente Go já existente | Uso hoje no Health Check |
|---|---|---|
| **Dynatrace** (problems OPEN) | `internal/dynatrace` + `DynatraceChecker.CheckAll` (`internal/healthcheck/dynatrace_checker.go`) | Sim — opt-in (`CheckDynatrace`), roda em paralelo, correlaciona depois |
| **Prometheus/Alertmanager** (alertas firing) | `internal/monitoring/alerts.Client.GetAlerts()` (`internal/monitoring/alerts/client.go`) | **Não** — só usado na aba de Alertas/Dashboard, nunca chamado pelo orchestrator do Health Check |
| **Zabbix** (problems) | Não existe ainda — ver `ZABBIX-INTEGRATION-PLAN.md` Fase 1 | N/A |
| **Elasticsearch/ELK** (volume de erro de log por app) | Não existe **nenhum** client Elasticsearch/Kibana nesta app hoje (confirmado por busca no código, 2026-08-20) — ver Fase 3 | N/A |

O motivo de ELK ter valor próprio, não redundante com Dynatrace: `/api/v2/logs/search` do
Dynatrace está documentado como bloqueado pra praticamente todo mundo (exige Bearer JWT via
OAuth2 client credentials — Settings → OAuth clients, escopo `storage:spans:read`/logs
equivalente —, credencial que ninguém no time tem permissão de criar; ver seção "Dynatrace" do
`CLAUDE.md`, `internal/dynatrace/logs.go`). Um Elasticsearch/Kibana próprio (Basic Auth, sem essa
exigência de Grail/Platform) contorna esse bloqueio por completo — **confirmado em 2026-08-20**:
esta empresa opera ELK de verdade, ver seção 4 item 5 (respostas).

`DynatraceHealth` (`internal/healthcheck/models.go:198`) já vem com `K8sNamespaces []string` e
`K8sWorkloads []string` (formato `"namespace/workload"`) por problem — dado pronto pra virar
alvo de triagem sem nenhum parsing novo.

`alerts.Alert` (`internal/monitoring/alerts/types.go`) expõe só `Labels map[string]string` cru —
**não há garantia de que toda regra de alerta tenha o label `namespace`** (depende de como cada
regra foi escrita no Prometheus desta empresa). `alerts.Client.GetHPAAlerts()`/`GetNodePoolAlerts()`
já fazem parte desse trabalho de extração pra alertas de HPA/Node especificamente — reaproveitável
como referência, mas o caso geral (qualquer alerta, não só HPA/Node) precisa de extração best-effort
própria (ver seção 2.2).

---

## 2. Desenho do Modo Triagem

### 2.1. Granularidade: por namespace na Fase 1, não por workload

Decisão deliberada pra minimizar risco/esforço: a Fase 1 resolve **quais namespaces têm algum
problema sinalizado** (não "quais workloads exatos"), e passa essa lista reduzida pros checkers
exatamente como `req.Namespaces` já funciona hoje — **zero mudança de assinatura em qualquer
`*_checker.go`**. Dentro de um namespace flagado, os checkers continuam varrendo tudo (não só o
workload exato que gerou o alerta) — dois motivos:

1. Um problema na mesma "vizinhança" (mesmo namespace) costuma ter relação (ex: HPA mal
   configurado num Deployment que causou o alerta de latência em outro).
2. Evita depender 100% de correlação workload-a-workload por nome, que já é heurística frágil
   (mesmo problema que o `extractNodePoolFromName`/`newWorkloadKey` do Dynatrace hoje têm) — usar
   granularidade de namespace tolera erro de matching sem deixar de checar nada relevante.

Granularidade por workload fica como possível incremento futuro (Fase 6, condicional — ver
seção 3), só se o uso real mostrar que "namespace inteiro" ainda traz ruído demais.

### 2.2. Fontes plugáveis — interface comum

```go
// internal/healthcheck/target_resolver.go (novo)

// TargetSource resolve, uma vez por cluster, quais namespaces têm algo sinalizado por uma
// fonte externa. Nunca é fatal — Available=false sinaliza "sem dado desta fonte" (ver 2.3),
// diferente de Namespaces vazio, que significa "fonte checou e não achou nada".
type TargetSource interface {
    Name() string
    Resolve(ctx context.Context, cluster string) TargetSourceResult
}

type TargetSourceResult struct {
    Available  bool     // false = fonte indisponível/erro/não configurada para este cluster
    Namespaces []string // namespaces com algum problema sinalizado por esta fonte
    Reasons    map[string][]string // namespace → lista de motivos (ex: "Dynatrace: problem X (ERROR)")
}
```

- **`DynatraceTargetSource`**: reaproveita `DynatraceChecker.CheckAll` (já existe, zero mudança)
  — roda a checagem de problems normalmente e extrai `K8sNamespaces` de cada `DynatraceHealth`
  retornado. `Available=false` quando `DynatraceURL`/token não configurados ou a chamada falha.
- **`PrometheusAlertsTargetSource`** (novo): usa `alerts.Client.GetAlerts()` já existente,
  filtra por `State == "firing"`, extrai `Labels["namespace"]` quando presente. Alertas sem esse
  label são ignorados pra fins de triagem (não quebram, só não contribuem um namespace) — mesma
  filosofia de "nunca inventa sinal" já usada no Dynatrace (`extractEnvHint`) e no Access Checker.
  `Available=false` quando o Prometheus do cluster não é alcançável (mesmo helper
  `discovery.IsEndpointAvailable` já usado pelo `ResourceEnricher`).
- **`ElasticsearchTargetSource`** (Fase 3, condicional — ver seção 4 item 5): consulta um índice
  de logs por janela curta (ex: `now-15m`), filtra por nível `error`/`fatal`, agrega por
  `kubernetes.namespace_name` (campo padrão quando o pipeline de ingestão é Filebeat/Fluentd com
  enriquecimento K8s — mesma convenção observada no script Python de referência). `Available=false`
  quando não configurado ou a query falhar.
- **`ZabbixTargetSource`** (Fase 5, condicional): ver seção 3.

União de `Namespaces` de todas as fontes `Available=true` = escopo de triagem. Antes dessa união,
cada fonte deve aplicar a lista de supressão da Fase 4 (seção 2.5) — um alerta/trigger/problem
"conhecido e aceito" não deve contribuir namespace nenhum, senão o Modo Triagem herda o mesmo
ruído que ele existe pra evitar.

### 2.3. Fluxo revisado no orchestrator

```go
// orchestrator.go — RunForCluster, seção inicial (revisada)
namespaces := req.Namespaces
if req.TriageMode {
    sources := []TargetSource{dtSource, promSource /*, zabbixSource quando existir */}
    resolved, anyAvailable := resolveTriageTargets(ctx, cluster, sources)
    switch {
    case anyAvailable && len(resolved) > 0:
        // Interseção com req.Namespaces se o usuário também filtrou manualmente,
        // senão usa o conjunto resolvido inteiro
        namespaces = intersectOrUse(req.Namespaces, resolved)
    case anyAvailable && len(resolved) == 0:
        // Toda fonte disponível respondeu "sem problema" — cluster saudável, relatório
        // rápido, SEM cair pra varredura completa (isso É o resultado esperado da triagem)
        namespaces = []string{}
    default:
        // NENHUMA fonte disponível (todas Available=false) — sem dado externo pra confiar,
        // cai pra Varredura Completa neste cluster (nunca "sem checar nada" por omissão)
        namespaces = req.Namespaces // comportamento de hoje (vazio = getAllNamespaces)
        result.TriageFallbackReason = "nenhuma fonte de triagem disponível para este cluster"
    }
}
if len(namespaces) == 0 && !req.TriageMode {
    namespaces, err = getAllNamespaces(ctx, client) // comportamento atual, inalterado
}
```

**Ponto que exige cuidado real** (a parte mais fácil de errar aqui): distinguir "fonte disponível
e sem problema" (bom sinal — cluster saudável, relatório rápido) de "fonte indisponível/erro"
(ausência de dado, não deve ser lido como saúde). É por isso que `TargetSourceResult.Available`
é um campo explícito separado de `Namespaces` vazio, não um valor sentinela dentro da lista.

### 2.4. UI / transparência do resultado

- `HealthCheckRequest` ganha `TriageMode bool` (`json:"triage_mode"`) — checkbox/toggle antes de
  iniciar o check, mesmo padrão visual dos demais `Check*` já existentes.
- `HealthCheckResult` ganha um resumo de triagem (`TriageSummary`): quais namespaces entraram no
  escopo e por qual fonte (`Reasons` de `TargetSourceResult`), quais namespaces existem no
  cluster mas foram pulados, e o motivo de fallback quando aplicável — sem isso, um resultado
  "vazio" da triagem é indistinguível de "não rodou direito", mesmo cuidado que a Fase 3 do
  `NOTES-PLAN.md`/Access Checker já tiveram com `isError` vs. lista vazia.
- Exibição: seção nova em `HealthReportTab.tsx` (ou painel próprio) — "N de M namespaces
  verificados (triagem: Dynatrace 3, Prometheus 2, sobreposição 1)".
- **Painel "farol" por fonte** (item do backlog de 2026-08-20, inspirado no
  `classify_source_status()` do script Python de referência — ver seção 0): um card por fonte
  (Dynatrace 🟢/Zabbix 🟡/Prometheus 🔴/Elasticsearch...) mostrando `TriageSourceStatus.Available`
  + o motivo resumido. **Não é dado novo** — `TriageSummary.Sources` já é exatamente esse array
  desde a Fase 1 (`internal/healthcheck/models.go`); é só uma decisão de desenho de UI da Fase 2,
  não uma fase própria. Cor do card: cinza quando `Available=false` (fonte fora do jogo, não
  "problema"), verde quando `Available=true` e `Namespaces` vazio (checou e achou tudo limpo),
  âmbar/vermelho proporcional à quantidade de namespaces sinalizados por aquela fonte.

### 2.5. Supressão de ruído — ignore-lists por nome de sinal externo (Fase 4)

**Gap real, confirmado lendo o código**: `FilterManager` (`internal/healthcheck/filters.go`) já
existe e já tem UI própria (`FiltersManagementModal.tsx` + `GET/POST/DELETE /api/v1/filters`), mas
seu modelo (`FilterRule{Type, ResourceType, Namespace, Name, Category}`) é inteiramente sobre
**postura de recursos K8s** (ConfigMap vazio, Secret de sistema, Deployment sem probe em
`kube-system`) — `ResourceType` só aceita `Deployment`/`Service`/`ConfigMap`/`Secret` (ver
`models.go`). Não existe hoje nenhum jeito de dizer "ignore o alerta Prometheus `Watchdog`" ou
"ignore esse problem Dynatrace específico, já sabemos, não é acionável" — o script Python de
referência (seção 0) resolve isso com listas por nome (`ignore.alertNames`, `.zabbixTriggers`,
`.dynatraceProblems`) carregadas do config da squad.

**Por que isso importa especificamente pro Modo Triagem** (não é só "mais uma feature de
filtro"): sem supressão, um alerta ruidoso-mas-aceito (ex: `Watchdog`, sempre firing por design;
ou um problem Dynatrace crônico já em acompanhamento formal fora desta ferramenta) força um
namespace inteiro pra dentro do escopo triado toda vez — na prática, esvazia o ganho da triagem
pra esse namespace, que volta a ser varrido por completo sempre. Diferente do `FilterManager`
atual, que suprime *achados* depois de encontrados, aqui a supressão precisa acontecer *antes*, na
hora de decidir escopo — dentro de cada `TargetSource.Resolve()`, não depois.

**Desenho proposto** — mecanismo próprio, paralelo ao `FilterManager` (não uma extensão dele: o
modelo de dados é estruturalmente diferente — nome de alerta/trigger/problem não é um
`ResourceType` K8s):

```go
// internal/healthcheck/triage_ignore.go (novo)

// TriageIgnoreConfig lista nomes de sinal externo (não recursos K8s) a ignorar na resolução de
// escopo do Modo Triagem — carregado/persistido em ~/.k8s-hpa-manager/triage-ignore.json, mesmo
// padrão de storage local em arquivo do FilterManager (filters.go).
type TriageIgnoreConfig struct {
    Version           string   `json:"version"`
    PrometheusAlerts  []string `json:"prometheus_alerts"`  // alertname, ex: "Watchdog"
    DynatraceProblems []string `json:"dynatrace_problems"` // título ou displayId
    ZabbixTriggers    []string `json:"zabbix_triggers"`    // nome do trigger (Fase 5)
    ElasticsearchPatterns []string `json:"elasticsearch_patterns"` // Fase 3
}
```

- Cada `TargetSource` concreto recebe o `TriageIgnoreConfig` (ou só a fatia relevante) no
  construtor — mesmo padrão de injeção usado hoje pra credenciais (`NewDynatraceTargetSource`) —
  e filtra ANTES de popular `Namespaces`/`Reasons`, nunca depois: um problem/alerta ignorado não
  deve nem aparecer em `Reasons`, senão o motivo mostrado na UI fica confuso ("por que este
  namespace está no escopo, se o único motivo listado está marcado como ignorado?").
- **Escopo da lista: global, não por cluster** — mesma decisão já tomada pelo `FilterManager`
  (`FilterRule` não tem campo cluster). Consistente, e mais simples: alertname/trigger/problem
  title costuma ser o mesmo em todo o ambiente de determinada empresa (não varia por cluster).
- **UI**: nova aba dentro do `FiltersManagementModal.tsx` existente (ou um modal irmão) — reutiliza
  o padrão visual (lista + adicionar + remover), evita inventar um componente do zero.
- **Endpoints**: `GET/POST/DELETE /api/v1/triage-ignore` — mesmo padrão de `internal/web/handlers/filters.go`.

---

## 3. Fases

### Fase 1 — `TargetResolver` + fontes Dynatrace/Prometheus (sem Zabbix) — ✅ implementada

**Arquivos criados:**
- `internal/healthcheck/target_resolver.go` — interface `TargetSource`, `TargetSourceResult`,
  `TriageResolution`, `resolveTriageTargets` (agrega as fontes, sequencial — custo de rede único
  por cluster por fonte, mesmo padrão do `ResourceEnricher`/`SpinnakerEnricher`), `intersectOrUse`
  (aplica o filtro manual de `req.Namespaces` por cima do escopo resolvido, quando informado)
- `internal/healthcheck/target_source_dynatrace.go` — `DynatraceTargetSource`, reaproveita
  `DynatraceChecker.CheckAll` sem duplicar lógica de correlação/enriquecimento
- `internal/healthcheck/target_source_prometheus.go` — `PrometheusAlertsTargetSource`, reaproveita
  `alerts.Client.GetAlerts()` filtrando `State=="firing"` + `Labels["namespace"]`
- `internal/healthcheck/target_resolver_test.go` — cobre explicitamente o risco #2 da seção 4
  (fonte disponível-mas-vazia vs. indisponível), união/dedup de namespaces entre fontes, e
  `intersectOrUse`

**Arquivos modificados:**
- `internal/healthcheck/models.go` — `HealthCheckRequest.TriageMode`, `HealthCheckResult.TriageSummary`,
  `TriageSummary`/`TriageSourceStatus` (novos tipos)
- `internal/healthcheck/orchestrator.go` — `buildTriageSources()` (novo helper) + fluxo de
  `executeClusterCheck` revisado: resolve o escopo de triagem ANTES do fallback
  `getAllNamespaces`, com uma flag `triageDecided` que impede esse fallback de sobrescrever um
  escopo já decidido (mesmo quando decidido como vazio — o caso "cluster saudável, nada a
  escanear") — ver nota abaixo, o pseudocódigo original da seção 2.3 tinha essa lacuna
- `internal/healthcheck/dynatrace_checker.go` — `DynatraceChecker.CheckAll` passou a retornar
  `(results, error)` em vez de só `results` — necessário pro `DynatraceTargetSource` distinguir
  "chamada falhou" de "chamada funcionou e não achou nada" (`CheckAll` sempre engoliu esse erro
  internamente, só logava; único call site existente, em `orchestrator.go`, foi atualizado pra
  `dtResults, _ := ...`, preservando o comportamento de quem só quer os resultados)
- `internal/web/handlers/healthcheck.go` — o gate que popula `req.DynatraceURL`/`Token` do
  `UserTokensStore` (`if req.CheckDynatrace || req.CheckOneAgentSignals`) ganhou `|| req.TriageMode`
  — achado real durante a implementação: sem isso, `DynatraceTargetSource` nunca teria credencial
  quando o usuário ligasse só o Modo Triagem sem também marcar o checkbox de correlação DT
  completa, e falharia silenciosamente como "fonte não configurada" (Available=false) mesmo com
  Dynatrace configurado no perfil do usuário
- **Nenhum `*_checker.go` de resultado (Deployment/Service/Config/Event/HPA/PVC) precisou mudar**
  — todos já aceitam `namespaces []string` filtrado, confirmando o achado-chave da seção 1.1

**Nota sobre a lacuna do pseudocódigo original (seção 2.3)**: o rascunho `if len(namespaces) == 0
&& !req.TriageMode` (usando a flag da REQUEST) bloquearia o fallback de Varredura Completa mesmo
no caso de fallback intencional (nenhuma fonte disponível, `req.Namespaces` vazio) — porque
`req.TriageMode` continua `true` nesse caso, mesmo a triagem tendo "desistido". A implementação
usa uma flag derivada do RESULTADO da resolução (`triageDecided`, só true quando alguma fonte
esteve disponível), não da request — corrige esse caso sem mudar a intenção da seção 2.3.

**Validação até agora**: `go build ./...`, `go vet`, `gofmt`, testes unitários dedicados (ver
`target_resolver_test.go`) e a suíte completa de `internal/healthcheck`/`internal/web/handlers`
(`go test -race`) passando — ver também "Validação ao vivo (2026-08-20)" logo após a Fase 2, que
exercitou este backend ponta a ponta contra um cluster real.

#### Validação ao vivo (2026-08-20) — API direta (curl + JWT real), não pelo navegador

Sessão com acesso real a um cluster (`akspriv-abastecimento-hlg`, 23/26 clusters acessíveis no
ambiente): login via `POST /auth/login` (sessão `az` já autenticada), depois
`POST /api/v1/healthcheck/run` com `"triage_mode": true` de verdade.

**Bug real encontrado e corrigido nesta validação** (não coberto por nenhum teste unitário até
então): `triage_summary` sempre voltava `null` no `GET /api/v1/healthcheck/:id`, mesmo com a
triagem rodando certo em memória. Causa: `internal/healthcheck/storage.go` serializa campos sem
coluna própria na tabela SQLite via um struct `extraResultFields` — `TriageSummary` nunca tinha
sido adicionado lá, então `Save`/`Get`/`GetHistory` descartavam o campo silenciosamente. **Mesma
classe de bug que `TestSaveAndGetHistory_RoundTripsAllExtraFields` já existia pra pegar** — o
comentário desse teste documenta um caso idêntico anterior com `NodeResults`; `TriageSummary`
simplesmente nunca tinha sido adicionado a esse teste também. Corrigido (commit `1d9d25df`):
campo adicionado a `extraResultFields` + aos 3 call sites + teste estendido.

**Depois da correção, validado com sucesso**:
- `triage_summary` veio completo: Dynatrace `available:true` (0 problems pro cluster/tag deste
  usuário), Prometheus `available:true` com **10 namespaces reais** e **alertnames reais**
  (`KubeDeploymentReplicasMismatch`, `KubeHpaMaxedOut`, `Eventos PodStatus`, `KubePodNotReady`,
  `KubeJobFailed`, `CPUThrottlingHigh`, `Eventos OOMKilled`, `KubeDaemonSetRolloutStuck`,
  `TargetDown`, `PrometheusNotConnectedToAlertmanagers`, `InfoInhibitor`).
- **Confirmado que o escopo realmente restringiu a varredura**: todo `namespace` presente em
  `deployment_results`/`hpa_results` do resultado é subconjunto do `triage_summary.namespaces`
  resolvido — a triagem não é só um relatório decorativo, ela de fato controla o que os checkers
  varrem.
- **Fase 4 (ignore-list) também validada no mesmo teste**: suprimir `InfoInhibitor` +
  `CPUThrottlingHigh` (via `POST /api/v1/triage-ignore`) derrubou o namespace
  `falcon-image-analyzer` do escopo inteiro (eram seus 2 únicos motivos) e removeu
  `CPUThrottlingHigh` da lista de motivos de todos os outros namespaces afetados, num segundo run.
- Dados de teste (entradas de ignore-list, resultados de health check) removidos do
  banco/config ao final — nada de teste ficou para trás.

**O que ainda não foi clicado**: a UI React em si (toggle no `HealthCheckingTab.tsx`, seção "Escopo
da Triagem" no `HealthReportTab.tsx`) — a validação acima cobriu o backend inteiro (API → cluster
real → resolução de triagem → persistência → API de leitura), mas não abriu um navegador de
verdade. Dado que o frontend só espelha tipos e passa `triage_mode`/lê `triage_summary` sem lógica
própria, o risco residual aqui é baixo, mas ainda é um "não visto com os próprios olhos".

### Fase 2 — Frontend: toggle de modo + transparência de origem — ✅ implementada

**Arquivos modificados:**
- `internal/web/frontend/src/types/healthcheck.ts` — `HealthCheckRequest.triage_mode`,
  `HealthCheckResult.triage_summary`, novos tipos `TriageSummary`/`TriageSourceStatus` (espelham
  `internal/healthcheck/models.go` 1:1)
- `internal/web/frontend/src/components/HealthCheckingTab.tsx` — novo card "Modo de Verificação"
  entre "Namespaces" e "Tipos de Verificação", toggle `triageMode` (`false` por padrão — preserva
  o comportamento atual pra quem não mexer em nada), enviado como `triage_mode` no request. Mesmo
  padrão visual do card "Filtros Inteligentes" já existente (botão que troca de rótulo/ícone,
  `Zap` pra Triagem Rápida vs. `Search` pra Varredura Completa) — não inventou um componente novo.
- `internal/web/frontend/src/components/HealthReportTab.tsx` — seção "Escopo da Triagem" (só
  renderiza quando `result.triage_summary?.enabled`), com: (1) texto do escopo final resolvido ou
  do motivo de fallback, (2) painel "farol" por fonte (item do backlog de 2026-08-20) — badge
  cinza `CircleOff` quando `!available` (fonte fora do jogo), verde `CheckCircle2` quando
  disponível e sem namespace sinalizado, âmbar `AlertTriangle` quando disponível e achou algo;
  `title` do badge mostra o erro real quando existe (`TriageSourceStatus.Error`). Reaproveita
  exatamente o dado que a Fase 1 já produz (`TriageSummary.Sources`) — nenhum campo novo no
  backend foi necessário pra esse painel.

**Validação até agora**: `npx tsc --noEmit -p tsconfig.app.json`, `npx eslint` nos arquivos
tocados, `./rebuild-web.sh -b` (build completo frontend+backend) e suíte Go (`-race`) passando. O
backend que esta UI consome foi validado ao vivo contra um cluster real (ver "Validação ao vivo
(2026-08-20)" ao final da Fase 1) — o que falta especificamente aqui é clicar a UI React em si num
navegador (ver a nota "O que ainda não foi clicado" na mesma seção). **A UI EM SI foi validada ao
vivo pelo usuário** (screenshot real de um cluster acessível, `akspriv-logreversa-prd`, com a
seção "Escopo da Triagem" renderizando corretamente: 8 namespaces resolvidos, badges
Dynatrace/Prometheus, e a tabela de findings abaixo já confinada a esses namespaces).

**Bug de UX real achado nessa mesma validação, round 1 (2026-08-20)**: o painel só mostrava
`Badge: "Prometheus: 8 namespace(s)"` — uma contagem, sem dizer **qual** alerta específico
colocou cada namespace no escopo. Feedback direto do usuário: "só há um badge... nada mais além
disso — como isso serve ao propósito que definimos no plano?". Achado real: `TriageSummary.Reasons`
(o mapa namespace→motivos, ex: `"Prometheus: KubeHpaMaxedOut (warning)"`) já existia no backend
desde a Fase 1 — populado, testado, confirmado nos dados reais da validação ao vivo anterior — e
simplesmente nunca tinha sido renderizado no frontend. Primeira correção: seção colapsável "Motivos
por namespace" dentro da aba "Relatório".

**Round 2 — feedback do usuário sobre a correção do round 1**: "essas informações não deveriam
estar apenas na sub tab relatório, pois ficam perdidas na leitura global do relatório. ela deveria
ter um modal próprio e com as informações mais detalhadas" — acompanhado de um exemplo real
(`calculo-de-fretes-prd`) com **9 linhas idênticas** de `"Prometheus: KubePodNotReady (warning)"`
seguidas. Dois problemas reais, corrigidos juntos:

1. **Lugar errado**: "Escopo da Triagem" vivia só dentro de UMA aba (Relatório) entre várias
   (Deploys/Services/HPAs/etc.) — mas o escopo reduzido afeta TODAS elas igualmente. Corrigido:
   `TriageScopeModal.tsx` (novo componente) — modal dedicado, acionado por um badge **"Triagem"**
   sempre visível no cabeçalho do resultado de cada cluster (mesma linha dos badges
   Healthy/Warning/Critical/Total, visível mesmo sem expandir o card ou trocar de aba). A seção
   correspondente em `HealthReportTab.tsx` foi reduzida a uma única linha-lembrete apontando pro
   badge — não duplica o conteúdo (mesmo princípio de fonte única já seguido por `NoteEntry.tsx`).
2. **Duplicação real, não só um problema de exibição**: o exemplo do usuário revelou que
   Prometheus dispara **um alerta por objeto afetado** (um `KubePodNotReady` por pod, um
   `KubeHpaMaxedOut` por HPA) — todos com o mesmo alertname/severity, virando o mesmo texto de
   motivo repetido dezenas de vezes por namespace. Corrigido na agregação (`resolveTriageTargets`,
   `target_resolver.go`) — motivos idênticos colapsam numa única entrada com contagem (ex:
   `"KubePodNotReady (warning) (×9)"`), ordenados, determinísticos. Corrigido no ponto único de
   merge entre fontes (não em cada `TargetSource`), então cobre automaticamente Zabbix/Elasticsearch
   quando essas fases existirem. Teste dedicado (`TestResolveTriageTargets_DedupsRepeatedReasons`).

**✅ Validado ao vivo de ponta a ponta (2026-08-20, conectividade do cluster restabelecida)**: via
Playwright real contra `akspriv-abastecimento-hlg` — badge "Triagem" (roxo, contador "8") aparece
no cabeçalho do resultado sem precisar expandir nada; clique abre o modal com o painel farol
(Dynatrace 0/Prometheus 8) e a lista por namespace já deduplicada — confirmado ao vivo, ex:
`adanalytics-hlg` foi de 5 linhas repetidas pra 3 linhas únicas com contagem
(`"KubeHpaMaxedOut (warning) (×3)"`). Resultado de teste removido do histórico depois.

**Nota "N de M namespaces"**: a seção 2.4 original sugeria um texto tipo "N de M namespaces
verificados" — implementado só como "N namespace(s) no escopo", sem o "de M", porque
`TriageSummary` (Fase 1) nunca ganhou um campo com o total de namespaces do cluster (só o escopo
resolvido) — adicionar isso exigiria uma chamada squeeze pra `getAllNamespaces` só pra exibir um
número, custo que não parecia valer a pena pro ganho de UI. Puramente uma decisão de escopo, não
um bug — revisitar se o "de M" fizer falta na prática.

**Round 3 — usuário reportou estar "perdido no entendimento da ferramenta"**: achava que nada
comunicava se a triagem achou problema, não achou, ou caiu pra Varredura Completa — mesmo com
badge+modal já existindo (round 2), a informação continuava atrás de um clique. Confirmado via
`AskUserQuestion` (não presumido): (1) o comportamento de fallback está correto como desenhado —
fonte disponível + sem problema = resultado rápido, **sem** escanear por dentro (não virou
"fallback pra Varredura Completa também nesse caso" — isso continuaria a decisão original da seção
0/2.3, não reaberta); (2) mas a comunicação devia ser um **banner sempre visível**, não escondido
atrás do badge. Corrigido: o badge "Triagem" foi removido da fileira de badges
(Healthy/Warning/Critical/Total) e virou um **banner de largura total**, sempre visível, logo
abaixo dessa fileira — cobre as 3 situações com frase explícita, sem exigir interpretação:
- Fallback: `"Modo Triagem: nenhuma fonte disponível — Varredura Completa foi usada nesta execução"` + o motivo
- Sem problema: `"Modo Triagem: nenhuma fonte sinalizou problema — cluster aparenta saudável, nenhum namespace verificado em profundidade"`
- Achou: `"Modo Triagem: N namespace(s) sinalizado(s) — varredura concentrada neles"` + resumo por fonte (`"Dynatrace: sem problema • Prometheus: N sinalizado(s)"`)

O banner inteiro é clicável (abre o `TriageScopeModal` já existente pro detalhe completo por
namespace) — cor muda por estado (âmbar/verde/roxo). **Validado ao vivo** contra
`akspriv-entregamais-hlg`: banner roxo renderizou corretamente com "12 namespace(s) sinalizado(s)
— varredura concentrada neles / Dynatrace: sem problema • Prometheus: 12 sinalizado(s)". Os
estados de fallback e "sem problema" não foram exercitados ao vivo nesta rodada (nenhum cluster
testado caiu nesses casos) — lógica é ternário simples, coberta por type-check, risco baixo.

**Round 4 — usuário questionou se o fallback rápido de fato existe** ("ao que parece está sim
fazendo buscas profundas... investigue isso"), depois de ver o Round 3 rodar por ~12s num cluster
real. Investigação empírica (sem alterar código, só medição) contra `akspriv-entregamais-hlg`:

| Cenário | Duração real | Namespaces verificados | Deployments verificados |
|---|---|---|---|
| Triagem, alertas reais firing | 12,2s | 12 de 18 (display) / 31 (total real) | 32 de 73 |
| Varredura Completa | 22,1s | 18 / 31 | 73 |
| Triagem, **todos os alertas suprimidos artificialmente** (via `/api/v1/triage-ignore`, teste) | **1,07s** | 0 | 0 |

Conclusão: **não é bug**. O fast path existe e funciona (1s comprovado quando genuinamente não há
sinal). A "lentidão" percebida no Round 3 era proporcional a um cluster real com bastante coisa
acesa — 12 dos 31 namespaces tinham alertas Prometheus genuínos de severidade warning/critical
(nenhum era ruído tipo `InfoInhibitor`/`CPUThrottlingHigh` — conferido um por um). `deployment_results`
confirmado como subconjunto estrito de `namespaces` resolvido nos dois casos — sem vazamento de
escopo. Confirmado com o usuário via `AskUserQuestion` que o comportamento de fallback em si
**não muda** — só a comunicação (ver Round 5).

**Round 5 — usuário pediu, a partir da conclusão do Round 4, que o banner deixasse explícito
"N de M namespaces têm problema real"** pra essa distinção (escopo pequeno vs. quase o cluster
inteiro) ficar visível sem precisar eu investigar de novo cada vez. Adicionado:
- `TriageSummary.AllNamespacesCount` (novo campo, backend) — 1 chamada `getAllNamespaces` extra
  (barata, não itera nada por namespace) só quando `TriageMode` resolve com fonte disponível;
  reaproveitada sem custo extra no caso de fallback (a mesma chamada que já ia rodar ali).
- Banner: texto vira `"N de M namespace(s) sinalizado(s)"` (`triageFoundLabel` em
  `HealthCheckResultsPanel.tsx`) — cai pra só "N" em resultados salvos antes desse campo existir
  (`all_namespaces_count` ausente/0, tratado como opcional, nunca quebra).
- Nova linha condicional, só quando a razão N/M ≥ 50%: *"Boa parte do cluster tem problema real
  sinalizado — por isso a varredura ainda pode demorar, mesmo reduzida."* — exatamente a frase que
  o usuário sugeriu, adaptada.
- `TriageScopeModal.tsx` ganhou o mesmo "N de M" no resumo.

**Validado ao vivo** contra o mesmo `akspriv-entregamais-hlg`: banner mostrou **"12 de 31
namespace(s) sinalizado(s)"** (31 = contagem real via `getAllNamespaces`, maior que os 18 que a
API de display filtrada mostra — reforça que a redução real é ainda melhor do que o Round 4
sugeria: ~39% do total real, não 66%). Como 12/31 < 50%, a linha extra de aviso corretamente
**não apareceu** — confirma que o limiar condicional funciona nos dois sentidos, não só quando
verdadeiro.

### Fase 3 — `ElasticsearchTargetSource` — ✅ implementada em 2026-08-20 (convenção assumida, não confirmada contra índice real)

Usuário optou explicitamente por "assumir a convenção padrão e ajustar depois" em vez de bloquear
o código até confirmar manualmente contra um índice real (ver `AskUserQuestion` da retomada desta
fase). Acesso confirmado como **direto ao Elasticsearch** (sem proxy Kibana).

**Arquivos criados:**
- `internal/elasticsearch/client.go` — pacote novo (não existia nenhum client Elasticsearch nesta
  app antes). `Client.NamespaceErrorCounts(ctx, cluster, timeWindow)` roda uma query de agregação
  (`_search` com `size:0` + `aggs.by_namespace.terms`) filtrando por nível de log de erro
  (`error`/`Error`/`ERROR`/`fatal`/`Fatal`/`FATAL`) + janela de tempo (`now-15m` default) + cluster,
  devolve `map[namespace]count`. `Client.TestConnection` faz um `GET /` barato pro botão "Testar
  Conexão" da UI, sem depender de nenhuma convenção de campo/índice. **Convenções assumidas,
  marcadas como constantes exportadas pra fácil ajuste** (`DefaultTimestampField="@timestamp"`,
  `DefaultLevelField="level"` — casa com o formato de log já documentado nesta app pro pipeline
  FluentD+EventHub, ver seção "JSON Inspector" do CLAUDE.md —, `DefaultNamespaceField=
  "kubernetes.namespace_name"` — a convenção que o usuário aprovou assumir).
- **Achado de design durante a implementação, não coberto pelo desenho original**: a query
  PRECISA filtrar por cluster (`DefaultClusterField="cluster_name"`, suposição adicional não
  confirmada) — esta app gerencia ~26 clusters, um índice/pipeline de log compartilhado é o caso
  comum, e sem esse filtro logs de clusters diferentes se misturariam na mesma contagem de
  namespace (ex: dois clusters diferentes com um namespace `logging` cada um contaria junto).
  Inspirado no script de referência da seção 0, que usava `cluster_name` pro mesmo propósito.
  Falha segura se o campo não existir: o filtro por termo não acha nada, resultado vem vazio
  (`Available=true`, `Namespaces=[]`) — nunca mistura dado de cluster errado, nunca fica fatal.
- `internal/elasticsearch/client_test.go` — 5 testes via `httptest`: caminho feliz (endpoint
  certo, Basic Auth correto, filtro de cluster presente na query, parse da agregação), resultado
  vazio (sucesso, não erro), erro HTTP real (distinção crítica pro `Available=false`), index
  pattern default quando não configurado, `TestConnection`.
- `internal/healthcheck/target_source_elasticsearch.go` — `ElasticsearchTargetSource`, mesmo
  formato dos `TargetSource` da Fase 1. Suporte a ignore-list (Fase 4) desde o início — diferente
  de Dynatrace/Prometheus (que suprimem por nome de alerta/problem), aqui o "sinal" já é o próprio
  namespace, então a supressão é por nome de namespace direto (`TriageIgnoreSourceElasticsearchApp`,
  já existia como const desde a Fase 4, só não tinha consumidor real).
- `internal/web/handlers/elasticsearch_config.go` — `ElasticsearchConfigHandler`
  (`GetConfig`/`SaveConfig`/`TestConnection`), mesmo padrão exato de `dynatrace.go` (merge com
  tokens existentes, identidade via `InjectUserEmail()`, senha só sobrescreve se vier não-vazia).
- `internal/web/frontend/src/components/profile/ElasticsearchCredentialModal.tsx` — mesmo padrão
  visual/estrutural de `DynatraceCredentialModal.tsx` (URL/usuário/senha/index pattern, botão
  Testar Conexão, alerta explícito listando as convenções assumidas — usuário vê exatamente o que
  ajustar se a fonte nunca achar nada).

**Arquivos modificados:**
- `internal/storage/user_tokens_store.go` — 4 campos novos (`ElasticsearchURL`/`Username`/
  `Password`/`IndexPattern`), migração `ALTER TABLE`, `SaveTokens`/`GetTokens` atualizados —
  mesma tabela `user_ai_tokens`, mesmo padrão de todas as outras credenciais.
- `internal/healthcheck/models.go` — `HealthCheckRequest` ganha os 4 campos de credencial
  (`json:"-"`, preenchidos só internamente, mesmo padrão de `DynatraceURL`/`DynatraceToken`).
- `internal/healthcheck/orchestrator.go` — `buildTriageSources` inclui a 3ª fonte.
- `internal/web/handlers/healthcheck.go` — o gate de credenciais (já corrigido na Fase 1 pro
  Dynatrace) ganhou os campos Elasticsearch também, mesmo `if (...|| req.TriageMode)`, reaproveita
  a mesma leitura de `GetTokens` — sem consulta extra ao SQLite.
- `internal/web/server.go` — grupo `/api/v1/elasticsearch` (`config` GET/POST, `test` POST),
  mesmo padrão do grupo `/dynatrace`.
- `internal/web/handlers/triage_ignore.go` — `ListSources` marca `elasticsearch_pattern` como
  `enabled: true` (era `false` desde a Fase 4, esperando esta fase); `field_label` ajustado pra
  "Nome do namespace a ignorar" (mais preciso que o "Padrão/app" genérico anterior).
- `internal/web/frontend/src/hooks/useUserProfile.ts` + `UserProfileMenu.tsx` +
  `types/profile.ts` — item "Elasticsearch" no menu de perfil, mesmo padrão de status
  (`configured`/`error`/`not_configured`/`validating`) das demais credenciais.
- `internal/web/frontend/src/lib/api/client.ts` — 3 métodos novos (`getElasticsearchConfig`,
  `saveElasticsearchConfig`, `testElasticsearchConnection`).

**Validação até agora**: `go build`/`go vet`/`gofmt`, 5 testes unitários dedicados (client HTTP,
via `httptest` — cobre a lógica da query/parse sem precisar de um Elasticsearch real),
`tsc --noEmit`/`eslint` limpos, `./rebuild-web.sh -b` com sucesso, rotas confirmadas registradas
(`401`, não `404`, sem token). **Não validado ao vivo contra um Elasticsearch real nem contra a UI
num navegador** — a sessão Azure AD expirou no meio desta implementação (limite de 4h de
"sign-in frequency" por conditional access, confirmado via `az account get-access-token`,
`AADSTS70043`), impedindo gerar um JWT novo pro resto da sessão. **Pendente de validação real**:
(1) confirmar as 4 convenções de campo assumidas (`@timestamp`/`level`/
`kubernetes.namespace_name`/`cluster_name`) contra um índice real desta empresa — o próximo passo
é literalmente uma query manual `curl` contra o Elasticsearch real antes de confiar no resultado
da triagem; (2) testar o fluxo completo na UI (menu de perfil → credencial → Modo Triagem
mostrando o badge "Elasticsearch" no painel farol).

### Fase 4 — Supressão de ruído (ignore-lists) — ✅ implementada (desenho original na seção 2.5)

**Arquivos criados:**
- `internal/healthcheck/triage_ignore.go` — `TriageIgnoreManager` + persistência em
  `~/.k8s-hpa-manager/triage_ignore.json`, mesmo padrão do `FilterManager` (`filters.go`:
  `Load`/`Save`/`AddEntry`/`RemoveEntry`/`GetEntries`, mutex, JSON indentado). Método novo
  `IgnoredValues(source) map[string]struct{}` é o ponto de consumo pelos `TargetSource`.
- `internal/healthcheck/triage_ignore_test.go` — validação/dedup de `AddEntry`, `RemoveEntry`,
  isolamento entre fontes em `IgnoredValues` (uma entrada Dynatrace não pode vazar pro conjunto
  do Prometheus), e round-trip real de persistência (Save → novo `NewTriageIgnoreManager` → Load).
- `internal/web/handlers/triage_ignore.go` — `TriageIgnoreHandler` (`ListEntries`/`AddEntry`/
  `RemoveEntry`/`ListSources`), mesmo padrão de `internal/web/handlers/filters.go`. `ListSources`
  é novo em relação ao design original — devolve as 4 fontes (`prometheus_alert`/
  `dynatrace_problem`/`zabbix_trigger`/`elasticsearch_pattern`) com `enabled: true/false` conforme
  a fase correspondente já tem `TargetSource` implementado — o frontend usa isso pra desabilitar
  Zabbix/Elasticsearch no seletor até as Fases 3/5 existirem, sem precisar hardcodear a lista lá.

**Arquivos modificados:**
- `internal/healthcheck/orchestrator.go` — campo `triageIgnoreManager *TriageIgnoreManager`
  (inicializado em `NewOrchestrator`, não-fatal — falha vira `log.Warn` + `nil`, igual ao
  `deploymentRegistry`), `GetTriageIgnoreManager()`, e `buildTriageSources` passa os conjuntos de
  supressão pros dois construtores.
- `internal/healthcheck/target_source_dynatrace.go` — campo `ignoredProblems map[string]struct{}`,
  filtra por `p.Title` OU `p.DisplayID` (o usuário pode cadastrar qualquer um dos dois, sem
  precisar saber qual usar) ANTES de popular `Namespaces`/`Reasons`.
- `internal/healthcheck/target_source_prometheus.go` — campo `ignoredAlerts map[string]struct{}`,
  filtra por `Labels["alertname"]` antes do filtro de label `namespace` (ordem importa pouco aqui,
  mas checar o nome do alerta primeiro evita o log de "sem label" pra alertas que nem deveriam
  contar de qualquer forma).
- `internal/web/server.go` — grupo de rotas `/api/v1/triage-ignore` (GET público, POST/DELETE
  atrás de `RequireSREGroup()`, mesmo padrão de `/api/v1/filters`).
- `internal/web/frontend/src/lib/api/client.ts` — 4 métodos novos (`getTriageIgnoreEntries`,
  `getTriageIgnoreSources`, `addTriageIgnoreEntry`, `removeTriageIgnoreEntry`).
- `internal/web/frontend/src/hooks/useTriageIgnore.ts` (CRIADO) — mesmo formato de `useFilters.ts`.
- `internal/web/frontend/src/components/FiltersManagementModal.tsx` — ganhou abas manuais
  ("Postura K8s" / "Sinal Externo (Triagem)", nunca shadcn `<Tabs>` — mesma convenção documentada
  no CLAUDE.md) em vez de um modal irmão separado (decisão tomada na implementação, diferente do
  "ou modal irmão" deixado em aberto no desenho original) — menos superfície de UI nova, e a
  distinção de propósito (postura vs. sinal externo) já fica clara só pelo rótulo da aba.

**Desvio deliberado do desenho original (seção 2.5)**: o sketch do plano tinha `TriageIgnoreConfig`
como 4 slices paralelas (`PrometheusAlerts []string`, `DynatraceProblems []string`, etc.). A
implementação usa uma lista única de `TriageIgnoreEntry{ID, Source, Value, Reason, CreatedAt,
CreatedBy}` com um campo `Source` (enum) em vez disso — mais consistente com o `FilterRule` que
já existe neste mesmo pacote (lista de entradas tipadas, não campos paralelos), e simplifica a
API REST pra 1 endpoint de escrita em vez de 4. Mesma ideia, estrutura de dados diferente.

- **Não dependia de Fase 3/5** — confirmado: aplica-se às 2 fontes já implementadas na Fase 1
  (Dynatrace/Prometheus) sem precisar de nenhuma delas existir.

**Validação até agora**: `go build`/`go vet`/`gofmt`, testes unitários dedicados (4 cenários em
`triage_ignore_test.go`), `tsc --noEmit`/`eslint` limpos, `./rebuild-web.sh -b` com sucesso. **✅
Validado ao vivo via API** (ver "Validação ao vivo (2026-08-20)" na Fase 1) — `POST`/`GET`/`DELETE
/api/v1/triage-ignore` exercitados contra o backend real com um JWT real, e o efeito de fato
confirmado contra um Health Check real em Modo Triagem (suprimir `InfoInhibitor`+
`CPUThrottlingHigh` derrubou `falcon-image-analyzer` do escopo). **Só a UI React (a nova aba
"Sinal Externo (Triagem)" dentro de `FiltersManagementModal.tsx`) não foi clicada num navegador.**

### Fase 5 — `ZabbixTargetSource` (condicional, depende de `ZABBIX-INTEGRATION-PLAN.md` Fase 1/4)

Único ponto que precisa de trabalho novo além de "implementar a interface": Zabbix não devolve
namespace diretamente — devolve **host** (e a convenção já confirmada é `host == nome do nó K8s`,
ver seção 5 do `ZABBIX-INTEGRATION-PLAN.md`). Pra virar namespaces flagados, é preciso um join que
**não existe em nenhum checker hoje**: nó → pods rodando nele → namespaces desses pods (o
`NodeChecker` atual resolve o caminho inverso — capacidade do nó, não "quem roda nele" pra fins
de triagem cruzada). Esse join é o único item de esforço real desta fase, além de:
- `internal/healthcheck/target_source_zabbix.go` (CRIAR) — reaproveita o `ZabbixEnricher` da
  Fase 4 do plano Zabbix (`ProblemsForHost`), resolve pods por nó via `client.CoreV1().Pods("").List`
  com `fieldSelector: spec.nodeName=<host>` (chamada padrão do client-go, sem novidade).

### Fase 6 (futuro/opcional) — granularidade por workload, não só por namespace

Só se o uso real da Fase 1 mostrar que "namespace inteiro" ainda traz ruído demais em namespaces
grandes/compartilhados. Toca a assinatura de **todo** `*_checker.go` (adicionar um filtro de nome
de workload opcional) — maior superfície de risco/esforço deste plano inteiro; não detalhado além
disso até a Fase 1 estar validada com uso real.

---

## 4. Riscos e decisões em aberto

1. **Cobertura do label `namespace` em alertas Prometheus não é garantida** — depende de como
   cada regra foi escrita nesta empresa. Mitigação já no desenho (seção 2.2): ausência do label
   só significa "esse alerta não contribui pra triagem", nunca quebra nada.
2. **Modelagem "fonte disponível vs. sem problema" (seção 2.3) é o ponto mais fácil de introduzir
   um bug real** — errar aqui faz um cluster saudável parecer "não verificado" (falso alarme de
   cobertura) ou o oposto (fallback de varredura completa nunca dispara quando deveria). Vale
   teste unitário dedicado nesse ponto específico antes de considerar a Fase 1 pronta.
3. **Fase 5 (Zabbix) depende de um join nó→pods que não existe hoje** — não é só "plugar mais uma
   fonte", é trabalho novo não coberto pelo `ZABBIX-INTEGRATION-PLAN.md` original.
4. **Não decidido ainda**: se `TriageMode` é um modo **exclusivo** (usuário escolhe um dos dois
   antes de rodar) ou se pode coexistir com a Varredura Completa numa mesma execução (ex: triagem
   rápida primeiro, oferecendo "expandir para varredura completa" depois, sem precisar rodar tudo
   de novo do zero). A primeira opção é mais simples de implementar na Fase 1; a segunda tem mais
   valor de UX mas exige guardar o contexto da execução anterior — decidir na Fase 2 com base em
   como a Fase 1 se comportar na prática.
5. **Fase 3 (Elasticsearch) — ✅ TODAS AS PERGUNTAS BLOQUEANTES RESPONDIDAS (2026-08-20)**:
   - Esta empresa opera ELK/Kibana de verdade? **Sim.**
   - Pipeline de ingestão de log (decide o nome do campo de namespace)? **Fluentd** — não
     Filebeat/Fluent Bit. A convenção `kubernetes.namespace_name` (assumida pelo script de
     referência da seção 0, e também comum em Fluentd via
     `fluent-plugin-kubernetes_metadata_filter`) ainda precisa ser **confirmada contra um índice
     real** antes de codar — configs Fluentd variam entre instalações, isso não é garantia
     automática só por ser Fluentd.
   - Já existe credencial de leitura dedicada para acesso automatizado? **Sim** — forma exata
     (Basic Auth vs. API key) não especificada, confirmar ao começar o código.
   - Acesso direto ao Elasticsearch, ou só via Kibana (proxy)? **Direto ao Elasticsearch** — o
     modo "console proxy do Kibana" que o script de referência também suportava não é necessário
     aqui, simplifica o `client.go` (só uma forma de montar a URL de busca, não duas).
   
   Conclusão: Fase 3 pode começar a ser codada quando houver prioridade — não há mais nenhuma
   pergunta pendente do lado de negócio/acesso, só o passo técnico de confirmar o nome exato do
   campo de namespace contra um índice real antes de escrever `client.go` (ver Fase 3, seção 3).
6. **Fase 4 (ignore-lists) decidiu por um mecanismo paralelo ao `FilterManager`, não uma extensão
   dele** — risco de os dois sistemas de supressão (postura K8s vs. sinal externo) parecerem
   redundantes pro usuário final sem uma UI que deixe a distinção clara. Mitigação proposta (seção
   2.5): mesmo modal (`FiltersManagementModal.tsx`), abas separadas — não decidido se vale a pena
   unificar de verdade num único conceito no futuro, revisitar se a duplicação incomodar na prática.

---

## 5. Arquivos a criar/modificar (resumo)

```
internal/healthcheck/target_resolver.go               ← ✅ CRIADO (Fase 1)
internal/healthcheck/target_resolver_test.go           ← ✅ CRIADO (Fase 1)
internal/healthcheck/target_source_dynatrace.go        ← ✅ CRIADO (Fase 1)
internal/healthcheck/target_source_prometheus.go       ← ✅ CRIADO (Fase 1)
internal/healthcheck/models.go                         ← ✅ MODIFICADO (Fase 1 — TriageMode, TriageSummary)
internal/healthcheck/orchestrator.go                   ← ✅ MODIFICADO (Fase 1 — reordenar fluxo)
internal/healthcheck/dynatrace_checker.go              ← ✅ MODIFICADO (Fase 1 — CheckAll retorna error)
internal/web/handlers/healthcheck.go                   ← ✅ MODIFICADO (Fase 1 — gate de credenciais DT)
internal/web/frontend/src/types/healthcheck.ts               ← ✅ MODIFICADO (Fase 2 — triage_mode, TriageSummary)
internal/web/frontend/src/components/HealthCheckingTab.tsx    ← ✅ MODIFICADO (Fase 2 — toggle Triagem/Varredura)
internal/web/frontend/src/components/HealthReportTab.tsx      ← ✅ MODIFICADO (Fase 2 — reduzido a 1 linha-lembrete, ver TriageScopeModal)
internal/web/frontend/src/components/TriageScopeModal.tsx     ← ✅ CRIADO (Fase 2 round 2 — modal dedicado, badge "Triagem" sempre visível)
internal/elasticsearch/client.go                        ← ✅ CRIADO (Fase 3)
internal/elasticsearch/client_test.go                    ← ✅ CRIADO (Fase 3)
internal/healthcheck/target_source_elasticsearch.go      ← ✅ CRIADO (Fase 3)
internal/storage/user_tokens_store.go                    ← ✅ MODIFICADO (Fase 3 — credenciais Elasticsearch)
internal/web/handlers/elasticsearch_config.go            ← ✅ CRIADO (Fase 3)
internal/web/frontend/src/components/profile/ElasticsearchCredentialModal.tsx ← ✅ CRIADO (Fase 3)
internal/web/frontend/src/hooks/useUserProfile.ts        ← ✅ MODIFICADO (Fase 3 — status Elasticsearch)
internal/web/frontend/src/components/UserProfileMenu.tsx ← ✅ MODIFICADO (Fase 3 — item de menu)
internal/web/frontend/src/types/profile.ts               ← ✅ MODIFICADO (Fase 3 — CredentialsState.elasticsearch)
internal/healthcheck/triage_ignore.go                     ← ✅ CRIADO (Fase 4)
internal/healthcheck/triage_ignore_test.go                ← ✅ CRIADO (Fase 4)
internal/web/handlers/triage_ignore.go                    ← ✅ CRIADO (Fase 4)
internal/web/server.go                                    ← ✅ MODIFICADO (Fase 4 — rotas /api/v1/triage-ignore)
internal/web/frontend/src/lib/api/client.ts               ← ✅ MODIFICADO (Fase 4 — 4 métodos triage-ignore)
internal/web/frontend/src/hooks/useTriageIgnore.ts        ← ✅ CRIADO (Fase 4)
internal/web/frontend/src/components/FiltersManagementModal.tsx ← ✅ MODIFICADO (Fase 4 — aba "Sinal Externo (Triagem)")
internal/healthcheck/target_source_zabbix.go            ← CRIAR (Fase 5, condicional)
```

---

## Fontes consultadas (leitura direta do código, não memória)

- `internal/healthcheck/orchestrator.go` (fluxo de `RunForCluster`, ordem de resolução de
  namespaces, execução paralela dos checkers, wiring do `SpinnakerEnricher`/`ResourceEnricher`)
- `internal/healthcheck/models.go` (`HealthCheckRequest`, `DynatraceHealth`)
- `internal/healthcheck/correlator.go` (padrão de correlação pós-hoc hoje usado só por Dynatrace)
- `internal/healthcheck/spinnaker_enricher.go` (padrão de enriquecimento leve, referência de custo)
- `internal/healthcheck/dynatrace_checker.go` (como `K8sNamespaces`/`K8sWorkloads` são populados)
- `internal/monitoring/alerts/types.go` + `client.go` (`Alert`, `GetAlerts`, `GetHPAAlerts`)
- `grafana/README.md` (confirma que o `grafana/` do repo é um dashboard consumindo `/metrics`
  desta própria app, não uma integração onde a app lê dados do Grafana)
- `ZABBIX-INTEGRATION-PLAN.md` (seção 5 — respostas já confirmadas sobre a instalação real)
- `internal/healthcheck/filters.go` + `internal/web/handlers/filters.go` (2026-08-20 — confirmado
  que `FilterManager` é só sobre postura de recursos K8s, não sobre nome de sinal externo; base
  do gap descrito na seção 2.5/Fase 4)
- Busca por `elasticsearch`/`kibana`/`elk` e `redis enterprise`/`azuremonitor` em todo `internal/`
  (2026-08-20 — confirmado que nenhum dos dois existe nesta app hoje; base da Fase 3 e de um item
  de backlog não registrado neste plano — Azure Monitor pra serviços gerenciados é uma categoria
  de feature independente do Health Check, ficou fora deste documento por escopo)
- Script Python de referência de outra ferramenta interna (SRE multi-squad, compartilhado pelo
  usuário em 2026-08-20) — não é parte deste repositório, citado só como inspiração comparativa
  pros itens acima (`classify_source_status()`, `ignore.*` do config de squad,
  `collect_elk`/`collect_azure_redis`)
