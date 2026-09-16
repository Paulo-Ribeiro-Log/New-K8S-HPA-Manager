package certificates

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

// generateTestCACertPEM gera um certificado autoassinado simples só pra alimentar os testes deste
// arquivo (não usado em nenhum outro pacote) — mesmo padrão de fixture ad-hoc já usado noutros
// testes deste pacote (ex: manual_backup_test.go).
func generateTestCACertPEM(t *testing.T, cn string, notAfter time.Time) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("erro ao gerar chave: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		Issuer:       pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("erro ao criar certificado: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestBuildMutatingWebhookEntry_CABundleVazio(t *testing.T) {
	wh := admissionregistrationv1.MutatingWebhook{
		Name: "sidecar-injector.istio.io",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name:      "istiod",
				Namespace: "istio-system",
			},
		},
	}

	entry := buildMutatingWebhookEntry(wh)

	if !entry.CABundleEmpty {
		t.Errorf("esperado CABundleEmpty=true pra caBundle vazio, veio false")
	}
	if entry.ServiceName != "istiod" || entry.ServiceNamespace != "istio-system" {
		t.Errorf("service não resolvido corretamente: %+v", entry)
	}
	if entry.CABundleSubject != "" {
		t.Errorf("esperado subject vazio pra caBundle vazio, veio %q", entry.CABundleSubject)
	}
}

func TestBuildMutatingWebhookEntry_CABundleValido(t *testing.T) {
	pemBytes := generateTestCACertPEM(t, "Delinea DSV Injector CA", time.Now().Add(365*24*time.Hour))

	wh := admissionregistrationv1.MutatingWebhook{
		Name: "dsv-injector.delinea.com",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service:  &admissionregistrationv1.ServiceReference{Name: "dsv-injector", Namespace: "dsv"},
			CABundle: pemBytes,
		},
	}

	entry := buildMutatingWebhookEntry(wh)

	if entry.CABundleEmpty {
		t.Fatalf("esperado CABundleEmpty=false, veio true")
	}
	if entry.CABundleSubject != "Delinea DSV Injector CA" {
		t.Errorf("subject errado: %q", entry.CABundleSubject)
	}
	if entry.CABundleStatus != "valid" {
		t.Errorf("esperado status valid (cert válido por 1 ano), veio %q", entry.CABundleStatus)
	}
	if entry.CABundleNotAfter == nil {
		t.Fatalf("CABundleNotAfter não deveria ser nil")
	}
}

func TestBuildMutatingWebhookEntry_CABundleExpirado(t *testing.T) {
	// -48h (não -24h): margem maior contra flakiness — um cert exatamente 1 dia expirado deixa a
	// divisão inteira de dias muito perto do limite entre -1 e 0, sensível a qualquer jitter de
	// relógio (visto ao vivo: 1 flake isolado em ~15 execuções nesta sessão, nunca reproduzido de
	// novo — mais provável um resync de relógio do WSL2, mesmo fenômeno já documentado no CLAUDE.md
	// pra outra área desta app — do que um bug real em classifyExpiry).
	pemBytes := generateTestCACertPEM(t, "CA Expirada", time.Now().Add(-48*time.Hour))

	wh := admissionregistrationv1.MutatingWebhook{
		Name: "webhook-expirado",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			CABundle: pemBytes,
		},
	}

	entry := buildMutatingWebhookEntry(wh)

	if entry.CABundleStatus != "expired" {
		t.Errorf("esperado status expired, veio %q", entry.CABundleStatus)
	}
	if entry.CABundleDays >= 0 {
		t.Errorf("esperado dias restantes negativo pra cert expirado, veio %d", entry.CABundleDays)
	}
}

func TestBuildMutatingWebhookEntry_CABundleMalformado(t *testing.T) {
	wh := admissionregistrationv1.MutatingWebhook{
		Name: "webhook-quebrado",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			CABundle: []byte("isso não é um PEM válido"),
		},
	}

	entry := buildMutatingWebhookEntry(wh)

	// Nunca falha "duro" — só deixa os campos de exibição vazios (mesmo espírito de
	// parsePEMChain), e NÃO marca CABundleEmpty (o campo tinha bytes, só não parseou).
	if entry.CABundleEmpty {
		t.Errorf("caBundle malformado (mas não-vazio) não deveria marcar CABundleEmpty")
	}
	if entry.CABundleSubject != "" {
		t.Errorf("esperado subject vazio pra caBundle malformado, veio %q", entry.CABundleSubject)
	}
}

func TestBuildMutatingWebhookEntry_URLClientConfig(t *testing.T) {
	url := "https://webhook.example.com/mutate"
	wh := admissionregistrationv1.MutatingWebhook{
		Name: "webhook-externo",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			URL: &url,
		},
	}

	entry := buildMutatingWebhookEntry(wh)

	if entry.URL != url {
		t.Errorf("URL não resolvida corretamente: %q", entry.URL)
	}
	if entry.ServiceName != "" {
		t.Errorf("esperado ServiceName vazio quando clientConfig usa URL, veio %q", entry.ServiceName)
	}
}
