package finops

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/storage"
)

// clusterCapacity guarda a capacidade total do cluster em millicores e MiB
type clusterCapacity struct {
	CPUMillicores int64
	MemoryMi      int64
}

// rawWorkload é a agregação temporária de pods por workload antes do cálculo de custo
type rawWorkload struct {
	Namespace        string
	Workload         string
	Pods             int
	CPURequestMillis float64
	MemRequestMi     float64
	HPAMin           int
	HPAMax           int
	HPACurrent       int
}

// Calculator realiza a análise FinOps de um cluster
type Calculator struct {
	pricer   *AzurePricer
	exchange *ExchangeRateProvider
}

// NewCalculator cria um novo calculator com as dependências injetadas
func NewCalculator(pricer *AzurePricer, exchange *ExchangeRateProvider) *Calculator {
	return &Calculator{pricer: pricer, exchange: exchange}
}

// BuildReport coleta dados do cluster e monta o FinOpsReport completo.
// namespaces: filtra namespaces específicos; vazio = todos (exceto kube-system/kube-public).
func (c *Calculator) BuildReport(
	ctx context.Context,
	cluster string,
	client kubernetes.Interface,
	pools []storage.NodePoolRegistryEntry,
	namespaces []string,
) (*FinOpsReport, error) {
	rate, rateDate := c.exchange.Get()

	// 1. Calcular custo e capacidade dos node pools
	finOpsPools, capacity, clusterCostUSD, err := c.calculatePoolCosts(pools, rate)
	if err != nil {
		return nil, err
	}

	// 2. Coletar workloads do cluster (pods + HPAs)
	rawWorkloads, err := collectWorkloads(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}

	// 3. Alocar custo proporcional a cada workload
	workloads := allocateCosts(rawWorkloads, capacity, clusterCostUSD, rate)

	// 4. Agregar por namespace
	nsMap := aggregateNamespaces(workloads)

	// 5. Montar summary
	summary := buildSummary(workloads, nsMap, clusterCostUSD, rate)

	return &FinOpsReport{
		Cluster:      cluster,
		GeneratedAt:  time.Now(),
		ExchangeRate: rate,
		ExchangeDate: rateDate,
		NodePools:    finOpsPools,
		Namespaces:   nsMap,
		Workloads:    workloads,
		Summary:      summary,
	}, nil
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

		cpuCores, memGB := GetVMSpecs(p.VMSize)

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

// collectWorkloads lista pods Running e HPAs do cluster, agregando por workload (namespace/nome).
func collectWorkloads(
	ctx context.Context,
	client kubernetes.Interface,
	namespaces []string,
) ([]rawWorkload, error) {
	// 1. Listar todos os ReplicaSets para resolver RS → Deployment
	rsList, err := client.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	rsOwner := make(map[string]string) // "ns/rs-name" → deployment name
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" {
				rsOwner[rs.Namespace+"/"+rs.Name] = ref.Name
			}
		}
	}

	// 2. Listar HPAs (todos os namespaces)
	hpaList, err := client.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	// hpaMap: "ns/workload" → {min, max, current}
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
	podList, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}

	// 4. Agregar por workload
	workloadMap := make(map[string]*rawWorkload)
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !shouldIncludeNamespace(pod.Namespace, nsFilter) {
			continue
		}

		workloadName := resolveWorkload(pod, rsOwner)
		key := pod.Namespace + "/" + workloadName

		if _, ok := workloadMap[key]; !ok {
			h := hpaMap[key]
			workloadMap[key] = &rawWorkload{
				Namespace:  pod.Namespace,
				Workload:   workloadName,
				HPAMin:     h.min,
				HPAMax:     h.max,
				HPACurrent: h.current,
			}
		}

		wl := workloadMap[key]
		wl.Pods++
		wl.CPURequestMillis += sumCPURequestsMillis(pod)
		wl.MemRequestMi += sumMemRequestsMi(pod)
	}

	result := make([]rawWorkload, 0, len(workloadMap))
	for _, wl := range workloadMap {
		result = append(result, *wl)
	}
	return result, nil
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

		result = append(result, FinOpsWorkload{
			Namespace:         wl.Namespace,
			Workload:          wl.Workload,
			Pods:              wl.Pods,
			CPURequestMillis:  round2(wl.CPURequestMillis),
			MemRequestMi:      round2(wl.MemRequestMi),
			CostShareUSD:      round2(costShareUSD),
			CostShareBRL:      round2(costShareUSD * rate),
			HPAMin:            hpaMin,
			HPAMax:            hpaMax,
			HPACurrent:        hpaCurrent,
			HPACostMinBRL:     round2(podCostUSD * float64(hpaMin) * rate),
			HPACostMaxBRL:     round2(podCostUSD * float64(hpaMax) * rate),
			HPACostCurrentBRL: round2(podCostUSD * float64(hpaCurrent) * rate),
			Verdict:           determineVerdict(wl),
		})
	}

	// Ordenar por maior custo primeiro
	sort.Slice(result, func(i, j int) bool {
		return result[i].CostShareBRL > result[j].CostShareBRL
	})

	return result
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
		switch wl.Verdict {
		case "superprovisioned":
			s.SuperprovisionedCount++
		case "oom_risk":
			s.OOMRiskCount++
		case "no_request":
			s.NoRequestCount++
		}
		// Economia potencial se todos HPAs ficassem no mínimo
		if wl.HPACostMinBRL < wl.HPACostCurrentBRL {
			s.HPASavingsIfMinBRL = round2(s.HPASavingsIfMinBRL + (wl.HPACostCurrentBRL - wl.HPACostMinBRL))
		}
	}

	return s
}

// resolveWorkload determina o nome do workload dono do pod.
// Pod → ReplicaSet → Deployment (via rsOwner map)
// Pod → StatefulSet / DaemonSet / Job (direto do ownerRef)
func resolveWorkload(pod *corev1.Pod, rsOwner map[string]string) string {
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
