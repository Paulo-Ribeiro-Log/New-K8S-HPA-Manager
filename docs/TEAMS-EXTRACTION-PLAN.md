# Plano: Extração de Mensagens do MR.ViaBot (Microsoft Teams)

**Branch sugerido:** `feat-teams-mrViaBot-extraction`  
**Base:** `main` (após merge da PR #130)  
**Estudo de referência:** `TEAMS-EXTRACTION-STUDY.md`

---

## Contexto

O bot **MR.ViaBot** no Microsoft Teams envia diariamente mensagens com links de aprovação e links do ServiceNow (CHGs). O objetivo é extrair automaticamente esses links do dia atual, eliminando o processo manual.

A empresa bloqueia o Microsoft Graph API externamente. A solução usa **interceptação CDP** (Chrome DevTools Protocol) via go-rod para capturar o **X-Skypetoken** que o Teams web obtém automaticamente, e depois faz chamadas HTTP diretas para a API interna `chatsvcagg.teams.microsoft.com`.

A sessão Azure AD é **compartilhada** com o ServiceNow — mesmo perfil Chromium, mesmo login.

---

## Checklist de Implementação

### Fase 0 — Descoberta (manual, uma vez)

- [ ] **0.1** Com sessão Teams ativa no Chromium, abrir DevTools → Network e mapear:
  - URL exata de `authsvc` que retorna o X-Skypetoken (verificar se está no body ou em header de resposta)
  - URL de listagem de conversas (`chatsvcagg` ou outro host)
  - URL de mensagens de um chat específico
  - Formato do threadId do chat do MR.ViaBot
- [ ] **0.2** Salvar exemplos reais de resposta JSON de cada endpoint em `internal/teams/testdata/`
- [ ] **0.3** Identificar e persistir o `threadId` do MR.ViaBot (não muda — pode ser config fixa)
- [ ] **0.4** Mapear o formato das mensagens do bot: HTML ou texto? Qual campo contém os links?

> **Dica:** Use `page.MustEval("() => JSON.stringify(performance.getEntriesByType('resource').map(r => r.name))")` para listar todas as URLs que o Teams carregou.

---

### Fase 1 — Estrutura base (`internal/teams/`)

- [ ] **1.1** Criar `internal/teams/auth.go`
  - Struct `TeamsAuth { SkypeToken string; ExpiresAt time.Time }`
  - `AcquireSkypeToken(page *rod.Page) (TeamsAuth, error)` — intercepta via CDP
  - Cache em memória com TTL (token dura ~24h)
- [ ] **1.2** Criar `internal/teams/client.go`
  - Struct `TeamsClient { auth TeamsAuth; httpClient *http.Client }`
  - `NewTeamsClient(auth TeamsAuth) *TeamsClient`
  - `ListConversations() ([]Conversation, error)`
  - `GetMessages(threadId string, since time.Time) ([]Message, error)`
- [ ] **1.3** Criar `internal/teams/models.go`
  - `Conversation { ID, DisplayName, LastMessagePreview string }`
  - `Message { ID, SentAt time.Time, FromName string, Body string, Links []ExtractedLink }`
  - `ExtractedLink { URL, Type string }` — Type: "servicenow" | "approval" | "other"
- [ ] **1.4** Criar `internal/teams/parser.go`
  - `ExtractLinks(body string) []ExtractedLink` — regex para CHGs e aprovações
  - `IsTodayMessage(msg Message) bool`
- [ ] **1.5** Criar `internal/teams/extractor.go`
  - `TeamsExtractor { rodExtractor *servicenow.RodExtractor; logger *zerolog.Logger }`
  - `ExtractTodayMessages(botName string) (*ExtractionResult, error)` — orquestra tudo
  - Reusa o perfil Chromium de `~/.k8s-hpa-manager/rod-session/` (sessão compartilhada)

---

### Fase 2 — Endpoints REST

- [ ] **2.1** Criar `internal/web/handlers/teams.go`
  - `TeamsHandler { extractor *teams.TeamsExtractor; logger *zerolog.Logger }`
  - `GET /api/v1/teams/session-status` — verifica se Teams está autenticado
  - `POST /api/v1/teams/extract` — extrai mensagens do MR.ViaBot do dia atual
  - `GET /api/v1/teams/messages/today` — retorna cache das últimas mensagens extraídas
- [ ] **2.2** Registrar rotas em `internal/web/server.go`

---

### Fase 3 — Frontend

- [ ] **3.1** Criar componente `TeamsExtractionPanel.tsx` (ou integrar ao modal ServiceNow)
  - Botão "Extrair links do Teams hoje"
  - Lista de mensagens do MR.ViaBot com links clicáveis
  - Badge por tipo de link: CHG (azul) | Aprovação (laranja)
- [ ] **3.2** Integrar ao fluxo de importação de CHG — ao colar URL de CHG, checar se já existe mensagem do bot com aprovação pendente para ela
- [ ] **3.3** Adicionar ao `ToolsMenu.tsx` como nova ferramenta "Links Teams (MR.ViaBot)"

---

### Fase 4 — Sessão compartilhada e login

- [ ] **4.1** Validar que `rod-session/` já autenticado no ServiceNow reconhece o Teams sem novo login
- [ ] **4.2** Se sessão Teams expirar independentemente do ServiceNow: detectar e instruir usuário a refazer `TestSession`
- [ ] **4.3** Documentar no `SERVICENOW-AUTH.md` que uma sessão cobre os dois sistemas

---

### Fase 5 — Testes e robustez

- [ ] **5.1** Testes unitários para `parser.go` (extração de links de CHG e aprovação)
- [ ] **5.2** Testes unitários para `models.go` (filtro de mensagens do dia)
- [ ] **5.3** Teste de integração: sessão real Teams → captura token → lista mensagens
- [ ] **5.4** Fallback gracioso se X-Skypetoken não aparecer no body de `authsvc`: tentar extrair de `localStorage` via JavaScript
- [ ] **5.5** Fallback se `chatsvcagg` mudar de URL: CDP loga todas as XHR e identifica nova URL automaticamente

---

## Ordem recomendada de execução

```
Fase 0 (descoberta manual)
    ↓
Fase 1.1 + 1.2 + 1.3 (estrutura e cliente HTTP)
    ↓
Fase 1.4 + 1.5 (parser + extractor)
    ↓
Fase 2 (endpoints REST) → testar via curl/Postman
    ↓
Fase 4.1 (validar sessão compartilhada)
    ↓
Fase 3 (frontend)
    ↓
Fase 5 (testes + robustez)
```

---

## Arquivos a criar

```
internal/teams/
    auth.go          ← captura X-Skypetoken via CDP
    client.go        ← chamadas HTTP para API interna Teams
    models.go        ← structs de dados
    parser.go        ← extração de links das mensagens
    extractor.go     ← orquestrador principal
    testdata/        ← exemplos JSON reais de resposta (Fase 0)
internal/web/handlers/teams.go
internal/web/frontend/src/components/teams/TeamsExtractionPanel.tsx
```

---

## Dependências

- PR #130 mergeada (Chromium headless + sessão ServiceNow funcionando)
- Xvfb instalado no WSL (se aplicável) ou WSLg ativo
- Sessão Azure AD válida em `~/.k8s-hpa-manager/rod-session/`
- `threadId` do MR.ViaBot mapeado na Fase 0
