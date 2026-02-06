# Grafana Dashboard - K8s HPA Manager Health Checking

Dashboard Grafana para monitoramento visual do sistema de Health Checking.

## Requisitos

- Grafana 9.0+ (testado em 10.x)
- Prometheus datasource configurado
- K8s HPA Manager com endpoint `/metrics` exposto

## Instalacao

### 1. Configurar Prometheus para Coletar Metricas

Adicione ao seu `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'k8s-hpa-manager'
    static_configs:
      - targets: ['localhost:8080']  # Ajuste para o endereco do seu K8s HPA Manager
    metrics_path: '/metrics'
    scrape_interval: 30s
```

### 2. Importar Dashboard no Grafana

1. Acesse Grafana: `http://localhost:3000`
2. Va em **Dashboards** > **Import**
3. Clique em **Upload JSON file**
4. Selecione o arquivo `k8s-hpa-manager-healthcheck-dashboard.json`
5. Selecione o datasource Prometheus
6. Clique em **Import**

### 3. Verificar Metricas

Acesse `http://localhost:8080/metrics` para verificar se as metricas estao sendo exportadas:

```bash
curl http://localhost:8080/metrics | grep k8s_hpa_manager
```

## Paineis do Dashboard

### Visao Geral
| Painel | Descricao |
|--------|-----------|
| Clusters Monitorados | Total de clusters com health checks |
| Total Health Checks | Quantidade de checks no periodo |
| Taxa de Saude | Percentual de clusters saudaveis |
| Duracao Media | Tempo medio de execucao dos checks |
| Issues Criticas | Total de issues criticas no periodo |
| Issues Warning | Total de warnings no periodo |

### Status por Cluster
| Painel | Descricao |
|--------|-----------|
| Status Atual dos Clusters | Tabela com status e ultimo check |
| Status Visual por Cluster | Gauges coloridos por cluster |

### Tendencias
| Painel | Descricao |
|--------|-----------|
| Duracao dos Health Checks | Grafico de linha da duracao ao longo do tempo |
| Issues Encontradas | Grafico de barras empilhadas por severidade |

### Recursos Verificados
| Painel | Descricao |
|--------|-----------|
| Deployments por Cluster | Bar gauge de deployments por status |
| HPAs por Cluster | Bar gauge de HPAs por status |
| Nodes por Cluster | Bar gauge de nodes por status |

### Distribuicao de Issues
| Painel | Descricao |
|--------|-----------|
| Issues por Severidade | Grafico pizza (critical/high/warning/medium/low) |
| Issues por Tipo de Check | Grafico pizza (deployments/services/hpas/etc) |
| Issues por Cluster | Grafico pizza por cluster |

### Eventos Kubernetes
| Painel | Descricao |
|--------|-----------|
| Eventos Criticos | Bar gauge de eventos criticos por cluster |
| Eventos Warning | Bar gauge de eventos warning por cluster |

## Metricas Prometheus Disponiveis

| Metrica | Tipo | Labels | Descricao |
|---------|------|--------|-----------|
| `k8s_hpa_manager_healthcheck_duration_seconds` | Histogram | cluster, check_type | Duracao do health check |
| `k8s_hpa_manager_healthcheck_status` | Gauge | cluster | Status (0=healthy, 1=warning, 2=critical) |
| `k8s_hpa_manager_healthcheck_issues_total` | Counter | cluster, severity, check_type | Total de issues |
| `k8s_hpa_manager_healthcheck_checks_total` | Counter | cluster, status | Total de checks |
| `k8s_hpa_manager_healthcheck_last_run_timestamp_seconds` | Gauge | cluster | Timestamp do ultimo check |
| `k8s_hpa_manager_healthcheck_deployments_checked` | Gauge | cluster, status | Deployments verificados |
| `k8s_hpa_manager_healthcheck_services_checked` | Gauge | cluster, status | Services verificados |
| `k8s_hpa_manager_healthcheck_hpas_checked` | Gauge | cluster, status | HPAs verificados |
| `k8s_hpa_manager_healthcheck_pvcs_checked` | Gauge | cluster, status | PVCs verificados |
| `k8s_hpa_manager_healthcheck_nodes_checked` | Gauge | cluster, status | Nodes verificados |
| `k8s_hpa_manager_healthcheck_events_found` | Gauge | cluster, severity | Eventos K8s encontrados |

## Alertas (Opcional)

Voce pode configurar alertas no Grafana ou no Prometheus/Alertmanager:

```yaml
# prometheus/rules/k8s-hpa-manager.yml
groups:
  - name: k8s-hpa-manager
    rules:
      - alert: HealthCheckCritical
        expr: k8s_hpa_manager_healthcheck_status == 2
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Cluster {{ $labels.cluster }} em estado critico"
          description: "O health check do cluster {{ $labels.cluster }} esta em estado critico por mais de 5 minutos."

      - alert: HealthCheckStale
        expr: time() - k8s_hpa_manager_healthcheck_last_run_timestamp_seconds > 3600
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Health check nao executado para {{ $labels.cluster }}"
          description: "O health check do cluster {{ $labels.cluster }} nao foi executado nas ultimas 1 hora."
```

## Personalizacao

### Alterar Refresh Rate
1. Clique no icone de engrenagem (Dashboard settings)
2. Em "Time options", altere "Auto refresh" para o intervalo desejado

### Filtrar por Cluster
Adicione uma variavel template:
1. Dashboard settings > Variables > New
2. Name: `cluster`
3. Type: Query
4. Query: `label_values(k8s_hpa_manager_healthcheck_status, cluster)`
5. Aplique o filtro `{cluster=~"$cluster"}` nas queries dos paineis

## Suporte

- Documentacao: [CLAUDE.md](../CLAUDE.md)
- Issues: [GitHub Issues](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues)
