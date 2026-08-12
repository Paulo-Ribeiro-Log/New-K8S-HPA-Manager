# Estudo de Viabilidade: Unificar todos os fluxos de browser no "embed" (Chromium do go-rod)

**Status:** 🔬 estudo — Fase 0 ✅ concluída (limpeza de código morto). Fases 1-4 não iniciadas.
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

## 4. Não existe abstração compartilhada — cada serviço reimplementa sua própria lógica

Confirmado: **não existe `internal/browser/`** nem nada equivalente. ServiceNow e Teams têm cada
um sua própria versão de:

- Lançar/reconectar num Chrome persistente entre chamadas (`getBrowser`)
- Matar processos travados no mesmo perfil antes de relançar (`killExistingChrome`)
- Limpar cache em disco antes de lançar
- Detectar login SSO/Azure AD pendente
- Minimizar/restaurar a janela

O código é estruturalmente muito parecido nos dois pacotes (comentários num arquivo já citam o
outro como referência — ex.: `browser_manager.go:18` cita `rod_extractor.go` e vice-versa) — é
duplicação por cópia manual, não reuso.

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

### Fase 1 — Extrair `internal/browser/` compartilhado (refatoração pura, zero mudança de comportamento)
Unificar a lógica duplicada (`getBrowser`/`killExistingChrome`/limpeza de cache/minimize-restore)
num pacote comum, parametrizado por: diretório de sessão, preferir `.Bin()` sistema ou não,
headless ou não. ServiceNow e Teams passam a chamar essa lib em vez de reimplementá-la — **Teams
continua usando `.Bin()` do sistema por enquanto**, só a duplicação é eliminada. Reduz superfície
de bugs (ex.: um fix de `killExistingChrome` feito só num dos dois pacotes hoje) sem qualquer
risco de quebrar a extração do Teams.

### Fase 2 — Validação empírica: Teams rodando no embed (atrás de flag, sem trocar o padrão)
Adicionar uma env var (`K8S_HPA_TEAMS_EMBED_BROWSER=true`) ou flag de config que, quando setada,
chama `getBrowser` do novo pacote comum **sem** `.Bin()` (força o Chromium do Rod) para o Teams.
Validar manualmente, numa sessão de teste:
- Login SSO/MFA Azure AD completo funciona igual (visual, sem travar)
- `RunDiscovery` extrai `SkypeToken` e conversas do IndexedDB normalmente
- `ScanConversations`/`SendBatch` funcionam sem erro
Esse teste **precisa ser feito contra o Teams real** — não dá pra confirmar por leitura de
código se o SPA do Teams se comporta diferente num Chromium genérico vs. Chrome instalado (ver
hipóteses da seção 3).

### Fase 3 — Se a Fase 2 validar: trocar o padrão do Teams pro embed
Remove `findSystemChrome()`/`.Bin()` do caminho padrão (mantém só como fallback documentado, ou
remove de vez). Resultado: **as duas features (ServiceNow + Teams) rodam 100% no Chromium
embutido do Rod**, sem depender de nenhum Chrome/Chromium pré-instalado no host — elimina uma
dependência de ambiente (`google-chrome-stable` no WSL2) e garante versão consistente entre
features/máquinas.

### Fase 4 — Se a Fase 2 REPROVAR (Teams não funciona bem no embed)
Documentar formalmente o motivo real (não mais hipótese) no CLAUDE.md, manter Teams no Chrome do
sistema como decisão deliberada, e considerar aplicar o **inverso**: usar o Chrome do sistema
também pro ServiceNow (menos provável de ser desejável, já que o modo atual headless do
ServiceNow funciona bem hoje e não tem o mesmo tipo de SPA pesado do Teams).

---

## 6. Recomendação

1. **Fase 0 é segura pra aplicar já** — é limpeza pura de código morto, sem risco, e resolve
   diretamente a confusão entre o que o CLAUDE.md descreve e o que o código faz.
2. **Fase 1 é segura e de baixo esforço** — reduz duplicação sem mudar comportamento observável.
3. **Fases 2/3 exigem validação manual contra o Teams real** antes de qualquer decisão — não há
   como confirmar por leitura de código se o Chromium genérico do Rod é aceito pelo SPA do Teams
   v2 exatamente como o Chrome instalado. Recomendo tratar isso como um teste dedicado (não como
   parte de uma sessão de refatoração maior), com rollback fácil (a flag da Fase 2) caso o
   comportamento do Teams degrade.

## 7. Perguntas em aberto pro usuário

- Quer que eu já aplique a **Fase 0** (limpeza do código morto + correção do CLAUDE.md) nesta
  sessão?
- Quer que eu prossiga com a **Fase 1** (pacote `internal/browser/` compartilhado) também, ou
  prefere revisar o resultado da Fase 0 primeiro?
- Pra Fase 2 (teste real no Teams), precisa ser feito com você presente pra validar o login
  SSO/MFA visualmente — quer agendar isso separadamente?
