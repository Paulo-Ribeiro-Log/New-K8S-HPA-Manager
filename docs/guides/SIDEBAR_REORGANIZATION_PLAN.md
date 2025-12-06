# 🎯 Plano de Reorganização com Sidebar

**Data:** 2025-12-04
**Versão:** v1.4.0 (proposta)
**Objetivo:** Reorganizar a interface web com sidebar e menu estruturado por responsabilidades

> **NOTA:** A interface TUI foi completamente removida (commit 0c1eb9b - 02/12/2025).
> O projeto agora é **100% Web** com React/TypeScript.

---

## 📊 Análise da Estrutura Atual

### Abas Atuais (11 abas horizontais)

```typescript
// internal/web/frontend/src/pages/Index.tsx (linha 311-322)
[
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "hpas", label: "HPAs", icon: Scale },
  { id: "nodepools", label: "Node Pools", icon: Server },
  { id: "staging", label: "Staging", icon: FileText },
  { id: "cronjobs", label: "CronJobs", icon: Clock },
  { id: "prometheus", label: "Prometheus", icon: Activity },
  { id: "monitoring", label: "Monitoramento", icon: BarChart3 },
  { id: "configmaps", label: "ConfigMaps", icon: FileCode },
  { id: "secrets", label: "Secrets", icon: Key },
  { id: "deployments", label: "Deployments", icon: Package },
  { id: "containers", label: "Containers", icon: Box }
]
```

### Problemas Identificados

❌ **Navegação horizontal saturada** - 11 abas ocupam muito espaço
❌ **Responsabilidades misturadas** - HPAs, monitoramento, recursos K8s juntos
❌ **Dificulta escalabilidade** - Adicionar novas features fica complicado
❌ **UX comprometida** - Usuário precisa rolar abas horizontalmente
❌ **Falta de hierarquia** - Todas as funcionalidades no mesmo nível

---

## 🎨 Proposta de Reorganização

### Estrutura com Sidebar (Grupos Lógicos)

```
┌─────────────────┬──────────────────────────────────────┐
│                 │  Header (Cluster Selector + User)    │
├─────────────────┼──────────────────────────────────────┤
│  SIDEBAR        │                                       │
│                 │                                       │
│  🏠 Dashboard   │         CONTEÚDO PRINCIPAL           │
│                 │                                       │
│  📊 Scaling     │  (Área de trabalho dinâmica          │
│    ├ HPAs      │   baseada na seleção da sidebar)     │
│    └ Node Pools│                                       │
│                 │                                       │
│  🚀 Workloads   │                                       │
│    ├ Pods      │                                       │
│    ├ Deployments│                                      │
│    └ Containers│                                       │
│                 │                                       │
│  ⚙️  Config      │                                       │
│    ├ ConfigMaps│                                       │
│    ├ Secrets   │                                       │
│    └ CronJobs  │                                       │
│                 │                                       │
│  📈 Monitoring  │                                       │
│    ├ Métricas  │                                       │
│    ├ Alertas   │                                       │
│    └ Prometheus│                                       │
│                 │                                       │
│  📦 Staging     │                                       │
│                 │                                       │
│  📜 History     │                                       │
│                 │                                       │
└─────────────────┴──────────────────────────────────────┘
```

---

## 🗂️ Grupos e Responsabilidades

### 1. 🏠 **Dashboard** (Visão Geral)
**Responsabilidade:** Visão consolidada do cluster
**Funcionalidades:**
- ✅ Cards de estatísticas (clusters, nodes, pods, HPAs)
- ✅ Gráficos de resumo (CPU, Memory, Replicas)
- ✅ Alertas críticos em destaque
- ✅ Links rápidos para recursos

**Componentes:**
- `DashboardCharts`
- `StatsCard`
- `ClusterContextCard`
- `CriticalAlertsBanner`

---

### 2. 📊 **Scaling** (Funcionalidade Original)
**Responsabilidade:** Ajustes de escala horizontal (HPAs) e vertical (Node Pools)

#### 2.1. **HPAs** (Horizontal Pod Autoscalers)
**Funcionalidades:**
- ✅ Listagem de HPAs por namespace
- ✅ Editor inline de Min/Max Replicas
- ✅ Targets (CPU/Memory)
- ✅ Resources (Requests/Limits)
- ✅ Modo de seleção múltipla
- ✅ Export para sessão
- ✅ Navegação para monitoramento

**Componentes:**
- `HPAListItem`
- `HPAEditor`
- `HPATableView`
- `HPAExportButton`

#### 2.2. **Node Pools** (Azure AKS)
**Funcionalidades:**
- ✅ Listagem de node pools do cluster
- ✅ Editor de Min/Max Count
- ✅ Node Count manual
- ✅ VM Size info (vCPUs, memória, disco)
- ✅ Sequenciamento de operações
- ✅ Cordon/Drain com progress
- ✅ Refresh manual de dados Azure

**Componentes:**
- `NodePoolListItem`
- `NodePoolEditor`
- `NodePoolApplyModal`
- `NodePoolSequencingModal`
- `SequenceProgressModal`

---

### 3. 🚀 **Workloads** (Carga de Trabalho)
**Responsabilidade:** Gerenciamento de recursos de aplicação

#### 3.1. **Pods** (Novo - Sugestão)
**Funcionalidades propostas:**
- 🆕 Listagem de pods por namespace
- 🆕 Filtros por status (Running, Pending, Failed)
- 🆕 Logs em tempo real
- 🆕 Exec into container
- 🆕 Port-forward quick access
- 🆕 Restart pods

#### 3.2. **Deployments**
**Funcionalidades atuais:**
- ✅ Listagem de deployments
- ✅ Labels recolhidos por padrão
- ✅ Versão da aplicação (app.kubernetes.io/version)
- ✅ Editor YAML (Monaco Editor)
- ✅ Diff visual
- ✅ Dry-run antes de aplicar

**Componentes:**
- `DeploymentsTab`

#### 3.3. **Containers**
**Funcionalidades atuais:**
- ✅ Listagem de containers por pod
- ✅ Informações de imagem e versão
- ✅ Status e ready state
- ✅ Restart count
- ✅ Refresh manual

**Componentes:**
- `ContainersTab`

---

### 4. ⚙️ **Configuration** (Configuração K8s)
**Responsabilidade:** Gerenciamento de configurações e agendamentos

#### 4.1. **ConfigMaps**
**Funcionalidades atuais:**
- ✅ Listagem de ConfigMaps
- ✅ Filtros por namespace e labels
- ✅ Editor YAML (Monaco Editor)
- ✅ Diff visual (GitHub-style)
- ✅ Labels recolhidos por padrão
- ✅ Campo "Versão" quando disponível

**Componentes:**
- `ConfigMapsTab`

#### 4.2. **Secrets**
**Funcionalidades atuais:**
- ✅ Listagem de Secrets
- ✅ Filtros por namespace e tipo
- ✅ Editor YAML (Monaco Editor)
- ✅ Valores sensíveis ofuscados
- ✅ Labels recolhidos por padrão

**Componentes:**
- `SecretsTab`

#### 4.3. **CronJobs**
**Funcionalidades atuais:**
- ✅ Listagem de CronJobs
- ✅ Parser de expressões cron
- ✅ Descrições legíveis (ex: "Every day at 2:00 AM")
- ✅ Suspend/Resume em lote
- ✅ Validação em tempo real

**Componentes:**
- `CronJobsPage`

---

### 5. 📈 **Monitoring** (Monitoramento e Observabilidade)
**Responsabilidade:** Métricas, alertas e integração Prometheus

#### 5.1. **Métricas** (HPA Watchdog)
**Funcionalidades atuais:**
- ✅ Gráficos de CPU, Memory, Replicas
- ✅ Comparação histórica D-1/D-2/D-3
- ✅ Baseline de 3 dias
- ✅ Seletor de tempo (5min - 24h)
- ✅ Navegação bidirecional com HPAs
- ✅ Modal expandido com detalhes

**Componentes:**
- `MonitoringPage`

#### 5.2. **Alertas** (AlertManager)
**Funcionalidades atuais:**
- ✅ Listagem de todos os alertas ativos
- ✅ Filtro por período de tempo
- ✅ Card de contexto destacado
- ✅ Extração inteligente de HPA/Pod/Container
- ✅ Notificações in-app clicáveis
- ✅ Navegação contextual

**Componentes:**
- `AlertsPage`
- `AlertsDialog`
- `CriticalAlertsBanner`

#### 5.3. **Prometheus Stack**
**Funcionalidades atuais:**
- ✅ Gerenciamento de recursos Prometheus
- ✅ Rollouts de componentes
- ✅ Status dos serviços

**Componentes:**
- `PrometheusPage`

---

### 6. 📦 **Staging** (Área de Pré-visualização)
**Responsabilidade:** Preview e aplicação de mudanças em lote

**Funcionalidades atuais:**
- ✅ SplitView com diff before/after
- ✅ Preview de HPAs e Node Pools
- ✅ Apply All com progresso SSE
- ✅ Temp Staging para "Aplicar Agora"
- ✅ Contador de mudanças (badge)
- ✅ Remoção individual de items

**Componentes:**
- `StagingPanel`
- `ApplyAllModal`

---

### 7. 📜 **History** (Histórico e Auditoria)
**Responsabilidade:** Rastreabilidade de operações

**Funcionalidades atuais:**
- ✅ Audit log completo
- ✅ Filtros por tipo de operação
- ✅ Diff GitHub-style
- ✅ Rollback de configurações
- ✅ Histórico persistente (SQLite)

**Componentes:**
- `HistoryViewer`

---

## 🎨 Design da Sidebar

### Características

**Largura:** 240px (colapsável para 60px)
**Posição:** Fixa à esquerda
**Scroll:** Independente do conteúdo principal
**Tema:** Suporta light/dark mode

### Estados

```typescript
// Sidebar expandida
┌─────────────────────┐
│  📊 Scaling         │ ← Grupo (expansível)
│    ├ HPAs          │ ← Item
│    └ Node Pools    │
│                     │
│  🚀 Workloads       │
│    ├ Pods          │
│    ├ Deployments   │
│    └ Containers    │
└─────────────────────┘

// Sidebar colapsada
┌───┐
│ 📊 │ ← Apenas ícones
│ 🚀 │
│ ⚙️  │
│ 📈 │
│ 📦 │
│ 📜 │
└───┘
```

---

## 🛠️ Plano de Implementação

### Fase 1: Preparação (1-2 dias)
**Objetivo:** Estruturar componentes base da sidebar

**Tarefas:**
1. ✅ Criar componente `<Sidebar>`
2. ✅ Criar componente `<SidebarGroup>` (expansível)
3. ✅ Criar componente `<SidebarItem>` (navegação)
4. ✅ Implementar estado de collapse/expand
5. ✅ Adicionar transições suaves (Framer Motion)
6. ✅ Criar context `SidebarContext` para estado global

**Componentes a criar:**
```
src/components/sidebar/
├── Sidebar.tsx           # Container principal
├── SidebarGroup.tsx      # Grupo expansível
├── SidebarItem.tsx       # Item de menu
├── SidebarToggle.tsx     # Botão collapse/expand
└── index.ts              # Exports
```

---

### Fase 2: Refatoração do Layout (2-3 dias)
**Objetivo:** Migrar de tabs horizontais para sidebar

**Tarefas:**
1. ✅ Refatorar `Index.tsx` para usar sidebar
2. ✅ Remover `TabNavigation` (obsoleto)
3. ✅ Ajustar `Header` para trabalhar com sidebar
4. ✅ Criar roteamento interno para grupos
5. ✅ Migrar estado de navegação para `SidebarContext`
6. ✅ Ajustar responsividade (mobile: sidebar overlay)

**Estrutura de Layout:**
```typescript
// pages/Index.tsx
<div className="flex h-screen">
  <Sidebar />
  <div className="flex-1 flex flex-col">
    <Header />
    <main className="flex-1 overflow-auto">
      {renderActiveSection()}
    </main>
  </div>
</div>
```

---

### Fase 3: Migração de Funcionalidades (3-4 dias)
**Objetivo:** Reorganizar componentes por grupos

**Tarefas:**
1. ✅ Grupo **Scaling**
   - Migrar `HPATab` e `NodePoolTab`
   - Criar rotas `/scaling/hpas` e `/scaling/nodepools`

2. ✅ Grupo **Workloads**
   - Criar `PodsTab` (novo)
   - Migrar `DeploymentsTab` e `ContainersTab`
   - Criar rotas `/workloads/*`

3. ✅ Grupo **Configuration**
   - Migrar `ConfigMapsTab`, `SecretsTab`, `CronJobsPage`
   - Criar rotas `/config/*`

4. ✅ Grupo **Monitoring**
   - Migrar `MonitoringPage`, `AlertsPage`, `PrometheusPage`
   - Criar rotas `/monitoring/*`

5. ✅ **Dashboard** e **Staging**
   - Manter como rotas principais `/` e `/staging`

6. ✅ **History**
   - Migrar `HistoryViewer` para rota `/history`

---

### Fase 4: Melhorias UX (2-3 dias)
**Objetivo:** Polir experiência do usuário

**Tarefas:**
1. ✅ Adicionar breadcrumbs no header
2. ✅ Implementar search global (Cmd+K / Ctrl+K)
3. ✅ Adicionar badges de notificação nos grupos
4. ✅ Tooltips em sidebar colapsada
5. ✅ Atalhos de teclado para navegação
6. ✅ Animações de transição entre páginas

**Atalhos propostos (Web Interface):**
- `Cmd/Ctrl + K` → Search global
- `Cmd/Ctrl + B` → Toggle sidebar
- `Cmd/Ctrl + 1-9` → Navegar para grupos

---

### Fase 5: Testes e Ajustes (1-2 dias)
**Objetivo:** Garantir estabilidade e compatibilidade

**Tarefas:**
1. ✅ Testar navegação entre grupos
2. ✅ Verificar estado persistente (localStorage)
3. ✅ Testar responsividade (mobile/tablet)
4. ✅ Validar acessibilidade (ARIA labels)
5. ✅ Testar dark mode
6. ✅ Corrigir bugs e ajustar espaçamentos

---

## 📱 Responsividade

### Desktop (≥1024px)
- Sidebar fixa visível
- Conteúdo principal com padding adequado

### Tablet (768px - 1023px)
- Sidebar colapsável por padrão
- Overlay ao expandir

### Mobile (< 768px)
- Sidebar como drawer (overlay full)
- Botão hamburger no header
- Fechamento automático ao selecionar item

---

## 🎯 Métricas de Sucesso

**UX:**
- ✅ Redução de cliques para acessar funcionalidades
- ✅ Navegação mais intuitiva e hierárquica
- ✅ Melhor aproveitamento de espaço vertical

**Técnico:**
- ✅ Código mais organizado e modular
- ✅ Facilita adição de novas features
- ✅ Melhor separação de responsabilidades

**Performance:**
- ✅ Lazy loading de componentes por grupo
- ✅ Code splitting por rota
- ✅ Melhor performance de renderização

---

## 🔄 Compatibilidade

### Manter Funcionalidades Atuais
✅ Todas as funcionalidades existentes serão mantidas
✅ Apenas a navegação muda (tabs → sidebar)
✅ Componentes internos permanecem os mesmos
✅ APIs backend não precisam de alteração
✅ **Projeto 100% Web** - TUI foi completamente removida

### Breaking Changes
❌ Nenhuma breaking change para usuários Web
❌ Nenhuma mudança no backend
✅ Apenas reorganização visual do frontend

---

## 📚 Referências de UI

**Exemplos de Sidebar Modernas:**
- GitHub (sidebar com grupos expansíveis)
- Vercel Dashboard (clean e minimalista)
- AWS Console (grupos hierárquicos)
- Azure Portal (sidebar com badges)
- Kubernetes Dashboard (estrutura similar proposta)

---

## 🚀 Próximos Passos

1. ✅ **Aprovação do Plano** - Revisar e aprovar estrutura
2. ⏳ **Criar Issue/Branch** - `feat/sidebar-reorganization`
3. ⏳ **Implementar Fase 1** - Componentes base da sidebar
4. ⏳ **Review Incremental** - Reviews a cada fase concluída
5. ⏳ **Deploy em Dev** - Testar em ambiente de desenvolvimento
6. ⏳ **Release v1.4.0** - Nova versão com sidebar

---

## 📝 Notas Importantes

**Prioridades:**
1. **Manter funcionalidades** - Nenhuma feature pode ser perdida
2. **UX consistente** - Seguir design system atual (shadcn/ui)
3. **Performance** - Não degradar performance atual
4. **Acessibilidade** - Manter/melhorar suporte ARIA

**Riscos:**
- ⚠️ Refatoração grande pode introduzir bugs
- ⚠️ Usuários acostumados com tabs horizontais
- ⚠️ Mobile pode precisar adaptações extras

**Mitigações:**
- ✅ Testes incrementais a cada fase
- ✅ Manter componentes internos intocados
- ✅ Feature flag para rollback se necessário
- ✅ Documentação clara de migração

---

**Autor:** Claude Code
**Data de Criação:** 2025-12-04
**Última Atualização:** 2025-12-04
**Status:** 📋 Proposta em Análise
