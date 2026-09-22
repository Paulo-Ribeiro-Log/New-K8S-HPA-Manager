package certificates

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// endpointDialTimeout limita quanto tempo CheckEndpointTLS espera pelo handshake — mais folga
// que tlsDialTimeoutPerHost (tls_dial_enrich.go, 3s) porque aqui o alvo pode ser um servidor
// on-prem atrás de um link mais lento que a rede interna de um cluster K8s.
const endpointDialTimeout = 5 * time.Second

// endpointPostHandshakeReadTimeout é quanto tempo CheckEndpointTLS espera, depois do handshake
// TLS "completar" do lado do cliente, por um alerta assíncrono do servidor — ver comentário
// detalhado no ponto de uso (por que isso é necessário em TLS 1.3, não um exagero de cautela).
const endpointPostHandshakeReadTimeout = 800 * time.Millisecond

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

	// IsDefaultFakeCert é true quando o certificado servido é o autoassinado padrão do
	// ingress-nginx (isIngressNginxDefaultFakeCert, parser.go) — sinal de que o host:porta
	// cadastrado não tem TLS de verdade configurado pra esse SNI, mesmo com Status="valid" (o
	// cert fake em si não está expirado). Ver comentário completo em isIngressNginxDefaultFakeCert.
	IsDefaultFakeCert bool
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
		return EndpointCheckResult{Success: false, ErrorMessage: classifyTLSDialError(err)}
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

	// Achado real: contra um servidor que exige certificado de cliente (mTLS) e não recebe
	// nenhum — exatamente o nosso caso, já que nunca apresentamos um —, o handshake do CLIENTE em
	// TLS 1.3 (padrão do Go desde 1.13) "completa" assim que ele manda seu próprio Finished, SEM
	// esperar nenhuma resposta do servidor depois disso (confirmado no código-fonte do Go,
	// handshake_client_tls13.go: a sequência é readServerCertificate → readServerFinished →
	// sendClientCertificate → sendClientFinished → isHandshakeComplete=true, nunca lê nada do
	// servidor depois do próprio Finished). O servidor, por sua vez, PROCESSA nosso Finished e,
	// só então, percebe que exigia certificado e não recebeu — manda o alerta
	// "certificate_required" como uma mensagem ASSÍNCRONA que só aparece numa leitura
	// subsequente da conexão, nunca durante o handshake em si. Sem esta leitura extra,
	// CheckEndpointTLS reportava "sucesso" mesmo contra servidores que na prática recusam a
	// conexão — confirmado com um servidor de teste real configurado com
	// ClientAuth: RequireAnyClientCert (endpoint_check_test.go). Timeout curto (800ms): no caso
	// normal (servidor não exige nada) o servidor não manda nada até recebermos uma request de
	// verdade, então isto sempre estoura o timeout — é o resultado ESPERADO, não um erro.
	tlsConn.SetReadDeadline(time.Now().Add(endpointPostHandshakeReadTimeout)) //nolint:errcheck
	if _, readErr := tlsConn.Read(make([]byte, 1)); readErr != nil && !isTimeoutErr(readErr) {
		return EndpointCheckResult{Success: false, ErrorMessage: classifyTLSDialError(readErr)}
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
		IsDefaultFakeCert: isIngressNginxDefaultFakeCert(leaf),
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

// classifyTLSDialError enriquece a mensagem de erro quando o handshake falha por um sinal de que
// o servidor exige certificado de CLIENTE (mTLS) — CheckEndpointTLS nunca apresenta nenhum
// certificado de cliente (só lê o certificado do SERVIDOR), então essa é uma causa real e comum
// de falha contra APIs que exigem mTLS. Achado real: usuário perguntou como saber se uma rota
// exige certificado de cliente pra acessar — o handshake que já fazemos entrega esse sinal
// sozinho, só precisava ser reconhecido em vez de reportado como erro genérico.
//
// Fraseologia deliberada, mesmo espírito de TrustedByPublicCA neutro em ChainValidationResult
// (documentado no CLAUDE.md): só "certificate required" vira afirmação definitiva — é o alerta
// TLS dedicado a exatamente esse cenário (RFC 8446 §6.2, alertCertificateRequired; confirmado no
// código-fonte do Go que o servidor emite esse alerta especificamente quando ClientAuth exige
// certificado e nenhum foi enviado — crypto/tls/handshake_server.go, requiresClientCert). "bad
// certificate" é um sinal mais fraco (histórico do TLS 1.2, também usado por outros motivos) —
// vira aviso qualificado, não afirmação. "i/o timeout" e "connection refused" também ganham
// explicação (ver abaixo) — qualquer outro erro (handshake failure genérico por incompatibilidade
// de versão/cifra, etc.) não é alterado, evita diagnosticar errado.
//
// Achado real corrigido — endpoint AWS EC2 relatado como "mesmo dentro da VPN não valida": o dial
// falhava com "i/o timeout" a partir do processo do servidor (mesma rede/VPN do usuário) — e
// também a partir de 6 tentativas seguidas num shell separado, sem nenhum sucesso, descartando
// flutuação momentânea. O usuário só conseguia conectar via `openssl s_client` rodando DE DENTRO
// da própria instância EC2 (terminal SSM) contra o próprio nome público dela — isso não prova
// alcançável de fora (AWS costuma permitir uma instância falar com seu próprio IP público mesmo
// com o Security Group bloqueando tudo externo). "i/o timeout" no dial (SYN sem resposta nenhuma)
// é a assinatura de um Security Group/firewall descartando o pacote em silêncio — diferente de
// "connection refused" (host alcançável, porta genuinamente fechada/sem nada escutando, ou um
// firewall no próprio host rejeitando ativamente, o que gera RST). Nenhuma das duas causas é bug
// desta checagem nem do certificado — por isso a mensagem nunca afirma isso, só explica o sinal.
// isTimeoutErr identifica um erro de timeout de rede (net.Error com Timeout()==true) — usado
// pra distinguir "não recebemos nada, como esperado" de "o servidor nos rejeitou".
func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func classifyTLSDialError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "certificate required"):
		return "Este servidor exige certificado de cliente (mTLS) para conectar — handshake recusado por não enviarmos nenhum certificado de cliente. Erro original: " + msg
	case strings.Contains(lower, "bad certificate"):
		return "Possível exigência de certificado de cliente (mTLS) — handshake falhou logo após não enviarmos nenhum certificado de cliente, mas esse erro também pode ter outras causas. Erro original: " + msg
	case strings.Contains(lower, "i/o timeout"):
		return "Sem resposta do host (timeout de conexão) — sinal típico de firewall/Security Group descartando o pacote em silêncio, não indício de problema no certificado nem nesta checagem. Esta checagem conecta a partir da rede/VPN onde o servidor desta aplicação está; se o destino só aceita conexões de dentro da própria rede (ex: só alcançável de dentro da própria instância/VPC, via SSM ou bastion), ele não vai validar por aqui mesmo com a VPN ativa — abrir o Security Group para a rede de onde esta aplicação roda é o que resolveria. Erro original: " + msg
	case strings.Contains(lower, "connection refused"):
		return "Conexão recusada — diferente de um timeout: o host respondeu, só que nada está escutando nesta porta (ou um firewall no próprio host rejeitou ativamente, gerando RST). O host está alcançável; a porta/serviço é que não está disponível. Erro original: " + msg
	default:
		return msg
	}
}
