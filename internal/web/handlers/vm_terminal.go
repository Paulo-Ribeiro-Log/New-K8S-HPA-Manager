package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/vmssh"
)

// vmTerminalUpgrader — mesmo padrão de codeEditorUpgrader (code_editor_terminal.go), CheckOrigin
// reaproveitando allowedOrigins (definida em podexec.go, mesmo pacote).
var vmTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host := r.Host
		return allowedOrigins[origin] ||
			strings.HasPrefix(origin, "http://"+host) ||
			strings.HasPrefix(origin, "https://"+host)
	},
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
}

// vmHostKeyConfirmTimeout — quanto tempo esperar o usuário confirmar uma host key desconhecida
// (TOFU) antes de desistir. Generoso (2min) porque envolve leitura/decisão humana, não uma
// operação automática.
const vmHostKeyConfirmTimeout = 2 * time.Minute

// VMTerminalHandler abre terminais SSH interativos contra VMs/instâncias — reaproveita o mesmo
// protocolo JSON WebSocket de code_editor_terminal.go (terminalMessage, já definida naquele
// arquivo, mesmo pacote), só trocando o transporte: uma sessão SSH remota
// (internal/vmssh.TerminalSession) no lugar de um PTY local (github.com/creack/pty).
type VMTerminalHandler struct {
	credStore      *storage.VMCredentialStore
	historyTracker *history.HistoryTracker
}

func NewVMTerminalHandler(cs *storage.VMCredentialStore, ht *history.HistoryTracker) *VMTerminalHandler {
	return &VMTerminalHandler{credStore: cs, historyTracker: ht}
}

// HandleSSHTerminal — GET /api/v1/vms/:instanceId/terminal/ssh?host=X&port=22&credentialProfileId=Y
func (h *VMTerminalHandler) HandleSSHTerminal(c *gin.Context) {
	if h.credStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "CREDENTIAL_STORE_UNAVAILABLE", "message": "Store de credenciais SSH indisponível neste servidor"},
		})
		return
	}

	instanceID := c.Param("instanceId")
	host := c.Query("host")
	profileID := c.Query("credentialProfileId")
	port := 22
	if p := c.Query("port"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			port = parsed
		}
	}

	if host == "" || profileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "host e credentialProfileId são obrigatórios"},
		})
		return
	}

	userInfo := GetUserInfoForHistory(c)
	cred, err := h.credStore.GetDecrypted(userInfo.Email, profileID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "CREDENTIAL_NOT_FOUND", "message": err.Error()},
		})
		return
	}

	ws, err := vmTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("[VMTerminal] upgrade error")
		return
	}
	defer ws.Close()

	addr := fmt.Sprintf("%s:%d", host, port)

	// Leitura do WebSocket centralizada numa única goroutine — tanto a confirmação de host key
	// (TOFU, antes da sessão existir) quanto o loop principal de input/resize (depois) consomem
	// do MESMO canal, evitando duas goroutines competindo por ws.ReadMessage() ao mesmo tempo.
	msgCh := make(chan terminalMessage, 32)
	go func() {
		defer close(msgCh)
		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var msg terminalMessage
			if json.Unmarshal(raw, &msg) == nil {
				msgCh <- msg
			}
		}
	}()

	confirmFunc := func(fingerprint string) (bool, error) {
		if err := ws.WriteJSON(terminalMessage{Type: "hostkey_confirm", Data: fingerprint}); err != nil {
			return false, err
		}
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return false, fmt.Errorf("conexão encerrada durante a confirmação da host key")
				}
				if msg.Type == "hostkey_response" {
					return msg.Data == "accept", nil
				}
				// Ignora qualquer outra mensagem (ex: input/resize precoce) chegando durante a
				// espera — o terminal ainda não existe pro usuário digitar nada de útil.
			case <-time.After(vmHostKeyConfirmTimeout):
				return false, fmt.Errorf("timeout aguardando confirmação da host key")
			}
		}
	}

	dialCtx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	client, err := vmssh.Dial(dialCtx, addr, addr, vmssh.AuthConfig{
		Username:      cred.Username,
		PrivateKeyPEM: cred.PrivateKeyPEM,
		Passphrase:    cred.Passphrase,
		Password:      cred.Password,
	}, confirmFunc)
	cancel()

	if err != nil {
		h.logConnection(c, instanceID, addr, "error", err.Error())
		_ = ws.WriteJSON(terminalMessage{Type: "error", Data: err.Error()})
		return
	}
	defer client.Close()

	sess, err := vmssh.OpenTerminal(client, 80, 24)
	if err != nil {
		h.logConnection(c, instanceID, addr, "error", err.Error())
		_ = ws.WriteJSON(terminalMessage{Type: "error", Data: err.Error()})
		return
	}
	defer sess.Close()

	h.logConnection(c, instanceID, addr, "success", "")

	welcome := fmt.Sprintf("\x1b[1;34m⚡ SSH — %s@%s\x1b[0m\r\n\r\n", cred.Username, addr)
	_ = ws.WriteJSON(terminalMessage{Type: "output", Data: base64.StdEncoding.EncodeToString([]byte(welcome))})

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				if werr := ws.WriteJSON(terminalMessage{Type: "output", Data: encoded}); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	for msg := range msgCh {
		switch msg.Type {
		case "input":
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				data = []byte(msg.Data)
			}
			_, _ = sess.Write(data)
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = sess.Resize(msg.Cols, msg.Rows)
			}
		}
	}

	<-done
}

// HandleSSMTerminal — GET /api/v1/vms/:instanceId/terminal/ssm?profile=X&region=Y (Fase 5 do
// plano). Diferente do SSH (HandleSSHTerminal), não precisa de credencial nenhuma — a sessão é
// autenticada pelo profile AWS já usado pra listar/gerenciar a instância (mesmo profile/region que
// o usuário já escolheu na aba VMs/EC2), autorização vem do IAM (SSM), não de chave/senha própria
// da VM. Reaproveita o mesmo mecanismo de PTY local já usado pelo terminal do Code Editor
// (github.com/creack/pty, ver code_editor_terminal.go) — só troca o shell local por
// `aws ssm start-session`, que já fala o protocolo de terminal certo sozinho via
// session-manager-plugin (subprocesso do próprio aws CLI).
func (h *VMTerminalHandler) HandleSSMTerminal(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")

	if instanceID == "" || profile == "" || region == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile e region são obrigatórios"},
		})
		return
	}

	if _, err := exec.LookPath("aws"); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "AWS_CLI_NOT_FOUND", "message": "aws CLI não encontrado no PATH do servidor"},
		})
		return
	}
	if _, err := exec.LookPath("session-manager-plugin"); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error": gin.H{
				"code": "SSM_PLUGIN_NOT_FOUND",
				"message": "session-manager-plugin não encontrado no PATH do servidor — instale em " +
					"https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html",
			},
		})
		return
	}

	ws, err := vmTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("[VMTerminal] upgrade error (ssm)")
		return
	}
	defer ws.Close()

	addr := fmt.Sprintf("ssm:%s@%s/%s", instanceID, profile, region)

	cmd := exec.Command("aws", "ssm", "start-session", "--target", instanceID, "--region", region, "--profile", profile)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		h.logConnection(c, instanceID, addr, "error", err.Error())
		_ = ws.WriteJSON(terminalMessage{Type: "error", Data: err.Error()})
		return
	}
	defer func() {
		ptmx.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	h.logConnection(c, instanceID, addr, "success", "")

	welcome := fmt.Sprintf("\x1b[1;34m⚡ SSM Session Manager — %s (profile %s, região %s)\x1b[0m\r\n\r\n", instanceID, profile, region)
	_ = ws.WriteJSON(terminalMessage{Type: "output", Data: base64.StdEncoding.EncodeToString([]byte(welcome))})

	// PTY → WebSocket (goroutine separada) — se `aws ssm start-session` falhar (instância não
	// gerenciada por SSM, sessão AWS expirada, etc.), o próprio texto de erro do CLI chega aqui
	// como output normal antes do processo sair — nunca precisa de tratamento especial, já
	// aparece legível no terminal do usuário.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				if werr := ws.WriteJSON(terminalMessage{Type: "output", Data: encoded}); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY (loop principal) — sem hostkey_confirm nem canal intermediário aqui: SSM não
	// tem TOFU de host key (a autorização é via IAM), então o loop pode ler direto de ws, sem
	// precisar do msgCh usado por HandleSSHTerminal.
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var msg terminalMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "input":
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				data = []byte(msg.Data)
			}
			_, _ = ptmx.Write(data)
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			}
		}
	}

	<-done
}

// logConnection registra tentativas de conexão (SSH ou SSM) no HistoryTracker — nunca inclui a
// credencial em si (nem mesmo o profile ID é sensível, mas o segredo decriptografado jamais passa
// perto deste ponto do código). Ação sempre "vm-ssh-connect" mesmo pra SSM — o `addr` já
// diferencia os dois casos (host:porta pro SSH, "ssm:instance@profile/region" pro SSM), e manter
// uma única ação evita fragmentar o histórico em duas categorias pra "a mesma intenção: abrir um
// terminal nesta VM".
func (h *VMTerminalHandler) logConnection(c *gin.Context, instanceID, addr, status, errMsg string) {
	entry := CreateHistoryEntry(c, "vm-ssh-connect", instanceID, addr, status, nil, nil, 0, errMsg)
	if err := h.historyTracker.Log(entry); err != nil {
		log.Warn().Err(err).Msg("erro ao registrar conexão SSH de VM no history tracker")
	}
}
