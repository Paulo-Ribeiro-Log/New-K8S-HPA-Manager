# Comandos de Desenvolvimento - k8s-hpa-manager

Guia de referência rápida com comandos úteis para desenvolvimento, debugging e troubleshooting.

---

## 📦 Build e Compilação

### Frontend (React/TypeScript)

```bash
# Instalar dependências
cd internal/web/frontend
npm install

# Desenvolvimento (dev server com HMR)
npm run dev
# Acesse: http://localhost:5173

# Build de produção
npm run build
# Output: internal/web/static/

# Limpeza de cache
rm -rf node_modules/.vite
rm -rf dist
```

### Backend (Go)

```bash
# Build padrão (injeta versão via git tags)
cd /home/paulo/Scripts/Scripts\ GO/Scale_HPA/Scale_HPA
go build -o build/k8s-hpa-manager

# Build com flags personalizadas
go build -ldflags="-X 'main.Version=v1.0.0'" -o build/k8s-hpa-manager

# Build para todas as plataformas
make release
# Output: build/k8s-hpa-manager-{linux,darwin,windows}-{amd64,arm64}

# Build rápido (sem otimizações)
go build -o build/k8s-hpa-manager

# Verificar versão compilada
./build/k8s-hpa-manager version
```

### Build Completo (Frontend + Backend)

```bash
# Script recomendado (evita cache issues)
./rebuild-web.sh -b

# Ou manual:
cd internal/web/frontend && npm run build
cd ../../..
go build -o build/k8s-hpa-manager
```

---

## 🚀 Executar Servidor

### Modo Foreground (logs no terminal)

```bash
./build/k8s-hpa-manager web -f
# Acesse: http://localhost:8080
# Ctrl+C para parar
```

### Modo Background (daemon)

```bash
./build/k8s-hpa-manager web
# Logs salvos em: /tmp/k8s-hpa-manager-web-*.log

# Ver logs em tempo real
tail -f /tmp/k8s-hpa-manager-web-*.log
```

### Porta Customizada

```bash
./build/k8s-hpa-manager web --port 9000 -f
# Acesse: http://localhost:9000
```

### Debug Mode

```bash
./build/k8s-hpa-manager web -f --debug
# Logs detalhados de requisições HTTP, engine, port-forwards, etc.
```

---

## 🔌 Gerenciamento de Portas

### Verificar Porta em Uso

```bash
# Verificar se porta 8080 está ocupada
lsof -i :8080
# Output: PID, comando, usuário

# Verificar múltiplas portas (8080, 55551-55556)
lsof -i :8080 -i :55551 -i :55552 -i :55553 -i :55554 -i :55555 -i :55556
```

### Matar Processo na Porta

```bash
# Matar processo na porta 8080
lsof -ti:8080 | xargs -r kill -9

# Matar TODOS os servidores k8s-hpa-manager
pkill -9 -f "k8s-hpa-manager web"
```

### Port-Forwards do Prometheus

```bash
# Verificar port-forwards ativos (55551-55556)
lsof -i :55551 -i :55552 -i :55553 -i :55554 -i :55555 -i :55556

# Detalhes de port-forward kubectl
ps aux | grep "kubectl port-forward"

# Matar port-forwards órfãos
pkill -9 -f "kubectl port-forward.*prometheus"
```

---

## 🗄️ SQLite - Monitoring Database

### Localização do Banco

```bash
~/.k8s-hpa-manager/monitoring.db
```

### Queries Úteis

#### Ver Snapshots Recentes

```bash
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT
    cluster,
    namespace,
    hpa_name,
    cpu_current,
    cpu_target,
    memory_current,
    memory_target,
    datetime(timestamp, 'unixepoch', 'localtime') as timestamp
FROM hpa_snapshots
ORDER BY timestamp DESC
LIMIT 10;
"
```

#### Contar Snapshots por HPA

```bash
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT
    cluster,
    namespace,
    hpa_name,
    COUNT(*) as total_snapshots,
    MIN(datetime(timestamp, 'unixepoch', 'localtime')) as first_snapshot,
    MAX(datetime(timestamp, 'unixepoch', 'localtime')) as last_snapshot
FROM hpa_snapshots
GROUP BY cluster, namespace, hpa_name
ORDER BY total_snapshots DESC;
"
```

#### Verificar Targets (CPU/Memory)

```bash
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT
    cluster,
    namespace,
    hpa_name,
    cpu_target,
    memory_target,
    COUNT(*) as count
FROM hpa_snapshots
WHERE cluster = 'akspriv-ofertalogistica-prd'
  AND namespace = 'retira-rapido-prd'
  AND hpa_name = 'retira-disponibilidade-api'
GROUP BY cpu_target, memory_target;
"
```

#### Atualizar Memory Target (bulk update)

```bash
# Atualizar todos os snapshots com memory_target=0 para 90
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
UPDATE hpa_snapshots
SET memory_target = 90
WHERE memory_target = 0
  AND cpu_target > 0;
"

# Verificar quantos foram atualizados
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT COUNT(*) as updated
FROM hpa_snapshots
WHERE memory_target = 90;
"
```

#### Ver Schema da Tabela

```bash
sqlite3 ~/.k8s-hpa-manager/monitoring.db ".schema hpa_snapshots"
```

#### Limpar Snapshots Antigos (>3 dias)

```bash
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
DELETE FROM hpa_snapshots
WHERE timestamp < (strftime('%s', 'now') - 259200);
-- 259200 = 3 dias em segundos
"

# Vacuum para liberar espaço
sqlite3 ~/.k8s-hpa-manager/monitoring.db "VACUUM;"
```

#### Tamanho do Banco de Dados

```bash
du -h ~/.k8s-hpa-manager/monitoring.db

# Estatísticas detalhadas
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT
    COUNT(*) as total_rows,
    COUNT(DISTINCT cluster) as clusters,
    COUNT(DISTINCT cluster || '/' || namespace || '/' || hpa_name) as unique_hpas,
    MIN(datetime(timestamp, 'unixepoch', 'localtime')) as oldest,
    MAX(datetime(timestamp, 'unixepoch', 'localtime')) as newest
FROM hpa_snapshots;
"
```

### Backup e Restore do SQLite

```bash
# Backup
sqlite3 ~/.k8s-hpa-manager/monitoring.db ".backup /tmp/monitoring-backup.db"

# Restore
sqlite3 ~/.k8s-hpa-manager/monitoring.db ".restore /tmp/monitoring-backup.db"

# Exportar para CSV
sqlite3 -header -csv ~/.k8s-hpa-manager/monitoring.db "
SELECT * FROM hpa_snapshots WHERE cluster = 'akspriv-prod'
" > snapshots.csv
```

---

## 🧪 Testing e Debugging

### Testar Compilação

```bash
# Verificar erros de sintaxe sem compilar
go vet ./...

# Executar testes unitários
make test

# Testes com cobertura
make test-coverage
# Output: coverage.html
```

### Debug do Frontend

```bash
# Console do navegador (F12)
# Verificar logs:
console.log("[MonitoringPage] ...")

# Verificar API calls
# Network tab → Filter: /api/v1

# React DevTools
# Components tab → Inspecionar props e state
```

### Debug do Backend

```bash
# Logs detalhados
./build/k8s-hpa-manager web -f --debug

# Filtrar logs específicos
./build/k8s-hpa-manager web -f 2>&1 | grep "reconciliação"

# JSON logs pretty-print
./build/k8s-hpa-manager web -f 2>&1 | jq
```

---

## 🔄 Workflow Completo de Desenvolvimento

### 1. Fazer Alterações no Frontend

```bash
cd internal/web/frontend
npm run dev
# Editar arquivos em src/
# Hot reload automático em http://localhost:5173
```

### 2. Build de Produção

```bash
npm run build
cd ../../..
```

### 3. Rebuild Backend (embeds frontend)

```bash
go build -o build/k8s-hpa-manager
```

### 4. Testar Build Completo

```bash
# Parar servidores antigos
lsof -ti:8080 | xargs -r kill -9

# Iniciar novo servidor
./build/k8s-hpa-manager web -f

# Testar em http://localhost:8080
# Hard refresh: Ctrl+Shift+R
```

### 5. Verificar Logs e Métricas

```bash
# Console do browser (F12)
# Verificar erros JavaScript

# Terminal do servidor
# Verificar erros HTTP, port-forwards, reconciliação
```

---

## 📊 Monitoramento em Tempo Real

### Status do Engine

```bash
curl -H 'Authorization: Bearer poc-token-123' http://localhost:8080/api/v1/monitoring/status | jq
```

### Métricas de um HPA

```bash
curl -H 'Authorization: Bearer poc-token-123' \
  "http://localhost:8080/api/v1/monitoring/metrics/akspriv-prod/default/my-hpa?duration=1h" | jq
```

### Listar HPAs Monitorados

```bash
# Via localStorage (abrir DevTools → Application → Local Storage)
localStorage.getItem("monitored_hpas")

# Via backend (visualizar targets)
cat ~/.k8s-hpa-manager/monitoring-targets.json | jq
```

---

## 🛠️ Troubleshooting Comum

### Frontend não atualiza após rebuild

```bash
# 1. Limpar cache do Vite
rm -rf internal/web/frontend/node_modules/.vite

# 2. Rebuild
cd internal/web/frontend && npm run build

# 3. Rebuild backend (embeds novo static/)
cd ../../.. && go build -o build/k8s-hpa-manager

# 4. Hard refresh no browser
# Ctrl+Shift+R
```

### Port-forwards timeout

```bash
# Verificar VPN conectada
ping akspriv-prod.privatelink.brazilsouth.azmk8s.io

# Verificar kubectl configurado
kubectl cluster-info --context=akspriv-prod-admin

# Logs do port-forward
kubectl port-forward svc/prometheus-k8s -n monitoring 55551:9090 --context=akspriv-prod-admin
```

### Linha cinza D-1 não aparece

```bash
# Verificar dados no SQLite
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
SELECT COUNT(*)
FROM hpa_snapshots
WHERE cluster = 'akspriv-prod'
  AND namespace = 'default'
  AND hpa_name = 'my-hpa'
  AND DATE(datetime(timestamp, 'unixepoch')) = DATE('now', '-1 day');
"

# Verificar response da API
curl -H 'Authorization: Bearer poc-token-123' \
  "http://localhost:8080/api/v1/monitoring/metrics/akspriv-prod/default/my-hpa?duration=24h" | jq '.snapshots_yesterday | length'

# Verificar console do browser
# F12 → Console → Procurar por "hasYesterdayData"
```

### Memory Target sempre 0

```bash
# Verificar JSON tags na struct (CORRIGIDO em Nov/2025)
grep -A2 "type HPASnapshot" internal/monitoring/models/types.go

# Deve ter:
# CPUTarget    int32 `json:"cpu_target"`
# MemoryTarget int32 `json:"memory_target"`

# Atualizar valores antigos no SQLite
sqlite3 ~/.k8s-hpa-manager/monitoring.db "
UPDATE hpa_snapshots SET memory_target = 90 WHERE memory_target = 0 AND cpu_target > 0;
"
```

---

## 🔐 Autenticação e Tokens

### Token Padrão (POC)

```bash
# Backend usa: poc-token-123
# Frontend envia automaticamente no header

# Testar via curl
curl -H 'Authorization: Bearer poc-token-123' http://localhost:8080/api/v1/clusters
```

### Token Customizado

```bash
# Definir variável de ambiente
export K8S_HPA_WEB_TOKEN="meu-token-secreto"

# Iniciar servidor
./build/k8s-hpa-manager web -f

# Usar no frontend
localStorage.setItem("auth_token", "meu-token-secreto")
```

---

## 📝 Logs e Arquivos de Configuração

### Localização de Arquivos

```bash
# Diretório base
~/.k8s-hpa-manager/

# Banco de dados SQLite
~/.k8s-hpa-manager/monitoring.db

# Targets monitorados (persistência)
~/.k8s-hpa-manager/monitoring-targets.json

# Sessões salvas (TUI e Web)
~/.k8s-hpa-manager/sessions/

# Logs do servidor (modo background)
/tmp/k8s-hpa-manager-web-*.log

# Configuração de clusters
~/.k8s-hpa-manager/clusters-config.json
```

### Ver Logs em Tempo Real

```bash
# Servidor em background
tail -f /tmp/k8s-hpa-manager-web-*.log

# Filtrar por tipo de log
tail -f /tmp/k8s-hpa-manager-web-*.log | grep "level\":\"info"
tail -f /tmp/k8s-hpa-manager-web-*.log | grep "level\":\"error"

# Pretty-print JSON logs
tail -f /tmp/k8s-hpa-manager-web-*.log | grep "^{" | jq
```

---

## 🚢 Deploy e Instalação

### Instalação Completa (1 comando)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

### Update Manual

```bash
# Auto-update interativo
~/.k8s-hpa-manager/scripts/auto-update.sh

# Auto-update sem confirmação (para cron)
~/.k8s-hpa-manager/scripts/auto-update.sh --yes

# Dry-run (simular sem aplicar)
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run
```

### Desinstalação

```bash
~/.k8s-hpa-manager/scripts/uninstall.sh

# Opções:
# 1. Remover apenas binário
# 2. Remover binário + dados (~/.k8s-hpa-manager/)
```

---

## 📚 Referências Rápidas

### Makefile Targets

```bash
make build          # Build TUI
make build-web      # Build completo (frontend + backend)
make web-dev        # Vite dev server
make web-build      # Build frontend apenas
make test           # Unit tests
make test-coverage  # Coverage report
make release        # Multi-platform build
make version        # Show git tag version
```

### Scripts Utilitários

```bash
./rebuild-web.sh -b         # Rebuild completo (recomendado)
./backup.sh "descrição"     # Backup antes de modificações
./restore.sh                # Listar/restaurar backups
./safe-deploy.sh            # Deploy dev2 → main (com validações)
./install.sh                # Instalar em /usr/local/bin/
./uninstall.sh              # Desinstalar
```

---

**Última atualização:** 09 de novembro de 2025
**Versão:** v1.3.9+
