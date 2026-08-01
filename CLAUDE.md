# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**New K8s HPA Manager** é uma ferramenta de gerenciamento de recursos Kubernetes/Azure AKS (com suporte também a EKS/GKE) em larga escala, com duas interfaces: **TUI** (Bubble Tea) e **Web** (Go/Gin API + React/TypeScript SPA). Cobre desde edição de HPAs/Node Pools em lote até Health Check com IA, FinOps, Access Checker, Code Editor com integração Git/LSP, e integrações com Dynatrace/ServiceNow/Teams.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Após `make build`, sempre reiniciar o servidor (`kill <PID> && ./build/new-k8s-hpa web -f`) — o processo não recarrega o binário automaticamente.
**IMPORTANTE**: Ao fazer alterações no frontend (React/TypeScript), sempre rebuild com `./rebuild-web.sh -b` E fazer hard refresh no navegador (Ctrl+Shift+R).

Todas as features abaixo já estão mescladas na `main` (verificado via `git merge-base`) e documentadas na seção `##`/`###` correspondente deste arquivo: Code Editor (Fases 1-10, incl. Source Control e integração K8s), Diagnóstico SNAT multi-cloud, Dynatrace + correlação K8s, Access Checker (`AccessCheckTab.tsx`), FinOps Storage, Teams/SRE Approval, JWT, RBAC K8s via `SelfSubjectRulesReview`, HPAEditor, Conntrack Viewer com comparação D-1/D-2/D-3, Resync AKV, CloudAccountHintField, sincronização de foco entre painel-lista e painel-tabela (`useRevealOnKeyChange`), drill-down de pods no DaemonSetMonitorTable, aba "Mesma Imagem" no `PodQuickViewModal`. Para o histórico de qual branch trouxe cada uma (com contexto e bugs corrigidos), ver [docs/history/CHANGELOG.md](docs/history/CHANGELOG.md).

### Requisitos

| Obrigatório | Opcional |
|-------------|----------|
| Go 1.25+ (compilação) | Azure CLI (Node Pools AKS) / AWS CLI (EKS) / gcloud (GKE) |
| kubectl configurado | Prometheus (métricas + Conntrack histórico) |
| Git | Ollama local ou API key Claude/Gemini/OpenAI (AI Diagnostics) |
| | Kiali/Istio (Service Mesh) |
| | Chrome/Edge do Windows via CDP (ServiceNow SSO, Teams — WSL2) |

---

## Documentação Modular

- [Quick Start & Features](docs/guides/QUICK_START.md)
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md)
- [Architecture Overview](docs/architecture/OVERVIEW.md)
- [Web Interface Guide](docs/guides/WEB_INTERFACE.md)
- [Common Pitfalls](docs/guides/COMMON_PITFALLS.md)
- [Troubleshooting Completo](docs/guides/TROUBLESHOOTING.md)
- [RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md)
- [GitHub Models como alternativa ao Vertex AI](docs/guides/GITHUB_MODELS_SETUP.md) — quando o Vertex AI corporativo não tem permissão IAM liberada, usar o provider OpenAI com Base URL customizada apontando pro GitHub Models (mesmo PAT do `gh` CLI)
- [Changelog](docs/history/CHANGELOG.md)
- [**Plano: Dynatrace × Health Check**](docs/planning/DYNATRACE_HEALTHCHECK_INTEGRATION.md) ← work in progress
- [**Plano: FinOps Storage**](docs/planning/FINOPS_STORAGE_PLAN.md) ← ✅ CONCLUÍDA — PVCs, discos OS dos nodes, Azure Files/Blob, Relatório Executivo integrado
- [**Plano: FinOps DT Metrics**](FINOPS-DT-METRICS.md) ← ✅ Fases 1-4 concluídas — DT como fonte primária, Prometheus parcial
- [**Plano: FinOps NR Metrics**](FINOPS-NR-METRICS.md) ← work in progress — New Relic para clusters EKS (nenhuma fase iniciada)
- [**Plano: Descoberta de Prometheus em Clusters GKE**](GKE-PROMETHEUS-DISCOVERY-PLAN.md) ← ✅ Fases 1-6 concluídas — GMP (Google Cloud Managed Service for Prometheus) funciona ponta a ponta (URL automática a partir do Project ID do context GKE, auth OAuth2 Bearer via hook `discovery.SetGCPTokenFunc`), mas descobriu-se que o cluster de teste (`gke-higgs-hlg`) tem o PodMonitoring gerenciado pelo GKE reduzido (sem HPA/resource requests) — resolvido na **Fase 6** com um mecanismo alternativo: `KubeConfigManager.OpenPortForward` (`internal/config/portforward.go`) abre um túnel SPDY nativo via `client-go` (mesma tecnologia de `kubectl port-forward`, como biblioteca) pra alcançar um Prometheus real in-cluster (`kube-prometheus-stack` completo, `ClusterIP` sem Ingress) — hook `discovery.SetPortForwardFunc` (mesmo padrão de inversão de dependência de `SetGCPTokenFunc`) + override manual `prometheusInClusterNamespace/Service/Port` nos 3 configs de cluster. Validado ao vivo: dado real de HPA/resources fluindo pelo túnel, e bônus não planejado — `/api/v1/alerts` também passou a funcionar (Prometheus real, não GMP). `internal/finops` (4º grupo de clientes Prometheus, antes descoberto sem cobertura) recebeu o mesmo tratamento de auth/túnel na Fase 4 — validado ao vivo com `req_millis`/`req_mi` reais retornados pelo endpoint de timeline
- [**Plano: FinOps Isenções**](FINOPS-EXEMPTIONS-PLAN.md) ← work in progress — whitelist por workload com threshold de réplicas (nenhuma fase iniciada)
- [**Plano: Cluster Discovery AKS+EKS**](CLUSTER-DISCOVERY-PLAN.md) ← ✅ Fases 1-5 concluídas — discovery paralelo, config EKS separada, semáforos ampliados, frontend com badges AKS/EKS
- [**Plano: Verificar Acesso (Access Checker)**](ACCESS-CHECK-PLAN.md) ← ⚠️ Revisão 7 pendente de validação real — checa acesso de analista via impersonation K8s + grupos AAD `VV_CLOUD*` resolvidos por `az ad user get-member-groups` (sem Graph API); detecta também acesso admin via IAM do Azure (bypass de RBAC, invisível à impersonation); scan de frota usa `SelfSubjectAccessReview` varrendo todos os namespaces
- [**Plano: Teste de Latência sob Demanda**](LATENCY-METRICS-PLAN.md) ← ✅ Fases 1-7 concluídas — teste ativo (pod efêmero + curl/ping via exec) com guardrails, contexto histórico DT/Prometheus (P95/P99), grafo de topologia (Cytoscape.js) e correlação de breach de latência no Health Check. Fase 7 validada só por teste unitário, não por navegador
- [**Plano: Teste de Kafka sob Demanda**](KAFKA-TEST-PLAN.md) ← ✅ implementado — aba "Teste Kafka": TCP/protocolo/SASL/produce-consume/visualizar mensagens contra broker externo, modos `pod` (Ephemeral Container `kcat`) ou `local` (Docker no host, mesmo padrão do Teste de Banco de Dados — pré-checagem + reaper compartilhados via `db_test_docker.go`). Botão "Visão geral de tópicos" mostra Tópico/Partições/~Mensagens (soma latest-earliest por partição) numa tabela ordenável, estilo "All Stats" do MongoDB Compass; no modo `local` ganha também coluna de tamanho em disco real via `kafka-log-dirs` numa imagem completa do Kafka (`confluentinc/cp-kafka`) — só nesse modo porque no `pod` a imagem pesaria no armazenamento do node do cluster a cada Ephemeral Container. Seletor de Pod/Container (`resolvePodForDeployment`, endpoint `GET /kafka-test/pods`) deixa escolher qual réplica recebe o Ephemeral Container em vez de sempre pegar o primeiro pod Running; mecanismo SASL `OAUTHBEARER` (Azure AD/service principal, `sasl.oauthbearer.method=oidc`) para Event Hub sem connection string — exigiu trocar a imagem `kafkaTestPodImage` de `edenhill/kcat:1.7.1` para `ueisele/kcat:1.7.1-librdkafka2.1.1` (mesma versão do kcat, rebuild de terceiro com librdkafka 2.1.1, que traz o método `oidc` do OAUTHBEARER ausente na 1.8.2 da imagem oficial — confirmado empiricamente via `docker run`, não só por documentação). Ver seção própria "Kafka Test Tool" abaixo
- [**Plano: Maturidade do Relatório de IA do Health Check**](HEALTHCHECK-AI-REPORT-MATURITY-PLAN.md) ← ✅ CONCLUÍDA (4 fases) — prompt estruturado com emoji de severidade/citação verbatim/3 baldes de urgência; heurística crônico/agudo via novo histórico SQLite `health_check_event_history`; métricas CPU/memória do Dynatrace não são mais descartadas; `NodeChecker` — antes órfão — agora roda via checkbox "Capacidade dos Nós"; comparação uso real vs. request via `ResourceEnricher` próprio do Health Check (P95 Prometheus, checkbox "Uso Real vs. Request", mesmos limiares do `finops/prometheus_enricher.go`, revelou de quebra que `DeploymentHealth.CPUUsagePercent`/`MemoryUsagePercent` ao vivo via metrics-server também estavam descartados); 10ª aba "Relatório" em `HealthCheckResultsPanel.tsx` (`HealthReportTab.tsx`) com tabela-resumo priorizada por severidade, tabela de nós e badges de crônico/veredicto de recursos — substitui o texto de IA solto por uma visão estruturada, sem depender de chamar a IA
- [**Plano: Verificação de Espaços em Branco em Secrets**](SECRETS-WHITESPACE-CHECK-PLAN.md) ← ✅ implementado — detecta espaço/tab/newline/CR na borda (início/fim) de valores base64 decodificados na aba Secrets, exclui certificados/chaves (PEM legitimamente termina em `\n`). Gatilho: menu de contexto do Monaco em qualquer linha do editor (`editor.addAction`, só aparece quando `kind: Secret` via context key `isSecretYaml`); indicação visual via `IEditorDecorationsCollection` (fundo âmbar + ícone ⚠ na margem + hover com prévia do valor decodificado) + toast-resumo. Puramente client-side (decode em memória, nunca escreve no editor nem chama o backend); decorations somem ao editar o conteúdo, exigindo novo scan
- [**Plano: Teste de Banco de Dados sob Demanda**](DATABASE-TEST-PLAN.md) ← ✅ implementado — aba "Teste de Banco de Dados": PostgreSQL/MySQL-MariaDB/MongoDB/Redis via registry de engines, conectividade+autenticação (manual ou lida de Secret/ConfigMap) e navegação só-leitura opcional; modos `pod` (Ephemeral Container) ou `local` (Docker no host, com pré-checagem de Docker + reaper de containers órfãos). Checkbox "Valores no Secret estão em Base64" (`DBSecretRef.Base64Decode`) para credenciais sincronizadas via Azure Key Vault/external-secrets já em base64 — mesmo campo/lógica do Kafka (`decodeSecretValueBase64`, compartilhada entre `db_test_tool.go` e `kafka_test_tool.go`). Tabela de estatísticas estilo Compass (Size/Storage Size/Count) + amostra de dados real (Preview) com layout mestre-detalhe, paginação e ordenação por coluna (teclado incluído) para Postgres/MySQL/Mongo; Redis tem paginação real só para list/zset (sem conceito de coluna/ordenação — estrutura chave-valor). `effectiveDatabase`/`connStringDatabase` resolvem o banco embutido na connection string sem exigir campo duplicado; mecanismo de autenticação Mongo (SCRAM-SHA-1/256) configurável; timeout do estágio Browse tem piso de 30s (`dbTestBrowseMinTimeoutMs`) — maior que o de conectividade, necessário para `$collStats` em bancos com muitas collections. **Nunca escreve no banco**. Fluxo Mongo (stats + preview + auth SCRAM + fallback de connection string) validado ponta a ponta contra um MongoDB real nesta rodada; Postgres/MySQL/Redis seguem validados só por build/testes unitários
- [**Plano: Botão "Notas"**](NOTES-PLAN.md) ← ✅ implementado — anotações em Markdown escopadas por cluster+aba ativa, modelo de diário (cada "Salvar" cria uma entrada nova, nunca sobrescreve). Ver seção própria "Notas (anotações Markdown por cluster+aba)" abaixo
- [**Plano: Gráfico de Comportamento do Deployment + Indicador Dynatrace na aba Pods**](DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md) ← work in progress — nenhuma fase iniciada. Fase 0: ícone de status de monitoramento Dynatrace por pod (monitorado/não-monitorado/não-aplicável) nos painéis esquerdo e direito da aba Pods. Fases 1-3: nova aba "Comportamento" no `PodQuickViewModal.tsx` com gráfico histórico (réplicas/CPU/mem/restarts) do Deployment dono do pod, Prometheus como fonte primária com fallback real para Dynatrace em clusters AKS sem Prometheus instalado, overlay de problems DT e comparação D-1/D-2/D-3 (mesmo padrão do Conntrack Viewer)
- [**Plano: Validação de Cadeia de Certificados + Rollback de Atualizações TLS**](CERT-ROLLBACK-VALIDATION-PLAN.md) ← ✅ Fases 1-8 concluídas — validação de cadeia via Go nativo (`crypto/x509`/`crypto/tls`, sem depender de binário `openssl` — imagem de produção é `FROM scratch`), rollback (`~/.k8s-hpa-manager/rollback-certs/<secret>/<data>/`) cobrindo upload manual e o fluxo AWX (que não passa pelo `Scanner.UploadCertificate`), e enriquecimento via Prometheus (`nginx_ingress_controller_ssl_*`) detectando propagação incompleta pro ingress-nginx — não se aplica a GKE Gateway API (TLS termina em Load Balancer gerenciado da GCP, sem pod pra Prometheus fazer scrape; equivalente seria a API da GCP, não investigado ainda). Fase 4 corrigiu 6 bugs reais achados via uso: falso-positivo na checagem de ordem da cadeia (bundles reais tipo Sectigo/USERTrust trazem intermediário/raiz fora da ordem canônica, sem quebrar o TLS de verdade), footer de modal sem `flex-wrap`, "Atualização em Massa" sem opção AWX, e **duas ocorrências do mesmo bug de stale closure** (`AWXCertForm.tsx`'s `es.onerror` e `CertificateRollbackModal.tsx`'s `AlertDialog.onOpenChange` checavam `useState` dentro de callback assíncrono disparado por API de terceiro — SSE/Radix —, sempre lendo o valor de ANTES do clique; corrigido com `useRef`/variável local nos dois). Fase 5: modal AWX fechava sozinho ao concluir o job (escondia logs/status já renderizados pelo `AWXCertForm` — não era stale closure, era só fechamento automático demais cedo; corrigido removendo o auto-close e trocando o botão pra "Fechar" em estado terminal), e `CertificateInfo` ganhou `ChainValidation` — `Scanner.scanCluster` já chama `ValidateCertificateChain` (sem alterá-la) por certificado durante o scan da frota, pré-populando `CertificateDetailModal` sem precisar clicar em "Validar Cadeia" (deliberadamente sem o enriquecimento Prometheus da Fase 3, que multiplicaria consultas por certificado escaneado). Fase 6: `ManualBackupStore` (`~/.k8s-hpa-manager/manual-cert-backups/<secret>/<data>/`) — mecanismo de backup separado do Rollback, disparado sob demanda (botão "Copiar para Backup" no detalhe do cert), com comentário opcional e `ListSecretsWithBackups()` navegável entre QUALQUER secret com backup (não só o atual); `CertificateSourcePickerModal.tsx` (2 abas — Rollback e Backup Apartado, cada uma com busca client-side) integrado nos dois modais manuais (Secrets e Certificados TLS), populando os campos de PEM a partir de um backup escolhido sem precisar colar. Cada backup manual listado no picker ganhou 2 ações inline: **editar nota** (`ManualBackupStore.UpdateComment`, só troca o comentário na `metadata.json`, não mexe no PEM) e **remover** (`ManualBackupStore.Delete`, exclui `tls.crt`/`tls.key`/`metadata.json` do disco — ação destrutiva, sempre atrás de `AlertDialog` de confirmação, mesmo padrão de `LoadSessionModal.tsx`/`CertificateRollbackModal.tsx`); rotas `PUT`/`DELETE` em `/certificates/manual-backups/:secretName/:backupId` atrás de `RequireSREGroup()`. Validado em navegador contra um backup real: editar reflete o novo texto na lista sem reload (toast "Nota atualizada"), remover mostra o dialog de confirmação com data/subject do backup e some da lista após confirmar. **Fase 7 — handshake TLS direto e universal, motivado pela migração da frota de ingress-nginx para Gateway API** (já em uso hoje em clusters GKE, onde o TLS termina num Load Balancer gerenciado da GCP — sem pod nenhum pra Prometheus fazer scrape, tornando "achar uma métrica equivalente" inviável; mesmo problema existiria pra Istio Gateway, que só expõe um gauge global sem correlação por Secret, e Traefik/HAProxy não têm evidência de uso real na frota): `EnrichWithTLSDial` (`internal/certificates/tls_dial_enrich.go`) conecta de verdade (`crypto/tls.Dial` com SNI) no(s) hostname(s) público(s) do Secret e compara o serial do certificado real servido — funciona independente de quem termina o TLS. Só roda como **fallback**, quando `EnrichWithPrometheus`/nginx não conseguiu checar (`Checked=false`) — nunca substitui nem altera o path nginx, que continua tentado primeiro por ser mais rico (dados por pod) e mais barato. Hosts resolvidos combinando Ingress (`resolveIngressHosts`, `internal/certificates/ingress_hosts.go`) e Gateway API (`resolveGatewayHosts`/`hostnameMatchesListener` com matching de wildcard, `internal/certificates/gateway_hosts.go` + `ListGatewayListenerCerts`/`ListHTTPRouteBindings` em `internal/kubernetes/gateway_query.go` — parsing tipado via `kubectl get gateway/httproute -o json`, já que não há dynamic client nem cliente `sigs.k8s.io/gateway-api` no vendor). Novo campo `LivePropagationResult.Method` (`"prometheus-nginx"` | `"tls-dial"`) identifica qual mecanismo respondeu, usado pelo frontend pra trocar o texto ("réplica do ingress-nginx" vs "host... handshake TLS direto"). **Bug de silêncio corrigido no mesmo lote**: `CertificateChainValidationPanel.tsx` só renderizava o bloco de propagação quando `checked=true` — quando a checagem falhava por qualquer motivo (`Checked=false`), as `Notes` explicativas que o backend já calculava (ex: "Prometheus indisponível", "Secret não referenciado por Ingress") nunca apareciam, indistinguível de "recurso nunca implementado"; painel agora mostra essas notas nesse caso. **`CertificateDetailModal.tsx` passou a disparar a validação completa (com `LivePropagation`) automaticamente ao abrir o modal** — antes só rodava sob clique manual em "Validar Cadeia" (o resultado pré-populado do scan nunca incluiu propagação, de propósito, pra não multiplicar consultas durante o scan da frota); like a Fase 3 original, abrir UM certificado por vez continua barato o suficiente pra não precisar desse cuidado. Validado em navegador contra um Secret TLS real (`via-tls`/`abastecimento-hlg`, cluster AKS `akspriv-abastecimento-hlg`): Prometheus foi descoberto e consultado com sucesso, mas a query `nginx_ingress_controller_ssl_certificate_info` voltou vazia (cluster não roda esse exporter) → fallback acionado → handshake TLS conectou nos 2 hosts reais dos Ingresses e confirmou "2/2 host(s) respondendo com o certificado atual" — primeira validação visual real desta feature desde a Fase 1 (Fases 1-6 nunca tinham sido testadas em navegador por falta de ferramenta de automação nas sessões anteriores). **Fase 8 — detecção de erros de configuração entre certificado e Ingress/Gateway API** (pedido explícito do usuário: "identificar erros de certificado entre a aplicação e o ingress"), 3 mecanismos: **(A) SAN não cobre o host** — `certSANCoversHost`/`matchesWildcardHost` (`internal/certificates/config_issues.go`/`hostmatch.go`, wildcard case-insensitive — SANs de certificado não são garantidamente lowercase, ao contrário de hosts K8s) comparam `CertificateInfo.DNSNames` contra os hosts de cada Ingress/Gateway; **(B) conflito de host** — `detectHostConflicts`/`groupConflictingOwners` agrupam Ingresses por `IngressClass` (evita falso-positivo em clusters com múltiplos controllers coexistindo) mas **ignoram a separação por classe quando um Gateway está envolvido** — é justamente o cenário real e valioso de pegar (Ingress legado + Gateway novo disputando o mesmo host durante a migração da Fase 7); **(C) falha de TLS entre ingress-controller e pod backend** (re-encryption via `backend-protocol: HTTPS`/`GRPCS`/`ssl-passthrough`) — sem métrica/API pra isso, só logs do próprio controller, por isso dividido em 2 camadas: um aviso **determinístico e de custo zero** anexado durante o scan (lê só a annotation) avisando que a superfície de risco existe, mais um botão explícito **"Diagnóstico Avançado"** (`CheckIngressBackendTLS`/`analyzeBackendTLSLogs`, `internal/certificates/backend_tls_check.go`) que acha os pods do ingress-controller (`FindIngressControllerPods`, busca por label em qualquer namespace — não assume `ingress-nginx` fixo) e faz regex nos últimos 20min de log (`SinceSeconds`, não `TailLines` — mais robusto a variação de volume de tráfego) procurando `x509: certificate signed by unknown authority`/`SSL_do_handshake() failed`/etc. correlacionados ao host. **Fraseologia deliberada**: nunca "confirmado sem erro" — só "sinal encontrado" vs. "nenhum sinal na janela analisada (não confirma ausência do problema)", mesmo espírito de `TrustedByPublicCA=false` tratado como neutro em `ChainValidationResult`. Resultado modelado por `HostIssue{Host,Severity,Message}` dentro de `IngressRef`/`GatewayRef` (não uma lista solta em `CertificateInfo`) — permite ao usuário saber exatamente qual Ingress/host tem qual problema, com um `CertificateInfo.HasConfigIssues` derivado só pra badge/filtro ("Conflito de Host/SAN") da listagem. Gateway API também passou a ser cruzada durante o scan em lote (`Scanner.crossRefGateways`, 1x por cluster via `ListGatewayListenerCerts`/`ListHTTPRouteBindings` já existentes da Fase 7 — não mais 1x por secret, que multiplicaria subprocessos `kubectl`), populando `CertificateInfo.UsedByGateways`. Efeito colateral: `buildIngressSummary` (`internal/kubernetes/client.go`) ganhou fallback pra annotation legada `kubernetes.io/ingress.class` quando `Spec.IngressClassName` é nil — beneficia também `IngressTab.tsx`. Validado em navegador contra o mesmo Secret real da Fase 7 (`via-tls`): badge "nginx" de IngressClass aparece corretamente em cada Ingress, filtro "Conflito de Host/SAN (0)" aparece na lista sem falso-positivo (SAN `*.via.com.br` cobre os 2 hosts reais, nenhuma classe conflitante).

**Bug real corrigido — Fases 7/8 totalmente quebradas em clusters EKS**: `internal/web/frontend/src/hooks/useCertificates.ts` monta as URLs de `validate-chain`/`backend-tls-check`/`backup`/`rollback`/`manual-backup` interpolando `cluster`/`namespace`/`name` direto na string, sem `encodeURIComponent` — diferente de `lib/api/client.ts` (usado pelo resto da app), que já faz isso em ~99 chamadas. Nomes de cluster AKS/GKE são identificadores simples e nunca expuseram o problema, mas nomes de cluster EKS são **ARNs completos** (`arn:aws:eks:<região>:<conta>:cluster/<nome>`), cheios de `:` e `/`. Como esses caracteres quebram o roteamento de path do Gin (o segmento `:cluster` do router para de bater no valor inteiro assim que encontra a primeira `/` do ARN), toda chamada que usa cluster como segmento de path 404ava silenciosamente em qualquer cluster EKS — e a Fase 7 (validação automática ao abrir o modal) tornou isso muito mais grave: o painel de cadeia/propagação simplesmente nunca aparecia pra nenhum certificado EKS, sem erro visível (o `catch` do handler só zera o estado). Confirmado contra um cluster EKS real (`arn:aws:eks:us-east-1:...:cluster/asaplog-production`): 404 antes da correção, painel completo funcionando depois.

**Achado real corrigido — falso-positivo "propagação em andamento" quando há CDN/proxy corporativo na frente do cluster**: no mesmo cluster EKS, o handshake TLS direto (Fase 7) reportava "13/13 hosts servindo certificado diferente do atual — propagação em andamento" pro Secret `asaplog-tls` (emissor Sectigo) — confirmado via `openssl s_client` direto que o host público na verdade serve um certificado corporativo wildcard da DigiCert (`*.via.com.br`/`*.viavarejo.com.br`/`*.grupocasasbahia.com.br`/etc.), não o Secret do cluster. Existe uma camada (CDN/WAF/proxy corporativo) na frente do `ingress-nginx` do EKS terminando TLS antes do tráfego chegar no cluster — o Secret é real e válido, só nunca é o que o cliente público recebe, então a mensagem de "propagação" era enganosa (nunca vai se resolver sozinha, porque não é uma propagação de verdade). Corrigido: `buildTLSDialResult` (`tls_dial_enrich.go`) agora compara o **emissor** (Issuer CN) do certificado realmente servido com o emissor esperado (`LeafIssuerCN`, nova função em `prometheus_enrich.go`) — emissor completamente diferente vira `LivePropagationResult.PossibleExternalLayer` (nota neutra explicando a arquitetura), emissor igual (ou indisponível pra comparar, fallback conservador) continua em `ReplicasStale` com a mensagem de propagação original. `CertificateChainValidationPanel.tsx` renderiza os dois blocos com tom visual distinto (âmbar/alarmante para propagação genuína, azul/neutro com ícone `Info` pra camada externa). Validado em navegador contra o mesmo cluster real: mensagem "camada externa" aparece corretamente, "Propagação em andamento" não aparece mais para este Secret.

---

## Comandos Essenciais

```bash
# Build
make build                    # Compilar backend Go (BUILD_PARALLEL=2 por padrão — WSL2 RAM)
make build BUILD_PARALLEL=4   # Override parallelismo se RAM disponível (>8GB livres)
./rebuild-web.sh -b           # Build frontend + backend + reinicia servidor em background (RECOMENDADO após mudanças React)
./rebuild-web.sh -n -b        # Reinicia servidor em background SEM rebuild (apenas restart)
./rebuild-web.sh -k           # Apenas mata o processo na porta 8080
./rebuild-web.sh -s           # Verifica se o servidor está rodando
./rebuild-web.sh -b --ai-provider ollama --ollama-model llama3.2:3b  # Com AI provider
make build-web                # Build completo (frontend + backend)

# Discovery
./build/new-k8s-hpa autodiscover   # Descobre clusters AKS+EKS+GKE em paralelo (salva configs separadas)

# Run
./build/new-k8s-hpa web       # Servidor web (porta 8080)
./build/new-k8s-hpa web -f    # Foreground mode (logs no terminal)
./build/new-k8s-hpa web --ad  # EMERGÊNCIA: Bypass RBAC (flag oculta)
./build/new-k8s-hpa           # TUI padrão
./build/new-k8s-hpa version   # Versão + updates disponíveis
# Atalhos TUI: F1 Ajuda · F3 Logs · F5 Reload · F8 Prometheus · F9 CronJobs · Ctrl+S Salvar sessão · Ctrl+L Carregar sessão · ESC Voltar

# Dev
make web-dev                  # Frontend dev server (Vite HMR - porta 5173)
make run-dev                  # TUI com debug

# Tests
go test -v ./internal/... -race              # Todos os testes com race detector
go test -v ./internal/healthcheck/... -race  # Pacote específico
go test -run TestGetClient ./internal/...    # Função específica em todos os pacotes
./testes/test-rbac.sh                        # Suite completa RBAC (40+ cenários)
SKIP_AZURE_TESTS=1 go test ./...             # Pula testes de Azure AD (usado no CI, útil sem az CLI autenticado)
make test                                    # go test -v ./... sem race/filtro (usado pela CI, ver .github/workflows/ci.yml)

# Debug
tail -f /tmp/k8s-hpa-manager-web-*.log  # Logs do servidor

# Release
make release                  # Build multi-plataforma → build/release/ (linux, darwin Intel, darwin ARM64)
make build-all                # Build multi-plataforma → build/ (sem subpasta release)
make version                  # Mostra versão detectada via git describe + commit atual (smoke-test usado pela CI)
# Publicar release no GitHub (ver seção Release no Fluxo de Desenvolvimento)

# Outros
make test-coverage            # Testes com cobertura HTML
make web-install              # npm install no frontend
make web-clean                # Limpa arquivos de build frontend

# Lint frontend
cd internal/web/frontend && npm run lint   # eslint .
```

### Notas de Build

- **`makefile` usa nome em minúsculas** — não `Makefile`. Ferramentas que procuram `Makefile` com M maiúsculo não encontrarão.
- **GOCACHE** redirecionado para `~/.cache/go-build-wsl` (não `/dev/shm`) — evita OOM em WSL2. Auto-trimado ao passar 1500MB. Para limpar: `go clean -cache`.
- **GOTMPDIR** usa `/tmp/go-tmp` — mesmo motivo (evita consumir RAM pura via `/dev/shm`).

### Antes de Commitar

```bash
go test -v ./internal/... -race  # 1. Testes com race detector
make build                        # 2. Verificar build
./rebuild-web.sh -b               # 3. Se alterou frontend
go fmt ./...                      # 4. Formatting Go
go mod vendor                     # 5. Vendored modules atualizados
```

### Após Mudanças no Frontend

```bash
./rebuild-web.sh -b
# Ctrl+Shift+R no navegador (hard refresh obrigatório)
ls -lh internal/web/static/assets/ | grep -E "\.(js|css)$"  # Verificar assets
```

---

## Estrutura do Projeto

```
k8s-hpa-manager/
├── cmd/                      # CLI commands (Cobra): web.go, autodiscover.go, diagnose.go
├── internal/
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   ├── web/
│   │   ├── frontend/         # React SPA (src/components/, src/hooks/, src/lib/api/)
│   │   ├── handlers/         # Go REST API handlers (um arquivo por recurso)
│   │   ├── sse/              # Server-Sent Events broker
│   │   └── middleware/       # RBAC, CORS
│   ├── kubernetes/           # K8s client wrapper (client.go - métodos centrais)
│   ├── azure/                # Azure SDK auth
│   ├── models/               # types.go - fonte de verdade de todos os tipos
│   ├── config/               # Kubeconfig, cache de clients K8s
│   │                         # + eks_config.go (EKSClusterConfig, load/save)
│   │                         # + eks_discovery.go (AutoDiscoverEKSClusters via AWS CLI)
│   ├── cloudprovider/        # Interface NodeGroupProvider + impls por cloud
│   │   ├── interface.go      # NodeGroupProvider: List/Scale/SetAutoscaling/AbortOperation
│   │   ├── azure/            # AzureNodeGroupProvider (az CLI)
│   │   └── aws/              # AWSNodeGroupProvider (aws CLI, normaliza ARN → nome curto)
│   ├── collectors/           # Coletores K8s: deployment, HPA, pod, node, investigator
│   ├── metrics/              # Cliente Prometheus (prometheus.go)
│   ├── session/              # Sessions TUI ↔ Web (formato JSON compatível)
│   ├── monitoring/           # Prometheus, predictions/, nodepoolpredictions/
│   │   └── engine/           # monitoring_v2.go — discovery automático sem port-forwards
│   ├── auth/                 # JWT: JWTManager (Generate/Validate/IsConfigured/TTL), claims email/name/is_sre
│   ├── rbac/                 # Azure AD RBAC (azure_ad.go)
│   ├── ai/                   # AI Diagnostics (Ollama/Claude/Gemini), reports/
│   ├── aierrors/             # Tipos de erro normalizados para AI providers
│   ├── sanitizer/            # Sanitização de logs antes de enviar para IA
│   ├── storage/              # SQLite: predictions.db, health_check.db, ai_diagnostics.db
│   │                         # + ai_history_store.go, dependency_registry.go, user_tokens_store.go
│   ├── certificates/         # Gerenciamento de certificados TLS
│   ├── dynatrace/            # Integração Dynatrace API v2 (problems, entities, metrics)
│   ├── servicenow/           # Integração ServiceNow
│   ├── healthcheck/          # Health checking: orchestrator, deployment/hpa/event/pv checkers
│   ├── history/              # History tracker
│   ├── logs/                 # Gerenciamento de logs da aplicação
│   ├── notifications/        # Notificações in-app e Windows (WSL2)
│   ├── sreapproval/          # Integração com sistema SRE Approval (devstartcd.via.com.br)
│   ├── teams/                # Extração de CHGs do Mr.ViaBot via browser automation (go-rod)
│   ├── updater/              # Auto-update: verificação de versão no GitHub
│   ├── validation/           # Validação de recursos K8s
│   └── pkg/
│       ├── helm/             # Cliente Helm via CLI
│       └── nexus/            # Cliente Nexus (artefatos)
├── build/                    # Binários compilados
├── vendor/                   # Go modules vendored (go build -mod=vendor)
├── scripts/                  # Scripts de diagnóstico e utilitários
└── docs/                     # Documentação modular
```

**Tech Stack:**
| Categoria | Tecnologia |
|-----------|------------|
| Backend | Go 1.25.0, client-go v0.34.1, Gin v1.11.0 |
| Frontend | React 18.3.1, TypeScript 5.8.3, Vite 5.4.21 |
| UI | shadcn/ui (Radix UI), Tailwind CSS 3.4.17, Recharts |
| Editor | Monaco Editor 0.52.2, xterm.js 5.3.0, diff2html |
| Web Server | Gin 1.11.0, SSE, WebSocket |
| Graphs | Cytoscape.js (dependency graphs) |
| Forms | react-hook-form + Zod validation |

---

## Conceitos de Arquitetura Críticos

### Thread-Safety (Go)

`sync.RWMutex` com double-check locking para o `clientCache` em `internal/config/kubeconfig.go`. Nunca acessar o cache sem o mutex.

**K8s client cache TTL**: `clientTTL = 30min`, `clientCleanupInterval = 15min`. Valores intencionalmente baixos para liberar clients inativos — não aumentar sem motivo (cada client K8s ocupa ~5-10MB).

**Kubeconfig: cópia privada por processo, nunca o arquivo compartilhado** (`snapshotKubeconfig`, `kubeconfig.go`): `NewKubeConfigManager` não abre mais `~/.kube/config` diretamente — copia o conteúdo pra `~/.k8s-hpa-manager/kubeconfig` (recriado a cada início de processo, via arquivo temp único + `os.Rename` atômico) e opera só sobre essa cópia dali em diante (`k.configPath`). Motivo: mesmo depois de `SwitchContext` parar de escrever `current-context` no arquivo compartilhado (histórico de bug de corrupção), `GetRestConfig` ainda **lia** o kubeconfig original do disco a cada cache-miss de `restConfigEntry` (a cada ~30-40min por cluster) — concorrendo com escritas de outras ferramentas que também usam `~/.kube/config` (kubectl, k9s, `az aks get-credentials`, `aws eks update-kubeconfig`, `gcloud container clusters get-credentials`). Uma leitura no meio de uma escrita externa pode pegar YAML parcial/inválido e derrubar a resolução de **todos** os clusters, não só o que estava sendo escrito nesse instante. Com a cópia privada, o processo nunca mais toca o arquivo original depois de copiá-lo — isolamento total de qualquer outra ferramenta, inclusive de outra instância desta própria app rodando em paralelo. Trade-off aceito: mudanças no kubeconfig original (novo contexto, credencial renovada externamente) só aparecem após reiniciar o app — já era o comportamento de fato mesmo sem a cópia, já que `k.config` (lista de contexts) sempre foi carregado uma única vez em memória no startup, nunca recarregado em runtime. Helm (`internal/helm/service.go`, via `kubeManager.ConfigPath()`) herda a cópia automaticamente, sem mudança própria.

**Bug real corrigido — nem todo consumidor usava a cópia privada**: a proteção acima só cobria o clientset típico (`GetRestConfig`, via `k.configPath` explícito) e, pra GKE/EKS, o kubeconfig temporário montado por `KubectlAuthArgs`. Só que **para AKS** (e qualquer chamada `kubectl` "nua" espalhada pelo código sem passar por `KubectlAuthArgs` — describe de node em `node_methods.go`, apply/patch/delete de VPA, Gateway API, Secrets, port-forward, Code Editor K8s tab, `web/validators/azure.go`, e o pacote independente `internal/monitoring/scanner/kubeconfig.go`), `KubectlAuthArgs` retorna só `--context <cluster>` sem `--kubeconfig` — o subprocesso `kubectl` cai na resolução padrão do sistema (`$KUBECONFIG` ou `~/.kube/config`), **ignorando completamente a cópia privada** e voltando a ler o arquivo compartilhado direto. Ou seja, a maior parte das operações reais da app (tudo que roda contra AKS via `exec.Command("kubectl", ...)`) continuava exposta ao mesmo vetor de corrupção que a cópia privada existe pra eliminar.

Corrigido em `snapshotKubeconfig`: além de copiar o arquivo, agora também seta `os.Setenv("KUBECONFIG", dest)` no processo — como `exec.Command` sem `cmd.Env` explícito herda `os.Environ()` do processo pai, isso corrige de uma vez **todas** as chamadas `kubectl` "nuas" da aplicação (incluindo o scanner de monitoring, que já lia `os.Getenv("KUBECONFIG")` com fallback pra `~/.kube/config` — passou a resolver a cópia privada automaticamente, sem mudança de código nele) sem precisar caçar cada um dos ~20 call sites individualmente. `--kubeconfig`/`--context` explícitos em linha de comando continuam tendo precedência sobre a env var quando presentes (caso do fluxo GKE/EKS via `KubectlAuthArgs`), então não há conflito. Validado removendo temporariamente o `~/.kube/config` real com o servidor rodando: endpoints que dependem de `kubectl` nu (ex: `GET /api/v1/vpn/status`, que roda `kubectl config get-contexts` sem nenhuma flag de kubeconfig) continuaram respondendo normalmente — confirma o desacoplamento total do arquivo compartilhado.

**Padrão: cache de chamadas de CLI externa (az/gcloud/aws)**: qualquer wrapper de CLI cloud chamado no hot path de uma requisição deve ser cacheado em memória com mutex + TTL curto — subprocessos custam 1-3s e são invocados por requisição sem isso. Exemplos existentes: `restConfigEntry` (`kubeconfig.go`, 40min GKE/30min outros), `IsGcloudAuthActive` (`internal/cloudprovider/gcp/auth.go`, 5min), cache de `ListNodeGroups` (2min), `checkReachability` — probe TCP de 3s cacheado por 15s para detectar VPN/rede fora do ar sem pagar o timeout completo de 30s do client K8s. Seguir esse padrão (não chamar CLI direto a cada request) ao adicionar novas integrações cloud.

**Cache de token EKS** (`getFreshEKSToken`, `kubeconfig.go`): mesmo padrão aplicado à autenticação EKS. O exec credential plugin nativo do client-go (`plugin/pkg/client/auth/exec`) roda `aws eks get-token` via `exec.Command` **sem nenhum timeout** — se a sessão AWS SSO do profile estiver expirada, o processo trava indefinidamente e a troca de cluster no frontend fica pendurada. `getFreshEKSToken` gera o token por conta própria com `exec.CommandContext` (timeout `eksTokenTimeout=10s`), cacheia em memória com TTL derivado da expiração real do token STS (`exp - eksTokenSafetyBuffer=1min`) e usa `singleflight.Group` para não spawnar subprocessos concorrentes quando várias requisições chegam juntas logo após a troca de cluster (mesmo padrão de `GetFreshGKEToken`). Em qualquer falha (aws ausente, timeout, sessão SSO expirada, resposta inválida), cai de volta no `ExecProvider` nativo do client-go — preserva o `EnrichEKSError` e a UX de erro já existentes. Como o client K8s fica cacheado por até `clientTTL` (30min) de idle — mais que a validade real de um token STS (~15min) —, um `eksTokenRoundTripper` reescreve o header `Authorization` a cada requisição HTTP com o token mais recente do cache, em vez de depender de um `BearerToken` estático travado no valor de quando o client foi criado.

**Bug real corrigido — `KubectlAuthArgs` (describe/Gateway API/Resource Explorer em EKS) usava Bearer Token estático potencialmente expirado**: `eksTokenRoundTripper` acima só protege o clientset típico (chamadas via `client-go`, HTTP em memória). `KubectlAuthArgs` (usada por `ExecuteKubectlDescribe` e outras chamadas que shell out pra `kubectl`) monta um kubeconfig **temporário em disco** com o `BearerToken` do `restConfig` — e esse `restConfig` vem do cache de `k.restConfigs`, válido por até 30min, sem nenhuma renovação. Como o token STS do EKS expira em ~15min, `kubectl describe` (e demais chamadas via subprocesso) funcionava só nos primeiros ~15min de cada janela de cache de 30min e falhava com 401/403 dali pra frente — sintoma observado: describe de pods em cluster EKS parava de funcionar pouco depois do primeiro uso do cluster, sem relação com nenhuma mudança recente de código (investigado a partir de um relato de regressão que apontava pro PR errado — a causa real não tinha nenhuma ligação com o PR suspeito). Corrigido: `KubectlAuthArgs` agora chama `getFreshEKSToken` de novo antes de gravar o token no kubeconfig temporário — como `getFreshEKSToken` já tem cache próprio com TTL derivado da expiração real do STS, isso não gera subprocessos `aws` extras na maioria das chamadas, só busca de novo quando o token realmente expirou. GKE não tem esse problema: `GetFreshGKEToken` já cacheia por 45min, maior que o TTL de 40min do `restConfig` para esse provider.

**Bubble Tea — NUNCA usar goroutines diretas:**
```go
// ❌ ERRADO - Race condition
go func() { result := applyHPA() }()

// ✅ CORRETO - Retornar tea.Cmd
return func() tea.Msg {
    err := applyHPA()
    return HPAAppliedMsg{err: err}
}
```

### Estado Global

`internal/models/types.go` é a **única** fonte de verdade. `AppModel` contém todo o estado da aplicação. Nunca criar estado local em handlers ou views — sempre modificar `AppModel` e retornar mensagem.

### Handlers HTTP (Padrão Gin + DI)

```go
// internal/web/handlers/example.go
type ExampleHandler struct {
    clientCache *cache.ClientCache  // Shared K8s clients — NUNCA criar direto
    logger      *zerolog.Logger
}
```

Rotas registradas em `internal/web/server.go`. RBAC via middleware em rotas POST/PUT/DELETE.

### Frontend — Roteamento SPA (App.tsx)

A SPA usa `react-router-dom`. Rotas definidas em `internal/web/frontend/src/App.tsx`:

| Rota | Componente | Uso |
|------|------------|-----|
| `/login` | `Login.tsx` | Autenticação (JWT ou token estático) |
| `/` | `Index.tsx` | App principal — toda a navegação por tabs |
| `/alerts/:cluster` | `AlertsPage.tsx` | Alertas do cluster |
| `/alerts/:cluster/:namespace/:hpaName` | `AlertsPage.tsx` | Alertas de HPA específico |
| `/ai-analysis/:id` | `AIAnalysisPage.tsx` | Relatório de análise AI salvo |

Todo o estado da aplicação vive em `Index.tsx` (`activeTab` string). Não há rotas para as tabs individuais — a navegação entre tabs é puro estado React.

### Frontend — Sistema de Tabs (Index.tsx)

`activeTab` é uma string que determina o conteúdo renderizado. Dois menus alimentam mudanças de tab:

**`WorkloadMenu`** (Workloads dropdown): `configmaps`, `ingresses`, `gateways`, `secrets`, `deployments`, `daemonsets`, `statefulsets`, `vpas`, `services`, `containers`, `pods`, `events`, `cronjobs`, `namespaces`, `helm`, `prometheus`

**`ToolsMenu`** (Tools dropdown): `monitoring`, `servicemesh`, `healthcheck`, `nexus-values`, `ai-diagnostics`, `github-releases`, `dependencies`, `certificates`, `resource-compare`, `command-runner`, `dynatrace`, `finops`, `teams-broadcast`, `access-check`, `latency-test`

**Tabs principais** (TabNavigation): `dashboard`, `hpa`, `nodepools`, `explorer`, `code-editor`

**Dois padrões de renderização** em `Index.tsx`:
```tsx
// Padrão 1 — display:none (tabs pesadas que ficam montadas em background):
// pods, configmaps, deployments, secrets, containers, ingresses, gateways, healthcheck, code-editor
<div style={{ display: activeTab === "pods" ? "block" : "none" }}>
  {(activeTab === "pods" || hasBeenMounted.current.pods) && <PodsPanel />}
</div>

// Padrão 2 — renderização condicional via renderTabContent() switch/case:
// Todas as outras tabs — são desmontadas quando inativas
```

O `hasBeenMounted` ref garante que tabs pesadas só sejam montadas na primeira visita, mas permanecem no DOM depois (evita perda de estado local e re-fetches).

### Frontend — Contexts

**`StagingContext`** (`src/contexts/StagingContext.tsx`): gerencia o "staging" de mudanças pendentes em HPAs e Node Pools antes do apply em lote. Expõe `addToStaging()`, `removeFromStaging()`, `applyAll()`. O contador de mudanças pendentes é exibido no header. Acessível via `useStagingContext()`.

**`TabContext`** (`src/contexts/TabContext.tsx`): gerencia o sistema multi-cluster (abas de browser `ClusterTabs`). Cada aba tem seu próprio `pageState` com `selectedCluster`, `selectedNamespace`, `activeTab`, `pendingChanges`, etc. Permite abrir o mesmo cluster em múltiplas abas com estados independentes. Acessível via `useTabContext()`.

### Frontend — API Client

Todas as chamadas HTTP centralizadas em `internal/web/frontend/src/lib/api/client.ts`. Nunca fazer `fetch` direto em componentes.

### React Query

```typescript
// SEMPRE usar queryKey único para invalidação
queryKey: ['resource-type', cluster, namespace],
// NUNCA usar window.location.reload() — usar queryClient.invalidateQueries()
```

### SSE (Server-Sent Events)

Broker em `internal/web/sse/progress.go` gerencia múltiplos clients. Usado em Cordon/Drain, Health Check, Helm Apply, Node Pool operations, **Command Runner**. Cada operação longa publica eventos via SSE para feedback em tempo real.

**Performance SSE**: limpeza de replay buffer pós-conclusão usa `time.AfterFunc` (nunca `go func()+time.Sleep` — goroutine leak). Cleanup de zumbis a cada **5 minutos** (`sseCleanupInterval`). Replay buffers inativos expiram após **1 hora** (`maxReplayBufferAge`).

**Auth em rotas SSE**: `EventSource` (browser) não suporta headers customizados — uma rota SSE protegida só por `Authorization` header no middleware sempre falha com 401 silencioso (a conexão fecha sem erro visível, o frontend fica travado esperando eventos que nunca chegam). Toda rota de stream SSE deve ficar num grupo de rotas com `middleware.WebSocketJWTAuthMiddleware` (aceita token via query param, mesmo mecanismo do WebSocket), nunca atrás do middleware JWT padrão. Ver `helmSSEGroup` em `internal/web/server.go` (rollback do Helm corrigido por esse motivo) e o mesmo padrão já usado no SSE do Health Check.

### WebSocket (Terminal)

Protocolo JSON em `internal/web/handlers/websocket_shell.go`:
- Envio: `{type: "input", data: "..."}` ou `{type: "resize", rows: N, cols: N}`
- Resposta: `{type: "output", data: "base64..."}`
- SEMPRE usar `event.preventDefault()` em key handlers para evitar duplicação de caracteres

**Auth WebSocket**: WebSockets não enviam headers customizados. O middleware `WebSocketAuthMiddleware` aceita token via query param como fallback: `ws://host/terminal?token=<TOKEN>`.

### Versionamento

Versão injetada via ldflags em build time (`main.version`). **Nunca hardcodear versão no código.**

```bash
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
go build -ldflags "-X main.version=$(VERSION)" -o build/new-k8s-hpa
```

---

## Peculiaridades Críticas

### CloudProvider Abstraction (Node Groups)

`internal/cloudprovider/interface.go` define `NodeGroupProvider` para abstrair operações de node groups por cloud:

```go
type NodeGroupProvider interface {
    ListNodeGroups(ctx, cluster) ([]models.NodePool, error)
    ScaleNodeGroup(ctx, cluster, group string, count int) error
    SetAutoscaling(ctx, cluster, group string, enable bool, min, max int) error
    AbortOperation(ctx, cluster, group string) error  // retorna ErrNotSupported se N/A
    ValidateAuth(ctx) error
}
```

- **Azure** (`cloudprovider/azure/`): usa `az aks nodepool` CLI — mesma lógica de `buildNodePoolCommands()`, mas encapsulada.
- **AWS** (`cloudprovider/aws/`): usa `aws eks` CLI. Normaliza ARN completo → nome curto via `parseEKSClusterName()`. Região pode ser extraída do ARN se não fornecida.
- **GCP** (`cloudprovider/gcp/`): usa `gcloud container node-pools` CLI. `GCPAuthManager` gerencia Device Auth Grant (RFC 8628) para autenticação sem gcloud local. `GetFreshGKEToken()` obtém access token via ADC salvo ou `gcloud auth print-access-token` (cache 45min).
- `GetNodeGroupProvider()` em `internal/config/kubeconfig.go` seleciona o provider pelo prefixo do context name: `arn:aws:eks:...` → AWS; `gke_...` → GCP; demais → Azure.

### Configs de Cluster Separadas por Provider

A config de clusters é dividida em arquivos separados por provider:

| Arquivo | Provider | Struct |
|---------|----------|--------|
| `~/.k8s-hpa-manager/clusters-config.json` | AKS | `ClusterConfig` (Name, ResourceGroup, Subscription) |
| `~/.k8s-hpa-manager/eks-clusters-config.json` | EKS | `EKSClusterConfig` (Name, AwsRegion, AwsProfile, AccountID) |
| `~/.k8s-hpa-manager/gke-clusters-config.json` | GKE | `GKEClusterConfig` (Name, ProjectID, Region) |
| `~/.k8s-hpa-manager/gcp-adc.json` | GKE auth | ADC JSON (client_id, client_secret, refresh_token) |

`GetNodeGroupProvider()` lê do arquivo correto. Retrocompatibilidade: `clusters-config.json` com campos `awsRegion`/`awsProfile` é aceito como fallback até o usuário rodar o novo `autodiscover`.

### GKE — Autenticação e Leitura de Workloads (branch `ajustes-gcp`)

**Problema**: clusters GKE autorizados não retornavam workloads (deployments, ingress, HPAs) porque `GetRestConfig()` não tinha tratamento GKE equivalente ao EKS. Com `USE_GKE_GCLOUD_AUTH_PLUGIN=True` setado pelo `EnsureGKEAuthPlugin()`, o kubeconfig exige o plugin, que pode não estar instalado.

**Solução**: `GetRestConfig()` detecta clusters GKE (`gke_` prefix no context name) e injeta um `BearerToken` obtido via `GetFreshGKEToken()`:
1. Tenta `~/.k8s-hpa-manager/gcp-adc.json` → troca `refresh_token` por access token via `https://oauth2.googleapis.com/token`
2. Fallback: `gcloud auth print-access-token` se gcloud estiver no PATH e autenticado
3. Cache em memória de 45min (tokens GCP duram 1h)
4. Se nenhum método funcionar, deixa o kubeconfig como está (funciona se `gke-gcloud-auth-plugin` estiver instalado)

**Device Auth Grant para autodiscovery GKE** (`internal/cloudprovider/gcp/auth.go`):
- `GCPAuthManager.StartLogin()` → chama `ai.StartDeviceAuth()` → obtém `user_code` + `verify_url`
- Frontend (`AutoDiscoverDialog.tsx`) exibe código e link para `accounts.google.com/device`
- `GCPAuthManager.PollStatus()` verifica se o token chegou (non-blocking channel)
- Após auth: salva `~/.k8s-hpa-manager/gcp-adc.json` e define `GOOGLE_APPLICATION_CREDENTIALS`
- Rotas: `GET /api/v1/gcp/auth/status`, `POST /api/v1/gcp/auth/login`, `GET /api/v1/gcp/auth/poll?session_id=...`

**Nota**: `gcpNeedsAuth` no `AutoDiscoverDialog` é `!authenticated && (has_gcloud || hasGKEClusters)` — `hasGKEClusters` vem de `checkGKEClustersInKubeconfig()` (reaproveita `GET /api/v1/clusters`, checando `cloud_provider === "gke"`). **Não simplificar para `has_gcloud && !authenticated`**: isso deixaria o autodiscovery nunca pedir login GCP quando `gcloud` não está instalado localmente, mesmo havendo clusters GKE no kubeconfig — o Device Auth Grant da própria app não depende do `gcloud` CLI.

### CloudAccountHintField — lembrete de conta secundária (GCP/AWS)

Na organização, alguns providers (GCP, AWS) só são acessíveis com uma conta secundária `*.ca@via.com.br`, diferente da conta normal, mesmo todos sendo federados via Azure AD. Como a escolha da conta acontece 100% na tela externa do Google/AWS/Microsoft (fora do controle do backend), a solução é puramente um **lembrete visual pessoal**, não uma troca de sessão.

- Backend: `CloudAccountHints{GCPEmail, AWSEmail}` (`internal/storage/user_tokens_store.go`) — mesmo padrão de `GitHubEditorProfiles` (coluna JSON `cloud_account_hints` em `user_ai_tokens`, chaveada por `user_email`); rotas `GET/POST /api/v1/user/cloud-account-hints` atrás de `rbacMiddleware.InjectUserEmail()`.
- Frontend: componente compartilhado `CloudAccountHintField.tsx` (prop `provider: "gcp"|"aws"`, `useQuery`/`useMutation` com queryKey `["cloud-account-hints"]` compartilhada entre instâncias) inserido nos 3 painéis de Device Auth Grant — `AutoDiscoverDialog.tsx` e `SNATPortWidget.tsx` (GCP, cópias duplicadas da UI — não unificados) e `AwsSsoLoginDialog.tsx` (AWS). Presença de e-mail não-vazio = "uso essa conta aqui".

### Azure CLI — Timeout Obrigatório

Todas as chamadas `exec.Command("az", ...)` devem usar `exec.CommandContext` com `context.WithTimeout`. Nunca usar `exec.Command` sem contexto — o Azure CLI pode travar indefinidamente em caso de VPN instável ou token expirado. Timeouts padrão: **30s** para operações de leitura, **60s** para `nodepool list/show`, **10min** para operações de escala.

### Azure CLI — Ordem de Operações para Node Pools

Azure CLI **rejeita** `az aks nodepool scale` se autoscaling estiver habilitado. Implementação em `internal/tui/app.go:buildNodePoolCommands()` lida com 4 cenários:
- Apenas autoscaling (min/max): um `update --enable-cluster-autoscaler`
- Apenas node count: `disable-cluster-autoscaler` → `scale`
- Ambos: enable → disable → scale → enable novamente
- Abort real: `az aks nodepool operation-abort` (cancela no ARM, não apenas o CLI local)

### Suffix `-admin` em Cluster Names

Sessions salvam sem `-admin`, mas kubeconfig contexts têm `-admin`. `StagingContext.tsx` usa `ensureAdminSuffix()` ao carregar sessões. Ao criar prompts IA, usar nome **sem** `-admin`.

### TypeMeta em YAMLs do typed API

A API typed do K8s (`clientset.CoreV1().Secrets().Get()`) **não preenche** `TypeMeta`. Antes de serializar para YAML com `yaml.Marshal`, sempre adicionar:

```go
secret.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
```

Sem isso, `kubectl apply` falha com "apiVersion not set, kind not set".

### Dynamic Client (CRDs)

O dynamic client **não está no vendor**. Para recursos CRD (VPAs, recursos do Explorer, etc.), usar kubectl shell: `kubectl get/apply/delete -o yaml`. O Discovery API (`clientset.Discovery().ServerPreferredResources()`) está disponível e é usado no Resource Explorer.

### Bubble Tea — Texto Unicode-Safe

Sempre usar `[]rune` ao invés de `string` para manipulação de texto no TUI. Cursor position em runes, não bytes.

### Audit Trail (History Tracking)

Toda operação destrutiva deve registrar no `HistoryTracker` (`internal/history/tracker.go`):

```go
entry := helpers.CreateHistoryEntry(c, "scale-hpa", before, after)
history.Log(entry)
```

`CreateHistoryEntry()` obtém `UserEmail`/`UserName` automaticamente via contexto Gin (RBAC). Dados persistidos em `~/.k8s-hpa-manager/history/` (JSON, max 1000 entradas em memória).

### Sanitizer (AI Diagnostics)

`internal/sanitizer/` mascara automaticamente IPv4, JWT, Bearer tokens, passwords, API keys antes de enviar contexto para IA. Nunca enviar dados brutos de log diretamente para AI providers — sempre passar pelo sanitizer.

### Variáveis de Ambiente (Auth)

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `K8S_HPA_WEB_TOKEN` | Token estático legado (backward compat) | `poc-token-123` |
| `K8S_HPA_JWT_SECRET` | Secret para assinar JWTs (mín. 32 bytes — ativa modo JWT) | não definido |
| `K8S_HPA_JWT_TTL` | TTL dos tokens JWT (ex: `8h`, `24h`) | `8h` |

### Autenticação JWT (branch `migracao-jwt`)

**Dual-mode**: quando `K8S_HPA_JWT_SECRET` está definido, o sistema opera em modo JWT; caso contrário, cai para token estático (`K8S_HPA_WEB_TOKEN`). O middleware `JWTAuthMiddleware` em `internal/web/middleware/auth.go` decide automaticamente.

**Pacote `internal/auth/`**:
- `jwt.go` — `JWTManager`: `Generate()`, `Validate()`, `IsConfigured()`, `TTL()`. Claims: `email`, `name`, `is_sre`.

**Endpoints de auth** (`internal/web/handlers/auth.go`) — sem middleware de autenticação prévia:
- `POST /auth/login` — obtém email via `az account show`, verifica grupo AD, emite JWT. Retorna `TOKEN_EXPIRED` (código `401`) quando expirado ou `JWT_NOT_CONFIGURED` (`501`) quando secret ausente.
- `POST /auth/logout` — stateless, apenas instrui o frontend a descartar o token.
- `POST /auth/refresh` — valida JWT atual, emite novo com mesmo email/isSRE sem re-consultar Azure AD.

**Frontend**:
- `Login.tsx`: tenta `/auth/login` automaticamente (modo JWT). Se receber `501 JWT_NOT_CONFIGURED`, cai para campo de token estático.
- `apiClient.isTokenExpired()`: decodifica JWT do localStorage e verifica `exp` localmente sem requisição.
- Auto-refresh: antes de cada requisição, se o token expirou, `apiClient` tenta `POST /auth/refresh`. Se falhar, dispara evento `jwt-expired` (capturado em `App.tsx` → logout).
- `App.tsx`: ouve evento `jwt-expired` via `window.addEventListener` para forçar re-login.

**WebSocket**: `WebSocketJWTAuthMiddleware` aceita JWT via query param `?token=<JWT>` (mesmo dual-mode).

### Auto-Shutdown por Inatividade

O servidor desliga automaticamente quando nenhuma página web está conectada por **40 minutos** (proteção contra instâncias esquecidas em WSL2).

**Mecanismo**:
- `startInactivityMonitor()` em `server.go` inicia timer de 45min na startup
- Frontend (`useHeartbeat.ts`) envia `POST /heartbeat` a cada **5 minutos**
- Cada heartbeat reseta o timer para **45 minutos**
- Se 40 minutos passarem sem heartbeat → `os.Exit(0)`

**Causa mais comum de desconexão**: browsers throttleiam `setInterval` em abas em segundo plano — o intervalo de 5min pode escalar para 60min+. O threshold de 40min dá margem para isso. Se ainda ocorrer, reabrir a aba (o frontend envia heartbeat imediatamente ao montar).

### Auto-Update / Instalação (`install-from-github.sh`)

Botão de update no Header (visível quando `GET /api/v1/version` retorna `update_available: true`) chama `POST /api/v1/version/update` → `VersionHandler.SelfUpdate` (`internal/web/handlers/version.go`), que dispara `curl .../main/install-from-github.sh | bash` numa goroutine. **Sempre aponta para a branch `main`** — nunca hardcodear outra branch aqui: já apontou para `new-k8s-hpa-dev` (uma branch congelada, ~1630 commits atrás da `main`), fazendo correções no script nunca chegarem ao fluxo de auto-update mesmo depois de mescladas.

**`install-from-github.sh` mata e reinicia o servidor quando necessário**: `install_binary()` mata (`kill -9`) qualquer processo na porta 8080 antes de substituir o binário — isso inclui o próprio servidor que disparou o self-update (a goroutine sobrevive porque `kill -9` no processo pai não mata o subprocesso bash já desatachado). `SERVER_WAS_RUNNING` marca quando isso acontece; `restart_server()` (chamada só no fluxo de sucesso, antes de `print_usage`) roda `$BINARY_NAME web` — que já se auto-daemoniza em background sozinho (mesmo mecanismo de `runInBackground()` em `cmd/web.go`, sem precisar de `&`) — só quando `SERVER_WAS_RUNNING=true`. Numa instalação nova (nenhum servidor rodando), nada é iniciado automaticamente — mantém o comportamento documentado de "rode `$BINARY_NAME web` manualmente depois".

**Feedback no frontend do processo de update** (`Header.tsx`): como o backend literalmente mata e reinicia o próprio processo, **SSE não serve aqui** (a conexão cairia junto) — o acompanhamento é por polling. Fluxo: clique no badge "Update" abre `AlertDialog` de confirmação (padrão de `LoadSessionModal.tsx`) antes de disparar `POST /version/update`; ao confirmar, `toast.loading(..., { id: "server-update" })` fica grudado na tela (sonner não some sozinho com `id` fixo) enquanto um `setInterval` de 4s chama `apiClient.getVersion()` — mesmo padrão de polling do Device Auth Grant GCP em `SNATPortWidget.tsx` (`useRef` do handle do interval, `catch` silencioso porque erro de rede é esperado enquanto o servidor está fora do ar reiniciando). Só considera concluído quando `current_version` **muda** em relação à versão capturada no momento do clique — evita falso-positivo, já que o servidor antigo continua respondendo normalmente durante download/instalação, antes do `kill -9` em `install_binary()`. Ao detectar a versão nova, `toast.success` + `window.location.reload()` após 1,5s. `setTimeout` de segurança de 3min cancela o polling e avisa erro caso o restart nunca aconteça (evita o botão ficar preso em "Atualizando..." pra sempre, que era o comportamento antigo — `setUpdating(false)` só rodava no catch do POST inicial).

### Monitoring V2

`internal/monitoring/engine/monitoring_v2.go` — sem port-forwards. Discovery automático via HTTPS: `https://prometheus-{cluster}-{env}.viavarejo.com.br/`. Cache em memória (TTL 1h). Endpoints em `/api/v1/monitoring/v2/`.

### Monaco Editor — Regras Críticas

**`configureMonacoYaml` é global e deve ser chamado UMA única vez por sessão.** Chamar múltiplas vezes (ex: um por instância de `MonacoYamlEditor`) recria o worker YAML global e pode invalidar `addAction`/`addCommand` registrados em instâncias anteriores — os atalhos Ctrl+Shift+D (decode base64) e Ctrl+Shift+E (encode base64) desaparecem do menu de contexto. Implementado via flag `_yamlConfigured` em `MonacoYamlEditor.tsx`. **Nunca remover esse guard.**

**Atalhos registrados** (em todos os editores não-readOnly via `addAction`):
- `Ctrl+Shift+E` → Encode seleção para Base64
- `Ctrl+Shift+D` → Decode seleção de Base64
- `Ctrl+Shift+Z` (context menu) → Cron → Texto legível
- `Ctrl+Shift+X` (context menu) → Texto → Expressão Cron

**Erros em aba anônima**: "Tracking Prevention blocked access to storage" e "Could not create web worker(s)" são **inofensivos** — Monaco usa fallback síncrono. YAML funciona normalmente.

### CronJobs — Criação de Jobs e CronJobs (branch `criar-jobs-cronjobs`, PR #155)

`CronJobsTab.tsx` + `internal/web/handlers/cronjobs.go` + `internal/kubernetes/client.go`:

**Criação via modal unificado** (`Novo` dropdown → "Job (execução única)" ou "CronJob (agendado)"):
- Jobs criados via `BatchV1().Jobs(ns).Create()` direto — sem `kubectl apply`, sem conflito com Helm field manager
- CronJobs criados via `BatchV1().CronJobs(ns).Create()` — exigem `name` explícito; YAML gerenciado pelo Helm pode ser removido no próximo `helm upgrade`
- Namespace sempre herdado do seletor — nunca do YAML. `generateName: "job-"` automático se YAML não tem `name`
- Dry-run disponível antes de criar

**Template de CronJob selecionado**: botão "Template de CronJob selecionado" no modal de Job carrega o `spec.jobTemplate.spec` do CronJob via `GET /api/v1/cronjobs/:cluster/:namespace/:name/job-template`

**Monaco context menu** (menu de contexto ao selecionar texto no editor YAML):
- Cron → texto: selecionar expressão cron → "Converter cron para texto" → substitui pela descrição em pt-BR
- Texto → cron: selecionar texto natural (ex: "todos os dias às 08:00") → "Converter texto para cron" → substitui pela expressão
- Implementado em `MonacoYamlEditor.tsx` usando `editor.addAction()` + `explainCronExpression()`/`textToCron()` de `cronParser.ts`

**Versionamento no GitHub**: seção colapsável no modal para commitar o YAML. O usuário cola a URL completa da pasta no GitHub (ex: `https://github.com/org/repo/tree/main/jobs/pasta`) — o frontend extrai `owner/repo/branch/path` via regex. Backend `CommitFile` (`POST /api/v1/github/commit-file`) faz GET para obter SHA atual antes de criar/atualizar — evita duplicar arquivos. Suporta qualquer organização.

**Rotas**: `POST /api/v1/jobs`, `POST /api/v1/cronjobs/new`, `GET /api/v1/cronjobs/:cluster/:namespace/:name/job-template`, `POST /api/v1/github/commit-file`

**Nota investigação**: "listar e selecionar CronJob parece disparar um job" — investigado e confirmado que **não há bug**. O `active_jobs > 0` visível é do agendamento natural do K8s, não da UI. O trigger real exige 3 ações explícitas.

### ResizeDivider (SplitView)

`SplitView.tsx` é o componente reutilizável para painéis side-by-side com resize. Usado em `CommandRunnerTab` e `ResourceCompareModal`. Implementação via `useRef` + mouse event listeners. Importar `SplitView` ao criar novas interfaces de edição lado a lado — **não reimplementar o drag logic**.

### Command Runner

`CommandRunnerTab.tsx` + `internal/web/handlers/command_runner.go`: executa comandos (kubectl/shell/python/go) em múltiplos clusters simultaneamente com SSE. Suporta **AI-powered command generation** via `POST /api/v1/command-runner/generate` (gera comandos a partir de prompt em linguagem natural).

### ToolsMenu

`ToolsMenu.tsx` — dropdown com 14 ferramentas avançadas acessíveis no header. Ao adicionar nova ferramenta, registrar aqui como novo item do dropdown.

### Editor de Código (Code Editor)

`CodeEditorTab.tsx` + `internal/web/handlers/code_editor.go` + `code_editor_terminal.go` + `code_editor_lsp.go`: editor de código completo com integração Git/GitHub e LSP, acessível via Tools → "Editor de Código" (tela cheia). Fases 1-7 concluídas — ver `CODE-EDITOR-PLAN.md` para detalhes.

**Repositórios**: clonados em `~/.k8s-hpa-manager/repos/<owner>-<repo>/`. ID local = `owner-repo`. Limite: 10 repos por instância.

**Operações Git via SSE** (progresso em tempo real):
- Clone: `POST /api/v1/code-editor/clone` — injeta token na URL (`https://TOKEN@github.com/...`); token removido da URL remota após push
- Pull: `POST /api/v1/code-editor/repos/:id/pull`
- Push: `POST /api/v1/code-editor/repos/:id/push`

**Operações síncronas — arquivo/árvore**:
- Árvore: `GET /api/v1/code-editor/repos/:id/tree` — profundidade máx 6, ignora `.git`, `node_modules`, `vendor`, `build`
- Arquivo: `GET /api/v1/code-editor/repos/:id/file?path=...` — limite 5MB; `POST` para salvar
- Original (HEAD): `GET /api/v1/code-editor/repos/:id/original?path=...` — conteúdo HEAD para DiffModal
- Criar arquivo: `POST /api/v1/code-editor/repos/:id/file/create`
- Criar pasta: `POST /api/v1/code-editor/repos/:id/mkdir`
- Renomear: `POST /api/v1/code-editor/repos/:id/rename`
- Excluir: `DELETE /api/v1/code-editor/repos/:id/file`
- Busca por nome: `GET /api/v1/code-editor/repos/:id/search?q=...`
- Busca em conteúdo: `GET /api/v1/code-editor/repos/:id/grep?q=` (via `git grep -n --ignore-case`)
- Formatar: `POST /api/v1/code-editor/repos/:id/fmt` — executa formatter da linguagem (`gofmt`, `prettier`, etc.)

**Operações síncronas — git**:
- Status: `GET /api/v1/code-editor/repos/:id/status` — porcelain + ahead/behind
- Branches: `GET /api/v1/code-editor/repos/:id/branches` — faz `fetch --prune` antes
- Commit: `POST /api/v1/code-editor/repos/:id/commit` — `git add .` + `git commit -m`; suporta `--amend`; retorna `{ message }` com output real do git
- Branch: `POST /api/v1/code-editor/repos/:id/branch` (criar), `POST .../checkout` (trocar); ambos retornam `{ branch, message }`
- Merge: `POST /api/v1/code-editor/repos/:id/merge` — suporta `no_ff`
- Stash: `POST /api/v1/code-editor/repos/:id/stash` (`--include-untracked`), `POST .../stash/pop`
- Reset de arquivo: `POST /api/v1/code-editor/repos/:id/reset-file` — `git checkout HEAD` ou `git clean`
- Cherry-pick: `POST /api/v1/code-editor/repos/:id/cherry-pick`
- Tags: `GET /api/v1/code-editor/repos/:id/tags`, `POST` (criar anotada ou leve), `DELETE .../tags/:tag`
- Log: `GET /api/v1/code-editor/repos/:id/log?limit=20`
- Diff: `GET /api/v1/code-editor/repos/:id/diff?path=...`

**Terminal integrado**: WebSocket `GET /api/v1/code-editor/repos/:id/terminal` abre PTY real via `creack/pty`; xterm.js no frontend. Suporta cores ANSI, resize, programas interativos. Painel na base da área do editor, altura inicial de 240px; barra de abas para múltiplos terminais simultâneos (estado em `terminalTabs[]` + `activeTerminalId`). Arrastável (`HResizeDivider`) até **90% da altura do pane do editor** (medido via `editorPaneRef.current.clientHeight`, fallback `window.innerHeight` sem o ref montado) — antes o teto era fixo em 600px, insuficiente para maximizar o terminal em telas grandes.

**Barra de status** (linha azul `#007acc`, altura 20px): `Ln X, Col Y` | linguagem | `UTF-8` | font size `−/NNpx/+` (10–24, `localStorage["ce_font_size"]`) | word wrap toggle (`localStorage["ce_word_wrap"]`) | auto-save toggle (debounce 1,5s, `localStorage["ce_autosave"]`) | format on save toggle (Go/TS/JS/Python/JSON, formata antes de gravar, `localStorage["ce_format_on_save"]`). Font size e word wrap sincronizados via `editorRef.current?.updateOptions()` sem recriar o editor.

**Barra do arquivo ativo**: breadcrumb clicável (cada segmento de dir faz switch para aba Arquivos + `setRevealPath`); botão `Copy` copia path; botão `Locate` revela na tree. Arquivo revelado: `data-reveal-path` + ring amarelo + `scrollIntoView` por 1,5s.

**Context menu da tree** (botão direito): estado `{ x, y, node }` posicionado com `position: fixed`; fecha via `document.addEventListener("mousedown")`; `onMouseDown={e.stopPropagation()}` impede fechamento ao clicar dentro. Arquivo: Abrir / Renomear / Deletar / Copiar caminho / Revelar na tree / Histórico. Pasta: Novo arquivo aqui / Nova pasta aqui / Renomear / Deletar / Copiar caminho.

**Ícones por extensão + bolinha de status na tree** (VSCode-like, `getFileIcon()` em `CodeEditorTab.tsx`): cada arquivo exibe ícone/cor conforme extensão (Go, TS/JS, Python, JSON, YAML, Markdown, CSS, HTML, SQL, shell, imagens, Terraform, Dockerfile, go.mod/package.json, etc.) em vez de um ícone genérico único. Bolinha de status (`StatusDot`) propagada até pastas colapsadas via `collectAncestorDirs()`: laranja = mudança git não commitada (cobre o repo inteiro, vem do `git status`), vermelho = erro (prioridade sobre laranja). **Limitação**: a bolinha de erro só reflete a aba atualmente aberta no editor — o Monaco reaproveita um único model entre abas (não há prop `path` no `<Editor>`), então arquivos nunca abertos na sessão não têm diagnóstico.

**Feedback de operações git via modal, não toast**: commit/push/pull/checkout/stash/tag/merge/cherry-pick usam `showGitResult()` → `GitResultState` (modal com título/mensagem/botões de ação), não mais o `Toast` antigo — toasts empilhavam e truncavam mensagens de erro longas do git. O modal suporta ações contextuais (ex: checkout falha por mudanças locais → botão "Fazer stash e tentar de novo"). Botão "Sync" encadeia Pull e depois Push automaticamente (`chainPush` no estado do `sseDialog`), mesmo padrão do "Sincronizar Alterações" do VSCode. `Toast`/`addToast` continuam existindo só para feedback curto (não-git).

**`ListBranches` também autentica o `git fetch`**: sem o token do usuário (`InjectUserEmail()` na rota + `gitCmdWithToken`), branches remotos de repositórios privados não apareciam e o erro era descartado silenciosamente — nenhum sinal de que o fetch tinha falhado. Mesmo padrão de autenticação do Pull/Push.

**Botão PR**: header, visível quando `branches?.current !== "main" && !== "master"`; abre `CreatePRModal` para criação de PR direto na aplicação. `owner`/`repo` extraídos via `ownerRepo(dir)` em `ListRepos` (lê `git remote get-url origin`) — **não** do ID local por split em `-`, que quebrava com owners com hífen (ex: `casas-bahia`). `CreatePRModal`: título auto-preenchido a partir do branch (ex: `feat/foo` → `Feat Foo`), dropdown de branch destino (exclui o branch atual), descrição opcional; chama `POST /api/v1/code-editor/repos/:id/pr/create` → GitHub REST API com o PAT do `tokenStore`; exibe `PR #N criado!` + botão "Abrir no GitHub" ao concluir.

**Bug real corrigido — `CreatePR` ignorava qual perfil GitHub estava ativo**: `CreatePR` sempre lia o token via `h.getToken(c)` (`GitHubTokenStore`, store legado single-token compartilhado com a aba GitHub Releases), nunca da lista de perfis nomeados (`GitHubEditorProfile`/ProfileSwitcher) usada por Clone/Pull/Push. Os dois stores são independentes — o `GitHubTokenStore` pode estar com o PAT de uma conta diferente da marcada como "ativa" na UI, e foi exatamente isso que aconteceu aqui: confirmado comparando hash SHA-256 dos tokens (sem expor os valores) que o token do `GitHubTokenStore` batia com um perfil **inativo** (conta secundária, corporativa/EMU), não com o perfil ativo (conta pessoal). O erro `"Unauthorized: As an Enterprise Managed User, you cannot access this content"` visto ao criar PR não era uma restrição permanente do GitHub sobre *aquele repo* — era o app autenticando com a conta errada. Corrigido: `CreatePR` agora resolve o token na ordem `req.ProfileID` explícito (`resolveProfileToken`) → perfil marcado `active` (`resolveActiveProfileToken`, novo) → `h.getToken(c)` como último fallback — mesma prioridade já usada em Clone/Pull/Push. Frontend (`CreatePRModal`) passa `profileId` (via `activeProfileId()`) para `codeEditorCreatePR`. Validado contra o repo real: antes retornava o erro EMU sempre; depois de resolver o perfil certo, retorna o erro esperado do GitHub (`"No commits between X and Y"` num teste com head==base) — confirma que a autenticação passou a usar o token certo.

O `error_type: "emu_pat_blocked"` (detecção por substring na mensagem do GitHub, independente do status HTTP) continua implementado como segunda camada de defesa — **caso a conta ativa em si seja EMU-restrita de verdade**, o erro aparece com `instructions[]` passo a passo em vez do texto cru, mesmo padrão de `saml_authorization_required` em `github_releases.go`. **Cuidado ao alterar o interceptor de 403 em `client.ts`**: ele sobrescrevia todo erro 403 por um texto genérico de RBAC K8s, mascarando tanto este `error_type` quanto o de SAML — agora só aplica o fallback quando `rawError.error_type` está ausente, e anexa `details: rawError` ao `Error` lançado para o chamador acessar `error_type`/`instructions` sem precisar bypassar `apiClient.request()` com `fetch` cru (como `GitHubReleasesTab.tsx` já fazia por esse motivo).

**Ctrl+P Quick Open**: overlay `absolute inset-0 z-50` (requer `relative` no container pai); `quickOpenFiles = useMemo(() => flattenTree(tree).filter(...))` filtra em tempo real; registrado no Monaco via `addCommand(2048|46)` e via `document.addEventListener("keydown")` global.

**Bug real corrigido — "Revelar na tree" não achava arquivos dentro de pastas recolhidas**: cada `FileTreeNode` de pasta guarda seu próprio `open` local (`useState`), e os filhos só são renderizados (`{open && node.children?.map(...)}`) quando a pasta está aberta — um arquivo dentro de uma pasta recolhida nunca chegava a montar no DOM, então `revealPath` nunca encontrava o nó pra destacar (`data-reveal-path`). Corrigido com um `useEffect` em cada nó de pasta que se auto-abre quando percebe que `revealPath` está dentro dela (`revealPath.startsWith(node.path + "/")`) — isso cascateia sozinho: abrir o pai monta os filhos, cujo próprio efeito abre o próximo nível, até o arquivo alvo aparecer no DOM. O efeito de scroll (`useEffect([revealPath])`, antes um único `document.querySelector` seguido de `setTimeout(1500)` fixo) também precisou virar polling via `requestAnimationFrame` (até ~60 frames) — como a cascata de aberturas leva alguns ciclos de render pra terminar em hierarquias profundas, o elemento não existe ainda no mesmo tick em que `revealPath` é setado; o timer de 1500ms que limpa o destaque só começa a contar depois que o elemento é de fato encontrado, não a partir do clique. Validado no navegador: `Ctrl+P` abrindo um arquivo fundo (`internal/web/frontend/src/components/CodeEditorTab.tsx`, com `internal` recolhido) + "Revelar na tree" expande toda a cadeia de pastas e centraliza o arquivo com o anel amarelo de destaque.

**GitHub PAT**: via `GitHubTokenStore` (mesmo store do GitHub Releases). Fallback para `GITHUB_TOKEN` env var. Token injetado via `InjectUserEmail` middleware.

**Perfis GitHub nomeados (ProfileSwitcher) — token nunca sai do servidor**: `GET /api/v1/code-editor/github-profiles` retornava o PAT completo em texto puro (`GitHubEditorProfile.Token`) — qualquer chamada ao endpoint expunha o segredo. `GetGitHubProfiles` agora mascara (`maskGitHubToken`, prefixo 8 chars + `••••` + sufixo 4 chars, mesmo formato já usado no preview local do `ProfileSwitcher`). Como o frontend recarrega os perfis do servidor para sincronizar entre abas/sessões e os cacheia em `localStorage`, mascarar sem mais nada quebraria Clone/Pull/Push (que enviavam o token bruto do perfil ativo no corpo da requisição). Corrigido nos dois lados: `SaveGitHubProfiles` detecta o marcador `••••` no token recebido e preserva o valor real já armazenado por ID (perfil não foi editado, só renomeado/trocado de ativo) — verificado que o PAT no SQLite permanece intacto após um save com token mascarado; `Clone`/`Pull`/`Push` passam a aceitar `profile_id` (novo `resolveProfileToken(c, profileID)` resolve o PAT real server-side), e o frontend troca `activeToken()` por `activeProfileId()` — só o ID (não sensível) trafega do browser, nunca mais o token.

**Monaco no CodeEditorTab**: usa `@monaco-editor/react` direto (sem `MonacoYamlEditor`), detecta linguagem pela extensão do arquivo. **Não chama `configureMonacoYaml`** — evita conflito com o singleton em `MonacoYamlEditor.tsx`. Sidebar arrastável via `ResizeDivider` (mín 160px, máx 520px); largura e último repo persistidos em `localStorage`.

**LSP (Language Server Protocol)**: `code_editor_lsp.go` gerencia processos `gopls`/`pyright` por repositório via `sync.Map` (key: `repoId/lang`). Sessões inativas > 10min são encerradas; cleanup a cada 5min. JSON-RPC via stdin/stdout com header `Content-Length`. Frontend registra providers nativos do Monaco **uma única vez** por sessão (flags `__monacoTSConfigured`, `__monacoGoLSPRegistered`, `__monacoPyLSPRegistered`) usando variáveis globais `window.__lspActiveRepoId` e `window.__lspActiveFilePath` para comunicar o arquivo ativo. Polling de diagnósticos a cada 2,5s via `setModelMarkers`. `gopls` esperado em `~/go/bin/gopls` ou PATH; `pyright`/`pylsp` no PATH (instalar com `npm i -g pyright` ou `pipx install pyright`); `lspVersionRef` incrementado a cada `updateTabContent` e troca de aba. **Nunca registrar `registerCompletionItemProvider("go"|"python", ...)` mais de uma vez** — flag global previne duplicação. `__lspApplyDiagnostics(model, diags, owner)` aceita owner genérico (`"gopls"` ou `"pyright"`) para não sobrescrever markers entre linguagens.

**Por que não usar `monaco-languageclient`**: a v8+ exige `@codingame/monaco-vscode-editor-api` como peer dependency — uma fork do Monaco que substituiria o `monaco-editor` padrão, quebrando `monaco-yaml`, os workers de YAML e toda a configuração atual. A solução adotada usa providers nativos do Monaco (`registerCompletionItemProvider`, `registerHoverProvider`, `registerDefinitionProvider`, `setModelMarkers`) com chamadas HTTP ao backend Go que faz proxy JSON-RPC para o processo do language server.

**Path traversal**: `ReadFile`/`WriteFile` verificam `strings.HasPrefix(fullPath, repoDir)` antes de operar.

**Integração K8s (Fase 9)**: aba "K8s" no sidebar (`code_editor_k8s.go`) — kubectl diff/dry-run/apply/get via SSE, cluster selector (contexts do kubeconfig), detecção automática de manifests (`apiVersion:` + `kind:` no conteúdo ativo do editor), output colorizado por tipo de linha, abas virtuais `__k8s_virtual__/` (read-only, guard em `saveFile`). Ver `CODE-EDITOR-PLAN.md` para detalhe de endpoints.

**Source Control VSCode-like (Fase 10)**: painel `source-control` com badges M/A/D/U/R na tree; `CommitDialog` com unstage; pull `--rebase` automático quando o push é rejeitado por non-fast-forward (se o rebase falhar por conflito, o push retorna erro com a mensagem de conflito); multi-select na tree + split editor lado a lado; sidebar convertida para ícones com tooltip; preview Markdown pode cobrir até 70% da área do editor.

**Refresh silencioso de tree/status**: `saveFile`/`saveRightFile` só chamavam `loadStatus` ao salvar pelo próprio editor — mudanças feitas fora desse fluxo (terminal integrado, `git` via CLI, edição externa) não disparavam refresh, exigindo F5. `useEffect` faz poll silencioso (`loadStatus` + `loadTreeSilent`, sem spinner) a cada 5s enquanto há repo selecionado, mais refresh imediato em `focus`/`visibilitychange` da janela. `loadTreeSilent` é igual a `loadTree` mas sem alternar `treeLoading`, para não piscar o spinner da árvore a cada ciclo do poll.

**Fonte do terminal integrado servida pelo backend**: a lista de fontes do seletor do `RepoTerminal` vem de `fc-list`, mas os *bytes* da fonte selecionada precisam ser servidos pelo servidor (não só o nome via CSS `font-family`) — senão a fonte escolhida não tem efeito quando o browser roda numa máquina diferente do servidor (ex: WSL2 servidor + browser Windows), pois o nome não corresponde a nada instalado no cliente. Endpoint `GET /api/v1/code-editor/fonts/:name/file` (`GetFontFile` em `code_editor.go`, resolve via `fc-match "<name>:spacing=mono" --format=%{file}`, `Cache-Control` 7 dias); frontend busca os bytes com o token de auth (`FontFace` não aceita headers customizados via `url()`, por isso fetch manual + `new FontFace(name, arrayBuffer)`) e registra em `document.fonts` antes de aplicar ao xterm — `ensureTerminalFontLoaded()` com cache em `Set` module-level compartilhado entre abas de terminal.

**Multi-select de arquivos e pastas na aba "Arquivos"** (`selectedPaths: Set<string>` em `CodeEditorTab.tsx`, distinto do multi-select da aba Source Control): clique normal planta a seleção só com aquele item (`onSingleSelect`, substitui a seleção anterior — mesma semântica do Explorer/VSCode), Ctrl/Cmd+click soma itens (`onMultiToggle`), aplicável tanto a arquivos quanto a pastas. **Cuidado ao alterar**: um clique normal que só chamasse `onSelect` sem também chamar `onSingleSelect` faz o primeiro item escolhido nunca entrar em `selectedPaths` — bug real já visto em produção (usuário clica normal no 1º arquivo pra começar a seleção, Ctrl+clica nos demais, e o 1º sempre ficava de fora do lote). Barra de ações (deletar/mover em lote, ícones no topo da tree) e o item "Deletar/Mover N selecionados" no menu de contexto (quando o nó clicado com botão direito faz parte de uma seleção com mais de 1 item) usam esse mesmo estado.

**Arrastar para mover (drag-and-drop) cobre pastas também**: pastas agora são `draggable` (antes só arquivos eram — arrastar uma pasta não fazia nada). Arrastar um item que faz parte de uma seleção múltipla move **todos os selecionados juntos**, via marcador `"__multi__"` no `dataTransfer` (distinto de carregar o path direto) que o `onDrop` da pasta/raiz interpreta chamando `onMoveMultiple` (== `handleMoveSelected`) em vez de `onMove` de item único.

**Cursor da linha na árvore — `cursor-default`, não `cursor-grab`**: linhas de arquivo/pasta são `draggable` (suportam arrastar para mover), mas isso não deve mudar o cursor padrão de repouso — `cursor-grab` deixava a "mãozinha" (mão aberta) visível o tempo todo em cima de qualquer item da árvore, mesmo fora de um arraste, quando o esperado é o cursor padrão (seta) até o usuário efetivamente clicar/segurar para arrastar. Classe ajustada para `cursor-default active:cursor-grabbing` nas duas linhas (arquivo e pasta) — a mãozinha fechada (`grabbing`) só aparece com o botão do mouse pressionado (`:active`), sinalizando a ação de arrastar em andamento, sem confundir o estado de repouso com "isto é arrastável" via cursor sozinho.

**Bug crítico de bubbling corrigido**: o `onDrop`/`onDragOver` da linha da pasta não chamava `e.stopPropagation()`. Como pastas-filhas são renderizadas como *irmãs* da própria linha da pasta-pai (não como descendentes dela no DOM — ambas são filhas do mesmo `<div>` wrapper), o evento de drop borbulhava até a zona de drop da raiz da árvore, que **também** processava o mesmo evento — dois `handleMove*` concorrendo pela mesma origem, corrompendo o destino de alguns itens (indo pra raiz do repo em vez da pasta certa) ou falhando silenciosamente. Afetava até o drag de item único (single-file), não só o multi-select — corrigido adicionando `e.stopPropagation()` nos dois handlers da pasta.

**`DeleteFile` (`code_editor.go`) precisa de `os.RemoveAll` para pastas**: usava `os.Remove`, que falha em diretório não vazio — necessário para o multi-select cobrir exclusão de pastas com conteúdo. Detecta via `os.Stat().IsDir()` antes de decidir entre `os.Remove` (arquivo) e `os.RemoveAll` (pasta).

**Colar (Ctrl+V) uma cópia no mesmo diretório do arquivo copiado**: `handleClipboardPaste` tinha um guard `if (clipboard.path === to) return` que existia para bloquear "mover para o próprio lugar" (faz sentido pro `cut`), mas também bloqueava silenciosamente `copy` no mesmo diretório — o usuário apertava Ctrl+C/Ctrl+V e nada acontecia, sem nenhum aviso. Corrigido: `cut` continua bloqueando; `copy` nesse caso gera um nome único via `uniqueCopyName()` (`arquivo.ts` → `arquivo copy.ts` → `arquivo copy 2.ts`, checando os nomes já existentes no diretório de destino a partir do `tree` em memória).

### AI Providers (Multi-provider)

`internal/ai/` suporta 5 providers: **Ollama**, **Claude** (Anthropic), **Gemini** (API Key ou Vertex AI via ADC/SSO), **OpenAI** e **Copilot** (Azure OpenAI). Configurável via `AISettingsTab.tsx`. Tokens de usuário persistidos em `internal/storage/user_tokens_store.go`. **Nunca hardcodear API keys** — sempre via storage de tokens.

**Gemini Vertex AI (SSO corporativo)**: `GeminiAuthMode = "vertex"` usa Application Default Credentials (`gcloud auth application-default login`). Requer `GeminiVertexProject` (ou env `GOOGLE_CLOUD_PROJECT`). O ADC do servidor tem prioridade sobre credenciais locais — não requer role IAM explícita se o servidor já tiver acesso.

**ADC file path**: `WriteADCFile()` em `internal/ai/google_device_auth.go` grava em `~/.k8s-hpa-manager/google_adc.json` (caminho próprio da app) — **nunca sobrescreve** `~/.config/gcloud/application_default_credentials.json`. Após gravar, define `GOOGLE_APPLICATION_CREDENTIALS` para apontar para esse arquivo.

**FallbackProvider**: quando o servidor não tem provider padrão configurado, tenta usar ADC da máquina host via `GOOGLE_APPLICATION_CREDENTIALS` ou `~/.config/gcloud/application_default_credentials.json`. Útil em ambientes de dev onde o ADC pessoal já está ativo.

**Vertex AI SSO — 3 tentativas com diagnóstico**: a lógica de inicialização do Gemini Vertex tenta até 3 vezes com logs de diagnóstico detalhados (endpoint, projeto, scopes) antes de falhar. Ver `internal/ai/gemini_provider.go`.

**Autenticação Vertex AI via Device Auth Grant (RFC 8628)**: Fluxo sem servidor de callback — obrigatório em WSL2 (loopback Linux isolado do Windows). Frontend chama `POST /ai/tokens/google-auth/start`, backend obtém `user_code` e `device_code` do Google, frontend exibe o código e `accounts.google.com/device`. Backend faz polling em `POST /ai/tokens/google-auth/poll` até receber o token. Implementado em `internal/ai/google_device_auth.go` + `internal/web/handlers/ai_tokens.go (StartGoogleDeviceAuth/PollGoogleDeviceAuth)`.

**Vertex AI via WIF SSO (Workforce Identity Federation)**: Campo `GeminiWifLoginURL` em `UserTokensStore` armazena o `poolID/providerID` (ex: `entraid-agentspace/entraid-federation-agentspace`). Backend em `google_auth_install.go:StartGoogleInstallAuth` usa `ai.ParseWIFPoolProvider()` para separar por `/` e chama `ai.StartWIFAppCallback(redirectURI, sessionID, poolID, providerID)` — endpoint `auth.cloud.google/authorize`. Callback retorna para `GET /oauth/google/callback` (porta 8080, forwarded no WSL2). Polling de status em `GET /ai/tokens/google-auth/install/status?session_id=...`. **UI (AISettingsTab.tsx)**: seção Vertex AI tem 3 passos — (1) Projeto GCP, (2) Autenticação com WIF Pool/Provider + botão OAuth + estado "aguardando" com link `<a>` clicável em vez de `window.open`, (3) Service Account JSON (alternativa). Tipo de retorno de `getAITokens()` inclui `gemini_wif_login_url?: string` — necessário para popular o campo ao carregar.

**Modelos Gemini Vertex AI**: `gemini-3.5-flash`, `gemini-3.1-pro-001`, `gemini-2.5-pro-preview-05-06` (Agentspace). Modo Vertex AI não aceita modelos do AI Studio — IDs diferentes. Ver `internal/ai/gemini_provider.go` para lista de modelos por modo.

**Copilot (Azure OpenAI)**: requer `CopilotAPIKey`, `CopilotEndpoint` (ex: `https://my-resource.openai.azure.com`) e `CopilotDeployment`. Env vars: `COPILOT_API_KEY`, `COPILOT_ENDPOINT`, `COPILOT_DEPLOYMENT`.

**OpenAI — Base URL customizado (`OpenAIBaseURL`)**: aponta o provider OpenAI para qualquer endpoint compatível com a API de chat completions (padrão `https://api.openai.com/v1/chat/completions`), ex: **GitHub Models** (`https://models.github.ai/inference/chat/completions`, autenticado com o mesmo PAT do `gh` CLI) — alternativa quando o Vertex AI corporativo não tem a permissão IAM liberada. Propagado para todos os pontos que instanciam o analyzer com esse provider (AI Diagnostics, Predictions, NodePool Predictions). Persistido em `user_ai_tokens.openai_base_url`. Ver [docs/guides/GITHUB_MODELS_SETUP.md](docs/guides/GITHUB_MODELS_SETUP.md) para o passo a passo. Suporta retry com truncamento de prompt em erro 413 (payload too large).

**Diagnósticos de erro Vertex AI/Gemini**: dica específica quando a API retorna 403 (projeto sem permissão IAM), validação de formato do campo WIF Pool/Provider (`poolID/providerID`) e aviso quando um refresh token salvo não consegue mais ser renovado — evita o usuário reautenticar às cegas sem saber qual das 3 causas é a real.

### Dynatrace (Integração de Problems + Correlação K8s)

`internal/dynatrace/` — cliente HTTP para Dynatrace Environment API v2. `DynatraceHandler` em `internal/web/handlers/dynatrace.go` expõe:
- `GET /api/v1/dynatrace/config` — configuração atual (sem expor token)
- `POST /api/v1/dynatrace/test` — testa conectividade
- `GET /api/v1/dynatrace/problems` — lista problems OPEN (com filtro por management zone ou tag)
- `GET /api/v1/dynatrace/problems/:problemId` — detalhes de um problem
- `POST /api/v1/dynatrace/problems/:problemId/analyze` — análise AI do problem
- `GET /api/v1/dynatrace/history` — histórico de análises

Credenciais salvas via `UserTokensStore` (`DynatraceURL` + `DynatraceToken`). Fallback para env vars `DT_API_URL` e `DT_API_TOKEN`. **Atenção**: URL deve usar `*.live.dynatrace.com` (API), não `*.apps.dynatrace.com` (UI) — o client corrige automaticamente.

**Correlação K8s↔DT no Health Check** (`internal/healthcheck/correlator.go`):
- `Correlate(result)` cruza `DeploymentResults`/`HPAResults`/`EventResults` com `DynatraceResults` pelo mesmo workload (`namespace/nome`)
- `newWorkloadKey()` normaliza: lowercase + remove sufixo `:port` (DT usa `namespace/workload:8080`)
- Escalada automática: se K8s severity >= High **E** DT severity >= High → `FinalSeverity = Critical`
- Busca reversa: workloads K8s sem match DT → `SearchProblemsForWorkloads()` pesquisa por `entityName.startsWith()`
- `POST /api/v1/healthcheck/correlated/analyze` — análise AI de um `CorrelatedHealthItem`
- Frontend: 8ª aba "K8s↔DT" em `HealthCheckResultsPanel.tsx` com badges tricolores e botão "Analisar com AI"

**Investigação Profunda (`InvestigateProblem`)** em `internal/web/handlers/dynatrace.go`:
- Fluxo de identificação de cluster/namespace em 3 etapas:
  1. HOST entity → regex `aks-<pool>-XXXXXXXX-vmssXXXXX` → `LookupByNodePool` no registry
  2. Fallback keyword: `extractKeywords(mgmtZones...)` → `LookupByKeyword` (LIKE) → `pickEntryByEnv` escolhe cluster compatível com `extractEnvHint(problem)`
  3. Fallback namespace: `FindNamespaceByKeywords` lista namespaces K8s, filtra por env token exato, pontua por keyword match + bônus de env
- **`extractEnvHint`**: retorna `"prd"` por padrão se nenhum token não-prd (hlg/sit/stg/hml/uat/dev) aparecer no problema DT — nunca analisa cluster hlg para problem sem marcador de ambiente
- **`extractKeywords`**: normaliza acentos PT/ES para ASCII (`"Cálculo"` → `"calculo"`) antes da busca — K8s só aceita nomes ASCII
- **`pickEntryByEnv`**: dentre resultados do registry, prefere cluster com env token igual ao envHint (`"prd"` → escolhe cluster prd, nunca hlg)
- Janela padrão para problems fechados: `now-4h` (API DT usa `now-2h` por padrão — insuficiente)

**"Ver Trace" — deep-link para Distributed Tracing Explorer** (`DynatraceContextPanel.tsx`): botão nas seções de Evidências Davis AI e Topologia que abre `{ui_base_url}/ui/apps/dynatrace.distributedtracing/explorer?filter=dt.entity.service = <entityId>&tf=<timeFrom>;<timeTo>` numa nova aba — URL confirmada empiricamente contra um tenant real (não documentada oficialmente pelo Dynatrace). Só aparece quando `entityType === "SERVICE"` (único tipo com o filtro DQL `dt.entity.service` confirmado). É puramente um link de UI aberto na sessão do próprio navegador do usuário — **não** passa pelo token de API do backend, então não esbarra na limitação de escopo abaixo.

**Por que não é embutido no app**: `GetProblemContext`/`getServiceTraces` (`internal/dynatrace/context.go`) tenta `/api/v2/distributed-tracing/traces`, mas esse endpoint clássico exige o escopo `DataExport`, que praticamente nenhum token de API tem habilitado. A alternativa moderna (Grail, DQL `fetch spans`, o mesmo motor por trás do app "Distributed Tracing" do Dynatrace) exige **Bearer JWT via OAuth2 client credentials** — testado empiricamente contra um tenant real: o token de API clássico (`Authorization: Api-Token ...`) é rejeitado com `"Dynatrace platform APIs require the authorization scheme 'Bearer'"`; tentando `Authorization: Bearer <mesmo token>`, o erro muda para `"Could not parse JWT"` — confirma que APIs de Platform/Grail exigem um JWT de verdade emitido via `sso.dynatrace.com/sso/oauth2/token`, não aceitando de forma alguma o formato de token clássico (`dt0c01...`) mesmo com o escopo certo atribuído. Portanto renderizar o waterfall de spans dentro do app exigiria um client OAuth2 novo (Settings → OAuth clients, escopo `storage:spans:read`) — dependência de quem administra o tenant Dynatrace, fora do controle do time de desenvolvimento; não implementado por falta dessa credencial.

**`/api/v2/logs/search` tem a mesma limitação de Platform/Grail** (`internal/dynatrace/logs.go`, `SearchRecentLogs` — usado só por `TestLogIngestion_Integration` em `internal/dynatrace/logs_ingestion_test.go`, um diagnóstico manual pra confirmar se o OneAgent está ingerindo log, não faz parte de nenhum fluxo funcional da app). Testado empiricamente com curl direto (fora do client Go, pra isolar) contra um tenant real: mesmo um token clássico (`dt0c01...`) recém-criado especificamente "para API v2" é rejeitado com `{"error":{"code":401,"message":"OAuth token is missing"}}` — recriar/rotacionar o token clássico não resolve, é o mesmo bloqueio estrutural do Distributed Tracing acima (endpoint migrado pra trás de Grail/Platform, exige OAuth2 client credentials que a maioria dos usuários não tem permissão de criar). Único jeito de confirmar ingestão de log sem esse client OAuth2: checar manualmente pela UI (Apps → Logs, usando a sessão já logada do navegador) — mesmo contorno já documentado acima para "Ver Trace".

**Node Pool Registry**: `internal/storage/nodepool_registry_store.go` — catálogo SQLite de node pools por cluster (`nodepool_registry.db`), multi-cloud (AKS/EKS/GKE). Handler em `internal/web/handlers/nodepool_registry.go`. Rotas: `GET /api/v1/nodepools/registry`, `GET /api/v1/nodepools/registry/lookup?name=<entity>`, `POST /api/v1/nodepools/registry/scan`. Usado pelo `DynatraceTab` para correlacionar entity names do padrão `aks-<nodepool>-XXXXXXXX-vmssXXXXX` com cluster/vm-size/mode (`Lookup`, via `extractNodePoolFromName` — legitimamente AKS-only, convenção de nome de VMSS) e pelo FinOps (`GetReport`) pra saber quais node pools/VM sizes existem no cluster antes de precificar. Botão "Escanear Clusters" no tab Dynatrace dispara scan em todos os clusters.

**Bug real corrigido — `Scan` descartava silenciosamente nós GKE/EKS**: só reconhecia o label `kubernetes.azure.com/agentpool` (+ fallback de nome de nó `aks-<pool>-<digits>-vmss*`) pra identificar o node pool — nós GKE/EKS não têm esse label, `poolName` ficava vazio e o nó era descartado sem nenhum erro/log (sintoma: `scan` reportava `"scanned": N` nós listados mas `"inserted": 0`, e o relatório FinOps de clusters GKE sempre falhava com "Nenhum node pool encontrado... Execute um scan primeiro" mesmo depois de rodar o scan). Corrigido reaproveitando `nodePoolLabel()` (`nodepools_snat.go`, mesmo pacote, já cobre AKS/EKS/GKE) em vez de duplicar lógica — o fallback histórico de nome de nó AKS foi preservado como camada extra só quando `nodePoolLabel` não reconhece nenhum label conhecido, sem mudar o comportamento existente pra AKS. Validado ao vivo: cluster GKE passou a retornar `"inserted": 1` (pool `gke-pool-higgs-hlg`, `n1-standard-2`), destravando o relatório FinOps completo pra GKE (ver seção FinOps acima).

**GitHub Releases (SSO/SAML)**:
- Autenticação via RBAC Azure AD: email injetado automaticamente pelo middleware `InjectUserEmail` — sem campo de email manual
- Org configurável via `localStorage["github_org"]` (padrão `casas-bahia`). Editar no modal de credenciais GitHub
- PATs precisam ter SSO autorizado: Classic (`ghp_*`) → "Configure SSO" no GitHub; Fine-grained (`github_pat_*`) → criar com org autorizada
- `apiClient.getGitHubOrg()` / `setGitHubOrg()` em `internal/web/frontend/src/lib/api/client.ts`
- ServiceNow: regex de repositório usa `[^/]+` (qualquer org) — não hardcodado. Fallback: se não extrai `github_repo`, usa `deploymentName`

**DynatraceGitHubSection** (`DynatraceGitHubSection.tsx`) — fallback em 3 níveis para correlação K8s↔GitHub sem OneAgent:
1. `k8sWorkloads[].AppName` (OneAgent DTLabels) — mais preciso
2. `k8sWorkloads[].Workload` sem AppName — busca no registry por nome do deployment
3. `affectedEntities[].k8sWorkload` — entidades impactadas com info K8s
- Versão: usa `DeploymentConfig.version` do registry quando DT não tem `AppVersion`
- Requer scan prévio na aba GitHub Releases para popular o registry

**EntityMetricsSection** (`DynatraceMetricsPanel.tsx`) — prop `columns?: 1 | 2`:
- `columns=1` (padrão): layout vertical, tab Métricas
- `columns=2`: grid 2 colunas, tab Diagnóstico — P50/P90/P95/P99 agrupados num único chart
- Fallback: métricas fora dos grupos predefinidos são exibidas como charts individuais genéricos

**Export PDF** (`exportPDF` em `DynatraceTab.tsx`):
- `sanitizePDF()` substitui todos os caracteres Unicode fora do WinAnsi antes de passar ao jsPDF
- Sem isso, caracteres como `═══`, `→`, `—`, `•` geram `%P%P%P` no documento
- `removeEmojis()` mantido apenas para uso fora do PDF

### FinOps — Storage & Relatório Executivo

**Pricer de compute por cloud provider**: `FinOpsHandler.pricerForCluster(cluster)` (`internal/web/handlers/finops.go`) escolhe `*finops.GCPPricer` pra GKE ou `*finops.AzurePricer` pra AKS via `config.DetectCloudProvider`. **Bug real corrigido**: antes, `h.pricer` era um único `*finops.AzurePricer` hardcoded usado incondicionalmente pra qualquer cluster — machine types GCE (`e2-standard-4`) nunca batiam em nenhum SKU Azure, caindo sempre no fallback genérico por família Standard_D/E/F/B, preço sem sentido nenhum pra GCP. `internal/finops/gcp_pricing.go` — `GCPPricer` via Google Cloud Billing Catalog API (`cloudbilling.googleapis.com`, service ID `6F81-5844-456A`, mesmo ID já usado no SNAT). Diferença estrutural confirmada empiricamente: GCP precifica vCPU e RAM **separadamente** por família de máquina (SKUs `"<Família> Instance Core"`/`"<Família> Instance Ram"`), não um preço único por VM inteira como a Azure — preço final = `core_usd_hora*vCPUs + ram_usd_gb_hora*RAM_GB`. A API não filtra por família/região no servidor (só lista o catálogo inteiro paginado, ~35 mil SKUs de Compute Engine) — `refreshCatalog` busca tudo de uma vez (paginação via `nextPageToken`) e cacheia as 16 famílias reconhecidas (e2/n1/n2/n2d/n4/n4d/c2d/c3/c3d/c4/c4a/c4d/t2d/a2/a3/g2) em SQLite (mesmo arquivo/TTL 24h do `AzurePricer`, tabela própria), protegido por `singleflight`. `parseGCEMachineType` interpreta o nome padrão (`<família>-<standard|highmem|highcpu>-<vCPUs>`) matematicamente, sem precisar de tabela de specs fixa (diferente da Azure). **Pricer de disco OS por cloud provider**: `Calculator.osDiskCostForPool` (`calculator.go`) bifurca por provider — AKS mantém 100% o caminho antigo (`OSDiskForNodePool` detecta tamanho ao vivo via label `kubernetes.azure.com/os-disk-size-gb` do node K8s, preço por "tier" fixo tipo Azure Managed Disk P10/P20). GKE **não expõe tamanho/tipo de disco de boot via nenhum label de node** — só a Container API do GKE sabe disso (`NodePool.config.diskSizeGb`/`diskType`), já capturada em `internal/cloudprovider/gcp/nodegroup.go` (mesma chamada que já buscava `machineType`) e persistida no Node Pool Registry (`Scan`, `internal/web/handlers/nodepool_registry.go`, 2 colunas novas `disk_size_gb`/`disk_type`, migração `ALTER TABLE` pra bancos já existentes). Preço GCP é **linear** em USD/GB/mês (`GetDiskPricePerGBMonth`, `gcp_pricing.go`) — sem conceito de tier — confirmado ao vivo: `pd-standard`=$0.06, `pd-balanced`=$0.15 (default real da plataforma GKE desde ~2022), `pd-ssd`=$0.255, `pd-extreme`=$0.188 (southamerica-east1). Reaproveita a MESMA varredura paginada do catálogo de Compute Engine que já busca preço de VM (`extractGCPDiskPrice` roda no mesmo loop de `extractGCPFamilyPrice`) — evita um segundo fetch completo do catálogo. Fallback quando o registry não tem o disco real (cluster nunca re-escaneado depois desta mudança): `pd-balanced`/100GB.

**Gaps conhecidos, não cobertos**: EKS ainda cai no `AzurePricer` (nenhum `AWSPricer` existe); custo de PVC (`StorageCalculator`, PVCs de aplicação — distinto do disco OS do node acima) continua Azure-only; `GetPricing`/`RefreshPricing`/`GetVMAlternatives` (endpoints sem parâmetro de cluster) permanecem Azure-only.

**Tab FinOps possui 7 abas** (ordem): Dashboard → Node Pools → Workloads → HPA Histórico → Armazenamento → Oportunidades → Relatório

**`StorageTab`** (`FinOpsTab.tsx`):
- 4 KPI cards: Custo Total Storage, Nº PVCs, Disco OS R$/mês, Custo Orfãos
- BarChart de custo por tipo de storage (Premium SSD, Standard SSD, Azure Files, etc.)
- Tabela de PVCs com filtro por namespace/tipo, ordenação, badge "orfão" vermelho
- Seção "PVCs Orfãos" colapsável com sugestão `kubectl delete pvc` e botão de cópia
- Aba só aparece quando `report.storage` presente (feature flag implícita)

**`RelatorioTab`** (`FinOpsTab.tsx`):
- Pie chart com composição de custo total: Compute Produtivo + Disco OS + PVCs Ativos + Desperdício (superprovisioning + orfãos)
- Findings priorizados (`critical`/`high`/`medium`) com evidência e ponteiro para a aba correta de ação
- Top 10 workloads por custo total (compute + storage)
- Botão "Exportar PDF": usa `html2canvas` + `jsPDF` (imports dinâmicos); divide canvas em fatias de página A4; arquivo gerado: `finops-relatorio-<cluster>-<date>.pdf`

**Recharts — `renderPieLabel`** (padrão para labels externas em `PieChart`):
- Função retorna `<g>` com dois `<text>` (nome curto + valor/percentual)
- Segmentos < 4% usam `outerRadius + 50` (evita label cair dentro do donut hole)
- `labelLine` deve ser objeto SVG `{ stroke, strokeWidth, opacity }` — **não função** (causa falha de renderização)
- `cursor={{ fill: 'transparent' }}` no `<Tooltip>` para suprimir fundo cinza no hover

**FinOps — cadeia de métricas históricas**: `MetricsSource` em `FinOpsWorkload` indica a fonte usada: `"dynatrace"` (AKS primário), `"newrelic"` (EKS — planejado, `internal/newrelic/` ainda não existe), `""` (sem dados). Cadeia final planejada: DT → NR → Prometheus. Badge cores na UI: DT=azul, NR=âmbar, Prom=laranja.

**FinOps Prometheus**: checkbox "Análise histórica Prometheus" é **`true` por padrão** (era `false`)

**Backend storage** (`internal/finops/`):
- `azure_disk_pricing.go`: `DiskPricer` com cache SQLite + fallback hardcoded por tier (P/E/S series, Azure Files, Blob)
- `storage_calculator.go`: `StorageCalculator.Calculate()` lista PVCs, correlaciona com workloads via ownerRef chain, calcula custo por tier ou por GB
- `storage_calculator_test.go`: 7 funções de teste — `MapStorageClassToAzureType`, `ResolveManagedDiskTier`, custo PVC, Files/Blob, `buildStorageSummary`, detecção de orfãos

**`buildFinOpsPrompt`** (`internal/web/handlers/finops.go`): seção `=== ARMAZENAMENTO ===` com total storage, breakdown por tipo, top 5 workloads por storage, PVCs orfãos com Retain policy destacados

### Conntrack Viewer (Node Pools)

`internal/web/handlers/nodepools_conntrack.go` — dois endpoints:
- `GET /api/v1/nodepools/conntrack?cluster=X&nodepool=Y` — snapshot atual. **Não cria pod efêmero** (documentação antiga estava errada): `findHostNetworkPod` localiza um pod **já em execução** com `hostNetwork:true` no nó alvo (prioriza `kube-proxy`; fallback em ordem — `k8s-app=aws-node` cobre o daemonset do VPC CNI da AWS/EKS, depois `azure-npm`/qualquer pod hostNetwork em `kube-system`; clusters EKS com `kube-proxy-replacement` do Cilium ou EKS Auto Mode/Fargate podem não ter nenhum candidato) e executa `exec` nele lendo os 3 sysctls diretamente — `/proc/sys/net/netfilter/nf_conntrack_count`, `nf_conntrack_max`, `nf_conntrack_buckets` (não conta linhas de `/proc/net/nf_conntrack`). **Sem cache** — cada chamada faz exec de novo (não existe `conntrackCache`/TTL apesar do que documentação antiga sugeria).
- `GET /api/v1/nodepools/conntrack/history?cluster=X&node=Y&hours=&step=&offset_days=` — histórico via Prometheus (`node_nf_conntrack_entries`/`node_nf_conntrack_entries_limit`), retorna array de pontos time-series. `offset_days` (0-7) desloca a janela inteira mantendo o mesmo horário do dia (comparação D-1/D-2/D-3).

**Fallback gracioso**: se Prometheus indisponível, histórico retorna array vazio — frontend exibe apenas snapshot atual sem mensagem de erro ao usuário.

**Frontend**: `ConntrackTab.tsx` (não `ConntrackViewerTab.tsx`) — BarChart comparando snapshot atual vs histórico 24h. Recomendação automática por nó (`getCapacityRec`): OK / Monitorar tendência (p95 histórico ≥65%) / Spike ativo / Aumentar limite (p95 ≥80%). **Scan automático e silencioso**: `useEffect([cluster, nodepool])` dispara `fetchStats()` ao abrir a aba (mount) e ao trocar de node pool com a aba já aberta — necessário porque `NodePoolEditor` não é remontado na troca de pool (só recebe novo prop `nodePool`), então sem esse efeito a aba ficava com dados do pool anterior até o usuário clicar manualmente em "Atualizar" (que continua existindo pra re-scan sob demanda).

**`ConntrackAlertWidget.tsx`**: badge compacto sempre visível na aba Node Pools assim que um cluster é selecionado (mesmo padrão visual do `SNATPortWidget.tsx`, renderizado ao lado dele no `titleAction` do painel direito — ver nota de layout na seção "Diagnóstico SNAT" abaixo), mostrando o nó mais saturado de **todo o cluster** — não depende de nenhum node pool estar selecionado. `GetConntrackStats` (`nodepools_conntrack.go`) aceita `nodepool` como query param **opcional**: se omitido, `resolveAllClusterNodes` lista e escaneia todos os nós do cluster (mesmo padrão paralelo de `probeConntrack` por nó); se informado, mantém o comportamento antigo filtrado por pool (usado pelo `ConntrackTab.tsx`). Limiares do widget: "Atenção" ≥65% (mesmo valor de `getCapacityRec`), "Crítico" ≥90% (mesmo limiar do backend em `probeConntrack`). Clique abre modal simples com a lista de nós ordenada por uso.

**Comparação D-1/D-2/D-3**: grupo de botões multi-seleção "Comparar: D-1 D-2 D-3" no header sobrepõe, no gráfico de cada nó, o uso histórico do mesmo horário N dias atrás em cima da série de hoje. `HistoryChart` é um `ComposedChart` — barras verde/amarelo/vermelho por threshold continuam representando "hoje", cada dia de comparação vira uma `Line` tracejada (D-1 laranja `#f97316`, D-2 roxo `#a855f7`, D-3 cinza `#64748b`) alinhada por índice relativo do array decimado (`decimate()`, não por timestamp absoluto — funciona pois os offsets são múltiplos exatos de 24h). Estado `compareHistoryMap: Record<offset, Record<nodeName, ConntrackNodeHistoryResponse>>` cacheia por offset+nó. **Cuidado com `ChartTooltipContent` do shadcn** ao adicionar overlays multi-série num `ComposedChart`: ele resolve a cor do indicador via `item.payload.fill`, mas todas as séries de um mesmo ponto compartilham o mesmo `payload` (a linha do chartData) — o `fill` de uma série vaza para as outras. Usar `formatter` custom no `ChartTooltip` que resolve cor/label explicitamente por `item.dataKey`, nunca depender do `payload.fill` compartilhado.

**Bug corrigido — fallback de limite fixo mascarava saturação real**: `internal/monitoring/nodepoolpredictions/collector.go` (`collectConntrackPool`, `collectConntrackCluster`, e o snapshot de tendência D-7) e `internal/monitoring/predictions/collector.go` — quando a query `node_nf_conntrack_entries_limit` não retornava série pro `instance` de um node (label não bate no Prometheus), o código **fabricava** um limite fixo de `131072` em vez de tratar como dado indisponível. Se o limite real do node fosse maior (comum em VMs maiores/tunadas, ex: `524288` = 4x o valor chutado), o percentual usado nos alertas de IA saía inflado (ex: 20% real virava 80% "alertado"). Corrigido nos 4 pontos: quando o limite real não é encontrado, o node é excluído do cálculo (log de aviso) em vez de fabricar percentual — vale tanto pra visão de Node Pools quanto pro prompt de IA de Deployments. **Nota**: esse bug era sobre os alertas de IA ficarem inflados; se o problema for o oposto (nossa leitura de conntrack — scan ao vivo ou histórico via Prometheus — mostrar valor **mais baixo** que outra ferramenta pro mesmo node/horário), a causa raiz não foi encontrada nesta rodada — hipóteses de node pool errado e resultado `Result[0]` ambíguo no Prometheus foram levantadas mas não confirmadas contra um cluster real; investigar de novo se recorrer.

### Diagnóstico SNAT (Node Pools)

`internal/web/handlers/nodepools_snat.go` + `SNATPortWidget.tsx` — diagnóstico de portas SNAT multi-cloud (AKS/GKE/EKS). Visível quando um cluster está selecionado.

**Layout do `SNATPortWidget.tsx`/`ConntrackAlertWidget.tsx`**: os dois viviam empilhados acima do `SplitView` inteiro (`Index.tsx` case `"nodepools"`), cada um ocupando uma linha de largura total — com `ml-auto` empurrando os valores pro canto oposto do botão, o que ficava com um vão enorme entre rótulo e valor em telas largas. Movidos pro `titleAction` do painel direito (`SplitView`, "Node Pool Editor") lado a lado, condicionados só a `selectedCluster` — **não** a `selectedNodePool` (continuam visíveis mesmo sem nenhum node pool escolhido; só o botão de refresh do editor depende de ter um pool selecionado). Botão de cada widget usa `w-fit` + `whitespace-nowrap` (não mais `w-full`/`truncate`) — o conteúdo nunca é cortado, o próprio `flex-wrap` do cabeçalho quebra pra nova linha se não couber ao lado do título.

**Atenção**: `NodePoolTab.tsx` é um componente órfão — **nunca é importado** pela aplicação. Toda adição de feature na aba Node Pools deve ir em `Index.tsx` (case `"nodepools"`) ou `TabContent.tsx` (sistema multi-tab), não em `NodePoolTab.tsx`.

**Endpoints**:
- `GET /api/v1/nodepools/snat?cluster=<cluster>` — perfil atual; salva snapshot no histórico SQLite de forma assíncrona (exceto EKS, onde `AllocatedOutboundPorts=0`)
- `GET /api/v1/nodepools/snat/projection?cluster=<cluster>` — histórico 30 dias + regressão linear para projeção de crescimento
- `GET /api/v1/nodepools/snat/nodes?cluster=<cluster>` — breakdown por nó via Prometheus (conntrack como proxy)
- `GET /api/v1/nodepools/snat/costs?cluster=<cluster>` — preços de referência via API nativa de cada cloud provider (ver abaixo)

**Detecção de provider** (`detectSNATProvider(serverURL, clusterCtx)`): usa `config.DetectCloudProvider(serverURL, clusterCtx)` — a mesma fonte de verdade de `GetRestConfig`/`GetNodeGroupProvider` —, não mais o prefixo do nome do context. Contexts EKS nem sempre têm o prefixo `arn:aws:eks:` (clusters descobertos via `autodiscover`/renomeados manualmente podem usar um alias amigável, ex: `asaplog-production-admin`, mesmo sendo EKS de fato); classificar só pelo prefixo fazia esses clusters caírem em "aks" por padrão e todo o fluxo de SNAT/conntrack falhar (cluster não encontrado em `clusters-config.json`, exclusivo de AKS). `GetEKSClusterConfig` também precisou aprender a normalizar: quando `clusterName` chega como ARN completo (formato usado como cluster/context query param para EKS), extrai o nome curto após a última `/` antes de comparar com `Name` em `eks-clusters-config.json` (que é sempre o nome curto, populado via `aws eks list-clusters`) — mesmo padrão já usado em `resolveAWSProfile`. `fetchEKSCosts` ganhou `profile`/`regionHint` (lidos de `EKSClusterConfig`) para não depender só de `extractAWSRegionFromARN` (que só funciona com ARN completo) e para reaproveitar o profile AWS certo em contas/perfis múltiplos por cluster.

**Constantes de portas**:
- AKS: `snatPortsPerIPAzure = 64000` — portas SNAT por IP público no LB
- GKE: `snatPortsPerIPGCP = 64512` — faixa 0-64511 do Cloud NAT
- EKS: `snatPortsPerIPAWS = 55000` — conexões simultâneas por EIP/destino (modelo diferente)

**Builder por provider**:
- `buildSNATProfileAKS` — `az aks show` para `allocatedOutboundPorts` + `managedOutboundIPs.count` (timeout 30s)
- `buildSNATProfileGKE` — verifica auth GCP (`GCPAuthManager.CheckStatus`) antes de chamar `gcloud compute routers list --regions <region> --format json(name,nats)`. Zona → região: strip último segmento quando tem 1 char (ex: `us-central1-a` → `us-central1`). Default `minPortsPerVm=64` quando não configurado. `AUTO_ONLY` NAT IPs: conta 1 por NAT (subestimado — aviso no campo `Error`). Retorna `RequiresGCPAuth=true` quando gcloud presente mas não autenticado
- `buildSNATProfileEKS` — `aws ec2 describe-nat-gateways` conta EIPs; `AllocatedOutboundPorts=0` sinaliza modelo diferente (não por nó); `Error` descreve o modelo AWS
- `buildSNATProfileFromValues` — fórmula compartilhada AKS + GKE: `totalAvailable = ipCount × portsPerIP`, `totalRequired = allocatedPorts × nodes`. Status: `ok` (<80%), `warning` (80-95%), `critical` (≥95%)

**`SNATProfile`** contém:
- `CloudProvider string` — `"aks"`, `"gke"`, `"eks"`
- `AllocatedOutboundPorts` — 0 para EKS (modelo N/A)
- `MaxNodesAllowed` — máximo de nós suportados antes de falha SNAT (0 para EKS)
- `NodesUntilLimit` — quantos nós ainda cabem
- `IPsNeededForCurrentNodes` — IPs adicionais necessários para cobrir a carga atual
- `RequiresGCPAuth bool` — true quando gcloud não autenticado (GKE); frontend exibe tela de login

**Detecção de node pool por cloud** (`nodePoolLabel`): tenta label `kubernetes.azure.com/agentpool` (AKS), `eks.amazonaws.com/nodegroup` (EKS), `cloud.google.com/gke-nodepool` (GKE) — na ordem, retorna o primeiro não-vazio.

**Histórico SQLite** (`internal/storage/snat_history_store.go`): store `snat_history.db` em WAL mode. Deduplica: no máximo 1 snapshot/hora/cluster. Retenção 90 dias. Não salva quando `AllocatedOutboundPorts == 0` (EKS). Métodos: `Save`, `GetRecent(cluster, days)`, `GetLatest(cluster)`, `ComputeSNATProjection(records, nodesUntilLimit)`.

**Projeção de crescimento** (`ComputeSNATProjection`): regressão linear sobre `total_node_count` ao longo do tempo. Confiança: `high` (≥14 pontos + ≥7 dias de span), `medium` (≥5 pontos + ≥2 dias), `low`, `none`. Retorna `GrowthPerDay`, `DaysUntilLimit` (-1 = indeterminado), `EstimatedDate`.

**Frontend**: header compacto sempre visível. Quando `requires_gcp_auth`, exibe badge âmbar "Login GCP necessário". Clique abre `Dialog` (`max-w-2xl`, `max-h-[78vh]`).

**Auth GCP no widget** (`SNATPortWidget.tsx`): quando `data.requires_gcp_auth && data.cloud_provider === "gke"` (`gcpNeedsAuth`), o modal exibe tela de autenticação inline com Device Auth Grant — mesmo fluxo de `AutoDiscoverDialog` (`checkGCPAuth` / `startGCPLogin` / `pollGCPLogin` via `/api/v1/gcp/auth/status|login|poll`). Após login bem-sucedido: `refetch()` recarrega o perfil SNAT. Tabs e conteúdo ficam ocultos enquanto auth é necessária.

**Pricing nativo por cloud** (`internal/web/handlers/nodepools_snat_costs.go`): endpoint `GetSNATCosts` busca preços reais da API de cada provider; fallback documental quando a API falha.

| Provider | API usada | O que busca |
|---|---|---|
| AKS | Azure Retail Prices API (`prices.azure.com`) | IP público Standard em BRL, `armRegionName=brazilsouth`, `productName=IP Addresses` |
| GKE | GCP Cloud Billing Catalog API (`cloudbilling.googleapis.com`) via ADC token (`GetFreshGKEToken`) | SKUs por substring: `"cloud nat gateway"`, `"cloud nat data"`, `"external ip in use"` nos service IDs `95FF-2EF5-5EA1` (Networking) e `6F81-5844-456A` (Compute Engine) |
| EKS | AWS Pricing API via `aws pricing get-products --service-code AmazonVPC --region us-east-1` | Grupos `NGW:NatGateway` (por hora) e `NGW:Data` (por GB); extrai preço via `terms.OnDemand.*.priceDimensions.*.pricePerUnit.USD` |

**`SNATCostInfo`** (resposta de `/snat/costs`): `ip_price_monthly` (AKS/GKE: custo por IP/mês), `gw_hourly_price` (GKE/EKS: custo por hora do NAT GW), `data_price_per_gb` (GKE/EKS: custo por GB processado), `currency` (`"BRL"` para AKS, `"USD"` para GKE/EKS), `pricing_region`, `source` (`"azure-retail-api"` / `"gcp-billing-api"` / `"aws-pricing-api"` / `"reference"`).

**`FALLBACK_COSTS`** (frontend): map de fallbacks documentais por provider, usados quando o endpoint `/snat/costs` falha — AKS: R$20/IP/mês, GKE: $0.004/h IP + $0.044/h NAT GW + $0.045/GB, EKS: $0.044/h NAT GW + $0.044/GB.

**5 abas manuais** (div + estado — nunca shadcn `<Tabs>`):
- **Diagnóstico** — barra de uso (AKS/GKE) ou info NAT GW (EKS), 6 cards métricas, seção Capacidade, breakdown por node pool
- **Financeiro** — usa `SNATCostInfo` do endpoint `/snat/costs` (lazy, cache 1h) com `fmtCost()` que formata BRL ou USD conforme `currency`; badge indica a fonte dos preços; AKS: custo IP/mês em BRL real; GKE: NAT GW/hora + IP externo/mês + GB em USD real; EKS: NAT GW/hora + GB em USD real com estimativa mensal. Fallback para `FALLBACK_COSTS` quando API indisponível
- **Fórmula** — AKS/GKE: equações passo a passo; EKS: modelo 55k conexões simultâneas por EIP
- **Projeção** — taxa de crescimento (nós/semana), badge de confiança, `LineChart` com `ReferenceLine` em `max_nodes_allowed`
- **Nós** — tabela com mini progress bars; usa `conntrack_usage_pct` como proxy quando `allocated_ports === 0` (EKS); histórico conntrack 6h via `AreaChart`

**Integração com análise preditiva de node pools**: `NodePoolPredictionsHandler` lê o snapshot mais recente do SQLite antes de chamar `analyzer.Analyze()` e popula `req.SNATContext` (`SNATContextData`). O prompt gerado inclui seção `# SNAT DO LOAD BALANCER AKS` com status, capacidade de nós, projeção de crescimento e comandos corretivos `az aks update`. Categorias `"snat"` adicionadas ao schema JSON de `root_cause` e `recommendations`.

Cache de 2min via React Query (`staleTime: 2 * 60 * 1000`).

**Por que não usar Conntrack aqui**: SNAT é uma limitação do Load Balancer/NAT Gateway (fora do node), não do kernel conntrack. São complementares — conntrack serve como proxy de estimativa por nó, mas o orçamento SNAT real vem da API de cada cloud.

### HPAEditor (HPA Tab)

`HPAEditor.tsx` — editor standalone de HPA extraído do inline editor do `HPAListItem`. Substitui o painel de edição que ficava acoplado ao item da lista.

**Funcionalidades**:
- Edição de `minReplicas`, `maxReplicas`, `targetCPU`, `targetMemory`
- Edição de recursos (`cpuRequest`, `cpuLimit`, `memoryRequest`, `memoryLimit`)
- Checkbox "Incluir no Staging sem alterar valores" — adiciona o HPA ao staging context sem modificar nenhum campo (útil para incluir HPAs na sessão de staging como referência ou para rollout)
- Checkbox para rollout de Deployment / DaemonSet / StatefulSet após aplicar
- Usa `useK8sPermissions` para habilitar/desabilitar botões conforme RBAC do cluster
- `ProtectedAction allowed={permissions.canUpdateHPA}` — usa RBAC K8s, não grupo AD

**Integração**: `HPATab.tsx` renderiza `HPAEditor` em painel lateral quando um HPA é selecionado na lista.

### Sincronização de foco entre painel-lista e painel-tabela (Deployments, Pods, DaemonSets, etc.)

Todas as abas de workload (`DeploymentsTab`, `PodsPanel`, `IngressTab`, `GatewayTab`, `ConfigMapsTab`, `SecretsTab`, `ServicesTab`, `DaemonSetsTab`, `StatefulSetsTab`, `VPAsTab`, `CronJobsTab`, `NamespacesTab`) usam `SplitView`: lista de cards à esquerda + detalhe/editor ou `*MonitorTable` à direita. Quando o item "ativo" no painel direito muda (seleção real ou drill-down), o card correspondente na lista esquerda rola até a área visível via hook `hooks/useRevealOnKeyChange.ts`:

```ts
useRevealOnKeyChange(containerRef, focusKey); // containerRef aponta pro wrapper da lista, cada card tem data-item-key
```

Escopado por `containerRef` (nunca `document.querySelector`) porque várias dessas abas ficam montadas em `display:none` em segundo plano — uma busca global casaria com uma instância oculta de outra aba.

**Duas categorias**:
- **Categoria A — Deployments e DaemonSets** (têm drill-down real de pods): a `*MonitorTable` do painel direito, ao clicar numa linha, chama um handler que muda `rightView` para `{kind:"pod-table", ...}` (mostrando os pods daquele workload via `PodMonitorTable`) **sem** tocar o estado de seleção canônico (`selectedDeployment`/`selectedDaemonSet`). Nesse caso a chave de foco é derivada do `rightView` e o card ganha um anel azul independente (`ring-2 ring-blue-400/60 bg-blue-400/5`, só quando `!isSelected`) — visualmente distinto do destaque roxo de "selecionado/aberto para edição". O ícone de lápis na tabela abre o YAML normalmente (`onOpenEditor`); clicar na linha em si dispara o drill-down (`onSelectDeployment`/`onSelectDaemonSet`).
- **Categoria B — as demais abas** (só têm uma ação: `onOpenEditor` direto): a chave de foco é a do item selecionado (`selectedX`) — o destaque já existe (`border-primary`), só faltava o auto-scroll.

**Pods** (`PodsPanel.tsx`) é uma variação da Categoria A: clicar numa linha do `PodMonitorTable` do painel direito abre o quick-view modal (`setQuickViewPod`) sem tocar `selectedPod` — o card ganha o mesmo anel azul.

### `*MonitorTable.tsx` — foco de teclado explícito, não `:focus-visible`

Navegação por seta (↑/↓) nessas tabelas usa `data-row-index` + `el.focus()` programático (`focusRow()`). **Não depender de `focus-visible:ring-*` no CSS** para indicar a linha em foco — o heurístico de `:focus-visible` do navegador nem sempre trata `el.focus()` programático como "foco visível" (varia por browser), deixando a navegação por teclado sem indicação visual. Solução: estado explícito `focusedRowIndex` atualizado via `onFocus`/`onBlur` na linha, aplicando uma classe incondicional (`ring-2 ring-inset ring-primary/70`) — não depende de heurística nenhuma. Ver `PodMonitorTable.tsx`.

### PodQuickViewModal — Destaque visual do termo buscado nos logs

O campo "Buscar nos logs..." (abas Logs e Logs Anteriores, mesmo `LogsViewer` reaproveitado pelas duas) já filtrava as linhas via `filterLogLines` (`line.toLowerCase().includes(q)`), mas não indicava **onde** dentro da linha o termo batia — em linhas longas (comum em log de sidecar Istio/Envoy), o usuário tinha que reler a linha inteira pra achar o trecho. `highlightMatches()` fatia cada linha renderizada em torno das ocorrências (case-insensitive, `RegExp` com o termo escapado via `escapeRegExp()` — sem isso, um termo digitado com `.`/`(`/`[` etc. seria interpretado como regex em vez de texto literal e o destaque saía errado ou a regex quebrava) e envolve cada match num `<mark>` (`bg-yellow-400/80 text-black`). Mesma lógica cobre `filteredLines` da aba atual e da aba "Logs Anteriores" — um único componente `LogsViewer` renderiza as duas, então um único ponto de mudança bastou. Validado contra log real de um pod (`sftp-neogrid-hlg-0`, container `istio-proxy`) via Playwright — buscar um timestamp específico da linha realça exatamente o timestamp, não a linha inteira.

### PodQuickViewModal — aba "Mesma Imagem" (estilo k9s)

4ª aba do quick-view de pod (`Detalhes | Logs | Logs Anteriores | Mesma Imagem`), ao lado das outras já documentadas na seção de Tabs em Modais. Reaproveita o seletor de container já usado nas abas de Logs (`selectedContainer`) para buscar, em **todos os namespaces do cluster atual**, outros pods que rodam a mesma imagem do container selecionado — útil para analisar o "raio de impacto" de uma imagem compartilhada (ex: antes de atualizar uma imagem base), do jeito que o k9s permite correlacionar por imagem.

- Busca via `apiClient.getPods(cluster, undefined, undefined, true, true)` (sem filtro de namespace = todos; `showSystem=true` inclui `kube-system`), filtrando client-side por `container.image === selectedContainerImage` e excluindo o próprio pod.
- Resultado cacheado em memória por `cluster::imagem` (`sameImageCache`, `useRef<Map>`) — evita rebuscar ao trocar de aba e voltar.
- **Escopo intencional: só o cluster atual** (não varre todos os clusters cadastrados) — mesmo escopo do k9s, que opera sempre no contexto de um cluster por vez.

**Botões de ação (Rollout Restart/Kill/Deletar) na linha das abas, não no conteúdo rolável**: esses 3 botões — e a barra de confirmação inline que aparece ao clicar um deles — ficam na mesma linha da tab bar (lado direito, só quando `activeTab === "details"`), não mais dentro da área `overflow-y-auto` da aba Detalhes. Motivo: pods com muitos containers/labels empurravam os botões para fora da tela, exigindo scroll para agir. O seletor de container + botão "Atualizar" da aba "Mesma Imagem" segue o mesmo padrão (lado direito da tab bar, visível só nessa aba) — ambos os grupos de controle coexistem na mesma linha, cada um condicionado ao `activeTab` ativo.

### PodQuickViewModal — Causa real de crash/reinício (Exit Code, Last State, Eventos do workload)

A aba "Logs Anteriores" fazia só `kubectl logs --previous` do **Pod object atualmente selecionado**, bloqueando totalmente quando `restartCount === 0` nesse pod — inútil no caso mais comum de troubleshooting: depois de um rollout, o Deployment tem um Pod **novo** (hash diferente, `restartCount = 0`); o pod antigo que crashou (ex: OOMKilled/137, SIGTERM/143) já foi deletado, e nem `kubectl logs --previous` nem `kubectl describe pod` conseguem ver um Pod object que não existe mais — limitação real do Kubernetes (esta app não tem backend de agregação de logs externo tipo Loki/ELK). A mudança maximiza o que **é** tecnicamente recuperável em vez de prometer logs brutos impossíveis de obter:

- **`LastTerminationState`** — antes descartado por completo (`getContainerState` só olhava `cs.State`, nunca `cs.LastTerminationState`). Novo `ContainerStatus.LastState` (`internal/web/handlers/pods.go`, `buildContainerLastState`) expõe Exit Code/Signal/Reason/Message/StartedAt/FinishedAt do reinício anterior **deste mesmo Pod object** — funciona sempre que o pod já reiniciou sozinho (CrashLoopBackOff vivo), sem precisar abrir o Describe. Renderizado como linha inline em cada container na aba Detalhes, como banner colorido (`LastStateBanner`) no topo da aba "Logs Anteriores", e como tabela "Códigos de saída encontrados" no topo do modal Describe do pod — as 3 fontes usam o mesmo helper `describeExitCode()` (`internal/web/frontend/src/lib/exitCodes.ts`, mapa de significado: 137=SIGKILL/OOMKilled, 143=SIGTERM, 139=SIGSEGV, etc., com fallback `128+sinal` pra códigos não mapeados).
- **Eventos de Warning do workload dono** — Events do K8s são objetos independentes do Pod, sobrevivem à sua deleção até expirar pelo TTL do cluster (tipicamente ~1h). Na aba "Logs Anteriores", quando o pod atual não tem restart (`selectedContainerRestartCount === 0`), busca `apiClient.getEvents(cluster, [namespace], workloadSearchTerm, "Warning", true)` — endpoint `/api/v1/events` **já existente**, sem rota nova — filtrando pelo nome do **workload** (não do pod), então cobre também pods de antes do rollout. `workloadSearchTerm` vem do novo campo `PodSummary.OwnerWorkload` (`"Deployment/checkout-api"`), preenchido reaproveitando `resolveOwnerDisplayName()` — a mesma função já usada no badge de uso de ConfigMaps (ver seção acima) — sem import novo, mesmo pacote `handlers`.
- Mensagem de bloqueio da aba "Logs Anteriores" ajustada: quando `restartCount === 0` só a busca do log bruto `--previous` é pulada (ela realmente não existe); o banner de Last State e a lista de eventos continuam tentando mostrar algo útil em vez de cortar a aba inteira numa frase só.

**Bug de UI corrigido no mesmo lote — badge de status virando barra vermelha sólida**: o badge no header mostrava `pod.statusReason || pod.phase`, mas `PodSummary.StatusReason` (`getPodStatus()` em `pods.go`) é preenchido com a **mensagem** longa do evento K8s (ex: `"back-off 5m0s restarting failed container=... asaplog(uuid)"`), não um motivo curto — um badge de altura fixa (`h-4`) com esse texto vira uma barra vermelha sólida ocupando a largura toda do header. `pod.status` (campo separado) é sempre curto (`"CrashLoopBackOff"`, phase, etc. — mesmo padrão já usado corretamente em `PodMonitorTable.tsx:713-714`). Badge trocado pra mostrar `pod.status || pod.phase` com `max-w-[320px] truncate`, mensagem longa só no `title` (tooltip).

**Modal Describe do pod sem scroll — lição de `max-height` vs `height`**: ao adicionar a tabela de exit codes acima do texto bruto do describe, o `DialogContent` (`max-h-[85vh]`, sem `overflow-hidden`) deixou o conteúdo extra vazar pra fora do modal — `max-height` sozinho não recorta nada, só limita o tamanho da caixa; sem `overflow` controlado o conteúdo continua renderizando visualmente além da caixa. Trocar `max-h-[85vh]` por `overflow-hidden` ainda não bastou: com **altura automática** (só um teto/`max-height`), o filho `flex-1 min-h-0` (`ScrollArea` do texto do describe, antes com `h-[60vh]` fixo) não tinha uma altura *determinada* do pai pra se basear, então crescia pra caber o conteúdo em vez de rolar. Correção final: `h-[85vh]` (altura **fixa**, não `max-h`) + `flex flex-col overflow-hidden` no `DialogContent` — mesmo padrão já usado com sucesso no `DialogContent` externo deste mesmo modal (`style={{ height: modalSize.height }}`). Regra geral: numa cadeia `flex-1 min-h-0` pra scroll interno funcionar, o container flex precisa de uma altura **determinada** (fixa ou herdada), nunca só um `max-height` — complementa a nota já existente sobre `min-h-0` em filhos de `flex-row`.

### shadcn Tabs em Modais com Altura Fixa

`TabsContent` do Radix UI usa `[data-state=active]:block` — o `display: block` quebra qualquer cadeia de `flex-1 min-h-0`. **Nunca usar shadcn `<Tabs>` em contextos onde a aba precisa preencher altura restante** (ex: modais, painéis com `flex-1`).

**Solução**: implementação manual com `div` + estado local + renderização condicional:

```tsx
// ✅ CORRETO — controle total do flex chain
const [activeTab, setActiveTab] = useState<"details" | "logs">("details");

<div className="flex-1 flex flex-col min-h-0">
  <div className="flex border-b border-border px-4 pt-3 gap-1 flex-shrink-0">
    {(["details", "logs"] as const).map(tab => (
      <button key={tab} onClick={() => setActiveTab(tab)}
        className={`px-3 py-1.5 text-xs font-medium transition-colors border-b-2 -mb-px ${
          activeTab === tab ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
        }`}>
        {tab === "details" ? "Detalhes" : "Logs"}
      </button>
    ))}
  </div>
  {activeTab === "details" && (
    <div className="flex-1 min-h-0 overflow-y-auto">...</div>
  )}
  {activeTab === "logs" && (
    <div className="flex-1 flex flex-col min-h-0">...</div>
  )}
</div>
```

Ver implementação em `PodQuickViewModal.tsx`.

### `min-h-0` também é necessário em filhos de flex-row, não só flex-col

Um painel lateral dentro de um container `flex` (linha, não coluna) — ex: lista mestre à esquerda de um painel de detalhe à direita, como no modal de amostra de dados do Teste de Banco de Dados (`DatabaseTestTab.tsx`) — não rola mesmo tendo `overflow-y-auto`, se faltar `min-h-0`. Motivo: um item de flex tem `min-height: auto` por padrão, o que deixa o conteúdo esticar o container ao invés de ser cortado/rolado — essa regra vale nos dois eixos do flex (row e column), não só na cadeia vertical mais comum (`flex-col` + `flex-1 min-h-0`). Sempre que um painel com `overflow-y-auto` não rolar dentro de um pai `flex` (independente da direção), verificar se `min-h-0` está no próprio painel.


### MonitorUtils — Conversão de Recursos K8s

`internal/web/frontend/src/lib/monitorUtils.ts` centraliza funções de formatação e parsing de recursos K8s:

- `parseCpuToMillicores(s)` — converte `"300m"` → 300, `"1"` → 1000, `"0.5"` → 500
- `parseMemoryToBytes(s)` — converte `"500Mi"`, `"4Gi"`, `"1024Ki"`, `"1G"` para bytes
- `formatMillicores(m)` — formata millicores para exibição (`"250m"`, `"1.5"`)
- `formatBytes(b)` — formata bytes para exibição (`"128Mi"`, `"2.50Gi"`)

Usar essas funções ao calcular percentuais de uso vs. limit/request. **Nunca calcular percentuais inline em componentes** — usar os parsers do `monitorUtils`.

**Aviso de `metrics-server` ausente** (`PodsPanel.tsx`): comum em clusters EKS sem `metrics-server` instalado (não vem por padrão, diferente do AKS). `GetPodMetricsFromServer`/`GetBatchPodMetrics` (`internal/kubernetes/client.go`) propagam o erro real de `metrics.k8s.io` em vez de engolir com uma mensagem genérica; `PodsPanel.tsx` exibe badge âmbar "⚠ Métricas indisponíveis" com o erro real no `title` (tooltip) quando `metrics.available === false`.

### JSON Inspector (Logs)

Ferramenta de inspeção e formatação de JSON embutida em todos os visualizadores de log. Ativada por **seleção de texto** — o usuário seleciona um trecho do log e clica no botão flutuante que aparece.

**Arquivos:**
- `src/lib/jsonFormatter.ts` — `tryFormatJson(input)`, `tokenizeJson(line)` e `extractJsonBlock(text)`
- `src/hooks/useJsonInspector.ts` — hook de detecção de seleção via `selectionchange` event + posicionamento do botão flutuante via `getRangeAt(0).getBoundingClientRect()`
- `src/components/JsonInspectorModal.tsx` — modal split-view com entrada editável (esquerda) e saída formatada + syntax highlight + numeração de linhas (direita). Também exporta `JsonFloatingButton`

**Componentes que usam o inspetor** (padrão idêntico nos 3 primeiros):
- `PodLogsPanel.tsx` — `onMouseUp={jsonInspector.handleMouseUp}` no container de log
- `PodQuickViewModal.tsx` — idem na área de scroll de logs
- `ContainersTab.tsx` — wrapper `<div onMouseUp={...}>` em volta do `<ScrollArea>` de logs
- `LogViewer.tsx` — abordagem diferente: `<Textarea ref={textareaRef} onSelect={...}>` + botão no toolbar (sem botão flutuante, pois `window.getSelection()` não funciona em textarea)

**Comportamento crítico de `tryFormatJson`:**
1. Tenta `JSON.parse()` no texto completo
2. Se falhar, chama `extractJsonBlock()` que percorre o texto procurando o primeiro `{` ou `[` e extrai o bloco balanceado — necessário para logs com prefixo (`2024-01-01T12:00:00Z INFO {"msg":"..."}` → extrai `{"msg":"..."}`)
3. Se a extração tiver sucesso, retorna `wasExtracted: true` → modal exibe aviso âmbar
4. Se tudo falhar, retorna `errorLine`/`errorCol` extraídos da mensagem V8 (`"(line N column M)"`) → linha com erro é destacada em vermelho no painel direito

**Renderização linha-a-linha** (design crítico): `ValidJsonPanel` e `InvalidJsonPanel` iteram `json.split("\n")` e tokenizam **cada linha individualmente** via `tokenizeJson(line)`. Isso garante que o número da linha N sempre corresponda ao conteúdo da linha N. **Não tokenizar o JSON completo** num único passo — o alinhamento com os números de linha quebra quando tokens `space` contêm `\n`.

**Formato de log correto para FluentD + EventHub**: JSON puro por linha com timestamp embutido:
```json
{"time":"2024-06-08T12:00:00Z","level":"INFO","msg":"pod started","pod":"api-7d9f"}
```
O formato `TIMESTAMP LEVEL {JSON}` (timestamp fora do objeto) falha no FluentD `@type json` e no EventHub consumer. O inspetor detecta esse caso via `extractJsonBlock` e avisa com o badge âmbar.

### ServiceNow — Rod (Go nativo) + WSL2 CDP

`internal/servicenow/` — extração de CHGs via browser automation com **go-rod v0.116.2** (Go nativo, sem Node.js/npm). Suporta autenticação SAML/SSO do Azure AD com persistência de sessão.

**Dois modos de execução** (selecionados automaticamente por `NeedsWindowsBrowser()`):
- **Modo local**: Chromium baixado automaticamente pelo Rod (`launcher.New()`). Sessão em `~/.k8s-hpa-manager/rod-session/`.
- **Modo Windows/WSL2**: Chrome/Edge do Windows via CDP na porta **`9223`** (não 9222 — evita conflito com instâncias existentes). Rod conecta em `ws://<windows-host>:9223`. Sessão no caminho Windows configurado em `BrowserConfig.WindowsSessionDir`.

**Precedência de `NeedsWindowsBrowser()`:**
1. Env var `K8S_HPA_WINDOWS_BROWSER=true` — força modo Windows
2. Config persistida em `~/.k8s-hpa-manager/servicenow-browser.json`
3. Auto-detect: WSL sem display gráfico (`DISPLAY`/`WAYLAND_DISPLAY` vazios)

**Sessão Azure AD**: expira em ~8h. `RodExtractor.GetSessionStatus()` valida pelo timestamp de modificação do diretório. `ClearSession()` remove e recria o diretório vazio.

**Endpoints de gerenciamento de sessão:**
- `GET /api/v1/servicenow/session-status` — status da sessão atual
- `DELETE /api/v1/servicenow/session` — limpar sessão
- `POST /api/v1/servicenow/session/test` — testar autenticação
- `GET/PUT /api/v1/servicenow/browser-config` — ler/gravar `BrowserConfig`

**Compatibilidade de frontend**: `RodExtractor.GetStatus()` retorna campos `playwright_configured`/`script_exists` como `true` para não quebrar o frontend legado (que esperava Playwright).

### Teams Mr.ViaBot + SRE Approval (branch `integracao-teams`)

**`internal/teams/`** — extrai CHGs de aprovação SRE das mensagens do Mr.ViaBot no Microsoft Teams via automação de browser (go-rod). O acesso HTTP direto ao `chatsvcagg` é bloqueado pelo MCAS (Microsoft Cloud App Security) — a extração ocorre inteiramente via DOM JS e IndexedDB do browser.

**Dois mecanismos de extração** (aplicados em ordem):
1. **DOM**: seletores CSS em `[data-tid="messageBody"]` e similares. Fallback: percorre leaf nodes com regex `CHG\d{5,}`, sobe a árvore DOM até achar container com `sre-approval` (max 15 ancestors), deduplica por substring.
2. **IndexedDB**: varre `conversation-manager:react-web-client`, `chat-info-pane-manager` e `skypexspaces` — busca keywords (`chg0`, `sre-approval`, `viabot`) e thread IDs do formato `19:...@thread.v2`.

**SkypeToken**: capturado do CDP Network (`X-Skypetoken` ou `authorization: skype_token`) antes do body da resposta. Fallback: `localStorage`/`sessionStorage` após carga do Teams. Necessário apenas para o endpoint HTTP de fallback (que falha com MCAS mesmo com token).

**Sessões separadas**:
- `~/.k8s-hpa-manager/teams-session/` — perfil Chrome para Teams (go-rod). **Nunca misturar com `rod-session`** do ServiceNow — perfis Chrome incompatíveis corrompem um ao outro.
- `~/.k8s-hpa-manager/teams-cache/approvals-cache.json` — cache de CHGs em disco. Persiste 48h por merge; `needs_refresh` na resposta JSON é apenas indicativo (não oculta dados).

**Refresh é síncrono e lento** (`POST /api/v1/teams/approvals/refresh`): abre o Chrome, navega para `teams.microsoft.com/v2/`, aguarda carregamento (~2min max), navega para o chat do Mr.ViaBot via hash SPA `#/conversations/<threadID>`, extrai o DOM e fecha. Pode levar **~90s**. O handler bloqueia e retorna `409 Conflict` se já houver extração em andamento (`h.refreshing`).

**Navegação Teams v2 + MCAS**: o redirect `teams.microsoft.com → teams.microsoft.com.mcas.ms` é automático. O `RunDiscovery` monitora novas abas (`browser.Pages()` a cada 3s) e anexa listeners CDP a cada aba com URL do Teams — necessário porque o v2 pode abrir em aba separada.

**Thread ID do Mr.ViaBot** é hardcoded em `discover.go` e `extractor.go`: `19:eab1be93-5589-4a3f-9f47-d6cfcbc50a0c_61740f97-9be2-4459-b054-5230364585a7@unq.gbl.spaces`. Se o bot mudar de conta, atualizar ambos os arquivos.

**`internal/sreapproval/`** — aprovação de deployments em `https://devstartcd.via.com.br`. Fluxo CSRF-aware: GET página → `cookiejar` mantém sessão → extrai campos `<input type="hidden">` e `<form action>` → POST com `email`. Detecta `já foi finalizada` no HTML e retorna `*ErrAlreadyFinalized{ApproverEmail, ApproverSquad}` — o handler retorna `200 OK` com `already_finalized: true` (não erro HTTP).

**Endpoints Teams**:
- `GET /api/v1/teams/approvals/today` — CHGs do dia (filtro por `ExtractedAt.YearDay`)
- `GET /api/v1/teams/approvals/search?chg=CHG0455046` — busca no cache 48h (resposta em ms)
- `POST /api/v1/teams/approvals/refresh` — extração completa (~90s, bloqueante)

**Endpoints SRE Approval**:
- `GET /api/v1/sre-approval/info?url=...` — scraping HTML da página de aprovação
- `POST /api/v1/sre-approval/approve` — submete aprovação (**requer `RequireSREGroup()`**)
- `GET /api/v1/sre-approval/extract-id?url=...` — extrai ID da URL
- `GET /api/v1/sre-approval/current-user` — email via `az account show`

**`SreApprovalButton.tsx`**: botão inline no header do Health Check SRE. Chama `getSreApprovalInfo()` automaticamente no mount (sem click). Exibe email do aprovador quando `finalized`. Obtém email do usuário logado via `/sre-approval/current-user` antes de aprovar.

**`ServiceNowImportModal.tsx`**: modal com 3 abas — **"Teams (Mr.ViaBot)"** (padrão), "Playwright/Rod" e "Manual". A aba Teams carrega `getTeamsApprovalsToday()` na abertura e permite selecionar CHGs para extração em lote via ServiceNow.

**`internal/teams/testdata/`** está no `.gitignore` — contém tokens de sessão capturados durante debug.

### Access Checker — Verificar Acesso (`AccessCheckTab.tsx`)

Ferramenta no Tools menu que checa se um analista (e-mail) tem acesso a um cluster/namespace via impersonation nativa do K8s (`rest.ImpersonationConfig`), sem depender de `kubectl` no servidor. Ver `ACCESS-CHECK-PLAN.md` para o histórico completo de decisões e comandos `az`/`kubectl` de validação.

**Backend** (`internal/web/handlers/access_check.go`, `AccessCheckHandler.GetRules`/`CanI`, `GET /api/v1/access-check/rules|can-i` atrás de `RequireSREGroup()`): monta `rest.Config` impersonado via `kubeManager.GetRestConfig` + grupos AAD resolvidos por `internal/rbac/aad_group_lookup.go` (`AADGroupLookup.GetAllGroups`/`ResolveVVCloudGroups`) via `az ad user get-member-groups --id <email>` (uma única chamada, sem Graph API; cache 10min), filtrando por prefixo `VV_CLOUD` (sem separador — cobre `_` e `-`, ex: `VV_CLOUD-ADM`) para os GUIDs usados em `--as-group`. Erro `Forbidden ... impersonate` mapeado para `IMPERSONATION_NOT_ALLOWED`. Toda consulta logada no `HistoryTracker` (`action: "access_check"`).

**Limitação estrutural** (`internal/web/handlers/access_check_iam.go`): acesso concedido via **IAM do Azure** no recurso AKS (Role Assignments, ex: "Azure Kubernetes Service Cluster Admin Role") é invisível a qualquer checagem via impersonation/`SelfSubjectRulesReview` — essa role permite buscar o kubeconfig ADMIN (`system:masters`, bypass total de RBAC), decidido pelo Azure Resource Manager antes de qualquer request chegar no `kube-apiserver` (mesma limitação existe no `kubectl auth can-i --as` nativo). `getAKSResourceRoleAssignments()` consulta `az role assignment list --scope <resource-id-do-aks>` (cache 45min) e `findIAMAdminBypass()` cruza com os grupos resolvidos; campo `iamAdminAccess` + banner vermelho sempre visível no `AccessCheckTab.tsx` quando detectado. Só implementado para AKS.

**Scan de frota** (`GET /api/v1/access-check/scan-fleet?email=&namespace=`, `access_check_scan.go`): varre todos os clusters AKS em paralelo (semáforo 8, timeout 45s/cluster, 150s total). Sem `namespace` informado, varre todos os namespaces não-sistema do cluster antes do RBAC real. Usa `SelfSubjectAccessReview` (não `SelfSubjectRulesReview`, que pode devolver regras incompletas para RBAC complexo) testando em ordem até o primeiro "Allowed": `list pods` → `list deployments.apps` → `list secrets` → `list configmaps`. Aba "Todos os Clusters" separa 3 blocos: acesso real do analista (IAM + RBAC), clusters não verificados (erro do servidor) e clusters sem acesso.

O scan de frota já resolvia internamente a lista completa de grupos AAD (`allGroupDTOs`, necessária para montar a impersonation por cluster), mas descartava esse resultado antes de responder ao frontend — só devolvia `matchedGroups` (`VV_CLOUD_*`). A resposta agora expõe `allGroups` também, sem nenhuma chamada `az` adicional (`GetAllGroups` já é cacheado por 10min e reaproveitado). `AccessCheckTab.tsx` usa `fleetResult` como fallback (atrás de `rulesResult`/`canIResult`) para popular a aba "Todos os Grupos AAD (N)" mesmo quando o usuário só rodou "Verificar em todos os clusters" (sem antes rodar a Verificação Pontual).

**Frontend**: 3 abas manuais (nunca shadcn `<Tabs>`) — "Visão Geral" (veredito SIM/NÃO por categoria de recurso), "Verificação Pontual" (frase explícita "SIM/NÃO — `email` PODE/NÃO PODE executar `verbo recurso`" + motivo do RBAC) e "Todos os Grupos AAD (N)". `ClusterSelectorForTab.tsx` usa `Popover`+`Command`/`CommandInput` (não `<Select>` — Radix fecha o popover ao focar input de busca externo).

**Bug de referência ao adicionar campos novos**: slice `nil` em Go vira `null` no JSON (não `[]`), e checks `campo !== undefined` no frontend não cobrem `null`. Frontend deve usar `campo && (...)` (truthy cobre ambos); backend nunca deve inicializar slices de resposta com `var s []T` — sempre `[]T{}`.

⚠️ Última revisão (Revisão 7, ver `ACCESS-CHECK-PLAN.md`) não foi validada contra clusters/analistas reais — validar antes de confiar em produção.

### Resync AKV (Secrets)

Botão **Resync AKV** na aba Secrets — exibido no painel direito logo após "Criar Secret", apenas quando o Secret selecionado tem `"akv"` no nome (case-insensitive; casa com o padrão gerado pelo `external-secrets` para AKV, ex: `akv-<namespace>`). Dispara `POST /api/v1/secrets/:cluster/:namespace/resync-akv` (`SecretHandler.ResyncAKV` em `secrets.go`), que executa `kubectl annotate externalsecret sre-tools-external-secrets-<namespace> force-sync=<unix-ts> -n <namespace> --context <cluster> --overwrite` — o nome do ExternalSecret é fixo por convenção do SRE Tools e resolvido a partir do namespace já selecionado. Protegido por `rbacMiddleware.RequireSREGroup()` no backend e `ProtectedAction allowed={canWriteSecrets}` no frontend. `ResyncAkvModal.tsx` dispara a chamada automaticamente ao abrir, exibe status + comando exato + saída do kubectl, com botão "Executar novamente"; operação registrada no `HistoryTracker` (`action: "resync_akv"`).

`ResourceCompareModal.tsx` (Edição Lado a Lado) também suporta o tipo `"gateway"` em `ResourceType` — reaproveita `apiClient.getGateway/getGateways/diffGateway/validateGateway/applyGateway`, fixo no kind `"gateway"` (não cobre HTTPRoute/GRPCRoute/TCPRoute/GatewayClass). `GatewayTab.tsx` tem o botão "Abrir em Edição Lado a Lado" (`SplitSquareHorizontal`) no painel direito quando `selectedGateway.kind` (case-insensitive) é `"gateway"`.

### Secrets — Botão "Atualizar Certificado" (TLS expirando/expirado)

Botão **Atualizar Certificado** na aba Secrets, ao lado de "Ver Certificado TLS" no painel de detalhes — só aparece quando o Secret selecionado é `kubernetes.io/tls` **e** está com status `expiring`/`expired` no `tlsCertMap` (`SecretsTab.tsx`, mapa já existente populado por `refreshTlsCertMap()` — scan da aba via `useCertificates().scanCertificates({clusters:[cluster]})` ao montar e a cada 1h; a própria presença no mapa já é a condição de exibição do botão, sem chamada extra). Cor do botão: vermelho se `expired`, âmbar se `expiring` — mesmas cores do badge da lista.

Abre `CertificateRenewModal.tsx` (novo componente, distinto do modal de Upload/Atualização em Massa de `CertificatesTab.tsx`, que permanece intocado): modal enxuto travado no cluster/namespace/nome do secret já selecionado (sem scan, sem seletor de múltiplos clusters — não fazem sentido nesse contexto). Switch **Manual/AWX** (mesmo padrão do modal de Upload) visível só quando o AWX está configurado e acessível:
- **Manual**: dois textareas (`tls.crt`/`tls.key` PEM) que chamam a **mesma** função já usada pela aba Certificados, `useCertificates().uploadCertificate()` → `POST /api/v1/certificates/upload` → `Scanner.UploadCertificate` (update-or-create do Secret TLS)
- **AWX**: reaproveita o `AWXCertForm` já existente (mesmo componente do modal de Upload em `CertificatesTab.tsx`), travado no cluster/namespace do secret selecionado — sem duplicar lógica de survey/launch/stream de logs

Reaproveita `getStatusBadge` de `CertificateDetailModal.tsx` (exportada para esse fim) para mostrar status/dias restantes no header do modal. Se `CertificateInfo.certManager` estiver presente (secret gerenciado por cert-manager), exibe aviso âmbar de que a atualização manual pode ser sobrescrita na próxima reconciliação — **não bloqueia** o envio.

**RBAC**: botão envolto em `<ProtectedAction>` **sem** `allowed` (grupo SRE), não `allowed={canWriteSecrets}` como os outros botões de escrita da aba — porque a rota reaproveitada `/certificates/upload` é protegida no backend por `rbacMiddleware.RequireSREGroup()` (RBAC de grupo AD), não por RBAC K8s. Mesmo padrão do botão "Atualizar" equivalente em `CertificatesTab.tsx`.

Ao concluir (`onSuccess`): `handleSelectSecret(selectedSecret)` recarrega o YAML no editor com o novo `tls.crt`/`tls.key`, `refreshTlsCertMap()` atualiza/remove o badge sem esperar o próximo ciclo de 1h, `silentRefetch()` (de `useSecrets`) atualiza a lista. **Limitação herdada**: `Scanner.UploadCertificate` não grava no `HistoryTracker` — mesmo comportamento do fluxo de Upload/Batch já existente na aba Certificados, não foi alterado por escopo.

### ConfigMaps — Badge de Uso (Órfão / Usado por)

Deploys frequentes (Helm/Kustomize com hash no nome) deixam ConfigMaps antigos sem nenhum Pod referenciando. `GET /api/v1/configmaps/usage?cluster=X&namespace=Y` (`ConfigMapHandler.Usage` em `internal/web/handlers/configmaps_usage.go`; `namespace` vazio = todos os namespaces do cluster) faz cross-reference: lista ConfigMaps + Pods do escopo (só essas 2 chamadas, sem custo extra por item) e varre `Volumes[].ConfigMap`/`.Projected.Sources`, `EnvFrom[].ConfigMapRef` e `Env[].ValueFrom.ConfigMapKeyRef` de `InitContainers`+`Containers`. Cada Pod referenciador é resolvido até o **workload dono de verdade** via `OwnerReferences` (`resolveOwnerDisplayName`) — `ReplicaSet` vira `Deployment/<nome>` (`stripReplicaSetHash` remove o sufixo de hash do ReplicaSet, convenção do controller de Deployment), `DaemonSet`/`StatefulSet`/`Job` são donos diretos do Pod. Isso é mais preciso que o cross-referencer que já existia em `internal/healthcheck/config_crossref_checker.go` (que só resolve até o Pod, não o workload, e está morto — não é chamado por nenhum handler/orchestrator hoje); optou-se por escrever uma função nova e independente em vez de estender aquele código já testado/usado alhures.

**Frontend**: badge vermelho "Órfão" ou verde "N app(s)" (tooltip com os workloads via atributo `title`) tanto na `ConfigMapMonitorTable` (nova coluna "USO") quanto no card de detalhes ao abrir um ConfigMap específico (`ConfigMapsTab.tsx`). Hook `useConfigMapUsage` (`hooks/useAPI.ts`) segue a mesma convenção manual (state + polling de 60s) do `useConfigMaps` já existente — este projeto não usa React Query para essas listas.

### Deployments — Status de pod individual refletido na listagem (`unhealthyPodCount`/`podIssueReason`)

**Bug real corrigido**: a listagem de Deployments (`GET /api/deployments` → `ListDeployments`/`buildDeploymentSummary` em `internal/kubernetes/client.go`) usava só os campos agregados de `dep.Status` (`ReadyReplicas`/`AvailableReplicas`/etc., populados pelo controller do K8s). Um Deployment com 5 réplicas e 1 pod em `CrashLoopBackOff` podia ter `ReadyReplicas == 5` no instante exato da consulta — o K8s marca o pod Ready de novo rapidamente entre restarts —, então esse "flapping" nunca aparecia na lista (diferente do k9s, que reflete o estado do pod individual). Corrigido com o mesmo padrão do badge de uso de ConfigMaps acima: `ListDeployments` faz **1 List de Pods por namespace** (cacheado, não é N+1 por deployment) e agrega por Deployment via owner chain Pod→ReplicaSet (`deploymentNameForPod`/`stripReplicaSetHashSuffix`, cópia local da mesma lógica de `resolveOwnerDisplayName`/`stripReplicaSetHash` — client.go não pode importar o pacote `handlers`). `podIssueReason(pod)` classifica o problema (`CrashLoopBackOff`/`ImagePullBackOff`/`ErrImagePull`/`Error` do container Waiting, `Pending`/`Failed`/`Unknown` da fase do pod, `Terminating` via `DeletionTimestamp`, ou "N container(s) not ready"), populando `DeploymentSummary.UnhealthyPodCount`/`PodIssueReason`.

**Frontend**: `isDeploymentHealthy()` (`DeploymentMonitorTable.tsx`) e `isDeploymentProblematic`/`getDeploymentSeverity`/`getDeploymentStatusInfo` (`DeploymentsTab.tsx`) passam a considerar `unhealthyPodCount` além dos campos agregados — cobre tanto a cor da linha/card quanto o filtro "Saudável/Degradado". Reasons de crash "duro" (`CrashLoopBackOff`/`Error`/`ImagePullBackOff`/`ErrImagePull`/`Failed`) contam como severidade `error` (vermelho); os demais (`Pending`, containers not-ready) como `degraded` (amarelo). Ícone de alerta com tooltip na coluna READY da `DeploymentMonitorTable`.

### Notas (anotações Markdown por cluster+aba)

Botão **Notas** dentro da barra `<TabNavigation>` em `Index.tsx`, inline ao lado do botão "Explorer" (não no `Header.tsx`, e não no `ToolsMenu` — decisão deliberada: todo item do `ToolsMenu` troca `activeTab`, mas Notas precisa abrir um modal por cima do que está sendo visto, sem sair da aba atual). Abre `NotesModal.tsx`, escopado por `cluster={selectedCluster}` + `tab={activeTab}` — ambos já no escopo local de `Index.tsx`, então nenhuma prop nova precisou ser plumbada por outro componente.

**Modelo de dados — diário, não documento único**: cada "Salvar" faz `INSERT`, nunca `UPDATE` — o histórico de anotações daquele cluster+aba fica visível como uma lista, mais recente primeiro (`NotesStore.List`, `ORDER BY created_at DESC`). "Tema" é implícito: é a própria aba (`activeTab`) onde a nota foi criada, sem campo livre extra.

**Backend** (`internal/storage/notes_store.go` + `internal/web/handlers/notes.go`): store SQLite standalone (`~/.k8s-hpa-manager/notes.db`, WAL, mesmo padrão de `snat_history_store.go` — não usa o `SQLiteClient` genérico). Rotas `GET/POST /api/v1/notes` e `PUT/DELETE /api/v1/notes/:id`, atrás só de `InjectUserEmail()` (sem `RequireSREGroup()` — não é mutação destrutiva de cluster). `Update`/`Delete` comparam `user_email` da nota com o e-mail do contexto Gin e retornam `403` se não for o autor — a única forma de RBAC aqui é "autoria", não grupo AD nem RBAC K8s.

**Editor — Markdown + toolbar, sem dependência nova**: `MarkdownToolbar.tsx` opera sobre `<textarea>` puro via `selectionStart`/`selectionEnd` (não Monaco), envolvendo a seleção atual com sintaxe (`**negrito**`, `*itálico*`, listas, link, código, citação) e reposicionando o cursor via `requestAnimationFrame` após o re-render (textarea controlado). Preview usa `react-markdown`+`remark-gfm`, já instalados no projeto (mesmo padrão de `TeamsBroadcastTab.tsx`/`AIAnalysisCard.tsx`, classe `prose prose-sm dark:prose-invert max-w-none` do `@tailwindcss/typography`).

**Modal** (`NotesModal.tsx`): `DialogContent` com **altura fixa** `h-[85vh]` (não `max-h-*`) + `flex flex-col overflow-hidden` — segue a regra já documentada nesta página ("Modal Describe do pod sem scroll — lição de `max-height` vs `height`"). Toggle Editor/Preview via `useState` simples, **não** shadcn `<Tabs>` (quebraria o `flex-1 min-h-0` da lista histórica). Botões Editar/Excluir de cada nota só aparecem quando `note.user_email === useUserProfile().user?.email` — o backend já impõe a regra real via 403, isso só evita clique→erro na UI.

**Frontend — hook** (`hooks/useNotes.ts`): React Query, `queryKey: ['notes', cluster, tab]`, `enabled: !!cluster && !!tab` (evita chamar a API sem cluster selecionado). `useCreateNote`/`useUpdateNote`/`useDeleteNote` invalidam essa mesma `queryKey` no `onSuccess`.

**Badge de contagem no botão "Notas"**: `Index.tsx` chama `useNotes(selectedCluster, activeTab)` (mesmo hook/queryKey usado por `NotesModal`, então não duplica requisição quando o modal está aberto) só para ler `.length` e renderizar um `Badge` (mesmo componente/estilo usado no badge da aba "Staging" em `TabNavigation.tsx`) ao lado do texto do botão. Sem isso, o botão era idêntico com ou sem histórico — só dava pra descobrir abrindo o modal.

**Busca cross-cluster/cross-aba** (`GET /api/v1/notes/search?q=&limit=`): como o modelo é diário e escopado por cluster+aba, achar uma nota antiga sem lembrar onde foi criada não era possível — `NotesStore.Search` faz `LIKE` sobre `content` em **todos** os clusters/abas (sem filtro de autor), `ORDER BY created_at DESC LIMIT` (default 30, máx 100). `escapeLike()` escapa `%`/`_`/`\` do termo digitado antes do `LIKE ... ESCAPE '\'` — sem isso, um usuário buscando por `"100%"` casaria também com qualquer nota que não tivesse o `%` (wildcard do SQL interpretado literalmente). Handler exige `q` não-vazio (`400` caso contrário).

Frontend: campo de busca sempre visível no topo do `NotesModal` (`useSearchNotes`, debounce de 400ms client-side via `useEffect`+`setTimeout` — evita 1 request por tecla). A partir de 2 caracteres, a busca substitui a lista escopada por resultados de **todos** os clusters/abas, cada um com badges `cluster`/`tab` (já vêm no próprio `Note` — sem endpoint/campo novo) para dar contexto de onde a nota foi criada. Resultados de busca são **somente leitura** (sem botões Editar/Excluir) — evita a complexidade de invalidar a `queryKey` `['notes', cluster, tab]` correta de uma nota que pode pertencer a um cluster/aba diferente do que está aberto no momento; para editar/excluir, o usuário navega até o cluster/aba real da nota.

**`NoteEntry.tsx` — item colapsável, colapsado por padrão**: componente compartilhado (`Collapsible`/`CollapsibleTrigger`/`CollapsibleContent` de `@/components/ui/collapsible`, mesmo primitivo Radix já usado em `AIHistoryPanel.tsx` para os Filtros Avançados) usado nos 2 lugares que renderizam uma nota dentro do `NotesModal` — lista escopada (aba atual ou lembretes gerais, conforme o toggle abaixo) e resultados de busca. Expandido: markdown completo renderizado + botões Editar/Excluir (quando `isAuthor` e os handlers são passados — resultados de busca não passam handlers, então ficam somente leitura). Existe só esse componente — nenhum dos 2 call sites reimplementa a renderização de nota.

**Cabeçalho = título derivado + data, não autor + data**: a primeira versão mostrava `{user_email} — {data}` na linha colapsada — pouco útil quando o autor já é implícito (o próprio usuário, na maioria dos casos) e não diz nada sobre o conteúdo da nota. `deriveTitle()` usa a 1ª linha não-vazia do conteúdo como título (sem exigir um campo `Title` novo no modelo — continua só Markdown livre), removendo marcadores de sintaxe (`#`/`##`, `-`/`*`/`>`, `1.`) pra não vazar Markdown cru no título exibido; trunca em 70 chars. O e-mail do autor não some — vira `title` HTML (tooltip ao passar o mouse) no cabeçalho, em vez de texto sempre visível.

**Lembretes gerais — toggle de escopo DENTRO do `NotesModal`, não um widget flutuante separado**: a primeira versão (`GeneralNotesWidget.tsx`, removido) era um post-it fixo (`fixed bottom-4 left-4`) fora da `TabNavigation` — funcionava, mas vivia numa superfície de UI própria, desconectada do botão "Notas" que o usuário já associa a anotações. Revertido pra um único ponto de entrada: `NotesModal` ganhou um seletor de escopo (`scope: "tab" | "general"`, dois botões estilo segmented control logo abaixo do `DialogTitle`) que troca o `tab` efetivo usado em todas as queries/mutations entre a aba real (`tab` prop) e `GENERAL_NOTES_TAB = "__general__"` (`hooks/useNotes.ts`, valor reservado que nunca colide com uma aba de verdade — mesmo truque de antes, zero schema novo). As duas contagens (`useNotes(cluster, tab)` e `useNotes(cluster, GENERAL_NOTES_TAB)`) ficam sempre buscadas em paralelo pra popular o badge dos dois botões do seletor simultaneamente, não só do que está ativo — react-query cacheia por `queryKey` então trocar de escopo não gera request nova se já foi buscado há menos de 30s. Botão "Lembretes gerais" mantém a identidade visual âmbar do widget antigo quando ativo (`bg-amber-400`), pra continuidade de reconhecimento visual. Trocar de escopo com um rascunho aberto cancela a composição (`switchScope` chama `cancelCompose`) — evita salvar uma nota no escopo errado por engano.

O botão "Notas" da `TabNavigation` (sempre visível, independente de `activeTab`) agora soma as duas contagens no badge (`notesForCurrentTab.length + generalNotesForBadge.length`) — sinaliza "tem algo pra ver no Notas" sem precisar abrir o modal pra saber se é da aba atual ou geral.

### Kafka Test Tool — Seleção de Pod/Container e OAUTHBEARER (Azure AD)

`internal/web/handlers/kafka_test_tool.go` + `kafka_test_pods.go` + `KafkaTestTab.tsx`. Duas evoluções sobre o Teste de Kafka já documentado acima:

**Seleção de pod/container**: antes, o modo `pod` sempre escolhia o primeiro pod Running do Deployment e seu primeiro container para receber o Ephemeral Container — sem controle do usuário. `resolveRunningPodForDeployment` foi dividido em `listRunningPodsForDeployment` (lista todos) + `resolvePodForDeployment(ctx, clientset, namespace, deployment, requestedPod, requestedContainer)`: sem `requestedPod`, mantém o comportamento antigo (retrocompatível); com `requestedPod`, valida que o pod pertence ao Deployment e está Running, e usa `requestedContainer` (ou o primeiro container, se vazio). Endpoint novo `GET /api/v1/kafka-test/pods?cluster=&namespace=&deployment=` (`kafka_test_pods.go`, sem RBAC — mesmo padrão do `docker-status`) alimenta dois `SearchableSelect` novos no frontend (Pod, depois Container — só exibidos em modo `pod`, cascata de reset igual aos seletores de cluster/namespace/deployment já existentes). `RunKafkaTestRequest`/`ListTopicsRequest` ganharam `pod_name`/`container_name` opcionais. Os handlers de Teste de Banco de Dados (`db_test_tool.go`, `db_test_preview.go`) usam a mesma função renomeada `resolvePodForDeployment` (chamada sem pod/container explícitos — comportamento idêntico ao antigo `resolveRunningPodForDeployment`).

**Mecanismo SASL `OAUTHBEARER` (Azure AD/Event Hub)**: cobre autenticação via service principal (client credentials) além do SAS por connection string já suportado (`PLAIN` com `$ConnectionString`). `KafkaSASLConfig` ganhou `OAuthClientID`/`OAuthClientSecret`/`OAuthTokenEndpointURL`/`OAuthScope`; `buildKcatAuthFlags` monta `-X sasl.mechanisms=OAUTHBEARER -X sasl.oauthbearer.method=oidc -X sasl.oauthbearer.client.id=... -X sasl.oauthbearer.client.secret=... -X sasl.oauthbearer.token.endpoint.url=... -X sasl.oauthbearer.scope=...` (scope opcional) em vez de `sasl.username`/`sasl.password`; `kafkaValidateOAuthBearerFields` exige os 3 campos obrigatórios. Frontend: dropdown de mecanismo ganhou `OAUTHBEARER (Azure AD)` (seleção liga TLS automaticamente); os campos Usuário/Senha somem e dão lugar a 4 campos (Client ID, Client Secret, Token Endpoint URL, Scope) com placeholders no padrão `https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token` / `https://<namespace>.servicebus.windows.net/.default`.

**Troca de imagem obrigatória**: `kafkaTestPodImage` mudou de `edenhill/kcat:1.7.1` (librdkafka 1.8.2, sem o método `oidc` do OAUTHBEARER — confirmado via `docker run edenhill/kcat:1.7.1 -X sasl.oauthbearer.method=oidc ...` → `No such configuration property`) para `ueisele/kcat:1.7.1-librdkafka2.1.1` (mesma versão do kcat CLI, rebuild de terceiro com librdkafka 2.1.1, que aceita a config — confirmado empiricamente). Usada tanto no modo `pod` (Ephemeral Container) quanto `local` (Docker); PLAIN/SCRAM continuam funcionando sem mudança (retrocompatíveis). **Limitação conhecida**: `buildKafkaClientPropertiesFile` (usado só pela feature de tamanho em disco do modo `local`, via `kafka-log-dirs`) não suporta OAUTHBEARER — fora de escopo, não bloqueia o teste principal.

**Fora de escopo**: Managed Identity (sem client secret, via IMDS) — só client credentials por ora, já que o teste roda via Ephemeral Container ou Docker local, sem identidade gerenciada do Azure disponível nesses contextos; build de imagem própria (Dockerfile custom) não foi feito — usa-se a imagem `ueisele/kcat` pronta.

### Certificates

`internal/certificates/` + `internal/web/handlers/certificates.go`: discovery de certs TLS em secrets K8s, validação de expiração, import/export. Usar para qualquer operação envolvendo TLS no cluster.

---

## RBAC Azure AD

- **Grupo SRE**: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
- Backend: `internal/rbac/azure_ad.go` + middleware em `internal/web/middleware/rbac.go` (RBAC de grupo). Auth de request: `internal/web/middleware/auth.go` (`JWTAuthMiddleware`)
- Frontend: hook `useUserPermissions()` + componente `<ProtectedAction>` para proteger botões
- Cache: TTL de 1 hora para permissões
- Rotas destrutivas (POST/PUT/DELETE) protegidas automaticamente pelo middleware
- **`OptionalSRECheck` sempre retorna `isSRE=true`** — verificação de grupo AD desabilitada em `rbac.go` (linha 145-148). Todos os usuários autenticados têm acesso SRE. Não remover esse comportamento sem alinhamento explícito.

---

## RBAC K8s via SelfSubjectRulesReview

Camada adicional ao RBAC Azure AD (branch `correcao-jwt`): permissões reais do cluster por namespace, obtidas via `SelfSubjectRulesReview` da API do K8s. Independente de grupos AD — reflete exatamente o que o kubeconfig do servidor permite.

**Backend** (`internal/kubernetes/permissions.go` + `internal/web/handlers/k8s_permissions.go`):
- `NamespacePermissions` — struct com campos `canListHPA`, `canUpdateHPA`, `canExecPods`, `canWriteSecrets`, etc.
- `K8sPermissionsHandler` — cache em memória com TTL de **5 minutos** por chave `cluster/namespace`
- Endpoint: `GET /api/v1/k8s-permissions?cluster=<c>&namespace=<ns>`
- Campo `Incomplete: true` quando o cluster usa wildcard policies complexas — nesse caso assume acesso total para não bloquear

**Frontend** (`internal/hooks/useK8sPermissions.ts`):
- `useK8sPermissions(cluster, namespace)` — React Query com `staleTime: 5min`, retry 1, sem refetch no foco
- Fallback conservador: leitura permitida, escrita bloqueada (enquanto carrega ou cluster indefinido)
- Retorna `{ permissions, canWrite }` onde `canWrite = permissions.canUpdateHPA`

**`ProtectedAction` atualizado** — nova prop `allowed?: boolean`:
```tsx
// Sem prop: verifica grupo AD (isSRE) — comportamento original
<ProtectedAction><Button>Escalar HPA</Button></ProtectedAction>

// Com prop: usa permissão K8s real — ignora grupo AD
const { permissions } = useK8sPermissions(cluster, namespace);
<ProtectedAction allowed={permissions.canUpdateHPA}>
  <Button>Escalar HPA</Button>
</ProtectedAction>
```

**Quando usar qual verificação:**
- `ProtectedAction` sem `allowed` → operações que requerem pertencer ao grupo SRE (ex: aprovar CHGs, operações de node pool)
- `ProtectedAction allowed={...}` → operações que refletem o RBAC do cluster (ex: editar HPA, secret, deployment — depende do que o kubeconfig permite)

---

## Troubleshooting Rápido

Os problemas mais críticos/surpreendentes:

| Problema | Solução |
|----------|---------|
| Frontend não atualiza após mudança | `./rebuild-web.sh -b` + Ctrl+Shift+R no browser |
| Mudanças no backend não tomam efeito | Servidor não reiniciado — `make build` só gera o binário; matar e reiniciar (`kill <PID> && ./build/new-k8s-hpa web -f`) |
| Servidor desliga sozinho após ~40min | Auto-shutdown por inatividade — reabrir a aba reinicia o heartbeat (browsers throttleiam `setInterval` em abas de fundo) |
| Build falha sem versão | `git fetch --tags --prune` |
| Cluster inacessível | VPN desconectada — `kubectl cluster-info --context <name>` |
| JWT: login retorna 501 | `K8S_HPA_JWT_SECRET` não definido — frontend cai para token estático automaticamente |
| JWT: login retorna "AZ_CLI_ERROR" | Azure CLI não autenticado — executar `az login` no servidor |
| JWT: frontend em loop de login | Limpar `localStorage` manualmente |
| Monaco: Ctrl+Shift+D/E sumiu do contexto | `configureMonacoYaml` chamado múltiplas vezes — verificar flag `_yamlConfigured` em `MonacoYamlEditor.tsx` |
| SNAT widget não aparece na aba Node Pools | Widget fica em `Index.tsx` case `"nodepools"` — `NodePoolTab.tsx` é componente **órfão** (nunca importado) |
| Arquivo em `pages/` não tem efeito | Vários arquivos em `src/pages/` são **mortos**: `Index.backup.tsx`, `Index.broken.tsx`, `Index.tsx.broken`, `SimpleIndex.tsx`, `MinimalIndex.tsx`, `TestIndex.tsx` — nunca importados. Editar apenas `Index.tsx` |
| Tab de modal não preenche a altura | shadcn `<Tabs>` usa `display:block` que quebra `flex-1 min-h-0` — usar implementação manual (ver `PodQuickViewModal.tsx`) |
| GKE: workloads não carregam | `GetFreshGKEToken()` sem credenciais — verificar `~/.k8s-hpa-manager/gcp-adc.json` ou autenticar via AutoDiscover |
| K8s RBAC: botão disabled mesmo sendo SRE | `useK8sPermissions` ainda carregando ou RBAC real do cluster prevalece — verificar se `allowed` prop está sendo passada |
| Teams: refresh retorna 409 Conflict | Extração já em andamento — aguardar ~90s ou reiniciar o servidor |
| Code Editor: push rejeitado com "non-fast-forward" | Pull --rebase automático implementado — se o rebase falhar (conflito), push retorna erro com mensagem de conflito |

> Troubleshooting completo (Code Editor, Dynatrace, ServiceNow, Teams, LSP, FinOps, SNAT, etc.) → [docs/guides/TROUBLESHOOTING.md](docs/guides/TROUBLESHOOTING.md)

---

## Fluxo de Desenvolvimento

### Backend (TUI ou API)
```bash
# Editar → testar → build
go test -v ./internal/... -race
make build
./build/new-k8s-hpa web -f  # testar
```

### Frontend (React)
```bash
# Dev com hot reload
cd internal/web/frontend && npm run dev  # porta 5173
# Em outro terminal:
./build/new-k8s-hpa web -f              # API na porta 8080

# Build para produção
./rebuild-web.sh -b  # + Ctrl+Shift+R no navegador
```

### Release
```bash
# Merge branch → main
git checkout main && git merge --no-ff <branch> && git push origin main

# Tag e push
git tag v1.3.X && git push origin v1.3.X

# Build multi-plataforma
make release   # gera: build/release/new-k8s-hpa-linux-amd64, darwin-amd64, darwin-arm64

# Criar release no GitHub (com upload de binários)
gh release create v1.3.X \
  build/release/new-k8s-hpa-linux-amd64 \
  build/release/new-k8s-hpa-darwin-amd64 \
  build/release/new-k8s-hpa-darwin-arm64 \
  --title "v1.3.X" \
  --notes "Descrição das mudanças"
```

> `create-v1-release.sh` era específico para v1.0.0 — **não usar** para releases correntes.

### CI (GitHub Actions)

`.github/workflows/ci.yml` roda automaticamente em todo push/PR para `main`/`master`: `go mod download` + `go mod verify` → `make test` (com `SKIP_AZURE_TESTS=1`) → `make build` → `make version` → `make release` (cross-compilation) → upload do binário linux como artifact (retenção 7 dias). Não publica release no GitHub — isso continua manual via `gh release create` (seção acima).

**`.github/workflows/release.yml`** existe como alternativa opcional ao `gh release create` manual acima — mas é **só `workflow_dispatch`** (Actions → Release → Run workflow, escolhendo a tag `vX.Y.Z` desejada em "Use workflow from"), **não** dispara mais automaticamente em `git push origin v*` (evita a corrida/duplicação com o fluxo manual documentado acima). Corrigido para Go 1.25 e convenção de nomes atual (`new-k8s-hpa-*`, `Paulo-Ribeiro-Log/New-K8S-HPA-Manager`) — antes usava Go 1.23 e nomes antigos (`k8s-hpa-manager-*`, `Scale_HPA`).
