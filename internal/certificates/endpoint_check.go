package certificates

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strconv"
	"time"
)

// endpointDialTimeout limita quanto tempo CheckEndpointTLS espera pelo handshake — mais folga
// que tlsDialTimeoutPerHost (tls_dial_enrich.go, 3s) porque aqui o alvo pode ser um servidor
// on-prem atrás de um link mais lento que a rede interna de um cluster K8s.
const endpointDialTimeout = 5 * time.Second

// EndpointCheckResult é o resultado de uma checagem TLS contra um endpoint externo cadastrado
// livremente pelo usuário (host:porta, sem nenhum Secret/Ingress/Gateway K8s associado) — mesma
// ideia do módulo https_2xx do blackbox_exporter do Prometheus, sem depender dele.
type EndpointCheckResult struct {
	Success      bool
	ErrorMessage string

	Subject      string
	Issuer       string
	SerialNumber string
	NotBefore    time.Time
	NotAfter     time.Time
	DNSNames     []string
	ChainLength  int

	Status        string // "valid" | "expiring" | "expired" — vazio quando Success=false
	DaysRemaining int

	TrustedByPublicCA bool
}

// CheckEndpointTLS conecta em host:port via TLS (ServerName = sni, ou host quando sni é vazio) e
// reporta o certificado real servido — status/validade calculados a partir dele, não de um
// Secret esperado (diferente de EnrichWithTLSDial, que compara com um serial conhecido — aqui
// não existe um Secret K8s de referência, é só o estado do certificado como ele é). Pensado pra
// endpoints fora de qualquer cluster K8s: servidor on-prem Windows/Linux, serviço externo.
func CheckEndpointTLS(ctx context.Context, host string, port int, sni string) EndpointCheckResult {
	serverName := sni
	if serverName == "" {
		serverName = host
	}

	dialCtx, cancel := context.WithTimeout(ctx, endpointDialTimeout)
	defer cancel()

	address := net.JoinHostPort(host, strconv.Itoa(port))
	tlsDialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		Config: &tls.Config{
			ServerName: serverName,
			// InsecureSkipVerify intencional: o objetivo é ler o certificado real servido mesmo
			// que autoassinado (comum em servidor on-prem interno) — a confiança numa CA pública
			// é reportada à parte via TrustedByPublicCA, sem impedir a leitura do certificado.
			InsecureSkipVerify: true, //nolint:gosec
		},
	}

	conn, err := tlsDialer.DialContext(dialCtx, "tcp", address)
	if err != nil {
		return EndpointCheckResult{Success: false, ErrorMessage: err.Error()}
	}
	defer conn.Close() //nolint:errcheck

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return EndpointCheckResult{Success: false, ErrorMessage: "conexão retornada não é TLS"}
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return EndpointCheckResult{Success: false, ErrorMessage: "handshake concluído sem certificado peer"}
	}

	leaf := certs[0]
	status, daysRemaining := classifyExpiry(leaf.NotAfter)

	return EndpointCheckResult{
		Success:           true,
		Subject:           certSubjectDisplayName(leaf),
		Issuer:            leaf.Issuer.CommonName,
		SerialNumber:      formatSerialNumber(leaf.SerialNumber),
		NotBefore:         leaf.NotBefore,
		NotAfter:          leaf.NotAfter,
		DNSNames:          leaf.DNSNames,
		ChainLength:       len(certs),
		Status:            status,
		DaysRemaining:     daysRemaining,
		TrustedByPublicCA: isTrustedByPublicCA(certs, serverName),
	}
}

// isTrustedByPublicCA tenta validar a cadeia contra o pool de CAs do sistema — best-effort e
// nunca fatal: qualquer falha (CA privada/interna, cadeia incompleta, hostname sem SAN
// correspondente) só resulta em false, mesmo espírito de TrustedByPublicCA=false tratado como
// neutro em ChainValidationResult (validate.go) — não é um erro, é o esperado pra CA interna,
// muito comum em servidor on-prem.
func isTrustedByPublicCA(certs []*x509.Certificate, serverName string) bool {
	if len(certs) == 0 {
		return false
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		return false
	}
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	_, err = certs[0].Verify(x509.VerifyOptions{
		Roots:         pool,
		Intermediates: intermediates,
		DNSName:       serverName,
	})
	return err == nil
}
