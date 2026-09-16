# Plano: Melhorias do FinOps (auditoria de gaps, falhas e riscos)

**Status**: 🟡 em execução — Fase -1 (crítico, fora do escopo original) e Fase 0 mescladas na
`main`, junto com a Fase 1. Fase 2 concluída (PR #434, aguardando merge). Fase 3 concluída (F3.1
implementada; F3.2 avaliada e conscientemente não implementada por falta de recurso real pra
validar — ver seção da fase). Fase 4 concluída (F4.1 singleflight + F4.2 RBAC, PR seguinte). Fase
5 pendente.
**Escopo**: o módulo FinOps inteiro — as 8 abas (Dashboard, Node Pools, Workloads, HPA Histórico,
Armazenamento, Oportunidades, Relatório, Rightsizing), backend (`internal/finops/`,
`internal/web/handlers/finops*.go`, `internal/storage/finops_rightsizing_store.go`) e frontend
(`FinOpsTab.tsx`, `RightsizingTab.tsx`, `DataResourcesPanel.tsx`).

**Origem**: pedido explícito do usuário ("analise essa ferramenta e encontre gaps, falhas e
melhorias significativas"), motivado por uma confusão real ao ler o card de sugestão de tier de VM
do node pool "ingress" (nginx-ingress-controller/velero/istio-ingressgateway) — a ferramenta
recomendou reduzir de `Standard_F4s_v2` pra `Standard_F2s_v2` baseada só em CPU 0%/Mem 3-4%, sem
nenhum aviso de que esse pool carrega workloads de infraestrutura crítica (burst-sensitive), não
apps de negócio comuns.

**Método**: 3 auditorias paralelas e independentes, cada uma lendo o código REAL atual (não
memória de sessões anteriores) — (A) lógica de métricas/pricing no backend, (B) handlers/RBAC/
multi-cloud/caching, (C) frontend/UX. Achados cruzados e consolidados abaixo, com arquivo:linha
de evidência em cada item. Itens marcados **[FINE]** nas auditorias (nada a corrigir — inclusos
aqui só pra registrar que foram checados e não são gap) não viraram itens de checklist.

---

## Achados descartados (checados, sem gap real)

- Sugestão de tier de VM (`SuggestVMTier`, `internal/finops/vm_tiers.go`) já é genuinamente
  multi-cloud (Azure/GCP/AWS) ponta a ponta — contradiz a suposição de "Azure-only" que ainda
  aparecia no plano original desta feature.
- `GetVMAlternatives` e o pricing de PVC (Persistent Disk/EBS) via `StorageCalculator` também já
  usam `pricerForCluster`/`DetectCloudProvider` corretamente — não hardcoded pra Azure.
- Duplicação de `SafetyMargin` (1.20) caiu pra só 1 cópia real hoje
  (`internal/healthcheck/resource_enricher.go:23`) — não são mais "5 lugares" como documentação
  antiga sugeria.
- Padrão de erro (200 + `available:false` + `reason` pra falha esperada vs. HTTP 4xx/5xx real só
  pra erro de configuração) é consistente e deliberado em `finops.go`/`finops_rightsizing.go`/
  `finops_data_resources.go` — nenhum endpoint devolve erro opaco pra uma falha transiente
  esperada.
- Nenhum endpoint FinOps está sem timeout/deadline — todos herdam `c.Request.Context()`.
- `RightsizingTab.tsx` já trata `isLoading`/`error` explicitamente nas queries principais
  (relatório + scan) — sem tela em branco nesses dois casos.
- Layout do Rightsizing não tem largura fixa em pixel que quebre em viewport estreito — usa
  breakpoints Tailwind relativos.

---

## Checklist de execução

### Fase -1 — CRÍTICO: métricas Dynatrace/Prometheus contaminadas (fora do escopo original, achado ao vivo) ✅

**Concluída** (commit `8e214ac2`, PR #431, baseado em cima da Fase 0/#430). Não fazia parte da
auditoria original — achado investigando um relato do usuário com números impossíveis num node
pool real ("CPU: 626%, Mem: 519%"). Dois bugs INDEPENDENTES, ambos confirmados ao vivo contra a
infra real da empresa:

1. **Dynatrace**: as queries de métrica de workload nunca tinham `k8s.cluster.name` no `splitBy` —
   o tenant DT desta empresa é compartilhado entre TODA a frota (17 clusters monitorados,
   confirmado ao vivo via `/entities`), então qualquer workload com nome comum entre clusters
   (ingress-controller, istiod, cert-manager, velero, prometheus, kyverno, calico) tinha sua
   métrica agregada através de TODOS os clusters que compartilham esse nome — mesmo quando o
   cluster analisado não tem nenhuma cobertura DT real. Corrigido com
   `filter(and(eq("k8s.cluster.name","<cluster>")))`.
2. **Prometheus**: as queries de P95/avg nunca tinham o wrapper `sum by (namespace, pod)` que as
   queries de MAX já usavam — um container que reinicia (cgroup id novo a cada restart) faz
   `quantile_over_time`/`avg_over_time` somar o valor de CADA reinicialização como se fosse uma
   série independente. Um pod com 24 reinícios em 30d tinha o P95 inflado 24x (31,5GB em vez de
   ~1,5GB, confirmado ao vivo). Corrigido replicando o wrapper já usado nas queries de MAX.

Resultado combinado, validado ao vivo ponta a ponta: pool real foi de CPU 626%/Mem 519%
(matematicamente impossível) pra CPU ~40%/Mem ~36% (plausível).

### Fase 0 — Correções de correção de dados (bugs reais, silenciosos, sem mudança de escopo) ✅

**Concluída** (commit `99271f0b`). Validado ao vivo contra um cluster real: 17 de 49 workloads
(35%) que antes apareciam com o badge verde "Eficiente" (sem nenhum dado de uso por trás) agora
corretamente mostram "Sem Dados de Uso" (F0.2). Achado extra no caminho de F0.1: `EnrichWorkloadsPartial`
sobrescrevia dado do Dynatrace com dado do Prometheus sempre que ambos cobriam o mesmo workload —
corrigido junto (fora do escopo original F0.1, mas no mesmo arquivo/função). F0.3 validado sem
regressão contra um cluster AKS real (path default inalterado); não validado ao vivo contra um
cluster EKS aliased (exigiria reproduzir esse cenário específico, não disponível nesta sessão) —
corrigido por revisão de código + o padrão já comprovado (`pricerForCluster`) sendo replicado.

- [x] **F0.1 — Enriquecimento Prometheus-only não cai pra avg quando P95 arredonda pra 0.**
  `internal/finops/prometheus_enricher.go:270` (`hasUsage := cpuP95 > 0 || memP95 > 0`) e `:304-309`
  só populam `CPURecommendedMillis`/`MemRecommendedMi` quando `cpuP95 > 0`/`memP95 > 0` — sem
  fallback pra `avg`, diferente de `internal/finops/dynatrace_enricher.go:76-84`, que já tem esse
  fallback. Um workload enriquecido só via Prometheus (sem dado DT) com uso baixo-mas-real (avg
  não-zero, P95 arredondando pra 0) fica sem NENHUMA recomendação e mantém o verdict default
  `"ok"`. **Ação**: espelhar em `prometheus_enricher.go` o mesmo fallback P95→avg que já existe em
  `dynatrace_enricher.go`. Cobre também `EnrichWorkloadsPartial` (mesma função por baixo).
  **Teste**: caso com `cpuP95=0, cpuAvg>0` deve popular `CPURecommendedMillis` a partir do avg,
  igual ao teste equivalente já existente pro enricher Dynatrace.

- [x] **F0.2 — Verdict default `"ok"` é indistinguível de "verificado eficiente".**
  `internal/finops/calculator.go:840-856` (`determineVerdict`) retorna `"ok"` sempre que não há
  HPA ou `HPAMin==HPAMax`, ANTES de qualquer enriquecimento rodar — e os dois enrichers só
  sobrescrevem `wl.Verdict` dentro do próprio gate de "tem uso" (`prometheus_enricher.go:318`,
  `dynatrace_enricher.go:96`). Um workload sem NENHUM dado (Prometheus e DT indisponíveis durante
  o scan — já aconteceu ao vivo nesta sessão) fica com `"ok"` pra sempre, indistinguível de
  "genuinamente eficiente". `wl.MetricsSource == ""` é o único sinal disso, mas nada força o
  consumidor a checar. **Ação**: introduzir um verdict explícito (`"sem_dados"` ou similar) e
  aplicá-lo quando `MetricsSource == ""` E o workload não tem HPA-based verdict — tanto no backend
  (pra não contar como "eficiente" nas agregações do Dashboard/Relatório) quanto no frontend
  (badge cinza "sem dados de uso", não verde "OK").

- [x] **F0.3 — Pricing de disco OS do EKS quebra silenciosamente pra contexts aliased.**
  `internal/finops/calculator.go:388-441` (`osDiskCostForPool`) despacha por
  `strings.HasPrefix(cluster, "arn:aws:eks:")`/`"gke_"` em vez de `config.DetectCloudProvider` (a
  fonte de verdade já usada em `pricerForCluster`/`calculatePVCCost`). Contexts EKS com alias (ex:
  `cluster-apis-prd`, não o ARN completo — classe de bug já documentada e corrigida noutros
  lugares desta app) caem no path DEFAULT (Azure): lê um label de node Azure-only, e se algo
  incompatível colar, chama `diskPricer.GetDiskPrice("Premium SSD", ...)` — um pricer de Managed
  Disk Azure — contra um node pool AWS. Best case: `ok=false`, custo de disco OS só ausente do
  total. Worst case: número Azure atribuído a um pool AWS, sem aviso. **Ação**: trocar o
  dispatch de `osDiskCostForPool` pra usar `config.DetectCloudProvider`, igual ao resto do pacote.

### Fase 1 — Segurança das recomendações (evitar sugestão perigosa em pool crítico) ✅

Motivado diretamente pelo incidente relatado (pool "ingress" com nginx-ingress-controller/velero/
istio-ingressgateway recebendo sugestão de downsize sem aviso).

**Concluída.** Validado com dados REAIS já persistidos de 8 clusters de produção (nunca contra
dado sintético) — VPN indisponível no momento desta implementação, então em vez de rodar um scan
ao vivo, uma cópia local do `finops-rightsizing.db` real (nunca o arquivo original, cópia num
diretório temporário) foi lida por uma ferramenta de debug (`cmd/_debugcriticalN`, removida
depois). Achado real, direto: `akspriv-entregamais-prd-admin/ingress` tem EXATAMENTE a mesma
combinação do incidente original (`nginx-ingress-controller`, `velero`, `istio-ingressgateway`) —
confirmando que a heurística por nome (F1.1) captura de fato o caso que motivou esta fase. 31
pools reais, em 8 clusters, bateram em pelo menos um padrão de infra conhecida — sem nenhum
falso-positivo óbvio contra nome de app de negócio nos dados reais inspecionados. F1.2 validado
com números reais do mesmo pool: `nginx-ingress-controller` em `.../ingress` pede genuinamente
1920m de CPU — a checagem de capacidade (`MarkInsufficientForLargestWorkload`) usa esse valor real
sem produzir números absurdos. Migração de schema (`has_critical_workload`/
`critical_workload_names`) aplicada sem erro contra a cópia do banco real, e nenhuma linha
pré-existente veio com `has_critical_workload=true` por engano (confirma o default correto da
coluna nova). `go test ./internal/finops/... ./internal/storage/... ./internal/web/handlers/...
-race`, `go build`/`go vet`/`gofmt`, `npx tsc --noEmit -p tsconfig.app.json`/`eslint`/`vite build`
— todos limpos. **Não clicado na UI real** (sem VPN pra levantar uma instância isolada e sem
ferramenta de automação de navegador nesta sessão) — o aviso visual (F1.3) foi revisado por
leitura de código, seguindo o mesmo padrão visual (`AlertTriangle` âmbar) já usado noutras partes
desta app.

- [x] **F1.1 — `SuggestVMTier` não tem nenhum sinal de criticidade do workload.**
  `internal/finops/vm_tiers.go:170-256` — os 3 branches (AKS/GKE/EKS) recebem só
  `cpuUtilPct`/`memUtilPct` AGREGADOS do pool; `isOversized := hasUsage && cpuUtilPct < 30 &&
  memUtilPct < 30` (linha ~183) é exatamente a regra que disparou pro pool "ingress" (CPU 0%, Mem
  3-4%). Nenhum branch olha namespace, nome de componente conhecido (ingress-controller,
  istio-*, velero, cert-manager, external-dns, coredns, etc.) nem replica/pod count. **Ação**:
  o CHAMADOR (`persistRightsizingFromReport`, `internal/web/handlers/finops_rightsizing.go`) já
  sabe quais workloads/namespaces alimentaram cada pool (`WorkloadCount`, a lista de nomes) —
  adicionar uma checagem simples (lista de nomes/prefixos conhecidos de infra crítica, mesmo
  padrão de heurística por nome já usado em outras partes desta app, ex: `isCompanyManagedDeployment`)
  que marca `NodePoolTierSuggestion` com um novo campo `HasCriticalWorkload bool` (+ lista de quais
  bateram) quando qualquer workload do pool corresponder. Não bloquear a sugestão — só marcá-la.

- [x] **F1.2 — Nenhuma checagem de que a SKU sugerida comporta o maior pod individual do pool.**
  Mesma função (`vm_tiers.go`) — a decisão é 100% sobre a MÉDIA/agregado do pool, nunca o pico de
  um único pod. Uma SKU menor sugerida pode ter memória-por-vCPU insuficiente pro maior pod
  individual rodando ali, que simplesmente não conseguiria ser agendado no node menor. **Ação**:
  no mesmo ponto que calcula `cpuUtilPct`/`memUtilPct` agregados
  (`internal/web/handlers/finops_rightsizing.go`, agregação por pool), também calcular o MAIOR
  `CPURequestMillis`/`MemRequestMi` individual entre os workloads do pool, e rejeitar (ou marcar
  como "requer revisão manual") qualquer alternativa cuja capacidade por node fique abaixo desse
  valor + margem.

- [x] **F1.3 — Frontend não exibe nenhum aviso de criticidade no card de sugestão.**
  `RightsizingTab.tsx` (`NodePoolTierCard`, linhas ~743-829, confirmado via leitura completa +
  grep por istio/velero/ingress/namespace — nenhuma checagem encontrada) renderiza a sugestão de
  downsize com a MESMA aparência visual pra qualquer pool, seja ele infra crítica ou app comum.
  **Ação** (depende de F1.1 persistir o sinal no backend primeiro): quando
  `has_critical_workload` vier `true`, renderizar um badge/alerta amarelo explícito no card
  ("Este pool roda componentes de infraestrutura — reveja com cuidado antes de aplicar") ao lado
  da sugestão, sem esconder a sugestão em si (a economia ainda pode ser válida, só precisa de
  mais atenção humana).

### Fase 2 — Discoverability / UX (mesma classe de problema já corrigida uma vez pro RG-data) ✅

**Concluída.** F2.1 validado com uma reprodução em Go da MESMA lógica do badge (soma da melhor
alternativa por pool + contagem de `verdict != "ok"`) contra dados reais já persistidos de 3
clusters — achado um caso real com economia relevante (`akspriv-ofertalogistica-hlg-admin`,
R$8684/mês, badge verde) e dois casos caindo corretamente no fallback de contagem (sem
alternativa persistida com economia > R$10). `useRightsizingReport` extraído pra
`hooks/useRightsizingReport.ts` (convenção já usada no resto do projeto) — reaproveitado tanto
pela aba Rightsizing quanto pelo badge novo, mesma queryKey, sem requisição duplicada quando os
dois estão montados. `npx tsc --noEmit`/`eslint`/`vite build` limpos (mesmo baseline de erros
pré-existentes em `FinOpsTab.tsx`, confirmado via `git stash`). **Não clicado na UI real** — VPN
indisponível na sessão inteira desta fase, sem instância isolada possível.

- [x] **F2.1 — Aba Rightsizing é a única das 8 sem badge de contagem/urgência.**
  `FinOpsTab.tsx:4500-4539` — Node Pools, Workloads, HPA, Armazenamento, Oportunidades e Relatório
  têm todas um `<Badge>` numérico (algumas em vermelho/laranja) no `TabsTrigger`; Rightsizing é só
  texto puro. Combinado com ser a última das 8 abas, não há nenhum sinal visual de que ali existe
  economia em R$. **Ação**: adicionar `<Badge>` na `TabsTrigger` do Rightsizing mostrando a soma
  de `monthly_savings_brl` das alternativas de tier + o número de workloads com `verdict !=
  "ok"` — mesmo padrão visual já usado nas outras 6 abas.

- [x] **F2.2 — `DataResourcesPanel.tsx` não distingue falha transiente de "não aplicável".**
  Linhas 75-87 — o `useQuery` desestrutura só `{ data, isLoading }`, nunca `error`/`isError`, com
  `retry: false`. Uma falha real de rede/Azure cai no MESMO branch (`!data?.available`, linha 107)
  que o caso legítimo "este cluster não tem RG de dados" — texto idêntico, sem botão de tentar de
  novo. **Ação**: capturar `error`/`isError` do `useQuery` e renderizar um estado distinto
  ("falha ao consultar — tentar novamente", com botão que chama `refetch()`) em vez de cair no
  mesmo texto neutro de "não disponível".

- [x] **F2.3 — `last_scanned_at` sem escalonamento visual de idade.**
  `RightsizingTab.tsx:994-998` — mostrado sempre em cinza neutro, "45d atrás" com a mesma
  aparência de "5min atrás". **Ação**: escalar cor/ícone quando a idade passar de um limiar (ex:
  >7 dias → âmbar com aviso "considere reanalisar"; >30 dias → vermelho).

### Fase 3 — Completude de precificação (RG de dados) ✅ (parcial, ver F3.2)

**F3.1 concluída, validada ao vivo (parcialmente — ver nota de escopo abaixo). F3.2 avaliada e
conscientemente NÃO implementada** — nenhuma conta Cosmos DB existe no tenant desta investigação
pra validar contra dado real, e este projeto tem uma disciplina forte de nunca escrever um caminho
de preço sem confirmar sintaxe/unidades ao vivo primeiro (ver o resto deste arquivo/CLAUDE.md).

- [x] **F3.1 — Storage Account nunca é precificado.**
  `internal/finops/data_resources_pricing.go` — `priceStorageAccount` implementada: estimativa
  best-effort a partir do volume REAL de uso (`az monitor metrics list`, métrica `UsedCapacity`,
  média das últimas 48h) × preço de "Data Stored" pra Blob Storage no access tier + redundância
  configurados (`az storage account show --query accessTier` — achado confirmado ao vivo desde a
  investigação inicial: `az resource list` genérico NÃO expõe esse campo). Retail Prices API tem
  preço TIERED por volume (3 faixas: 0-50TB/50-500TB/500TB+, cada uma mais barata) —
  `pickTieredPrice` escolhe a faixa certa pro uso real. `PriceSource="estimated"` (distinto de
  `"api"` usado por VM/disco/Redis/etc.) — frontend mostra "≈" antes do valor + tooltip explicando
  a aproximação, nunca disfarça de preço fixo. **Nunca inclui transações/banda** (não observáveis
  via essa métrica isolada) — sempre citado na nota.

  **Escopo de cobertura, honesto**: só `kind` StorageV2/Storage/BlobStorage (onde "Block Blob" é o
  produto certo pra "Data Stored") — as 2 Storage Accounts reais encontradas no tenant
  (`stgcdchlg`/`stgtrackinghlg`) eram ambas StorageV2, então essa cobertura já resolve o caso real
  confirmado; FileStorage/BlockBlobStorage (nunca confirmados ao vivo) caem no fallback "não
  estimado" honesto, sem inventar.

  **Validação ao vivo — o que foi confirmado e o que não**: `az storage account show`
  (`accessTier="Hot"`), `az monitor metrics list --metric UsedCapacity` (2714161541 bytes reais) e
  a Retail Prices API tiered (3 faixas reais, 0.0326/0.03146/0.03032 USD/GB/mês) foram TODOS
  confirmados ao vivo, individualmente, contra a Storage Account real `stgcdchlg`
  (`rg-cdc-data-hlg`) e a API pública — inclusive usados como fixtures reais nos testes unitários
  novos (`TestPickTieredPrice_RealAzureTiers`, `TestStorageRedundancyFromSKU`,
  `TestResourceGroupFromID`, `internal/finops/data_resources_pricing_test.go`). **O caminho Go
  completo, ponta a ponta (a função `priceStorageAccount` chamando os 2 comandos `az` em
  sequência), não pôde ser validado end-to-end**: o token AAD do `az` CLI expirou genuinamente no
  meio da sessão (política de conditional access, limite de 4h) bem depois da validação individual
  de cada peça, mas antes de rodar o teste de integração final — reautenticar exige um `az login`
  interativo (browser), impossível neste ambiente sandboxed. Risco residual: só bugs de "encanamento"
  Go puro (parse de JSON, nomes de campo) — a lógica de negócio (fórmula, seleção de faixa,
  extração de redundância) já está coberta por teste unitário com os valores reais capturados.
  `go build`/`go vet`/`gofmt`/`go test ./internal/finops/... ./internal/web/handlers/... -race`
  limpos; `tsc --noEmit`/`eslint` sem erro novo no frontend.

- [ ] **F3.2 — Cosmos DB nunca é precificado — avaliado, não implementado nesta rodada.**
  `internal/finops/data_resources_pricing.go` — investigado durante esta sessão: **o tenant desta
  investigação não tem NENHUMA conta Cosmos DB provisionada** (`az resource list --query
  "[?type=='Microsoft.DocumentDB/databaseAccounts']"` → lista vazia, confirmado ao vivo). Cosmos DB
  é cobrado por RU/s provisionado por banco/container (não por conta, ao contrário de Storage) —
  implementar sem NENHUM recurso real pra confirmar a sintaxe do `az cosmosdb sql database/
  container throughput show` (ou a métrica `NormalizedRUConsumption`) e as unidades de preço da
  Retail Prices API pra RU/s contrariaria a disciplina de validação ao vivo seguida no resto deste
  projeto (ver F3.1 acima e o restante do CLAUDE.md). Mantido como "não estimado" honesto — retomar
  quando houver um Cosmos DB real disponível pra validar contra (outro tenant/cluster, ou se algum
  dia esta empresa provisionar um).

### Fase 4 — Robustez operacional (F4.1 concluída, validada ao vivo; F4.2 pendente de decisão)

- [x] **F4.1 — Sem debounce/lock em scans caros e repetíveis.**
  `GetReport` (`finops.go`) e `ScanRightsizing` (`finops_rightsizing.go`) foram divididos em
  handler-fino (parseia query params, monta a chave de dedup) + `doGetReport`/
  `doScanRightsizing` (o trabalho de verdade, agora rodando atrás de um
  `singleflight.Group` por handler — `reportSF`/`rightsizingScanSF`, campos novos em
  `FinOpsHandler`), mesmo padrão já usado em `GetFreshEKSToken`/`GetFreshGKEToken`
  (`internal/config/kubeconfig.go`). Chave de dedup (`reportSFKey`/`rightsizingScanSFKey`, funções
  puras testáveis) inclui TODO parâmetro que afeta o resultado/efeito colateral — cluster,
  namespaces, window_days, with_prometheus, prometheus_url, persist_rightsizing pra GetReport;
  cluster, window_days, prometheus_url pra ScanRightsizing — requisições com qualquer parâmetro
  diferente nunca são dedupadas entre si. O trabalho compartilhado roda com `context.Background()`
  (não o da requisição que disparou), mesmo trade-off já aceito por `getFreshEKSToken`: se essa
  requisição específica cancelar, o trabalho continua pros outros chamadores esperando o mesmo
  resultado.

  **Validado ao vivo, ponta a ponta, contra um cluster de produção real**
  (`akspriv-abastecimento-hlg-admin`, instância de teste isolada na porta 8091, nunca o processo
  real do usuário na 8080): 6 requisições `GET /finops/report` concorrentes e idênticas
  produziram exatamente **1** execução real (`"FinOps: relatório gerado"` no log, 1 ocorrência;
  as 6 respostas HTTP byte-a-byte idênticas via `md5sum`, todas em ~18,4s — não 6×18s); 4
  requisições `POST /finops/rightsizing/scan` concorrentes e idênticas produziram exatamente **1**
  scan real (mesmo padrão de confirmação); 2 requisições `GET /finops/report` concorrentes com
  `window_days` DIFERENTE (7 vs. 14) corretamente dispararam **2** execuções independentes — nunca
  uma falsa deduplicação entre parâmetros distintos. Também coberto por 6 testes unitários
  permanentes (`internal/web/handlers/finops_singleflight_test.go`) — 4 sobre as funções de chave
  (mesmos parâmetros → mesma chave; qualquer parâmetro diferente → chave nunca colide) e 2 sobre o
  mecanismo `singleflight.Group` em si (N chamadas concorrentes com a mesma chave → 1 execução;
  chaves diferentes → execuções independentes), rodados 5x seguidas com `-race` sem flake.
  `go build`/`go vet`/`gofmt`/`go test ./internal/web/handlers/... -race` limpos.

- [x] **F4.2 — Decisão de RBAC pendente pras rotas `/finops/*`.**
  Decisão explícita do usuário (`AskUserQuestion`): aplicar `RequireSREGroup()` agora, por
  consistência com o resto da app. `internal/web/server.go` — as 4 rotas de escrita/scan do
  FinOps (`POST /finops/rightsizing/scan`, `POST /finops/pricing/refresh`, `POST /finops/analyze`,
  `POST /finops/storage/refresh`) ganharam `rbacMiddleware.RequireSREGroup()`, mesmo padrão já
  usado por `POST /nodepools/registry/scan`. As demais rotas `GET` (leitura pura) permanecem sem
  RBAC extra, mesmo critério já documentado inline nelas ("histórico on-demand... leitura, sem RBAC
  extra"). `RequireSREGroup()` continua sendo um no-op documentado
  (`internal/web/middleware/rbac.go`, `c.Set("isSRE", true); c.Next()`) — zero efeito comportamental
  hoje, confirmado lendo o código-fonte do middleware antes de aplicar — só protege
  automaticamente estas 4 rotas no momento em que ele for reativado em qualquer lugar da app,
  sem exigir lembrar de voltar aqui. `go build`/`go vet`/`gofmt` limpos; nenhum teste (RBAC ou
  outro) referencia essas rotas, então nada precisou de atualização.

### Fase 5 — Manutenibilidade (menor risco/urgência — adiável)

- [x] **F5.1 — `FinOpsTab.tsx` com 4582 linhas, um único arquivo.**
  Extraído em `internal/web/frontend/src/components/finops/` (mesmo padrão já usado pra
  `RightsizingTab.tsx`/`DataResourcesPanel.tsx`): `types.ts` (todas as interfaces compartilhadas —
  `FinOpsPool`/`FinOpsWorkload`/`FinOpsReport`/tipos de Timeline/`Recommendation`), `helpers.ts`
  (`buildRecommendation` — compartilhada por Workloads e Oportunidades —, `financeProviderInfo`,
  `metricsCollectionLikelyFailed`) e um arquivo por aba: `DashboardTab.tsx`, `NodePoolsTab.tsx`
  (inclui `PoolSKUAlternatives`), `WorkloadsTab.tsx`, `HPAHistoryTab.tsx` (inclui
  `HPAComparePanel`/`HPADetailChart`/`HPASparkline`), `StorageTab.tsx`, `OpportunitiesTab.tsx`,
  `RelatorioTab.tsx` (inclui o export de PDF). `FinOpsTab.tsx` caiu de 4589 pra 483 linhas — agora
  só orquestra o fetch do relatório principal + a barra de abas, importando cada aba do módulo
  próprio.

  **Extração puramente mecânica, sem mudança de comportamento** — cada bloco foi movido linha a
  linha (via `sed` pra extrair os ranges exatos, cross-referenciados por grep pra achar toda
  dependência cruzada entre abas antes de cortar), sem tocar em nenhuma lógica de negócio, JSX ou
  fraseologia. Validado comparando o estado ANTES/DEPOIS via `git stash`: `npx tsc --noEmit`
  idêntico (0 erros nos dois), `npx eslint .` com contagem **byte-a-byte idêntica** (555
  problemas: 436 erros + 119 warnings, e a contagem POR REGRA também idêntica — confirma que
  nenhum lint novo foi introduzido nem nenhum pré-existente foi silenciosamente corrigido/mascarado
  durante a extração). `npx vite build` limpo (mesmos avisos pré-existentes de code-splitting do
  `jsPDF`, já presentes antes por outros consumidores do mesmo import dinâmico). `go build`/`make
  build` também limpos (mudança 100% frontend, backend intocado). **Não clicado no navegador nesta
  rodada** (sem ferramenta de automação disponível) — risco residual mitigado pela extração
  mecânica + validação de lint/tipo idêntica ao baseline, mesmo padrão de risco já aceito noutras
  extrações de componente desta sessão (ex: `RightsizingTab.tsx`).

- [ ] **F5.2 — Sem TTL/expiração de recomendações persistidas muito antigas.**
  `internal/storage/finops_rightsizing_store.go` — `generated_at` é guardado e exposto, mas nada
  no STORE em si força expiração; é 100% escolha de exibição do frontend (ver F2.3). **Ação**:
  avaliar se vale um `GetByCluster` que já sinaliza `stale: true` quando `generated_at` passa de
  um limiar, em vez de deixar essa lógica só no componente React.

---

## Ordem de execução recomendada

1. **Fase 0** primeiro — são bugs de correção de dados, sem risco de regressão visual, e afetam a
   confiabilidade de TODAS as outras fases (uma sugestão de tier baseada em dado incompleto/errado
   é pior que nenhuma sugestão).
2. **Fase 1** em seguida — resolve diretamente o incidente que motivou esta auditoria.
3. **Fases 2-3** podem rodar em paralelo (times/sessões diferentes) — não se sobrepõem em arquivo.
4. **Fase 4** antes de qualquer divulgação mais ampla da ferramenta pra mais usuários (o risco de
   scans concorrentes cresce com mais gente usando ao mesmo tempo).
5. **Fase 5** oportunista — fazer quando outra fase já estiver tocando o mesmo arquivo, não como
   trabalho dedicado isolado.

## Verificação (aplicar a cada item concluído, mesmo padrão já usado nesta sessão)

- `go build ./...`, `go vet ./...`, `gofmt -l`, `go test ./internal/finops/... ./internal/storage/... ./internal/web/handlers/... -race`
- `npx tsc --noEmit -p tsconfig.app.json`, `npx eslint <arquivos tocados>`, `npx vite build`
- Validar ao vivo contra pelo menos 1 cluster real por mudança de backend (instância isolada em
  porta diferente de 8080, nunca o servidor real do usuário — mesmo protocolo já seguido durante
  toda esta sessão) antes de considerar o item concluído.
