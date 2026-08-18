package handlers

import (
	"testing"

	"k8s-hpa-manager/internal/spinnaker"
	"k8s-hpa-manager/internal/storage"
)

// TestToHistoryRecord_RoundTripsThroughFromHistoryRecord garante que persistir um RollbackInfo
// e reconstruí-lo depois (fallback quando o Gate não devolve nada dentro da janela atual)
// preserva os campos usados pelo frontend, e que fromHistoryRecord sempre marca FromCache.
func TestToHistoryRecord_RoundTripsThroughFromHistoryRecord(t *testing.T) {
	isRollback := true
	original := &spinnaker.RollbackInfo{
		Matched:               true,
		IsRollback:            &isRollback,
		RollbackType:          "implicit",
		LastCHGApplied:        "CHG0001234",
		PipelineExecutedAt:    1000,
		ExecutionStatus:       "TERMINAL",
		RollbackStartedAt:     1000,
		RollbackEndedAt:       2000,
		FailedCHG:             "CHG0001234",
		RollbackPipelineName:  "deploy-aks-global",
		SpinnakerExecutionID:  "exec-1",
		SpinnakerExecutionURL: "https://deck/exec-1",
	}

	rec := toHistoryRecord("cluster-x", "ns-x", "deploy-x", original)
	if rec.Cluster != "cluster-x" || rec.Namespace != "ns-x" || rec.DeploymentName != "deploy-x" {
		t.Fatalf("chave incorreta: %+v", rec)
	}
	if !rec.IsRollback {
		t.Error("IsRollback deveria ser true")
	}
	if rec.LastCHGApplied != "CHG0001234" {
		t.Errorf("LastCHGApplied = %q, want CHG0001234", rec.LastCHGApplied)
	}

	// UpdatedAt é preenchido pelo Upsert real (não por toHistoryRecord) — zero value aqui é
	// esperado, fromHistoryRecord só precisa não panicar e propagar o que tiver.
	rebuilt := fromHistoryRecord(rec)

	if !rebuilt.Matched {
		t.Error("fromHistoryRecord deveria sempre marcar Matched=true")
	}
	if !rebuilt.FromCache {
		t.Error("fromHistoryRecord deveria sempre marcar FromCache=true")
	}
	if rebuilt.IsRollback == nil || *rebuilt.IsRollback != true {
		t.Errorf("IsRollback = %v, want true", rebuilt.IsRollback)
	}
	if rebuilt.LastCHGApplied != original.LastCHGApplied {
		t.Errorf("LastCHGApplied = %q, want %q", rebuilt.LastCHGApplied, original.LastCHGApplied)
	}
	if rebuilt.ExecutionStatus != original.ExecutionStatus {
		t.Errorf("ExecutionStatus = %q, want %q", rebuilt.ExecutionStatus, original.ExecutionStatus)
	}
	if rebuilt.SpinnakerExecutionURL != original.SpinnakerExecutionURL {
		t.Errorf("SpinnakerExecutionURL = %q, want %q", rebuilt.SpinnakerExecutionURL, original.SpinnakerExecutionURL)
	}
}

// TestToHistoryRecord_NilIsRollback cobre o caso defensivo — DetectRollback nunca deveria
// retornar Matched=true com IsRollback nil, mas toHistoryRecord não deve panicar se acontecer.
func TestToHistoryRecord_NilIsRollback(t *testing.T) {
	info := &spinnaker.RollbackInfo{Matched: true, IsRollback: nil}
	rec := toHistoryRecord("c", "ns", "app", info)
	if rec.IsRollback {
		t.Error("IsRollback deveria ser false quando o ponteiro original é nil")
	}
}

// TestFromHistoryRecord_SetsCachedAtFromUpdatedAt confirma que o timestamp exposto ao frontend
// (CachedAt, epoch ms) vem do UpdatedAt persistido, não de time.Now() no momento da leitura —
// senão o frontend nunca saberia há quanto tempo o dado realmente é.
func TestFromHistoryRecord_SetsCachedAtFromUpdatedAt(t *testing.T) {
	rec := storage.SpinnakerRolloutRecord{
		Cluster: "c", Namespace: "ns", DeploymentName: "app",
	}
	rec.UpdatedAt = rec.UpdatedAt.AddDate(0, 0, -30) // ~30 dias atrás (zero value + delta, só pra ter um valor não-atual)

	info := fromHistoryRecord(rec)
	if info.CachedAt != rec.UpdatedAt.UnixMilli() {
		t.Errorf("CachedAt = %d, want %d (UpdatedAt.UnixMilli())", info.CachedAt, rec.UpdatedAt.UnixMilli())
	}
}
