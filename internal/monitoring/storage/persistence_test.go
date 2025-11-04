package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
)

func TestPersistence(t *testing.T) {
	// Cria DB temporário
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := &PersistenceConfig{
		Enabled:     true,
		DBPath:      dbPath,
		MaxAge:      24 * time.Hour,
		BatchSize:   100,
		AutoCleanup: true,
	}

	// Cria persistência
	p, err := NewPersistence(config)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer p.Close()

	// Verifica que DB foi criado
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Database file was not created")
	}

	// Cria snapshots de teste
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "cluster1",
		Namespace:       "namespace1",
		Name:            "hpa1",
		CurrentReplicas: 3,
		CPUCurrent:      50.0,
	}

	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(30 * time.Second),
		Cluster:         "cluster1",
		Namespace:       "namespace1",
		Name:            "hpa1",
		CurrentReplicas: 5,
		CPUCurrent:      75.0,
	}

	// Salva snapshots
	if err := p.SaveSnapshot(snapshot1); err != nil {
		t.Fatalf("Failed to save snapshot1: %v", err)
	}

	if err := p.SaveSnapshot(snapshot2); err != nil {
		t.Fatalf("Failed to save snapshot2: %v", err)
	}

	// Carrega snapshots
	since := time.Now().Add(-1 * time.Hour)
	snapshots, err := p.LoadSnapshots("cluster1", "namespace1", "hpa1", since)
	if err != nil {
		t.Fatalf("Failed to load snapshots: %v", err)
	}

	if len(snapshots) != 2 {
		t.Errorf("Expected 2 snapshots, got %d", len(snapshots))
	}

	// Verifica dados
	if snapshots[0].CurrentReplicas != 3 {
		t.Errorf("Expected 3 replicas, got %d", snapshots[0].CurrentReplicas)
	}

	if snapshots[1].CurrentReplicas != 5 {
		t.Errorf("Expected 5 replicas, got %d", snapshots[1].CurrentReplicas)
	}
}

func TestPersistenceBatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_batch.db")

	config := &PersistenceConfig{
		Enabled:   true,
		DBPath:    dbPath,
		MaxAge:    24 * time.Hour,
		BatchSize: 100,
	}

	p, err := NewPersistence(config)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer p.Close()

	// Cria múltiplos snapshots
	snapshots := make([]*models.HPASnapshot, 50)
	for i := 0; i < 50; i++ {
		snapshots[i] = &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * time.Second),
			Cluster:         "cluster1",
			Namespace:       "namespace1",
			Name:            "hpa1",
			CurrentReplicas: int32(i + 1),
			CPUCurrent:      float64(i * 2),
		}
	}

	// Salva em batch
	if err := p.SaveSnapshots(snapshots); err != nil {
		t.Fatalf("Failed to save batch: %v", err)
	}

	// Carrega todos
	since := time.Now().Add(-1 * time.Hour)
	loaded, err := p.LoadSnapshots("cluster1", "namespace1", "hpa1", since)
	if err != nil {
		t.Fatalf("Failed to load snapshots: %v", err)
	}

	if len(loaded) != 50 {
		t.Errorf("Expected 50 snapshots, got %d", len(loaded))
	}
}

func TestPersistenceMultipleHPAs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_multi.db")

	config := &PersistenceConfig{
		Enabled: true,
		DBPath:  dbPath,
		MaxAge:  24 * time.Hour,
	}

	p, err := NewPersistence(config)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer p.Close()

	// Cria snapshots de múltiplos HPAs
	clusters := []string{"cluster1", "cluster2"}
	namespaces := []string{"ns1", "ns2"}
	hpas := []string{"hpa1", "hpa2"}

	totalSaved := 0
	for _, cluster := range clusters {
		for _, namespace := range namespaces {
			for _, hpa := range hpas {
				snapshot := &models.HPASnapshot{
					Timestamp:       time.Now(),
					Cluster:         cluster,
					Namespace:       namespace,
					Name:            hpa,
					CurrentReplicas: 3,
					CPUCurrent:      50.0,
				}
				if err := p.SaveSnapshot(snapshot); err != nil {
					t.Fatalf("Failed to save snapshot: %v", err)
				}
				totalSaved++
			}
		}
	}

	// Carrega todos
	since := time.Now().Add(-1 * time.Hour)
	all, err := p.LoadAll(since)
	if err != nil {
		t.Fatalf("Failed to load all: %v", err)
	}

	if len(all) != totalSaved {
		t.Errorf("Expected %d HPAs, got %d", totalSaved, len(all))
	}

	t.Logf("Saved and loaded %d HPAs across %d clusters", totalSaved, len(clusters))
}

func TestPersistenceStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	config := &PersistenceConfig{
		Enabled: true,
		DBPath:  dbPath,
		MaxAge:  24 * time.Hour,
	}

	p, err := NewPersistence(config)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer p.Close()

	// Salva alguns snapshots
	for i := 0; i < 10; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * time.Second),
			Cluster:         "cluster1",
			Namespace:       "ns1",
			Name:            "hpa1",
			CurrentReplicas: 3,
			CPUCurrent:      50.0,
		}
		if err := p.SaveSnapshot(snapshot); err != nil {
			t.Fatalf("Failed to save snapshot: %v", err)
		}
	}

	// Verifica stats
	stats, err := p.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if !stats.Enabled {
		t.Error("Expected persistence to be enabled")
	}

	if stats.TotalSnapshots != 10 {
		t.Errorf("Expected 10 snapshots, got %d", stats.TotalSnapshots)
	}

	if stats.TotalHPAs != 1 {
		t.Errorf("Expected 1 HPA, got %d", stats.TotalHPAs)
	}

	if stats.DBSize == 0 {
		t.Error("Expected non-zero DB size")
	}

	t.Logf("DB Stats: %d snapshots, %d HPAs, %d bytes",
		stats.TotalSnapshots, stats.TotalHPAs, stats.DBSize)
}

func TestPersistenceCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_cleanup.db")

	config := &PersistenceConfig{
		Enabled:     true,
		DBPath:      dbPath,
		MaxAge:      3 * time.Second, // 3 segundos para teste
		AutoCleanup: false,           // Manual cleanup
	}

	p, err := NewPersistence(config)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer p.Close()

	// Salva snapshot antigo
	oldSnapshot := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-5 * time.Second), // 5s atrás (será removido)
		Cluster:         "cluster1",
		Namespace:       "ns1",
		Name:            "hpa1",
		CurrentReplicas: 3,
		CPUCurrent:      50.0,
	}
	if err := p.SaveSnapshot(oldSnapshot); err != nil {
		t.Fatalf("Failed to save old snapshot: %v", err)
	}

	// Aguarda 1s antes de salvar o snapshot recente
	time.Sleep(1 * time.Second)

	// Salva snapshot recente (menos de 3s atrás - não será removido)
	newSnapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "cluster1",
		Namespace:       "ns1",
		Name:            "hpa1",
		CurrentReplicas: 5,
		CPUCurrent:      75.0,
	}
	if err := p.SaveSnapshot(newSnapshot); err != nil {
		t.Fatalf("Failed to save new snapshot: %v", err)
	}

	// Stats antes do cleanup
	statsBefore, err := p.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats before cleanup: %v", err)
	}
	if statsBefore.TotalSnapshots != 2 {
		t.Errorf("Expected 2 snapshots before cleanup, got %d", statsBefore.TotalSnapshots)
	}

	// Executa cleanup (removerá apenas o snapshot de 5s atrás)
	if err := p.Cleanup(); err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	// Stats depois do cleanup
	statsAfter, err := p.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats after cleanup: %v", err)
	}
	if statsAfter.TotalSnapshots != 1 {
		t.Errorf("Expected 1 snapshot after cleanup, got %d", statsAfter.TotalSnapshots)
	}

	t.Logf("Cleanup: %d → %d snapshots", statsBefore.TotalSnapshots, statsAfter.TotalSnapshots)
}

func TestCacheWithPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_cache.db")

	// Cria persistência
	persistConfig := &PersistenceConfig{
		Enabled: true,
		DBPath:  dbPath,
		MaxAge:  24 * time.Hour,
	}
	persist, err := NewPersistence(persistConfig)
	if err != nil {
		t.Fatalf("Failed to create persistence: %v", err)
	}
	defer persist.Close()

	// Cria cache
	cacheConfig := &CacheConfig{
		MaxDuration:  5 * time.Minute,
		ScanInterval: 30 * time.Second,
	}
	cache := NewTimeSeriesCache(cacheConfig)
	defer cache.Close()

	// Conecta persistência ao cache
	cache.SetPersistence(persist)

	// Adiciona snapshots
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "cluster1",
		Namespace:       "ns1",
		Name:            "hpa1",
		CurrentReplicas: 3,
		CPUCurrent:      50.0,
	}

	if err := cache.Add(snapshot1); err != nil {
		t.Fatalf("Failed to add snapshot: %v", err)
	}

	// Aguarda persistência async
	time.Sleep(100 * time.Millisecond)

	// Verifica que foi salvo
	stats, _ := persist.Stats()
	if stats.TotalSnapshots < 1 {
		t.Error("Expected snapshot to be persisted")
	}

	t.Logf("Cache + Persistence: %d snapshots saved", stats.TotalSnapshots)
}

func TestPersistenceLoadOnStartup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_load.db")

	// Fase 1: Cria cache e salva dados
	{
		persist, _ := NewPersistence(&PersistenceConfig{
			Enabled: true,
			DBPath:  dbPath,
			MaxAge:  24 * time.Hour,
		})

		cache := NewTimeSeriesCache(nil)
		cache.SetPersistence(persist)

		// Adiciona snapshots
		for i := 0; i < 5; i++ {
			snapshot := &models.HPASnapshot{
				Timestamp:       time.Now().Add(time.Duration(i) * time.Second),
				Cluster:         "cluster1",
				Namespace:       "ns1",
				Name:            "hpa1",
				CurrentReplicas: int32(i + 1),
				CPUCurrent:      float64(i * 10),
			}
			cache.Add(snapshot)
		}

		time.Sleep(500 * time.Millisecond) // Aguarda persistência
		cache.Close()
	}

	// Fase 2: Reinicia e carrega do banco
	{
		persist, _ := NewPersistence(&PersistenceConfig{
			Enabled: true,
			DBPath:  dbPath,
			MaxAge:  24 * time.Hour,
		})
		defer persist.Close()

		cache := NewTimeSeriesCache(nil)
		defer cache.Close()

		cache.SetPersistence(persist)

		// Aguarda carregamento async
		time.Sleep(500 * time.Millisecond)

		// Verifica que dados foram carregados
		ts := cache.Get("cluster1", "ns1", "hpa1")
		if ts == nil {
			t.Fatal("Expected to load HPA from persistence")
		}

		if len(ts.Snapshots) != 5 {
			t.Errorf("Expected 5 snapshots loaded, got %d", len(ts.Snapshots))
		}

		t.Logf("Successfully loaded %d snapshots from persistence", len(ts.Snapshots))
	}
}
