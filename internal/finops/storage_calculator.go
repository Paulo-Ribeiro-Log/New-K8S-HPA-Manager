package finops

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// scInfo armazena dados relevantes de uma StorageClass para cálculo de custo
type scInfo struct {
	Provisioner string
	SkuName     string // parâmetro skuName (ou storageaccounttype), se presente
}

// StorageCalculator enumera PVCs do cluster e calcula seus custos mensais
type StorageCalculator struct {
	diskPricer    *DiskPricer
	prometheusAPI v1.API // opcional — usado para obter uso real de Blob/Files
}

// NewStorageCalculator cria um StorageCalculator com DiskPricer injetado
func NewStorageCalculator(diskPricer *DiskPricer) *StorageCalculator {
	return &StorageCalculator{diskPricer: diskPricer}
}

// WithPrometheus adiciona um cliente Prometheus para consultar uso real de Blob/Files.
func (s *StorageCalculator) WithPrometheus(prometheusURL string) {
	if prometheusURL == "" {
		return
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		Timeout: 10 * time.Second,
	}
	apiClient, err := api.NewClient(api.Config{
		Address:      prometheusURL,
		RoundTripper: httpClient.Transport,
	})
	if err != nil {
		log.Warn().Err(err).Str("url", prometheusURL).Msg("FinOps storage: falha ao criar cliente Prometheus para uso de volume")
		return
	}
	s.prometheusAPI = v1.NewAPI(apiClient)
}

// queryVolumeUsedBytes consulta o uso real de um PVC via kubelet_volume_stats_used_bytes.
// Retorna (usedGB, true) se disponível, (0, false) caso contrário.
func (s *StorageCalculator) queryVolumeUsedBytes(ctx context.Context, namespace, pvcName string) (float64, bool) {
	if s.prometheusAPI == nil {
		return 0, false
	}
	query := fmt.Sprintf(`kubelet_volume_stats_used_bytes{namespace=%q,persistentvolumeclaim=%q}`, namespace, pvcName)
	result, _, err := s.prometheusAPI.Query(ctx, query, time.Now())
	if err != nil {
		log.Debug().Err(err).Str("pvc", namespace+"/"+pvcName).Msg("FinOps storage: falha ao consultar uso de volume no Prometheus")
		return 0, false
	}
	vec, ok := result.(model.Vector)
	if !ok || len(vec) == 0 {
		return 0, false
	}
	usedBytes := float64(vec[0].Value)
	if usedBytes <= 0 {
		return 0, false
	}
	return usedBytes / (1024 * 1024 * 1024), true
}

// Calculate coleta todos os PVCs do cluster, calcula o custo de cada um e retorna
// a lista de PVCCostItem e o StorageSummary agregado.
// rate: taxa de câmbio USD→BRL (mesma usada no relatório principal).
// Erro não fatal: falhas parciais retornam dados disponíveis + log.Warn.
func (s *StorageCalculator) Calculate(
	ctx context.Context,
	client kubernetes.Interface,
	rate float64,
) ([]PVCCostItem, StorageSummary, error) {
	region := defaultPricingRegion

	// 1. StorageClasses → mapa name → scInfo (provisioner + skuName)
	scMap, err := s.listStorageClasses(ctx, client)
	if err != nil {
		log.Warn().Err(err).Msg("FinOps storage: falha ao listar StorageClasses, usando mapeamento por nome")
		scMap = make(map[string]scInfo)
	}

	// 2. PVC → workload correlation via pods Running
	pvcToWorkload, err := s.correlateToWorkloads(ctx, client)
	if err != nil {
		log.Warn().Err(err).Msg("FinOps storage: falha ao correlacionar PVCs, todos marcados como orfãos")
		pvcToWorkload = make(map[string]string)
	}

	// 3. Listar PVCs em todos os namespaces
	pvcList, err := client.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, StorageSummary{}, fmt.Errorf("listar PVCs: %w", err)
	}

	// 4. Listar PVs para obter capacidade real provisionada (best-effort)
	pvMap := make(map[string]corev1.PersistentVolume)
	if pvList, pvErr := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{}); pvErr != nil {
		log.Warn().Err(pvErr).Msg("FinOps storage: falha ao listar PVs, usando capacidade requisitada")
	} else {
		for _, pv := range pvList.Items {
			pvMap[pv.Name] = pv
		}
	}

	// 5. Calcular custo de cada PVC
	items := make([]PVCCostItem, 0, len(pvcList.Items))
	for _, pvc := range pvcList.Items {
		item := s.calculatePVCCost(ctx, pvc, pvMap, scMap, pvcToWorkload, rate, region)
		items = append(items, item)
	}

	summary := buildStorageSummary(items)
	return items, summary, nil
}

// OSDiskForNodePool detecta o tamanho do disco OS dos nodes de um pool AKS.
// Retorna (sizeGB, source) onde source é "label" ou "default".
func (s *StorageCalculator) OSDiskForNodePool(
	ctx context.Context,
	client kubernetes.Interface,
	poolName string,
) (int, string) {
	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "kubernetes.azure.com/agentpool=" + poolName,
	})
	if err != nil || len(nodeList.Items) == 0 {
		return 128, "default"
	}

	// Todos os nodes do pool têm o mesmo tamanho de OS disk — verificar no primeiro
	if sizeStr, ok := nodeList.Items[0].Labels["kubernetes.azure.com/os-disk-size-gb"]; ok {
		var size int
		if _, err := fmt.Sscanf(sizeStr, "%d", &size); err == nil && size > 0 {
			return size, "label"
		}
	}

	return 128, "default"
}

// groupPVCsByWorkload cria um mapa workloadRef → []PVCCostItem para enriquecer FinOpsWorkloads
func groupPVCsByWorkload(items []PVCCostItem) map[string][]PVCCostItem {
	m := make(map[string][]PVCCostItem)
	for _, item := range items {
		if item.WorkloadRef != "" {
			m[item.WorkloadRef] = append(m[item.WorkloadRef], item)
		}
	}
	return m
}

// ── internals ─────────────────────────────────────────────────────────────────

func (s *StorageCalculator) calculatePVCCost(
	ctx context.Context,
	pvc corev1.PersistentVolumeClaim,
	pvMap map[string]corev1.PersistentVolume,
	scMap map[string]scInfo,
	pvcToWorkload map[string]string,
	rate float64,
	region string,
) PVCCostItem {
	item := PVCCostItem{
		Namespace:     pvc.Namespace,
		Name:          pvc.Name,
		BoundPV:       pvc.Spec.VolumeName,
		StorageClass:  pvcStorageClass(pvc),
		Phase:         string(pvc.Status.Phase),
		ReclaimPolicy: "Delete",
	}

	// Capacidade: preferir PV provisionado (reflete tier real) sobre o requisitado.
	// Azure Blob NFS e Azure Files definem capacidade placeholder gigante (ex: 5242880Gi ≈ 5 PB)
	// tanto no PV quanto na request do PVC, pois não têm limite físico real. Acima de 50 TB
	// o valor é tratado como placeholder e a capacidade fica em 0 (custo não estimável).
	const maxReasonableGB = 50 * 1024.0 // 50 TB
	if pv, ok := pvMap[pvc.Spec.VolumeName]; ok {
		if capQ, cok := pv.Spec.Capacity[corev1.ResourceStorage]; cok {
			cap := float64(capQ.Value()) / (1024 * 1024 * 1024)
			if cap <= maxReasonableGB {
				item.CapacityGB = cap
			}
		}
		item.ReclaimPolicy = string(pv.Spec.PersistentVolumeReclaimPolicy)
	}
	if item.CapacityGB == 0 {
		if req, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			cap := float64(req.Value()) / (1024 * 1024 * 1024)
			if cap <= maxReasonableGB {
				item.CapacityGB = cap
			}
		}
	}
	// Blob/Files com capacidade placeholder (> 50 TB) → usar uso real via kubelet_volume_stats_used_bytes
	if item.CapacityGB == 0 {
		if usedGB, ok := s.queryVolumeUsedBytes(ctx, pvc.Namespace, pvc.Name); ok {
			item.CapacityGB = usedGB
			item.PriceSource = "prometheus_usage"
		}
		// Se Prometheus também não tem dados → CapacityGB = 0, custo = 0 (não estimável)
	}

	// Resolver tipo Azure a partir da StorageClass
	sc := scMap[item.StorageClass]
	azureType, method := MapStorageClassToAzureType(item.StorageClass, sc.Provisioner, sc.SkuName)
	item.AzureDiskType = azureType

	// Calcular custo conforme o tipo
	if _, isManagedDisk := managedDiskTiers[azureType]; isManagedDisk {
		tier := ResolveManagedDiskTier(azureType, item.CapacityGB)
		item.AzureDiskTier = tier
		price, src, err := s.diskPricer.GetDiskPrice(azureType, tier, region)
		if err != nil {
			log.Debug().Err(err).
				Str("pvc", pvc.Namespace+"/"+pvc.Name).
				Msg("FinOps storage: preço de disco não encontrado")
		}
		item.PricePerMonth = price
		item.PriceSource = src
		item.MonthlyCostUSD = round2(price)
	} else {
		// Azure Files ou Blob: custo = CapacityGB × preço/GB/mês
		pricePerGB, src, err := s.diskPricer.GetFilesPricePerGB(azureType, region)
		if err != nil {
			log.Debug().Err(err).
				Str("pvc", pvc.Namespace+"/"+pvc.Name).
				Msg("FinOps storage: preço de Files/Blob não encontrado")
		}
		item.PricePerMonth = pricePerGB
		item.PriceSource = src
		item.MonthlyCostUSD = round2(item.CapacityGB * pricePerGB)
	}
	item.MonthlyCostBRL = round2(item.MonthlyCostUSD * rate)
	log.Debug().
		Str("pvc", pvc.Namespace+"/"+pvc.Name).
		Str("storage_class", item.StorageClass).
		Str("provisioner", sc.Provisioner).
		Str("azure_type", azureType).
		Str("method", method).
		Float64("capacity_gb", item.CapacityGB).
		Float64("cost_brl", item.MonthlyCostBRL).
		Msg("FinOps storage: classificação de PVC")

	// Correlação com workload
	pvcKey := pvc.Namespace + "/" + pvc.Name
	if ref, ok := pvcToWorkload[pvcKey]; ok {
		item.WorkloadRef = ref
	} else {
		item.IsOrphaned = true
	}

	return item
}

// listStorageClasses retorna um mapa de nome → scInfo com provisioner e skuName
func (s *StorageCalculator) listStorageClasses(
	ctx context.Context,
	client kubernetes.Interface,
) (map[string]scInfo, error) {
	scList, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make(map[string]scInfo, len(scList.Items))
	for _, sc := range scList.Items {
		info := scInfo{
			Provisioner: sc.Provisioner,
			SkuName:     sc.Parameters["skuName"],
		}
		// Alguns CSI drivers usam "storageaccounttype" em vez de "skuName"
		if info.SkuName == "" {
			info.SkuName = sc.Parameters["storageaccounttype"]
		}
		result[sc.Name] = info
	}
	return result, nil
}

// correlateToWorkloads constrói o mapa pvcKey("ns/name") → workloadRef("ns/workload")
// percorrendo os Pods Running e suas referências a PVCs.
func (s *StorageCalculator) correlateToWorkloads(
	ctx context.Context,
	client kubernetes.Interface,
) (map[string]string, error) {
	// Resolver RS → Deployment (mesmo padrão do calculator principal)
	rsList, err := client.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	rsOwner := make(map[string]string)
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" {
				rsOwner[rs.Namespace+"/"+rs.Name] = ref.Name
			}
		}
	}

	podList, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for i := range podList.Items {
		pod := &podList.Items[i]
		workload := resolveWorkload(pod, rsOwner)
		workloadRef := pod.Namespace + "/" + workload

		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim == nil {
				continue
			}
			pvcKey := pod.Namespace + "/" + vol.PersistentVolumeClaim.ClaimName
			// Manter o primeiro workload encontrado (PVC montado por múltiplos workloads é incomum)
			if _, exists := result[pvcKey]; !exists {
				result[pvcKey] = workloadRef
			}
		}
	}
	return result, nil
}

// buildStorageSummary agrega PVCCostItems em um StorageSummary
func buildStorageSummary(items []PVCCostItem) StorageSummary {
	s := StorageSummary{
		ByNamespace: make(map[string]float64),
	}

	scBreakdown := make(map[string]*StorageClassBreakdown)

	for _, item := range items {
		s.PVCCount++
		s.TotalCapacityGB = round2(s.TotalCapacityGB + item.CapacityGB)
		s.TotalMonthlyCostUSD = round2(s.TotalMonthlyCostUSD + item.MonthlyCostUSD)
		s.TotalMonthlyCostBRL = round2(s.TotalMonthlyCostBRL + item.MonthlyCostBRL)

		if strings.EqualFold(item.Phase, "Bound") {
			s.BoundPVCCount++
		}
		if item.IsOrphaned {
			s.OrphanedPVCCount++
			s.OrphanedCostBRL = round2(s.OrphanedCostBRL + item.MonthlyCostBRL)
		}

		s.ByNamespace[item.Namespace] = round2(s.ByNamespace[item.Namespace] + item.MonthlyCostBRL)

		key := item.StorageClass
		if _, ok := scBreakdown[key]; !ok {
			scBreakdown[key] = &StorageClassBreakdown{
				StorageClass: key,
				AzureType:    item.AzureDiskType,
			}
		}
		bd := scBreakdown[key]
		bd.PVCCount++
		bd.TotalGB = round2(bd.TotalGB + item.CapacityGB)
		bd.MonthlyCostBRL = round2(bd.MonthlyCostBRL + item.MonthlyCostBRL)
	}

	for _, bd := range scBreakdown {
		s.ByStorageClass = append(s.ByStorageClass, *bd)
	}
	sort.Slice(s.ByStorageClass, func(i, j int) bool {
		return s.ByStorageClass[i].MonthlyCostBRL > s.ByStorageClass[j].MonthlyCostBRL
	})

	return s
}

// pvcStorageClass retorna a StorageClass do PVC, com fallback para "default"
func pvcStorageClass(pvc corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		return *pvc.Spec.StorageClassName
	}
	return "default"
}
