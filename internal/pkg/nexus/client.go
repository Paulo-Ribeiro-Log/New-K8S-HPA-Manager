package nexus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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
			Timeout: 60 * time.Second,
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
const DefaultURLPattern = "{baseUrl}/repository/{repository}/{release}/{version}/{environment}/values/{type}-values.yaml"

// BuildURL constrói a URL completa para um arquivo de values
func (c *HTTPClient) BuildURL(req ValuesFileRequest) string {
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")

	// Se temos FilePath (path real coletado da busca), usar URL direta
	if req.FilePath != "" {
		repository := req.Repository
		if repository == "" {
			repository = c.config.Repository
		}
		url := fmt.Sprintf("%s/repository/%s/%s", baseURL, repository, strings.TrimPrefix(req.FilePath, "/"))
		fmt.Printf("[Nexus] BuildURL (direct): %s\n", url)
		return url
	}

	// Fallback: URL por pattern (modo legado)
	repository := c.config.Repository
	if req.Repository != "" {
		repository = req.Repository
	}

	pattern := c.config.URLPattern
	if pattern == "" {
		pattern = DefaultURLPattern
	}

	url := pattern
	url = strings.ReplaceAll(url, "{baseUrl}", baseURL)
	url = strings.ReplaceAll(url, "{repository}", repository)
	url = strings.ReplaceAll(url, "{release}", req.Release)
	url = strings.ReplaceAll(url, "{version}", req.Version)
	url = strings.ReplaceAll(url, "{environment}", req.Environment)
	url = strings.ReplaceAll(url, "{type}", req.Type)

	fmt.Printf("[Nexus] BuildURL (pattern): %s\n", url)
	return url
}

// DownloadValues baixa um arquivo de values do Nexus
func (c *HTTPClient) DownloadValues(req ValuesFileRequest) (*ValuesFileResponse, error) {
	// Valida inputs apenas no modo legado (sem FilePath) — com FilePath, a URL é direta
	// (baseURL/repository/FilePath) e não depende de Release/Version/Environment/Type.
	if req.FilePath == "" {
		if req.Release == "" || req.Version == "" {
			return &ValuesFileResponse{
				Error: "release e version são obrigatórios quando filePath não é informado",
			}, nil
		}
		if req.Environment != "" && !IsValidEnvironment(req.Environment) {
			return &ValuesFileResponse{
				Error: fmt.Sprintf("Invalid environment: %s. Valid values: %v", req.Environment, ValidEnvironments),
			}, nil
		}

		if req.Type != "" && !IsValidType(req.Type) {
			return &ValuesFileResponse{
				Error: fmt.Sprintf("Invalid type: %s. Valid values: %v", req.Type, ValidTypes),
			}, nil
		}
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
			Error:   errMsg,
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
			Error:   errMsg,
			FullURL: url,
		}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		errMsg := "Authentication failed"
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error:   errMsg,
			FullURL: url,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Unexpected status code: %d", resp.StatusCode)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error:   errMsg,
			FullURL: url,
		}, nil
	}

	// Lê conteúdo
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to read response: %v", err)
		fmt.Printf("[Nexus] Error: %s\n", errMsg)
		return &ValuesFileResponse{
			Error:   errMsg,
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
		FilePath: fmt.Sprintf("%s/%s/%s/values/%s-values.yaml", req.Release, req.Version, req.Environment, req.Type),
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
			semaphore <- struct{}{}        // Adquire slot
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

// BrowseRepository busca releases/versões no Nexus usando a Search API (/search)
// Busca em TODOS os repositórios pelo nome da release
// path="" → lista releases cujo nome contém query
// path="meu-release" → lista versões desse release
func (c *HTTPClient) BrowseRepository(path string, query string, repository string) (*BrowseResponse, error) {
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	path = strings.Trim(path, "/")

	if query == "" && path == "" {
		return &BrowseResponse{Items: []BrowseItem{}, Path: ""}, nil
	}

	// Termo de busca
	searchTerm := query
	if path != "" {
		searchTerm = path
	}

	uniqueItems := make(map[string]bool)
	// Mapa de release → set de versões (coletadas junto com a busca de releases)
	releaseVersions := make(map[string]map[string]bool)
	// Mapa de release → repositório (coletado da Search API)
	releaseRepository := make(map[string]string)
	// Mapa de release → versão → set de arquivos reais
	releaseFiles := make(map[string]map[string]map[string]bool)
	continuationToken := ""
	maxPages := 5

	for page := 0; page < maxPages; page++ {
		// /search (componentes) — repository opcional (achado real: sem esse filtro, a busca por
		// nome de release cruza TODOS os repositórios que as credenciais alcançam, o que pode
		// misturar resultados de repositórios sem relação nenhuma com o rollback — ex: um
		// repositório genérico de artefatos vs. o repositório dedicado a histórico de deploy,
		// "continuousdeploy-history" nesta empresa). O campo "name" do componente contém o path
		// completo: "release/version/file.yaml"
		apiURL := fmt.Sprintf("%s/service/rest/v1/search?q=%s", baseURL, searchTerm)
		if repository != "" {
			apiURL += "&repository=" + url.QueryEscape(repository)
		}
		if continuationToken != "" {
			apiURL += "&continuationToken=" + continuationToken
		}

		fmt.Printf("[Nexus] Search: %s (page %d)\n", apiURL, page+1)

		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.SetBasicAuth(c.config.Username, c.config.Password)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("search failed with status %d", resp.StatusCode)
		}

		var result struct {
			Items []struct {
				Name       string `json:"name"`
				Group      string `json:"group"`
				Repository string `json:"repository"`
				Assets     []struct {
					Path       string `json:"path"`
					Repository string `json:"repository"`
				} `json:"assets"`
			} `json:"items"`
			ContinuationToken string `json:"continuationToken"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("failed to decode: %w", err)
		}
		resp.Body.Close()
		cancel()

		fmt.Printf("[Nexus] Search returned %d components on page %d\n", len(result.Items), page+1)
		if page == 0 && len(result.Items) > 0 {
			fmt.Printf("[Nexus] Sample: name=%q group=%q\n", result.Items[0].Name, result.Items[0].Group)
		}

		queryLower := strings.ToLower(query)

		// Função auxiliar para processar um path e extrair release/versão/repositório/arquivo
		processPath := func(fullPath string, repo string) {
			fullPath = strings.Trim(fullPath, "/")
			parts := strings.Split(fullPath, "/")
			if len(parts) < 2 {
				return
			}

			releaseName := parts[0]
			version := parts[1]
			// Tudo após release/version é o path relativo do arquivo
			// Ex: "carga-notfis-api/2.0.5-8/deploy-via-sit.yaml" → "deploy-via-sit.yaml"
			// Ex: "vv-retira/2.5.8-1/hlg/values/values-hlg.yaml" → "hlg/values/values-hlg.yaml"
			var filePath string
			if len(parts) > 2 {
				filePath = strings.Join(parts[2:], "/")
			}

			if path == "" {
				// Listar releases + coletar versões, repositório e arquivos
				if strings.Contains(strings.ToLower(releaseName), queryLower) {
					uniqueItems[releaseName] = true
					if releaseVersions[releaseName] == nil {
						releaseVersions[releaseName] = make(map[string]bool)
					}
					releaseVersions[releaseName][version] = true
					if repo != "" {
						releaseRepository[releaseName] = repo
					}
					if filePath != "" {
						if releaseFiles[releaseName] == nil {
							releaseFiles[releaseName] = make(map[string]map[string]bool)
						}
						if releaseFiles[releaseName][version] == nil {
							releaseFiles[releaseName][version] = make(map[string]bool)
						}
						releaseFiles[releaseName][version][filePath] = true
					}
				}
			} else {
				// Listar versões de uma release específica
				if strings.EqualFold(releaseName, path) {
					if query == "" || strings.Contains(strings.ToLower(version), queryLower) {
						uniqueItems[version] = true
					}
				}
			}
		}

		for _, comp := range result.Items {
			// Filtro defensivo em cima do `&repository=` já mandado na URL acima — nunca
			// confiar só no parâmetro da API pra excluir resultados de outro repositório (sem
			// acesso a um Nexus real nesta sessão pra confirmar que a API sempre respeita o
			// filtro; melhor rejeitar aqui do que arriscar misturar repositórios errados).
			if repository != "" && comp.Repository != "" && !strings.EqualFold(comp.Repository, repository) {
				continue
			}

			// Extrair release e versão do name ou group
			if comp.Name != "" {
				processPath(comp.Name, comp.Repository)
			} else if comp.Group != "" {
				processPath(comp.Group, comp.Repository)
			}

			// Também verificar assets
			for _, asset := range comp.Assets {
				repo := asset.Repository
				if repo == "" {
					repo = comp.Repository
				}
				processPath(asset.Path, repo)
			}
		}

		if result.ContinuationToken == "" {
			break
		}
		continuationToken = result.ContinuationToken
	}

	items := make([]BrowseItem, 0, len(uniqueItems))
	for name := range uniqueItems {
		itemPath := name
		if path != "" {
			itemPath = path + "/" + name
		}
		item := BrowseItem{Name: name, Path: itemPath}

		// Incluir versões, repositório e arquivos quando buscando releases
		if path == "" {
			if versions, ok := releaseVersions[name]; ok {
				versionList := make([]string, 0, len(versions))
				for v := range versions {
					versionList = append(versionList, v)
				}
				sort.Strings(versionList)
				item.Versions = versionList
			}
			if repo, ok := releaseRepository[name]; ok {
				item.Repository = repo
			}
			if versionFiles, ok := releaseFiles[name]; ok {
				item.Files = make(map[string][]string)
				for ver, files := range versionFiles {
					fileList := make([]string, 0, len(files))
					for f := range files {
						fileList = append(fileList, f)
					}
					sort.Strings(fileList)
					item.Files[ver] = fileList
				}
			}
		}

		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	fmt.Printf("[Nexus] Found %d unique items (path='%s', query='%s')\n", len(items), path, query)

	return &BrowseResponse{Items: items, Path: path}, nil
}

// SearchFlatArtifacts busca componentes num repositório SEM hierarquia release/version/arquivo —
// ver comentário de FlatArtifact (types.go). Reaproveita a mesma API de busca/paginação de
// BrowseRepository, mas sem nenhuma tentativa de agrupar por segmentos de path (que é justamente o
// que fazia BrowseRepository descartar esses componentes em silêncio).
func (c *HTTPClient) SearchFlatArtifacts(repository, query string, allRepos bool) ([]FlatArtifact, error) {
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	if query == "" {
		return []FlatArtifact{}, nil
	}

	artifacts := []FlatArtifact{}
	continuationToken := ""
	maxPages := 5

	for page := 0; page < maxPages; page++ {
		apiURL := fmt.Sprintf("%s/service/rest/v1/search?q=%s", baseURL, url.QueryEscape(query))
		if !allRepos && repository != "" {
			apiURL += "&repository=" + url.QueryEscape(repository)
		}
		if continuationToken != "" {
			apiURL += "&continuationToken=" + continuationToken
		}

		fmt.Printf("[Nexus] SearchFlat: %s (page %d)\n", apiURL, page+1)

		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.SetBasicAuth(c.config.Username, c.config.Password)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("search failed with status %d", resp.StatusCode)
		}

		var result struct {
			Items []struct {
				Repository string `json:"repository"`
				Assets     []struct {
					Path         string `json:"path"`
					Repository   string `json:"repository"`
					DownloadURL  string `json:"downloadUrl"`
					LastModified string `json:"lastModified"`
					Uploader     string `json:"uploader"`
				} `json:"assets"`
			} `json:"items"`
			ContinuationToken string `json:"continuationToken"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("failed to decode: %w", err)
		}
		resp.Body.Close()
		cancel()

		for _, comp := range result.Items {
			repo := comp.Repository
			for _, asset := range comp.Assets {
				assetRepo := asset.Repository
				if assetRepo == "" {
					assetRepo = repo
				}
				if !allRepos && repository != "" && !strings.EqualFold(assetRepo, repository) {
					continue // defesa em profundidade, mesmo padrão de BrowseRepository
				}
				lastMod, _ := time.Parse(time.RFC3339, asset.LastModified)
				artifacts = append(artifacts, FlatArtifact{
					Name:         asset.Path,
					Repository:   assetRepo,
					DownloadURL:  asset.DownloadURL,
					LastModified: lastMod,
					Uploader:     asset.Uploader,
				})
			}
		}

		if result.ContinuationToken == "" {
			break
		}
		continuationToken = result.ContinuationToken
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].LastModified.After(artifacts[j].LastModified)
	})

	fmt.Printf("[Nexus] SearchFlat found %d artifacts (query='%s', repository='%s', allRepos=%v)\n", len(artifacts), query, repository, allRepos)

	return artifacts, nil
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
