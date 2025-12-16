# 🎨 Proposta Visual: Sidebar com Grupos

**Versão:** v1.4.0 (proposta)
**Data:** 2025-12-04

---

## 📊 Comparação: Antes vs Depois

### ❌ ANTES (v1.3.1) - 11 Tabs Horizontais

```
┌─────────────────────────────────────────────────────────────────────┐
│  Header: [Cluster Selector] [User Menu]                            │
├─────────────────────────────────────────────────────────────────────┤
│  TABS: [Dashboard][HPAs][NodePools][Staging][CronJobs]...          │
│        [...Prometheus][Monitoring][ConfigMaps][Secrets]...          │
│        [...Deployments][Containers]                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│                    CONTEÚDO DA ABA ATIVA                            │
│                                                                      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Problemas:**
- ❌ Saturação horizontal (precisa scroll)
- ❌ Difícil adicionar novas features
- ❌ Sem hierarquia clara
- ❌ Todas funcionalidades no mesmo nível

---

### ✅ DEPOIS (v1.4.0) - Sidebar com 7 Grupos

```
┌──────────────┬──────────────────────────────────────────────────────┐
│              │  Header: [Cluster Selector] [User Menu]             │
│              ├──────────────────────────────────────────────────────┤
│  SIDEBAR     │                                                       │
│              │                                                       │
│  🏠 Dashboard│                                                       │
│              │                                                       │
│  📊 Scaling  │             CONTEÚDO DO GRUPO ATIVO                  │
│   ├ HPAs    │                                                       │
│   └ Pools   │                                                       │
│              │                                                       │
│  🚀 Workloads│                                                       │
│   ├ Pods    │                                                       │
│   ├ Deploy  │                                                       │
│   └ Contain │                                                       │
│              │                                                       │
│  ⚙️  Config   │                                                       │
│   ├ ConfigM │                                                       │
│   ├ Secrets │                                                       │
│   └ CronJob │                                                       │
│              │                                                       │
│  📈 Monitor  │                                                       │
│   ├ Metrics │                                                       │
│   ├ Alerts  │                                                       │
│   └ Prometh │                                                       │
│              │                                                       │
│  📦 Staging  │                                                       │
│              │                                                       │
│  📜 History  │                                                       │
│              │                                                       │
└──────────────┴──────────────────────────────────────────────────────┘
```

**Benefícios:**
- ✅ Hierarquia clara (grupos → items)
- ✅ Espaço horizontal liberado
- ✅ Fácil adicionar novas features
- ✅ Navegação intuitiva
- ✅ Melhor UX mobile (sidebar vira drawer)

---

## 🗂️ Estrutura dos 7 Grupos

### 1. 🏠 Dashboard
**O que é:** Visão geral do cluster
**Onde está hoje:** Aba "Dashboard"
**Mudança:** Apenas movido para sidebar (sem alteração de conteúdo)

```
├─ Cards de estatísticas
├─ Gráficos de resumo
├─ Alertas críticos em destaque
└─ Links rápidos
```

---

### 2. 📊 Scaling (Funcionalidade Original)
**O que é:** Ajustes de escala (HPAs + Node Pools)
**Onde está hoje:** Abas "HPAs" e "Node Pools"
**Mudança:** Agrupados sob "Scaling"

```
📊 Scaling
  ├─ HPAs
  │   ├─ Listagem por namespace
  │   ├─ Editor inline (Min/Max/Targets)
  │   ├─ Export para sessão
  │   └─ Navegação para monitoramento
  │
  └─ Node Pools
      ├─ Listagem de pools Azure AKS
      ├─ Editor (Min/Max/Node Count)
      ├─ VM Size info (vCPUs, memória, disco)
      ├─ Sequenciamento de operações
      └─ Cordon/Drain com progress
```

---

### 3. 🚀 Workloads (Carga de Trabalho)
**O que é:** Pods, Deployments, Containers
**Onde está hoje:** Abas "Deployments" e "Containers" (Pods não existe)
**Mudança:** Agrupados sob "Workloads" + criar tab "Pods"

```
🚀 Workloads
  ├─ Pods (🆕 NOVO)
  │   ├─ Listagem por namespace
  │   ├─ Filtros por status
  │   ├─ Logs em tempo real
  │   └─ Port-forward quick access
  │
  ├─ Deployments
  │   ├─ Listagem de deployments
  │   ├─ Editor YAML (Monaco)
  │   ├─ Diff visual
  │   └─ Dry-run antes de aplicar
  │
  └─ Containers
      ├─ Listagem por pod
      ├─ Informações de imagem/versão
      ├─ Status e restart count
      └─ Refresh manual
```

---

### 4. ⚙️ Configuration (Configuração K8s)
**O que é:** ConfigMaps, Secrets, CronJobs
**Onde está hoje:** Abas "ConfigMaps", "Secrets", "CronJobs"
**Mudança:** Agrupados sob "Configuration"

```
⚙️ Configuration
  ├─ ConfigMaps
  │   ├─ Listagem + filtros
  │   ├─ Editor YAML (Monaco)
  │   ├─ Diff visual GitHub-style
  │   └─ Labels recolhidos
  │
  ├─ Secrets
  │   ├─ Listagem + filtros
  │   ├─ Editor YAML
  │   ├─ Valores ofuscados
  │   └─ Labels recolhidos
  │
  └─ CronJobs
      ├─ Listagem de CronJobs
      ├─ Parser de expressões cron
      ├─ Suspend/Resume em lote
      └─ Validação em tempo real
```

---

### 5. 📈 Monitoring (Monitoramento)
**O que é:** Métricas, Alertas, Prometheus
**Onde está hoje:** Abas "Monitoring", "Prometheus" (Alertas tem rota própria)
**Mudança:** Agrupados sob "Monitoring"

```
📈 Monitoring
  ├─ Métricas (HPA Watchdog)
  │   ├─ Gráficos CPU/Memory/Replicas
  │   ├─ Comparação histórica D-1/D-2/D-3
  │   ├─ Baseline de 3 dias
  │   ├─ Seletor de tempo (5min-24h)
  │   └─ Navegação bidirecional com HPAs
  │
  ├─ Alertas (AlertManager)
  │   ├─ Listagem de alertas ativos
  │   ├─ Filtro por período
  │   ├─ Card de contexto destacado
  │   ├─ Notificações in-app clicáveis
  │   └─ Navegação contextual
  │
  └─ Prometheus Stack
      ├─ Gerenciamento de recursos
      ├─ Rollouts de componentes
      └─ Status dos serviços
```

---

### 6. 📦 Staging (Preview de Mudanças)
**O que é:** Área de pré-visualização e apply em lote
**Onde está hoje:** Aba "Staging"
**Mudança:** Apenas movido para sidebar (sem alteração de conteúdo)

```
📦 Staging
  ├─ SplitView com diff before/after
  ├─ Preview de HPAs e Node Pools
  ├─ Apply All com progresso SSE
  ├─ Temp Staging para "Aplicar Agora"
  ├─ Contador de mudanças (badge)
  └─ Remoção individual de items
```

---

### 7. 📜 History (Auditoria)
**O que é:** Histórico de operações e rollback
**Onde está hoje:** Botão no header (modal)
**Mudança:** Promovido para grupo principal na sidebar

```
📜 History
  ├─ Audit log completo
  ├─ Filtros por tipo de operação
  ├─ Diff GitHub-style
  ├─ Rollback de configurações
  └─ Histórico persistente (SQLite)
```

---

## 💡 Interação: Sidebar Colapsável

### Estado Expandido (240px)
```
┌─────────────────────────┐
│  🏠 Dashboard           │
│                          │
│  📊 Scaling             │ ← Clique para expandir/colapsar
│    ├ HPAs              │
│    └ Node Pools        │
│                          │
│  🚀 Workloads           │
│    ├ Pods              │
│    ├ Deployments       │
│    └ Containers        │
└─────────────────────────┘
```

### Estado Colapsado (60px)
```
┌────┐
│ 🏠 │ ← Tooltip: "Dashboard"
│    │
│ 📊 │ ← Tooltip: "Scaling"
│    │
│ 🚀 │ ← Tooltip: "Workloads"
│    │
│ ⚙️  │ ← Tooltip: "Configuration"
│    │
│ 📈 │ ← Tooltip: "Monitoring"
│    │
│ 📦 │ ← Tooltip: "Staging (2)" (badge)
│    │
│ 📜 │ ← Tooltip: "History"
└────┘
```

**Interação:**
- Clique no ícone de grupo → Expande/colapsa grupo
- Clique no item → Navega para a página
- Clique no botão toggle (topo da sidebar) → Colapsa/expande sidebar inteira
- Hover sobre ícone (sidebar colapsada) → Mostra tooltip

---

## 📱 Responsividade

### Desktop (≥1024px)
```
┌──────────────┬─────────────────────────────────┐
│   SIDEBAR    │         CONTEÚDO               │
│   (240px)    │         (flex-1)               │
│   Fixa       │                                │
└──────────────┴─────────────────────────────────┘
```

### Tablet (768px - 1023px)
```
┌───┬──────────────────────────────────────────┐
│ S │           CONTEÚDO                      │
│ I │           (flex-1)                      │
│ D │                                         │
│ E │  ← Sidebar colapsada por padrão       │
│   │  ← Clique para expandir (overlay)     │
└───┴──────────────────────────────────────────┘
```

### Mobile (< 768px)
```
┌──────────────────────────────────────────────┐
│  [☰] Header                                 │ ← Botão hamburger
├──────────────────────────────────────────────┤
│                                              │
│            CONTEÚDO                          │
│                                              │
└──────────────────────────────────────────────┘

Clique em [☰] abre Sidebar como Drawer (overlay full-width)
```

---

## 🎯 Resumo de Mudanças

| Item | Antes (v1.3.1) | Depois (v1.4.0) |
|------|----------------|-----------------|
| **Navegação** | 11 tabs horizontais | Sidebar com 7 grupos |
| **Dashboard** | Tab "Dashboard" | Grupo "🏠 Dashboard" |
| **HPAs** | Tab "HPAs" | Grupo "📊 Scaling" → HPAs |
| **Node Pools** | Tab "Node Pools" | Grupo "📊 Scaling" → Node Pools |
| **Pods** | ❌ Não existe | 🆕 Grupo "🚀 Workloads" → Pods |
| **Deployments** | Tab "Deployments" | Grupo "🚀 Workloads" → Deployments |
| **Containers** | Tab "Containers" | Grupo "🚀 Workloads" → Containers |
| **ConfigMaps** | Tab "ConfigMaps" | Grupo "⚙️ Configuration" → ConfigMaps |
| **Secrets** | Tab "Secrets" | Grupo "⚙️ Configuration" → Secrets |
| **CronJobs** | Tab "CronJobs" | Grupo "⚙️ Configuration" → CronJobs |
| **Monitoramento** | Tab "Monitoring" | Grupo "📈 Monitoring" → Métricas |
| **Alertas** | Rota `/alerts/:cluster` | Grupo "📈 Monitoring" → Alertas |
| **Prometheus** | Tab "Prometheus" | Grupo "📈 Monitoring" → Prometheus Stack |
| **Staging** | Tab "Staging" | Grupo "📦 Staging" |
| **History** | Botão no header (modal) | Grupo "📜 History" |

---

## ✅ Checklist de Implementação

### Fase 1: Componentes Base
- [ ] Criar `<Sidebar>`
- [ ] Criar `<SidebarGroup>` (expansível)
- [ ] Criar `<SidebarItem>`
- [ ] Criar `<SidebarToggle>`
- [ ] Implementar `SidebarContext`

### Fase 2: Layout
- [ ] Refatorar `Index.tsx` para usar sidebar
- [ ] Remover `TabNavigation`
- [ ] Ajustar `Header`
- [ ] Implementar roteamento interno

### Fase 3: Migração de Funcionalidades
- [ ] Grupo Scaling (HPAs + Node Pools)
- [ ] Grupo Workloads (criar Pods, migrar Deployments/Containers)
- [ ] Grupo Configuration (ConfigMaps + Secrets + CronJobs)
- [ ] Grupo Monitoring (Métricas + Alertas + Prometheus)
- [ ] Dashboard e Staging (sem mudanças)
- [ ] History (promover para grupo)

### Fase 4: UX
- [ ] Breadcrumbs no header
- [ ] Search global (Cmd+K)
- [ ] Badges de notificação
- [ ] Tooltips em sidebar colapsada
- [ ] Atalhos de teclado

### Fase 5: Testes
- [ ] Testar navegação
- [ ] Testar responsividade
- [ ] Validar acessibilidade
- [ ] Testar dark mode

---

**Aprovação necessária para iniciar implementação.**

**Estimativa:** 10-14 dias de trabalho
**Release:** v1.4.0
