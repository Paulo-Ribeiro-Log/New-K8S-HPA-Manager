package servicenow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// cdpTarget representa uma aba/target retornado pelo endpoint /json do Chrome.
type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// cdpCookies é a resposta do CDP para Network.getAllCookies.
type cdpCookies struct {
	Result struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
		} `json:"cookies"`
	} `json:"result"`
}

// ExtractCookiesViaCDP obtém cookies do Chrome via CDP WebSocket em Go puro.
// Conecta na porta informada (normalmente WindowsCDPPort=9223), filtra por domínio.
// Não usa PowerShell, sqlite3 nem DPAPI — Chrome devolve os valores já descriptografados.
func ExtractCookiesViaCDP(port int, domain string) (map[string]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}

	wsURL, err := cdpFindWSURL(client, port)
	if err != nil {
		return nil, err
	}

	// gorilla/websocket: conecta e envia Network.getAllCookies
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("CDP WebSocket: %v", err)
	}
	defer conn.Close()

	msg := `{"id":1,"method":"Network.getAllCookies"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		return nil, fmt.Errorf("CDP send: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("CDP recv: %v", err)
	}

	var resp cdpCookies
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("CDP parse: %v (%.100s)", err, string(raw))
	}

	result := make(map[string]string)
	for _, c := range resp.Result.Cookies {
		if strings.Contains(c.Domain, domain) {
			result[c.Name] = c.Value
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("nenhum cookie para %s — faça login no ServiceNow e tente novamente", domain)
	}
	return result, nil
}

// cdpFindWSURL obtém o WebSocket debugger URL de um page target no Chrome.
// Prefere abas do ServiceNow; fallback para qualquer página; último recurso: browser target.
func cdpFindWSURL(client *http.Client, port int) (string, error) {
	for _, host := range cdpHosts() {
		url := fmt.Sprintf("http://%s:%d/json", host, port)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var targets []cdpTarget
		if json.Unmarshal(body, &targets) != nil {
			continue
		}

		// Prefere aba do ServiceNow (contexto mais confiável para cookies)
		for _, t := range targets {
			if t.Type == "page" && strings.Contains(t.URL, "service-now.com") && t.WebSocketDebuggerURL != "" {
				return rewriteWSHost(t.WebSocketDebuggerURL, host), nil
			}
		}
		// Qualquer page target serve (cookies são compartilhados no perfil)
		for _, t := range targets {
			if t.Type == "page" && t.WebSocketDebuggerURL != "" {
				return rewriteWSHost(t.WebSocketDebuggerURL, host), nil
			}
		}
	}

	// Último recurso: browser target em /json/version
	for _, host := range cdpHosts() {
		url := fmt.Sprintf("http://%s:%d/json/version", host, port)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var ver struct {
			WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		}
		if json.Unmarshal(body, &ver) == nil && ver.WebSocketDebuggerURL != "" {
			return rewriteWSHost(ver.WebSocketDebuggerURL, host), nil
		}
	}

	return "", fmt.Errorf("Chrome CDP não disponível na porta %d — inicie o Chrome com --remote-debugging-port=%d", port, port)
}

// rewriteWSHost substitui o host na URL WebSocket pelo host que respondeu ao HTTP.
// Necessário em WSL2: Chrome reporta ws://127.0.0.1:9223/... mas o acesso é via 172.x.x.x.
func rewriteWSHost(wsURL, host string) string {
	if strings.Contains(wsURL, "127.0.0.1") && host != "127.0.0.1" {
		return strings.Replace(wsURL, "127.0.0.1", host, 1)
	}
	return wsURL
}
