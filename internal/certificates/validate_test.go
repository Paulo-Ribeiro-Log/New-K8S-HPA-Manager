package certificates

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// testCert é um certificado gerado em memória pra teste, junto com sua chave privada e o PEM já
// codificado — evita depender de fixtures externas (arquivos .pem no repo).
type testCert struct {
	cert    *x509.Certificate
	key     *rsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

var testCertSerial int64 = 1

func nextSerial() *big.Int {
	testCertSerial++
	return big.NewInt(testCertSerial)
}

// genCert gera um certificado auto-assinado (se signer for nil) ou assinado por signer/signerKey.
func genCert(t *testing.T, cn string, isCA bool, notBefore, notAfter time.Time, signer *testCert) *testCert {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("erro ao gerar chave: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	parentTmpl := tmpl
	parentKey := key
	if signer != nil {
		parentTmpl = signer.cert
		parentKey = signer.key
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentTmpl, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("erro ao criar certificado: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("erro ao parsear certificado gerado: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return &testCert{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}
}

func concatPEM(certs ...*testCert) []byte {
	var out []byte
	for _, c := range certs {
		out = append(out, c.certPEM...)
	}
	return out
}

func TestValidateCertificateChain_ValidChain_PrivateCA_WarnsNotErrors(t *testing.T) {
	now := time.Now()
	root := genCert(t, "Root CA de Teste", true, now.Add(-time.Hour), now.Add(10*365*24*time.Hour), nil)
	intermediate := genCert(t, "Intermediate CA de Teste", true, now.Add(-time.Hour), now.Add(5*365*24*time.Hour), root)
	leaf := genCert(t, "app.exemplo.com", false, now.Add(-time.Hour), now.Add(90*24*time.Hour), intermediate)

	certPEM := concatPEM(leaf, intermediate, root)
	result, err := ValidateCertificateChain(certPEM, leaf.keyPEM)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if !result.KeyMatchesCert {
		t.Error("esperava KeyMatchesCert=true")
	}
	if !result.ChainOrderCorrect {
		t.Errorf("esperava ChainOrderCorrect=true, errors: %v", result.Errors)
	}
	if !result.ExpiryOK {
		t.Error("esperava ExpiryOK=true")
	}
	if result.TrustedByPublicCA {
		t.Error("esperava TrustedByPublicCA=false — CA de teste não está no root store do sistema")
	}
	if !result.Valid {
		t.Errorf("esperava Valid=true (CA privada é warning, não erro) — errors: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Error("esperava pelo menos 1 warning sobre CA não-pública")
	}
	wantSubjects := []string{"app.exemplo.com", "Intermediate CA de Teste", "Root CA de Teste"}
	for i, want := range wantSubjects {
		if i >= len(result.ChainSubjects) || result.ChainSubjects[i] != want {
			t.Errorf("ChainSubjects[%d] = %v, want %q", i, result.ChainSubjects, want)
		}
	}
}

func TestValidateCertificateChain_KeyMismatch(t *testing.T) {
	now := time.Now()
	leaf := genCert(t, "app.exemplo.com", false, now.Add(-time.Hour), now.Add(90*24*time.Hour), nil)
	other := genCert(t, "outra-chave", false, now.Add(-time.Hour), now.Add(90*24*time.Hour), nil)

	result, err := ValidateCertificateChain(leaf.certPEM, other.keyPEM)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if result.KeyMatchesCert {
		t.Error("esperava KeyMatchesCert=false — chave de outro par")
	}
	if result.Valid {
		t.Error("esperava Valid=false")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "não corresponde") {
			found = true
		}
	}
	if !found {
		t.Errorf("esperava erro mencionando chave não corresponder, got %v", result.Errors)
	}
}

func TestValidateCertificateChain_WrongOrder(t *testing.T) {
	now := time.Now()
	root := genCert(t, "Root CA de Teste", true, now.Add(-time.Hour), now.Add(10*365*24*time.Hour), nil)
	intermediate := genCert(t, "Intermediate CA de Teste", true, now.Add(-time.Hour), now.Add(5*365*24*time.Hour), root)
	leaf := genCert(t, "app.exemplo.com", false, now.Add(-time.Hour), now.Add(90*24*time.Hour), intermediate)

	// Ordem errada: leaf certo em [0] (chave ainda bate), mas [1]/[2] trocados — root antes do
	// intermediário. certs[0].CheckSignatureFrom(certs[1]) checa "leaf foi assinado por root?",
	// que é falso (leaf foi assinado pelo intermediário).
	certPEM := concatPEM(leaf, root, intermediate)
	result, err := ValidateCertificateChain(certPEM, leaf.keyPEM)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !result.KeyMatchesCert {
		t.Error("esperava KeyMatchesCert=true — leaf continua em [0], só a ordem dos demais mudou")
	}
	if result.ChainOrderCorrect {
		t.Error("esperava ChainOrderCorrect=false")
	}
	if result.Valid {
		t.Error("esperava Valid=false")
	}
}

func TestValidateCertificateChain_Expired(t *testing.T) {
	now := time.Now()
	leaf := genCert(t, "app.exemplo.com", false, now.Add(-100*24*time.Hour), now.Add(-1*24*time.Hour), nil)

	result, err := ValidateCertificateChain(leaf.certPEM, leaf.keyPEM)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if result.ExpiryOK {
		t.Error("esperava ExpiryOK=false")
	}
	if result.Valid {
		t.Error("esperava Valid=false pra certificado expirado")
	}
}

func TestValidateCertificateChain_InvalidPEM(t *testing.T) {
	_, err := ValidateCertificateChain([]byte("not a pem"), []byte("not a pem either"))
	if err == nil {
		t.Error("esperava erro pra PEM inválido")
	}
}
