# 📋 Planejamento: Menu Workload

**Data**: 2025-12-09
**Versão**: 1.0
**Status**: 🟡 Planejamento (Aguardando aprovação para implementação)

---

## 🎯 Objetivo

Criar um menu dropdown "Workload" na navegação principal que agrupe todas as abas relacionadas a recursos Kubernetes de workload, melhorando a organização e usabilidade da interface.

---

## 📊 Análise da Estrutura Atual

### Abas Existentes (13 total)

Atualmente, todas as abas estão no mesmo nível hierárquico na `TabNavigation`:

```typescript
const tabs = [
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "hpas", label: "HPAs", icon: Scale },
  { id: "nodepools", label: "Node Pools", icon: Server },
  { id: "staging", label: "Staging", icon: FileText, badge: stagingCount },
  { id: "cronjobs", label: "CronJobs", icon: Clock },
  { id: "prometheus", label: "Prometheus", icon: Activity },
  { id: "monitoring", label: "Monitoramento", icon: BarChart3 },
  { id: "namespaces", label: "Namespaces", icon: Database },
  { id: "configmaps", label: "ConfigMaps", icon: FileCode },
  { id: "secrets", label: "Secrets", icon: Key },
  { id: "deployments", label: "Deployments", icon: Package },
  { id: "containers", label: "Containers", icon: Box },
  { id: "pods", label: "Pods", icon: Layers },
];
```

**Localização**: `internal/web/frontend/src/pages/Index.tsx:312-326`

---

## 🏗️ Proposta de Nova Estrutura

### 1. Categorização de Abas

#### 📦 **Workload** (Menu Dropdown)
Recursos relacionados à execução de aplicações no cluster:

- **Pods** (`pods`) - `Layers` icon
- **Deployments** (`deployments`) - `Package` icon
- **Containers** (`containers`) - `Box` icon
- **HPAs** (`hpas`) - `Scale` icon
- **CronJobs** (`cronjobs`) - `Clock` icon

#### 🎛️ **Configuration** (Possível menu futuro)
Recursos de configuração do cluster:

- **ConfigMaps** (`configmaps`) - `FileCode` icon
- **Secrets** (`secrets`) - `Key` icon
- **Namespaces** (`namespaces`) - `Database` icon

#### 🖥️ **Infrastructure** (Abas de nível superior)
Recursos de infraestrutura e gerenciamento:

- **Dashboard** (`dashboard`) - `LayoutDashboard` icon
- **Node Pools** (`nodepools`) - `Server` icon
- **Staging** (`staging`) - `FileText` icon + badge
- **Prometheus** (`prometheus`) - `Activity` icon
- **Monitoramento** (`monitoring`) - `BarChart3` icon

---

## 🎨 Design da Interface

### Layout Proposto

```
┌─────────────────────────────────────────────────────────────────┐
│ [Dashboard] [Node Pools] [Staging (3)] [▼ Workload] [...]      │
└─────────────────────────────────────────────────────────────────┘
                                        │
                    ┌───────────────────┴──────────────────────┐
                    │ 📦 Workload                              │
                    ├─────────────────────────────────────────┤
                    │ 🔷 Pods                                  │
                    │ 📦 Deployments                           │
                    │ 📦 Containers                            │
                    │ ⚖️  HPAs                                  │
                    │ ⏰ CronJobs                              │
                    └─────────────────────────────────────────┘
```

### Comportamento Visual

1. **Botão "Workload"**:
   - Ícone: `Layers` ou `Box` (para representar workloads)
   - Label: "Workload"
   - Indicador dropdown: Seta para baixo (`ChevronDown`)
   - Estado ativo: Quando qualquer sub-aba estiver ativa

2. **Dropdown Menu**:
   - Abre ao clicar no botão "Workload"
   - Itens com ícone + label
   - Item ativo destacado visualmente
   - Fecha ao selecionar um item
   - Fecha ao clicar fora (comportamento padrão Radix UI)

3. **Estados Visuais**:
   - **Normal**: Cinza com hover
   - **Ativo**: Gradient azul quando qualquer sub-aba estiver ativa
   - **Hover**: Background cinza claro

---

## 🔧 Implementação Técnica

### Componentes a Criar/Modificar

#### 1. **Novo Componente: `WorkloadMenu.tsx`**

**Localização**: `internal/web/frontend/src/components/WorkloadMenu.tsx`

**Funcionalidades**:
- Dropdown menu usando Radix UI (`DropdownMenu`)
- Lista de abas workload
- Navegação ao clicar em item
- Estado ativo sincronizado com `activeTab`
- Ícones Lucide React

**Props**:
```typescript
interface WorkloadMenuProps {
  activeTab: string;
  onTabChange: (tabId: string) => void;
}
```

**Estrutura**:
```typescript
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Layers, ChevronDown, Box, Package, Scale, Clock } from "lucide-react";

const workloadTabs = [
  { id: "pods", label: "Pods", icon: Layers },
  { id: "deployments", label: "Deployments", icon: Package },
  { id: "containers", label: "Containers", icon: Box },
  { id: "hpas", label: "HPAs", icon: Scale },
  { id: "cronjobs", label: "CronJobs", icon: Clock },
];

export const WorkloadMenu = ({ activeTab, onTabChange }: WorkloadMenuProps) => {
  const isWorkloadActive = workloadTabs.some(tab => tab.id === activeTab);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className={/* styles */}>
          <Layers className="w-4 h-4" />
          Workload
          <ChevronDown className="w-4 h-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {workloadTabs.map(tab => (
          <DropdownMenuItem
            key={tab.id}
            onClick={() => onTabChange(tab.id)}
          >
            <tab.icon className="w-4 h-4 mr-2" />
            {tab.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
```

#### 2. **Modificar: `TabNavigation.tsx`**

**Mudanças**:
- Aceitar prop `children` para renderizar componentes customizados (como WorkloadMenu)
- Manter compatibilidade com abas normais

**Nova Interface**:
```typescript
interface TabNavigationProps {
  tabs: Tab[];
  activeTab: string;
  onTabChange: (tabId: string) => void;
  children?: React.ReactNode; // Novo: para WorkloadMenu
}
```

**Implementação**:
```typescript
export const TabNavigation = ({
  tabs,
  activeTab,
  onTabChange,
  children
}: TabNavigationProps) => {
  return (
    <div className="h-12 bg-card border-b border-border flex items-center px-4 gap-1">
      {tabs.map((tab) => (
        <button key={tab.id} onClick={() => onTabChange(tab.id)}>
          {/* ... renderização normal */}
        </button>
      ))}
      {children} {/* Renderizar WorkloadMenu aqui */}
    </div>
  );
};
```

#### 3. **Modificar: `Index.tsx`**

**Mudanças**:
- Remover abas workload do array `tabs`
- Adicionar `<WorkloadMenu>` dentro de `<TabNavigation>`

**Antes**:
```typescript
const tabs = [
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "hpas", label: "HPAs", icon: Scale },
  // ... todas as abas
];

<TabNavigation tabs={tabs} activeTab={activeTab} onTabChange={handleTabChange} />
```

**Depois**:
```typescript
const tabs = [
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "nodepools", label: "Node Pools", icon: Server },
  { id: "staging", label: "Staging", icon: FileText, badge: staging.getChangesCount().total },
  { id: "prometheus", label: "Prometheus", icon: Activity },
  { id: "monitoring", label: "Monitoramento", icon: BarChart3 },
  { id: "namespaces", label: "Namespaces", icon: Database },
  { id: "configmaps", label: "ConfigMaps", icon: FileCode },
  { id: "secrets", label: "Secrets", icon: Key },
];

<TabNavigation tabs={tabs} activeTab={activeTab} onTabChange={handleTabChange}>
  <WorkloadMenu activeTab={activeTab} onTabChange={handleTabChange} />
</TabNavigation>
```

---

## 📝 Checklist de Implementação

### Fase 1: Criação de Componentes
- [ ] Criar `WorkloadMenu.tsx` com estrutura base
- [ ] Implementar dropdown usando Radix UI
- [ ] Adicionar lista de abas workload
- [ ] Estilizar botão e dropdown (Tailwind CSS)
- [ ] Implementar estados visuais (normal, ativo, hover)

### Fase 2: Integração
- [ ] Modificar `TabNavigation.tsx` para aceitar `children`
- [ ] Atualizar array `tabs` em `Index.tsx` (remover abas workload)
- [ ] Adicionar `<WorkloadMenu>` dentro de `<TabNavigation>`
- [ ] Testar navegação entre abas

### Fase 3: Ajustes Visuais
- [ ] Ajustar espaçamento do menu
- [ ] Validar alinhamento com outras abas
- [ ] Testar responsividade
- [ ] Verificar estado ativo do botão Workload

### Fase 4: Testes
- [ ] Testar navegação de/para cada aba workload
- [ ] Verificar sincronização de estado ativo
- [ ] Testar fechamento do dropdown (click fora)
- [ ] Validar acessibilidade (keyboard navigation)
- [ ] Testar em diferentes resoluções

### Fase 5: Build e Deploy
- [ ] Executar `./rebuild-web.sh -b`
- [ ] Fazer hard refresh no navegador (Ctrl+Shift+R)
- [ ] Validar funcionamento em produção
- [ ] Atualizar documentação (CLAUDE.md)

---

## 🎨 Estilos CSS (Tailwind)

### Botão Workload (Trigger)

```typescript
className={`
  flex items-center gap-2 px-3 py-1.5 rounded-lg font-medium text-sm
  transition-all duration-200 relative
  ${
    isWorkloadActive
      ? "bg-gradient-primary text-white shadow-md"
      : "text-muted-foreground hover:bg-muted hover:text-foreground"
  }
`}
```

### Dropdown Items

```typescript
className={`
  flex items-center gap-2 px-3 py-2 text-sm
  cursor-pointer transition-colors
  ${
    activeTab === tab.id
      ? "bg-accent text-accent-foreground font-medium"
      : "text-foreground hover:bg-accent/50"
  }
`}
```

---

## 🔄 Fluxo de Navegação

```
Usuario clica em "Workload"
  → Dropdown abre
    → Usuario clica em "Pods"
      → handleTabChange("pods")
        → activeTab = "pods"
          → TabNavigation detecta workload ativo
            → Botão "Workload" fica destacado
          → Dropdown fecha automaticamente
            → Renderiza PodsPanel
```

---

## 🚨 Considerações Importantes

### 1. **Estado Ativo do Botão Workload**
O botão "Workload" deve ficar destacado quando **qualquer** sub-aba estiver ativa:

```typescript
const isWorkloadActive = ["pods", "deployments", "containers", "hpas", "cronjobs"]
  .includes(activeTab);
```

### 2. **Badge no Menu Workload**
Se quisermos adicionar badge (ex: número de pods), isso pode ser feito no botão principal:

```typescript
<button>
  <Layers className="w-4 h-4" />
  Workload
  {workloadBadge > 0 && <Badge>{workloadBadge}</Badge>}
  <ChevronDown className="w-4 h-4" />
</button>
```

### 3. **Compatibilidade com Context**
Verificar se `TabContext` precisa ser atualizado para reconhecer abas dentro de menus.

### 4. **Deep Linking**
URLs devem continuar funcionando (ex: `?tab=pods` deve abrir a aba Pods mesmo dentro do menu).

---

## 🔮 Próximos Passos (Futuro)

### Fase 2: Menu "Configuration"
Agrupar ConfigMaps, Secrets e Namespaces em um menu dropdown similar.

### Fase 3: Breadcrumbs
Adicionar breadcrumb para mostrar hierarquia visual:
```
Home > Workload > Pods
```

### Fase 4: Atalhos de Teclado
- `Alt+W` → Abrir menu Workload
- `Alt+P` → Ir para Pods
- `Alt+D` → Ir para Deployments

---

## 📊 Impacto nos Arquivos

| Arquivo | Ação | Complexidade |
|---------|------|--------------|
| `components/WorkloadMenu.tsx` | ✅ Criar | 🟢 Baixa |
| `components/TabNavigation.tsx` | 🔄 Modificar | 🟢 Baixa |
| `pages/Index.tsx` | 🔄 Modificar | 🟡 Média |
| `components/ui/dropdown-menu.tsx` | ✅ Já existe (Radix UI) | - |

**Total de Linhas Estimadas**: ~150-200 linhas (novo componente + modificações)

---

## ✅ Critérios de Aceitação

- [ ] Botão "Workload" visível na navegação principal
- [ ] Dropdown abre ao clicar no botão
- [ ] 5 abas workload presentes no dropdown (Pods, Deployments, Containers, HPAs, CronJobs)
- [ ] Navegação funciona corretamente ao clicar em qualquer item
- [ ] Botão "Workload" fica destacado quando qualquer sub-aba está ativa
- [ ] Dropdown fecha automaticamente ao selecionar item
- [ ] Dropdown fecha ao clicar fora
- [ ] Estilos consistentes com design atual
- [ ] Responsivo em todas as resoluções
- [ ] Acessível via teclado
- [ ] Hard refresh funciona corretamente após build

---

## 📚 Referências

- **Radix UI DropdownMenu**: https://www.radix-ui.com/docs/primitives/components/dropdown-menu
- **Lucide React Icons**: https://lucide.dev/
- **shadcn/ui Dropdown**: https://ui.shadcn.com/docs/components/dropdown-menu
- **Tailwind CSS**: https://tailwindcss.com/docs

---

## 🤔 Questões Abertas

1. **Nome do Menu**: "Workload" ou "Workloads" (plural)?
   - **Recomendação**: "Workload" (singular, mais clean)

2. **Ícone**: `Layers`, `Box` ou `Package`?
   - **Recomendação**: `Layers` (representa múltiplas camadas/recursos)

3. **Ordem dos Itens**: Alfabética ou por relevância?
   - **Recomendação**: Por relevância (Pods → Deployments → Containers → HPAs → CronJobs)

4. **Tooltip**: Adicionar tooltip no botão?
   - **Recomendação**: Sim, "Recursos de workload do cluster"

---

**Aguardando aprovação para iniciar implementação** ✋

---

[⬅️ Voltar ao CLAUDE.md principal](../../CLAUDE.md)
