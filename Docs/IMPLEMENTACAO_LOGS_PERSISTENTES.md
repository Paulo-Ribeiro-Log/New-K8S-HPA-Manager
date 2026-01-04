# Implementação de Logs Persistentes - Health Checking

**Data:** 30/12/2025
**Versão:** v1.3.7+

---

## ✅ Trabalho Concluído

### 1. Backend - Storage SQLite

**Arquivo:** `internal/healthcheck/storage.go`

- ✅ Criada tabela `health_check_events` com campos:
  - `session_id`, `cluster`, `event_type`, `phase`, `message`, `progress`, `status`, `timestamp`
- ✅ Índices criados para performance (session_id, cluster, timestamp)
- ✅ Métodos implementados:
  - `SaveEvent(ctx, event)` - Salvar evento de progresso
  - `GetEvents(ctx, sessionID)` - Recuperar eventos de um cluster
  - `DeleteEvents(ctx, sessionID)` - Limpar eventos antigos

**Struct `ProgressEvent`:**
```go
type ProgressEvent struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Cluster   string    `json:"cluster"`
	Type      string    `json:"type"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Progress  int       `json:"progress"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
```

---

## ✅ Trabalho Concluído (31/12/2025)

### 2. Backend - Salvar Eventos Automaticamente

**Arquivo a modificar:** `internal/healthcheck/orchestrator.go`

**Localização:** Método `publishProgress()` (linha ~200+)

**Modificação necessária:**
```go
func (o *Orchestrator) publishProgress(sessionID string, cluster string, event sse.ProgressEvent) {
	// ✅ Publicar via SSE (código existente)
	o.tracker.SendToClient(sessionID, event)

	// 🆕 ADICIONAR: Salvar no banco
	ctx := context.Background()
	progressEvent := &healthcheck.ProgressEvent{
		SessionID: sessionID,
		Cluster:   cluster,
		Type:      event.Type,
		Phase:     event.Phase,
		Message:   event.Message,
		Progress:  event.Progress,
		Status:    string(event.Status), // converter HealthStatus para string
		Timestamp: event.Timestamp,
	}

	if err := o.storage.SaveEvent(ctx, progressEvent); err != nil {
		log.Error().Err(err).
			Str("session_id", sessionID).
			Str("cluster", cluster).
			Msg("Failed to save progress event")
		// NÃO BLOQUEAR - apenas logar erro
	}
}
```

**Campo adicional no Orchestrator:**
```go
type Orchestrator struct {
	kubeManager *config.KubeConfigManager
	tracker     *sse.ProgressTracker
	storage     *healthcheck.HealthCheckStorage // 🆕 ADICIONAR
}
```

**Modificar construtor:**
```go
func NewOrchestrator(km *config.KubeConfigManager, tracker *sse.ProgressTracker, storage *healthcheck.HealthCheckStorage) *Orchestrator {
	return &Orchestrator{
		kubeManager: km,
		tracker:     tracker,
		storage:     storage, // 🆕
	}
}
```

---

### 3. Backend - Endpoint para Recuperar Eventos

**Arquivo a modificar:** `internal/web/handlers/healthcheck.go`

**Adicionar novo endpoint:**
```go
// GetEvents retorna eventos de progresso de um cluster
// GET /api/v1/healthcheck/events/:sessionId
func (h *HealthCheckHandler) GetEvents(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "session ID é obrigatório",
			},
		})
		return
	}

	ctx := c.Request.Context()
	events, err := h.orchestrator.Storage().GetEvents(ctx, sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to get events")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Erro ao buscar eventos",
				"details": err.Error(),
			},
		})
		return
	}

	// Converter para formato frontend
	responseEvents := make([]gin.H, len(events))
	for i, e := range events {
		responseEvents[i] = gin.H{
			"id":         e.SessionID,
			"type":       e.Type,
			"phase":      e.Phase,
			"message":    e.Message,
			"progress":   e.Progress,
			"status":     e.Status,
			"timestamp":  e.Timestamp.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"events":  responseEvents,
		"count":   len(responseEvents),
	})
}
```

**Registrar rota em `internal/web/routes.go`:**
```go
healthCheckGroup.GET("/events/:sessionId", healthCheckHandler.GetEvents)
```

---

### 4. Frontend - API Client

**Arquivo a modificar:** `internal/web/frontend/src/lib/api/client.ts`

**Adicionar método:**
```typescript
async getHealthCheckEvents(sessionId: string) {
  const response = await fetch(`/api/v1/healthcheck/events/${sessionId}`, {
    headers: {
      'Authorization': `Bearer ${localStorage.getItem('auth_token') || 'poc-token-123'}`
    }
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch events: ${response.statusText}`);
  }

  return response.json();
}
```

---

### 5. Frontend - Badges Clicáveis

**Arquivo a modificar:** `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx`

**Substituir cards de métricas por badges** (linhas 253-281):

```tsx
{/* Métricas Resumidas com Badges Clicáveis */}
<CardContent className="pt-0 pb-3">
  <div className="flex items-center gap-2 ml-6 flex-wrap">
    {/* Badge Healthy */}
    <Button
      variant="outline"
      size="sm"
      onClick={() => handleViewLogs(cluster, result)}
      className="relative h-auto py-2 px-3 border-green-500 hover:bg-green-50 dark:hover:bg-green-950/20"
    >
      <div className="flex items-center gap-2">
        <CheckCircle2 className="h-4 w-4 text-green-600" />
        <span className="text-sm font-medium text-green-700 dark:text-green-500">
          Healthy
        </span>
        {/* Círculo com contador */}
        {result.healthy_count > 0 && (
          <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-green-600 text-white text-xs flex items-center justify-center font-bold">
            {result.healthy_count}
          </div>
        )}
      </div>
    </Button>

    {/* Badge Warning */}
    <Button
      variant="outline"
      size="sm"
      onClick={() => handleViewLogs(cluster, result)}
      className="relative h-auto py-2 px-3 border-yellow-500 hover:bg-yellow-50 dark:hover:bg-yellow-950/20"
    >
      <div className="flex items-center gap-2">
        <AlertCircle className="h-4 w-4 text-yellow-600" />
        <span className="text-sm font-medium text-yellow-700 dark:text-yellow-500">
          Warning
        </span>
        {result.warning_count > 0 && (
          <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-yellow-600 text-white text-xs flex items-center justify-center font-bold">
            {result.warning_count}
          </div>
        )}
      </div>
    </Button>

    {/* Badge Critical */}
    <Button
      variant="outline"
      size="sm"
      onClick={() => handleViewLogs(cluster, result)}
      className="relative h-auto py-2 px-3 border-red-500 hover:bg-red-50 dark:hover:bg-red-950/20"
    >
      <div className="flex items-center gap-2">
        <XCircle className="h-4 w-4 text-red-600" />
        <span className="text-sm font-medium text-red-700 dark:text-red-500">
          Critical
        </span>
        {result.critical_count > 0 && (
          <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-red-600 text-white text-xs flex items-center justify-center font-bold">
            {result.critical_count}
          </div>
        )}
      </div>
    </Button>

    {/* Badge Total */}
    <Button
      variant="outline"
      size="sm"
      onClick={() => handleViewLogs(cluster, result)}
      className="relative h-auto py-2 px-3 border-gray-400"
    >
      <div className="flex items-center gap-2">
        <Activity className="h-4 w-4" />
        <span className="text-sm font-medium">Total</span>
        {result.total_checks > 0 && (
          <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-gray-600 text-white text-xs flex items-center justify-center font-bold">
            {result.total_checks}
          </div>
        )}
      </div>
    </Button>
  </div>
</CardContent>
```

**Adicionar handler:**
```typescript
const handleViewLogs = async (cluster: string, result: HealthCheckResult) => {
  try {
    // Buscar eventos salvos do banco
    const response = await apiClient.getHealthCheckEvents(result.id);

    if (response.success && response.events) {
      // Abrir modal com eventos (implementar próximo passo)
      onShowLogsModal(cluster, response.events);
    } else {
      toast.error("Nenhum log encontrado para este cluster");
    }
  } catch (error) {
    console.error("Failed to fetch logs:", error);
    toast.error("Erro ao carregar logs");
  }
};
```

---

### 6. Frontend - Modal de Visualização de Logs

**Nova prop no HealthCheckingTab:**
```typescript
const [selectedClusterLogs, setSelectedClusterLogs] = useState<{
  cluster: string;
  sessionId: string;
  events: HealthCheckProgress[];
} | null>(null);

const handleShowLogsModal = (cluster: string, events: HealthCheckProgress[]) => {
  setSelectedClusterLogs({
    cluster,
    sessionId: clusterSessions[cluster],
    events
  });
  setShowProgressModal(true);
};
```

**Modificar HealthCheckProgressModal:**
```tsx
<HealthCheckProgressModal
  sessionId={selectedClusterLogs?.sessionId || sessionId}
  clusterSessions={clusterSessions}
  open={showProgressModal}
  preloadedEvents={selectedClusterLogs?.events} // 🆕 Eventos pré-carregados
  viewMode={!!selectedClusterLogs} // 🆕 Modo visualização (não conecta SSE)
  onOpenChange={...}
  onComplete={...}
/>
```

**Modificar ClusterTabContent para aceitar eventos pré-carregados:**
```typescript
const ClusterTabContent = ({
  cluster,
  sessionId,
  enabled,
  preloadedEvents, // 🆕
  viewMode,        // 🆕
  onComplete,
  onError,
}: ClusterTabContentProps) => {
  const [events, setEvents] = useState<HealthCheckProgress[]>(preloadedEvents || []);

  // Se viewMode = true, NÃO conectar SSE - apenas exibir eventos pré-carregados
  const {
    events: liveEvents,
    isConnected,
    ...
  } = useHealthCheckProgress({
    sessionId,
    enabled: !viewMode && enabled, // 🆕 Desabilitar SSE em viewMode
    ...
  });

  // Usar eventos ao vivo ou pré-carregados
  const displayEvents = viewMode ? events : liveEvents;

  // Resto do código usa displayEvents ao invés de events
};
```

---

## 📊 Resumo de Arquivos Modificados

| Arquivo | Status | Mudanças |
|---------|--------|----------|
| `internal/healthcheck/storage.go` | ✅ Completo | Tabela + métodos Save/Get/DeleteEvents |
| `internal/healthcheck/orchestrator.go` | ⏳ Pendente | Salvar eventos ao publicar SSE |
| `internal/web/handlers/healthcheck.go` | ⏳ Pendente | Endpoint GET /events/:sessionId |
| `internal/web/routes.go` | ⏳ Pendente | Registrar rota de eventos |
| `internal/web/frontend/src/lib/api/client.ts` | ⏳ Pendente | Método getHealthCheckEvents() |
| `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx` | ⏳ Pendente | Badges clicáveis com círculos |
| `internal/web/frontend/src/components/HealthCheckProgressModal.tsx` | ⏳ Pendente | Modo visualização com eventos pré-carregados |
| `internal/web/frontend/src/components/HealthCheckingTab.tsx` | ⏳ Pendente | Handler para abrir modal com logs |

---

## 🎉 Status Final

✅ **TODAS as tarefas foram concluídas com sucesso!**

### Implementação Completa (31/12/2025):

1. ✅ Backend - Persistência automática de eventos no SQLite (já estava pronto)
2. ✅ Backend - Endpoint `GET /api/v1/healthcheck/events/:sessionId` (já estava pronto)
3. ✅ Frontend - Método `getHealthCheckEvents()` no API client (já estava pronto)
4. ✅ Frontend - Badges clicáveis passam `cluster` e `result` para handler
5. ✅ Frontend - Handler `handleShowProgress()` busca eventos e abre modal
6. ✅ Frontend - Modal aceita `preloadedEvents` e `viewMode` (não conecta SSE)
7. ✅ Build completo: Frontend + Backend compilados sem erros

### Fluxo Funcional:

1. Durante execução do health check → eventos salvos automaticamente no SQLite
2. Ao clicar em badge (Healthy/Warning/Critical/Total) → busca eventos do banco
3. Modal abre em modo visualização com logs completos persistidos
4. Não conecta SSE em modo visualização (apenas exibe logs históricos)

---

**Tempo total:** ~1.5 horas (mais rápido que estimado - backend já estava pronto)
