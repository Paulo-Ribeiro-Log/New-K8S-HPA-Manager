# k8s-hpa-manager - Web Frontend

Interface web moderna construída com React, TypeScript, e shadcn/ui para gerenciamento de HPAs e Node Pools do Kubernetes.

## ⚠️ Requisitos

- **Node.js >= 18.0.0** (recomendado: LTS 20.x)
- **npm >= 8.0.0**

**Verificar versão:**
```bash
node --version  # Deve ser >= 18
npm --version   # Deve ser >= 8
```

**Se precisar atualizar:** Ver `ATUALIZAR_NODE.md` na raiz do projeto.

## 🚀 Quick Start

### Instalação

```bash
# Da raiz do projeto
make web-install

# Ou diretamente
cd internal/web/frontend
npm install
```

### Desenvolvimento

```bash
# Opção 1: Via Makefile (da raiz do projeto)
make web-dev

# Opção 2: Diretamente
cd internal/web/frontend
npm run dev
```

Isso iniciará:
- **Frontend dev server**: http://localhost:5173 (Vite com HMR)
- **Proxy API**: `/api/*` → http://localhost:8080 (backend Go)

**IMPORTANTE**: O backend Go deve estar rodando separadamente em http://localhost:8080

```bash
# Em outro terminal, inicie o backend
./build/k8s-hpa-manager web --port 8080
```

### Build para Produção

```bash
# Da raiz do projeto
make web-build
```

Build será gerado em `internal/web/static/` e embedado automaticamente no binário Go.

## 📁 Estrutura

```
internal/web/frontend/
├── src/
│   ├── components/           # Componentes React
│   │   ├── ui/              # shadcn/ui components
│   │   └── ...
│   ├── pages/               # Páginas
│   ├── lib/                 # Utilitários
│   └── App.tsx              # App root
├── vite.config.ts          # Config Vite
└── package.json
```

## 🛠️ Tech Stack

- React 18.3 + TypeScript 5.8
- Vite 5.4 + Tailwind CSS 3.4
- shadcn/ui + Radix UI
- React Query + React Router

## 📚 Documentação

Ver [Docs/README_WEB.md](../../../Docs/README_WEB.md) para mais detalhes.
