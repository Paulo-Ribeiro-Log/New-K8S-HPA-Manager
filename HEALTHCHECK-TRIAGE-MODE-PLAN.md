# Plano: Modo Triagem no Health Check (sinaliza primeiro, investiga depois)

**Status:** 🟡 Fase 1 implementada (backend) — Fase 2 (frontend: toggle + transparência de escopo)
ainda não iniciada. Sem validação ao vivo contra um cluster real ainda (só testes unitários +
build/vet/testes existentes passando).
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
   implementar o Modo Triagem inteiro hoje. Zabbix entra depois, como uma 3ª fonte plugável,
   quando `ZABBIX-INTEGRATION-PLAN.md` tiver pelo menos a Fase 1 pronta — sem precisar reabrir a
   arquitetura deste plano (ver seção 3, Fase 3).

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

Granularidade por workload fica como possível incremento futuro (Fase 4, condicional — ver
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
- **`ZabbixTargetSource`** (Fase 3, condicional): ver seção 3.

União de `Namespaces` de todas as fontes `Available=true` = escopo de triagem.

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
(`go test -race`) passando. **Não validado ainda contra um cluster real** — a Fase 1 é só backend;
sem toggle no frontend (Fase 2), a única forma de exercitar `triage_mode` hoje é enviando
`{"triage_mode": true, ...}` direto pro `POST /api/v1/healthcheck/run`.

### Fase 2 — Frontend: toggle de modo + transparência de origem

- Componente de configuração do Health Check (verificar nome exato do form na hora — painel de
  opções antes de iniciar o check): toggle "Triagem rápida (recomendado)" vs. "Varredura completa"
- `HealthReportTab.tsx` (ou onde fizer mais sentido dentro dos resultados): seção de escopo da
  triagem (seção 2.4)

### Fase 3 — `ZabbixTargetSource` (condicional, depende de `ZABBIX-INTEGRATION-PLAN.md` Fase 1/4)

Único ponto que precisa de trabalho novo além de "implementar a interface": Zabbix não devolve
namespace diretamente — devolve **host** (e a convenção já confirmada é `host == nome do nó K8s`,
ver seção 5 do `ZABBIX-INTEGRATION-PLAN.md`). Pra virar namespaces flagados, é preciso um join que
**não existe em nenhum checker hoje**: nó → pods rodando nele → namespaces desses pods (o
`NodeChecker` atual resolve o caminho inverso — capacidade do nó, não "quem roda nele" pra fins
de triagem cruzada). Esse join é o único item de esforço real desta fase, além de:
- `internal/healthcheck/target_source_zabbix.go` (CRIAR) — reaproveita o `ZabbixEnricher` da
  Fase 4 do plano Zabbix (`ProblemsForHost`), resolve pods por nó via `client.CoreV1().Pods("").List`
  com `fieldSelector: spec.nodeName=<host>` (chamada padrão do client-go, sem novidade).

### Fase 4 (futuro/opcional) — granularidade por workload, não só por namespace

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
3. **Fase 3 (Zabbix) depende de um join nó→pods que não existe hoje** — não é só "plugar mais uma
   fonte", é trabalho novo não coberto pelo `ZABBIX-INTEGRATION-PLAN.md` original.
4. **Não decidido ainda**: se `TriageMode` é um modo **exclusivo** (usuário escolhe um dos dois
   antes de rodar) ou se pode coexistir com a Varredura Completa numa mesma execução (ex: triagem
   rápida primeiro, oferecendo "expandir para varredura completa" depois, sem precisar rodar tudo
   de novo do zero). A primeira opção é mais simples de implementar na Fase 1; a segunda tem mais
   valor de UX mas exige guardar o contexto da execução anterior — decidir na Fase 2 com base em
   como a Fase 1 se comportar na prática.

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
internal/web/frontend/src/components/HealthReportTab.tsx (ou equivalente) ← MODIFICAR (Fase 2)
internal/healthcheck/target_source_zabbix.go            ← CRIAR (Fase 3, condicional)
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
