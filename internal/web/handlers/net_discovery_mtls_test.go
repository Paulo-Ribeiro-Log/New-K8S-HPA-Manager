package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"
)

// ─── "Descoberta de Rede" — mTLS (certificado de cliente), pedido explícito do usuário depois de
// perguntar se ter o certificado "que já existe nesses clusters/servidores" ajudaria a descoberta.
// Ver comentário completo em netDiscoveryFingerprintScript (net_discovery_fingerprint.go) — o
// mecanismo em si (stdin+awk+curl --cert/--key) foi validado ao vivo (openssl s_server e um
// servidor Go real com tls.RequireAndVerifyClientCert, dentro da MESMA imagem netshoot usada em
// produção) antes de escrever qualquer linha de Go — estes testes cobrem só a parte Go
// (validação/parsing), que não depende de rede/Docker pra rodar em CI.

// genTestCertKeyPEM gera um par certificado+chave autoassinado válido só pra teste — sem depender
// de arquivo externo nenhum, cada teste é hermético.
func genTestCertKeyPEM(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave RSA de teste: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "net-discovery-test-client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado de teste: %v", err)
	}
	var certBuf, keyBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("codificar PEM do certificado: %v", err)
	}
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		t.Fatalf("codificar PEM da chave: %v", err)
	}
	return certBuf.String(), keyBuf.String()
}

func TestValidateClientCertPair_ParValido(t *testing.T) {
	certPEM, keyPEM := genTestCertKeyPEM(t)
	if err := validateClientCertPair(certPEM, keyPEM); err != nil {
		t.Errorf("par cert+chave genuinamente válido rejeitado: %v", err)
	}
}

func TestValidateClientCertPair_ChaveNaoBateComCertificado(t *testing.T) {
	certPEM, _ := genTestCertKeyPEM(t)
	_, keyPEMOutro := genTestCertKeyPEM(t) // chave de OUTRO par — não corresponde ao certificado acima
	err := validateClientCertPair(certPEM, keyPEMOutro)
	if err == nil {
		t.Fatal("par cert+chave incompatíveis deveria ser rejeitado, não foi")
	}
}

func TestValidateClientCertPair_TextoInvalido(t *testing.T) {
	if err := validateClientCertPair("isso não é um certificado", "isso não é uma chave"); err == nil {
		t.Error("texto que não é PEM nenhum deveria ser rejeitado")
	}
}

func TestValidateClientCertPair_ConteúdoComLinhaDoMarcadorReservado(t *testing.T) {
	certPEM, keyPEM := genTestCertKeyPEM(t)
	// Simula (de forma extremamente improvável na prática) um PEM que por acidente contém a mesma
	// linha usada internamente pra separar cert/chave no stdin — precisa ser rejeitado ANTES de
	// tentar rodar a descoberta, nunca silenciosamente corromper o split dentro do container.
	poisoned := certPEM + "\n" + netDiscoveryMTLSSplitMarker + "\n"
	if err := validateClientCertPair(poisoned, keyPEM); err == nil {
		t.Error("PEM contendo a linha do marcador reservado deveria ser rejeitado")
	}
}

func TestBuildMTLSStdinPayload_SemCertificadoDevolveVazio(t *testing.T) {
	r := buildMTLSStdinPayload("", "")
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(b) != 0 {
		t.Errorf("payload sem cert deveria ser vazio, veio %q", string(b))
	}
}

func TestBuildMTLSStdinPayload_SplitCorreto(t *testing.T) {
	certPEM, keyPEM := genTestCertKeyPEM(t)
	r := buildMTLSStdinPayload(certPEM, keyPEM)
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	payload := string(b)

	parts := strings.SplitN(payload, netDiscoveryMTLSSplitMarker+"\n", 2)
	if len(parts) != 2 {
		t.Fatalf("payload não contém exatamente 1 marcador de split, partes=%d", len(parts))
	}
	gotCert := strings.TrimRight(parts[0], "\n")
	gotKey := strings.TrimRight(parts[1], "\n")
	if gotCert != strings.TrimRight(certPEM, "\n") {
		t.Errorf("bloco do certificado não bate\ngot:  %q\nwant: %q", gotCert, strings.TrimRight(certPEM, "\n"))
	}
	if gotKey != strings.TrimRight(keyPEM, "\n") {
		t.Errorf("bloco da chave não bate\ngot:  %q\nwant: %q", gotKey, strings.TrimRight(keyPEM, "\n"))
	}
	// awk do script real casa a linha do marcador via regex ancorada (^...$) — precisa estar
	// sozinha na própria linha, nunca colada ao conteúdo adjacente.
	if !strings.Contains(payload, "\n"+netDiscoveryMTLSSplitMarker+"\n") {
		t.Error("marcador não está isolado em sua própria linha — o awk do script real não bateria com /^marker$/")
	}
}

func TestParseFingerprintOutput_MTLSUsedFlag(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"marcado como 1", "@@TTL 52\n@@MTLS_USED 1\n", true},
		{"marcado como 0", "@@TTL 52\n@@MTLS_USED 0\n", false},
		{"ausente (descoberta sem mTLS, saída antiga)", "@@TTL 52\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fp := parseFingerprintOutput(c.output)
			if fp.ClientCertUsed != c.want {
				t.Errorf("ClientCertUsed = %v, esperava %v", fp.ClientCertUsed, c.want)
			}
		})
	}
}
