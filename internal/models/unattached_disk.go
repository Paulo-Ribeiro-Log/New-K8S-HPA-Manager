package models

// UnattachedDisk é um disco (Azure Managed Disk / GCP Persistent Disk / AWS EBS) que existe na
// conta do cloud mas não está atachado a nenhuma VM — ou seja, gera custo de armazenamento sem
// estar em uso por nenhuma máquina. Só descrição bruta da descoberta (sem custo nem veredito):
// preço e classificação são responsabilidade de internal/finops.
type UnattachedDisk struct {
	Provider string `json:"provider"` // "azure" | "gcp" | "aws"
	ID       string `json:"id"`       // Azure: resource ID · GCP: selfLink/nome · AWS: vol-xxxx
	Name     string `json:"name"`     // Azure/GCP: nome do disco · AWS: tag Name, senão o VolumeId

	// Localização — Azure: ResourceGroup + Location (região) · GCP: Zone (ou região, disco
	// regional) · AWS: AvailabilityZone (Zone) + Location (região).
	ResourceGroup string `json:"resource_group,omitempty"`
	Location      string `json:"location,omitempty"`
	Zone          string `json:"zone,omitempty"`

	SizeGB    float64 `json:"size_gb"`
	DiskType  string  `json:"disk_type"`            // valor cru do cloud: "Premium_LRS" | "pd-ssd" | "gp3" ...
	DiskState string  `json:"disk_state,omitempty"` // valor cru: Unattached | READY | available

	// Performance PROVISIONADA (0 = não informada/não aplicável). Só importa pros tipos que cobram
	// por IOPS/throughput além da capacidade: Azure Ultra e Premium SSD v2, GCP Hyperdisk.
	ProvisionedIOPS float64 `json:"provisioned_iops,omitempty"`
	ProvisionedMBps float64 `json:"provisioned_mbps,omitempty"`

	CreatedAt       string `json:"created_at,omitempty"`       // RFC3339, quando o cloud informa
	UnattachedSince string `json:"unattached_since,omitempty"` // RFC3339 — só quando o cloud informa (Azure lastOwnershipUpdateTime, GCP lastDetachTimestamp); AWS não expõe

	Tags map[string]string `json:"tags,omitempty"` // Azure tags · GCP labels · AWS tags

	// Origem Kubernetes, extraída de tags/labels/description que o driver de storage grava ao
	// provisionar dinamicamente um PV — vazio pra disco criado fora do K8s.
	K8sPVName       string `json:"k8s_pv_name,omitempty"`
	K8sPVCName      string `json:"k8s_pvc_name,omitempty"`
	K8sPVCNamespace string `json:"k8s_pvc_namespace,omitempty"`
	// K8sClusterHint é a melhor pista de QUAL cluster provisionou o disco (AWS: tag
	// kubernetes.io/cluster/<nome>; Azure: nome do node resource group MC_<rg>_<cluster>_<região>).
	// Só uma pista — nunca prova.
	K8sClusterHint string `json:"k8s_cluster_hint,omitempty"`
}

// IsK8sProvisioned diz se alguma marca de provisionamento dinâmico do K8s foi encontrada.
func (d UnattachedDisk) IsK8sProvisioned() bool {
	return d.K8sPVName != "" || d.K8sPVCName != "" || d.K8sClusterHint != ""
}
