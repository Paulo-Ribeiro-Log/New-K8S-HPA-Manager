# Plano de Correção - Aba Helm

**Data**: 08/01/2026
**Status**: 🔴 EM EXECUÇÃO
**Prioridade**: CRÍTICA

---

## 📋 Sumário Executivo

A aba Helm está 80% completa, mas possui **4 problemas críticos** que impedem uso em produção:

1. ❌ Busca não é dinâmica (padrão diferente de outras abas)
2. ❌ Botão Apply não funciona (feature principal incompleta)
3. ❌ Sem dry-run/preview (risco de mudanças destrutivas)
4. ❌ window.location.reload() ao invés de cache invalidation

---

## 🎯 Objetivos

- [x] **Fase 1**: Busca dinâmica (1h)
- [ ] **Fase 2**: Botão Apply funcional (2h)
- [ ] **Fase 3**: Dry-run e Diff Preview (2h)
- [ ] **Fase 4**: React Query invalidation (1h)
- [ ] **Fase 5**: Barra de progresso SSE (1h)

**Tempo Total Estimado**: 7 horas

---

## 🔧 Fase 1: Busca Dinâmica (PRIORIDADE MÁXIMA)

### Problema Atual
```typescript
// HelmTab.tsx - PADRÃO INCORRETO
const [searchInput, setSearchInput] = useState(filters.search);

// Busca só acontece ao clicar no botão Filter ou pressionar Enter
const handleSearchSubmit = () => {
  setFilters({ search: searchInput });
};
```

### Padrão Correto (DeploymentsTab)
```typescript
// DeploymentsTab.tsx - PADRÃO CORRETO
const [searchQuery, setSearchQuery] = usePersistedTabState('deployments', 'searchQuery', "");

// Busca acontece dinamicamente no onChange
<Input
  value={searchQuery}
  onChange={(e) => setSearchQuery(e.target.value)}  // ✅ Direto, sem Submit
/>

// Filtragem no useMemo
const filtered = useMemo(() => {
  return deployments.filter(d =>
    d.name.toLowerCase().includes(searchQuery.toLowerCase())
  );
}, [deployments, searchQuery]);
```

### Ações
1. ✅ Remover `searchInput` state e botão Submit
2. ✅ Usar `searchQuery` direto no filtro (client-side)
3. ✅ Aplicar filtro em `useMemo` na lista de releases
4. ✅ Debounce opcional (300ms) para otimização

### Arquivos Modificados
- `internal/web/frontend/src/components/HelmTab.tsx`
- `internal/web/frontend/src/components/HelmReleaseList.tsx`

---

## 🔧 Fase 2: Botão Apply Funcional

### Problema Atual
```typescript
// HelmReleaseDetails.tsx - NÃO FUNCIONA
const handleApply = () => {
  if (validateYaml()) {
    setShowApplyModal(true);  // ❌ Apenas modal, sem ação real
  }
};
```

### Solução
```typescript
// ApplyValuesModal.tsx - IMPLEMENTAR
const handleConfirmApply = async () => {
  setIsApplying(true);
  try {
    const response = await fetch(`/api/v1/helm/releases/${releaseName}?cluster=${cluster}`, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify({
        namespace,
        releaseName,
        action: 'upgrade',
        chartRef: chart,
        valuesYaml: newValues,
        dryRun: false,
      }),
    });

    const data = await response.json();
    if (!data.success) throw new Error(data.error.message);

    // Conectar SSE streaming
    const operationId = data.data.operationId;
    streamOperation(operationId, (event) => {
      setLogs(prev => [...prev, event.message]);
    });
  } catch (error) {
    toast.error("Erro ao aplicar valores", { description: error.message });
  }
};
```

### Ações
1. ✅ Implementar `handleConfirmApply` em `ApplyValuesModal.tsx`
2. ✅ Conectar SSE streaming (reutilizar código de `HelmUpgradeModal`)
3. ✅ Adicionar barra de progresso visual
4. ✅ Toast de sucesso/erro
5. ✅ Invalidar cache React Query após sucesso

### Arquivos Modificados
- `internal/web/frontend/src/components/ApplyValuesModal.tsx` (criar se não existir)
- `internal/web/frontend/src/components/HelmReleaseDetails.tsx`

---

## 🔧 Fase 3: Dry-run e Diff Preview

### Implementação Backend (Já Existe)
```go
// internal/web/handlers/helm.go
// Endpoint já suporta dryRun via payload.DryRun
```

### Implementação Frontend
```typescript
// 1. Executar dry-run ANTES de mostrar modal de confirmação
const handleApply = async () => {
  if (!validateYaml()) return;

  setIsDryRunning(true);
  try {
    const dryRunResponse = await fetch(`/api/v1/helm/releases/${releaseName}?cluster=${cluster}`, {
      method: 'PUT',
      body: JSON.stringify({ ...payload, dryRun: true }),
    });

    const preview = await dryRunResponse.json();
    setDiffPreview(preview.data);  // Manifests que serão aplicados
    setShowApplyModal(true);
  } catch (error) {
    toast.error("Erro ao validar mudanças", { description: error.message });
  } finally {
    setIsDryRunning(false);
  }
};

// 2. Mostrar diff no modal
<Dialog>
  <DialogTitle>Confirmar Aplicação de Valores</DialogTitle>
  <DialogContent>
    <Tabs>
      <TabsList>
        <TabsTrigger value="diff">Diff (YAML)</TabsTrigger>
        <TabsTrigger value="preview">Recursos Afetados</TabsTrigger>
      </TabsList>
      <TabsContent value="diff">
        <MonacoYamlEditor mode="diff" originalValue={original} value={newValues} />
      </TabsContent>
      <TabsContent value="preview">
        {diffPreview.resources.map(r => <ResourceCard key={r.name} {...r} />)}
      </TabsContent>
    </Tabs>
  </DialogContent>
  <DialogFooter>
    <Button variant="outline" onClick={() => setShowApplyModal(false)}>Cancelar</Button>
    <Button onClick={handleConfirmApply}>Aplicar Mudanças</Button>
  </DialogFooter>
</Dialog>
```

### Ações
1. ✅ Adicionar endpoint dry-run call antes de modal
2. ✅ Criar componente `DiffPreviewModal`
3. ✅ Mostrar recursos afetados (Deployments, Services, etc)
4. ✅ Diff YAML lado a lado (Monaco)

### Arquivos Criados/Modificados
- `internal/web/frontend/src/components/DiffPreviewModal.tsx` (criar)
- `internal/web/frontend/src/components/HelmReleaseDetails.tsx`

---

## 🔧 Fase 4: React Query Invalidation

### Problema Atual
```typescript
// HelmUpgradeModal.tsx, HelmRollbackModal.tsx, etc
onSuccess={() => {
  window.location.reload();  // ❌ RUIM: Recarrega página inteira
}}
```

### Solução
```typescript
// 1. Criar queryClient global
import { useQueryClient } from '@tanstack/react-query';

const queryClient = useQueryClient();

// 2. Invalidar queries específicas
onSuccess={() => {
  queryClient.invalidateQueries({ queryKey: ['helm-releases', cluster] });
  queryClient.invalidateQueries({ queryKey: ['helm-release', cluster, releaseName] });
  queryClient.invalidateQueries({ queryKey: ['helm-history', cluster, releaseName] });

  toast.success("Upgrade concluído", { description: `Release ${releaseName} atualizado` });
}
```

### Ações
1. ✅ Remover todos `window.location.reload()`
2. ✅ Adicionar `useQueryClient` nos componentes
3. ✅ Invalidar queries corretas após operações
4. ✅ Testar que dados atualizam sem reload

### Arquivos Modificados
- `internal/web/frontend/src/components/HelmUpgradeModal.tsx`
- `internal/web/frontend/src/components/HelmRollbackModal.tsx`
- `internal/web/frontend/src/components/HelmActionModals.tsx`
- `internal/web/frontend/src/components/ApplyValuesModal.tsx`

---

## 🔧 Fase 5: Barra de Progresso SSE

### Implementação
```typescript
// Reutilizar padrão de Health Checking
import { Progress } from "@/components/ui/progress";

const [progress, setProgress] = useState(0);
const [currentPhase, setCurrentPhase] = useState("");

// No handler SSE
eventSource.addEventListener('helm-operation', (e) => {
  const event = JSON.parse(e.data);

  // Calcular progresso baseado na fase
  const phaseProgress = {
    'downloading': 20,
    'extracting': 40,
    'validating': 60,
    'applying': 80,
    'completed': 100,
  };

  setProgress(phaseProgress[event.phase] || 0);
  setCurrentPhase(event.phase);
  setLogs(prev => [...prev, event.message]);
});

// UI
<div className="space-y-2">
  <div className="flex items-center justify-between text-sm">
    <span className="text-muted-foreground">{currentPhase}</span>
    <span className="font-medium">{progress}%</span>
  </div>
  <Progress value={progress} max={100} />
</div>
```

### Ações
1. ✅ Adicionar state `progress` e `currentPhase`
2. ✅ Mapear fases do backend para % de progresso
3. ✅ Adicionar componente `Progress` do shadcn/ui
4. ✅ Testar com upgrade real

### Arquivos Modificados
- `internal/web/frontend/src/components/HelmUpgradeModal.tsx`
- `internal/web/frontend/src/components/ApplyValuesModal.tsx`

---

## 📊 Checklist de Qualidade

Antes de considerar concluído, validar:

- [ ] **Busca dinâmica**: Digitar filtra instantaneamente
- [ ] **Apply funcional**: Upgrade executa e streaming SSE mostra logs
- [ ] **Dry-run**: Modal mostra diff antes de aplicar
- [ ] **Cache**: Dados atualizam sem reload de página
- [ ] **Progresso**: Barra visual 0-100% durante operações
- [ ] **Erros em pt-BR**: Todas mensagens em português
- [ ] **RBAC**: Operações destrutivas protegidas (fase futura)
- [ ] **Testes manuais**: Install → Upgrade → Rollback → Uninstall

---

## 🚀 Ordem de Execução

1. **AGORA**: Fase 1 (Busca Dinâmica) - 1h
2. **SEGUIDA**: Fase 2 (Apply Funcional) - 2h
3. **DEPOIS**: Fase 3 (Dry-run) - 2h
4. **FINALMENTE**: Fase 4 (Cache) + Fase 5 (Progresso) - 2h

**Tempo Total**: ~7 horas de desenvolvimento focado

---

## 📝 Notas de Implementação

### Padrões a Seguir
- ✅ Reutilizar hooks existentes (`usePersistedTabState`, `useQueryClient`)
- ✅ Componentes shadcn/ui (Progress, Dialog, Tabs)
- ✅ Mensagens em pt-BR
- ✅ Toast para feedback (sonner)
- ✅ Monaco Editor para diffs
- ✅ SSE para streaming (EventSource)

### Dependências Necessárias
- `@tanstack/react-query` (já instalado)
- `sonner` (já instalado)
- `@monaco-editor/react` (já instalado)
- `shadcn/ui` components (já instalado)

---

**Status**: Iniciando Fase 1 agora...
