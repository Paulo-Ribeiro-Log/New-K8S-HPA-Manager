# Checklist Completo - Health Checking Improvements

**Última atualização**: 31/01/2026
**Progresso geral**: 10/19 itens (53%)

---

## ✅ Sprint 1 - COMPLETO (5/5 = 100%)

### 1. Service Checker via Jobs K8s ✅
- [x] Job temporário com busybox para testar conectividade TCP
- [x] Segurança máxima: runAsNonRoot, readOnlyRootFilesystem, drop ALL capabilities
- [x] TTL automático + cleanup manual (dupla garantia)
- [x] Discovery automático de Services K8s
- **Arquivo**: `internal/healthcheck/service_checker.go`
- **Testes**: `internal/healthcheck/service_checker_test.go`

### 2. Timeouts Específicos por Tipo de Check ✅
- [x] `timeout_deployments`, `timeout_configs`, `timeout_services`, `timeout_events`
- [x] UI com inputs separados para cada timeout
- [x] Backward compatibility com timeout único
- **Arquivo**: `internal/healthcheck/models.go`
- **Testes**: `internal/healthcheck/models_test.go`

### 3. Circuit Breaker de Métricas ✅
- [x] Contador de falhas no metrics-server
- [x] Após 3 erros consecutivos, skip métricas até próximo ciclo
- [x] Thread-safe com sync.Mutex
- **Arquivo**: `internal/healthcheck/deployment_checker.go`
- **Testes**: `internal/healthcheck/deployment_checker_test.go`

### 4. Replay Buffer + SSE Resiliente ✅
- [x] Buffer in-memory com últimos 100 eventos por sessão
- [x] FIFO: remove evento mais antigo quando cheio
- [x] Envio de replay buffer ao conectar
- [x] Retry automático no frontend (3 tentativas, backoff exponencial)
- **Backend**: `internal/web/sse/progress.go`
- **Frontend**: `internal/web/frontend/src/hooks/useHealthCheckProgress.ts`
- **Testes**: `internal/web/sse/progress_test.go`

### 5. EventChecker ✅
- [x] Consulta API de Events do Kubernetes
- [x] Mapeia eventos críticos: FailedScheduling, CrashLoopBackOff, OOMKilling, etc
- [x] Sugestões contextuais para cada tipo de evento
- [x] Checkbox "Verificar Eventos K8s" no frontend
- [x] Aba "Events" no painel de resultados
- **Backend**: `internal/healthcheck/event_checker.go`
- **Frontend**: `HealthCheckingTab.tsx`, `HealthCheckResultsPanel.tsx`
- **Testes**: `internal/healthcheck/event_checker_test.go`

---

## ✅ Sprint 2 - COMPLETO (5/5 = 100%)

### 6. Validação de Probes Configurations ✅
- [x] Verificar se deployments têm livenessProbe configurado
- [x] Verificar se deployments têm readinessProbe configurado
- [x] Verificar se deployments têm startupProbe configurado (best practice moderna)
- [x] Validar timeouts adequados (não muito curtos)
- [x] Validar initialDelaySeconds razoável
- [x] Validar failureThreshold (não muito baixo)
- [x] Validar periodSeconds (não muito frequente)
- [x] Sugestões de best practices
- **Arquivos**: `internal/healthcheck/models.go`, `internal/healthcheck/deployment_checker.go`
- **Testes**: `internal/healthcheck/deployment_checker_test.go` (9 testes de validação de probes)

### 7. Resource Requests/Limits Validation ✅
- [x] Detectar containers sem requests/limits definidos
- [x] Analisar QoS class (Guaranteed, Burstable, BestEffort)
- [x] Alertar sobre memory limit faltando (severity: critical - risco OOMKill)
- [x] Alertar sobre CPU/Memory requests faltando (severity: warning)
- [x] Estruturas: `QoSClass`, `ResourceIssue`, `ContainerResources`
- [x] Sugestoes contextuais baseadas em QoS class
- **Arquivos**: `internal/healthcheck/models.go`, `internal/healthcheck/deployment_checker.go`
- **Testes**: `internal/healthcheck/deployment_checker_test.go` (9 testes de recursos)

### 8. Node Health Checker ✅
- [x] Verificar Node Conditions (Ready, MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable)
- [x] Analisar capacidade vs alocacao (CPU, Memory, Pods, Ephemeral Storage)
- [x] Calcular utilizacao percentual (CPU, Memory, Pods)
- [x] Detectar nodes com problemas (Critical: NotReady, Pressure; Warning: >90% utilizacao)
- [x] Correlacionar com pods afetados (lista top 10 pods no node com problema)
- [x] Extrair info do node (KubeletVersion, OSImage, Architecture)
- **Arquivos**: `internal/healthcheck/node_checker.go` (~300 linhas)
- **Testes**: `internal/healthcheck/node_checker_test.go` (14 testes)

### 9. ConfigMap/Secret Cross-reference ✅
- [x] Detectar ConfigMaps/Secrets orfaos (nao referenciados por pods)
- [x] Validar que referencias existem (ConfigMaps/Secrets referenciados)
- [x] Detectar referencias faltando (pods apontam para recursos inexistentes)
- [x] Ignorar referencias opcionais (optional: true)
- [x] Filtrar recursos de sistema (kube-, istio-, service account tokens, etc)
- [x] Mapear todos os tipos de referencia: volumes, envFrom, env.valueFrom, projected
- **Arquivos**: `internal/healthcheck/config_crossref_checker.go` (~350 linhas)
- **Testes**: `internal/healthcheck/config_crossref_checker_test.go` (18 testes)

### 10. Health Check do Health Checker (/healthz) ✅
- [x] GET /healthz - Status detalhado com componentes
- [x] GET /healthz/live - Liveness probe
- [x] GET /healthz/ready - Readiness probe
- [x] Verificação de kubernetes, storage, memory
- **Arquivo**: `internal/web/handlers/system_health.go`
- **Testes**: `internal/web/handlers/system_health_test.go`

---

## 🟢 Sprint 3 - NÃO INICIADO (0/5 = 0%)

### 11. Time-series Trend Analysis ⏳
- [ ] Queries SQL para análise de tendências
- [ ] Detectar degradação progressiva
- [ ] Alertas de tendência negativa
- [ ] Gráficos de evolução no frontend
- **Estimativa**: 4 dias

### 12. Network Policies Validation ⏳
- [ ] Listar NetworkPolicies por namespace
- [ ] Detectar pods sem NetworkPolicy
- [ ] Validar regras de ingress/egress
- [ ] Testes de conectividade básicos
- **Estimativa**: 3 dias

### 13. HPA/VPA Validation ⏳
- [ ] Verificar HPAs configurados corretamente
- [ ] Detectar HPAs com min=max (não escala)
- [ ] Validar métricas utilizadas
- [ ] Analisar histórico de scaling
- **Estimativa**: 3 dias

### 14. PersistentVolumes Validation ⏳
- [ ] Verificar status de PVs e PVCs
- [ ] Detectar PVCs pending
- [ ] Alertar sobre storage quase cheio
- [ ] Validar StorageClass
- **Estimativa**: 2 dias

### 15. Severity Levels Refinement ⏳
- [ ] Implementar 5 níveis: Critical, High, Medium, Low, Info
- [ ] Regras de classificação por tipo de problema
- [ ] Filtros por severidade no frontend
- [ ] Ordenação por severidade
- **Estimativa**: 1 dia

---

## 🔵 Sprint 4 - NÃO INICIADO (0/4 = 0%)

### 16. Export de Relatórios (PDF/CSV) ⏳
- [ ] Geração de PDF com jsPDF
- [ ] Export CSV para análise
- [ ] Template profissional sem emojis
- [ ] Seleção de período e clusters
- **Estimativa**: 3 dias

### 17. Integrações (Slack/Teams/Email) ⏳
- [ ] Webhooks para Slack
- [ ] Webhooks para Microsoft Teams
- [ ] Notificações por email (SMTP)
- [ ] Templates customizáveis
- **Estimativa**: 3 dias

### 18. Grafana Dashboard ⏳
- [ ] Dashboard JSON pronto para importar
- [ ] Queries Prometheus para métricas
- [ ] Painéis de status por cluster
- [ ] Alertas visuais
- **Estimativa**: 2 dias

### 19. Prometheus Metrics Export ⏳
- [ ] Endpoint /metrics no formato Prometheus
- [ ] Métricas de health check (tempo, status, contadores)
- [ ] Labels por cluster/namespace
- [ ] Histograma de latências
- **Estimativa**: 2 dias

---

## 📊 Resumo de Progresso

| Sprint | Completos | Total | Progresso |
|--------|-----------|-------|-----------|
| Sprint 1 | 5 | 5 | ✅ 100% |
| Sprint 2 | 5 | 5 | ✅ 100% |
| Sprint 3 | 0 | 5 | ⏳ 0% |
| Sprint 4 | 0 | 4 | ⏳ 0% |
| **TOTAL** | **10** | **19** | **53%** |

---

## 🚀 Como Continuar

1. Abrir novo chat com Claude
2. Mencionar: "Continue o checklist de Health Checking em `CHECKLIST_HEALTHCHECK_COMPLETO.md`"
3. Especificar qual item quer implementar (ex: "Implementar item 6 - Validação de Probes")

---

## 📁 Arquivos Principais

### Backend (Go)
- `internal/healthcheck/` - Core do health checking
- `internal/web/handlers/healthcheck.go` - API REST
- `internal/web/sse/progress.go` - SSE + Replay Buffer

### Frontend (React/TypeScript)
- `src/components/HealthCheckingTab.tsx` - Configuração
- `src/components/HealthCheckResultsPanel.tsx` - Resultados
- `src/hooks/useHealthCheckProgress.ts` - SSE client

### Testes
- `internal/healthcheck/*_test.go` - Testes unitários
- `internal/web/sse/progress_test.go` - Testes SSE
