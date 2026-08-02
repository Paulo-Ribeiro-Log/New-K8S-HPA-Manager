package discovery

import (
	"errors"
	"testing"
	"time"
)

// TestCheckKnownUnreachable_CachesRecentFailure cobre o bug real corrigido: sem esse cache,
// cada chamador (alerts.Client, DiscoverEndpoint) pagava o timeout completo de rede de novo a
// cada tentativa, mesmo sabendo segundos antes que aquela URL estava fora do ar.
func TestCheckKnownUnreachable_CachesRecentFailure(t *testing.T) {
	url := "https://prometheus-teste-cache-1.viavarejo.com.br/"
	t.Cleanup(func() { unreachableCache.Delete(url) })

	if err := CheckKnownUnreachable(url); err != nil {
		t.Fatalf("URL nunca vista deveria retornar nil, got %v", err)
	}

	wantErr := errors.New("erro de conexão simulado")
	MarkPrometheusUnreachable(url, wantErr)

	got := CheckKnownUnreachable(url)
	if got == nil || got.Error() != wantErr.Error() {
		t.Fatalf("esperava o erro cacheado %v, got %v", wantErr, got)
	}
}

// TestMarkPrometheusReachable_ClearsFailure cobre a recuperação: uma tentativa bem-sucedida deve
// limpar o cache negativo imediatamente, sem esperar o TTL expirar sozinho.
func TestMarkPrometheusReachable_ClearsFailure(t *testing.T) {
	url := "https://prometheus-teste-cache-2.viavarejo.com.br/"
	t.Cleanup(func() { unreachableCache.Delete(url) })

	MarkPrometheusUnreachable(url, errors.New("falhou"))
	if err := CheckKnownUnreachable(url); err == nil {
		t.Fatalf("esperava erro cacheado antes da recuperação")
	}

	MarkPrometheusReachable(url)

	if err := CheckKnownUnreachable(url); err != nil {
		t.Fatalf("esperava cache limpo após MarkPrometheusReachable, got %v", err)
	}
}

// TestCheckKnownUnreachable_ExpiresAfterTTL garante que a falha cacheada não fica presa pra
// sempre — depois do TTL, a próxima checagem deve permitir uma nova tentativa real.
func TestCheckKnownUnreachable_ExpiresAfterTTL(t *testing.T) {
	url := "https://prometheus-teste-cache-3.viavarejo.com.br/"
	t.Cleanup(func() {
		unreachableCache.Delete(url)
		unreachableCacheTTL = 60 * time.Second
	})

	unreachableCacheTTL = 20 * time.Millisecond
	MarkPrometheusUnreachable(url, errors.New("falhou"))

	if err := CheckKnownUnreachable(url); err == nil {
		t.Fatalf("esperava erro cacheado logo após a falha")
	}

	time.Sleep(30 * time.Millisecond)

	if err := CheckKnownUnreachable(url); err != nil {
		t.Fatalf("esperava cache expirado após o TTL, got %v", err)
	}
}
