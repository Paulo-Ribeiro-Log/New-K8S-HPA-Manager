package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s-hpa-manager/internal/monitoring/analyzer"
	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/monitoring/storage"
	"github.com/rs/zerolog/log"
)

// Collector unified collector that integrates K8s + Prometheus + Analyzer
type Collector struct {
	k8sClient  *K8sClient
	promClient *prometheus.Client
	cache      *storage.TimeSeriesCache
	detector   *analyzer.Detector
	cluster    *models.ClusterInfo
	config     *CollectorConfig
	mu         sync.RWMutex
}

// CollectorConfig configuração do collector
type CollectorConfig struct {
	ScanInterval      time.Duration // Intervalo entre scans (default: 30s)
	ExcludeNamespaces []string      // Namespaces to exclude
	EnablePrometheus  bool          // Enable Prometheus enrichment
	DetectorConfig    *analyzer.DetectorConfig
	CacheConfig       *storage.CacheConfig
}

// DefaultCollectorConfig retorna configuração padrão
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		ScanInterval:      30 * time.Second,
		ExcludeNamespaces: []string{},
		EnablePrometheus:  true,
		DetectorConfig:    analyzer.DefaultDetectorConfig(),
		CacheConfig:       nil, // usa default do cache
	}
}

// NewCollector cria um novo unified collector
func NewCollector(cluster *models.ClusterInfo, prometheusEndpoint string, config *CollectorConfig) (*Collector, error) {
	if config == nil {
		config = DefaultCollectorConfig()
	}

	// Cria K8s client
	k8sClient, err := NewK8sClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	// Testa conexão K8s
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := k8sClient.TestConnection(ctx); err != nil {
		return nil, fmt.Errorf("K8s connection test failed: %w", err)
	}

	// Cria cache
	cache := storage.NewTimeSeriesCache(config.CacheConfig)

	// Cria detector
	detector := analyzer.NewDetector(cache, config.DetectorConfig)

	collector := &Collector{
		k8sClient: k8sClient,
		cache:     cache,
		detector:  detector,
		cluster:   cluster,
		config:    config,
	}

	// Cria Prometheus client se habilitado
	if config.EnablePrometheus && prometheusEndpoint != "" {
		promClient, err := prometheus.NewClient(cluster.Name, prometheusEndpoint)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster.Name).
				Str("endpoint", prometheusEndpoint).
				Msg("Failed to create Prometheus client (will use MetricsServer only)")
		} else {
			collector.promClient = promClient
		}
	}

	log.Info().
		Str("cluster", cluster.Name).
		Bool("prometheus_enabled", collector.promClient != nil).
		Dur("scan_interval", config.ScanInterval).
		Msg("Collector created successfully")

	return collector, nil
}

// ScanResult resultado de um scan
type ScanResult struct {
	Cluster       string
	Timestamp     time.Time
	SnapshotsCount int
	Anomalies     []analyzer.Anomaly
	Duration      time.Duration
	Errors        []error
}

// Scan executa um scan completo do cluster
func (c *Collector) Scan(ctx context.Context) (*ScanResult, error) {
	startTime := time.Now()

	result := &ScanResult{
		Cluster:   c.cluster.Name,
		Timestamp: startTime,
		Errors:    []error{},
	}

	log.Debug().
		Str("cluster", c.cluster.Name).
		Msg("Starting cluster scan")

	// 1. Lista namespaces
	namespaces, err := c.k8sClient.ListNamespaces(ctx, c.config.ExcludeNamespaces)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	log.Debug().
		Str("cluster", c.cluster.Name).
		Int("namespaces", len(namespaces)).
		Msg("Namespaces listed")

	// 2. Para cada namespace, coleta HPAs
	for _, ns := range namespaces {
		hpas, err := c.k8sClient.ListHPAs(ctx, ns)
		if err != nil {
			log.Error().
				Err(err).
				Str("cluster", c.cluster.Name).
				Str("namespace", ns).
				Msg("Failed to list HPAs")
			result.Errors = append(result.Errors, fmt.Errorf("namespace %s: %w", ns, err))
			continue
		}

		// 3. Para cada HPA, coleta snapshot
		for _, hpa := range hpas {
			snapshot, err := c.k8sClient.CollectHPASnapshot(ctx, &hpa)
			if err != nil {
				log.Error().
					Err(err).
					Str("cluster", c.cluster.Name).
					Str("namespace", ns).
					Str("hpa", hpa.Name).
					Msg("Failed to collect HPA snapshot")
				result.Errors = append(result.Errors, fmt.Errorf("HPA %s/%s: %w", ns, hpa.Name, err))
				continue
			}

			// 4. Enriquece com Prometheus se disponível
			if c.promClient != nil && c.promClient.IsConnected() {
				if err := c.promClient.EnrichSnapshot(ctx, snapshot); err != nil {
					log.Debug().
						Err(err).
						Str("cluster", c.cluster.Name).
						Str("namespace", ns).
						Str("hpa", hpa.Name).
						Msg("Failed to enrich snapshot with Prometheus (using MetricsServer only)")
				}
			}

			// 5. Adiciona ao cache
			if err := c.cache.Add(snapshot); err != nil {
				log.Error().
					Err(err).
					Str("cluster", c.cluster.Name).
					Str("namespace", ns).
					Str("hpa", hpa.Name).
					Msg("Failed to add snapshot to cache")
				result.Errors = append(result.Errors, fmt.Errorf("cache add %s/%s: %w", ns, hpa.Name, err))
				continue
			}

			result.SnapshotsCount++
		}
	}

	// 6. Detecta anomalias
	detectionResult := c.detector.Detect()
	result.Anomalies = detectionResult.Anomalies

	result.Duration = time.Since(startTime)

	log.Info().
		Str("cluster", c.cluster.Name).
		Int("snapshots", result.SnapshotsCount).
		Int("anomalies", len(result.Anomalies)).
		Int("errors", len(result.Errors)).
		Dur("duration", result.Duration).
		Msg("Cluster scan completed")

	return result, nil
}

// StartMonitoring inicia o loop de monitoramento
func (c *Collector) StartMonitoring(ctx context.Context, resultChan chan<- *ScanResult) {
	log.Info().
		Str("cluster", c.cluster.Name).
		Dur("interval", c.config.ScanInterval).
		Msg("Starting monitoring loop")

	ticker := time.NewTicker(c.config.ScanInterval)
	defer ticker.Stop()

	// Primeiro scan imediato
	c.runScan(ctx, resultChan)

	// Loop de monitoramento
	for {
		select {
		case <-ctx.Done():
			log.Info().
				Str("cluster", c.cluster.Name).
				Msg("Monitoring loop stopped")
			return

		case <-ticker.C:
			c.runScan(ctx, resultChan)
		}
	}
}

// runScan executa um scan e envia resultado pelo channel
func (c *Collector) runScan(ctx context.Context, resultChan chan<- *ScanResult) {
	// Cria contexto com timeout para o scan
	scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result, err := c.Scan(scanCtx)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", c.cluster.Name).
			Msg("Scan failed")

		// Envia resultado com erro
		result = &ScanResult{
			Cluster:   c.cluster.Name,
			Timestamp: time.Now(),
			Errors:    []error{err},
		}
	}

	// Envia resultado pelo channel (non-blocking)
	select {
	case resultChan <- result:
	default:
		log.Warn().
			Str("cluster", c.cluster.Name).
			Msg("Result channel full, dropping scan result")
	}
}

// GetCache retorna o cache para acesso externo
func (c *Collector) GetCache() *storage.TimeSeriesCache {
	return c.cache
}

// GetDetector retorna o detector para acesso externo
func (c *Collector) GetDetector() *analyzer.Detector {
	return c.detector
}

// GetCluster retorna informações do cluster
func (c *Collector) GetCluster() *models.ClusterInfo {
	return c.cluster
}

// GetPrometheusClient retorna cliente Prometheus
func (c *Collector) GetPrometheusClient() *prometheus.Client {
	return c.promClient
}

// GetK8sClient retorna cliente K8s
func (c *Collector) GetK8sClient() *K8sClient {
	return c.k8sClient
}

// IsPrometheusConnected retorna se Prometheus está conectado
func (c *Collector) IsPrometheusConnected() bool {
	if c.promClient == nil {
		return false
	}
	return c.promClient.IsConnected()
}

// GetStats retorna estatísticas do collector
func (c *Collector) GetStats() *CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cacheStats := c.cache.Stats()

	return &CollectorStats{
		Cluster:           c.cluster.Name,
		TotalHPAs:         cacheStats.TotalHPAs,
		TotalSnapshots:    cacheStats.TotalSnapshots,
		MemoryUsage:       c.cache.MemoryUsage(),
		PrometheusEnabled: c.promClient != nil,
		PrometheusConnected: c.IsPrometheusConnected(),
	}
}

// CollectorStats estatísticas do collector
type CollectorStats struct {
	Cluster             string
	TotalHPAs           int
	TotalSnapshots      int
	MemoryUsage         int64
	PrometheusEnabled   bool
	PrometheusConnected bool
}
