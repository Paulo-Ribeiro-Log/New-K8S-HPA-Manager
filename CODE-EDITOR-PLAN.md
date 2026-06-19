# Code Editor — Plano e Checklist de Implementação

Editor de código web integrado ao k8s-hpa-manager, acessível via **Tools → Editor de Código**.
Permite clonar repositórios GitHub, editar arquivos com Monaco e versionar via git, sem sair da aplicação.

---

## Estado Atual (branch `editor-github`, último commit `7d113d1a`)

### ✅ Concluído — Fase 1 (MVP)

#### Backend (`internal/web/handlers/code_editor.go`)
- [x] `GET  /api/v1/code-editor/repos` — listar repos clonados
- [x] `POST /api/v1/code-editor/clone` — clonar via SSE (streaming de progresso)
- [x] `DELETE /api/v1/code-editor/repos/:id` — remover repo local
- [x] `GET  /api/v1/code-editor/repos/:id/tree` — árvore de arquivos (profundidade 6)
- [x] `GET  /api/v1/code-editor/repos/:id/file?path=...` — ler arquivo (limite 5MB)
- [x] `POST /api/v1/code-editor/repos/:id/file` — salvar arquivo
- [x] `GET  /api/v1/code-editor/repos/:id/status` — git status + ahead/behind
- [x] `GET  /api/v1/code-editor/repos/:id/branches` — listar branches local e remoto
- [x] `POST /api/v1/code-editor/repos/:id/branch` — criar branch + retorna `{ branch, message }` com output do git
- [x] `POST /api/v1/code-editor/repos/:id/checkout` — trocar branch + retorna `{ branch, message }`
- [x] `POST /api/v1/code-editor/repos/:id/pull` — git pull via SSE
- [x] `POST /api/v1/code-editor/repos/:id/commit` — git add + commit + retorna `{ message }` com output do git
- [x] `POST /api/v1/code-editor/repos/:id/push` — git push via SSE
- [x] `GET  /api/v1/code-editor/repos/:id/log?limit=20` — histórico de commits
- [x] `GET  /api/v1/code-editor/repos/:id/diff?path=...` — diff do arquivo
- [x] `GET  /api/v1/code-editor/repos/:id/search?q=...` — busca por nome de arquivo
- [x] GitHub PAT via `GitHubTokenStore` (mesmo do GitHub Releases) + fallback `GITHUB_TOKEN`
- [x] Proteção path traversal: `strings.HasPrefix(fullPath, repoDir)`
- [x] Token mascarado nos logs SSE (nunca exposto ao frontend)
- [x] Repos armazenados em `~/.k8s-hpa-manager/repos/<owner>-<repo>/`

#### Frontend (`internal/web/frontend/src/components/CodeEditorTab.tsx`)
- [x] Entrada via Tools → "Editor de Código" (`ToolsMenu.tsx`, ícone `Code2`)
- [x] Renderizado em `Index.tsx` (case `"code-editor"`)
- [x] Sidebar com **4 abas**: **Arquivos** / **Branches** / **Git** / **Log**
- [x] Árvore de arquivos com expansão/colapso por diretório
- [x] Badge amarelo em arquivos modificados (não salvos e no git)
- [x] Busca de arquivos por nome na sidebar
- [x] Monaco Editor com detecção automática de linguagem por extensão
- [x] Ctrl+S para salvar arquivo atual
- [x] Indicador de "não salvo" (bolinha amarela na aba do arquivo)
- [x] **Painel "Branches"** na sidebar:
  - [x] Branch atual destacado com cor primária
  - [x] Lista de branches locais com ícone de seta para alternar (hover)
  - [x] Lista de branches remotos que não existem localmente (ícone download)
  - [x] Checkout de branch remoto cria tracking branch local automaticamente
  - [x] Spinner no branch durante o checkout
  - [x] Botão "+" para criar novo branch no topo do painel
  - [x] Botão "Atualizar" faz `git fetch --prune` e recarrega a lista
- [x] Branch atual no header é botão que abre o painel Branches diretamente
- [x] **CommitDialog**: exibe output real do git após commit antes de fechar
  - ex: `[main abc1234] feat: ... \n 1 file changed, 2 insertions(+)`
- [x] **BranchDialog**: exibe confirmação + output do git após criar branch
  - mostra nome do branch criado com ícone; fecha automaticamente após 1.5s
- [x] **Toast notifications** no canto inferior direito após cada operação:
  - Checkout → "Alternado para: feature-x"
  - Salvar arquivo → "Salvo: arquivo.go"
  - Commit → "Commit criado com sucesso"
  - Criar branch → "Agora em: feature-x"
  - Remover repo → "Repositório X removido"
- [x] Dialog **Clonar**: URL GitHub, branch opcional, logs SSE em tempo real
- [x] Dialog **Pull/Push** (SSE): progresso em tempo real
- [x] Badges de contagem (arquivos alterados no botão Commit, commits à frente no Push)
- [x] Botão para remover repo local (sem afetar GitHub)
- [x] Seletor de repo por dropdown quando múltiplos estão clonados
- [x] Painel Git mostra ahead/behind do upstream

#### API Client (`internal/web/frontend/src/lib/api/client.ts`)
- [x] Tipos: `CodeEditorRepo`, `CodeEditorFileNode`, `CodeEditorGitStatus`, `CodeEditorBranches`, `CodeEditorLogEntry`
- [x] `codeEditorCommit` retorna `Promise<{ message: string }>` (output do git)
- [x] `codeEditorCreateBranch` retorna `Promise<{ branch: string; message: string }>`
- [x] `codeEditorCheckoutBranch` retorna `Promise<{ branch: string; message: string }>`
- [x] Demais métodos: `codeEditorListRepos`, `codeEditorDeleteRepo`, `codeEditorGetFileTree`, `codeEditorReadFile`, `codeEditorWriteFile`, `codeEditorGetStatus`, `codeEditorGetBranches`, `codeEditorGetLog`, `codeEditorGetDiff`, `codeEditorSearchFiles`

---

## Fase 2 — Melhorias Planejadas

### Edição Avançada
- [ ] **Diff visual antes de commitar** — abrir DiffEditor do Monaco mostrando `git diff HEAD` do arquivo
- [ ] **Aba múltipla de arquivos** — barra de abas com arquivos abertos (como VS Code)
- [ ] **Busca em conteúdo de arquivo** — `grep -r` via `GET /api/v1/code-editor/repos/:id/grep?q=...`
- [ ] **Rename/move/delete de arquivo** — endpoints `PATCH /file` (rename) e `DELETE /file`
- [ ] **Criar arquivo/pasta** — botão "+" na árvore de arquivos

### Git Avançado
- [ ] **Stash** — `POST .../stash` e `POST .../stash/pop`
- [ ] **Merge/Rebase** — dialog de merge com seleção de branch origem
- [ ] **Amend commit** — checkbox "Emendar último commit" no dialog de commit
- [ ] **Reset de arquivo** — botão "Descartar mudanças" por arquivo no painel Git
- [ ] **Cherry-pick** — selecionar commit do log e aplicar no branch atual
- [ ] **Tag** — criar tag a partir de um commit
- [ ] **Confirmação antes de trocar branch** com alterações não commitadas

### UX
- [x] **Painel redimensionável** — `ResizeDivider` entre sidebar e Monaco (mín 160px, máx 520px, padrão 224px)
- [ ] **Persistir largura da sidebar** — salvar `sidebarWidth` no `localStorage`
- [ ] **Persistir arquivo aberto** — salvar `selectedRepo` + `selectedFile` no `localStorage` ao trocar de aba
- [ ] **Confirmação antes de fechar arquivo** modificado não salvo
- [ ] **Minimap** opcional no Monaco (desligado por padrão)
- [ ] **Terminal integrado** — abrir xterm.js na raiz do repo (reutilizar `WebSocketShell`)

### Autocomplete / LSP (Fase 3 — complexo)
- [ ] **Go** — `gopls` via WebSocket proxy (`POST /api/v1/code-editor/repos/:id/lsp/start`)
- [ ] **Python** — `pyright` ou `pylsp`
- [ ] **TypeScript/JavaScript** — Monaco já tem suporte nativo (apenas habilitar `tsconfig`)
- [ ] Monaco já tem autocomplete nativo para: JSON, YAML, HTML, CSS, SQL

### Segurança / Produção
- [ ] **Limite de repos simultâneos** por usuário (evitar disk exhaustion)
- [ ] **Quota de disco** — checar espaço antes de clonar
- [ ] **Audit log** de commits feitos pelo editor (integrar com `HistoryTracker`)
- [ ] **Revoke token da URL remota** após push — substituir URL de volta para `https://github.com/...` sem token

---

## Arquitetura

```
Tools → Editor de Código
         │
         └─ CodeEditorTab.tsx  (layout: sidebar arrastável + Monaco)
               │
               ├─ ResizeDivider  (arrastar borda sidebar↔editor, mín 160 máx 520px)
               ├─ FileTreeNode   (árvore com lazy expand, badges de modificado)
               ├─ BranchesPanel  (local + remoto, checkout inline, criar branch)
               ├─ GitPanel       (status, ahead/behind, botão commit)
               ├─ LogPanel       (histórico de commits)
               ├─ ToastContainer (notificações 4s no canto inferior direito)
               │
               ├─ CloneDialog    → POST /code-editor/clone (SSE)
               ├─ CommitDialog   → POST /code-editor/repos/:id/commit (exibe output git)
               ├─ BranchDialog   → POST /code-editor/repos/:id/branch (exibe output git)
               └─ SseDialog      → POST /code-editor/repos/:id/pull|push (SSE)
```

```
internal/web/handlers/code_editor.go
  CodeEditorHandler
    tokenStore  *storage.GitHubTokenStore   ← PAT por usuário (email via InjectUserEmail)
    reposBase   string                       ← ~/.k8s-hpa-manager/repos/

  runGit(dir, args...)   → exec.CommandContext (timeout 2min)
  buildTree(...)         → recursivo, maxDepth=6, skip ignoredDirs
  ownerRepo(dir)         → extrai owner/repo da URL remota git
```

### Diretórios ignorados na árvore
```go
var ignoredDirs = map[string]bool{
    ".git", "node_modules", "__pycache__", ".idea", ".vscode",
    "vendor", "dist", "build", ".next", "target",
}
```

### Detecção de linguagem (extensão → Monaco language ID)
`go`, `ts/tsx` → typescript, `js/jsx` → javascript, `py` → python,
`yaml/yml` → yaml, `json` → json, `md` → markdown, `sh/bash` → shell,
`tf/hcl` → hcl, `sql` → sql, `dockerfile` → dockerfile, `makefile` → makefile

---

## Dependências

| Componente | Dependência | Já disponível? |
|---|---|---|
| Backend | `os/exec` (git CLI) | ✅ stdlib |
| Backend | `GitHubTokenStore` | ✅ `internal/storage/github_tokens.go` |
| Frontend Monaco | `@monaco-editor/react` | ✅ já instalado |
| Frontend LSP | `monaco-languageclient` | ❌ não instalado (Fase 3) |
| Terminal integrado | xterm.js + WebSocketShell | ✅ já existe (reutilizar) |

---

## Como Continuar

1. **Fazer hard refresh** no browser após qualquer rebuild (Ctrl+Shift+R)
2. **Token GitHub**: configurar via GitHub Releases → ícone de perfil → Token GitHub
   - O mesmo PAT serve para clone/pull/push do editor
3. **Clonar repo privado**: requer PAT com scope `repo` (e SSO autorizado se org corporativa)
4. **Testar**: Tools → Editor de Código → Clonar → `https://github.com/owner/repo`

---

## Pontos de Atenção

- **Monaco no CodeEditorTab NÃO usa `configureMonacoYaml`** — evitar conflito com o singleton em `MonacoYamlEditor.tsx`. Se precisar de YAML schema, reutilizar a flag `_yamlConfigured` existente.
- **Clone injeta token na URL** (`https://TOKEN@github.com/...`) — a URL com token é salva no `.git/config`. A função `remoteURL()` remove o token ao exibir para o usuário, mas o arquivo `.git/config` local contém o token. Para Fase 2: substituir URL após push.
- **SSE clone usa stderr** do git — o progresso do `git clone --progress` vai para stderr, não stdout.
- **Checkout de branch remoto**: se o branch `origin/feature-x` não existir localmente, faz `checkout -b feature-x origin/feature-x`. Se já existir, faz `checkout feature-x`.
- **`git fetch --prune`** é chamado automaticamente ao listar branches — pode demorar se VPN lenta.
- **Toast** usa `setTimeout` de 4s + remoção por ID — não usa biblioteca externa.
- **Tipos de retorno** dos métodos commit/branch/checkout retornam `{ message }` contendo o output real do git (não apenas `void`). Usar isso para exibir feedback ao usuário.
