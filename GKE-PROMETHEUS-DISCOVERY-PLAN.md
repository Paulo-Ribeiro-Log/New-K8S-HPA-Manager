# Descoberta de Prometheus em Clusters GKE (Google Cloud Managed Service for Prometheus)

Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

## Diagnóstico

### Causa raiz original (por que GKE nunca funcionou)

A única fonte de descoberta de URL do Prometheus no sistema é `internal/monitoring/discovery/prometheus.go`. `buildPrometheusURL`/`parseClusterName` são hardcoded para o padrão AKS/EKS da Via Varejo — assumem incondicionalmente que **todo** cluster segue `https://prometheus-<nome>-<ambiente>.viavarejo.com.br/`, construído a partir de um `split("-")` no context name.

Para GKE, o context name tem o formato `gke_<project>_<region>_<cluster>` (confirmado em `internal/config/gke_config.go`, `splitGKEContext`). O parser aplicado cegamente a esse formato produz um hostname sem sentido nenhum (ex: `gke_meuprojeto_us` vira "nome", `central1_meucluster` vira "ambiente"), que nunca resolve via DNS. Toda a stack (HPA metrics engine, Alertas, FinOps, Predictions HPA/NodePool, Latency Test) trata isso como "Prometheus indisponível".

**Consumidores afetados** (todos chamam `discovery.GetPrometheusURL`/`DiscoverEndpoint` sem tratamento por provider):
- `internal/monitoring/client/prometheus_client.go` (`NewPrometheusClient`) — base do `MonitoringEngineV2` (HPA metrics)
- `internal/web/handlers/alerts.go` (`getPrometheusURL`)
- `internal/web/handlers/finops.go` (5 call sites — report, timeline, timeline/compare, compare-snapshot)
- `internal/web/handlers/predictions.go` (`getPrometheusClient`)
- `internal/web/handlers/nodepool_predictions.go` (`getNodePoolPrometheusClient`)
- `internal/monitoring/latencylookup/lookup.go`
- `internal/certificates/prometheus_enrich.go` (única exceção com fallback real — `EnrichWithTLSDial`, mas desenhado para o cenário Gateway API, não é fix geral)
- `internal/healthcheck/orchestrator.go` (única exceção com degradação graciosa — loga e segue sem a comparação)

Existem **3 implementações de cliente HTTP Prometheus distintas e não centralizadas**, cada uma montando `http.Request` manualmente:
- `internal/monitoring/client/prometheus_client.go` (`PrometheusClient`)
- `internal/monitoring/prometheus/client.go` (usado por Predictions/FinOps)
- `internal/monitoring/alerts/client.go`

### ✅ Fase 1 concluída — override manual (mitigação, não a automação pedida)

Implementado nesta sessão: campo opcional `PrometheusURL`/`prometheusUrl` adicionado a `ClusterConfig` (AKS), `EKSClusterConfig` e `GKEClusterConfig` (`internal/config/*.go`). `internal/monitoring/discovery/overrides.go` (novo arquivo) lê esse campo diretamente dos 3 arquivos JSON (`clusters-config.json`/`eks-clusters-config.json`/`gke-clusters-config.json`) sem importar `internal/config` — ver seção "Restrição de import cycle" abaixo para o motivo.

`resolvePrometheusURL()` em `internal/monitoring/discovery/prometheus.go` checa o override primeiro; se vazio, cai exatamente no `buildPrometheusURL` de sempre. **Nenhuma instalação existente tem esse campo preenchido**, então o comportamento de AKS/EKS já funcionando hoje é idêntico — validado com `go test ./internal/... -race` (100% verde) e `make build`.

**Limitação**: exige edição manual do JSON, sem UI. Resolve o caso "Prometheus self-hosted em hostname fora do padrão viavarejo" (AKS/EKS atípicos), mas **não** resolve o pedido real do usuário: descoberta **automática** para clusters GKE que usam GMP (Google Cloud Managed Service for Prometheus), não um Prometheus self-hosted com URL própria.

### O problema real: GMP não é um Prometheus self-hosted

Confirmado com o usuário: os clusters GKE da organização usam **Google Cloud Managed Service for Prometheus**, não um Prometheus self-hosted com endpoint HTTP próprio. Isso muda o problema de "descobrir a URL certa" para "suportar um mecanismo de auth e API completamente diferente":

- **URL determinística** (boa notícia — não precisa de heurística nenhuma): `https://monitoring.googleapis.com/v1/projects/{PROJECT_ID}/location/global/prometheus/`, expondo a mesma API PromQL padrão (`/api/v1/query`, `/api/v1/query_range`, etc.) — compatível com a forma como os 3 clientes já constroem `endpoint.URL + "api/v1/..."`.
- **`PROJECT_ID` já está embutido no context name** (`gke_<project>_<region>_<cluster>`) — dá pra construir a URL 100% automaticamente, sem nenhuma config manual, exatamente como o usuário pediu.
- **Requer `Authorization: Bearer <token OAuth2>`** em toda requisição — diferente do Prometheus self-hosted (`InsecureSkipVerify`, sem auth, certificado auto-assinado). A função que já obtém esse token (`GetFreshGKEToken()`, com cache/singleflight/fallback ADC↔gcloud já testados) vive em `internal/cloudprovider/gcp`.
- **TLS real**: o host `monitoring.googleapis.com` tem certificado válido — não deve usar `InsecureSkipVerify: true` como o path AKS/EKS usa hoje.

### Restrição de import cycle (por que não é só "importar GetFreshGKEToken")

`internal/cloudprovider/gcp` **depende transitivamente** dos pacotes de Prometheus:

```
internal/cloudprovider/gcp (auth.go chama ai.StartDeviceAuth)
  → internal/ai (analyzer.go)
    → internal/collectors (context_builder.go, monta contexto de IA com métricas Prometheus ao vivo)
      → internal/monitoring/client (prometheus_client.go)
        → internal/monitoring/discovery
```

Importar `cloudprovider/gcp` a partir de `monitoring/discovery` ou `monitoring/client` fecha o ciclo. Isso já foi confirmado experimentalmente nesta sessão (build falhou com "import cycle not allowed" ao tentar o caminho ingênuo). Além disso, `internal/collectors` e `internal/ai` **também não podem** importar `cloudprovider/gcp` diretamente pelo mesmo motivo — não é um problema isolado do pacote discovery.

**Solução escolhida**: hook de inversão de dependência. `internal/monitoring/discovery` expõe uma variável de função não implementada (`SetGCPTokenFunc`); quem faz a ligação real é a inicialização do servidor (`cmd/web.go` ou `internal/web/server.go`), o único lugar que enxerga os dois lados sem ciclo (pacote raiz, nada importa `cmd`). Padrão padrão de Go para este tipo de situação — não introduz um framework de DI, só uma variável de pacote + setter.

---

## Fases de implementação

### Fase 1 — Override manual (AKS/EKS/GKE fora do padrão) ✅ CONCLUÍDA

- [x] Campo `PrometheusURL` em `ClusterConfig`, `EKSClusterConfig`, `GKEClusterConfig`
- [x] `internal/monitoring/discovery/overrides.go` — leitura direta dos 3 JSONs, sem import cycle
- [x] `resolvePrometheusURL()` em `prometheus.go` — override primeiro, fallback pro padrão de sempre
- [x] `go test ./internal/... -race` + `make build` — sem regressão
- [x] Commit + push (`feat/prometheus-override-gke-discovery`, commit `4167517e`)

### Fase 2 — Detecção automática de GMP + construção de URL ✅ CONCLUÍDA

- [x] `internal/monitoring/discovery/gmp.go` (novo arquivo): `buildGMPURL(cluster string) string` — extrai `PROJECT_ID` do context `gke_<project>_<region>_<cluster>` via `gkeProjectID` (adicionado em `overrides.go`, mesma convenção de split de `gkeShortName`) e monta `https://monitoring.googleapis.com/v1/projects/{PROJECT_ID}/location/global/prometheus/`
- [x] `PrometheusEndpoint` (struct em `prometheus.go`) ganhou campo `RequiresGCPAuth bool`
- [x] `resolvePrometheusSource()` (renomeado de `resolvePrometheusURL`, agora retorna `(url string, requiresGCPAuth bool)`): override manual (Fase 1) → GMP automático (`gke_*` sem override) → padrão viavarejo de sempre, nessa ordem
- [x] `validateEndpoint` adaptado: para `RequiresGCPAuth`, usa `api/v1/query?query=up` em vez de `api/v1/status/config` (GMP não implementa introspecção de servidor Prometheus real — só a API de query) e **não** seta `InsecureSkipVerify` (certificado do Google é válido)

### Fase 3 — Hook de autenticação (quebra o import cycle) ✅ CONCLUÍDA

- [x] `internal/monitoring/discovery/gcp_auth.go` (novo arquivo): `var gcpTokenFunc func(ctx context.Context) string` + `SetGCPTokenFunc(fn)` — idempotente, nunca panica se não configurado
- [x] `GCPAuthTransport(base http.RoundTripper) http.RoundTripper` — round tripper que clona o request e injeta `Authorization: Bearer <token>`; usa `http.DefaultTransport` se `base == nil`
- [x] `RequiresGCPAuth(cluster string) bool` — variante barata (sem HTTP) de `resolvePrometheusSource`, pra quem só tem a URL crua via `GetPrometheusURL` (finops.go, alerts.go, latencylookup)
- [x] Wiring: **não foi em `cmd/web.go`** como o plano original previa — o ponto real escolhido foi `internal/config/kubeconfig.go`, dentro de `DiscoverClusters()`, no mesmo bloco `if hasGKE` onde `EnsureGKEAuthPlugin`/`LoadSavedGCPADC`/pré-aquecimento de token já rodavam. `internal/config` já importa `cloudprovider/gcp` diretamente ali — importar `internal/monitoring/discovery` também não fecha ciclo (confirmado com build real), e reaproveita um bootstrap que já existia em vez de criar um novo ponto de entrada em `cmd/web.go`

### Fase 4 — Aplicar nos clientes Prometheus (parcial — ver limitações abaixo) ⚠️ PARCIAL

Aplicado nos 2 clientes que fazem **query** (compatíveis com a API que o GMP realmente expõe), sem tocar em nenhum método `Query`/`QueryRange`/etc. — só na construção do `http.Client`:
- [x] `internal/monitoring/client/prometheus_client.go` (`NewPrometheusClient`) — branch por `endpoint.RequiresGCPAuth`
- [x] `internal/monitoring/prometheus/client.go` — `NewClient` ganhou 3º parâmetro `requiresGCPAuth bool` (assinatura mudou; 6 call sites atualizados: `predictions.go`, `nodepool_predictions.go`, `latencylookup/lookup.go` via `discovery.RequiresGCPAuth`, e 3 usos internos em `discovery.go` do próprio pacote — todos dead-code/discovery alternativa não usada, passam `false`)
- [x] `go test ./internal/... -race` + `make build` — sem regressão, sem import cycle

**NÃO aplicado — `internal/monitoring/alerts/client.go`, e por um motivo estrutural, não só falta de tempo**: esse cliente consulta `/api/v1/alerts` (lista de alertas *firing* avaliados por um Prometheus server real). O GMP **não é um Prometheus server** — é só um backend de métricas consultável via PromQL (`/api/v1/query`, `/query_range`, `/series`, `/labels`, `/label/.../values`, `/metadata`); não tem motor de avaliação de regras de alerta, então `/api/v1/alerts` não existe nele independente de autenticação. Ou seja: **a aba Alertas nunca vai funcionar para clusters GKE só com wiring de auth** — precisaria de uma integração completamente diferente (ex: Google Cloud Monitoring Alerting API, `monitoring.googleapis.com/v3/projects/{id}/alertPolicies`, formato de dado totalmente distinto de `Alert`/`AlertFilter` deste pacote). Não implementado nesta fase; `getPrometheusURL()` em `alerts.go` continua devolvendo a URL do GMP automaticamente (via `GetPrometheusURL`), então a chamada vai falhar com 404 do Google em vez do erro de "endpoint não encontrado" de antes — melhor sinal de diagnóstico, mas ainda sem dado nenhum.

**NÃO aplicado — clientes Prometheus internos do FinOps** (`internal/finops/prometheus_enricher.go`, `internal/finops/timeline.go` `QueryTimeline`, `internal/finops/calculator.go` `WithPrometheusURL`): descobertos durante esta fase — são um **4º grupo de clientes HTTP Prometheus**, não catalogado no diagnóstico original deste plano (que falava em "3 clientes"). Todos os 5 call sites em `finops.go` usam `discovery.GetPrometheusURL(cluster)` (string crua) alimentando essas funções, que hoje não têm nenhum parâmetro de auth. `discovery.RequiresGCPAuth(cluster)` já está disponível pra uso deles, mas a mudança em si (plumbing do Bearer token dentro do `internal/finops`) não foi feita.

### Fase 5 — Validação end-to-end (requer cluster GKE real com GMP habilitado)

Nada disto foi/pode ser validado neste ambiente de desenvolvimento — checklist para quando houver acesso a um cluster real:
- [ ] Confirmar que a identidade usada pelo `GetFreshGKEToken()` (ADC salvo em `~/.k8s-hpa-manager/gcp-adc.json` ou `gcloud` local) tem a IAM role `roles/monitoring.viewer` (ou mais ampla) no projeto GCP do cluster
- [ ] Confirmar que o cluster realmente tem GMP habilitado e coletando métricas (`--enable-managed-prometheus` no `gcloud container clusters` ou equivalente Terraform) — sem isso a API responde vazio mesmo com auth/URL corretos, indistinguível de "não implementado" à primeira vista
- [ ] Testar uma query real (`up`, ou uma métrica de HPA tipo `kube_pod_status_phase`) via `MonitoringEngineV2`/Alertas/Predictions contra o cluster GKE e comparar com o que aparece no Cloud Monitoring do console GCP para o mesmo projeto
- [ ] Verificar se o formato de resposta do GMP bate 100% com os structs `QueryResult`/`QueryRangeResult` já existentes (campos `metric`/`value`/`values`) — API é "compatível", mas não há garantia formal de paridade byte-a-byte com Prometheus OSS para todo tipo de query
- [ ] Se `validateEndpoint`/health-check inicial falhar por falta de dados (cluster sem métricas coletadas ainda) vs. falha real de auth/URL, garantir que a mensagem de erro exposta ao usuário distingue os dois casos

### Riscos conhecidos / não cobertos por este plano

- Rate limits/quotas do Cloud Monitoring API são diferentes de um Prometheus self-hosted — nenhum tratamento de backoff/retry específico está planejado aqui
- Managed Service for Prometheus pode não coletar 100% das métricas que o Prometheus self-hosted da Via Varejo coleta (ex: métricas de conntrack via node-exporter, usadas no Conntrack Viewer/SNAT) — esses módulos não foram avaliados neste plano, só os consumidores listados na seção "Consumidores afetados" acima
- Nenhuma UI para o usuário ver/configurar manualmente algo relacionado a GMP (nem precisa, dado que a Fase 2 é automática) — mas também não há UI para diagnosticar "por que o GMP não está respondendo" além dos logs do servidor
