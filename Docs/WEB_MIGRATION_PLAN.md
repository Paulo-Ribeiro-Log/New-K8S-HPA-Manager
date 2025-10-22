# Plano de Migração - Interface Web Antiga → Nova Interface React

**Data:** 2025-10-17
**Status:** 🚧 Em Progresso (40% completo)
**Referência:** Análise completa em `/tmp/old-web-features.md`

---

## 📊 Situação Atual

### ✅ Já Implementado (40%)
- [x] Login com token Bearer
- [x] Listagem de clusters (24 clusters)
- [x] Carregamento de namespaces por cluster
- [x] Listagem de HPAs (todos os namespaces)
- [x] Stats cards dinâmicos (Clusters, Namespaces, HPAs, Node Pools)
- [x] Grid de Node Pools
- [x] Edição básica de HPAs (min/max replicas, CPU target)
- [x] Sistema de rotas protegidas
- [x] API Client TypeScript completo
- [x] Hooks React para dados (useClusters, useHPAs, etc)

### ❌ Faltando (60%)

#### 🔴 CRÍTICO - Funcionalidades Essenciais
- [ ] **Sistema de Staging Area** (salvar ≠ aplicar)
- [ ] **Modal de Validação Pré-Requisitos** (VPN + Azure)
- [ ] **Modal de Aplicação em Lote** com progress tracking
- [ ] **Rollouts DaemonSet/StatefulSet** (completar 3 tipos)

#### 🟠 ALTA - Funcionalidades Importantes
- [ ] **Contador de Modificações** no header `(N alterado(s))`
- [ ] **Dashboard com 4 Gráficos** (Chart.js ou Recharts)
- [ ] **Toast Notifications** com animações slideIn/slideOut
- [ ] **Highlight de Seleção** visual (border 2px + background)

#### 🟡 MÉDIA - Melhorias de UX
- [ ] **Validação de Campos** (min > max, CPU 1-100%)
- [ ] **Shutdown Endpoint** (POST /shutdown no logout)
- [ ] **Tratamento de Erros** Node Pools com instruções
- [ ] **Tab Dashboard** com gráficos

---

## 🎯 PLANO DE IMPLEMENTAÇÃO

### FASE 1 - CRÍTICA (Sessão Atual) ⚡

#### 1.1 Sistema de Staging Area
**Prioridade:** 🔴 MÁXIMA
**Impacto:** Permite editar múltiplos HPAs e revisar antes de aplicar
**Tempo estimado:** 2-3 horas

**Arquivos a criar/modificar:**
- `src/contexts/StagingContext.tsx` (NOVO) - Context API para staging area
- `src/hooks/useStaging.ts` (NOVO) - Hook para gerenciar modificações
- `src/pages/Index.tsx` (MODIFICAR) - Integrar staging context
- `src/components/HPAEditor.tsx` (MODIFICAR) - Botões Salvar/Aplicar/Cancelar

**Estrutura de Dados:**
```typescript
interface StagingArea {
  modifiedHPAs: Map<string, HPA>; // key: "namespace/name"
  originalValues: Map<string, HPA>;
  count: number;
  add: (hpa: HPA, original: HPA) => void;
  remove: (key: string) => void;
  clear: () => void;
  getChanges: (key: string) => { before: HPA; after: HPA };
}
```

**Funcionalidades:**
- Botão "💾 Salvar" → Adiciona HPA ao Map (NÃO aplica)
- Botão "✅ Aplicar Este" → PUT individual
- Botão "✅ Aplicar Todos (N)" → Abre modal de lote
- Contador atualiza automaticamente

---

#### 1.2 Contador de Modificações no Header
**Prioridade:** 🔴 CRÍTICA
**Impacto:** Feedback visual de quantas alterações estão pendentes
**Tempo estimado:** 30 minutos

**Arquivos a modificar:**
- `src/components/Header.tsx` - Adicionar prop `modifiedCount`
- `src/pages/Index.tsx` - Passar `staging.count` para Header

**UI:**
```tsx
<button className="badge">
  Alterações Pendentes ({modifiedCount})
</button>
```

---

#### 1.3 Modal de Validação Pré-Requisitos
**Prioridade:** 🔴 CRÍTICA
**Impacto:** Impede uso sem VPN/Azure configurado
**Tempo estimado:** 1-2 horas

**Arquivos a criar:**
- `src/components/ValidationModal.tsx` (NOVO)
- `src/lib/api/client.ts` (MODIFICAR) - Adicionar método `validateEnvironment()`

**Endpoint:**
```typescript
GET /api/v1/validate
Response: {
  success: boolean,
  vpnConnected: boolean,
  azureCliAvailable: boolean,
  kubectlAvailable: boolean,
  errors: string[],   // ❌ Críticos
  warnings: string[]  // 💡 Ações necessárias
}
```

**Comportamento:**
- Modal aparece ANTES do login
- Blocking: não pode ser fechado até resolver
- Botão "Tentar Novamente" para re-validar
- Spinner durante validação
- Lista de erros/warnings colorida

---

#### 1.4 Rollouts DaemonSet/StatefulSet
**Prioridade:** 🔴 CRÍTICA
**Impacto:** Completar os 3 tipos de rollout (atualmente só Deployment)
**Tempo estimado:** 1 hora

**Arquivos a modificar:**
- `src/components/HPAEditor.tsx` - Adicionar 2 checkboxes
- `src/lib/api/types.ts` - Adicionar campos `performDaemonSetRollout`, `performStatefulSetRollout`
- Backend: `internal/web/handlers/hpas.go` - Já suporta (verificar)

**UI:**
```tsx
<div className="space-y-2">
  <label>
    <input type="checkbox" checked={performRollout} />
    🔄 Deployment Rollout
  </label>
  <label>
    <input type="checkbox" checked={performDaemonSetRollout} />
    🔄 DaemonSet Rollout
  </label>
  <label>
    <input type="checkbox" checked={performStatefulSetRollout} />
    🔄 StatefulSet Rollout
  </label>
</div>
```

---

### FASE 2 - ALTA PRIORIDADE (Próxima Sessão) 🟠

#### 2.1 Modal de Aplicação em Lote
**Prioridade:** 🟠 ALTA
**Impacto:** Preview de alterações + progress tracking
**Tempo estimado:** 3-4 horas

**Arquivos a criar:**
- `src/components/BatchApplyModal.tsx` (NOVO)
- `src/components/ProgressBar.tsx` (NOVO)

**Funcionalidades:**
- Lista de HPAs modificados com preview `antes → depois`
- Progress bars individuais por HPA
- Estados: Aguardando → Aplicando (50%) → ✅ Completado / ❌ Erro
- Botão "Confirmar Aplicação"
- Resumo final: `✅ X HPAs atualizados, Y falharam`

**Estrutura:**
```typescript
interface ApplyProgress {
  hpaKey: string;
  status: 'pending' | 'applying' | 'success' | 'error';
  progress: number; // 0-100
  message?: string;
}
```

---

#### 2.2 Dashboard com Gráficos
**Prioridade:** 🟠 ALTA
**Impacto:** Visualização de métricas do cluster
**Tempo estimado:** 4-5 horas

**Biblioteca:** Recharts (melhor integração com React/TypeScript)

**Arquivos a criar:**
- `src/components/charts/CPUChart.tsx` (NOVO)
- `src/components/charts/MemoryChart.tsx` (NOVO)
- `src/components/charts/HPAsByNamespace.tsx` (NOVO)
- `src/components/charts/ReplicasDistribution.tsx` (NOVO)
- `src/components/DashboardCharts.tsx` (MODIFICAR)

**4 Gráficos:**
1. **CPU Usage Over Time** (Line Chart)
2. **Memory Usage Over Time** (Area Chart)
3. **HPAs por Namespace** (Bar Chart horizontal)
4. **Distribuição de Replicas** (Doughnut/Pie Chart)

**Dados:**
- Calculados a partir dos HPAs carregados (não simulados)
- Agregações: count por namespace, distribuição de replicas

---

#### 2.3 Toast Notifications Animadas
**Prioridade:** 🟠 ALTA
**Impacto:** Feedback visual de operações
**Tempo estimado:** 1 hora

**Biblioteca:** `sonner` (já instalada via shadcn/ui)

**Modificações:**
- Substituir `console.log` por `toast.success()` / `toast.error()`
- Animações: slideIn/slideOut (0.3s)
- Posição: top-right
- Auto-remove: 5 segundos

---

#### 2.4 Highlight de Seleção Visual
**Prioridade:** 🟠 ALTA
**Impacto:** Melhora UX de seleção
**Tempo estimado:** 30 minutos

**Arquivo a modificar:**
- `src/components/HPAListItem.tsx`

**CSS:**
```css
.selected {
  border: 2px solid #667eea;
  background: #f8f9ff;
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.15);
}

.hpa-item:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}
```

---

### FASE 3 - MÉDIA PRIORIDADE (Backlog) 🟡

#### 3.1 Validação de Campos
**Tempo estimado:** 1 hora

**Validações:**
- Min Replicas > Max Replicas → Toast de erro
- Target CPU fora de 1-100% → Toast de erro
- Node Count: Min > Max → Toast de erro

**Arquivo:** `src/components/HPAEditor.tsx`

---

#### 3.2 Shutdown Endpoint
**Tempo estimado:** 30 minutos

**Endpoint:** `POST /api/v1/shutdown`
**Ação:** Logout → Shutdown do servidor (com confirmação)

---

#### 3.3 Tratamento de Erros Node Pools
**Tempo estimado:** 1 hora

**Mensagens de erro com instruções:**
- Azure CLI não instalado → `az --version`
- Não autenticado → `az login`
- `clusters-config.json` ausente → `k8s-hpa-manager autodiscover`

---

#### 3.4 Tab Dashboard
**Tempo estimado:** 30 minutos

**Ação:**
- Tornar tab "Dashboard" funcional (atualmente apenas DashboardCharts)
- Integrar os 4 gráficos

---

## 📂 ESTRUTURA DE ARQUIVOS (Após Implementação Completa)

```
internal/web/frontend/src/
├── components/
│   ├── charts/                      # 🆕 FASE 2
│   │   ├── CPUChart.tsx
│   │   ├── MemoryChart.tsx
│   │   ├── HPAsByNamespace.tsx
│   │   └── ReplicasDistribution.tsx
│   ├── modals/                      # 🆕 FASE 1-2
│   │   ├── ValidationModal.tsx
│   │   └── BatchApplyModal.tsx
│   ├── ui/                          # shadcn/ui components
│   ├── Header.tsx                   # ✏️ Modificar FASE 1
│   ├── HPAEditor.tsx                # ✏️ Modificar FASE 1
│   ├── HPAListItem.tsx              # ✏️ Modificar FASE 2
│   ├── ProgressBar.tsx              # 🆕 FASE 2
│   └── ...
├── contexts/                        # 🆕 FASE 1
│   └── StagingContext.tsx
├── hooks/
│   ├── useAPI.ts
│   └── useStaging.ts                # 🆕 FASE 1
├── lib/
│   └── api/
│       ├── client.ts                # ✏️ Modificar FASE 1
│       └── types.ts                 # ✏️ Modificar FASE 1
├── pages/
│   ├── Index.tsx                    # ✏️ Modificar FASE 1
│   └── Login.tsx
└── App.tsx
```

---

## 🔄 FLUXO DE DADOS (Nova Arquitetura)

### 1. Inicialização
```
App.tsx → Check Token → ValidationModal (VPN + Azure)
  ↓ Validação OK
Login → API Client → Index.tsx
  ↓
StagingContext Provider → Wrap toda aplicação
```

### 2. Edição de HPA com Staging Area
```
Selecionar Cluster → loadHPAs() → state.hpas[]
  ↓
Click HPA → HPAEditor renderiza
  ↓
Editar campos → Click "💾 Salvar"
  ↓
staging.add(hpa, original) → Map atualizado
  ↓
Header mostra: "(1 alterado)"
  ↓
Opção A: "✅ Aplicar Este" → PUT individual
Opção B: "✅ Aplicar Todos (N)" → BatchApplyModal
         ↓
         Preview (antes → depois) → Confirmar
         ↓
         Loop com Progress Bars → PUT sequencial
         ↓
         Resumo → Limpar staging → Recarregar HPAs
```

### 3. Dashboard com Gráficos
```
onClusterChange() → loadHPAs()
  ↓
state.hpas[] preenchido
  ↓
useMemo(() => {
  // Calcular agregações
  const byNamespace = groupBy(hpas, 'namespace');
  const replicasDistribution = categorizeReplicas(hpas);
  const cpuData = calculateAverageCPU(hpas);
  return { byNamespace, replicasDistribution, cpuData };
}, [hpas])
  ↓
Recharts atualiza automaticamente (reactive)
```

---

## 🧪 CHECKLIST DE VALIDAÇÃO

### FASE 1 - CRÍTICA
- [ ] Staging Area funciona (salvar ≠ aplicar)
- [ ] Contador atualiza no header
- [ ] Botão "Aplicar Este" funciona (PUT individual)
- [ ] Botão "Aplicar Todos (N)" desabilitado se count = 0
- [ ] ValidationModal bloqueia acesso se VPN OFF
- [ ] ValidationModal mostra erros/warnings corretos
- [ ] 3 checkboxes de rollout (Deployment/DaemonSet/StatefulSet)
- [ ] Rollouts salvos corretamente no staging

### FASE 2 - ALTA
- [ ] BatchApplyModal mostra preview de alterações
- [ ] Progress bars animam corretamente (0% → 50% → 100%)
- [ ] Tratamento de erros individuais funciona
- [ ] Resumo final exibe corretamente
- [ ] 4 gráficos renderizam com dados reais
- [ ] Gráficos atualizam ao mudar de cluster
- [ ] Toast notifications aparecem (success/error)
- [ ] Highlight de seleção funciona (border + background)

### FASE 3 - MÉDIA
- [ ] Validações de campo mostram toast de erro
- [ ] Shutdown endpoint funciona
- [ ] Erros de Node Pools mostram instruções
- [ ] Tab Dashboard totalmente funcional

---

## 📊 PROGRESSO

| Fase | Status | Progresso | Tempo Estimado |
|------|--------|-----------|----------------|
| **FASE 1** | 🚧 Em Progresso | 0% | 4-6 horas |
| **FASE 2** | ⏳ Pendente | 0% | 8-10 horas |
| **FASE 3** | ⏳ Pendente | 0% | 3-4 horas |
| **TOTAL** | 🚧 Em Progresso | 40% → 100% | 15-20 horas |

---

## 🎯 CRITÉRIOS DE SUCESSO

### Mínimo Viável (MVP)
- ✅ Staging Area funcionando
- ✅ Modal de validação implementado
- ✅ Rollouts DaemonSet/StatefulSet
- ✅ Contador de modificações

### Produto Completo
- ✅ MVP + Modal de aplicação em lote
- ✅ Dashboard com 4 gráficos
- ✅ Toast notifications
- ✅ Todas as validações

---

## 📚 REFERÊNCIAS

- **Análise Completa:** `/tmp/old-web-features.md`
- **Commit da Página Antiga:** `b45464e`
- **HTML Antigo Extraído:** `/tmp/old-web-interface.html`
- **Backend Handlers:** `internal/web/handlers/*.go`
- **Documentação Web:** `Docs/README_WEB.md`

---

## 🚀 PRÓXIMOS PASSOS (Sessão Atual)

1. ✅ Documentar plano completo (CONCLUÍDO)
2. 🔄 Implementar StagingContext + Hook
3. 🔄 Modificar HPAEditor (botões Salvar/Aplicar)
4. 🔄 Adicionar contador no Header
5. 🔄 Implementar ValidationModal
6. 🔄 Adicionar checkboxes de rollout DaemonSet/StatefulSet
7. ✅ Rebuild frontend + backend
8. ✅ Testar funcionalidades críticas

---

**Última atualização:** 2025-10-17 13:30 UTC
**Autor:** Claude Code
**Status:** 🚧 Documentação completa - Iniciando implementação FASE 1
