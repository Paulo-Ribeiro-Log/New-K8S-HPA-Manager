package sreapproval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

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

// GetApprovalInfo obtém informações de uma solicitação de aprovação
func (c *Client) GetApprovalInfo(ctx context.Context, approvalURL string) (*ApprovalInfo, error) {
	// Extrair ID da URL
	approvalID, err := ExtractApprovalID(approvalURL)
	if err != nil {
		return nil, err
	}

	c.logger.Info().
		Str("approval_id", approvalID).
		Str("url", approvalURL).
		Msg("Buscando informações de aprovação SRE")

	// Tentar API primeiro
	info, err := c.getFromAPI(ctx, approvalID)
	if err == nil {
		return info, nil
	}

	c.logger.Warn().Err(err).Msg("API não disponível, tentando scraping da página")

	// Fallback: scraping da página HTML
	return c.getFromHTML(ctx, approvalURL)
}

// getFromAPI tenta obter dados via API REST
func (c *Client) getFromAPI(ctx context.Context, approvalID string) (*ApprovalInfo, error) {
	url := fmt.Sprintf("%s/sre-approval/api/approval/%s", c.baseURL, approvalID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API retornou status %d", resp.StatusCode)
	}

	var info ApprovalInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

// getFromHTML faz scraping da página HTML para extrair informações
func (c *Client) getFromHTML(ctx context.Context, approvalURL string) (*ApprovalInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", approvalURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "K8s-HPA-Manager/1.0")

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

	// Padrões para extrair dados do HTML
	// CHG0436696
	chgPattern := regexp.MustCompile(`(CHG\d+)`)
	if match := chgPattern.FindStringSubmatch(html); len(match) > 1 {
		info.ChangeNumber = match[1]
	}

	// Título: [Squad] app - version
	// Procura no HTML entre tags ou em texto
	titlePattern := regexp.MustCompile(`\[([^\]]+)\]\s*([a-zA-Z0-9_-]+)\s*-\s*([0-9][0-9.\-]+)`)
	if match := titlePattern.FindStringSubmatch(html); len(match) > 3 {
		info.SquadOwner = match[1]
		info.Application = match[2]
		info.Version = match[3]
		info.Title = fmt.Sprintf("[%s] %s - %s", match[1], match[2], match[3])
	}

	// Status: verificar se já foi finalizada
	if strings.Contains(html, "já foi finalizada") || strings.Contains(html, "finalizada!") {
		info.Status = StatusFinalized
		info.IsFinalized = true
	} else if strings.Contains(html, "aguardando") || strings.Contains(html, "pendente") {
		info.Status = StatusPending
		info.IsFinalized = false
	}

	// Email do aprovador
	emailPattern := regexp.MustCompile(`Email:\s*([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`)
	if match := emailPattern.FindStringSubmatch(html); len(match) > 1 {
		info.ApproverEmail = match[1]
	}

	// Squad do aprovador
	squadPattern := regexp.MustCompile(`Squad:\s*([^\n<]+)`)
	if match := squadPattern.FindStringSubmatch(html); len(match) > 1 {
		info.ApproverSquad = strings.TrimSpace(match[1])
	}

	return info, nil
}

// Approve executa a aprovação de uma solicitação
func (c *Client) Approve(ctx context.Context, approvalURL string, approverEmail string) error {
	approvalID, err := ExtractApprovalID(approvalURL)
	if err != nil {
		return err
	}

	// Se email não fornecido, obter do Azure CLI
	if approverEmail == "" {
		approverEmail, err = GetCurrentUserEmail(ctx)
		if err != nil {
			return fmt.Errorf("não foi possível obter email do usuário: %w", err)
		}
	}

	c.logger.Info().
		Str("approval_id", approvalID).
		Str("approver_email", approverEmail).
		Msg("Executando aprovação SRE")

	// Tentar aprovar via API
	url := fmt.Sprintf("%s/sre-approval/api/approve", c.baseURL)

	payload := map[string]string{
		"approval_id": approvalID,
		"email":       approverEmail,
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro ao chamar API de aprovação: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aprovação falhou (status %d): %s", resp.StatusCode, string(body))
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
