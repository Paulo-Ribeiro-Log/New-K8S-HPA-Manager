# Estudo: Extração de Mensagens do Microsoft Teams (MR.ViaBot)

## Contexto

A empresa bloqueou o Microsoft Graph API para apps externos (`ChannelMessage.Read.All` requer consentimento de admin de tenant). O único acesso disponível é via interface web do Teams. O objetivo é extrair automaticamente as mensagens do bot **MR.ViaBot** do dia atual, que contêm links de aprovação e links do ServiceNow — eliminando o trabalho manual de copiar esses links.

---

## Por que não usar Graph API

```
GET https://graph.microsoft.com/v1.0/me/chats/{chatId}/messages
→ 403 Forbidden (permissão bloqueada pelo tenant)
```

Mesmo com token Azure AD válido, a permissão `ChannelMessage.Read.All` precisa ser concedida pelo admin da organização para apps externos. Não está disponível.

---

## A Abordagem: Interceptação da API Interna do Teams

O Teams web não é o Graph API — ele é um SPA que faz chamadas para **APIs internas próprias** da Microsoft. Essas APIs usam um token diferente: o **X-Skypetoken**, que o browser obtém automaticamente ao fazer login.

### Arquitetura interna do Teams web

```
Browser logado no Teams
        │
        ├── POST authsvc.teams.microsoft.com/v1.0/authz
        │          Bearer: <token Azure AD>
        │          ← X-Skypetoken: <skype_token>   ← QUEREMOS ESSE
        │
        └── GET chatsvcagg.teams.microsoft.com/v1/users/ME/conversations
                   X-Skypetoken: <skype_token>
                   ← lista de chats + mensagens
```

O X-Skypetoken é obtido automaticamente pelo Teams web logo após o login. Ele é enviado em cada requisição interna. **Podemos capturá-lo via CDP (Chrome DevTools Protocol) interceptando as requisições de rede.**

---

## Plano de Implementação

### Fase 1 — Autenticação e captura do X-Skypetoken

```
Chromium (go-rod, mesmo perfil da sessão ServiceNow)
    → Navega para teams.microsoft.com
    → Azure AD SSO já estava ativo (mesma sessão Chromium)
    → Teams carrega e chama authsvc automaticamente
    → CDP intercepta a resposta de authsvc
    → Extrai X-Skypetoken do response body
```

**Por que funciona sem login manual:** O perfil Chromium em `~/.k8s-hpa-manager/rod-session/` já tem cookies Azure AD da sessão ServiceNow. Teams e ServiceNow usam o mesmo Azure AD tenant — o Chromium reutiliza a sessão sem pedir login de novo.

**Implementação CDP com go-rod:**
```go
// Habilitar interceptação de rede
router := page.HijackRequests()
router.MustAdd("*authsvc.teams.microsoft.com/v1.0/authz*", func(ctx *rod.Hijack) {
    ctx.MustLoadResponse()
    // Extrair X-Skypetoken do body da resposta
    body := ctx.Response.Body()
    // parse JSON → campo "tokens.skypeToken"
})
go router.Run()
page.MustNavigate("https://teams.microsoft.com")
```

### Fase 2 — Localizar o chat do MR.ViaBot

Com o X-Skypetoken em mãos, chamada HTTP direta (sem browser):

```
GET https://chatsvcagg.teams.microsoft.com/v1/users/ME/conversations?pageSize=50
X-Skypetoken: <token>
```

Resposta: lista de conversas/chats. Filtrar pelo campo `displayName` ou pelo remetente das últimas mensagens para encontrar o chat do **MR.ViaBot**.

O ID do chat (threadId) tem formato: `19:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx@thread.v2`

### Fase 3 — Buscar mensagens do dia atual

```
GET https://chatsvcagg.teams.microsoft.com/v1/users/ME/conversations/{threadId}/messages?startTime={today_unix_ms}
X-Skypetoken: <token>
```

Filtrar mensagens onde:
- `from.displayName == "MR.ViaBot"` (ou campo `botId`)
- `originalarrivaltime` dentro do dia atual

### Fase 4 — Extrair links das mensagens

As mensagens chegam em HTML ou texto com links incorporados. Extrair com regex:

```go
// Links ServiceNow (CHGs)
chgRegex := regexp.MustCompile(`https://viavarejo\.service-now\.com[^\s"<]+sys_id=[a-f0-9]{32}`)

// Links de aprovação (padrão a identificar na primeira execução)
approvalRegex := regexp.MustCompile(`https://[^\s"<]*(approv|aprovar|approval)[^\s"<]*`)
```

---

## Sessão Compartilhada: Teams + ServiceNow

Este é o ponto mais elegante da abordagem. O perfil Chromium em `~/.k8s-hpa-manager/rod-session/` pode ter **duas sessões ativas simultaneamente**:

```
rod-session/
    ├── Cookies Azure AD (compartilhado automaticamente)
    ├── Sessão ServiceNow → viavarejo.service-now.com
    └── Sessão Teams     → teams.microsoft.com
```

Um único login resolve os dois. O usuário faz `TestSession` uma vez e extrai tanto CHGs quanto links de aprovação do Teams sem precisar autenticar novamente.

---

## Fluxo Completo de Extração

```
1. Chromium headless carrega (sessão salva em rod-session/)
2. Abre teams.microsoft.com → SSO silencioso via cookies Azure AD
3. CDP intercepta authsvc → captura X-Skypetoken
4. HTTP: lista conversas → encontra threadId do MR.ViaBot
5. HTTP: busca mensagens de hoje do MR.ViaBot
6. Parse: extrai links ServiceNow + links de aprovação
7. Retorna estrutura com CHGs e aprovações do dia
```

Total estimado: **5-15 segundos** (headless, sem interação visual).

---

## Estrutura de Dados de Saída

```go
type TeamsBotMessage struct {
    MessageID    string    `json:"message_id"`
    SentAt       time.Time `json:"sent_at"`
    Content      string    `json:"content"`
    SNowLinks    []string  `json:"snow_links"`    // CHGs extraídas
    ApprovalLinks []string `json:"approval_links"` // Links de aprovação
}

type TeamsExtractionResult struct {
    Date      string             `json:"date"`
    BotName   string             `json:"bot_name"`   // "MR.ViaBot"
    Messages  []TeamsBotMessage  `json:"messages"`
    TotalCHGs int                `json:"total_chgs"`
}
```

---

## Riscos e Mitigações

| Risco | Probabilidade | Mitigação |
|---|---|---|
| Microsoft muda URL interna `chatsvcagg` | Média | CDP detecta a URL real na interceptação → auto-atualiza |
| X-Skypetoken expira | Baixa (dura ~24h) | Renovar navegando em teams.microsoft.com novamente |
| SSO não funciona silenciosamente | Baixa (mesmo tenant) | Fallback: TestSession com Xvfb |
| Formato JSON de mensagens muda | Baixa | Parser tolerante a falhas com múltiplos campos candidatos |
| X-Skypetoken não aparece no body de authsvc | Média | Alternativa: interceptar headers das chamadas subsequentes |

---

## Alternativa se authsvc não expor o token no body

O Teams também pode armazenar o token no **localStorage** do browser. Com go-rod é simples:

```go
token, _ := page.Eval(`() => {
    // Teams armazena em chaves como "ts.xxx.cache" no localStorage
    for (let key of Object.keys(localStorage)) {
        if (key.includes('skype') || key.includes('teams')) {
            try {
                const val = JSON.parse(localStorage[key]);
                if (val.secret || val.skypeToken) return val.secret || val.skypeToken;
            } catch {}
        }
    }
    return null;
}`)
```

---

## Arquivos a Criar

| Arquivo | Responsabilidade |
|---|---|
| `internal/teams/extractor.go` | Orquestrador principal (Chromium + CDP + HTTP) |
| `internal/teams/auth.go` | Captura X-Skypetoken via interceptação CDP |
| `internal/teams/client.go` | Cliente HTTP para APIs internas do Teams |
| `internal/teams/parser.go` | Extração de links de mensagens HTML/texto |
| `internal/web/handlers/teams.go` | Endpoints REST: status, extrair mensagens do dia |

---

## Próximos Passos

1. **Descoberta** — Executar uma sessão manual instrumentada para mapear as URLs reais que o Teams usa (pode variar por tenant/região)
2. **Prova de conceito** — Interceptar e logar todas as chamadas de rede ao carregar teams.microsoft.com com a sessão autenticada
3. **Identificar threadId do MR.ViaBot** — Uma vez mapeado, pode ser persistido em config (não muda)
4. **Implementar parser de links** — Baseado no formato real das mensagens do bot (HTML vs texto)
