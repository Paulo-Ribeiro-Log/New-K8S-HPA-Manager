package servicenow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const snBaseURL = "https://viavarejo.service-now.com"
const snCookieDomain = "service-now.com"

// SNDirectClient usa cookies de sessão do Chrome para chamar a REST API do ServiceNow
// diretamente — sem browser, sem CDP, sem display.
type SNDirectClient struct {
	cookies map[string]string
	http    *http.Client
}

// NewSNDirectClient cria um cliente com os cookies informados.
func NewSNDirectClient(cookies map[string]string) *SNDirectClient {
	return &SNDirectClient{
		cookies: cookies,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// cookieHeader monta o valor do header Cookie a partir do mapa de cookies.
func (c *SNDirectClient) cookieHeader() string {
	parts := make([]string, 0, len(c.cookies))
	for name, value := range c.cookies {
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "; ")
}

// get faz GET autenticado e retorna o body.
func (c *SNDirectClient) get(url string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cookie", c.cookieHeader())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("erro de rede: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

// snTableResult é a estrutura de resposta da Table API do ServiceNow.
type snTableResult struct {
	Result json.RawMessage `json:"result"`
	Error  struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	} `json:"error"`
}

// FetchBySysID busca uma CHG pelo sys_id via REST API do ServiceNow.
func (c *SNDirectClient) FetchBySysID(sysID string) (*PlaywrightResult, error) {
	fields := "number,short_description,description,justification,state,u_motivo_mudanca"
	url := fmt.Sprintf("%s/api/now/table/change_request/%s?sysparm_fields=%s&sysparm_display_value=true", snBaseURL, sysID, fields)

	body, status, err := c.get(url)
	if err != nil {
		return nil, err
	}

	if status == 401 || status == 403 {
		return &PlaywrightResult{
			Success: false,
			Error:   "Sessão ServiceNow expirada ou inválida. Faça login no Chrome e use 'Atualizar Sessão'.",
		}, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("ServiceNow API retornou HTTP %d", status)
	}

	var envelope snTableResult
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("resposta inválida do ServiceNow: %v", err)
	}
	if envelope.Error.Message != "" {
		return &PlaywrightResult{Success: false, Error: envelope.Error.Message}, nil
	}

	return c.parseResult(envelope.Result)
}

// FetchByNumber busca uma CHG pelo número (ex: CHG0001234) via query.
func (c *SNDirectClient) FetchByNumber(chgNumber string) (*PlaywrightResult, error) {
	fields := "number,short_description,description,justification,state,u_motivo_mudanca"
	url := fmt.Sprintf("%s/api/now/table/change_request?sysparm_query=number=%s&sysparm_fields=%s&sysparm_display_value=true&sysparm_limit=1",
		snBaseURL, chgNumber, fields)

	body, status, err := c.get(url)
	if err != nil {
		return nil, err
	}

	if status == 401 || status == 403 {
		return &PlaywrightResult{
			Success: false,
			Error:   "Sessão ServiceNow expirada. Atualize a sessão no menu de perfil.",
		}, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("ServiceNow API retornou HTTP %d", status)
	}

	var envelope struct {
		Result []json.RawMessage `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("resposta inválida: %v", err)
	}
	if envelope.Error.Message != "" {
		return &PlaywrightResult{Success: false, Error: envelope.Error.Message}, nil
	}
	if len(envelope.Result) == 0 {
		return &PlaywrightResult{Success: false, Error: fmt.Sprintf("CHG %s não encontrada", chgNumber)}, nil
	}

	return c.parseResult(envelope.Result[0])
}

// parseResult converte o JSON de um item da Table API em PlaywrightResult.
func (c *SNDirectClient) parseResult(raw json.RawMessage) (*PlaywrightResult, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("erro ao parsear campos: %v", err)
	}

	str := func(key string) string {
		v, ok := fields[key]
		if !ok {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case map[string]interface{}:
			if dv, ok := val["display_value"].(string); ok {
				return dv
			}
		}
		return ""
	}

	number := str("number")
	shortDesc := str("short_description")

	// Prefere o campo que contém o template da esteira CHG (marcadores: github.com/, * Aplicação, etc.)
	// u_motivo_mudanca é o campo PT-BR customizado — tem prioridade sobre justification (genérico).
	candidates := []string{
		str("u_motivo_mudanca"),
		str("justification"),
		str("description"),
	}
	description := ""
	for _, c := range candidates {
		if c != "" && snHasTemplate(c) {
			description = c
			break
		}
	}
	// Nenhum campo tem template (todos vazios ou ACL bloqueou) — usar o primeiro não-vazio
	if description == "" {
		for _, c := range candidates {
			if c != "" {
				description = c
				break
			}
		}
	}
	state := str("state")

	re := &RodExtractor{} // só para reutilizar parseDescription
	extracted := re.parseDescription(description)

	// Tentar extrair CHG number do campo se veio vazio
	if number == "" {
		re2 := regexp.MustCompile(`CHG\d+`)
		if m := re2.FindString(description); m != "" {
			number = m
		}
	}

	return &PlaywrightResult{
		Success:          true,
		ChangeNumber:     number,
		ShortDescription: shortDesc,
		Description:      description,
		State:            state,
		Extracted:        extracted,
	}, nil
}

// snHasTemplate verifica se o texto contém os marcadores do template CHG da esteira.
// Usado para distinguir o "Motivo da mudança" (com github.com/, * Aplicação, etc.)
// de campos genéricos como justification (ex: "Realizar deploy conforme planejado").
func snHasTemplate(s string) bool {
	return strings.Contains(s, "github.com/") ||
		strings.Contains(s, "* Aplicação") ||
		strings.Contains(s, "* Versão:") ||
		strings.Contains(s, "* Repositório:")
}

// SNUserResult é o retorno de FetchUserByEmail — busca na tabela sys_user (usuário/matrícula),
// não confundir com PlaywrightResult (CHGs em change_request).
type SNUserResult struct {
	Success   bool   `json:"success"`
	Email     string `json:"email"`
	Matricula string `json:"matricula"`
	Name      string `json:"name"`
	UserName  string `json:"userName"`
	Error     string `json:"error,omitempty"`
}

// FetchUserByEmail busca um usuário na tabela sys_user pelo e-mail via REST API do ServiceNow.
// O nome do campo de matrícula varia por instância — tenta employee_number (nome padrão do
// ServiceNow) e cai para u_matricula (convenção de campo customizado PT-BR já vista em
// u_motivo_mudanca/change_request nesta mesma instância) se o primeiro vier vazio.
func (c *SNDirectClient) FetchUserByEmail(email string) (*SNUserResult, error) {
	fields := "email,employee_number,u_matricula,name,user_name"
	apiURL := fmt.Sprintf("%s/api/now/table/sys_user?sysparm_query=email=%s&sysparm_fields=%s&sysparm_display_value=true&sysparm_limit=1",
		snBaseURL, url.QueryEscape(email), fields)

	body, status, err := c.get(apiURL)
	if err != nil {
		return nil, err
	}

	if status == 401 || status == 403 {
		return &SNUserResult{
			Success: false,
			Error:   "Sessão ServiceNow expirada. Atualize a sessão no menu de perfil.",
		}, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("ServiceNow API retornou HTTP %d", status)
	}

	var envelope struct {
		Result []json.RawMessage `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("resposta inválida: %v", err)
	}
	if envelope.Error.Message != "" {
		return &SNUserResult{Success: false, Error: envelope.Error.Message}, nil
	}
	if len(envelope.Result) == 0 {
		return &SNUserResult{Success: false, Error: fmt.Sprintf("nenhum usuário encontrado no ServiceNow para %s", email)}, nil
	}

	var fields2 map[string]interface{}
	if err := json.Unmarshal(envelope.Result[0], &fields2); err != nil {
		return nil, fmt.Errorf("erro ao parsear usuário: %v", err)
	}
	str := func(key string) string {
		v, ok := fields2[key]
		if !ok {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case map[string]interface{}:
			if dv, ok := val["display_value"].(string); ok {
				return dv
			}
		}
		return ""
	}

	matricula := str("employee_number")
	if matricula == "" {
		matricula = str("u_matricula")
	}

	return &SNUserResult{
		Success:   true,
		Email:     str("email"),
		Matricula: matricula,
		Name:      str("name"),
		UserName:  str("user_name"),
	}, nil
}

// FetchUserByEmailViaCDP resolve os cookies de sessão do Chrome via CDP (mesmo "caminho rápido"
// usado por RodExtractor.Extract para CHGs) e busca a matrícula do usuário pelo e-mail — sem
// abrir browser nem PowerShell.
func FetchUserByEmailViaCDP(email string) (*SNUserResult, error) {
	cookies, err := ExtractCookiesViaCDP(WindowsCDPPort, snCookieDomain)
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter cookies de sessão do ServiceNow via CDP: %w", err)
	}
	client := NewSNDirectClient(cookies)
	return client.FetchUserByEmail(email)
}

// FetchFromURL extrai sys_id ou número da URL e busca via API.
func (c *SNDirectClient) FetchFromURL(chgURL string) (*PlaywrightResult, error) {
	sysIDRe := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	if m := sysIDRe.FindStringSubmatch(chgURL); len(m) > 1 {
		return c.FetchBySysID(m[1])
	}

	chgNumRe := regexp.MustCompile(`(CHG\d+)`)
	if m := chgNumRe.FindStringSubmatch(chgURL); len(m) > 1 {
		return c.FetchByNumber(m[1])
	}

	return nil, fmt.Errorf("URL ServiceNow inválida: não contém sys_id nem número CHG")
}
