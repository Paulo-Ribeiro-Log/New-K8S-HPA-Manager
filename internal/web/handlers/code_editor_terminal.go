package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var codeEditorUpgrader = websocket.Upgrader{
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

// TerminalMessage — protocolo JSON idêntico ao PodTerminal.tsx
type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data"` // base64 para output, raw para input/resize
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// HandleTerminal — GET /api/v1/code-editor/repos/:id/terminal
// Abre um terminal bash interativo na raiz do repositório via PTY + WebSocket.
func (h *CodeEditorHandler) HandleTerminal(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "repositório não encontrado"})
		return
	}

	ws, err := codeEditorUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[CodeEditorTerminal] upgrade error: %v", err)
		return
	}
	defer ws.Close()

	// Escolher shell: respeita $SHELL do ambiente, senão busca bash → sh
	shell := os.Getenv("SHELL")
	if shell == "" {
		if path, err := exec.LookPath("bash"); err == nil {
			shell = path
		} else {
			shell = "sh"
		}
	}

	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","data":"Falha ao iniciar terminal: `+err.Error()+`"}`))
		return
	}
	defer func() {
		ptmx.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Repo info no welcome message
	repoName := filepath.Base(dir)
	welcome := fmt.Sprintf("\x1b[1;34m⚡ Terminal — %s\x1b[0m\r\n\x1b[2m%s\x1b[0m\r\n\r\n", repoName, dir)
	b64 := base64.StdEncoding.EncodeToString([]byte(welcome))
	_ = ws.WriteJSON(terminalMessage{Type: "output", Data: b64})

	// PTY → WebSocket (goroutine separada)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recover() }() //nolint:errcheck
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				if err2 := ws.WriteJSON(terminalMessage{Type: "output", Data: encoded}); err2 != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY (loop principal)
	ws.SetReadDeadline(time.Time{}) // sem timeout
	for {
		_, msgBytes, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var msg terminalMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				data = []byte(msg.Data)
			}
			ptmx.Write(data)
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			}
		}
	}

	<-done
}
