# Teste de Banco de Dados sob Demanda

✅ Implementado — aba **"Teste de Banco de Dados"** no ToolsMenu
(`internal/web/frontend/src/components/DatabaseTestTab.tsx` +
`internal/web/handlers/db_test_tool.go`).

## O que é

Testa conectividade e autenticação — e, opcionalmente, lista databases/tabelas/collections/chaves
(só leitura) — contra um banco de dados **gerenciado ou self-hosted**, dentro ou fora do cluster,
a partir da identidade de rede de um Deployment específico já rodando em produção/homologação.
Cobre 4 engines: **PostgreSQL**, **MySQL/MariaDB**, **MongoDB** e **Redis**.

Mesma motivação do [Teste de Kafka](KAFKA-TEST-PLAN.md): rodar `kubectl exec` manual pra testar
`psql`/`mysql`/`mongosh`/`redis-cli` funciona, mas é repetitivo e exige lembrar sintaxe de conexão
toda vez. Esta ferramenta automatiza isso com progresso em tempo real (SSE) e documenta aqui,
ponto a ponto, o comando `kubectl`/CLI equivalente ao que ela faz.

## Por que Ephemeral Container, não um pod novo

Mesma decisão e mesma motivação do Teste de Kafka (ver
[KAFKA-TEST-PLAN.md § Por que Ephemeral Container](KAFKA-TEST-PLAN.md#por-que-ephemeral-container-não-um-pod-novo)):
`NetworkPolicy`/`Istio AuthorizationPolicy` avaliam egress por label do pod ou service account, não
por namespace inteiro — um pod avulso genérico teria uma identidade de rede diferente do Deployment
real. O teste anexa um Ephemeral Container num pod **já rodando** do Deployment escolhido, herdando
automaticamente essa identidade.

**Trade-off aceito, idêntico ao Kafka**: o comando roda `sleep 300` e se encerra sozinho, mas a
entrada continua em `pod.spec.ephemeralContainers` até o pod reiniciar (Ephemeral Containers não
podem ser removidos via API do K8s).

## Modo alternativo: execução local via Docker (terminal do servidor)

Diferente do Kafka, o Teste de Banco de Dados tem um **segundo modo de execução**
(`execution_mode: "pod" | "local"`, radio no topo da UI) — pensado pra bancos alcançáveis
**diretamente da rede do servidor da aplicação** (VPN, endpoint público, LoadBalancer), onde não
faz sentido nem é possível simular a identidade de rede de um workload específico do cluster:

- **`pod`** (default) — tudo descrito acima: Ephemeral Container anexado a um pod real.
- **`local`** — mesmo princípio do `kcat` no Teste de Kafka: roda a **mesma imagem do engine já
  usada no modo `pod`** (`postgres:16-alpine`/`mariadb:11`/`mongo:7`/`redis:7-alpine`) num
  **container Docker local** (`docker run --rm --network host <imagem> sh -c "<script>"`) no host
  onde o binário `new-k8s-hpa` está rodando, sem nenhuma chamada à API do K8s pro teste em si.
  Cluster/Namespace/Deployment ficam **opcionais** — só passam a ser obrigatórios se alguma
  referência de Secret/ConfigMap for usada pra ler host/porta/credenciais (ver `usesK8sRef` em
  `DatabaseTestTab.tsx` — a leitura do Secret/ConfigMap em si sempre passa pela API do K8s,
  independente de onde o cliente do banco executa depois).

**Pré-requisito real**: o modo `local` exige **apenas o binário `docker` + daemon rodando** no
servidor — não os clientes (`psql`/`mysql`/`mongosh`/`redis-cli`) instalados nativamente, a imagem
já traz tudo. `--network host` dá ao container a mesma visibilidade de rede que o processo do
servidor teria — funciona em Linux/WSL2 (ambiente alvo desta app), mas Docker Desktop no
Mac/Windows tem semântica de host networking diferente e não foi validado. Pré-checagem e limpeza
de containers órfãos: ver seção dedicada abaixo.

**`--network host` não cria acesso novo, só reflete o que o host já tem**: pra testar um banco
**on-premise**, quem precisa ter rota até ele é o **servidor da aplicação** (VPN ativa, mesma rede
corporativa, firewall liberado) — o container Docker só herda exatamente essa reachability, nem
mais nem menos. Cuidado específico com **WSL2**: por padrão a instância WSL2 tem namespace de rede
isolado do Windows — se a VPN estiver ativa só no Windows (não dentro do WSL2), nem o modo `local`
nem um cliente nativo instalado diretamente no WSL2 vão alcançar o on-premise; a limitação está na
fronteira Windows↔WSL2, não é algo que o Docker resolve ou piora.

**Implementação**: `runDBConnectivityStage`/`runDBBrowseStage` não sabem mais ONDE o script roda —
recebem um `dbExecFunc` (`func(ctx, script string) (string, error)`), resolvido em `runTest()` pra
`execCmdInPod` (modo `pod`) ou `execLocalDocker` (modo `local`, `db_test_tool.go`). Mesmo formato
de erro (`"... (stderr: ...)"`) nos dois — a classificação de erro (`extractStderr` + regexes por
engine) não muda entre modos, só a função que efetivamente executa o comando.

**Correção de um bug real** (achado testando neste próprio ambiente de dev, que não tem `docker`
instalado): tanto `execCmdInPod` quanto `execLocalDocker` descartavam todo o `stdout` capturado
sempre que o comando saía com erro (`return "", err`) — mas os scripts de cada engine rodam com
`... 2>&1`, então a mensagem real de erro do cliente (`psql`/`mysql`/`mongosh`/`redis-cli`) vem em
`stdout`, não no `stderr` separado do exec do K8s/Docker (que fica vazio nesse caso). Resultado:
`raw_output` chegava sempre `""` no frontend (mostrado como "(sem saída)") em qualquer falha de
conectividade — não só com Docker ausente. Corrigido devolvendo o `stdout` capturado mesmo no erro,
usado como saída bruta primária em `runDBConnectivityStage`/`runDBBrowseStage` (cai pro `stderr` do
exec só se `stdout` vier vazio). `extractStderr` (compartilhada com Kafka/Latência,
`kafka_test_tool.go`) também jogava fora a mensagem de erro real quando o trecho `(stderr: ...)`
embutido vinha vazio — caso clássico de falha antes do processo sequer rodar (`docker` não
instalado: `exec.LookPath` falha sem gerar stdout/stderr nenhum, a causa real fica ANTES dos
parênteses). Agora cai pra mensagem completa do erro nesse caso.

**Bug de UI relacionado**: `deriveConnectivityBadges` (`DatabaseTestTab.tsx`) marcava TCP/DNS e
Autenticação como "ok" (verde) por padrão pra qualquer status que não fosse `tcp_failed`/
`auth_failed` — incluindo `unknown_failed` (falha não classificada pelas regexes do engine).
Resultado visual contraditório: badges verdes ao lado de "Falha não classificada". Corrigido pra
marcar todos os sub-estágios como "skipped" (desconhecido) nesse caso, já que não dá pra saber em
qual etapa falhou.

## Pré-checagem de Docker + reaper de containers órfãos (modo `local`)

Dois problemas de UX ficaram claros corrigindo os bugs acima (justo testando num ambiente sem
`docker` instalado):

1. **Sem pré-checagem**: o usuário só descobria que faltava Docker depois de clicar em "Executar
   Teste". Agora `GET /api/v1/db-test/docker-status` (`db_test_docker.go`) checa
   `exec.LookPath("docker")` e, se instalado, `docker info --format '{{.ServerVersion}}'` (timeout
   5s) — cacheado por 20s (mesmo padrão de `IsGcloudAuthActive`,
   `internal/cloudprovider/gcp/auth.go`). Classifica `permission denied` separado de "daemon não
   respondeu" (causa comum: usuário fora do grupo `docker`). Sem `RequireSREGroup()` — é leitura
   informacional sobre o próprio servidor, não sobre um recurso do usuário.

   Frontend: `DatabaseTestTab.tsx` consulta esse endpoint só quando `execution_mode === "local"`
   (`staleTime` 15s); se não estiver pronto, mostra painel âmbar com o passo a passo de instalação
   (Ubuntu/WSL2 — `curl -fsSL https://get.docker.com | sudo sh` + `usermod -aG docker $USER` +
   `service docker start`, já que WSL2 normalmente não sobe serviços sozinho no boot) e botão
   "Verificar novamente". `canRun` passa a exigir `dockerReady` — **decisão deliberada**: bloquear
   o botão em vez de só avisar e deixar tentar, consistente com outros fluxos de "auth necessária"
   já existentes na app (ex: `SNATPortWidget.tsx`).

2. **Risco de container órfão**: `docker run --rm` já remove o container quando o processo termina
   sozinho (inclusive quando o `timeout Ns` interno do script expira — o container ainda sai
   normalmente depois, `--rm` cobre esse caso). O que **não** é coberto: se o usuário cancela um
   teste local em andamento, o Go cancela o `context.Context` e mata o processo `docker` (CLI) via
   SIGKILL — sinal que não pode ser interceptado, então o CLI nunca chega a rodar sua própria
   lógica de `--rm`. O container pode continuar rodando no daemon, órfão.

   **Decisão deliberada**: o reaper limpa só **containers**, nunca as imagens base
   (`postgres:16-alpine`/`mariadb:11`/`mongo:7`/`redis:7-alpine`) — elas ficam paradas em disco sem
   consumir CPU/RAM, e apagá-las forçaria um `pull` novo (rede + tempo) no próximo teste.

   `execLocalDocker` passou a receber um `name` (`k8s-hpa-dbtest-<sessionID>`, sessionID já é único
   por execução) — `docker run` ganhou `--name <name>` e `--label app=k8s-hpa-manager-db-test`
   (`dbTestDockerLabel`, `db_test_docker.go`). Duas camadas de limpeza:
   - **Imediata**: se `cmd.Run()` falhar E `ctx.Err() != nil` (veio de cancelamento, não de exit
     code normal), dispara em goroutine própria (contexto novo, 10s, best-effort, só loga se
     falhar) `docker rm -f <name>`.
   - **Reaper periódico** (rede de segurança pro que escapar da limpeza imediata — crash do
     servidor, SIGKILL do SO): `startDBTestContainerReaper()`, ticker de 5min (mesmo padrão do
     `cleanupLoop` em `internal/web/sse/progress.go`), iniciado uma vez em `NewDBTestHandler`. A
     cada tick: `docker ps -a --filter "label=..."  -q` → se vier algum ID, `docker inspect -f
     '{{.Id}}|{{.Created}}' <ids...>` (uma chamada só, batched) → parseia `Created`
     (`time.RFC3339Nano`) → qualquer container com mais de 10min (`dbTestContainerMaxAge`) leva
     `docker rm -f` (generoso: o timeout máximo de um estágio é 15s, `dbTestMaxTimeoutMs`, então
     nada legítimo passa de ~1min rodando). Se `docker` não estiver instalado, vira no-op silencioso
     (sem spam de log). Lógica de "quais containers estão velhos" isolada em função pura
     (`filterStaleContainers`) — testável com timestamps fake sem precisar de um daemon Docker real
     (`db_test_docker_test.go`).

## Diferente do Kafka: teste **nunca escreve** no banco

O Teste de Kafka tem um estágio opcional de produce/consume que publica uma mensagem real (atrás de
confirmação explícita). O Teste de Banco de Dados **não tem equivalente** — decisão de escopo
deliberada (bancos de produção não devem receber escritas de uma ferramenta de diagnóstico). Os
únicos dois estágios são:

1. **Conectividade + autenticação** — sempre executado.
2. **Explorar dados** (opcional, `browse: true`) — só leitura, lista nomes de databases/tabelas/
   collections ou uma amostra de chaves (Redis). Não precisa de confirmação porque nada muta.

## Imagens por engine

| Engine | Imagem (tag fixa) | Cliente |
|---|---|---|
| PostgreSQL | `postgres:16-alpine` | `psql` |
| MySQL/MariaDB | `mariadb:11` | `mysql`/`mariadb` (mesmo binário atende os dois servers) |
| MongoDB | `mongo:7` | `mongosh` |
| Redis | `redis:7-alpine` | `redis-cli` |

Todas as tags são fixadas de propósito (nunca `latest`), mesmo princípio do `kcat` no Kafka.

---

## Os 2 estágios e o `kubectl`/CLI equivalente

Em todos os exemplos: `<pod>` é o pod real do Deployment (resolvido no passo 0, mesmo mecanismo do
Kafka), `<container-efêmero>` é o nome gerado (`db-test-<timestamp>`).

### Passo 0 — Resolver um pod Running do Deployment e anexar o container efêmero

Idêntico ao Kafka, trocando só a imagem:

```bash
kubectl debug <pod> -n <namespace> \
  --image=<imagem-do-engine> \
  --target=<container-principal-do-pod> \
  --container=<container-efêmero> \
  -- sleep 300

kubectl get pod <pod> -n <namespace> -o jsonpath='{.status.ephemeralContainerStatuses}'
```

### Passo 1 — Conectividade + autenticação

**PostgreSQL** (psql aceita URI nativa):

```bash
# Discreto (montado internamente como URI antes de chamar)
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s psql "postgresql://user:pass@host:5432/db?sslmode=require" -c "SELECT 1;"

# Connection string informada diretamente pelo usuário — passada como está
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s psql "<connection-string-do-usuário>" -c "SELECT 1;"
```

**MySQL/MariaDB** (cliente NÃO aceita URI — connection string é parseada no backend com
`net/url.Parse` e convertida pros flags discretos; senha via `MYSQL_PWD` env, não `-p<senha>`, pra
não aparecer em `ps`):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'MYSQL_PWD="<senha>" timeout 5s mysql -h <host> -P 3306 -u <user> --ssl-mode=REQUIRED <db> -e "SELECT 1;"'
```

**MongoDB** (mongosh aceita URI nativa, incluindo `mongodb+srv://` do Atlas — modo discreto é
montado como URI internamente antes de chamar):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s mongosh "mongodb://user:pass@host:27017/db?tls=true" --quiet --eval "db.runCommand({ping:1})"
```

**Redis** (redis-cli 6+ aceita `-u` com URI completa):

```bash
# Discreto
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s redis-cli -h <host> -p 6379 -a <senha> --no-auth-warning --tls PING

# Connection string
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s redis-cli -u "<connection-string-do-usuário>" PING
```

**Diagnóstico por camada** (mesma lógica implementada em `runDBConnectivityStage`, uma regex de
rede/auth/TLS por engine — texto do erro é bem diferente entre eles):

| Engine | Rede/DNS | Autenticação | TLS |
|---|---|---|---|
| PostgreSQL | `could not connect to server`, `connection refused` | `password authentication failed`, `no pg_hba.conf entry` | `SSL error`, `certificate verify failed` |
| MySQL/MariaDB | `Can't connect to MySQL server` | `Access denied for user` | `SSL connection error`, TLS handshake |
| MongoDB | `ECONNREFUSED`, `ServerSelectionError` | `Authentication failed`, `not authorized` | erro contendo `tls`/`ssl` + `error`/`handshake`/`certificate` |
| Redis | `Could not connect`, `connection refused` | `NOAUTH`, `WRONGPASS` | erro contendo `tls`/`ssl` + `error`/`handshake` |

Diferente do Kafka, **não há extração de "mecanismo sugerido pelo servidor"** — isso era específico
do handshake SASL do protocolo Kafka (o broker geralmente informa quais mecanismos aceita quando
você erra o mecanismo). Bancos de dados não têm um equivalente genérico disso; a classificação aqui
só resulta em `ok`/`tcp_failed`/`auth_failed`/`tls_failed`/`unknown_failed`, sem sugestão automática
de correção.

### Passo 2 — Explorar dados (só leitura, sem confirmação)

O nível listado depende de ter ou não um **Database** informado — sem ele, lista o nível
"database"; com ele, desce um nível e traz tabelas/collections com informação extra (colunas,
contagem de documentos). Redis não tem esse conceito de dois níveis — sempre lista chaves, mas
agora com o `TYPE` de cada uma.

**PostgreSQL sem Database** — lista databases (evita parsear a formatação de `\l`):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s psql "<conn>" -t -A -c "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname;"
```

**PostgreSQL com Database** — lista tabelas do schema `public` + colunas/tipos (uma linha por
coluna, formato `tabela|coluna|tipo`, agrupado pela ferramenta em "tabela: colunas..."):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s psql "<conn>" -t -A -c "SELECT table_name || '|' || column_name || '|' || data_type FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name, ordinal_position;"
```

**MySQL/MariaDB sem Database**:

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'MYSQL_PWD="<senha>" timeout 5s mysql -h <host> -P 3306 -u <user> -N -e "SHOW DATABASES;"'
```

**MySQL/MariaDB com Database** — mesmo formato `tabela|coluna|tipo` do Postgres; `DATABASE()`
resolve pro banco já selecionado na conexão (`-D <db>`), sem precisar reinterpolar o nome na query:

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'MYSQL_PWD="<senha>" timeout 5s mysql -h <host> -P 3306 -u <user> <db> -N -e "SELECT CONCAT(table_name,'"'"'|'"'"',column_name,'"'"'|'"'"',column_type) FROM information_schema.columns WHERE table_schema=DATABASE() ORDER BY table_name, ordinal_position;"'
```

**MongoDB sem Database** — lista databases + tamanho em disco:

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s mongosh "<conn>" --quiet --eval "JSON.stringify(db.adminCommand({listDatabases:1}).databases.map(function(d){return {name:d.name, sizeOnDisk:d.sizeOnDisk};}))"
```

**MongoDB com Database** — lista collections + contagem estimada de documentos
(`estimatedDocumentCount()` lê metadata do storage engine, não escaneia a collection inteira —
seguro mesmo em collections grandes):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s mongosh "<conn>" --quiet --eval "JSON.stringify(db.getSiblingDB('<db>').getCollectionNames().map(function(c){return {name:c, count:db.getSiblingDB('<db>').getCollection(c).estimatedDocumentCount()};}))"
```

**Redis** — amostra de até 100 chaves via `SCAN` (nunca `KEYS *`, que bloqueia instâncias grandes;
`--scan` sozinho não tem teto total — o `head` garante o limite real) + `TYPE` de cada chave (um
round-trip extra por chave — O(1) no Redis, o teto de 100 mantém isso rápido; todo o pipeline roda
dentro do mesmo timeout via `timeout Ns sh -c '...'` envolvendo o scan+loop inteiro):

```bash
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  timeout 5s sh -c 'redis-cli -h <host> -p 6379 -a <senha> --no-auth-warning --scan | head -n 100 | while IFS= read -r k; do t=$(redis-cli -h <host> -p 6379 -a <senha> --no-auth-warning TYPE "$k" 2>/dev/null); printf "%s|%s\n" "$k" "$t"; done'
```

Quando exatamente 100 chaves voltam (Postgres/MySQL/Mongo não têm esse teto — só Redis), a
ferramenta marca o resultado como `truncated: true` — é uma **amostra**, não uma listagem
completa do keyspace.

---

## Autenticação — referência

| Modo | Quando usar |
|---|---|
| Sem autenticação | Instâncias de dev/teste sem auth configurada |
| Usuário e senha | Discreto — host/porta + user/pass (manual ou via Secret K8s) |
| Connection string | URI completa (`postgresql://`, `mysql://`, `mongodb+srv://`, `redis://`) — cobre bancos gerenciados com formato próprio (Atlas, RDS, etc.) |

**Credenciais via Secret do K8s** (modo "Usuário e senha" → "Ler de um Secret do K8s"): mesmo
mecanismo do Kafka — o backend lê o Secret direto via `clientset.CoreV1().Secrets(ns).Get(...)`
(client-go já decodifica base64), injeta só no comando executado dentro do pod. Nunca trafega de
volta pro frontend nem é gravado no audit log.

```bash
USER=$(kubectl get secret <secret> -n <namespace> -o jsonpath='{.data.username}' | base64 -d)
PASS=$(kubectl get secret <secret> -n <namespace> -o jsonpath='{.data.password}' | base64 -d)
```

**Host/porta via ConfigMap do K8s** (modo "manual"/"usuário e senha" → "Ler de um ConfigMap do
K8s", só disponível quando `auth.mode != connstring` — a connection string já embute host/porta):
mesmo espírito do Secret pra credenciais, mas ConfigMap porque host/porta não são dado sensível.
Útil quando o endereço do banco já está parametrizado num ConfigMap existente do próprio workload.
Chaves configuráveis (`host_key`/`port_key`, default `"host"`/`"port"`). Equivalente manual:

```bash
kubectl get configmap <configmap> -n <namespace> -o jsonpath='{.data.host}'
kubectl get configmap <configmap> -n <namespace> -o jsonpath='{.data.port}'
```

**Connection string completa via Secret/ConfigMap do K8s** (modo "Connection string" → "Ler de um
ConfigMap"/"Ler de um Secret do K8s"): cobre o caso comum de a URI já vir pronta, com a credencial
embutida, de um ConfigMap/Secret existente do workload — ex:

```
mongodb://svc_app:senha@10.104.15.38:27017/db_app?authSource=admin&replicaSet=rsApp&readPreference=primary&ssl=false&authMechanism=SCRAM-SHA-256
```

Diferente do host/porta (sempre ConfigMap, nunca sensível), aqui a fonte é **selecionável entre os
dois tipos** — `connstring_ref.kind: "configmap" | "secret"` — porque na prática essas URIs às
vezes ficam num Secret (contêm senha) e às vezes num ConfigMap comum (padrão observado em alguns
workloads legados, mesmo não sendo a prática mais segura — a ferramenta só lê o que já existe, não
julga onde foi guardado). Chave configurável (`key`, default `"connectionString"`). Equivalente
manual:

```bash
kubectl get configmap <configmap> -n <namespace> -o jsonpath='{.data.connectionString}'
# ou, se a fonte for Secret:
kubectl get secret <secret> -n <namespace> -o jsonpath='{.data.connectionString}' | base64 -d
```

**Gerenciado vs self-hosted**: a ferramenta não distingue os dois — RDS Postgres, Azure Database
for PostgreSQL, MongoDB Atlas, ou um StatefulSet self-hosted no próprio cluster são todos só
host/porta/autenticação vistos de fora. TLS costuma ser obrigatório em bancos gerenciados
(`sslmode=require`/`verify-full`, `tls=true`, `--tls`) — toggle único na UI, mapeado por engine no
backend.

**Seleção de Secret/ConfigMap por nome**: mesmo padrão de busca já usado pra cluster/namespace/
deployment (combobox com busca embutida) — a UI lista os Secrets/ConfigMaps reais do namespace
escolhido (`apiClient.getSecrets`/`getConfigMaps`) em vez do usuário digitar o nome às cegas, e
depois de selecionar o recurso, as chaves (`username_key`/`password_key`/`host_key`/`port_key`)
também viram um select populado com as chaves reais do Secret/ConfigMap (`dataKeys`, já retornado
pela mesma chamada de listagem — sem round-trip extra).

---

## Redis — banco por índice numérico e filtro de chaves

Redis não tem nomes de banco como os demais engines — só um índice numérico **0-15** (`SELECT n`
no protocolo, `-n <n>` no `redis-cli`), selecionado no mesmo campo "Database" da UI (rotulado
"Índice do banco" quando o engine é Redis). Omitido = banco 0 (default do Redis).

**Autenticação com usuário (ACL, Redis 6+)**: diferente dos outros engines, Redis tradicionalmente
autentica só com senha (`AUTH <senha>`) — usuário é opcional, só relevante com ACL configurada
(`AUTH <usuário> <senha>`, `redis-cli --user <usuário> -a <senha>`). O campo "Usuário" do modo
"Usuário e senha" é enviado como `--user` só quando preenchido; deixe em branco pra AUTH clássico
(só senha).

**Filtro de chaves no Explorar dados** (`redis_key_pattern`, só quando `engine="redis"`): o SCAN
usado pelo estágio de navegação (ver seção anterior) aceita um padrão glob via `MATCH`
(`redis-cli --scan --pattern "sessao:*"`) — sem isso, a amostra de até 100 chaves vem sem filtro
algum, o que é pouco útil numa instância com milhões de chaves de tipos variados. Campo exibido só
quando Engine=Redis e "Explorar dados" está ligado.

```bash
# Amostra sem filtro (equivalente a MATCH "*")
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'timeout 5s redis-cli -h <host> -p 6379 -a <senha> --no-auth-warning -n 3 --scan | head -n 100'

# Com padrão de chaves
kubectl exec <pod> -n <namespace> -c <container-efêmero> -- \
  sh -c 'timeout 5s redis-cli -h <host> -p 6379 -a <senha> --no-auth-warning --scan --pattern "sessao:*" | head -n 100'
```

---

## Guardrails

- RBAC: rotas de escrita (`POST /db-test/run`, `POST /db-test/cancel/:sessionId`) atrás de
  `rbacMiddleware.RequireSREGroup()`.
- Um teste por vez por usuário (`sync.Map` de lock, mesmo padrão do Kafka/Latency Test).
- Timeout hardcoded no backend (`dbTestMaxTimeoutMs = 15000`ms) — não confia só no valor mandado
  pelo frontend.
- Nenhum estágio escreve no banco — não há equivalente ao `confirm_produce` do Kafka porque não há
  nada que exija confirmação.
- Amostra do Redis limitada a 100 chaves (`dbRedisScanCap`) via `head`, nunca `KEYS *`.
- Toda execução é logada no `HistoryTracker` (`action: "db_test"`) — engine, host, namespace,
  deployment, pod alvo, container efêmero, status de cada estágio; nunca username/password/
  connection string.

## Onde conferir o estado do container efêmero depois

```bash
kubectl get pod <target_pod> -n <namespace> -o jsonpath='{.status.ephemeralContainerStatuses}'
kubectl describe pod <target_pod> -n <namespace>
```

---

## Mapa de arquivos

| Arquivo | O quê |
|---|---|
| `internal/web/handlers/db_test_tool.go` | Registry de engines (imagem, comandos, regexes de erro) + handler: resolução de pod/deployment, ephemeral container, os 2 estágios, SSE, guardrails |
| `internal/web/handlers/db_test_docker.go` | Pré-checagem de Docker (`GET /docker-status`, cache 20s) + reaper de containers órfãos (imediato no cancelamento + ticker periódico de 5min) |
| `internal/web/frontend/src/components/DatabaseTestTab.tsx` | UI: seletores cluster/namespace/deployment/engine, modo de autenticação, toggle de explorar dados, resultado com badges/lista de objetos, painel de pré-checagem de Docker no modo local |
| `internal/web/frontend/src/lib/api/types.ts` | Tipos `RunDBTestRequest`, `DBAuthConfig`, `DBTestResult`, etc. |
| `internal/web/frontend/src/lib/api/client.ts` | `runDBTest`/`getDBTestStreamURL`/`cancelDBTest` |
| `internal/web/server.go` | Registro das rotas `/api/v1/db-test/*` |

## Fora de escopo (por enquanto)

- Qualquer escrita real no banco (sem equivalente ao produce/consume do Kafka) — decisão de escopo
  deliberada, não uma limitação técnica.
- Engines além de PostgreSQL/MySQL-MariaDB/MongoDB/Redis (MSSQL, Cassandra, Elasticsearch, DynamoDB
  etc.) — arquitetura em registry (`dbEngines` em `db_test_tool.go`) deixa isso barato de adicionar
  depois, um novo item no map sem mexer no resto do handler.
- Execução de query arbitrária — viraria um console SQL/shell genérico, risco de segurança bem maior
  que listar databases/tabelas; fora do espírito de "teste de conectividade".
- Deixar escolher qual pod (quando o Deployment tem várias réplicas) ou qual container (pods
  multi-container) — mesma simplificação do Kafka, sempre o primeiro Running/primeiro container.
- Validado nesta rodada só por `go build`/`tsc --noEmit`/`rebuild-web.sh` — sem banco real disponível
  no ambiente de desenvolvimento para validar ponta a ponta contra os 4 engines; a imagem `mongo:7`
  em particular precisa de confirmação em produção de que `mongosh` vem embutido nessa tag.
- Pré-checagem de Docker e correção de saída bruta vazia validadas de verdade neste ambiente (que
  não tem `docker` instalado — reproduz o cenário real). O reaper de containers órfãos foi validado
  só por teste unitário da função pura de decisão de idade (`filterStaleContainers`,
  `db_test_docker_test.go`) — a limpeza imediata no cancelamento e o reaper periódico rodando de
  fato contra um daemon Docker real, cancelando um teste no meio, não foram exercitados ponta a
  ponta (sem Docker disponível aqui pra reproduzir).
