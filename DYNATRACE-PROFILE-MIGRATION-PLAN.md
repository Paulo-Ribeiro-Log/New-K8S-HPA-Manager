# Plano: Mover autenticação Dynatrace da aba AI Diagnostics para o Perfil do Usuário — 🟡 Fases 1-3 concluídas, Fase 4 parcial (bloqueio documentado), Fase 5 pendente

**Branch da implementação**: `feat/dynatrace-profile-auth-backend` (a partir de `main`, ainda não
pusheada/PR aberto no momento em que este checklist foi atualizado).

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

### Fase 1 — Backend ✅ concluída (commit `6ee7b91b`)

- [x] `internal/web/handlers/dynatrace.go`: novo `SaveConfig(c *gin.Context)` — lê
      `c.GetString("user_email")`, busca tokens existentes, faz merge só das 3 colunas
      Dynatrace (mesma lógica de `ai_tokens.go:286-298`), chama `tokensStore.SaveTokens(...)`.
- [x] `internal/web/handlers/dynatrace.go`: `GetConfig` — trocado `c.Query("ai_email")` por
      `c.GetString("user_email")`.
- [x] `internal/web/handlers/dynatrace.go`: `TestConnection` — trocada leitura de `ai_email` do
      body por `c.GetString("user_email")`.
- [x] `internal/web/server.go`: `dt := api.Group("/dynatrace")` — `rbacMiddleware.InjectUserEmail()`
      adicionado só nas 3 rotas afetadas (`GET/POST /config`, `POST /test`), demais rotas do
      grupo (management-zones, problems, investigate, etc.) sem middleware novo — conforme
      decisão de escopo. `dt.POST("/config", dtHandler.SaveConfig)` registrado.
- [x] `go build ./...`, `gofmt -l`, `SKIP_AZURE_TESTS=1 go test ./internal/... -race` — OK.

### Fase 2 — Frontend: novo modal de perfil ✅ concluída (commit `a8f97bf5`)

- [x] Novo `internal/web/frontend/src/components/profile/DynatraceCredentialModal.tsx`,
      seguindo a estrutura de `GitHubCredentialModal.tsx` (usa `CredentialModalProps`,
      `useUserPermissions()` pra exibir o e-mail — somente leitura, sem campo de texto).
- [x] Campos URL/Token (show/hide)/Tag Filter reaproveitando textos/placeholders do bloco
      antigo de `AISettingsTab.tsx`.
- [x] Botão "Testar Conexão" → `apiClient.testDynatraceConnection()` sem `ai_email`.
- [x] Salvar → novo `apiClient.saveDynatraceConfig({ dynatrace_url, dynatrace_token,
      dynatrace_tag_filter })` em `client.ts` (sem `ai_email` no payload).
- [x] Assinatura de `apiClient.getDynatraceConfig()` atualizada em `client.ts` (sem `ai_email`).
- [x] `internal/web/frontend/src/types/profile.ts`: `dynatrace?: CredentialInfo` adicionado em
      `CredentialsState`.
- [x] `internal/web/frontend/src/hooks/useUserProfile.ts`: `dynatraceStatus`/`dynatraceLoading`
      + `checkDynatrace` no mount effect + `refreshCredentials` + `dynatrace: {...}` no retorno.
- [x] `internal/web/frontend/src/components/UserProfileMenu.tsx`: entrada "Dynatrace" na seção
      Credenciais, abrindo o novo modal.

### Fase 3 — Frontend: remover o bloco antigo ✅ concluída (commit `e12a7525`)

- [x] Removido o bloco "Dynatrace Integration" inteiro de `AISettingsTab.tsx` (JSX + os
      `<Separator/>` ao redor, sem duplicar separador).
- [x] Removido estado (`dynatraceURL`, `dynatraceToken`, `dynatraceTagFilter`,
      `showDynatraceToken`, `dtTesting`, `dtTestResult`), handler `handleTestDynatrace`, linhas
      do payload Dynatrace em `handleSave()`, e o badge "Status Atual" correspondente.
- [x] `tokenStatus?.has_dynatrace`/`dynatrace_url`/`dynatrace_tag_filter` removidos de
      `TokenStatus` e de `loadTokenStatus()` — sem uso órfão restante. Sweep adicional via grep
      encontrou e removeu 2 chamadas `setDynatrace*("")` residuais (reset pós-save e reset de
      troca de e-mail).

### Fase 4 — Frontend: trocar fonte do `ai_email` nos demais consumidores 🟡 parcial (commit `97efc82b`)

- [x] `grep -rn "ai_email" internal/web/frontend/src` — lista completa de call sites confirmada.
- [x] **Migrados** (badge de monitoramento Dynatrace, endpoint puro sem resolução de provedor
      de IA — seguro): `PodsPanel.tsx`, `DaemonSetsTab.tsx`, `DeploymentsTab.tsx` — `aiEmailForDT`
      agora vem de `useUserPermissions().data?.email` em vez de
      `localStorage.getItem("ai_email")`.
- [ ] **Bloqueados — decisão arquitetural pendente, não migrados nesta sessão**:
      `DynatraceTab.tsx`, `HealthCheckResultsPanel.tsx`, `HealthCheckingTab.tsx`,
      `DeploymentBehaviorChart.tsx`. Motivo: nesses arquivos o MESMO `ai_email` também é usado
      pelo backend para resolver o **provedor de IA** (`h.aiHandler.GetProviderForUser(email)`
      dentro de `AnalyzeProblem`/`InvestigateProblem`/`analyzeCorrelatedItem`/
      `analyzeOneAgentSignal`/`analyzeCorrelatedBatch`, e no fluxo de Health Check via
      `check_dynatrace`/`check_oneagent_signals`) — não só o token Dynatrace. Trocar a fonte só
      no frontend, sem separar as duas identidades no backend, arriscaria quebrar a seleção do
      provedor de IA para qualquer usuário cujo `ai_email` digitado seja diferente do e-mail
      real de login. Ver mensagem completa do commit `97efc82b` para o detalhamento por arquivo.
      **Próximo passo sugerido**: decidir (a) manter os dois desacoplados de propósito (Dynatrace
      via login real, IA via `ai_email` manual) documentando a divergência, ou (b) desacoplar os
      dois parâmetros no backend (`ai_email` continua só para IA; novo parâmetro/identidade
      própria para Dynatrace nesses endpoints) antes de migrar o restante do frontend.
      `CommandRunnerTab.tsx`/`FinOpsTab.tsx`/`NodePoolEditor.tsx`/`useAIDiagnostics.ts` usam
      `ai_email` só para IA (não-Dynatrace) — corretamente fora de escopo, não tocar.

### Achado extra não planejado — bug real corrigido (commit `2784ebaf`)

Durante a validação ao vivo do `SaveConfig` novo, descoberto que `GetTokens()`
(`internal/storage/user_tokens_store.go`) quebrava com
`sql: Scan error ... converting NULL to string is unsupported` para qualquer usuário cuja única
linha em `user_ai_tokens` tivesse sido criada por um INSERT parcial (`SaveGitHubEditorProfiles`/
`SaveCloudAccountHints`, que só preenchem `user_email` + a própria coluna + `preferred_provider`,
deixando `gemini_api_key`/`metadata`/etc. como SQL NULL genuíno). Bug pré-existente, não
introduzido por esta migração — só surgiu porque `SaveConfig` foi o primeiro caminho de código a
chamar `GetTokens` para um usuário real nesse estado. Corrigido convertendo os scans afetados
para `sql.NullString` (benefícia TODOS os chamadores de `GetTokens`, não só o Dynatrace). 2 testes
de regressão novos em `internal/storage/user_tokens_store_test.go`.

### Fase 5 — Verificação 🟡 parcial

- [x] `go build ./...`, `gofmt -l`, `SKIP_AZURE_TESTS=1 go test ./internal/... -race` — OK em
      cada commit.
- [x] `./rebuild-web.sh -b` + restart — feito durante a implementação.
- [x] Validação via `curl` direto (login JWT real + `GET/POST /dynatrace/config`,
      `POST /dynatrace/test`) contra um usuário real (`4960023587.ca@via.com.br`) — confirmado
      save/load funcionando após o fix do bug de NULL acima; dados de teste limpos do banco real
      depois (`cmd/cleanup_debug`, deletado).
- [ ] **Validação em navegador ainda não feita** (sem ferramenta de browser disponível nesta
      sessão): login → Perfil → Dynatrace → configurar → Testar Conexão → badge "conectado"; AI
      Diagnostics não mostra mais a seção Dynatrace; badge de monitoramento Dynatrace na aba
      Pods/Deployments/DaemonSets e a aba Dynatrace continuam funcionando visualmente.
- [x] `npx tsc --noEmit -p internal/web/frontend` — 0 erros. `npm run lint` — 548 problemas
      pré-existentes no repo inteiro (nenhum nos arquivos tocados por esta migração; conferido
      por linha — os únicos erros/warnings em `AISettingsTab.tsx`/`DaemonSetsTab.tsx`/
      `DeploymentsTab.tsx`/`PodsPanel.tsx`/`client.ts` ficam em trechos não relacionados às
      mudanças desta migração).
- [ ] Comunicar à equipe: quem tinha `ai_email` diferente do e-mail real precisa reconfigurar
      o token Dynatrace uma vez.
- [ ] Branch `feat/dynatrace-profile-auth-backend` ainda não pusheada / sem PR aberto.
- [ ] Reconciliar com `docs/dynatrace-profile-migration-plan` (PR #332, aberto, tem a versão
      original não marcada deste checklist) — decidir se fecha em favor deste arquivo atualizado
      ou se faz merge/rebase.

---

## Retomando em outra sessão

Se esta sessão for interrompida, o próximo chat pode ler este arquivo do zero e seguir direto
pro checklist — as seções "Descobertas" e "Decisões de escopo" acima já têm os números de linha
e nomes de função necessários pra não precisar re-explorar o código.
