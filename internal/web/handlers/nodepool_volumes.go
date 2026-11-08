package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Volumes do cluster (PV/PVC) — lista todo volume persistente e o liga à aplicação que o usa:
// pods que montam o PVC → workload dono (Deployment/StatefulSet/…) → node → node pool. Também
// aponta o que costuma ser desperdício ou problema (PVC sem pod, PV órfão/Released, PVC Pending).
// Somente leitura; o uso em bytes (Prometheus) só entra a pedido (?include_usage=true).

// VolumeItem é uma linha da visão de volumes: um PVC (com o PV que o atende) ou um PV sem PVC.
type VolumeItem struct {
	Kind          string       `json:"kind"` // "pvc" | "pv" (PV sem PVC existente) | "nfs" (NFS montado direto no pod, sem PV/PVC)
	Namespace     string       `json:"namespace"`
	PVC           string       `json:"pvc,omitempty"`
	PVCStatus     string       `json:"pvc_status,omitempty"`
	PV            string       `json:"pv,omitempty"`
	PVPhase       string       `json:"pv_phase,omitempty"`
	StorageClass  string       `json:"storage_class"`
	Source        string       `json:"source,omitempty"`        // tipo do backend: NFS, Azure Disk, Azure Files, EBS, EFS…
	SourceDetail  string       `json:"source_detail,omitempty"` // NFS: servidor:/caminho
	CapacityBytes float64      `json:"capacity_bytes"`
	AccessModes   []string     `json:"access_modes"`
	ReclaimPolicy string       `json:"reclaim_policy,omitempty"`
	WorkloadKind  string       `json:"workload_kind,omitempty"` // dono principal (o primeiro de Workloads)
	Workload      string       `json:"workload,omitempty"`
	Workloads     []string     `json:"workloads"`              // TODAS as aplicações que usam o volume, "Kind/nome"
	OwnerSource   string       `json:"owner_source,omitempty"` // como o dono foi descoberto: pod | spec | statefulset | label
	AppHint       string       `json:"app_hint,omitempty"`     // nome da aplicação pelas labels do PVC, quando nenhum workload referencia o volume
	Pods          []string     `json:"pods"`
	Nodes         []string     `json:"nodes"`
	NodePools     []string     `json:"node_pools"`
	ManagedBy     string       `json:"managed_by,omitempty"`     // Flux/Argo CD/Helm que gerencia o volume ou o workload dono
	TemplateSize  string       `json:"template_size,omitempty"`  // StatefulSet: tamanho declarado no volumeClaimTemplate
	TemplateOwner string       `json:"template_owner,omitempty"` // StatefulSet dono do template
	IdleSince     string       `json:"idle_since,omitempty"`     // desde quando a ferramenta viu o volume ocioso
	IdleDays      *int         `json:"idle_days,omitempty"`
	Storage       *StorageInfo `json:"storage,omitempty"`    // SKU, redundância, tier, protocolo, conta (só do Kubernetes)
	CanExpand     *bool        `json:"can_expand,omitempty"` // StorageClass com allowVolumeExpansion (nil = não sei)
	Snapshots     int          `json:"snapshots,omitempty"`
	LastSnapshot  string       `json:"last_snapshot,omitempty"`
	CreatedAt     string       `json:"created_at,omitempty"` // criação do PVC (ou do PV, se não há PVC)
	AgeDays       *int         `json:"age_days,omitempty"`
	Zone          string       `json:"zone,omitempty"` // zona do disco (PV zonal); vazio = sem afinidade de zona
	UsedBytes     float64      `json:"used_bytes,omitempty"`
	UsagePercent  float64      `json:"usage_percentage,omitempty"`
	// Tendência (só com include_usage): crescimento médio dos últimos 7 dias e previsão de quando enche
	GrowthBytesPerDay float64  `json:"growth_bytes_per_day,omitempty"`
	DaysToFull        *int     `json:"days_to_full,omitempty"`
	UsageClass        string   `json:"usage_class,omitempty"` // critical | warning | oversized | ok
	Issues            []string `json:"issues"`
	Idle              bool     `json:"idle"` // PVC sem pod ou PV sem PVC: o disco existe e cobra sem uso
	CostBRL           float64  `json:"cost_brl_month,omitempty"`
	CostKind          string   `json:"cost_kind,omitempty"` // effective | estimate | none (só com include_cost)
	CostNote          string   `json:"cost_note,omitempty"`
}

// VolumesSummary são os totais do cluster.
type VolumesSummary struct {
	PVCs           int     `json:"pvcs"`
	PVs            int     `json:"pvs"`
	CapacityBytes  float64 `json:"capacity_bytes"`
	WithIssues     int     `json:"with_issues"`
	OrphanPVs      int     `json:"orphan_pvs"`
	UnusedPVCs     int     `json:"unused_pvcs"`
	CostBRL        float64 `json:"cost_brl_month"`
	CostIdleBRL    float64 `json:"cost_idle_brl_month"` // custo dos volumes ociosos (sem pod / sem PVC)
	CostEffective  int     `json:"cost_effective"`
	CostEstimate   int     `json:"cost_estimate"`
	CostNone       int     `json:"cost_none"`
	ResizePending  int     `json:"resize_pending"`         // expansões em andamento ou aguardando reinício do pod
	ExpandBlocked  int     `json:"expand_blocked"`         // volume cheio/enchendo em StorageClass sem expansão (com uso)
	SnapshotsKnown bool    `json:"snapshots_known"`        // a API de VolumeSnapshot respondeu
	IdleTracking   bool    `json:"idle_tracking"`          // o registro de ociosidade está ligado neste servidor
	SnapshotAPI    string  `json:"snapshot_api,omitempty"` // versão servida de snapshot.storage.k8s.io (v1, v1beta1); vazio = ausente
	WithSnapshot   int     `json:"with_snapshot"`
	IdleOld        int     `json:"idle_old"`         // volumes ociosos criados há mais de 30 dias: candidatos a limpeza
	ZoneNoNodes    int     `json:"zone_no_nodes"`    // volumes cuja zona não tem nenhum node no cluster
	ZoneSingleNode int     `json:"zone_single_node"` // volumes em uso cuja zona tem um único node
	UsageKnown     int     `json:"usage_known"`      // PVCs com métrica de uso no Prometheus
	UsageCritical  int     `json:"usage_critical"`
	UsageWarning   int     `json:"usage_warning"`
	UsageOversized int     `json:"usage_oversized"`
	NoOwner        int     `json:"no_owner"` // volumes sem nenhuma aplicação identificada
	NFS            int     `json:"nfs"`      // volumes NFS: PVs NFS + NFS montado direto em pod
}

// nodePoolLabelKeys: label do node que diz a que pool/group ele pertence (AKS, AKS antigo, EKS, GKE).
var nodePoolLabelKeys = []string{"agentpool", "kubernetes.azure.com/agentpool", "eks.amazonaws.com/nodegroup", "cloud.google.com/gke-nodepool"}

func nodePoolOf(labels map[string]string) string {
	for _, k := range nodePoolLabelKeys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

// workloadOf devolve o dono lógico do pod. Pod de ReplicaSet vira o Deployment (o nome do RS é
// <deployment>-<pod-template-hash>).
func workloadOf(pod corev1.Pod) (kind, name string) {
	for _, o := range pod.OwnerReferences {
		if o.Controller != nil && !*o.Controller {
			continue
		}
		if o.Kind == "ReplicaSet" {
			if h := pod.Labels["pod-template-hash"]; h != "" && strings.HasSuffix(o.Name, "-"+h) {
				return "Deployment", strings.TrimSuffix(o.Name, "-"+h)
			}
		}
		return o.Kind, o.Name
	}
	return "Pod", pod.Name
}

// pvSource descreve o backend do PV. NFS vem de spec.nfs ou de um driver CSI de NFS (servidor/share
// nos volumeAttributes); EFS é NFS gerenciado da AWS.
func pvSource(pv corev1.PersistentVolume) (source, detail string, isNFS bool) {
	sp := pv.Spec
	switch {
	case sp.NFS != nil:
		return "NFS", sp.NFS.Server + ":" + sp.NFS.Path, true
	case sp.CSI != nil:
		d := sp.CSI.Driver
		attrs := sp.CSI.VolumeAttributes
		switch {
		case strings.Contains(d, "nfs"):
			detail := attrs["server"]
			if sh := attrs["share"]; sh != "" {
				detail += ":" + sh
			}
			return "NFS (CSI)", detail, true
		case d == "efs.csi.aws.com":
			return "EFS (NFS)", sp.CSI.VolumeHandle, true
		case d == "file.csi.azure.com":
			return "Azure Files", attrs["shareName"], false
		case d == "disk.csi.azure.com":
			return "Azure Disk", "", false
		case d == "ebs.csi.aws.com":
			return "EBS", sp.CSI.VolumeHandle, false
		case d == "pd.csi.storage.gke.io":
			return "GCE PD", "", false
		}
		return "CSI: " + d, "", false
	case sp.AzureFile != nil:
		return "Azure Files", sp.AzureFile.ShareName, false
	case sp.AzureDisk != nil:
		return "Azure Disk", "", false
	case sp.AWSElasticBlockStore != nil:
		return "EBS", sp.AWSElasticBlockStore.VolumeID, false
	case sp.GCEPersistentDisk != nil:
		return "GCE PD", sp.GCEPersistentDisk.PDName, false
	case sp.HostPath != nil:
		return "HostPath", sp.HostPath.Path, false
	case sp.Local != nil:
		return "Local", sp.Local.Path, false
	}
	return "", "", false
}

// workloadSpec é um workload lido do cluster (Deployment, StatefulSet, DaemonSet, CronJob), com os
// volumes do pod template. Serve para achar o dono de um volume mesmo sem pod rodando (0 réplicas).
type workloadSpec struct {
	Namespace, Kind, Name string
	Volumes               []corev1.Volume
	ClaimTemplates        []string          // StatefulSet: nomes dos volumeClaimTemplates
	ClaimSizes            map[string]string // StatefulSet: tamanho declarado em cada volumeClaimTemplate
	Labels, Annotations   map[string]string // do objeto workload (detecção de Flux/Argo/Helm)
}

func specsFromCluster(deps []appsv1.Deployment, stss []appsv1.StatefulSet, dss []appsv1.DaemonSet, cjs []batchv1.CronJob) []workloadSpec {
	var out []workloadSpec
	for _, d := range deps {
		out = append(out, workloadSpec{Namespace: d.Namespace, Kind: "Deployment", Name: d.Name, Volumes: d.Spec.Template.Spec.Volumes, Labels: d.Labels, Annotations: d.Annotations})
	}
	for _, st := range stss {
		w := workloadSpec{Namespace: st.Namespace, Kind: "StatefulSet", Name: st.Name, Volumes: st.Spec.Template.Spec.Volumes, Labels: st.Labels, Annotations: st.Annotations, ClaimSizes: map[string]string{}}
		for _, t := range st.Spec.VolumeClaimTemplates {
			w.ClaimTemplates = append(w.ClaimTemplates, t.Name)
			if q, ok := t.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				w.ClaimSizes[t.Name] = q.String()
			}
		}
		out = append(out, w)
	}
	for _, d := range dss {
		out = append(out, workloadSpec{Namespace: d.Namespace, Kind: "DaemonSet", Name: d.Name, Volumes: d.Spec.Template.Spec.Volumes, Labels: d.Labels, Annotations: d.Annotations})
	}
	for _, c := range cjs {
		out = append(out, workloadSpec{Namespace: c.Namespace, Kind: "CronJob", Name: c.Name, Volumes: c.Spec.JobTemplate.Spec.Template.Spec.Volumes, Labels: c.Labels, Annotations: c.Annotations})
	}
	return out
}

// isOrdinalSuffix: "0", "12"… (o que o StatefulSet acrescenta ao nome do PVC).
func isOrdinalSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// appHintOf: nome da aplicação pelas labels do PVC/PV (padrão Kubernetes e o legado "app").
func appHintOf(labels map[string]string) string {
	for _, k := range []string{"app.kubernetes.io/name", "app.kubernetes.io/instance", "app"} {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

// volumeInput agrupa o que foi lido do cluster (separado do handler para testar sem API).
type volumeInput struct {
	PVCs  []corev1.PersistentVolumeClaim
	PVs   []corev1.PersistentVolume
	Pods  []corev1.Pod
	Nodes []corev1.Node
	Specs []workloadSpec
}

var zoneLabels = []string{"topology.kubernetes.io/zone", "failure-domain.beta.kubernetes.io/zone"}

// pvZone: zona do disco, pela label do PV ou pela nodeAffinity (CSI). Vazio para disco regional
// (várias zonas) ou sem zona (Azure usa "0" para disco não zonal).
func pvZone(pv corev1.PersistentVolume) string {
	valid := func(z string) bool { return z != "" && z != "0" && !strings.ContainsAny(z, ",_") }
	for _, k := range zoneLabels {
		if z := pv.Labels[k]; valid(z) {
			return z
		}
	}
	if na := pv.Spec.NodeAffinity; na != nil && na.Required != nil {
		for _, t := range na.Required.NodeSelectorTerms {
			for _, e := range t.MatchExpressions {
				for _, k := range zoneLabels {
					if e.Key == k && len(e.Values) == 1 && valid(e.Values[0]) {
						return e.Values[0]
					}
				}
			}
		}
	}
	return ""
}

// applyZoneIssues aponta disco zonal em zona sem node (não monta até existir um) e em zona com um
// só node (se ele cair, o pod não reagenda em outra zona).
func applyZoneIssues(items []VolumeItem, nodes []corev1.Node, sum *VolumesSummary) {
	perZone := map[string]int{}
	for _, n := range nodes {
		for _, k := range zoneLabels {
			if z := n.Labels[k]; z != "" {
				perZone[z]++
				break
			}
		}
	}
	if len(perZone) == 0 {
		return // sem label de zona nos nodes: nada a concluir
	}
	for i := range items {
		it := &items[i]
		switch c := perZone[it.Zone]; {
		case it.Zone == "":
		case c == 0:
			it.Issues = append(it.Issues, "zona "+it.Zone+" do disco sem nenhum node: o volume não monta até existir um node nela")
			sum.ZoneNoNodes++
		case c == 1 && len(it.Pods) > 0:
			it.Issues = append(it.Issues, "zona "+it.Zone+" tem um só node: se ele cair, o pod não reagenda em outra zona")
			sum.ZoneSingleNode++
		}
	}
}

func addUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func buildVolumeItems(in volumeInput) ([]VolumeItem, VolumesSummary) {
	poolByNode := map[string]string{}
	for _, n := range in.Nodes {
		poolByNode[n.Name] = nodePoolOf(n.Labels)
	}
	pvByName := map[string]corev1.PersistentVolume{}
	for _, pv := range in.PVs {
		pvByName[pv.Name] = pv
	}

	// owners: quem usa cada PVC ("<ns>/<pvc>"), em ordem de descoberta e sem repetir.
	type owner struct{ kind, name, via string }
	type mount struct {
		pods, nodes, pools []string
		owners             []owner
	}
	mounts := map[string]*mount{}
	mountOf := func(key string) *mount {
		m := mounts[key]
		if m == nil {
			m = &mount{}
			mounts[key] = m
		}
		return m
	}
	addOwner := func(m *mount, kind, name, via string) {
		for _, o := range m.owners {
			if o.kind == kind && o.name == name {
				return
			}
		}
		m.owners = append(m.owners, owner{kind, name, via})
	}
	directNFS := map[string]*VolumeItem{}
	var directOrder []string
	nfsItem := func(ns, kind, name, detail, via string) *VolumeItem {
		key := ns + "/" + kind + "/" + name + "/" + detail
		it := directNFS[key]
		if it == nil {
			it = &VolumeItem{Kind: "nfs", Namespace: ns, Source: "NFS (direto no pod)", SourceDetail: detail, WorkloadKind: kind, Workload: name, Workloads: []string{kind + "/" + name},
				OwnerSource: via, AccessModes: []string{}, Pods: []string{}, Nodes: []string{}, NodePools: []string{}, Issues: []string{}}
			directNFS[key] = it
			directOrder = append(directOrder, key)
		}
		return it
	}

	for _, pod := range in.Pods {
		kind, name := workloadOf(pod)
		for _, v := range pod.Spec.Volumes {
			if v.NFS != nil { // montado direto no pod: não há PV nem PVC, só aparece aqui
				it := nfsItem(pod.Namespace, kind, name, v.NFS.Server+":"+v.NFS.Path, "pod")
				it.Pods = addUnique(it.Pods, pod.Name)
				if pod.Spec.NodeName != "" {
					it.Nodes = addUnique(it.Nodes, pod.Spec.NodeName)
					it.NodePools = addUnique(it.NodePools, poolByNode[pod.Spec.NodeName])
				}
				continue
			}
			claim := ""
			switch {
			case v.PersistentVolumeClaim != nil:
				claim = v.PersistentVolumeClaim.ClaimName
			case v.Ephemeral != nil: // PVC gerado para o volume efêmero: <pod>-<volume>
				claim = pod.Name + "-" + v.Name
			default:
				continue
			}
			m := mountOf(pod.Namespace + "/" + claim)
			addOwner(m, kind, name, "pod")
			m.pods = addUnique(m.pods, pod.Name)
			if pod.Spec.NodeName != "" {
				m.nodes = addUnique(m.nodes, pod.Spec.NodeName)
				m.pools = addUnique(m.pools, poolByNode[pod.Spec.NodeName])
			}
		}
	}

	// Specs dos workloads: acha o dono mesmo com 0 réplicas ou entre execuções (CronJob).
	type vct struct{ ns, kind, name, prefix string }
	var vcts []vct
	for _, w := range in.Specs {
		for _, v := range w.Volumes {
			switch {
			case v.PersistentVolumeClaim != nil:
				addOwner(mountOf(w.Namespace+"/"+v.PersistentVolumeClaim.ClaimName), w.Kind, w.Name, "spec")
			case v.NFS != nil:
				nfsItem(w.Namespace, w.Kind, w.Name, v.NFS.Server+":"+v.NFS.Path, "spec")
			}
		}
		for _, t := range w.ClaimTemplates {
			vcts = append(vcts, vct{w.Namespace, w.Kind, w.Name, t + "-" + w.Name + "-"})
		}
	}
	// PVC criado por volumeClaimTemplate de StatefulSet: <template>-<sts>-<ordinal>
	vctOwner := func(ns, claim string) (kind, name string, ok bool) {
		for _, t := range vcts {
			if t.ns == ns && strings.HasPrefix(claim, t.prefix) && isOrdinalSuffix(strings.TrimPrefix(claim, t.prefix)) {
				return t.kind, t.name, true
			}
		}
		return "", "", false
	}
	// completa o dono de um PVC (existente ou já apagado) e devolve o que foi achado
	resolveOwners := func(ns, claim string) *mount {
		key := ns + "/" + claim
		if k, n, ok := vctOwner(ns, claim); ok {
			addOwner(mountOf(key), k, n, "statefulset")
		}
		return mounts[key]
	}
	fillOwners := func(it *VolumeItem, m *mount) {
		if m == nil {
			return
		}
		for _, o := range m.owners {
			it.Workloads = append(it.Workloads, o.kind+"/"+o.name)
		}
		if len(m.owners) > 0 {
			it.WorkloadKind, it.Workload, it.OwnerSource = m.owners[0].kind, m.owners[0].name, m.owners[0].via
		}
		it.Pods, it.Nodes, it.NodePools = m.pods, m.nodes, m.pools
		if it.Pods == nil {
			it.Pods, it.Nodes, it.NodePools = []string{}, []string{}, []string{}
		}
	}

	now := time.Now()
	sum := VolumesSummary{PVCs: len(in.PVCs), PVs: len(in.PVs)}
	items := make([]VolumeItem, 0, len(in.PVCs))
	claimed := map[string]bool{} // PVs atendendo algum PVC existente

	for _, pvc := range in.PVCs {
		it := VolumeItem{
			Kind: "pvc", Namespace: pvc.Namespace, PVC: pvc.Name, PVCStatus: string(pvc.Status.Phase),
			PV: pvc.Spec.VolumeName, AccessModes: []string{}, Workloads: []string{}, Pods: []string{}, Nodes: []string{}, NodePools: []string{}, Issues: []string{},
		}
		if pvc.Spec.StorageClassName != nil {
			it.StorageClass = *pvc.Spec.StorageClassName
		}
		if c, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			it.CapacityBytes = float64(c.Value())
		} else if c, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			it.CapacityBytes = float64(c.Value())
		}
		for _, m := range pvc.Spec.AccessModes {
			it.AccessModes = append(it.AccessModes, string(m))
		}
		if pv, ok := pvByName[pvc.Spec.VolumeName]; ok {
			claimed[pv.Name] = true
			it.PVPhase = string(pv.Status.Phase)
			it.Zone = pvZone(pv)
			it.ReclaimPolicy = string(pv.Spec.PersistentVolumeReclaimPolicy)
			var nfs bool
			it.Source, it.SourceDetail, nfs = pvSource(pv)
			if nfs {
				sum.NFS++
			}
			if it.StorageClass == "" {
				it.StorageClass = pv.Spec.StorageClassName
			}
		}
		fillOwners(&it, resolveOwners(pvc.Namespace, pvc.Name))
		if len(it.Workloads) == 0 {
			if it.AppHint = appHintOf(pvc.Labels); it.AppHint != "" {
				it.OwnerSource = "label"
			}
		}

		switch {
		case pvc.Status.Phase == corev1.ClaimPending:
			it.Issues = append(it.Issues, "PVC Pending (sem PV)")
		case pvc.Status.Phase == corev1.ClaimLost:
			it.Issues = append(it.Issues, "PVC Lost (PV perdido)")
		case len(it.Pods) == 0:
			it.Issues = append(it.Issues, "PVC sem pod usando")
			it.Idle = true
			sum.UnusedPVCs++
		}
		if it.ReclaimPolicy == string(corev1.PersistentVolumeReclaimRetain) && len(it.Pods) == 0 {
			it.Issues = append(it.Issues, "PV com reclaim Retain: continua cobrando se o PVC for apagado")
		}
		setAge(&it, pvc.CreationTimestamp, now)
		sum.CapacityBytes += it.CapacityBytes
		items = append(items, it)
	}

	for _, pv := range in.PVs {
		if claimed[pv.Name] {
			continue
		}
		it := VolumeItem{
			Kind: "pv", PV: pv.Name, PVPhase: string(pv.Status.Phase), StorageClass: pv.Spec.StorageClassName,
			ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy), Zone: pvZone(pv), AccessModes: []string{}, Workloads: []string{}, Pods: []string{}, Nodes: []string{}, NodePools: []string{}, Issues: []string{},
		}
		if c, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
			it.CapacityBytes = float64(c.Value())
		}
		var nfs bool
		it.Source, it.SourceDetail, nfs = pvSource(pv)
		if nfs {
			sum.NFS++
		}
		for _, m := range pv.Spec.AccessModes {
			it.AccessModes = append(it.AccessModes, string(m))
		}
		if pv.Spec.ClaimRef != nil {
			it.Namespace, it.PVC = pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name
			// o PVC sumiu, mas o spec do workload (ou o nome do PVC do StatefulSet) ainda diz quem era o dono
			fillOwners(&it, resolveOwners(it.Namespace, it.PVC))
			it.Issues = append(it.Issues, "PV "+string(pv.Status.Phase)+": o PVC "+pv.Spec.ClaimRef.Name+" não existe mais, mas o disco continua")
		} else {
			it.Issues = append(it.Issues, "PV sem PVC (disco ocioso)")
		}
		if len(it.Workloads) == 0 {
			if it.AppHint = appHintOf(pv.Labels); it.AppHint != "" {
				it.OwnerSource = "label"
			}
		}
		it.Idle = true
		setAge(&it, pv.CreationTimestamp, now)
		sum.OrphanPVs++
		sum.CapacityBytes += it.CapacityBytes
		items = append(items, it)
	}

	for _, key := range directOrder {
		items = append(items, *directNFS[key])
		sum.NFS++
	}

	applyZoneIssues(items, in.Nodes, &sum)
	flagIdleOld(items, &sum)

	for _, it := range items {
		if len(it.Workloads) == 0 {
			sum.NoOwner++
		}
		if len(it.Issues) > 0 {
			sum.WithIssues++
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		if items[i].Workload != items[j].Workload {
			return items[i].Workload < items[j].Workload
		}
		return items[i].PVC+items[i].PV < items[j].PVC+items[j].PV
	})
	return items, sum
}

// GetVolumes — GET /api/v1/nodepools/volumes?cluster=<ctx>&include_usage=true
// volumesData é o resultado de uma leitura de volumes (resposta de GET /nodepools/volumes e base da
// varredura periódica).
type volumesData struct {
	Items        []VolumeItem   `json:"items"`
	Summary      VolumesSummary `json:"summary"`
	ByApp        []AppCost      `json:"by_app"`
	UsageWarning string         `json:"usage_warning,omitempty"`
	CostWarning  string         `json:"cost_warning,omitempty"`
}

// volumesError carrega o status HTTP e o código da falha, para o handler responder igual a antes.
type volumesError struct {
	Status  int
	Code    string
	Message string
}

func (e *volumesError) Error() string { return e.Message }

// collectVolumes lê o cluster e monta a visão de volumes. ctx já deve ter o timeout.
func (h *NodePoolHandler) collectVolumes(ctx context.Context, cluster string, includeUsage, includeCost bool) (*volumesData, *volumesError) {
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, &volumesError{http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", "Failed to get Kubernetes client: " + err.Error()}
	}

	var in volumeInput
	fail := func(what string, err error) *volumesError {
		return &volumesError{http.StatusBadGateway, "KUBERNETES_API_ERROR", "Falha ao listar " + what + ": " + err.Error()}
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fail("PVCs", err)
	}
	in.PVCs = pvcs.Items
	pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fail("PVs", err)
	}
	in.PVs = pvs.Items
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fail("pods", err)
	}
	in.Pods = pods.Items
	// nodes é só enriquecimento (node pool): se o RBAC negar, segue sem a coluna
	if nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		in.Nodes = nodes.Items
	}
	// specs dos workloads: enriquecimento (dono do volume sem pod rodando); RBAC negado → segue sem
	var deps []appsv1.Deployment
	var stss []appsv1.StatefulSet
	var dss []appsv1.DaemonSet
	var cjs []batchv1.CronJob
	if l, err := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		deps = l.Items
	}
	if l, err := client.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{}); err == nil {
		stss = l.Items
	}
	if l, err := client.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{}); err == nil {
		dss = l.Items
	}
	if l, err := client.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{}); err == nil {
		cjs = l.Items
	}
	in.Specs = specsFromCluster(deps, stss, dss, cjs)
	items, summary := buildVolumeItems(in)
	expandBySC := map[string]bool{}
	scParams := map[string]map[string]string{}
	if scs, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for _, sc := range scs.Items {
			expandBySC[sc.Name] = sc.AllowVolumeExpansion != nil && *sc.AllowVolumeExpansion
			scParams[sc.Name] = sc.Parameters
		}
	}
	applyExpansion(items, in.PVCs, expandBySC, &summary)
	applyStorageInfo(items, in.PVs, scParams)
	applyManagement(items, in.PVCs, in.Specs)
	h.trackIdle(items, cluster, &summary)
	summary.SnapshotAPI = detectSnapshotVersion(ctx, client)
	if snaps, ok := fetchSnapshots(ctx, client, summary.SnapshotAPI); ok {
		applySnapshots(items, snaps, &summary)
	}
	recountIssues(items, &summary)
	data := &volumesData{Items: items, Summary: summary, ByApp: []AppCost{}}
	if includeUsage {
		if usage, err := fetchVolumeUsage(ctx, cluster); err != nil {
			data.UsageWarning = err.Error()
		} else {
			applyVolumeUsage(items, usage, &summary)
			markExpandBlocked(items, &summary)
			recountIssues(items, &summary)
			data.Summary = summary
		}
	}
	if includeCost {
		if h.volumeCost == nil {
			data.CostWarning = "Custo indisponível: o FinOps não está ligado neste servidor."
		} else if costIn, err := h.volumeCost.VolumeCostInputs(cluster); err != nil {
			data.CostWarning = "Custo indisponível: " + err.Error()
		} else {
			data.ByApp = applyVolumeCosts(items, in.PVs, scParams, costIn, &summary)
			data.Summary = summary
		}
	}
	return data, nil
}

func (h *NodePoolHandler) GetVolumes(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "MISSING_PARAMETERS", "message": "Parameter 'cluster' is required"}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	data, verr := h.collectVolumes(ctx, cluster, c.Query("include_usage") == "true", c.Query("include_cost") == "true")
	if verr != nil {
		c.JSON(verr.Status, gin.H{"success": false, "error": gin.H{"code": verr.Code, "message": verr.Message}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
