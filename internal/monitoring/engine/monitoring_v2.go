package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s-hpa-manager/internal/monitoring/cache"
	"k8s-hpa-manager/internal/monitoring/client"
	"k8s-hpa-manager/internal/monitoring/discovery"

	"github.com/rs/zerolog/log"
)

// HPAMetrics representa métricas de um HPA
type HPAMetrics struct {
	Cluster          string
	Namespace        string
	Name             string
	Timestamp        time.Time
	CurrentReplicas  float64
	MinReplicas      int
	MaxReplicas      int
	DesiredReplicas  int
	CPUCurrent       float64
	CPUTarget        float64
	CPURequest       string  // Formatado como string (ex: "2500m")
	CPULimit         string  // Formatado como string (ex: "5000m")
	MemoryCurrent    float64
	MemoryTarget     float64
	MemoryRequest    string  // Formatado como string (ex: "2684354560")
	MemoryLimit      string  // Formatado como string (ex: "5368709120")
}

// MonitoringEngineV2 nova engine de monitoramento sem port-forwards
type MonitoringEngineV2 struct {
	cache         *cache.MemoryCache
	clients       map[string]*client.PrometheusClient
	clientsMutex  sync.RWMutex
	running       bool
	runningMutex  sync.RWMutex
	stopCh        chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewMonitoringEngineV2 cria nova engine de monitoramento
func NewMonitoringEngineV2() *MonitoringEngineV2 {
	ctx, cancel := context.WithCancel(context.Background())

	engine := &MonitoringEngineV2{
		cache:    cache.NewMemoryCache(1 * time.Hour), // TTL de 1 hora
		clients:  make(map[string]*client.PrometheusClient),
		running:  false,
		stopCh:   make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}

	log.Info().Msg("MonitoringEngineV2 criada")

	return engine
}

// Start inicia a engine de monitoramento
func (e *MonitoringEngineV2) Start() error {
	e.runningMutex.Lock()
	defer e.runningMutex.Unlock()

	if e.running {
		return fmt.Errorf("engine já está rodando")
	}

	e.running = true

	log.Info().Msg("MonitoringEngineV2 iniciada")

	return nil
}

// Stop para a engine de monitoramento
func (e *MonitoringEngineV2) Stop() error {
	e.runningMutex.Lock()
	defer e.runningMutex.Unlock()

	if !e.running {
		return fmt.Errorf("engine não está rodando")
	}

	e.running = false
	e.cancel()
	close(e.stopCh)

	// Fechar todos os clientes
	e.clientsMutex.Lock()
	for cluster, client := range e.clients {
		client.Close()
		log.Debug().Str("cluster", cluster).Msg("Cliente fechado")
	}
	e.clients = make(map[string]*client.PrometheusClient)
	e.clientsMutex.Unlock()

	// Limpar cache
	e.cache.Clear()

	log.Info().Msg("MonitoringEngineV2 parada")

	return nil
}

// IsRunning retorna se a engine está rodando
func (e *MonitoringEngineV2) IsRunning() bool {
	e.runningMutex.RLock()
	defer e.runningMutex.RUnlock()
	return e.running
}

// getOrCreateClient retorna cliente Prometheus existente ou cria novo
func (e *MonitoringEngineV2) getOrCreateClient(cluster string) (*client.PrometheusClient, error) {
	// Tentar obter cliente existente
	e.clientsMutex.RLock()
	if existingClient, exists := e.clients[cluster]; exists {
		e.clientsMutex.RUnlock()
		return existingClient, nil
	}
	e.clientsMutex.RUnlock()

	// Criar novo cliente
	e.clientsMutex.Lock()
	defer e.clientsMutex.Unlock()

	// Double-check após adquirir write lock
	if existingClient, exists := e.clients[cluster]; exists {
		return existingClient, nil
	}

	newClient, err := client.NewPrometheusClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar cliente: %w", err)
	}

	e.clients[cluster] = newClient

	log.Info().
		Str("cluster", cluster).
		Msg("Cliente Prometheus criado e cacheado")

	return newClient, nil
}

// GetHPAMetrics busca métricas de um HPA (com cache)
func (e *MonitoringEngineV2) GetHPAMetrics(cluster, namespace, hpaName string) (*HPAMetrics, error) {
	cacheKey := fmt.Sprintf("hpa:%s:%s:%s", cluster, namespace, hpaName)

	// Tentar buscar do cache
	if cached, exists := e.cache.Get(cacheKey); exists {
		log.Debug().Str("key", cacheKey).Msg("Métricas do cache (HPA)")
		return cached.(*HPAMetrics), nil
	}

	// Cache miss - buscar do Prometheus
	promClient, err := e.getOrCreateClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter cliente: %w", err)
	}

	metrics, err := promClient.GetHPAMetrics(e.ctx, namespace, hpaName)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar métricas: %w", err)
	}

	// Converter para HPAMetrics
	snapshot := &HPAMetrics{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      hpaName,
		Timestamp: time.Now(),
	}

	// Extrair métricas do map
	if currentReplicas, ok := metrics["current_replicas"].(string); ok {
		snapshot.CurrentReplicas = parseFloat(currentReplicas)
	}

	if minReplicas, ok := metrics["min_replicas"].(string); ok {
		snapshot.MinReplicas = int(parseFloat(minReplicas))
	}

	if maxReplicas, ok := metrics["max_replicas"].(string); ok {
		snapshot.MaxReplicas = int(parseFloat(maxReplicas))
	}

	if desiredReplicas, ok := metrics["desired_replicas"].(string); ok {
		snapshot.DesiredReplicas = int(parseFloat(desiredReplicas))
	}

	if cpuUsage, ok := metrics["cpu_usage_percent"].(string); ok {
		snapshot.CPUCurrent = parseFloat(cpuUsage)
	}

	if memUsage, ok := metrics["memory_usage_percent"].(string); ok {
		snapshot.MemoryCurrent = parseFloat(memUsage)
	}

	if cpuRequest, ok := metrics["cpu_request"].(string); ok {
		snapshot.CPURequest = cpuRequest // Mantém como string
	}

	if cpuLimit, ok := metrics["cpu_limit"].(string); ok {
		snapshot.CPULimit = cpuLimit // Mantém como string
	}

	if memRequest, ok := metrics["memory_request"].(string); ok {
		snapshot.MemoryRequest = memRequest // Mantém como string
	}

	if memLimit, ok := metrics["memory_limit"].(string); ok {
		snapshot.MemoryLimit = memLimit // Mantém como string
	}

	// Adicionar ao cache
	e.cache.Set(cacheKey, snapshot)

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("Métricas HPA coletadas")

	return snapshot, nil
}

// GetHPAHistoricalMetrics busca métricas históricas de um HPA
func (e *MonitoringEngineV2) GetHPAHistoricalMetrics(cluster, namespace, hpaName string, duration time.Duration, step time.Duration) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("hpa_hist:%s:%s:%s:%s:%s", cluster, namespace, hpaName, duration, step)

	// Usar GetOrSet do cache para evitar múltiplas queries simultâneas
	result, err := e.cache.GetOrSet(cacheKey, func() (interface{}, error) {
		promClient, err := e.getOrCreateClient(cluster)
		if err != nil {
			return nil, fmt.Errorf("erro ao obter cliente: %w", err)
		}

		historical, err := promClient.GetHPAHistoricalMetrics(e.ctx, namespace, hpaName, duration, step)
		if err != nil {
			return nil, fmt.Errorf("erro ao buscar histórico: %w", err)
		}

		return historical, nil
	})

	if err != nil {
		return nil, err
	}

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Dur("duration", duration).
		Msg("Métricas históricas coletadas")

	// Type assertion correta para map[string]*client.QueryRangeResult
	historicalData, ok := result.(map[string]*client.QueryRangeResult)
	if !ok {
		return nil, fmt.Errorf("tipo inesperado no cache: %T", result)
	}

	// Converter para map[string]interface{} para retornar
	converted := make(map[string]interface{})
	for key, value := range historicalData {
		converted[key] = value
	}

	return converted, nil
}

// GetMultipleHPAMetrics busca métricas de múltiplos HPAs em paralelo
func (e *MonitoringEngineV2) GetMultipleHPAMetrics(targets []HPATarget) ([]*HPAMetrics, error) {
	var wg sync.WaitGroup
	results := make([]*HPAMetrics, len(targets))
	errors := make([]error, len(targets))

	for i, target := range targets {
		wg.Add(1)
		go func(idx int, t HPATarget) {
			defer wg.Done()

			snapshot, err := e.GetHPAMetrics(t.Cluster, t.Namespace, t.HPAName)
			if err != nil {
				errors[idx] = err
				return
			}

			results[idx] = snapshot
		}(i, target)
	}

	wg.Wait()

	// Verificar se houve erros
	hasErrors := false
	for _, err := range errors {
		if err != nil {
			hasErrors = true
			log.Error().Err(err).Msg("Erro ao coletar métricas")
		}
	}

	if hasErrors {
		return results, fmt.Errorf("alguns targets falharam ao coletar métricas")
	}

	return results, nil
}

// ClearCache limpa todo o cache
func (e *MonitoringEngineV2) ClearCache() {
	e.cache.Clear()
	log.Info().Msg("Cache de monitoramento limpo")
}

// ClearCacheForHPA limpa cache de um HPA específico
func (e *MonitoringEngineV2) ClearCacheForHPA(cluster, namespace, hpaName string) {
	cacheKey := fmt.Sprintf("hpa:%s:%s:%s", cluster, namespace, hpaName)
	e.cache.Delete(cacheKey)

	log.Debug().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("Cache do HPA limpo")
}

// GetCacheStats retorna estatísticas do cache
func (e *MonitoringEngineV2) GetCacheStats() map[string]interface{} {
	return e.cache.Stats()
}

// GetPrometheusURL retorna a URL do Prometheus para um cluster
func (e *MonitoringEngineV2) GetPrometheusURL(cluster string) string {
	return discovery.GetPrometheusURL(cluster)
}

// CheckEndpointAvailability verifica se endpoint Prometheus está disponível
func (e *MonitoringEngineV2) CheckEndpointAvailability(cluster string) bool {
	url := discovery.GetPrometheusURL(cluster)
	return discovery.IsEndpointAvailable(url)
}

// HPATarget representa um alvo de monitoramento
type HPATarget struct {
	Cluster   string
	Namespace string
	HPAName   string
}

// Helper function para parse de float
func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
