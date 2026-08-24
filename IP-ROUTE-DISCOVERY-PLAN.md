# Estudo + Plano: Descoberta de Rota e Identificação de Destino por IP

**Status:** 🔬 estudo/planejamento — nenhuma fase iniciada, nenhum código escrito.

**Pedido original do usuário:** uma ferramenta que, a partir de um IP informado, mostre a rota
(hop-a-hop) até ele e identifique o destino — se é um endereço Web, um servidor Linux ou Windows,
etc. — com visualização **gráfica**, pensada pra facilitar a leitura de um operador/analista SRE.
Pedido explícito: "pense em uma solução profissional e completa".

Este documento responde três perguntas: (1) quais mecanismos técnicos existem pra cada tipo de
informação pedida, verificados ao vivo nesta sessão (não é conhecimento de memória sem checagem);
(2) o que a própria aplicação já tem pronto e deveria ser reaproveitado, não reinventado; (3) uma
arquitetura e um plano faseado — mas **deixa decisões-chave em aberto** (seção 9) porque mudam a
forma da solução e são escolha do usuário, não algo que dá pra assumir.

---

## 1. O que a ferramenta precisa responder

Traduzindo o pedido em requisitos concretos:

1. **Caminho de rede** — de onde a análise parte até o IP informado, hop a hop, com latência por
   salto quando disponível.
2. **Classificação do destino** — é um servidor Web? Linux? Windows? Equipamento de rede (roteador/
   firewall/load balancer)? Um recurso da própria frota K8s desta aplicação (nó/pod/service)?
3. **Contexto de origem/propriedade do IP** — a quem pertence (empresa/provedor), em qual
   ASN/rede, se é uma faixa de nuvem pública conhecida (Azure/AWS/GCP), se tem DNS reverso.
4. **Visualização gráfica** — não uma lista de texto (`traceroute` cru já existe pra isso), um
   grafo navegável, com os nós identificados visualmente (ícone/cor por tipo), pensado pra reduzir
   o tempo de leitura de um analista sob pressão (o mesmo motivo que already levou a esta app usar
   Cytoscape.js em `LatencyTopologyGraph.tsx`/`ServiceMeshGraph.tsx`).

---

## 2. O que a própria aplicação já tem pronto — reaproveitar, não reinventar

Levantamento feito nesta sessão, olhando o código real (não por suposição):

- **Padrão dual "modo pod / modo local" já estabelecido e testado em produção** — Teste de
  Latência, Teste de Kafka e Teste de Banco de Dados (`internal/web/handlers/latency_test_tool.go`,
  `kafka_test_tool.go`, `db_test_tool.go`) já resolvem exatamente o mesmo dilema que uma ferramenta
  de rota/traceroute teria: rodar via **Ephemeral Container** dentro de um pod real do cluster
  (pra que a análise reflita a rede/perspectiva DAQUELE cluster) ou via **Docker local no host**
  (pré-checagem + reaper de containers órfãos já compartilhados em `db_test_docker.go`, reaproveitado
  também pelo Kafka Test). Uma ferramenta de rota deveria seguir o mesmo padrão dual, não inventar
  um terceiro mecanismo.
- **A imagem de troubleshooting já usada (`nicolaka/netshoot:v0.12`) já traz TUDO que esta
  ferramenta precisa, sem imagem nova pra vetar**: `traceroute`, `mtr`, `dig`, `nslookup`, `whois`,
  `nc`, `curl`, `openssl s_client`, `ping`. Confirmado em `latency_test_tool.go:36`
  (`latencyTestPodImage = "nicolaka/netshoot:v0.12"`, já usada pelo modo ICMP do Teste de Latência).
  Isso elimina de cara a maior fonte de risco/atrito de um plano desse tipo (escolher e validar uma
  imagem de container nova).
- **`internal/certificates/endpoint_check.go` (`CheckEndpointTLS`) já faz handshake TLS direto
  contra `host:porta` arbitrário e extrai o certificado completo** — construído originalmente pro
  Monitor de Certificados Externos, mas é EXATAMENTE o mecanismo certo pra responder "isso é um
  servidor Web com TLS, e qual certificado ele apresenta" (CN/SAN/Issuer/validade) sem escrever
  nada novo — só reaproveitar a função contra o IP informado.
- **`internal/kubernetes/node_methods.go`/`client.go` já expõem `Node.Status.Addresses` e
  `Service.Spec.ClusterIP`** (confirmado por grep) — a base pra cruzar um IP contra o inventário
  já carregado da frota (ver seção 3.8) sem precisar de nenhuma chamada nova ao K8s além das que
  outras abas já fazem.
- **Padrão de cache de chamada externa com TTL** (documentado no `CLAUDE.md`, usado por
  `AzurePricer`/`GCPPricer`/`IsGcloudAuthActive`/etc.) — mesma receita pra cachear os feeds de IP
  de nuvem pública (seção 3.7), que mudam raramente e não devem ser buscados a cada consulta.
- **`internal/web/frontend/src/components/LatencyTopologyGraph.tsx`** é praticamente um protótipo
  já pronto do grafo que esta ferramenta precisa — nós com cor por provider (mesma paleta
  `PROVIDER_COLORS` já usada em badges AKS/EKS/GKE em `useCloudProvider.ts`), arestas com latência
  como label, zoom/pan/fit já resolvidos. A ferramenta de rota é, estruturalmente, uma
  especialização LINEAR (cadeia de hops, não um grafo geral) desse mesmo componente.

**Conclusão da seção**: tecnicamente, boa parte do trabalho pesado (imagem de troubleshooting,
padrão dual pod/local, handshake TLS pra fingerprint Web, grafo Cytoscape) já existe e só precisa
ser apontado pra um novo alvo. O trabalho novo de verdade é: orquestrar o traceroute em si, os
mecanismos de identificação de OS/ASN/nuvem (nenhum existe ainda), o cruzamento contra o inventário
interno, e a UI do grafo hop-a-hop + painel de detalhe.

---

## 3. Camadas de informação e mecanismos técnicos

### 3.1 Caminho de rede (hop-a-hop)

Três variantes de traceroute existem, com trade-offs reais (não é só "rodar o traceroute e
pronto"):

| Variante | Como funciona | Prós | Contras |
|---|---|---|---|
| **ICMP** (`traceroute` padrão) | Envia pacotes com TTL crescente, lê ICMP Time-Exceeded de cada salto | Mais simples, mais suportado | Muitos firewalls/NSGs corporativos e de nuvem **bloqueiam ICMP** — hops aparecem como `* * *` mesmo existindo. Exige `CAP_NET_RAW` no container (mesma limitação **já documentada nesta app** pro modo ICMP do Teste de Latência: "pod sem CAP_NET_RAW pode não conseguir abrir socket ICMP raw, dependendo do sysctl/PodSecurityPolicy") |
| **UDP** (`traceroute -U`) | Mesma técnica de TTL, mas com pacote UDP pra uma porta alta | Passa por alguns firewalls que bloqueiam ICMP puro | Ainda depende de o firewall devolver ICMP Time-Exceeded, que também pode estar bloqueado |
| **TCP** (`traceroute -T -p 443`, equivalente ao `tcptraceroute`) | TTL crescente com pacote TCP SYN pra uma porta real (ex: 443) | **O mais confiável em ambiente corporativo/nuvem** — like um SYN de verdade pra uma porta de serviço tem muito mais chance de atravessar firewalls do que ICMP/UDP crus, porque parece tráfego legítimo | Ainda precisa saber uma porta provavelmente aberta no destino (assume 443/80 por padrão, mas nem todo host responde nelas) |

**Recomendação**: `traceroute -T` (TCP) como modo padrão, com fallback pra ICMP se o TCP falhar
completamente (nenhum hop respondeu) — mesmo espírito de "tenta o mecanismo mais provável de
funcionar primeiro, degrada com honestidade" já usado em `internal/certificates`
(Prometheus→nginx primeiro, `EnrichWithTLSDial` como fallback). Complemento: `mtr --report -c 5`
(já presente no netshoot) dá o mesmo dado que `traceroute` só que com múltiplas amostras de
latência por salto (min/avg/max/perda de pacote) — melhor pra visualizar "qual salto está
degradado", não só "qual é o caminho". Proposta: rodar `mtr` como mecanismo primário (mais rico)
com `traceroute -T` como fallback caso `mtr` não esteja disponível/falhe.

**Limitação a comunicar ao usuário, não escondida**: mesmo com TCP, alguns hops legitimamente não
respondem (roteadores configurados pra não gerar ICMP, NAT que reescreve o caminho, cloud load
balancers que escondem a topologia real atrás de um único IP virtual). O grafo deve mostrar hops
`* * *` como nós "desconhecido" no meio da cadeia (não escondê-los), e nunca afirmar "esse é o
caminho real" quando é, na verdade, "esse é o caminho que respondeu".

### 3.2 Fingerprint de sistema operacional (heurística de TTL)

Técnica clássica e bem conhecida, mas **é heurística, não certeza** — deve ser rotulada como tal
na UI (mesmo princípio de fraseologia neutra já usado em `ChainValidationResult`/
`TrustedByPublicCA` desta app: nunca afirmar como fato o que é inferência).

TTL inicial padrão por SO (valor observado na resposta = TTL inicial − nº de saltos já
percorridos; a heurística arredonda pra cima até o próximo valor "redondo" conhecido):

| TTL observado (aprox.) | TTL inicial provável | SO provável |
|---|---|---|
| ≤ 64 | 64 | Linux / macOS / *BSD / maioria dos dispositivos de rede modernos |
| 65–128 | 128 | Windows |
| 129–255 | 255 | Solaris antigo / alguns equipamentos Cisco/roteadores |

Fonte do TTL: a própria resposta do `ping`/`mtr`/`traceroute` já usados na seção 3.1 (o campo
`ttl=` já aparece no stdout que a app já sabe parsear — mesmo regex-parsing de stdout já usado em
`icmpTimeRegex` no Teste de Latência, só que capturando o TTL em vez do tempo).

### 3.3 Fingerprint de serviço/porta (banner grab em portas conhecidas)

**Escopo deliberadamente limitado** — não é um scan de porta completo (nmap `-p-` de 65535 portas
levaria minutos e se parece com um scan agressivo, o que pode disparar alerta de IDS/SOC mesmo
dentro da própria rede da empresa). Proposta: testar um conjunto curado de ~15-20 portas
conhecidas com timeout curto (1-2s cada, em paralelo), goroutines com `net.DialTimeout`:

`22 (SSH), 80/443 (HTTP/HTTPS), 445 (SMB), 3389 (RDP), 21 (FTP), 25/587 (SMTP), 53 (DNS),
3306 (MySQL), 5432 (Postgres), 6379 (Redis), 27017 (MongoDB), 9200 (Elasticsearch), 8080/8443
(HTTP alternativo), 5985/5986 (WinRM)`.

Onde a porta abre, banner grab passivo (ler os primeiros bytes que o próprio protocolo manda sem
precisar enviar nada — SSH manda `SSH-2.0-...` na conexão, SMTP/FTP mandam `220 ...`) — não é
scan agressivo, é só "a porta respondeu e disse o quê" com timeout curto. Sinal de OS reforçado
por porta: `3389`/`445`/`5985` abertas = forte indício de Windows; `22` aberta = forte indício de
Linux/Unix (mais confiável que a heurística de TTL sozinha).

### 3.4 "É um servidor Web?" — HTTP + certificado TLS

Já coberto quase por completo pela seção 2: `CheckEndpointTLS` (reaproveitado) responde se há TLS
válido/o que o certificado diz. Complementar com uma checagem HTTP simples (`curl -sI` já presente
no netshoot, ou `net/http` puro do lado do backend se o alvo for alcançável direto do servidor) pra
capturar o header `Server:` quando presente (nem todo servidor expõe, mas quando expõe é um sinal
forte — ex: `Server: nginx`, `Server: Microsoft-IIS/10.0`, o segundo sendo outro forte indício de
Windows).

### 3.5 DNS reverso

`net.LookupAddr` (biblioteca padrão do Go, sem precisar de shell-out) resolve PTR usando o DNS que
o **servidor** (backend) enxerga. Complementar com `dig -x <ip>` dentro do pod quando o modo
escolhido for "pod" — útil pra IPs internos que só resolvem via o DNS interno do cluster (CoreDNS),
que o backend rodando fora do cluster não enxerga.

### 3.6 WHOIS / RDAP / ASN — verificado ao vivo nesta sessão

**RDAP** (Registration Data Access Protocol, sucessor estruturado do WHOIS — JSON, não texto pra
parsear) é a fonte certa aqui, não `whois` cru. Testado ao vivo: `GET https://rdap.org/ip/8.8.8.8`
segue redirect (302) pro RIR certo (ARIN/RIPE/APNIC/etc. conforme o range) e devolve JSON com
`entities` (organização dona do range), `cidr0_cidrs` (bloco), datas de registro. `rdap.org` é o
bootstrap oficial da IANA — não precisa saber de antemão qual RIR responde por aquele IP.

**ASN — mecanismo mais barato e confiável, também verificado ao vivo**: consulta DNS TXT contra o
serviço público da Team Cymru (`net.LookupTXT`, sem shell-out nem HTTP):
```
dig +short 8.8.8.8.origin.asn.cymru.com TXT
→ "15169 | 8.8.8.0/24 | US | arin | 2023-12-28"
dig +short AS15169.asn.cymru.com TXT
→ "15169 | US | arin | 2000-03-30 | GOOGLE - Google LLC, US"
```
Duas consultas DNS (rápidas, cacheáveis) dão ASN + bloco + país + nome da organização — mais
barato e mais consistente entre RIRs do que fazer parsing da estrutura `entities` do RDAP (que
varia de formato entre ARIN/RIPE/APNIC). Proposta: Cymru como fonte primária de ASN/organização,
RDAP como enriquecimento opcional (bloco exato, datas de registro) quando o usuário expandir o
detalhe de um hop.

### 3.7 Reconhecimento de faixas de IP de nuvem pública

Três fontes, uma por provider, verificadas ao vivo nesta sessão (as duas HTTP retornaram `200`):

- **AWS**: `https://ip-ranges.amazonaws.com/ip-ranges.json` — JSON estável, URL fixa, sem
  autenticação. Campo `prefixes[].ip_prefix` (CIDR) + `region` + `service` (ex: `AMAZON`, `EC2`,
  `CLOUDFRONT`). Confirmado ao vivo: JSON bem-formado, ~200KB.
- **GCP**: `https://www.gstatic.com/ipranges/cloud.json` — mesmo padrão, URL fixa, sem auth.
  Confirmado ao vivo: `200`.
- **Azure**: **sem URL JSON fixa** — a Microsoft publica um arquivo novo toda semana num link de
  download que muda de nome (`ServiceTags_Public_YYYYMMDD.json`), atrás de uma página de captura
  de link (id=56519), não uma API estável. Mecanismo correto pra esta app (que já shell-a pro
  Azure CLI extensivamente, com timeout, padrão já documentado no `CLAUDE.md`): **`az network
  list-service-tags --location <region>`** — devolve os mesmos dados (Service Tags, que incluem
  `AzureCloud`, por região) via CLI já autenticado, sem depender de raspar uma página de download.
  Não verificado ao vivo nesta sessão (exigiria `az` autenticado), mas é comando documentado oficial
  da Microsoft e seria o único dos três providers que aproveita infraestrutura de auth que a app já
  tem — reaproveita o padrão de "cache de chamada CLI com TTL" já usado por outras integrações
  Azure (`nodepool_registry`/`AzurePricer` etc.).

Todas as três fontes devem ser baixadas periodicamente (não a cada consulta) e cacheadas — mesmo
padrão de TTL diário já usado por `AzurePricer`/`GCPPricer` (`sqlite`, TTL 24h). Match contra as
faixas via `net.ParseCIDR`/`Contains` — puramente aritmético, sem custo de rede por consulta depois
do cache aquecido.

### 3.8 Cross-reference interno — o diferencial real desta ferramenta

Nenhuma ferramenta de traceroute genérica (nem um `mtr` de terminal, nem um nmap) sabe responder
"esse IP é o node `aks-pool1-xxxxx` do cluster `akspriv-abastecimento-prd`" ou "esse IP é o Pod do
Deployment `checkout-api`". **Esta app já sabe** — é a única fonte de vantagem competitiva real
que faz sentido investir aqui, porque é o que nenhuma ferramenta de mercado replica sem acesso ao
mesmo inventário.

Fontes já existentes na app pra cruzar (sem persistir nada novo, ao menos na v1 — ver decisão em
aberto na seção 9):
- `Node.Status.Addresses` (InternalIP/ExternalIP) → nome do node, node pool, cluster.
- `Pod.Status.PodIP`/`HostIP` → nome do pod, Deployment/DaemonSet/StatefulSet dono, namespace,
  cluster (mesmo padrão de resolução de owner já usado no badge de uso de ConfigMaps,
  `resolveOwnerDisplayName`).
- `Service.Spec.ClusterIP`/`.Status.LoadBalancer.Ingress[].IP` → nome do Service, namespace,
  cluster — cobre também o caso de "esse IP é o Load Balancer externo de tal Service".
- **Node Pool Registry** (`internal/storage/nodepool_registry_store.go`) — já correlaciona nome de
  VM/nó com pool/cluster/cloud provider, útil como atalho quando o IP já bate com algo ali sem
  precisar de uma consulta live ao K8s.

Quando o IP bate com algo conhecido, o nó do grafo (seção 6) deveria virar clicável, levando direto
pra aquele recurso noutra aba da app (mesmo espírito de "conectar as pontas" já usado em vários
lugares desta app, ex: link CHG→ServiceNow, link execução→Spinnaker Deck).

---

## 4. Arquitetura proposta

### 4.1 Modo de execução

Reaproveitar o padrão dual já estabelecido (seção 2): **modo pod** (Ephemeral Container na imagem
`nicolaka/netshoot:v0.12` já usada, via `resolvePodForDeployment`/seletor de cluster+namespace+
deployment+pod já existente no Teste de Latência/Kafka/DB) e **modo local** (Docker no host via
`db_test_docker.go`, pré-checagem + reaper já compartilhados). A escolha do modo muda de onde a
"origem" do traceroute enxerga a rede — é uma decisão real do usuário em cada consulta, não algo
pra esconder atrás de um automatismo.

Camadas que **não** dependem do modo pod/local (rodam sempre do backend, sem custo de spawnar
container): DNS reverso (3.5), ASN/RDAP (3.6), faixas de nuvem (3.7), cross-reference interno
(3.8) — só o traceroute em si (3.1) e o banner grab de portas (3.3, quando o alvo só é alcançável
de dentro do cluster) precisam do container.

### 4.2 Endpoints REST propostos

```
POST /api/v1/ip-route/trace         — inicia a descoberta (SSE, como Health Check/Command Runner)
GET  /api/v1/ip-route/trace/:id     — stream de progresso (broker SSE já existente)
GET  /api/v1/ip-route/enrich?ip=    — só as camadas "baratas" (DNS reverso/ASN/nuvem/cross-ref),
                                       sem traceroute — útil pra popular o painel de detalhe de um
                                       hop já descoberto sem re-rodar o traceroute inteiro
```

SSE porque um traceroute com `mtr -c 5` pode levar dezenas de segundos (30 hops × ciclos de
amostra) — mesmo padrão de feedback incremental já usado em Cordon/Drain, Health Check, Command
Runner (broker em `internal/web/sse/progress.go`).

### 4.3 Cache

Feeds de nuvem pública (3.7): TTL 24h, SQLite (mesmo padrão de `AzurePricer`). Resultado de
ASN/RDAP por IP (3.6): TTL mais curto mas ainda cacheável (ex: 6h) — ASN de um IP praticamente
nunca muda de um dia pro outro. Resultado de traceroute em si: **não cachear** — é uma leitura de
estado de rede no momento, cachear tornaria a ferramenta enganosa exatamente no cenário em que ela
mais importa (rede instável, incidente em andamento).

---

## 5. Modelo de dados (contrato JSON, rascunho)

```go
type IPRouteHop struct {
    Index       int     `json:"index"`
    IP          string  `json:"ip,omitempty"`       // vazio = hop não respondeu ("*")
    RTTMs       float64 `json:"rtt_ms,omitempty"`
    PacketLoss  float64 `json:"packet_loss_pct,omitempty"` // via mtr
    ReverseDNS  string  `json:"reverse_dns,omitempty"`
    ASN         string  `json:"asn,omitempty"`
    ASNOrg      string  `json:"asn_org,omitempty"`
    CloudMatch  string  `json:"cloud_match,omitempty"` // "aws"|"gcp"|"azure"|""
    InternalRef *InternalResourceRef `json:"internal_ref,omitempty"` // node/pod/service conhecido
}

type IPRouteDestination struct {
    IP           string   `json:"ip"`
    ReverseDNS   string   `json:"reverse_dns,omitempty"`
    ASN          string   `json:"asn,omitempty"`
    ASNOrg       string   `json:"asn_org,omitempty"`
    CloudMatch   string   `json:"cloud_match,omitempty"`
    InternalRef  *InternalResourceRef `json:"internal_ref,omitempty"`
    OSGuess      string   `json:"os_guess,omitempty"`       // "linux"|"windows"|"" — SEMPRE com...
    OSConfidence string   `json:"os_confidence,omitempty"`  // ...um confidence junto (ex: "heurística de TTL")
    OpenPorts    []PortResult `json:"open_ports,omitempty"`
    TLSCert      *TLSCertSummary `json:"tls_cert,omitempty"` // reaproveita CheckEndpointTLS
    IsWebServer  bool     `json:"is_web_server"`
}

type InternalResourceRef struct {
    Kind      string `json:"kind"` // "node"|"pod"|"service"
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
    Cluster   string `json:"cluster"`
    // link pra abrir esse recurso direto na aba certa da app
}
```

---

## 6. Design de UI

### 6.1 Entrada

Campo de IP (ou hostname — resolve pra IP antes de traçar), seletor de modo (pod/local, reaproveita
`ClusterSelectorForTab`/seletor de namespace+deployment+pod já existente nas outras ferramentas de
teste ativo), botão "Traçar rota".

### 6.2 Grafo hop-a-hop (Cytoscape)

Especialização linear de `LatencyTopologyGraph.tsx` — nós em cadeia (Origem → Hop 1 → ... →
Destino), não um grafo geral. Cor/ícone por classificação:

- **Cinza** — hop desconhecido (não respondeu).
- **Azul/laranja/verde** — mesma paleta `PROVIDER_COLORS` já usada em badges AKS/EKS/GKE, quando
  o hop bate com uma faixa de nuvem pública conhecida (3.7).
- **Roxo** (cor de marca da app) — hop reconhecido como recurso interno da própria frota (3.8),
  com o nome do recurso como label em vez do IP cru.
- **Nó de destino** sempre maior/destacado, com um badge de ícone: 🌐 (Web/TLS), 🐧 (Linux
  provável), 🪟 (Windows provável), ❓ (sem sinal suficiente pra inferir) — nunca afirmando com
  certeza o que é heurística.

Arestas com RTT como label (mesmo padrão de `p95ms` já usado no grafo de latência).

### 6.3 Painel de detalhe por nó

Clicar num hop abre um painel lateral (mesmo padrão de detalhe-ao-clicar já usado no
`ServiceMeshGraph.tsx`/`LatencyTopologyGraph.tsx`) com: IP, DNS reverso, ASN/organização, match de
nuvem, se é recurso interno conhecido (com link pra abrir na aba certa), portas abertas +
banner (só no nó de destino), certificado TLS quando aplicável.

### 6.4 Integração no ToolsMenu

Novo item no dropdown `ToolsMenu.tsx` (18º item) — nome de trabalho sugerido: "Rota de Rede" ou
"Diagnóstico de IP".

---

## 7. Fases de implementação propostas

1. **Fase 1 — traceroute básico + grafo** (mtr/traceroute via modo pod/local, sem nenhuma camada
   de enriquecimento ainda) — já entrega o "gráfico da rota", valor visível rápido.
2. **Fase 2 — enriquecimento passivo** (DNS reverso, ASN/Cymru, RDAP, faixas de nuvem) — camadas
   que não dependem do modo pod/local, adicionáveis sem re-arquitetar nada da Fase 1.
3. **Fase 3 — cross-reference interno** (3.8) — a peça mais valiosa/diferenciada, mas também a que
   mais depende de decidir "live query ou registry persistido" (seção 9).
4. **Fase 4 — fingerprint do destino** (banner grab de portas + heurística de TTL + certificado
   TLS via `CheckEndpointTLS` reaproveitado) — "é Web/Linux/Windows".
5. **Fase 5 — polimento de UI** (painel de detalhe completo, link pra abrir recurso interno na aba
   certa, exportar/copiar resultado).

---

## 8. Limitações conhecidas / riscos (a comunicar, não esconder)

- **Hops "invisíveis" são o normal, não uma falha da ferramenta** — firewalls corporativos, NSGs
  de nuvem e NAT escondem topologia real com frequência. A UI precisa deixar isso claro (mesmo
  princípio de fraseologia honesta desta app: "sinal encontrado" vs. "nenhum sinal — não confirma
  ausência").
- **Fingerprint de SO é heurística, nunca certeza** — TTL pode ser alterado por proxy/NAT no
  caminho; banner grab pode estar desabilitado/customizado. Rotular sempre como "provável".
- **Scan de portas, mesmo limitado, pode disparar alerta de SOC/IDS interno** — mesmo com só ~15-20
  portas bem conhecidas e sem varredura sequencial agressiva, é tecnicamente uma atividade de
  reconhecimento de rede. Recomendação: documentar claramente na própria UI que a ferramenta é
  destinada a IPs da própria infraestrutura/autorizados, não pra varrer IPs de terceiros — mesma
  postura de uso responsável já assumida no resto da app (ex: Access Checker é sobre a própria
  frota, não sobre terceiros).
- **CAP_NET_RAW pode estar ausente no cluster-alvo** (mesma limitação já documentada pro Teste de
  Latência ICMP) — o modo TCP do traceroute (3.1) mitiga isso parcialmente (não depende de socket
  ICMP raw pra ENVIAR, mas ainda depende de raw socket pra LER as respostas ICMP Time-Exceeded em
  alguns SOs/kernels) — a ferramenta precisa degradar com uma mensagem clara em vez de falhar
  silenciosamente, mesmo padrão já usado no Teste de Latência.
- **Azure Service Tags não tem URL JSON fixa** (única das 3 fontes de nuvem que depende de CLI
  autenticado em vez de um download público estável) — ver 3.7.

---

## 9. Perguntas antes de começar (decisões do usuário, não assumidas)

1. **Cross-reference interno (seção 3.8, Fase 3): live query por cluster selecionado, ou um
   registry persistido (SQLite) tipo Node Pool Registry, alimentado por scan?** Live query é mais
   simples (KISS) e sempre fresco, mas exige que o usuário informe/selecione contra qual(is)
   cluster(s) cruzar (não dá pra cruzar contra a frota inteira a cada consulta sem custo). Registry
   persistido permite cruzar contra TODA a frota já escaneada instantaneamente, mas herda a mesma
   limitação já documentada do Node Pool Registry/Deployment Registry (só sabe o que já foi
   escaneado, pode ficar desatualizado).
2. **Escopo de alvo: só IPs internos/da própria infraestrutura, ou qualquer IP incluindo internet
   pública?** Muda o peso relativo das camadas — se for só interno, RDAP/ASN/faixas de nuvem
   (3.6/3.7) viram só um "bônus" e o foco real é 3.1+3.8; se for qualquer IP, essas camadas viram
   centrais.
3. **Nome definitivo da ferramenta / onde entra no menu** — "Rota de Rede"? "Diagnóstico de IP"?
   Outro nome que já faça sentido pro time.
4. **Confirma o modo dual pod/local como está usado nas outras 3 ferramentas de teste ativo desta
   app**, ou este caso tem alguma particularidade que pede um mecanismo diferente (ex: rodar
   sempre a partir do host, nunca de dentro de um pod)?
