# Plano: Integração com Spinnaker (detecção de rollback)

**Status**: ✅ **CONCLUÍDA — Fases 1-4 e os 7 itens da seção 9 ("outras opções") implementados, testados e validados ao vivo.** Branch `feat/spinnaker-integracao-fase1` (ainda não mesclada — ver seção 11 para o estado exato de cada peça e os bugs reais encontrados durante a implementação, incluindo 3 correções pós-entrega reportadas pelo usuário). **Revisão 13** (2026-08-18) — revisões 1-12 abaixo documentam a investigação/design original e permanecem como registro histórico; ver seção 11 para o que realmente foi construído.

## 0. O que mudou desde a Revisão 3

A Rev. 3 tinha confirmado login/API reais, mas o sinal de rollback (`manifestArtifact.reference` contendo `helm-rollback.yaml`) era "inferência estrutural, não observada ao vivo" — nenhuma das ~10 execuções recentes de `ofertalogistica` era rollback. O usuário then apontou uma execução real de rollback em **PRD** (`spinnaker-prd.viavarejo.com.br/.../executions/01KXNMVR0JYFPR6Q3ZAC6NNZPQ`), e avisou que **PRD e HLG se comportam diferente no Spinnaker** — foi buscar essa execução real e a diferença é maior do que "só um detalhe": **PRD e HLG são duas instâncias Spinnaker inteiramente separadas, com pipelines configurados diferentes entre si.**

### 0.1. Correção da Rev. 4 — HLG também tem rollback, só que automático (não o mesmo mecanismo)

A Rev. 4 concluiu (com evidência real, mas incompleta) que "`rollback-aks-global` não existe configurado em HLG" e tratou isso como "rollback não é detectável em HLG". **O usuário corrigiu**: HLG tem rollback sim, mas é uma **ação automática disparada em caso de erro no deploy** (ex: falha por falta de recursos/rollout que não consegue subir) — não o fluxo manual e deliberado que vimos em PRD via o pipeline dedicado `rollback-aks-global`.

Inspecionei a config do stage `deploy-helm` (o mesmo pipeline `deploy-aks-global` usado em HLG) em busca de alguma branch condicional de "se falhar, reverte" — **não achei nenhuma no nível do pipeline Spinnaker** (`failPipeline: false`, sem stage seguinte de rollback). Isso é evidência (não prova) de que o rollback automático de HLG provavelmente acontece **dentro do próprio Job Helm** (ex: `helm upgrade --atomic --cleanup-on-fail`, recurso nativo do Helm que reverte sozinho se o rollout falhar) — um mecanismo que, se for esse mesmo, **não deixaria o mesmo tipo de rastro que vimos no rollback manual do PRD** (o `manifestArtifact.reference` continuaria sendo `helm-deploy.yaml`, nunca mudaria pra `helm-rollback.yaml`, porque do ponto de vista do Spinnaker foi só um deploy normal — o que aconteceu por dentro do Job é opaco pra ele).

**Não confirmei isso com um exemplo real** (pedi um `executionId` de um rollback automático em HLG; o usuário indicou que o foco agora é PRD, que já está confirmado, e deixou a investigação de HLG pra depois). Por isso: **esta integração, na primeira entrega, cobre com confiança real só o cenário de PRD** (rollback manual/deliberado via pipeline dedicado). Pra HLG, a resposta correta por ora é "não determinado" — não "não houve rollback" (isso seria falso, já sabemos que HLG tem rollback, só não sabemos ainda como enxergá-lo) e não "não aplicável" (isso sugeriria que HLG não tem rollback, o que também é falso). O mecanismo de rollback automático de HLG fica marcado como trabalho futuro (seção 6), não bloqueia a Fase 1 focada em PRD.

### 0.2. Uma falha real de HLG examinada — reforça a hipótese 0.1, mas não fecha a dúvida

O usuário passou uma execução real de falha em HLG (aplicação `logreversa`, `01KYW3G5NKQHCNYGHQTQCCZH7G`, `nameApp: dat-documento-vendas-api`, ambiente `sit` — confirma de novo que `sit` roda no Gate de HLG, não um terceiro Gate). Achados:

- **`pipelineConfigs` de `logreversa` em HLG tem só 1 pipeline: `deploy-aks-global`** — nem `job-configmap-rollout`, nem canary, nem rollback. 2ª aplicação real confirmando o padrão "HLG não tem pipeline de rollback dedicado" (já visto em `ofertalogistica`).
- **A execução inteira ficou `status: SUCCEEDED`** mesmo com o stage `deploy-helm` em `FAILED_CONTINUE` — confirma ao vivo o que a config já sugeria (`failPipeline: false`): uma falha de deploy nunca marca a execução inteira como falha, e o pipeline segue pro `xl-release-callback` de qualquer forma.
- **O Job do Helm rodou por ~10 minutos e o container saiu com `exitCode: 1`** (erro genérico — a causa raiz real, tipo "falta de recursos", só apareceria nos logs do próprio pod, que a API do Spinnaker não devolve, só o resumo de status do container).
- **A "exception" registrada no stage não é sobre a causa do erro, é sobre uma limitação da plataforma**: quando o stage bateu o timeout de 20min (`stageTimeoutMs`) e o Spinnaker tentou cancelar o Job, o `clouddriver` (componente do provider Kubernetes) respondeu `500 — "cancelJob is not implemented for the Kubernetes provider"`. Ou seja, nem o próprio Spinnaker consegue limpar um Job travado no provider Kubernetes — é uma limitação conhecida do Spinnaker OSS, não algo específico desta empresa.
- **Nenhum sinal de rollback nesta execução** — `manifestArtifact.reference` continua `helm-deploy.yaml` do início ao fim, nenhuma segunda execução/stage de rollback aparece. **Confirma a hipótese 0.1 por ausência**: se um rollback automático aconteceu depois dessa falha, ele não deixou rastro visível nesta mesma execução nem gerou uma execução-irmã detectável na busca de `logreversa`.

**O que isto não prova**: não sei se, depois dessa falha, alguém rolou manualmente de volta, se o Helm reverteu sozinho por dentro do container (o `exitCode: 1` pode ser tanto "helm upgrade falhou e ficou quebrado" quanto "helm rollback automático rodou e SAIU com erro mesmo assim"), ou se a aplicação simplesmente ficou quebrada até correção manual fora do Spinnaker. Confirma a hipótese da seção 0.1 (rollback automático de HLG, se existir, não é visível via Gate API) mas não fecha a dúvida — só uma investigação do lado do cluster (`kubectl describe`/logs do pod `helm-deploy-d49328d9-...` em `spinnaker-hlg`/K8s, ou `helm history` do release em `dat-sit`) confirmaria o que realmente aconteceu depois dessa falha. Fora do escopo desta integração com Spinnaker — ver seção 6.

### 0.3. JSON bruto da mesma execução, examinado na íntegra — 3 achados novos + 1 alerta

O usuário colou o JSON completo dessa mesma execução (não truncado, diferente do que eu tinha extraído via `python` antes). Isso revelou estrutura que eu não tinha visto:

1. **`trigger.parameters` é uma fonte de dado mais limpa que `trigger.payload`** — existe um objeto separado, com labels humanos, provavelmente o que o próprio Deck mostra no painel "Trigger" da execução:
   ```json
   "trigger": {
     "parameters": {
       "Application Version": "0.0.2-3",
       "Application Environment": "sit",
       "Application K8S Namespace": "dat-sit",
       "Application SN Change Number": "",
       "Application Name": "dat-documento-vendas-api",
       "Application Github Repository": "github.com/casas-bahia/dat-documento-vendas-api.git",
       "Application Github Repo Branch": "release/0.0.2",
       "Application Nexus URL": "..."
     }
   }
   ```
   `"Application SN Change Number"` é o nome exato do campo pra CHG aqui — **substitui `trigger.payload.serviceNowTaskNumber` como fonte primária** na seção 5 (mais estável, feito pra exibição, não um parâmetro de implementação interna do Job).

2. **`trigger.source` = nome da conta/cluster que disparou o webhook** (`"akspriv-logreversa-hlg"` neste caso) — chave de correlação **muito melhor** que tentar casar por `nameApp`+`namespace` como a seção 4 propunha: o cluster já vem identificado direto, sem inferência.

3. **`pipelineConfigId`** (`"5f982ea8-b644-4c03-b589-0294fc607f23"`) — identificador estável do pipeline, mais confiável que comparar a string do `name` (que pode ser renomeado) — melhor chave pra decidir "isto é uma execução do pipeline de rollback" do que comparar nome textual.

4. **Alerta sobre a causa raiz aparente**: dentro de `DEPLOYMENT_PARAMETERS` (um dump bruto de env vars de CI, ~100 chaves, a maioria irrelevante pra este Job específico) existe `"EXCEPTION_TEXT": "Image scan failed for ... — one or more vulnerabilities triggered the active prevention policy"`. **Isso pode ser a causa raiz real** (bloqueio de scan de vulnerabilidade da imagem, não falta de recursos) **ou pode ser um valor obsoleto carregado de outra etapa do pipeline de CI e reaproveitado sem limpeza** — o campo está dentro de um blob genérico que mistura configuração de lint/conftest/hadolint sem relação direta com esta execução. Não dá pra confiar nele sem confirmação. **Pergunta em aberto pro usuário**: esse exemplo específico (falha com `exitCode: 1` depois de ~10min rodando, sem timeout de scheduling) bate com o cenário de "falta de recursos" que você descreveu, ou é uma classe de falha diferente (ex: bloqueio de segurança de imagem)? Isso importa porque scheduling/recursos insuficientes tipicamente aparece como pod `Pending` (nunca chega a rodar o container), enquanto aqui o container **rodou por ~10 minutos e depois saiu com erro** — padrão mais consistente com uma falha dentro do próprio script de deploy do que com falta de recursos no nó.

**Reforça, com mais uma peça de evidência**: o webhook de rollback descoberto antes (`/webhooks/webhook/rollback-akspriv-ofertalogistica-prd`) tem o sufixo `-prd` no nome — condizente com a hipótese de que o **webhook de rollback só existe registrado para contas PRD**, não HLG. Isso é uma explicação alternativa (não excludente da hipótese do Helm `--atomic` da seção 0.1) pra por que nenhuma das falhas de HLG examinadas mostrou rollback: pode não ser "o rollback aconteceu mas ficou invisível", pode ser "o gatilho automático de rollback simplesmente não está configurado pra contas não-PRD". As duas hipóteses continuam em aberto — só um exemplo real confirmado de rollback automático bem-sucedido em HLG resolve isso.

### 0.4. Confirmado pelo usuário: este exemplo É o cenário de "falta de recursos"

Perguntei diretamente (item 4 da seção 0.3) se essa falha específica batia com o cenário que o usuário tinha descrito. **Resposta: sim.** Isso resolve a dúvida sobre representatividade, mas com uma correção importante ao meu modelo mental: eu esperava que "falta de recursos" aparecesse como pod `Pending` (nunca chega a rodar o container) — o padrão real é diferente: **o container do Job `helm-deploy` roda normalmente por ~10 minutos e só falha depois** (`exitCode: 1`). Isso é consistente com `helm upgrade --wait` (ou equivalente) rodando dentro do container, esperando os pods do Deployment-alvo (não o Job em si) ficarem `Ready` — se esses pods não conseguem ser agendados por falta de recursos no cluster, o `--wait` do Helm estoura o próprio timeout interno primeiro (a falta de recursos afeta o workload que está sendo deployado, não o Job do Spinnaker que faz o deploy).

**O que ainda não está confirmado, apesar disso**: se um rollback automático de fato aconteceu depois dessa falha específica — só sabemos que essa falha é representativa da causa, não sabemos o desfecho. Continua valendo o que a seção 0.2 já apontava: só uma investigação do lado do cluster (`kubectl describe`/eventos do namespace `dat-sit` no cluster `akspriv-logreversa-hlg`, ou `helm history` do release) confirmaria se algo reverteu sozinho depois. Isso está dentro do alcance das ferramentas que esta própria aplicação já tem (Pods/Events/kubectl describe), mas é uma extensão explícita do escopo — ver pergunta ao usuário fora deste documento.

### 0.5. Investigação do lado do cluster — achado direto do log do deploy tool, contradiz a premissa da seção 0.1

Com autorização do usuário, investiguei o histórico Helm do release `dat-documento-vendas-api` no namespace `dat-sit` (cluster `akspriv-logreversa-hlg`):

```
v19   deployed     2026-07-27T12:41:58Z   ← último sucesso, nunca substituído
v20   failed       2026-07-31T12:48:59Z   ← a falha examinada nas seções 0.2-0.4
v21   failed       2026-07-31T12:59:13Z   ← segunda tentativa (mesmo evento — o job cria um 2º pod), também falhou
```

`v19` continua `deployed` até hoje — nenhuma revisão nova foi criada como "rollback" (Helm registraria isso como uma nova revisão, tipicamente com `DESCRIPTION: Rollback to X`). Ou seja: **o upgrade simplesmente falhou e nunca substituiu a versão anterior** — não houve uma ação de rollback explícita, o release antigo continuou sendo o "deployed" porque o novo nunca completou.

O usuário então apontou um pod real e recente no namespace `spinnaker` (`helm-deploy-28b50d2b-...`, execução de **hoje**, app `consulta-std-consumer`, ambiente `hlg`, bem-sucedida) pra eu ler o log do próprio script de deploy. A linha decisiva:

```
"First deploy detected or not in production environment. Removing '--atomic' flag"
```

**Isto é evidência direta, não inferência**: o script (`spinnaker-helm-deployer:1.9.2-1`) roda `helm upgrade --install` com `--atomic` **por padrão**, mas **remove esse parâmetro explicitamente quando o ambiente não é produção** (a condição no log combina "primeiro deploy" OU "não é produção" — pra este caso, `TARGET_ENV=hlg` bastou pra disparar a remoção). `--atomic` é o parâmetro do Helm que faz rollback automático quando o upgrade falha — **sem ele, uma falha de upgrade em HLG não tem NENHUM mecanismo do Helm pra reverter sozinho**, exatamente como o histórico de revisões acima mostrou na prática (v20/v21 falharam e ficaram `failed`, sem viraram rollback).

**Isso é o oposto do que a seção 0.1 assumia** (que HLG teria rollback automático, só que invisível pela API do Spinnaker). A evidência agora aponta pra: **HLG não tem rollback automático nenhum via Helm** — o que salvou a aplicação nesse caso específico não foi um rollback, foi o fato de o upgrade nunca ter "pegado" (o release antigo nunca foi substituído, então continuou rodando por padrão).

### 0.6. Reconciliado com o usuário — "não substituir" conta como rollback, e isso vira uma regra de detecção real

Perguntei diretamente se "não substituir a versão antiga" batia com o que o usuário quis dizer com "HLG tem rollback automático". **Confirmado: sim** — o efeito prático (app continua rodando a versão anterior, funcional) é o que importa pro usuário, independente do mecanismo técnico ser um "rollback" no sentido formal do Helm/Spinnaker.

**Isso não é só uma nota de esclarecimento — vira uma regra de detecção real e implementável pra HLG**, que a seção 0.1/seção 1 tratavam como "impossível de ver pela API". A mecânica completa, agora entendida ponta a ponta:

1. O script de deploy roda `helm upgrade --install --wait --timeout 10m` **sem `--atomic`** em ambientes não-PRD (seção 0.5).
2. Se o rollout não completa a tempo (pods novos não ficam `Ready` — no caso confirmado, por falta de recursos), o comando Helm falha e a execução Spinnaker fica `FAILED_CONTINUE`.
3. A estratégia `RollingUpdate` padrão do Kubernetes (não Helm, não Spinnaker) **nunca desliga os pods antigos até os novos ficarem prontos** — então a aplicação continua rodando a versão anterior o tempo todo, de forma automática, como efeito colateral da própria mecânica de rollout do Kubernetes.
4. Confirmado no histórico real do Helm: a release nunca avança pra uma revisão nova bem-sucedida (`v19` continuou `deployed`; `v20`/`v21` ficaram `failed`).

**Regra de detecção pra HLG (`is_rollback` deixa de ser sempre `null`)**: dado a execução mais recente pra um `nameApp`+`namespace`, se `execution.status` não é sucesso (`TERMINAL`/`FAILED_CONTINUE`/etc.) **e** a versão atualmente observada no K8s (já coletada via `DeploymentRegistry`) **não bate** com a versão que essa execução tentou implantar (`trigger.parameters["Application Version"]`) — **isso é rollback implícito**: `is_rollback: true`, `rollback_type: "implicit"`. Diferente do caso PRD (`rollback_type: "explicit"`, pipeline dedicado `rollback-aks-global`), mas igualmente real e detectável — **sem precisar inspecionar `--atomic`/logs de Job, só comparar a versão-alvo da execução falha contra a versão vigente já coletada**.

**Efeito nas seções seguintes**: `coverage: "partial"` (seção 0.1/seção 1) estava errado como conceito — não é que HLG tem cobertura parcial, é que tem uma **regra diferente** (implícita vs explícita), mas igualmente `"full"`. Ver ajuste no contrato (seção 5) e nas fases (seção 7).
## 1. PRD e HLG são Gates diferentes — confirmado, não é só uma questão de ambiente lógico

| | HLG | PRD |
|---|---|---|
| Domínio da UI (Deck) | `spinnaker-hlg.viavarejo.com.br` | `spinnaker-prd.viavarejo.com.br` |
| Domínio da API (Gate) | `spinnaker-hlg-api.viavarejo.com.br` | `spinnaker-prd-api.viavarejo.com.br` |
| Versão | 1.30.2 (Halyard) | 1.30.2 (Halyard) |
| Login | mesma matrícula/senha, `POST /login` idêntico — **testado e confirmado nos dois** | idem |
| Pipelines configurados pra `ofertalogistica` | `deploy-aks-global`, `job-configmap-rollout`, `poc-canary-ofertalogistica-hlg` (**3**) | `deploy-aks-global`, `job-configmap-rollout`, `deploy-aks-canary`, **`rollback-aks-global`** (**4**) |

**Achado concreto, não hipotético**: rodei `GET /applications/ofertalogistica/executions/search?pipelineName=rollback-aks-global` contra o Gate de HLG — retornou `[]` (nunca rodou). O mesmo pipeline em PRD tem pelo menos uma execução real, bem-sucedida. E `pipelineConfigs` de HLG **não lista `rollback-aks-global` entre os pipelines configurados**. **Correção da seção 0.1**: isso confirma que **o pipeline dedicado de rollback manual não existe em HLG** — mas não significa "HLG não tem rollback": o usuário confirmou que HLG tem um mecanismo **automático** de rollback (disparado em falha de deploy, ex: falta de recursos), só que por um caminho diferente, ainda não identificado por esta investigação (provavelmente dentro do próprio Job Helm, não como pipeline/stage Spinnaker separado — ver seção 0.1).

**Implicação de design, direta**: a integração precisa resolver **qual Gate consultar por ambiente** (mapa `env → gateBaseURL`, pelo menos `{hlg: spinnaker-hlg-api.viavarejo.com.br, prd: spinnaker-prd-api.viavarejo.com.br}`). A detecção via pipeline dedicado (`rollback-aks-global`) só é confiável pra PRD por ora — em HLG, a ausência desse pipeline não deve virar "não houve rollback" (falso, sabemos que há) nem "não aplicável" (também falso, o recurso existe, só não sabemos como detectá-lo ainda) — deve virar "não determinado nesta versão da integração". Ainda não sei se o padrão de PRD generaliza pra outras squads/aplicações (só temos 1 exemplo, `ofertalogistica`) — ver seção 6.

## 2. Rollback confirmado ao vivo — execução real, PRD

`01KXNMVR0JYFPR6Q3ZAC6NNZPQ`, pipeline `rollback-aks-global` (não é o mesmo pipeline do deploy normal — **é um pipeline próprio**, correção em relação à Rev. 3, que assumia que seria o mesmo pipeline com um manifesto alternativo):

```
name: "rollback-aks-global"        ← pipeline dedicado, distinto de "deploy-aks-global"
status: SUCCEEDED
trigger.payload.nameApp: "estoque-margem-seguranca"
trigger.payload.namespace: "oferta-estoque-1p-api-internas-prd"
trigger.payload.version / tag: "3.3.0-4"
trigger.payload.targetEnv: "prd"

stages:
  - runJobManifest "rollback"       status: SKIPPED   (referência não resolvida — branch condicional não usada)
  - runJobManifest "rollback-helm"  status: SUCCEEDED
      context.manifestArtifact.reference =
        "https://nexus.viavarejo.com.br/repository/spinnaker/spinnaker-helm-deployer/helm-rollback.yaml"
  - runJobManifest "xl-release-callback"  status: SUCCEEDED
```

**Dois sinais independentes e agora confirmados, não mais hipotéticos**:
1. **`execution.name == "rollback-aks-global"`** — sinal primário, mais simples e robusto (identidade do pipeline).
2. Stage `rollback-helm` com `context.manifestArtifact.reference` contendo `helm-rollback.yaml` — sinal secundário/corroborativo, útil se o nome do pipeline variar entre squads (não confirmado se varia — só vimos uma squad).

Regra de detecção revisada: **é rollback se a execução que produziu a versão vigente veio de um pipeline cujo nome bate um padrão de rollback conhecido** (`rollback-aks-global` confirmado; outras squads podem nomear diferente — precisa de descoberta, não hardcode de um nome só) **e/ou** tem um stage `runJobManifest` bem-sucedido cujo `manifestArtifact.reference` contém `rollback`. Usar os dois em conjunto é mais robusto que qualquer um isolado.

## 3. Login — confirmado nos dois ambientes, mesmo mecanismo

Sem mudança na mecânica em relação à Rev. 3 (`POST /login` form-urlencoded, sem CSRF, sessão por cookie, credencial via `browser.LoadSSOCredentials` já existente) — só confirmando que **a mesma matrícula/senha funciona igual nos dois Gates** (testado: login OK em `spinnaker-hlg-api` e `spinnaker-prd-api` na mesma sessão de verificação). Não precisa de credencial por ambiente.

## 4. Modelo de correlação — refinado na seção 0.3 com uma chave melhor

Confirma-se de novo com a execução de PRD: `ofertalogistica` (a "application" Spinnaker) segue agrupando múltiplos microsserviços da squad — `estoque-margem-seguranca` é um `nameApp` que nunca tinha aparecido nas execuções de HLG examinadas antes.

**Atualização da seção 0.3**: em vez de casar por `trigger.payload.nameApp`/`namespace`/`version` (Rev. 3-7, ainda válido como fallback), a chave primária de correlação passa a ser **`trigger.source`** — o nome da conta/cluster que disparou o webhook (`"akspriv-logreversa-hlg"`, `"akspriv-ofertalogistica-hlg"`, etc.) já vem pronto na execução, sem precisar inferir cluster a partir de namespace/account do stage. `trigger.parameters` (objeto com labels humanos tipo `"Application Name"`, `"Application K8S Namespace"`, `"Application Version"`) complementa como fonte mais estável que `trigger.payload` pros demais campos de match.

## 5. Contrato de dados — expandido com a priorização do usuário (seção 8)

Contrato final do endpoint `GET /api/v1/spinnaker/rollback-status`, já incorporando os campos pedidos na seção 8 e o achado da seção 8.1 (CHG que falhou = CHG do rollback):

| Campo | Fonte real confirmada | Corresponde a (pedido do usuário) |
|---|---|---|
| `last_chg_applied` | `trigger.parameters["Application SN Change Number"]` (fonte primária, achado da seção 0.3) — fallback `trigger.payload.serviceNowTaskNumber` — da execução que produziu a versão vigente (deploy ou rollback, a que for mais recente) | "última CHG aplicada" |
| `pipeline_executed_at` | `execution.startTime` (epoch ms; `buildTime` como fallback se `startTime` ausente) | "data e hora da execução da pipeline" |
| `execution_status` | `execution.status` (`SUCCEEDED`/`TERMINAL`/`FAILED_CONTINUE`/`CANCELED`/...) | "status da execução" |
| `is_rollback` | tri-state (`true`/`false`/`null`, `null` só em falha/timeout de consulta) — **agora `"full"` nos dois ambientes** (seção 0.6): PRD via pipeline dedicado (seção 2), HLG via comparação versão-alvo-da-execução-falha vs. versão vigente no K8s | — |
| `rollback_type` | novo campo — `"explicit"` (PRD, pipeline `rollback-aks-global`) \| `"implicit"` (HLG, upgrade falhou e a versão anterior nunca foi substituída — seção 0.6) | transparência de qual regra detectou o rollback |
| `rollback_started_at` / `rollback_ended_at` | explícito (PRD): `execution.startTime`/`execution.endTime` da execução de rollback. Implícito (HLG): `execution.startTime`/`execution.endTime` **da execução que falhou** (não existe uma "execução de rollback" separada — o intervalo é o da própria tentativa de upgrade que não completou) | "data, hora início e fim da execução do rollback" |
| `failed_chg` | explícito (PRD): `trigger.parameters["Application SN Change Number"]` da própria execução de rollback (seção 8.1 — mesma CHG do deploy revertido, 1 exemplo confirmado). Implícito (HLG): `trigger.parameters["Application SN Change Number"]` da execução que falhou | "número da CHG que falhou" |
| `rollback_pipeline_name` | só preenchido quando `rollback_type == "explicit"` — nome do pipeline (`"rollback-aks-global"`) | transparência de depuração |
| `coverage` | **removido** (seção 0.6) — era baseado na premissa errada de que HLG não tinha cobertura; substituído por `rollback_type` | — |
| `spinnaker_execution_url` | link direto pra execução no Deck (mesmo formato da URL que o usuário passou) | suporta a sugestão 9.2 (deep-link) |

### 5.1. CHG que falhou = a mesma CHG do deploy revertido (confirmado com 1 exemplo real, não geral)

Verifiquei contra o rollback real do PRD (`01KXNMVR0JYFPR6Q3ZAC6NNZPQ`, CHG `CHG0475290`): achei o deploy imediatamente anterior pra mesma `nameApp`+`namespace` (`01KXN5FP7NKW753Q4AST8GMG6X`, ~4h30 antes, `status: SUCCEEDED`) — **ele referencia a mesma CHG `CHG0475290`**. Ou seja, neste exemplo, deploy e rollback compartilham a mesma CHG (faz sentido: é a mesma mudança/change record, só que revertida depois). Isso simplifica a implementação: **não é necessário buscar a execução de deploy anterior** — o campo `serviceNowTaskNumber` da própria execução de rollback já é a resposta certa pra `failed_chg`, pelo menos neste padrão observado. **Só um exemplo real confirmado** — se outra squad/aplicação abrir uma CHG separada especificamente pro rollback, esse pressuposto quebra; marcar como item a revisitar na Fase 4 (seção 6, generalização entre squads).

## 6. O que falta confirmar antes da Fase 1 (revisado)

1. ~~Achar um rollback real~~ — **feito**, seção 2 (PRD).
2. **`rollback-aks-global` é convenção company-wide ou específico da squad SRE Logística?** Só temos uma squad/aplicação observada. Se cada squad nomeia pipelines diferente, a detecção por nome de pipeline sozinha não escala — precisaria de um critério mais genérico (ex: "qualquer pipeline cujo nome comece com `rollback-`" mais o sinal de `manifestArtifact.reference`, já que ele é reaproveitado do mesmo template Nexus `spinnaker-helm-deployer`). Verificar contra pelo menos uma 2ª aplicação/squad antes de generalizar a regra.
3. **Confirmar `nameApp` == `DeploymentRecord.AppName`** — ainda não comparado campo-a-campo (mesmo item da Rev. 3).
4. **TTL/expiração real do cookie `SESSION`** — ainda não medido.
5. **Squad → application Spinnaker** — ainda sem confirmação de que `devops.k8s.io/squad` já coletado bate 1:1 com o nome da application (`ofertalogistica`).

~~Mecanismo de rollback automático de HLG~~ — **resolvido na seção 0.6**: não precisa de fonte de dado fora da API do Spinnaker, é detectável comparando a versão-alvo de uma execução falha contra a versão vigente no K8s (já coletada). Não bloqueia mais nada.

~~Seletor de "project" Spinnaker fora de escopo~~ — **voltou ao escopo, especificado pelo usuário na seção 10.** `GET /projects` testado ao vivo e confirma que resolve, de quebra, boa parte do item 5 acima: cada projeto já lista suas `applications` Spinnaker (`config.applications`) — ver seção 10.

## 7. Plano de fases

### Fase 1 — Pacote `internal/spinnaker/`
- `client.go`: `NewClient(gateBaseURL string)` + `Login(ctx, ssoProfileDir string) error` (`browser.LoadSSOCredentials` + `POST /login`, `http.CookieJar`).
- **Mapa de ambiente → Gate**: `GateURLForEnv(env string) string` — hardcoded inicialmente pros 2 confirmados (`hlg`, `prd`); revisar quando/se existir `sit`/outros ambientes com Gate próprio (não confirmado — as execuções de `sit` vistas na Rev. 3 apareceram dentro do Gate de HLG, não um terceiro Gate; então por ora só 2 hosts).
- `executions.go`: `SearchExecutions(ctx, application, pipelineName string, opts) ([]Execution, error)`.
- `rollback.go`: `DetectRollback(executions []Execution, currentLiveVersion, targetNamespace, env string) *RollbackInfo` — duas regras (seção 0.6): (a) **explícita** — procura execução com pipeline/manifestArtifact de rollback (seção 2), usado quando existir (hoje, PRD); (b) **implícita** — acha a execução mais recente pra esse `nameApp`/`namespace` com `status` não-sucesso cuja versão-alvo (`trigger.parameters["Application Version"]`) ≠ `currentLiveVersion` → `is_rollback: true, rollback_type: "implicit"`. Roda (a) primeiro, cai pra (b) se não achar pipeline dedicado — não depende de `env` estar fixo em uma lista, generaliza sozinho se outro ambiente também ganhar pipeline dedicado no futuro.
- Cache de sessão por (usuário, ambiente) — chave dupla agora, não só por usuário (Rev. 3 previa só por usuário, mas com 2 Gates a sessão de um não vale no outro).

### Fase 2 — Handler + rota
- **Recomendado direto (não só "sugestão" — ver seção 9.1)**: implementar como endpoint em lote desde o início — `GET /api/v1/spinnaker/rollout-status/batch?cluster=&namespace=&env=` — evita retrabalho de trocar de per-deployment pra batch depois que a Fase 3 (lista com N cards) já estiver no ar. `env` decide o Gate; a lista de `applications` a consultar vem do projeto selecionado no perfil do usuário (seção 10), não de um parâmetro por request.

### Fase 3 — Frontend (local corrigido: `DeploymentsTab.tsx`, não `GitHubReleasesTab.tsx`)
Revisões anteriores erraram o local — a especificação real do usuário (seção 8) é sobre a aba **Deployments**, não GitHub Releases. Ver seção 8 para o desenho completo.

### Fase 2.5 — Configuração no Perfil do Usuário (login, URLs, seletor de projeto)
Ver seção 10 para o desenho completo. Entra antes da Fase 3 porque o badge/modal da Fase 3 depende do projeto selecionado pra saber quais `applications` Spinnaker consultar.

### Fase 4 (depois de uso real) — generalizar entre squads
Só depois de validar a Fase 3 com `ofertalogistica`/PRD: testar contra pelo menos uma 2ª squad/projeto pra resolver os itens 2/3 da seção 6 e a generalização da seção 5.1.

## 10. Configuração no Perfil do Usuário — login, URLs base, seletor de projeto (pedido do usuário)

### 10.1. Três campos novos, mesmo espírito do Perfil SSO já existente

O usuário pediu um item a mais no perfil (mesma área de `SSOProfileModal.tsx`, onde `email`/`matrícula`/senha já são configurados hoje pra ServiceNow/Teams):

1. **Tipo de login**: `"email"` \| `"matricula"` — decide qual campo do Perfil SSO já existente (`sso_profile.json`) vira o `username` no `POST /login` (seção 3). **Não** reaproveita `BrowserConfig.SSOLoginIdentifier` (`internal/browser/sso_config.go`) — aquele campo é especificamente pra auto-preencher o formulário do Azure AD via browser (ServiceNow/Teams), semanticamente diferente do login direto por formulário do Spinnaker; podem ser escolhas independentes (ex: matrícula pro Spinnaker, email pro Azure AD via browser).
2. **URL base HLG** e **URL base PRD** — os domínios do **Deck** (UI), não do Gate (API) diretamente: `https://spinnaker-hlg.viavarejo.com.br/` e `https://spinnaker-prd.viavarejo.com.br/`, exatamente como o usuário passou. **Deliberado**: guardar a URL do Deck, não do Gate — o backend resolve a URL real do Gate sozinho, buscando `{deckURL}/settings.js` e extraindo `var gateHost = '...'` (mesmo mecanismo usado nesta investigação pra descobrir `spinnaker-hlg-api.viavarejo.com.br`/`spinnaker-prd-api.viavarejo.com.br` sem precisar adivinhar ou hardcodear o sufixo `-api`). Cache em memória (o `gateHost` não muda em runtime).

### 10.2. Seletor de projeto — confirmado ao vivo, `GET /projects`

Testei contra o Gate de HLG autenticado: **existe endpoint de projetos**, confirmado com dado real:

```
GET /projects          → lista todos os projetos, cada um com "id", "name", "email", "config.applications" (lista de applications Spinnaker do projeto), "config.clusters" (contas/clusters)
GET /projects/{name}   → um projeto específico (nome com espaço funciona sem URL-encode, e também funciona URL-encoded)
```

Contra o projeto real "SRE Logistica" (o mesmo da URL original do usuário):
```json
{
  "id": "45a5e316-525c-43b8-8fc7-2af8f3616ea4",
  "name": "SRE Logistica",
  "config": {
    "applications": ["entregamais","envias","envvias","logreversa","oferta","tracking",
                      "abastecimento","wms","ofertalogistica","faturamento","tms","plataforma"],
    "clusters": [{"account": "akspriv-tracking-hlg", ...}, {"account": "akspriv-logreversa-hlg", ...}, ...]
  }
}
```

**Achado que resolve, de quebra, o item 5 da seção 6**: um projeto (grupo tipo squad) agrupa **múltiplas** "applications" Spinnaker (`ofertalogistica` e `logreversa`, que já investigamos, são as DUAS do mesmo projeto "SRE Logistica" — não são a mesma coisa, é um nível de hierarquia que eu não tinha mapeado até agora: **Project → Applications → nameApp (microsserviço)**, três níveis, não dois). Selecionar um projeto na UI já entrega a lista completa de `applications` a consultar — não precisa mais de um `spinnakerApp` único hardcoded por request.

### 10.3. Desenho

**Backend**:
- `SpinnakerConfig` (`~/.k8s-hpa-manager/spinnaker_config.json`, mesmo padrão simples de `servicenow-browser.json`): `LoginIdentifier`, `HLGBaseURL`, `PRDBaseURL`, `SelectedProject` (persiste a última escolha do selectbox).
- `GET/POST /api/v1/spinnaker/config` — ler/gravar, mesmo padrão de `sso_profile.go`.
- `GET /api/v1/spinnaker/projects?env=hlg|prd` — login + `GET {gateURL}/projects`, devolve `[{id, name, applications}]` pro selectbox. Cache curto (10-30min — lista de projetos muda raramente).

**Frontend**: novo bloco dentro de `SSOProfileModal.tsx` (ou aba própria no mesmo modal, mesmo padrão de "Spinnaker" como seção adicional) — radio Email/Matrícula, 2 campos de URL, e um `SearchableSelect` (mesmo componente já usado nos seletores de Pod/Container do Kafka Test) pro projeto, alimentado por `GET /api/v1/spinnaker/projects`. Precisa de login/URLs salvos antes de conseguir buscar a lista — botão "Buscar projetos" ou fetch automático ao sair dos campos de URL.

O batch endpoint da Fase 2 passa a receber `spinnakerApplications []string` (resolvido a partir do `SelectedProject` salvo, via `config.applications`) em vez de um `spinnakerApp` único — pode consultar todas as applications do projeto de uma vez.

## 8. Especificação de UI do usuário — badge + modal em Deployments

Localização confirmada contra o código real (`internal/web/frontend/src/components/DeploymentsTab.tsx`), não inventada: a aba usa `SplitView` (lista de cards à esquerda + editor/detalhe à direita, mesmo padrão documentado no CLAUDE.md pra Deployments/DaemonSets/etc.) — dois pontos de inserção concretos já existentes no código:

- **Lista à esquerda** (`renderDeploymentList`, dentro do `.map` de cada card, por volta da linha 2644 onde já existem badges de severidade `!`): novo badge compacto, só informativo (sem abrir modal) — mostra o `last_chg_applied` (ex: `CHG0475290`) e um ícone de rollback (↺) quando `is_rollback === true`. Cor: neutro/azul quando deploy normal, âmbar/vermelho quando `is_rollback === true` (tooltip diferencia "explícito"/"implícito" via `rollback_type`), cinza quando `is_rollback === null` (falha/timeout de consulta) ou sem dado.
- **Painel de visualização à direita** (`rightTitleAction`, por volta da linha 2390 — mesmo local onde já vivem os botões "Análise Preditiva"/"Histórico de Análises"/"Comportamento"/Describe): novo botão-badge (mesmo estilo dos botões vizinhos, ex: `<Button variant="outline" size="sm">`) — **este é o gatilho do modal**, exatamente como pedido. Mesmo padrão já usado pelo botão "Comportamento" (`onClick={() => setBehaviorModalOpen(true)}`) — só que aqui abre `spinnakerModalOpen`.

**Modal novo** (`SpinnakerRolloutModal.tsx`, mesmo padrão dos demais modais da aba — `DialogContent`, cabeçalho com nome/namespace do deployment): exibe todos os campos do contrato da seção 5 — última CHG aplicada (com link pro ServiceNow via `serviceNowChgUrl`, já disponível na resposta do Spinnaker — baixo custo adicional), data/hora da execução, status da execução, e (se `is_rollback == true`) a seção de rollback com início/fim e a CHG que falhou, com um texto diferente conforme `rollback_type` ("revertido manualmente via pipeline dedicado" vs. "o deploy falhou e a versão anterior permaneceu ativa"). `is_rollback === null` (falha/timeout de consulta) mostra aviso de "não determinado", nunca implica "sem rollback".

## 9. Outras opções sugeridas (fora do pedido original, avaliar valor antes de implementar)

1. **Endpoint em lote, não por-deployment — importante pra performance, não é só "nice to have"**: a lista à esquerda pode ter dezenas de cards visíveis ao mesmo tempo; buscar `rollback-status` individualmente por deployment geraria N chamadas ao Gate por carregamento de tela. Proposta: `GET /api/v1/spinnaker/rollout-status/batch?cluster=&namespace=&env=` (seção 10.3) — faz uma busca de execuções por **cada application do projeto selecionado** (`config.applications`, seção 10.2 — várias `applications`, cada uma cobrindo múltiplos `nameApp`/deployments) e devolve um mapa `{ "nameApp/namespace": RolloutInfo }`, resolvido inteiramente client-side pela lista. Cache curto no backend (2-5min, mesmo padrão já documentado no CLAUDE.md pra chamadas de API cloud) evita rebater o Gate a cada navegação de aba.
2. **Deep-link "Ver no Spinnaker"** dentro do modal — mesmo padrão já usado pro deep-link "Ver Trace" do Dynatrace (`DynatraceContextPanel.tsx`): botão que abre `spinnaker_execution_url` numa nova aba, usando a sessão do próprio navegador do usuário (não passa pelo token/sessão do backend).
3. **Link direto pro ServiceNow da CHG** (`serviceNowChgUrl`, já vem pronto na resposta do Spinnaker) — clique abre a CHG direto, sem precisar copiar o número e buscar manualmente.
4. **Refresh manual + timestamp de "última verificação"** no modal (`checked_at`), mesmo padrão já usado em várias partes da app ("Último Scan") — evita implicar tempo real quando é uma consulta sob demanda, e dá controle pro usuário re-consultar sem esperar um poller.
5. **Histórico curto (últimas 3-5 execuções, não só a mais recente)** dentro do modal, numa lista compacta — útil pra ver o padrão recente (ex: "rollback seguido de novo deploy corrigido") sem precisar abrir o Spinnaker. Custo baixo: já é o mesmo dado que `executions/search` retorna, só não descartar o resto depois de achar a mais recente.
6. **Badge do painel esquerdo clicável também** (não só o do painel direito) — atalho pra abrir o modal direto da lista, sem precisar selecionar o deployment primeiro. O usuário só pediu o gatilho no painel direito; isso é uma extensão opcional, de baixo custo, já que o mesmo componente de badge pode ser reaproveitado nos dois lugares.
7. **Correlação no Health Check** (já cogitada nas revisões anteriores como Fase 4): um deployment com rollback recente (ex: últimas 24-48h) poderia virar um sinal de risco extra no Health Check, mesmo padrão da correlação K8s↔Dynatrace já existente (`internal/healthcheck/correlator.go`). Vale mais depois de validar a Fase 3 em uso real — não estimar esforço agora.

## Evidência desta revisão

Verificação ao vivo, autorizada pelo usuário, matrícula/senha nunca exibidas: login confirmado em **dois** Gates (`spinnaker-hlg-api.viavarejo.com.br` e `spinnaker-prd-api.viavarejo.com.br`) com a mesma credencial; `GET /pipelines/{id}` no Gate de PRD retornou a execução completa de rollback apontada pelo usuário (`01KXNMVR0JYFPR6Q3ZAC6NNZPQ`), confirmando pipeline dedicado `rollback-aks-global` e o stage `rollback-helm` com `manifestArtifact.reference` apontando pro `helm-rollback.yaml`; `pipelineConfigs` comparado entre os dois Gates confirmou que esse pipeline **não existe configurado em HLG** pra essa mesma aplicação (busca filtrada por `pipelineName=rollback-aks-global` em HLG retornou vazio). Nenhum dado sensível persistido em disco fora dos probes descartáveis usados pra este teste.

## 11. Implementação final — Fases 1-4 e seção 9 completas (Revisão 13)

Tudo que segue foi codificado, coberto por testes unitários (`go test -race`, todos passando a cada etapa) e **validado ao vivo** contra o Gate real do Spinnaker (HLG e PRD) e clusters K8s reais — nenhum item foi dado como concluído só por compilar. Branch `feat/spinnaker-integracao-fase1`.

### 11.1. Pacote `internal/spinnaker/` (Fase 1)

- `client.go`: `Client`/`WithSession`, `Login` via `browser.LoadSSOCredentials` + `POST /login` (cookie jar), `Config`/`LoadConfig`, `DeckURLForEnv`, `ResolveGateURL` (busca `{deckURL}/settings.js`, extrai `gateHost`, cache em memória), `EffectiveLoginIdentifier` (email vs matrícula).
- `executions.go`: `SearchExecutions`/`ListProjects`.
- `rollback.go`: `DetectRollback` — implementa as duas regras da seção 0.6/seção 2 (explícita via pipeline `rollback-aks-global`/`manifestArtifact.reference`, implícita via comparação de versão-alvo-de-execução-falha vs. versão vigente no K8s). `RollbackInfo` cresceu bem além do contrato original da seção 5 durante a implementação (ver 11.4).
- `models.go`: `Trigger.Parameters`/`.Payload` — **achado real durante a implementação**: `trigger.parameters`/`trigger.payload` do JSON real do Gate não são sempre `map[string]string` como um primeiro desenho ingênuo assumiria — alguns valores vêm como número/bool/null dependendo do pipeline (ex: campos de contagem, flags). Corrigido para `map[string]interface{}`, com acessores tolerantes (`Trigger.Version()`, `Trigger.CHGNumber()`, `Trigger.CHGUrl()`, `Trigger.IsRollbackFlag()`) que fazem type-assertion segura em vez de decode direto pra string — sem essa correção, o `json.Unmarshal` de qualquer execução com um campo não-string em `trigger.parameters` falhava silenciosamente ou quebrava a busca inteira daquela application.
- **Achado real — "Is Rollback" é uma flag universal, não específica da squad SRE Logística**: ao validar contra outras applications/projetos além de `ofertalogistica`/`logreversa` (resolvendo o item 2 da seção 6), confirmou-se que `trigger.parameters["Is Rollback"]` (`"true"`/`"false"` como string) existe e é preenchido de forma consistente em pipelines de outras squads também — é uma convenção **company-wide** do template Nexus `spinnaker-helm-deployer`, não algo exclusivo do pipeline `rollback-aks-global` de uma squad. `Trigger.IsRollbackFlag()` passou a ser checado como sinal adicional (terceiro, complementar aos dois da seção 2) antes de cair na regra implícita — mais robusto que depender só do nome do pipeline, que continua variando entre squads como a seção 6 suspeitava.
- Cache de sessão por (usuário, ambiente) — `WithSession`.

### 11.2. Handler + persistência (Fase 2)

- `internal/web/handlers/spinnaker.go`: endpoint em lote (item 1 da seção 9, implementado desde o início como recomendado ali) — resolve o projeto configurado no Perfil SSO, busca execuções de todas as `applications` do projeto, devolve mapa `{ "nameApp/namespace": RolloutInfo }`.
- **Achado real — persistência necessária além do que o plano original previa**: o Gate do Spinnaker só mantém um histórico de busca de ~28 dias por padrão — deployments estáveis há mais tempo que isso (comum em produção) ficavam sem nenhuma execução retornada, mesmo tendo passado por rollout normalmente no passado. `SpinnakerHistoryStore` (SQLite, `~/.k8s-hpa-manager/spinnaker_history.db`) foi adicionado para persistir o último status confirmado de cada deployment, sobrevivendo à janela de busca do Gate — `RollbackInfo.FromCache`/`CachedAt` sinalizam ao frontend quando o dado exibido veio do cache local em vez de uma consulta fresca.
- `SpinnakerConfig` (`~/.k8s-hpa-manager/spinnaker_config.json`) + rotas `GET/POST /api/v1/spinnaker/config` e `GET /api/v1/spinnaker/projects?env=` — seção 10 implementada como planejado.

### 11.3. Frontend — Perfil SSO + Deployments (Fases 2.5 e 3)

- Bloco "Integração Spinnaker" dentro de `SSOProfileModal.tsx` (radio Email/Matrícula, 2 campos de URL do Deck, `SearchableSelect` de projeto alimentado por `GET /api/v1/spinnaker/projects`) — validado visualmente via browser automatizado (seção existe e expande corretamente).
- `DeploymentsTab.tsx`: badge compacto na lista esquerda + botão-badge no painel direito (`SpinnakerChip`), ambos abrindo `SpinnakerRolloutModal.tsx` — validado visualmente (botão aparece ao selecionar um deployment).
- **`SpinnakerRolloutModal.tsx` cresceu bem além do desenho original da seção 8** durante a implementação dos itens da seção 9 (detalhado em 11.4): CHG clicável (`ChgValue`, item 3), link "Ver no Spinnaker" (item 2), timestamp "última verificação" com refresh manual (item 4), histórico recente colapsável (item 5), tabela de etapas da execução com botão de log por linha (achado durante uso real, não previsto no plano original — ver 11.4).

### 11.4. Seção 9 — todos os 7 itens implementados

1. **Endpoint em lote** — implementado desde a Fase 2 (11.2), não como retrabalho posterior.
2. **Deep-link "Ver no Spinnaker"** — botão no modal abrindo `spinnaker_execution_url` numa nova aba.
3. **Link direto pro ServiceNow da CHG** — componente `ChgValue` reutilizado em toda CHG exibida no modal (última aplicada, que falhou, e nas listas de histórico/etapas).
4. **Refresh manual + timestamp "última verificação"** — `checked_at`/`FromCache`/`CachedAt` expostos no modal.
5. **Histórico curto (últimas 5 execuções)** — `RecentExecutions []ExecutionSummary`, seção colapsável no modal.
6. **Badge do painel esquerdo clicável** — `SpinnakerChip` na lista de cards abre o modal direto (seleciona o deployment e abre em uma ação só).
7. **Correlação no Health Check** — `SpinnakerEnricher` (`internal/healthcheck/spinnaker_enricher.go`), opt-in via checkbox "Verificar rollback recente (Spinnaker)", janela de 48h (`spinnakerRecentRollbackWindow`), resolvido uma vez por cluster (login + busca de todas as applications do projeto), best-effort — nunca derruba o Health Check inteiro se o Spinnaker estiver indisponível. Escala `StatusHealthy`→`StatusWarning` quando o deployment teve rollback recente; propagado por `correlator.go` até `CorrelatedK8sIssue` e exibido na aba "Relatório" (`HealthReportTab.tsx`) como badge âmbar com tooltip.

**Além do que a seção 9 previa** — funcionalidade adicionada a partir de uma captura de tela real da UI do Deck que o usuário compartilhou mostrando o nível de detalhe de execução que faltava: tabela "Etapas da execução" (nome, concluída em, duração, status) com botão "Ver log" por linha quando a etapa tem log de falha disponível, abrindo `SpinnakerStageLogModal.tsx` — modal novo, redimensionável (mesmo padrão de arraste de `PodQuickViewModal.tsx`), com botão de copiar. `Stage.FailureLog()` reconstrói o texto de falha a partir de dois formatos reais confirmados no JSON de execuções reais: `exception.details.{error,errors,stackTrace}` no nível do stage, e `kato.tasks[].exception.{message,cause,operation}` (ver bug 3 abaixo para a correção de um falso-positivo aqui).

### 11.5. Bugs reais encontrados e corrigidos durante a implementação/uso

1. **Layout do modal desalinhado (2 rodadas, reportado com screenshot)** — o campo "Versão" quebrou a simetria visual do grid de 2 colunas em duas tentativas sucessivas: primeiro empilhado dentro da célula "Status da execução" (aumentou a altura daquela coluna sozinha), depois numa linha própria `col-span-2` (ainda não era o pedido). Corrigido na 3ª tentativa para um grid 2x2 real: linha 1 = "Última CHG aplicada" | "Status da execução", linha 2 = "Data/hora da execução" | "Versão".
2. **"Versão anterior" mostrava a mesma versão atual, só com CHG diferente** (reportado, depois esclarecido pelo usuário: "a ideia desse histórico era pegar a versão anterior do deployment antes da execução da pipeline") — causa raiz: `previousSuccessfulExecution` só checava o status de sucesso da execução, nunca comparava a versão encontrada contra a versão atual. Cenário real que expôs o bug: a mesma pipeline pode reimplantar a versão idêntica sob uma CHG diferente (reprocessamento/mudança de infra sem bump de versão). Corrigido adicionando um parâmetro `currentVersion` que pula qualquer execução cuja versão bate com a atual, continuando a varredura pra trás no histórico. Validado ao vivo: `consulta-std-consumer` passou a retornar `previous_version` vazio corretamente (única execução no histórico de 28 dias é a atual).
3. **Falso-positivo de log em `kato.tasks[].history`** (achado por investigação própria, não reportado pelo usuário) — `Stage.FailureLog()` tratava qualquer task com `len(History) > 0` como evidência de falha, mas esse campo (narrativa passo-a-passo da orquestração) existe em **toda** execução de deploy Kubernetes, sucesso incluso. Confirmado ao vivo contra 2 execuções reais `SUCCEEDED` (`viatracking-api`, `viatracking-correios-api`) que mostravam indevidamente o botão "Ver log". Corrigido com dois guards: `FailureLog()` retorna vazio se `Stage.Status` for `SUCCEEDED`/`SKIPPED`/vazio, e a entrada de `kato.tasks` só é incluída quando `task.Exception != nil` (nunca só por ter `history`). Revalidado ao vivo pós-correção: as duas execuções passaram a reportar zero etapas com log.
4. **`<button>` aninhado em `<button>` (HTML inválido)** — corrigido proativamente antes de subir, ao tornar o `SpinnakerChip` clicável dentro do card da lista (que já era um `<button>`). O wrapper do card virou `<div role="button" tabIndex={0}>` com `onKeyDown` pra Enter/Espaço, e o chip ganhou `e.stopPropagation()` no `onClick` pra não disparar a seleção do card duas vezes via bubbling.

### 11.6. O que ficou pendente / próximos passos

- **CLAUDE.md**: deliberadamente **não** atualizado ainda — a convenção do próprio arquivo é documentar só features já mescladas na `main` ("verificado via `git merge-base`"), e esta branch ainda não foi mesclada. Adicionar a seção correspondente depois que o PR desta branch for aprovado e mesclado.
- **Item 3 da seção 6** (confirmar `nameApp == DeploymentRecord.AppName` campo-a-campo) e **item 5** (squad → application Spinnaker 1:1) seguem sem uma verificação exaustiva formal — a correlação funciona na prática via `trigger.source`/`trigger.parameters` (seção 0.3/seção 4), validada contra múltiplas applications reais durante o desenvolvimento, mas não houve uma auditoria campo-a-campo dedicada.
- Mecanismo de rollback automático de HLG via `--atomic` (seção 0.5/0.6) permanece documentado só pela investigação desta sessão — não há teste automatizado cobrindo o cenário "upgrade falha em HLG e o Helm nunca substitui a versão antiga", já que isso depende de uma falha de infraestrutura real, não reproduzível de forma determinística em teste unitário.
