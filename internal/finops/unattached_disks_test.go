package finops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"k8s-hpa-manager/internal/models"
)

var testNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func azDisk(name, sku string, sizeGB float64) models.UnattachedDisk {
	return models.UnattachedDisk{
		Provider: "azure", Name: name, DiskType: sku, SizeGB: sizeGB, Location: "brazilsouth",
		ID:        "/subscriptions/s/resourceGroups/MC_rg_aks-prd_brazilsouth/providers/Microsoft.Compute/disks/" + name,
		CreatedAt: "2025-01-01T00:00:00Z",
	}
}

func azPrice(azType, tier, region string) (float64, string, error) {
	if azType == "Premium SSD" && tier == "P10" && region == "brazilsouth" {
		return 19.71, "api", nil
	}
	return 0, "", errors.New("preço inesperado: " + azType + "/" + tier)
}

func baseInput(disks ...models.UnattachedDisk) UnattachedDisksInput {
	return UnattachedDisksInput{
		Cluster: "aks-prd-admin", Provider: "azure", Scope: "sub", Disks: disks,
		PVIndex: map[string]PVDiskRef{}, PVIndexOK: true,
		Prices:       DiskPriceFuncs{Azure: azPrice},
		ExchangeRate: 5, Now: testNow,
	}
}

func TestBuild_PricesAzureByTier(t *testing.T) {
	r := BuildUnattachedDisksReport(baseInput(azDisk("d1", "Premium_LRS", 100)))
	it := r.Disks[0]
	if it.MonthlyCostUSD != 19.71 || it.MonthlyCostBRL != 98.55 || it.PriceSource != "api" {
		t.Errorf("custo Azure (100GB Premium → P10): %+v", it)
	}
}

func perfDisk(provider, sku string, sizeGB, iops, mbps float64) models.UnattachedDisk {
	return models.UnattachedDisk{
		Provider: provider, Name: "d", ID: "id-" + sku, DiskType: sku, SizeGB: sizeGB,
		ProvisionedIOPS: iops, ProvisionedMBps: mbps, Location: "brazilsouth", Zone: "southamerica-east1-a",
	}
}

func TestPriceTable_ChargesProvisionedPerformance(t *testing.T) {
	// Valores esperados = capacidade + IOPS/throughput acima da cota gratuita, com os preços da
	// tabela (Azure em USD/hora × 730; GCP em USD/mês).
	cases := []struct {
		name string
		d    models.UnattachedDisk
		want float64
	}{
		// Ultra: 1024×0.23944 + 5000×0.09928 + 200×0.66649 (sem cota gratuita)
		{"azure ultra", perfDisk("azure", "UltraSSD_LRS", 1024, 5000, 200), 874.88},
		// Premium SSD v2 no baseline (3000 IOPS / 125 MBps): só capacidade — 1024×0.15184
		{"azure premiumv2 baseline", perfDisk("azure", "PremiumV2_LRS", 1024, 3000, 125), 155.48},
		// acima do baseline: + 3000×0.00949 + 100×0.07592
		{"azure premiumv2 acima", perfDisk("azure", "PremiumV2_LRS", 1024, 6000, 225), 191.55},
		// Hyperdisk Balanced: 100×0.127, baseline 3000/140 → só capacidade; acima: +7000×0.008 +360×0.064
		{"gcp hyperdisk-balanced baseline", perfDisk("gcp", "hyperdisk-balanced", 100, 3000, 140), 12.7},
		{"gcp hyperdisk-balanced acima", perfDisk("gcp", "hyperdisk-balanced", 100, 10000, 500), 91.74},
		{"gcp hyperdisk-extreme", perfDisk("gcp", "hyperdisk-extreme", 100, 20000, 0), 1039.9},
		{"gcp hyperdisk-throughput", perfDisk("gcp", "hyperdisk-throughput", 1000, 0, 100), 47.8},
		{"aws ebs magnético", perfDisk("aws", "standard", 100, 0, 0), 5},
		// caixa do SKU não importa
		{"azure sku em outra caixa", perfDisk("azure", "ultrassd_lrs", 1024, 5000, 200), 874.88},
	}
	for _, c := range cases {
		usd, src := priceUnattachedDisk(c.d, DiskPriceFuncs{})
		if src != "table" || usd != c.want {
			t.Errorf("%s: preço = %v (%s), quer %v (table)", c.name, usd, src, c.want)
		}
	}
}

func TestPriceTable_TakesPrecedenceOverAPIPricers(t *testing.T) {
	// Mesmo com os pricers por API injetados, um tipo da tabela nunca cai neles (que errariam ou,
	// pior, mapeariam "premiumv2" pra Premium SSD por prefixo).
	called := false
	p := DiskPriceFuncs{Azure: func(_, _, _ string) (float64, string, error) { called = true; return 1, "api", nil }}
	if _, src := priceUnattachedDisk(perfDisk("azure", "PremiumV2_LRS", 100, 0, 0), p); src != "table" || called {
		t.Errorf("Premium SSD v2 deveria usar a tabela sem consultar o pricer (src=%s called=%v)", src, called)
	}
}

func TestPrice_UnknownTypesStayUnsupported(t *testing.T) {
	for _, d := range []models.UnattachedDisk{
		perfDisk("azure", "FooSSD_LRS", 100, 0, 0),
		perfDisk("gcp", "hyperdisk-novo", 100, 0, 0),
		perfDisk("aws", "sc2", 100, 0, 0),
	} {
		in := baseInput(d)
		in.Prices = DiskPriceFuncs{
			GCP: func(string) (float64, string, error) { return 0, "", errors.New("tipo desconhecido") },
			AWS: func(string) (float64, string, error) { return 0, "", errors.New("tipo desconhecido") },
		}
		it := BuildUnattachedDisksReport(in).Disks[0]
		if it.PriceSource != "unsupported" || it.MonthlyCostBRL != 0 {
			t.Errorf("%s/%s desconhecido deveria ficar sem preço: %+v", d.Provider, d.DiskType, it)
		}
	}
}

func TestBuild_GCPAndAWSPerGB(t *testing.T) {
	in := baseInput(
		models.UnattachedDisk{Provider: "gcp", Name: "pd1", ID: "pd1", DiskType: "pd-ssd", SizeGB: 200, Zone: "z-a", CreatedAt: "2025-01-01T00:00:00Z"},
		models.UnattachedDisk{Provider: "gcp", Name: "hd", ID: "hd", DiskType: "hyperdisk-balanced", SizeGB: 10, Zone: "z-a"},
		models.UnattachedDisk{Provider: "aws", Name: "v", ID: "vol-1", DiskType: "gp3", SizeGB: 50, Location: "us-east-1"},
		models.UnattachedDisk{Provider: "aws", Name: "m", ID: "vol-2", DiskType: "standard", SizeGB: 50, Location: "us-east-1"},
	)
	in.Prices = DiskPriceFuncs{
		GCP: func(t string) (float64, string, error) {
			if t == "pd-ssd" {
				return 0.25, "api", nil
			}
			return 0, "", errors.New("tipo desconhecido")
		},
		AWS: func(t string) (float64, string, error) {
			if t == "gp3" {
				return 0.08, "fallback", nil
			}
			return 0, "", errors.New("tipo desconhecido")
		},
	}
	r := BuildUnattachedDisksReport(in)
	got := map[string]UnattachedDiskItem{}
	for _, it := range r.Disks {
		got[it.Name] = it
	}
	if got["pd1"].MonthlyCostUSD != 50 || got["v"].MonthlyCostUSD != 4 {
		t.Errorf("preço por GB: pd1=%v v=%v", got["pd1"].MonthlyCostUSD, got["v"].MonthlyCostUSD)
	}
	if got["hd"].PriceSource != "table" || got["hd"].MonthlyCostUSD != 1.27 || got["m"].PriceSource != "table" || got["m"].MonthlyCostUSD != 2.5 {
		t.Errorf("hyperdisk/EBS magnético agora têm preço de tabela: hd=%+v m=%+v", got["hd"], got["m"])
	}
	if r.Disks[0].Name != "pd1" {
		t.Errorf("ordenação por custo desc: primeiro = %s", r.Disks[0].Name)
	}
}

func TestClassify_PVBoundIsNeverACandidate(t *testing.T) {
	d := azDisk("pvc-1", "Premium_LRS", 100)
	in := baseInput(d)
	in.PVIndex = map[string]PVDiskRef{
		strings.ToLower(d.ID): {PVName: "pv-1", Phase: "Bound", PVC: "db/data-0", ReclaimPolicy: "Delete"},
	}
	it := BuildUnattachedDisksReport(in).Disks[0]
	if it.Verdict != DiskVerdictInUseByPV || it.Origin != DiskOriginClusterPV || it.PVC != "db/data-0" {
		t.Errorf("PV Bound: %+v", it)
	}
	if it.DeleteCommand != "" {
		t.Errorf("in_use_by_pv não deve sugerir comando de exclusão: %q", it.DeleteCommand)
	}
}

func TestClassify_ReleasedPVIsCandidate(t *testing.T) {
	d := azDisk("pvc-2", "Premium_LRS", 100)
	in := baseInput(d)
	in.PVIndex = map[string]PVDiskRef{strings.ToLower(d.Name): {PVName: "pv-2", Phase: "Released", ReclaimPolicy: "Retain"}}
	it := BuildUnattachedDisksReport(in).Disks[0]
	if it.Verdict != DiskVerdictCandidate || it.PVPhase != "Released" {
		t.Errorf("PV Released: %+v", it)
	}
	if !strings.HasPrefix(it.DeleteCommand, "az disk delete --ids") {
		t.Errorf("comando de exclusão: %q", it.DeleteCommand)
	}
}

func TestClassify_K8sTagsAndClusterHint(t *testing.T) {
	mine := azDisk("mine", "Premium_LRS", 100)
	mine.K8sPVCName, mine.K8sPVCNamespace, mine.K8sClusterHint = "data-0", "db", "MC_rg_aks-prd_brazilsouth"
	other := azDisk("other", "Premium_LRS", 100)
	other.K8sPVCName, other.K8sClusterHint = "x", "MC_rg_aks-outro_brazilsouth"
	nohint := azDisk("nohint", "Premium_LRS", 100)
	nohint.K8sPVCName = "y"

	r := BuildUnattachedDisksReport(baseInput(mine, other, nohint))
	v := map[string]string{}
	for _, it := range r.Disks {
		v[it.Name] = it.Verdict
	}
	if v["mine"] != DiskVerdictCandidate {
		t.Errorf("criado por ESTE cluster, sem PV: %s", v["mine"])
	}
	if v["other"] != DiskVerdictReview || v["nohint"] != DiskVerdictReview {
		t.Errorf("de outro cluster / sem pista deve ser review: other=%s nohint=%s", v["other"], v["nohint"])
	}
}

func TestClassify_WithoutPVIndexNothingIsCandidate(t *testing.T) {
	mine := azDisk("mine", "Premium_LRS", 100)
	mine.K8sPVCName, mine.K8sClusterHint = "data-0", "MC_rg_aks-prd_brazilsouth"
	in := baseInput(mine)
	in.PVIndexOK = false
	it := BuildUnattachedDisksReport(in).Disks[0]
	if it.Verdict != DiskVerdictReview || it.Verdict == DiskVerdictCandidate {
		t.Errorf("sem cruzamento com PVs não se pode afirmar candidato: %+v", it)
	}
	if BuildUnattachedDisksReport(in).PVCrossRef {
		t.Error("PVCrossRef deveria ser false")
	}
}

func TestClassify_ExternalDiskNeedsReview(t *testing.T) {
	it := BuildUnattachedDisksReport(baseInput(azDisk("manual", "Standard_LRS", 10))).Disks[0]
	if it.Verdict != DiskVerdictReview || it.Origin != DiskOriginExternal {
		t.Errorf("disco externo: %+v", it)
	}
}

func TestClassify_RecentDetachDowngradesCandidate(t *testing.T) {
	d := azDisk("mine", "Premium_LRS", 100)
	d.K8sPVCName, d.K8sClusterHint = "data-0", "MC_rg_aks-prd_brazilsouth"
	d.UnattachedSince = testNow.Add(-48 * time.Hour).Format(time.RFC3339)
	it := BuildUnattachedDisksReport(baseInput(d)).Disks[0]
	if it.Verdict != DiskVerdictReview || it.AgeBasis != "unattached" || it.AgeDays != 2 {
		t.Errorf("desatachado há 2 dias deve virar review: %+v", it)
	}

	d.UnattachedSince = testNow.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	if it := BuildUnattachedDisksReport(baseInput(d)).Disks[0]; it.Verdict != DiskVerdictCandidate {
		t.Errorf("desatachado há 30 dias continua candidato: %+v", it)
	}
}

func TestAge_FallsBackToCreationDate(t *testing.T) {
	d := azDisk("x", "Premium_LRS", 1)
	d.CreatedAt = testNow.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	it := BuildUnattachedDisksReport(baseInput(d)).Disks[0]
	if it.AgeBasis != "created" || it.AgeDays != 10 {
		t.Errorf("idade pela criação: %+v", it)
	}
	// Criação recente NÃO rebaixa candidato — só o detach recente conta.
	d.K8sPVCName, d.K8sClusterHint = "p", "MC_rg_aks-prd_brazilsouth"
	d.CreatedAt = testNow.Add(-24 * time.Hour).Format(time.RFC3339)
	if it := BuildUnattachedDisksReport(baseInput(d)).Disks[0]; it.Verdict != DiskVerdictCandidate {
		t.Errorf("disco criado ontem e já órfão continua candidato: %+v", it)
	}
}

func TestDeleteCommands(t *testing.T) {
	in := UnattachedDisksInput{GCPProject: "proj", AWSProfile: "prof"}
	cases := []struct {
		d    models.UnattachedDisk
		want string
	}{
		{models.UnattachedDisk{Provider: "gcp", Name: "d", Zone: "us-a", Location: "us"}, "gcloud compute disks delete d --zone us-a --project proj"},
		{models.UnattachedDisk{Provider: "gcp", Name: "d", Location: "us-east1"}, "gcloud compute disks delete d --region us-east1 --project proj"},
		{models.UnattachedDisk{Provider: "aws", ID: "vol-1", Location: "us-east-1"}, "aws ec2 delete-volume --volume-id vol-1 --region us-east-1 --profile prof"},
	}
	for _, c := range cases {
		if got := deleteCommand(c.d, in); got != c.want {
			t.Errorf("deleteCommand = %q, quer %q", got, c.want)
		}
	}
}

func TestClusterHintMatches(t *testing.T) {
	cases := []struct {
		hint, cluster string
		want          bool
	}{
		{"MC_rg_aks-prd_brazilsouth", "aks-prd-admin", true},
		{"MC_rg_aks-prd-2_brazilsouth", "aks-prd-admin", false}, // não casa por substring solta
		{"eks-prd", "arn:aws:eks:us-east-1:123:cluster/eks-prd", true},
		{"gke-meucluster-abc123-pvc-9", "gke_proj_us-central1_meucluster", true},
		{"gke-meucl-abc123-pvc-9", "gke_proj_us-central1_meucluster", true}, // nome truncado pelo GKE
		{"gke-outro-abc123-pvc-9", "gke_proj_us-central1_meucluster", false},
		{"", "aks-prd", false},
	}
	for _, c := range cases {
		if got := clusterHintMatches(c.hint, c.cluster); got != c.want {
			t.Errorf("clusterHintMatches(%q, %q) = %v, quer %v", c.hint, c.cluster, got, c.want)
		}
	}
}

func TestSummary(t *testing.T) {
	cand := azDisk("cand", "Premium_LRS", 100)
	cand.K8sPVCName, cand.K8sClusterHint = "p", "MC_rg_aks-prd_brazilsouth"
	bound := azDisk("bound", "Premium_LRS", 100)
	ext := azDisk("ext", "Premium_LRS", 100)
	in := baseInput(cand, bound, ext)
	in.PVIndex = map[string]PVDiskRef{strings.ToLower(bound.ID): {PVName: "pv", Phase: "Bound"}}

	s := BuildUnattachedDisksReport(in).Summary
	if s.TotalCount != 3 || s.CandidateCount != 1 || s.InUseByPVCount != 1 || s.ReviewCount != 1 {
		t.Errorf("contagens: %+v", s)
	}
	if s.TotalCostBRL != 295.65 || s.CandidateCostBRL != 98.55 {
		t.Errorf("custos: %+v", s)
	}
	if s.TotalSizeGB != 300 {
		t.Errorf("TotalSizeGB = %v", s.TotalSizeGB)
	}
}

func TestLoadPVDiskIndex_AllProviders(t *testing.T) {
	mk := func(name string, phase corev1.PersistentVolumePhase, src corev1.PersistentVolumeSource, claim *corev1.ObjectReference) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeSource:        src,
				ClaimRef:                      claim,
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "sc",
			},
			Status: corev1.PersistentVolumeStatus{Phase: phase},
		}
	}
	client := fake.NewSimpleClientset(
		mk("pv-az", corev1.VolumeBound, corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{
			Driver: "disk.csi.azure.com", VolumeHandle: "/subscriptions/S/resourceGroups/MC_RG/providers/Microsoft.Compute/disks/PVC-AZ"}},
			&corev1.ObjectReference{Namespace: "db", Name: "data-0"}),
		mk("pv-gcp", corev1.VolumeReleased, corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{
			Driver: "pd.csi.storage.gke.io", VolumeHandle: "projects/p/zones/southamerica-east1-a/disks/pvc-gcp"}}, nil),
		mk("pv-aws", corev1.VolumeAvailable, corev1.PersistentVolumeSource{
			AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{VolumeID: "aws://us-east-1a/vol-0abc"}}, nil),
	)

	idx, err := LoadPVDiskIndex(context.Background(), client)
	if err != nil {
		t.Fatalf("LoadPVDiskIndex: %v", err)
	}

	// Azure: casa pelo resource ID completo e pelo nome, sem diferenciar caixa.
	az := models.UnattachedDisk{Provider: "azure", ID: "/subscriptions/s/resourcegroups/mc_rg/providers/microsoft.compute/disks/pvc-az", Name: "pvc-az"}
	// GCP: casa por zona/nome.
	gcp := models.UnattachedDisk{Provider: "gcp", ID: "x", Name: "pvc-gcp", Zone: "southamerica-east1-a"}
	// AWS: só pelo VolumeId, mesmo quando o PV usa o formato aws://zona/vol.
	aws := models.UnattachedDisk{Provider: "aws", ID: "vol-0abc", Name: "tag-name-qualquer"}

	for _, tc := range []struct {
		d     models.UnattachedDisk
		phase string
	}{{az, "Bound"}, {gcp, "Released"}, {aws, "Available"}} {
		var found *PVDiskRef
		for _, k := range diskKeysFor(tc.d) {
			if ref, ok := idx[k]; ok {
				found = &ref
				break
			}
		}
		if found == nil || found.Phase != tc.phase {
			t.Errorf("%s: PV não encontrado no índice (chaves %v): %+v", tc.d.Provider, diskKeysFor(tc.d), found)
		}
	}

	if ref := idx[strings.ToLower(az.ID)]; ref.PVC != "db/data-0" || ref.ReclaimPolicy != "Retain" || ref.StorageClass != "sc" {
		t.Errorf("metadados do PV Azure: %+v", ref)
	}
	// Disco de outra zona com o mesmo nome NÃO casa pela chave zona/nome (só pelo nome cru).
	other := models.UnattachedDisk{Provider: "gcp", Name: "pvc-gcp", Zone: "us-central1-a"}
	if _, ok := idx[diskKeysFor(other)[0]]; ok {
		t.Error("chave zona/nome de outra zona não deveria casar")
	}
}
