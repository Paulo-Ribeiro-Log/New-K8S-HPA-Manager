package certificates

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ExpiringThresholdDays define o limiar em dias para considerar um certificado "expirando"
const ExpiringThresholdDays = 30

// classifyExpiry classifica o status de expiração de um certificado a partir do seu NotAfter,
// usando ExpiringThresholdDays como limiar. Compartilhada entre ParseTLSSecret (Secret K8s) e
// CheckEndpointTLS (endpoint externo, ver endpoint_check.go) — mesmo limiar, uma única fonte de
// verdade em vez de duplicar a lógica em cada caminho.
func classifyExpiry(notAfter time.Time) (status string, daysRemaining int) {
	now := time.Now()
	daysRemaining = int(notAfter.Sub(now).Hours() / 24)
	switch {
	case now.After(notAfter):
		return "expired", daysRemaining
	case daysRemaining <= ExpiringThresholdDays:
		return "expiring", daysRemaining
	default:
		return "valid", daysRemaining
	}
}

// ParseTLSSecret parseia um Secret do tipo kubernetes.io/tls e extrai informações do certificado
func ParseTLSSecret(secret *corev1.Secret, cluster string) (*CertificateInfo, error) {
	certPEM, ok := secret.Data["tls.crt"]
	if !ok || len(certPEM) == 0 {
		return nil, fmt.Errorf("secret %s/%s não contém tls.crt", secret.Namespace, secret.Name)
	}

	// Parsear todos os certificados na chain
	certs, err := parsePEMChain(certPEM)
	if err != nil {
		return nil, fmt.Errorf("erro ao parsear PEM de %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("nenhum certificado encontrado em %s/%s", secret.Namespace, secret.Name)
	}

	// O primeiro certificado é o leaf (principal)
	leaf := certs[0]

	info := &CertificateInfo{
		SecretName:   secret.Name,
		Namespace:    secret.Namespace,
		Cluster:      cluster,
		Subject:      leaf.Subject.CommonName,
		Issuer:       leaf.Issuer.CommonName,
		SerialNumber: formatSerialNumber(leaf.SerialNumber),
		NotBefore:    leaf.NotBefore,
		NotAfter:     leaf.NotAfter,
		DNSNames:     leaf.DNSNames,
		IsCA:         leaf.IsCA,
		KeyAlgorithm: getKeyAlgorithm(leaf),
		KeySize:      getKeySize(leaf),
		ChainLength:  len(certs),
	}

	// Calcular status e dias restantes
	info.Status, info.DaysRemaining = classifyExpiry(leaf.NotAfter)

	// Chain details (excluindo o leaf)
	if len(certs) > 1 {
		for _, cert := range certs[1:] {
			info.ChainDetails = append(info.ChainDetails, ChainCertInfo{
				Subject:  cert.Subject.CommonName,
				Issuer:   cert.Issuer.CommonName,
				NotAfter: cert.NotAfter,
				IsCA:     cert.IsCA,
			})
		}
	}

	// Detectar cert-manager via annotations
	info.CertManager = extractCertManagerInfo(secret)

	return info, nil
}

// ValidatePEM verifica se os dados PEM são válidos (certificado e chave)
func ValidatePEM(certPEM, keyPEM []byte) error {
	certs, err := parsePEMChain(certPEM)
	if err != nil {
		return fmt.Errorf("certificado PEM inválido: %w", err)
	}
	if len(certs) == 0 {
		return fmt.Errorf("nenhum certificado encontrado no PEM")
	}

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("chave PEM inválida: nenhum bloco PEM encontrado")
	}

	return nil
}

// parsePEMChain parseia todos os certificados em um bloco PEM (chain)
func parsePEMChain(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := data

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("erro ao parsear certificado: %w", err)
		}
		certs = append(certs, cert)
	}

	return certs, nil
}

// formatSerialNumber formata o serial number do certificado em hexadecimal
func formatSerialNumber(sn *big.Int) string {
	if sn == nil {
		return ""
	}
	return fmt.Sprintf("%X", sn)
}

// getKeyAlgorithm retorna o nome do algoritmo da chave pública
func getKeyAlgorithm(cert *x509.Certificate) string {
	switch cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "ECDSA"
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return cert.PublicKeyAlgorithm.String()
	}
}

// getKeySize retorna o tamanho da chave em bits
func getKeySize(cert *x509.Certificate) int {
	switch key := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return key.N.BitLen()
	case *ecdsa.PublicKey:
		return key.Curve.Params().BitSize
	default:
		return 0
	}
}

// extractCertManagerInfo extrai informações do cert-manager das annotations do Secret
func extractCertManagerInfo(secret *corev1.Secret) *CertManagerInfo {
	if secret.Annotations == nil {
		return nil
	}

	certName := secret.Annotations["cert-manager.io/certificate-name"]
	issuerName := secret.Annotations["cert-manager.io/issuer-name"]

	// Se não tem annotations do cert-manager, retornar nil
	if certName == "" && issuerName == "" {
		return nil
	}

	issuerKind := secret.Annotations["cert-manager.io/issuer-kind"]
	if issuerKind == "" {
		issuerKind = "Issuer"
	}

	return &CertManagerInfo{
		CertificateName: certName,
		IssuerName:      issuerName,
		IssuerKind:      issuerKind,
		IsReady:         true, // Se o secret existe, o certificado foi emitido
	}
}
