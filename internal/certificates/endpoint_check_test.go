package certificates

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCheckEndpointTLS_HandshakeReal(t *testing.T) {
	// httptest.NewTLSServer sobe um servidor TLS real e local (cert autoassinado gerado pelo
	// próprio stdlib) — handshake de verdade, sem mock de rede.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	if err != nil {
		t.Fatalf("erro ao extrair host/porta de %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("porta inválida %q: %v", portStr, err)
	}

	result := CheckEndpointTLS(context.Background(), host, port, "")

	if !result.Success {
		t.Fatalf("esperava Success=true, obtive erro: %s", result.ErrorMessage)
	}
	if result.ChainLength == 0 {
		t.Error("esperava ChainLength > 0")
	}
	if result.SerialNumber == "" {
		t.Error("esperava SerialNumber preenchido")
	}
	if result.NotAfter.IsZero() {
		t.Error("esperava NotAfter preenchido")
	}
	// Certificado de teste do httptest expira no futuro distante — deve classificar como "valid".
	if result.Status != "valid" {
		t.Errorf("Status = %q, esperado %q (DaysRemaining=%d)", result.Status, "valid", result.DaysRemaining)
	}
	// Certificado autoassinado do httptest não é de nenhuma CA pública.
	if result.TrustedByPublicCA {
		t.Error("esperava TrustedByPublicCA=false para certificado autoassinado de teste")
	}
}

func TestCheckEndpointTLS_SNIExplicito(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	port, _ := strconv.Atoi(portStr)

	// SNI explícito igual ao host — deve se comportar como o teste acima (mesmo handshake).
	result := CheckEndpointTLS(context.Background(), host, port, host)
	if !result.Success {
		t.Fatalf("esperava Success=true com SNI explícito, obtive erro: %s", result.ErrorMessage)
	}
}

func TestCheckEndpointTLS_ConexaoRecusada(t *testing.T) {
	// Reserva uma porta livre e fecha o listener imediatamente — a porta continua fechada
	// (ninguém mais deve abri-la durante o teste), então a conexão seguinte falha com
	// "connection refused" real, sem precisar mockar nada.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("erro ao reservar porta livre: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	l.Close() //nolint:errcheck

	result := CheckEndpointTLS(context.Background(), "127.0.0.1", addr.Port, "")

	if result.Success {
		t.Fatal("esperava Success=false para porta fechada")
	}
	if result.ErrorMessage == "" {
		t.Error("esperava ErrorMessage preenchida")
	}
}

func TestCheckEndpointTLS_TimeoutRespeitaContexto(t *testing.T) {
	// Contexto já cancelado — o dial deve falhar rápido em vez de esperar o timeout inteiro.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	result := CheckEndpointTLS(ctx, "127.0.0.1", 1, "") // porta 1: privilegiada, tipicamente fechada
	elapsed := time.Since(start)

	if result.Success {
		t.Fatal("esperava Success=false com contexto já cancelado")
	}
	if elapsed > 1*time.Second {
		t.Errorf("esperava falha quase imediata com contexto cancelado, levou %s", elapsed)
	}
}

func TestClassifyExpiry(t *testing.T) {
	tests := []struct {
		name       string
		notAfter   time.Time
		wantStatus string
	}{
		{"expirado", time.Now().Add(-24 * time.Hour), "expired"},
		{"expirando em 5 dias", time.Now().Add(5 * 24 * time.Hour), "expiring"},
		{"no limiar exato de 30 dias", time.Now().Add(ExpiringThresholdDays * 24 * time.Hour), "expiring"},
		{"valido por 90 dias", time.Now().Add(90 * 24 * time.Hour), "valid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, days := classifyExpiry(tt.notAfter)
			if status != tt.wantStatus {
				t.Errorf("status = %q, esperado %q (days=%d)", status, tt.wantStatus, days)
			}
		})
	}
}
