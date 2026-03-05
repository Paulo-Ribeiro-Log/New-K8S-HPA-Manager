package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"k8s-hpa-manager/internal/pkg/nexus"
)

// awxCredentials armazena URL + credenciais SSO do AWX.
type awxCredentials struct {
	BaseURL           string `json:"base_url"`
	Username          string `json:"username"`
	EncryptedPassword string `json:"encrypted_password"`
}

// AWXHandler integra com a API do AWX (Ansible AWX/Tower) para gerenciar certificados TLS.
type AWXHandler struct {
	baseDir    string // ~/.k8s-hpa-manager/ — onde salvar credenciais
	httpClient *http.Client
}

// NewAWXHandler cria o handler.
func NewAWXHandler(baseDir string) *AWXHandler {
	return &AWXHandler{
		baseDir: baseDir,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// credentialsPath retorna o caminho do arquivo de credenciais AWX.
func (h *AWXHandler) credentialsPath() string {
	return filepath.Join(h.baseDir, "awx_credentials.json")
}

// loadCredentials carrega URL + credenciais do arquivo e descriptografa a senha.
func (h *AWXHandler) loadCredentials() (*awxCredentials, error) {
	data, err := os.ReadFile(h.credentialsPath())
	if err != nil {
		return nil, fmt.Errorf("credenciais AWX não configuradas")
	}

	var creds awxCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("arquivo de credenciais AWX inválido")
	}

	if creds.BaseURL == "" {
		return nil, fmt.Errorf("URL do AWX não configurada")
	}
	if creds.Username == "" || creds.EncryptedPassword == "" {
		return nil, fmt.Errorf("usuário/senha AWX não configurados")
	}

	password, err := nexus.DecryptPassword(creds.EncryptedPassword)
	if err != nil {
		return nil, fmt.Errorf("erro ao descriptografar senha AWX: %w", err)
	}
	creds.EncryptedPassword = password // campo reutilizado como senha plaintext
	return &creds, nil
}

// awxGet executa GET autenticado com Basic Auth na API do AWX.
func (h *AWXHandler) awxGet(path string, out interface{}) error {
	creds, err := h.loadCredentials()
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(creds.BaseURL, "/")
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(creds.Username, creds.EncryptedPassword)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar no AWX: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("usuário ou senha inválidos (401)")
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AWX retornou %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// awxPost executa POST autenticado com Basic Auth na API do AWX.
func (h *AWXHandler) awxPost(path string, payload interface{}, out interface{}) error {
	creds, err := h.loadCredentials()
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(creds.BaseURL, "/")
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.SetBasicAuth(creds.Username, creds.EncryptedPassword)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar no AWX: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("usuário ou senha inválidos (401)")
	}
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AWX retornou %d: %s", resp.StatusCode, string(respBody))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ── Gerenciamento de Credenciais ──────────────────────────────────────────────

// SaveCredentials — POST /api/v1/awx/credentials
// Salva URL + usuário + senha (senha criptografada em AES-256-GCM).
func (h *AWXHandler) SaveCredentials(c *gin.Context) {
	var req struct {
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.BaseURL == "" || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url e username são obrigatórios"})
		return
	}

	// Se não enviou senha, mantém a atual (se existir)
	password := req.Password
	if password == "" {
		existing, err := h.loadCredentials()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "senha é obrigatória na primeira configuração"})
			return
		}
		password = existing.EncryptedPassword // já descriptografada pelo loadCredentials
	}

	encrypted, err := nexus.EncryptPassword(password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criptografar senha: " + err.Error()})
		return
	}

	// Normaliza URL: remove fragmento (#/login etc.) e trailing slash
	cleanURL := req.BaseURL
	if idx := strings.Index(cleanURL, "#"); idx != -1 {
		cleanURL = cleanURL[:idx]
	}
	cleanURL = strings.TrimRight(cleanURL, "/")

	creds := awxCredentials{
		BaseURL:           cleanURL,
		Username:          req.Username,
		EncryptedPassword: encrypted,
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(h.baseDir, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.WriteFile(h.credentialsPath(), data, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Info().Str("base_url", creds.BaseURL).Str("username", creds.Username).Msg("[AWX] Credenciais salvas")
	c.JSON(http.StatusOK, gin.H{"message": "Credenciais AWX salvas com sucesso"})
}

// GetCredentialsStatus — GET /api/v1/awx/credentials/status
// Verifica se credenciais estão configuradas e se o AWX está acessível.
func (h *AWXHandler) GetCredentialsStatus(c *gin.Context) {
	creds, err := h.loadCredentials()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"configured": false, "reachable": false, "reason": err.Error()})
		return
	}

	var info map[string]interface{}
	if err := h.awxGet("/api/v2/", &info); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"configured": true,
			"reachable":  false,
			"base_url":   creds.BaseURL,
			"username":   creds.Username,
			"error":      err.Error(),
		})
		return
	}

	version := ""
	if v, ok := info["version"].(string); ok {
		version = v
	}
	c.JSON(http.StatusOK, gin.H{
		"configured": true,
		"reachable":  true,
		"base_url":   creds.BaseURL,
		"username":   creds.Username,
		"version":    version,
	})
}

// DeleteCredentials — DELETE /api/v1/awx/credentials
// Remove as credenciais AWX salvas.
func (h *AWXHandler) DeleteCredentials(c *gin.Context) {
	if err := os.Remove(h.credentialsPath()); err != nil && !os.IsNotExist(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info().Msg("[AWX] Credenciais removidas")
	c.JSON(http.StatusOK, gin.H{"message": "Credenciais AWX removidas"})
}

// ── Status (legado — mantido para compatibilidade) ───────────────────────────

// Status — GET /api/v1/awx/status
func (h *AWXHandler) Status(c *gin.Context) {
	h.GetCredentialsStatus(c)
}

// ── Informações do Cluster (via clusters-config.json) ────────────────────────

var nonAlphanumRE = regexp.MustCompile(`[^A-Z0-9]+`)

// subscriptionToSubsEnv normaliza a subscription para o formato AWX subs_env.
// Ex: "DEV/HLG - ONLINE" → "DEV_HLG_ONLINE", "PRD - ONLINE 2" → "PRD_ONLINE_2"
func subscriptionToSubsEnv(subscription string) string {
	upper := strings.ToUpper(subscription)
	normalized := nonAlphanumRE.ReplaceAllString(upper, "_")
	return strings.Trim(normalized, "_")
}

// isUUID retorna true se s parece um UUID (ex: gerado pelo autodiscover do Azure).
func isUUID(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}

// awxClusterEntry é uma entrada do clusters-config.json lido diretamente para o AWX.
type awxClusterEntry struct {
	ClusterName    string `json:"clusterName"`
	ResourceGroup  string `json:"resourceGroup"`
	Subscription   string `json:"subscription"`             // Nome legível ou UUID (configs antigas)
	SubscriptionID string `json:"subscriptionId,omitempty"` // UUID (configs novas geradas pelo autodiscover)
}

// loadClustersConfigDirect lê clusters-config.json priorizando:
// 1. diretório de trabalho (projeto) — tem nomes legíveis como "DEV/HLG - ONLINE"
// 2. diretório do executável
// 3. ~/.k8s-hpa-manager/ — pode ter GUIDs do autodiscover
func loadClustersConfigDirect() ([]awxClusterEntry, error) {
	candidates := []string{}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "clusters-config.json"))
	}
	if execPath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(execPath), "clusters-config.json"))
	}
	candidates = append(candidates, filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json"))

	var lastErr error
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		var entries []awxClusterEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			lastErr = err
			continue
		}
		// Verificar se este arquivo tem nomes legíveis (não GUIDs)
		hasReadableNames := false
		for _, e := range entries {
			if !isUUID(e.Subscription) && e.Subscription != "" {
				hasReadableNames = true
				break
			}
		}
		if hasReadableNames {
			return entries, nil
		}
		// Se só tem GUIDs, guarda como fallback e continua tentando
		if len(entries) > 0 && lastErr == nil {
			lastErr = nil
			// continua buscando arquivo com nomes legíveis
		}
	}

	// Fallback: retornar o que tiver, mesmo que sejam GUIDs
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entries []awxClusterEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			continue
		}
		if len(entries) > 0 {
			return entries, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("clusters-config.json não encontrado")
}

// GetClusterInfo — GET /api/v1/awx/cluster-info?cluster=X
// Retorna resource_group e subs_env a partir do clusters-config.json.
func (h *AWXHandler) GetClusterInfo(c *gin.Context) {
	clusterParam := c.Query("cluster")
	if clusterParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}

	entries, err := loadClustersConfigDirect()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "clusters-config.json não encontrado"})
		return
	}

	normalized := strings.TrimSuffix(clusterParam, "-admin")
	for _, e := range entries {
		if strings.TrimSuffix(e.ClusterName, "-admin") == normalized || e.ClusterName == clusterParam {
			subscription := e.Subscription

			// Se subscription é UUID (config gerada pelo autodiscover antigo sem resolução de nome),
			// tentar resolver o nome legível via Azure CLI.
			// Se subscription é UUID (config antiga gerada pelo autodiscover),
			// resolver o nome legível passando o próprio UUID para o Azure CLI.
			if isUUID(subscription) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				nameCmd := exec.CommandContext(ctx, "az", "account", "show",
					"--subscription", subscription,
					"--query", "name",
					"-o", "tsv",
					"--only-show-errors")
				if nameOut, err := nameCmd.Output(); err == nil {
					if name := strings.TrimSpace(string(nameOut)); name != "" {
						log.Info().
							Str("cluster", clusterParam).
							Str("uuid", subscription).
							Str("name", name).
							Msg("[AWX] Subscription UUID resolvido para nome legível")
						subscription = name
					}
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"resource_group":  e.ResourceGroup,
				"subs_env":        subscriptionToSubsEnv(subscription),
				"subscription":    subscription,
				"subscription_id": e.SubscriptionID,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("cluster '%s' não encontrado no clusters-config.json", clusterParam)})
}

// ── Survey do Job Template ────────────────────────────────────────────────────

// parseAWXChoices tenta interpretar o campo "choices" do survey AWX, que pode ser
// uma string separada por "\n" ou um array JSON (dependendo da versão do AWX).
func parseAWXChoices(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Tenta como string ("\n"-separated)
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		var choices []string
		for _, ch := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(ch); t != "" {
				choices = append(choices, t)
			}
		}
		return choices
	}
	// Tenta como []string
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		var choices []string
		for _, ch := range arr {
			if t := strings.TrimSpace(ch); t != "" {
				choices = append(choices, t)
			}
		}
		return choices
	}
	return nil
}

// GetTemplateSurvey — GET /api/v1/awx/templates/:id/survey
// Retorna as opções do campo cert_tls do survey do job template AWX.
// Suporta choices como string "\n"-separada OU como array JSON.
func (h *AWXHandler) GetTemplateSurvey(c *gin.Context) {
	templateID := c.Param("id")

	var survey struct {
		Spec []struct {
			Variable string          `json:"variable"`
			Type     string          `json:"type"`
			Choices  json.RawMessage `json:"choices"` // string "\n"-sep OU []string
		} `json:"spec"`
	}

	path := fmt.Sprintf("/api/v2/job_templates/%s/survey_spec/", templateID)
	if err := h.awxGet(path, &survey); err != nil {
		log.Error().Err(err).Str("template_id", templateID).Msg("[AWX] Falha ao buscar survey do template")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Info().
		Str("template_id", templateID).
		Int("spec_fields", len(survey.Spec)).
		Msg("[AWX] Survey spec recebida")

	// Prioridade: campo exato "cert_tls", depois qualquer campo com "cert" no nome
	var fallbackChoices []string
	var fallbackVariable string

	for _, field := range survey.Spec {
		choices := parseAWXChoices(field.Choices)

		log.Debug().
			Str("variable", field.Variable).
			Str("type", field.Type).
			Int("choices_count", len(choices)).
			Msg("[AWX] Campo do survey")

		if len(choices) == 0 {
			continue
		}

		if field.Variable == "cert_tls" {
			c.JSON(http.StatusOK, gin.H{"choices": choices})
			return
		}

		if strings.Contains(strings.ToLower(field.Variable), "cert") && fallbackChoices == nil {
			fallbackChoices = choices
			fallbackVariable = field.Variable
		}
	}

	if len(fallbackChoices) > 0 {
		log.Info().Str("variable", fallbackVariable).Msg("[AWX] Usando campo cert alternativo do survey")
		c.JSON(http.StatusOK, gin.H{"choices": fallbackChoices, "variable": fallbackVariable})
		return
	}

	// Retornar lista de variáveis para diagnóstico nos logs
	fieldNames := make([]string, len(survey.Spec))
	for i, f := range survey.Spec {
		fieldNames[i] = fmt.Sprintf("%s(%s)", f.Variable, f.Type)
	}
	log.Warn().
		Strs("fields", fieldNames).
		Str("template_id", templateID).
		Msg("[AWX] cert_tls não encontrado no survey")

	c.JSON(http.StatusOK, gin.H{"choices": []string{}, "debug_fields": fieldNames})
}

// ── Certificados ──────────────────────────────────────────────────────────────

type awxCredential struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type awxCredentialList struct {
	Count   int             `json:"count"`
	Next    *string         `json:"next"`
	Results []awxCredential `json:"results"`
}

// ListCerts — GET /api/v1/awx/certificates
// Lista todas as credenciais do AWX para o usuário selecionar.
func (h *AWXHandler) ListCerts(c *gin.Context) {
	var result awxCredentialList
	if err := h.awxGet("/api/v2/credentials/?order_by=name&page_size=200", &result); err != nil {
		log.Error().Err(err).Msg("[AWX] Falha ao listar certificados")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	certs := make([]gin.H, 0, len(result.Results))
	for _, cr := range result.Results {
		certs = append(certs, gin.H{"id": cr.ID, "name": cr.Name})
	}
	c.JSON(http.StatusOK, gin.H{"certificates": certs, "total": result.Count})
}

// ── Lançamento de Jobs ────────────────────────────────────────────────────────

type awxLaunchRequest struct {
	TemplateID           int    `json:"template_id"`
	AppName              string `json:"app_name"`
	SubsEnv              string `json:"subs_env"`
	ClusterResourceGroup string `json:"cluster_resource_group_name"`
	Namespace            string `json:"namespace"`
	CertTLS              string `json:"cert_tls"`
}

type awxJobResponse struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

// LaunchJob — POST /api/v1/awx/jobs/launch
func (h *AWXHandler) LaunchJob(c *gin.Context) {
	var req awxLaunchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}

	if req.TemplateID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template_id é obrigatório (25=instalar, 26=atualizar)"})
		return
	}

	payload := map[string]interface{}{
		"extra_vars": map[string]string{
			"app_name":                    req.AppName,
			"subs_env":                    req.SubsEnv,
			"cluster_resource_group_name": req.ClusterResourceGroup,
			"namespace":                   req.Namespace,
			"cert_tls":                    req.CertTLS,
		},
	}

	var jobResp awxJobResponse
	path := fmt.Sprintf("/api/v2/job_templates/%d/launch/", req.TemplateID)
	if err := h.awxPost(path, payload, &jobResp); err != nil {
		log.Error().Err(err).Int("template_id", req.TemplateID).Msg("[AWX] Falha ao lançar job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Info().Int("job_id", jobResp.ID).Int("template_id", req.TemplateID).
		Str("app_name", req.AppName).Str("namespace", req.Namespace).
		Msg("[AWX] Job lançado com sucesso")

	c.JSON(http.StatusOK, gin.H{"job_id": jobResp.ID})
}

// ── SSE de Logs do Job ────────────────────────────────────────────────────────

type awxJobStatus struct {
	ID              int    `json:"id"`
	Status          string `json:"status"`
	ResultTraceback string `json:"result_traceback"`
	JobExplanation  string `json:"job_explanation"`
}

type awxJobEvent struct {
	ID     int    `json:"id"`
	Stdout string `json:"stdout"`
}

type awxJobEventList struct {
	Count   int           `json:"count"`
	Next    *string       `json:"next"`
	Results []awxJobEvent `json:"results"`
}

var terminalStatuses = map[string]bool{
	"successful": true,
	"failed":     true,
	"canceled":   true,
	"error":      true,
}

// StreamJobLogs — GET /api/v1/awx/jobs/:id/stream (SSE)
func (h *AWXHandler) StreamJobLogs(c *gin.Context) {
	jobIDStr := c.Param("id")
	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id inválido"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	type sseEvent struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	sendEvent := func(evType, data string) {
		payload, _ := json.Marshal(sseEvent{Type: evType, Data: data})
		c.SSEvent("message", string(payload))
		c.Writer.Flush()
	}

	log.Info().Int("job_id", jobID).Msg("[AWX] SSE stream iniciado")

	page := 1
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			log.Info().Int("job_id", jobID).Msg("[AWX] SSE cliente desconectou")
			return
		case <-ticker.C:
			var jobSt awxJobStatus
			if err := h.awxGet(fmt.Sprintf("/api/v2/jobs/%d/", jobID), &jobSt); err != nil {
				log.Error().Err(err).Int("job_id", jobID).Msg("[AWX] Erro ao buscar status do job")
				sendEvent("error", "Erro ao verificar status: "+err.Error())
				return
			}

			sendEvent("status", jobSt.Status)

			eventsPath := fmt.Sprintf("/api/v2/job_events/?job=%d&order_by=counter&page=%d&page_size=50", jobID, page)
			var eventList awxJobEventList
			if err := h.awxGet(eventsPath, &eventList); err != nil {
				log.Warn().Err(err).Int("job_id", jobID).Msg("[AWX] Erro ao buscar eventos")
			} else {
				for _, ev := range eventList.Results {
					if ev.Stdout != "" {
						for _, line := range strings.Split(ev.Stdout, "\n") {
							if trimmed := strings.TrimRight(line, " \t\r"); trimmed != "" {
								sendEvent("log", trimmed)
							}
						}
					}
				}
				if eventList.Next != nil {
					page++
				}
			}

			if terminalStatuses[jobSt.Status] {
				// Enviar detalhes do erro como linhas de log antes do evento final
				if jobSt.Status != "successful" {
					if jobSt.JobExplanation != "" {
						sendEvent("log", "[AWX] "+jobSt.JobExplanation)
					}
					if jobSt.ResultTraceback != "" {
						for _, line := range strings.Split(jobSt.ResultTraceback, "\n") {
							if trimmed := strings.TrimRight(line, " \t\r"); trimmed != "" {
								sendEvent("log", trimmed)
							}
						}
					}
				}
				if jobSt.Status == "successful" {
					sendEvent("complete", "successful")
				} else {
					sendEvent("error", jobSt.Status)
				}
				log.Info().Int("job_id", jobID).Str("status", jobSt.Status).Msg("[AWX] Job finalizado")
				return
			}
		}
	}
}
