package aierrors

import (
	"sync"
	"time"
)

// ErrorRecord representa um erro de IA com timestamp
type ErrorRecord struct {
	Error     string
	Timestamp time.Time
	Provider  string
	Model     string
}

// CallStats representa estatísticas de chamadas à AI por usuário
type CallStats struct {
	TotalCalls     int64
	SuccessfulCalls int64
	FailedCalls    int64
	LastCallTime   time.Time
}

var (
	globalAIErrors   = make(map[string]*ErrorRecord)
	globalAIErrorsMu sync.RWMutex
	
	// Contador global de chamadas à AI por usuário
	globalAICallStats   = make(map[string]*CallStats)
	globalAICallStatsMu sync.RWMutex
)

// RecordGlobalAIError registra um erro de IA globalmente
func RecordGlobalAIError(userEmail, provider, model string, err error) {
	globalAIErrorsMu.Lock()
	defer globalAIErrorsMu.Unlock()

	if userEmail == "" {
		userEmail = "default"
	}

	if err == nil {
		// Limpar erro para este usuário
		delete(globalAIErrors, userEmail)
		return
	}

	globalAIErrors[userEmail] = &ErrorRecord{
		Error:     err.Error(),
		Timestamp: time.Now(),
		Provider:  provider,
		Model:     model,
	}
}

// GetGlobalAIError retorna o erro de IA para um usuário (se houver)
// Retorna nil se não houver erro ou se o erro for muito antigo (>10 minutos)
func GetGlobalAIError(userEmail string) *ErrorRecord {
	globalAIErrorsMu.RLock()
	defer globalAIErrorsMu.RUnlock()

	if userEmail == "" {
		userEmail = "default"
	}

	record, exists := globalAIErrors[userEmail]
	if !exists {
		return nil
	}

	// Ignorar erros muito antigos
	if time.Since(record.Timestamp) > 10*time.Minute {
		return nil
	}

	return record
}

// IncrementAICall registra uma chamada à AI
func IncrementAICall(userEmail string, success bool) {
	globalAICallStatsMu.Lock()
	defer globalAICallStatsMu.Unlock()

	if userEmail == "" {
		userEmail = "default"
	}

	stats, exists := globalAICallStats[userEmail]
	if !exists {
		stats = &CallStats{}
		globalAICallStats[userEmail] = stats
	}

	stats.TotalCalls++
	stats.LastCallTime = time.Now()
	
	if success {
		stats.SuccessfulCalls++
	} else {
		stats.FailedCalls++
	}
}

// GetAICallStats retorna as estatísticas de chamadas à AI para um usuário
func GetAICallStats(userEmail string) *CallStats {
	globalAICallStatsMu.RLock()
	defer globalAICallStatsMu.RUnlock()

	if userEmail == "" {
		userEmail = "default"
	}

	stats, exists := globalAICallStats[userEmail]
	if !exists {
		return &CallStats{}
	}

	// Retornar cópia para evitar race conditions
	return &CallStats{
		TotalCalls:     stats.TotalCalls,
		SuccessfulCalls: stats.SuccessfulCalls,
		FailedCalls:    stats.FailedCalls,
		LastCallTime:   stats.LastCallTime,
	}
}

// ResetAICallStats reseta as estatísticas de um usuário
func ResetAICallStats(userEmail string) {
	globalAICallStatsMu.Lock()
	defer globalAICallStatsMu.Unlock()

	if userEmail == "" {
		userEmail = "default"
	}

	delete(globalAICallStats, userEmail)
}
