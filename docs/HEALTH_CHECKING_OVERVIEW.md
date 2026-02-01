# Sistema de Health Checking - Visao Geral Executiva

**Projeto**: K8s HPA Manager - Modulo de Health Checking
**Versao**: 1.3.9+
**Data**: 31/01/2026
**Status**: Em Producao (Sprint 1 completo, Sprint 2 em andamento)

---

## 1. Resumo Executivo

O **Sistema de Health Checking** e um modulo integrado ao K8s HPA Manager que realiza verificacoes automatizadas de saude dos clusters Kubernetes. Ele detecta problemas em deployments, services, configuracoes e eventos criticos, gerando alertas e sugestoes de correcao em tempo real.

### Principais Beneficios

| Beneficio | Descricao |
|-----------|-----------|
| **Deteccao Proativa** | Identifica problemas antes que afetem usuarios finais |
| **Reducao de MTTR** | Sugestoes automaticas aceleram resolucao de incidentes |
| **Visibilidade Multi-Cluster** | Analise paralela de multiplos clusters simultaneamente |
| **Auditoria Completa** | Historico persistente de todas as verificacoes |
| **Integracao Nativa** | Funciona com infraestrutura Kubernetes existente |

---

## 2. Arquitetura do Sistema

```
+------------------+     +-------------------+     +------------------+
|    Frontend      |     |     Backend       |     |   Kubernetes     |
|    (React)       |<--->|     (Go/Gin)      |<--->|   API Server     |
+------------------+     +-------------------+     +------------------+
        |                        |                        |
        |   SSE (Real-time)      |                        |
        |<-----------------------|                        |
        |                        |                        |
+------------------+     +-------------------+     +------------------+
|   Resultados     |     |    Orchestrator   |     |  Metrics Server  |
|   (UI/Export)    |     |    (Paralelo)     |     |   (CPU/Memory)   |
+------------------+     +-------------------+     +------------------+
                                 |
                         +-------+-------+
                         |               |
                  +------+------+ +------+------+
                  |   SQLite    | |   Replay    |
                  |  (Historico)| |   Buffer    |
                  +-------------+ +-------------+
```

### Componentes Principais

1. **Orchestrator** (`orchestrator.go`)
   - Coordena execucao paralela de checks em multiplos clusters
   - Gerencia workers e distribui carga
   - Publica progresso via SSE (Server-Sent Events)

2. **Deployment Checker** (`deployment_checker.go`)
   - Verifica saude de Deployments Kubernetes
   - Analisa replicas, containers, probes e metricas
   - Detecta CrashLoopBackOff, ImagePullErrors, etc.

3. **Service Checker** (`service_checker.go`)
   - Testa conectividade de services internos
   - Usa Jobs temporarios para verificacao in-cluster
   - Suporta MongoDB, Redis, PostgreSQL, Kafka, HTTP

4. **Event Checker** (`event_checker.go`)
   - Monitora eventos criticos do Kubernetes
   - Detecta FailedScheduling, OOMKilling, FailedMount
   - Correlaciona eventos com recursos afetados

5. **System Health** (`system_health.go`)
   - Endpoints /healthz para monitoramento do proprio servico
   - Verifica conectividade com clusters, storage e memoria

---

## 3. Funcionalidades Detalhadas

### 3.1 Verificacao de Deployments

O sistema analisa cada Deployment nos namespaces selecionados e verifica:

| Verificacao | Descricao | Severidade |
|-------------|-----------|------------|
| **Replicas** | Compara replicas prontas vs desejadas | Critical se 0, Warning se parcial |
| **CrashLoopBackOff** | Detecta containers em loop de crash | Critical |
| **ImagePullErrors** | Identifica falhas ao baixar imagens | Critical |
| **Liveness Probe** | Verifica se probe esta configurado | Warning se ausente |
| **Readiness Probe** | Verifica se probe esta configurado | Warning se ausente |
| **Startup Probe** | Verifica se probe esta configurado | Info (best practice) |
| **CPU/Memory** | Coleta metricas de uso via Metrics Server | Info |

**Exemplo de Saida:**
```json
{
  "name": "api-gateway",
  "namespace": "production",
  "status": "warning",
  "replicas_ready": 2,
  "replicas_desired": 3,
  "has_liveness_probe": true,
  "has_readiness_probe": false,
  "message": "Apenas 2/3 replicas prontas",
  "suggestions": [
    "kubectl get pods -n production -l app=api-gateway",
    "Configurar readiness probe para controlar trafego"
  ]
}
```

### 3.2 Validacao de Probes (Novo - v1.3.9+)

Alem de verificar se probes existem, o sistema valida suas configuracoes:

| Parametro | Validacao | Recomendacao |
|-----------|-----------|--------------|
| `timeoutSeconds` | Nao pode ser muito curto | >= 3s para liveness |
| `initialDelaySeconds` | Deve permitir inicializacao | >= 5s para apps lentas |
| `failureThreshold` | Nao pode ser muito baixo | >= 3 para evitar falsos positivos |
| `periodSeconds` | Nao pode ser muito frequente | >= 5s para nao sobrecarregar |

**Problemas Detectados:**
```json
{
  "probe_issues": [
    {
      "container": "nginx",
      "probe_type": "liveness",
      "issue": "initialDelaySeconds=0 pode causar restarts durante inicializacao lenta",
      "severity": "warning"
    }
  ]
}
```

### 3.3 Verificacao de Services

O sistema cria Jobs temporarios dentro do cluster para testar conectividade:

```
+----------------+     +------------------+     +----------------+
|  Health Check  | --> | Job (busybox)    | --> | Service Target |
|  Controller    |     | nc -zv host port |     | (MongoDB, etc) |
+----------------+     +------------------+     +----------------+
        |                      |
        |   Coleta Logs        |
        |<---------------------|
        |                      |
        v                      v
   Analisa Resultado      Auto-Delete (TTL 60s)
```

**Seguranca do Job:**
- `runAsNonRoot: true` - Executa como usuario nao-root
- `readOnlyRootFilesystem: true` - Sistema de arquivos somente leitura
- `allowPrivilegeEscalation: false` - Sem escalacao de privilegios
- `drop: ["ALL"]` - Remove todas as capabilities Linux
- `seccompProfile: RuntimeDefault` - Perfil de seguranca padrao
- TTL de 60 segundos + cleanup manual (dupla garantia)

### 3.4 Monitoramento de Eventos Kubernetes

O Event Checker monitora eventos criticos que indicam problemas:

| Evento | Tipo | Sugestoes |
|--------|------|-----------|
| `FailedScheduling` | Critical | Verificar recursos disponiveis nos nodes |
| `CrashLoopBackOff` | Critical | Analisar logs anteriores ao crash |
| `OOMKilling` | Critical | Aumentar limits de memoria |
| `FailedMount` | Critical | Verificar PV/PVC e permissoes |
| `ImagePullBackOff` | Critical | Validar credenciais e nome da imagem |
| `Unhealthy` | Warning | Verificar probes e endpoints |
| `Evicted` | Warning | Verificar pressao de recursos no node |

### 3.5 Circuit Breaker para Metricas

O sistema implementa um Circuit Breaker para evitar degradacao quando o Metrics Server esta indisponivel:

```
Estado Normal          Apos 3 Falhas         Proxima Sessao
+-------------+       +-------------+       +-------------+
|   FECHADO   | ----> |   ABERTO    | ----> |   FECHADO   |
| (coletando) |       | (skip)      |       | (reset)     |
+-------------+       +-------------+       +-------------+
```

**Comportamento:**
- Apos 3 falhas consecutivas, para de tentar coletar metricas
- Evita timeout desnecessario em cada deployment
- Reseta automaticamente na proxima sessao de health check

### 3.6 SSE e Replay Buffer

O frontend recebe atualizacoes em tempo real via Server-Sent Events:

```
Cliente Conecta --> Recebe Replay Buffer --> Stream em Tempo Real
                         |
                         v
                 [Ultimos 100 eventos]
                 (caso reconexao)
```

**Recursos:**
- Buffer de 100 eventos por sessao (FIFO)
- Retry automatico com backoff exponencial (1s, 2s, 4s)
- Isolamento entre sessoes de diferentes usuarios

---

## 4. Interface do Usuario

### 4.1 Tela de Configuracao

A interface permite configurar:
- Selecao de clusters (individual ou por ambiente: prod/hlg/all)
- Namespaces a verificar (ou todos)
- Tipos de verificacao (Deployments, Services, Configs, Events)
- Timeouts especificos por tipo de check
- Filtros de falsos positivos

### 4.2 Modal de Progresso

Durante a execucao, o usuario ve:
- Barra de progresso por cluster
- Tabs para clusters multiplos
- Log em tempo real de cada verificacao
- Contadores de Healthy/Warning/Critical

### 4.3 Painel de Resultados

Apos conclusao:
- Badges clicaveis com contadores por status
- Historico persistente (SQLite)
- Exportacao em PDF, Markdown ou CSV

---

## 5. Endpoints da API

| Metodo | Endpoint | Descricao |
|--------|----------|-----------|
| `POST` | `/api/v1/healthcheck/run` | Inicia verificacao de saude |
| `GET` | `/api/v1/healthcheck/progress` | Stream SSE de progresso |
| `GET` | `/api/v1/healthcheck/events/:id` | Eventos de uma sessao |
| `GET` | `/healthz` | Status detalhado do sistema |
| `GET` | `/healthz/live` | Liveness probe (Kubernetes) |
| `GET` | `/healthz/ready` | Readiness probe (Kubernetes) |

---

## 6. Metricas e Observabilidade

### Logs Estruturados (Zerolog)

```json
{
  "level": "info",
  "cluster": "aks-prod-admin",
  "namespace": "api-gateway",
  "deployment": "payment-service",
  "status": "warning",
  "message": "Apenas 2/3 replicas prontas",
  "timestamp": "2026-01-31T10:30:00Z"
}
```

### Persistencia (SQLite)

Todos os eventos sao salvos automaticamente:
- Tabela `health_check_events`
- Filtros por data, cluster, namespace, status
- Retencao configuravel

---

## 7. Roadmap de Implementacao

### Sprint 1 - COMPLETO (100%)

| Item | Status |
|------|--------|
| Service Checker via Jobs K8s | Completo |
| Timeouts Especificos por Tipo | Completo |
| Circuit Breaker de Metricas | Completo |
| Replay Buffer + SSE Resiliente | Completo |
| EventChecker | Completo |

### Sprint 2 - EM ANDAMENTO (40%)

| Item | Status |
|------|--------|
| Validacao de Probes Configurations | Completo |
| Resource Requests/Limits Validation | Pendente |
| Node Health Checker | Pendente |
| ConfigMap/Secret Cross-reference | Pendente |
| Health Check do Health Checker (/healthz) | Completo |

### Sprint 3 e 4 - PLANEJADO

- Time-series Trend Analysis
- Network Policies Validation
- HPA/VPA Validation
- PersistentVolumes Validation
- Export de Relatorios (PDF/CSV)
- Integracoes (Slack/Teams/Email)
- Grafana Dashboard
- Prometheus Metrics Export

---

## 8. Requisitos Tecnicos

### Backend
- Go 1.24+
- client-go v0.34.1 (Kubernetes SDK)
- Gin v1.11.0 (HTTP Framework)
- SQLite (Persistencia)

### Frontend
- React 18.3.1
- TypeScript 5.8.3
- shadcn/ui (Componentes)
- Recharts (Graficos)

### Infraestrutura
- Kubernetes 1.25+
- Metrics Server (opcional, para metricas de CPU/Memory)
- RBAC configurado para acesso aos clusters

---

## 9. Seguranca

| Aspecto | Implementacao |
|---------|---------------|
| **Autenticacao** | Azure AD via grupos (VV_CLOUD_SRE) |
| **Autorizacao** | RBAC middleware protege operacoes destrutivas |
| **Jobs de Teste** | Executam como usuario nao-privilegiado |
| **Dados Sensiveis** | Secrets nunca sao expostos nos logs |
| **Sanitizacao** | Logs de IA mascaram credenciais automaticamente |

---

## 10. Como Usar

### Iniciar Health Check

1. Acessar a aba "Health Checking" na interface web
2. Selecionar clusters (ou ambiente: prod/hlg)
3. Escolher namespaces (ou deixar vazio para todos)
4. Marcar tipos de verificacao desejados
5. Clicar em "Iniciar Health Check"

### Interpretar Resultados

| Status | Significado | Acao |
|--------|-------------|------|
| **Healthy** | Recurso funcionando normalmente | Nenhuma |
| **Warning** | Problema detectado, mas nao critico | Investigar quando possivel |
| **Critical** | Problema grave que requer atencao imediata | Acao urgente |

### Exportar Relatorio

1. Clicar no botao "Exportar" no painel de resultados
2. Selecionar formato (PDF, Markdown ou CSV)
3. Arquivo sera gerado com timestamp da analise

---

## 11. Contato e Suporte

Para duvidas ou sugestoes sobre o sistema de Health Checking:
- **Repositorio**: github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager
- **Documentacao**: /docs/guides/ no repositorio

---

*Documento gerado automaticamente em 31/01/2026*
