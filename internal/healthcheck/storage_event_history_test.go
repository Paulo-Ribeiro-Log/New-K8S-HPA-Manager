package healthcheck

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStorage(t *testing.T) *HealthCheckStorage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_health_check.db")
	s, err := NewHealthCheckStorage(dbPath)
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestEventChronicity_FirstSighting cobre o caso mais comum: evento nunca visto antes.
// GetEventChronicity deve retornar (nil, nil) — não é erro, é o estado esperado.
func TestEventChronicity_FirstSighting(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "FailedScheduling")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity != nil {
		t.Fatalf("expected nil chronicity for unseen event, got %+v", chronicity)
	}
}

// TestEventChronicity_SameCycleUpdate cobre o caso em que o Count sobe entre execuções porque o
// MESMO objeto de evento no apiserver segue acumulando (ainda não expirou) — não deve somar,
// apenas refletir o valor mais recente.
func TestEventChronicity_SameCycleUpdate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	firstSeen := time.Now().Add(-2 * time.Hour)

	event := EventHealth{
		Namespace:      "ns",
		Reason:         "FailedScheduling",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          5,
		FirstTimestamp: firstSeen,
		LastTimestamp:  firstSeen,
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Mesmo ciclo: Count sobe de 5 para 10 (objeto de evento ainda vivo)
	event.Count = 10
	event.LastTimestamp = time.Now()
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "FailedScheduling")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity == nil {
		t.Fatal("expected non-nil chronicity after upserts")
	}
	if chronicity.CumulativeCount != 10 {
		t.Errorf("expected cumulative count 10 (same cycle, no double counting), got %d", chronicity.CumulativeCount)
	}
}

// TestEventChronicity_CycleReset cobre o caso central do design: o Count atual é MENOR que o
// último conhecido, indicando que o objeto de evento no apiserver expirou e foi recriado — o
// ciclo anterior deve ser somado ao acumulado, não descartado.
func TestEventChronicity_CycleReset(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	firstSeen := time.Now().Add(-72 * time.Hour)

	event := EventHealth{
		Namespace:      "ns",
		Reason:         "BackOff",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          10,
		FirstTimestamp: firstSeen,
		LastTimestamp:  firstSeen.Add(time.Hour),
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Objeto de evento expirou e foi recriado: Count reseta para 2 (< 10 conhecido)
	event.Count = 2
	event.FirstTimestamp = time.Now() // novo objeto, novo FirstTimestamp — mas first_seen_ever não deve regredir
	event.LastTimestamp = time.Now()
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("second upsert (cycle reset) failed: %v", err)
	}

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "BackOff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity == nil {
		t.Fatal("expected non-nil chronicity after upserts")
	}
	if chronicity.CumulativeCount != 12 {
		t.Errorf("expected cumulative count 12 (10 from closed cycle + 2 from new cycle), got %d", chronicity.CumulativeCount)
	}
	if !chronicity.FirstSeenEver.Equal(firstSeen) {
		t.Errorf("expected first_seen_ever to stay at the original %v (never regress), got %v", firstSeen, chronicity.FirstSeenEver)
	}
}

// TestEventChronicity_IsChronicByCount cobre o limiar de contagem acumulada.
func TestEventChronicity_IsChronicByCount(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	event := EventHealth{
		Namespace:      "ns",
		Reason:         "Unhealthy",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          eventChronicCountThreshold,
		FirstTimestamp: time.Now(), // recente — só o count deve disparar IsChronic
		LastTimestamp:  time.Now(),
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "Unhealthy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity == nil || !chronicity.IsChronic {
		t.Fatalf("expected IsChronic=true when cumulative count reaches threshold, got %+v", chronicity)
	}
}

// TestEventChronicity_IsChronicByAge cobre o limiar de idade — poucas ocorrências mas há muito tempo.
func TestEventChronicity_IsChronicByAge(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	event := EventHealth{
		Namespace:      "ns",
		Reason:         "Unhealthy",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          3, // bem abaixo do limiar de contagem
		FirstTimestamp: time.Now().Add(-eventChronicAge - 24*time.Hour),
		LastTimestamp:  time.Now(),
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "Unhealthy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity == nil || !chronicity.IsChronic {
		t.Fatalf("expected IsChronic=true when first_seen_ever is older than threshold, got %+v", chronicity)
	}
}

// TestEventChronicity_NotChronic cobre o caso "agudo": poucas ocorrências, recente.
func TestEventChronicity_NotChronic(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	event := EventHealth{
		Namespace:      "ns",
		Reason:         "FailedScheduling",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          3,
		FirstTimestamp: time.Now().Add(-1 * time.Hour),
		LastTimestamp:  time.Now(),
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", event); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	chronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "FailedScheduling")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chronicity == nil {
		t.Fatal("expected non-nil chronicity")
	}
	if chronicity.IsChronic {
		t.Errorf("expected IsChronic=false for a recent, low-count event, got %+v", chronicity)
	}
}

// TestEventChronicity_DistinctKeys garante que eventos com chaves diferentes (namespace, recurso
// ou reason) não se misturam no histórico acumulado.
func TestEventChronicity_DistinctKeys(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	base := EventHealth{
		Namespace:      "ns",
		Reason:         "FailedScheduling",
		InvolvedKind:   "Deployment",
		InvolvedName:   "app",
		Count:          5,
		FirstTimestamp: time.Now(),
		LastTimestamp:  time.Now(),
	}
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", base); err != nil {
		t.Fatalf("upsert base failed: %v", err)
	}

	otherReason := base
	otherReason.Reason = "BackOff"
	if err := s.UpsertEventHistory(ctx, "cluster-a", "ns", otherReason); err != nil {
		t.Fatalf("upsert other reason failed: %v", err)
	}

	baseChronicity, err := s.GetEventChronicity(ctx, "cluster-a", "ns", "Deployment", "app", "FailedScheduling")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if baseChronicity == nil || baseChronicity.CumulativeCount != 5 {
		t.Errorf("expected FailedScheduling untouched by BackOff upsert, got %+v", baseChronicity)
	}
}
