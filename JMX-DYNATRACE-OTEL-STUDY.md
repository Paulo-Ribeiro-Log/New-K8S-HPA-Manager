# Estudo: Visibilidade de JMX no Dynatrace em clusters com OpenTelemetry

## Contexto

Cluster que usa OpenTelemetry para ingestão de telemetria no Dynatrace (não OneAgent com injeção de código direta). Pergunta original: dá pra ter visão de métricas JMX no Dynatrace nesse cenário? E, sendo uma frota com **muitas aplicações Java** (não uma só), como fazer isso escalar sem virar trabalho manual por aplicação?

Este documento não é um plano de implementação desta ferramenta (`New K8s HPA Manager`) — é um estudo de arquitetura para a infraestrutura Dynatrace/K8s do usuário, registrado aqui a pedido para referência futura.

---

## Dois caminhos possíveis para JMX no Dynatrace

### Caminho 1 — JMX nativo do OneAgent

Se o OneAgent estiver com **injeção de código no processo da JVM** (Cloud Native Full Stack / Application-Only Monitoring), ele expõe métricas de JVM nativamente e permite configurar Custom JMX Metrics por Process Group (Settings → Monitoring → Custom JMX Metrics, ou Extension 2.0 baseada em JMX) — sem depender de OpenTelemetry. Isso roda em paralelo a qualquer pipeline de logs via OTel, são streams de telemetria diferentes.

Não depende de nada do OTel — mas também não aparece se o OneAgent não estiver de fato atachado ao processo da JVM (cenário "só recebe o que o OTel manda", sem instrumentação de processo).

### Caminho 2 — JMX via OpenTelemetry (o caminho relevante para este cluster)

Reaproveita a infraestrutura de ingestão que já existe: um receiver JMX no pipeline do Collector, convertendo MBeans em métricas OTel, exportadas pelo mesmo caminho que já leva outra telemetria pro Dynatrace.

---

## Descoberta: qual é o Collector real deste cluster

Manifesto real inspecionado (Pod `eks-asaplog-prd-agents-otel-collector-0`, namespace `dynatrace`):

- **StatefulSet gerenciado pelo Dynatrace Operator** (`app.kubernetes.io/managed-by: dynatrace-operator`) — recurso **Telemetry Ingest** do Operator, não um Collector "cru" configurado à mão.
- Config carregada de `--config=file:///config/telemetry.yaml`, montada de um ConfigMap (`eks-asaplog-prd-agents-telemetry-collector-config`) cuja annotation `internal.operator.dynatrace.com/telemetry-ingest-config-hash` indica que é **reconciliada pelo Operator** — não é seguro editar à mão (o Operator pode reverter no próximo reconcile; o caminho suportado seria via o CR `DynaKube`, se a versão do Operator expuser essa seção).
- Expõe um endpoint OTLP genérico: `OTLP_GRPC_PORT=10001`, `OTLP_HTTP_PORT=10002`.
- Encaminha para o Dynatrace já autenticado via `DT_ENDPOINT` (ConfigMap `dynatrace-otlp-api-endpoint`) + `DT_DATA_INGEST_TOKEN` (Secret `eks-asaplog-prd`) — ou seja, qualquer app que aponte OTLP pra esse Collector **não precisa lidar com token/endpoint do Dynatrace diretamente**.

### Implicação prática: RMI não atravessa bem esse desenho

JMX usa RMI, que faz handshake em duas etapas — conecta na porta do registry, e o registry manda o cliente **reconectar** usando `java.rmi.server.hostname`. Esse Collector é **centralizado**, fora do namespace/rede dos pods de aplicação. Apontá-lo direto pra uma JVM de app é frágil:

- Não roteia bem entre namespaces sem hostname/IP estável.
- Multi-réplica atrás de um Service com load balancing não funciona — JMX precisa de sessão com **um** pod específico, não um Service que distribui entre réplicas.

Conclusão: **quem lê o JMX precisa estar co-localizado com a JVM** (mesmo Pod ou, no mínimo, mesmo host de forma confiável), independente de qual seja o backbone de ingestão.

---

## Como coletar JMX sem tocar no Collector do Operator

### Passo 1 — Habilitar JMX remoto na JVM alvo

```
-Dcom.sun.management.jmxremote
-Dcom.sun.management.jmxremote.port=9010
-Dcom.sun.management.jmxremote.rmi.port=9010
-Dcom.sun.management.jmxremote.ssl=false
-Dcom.sun.management.jmxremote.authenticate=true
-Dcom.sun.management.jmxremote.access.file=/etc/jmx/jmxremote.access
-Dcom.sun.management.jmxremote.password.file=/etc/jmx/jmxremote.password
-Djava.rmi.server.hostname=127.0.0.1
```

`java.rmi.server.hostname=127.0.0.1` só funciona se quem lê estiver no mesmo Pod (mesmo namespace de rede) — daí a recomendação de sidecar abaixo.

### Passo 2 — Imagem do Collector sidecar precisa de JRE

O `jmxreceiver` do OTel Collector Contrib não é um receiver Go nativo — sobe um subprocesso Java (o *JMX Metrics Gatherer*) por baixo dos panos. A imagem oficial `otel/opentelemetry-collector-contrib` não vem com JVM:

```dockerfile
FROM otel/opentelemetry-collector-contrib:0.1XX.0
USER root
RUN apk add --no-cache openjdk17-jre-headless
USER 10001
```

### Passo 3 — Config do sidecar (exemplo mínimo)

```yaml
receivers:
  jmx:
    jar_path: /opt/opentelemetry-java-contrib-jmx-metrics.jar
    endpoint: service:jmx:rmi:///jndi/rmi://localhost:9010/jmxrmi
    target_system: jvm          # bundles prontos também para kafka, tomcat, wildfly, activemq, cassandra, hadoop, solr, camel
    collection_interval: 30s
    username: monitorRole
    password: ${env:JMX_PASSWORD}

exporters:
  otlp:
    endpoint: eks-asaplog-prd-agents-otel-collector.dynatrace.svc.cluster.local:10001
    tls:
      insecure: true   # tráfego interno ao cluster; confirmar se o receiver do Operator exige TLS/mTLS

service:
  pipelines:
    metrics:
      receivers: [jmx]
      exporters: [otlp]
```

Esse desenho não altera nada gerenciado pelo Operator — o Collector do Dynatrace só passa a receber mais um tipo de sinal (métricas) no mesmo endpoint OTLP genérico que já expõe.

### Expectativa importante

Métricas JMX via OTel **não** aparecem na telinha nativa "JVM" do Dynatrace (essa é alimentada só pelo monitoramento de processo do OneAgent — Caminho 1). Elas entram como métricas OTLP normais, navegáveis no Metrics Browser e usáveis em dashboards custom.

---

## Escalando para muitas aplicações

O sidecar acima é **1:1** — cobre só o Pod em que está. Para uma frota com várias aplicações Java, três caminhos, com trade-offs bem diferentes de "esforço por aplicação nova":

### Opção A — Sidecar replicado via Helm

Bloco do sidecar atrás de um flag no chart compartilhado (`jmxMonitoring.enabled: true`). Funciona, mas continua sendo **um container extra por Pod** — overhead de memória de JVM por réplica, e mais superfície de coisa pra quebrar (imagem, config, restart) espalhada pela frota.

### Opção B — javaagent Prometheus + Collector central com service discovery (recomendado)

Em vez de RMI, expor o JMX como endpoint HTTP `/metrics` **de dentro do próprio processo Java**, via [Prometheus JMX Exporter javaagent](https://github.com/prometheus/jmx_exporter):

```
-javaagent:/opt/jmx_prometheus_javaagent.jar=9404:/opt/jmx-config.yaml
```

Sem o problema de reconexão do RMI (é HTTP simples) — um **único Collector central** (próprio, separado do gerenciado pelo Operator) descobre e coleta de todas as apps automaticamente via `prometheusreceiver` + `kubernetes_sd_configs`, filtrando por annotation:

```yaml
receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: jmx-apps
          kubernetes_sd_configs:
            - role: pod
          relabel_configs:
            - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
              action: keep
              regex: "true"
            - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
              action: replace
              target_label: __address__
              regex: (.+)
              replacement: "${1}"
```

Esse Collector de descoberta exporta na sequência pro Collector do Dynatrace Operator (`otlp exporter → eks-asaplog-prd-agents-otel-collector.dynatrace.svc.cluster.local:10001`), sem tocar em nada gerenciado pelo Operator.

**Efeito prático**: app nova só precisa (1) ter o javaagent na imagem/chart base e (2) duas annotations (`prometheus.io/scrape: "true"`, `prometheus.io/port: "9404"`) — nenhuma infra nova por aplicação, o Collector central já pega sozinho.

### Opção C — Webhook de injeção automática

Mutating Admission Webhook que injeta o sidecar da Opção A automaticamente em qualquer Pod com um label (`jmx-otel/inject: "true"`), estilo auto-inject do Istio. Resolve a duplicação de YAML da Opção A, mas mantém o custo de recurso por Pod (ainda é um processo extra por réplica).

### Recomendação

**Opção B** — menor custo por app, escala sem esforço adicional por aplicação nova, e evita o problema de RMI de raiz em vez de contorná-lo com colocation. O único trabalho "por app" que sobra é inerente a qualquer caminho: alguém precisa ligar a exposição do JMX na JVM — mas via javaagent isso é uma flag + duas annotations, não um sidecar inteiro.

Pré-requisito para essa opção compensar de verdade: as aplicações já compartilharem uma **imagem base Java** ou **chart Helm comum**, onde o javaagent pode ser embutido uma única vez — nesse caso a adoção por app vira só as duas annotations, sem nenhuma mudança de infraestrutura por aplicação nova.

---

## Perguntas em aberto (não confirmadas nesta conversa)

- Conteúdo real de `telemetry.yaml` (ConfigMap `eks-asaplog-prd-agents-telemetry-collector-config`) — não inspecionado; teria confirmado se o receiver OTLP do Operator exige TLS/mTLS de clientes internos, e se por acaso já existe um receiver Prometheus com discovery configurado (o que tornaria a Opção B ainda mais simples, reaproveitando o próprio Collector do Operator em vez de subir um novo).
- Existência de um CR `DynaKube` com seção `telemetryIngest` explícita, que seria o caminho suportado para customizar esse Collector sem risco de reconcile revertendo mudanças manuais.
- Nome exato do Service que aponta para o StatefulSet `eks-asaplog-prd-agents-otel-collector` (necessário para o `endpoint:` do exporter OTLP do sidecar/Collector novo) — comando sugerido: `kubectl get svc -n dynatrace | grep otel-collector`.
