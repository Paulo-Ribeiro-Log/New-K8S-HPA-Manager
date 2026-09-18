package certificates

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// ValidateCertKeyPair confirma que keyPEM é de fato a chave privada do certificado leaf em
// certPEM — não só "os dois são PEM válidos" (ValidatePEM já garante isso), mas que a chave
// pública do certificado bate byte a byte com a chave privada fornecida (mesmo mecanismo de
// certMatchesPrivateKey, já usado por ExtractPFX pra achar o leaf certo num .pfx com bags fora de
// ordem). Nunca faz rede — só parsing local, pensado pra rejeitar um par cert+chave incompatível
// ANTES de qualquer transferência real pra uma VM (ver VM-EC2-PLAN, Fase 6).
func ValidateCertKeyPair(certPEM, keyPEM []byte) (*x509.Certificate, error) {
	certs, err := parsePEMChain(certPEM)
	if err != nil {
		return nil, fmt.Errorf("certificado PEM inválido: %w", err)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("nenhum certificado encontrado no PEM")
	}
	leaf := certs[0]

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("chave PEM inválida: nenhum bloco PEM encontrado")
	}

	privKey, err := parsePrivateKeyBlock(block)
	if err != nil {
		return nil, fmt.Errorf("chave privada inválida: %w", err)
	}

	if !certMatchesPrivateKey(leaf, privKey) {
		return nil, fmt.Errorf("o certificado e a chave privada não correspondem (chaves públicas diferentes)")
	}

	return leaf, nil
}

// parsePrivateKeyBlock decodifica um bloco PEM de chave privada nos 3 formatos comuns — PKCS#1
// ("RSA PRIVATE KEY"), SEC1 ("EC PRIVATE KEY") e PKCS#8 (genérico, "PRIVATE KEY") — tentando
// primeiro o formato que o próprio Type do bloco sugere, com fallback pros outros dois (alguns
// geradores rotulam o bloco de forma inconsistente com o conteúdo real).
func parsePrivateKeyBlock(block *pem.Block) (interface{}, error) {
	switch block.Type {
	case "RSA PRIVATE KEY":
		if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			return k, nil
		}
	case "EC PRIVATE KEY":
		if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
			return k, nil
		}
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("formato de chave não reconhecido (esperado PKCS#1/SEC1/PKCS#8)")
}
