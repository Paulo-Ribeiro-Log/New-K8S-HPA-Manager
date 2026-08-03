# Plano: Mover autenticação Dynatrace da aba AI Diagnostics para o Perfil do Usuário — ⏳ PLANEJADO (nenhuma fase iniciada)

Move a configuração de Dynatrace (URL, API token, filtro de Management Zone) de dentro da aba
**AI Diagnostics → Configurações** (`AISettingsTab.tsx`) para o menu **Perfil do Usuário**
(`UserProfileMenu.tsx`, seção "Credenciais", ao lado de GitHub/Nexus/ServiceNow/AWX) — e, na
mesma mudança, corrige a identidade do Dynatrace de um campo de e-mail digitado manualmente
(`ai_email`, desacoplado do login) para o e-mail real do usuário logado (RBAC/JWT), igual aos
outros itens desse mesmo menu.

---

## Contexto / motivação

O Dynatrace hoje é identificado por `ai_email` — um campo de texto salvo em `localStorage`,
independente do Azure AD. Isso é um resquício de quando o app suportava rodar sem JWT (modo
token estático, sem identidade real de usuário). **Esse motivo não existe mais**: o JWT está
sempre ativo hoje (`ensureJWTSecret()` gera um secret automaticamente se não houver um
configurado, `internal/web/server.go:170`) — todo usuário logado já tem um e-mail real e
verificado disponível.

Numa aplicação de uso profissional, ter uma credencial de uma ferramenta de observabilidade
amarrada a um campo de texto livre (em vez do login real verificado) é um ponto fraco de
segurança/consistência: hoje, em teoria, qualquer pessoa pode digitar o e-mail de outra pessoa
nesse campo. Todos os outros itens do menu Perfil (GitHub, Nexus, ServiceNow, AWX) já usam o
e-mail real via `useUserPermissions()`/`InjectUserEmail()` — o Dynatrace deve seguir o mesmo
padrão.

O esforço é menor do que parece à primeira vista: os endpoints de backend que consomem
Dynatrace (problems, management-zones, investigate, etc.) já são agnósticos a "de onde vem o
e-mail" — só usam como chave de busca no SQLite. A maior parte da migração é troca de
**sourcing no frontend** (de onde o e-mail é lido), não mudança de schema/contrato.

---

## Descobertas da investigação (não re-explorar do zero ao retomar)

- **Frontend atual**: bloco Dynatrace inline em `AISettingsTab.tsx:1263-1349` (estado local
  `dynatraceURL`/`dynatraceToken`/`dynatraceTagFilter`/`showDynatraceToken`/`dtTesting`/
  `dtTestResult`). Carrega via `apiClient.getAITokens()` (`loadTokenStatus()`, linha ~151),
  salva junto com TUDO num único `apiClient.saveAITokens(payload)` (`handleSave()`, linha 392,
  campos `dynatrace_*` nas linhas 379-381). Teste via
  `apiClient.testDynatraceConnection(aiEmail)` (linha 308). `aiEmail` vem de
  `localStorage.getItem("ai_email")` (linha ~139).
- **Backend atual**: `internal/web/handlers/dynatrace.go` — `GetConfig` (linha 77) lê
  `c.Query("ai_email")`; `TestConnection` (linha 103) lê `ai_email` do body JSON.
  `clientForUser(aiEmail)` (linha 60) é o helper compartilhado usado por TODOS os endpoints
  consumidores de Dynatrace (problems, management-zones, investigate, etc.) — esses outros
  endpoints **não mudam** de contrato nesta migração, só quem envia o parâmetro no frontend
  muda de fonte.
- **Storage**: `internal/storage/user_tokens_store.go` — tabela única `user_ai_tokens`
  (PK `user_email`), colunas `dynatrace_url`/`dynatrace_token`/`dynatrace_tag_filter` ao lado
  das colunas de todos os provedores de IA. `SaveTokens()` (linha 110) faz **overwrite de linha
  inteira** (`ON CONFLICT DO UPDATE SET` todas as colunas) — por isso o handler HTTP
  (`ai_tokens.go:SaveTokens`, linha 85) sempre busca o registro existente primeiro e faz merge
  campo-a-campo antes de chamar `store.SaveTokens()` (linhas 118-132, 286-298 para os campos
  Dynatrace especificamente). O novo endpoint dedicado de Dynatrace precisa seguir o mesmo
  padrão de merge, só que escopado às 3 colunas Dynatrace (nunca tocar nas colunas de IA).
- **Identidade verificada já existe e é reaproveitável**: `internal/web/middleware/rbac.go:39`
  — `RBACMiddleware.InjectUserEmail()` já resolve o e-mail real do usuário logado (lê dos
  claims JWT quando presente; fallback pra `az account show` em modo legado sem JWT) e injeta
  no Gin context via `c.Set("user_email", ...)`. Lido depois via helper `profileEmail(c)`
  (`code_editor.go:2291`, `c.Get("user_email")`) — mesmo padrão já usado por GitHub Editor
  Profiles e Cloud Account Hints. Reaproveitar esse padrão, não inventar um novo.
- **Menu Perfil já existe e tem o padrão certo pra copiar**: `UserProfileMenu.tsx` (montado em
  `Header.tsx:348`) renderiza uma seção "Credenciais" com modais em
  `internal/web/frontend/src/components/profile/` — `GitHubCredentialModal.tsx` é o melhor
  exemplo a copiar: usa `useUserPermissions()` pra pegar `userPerms?.email` (não um campo
  digitado). Tipos em `internal/web/frontend/src/types/profile.ts`
  (`UserProfile`/`CredentialsState`/`CredentialInfo`/`CredentialModalProps`).
- **Outros consumidores de `ai_email` (fora do save/load de config) que precisam trocar a FONTE
  do e-mail no frontend, sem mudar contrato de backend**: `PodsPanel.tsx`/`DaemonSetsTab.tsx`/
  `DeploymentsTab.tsx` (variável `aiEmailForDT`, badge de monitoramento Dynatrace na aba Pods),
  `DynatraceTab.tsx` (listagem/análise/investigação de problems), Health Check correlator
  ("Analisar com AI" de um item correlacionado). **Confirmar a lista exata** com
  `grep -rn "ai_email" internal/web/frontend/src` no início da implementação — pode haver mais
  call sites do que os já mapeados aqui.

---

## Decisões de escopo (pra manter baixo risco)

- **Backend com mudança de contrato**: só os 2 endpoints de configuração do próprio usuário
  (`GET /dynatrace/config`, novo `POST /dynatrace/config`) e `POST /dynatrace/test` passam a
  derivar o e-mail via `InjectUserEmail()` (verificado, não confiável-pelo-cliente) — faz
  sentido por serem operações sensíveis (ler/gravar credencial).
- **Backend sem mudança de contrato**: os demais endpoints Dynatrace (problems,
  management-zones, investigate, pod-monitoring-status) continuam aceitando `ai_email` como
  parâmetro exatamente como hoje — só o FRONTEND passa a preencher esse parâmetro com o e-mail
  real (`useUserPermissions().email`) em vez de `localStorage.getItem("ai_email")`. Evita
  reabrir e arriscar quebrar endpoints que já funcionam.
- **Sem migração automática de dados**: um usuário que hoje tem um `ai_email` digitado
  DIFERENTE do seu e-mail real vai precisar reconfigurar o token Dynatrace uma vez (a chave de
  busca no SQLite muda de "o que a pessoa digitou" pra "seu e-mail de login real"). Aceitável —
  comunicar à equipe antes/depois do merge, não vale a complexidade de auto-migrar sem saber o
  mapeamento e-mail-digitado → e-mail-real.

---

## Checklist de implementação

### Fase 1 — Backend

- [ ] `internal/web/handlers/dynatrace.go`: novo `SaveConfig(c *gin.Context)` — lê
      `c.GetString("user_email")`, busca tokens existentes, faz merge só das 3 colunas
      Dynatrace (mesma lógica de `ai_tokens.go:286-298`), chama `tokensStore.SaveTokens(...)`.
- [ ] `internal/web/handlers/dynatrace.go`: `GetConfig` — trocar `c.Query("ai_email")` por
      `c.GetString("user_email")`.
- [ ] `internal/web/handlers/dynatrace.go`: `TestConnection` — trocar leitura de `ai_email` do
      body por `c.GetString("user_email")`.
- [ ] `internal/web/server.go`: localizar `dt := api.Group("/dynatrace")` (linha ~1016),
      adicionar `rbacMiddleware.InjectUserEmail()` (no grupo inteiro ou só nas 3 rotas afetadas
      — checar se o grupo tem outras rotas que não devem exigir isso antes de decidir).
      Registrar `dt.POST("/config", dynatraceHandler.SaveConfig)`.
- [ ] `go build ./...`, `gofmt -l`, `SKIP_AZURE_TESTS=1 go test ./internal/... -race`.

### Fase 2 — Frontend: novo modal de perfil

- [ ] Novo `internal/web/frontend/src/components/profile/DynatraceCredentialModal.tsx`,
      copiando a estrutura de `GitHubCredentialModal.tsx` (usa `CredentialModalProps`,
      `useUserPermissions()` pra exibir o e-mail — somente leitura, sem campo de texto).
- [ ] Campos URL/Token/Tag Filter reaproveitando textos/placeholders de
      `AISettingsTab.tsx:1263-1349` (não reescrever do zero).
- [ ] Botão "Testar Conexão" → `apiClient.testDynatraceConnection()` sem `ai_email`.
- [ ] Salvar → novo `apiClient.saveDynatraceConfig({ dynatrace_url, dynatrace_token,
      dynatrace_tag_filter })` em `client.ts` (sem `ai_email` no payload).
- [ ] Atualizar assinatura de `apiClient.getDynatraceConfig()` em `client.ts` (sem `ai_email`).
- [ ] `internal/web/frontend/src/types/profile.ts`: adicionar `dynatrace` em
      `CredentialsState`/`CredentialInfo`.
- [ ] `internal/web/frontend/src/components/UserProfileMenu.tsx`: entrada "Dynatrace" na seção
      Credenciais (ícone, status via `has_dynatrace`), abrindo o novo modal.

### Fase 3 — Frontend: remover o bloco antigo

- [ ] Remover JSX `AISettingsTab.tsx:1263-1349` (bloco "Dynatrace Integration" + os 2
      `<Separator/>` ao redor, sem deixar separador duplicado sobrando).
- [ ] Remover estado (`dynatraceURL`, `dynatraceToken`, `dynatraceTagFilter`,
      `showDynatraceToken`, `dtTesting`, `dtTestResult`), handler `handleTestDynatrace`, e as
      linhas do payload Dynatrace em `handleSave()`.
- [ ] Checar se `tokenStatus?.has_dynatrace` fica órfão em outro lugar de `AISettingsTab.tsx`
      antes de remover essa leitura também.

### Fase 4 — Frontend: trocar fonte do `ai_email` nos demais consumidores

- [ ] `grep -rn "ai_email" internal/web/frontend/src` — confirmar lista completa de call sites.
- [ ] Trocar `localStorage.getItem("ai_email")` por `useUserPermissions().data?.email` (ou hook
      equivalente já usado no arquivo) em cada um, mantendo o nome de variável local
      (`aiEmailForDT` etc.) pra minimizar o diff.

### Fase 5 — Verificação

- [ ] `npx tsc --noEmit -p internal/web/frontend`, `npm run lint` (0 erros novos nos arquivos
      tocados).
- [ ] `./rebuild-web.sh -b` + restart.
- [ ] Validação ao vivo (depende de VPN): login → Perfil → Dynatrace → configurar → Testar
      Conexão → badge "conectado"; AI Diagnostics não mostra mais a seção Dynatrace; badge de
      monitoramento Dynatrace na aba Pods e a aba Dynatrace continuam funcionando.
- [ ] Comunicar à equipe: quem tinha `ai_email` diferente do e-mail real precisa reconfigurar
      o token Dynatrace uma vez.

---

## Retomando em outra sessão

Se esta sessão for interrompida, o próximo chat pode ler este arquivo do zero e seguir direto
pro checklist — as seções "Descobertas" e "Decisões de escopo" acima já têm os números de linha
e nomes de função necessários pra não precisar re-explorar o código.
