# Estudo + Plano: Integração com Delinea Vault (Secret Server) — SSH (Linux) e RDP (Windows)

**Status:** 🔬 estudo/planejamento — nenhuma fase iniciada, nenhum código escrito.
**Confirmado com o usuário (2026-08-26)** — 4 das 10 perguntas originais da seção 8 já
respondidas, com impacto real na arquitetura (detalhado nas seções 2.1 e 5 abaixo):
1. Instância é **Cloud (Delinea SaaS)**, não on-prem.
2. **Não haverá conta de serviço única** — cada analista terá sua própria API key pessoal. Isso
   muda o modelo de credencial de "um segredo compartilhado" (como Dynatrace/Zabbix hoje) para
   "um segredo por usuário" — o mesmo padrão já usado nesta app para `GitHubEditorProfile`/tokens
   de IA por usuário (`UserTokensStore` chaveado por `user_email`), não um padrão novo.
3. Os secrets-alvo **exigem aprovação/checkout** — ⚠️ **revisado logo depois** (ver "Rodada 3"
   abaixo): não é um humano aprovando a liberação da senha. No Linux é uma tela de **escolha de
   launcher**; no Windows é **exclusão mútua entre analistas SRE** (não deixa dois conectados ao
   mesmo tempo). O desenho de "aguardando aprovação" das seções 5/6 ficou mais brando do que a
   primeira leitura sugeria — ver correção completa abaixo.
4. Os servidores **só são alcançáveis através do SSH Proxy/bastion do próprio Delinea** — a
   "Opção A" (SSH direto do backend) do rascunho original está **descartada**; a arquitetura
   inteira da seção 5 foi reescrita em cima do mecanismo confirmado (`SSH Terminal`, comando
   `launch <secret_id>`).

**Rodada seguinte (mesma sessão)**: usuário confirmou que os servidores Linux hoje são acessados
via **PuTTY** (lançado pelo Launcher do Delinea — "muito fraco como recurso") e que também existe
uma frota de **servidores Windows via RDP**, perguntando como trazer isso pra dentro desta
aplicação também. Seção 6 (nova) cobre esse levantamento — a conclusão principal é que RDP é
**arquiteturalmente mais complexo** que SSH aqui: não dá pra reaproveitar o terminal de texto do
Code Editor (RDP é protocolo gráfico), e o mecanismo de "launch" documentado do lado Delinea para
RDP é bem menos claro que o `launch <secret_id>` do SSH Terminal — ver seção 6 para o porquê e a
arquitetura proposta (Apache Guacamole auto-hospedado como ponte).

**Rodada 3 (mesma sessão) — correção real de entendimento sobre "Checkout/Approval"**: ao pedir
uma lista de benefícios da integração, o usuário corrigiu a premissa da confirmação #3 acima —
"Checkout/Approval Workflow só existe em servidores linux para escolher entre terminais a serem
usado como o PuTTY. já em servidores windows é feito para travar o uso simultâneo por mais de um
analista SRE." Ou seja: **não existe aprovação humana como a documentação genérica da Delinea
descreve** (seção 3) — o que existe de fato nesta empresa é:
- **Linux**: a tela que apareceu como "aprovação" é uma **escolha de launcher** — hoje há 3
  configurados (**PuTTY, MobaXterm, WinSCP**), e só o PuTTY roda sem exigir configuração dedicada
  na máquina do analista (confirmado pelo usuário: MobaXterm e WinSCP precisam de algum setup
  local extra). Isso reforça ainda mais o valor da ponte via SSH Terminal (seção 5) — ela não é só
  "melhor que PuTTY", é a única opção hoje que não depende de nenhuma configuração por analista.
  **Bônus percebido, não escopado ainda**: como WinSCP (transferência de arquivo) também está na
  lista de launchers, e esta aplicação já tem um navegador SFTP embutido pronto
  (`SFTP-FILE-BROWSER-PLAN.md`, server+client do `pkg/sftp` conectados via `net.Pipe()` no mesmo
  processo), uma fase futura poderia apontar essa mesma infraestrutura para os hosts do Delinea
  via SSH Terminal — substituindo o WinSCP local do mesmo jeito que a Fase 4 substitui o PuTTY,
  sem exigir nenhum protocolo novo (é o mesmo SSH Terminal, só outro subcomando/canal).
- **Windows**: o Checkout ali é exclusão mútua de verdade — impede dois SREs conectados ao mesmo
  servidor simultaneamente. Isso bate exatamente com a semântica documentada de `Checkout`/
  `ForceCheckIn` (seção 3), só que sem aprovação humana no meio — é "ocupado agora" ou "livre
  agora", não "aguardando alguém aprovar". Isso muda a Fase Windows-2 (seção 7): em vez de um
  fluxo de espera de aprovação, o desenho precisa mostrar **ocupação em tempo real** ("em uso por
  fulano desde HH:MM") e, com RBAC adequado, uma opção de liberar (`ForceCheckIn`) — nunca uma
  tela de "aguarde a aprovação".

Seções 5, 6 e 8 abaixo já refletem essa correção.

**Pedido original do usuário:** o usuário tem uma API key do Delinea Vault e quer (1) listar
servidores/IPs/SO/heartbeat/informações de cada host cadastrado no cofre, (2) filtrar os que são
Linux, (3) construir uma ponte que abra um terminal — reaproveitando o mesmo terminal já usado na
aba **Code Editor** desta aplicação — já autenticado no servidor selecionado, buscando a
credencial (que roda em rotação diária) automaticamente no momento da conexão. Pediu também um
estudo profundo de tudo que a API do Delinea pode oferecer, não só o necessário para este recurso;
depois estendeu o pedido para cobrir também os servidores Windows via RDP.
**Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`** (seção "Terminal integrado" do
Code Editor, para o protocolo WebSocket a reaproveitar no caso SSH, e as menções a "cofre
Delinea"/"bastion" na seção "Descoberta de Rede", que já é evidência indireta de qual produto
Delinea está em uso nesta empresa — ver seção 1 abaixo).

Este documento segue o mesmo padrão de rigor já estabelecido em `ZABBIX-INTEGRATION-PLAN.md`:
fatos verificados contra a documentação oficial da Delinea (`docs.delinea.com`) são marcados como
tal; qualquer coisa que dependa de como a instância real desta empresa está configurada, ou que a
pesquisa não confirmou com uma fonte específica, é marcada como **a confirmar** — não é
adivinhação apresentada como fato.

---

## 1. Qual produto Delinea é este, e por que isso já não é 100% desconhecido

"Delinea" é a fusão de dois produtos que antes eram separados — **Thycotic Secret Server** (o
"vault" de senhas/segredos, foco em PAM tradicional) e **Centrify** (hoje "Delinea Platform" /
Privileged Access Service — PAS / Server Suite, foco em identidade/AD-bridging). O pedido do
usuário ("API key", "senha renovada diariamente", "lista de servidores com heartbeat", "Launcher
abre PuTTY/RDP") bate com **Secret Server** — é ele quem tem o conceito de "Secret" (uma
credencial + metadados), Heartbeat, RPC/Remote Password Changing, e os **Secret Launchers**
(botão "Launch" que abre PuTTY/mstsc.exe já preenchidos — exatamente o comportamento "fraco" que
o usuário descreveu, porque o Launcher clássico só invoca o cliente nativo do sistema operacional
do analista, não renderiza nada dentro do navegador). **Confirmado nesta rodada**: a instância é
**Secret Server Cloud** (SaaS, `*.delinea.app`), não on-prem.

**Evidência indireta já existente no próprio código desta aplicação, agora também confirmada
diretamente pelo usuário**: a seção "Descoberta de Rede" do `CLAUDE.md` documenta, em duas
rodadas de correção reais, testes contra hosts atrás de um **"cofre Delinea"** que bloqueia
qualquer sondagem de rede direta — o usuário confirmou que isso é literal: **os servidores só são
alcançáveis através do proxy do Delinea**, não há caminho de rede direto do host deste backend
até eles (nem para SSH, nem — presumivelmente, ver seção 6 — para RDP).

---

## 2. Existe API? Sim — REST (+ SOAP legado), madura, versionada por instância

Fontes: [REST API Overview](https://docs.delinea.com/online-help/secret-server/api-scripting/rest-api/index.htm),
[APIs and Scripting](https://docs.delinea.com/online-help/secret-server/api-scripting/index.htm),
[Script Authentication Using Tokens](https://docs.delinea.com/online-help/secret-server-11-5-x/api-scripting/authenticating/index.htm),
[python-tss-sdk (SDK oficial da Delinea)](https://github.com/DelineaXPM/python-tss-sdk).

- **Protocolo**: REST puro sobre HTTP(S), JSON. Interface SOAP legada também existe, mas a
  documentação atual trata REST como o caminho recomendado.
- **URL base** (confirmado Cloud): `https://<tenant>.delinea.app`, endpoints sob `/api/v1/...`
  (e possivelmente `/api/v2/...` para algumas operações mais novas, típico de instâncias Cloud —
  a confirmar caso a caso durante a Fase 1, tentando `v2` com fallback para `v1`).
- **Autenticação REST — dois mecanismos**:
  1. **OAuth2 Resource Owner Password Grant**: `POST /oauth2/token`, form-urlencoded, corpo
     `grant_type=password&username=<usuário>&password=<senha>` (+ header `OTP` se 2FA). Resposta
     traz `access_token` (Bearer, TTL curto) **e** `refresh_token`.
  2. **API Token estático gerado na UI** (Preferências do usuário) — a documentação da Delinea
     classifica isso como **"Deprecated Method"** no sentido de "prefira OAuth2 para automação
     genérica", mas é exatamente o mecanismo que se encaixa no modelo escolhido nesta rodada (ver
     2.1 abaixo).
- **Versionamento**: em Cloud, a Delinea controla isso centralmente (sempre a versão mais
  recente), diferente de on-prem onde pode ficar desatualizada por anos.

### 2.1. Confirmado: modelo é "uma API key por analista", não uma conta de serviço

Ao perguntar sobre conta de serviço dedicada, a resposta foi **"para cada analista será uma
apikey"** — cada usuário desta aplicação vai gerar seu próprio **API Token estático** (mecanismo 2
acima) no seu perfil pessoal do Secret Server, e colar essa chave nesta aplicação. Isso muda o
desenho de credencial da Fase 2 (seção 7) de "um segredo global" para "um segredo por usuário,
chaveado por `user_email`" — mesmo padrão já maduro nesta app (`GitHubEditorProfile`, tokens de IA
por usuário, `CloudAccountHints`).

**Vantagem real, não só burocrática**: cada analista só enxerga/consegue agir sobre os secrets que
o **próprio RBAC do Delinea** já concede a ele — a aplicação nunca precisa replicar permissão
nenhuma por conta própria.

**Atenção — isso é uma credencial DIFERENTE da que autentica a sessão SSH/RDP em si (seções 5/6)**:
o API Token pessoal serve para chamadas REST (listar/buscar secrets, ver heartbeat, etc.). A ponte
de terminal SSH usa **SSH Terminal**, que se autentica com usuário/senha do Secret Server **ou**
uma chave pública SSH cadastrada no perfil — não aceita o Bearer token da REST API para esse
propósito (seção 7, pergunta 12). A ponte RDP (seção 6) provavelmente também depende de outro
mecanismo (um token de curta duração emitido "no momento do launch", não o API Token REST — ver
seção 6).

---

## 3. Levantamento amplo — tudo que a API do Secret Server oferece (pedido explícito do usuário)

| Área | O que permite | Relevância aqui |
|---|---|---|
| **Secrets** (`/api/v1/secrets`) | CRUD completo, busca por texto (`filter.searchtext`), filtro por pasta/template/heartbeat (`filter.HeartbeatStatus`), leitura de campos (`fields[]`, nomes dependem do *template*), histórico de acesso | **Central** — fonte da "lista de servidores" e da credencial em si |
| **Heartbeat** | Verifica, por secret, se a credencial armazenada ainda autentica de verdade contra o alvo. Desligado por padrão. Status: Success, Heartbeat Failed, Unable to Connect, Pending, Incompatible Host | **Central** — é o "heartbeat" pedido; mas é por-secret, não por-host genérico (seção 4.1) |
| **RPC / Remote Password Changing** | Rotação automática — `Auto Change` (na expiração) ou `Auto Change Schedule` (`Rotate Password Every`). Aplicável a AD, Windows, Unix (SSH), MS SQL | **Central** — é o mecanismo de "senha renovada diariamente" |
| **Checkout & Approval Workflows** | Acesso exclusivo (`Checkout`, com OTP) + aprovação opcional (`Approval Workflow`, humano aprova antes da senha ser revelada) — dois mecanismos distintos da documentação genérica | **Confirmado que esta empresa usa só o `Checkout`, sem `Approval Workflow` humano** — no Linux aparece como escolha de launcher (PuTTY/MobaXterm/WinSCP), no Windows como exclusão mútua entre SREs (ver "Rodada 3" no topo do documento) |
| **Discovery** | Varredura de rede (AD, ESX/ESXi, AWS, GCP, Linux/Unix) que descobre contas/dependências e importa como secrets | Explica como os servidores entraram no cofre; sem endpoint limpo de "lista de computadores" separado dos secrets — **a confirmar** |
| **Folders & Permissions** | Organização hierárquica, `GET /api/v1/folders?filter.searchText=...` | Útil para escopar a busca |
| **Reports** | Relatórios pré-construídos ou customizados sobre qualquer dado do Secret Server, executáveis via API | **Atalho possível** se já existir um relatório de inventário com SO como coluna |
| **SSH Proxy / SSH Terminal** | O Secret Server faz a ponte SSH — `ssh <usuário>@<host_proxy> -p <porta> -t launch <secret_id>`. Senha do alvo nunca sai do Secret Server | **Confirmado como único caminho viável** para Linux — ver seção 5 |
| **RDP Proxy** | Análogo para RDP — dois modos: conexão direta na porta do proxy, ou **gateway mode** (RDP-sobre-HTTPS via protocolo MS-TSGU/RD Gateway da própria Microsoft, recomendado pela Delinea para instalações novas). Cliente se autentica no gateway com "um bearer token de curta duração assinado, emitido pelo Secret Server no momento do launch" | **Provável único caminho viável para Windows** — ver seção 6, bem mais incerto que o SSH Terminal em termos de documentação pública disponível |
| **Session Connector** | Alternativa: RDS (Remote Desktop Services) com um componente instalado, faz o usuário baixar um `.rdp` padrão que abre via app remoto na RDS — sem instalar nada na máquina do usuário. Também mencionada integração com **Privileged Remote Access (PRA)**, produto Delinea separado, para "acesso baseado em browser" | Ver seção 6 — pode ser um atalho se PRA já estiver licenciado nesta empresa, evitaria construirmos nossa própria ponte RDP |
| **Users / Groups / Roles** | Provisionamento e RBAC do próprio Secret Server | Fora de escopo — só precisamos **ler** |
| **Auditoria** | Trilha de quem acessou o quê, quando | Complementar ao `HistoryTracker` já existente nesta app |
| Webhooks / notificação de eventos | Mencionado de forma genérica em buscas, **não confirmado com fonte específica** | Não considerar disponível até confirmar |

---

## 4. O que interessa especificamente para o pedido do usuário (servidores Linux)

### 4.1. Lista de servidores/IPs/SO/heartbeat

Não existe (confirmado até onde a pesquisa alcançou) um endpoint dedicado "lista de hosts com SO e
IP" separado dos próprios Secrets. Caminho realista: buscar secrets por **template** (ex.: "Unix
Account (SSH)") e/ou pasta; cada secret carrega um campo de host/máquina (nome exato do campo
ainda **a confirmar** — frequentemente `Machine`/`Server`). Campo de "SO" não é nativo — só existe
se houver campo customizado ou um Report pronto (seção 3). A sinergia óbvia seria reaproveitar o
fingerprint de SO já existente no Net Discovery, mas como a rede está bloqueada pelo bastion
(confirmado), **isso provavelmente não alcança esses hosts** a partir do backend — marcar como
inviável para esta frota, a menos que se prove o contrário.

### 4.2. Filtrar por Linux

Sem campo de SO nativo confiável, a via mais realista é **filtrar por template do secret**
(`Unix Account (SSH)` vs `Windows Account`, por exemplo) — **a confirmar** contra os templates
reais da instância.

### 4.3. Ponte de terminal com credencial pré-preenchida (Linux)

Ver seção 5 — confirmada como SSH Terminal (`launch <secret_id>`), não SSH direto do backend.

### 4.4. "Poderes de tmux"

Depois que `launch <secret_id>` estabelece a sessão proxiada, mandar `tmux new-session -A -s
k8s-hpa-mgr\n` como primeira entrada de texto (não como comando SSH em si) — se a sessão cair, o
usuário reconecta e volta a ver o mesmo terminal remoto. Exige checar `tmux` no alvo antes, com
fallback — **a confirmar se a frota tem tmux** (seção 8).

### 4.5. Rotação diária de senha — implicação de design

Nunca cachear/persistir o valor do secret nesta aplicação. No modelo confirmado (seção 5), isso
fica garantido por construção: como a ponte usa `launch <secret_id>` via SSH Terminal, **a senha
do alvo Linux nunca chega a esta aplicação em momento nenhum** — quem busca e usa a senha é o
próprio Secret Server.

---

## 5. Arquitetura da ponte de terminal — servidores Linux (SSH Terminal)

### Reaproveitar o protocolo do terminal do Code Editor, não a implementação em si

O terminal do Code Editor (`internal/web/handlers/code_editor_terminal.go` +
`RepoTerminal`/xterm.js) já resolve toda a UX de terminal nesta app: protocolo WebSocket simples
(`{type: "input"|"output"|"resize", data, cols, rows}`, output em base64), PTY real, cores ANSI,
resize, atalhos de copiar/colar. A diferença é só o transporte: em vez de um PTY **local**
(`creack/pty` + `exec.Command(shell)`), o backend abre uma sessão SSH contra o **SSH Terminal do
próprio Secret Server**, não contra o servidor Linux alvo diretamente.

### Mecanismo confirmado: SSH Terminal do Secret Server (`launch <secret_id>`)

Fontes: [SSH Terminal Administration](https://docs.delinea.com/online-help/secret-server-11-6-x/networking/ssh-terminal/index.htm),
[SSH and Secret Server](https://docs.delinea.com/online-help/secret-server/networking/ssh/ssh-overview.htm),
[SSH Proxy Configuration](https://docs.delinea.com/online-help/secret-server/networking/ssh/ssh-proxy-configuration/index.htm).

- O Secret Server expõe um **servidor SSH próprio** (porta configurável pelo admin, "SSH Public
  Host"/"SSH Bind IP Address" em Admin > Proxying). O backend conecta usando
  `golang.org/x/crypto/ssh` (**já vendorizado como dependência indireta**, viraria direta).
- Comando confirmado: `ssh <usuário_secret_server>@<host_ssh_terminal> -p <porta> -t launch
  <secret_id>` — conecta e já inicia a sessão proxiada até o alvo. Outros comandos: `man`, `search
  <termo>`, `cat <secret_id>` (detalhes do secret, captura erros de "requires approval").
- **Checkout automático e silencioso** ao lançar via SSH Terminal. Nesta empresa (confirmado —
  "Rodada 3" no topo) **não há Approval Workflow humano nos secrets Linux** — a tela que parecia
  aprovação é escolha de launcher (PuTTY/MobaXterm/WinSCP), então `launch <secret_id>` deveria
  mesmo ser "clique e conecta", sem espera. Mesmo assim, vale manter uma detecção defensiva de
  erro de acesso na saída inicial do canal (secrets individuais podem ter regra própria, e a
  configuração pode mudar) — mas isso deixa de ser o desenho central da Fase 4, é só uma rede de
  segurança.
- **A senha do alvo nunca passa pelo nosso backend** — o Secret Server autentica no servidor final
  por conta própria; o backend só encaminha bytes entre o WebSocket do navegador e o canal SSH
  aberto contra o SSH Terminal.

### Credencial para autenticar a conexão SSH Terminal em si (diferente da API key REST)

"Log in to the terminal with Secret Server user credentials" — usuário/senha do login do Secret
Server — **ou** uma **chave pública SSH** cadastrada no perfil do usuário (o servidor valida a
chave privada contra as públicas cadastradas). **Recomendação**: usar a via de chave SSH, nunca
guardar a senha pessoal do Delinea nesta aplicação — cada analista cadastraria uma chave pública
uma única vez no próprio perfil do Secret Server, e a privada correspondente ficaria em
`UserTokensStore` por analista. Pergunta em aberto (seção 8, pergunta 12).

### Verificação de host key

Proposta: TOFU (trust-on-first-use) já na v1, não adiado — o host key do SSH Terminal é fixo e
crítico (porta de entrada do cofre inteiro), diferente de N hosts variados.

### RBAC e auditoria

No mínimo `RequireSREGroup()`, e registrar cada conexão no `HistoryTracker` já existente.

---

## 6. Servidores Windows — ponte RDP (mesmo princípio, protocolo bem mais complexo)

### Por que isto é uma categoria de problema diferente do SSH

RDP é um protocolo **gráfico**, não texto — não existe like-for-like reaproveitamento do terminal
do Code Editor (xterm.js só desenha texto/ANSI). O único princípio que se mantém idêntico ao
desenho da seção 5 é o **arquitetural**: buscar/usar a credencial só no backend, nunca expor ao
navegador, transporte via WebSocket. A UI em si precisa de um componente novo (canvas), não uma
reutilização de widget existente.

### O que a Delinea documenta — RDP Proxy

Fontes: [RDP Proxy Overview](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-overview.htm),
[RDP Proxy Technical Notes](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-technical-notes/index.htm),
[RDP Proxy Configuration](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-configuration/index.htm).

- **Dois modos**: conexão direta na porta do proxy RDP (o proxy simula um handshake RDP, obtém
  credenciais temporárias, encaminha ao alvo), ou **gateway mode** — cliente tunela RDP sobre
  HTTPS até um listener gateway usando o protocolo padrão da Microsoft **MS-TSGU / RD Gateway
  (RDG)**, removendo NTLM da conexão cliente-proxy. **Delinea recomenda gateway mode para
  instalações novas.**
- **Autenticação no gateway**: "Clients authenticate to the gateway using a short-lived signed
  bearer token issued by Secret Server at launch time" — ou seja, existe um passo prévio (dentro
  do Secret Server) que gera um token efêmero especificamente para aquele lançamento; o cliente
  RDP então usa esse token para autenticar no gateway. **A pesquisa não encontrou o endpoint REST
  exato que emite esse token** — só a existência do mecanismo. Achar esse endpoint (provavelmente
  documentado no "REST API Reference" completo, só acessível de dentro de uma instância real, ou
  descobrível via DevTools do navegador observando a chamada disparada ao clicar em "Launch" num
  secret RDP) é o bloqueio real da Fase de RDP.
- **Compatibilidade de cliente**: a documentação diz literalmente que "qualquer cliente RDP que
  implemente o protocolo de gateway pode conectar", mas a Delinea **só valida oficialmente o
  mstsc.exe** (cliente nativo da Microsoft) — nenhuma menção a FreeRDP ou outro cliente
  alternativo. Isso não significa que não funcione, só que **não há garantia nem precedente
  documentado** — precisa ser testado contra a instância real.
- **Session Connector** (mecanismo alternativo, não gateway): um servidor RDS com um componente
  Delinea instalado — o usuário baixa um `.rdp` padrão e abre com o app remoto da RDS, sem instalar
  nada na própria máquina; ainda assim é fundamentalmente "abrir um cliente RDP local", não
  renderização no navegador. A documentação menciona de passagem que "Session Connector launchers
  também podem ser acessados através do **Privileged Remote Access (PRA)**", um produto Delinea
  **separado**, que aí sim promete "acesso baseado em browser" — **vale a pena confirmar com o
  administrador do Delinea se PRA já está licenciado nesta empresa antes de investir esforço em
  construir uma ponte RDP própria** (seção 8, pergunta 14) — se já existe, pode ser mais barato
  usar o que a Delinea já oferece "de fábrica" em vez de reconstruir.

### Arquitetura proposta, caso PRA não esteja disponível: Apache Guacamole auto-hospedado

Se a única via realista for o RDP Proxy em modo gateway, a peça que falta é um **cliente RDP que
fale o protocolo RD Gateway e consiga ser pilotado pelo nosso backend** (equivalente ao papel que
`golang.org/x/crypto/ssh` cumpre na seção 5) — não existe biblioteca Go madura para isso. A opção
realista e amplamente usada no mercado (outras ferramentas de bastion/PAM fazem exatamente isso)
é **Apache Guacamole**:

- **`guacd`** — daemon C oficial do projeto Guacamole, que fala protocolos remotos nativos (RDP
  via **FreeRDP** internamente, também VNC/SSH/Telnet) e os traduz para o "protocolo Guacamole",
  um protocolo próprio de renderização remota + eventos de teclado/mouse, agnóstico do protocolo
  de origem. Confirmado que **FreeRDP (≥1.2) suporta nativamente RD Gateway** (parâmetros de
  conexão tipo `/gw:g:<gateway>:<porta>,u:<user>@<domínio>,p:<senha>,type:rpc` — sintaxe
  confirmada via issues/documentação do próprio FreeRDP), e o `guacd` expõe esse suporte de
  gateway como parâmetros de conexão do protocolo Guacamole (nome exato dos parâmetros —
  `gateway-hostname`/`gateway-port`/`gateway-username`/etc. — **a confirmar diretamente na
  documentação do Guacamole antes de implementar**, não vi uma fonte que listasse os nomes exatos
  nesta pesquisa, só a capacidade em si).
- **`guacamole-common-js`** — biblioteca JS oficial (MIT) que desenha a sessão remota num
  `<canvas>` e transporta o protocolo Guacamole via WebSocket/HTTP. Seria o componente novo de
  frontend (não reaproveita xterm.js).
- **Papel do nosso backend Go**: em vez de rodar toda a stack Java/Tomcat do `guacamole-client`
  oficial (pesada, fora do padrão desta app), escrever um bridge fino próprio — mesmo espírito já
  usado nesta aplicação para o navegador SFTP embutido (server+client do `pkg/sftp` conectados via
  `net.Pipe()` no mesmo processo): o Go backend abre uma conexão TCP com `guacd` (rodando como
  container Docker, imagem oficial `guacamole/guacd` — mesmo padrão já usado por Kafka Test/DB
  Test para containers efêmeros/persistentes, `internal/*/db_test_docker.go`), envia as
  instruções do protocolo Guacamole informando os parâmetros de conexão (host do gateway RDP do
  Delinea, o token efêmero obtido no momento do "launch" como credencial do gateway, domínio,
  etc. — nunca a senha real do Windows alvo, que segue dentro do Secret Server o tempo todo), e
  faz bridge entre esse socket TCP e o WebSocket do navegador — o `guacamole-common-js` do lado do
  cliente fala nativamente esse protocolo, sem precisar reimplementar nada da renderização.
- **Nunca expor `guacd` diretamente à rede/usuário** — só nosso backend fala com ele, mesmo
  princípio de isolamento já usado no Port Forward/SFTP desta app.

### Checkout no Windows = exclusão mútua entre SREs, não aprovação (confirmado — "Rodada 3")

Diferente do que a documentação genérica da Delinea sugere (Checkout **+** Approval Workflow
opcional), nesta empresa o Checkout nos secrets Windows serve só para **impedir dois analistas
conectados ao mesmo servidor ao mesmo tempo** — sem humano aprovando nada, é "ocupado" ou "livre"
agora. Isso muda o requisito de design da ponte RDP (Fase Windows-2): em vez de um fluxo de
"solicitar acesso e esperar aprovação", o certo é:
- Mostrar **ocupação em tempo real** na lista de servidores (seção 4/Fase 3) — "em uso por
  `fulano` desde HH:MM", provavelmente lendo o status de checkout do secret via REST antes de
  oferecer o botão "Conectar".
- Se o secret já estiver checked-out por outro analista, `launch`/o fluxo de gateway
  provavelmente vai falhar — a UI deveria refletir isso claramente, com uma opção de
  **`ForceCheckIn`** (a API já suporta) só quando o analista tiver permissão/justificativa pra
  liberar (não deveria ser um clique casual — é literalmente "tirar outro SRE de dentro do
  servidor").
- Ao encerrar a sessão RDP no nosso terminal, fazer o check-in de volta (mesmo princípio de
  "silenciosamente checked-in" que o SSH Terminal já faz — a confirmar se o RDP Proxy se comporta
  igual).

### Nível de confiança desta seção 6, comparado à seção 5

Deliberadamente mais baixo — a seção 5 (SSH) tem uma fonte primária clara e um comando concreto e
testável (`launch <secret_id>`) documentado pela própria Delinea. Esta seção 6 (RDP) combina: (a)
um mecanismo Delinea real mas com uma peça-chave não documentada publicamente (o endpoint que
emite o token efêmero de gateway), (b) uma compatibilidade de cliente "teoricamente aberta, só
oficialmente validada para mstsc.exe", e (c) uma arquitetura de bridge própria via Guacamole que é
tecnicamente sólida em si (é como o mercado resolve esse problema em geral), mas cuja integração
específica com o RD Gateway do Delinea **nunca foi testada nem confirmada nesta pesquisa**. Tratar
como uma investigação de viabilidade (Fase Windows-1 abaixo) antes de comprometer esforço de
implementação de verdade.

---

## 7. Plano de implementação (fases)

Numeração e formato seguem o padrão já usado em `ZABBIX-INTEGRATION-PLAN.md`/`KAFKA-TEST-PLAN.md`.
Trilha Linux (SSH) e trilha Windows (RDP) são independentes a partir da Fase 2 — dá pra entregar
uma sem esperar a outra.

### Fase 1 — Pacote `internal/delinea/` (cliente REST + secrets, sem UI ainda)
- `client.go`: `NewClient(baseURL, apiToken string)` — Bearer direto (API Token estático por
  analista, ver 2.1).
- `models.go`: `Secret`, `SecretField`, `HeartbeatStatus`, filtros de busca.
- `SearchSecrets(filter)` e `GetSecret(id)` (nunca cacheado — 4.5).
- Testar autenticação com um API Token real contra a instância Cloud, mais uma busca
  `filter.searchtext` com `limit` pequeno.
- **Objetivo desta fase**: confirmar empiricamente as perguntas 3, 4, 6, 7 da seção 8 (nome real
  do campo de host, se Heartbeat está ligado, cadência real de rotação, mix senha/chave, e —
  Windows — o nome do template usado para secrets RDP).

### Fase 2 — Credenciais por usuário + handler + rotas (comum às duas trilhas)
- `UserTokensStore`: `DelineaAPIToken string` chaveado por `user_email`.
- `internal/web/handlers/delinea.go`: `GET /api/v1/delinea/config`, `POST /api/v1/delinea/test`,
  `GET /api/v1/delinea/servers` (busca + normaliza template/host/heartbeat/SO-se-existir).

### Fase 3 — Frontend: aba "Delinea Vault" no Tools menu (comum às duas trilhas)
- `DelineaVaultTab.tsx`: tabela de servidores, filtro "Somente Linux"/"Somente Windows", busca.
- Configuração da API key pessoal.

### Fase 4 (trilha Linux) — Ponte de terminal via SSH Terminal (`launch <secret_id>`)
- `internal/web/handlers/delinea_terminal.go`: WebSocket handler espelhando o protocolo de
  `code_editor_terminal.go`, abrindo canal SSH contra o SSH Terminal do Secret Server, executando
  `launch <secret_id>`.
- Autenticação via chave pública cadastrada por analista.
- Detecção do estado "aguardando aprovação".
- Botão "Conectar" na lista da Fase 3. Registro no `HistoryTracker`. TOFU do host-key.

### Fase Windows-1 — Investigação de viabilidade do RDP Proxy (bloqueante, antes de codar)
- Confirmar se **PRA (Privileged Remote Access)** já está licenciado (pergunta 14) — se sim,
  reavaliar se vale mais a pena usar a solução pronta da Delinea em vez de construir a nossa.
- Se não houver PRA: descobrir o endpoint real de emissão do token de gateway efêmero — via
  DevTools do navegador, clicando em "Launch" num secret RDP real na UI do Secret Server, e
  inspecionando a chamada de rede disparada.
- Provisionar um `guacd` de teste (container Docker) e validar manualmente, fora da aplicação
  (script isolado, mesmo hábito já usado nesta app antes de escrever código de produção — ver
  "Descoberta de Rede" e "Teste de Banco de Dados" no `CLAUDE.md`), se uma conexão RDP via gateway
  do Delinea consegue de fato ser estabelecida através do FreeRDP interno do `guacd`.

### Fase Windows-2 (condicional ao sucesso da Fase Windows-1) — Ponte RDP via Guacamole
- `internal/delinea/rdp_bridge.go` (ou pacote próprio `internal/guacbridge/`): cliente do
  protocolo Guacamole em Go, fala com `guacd` via TCP, expõe um WebSocket handler.
- `guacd` como container Docker gerenciado pela app (mesmo padrão de reaper/pré-checagem já usado
  em `db_test_docker.go`/Kafka Test).
- Novo componente de frontend usando `guacamole-common-js` — canvas, não terminal de texto.
- Mesmo tratamento de Checkout/Approval Workflow da Fase 4, adaptado ao fluxo RDP (a confirmar se
  o "launch" de RDP também expõe esse erro de forma parecida ao `launch` do SSH Terminal).

### Fase 5 — Refinamentos
- `tmux new-session -A` injetado como primeira entrada após o shell do `launch` SSH responder
  pronto, com fallback quando `tmux` não existir no alvo (4.4).
- Atalho via **Reports** (seção 3) em vez de reconstruir a busca de secrets do zero, se aplicável.

---

## 8. Perguntas — o que já foi confirmado e o que ainda falta

### Já confirmado
1. ✅ Instância é **Cloud (Delinea SaaS)**.
2. ✅ Modelo de credencial REST é **uma API key por analista**, não conta de serviço compartilhada.
5. ✅ **Revisado** — não há Approval Workflow humano; Checkout no Linux é escolha de launcher
   (PuTTY/MobaXterm/WinSCP, só PuTTY sem config dedicada), no Windows é exclusão mútua entre SREs.
8. ✅ Servidores Linux **só alcançáveis via SSH Proxy/bastion do Delinea** — SSH Terminal
   (`launch`) é o único caminho confirmado.

### Ainda em aberto (trilha Linux)

3. **Nome real do campo de host/máquina e organização dos secrets Linux** — template exato, nome
   do campo de host/IP, pasta(s), campo customizado de SO. Respondível na Fase 1.
4. **Heartbeat está habilitado** nesses secrets? Respondível na Fase 1.
6. **A rotação (RPC) está de fato configurada como diária** para os secrets-alvo?
7. **Autenticação nos alvos é sempre por senha, ou parte usa Private Key**?
9. **`tmux` está instalado nos servidores alvo?**
10. **Escala aproximada** — dezenas ou centenas de servidores?
11. **SSH Terminal está de fato habilitado** nesta instância Cloud (Admin > Proxying, licença
    Professional/Platinum)? Sem isso, a Fase 4 inteira não funciona, independente do nosso código.
12. **Qual credencial autentica a conexão SSH Terminal em si** — chave pública SSH por analista
    (recomendado) ou usuário/senha pessoal do Delinea (não recomendado)?
13. ~~Como o aprovador é notificado quando um Approval Workflow é acionado~~ — **obsoleta**, não
    há Approval Workflow humano nesta empresa (ver "Rodada 3" no topo). Substituída pela pergunta
    18 abaixo.
18. **Quem pode usar `ForceCheckIn`** pra liberar um servidor Windows ocupado por outro SRE — todo
    analista, só líder de squad, algum grupo específico? Decide o RBAC do botão de liberação
    forçada na Fase Windows-2 (seção 6) — não deveria ser um clique casual disponível a qualquer
    um.

### Novas — trilha Windows (RDP)

14. **A empresa já tem licença/uso de Privileged Remote Access (PRA)** da Delinea, ou algum outro
    mecanismo de acesso RDP via browser já pronto? Se sim, pode ser mais barato usar isso do que
    construir a ponte Guacamole própria da Fase Windows-2.
15. **RDP Proxy (gateway mode) está habilitado** nesta instância, e qual é exatamente o fluxo hoje
    quando um analista clica "Launch" num secret Windows — abre mstsc.exe direto, ou passa por
    alguma etapa de download de arquivo `.rdp`/token? (Determina onde/como capturar o token
    efêmero de gateway mencionado na documentação.)
16. **Template usado para os secrets Windows** (`Windows Account`? alguma variante?) e se há
    domínio AD envolvido na autenticação (RDP corporativo tipicamente é `DOMINIO\usuário`, não
    usuário local) — afeta os parâmetros de conexão do FreeRDP/Guacamole.
17. **Existe apetite/infraestrutura para hospedar mais um componente** (`guacd`, um processo C/
    container Docker) além do que a aplicação já roda hoje? A trilha Windows depende disso se PRA
    não estiver disponível (pergunta 14).

---

## 9. Arquivos a criar/modificar (resumo, quando aprovado)

```
internal/delinea/client.go                            ← CRIAR (Fase 1)
internal/delinea/models.go                             ← CRIAR (Fase 1)
internal/storage/user_tokens_store.go                   ← MODIFICAR (Fase 2 — campos por usuário)
internal/web/handlers/delinea.go                         ← CRIAR (Fase 2)
internal/web/server.go                                   ← MODIFICAR (Fase 2 — registrar rotas)
internal/web/frontend/src/components/ToolsMenu.tsx       ← MODIFICAR (Fase 3)
internal/web/frontend/src/components/DelineaVaultTab.tsx ← CRIAR (Fase 3)
internal/web/frontend/src/lib/api/client.ts               ← MODIFICAR (Fase 3 — novos métodos)
internal/web/handlers/delinea_terminal.go                 ← CRIAR (Fase 4 — bridge SSH Terminal/launch via WebSocket)
internal/history/tracker.go                               ← usar já-existente (Fase 4 — log de conexão)
internal/guacbridge/ (ou internal/delinea/rdp_bridge.go)  ← CRIAR (Fase Windows-2 — cliente do protocolo Guacamole)
internal/web/handlers/delinea_rdp.go                       ← CRIAR (Fase Windows-2 — WebSocket handler RDP)
internal/web/frontend/src/components/DelineaRdpViewer.tsx  ← CRIAR (Fase Windows-2 — canvas via guacamole-common-js)
```

---

## 10. Dependências externas

`golang.org/x/crypto/ssh` já está vendorizado como dependência **indireta** — passaria a ser
direta (Fase 4), exigindo `go mod tidy`/`go mod vendor`. Para o TOFU de host-key, também
`golang.org/x/crypto/ssh/knownhosts` (hoje ausente do vendor). Nenhum SDK Go oficial ou de
terceiros para Delinea é necessário — cliente HTTP fino próprio, mesma filosofia de Dynatrace/
Zabbix.

**Trilha Windows (Fase Windows-2), infraestrutura nova para este projeto**: `guacd` (daemon C,
não Go — rodaria como container Docker separado, imagem oficial `guacamole/guacd`, gerenciado com
o mesmo padrão de pré-checagem/reaper já usado para Docker em `db_test_docker.go`) +
`guacamole-common-js` no frontend (biblioteca JS MIT, nova dependência npm). Nenhuma lib Go de
protocolo Guacamole madura conhecida — o bridge do lado do backend (fala TCP com `guacd`, expõe
WebSocket) precisaria ser escrito à mão seguindo a especificação do protocolo Guacamole (texto
simples, formato `<comprimento>.<valor>,<comprimento>.<valor>;`), mais simples do que parece à
primeira vista mas ainda um protocolo novo dentro desta app.

---

## Fontes consultadas

- [REST API Documentation for Secret Server](https://docs.delinea.com/online-help/platform-api/secret-server.htm)
- [REST API Overview](https://docs.delinea.com/online-help/secret-server/api-scripting/rest-api/index.htm)
- [APIs and Scripting](https://docs.delinea.com/online-help/secret-server/api-scripting/index.htm)
- [Script Authentication Using Tokens](https://docs.delinea.com/online-help/secret-server-11-5-x/api-scripting/authenticating/index.htm)
- [python-tss-sdk (SDK oficial Delinea)](https://github.com/DelineaXPM/python-tss-sdk)
- [Heartbeat Overview](https://docs.delinea.com/online-help/secret-server/rpc-heartbeat/heartbeats/index.htm)
- [Running Heartbeat for a Secret](https://docs.delinea.com/online-help/secret-server/rpc-heartbeat/heartbeats/running-heartbeat-for-a-secret/index.htm)
- [Heartbeat Status Codes](https://docs.delinea.com/online-help/secret-server/rpc-heartbeat/heartbeats/heartbeat-status-codes/index.htm)
- [Remote Password Changing](https://docs.delinea.com/online-help/secret-server-11-6-x/remote-password-changing/index.htm)
- [Understanding Expiration, Auto Change and Auto Change Schedules](https://docs.delinea.com/online-help/secret-server-11-5-x/remote-password-changing/automatic-rpc/autochange-expiration-schedules/index.htm)
- [Secret Checkout / Checkout Overview](https://docs.delinea.com/online-help/secret-server/secret-operations/secret-access-workflow/secret-checkout/index.htm)
- [Discovery Overview](https://docs.delinea.com/online-help/secret-server/discovery/understanding-discovery/discovery-overview/index.htm)
- [Reports Overview](https://docs.delinea.com/online-help/secret-server/reports/index.htm)
- [SSH Terminal Administration](https://docs.delinea.com/online-help/secret-server-11-6-x/networking/ssh-terminal/index.htm) — fonte central da seção 5 (comando `launch`, checkout/aprovação, config de porta)
- [SSH and Secret Server](https://docs.delinea.com/online-help/secret-server/networking/ssh/ssh-overview.htm)
- [SSH Proxy Configuration](https://docs.delinea.com/online-help/secret-server/networking/ssh/ssh-proxy-configuration/index.htm)
- [SSH Jumpbox Routes](https://docs.delinea.com/online-help/secret-server-11-6-x/networking/ssh-jumpbox-routes/index.htm)
- [Unix Account (SSH) Secret Template for RPC](https://docs.delinea.com/online-help/secret-server/rpc-heartbeat/rpc/rpc-secret-templates/templates/unix_account__ssh__secret_template_for_rpc.htm)
- [Creating a Unix Account Secret Template that Uses Key Authentication](https://docs.delinea.com/online-help/secret-server/secret-operations/secret-templates/specific-templates/unix-account-template-that-uses-key-auth-instead-of-password/index.htm)
- [RDP Proxy Overview](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-overview.htm) — fonte central da seção 6 (dois modos, token efêmero de gateway)
- [RDP Proxy Technical Notes](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-technical-notes/index.htm) — confirma limite de validação oficial só para mstsc.exe
- [RDP Proxy Configuration](https://docs.delinea.com/online-help/secret-server/networking/rdp-proxy/rdp-proxy-configuration/index.htm)
- [Secret Server Session Connector](https://docs.delinea.com/online-help/secret-server/launcher-protocol-handler/launchers/procedures/session-connector/index.htm) — menciona Privileged Remote Access (PRA) para acesso via browser
- [Apache Guacamole — Implementation and architecture](https://guacamole.apache.org/doc/gug/guacamole-architecture.html) — `guacd`, protocolo Guacamole, `guacamole-common-js`
- [FreeRDP/FreeRDP — rdg.c (suporte a RD Gateway)](https://github.com/FreeRDP/FreeRDP/blob/master/libfreerdp/core/gateway/rdg.c)
