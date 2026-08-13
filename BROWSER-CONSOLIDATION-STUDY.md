# Estudo de Viabilidade: Unificar todos os fluxos de browser no "embed" (Chromium do go-rod)

**Status:** 🔬 estudo **CONCLUÍDO**. Fase 0 ✅ (limpeza de código morto). Fase 1 ✅
(`internal/browser/` extraído). Fase 2 ❌ **reprovada com evidência real** — Chromium embed
(revisão `1321438`) crasha (SIGTRAP) ao renderizar o Teams v2, reproduzido 3x ao vivo em
12/08/2026, confirmado via kernel log do WSL2. Fase 3 não prossegue. Fase 4 ✅ aplicada — Teams
permanece no Chrome do sistema, decisão final documentada na seção 5.
**Pergunta original do usuário:** existem hoje vários tipos de navegador usados pela aplicação
(um "embed" pro ServiceNow, outros com navegadores instalados no WSL) — mapear tudo e avaliar a
viabilidade de padronizar em um único mecanismo ("tudo no embed").
Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` (seções "ServiceNow — Rod (Go
nativo) + WSL2 CDP" e "Teams Mr.ViaBot + SRE Approval").

---

## 1. Definição: o que é "embed" neste projeto

"Embed" = **Chromium baixado e gerenciado pelo próprio `go-rod`** (`launcher.New()` sem `.Bin(...)`,
revisão fixa `1321438` do Chromium open-source, vendorizada em
`vendor/github.com/go-rod/rod/lib/launcher/revision.go:6`). Características:

- Binário próprio da aplicação — não depende de nada pré-instalado no host/WSL.
- Versão **fixa e previsível** (a mesma em qualquer máquina, não segue o Chrome que o usuário
  tem instalado).
- É o **Chromium genuíno open-source**, não o "Google Chrome" — sem branding Google, sem
  Widevine CDM (DRM), sem alguns componentes proprietários que só vêm no Chrome oficial.
- Perfil de sessão isolado em `~/.k8s-hpa-manager/<algo>-session/`.

O oposto ("navegador do sistema") é `.Bin(<path>)` apontando pro Chrome/Chromium **já instalado**
no host — versão variável, pode já ter uma sessão SSO logada externamente, mas é uma dependência
externa que pode não existir ou mudar de versão sem aviso.

---

## 2. Inventário completo — o que cada feature usa hoje

| Feature | Mecanismo real hoje | Chromium usado | Sessão/perfil | Headless? |
|---|---|---|---|---|
| **ServiceNow — extração de CHG** (`Extract`) | go-rod | **embed** (Rod baixa, sem `.Bin()`) | `~/.k8s-hpa-manager/rod-session/` | sim |
| **ServiceNow — login/renovação** (`TestSession`) | go-rod | **embed** | idem | não (Xvfb se sem display) |
| **Teams — aprovações/discovery/scan/envio** | go-rod | **sistema** (`findSystemChrome()` + `.Bin()`, só cai pro embed se não achar) | `~/.k8s-hpa-manager/teams-session/` | não, sempre visível |
| App — abrir a própria SPA no navegador (`cmd/web.go`) | `exec.Command(xdg-open/open/start)` | navegador padrão do usuário | n/a | n/a |
| Auth GCP/AWS/GitHub/Vertex AI (frontend) | `window.open` puro | navegador padrão do usuário | n/a | n/a |

**Confirma exatamente a observação do usuário**: ServiceNow já usa o "embed" (Chromium do Rod);
Teams usa o Chrome/Chromium **instalado no sistema/WSL** (`google-chrome-stable`,
`chromium-browser`, etc., buscados em paths fixos por `findSystemChrome()` em
`internal/teams/discover.go:1069`, usado em `internal/teams/browser_manager.go:43-79`).

### 2.1 Achado colateral: parte do que o CLAUDE.md documenta pro ServiceNow está morta

O CLAUDE.md descreve dois modos alternáveis pro ServiceNow — "Modo local" (embed) e "Modo
Windows/WSL2" (Chrome do Windows via CDP na porta 9223, lançado pela própria app via
PowerShell). Na prática:

- `NeedsWindowsBrowser()`, `LaunchWindowsBrowserForCDP()`, `StartCDPRelay()`,
  `WindowsSessionWSLDir()` (todas em `internal/servicenow/wsl_browser.go`) **não são chamadas por
  nada** — nem pelo `rod_extractor.go`, nem pelos handlers HTTP, nem pelo frontend.
- `GetBrowserConfig`/`SetBrowserConfig` (`internal/web/handlers/servicenow.go:353-399`) reportam
  incondicionalmente `"browser_mode": "chromium-local"` — nunca `"windows-cdp"`.
- O único resquício vivo é um fast-path **passivo**: `ExtractCookiesViaCDP(WindowsCDPPort=9223,
  ...)` (`rod_extractor.go:642`) tenta ler cookies de um Chrome que **o usuário** (não a app) já
  tenha aberto manualmente com `--remote-debugging-port=9223`. A app nunca lança esse Chrome.
- Também mortos: `internal/servicenow/playwright.go` (implementação legada via `npx playwright`,
  substituída pelo Rod) + `internal/servicenow/script_embed.go` (script TS que só esse arquivo
  morto usa), e `internal/servicenow/cdp_powershell.go` (equivalentes PowerShell de
  `cdp_cookies.go`, nenhuma função chamada).

Ou seja: **o ServiceNow já é 100% embed hoje**, exceto por essa infraestrutura órfã de um modo
Windows-CDP que nunca chega a rodar. Isso simplifica o estudo — não é preciso "migrar" o
ServiceNow, só decidir o que fazer com o código morto (seção 5).

---

## 3. Por que o Teams usa o navegador do sistema (hipótese — não confirmada em código)

**Não há nenhum comentário no código explicando essa escolha.** `getBrowser()`
(`internal/teams/browser_manager.go:43-48`) só loga "Usando Chrome do sistema" ou "não
encontrado — usando Chromium do Rod" — é uma preferência silenciosa, sem justificativa
registrada. Hipóteses plausíveis (nenhuma confirmada, precisam de teste empírico antes de agir):

1. **Fingerprinting/bloqueio de browser pela Microsoft**: o Teams web historicamente detecta e
   às vezes degrada a experiência (ou bloqueia) para builds de Chromium não-oficiais/automação
   detectável. O Chromium do Rod é um build genérico sem branding "Google Chrome" — é possível
   que o SPA do Teams v2 se comporte diferente ou rejeite esse UA/fingerprint.
2. **Robustez de renderização do SPA pesado**: Teams v2 é uma aplicação web muito mais pesada
   (WebSockets, Service Workers, IndexedDB em escala) que o formulário HTML clássico do
   ServiceNow — pode expor bugs de uma revisão de Chromium mais antiga (`1321438`, pinada há
   tempo) que o Chrome real (auto-atualizado) não teria.
3. **Sessão pré-existente**: se o usuário já usa esse mesmo Chrome do sistema para acessar Teams
   manualmente, poderia (em teoria) haver reaproveitamento de cookies — mas isso **não se
   confirma no código**: o perfil usado (`teams-session/`, `UserDataDir`) é um diretório próprio
   da aplicação, isolado de qualquer perfil pessoal do usuário no mesmo Chrome. Então essa
   hipótese provavelmente não se sustenta — é um Chrome do sistema, mas com perfil isolado
   mesmo assim.

**Recomendação**: antes de mudar qualquer coisa, validar empiricamente (fase 2 abaixo) se o
Chromium embed do Rod consegue autenticar e extrair dados do Teams v2 do mesmo jeito. Não vale a
pena especular mais sem esse teste.

---

## 4. Não existia abstração compartilhada — cada serviço reimplementava sua própria lógica (✅ resolvido na Fase 1)

Confirmado (no momento em que este estudo foi escrito): **não existia `internal/browser/`** nem
nada equivalente. ServiceNow e Teams tinham cada um sua própria versão de:

- Lançar/reconectar num Chrome persistente entre chamadas (`getBrowser`)
- Matar processos travados no mesmo perfil antes de relançar (`killExistingChrome`)
- Limpar cache em disco antes de lançar
- Minimizar/restaurar a janela

O código era estruturalmente muito parecido nos dois pacotes (comentários num arquivo já citavam
o outro como referência — ex.: `browser_manager.go:18` citava `rod_extractor.go` e vice-versa) —
era duplicação por cópia manual, não reuso. A Fase 1 (seção 5) extraiu isso pra `internal/browser/`.

Detecção de login SSO/Azure AD pendente **continua** não-compartilhada — cada serviço lida com
isso de um jeito bem diferente (ServiceNow: `SessionStatus`/timestamp de diretório; Teams: espera
DOM/IndexedDB específica do Teams v2) — não era um caso genuíno de duplicação, ficou de fora da
Fase 1 de propósito.

---

## 5. O que "usar tudo no embed" significa concretamente — plano faseado

### Fase 0 — Limpeza do código morto ✅ CONCLUÍDA
Removido o que não era chamado por ninguém:
- `internal/servicenow/playwright.go` + `internal/servicenow/script_embed.go` (Playwright legado,
  `PlaywrightExtractor` e todo o script TS embutido) — deletados. Os tipos `PlaywrightResult` e
  `SessionStatus` (ainda usados de verdade por `rod_extractor.go`/`sn_direct_client.go`, nome
  herdado da implementação antiga) foram preservados e movidos para `models.go`.
- `internal/servicenow/cdp_powershell.go` (equivalentes PowerShell não usados) — deletado.
- `internal/servicenow/wsl_browser.go`: mantido só `IsWSL()`, `HasGraphicalDisplay()`,
  `LoadBrowserConfig`/`SaveBrowserConfig` (config genérica, hoje só `sso_login_identifier` é
  lido/escrito de fato — `ForceWindowsBrowser`/`WindowsSessionDir` removidos do struct
  `BrowserConfig` por não terem nenhum consumidor) e `WindowsCDPPort`/`cdpHosts()` (fast path de
  leitura passiva de cookies em `cdp_cookies.go`, ainda usado); removidos
  `LaunchWindowsBrowserForCDP`, `StartCDPRelay`, `WindowsSessionWSLDir`, `FindWindowsBrowser`,
  `NeedsWindowsBrowser`, `wslToWindowsPath`, `windowsToWSLPath` e as constantes/paths auxiliares
  só usados por essas funções — nenhuma tinha call site fora do próprio arquivo.
- CLAUDE.md (seção ServiceNow) atualizado pra refletir que hoje só existe o modo embed de fato —
  removida a descrição do "Modo Windows/WSL2" como algo ativo/alternável; tabela de requisitos
  opcionais também corrigida (não é mais "Chrome/Edge do Windows via CDP" pro ServiceNow).
- Validado: `go build ./...`, `go vet`, `gofmt` e `go test ./internal/servicenow/... -race`
  passando sem nenhuma mudança de comportamento (só remoção de código inalcançável).
- **Consolida exatamente o pedido do usuário**: já elimina a única infraestrutura de "navegador
  do Windows via CDP lançado pela app" que existia no repo (mesmo que já estivesse inerte).

### Fase 1 — Extrair `internal/browser/` compartilhado (refatoração pura, zero mudança de comportamento) ✅ CONCLUÍDA
Criado `internal/browser/browser.go`:
- `Launch(opts LaunchOptions) (*rod.Browser, func(), error)` — o boilerplate `launcher.New()...
  Launch()` + `rod.New().ControlURL(...).Connect()`, parametrizado por `SessionDir`/`Headless`/
  `SystemBin` (vazio = embed do Rod)/`Flags`/`DeleteFlags`. Único ponto que chama `launcher.New()`
  na aplicação agora (antes: 1 em `rod_extractor.go`, 1 em `browser_manager.go`, duplicados).
- `Manager` — wrapper com mutex do padrão "browser persistente, reconecta se `.Pages()` falhar,
  relança se o `SessionDir` pedido mudou" que ServiceNow (`RodExtractor.getBrowser`) e Teams
  (`getBrowser`) cada um reimplementava por conta própria. `Get(opts, beforeLaunch, afterLaunch,
  logger)` aceita hooks pra cada chamador preservar sua própria lógica pré-launch (matar
  processos travados, limpar cache, remover locks) e o log "iniciado" que só disparava no
  caminho de lançamento novo, nunca no de reuso — sem os hooks, um `Manager` genérico teria
  mudado esse comportamento observável por acidente.
- `FindSystemChrome()`, `KillExistingChrome(sessionDir, logger)` — movidas de
  `internal/teams/discover.go` (únicas usuárias hoje; ServiceNow não killa processos por PID, só
  remove lock files — ver `RemoveStaleLockFiles` abaixo, comportamento distinto preservado como
  está, não unificado à força).
- `RemoveStaleLockFiles(sessionDir, logger)` — movida de `rod_extractor.go` (SingletonLock/
  SingletonSocket/SingletonCookie).
- `RestoreWindow(page, logger)`/`MinimizeWindow(page, logger)` — movidas de
  `internal/teams/browser_manager.go` (únicas usuárias hoje; ServiceNow não minimiza/restaura
  janela, capacidade fica disponível pra reuso futuro sem mudar nada no ServiceNow agora).

`internal/servicenow/rod_extractor.go`: `RodExtractor.browser`/`browserStop`/`mu` (campos do
struct) viraram um único campo `browserMgr sharedbrowser.Manager`; `getBrowser()`/
`launchLocalBrowserWithDir()`/`ClearSession()`/`TestSession()` passaram a chamar o pacote
compartilhado. `internal/teams/browser_manager.go`: `sharedBrowser`/`sharedSessionDir`/
`browserMu` (variáveis de pacote) viraram um único `browserMgr browser.Manager`; `getBrowser()`
idem. **Teams continua usando `.Bin()` do sistema por enquanto** (`FindSystemChrome()` chamada
com o mesmo comportamento de antes) — só a duplicação foi eliminada, nenhum comportamento
observável muda.

Validado: `go build ./...`, `go vet ./...`, `gofmt`, `go test ./internal/servicenow/...
./internal/teams/... ./internal/browser/... -race`, `make build` — tudo passando.

### Fase 2 — Validação empírica: Teams rodando no embed (atrás de flag, sem trocar o padrão)

**Código implementado** (`internal/teams/browser_manager.go`): env var `K8S_HPA_TEAMS_EMBED_BROWSER`
(`"true"`/`"1"`) faz `getBrowser()` ignorar `FindSystemChrome()` mesmo quando o Chrome do sistema
está disponível, forçando `chromeBin=""` → `browser.Launch` sem `.Bin()` → Chromium embutido do
Rod. Sem a env var, comportamento 100% idêntico a antes (prefere sistema, cai pro embed só se não
achar nada instalado) — reversível, zero risco pra quem não setar a flag.

**Como rodar o teste** (precisa de você presente pro login SSO/MFA — não dá pra automatizar):
```bash
export K8S_HPA_TEAMS_EMBED_BROWSER=true
kill <PID_atual> && ./build/new-k8s-hpa web -f   # reinicia com a flag setada no ambiente
```
Na Web UI, Tools → "Teams Broadcast" (ou o fluxo que aciona `RunDiscovery`/`ScanConversations`/
`SendBatch`) → disparar a extração normalmente. Como `Headless(false)` é sempre usado pro Teams
(igual antes), uma janela deve abrir — em WSL2 precisa de WSLg (Windows 11) ou Xvfb+VNC pra
enxergá-la; sem isso o browser sobe mas fica invisível, inviável de completar login manual.

Checklist de validação:
- [x] ~~Login SSO/MFA Azure AD completo funciona igual~~ → **REPROVADO** (ver abaixo)
- [x] ~~`RunDiscovery` extrai `SkypeToken` e conversas do IndexedDB normalmente~~ → **REPROVADO**
- [ ] `ScanConversations`/`SendBatch` — nem chegou a ser testado, `RunDiscovery` já reprova antes
- [x] ~~Nenhuma diferença perceptível de performance/estabilidade~~ → **REPROVADO**, ver abaixo

Esse teste **precisa ser feito contra o Teams real** — não dá pra confirmar por leitura de
código se o SPA do Teams se comporta diferente num Chromium genérico vs. Chrome instalado (ver
hipóteses da seção 3). ✅ **Executado ao vivo em 12/08/2026 (3 tentativas) — REPROVADO.**

**Resultado real — ❌ Chromium embed (revisão `1321438`) crasha ao renderizar o Teams v2**

3 tentativas ao vivo, todas com o mesmo desfecho: o processo Chrome embed morre sozinho entre
~40-90s depois de "Teams v2 carregado", sempre durante a espera do `SkypeToken`
(`CDP: write tcp ...: use of closed network connection` do lado Go). Confirmado com evidência de
kernel — as 3 tentativas batem 1:1 com 3 crashes reais capturados pelo WSL2:

```
kernel: chrome: potentially unexpected fatal signal 5.
unknown: WSL (... - CaptureCrash): Capturing crash for pid: ..., executable:
  !home!paulo!.cache!rod!browser!chromium-1321438!chrome, signal: 5, port: 50005
```

Sinal 5 = SIGTRAP, o padrão de crash de uma `CHECK`/`DCHECK` falhando dentro do próprio Chromium
— não é sandbox, não é GPU (testado com `disable-gpu`/`no-sandbox`/`disable-dev-shm-usage`/
`disable-setuid-sandbox` na 2ª tentativa, mesmo assim crashou), não é falta de display (WSLg
confirmado ativo, janela visível), não é timeout/rede. É a revisão **pinada e antiga** do
Chromium (a mesma desde sempre no `go-rod` vendorizado) tendo um bug real ao processar o SPA
pesado do Teams v2 — confirma **exatamente a Hipótese 2** da seção 3.

**Achado colateral real, corrigido no mesmo lote** (`internal/teams/discover.go`): investigando a
1ª queda, achamos que `minimizeWindow()` rodava logo depois de "Teams v2 carregado!" — um check
que só confirma a URL (`teams.microsoft.com/v2`), não login de fato completo (a URL já bate
mesmo em telas de login/MFA em andamento). Com o Chrome do sistema isso nunca dava sintoma
(sessão já vinha autenticada, login sempre silencioso); no embed, sem sessão prévia, a janela de
login minimizava sozinha antes do usuário conseguir interagir. Corrigido: `minimizeWindow()`
movida pra depois do loop de espera do `SkypeToken` (só minimiza quando o login de fato
completou; se não completou em 90s, a janela fica visível). **Esse fix é real e válido
independente do resultado da Fase 2** — beneficia também o caminho do Chrome do sistema em
qualquer cenário de login não-silencioso (sessão nova/expirada) — mantido e commitado. Mas não
foi a causa da queda: o processo morre por SIGTRAP mesmo com a janela minimizada ou visível.

**Conclusão: Fase 2 reprovada com evidência real, não apenas hipótese.** Sem trocar a revisão do
Chromium vendorizada no `go-rod` (mudança bem maior, fora de escopo deste estudo — nem se sabe se
uma revisão mais nova resolveria, exigiria repetir todo o teste), o embed não é viável pro Teams
hoje. Ver Fase 4 abaixo.

### Fase 3 — Trocar o padrão do Teams pro embed — ❌ NÃO PROSSEGUE
Dependia da Fase 2 validar. Reprovada com evidência real (crash SIGTRAP reproduzido 3x) — não há
motivo pra prosseguir com essa fase.

### Fase 4 — Fase 2 REPROVOU — ✅ EXECUTADA
- Teams **permanece no Chrome do sistema** (`findSystemChrome()`/`.Bin()`) — decisão deliberada,
  documentada aqui com causa raiz real (crash SIGTRAP na revisão pinada do Chromium embed ao
  renderizar o Teams v2), não mais hipótese.
- `K8S_HPA_TEAMS_EMBED_BROWSER` **permanece no código**, desligada por padrão — inofensiva (só
  ativa se alguém setar a env var explicitamente) e serve como ponto de retomada caso o `go-rod`
  vendorizado suba de revisão no futuro (só então valeria repetir este teste).
- Inverter o modelo (Chrome do sistema também pro ServiceNow) **não se aplica** — o ServiceNow
  segue headless, sem o mesmo tipo de SPA pesado, e nunca teve esse sintoma.
- Servidor do usuário restaurado ao estado normal (sem a flag) ao final da sessão de teste.

---

## 6. Recomendação

1. **Fase 0 aplicada** — limpeza pura de código morto, sem risco.
2. **Fase 1 aplicada** — reduz duplicação sem mudar comportamento observável.
3. **Fase 2 executada e reprovada com evidência real** (crash SIGTRAP do Chromium embed
   reproduzido 3x ao vivo, confirmado via kernel log do WSL2). **Fase 3 não prossegue.** **Fase 4
   aplicada**: Teams permanece no Chrome do sistema, motivo documentado formalmente acima.

## 7. Estudo concluído

- ~~Fase 0~~ ✅ aplicada e commitada (`refactor/limpeza-browser-servicenow`).
- ~~Fase 1~~ ✅ aplicada nesta mesma branch.
- ~~Fase 2~~ ✅ testada ao vivo com o usuário em 12/08/2026 — **reprovada** (crash SIGTRAP do
  Chromium embed, 3x reproduzido, evidência de kernel).
- ~~Fase 3~~ não prossegue (dependia da Fase 2 validar).
- ~~Fase 4~~ ✅ aplicada — decisão final documentada, nada pendente.

Nenhuma pergunta em aberto — não há mais fases planejadas neste estudo.
