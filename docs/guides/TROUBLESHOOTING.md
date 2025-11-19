# 🔧 Troubleshooting

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Problemas Comuns Web

| Problema | Solução |
|----------|---------|
| **Tela branca após rebuild** | Hard refresh: `Ctrl+Shift+R` |
| **"TabProvider not found"** | Adicionar `<TabProvider>` em App.tsx |
| **Sessions não carregam** | Verificar `~/.k8s-hpa-manager/sessions/` existe |
| **Cluster not found** | Executar `k8s-hpa-manager autodiscover` |
| **401 Unauthorized** | Token incorreto - usar `poc-token-123` (default) |
| **Servidor não desliga** | Verificar heartbeat no console do browser (POST /heartbeat a cada 5min) |

## Problemas Comuns TUI

| Problema | Solução |
|----------|---------|
| **Cluster offline** | `kubectl cluster-info --context=<cluster>` |
| **VPN desconectada** | Conectar VPN e pressionar F5 para reload |
| **HPAs não carregam** | Verificar RBAC e toggle namespaces sistema (tecla `S`) |
| **Azure timeout** | Validar `az login` e subscription ativa |
| **Race condition** | Atualizar para versão com mutex fix (v1.6.0+) |
| **Node pools não carregam** | Executar `k8s-hpa-manager autodiscover` |

## Problemas Comuns - Sistema de Updates

| Problema | Solução |
|----------|---------|
| **Updates não detectados** | Remover cache: `rm ~/.k8s-hpa-manager/.update-check` e executar `k8s-hpa-manager version` |
| **GitHub API rate limit** | Configurar token: `export GITHUB_TOKEN=ghp_...` antes de executar |
| **Versão mostra "dev"** | Recompilar com `make build` (injeta versão via git tags) |
| **Cache não expira** | TTL de 24h - forçar com `rm ~/.k8s-hpa-manager/.update-check` |
| **Auto-update falha** | Verificar conexão, permissões sudo e requisitos (Go, Git, kubectl) |
| **Scripts não instalados** | Executar `curl ... install-from-github.sh | bash` novamente |

## Debug Mode

```bash
# TUI
k8s-hpa-manager --debug

# Web
./build/k8s-hpa-manager web -f --debug

# Logs exibidos:
#   - Estado da aplicação (AppState transitions)
#   - Mensagens Bubble Tea
#   - Operações Kubernetes (API calls)
#   - Azure authentication flow
#   - HTTP requests/responses (web)
```

## Backup e Restore

```bash
# Criar backup antes de modificações
./backup.sh "descrição do backup"

# Listar backups disponíveis
./restore.sh

# Restaurar backup específico
./restore.sh backup_20251001_122526
```

- Mantém 10 backups mais recentes
- Metadados inclusos (git commit, data, usuário)
