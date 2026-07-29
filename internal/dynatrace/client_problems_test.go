package dynatrace

import "testing"

// TestMergeProblemsDedup_RemovesDuplicatesAcrossEntities cobre o caso real que motivou o merge:
// um Deployment com várias réplicas (classicFullStack, sem CLOUD_APPLICATION) resolve pra várias
// PROCESS_GROUP_INSTANCE — um mesmo problem geralmente afeta várias réplicas ao mesmo tempo, e
// apareceria duplicado no overlay sem essa deduplicação.
func TestMergeProblemsDedup_RemovesDuplicatesAcrossEntities(t *testing.T) {
	shared := Problem{ProblemID: "P-1", Title: "Container restarts"}
	onlyOnSecond := Problem{ProblemID: "P-2", Title: "Not all pods ready"}

	merged := mergeProblemsDedup([][]Problem{
		{shared},
		{shared, onlyOnSecond},
	})

	if len(merged) != 2 {
		t.Fatalf("esperado 2 problems únicos, got %d: %+v", len(merged), merged)
	}
	ids := map[string]bool{}
	for _, p := range merged {
		ids[p.ProblemID] = true
	}
	if !ids["P-1"] || !ids["P-2"] {
		t.Errorf("esperava P-1 e P-2 no resultado mesclado, got %+v", merged)
	}
}

func TestMergeProblemsDedup_EmptyInput(t *testing.T) {
	merged := mergeProblemsDedup(nil)
	if len(merged) != 0 {
		t.Errorf("esperado slice vazio, got %+v", merged)
	}
}

func TestMergeProblemsDedup_PreservesOrderOfFirstOccurrence(t *testing.T) {
	p1 := Problem{ProblemID: "P-1"}
	p2 := Problem{ProblemID: "P-2"}
	merged := mergeProblemsDedup([][]Problem{{p2}, {p1, p2}})
	if len(merged) != 2 || merged[0].ProblemID != "P-2" || merged[1].ProblemID != "P-1" {
		t.Errorf("ordem inesperada: %+v", merged)
	}
}
