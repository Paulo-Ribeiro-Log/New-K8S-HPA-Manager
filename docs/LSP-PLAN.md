# LSP no Editor de Código — Plano de Implementação

## Por que não usar `monaco-languageclient`

`monaco-languageclient` v8+ exige `@codingame/monaco-vscode-editor-api` como
peer dependency — uma fork pesada do Monaco que **substitui** o `monaco-editor`
padrão. Instalar quebraria `monaco-yaml`, o worker de YAML e toda a configuração
atual do editor.

**Solução adotada**: providers nativos do Monaco (`registerCompletionItemProvider`,
`registerHoverProvider`, `registerDefinitionProvider`, `setModelMarkers`) chamando
o backend via HTTP. O backend mantém um processo de language server (`gopls` /
`pyright`) por repositório e faz proxy dos pedidos JSON-RPC via stdin/stdout.

---

## Arquitetura

```
Monaco (browser)
  │  registerCompletionItemProvider("go", ...)
  │  registerHoverProvider("go", ...)
  │  registerDefinitionProvider("go", ...)
  │  setModelMarkers(...)  ← diagnósticos
  │
  │  HTTP POST /api/v1/code-editor/repos/:id/lsp/complete
  │  HTTP POST /api/v1/code-editor/repos/:id/lsp/hover
  │  HTTP POST /api/v1/code-editor/repos/:id/lsp/definition
  │  HTTP GET  /api/v1/code-editor/repos/:id/lsp/diagnostics?path=...
  ▼
Go backend (CodeEditorLSPHandler)
  │  lspSessionManager: sync.Map[repoId/lang → *lspSession]
  │  cada sessão: processo gopls/pyright com stdin/stdout JSON-RPC
  │  protocolo: Content-Length header + JSON body (LSP spec)
  │  timeout de inatividade: 10 minutos (mata o processo)
  ▼
gopls (~/go/bin/gopls) ou pyright (npx pyright --stdio)
  Workspace root = ~/.k8s-hpa-manager/repos/<owner>-<repo>/
```

### Protocolo LSP JSON-RPC (stdin/stdout)

```
Content-Length: 123\r\n
\r\n
{"jsonrpc":"2.0","id":1,"method":"textDocument/completion","params":{...}}
```

O backend gerencia `initialize` (uma vez por sessão) e encaminha os pedidos
HTTP do frontend como requests LSP, aguardando a resposta correspondente pelo `id`.

---

## Fase 1 — TypeScript/JavaScript (Monaco built-in) ✅ Concluída

Sem backend, sem novos pacotes npm. Monaco já embarca `tsserver` em Web Worker.

**Implementação** (em `handleEditorMount`, segundo argumento `monacoInstance`):
```typescript
const handleEditorMount: OnMount = (editor, monacoInstance) => {
  // TS/JS: habilita worker built-in com opções relaxadas
  monacoInstance.languages.typescript.typescriptDefaults.setCompilerOptions({
    target: monacoInstance.languages.typescript.ScriptTarget.ESNext,
    moduleResolution: monacoInstance.languages.typescript.ModuleResolutionKind.NodeJs,
    module: monacoInstance.languages.typescript.ModuleKind.ESNext,
    jsx: monacoInstance.languages.typescript.JsxEmit.ReactJSX,
    allowJs: true, allowSyntheticDefaultImports: true, esModuleInterop: true,
    strict: false, noImplicitAny: false,
  });
  monacoInstance.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: false,   // erros de tipo (em vermelho)
    noSyntaxValidation: false,     // erros de sintaxe
    onlyVisible: true,             // só valida modelos visíveis
  });
  // ... resto do handleEditorMount
};
```

**Funcionalidades ativas após Fase 1:**
- Completions TS/JS (incluindo `.tsx`, `.jsx`) — sem instalar nada
- Erros de sintaxe e tipo inline (squiggles vermelhos/amarelos)
- Hover com tipo inferido
- Go to Definition (mesmo arquivo)
- Rename symbol (Monaco built-in)

---

## Fase 2 — Go via gopls ✅ Concluída

### Backend (`internal/web/handlers/code_editor_lsp.go`)

```go
type lspSession struct {
    lang     string
    repoDir  string
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   *bufio.Reader
    mu       sync.Mutex
    nextID   int
    pending  map[int]chan lspResponse
    lastUsed time.Time
}
```

**Endpoints HTTP:**
```
POST /api/v1/code-editor/repos/:id/lsp/open       — textDocument/didOpen (inicia sessão se necessário)
POST /api/v1/code-editor/repos/:id/lsp/change     — textDocument/didChange
POST /api/v1/code-editor/repos/:id/lsp/complete   — textDocument/completion
POST /api/v1/code-editor/repos/:id/lsp/hover      — textDocument/hover
POST /api/v1/code-editor/repos/:id/lsp/definition — textDocument/definition
GET  /api/v1/code-editor/repos/:id/lsp/diagnostics?path=... — diagnósticos acumulados
DELETE /api/v1/code-editor/repos/:id/lsp          — shutdown + kill
```

**Lifecycle:**
- Sessão criada no primeiro `open` do arquivo
- `initialize` + `initialized` na criação
- Inatividade > 10min → `shutdown` + `exit` + kill
- Diagnósticos chegam como notificação `textDocument/publishDiagnostics` (push do gopls) → armazenados em memória por arquivo
- Cleanup de sessões mortas a cada 5min

### Frontend (`CodeEditorTab.tsx`)

```typescript
// Chamado quando um arquivo .go é aberto
async function initGoLSP(repoId: string, filePath: string, content: string) {
  await apiClient.lspOpen(repoId, "go", filePath, content);
}

// Provider de completions
monacoInstance.languages.registerCompletionItemProvider("go", {
  triggerCharacters: [".", "(", " "],
  provideCompletionItems: async (model, position) => {
    const items = await apiClient.lspComplete(repoId, "go", currentFilePath,
      model.getValue(), position.lineNumber - 1, position.column - 1);
    return { suggestions: items.map(convertLSPCompletion) };
  }
});

// Provider de hover
monacoInstance.languages.registerHoverProvider("go", { ... });

// Provider de go-to-definition  
monacoInstance.languages.registerDefinitionProvider("go", { ... });

// Polling de diagnósticos (a cada 2s quando aba Go está ativa)
useEffect(() => {
  if (!activeGoFile) return;
  const interval = setInterval(() => pollDiagnostics(), 2000);
  return () => clearInterval(interval);
}, [activeGoFile, repoId]);
```

---

## Fase 3 — Python via pyright (planejada)

Mesma arquitetura da Fase 2. `pyright --stdio` como processo filho.

Requer `pyright` instalado no servidor: `npm install -g pyright` ou `pipx install pyright`.

**Detecção**: se `pyright` não estiver no PATH, LSP Python não é iniciado (sem erro ao usuário).

---

## Detalhes de Implementação

### Conversão LSP → Monaco (completions)

| LSP `CompletionItemKind` | Monaco `CompletionItemKind` |
|---|---|
| 1 Text | `Snippet` |
| 2 Method | `Method` |
| 3 Function | `Function` |
| 5 Field | `Field` |
| 6 Variable | `Variable` |
| 7 Class | `Class` |
| 9 Module | `Module` |
| 14 Keyword | `Keyword` |

### Conversão LSP → Monaco (diagnósticos)

| LSP `DiagnosticSeverity` | Monaco `MarkerSeverity` |
|---|---|
| 1 Error | 8 Error |
| 2 Warning | 4 Warning |
| 3 Information | 2 Info |
| 4 Hint | 1 Hint |

### Posição LSP vs Monaco

LSP usa 0-indexed (line 0, char 0). Monaco usa 1-indexed (line 1, col 1).
Conversão: `lspLine = monacoLine - 1`, `lspChar = monacoCol - 1`.

---

## Limitações

- **Sem WebSocket bidirecional**: diagnósticos chegam por polling (2s), não push em tempo real. Aceitável para dev tool.
- **Uma sessão por repositório**: todos os arquivos Go do mesmo repo compartilham o mesmo processo gopls — correto, pois o gopls precisa do workspace inteiro para análise.
- **gopls frio**: a primeira completion após abrir o repositório pode demorar 3-5s enquanto o gopls indexa o workspace. Após isso, respostas < 500ms.
- **Cross-file Go to Definition**: funciona via gopls. Abre o arquivo alvo em nova aba.
- **TypeScript sem node_modules**: erros de "cannot find module" são esperados em arquivos com imports de pacotes externos. Usar `noSemanticValidation: true` se os falsos positivos forem perturbadores.
