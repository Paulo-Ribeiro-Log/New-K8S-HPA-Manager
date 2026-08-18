package spinnaker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"k8s-hpa-manager/internal/browser"
)

// ErrSessionExpired sinaliza que a sessão (cookie SESSION) não é mais válida — o chamador
// (session.go) trata isso invalidando o cache e tentando login de novo uma vez.
var ErrSessionExpired = fmt.Errorf("sessão Spinnaker expirada ou inválida")

// Client é o cliente HTTP pro Gate API do Spinnaker. Login é via POST /login (matrícula/email +
// senha do Perfil SSO já existente nesta app), sessão por cookie — confirmado ao vivo contra
// as duas instâncias reais (hlg/prd), ver seção 1/3 do plano. Sem CSRF, sem go-rod, sem OAuth2.
type Client struct {
	gateURL string
	http    *http.Client
}

// NewClient cria um cliente pra um Gate já resolvido (ver ResolveGateURL). Não faz login sozinho
// — chamar Login() antes de qualquer operação autenticada.
func NewClient(gateURL string) (*Client, error) {
	gateURL = strings.TrimRight(gateURL, "/")
	if gateURL == "" {
		return nil, fmt.Errorf("gateURL vazio")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("criar cookie jar: %w", err)
	}
	return &Client{
		gateURL: gateURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}, nil
}

// Login autentica no Gate via POST /login (form-urlencoded, sem CSRF — confirmado ao vivo).
// ssoProfileDir é o diretório do Perfil SSO já existente (~/.k8s-hpa-manager, mesmo usado por
// ServiceNow/Teams via internal/browser); loginIdentifier é "email" ou "matricula" — campo
// próprio desta integração (seção 10.1 do plano), independente do BrowserConfig.SSOLoginIdentifier
// usado pelo login via browser do ServiceNow/Teams.
func (c *Client) Login(ctx context.Context, ssoProfileDir, loginIdentifier string) error {
	username, password, ok := browser.LoadSSOCredentials(ssoProfileDir, loginIdentifier)
	if !ok {
		return fmt.Errorf("perfil SSO não configurado ou sem %s/senha", loginIdentifier)
	}

	// GET /login primeiro (mesmo padrão validado ao vivo) — não é estritamente necessário pro
	// POST funcionar, mas espelha o fluxo real observado e garante que eventuais cookies de
	// sessão inicial já estejam presentes antes do POST.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gateURL+"/login", nil)
	if err == nil {
		if resp, err := c.http.Do(getReq); err == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()
		}
	}

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gateURL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("montar POST /login: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST /login: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	// POST /login bem-sucedido sempre responde 200 (mesmo em credencial errada — ele só
	// re-renderiza a tela de login). O jeito confiável de confirmar login real é chamar
	// /auth/user com a sessão obtida e checar se veio um usuário — ver checkAuthenticated.
	return c.checkAuthenticated(ctx)
}

// checkAuthenticated confirma que a sessão atual está autenticada via GET /auth/user
// (retorna corpo vazio quando não-autenticado — confirmado ao vivo, ver seção 1 do plano).
func (c *Client) checkAuthenticated(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gateURL+"/auth/user", nil)
	if err != nil {
		return fmt.Errorf("montar GET /auth/user: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET /auth/user: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("ler /auth/user: %w", err)
	}

	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("login no Spinnaker falhou — credenciais rejeitadas (matrícula/senha do Perfil SSO)")
	}
	return nil
}

// doGet executa um GET autenticado e decodifica o corpo em v. Trata 401/302 como sessão
// expirada (ErrSessionExpired) pra permitir retry com login novo no nível de session.go.
func (c *Client) doGet(ctx context.Context, path string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gateURL+path, nil)
	if err != nil {
		return fmt.Errorf("montar GET %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrSessionExpired
	}
	// O Gate responde 200 com a tela de login (HTML) quando a sessão expira no meio do
	// caminho, em vez de 401 — Content-Type text/html é o sinal confiável disso, já que toda
	// resposta autenticada real desta integração é sempre JSON.
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		return ErrSessionExpired
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("GET %s retornou status %d: %s", path, resp.StatusCode, string(body))
	}

	if v == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decodificar resposta de %s: %w", path, err)
	}
	return nil
}
