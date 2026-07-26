package handlers

import "testing"

func TestIsAzureRedisHost(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		wantCache string
		wantOK    bool
	}{
		{"classico windows.net", "mycache.redis.cache.windows.net", "mycache", true},
		{"enterprise/managed redis com regiao", "mycache.brazilsouth.redis.azure.net", "mycache", true},
		{"case insensitive", "MyCache.BrazilSouth.Redis.Azure.Net", "MyCache", true},
		{"self-hosted comum", "10.0.0.5", "", false},
		{"outro dominio redis-like", "mycache.redis.example.com", "", false},
		{"vazio", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache, ok := isAzureRedisHost(tc.host)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && cache != tc.wantCache {
				t.Errorf("cache = %q, want %q", cache, tc.wantCache)
			}
		})
	}
}

func TestFormatAzureRedisTierLabel(t *testing.T) {
	t.Run("classico com familia e capacidade", func(t *testing.T) {
		got := formatAzureRedisTierLabel("Microsoft.Cache/Redis", "Premium", "P", 1)
		if got != "Premium P1" {
			t.Errorf("got %q, want %q", got, "Premium P1")
		}
	})

	t.Run("enterprise com capacidade vira contagem de nos", func(t *testing.T) {
		got := formatAzureRedisTierLabel("Microsoft.Cache/redisEnterprise", "Enterprise_E10", "", 2)
		if got != "Enterprise_E10 (2 nó(s))" {
			t.Errorf("got %q, want %q", got, "Enterprise_E10 (2 nó(s))")
		}
	})

	t.Run("enterprise sem capacidade so mostra o nome", func(t *testing.T) {
		got := formatAzureRedisTierLabel("Microsoft.Cache/redisEnterprise", "Enterprise_E10", "", 0)
		if got != "Enterprise_E10" {
			t.Errorf("got %q, want %q", got, "Enterprise_E10")
		}
	})
}
