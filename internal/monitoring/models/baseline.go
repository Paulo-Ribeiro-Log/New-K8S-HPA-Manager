package models

import "time"

// BaselineSnapshot captura o estado do sistema ANTES do stress test
type BaselineSnapshot struct {
	Timestamp time.Time
	Duration  time.Duration // Duração do período analisado (ex: 30min)

	// Métricas globais
	TotalClusters int
	TotalHPAs     int
	TotalReplicas int

	// Estatísticas de CPU
	CPUAvg float64
	CPUMax float64
	CPUMin float64
	CPUP95 float64

	// Estatísticas de Memória
	MemoryAvg float64
	MemoryMax float64
	MemoryMin float64
	MemoryP95 float64

	// Estatísticas de Réplicas
	ReplicasAvg float64
	ReplicasMax int32
	ReplicasMin int32

	// Estatísticas de Tráfego
	RequestRateAvg float64
	RequestRateMax float64
	ErrorRateAvg   float64
	ErrorRateMax   float64
	LatencyP95Avg  float64
	LatencyP95Max  float64

	// Baselines por HPA
	HPABaselines map[string]*HPABaseline // key: cluster/namespace/name
}

// HPABaseline baseline de um HPA específico
type HPABaseline struct {
	Cluster   string
	Namespace string
	Name      string

	// Configuração do HPA
	MinReplicas int32
	MaxReplicas int32
	TargetCPU   int32

	// Estado atual (antes do teste)
	CurrentReplicas int32

	// Estatísticas do período de observação
	CPUAvg    float64
	CPUMax    float64
	CPUMin    float64
	MemoryAvg float64
	MemoryMax float64
	MemoryMin float64

	// Histórico de réplicas
	ReplicasAvg    float64
	ReplicasMax    int32
	ReplicasMin    int32
	ReplicasStdDev float64 // Desvio padrão para detectar oscilação

	// Métricas de aplicação (se disponíveis)
	RequestRateAvg float64
	ErrorRateAvg   float64
	LatencyP95Avg  float64

	// Metadata
	Timestamp time.Time
	Healthy   bool   // Se HPA estava saudável no baseline
	Notes     string // Observações
}
