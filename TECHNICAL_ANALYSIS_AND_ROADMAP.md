# Análise Técnica e Roadmap - New K8s HPA Manager

**Data:** 13 de novembro de 2025
**Autor:** Análise técnica baseada em sugestões do time de SRE

---

## 📋 Índice

1. [Análise das Sugestões Apresentadas](#análise-das-sugestões-apresentadas)
2. [Opinião Técnica por Área](#opinião-técnica-por-área)
3. [Gaps Identificados](#gaps-identificados)
4. [Roadmap Sugerido](#roadmap-sugerido)
5. [Considerações Arquiteturais](#considerações-arquiteturais)

---

## 🔍 Análise das Sugestões Apresentadas

### 1. Observabilidade Completa do Monitoring Engine

**Sugestão Original:**
> "Observabilidade completa do Monitoring – hoje dependemos dos snapshots; valeria expor logs/health do engine diretamente na aba (status do port-forward, fila de baseline, erros recentes). Facilita troubleshooting sem precisar do servidor."

**Análise Técnica:**

**Prós:**
- ✅ Reduz drasticamente o tempo de diagnóstico de problemas
- ✅ Visibilidade em tempo real do estado do sistema de monitoramento
- ✅ Permite identificar falhas de port-forward antes que afetem coleta de métricas
- ✅ Transparência sobre o que está sendo monitorado ativamente

**Contras:**
- ⚠️ Aumenta complexidade do frontend (mais um painel para gerenciar)
- ⚠️ Pode expor informações sensíveis se não filtrado corretamente

**Viabilidade:** ALTA
**Prioridade:** ALTA (impacto direto na experiência de troubleshooting)

**Implementação Recomendada:**
1. **Endpoint de Health:** `/api/v1/monitoring/health`
   - Status do engine (running/stopped/error)
   - Clusters ativos e status de cada port-forward
   - Última execução de scan e próxima agendada
   - Contadores: snapshots coletados, erros, baseline pendentes

2. **Endpoint de Logs:** `/api/v1/monitoring/logs?level=error&limit=50`
   - Últimos N logs filtrados por nível
   - Timestamps, mensagem, contexto (cluster/namespace/hpa)
   - Sem informações sensíveis (tokens, passwords)

3. **WebSocket para Live Updates:** `/ws/monitoring/status`
   - Push de eventos em tempo real (port-forward criado/destruído, scan iniciado/concluído)
   - Evita polling excessivo

**UI Sugerida:**
- Painel colapsável no topo da aba Monitoring
- Badges visuais: 🟢 Healthy | 🟡 Warning | 🔴 Error
- Accordion com logs expansíveis (últimos 20)
- Indicador por cluster: "✅ Port-forward ativo" | "❌ Sem conexão"

---

### 2. Cache/Streaming Bidirecional (WebSocket/EventSource)

**Sugestão Original:**
> "Cache/streaming bidirecional – integrar WebSocket/EventSource para métricas e status dos HPAs monitorados, reduzindo polls e dando sensação 'live'."

**Análise Técnica:**

**Prós:**
- ✅ Reduz carga no servidor (elimina polling a cada X segundos)
- ✅ Latência mínima para atualizações críticas (ex: HPA atingiu max replicas)
- ✅ Melhor UX - sensação de aplicação "real-time"
- ✅ Permite notificações proativas (ex: anomalia detectada)

**Contras:**
- ⚠️ Complexidade adicional no backend (gerenciar conexões WebSocket)
- ⚠️ Requer fallback para ambientes que bloqueiam WebSocket
- ⚠️ Precisa gerenciar reconexão automática em caso de queda

**Viabilidade:** MÉDIA-ALTA
**Prioridade:** MÉDIA (melhora UX mas não resolve problema crítico)

**Implementação Recomendada:**

**Backend (Go):**
```go
// Usar gorilla/websocket ou nhooyr.io/websocket
type MonitoringHub struct {
    clients   map[*Client]bool
    broadcast chan MonitoringEvent
    register  chan *Client
}

type MonitoringEvent struct {
    Type      string      `json:"type"` // "snapshot", "anomaly", "port_forward_status"
    Cluster   string      `json:"cluster"`
    Namespace string      `json:"namespace"`
    HPAName   string      `json:"hpa_name"`
    Data      interface{} `json:"data"`
    Timestamp time.Time   `json:"timestamp"`
}
```

**Frontend (React):**
```typescript
// Hook customizado para WebSocket
const useMonitoringStream = (cluster: string, namespace: string, hpaName: string) => {
  const [data, setData] = useState<HPASnapshot | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<'connecting' | 'connected' | 'disconnected'>('connecting');

  useEffect(() => {
    const ws = new WebSocket(`ws://localhost:8080/ws/monitoring?cluster=${cluster}&ns=${namespace}&hpa=${hpaName}`);

    ws.onopen = () => setConnectionStatus('connected');
    ws.onmessage = (event) => {
      const update = JSON.parse(event.data);
      if (update.type === 'snapshot') {
        setData(update.data);
      }
    };
    ws.onerror = () => setConnectionStatus('disconnected');
    ws.onclose = () => {
      setConnectionStatus('disconnected');
      // Auto-reconexão após 5s
      setTimeout(() => window.location.reload(), 5000);
    };

    return () => ws.close();
  }, [cluster, namespace, hpaName]);

  return { data, connectionStatus };
};
```

**Fallback Necessário:**
- Se WebSocket falhar, voltar para polling (useQuery com refetchInterval)
- Detectar via try/catch no constructor do WebSocket

**Ganho Esperado:**
- **Redução de requests:** De ~120 req/min (polling 10s) para ~0 req + push eventos
- **Latência:** De ~5-10s (média do polling) para <1s (push imediato)

---

### 3. Fluxo ConfigMaps com Histórico (Audit Trail)

**Sugestão Original:**
> "Fluxo ConfigMaps com histórico – registrar diffs aplicados (audit trail) e permitir rollback rápido; junto disso, validações automáticas (ex: YAML lint) antes mesmo do dry-run."

**Análise Técnica:**

**Prós:**
- ✅ Compliance/auditoria facilitada (quem alterou, quando, o quê)
- ✅ Rollback rápido em caso de problemas
- ✅ Reduz erros com validação prévia (YAML lint, schemas)
- ✅ Rastreabilidade completa de mudanças

**Contras:**
- ⚠️ Requer armazenamento persistente (SQLite ou PostgreSQL)
- ⚠️ Precisa cuidado com dados sensíveis (Secrets no histórico)
- ⚠️ Pode crescer rapidamente em ambientes com muitas alterações

**Viabilidade:** ALTA
**Prioridade:** ALTA (governança é crítica em ambientes corporativos)

**Implementação Recomendada:**

**1. Schema de Banco (SQLite/PostgreSQL):**
```sql
CREATE TABLE configmap_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    action TEXT NOT NULL, -- 'create', 'update', 'delete'
    user TEXT NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,

    -- Conteúdo
    data_before TEXT, -- YAML antes da alteração
    data_after TEXT,  -- YAML depois da alteração
    diff_unified TEXT, -- Diff unificado

    -- Metadados
    resource_version TEXT,
    dry_run BOOLEAN DEFAULT 0,
    apply_success BOOLEAN,
    error_message TEXT,

    -- Auditoria
    pr_link TEXT, -- Se vier de validação de PR
    session_id TEXT, -- Para correlação

    INDEX idx_configmap (cluster, namespace, name),
    INDEX idx_timestamp (timestamp DESC),
    INDEX idx_user (user)
);
```

**2. Backend - Endpoints:**
- `GET /api/v1/configmaps/history?cluster=X&namespace=Y&name=Z` - Lista histórico
- `GET /api/v1/configmaps/history/:id/diff` - Diff específico
- `POST /api/v1/configmaps/rollback/:history_id` - Rollback para versão anterior

**3. Frontend - UI:**
- Botão "Histórico" ao lado do editor ConfigMap
- Modal com timeline de alterações (cards por mudança)
- Cada card mostra: timestamp, usuário, tipo de ação, botão "Ver Diff", botão "Rollback"
- Diff visual usando diff2html (mesmo padrão já implementado)

**4. Validações Automáticas:**

**YAML Lint (pre-dry-run):**
```typescript
import yaml from 'js-yaml';

const validateYAML = (content: string): ValidationResult => {
  try {
    yaml.load(content);
    return { valid: true };
  } catch (e) {
    return {
      valid: false,
      error: e.message,
      line: e.mark?.line,
      column: e.mark?.column
    };
  }
};
```

**Schema Validation (Kubernetes API):**
```go
// Usar k8s.io/apimachinery/pkg/util/validation
import "k8s.io/apimachinery/pkg/util/validation"

func ValidateConfigMapName(name string) []string {
    return validation.IsDNS1123Subdomain(name)
}

func ValidateLabels(labels map[string]string) []string {
    var errs []string
    for k, v := range labels {
        errs = append(errs, validation.IsQualifiedName(k)...)
        errs = append(errs, validation.IsValidLabelValue(v)...)
    }
    return errs
}
```

**Retenção de Dados:**
- Padrão: 90 dias de histórico
- Configurável via config
- Auto-cleanup via cron job (backend)

**Estimativa de Armazenamento:**
- ~50 KB por entrada (YAML before/after + diff)
- 100 alterações/dia = 5 MB/dia
- 90 dias = ~450 MB (aceitável para SQLite)

---

### 4. Automação de Deploy (Build + Push Pipeline)

**Sugestão Original:**
> "Automação de deploy – criar pipeline ou script 'build+push' único que já copia assets, recompila binário e reinicia o serviço para evitar esquecimentos manuais."

**Análise Técnica:**

**Prós:**
- ✅ Elimina erros manuais (esquecer rebuild, esquecer copiar assets)
- ✅ Processo padronizado e reproduzível
- ✅ Reduz tempo de deploy (de ~5min manual para ~1min automático)
- ✅ Facilita CI/CD

**Contras:**
- ⚠️ Requer acesso SSH/deployment no servidor (pode ter restrições corporativas)
- ⚠️ Precisa gerenciar downtime durante restart
- ⚠️ Rollback precisa ser igualmente automatizado

**Viabilidade:** ALTA
**Prioridade:** MÉDIA-ALTA (aumenta confiabilidade mas não adiciona features)

**Implementação Recomendada:**

**Script Unificado: `deploy.sh`**
```bash
#!/bin/bash
# Automated deployment script for new-k8s-hpa

set -e

VERSION=${1:-$(git describe --tags --always)}
TARGET=${2:-production} # production | staging | local

echo "🚀 Deploying new-k8s-hpa version $VERSION to $TARGET"

# 1. Build frontend
echo "📦 Building frontend..."
cd internal/web/frontend
npm ci --production
npm run build
cd ../../..

# 2. Embed static files
echo "🔨 Compiling Go binary..."
LDFLAGS="-X k8s-hpa-manager/internal/updater.Version=$VERSION"
go build -ldflags "$LDFLAGS" -o build/new-k8s-hpa .

# 3. Run tests
echo "🧪 Running tests..."
go test ./... -v

# 4. Deploy based on target
case $TARGET in
  production)
    echo "🔄 Deploying to production..."
    sudo systemctl stop new-k8s-hpa-web || true
    sudo cp build/new-k8s-hpa /usr/local/bin/
    sudo chmod +x /usr/local/bin/new-k8s-hpa
    sudo systemctl start new-k8s-hpa-web
    ;;

  staging)
    echo "🔄 Deploying to staging..."
    scp build/new-k8s-hpa staging-server:/opt/new-k8s-hpa/
    ssh staging-server 'systemctl restart new-k8s-hpa-web'
    ;;

  local)
    echo "✅ Local build complete. Binary: ./build/new-k8s-hpa"
    ;;
esac

echo "✅ Deployment complete!"
new-k8s-hpa version
```

**Systemd Service (para restart automático):**
```ini
# /etc/systemd/system/new-k8s-hpa-web.service
[Unit]
Description=New K8s HPA Manager Web Server
After=network.target

[Service]
Type=simple
User=sre-user
WorkingDirectory=/opt/new-k8s-hpa
ExecStart=/usr/local/bin/new-k8s-hpa web --port 8080
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

**CI/CD GitHub Actions:**
```yaml
name: Deploy

on:
  push:
    branches: [main]
    tags: ['v*']

jobs:
  deploy:
    runs-on: self-hosted # Ou usar runner corporativo

    steps:
      - uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: '18'

      - name: Deploy
        run: ./deploy.sh ${{ github.ref_name }} production
        env:
          DEPLOY_KEY: ${{ secrets.DEPLOY_KEY }}
```

**Healthcheck pós-deploy:**
```bash
# Validar que o serviço subiu corretamente
for i in {1..30}; do
  if curl -f http://localhost:8080/health > /dev/null 2>&1; then
    echo "✅ Service healthy!"
    exit 0
  fi
  echo "⏳ Waiting for service... ($i/30)"
  sleep 2
done

echo "❌ Service failed to start!"
exit 1
```

**Rollback Automático:**
```bash
# Manter últimas 3 versões
sudo cp /usr/local/bin/new-k8s-hpa /usr/local/bin/new-k8s-hpa.backup.$(date +%s)
ls -t /usr/local/bin/new-k8s-hpa.backup.* | tail -n +4 | xargs -r sudo rm

# Rollback
sudo cp /usr/local/bin/new-k8s-hpa.backup.<timestamp> /usr/local/bin/new-k8s-hpa
sudo systemctl restart new-k8s-hpa-web
```

---

### 5. Testes End-to-End (E2E) Web

**Sugestão Original:**
> "Testes end-to-end/Web – um pacote básico (Playwright/Cypress) cobrindo ações críticas (editar HPA, aplicar ConfigMap, navegar entre HPAs do monitoring) garantiria que regressões de UI sejam detectadas cedo."

**Análise Técnica:**

**Prós:**
- ✅ Detecta regressões antes de chegarem a produção
- ✅ Valida fluxos críticos de ponta a ponta
- ✅ Aumenta confiança em deploys
- ✅ Documenta comportamento esperado (testes como documentação viva)

**Contras:**
- ⚠️ Testes E2E são lentos (minutos vs segundos de unit tests)
- ⚠️ Requer ambiente de teste isolado (clusters, dados mock)
- ⚠️ Flaky tests podem gerar falsos positivos
- ⚠️ Manutenção contínua necessária

**Viabilidade:** ALTA
**Prioridade:** MÉDIA (importante mas não urgente)

**Implementação Recomendada:**

**Escolha de Ferramenta: Playwright**
- ✅ Melhor suporte para TypeScript
- ✅ Parallelização nativa
- ✅ Screenshots/videos automáticos em falhas
- ✅ API moderna e estável

**Estrutura de Testes:**
```
tests/
├── e2e/
│   ├── hpas/
│   │   ├── list-hpas.spec.ts
│   │   ├── edit-hpa.spec.ts
│   │   └── apply-hpa.spec.ts
│   ├── configmaps/
│   │   ├── view-configmap.spec.ts
│   │   ├── edit-configmap.spec.ts
│   │   └── diff-configmap.spec.ts
│   ├── monitoring/
│   │   ├── add-hpa-to-monitoring.spec.ts
│   │   └── view-metrics.spec.ts
│   └── sessions/
│       ├── save-session.spec.ts
│       └── load-session.spec.ts
├── fixtures/
│   ├── mock-clusters.json
│   ├── mock-hpas.json
│   └── mock-configmaps.json
└── playwright.config.ts
```

**Exemplo de Teste:**
```typescript
// tests/e2e/hpas/edit-hpa.spec.ts
import { test, expect } from '@playwright/test';

test.describe('Edit HPA', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('http://localhost:8080');
    // Login mock se necessário
    await page.getByRole('button', { name: 'HPAs' }).click();
  });

  test('should edit min replicas and save', async ({ page }) => {
    // Selecionar primeiro HPA
    await page.getByTestId('hpa-card').first().click();

    // Abrir editor
    await page.getByRole('button', { name: 'Edit' }).click();

    // Alterar min replicas
    const minReplicasInput = page.getByLabel('Min Replicas');
    await minReplicasInput.clear();
    await minReplicasInput.fill('3');

    // Salvar
    await page.getByRole('button', { name: 'Save' }).click();

    // Validar que apareceu no staging
    await page.getByRole('tab', { name: 'Staging' }).click();
    await expect(page.getByText('Min Replicas: 2 → 3')).toBeVisible();

    // Aplicar mudanças
    await page.getByRole('button', { name: 'Apply Changes' }).click();
    await page.getByRole('button', { name: 'Confirm' }).click();

    // Validar sucesso
    await expect(page.getByText('Changes applied successfully')).toBeVisible({ timeout: 10000 });
  });

  test('should validate min < max replicas', async ({ page }) => {
    await page.getByTestId('hpa-card').first().click();
    await page.getByRole('button', { name: 'Edit' }).click();

    // Tentar min > max (inválido)
    await page.getByLabel('Min Replicas').fill('10');
    await page.getByLabel('Max Replicas').fill('5');

    // Validar erro
    await expect(page.getByText('Min replicas must be less than or equal to max replicas')).toBeVisible();

    // Botão Save deve estar desabilitado
    await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();
  });
});
```

**Mock do Backend (para testes isolados):**
```typescript
// tests/mocks/api-mock.ts
import { rest } from 'msw';
import { setupServer } from 'msw/node';

export const handlers = [
  rest.get('/api/v1/hpas', (req, res, ctx) => {
    return res(
      ctx.json({
        hpas: [
          { name: 'api-hpa', namespace: 'production', min_replicas: 2, max_replicas: 10 },
          { name: 'worker-hpa', namespace: 'production', min_replicas: 1, max_replicas: 5 }
        ]
      })
    );
  }),

  rest.put('/api/v1/hpas/:cluster/:namespace/:name', async (req, res, ctx) => {
    const body = await req.json();
    return res(ctx.json({ success: true, hpa: body }));
  })
];

export const server = setupServer(...handlers);
```

**CI Integration:**
```yaml
# .github/workflows/e2e-tests.yml
name: E2E Tests

on: [pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: '18'

      - name: Install dependencies
        run: |
          cd internal/web/frontend
          npm ci
          npx playwright install --with-deps

      - name: Start backend (mock mode)
        run: |
          go run . web --port 8080 --mock &
          sleep 5

      - name: Run E2E tests
        run: npm run test:e2e

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: playwright-report
          path: playwright-report/
```

**Cobertura Mínima Recomendada:**
1. **HPAs:** Listar, editar (min/max/targets), aplicar, validações
2. **ConfigMaps:** Ver, editar YAML, diff, dry-run, apply
3. **Monitoring:** Adicionar HPA, ver métricas, remover
4. **Sessions:** Salvar, carregar, renomear, deletar
5. **Node Pools:** Editar, aplicar agora

**Estimativa de Tempo:**
- Setup inicial: ~3 dias
- Por fluxo coberto: ~2-4 horas
- Manutenção: ~2 horas/sprint

---

### 6. Documentação Operacional

**Sugestão Original:**
> "Documentação operacional – um anexo mostrando passo a passo para atualizar o binário, regras para uso do modo tela cheia, e troubleshooting rápido quando o monitoring não sincroniza."

**Análise Técnica:**

**Prós:**
- ✅ Reduz carga em suporte/onboarding
- ✅ Padroniza operações críticas
- ✅ Facilita transferência de conhecimento
- ✅ Referência rápida em situações de emergência

**Contras:**
- ⚠️ Documentação fica desatualizada facilmente
- ⚠️ Requer manutenção contínua

**Viabilidade:** ALTA
**Prioridade:** MÉDIA (essencial mas não bloqueia desenvolvimento)

**Implementação Recomendada:**

**Criar: `OPERATIONS_GUIDE.md`**

Estrutura sugerida:
```markdown
# Guia Operacional - New K8s HPA Manager

## 📦 Deployment

### Atualizar Binário (Produção)
1. Baixar release: `wget https://github.com/.../new-k8s-hpa-v1.x.x`
2. Parar serviço: `sudo systemctl stop new-k8s-hpa-web`
3. Backup: `sudo cp /usr/local/bin/new-k8s-hpa /usr/local/bin/new-k8s-hpa.backup`
4. Instalar: `sudo mv new-k8s-hpa-v1.x.x /usr/local/bin/new-k8s-hpa && sudo chmod +x /usr/local/bin/new-k8s-hpa`
5. Iniciar: `sudo systemctl start new-k8s-hpa-web`
6. Validar: `curl http://localhost:8080/health`

### Rollback
```bash
sudo systemctl stop new-k8s-hpa-web
sudo cp /usr/local/bin/new-k8s-hpa.backup /usr/local/bin/new-k8s-hpa
sudo systemctl start new-k8s-hpa-web
```

## 🔧 Troubleshooting

### Monitoring Não Sincroniza

**Sintomas:** HPAs não aparecem métricas, gráficos vazios

**Checklist:**
1. Verificar se engine está rodando:
   - Acessar aba Monitoring > Status Panel
   - Ver "Engine Status: 🟢 Running"

2. Validar port-forwards:
   ```bash
   lsof -i :55551-55556
   # Deve mostrar processos kubectl port-forward
   ```

3. Logs do backend:
   ```bash
   tail -f /tmp/new-k8s-hpa-web.log | grep monitoring
   ```

4. Testar conectividade Prometheus:
   ```bash
   kubectl port-forward -n monitoring svc/prometheus-k8s 9090:9090
   curl http://localhost:9090/-/healthy
   ```

5. Verificar baseline:
   - Se HPA foi adicionado há <5min, baseline ainda está sendo coletado
   - Aguardar até 3min (coleta de 3 dias pode demorar)

**Soluções Comuns:**
- Port-forward morto: Restart do engine (botão na UI)
- Baseline travado: Remover HPA e adicionar novamente
- Prometheus inacessível: Verificar VPN/conectividade cluster

### Interface Web Não Carrega

**Sintomas:** Tela branca, erro 404

**Checklist:**
1. Verificar se servidor está rodando:
   ```bash
   new-k8s-hpa-web status
   ```

2. Hard refresh do browser: `Ctrl+Shift+R`

3. Verificar logs:
   ```bash
   tail -100 /tmp/new-k8s-hpa-web.log
   ```

4. Testar API diretamente:
   ```bash
   curl http://localhost:8080/health
   curl -H "Authorization: Bearer poc-token-123" http://localhost:8080/api/v1/clusters
   ```

**Soluções Comuns:**
- Assets não embeddados: Rebuild completo (`./rebuild-web.sh -b`)
- Porta em uso: Parar processo antigo (`pkill -f "new-k8s-hpa web"`)
- Token inválido: Usar `poc-token-123` (default)

### ConfigMap Diff Não Aparece

**Sintomas:** Modal de diff vazio ou erro

**Checklist:**
1. Validar YAML syntax no Monaco Editor (erros aparecem em vermelho)
2. Verificar se há mudanças reais (diff só aparece se houver alterações)
3. Logs do backend para erro de parsing

**Solução:**
- Se YAML inválido: corrigir syntax primeiro
- Se sem mudanças: verificar se está comparando versão correta

## 📊 Monitoramento de Saúde

### Métricas Importantes
- **Uptime do servidor:** `systemctl status new-k8s-hpa-web`
- **Port-forwards ativos:** `lsof -i :55551-55556 | wc -l` (esperado: 2-4)
- **Snapshots coletados:** Ver na UI > Monitoring > Status
- **Tamanho do SQLite:** `du -h ~/.new-k8s-hpa/monitoring.db` (max ~500MB)

### Limpeza de Cache
```bash
# Limpar snapshots antigos (>3 dias)
sqlite3 ~/.new-k8s-hpa/monitoring.db "DELETE FROM hpa_snapshots WHERE timestamp < datetime('now', '-3 days');"

# Vacuum para liberar espaço
sqlite3 ~/.new-k8s-hpa/monitoring.db "VACUUM;"
```

## 🎯 Modo Tela Cheia (TUI)

### Requisitos
- Terminal mínimo: 80x24
- Recomendado: 120x30 ou maior
- Manter zoom do terminal em 100% (Ctrl+0)

### Troubleshooting Visual
- **Texto sobrepondo:** Aumentar janela do terminal
- **Cores estranhas:** Verificar TERM environment (`echo $TERM`)
- **Caracteres quebrados:** Instalar fonte com suporte Unicode (ex: JetBrains Mono)

## 🔐 Segurança

### Tokens
- **Web:** Token padrão `poc-token-123` (MUDAR em produção!)
- **GitHub:** Não armazenar tokens no servidor (usar sessão do browser)

### Permissions
- Binário deve ter owner `root:sre-team` e permissions `755`
- Diretório de dados `~/.new-k8s-hpa/` deve ser `700` (apenas usuário)

## 📞 Suporte
- Issues: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues
- Docs: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/wiki
```

**Integrar ao README:**
- Link "📚 Operations Guide" no README principal
- Quick links para troubleshooting comum

---

## 🔍 ConfigMaps, Secrets, Ingress e Deployments

### 7. Expansão para Secrets

**Análise Técnica:**

**Desafios:**
- 🔐 **Segurança crítica:** Secrets contêm informações sensíveis (passwords, tokens, certificates)
- ⚠️ **Base64 encoding:** Não pode editar diretamente sem decode/encode
- ⚠️ **Tipos específicos:** `kubernetes.io/dockerconfigjson`, `kubernetes.io/tls`, etc.
- 📜 **Auditoria obrigatória:** Quem visualizou e alterou

**Viabilidade:** ALTA (mas requer cuidados extras)
**Prioridade:** ALTA (mesma importância que ConfigMaps)

**Implementação Recomendada:**

**1. Visualização Segura:**
```typescript
// Nunca exibir valores em plain text por padrão
<SecretViewer>
  {Object.entries(secret.data).map(([key, value]) => (
    <SecretField key={key}>
      <Label>{key}</Label>
      <Value>
        {revealed[key] ? atob(value) : '••••••••'}
        <Button onClick={() => toggleReveal(key)}>
          {revealed[key] ? <EyeOff /> : <Eye />}
        </Button>
        <Button onClick={() => copyToClipboard(atob(value))}>
          <Copy />
        </Button>
      </Value>
    </SecretField>
  ))}
</SecretViewer>
```

**2. Editor com Decode/Encode Automático:**
```typescript
const SecretEditor = ({ secret, onSave }) => {
  const [decodedData, setDecodedData] = useState(() =>
    Object.fromEntries(
      Object.entries(secret.data).map(([k, v]) => [k, atob(v)])
    )
  );

  const handleSave = () => {
    const encoded = Object.fromEntries(
      Object.entries(decodedData).map(([k, v]) => [k, btoa(v)])
    );
    onSave({ ...secret, data: encoded });
  };

  return (
    <Editor>
      {Object.entries(decodedData).map(([key, value]) => (
        <Field key={key}>
          <Input
            label={key}
            type="password" // Masked por padrão
            value={value}
            onChange={(e) => setDecodedData({ ...decodedData, [key]: e.target.value })}
          />
        </Field>
      ))}
    </Editor>
  );
};
```

**3. Validações por Tipo:**

**TLS Certificates:**
```typescript
const validateTLSSecret = (cert: string, key: string): ValidationResult => {
  try {
    // Verificar formato PEM
    if (!cert.includes('BEGIN CERTIFICATE') || !key.includes('BEGIN PRIVATE KEY')) {
      return { valid: false, error: 'Invalid PEM format' };
    }

    // Verificar se cert e key combinam (opcional, via backend)
    // Pode usar bibliotecas como node-forge

    return { valid: true };
  } catch (e) {
    return { valid: false, error: e.message };
  }
};
```

**Docker Config:**
```typescript
const validateDockerConfig = (config: string): ValidationResult => {
  try {
    const parsed = JSON.parse(config);
    if (!parsed.auths || typeof parsed.auths !== 'object') {
      return { valid: false, error: 'Missing "auths" field' };
    }
    return { valid: true };
  } catch (e) {
    return { valid: false, error: 'Invalid JSON' };
  }
};
```

**4. Audit Trail (obrigatório para Secrets):**
```sql
CREATE TABLE secret_access_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    user TEXT NOT NULL,
    action TEXT NOT NULL, -- 'view', 'reveal', 'edit', 'delete'
    field_revealed TEXT, -- Qual campo foi revelado
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    ip_address TEXT,
    user_agent TEXT
);
```

**5. Permissions Check (opcional):**
```go
// Verificar se usuário tem permissão para acessar secret
func (k *K8sClient) CanAccessSecret(user, namespace, secretName string) bool {
    // Via RBAC review API
    sar := &authv1.SelfSubjectAccessReview{
        Spec: authv1.SelfSubjectAccessReviewSpec{
            ResourceAttributes: &authv1.ResourceAttributes{
                Namespace: namespace,
                Verb:      "get",
                Resource:  "secrets",
                Name:      secretName,
            },
        },
    }

    result, err := k.clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
    return err == nil && result.Status.Allowed
}
```

**Recomendações de Segurança:**
- ❌ Nunca logar valores de secrets (apenas keys e metadata)
- ✅ Sempre mascarar valores por padrão (revelar sob demanda)
- ✅ Audit log obrigatório para todas as operações
- ✅ Considerar integração com Vault/External Secrets Operator

---

### 8. Expansão para Ingress

**Análise Técnica:**

**Casos de Uso:**
- 🌐 **Editar hosts/paths:** Alterar rotas de ingress
- 🔐 **Configurar TLS:** Associar certificados
- 🎯 **Annotations:** Configurar nginx/traefik (rate limit, CORS, etc.)

**Desafios:**
- ⚠️ Validação de hosts/DNS
- ⚠️ TLS secrets precisam existir
- ⚠️ Annotations específicas por ingress controller

**Viabilidade:** ALTA
**Prioridade:** MÉDIA (menos crítico que ConfigMaps/Secrets)

**Implementação Recomendada:**

**1. Editor Visual Simplificado:**
```typescript
const IngressEditor = ({ ingress, secrets, onSave }) => {
  return (
    <>
      {/* Hosts e Paths */}
      <Section title="Rules">
        {ingress.spec.rules.map((rule, idx) => (
          <RuleEditor key={idx}>
            <Input label="Host" value={rule.host} />
            {rule.http.paths.map((path, pidx) => (
              <PathEditor key={pidx}>
                <Input label="Path" value={path.path} />
                <Select label="Service" options={services} value={path.backend.service.name} />
                <Input label="Port" type="number" value={path.backend.service.port.number} />
              </PathEditor>
            ))}
          </RuleEditor>
        ))}
      </Section>

      {/* TLS */}
      <Section title="TLS">
        <Checkbox label="Enable TLS" checked={ingress.spec.tls?.length > 0} />
        {ingress.spec.tls?.map((tls, idx) => (
          <TLSEditor key={idx}>
            <Select label="Secret" options={secrets.filter(s => s.type === 'kubernetes.io/tls')} />
            <MultiSelect label="Hosts" values={tls.hosts} />
          </TLSEditor>
        ))}
      </Section>

      {/* Annotations (avançado) */}
      <Section title="Annotations">
        <KeyValueEditor data={ingress.metadata.annotations} />
      </Section>
    </>
  );
};
```

**2. Validações:**

**Host/DNS:**
```typescript
const validateHost = (host: string): boolean => {
  const dnsRegex = /^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$/i;
  return dnsRegex.test(host);
};
```

**TLS Secret Exists:**
```typescript
const validateTLSSecret = async (secretName: string, namespace: string): Promise<boolean> => {
  const secrets = await apiClient.getSecrets(cluster, namespace);
  const secret = secrets.find(s => s.name === secretName);
  return secret?.type === 'kubernetes.io/tls';
};
```

**3. Preview de Rotas:**
```typescript
// Mostrar preview de como as rotas ficarão
<RoutePreview>
  {ingress.spec.rules.map(rule =>
    rule.http.paths.map(path => (
      <Route key={`${rule.host}${path.path}`}>
        <Badge color={ingress.spec.tls?.some(t => t.hosts.includes(rule.host)) ? 'green' : 'gray'}>
          {ingress.spec.tls ? 'https' : 'http'}
        </Badge>
        {rule.host}{path.path} → {path.backend.service.name}:{path.backend.service.port.number}
      </Route>
    ))
  )}
</RoutePreview>
```

**4. Annotations Comuns (templates):**
```typescript
const commonAnnotations = {
  'nginx.ingress.kubernetes.io/rate-limit': '100',
  'nginx.ingress.kubernetes.io/cors-allow-origin': '*',
  'cert-manager.io/cluster-issuer': 'letsencrypt-prod',
  'traefik.ingress.kubernetes.io/router.middlewares': 'default-redirect-https@kubernetescrd'
};

// Botão "Add Common Annotation" com autocomplete
```

---

### 9. Deployments Gerenciados por Helm

**Análise Técnica:**

**Contexto:** Como os Deployments são gerenciados via Helm, a abordagem é **observabilidade + operações seguras**, não edição direta de manifests.

**Viabilidade:** ALTA (leitura) / BAIXA (edição direta)
**Prioridade:** MÉDIA-ALTA (visibilidade é essencial)

**Implementação Recomendada:**

**1. Modo Read-Only com Insights:**
```typescript
const DeploymentViewer = ({ deployment, helmRelease }) => {
  return (
    <ViewMode>
      {/* Alerta se for gerenciado por Helm */}
      {helmRelease && (
        <Alert variant="info">
          <HelmIcon />
          Este Deployment é gerenciado por Helm Release: <Badge>{helmRelease.name}</Badge>
          <Link to={`/helm/releases/${helmRelease.name}`}>Ver Release</Link>
        </Alert>
      )}

      {/* Cards de métricas importantes */}
      <MetricsGrid>
        <Card title="Image">
          {deployment.spec.template.spec.containers[0].image}
          <Badge>{extractTag(deployment.spec.template.spec.containers[0].image)}</Badge>
        </Card>

        <Card title="Replicas">
          {deployment.status.replicas} / {deployment.spec.replicas} ready
          <Progress value={deployment.status.readyReplicas} max={deployment.spec.replicas} />
        </Card>

        <Card title="Strategy">
          {deployment.spec.strategy.type}
        </Card>

        <Card title="Resources">
          CPU: {deployment.spec.template.spec.containers[0].resources.requests.cpu} - {deployment.spec.template.spec.containers[0].resources.limits.cpu}
          Memory: {deployment.spec.template.spec.containers[0].resources.requests.memory} - {deployment.spec.template.spec.containers[0].resources.limits.memory}
        </Card>
      </MetricsGrid>

      {/* YAML readonly (expandível) */}
      <Collapsible title="View Full Manifest">
        <MonacoEditor value={yaml.dump(deployment)} language="yaml" options={{ readOnly: true }} />
      </Collapsible>
    </ViewMode>
  );
};
```

**2. Painel de Helm Release:**
```typescript
const HelmReleasePanel = ({ release }) => {
  return (
    <Panel>
      <Header>
        <Title>Helm Release: {release.name}</Title>
        <Badge>{release.chart} v{release.version}</Badge>
        <StatusBadge status={release.status} />
      </Header>

      <Section title="Release Info">
        <KeyValue label="Namespace" value={release.namespace} />
        <KeyValue label="Chart" value={`${release.chart}:${release.version}`} />
        <KeyValue label="App Version" value={release.appVersion} />
        <KeyValue label="Last Updated" value={formatDate(release.lastUpdated)} />
        <KeyValue label="Revision" value={release.revision} />
      </Section>

      <Section title="Values">
        <Button onClick={() => downloadValues(release)}>
          Download values.yaml
        </Button>
        <MonacoEditor
          value={release.values}
          language="yaml"
          options={{ readOnly: true }}
        />
      </Section>

      <Section title="Actions">
        <ButtonGroup>
          <Button onClick={() => helmHistory(release)}>
            <History /> View History
          </Button>
          <Button onClick={() => helmRollback(release)}>
            <Rewind /> Rollback
          </Button>
          <Button onClick={() => helmStatus(release)}>
            <Info /> Status
          </Button>
        </ButtonGroup>
      </Section>

      {/* Drift Detection */}
      <DriftDetection deployment={deployment} helmChart={release.chart} />
    </Panel>
  );
};
```

**3. Drift Detection:**
```go
// Backend: Comparar manifesto atual com template Helm
func DetectDrift(deployment *appsv1.Deployment, releaseName, namespace string) (*DriftReport, error) {
    // 1. Obter values do release
    valuesCmd := exec.Command("helm", "get", "values", releaseName, "-n", namespace, "--all")
    valuesOutput, _ := valuesCmd.Output()

    // 2. Renderizar template com os values
    templateCmd := exec.Command("helm", "template", releaseName, "chart-repo/chart-name", "--values", "-")
    templateCmd.Stdin = bytes.NewReader(valuesOutput)
    expectedManifest, _ := templateCmd.Output()

    // 3. Comparar com manifesto atual
    currentManifest, _ := yaml.Marshal(deployment)

    diff := generateDiff(string(expectedManifest), string(currentManifest))

    return &DriftReport{
        HasDrift: len(diff) > 0,
        Diff: diff,
        Fields: extractChangedFields(diff),
    }, nil
}
```

**4. Operações Seguras (sem editar manifest):**

**Restart Pods:**
```bash
kubectl rollout restart deployment/<name> -n <namespace>
```
- ✅ Respeita Helm (não altera o manifesto)
- ✅ Usa estratégia de rolling update configurada

**Scale Temporário:**
```bash
kubectl scale deployment/<name> --replicas=<N> -n <namespace>
```
- ⚠️ Alerta: "Esta mudança será sobrescrita no próximo helm upgrade"
- 🔔 Notificar usuário para atualizar values.yaml se for permanente

**5. Integração com GitOps (ArgoCD/Flux):**
```typescript
// Detectar se release é gerenciado por ArgoCD
const detectGitOps = (deployment: Deployment): GitOpsInfo | null => {
  const annotations = deployment.metadata.annotations;

  if (annotations['argocd.argoproj.io/instance']) {
    return {
      tool: 'ArgoCD',
      app: annotations['argocd.argoproj.io/instance'],
      url: `https://argocd.example.com/applications/${annotations['argocd.argoproj.io/instance']}`
    };
  }

  if (annotations['fluxcd.io/automated']) {
    return {
      tool: 'Flux',
      kustomization: annotations['kustomization.toolkit.fluxcd.io/name'],
      url: `https://github.com/org/repo/tree/main/k8s/${annotations['kustomization.toolkit.fluxcd.io/name']}`
    };
  }

  return null;
};

// UI
{gitOps && (
  <Alert variant="warning">
    <GitBranch />
    This deployment is managed by {gitOps.tool}. Changes should be made via Git.
    <Link href={gitOps.url} external>Open in {gitOps.tool}</Link>
  </Alert>
)}
```

**6. Validação de PR Helm (já documentado em PR_VALIDATION_WORKFLOW.md):**
- Integrar painel de validação de PR no card do Deployment
- Botão "Validate PR" que abre modal com comparação de values

---

## 🎯 Roadmap Sugerido

### Fase 1: Estabilização e Observabilidade (Sprint 1-2)
**Objetivo:** Aumentar confiança e visibilidade do sistema atual

1. **Observabilidade do Monitoring Engine** (1 sprint)
   - Endpoint `/monitoring/health`
   - Painel de status na UI
   - Logs filtrados

2. **Documentação Operacional** (0.5 sprint)
   - `OPERATIONS_GUIDE.md`
   - Troubleshooting comum
   - Integrar no README

3. **Automação de Deploy** (0.5 sprint)
   - Script `deploy.sh`
   - Systemd service
   - Healthcheck pós-deploy

### Fase 2: Governança e Auditoria (Sprint 3-4)
**Objetivo:** Compliance e rastreabilidade

1. **ConfigMaps com Histórico** (1.5 sprints)
   - Schema de banco
   - Endpoints de histórico
   - UI de timeline/diff/rollback
   - Validações YAML

2. **Expansão para Secrets** (1 sprint)
   - Editor seguro com reveal
   - Validações por tipo
   - Audit trail completo

3. **Audit Trail Global** (0.5 sprint)
   - Log centralizado de todas as operações
   - Export para SIEM/auditoria

### Fase 3: Features Avançadas (Sprint 5-7)
**Objetivo:** Melhorar UX e adicionar novos recursos

1. **WebSocket/Streaming** (1 sprint)
   - Hub de broadcasting
   - Frontend hooks
   - Fallback para polling

2. **Ingress Support** (1 sprint)
   - Editor visual
   - Validações de hosts/TLS
   - Preview de rotas

3. **Deployments (Observabilidade)** (1 sprint)
   - Read-only viewer
   - Helm release panel
   - Drift detection
   - Operações seguras (restart/scale)

### Fase 4: Qualidade e Performance (Sprint 8-9)
**Objetivo:** Garantir estabilidade e performance

1. **Testes E2E** (1.5 sprints)
   - Setup Playwright
   - Cobertura de fluxos críticos
   - Integração CI

2. **Performance Optimization** (0.5 sprint)
   - Lazy loading de componentes
   - Virtualização de listas longas
   - Cache agressivo

### Fase 5: Integração Helm/GitOps (Sprint 10-11)
**Objetivo:** Completar ciclo de validação de PRs

1. **Validação de PR Helm** (1.5 sprints)
   - Frontend: fetch de raw.githubusercontent.com
   - Backend: helm get values + diff
   - UI de comparação
   - Audit trail

2. **Integração GitOps** (0.5 sprint)
   - Detectar ArgoCD/Flux
   - Links contextuais
   - Alertas sobre drift

---

## 💭 Considerações Arquiteturais

### Escalabilidade

**Horizontal:**
- ✅ Backend atual é stateless (pode escalar horizontalmente)
- ⚠️ SQLite não suporta múltiplas instâncias escrevendo
- 💡 **Solução:** Migrar para PostgreSQL quando >1 instância necessária

**Vertical:**
- Monitoramento de múltiplos clusters pode consumir RAM
- Estimar: ~50MB por cluster monitorado (port-forwards + cache)
- 10 clusters = ~500MB RAM adicional

### Persistência

**Atual:** SQLite em `~/.new-k8s-hpa/monitoring.db`

**Limites:**
- ✅ Até ~1GB de dados (3 dias de 100 HPAs)
- ⚠️ Sem high availability
- ⚠️ Backups manuais

**Migração Futura para PostgreSQL:**
```go
type PersistenceConfig struct {
    Driver string // "sqlite" | "postgres"
    DSN    string // Connection string
}

// Auto-detect e migrar
if cfg.Driver == "postgres" {
    db, err = sql.Open("postgres", cfg.DSN)
} else {
    db, err = sql.Open("sqlite3", cfg.DSN)
}
```

### Segurança

**Atual:**
- Token fixo `poc-token-123` (desenvolvimento)
- Sem rate limiting
- Sem RBAC granular

**Recomendações Produção:**
1. **Autenticação via SSO:**
   - OAuth2/OIDC (Azure AD, Okta)
   - JWT tokens com expiração
   - Refresh token flow

2. **Autorização:**
   - RBAC baseado em grupos AD
   - Policies por namespace/cluster
   - Audit log de acessos

3. **Rate Limiting:**
   - Por usuário/IP
   - Prevenir abuse de APIs

4. **HTTPS Obrigatório:**
   - Certificado via Let's Encrypt
   - HSTS headers

### Performance

**Otimizações Implementadas:**
- ✅ Cache de clients K8s (evita re-autenticação)
- ✅ Batch insert de snapshots (100 por vez)
- ✅ RWMutex para concorrência

**Otimizações Pendentes:**
- ⏳ Lazy loading de métricas (carregar sob demanda)
- ⏳ Virtualização de listas (react-window)
- ⏳ Compression de responses (gzip)
- ⏳ CDN para assets estáticos

---

## 📊 Priorização Final

### 🔥 Prioridade ALTA (Implementar Primeiro)
1. **Observabilidade do Monitoring** - Facilita troubleshooting imediato
2. **ConfigMaps com Histórico** - Governança crítica
3. **Secrets Support** - Paridade com ConfigMaps
4. **Documentação Operacional** - Reduz carga de suporte

### 🔶 Prioridade MÉDIA (Próximos Sprints)
5. **WebSocket/Streaming** - Melhora UX significativamente
6. **Automação de Deploy** - Reduz erros humanos
7. **Ingress Support** - Completa cobertura de recursos
8. **Deployments (Read-only)** - Visibilidade necessária

### 🔷 Prioridade BAIXA (Backlog)
9. **Testes E2E** - Importante mas pode aguardar estabilização
10. **Validação de PR Helm** - Nice to have, não bloqueia operação
11. **Integração GitOps** - Adiciona valor mas não é crítico

---

## 🎓 Conclusão

O **New K8s HPA Manager** já possui uma base sólida e funcional. As sugestões apresentadas são **todas viáveis tecnicamente** e agregariam valor significativo.

**Recomendação principal:** Focar em **observabilidade e governança** primeiro (Fases 1-2), pois:
- ✅ Aumentam confiança no sistema
- ✅ Reduzem tempo de troubleshooting
- ✅ Atendem requisitos de compliance
- ✅ Não adicionam complexidade excessiva

Após estabilizar essas áreas, expandir para **features avançadas** (WebSocket, Ingress, Deployments) que melhoram UX mas não são críticas para operação.

**Estimativa total:** ~11 sprints (22-33 semanas) para implementar todas as sugestões com qualidade.

---

**Documentos Relacionados:**
- [PR_VALIDATION_WORKFLOW.md](PR_VALIDATION_WORKFLOW.md) - Detalhamento de validação de PRs Helm
- [CLAUDE.md](CLAUDE.md) - Guia completo de desenvolvimento
- [README.md](README.md) - Documentação principal do projeto

**Última atualização:** 13 de novembro de 2025
