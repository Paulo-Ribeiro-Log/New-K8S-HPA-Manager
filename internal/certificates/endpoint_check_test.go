package certificates

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
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

// makeTestCert gera um certificado autoassinado de verdade (x509.CreateCertificate, sem TLS/rede)
// com o CommonName e SANs informados — usado só para testar certSubjectDisplayName contra as
// combinações reais possíveis (CN presente, CN vazio com SAN, nenhum dos dois).
func makeTestCert(t *testing.T, cn string, dnsNames []string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dnsNames,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsear certificado: %v", err)
	}
	return cert
}

func TestCertSubjectDisplayName(t *testing.T) {
	t.Run("usa CommonName quando presente", func(t *testing.T) {
		cert := makeTestCert(t, "meu-servidor.local", nil)
		if got := certSubjectDisplayName(cert); got != "meu-servidor.local" {
			t.Errorf("got %q, esperado %q", got, "meu-servidor.local")
		}
	})

	t.Run("cai pro primeiro SAN quando CommonName vazio (baseline requirements modernos)", func(t *testing.T) {
		// Desde ~2021 CAs públicas frequentemente não preenchem mais CommonName, só SAN — este é
		// o caso real que motivou o bug reportado (nome do certificado não aparecia).
		cert := makeTestCert(t, "", []string{"svc.example.com", "svc-alt.example.com"})
		if got := certSubjectDisplayName(cert); got != "svc.example.com" {
			t.Errorf("got %q, esperado %q", got, "svc.example.com")
		}
	})

	t.Run("nunca retorna vazio mesmo sem CommonName nem SAN", func(t *testing.T) {
		cert := makeTestCert(t, "", nil)
		if got := certSubjectDisplayName(cert); got == "" {
			t.Error("certSubjectDisplayName não deveria retornar string vazia")
		}
	})
}
