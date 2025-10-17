# Interface Web para k8s-hpa-manager

**Status:** Proposta de Design
**Data:** Outubro 2025
**Versão:** 1.0

---

## 📋 Índice

1. [Visão Geral](#visão-geral)
2. [Arquitetura](#arquitetura)
3. [Estrutura de Arquivos](#estrutura-de-arquivos)
4. [Stack Tecnológica](#stack-tecnológica)
5. [API REST](#api-rest)
6. [Interface do Usuário](#interface-do-usuário)
7. [Autenticação e Segurança](#autenticação-e-segurança)
8. [Real-time Updates](#real-time-updates)
9. [Impacto na Aplicação Existente](#impacto-na-aplicação-existente)
10. [Roadmap de Implementação](#roadmap-de-implementação)
11. [Exemplos de Código](#exemplos-de-código)

---

## Visão Geral

### Objetivo

Criar uma interface web **complementar** ao TUI existente, permitindo acesso via navegador sem modificar a lógica de negócio atual.

### Conceito

```
┌─────────────────────────────────────────────────────────────┐
│                    k8s-hpa-manager                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐          ┌──────────────┐               │
│  │   TUI Mode   │          │   Web Mode   │               │
│  │              │          │              │               │
│  │  Bubble Tea  │          │  HTTP Server │               │
│  │  Terminal UI │          │  REST API    │               │
│  │              │          │  WebSocket   │               │
│  └──────┬───────┘          └──────┬───────┘               │
│         │                         │                        │
│         └─────────┬───────────────┘                        │
│                   │                                        │
│         ┌─────────▼─────────┐                             │
│         │  Business Logic   │                             │
│         │  (compartilhada)  │                             │
│         ├───────────────────┤                             │
│         │ • kubernetes/     │                             │
│         │ • config/         │                             │
│         │ • session/        │                             │
│         │ • azure/          │                             │
│         │ • models/         │                             │
│         └───────────────────┘                             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Modo de Operação: Exclusivo

**Recomendação:** Modo exclusivo (TUI **ou** Web, não ambos simultaneamente)

```bash
# Modo TUI (padrão - existente)
k8s-hpa-manager

# Modo Web (novo)
k8s-hpa-manager web --port 8080

# Ou com variável de ambiente
K8S_HPA_WEB_TOKEN=secret123 k8s-hpa-manager web --port 8080
```

**Vantagens do modo exclusivo:**
- ✅ Sem conflito entre TUI e Web
- ✅ Implementação mais simples
- ✅ Melhor isolamento de código
- ✅ Estado único (não precisa sincronização)

---

## Arquitetura

### Princípios de Design

1. **Zero Impacto no TUI** - Código web completamente isolado
2. **Reutilizar Lógica** - Usar `kubernetes/`, `config/`, `session/`, `models/` existentes
3. **RESTful API** - Endpoints claros e semânticos
4. **Real-time** - WebSocket para updates de progresso
5. **Segurança First** - Autenticação obrigatória

### Camadas da Aplicação Web

```
┌─────────────────────────────────────────┐
│         Frontend (Browser)              │
│  Vue.js 3 + TailwindCSS + WebSocket    │
└─────────────────┬───────────────────────┘
                  │ HTTP/WS
┌─────────────────▼───────────────────────┐
│       Backend (Go HTTP Server)          │
│  Gin Framework + WebSocket Handler      │
├─────────────────────────────────────────┤
│         Middleware Layer                │
│  Auth | CORS | Logging | Rate Limit    │
├─────────────────────────────────────────┤
│          REST API Handlers              │
│  Clusters | HPAs | Nodes | Sessions    │
├─────────────────────────────────────────┤
│       Business Logic (Existente)        │
│  kubernetes/ | config/ | session/      │
└─────────────────────────────────────────┘
```

---

## Estrutura de Arquivos

### Estrutura Proposta (sem modificar código existente)

```
k8s-hpa-manager/
├── cmd/
│   ├── root.go              # Modificar: adicionar flag --web
│   ├── version.go           # Existente
│   └── web.go               # NOVO - comando web
│
├── internal/
│   ├── tui/                 # Existente - NÃO MEXER
│   ├── kubernetes/          # Existente - REUTILIZAR
│   ├── config/              # Existente - REUTILIZAR
│   ├── session/             # Existente - REUTILIZAR
│   ├── models/              # Existente - REUTILIZAR
│   ├── azure/               # Existente - REUTILIZAR
│   ├── logs/                # Existente - REUTILIZAR
│   ├── updater/             # Existente - REUTILIZAR
│   │
│   └── web/                 # NOVO - Interface Web
│       ├── server.go        # HTTP server setup
│       ├── router.go        # Routes definition
│       ├── middleware.go    # Auth, CORS, logging
│       ├── handlers/        # REST API handlers
│       │   ├── clusters.go
│       │   ├── namespaces.go
│       │   ├── hpas.go
│       │   ├── nodepools.go
│       │   ├── sessions.go
│       │   ├── cronjobs.go
│       │   └── prometheus.go
│       ├── websocket/       # WebSocket handlers
│       │   ├── logs.go
│       │   └── progress.go
│       └── static/          # Frontend assets (embedded)
│           ├── index.html
│           ├── assets/
│           │   ├── app.js
│           │   └── styles.css
│           └── favicon.ico
│
├── web/                     # NOVO - Frontend source (desenvolvimento)
│   ├── src/
│   │   ├── main.js
│   │   ├── App.vue
│   │   ├── components/
│   │   │   ├── ClusterSelector.vue
│   │   │   ├── NamespaceList.vue
│   │   │   ├── HPAList.vue
│   │   │   ├── HPAEditor.vue
│   │   │   ├── NodePoolList.vue
│   │   │   ├── SessionManager.vue
│   │   │   ├── CronJobList.vue
│   │   │   ├── PrometheusStack.vue
│   │   │   └── StatusLog.vue
│   │   ├── stores/
│   │   │   ├── clusters.js
│   │   │   ├── hpas.js
│   │   │   └── sessions.js
│   │   ├── services/
│   │   │   ├── api.js
│   │   │   └── websocket.js
│   │   └── router/
│   │       └── index.js
│   ├── package.json
│   ├── vite.config.js
│   └── tailwind.config.js
│
├── WEB_INTERFACE_DESIGN.md  # Este documento
├── CLAUDE.md                # Documentação existente
├── README.md                # Documentação principal
└── makefile                 # Adicionar targets web
```

### Build Process

```makefile
# Adicionar ao makefile existente

# Build frontend
.PHONY: web-build
web-build:
	@echo "Building frontend..."
	cd web && npm install && npm run build
	@echo "✅ Frontend built to internal/web/static/"

# Build completo (TUI + Web)
.PHONY: build-all-web
build-all-web: web-build build
	@echo "✅ Complete build with web interface"

# Dev mode (hot reload)
.PHONY: web-dev
web-dev:
	cd web && npm run dev
```

---

## Stack Tecnológica

### Backend: Go

**Framework HTTP:** [Gin](https://github.com/gin-gonic/gin)
```go
import "github.com/gin-gonic/gin"
```

**Alternativas consideradas:**
- Fiber (mais rápido, mas menos maduro)
- Echo (similar ao Gin)
- Chi (minimalista)

**Justificativa Gin:**
- ✅ Performance excelente
- ✅ Comunidade grande
- ✅ Middleware rico
- ✅ Documentação completa

**WebSocket:** [Gorilla WebSocket](https://github.com/gorilla/websocket)
```go
import "github.com/gorilla/websocket"
```

**Embed Frontend:** Go 1.16+ embed
```go
//go:embed static/*
var staticFiles embed.FS
```

### Frontend: Vue.js 3

**Framework:** [Vue.js 3](https://vuejs.org/) (Composition API)
**UI:** [TailwindCSS](https://tailwindcss.com/)
**Build:** [Vite](https://vitejs.dev/)
**State:** [Pinia](https://pinia.vuejs.org/)
**Router:** [Vue Router](https://router.vuejs.org/)

**Alternativas consideradas:**
- React + shadcn/ui (mais verboso)
- Svelte (menos maduro)

**Justificativa Vue.js 3:**
- ✅ Reatividade nativa
- ✅ Composition API moderna
- ✅ Bundle size pequeno
- ✅ Curva de aprendizado suave
- ✅ Excelente para dashboards

### Dependências Adicionais

```go
// go.mod adições
require (
    github.com/gin-gonic/gin v1.10.0
    github.com/gorilla/websocket v1.5.1
    github.com/gin-contrib/cors v1.5.0
)
```

```json
// web/package.json
{
  "dependencies": {
    "vue": "^3.4.0",
    "vue-router": "^4.2.0",
    "pinia": "^2.1.0",
    "axios": "^1.6.0"
  },
  "devDependencies": {
    "vite": "^5.0.0",
    "tailwindcss": "^3.4.0",
    "@vitejs/plugin-vue": "^5.0.0"
  }
}
```

---

## API REST

### Endpoints Principais

#### Clusters

```
GET    /api/v1/clusters
Response: [
  {
    "name": "akspriv-prod-01",
    "context": "akspriv-prod-01-admin",
    "status": "connected",
    "namespaceCount": 15
  }
]

GET    /api/v1/clusters/:name/test
Response: {
  "status": "connected",
  "latency": "45ms"
}
```

#### Namespaces

```
GET    /api/v1/namespaces?cluster=akspriv-prod-01&showSystem=false
Response: [
  {
    "name": "ingress-nginx",
    "cluster": "akspriv-prod-01",
    "hpaCount": 3,
    "selected": false
  }
]
```

#### HPAs

```
GET    /api/v1/hpas?cluster=akspriv-prod-01&namespace=ingress-nginx
Response: [
  {
    "name": "nginx-ingress-controller",
    "namespace": "ingress-nginx",
    "cluster": "akspriv-prod-01",
    "minReplicas": 2,
    "maxReplicas": 12,
    "targetCPU": 70,
    "targetMemory": 80,
    "currentReplicas": 5,
    "deploymentName": "nginx-ingress-controller",
    "resources": {
      "cpuRequest": "100m",
      "memoryRequest": "180Mi",
      "cpuLimit": "200m",
      "memoryLimit": "256Mi"
    }
  }
]

GET    /api/v1/hpas/:cluster/:namespace/:name
Response: { /* detalhes completos */ }

PUT    /api/v1/hpas/:cluster/:namespace/:name
Body: {
  "minReplicas": 3,
  "maxReplicas": 15,
  "targetCPU": 80,
  "resources": {
    "cpuRequest": "150m",
    "memoryRequest": "256Mi"
  }
}
Response: {
  "success": true,
  "message": "HPA updated successfully"
}

POST   /api/v1/hpas/:cluster/:namespace/:name/rollout
Response: {
  "success": true,
  "jobId": "rollout-123",
  "message": "Rollout started"
}
```

#### Node Pools

```
GET    /api/v1/nodepools?cluster=akspriv-prod-01
Response: [
  {
    "name": "monitoring-1",
    "cluster": "akspriv-prod-01",
    "nodeCount": 3,
    "minCount": 2,
    "maxCount": 5,
    "autoscalingEnabled": true,
    "vmSize": "Standard_D4s_v3"
  }
]

PUT    /api/v1/nodepools/:cluster/:name
Body: {
  "nodeCount": 5,
  "autoscalingEnabled": true,
  "minCount": 3,
  "maxCount": 8
}
Response: {
  "success": true,
  "jobId": "nodepool-456"
}
```

#### Sessions

```
GET    /api/v1/sessions
Response: [
  {
    "name": "upscale_prod_2025-10-15",
    "type": "hpa-upscale",
    "timestamp": "2025-10-15T14:30:00Z",
    "itemCount": 15
  }
]

GET    /api/v1/sessions/:name
Response: {
  "name": "upscale_prod_2025-10-15",
  "type": "hpa-upscale",
  "hpas": [ /* ... */ ],
  "nodePools": [ /* ... */ ]
}

POST   /api/v1/sessions
Body: {
  "name": "my-session",
  "type": "hpa-upscale",
  "hpas": [ /* ... */ ]
}
Response: {
  "success": true,
  "sessionName": "my-session"
}

POST   /api/v1/sessions/:name/apply
Response: {
  "success": true,
  "jobId": "session-apply-789",
  "itemsApplied": 15
}
```

#### CronJobs

```
GET    /api/v1/cronjobs?cluster=akspriv-prod-01&namespace=default
Response: [
  {
    "name": "backup-job",
    "namespace": "default",
    "schedule": "0 2 * * *",
    "scheduleText": "Executa todo dia às 2:00 AM",
    "suspend": false,
    "lastSchedule": "2025-10-15T02:00:00Z",
    "status": "active"
  }
]

PUT    /api/v1/cronjobs/:cluster/:namespace/:name
Body: {
  "suspend": true
}
Response: {
  "success": true
}
```

#### Prometheus Stack

```
GET    /api/v1/prometheus?cluster=akspriv-prod-01
Response: [
  {
    "name": "prometheus-server",
    "namespace": "monitoring",
    "workloadType": "StatefulSet",
    "replicas": 2,
    "resources": {
      "cpuRequest": "1",
      "memoryRequest": "8Gi",
      "cpuLimit": "2",
      "memoryLimit": "12Gi"
    },
    "metrics": {
      "cpuUsage": "264m",
      "memoryUsage": "3918Mi"
    }
  }
]

PUT    /api/v1/prometheus/:cluster/:namespace/:name
Body: {
  "resources": {
    "cpuRequest": "2",
    "memoryRequest": "12Gi"
  }
}
Response: {
  "success": true
}
```

#### WebSocket Endpoints

```
WS     /ws/logs
       -> Envia logs em tempo real

WS     /ws/progress
       -> Envia progresso de operações (rollouts, node pools)
```

### Estrutura de Erro Padrão

```json
{
  "success": false,
  "error": {
    "code": "CLUSTER_UNAVAILABLE",
    "message": "Cluster akspriv-prod-01 is not accessible",
    "details": "VPN connection required"
  }
}
```

### Rate Limiting

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1699999999
```

---

## Interface do Usuário

### Layout Principal

```
┌────────────────────────────────────────────────────────────┐
│ k8s-hpa-manager [Web]           [akspriv-prod-01] [user] │
├────────────────────────────────────────────────────────────┤
│ [Clusters] [HPAs] [Node Pools] [Sessions] [CronJobs] [⚙️] │
├────────┬───────────────────────────────────────────────────┤
│        │                                                   │
│ Side   │              Main Content Area                   │
│ Panel  │                                                   │
│        │                                                   │
│ ├──┤   │  ┌─────────────────────────────────────────┐    │
│ Nav    │  │                                          │    │
│ Items  │  │        Content específico da página      │    │
│        │  │                                          │    │
│        │  └─────────────────────────────────────────┘    │
│        │                                                   │
├────────┴───────────────────────────────────────────────────┤
│ 📊 Status: ✅ Connected | 🔄 3 operations running        │
└────────────────────────────────────────────────────────────┘
```

### Páginas Principais

#### 1. Dashboard (/)

```vue
<template>
  <div class="dashboard">
    <div class="stats-grid">
      <StatCard title="Clusters" :value="clusterCount" icon="🏢" />
      <StatCard title="HPAs" :value="hpaCount" icon="⚖️" />
      <StatCard title="Node Pools" :value="nodePoolCount" icon="🔧" />
      <StatCard title="Sessions" :value="sessionCount" icon="💾" />
    </div>

    <div class="recent-activity">
      <h2>Recent Activity</h2>
      <ActivityList :items="recentActivity" />
    </div>
  </div>
</template>
```

#### 2. HPAs (/hpas)

```vue
<template>
  <div class="hpa-page">
    <!-- Filters -->
    <div class="filters">
      <ClusterSelector v-model="selectedCluster" />
      <NamespaceFilter v-model="selectedNamespaces" />
      <SearchInput v-model="searchQuery" />
    </div>

    <!-- HPA List -->
    <HPATable
      :hpas="filteredHPAs"
      @edit="openEditor"
      @rollout="triggerRollout"
    />

    <!-- Editor Modal -->
    <HPAEditor
      v-if="editorOpen"
      :hpa="selectedHPA"
      @save="saveHPA"
      @close="editorOpen = false"
    />
  </div>
</template>
```

**Exemplo de HPA Table:**

```
┌──────────────────────────────────────────────────────────────────┐
│ HPAs - ingress-nginx                            [+ Edit] [Save]  │
├──────────────────────────────────────────────────────────────────┤
│ ☐ Name                    Min Max CPU% Mem% Resources  Rollout  │
├──────────────────────────────────────────────────────────────────┤
│ ☑ nginx-ingress          [ 2] [12] [70] [80] 100m/180Mi   🔄    │
│ ☐ cert-manager           [ 1] [ 3] [80] [-]  50m/90Mi     ⏸️    │
│ ☐ monitoring-prometheus  [ 2] [ 8] [60] [70] 200m/512Mi   🔄    │
└──────────────────────────────────────────────────────────────────┘
```

#### 3. Node Pools (/nodepools)

```vue
<template>
  <div class="nodepool-page">
    <div class="cluster-selector">
      <ClusterSelector v-model="selectedCluster" />
    </div>

    <NodePoolTable
      :nodePools="nodePools"
      @edit="openEditor"
    />

    <NodePoolEditor
      v-if="editorOpen"
      :nodePool="selectedNodePool"
      @save="saveNodePool"
      @close="editorOpen = false"
    />
  </div>
</template>
```

#### 4. Sessions (/sessions)

```vue
<template>
  <div class="session-page">
    <div class="session-actions">
      <button @click="createSession">+ New Session</button>
      <button @click="loadSession">📂 Load</button>
    </div>

    <SessionList
      :sessions="sessions"
      @load="loadSession"
      @apply="applySession"
      @delete="deleteSession"
    />

    <SessionDetails
      v-if="currentSession"
      :session="currentSession"
    />
  </div>
</template>
```

#### 5. CronJobs (/cronjobs)

```vue
<template>
  <div class="cronjob-page">
    <div class="filters">
      <ClusterSelector v-model="selectedCluster" />
      <NamespaceFilter v-model="selectedNamespaces" />
    </div>

    <CronJobTable
      :cronJobs="cronJobs"
      @toggle="toggleSuspend"
    />
  </div>
</template>
```

#### 6. Prometheus Stack (/prometheus)

```vue
<template>
  <div class="prometheus-page">
    <ClusterSelector v-model="selectedCluster" />

    <PrometheusResourceTable
      :resources="prometheusResources"
      @edit="openEditor"
    />

    <ResourceEditor
      v-if="editorOpen"
      :resource="selectedResource"
      @save="saveResource"
    />
  </div>
</template>
```

### Componentes Reutilizáveis

```vue
<!-- ClusterSelector.vue -->
<template>
  <select v-model="selectedCluster" class="cluster-select">
    <option v-for="cluster in clusters" :key="cluster.name">
      {{ cluster.name }}
      <span class="status" :class="cluster.status">●</span>
    </option>
  </select>
</template>

<!-- StatusLog.vue -->
<template>
  <div class="status-log">
    <div v-for="log in logs" :key="log.id" :class="log.type">
      <span class="icon">{{ log.icon }}</span>
      <span class="message">{{ log.message }}</span>
      <span class="time">{{ log.timestamp }}</span>
    </div>
  </div>
</template>

<!-- ProgressBar.vue -->
<template>
  <div class="progress-bar">
    <div class="progress-fill" :style="{ width: `${progress}%` }">
      <span>{{ progress }}%</span>
    </div>
  </div>
</template>
```

### Design System (TailwindCSS)

```javascript
// tailwind.config.js
module.exports = {
  theme: {
    extend: {
      colors: {
        primary: '#3B82F6',    // Blue
        success: '#10B981',    // Green
        warning: '#F59E0B',    // Orange
        error: '#EF4444',      // Red
        dark: '#1F2937',       // Dark gray
        light: '#F9FAFB',      // Light gray
      }
    }
  }
}
```

---

## Autenticação e Segurança

### Opção Recomendada: Bearer Token

```bash
# Gerar token
export K8S_HPA_WEB_TOKEN=$(openssl rand -hex 32)

# Iniciar servidor
K8S_HPA_WEB_TOKEN=$K8S_HPA_WEB_TOKEN k8s-hpa-manager web --port 8080
```

**Frontend:**
```javascript
// src/services/api.js
import axios from 'axios';

const api = axios.create({
  baseURL: '/api/v1',
  headers: {
    'Authorization': `Bearer ${localStorage.getItem('token')}`
  }
});

export default api;
```

**Backend:**
```go
// internal/web/middleware.go
func AuthMiddleware(token string) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")

        if authHeader == "" {
            c.JSON(401, gin.H{"error": "No authorization header"})
            c.Abort()
            return
        }

        parts := strings.Split(authHeader, " ")
        if len(parts) != 2 || parts[0] != "Bearer" {
            c.JSON(401, gin.H{"error": "Invalid authorization format"})
            c.Abort()
            return
        }

        if parts[1] != token {
            c.JSON(401, gin.H{"error": "Invalid token"})
            c.Abort()
            return
        }

        c.Next()
    }
}
```

### Login Page

```vue
<template>
  <div class="login-page">
    <div class="login-card">
      <h1>k8s-hpa-manager</h1>
      <form @submit.prevent="login">
        <input
          v-model="token"
          type="password"
          placeholder="Access Token"
          class="token-input"
        />
        <button type="submit">Login</button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import api from '@/services/api';

const token = ref('');
const router = useRouter();

const login = async () => {
  localStorage.setItem('token', token.value);

  // Testar token
  try {
    await api.get('/clusters');
    router.push('/');
  } catch (error) {
    alert('Invalid token');
    localStorage.removeItem('token');
  }
};
</script>
```

### CORS Configuration

```go
// internal/web/server.go
import "github.com/gin-contrib/cors"

func setupCORS(router *gin.Engine) {
    config := cors.DefaultConfig()
    config.AllowOrigins = []string{"http://localhost:5173"} // Dev
    config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE"}
    config.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}

    router.Use(cors.New(config))
}
```

### Rate Limiting

```go
// internal/web/middleware.go
import "github.com/ulule/limiter/v3"

func RateLimitMiddleware() gin.HandlerFunc {
    rate := limiter.Rate{
        Period: 1 * time.Minute,
        Limit:  100,
    }

    store := memory.NewStore()
    limiter := limiter.New(store, rate)

    return func(c *gin.Context) {
        context, err := limiter.Get(c, c.ClientIP())
        if err != nil {
            c.JSON(500, gin.H{"error": "Rate limiter error"})
            c.Abort()
            return
        }

        c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", context.Limit))
        c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", context.Remaining))
        c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", context.Reset))

        if context.Reached {
            c.JSON(429, gin.H{"error": "Rate limit exceeded"})
            c.Abort()
            return
        }

        c.Next()
    }
}
```

---

## Real-time Updates

### WebSocket para Logs

**Backend:**
```go
// internal/web/websocket/logs.go
package websocket

import (
    "github.com/gin-gonic/gin"
    "github.com/gorilla/websocket"
    "k8s-hpa-manager/internal/logs"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func HandleLogsWebSocket(c *gin.Context) {
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    // Criar subscriber para logs
    logChan := make(chan logs.LogEntry, 100)
    logs.Subscribe(logChan)
    defer logs.Unsubscribe(logChan)

    // Enviar logs em tempo real
    for log := range logChan {
        if err := conn.WriteJSON(log); err != nil {
            break
        }
    }
}
```

**Frontend:**
```javascript
// src/services/websocket.js
export class LogWebSocket {
  constructor() {
    this.ws = null;
    this.listeners = [];
  }

  connect() {
    const token = localStorage.getItem('token');
    this.ws = new WebSocket(`ws://localhost:8080/ws/logs?token=${token}`);

    this.ws.onmessage = (event) => {
      const log = JSON.parse(event.data);
      this.listeners.forEach(fn => fn(log));
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
    };

    this.ws.onclose = () => {
      // Reconectar após 3 segundos
      setTimeout(() => this.connect(), 3000);
    };
  }

  onLog(callback) {
    this.listeners.push(callback);
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
    }
  }
}
```

**Componente Vue:**
```vue
<template>
  <div class="log-viewer">
    <div v-for="log in logs" :key="log.id" :class="log.level">
      {{ log.timestamp }} | {{ log.message }}
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import { LogWebSocket } from '@/services/websocket';

const logs = ref([]);
const ws = new LogWebSocket();

onMounted(() => {
  ws.connect();
  ws.onLog((log) => {
    logs.value.push(log);
    if (logs.value.length > 1000) {
      logs.value.shift(); // Manter apenas últimas 1000 linhas
    }
  });
});

onUnmounted(() => {
  ws.disconnect();
});
</script>
```

### WebSocket para Progresso

**Backend:**
```go
// internal/web/websocket/progress.go
type ProgressUpdate struct {
    JobID    string  `json:"jobId"`
    Type     string  `json:"type"`     // "hpa", "nodepool", "rollout"
    Resource string  `json:"resource"`
    Progress int     `json:"progress"` // 0-100
    Status   string  `json:"status"`   // "running", "completed", "failed"
    Message  string  `json:"message"`
}

func HandleProgressWebSocket(c *gin.Context) {
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    progressChan := make(chan ProgressUpdate, 100)
    progress.Subscribe(progressChan)
    defer progress.Unsubscribe(progressChan)

    for update := range progressChan {
        if err := conn.WriteJSON(update); err != nil {
            break
        }
    }
}
```

**Frontend:**
```vue
<template>
  <div class="progress-monitor">
    <div v-for="job in activeJobs" :key="job.jobId" class="job-card">
      <h3>{{ job.resource }}</h3>
      <ProgressBar :progress="job.progress" />
      <p>{{ job.message }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { ProgressWebSocket } from '@/services/websocket';

const activeJobs = ref([]);
const ws = new ProgressWebSocket();

onMounted(() => {
  ws.connect();
  ws.onProgress((update) => {
    const index = activeJobs.value.findIndex(j => j.jobId === update.jobId);

    if (index >= 0) {
      // Atualizar job existente
      activeJobs.value[index] = update;

      // Remover se completado
      if (update.status === 'completed' || update.status === 'failed') {
        setTimeout(() => {
          activeJobs.value.splice(index, 1);
        }, 3000);
      }
    } else {
      // Adicionar novo job
      activeJobs.value.push(update);
    }
  });
});
</script>
```

---

## Impacto na Aplicação Existente

### ✅ Modificações Mínimas (Recomendado)

#### 1. cmd/root.go - Adicionar flag

```go
// Adicionar ao init()
rootCmd.PersistentFlags().BoolVar(&webMode, "web", false,
    "Run in web mode instead of TUI")
rootCmd.PersistentFlags().IntVar(&webPort, "port", 8080,
    "Web server port (only with --web)")
```

#### 2. cmd/web.go - Novo comando (NOVO ARQUIVO)

```go
package cmd

import (
    "k8s-hpa-manager/internal/web"
    "github.com/spf13/cobra"
)

var webCmd = &cobra.Command{
    Use:   "web",
    Short: "Start web interface",
    Long: `Start k8s-hpa-manager in web mode with HTTP API and browser interface.

Example:
  k8s-hpa-manager web --port 8080
  K8S_HPA_WEB_TOKEN=secret k8s-hpa-manager web`,
    RunE: func(cmd *cobra.Command, args []string) error {
        server, err := web.NewServer(kubeconfig, webPort, debug)
        if err != nil {
            return err
        }
        return server.Start()
    },
}

func init() {
    rootCmd.AddCommand(webCmd)
}
```

#### 3. internal/web/ - Todo código web isolado (NOVO PACKAGE)

**Zero modificações em:**
- ❌ `internal/tui/` - mantém intacto
- ❌ `internal/models/` - apenas reutilizado
- ❌ `internal/kubernetes/` - apenas reutilizado
- ❌ `internal/config/` - apenas reutilizado
- ❌ `internal/session/` - apenas reutilizado

### Princípio de Isolamento

```
┌─────────────────────────────────────────┐
│         Código Existente                │
│  (NÃO MODIFICAR - apenas reutilizar)    │
├─────────────────────────────────────────┤
│ internal/tui/        ← TUI isolado      │
│ internal/kubernetes/ ← Reutilizar       │
│ internal/config/     ← Reutilizar       │
│ internal/session/    ← Reutilizar       │
│ internal/models/     ← Reutilizar       │
│ internal/azure/      ← Reutilizar       │
│ internal/logs/       ← Reutilizar       │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│         Código Novo                     │
│  (adicionar sem tocar no existente)     │
├─────────────────────────────────────────┤
│ internal/web/        ← Web isolado      │
│ cmd/web.go           ← Comando novo     │
│ web/                 ← Frontend         │
└─────────────────────────────────────────┘
```

### Garantias de Não-Interferência

✅ **TUI continua funcionando exatamente igual**
✅ **Nenhuma lógica de negócio modificada**
✅ **Builds separados (make build vs make build-all-web)**
✅ **Testes não afetados**
✅ **Deploy independente**

---

## Roadmap de Implementação

### Fase 1: Fundação (Semana 1-2)

**Objetivos:**
- Setup básico do servidor HTTP
- API REST para clusters e namespaces
- Frontend básico com Vue.js

**Tarefas:**
```
[x] Criar estrutura internal/web/
[x] Implementar server.go com Gin
[x] Criar handlers básicos (clusters, namespaces)
[x] Setup frontend com Vite + Vue.js 3
[x] Implementar ClusterSelector component
[x] Implementar NamespaceList component
[x] Autenticação Bearer Token
[x] CORS configuration
```

**Deliverable:**
- Servidor HTTP funcional
- Listagem de clusters e namespaces no browser

### Fase 2: HPAs (Semana 3-4)

**Objetivos:**
- API completa para HPAs
- Interface de listagem e edição
- Aplicação de mudanças

**Tarefas:**
```
[ ] GET /api/v1/hpas endpoint
[ ] PUT /api/v1/hpas/:id endpoint
[ ] POST /api/v1/hpas/:id/rollout endpoint
[ ] HPAList component
[ ] HPAEditor component
[ ] Integração com kubernetes/client.go
[ ] Testes de edição de HPAs
```

**Deliverable:**
- Edição completa de HPAs via web

### Fase 3: Node Pools & Sessions (Semana 5-6)

**Objetivos:**
- Gerenciamento de Node Pools
- Sistema de sessões

**Tarefas:**
```
[ ] API endpoints para node pools
[ ] NodePoolList component
[ ] NodePoolEditor component
[ ] API endpoints para sessions
[ ] SessionManager component
[ ] Integração com session/manager.go
```

**Deliverable:**
- Node pools e sessões funcionais

### Fase 4: Features Avançadas (Semana 7-8)

**Objetivos:**
- CronJobs
- Prometheus Stack
- WebSocket real-time

**Tarefas:**
```
[ ] CronJob API + UI
[ ] Prometheus Stack API + UI
[ ] WebSocket para logs
[ ] WebSocket para progresso
[ ] Dashboard com estatísticas
```

**Deliverable:**
- Feature parity com TUI

### Fase 5: Produção (Semana 9-10)

**Objetivos:**
- Hardening, testes, documentação

**Tarefas:**
```
[ ] Rate limiting
[ ] Error handling robusto
[ ] Testes unitários (backend)
[ ] Testes E2E (frontend)
[ ] Documentação API (Swagger)
[ ] Docker image
[ ] Kubernetes deployment manifests
[ ] README web mode
```

**Deliverable:**
- Aplicação production-ready

### Cronograma

```
Semana 1-2:  Fundação + Setup
Semana 3-4:  HPAs completos
Semana 5-6:  Node Pools + Sessions
Semana 7-8:  Features avançadas
Semana 9-10: Produção + Deploy

Total: ~10 semanas (2.5 meses)
```

---

## Exemplos de Código

### Backend: Server Setup

```go
// internal/web/server.go
package web

import (
    "embed"
    "fmt"
    "io/fs"
    "net/http"
    "os"

    "github.com/gin-gonic/gin"
    "github.com/gin-contrib/cors"

    "k8s-hpa-manager/internal/config"
    "k8s-hpa-manager/internal/web/handlers"
    "k8s-hpa-manager/internal/web/middleware"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
    router      *gin.Engine
    kubeManager *config.KubeConfigManager
    port        int
    token       string
}

func NewServer(kubeconfig string, port int, debug bool) (*Server, error) {
    // Reutilizar gerenciador de kube existente
    kubeManager, err := config.NewKubeConfigManager(kubeconfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create kube manager: %w", err)
    }

    // Token de autenticação
    token := os.Getenv("K8S_HPA_WEB_TOKEN")
    if token == "" {
        return nil, fmt.Errorf("K8S_HPA_WEB_TOKEN environment variable is required")
    }

    // Setup Gin
    if !debug {
        gin.SetMode(gin.ReleaseMode)
    }
    router := gin.Default()

    server := &Server{
        router:      router,
        kubeManager: kubeManager,
        port:        port,
        token:       token,
    }

    server.setupMiddleware()
    server.setupRoutes()
    server.setupStatic()

    return server, nil
}

func (s *Server) setupMiddleware() {
    // CORS
    s.router.Use(cors.New(cors.Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: true,
    }))

    // Logging
    s.router.Use(gin.Logger())

    // Recovery
    s.router.Use(gin.Recovery())
}

func (s *Server) setupRoutes() {
    // Health check (sem auth)
    s.router.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })

    // API v1 (com auth)
    api := s.router.Group("/api/v1")
    api.Use(middleware.AuthMiddleware(s.token))

    // Clusters
    clusterHandler := handlers.NewClusterHandler(s.kubeManager)
    api.GET("/clusters", clusterHandler.List)
    api.GET("/clusters/:name/test", clusterHandler.Test)

    // Namespaces
    namespaceHandler := handlers.NewNamespaceHandler(s.kubeManager)
    api.GET("/namespaces", namespaceHandler.List)

    // HPAs
    hpaHandler := handlers.NewHPAHandler(s.kubeManager)
    api.GET("/hpas", hpaHandler.List)
    api.GET("/hpas/:cluster/:namespace/:name", hpaHandler.Get)
    api.PUT("/hpas/:cluster/:namespace/:name", hpaHandler.Update)
    api.POST("/hpas/:cluster/:namespace/:name/rollout", hpaHandler.Rollout)

    // Node Pools
    nodePoolHandler := handlers.NewNodePoolHandler(s.kubeManager)
    api.GET("/nodepools", nodePoolHandler.List)
    api.PUT("/nodepools/:cluster/:name", nodePoolHandler.Update)

    // Sessions
    sessionHandler := handlers.NewSessionHandler()
    api.GET("/sessions", sessionHandler.List)
    api.GET("/sessions/:name", sessionHandler.Get)
    api.POST("/sessions", sessionHandler.Create)
    api.POST("/sessions/:name/apply", sessionHandler.Apply)

    // WebSocket (com auth via query param)
    s.router.GET("/ws/logs", middleware.WSAuthMiddleware(s.token), handlers.HandleLogsWS)
    s.router.GET("/ws/progress", middleware.WSAuthMiddleware(s.token), handlers.HandleProgressWS)
}

func (s *Server) setupStatic() {
    // Servir arquivos estáticos do embed
    staticFS, _ := fs.Sub(staticFiles, "static")
    s.router.StaticFS("/assets", http.FS(staticFS))

    // SPA fallback - todas as rotas não-API servem index.html
    s.router.NoRoute(func(c *gin.Context) {
        data, _ := staticFiles.ReadFile("static/index.html")
        c.Data(200, "text/html; charset=utf-8", data)
    })
}

func (s *Server) Start() error {
    addr := fmt.Sprintf(":%d", s.port)
    fmt.Printf("🌐 k8s-hpa-manager web interface starting...\n")
    fmt.Printf("📍 Server: http://localhost%s\n", addr)
    fmt.Printf("🔐 Auth: Bearer token required\n")
    fmt.Printf("📝 API Docs: http://localhost%s/api/v1/docs\n\n", addr)

    return s.router.Run(addr)
}
```

### Backend: HPA Handler Example

```go
// internal/web/handlers/hpas.go
package handlers

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "k8s-hpa-manager/internal/config"
    "k8s-hpa-manager/internal/models"
)

type HPAHandler struct {
    kubeManager *config.KubeConfigManager
}

func NewHPAHandler(km *config.KubeConfigManager) *HPAHandler {
    return &HPAHandler{kubeManager: km}
}

func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    namespace := c.Query("namespace")

    if cluster == "" {
        c.JSON(400, gin.H{"error": "cluster parameter is required"})
        return
    }

    // Obter client do cluster (reutilizar código existente)
    client, err := h.kubeManager.GetClient(cluster)
    if err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to get client: %v", err)})
        return
    }

    kubeClient := kubernetes.NewClient(client, cluster)

    // Listar HPAs (reutilizar código existente)
    hpas, err := kubeClient.ListHPAs(c.Request.Context(), namespace)
    if err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to list HPAs: %v", err)})
        return
    }

    c.JSON(200, hpas)
}

func (h *HPAHandler) Update(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    name := c.Param("name")

    var hpa models.HPA
    if err := c.ShouldBindJSON(&hpa); err != nil {
        c.JSON(400, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
        return
    }

    // Validações
    if hpa.MinReplicas != nil && *hpa.MinReplicas < 1 {
        c.JSON(400, gin.H{"error": "minReplicas must be >= 1"})
        return
    }
    if hpa.MaxReplicas < 1 {
        c.JSON(400, gin.H{"error": "maxReplicas must be >= 1"})
        return
    }

    // Obter client
    client, err := h.kubeManager.GetClient(cluster)
    if err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to get client: %v", err)})
        return
    }

    kubeClient := kubernetes.NewClient(client, cluster)

    // Aplicar mudanças (reutilizar código existente)
    hpa.Name = name
    hpa.Namespace = namespace
    hpa.Cluster = cluster

    if err := kubeClient.UpdateHPA(c.Request.Context(), hpa); err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to update HPA: %v", err)})
        return
    }

    c.JSON(200, gin.H{
        "success": true,
        "message": fmt.Sprintf("HPA %s/%s updated successfully", namespace, name),
    })
}

func (h *HPAHandler) Rollout(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    name := c.Param("name")

    // Obter client
    client, err := h.kubeManager.GetClient(cluster)
    if err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to get client: %v", err)})
        return
    }

    kubeClient := kubernetes.NewClient(client, cluster)

    // Trigger rollout (reutilizar código existente)
    hpa := models.HPA{
        Name:           name,
        Namespace:      namespace,
        Cluster:        cluster,
        PerformRollout: true,
    }

    if err := kubeClient.TriggerRollout(c.Request.Context(), hpa); err != nil {
        c.JSON(500, gin.H{"error": fmt.Sprintf("failed to trigger rollout: %v", err)})
        return
    }

    c.JSON(200, gin.H{
        "success": true,
        "jobId":   fmt.Sprintf("rollout-%s-%s-%d", namespace, name, time.Now().Unix()),
        "message": "Rollout started successfully",
    })
}
```

### Frontend: Main App

```javascript
// web/src/main.js
import { createApp } from 'vue';
import { createPinia } from 'pinia';
import router from './router';
import App from './App.vue';
import './assets/styles.css';

const app = createApp(App);

app.use(createPinia());
app.use(router);

app.mount('#app');
```

```vue
<!-- web/src/App.vue -->
<template>
  <div id="app">
    <LoginPage v-if="!isAuthenticated" @login="handleLogin" />
    <MainLayout v-else>
      <router-view />
    </MainLayout>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import LoginPage from './components/LoginPage.vue';
import MainLayout from './components/MainLayout.vue';
import api from './services/api';

const isAuthenticated = ref(false);
const router = useRouter();

onMounted(async () => {
  const token = localStorage.getItem('token');
  if (token) {
    try {
      await api.get('/clusters');
      isAuthenticated.value = true;
    } catch {
      localStorage.removeItem('token');
    }
  }
});

const handleLogin = async (token) => {
  localStorage.setItem('token', token);
  isAuthenticated.value = true;
  router.push('/');
};
</script>
```

### Frontend: HPA List Component

```vue
<!-- web/src/components/HPAList.vue -->
<template>
  <div class="hpa-list">
    <div class="filters">
      <select v-model="selectedCluster" @change="loadHPAs">
        <option value="">Select Cluster</option>
        <option v-for="cluster in clusters" :key="cluster.name" :value="cluster.name">
          {{ cluster.name }}
        </option>
      </select>

      <input
        v-model="searchQuery"
        type="text"
        placeholder="Search HPAs..."
        class="search-input"
      />
    </div>

    <div v-if="loading" class="loading">
      Loading HPAs...
    </div>

    <table v-else class="hpa-table">
      <thead>
        <tr>
          <th>
            <input type="checkbox" @change="toggleAll" />
          </th>
          <th>Name</th>
          <th>Namespace</th>
          <th>Min</th>
          <th>Max</th>
          <th>CPU%</th>
          <th>Mem%</th>
          <th>Current</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="hpa in filteredHPAs" :key="hpa.name">
          <td>
            <input type="checkbox" v-model="hpa.selected" />
          </td>
          <td>{{ hpa.name }}</td>
          <td>{{ hpa.namespace }}</td>
          <td>{{ hpa.minReplicas }}</td>
          <td>{{ hpa.maxReplicas }}</td>
          <td>{{ hpa.targetCPU || '-' }}</td>
          <td>{{ hpa.targetMemory || '-' }}</td>
          <td>{{ hpa.currentReplicas }}</td>
          <td class="actions">
            <button @click="editHPA(hpa)" class="btn-edit">✏️</button>
            <button @click="rolloutHPA(hpa)" class="btn-rollout">🔄</button>
          </td>
        </tr>
      </tbody>
    </table>

    <HPAEditor
      v-if="editorOpen"
      :hpa="selectedHPA"
      @save="saveHPA"
      @close="editorOpen = false"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import api from '@/services/api';
import HPAEditor from './HPAEditor.vue';

const clusters = ref([]);
const hpas = ref([]);
const selectedCluster = ref('');
const searchQuery = ref('');
const loading = ref(false);
const editorOpen = ref(false);
const selectedHPA = ref(null);

const filteredHPAs = computed(() => {
  if (!searchQuery.value) return hpas.value;

  const query = searchQuery.value.toLowerCase();
  return hpas.value.filter(hpa =>
    hpa.name.toLowerCase().includes(query) ||
    hpa.namespace.toLowerCase().includes(query)
  );
});

onMounted(async () => {
  const { data } = await api.get('/clusters');
  clusters.value = data;
});

const loadHPAs = async () => {
  if (!selectedCluster.value) return;

  loading.value = true;
  try {
    const { data } = await api.get('/hpas', {
      params: { cluster: selectedCluster.value }
    });
    hpas.value = data.map(hpa => ({ ...hpa, selected: false }));
  } finally {
    loading.value = false;
  }
};

const editHPA = (hpa) => {
  selectedHPA.value = hpa;
  editorOpen.value = true;
};

const saveHPA = async (updatedHPA) => {
  await api.put(
    `/hpas/${updatedHPA.cluster}/${updatedHPA.namespace}/${updatedHPA.name}`,
    updatedHPA
  );

  editorOpen.value = false;
  await loadHPAs();
};

const rolloutHPA = async (hpa) => {
  if (!confirm(`Trigger rollout for ${hpa.namespace}/${hpa.name}?`)) return;

  await api.post(`/hpas/${hpa.cluster}/${hpa.namespace}/${hpa.name}/rollout`);
  alert('Rollout started successfully');
};

const toggleAll = (event) => {
  hpas.value.forEach(hpa => {
    hpa.selected = event.target.checked;
  });
};
</script>

<style scoped>
.hpa-list {
  padding: 20px;
}

.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
}

.search-input {
  flex: 1;
  padding: 8px;
  border: 1px solid #ddd;
  border-radius: 4px;
}

.hpa-table {
  width: 100%;
  border-collapse: collapse;
}

.hpa-table th,
.hpa-table td {
  padding: 12px;
  text-align: left;
  border-bottom: 1px solid #ddd;
}

.hpa-table th {
  background: #f5f5f5;
  font-weight: 600;
}

.actions {
  display: flex;
  gap: 8px;
}

.btn-edit,
.btn-rollout {
  padding: 6px 12px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
}

.btn-edit:hover,
.btn-rollout:hover {
  opacity: 0.8;
}
</style>
```

---

## Conclusão

Este documento define uma arquitetura completa para adicionar uma interface web ao k8s-hpa-manager **sem modificar a aplicação existente**.

### Próximos Passos

1. **Aprovar proposta** - Review e feedback deste documento
2. **Criar branch** - `git checkout -b feature/web-interface`
3. **Fase 1** - Implementar fundação (2 semanas)
4. **Review iterativo** - Validar após cada fase
5. **Deploy produção** - Após Fase 5 completa

### Perguntas em Aberto

- [ ] Preferência de framework frontend? (Vue.js, React, Svelte)
- [ ] Hospedagem separada ou binário único com embed?
- [ ] Necessidade de multi-tenancy ou single-user apenas?
- [ ] Deploy em Kubernetes ou VM tradicional?
- [ ] OAuth2/OIDC necessário ou Bearer Token suficiente?

### Referências

- [Gin Framework](https://github.com/gin-gonic/gin)
- [Vue.js 3](https://vuejs.org/)
- [TailwindCSS](https://tailwindcss.com/)
- [Gorilla WebSocket](https://github.com/gorilla/websocket)
- [Go Embed](https://pkg.go.dev/embed)

---

**Última atualização:** Outubro 2025
**Autor:** Design técnico para k8s-hpa-manager web interface
**Status:** 📝 Proposta - Aguardando aprovação
