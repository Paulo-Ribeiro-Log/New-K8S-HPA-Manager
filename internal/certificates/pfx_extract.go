package certificates

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// ExtractPFX decodifica um arquivo .pfx/.p12 (PKCS#12) e devolve tlsCrt (leaf + chain já
// concatenados em PEM, ordem leaf-primeiro — mesma convenção que um Secret kubernetes.io/tls
// espera) e tlsKey (chave privada em PEM, sempre SEM senha), além do leaf já parseado (pra
// popular metadata sem precisar reparsear tlsCrt em seguida).
//
// Usa software.sslmate.com/src/go-pkcs12 (fork ativamente mantido) em vez da alternativa já
// indiretamente vendorizada golang.org/x/crypto/pkcs12 — essa última é legada e só suporta
// RC2/3DES, falhando em .pfx modernos com criptografia AES (mesma classe de problema já vista
// nesta investigação com chaves "ENCRYPTED PRIVATE KEY": formato mais novo, ferramenta antiga
// não entende). Ver PFX-CERT-EXTRACTION-PLAN.md.
func ExtractPFX(pfxBytes []byte, password string) (tlsCrt, tlsKey []byte, leaf *x509.Certificate, err error) {
	privateKey, cert, caCerts, err := pkcs12.DecodeChain(pfxBytes, password)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao decodificar .pfx: %w", err)
	}
	if cert == nil {
		return nil, nil, nil, fmt.Errorf(".pfx não contém certificado")
	}

	// tlsCrt: leaf primeiro, seguido das intermediárias/raiz (se houver) — a chain inteira num
	// único tls.crt, exatamente o formato que um Secret kubernetes.io/tls espera (ver decisão em
	// PFX-CERT-EXTRACTION-PLAN.md: "no uso real desta empresa a chain já fica dentro do tls.crt").
	var crtBuf bytes.Buffer
	if err := pem.Encode(&crtBuf, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao codificar leaf em PEM: %w", err)
	}
	for _, ca := range caCerts {
		if err := pem.Encode(&crtBuf, &pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}); err != nil {
			return nil, nil, nil, fmt.Errorf("erro ao codificar certificado da chain em PEM: %w", err)
		}
	}

	// tlsKey: PKCS#8 SEM senha (-----BEGIN PRIVATE KEY-----) — nunca criptografado. Resolve de
	// saída o mesmo problema já visto com chaves "ENCRYPTED PRIVATE KEY": um Secret K8s não tem
	// como fornecer senha no momento do handshake TLS, então a chave extraída aqui já sai pronta
	// pra uso, sem precisar de um passo de descriptografia manual depois.
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao serializar chave privada: %w", err)
	}
	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao codificar chave privada em PEM: %w", err)
	}

	return crtBuf.Bytes(), keyBuf.Bytes(), cert, nil
}
