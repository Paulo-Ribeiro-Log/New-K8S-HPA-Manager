# 🔔 Sistema de Notificações Windows

## Visão Geral

Sistema de notificações nativas do Windows 10/11 usando PowerShell via WSL2, implementado com filosofia KISS (Keep It Simple, Stupid).

### Características

- ✅ **Notificações Toast Nativas** - Windows.UI.Notifications API (sem dependências externas)
- ✅ **Deduplicação Inteligente** - Cooldown de 5 minutos entre alertas idênticos
- ✅ **3 Níveis de Severidade** - Critical (🔴), Warning (🟡), Info (ℹ️)
- ✅ **Sons Diferentes** - Alarm para crítico, Reminder para warning, Default para info
- ✅ **Thread-Safe** - sync.RWMutex para operações concorrentes
- ✅ **Limpeza Automática** - Cache limpo a cada 1 hora

## Arquitetura

```
┌─────────────────────────────────────────────────────────┐
│                    Web Interface                        │
│                (React/TypeScript)                       │
└────────────────────┬────────────────────────────────────┘
                     │ HTTP POST
                     ▼
┌─────────────────────────────────────────────────────────┐
│           handlers/notifications.go                     │
│         (API REST /api/v1/notifications/*)             │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│         notifications/manager.go                        │
│      (Gerenciador com Deduplicação)                    │
│                                                         │
│  - alertCache map[string]time.Time                     │
│  - cooldownPeriod: 5 minutes                           │
│  - Cleanup automático (1h)                             │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│      notifications/windows_notifier.go                  │
│         (PowerShell Toast via WSL2)                     │
│                                                         │
│  powershell.exe -Command "<script>"                     │
│  └─> Windows.UI.Notifications.ToastNotification        │
└─────────────────────────────────────────────────────────┘
```

## API REST

### Endpoints Disponíveis

#### 1. POST /api/v1/notifications/test
Envia uma notificação de teste.

**Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/notifications/test
```

**Response:**
```json
{
  "message": "Test notification sent successfully",
  "enabled": true
}
```

---

#### 2. GET /api/v1/notifications/status
Retorna o status do sistema de notificações.

**Request:**
```bash
curl -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/notifications/status
```

**Response:**
```json
{
  "enabled": true,
  "cacheSize": 2,
  "platform": "windows",
  "mechanism": "PowerShell Toast Notifications"
}
```

---

#### 3. POST /api/v1/notifications/toggle
Habilita/desabilita notificações.

**Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer poc-token-123" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}' \
  http://localhost:8080/api/v1/notifications/toggle
```

**Response:**
```json
{
  "message": "Notification settings updated",
  "enabled": false
}
```

---

#### 4. POST /api/v1/notifications/alert
Envia um alerta manual.

**Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer poc-token-123" \
  -H "Content-Type: application/json" \
  -d '{
    "alertName": "HighCPU",
    "severity": "critical",
    "cluster": "akspriv-abastecimento-prd",
    "namespace": "default",
    "hpaName": "app-hpa",
    "message": "CPU acima de 90% por mais de 5 minutos"
  }' \
  http://localhost:8080/api/v1/notifications/alert
```

**Response:**
```json
{
  "message": "Alert notification sent"
}
```

**Parâmetros:**
- `alertName` (string) - Nome do alerta (ex: "HighCPU", "MemoryPressure")
- `severity` (string) - Severidade: "critical", "warning" ou "info"
- `cluster` (string) - Nome do cluster AKS
- `namespace` (string) - Namespace Kubernetes
- `hpaName` (string) - Nome do HPA
- `message` (string) - Mensagem detalhada do alerta

## Sistema de Deduplicação

### Como Funciona

O sistema cria uma chave única para cada alerta:
```
alertKey = "cluster:namespace:hpaName:alertName:severity"
```

**Exemplo:**
```
"akspriv-abastecimento-prd:default:app-hpa:HighCPU:critical"
```

Quando um alerta é enviado:
1. Verifica se a chave existe no cache
2. Se existe e foi enviada há menos de 5 minutos → **ignora (sem spam)**
3. Se não existe ou passou do cooldown → **envia notificação**
4. Atualiza timestamp no cache

### Cooldown Period

- **Padrão:** 5 minutos
- **Configurável:** Via `manager.SetCooldownPeriod(duration)`

```go
// Exemplo: mudar para 10 minutos
notificationManager.SetCooldownPeriod(10 * time.Minute)
```

## Testes

### Script de Teste Completo

Execute o script de teste incluído:

```bash
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
./test-notifications.sh
```

**O que o script testa:**
1. ✅ Status inicial do sistema
2. ✅ Notificação de teste
3. ✅ Alerta crítico (🔴 som de alarme)
4. ✅ Alerta warning (🟡 som de lembrete)
5. ✅ Alerta info (ℹ️ som padrão)
6. ✅ Verificação do cache
7. ✅ Deduplicação (mesmo alerta não envia novamente)

### Teste Manual Rápido

```bash
# 1. Testar notificação
curl -X POST -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/notifications/test

# 2. Verificar status
curl -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/notifications/status
```

## Integração com Prometheus AlertManager

### Configuração do AlertManager

Para integrar com alertas do Prometheus, configure um webhook receiver no AlertManager:

```yaml
# alertmanager.yml
receivers:
  - name: 'k8s-hpa-manager'
    webhook_configs:
      - url: 'http://localhost:8080/api/v1/notifications/alert'
        send_resolved: true
        http_config:
          bearer_token: 'poc-token-123'

route:
  receiver: 'k8s-hpa-manager'
  routes:
    - match:
        severity: critical
      receiver: 'k8s-hpa-manager'
    - match:
        severity: warning
      receiver: 'k8s-hpa-manager'
```

### Payload do Prometheus

O AlertManager envia payloads no formato:

```json
{
  "receiver": "k8s-hpa-manager",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "HighCPU",
        "severity": "critical",
        "cluster": "akspriv-abastecimento-prd",
        "namespace": "default",
        "hpa": "app-hpa"
      },
      "annotations": {
        "summary": "CPU acima de 90%"
      }
    }
  ]
}
```

**Nota:** Será necessário criar um middleware para transformar o payload do Prometheus no formato esperado pela API.

## Severidades e Estilos

### Critical (🔴)
- **Som:** Alarm (ms-winsoundevent:Notification.Alarm)
- **Cor:** Vermelho
- **Uso:** Problemas que exigem ação imediata

### Warning (🟡)
- **Som:** Reminder (ms-winsoundevent:Notification.Reminder)
- **Cor:** Amarelo
- **Uso:** Situações que precisam atenção

### Info (ℹ️)
- **Som:** Default (ms-winsoundevent:Notification.Default)
- **Cor:** Azul
- **Uso:** Informações gerais, eventos normais

## Exemplo de Notificação Toast

```
┌─────────────────────────────────────────┐
│  K8sHPAManager                     [X]  │
├─────────────────────────────────────────┤
│  🔴 CRÍTICO: HighCPU                    │
│                                         │
│  Cluster: akspriv-abastecimento-prd    │
│  Namespace: default                     │
│  HPA: app-hpa                          │
│                                         │
│  CPU acima de 90% por mais de 5        │
│  minutos                               │
└─────────────────────────────────────────┘
```

## Troubleshooting

### Notificações não aparecem

1. **Verificar se está no WSL2:**
   ```bash
   uname -r | grep microsoft
   # Deve retornar algo como: 6.6.87.2-microsoft-standard-WSL2
   ```

2. **Testar PowerShell manualmente:**
   ```bash
   powershell.exe -Command "Write-Host 'Test'"
   ```

3. **Verificar permissões de notificação no Windows:**
   - Settings → System → Notifications
   - Verificar se notificações estão habilitadas

4. **Verificar logs do servidor:**
   ```bash
   # O servidor deve mostrar:
   # 📢 Notification Manager inicializado
   # ✅ Notificações Windows habilitadas (via PowerShell)
   ```

### Erro "address already in use"

```bash
# Matar processos antigos
pkill -f "new-k8s-hpa web"

# Reiniciar servidor
./build/new-k8s-hpa web -f
```

### Cache não limpa

O cache limpa automaticamente alertas com mais de 1 hora. Para forçar limpeza, reinicie o servidor.

## Limitações Conhecidas

1. **Apenas WSL2:** Funciona apenas no Windows via WSL2 (não funciona em Linux nativo ou macOS)
2. **Focus Assist:** Notificações podem ser bloqueadas pelo Focus Assist do Windows
3. **Toast Limit:** Windows pode limitar o número de toasts simultâneos

## Próximos Passos

- [ ] Integração automática com Prometheus AlertManager
- [ ] Frontend (React) para configurar notificações
- [ ] Suporte a templates customizados de notificação
- [ ] Histórico de notificações enviadas
- [ ] Estatísticas de alertas por cluster/namespace

## Referências

- [Windows.UI.Notifications](https://docs.microsoft.com/en-us/uwp/api/windows.ui.notifications)
- [Toast Notifications XML Schema](https://docs.microsoft.com/en-us/windows/apps/design/shell/tiles-and-notifications/toast-xml-schema)
- [PowerShell Toast Notifications](https://learn.microsoft.com/en-us/powershell/scripting/samples/sample-scripts)
