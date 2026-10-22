# Estudo + Plano: Descoberta de Rede ("Descoberta de Rota e Identificação de Destino por IP")

**Status:** ✅ Fases 1-5 (P1+P2+P3+P4) concluídas, validadas e mescladas — item real do
`ToolsMenu.tsx` hoje. Ver `CLAUDE.md`, seções "Descoberta de Rede (Fase 1)" a "(Fase 5)", pro
detalhe completo de cada camada, achados reais e bugs corrigidos ao vivo (SNI/hostname atrás de
bastion, porta/timeout de sonda configuráveis, etc.). **Roadmap de maturidade profissional (seção
10) completo — P1 a P4, todos concluídos.** Único item fora do roadmap por decisão já tomada: sonda
ICMP/UDP (esbarra em `CAP_NET_RAW`, não contornável sem mudança de infraestrutura).

**Nome decidido da ferramenta: "Descoberta de Rede"** (item do `ToolsMenu.tsx`).

**Pedido original do usuário:** uma ferramenta que, a partir de um IP informado, mostre a rota
(hop-a-hop) até ele e identifique o destino — se é um endereço Web, um servidor Linux ou Windows,
etc. — com visualização **gráfica**, pensada pra facilitar a leitura de um operador/analista SRE.
Pedido explícito: "pense em uma solução profissional e completa".

**Correção explícita do usuário, essencial pro escopo (resolve a Pergunta 2 da seção 9 original)**:
a ferramenta **não pode depender de o alvo estar dentro de um cluster do kubeconfig** — a empresa
tem servidores e VMs em locais remotos fora de qualquer cluster K8s gerenciado por esta app (ex:
infraestrutura hospedada na **IBM Kyndryl**). Isso muda a ênfase do plano: a identificação de
SO/serviço (seções 3.2/3.3/3.4) é o mecanismo **primário e obrigatório**, precisa funcionar sozinha
pra qualquer IP alcançável; o cross-reference contra a frota K8s (seção 3.8) vira um enriquecimento
**opcional/bônus** — acende quando o IP por acaso bate com algo conhecido, nunca um pré-requisito.
Ver seção 4.1 (modo de execução) pra como isso muda a arquitetura.

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
   firewall/load balancer)? **Precisa funcionar pra qualquer IP alcançável, dentro ou fora dos
   clusters do kubeconfig** — servidor/VM em datacenter próprio, em terceiro (ex: IBM Kyndryl), em
   nuvem pública fora desta app, etc. Se o IP também bater com um recurso da própria frota K8s
   (nó/pod/service), isso é um enriquecimento a mais, não o mecanismo de identificação em si.
3. **Contexto de origem/propriedade do IP** — a quem pertence (empresa/provedor), em qual
   ASN/rede, se é uma faixa de nuvem pública conhecida (Azure/AWS/GCP), se tem DNS reverso.
4. **Visualização gráfica** — não uma lista de texto (`traceroute` cru já existe pra isso), um
   grafo navegável, com os nós identificados visualmente (ícone/cor por tipo), pensado pra reduzir
   o tempo de leitura de um analista sob pressão (o mesmo motivo que já levou esta app a usar
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

### 3.8 Cross-reference interno — enriquecimento bônus, NUNCA pré-requisito

**Importante (correção explícita do usuário)**: esta camada só acende quando o IP informado por
acaso é um dos nós/pods/services dos clusters do kubeconfig — a maioria dos alvos reais (servidor
on-prem, VM em datacenter de terceiro como IBM Kyndryl, host de nuvem fora desta app) **nunca vai
bater aqui**, e a ferramenta precisa identificar SO/serviço igualmente bem nesse caso, usando só as
seções 3.2/3.3/3.4 (heurística de TTL, banner grab de porta, HTTP/TLS). Esta seção nunca pode virar
um gate — é estritamente um "se bater, ótimo, mostra o recurso; se não bater, segue normal com o
resto do fingerprint".

Dito isso, quando bate, nenhuma ferramenta de traceroute genérica (nem um `mtr` de terminal, nem um
nmap) sabe responder "esse IP é o node `aks-pool1-xxxxx` do cluster `akspriv-abastecimento-prd`" ou
"esse IP é o Pod do Deployment `checkout-api`" — **esta app já sabe**, e é um diferencial real que
vale a pena entregar como bônus, só não como base da identificação.

Fontes já existentes na app pra cruzar:
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

**Decisão (pedida explicitamente ao Claude Code pra decidir): cache-on-read persistido, leve —
nem live query pura, nem scan periódico completo da frota.** Nem uma coisa nem outra sozinha
respondia bem ao que o usuário pediu ("com a persistência a busca é mais rápida, indo em busca
apenas dos itens que não se tem salvo... mas deve ser leve"). O mecanismo:

- Um SQLite novo (mesmo padrão de todo `internal/storage/*_store.go` já existente nesta app — WAL
  mode, mesma família de `notes_store.go`/`cert_endpoints_store.go` — não um arquivo texto/JSON solto:
  consistência com o resto da app importa mais aqui do que a escolha específica de formato, e o
  volume de dado real por linha é minúsculo de qualquer jeito, então SQLite não pesa mais que JSON
  pra esse caso). Tabela única, ~5 colunas: `ip`, `kind` (node/pod/service), `name`, `namespace`,
  `cluster`, `cached_at`.
- **Nunca um scan de fundo/botão "Escanear frota" dedicado** (diferente do Node Pool Registry) — o
  índice se popula sozinho, de leitura em leitura: toda vez que uma consulta busca um IP contra
  os clusters do kubeconfig e encontra (ou não encontra) uma correspondência, o resultado é
  gravado/atualizado no cache. Consultas futuras pro MESMO IP são instantâneas (cache hit, sem
  tocar o K8s de novo); IPs nunca vistos custam a consulta live normal na primeira vez, e ficam
  rápidos dali em diante — exatamente "busca só o que não tem salvo", como o usuário descreveu.
- **TTL diferenciado por `kind`, não um único valor pra tudo** — cuidado real de corretude, não só
  performance: IP de **Pod é efêmero** (reagendamento/restart reusa o mesmo IP pra um pod
  completamente diferente em minutos/horas) — TTL curto (proposta: 2h, mesma ordem de grandeza já
  usada em outros TTLs "sensíveis a mudança" desta app, ex: cache de `ListNodeGroups`). IP de
  **Node/Service é muito mais estável** (raramente muda) — TTL bem mais longo (proposta: 24h, mesmo
  valor já usado pro Node Pool Registry/pricers). Sem essa distinção, o cache viraria uma fonte de
  **falso positivo perigoso** (afirmar com confiança que um IP "é o Pod X" quando na real já é
  outro pod há horas) — pior que não ter cache nenhum, e contrário ao princípio desta app de nunca
  afirmar com certeza o que não está mais confirmado.

---

## 4. Arquitetura proposta

### 4.1 Modo de execução

Reaproveitar o padrão dual já estabelecido (seção 2): **modo pod** (Ephemeral Container na imagem
`nicolaka/netshoot:v0.12` já usada, via `resolvePodForDeployment`/seletor de cluster+namespace+
deployment+pod já existente no Teste de Latência/Kafka/DB) e **modo local** (Docker no host via
`db_test_docker.go`, pré-checagem + reaper já compartilhados).

**Ajuste de ênfase pós-correção do usuário (ver início do documento)**: como o alvo típico é
frequentemente **fora** de qualquer cluster gerenciado por esta app (servidor/VM remota, ex. IBM
Kyndryl), o **modo local vira o caminho padrão/mais geral**, não uma alternativa secundária — o
host onde o backend roda está na rede corporativa (VPN/rota já existente pra alcançar esses
sistemas remotos, o mesmo caminho que um analista usaria manualmente), enquanto o modo pod só
alcança o que o egress **daquele cluster específico** consegue rotear (que pode ou não incluir a
rede da Kyndryl, dependendo de peering — não é garantido). Modo pod continua tendo valor real, só
que num caso de uso mais específico: diagnosticar conectividade **a partir da perspectiva de um
cluster específico** (ex: "por que o Pod X não consegue falar com o servidor Y" — aí importa
literalmente rodar de dentro daquele cluster, não do host do backend). Os dois modos continuam
valendo a pena, mas a UI/expectativa deveria deixar claro que **modo local é o default pra alvos
genéricos fora do kubeconfig**, e modo pod é a escolha certa quando a pergunta é especificamente
sobre a rede de um cluster.

Mesma ressalva de sempre nesta app: o alcance de qualquer um dos dois modos depende da rede que o
host (ou o cluster escolhido) realmente enxerga — se a VPN corporativa não tiver rota até o
datacenter da Kyndryl (ou estiver caída), a ferramenta vai reportar isso com honestidade
("inalcançável a partir desta origem"), não vai inventar um caminho que não existe. Mesmo padrão
já documentado nesta app pra VPN/conectividade de cluster (`checkReachability`).

**Confirmado pelo usuário (ver seção 9, Pergunta 4)**: existe VPN até a Kyndryl, e é **a mesma VPN
já usada pro AKS e pro GCP** — não uma rota nova/não testada, é a mesma infraestrutura de rede que
toda troca de cluster desta app já depende pra funcionar. Isso valida com confiança maior a escolha
de modo local como default (o host do backend, na mesma rede lógica dessa VPN, já prova
diariamente que o caminho existe) — resta só o cuidado de segmentação/firewall específico do lado
da Kyndryl por IP individual, que a VPN em si não garante (ver seção 9).

Camadas que **não** dependem do modo pod/local (rodam sempre do backend, sem custo de spawnar
container): DNS reverso (3.5), ASN/RDAP (3.6), faixas de nuvem (3.7), cross-reference interno
(3.8) — só o traceroute em si (3.1) e o banner grab de portas (3.3, quando o alvo só é alcançável
de dentro de uma rede privada — cluster K8s ou a rede que o host do backend enxerga) precisam do
container/host.

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

**Confirmado pelo usuário: campo único, aceita IP OU hostname/FQDN, nos dois sentidos** — não é só
"IP → identidade" (fluxo principal), é bidirecional: informar um IP resolve/identifica o destino
(seções 3.2-3.8); informar um hostname resolve pro IP primeiro (`net.LookupHost`/`net.LookupIP`,
padrão) e roda exatamente o mesmo pipeline dali em diante — as duas entradas convergem pro mesmo
fluxo, sem tela/modo separado. Detecção automática de qual é qual (regex de IPv4/IPv6 vs. o resto
tratado como hostname), sem precisar o usuário escolher um seletor "tipo de entrada" à parte.

Seletor de modo (pod/local, reaproveita `ClusterSelectorForTab`/seletor de namespace+deployment+pod
já existente nas outras ferramentas de teste ativo), botão "Traçar rota".

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

Novo item no dropdown `ToolsMenu.tsx` (18º item). **Nome decidido: "Descoberta de Rede"** — ver
justificativa na seção 9 (Pergunta 2, decisão do nome).

---

## 7. Fases de implementação (1-4 concluídas)

Reordenado depois da correção do usuário sobre escopo (fingerprint de SO precisa funcionar sozinho,
sem depender de cross-reference K8s — por isso a Fase de fingerprint subiu antes da Fase de
cross-reference, que agora é claramente marcada como bônus):

1. **✅ Fase 1 — traceroute básico + grafo** (mtr/traceroute via modo pod/local, sem nenhuma camada
   de enriquecimento ainda) — já entrega o "gráfico da rota", valor visível rápido. **Concluída**:
   `tcptraceroute` (não o `traceroute`/BusyBox), grafo Cytoscape com animação salto-a-salto ao vivo
   via SSE. Ver CLAUDE.md "Descoberta de Rede (Fase 1)".
2. **✅ Fase 2 — fingerprint do destino** (banner grab de portas + heurística de TTL + certificado
   TLS) — "é Web/Linux/Windows", funciona pra qualquer IP alcançável, dentro ou fora dos clusters
   do kubeconfig. Ver CLAUDE.md "Descoberta de Rede (Fase 2)".
3. **✅ Fase 3 — enriquecimento passivo** (DNS reverso, ASN/Cymru, faixas de nuvem AWS/GCP — Azure
   e RDAP ficaram de fora, ver seção 8 e o item P3 da seção 10). Ver CLAUDE.md "Descoberta de Rede
   (Fase 3)".
4. **✅ Fase 4 — cross-reference interno** (3.8) — bônus, cache-on-read persistido (SQLite),
   busca ao vivo só no modo pod. Ver CLAUDE.md "Descoberta de Rede (Fase 4)".

**Bugs reais corrigidos ao vivo depois das 4 fases** (todos documentados em detalhe no CLAUDE.md,
seção "Descoberta de Rede", não repetidos aqui pra não duplicar fonte de verdade): label do grafo
estourando o círculo do nó (3 rodadas de correção — texto flutuante separado, `text-max-width`
generoso, emoji removido do canvas por renderizar corrompido), SNI/Host usando o IP cru em vez do
hostname real (certificado "fake"/handshake falhando, hostname incorreto atrás de bastion/PAM), e
porta+timeout de sonda fixos (443/2s) impedindo alcançar hosts atrás de cofres PAM como o Delinea —
esse último **investigado a fundo e confirmado como bloqueio de rede genuíno** (firewall/boundary
de segurança do próprio cofre, não bug), documentado como limitação estrutural aceita.

---

## 10. Fase 5 — Roadmap de maturidade profissional (checklist vivo, retomável entre sessões)

Depois das Fases 1-4 (traceroute+fingerprint+enriquecimento+cross-reference, todas validadas ao
vivo) e de uma rodada real de troubleshooting contra um host atrás de um cofre PAM (Delinea) que
expôs a limitação mais dolorosa na prática — nenhuma memória entre buscas —, o usuário pediu uma
priorização explícita do que falta pra esta ferramenta virar algo usado no dia a dia, não só numa
investigação pontual. Prioridade combinada com o usuário (maior valor/menor risco primeiro):

- [x] **P1 — Histórico de Descobertas** ✅ **concluído, validado ao vivo** (ver checklist
      detalhado abaixo — CLAUDE.md "Descoberta de Rede (Fase 5)")
- [x] **P2 — Exportar resultado (PDF)** ✅ **concluído** — `internal/web/frontend/src/lib/
      netDiscoveryPdfExport.ts` (mesmo padrão de `nodePoolPdfGenerator.ts`: jsPDF+autoTable+
      logoUtils, sem mudança de backend). Botão "Exportar PDF" no resultado ao vivo e em cada
      execução do histórico expandido (Fase 5/P1). Ver CLAUDE.md "Descoberta de Rede (Fase 5) —
      Exportar PDF". **Não validado clicando no navegador** (sem ferramenta de automação de browser
      nesta sessão) — validado por `tsc`/`eslint` limpos + reuso do mesmo padrão já comprovado em
      produção por FinOps/Health Check/Node Pool Predictions.
- [x] **P3 — Faixas de IP da Azure no `cloud_match`** ✅ **concluído, validado ao vivo** —
      `fetchAzureRanges`/`parseAzureServiceTagsDoc` (`net_discovery_enrich.go`), via `az network
      list-service-tags` (sem URL JSON pública fixa como AWS/GCP). Achado real confirmado ao vivo
      ANTES de codar: o parâmetro `-l <região>` é cosmético pro CONTEÚDO — `-l brazilsouth` e
      `-l eastus` devolveram os mesmos 1556 registros globais, só o campo `region` de CADA entrada
      individual varia. Ver CLAUDE.md "Descoberta de Rede (Fase 5, item P3)".
- [x] **P4 — Múltiplos alvos em lote** ✅ **concluído, validado ao vivo** — decisão de design:
      fila SEQUENCIAL (não paralela), reaproveitando `runDiscovery` (net_discovery.go) SEM
      MODIFICAR NADA nele — cada alvo do lote é uma execução single-target normal, N delas em
      sequência dentro da mesma goroutine, cada uma com seu próprio session_id. Histórico (P1) e
      Exportar PDF (P2) funcionam de graça pra cada alvo, sem código extra. Frontend reaproveita
      100% o painel de resultado já existente (grafo/tabela/fingerprint) pra mostrar sempre "o
      alvo atualmente ativo na fila" — só uma faixa compacta de status acima mostra o progresso de
      todos os alvos, sem duplicar a renderização pesada. Ver CLAUDE.md "Descoberta de Rede
      (Fase 5, item P4)".
- **Fora do roadmap, decisão já tomada**: sonda ICMP/UDP como alternativa ao TCP — esbarra na
  mesma limitação de `CAP_NET_RAW` já documentada pro Teste de Latência (`internal/web/handlers/
  latency_test_tool.go`), não contornável sem mudança de infraestrutura mais profunda (privileged
  container). Não reabrir sem uma razão nova.

### 10.1 Checklist detalhado — P1: Histórico de Descobertas

**Objetivo**: persistir cada descoberta concluída (mesmo padrão já usado 4x neste projeto —
`SNATHistoryStore`, `LatencyTestHistoryStore`, `NetDiscoveryRegistryStore` da própria Fase 4) pra
que, ao buscar de novo o MESMO alvo, a ferramenta mostre o que já se sabe em vez de reinvestigar do
zero. Resolve diretamente a dor observada ao vivo nesta sessão (reinvestigar o mesmo host atrás do
Delinea do zero, múltiplas vezes, em conversas diferentes).

**Backend**:
- [x] `internal/storage/net_discovery_history_store.go` — novo store SQLite WAL (`net-discovery-
      history.db`), mesmo padrão de `NewNetDiscoveryRegistryStore`/`NewNotesStore`. Tabela única
      (schema achatado, sem normalizar em várias tabelas — o payload é auto-contido e só consultado
      por alvo/data, mesmo princípio de outras stores desta app que guardam um blob JSON quando o
      dado não precisa ser consultado por campo interno): `id, target_input, target_ip, mode,
      reached, hops_count, result_json (TEXT, o NetDiscoveryResult inteiro serializado), created_at
      DATETIME, created_by TEXT`.
- [x] Métodos: `Save(record)`, `GetRecentByTarget(targetInputOrIP string, limit int)` — casa por
      `target_input` (normalizado: trim+lowercase) OU `target_ip`, cobre o caso de o usuário
      alternar entre digitar o hostname e o IP resolvido do mesmo host —, `GetRecent(limit int)`
      (lista geral, usado só se a aba/lista de histórico completa entrar no escopo — opcional pro
      P1 mínimo). Retenção: reaproveitar o mesmo padrão de 90 dias já usado no SNAT History (poda
      automática, não deixar crescer sem limite).
- [x] Wiring em `server.go`: criação do store (mesmo bloco `if store, err := storage.New...`, log
      `✅ Net Discovery History Store inicializado`) + passagem pro `NewNetDiscoveryHandler` (mais
      um parâmetro, mesmo padrão já usado pro `registry` da Fase 4).
- [x] `runDiscovery` (`net_discovery.go`): chamar `h.historyStore.Save(...)` no fim, junto (não em
      vez) do `h.logHistory(...)` já existente — são stores DIFERENTES com propósitos diferentes
      (`HistoryTracker` é auditoria genérica da app inteira; este é específico pra "o que sei sobre
      este alvo", consultável de volta).
- [x] Endpoint novo: `GET /api/v1/net-discovery/history?target=<texto>` — devolve as últimas N
      (ex: 3) execuções pra aquele alvo, mais recente primeiro. Sem `RequireSREGroup()` (é
      consulta, não mutação — mesmo padrão de leitura do resto desta ferramenta).
- [x] Testes: store (`Save`+`GetRecentByTarget` round-trip, match por IP quando buscado pelo
      hostname e vice-versa, retenção/poda), handler (endpoint devolve JSON esperado, alvo nunca
      visto devolve lista vazia — não erro).

**Frontend**:
- [x] `apiClient.getNetDiscoveryHistory(target)` (`client.ts`) + tipo `NetDiscoveryHistoryEntry`
      (`types.ts`).
- [x] `NetDiscoveryTab.tsx`: `useQuery` disparada quando `target` tem valor (debounce ~400ms, mesmo
      padrão já usado noutras buscas desta app) — mostra um banner/card compacto ANTES mesmo de
      clicar "Traçar rota" quando há histórico: "Última busca: DD/MM HH:MM — alcançado: sim/não —
      SO: Linux/Windows/? — ver detalhes" (expansível, sem precisar rodar de novo). Nunca bloqueia
      o fluxo normal — é só um atalho informativo.
- [x] Formatação de data/hora: mesma convenção já usada no resto do frontend (`toLocaleString` no
      browser do cliente — nunca formatar hora no backend pra exibição, lição já documentada nesta
      app pro texto de notificação do Spinnaker).

**Validação (mesma disciplina das Fases 1-4)**:
- [x] `go build`/`go vet`/`gofmt` limpos; `npx tsc --noEmit -p tsconfig.app.json`/`eslint` limpos.
- [x] Testes automatizados passando (`go test ./internal/storage/... ./internal/web/handlers/...`).
- [x] `./rebuild-web.sh -b` + restart.
- [x] Validação ao vivo: rodar uma descoberta contra um alvo, confirmar persistência (`sqlite3
      ~/.k8s-hpa-manager/net-discovery-history.db "SELECT ..."`), rodar de novo contra o MESMO
      alvo e confirmar que o banner de histórico aparece com o resultado anterior correto.
- [x] `CLAUDE.md`: nova subseção "Descoberta de Rede (Fase 5) — Histórico de Descobertas", mesmo
      estilo narrativo das Fases 1-4 (o que foi feito, achados reais, validação ao vivo).
- [x] Commit + push na branch `feat/net-discovery-fase1` (mesma branch usada pra toda a ferramenta
      até aqui — não abrir uma nova só pra isso).
- [x] Marcar este checklist (P1) como concluído nesta própria seção do plano, e mover a atenção pro
      P2 (ou parar aqui se o usuário decidir que já basta).

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

## 9. Decisões (histórico de perguntas → respostas)

Todas as decisões que originalmente exigiam o usuário já foram tomadas. Registro histórico de cada
uma, na ordem em que surgiram:

**Pergunta 2 original (escopo) — RESOLVIDA pelo usuário**: o escopo é qualquer IP alcançável,
dentro ou fora dos clusters do kubeconfig — inclui servidores/VMs remotas de terceiros (ex: IBM
Kyndryl). Isso eleva 3.2/3.3/3.4 (fingerprint de SO/serviço) a mecanismo central e obrigatório, e
reclassifica 3.8 (cross-reference K8s) como enriquecimento opcional — ver correção no início do
documento e ajustes nas seções 3.8/4.1/7.

**Pergunta 4 original (rota de rede até a Kyndryl) — RESOLVIDA pelo usuário**: sim, há VPN até a
IBM Kyndryl — e é **a mesma VPN já usada pro AKS e pro GCP**. Confirmação forte, não só "existe uma
rota genérica": o host onde este backend roda já prova, todo dia, que essa VPN funciona (é o mesmo
caminho que `kubeconfig`/`GetRestConfig` usam pra falar com o `kube-apiserver` de qualquer cluster
AKS, e que `GetFreshGKEToken`/etc. usam pro GCP) — não é uma rota nova e não testada, é a mesma
infraestrutura de rede que a aplicação inteira já depende pra existir. Reforça com mais confiança
ainda a decisão da seção 4.1: **modo local é o caminho certo pra alcançar a Kyndryl**, sem precisar
rotear nada através de um pod específico. Único cuidado mantido: nada garante que TODA sub-rede da
Kyndryl esteja alcançável por essa VPN (segmentação/firewall interno do lado deles pra IPs
específicos que a VPN nunca tentou tocar até hoje) — a ferramenta ainda reporta "inalcançável" com
honestidade quando um IP específico não responder.

**Pergunta 1 (cross-reference: live query vs. registry) — RESOLVIDA, decisão delegada ao Claude
Code pelo usuário**: nem uma coisa nem outra sozinha — **cache-on-read persistido, leve** (SQLite,
~5 colunas, TTL curto pra Pod/longo pra Node/Service, sem scan periódico de fundo). Ver mecanismo
completo e justificativa na seção 3.8.

**Pergunta sobre entrada bidirecional (IP↔hostname) — confirmada pelo usuário**: o campo de entrada
aceita tanto IP quanto hostname/FQDN, nos dois sentidos, com detecção automática — não é preciso um
seletor de "tipo de entrada" separado. Ver seção 6.1.

**Nome da ferramenta — RESOLVIDO, decisão delegada ao Claude Code pelo usuário**: **"Descoberta de
Rede"**. O usuário sugeriu "Net Discovery" como ponto de partida, mas deixando claro que não tinha
certeza do nome ("qualquer solução de nome elegante") — todos os 18 itens hoje existentes no
`ToolsMenu.tsx` (documentados no `CLAUDE.md`: "Verificar Acesso", "Editor de Código",
"Dependências", "Teste de Latência", "Teste Kafka", "Teste de Banco de Dados", etc.) são nomeados
em português puro, sem mistura de inglês — manter esse padrão importa mais do que a sonoridade
específica de "Net Discovery" em si. "Descoberta de Rede" é a tradução direta da ideia original do
usuário (descoberta + rede), preservando a intenção, só que consistente com a convenção de
nomenclatura já estabelecida no menu inteiro.

**Item que segue genuinamente sem resposta explícita** (não bloqueia nada, é só uma confirmação
final antes de codificar): confirmar o padrão dual pod/local exatamente como nas outras 3
ferramentas de teste ativo desta app, com modo local como default — reforçado com confiança pela
resposta da Pergunta 4, mas nunca formalmente confirmado como "sim, é isso mesmo" pelo usuário.
