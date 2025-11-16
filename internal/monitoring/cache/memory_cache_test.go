package cache

import (
	"errors"
	"testing"
	"time"
)

func TestNewMemoryCache(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewMemoryCache(ttl)

	if cache == nil {
		t.Fatal("Expected non-nil cache")
	}

	if cache.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cache.ttl)
	}

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", cache.Size())
	}
}

func TestSetAndGet(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	key := "test-key"
	value := "test-value"

	cache.Set(key, value)

	got, exists := cache.Get(key)
	if !exists {
		t.Error("Expected key to exist")
	}

	if got != value {
		t.Errorf("Expected value %v, got %v", value, got)
	}
}

func TestGet_NotExists(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	_, exists := cache.Get("non-existent-key")
	if exists {
		t.Error("Expected key to not exist")
	}
}

func TestExpiration(t *testing.T) {
	ttl := 100 * time.Millisecond
	cache := NewMemoryCache(ttl)

	key := "expiring-key"
	value := "expiring-value"

	cache.Set(key, value)

	// Verificar que existe imediatamente
	got, exists := cache.Get(key)
	if !exists {
		t.Error("Expected key to exist immediately")
	}

	if got != value {
		t.Errorf("Expected value %v, got %v", value, got)
	}

	// Aguardar expiração
	time.Sleep(150 * time.Millisecond)

	// Verificar que expirou
	_, exists = cache.Get(key)
	if exists {
		t.Error("Expected key to be expired")
	}
}

func TestDelete(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	key := "delete-key"
	value := "delete-value"

	cache.Set(key, value)

	// Verificar que existe
	_, exists := cache.Get(key)
	if !exists {
		t.Error("Expected key to exist")
	}

	// Deletar
	cache.Delete(key)

	// Verificar que foi removido
	_, exists = cache.Get(key)
	if exists {
		t.Error("Expected key to be deleted")
	}
}

func TestClear(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	// Adicionar múltiplos items
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Limpar cache
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", cache.Size())
	}

	// Verificar que items não existem mais
	_, exists := cache.Get("key1")
	if exists {
		t.Error("Expected cache to be empty")
	}
}

func TestGetOrSet_CacheHit(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	key := "test-key"
	originalValue := "original-value"

	// Adicionar ao cache
	cache.Set(key, originalValue)

	// GetOrSet deve retornar valor do cache sem executar função
	called := false
	fn := func() (interface{}, error) {
		called = true
		return "new-value", nil
	}

	got, err := cache.GetOrSet(key, fn)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if called {
		t.Error("Expected function not to be called (cache hit)")
	}

	if got != originalValue {
		t.Errorf("Expected original value %v, got %v", originalValue, got)
	}
}

func TestGetOrSet_CacheMiss(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	key := "missing-key"
	newValue := "new-value"

	// GetOrSet deve executar função e adicionar ao cache
	called := false
	fn := func() (interface{}, error) {
		called = true
		return newValue, nil
	}

	got, err := cache.GetOrSet(key, fn)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !called {
		t.Error("Expected function to be called (cache miss)")
	}

	if got != newValue {
		t.Errorf("Expected new value %v, got %v", newValue, got)
	}

	// Verificar que foi adicionado ao cache
	cached, exists := cache.Get(key)
	if !exists {
		t.Error("Expected key to be added to cache")
	}

	if cached != newValue {
		t.Errorf("Expected cached value %v, got %v", newValue, cached)
	}
}

func TestGetOrSet_Error(t *testing.T) {
	cache := NewMemoryCache(1 * time.Hour)

	key := "error-key"
	expectedError := errors.New("test error")

	fn := func() (interface{}, error) {
		return nil, expectedError
	}

	_, err := cache.GetOrSet(key, fn)
	if err != expectedError {
		t.Errorf("Expected error %v, got %v", expectedError, err)
	}

	// Verificar que não foi adicionado ao cache
	_, exists := cache.Get(key)
	if exists {
		t.Error("Expected key not to be added on error")
	}
}

func TestStats(t *testing.T) {
	ttl := 100 * time.Millisecond
	cache := NewMemoryCache(ttl)

	// Adicionar items
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	stats := cache.Stats()

	if stats["total_entries"] != 2 {
		t.Errorf("Expected 2 total entries, got %v", stats["total_entries"])
	}

	// Aguardar expiração
	time.Sleep(150 * time.Millisecond)

	stats = cache.Stats()

	if stats["expired_entries"] != 2 {
		t.Errorf("Expected 2 expired entries, got %v", stats["expired_entries"])
	}

	if stats["active_entries"] != 0 {
		t.Errorf("Expected 0 active entries, got %v", stats["active_entries"])
	}
}

func TestCleanup(t *testing.T) {
	ttl := 50 * time.Millisecond
	cache := NewMemoryCache(ttl)

	// Adicionar items
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	// Aguardar expiração + cleanup (cleanup executa a cada 1 minuto, mas podemos forçar)
	time.Sleep(100 * time.Millisecond)

	// Forçar cleanup manual
	cache.cleanup()

	// Verificar que items expirados foram removidos
	if cache.Size() != 0 {
		t.Errorf("Expected empty cache after cleanup, got size %d", cache.Size())
	}
}
