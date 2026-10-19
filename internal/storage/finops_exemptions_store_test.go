package storage

import (
	"path/filepath"
	"testing"
)

func TestFinOpsExemptionsStore(t *testing.T) {
	s, err := NewFinOpsExemptionsStore(filepath.Join(t.TempDir(), "ex.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := FinOpsExemption{Cluster: "aks-hlg", Namespace: "pay", Workload: "api", MaxReplicasThreshold: 1, Reason: "Ambiente HLG", CreatedBy: "a@x"}
	if err := s.Upsert(e); err != nil {
		t.Fatal(err)
	}
	e.MaxReplicasThreshold, e.Reason, e.CreatedBy = 2, "Controle de custos", "b@x"
	if err := s.Upsert(e); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetAllAsMap("aks-hlg")
	if err != nil {
		t.Fatal(err)
	}
	got := m["pay/api"]
	if len(m) != 1 || got == nil || got.MaxReplicasThreshold != 2 || got.Reason != "Controle de custos" || got.CreatedBy != "a@x" {
		t.Fatalf("upsert deveria atualizar limite/razão e manter o autor: %+v", got)
	}
	if other, _ := s.ListByCluster("outro"); len(other) != 0 {
		t.Errorf("isenção vazou para outro cluster")
	}
	if found, err := s.Delete("aks-hlg", "pay", "api"); err != nil || !found {
		t.Fatalf("delete: found=%v err=%v", found, err)
	}
	if found, _ := s.Delete("aks-hlg", "pay", "api"); found {
		t.Error("segundo delete não deveria achar nada")
	}
}
