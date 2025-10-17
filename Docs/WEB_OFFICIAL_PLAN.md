# 🚀 Interface Web - Plano de Implementação Oficial

**Data:** 16 de Outubro de 2025
**Versão Alvo:** 1.0.0 Official
**Base:** POC Funcional (80% → 100%)

---

## 📊 Status Atual (POC)

### ✅ Implementado
- [x] Backend REST API (Gin Framework)
- [x] Autenticação Bearer Token
- [x] Endpoints: Clusters, Namespaces, HPAs (GET)
- [x] Frontend SPA básico (HTML/CSS/JS)
- [x] Login e navegação básica
- [x] Listagem de Clusters/Namespaces/HPAs

### ❌ Faltando (Features do TUI)
- [ ] **Edição de HPAs** (min/max replicas, CPU/Memory targets)
- [ ] **Edição de Resources** (CPU/Memory requests/limits)
- [ ] **Rollouts** (Deployment/DaemonSet/StatefulSet)
- [ ] **Node Pools** (escala, autoscaling, execução sequencial)
- [ ] **CronJobs** (enable/disable, listagem)
- [ ] **Prometheus Stack** (gerenciamento de recursos)
- [ ] **Sessões** (save/load/templates)
- [ ] **Batch Operations** (aplicar múltiplos HPAs/Node Pools)
- [ ] **Progress Tracking** (real-time updates)
- [ ] **Logs** (visualizador integrado)

---

## 🎯 Funcionalidades Completas (TUI Parity)

### 1. 🔧 HPA Management (Completo)

#### 1.1 Edição Individual
**Endpoint:** `PUT /api/v1/hpas/:cluster/:namespace/:name`

**Campos editáveis:**
- Min Replicas (1-1000)
- Max Replicas (1-1000)
- Target CPU (1-100%)
- Target Memory (1-100%) [opcional]

**Validações:**
- Min >= 1
- Max >= Min
- CPU/Memory 1-100%

#### 1.2 Edição de Resources
**Endpoint:** `PUT /api/v1/hpas/:cluster/:namespace/:name/resources`

**Campos editáveis:**
- CPU Request (ex: "100m", "1")
- CPU Limit (ex: "500m", "2")
- Memory Request (ex: "128Mi", "1Gi")
- Memory Limit (ex: "512Mi", "2Gi")

**Validações:**
- Formato válido (regex: `^\d+(\.\d+)?(m|Mi|Gi)?$`)
- Limit >= Request

#### 1.3 Rollouts
**Endpoint:** `PUT /api/v1/hpas/:cluster/:namespace/:name/rollout`

**Tipos:**
- Deployment (padrão)
- DaemonSet
- StatefulSet

**Funcionalidade:**
- Checkbox para habilitar rollout
- Seleção do tipo
- Preview do comando kubectl

#### 1.4 Batch Operations
**Endpoint:** `POST /api/v1/hpas/batch`

**Body:**
```json
{
  "hpas": [
    {
      "cluster": "...",
      "namespace": "...",
      "name": "...",
      "changes": {
        "min_replicas": 5,
        "max_replicas": 20,
        "target_cpu": 70
      }
    }
  ]
}
```

**Funcionalidade:**
- Aplicar mudanças em múltiplos HPAs
- Progress tracking em tempo real
- Rollback em caso de erro

---

### 2. 🖥️ Node Pool Management

#### 2.1 Listagem
**Endpoint:** `GET /api/v1/nodepools?cluster=X`

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "name": "monitoring-1",
      "cluster": "akspriv-prod",
      "resource_group": "rg-prod",
      "node_count": 3,
      "autoscaling_enabled": true,
      "min_count": 2,
      "max_count": 10,
      "vm_size": "Standard_D4s_v3"
    }
  ]
}
```

#### 2.2 Edição Individual
**Endpoint:** `PUT /api/v1/nodepools/:cluster/:name`

**Campos editáveis:**
- Node Count (manual mode)
- Autoscaling Enabled (true/false)
- Min Count (autoscaling mode)
- Max Count (autoscaling mode)

**Validações:**
- Node Count >= 0
- Min >= 0
- Max >= Min
- Se autoscaling: Min/Max obrigatórios

#### 2.3 Execução Sequencial
**Endpoint:** `POST /api/v1/nodepools/sequential`

**Body:**
```json
{
  "pools": [
    {
      "cluster": "...",
      "name": "monitoring-1",
      "order": 1,
      "changes": {...}
    },
    {
      "cluster": "...",
      "name": "monitoring-2",
      "order": 2,
      "changes": {...}
    }
  ]
}
```

**Funcionalidade:**
- Marca até 2 pools (*1, *2)
- Executa *1 primeiro
- Aguarda conclusão
- Executa *2 automaticamente
- WebSocket para updates em tempo real

---

### 3. ⏰ CronJob Management

#### 3.1 Listagem
**Endpoint:** `GET /api/v1/cronjobs?cluster=X&namespace=Y`

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "name": "backup-job",
      "namespace": "production",
      "schedule": "0 2 * * *",
      "schedule_text": "Executa todo dia às 2:00 AM",
      "suspend": false,
      "status": "active",
      "last_schedule": "2025-10-16T02:00:00Z"
    }
  ]
}
```

#### 3.2 Enable/Disable
**Endpoint:** `PUT /api/v1/cronjobs/:cluster/:namespace/:name`

**Body:**
```json
{
  "suspend": true  // ou false
}
```

#### 3.3 Batch Operations
**Endpoint:** `POST /api/v1/cronjobs/batch`

---

### 4. 📊 Prometheus Stack Management

#### 4.1 Listagem com Métricas
**Endpoint:** `GET /api/v1/prometheus?cluster=X`

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "name": "prometheus-server",
      "namespace": "monitoring",
      "cpu_request": "1",
      "cpu_limit": "2",
      "cpu_usage": "264m",
      "memory_request": "8Gi",
      "memory_limit": "12Gi",
      "memory_usage": "3918Mi"
    }
  ]
}
```

#### 4.2 Edição de Resources
**Endpoint:** `PUT /api/v1/prometheus/:cluster/:namespace/:name`

**Body:**
```json
{
  "cpu_request": "2",
  "cpu_limit": "4",
  "memory_request": "16Gi",
  "memory_limit": "24Gi"
}
```

---

### 5. 💾 Session Management

#### 5.1 Salvar Sessão
**Endpoint:** `POST /api/v1/sessions`

**Body:**
```json
{
  "name": "upscale-prod-2025-10-16",
  "type": "HPA-Upscale",  // HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale, Mixed
  "template": "{action}_{cluster}_{date}_{user}",
  "data": {
    "hpas": [...],
    "nodepools": [...]
  }
}
```

#### 5.2 Listar Sessões
**Endpoint:** `GET /api/v1/sessions?type=HPA-Upscale`

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": "uuid",
      "name": "upscale-prod-2025-10-16",
      "type": "HPA-Upscale",
      "created_at": "2025-10-16T10:00:00Z",
      "hpa_count": 15,
      "nodepool_count": 2
    }
  ]
}
```

#### 5.3 Carregar Sessão
**Endpoint:** `GET /api/v1/sessions/:id`

#### 5.4 Aplicar Sessão
**Endpoint:** `POST /api/v1/sessions/:id/apply`

---

### 6. 📡 WebSocket (Real-time Updates)

#### 6.1 Conexão
**Endpoint:** `ws://localhost:8080/ws`

**Auth:** Bearer Token via query param
```
ws://localhost:8080/ws?token=poc-token-123
```

#### 6.2 Mensagens

**Progress Update:**
```json
{
  "type": "progress",
  "data": {
    "operation": "hpa_update",
    "cluster": "akspriv-prod",
    "namespace": "ingress-nginx",
    "name": "nginx-controller",
    "status": "in_progress",
    "percentage": 50
  }
}
```

**Completion:**
```json
{
  "type": "complete",
  "data": {
    "operation": "hpa_update",
    "status": "success",
    "message": "HPA updated successfully"
  }
}
```

**Error:**
```json
{
  "type": "error",
  "data": {
    "operation": "hpa_update",
    "error": "Failed to update: connection timeout"
  }
}
```

---

### 7. 📊 Logs Viewer

#### 7.1 Stream de Logs
**Endpoint:** `GET /api/v1/logs/stream` (SSE)

**Response:**
```
data: {"level":"info","message":"HPA updated: ingress-nginx/nginx-controller","timestamp":"2025-10-16T10:00:00Z"}

data: {"level":"success","message":"✅ All changes applied","timestamp":"2025-10-16T10:00:05Z"}
```

#### 7.2 Download de Logs
**Endpoint:** `GET /api/v1/logs/download?date=2025-10-16`

---

## 🎨 Frontend (Vue.js 3 + Vite)

### Estrutura de Diretórios
```
web/
├── public/
│   └── favicon.ico
├── src/
│   ├── main.js
│   ├── App.vue
│   ├── router/
│   │   └── index.js
│   ├── stores/
│   │   ├── auth.js
│   │   ├── clusters.js
│   │   ├── hpas.js
│   │   ├── nodepools.js
│   │   └── sessions.js
│   ├── views/
│   │   ├── Login.vue
│   │   ├── Dashboard.vue
│   │   ├── Clusters.vue
│   │   ├── HPAs.vue
│   │   ├── HPAEditor.vue
│   │   ├── NodePools.vue
│   │   ├── NodePoolEditor.vue
│   │   ├── CronJobs.vue
│   │   ├── Prometheus.vue
│   │   └── Sessions.vue
│   ├── components/
│   │   ├── NavBar.vue
│   │   ├── SideBar.vue
│   │   ├── ClusterCard.vue
│   │   ├── HPACard.vue
│   │   ├── NodePoolCard.vue
│   │   ├── ProgressBar.vue
│   │   ├── LogViewer.vue
│   │   └── ConfirmModal.vue
│   ├── composables/
│   │   ├── useAPI.js
│   │   ├── useWebSocket.js
│   │   └── useNotifications.js
│   └── assets/
│       └── styles/
├── package.json
├── vite.config.js
└── tailwind.config.js
```

### Tech Stack
- **Framework:** Vue.js 3 (Composition API)
- **Build:** Vite
- **Router:** Vue Router 4
- **State:** Pinia
- **UI:** Tailwind CSS + Headless UI
- **Icons:** Heroicons
- **Charts:** Chart.js
- **WebSocket:** native WebSocket API
- **HTTP:** Axios

### Features do Frontend
1. **Login & Auth**
   - Token input
   - Persistent login (localStorage)
   - Auto-logout on 401

2. **Dashboard**
   - Cards com estatísticas
   - Gráficos de uso
   - Quick actions

3. **HPA Editor**
   - Form fields para todos os valores
   - Validação em tempo real
   - Preview de mudanças
   - Rollout toggles
   - Resources editor

4. **Node Pool Editor**
   - Manual vs Autoscaling toggle
   - Node count slider
   - Min/Max inputs
   - Sequential execution marker

5. **Batch Operations**
   - Seleção múltipla (checkboxes)
   - Preview de mudanças em lote
   - Progress bars por item
   - Rollback automático em erro

6. **Sessions**
   - Template builder
   - Save/Load/Delete
   - Session browser
   - Preview antes de aplicar

7. **Real-time Updates**
   - WebSocket connection status
   - Live progress bars
   - Auto-refresh em mudanças
   - Notificações toast

8. **Logs Viewer**
   - Stream em tempo real
   - Filtros por nível
   - Search
   - Download

---

## 📋 Roadmap de Implementação

### Fase 1: Backend Core (Semana 1)
**Duração:** 5 dias

**Tasks:**
1. ✅ Refatorar handlers para suportar PUT/POST
2. ✅ Implementar HPAHandler.Update()
3. ✅ Implementar HPAHandler.UpdateResources()
4. ✅ Implementar HPAHandler.UpdateRollout()
5. ✅ Implementar HPAHandler.BatchUpdate()
6. ✅ Implementar NodePoolHandler (CRUD completo)
7. ✅ Implementar NodePoolHandler.Sequential()
8. ✅ Implementar CronJobHandler (CRUD)
9. ✅ Implementar PrometheusHandler (CRUD)
10. ✅ Implementar SessionHandler (CRUD)
11. ✅ Testes unitários de handlers

**Entregável:** API REST completa e testada

---

### Fase 2: WebSocket & Real-time (Semana 2)
**Duração:** 3 dias

**Tasks:**
1. ✅ Implementar WebSocket server
2. ✅ Broadcast de progress updates
3. ✅ Room management (por usuário)
4. ✅ Reconnection handling
5. ✅ Log streaming via SSE
6. ✅ Testes de WebSocket

**Entregável:** Real-time updates funcionando

---

### Fase 3: Frontend Setup (Semana 2)
**Duração:** 2 dias

**Tasks:**
1. ✅ Setup Vue.js 3 + Vite
2. ✅ Configurar Tailwind CSS
3. ✅ Setup Pinia stores
4. ✅ Setup Vue Router
5. ✅ Criar layout base
6. ✅ Implementar autenticação
7. ✅ Build system

**Entregável:** Estrutura frontend pronta

---

### Fase 4: Views Principais (Semana 3)
**Duração:** 5 dias

**Tasks:**
1. ✅ Dashboard view
2. ✅ Clusters view
3. ✅ HPAs list view
4. ✅ HPAs editor view (completo)
5. ✅ Node Pools view
6. ✅ Node Pool editor view
7. ✅ CronJobs view
8. ✅ Prometheus view
9. ✅ Sessions view

**Entregável:** Todas as telas principais

---

### Fase 5: Features Avançadas (Semana 4)
**Duração:** 5 dias

**Tasks:**
1. ✅ Batch operations UI
2. ✅ Progress tracking UI
3. ✅ WebSocket integration
4. ✅ Log viewer
5. ✅ Session templates
6. ✅ Notifications system
7. ✅ Error handling
8. ✅ Loading states

**Entregável:** Features avançadas completas

---

### Fase 6: Testes & Deploy (Semana 5)
**Duração:** 5 dias

**Tasks:**
1. ✅ Testes E2E (Playwright)
2. ✅ Testes de integração
3. ✅ Performance testing
4. ✅ Security audit
5. ✅ Documentação API (Swagger)
6. ✅ Documentação usuário
7. ✅ Docker setup
8. ✅ CI/CD pipeline
9. ✅ Release 1.0.0

**Entregável:** Versão 1.0.0 production-ready

---

## 🔧 Desenvolvimento Incremental

### Sprint 1 (Esta semana)
**Foco:** Edição de HPAs completa

**Entregáveis:**
- [ ] PUT /api/v1/hpas/:cluster/:namespace/:name
- [ ] PUT /api/v1/hpas/:cluster/:namespace/:name/resources
- [ ] PUT /api/v1/hpas/:cluster/:namespace/:name/rollout
- [ ] Frontend: HPA Editor component
- [ ] Frontend: Form validation
- [ ] Frontend: Preview de mudanças

**Critério de aceitação:**
- Usuário pode editar todos os campos de um HPA
- Validações impedem valores inválidos
- Preview mostra antes/depois
- Apply button executa mudança
- Feedback de sucesso/erro

---

### Sprint 2 (Semana 2)
**Foco:** Node Pools + Batch

**Entregáveis:**
- [ ] NodePool handlers completos
- [ ] Batch operations
- [ ] Sequential execution
- [ ] Progress tracking
- [ ] Frontend completo

---

### Sprint 3 (Semana 3)
**Foco:** CronJobs + Prometheus + Sessions

**Entregáveis:**
- [ ] CronJob management
- [ ] Prometheus management
- [ ] Session CRUD
- [ ] Templates
- [ ] Frontend completo

---

### Sprint 4 (Semana 4)
**Foco:** WebSocket + Real-time

**Entregáveis:**
- [ ] WebSocket server
- [ ] Real-time updates
- [ ] Log streaming
- [ ] Notifications
- [ ] Frontend integration

---

### Sprint 5 (Semana 5)
**Foco:** Testes + Deploy

**Entregáveis:**
- [ ] Testes E2E
- [ ] Documentação
- [ ] Docker
- [ ] CI/CD
- [ ] Release 1.0.0

---

## 📊 Métricas de Sucesso

### Funcionalidade
- ✅ 100% feature parity com TUI
- ✅ Todos endpoints testados
- ✅ Coverage > 80%

### Performance
- ✅ API response < 200ms (média)
- ✅ WebSocket latency < 50ms
- ✅ Frontend load < 2s

### UX
- ✅ Mobile-responsive
- ✅ Acessibilidade (WCAG 2.1)
- ✅ Dark mode
- ✅ Keyboard shortcuts

---

## 🎯 Começar Agora

### Primeira Task: HPA Editor Completo

**Backend:**
```bash
# 1. Criar handler de update
touch internal/web/handlers/hpa_update.go

# 2. Implementar validações
# 3. Integrar com kubernetes client
# 4. Adicionar routes
# 5. Testar com curl
```

**Frontend:**
```bash
# 1. Setup Vue.js project
cd web/
npm create vite@latest . -- --template vue
npm install

# 2. Instalar dependências
npm install vue-router pinia axios tailwindcss

# 3. Criar componente HPAEditor.vue
# 4. Implementar form
# 5. Integrar com API
```

---

**Pronto para começar?** 🚀

Sugestão: Começar pelo **HPA Editor** (backend + frontend) como primeiro entregável completo.
