# ✅ Checklist – Correções Prioritárias do Health Checking (Sprint 1)

## 0. Base de Linha (KPI)
- [ ] Medir tempo médio atual do health check por cluster.
- [ ] Levantar percentual de falsos positivos / issues ignoradas.
- [ ] Registrar número de incidentes mensais atribuídos a falta de health checking.

## 1. Service Checker Rodando no Cluster
- [ ] Definir ServiceAccount/RBAC mínimos e namespaces permitidos para o job de diagnóstico.
- [ ] Implementar Job/Pod temporário que executa os analisadores (`internal/healthcheck/analyzers`).
- [ ] Ajustar backend para orquestrar os jobs e coletar os resultados reais.
- [ ] Validar em cluster de teste que MongoDB/Redis/etc. são verificados com sucesso.

## 2. Timeouts Específicos por Tipo de Check
- [ ] Atualizar `HealthCheckRequest` (Go + frontend) para aceitar `timeout_deployments`, `timeout_configs`, `timeout_services`.
- [ ] Propagar valores na UI (HealthCheckingTab) e validar formulário.
- [ ] Ajustar orchestrator/checkers para usar cada timeout específico.
- [ ] Adicionar testes (unitários/integrados) garantindo backward compatibility com o timeout único.

## 3. Circuit Breaker de Métricas
- [ ] Implementar contador de falhas no `DeploymentChecker.enrichWithMetrics`.
- [ ] Após 3 erros consecutivos, marcar metrics-server como indisponível até o próximo ciclo.
- [ ] Expor status em logs e eventos para observabilidade.
- [ ] Testar cenário com metrics-server desligado e confirmar que o checker não degrada o tempo total.

## 4. Replay Buffer + SSE Resiliente
- [ ] Criar buffer in-memory dos últimos N eventos por sessão no orchestrator.
- [ ] Servir eventos históricos para novos clientes SSE.
- [ ] Atualizar `useHealthCheckProgressMultiplexed` para implementar retry/backoff e consumir o buffer.
- [ ] Testar desconexões e verificar se o progresso é retomado sem perda de dados.

## 5. EventChecker
- [ ] Criar `event_checker.go` para consultar `kubectl events`/API.
- [ ] Mapear eventos críticos (FailedScheduling, BackOff, FailedMount, etc.) e correlacionar com resources.
- [ ] Persistir/mostrar resultados no painel de Health Checking (backend + frontend).
- [ ] Validar em cluster de teste com eventos sintéticos.

## 6. Revisões Semanais
- [ ] Registrar avanços/métricas após cada entrega parcial.
- [ ] Atualizar `ANALISE_HEALTHCHECK_MELHORIAS.md` com status das falhas críticas.
- [ ] Preparar reunião de validação para liberar Sprint 2.
