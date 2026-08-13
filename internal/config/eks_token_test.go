package config

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeRoundTripFn permite construir um http.RoundTripper de teste a partir de uma função.
type fakeRoundTripFn func(*http.Request) (*http.Response, error)

func (f fakeRoundTripFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// resetEKSTokenCache limpa o cache global de tokens EKS antes/depois de cada teste, evitando
// que testes interfiram entre si (eksTokenCache é estado de pacote compartilhado).
func resetEKSTokenCache(t *testing.T) {
	t.Helper()
	eksTokenCacheMu.Lock()
	eksTokenCache = make(map[string]*eksTokenCacheEntry)
	eksTokenCacheMu.Unlock()
	t.Cleanup(func() {
		eksTokenCacheMu.Lock()
		eksTokenCache = make(map[string]*eksTokenCacheEntry)
		eksTokenCacheMu.Unlock()
	})
}

func TestEvictEKSTokenCacheByProfile(t *testing.T) {
	resetEKSTokenCache(t)

	asaplogArgs := []string{"eks", "get-token", "--cluster-name", "foo", "--region", "us-east-1", "--output", "json", "--profile", "asaplog"}
	otherArgs := []string{"eks", "get-token", "--cluster-name", "bar", "--region", "us-east-1", "--output", "json", "--profile", "other"}
	asaplogKey := strings.Join(asaplogArgs, "|")
	otherKey := strings.Join(otherArgs, "|")

	eksTokenCacheMu.Lock()
	eksTokenCache[asaplogKey] = &eksTokenCacheEntry{token: "t1", exp: time.Now().Add(time.Minute)}
	eksTokenCache[otherKey] = &eksTokenCacheEntry{token: "t2", exp: time.Now().Add(time.Minute)}
	eksTokenCacheMu.Unlock()

	evictEKSTokenCacheByProfile("asaplog")

	eksTokenCacheMu.Lock()
	defer eksTokenCacheMu.Unlock()
	if _, ok := eksTokenCache[asaplogKey]; ok {
		t.Fatal("esperava que a entrada do perfil 'asaplog' fosse evictada")
	}
	if _, ok := eksTokenCache[otherKey]; !ok {
		t.Fatal("entrada de outro perfil não deveria ser afetada")
	}
}

func TestEvictEKSTokenCacheByProfile_EmptyProfileIsNoop(t *testing.T) {
	resetEKSTokenCache(t)

	key := strings.Join([]string{"eks", "get-token", "--cluster-name", "foo", "--region", "us-east-1", "--output", "json"}, "|")
	eksTokenCacheMu.Lock()
	eksTokenCache[key] = &eksTokenCacheEntry{token: "t1", exp: time.Now().Add(time.Minute)}
	eksTokenCacheMu.Unlock()

	evictEKSTokenCacheByProfile("")

	eksTokenCacheMu.Lock()
	defer eksTokenCacheMu.Unlock()
	if _, ok := eksTokenCache[key]; !ok {
		t.Fatal("chamar com perfil vazio não deveria remover nenhuma entrada")
	}
}

func TestInvalidateEKSTokenCache(t *testing.T) {
	resetEKSTokenCache(t)

	args := []string{"eks", "get-token", "--cluster-name", "foo", "--region", "us-east-1", "--output", "json", "--profile", "asaplog"}
	key := strings.Join(args, "|")

	eksTokenCacheMu.Lock()
	eksTokenCache[key] = &eksTokenCacheEntry{token: "stale", exp: time.Now().Add(time.Minute)}
	eksTokenCacheMu.Unlock()

	invalidateEKSTokenCache(args)

	eksTokenCacheMu.Lock()
	defer eksTokenCacheMu.Unlock()
	if _, ok := eksTokenCache[key]; ok {
		t.Fatal("esperava que a entrada fosse invalidada")
	}
}

// TestEKSTokenRoundTripper_401InvalidatesCacheAndRetries confirma o self-healing: um token
// cacheado que a API rejeita com 401 é invalidado do cache e a chamada é retentada uma vez
// (mesmo padrão de gkeTokenRoundTripper) — sem isso, o mesmo token ruim seria reaproveitado em
// toda chamada subsequente até o TTL do cache expirar sozinho (~14min), mesmo com a sessão AWS
// já corrigida por um novo login.
func TestEKSTokenRoundTripper_401InvalidatesCacheAndRetries(t *testing.T) {
	resetEKSTokenCache(t)

	// Perfil deliberadamente inexistente: se a máquina de teste tiver o CLI `aws` instalado,
	// isso garante que o retry (que dispara um `aws eks get-token` de verdade — não há injeção
	// de dependência de exec neste pacote) falhe rápido e de forma determinística, em vez de
	// depender de alguma sessão SSO real porventura já autenticada na máquina que roda o teste.
	args := []string{"eks", "get-token", "--cluster-name", "foo", "--region", "us-east-1", "--output", "json", "--profile", "claude-code-test-nonexistent-profile"}
	key := strings.Join(args, "|")

	eksTokenCacheMu.Lock()
	eksTokenCache[key] = &eksTokenCacheEntry{token: "stale-token", exp: time.Now().Add(time.Minute)}
	eksTokenCacheMu.Unlock()

	var calls int
	var firstAuthHeader string
	base := fakeRoundTripFn(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			firstAuthHeader = req.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody, Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	rt := &eksTokenRoundTripper{args: args, base: base}
	// http.NewRequest (client-side), não httptest.NewRequest (server-side) — este último sempre
	// preenche req.Body com um leitor vazio não-nil (comportamento documentado do pacote server),
	// o que cairia sempre no ramo "corpo não rebobinável" e nunca exercitaria o retry.
	req, err := http.NewRequest(http.MethodGet, "https://example.com/api", nil)
	if err != nil {
		t.Fatalf("falha ao construir request de teste: %v", err)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if firstAuthHeader != "Bearer stale-token" {
		t.Fatalf("esperava que a primeira chamada usasse o token cacheado, veio %q", firstAuthHeader)
	}
	if calls != 2 {
		t.Fatalf("esperava exatamente 2 chamadas ao transporte base (original + retry), veio %d", calls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("esperava que o retry devolvesse 200, veio %d", resp.StatusCode)
	}

	// A entrada original ("stale-token") nunca deve sobreviver ao 401 — o retry só a
	// repovoaria com um token novo em caso de sucesso (perfil inexistente aqui garante que
	// isso não aconteça), nunca reaproveitar o mesmo token ruim.
	eksTokenCacheMu.Lock()
	entry, stillCached := eksTokenCache[key]
	eksTokenCacheMu.Unlock()
	if stillCached && entry.token == "stale-token" {
		t.Fatal("esperava que o token cacheado ('stale-token') tivesse sido invalidado após o 401")
	}
}

// TestEKSTokenRoundTripper_NonRetryableBodyDoesNotRetry confirma que um corpo não rebobinável
// (sem GetBody) não é reenviado — arriscaria mandar um corpo vazio/corrompido numa escrita —,
// mas o cache ainda é invalidado para que a PRÓXIMA chamada do caller já use um token fresco.
func TestEKSTokenRoundTripper_NonRetryableBodyDoesNotRetry(t *testing.T) {
	resetEKSTokenCache(t)

	args := []string{"eks", "get-token", "--cluster-name", "foo", "--region", "us-east-1", "--output", "json", "--profile", "asaplog"}
	key := strings.Join(args, "|")

	eksTokenCacheMu.Lock()
	eksTokenCache[key] = &eksTokenCacheEntry{token: "stale-token", exp: time.Now().Add(time.Minute)}
	eksTokenCacheMu.Unlock()

	var calls int
	base := fakeRoundTripFn(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	rt := &eksTokenRoundTripper{args: args, base: base}
	req := httptest.NewRequest(http.MethodPost, "https://example.com/api", strings.NewReader("payload"))
	req.GetBody = nil // corpo não rebobinável

	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if calls != 1 {
		t.Fatalf("esperava só 1 chamada (sem retry para corpo não rebobinável), veio %d", calls)
	}

	eksTokenCacheMu.Lock()
	_, stillCached := eksTokenCache[key]
	eksTokenCacheMu.Unlock()
	if stillCached {
		t.Fatal("esperava que o cache fosse invalidado mesmo sem retentar")
	}
}
