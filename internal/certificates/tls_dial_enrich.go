package certificates

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
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
	// IsDefaultFakeCert — true quando o host respondeu com o certificado autoassinado padrão do
	// ingress-nginx (isIngressNginxDefaultFakeCert, parser.go) em vez do certificado esperado.
	// Achado real: sem essa checagem, esse cenário caía na comparação genérica de IssuerCN e era
	// classificado como PossibleExternalLayer ("provavelmente CDN/WAF/proxy corporativo") — um
	// diagnóstico errado, já que a causa real é o host não bater com nenhum Ingress válido no
	// ingress-nginx, não uma camada externa terminando TLS antes do cluster.
	IsDefaultFakeCert bool
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
		Host:              host,
		SerialDec:         leaf.SerialNumber.String(),
		IssuerCN:          leaf.Issuer.CommonName,
		NotAfter:          leaf.NotAfter,
		IsDefaultFakeCert: isIngressNginxDefaultFakeCert(leaf),
	}
}

// EnrichWithTLSDial conecta em cada host de `hosts` na porta 443 em paralelo (timeout por host,
// nunca por lote inteiro) e compara o serial do certificado realmente servido com
// leafSerialDecimal — funciona independente de quem termina o TLS (nginx, Istio, Traefik, GCP LB
// do GKE Gateway, ALB da AWS), ao contrário de EnrichWithPrometheus que só enxerga ingress-nginx.
// leafIssuerCN (LeafIssuerCN) é usado só pra classificar hosts "stale" — ver buildTLSDialResult.
func EnrichWithTLSDial(ctx context.Context, hosts []string, leafSerialDecimal, leafIssuerCN string) *LivePropagationResult {
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

	return buildTLSDialResult(results, leafSerialDecimal, leafIssuerCN)
}

// buildTLSDialResult é a lógica pura de agregação — separada de EnrichWithTLSDial para ser
// testável sem rede real.
//
// Classificação de host "stale" (serial diferente do esperado) em 3 baldes disjuntos:
//   - DefaultFakeCert (checado primeiro, prioridade sobre os outros dois): o host respondeu com o
//     certificado autoassinado padrão do ingress-nginx (isIngressNginxDefaultFakeCert) — não é uma
//     questão de propagação nem de camada externa, é o host não bater com nenhum Ingress válido
//     configurado nesse ingress-nginx (SNI sem match). Achado real: mesma armadilha já corrigida no
//     Monitor de Certificados Externos (endpoint_check.go) também existe aqui — sem essa checagem,
//     esse cenário caía indistinguível de PossibleExternalLayer (emissor diferente do esperado).
//   - mesmo emissor do esperado → ReplicasStale: é plausível que seja o MESMO certificado numa
//     versão anterior (renovação recente ainda não propagada) — caso genuíno de propagação atrasada.
//   - emissor diferente → PossibleExternalLayer: não é uma versão antiga do certificado esperado,
//     é um certificado de outra autoridade — sinal de que existe uma camada externa (CDN/WAF/proxy
//     corporativo) terminando TLS antes do tráfego chegar no cluster. Achado real: cluster EKS onde
//     o Secret tem um cert Sectigo mas o host público serve um wildcard corporativo da DigiCert —
//     nesse caso "propagação em andamento" é uma mensagem enganosa, o Secret nunca vai "propagar"
//     porque o cliente público nunca fala TLS direto com o ingress-nginx.
//
// Best-effort: se leafIssuerCN vier vazio (falha ao parsear o PEM esperado) ou o host não retornar
// IssuerCN, cai no balde ReplicasStale (comportamento conservador — não afirma "camada externa"
// sem emissor pra comparar).
func buildTLSDialResult(results []tlsDialResult, leafSerialDecimal, leafIssuerCN string) *LivePropagationResult {
	result := &LivePropagationResult{Method: "tls-dial"}

	var reached int
	var dialErrors []string
	for _, r := range results {
		if r.Err != nil {
			dialErrors = append(dialErrors, fmt.Sprintf("%s: %s", r.Host, r.Err.Error()))
			continue
		}
		reached++
		switch {
		case r.SerialDec == leafSerialDecimal:
			result.ReplicasCurrent++
		case r.IsDefaultFakeCert:
			result.DefaultFakeCert = append(result.DefaultFakeCert, r.Host)
		case leafIssuerCN != "" && r.IssuerCN != "" && r.IssuerCN != leafIssuerCN:
			result.PossibleExternalLayer = append(result.PossibleExternalLayer, r.Host)
		default:
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

	if len(result.DefaultFakeCert) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d de %d host(s) respondem com o certificado autoassinado PADRÃO do ingress-nginx, não o certificado esperado — esse host não bate com nenhum Ingress válido configurado nesse ingress-nginx (SNI sem match), não é uma questão de propagação nem de camada externa: %s",
			len(result.DefaultFakeCert), reached, strings.Join(result.DefaultFakeCert, ", "),
		))
	}
	if len(result.ReplicasStale) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d de %d host(s) ainda servem um certificado diferente do atual, mesmo emissor esperado (handshake TLS direto) — propagação pode estar em andamento",
			len(result.ReplicasStale), reached,
		))
	}
	if len(result.PossibleExternalLayer) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d de %d host(s) servem um certificado de emissor completamente diferente do esperado — provavelmente existe uma camada externa (CDN/WAF/proxy corporativo) terminando TLS antes do cluster; isso NÃO é necessariamente um problema de propagação, é comum nessa arquitetura",
			len(result.PossibleExternalLayer), reached,
		))
	}

	return result
}
