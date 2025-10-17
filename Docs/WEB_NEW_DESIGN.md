# 🎨 Interface Web - Novo Design (Sem Scroll)

**Data:** 16 de Outubro de 2025
**Inspiração:** Streamlit, Grafana, Vercel Dashboard
**Conceito:** Layout fixo sem scroll + Gráficos interativos

---

## 🎯 Problemas do Design Atual (POC)

### ❌ Issues
1. **Scroll excessivo** - Usuário perde contexto ao rolar página
2. **Seleção de cluster confusa** - Cards grandes ocupam muito espaço
3. **Sem visualização de dados** - Apenas tabelas/listas
4. **Navegação lenta** - Muitos cliques para chegar nos HPAs
5. **Estatísticas escondidas** - Dashboard pouco útil

---

## ✨ Novo Design: Dashboard Moderno

### 📐 Layout Principal (Sem Scroll)

```
┌─────────────────────────────────────────────────────────────────┐
│  🔷 k8s-hpa-manager    [Cluster: akspriv-prod ▼]  👤 User  🔔  │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────┐ │
│  │ 📦 24      │  │ 📁 45      │  │ ⚖️ 120     │  │ 🖥️ 8    │ │
│  │ Clusters   │  │ Namespaces │  │ HPAs       │  │ Pools    │ │
│  └────────────┘  └────────────┘  └────────────┘  └──────────┘ │
│                                                                 │
│  ┌───────────────────────────────┐  ┌─────────────────────────┐│
│  │  📊 CPU Usage (24h)           │  │  📈 Memory Usage (24h)  ││
│  │  ┌─────────────────────────┐  │  │  ┌───────────────────┐ ││
│  │  │    ╱╲    ╱╲             │  │  │  │      ╱╲          │ ││
│  │  │   ╱  ╲  ╱  ╲            │  │  │  │     ╱  ╲    ╱╲   │ ││
│  │  │  ╱    ╲╱    ╲           │  │  │  │    ╱    ╲  ╱  ╲  │ ││
│  │  │ ╱              ╲        │  │  │  │   ╱      ╲╱    ╲ │ ││
│  │  └─────────────────────────┘  │  │  └───────────────────┘ ││
│  │  Avg: 45%  Peak: 78%         │  │  Avg: 3.2Gi Peak: 8Gi ││
│  └───────────────────────────────┘  └─────────────────────────┘│
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  ⚖️ HPAs por Namespace                                   │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │ ingress-nginx    ████████████░░░░░░░░░░  12        │  │  │
│  │  │ monitoring       ███████░░░░░░░░░░░░░░░░   7        │  │  │
│  │  │ production       █████████████████░░░░░░  15        │  │  │
│  │  │ staging          ████░░░░░░░░░░░░░░░░░░░   4        │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Características:**
- ✅ **100vh** - Altura fixa (viewport height)
- ✅ **Sem scroll** - Todo conteúdo visível
- ✅ **Grid layout** - 2 colunas + cards
- ✅ **Gráficos inline** - Visualização imediata
- ✅ **Cluster no header** - Dropdown sempre visível

---

## 🎨 Componentes do Novo Design

### 1. Header Fixo (Sempre Visível)

```html
┌─────────────────────────────────────────────────────────────┐
│ 🔷 k8s-hpa-manager                                          │
│                                                             │
│ 📦 Cluster: [akspriv-faturamento-prd ▼]  🔔 👤 Admin  ⚙️  │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Dropdown de cluster (sempre acessível)
- Notificações em tempo real
- User menu
- Settings

**Dropdown de Cluster:**
```
┌─────────────────────────────┐
│ 🔍 Buscar cluster...        │
├─────────────────────────────┤
│ ✓ akspriv-faturamento-prd   │ ← Selecionado
│   akspriv-faturamento-hlg   │
│   akspriv-plataforma-prd    │
│   akspriv-plataforma-hlg    │
│ ──────────────────────────  │
│ 📊 Todos (24 clusters)      │
└─────────────────────────────┘
```

---

### 2. Stats Cards (4 Cards Horizontais)

```html
┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ 📦 24        │  │ 📁 45        │  │ ⚖️ 120       │  │ 🖥️ 8         │
│ Clusters     │  │ Namespaces   │  │ HPAs         │  │ Node Pools   │
│ +2 online    │  │ +5 new       │  │ ↑15 scale up │  │ 3 autoscale  │
└──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘
```

**Features:**
- Números grandes
- Ícones coloridos
- Trend indicators (↑↓)
- Clicável (filtro rápido)

---

### 3. Gráficos Interativos (2 Colunas)

#### 3.1 CPU Usage (Line Chart)
```javascript
// Chart.js config
{
  type: 'line',
  data: {
    labels: ['00h', '04h', '08h', '12h', '16h', '20h', '24h'],
    datasets: [{
      label: 'CPU Usage',
      data: [45, 52, 48, 71, 65, 58, 49],
      borderColor: '#667eea',
      backgroundColor: 'rgba(102, 126, 234, 0.1)',
      tension: 0.4
    }]
  },
  options: {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: { mode: 'index', intersect: false }
    }
  }
}
```

#### 3.2 Memory Usage (Area Chart)
```javascript
{
  type: 'line',
  data: {
    datasets: [{
      label: 'Memory',
      data: [...],
      fill: true,
      backgroundColor: 'rgba(72, 187, 120, 0.2)',
      borderColor: '#48bb78'
    }]
  }
}
```

#### 3.3 HPAs por Namespace (Horizontal Bar)
```javascript
{
  type: 'bar',
  data: {
    labels: ['ingress-nginx', 'monitoring', 'production'],
    datasets: [{
      data: [12, 7, 15],
      backgroundColor: ['#667eea', '#764ba2', '#f093fb']
    }]
  },
  options: {
    indexAxis: 'y'  // Horizontal
  }
}
```

#### 3.4 Replicas Distribution (Doughnut)
```javascript
{
  type: 'doughnut',
  data: {
    labels: ['Min', 'Current', 'Max'],
    datasets: [{
      data: [120, 245, 600],
      backgroundColor: ['#f56565', '#ecc94b', '#48bb78']
    }]
  }
}
```

---

### 4. Layout com Tabs (Sem Scroll)

```html
┌─────────────────────────────────────────────────────────────┐
│ Header (fixo)                                               │
├─────────────────────────────────────────────────────────────┤
│ Stats Cards (fixo)                                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ ┌─────┬─────┬─────┬─────┬─────┬─────┐                      │
│ │ 📊  │ ⚖️  │ 🖥️  │ ⏰  │ 📈  │ 💾  │  ← Tabs             │
│ │ Dash│ HPAs│Pools│Cron │Prom │Sess │                      │
│ └─────┴─────┴─────┴─────┴─────┴─────┘                      │
│                                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │                                                         │ │
│ │        Tab Content (altura fixa, scroll interno)       │ │
│ │                                                         │ │
│ │                                                         │ │
│ │                                                         │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Características:**
- Tabs para navegação rápida
- Conteúdo de cada tab tem altura fixa
- Scroll **apenas dentro** do tab content (se necessário)
- Header e stats sempre visíveis

---

## 🎨 Design Específico por Tab

### Tab 1: 📊 Dashboard (Gráficos)

```html
┌─────────────────────────────────────────────────────────────┐
│  CPU Usage (24h)          │  Memory Usage (24h)            │
│  ┌─────────────────────┐  │  ┌───────────────────────────┐ │
│  │  [Line Chart]       │  │  │  [Area Chart]             │ │
│  └─────────────────────┘  │  └───────────────────────────┘ │
├───────────────────────────┴────────────────────────────────┤
│  HPAs by Namespace        │  Replicas Distribution        │
│  ┌─────────────────────┐  │  ┌───────────────────────────┐ │
│  │  [Bar Chart]        │  │  │  [Doughnut Chart]         │ │
│  └─────────────────────┘  │  └───────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Gráficos:**
1. CPU Usage Over Time (line)
2. Memory Usage Over Time (area)
3. HPAs per Namespace (horizontal bar)
4. Current vs Min/Max Replicas (doughnut)
5. Node Pool Autoscaling Status (pie)
6. CronJob Success Rate (bar)

---

### Tab 2: ⚖️ HPAs (Lista + Editor)

**Layout Split:**
```html
┌─────────────────────────────────────────────────────────────┐
│  Lista HPAs (40%)         │  Editor HPA (60%)              │
│  ┌─────────────────────┐  │  ┌───────────────────────────┐ │
│  │ ☑ nginx-controller  │  │  │ 📝 Editando: nginx...     │ │
│  │ ☑ api-gateway       │  │  │                           │ │
│  │ ☐ auth-service      │  │  │ Min Replicas: [3]         │ │
│  │ ☐ worker-pool       │  │  │ Max Replicas: [20]        │ │
│  │                     │  │  │ Target CPU:   [70]%       │ │
│  │ [5 de 45]           │  │  │                           │ │
│  │                     │  │  │ 🔧 Resources:             │ │
│  │ Ações:              │  │  │ CPU Req: [100m]           │ │
│  │ [Apply Selected]    │  │  │ CPU Lim: [500m]           │ │
│  │ [Load Session]      │  │  │                           │ │
│  └─────────────────────┘  │  │ [Preview] [Apply] [Reset] │ │
│                           │  └───────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Lista à esquerda (searchable, filterable)
- Editor à direita (form completo)
- Multi-select para batch
- Preview de mudanças
- Gráfico inline: Current replicas over time

---

### Tab 3: 🖥️ Node Pools (Cards Grid)

```html
┌─────────────────────────────────────────────────────────────┐
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ monitoring-1 │  │ monitoring-2 │  │ production   │      │
│  │ ━━━━━━░░░░   │  │ ━━━━━━━━━░   │  │ ━━━━━━━━━━   │      │
│  │ 3/10 nodes   │  │ 9/10 nodes   │  │ 10/15 nodes  │      │
│  │              │  │              │  │              │      │
│  │ Auto: ON ✓   │  │ Auto: ON ✓   │  │ Manual       │      │
│  │ Min: 2       │  │ Min: 5       │  │ Count: 10    │      │
│  │ Max: 10      │  │ Max: 10      │  │              │      │
│  │              │  │              │  │              │      │
│  │ [Edit] [*1]  │  │ [Edit] [*2]  │  │ [Edit]       │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                             │
│  Sequential Execution: *1 → *2  [Start Sequence]           │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Cards em grid (2-3 colunas)
- Progress bars visuais
- Toggle auto/manual inline
- Sequential markers (*1, *2)
- Mini gráfico: Node count history

---

### Tab 4: ⏰ CronJobs (Table Compacta)

```html
┌─────────────────────────────────────────────────────────────┐
│ Name              Schedule        Status   Last Run Actions │
│ ───────────────────────────────────────────────────────────│
│ backup-db         0 2 * * *      🟢 Active  2h ago  [Edit] │
│ cleanup-logs      0 */6 * * *    🔴 Suspend 1d ago  [Edit] │
│ report-generator  0 8 * * 1      🟢 Active  3d ago  [Edit] │
│ ───────────────────────────────────────────────────────────│
│                                           [Batch Enable]    │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Tabela compacta (sem scroll)
- Toggle suspend inline
- Batch operations
- Mini chart: Execution success rate

---

### Tab 5: 📈 Prometheus (Grid + Metrics)

```html
┌─────────────────────────────────────────────────────────────┐
│  Component         CPU Usage      Memory Usage     Actions  │
│  ─────────────────────────────────────────────────────────  │
│  prometheus-server                                          │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Request: 1       ━━━━━━░░░░░░░░  Usage: 264m (26%)    │ │
│  │ Limit:   2                                             │ │
│  │                                                        │ │
│  │ Request: 8Gi     ━━━━━━━━━━━░░  Usage: 3.9Gi (49%)   │ │
│  │ Limit:   12Gi                                          │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                  [Edit]     │
│  ─────────────────────────────────────────────────────────  │
│  grafana                                                    │
│  [Similar layout...]                                        │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Progress bars de uso
- Request/Limit lado a lado
- Inline editing
- Real-time metrics

---

## 📊 Biblioteca de Gráficos Recomendada

### Opção 1: Chart.js (Mais Simples)
```bash
npm install chart.js react-chartjs-2
```

**Prós:**
- ✅ Simples e leve
- ✅ Gráficos bonitos out-of-the-box
- ✅ Boa documentação
- ✅ Responsivo

**Contras:**
- ❌ Menos customizável
- ❌ Performance em datasets grandes

### Opção 2: Recharts (React Native)
```bash
npm install recharts
```

**Prós:**
- ✅ Componentes React nativos
- ✅ Composable (flexível)
- ✅ TypeScript support
- ✅ Animações suaves

**Contras:**
- ❌ Bundle maior
- ❌ Curva de aprendizado

### Opção 3: Apache ECharts (Mais Poderoso)
```bash
npm install echarts echarts-for-react
```

**Prós:**
- ✅ Muito poderoso
- ✅ Performance excelente
- ✅ Muitos tipos de gráficos
- ✅ Temas prontos (dark mode)

**Contras:**
- ❌ Bundle pesado
- ❌ Complexo para simples

**Recomendação:** **Recharts** para balance entre simplicidade e flexibilidade

---

## 🎨 Exemplo de Implementação (Vue.js)

### Dashboard.vue (Com Gráficos)

```vue
<template>
  <div class="dashboard-container">
    <!-- Header Fixo -->
    <header class="header">
      <h1>🔷 k8s-hpa-manager</h1>
      <select v-model="selectedCluster" @change="onClusterChange">
        <option v-for="c in clusters" :key="c.context" :value="c.context">
          {{ c.name }}
        </option>
      </select>
    </header>

    <!-- Stats Cards -->
    <div class="stats-grid">
      <StatsCard icon="📦" label="Clusters" :value="stats.clusters" />
      <StatsCard icon="📁" label="Namespaces" :value="stats.namespaces" />
      <StatsCard icon="⚖️" label="HPAs" :value="stats.hpas" />
      <StatsCard icon="🖥️" label="Pools" :value="stats.pools" />
    </div>

    <!-- Tabs -->
    <div class="tabs">
      <button @click="activeTab = 'dashboard'" :class="{active: activeTab === 'dashboard'}">
        📊 Dashboard
      </button>
      <button @click="activeTab = 'hpas'" :class="{active: activeTab === 'hpas'}">
        ⚖️ HPAs
      </button>
      <!-- ... mais tabs -->
    </div>

    <!-- Tab Content -->
    <div class="tab-content">
      <!-- Dashboard Tab -->
      <div v-if="activeTab === 'dashboard'" class="charts-grid">
        <div class="chart-container">
          <h3>CPU Usage (24h)</h3>
          <Line :data="cpuChartData" :options="chartOptions" />
        </div>
        <div class="chart-container">
          <h3>Memory Usage (24h)</h3>
          <Line :data="memoryChartData" :options="chartOptions" />
        </div>
        <div class="chart-container">
          <h3>HPAs by Namespace</h3>
          <Bar :data="hpasByNsData" :options="barOptions" />
        </div>
        <div class="chart-container">
          <h3>Replicas Distribution</h3>
          <Doughnut :data="replicasData" :options="doughnutOptions" />
        </div>
      </div>

      <!-- HPAs Tab -->
      <div v-if="activeTab === 'hpas'" class="split-layout">
        <div class="list-panel">
          <HPAList @select="onHPASelect" />
        </div>
        <div class="editor-panel">
          <HPAEditor :hpa="selectedHPA" @save="onHPASave" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { Line, Bar, Doughnut } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  Title,
  Tooltip,
  Legend
} from 'chart.js'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  Title,
  Tooltip,
  Legend
)

const selectedCluster = ref('')
const activeTab = ref('dashboard')
const selectedHPA = ref(null)

const cpuChartData = computed(() => ({
  labels: ['00h', '04h', '08h', '12h', '16h', '20h', '24h'],
  datasets: [{
    label: 'CPU %',
    data: [45, 52, 48, 71, 65, 58, 49],
    borderColor: '#667eea',
    backgroundColor: 'rgba(102, 126, 234, 0.1)',
    tension: 0.4
  }]
}))

const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false }
  }
}
</script>

<style scoped>
.dashboard-container {
  display: flex;
  flex-direction: column;
  height: 100vh; /* Altura fixa */
  overflow: hidden; /* Sem scroll */
}

.header {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 20px;
  padding: 20px;
  height: 120px;
}

.tabs {
  display: flex;
  gap: 0;
  height: 50px;
  border-bottom: 1px solid #e2e8f0;
}

.tab-content {
  flex: 1; /* Ocupa espaço restante */
  overflow: auto; /* Scroll apenas aqui se necessário */
  padding: 20px;
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
  height: 100%;
}

.chart-container {
  background: white;
  border-radius: 8px;
  padding: 20px;
  box-shadow: 0 2px 4px rgba(0,0,0,0.1);
  height: calc((100vh - 300px) / 2); /* Altura calculada */
}

.split-layout {
  display: grid;
  grid-template-columns: 40% 60%;
  gap: 20px;
  height: 100%;
}
</style>
```

---

## 🚀 Implementação Incremental

### Fase 1: Layout Sem Scroll (2 horas)
```bash
✅ Header fixo com cluster selector
✅ Stats cards (4 cards)
✅ Tabs navigation
✅ Tab content com altura fixa
✅ CSS grid/flexbox
```

### Fase 2: Gráficos Básicos (3 horas)
```bash
✅ Setup Chart.js ou Recharts
✅ CPU usage line chart
✅ Memory usage area chart
✅ HPAs bar chart
✅ Responsivo
```

### Fase 3: Integração API (2 horas)
```bash
✅ Fetch dados reais
✅ Atualizar gráficos
✅ Loading states
✅ Error handling
```

### Fase 4: Tabs Funcionais (3 horas)
```bash
✅ HPA list + editor
✅ Node pools grid
✅ CronJobs table
✅ Prometheus metrics
```

**Total: ~10 horas de desenvolvimento**

---

## 📋 Checklist de Implementação

### Backend (Novos Endpoints)
- [ ] `GET /api/v1/metrics/cpu?cluster=X&range=24h`
- [ ] `GET /api/v1/metrics/memory?cluster=X&range=24h`
- [ ] `GET /api/v1/metrics/hpas-by-namespace?cluster=X`
- [ ] `GET /api/v1/metrics/replicas-distribution?cluster=X`

### Frontend
- [ ] Redesign index.html sem scroll
- [ ] Implementar cluster selector no header
- [ ] Adicionar biblioteca de gráficos
- [ ] Criar 4 gráficos principais
- [ ] Layout de tabs
- [ ] Split view para HPA editor
- [ ] Grid para node pools
- [ ] Tabela compacta para cronjobs

---

**Quer que eu implemente o novo layout agora?** 🎨

A. Sim, criar novo index.html com gráficos
B. Primeiro ver mockup/protótipo
C. Ajustar design antes
