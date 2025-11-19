# 🌐 Interface Web (React/TypeScript)

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Quick Start Web

```bash
# Development (2 terminais)
make web-install                              # Terminal 1: Install dependencies
make web-dev                                  # Terminal 1: Vite dev server (port 5173)
./build/k8s-hpa-manager web --port 8080       # Terminal 2: Backend API

# Production Build
make build-web                                # Build frontend + Go binary (embeds static/)
./build/k8s-hpa-manager web                   # Run integrated server (background mode)

# Background vs Foreground
./build/k8s-hpa-manager web                   # Background (default) - daemon mode
./build/k8s-hpa-manager web -f                # Foreground - logs no terminal
# Auto-shutdown: 20 min após última página fechar (sistema de heartbeat)
```

## Tech Stack Frontend

| Tecnologia | Versão | Uso |
|------------|--------|-----|
| **React** | 18.3 | UI framework |
| **TypeScript** | 5.8 | Type safety |
| **Vite** | 5.4 | Build tool (HMR rápido) |
| **shadcn/ui** | Latest | UI components (Radix UI) |
| **Tailwind CSS** | 3.4 | Styling |
| **React Query** | TanStack | Server state management |
| **React Router** | DOM | Client-side routing |
| **Lucide React** | Latest | Icons |
| **Recharts** | Latest | Charts (Dashboard) |

## Sistema de Heartbeat e Auto-Shutdown

**Problema resolvido:** Servidor web rodando em background consome recursos indefinidamente mesmo sem uso.

**⚠️ CORREÇÃO CRÍTICA (Outubro 2025):** Dois bugs críticos foram corrigidos no sistema de heartbeat que causavam shutdown prematuro. Ver detalhes completos na seção [Histórico de Correções](../history/CHANGELOG.md#correção-crítica-sistema-de-heartbeatauto-shutdown-outubro-2025-).

**Solução:**
- **Frontend**: Hook `useHeartbeat` envia POST `/heartbeat` a cada 5 minutos
- **Backend**: Reseta timer de 20 minutos (ou 30min inicial) ao receber heartbeat
- **Auto-shutdown**: Servidor desliga automaticamente se nenhuma página conectada por 20min
- **Thread-safe**: `sync.RWMutex` protege timestamp de heartbeat + `sync.Mutex` protege timer (corrigido em Oct/2025)

**Implementação:**

```typescript
// Frontend: hooks/useHeartbeat.ts
useEffect(() => {
  const sendHeartbeat = async () => {
    await fetch('/heartbeat', { method: 'POST' });
  };

  sendHeartbeat(); // Imediato ao montar
  const interval = setInterval(sendHeartbeat, 5 * 60 * 1000); // 5 min

  return () => clearInterval(interval);
}, []);
```

```go
// Backend: internal/web/server.go
func (s *Server) startInactivityMonitor() {
    s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)
}

func (s *Server) handleHeartbeat(c *gin.Context) {
    s.heartbeatMutex.Lock()
    s.lastHeartbeat = time.Now()
    s.heartbeatMutex.Unlock()

    if s.shutdownTimer != nil {
        s.shutdownTimer.Stop()
    }
    s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)
}
```

## Features Implementadas Web

| Feature | Status | Descrição |
|---------|--------|-----------|
| **HPAs** | ✅ 100% | CRUD completo com edição de recursos (CPU/Memory Request/Limit) + Aplicar Agora |
| **Node Pools** | ✅ 100% | Editor funcional (autoscaling, node count, min/max) + **Botão "Aplicar Agora"** |
| **CronJobs** | ✅ 100% | Suspend/Resume |
| **Prometheus Stack** | ✅ 100% | Resource management + **Rollout individual (Deployment/StatefulSet/DaemonSet)** |
| **Sessions** | ✅ 100% | Save/Load/Rename/Delete/Edit (compatível TUI) |
| **Staging Area** | ✅ 100% | Preview de alterações antes de aplicar |
| **ApplyAllModal** | ✅ 100% | Progress tracking com rollout simulation |
| **Dashboard** | ✅ 100% | Grid 2x2 com métricas reais (CPU/Memory allocation) |
| **Snapshot Cluster** | ✅ 100% | Captura estado atual para rollback |
| **Heartbeat System** | ✅ 100% | Auto-shutdown em 20min inatividade |
| **Log Viewer** | ✅ 100% | Modal com logs em tempo real (app + servidor), auto-refresh, copiar, exportar CSV, limpar |
| **System Namespaces Toggle** | ✅ 100% | Filtro de namespaces de sistema (kube-*, monitoring, etc.) com botão toggle |
| **SSE Progress Bar** | ✅ 100% | Progress bar em tempo real para operações Cordon/Drain via Server-Sent Events |

## Workflow Session Management (Web)

```
1. Editar HPAs/Node Pools → Staging Area (mudanças pendentes em memória)
2. "Save Session" → Modal com folders (HPA-Upscale/Downscale/Node-Upscale/Downscale)
3. Templates de nomenclatura: {action}_{cluster}_{timestamp}_{env}
4. "Load Session" → Grid de sessões com dropdown menu (⋮)
5. Dropdown actions:
   - Load: Carrega para Staging Area
   - Rename: Altera nome da sessão
   - Edit Content: EditSessionModal (edita HPAs/Node Pools salvos)
   - Delete: Remove sessão (com confirmação)
6. "Apply Changes" → ApplyAllModal com preview before/after
7. Progress tracking: Rollout simulation com progress bars
```

## Snapshot de Cluster para Rollback

**Feature NOVA (Outubro 2025):**
- Captura estado atual do cluster (TODOS os HPAs + Node Pools)
- Salva como sessão sem modificações (original_values = new_values)
- Permite rollback completo em caso de incident

**Workflow:**
```
1. Selecionar cluster
2. "Save Session" → Detecta staging vazio
3. Modal oferece "Capturar Snapshot do Cluster"
4. Backend busca dados FRESCOS via API K8s/Azure (não usa cache)
5. Salva em folder "Rollback" ou custom
6. Para restaurar: Load session → Apply
```

## Toggle de Namespaces de Sistema

**Feature NOVA (Outubro 2025):**
- Filtro inteligente de namespaces de sistema (kube-system, kube-public, monitoring, etc.)
- Botão toggle na mesma linha do título "Available HPAs"
- Estados visuais distintos: ON (azul/primary) e OFF (cinza/muted)
- Default: desabilitado (namespaces de sistema ocultos)

**Implementação:**
- **Backend**: Query parameter `showSystem=true` em `/api/v1/hpas`
- **Frontend**: Estado React com ícones Eye/EyeOff
- **Filtro**: Lista de 53+ namespaces de sistema em `internal/kubernetes/client.go`
- **Posicionamento**: Propriedade `titleAction` no componente `SplitView`

**Workflow:**
```
1. Usuário acessa página de HPAs
2. Por padrão, namespaces de sistema estão ocultos (botão OFF - cinza)
3. Clicar no botão toggle:
   - ON (Eye + azul): Mostra namespaces de sistema
   - OFF (EyeOff + cinza): Oculta namespaces de sistema
4. Backend filtra usando isSystemNamespace()
5. Lista de HPAs atualizada automaticamente via useEffect
```

**Namespaces de sistema filtrados:**
- Kubernetes core: `kube-system`, `kube-public`, `kube-node-lease`, `default`
- Monitoring: `monitoring`, `prometheus`, `grafana`, `kube-prometheus-stack`
- Networking: `calico-system`, `tigera-operator`, `istio-system`, `linkerd`
- Storage: `rook-ceph`, `longhorn-system`, `openebs`
- CI/CD: `argocd`, `flux-system`, `tekton-pipelines`
- Logging: `logging`, `elastic-system`, `loki`
- Security: `cert-manager`, `vault`, `gatekeeper-system`
- E mais 30+ namespaces...

**Arquivos modificados:**
- `internal/web/handlers/hpas.go` - Parse query parameter `showSystem`
- `internal/web/frontend/src/lib/api/client.ts` - Parâmetro `showSystem` em `getHPAs()`
- `internal/web/frontend/src/hooks/useAPI.ts` - Hook `useHPAs` com `showSystem`
- `internal/web/frontend/src/components/SplitView.tsx` - Suporte a `titleAction`
- `internal/web/frontend/src/pages/Index.tsx` - Estado e botão toggle

## Rebuild Web Obrigatório

**IMPORTANTE**: Sempre use o script recomendado para rebuilds web:

```bash
./rebuild-web.sh -b           # Build completo (frontend + backend)
```

**Por que não usar `make build` direto:**
- Cache do Vite pode causar stale files
- Static files podem não embedar corretamente
- Frontend e backend precisam sincronizar versões

**Após rebuild:**
1. Hard refresh no browser: `Ctrl+Shift+R`
2. Verificar logs: `/tmp/k8s-hpa-manager-web-*.log` (modo background)

## API Endpoints

**Base URL**: `http://localhost:8080/api/v1`

**Autenticação**: Bearer token no header `Authorization: Bearer poc-token-123`

| Endpoint | Method | Descrição |
|----------|--------|-----------|
| `/clusters` | GET | Lista clusters disponíveis |
| `/namespaces?cluster=X` | GET | Lista namespaces do cluster |
| `/hpas?cluster=X&namespace=Y` | GET | Lista HPAs |
| `/hpas/:cluster/:namespace/:name` | PUT | Atualiza HPA |
| `/nodepools?cluster=X` | GET | Lista node pools |
| `/nodepools/:cluster/:rg/:name` | PUT | Atualiza node pool |
| `/nodepools/progress/:operationId` | GET | SSE stream de progresso Cordon/Drain |
| `/nodepools/progress/:operationId/status` | GET | Status da operação SSE |
| `/sessions` | GET | Lista sessões salvas |
| `/sessions` | POST | Salva nova sessão |
| `/sessions/:name` | DELETE | Remove sessão |
| `/sessions/:name/rename` | PUT | Renomeia sessão |
| `/sessions/:name` | PUT | Atualiza conteúdo da sessão |
| `/cronjobs?cluster=X&namespace=Y` | GET | Lista CronJobs |
| `/prometheus?cluster=X` | GET | Lista recursos Prometheus |
| `/prometheus/:cluster/:namespace/:type/:name/rollout` | POST | **Rollout de recurso Prometheus (deployment/statefulset/daemonset)** |
| `/logs` | GET | Retorna logs da aplicação e servidor (buffer + arquivos) |
| `/logs` | DELETE | Limpa buffer de logs da aplicação |
| `/heartbeat` | POST | Heartbeat (mantém servidor vivo) |
