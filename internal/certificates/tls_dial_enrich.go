package certificates

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"
)

// tlsDialTimeoutPerHost limita quanto tempo EnrichWithTLSDial espera por cada host individual —
// nunca deve travar a resposta de validate-chain mesmo com vários hosts resolvidos.
const tlsDialTimeoutPerHost = 3 * time.Second

// tlsDialResult é o resultado bruto de 1 handshake — separado de EnrichWithTLSDial para permitir
// testar a agregação (buildTLSDialResult) sem abrir conexão de rede real, mesmo espírito de
// buildLivePropagationResult vs EnrichWithPrometheus.
type tlsDialResult struct {
	Host      string
	Err       error
	SerialDec string
	IssuerCN  string
	NotAfter  time.Time
}

// dialHostForCertFn é var (não const) só para permitir substituição em teste sem tocar rede real.
var dialHostForCertFn = dialHostForCert

// dialHostForCert conecta em host:443 via TLS (ServerName=host, SNI) e lê o certificado
// efetivamente servido na conexão. InsecureSkipVerify=true de propósito: o objetivo aqui é ver o
// certificado real servido, não validar confiança — isso já é coberto por
// ValidateCertificateChain/TrustedByPublicCA.
func dialHostForCert(ctx context.Context, host string) tlsDialResult {
	dialer := &net.Dialer{}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, "443"), &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // intencional: lemos o cert real servido, não validamos confiança aqui
	})
	if err != nil {
		return tlsDialResult{Host: host, Err: err}
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return tlsDialResult{Host: host, Err: fmt.Errorf("handshake concluído sem certificado peer")}
	}

	leaf := certs[0]
	return tlsDialResult{
		Host:      host,
		SerialDec: leaf.SerialNumber.String(),
		IssuerCN:  leaf.Issuer.CommonName,
		NotAfter:  leaf.NotAfter,
	}
}

// EnrichWithTLSDial conecta em cada host de `hosts` na porta 443 em paralelo (timeout por host,
// nunca por lote inteiro) e compara o serial do certificado realmente servido com
// leafSerialDecimal — funciona independente de quem termina o TLS (nginx, Istio, Traefik, GCP LB
// do GKE Gateway, ALB da AWS), ao contrário de EnrichWithPrometheus que só enxerga ingress-nginx.
func EnrichWithTLSDial(ctx context.Context, hosts []string, leafSerialDecimal string) *LivePropagationResult {
	results := make([]tlsDialResult, len(hosts))

	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			hostCtx, cancel := context.WithTimeout(ctx, tlsDialTimeoutPerHost)
			defer cancel()
			results[i] = dialHostForCertFn(hostCtx, host)
		}(i, host)
	}
	wg.Wait()

	return buildTLSDialResult(results, leafSerialDecimal)
}

// buildTLSDialResult é a lógica pura de agregação — separada de EnrichWithTLSDial para ser
// testável sem rede real.
func buildTLSDialResult(results []tlsDialResult, leafSerialDecimal string) *LivePropagationResult {
	result := &LivePropagationResult{Method: "tls-dial"}

	var reached int
	var dialErrors []string
	for _, r := range results {
		if r.Err != nil {
			dialErrors = append(dialErrors, fmt.Sprintf("%s: %s", r.Host, r.Err.Error()))
			continue
		}
		reached++
		if r.SerialDec == leafSerialDecimal {
			result.ReplicasCurrent++
		} else {
			result.ReplicasStale = append(result.ReplicasStale, r.Host)
		}
		if result.LiveIssuerCN == "" {
			result.LiveIssuerCN = r.IssuerCN
		}
		if result.LiveExpiresAt == nil || r.NotAfter.After(*result.LiveExpiresAt) {
			notAfter := r.NotAfter
			result.LiveExpiresAt = &notAfter
		}
	}

	if reached == 0 {
		result.Checked = false
		result.Notes = append(result.Notes, "não foi possível conectar (handshake TLS) em nenhum host resolvido")
		result.Notes = append(result.Notes, dialErrors...)
		return result
	}

	result.Checked = true
	result.TotalReplicasFound = reached

	if len(result.ReplicasStale) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d de %d host(s) ainda servem um certificado diferente do atual (handshake TLS direto) — propagação pode estar em andamento",
			len(result.ReplicasStale), reached,
		))
	}

	return result
}
