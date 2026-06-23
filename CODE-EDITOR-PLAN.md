# Code Editor — Plano e Checklist de Implementação

Editor de código web integrado ao k8s-hpa-manager, acessível via **Tools → Editor de Código**.
Permite clonar repositórios GitHub, editar arquivos com Monaco e versionar via git, sem sair da aplicação.

---

## Estado Atual (branch `editor-github`, último commit `e09a749d`)

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

## ✅ Concluído — Fase 2

### Edição Avançada
- [x] **Diff visual** — `DiffModal` com Monaco DiffEditor comparando HEAD vs. arquivo atual (`GET /repos/:id/original`)
- [x] **Abas múltiplas** — `openTabs[]` + `activeTabIdx`; barra de abas com close (X) e indicador de não salvo
- [x] **Busca em conteúdo** — toggle grep mode na sidebar; `git grep -n --ignore-case` via `GET /repos/:id/grep?q=`; exibe file:line:content
- [x] **Rename de arquivo** — `RenameDialog` + `POST /repos/:id/rename`; atualiza aba aberta se renomeada
- [x] **Delete de arquivo** — ícone Trash2 no hover do tree + `DELETE /repos/:id/file`; fecha aba se estiver aberta
- [x] **Criar arquivo/pasta** — botões `FilePlus`/`FolderPlus` no cabeçalho da árvore; `CreateFileDialog` + `POST /repos/:id/file/create` e `POST /repos/:id/mkdir`

### Git Avançado
- [x] **Stash / StashPop** — botões no painel Git; `POST /repos/:id/stash` (--include-untracked) e `.../stash/pop`
- [x] **Merge** — `MergeDialog` no painel Branches; `POST /repos/:id/merge` com opção `no_ff`
- [x] **Amend commit** — checkbox no `CommitDialog`; backend suporta `--amend` e `--no-edit`
- [x] **Reset de arquivo** — ícone `RotateCcw` por arquivo no painel Git; `POST /repos/:id/reset-file` (git checkout HEAD ou git clean)
- [x] **Confirmação antes de trocar branch** — alerta se alguma aba tiver mudanças não salvas

### UX
- [x] **Painel redimensionável** — `ResizeDivider` entre sidebar e Monaco (mín 160px, máx 520px, padrão 224px)
- [x] **Persistir largura da sidebar** — `localStorage["ce_sidebar_width"]`
- [x] **Persistir último repo** — `localStorage["ce_last_repo"]`; restaurado ao carregar a aba
- [x] **Confirmação ao fechar aba** com mudanças não salvas (confirm nativo)
- [x] **Minimap toggle** — botão `Map` no cabeçalho do editor (desligado por padrão)

### Segurança
- [x] **Remove token da URL remota após push** — `git remote set-url origin` com URL limpa após push bem-sucedido

---

## ✅ Concluído — Fase 3

### Git Avançado
- [x] **Cherry-pick** — botão "pick" aparece ao hover em cada commit do LogPanel; `POST /repos/:id/cherry-pick`
- [x] **Tags** — sub-aba "Tags" no LogPanel: listar, criar (anotada ou leve) via `CreateTagDialog`, deletar; `GET|POST|DELETE /repos/:id/tags`

### UX
- [x] **Confirm dialog React** — substitui todos os `window.confirm()` nativos por `<Dialog>` assíncrono (`showConfirm()`)
- [x] **Terminal integrado** — painel `RepoTerminal` com xterm.js + PTY real (creack/pty) via WebSocket `GET /repos/:id/terminal`; botão "Terminal" no header toggle show/hide; altura 240px fixada na base da área do editor
- [x] **Limite de repos** — máximo 10 por instância; verificado no `CloneRepo` antes de criar o diretório

### Segurança / Qualidade
- [x] **creack/pty** adicionado ao vendor — terminal com PTY real, suporta cores ANSI, resize, programas interativos

## ✅ Concluído — Fase 4

### Busca e edição
- [x] **Find & Replace global** — painel "Replace" na sidebar; busca com `git grep`, regex opcional, glob filter, dry-run preview, aplicação por arquivo; backend: `POST /repos/:id/replace`
- [x] **Histórico de um arquivo** — `FileHistoryModal` via ícone de contexto na árvore; `git log --follow <path>`; clique no commit abre diff via `GetFileAtCommit`; backend: `GET /repos/:id/file-log?path=`, `GET /repos/:id/file-show?hash=&path=`
- [x] **Upload de arquivo via drag & drop** — arrastar para qualquer diretório da árvore; backend: `POST /repos/:id/upload` (multipart)

### Git
- [x] **Git blame inline** — Monaco decorations por linha com autor + hash abreviado; botão `User` no header do editor toggle; backend: `GET /repos/:id/blame?path=`

## ✅ Concluído — Fase 5

### Git
- [x] **Resolução visual de conflitos de merge** — `ConflictResolverModal` tela cheia; detecta `MERGE_HEAD`; analisa marcadores `<<<<<<<`/`=======`/`>>>>>>>`; botões "Aceitar Atual (HEAD)" / "Aceitar Vindo" por bloco com textarea editável; `MergeBranch` retorna `has_conflicts: true` ao detectar conflitos; "Salvar e Marcar Resolvido" → `POST /resolve-conflict` (git add); "Commit do Merge" → `POST /merge/commit`; "Abortar Merge" → `POST /merge/abort`; backend: `GET /repos/:id/conflicts`, `POST /repos/:id/resolve-conflict`, `POST /repos/:id/merge/abort`, `POST /repos/:id/merge/commit`
- [x] **Diff entre duas branches** — `BranchDiffModal` tela cheia; seletores from/to; diff colorizado linha a linha (verde/vermelho/azul); lista lateral de arquivos alterados com badge A/M/D; backend: `GET /repos/:id/branch-diff?from=&to=` (limite 500 KB)

### UX
- [x] **Preview Markdown** — botão `BookOpen` no header do editor (visível apenas em arquivos `.md`); split 50/50: Monaco à esquerda + `react-markdown` + `remark-gfm` à direita; toggle on/off sem perder o conteúdo editado

## ✅ Concluído — Fase 6

### Terminal
- [x] **Terminal múltiplo** — barra de abas acima do terminal (como VS Code); cada aba tem seu próprio PTY/WebSocket; botão "+" cria nova aba; "×" fecha a aba e encerra o processo; estado em `terminalTabs[]` + `activeTerminalId`; troca de aba sem desmontar o xterm; `visible` prop dispara refit do xterm ao ficar visível

### Qualidade / Infra
- [x] **Quota de disco** — `availableDiskMB()` via `syscall.Statfs` verifica espaço livre antes de clonar; erro HTTP 400 se < 500 MB; `repoSize()` via `du -sh` exibido em cada repo na lista lateral
- [x] **Audit log** — `Commit` registra no `HistoryTracker` com email, repo, remote URL, branch, mensagem e output; `CodeEditorHandler` recebe `*history.HistoryTracker` via construtor

### Gestão de Arquivos (Tree)
- [x] **Drag & drop para mover arquivos** — arrastar nó de arquivo sobre outro diretório; MIME type `application/x-tree-node` diferencia drag interno de upload externo; confirmação antes de executar; atualiza abas abertas se arquivo movido
- [x] **Ctrl+C / Ctrl+X / Ctrl+V na tree** — clipboard React (`{ path, op: "cut"|"copy" }`); Ctrl+V cola no diretório focado com confirmação; corte limpa clipboard após mover; badge indicador no topo da tree; `CopyFile` backend via `io.Copy` + `POST /repos/:id/copy`
- [x] **Foco de diretório atualizado ao abrir arquivo** — `setFocusedDirPath(parentDir)` em `openFile()` garante que criação de arquivo/pasta vai para o diretório correto mesmo quando o foco vem de uma aba aberta
- [x] **Botões de ação na barra de tabs** — `FilePlus`, `FolderPlus`, `RefreshCw` e `X` (fechar repo) movidos para a mesma linha das tabs do painel lateral; removido o sub-header redundante dentro do ScrollArea

### UX / Produtividade
- [x] **Ctrl+P Quick Open** — paleta de arquivos estilo VS Code; filtro em tempo real por nome e caminho (`flattenTree`); navegação com ↑↓, Enter abre, Esc fecha; overlay escuro sobre a área do editor; registrado no Monaco (`addCommand 2048|46`) e via `document.addEventListener` global para quando o editor não está focado
- [x] **Barra de status** — barra azul fixa abaixo do Monaco (altura 20px, estilo VS Code `#007acc`): posição do cursor `Ln X, Col Y`, linguagem detectada, `UTF-8`; toggles de auto-save e format on save embutidos
- [x] **Auto-save** — debounce 1,5s após cada keystroke quando ativado; usa `saveFileRef` para evitar stale closure; toggle na barra de status; estado em `localStorage["ce_autosave"]`
- [x] **Format on save** — ao salvar (Ctrl+S) com toggle ativo, formata antes de gravar em disco (Go/TS/JS/Python/JSON); preserva posição do cursor com `model.setValue` + `setPosition`; atualiza `currentContent` e `savedContent` juntos evitando re-save; toast "Salvo e formatado"; estado em `localStorage["ce_format_on_save"]`

### Autocomplete / LSP (complexo — não implementado)
- [ ] **Go** — `gopls` via processo filho + proxy WebSocket (`POST /repos/:id/lsp/start`); Monaco `MonacoLanguageClient`; requer `monaco-languageclient` no frontend
- [ ] **Python** — `pyright` ou `pylsp`; mesma arquitetura do gopls
- [ ] **TypeScript/JavaScript** — Monaco já tem suporte nativo; habilitar via `tsconfig` do repo

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
