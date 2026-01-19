package nexus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HTTPClient implementa a interface Client usando HTTP
type HTTPClient struct {
	config     Config
	httpClient *http.Client
}

// NewHTTPClient cria uma nova instância do HTTPClient
func NewHTTPClient(config Config) *HTTPClient {
	return &HTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TestConnection testa a conexão com o Nexus
func (c *HTTPClient) TestConnection() (*TestConnectionResponse, error) {
	// Tenta acessar a API do Nexus para verificar a versão
	url := fmt.Sprintf("%s/service/rest/v1/status", strings.TrimSuffix(c.config.BaseURL, "/"))
	
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return &TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create request: %v", err),
		}, nil
	}

	// Adiciona autenticação
	req.SetBasicAuth(c.config.Username, c.config.Password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Connection failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return &TestConnectionResponse{
			Success: false,
			Message: "Authentication failed: invalid username or password",
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return &TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Unexpected status code: %d", resp.StatusCode),
		}, nil
	}

	return &TestConnectionResponse{
		Success: true,
		Message: "Connection successful",
		Version: "Nexus 3.x",
	}, nil
}

// DefaultURLPattern é o padrão de URL padrão para arquivos no Nexus
const DefaultURLPattern = "{baseUrl}/repository/{repository}/{release}/{version}/{environment}/helm-values/{type}-values.yaml"

// BuildURL constrói a URL completa para um arquivo de values
func (c *HTTPClient) BuildURL(req ValuesFileRequest) string {
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	repository := c.config.Repository

	// Usa o padrão customizado se fornecido, senão usa o padrão default
	pattern := c.config.URLPattern
	if pattern == "" {
		pattern = DefaultURLPattern
	}

	// Substitui os placeholders
	url := pattern
	url = strings.ReplaceAll(url, "{baseUrl}", baseURL)
	url = strings.ReplaceAll(url, "{repository}", repository)
	url = strings.ReplaceAll(url, "{release}", req.Release)
	url = strings.ReplaceAll(url, "{version}", req.Version)
	url = strings.ReplaceAll(url, "{environment}", req.Environment)
	url = strings.ReplaceAll(url, "{type}", req.Type)

	return url
}

// DownloadValues baixa um arquivo de values do Nexus
func (c *HTTPClient) DownloadValues(req ValuesFileRequest) (*ValuesFileResponse, error) {
	// Valida inputs
	if !IsValidEnvironment(req.Environment) {
		return &ValuesFileResponse{
			Error: fmt.Sprintf("Invalid environment: %s. Valid values: %v", req.Environment, ValidEnvironments),
		}, nil
	}

	if !IsValidType(req.Type) {
		return &ValuesFileResponse{
			Error: fmt.Sprintf("Invalid type: %s. Valid values: %v", req.Type, ValidTypes),
		}, nil
	}

	// Constrói URL
	url := c.BuildURL(req)
	fmt.Printf("[Nexus] Downloading from URL: %s\n", url)
	
	// Cria requisição
	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to create request: %v", err)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
		}, nil
	}

	// Adiciona autenticação
	httpReq.SetBasicAuth(c.config.Username, c.config.Password)
	fmt.Printf("[Nexus] Using authentication for user: %s\n", c.config.Username)

	// Executa requisição
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		errMsg := fmt.Sprintf("Request failed: %v", err)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
			FullURL: url,
		}, nil
	}
	defer resp.Body.Close()

	fmt.Printf("[Nexus] Response status: %d\n", resp.StatusCode)

	// Verifica status
	if resp.StatusCode == http.StatusNotFound {
		errMsg := fmt.Sprintf("File not found: %s", url)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
			FullURL: url,
		}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		errMsg := "Authentication failed"
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
			FullURL: url,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Unexpected status code: %d", resp.StatusCode)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
			FullURL: url,
		}, nil
	}

	// Lê conteúdo
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to read response: %v", err)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error: errMsg,
			FullURL: url,
		}, nil
	}

	fmt.Printf("[Nexus] Downloaded %d bytes successfully\n", len(content))

	// Salva em arquivo temporário se TempDir estiver configurado
	var localPath string
	if c.config.TempDir != "" {
		localPath, err = c.saveToTemp(req, content)
		if err != nil {
			// Não falha se não conseguir salvar localmente, apenas loga
			fmt.Printf("[Nexus] Warning: failed to save temp file: %v\n", err)
		} else {
			fmt.Printf("[Nexus] Saved to temp file: %s\n", localPath)
		}
	} else {
		fmt.Printf("[Nexus] TempDir not configured, skipping local save\n")
	}

	// Log do conteúdo para debug
	contentStr := string(content)
	fmt.Printf("[Nexus] Content length: %d, preview: %s...\n", len(contentStr), truncateString(contentStr, 100))

	return &ValuesFileResponse{
		Content:  contentStr,
		FilePath: fmt.Sprintf("%s/%s/%s/helm-values/%s-values.yaml", req.Release, req.Version, req.Environment, req.Type),
		FullURL:  url,
		Size:     int64(len(content)),
	}, nil
}

// truncateString trunca uma string para o tamanho máximo especificado
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// saveToTemp salva o conteúdo em um arquivo temporário
func (c *HTTPClient) saveToTemp(req ValuesFileRequest, content []byte) (string, error) {
	// Cria estrutura de diretórios
	tempDir := filepath.Join(c.config.TempDir, req.Release, req.Version, req.Environment)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", err
	}

	// Salva arquivo
	filename := fmt.Sprintf("%s-values.yaml", req.Type)
	filePath := filepath.Join(tempDir, filename)
	
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return "", err
	}

	return filePath, nil
}

// DownloadMultipleValues baixa múltiplos arquivos em paralelo
func (c *HTTPClient) DownloadMultipleValues(reqs []ValuesFileRequest) ([]ValuesFileResponse, error) {
	results := make([]ValuesFileResponse, len(reqs))
	errors := make([]error, len(reqs))

	// Canal para limitar concorrência
	semaphore := make(chan struct{}, 5) // Máximo 5 downloads paralelos

	// WaitGroup para sincronização
	type result struct {
		index    int
		response *ValuesFileResponse
		err      error
	}
	resultChan := make(chan result, len(reqs))

	// Inicia downloads em paralelo
	for i, req := range reqs {
		go func(index int, request ValuesFileRequest) {
			semaphore <- struct{}{} // Adquire slot
			defer func() { <-semaphore }() // Libera slot

			resp, err := c.DownloadValues(request)
			resultChan <- result{index: index, response: resp, err: err}
		}(i, req)
	}

	// Coleta resultados
	for i := 0; i < len(reqs); i++ {
		res := <-resultChan
		if res.response != nil {
			results[res.index] = *res.response
		}
		errors[res.index] = res.err
	}

	// Verifica se houve algum erro crítico
	hasError := false
	for _, err := range errors {
		if err != nil {
			hasError = true
			break
		}
	}

	if hasError {
		return results, fmt.Errorf("some downloads failed")
	}

	return results, nil
}

// CleanupTempFiles remove arquivos temporários mais antigos que a duração especificada
func (c *HTTPClient) CleanupTempFiles(olderThan time.Duration) error {
	if c.config.TempDir == "" {
		return nil
	}

	cutoff := time.Now().Add(-olderThan)

	return filepath.Walk(c.config.TempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Não remove diretórios, apenas arquivos
		if info.IsDir() {
			return nil
		}

		// Remove se for mais antigo que o cutoff
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				fmt.Printf("Warning: failed to remove old temp file %s: %v\n", path, err)
			}
		}

		return nil
	})
}
