package healthcheck

import (
	"context"
	"testing"
	"time"
)

func baseCorrelatedResult() *HealthCheckResult {
	return &HealthCheckResult{
		Cluster: "cluster-a",
		EventResults: []EventHealth{
			{
				Namespace:      "ns",
				Reason:         "FailedScheduling",
				Message:        "0/22 nodes are available: 10 Insufficient memory",
				Type:           "Warning",
				Severity:       SeverityCritical,
				Status:         StatusCritical,
				Count:          8,
				FirstTimestamp: time.Now().Add(-1 * time.Hour),
				LastTimestamp:  time.Now(),
				InvolvedKind:   "Deployment",
				InvolvedName:   "cargas-web",
			},
			{
				// Pod não é correlacionado — mesma exclusão de sempre (pods são efêmeros)
				Namespace:      "ns",
				Reason:         "Unhealthy",
				Message:        "Readiness probe failed",
				Type:           "Warning",
				Severity:       SeverityHigh,
				Status:         StatusCritical,
				Count:          3,
				FirstTimestamp: time.Now(),
				LastTimestamp:  time.Now(),
				InvolvedKind:   "Pod",
				InvolvedName:   "cargas-web-abc123",
			},
		},
		DynatraceResults: []DynatraceHealth{
			{
				ProblemID:    "P-1",
				DisplayID:    "P-1",
				Title:        "Nginx failure rate",
				DTSeverity:   "ERROR",
				Status:       StatusCritical,
				Severity:     SeverityCritical,
				K8sWorkloads: []string{"ns/cargas-web"},
			},
		},
	}
}

// TestCorrelate_PropagatesEventCountAndTimestamp garante que Count/FirstTimestamp do EventHealth
// chegam até o CorrelatedK8sIssue — antes desta feature eram descartados no meio do caminho.
func TestCorrelate_PropagatesEventCountAndTimestamp(t *testing.T) {
	result := baseCorrelatedResult()
	items := Correlate(context.Background(), nil, result)

	if len(items) != 1 {
		t.Fatalf("expected 1 correlated item, got %d", len(items))
	}

	var eventIssue *CorrelatedK8sIssue
	for i := range items[0].K8sIssues {
		if items[0].K8sIssues[i].ResourceKind == "Event" {
			eventIssue = &items[0].K8sIssues[i]
		}
	}
	if eventIssue == nil {
		t.Fatal("expected an Event issue in the correlated item")
	}
	if eventIssue.Count != 8 {
		t.Errorf("expected Count=8 propagated from EventHealth, got %d", eventIssue.Count)
	}
	if eventIssue.FirstTimestamp.IsZero() {
		t.Error("expected FirstTimestamp propagated from EventHealth, got zero value")
	}
}

// TestCorrelate_NilStoreSkipsChronicity garante que passar store=nil (ex: chamadores que não têm
// storage disponível) não gera panic e simplesmente deixa Chronicity vazio.
func TestCorrelate_NilStoreSkipsChronicity(t *testing.T) {
	result := baseCorrelatedResult()
	items := Correlate(context.Background(), nil, result)

	for _, issue := range items[0].K8sIssues {
		if issue.Chronicity != nil {
			t.Errorf("expected nil Chronicity when store is nil, got %+v", issue.Chronicity)
		}
	}
}

// TestCorrelate_AttachesChronicityFromStore garante que, com storage disponível e histórico já
// persistido, Correlate preenche Chronicity no issue do evento correspondente.
func TestCorrelate_AttachesChronicityFromStore(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	result := baseCorrelatedResult()
	deploymentEvent := result.EventResults[0]

	// Simula histórico de execuções anteriores já persistido, com contagem acima do limiar
	// crônico — precisa ser encontrado por Correlate ANTES do Save() desta execução (ver ordem
	// real no orchestrator: Correlate lê o estado anterior, Save atualiza para a próxima vez).
	seed := EventHealth{
		Namespace:      deploymentEvent.Namespace,
		Reason:         deploymentEvent.Reason,
		InvolvedKind:   deploymentEvent.InvolvedKind,
		InvolvedName:   deploymentEvent.InvolvedName,
		Count:          eventChronicCountThreshold,
		FirstTimestamp: time.Now().Add(-72 * time.Hour),
		LastTimestamp:  time.Now(),
	}
	if err := store.UpsertEventHistory(ctx, result.Cluster, deploymentEvent.Namespace, seed); err != nil {
		t.Fatalf("failed to seed event history: %v", err)
	}

	items := Correlate(ctx, store, result)

	var eventIssue *CorrelatedK8sIssue
	for i := range items[0].K8sIssues {
		if items[0].K8sIssues[i].ResourceKind == "Event" {
			eventIssue = &items[0].K8sIssues[i]
		}
	}
	if eventIssue == nil {
		t.Fatal("expected an Event issue in the correlated item")
	}
	if eventIssue.Chronicity == nil {
		t.Fatal("expected Chronicity populated from seeded store history")
	}
	if !eventIssue.Chronicity.IsChronic {
		t.Errorf("expected IsChronic=true from seeded history, got %+v", eventIssue.Chronicity)
	}
}
