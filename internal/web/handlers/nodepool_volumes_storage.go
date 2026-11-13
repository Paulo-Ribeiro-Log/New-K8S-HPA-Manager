package handlers

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Atributos do storage de cada volume, lidos só do Kubernetes (parameters da StorageClass +
// volumeAttributes do PV): SKU e redundância (LRS/ZRS/GRS/GZRS…), tier de acesso, protocolo e conta.
// O que só existe na nuvem (kind da conta StorageV2/FileStorage, tier real da conta) NÃO é
// consultado aqui — a tela oferece o comando para o usuário rodar.

// StorageInfo descreve o backend de um volume. Campos vazios = o cluster não informa.
type StorageInfo struct {
	Driver         string            `json:"driver,omitempty"`
	SKU            string            `json:"sku,omitempty"`         // Premium_LRS, Standard_ZRS, gp3, pd-ssd…
	Performance    string            `json:"performance,omitempty"` // Premium, Standard, StandardSSD, Ultra…
	Redundancy     string            `json:"redundancy,omitempty"`  // LRS, ZRS, GRS, GZRS, RAGRS, RAGZRS, regional-pd
	RedundancyNote string            `json:"redundancy_note,omitempty"`
	Tier           string            `json:"tier,omitempty"` // accessTier/shareAccessTier (Hot, Cool, TransactionOptimized…)
	Protocol       string            `json:"protocol,omitempty"`
	Account        string            `json:"account,omitempty"`
	ResourceGroup  string            `json:"resource_group,omitempty"`
	Share          string            `json:"share,omitempty"`       // share (Files) ou container (Blob)
	ResourceID     string            `json:"resource_id,omitempty"` // Azure Disk: ID completo do recurso (vem do volumeHandle)
	Params         map[string]string `json:"params,omitempty"`
}

var redundancyNote = map[string]string{
	"LRS":    "3 cópias num único datacenter/zona: não sobrevive à perda da zona",
	"ZRS":    "3 cópias em 3 zonas da região",
	"GRS":    "LRS + cópia assíncrona numa região secundária",
	"GZRS":   "ZRS + cópia assíncrona numa região secundária",
	"RAGRS":  "GRS com leitura na região secundária",
	"RAGZRS": "GZRS com leitura na região secundária",
}

// splitSKU separa "Premium_LRS" em desempenho ("Premium") e redundância ("LRS"). Nomes sem sufixo
// de redundância (gp3, pd-ssd) voltam inteiros como desempenho.
func splitSKU(sku string) (perf, redundancy string) {
	i := strings.LastIndex(sku, "_")
	if i < 0 {
		return sku, ""
	}
	r := strings.ToUpper(strings.ReplaceAll(sku[i+1:], "-", ""))
	if _, ok := redundancyNote[r]; ok {
		return sku[:i], r
	}
	return sku, ""
}

// sensitiveParam: chaves que podem carregar segredo ou só referenciam um (não vão para a tela).
func sensitiveParam(k string) bool {
	l := strings.ToLower(k)
	if strings.HasPrefix(l, "csi.storage.k8s.io/") {
		return true
	}
	for _, w := range []string{"secret", "password", "token", "sas", "accountkey", "credential"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// storageInfoFor combina os parameters da StorageClass com os volumeAttributes do PV (estes valem
// mais: refletem o que foi de fato provisionado).
func storageInfoFor(pv corev1.PersistentVolume, scParams map[string]string) *StorageInfo {
	merged := map[string]string{}
	for k, v := range scParams {
		merged[strings.ToLower(k)] = v
	}
	sp := pv.Spec
	if sp.CSI != nil {
		for k, v := range sp.CSI.VolumeAttributes {
			merged[strings.ToLower(k)] = v
		}
	}
	driver := pvDriver(pv)
	if driver == "" && sp.NFS == nil {
		return nil
	}
	info := &StorageInfo{Driver: driver, Params: map[string]string{}}
	info.SKU = getCI(merged, "skuname", "storageaccounttype", "type")
	info.Tier = getCI(merged, "accesstier", "shareaccesstier")
	info.Protocol = getCI(merged, "protocol")
	info.Account = getCI(merged, "storageaccount", "storageaccountname")
	info.ResourceGroup = getCI(merged, "resourcegroup")
	info.Share = getCI(merged, "sharename", "containername")
	if sp.AzureDisk != nil && info.SKU == "" && sp.AzureDisk.Kind != nil {
		info.Params["kind"] = string(*sp.AzureDisk.Kind)
	}
	switch {
	case sp.AzureDisk != nil:
		info.ResourceID = sp.AzureDisk.DataDiskURI
	case sp.CSI != nil && driver == "disk.csi.azure.com":
		info.ResourceID = sp.CSI.VolumeHandle
	case sp.CSI != nil && (driver == "file.csi.azure.com" || driver == "blob.csi.azure.com"):
		// volumeHandle: "<resource-group>#<conta>#<share|container>#..." — completa o que os atributos não trazem
		if p := strings.Split(sp.CSI.VolumeHandle, "#"); len(p) >= 3 {
			if info.ResourceGroup == "" {
				info.ResourceGroup = p[0]
			}
			if info.Account == "" {
				info.Account = p[1]
			}
			if info.Share == "" {
				info.Share = p[2]
			}
		}
	}
	if info.SKU != "" {
		info.Performance, info.Redundancy = splitSKU(info.SKU)
	}
	if strings.EqualFold(getCI(merged, "replication-type"), "regional-pd") {
		info.Redundancy = "regional-pd"
		info.RedundancyNote = "disco replicado em duas zonas"
	}
	if n, ok := redundancyNote[info.Redundancy]; ok {
		info.RedundancyNote = n
	}
	for k, v := range merged {
		if v != "" && !sensitiveParam(k) {
			info.Params[k] = v
		}
	}
	if len(info.Params) == 0 {
		info.Params = nil
	}
	if info.SKU == "" && info.Tier == "" && info.Protocol == "" && info.Account == "" && info.Params == nil {
		return nil
	}
	return info
}

// applyStorageInfo preenche Storage de cada item que tem PV. scParams: parameters por StorageClass.
func applyStorageInfo(items []VolumeItem, pvs []corev1.PersistentVolume, scParams map[string]map[string]string) {
	byName := map[string]corev1.PersistentVolume{}
	for _, pv := range pvs {
		byName[pv.Name] = pv
	}
	for i := range items {
		it := &items[i]
		pv, ok := byName[it.PV]
		if !ok {
			continue
		}
		it.Storage = storageInfoFor(pv, scParams[pv.Spec.StorageClassName])
	}
}
