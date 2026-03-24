# ✅ Checklist – Correções Prioritárias do Health Checking (Sprint 1)

## 0. Base de Linha (KPI)
- [ ] Medir tempo médio atual do health check por cluster.
- [ ] Levantar percentual de falsos positivos / issues ignoradas.
- [ ] Registrar número de incidentes mensais atribuídos a falta de health checking.

## 1. Service Checker via Jobs K8s ✅
- [x] Definir ServiceAccount/RBAC mínimos e namespaces permitidos para o job de diagnóstico.
  - **Implementação**: Job usa default ServiceAccount sem token montado (`AutomountServiceAccountToken: false`)
  - **Segurança**: runAsNonRoot, readOnlyRootFilesystem, drop ALL capabilities, SeccompProfile RuntimeDefault
  - **User/Group**: 65534 (nobody/nogroup) - sem privilégios
- [x] Implementar Job/Pod temporário que executa os analisadores.
  - **Arquivo**: `internal/healthcheck/service_checker.go` (~365 linhas)
  - **Imagem**: busybox:1.36 (minimal, estável)
  - **Teste**: `nc -zv -w5 host port` (TCP connect)
  - **TTL**: 60s auto-delete + cleanup manual (dupla garantia)
  - **Timeout**: ActiveDeadlineSeconds=30s, BackoffLimit=0
  - **Recursos**: CPU 10m-50m, Memory 16Mi-32Mi
- [x] Ajustar backend para orquestrar os jobs e coletar os resultados reais.
  - **Discovery**: Lista Services K8s por namespace (ignora kube-system, istio-system, etc)
  - **Filtragem**: Ignora headless services (ClusterIP=None) e service kubernetes
  - **Logs**: Coleta logs do Pod do Job para analisar resultado
- [ ] Validar em cluster de teste que MongoDB/Redis/etc. são verificados com sucesso.
  - **Testes unitários**: `internal/healthcheck/service_checker_test.go` (6 testes)
  - **Pendente**: Teste de integração em cluster real

## 2. Timeouts Específicos por Tipo de Check ✅
- [x] Atualizar `HealthCheckRequest` (Go + frontend) para aceitar `timeout_deployments`, `timeout_configs`, `timeout_services`.
- [x] Propagar valores na UI (HealthCheckingTab) e validar formulário.
- [x] Ajustar orchestrator/checkers para usar cada timeout específico.
- [x] Adicionar testes (unitários/integrados) garantindo backward compatibility com o timeout único.
  - **Arquivo**: `internal/healthcheck/models_test.go` (18 testes)
  - **Cobertura**: Fallback para timeout geral, constantes padrão, cenários mistos

## 3. Circuit Breaker de Métricas ✅
- [x] Implementar contador de falhas no `DeploymentChecker.enrichWithMetrics`.
- [x] Após 3 erros consecutivos, marcar metrics-server como indisponível até o próximo ciclo.
- [x] Expor status em logs e eventos para observabilidade.
- [x] Testar cenário com metrics-server desligado e confirmar que o checker não degrada o tempo total.
  - **Arquivo**: `internal/healthcheck/deployment_checker_test.go` (11 testes)
  - **Cobertura**: Estado inicial, threshold, reset, thread safety, comportamento entre sessões

## 4. Replay Buffer + SSE Resiliente ✅
- [x] Criar buffer in-memory dos últimos N eventos por sessão no orchestrator.
  - **Arquivo**: `internal/web/sse/progress.go` - Struct `ReplayBuffer` (100 eventos por sessão)
  - **Métodos**: `Add()`, `GetAll()`, `Clear()` com thread-safety (sync.RWMutex)
  - **FIFO**: Remove evento mais antigo quando buffer cheio
- [x] Servir eventos históricos para novos clientes SSE.
  - **Handler**: `healthcheck.go:Progress()` envia replay buffer antes de iniciar stream
  - **Lógica**: Se buffer tem evento `complete`/`error`, fecha stream imediatamente
  - **Cleanup**: Buffer limpo 5 minutos após conclusão (tempo para cliente buscar)
- [x] Atualizar `useHealthCheckProgress` para implementar retry/backoff.
  - **Retry**: Até 3 tentativas com backoff exponencial (1s, 2s, 4s)
  - **Estado**: `retryCount` exposto para UI mostrar status de reconexão
  - **Cleanup**: Timeout de retry cancelado no cleanup/clearEvents
- [x] Testar replay buffer.
  - **Testes**: `internal/web/sse/progress_test.go` (9 testes)
  - **Cobertura**: FIFO, isolamento de sessões, cópia de dados, cliente conectado/desconectado

## 5. EventChecker ✅ (Backend + Frontend Completo)
- [x] Criar `event_checker.go` para consultar `kubectl events`/API.
  - **Arquivo**: `internal/healthcheck/event_checker.go` (~395 linhas)
  - Consulta API de Events via `client.CoreV1().Events(ns).List()`
- [x] Mapear eventos críticos (FailedScheduling, BackOff, FailedMount, etc.) e correlacionar com resources.
  - **CriticalEventReasons**: FailedScheduling, CrashLoopBackOff, ErrImagePull, FailedMount, OOMKilling, etc.
  - **WarningEventReasons**: Unhealthy, Evicted, Preempted, etc.
  - Sugestões contextuais para cada tipo de evento
- [x] Persistir/mostrar resultados no painel de Health Checking (backend + frontend).
  - **Backend**: Integrado no orchestrator.go (seção "Check Events")
  - **Models**: `EventHealth`, `CheckEvents`, `TimeoutEvents`, `EventResults` adicionados
  - **Frontend**: ✅ COMPLETO (verificado 31/01/2026)
    - Checkbox "Verificar Eventos K8s" em `HealthCheckingTab.tsx` (linhas 509-521)
    - Estado `checkEvents` com timeout específico (linhas 73, 647-651)
    - Parâmetro `check_events` enviado no request (linha 268)
    - Aba "Events" no `HealthCheckResultsPanel.tsx` (linha 483)
    - Renderização de resultados com detalhes (linhas 554-559)
- [ ] Validar em cluster de teste com eventos sintéticos.
  - **Testes unitários**: `internal/healthcheck/event_checker_test.go` (20 testes)

## 6. Revisões Semanais
- [ ] Registrar avanços/métricas após cada entrega parcial.
- [ ] Atualizar `ANALISE_HEALTHCHECK_MELHORIAS.md` com status das falhas críticas.
- [ ] Preparar reunião de validação para liberar Sprint 2.
