package gcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// GCPAuthManager gerencia autenticação GCP rodando `gcloud auth login` como subprocesso.
//
// NÃO usa Device Authorization Grant (RFC 8628) nem uma implementação própria de OAuth2 — as
// duas primeiras versões deste arquivo tentaram isso e nenhuma funcionava de fato:
//
//  1. Device Authorization Grant (StartDeviceAuth/TryExchangeDeviceToken, internal/ai): o
//     client_id público do gcloud SDK está registrado no Google como "Desktop app", e o endpoint
//     https://oauth2.googleapis.com/device/code rejeita esse tipo de client incondicionalmente
//     com `{"error":"invalid_client","error_description":"Invalid client type."}` — confirmado
//     com um POST direto ao endpoint, fora do código Go, pra isolar a causa. Só clients
//     registrados como "TV and Limited Input" suportam esse grant type.
//  2. OAuth2 Authorization Code reimplementado à mão (StartOAuth2AppCallback/ExchangeAuthCode,
//     mesmo mecanismo usado com sucesso pela autenticação Vertex AI): funcionava tecnicamente,
//     mas o usuário apontou (com razão) que é complexidade desnecessária reimplementar o fluxo
//     OAuth2 do zero quando o próprio `gcloud auth login` já faz exatamente isso.
//
// A solução final: rodar `gcloud auth login` como subprocesso (sem DISPLAY/BROWSER no ambiente),
// capturar a URL que ele mesmo imprime no fallback "não consigo abrir um browser local"
// (`https://accounts.google.com/o/oauth2/auth?response_type=code&client_id=32555940559...`,
// com redirect_uri apontando pro listener loopback que o PRÓPRIO gcloud sobe numa porta
// efêmera) e mostrar essa URL pro usuário — o resto (trocar o código por token, validar,
// persistir a credencial) é feito inteiramente pelo gcloud, do jeito que ele já faz quando
// rodado manualmente num terminal. Simples, e usa exatamente o mesmo client_id/fluxo que
// `gcloud auth login` sempre usou.
type GCPAuthManager struct {
	mu              sync.Mutex
	sessions        map[string]*gcloudLoginSession
	activeSessionID string // sessão em andamento, se houver — ver StartGcloudLogin
}

// gcloudLoginSession rastreia um `gcloud auth login` rodando em background.
type gcloudLoginSession struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	status    string // "waiting_url" | "waiting_browser" | "authenticated" | "error"
	authURL   string
	errMsg    string
	expiresAt time.Time
}

// gcloudAuthURLRe casa a URL de autorização impressa por `gcloud auth login` quando ele não
// consegue abrir um browser local — mesmo padrão em todas as versões recentes do SDK.
var gcloudAuthURLRe = regexp.MustCompile(`https://accounts\.google\.com/o/oauth2/auth\S+`)

// GCPAuthStatus descreve o estado atual da autenticação GCP.
type GCPAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Account       string `json:"account,omitempty"`
	HasGcloud     bool   `json:"has_gcloud"`
	HasADC        bool   `json:"has_adc"`
}

// GCPLoginResult retorna os dados para o frontend iniciar o fluxo. Sem UserCode: o fluxo
// Authorization Code não tem etapa de "digite este código" — o usuário só clica no link,
// autentica no Google e é redirecionado de volta automaticamente.
type GCPLoginResult struct {
	SessionID   string    `json:"session_id"`
	VerifyURL   string    `json:"verify_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	IntervalSec int       `json:"interval_sec"`
}

func NewGCPAuthManager() *GCPAuthManager {
	return &GCPAuthManager{sessions: make(map[string]*gcloudLoginSession)}
}

// CheckStatus verifica o estado atual da autenticação GCP.
func (m *GCPAuthManager) CheckStatus(ctx context.Context) GCPAuthStatus {
	status := GCPAuthStatus{}

	if _, err := exec.LookPath("gcloud"); err == nil {
		status.HasGcloud = true

		cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(cmdCtx, "gcloud", "auth", "list",
			"--filter=status:ACTIVE", "--format=json").Output()
		cancel()
		if err == nil {
			var accounts []struct {
				Account string `json:"account"`
			}
			if json.Unmarshal(out, &accounts) == nil && len(accounts) > 0 {
				status.Account = accounts[0].Account
			}
		}
	}

	if _, err := os.Stat(gcpADCPath()); err == nil {
		status.HasADC = true
	}

	// Checagem "ao vivo": tenta de fato obter um access token, pelo mesmo caminho usado pela
	// autenticação real do GKE (GetFreshGKEToken: ADC via refresh_token → gcloud), em vez de só
	// inferir a partir de estado em disco. Um refresh_token revogado/expirado no lado do Google
	// não muda nem o conteúdo do arquivo ADC nem a listagem local do `gcloud auth list` — a
	// checagem anterior (isADCValid: só via se o campo refresh_token estava presente/não-vazio,
	// e `gcloud auth list --filter=status:ACTIVE` também é só estado local) reportava
	// "autenticado" mesmo com a sessão morta no Google, então o dialog de login SSO proativo
	// (GcpAuthDialog/useGcpSsoAuth) nunca disparava ao trocar de cluster GKE. Mesmo princípio já
	// usado pelo SSO AWS (IsTokenValid faz `aws sts get-caller-identity`, uma chamada real, não
	// leitura de estado local).
	status.Authenticated = GetFreshGKEToken(ctx) != ""

	return status
}

// StartGcloudLogin roda `gcloud auth login` em background e retorna assim que a URL de
// autenticação aparece na saída do processo (normalmente 1-2s) — a mesma URL que apareceria
// rodando o comando manualmente num terminal sem browser. O restante do fluxo (trocar o código
// por token, validar, persistir a credencial em ~/.config/gcloud/) é feito inteiramente pelo
// próprio gcloud; esta função só observa o resultado.
func (m *GCPAuthManager) StartGcloudLogin(ctx context.Context) (*GCPLoginResult, error) {
	// Reaproveita uma sessão já em andamento em vez de spawnar outro `gcloud auth login`
	// concorrente — importante porque o gcloud CLI usa um credential store compartilhado
	// (~/.config/gcloud/credentials.db) entre processos, e múltiplos `gcloud auth login`
	// simultâneos (ex: usuário clica "Tentar novamente" mais de uma vez, ou o listener reativo
	// gcp-sso-token-expired dispara de novo enquanto o primeiro login ainda não terminou) podem
	// interferir um no outro e deixar processos órfãos presos esperando um redirect que nunca
	// chega — confirmado na prática durante o desenvolvimento desta função (2 subprocessos
	// `gcloud auth login` ficaram vivos simultaneamente após alguns testes de chamada repetida).
	m.mu.Lock()
	if id := m.activeSessionID; id != "" {
		if s, ok := m.sessions[id]; ok && s.status != "authenticated" && s.status != "error" && time.Now().Before(s.expiresAt) {
			if s.authURL != "" {
				m.mu.Unlock()
				return &GCPLoginResult{SessionID: id, VerifyURL: s.authURL, ExpiresAt: s.expiresAt, IntervalSec: 2}, nil
			}
			// URL ainda não capturada (janela de ~1-2s logo após o início) — deixa a chamada
			// original terminar de resolver em vez de competir por ela.
			m.mu.Unlock()
			return nil, fmt.Errorf("autenticação GCP já em andamento — aguarde alguns segundos e tente de novo")
		}
	}
	m.mu.Unlock()

	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, fmt.Errorf("gcloud CLI não encontrado — instale o Google Cloud SDK")
	}

	// Timeout generoso pro processo inteiro (usuário precisa ter tempo de abrir o link e logar).
	procCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	cmd := exec.CommandContext(procCtx, "gcloud", "auth", "login", "--quiet")
	// Sem DISPLAY/BROWSER, o gcloud detecta que não consegue abrir um browser local e cai
	// automaticamente no fallback: imprime a URL de autorização e sobe seu próprio listener
	// loopback numa porta efêmera, esperando o redirect — sem isso (ex: rodando com o DISPLAY
	// herdado do processo pai), ele tentaria abrir um browser gráfico que não existe no servidor.
	cmd.Env = append(os.Environ(), "DISPLAY=", "BROWSER=")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("falha ao capturar saída do gcloud: %w", err)
	}
	cmd.Stderr = cmd.Stdout // gcloud imprime o prompt de auth no stderr em algumas versões — unificar simplifica o parsing

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("falha ao iniciar gcloud auth login: %w", err)
	}

	sessionID := fmt.Sprintf("gcloud-%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(5 * time.Minute)
	session := &gcloudLoginSession{
		cmd:       cmd,
		cancel:    cancel,
		status:    "waiting_url",
		expiresAt: expiresAt,
	}
	m.mu.Lock()
	m.sessions[sessionID] = session
	m.activeSessionID = sessionID
	m.mu.Unlock()

	urlChan := make(chan string, 1)
	go m.streamGcloudLoginOutput(session, stdout, urlChan)
	go m.awaitGcloudLoginExit(session)

	select {
	case url, ok := <-urlChan:
		if !ok || url == "" {
			m.removeSession(sessionID)
			return nil, fmt.Errorf("gcloud auth login não imprimiu uma URL de autenticação (saída inesperada)")
		}
		return &GCPLoginResult{
			SessionID:   sessionID,
			VerifyURL:   url,
			ExpiresAt:   expiresAt,
			IntervalSec: 2,
		}, nil
	case <-time.After(20 * time.Second):
		m.removeSession(sessionID)
		return nil, fmt.Errorf("timeout aguardando gcloud imprimir a URL de autenticação")
	}
}

// streamGcloudLoginOutput lê a saída (stdout+stderr combinados) do subprocesso linha a linha,
// procurando a URL de autorização. Assim que encontra, publica em urlChan (uma única vez) e
// continua drenando o resto da saída até o processo terminar — necessário pra `cmd.Wait()` (em
// awaitGcloudLoginExit) não bloquear esperando o pipe esvaziar.
func (m *GCPAuthManager) streamGcloudLoginOutput(session *gcloudLoginSession, r io.Reader, urlChan chan<- string) {
	found := false
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if found {
			continue
		}
		if url := gcloudAuthURLRe.FindString(scanner.Text()); url != "" {
			found = true
			m.mu.Lock()
			session.authURL = url
			session.status = "waiting_browser"
			m.mu.Unlock()
			urlChan <- url
		}
	}
	if !found {
		close(urlChan)
	}
}

// awaitGcloudLoginExit espera o subprocesso terminar e, em caso de sucesso, faz uma checagem
// "ao vivo" (gcloud auth print-access-token) antes de marcar a sessão como autenticada — mesmo
// princípio já usado em CheckStatus: o código de saída 0 já é uma validação real feita pelo
// próprio gcloud (só sai 0 se conseguiu trocar o código por token), mas confirmar que dá pra
// obter um access token de verdade cobre o caso raro de a conta logada não ter permissão de
// gerar token de acesso mesmo com o login em si tendo "funcionado".
func (m *GCPAuthManager) awaitGcloudLoginExit(session *gcloudLoginSession) {
	waitErr := session.cmd.Wait()

	m.mu.Lock()
	alreadyFailed := session.status == "error"
	m.mu.Unlock()
	if alreadyFailed {
		return
	}

	if waitErr != nil {
		m.mu.Lock()
		session.status = "error"
		session.errMsg = "login cancelado ou falhou: " + waitErr.Error()
		m.mu.Unlock()
		return
	}

	// Contexto próprio para a checagem final, em vez de reaproveitar `ctx` (o mesmo passado pro
	// subprocesso que acabou de sair) — evita depender de quanto tempo sobrou no timeout de 5min
	// do processo, e de uma possível corrida com PollGcloudLogin cancelando essa mesma sessão por
	// expiração bem no instante em que o processo termina com sucesso.
	tokenCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if tokenFromGcloud(tokenCtx) == "" {
		m.mu.Lock()
		session.status = "error"
		session.errMsg = "gcloud reportou sucesso, mas não foi possível obter um access token — verifique as permissões da conta"
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	session.status = "authenticated"
	m.mu.Unlock()
}

// PollGcloudLogin verifica se a sessão de login completou. Retorna (done, success, errMsg).
func (m *GCPAuthManager) PollGcloudLogin(sessionID string) (done bool, success bool, errMsg string) {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return true, false, "sessão não encontrada ou expirada"
	}

	m.mu.Lock()
	status, msg, expired := session.status, session.errMsg, time.Now().After(session.expiresAt)
	m.mu.Unlock()

	switch {
	case status == "authenticated":
		m.removeSession(sessionID)
		return true, true, ""
	case status == "error":
		m.removeSession(sessionID)
		return true, false, msg
	case expired:
		session.cancel() // mata o subprocesso órfão em vez de deixá-lo pendurado
		m.removeSession(sessionID)
		return true, false, "tempo esgotado aguardando autenticação"
	default:
		return false, false, ""
	}
}

func (m *GCPAuthManager) removeSession(sessionID string) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	if m.activeSessionID == sessionID {
		m.activeSessionID = ""
	}
	m.mu.Unlock()
}

// LoadSavedGCPADC carrega o ADC salvo anteriormente e define GOOGLE_APPLICATION_CREDENTIALS.
// Chamado na inicialização quando há clusters GKE no kubeconfig.
func LoadSavedGCPADC() {
	path := gcpADCPath()
	if isADCValid(path) {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path) //nolint:errcheck
	}
}

// Cache de token GKE para evitar chamada HTTP a cada requisição K8s.
var (
	gkeTokenCache    string
	gkeTokenCacheExp time.Time
	gkeTokenMu       sync.Mutex
	gkeTokenSF       singleflight.Group // coalesce concurrent refreshes
)

// GetFreshGKEToken retorna um access token OAuth2 válido para autenticação GKE.
//
// Ordem de tentativas:
//  1. Cache em memória (TTL 45min — tokens GCP duram 1h)
//  2. Arquivo ADC da app (~/.k8s-hpa-manager/gcp-adc.json) via refresh_token
//  3. `gcloud auth print-access-token` se gcloud estiver disponível
//
// Retorna "" se nenhum método funcionar (o chamador usa a auth do kubeconfig como fallback).
func GetFreshGKEToken(ctx context.Context) string {
	// Fast path: cache hit (sem lock de escrita)
	gkeTokenMu.Lock()
	if gkeTokenCache != "" && time.Now().Before(gkeTokenCacheExp) {
		tok := gkeTokenCache
		gkeTokenMu.Unlock()
		return tok
	}
	gkeTokenMu.Unlock()

	// singleflight: apenas uma goroutine faz o fetch HTTP; as demais esperam e reutilizam o resultado.
	// Elimina o thundering herd quando N requisições chegam simultâneas com cache frio.
	v, _, _ := gkeTokenSF.Do("gke-token", func() (interface{}, error) {
		// Double-check: outra goroutine pode ter populado o cache enquanto esperávamos.
		gkeTokenMu.Lock()
		if gkeTokenCache != "" && time.Now().Before(gkeTokenCacheExp) {
			tok := gkeTokenCache
			gkeTokenMu.Unlock()
			return tok, nil
		}
		gkeTokenMu.Unlock()

		var tok string
		if tok = tokenFromADC(ctx); tok == "" {
			tok = tokenFromGcloud(ctx)
		}
		if tok != "" {
			gkeTokenMu.Lock()
			gkeTokenCache = tok
			gkeTokenCacheExp = time.Now().Add(45 * time.Minute)
			gkeTokenMu.Unlock()
		}
		return tok, nil
	})

	if tok, ok := v.(string); ok {
		return tok
	}
	return ""
}

// InvalidateGKETokenCache limpa o token GKE cacheado, forçando a próxima chamada a
// GetFreshGKEToken buscar um novo em vez de servir o valor em cache até o TTL de 45min expirar
// sozinho. Usado por gkeTokenRoundTripper (internal/config/kubeconfig.go) quando uma requisição
// autenticada com o token cacheado é rejeitada pela API com 401 — sem isso, um token cacheado que
// ficasse inválido antes do TTL natural (ex: revogado, ou obtido já inválido por uma corrida entre
// múltiplas invocações concorrentes do `gcloud` CLI local) deixava toda a autenticação GKE
// (client K8s + Prometheus GMP, que reusa o mesmo cache) quebrada até reiniciar o processo —
// confirmado em uso real.
func InvalidateGKETokenCache() {
	gkeTokenMu.Lock()
	gkeTokenCache = ""
	gkeTokenCacheExp = time.Time{}
	gkeTokenMu.Unlock()
}

// Cache do resultado de `gcloud auth list` — evita pagar ~1-3s de subprocesso gcloud
// a cada cache-miss de ListNodeGroups (ValidateAuth era chamado toda vez).
var (
	gcloudAuthCacheErr error
	gcloudAuthCacheExp time.Time
	gcloudAuthMu       sync.Mutex
)

// IsGcloudAuthActive verifica (com cache de 5min) se há conta GCP ativa via `gcloud auth list`.
// Autenticação raramente muda durante a sessão — não há necessidade de checar a cada request.
func IsGcloudAuthActive(ctx context.Context) error {
	gcloudAuthMu.Lock()
	if time.Now().Before(gcloudAuthCacheExp) {
		err := gcloudAuthCacheErr
		gcloudAuthMu.Unlock()
		return err
	}
	gcloudAuthMu.Unlock()

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "gcloud", "auth", "list",
		"--filter=status:ACTIVE", "--format=json").Output()

	var result error
	if err != nil {
		result = fmt.Errorf("gcloud auth list: %w", err)
	} else {
		var accounts []struct {
			Account string `json:"account"`
		}
		if json.Unmarshal(out, &accounts) != nil || len(accounts) == 0 {
			result = fmt.Errorf("nenhuma conta GCP ativa — execute: gcloud auth login")
		}
	}

	gcloudAuthMu.Lock()
	gcloudAuthCacheErr = result
	gcloudAuthCacheExp = time.Now().Add(5 * time.Minute)
	gcloudAuthMu.Unlock()

	return result
}

// tokenFromADC usa o refresh_token do ADC para obter um novo access_token.
func tokenFromADC(ctx context.Context) string {
	data, err := os.ReadFile(gcpADCPath())
	if err != nil {
		return ""
	}
	var adc struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(data, &adc) != nil || adc.RefreshToken == "" {
		return ""
	}
	return exchangeRefreshToken(ctx, adc.ClientID, adc.ClientSecret, adc.RefreshToken)
}

// exchangeRefreshToken troca um refresh_token por um novo access_token via Google OAuth.
func exchangeRefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) string {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	form := strings.NewReader("client_id=" + clientID +
		"&client_secret=" + clientSecret +
		"&refresh_token=" + refreshToken +
		"&grant_type=refresh_token")

	req, err := http.NewRequestWithContext(reqCtx, "POST",
		"https://oauth2.googleapis.com/token", form)
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return ""
	}
	return result.AccessToken
}

// tokenFromGcloud obtém um access_token via `gcloud auth print-access-token`.
func tokenFromGcloud(ctx context.Context) string {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return ""
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(string(out))
	if len(token) < 10 {
		return ""
	}
	return token
}

// gcpADCPath retorna o caminho do arquivo ADC da aplicação.
func gcpADCPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".k8s-hpa-manager", "gcp-adc.json")
}

// isADCValid verifica se o arquivo ADC contém um refresh_token.
func isADCValid(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var adc struct {
		RefreshToken string `json:"refresh_token"`
	}
	return json.Unmarshal(data, &adc) == nil && adc.RefreshToken != ""
}
