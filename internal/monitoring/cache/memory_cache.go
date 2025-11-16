package cache

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// CacheEntry entrada no cache com TTL
type CacheEntry struct {
	Data      interface{}
	ExpiresAt time.Time
}

// MemoryCache cache em memória com TTL
type MemoryCache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
}

// NewMemoryCache cria um novo cache em memória
func NewMemoryCache(ttl time.Duration) *MemoryCache {
	cache := &MemoryCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}

	// Inicia limpeza periódica de entries expirados
	go cache.cleanupLoop()

	log.Info().
		Dur("ttl", ttl).
		Msg("Cache em memória criado")

	return cache
}

// Set adiciona um item ao cache
func (c *MemoryCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		Data:      value,
		ExpiresAt: time.Now().Add(c.ttl),
	}

	log.Debug().
		Str("key", key).
		Time("expires_at", c.entries[key].ExpiresAt).
		Msg("Item adicionado ao cache")
}

// Get retorna um item do cache (nil se não existir ou expirado)
func (c *MemoryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Verificar se expirou
	if time.Now().After(entry.ExpiresAt) {
		log.Debug().
			Str("key", key).
			Msg("Item do cache expirado")
		return nil, false
	}

	log.Debug().
		Str("key", key).
		Msg("Cache hit")

	return entry.Data, true
}

// Delete remove um item do cache
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)

	log.Debug().
		Str("key", key).
		Msg("Item removido do cache")
}

// Clear limpa todo o cache
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := len(c.entries)
	c.entries = make(map[string]*CacheEntry)

	log.Info().
		Int("cleared_entries", count).
		Msg("Cache limpo")
}

// Size retorna o número de items no cache
func (c *MemoryCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// cleanupLoop executa limpeza periódica de entries expirados
func (c *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup remove entries expirados
func (c *MemoryCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		log.Debug().
			Int("expired_entries", expiredCount).
			Int("remaining_entries", len(c.entries)).
			Msg("Cache cleanup executado")
	}
}

// GetOrSet retorna item do cache ou executa função para obter valor
func (c *MemoryCache) GetOrSet(key string, fn func() (interface{}, error)) (interface{}, error) {
	// Tentar obter do cache primeiro
	if value, exists := c.Get(key); exists {
		return value, nil
	}

	// Cache miss - executar função para obter valor
	value, err := fn()
	if err != nil {
		return nil, err
	}

	// Adicionar ao cache
	c.Set(key, value)

	return value, nil
}

// Stats retorna estatísticas do cache
func (c *MemoryCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	expiredCount := 0
	now := time.Now()

	for _, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			expiredCount++
		}
	}

	return map[string]interface{}{
		"total_entries":   len(c.entries),
		"expired_entries": expiredCount,
		"active_entries":  len(c.entries) - expiredCount,
		"ttl_seconds":     c.ttl.Seconds(),
	}
}
