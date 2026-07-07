# Dynatrace — Diagnóstico Acionável (Regras Unificadas + Navegação + Distributed Tracing)

Checklist de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

**Contexto**: investigação da aba Dynatrace (branch atual) encontrou que a coleta de dados é sólida
(evidência Davis AI, métricas por tipo de entidade, identificação de cluster/namespace via Node
Pool Registry, prompt de IA bem estruturado, correlação com GitHub Releases), mas boa parte do que
chega na tela é só exibição — não fecha o ciclo até uma ação real. Decidido em conversa com o
usuário: **profissionalizar em 3 fases, nessa ordem** — (1) unificar as regras de threshold→ação
que hoje estão duplicadas e divergentes em 3 lugares, (2) só depois conectar as sugestões a
navegação real na UI, (3) desenhar uma visão de distributed tracing genuinamente útil (hoje é só
uma tabela plana do trace raiz, sem waterfall de spans). Ordem escolhida deliberadamente: navegação
em cima de regras inconsistentes seria "botão bonito apontando pra decisão errada".

---

## Fase 1 — Unificação de regras threshold→ação ✅ CONCLUÍDA

### 1.1 Levantamento (concluído nesta sessão) — 3 lugares, 3 opiniões diferentes

| Sinal | `generateActionItems` (`dynatrace.go:1242`, hardcoded) | `evaluateRisk` (`oneagent_signals_checker.go:330`, configurável) | `EnrichWithLatencyBreach` (`latency_breach_checker.go`, Fase 7 Health Check) |
|---|---|---|---|
| Error rate | ALTA >5% · IMEDIATA >20% | Medium ≥5% · Critical ≥10% | — |
| Latência | **P95**: ALTA >1000ms · IMEDIATA >5000ms | **P90**: Medium ≥2000ms · High ≥5000ms | **P95 fixo**: breach >500ms |
| Pod restarts | ALTA >1 · IMEDIATA >5 | Medium ≥3 · Critical ≥10 | — |
| Pods ready % | ALTA <95% · IMEDIATA <80% | Medium ≤90% · Critical ≤70% | — |
| CPU throttle | ALTA >100% · IMEDIATA >500% | Medium ≥20% · Critical ≥50% | — |

Três implementações independentes do mesmo tipo de julgamento ("essa métrica está ruim?"), com
números diferentes e até **percentil de latência diferente** (P95 vs P90 vs P95-fixo-diferente).
Isso já causou (ou vai causar) o usuário ver o mesmo workload classificado como severidade
diferente dependendo de qual tela ele está olhando — exatamente o tipo de inconsistência que corrói
confiança numa ferramenta profissional.

**Decisões confirmadas com o usuário**: convergir latência pra **P95** em todos os 3 lugares
(`evaluateRisk` passa a consultar P95 em vez de P90); `OneAgentThresholds` vira a fonte única de
verdade (`generateActionItems`/`EnrichWithLatencyBreach` passam a consumir os mesmos números, não
mais valores hardcoded independentes).

**Achado adicional durante a investigação — CPU throttle não é só threshold divergente, é métrica
quebrada** (✅ corrigido nesta sessão): `generateActionItems` usa a métrica
`builtin:containers.cpu.throttlingTime` — testado ao vivo contra o Dynatrace real do projeto
(reconfirmado após checar rede — túnel VPN `tun0` ativo, mesmo IP de saída já usado anteriormente
pelo token, os 404 são reais e não artefato de rede), esse metric ID **não existe mais**
(`404 "No metric found"`). O substituto óbvio por nome, `builtin:containers.cpu.throttledMilliCores`,
**também não funciona** pra este caso — testado ao vivo, sua entidade primária é
`CONTAINER_GROUP_INSTANCE`, não `CLOUD_APPLICATION` (warning da própria API: `"Entity type
mismatch... Possible primary entity types: [CONTAINER_GROUP_INSTANCE]"`). A métrica certa pra
entidades `CLOUD_APPLICATION` (workload) é `builtin:kubernetes.workload.cpu_throttled`
(unidade `MilliCores`) — validado com dado real (7 dias via `entitySelector`, valores de 1.3 a
13.9 mCores num workload de produção real, `k8s.namespace.name`/`k8s.workload.name` presentes no
`dimensionMap`). Não existe métrica nativa de "% de tempo throttled" no catálogo do Dynatrace —
`OneAgentSignal.CPUThrottlePct` (`oneagent_signals_checker.go:170/288`) recebia o valor cru do
metric ID quebrado e tratava como se fosse `%`, inclusive no texto exibido ao usuário
(`"CPU throttle %.1f%% ≥ %.0f%%"`) — sinal que provavelmente sempre voltou vazio/zero (por isso
nunca foi percebido: `if maxVal > 0` em `BatchQueryMetrics` descarta silenciosamente entidades sem
dado pro metric ID inválido).

**Correção aplicada**: `k8sWorkloadMetricDefs` (`metrics.go`) agora usa
`builtin:kubernetes.workload.cpu_throttled`, chave renomeada de `"cpu_throttle"` (%, nunca
funcionou) pra `"cpu_throttle_millicores"` (mCores, unidade real) — `internal/actionrules` usa
`SignalCPUThrottleMilliCores` com esse nome. Threshold em mCores ainda **não tem base empírica
robusta** (só 1 workload real de referência visto nesta sessão, com throttle baixo ~1-14 mCores) —
`50`/`200` mCores warn/critical em `DefaultThresholds()` são primeira aproximação, documentado
explicitamente no código como "recalibrar depois de ver dado real de produção" (mesmo padrão já
usado pra `latencyBreachThresholdMs`).

### 1.2 Desenho do pacote compartilhado

- [x] Nome definido: `internal/actionrules/` (top-level, não aninhado em `healthcheck` nem em
      `web/handlers` — precisa ser importável pelos dois sem inverter a direção de dependência já
      estabelecida na Fase 7 do plano de latência, mesmo motivo de `internal/monitoring/latencylookup`)
- [x] Um tipo único de regra: `{SignalKey, WarnThreshold, CriticalThreshold, Comparator (>/<),
      AppSection, ActionTemplate}` — `ActionTemplate` é a string de ação (ex: "Aumentar maxReplicas
      — latência P95 indica sobrecarga"), reaproveitando o texto já bom de `generateActionItems`
- [x] O pacote devolve um nível NEUTRO de 3 estados (`LevelOK`/`LevelWarn`/`LevelCritical`), não o
      vocabulário de nenhum chamador específico — `generateActionItems` mapeia isso pra
      "MONITORAR"/"ALTA"/"IMEDIATA" (`actionRulesLevelToUrgency`), `evaluateRisk` mapeia pra
      `healthcheck.Severity` via `oneAgentSeverityCeiling` (preserva o teto de severidade por sinal
      já existente antes: error_rate/pod_restarts/pods_ready_pct escalam até Critical, latência/CPU
      throttle ficam no teto High — decisão de vocabulário mantida do código original, não é
      "unificação" do lado da severidade, só do lado dos números/texto de ação)
- [x] Struct de retorno: `{Signal, Level, Reason, Action, AppSection}` — hoje `ActionItem` tem
      `Action`+`Reason` mas `OneAgentSignal` só tem `RiskReasons` (sem ação sugerida); os dois
      passam a ganhar o mesmo `Action` do pacote compartilhado (`OneAgentSignal.SuggestedActions`)

**Achado extra durante a implementação**: existia um **4º lugar** com o mesmo tipo de threshold
divergente — `severityLabel()` (`dynatrace.go`, usada no painel "Métricas" e no prompt de IA pra
colorir/rotular valores 🔴🟡🟢). Também migrada pro `actionrules` (trata P90/P99 como P95 pra fins
de rotulagem visual — só a decisão de ação em `generateActionItems`/`evaluateRisk` exige P95
estrito; aqui é só cor).

### 1.3 Migrar `generateActionItems` (`internal/web/handlers/dynatrace.go`) ✅

- [x] Trocar os `switch m.Key { case "error_rate": if mx > 20 ... }` por chamada ao pacote
      compartilhado, mantendo a assinatura pública `generateActionItems(problem, mr)
      []ActionItem` (não quebrou `AnalyzeProblem`) — via `actionRulesSignalForMetric`/
      `actionRulesLevelToUrgency`

### 1.4 Migrar `evaluateRisk` (`internal/healthcheck/oneagent_signals_checker.go`) ✅

- [x] Trocar a lógica de threshold por chamada ao pacote compartilhado
- [x] Adicionar campo `SuggestedActions []string` em `OneAgentSignal`
      (`internal/healthcheck/models.go`) — paridade com `ActionItem.Action`
- [x] Trocar a métrica consultada de P90 pra P95 — confirmado que `response_p90` só era usado
      dentro do próprio `evaluateRisk`/`OneAgentSignal` (não usado em nenhum outro lugar do
      backend); `serviceMetricDefs` já tinha `response_p95` definido, não precisou de métrica nova
- [x] **Bônus não previsto no plano original**: `OneAgentThresholds` virou `type ... =
      actionrules.Thresholds` (alias, não struct duplicado) — mais simples que um conversor,
      possível porque nada no frontend referenciava os campos JSON antigos (`response_p90_warn_ms`
      etc. — confirmado via grep, zero uso), então renomear os campos foi seguro

### 1.5 `latencyBreachThresholdMs` (`internal/healthcheck/latency_breach_checker.go`, Fase 7) — ⚠️ revisado, NÃO migrado

**Decisão revisada durante a implementação**: o item original assumia trocar a constante local
`500.0` pelo `actionrules.DefaultThresholds().LatencyP95CritMs` — mas esse valor compartilhado é
`5000ms` (herdado do antigo `ResponseP90CritMs`/`OneAgentThresholds`), **10x mais permissivo** que
os `500ms` que o usuário já tinha confirmado explicitamente nesta sessão pro breach do Health
Check. Os dois "P95 crítico" respondem perguntas diferentes, não é a mesma regra duplicada:
`EnrichWithLatencyBreach` pergunta "isso **corrobora** um DT Problem que já está aberto?" (barra
mais sensível, porque já há outro sinal independente apontando problema) — `generateActionItems`/
`evaluateRisk` perguntam "isso **sozinho** já é motivo de alerta?" (barra mais alta, é o único
sinal). Migrar silenciosamente pra 5000ms teria revertido uma decisão já tomada com o usuário sem
avisar — não fiz isso.

- [x] `latencyBreachThresholdMs = 500.0` mantido como constante própria, com comentário explicando
      por que não usa `actionrules.DefaultThresholds()` (diferença conceitual, não descuido)
- [ ] (opcional, não crítico) considerar expor esse "threshold de corroboração" como um segundo
      valor dentro de `actionrules` no futuro, se mais lugares precisarem da mesma semântica —
      por ora, 1 único consumidor não justifica generalizar

### 1.6 Testes unitários da tabela de regras ✅

- [x] Tabela de casos determinística (dado valor X + threshold, retorna severidade+ação
      esperada) — `internal/actionrules/rules_test.go`, mesmo padrão de teste sem rede já usado em
      `latency_breach_checker_test.go`
- [x] Teste de regressão adaptado: em vez de "rodar os 3 chamadores com mesma entrada" (não faz
      mais sentido do jeito que a Fase 1.5 evoluiu — `EnrichWithLatencyBreach` deliberadamente usa
      um threshold diferente, ver 1.5), o teste real de consistência é `TestEvaluate_TableDriven` +
      `TestEvaluate_WarnAndCriticalHaveDifferentActions` cobrindo os 5 sinais que `generateActionItems`
      e `evaluateRisk` SIM compartilham hoje. `go test ./internal/... -race` completo passa.

### 1.7 Frontend — exibir a ação sugerida onde hoje só mostra o motivo ✅

- [x] `OneAgentSignalCard` (`HealthCheckResultsPanel.tsx`) — hoje mostrava só `risk_reasons`;
      adicionada seção `suggested_actions` (texto com ícone `ArrowRight`, cor âmbar/verde — sem
      navegação ainda, isso é Fase 2). Labels de métrica também corrigidos: "P90"→"P95",
      "CPU throttle: X%"→"CPU throttling: X mCores"
- [x] `types/healthcheck.ts` — `OneAgentSignal` ganhou `response_p95_ms` (era `response_p90_ms`),
      `cpu_throttle_millicores` (era `cpu_throttle_pct`), `suggested_actions?: string[]`
- [x] `tsc --noEmit` limpo, `./rebuild-web.sh -b` ok, servidor sobe e responde 200

### Validar antes de codar — ✅ ambas confirmadas com o usuário via pergunta direta antes de codar

- [x] Convergir pra P95 (item 1.2): **confirmado, sim** — `evaluateRisk` passou a consultar
      `response_p95` em vez de `response_p90`
- [x] `OneAgentThresholds` fonte única de verdade também pro fluxo de Problems: **confirmado, sim**
      — virou alias de `actionrules.Thresholds`, sem valores independentes por fluxo

---

## Fase 2 — Fechar o loop: navegação real das sugestões ✅ CONCLUÍDA (código; validação no navegador pendente)

**Escopo combinado com o usuário antes de codar**: só `app_section` `"HPA"` e `"Deployments"`
nesta rodada — `"Resource Explorer"` (CPU throttle) e `"Health Check"` ficam texto puro (nenhuma
aba tem pré-seleção de workload pra elas ainda). As 3 superfícies (ActionPlanCard,
HealthCheckResultsPanel "Onde atuar", OneAgentSignalCard) entraram todas nesta rodada.

- [x] Contrato de navegação definido — **`TabContext`/`useTabManager` NÃO é o mecanismo** (achado
      da pesquisa: é só persistência de sessão por aba-de-cluster tipo browser, Alt+1..9, não
      dirige o que `Index.tsx` renderiza — `updateActiveTabState` não teria efeito visível
      nenhum). O mecanismo real é o padrão já usado por `pendingHPANavigation` +
      `preSelectedHPA`/`MonitoringPage`: estado pendente em `Index.tsx`, resolução contra a lista
      já carregada localmente pelo componente alvo. `Index.tsx` ganhou `navigateToHPA` (extraído
      do antigo callback inline `onNavigateToHPA`, sem duplicação), `navigateToDeployment` (novo,
      com `pendingDeploymentNavigation`) e `navigateToWorkload` (combinador único passado como
      prop pros consumidores externos)
- [x] `ActionPlanCard` (`DynatraceTab.tsx`) — botão "Abrir →" usando `cluster`/`namespace`/
      `workload` de `ActionItem`, threading por 4 componentes (`DynatraceTab` →
      `ProblemDetailPanel` → `DiagnosticoTab` → `AIAnalysisResult`/`ActionPlanCard`)
- [x] "Onde atuar no HPA Manager" (`HealthCheckResultsPanel.tsx`, K8s↔DT correlacionado) — zero
      mudança de backend: `CorrelatedK8sIssue.resource_kind` já indica a aba do lado K8s, e
      `DynatraceHealth.suggestions` do lado DT já vem prefixado `"Aba HPA/Deployments → ..."`
      (`buildDTSuggestions` no backend) — extraído via regex `^Aba (HPA|Deployments)\b`
- [x] `OneAgentSignalCard` — `suggested_actions` é um `[]string` solto vindo de
      `internal/actionrules/rules.go` (10 strings fixas, 5 sinais × warn/crit); mapeado via
      tabela exata `ONE_AGENT_ACTION_APP_SECTION` no frontend (sem heurística de keyword,
      sem mudança de backend) — precisa ficar em sincronia manual se as strings do Go mudarem
- [x] `DeploymentsTab` ganhou `preSelectedDeployment`/`onDeploymentSelected`, espelhando
      `preSelectedHPA`/`MonitoringPage` (resolução local contra a lista `deployments` já
      buscada pelo componente, não uma lista lifted pra `Index.tsx`)
- [x] `tsc --noEmit` limpo, `go build ./...` ok (nenhuma mudança de backend), `./rebuild-web.sh -b`
      ok, servidor sobe e responde 200
- [ ] **Validar navegação real no navegador** — não foi possível nesta sessão (exige cluster com
      Health Check/DT rodado, com um problem `app_section: "HPA"` e outro `"Deployments"`
      simultâneos, e um item correlacionado K8s↔DT com sugestões navegáveis) — validar os 3
      fluxos (ActionPlanCard, "Onde atuar", OneAgent Signals) contra dado real antes de considerar
      a fase 100% fechada

Commit: `feat(dynatrace): Fase 2 - navegação real das sugestões de ação`.

---

## Fase 3 — Distributed Tracing genuinamente útil ⚠️ alto risco, validar antes de desenhar

### 3.1 Estado atual (o que já existe, e por que não ajuda)

- Backend `getServiceTraces` (`internal/dynatrace/context.go:158`) já busca traces via
  `/api/v2/distributed-tracing/traces`, mas só extrai o **span raiz** de cada trace (nome, método,
  status, duração total) — descarta a árvore de spans filhos (chamada de banco, chamada externa,
  etc.), que é justamente o que responde "onde foi parado o tempo dentro dessa requisição"
- Frontend `TracesSection` (`DynatraceContextPanel.tsx:326`) é uma tabela plana (endpoint / método
  / status / duração / início) — sem waterfall, sem hierarquia de spans

### 3.2 Achado desta sessão — o endpoint clássico pode nem funcionar mais neste tenant

Testado ao vivo contra o Dynatrace real do projeto (token com escopo `traces.lookup` confirmado):

- `GET /api/v2/distributed-tracing/traces` → **404** (o path que o código atual usa)
- `GET /api/v2/distributedTracing/traces` (camelCase) → **404**
- `GET /api/v2/traces` → **404**
- `POST /platform/storage/query/v1/query:execute` (Grail DQL, `fetch spans`) → **403 Forbidden by
  administrative rules** — não é erro de escopo do token, parece bloqueio de infraestrutura/proxy
  ou exigência de autenticação diferente (OAuth2, não `Api-Token` clássico) pra esse grupo de APIs

**Hipótese mais provável**: este tenant Dynatrace já migrou pro modelo "Grail" de distributed
tracing (o escopo `traces.lookup` é o nome usado pelas APIs novas, diferente de `DataExport`/
`CaptureRequestData` mencionados no comentário do código atual, que são nomes de escopo do modelo
clássico) — e as APIs Grail/Platform tipicamente exigem um client OAuth2 registrado no tenant
(client_id/client_secret via Dynatrace SSO), não o token clássico `Api-Token` usado em toda a
`internal/dynatrace/client.go` hoje.

### 3.3 Antes de desenhar qualquer waterfall — decisões e validações necessárias

- [x] **Decisão do usuário**: no uso normal, o login é pessoal (usuário/senha) — as operações
      automatizadas do app sempre usaram `Api-Token` clássico, nunca OAuth2. Criar um client
      OAuth2 pras APIs Platform exige acesso admin no tenant Dynatrace (Dynatrace ID Settings),
      separado do login pessoal. **Decisão**: usuário vai verificar primeiro se tem (ou consegue)
      esse acesso admin antes de qualquer código de Fase 3 — nenhuma implementação começa até essa
      confirmação. Se não houver acesso admin viável, cai automaticamente na alternativa de escopo
      menor (mensagem de erro honesta, ver abaixo) sem waterfall de spans.
- [ ] Se seguir adiante: descobrir o endpoint Grail correto pra distributed tracing (a query DQL
      `fetch spans` pode não ser a sintaxe certa — precisa de acesso a um tenant/documentação
      Dynatrace atualizada, ou testar com um token que tenha permissão pra `/platform/`)
- [ ] Confirmar se o problema é mesmo auth (testar o mesmo request com uma OAuth2 client
      credentials válida, se/quando existir) antes de assumir que é só isso
- [ ] Alternativa de escopo menor, caso o investimento em OAuth2 não valha a pena agora: **melhorar
      a mensagem de erro atual** (`TracesSection`'s `tracesError` handling) pra ser honesta sobre
      isso — hoje ela sugere "adicione o escopo DataExport ao token", o que é uma instrução que já
      sabemos (por este teste) que não resolve nesse tenant; a causa real é outra

### 3.4 Se a validação acima confirmar que dá pra buscar a árvore completa de spans

- [ ] Expandir `tracesResponseRaw`/`TraceEntry` pra capturar por span: `spanId`, `parentSpanId`,
      `startTime` (offset relativo ao início do trace), `duration`, nome do serviço
- [ ] Componente de waterfall: barra horizontal por span, offset proporcional ao tempo relativo,
      largura proporcional à duração — não precisa de lib nova, um SVG/div simples com `position:
      absolute` + `left`/`width` em % já resolve (mesmo espírito do `HistoryChart` já usado em
      Conntrack, mas sem precisar de Recharts pra isso)
- [ ] Priorizar traces com erro (`hasError: true`) no topo da lista — é o que interessa pra debugar
      o alarme, não os traces saudáveis
- [ ] Destacar visualmente o span mais lento dentro de cada trace (o "onde foi parado o tempo")

---

## Ordem de implementação combinada com o usuário

1. Fase 1 (regras unificadas) — começar imediatamente
2. Fase 2 (navegação) — só depois da Fase 1 fechada
3. Fase 3 (tracing) — precisa de decisão explícita do usuário sobre investir em OAuth2 Dynatrace
   antes de qualquer código; pode ficar descoberta por bastante tempo dependendo dessa decisão
