package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFinOpsActionsStore(t *testing.T) {
	s, err := NewFinOpsActionsStore(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	base := FinOpsAction{Cluster: "c1", Namespace: "pay", Workload: "api", Channel: "staging",
		BaselineReportAt: time.Now().Add(-time.Hour), BaselineCPUMillis: 1000, TargetCPUMillis: 300}
	id1, err := s.Insert(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Channel, base.CreatedAt = "values", time.Now().Add(time.Minute)
	if _, err := s.Insert(base); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListByCluster("c1")
	if err != nil || len(list) != 2 || list[0].Channel != "values" || list[1].TargetCPUMillis != 300 {
		t.Fatalf("lista (mais recente primeiro): %+v err=%v", list, err)
	}
	dup := base
	dup.Channel = "values"
	if id, err := s.FindRecent(dup, time.Now().Add(-24*time.Hour)); err != nil || id == 0 {
		t.Errorf("FindRecent deveria achar a ação igual: id=%d err=%v", id, err)
	}
	dup.TargetCPUMillis = 400
	if id, _ := s.FindRecent(dup, time.Now().Add(-24*time.Hour)); id != 0 {
		t.Errorf("alvo diferente não é duplicata: id=%d", id)
	}
	if found, _ := s.Delete("outro", id1); found {
		t.Error("delete não pode apagar ação de outro cluster")
	}
	if found, err := s.Delete("c1", id1); err != nil || !found {
		t.Fatalf("delete: %v %v", found, err)
	}
	if list, _ := s.ListByCluster("c1"); len(list) != 1 {
		t.Errorf("sobrou %d", len(list))
	}
}
