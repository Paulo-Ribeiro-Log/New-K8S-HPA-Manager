package finops

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/models"
)

func TestAnnotateAndPriceOrphans(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	items := []models.OrphanResource{
		{ID: "/S/pip", Type: "microsoft.network/publicipaddresses", SKU: "Standard Regional"},
		{ID: "/S/nic", Type: "microsoft.network/networkinterfaces"},
		{ID: "/S/pe", Type: "microsoft.network/privateendpoints"},
	}
	AnnotateOrphanAges(items, map[string]time.Time{
		"/s/nic": now.Add(-3 * 24 * time.Hour),  // mexido há 3 dias → recente
		"/s/pe":  now.Add(-10 * 24 * time.Hour), // 10 dias
	}, now)
	PriceOrphanResources(items, 5, func(o models.OrphanResource) string { return "del " + o.ID })

	byID := map[string]models.OrphanResource{}
	for _, o := range items {
		byID[o.ID] = o
	}
	if o := byID["/S/pip"]; o.SinceBasis != "no_change_14d" || o.AgeDays != 14 || o.Recent || o.MonthlyCostBRL != 18.25 {
		t.Errorf("pip = %+v", o)
	}
	if o := byID["/S/nic"]; !o.Recent || o.AgeDays != 3 || o.MonthlyCostUSD != 0 {
		t.Errorf("nic = %+v", o)
	}
	if o := byID["/S/pe"]; o.Recent || o.AgeDays != 10 || o.MonthlyCostUSD != 7.30 || o.DeleteCommand != "del /S/pe" {
		t.Errorf("pe = %+v", o)
	}
	if items[0].ID != "/S/pe" { // PE (US$ 7,30) custa mais que o IP (US$ 3,65)
		t.Errorf("ordenação por custo: primeiro = %s", items[0].ID)
	}
	s := SummarizeOrphans(items)
	if s.TotalCount != 3 || s.RecentCount != 1 || s.AgedCount != 2 || s.AgedCostBRL != round2(18.25+36.5) {
		t.Errorf("summary = %+v", s)
	}
}

func TestBuildUnattachedDisksReportForClusters(t *testing.T) {
	mcA := "MC_rg-a-app-prd_aks-a-prd_brazilsouth"
	disks := []models.UnattachedDisk{
		{Provider: "azure", ID: "/d/1", Name: "pvc-1", ResourceGroup: mcA, K8sClusterHint: mcA, K8sPVCName: "x", UnattachedSince: "2026-08-01T00:00:00Z"},
		{Provider: "azure", ID: "/d/2", Name: "avulso", ResourceGroup: "rg-a-data-prd"},
	}
	r := BuildUnattachedDisksReportForClusters(
		UnattachedDisksInput{Provider: "azure", Disks: disks, Now: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)},
		[]ClusterPVIndex{{Cluster: "aks-a-prd", Index: map[string]PVDiskRef{}, OK: true}},
	)
	got := map[string]UnattachedDiskItem{}
	for _, d := range r.Disks {
		got[d.ID] = d
	}
	if d := got["/d/1"]; d.Cluster != "aks-a-prd" || d.Verdict != DiskVerdictCandidate {
		t.Errorf("disco do cluster: cluster=%q verdict=%q", d.Cluster, d.Verdict)
	}
	if d := got["/d/2"]; d.Cluster != "" || d.Verdict != DiskVerdictReview {
		t.Errorf("disco sem cluster: cluster=%q verdict=%q", d.Cluster, d.Verdict)
	}
	if !r.PVCrossRef || r.Summary.TotalCount != 2 {
		t.Errorf("report = cross=%v total=%d", r.PVCrossRef, r.Summary.TotalCount)
	}
}

func TestFilterByJourney(t *testing.T) {
	disks := []models.UnattachedDisk{{ID: "d1", Journey: "Logistica"}, {ID: "d2", Journey: "backoffice"}, {ID: "d3"}}
	others := []models.OrphanResource{{ID: "o1", Journey: "logistica"}, {ID: "o2", Journey: "vendas"}}
	d, o, ign := FilterByJourney(disks, others, []string{"logistica"})
	if len(d) != 1 || d[0].ID != "d1" || len(o) != 1 || o[0].ID != "o1" {
		t.Errorf("kept disks=%+v others=%+v", d, o)
	}
	if ign.OtherJourney != 2 || ign.NoJourney != 1 {
		t.Errorf("ignored = %+v", ign)
	}
	// Sem jornada selecionada: qualquer jornada fica, recurso sem jornada continua fora.
	d, o, ign = FilterByJourney(disks, others, nil)
	if len(d) != 2 || len(o) != 2 || ign.NoJourney != 1 || ign.OtherJourney != 0 {
		t.Errorf("sem filtro: disks=%d others=%d ignored=%+v", len(d), len(o), ign)
	}
}
