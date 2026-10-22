# ServiceNow — Autenticação e Extração de CHGs

## O problema que existia

ServiceNow usa Azure AD SSO/SAML — login que exige um browser real abrindo, redirecionando para Microsoft, e só então voltando ao ServiceNow com cookies de sessão válidos. Em WSL2 sem interface gráfica, não há onde renderizar esse browser.

A abordagem anterior tentava usar o Chrome Windows via CDP (remote debugging port), mas empresas bloqueiam isso por política: `RemoteDebuggingAllowed = false`. Fim do caminho.

---

## A solução: dois modos, um único caminho

### Modo 1 — Extração (roda toda vez que uma CHG é solicitada)

```
Você clica "Extrair CHG" → Chromium headless → ServiceNow API → dados
```

**Headless** significa que o Chromium roda sem renderizar nada na tela — igual ao que pipelines de CI/CD fazem no mundo inteiro. Não precisa de display, não precisa de Xvfb, não precisa de nada. Funciona em WSL2, Linux puro, servidor sem monitor, Docker.

O Rod (biblioteca Go) baixa o Chromium automaticamente na primeira vez e reutiliza a sessão salva em `~/.k8s-hpa-manager/rod-session/`. Uma vez autenticado, extrações futuras são silenciosas e rápidas.

### Modo 2 — Login (feito uma vez, válido por 8-12h)

```
Você clica "Fazer Login" → precisa de display para ver o browser
```

Aqui é onde o Xvfb entra. O **X Virtual Framebuffer** é um servidor X11 que existe há décadas — é exatamente o que todo servidor CI (GitHub Actions, Jenkins, GitLab) usa para rodar testes com browser sem monitor físico.

```
WSL sem display → inicia Xvfb :99 → Chromium abre no display virtual → Azure AD SSO
```

O Chromium não sabe que o display é virtual. Para ele é um monitor normal de 1280×900. Ele abre, faz o redirect para Azure AD, e dependendo do ambiente:

- **SSO corporativo silencioso** (máquina com certificados da empresa): autentica sozinho, sem interação
- **Precisa de interação manual**: conecte um VNC viewer no `:99` e faça login visualmente — ou instale WSLg (Windows 11 inclui suporte nativo)

---

## O que o WSLg resolve de vez

Windows 11 inclui **WSL GUI** nativamente. Com WSLg ativo, qualquer janela Linux aparece direto no desktop Windows — sem Xvfb, sem VNC, sem configuração. O Chromium simplesmente abre como se fosse um app Windows normal.

---

## Instalação do Xvfb (WSL sem WSLg)

```bash
# Ubuntu/Debian
sudo apt-get install -y xvfb

# Fedora/RHEL
sudo dnf install -y xorg-x11-server-Xvfb
```

Para visualizar o browser durante o login:
```bash
# Instalar VNC server
sudo apt-get install -y x11vnc
# Em outro terminal, conectar ao display virtual
x11vnc -display :99 -forever
# Abrir VNC viewer em localhost:5900
```

---

## Por que isso é robusto

| Cenário | Funciona? |
|---|---|
| macOS com qualquer browser | ✅ display nativo (Quartz/Aqua) |
| Linux com desktop | ✅ DISPLAY já setado |
| WSL2 + WSLg (Windows 11) | ✅ DISPLAY via WSLg |
| WSL2 sem display + Xvfb instalado | ✅ display virtual :99 |
| Servidor headless puro | ✅ extração headless sem display |
| Chrome corporativo com política anti-debug | ✅ irrelevante — não usa CDP |

A chave: **extração é sempre headless** (nunca precisou de display), e **login é visual apenas uma vez**. A sessão dura 8-12h e é persistida em disco — o Chromium a reutiliza automaticamente nas próximas extrações sem pedir login de novo.

---

## Arquivos relevantes

| Arquivo | Responsabilidade |
|---|---|
| `internal/servicenow/rod_extractor.go` | Extração CHG + fluxo de login (Rod/Chromium) |
| `internal/servicenow/wsl_xvfb.go` | Detecção e inicialização do Xvfb |
| `internal/servicenow/wsl_browser.go` | Detecção de ambiente (WSL, display, macOS) |
| `internal/web/handlers/servicenow.go` | Endpoints REST de sessão e extração |
| `internal/web/frontend/src/components/profile/ServiceNowSessionModal.tsx` | Modal de gerenciamento de sessão |
