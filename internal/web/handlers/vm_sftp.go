package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/vmssh"
)

// hostKeyUnknownSentinel — prefixo detectável embutido no erro devolvido por confirmFunc quando a
// host key é desconhecida e o chamador não pediu explicitamente pra aceitar aquela fingerprint
// exata (via query param acceptHostKeyFingerprint). Rotas SFTP são REST puro, sem canal
// interativo (diferente do terminal, que usa WebSocket + confirmação em tempo real) — esse
// sentinel permite ao handler devolver a fingerprint de forma estruturada (SSH_HOSTKEY_UNKNOWN),
// pro frontend mostrar e o usuário decidir; aceitando, a MESMA chamada é refeita com
// acceptHostKeyFingerprint=<fp>, que faz confirmFunc aceitar e gravar em vm_known_hosts — dali em
// diante a conexão já é confiável, sem precisar perguntar de novo.
const hostKeyUnknownSentinel = "HOSTKEY_UNKNOWN|"

// extractUnknownHostKeyFingerprint procura o sentinel na cadeia de erro (pode estar envolto por
// fmt.Errorf("...: %w", ...) várias camadas acima, daí buscar em err.Error() em vez de um
// errors.As tipado) e devolve a fingerprint pura, sem o resto do texto de erro.
func extractUnknownHostKeyFingerprint(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	s := err.Error()
	idx := strings.Index(s, hostKeyUnknownSentinel)
	if idx == -1 {
		return "", false
	}
	rest := s[idx+len(hostKeyUnknownSentinel):]
	// Corta só em espaço/quebra/aspas — NUNCA em ":" (a fingerprint em si tem esse formato,
	// "SHA256:base64...", cortar no primeiro ":" truncaria o valor pela metade).
	if sp := strings.IndexAny(rest, " \n\""); sp != -1 {
		rest = rest[:sp]
	}
	return rest, true
}

// vm_sftp.go — SFTP contra VMs/instâncias reais (Fase 4 do plano em
// /home/paulo/.claude/plans/scalable-greeting-kazoo.md). Espelha internal/web/handlers/pod_sftp.go
// nas rotas/formato de resposta, mas sobre um client SFTP de verdade (internal/vmssh.OpenSFTP),
// não o servidor-em-memória-sobre-kubectl-exec do internal/podsftp — uma VM já tem sshd nativo.
// Deliberadamente sem persistência entre requisições (mesmo princípio de podsftp.Session): cada
// operação abre SSH+SFTP, faz UMA coisa, fecha os dois — nenhuma sessão fica presa em memória.

// vmSFTPDialTimeout — orçamento pro handshake SSH em si; a operação SFTP depois disso não tem
// timeout próprio (arquivo grande pode legitimamente levar mais que isso).
const vmSFTPDialTimeout = 20 * time.Second

type VMSFTPHandler struct {
	credStore      *storage.VMCredentialStore
	historyTracker *history.HistoryTracker
	ssmTunnels     *VMSSMTunnelManager
}

func NewVMSFTPHandler(cs *storage.VMCredentialStore, ht *history.HistoryTracker, ssmTunnels *VMSSMTunnelManager) *VMSFTPHandler {
	return &VMSFTPHandler{credStore: cs, historyTracker: ht, ssmTunnels: ssmTunnels}
}

// vmSFTPSession agrega o client SFTP + o client SSH por baixo dele — Close() dos dois precisa
// acontecer juntos (fechar só o SFTP deixaria a conexão TCP/SSH pendurada).
type vmSFTPSession struct {
	sftp *sftp.Client
	ssh  *ssh.Client
}

func (s *vmSFTPSession) Close() {
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
	if s.ssh != nil {
		_ = s.ssh.Close()
	}
}

// openSFTPSession resolve o destino (host:porta direto OU um túnel SSM já aberto via
// tunnelSessionId — ver vm_ssm_tunnel.go) + credencial da query string, conecta via SSH e abre
// SFTP em cima — escrita da resposta de erro já feita quando retorna ok=false.
//
// Host key desconhecida (TOFU) SEMPRE é rejeitada aqui, nunca delegada a nenhuma confirmação
// interativa — diferente do terminal (vm_terminal.go), esta é uma rota REST comum, sem canal
// nenhum pra perguntar "confia?" a um usuário. O known_hosts (~/.k8s-hpa-manager/vm_known_hosts)
// é COMPARTILHADO entre terminal e SFTP (mesmo arquivo, chaveado por host:porta) — na prática,
// basta o usuário ter aberto um terminal SSH pra essa instância uma vez (aceitando a fingerprint
// lá) para que toda operação de SFTP subsequente contra o mesmo host:porta funcione sem fricção.
//
// No modo túnel SSM, dialAddr (127.0.0.1:<porta local>) muda a cada sessão — a porta é sempre
// escolhida aleatoriamente (ver findFreeLocalPort em vm_ssm_tunnel.go). Se o known_hosts fosse
// chaveado por esse endereço, o TOFU NUNCA bateria duas vezes seguidas contra o mesmo sshd real
// (toda sessão pareceria "host novo", e a rota REST sempre rejeita host key desconhecida por não
// ter canal de confirmação). Por isso vmssh.Dial recebe hostKeyIdentity SEPARADO do endereço de
// discagem real — uma identidade estável por instância (não pelo túnel efêmero), garantindo que o
// TOFU funcione de verdade entre sessões de túnel diferentes contra o mesmo sshd remoto.
// SFTPSessionError é o erro estruturado devolvido por resolveSFTPSession — carrega status HTTP +
// code + message (mesmo shape que openSFTPSession já escrevia direto em c.JSON) e, quando
// aplicável, a fingerprint de uma host key desconhecida (SSH_HOSTKEY_UNKNOWN). Exportado pra
// permitir que OUTROS handlers (ex: certificates_vm.go, Fase 6) que reaproveitam OpenFileSession
// decidam como reportar o erro dentro da própria resposta que estão montando, sem depender de
// c.JSON já ter sido chamado.
type SFTPSessionError struct {
	Status      int
	Code        string
	Message     string
	Fingerprint string
}

func (e *SFTPSessionError) Error() string { return e.Message }

func (e *SFTPSessionError) writeJSON(c *gin.Context) {
	body := gin.H{"error": gin.H{"code": e.Code, "message": e.Message}}
	if e.Fingerprint != "" {
		body["error"].(gin.H)["fingerprint"] = e.Fingerprint
	}
	body["success"] = false
	c.JSON(e.Status, body)
}

func (h *VMSFTPHandler) openSFTPSession(c *gin.Context) (*vmSFTPSession, bool) {
	sess, sErr := h.resolveSFTPSession(c)
	if sErr != nil {
		sErr.writeJSON(c)
		return nil, false
	}
	return sess, true
}

// resolveSFTPSession faz todo o trabalho de openSFTPSession (resolver host/túnel/credencial,
// discar SSH, abrir SFTP em cima) mas SEM escrever nada em c — devolve um *SFTPSessionError
// estruturado em vez disso, pra permitir reuso por chamadores que querem montar sua própria
// resposta JSON (ver OpenFileSession). openSFTPSession continua sendo o caminho usado pelos
// handlers deste arquivo (só um wrapper fino por cima desta função agora).
func (h *VMSFTPHandler) resolveSFTPSession(c *gin.Context) (*vmSFTPSession, *SFTPSessionError) {
	if h.credStore == nil {
		return nil, &SFTPSessionError{
			Status: http.StatusServiceUnavailable, Code: "CREDENTIAL_STORE_UNAVAILABLE",
			Message: "Store de credenciais SSH indisponível neste servidor",
		}
	}

	profileID := c.Query("credentialProfileId")
	if profileID == "" {
		return nil, &SFTPSessionError{
			Status: http.StatusBadRequest, Code: "INVALID_REQUEST",
			Message: "credentialProfileId é obrigatório",
		}
	}

	var dialAddr, hostKeyIdentity, tunnelSessionID string
	if tunnelSessionID = c.Query("tunnelSessionId"); tunnelSessionID != "" {
		if h.ssmTunnels == nil {
			return nil, &SFTPSessionError{
				Status: http.StatusServiceUnavailable, Code: "SSM_TUNNEL_UNAVAILABLE",
				Message: "gerenciador de túnel SSM indisponível neste servidor",
			}
		}
		resolvedAddr, err := h.ssmTunnels.ResolveLocalAddr(tunnelSessionID)
		if err != nil {
			return nil, &SFTPSessionError{Status: http.StatusBadRequest, Code: "SSM_TUNNEL_NOT_FOUND", Message: err.Error()}
		}
		dialAddr = resolvedAddr
		// ":0" só pra manter o formato host:porta que net.SplitHostPort exige (usado internamente
		// por x/crypto/ssh/knownhosts pra decompor a identidade) — nunca discado de verdade, é
		// só a chave de identidade no known_hosts.
		hostKeyIdentity = fmt.Sprintf("ssm-tunnel-%s:0", c.Param("instanceId"))
	} else {
		host := c.Query("host")
		port := 22
		if p := c.Query("port"); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
				port = parsed
			}
		}
		if host == "" {
			return nil, &SFTPSessionError{
				Status: http.StatusBadRequest, Code: "INVALID_REQUEST",
				Message: "host (ou tunnelSessionId) é obrigatório",
			}
		}
		dialAddr = fmt.Sprintf("%s:%d", host, port)
		hostKeyIdentity = dialAddr
	}

	userInfo := GetUserInfoForHistory(c)
	cred, err := h.credStore.GetDecrypted(userInfo.Email, profileID)
	if err != nil {
		return nil, &SFTPSessionError{Status: http.StatusBadRequest, Code: "CREDENTIAL_NOT_FOUND", Message: err.Error()}
	}

	// Rota REST pura, sem canal interativo (diferente do terminal, que confirma via WebSocket em
	// tempo real) — host key desconhecida só é aceita quando o CHAMADOR já sabe a fingerprint
	// exata de antemão (acceptHostKeyFingerprint, populado pelo frontend depois de mostrar a
	// fingerprint pro usuário numa 1ª tentativa que falhou com SSH_HOSTKEY_UNKNOWN) e ela bate com
	// a observada agora. Sem isso, retorna o sentinel (ver hostKeyUnknownSentinel) pro chamador
	// desta função extrair a fingerprint e devolver um erro estruturado.
	confirmFunc := func(fingerprint string) (bool, error) {
		if want := c.Query("acceptHostKeyFingerprint"); want != "" && want == fingerprint {
			return true, nil
		}
		return false, fmt.Errorf("%s%s", hostKeyUnknownSentinel, fingerprint)
	}

	dialCtx, cancel := context.WithTimeout(c.Request.Context(), vmSFTPDialTimeout)
	defer cancel()
	sshClient, err := vmssh.Dial(dialCtx, dialAddr, hostKeyIdentity, vmssh.AuthConfig{
		Username:      cred.Username,
		PrivateKeyPEM: cred.PrivateKeyPEM,
		Passphrase:    cred.Passphrase,
		Password:      cred.Password,
	}, confirmFunc)
	if err != nil {
		if fp, ok := extractUnknownHostKeyFingerprint(err); ok {
			return nil, &SFTPSessionError{
				Status:      http.StatusBadRequest,
				Code:        "SSH_HOSTKEY_UNKNOWN",
				Message:     fmt.Sprintf("host key desconhecida (fingerprint %s) — confirme e tente de novo", fp),
				Fingerprint: fp,
			}
		}
		message := err.Error()
		// No modo túnel SSM, um handshake que cai com EOF logo após conectar tem 2 causas
		// possíveis, indistinguíveis do lado do cliente SSH — o túnel ainda não estava pronto
		// (já mitigado em waitTunnelReady), ou a INSTÂNCIA não tem nada escutando na porta de
		// destino (sshd não roda ali/firewall interno bloqueia). Reaproveita a saída já capturada
		// do `aws ssm` (RecentOutput) pra diferenciar e dar um erro de verdade acionável no 2º
		// caso, em vez do "handshake failed: EOF" cru, que não diz nada sobre a causa real.
		if tunnelSessionID != "" && h.ssmTunnels != nil {
			if strings.Contains(strings.ToLower(h.ssmTunnels.RecentOutput(tunnelSessionID)), "connection to destination port failed") {
				message = "o túnel SSM conectou normalmente, mas a instância recusou a conexão na porta informada — " +
					"nada está escutando ali (sshd não instalado/não rodando nessa porta) ou um firewall interno da " +
					"instância está bloqueando. Confirme a porta real do sshd nesta instância (campo \"Porta remota\") " +
					"ou, se ela não tiver SSH configurado, use apenas o terminal nativo via SSM (sem arquivos)."
			}
		}
		return nil, &SFTPSessionError{Status: http.StatusBadGateway, Code: "SSH_CONNECT_ERROR", Message: message}
	}

	sftpClient, err := vmssh.OpenSFTP(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, &SFTPSessionError{Status: http.StatusBadGateway, Code: "SFTP_SESSION_ERROR", Message: err.Error()}
	}

	return &vmSFTPSession{sftp: sftpClient, ssh: sshClient}, nil
}

// VMFileSession — sessão SFTP reaproveitável por outros handlers (ex: certificates_vm.go, Fase 6)
// sem duplicar a resolução de host/túnel/credencial/host-key já feita em resolveSFTPSession. Só
// expõe o necessário (ler/escrever um arquivo inteiro) — nunca o cliente *sftp.Client cru, pra não
// vazar a API completa do pacote sftp pra fora deste arquivo.
type VMFileSession struct {
	sess *vmSFTPSession
}

// Close fecha SFTP e SSH juntos (mesmo princípio de vmSFTPSession.Close).
func (s *VMFileSession) Close() { s.sess.Close() }

// ReadFile lê o conteúdo inteiro de um arquivo remoto.
func (s *VMFileSession) ReadFile(path string) ([]byte, error) {
	f, err := s.sess.sftp.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteFile grava data inteiro num arquivo remoto (cria ou sobrescreve — mesmo comportamento de
// sftp.Create já usado por VMSFTPUpload).
func (s *VMFileSession) WriteFile(path string, data []byte) (int64, error) {
	f, err := s.sess.sftp.Create(path)
	if err != nil {
		return 0, err
	}
	n, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return int64(n), werr
	}
	if cerr != nil {
		return int64(n), fmt.Errorf("falha ao enviar pro destino: %w", cerr)
	}
	return int64(n), nil
}

// OpenFileSession abre uma sessão SFTP a partir dos MESMOS parâmetros de query que
// List/Download/Upload/Remove já usam (host/port/tunnelSessionId/credentialProfileId,
// acceptHostKeyFingerprint) — usado por certificates_vm.go pra ler/gravar o par cert+chave direto
// numa VM sem reimplementar a resolução de conexão. O erro, quando não-nil, já é um
// *SFTPSessionError (sempre — nunca um erro genérico) pronto pra virar parte da resposta JSON que
// o chamador está montando, incluindo o caso SSH_HOSTKEY_UNKNOWN com a fingerprint estruturada.
func (h *VMSFTPHandler) OpenFileSession(c *gin.Context) (*VMFileSession, *SFTPSessionError) {
	sess, sErr := h.resolveSFTPSession(c)
	if sErr != nil {
		return nil, sErr
	}
	return &VMFileSession{sess: sess}, nil
}

// sftpTargetLabel monta um rótulo legível pro audit log (logAction) — "host:porta" no modo SSH
// direto, ou "ssm-tunnel:<sessionId>" no modo túnel (host/port ficam vazios nesse caso).
func sftpTargetLabel(c *gin.Context) string {
	if tunnelSessionID := c.Query("tunnelSessionId"); tunnelSessionID != "" {
		return "ssm-tunnel:" + tunnelSessionID
	}
	return fmt.Sprintf("%s:%s", c.Query("host"), c.Query("port"))
}

func (h *VMSFTPHandler) logAction(c *gin.Context, action, instanceID, addr, status string, extra map[string]interface{}, errMsg string) {
	if h.historyTracker == nil {
		return
	}
	entry := CreateHistoryEntry(c, action, instanceID, addr, status, nil, extra, 0, errMsg)
	_ = h.historyTracker.Log(entry)
}

// VMSFTPFileEntry é a projeção JSON de uma entrada de diretório — mesmo shape de SFTPFileEntry
// (pod_sftp.go), campo a campo, pra o frontend reaproveitar o mesmo componente de listagem.
type VMSFTPFileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
	Mode    string `json:"mode"`
}

// VMSFTPList — GET /api/v1/vms/:instanceId/sftp/list?host=&port=&credentialProfileId=&path=
func (h *VMSFTPHandler) VMSFTPList(c *gin.Context) {
	dirPath := c.DefaultQuery("path", "/")
	if dirPath == "" {
		dirPath = "/"
	}

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	entries, err := sess.sftp.ReadDir(dirPath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_LIST_ERROR", "message": err.Error()},
		})
		return
	}

	result := make([]VMSFTPFileEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, VMSFTPFileEntry{
			Name:    e.Name(),
			Path:    path.Join(dirPath, e.Name()),
			Size:    e.Size(),
			IsDir:   e.IsDir(),
			ModTime: e.ModTime().Format(time.RFC3339),
			Mode:    e.Mode().String(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "path": dirPath, "entries": result})
}

// VMSFTPDownload — GET /api/v1/vms/:instanceId/sftp/download?host=&port=&credentialProfileId=&path=
func (h *VMSFTPHandler) VMSFTPDownload(c *gin.Context) {
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PATH", "message": "path é obrigatório"},
		})
		return
	}

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	f, err := sess.sftp.Open(filePath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_DOWNLOAD_ERROR", "message": err.Error()},
		})
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	filename := path.Base(filePath)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")
	if info != nil {
		c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	}

	h.logAction(c, "vm-sftp-download", c.Param("instanceId"), sftpTargetLabel(c), "success", map[string]interface{}{"path": filePath}, "")

	if _, err := io.Copy(c.Writer, f); err != nil {
		// Resposta já começou a ser escrita — não dá mais pra mandar um JSON de erro limpo, só
		// logar. Mesmo padrão de streaming já usado em SFTPDownload (pod_sftp.go).
		fmt.Printf("[VMSFTPDownload] erro durante o streaming: %v\n", err)
	}
}

// VMSFTPUpload — POST /api/v1/vms/:instanceId/sftp/upload?host=&port=&credentialProfileId=&path=
// (multipart, campo "file"). `path` é o caminho remoto completo de destino.
func (h *VMSFTPHandler) VMSFTPUpload(c *gin.Context) {
	remotePath := c.Query("path")
	if remotePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PATH", "message": "path é obrigatório"},
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_FILE", "message": "campo 'file' (multipart) é obrigatório: " + err.Error()},
		})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_OPEN_ERROR", "message": err.Error()},
		})
		return
	}
	defer src.Close()

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	dst, err := sess.sftp.Create(remotePath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_UPLOAD_ERROR", "message": err.Error()},
		})
		return
	}

	written, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_UPLOAD_ERROR", "message": copyErr.Error()},
		})
		return
	}
	if closeErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_UPLOAD_ERROR", "message": "falha ao enviar pro destino: " + closeErr.Error()},
		})
		return
	}

	h.logAction(c, "vm-sftp-upload", c.Param("instanceId"), sftpTargetLabel(c), "success", map[string]interface{}{"path": remotePath, "bytes": written}, "")

	c.JSON(http.StatusOK, gin.H{"success": true, "path": remotePath, "bytes_written": written})
}

type vmSFTPMkdirRequest struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TunnelSessionID     string `json:"tunnelSessionId"`
	CredentialProfileID string `json:"credentialProfileId" binding:"required"`
	Path                string `json:"path" binding:"required"`
}

// VMSFTPMkdir — POST /api/v1/vms/:instanceId/sftp/mkdir (corpo JSON, ver vmSFTPMkdirRequest —
// diferente de List/Download/Upload, que usam query string; escolhido corpo JSON aqui só porque
// já é uma requisição POST sem multipart, mesmo padrão de SFTPMkdir em pod_sftp.go).
func (h *VMSFTPHandler) VMSFTPMkdir(c *gin.Context) {
	var req vmSFTPMkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	c.Request.URL.RawQuery = buildSFTPTargetQuery(req.Host, req.Port, req.TunnelSessionID, req.CredentialProfileID)

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	if err := sess.sftp.MkdirAll(req.Path); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_MKDIR_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "path": req.Path})
}

type vmSFTPRenameRequest struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TunnelSessionID     string `json:"tunnelSessionId"`
	CredentialProfileID string `json:"credentialProfileId" binding:"required"`
	OldPath             string `json:"old_path" binding:"required"`
	NewPath             string `json:"new_path" binding:"required"`
}

// VMSFTPRename — POST /api/v1/vms/:instanceId/sftp/rename (corpo JSON)
func (h *VMSFTPHandler) VMSFTPRename(c *gin.Context) {
	var req vmSFTPRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	c.Request.URL.RawQuery = buildSFTPTargetQuery(req.Host, req.Port, req.TunnelSessionID, req.CredentialProfileID)

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	if err := sess.sftp.Rename(req.OldPath, req.NewPath); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_RENAME_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// VMSFTPRemove — DELETE /api/v1/vms/:instanceId/sftp/remove?host=&port=&credentialProfileId=&path=&is_dir=
func (h *VMSFTPHandler) VMSFTPRemove(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PATH", "message": "path é obrigatório"},
		})
		return
	}
	isDir := c.Query("is_dir") == "true"

	sess, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer sess.Close()

	var err error
	if isDir {
		err = sess.sftp.RemoveDirectory(targetPath)
	} else {
		err = sess.sftp.Remove(targetPath)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SFTP_REMOVE_ERROR", "message": err.Error()},
		})
		return
	}

	h.logAction(c, "vm-sftp-remove", c.Param("instanceId"), sftpTargetLabel(c), "success", map[string]interface{}{"path": targetPath, "is_dir": isDir}, "")

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// buildSFTPTargetQuery — Mkdir/Rename recebem host/port/tunnelSessionId/credentialProfileId no
// CORPO JSON (não na query string, diferente de List/Download/Upload/Remove), mas
// openSFTPSession só sabe ler da query string (reaproveitada por todos os 6 handlers). Reescreve
// a query da própria requisição antes de chamar openSFTPSession — truque local, nunca observado
// fora desses dois handlers.
func buildSFTPTargetQuery(host string, port int, tunnelSessionID, profileID string) string {
	v := url.Values{"credentialProfileId": {profileID}}
	if tunnelSessionID != "" {
		v.Set("tunnelSessionId", tunnelSessionID)
	} else {
		v.Set("host", host)
		if port > 0 {
			v.Set("port", strconv.Itoa(port))
		}
	}
	return v.Encode()
}
