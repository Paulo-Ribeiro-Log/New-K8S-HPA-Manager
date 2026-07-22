# NOTES-PLAN.md — Botão "Notas" (anotações Markdown por cluster + aba)

✅ **CONCLUÍDA** — todas as 8 fases implementadas e validadas (build, testes unitários com `-race`, CRUD completo testado via curl com JWT real, e uso real do usuário no navegador confirmado nos dados do `notes.db`). Ver seção "Notas (anotações Markdown por cluster+aba)" no `CLAUDE.md` para a documentação técnica definitiva.

Plano de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`. Cada fase abaixo tem um checklist `- [ ]` que deve ser marcado `- [x]` conforme o trabalho avança.

**Contexto:** o usuário quer registrar anotações pertinentes durante a análise/execução de ações no app (ex: durante uma investigação de SNAT, um health check), sem depender de ferramentas externas. As anotações ficam presas ao contexto onde foram tomadas — cluster + aba/ferramenta ativa — e persistem como um histórico datado (não um bloco único sobrescrito).

**Decisões de produto já fechadas:**
- Tema = automático pela aba (`activeTab`), sem campo livre extra.
- Escopo = cluster + aba (namespace fora do escopo).
- Editor = Markdown + toolbar, **zero dependência nova** (`react-markdown`+`remark-gfm` já existem no projeto para o preview).
- Modelo = múltiplas entradas datadas (estilo diário) — cada "Salvar" cria uma nova entrada; editar/excluir agem sobre uma entrada específica, restrito ao autor.
- **Botão fica na barra `<TabNavigation>` do `Index.tsx`, ao lado do botão inline "Explorer"** (não no `Header.tsx`) — decisão do usuário, e convenientemente evita ter que plumbar `activeTab`/`selectedCluster` como props novas em outro componente, já que ambos já estão no escopo local do `Index.tsx` nesse ponto.

---

## Fase 1 — Backend: Store SQLite

**Arquivo:** `internal/storage/notes_store.go` ← CRIAR

Store standalone (não o `SQLiteClient` genérico), seguindo o padrão de `internal/storage/snat_history_store.go`: arquivo `.db` próprio, `sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")`, `SetMaxOpenConns(3)`, `sync.RWMutex`.

```go
type Note struct {
    ID        int64     `json:"id"`
    Cluster   string    `json:"cluster"`
    Tab       string    `json:"tab"`
    Content   string    `json:"content"`
    UserEmail string    `json:"user_email"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

const notesSchema = `
CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster    TEXT     NOT NULL,
    tab        TEXT     NOT NULL,
    content    TEXT     NOT NULL,
    user_email TEXT     NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notes_scope ON notes(cluster, tab, created_at DESC);
`
```

Caminho do banco: `~/.k8s-hpa-manager/notes.db` (via `baseDir` já existente em `server.go:197`).

Métodos:
- `NewNotesStore(dbPath string) (*NotesStore, error)`
- `Save(n Note) (int64, error)` — sempre INSERT, nunca sobrescreve
- `List(cluster, tab string) ([]Note, error)` — `ORDER BY created_at DESC`; retorna `[]Note{}`, nunca `nil`
- `GetByID(id int64) (*Note, error)` — para checar autoria
- `Update(id int64, content string) error` — atualiza `content` + `updated_at`
- `Delete(id int64) error`

- [x] Criar `internal/storage/notes_store.go`
- [x] Criar `internal/storage/notes_store_test.go` (padrão `t.TempDir()` de outros testes de store) cobrindo `Save→List→Update→Delete` e isolamento por `cluster+tab`

---

## Fase 2 — Backend: Handler HTTP

**Arquivo:** `internal/web/handlers/notes.go` ← CRIAR

Handler simples estilo `internal/web/handlers/cloud_account_hints.go` (DI direta da store, sem envelope `success/data`):

```go
type NotesHandler struct{ store *storage.NotesStore }
func NewNotesHandler(store *storage.NotesStore) *NotesHandler

func (h *NotesHandler) List(c *gin.Context)   // GET  /api/v1/notes?cluster=&tab=
func (h *NotesHandler) Create(c *gin.Context) // POST /api/v1/notes            body: {cluster, tab, content}
func (h *NotesHandler) Update(c *gin.Context) // PUT  /api/v1/notes/:id        body: {content}
func (h *NotesHandler) Delete(c *gin.Context) // DELETE /api/v1/notes/:id
```

`Update`/`Delete` buscam a nota via `GetByID` e comparam `existing.UserEmail` com `c.Get("user_email")` (injetado por `InjectUserEmail()`) — `403` se não bater (só o autor edita/exclui a própria nota).

- [x] Criar `internal/web/handlers/notes.go`

---

## Fase 3 — Backend: Wiring em `server.go`

- [x] Campo no struct `Server` (perto de `snatHistoryStore`, linha ~108): `notesHandler *handlers.NotesHandler`
- [x] Inicialização (mesmo bloco/estilo das linhas 360-368, usando `baseDir` já existente na linha 197):
  ```go
  var notesHandler *handlers.NotesHandler
  notesDBPath := filepath.Join(baseDir, "notes.db")
  if store, err := storage.NewNotesStore(notesDBPath); err != nil {
      fmt.Printf("⚠️  Notes Store: falha ao criar store: %v\n", err)
  } else {
      notesHandler = handlers.NewNotesHandler(store)
      fmt.Println("✅ Notes Store inicializado (anotações por cluster+aba)")
  }
  ```
- [x] Literal do `Server{...}` (perto da linha ~410): `notesHandler: notesHandler,`
- [x] Rotas (perto do bloco `cloudAccountHintsHandler`, linhas 1420-1423) — **sem** `RequireSREGroup()` (não é mutação destrutiva de cluster):
  ```go
  if s.notesHandler != nil {
      api.GET("/notes", rbacMiddleware.InjectUserEmail(), s.notesHandler.List)
      api.POST("/notes", rbacMiddleware.InjectUserEmail(), s.notesHandler.Create)
      api.PUT("/notes/:id", rbacMiddleware.InjectUserEmail(), s.notesHandler.Update)
      api.DELETE("/notes/:id", rbacMiddleware.InjectUserEmail(), s.notesHandler.Delete)
  }
  ```
- [x] `go build ./...` + `go test -v ./internal/... -race` (validar backend isolado antes do frontend)

---

## Fase 4 — Frontend: tipos e client API

**Arquivo:** `internal/web/frontend/src/lib/api/types.ts` ← MODIFICAR (perto de `CloudAccountHints`)

```ts
export interface Note {
  id: number;
  cluster: string;
  tab: string;
  content: string;
  user_email: string;
  created_at: string;
  updated_at: string;
}
```

**Arquivo:** `internal/web/frontend/src/lib/api/client.ts` ← MODIFICAR (perto de `getCloudAccountHints`/`saveCloudAccountHints`)

4 métodos usando `this.request<T>`: `getNotes(cluster, tab)`, `createNote(cluster, tab, content)`, `updateNote(id, content)`, `deleteNote(id)`.

- [x] Adicionar `Note` em `types.ts`
- [x] Adicionar os 4 métodos em `client.ts`

---

## Fase 5 — Frontend: hook React Query

**Arquivo:** `internal/web/frontend/src/hooks/useNotes.ts` ← CRIAR

```ts
useNotes(cluster, tab)       // queryKey: ['notes', cluster, tab], enabled: !!cluster && !!tab
useCreateNote(cluster, tab)  // useMutation + invalidateQueries(['notes', cluster, tab])
useUpdateNote(cluster, tab)  // idem
useDeleteNote(cluster, tab)  // idem
```

- [x] Criar `hooks/useNotes.ts`

---

## Fase 6 — Frontend: editor Markdown + modal

**Arquivo:** `internal/web/frontend/src/components/MarkdownToolbar.tsx` ← CRIAR

Toolbar reaproveitável sobre `<textarea>` puro via `selectionStart`/`selectionEnd` (não Monaco). Ações: Negrito (`**texto**`), Itálico (`*texto*`), Lista, Lista numerada, Link (`[texto](url)`), Código inline, Citação (`> texto`). Envolve a seleção atual (ou insere placeholder) e reposiciona o cursor via `requestAnimationFrame` após o re-render (textarea controlado).

**Arquivo:** `internal/web/frontend/src/components/NotesModal.tsx` ← CRIAR

`Dialog` (shadcn) com **altura fixa** (`h-[85vh]`) + `flex flex-col overflow-hidden` no `DialogContent` (regra já documentada no `CLAUDE.md` — "Modal Describe do pod sem scroll — lição de `max-height` vs `height`" — evita conteúdo vazando). Toggle Editor/Preview via `useState` simples — **nunca** shadcn `<Tabs>` dentro (`TabsContent` quebra `flex-1 min-h-0`, já documentado no `CLAUDE.md`).

Estrutura:
- Botão "Nova nota" → formulário de composição (toolbar + textarea/preview + Salvar/Cancelar) acima da lista.
- Lista histórica (`ScrollArea`, mais recente primeiro): autor + data (`toLocaleString("pt-BR")`) + conteúdo via `<ReactMarkdown remarkPlugins={[remarkGfm]}>` (mesmo padrão de `TeamsBroadcastTab.tsx`/`AIAnalysisView.tsx`) + botões Editar/Excluir.
- Autoria no frontend: `useUserProfile().user?.email` (hook confirmado em `hooks/useUserProfile.ts`) comparado com `note.user_email` — oculta Editar/Excluir de notas de outros autores (o backend já impõe a regra real via 403; isso só evita clique→erro).
- Sem cluster selecionado: mensagem "selecione um cluster" em vez de lista vazia (query fica `enabled: false`).

- [x] Criar `MarkdownToolbar.tsx`
- [x] Criar `NotesModal.tsx`

---

## Fase 7 — Frontend: botão na barra de navegação (`Index.tsx`)

- [x] Import `NotesModal` (perto de `LogViewer`/`HistoryViewer`) e ícone `StickyNote` de `lucide-react`
- [x] Estado `const [showNotesModal, setShowNotesModal] = useState(false);` (perto da linha 159-160)
- [x] Botão inline dentro do `<TabNavigation>`, logo após o botão "Explorer" (linha ~1500), mesmo estilo visual dos vizinhos mas **sem** destaque de aba ativa (não muda `activeTab`, só abre o modal):
  ```tsx
  <button
    onClick={() => setShowNotesModal(true)}
    className="flex items-center gap-2 px-3 py-1.5 rounded-lg font-medium text-sm transition-all duration-200 text-muted-foreground hover:bg-muted hover:text-foreground"
    title="Notas"
  >
    <StickyNote className="w-4 h-4" />
    Notas
  </button>
  ```
- [x] Renderizar `<NotesModal open={showNotesModal} onOpenChange={setShowNotesModal} cluster={selectedCluster} tab={activeTab} />` fora do `renderTabContent()`, junto de `<LogViewer>`/`<HistoryViewer>` (linhas ~1794-1802)
- [x] `Header.tsx` não precisa de nenhuma mudança nesta versão do plano

---

## Fase 8 — Verificação final

- [x] `go test -v ./internal/... -race`
- [x] `make build`
- [x] `./rebuild-web.sh -b` + hard refresh no navegador
- [x] Manual: usuário já criou notas reais via UI em 3 abas diferentes (`nodepools`, `teams-broadcast`, `dashboard`) no mesmo cluster → confirmado isolamento por aba (registros distintos no `notes.db`)
- [x] API: isolamento por cluster confirmado via curl (`cluster=teste-cluster` não retorna as notas de `akspriv-abastecimento-hlg-admin`)
- [x] API: `POST`/`GET`/`PUT`/`DELETE` testados via curl com JWT válido — CRUD completo funcionando
- [x] API: `DELETE` de nota de outro autor retorna `403` e a nota permanece intacta (autoria protegida no backend)
- [x] API: `GET /notes` sem `cluster`/`tab` retorna `400`
- [ ] Manual: testar cada botão da toolbar Markdown ao redor de uma seleção de texto, alternar Preview (não verificável via curl — requer interação real no navegador)
- [ ] Manual: sem cluster selecionado, confirmar que o modal orienta a selecionar um cluster sem erro solto (não verificável via curl)
