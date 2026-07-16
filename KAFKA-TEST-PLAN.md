# Teste de Kafka sob Demanda

✅ Implementado — aba **"Teste Kafka"** no ToolsMenu (`internal/web/frontend/src/components/KafkaTestTab.tsx`
+ `internal/web/handlers/kafka_test_tool.go`).

## O que é

Testa conectividade, protocolo Kafka, autenticação SASL e produção/consumo de mensagens contra um
broker **externo ao cluster** (Kafka gerenciado, on-prem, ou o endpoint compatível com Kafka do
Azure Event Hub) — a partir de dentro do cluster K8s, na identidade de rede de um Deployment
específico já rodando em produção/homologação.

Motivação: rodar `kubectl exec` manual num pod pra testar `kcat`/`kafka-topics.sh` funciona, mas é
repetitivo e exige lembrar sintaxe de SASL toda vez. Esta ferramenta automatiza isso com progresso
em tempo real (SSE) e guardrails — e documenta aqui, ponto a ponto, o comando `kubectl` equivalente
ao que ela faz, pra quem preferir (ou precisar) rodar manualmente.

## Por que Ephemeral Container, não um pod novo

**Decisão central da ferramenta**: o teste roda dentro de um **Ephemeral Container** (mesmo recurso
usado por `kubectl debug --target`) anexado a um **pod já rodando** do Deployment escolhido — não um
pod avulso criado do zero.

Motivo: `NetworkPolicy` e `Istio AuthorizationPolicy` tipicamente liberam egress por **label do pod**
ou **service account**, não por namespace inteiro. Um pod solto genérico pode ter um resultado de
conectividade diferente do que o Deployment real teria. Anexar num pod já rodando herda
automaticamente a identidade de rede real (mesmo IP, mesmos labels, mesmo service account) sem
precisar clonar o spec do Deployment manualmente.

**Trade-off aceito**: Ephemeral Containers não podem ser removidos via API do K8s — ficam listados no
`pod.spec.ephemeralContainers` até o pod reiniciar. O comando roda `sleep 300`, então o processo se
encerra sozinho depois de 5 minutos (zero custo de CPU/memória depois disso), mas a entrada continua
aparecendo em `kubectl describe pod`. Mesma limitação já aceita pela Debug Container existente no
terminal de Pods (`internal/web/handlers/podexec.go`).

## Imagem usada

`edenhill/kcat:1.7.1` (kcat, ex-kafkacat) — binário C leve sobre `librdkafka`, cobre TCP, protocolo
Kafka, SASL PLAIN/SCRAM, TLS e produce/consume num único binário. Tag fixada de propósito (nunca
`latest`).

## Dois modos de execução: Pod (padrão) ou Docker local

Mesmo padrão do Teste de Banco de Dados (`DatabaseTestTab.tsx`/`db_test_tool.go`) — `execution_mode`
decide ONDE o `kcat` roda:

- **`pod`** (default): Ephemeral Container anexado a um pod real do Deployment (ver seção acima) —
  reflete `NetworkPolicy`/`Istio AuthorizationPolicy` do workload escolhido.
- **`local`**: `docker run --rm --network host edenhill/kcat:1.7.1 ...` direto no host onde o
  servidor da aplicação roda — sem tocar o cluster K8s. Útil quando o broker é alcançável
  diretamente da rede do servidor (VPN, endpoint público, LoadBalancer) e não faz sentido/não é
  possível refletir a identidade de rede de um pod específico. **Não reflete NetworkPolicy/Istio**.

Requer `docker` instalado e o daemon rodando no servidor — pré-checagem em
`GET /api/v1/kafka-test/docker-status` (reaproveita `checkDockerStatus`/`DBDockerStatusResult` do
Teste de Banco de Dados: a checagem não tem nada específico de engine, é sobre o Docker do host).
Cluster/namespace/deployment só são obrigatórios no modo `pod` — no modo `local`, só são exigidos se
a credencial SASL vem de um Secret do K8s (`sasl.secret_ref`), já que ler o Secret ainda precisa da
API do cluster mesmo com o teste em si rodando local.

**Reaper de containers órfãos**: containers `docker run --rm` criados no modo local levam o label
`app=k8s-hpa-manager-kafka-test` (`kafkaTestDockerLabel`, `kafka_test_docker.go`) — label separado
do Teste de Banco de Dados (`app=k8s-hpa-manager-db-test`) só pra não misturar as duas ferramentas
no `docker ps --filter label=...`, mas o mecanismo de limpeza (`reapOrphanedContainersByLabel`,
`db_test_docker.go`) é o mesmo: ticker de 5min remove containers rodando há mais de 10min (nenhum
teste legítimo passa de ~1min, já que o timeout máximo de um estágio é 15s).

**Bônus do modo `local`**: a "Visão geral de tópicos" ganha uma coluna real de tamanho em disco
(via `kafka-log-dirs`, imagem `confluentinc/cp-kafka` separada do `kcat`) só quando `execution_mode`
é `local` — ver seção própria mais abaixo.

---

## Os 3 estágios e o `kubectl` equivalente

Em todos os exemplos abaixo: `<pod>` é o pod real do Deployment (resolvido no passo 0),
`<container-efêmero>` é o nome gerado pra ele (`kafka-test-<timestamp>`), `<broker>` é
`host:porta` do broker Kafka/Event Hub.

### Passo 0 — Resolver um pod Running do Deployment e anexar o container efêmero

```bash
# 1. Descobrir o label selector do Deployment
kubectl get deployment <deployment> -n <namespace> -o jsonpath='{.spec.selector.matchLabels}'

# 2. Listar pods Running que batem com esse selector (troque pela saída real do passo 1)
kubectl get pods -n <namespace> -l <chave>=<valor> --field-selector=status.phase=Running

# 3. Anexar o ephemeral container kcat num desses pods (equivalente não-interativo do
#    `kubectl debug -it --target=...`)
kubectl debug <pod> -n <namespace> \
  --image=edenhill/kcat:1.7.1 \
  --target=<container-principal-do-pod> \
  --container=<container-efêmero> \
  -- sleep 300

# 4. Esperar ficar Running
kubectl get pod <pod> -n <namespace> -o jsonpath='{.status.ephemeralContainerStatuses}'
```

### Passo 1 — Conectividade + protocolo Kafka + SASL (`kcat -L`)

Um único comando cobre TCP, DNS, handshake do protocolo Kafka e autenticação — a ferramenta
classifica a causa da falha (rede/DNS vs SASL vs TLS) a partir do texto do erro.

```bash
# Sem autenticação
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -L

# Com SASL (PLAIN, sem TLS)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> \
    -X security.protocol=SASL_PLAINTEXT \
    -X sasl.mechanisms=PLAIN \
    -X sasl.username=<usuário> \
    -X sasl.password=<senha> \
    -L

# Com SASL + TLS (comum em Kafka gerenciado / Event Hub)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> \
    -X security.protocol=SASL_SSL \
    -X sasl.mechanisms=PLAIN \
    -X sasl.username=<usuário> \
    -X sasl.password=<senha> \
    -L
```

Se `-L` der certo, a saída lista brokers e tópicos — é isso que a ferramenta conta pra preencher
"N broker(s)" / "N tópico(s)" no resultado.

**Diagnóstico por camada** (mesma lógica implementada em `runKafkaConnectivityStage`):
- Erro contendo `Connect to ... failed` / `Failed to resolve` → problema de rede/DNS, nem chegou a
  tentar o protocolo Kafka.
- Erro contendo `SASL` → TCP conectou, mas a autenticação falhou (credencial errada, ou o broker
  exige SASL e você não configurou nenhum). Se o erro citar os mecanismos aceitos pelo broker
  (comum quando o mecanismo enviado é o errado), a ferramenta extrai isso automaticamente.
- Erro contendo `SSL` → problema de handshake TLS (certificado, ou `security.protocol` errado —
  ex: tentou `SASL_PLAINTEXT` num broker que exige `SASL_SSL`).

### Passo 2 — Produzir e consumir mensagem de teste (escreve estado real)

⚠️ Publica uma mensagem de verdade no tópico — a ferramenta exige confirmação explícita
(`confirm_produce: true`) antes de rodar isso, validada tanto no frontend quanto no backend.

```bash
# Produzir (marcador único como payload, via stdin)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'printf "%s" "meu-marcador-unico" | timeout 5s kcat -b <broker> -P -t <tópico>'

# Consumir de volta — lê até 50 mensagens desde o início do tópico procurando o marcador
# (compromisso de simplicidade: não usa offset exato pré-produce, ver nota abaixo)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -C -t <tópico> -o beginning -c 50 -e
```

*Nota de precisão*: como não há como saber o offset exato antes de produzir sem uma chamada extra de
metadata, a ferramenta lê até 50 mensagens do **início** do tópico e procura o marcador no texto —
funciona bem para tópicos pequenos/de teste, mas pode não achar a mensagem em tópicos com muito
volume (ela existe, só está além do alcance de 50 mensagens lidas desde o início).

### Passo 3 — Visualizar mensagens existentes (só leitura, sem confirmação)

Lê as últimas N mensagens já existentes no tópico, sem publicar nada. Usa offset negativo do kcat
(`-o -N` = "N mensagens antes do fim de cada partição") + saída em JSON (`-J`) pra extrair
partição/offset/timestamp/key/payload de cada mensagem.

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -C -t <tópico> -o -10 -c 10 -e -J
```

Cada linha da saída é um objeto JSON: `{"topic":"...","partition":0,"offset":123,"ts":..., "key":"...", "payload":"..."}`.

**Limitação conhecida — payload binário**: se o payload/key não for UTF-8 válido (protobuf, Avro, ou
um tópico interno do Kafka como `__consumer_offsets`), o `kcat -J` substitui os bytes inválidos por
`�` (U+FFFD) **antes** de entregar a saída — os bytes originais já se perdem nesse ponto, não é
recuperável no parsing. A ferramenta detecta a presença desse caractere e marca a mensagem como
"binário" na UI, mas não reconstrói o conteúdo original. Pra ler binário de verdade, use uma
ferramenta com o schema certo (Avro console consumer, decoder de protobuf, etc.) fora desta
ferramenta.

---

## Visão geral de tópicos — Partições + ~Mensagens (estilo "All Stats" do MongoDB Compass)

Botão **"Visão geral de tópicos"** (independente do teste principal, mesmo espírito do campo de
busca de tópicos): lista TODOS os tópicos do broker com número de partições e uma estimativa de
quantas mensagens estão retidas em cada um — a mesma ideia da tabela "All Stats" que o MongoDB
Compass mostra pra collections, adaptada ao que o `kcat` consegue expor (sem tamanho em disco —
ver "Fora de escopo" mais abaixo).

`~Mensagens` é `latest - earliest`, somado por partição — não é um `COUNT(*)` real (Kafka não tem
isso), é o teto teórico de mensagens ainda recuperáveis considerando a política de retenção atual.

```bash
# 1. Metadata — descobre todos os tópicos + partições de cada um numa única chamada
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -L

# 2. Offset mais recente (fim) de TODAS as partições de TODOS os tópicos, numa única chamada —
#    uma entrada -t por partição
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -Q \
    -t topico-a:0:-1 -t topico-a:1:-1 -t topico-b:0:-1 ...

# 3. Offset mais antigo (início) das MESMAS partições — EXEC SEPARADO, nunca junto com o passo 2
#    (kcat 1.7.1 deduplica internamente quando -1 e -2 da mesma partição aparecem na mesma chamada)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s kcat -b <broker> -Q \
    -t topico-a:0:-2 -t topico-a:1:-2 -t topico-b:0:-2 ...
```

**Teto de segurança** (`kafkaTopicsOverviewCap = 200`): brokers com muitos tópicos gerariam um
comando `-Q` gigante (uma entrada por partição) e uma varredura pesada no broker. Além do teto, só
os primeiros tópicos em ordem alfabética entram na consulta de offsets — os demais aparecem na
tabela com "—" na coluna `~Mensagens` (`message_count: -1` no JSON, pra distinguir de um tópico
realmente vazio, que é `0`).

**Coluna de tamanho em disco — só no modo `local`** (`kafka_test_logdirs.go`): diferente do
Postgres/MySQL/Mongo (que têm estatística de tamanho via catálogo, sem scan), o `kcat` não expõe
tamanho em disco — só os scripts nativos do Kafka (`kafka-log-dirs`) conseguem. A decisão inicial
foi não vale o esforço de trocar de imagem, mas isso reconsiderou uma vez que o modo `local` (Docker
no host do servidor) já existia: o custo de puxar uma imagem completa do Kafka (`confluentinc/cp-
kafka:7.7.1`, +1GB contra ~30MB do `kcat`) só se aplica ao modo `local`, onde a imagem fica cacheada
no Docker do próprio servidor — sem o efeito colateral de inflar o armazenamento compartilhado dos
NODES do cluster que o modo `pod` teria (Ephemeral Container puxaria a imagem pro node a cada
teste).

Fluxo (best-effort, roda DEPOIS do cálculo de Partições+~Mensagens via `kcat`, não impede a resposta
se falhar):
1. Monta um `client.properties` (formato dos clientes Java Kafka — vocabulário diferente do
   `librdkafka`/`kcat`: `sasl.mechanism` singular, `sasl.jaas.config` em vez de username/password
   soltos) via `buildKafkaClientPropertiesFile`, com username/senha escapados
   (`escapeJaasPropertyValue`) pra não quebrar o parsing do JAAS config.
2. Grava esse arquivo dentro do container via base64 (`echo <b64> | base64 -d > ...`) — evita
   qualquer problema de quoting de shell com o conteúdo do arquivo.
3. Roda `kafka-log-dirs --bootstrap-server <broker> --describe --topic-list <tópicos> --command-
   config /tmp/kafka-client.properties` num `docker run --rm` separado (imagem `confluentinc/cp-
   kafka:7.7.1`, mesmo label `kafkaTestDockerLabel`), com timeout próprio de 30s (JVM demora mais
   pra subir que o `kcat`).
4. Parseia o JSON de saída (`parseKafkaLogDirsOutput`): cada partição aparece uma vez POR RÉPLICA
   (líder + followers) — soma ingênua superestimaria pelo fator de replicação, então pega o MAIOR
   valor visto por partição e só então soma por tópico.

Falha nessa etapa vira `disk_usage_warning` na resposta (não deixa a coluna nem derruba a Visão
geral inteira, que já é útil só com Partições+~Mensagens). Assumpção não validada contra uma imagem
real: `kafka-log-dirs` está no PATH da tag usada (prática usual do empacotamento Confluent).

**Limitação de TLS**: o equivalente ao `enable.ssl.certificate.verification=false` do `librdkafka`
não existe como propriedade simples nos clientes Java — só dá pra desabilitar verificação de
hostname (`ssl.endpoint.identification.algorithm=`), não a CA inteira. Broker com certificado
self-signed pode falhar aqui mesmo com "Ignorar verificação TLS" marcado — cai no
`disk_usage_warning`, sem afetar o resto.

---

## SASL — referência de flags

| Cenário | `security.protocol` | `sasl.mechanisms` |
|---|---|---|
| Kafka interno sem TLS | `SASL_PLAINTEXT` | `PLAIN` ou `SCRAM-SHA-512` |
| Kafka gerenciado / Confluent Cloud | `SASL_SSL` | `PLAIN` |
| MSK / Strimzi com SCRAM | `SASL_SSL` | `SCRAM-SHA-512` |
| Só TLS, sem autenticação | `SSL` | — |

**Azure Event Hub** (endpoint compatível com Kafka): `security.protocol=SASL_SSL`,
`sasl.mechanisms=PLAIN`, usuário literal `$ConnectionString`, senha = a connection string completa
do namespace Event Hub. A UI já sugere isso como placeholder nos campos de usuário/senha.

**Credenciais via Secret do K8s**: quando a origem é "Ler de um Secret do K8s", o backend lê o
Secret direto via `clientset.CoreV1().Secrets(ns).Get(...)` — client-go já decodifica base64
automaticamente — e injeta usuário/senha só no comando executado dentro do pod. As credenciais
**nunca** trafegam de volta pro frontend nem são gravadas no audit log (`HistoryTracker`).
Equivalente manual:

```bash
USER=$(kubectl get secret <secret> -n <namespace> -o jsonpath='{.data.username}' | base64 -d)
PASS=$(kubectl get secret <secret> -n <namespace> -o jsonpath='{.data.password}' | base64 -d)
```

---

## Guardrails

- RBAC: rotas de escrita (`POST /kafka-test/run`, `POST /kafka-test/cancel/:sessionId`) atrás de
  `rbacMiddleware.RequireSREGroup()`.
- Um teste por vez por usuário (`sync.Map` de lock, mesmo padrão do Latency Test).
- Timeout hardcoded no backend (`kafkaTestMaxTimeoutMs = 15000`ms) — não confia só no valor mandado
  pelo frontend.
- Produzir mensagem exige `confirm_produce: true` explícito no request — rejeitado com 400 sem
  isso, validado nos dois lados (frontend desabilita o botão, backend rejeita mesmo assim).
- Visualizar mensagens é só leitura — não precisa de confirmação, mas tem teto de 50 mensagens por
  chamada (`kafkaTestViewMaxMessages`).
- Toda execução é logada no `HistoryTracker` (`action: "kafka_test"`) — broker, namespace,
  deployment, pod alvo, container efêmero, status de cada estágio; nunca senha/usuário.

## Onde conferir o estado do container efêmero depois

O resultado da ferramenta mostra `target_pod` e `ephemeral_container`. Pra conferir o estado real a
qualquer momento (ele não desaparece sozinho, só o processo termina):

```bash
kubectl get pod <target_pod> -n <namespace> -o jsonpath='{.status.ephemeralContainerStatuses}'
kubectl describe pod <target_pod> -n <namespace>   # mostra ele na lista de containers
```

---

## Mapa de arquivos

| Arquivo | O quê |
|---|---|
| `internal/web/handlers/kafka_test_tool.go` | Handler completo: `kafkaExecFunc` abstrai pod vs. local, resolução de pod/deployment, ephemeral container, os 3 estágios do teste principal (conectividade/produce-consume/view), `ListTopics` (busca), `TopicsOverview` (Partições + ~Mensagens), SSE, guardrails |
| `internal/web/handlers/kafka_test_docker.go` | Modo `local`: label `kafkaTestDockerLabel`, `DockerStatus` (reaproveita `checkDockerStatus`), `startKafkaTestContainerReaper` |
| `internal/web/handlers/kafka_test_logdirs.go` | Tamanho em disco por tópico no modo `local`: `client.properties` (`buildKafkaClientPropertiesFile`), script `kafka-log-dirs` (`buildKafkaLogDirsScript`), parsing (`parseKafkaLogDirsOutput`) |
| `internal/web/handlers/kafka_test_logdirs_test.go` | Testes unitários (escaping JAAS, client.properties por cenário de auth/TLS, extração/parsing do JSON do `kafka-log-dirs`) |
| `internal/web/handlers/db_test_docker.go` | `execLocalDocker`/`reapOrphanedContainersByLabel`/`checkDockerStatus` — compartilhados com o Teste de Banco de Dados, generalizados por `label` |
| `internal/web/handlers/kafka_test_tool_test.go` | Testes unitários das funções puras de parsing/montagem de comando (`buildKafkaOffsetQueryArgsMulti`, `parseKafkaOffsetLinesWithTopic`) |
| `internal/web/frontend/src/components/KafkaTestTab.tsx` | UI: seletores cluster/namespace/deployment, radio Pod/Docker local + painel de pré-checagem, config SASL, toggles de produce/consume e view, busca de tópicos, botão + modal "Visão geral de tópicos", resultado com badges/modal de mensagem |
| `internal/web/frontend/src/lib/dockerFixSnippets.ts` | Snippets de instalação/fix do Docker — compartilhado entre `KafkaTestTab.tsx` e `DatabaseTestTab.tsx` |
| `internal/web/frontend/src/lib/api/types.ts` | Tipos `RunKafkaTestRequest`, `KafkaTestResult`, `KafkaMessage`, `ListKafkaTopicsRequest/Response`, `TopicsOverviewResponse`, etc. |
| `internal/web/frontend/src/lib/api/client.ts` | `runKafkaTest`/`getKafkaTestStreamURL`/`cancelKafkaTest`/`listKafkaTopics`/`kafkaTopicsOverview`/`getKafkaTestDockerStatus` |
| `internal/web/server.go` | Registro das rotas `/api/v1/kafka-test/*` |

## Fora de escopo (por enquanto)

- Histórico persistente (SQLite) e visão agregada tipo "topologia" (o Latency Test tem, este não) —
  só audit log genérico via `HistoryTracker`.
- Coluna de tamanho em disco no modo `pod` — ✅ implementada no modo `local` (`kafka-log-dirs` via
  imagem completa do Kafka, ver seção acima); no `pod` continua fora de escopo porque puxaria essa
  imagem pro node do cluster a cada Ephemeral Container, inflando armazenamento compartilhado.
- Deixar escolher qual pod (quando o Deployment tem várias réplicas) ou qual container (pods
  multi-container) — hoje pega sempre o primeiro Running e o primeiro container.
- Validação de certificado TLS customizado (`ssl.ca.location` apontando pra uma CA específica) — só
  o toggle simples "usar TLS" + "ignorar verificação".
