package handlers

import (
	"sort"
	"strconv"
	"strings"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/models"

	corev1 "k8s.io/api/core/v1"
)

// Custo dos volumes (PV/PVC) — por volume e por aplicação. Reaproveita o que o FinOps já tem:
//   - custo EFETIVO (Cost Management, só Azure, já com reserva/desconto) quando gravado para o disco;
//   - senão ESTIMATIVA por preço de tabela/API on-demand (Azure, AWS, GCP) a partir do tipo e do
//     tamanho do disco.
// NFS, Azure Files, EFS, HostPath e Local não têm preço por volume (o custo é do servidor/share).

// VolumeCostInputs é o que o FinOps fornece para precificar os volumes de um cluster.
type VolumeCostInputs struct {
	Provider  string // azure | gcp | aws
	Prices    finops.DiskPriceFuncs
	Rate      float64            // USD → BRL
	Effective map[string]float64 // nome do disco em minúsculas → R$/mês efetivo
}

// VolumeCostSource é implementado pela FinOpsHandler; NodePoolHandler só depende desta interface.
type VolumeCostSource interface {
	VolumeCostInputs(cluster string) (VolumeCostInputs, error)
}

// SetVolumeCostSource liga a visão de volumes ao FinOps (preços e custo efetivo).
func (h *NodePoolHandler) SetVolumeCostSource(src VolumeCostSource) { h.volumeCost = src }

// AppCost é o custo mensal de uma aplicação com a soma dos volumes que ela usa. Volume
// compartilhado por N aplicações entra dividido por N em cada uma.
type AppCost struct {
	Namespace     string  `json:"namespace"`
	App           string  `json:"app"` // "Kind/nome" ou "(sem aplicação identificada)"
	Volumes       int     `json:"volumes"`
	SharedVolumes int     `json:"shared_volumes"`
	CapacityBytes float64 `json:"capacity_bytes"`
	CostBRL       float64 `json:"cost_brl_month"`
	Unpriced      int     `json:"unpriced"` // volumes dela sem preço (NFS/Files, tipo desconhecido…)
}

const noAppLabel = "(sem aplicação identificada)"

// getCI busca a primeira chave existente, sem diferenciar maiúsculas (volumeAttributes vêm em
// minúsculas; parameters das StorageClasses misturam os dois).
func getCI(m map[string]string, keys ...string) string {
	for _, k := range keys {
		for mk, v := range m {
			if strings.EqualFold(mk, k) && v != "" {
				return v
			}
		}
	}
	return ""
}

// pvRegion deriva a região do PV pelas labels de topologia (Azure: zona "brazilsouth-1").
func pvRegion(pv corev1.PersistentVolume) string {
	if r := pv.Labels["topology.kubernetes.io/region"]; r != "" {
		return r
	}
	zone := pv.Labels["topology.kubernetes.io/zone"]
	if zone == "" {
		zone = pv.Labels["topology.disk.csi.azure.com/zone"]
	}
	if zone == "" && pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
	outer:
		for _, t := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, e := range t.MatchExpressions {
				if (e.Key == "topology.kubernetes.io/zone" || e.Key == "topology.disk.csi.azure.com/zone") && len(e.Values) > 0 {
					zone = e.Values[0]
					break outer
				}
			}
		}
	}
	if i := strings.LastIndex(zone, "-"); i > 0 { // "brazilsouth-1" → "brazilsouth"
		if _, err := strconv.Atoi(zone[i+1:]); err == nil {
			return zone[:i]
		}
	}
	return ""
}

// pvDisk monta o disco do cloud que sustenta o PV. ok=false: o PV não é um disco de bloco
// precificável por tipo/tamanho (NFS, Files, EFS, HostPath, Local…).
func pvDisk(provider string, pv corev1.PersistentVolume, scParams map[string]string, sizeGB float64) (disk models.UnattachedDisk, name string, ok bool) {
	sp := pv.Spec
	var attrs map[string]string
	driver := ""
	if sp.CSI != nil {
		attrs, driver = sp.CSI.VolumeAttributes, sp.CSI.Driver
	}
	disk = models.UnattachedDisk{Provider: provider, SizeGB: sizeGB, Location: pvRegion(pv)}
	switch provider {
	case "azure":
		switch {
		case driver == "disk.csi.azure.com":
			h := sp.CSI.VolumeHandle
			name = h[strings.LastIndex(h, "/")+1:]
		case sp.AzureDisk != nil:
			name = sp.AzureDisk.DiskName
		default:
			return disk, "", false
		}
		disk.DiskType = getCI(attrs, "skuname", "storageaccounttype")
		if disk.DiskType == "" {
			disk.DiskType = getCI(scParams, "skuName", "storageaccounttype")
		}
		disk.ProvisionedIOPS, _ = strconv.ParseFloat(getCI(attrs, "diskiopsreadwrite"), 64)
		disk.ProvisionedMBps, _ = strconv.ParseFloat(getCI(attrs, "diskmbpsreadwrite"), 64)
	case "aws":
		if driver != "ebs.csi.aws.com" && sp.AWSElasticBlockStore == nil {
			return disk, "", false
		}
		disk.DiskType = getCI(attrs, "type")
		if disk.DiskType == "" {
			disk.DiskType = getCI(scParams, "type")
		}
	case "gcp":
		if driver != "pd.csi.storage.gke.io" && sp.GCEPersistentDisk == nil {
			return disk, "", false
		}
		disk.DiskType = getCI(attrs, "type")
		if disk.DiskType == "" {
			disk.DiskType = getCI(scParams, "type")
		}
	default:
		return disk, "", false
	}
	return disk, name, true
}

// applyVolumeCosts preenche o custo de cada item e devolve o custo por aplicação. `pvs` e
// `scParams` (parameters por StorageClass) vêm do cluster.
func applyVolumeCosts(items []VolumeItem, pvs []corev1.PersistentVolume, scParams map[string]map[string]string, in VolumeCostInputs, sum *VolumesSummary) []AppCost {
	pvByName := map[string]corev1.PersistentVolume{}
	for _, pv := range pvs {
		pvByName[pv.Name] = pv
	}
	for i := range items {
		it := &items[i]
		it.CostKind, it.CostNote = "none", "sem PV associado"
		pv, found := pvByName[it.PV]
		if it.PV == "" || !found {
			if it.Kind == "nfs" {
				it.CostNote = "NFS direto no pod: o custo é do servidor, não do volume"
			}
			sum.CostNone++
			continue
		}
		disk, diskName, isDisk := pvDisk(in.Provider, pv, scParams[it.StorageClass], it.CapacityBytes/(1024*1024*1024))
		switch {
		case !isDisk:
			it.CostNote = "sem preço por volume (NFS, Files, EFS, HostPath ou Local): o custo é do servidor/share"
			sum.CostNone++
		case in.Effective[strings.ToLower(pv.Name)] > 0 || (diskName != "" && in.Effective[strings.ToLower(diskName)] > 0):
			v := in.Effective[strings.ToLower(pv.Name)]
			if v == 0 {
				v = in.Effective[strings.ToLower(diskName)]
			}
			it.CostBRL, it.CostKind, it.CostNote = round2f(v), "effective", "custo efetivo do Cost Management (com reserva/desconto)"
			sum.CostEffective++
		case disk.DiskType == "":
			it.CostNote = "tipo do disco não identificado (nem no PV nem na StorageClass)"
			sum.CostNone++
		default:
			usd, src := finops.EstimateDiskMonthlyUSD(disk, in.Prices)
			if usd <= 0 {
				it.CostNote = "sem preço mapeado para " + disk.DiskType
				sum.CostNone++
				break
			}
			it.CostBRL, it.CostKind = round2f(usd*in.Rate), "estimate"
			it.CostNote = "estimativa on-demand (" + src + ") de " + disk.DiskType + ", sem reserva"
			sum.CostEstimate++
		}
		sum.CostBRL += it.CostBRL
		if it.Idle {
			sum.CostIdleBRL += it.CostBRL
		}
	}
	sum.CostBRL, sum.CostIdleBRL = round2f(sum.CostBRL), round2f(sum.CostIdleBRL)

	byApp := map[string]*AppCost{}
	for _, it := range items {
		owners := it.Workloads
		if len(owners) == 0 {
			owners = []string{noAppLabel}
		}
		for _, o := range owners {
			key := it.Namespace + "|" + o
			a := byApp[key]
			if a == nil {
				a = &AppCost{Namespace: it.Namespace, App: o}
				byApp[key] = a
			}
			n := float64(len(owners))
			a.Volumes++
			a.CapacityBytes += it.CapacityBytes / n
			a.CostBRL += it.CostBRL / n
			if n > 1 {
				a.SharedVolumes++
			}
			if it.CostKind == "none" && it.Kind != "nfs" {
				a.Unpriced++
			}
		}
	}
	out := make([]AppCost, 0, len(byApp))
	for _, a := range byApp {
		a.CostBRL, a.CapacityBytes = round2f(a.CostBRL), float64(int64(a.CapacityBytes))
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostBRL != out[j].CostBRL {
			return out[i].CostBRL > out[j].CostBRL
		}
		return out[i].Namespace+out[i].App < out[j].Namespace+out[j].App
	})
	return out
}

func round2f(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
