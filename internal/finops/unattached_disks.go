package finops

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/models"
)

// Veredito de cada disco desatachado — vocabulário fechado, também usado pelo frontend.
const (
	// DiskVerdictCandidate: forte candidato a exclusão — o disco foi criado por este cluster e o PV
	// que o representava não existe mais (ou está Released/Available/Failed).
	DiskVerdictCandidate = "candidate"
	// DiskVerdictReview: sem evidência suficiente pra afirmar que sobrou — exige conferência
	// humana (disco fora do K8s, de outro cluster, ou desatachado há pouco tempo).
	DiskVerdictReview = "review"
	// DiskVerdictInUseByPV: um PV Bound deste cluster referencia o disco. Está desatachado só
	// porque nenhum pod o monta agora (workload escalado a 0, pod não agendado) — não excluir.
	DiskVerdictInUseByPV = "in_use_by_pv"
)

// Origem do disco, do ponto de vista do cluster analisado.
const (
	DiskOriginClusterPV = "cluster_pv" // um PV deste cluster aponta pra ele
	DiskOriginK8sTags   = "k8s_tags"   // criado por provisionamento dinâmico do K8s, mas sem PV neste cluster
	DiskOriginExternal  = "external"   // sem nenhuma marca de K8s
)

// recentUnattachedDays: desatachado há menos que isso nunca é "candidato" — pode ser só uma
// janela de troca de pod/nó (o disco é desatachado e reatachado no reagendamento).
const recentUnattachedDays = 7

// PVDiskRef descreve o PV deste cluster que referencia um disco do cloud.
type PVDiskRef struct {
	PVName        string
	Phase         string // Bound | Released | Available | Failed | Pending
	PVC           string // "namespace/nome", vazio se o PV não tem claimRef
	ReclaimPolicy string
	StorageClass  string
}

// UnattachedDiskItem é um disco desatachado + custo + classificação. Embute models.UnattachedDisk
// (JSON achatado).
type UnattachedDiskItem struct {
	models.UnattachedDisk

	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
	PriceSource    string  `json:"price_source"` // "api" | "fallback" | "table" | "unsupported" | "unpriced"

	AgeDays      int    `json:"age_days"`      // dias desde AgeBasis
	AgeBasis     string `json:"age_basis"`     // "unattached" (desde o detach) | "created" (desde a criação) | ""
	Origin       string `json:"origin"`        // DiskOrigin*
	Verdict      string `json:"verdict"`       // DiskVerdict*
	Reason       string `json:"reason"`        // por que esse veredito, em português
	ClusterMatch bool   `json:"cluster_match"` // a pista de cluster do disco aponta pra ESTE cluster

	PVName        string `json:"pv_name,omitempty"`
	PVPhase       string `json:"pv_phase,omitempty"`
	PVC           string `json:"pvc,omitempty"`
	ReclaimPolicy string `json:"reclaim_policy,omitempty"`
	StorageClass  string `json:"storage_class,omitempty"`
	DeleteCommand string `json:"delete_command,omitempty"` // nunca preenchido pra in_use_by_pv
}

// UnattachedDisksSummary consolida os números do relatório.
type UnattachedDisksSummary struct {
	TotalCount    int     `json:"total_count"`
	TotalSizeGB   float64 `json:"total_size_gb"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	TotalCostBRL  float64 `json:"total_cost_brl"`
	UnpricedCount int     `json:"unpriced_count"`

	CandidateCount   int     `json:"candidate_count"`
	CandidateCostBRL float64 `json:"candidate_cost_brl"`
	ReviewCount      int     `json:"review_count"`
	ReviewCostBRL    float64 `json:"review_cost_brl"`
	InUseByPVCount   int     `json:"in_use_by_pv_count"`
	InUseByPVCostBRL float64 `json:"in_use_by_pv_cost_brl"`
}

// UnattachedDisksReport é a resposta de GET /finops/unattached-disks.
type UnattachedDisksReport struct {
	Cluster      string                 `json:"cluster"`
	Provider     string                 `json:"provider"` // azure | gcp | aws
	Scope        string                 `json:"scope"`    // texto do escopo varrido (subscription / projeto / região+profile)
	Disks        []UnattachedDiskItem   `json:"disks"`
	Summary      UnattachedDisksSummary `json:"summary"`
	ExchangeRate float64                `json:"exchange_rate"`
	ExchangeDate string                 `json:"exchange_date"`
	// PVCrossRef é false quando não deu pra listar os PVs do cluster — sem isso nenhum disco pode
	// ser afirmado "candidato" (não dá pra saber se algum PV ainda o referencia).
	PVCrossRef bool      `json:"pv_cross_ref"`
	Warnings   []string  `json:"warnings"`
	ScannedAt  time.Time `json:"scanned_at"`
	FromCache  bool      `json:"from_cache"`
}

// DiskPriceFuncs injeta as consultas de preço por cloud (nil = sem preço pra aquele cloud).
// Funções em vez dos pricers concretos pra ficar testável sem SQLite/rede.
type DiskPriceFuncs struct {
	Azure func(azureType, tier, region string) (price float64, source string, err error) // USD/mês por disco (tier)
	GCP   func(diskType string) (pricePerGBMonth float64, source string, err error)
	AWS   func(volumeType string) (pricePerGBMonth float64, source string, err error)
}

// UnattachedDisksInput agrupa tudo que BuildUnattachedDisksReport precisa.
type UnattachedDisksInput struct {
	Cluster      string
	Provider     string
	Scope        string
	Disks        []models.UnattachedDisk
	PVIndex      map[string]PVDiskRef // nil/vazio + PVIndexOK=false → cruzamento indisponível
	PVIndexOK    bool
	Prices       DiskPriceFuncs
	ExchangeRate float64
	ExchangeDate string
	Now          time.Time

	// Contexto pros comandos de exclusão sugeridos.
	GCPProject string
	AWSProfile string
}

// ── Índice de PVs do cluster ───────────────────────────────────────────────

var gcpDiskHandleRe = regexp.MustCompile(`(?:zones|regions)/([^/]+)/disks/([^/]+)$`)

// handleKeys devolve as chaves normalizadas (minúsculas) que identificam um disco do cloud a
// partir do handle de um PV, da mais específica pra menos. diskKeysFor faz o equivalente pro lado
// do disco descoberto — os dois lados sempre geram chaves no mesmo formato.
func handleKeys(handle string) []string {
	h := strings.ToLower(strings.TrimSpace(handle))
	if h == "" {
		return nil
	}
	keys := []string{h}
	if m := gcpDiskHandleRe.FindStringSubmatch(h); m != nil {
		keys = append(keys, m[1]+"/"+m[2]) // zona-ou-região/nome
	}
	if i := strings.LastIndex(h, "/"); i >= 0 && i < len(h)-1 {
		keys = append(keys, h[i+1:]) // último segmento: nome do disco ou vol-xxxx
	}
	return keys
}

// diskKeysFor: chaves de um disco descoberto, da mais específica pra menos.
func diskKeysFor(d models.UnattachedDisk) []string {
	switch d.Provider {
	case "azure":
		return dedupe([]string{strings.ToLower(d.ID), strings.ToLower(d.Name)})
	case "gcp":
		loc := d.Zone
		if loc == "" {
			loc = d.Location
		}
		return dedupe([]string{strings.ToLower(loc + "/" + d.Name), strings.ToLower(d.Name)})
	default: // aws — d.Name pode ser a tag Name, então só o VolumeId identifica
		return []string{strings.ToLower(d.ID)}
	}
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// pvBackingHandles extrai os identificadores do disco do cloud a partir do PV, cobrindo o driver
// CSI (qualquer um: o VolumeHandle) e os plugins in-tree legados.
func pvBackingHandles(pv corev1.PersistentVolume) []string {
	var handles []string
	if pv.Spec.CSI != nil {
		handles = append(handles, pv.Spec.CSI.VolumeHandle)
	}
	if ad := pv.Spec.AzureDisk; ad != nil {
		handles = append(handles, ad.DataDiskURI, ad.DiskName)
	}
	if gce := pv.Spec.GCEPersistentDisk; gce != nil {
		handles = append(handles, gce.PDName)
	}
	if ebs := pv.Spec.AWSElasticBlockStore; ebs != nil {
		handles = append(handles, ebs.VolumeID) // "vol-xxx" ou "aws://zona/vol-xxx"
	}
	return handles
}

// LoadPVDiskIndex lista os PVs do cluster e indexa por identificador do disco de backing.
func LoadPVDiskIndex(ctx context.Context, client kubernetes.Interface) (map[string]PVDiskRef, error) {
	pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listar PVs: %w", err)
	}
	index := make(map[string]PVDiskRef)
	for _, pv := range pvs.Items {
		ref := PVDiskRef{
			PVName:        pv.Name,
			Phase:         string(pv.Status.Phase),
			ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
			StorageClass:  pv.Spec.StorageClassName,
		}
		if c := pv.Spec.ClaimRef; c != nil {
			ref.PVC = c.Namespace + "/" + c.Name
		}
		for _, h := range pvBackingHandles(pv) {
			for _, k := range handleKeys(h) {
				// Primeiro PV a registrar a chave vence — chave duplicada só acontece com o
				// nome "cru" do disco, que é o menos específico.
				if _, taken := index[k]; !taken {
					index[k] = ref
				}
			}
		}
	}
	return index, nil
}

// ── Preço ───────────────────────────────────────────────────────────────────

func priceUnattachedDisk(d models.UnattachedDisk, p DiskPriceFuncs) (usd float64, source string) {
	// Tipos que cobram por IOPS/throughput provisionados (ou que as APIs por tier/GB não cobrem)
	// têm preço de TABELA — checados antes das consultas por API, que não os conhecem.
	if row, ok := tablePriceFor(d); ok {
		return tableMonthlyCostUSD(d, row), "table"
	}
	switch d.Provider {
	case "azure":
		azType, _ := MapStorageClassToAzureType("", "", d.DiskType)
		if _, managed := managedDiskTiers[azType]; !managed || p.Azure == nil {
			// SKU sem preço mapeado nem linha na tabela (ex: tipo novo do Azure).
			return 0, "unsupported"
		}
		if !isKnownAzureSKU(d.DiskType) {
			return 0, "unsupported"
		}
		tier := ResolveManagedDiskTier(azType, d.SizeGB)
		price, src, err := p.Azure(azType, tier, d.Location)
		if err != nil {
			return 0, "unpriced"
		}
		return round2(price), src
	case "gcp":
		if p.GCP == nil {
			return 0, "unpriced"
		}
		perGB, src, err := p.GCP(d.DiskType)
		if err != nil {
			return 0, "unsupported" // tipo sem preço mapeado (nem na tabela)
		}
		return round2(perGB * d.SizeGB), src
	case "aws":
		if p.AWS == nil {
			return 0, "unpriced"
		}
		perGB, src, err := p.AWS(d.DiskType)
		if err != nil {
			return 0, "unsupported" // tipo sem preço mapeado (nem na tabela)
		}
		return round2(perGB * d.SizeGB), src
	}
	return 0, "unpriced"
}

func isKnownAzureSKU(sku string) bool {
	_, ok := skuNameToAzureType[strings.ToLower(sku)]
	return ok
}

// ── Classificação ───────────────────────────────────────────────────────────

// clusterHintMatches: a pista de cluster do disco aponta pro cluster analisado?
// Azure: "MC_<rg>_<cluster>_<região>" · AWS: nome do cluster · GCP: "gke-<cluster truncado>-...".
func clusterHintMatches(hint, cluster string) bool {
	hint = strings.ToLower(hint)
	name := strings.ToLower(clusterShortName(cluster))
	if hint == "" || name == "" {
		return false
	}
	if hint == name || strings.Contains(hint, "_"+name+"_") {
		return true
	}
	// GKE trunca o nome do cluster dentro do nome do disco — aceita prefixo de 4+ letras só
	// quando o hint tem a forma "gke-<algo>-…".
	if strings.HasPrefix(hint, "gke-") {
		rest := strings.TrimPrefix(hint, "gke-")
		for l := len(name); l >= 4; l-- {
			if strings.HasPrefix(rest, name[:l]+"-") {
				return true
			}
		}
	}
	return false
}

// clusterShortName reduz o context do kubeconfig ao nome curto do cluster: tira "-admin" e, pra
// ARN de EKS ("arn:aws:eks:região:conta:cluster/nome") e context GKE ("gke_proj_região_nome"),
// devolve só o nome.
func clusterShortName(cluster string) string {
	n := strings.TrimSuffix(cluster, "-admin")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	if strings.HasPrefix(n, "gke_") {
		if parts := strings.Split(n, "_"); len(parts) >= 4 {
			n = strings.Join(parts[3:], "_")
		}
	}
	return n
}

func diskAge(d models.UnattachedDisk, now time.Time) (days int, basis string) {
	for _, c := range []struct{ ts, basis string }{{d.UnattachedSince, "unattached"}, {d.CreatedAt, "created"}} {
		if c.ts == "" {
			continue
		}
		if t, err := parseCloudTime(c.ts); err == nil {
			days := int(now.Sub(t).Hours() / 24)
			if days < 0 {
				days = 0
			}
			return days, c.basis
		}
	}
	return 0, ""
}

// parseCloudTime aceita os formatos de data dos 3 clouds (RFC3339 com/sem fração de segundo).
func parseCloudTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000-07:00", "2006-01-02T15:04:05.000000Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("data não reconhecida: %q", s)
}

func classifyUnattachedDisk(item *UnattachedDiskItem, in UnattachedDisksInput) {
	// 1) Algum PV deste cluster aponta pro disco?
	var pv *PVDiskRef
	if in.PVIndexOK {
		for _, k := range diskKeysFor(item.UnattachedDisk) {
			if ref, ok := in.PVIndex[k]; ok {
				r := ref
				pv = &r
				break
			}
		}
	}

	item.ClusterMatch = clusterHintMatches(item.K8sClusterHint, in.Cluster)
	recent := item.AgeBasis == "unattached" && item.AgeDays < recentUnattachedDays

	switch {
	case pv != nil:
		item.Origin = DiskOriginClusterPV
		item.PVName, item.PVPhase, item.PVC = pv.PVName, pv.Phase, pv.PVC
		item.ReclaimPolicy, item.StorageClass = pv.ReclaimPolicy, pv.StorageClass
		item.ClusterMatch = true
		if pv.Phase == string(corev1.VolumeBound) {
			item.Verdict = DiskVerdictInUseByPV
			item.Reason = fmt.Sprintf("O PV %s está Bound ao PVC %s, mas nenhum pod o monta agora (workload escalado a 0 ou pod não agendado). Não excluir — remova o PVC pelo cluster se for o caso.", pv.PVName, orDash(pv.PVC))
		} else {
			item.Verdict = DiskVerdictCandidate
			item.Reason = fmt.Sprintf("O PV %s está %s (reclaimPolicy %s): o PVC foi excluído e o disco ficou retido. Confirme que não há dado necessário e exclua o PV e o disco.", pv.PVName, pv.Phase, orDash(pv.ReclaimPolicy))
		}
	case item.IsK8sProvisioned():
		item.Origin = DiskOriginK8sTags
		switch {
		case !in.PVIndexOK:
			item.Verdict = DiskVerdictReview
			item.Reason = "Criado por provisionamento dinâmico do K8s, mas não foi possível listar os PVs do cluster para confirmar se ainda está referenciado."
		case item.ClusterMatch:
			item.Verdict = DiskVerdictCandidate
			item.Reason = "Criado por este cluster (PVC " + pvcLabel(item.UnattachedDisk) + ") e nenhum PV atual o referencia — o PVC/PV foi excluído e o disco sobrou."
		case item.K8sClusterHint != "":
			item.Verdict = DiskVerdictReview
			item.Reason = "Criado pelo K8s, mas a pista de cluster (" + item.K8sClusterHint + ") não aponta para este cluster — pode ser de outro cluster; confira lá antes de excluir."
		default:
			item.Verdict = DiskVerdictReview
			item.Reason = "Criado pelo K8s (PVC " + pvcLabel(item.UnattachedDisk) + "), sem PV neste cluster e sem como saber de qual cluster veio — pode pertencer a outro cluster."
		}
	default:
		item.Origin = DiskOriginExternal
		item.Verdict = DiskVerdictReview
		item.Reason = "Disco sem vínculo com Kubernetes — confirme com o dono antes de excluir."
	}

	if recent && item.Verdict == DiskVerdictCandidate {
		item.Verdict = DiskVerdictReview
		item.Reason = fmt.Sprintf("Desatachado há apenas %d dia(s) — pode ser só uma troca de pod/nó. %s", item.AgeDays, item.Reason)
	}
}

func pvcLabel(d models.UnattachedDisk) string {
	switch {
	case d.K8sPVCNamespace != "" && d.K8sPVCName != "":
		return d.K8sPVCNamespace + "/" + d.K8sPVCName
	case d.K8sPVCName != "":
		return d.K8sPVCName
	default:
		return orDash(d.K8sPVName)
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// deleteCommand devolve o comando (pra copiar/colar) que exclui o disco — a app NUNCA exclui.
func deleteCommand(d models.UnattachedDisk, in UnattachedDisksInput) string {
	switch d.Provider {
	case "azure":
		return fmt.Sprintf("az disk delete --ids %q --yes", d.ID)
	case "gcp":
		loc := "--zone " + d.Zone
		if d.Zone == "" {
			loc = "--region " + d.Location
		}
		cmd := fmt.Sprintf("gcloud compute disks delete %s %s", d.Name, loc)
		if in.GCPProject != "" {
			cmd += " --project " + in.GCPProject
		}
		return cmd
	case "aws":
		cmd := fmt.Sprintf("aws ec2 delete-volume --volume-id %s --region %s", d.ID, d.Location)
		if in.AWSProfile != "" {
			cmd += " --profile " + in.AWSProfile
		}
		return cmd
	}
	return ""
}

// BuildUnattachedDisksReport precifica, classifica e agrega os discos desatachados. Puro (sem
// rede nem disco): tudo que depende de cloud/K8s chega pronto em `in`.
func BuildUnattachedDisksReport(in UnattachedDisksInput) UnattachedDisksReport {
	rate := in.ExchangeRate
	if rate <= 0 {
		rate = DefaultExchangeRate
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	report := UnattachedDisksReport{
		Cluster:      in.Cluster,
		Provider:     in.Provider,
		Scope:        in.Scope,
		Disks:        make([]UnattachedDiskItem, 0, len(in.Disks)),
		ExchangeRate: rate,
		ExchangeDate: in.ExchangeDate,
		PVCrossRef:   in.PVIndexOK,
		Warnings:     []string{},
		ScannedAt:    now,
	}

	for _, d := range in.Disks {
		item := UnattachedDiskItem{UnattachedDisk: d}
		usd, src := priceUnattachedDisk(d, in.Prices)
		item.MonthlyCostUSD = usd
		item.MonthlyCostBRL = round2(usd * rate)
		item.PriceSource = src
		item.AgeDays, item.AgeBasis = diskAge(d, now)
		classifyUnattachedDisk(&item, in)
		if item.Verdict != DiskVerdictInUseByPV {
			item.DeleteCommand = deleteCommand(d, in)
		}
		report.Disks = append(report.Disks, item)
	}

	sort.SliceStable(report.Disks, func(i, j int) bool {
		a, b := report.Disks[i], report.Disks[j]
		if a.MonthlyCostBRL != b.MonthlyCostBRL {
			return a.MonthlyCostBRL > b.MonthlyCostBRL
		}
		return a.SizeGB > b.SizeGB
	})

	report.Summary = summarizeUnattachedDisks(report.Disks)
	return report
}

func summarizeUnattachedDisks(items []UnattachedDiskItem) UnattachedDisksSummary {
	var s UnattachedDisksSummary
	for _, it := range items {
		s.TotalCount++
		s.TotalSizeGB = round2(s.TotalSizeGB + it.SizeGB)
		s.TotalCostUSD = round2(s.TotalCostUSD + it.MonthlyCostUSD)
		s.TotalCostBRL = round2(s.TotalCostBRL + it.MonthlyCostBRL)
		if it.PriceSource == "unsupported" || it.PriceSource == "unpriced" {
			s.UnpricedCount++
		}
		switch it.Verdict {
		case DiskVerdictCandidate:
			s.CandidateCount++
			s.CandidateCostBRL = round2(s.CandidateCostBRL + it.MonthlyCostBRL)
		case DiskVerdictInUseByPV:
			s.InUseByPVCount++
			s.InUseByPVCostBRL = round2(s.InUseByPVCostBRL + it.MonthlyCostBRL)
		default:
			s.ReviewCount++
			s.ReviewCostBRL = round2(s.ReviewCostBRL + it.MonthlyCostBRL)
		}
	}
	return s
}
