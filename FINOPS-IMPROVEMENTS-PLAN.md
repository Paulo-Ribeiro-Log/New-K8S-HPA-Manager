# Plano: Melhorias do FinOps (auditoria de gaps, falhas e riscos)

**Status**: 📋 planejado — checklist pronta para execução, nenhuma fase iniciada.
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

### Fase 0 — Correções de correção de dados (bugs reais, silenciosos, sem mudança de escopo)

- [ ] **F0.1 — Enriquecimento Prometheus-only não cai pra avg quando P95 arredonda pra 0.**
  `internal/finops/prometheus_enricher.go:270` (`hasUsage := cpuP95 > 0 || memP95 > 0`) e `:304-309`
  só populam `CPURecommendedMillis`/`MemRecommendedMi` quando `cpuP95 > 0`/`memP95 > 0` — sem
  fallback pra `avg`, diferente de `internal/finops/dynatrace_enricher.go:76-84`, que já tem esse
  fallback. Um workload enriquecido só via Prometheus (sem dado DT) com uso baixo-mas-real (avg
  não-zero, P95 arredondando pra 0) fica sem NENHUMA recomendação e mantém o verdict default
  `"ok"`. **Ação**: espelhar em `prometheus_enricher.go` o mesmo fallback P95→avg que já existe em
  `dynatrace_enricher.go`. Cobre também `EnrichWorkloadsPartial` (mesma função por baixo).
  **Teste**: caso com `cpuP95=0, cpuAvg>0` deve popular `CPURecommendedMillis` a partir do avg,
  igual ao teste equivalente já existente pro enricher Dynatrace.

- [ ] **F0.2 — Verdict default `"ok"` é indistinguível de "verificado eficiente".**
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

- [ ] **F0.3 — Pricing de disco OS do EKS quebra silenciosamente pra contexts aliased.**
  `internal/finops/calculator.go:388-441` (`osDiskCostForPool`) despacha por
  `strings.HasPrefix(cluster, "arn:aws:eks:")`/`"gke_"` em vez de `config.DetectCloudProvider` (a
  fonte de verdade já usada em `pricerForCluster`/`calculatePVCCost`). Contexts EKS com alias (ex:
  `cluster-apis-prd`, não o ARN completo — classe de bug já documentada e corrigida noutros
  lugares desta app) caem no path DEFAULT (Azure): lê um label de node Azure-only, e se algo
  incompatível colar, chama `diskPricer.GetDiskPrice("Premium SSD", ...)` — um pricer de Managed
  Disk Azure — contra um node pool AWS. Best case: `ok=false`, custo de disco OS só ausente do
  total. Worst case: número Azure atribuído a um pool AWS, sem aviso. **Ação**: trocar o
  dispatch de `osDiskCostForPool` pra usar `config.DetectCloudProvider`, igual ao resto do pacote.

### Fase 1 — Segurança das recomendações (evitar sugestão perigosa em pool crítico)

Motivado diretamente pelo incidente relatado (pool "ingress" com nginx-ingress-controller/velero/
istio-ingressgateway recebendo sugestão de downsize sem aviso).

- [ ] **F1.1 — `SuggestVMTier` não tem nenhum sinal de criticidade do workload.**
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

- [ ] **F1.2 — Nenhuma checagem de que a SKU sugerida comporta o maior pod individual do pool.**
  Mesma função (`vm_tiers.go`) — a decisão é 100% sobre a MÉDIA/agregado do pool, nunca o pico de
  um único pod. Uma SKU menor sugerida pode ter memória-por-vCPU insuficiente pro maior pod
  individual rodando ali, que simplesmente não conseguiria ser agendado no node menor. **Ação**:
  no mesmo ponto que calcula `cpuUtilPct`/`memUtilPct` agregados
  (`internal/web/handlers/finops_rightsizing.go`, agregação por pool), também calcular o MAIOR
  `CPURequestMillis`/`MemRequestMi` individual entre os workloads do pool, e rejeitar (ou marcar
  como "requer revisão manual") qualquer alternativa cuja capacidade por node fique abaixo desse
  valor + margem.

- [ ] **F1.3 — Frontend não exibe nenhum aviso de criticidade no card de sugestão.**
  `RightsizingTab.tsx` (`NodePoolTierCard`, linhas ~743-829, confirmado via leitura completa +
  grep por istio/velero/ingress/namespace — nenhuma checagem encontrada) renderiza a sugestão de
  downsize com a MESMA aparência visual pra qualquer pool, seja ele infra crítica ou app comum.
  **Ação** (depende de F1.1 persistir o sinal no backend primeiro): quando
  `has_critical_workload` vier `true`, renderizar um badge/alerta amarelo explícito no card
  ("Este pool roda componentes de infraestrutura — reveja com cuidado antes de aplicar") ao lado
  da sugestão, sem esconder a sugestão em si (a economia ainda pode ser válida, só precisa de
  mais atenção humana).

### Fase 2 — Discoverability / UX (mesma classe de problema já corrigida uma vez pro RG-data)

- [ ] **F2.1 — Aba Rightsizing é a única das 8 sem badge de contagem/urgência.**
  `FinOpsTab.tsx:4500-4539` — Node Pools, Workloads, HPA, Armazenamento, Oportunidades e Relatório
  têm todas um `<Badge>` numérico (algumas em vermelho/laranja) no `TabsTrigger`; Rightsizing é só
  texto puro. Combinado com ser a última das 8 abas, não há nenhum sinal visual de que ali existe
  economia em R$. **Ação**: adicionar `<Badge>` na `TabsTrigger` do Rightsizing mostrando a soma
  de `monthly_savings_brl` das alternativas de tier + o número de workloads com `verdict !=
  "ok"` — mesmo padrão visual já usado nas outras 6 abas.

- [ ] **F2.2 — `DataResourcesPanel.tsx` não distingue falha transiente de "não aplicável".**
  Linhas 75-87 — o `useQuery` desestrutura só `{ data, isLoading }`, nunca `error`/`isError`, com
  `retry: false`. Uma falha real de rede/Azure cai no MESMO branch (`!data?.available`, linha 107)
  que o caso legítimo "este cluster não tem RG de dados" — texto idêntico, sem botão de tentar de
  novo. **Ação**: capturar `error`/`isError` do `useQuery` e renderizar um estado distinto
  ("falha ao consultar — tentar novamente", com botão que chama `refetch()`) em vez de cair no
  mesmo texto neutro de "não disponível".

- [ ] **F2.3 — `last_scanned_at` sem escalonamento visual de idade.**
  `RightsizingTab.tsx:994-998` — mostrado sempre em cinza neutro, "45d atrás" com a mesma
  aparência de "5min atrás". **Ação**: escalar cor/ícone quando a idade passar de um limiar (ex:
  >7 dias → âmbar com aviso "considere reanalisar"; >30 dias → vermelho).

### Fase 3 — Completude de precificação (RG de dados)

- [ ] **F3.1 — Storage Account nunca é precificado.**
  `internal/finops/data_resources_pricing.go:70` — sempre `pricing_note` explicando que é
  baseado em volume/uso, nunca um número. Pra times com arquitetura PaaS-heavy (ao contrário do
  cluster "abastecimento", VM-heavy, único validado ao vivo até agora), o total do RG de dados
  pode aparecer perto de zero enquanto o gasto real está justamente aqui. **Ação**: avaliar
  estimativa via Azure Monitor Metrics API (capacidade usada real da conta, `UsedCapacity`) — ou,
  na ausência disso, tornar o aviso de "sem estimativa" mais visível no total agregado (hoje só
  aparece por item, dentro da lista expandida).

- [ ] **F3.2 — Cosmos DB nunca é precificado.**
  `internal/finops/data_resources_pricing.go:76` — mesma lacuna, "RU/s não visível neste nível".
  **Ação**: avaliar Azure Monitor Metrics API (`NormalizedRUConsumption`/RU provisionado) como
  fonte, mesmo princípio de F3.1.

### Fase 4 — Robustez operacional

- [ ] **F4.1 — Sem debounce/lock em scans caros e repetíveis.**
  `POST /finops/rightsizing/scan` (`finops_rightsizing.go`) e `GET /finops/report?
  with_prometheus=true` (`finops.go:141`) não têm nenhum `singleflight.Group` nem lock por
  cluster — confirmado via grep (só existe `awsPricersMu`, que protege um cache não relacionado).
  Cada chamada é 40-60s batendo Prometheus+Dynatrace+Azure Retail Prices+K8s API. Duas abas do
  browser, dois usuários, ou um duplo-clique no meio do fluxo disparam scans concorrentes
  redundantes pro MESMO cluster. **Ação**: `singleflight.Group` por cluster nos dois endpoints,
  mesmo padrão já usado em `GetFreshEKSToken`/`GetFreshGKEToken` nesta app pra "operação cara e
  provavelmente duplicada em voo".

- [ ] **F4.2 — Decisão de RBAC pendente pras rotas `/finops/*`.**
  `internal/web/server.go:839-854` — nenhuma das 14 rotas FinOps usa
  `rbacMiddleware.RequireSREGroup()`, diferente de operações comparáveis noutras partes da app
  (ex: `POST /nodepools/registry/scan`). Hoje isso não é uma vulnerabilidade viva —
  `RequireSREGroup()` é um no-op documentado (`internal/web/middleware/rbac.go:68-73`) — mas é uma
  inconsistência que deixaria FinOps (IDs de subscription, nomes de recurso, dados de custo via
  `GetDataResources`, scans caros) desprotegido no exato momento em que esse middleware for
  reativado em qualquer outro lugar da app, porque ninguém vai lembrar de adicionar aqui também.
  **Ação**: decisão explícita do usuário — aplicar `RequireSREGroup()` nas rotas de escrita/scan
  do FinOps agora (por consistência, mesmo sendo no-op), ou registrar deliberadamente como "fora
  de escopo" e mover pra um backlog de segurança geral da app.

### Fase 5 — Manutenibilidade (menor risco/urgência — adiável)

- [ ] **F5.1 — `FinOpsTab.tsx` com 4582 linhas, um único arquivo.** Já era um problema conhecido;
  `RightsizingTab.tsx` (1141 linhas) está seguindo a mesma trajetória depois de só 1 feature nova.
  **Ação**: extrair cada aba do `FinOpsTab.tsx` pro próprio arquivo, mesmo padrão já usado pra
  `RightsizingTab.tsx`/`DataResourcesPanel.tsx` — comece pelas abas mais simples
  (Dashboard/Relatório) antes das mais acopladas (Workloads/Oportunidades).

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
