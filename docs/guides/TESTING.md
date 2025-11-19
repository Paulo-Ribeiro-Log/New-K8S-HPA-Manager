# 🧪 Testing Strategy

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Unit Tests

```bash
make test                     # Run all tests
make test-coverage            # Coverage report → coverage.html
```

## Manual Testing Web

**Pre-requisitos:**
1. Build obrigatório: `./rebuild-web.sh -b`
2. Hard refresh no browser: `Ctrl+Shift+R`

**Checklist:**
- [ ] HPAs: Load, Edit (min/max replicas, targets, resources), Apply
- [ ] Node Pools: Load, Edit (count, autoscaling, min/max), Apply
- [ ] Sessions: Save, Load, Rename, Delete, Edit Content
- [ ] Staging Area: Add items, Clear, Apply, Cancel
- [ ] ApplyAllModal: Preview changes, Apply, Progress tracking
- [ ] Heartbeat: Abrir tab → fechar → servidor desliga em 20min
- [ ] Snapshot: Capturar estado do cluster para rollback
- [ ] Dashboard: Métricas reais (CPU/Memory allocation)
- [ ] **Recovery Mode**: Seleção granular de itens, validação de cluster, progress tracking, resumo final
- [ ] **SSE Progress**: Operações Cordon/Drain com progress bar em tempo real

**Logs:**
```bash
# Modo background
tail -f /tmp/k8s-hpa-manager-web-*.log

# Modo foreground
./build/k8s-hpa-manager web -f --debug
```

## Manual Testing TUI

```bash
make run-dev                              # Debug mode
./build/k8s-hpa-manager --demo            # Demo mode (sem executar)
./build/k8s-hpa-manager --debug           # Debug logging
```

**Checklist:**
- [ ] Cluster discovery e conexão (F5 para reload)
- [ ] Multi-namespace selection (Space para selecionar múltiplos)
- [ ] HPA batch operations (Ctrl+U para aplicar todos)
- [ ] Node Pool sequential execution (F12 para marcar *1 e *2)
- [ ] Session save/load (Ctrl+S/Ctrl+L)
- [ ] VPN validation (mensagens em operações críticas)
- [ ] CronJob management (F9)
- [ ] Prometheus Stack (F8)
- [ ] Log viewer (F3)
- [ ] Modais de confirmação (Ctrl+D/Ctrl+U)

## Testing VPN Validation

**Simular VPN desconectada:**
```bash
# Desconectar VPN
sudo ifconfig <vpn-interface> down

# Iniciar aplicação
./build/k8s-hpa-manager

# Esperado:
# 🔍 Validando conectividade VPN...
# ❌ VPN desconectada - Kubernetes inacessível
# 💡 SOLUÇÃO: Conecte-se à VPN e tente novamente (F5)
```

## Testing Auto-Shutdown (Web)

```bash
# Iniciar servidor em foreground para ver logs
./build/k8s-hpa-manager web -f --debug

# Abrir browser em http://localhost:8080
# Fechar todas as abas
# Aguardar 20 minutos

# Esperado no terminal:
# ╔════════════════════════════════════════════════════════════╗
# ║             AUTO-SHUTDOWN POR INATIVIDADE                 ║
# ╚════════════════════════════════════════════════════════════╝
# ⏰ Último heartbeat: 14:35:22 (há 20 minutos)
# 🛑 Nenhuma página web conectada por mais de 20 minutos
# ✅ Servidor sendo encerrado...
```

## Testing Update System

**Teste 1: Detecção de Updates**
```bash
./build/k8s-hpa-manager version

# Esperado (se houver update disponível):
# k8s-hpa-manager versão 1.1.0
# 🔍 Verificando updates...
# 🆕 Nova versão disponível: 1.1.0 → 1.2.0
# 📦 Download: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0
# 📝 Release Notes (preview): ...
```

**Teste 2: Auto-Update Check**
```bash
~/.k8s-hpa-manager/scripts/auto-update.sh --check

# Esperado:
# Status da Instalação
# ℹ️  Versão atual: 1.1.0
# ℹ️  Localização: /usr/local/bin/k8s-hpa-manager
# ⚠️  Nova versão disponível: 1.1.0 → 1.2.0
```

**Teste 3: Auto-Update Dry-Run**
```bash
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run --yes

# Esperado:
# ⚠️  MODO DRY RUN - Nenhuma alteração será feita
# ℹ️  Auto-confirmação ativada (--yes)
# [DRY RUN] Simulando download e instalação...
# ✅ Simulação concluída! (modo dry-run)
```

**Teste 4: Cache de Verificação**
```bash
# Verificar cache
ls -lh ~/.k8s-hpa-manager/.update-check
cat ~/.k8s-hpa-manager/.update-check

# Forçar nova verificação
rm ~/.k8s-hpa-manager/.update-check
./build/k8s-hpa-manager version
```

**Teste 5: Instalação do Zero**
```bash
# Em máquina limpa ou container
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Esperado:
# ✅ Instalação concluída com sucesso!
# Versão instalada: 1.2.0
# Binário: /usr/local/bin/k8s-hpa-manager
```
