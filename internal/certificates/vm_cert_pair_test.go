package certificates

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// genCertPEM gera um par certificado+chave autoassinado de verdade (x509.CreateCertificate, sem
// TLS/rede), devolvendo o certificado em PEM e a chave privada no formato pedido — usado para
// testar ValidateCertKeyPair contra todos os formatos de chave que ela precisa reconhecer.
func genCertPEM(t *testing.T, keyFormat string) (certPEM, keyPEM []byte, key interface{}) {
	t.Helper()

	var pub interface{}
	switch keyFormat {
	case "rsa-pkcs1", "rsa-pkcs8":
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("gerar chave RSA: %v", err)
		}
		key = rsaKey
		pub = &rsaKey.PublicKey
	case "ec-sec1", "ec-pkcs8":
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("gerar chave EC: %v", err)
		}
		key = ecKey
		pub = &ecKey.PublicKey
	default:
		t.Fatalf("formato de chave desconhecido no teste: %s", keyFormat)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "vm-cert-pair-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	switch keyFormat {
	case "rsa-pkcs1":
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key.(*rsa.PrivateKey))})
	case "rsa-pkcs8":
		b, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("marshal PKCS8: %v", err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
	case "ec-sec1":
		b, err := x509.MarshalECPrivateKey(key.(*ecdsa.PrivateKey))
		if err != nil {
			t.Fatalf("marshal SEC1: %v", err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	case "ec-pkcs8":
		b, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("marshal PKCS8: %v", err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
	}

	return certPEM, keyPEM, key
}

func TestValidateCertKeyPair_ParFormatosSuportados(t *testing.T) {
	for _, format := range []string{"rsa-pkcs1", "rsa-pkcs8", "ec-sec1", "ec-pkcs8"} {
		t.Run(format, func(t *testing.T) {
			certPEM, keyPEM, _ := genCertPEM(t, format)
			leaf, err := ValidateCertKeyPair(certPEM, keyPEM)
			if err != nil {
				t.Fatalf("par válido rejeitado (%s): %v", format, err)
			}
			if leaf.Subject.CommonName != "vm-cert-pair-test" {
				t.Errorf("leaf inesperado: %+v", leaf.Subject)
			}
		})
	}
}

func TestValidateCertKeyPair_ParIncompativelRejeitado(t *testing.T) {
	certPEM, _, _ := genCertPEM(t, "rsa-pkcs1")
	_, outraChavePEM, _ := genCertPEM(t, "rsa-pkcs1") // chave de OUTRO par, nunca corresponde ao certPEM acima

	_, err := ValidateCertKeyPair(certPEM, outraChavePEM)
	if err == nil {
		t.Fatal("esperava erro para par cert+chave incompatível, mas passou")
	}
}

func TestValidateCertKeyPair_PEMInvalidoRejeitadoSemPanico(t *testing.T) {
	if _, err := ValidateCertKeyPair([]byte("não é PEM"), []byte("também não é PEM")); err == nil {
		t.Fatal("esperava erro para certPEM inválido")
	}

	certPEM, _, _ := genCertPEM(t, "rsa-pkcs1")
	if _, err := ValidateCertKeyPair(certPEM, []byte("chave inválida")); err == nil {
		t.Fatal("esperava erro para keyPEM inválido")
	}
}
