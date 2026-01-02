# Progresso: SSE Multiplexado para Health Check

**Data:** 02/01/2026
**Objetivo:** Resolver travamento com 12+ clusters simultâneos

---

## ✅ Problemas Identificados

1. **Limite de conexões do navegador**: HTTP/1.1 limita ~6 conexões simultâneas por domínio
2. **Cluster em 0%**: Com 12+ clusters, conexões extras bloqueadas pelo navegador
3. **Botão cancelar não funciona**: Múltiplas conexões difíceis de gerenciar
4. **Modal não reabre após fechar**: Estado fragmentado entre múltiplos hooks

---

## ✅ Solução: SSE Multiplexado (CONCLUÍDO - Backend)

### Backend (100% Completo)

**Commit:** `5a363a3`

#### 1. Endpoint Multiplexado

**Arquivo:** `internal/web/handlers/healthcheck.go`

```go
// ProgressMultiplexed - Nova função (linhas 250-407)
// GET /api/v1/healthcheck/progress-multiplex?session={baseId}&clusters=cluster1,cluster2,cluster3

func (h *HealthCheckHandler) ProgressMultiplexed(c *gin.Context) {
    // 1. Parse clusters da query string
    // 2. Criar 1 client SSE para CADA cluster (interno)
    // 3. Usar reflect.Select() para multiplexar N channels
    // 4. Adicionar campo "cluster" em cada evento via Details
    // 5. Fechar stream quando TODOS os clusters completarem
}
```

**Técnica:** `reflect.Select()` para escutar múltiplos channels simultaneamente

**Imports adicionados:**
- `reflect` (para multiplexação)
- `strings` (para parse de clusters)

#### 2. Rota Registrada

**Arquivo:** `internal/web/server.go` (linha 711)

```go
sseGroup.GET("/progress-multiplex", healthCheckHandler.ProgressMultiplexed)
```

#### 3. Buffer SSE Aumentado

**Arquivo:** `internal/web/sse/progress.go` (linha 39)

```go
Channel: make(chan ProgressEvent, 500), // Antes: 10 eventos
```

**Razão:** 12 clusters × ~100 eventos cada = ~1200 eventos totais

---

## ✅ Frontend (Hook Criado - 100%)

### Hook Multiplexado

**Arquivo:** `internal/web/frontend/src/hooks/useHealthCheckProgressMultiplexed.ts` (320 linhas)

**Funcionalidades:**
- ✅ Uma única conexão EventSource
- ✅ Agrupa eventos por cluster (`eventsPerCluster: Record<string, HealthCheckProgress[]>`)
- ✅ Métodos utilitários por cluster:
  - `getClusterEvents(cluster)`
  - `getProgress(cluster)`
  - `getCurrentPhase(cluster)`
  - `isComplete(cluster)`
  - `hasError(cluster)`
- ✅ Detecta cluster via campo `Details` do evento (formato: `"cluster:nome"`)
- ✅ Rastreia clusters completados (`completedClusters: Set<string>`)

**API:**
```typescript
const {
  eventsPerCluster,         // Record<string, HealthCheckProgress[]>
  completedClusters,        // Set<string>
  isConnected,              // boolean
  disconnect,               // () => void
  getProgress,              // (cluster: string) => number
  isComplete,               // (cluster: string) => boolean
} = useHealthCheckProgressMultiplexed({
  sessionId: 'base-uuid',
  clusters: ['cluster1', 'cluster2', ...],
  onComplete: (cluster) => console.log(`${cluster} done`),
  enabled: true
});
```

---

## ⚠️ Pendente: Refatoração do Modal (Frontend)

### Arquivo a Modificar

**`internal/web/frontend/src/components/HealthCheckProgressModal.tsx`** (~650 linhas)

### Mudanças Necessárias

#### 1. Remover ClusterTabContent com hooks individuais

**ANTES (atual):**
```tsx
// Cada tab cria sua própria conexão SSE
<ClusterTabContent
  cluster={cluster}
  sessionId={clusterSessions[cluster]}
  enabled={open}  // ❌ Problema: 12 conexões simultâneas
/>
```

**DEPOIS (novo):**
```tsx
// Modal pai gerencia 1 conexão SSE para TODOS
const {
  eventsPerCluster,
  disconnect,
  getProgress,
  isComplete
} = useHealthCheckProgressMultiplexed({
  sessionId,
  clusters,
  enabled: open && !viewMode
});

// Tab recebe eventos via props (sem hooks próprios)
<ClusterTabContentStatic
  cluster={cluster}
  events={eventsPerCluster[cluster] || []}
  progress={getProgress(cluster)}
  isComplete={isComplete(cluster)}
/>
```

#### 2. Simplificar ClusterTabContent

**Remover:**
- Hook `useHealthCheckProgress` (individual)
- Estado `liveEvents`, `lastEvent`, `isCompleted`
- Lógica de conexão SSE

**Manter apenas:**
- Renderização de UI (progress bar, logs, badges)
- Filtros de status (healthy/warning/critical)
- Fetch de resultado final quando completo

#### 3. Botão Cancelar Simplificado

**ANTES:**
```tsx
// Desconectar 12 funções diferentes
disconnectFunctionsRef.current.forEach((disconnect, cluster) => {
  disconnect();  // ❌ Complexo
});
```

**DEPOIS:**
```tsx
// Uma única função disconnect
disconnect();  // ✅ Simples
```

#### 4. Modal Reabertura

**Problema atual:** Estado fragmentado entre múltiplos hooks

**Solução:** Hook multiplexado mantém estado centralizado

```tsx
// Modal fecha mas estado persiste no hook
onOpenChange(false);  // Fecha

// Reabre com estado preservado
onOpenChange(true);   // Eventos ainda em eventsPerCluster
```

---

## 📋 Checklist de Implementação

### Backend ✅
- [x] Endpoint `ProgressMultiplexed` criado
- [x] Rota registrada em `server.go`
- [x] Imports adicionados (reflect, strings)
- [x] Buffer SSE aumentado (10 → 500)
- [x] Log detalhado para diagnóstico
- [x] Compilação bem-sucedida

### Frontend Hook ✅
- [x] Hook `useHealthCheckProgressMultiplexed` criado
- [x] Eventos agrupados por cluster
- [x] Métodos utilitários implementados
- [x] Detecção de cluster via `Details`
- [x] Rastreamento de completions

### Frontend Modal ⏳ (PENDENTE)
- [ ] Remover hook individual de ClusterTabContent
- [ ] Usar hook multiplexado no componente pai
- [ ] Passar eventos via props (ClusterTabContentStatic)
- [ ] Simplificar botão cancelar
- [ ] Testar reabertura do modal
- [ ] Testar com 12+ clusters

### Testes 📝 (PENDENTE)
- [ ] Teste com 1 cluster (caso especial)
- [ ] Teste com 4 clusters
- [ ] Teste com 12 clusters
- [ ] Teste com 30+ clusters
- [ ] Teste botão cancelar
- [ ] Teste modal reabrir
- [ ] Verificar logs SSE no backend

---

## 🎯 Próximos Passos

1. **Refatorar HealthCheckProgressModal.tsx**
   - Substituir hook individual por multiplexado
   - Simplificar lógica de cancelamento
   - Testar reabertura

2. **Validação com cenário real**
   - Executar health check em 12 clusters de produção
   - Monitorar logs backend (canal cheio?)
   - Verificar se todos os clusters progridem

3. **Ajustes finais se necessário**
   - Aumentar buffer se ainda houver perda de eventos
   - Otimizar performance do reflect.Select se lento
   - Adicionar retry logic se conexão cair

4. **Documentação**
   - Atualizar CLAUDE.md com nova arquitetura
   - Documentar diferença entre Progress (antigo) e ProgressMultiplexed (novo)

---

## 📊 Performance Esperada

### Antes (Conexões Individuais)
- **12 clusters** = 12 conexões EventSource
- **Limite navegador:** ~6 conexões
- **Resultado:** 6 clusters OK, 6+ travados em 0%

### Depois (Multiplexado)
- **12 clusters** = 1 conexão EventSource
- **Limite navegador:** Não é problema
- **Resultado:** Todos os 12 clusters progridem normalmente

### Escalabilidade
- **30 clusters:** 1 conexão (OK)
- **50 clusters:** 1 conexão (OK, mas aumentar buffer para 1000+)
- **100+ clusters:** Considerar paginação ou análise em batches

---

## 🐛 Troubleshooting

### Problema: Eventos ainda sendo perdidos

**Diagnóstico:**
```bash
# Verificar logs backend para warnings
grep "Canal cheio" /tmp/k8s-hpa-manager-web-*.log
```

**Solução:** Aumentar buffer
```go
Channel: make(chan ProgressEvent, 1000), // ou mais
```

### Problema: reflect.Select muito lento

**Diagnóstico:**
- Latência alta (>100ms por evento)
- CPU usage alto no backend

**Solução alternativa:** Worker pool ao invés de reflect.Select

### Problema: Cluster não detectado no frontend

**Diagnóstico:**
```typescript
// Verificar se campo Details está presente
console.log('[Frontend] Event Details:', event.Details);
```

**Solução:** Corrigir extração de cluster no backend
```go
event.Details = fmt.Sprintf("cluster:%s", cluster)
```

---

## 📝 Notas Técnicas

### Por que reflect.Select?

Go não tem sintaxe nativa para `select` dinâmico (N cases).

**Alternativa rejeitada:** Goroutine por cluster
```go
// ❌ Complexo, mais overhead
for _, client := range clients {
  go func(c *sse.Client) {
    for event := range c.Channel {
      // Como enviar para stream?
    }
  }(client)
}
```

**Solução escolhida:** reflect.Select
```go
// ✅ Simples, performático
cases := []reflect.SelectCase{...}
chosen, value, ok := reflect.Select(cases)
```

### Formato do Evento

**Backend adiciona cluster via Details:**
```go
event.Details = fmt.Sprintf("cluster:%s", cluster)
```

**Frontend extrai:**
```typescript
if (event.Details && event.Details.startsWith('cluster:')) {
  cluster = event.Details.replace('cluster:', '');
}
```

**Fallback:** Extrair do sessionID (`baseSessionID-clusterName`)

---

**Status Final Backend:** ✅ 100% Completo
**Status Final Frontend:** ⏳ 50% (Hook pronto, Modal pendente)
**Próxima ação:** Refatorar `HealthCheckProgressModal.tsx`
