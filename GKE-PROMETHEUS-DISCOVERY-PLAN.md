# Descoberta de Prometheus em Clusters GKE (Google Cloud Managed Service for Prometheus)

Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

## ✅ Bug lateral corrigido durante a Fase 5 — cache de token GKE não se autorrecuperava

Achado durante a validação ao vivo (ver Fase 5 abaixo): o token OAuth2 GKE cacheado em `GetFreshGKEToken` (`internal/cloudprovider/gcp/auth.go`, TTL 45min) ficou inválido bem antes do TTL expirar — provável corrida entre chamadas concorrentes ao `gcloud` CLI local durante os testes desta sessão. Nada detectava/corrigia isso: toda autenticação GKE (client K8s + Prometheus GMP, que reusam o mesmo cache) ficou quebrada até um restart manual do processo.

Corrigido (commit `b8c647a7`): `InvalidateGKETokenCache()` + `gkeTokenRoundTripper` (mesmo padrão de `eksTokenRoundTripper` já existente — client K8s parava de usar `restConfig.BearerToken` estático, passa a reescrever o header a cada requisição) + `gcpAuthRoundTripper` (GMP, `internal/monitoring/discovery/gcp_auth.go`) — ambos invalidam o cache e tentam de novo uma vez ao receber 401. Validado ao vivo: depois do fix, um restart teve só 1 `401` (corrida de boot benigna, hooks ainda não registrados) e zero depois disso — métricas que falharam no boot se recuperaram sozinhas sem restart manual. Detalhe completo no commit message.

## ✅ Fase 6 — Prometheus in-cluster via port-forward nativo (SPDY), quando GMP não tem os dados

Motivado por uma pergunta direta do usuário depois da Fase 5: o cluster `gke-higgs-hlg` tem, no namespace `monitoring`, um `kube-prometheus-stack` **completo** (`prometheus-prometheus-prometheus-0`, Alertmanager, node-exporter — instalação real do `prometheus-operator`), com ServiceMonitors próprios já cobrindo `kube_horizontalpodautoscaler_*`/`kube_pod_container_resource_*` (confirmado via port-forward manual de teste: `count(kube_pod_container_resource_requests)` = 195 séries reais). O problema nunca foi falta de dado no cluster — foi só o GMP (Fase 2-5) não enxergar esse Prometheus específico (só o addon reduzido do GKE), e esse Prometheus real não ter Ingress externo (`ClusterIP` only).

Implementado (commit `86724736`): `internal/config/portforward.go` (`KubeConfigManager.OpenPortForward`) abre um túnel SPDY via `client-go` (`k8s.io/client-go/tools/portforward` + `transport/spdy` — mesma tecnologia por trás de `kubectl port-forward`, usada como biblioteca), resolvendo o pod Running por trás do Service pelo próprio selector do Service. Cacheado por `cluster+namespace+service+port` com TTL de idle (30min) — não abre um túnel novo por request.

`internal/monitoring/discovery/portforward.go`: `PortForwardTarget` + hook `SetPortForwardFunc`, mesmo padrão de inversão de dependência de `SetGCPTokenFunc` (import cycle real, `discovery` não pode importar `internal/config`). Ligado incondicionalmente em `DiscoverClusters` (não só GKE — o override novo é suportado pelos 3 providers).

Novos campos de override manual `prometheusInClusterNamespace`/`Service`/`Port` em `ClusterConfig`/`EKSClusterConfig`/`GKEClusterConfig` — mesma tier de `prometheusUrl` (Fase 1). `resolvePrometheusSource` ganhou essa 3ª prioridade entre o override de URL e o GMP automático.

**Zero mudança nos clientes Prometheus existentes** (`internal/monitoring/client`, `internal/monitoring/prometheus`) — a resolução do túnel acontece inteiramente dentro de `discovery.DiscoverEndpoint`/`GetPrometheusURL`, que entrega uma URL pronta (`http://127.0.0.1:<porta>/`) pros construtores já existentes, exatamente como fariam com qualquer URL externa.

**Validado ao vivo** (configurado manualmente em `~/.k8s-hpa-manager/gke-clusters-config.json`, fora do repo): túnel abriu, pod certo resolvido, `kube_horizontalpodautoscaler_status_current_replicas`/`kube_pod_container_resource_requests` retornam dado real através do túnel. **Bônus não planejado**: `/api/v1/alerts` também passou a funcionar pra esse cluster (é um Prometheus real, diferente do GMP — a limitação estrutural documentada na Fase 4 pro `internal/monitoring/alerts` não se aplica aqui). AKS continuou 100% inalterado em paralelo.

**Não resolvido**: `internal/finops` (4º grupo de clientes Prometheus, Fase 4) ainda não usa nenhum dos dois mecanismos (GMP nem port-forward) — continua limitado a `discovery.GetPrometheusURL(cluster)` puro. Nenhuma UI pra configurar o override de port-forward — só editando o JSON manualmente, mesma limitação já documentada pro override de URL da Fase 1.

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

### Fase 5 — Validação end-to-end contra cluster GKE real (`gke_via-gcb-higgs-hlg_southamerica-east1_gke-higgs-hlg`, projeto `via-gcb-higgs-hlg`)

**Atenção — lição operacional que gerou o primeiro falso positivo desta fase**: depois de implementar as Fases 2-4, o teste inicial do usuário bateu na URL antiga quebrada (`prometheus-gke_via-...-hlg.viavarejo.com.br`). Causa: o processo do servidor rodando (PID antigo, iniciado antes desta sessão) nunca recarrega o binário sozinho — `make build` só atualiza o arquivo em disco. Precisou `kill <PID antigo> && ./build/new-k8s-hpa web ...` pra o código novo entrar em vigor. Isso já está documentado na tabela de Troubleshooting do `CLAUDE.md`, mas vale reforçar aqui: **sempre reiniciar o servidor depois de build, antes de testar qualquer fase deste plano**.

**Mecanismo (auth + URL + transporte HTTP) — ✅ confirmado com dados reais, via curl direto usando `gcloud auth print-access-token`** (mesmo mecanismo de fallback que `GetFreshGKEToken` usa quando não há `~/.k8s-hpa-manager/gcp-adc.json` — só esse fallback existe neste ambiente, confirmado por `ls` retornando "No such file"):
- `GET .../api/v1/query?query=up` → HTTP 200, `"status":"success"`, com séries reais (`job="gmp-kubelet-cadvisor"`, `job="gmp-kubelet-metrics"`, `job="kube-state-metrics"`, todas do cluster `gke-higgs-hlg`)
- `count(container_cpu_usage_seconds_total)` e `count(container_memory_working_set_bytes)` → 261 séries cada (métricas de container via kubelet/cAdvisor, GKE expõe isso ao GMP automaticamente, sem PodMonitoring manual)
- Confirma: Project ID extraído certo do context, token OAuth2 aceito pelo Google, `resolvePrometheusSource`/`GCPAuthTransport`/`validateEndpoint` funcionando ponta a ponta

**Dado de aplicação (o que o app realmente precisa: HPA, resources, deployment status) — ❌ ausente, causa raiz identificada e não é bug de código**:
- `kube_horizontalpodautoscaler_status_current_replicas`, `kube_horizontalpodautoscaler_spec_max_replicas`, `kube_pod_container_resource_requests`, `kube_pod_container_resource_limits`, `kube_pod_info`, `kube_node_info`, `kube_deployment_status_replicas` (sem sufixo) → todas retornam `"result":[]` (vazio, não erro)
- Outras como `kube_deployment_status_replicas_available`, `kube_pod_status_phase`, `kube_statefulset_status_replicas_ready`, `kube_daemonset_status_*`, `kube_persistentvolume*` → **têm dado real** (confirmado `count(kube_pod_status_phase)` = 120 séries)
- **Causa raiz**: o cluster tem **dois** kube-state-metrics coexistindo:
  1. `gke-managed-cim/kube-state-metrics-0` — addon **gerenciado pelo GKE** (`components.gke.io/component-name: cluster-infra-metrics`), já tem um `ClusterPodMonitoring` (`kubectl get clusterpodmonitoring kube-state-metrics -o yaml`) escaneando a porta nomeada `k8s-objects`, mas expõe só um **subconjunto curado** de métricas (o que aparece no catálogo `/api/v1/label/__name__/values` acima) — não inclui HPA nem resource requests/limits.
  2. `monitoring/prometheus-kube-state-metrics-...` — kube-state-metrics **completo e padrão** (Helm chart `kube-prometheus-stack`, `kube-state-metrics-5.6.2`, versão `2.8.2`), Service `prometheus-kube-state-metrics.monitoring.svc:8080` (porta nomeada `http`), criado há só 8h no momento do teste — **mas sem nenhum `PodMonitoring`/`ClusterPodMonitoring` apontando pra ele** (`kubectl get podmonitoring -A` → `No resources found`). GMP simplesmente nunca sabe que esse Pod existe.
- **Correção identificada, pronta pra aplicar quando aprovado** (aditiva, não mexe em nada existente — só adiciona um novo alvo de scrape):
  ```yaml
  apiVersion: monitoring.googleapis.com/v1
  kind: PodMonitoring
  metadata:
    name: kube-state-metrics
    namespace: monitoring
  spec:
    selector:
      matchLabels:
        app.kubernetes.io/name: kube-state-metrics
        app.kubernetes.io/instance: prometheus
    endpoints:
    - port: http
      interval: 30s
  ```
  `kubectl apply -f` isso no cluster GKE deve fazer o coletor gerenciado do GMP começar a escanear `prometheus-kube-state-metrics:8080/metrics` no próximo ciclo — métricas de HPA/resources devem aparecer em ~1-2min. **Usuário optou por não aplicar nesta sessão** (2026-07-31) — decisão de infraestrutura em cluster real, não de código desta app.
- [ ] Aplicar o `PodMonitoring` acima (ou equivalente `ClusterPodMonitoring` se o padrão preferido da organização for cluster-wide) e reconfirmar as queries que hoje retornam vazio
- [ ] Depois de aplicado: revalidar `internal/monitoring/client`/`internal/monitoring/prometheus` na UI real (HPA metrics, Deployment Behavior, Node Pool predictions) — não só via curl direto
- [ ] Confirmar se esse gap (dois kube-state-metrics, só o "reduzido" com PodMonitoring) é específico deste cluster ou padrão em todos os clusters GKE da frota — se for padrão, o `PodMonitoring` acima precisa ser aplicado em cada cluster GKE novo (ou automatizado via discovery/autodiscover desta app — fora de escopo deste plano)
- [ ] Verificar se o formato de resposta do GMP bate 100% com os structs `QueryResult`/`QueryRangeResult` já existentes para métricas com dado real (histogram_quantile, matrix multi-série) — só testado com vetores simples (`up`, `count(...)`) até aqui

### Riscos conhecidos / não cobertos por este plano

- Rate limits/quotas do Cloud Monitoring API são diferentes de um Prometheus self-hosted — nenhum tratamento de backoff/retry específico está planejado aqui
- Managed Service for Prometheus pode não coletar 100% das métricas que o Prometheus self-hosted da Via Varejo coleta (ex: métricas de conntrack via node-exporter, usadas no Conntrack Viewer/SNAT) — esses módulos não foram avaliados neste plano, só os consumidores listados na seção "Consumidores afetados" acima
- Nenhuma UI para o usuário ver/configurar manualmente algo relacionado a GMP (nem precisa, dado que a Fase 2 é automática) — mas também não há UI para diagnosticar "por que o GMP não está respondendo" além dos logs do servidor
