package storage

import "testing"

func TestScanIssuesRegistraELimpa(t *testing.T) {
	r := newTestDependencyRegistry(t)
	if err := r.SetScanIssue("c1", ScanScopeVolumes, "sem permissão"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetScanIssue("c1", ScanScopeVolumes, "outro erro"); err != nil { // atualiza, não duplica
		t.Fatal(err)
	}
	if err := r.SetScanIssue("c2", ScanScopeCluster, "timeout"); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetScanIssues()
	if err != nil || len(got) != 2 {
		t.Fatalf("issues = %+v err=%v", got, err)
	}
	if err := r.SetScanIssue("c1", ScanScopeVolumes, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = r.GetScanIssues()
	if len(got) != 1 || got[0].Cluster != "c2" || got[0].Scope != ScanScopeCluster {
		t.Errorf("depois de limpar: %+v", got)
	}
	stats, err := r.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if is, ok := stats["scan_issues"].([]ScanIssue); !ok || len(is) != 1 {
		t.Errorf("stats.scan_issues = %#v", stats["scan_issues"])
	}
}
