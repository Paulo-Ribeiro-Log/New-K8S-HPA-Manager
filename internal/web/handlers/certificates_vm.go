package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"

	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"

	"k8s-hpa-manager/internal/certificates"
	"k8s-hpa-manager/internal/history"
)

// certificates_vm.go — sub-aba "Certificados em VM" (Fase 6 do plano em
// /home/paulo/.claude/plans/scalable-greeting-kazoo.md), dentro de Certificados TLS. Reaproveita
// internal/certificates/* sem alteração de comportamento existente (só um helper novo,
// ValidateCertKeyPair, ver vm_cert_pair.go) e a sessão SFTP já resolvida pelo VMSFTPHandler
// (Fase 4, via OpenFileSession) — nenhum mecanismo de conexão/credencial novo aqui. "Reload" em si
// é sempre manual: o usuário abre o terminal SSH/SSM já existente na aba VMs/EC2 e roda o comando
// de reload da aplicação — esta ferramenta não tenta adivinhar/automatizar isso.
//
// Modo alternativo SEM SSH (ReadRemoteCertificateViaSSM/TransferCertificateViaSSM) — motivado por
// um caso real: instância gerida só via SSM, sem sshd instalado. Nesse cenário nem o túnel SSM
// resolve (só encaminha TCP, ainda precisa de sshd do outro lado) — o único canal que sobra é
// executar um comando remoto de verdade via AWS SSM Run Command (awsprovider.RunShellCommand,
// documento AWS-RunShellScript), que só depende do agente SSM já exigido pelo terminal. Certificado
// PEM é texto puro, mas o conteúdo (chave privada em particular) nunca é interpolado cru num
// comando de shell — sempre base64 + ShellQuote, evitando qualquer ambiguidade de escaping.
type VMCertificatesHandler struct {
	sftp           *VMSFTPHandler
	historyTracker *history.HistoryTracker
}

func NewVMCertificatesHandler(sftp *VMSFTPHandler, ht *history.HistoryTracker) *VMCertificatesHandler {
	return &VMCertificatesHandler{sftp: sftp, historyTracker: ht}
}

// vmCertLiveCheck é a projeção JSON (snake_case, mesma convenção de storage.CertEndpointCheck —
// vizinho mais próximo desta feature) de certificates.EndpointCheckResult, que não tem json tags
// próprios.
type vmCertLiveCheck struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`

	Subject      string `json:"subject,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Status       string `json:"status,omitempty"`

	// MatchesTarget é true quando o certificado servido AGORA (via dial TLS real) tem o mesmo
	// número de série do certificado validado nesta chamada — sinal direto de "o reload já
	// surtiu efeito" quando checado depois de reiniciar o serviço na VM.
	MatchesTarget bool `json:"matches_target"`
}

func liveCheckFromEndpointResult(r certificates.EndpointCheckResult, targetSerial string) vmCertLiveCheck {
	return vmCertLiveCheck{
		Success:       r.Success,
		ErrorMessage:  r.ErrorMessage,
		Subject:       r.Subject,
		Issuer:        r.Issuer,
		SerialNumber:  r.SerialNumber,
		Status:        r.Status,
		MatchesTarget: r.Success && targetSerial != "" && r.SerialNumber == targetSerial,
	}
}

type vmCertValidateRequest struct {
	CertPEM string `json:"certPem" binding:"required"`
	KeyPEM  string `json:"keyPem" binding:"required"`

	// Checagem ao vivo OPCIONAL — dial TLS real contra host:porta (tipicamente a própria VM, na
	// porta do serviço que serve o certificado, ex: 443 do nginx) pra comparar o que está sendo
	// SERVIDO agora contra o par cert+chave em mãos. Sem esses 2 campos, a validação fica só
	// local (par compatível), sem nenhuma chamada de rede. Reaproveitada tanto ANTES de
	// transferir (ver o que está no ar hoje) quanto DEPOIS de um reload manual (confirmar que
	// mudou — comparar matches_target).
	CheckHost string `json:"checkHost,omitempty"`
	CheckPort int    `json:"checkPort,omitempty"`
	CheckSNI  string `json:"checkSni,omitempty"`
}

// certKeyPairInfo constrói um *corev1.Secret sintético (nunca tocado por nenhum cluster real) só
// pra reaproveitar certificates.ParseTLSSecret — já calcula Subject/Issuer/SerialNumber/
// Status/DaysRemaining/DNSNames/KeyAlgorithm/etc. de um jeito testado, sem duplicar essa lógica
// aqui.
func certKeyPairInfo(certPEM, keyPEM []byte) (*certificates.CertificateInfo, error) {
	secret := &corev1.Secret{
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	return certificates.ParseTLSSecret(secret, "")
}

// ValidateCertKeyPair — POST /api/v1/vms/certificates/validate (corpo JSON; sem :instanceId — a
// validação em si é puramente local, não precisa de nenhuma VM real). Rejeita um par cert+chave
// incompatível ANTES de qualquer rede — mesmo quando checkHost é informado, a checagem local roda
// primeiro e um par errado aborta sem sequer tentar o dial TLS.
func (h *VMCertificatesHandler) ValidateCertKeyPair(c *gin.Context) {
	var req vmCertValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	if _, err := certificates.ValidateCertKeyPair([]byte(req.CertPEM), []byte(req.KeyPEM)); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": err.Error()},
		})
		return
	}

	info, err := certKeyPairInfo([]byte(req.CertPEM), []byte(req.KeyPEM))
	if err != nil {
		// Não deveria acontecer (ValidateCertKeyPair já confirmou que certPEM parseia) — defesa
		// em profundidade, nunca um panic silencioso.
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": err.Error()},
		})
		return
	}

	resp := gin.H{"success": true, "certificate": info}

	if req.CheckHost != "" && req.CheckPort > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		live := certificates.CheckEndpointTLS(ctx, req.CheckHost, req.CheckPort, req.CheckSNI)
		resp["live_check"] = liveCheckFromEndpointResult(live, info.SerialNumber)
	}

	c.JSON(http.StatusOK, resp)
}

// ReadRemoteCertificate — GET /api/v1/vms/:instanceId/certificates/read?host=&port=&
// tunnelSessionId=&credentialProfileId=&path= — lê e parseia um certificado JÁ instalado na VM
// (só o arquivo do certificado, a chave privada não é necessária pra inspecionar — ParseTLSSecret
// nunca lê tls.key). Resolve a pergunta "como vejo o que já está na VM": sem isso, o usuário só
// tinha caminho pra VALIDAR/TRANSFERIR um certificado novo, nunca pra enxergar o que já existe.
// Mesmo nível de exposição de List/Download (leitura, sem RequireSREGroup) — só lê, nunca grava.
func (h *VMCertificatesHandler) ReadRemoteCertificate(c *gin.Context) {
	remotePath := c.Query("path")
	if remotePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PATH", "message": "path é obrigatório"},
		})
		return
	}

	sess, sErr := h.sftp.OpenFileSession(c)
	if sErr != nil {
		body := gin.H{"success": false, "error": gin.H{"code": sErr.Code, "message": sErr.Message}}
		if sErr.Fingerprint != "" {
			body["error"].(gin.H)["fingerprint"] = sErr.Fingerprint
		}
		c.JSON(sErr.Status, body)
		return
	}
	defer sess.Close()

	data, err := sess.ReadFile(remotePath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_READ_ERROR", "message": err.Error()},
		})
		return
	}

	info, err := certKeyPairInfo(data, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": "arquivo lido, mas não parece um certificado PEM válido: " + err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"certificate": info,
		"raw_pem":     string(data),
	})
}

type vmCertTransferRequest struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TunnelSessionID     string `json:"tunnelSessionId"`
	CredentialProfileID string `json:"credentialProfileId" binding:"required"`

	CertPEM string `json:"certPem" binding:"required"`
	KeyPEM  string `json:"keyPem" binding:"required"`

	// Caminhos remotos EXATOS de destino — decisão deliberada de não adivinhar (nginx, Apache,
	// HAProxy, um serviço custom... cada um espera o par em lugares diferentes); o usuário
	// informa os dois caminhos completos, exatamente como faria via scp manual.
	RemoteCertPath string `json:"remoteCertPath" binding:"required"`
	RemoteKeyPath  string `json:"remoteKeyPath" binding:"required"`
}

// TransferCertificate — POST /api/v1/vms/:instanceId/certificates/transfer (corpo JSON). Revalida
// o par cert+chave no servidor (defesa em profundidade — nunca confia só na validação que o
// frontend já fez) e só então grava os dois arquivos via SFTP (Fase 4, mesma sessão/credencial/
// túnel já usados pelo navegador de arquivos). Nunca reinicia nenhum serviço — o "reload" é
// sempre manual, feito pelo usuário no terminal SSH/SSM já existente na aba VMs/EC2.
func (h *VMCertificatesHandler) TransferCertificate(c *gin.Context) {
	var req vmCertTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	if _, err := certificates.ValidateCertKeyPair([]byte(req.CertPEM), []byte(req.KeyPEM)); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": err.Error()},
		})
		return
	}

	// openSFTPSession/resolveSFTPSession (vm_sftp.go) sempre leem host/port/tunnelSessionId/
	// credentialProfileId da QUERY string — mesmo truque já usado por VMSFTPMkdir/VMSFTPRename
	// pra reaproveitar sem duplicar a resolução de conexão.
	c.Request.URL.RawQuery = buildSFTPTargetQuery(req.Host, req.Port, req.TunnelSessionID, req.CredentialProfileID)

	sess, sErr := h.sftp.OpenFileSession(c)
	if sErr != nil {
		body := gin.H{"success": false, "error": gin.H{"code": sErr.Code, "message": sErr.Message}}
		if sErr.Fingerprint != "" {
			body["error"].(gin.H)["fingerprint"] = sErr.Fingerprint
		}
		c.JSON(sErr.Status, body)
		return
	}
	defer sess.Close()

	certBytes, err := sess.WriteFile(req.RemoteCertPath, []byte(req.CertPEM))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_UPLOAD_ERROR", "message": "erro ao enviar o certificado: " + err.Error()},
		})
		return
	}
	keyBytes, err := sess.WriteFile(req.RemoteKeyPath, []byte(req.KeyPEM))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_UPLOAD_ERROR", "message": "certificado enviado, mas falhou ao enviar a chave privada: " + err.Error()},
		})
		return
	}

	if h.historyTracker != nil {
		entry := CreateHistoryEntry(c, "vm-cert-transfer", c.Param("instanceId"), sftpTargetLabel(c), "success", nil, map[string]interface{}{
			"remote_cert_path": req.RemoteCertPath,
			"remote_key_path":  req.RemoteKeyPath,
		}, 0, "")
		_ = h.historyTracker.Log(entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":            true,
		"cert_bytes_written": certBytes,
		"key_bytes_written":  keyBytes,
	})
}

// ssmCommandTimeout — teto do contexto HTTP pra qualquer chamada via awsprovider.RunShellCommand
// (send-command + polling de get-command-invocation) — folga confortável acima do
// ssmCommandPollTimeout interno (30s) + latência normal da API AWS.
const ssmCommandTimeout = 45 * time.Second

type vmCertSSMTarget struct {
	Profile string `json:"profile" binding:"required"`
	Region  string `json:"region" binding:"required"`
}

// writeCertKeyViaSSM monta e executa, via SSM Run Command, o script que grava certPEM/keyPEM nos
// caminhos remotos informados — base64 + ShellQuote em cada valor interpolado, nunca conteúdo cru
// dentro de uma linha de shell (ver comentário de topo do arquivo). `chmod 600` só na chave, mesmo
// cuidado que qualquer instalação manual de certificado já teria.
func writeCertKeyViaSSM(ctx context.Context, profile, region, instanceID, certPEM, keyPEM, certPath, keyPath string) (*awsprovider.SSMCommandResult, error) {
	certB64 := base64.StdEncoding.EncodeToString([]byte(certPEM))
	keyB64 := base64.StdEncoding.EncodeToString([]byte(keyPEM))

	commands := []string{
		"set -e",
		fmt.Sprintf("mkdir -p %s", awsprovider.ShellQuote(path.Dir(certPath))),
		fmt.Sprintf("echo %s | base64 -d > %s", awsprovider.ShellQuote(certB64), awsprovider.ShellQuote(certPath)),
		fmt.Sprintf("mkdir -p %s", awsprovider.ShellQuote(path.Dir(keyPath))),
		fmt.Sprintf("echo %s | base64 -d > %s", awsprovider.ShellQuote(keyB64), awsprovider.ShellQuote(keyPath)),
		fmt.Sprintf("chmod 600 %s", awsprovider.ShellQuote(keyPath)),
		"echo TRANSFER_OK",
	}

	return awsprovider.RunShellCommand(ctx, profile, region, instanceID, commands)
}

type vmCertTransferSSMRequest struct {
	vmCertSSMTarget
	CertPEM        string `json:"certPem" binding:"required"`
	KeyPEM         string `json:"keyPem" binding:"required"`
	RemoteCertPath string `json:"remoteCertPath" binding:"required"`
	RemoteKeyPath  string `json:"remoteKeyPath" binding:"required"`
}

// TransferCertificateViaSSM — POST /api/v1/vms/:instanceId/certificates/transfer-ssm. Mesma
// revalidação server-side do par (defesa em profundidade) que TransferCertificate já faz — só a
// camada de transporte muda: SSM Run Command em vez de SFTP, sem nenhuma dependência de sshd.
func (h *VMCertificatesHandler) TransferCertificateViaSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	var req vmCertTransferSSMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	if _, err := certificates.ValidateCertKeyPair([]byte(req.CertPEM), []byte(req.KeyPEM)); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": err.Error()},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	result, err := writeCertKeyViaSSM(ctx, req.Profile, req.Region, instanceID, req.CertPEM, req.KeyPEM, req.RemoteCertPath, req.RemoteKeyPath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		msg := result.StandardErrorContent
		if msg == "" {
			msg = fmt.Sprintf("comando terminou com status %s", result.Status)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": msg},
		})
		return
	}

	if h.historyTracker != nil {
		entry := CreateHistoryEntry(c, "vm-cert-transfer-ssm", instanceID, fmt.Sprintf("%s/%s", req.Profile, req.Region), "success", nil, map[string]interface{}{
			"remote_cert_path": req.RemoteCertPath,
			"remote_key_path":  req.RemoteKeyPath,
		}, 0, "")
		_ = h.historyTracker.Log(entry)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ReadRemoteCertificateViaSSM — GET /api/v1/vms/:instanceId/certificates/read-ssm?profile=&region=&path=
// Equivalente sem-SSH de ReadRemoteCertificate: lê o arquivo via `cat` (SSM Run Command) em vez de
// SFTP, e parseia do mesmo jeito (certKeyPairInfo nunca precisa da chave, só do certificado).
func (h *VMCertificatesHandler) ReadRemoteCertificateViaSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	remotePath := c.Query("path")
	if profile == "" || region == "" || remotePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile, region e path são obrigatórios"},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	result, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, []string{
		fmt.Sprintf("cat %s", awsprovider.ShellQuote(remotePath)),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		msg := result.StandardErrorContent
		if msg == "" {
			msg = fmt.Sprintf("comando terminou com status %s", result.Status)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": msg},
		})
		return
	}

	info, err := certKeyPairInfo([]byte(result.StandardOutputContent), nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_CERT_PAIR", "message": "arquivo lido, mas não parece um certificado PEM válido: " + err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"certificate": info,
		"raw_pem":     result.StandardOutputContent,
	})
}

// ListDirectoryViaSSM — GET /api/v1/vms/:instanceId/certificates/browse-ssm?profile=&region=&path=
// Equivalente sem-SSH de VMSFTPList — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário: em
// instâncias geridas só via SSM (sem sshd), "Procurar na VM" ficava SEMPRE desabilitado (sem SFTP
// não há como navegar) — sem nenhuma pista clara na tela do motivo, dando a impressão de que a
// ferramenta inteira "não conecta", mesmo quando o caminho/arquivo já existe de verdade (caso mais
// comum: ATUALIZAR um certificado existente, não instalar um novo). Lista via `find` (GNU
// findutils, presente em qualquer AMI real de EC2 — diferente de BusyBox, que é característica de
// imagem de container, não de VM), formato delimitado por tab (`%y\t%s\t%f`) — sem a ambiguidade de
// formato de data que `ls -la` teria (mesma lição já documentada nesta app pra parsing de listagem
// via exec, ver internal/kubernetes: "Bug real corrigido... ls do BusyBox").
func (h *VMCertificatesHandler) ListDirectoryViaSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	dirPath := c.DefaultQuery("path", "/")
	if dirPath == "" {
		dirPath = "/"
	}
	if profile == "" || region == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile e region são obrigatórios"},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	cmd := fmt.Sprintf(
		"find %s -mindepth 1 -maxdepth 1 -printf '%%y\\t%%s\\t%%f\\n' 2>/dev/null | sort -k3",
		awsprovider.ShellQuote(dirPath),
	)
	result, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, []string{cmd})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		msg := result.StandardErrorContent
		if msg == "" {
			msg = fmt.Sprintf("comando terminou com status %s", result.Status)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": msg},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"path":    dirPath,
		"entries": parseFindPrintfOutput(result.StandardOutputContent, dirPath),
	})
}

// parseFindPrintfOutput parseia a saída de `find -printf '%y\t%s\t%f\n'` — tipo de entrada ('d'
// diretório, 'f' arquivo, 'l' link, etc.), tamanho em bytes, nome do arquivo (sem o diretório-pai).
// Linhas malformadas (menos de 3 campos — nunca deveria acontecer com este formato fixo, mas nunca
// derruba o parse inteiro por causa de uma linha ruidosa) são só ignoradas.
func parseFindPrintfOutput(output, dirPath string) []VMSFTPFileEntry {
	entries := make([]VMSFTPFileEntry, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		typ, sizeStr, name := parts[0], parts[1], parts[2]
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		entries = append(entries, VMSFTPFileEntry{
			Name:  name,
			Path:  path.Join(dirPath, name),
			Size:  size,
			IsDir: typ == "d",
		})
	}
	return entries
}

// serviceRestartTimeout — teto do contexto pro comando de restart via SSH (RunCommand). Mais
// generoso que uma operação de leitura comum — um `systemctl restart` pode legitimamente demorar
// (parar conexões em andamento, recarregar config grande) sem que isso signifique travamento.
const serviceRestartTimeout = 30 * time.Second

// restartServiceCommand/serviceStatusCommand — pedido explícito do usuário: botão "Reiniciar
// serviço" com seleção entre os gerenciadores mais comuns de mercado (nginx/apache2/httpd/haproxy/
// etc., mais um campo livre pro nome exato) em vez de só abrir o terminal e digitar manualmente
// (passo 6, que continua existindo como alternativa 100% manual). `sudo -n` (nunca `sudo` puro) —
// se o usuário SSH não tiver NOPASSWD configurado pra esse comando, falha na hora com uma mensagem
// clara em vez de FICAR PENDURADO esperando uma senha que nunca vai chegar (esta é uma sessão SSH
// não-interativa, sem canal nenhum pra responder um prompt). Como root (já é o caso de toda
// execução via SSM Run Command), `sudo -n` continua funcionando normalmente — nunca pede senha pra
// quem já é root. awsprovider.ShellQuote (mesma função já usada pra escapar o par cert+chave no
// modo SSM) garante que o nome do serviço nunca é interpolado cru numa linha de shell, mesmo que o
// usuário digite algo com espaço/aspas/`;` no campo livre.
func restartServiceCommand(serviceName, initSystem string) string {
	quoted := awsprovider.ShellQuote(serviceName)
	if initSystem == "sysv" {
		return fmt.Sprintf("sudo -n service %s restart", quoted)
	}
	return fmt.Sprintf("sudo -n systemctl restart %s", quoted)
}

func serviceStatusCommand(serviceName, initSystem string) string {
	quoted := awsprovider.ShellQuote(serviceName)
	if initSystem == "sysv" {
		return fmt.Sprintf("sudo -n service %s status", quoted)
	}
	return fmt.Sprintf("sudo -n systemctl is-active %s", quoted)
}

type vmServiceRestartRequest struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TunnelSessionID     string `json:"tunnelSessionId"`
	CredentialProfileID string `json:"credentialProfileId" binding:"required"`
	ServiceName         string `json:"serviceName" binding:"required"`
	// InitSystem — "systemd" (default, omitido) ou "sysv" (`service <nome> restart`, pra imagens
	// mais antigas sem systemd — ainda em uso em parte real do mercado, daí o toggle em vez de
	// assumir systemd incondicionalmente).
	InitSystem string `json:"initSystem"`
}

// RestartService — POST /api/v1/vms/:instanceId/certificates/restart-service. Reaproveita a MESMA
// sessão SSH já resolvida por OpenFileSession (host/porta/túnel/credencial/host-key) pra rodar o
// comando de restart de verdade (RunCommand, vm_sftp.go) — nenhuma conexão nova. Depois de um
// restart bem-sucedido, roda uma 2ª checagem (status do serviço) best-effort — nunca derruba a
// resposta principal se essa checagem falhar, o restart em si já teve sucesso nesse ponto.
func (h *VMCertificatesHandler) RestartService(c *gin.Context) {
	var req vmServiceRestartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "serviceName é obrigatório"},
		})
		return
	}

	// resolveSFTPSession (via OpenFileSession) lê host/port/tunnelSessionId/credentialProfileId da
	// QUERY, não do corpo — mesmo truque já usado por TransferCertificate pra reaproveitar sem
	// duplicar a resolução de conexão.
	c.Request.URL.RawQuery = buildSFTPTargetQuery(req.Host, req.Port, req.TunnelSessionID, req.CredentialProfileID)

	sess, sErr := h.sftp.OpenFileSession(c)
	if sErr != nil {
		body := gin.H{"success": false, "error": gin.H{"code": sErr.Code, "message": sErr.Message}}
		if sErr.Fingerprint != "" {
			body["error"].(gin.H)["fingerprint"] = sErr.Fingerprint
		}
		c.JSON(sErr.Status, body)
		return
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), serviceRestartTimeout)
	defer cancel()

	result, err := sess.RunCommand(ctx, restartServiceCommand(serviceName, req.InitSystem))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSH_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.ExitCode != 0 {
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"error":     gin.H{"code": "SERVICE_RESTART_FAILED", "message": restartFailureMessage(result)},
			"exit_code": result.ExitCode,
		})
		return
	}

	status := ""
	if statusResult, statusErr := sess.RunCommand(ctx, serviceStatusCommand(serviceName, req.InitSystem)); statusErr == nil && statusResult != nil {
		status = strings.TrimSpace(statusResult.Stdout)
	}

	if h.historyTracker != nil {
		entry := CreateHistoryEntry(c, "vm-service-restart", c.Param("instanceId"), sftpTargetLabel(c), "success", nil, map[string]interface{}{
			"service_name": serviceName,
		}, 0, "")
		_ = h.historyTracker.Log(entry)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
}

// restartFailureMessage prioriza stderr (onde `systemctl`/`service` normalmente escrevem o motivo
// real da falha), cai pro stdout quando stderr vier vazio, e só usa um texto genérico com o código
// de saída como último recurso.
func restartFailureMessage(result *RemoteCommandResult) string {
	if msg := strings.TrimSpace(result.Stderr); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(result.Stdout); msg != "" {
		return msg
	}
	return fmt.Sprintf("comando terminou com código %d", result.ExitCode)
}

type vmServiceRestartSSMRequest struct {
	vmCertSSMTarget
	ServiceName string `json:"serviceName" binding:"required"`
	InitSystem  string `json:"initSystem"`
}

// RestartServiceViaSSM — POST /api/v1/vms/:instanceId/certificates/restart-service-ssm. Mesmo
// comando de restart de RestartService, só a camada de transporte muda: SSM Run Command em vez de
// SSH, sem nenhuma dependência de sshd (mesmo modelo de TransferCertificateViaSSM). Duas chamadas
// SSM separadas (restart, depois status) — mais simples/robusto que tentar embutir os dois passos
// num script só com `set -e` (que abortaria a checagem de status se o restart falhasse, escondendo
// o que aconteceu de fato).
func (h *VMCertificatesHandler) RestartServiceViaSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	var req vmServiceRestartSSMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "serviceName é obrigatório"},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	restartResult, err := awsprovider.RunShellCommand(ctx, req.Profile, req.Region, instanceID, []string{
		restartServiceCommand(serviceName, req.InitSystem),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if restartResult.Status != "Success" {
		msg := restartResult.StandardErrorContent
		if msg == "" {
			msg = fmt.Sprintf("comando terminou com status %s", restartResult.Status)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SERVICE_RESTART_FAILED", "message": msg},
		})
		return
	}

	status := ""
	statusCtx, statusCancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer statusCancel()
	if statusResult, statusErr := awsprovider.RunShellCommand(statusCtx, req.Profile, req.Region, instanceID, []string{
		serviceStatusCommand(serviceName, req.InitSystem),
	}); statusErr == nil && statusResult.Status == "Success" {
		status = strings.TrimSpace(statusResult.StandardOutputContent)
	}

	if h.historyTracker != nil {
		entry := CreateHistoryEntry(c, "vm-service-restart-ssm", instanceID, fmt.Sprintf("profile=%s region=%s", req.Profile, req.Region), "success", nil, map[string]interface{}{
			"service_name": serviceName,
		}, 0, "")
		_ = h.historyTracker.Log(entry)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
}
