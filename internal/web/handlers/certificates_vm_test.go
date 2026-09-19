package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// genTestRSACertPEM gera um par certificado+chave RSA autoassinado de verdade (sem TLS/rede) —
// mesmo padrão já usado em internal/certificates/vm_cert_pair_test.go, duplicado aqui em escala
// menor porque handlers não importa internal/certificates só pra gerar fixture de teste.
func genTestRSACertPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	// Serial ALEATÓRIO (não um valor fixo) — igual a qualquer CA real, que nunca reemite o mesmo
	// serial pra dois certificados diferentes; um valor fixo faria dois certs de teste
	// DISTINTOS (chaves diferentes) parecerem o MESMO certificado na comparação de
	// matches_target, mascarando exatamente o cenário que TestValidateCertKeyPairHandler_
	// LiveCheckDetectaCertificadoDiferente existe pra cobrir.
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("gerar serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "certificates-vm-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func newVMCertTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// sftp=nil de propósito — os testes deste arquivo só cobrem os caminhos que rejeitam ANTES
	// de qualquer tentativa de sessão SFTP (par inválido); nenhum deles deveria alcançar
	// h.sftp.OpenFileSession. Se algum teste futuro precisar do caminho feliz de transferência,
	// precisará de um *VMSFTPHandler real com credStore configurado.
	h := NewVMCertificatesHandler(nil, nil)
	r.POST("/validate", h.ValidateCertKeyPair)
	r.POST("/:instanceId/transfer", h.TransferCertificate)
	r.GET("/:instanceId/read", h.ReadRemoteCertificate)
	r.POST("/:instanceId/transfer-ssm", h.TransferCertificateViaSSM)
	r.GET("/:instanceId/read-ssm", h.ReadRemoteCertificateViaSSM)
	return httptest.NewServer(r)
}

func postJSON(t *testing.T, url string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp, decoded
}

func TestValidateCertKeyPairHandler_ParValidoRetornaCertificado(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, keyPEM := genTestRSACertPEM(t)
	_, body := postJSON(t, srv.URL+"/validate", map[string]string{
		"certPem": string(certPEM),
		"keyPem":  string(keyPEM),
	})

	if success, _ := body["success"].(bool); !success {
		t.Fatalf("esperava success=true, corpo: %+v", body)
	}
	cert, ok := body["certificate"].(map[string]interface{})
	if !ok {
		t.Fatalf("esperava campo 'certificate' na resposta, corpo: %+v", body)
	}
	if subject, _ := cert["subject"].(string); subject != "certificates-vm-test" {
		t.Errorf("subject = %q, esperado %q", subject, "certificates-vm-test")
	}
	if _, hasLive := body["live_check"]; hasLive {
		t.Error("não deveria ter live_check sem checkHost/checkPort informados")
	}
}

func TestValidateCertKeyPairHandler_ParIncompativelRejeitadoSemLiveCheck(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, _ := genTestRSACertPEM(t)
	_, outraChavePEM := genTestRSACertPEM(t)

	resp, body := postJSON(t, srv.URL+"/validate", map[string]interface{}{
		"certPem":   string(certPEM),
		"keyPem":    string(outraChavePEM),
		"checkHost": "example.invalid", // nunca deveria ser discado — par já é rejeitado antes
		"checkPort": 443,
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (erro reportado no corpo, não no status)", resp.StatusCode)
	}
	if success, _ := body["success"].(bool); success {
		t.Fatalf("esperava success=false para par incompatível, corpo: %+v", body)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_CERT_PAIR" {
		t.Fatalf("esperava error.code=INVALID_CERT_PAIR, corpo: %+v", body)
	}
	if _, hasLive := body["live_check"]; hasLive {
		t.Error("live_check nunca deveria aparecer quando o par local já foi rejeitado")
	}
}

func TestTransferCertificateHandler_ParIncompativelRejeitadoAntesDoSFTP(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, _ := genTestRSACertPEM(t)
	_, outraChavePEM := genTestRSACertPEM(t)

	// h.sftp é nil neste servidor de teste — se o handler tentasse abrir uma sessão SFTP antes
	// de validar o par, isso panicaria (nil pointer) em vez de retornar um JSON de erro limpo.
	// O teste passa se, e só se, a validação local abortar ANTES de qualquer uso de h.sftp.
	resp, body := postJSON(t, srv.URL+"/i-12345/transfer", map[string]interface{}{
		"host":                "10.0.0.1",
		"port":                22,
		"credentialProfileId": "perfil-qualquer",
		"certPem":             string(certPEM),
		"keyPem":              string(outraChavePEM),
		"remoteCertPath":      "/etc/ssl/tls.crt",
		"remoteKeyPath":       "/etc/ssl/tls.key",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (erro reportado no corpo)", resp.StatusCode)
	}
	if success, _ := body["success"].(bool); success {
		t.Fatalf("esperava success=false para par incompatível, corpo: %+v", body)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_CERT_PAIR" {
		t.Fatalf("esperava error.code=INVALID_CERT_PAIR, corpo: %+v", body)
	}
}

func TestValidateCertKeyPairHandler_LiveCheckContraServidorTLSReal(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, keyPEM := genTestRSACertPEM(t)
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("montar tls.Certificate a partir do par gerado: %v", err)
	}

	// Servidor TLS real servindo EXATAMENTE o par que será validado — handshake de verdade, sem
	// mock de rede (mesmo princípio de TestCheckEndpointTLS_HandshakeReal em
	// internal/certificates/endpoint_check_test.go).
	tlsSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tlsSrv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	tlsSrv.StartTLS()
	defer tlsSrv.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(tlsSrv.URL, "https://"))
	if err != nil {
		t.Fatalf("extrair host/porta de %q: %v", tlsSrv.URL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("porta inválida %q: %v", portStr, err)
	}

	_, body := postJSON(t, srv.URL+"/validate", map[string]interface{}{
		"certPem":   string(certPEM),
		"keyPem":    string(keyPEM),
		"checkHost": host,
		"checkPort": port,
	})

	if success, _ := body["success"].(bool); !success {
		t.Fatalf("esperava success=true, corpo: %+v", body)
	}
	live, ok := body["live_check"].(map[string]interface{})
	if !ok {
		t.Fatalf("esperava campo 'live_check' na resposta, corpo: %+v", body)
	}
	if liveSuccess, _ := live["success"].(bool); !liveSuccess {
		t.Fatalf("live_check.success = false, esperado true: %+v", live)
	}
	if matches, _ := live["matches_target"].(bool); !matches {
		t.Errorf("matches_target = false, esperado true (servidor está servindo exatamente este certificado): %+v", live)
	}
}

func TestValidateCertKeyPairHandler_LiveCheckDetectaCertificadoDiferente(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	// Par A: o que estamos "validando" (o que queremos instalar).
	certPEM, keyPEM := genTestRSACertPEM(t)
	// Par B: o que o servidor TLS real está servindo AGORA — diferente do par A, simulando
	// "ainda não fez o reload" (a transferência já rodou, mas o serviço na VM ainda não recarregou).
	servedCertPEM, servedKeyPEM := genTestRSACertPEM(t)
	tlsCert, err := tls.X509KeyPair(servedCertPEM, servedKeyPEM)
	if err != nil {
		t.Fatalf("montar tls.Certificate servido: %v", err)
	}

	tlsSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tlsSrv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	tlsSrv.StartTLS()
	defer tlsSrv.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(tlsSrv.URL, "https://"))
	if err != nil {
		t.Fatalf("extrair host/porta de %q: %v", tlsSrv.URL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("porta inválida %q: %v", portStr, err)
	}

	_, body := postJSON(t, srv.URL+"/validate", map[string]interface{}{
		"certPem":   string(certPEM),
		"keyPem":    string(keyPEM),
		"checkHost": host,
		"checkPort": port,
	})

	live, ok := body["live_check"].(map[string]interface{})
	if !ok {
		t.Fatalf("esperava campo 'live_check' na resposta, corpo: %+v", body)
	}
	if matches, _ := live["matches_target"].(bool); matches {
		t.Error("matches_target = true, esperado false — o servidor está servindo um certificado DIFERENTE do validado (reload ainda não aconteceu)")
	}
}

func TestTransferCertificateViaSSMHandler_ParIncompativelRejeitadoAntesDoComandoRemoto(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, _ := genTestRSACertPEM(t)
	_, outraChavePEM := genTestRSACertPEM(t)

	resp, body := postJSON(t, srv.URL+"/i-12345/transfer-ssm", map[string]interface{}{
		"profile":        "perfil-qualquer",
		"region":         "us-east-1",
		"certPem":        string(certPEM),
		"keyPem":         string(outraChavePEM),
		"remoteCertPath": "/etc/ssl/tls.crt",
		"remoteKeyPath":  "/etc/ssl/tls.key",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (erro reportado no corpo)", resp.StatusCode)
	}
	if success, _ := body["success"].(bool); success {
		t.Fatalf("esperava success=false para par incompatível, corpo: %+v", body)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_CERT_PAIR" {
		t.Fatalf("esperava error.code=INVALID_CERT_PAIR (rejeitado antes de qualquer comando SSM), corpo: %+v", body)
	}
}

func TestTransferCertificateViaSSMHandler_CamposObrigatoriosAusentesRejeitados(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	resp, body := postJSON(t, srv.URL+"/i-12345/transfer-ssm", map[string]interface{}{
		"profile": "perfil-qualquer",
		// region/certPem/keyPem/remoteCertPath/remoteKeyPath ausentes de propósito
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", resp.StatusCode)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_REQUEST" {
		t.Fatalf("esperava error.code=INVALID_REQUEST, corpo: %+v", body)
	}
}

func TestReadRemoteCertificateViaSSMHandler_ParametrosObrigatoriosAusentesRejeitados(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/i-12345/read-ssm")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_REQUEST" {
		t.Fatalf("esperava error.code=INVALID_REQUEST, corpo: %+v", body)
	}
}

// installFakeSSMCommandAWSCLI — versão mínima do fake usado em
// internal/cloudprovider/aws/ssm_command_test.go (não reaproveitável direto, pacote diferente):
// simula `aws ssm send-command`/`get-command-invocation` resolvendo de primeira (sem simular a
// janela de inconsistência eventual, já coberta a fundo naquele outro pacote) — aqui o objetivo é
// só confirmar que a cadeia HTTP→handler→awsprovider.RunShellCommand→subprocesso funciona de
// ponta a ponta, não re-testar o polling em si.
//
// A resposta do get-command-invocation é montada via encoding/json.Marshal (Go) e gravada num
// arquivo fixture que o script só faz `cat` — evita por completo qualquer fragilidade de
// escaping de shell (printf interpreta \n do próprio jeito dele, então montar JSON com conteúdo
// multi-linha — como um certificado PEM real — via printf/sprintf de shell é uma fonte clássica de
// bug de teste; ver o 1º bug real encontrado ao escrever este teste, corrigido nesta mesma rodada).
func installFakeSSMCommandAWSCLI(t *testing.T, status, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()

	invocation := map[string]string{
		"Status":                status,
		"StandardOutputContent": stdout,
		"StandardErrorContent":  stderr,
	}
	invocationJSON, err := json.Marshal(invocation)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	fixturePath := filepath.Join(dir, "invocation.json")
	if err := os.WriteFile(fixturePath, invocationJSON, 0o644); err != nil {
		t.Fatalf("escrever fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
if [ "$2" = "send-command" ]; then
  echo '{"Command":{"CommandId":"cmd-fake-1"}}'
  exit 0
fi
if [ "$2" = "get-command-invocation" ]; then
  cat %q
  exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`, fixturePath)
	scriptPath := filepath.Join(dir, "aws")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("escrever fake aws: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
}

func TestTransferCertificateViaSSMHandler_CaminhoFelizPontaAPonta(t *testing.T) {
	installFakeSSMCommandAWSCLI(t, "Success", "TRANSFER_OK", "")

	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, keyPEM := genTestRSACertPEM(t)
	resp, body := postJSON(t, srv.URL+"/i-12345/transfer-ssm", map[string]interface{}{
		"profile":        "perfil-qualquer",
		"region":         "us-east-1",
		"certPem":        string(certPEM),
		"keyPem":         string(keyPEM),
		"remoteCertPath": "/etc/ssl/tls.crt",
		"remoteKeyPath":  "/etc/ssl/tls.key",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", resp.StatusCode)
	}
	if success, _ := body["success"].(bool); !success {
		t.Fatalf("esperava success=true, corpo: %+v", body)
	}
}

func TestTransferCertificateViaSSMHandler_ComandoRemotoFalhou(t *testing.T) {
	installFakeSSMCommandAWSCLI(t, "Failed", "", "cat: No such file or directory")

	srv := newVMCertTestServer(t)
	defer srv.Close()

	certPEM, keyPEM := genTestRSACertPEM(t)
	_, body := postJSON(t, srv.URL+"/i-12345/transfer-ssm", map[string]interface{}{
		"profile":        "perfil-qualquer",
		"region":         "us-east-1",
		"certPem":        string(certPEM),
		"keyPem":         string(keyPEM),
		"remoteCertPath": "/etc/ssl/tls.crt",
		"remoteKeyPath":  "/etc/ssl/tls.key",
	})

	if success, _ := body["success"].(bool); success {
		t.Fatalf("esperava success=false quando o comando remoto termina com Status=Failed, corpo: %+v", body)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "SSM_COMMAND_FAILED" {
		t.Fatalf("esperava error.code=SSM_COMMAND_FAILED, corpo: %+v", body)
	}
}

func TestReadRemoteCertificateViaSSMHandler_CaminhoFelizPontaAPonta(t *testing.T) {
	certPEM, _ := genTestRSACertPEM(t)
	installFakeSSMCommandAWSCLI(t, "Success", string(certPEM), "")

	srv := newVMCertTestServer(t)
	defer srv.Close()

	params := url.Values{"profile": {"perfil-qualquer"}, "region": {"us-east-1"}, "path": {"/etc/ssl/tls.crt"}}
	resp, err := http.Get(srv.URL + "/i-12345/read-ssm?" + params.Encode())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if success, _ := body["success"].(bool); !success {
		t.Fatalf("esperava success=true, corpo: %+v", body)
	}
	cert, ok := body["certificate"].(map[string]interface{})
	if !ok {
		t.Fatalf("esperava campo 'certificate' na resposta, corpo: %+v", body)
	}
	if subject, _ := cert["subject"].(string); subject != "certificates-vm-test" {
		t.Errorf("subject = %q, esperado %q", subject, "certificates-vm-test")
	}
}

func TestReadRemoteCertificateHandler_PathAusenteRejeitadoAntesDoSFTP(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	// h.sftp é nil neste servidor de teste — se o handler tentasse abrir uma sessão SFTP antes
	// de checar 'path', isso panicaria em vez de retornar um JSON de erro limpo. O teste passa
	// se, e só se, a checagem de path abortar ANTES de qualquer uso de h.sftp.
	resp, err := http.Get(srv.URL + "/i-12345/read")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "MISSING_PATH" {
		t.Fatalf("esperava error.code=MISSING_PATH, corpo: %+v", body)
	}
}

func TestTransferCertificateHandler_CamposObrigatoriosAusentesRejeitados(t *testing.T) {
	srv := newVMCertTestServer(t)
	defer srv.Close()

	resp, body := postJSON(t, srv.URL+"/i-12345/transfer", map[string]interface{}{
		"credentialProfileId": "perfil-qualquer",
		// certPem/keyPem/remoteCertPath/remoteKeyPath ausentes de propósito
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", resp.StatusCode)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok || errObj["code"] != "INVALID_REQUEST" {
		t.Fatalf("esperava error.code=INVALID_REQUEST, corpo: %+v", body)
	}
}

// TestParseFindPrintfOutput_SaidaReal — fixture capturada ao vivo rodando
// `find . -mindepth 1 -maxdepth 1 -printf '%y\t%s\t%f\n' | sort -k3` de verdade (GNU findutils,
// WSL2/Linux) contra um diretório real com 1 subpasta + 2 arquivos, antes de escrever o parser —
// mesma disciplina já usada no resto desta ferramenta (validar contra saída real, não inventada).
func TestParseFindPrintfOutput_SaidaReal(t *testing.T) {
	output := "d\t4096\tsub\nf\t6\ttls.crt\nf\t6\ttls.key\n"
	entries := parseFindPrintfOutput(output, "/etc/nginx/ssl")

	if len(entries) != 3 {
		t.Fatalf("esperava 3 entradas, veio %d: %+v", len(entries), entries)
	}
	want := map[string]struct {
		isDir bool
		size  int64
		path  string
	}{
		"sub":     {true, 4096, "/etc/nginx/ssl/sub"},
		"tls.crt": {false, 6, "/etc/nginx/ssl/tls.crt"},
		"tls.key": {false, 6, "/etc/nginx/ssl/tls.key"},
	}
	for _, e := range entries {
		w, ok := want[e.Name]
		if !ok {
			t.Fatalf("entrada inesperada: %+v", e)
		}
		if e.IsDir != w.isDir || e.Size != w.size || e.Path != w.path {
			t.Errorf("entrada %q = %+v, esperava is_dir=%v size=%d path=%q", e.Name, e, w.isDir, w.size, w.path)
		}
	}
}

func TestParseFindPrintfOutput_RaizNuncaDuplicaBarra(t *testing.T) {
	entries := parseFindPrintfOutput("f\t123\ttls.crt\n", "/")
	if len(entries) != 1 || entries[0].Path != "/tls.crt" {
		t.Fatalf("esperava path=/tls.crt, veio %+v", entries)
	}
}

func TestParseFindPrintfOutput_LinhasMalformadasIgnoradasSemDerrubarOParse(t *testing.T) {
	// Linha vazia, linha sem os 3 campos esperados, e uma linha válida no meio — o parser nunca
	// deveria abortar por completo só porque uma linha ruidosa apareceu (comando remoto genuíno
	// pode emitir avisos em stderr/stdout dependendo do shell, mesmo com 2>/dev/null no comando).
	output := "\nlixo-sem-tab\nf\t42\treal.txt\n"
	entries := parseFindPrintfOutput(output, "/tmp")
	if len(entries) != 1 || entries[0].Name != "real.txt" || entries[0].Size != 42 {
		t.Fatalf("esperava só a entrada válida, veio %+v", entries)
	}
}
