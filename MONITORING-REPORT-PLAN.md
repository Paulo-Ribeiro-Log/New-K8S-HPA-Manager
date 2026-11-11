# Plano: relatório de métricas na aba Monitoramento

Status: **fase 2 (backend) implementada e validada; fase 3, etapa 1 (aba com seleção, janela, coleta e tabela) implementada, com alvos escolhidos direto nos clusters (cartões com cluster HLG/PRD, namespaces e deployments, e o botão + para outro alvo; sem depender da lista de HPAs monitorados)**; faltam a exportação (CSV, Markdown com o código para copiar e PDF), a coluna de node pool e salvar/comparar eventos. Atualizar a tabela de fases conforme o trabalho avançar.

## 1. Objetivo

Hoje a aba Monitoramento mostra CPU, memória e réplicas de **um HPA por vez**. Falta a visão que o SRE usa no dia a dia: **uma tabela por aplicação** com tráfego, latência, erros e consumo, numa **janela de tempo escolhida** (por exemplo, de 01/10/2026 23:00 até 02/10/2026 04:30), pronta para copiar e exportar.

Exemplo do relatório desejado (uma linha por aplicação/namespace):

| Aplicação | Total Requests | Max RPS | Latência P99 (pico) | Erros 4xx | Erros 5xx | CPU % (máx/mín/média) | Mem % (máx/mín/média) | Pico Nodepool (CPU/Mem) |
|---|---|---|---|---|---|---|---|---|
| frete-hub (ns: calculo-de-fretes-prd) | 1170869 | 740.0 | 1.769s 🔴 | 78883 ⚠️ | 0 | 9.0% (Min 1.9%, Avg 6.7%) | 48.0% (Min 47.5%, Avg 47.8%) | calculofrete: 9.9C / 80.9GB |
| entrega-envias (ns: envvias-prd) | 614448 | 378.8 | 4.534s 🔴 | 95905 ⚠️ | 1 🔴 | 12.3% (Min 2.8%, Avg 8.2%) | 49.0% (Min 48.9%, Avg 48.9%) | envias: 0.8C / 4.2GB |

## 2. Estado atual (o que já existe)

- **Aba:** foco em HPA; lista de monitorados no navegador; alertas de anomalia; dados do Prometheus.
- **Não existe:** RPS, requisições, latência, erros 4xx/5xx, visão multi-aplicação, node pool, relatório, exportação, histórico.
- **Reaproveitar:**
  - `internal/dynatrace/metrics.go`: throughput, latência P50/P90/P95/P99, taxa e contagem de erros.
  - `internal/monitoring/latencylookup`: busca Dynatrace → Prometheus (mesmo padrão `MetricsSource` do FinOps).
  - `internal/monitoring/client/prometheus_client.go`: histórico por Deployment (`GetDeploymentHistoricalMetrics`), limites e requests.
  - `internal/monitoring/nodepoolpredictions`: coleta por node pool.
  - `internal/monitoring/discovery`: proxy do Grafana Cloud (rótulo `cluster` já injetado).
  - `internal/monitoring/prometheus/queries.go`: templates de `http_requests_total` e latência, **ainda não ligados a nada**.

## 3. Janela de tempo

O relatório sempre roda sobre uma **janela absoluta** `[início, fim]`.

**Seleção na tela**
- Atalhos: última 1 h, 6 h, 24 h, 7 d (a janela termina "agora" no momento de coletar).
- **Personalizado:** campos de **data e hora de início e de fim** (ex.: início 01/10/2026 23:00, fim 02/10/2026 04:30), com cruzamento de meia-noite permitido.
- Fuso: **America/Sao_Paulo** por padrão, mostrado ao lado dos campos; internamente tudo em UTC. O servidor recebe os instantes em ISO 8601 com offset.
- A janela usada aparece no cabeçalho do relatório e vai junto de qualquer exportação.

**Validações (servidor e tela)**
- Fim maior que o início; fim não pode estar no futuro (é cortado em "agora" com aviso).
- Janela mínima de 5 min e máxima de 30 dias (ajustável por constante).
- Aviso quando a janela **começa antes da retenção** da fonte: o relatório sai com o que existe e a coluna indica "retenção insuficiente" em vez de preencher com zero. Retenção do Prometheus e do Dynatrace são descobertas na sondagem (fase 1), não assumidas.
- Mudança de horário de verão não existe hoje no Brasil, mas o código usa o fuso IANA e não um offset fixo.

**Como a janela entra nos cálculos**
- **Passo (`step`) automático** pela duração da janela (ex.: ≤ 6 h → 1 min; ≤ 24 h → 5 min; ≤ 7 d → 15 min; maior → 1 h), limitado a ~1000 pontos por série. O passo usado é registrado no relatório.
- **Total de requisições:** `increase` do contador sobre a janela (trata reset de contador); no Dynatrace, soma da contagem.
- **Max RPS:** pico da taxa (`rate` de 1 min) dentro da janela; a janela da taxa é fixa e registrada.
- **P99 de pico:** máximo da série de P99 na janela (nunca média de percentis).
- **CPU/Mem máx/mín/média:** estatísticas da série na janela.
- **Pico do node pool:** valor no instante do pico da aplicação dentro da janela (definição a confirmar, ver seção 8).
- Janela longa com passo grosso pode **suavizar picos curtos**: o relatório mostra o passo para o leitor saber.

### 3.1 Janelas antigas e eventos (stress test)

O uso real inclui **janelas de eventos passados** (ex.: um stress test de meses atrás). Isso muda dois pontos:

- **Retenção por idade.** O Dynatrace guarda a resolução fina só por um tempo e depois agrega (valores padrão do SaaS, **a confirmar no ambiente**, pois o Managed pode diferir): 1 min ≈ 14 dias, 5 min ≈ 28 dias, 1 h ≈ 400 dias. Numa janela de meses, um pico curto (ex.: 740 RPS por poucos minutos) **vira média de hora** e é subestimado. O relatório escolhe a **melhor resolução disponível para a idade da janela**, mostra a resolução usada em cada coluna de pico (Max RPS, P99) e marca **"pico aproximado (resolução 1 h)"** quando a resolução é grossa demais para a duração da janela. O Prometheus tem retenção própria (descoberta na sondagem, seção 7).
- **Guardar o resultado.** Como a fonte perde o detalhe com o tempo, o relatório salvo (SQLite) guarda a **fotografia dos números** no momento da coleta. Para eventos planejados (stress test), o ideal é **coletar logo depois do evento** e salvar com um nome ("Stress test frete 01/10"); o histórico passa a ser a fonte da verdade para comparar testes futuros.
- **Evento nomeado:** além de "janela", o relatório pode ser salvo como **evento** (nome, janela, aplicações, observações), reabrível e comparável com outro evento.

## 4. Colunas e fontes

| Coluna | Fonte provável | Observação |
|---|---|---|
| Total Requests | Dynatrace `requestCount` ou Prometheus `increase` | |
| Max RPS | Prometheus `rate` / Dynatrace | depende da janela da taxa |
| Latência P99 (pico) | Dynatrace P99 ou `histogram_quantile` | máximo da série |
| Erros 4xx / 5xx | por classe de status | confirmar métrica por classe no Dynatrace |
| CPU % e Mem % | Prometheus (cAdvisor) | base do %: request ou limit (a decidir) |
| Pico Nodepool | `nodepoolpredictions` | definição a confirmar |

**Atenção a métrica compartilhada:** no exemplo, `frete-hub`, `frete-b2c` e `frete-envvias` têm requisições, RPS, P99 e 4xx idênticos (o mesmo vale para as duas `estoque-*`). Isso indica fonte por ingress/gateway/namespace, não por aplicação. O relatório coleta **por serviço** quando a fonte permite; quando só há métrica compartilhada, a linha recebe a marca **"métrica do namespace"** e não finge precisão.

Regras gerais: sem dado = "sem dado" (nunca 0); cada coluna indica a **fonte** usada (Dynatrace ou Prometheus); limites dos selos 🔴/⚠️ configuráveis (valores a decidir).

## 5. Interface

- Sub-visão **Relatórios** dentro de Monitoramento; código e rotas próprios, sem alterar os gráficos de HPA.
- Passos: escolher cluster(s) → aplicações (por namespace ou **conjunto salvo**) → janela de tempo (seção 3) → **Coletar**.
- Coleta com **progresso em tempo real** (SSE) e resultado parcial; cancelável.
- Tabela ordenável e filtrável, com selos e fonte por coluna; linha expansível com os gráficos da aplicação na janela.
- Exportar: copiar como TSV/Markdown (formato do exemplo), CSV e XLSX; a janela e o passo vão no cabeçalho.
- Salvar o relatório (com a janela) e **comparar com outro período** (deltas de requisições, latência, erros, consumo).
- Página de ajuda `help/pages/monitoring.md` atualizada e CHANGELOG.

## 6. Backend

- Pacote novo `internal/monitoring/report` (independente do engine atual).
- `MetricsSource` com implementações Dynatrace e Prometheus; fonte por cluster descoberta na sondagem.
- **Uma consulta por métrica para o namespace inteiro**, agrupada por serviço (`sum by (...)`), em vez de N consultas por aplicação.
- Coleta paralela com limite de simultaneidade, `context` com timeout, cache por (cluster, consulta, janela).
- Rotas `POST /api/v1/monitoring/reports` (inicia coleta → id), `GET .../:id` (estado/resultado), `GET .../:id/stream` (SSE, token em `?token=`), `GET .../` (salvos), `GET .../:id/export?format=`.
- Histórico em SQLite próprio (`~/.k8s-hpa-manager/monitoring_reports.db`, WAL); somente leitura nos clusters.
- Clusters com Grafana Cloud passam pelo proxy existente (rótulo `cluster`).

## 7. Fases

| Fase | Entrega | Validação |
|---|---|---|
| 0 | Decisões da seção 8 | respostas do usuário |
| 1 | **Sondagem das fontes** em 2 clusters: métricas disponíveis (Istio, ingress-nginx, Dynatrace), por serviço ou por namespace, retenção | comandos rodados pelo usuário com `!` (o agente não consulta a nuvem por conta própria) |
| 2 | Backend de coleta e cálculo da janela, com testes de fixture | `go test -race` |
| 3 | Interface: janela de tempo, coleta, tabela, ajuda | `tsc`, `eslint`, vitest para a lógica de janela/passo |
| 4 | Exportar, salvar relatório/evento (fotografia dos números), comparar eventos e janelas antigas com resolução e aviso de pico aproximado | teste manual contra um cluster real |
| 5 | Análises por HPA: throttling de CPU, OOM/restarts, tempo no máximo de réplicas, folga, sugestão de ajuste, correlação com deploys | teste manual |

## 7.1 Achados da sondagem (fase 1, parcial)

- **Dynatrace responde** (`/api/v2/metrics`) e tem erros **por classe de status**: `builtin:service.errors.fourxx.count|rate` e `builtin:service.errors.fivexx.count|rate` (24 métricas em `builtin:service.errors.*`). A coluna 4xx/5xx pode vir do Dynatrace. Falta conferir se a contagem bate com o relatório de referência (ex.: 78.883 de 4xx no `frete-hub`).
- **Nome não identifica a aplicação no Dynatrace** (confirmado):
  - os 7 serviços chamados exatamente `frete-hub` são `DATABASE_SERVICE` (visão de chamadas de banco, sem tags), não a aplicação;
  - os serviços da aplicação têm nomes derivados por regra de nomeação (`frete-hub-*`, `frete-hub-regionalizado-*`); `frete-envvias` tem ~19 entidades, incluindo "Requests executed in background threads of SpringBoot …";
  - `frete-b2c` não existe com esse nome: aparece como controllers (`FreteB2CAdminController`, `FreteProcessamentoMassivoB2CController`), e os `api-b2c.*` (IIS, CB/PF/EX) são outras aplicações.
  - **Consequência:** buscar por nome e somar está errado. O `latencylookup.Fetch` tem essa limitação documentada (fica com a primeira entidade devolvida) e **não deve ser reutilizado como está** no relatório.
  - **Resolução proposta:** reaproveitar o caminho de `internal/dynatrace/coverage.go` (processos do host group do cluster → `SERVICE runsOnProcessGroupInstance`, com namespace e nome do serviço por linha), excluir `DATABASE_SERVICE` e "Requests executed in background threads", deixar o usuário escolher quando houver mais de um candidato e **gravar a escolha no conjunto salvo**.
- **Confirmado pelo usuário:** no Dynatrace cada um dos 3 Deployments (`frete-hub`, `frete-b2c`, `frete-envvias`) é lido como **um process group e um serviço próprios**. Logo, tráfego, latência e erros **por aplicação** são possíveis pelo Dynatrace; o elo exato Deployment → process group → serviço (propriedades Kubernetes do PGI/PG) será validado na fase 2 contra um workload real.
- **Elo Deployment → serviço (confirmado no `frete-hub`):** o `displayName` do PROCESS_GROUP segue `<tecnologia> - <deployment> - <namespace>` (ex.: `SpringBoot - frete-hub - calculo-de-fretes-prd`); o PG não tem propriedades Kubernetes (só `detectedName`, tecnologias). Resolver por **nome exato** desse padrão (um `contains` pegaria `frete-hub-regionalizado`, que é outro Deployment) e seguir `toRelationships.runsOn` até os serviços. O PG do `frete-hub` liga 5 serviços e o `frete-hub-regionalizado` 2.
  - **Medido (24 h, `frete-hub`):** `FreteController` (WEB_SERVICE) 6.204.357 req / 526.186 4xx / 124 5xx / P99 pico 1,747 s; `CotacaoController` 5.739 req; `frete-hub-*` (WEB_REQUEST_SERVICE) só 3.645 req (3.624 são 4xx, o que o Spring não atende); `OpenApiWebMvcResource` e `SwaggerConfigResource` 4 req cada (P99 pico 3,6 s é ruído de 4 requisições).
  - **Logo, o WEB_REQUEST_SERVICE não é um superconjunto dos controllers: são pontos de entrada disjuntos.** Regras do relatório: (1) contagens (requisições, 4xx, 5xx) = **soma de todos os pontos de entrada** do PG (`WEB_REQUEST_SERVICE` e `WEB_SERVICE`, ...), excluindo `DATABASE_SERVICE`, "Requests executed in background threads" e consumidores; (2) **Max RPS** = pico da série **somada** (resolução de 1 min; nunca soma de picos); (3) **P99 de pico** = máximo apenas entre serviços com fatia mínima do tráfego (proposta: ≥ 1% das requisições do PG), mostrando qual serviço o gerou; sem esse piso, serviços com 4 requisições dominam o pico; (4) o piso de 1% é configurável.
  - O P99 de pico do `FreteController` (1,747 s) é muito próximo do 1,769 s do relatório de referência para o `frete-hub`: forte indício de que o exemplo vinha desse serviço.
  `pgi: 70` (instâncias, inclui pods antigos): usar sempre a série agregada do serviço, nunca por pod.
- **Soma medida (últimas 24 h, 5 serviços do `frete-hub`):** 6.756.657 requisições, 585.631 de 4xx (8,7%), 162 de 5xx, **Max RPS 265,8** (série somada, resolução de 1 min: 1.440 pontos), P99 de pico 1,747 s (`FreteController`). A consulta de soma e de pico exige `splitBy()` sem dimensão (`:splitBy():sum`); `:sum` sozinho não agrega as séries. **Ainda não validado** contra o relatório de referência (1.170.869 / 740 RPS / 1,769 s / 78.883 / 0): falta a janela exata dele.
- **Janela personalizada validada** (10/11 10:22:30 → 11/11 02:00, -03:00, 15 h 37 min): total 5.400.807, 4xx 462.050 (8,6%), 5xx 49, Max RPS 265,8 (938 pontos de 1 min), P99 pico 1,495 s (`FreteController`). **Não bate com o relatório de referência** (1.170.869 / 740 RPS / 1,769 s / 78.883 / 0): o exemplo tem menos requisições e pico de RPS ~3× maior, então vem de outro período ou de uma fonte que soma o ingress/namespace (hipótese: as 3 aplicações têm valores idênticos). O relatório novo mostra a **fonte e a janela** em cada coluna e não promete igualar relatórios antigos. Mecânica (resolução por process group, `splitBy()`, janela, picos) validada; equivalência com o exemplo não.
- **Divergência explicada:** o relatório de referência foi coletado num **evento de stress test de meses atrás** (informado pelo usuário), por isso os valores não batem com os atuais. A validação numérica contra ele não é possível; vale a mecânica validada acima.
- Os valores idênticos entre `frete-hub`, `frete-b2c` e `frete-envvias` no relatório de referência não vêm do Dynatrace (que separa por serviço): a fonte daquele relatório era compartilhada (ingress/gateway/namespace). A coleta do Prometheus (script da fase 1) ainda não foi rodada e vai dizer qual.

## 7.2 Fase 2 — o que foi implementado (backend, sem tela)

| Peça | Onde | Observação |
|---|---|---|
| Janela e resolução | `internal/monitoring/report/window.go` | valida (5 min a 400 dias, fim no futuro cortado), escolhe a resolução pela idade do INÍCIO da janela (1 min ≈ 14 d, 5 min ≈ 28 d, 1 h ≈ 400 d; sobe até caber em 3000 pontos) e marca "pico aproximado" quando > 1 min |
| Agregação | `report/traffic.go` | soma dos pontos de entrada; Max RPS = pico da série **somada**; P99 de pico só de serviços com ≥ 1% das requisições; "sem dado" é nulo, nunca zero |
| Dynatrace | `internal/dynatrace/report_queries.go` | `ResolveEntryServices` (process group de nome exato `<tec> - <deployment> - <namespace>` → `runsOn` → só `WEB_REQUEST_SERVICE`/`WEB_SERVICE`/`RPC_SERVICE`, sem banco nem "background threads") e `QueryServiceSeries` (preserva nulos) |
| Consumo | `report/usage.go` | CPU e memória em **% do request e % do limit** (máx/mín/média), média entre os containers dos pods do Deployment; seletor de pod estrito (não casa `frete-hub-regionalizado`); limite ausente = sem dado |
| Coletor | `report/collect.go` | itens em paralelo (4), timeout por item, **falha por fonte não derruba a linha**, selos pelos limites do exemplo (P99 > 1 s vermelho; 4xx > 0 aviso; 5xx > 0 vermelho) |
| Rotas | `handlers/monitoring_reports.go` | `POST /api/v1/monitoring/reports` (job em memória, 400 para pedido inválido), `GET /:id` (andamento + resultado), `DELETE /:id` (cancela); relatório isolado por usuário; só leitura |

Suposições assumidas (ajustáveis): janela máxima 400 dias; CPU/Mem nos dois modos (request e limit); um relatório aceita itens de vários clusters; limites dos selos inferidos do relatório de referência. **Fora desta fase:** coluna "Pico Nodepool" (definição pendente), persistência/histórico e exportação (fase 4), SSE (a tela faz polling do `GET`).

## 7.3 Primeira execução real (11/11/2026, `calculo-de-fretes-prd`, janela de 6 h)

- **Dynatrace OK** para `frete-hub` (2.037.333 req, 265,8 RPS, P99 pico 1,043 s no `FreteController`, 4xx 190.217, 5xx 38) e `frete-envvias` (220.158 req, 28,0 RPS, P99 0,274 s, 4xx 1.469); o serviço `BACKGROUND_ACTIVITY` do `frete-envvias` foi corretamente ignorado.
- **Prometheus falhou (HTTP 422, "found duplicate series … on the right hand-side")**: o `kube-state-metrics` tem séries duplicadas (duas instâncias com `instance` diferente para o mesmo pod/container). Corrigido: os dois lados da divisão usam `max by (pod,container)`, sem `group_left`. **A revalidar** rodando de novo (o teste só confere o texto da consulta).
- **`frete-b2c` não casa com nenhum process group** pelo padrão `<tecnologia> - <deployment> - <namespace>`: o nome do PG dele é diferente. Investigar com `probe-dt-pg.sh b2c` e, se necessário, permitir **mapeamento manual** (deployment → process group) salvo no conjunto do relatório.
- **Validado (3ª execução, 11/11 09:07→15:07Z):** com `max by (pod,container)` nos dois lados da divisão, CPU e memória (% do request e % do limit) voltam para as três aplicações; falha numa fonte não derruba a linha (o `frete-b2c` sem Dynatrace mostrou o consumo). `frete-hub`: 2.120.803 req, 158,7 RPS, P99 1,501 s, 9 de 5xx; `frete-envvias`: 182.110 req, 31,9 RPS, P99 0,208 s.
- **`frete-b2c`:** o namespace `calculo-de-fretes-prd` tem PGs `freight-bff`, `freight-job`, `frete-aggregate-sku-mktp`, `frete-consumer-sku`, `frete-envvias`, `frete-hub`, `frete-hub-regionalizado`, `frete-marketplace` e mais 3 (lista cortada em 8; agora o erro lista até 60). Nenhum se chama `frete-b2c`: ou ele está entre os 3 cortados com outro nome, ou não é instrumentado pelo OneAgent.
- **Causa do `frete-b2c` (informada pelo usuário):** a entidade no Dynatrace se chama **`Frete.WebApi.dll frete-b*c-*`** (processo .NET). O nome não segue `<tecnologia> - <deployment> - <namespace>` e o `2` aparece como curinga `*`. **Correção:** a resolução agora tem 3 etapas — (1) nome do process group, (2) **metadados Kubernetes dos processos do cluster** (`KUBERNETES_NAMESPACE` + `KUBERNETES_BASE_POD_NAME`, cacheado 10 min por cluster; nome do cluster normalizado sem `-admin`), (3) **tags** do processo (sugestão do usuário) — e o `*` do Dynatrace casa com uma sequência de dígitos (`frete-b*c` = `frete-b2c`). Cada resolução registra em nota como foi feita; a de tag avisa que pode agrupar mais de um Deployment. Se nada casa, o erro lista os process groups parecidos e os Deployments com processo monitorado no namespace. **A revalidar** contra o cluster real.
- **Validado (4ª execução, 11/11 09:24→15:24Z, cluster `akspriv-oferta-prd-admin`):** os 3 Deployments resolvem e trazem Dynatrace + Prometheus sem erros. `frete-b2c` resolveu pelos **metadados Kubernetes** (`Frete.WebApi`, serviço único; o `CUSTOM_SERVICE` do driver MongoDB foi ignorado): 709.077 req, 52,9 RPS, P99 pico 2,126 s, 4xx 27.900, 5xx 135; CPU 8,9% do request / 4,9% do limit, memória 52% / 29,3%. `frete-hub` (2.187.668 req, 158,7 RPS, P99 1,501 s) e `frete-envvias` (180.634 req, 31,9 RPS, P99 0,2 s) seguem iguais, resolvidos pelo nome do process group.
- O `-admin` do nome do contexto não é a causa do 422: a consulta chegou ao Prometheus e executou.

## 7.4 Percentis escolhidos pelo operador

Os percentis de latência são **P50, P95 e P99, escolhidos na tela** (padrão: só P99). O pedido leva `percentiles: [50, 95, 99]` (vazio = `[99]`; fora desse conjunto, 400) e o resultado devolve quais foram coletados. Só os escolhidos geram consulta ao Dynatrace (`percentile(50|95|99)`, resolução de no mínimo 5 min, µs → ms) e aparecem na tabela; o valor de cada um é o **pico na janela**, sem serviços com menos de 1% das requisições. O selo 🔴 por latência continua sendo só do P99 (> 1 s).

## 8. Decisões pendentes

1. **Fonte de RPS, latência e erros:** Istio, ingress-nginx, Dynatrace, ou varia por cluster?
2. **CPU % e Mem %:** relativos ao request ou ao limit?
3. **Pico Nodepool:** soma do consumo dos pods da aplicação no pool, ou uso do pool inteiro no pico? Com dois pools (ex.: `mp-inventario`), lista cada um?
4. **Limites dos selos:** 🔴 para P99 acima de quanto? ⚠️ para qualquer 4xx ou acima de uma taxa? 🔴 para qualquer 5xx?
5. **Escopo:** um cluster por relatório ou vários no mesmo?
6. **Janela máxima:** 30 dias é suficiente? (limitada pela retenção das fontes.)

## 9. Riscos

- **Volume de consultas** em janelas longas com muitas aplicações: mitigado pela consulta agrupada por namespace, passo automático e cache.
- **Métrica compartilhada** entre aplicações (seção 4): marcar, não esconder.
- **Percentis** não se agregam: usar máximo da série ou o histograma, nunca média de P99.
- **Retenção** menor que a janela pedida: avisar por coluna.
- **Limites do Dynatrace** (taxa de chamadas, entidades): coleta com limite de simultaneidade e novas tentativas curtas.
- **Cardinalidade** no Prometheus/Mimir: agrupar só pelos rótulos necessários.
