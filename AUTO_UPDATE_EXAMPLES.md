# 🔄 Auto-Update - Exemplos de Uso

## 📋 Referência Rápida

### Flags Disponíveis

| Flag | Descrição | Uso |
|------|-----------|-----|
| `--yes, -y` | Auto-confirmar (sem perguntar) | Scripts/Cron |
| `--dry-run, -d` | Simular sem executar | Testes |
| `--update, -u` | Forçar verificação e atualização | Manual |
| `--check, -c` | Apenas verificar status | Consulta |
| `--force, -f` | Reinstalar mesmo se atualizado | Corrigir instalação |
| `--help, -h` | Mostrar ajuda | Documentação |

---

## 💡 Exemplos Práticos

### 1. Uso Interativo (Modo Padrão)

```bash
./auto-update.sh
```

**O que acontece:**
```
🔄 K8s HPA Manager - Auto Update
==================================================

ℹ️  Verificando atualizações disponíveis...

Iniciando atualização
==================================================
ℹ️  Versão atual: 1.1.0
ℹ️  Versão disponível: 1.2.0

Deseja atualizar agora? [Y/n]: _
```

✅ **Uso:** Atualização manual interativa
✅ **Seguro:** Pede confirmação antes de instalar

---

### 2. Verificar Status (Sem Instalar)

```bash
./auto-update.sh --check
```

**Output:**
```
🔄 K8s HPA Manager - Auto Update
==================================================

Status da Instalação
==================================================
ℹ️  Versão atual: 1.1.0
ℹ️  Localização: /usr/local/bin/k8s-hpa-manager

⚠️  Nova versão disponível: 1.1.0 → 1.2.0

Execute './auto-update.sh --update' para atualizar
Ou './auto-update.sh --yes' para atualizar sem confirmação
```

✅ **Uso:** Verificar sem instalar
✅ **Seguro:** Nunca modifica nada

---

### 3. Auto-Confirmar (Para Scripts)

```bash
./auto-update.sh --yes
```

**O que acontece:**
```
🔄 K8s HPA Manager - Auto Update
==================================================

ℹ️  Verificando atualizações disponíveis...

Iniciando atualização
==================================================
ℹ️  Versão atual: 1.1.0
ℹ️  Versão disponível: 1.2.0

ℹ️  Auto-confirmação ativada (--yes), prosseguindo com atualização...

ℹ️  Baixando e executando instalador...
[...]
✅ Atualização concluída com sucesso!
```

✅ **Uso:** Scripts automatizados, cron jobs
⚠️ **Atenção:** Não pede confirmação!

---

### 4. Modo Dry-Run (Simular)

```bash
./auto-update.sh --dry-run
```

**Output:**
```
🔄 K8s HPA Manager - Auto Update
==================================================
⚠️  MODO DRY RUN - Nenhuma alteração será feita

ℹ️  Verificando atualizações disponíveis...
⚠️  [DRY RUN] Não removendo cache (dry run)

Iniciando atualização
==================================================
ℹ️  Versão atual: 1.1.0
ℹ️  Versão disponível: 1.2.0

Deseja atualizar agora? [Y/n]: y

⚠️  [DRY RUN] Simulando download e instalação...
⚠️  [DRY RUN] curl -fsSL https://raw.githubusercontent.com/.../install-from-github.sh | bash

✅ Simulação concluída! (modo dry-run)
ℹ️  Execute sem --dry-run para instalar de verdade
```

✅ **Uso:** Testar antes de executar de verdade
✅ **Seguro:** Nunca modifica nada

---

### 5. Dry-Run + Auto-Confirm

```bash
./auto-update.sh --dry-run --yes
```

Simula atualização sem perguntar (útil para testar scripts).

---

### 6. Forçar Reinstalação

```bash
./auto-update.sh --force
```

Reinstala mesmo que já esteja na versão mais recente.
Útil se a instalação estiver corrompida.

---

### 7. Forçar Reinstalação Sem Confirmar

```bash
./auto-update.sh --yes --force
```

Reinstala automaticamente sem perguntar.

---

## 🤖 Uso em Automação

### Cron Job (Atualização Semanal)

```bash
# Editar crontab
crontab -e

# Adicionar linha (toda segunda-feira às 9h)
0 9 * * 1 /path/to/auto-update.sh --yes >> /var/log/k8s-hpa-update.log 2>&1
```

**O que faz:**
- Verifica updates toda segunda às 9h
- Se houver update, instala automaticamente
- Logs salvos em `/var/log/k8s-hpa-update.log`

---

### Script Bash com Verificação de Erro

```bash
#!/bin/bash

# Script de atualização com notificação de erro

SCRIPT="/path/to/auto-update.sh"
LOG="/var/log/k8s-hpa-update.log"

echo "=== $(date) ===" >> "$LOG"

if $SCRIPT --yes >> "$LOG" 2>&1; then
    echo "✅ Atualização bem-sucedida" >> "$LOG"
else
    echo "❌ Falha na atualização" >> "$LOG"

    # Notificar administrador
    echo "Falha ao atualizar k8s-hpa-manager" | \
        mail -s "[ERRO] K8s HPA Manager Update Failed" admin@example.com
fi
```

---

### Systemd Timer (Alternativa ao Cron)

**Arquivo: `/etc/systemd/system/k8s-hpa-update.service`**

```ini
[Unit]
Description=K8s HPA Manager Auto Update
After=network.target

[Service]
Type=oneshot
ExecStart=/path/to/auto-update.sh --yes
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

**Arquivo: `/etc/systemd/system/k8s-hpa-update.timer`**

```ini
[Unit]
Description=K8s HPA Manager Auto Update Timer
Requires=k8s-hpa-update.service

[Timer]
OnCalendar=Mon *-*-* 09:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

**Ativar:**

```bash
sudo systemctl daemon-reload
sudo systemctl enable k8s-hpa-update.timer
sudo systemctl start k8s-hpa-update.timer

# Verificar status
sudo systemctl status k8s-hpa-update.timer
sudo systemctl list-timers
```

---

### Script Python com Notificação Slack

```python
#!/usr/bin/env python3
import subprocess
import requests
import json
from datetime import datetime

SLACK_WEBHOOK = "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
UPDATE_SCRIPT = "/path/to/auto-update.sh"

def notify_slack(message):
    payload = {
        "text": f"🔄 K8s HPA Manager Update\n{message}",
        "username": "Update Bot"
    }
    requests.post(SLACK_WEBHOOK, data=json.dumps(payload))

try:
    # Executar atualização
    result = subprocess.run(
        [UPDATE_SCRIPT, "--yes"],
        capture_output=True,
        text=True,
        check=True
    )

    # Sucesso
    notify_slack(f"✅ Atualização concluída com sucesso\n```{result.stdout[-500:]}```")

except subprocess.CalledProcessError as e:
    # Erro
    notify_slack(f"❌ Falha na atualização\n```{e.stderr[-500:]}```")
    raise
```

---

## 🎯 Casos de Uso Recomendados

### Para Desenvolvedores

```bash
# Verificar antes de commitar
./auto-update.sh --check

# Testar script antes de usar
./auto-update.sh --dry-run

# Atualizar interativamente
./auto-update.sh
```

### Para SRE/DevOps

```bash
# Verificar em múltiplos servidores
ansible all -m shell -a "/path/to/auto-update.sh --check"

# Atualizar via Ansible (com confirmação individual)
ansible all -m shell -a "/path/to/auto-update.sh"

# Atualizar automaticamente via Ansible
ansible all -m shell -a "/path/to/auto-update.sh --yes"
```

### Para CI/CD

```yaml
# GitHub Actions
name: Update K8s HPA Manager

on:
  schedule:
    - cron: '0 9 * * 1'  # Toda segunda às 9h

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - name: Check for updates
        run: |
          curl -fsSL https://raw.githubusercontent.com/.../auto-update.sh | bash -s -- --check

      - name: Update if available
        run: |
          curl -fsSL https://raw.githubusercontent.com/.../auto-update.sh | bash -s -- --yes
```

---

## 🔒 Segurança e Boas Práticas

### ✅ Recomendações

1. **Use `--check` antes de `--yes`** em ambientes de produção
2. **Teste com `--dry-run`** antes de automação
3. **Monitore logs** em atualizações automáticas
4. **Configure alertas** para falhas de atualização
5. **Faça backups** antes de forçar reinstalação (`--force`)

### ⚠️ Cuidados

1. **`--yes` em cron**: Certifique-se de ter alertas de erro
2. **`--force`**: Pode sobrescrever customizações locais
3. **Automação**: Sempre tenha rollback plan
4. **Produção**: Teste em staging primeiro

---

## 📊 Tabela Comparativa

| Comando | Verifica | Instala | Confirma | Seguro | Uso |
|---------|----------|---------|----------|--------|-----|
| `./auto-update.sh` | ✅ | ✅ | ✅ | ✅ | Interativo |
| `./auto-update.sh --check` | ✅ | ❌ | ❌ | ✅✅ | Consulta |
| `./auto-update.sh --yes` | ✅ | ✅ | ❌ | ⚠️ | Automação |
| `./auto-update.sh --dry-run` | ✅ | ❌ | ✅ | ✅✅ | Teste |
| `./auto-update.sh --force` | ❌ | ✅ | ✅ | ⚠️ | Repair |
| `./auto-update.sh --yes --force` | ❌ | ✅ | ❌ | ⚠️⚠️ | Automação force |

---

## 🆘 Troubleshooting

### Problema: Script não encontra update

```bash
# Forçar remoção do cache
rm ~/.k8s-hpa-manager/.update-check

# Verificar novamente
./auto-update.sh --check
```

### Problema: Dry-run não mostra resultado

```bash
# Dry-run funciona mesmo sem updates disponíveis
./auto-update.sh --dry-run --force
```

### Problema: Erro de permissão

```bash
# Instalação requer sudo
sudo ./auto-update.sh --yes
```

---

**Dúvidas?** Ver documentação completa em `UPDATE_BEHAVIOR.md`
