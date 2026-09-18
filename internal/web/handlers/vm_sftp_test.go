package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// newTestGinContextWithQuery — mesmo padrão já usado em clusters_test.go/sreapproval_test.go
// (gin.CreateTestContext + httptest.NewRequest), só que populando query params arbitrários.
func newTestGinContextWithQuery(t *testing.T, query map[string]string) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	v := url.Values{}
	for k, val := range query {
		v.Set(k, val)
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+v.Encode(), nil)
	return c
}

func TestExtractUnknownHostKeyFingerprint(t *testing.T) {
	fp := "SHA256:AbCdEf1234567890+/=="

	cases := []struct {
		name      string
		err       error
		wantFound bool
		wantFP    string
	}{
		{
			name:      "sentinel puro",
			err:       fmt.Errorf("%s%s", hostKeyUnknownSentinel, fp),
			wantFound: true,
			wantFP:    fp,
		},
		{
			name:      "envolto por wrapping externo (handshake SSH)",
			err:       fmt.Errorf("erro no handshake SSH com ssm-tunnel-i-0123:0: %s%s", hostKeyUnknownSentinel, fp),
			wantFound: true,
			wantFP:    fp,
		},
		{
			name:      "fingerprint nunca deve ser truncada no primeiro ':' (formato SHA256:base64)",
			err:       fmt.Errorf("%sSHA256:xyz==", hostKeyUnknownSentinel),
			wantFound: true,
			wantFP:    "SHA256:xyz==",
		},
		{
			name:      "erro comum sem sentinel",
			err:       fmt.Errorf("dial tcp 10.0.0.1:22: connection refused"),
			wantFound: false,
		},
		{
			name:      "nil",
			err:       nil,
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotFP, gotFound := extractUnknownHostKeyFingerprint(tc.err)
			if gotFound != tc.wantFound {
				t.Fatalf("found = %v, esperado %v (err=%v)", gotFound, tc.wantFound, tc.err)
			}
			if tc.wantFound && gotFP != tc.wantFP {
				t.Errorf("fingerprint = %q, esperado %q", gotFP, tc.wantFP)
			}
		})
	}
}

func TestSftpTargetLabel_NuncaRecursivo(t *testing.T) {
	// Regressão real: um sed mal aplicado durante o desenvolvimento desta feature substituiu a
	// própria implementação de sftpTargetLabel por uma chamada a si mesma (recursão infinita,
	// stack overflow em runtime — só descoberto por inspeção manual do diff, não por este teste
	// na hora, mas travado aqui pra nunca mais regredir despercebido). Sem um gin.Context real
	// mockado, o teste mais simples e robusto é só confirmar que a função COMPILA e RETORNA algo
	// determinístico pro caso trivial (sem precisar de um *gin.Context de verdade) — feito
	// indiretamente via um teste de smoke no pacote inteiro (go vet/go build já pegam recursão
	// óbvia, mas um teste que de fato CHAMA a função garante que ela retorna, não trava).
	//
	// gin.Context exige um httptest.ResponseRecorder + *http.Request mínimos pra construir; como
	// sftpTargetLabel só lê query params, isso é barato de montar aqui.
	c := newTestGinContextWithQuery(t, map[string]string{"host": "10.0.0.1", "port": "22"})
	if got := sftpTargetLabel(c); got != "10.0.0.1:22" {
		t.Fatalf("sftpTargetLabel (modo host) = %q, esperado 10.0.0.1:22", got)
	}

	c2 := newTestGinContextWithQuery(t, map[string]string{"tunnelSessionId": "abc-123"})
	if got := sftpTargetLabel(c2); got != "ssm-tunnel:abc-123" {
		t.Fatalf("sftpTargetLabel (modo túnel) = %q, esperado ssm-tunnel:abc-123", got)
	}
}
