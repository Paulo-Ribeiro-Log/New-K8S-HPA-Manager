# Plano de Implementação: Dashboard de Produção para AI Diagnostics

**Data**: 23 de dezembro de 2025
**Objetivo**: Transformar a aba AI Diagnostics de modo debug para interface de produção completa
**Estimativa**: ~4-6 horas de desenvolvimento
**Localização do Plano**:
- Este arquivo: `/home/paulo/.claude/plans/validated-finding-bunny.md`
- Cópia para raiz do projeto: `PLANO_AI_DIAGNOSTICS_UI.md` (copiar após sair do plan mode)

---

## 📋 Contexto

### Situação Atual
A aba **AI Diagnostics** (`AIDiagnosticsTab.tsx`) está funcional mas em **modo debug**:
- ✅ Hook `useAIDiagnostics` completo e funcional
- ✅ Componentes `AIHistoryPanel`, `AIAnalysisCard`, `AISettingsTab` prontos
- ✅ API backend 100% operacional
- ⚠️ **UI em modo debug** com botões de teste e sem visualizações de produção

### O que falta
1. **Dashboard de Estatísticas** - Cards de métricas e gráficos
2. **Status do Provider** - Visual indicator do provider ativo
3. **Layout de produção** - Remover debug mode, organizar componentes
4. **Quick Start Guide** - Instruções para novos usuários

---

## 🎯 Objetivos

### 1. Criar Dashboard de Estatísticas
**Componente**: `AIStatsCard.tsx` (NOVO)

**Funcionalidades**:
- Grid 2x2 de métricas principais:
  - Total de análises realizadas
  - Tempo médio de resposta
  - Total de tokens consumidos
  - Último uso (timestamp relativo)
- Gráfico de barras: Análises por tipo de recurso (Pod, Deployment, HPA, Node)
- Gráfico de pizza: Distribuição por provider (Ollama, Gemini, Claude, OpenAI, Copilot)
- Auto-refresh a cada 60 segundos

**Padrão de UI**:
```tsx
<Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60">
  <CardHeader>
    <div className="flex items-center gap-3">
      <div className="p-3 bg-gradient-to-r from-purple-500 to-indigo-600 rounded-xl shadow-lg">
        <BarChart3 className="w-6 h-6 text-white" />
      </div>
      <div>
        <CardTitle>Estatísticas de Uso</CardTitle>
        <CardDescription>Análises AI realizadas</CardDescription>
      </div>
    </div>
  </CardHeader>
  <CardContent>
    {/* Grid 2x2 de métricas */}
    <div className="grid grid-cols-2 gap-4 mb-6">
      <MetricBox label="Total Análises" value={stats.totalAnalyses} icon={Brain} />
      <MetricBox label="Tempo Médio" value={`${stats.avgResponseTime}s`} icon={Clock} />
      <MetricBox label="Tokens Usados" value={stats.totalTokensUsed} icon={Zap} />
      <MetricBox label="Último Uso" value={relativeTime} icon={History} />
    </div>

    {/* Gráficos lado a lado */}
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div>
        <h4 className="text-sm font-semibold mb-3">Análises por Recurso</h4>
        <ResponsiveContainer width="100%" height={200}>
          <BarChart data={stats.analysesByResource}>
            <XAxis dataKey="type" />
            <YAxis />
            <Tooltip />
            <Bar dataKey="count" fill="#3b82f6" />
          </BarChart>
        </ResponsiveContainer>
      </div>

      <div>
        <h4 className="text-sm font-semibold mb-3">Distribuição por Provider</h4>
        <ResponsiveContainer width="100%" height={200}>
          <PieChart>
            <Pie data={stats.analysesByProvider} dataKey="count" nameKey="provider">
              {data.map((_, index) => (
                <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
              ))}
            </Pie>
            <Tooltip />
            <Legend />
          </PieChart>
        </ResponsiveContainer>
      </div>
    </div>
  </CardContent>
</Card>
```

---

### 2. Criar Card de Status do Provider
**Componente**: `AIProviderStatusCard.tsx` (NOVO)

**Funcionalidades**:
- Badge grande com ícone gradiente indicando status (online/offline)
- Provider ativo e modelo em uso
- Timestamp da última verificação
- Botão de refresh manual
- Badge de cor dinâmica (verde=online, vermelho=offline)

**Layout**:
```tsx
<Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60">
  <CardContent className="p-6">
    <div className="flex items-center gap-4">
      {/* Ícone com gradiente */}
      <div className={`p-4 rounded-xl shadow-lg ${
        providerStatus.available
          ? 'bg-gradient-to-r from-green-500 to-emerald-600'
          : 'bg-gradient-to-r from-red-500 to-rose-600'
      }`}>
        <Brain className="w-8 h-8 text-white" />
      </div>

      {/* Info */}
      <div className="flex-1">
        <div className="flex items-center gap-2 mb-1">
          <h3 className="text-lg font-semibold">AI Provider Status</h3>
          <Badge variant={providerStatus.available ? "default" : "destructive"}>
            {providerStatus.available ? "Online" : "Offline"}
          </Badge>
        </div>
        <p className="text-sm text-muted-foreground">
          {providerStatus.provider} - {providerStatus.model}
        </p>
        <p className="text-xs text-muted-foreground mt-1">
          Última verificação: {relativeTime(providerStatus.lastCheck)}
        </p>
      </div>

      {/* Botão refresh */}
      <Button
        variant="outline"
        size="icon"
        onClick={fetchProviderStatus}
        disabled={isLoadingStatus}
      >
        <RefreshCw className={`h-4 w-4 ${isLoadingStatus ? 'animate-spin' : ''}`} />
      </Button>
    </div>
  </CardContent>
</Card>
```

---

### 3. Refatorar AIDiagnosticsTab para Layout de Produção

**Arquivo**: `AIDiagnosticsTab.tsx` (MODIFICAR)

**Estrutura Final**:
```tsx
<div className="space-y-6 p-6">
  <Tabs defaultValue="diagnostics" className="w-full">
    <TabsList>
      <TabsTrigger value="diagnostics">
        <Brain className="h-4 w-4 mr-2" />
        Diagnósticos
      </TabsTrigger>
      <TabsTrigger value="settings">
        <Settings className="h-4 w-4 mr-2" />
        Configurações
      </TabsTrigger>
    </TabsList>

    <TabsContent value="diagnostics" className="space-y-6">
      {/* Análise atual (se existir) */}
      {currentAnalysis && (
        <AIAnalysisCard
          analysis={currentAnalysis}
          onClose={clearCurrentAnalysis}
        />
      )}

      {/* Grid 2 colunas: Stats + Provider Status */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <AIStatsCard stats={stats} isLoading={isLoadingStats} onRefresh={fetchStats} />
        <AIProviderStatusCard
          providerStatus={providerStatus}
          isLoading={isLoadingStatus}
          onRefresh={fetchProviderStatus}
        />
      </div>

      {/* Quick Start Guide (se não houver histórico) */}
      {(!history || history.length === 0) && (
        <AIQuickStartGuide />
      )}

      {/* Histórico completo */}
      <AIHistoryPanel
        history={history || []}
        isLoading={isLoadingHistory}
        onRefresh={fetchHistory}
        onViewAnalysis={(analysis) => {
          // Opção 1: Scroll até o topo e exibir em AIAnalysisCard
          window.scrollTo({ top: 0, behavior: 'smooth' });
          // currentAnalysis já é gerenciado pelo hook
        }}
        onDeleteAnalysis={deleteAnalysis}
      />
    </TabsContent>

    <TabsContent value="settings">
      <AISettingsTab />
    </TabsContent>
  </Tabs>
</div>
```

**Mudanças principais**:
1. ❌ Remover todos os botões de debug ("Testar Provider Status", etc)
2. ❌ Remover panel de debug com estado JSON
3. ❌ Remover mock data
4. ✅ Adicionar `AIStatsCard` e `AIProviderStatusCard`
5. ✅ Adicionar `AIQuickStartGuide` (condicional)
6. ✅ Manter `AIHistoryPanel` sempre visível na parte inferior

---

### 4. Criar Quick Start Guide
**Componente**: `AIQuickStartGuide.tsx` (NOVO)

**Funcionalidades**:
- Card informativo com instruções de uso
- Aparece apenas quando histórico está vazio (primeira vez)
- Links para configurar providers (se nenhum configurado)
- Lista de recursos suportados (Pod, Deployment, HPA, Node)

**Layout**:
```tsx
<Card className="bg-gradient-to-br from-blue-50 to-indigo-50 dark:from-slate-800 dark:to-slate-900 border-blue-200 dark:border-blue-800">
  <CardContent className="p-6">
    <div className="flex items-start gap-4">
      <div className="p-3 bg-blue-500 rounded-xl">
        <Lightbulb className="w-6 h-6 text-white" />
      </div>
      <div className="flex-1">
        <h3 className="text-lg font-semibold mb-2">Como usar AI Diagnostics</h3>
        <div className="space-y-3 text-sm">
          <div className="flex items-start gap-2">
            <CheckCircle2 className="w-4 h-4 text-green-500 mt-0.5 flex-shrink-0" />
            <p>
              Vá para a aba <strong>Pods</strong>, selecione um pod e clique em
              <Badge variant="outline" className="mx-1">Analisar com AI</Badge>
            </p>
          </div>
          <div className="flex items-start gap-2">
            <CheckCircle2 className="w-4 h-4 text-green-500 mt-0.5 flex-shrink-0" />
            <p>
              Recursos suportados: <Badge variant="secondary">Pod</Badge>{" "}
              <Badge variant="secondary">Deployment</Badge>{" "}
              <Badge variant="secondary">HPA</Badge>{" "}
              <Badge variant="secondary">Node</Badge>
            </p>
          </div>
          <div className="flex items-start gap-2">
            <Settings className="w-4 h-4 text-blue-500 mt-0.5 flex-shrink-0" />
            <p>
              Configure suas API keys na aba <strong>Configurações</strong> para usar
              providers externos (Gemini, OpenAI, Claude, Copilot)
            </p>
          </div>
        </div>
      </div>
    </div>
  </CardContent>
</Card>
```

---

## 🏗️ Estrutura de Arquivos

### Novos Arquivos
```
internal/web/frontend/src/components/
├── AIStatsCard.tsx                    (NOVO - 180 linhas)
├── AIProviderStatusCard.tsx           (NOVO - 80 linhas)
├── AIQuickStartGuide.tsx              (NOVO - 50 linhas)
```

### Arquivos Modificados
```
internal/web/frontend/src/components/
├── AIDiagnosticsTab.tsx               (MODIFICAR - reduzir de 219 para ~120 linhas)
```

### Arquivos Inalterados (Reutilizados)
```
internal/web/frontend/src/components/
├── AIHistoryPanel.tsx                 ✅ Pronto
├── AIAnalysisCard.tsx                 ✅ Pronto
├── AISettingsTab.tsx                  ✅ Pronto
```

### Hooks
```
internal/web/frontend/src/hooks/
├── useAIDiagnostics.ts                ✅ Pronto (sem mudanças)
```

---

## 📊 Backend - Endpoint de Stats

### Verificar Existência
**Endpoint esperado**: `GET /api/v1/ai/stats`

**Response esperado**:
```json
{
  "total_analyses": 142,
  "avg_response_time": 2.3,
  "total_tokens_used": 15420,
  "analyses_by_resource": {
    "Pod": 85,
    "Deployment": 32,
    "HPA": 15,
    "Node": 10
  },
  "analyses_by_provider": {
    "ollama": 98,
    "gemini": 30,
    "claude": 10,
    "openai": 4
  },
  "last_analysis_at": "2025-12-23T18:45:30Z"
}
```

### Ação Necessária
- ✅ Se endpoint **já existe**, usar diretamente
- ⚠️ Se endpoint **não existe**, criar backend handler em Go:
  - Arquivo: `internal/web/handlers/ai_diagnostics.go`
  - Função: `GetStats(c *gin.Context)`
  - Query no SQLite: `SELECT COUNT(*), AVG(response_time), SUM(tokens_used) FROM ai_diagnostics_history`

---

## 🎨 Componentes UI Reutilizados

### Do Dashboard Principal
- `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent` - Estrutura de cards
- `Badge` - Status indicators
- `Button` - Ações e refresh
- Grid layout: `grid grid-cols-1 lg:grid-cols-2 gap-6`

### De Recharts
- `BarChart`, `Bar` - Gráfico de análises por recurso
- `PieChart`, `Pie`, `Cell` - Gráfico de distribuição por provider
- `ResponsiveContainer` - Container responsivo
- `XAxis`, `YAxis`, `Tooltip`, `Legend` - Componentes auxiliares

### Ícones (lucide-react)
- `Brain` - AI provider
- `BarChart3` - Estatísticas
- `Clock` - Tempo médio
- `Zap` - Tokens
- `History` - Último uso
- `Lightbulb` - Quick start
- `CheckCircle2` - Checklist
- `Settings` - Configurações
- `RefreshCw` - Refresh

---

## 🔧 Implementação Passo a Passo

### Fase 1: Novos Componentes (2-3h)

#### 1.1 AIStatsCard.tsx
```bash
# Criar arquivo
touch internal/web/frontend/src/components/AIStatsCard.tsx
```

**Checklist**:
- [ ] Importar hooks: `useState`, `useEffect`
- [ ] Importar UI: `Card`, `CardHeader`, `CardContent`, `Badge`
- [ ] Importar Recharts: `BarChart`, `PieChart`
- [ ] Criar interface `AIStatsCardProps`
- [ ] Implementar grid 2x2 de métricas
- [ ] Implementar gráfico de barras (análises por recurso)
- [ ] Implementar gráfico de pizza (distribuição por provider)
- [ ] Adicionar loading state (skeleton)
- [ ] Adicionar empty state
- [ ] Adicionar botão de refresh

#### 1.2 AIProviderStatusCard.tsx
```bash
# Criar arquivo
touch internal/web/frontend/src/components/AIProviderStatusCard.tsx
```

**Checklist**:
- [ ] Importar UI: `Card`, `CardContent`, `Badge`, `Button`
- [ ] Criar interface `AIProviderStatusCardProps`
- [ ] Implementar ícone com gradiente dinâmico (verde/vermelho)
- [ ] Implementar badge de status (Online/Offline)
- [ ] Implementar formatação de timestamp relativo
- [ ] Adicionar botão de refresh
- [ ] Adicionar loading state

#### 1.3 AIQuickStartGuide.tsx
```bash
# Criar arquivo
touch internal/web/frontend/src/components/AIQuickStartGuide.tsx
```

**Checklist**:
- [ ] Importar UI: `Card`, `CardContent`, `Badge`
- [ ] Implementar card com background gradiente azul
- [ ] Adicionar ícone de "Lightbulb"
- [ ] Criar lista de instruções com checkmarks
- [ ] Adicionar badges de recursos suportados
- [ ] Adicionar link para aba Configurações

---

### Fase 2: Refatoração do AIDiagnosticsTab (1-2h)

#### 2.1 Limpar código debug
**Checklist**:
- [ ] Remover state `showDebug`
- [ ] Remover state `showHistoryPanel` (sempre visível)
- [ ] Remover state `showAnalysisCard` (usar `currentAnalysis` do hook)
- [ ] Remover `mockAnalysis`
- [ ] Remover todos os botões de teste ("Testar Provider Status", etc)
- [ ] Remover `<div>` de debug info

#### 2.2 Adicionar novos componentes
**Checklist**:
- [ ] Importar `AIStatsCard`, `AIProviderStatusCard`, `AIQuickStartGuide`
- [ ] Adicionar grid 2 colunas com Stats + Provider Status
- [ ] Adicionar `AIQuickStartGuide` condicional (quando `history.length === 0`)
- [ ] Manter `AIHistoryPanel` sempre visível
- [ ] Adicionar `clearCurrentAnalysis()` no botão de fechar do `AIAnalysisCard`

#### 2.3 Testar integração
**Checklist**:
- [ ] Verificar que `useAIDiagnostics()` fornece todos os dados
- [ ] Testar auto-refresh de stats (60s)
- [ ] Testar refresh manual de provider status
- [ ] Testar que Quick Start desaparece após primeira análise
- [ ] Testar navegação entre tabs (Diagnósticos ↔ Configurações)

---

### Fase 3: Backend (se necessário) (1h)

#### 3.1 Verificar endpoint existente
```bash
# Checar se existe
grep -r "GetStats\|getAIStats" internal/web/handlers/
```

#### 3.2 Criar handler (se não existir)
**Arquivo**: `internal/web/handlers/ai_diagnostics.go`

**Função**:
```go
func (h *AIDiagnosticsHandler) GetStats(c *gin.Context) {
    ctx := context.Background()

    // Query agregada no SQLite
    query := `
        SELECT
            COUNT(*) as total_analyses,
            AVG(response_time) as avg_response_time,
            SUM(tokens_used) as total_tokens_used,
            MAX(analyzed_at) as last_analysis_at
        FROM ai_diagnostics_history
    `

    var stats AIStats
    row := h.db.QueryRowContext(ctx, query)
    err := row.Scan(&stats.TotalAnalyses, &stats.AvgResponseTime,
                     &stats.TotalTokensUsed, &stats.LastAnalysisAt)

    // Análises por recurso
    resourceQuery := `
        SELECT resource_type, COUNT(*) as count
        FROM ai_diagnostics_history
        GROUP BY resource_type
    `
    // ... implementação

    // Análises por provider
    providerQuery := `
        SELECT provider, COUNT(*) as count
        FROM ai_diagnostics_history
        GROUP BY provider
    `
    // ... implementação

    c.JSON(http.StatusOK, stats)
}
```

#### 3.3 Registrar rota
**Arquivo**: `internal/web/routes.go`

```go
aiGroup := v1.Group("/ai")
{
    aiGroup.GET("/stats", aiHandler.GetStats)
    // ... outras rotas existentes
}
```

---

### Fase 4: Frontend API Client (30min)

#### 4.1 Adicionar método em client.ts
**Arquivo**: `internal/web/frontend/src/lib/api/client.ts`

```typescript
async getAIStats(): Promise<AIStats> {
  return this.request(`/ai/stats`, {
    method: "GET",
  });
}
```

#### 4.2 Atualizar hook useAIDiagnostics
**Arquivo**: `internal/web/frontend/src/hooks/useAIDiagnostics.ts`

```typescript
// Adicionar chamada automática no useEffect inicial
useEffect(() => {
  fetchProviderStatus();
  fetchStats();        // ✅ Adicionar
  fetchHistory();
}, []);

// Auto-refresh de stats a cada 60s
useEffect(() => {
  const interval = setInterval(() => {
    fetchStats();
  }, 60000);
  return () => clearInterval(interval);
}, [fetchStats]);
```

---

### Fase 5: Testes e Ajustes Finais (30min)

#### 5.1 Testes de UI
**Checklist**:
- [ ] Abrir `/ai-diagnostics` no navegador
- [ ] Verificar que cards de estatísticas aparecem corretamente
- [ ] Verificar que provider status exibe corretamente (online/offline)
- [ ] Verificar que Quick Start aparece quando histórico vazio
- [ ] Fazer uma análise em Pods e verificar que Quick Start desaparece
- [ ] Verificar que histórico atualiza após análise
- [ ] Verificar auto-refresh de stats (esperar 60s)
- [ ] Verificar refresh manual de provider status
- [ ] Verificar responsividade (mobile/tablet/desktop)

#### 5.2 Testes de Performance
**Checklist**:
- [ ] Verificar que não há memory leaks (DevTools → Memory)
- [ ] Verificar que gráficos renderizam rápido (< 500ms)
- [ ] Verificar que auto-refresh não trava UI

#### 5.3 Ajustes de Estilo
**Checklist**:
- [ ] Cores consistentes com tema dark/light
- [ ] Espaçamentos seguem padrão (gap-4, gap-6)
- [ ] Tipografia consistente (text-sm, text-base, text-lg)
- [ ] Ícones com tamanho correto (h-4 w-4, h-6 w-6)

---

## 📝 Checklist de Entrega

### Frontend
- [ ] `AIStatsCard.tsx` criado e funcional
- [ ] `AIProviderStatusCard.tsx` criado e funcional
- [ ] `AIQuickStartGuide.tsx` criado e funcional
- [ ] `AIDiagnosticsTab.tsx` refatorado (modo produção)
- [ ] Botões de debug removidos
- [ ] Mock data removido
- [ ] Auto-refresh funcionando
- [ ] Loading states implementados
- [ ] Empty states implementados
- [ ] Responsividade OK (mobile/tablet/desktop)

### Backend (se necessário)
- [ ] Endpoint `/api/v1/ai/stats` criado
- [ ] Queries no SQLite otimizadas
- [ ] Response formatado corretamente
- [ ] Rota registrada em `routes.go`

### Testes
- [ ] UI testada manualmente
- [ ] Performance OK (sem lags)
- [ ] Sem erros no console
- [ ] Sem memory leaks

### Documentação
- [ ] CLAUDE.md atualizado (seção AI Diagnostics)
- [ ] Comentários no código explicando lógica complexa

---

## 🎯 Resultado Esperado

### Antes (Modo Debug)
```
┌─────────────────────────────────────────────────────────┐
│ AI-Powered Diagnostics (Debug Mode)                    │
│                                                         │
│ [Testar Provider Status] [Testar History] [Testar Stats]│
│                                                         │
│ Debug Info: { provider: "ollama", available: true }    │
└─────────────────────────────────────────────────────────┘
```

### Depois (Modo Produção)
```
┌──────────────────────────┬──────────────────────────┐
│ 📊 Estatísticas de Uso   │ 🤖 AI Provider Status    │
│                          │                          │
│ ┌──────┬──────┐          │ ✅ Online                │
│ │ 142  │ 2.3s │          │ Ollama - llama3.2:3b     │
│ │Total │Tempo │          │                          │
│ └──────┴──────┘          │ [🔄 Refresh]             │
│ ┌──────┬──────┐          │                          │
│ │15.4K │5m ago│          │                          │
│ │Tokens│Uso   │          │                          │
│ └──────┴──────┘          │                          │
│                          │                          │
│ [Gráfico Barras]         │                          │
│ [Gráfico Pizza]          │                          │
└──────────────────────────┴──────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ 💡 Como usar AI Diagnostics                            │
│                                                         │
│ ✅ Vá para Pods e clique em "Analisar com AI"          │
│ ✅ Recursos: Pod | Deployment | HPA | Node             │
│ ⚙️ Configure providers na aba Configurações            │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ 📜 Histórico de Análises                               │
│                                                         │
│ [Filtros: Search | Tipo | Provider]                    │
│                                                         │
│ [Cards de análises anteriores...]                      │
└─────────────────────────────────────────────────────────┘
```

---

## 📚 Referências de Código

### Padrões Reutilizados de:
- **DashboardCharts.tsx** - Grid layout, cards com gradiente
- **MetricsGauge.tsx** - Métricas visuais
- **TopNamespacesCard.tsx** - Gráficos Recharts, Progress bars
- **AIHistoryPanel.tsx** - Filtros, ScrollArea
- **AISettingsTab.tsx** - Tabs, Cards de configuração

### Arquivos Críticos:
```
internal/web/frontend/src/components/
├── DashboardCharts.tsx          (referência para layout)
├── MetricsGauge.tsx             (referência para cards de métricas)
├── TopNamespacesCard.tsx        (referência para gráficos)
├── AIHistoryPanel.tsx           (reutilizar como está)
├── AISettingsTab.tsx            (reutilizar como está)
```

---

## ⏱️ Cronograma Estimado

| Fase | Tarefa | Tempo |
|------|--------|-------|
| 1 | Criar AIStatsCard.tsx | 1.5h |
| 1 | Criar AIProviderStatusCard.tsx | 0.5h |
| 1 | Criar AIQuickStartGuide.tsx | 0.5h |
| 2 | Refatorar AIDiagnosticsTab.tsx | 1h |
| 3 | Backend (se necessário) | 1h |
| 4 | Frontend API Client | 0.5h |
| 5 | Testes e ajustes | 0.5h |
| **Total** | | **5-6h** |

---

## 🚀 Priorização

### Prioridade Alta (Essencial)
1. ✅ Remover modo debug de `AIDiagnosticsTab`
2. ✅ Adicionar `AIProviderStatusCard` (feedback visual importante)
3. ✅ Manter `AIHistoryPanel` sempre visível

### Prioridade Média (Importante)
4. ✅ Adicionar `AIStatsCard` (métricas de uso)
5. ✅ Adicionar `AIQuickStartGuide` (onboarding)

### Prioridade Baixa (Nice to Have)
6. ⚠️ Gráficos em AIStatsCard (pode ser adicionado depois)
7. ⚠️ Timeline de análises (feature futura)

---

## 📋 Notas Finais

### Decisões Técnicas
1. **Recharts** para todos os gráficos (já usado no projeto)
2. **Auto-refresh de 60s** para stats (evitar sobrecarga)
3. **Componentes separados** (modular, reutilizável)
4. **Grid responsivo** (mobile-first)
5. **Loading/Empty states** (UX completa)

### Não Implementar Agora
- ❌ Filtro de data em histórico (usar filtros existentes)
- ❌ Paginação de histórico (implementar só se necessário)
- ❌ Export de análises (feature futura)
- ❌ Comparação de análises (feature futura)

### Próximos Passos Após Entrega
1. Adicionar `AITriggerButton` em Deployments, HPAs, Nodes
2. Implementar modal em PodsPanel (feedback visual imediato)
3. Adicionar filtro de data em `AIHistoryPanel`
4. Implementar paginação se histórico crescer muito

---

**Fim do Plano de Implementação**
