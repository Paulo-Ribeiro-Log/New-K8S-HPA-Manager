# ⚠️ Common Pitfalls / Gotchas

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Web Development

1. **SEMPRE usar `./rebuild-web.sh -b`** para builds web
   - ❌ NÃO: `npm run build && make build` (pode causar cache issues)
   - ✅ SIM: `./rebuild-web.sh -b`

2. **Hard refresh obrigatório** após rebuild
   - `Ctrl+Shift+R` no browser para limpar cache JavaScript

3. **TabProvider obrigatório** no App.tsx
   - Deve envolver `StagingProvider` e outros contexts
   - Erro sem TabProvider: "useTabManager must be used within a TabProvider"

4. **Cluster name suffix mismatch**
   - Sessions salvam sem `-admin` (ex: `akspriv-prod`)
   - Kubeconfig contexts têm `-admin` (ex: `akspriv-prod-admin`)
   - **Fix**: `StagingContext.loadFromSession()` adiciona `-admin` automaticamente
   - **Fix**: `findClusterInConfig()` remove `-admin` para matching

5. **Staging context patterns**
   - ❌ NÃO existe: `staging.add()`, `staging.getNodePool()`
   - ✅ Usar: `staging.addHPAToStaging()`, `staging.stagedNodePools.find()`

6. **Background mode logs**
   - Logs salvos em `/tmp/k8s-hpa-manager-web-*.log`
   - Use `tail -f /tmp/k8s-hpa-manager-web-*.log` para debug

## TUI Development

1. **Sempre usar `[]rune` para texto** (Unicode-safe)
   ```go
   // ❌ ERRADO
   text := "Hello"
   text[0] = 'h' // Não funciona com emojis

   // ✅ CORRETO
   runes := []rune("Hello 👋")
   runes[0] = 'h'
   text = string(runes)
   ```

2. **ESC deve preservar contexto**
   - Usar `handleEscape()` centralizado em `handlers.go`
   - NUNCA fazer `return tea.Quit` direto no ESC
   - Exemplo: F9 (CronJobs) → ESC → volta para Namespaces (preserva seleções)

3. **Estado sempre em AppModel**
   - `internal/models/types.go` é a ÚNICA fonte de verdade
   - NUNCA criar estado local em handlers ou views
   - Bubble Tea messages para comunicação assíncrona

4. **Bubble Tea messages para async**
   - NUNCA usar goroutines diretas para operações K8s/Azure
   - Sempre retornar `tea.Cmd` que envia mensagem quando completo
   ```go
   // ❌ ERRADO
   go func() {
       applyHPA() // Race condition!
   }()

   // ✅ CORRETO
   return func() tea.Msg {
       err := applyHPA()
       return HPAAppliedMsg{err: err}
   }
   ```

5. **Mutex para concorrência**
   - `clientMutex` em `getClient()` - protege criação de K8s clients
   - `heartbeatMutex` em web server - protege timestamp
   - Double-check locking pattern para performance

## Azure CLI

1. **Warnings não são erros**
   - `pkg_resources deprecated` → ignorar
   - `isOnlyWarnings()` em `executeAzureCommand()` separa stderr real de warnings

2. **Scale com autoscaling habilitado**
   - Azure CLI rejeita `scale` se autoscaling enabled
   - **Ordem correta**: Disable autoscaling → Scale → Enable autoscaling
   - Ver `buildNodePoolCommands()` em `app.go` para lógica de 4 cenários

3. **Timeout de 5 segundos**
   - Validação Azure com timeout evita travamento em problemas de rede/DNS
   - Ver `configurateSubscription()` em `cmd/root.go`

## Session System

1. **Folders obrigatórios**
   - Save/Load/Delete/Rename requerem `folder` parameter (query string na API)
   - Folders: `HPA-Upscale`, `HPA-Downscale`, `Node-Upscale`, `Node-Downscale`, `Mixed`

2. **Metadata auto-calculada**
   - NÃO editar manualmente campos `clusters_affected`, `namespaces_affected`
   - Backend recalcula automaticamente ao salvar/atualizar sessão

3. **Compatibilidade TUI ↔ Web**
   - Mesmo formato JSON
   - Mesma estrutura de diretórios (`~/.k8s-hpa-manager/sessions/`)
   - `SessionManager` Go compartilhado por ambos

## Race Conditions Conhecidas (RESOLVIDAS)

1. **getClient() race condition** ✅ RESOLVIDO
   - Múltiplos goroutines criavam clients simultaneamente
   - **Fix**: `sync.RWMutex` com double-check locking
   - Ver `internal/config/kubeconfig.go`

2. **testClusterConnections() race** ✅ RESOLVIDO
   - `tea.Batch()` iniciava todos testes em paralelo
   - **Fix**: Mutex protege criação de clients (read lock para leituras, write lock para criação)
