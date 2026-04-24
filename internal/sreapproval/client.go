package sreapproval

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ErrAlreadyFinalized indica que a solicitação já foi finalizada, com o email do aprovador.
type ErrAlreadyFinalized struct {
	ApproverEmail string
	ApproverSquad string
}

func (e *ErrAlreadyFinalized) Error() string {
	return "esta solicitação de aprovação já foi finalizada"
}

// Client é o cliente para o sistema SRE Approval
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *zerolog.Logger
}

// NewClient cria um novo cliente SRE Approval
func NewClient(logger *zerolog.Logger) *Client {
	return &Client{
		baseURL: "https://devstartcd.via.com.br",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// GetApprovalInfo obtém informações de uma solicitação de aprovação via scraping HTML.
func (c *Client) GetApprovalInfo(ctx context.Context, approvalURL string) (*ApprovalInfo, error) {
	_, err := ExtractApprovalID(approvalURL)
	if err != nil {
		return nil, err
	}
	return c.getFromHTML(ctx, approvalURL)
}

// getFromHTML faz scraping da página HTML para extrair informações
func (c *Client) getFromHTML(ctx context.Context, approvalURL string) (*ApprovalInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", approvalURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("página retornou status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return c.parseHTML(string(body), approvalURL)
}

// parseHTML extrai informações do HTML da página
func (c *Client) parseHTML(html, approvalURL string) (*ApprovalInfo, error) {
	info := &ApprovalInfo{}

	// Extrair ID da URL
	id, _ := ExtractApprovalID(approvalURL)
	info.ID = id

	// Título preferencial: <title>SRE Approval | CHGxxxxx | [Squad] app - version</title>
	titleTagRe := regexp.MustCompile(`(?i)<title>[^<]*\|\s*(CHG\d+)\s*\|\s*([^<]+)</title>`)
	if match := titleTagRe.FindStringSubmatch(html); len(match) > 2 {
		info.ChangeNumber = match[1]
		raw := strings.TrimSpace(match[2])
		info.Title = raw
		// Extrair [Squad] app - version do título
		if m := regexp.MustCompile(`\[([^\]]+)\]\s*([a-zA-Z0-9_.-]+(?:-[a-zA-Z][a-zA-Z0-9_.-]*)*)\s*-\s*([0-9][0-9.\-]+\S*)`).FindStringSubmatch(raw); len(m) > 3 {
			info.SquadOwner = m[1]
			info.Application = m[2]
			info.Version = m[3]
		}
	} else {
		// Fallback: CHG no corpo do HTML
		if match := regexp.MustCompile(`(CHG\d+)`).FindStringSubmatch(html); len(match) > 1 {
			info.ChangeNumber = match[1]
		}
		// Fallback: [Squad] app - version no corpo
		if match := regexp.MustCompile(`\[([^\]]+)\]\s*([a-zA-Z0-9_-]+)\s*-\s*([0-9][0-9.\-]+)`).FindStringSubmatch(html); len(match) > 3 {
			info.SquadOwner = match[1]
			info.Application = match[2]
			info.Version = match[3]
			info.Title = fmt.Sprintf("[%s] %s - %s", match[1], match[2], match[3])
		}
	}

	// Status: página finalizada tem texto explícito; pendente tem o botão submit
	if strings.Contains(html, "já foi finalizada") || strings.Contains(html, "finalizada!") {
		info.Status = StatusFinalized
		info.IsFinalized = true
	} else if regexp.MustCompile(`(?i)<input[^>]+type=["']?submit["']?`).MatchString(html) {
		// Formulário ainda aberto (botão "Aprovar Mudança" presente)
		info.Status = StatusPending
		info.IsFinalized = false
	}

	// Email do aprovador (quando já aprovado/finalizado)
	emailPattern := regexp.MustCompile(`Email:\s*([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`)
	if match := emailPattern.FindStringSubmatch(html); len(match) > 1 {
		info.ApproverEmail = match[1]
	}

	// Squad do aprovador: elemento com classe sre_group_name
	sreGroupRe := regexp.MustCompile(`(?i)class="[^"]*sre_group_name[^"]*"[^>]*>([^<]+)<`)
	if match := sreGroupRe.FindStringSubmatch(html); len(match) > 1 {
		info.ApproverSquad = strings.TrimSpace(match[1])
	} else {
		// Fallback texto "Squad: ..."
		if match := regexp.MustCompile(`Squad:\s*([^\n<]+)`).FindStringSubmatch(html); len(match) > 1 {
			info.ApproverSquad = strings.TrimSpace(match[1])
		}
	}

	return info, nil
}

// Approve submete o formulário de aprovação SRE com o email do aprovador.
// Fluxo: GET página → extrai CSRF + campos hidden → POST com email.
func (c *Client) Approve(ctx context.Context, approvalURL string, approverEmail string) error {
	var err error
	if approverEmail == "" {
		approverEmail, err = GetCurrentUserEmail(ctx)
		if err != nil {
			return fmt.Errorf("não foi possível obter email do usuário: %w", err)
		}
	}

	// Cookie jar mantém sessão entre GET e POST (necessário para CSRF session-based)
	jar, _ := cookiejar.New(nil)
	sessionClient := &http.Client{Timeout: 30 * time.Second, Jar: jar}

	// GET: carregar o formulário
	getReq, err := http.NewRequestWithContext(ctx, "GET", approvalURL, nil)
	if err != nil {
		return err
	}
	getReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	getReq.Header.Set("Accept", "text/html,application/xhtml+xml")

	getResp, err := sessionClient.Do(getReq)
	if err != nil {
		return fmt.Errorf("erro ao carregar formulário de aprovação: %w", err)
	}
	rawBody, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	html := string(rawBody)

	if strings.Contains(html, "já foi finalizada") {
		finErr := &ErrAlreadyFinalized{}
		emailRe := regexp.MustCompile(`Email:\s*([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)
		if m := emailRe.FindStringSubmatch(html); len(m) > 1 {
			finErr.ApproverEmail = m[1]
		}
		squadRe := regexp.MustCompile(`Squad:\s*([^\n<]+)`)
		if m := squadRe.FindStringSubmatch(html); len(m) > 1 {
			finErr.ApproverSquad = strings.TrimSpace(m[1])
		}
		return finErr
	}

	// Extrair action do <form> (default: POST para a mesma URL)
	baseURL := ExtractBaseURL(approvalURL)
	formAction := approvalURL
	if m := regexp.MustCompile(`(?i)<form[^>]+action="([^"]+)"`).FindStringSubmatch(html); len(m) > 1 {
		action := m[1]
		switch {
		case strings.HasPrefix(action, "http"):
			formAction = action
		case strings.HasPrefix(action, "/"):
			formAction = baseURL + action
		}
	}

	// Coletar todos os campos <input type="hidden"> (inclui csrf_token se presente)
	formData := url.Values{}
	hiddenRe := regexp.MustCompile(`(?i)<input[^>]+type=["']?hidden["']?[^>]*>`)
	nameRe := regexp.MustCompile(`(?i)\sname=["']([^"']+)["']`)
	valueRe := regexp.MustCompile(`(?i)\svalue=["']([^"']*)["']`)
	for _, field := range hiddenRe.FindAllString(html, -1) {
		nm := nameRe.FindStringSubmatch(field)
		if len(nm) < 2 {
			continue
		}
		val := ""
		if vm := valueRe.FindStringSubmatch(field); len(vm) > 1 {
			val = vm[1]
		}
		formData.Set(nm[1], val)
	}
	formData.Set("email", approverEmail)
	formData.Set("submit", "Aprovar Mudança")

	c.logger.Info().
		Str("form_action", formAction).
		Str("approver_email", approverEmail).
		Int("hidden_fields", len(formData)-1).
		Msg("[SRE] Submetendo aprovação via form POST")

	// POST: enviar formulário
	postReq, err := http.NewRequestWithContext(ctx, "POST", formAction, strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Referer", approvalURL)
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	postReq.Header.Set("Accept", "text/html,application/xhtml+xml")

	postResp, err := sessionClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("erro ao submeter aprovação: %w", err)
	}
	defer postResp.Body.Close()
	respBody, _ := io.ReadAll(postResp.Body)

	if postResp.StatusCode >= 400 {
		return fmt.Errorf("aprovação falhou (status %d) — verifique se a URL está correta", postResp.StatusCode)
	}

	respHTML := string(respBody)
	if strings.Contains(respHTML, "já foi finalizada") || strings.Contains(strings.ToLower(respHTML), "aprovad") {
		c.logger.Info().Str("url", approvalURL).Msg("[SRE] Aprovação confirmada")
	}

	return nil
}

// GetCurrentUserEmail obtém o email do usuário atual via Azure CLI
func GetCurrentUserEmail(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "az", "account", "show", "--query", "user.name", "-o", "tsv")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("falha ao obter email do usuário: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
