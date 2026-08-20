# Estudo + Plano: Integração com Zabbix

**Status:** 🔬 estudo/planejamento — nenhuma fase iniciada, nenhum código escrito, mas **todas as
perguntas bloqueantes da seção 5 já foram respondidas** (2026-08-19) — Fases 1, 2 e 4 (nível de
nó) podem ser estimadas/iniciadas sem suposições pendentes. Revisado também contra o estado atual
da ferramenta (ver seção 0): conclusões da Rev. 1 mantidas, só a Fase 4 foi realinhada pra um
padrão mais leve, validado por uma integração real feita nesse meio-tempo (Spinnaker).
**Confirmado com o usuário**: Zabbix está instalado (servidor próprio) e em uso real monitorando
vários clusters e VMs desta empresa (2026-08-11); versão 7.x, host Zabbix = nome do nó K8s
(correlação determinística), token de serviço só-leitura já existe, "integração K8s" é Zabbix
Agent clássico por VM sem noção de pod/namespace (2026-08-19, ver seção 5 para o detalhe de cada
resposta).
Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` (seções "Dynatrace" e
"Health Checking" para o padrão de correlação já existente que este plano reaproveita).

**Pergunta original do usuário:** existe API para o Zabbix, como funciona, e que benefícios
isso agregaria à aplicação. Este documento responde as duas primeiras perguntas com uma
pesquisa verificada contra a documentação oficial (não é conhecimento de memória sem
checagem) e propõe um plano faseado para a terceira — as decisões-chave que dependiam da
instalação real (seção 5) já foram todas confirmadas com o usuário.

---

## 0. Atualização (2026-08-19) — o que mudou na ferramenta desde a Rev. 1

A Rev. 1 deste estudo (2026-08-11/12) comparava a proposta de Zabbix só contra o padrão já
existente do **Dynatrace** (`Correlate()` em `internal/healthcheck/correlator.go`: correlação
dedicada, mapa K8s↔DT por workload, 8ª aba própria "K8s↔DT" nos resultados do Health Check).
Nos ~7 dias seguintes, a integração **Spinnaker** (`SPINNAKER-INTEGRATION-PLAN.md`) foi implementada
de ponta a ponta (Fases 1-4 + seção 9, validada ao vivo) e inclui, na seção 9 item 7, uma **segunda
fonte externa correlacionada no Health Check** — mas com uma arquitetura mais leve que a do
Dynatrace, relevante o suficiente pra revisar a Fase 4 deste plano:

- **`internal/healthcheck/spinnaker_enricher.go`**: em vez de uma correlação dedicada tipo
  `Correlate()` (mapa completo + tab própria), o `SpinnakerEnricher` só resolve **uma vez por
  cluster** (login + 1 busca de execuções, custo comparável ao `ResourceEnricher`/Prometheus já
  existente) e expõe um método simples — `RecentRollback(deployment, namespace, currentVersion)`
  — chamado durante a checagem normal de cada Deployment. O resultado vira só **3 campos novos**
  em `DeploymentHealth`/`CorrelatedK8sIssue` (`SpinnakerRecentRollback`, `SpinnakerRollbackCHG`,
  `SpinnakerRollbackAt`), escalando `StatusHealthy`→`StatusWarning` quando aplicável — sem tab
  própria, exibido como badge na aba "Relatório" (`HealthReportTab.tsx`) já existente.
- **Opt-in via checkbox** (`HealthCheckRequest.CheckSpinnakerRollback`), condicionado a
  `CheckDeployments` já estar marcado — mesmo padrão de opt-in que este plano já previa pro
  Zabbix na Fase 4, confirmado como o caminho certo.
- **Best-effort de verdade, testado ao vivo**: se o Spinnaker estiver indisponível ou o cluster
  não tiver ambiente reconhecido, `NewSpinnakerEnricher` retorna erro que o orchestrator só loga
  (`Debug`/`Warn`) — nunca derruba o Health Check inteiro. Mesmo contrato que este plano já
  assumia por analogia ao Dynatrace, agora com um segundo exemplo real confirmando que o padrão
  se generaliza bem para uma terceira fonte.

**Impacto na Fase 4 deste plano (revisado abaixo, seção 4)**: o esforço/risco da correlação
Zabbix↔K8s cai em relação à Rev. 1 — não é mais necessário decidir entre "replicar o modelo
pesado do Dynatrace" ou inventar algo novo; existe agora um template leve, já provado com uma
segunda integração, que se encaixa bem no formato de dado do Zabbix (`problem.get` por host →
resolver host↔workload uma vez por cluster → enriquecer `DeploymentHealth`, sem exigir uma nova
aba dedicada tipo "K8s↔Zabbix"). Isso **não** resolve a pergunta em aberto mais importante (seção
5, item 3 — se existe convenção de nome/IP/tag pra casar host Zabbix com nó/workload K8s), só
reduz o custo de implementar a correlação **depois** que essa convenção for confirmada.

**Distinção importante que o exemplo Spinnaker também deixa clara**: Spinnaker usa login SSO
**por usuário** (matrícula/email do Perfil SSO, `SSOProfileModal.tsx`) porque a resposta da API
precisa refletir quem tem permissão de ver aquela application — não é o mesmo modelo do Zabbix,
que usa um **API Token de serviço** (Fase 2 original, `UserTokensStore` com `ZabbixURL`/
`ZabbixAPIToken`, mesmo padrão de credencial compartilhada já usado por Dynatrace/GitHub). A Fase
2 deste plano **não muda** por causa do precedente Spinnaker — os dois modelos de credencial
coexistem na aplicação por motivos diferentes, e Zabbix se encaixa no modelo Dynatrace, não no
Spinnaker.

Nenhuma outra mudança recente na ferramenta (certificados, Dependencies, Teams, EKS/TLS) afeta
as conclusões da Rev. 1 — revisadas e mantidas como estavam, exceto a Fase 4 (seção 4 abaixo).

---

## 1. Existe API? Sim — JSON-RPC 2.0, madura e bem documentada

Fonte: [documentação oficial Zabbix — API](https://www.zabbix.com/documentation/current/en/manual/api),
[problem.get](https://www.zabbix.com/documentation/current/en/manual/api/reference/problem/get).

- **Protocolo**: JSON-RPC 2.0 puro sobre HTTP(S). Um único endpoint por instância:
  `https://<zabbix-host>/api_jsonrpc.php`. `Content-Type: application/json-rpc`.
- **Formato de request**: `{"jsonrpc":"2.0","method":"<grupo>.<ação>","params":{...},"id":1}`.
  Resposta: `{"jsonrpc":"2.0","result":[...],"id":1}` ou `{"jsonrpc":"2.0","error":{...},"id":1}`.
- **Autenticação — dois mecanismos**:
  1. **API Token** (recomendado, Zabbix ≥ 5.4): criado em *Users → API tokens* na UI, com
     expiração opcional. Usado via header `Authorization: Bearer <token>` — não expõe usuário/senha,
     é revogável individualmente, e é exatamente o mesmo padrão já usado por este projeto para
     Dynatrace (`Api-Token` fixo, sem sessão) e GitHub (PAT). **Este é o mecanismo recomendado
     para este projeto** — evita reimplementar `user.login`/gestão de sessão.
  2. **Login tradicional** (`user.login` com usuário/senha) → retorna um token de sessão
     temporário, usado do mesmo jeito via header `Authorization: Bearer <token>`. Necessário só em
     Zabbix < 5.4 ou se a empresa não permitir criar API tokens dedicados.
- **Métodos relevantes pra este projeto** (todos seguem o padrão `<grupo>.get`):
  - `problem.get` — problemas em aberto (ou já resolvidos, via `recent`). Filtros: `hostids`,
    `groupids`, `severities` (0–5: Not classified → Disaster), `tags`, `time_from`/`time_till`,
    `acknowledged`, `suppressed`. Retorna `eventid`, `name`, `severity`, `clock` (Unix epoch),
    `r_eventid`/`r_clock` (evento de resolução, se já resolvido), `tags`, `acknowledges`.
  - `event.get` — histórico de eventos (superset de `problem.get`, inclui já resolvidos e não
    só triggers).
  - `host.get` — inventário de hosts (nome visível, hostgroups, interfaces/IP, status,
    inventário customizado se preenchido). É a peça que decide se a correlação com K8s é viável
    (ver seção 3).
  - `hostgroup.get` — grupos de hosts (equivalente a "management zone" do Dynatrace ou tags).
  - `trigger.get` — definição das triggers (o "gatilho" que gera um problem; útil pra saber
    a expressão/threshold que disparou).
  - `history.get` / `trend.get` — série histórica de um item (métrica). `history.get` para
    granularidade fina e janelas curtas, `trend.get` para agregados horários em janelas longas
    (Zabbix armazena `history` com retenção curta e `trends` com retenção longa — arquitetura
    diferente de Prometheus/Dynatrace, que guardam granularidade única por período).
  - `item.get` — definição dos itens coletados por host (nome, chave, unidade) — necessário pra
    descobrir *quais* `itemid`s existem antes de chamar `history.get`/`trend.get`.
- **Versionamento**: a versão da API acompanha a versão do servidor Zabbix (atual: 7.4; 8.0 em
  desenvolvimento). Compatibilidade retroativa dentro de major version; mudanças de schema podem
  ocorrer entre majors — o cliente Go deveria expor a versão detectada (`apiinfo.version`, método
  público sem autenticação) e logar um aviso se for muito antiga (< 5.4, sem API token nativo).

**Conclusão da parte 1**: tecnicamente é uma integração tranquila — mesmo formato de "cliente HTTP
fino + JSON puro" que já existe para Dynatrace (`internal/dynatrace/client.go`) e foi cogitado
para New Relic (`FINOPS-NR-METRICS.md`, NerdGraph). Nenhuma biblioteca Go nova necessária
(`net/http` + `encoding/json` bastam, mesmo padrão dos outros dois).

---

## 2. O que o Zabbix cobre que Dynatrace/Prometheus hoje não cobrem nesta aplicação

Esse é o ponto central para decidir se vale a pena. Dynatrace, nesta aplicação, é usado
essencialmente para **APM + Kubernetes** (problems por workload/entidade K8s, correlacionados
via `internal/healthcheck/correlator.go`). Prometheus é usado para métricas de cluster
(conntrack, SNAT, HPA histórico, alertas). **Nenhum dos dois enxerga infraestrutura fora do
cluster** — e Zabbix, no mercado, é tipicamente usado exatamente para isso: VMs, hosts físicos,
switches/roteadores, storage, bancos de dados on-prem, serviços de rede — a mesma motivação que
já levou este projeto a construir o **Monitor de Certificados Externos** do zero (
`EXTERNAL-CERT-MONITOR-PLAN.md`) em vez de depender de `blackbox_exporter`, porque parte da frota
está fora de qualquer cluster K8s.

Isso é uma **suposição baseada no uso típico de Zabbix**, não um fato confirmado sobre a
instalação desta empresa — ver seção 5 antes de implementar qualquer coisa.

### 2.1. Nova aba "Zabbix" no Tools menu — problems/alertas de infraestrutura (maior valor, menor incerteza)

Mesmo padrão já provado com o Dynatrace (`DynatraceTab.tsx` + `internal/web/handlers/dynatrace.go`):
lista de problems ativos, filtro por host group/severidade, botão "Analisar com AI" reaproveitando
`internal/ai` + `internal/sanitizer` (os problems do Zabbix — texto livre de trigger — passam pelo
mesmo sanitizador antes de qualquer prompt, igual ao já feito para logs/problems Dynatrace).
Não depende de nenhuma suposição sobre nomenclatura de host — funciona com qualquer Zabbix
configurado, mesmo sem nenhuma correlação com K8s. **Candidato à Fase 1** por ser o menor risco.

### 2.2. Correlação no Health Check (K8s ↔ Zabbix) — maior valor, mas depende de nomenclatura

Réplica do padrão Dynatrace (`Correlate()` em `internal/healthcheck/correlator.go`): se um
Deployment está degradado E o Zabbix reporta um problem no host que o hospeda (nó AKS/EKS como
VM, ou uma dependência externa — banco, load balancer, storage), a severidade final escala e o
usuário vê os dois lados no mesmo card, igual à 8ª aba "K8s↔DT" do Health Check hoje.
**O bloqueio real** é o mesmo já documentado para Dynatrace (`extractNodePoolFromName`,
convenção `aks-<pool>-XXXXXXXX-vmssXXXXX`): a correlação só funciona se o **nome do host no
Zabbix** puder ser mapeado para um **nó/namespace/workload K8s** de alguma forma determinística
(nome de VM igual ao node name, IP do host batendo com IP interno do node, hostgroup com o nome
do cluster, tag customizada, etc.). Sem confirmar isso contra a instalação real, esta fase não
pode ser estimada com segurança.

### 2.3. FinOps — métricas históricas para infraestrutura fora de nuvem pública (especulativo)

A cadeia de métricas do FinOps hoje é **DT (AKS) → NR (EKS, planejado) → Prometheus (fallback)**
(`FINOPS-DT-METRICS.md`, `FINOPS-NR-METRICS.md`). Nenhuma dessas fontes cobre nós self-managed/
bare-metal/on-prem, porque não há Azure/AWS Retail Pricing API pra precificar hardware próprio.
Zabbix **poderia** entrar como fonte de CPU/mem histórico (via `trend.get` num item tipo
`system.cpu.util`/`vm.memory.size[pused]`) para esse cenário — mas **só se** os nós K8s
relevantes tiverem Zabbix Agent instalado E o custo de referência (preço do hardware/aluguel do
DC) for alimentado manualmente, já que não existe "Retail Prices API" para datacenter próprio.
Marcado como especulativo porque depende de dois fatores desconhecidos: (a) se existe hardware
self-managed na frota desta empresa hoje, e (b) se o Zabbix já coleta esses itens.

### 2.4. Contexto histórico em Teste de Latência / Teste de Banco de Dados (baixo esforço, valor incerto)

`LATENCY-METRICS-PLAN.md` e `DATABASE-TEST-PLAN.md` já buscam contexto histórico DT/Prometheus
antes de rodar um teste ativo contra um host. Se o alvo do teste for um host monitorado pelo
Zabbix (ex: banco on-prem), mostrar "Zabbix já reporta este host como down há 12 min" antes de
gastar tempo com um teste ativo é uma melhoria de UX pequena e de baixo risco — mas só faz
sentido depois que a Fase 1 (cliente + endpoint de teste) já existir; não é uma fase própria.

### 2.5. O que **não** parece valer a pena duplicar

- **Certificados**: o Monitor de Certificados Externos já resolve isso sem depender de nada
  externo (handshake TLS direto, `internal/certificates/endpoint_check.go`). Se o Zabbix já tem
  "web scenarios" checando os mesmos certificados, são duas fontes da mesma informação — não
  há necessidade de unificar, e o monitor desta app tem vantagens próprias (nome do certificado
  extraído, comparação de emissor, etc.) que Zabbix não replica nativamente.
- **Alertas de cluster K8s (HPA, conntrack, SNAT)**: Prometheus já é a fonte certa — nativo,
  granular, já integrado em 4+ pontos da aplicação. Zabbix não teria acesso a essas métricas a
  menos que alguém já tenha montado scraping K8s→Zabbix, o que reintroduziria a mesma pergunta
  de nomenclatura da seção 2.2.

---

## 3. Mapeamento de dados — Zabbix ↔ modelo já usado nesta app

| Zabbix | Equivalente Dynatrace já usado nesta app | Observação |
|---|---|---|
| `host` (nome visível + hostgroups + interfaces) | `EntityStub` (HOST) | Zabbix não tem hierarquia de entidade K8s nativa — sem plugin/agente K8s, um "host" Zabbix é só uma VM/dispositivo |
| `problem` (severidade 0–5, `clock`, `r_clock`) | `Problem`/`DynatraceHealth` | Escala de severidade **diferente** — precisa de tabela de conversão própria (`0 Not classified`…`5 Disaster` → `Severity` interno do projeto), mesmo trabalho já feito para o Health Check com `statusToSeverity`/pesos |
| `tags` no problem | `Tags` do problem DT | Zabbix permite tags customizadas por trigger — se a empresa já tagueia com nome de namespace/cluster, a correlação da seção 2.2 fica trivial em vez de heurística por nome |
| `trend.get` (agregado horário, retenção longa) | métricas Prometheus/DT | Zabbix separa `history` (granular, retenção curta) de `trends` (agregado, retenção longa) — arquitetura de storage diferente, precisa decidir qual usar por caso de uso (snapshot recente vs. tendência) |

---

## 4. Plano de implementação (fases, só a partir da confirmação da seção 5)

Numeração e formato seguem o padrão já usado em `FINOPS-NR-METRICS.md`/`KAFKA-TEST-PLAN.md` —
cada fase é independentemente entregável, e a Fase 1 sozinha já gera valor sem exigir nenhuma
suposição sobre nomenclatura de host.

### Fase 1 — Pacote `internal/zabbix/` (cliente + problems, sem correlação ainda)

**Arquivos a criar:**
- `internal/zabbix/client.go`
- `internal/zabbix/models.go`

```go
// client.go
type Client struct {
    baseURL    string // ex: https://zabbix.empresa.com.br (sem /api_jsonrpc.php)
    apiToken   string // API Token (Users > API tokens) — Bearer
    httpClient *http.Client
}

func NewClient(baseURL, apiToken string) (*Client, error)

// call executa um método JSON-RPC genérico e decodifica o "result" em dest.
func (c *Client) call(ctx context.Context, method string, params interface{}, dest interface{}) error

func (c *Client) APIVersion(ctx context.Context) (string, error)          // apiinfo.version, sem auth
func (c *Client) GetProblems(ctx context.Context, opts ProblemFilter) ([]Problem, error)
func (c *Client) GetHosts(ctx context.Context, opts HostFilter) ([]Host, error)
func (c *Client) GetHostGroups(ctx context.Context) ([]HostGroup, error)
```

```go
// models.go
type Problem struct {
    EventID    string   `json:"eventid"`
    Name       string   `json:"name"`
    Severity   int      `json:"severity"` // 0-5
    Clock      string   `json:"clock"`    // unix epoch, como string (API retorna string)
    RClock     string   `json:"r_clock"`  // vazio = ainda aberto
    Acknowledged string `json:"acknowledged"`
    Tags       []Tag    `json:"tags"`
    Hosts      []Host   `json:"hosts,omitempty"` // via output extend + selectHosts
}

type Host struct {
    HostID     string       `json:"hostid"`
    Host       string       `json:"host"`       // nome técnico
    Name       string       `json:"name"`       // nome visível
    Groups     []HostGroup  `json:"groups,omitempty"`
    Interfaces []Interface  `json:"interfaces,omitempty"`
}

func SeverityToInternal(z int) Severity // 0-5 → Severity do projeto (mesmo enum de healthcheck)
```

- Timeout 30s (mesmo padrão do cliente Dynatrace).
- **Sem cache próprio** — replicar decisão do NR: quem chama decide TTL/cache.
- Testar autenticação via `problem.get` com `limit: 1` (não existe endpoint de "ping" dedicado
  além de `apiinfo.version`, que não exige token).

### Fase 2 — Credenciais + handler + rotas

**`internal/storage/user_tokens_store.go`** — mesmo padrão de `DynatraceURL`/`DynatraceToken`:
```go
ZabbixURL      string `json:"zabbix_url,omitempty"`
ZabbixAPIToken string `json:"zabbix_api_token,omitempty"`
```
+ `GetZabbixConfig() (url, token string, ok bool)`.

**`internal/web/handlers/zabbix.go`** (novo, espelha `dynatrace.go`):
- `GET /api/v1/zabbix/config` — config atual sem expor o token
- `POST /api/v1/zabbix/test` — chama `APIVersion` + `GetProblems(limit:1)`
- `GET /api/v1/zabbix/problems` — filtro por host group/severidade, mesmo shape de resposta do
  endpoint Dynatrace equivalente pra reaproveitar componentes de frontend
- `POST /api/v1/zabbix/problems/:eventId/analyze` — análise AI (sanitizer + provider já
  configurado do usuário, mesmo fluxo do `dynatrace.go`)

### Fase 3 — Frontend: aba "Zabbix" no Tools menu

- `ToolsMenu.tsx`: novo item `{ id: "zabbix", label: "Zabbix", icon: AlertTriangle }` (ou ícone
  próprio — o logo do Zabbix é um "Z" estilizado, sem equivalente direto no lucide-react; usar
  `Server`/`AlertTriangle` por ora).
- `ZabbixTab.tsx` (novo): lista de problems (tabela ordenável por severidade/tempo aberto),
  filtro por host group, botão "Analisar com AI" por problem — reaproveitar o máximo de UI já
  existente em `DynatraceTab.tsx`/`DynatraceMetricsPanel.tsx` em vez de recriar do zero.
- Configuração de credenciais: mesma seção de AI Settings/tokens onde Dynatrace já vive
  (`profile/` — verificar componente exato na hora, ex. `AISettingsModal.tsx` ou modal próprio).

### Fase 4 — Correlação no Health Check (condicionada à seção 5; revisada 2026-08-19)

Só inicia depois de confirmado que existe uma forma determinística de casar host Zabbix ↔
nó/workload K8s (nome, IP ou tag). **Réplica do padrão `SpinnakerEnricher`** (mais barato que o
`Correlate()` do Dynatrace — ver seção 0), não uma correlação dedicada nova:

- `internal/healthcheck/zabbix_enricher.go` (CRIAR) — `NewZabbixEnricher(ctx, cluster)` resolve
  **uma vez por cluster**: busca `problem.get` filtrado por hosts do cluster (via a convenção de
  nome/IP/tag confirmada na seção 5.3) e guarda em memória; método
  `ProblemsForHost(hostRef string) []zabbix.Problem` (ou por node name, dependendo do que a seção
  5.3 confirmar) consultado durante a checagem normal de cada Deployment/Node. Nunca é fatal —
  erro só é logado (`Debug`/`Warn`), Health Check segue sem o sinal extra.
- `HealthCheckRequest.CheckZabbixProblems bool` (opt-in, condicionado a `CheckDeployments` ou
  `CheckNodes` já marcados, conforme o alvo da correlação — nós ou workloads). Credenciais
  **sempre** do `UserTokensStore` (Fase 2), nunca pedidas de novo na request — diferente do
  Dynatrace hoje (que pede na request por herança histórica) e igual ao que a Fase 2 deste plano
  já previa.
- Campos novos em `DeploymentHealth`/`CorrelatedK8sIssue` (mesmo padrão de
  `SpinnakerRecentRollback`/`SpinnakerRollbackCHG`/`SpinnakerRollbackAt`): algo como
  `ZabbixHasOpenProblem bool` + `ZabbixProblemName`/`ZabbixProblemSeverity` — escalando severidade
  quando aplicável (`StatusHealthy`→`StatusWarning`, ou `Critical` se a severidade Zabbix mapeada
  for alta — ver `SeverityToInternal` da Fase 1).
- **Sem aba nova dedicada** — exibido como badge na aba "Relatório" (`HealthReportTab.tsx`) já
  existente, mesmo lugar onde o badge de rollback recente do Spinnaker apareceu. Evita replicar o
  esforço de UI que a 8ª aba "K8s↔DT" exigiu, que só se justifica pelo volume/granularidade
  específica dos problems Dynatrace (correlação completa workload↔entidade, não só um sinal
  booleano por host).

### Fase 5 — FinOps (só se a seção 2.3 for confirmada como aplicável)

Réplica de `FINOPS-NR-METRICS.md` Fase 1-2 (enricher + wiring no `Calculator.BuildReport`), com
`trend.get` como fonte em vez de NRQL. Não detalhado aqui até confirmar que faz sentido.

---

## 5. Perguntas antes de começar qualquer fase além da 1 — ✅ TODAS RESPONDIDAS (2026-08-19)

Este plano evitava comprometer esforço em cima de suposições. As 5 perguntas abaixo foram
confirmadas diretamente com o usuário em 2026-08-19 — **nenhum bloqueio restante pras Fases 1, 2
e 4 (nível de nó)**. Fase 3 (FinOps) segue como estava (seção 2.3, ver nota abaixo).

1. **Versão do Zabbix em uso** — ✅ **7.x (atual)**. API Token nativo (Bearer) disponível, mecanismo
   recomendado desde o início (seção 1) confirmado como o caminho certo — não precisa de
   `user.login`/gestão de sessão.
2. **O que está de fato monitorado no Zabbix desta empresa?** — ✅ **VMs/hosts físicos, incluindo
   as próprias VMs que são nós dos clusters K8s** (via Zabbix Agent clássico instalado no SO —
   ver item 2b). Não foi confirmado monitoramento de switches/rede ou bancos on-prem nesta rodada
   (não perguntado diretamente); se relevante, reconfirmar antes da Fase 3.
   - **2b. Que tipo de "integração K8s" existe?** — ✅ **Zabbix Agent clássico rodando dentro das
     VMs-nó, sem noção de pod/namespace/deployment** (monitora CPU/memória/disco/processos do SO,
     nada nativo de Kubernetes/LLD). Isso **confirma** a premissa original da tabela da seção 3
     ("sem plugin/agente K8s, um host Zabbix é só uma VM") — não precisa revisar o mapeamento.
     Também significa: **sem sobreposição real com Prometheus/Dynatrace no nível de workload** —
     o Zabbix aqui enxerga só a VM/SO por baixo do nó, nunca o pod/deployment rodando nela. Pode
     haver sobreposição parcial com métricas de nó (CPU/mem do host) que Prometheus/node exporter
     também expõem — mas os thresholds/triggers do Zabbix são configurados independentemente e
     podem carregar tuning operacional próprio (histórico de incidentes reais), então mesmo essa
     sobreposição parcial tem valor como segunda fonte de alerta, não é dado redundante descartável.
3. **Existe convenção de nome/IP/tag que permita casar um host Zabbix com um nó ou workload
   K8s desta aplicação?** — ✅ **Sim — nome do host Zabbix = nome do nó K8s.** Correlação
   determinística e simples: mesma string usada em `node.Name` do K8s aparece como `host`/`name`
   no Zabbix. **Destrava a Fase 4 sem heurística** — `ZabbixEnricher.ProblemsForHost` pode indexar
   direto por nome de nó, sem precisar de matching por IP/regex/tag (mais simples que o
   `extractNodePoolFromName` que o Dynatrace precisa pra VMSS).
4. **Há (ou pode ser criado) um usuário/token de API só-leitura dedicado?** — ✅ **Já existe um
   usuário/token de serviço só-leitura.** Fase 2 pode usá-lo diretamente — nada a negociar/criar
   antes de começar.
5. **Faz sentido pra esta empresa hoje?** — ✅ Já confirmado antes (2026-08-11): Zabbix está em
   uso real monitorando vários clusters e VMs, não é infra legada em descomissionamento.

**Conclusão**: as 4 perguntas antes em aberto (1, 3, 4, e o desdobramento de 2) têm resposta
favorável e sem ambiguidade — **Fases 1, 2 e 4 (correlação a nível de nó) podem ser estimadas e
iniciadas sem mais nenhuma suposição pendente.** A única área que segue especulativa é a Fase 3/
seção 2.3 (FinOps para infra self-managed): as VMs-nó identificadas aqui são nós de cluster K8s
normais (AKS/EKS/GKE), já precificados via API de cada cloud — não são hardware bare-metal
self-managed sem fonte de preço, então a motivação original da seção 2.3 (precificar o que a
Retail Pricing API não cobre) **não se aplica a essas VMs especificamente**. Se houver outra
categoria de host (bancos on-prem, hardware próprio fora de qualquer cluster) coberta pelo Zabbix,
isso ainda não foi confirmado — reconfirmar só quando a Fase 3 for cogitada.

---

## 6. Arquivos a criar/modificar (resumo, quando aprovado)

```
internal/zabbix/client.go                          ← CRIAR (Fase 1)
internal/zabbix/models.go                           ← CRIAR (Fase 1)
internal/storage/user_tokens_store.go               ← MODIFICAR (Fase 2)
internal/web/handlers/zabbix.go                      ← CRIAR (Fase 2)
internal/web/server.go                               ← MODIFICAR (Fase 2 — registrar rotas)
internal/web/frontend/src/components/ToolsMenu.tsx   ← MODIFICAR (Fase 3)
internal/web/frontend/src/components/ZabbixTab.tsx    ← CRIAR (Fase 3)
internal/web/frontend/src/lib/api/client.ts           ← MODIFICAR (Fase 3 — novos métodos)
internal/healthcheck/zabbix_enricher.go               ← CRIAR (Fase 4, condicional — padrão SpinnakerEnricher, ver seção 0)
internal/healthcheck/orchestrator.go                  ← MODIFICAR (Fase 4, condicional — wiring do opt-in, mesmo ponto do CheckSpinnakerRollback)
internal/finops/zabbix_enricher.go                    ← CRIAR (Fase 5, condicional)
internal/finops/calculator.go                         ← MODIFICAR (Fase 5, condicional)
```

---

## 7. Dependências externas

Nenhuma biblioteca Go nova — `net/http` + `encoding/json`, mesmo padrão de Dynatrace e do plano
New Relic. Não há SDK oficial Go mantido pela Zabbix; existem wrappers de terceiros
(`github.com/claranet/go-zabbix-api`, `github.com/cavaliercoder/go-zabbix`) mas seguem o mesmo
princípio de "JSON-RPC fino" — não trazem benefício sobre escrever o cliente próprio, e evitam
puxar uma dependência externa de manutenção incerta pra vendor/.

---

## Fontes consultadas

- [Zabbix Manual — API](https://www.zabbix.com/documentation/current/en/manual/api)
- [Zabbix API Reference — problem.get](https://www.zabbix.com/documentation/current/en/manual/api/reference/problem/get)
