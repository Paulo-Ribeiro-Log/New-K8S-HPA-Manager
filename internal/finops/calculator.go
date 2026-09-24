package finops

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"k8s-hpa-manager/internal/storage"
)

// clusterCapacity é o alias interno de ClusterCapacity (definida em models.go)
type clusterCapacity = ClusterCapacity

// rawWorkload é a agregação temporária de pods por workload antes do cálculo de custo
type rawWorkload struct {
	Namespace        string
	Workload         string
	Pods             int
	CPURequestMillis float64
	MemRequestMi     float64
	CPULimitMillis   float64
	MemLimitMi       float64
	HPAMin           int
	HPAMax           int
	HPACurrent       int
	// PoolPodCounts conta quantos pods deste workload rodam em cada node pool — na prática quase
	// sempre 1 pool só (afinidade/taint), mas cobre o caso raro de split sem quebrar. Usado só pra
	// escolher o NodePool "principal" do workload (allocateCosts) — nunca pra ratear custo, que
	// continua sendo por fração do cluster inteiro (allocateCosts, inalterado).
	PoolPodCounts map[string]int
	// NodePodCounts é o mesmo critério de PoolPodCounts, mas por nome literal de node (não pool) —
	// usado pra popular FinOpsWorkload.NodeName, que correlaciona o workload com NodeUsage
	// (ver live_metrics.go) na aba Rightsizing.
	NodePodCounts map[string]int
	// OldestPodStartedAt é o CreationTimestamp do pod Running mais antigo deste workload —
	// contextualiza "há quanto tempo o workload está no ar sem reiniciar" pra interpretar um pico
	// histórico (ver FinOpsWorkload.OldestPodStartedAt em models.go).
	OldestPodStartedAt time.Time
}

// nodePoolLabelFromNode retorna o nome do node pool a partir dos labels de um node K8s
// (multi-cloud: AKS/EKS/GKE). Duplica de propósito a mesma lógica de
// internal/web/handlers/nodepools_snat.go::nodePoolLabel — não dá pra importar de lá
// (handlers já importa finops, importar de volta criaria ciclo); mesma classe de duplicação
// pequena/estável já aceita neste projeto por motivo de fronteira de pacote (ver
// internal/healthcheck/resource_enricher.go, que duplica verdictFromPrometheus pelo mesmo motivo).
func nodePoolLabelFromNode(labels map[string]string) string {
	if v := labels["kubernetes.azure.com/agentpool"]; v != "" { // AKS
		return v
	}
	if v := labels["agentpool"]; v != "" {
		return v
	}
	if v := labels["eks.amazonaws.com/nodegroup"]; v != "" { // EKS
		return v
	}
	if v := labels["cloud.google.com/gke-nodepool"]; v != "" { // GKE
		return v
	}
	return ""
}

// Calculator realiza a análise FinOps de um cluster
type Calculator struct {
	pricer                    CloudPricer
	diskPricer                *DiskPricer // nil = análise de storage desabilitada
	exchange                  *ExchangeRateProvider
	prometheusURL             string // opcional — usado para kubelet_volume_stats_used_bytes (Blob/Files)
	prometheusRequiresGCPAuth bool   // true quando prometheusURL é o GMP (ver internal/monitoring/discovery.RequiresGCPAuth)
}

// NewCalculator cria um novo calculator com as dependências injetadas.
// diskPricer pode ser nil — nesse caso a análise de storage é omitida sem erro.
func NewCalculator(pricer CloudPricer, diskPricer *DiskPricer, exchange *ExchangeRateProvider) *Calculator {
	return &Calculator{pricer: pricer, diskPricer: diskPricer, exchange: exchange}
}

// WithPrometheusURL define a URL do Prometheus para consultar uso real de Blob/Files.
//
// requiresGCPAuth: true quando url é o Google Cloud Managed Service for Prometheus (GMP, ver
// internal/monitoring/discovery.RequiresGCPAuth).
func (c *Calculator) WithPrometheusURL(url string, requiresGCPAuth bool) *Calculator {
	c.prometheusURL = url
	c.prometheusRequiresGCPAuth = requiresGCPAuth
	return c
}

// BuildReport coleta dados do cluster e monta o FinOpsReport completo.
// namespaces: filtra namespaces específicos; vazio = todos (exceto kube-system/kube-public).
// BuildReport gera o relatório FinOps para o cluster.
// dtEnricher: fonte primária de métricas históricas (Dynatrace). Pode ser nil.
// enricher:   fonte secundária (Prometheus). Usado como fallback quando DT não tem dados
//             para um workload, ou quando dtEnricher é nil.
// metricsClient: opcional (pode ser nil) — quando presente, popula CPUCurrentMillis/MemCurrentMi
//                por workload (live, via metrics-server) e FinOpsReport.NodeUsage (current+top
//                por node, ver live_metrics.go). Sem ele, o relatório funciona exatamente como
//                antes (só uso histórico via Prometheus/Dynatrace), sem os campos "current".
func (c *Calculator) BuildReport(
	ctx context.Context,
	cluster string,
	client kubernetes.Interface,
	pools []storage.NodePoolRegistryEntry,
	namespaces []string,
	dtEnricher *DTEnricher,
	enricher *PrometheusEnricher,
	metricsClient metricsclientset.Interface,
) (*FinOpsReport, error) {
	// Timing por fase — bug real relatado pelo usuário: "definitivamente depois dos ajuste...
	// o que temos é um scan de 2 minutos... não parece haver nenhum paralelismo". As rodadas
	// anteriores desta investigação já paralelizaram as queries do Prometheus, do Dynatrace e o
	// pré-fetch de uso de PVC — mas sem NENHUMA telemetria real de qual fase é de fato a
	// dominante, cada rodada foi um chute (ainda que fundamentado) sobre código nunca visto
	// rodando ao vivo. Esses logs eliminam o chute: a próxima vez que "ainda está lento" for
	// relatado, o log do servidor já mostra o tempo exato de cada fase, sem precisar de mais uma
	// rodada de "acho que pode ser X".
	overallStart := time.Now()
	logTiming := func(step string, start time.Time, extra map[string]interface{}) {
		ev := log.Info().Str("cluster", cluster).Str("step", step).Dur("elapsed", time.Since(start))
		for k, v := range extra {
			ev = ev.Interface(k, v)
		}
		ev.Msg("FinOps/timing")
	}

	rate, rateDate := c.exchange.Get()

	// 1. Calcular custo e capacidade dos node pools
	stepStart := time.Now()
	finOpsPools, capacity, clusterCostUSD, err := c.calculatePoolCosts(pools, rate)
	if err != nil {
		return nil, err
	}
	logTiming("calculatePoolCosts", stepStart, map[string]interface{}{"pools": len(finOpsPools)})

	// 2. Coletar workloads do cluster (pods + HPAs) + mapa pod→workload para o enricher +
	// mapa node→pool (reaproveitado abaixo pra NodeUsage).
	stepStart = time.Now()
	rawWorkloads, podToWorkload, nodeToPool, err := collectWorkloads(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}
	logTiming("collectWorkloads", stepStart, map[string]interface{}{"workloads": len(rawWorkloads)})

	// 3. Alocar custo proporcional a cada workload
	workloads := allocateCosts(rawWorkloads, capacity, clusterCostUSD, rate)

	// 4. Enriquecer com métricas históricas: Dynatrace (primário) → Prometheus (fallback) — roda
	// EM PARALELO com ComputeNodeUsage (uso por node, também via Prometheus) — bug real
	// corrigido, relatado pelo usuário: "a ferramenta está 2x mais demorada do que era quando
	// iniciamos esses ajustes". Achado via telemetria real do log do servidor (FinOps/timing):
	// as duas etapas fazem chamadas INDEPENDENTES ao MESMO Prometheus, cada uma com seu próprio
	// timeout de contexto (~60-75s) — quando o Prometheus do cluster está genuinamente fora do
	// ar (confirmado ao vivo: "context deadline exceeded" nas duas, uma logo depois da outra),
	// elas rodavam SEQUENCIALMENTE, somando os dois timeouts (~135s) em vez de pagar só o maior
	// dos dois (~75s) — exatamente a duplicação de tempo relatada. `uniqueNodes`/`nodeToPool` já
	// estão disponíveis desde collectWorkloads/allocateCosts (NodeName é preenchido lá, nunca
	// pelos enrichers abaixo — ver uniqueNonEmptyNodeNames), então ComputeNodeUsage não depende
	// de nada que o enriquecimento de workload produza — as duas são genuinamente independentes
	// (ComputeNodeUsage nunca lê/escreve em `workloads`, só devolve um []NodeUsage novo).
	windowDays := 0
	var dtEnriched map[string]bool
	var nodeUsage []NodeUsage

	var nodeWg sync.WaitGroup
	if uniqueNodes := uniqueNonEmptyNodeNames(workloads); len(uniqueNodes) > 0 {
		nodeWg.Add(1)
		go func() {
			defer nodeWg.Done()
			nodeStart := time.Now()
			nodeUsage = ComputeNodeUsage(ctx, client, metricsClient, uniqueNodes, nodeToPool, enricher)
			logTiming("ComputeNodeUsage", nodeStart, map[string]interface{}{"nodes": len(uniqueNodes)})
		}()
	}

	if dtEnricher != nil {
		stepStart = time.Now()
		dtEnriched = dtEnricher.EnrichWorkloads(ctx, workloads)
		windowDays = dtEnricher.windowDays
		logTiming("dtEnricher.EnrichWorkloads", stepStart, nil)
	}

	if enricher != nil {
		enricher.SetPodMapping(podToWorkload)
		stepStart = time.Now()
		if len(dtEnriched) > 0 {
			// Aplicar Prometheus apenas nos workloads sem dados DT
			enricher.EnrichWorkloadsPartial(ctx, workloads, dtEnriched)
		} else {
			enricher.EnrichWorkloads(ctx, workloads)
		}
		logTiming("prometheusEnricher.EnrichWorkloads", stepStart, nil)
		if windowDays == 0 {
			windowDays = enricher.window
		}
	}

	// 4b. Live (metrics-server): uso "current" de verdade por workload — best-effort, nunca
	// bloqueia o relatório se o metrics-server não estiver disponível (ver live_metrics.go).
	if metricsClient != nil {
		stepStart = time.Now()
		EnrichWorkloadsLiveMetrics(ctx, metricsClient, workloads, podToWorkload)
		logTiming("EnrichWorkloadsLiveMetrics", stepStart, nil)
	}

	nodeWg.Wait() // espera ComputeNodeUsage (goroutine acima) terminar antes de montar o relatório

	// 4c. Reclassifica "ok" genérico (sem enriquecimento nenhum) pra "sem_dados" — ver
	// reclassifyNoDataVerdicts.
	reclassifyNoDataVerdicts(workloads)

	// 5. Agregar por namespace
	nsMap := aggregateNamespaces(workloads)

	// 6. Montar summary base (compute)
	summary := buildSummary(workloads, nsMap, clusterCostUSD, rate)
	summary.MetricsAttempted = dtEnricher != nil || enricher != nil
	// Bug real corrigido — relatado pelo usuário com um scan real onde TODOS os workloads/pools
	// vieram com desperdício R$0, CPU/Mem 0%, "Com Oportunidade 0", e a suspeita certa dele foi
	// "a falha está em tentar buscar informações e falhar silenciosamente". Confirmado: DT/
	// Prometheus enrichment tinha essa exata falha — erro de query vira só log.Warn, nunca chega
	// na resposta da API, e "0 workloads com uso" é visualmente idêntico a "cluster sem
	// desperdício nenhum". summary.MetricsAttempted/MetricsWorkloadsEnriched (ver models.go) dão
	// ao frontend o sinal pra distinguir os dois casos; este log torna o mesmo sinal visível
	// direto no servidor, sem precisar abrir a UI pra perceber.
	// summary.MetricsCollectionError — distingue "falha de coleta transitória" (query de fato
	// falhou) de "cluster sem cobertura de monitoramento" (query teve sucesso, resultado vazio) —
	// ver comentário do campo em models.go. Computado sempre (não só quando MetricsWorkloadsEnriched
	// == 0) pra o frontend também poder mostrar a causa real quando ENRIQUECEU PARCIALMENTE mas
	// alguma das fontes falhou de verdade (ex: Dynatrace timeout, Prometheus fallback funcionou).
	var collectionErrParts []string
	if dtEnricher != nil {
		if dtErr := dtEnricher.CollectionError(); dtErr != nil {
			collectionErrParts = append(collectionErrParts, "Dynatrace: "+dtErr.Error())
		}
	}
	if enricher != nil {
		for _, promErr := range enricher.CollectionErrors() {
			collectionErrParts = append(collectionErrParts, "Prometheus/"+promErr)
		}
	}
	if len(collectionErrParts) > 0 {
		summary.MetricsCollectionError = strings.Join(collectionErrParts, "; ")
		// Só Prometheus, só timeout de query pesada: nenhuma falha do Dynatrace no meio.
		summary.MetricsCollectionTimeout = enricher != nil && enricher.HeavyQueryTimeoutsOnly() &&
			!strings.Contains(summary.MetricsCollectionError, "Dynatrace:")
	}

	if summary.MetricsAttempted && summary.MetricsWorkloadsEnriched == 0 && len(workloads) > 0 {
		log.Warn().Str("cluster", cluster).Int("workloads", len(workloads)).
			Bool("dynatrace_configured", dtEnricher != nil).
			Bool("prometheus_configured", enricher != nil).
			Bool("real_collection_error", summary.MetricsCollectionError != "").
			Str("collection_error_detail", summary.MetricsCollectionError).
			Msg("FinOps: NENHUM workload recebeu dado real de uso (Dynatrace/Prometheus) nesta análise — verifique 'real_collection_error' acima: true = falha transitória de coleta (VPN/rede/API), false = as consultas tiveram sucesso mas o cluster genuinamente não tem cobertura de monitoramento pra nenhum workload (não é algo que 'reanalisar' resolve sozinho).")
	}

	// 7. Storage: PVCs + disco OS por pool (não fatal — relatório retorna mesmo sem dados de storage)
	var pvcs []PVCCostItem
	var storageSummary StorageSummary
	if c.diskPricer != nil {
		storageCalc := NewStorageCalculator(c.diskPricer)
		storageCalc.WithPrometheus(c.prometheusURL, c.prometheusRequiresGCPAuth)
		// c.pricer já é *GCPPricer pra clusters GKE (escolhido por FinOpsHandler.pricerForCluster
		// antes de construir o Calculator) — reusa a mesma instância pra precificar PVCs em vez de
		// criar outra (mesmo catálogo/cache já aquecido pelo cálculo de compute acima).
		if gcpPricer, isGCP := c.pricer.(*GCPPricer); isGCP {
			storageCalc.WithGCPPricer(gcpPricer)
		}
		if awsPricer, isAWS := c.pricer.(*AWSPricer); isAWS {
			storageCalc.WithAWSPricer(awsPricer)
		}

		stepStart = time.Now()
		pvcs, storageSummary, err = storageCalc.Calculate(ctx, client, cluster, rate)
		if err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps: falha ao calcular storage (relatório retorna sem dados de storage)")
		}
		logTiming("storageCalc.Calculate", stepStart, map[string]interface{}{"pvcs": len(pvcs)})

		// 7a. Custo de disco OS por node pool — bug real corrigido: pra AKS (o caminho padrão de
		// osDiskCostForPool, ver comentário da função) isso é uma chamada K8s AO VIVO (Nodes List
		// por pool, via OSDiskForNodePool) — rodava sequencial, UMA por pool, aqui dentro deste
		// loop. Num cluster com muitos pools, isso sozinho já pagava N round-trips ao kube-
		// apiserver em série, mesma classe de problema já corrigida (Prometheus, Dynatrace, PVC)
		// nas rodadas anteriores desta mesma investigação, só que nunca olhada até agora. GKE/EKS
		// não sofrem disso (osDiskCostForPool só consulta pricer em cache pra esses providers),
		// mas paraleliza sempre — sem custo extra nesse caso, cada goroutine só lê de um cache.
		stepStart = time.Now()
		poolRegistryByName := make(map[string]storage.NodePoolRegistryEntry, len(pools))
		for _, p := range pools {
			poolRegistryByName[p.NodePool] = p
		}

		var wg sync.WaitGroup
		for i := range finOpsPools {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				sku, tier, sizeGB, priceUSD, ok := c.osDiskCostForPool(ctx, client, finOpsPools[i].Name, poolRegistryByName[finOpsPools[i].Name], storageCalc)
				if !ok {
					log.Debug().Str("pool", finOpsPools[i].Name).Msg("FinOps: preço de OS disk não encontrado")
					return
				}
				osDiskCostUSD := round2(priceUSD * float64(finOpsPools[i].NodeCount))
				finOpsPools[i].OSDiskSKU = sku
				finOpsPools[i].OSDiskTier = tier
				finOpsPools[i].OSDiskGB = sizeGB
				finOpsPools[i].OSDiskCostUSD = osDiskCostUSD
				finOpsPools[i].OSDiskCostBRL = round2(osDiskCostUSD * rate)
				finOpsPools[i].TotalCostUSD = round2(finOpsPools[i].MonthlyCostUSD + osDiskCostUSD)
				finOpsPools[i].TotalCostBRL = round2(finOpsPools[i].MonthlyCostBRL + finOpsPools[i].OSDiskCostBRL)
			}()
		}
		wg.Wait()

		// Soma sequencial DEPOIS de todas as goroutines terminarem — nunca dentro delas (evita
		// precisar de mutex pras variáveis compartilhadas abaixo, cada goroutine só escreve no
		// seu próprio índice finOpsPools[i], nunca em totalOSDiskCostBRL/storageSummary).
		var totalOSDiskCostBRL float64
		for i := range finOpsPools {
			totalOSDiskCostBRL = round2(totalOSDiskCostBRL + finOpsPools[i].OSDiskCostBRL)
			storageSummary.OSDiskCostUSD = round2(storageSummary.OSDiskCostUSD + finOpsPools[i].OSDiskCostUSD)
			storageSummary.OSDiskCostBRL = round2(storageSummary.OSDiskCostBRL + finOpsPools[i].OSDiskCostBRL)
		}
		logTiming("osDiskCostForPool (todos os pools, em paralelo)", stepStart, map[string]interface{}{"pools": len(finOpsPools)})

		// 7b. Enriquecer workloads com custo de PVCs correlacionados
		pvcByWorkload := groupPVCsByWorkload(pvcs)
		for i := range workloads {
			key := workloads[i].Namespace + "/" + workloads[i].Workload
			if pvcItems, ok := pvcByWorkload[key]; ok {
				for _, p := range pvcItems {
					workloads[i].StorageCostUSD = round2(workloads[i].StorageCostUSD + p.MonthlyCostUSD)
					workloads[i].StorageCostBRL = round2(workloads[i].StorageCostBRL + p.MonthlyCostBRL)
					workloads[i].PVCCount++
					workloads[i].PVCCapacityGB = round2(workloads[i].PVCCapacityGB + p.CapacityGB)
				}
			}
		}

		// 7c. Atualizar summary com totais de storage
		summary.StorageMonthlyCostUSD = storageSummary.TotalMonthlyCostUSD
		summary.StorageMonthlyCostBRL = storageSummary.TotalMonthlyCostBRL
		summary.OSDiskCostBRL = totalOSDiskCostBRL
		summary.OrphanedStorageCostBRL = storageSummary.OrphanedCostBRL
		summary.TotalWithStorageBRL = round2(summary.TotalMonthlyCostBRL + storageSummary.TotalMonthlyCostBRL + totalOSDiskCostBRL)
	}

	logTiming("BuildReport TOTAL", overallStart, map[string]interface{}{"workloads": len(workloads), "node_pools": len(finOpsPools)})

	return &FinOpsReport{
		Cluster:      cluster,
		GeneratedAt:  time.Now(),
		ExchangeRate: rate,
		ExchangeDate: rateDate,
		WindowDays:   windowDays,
		NodePools:    finOpsPools,
		Namespaces:   nsMap,
		Workloads:    workloads,
		PVCs:         pvcs,
		Storage:      storageSummary,
		Summary:      summary,
		NodeUsage:    nodeUsage,
	}, nil
}

// uniqueNonEmptyNodeNames extrai os NodeName distintos e não-vazios dos workloads já alocados —
// usado pra saber quais nodes computar em ComputeNodeUsage sem repetir o mesmo node várias vezes
// (workloads costumam compartilhar node dentro do mesmo pool).
func uniqueNonEmptyNodeNames(workloads []FinOpsWorkload) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, wl := range workloads {
		if wl.NodeName == "" {
			continue
		}
		if _, ok := seen[wl.NodeName]; ok {
			continue
		}
		seen[wl.NodeName] = struct{}{}
		names = append(names, wl.NodeName)
	}
	return names
}

// Defaults usados quando o registry não tem o disco real do pool GKE (cluster nunca escaneado
// depois desta mudança, ou Container API falhou no scan) — pd-balanced é o default real da
// plataforma GKE desde ~2022 (antes era pd-standard); 100GB é o tamanho de boot disk sugerido
// pela documentação do GKE quando não customizado.
const (
	defaultGKEDiskSizeGB = 100
	defaultGKEDiskType   = "pd-balanced"
)

// osDiskCostForPool calcula o custo mensal do disco de boot/OS de um node pool, dividido em
// caminhos totalmente diferentes por provider (não é só trocar o pricer — o próprio modelo de
// billing é diferente):
//   - AKS (padrão/fallback pra qualquer provider que não seja GKE/EKS): tamanho detectado ao vivo
//     via label de node K8s (OSDiskForNodePool, inalterado), preço por "tier" fixo (Azure Managed
//     Disk) — mesmo comportamento de sempre.
//   - GKE: tamanho/tipo reais vindos do Node Pool Registry (populados no Scan via Container API —
//     K8s não expõe isso como label), preço linear USD/GB/mês (GetDiskPricePerGBMonth). Sem
//     conceito de "tier" — retorna tier="".
//   - EKS: preço linear USD/GB/mês (AWSPricer.GetDiskPricePerGBMonth), mas sempre com os defaults
//     acima (defaultEKSDiskSizeGB/gp3) — diferente do GKE, o tamanho/tipo reais de EBS não são
//     capturados nesta fase (não validado ao vivo contra conta AWS real, ver CLAUDE.md).
// osDiskCostForPool despacha a precificação de disco OS por cloud provider. Bug real corrigido
// (FINOPS-IMPROVEMENTS-PLAN.md F0.3): a versão anterior decidia o provider via
// `strings.HasPrefix(cluster, "gke_"/"arn:aws:eks:")` — o resto do pacote (`pricerForCluster`,
// que já escolheu `c.pricer` antes deste Calculator ser construído) usa
// `config.DetectCloudProvider`, muito mais robusto (cobre contexts EKS "aliased", ex:
// "cluster-apis-prd" em vez do ARN completo — classe de bug já documentada e corrigida noutros
// lugares desta app). Um context EKS aliased nunca batia no prefixo `"arn:aws:eks:"` aqui e caía
// no path DEFAULT (Azure): lia um label de node Azure-only e, na pior hipótese, chamava
// `diskPricer.GetDiskPrice("Premium SSD", ...)` — um pricer de Managed Disk Azure — pra um node
// pool AWS de verdade, sem nenhum aviso.
//
// Corrigido despachando pelo TIPO CONCRETO de `c.pricer` em vez de reanalisar o nome do cluster
// — `c.pricer` já reflete a detecção correta (é sempre construído via `pricerForCluster`, que já
// chama `config.DetectCloudProvider`, antes de qualquer `Calculator` existir), então checar o
// tipo aqui nunca diverge da fonte de verdade usada pelo resto do relatório (compute, PVC).
func (c *Calculator) osDiskCostForPool(
	ctx context.Context,
	client kubernetes.Interface,
	poolName string,
	registryEntry storage.NodePoolRegistryEntry,
	storageCalc *StorageCalculator,
) (sku, tier string, sizeGB int, priceUSD float64, ok bool) {
	if gcpPricer, isGCP := c.pricer.(*GCPPricer); isGCP {
		diskType := registryEntry.DiskType
		if diskType == "" {
			diskType = defaultGKEDiskType
		}
		size := registryEntry.DiskSizeGB
		if size <= 0 {
			size = defaultGKEDiskSizeGB
		}

		pricePerGBMonth, _, err := gcpPricer.GetDiskPricePerGBMonth(diskType)
		if err != nil {
			return "", "", 0, 0, false
		}
		return diskType, "", size, round2(pricePerGBMonth * float64(size)), true
	}

	if awsPricer, isAWS := c.pricer.(*AWSPricer); isAWS {
		diskType := defaultEKSDiskType
		size := defaultEKSDiskSizeGB

		pricePerGBMonth, _, err := awsPricer.GetDiskPricePerGBMonth(diskType)
		if err != nil {
			return "", "", 0, 0, false
		}
		return diskType, "", size, round2(pricePerGBMonth * float64(size)), true
	}

	// Default: AKS (c.pricer é *AzurePricer nesse caso).
	sizeGB, _ = storageCalc.OSDiskForNodePool(ctx, client, poolName)
	tier = ResolveManagedDiskTier("Premium SSD", float64(sizeGB)) // AKS default: Premium SSD
	priceUSD, _, err := c.diskPricer.GetDiskPrice("Premium SSD", tier, defaultPricingRegion)
	if err != nil {
		return "", "", 0, 0, false
	}
	return "Premium SSD", tier, sizeGB, priceUSD, true
}

// calculatePoolCosts retorna os FinOpsPools com preço real, a capacidade total do cluster
// e o custo mensal total em USD.
func (c *Calculator) calculatePoolCosts(
	pools []storage.NodePoolRegistryEntry,
	rate float64,
) ([]FinOpsPool, clusterCapacity, float64, error) {
	var finOpsPools []FinOpsPool
	var cap clusterCapacity
	var totalCostUSD float64

	for _, p := range pools {
		if p.VMSize == "" || p.NodeCount == 0 {
			continue
		}

		price, source, err := c.pricer.GetPrice(p.VMSize)
		if err != nil {
			log.Warn().Str("vm_size", p.VMSize).Str("cluster", p.Cluster).
				Err(err).Msg("Preço não encontrado para VM, ignorando pool")
			continue
		}

		cpuCores, memGB := c.pricer.GetVMSpecs(p.VMSize)

		monthlyCostUSD := round2(price * HoursPerMonth * float64(p.NodeCount))
		totalCPUMillis := int64(cpuCores) * int64(p.NodeCount) * 1000
		totalMemMi := int64(memGB) * int64(p.NodeCount) * 1024

		finOpsPools = append(finOpsPools, FinOpsPool{
			Name:               p.NodePool,
			VMSize:             p.VMSize,
			VMCPUCores:         cpuCores,
			VMMemoryGB:         memGB,
			VMPriceUSDHour:     price,
			PriceSource:        source,
			NodeCount:          p.NodeCount,
			Mode:               p.Mode,
			MonthlyCostUSD:     monthlyCostUSD,
			MonthlyCostBRL:     round2(monthlyCostUSD * rate),
			TotalCPUMillicores: totalCPUMillis,
			TotalMemoryMi:      totalMemMi,
		})

		cap.CPUMillicores += totalCPUMillis
		cap.MemoryMi += totalMemMi
		totalCostUSD += monthlyCostUSD
	}

	return finOpsPools, cap, totalCostUSD, nil
}

// collectWorkloads lista pods Running e HPAs do cluster.
// Retorna: lista de rawWorkload + mapa "ns/pod" → "ns/workload" para o enricher Prometheus +
// mapa "node" → "pool" (usado tanto pro NodePool dos workloads quanto pra NodeUsage, ver
// Calculator.BuildReport).
func collectWorkloads(
	ctx context.Context,
	client kubernetes.Interface,
	namespaces []string,
) ([]rawWorkload, map[string]string, map[string]string, error) {
	// 1. Listar todos os ReplicaSets para resolver RS → Deployment
	rsItems, err := listAllReplicaSets(ctx, client)
	if err != nil {
		return nil, nil, nil, err
	}
	rsOwner := make(map[string]string) // "ns/rs-name" → deployment name
	for _, rs := range rsItems {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" {
				rsOwner[rs.Namespace+"/"+rs.Name] = ref.Name
			}
		}
	}

	// 2. Listar HPAs (todos os namespaces)
	hpaList, err := client.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	type hpaInfo struct{ min, max, current int }
	hpaMap := make(map[string]hpaInfo)
	for _, h := range hpaList.Items {
		min := 1
		if h.Spec.MinReplicas != nil {
			min = int(*h.Spec.MinReplicas)
		}
		key := h.Namespace + "/" + h.Spec.ScaleTargetRef.Name
		hpaMap[key] = hpaInfo{
			min:     min,
			max:     int(h.Spec.MaxReplicas),
			current: int(h.Status.CurrentReplicas),
		}
	}

	// 3. Listar pods Running
	nsFilter := buildNamespaceFilter(namespaces)
	podItems, err := listAllPods(ctx, client, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, nil, nil, err
	}

	// 3b. Listar Nodes (uma única vez) e resolver node → pool — necessário pra saber em qual pool
	// cada workload roda (usado pela sugestão de tier de VM, ver SuggestVMTier em vm_tiers.go).
	// Best-effort: falha aqui não aborta o relatório inteiro, só deixa NodePool vazio nos
	// workloads (a sugestão de tier fica sem dado pro pool afetado, resto do relatório intacto).
	nodeToPool := make(map[string]string)
	if nodeList, nodeErr := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); nodeErr == nil {
		for _, n := range nodeList.Items {
			if pool := nodePoolLabelFromNode(n.Labels); pool != "" {
				nodeToPool[n.Name] = pool
			}
		}
	} else {
		log.Warn().Err(nodeErr).Msg("FinOps: falha ao listar nodes — sugestão de tier de VM ficará sem dado de uso real por pool")
	}

	// 4. Agregar por workload + construir mapa pod→workload para o enricher Prometheus
	workloadMap := make(map[string]*rawWorkload)
	podToWorkload := make(map[string]string, len(podItems))

	for i := range podItems {
		pod := &podItems[i]
		if !shouldIncludeNamespace(pod.Namespace, nsFilter) {
			continue
		}

		workloadName := ResolveWorkload(pod, rsOwner)
		key := pod.Namespace + "/" + workloadName

		// Mapa para o enricher: "ns/pod-name" → "ns/workload-name"
		podToWorkload[pod.Namespace+"/"+pod.Name] = key

		if _, ok := workloadMap[key]; !ok {
			h := hpaMap[key]
			workloadMap[key] = &rawWorkload{
				Namespace:     pod.Namespace,
				Workload:      workloadName,
				HPAMin:        h.min,
				HPAMax:        h.max,
				HPACurrent:    h.current,
				PoolPodCounts: make(map[string]int),
				NodePodCounts: make(map[string]int),
			}
		}

		wl := workloadMap[key]
		wl.Pods++
		wl.CPURequestMillis += sumCPURequestsMillis(pod)
		wl.MemRequestMi += sumMemRequestsMi(pod)
		wl.CPULimitMillis += sumCPULimitsMillis(pod)
		wl.MemLimitMi += sumMemLimitsMi(pod)
		if pool := nodeToPool[pod.Spec.NodeName]; pool != "" {
			wl.PoolPodCounts[pool]++
		}
		if pod.Spec.NodeName != "" {
			wl.NodePodCounts[pod.Spec.NodeName]++
		}
		if podCreated := pod.CreationTimestamp.Time; !podCreated.IsZero() {
			if wl.OldestPodStartedAt.IsZero() || podCreated.Before(wl.OldestPodStartedAt) {
				wl.OldestPodStartedAt = podCreated
			}
		}
	}

	result := make([]rawWorkload, 0, len(workloadMap))
	for _, wl := range workloadMap {
		result = append(result, *wl)
	}
	return result, podToWorkload, nodeToPool, nil
}

// allocateCosts distribui o custo do cluster proporcionalmente entre os workloads.
// Fórmula: costShare = ((cpuFraction + memFraction) / 2) × clusterMonthlyCostUSD
func allocateCosts(
	workloads []rawWorkload,
	cap clusterCapacity,
	clusterCostUSD float64,
	rate float64,
) []FinOpsWorkload {
	result := make([]FinOpsWorkload, 0, len(workloads))

	for _, wl := range workloads {
		var cpuFraction, memFraction float64
		if cap.CPUMillicores > 0 {
			cpuFraction = wl.CPURequestMillis / float64(cap.CPUMillicores)
		}
		if cap.MemoryMi > 0 {
			memFraction = wl.MemRequestMi / float64(cap.MemoryMi)
		}

		avgFraction := (cpuFraction + memFraction) / 2
		costShareUSD := avgFraction * clusterCostUSD

		// Custo por pod (para cenários HPA)
		podCount := float64(wl.Pods)
		if podCount == 0 {
			podCount = 1
		}
		podCostUSD := costShareUSD / podCount

		// Normaliza replicas HPA: se sem HPA, HPACurrent = pods
		hpaCurrent := wl.HPACurrent
		if hpaCurrent == 0 {
			hpaCurrent = wl.Pods
		}
		hpaMin := wl.HPAMin
		if hpaMin == 0 {
			hpaMin = wl.Pods
		}
		hpaMax := wl.HPAMax
		if hpaMax == 0 {
			hpaMax = wl.Pods
		}

		// Request/limit do FinOpsWorkload são POR POD (contrato do modelo e do frontend, que
		// multiplica por `pods` quando precisa do total). rawWorkload acumula a SOMA de todos os
		// pods — necessária só pra fração de custo acima — então divide aqui. Sem isso, o request
		// (total) era comparado com o uso/recomendado (P95 de UM pod): workload com 70 pods
		// aparecia com ~99% de desperdício (frete-hub: R$ 10.846 de custo, "R$ 10.758 de economia").
		fw := FinOpsWorkload{
			Namespace:         wl.Namespace,
			Workload:          wl.Workload,
			Pods:              wl.Pods,
			CPURequestMillis:  round2(wl.CPURequestMillis / podCount),
			MemRequestMi:      round2(wl.MemRequestMi / podCount),
			CPULimitMillis:    round2(wl.CPULimitMillis / podCount),
			MemLimitMi:        round2(wl.MemLimitMi / podCount),
			NodePool:          dominantPool(wl.PoolPodCounts),
			NodeName:          dominantPool(wl.NodePodCounts),
			CostShareUSD:      round2(costShareUSD),
			CostShareBRL:      round2(costShareUSD * rate),
			HPAMin:            hpaMin,
			HPAMax:            hpaMax,
			HPACurrent:        hpaCurrent,
			HPACostMinBRL:     round2(podCostUSD * float64(hpaMin) * rate),
			HPACostMaxBRL:     round2(podCostUSD * float64(hpaMax) * rate),
			HPACostCurrentBRL: round2(podCostUSD * float64(hpaCurrent) * rate),
			Verdict:           determineVerdict(wl),
		}
		if !wl.OldestPodStartedAt.IsZero() {
			started := wl.OldestPodStartedAt
			fw.OldestPodStartedAt = &started
		}
		result = append(result, fw)
	}

	// Ordenar por maior custo primeiro
	sort.Slice(result, func(i, j int) bool {
		return result[i].CostShareBRL > result[j].CostShareBRL
	})

	// Segunda passagem: identificar workloads caros sem HPA (fixed_high_cost)
	if len(result) > 0 {
		var totalCostBRL float64
		for _, wl := range result {
			totalCostBRL += wl.CostShareBRL
		}
		avgCostBRL := totalCostBRL / float64(len(result))

		// Mapa: ns/workload → HPAMax original (0 = sem HPA)
		rawMap := make(map[string]int, len(workloads))
		for _, rw := range workloads {
			rawMap[rw.Namespace+"/"+rw.Workload] = rw.HPAMax
		}

		for i := range result {
			wl := &result[i]
			origHPAMax := rawMap[wl.Namespace+"/"+wl.Workload]
			if origHPAMax == 0 && wl.Pods > 3 && wl.CostShareBRL > avgCostBRL*2 && wl.Verdict == "ok" {
				wl.Verdict = "fixed_high_cost"
			}
		}
	}

	return result
}

// dominantPool retorna a chave com mais pods do workload (best-effort — cobre o caso raro de um
// workload com pods espalhados por mais de uma chave, escolhendo a predominante). Genérica o
// bastante pra ser usada tanto com PoolPodCounts (→ NodePool) quanto NodePodCounts (→ NodeName).
// Vazia se nenhum pod resolveu pra uma chave conhecida.
func dominantPool(counts map[string]int) string {
	best, bestCount := "", 0
	for pool, n := range counts {
		if n > bestCount {
			best, bestCount = pool, n
		}
	}
	return best
}

// aggregateNamespaces agrupa workloads por namespace e soma os custos
func aggregateNamespaces(workloads []FinOpsWorkload) []FinOpsNamespace {
	nsMap := make(map[string]*FinOpsNamespace)
	for _, wl := range workloads {
		if _, ok := nsMap[wl.Namespace]; !ok {
			nsMap[wl.Namespace] = &FinOpsNamespace{Namespace: wl.Namespace}
		}
		ns := nsMap[wl.Namespace]
		ns.WorkloadCount++
		ns.MonthlyCostUSD = round2(ns.MonthlyCostUSD + wl.CostShareUSD)
		ns.MonthlyCostBRL = round2(ns.MonthlyCostBRL + wl.CostShareBRL)
	}

	result := make([]FinOpsNamespace, 0, len(nsMap))
	for _, ns := range nsMap {
		result = append(result, *ns)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].MonthlyCostBRL > result[j].MonthlyCostBRL
	})
	return result
}

// buildSummary consolida os totais do relatório
func buildSummary(workloads []FinOpsWorkload, namespaces []FinOpsNamespace, clusterCostUSD, rate float64) FinOpsSummary {
	s := FinOpsSummary{
		TotalMonthlyCostUSD: round2(clusterCostUSD),
		TotalMonthlyCostBRL: round2(clusterCostUSD * rate),
		WorkloadsAnalyzed:   len(workloads),
	}

	if len(namespaces) > 0 {
		s.TopNamespace = namespaces[0].Namespace
	}

	for _, wl := range workloads {
		if wl.MetricsSource != "" {
			s.MetricsWorkloadsEnriched++
		}
		switch wl.Verdict {
		case "superprovisioned":
			s.SuperprovisionedCount++
		case "oom_risk":
			s.OOMRiskCount++
		case "no_request":
			s.NoRequestCount++
		case "hpa_removable":
			s.HPARemovableCount++
		case "fixed_high_cost":
			s.FixedHighCostCount++
		case "sem_dados":
			s.NoDataCount++
		}
		if wl.HPACostMinBRL < wl.HPACostCurrentBRL {
			s.HPASavingsIfMinBRL = round2(s.HPASavingsIfMinBRL + (wl.HPACostCurrentBRL - wl.HPACostMinBRL))
		}
		if wl.WasteBRL > 0 {
			s.PotentialSavingsBRL = round2(s.PotentialSavingsBRL + wl.WasteBRL)
		}
	}

	return s
}

// ResolveWorkload determina o nome do workload dono do pod.
// Pod → ReplicaSet → Deployment (via rsOwner map)
// Pod → StatefulSet / DaemonSet / Job (direto do ownerRef)
// Exportada (era resolveWorkload) pra ser reaproveitada por internal/web/handlers
// (GetWorkloadHistory, finops_rightsizing.go) sem duplicar a lógica de owner chain.
func ResolveWorkload(pod *corev1.Pod, rsOwner map[string]string) string {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			if dep, ok := rsOwner[pod.Namespace+"/"+ref.Name]; ok {
				return dep
			}
			// RS sem Deployment — retorna RS sem hash final
			return stripHash(ref.Name)
		case "StatefulSet", "DaemonSet", "Job":
			return ref.Name
		}
	}
	// Pod avulso — usar nome sem hash
	return stripHash(pod.Name)
}

// stripHash remove o último segmento de hash de um nome K8s
// Ex: "my-app-7d9f8b6c5" → "my-app"
func stripHash(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) <= 1 {
		return name
	}
	last := parts[len(parts)-1]
	// Hash K8s tem 5-10 chars alfanuméricos lowercase
	if len(last) >= 5 && len(last) <= 10 && isAlphanumericLower(last) {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	return name
}

func isAlphanumericLower(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// determineVerdict atribui um veredicto ao workload baseado em requests e HPA.
// Na Fase 6 (Prometheus), este veredicto será enriquecido com dados de uso real.
func determineVerdict(wl rawWorkload) string {
	if wl.CPURequestMillis == 0 && wl.MemRequestMi == 0 {
		return "no_request"
	}
	// Sem HPA: não há como inferir superprovisionamento sem Prometheus
	if wl.HPAMax == 0 || wl.HPAMin == wl.HPAMax {
		return "ok"
	}
	// HPA com min muito baixo em relação ao current → potencial economia
	if wl.HPACurrent > 0 && wl.HPAMin > 0 {
		ratio := float64(wl.HPACurrent) / float64(wl.HPAMax)
		if ratio < 0.35 {
			// Rodando consistentemente abaixo de 35% do máximo → pode reduzir minReplicas
			return "superprovisioned"
		}
	}
	return "ok"
}

// reclassifyNoDataVerdicts corrige um bug real: `determineVerdict` (baseado só em HPA, chamado
// antes de qualquer enriquecimento) devolve "ok" tanto pro caso "sem HPA, nada a avaliar" quanto
// — depois de nenhum enricher rodar pra este workload (Prometheus/DT indisponíveis, VPN instável
// durante o scan, workload sem pods rodando na janela) — pro caso "nunca chegamos a checar uso
// real". As duas situações ficavam indistinguíveis na API/UI: um workload sem NENHUM dado
// parecia "verificado eficiente", com o mesmo badge verde de um workload genuinamente medido e
// OK. `wl.MetricsSource == ""` é o sinal confiável de que nenhum enricher tocou este workload
// (verdictFromPrometheus SEMPRE seta MetricsSource junto, nos dois enrichers — nunca roda sem
// isso) — só reclassifica o "ok" genérico, nunca "no_request"/"superprovisioned" (conclusões já
// válidas só com dado de HPA, sem depender de métrica de uso nenhuma). Chamada depois que
// dtEnricher/enricher já rodaram (ver BuildReport), antes de agregar/montar o summary.
func reclassifyNoDataVerdicts(workloads []FinOpsWorkload) {
	for i := range workloads {
		if workloads[i].MetricsSource == "" && workloads[i].Verdict == "ok" {
			workloads[i].Verdict = "sem_dados"
		}
	}
}

// sumCPURequestsMillis soma os CPU requests de todos os containers de um pod em millicores
func sumCPURequestsMillis(pod *corev1.Pod) float64 {
	var total float64
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			total += float64(req.MilliValue())
		}
	}
	return total
}

// sumMemRequestsMi soma os Memory requests de todos os containers de um pod em MiB
func sumMemRequestsMi(pod *corev1.Pod) float64 {
	var total float64
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			total += float64(req.Value()) / (1024 * 1024)
		}
	}
	return total
}

// sumCPULimitsMillis soma os CPU limits de todos os containers de um pod em millicores
func sumCPULimitsMillis(pod *corev1.Pod) float64 {
	var total float64
	for _, c := range pod.Spec.Containers {
		if lim, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
			total += float64(lim.MilliValue())
		}
	}
	return total
}

// sumMemLimitsMi soma os Memory limits de todos os containers de um pod em MiB
func sumMemLimitsMi(pod *corev1.Pod) float64 {
	var total float64
	for _, c := range pod.Spec.Containers {
		if lim, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			total += float64(lim.Value()) / (1024 * 1024)
		}
	}
	return total
}

// buildNamespaceFilter cria um set para lookup O(1). Nil = sem filtro.
func buildNamespaceFilter(namespaces []string) map[string]struct{} {
	if len(namespaces) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		m[ns] = struct{}{}
	}
	return m
}

// shouldIncludeNamespace decide se o namespace deve ser incluído na análise.
// Exclui namespaces de sistema quando sem filtro explícito.
var systemNamespaces = map[string]struct{}{
	"kube-system":     {},
	"kube-public":     {},
	"kube-node-lease": {},
}

func shouldIncludeNamespace(ns string, filter map[string]struct{}) bool {
	if filter != nil {
		_, ok := filter[ns]
		return ok
	}
	_, isSystem := systemNamespaces[ns]
	return !isSystem
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
