# Integração Frontend React - Guia Completo

**Data:** 17 de Outubro de 2025  
**Status:** ✅ Estrutura Reorganizada e Integrada

---

## 📋 O Que Foi Feito

### 1. Reorganização de Arquivos

**Antes:**
```
new-web-page/              # Pasta externa (desorganizado)
├── src/
├── package.json
└── ...

internal/web/
├── handlers/
├── static/               # HTML estático antigo
└── server.go
```

**Depois:**
```
internal/web/
├── frontend/             # ✅ NOVO - App React organizado
│   ├── src/
│   ├── public/
│   ├── package.json
│   ├── vite.config.ts
│   └── README.md
├── static/              # Build output (embedado no Go)
│   └── .gitkeep
├── handlers/            # Go REST API
├── middleware/          # Auth, CORS
└── server.go           # Gin server
```

### 2. Configurações Atualizadas

#### Vite Config (`internal/web/frontend/vite.config.ts`)
```typescript
build: {
  outDir: "../static",        // Build direto para embed
  emptyOutDir: true,
  sourcemap: mode === "development",
  assetsInlineLimit: 4096,
}
```

#### .gitignore
```
# Web frontend
new-web-page/                    # Ignorar pasta antiga
internal/web/static/*            # Ignorar builds
!internal/web/static/.gitkeep    # Manter diretório
internal/web/frontend/node_modules/
internal/web/frontend/dist/
```

### 3. Makefile Targets

Novos comandos adicionados:

```makefile
make web-install    # Instalar npm dependencies
make web-dev        # Dev server (Vite HMR)
make web-build      # Build produção → static/
make web-clean      # Limpar build
make build-web      # Build completo (frontend + backend)
```

---

## 🚀 Workflows

### Desenvolvimento Local

**Terminal 1 - Backend Go:**
```bash
go build -o ./build/k8s-hpa-manager .
./build/k8s-hpa-manager web --port 8080
```

**Terminal 2 - Frontend React:**
```bash
make web-dev
# ou
cd internal/web/frontend && npm run dev
```

**Resultado:**
- Frontend: http://localhost:5173 (Vite dev server com HMR)
- Backend: http://localhost:8080 (Go API)
- Proxy: `/api/*` no frontend → backend automaticamente

### Build para Produção

```bash
# Opção 1: Build completo automático
make build-web

# Opção 2: Build manual passo a passo
make web-build    # 1. Build React → internal/web/static/
make build        # 2. Build Go (embeda static/)
```

**Resultado:**
- Frontend compilado em `internal/web/static/`
- Go binary em `build/k8s-hpa-manager` (com frontend embedado)
- Deploy: apenas 1 arquivo binário

### Executar Produção

```bash
./build/k8s-hpa-manager web --port 8080

# Acesse: http://localhost:8080
# Token: poc-token-123
```

---

## 📁 Estrutura Detalhada

### Frontend (`internal/web/frontend/`)

```
frontend/
├── src/
│   ├── components/
│   │   ├── ui/              # shadcn/ui components
│   │   │   ├── button.tsx
│   │   │   ├── card.tsx
│   │   │   └── ...
│   │   ├── Header.tsx       # App header
│   │   ├── HPAEditor.tsx    # HPA editor
│   │   ├── HPAListItem.tsx  # HPA list item
│   │   ├── SplitView.tsx    # Split panel layout
│   │   ├── StatsCard.tsx    # Dashboard stats
│   │   ├── TabNavigation.tsx
│   │   └── DashboardCharts.tsx
│   ├── pages/
│   │   ├── Index.tsx        # Main dashboard
│   │   └── NotFound.tsx     # 404 page
│   ├── hooks/
│   │   └── use-mobile.tsx   # Responsive hook
│   ├── lib/
│   │   └── utils.ts         # Utilities (cn, etc)
│   ├── App.tsx              # App root (Router)
│   ├── main.tsx             # Entry point
│   └── index.css            # Global styles (Tailwind)
├── public/
│   ├── favicon.ico
│   └── ...
├── package.json
├── vite.config.ts          # Vite configuration
├── tailwind.config.ts      # Tailwind config
├── tsconfig.json           # TypeScript config
└── README.md               # Frontend docs
```

### Backend (`internal/web/`)

```
internal/web/
├── handlers/
│   ├── clusters.go         # GET /api/v1/clusters
│   ├── namespaces.go       # GET /api/v1/namespaces
│   ├── hpas.go            # GET/PUT /api/v1/hpas
│   ├── nodepools.go       # GET /api/v1/nodepools
│   ├── cronjobs.go        # GET/PUT /api/v1/cronjobs
│   ├── prometheus.go      # GET/PUT /api/v1/prometheus
│   └── validation.go      # GET /api/v1/validate
├── middleware/
│   └── auth.go            # Bearer token auth
├── validators/
│   └── azure.go           # Azure/VPN validation
├── static/                # Frontend build output
│   └── .gitkeep
└── server.go              # Gin HTTP server
```

---

## 🔧 Dependências

### Frontend (npm)

**Core:**
- react@18.3.1
- react-dom@18.3.1
- react-router-dom@6.30.1

**Build:**
- vite@5.4.19
- typescript@5.8.3
- @vitejs/plugin-react-swc@3.11.0

**UI:**
- tailwindcss@3.4.17
- @radix-ui/* (40+ packages)
- lucide-react@0.462.0

**State/Forms:**
- @tanstack/react-query@5.83.0
- react-hook-form@7.61.1
- zod@3.25.76

**Charts:**
- recharts@2.15.4

### Backend (Go)

**Web:**
- github.com/gin-gonic/gin
- github.com/gin-contrib/cors

**Kubernetes:**
- k8s.io/client-go
- k8s.io/api

**Azure:**
- github.com/Azure/azure-sdk-for-go/sdk/azcore
- github.com/Azure/azure-sdk-for-go/sdk/azidentity

---

## 🎯 Próximos Passos

### Implementação Pendente (10%)

1. **API Client TypeScript**
   - Criar `src/lib/api.ts` com client HTTP
   - Auth manager (localStorage)
   - Type-safe endpoints

2. **Auth Provider React**
   - Context + hooks para autenticação
   - Login page funcional
   - Protected routes

3. **Conectar Componentes**
   - Substituir mock data por API real
   - Implementar CRUD de HPAs
   - Node Pools grid funcional
   - CronJobs management
   - Prometheus resources

4. **Features Avançadas**
   - Sessions management
   - Rollout triggers
   - Real-time updates (polling ou WebSocket)
   - Error boundaries
   - Loading states

---

## 📝 Comandos Úteis

```bash
# Desenvolvimento
make web-install              # Primeira vez
make web-dev                  # Dev server

# Build
make web-build                # Frontend only
make build                    # Go only
make build-web                # Completo

# Limpeza
make web-clean                # Limpar frontend build
make clean                    # Limpar Go build
rm -rf internal/web/frontend/node_modules  # Limpar deps

# Testes
cd internal/web/frontend
npm run lint                  # ESLint
npm run build:dev             # Build com sourcemaps
npm run preview               # Preview do build
```

---

## 🐛 Troubleshooting

### Frontend não compila

```bash
cd internal/web/frontend
rm -rf node_modules package-lock.json
npm install
npm run build
```

### Backend não encontra frontend

```bash
# Verificar se build existe
ls -la internal/web/static/

# Rebuild frontend
make web-build

# Rebuild Go
make build
```

### Porta em uso

```bash
# Frontend (5173)
lsof -ti:5173 | xargs kill -9

# Backend (8080)
lsof -ti:8080 | xargs kill -9
```

---

## ✅ Checklist de Integração

- [x] Mover arquivos para `internal/web/frontend/`
- [x] Configurar Vite build → `internal/web/static/`
- [x] Atualizar .gitignore
- [x] Criar Makefile targets
- [x] Atualizar documentação (CLAUDE.md)
- [x] README específico do frontend
- [ ] Implementar API client TypeScript
- [ ] Auth provider React
- [ ] Conectar todos componentes
- [ ] Testes E2E
- [ ] Screenshots/demos

---

## 📚 Referências

- Frontend README: `internal/web/frontend/README.md`
- Backend docs: `Docs/README_WEB.md`
- Arquitetura: `Docs/WEB_INTERFACE_DESIGN.md`
- CLAUDE.md: Seção "Interface Web"

---

**Estrutura limpa e organizada! 🚀**
**Pronto para desenvolvimento da integração API.**
