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
| `internal/web/handlers/kafka_test_tool.go` | Handler completo: resolução de pod/deployment, ephemeral container, os 3 estágios, SSE, guardrails |
| `internal/web/frontend/src/components/KafkaTestTab.tsx` | UI: seletores cluster/namespace/deployment, config SASL, toggles de produce/consume e view, resultado com badges/modal de mensagem |
| `internal/web/frontend/src/lib/api/types.ts` | Tipos `RunKafkaTestRequest`, `KafkaTestResult`, `KafkaMessage`, etc. |
| `internal/web/frontend/src/lib/api/client.ts` | `runKafkaTest`/`getKafkaTestStreamURL`/`cancelKafkaTest` |
| `internal/web/server.go` | Registro das rotas `/api/v1/kafka-test/*` |

## Fora de escopo (por enquanto)

- Histórico persistente (SQLite) e visão agregada tipo "topologia" (o Latency Test tem, este não) —
  só audit log genérico via `HistoryTracker`.
- Descoberta automática de tópicos existentes pra popular um `<Select>` (o `-L` já retorna os nomes,
  mas isso só roda depois que o teste começa — daria pra reaproveitar numa iteração futura).
- Deixar escolher qual pod (quando o Deployment tem várias réplicas) ou qual container (pods
  multi-container) — hoje pega sempre o primeiro Running e o primeiro container.
- Validação de certificado TLS customizado (`ssl.ca.location` apontando pra uma CA específica) — só
  o toggle simples "usar TLS" + "ignorar verificação".
