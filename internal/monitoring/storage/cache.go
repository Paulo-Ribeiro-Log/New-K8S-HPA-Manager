package storage

import (
	"fmt"
	"math"
	"sync"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/rs/zerolog/log"
)

// TimeSeriesCache armazena histórico de snapshots em memória
type TimeSeriesCache struct {
	data          map[string]*models.TimeSeriesData
	maxDuration   time.Duration
	scanInterval  time.Duration
	maxSnapshots  int
	totalSnapshots int64
	persistence   *Persistence // Persistência em SQLite (opcional)
	mu            sync.RWMutex
}

// CacheConfig configuração do cache
type CacheConfig struct {
	MaxDuration  time.Duration // Quanto tempo manter histórico (default: 5min)
	ScanInterval time.Duration // Intervalo entre scans (default: 30s)
}

// DefaultCacheConfig retorna configuração padrão
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		MaxDuration:  5 * time.Minute,
		ScanInterval: 30 * time.Second,
	}
}

// NewTimeSeriesCache cria novo cache
func NewTimeSeriesCache(config *CacheConfig) *TimeSeriesCache {
	if config == nil {
		config = DefaultCacheConfig()
	}

	maxSnapshots := int(config.MaxDuration / config.ScanInterval)
	if maxSnapshots < 1 {
		maxSnapshots = 10 // default: 10 snapshots
	}

	log.Info().
		Dur("max_duration", config.MaxDuration).
		Dur("scan_interval", config.ScanInterval).
		Int("max_snapshots", maxSnapshots).
		Msg("TimeSeriesCache initialized")

	return &TimeSeriesCache{
		data:         make(map[string]*models.TimeSeriesData),
		maxDuration:  config.MaxDuration,
		scanInterval: config.ScanInterval,
		maxSnapshots: maxSnapshots,
		persistence:  nil, // Será configurado via SetPersistence()
	}
}

// SetPersistence configura persistência no cache
func (c *TimeSeriesCache) SetPersistence(p *Persistence) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.persistence = p

	if p != nil && p.config.Enabled {
		log.Info().Msg("Persistence enabled for cache")

		// Carrega snapshots existentes do banco
		go c.loadFromPersistence()
	}
}

// loadFromPersistence carrega snapshots do banco ao iniciar
func (c *TimeSeriesCache) loadFromPersistence() {
	if c.persistence == nil || !c.persistence.config.Enabled {
		return
	}

	// Carrega snapshots dos últimos MaxDuration (5min)
	since := time.Now().Add(-c.maxDuration)

	snapshots, err := c.persistence.LoadAll(since)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load snapshots from database")
		return
	}

	if len(snapshots) == 0 {
		log.Info().Msg("No snapshots loaded from database")
		return
	}

	// Adiciona snapshots ao cache
	c.mu.Lock()
	defer c.mu.Unlock()

	loadedCount := 0
	for key, snaps := range snapshots {
		ts := &models.TimeSeriesData{
			HPAKey:      key,
			Snapshots:   snaps,
			MaxDuration: c.maxDuration,
		}
		c.data[key] = ts
		c.calculateStats(ts)
		loadedCount += len(snaps)
	}

	log.Info().
		Int("hpas", len(snapshots)).
		Int("snapshots", loadedCount).
		Msg("Snapshots loaded from database into cache")
}

// Add adiciona snapshot ao cache
func (c *TimeSeriesCache) Add(snapshot *models.HPASnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	key := makeKey(snapshot.Cluster, snapshot.Namespace, snapshot.Name)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Cria TimeSeriesData se não existir
	ts, exists := c.data[key]
	if !exists {
		ts = &models.TimeSeriesData{
			HPAKey:      key,
			Snapshots:   make([]models.HPASnapshot, 0, c.maxSnapshots),
			MaxDuration: c.maxDuration,
		}
		c.data[key] = ts
		log.Debug().
			Str("key", key).
			Msg("New HPA tracked")
	}

	// Adiciona snapshot
	ts.Add(*snapshot)
	c.totalSnapshots++

	// Limpa snapshots antigos
	c.cleanupOld(ts)

	// Calcula estatísticas
	c.calculateStats(ts)

	// Salva no banco (async)
	if c.persistence != nil && c.persistence.config.Enabled {
		go func(s *models.HPASnapshot) {
			if err := c.persistence.SaveSnapshot(s); err != nil {
				log.Warn().
					Err(err).
					Str("hpa", s.Name).
					Msg("Failed to persist snapshot")
			}
		}(snapshot)
	}

	return nil
}

// Get retorna TimeSeriesData para uma chave
func (c *TimeSeriesCache) Get(cluster, namespace, name string) *models.TimeSeriesData {
	key := makeKey(cluster, namespace, name)

	c.mu.RLock()
	defer c.mu.RUnlock()

	ts, exists := c.data[key]
	if !exists {
		return nil
	}

	// Retorna cópia para evitar race conditions
	return ts
}

// GetLatestSnapshot retorna snapshot mais recente
func (c *TimeSeriesCache) GetLatestSnapshot(cluster, namespace, name string) *models.HPASnapshot {
	ts := c.Get(cluster, namespace, name)
	if ts == nil {
		return nil
	}

	return ts.GetLatest()
}

// GetAll retorna todos os TimeSeriesData
func (c *TimeSeriesCache) GetAll() map[string]*models.TimeSeriesData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Retorna cópia
	result := make(map[string]*models.TimeSeriesData, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}

	return result
}

// GetByCluster retorna todos TimeSeriesData de um cluster
func (c *TimeSeriesCache) GetByCluster(cluster string) map[string]*models.TimeSeriesData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*models.TimeSeriesData)
	prefix := cluster + "/"

	for k, v := range c.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			result[k] = v
		}
	}

	return result
}

// Delete remove TimeSeriesData do cache
func (c *TimeSeriesCache) Delete(cluster, namespace, name string) {
	key := makeKey(cluster, namespace, name)

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	log.Debug().
		Str("key", key).
		Msg("HPA removed from cache")
}

// Clear limpa todo o cache
func (c *TimeSeriesCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]*models.TimeSeriesData)
	c.totalSnapshots = 0
	log.Info().Msg("Cache cleared")
}

// Stats retorna estatísticas do cache
func (c *TimeSeriesCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalSnapshots := 0
	for _, ts := range c.data {
		totalSnapshots += len(ts.Snapshots)
	}

	return CacheStats{
		TotalHPAs:         len(c.data),
		TotalSnapshots:    totalSnapshots,
		TotalSnapshotsAll: c.totalSnapshots,
		MaxDuration:       c.maxDuration,
		MaxSnapshots:      c.maxSnapshots,
	}
}

// CacheStats estatísticas do cache
type CacheStats struct {
	TotalHPAs         int
	TotalSnapshots    int
	TotalSnapshotsAll int64
	MaxDuration       time.Duration
	MaxSnapshots      int
}

// cleanupOld remove snapshots antigos (> MaxDuration)
func (c *TimeSeriesCache) cleanupOld(ts *models.TimeSeriesData) {
	cutoff := time.Now().Add(-c.maxDuration)
	validSnapshots := make([]models.HPASnapshot, 0, len(ts.Snapshots))

	for _, s := range ts.Snapshots {
		if s.Timestamp.After(cutoff) {
			validSnapshots = append(validSnapshots, s)
		}
	}

	ts.Snapshots = validSnapshots
}

// calculateStats calcula estatísticas dos snapshots
func (c *TimeSeriesCache) calculateStats(ts *models.TimeSeriesData) {
	if len(ts.Snapshots) == 0 {
		return
	}

	// Inicializa stats
	stats := models.HPAStats{}

	// Coleta valores
	cpuValues := make([]float64, 0, len(ts.Snapshots))
	memValues := make([]float64, 0, len(ts.Snapshots))
	replicaValues := make([]int32, 0, len(ts.Snapshots))

	for _, s := range ts.Snapshots {
		if s.CPUCurrent > 0 {
			cpuValues = append(cpuValues, s.CPUCurrent)
		}
		if s.MemoryCurrent > 0 {
			memValues = append(memValues, s.MemoryCurrent)
		}
		replicaValues = append(replicaValues, s.CurrentReplicas)
	}

	// CPU Statistics
	if len(cpuValues) > 0 {
		stats.CPUAverage = average(cpuValues)
		stats.CPUMin = min(cpuValues)
		stats.CPUMax = max(cpuValues)
		stats.CPUStdDev = stdDev(cpuValues, stats.CPUAverage)
		stats.CPUTrend = calculateTrend(cpuValues)
	}

	// Memory Statistics
	if len(memValues) > 0 {
		stats.MemoryAverage = average(memValues)
		stats.MemoryMin = min(memValues)
		stats.MemoryMax = max(memValues)
		stats.MemoryStdDev = stdDev(memValues, stats.MemoryAverage)
		stats.MemoryTrend = calculateTrend(memValues)
	}

	// Replica Changes
	stats.ReplicaChanges = 0
	for i := 1; i < len(replicaValues); i++ {
		if replicaValues[i] != replicaValues[i-1] {
			stats.ReplicaChanges++
			stats.LastChange = ts.Snapshots[i].Timestamp
		}
	}

	if len(replicaValues) > 0 {
		stats.ReplicaTrend = calculateReplicaTrend(replicaValues)
	}

	ts.Stats = stats
}

// makeKey cria chave única para HPA
func makeKey(cluster, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", cluster, namespace, name)
}

// average calcula média
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// min retorna valor mínimo
func min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	minVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}

// max retorna valor máximo
func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	maxVal := values[0]
	for _, v := range values[1:] {
		if v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

// stdDev calcula desvio padrão
func stdDev(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sumSquares := 0.0
	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(values))
	return math.Sqrt(variance)
}

// calculateTrend determina tendência (increasing, decreasing, stable)
func calculateTrend(values []float64) string {
	if len(values) < 3 {
		return "stable"
	}

	// Calcula média dos primeiros 1/3 e últimos 1/3
	third := len(values) / 3
	if third < 1 {
		third = 1
	}

	firstThird := values[:third]
	lastThird := values[len(values)-third:]

	avgFirst := average(firstThird)
	avgLast := average(lastThird)

	// Considera trend se diferença > 10%
	percentChange := ((avgLast - avgFirst) / avgFirst) * 100

	if percentChange > 10 {
		return "increasing"
	} else if percentChange < -10 {
		return "decreasing"
	}

	return "stable"
}

// calculateReplicaTrend determina tendência de réplicas
func calculateReplicaTrend(values []int32) string {
	if len(values) < 2 {
		return "stable"
	}

	first := values[0]
	last := values[len(values)-1]

	if last > first {
		return "increasing"
	} else if last < first {
		return "decreasing"
	}

	return "stable"
}

// Cleanup executa limpeza periódica do cache
func (c *TimeSeriesCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-c.maxDuration)
	removed := 0

	for key, ts := range c.data {
		// Remove HPAs sem snapshots recentes
		if len(ts.Snapshots) == 0 {
			delete(c.data, key)
			removed++
			continue
		}

		// Limpa snapshots antigos
		c.cleanupOld(ts)

		// Remove HPA se último snapshot é muito antigo (>2x MaxDuration)
		latest := ts.GetLatest()
		if latest != nil && latest.Timestamp.Before(cutoff.Add(-c.maxDuration)) {
			delete(c.data, key)
			removed++
		}
	}

	if removed > 0 {
		log.Debug().
			Int("removed", removed).
			Msg("Cleanup: removed stale HPAs")
	}
}

// MemoryUsage estima uso de memória
func (c *TimeSeriesCache) MemoryUsage() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Estimativa simplificada
	// HPASnapshot ~ 500 bytes
	// TimeSeriesData overhead ~ 200 bytes
	const snapshotSize = 500
	const tsOverhead = 200

	totalBytes := int64(0)
	for _, ts := range c.data {
		totalBytes += int64(len(ts.Snapshots)*snapshotSize + tsOverhead)
	}

	return totalBytes
}

// Close fecha recursos do cache (persistence)
func (c *TimeSeriesCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.persistence != nil {
		log.Info().Msg("Closing cache persistence")
		return c.persistence.Close()
	}

	return nil
}
