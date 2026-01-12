package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v57/github"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v3"

	"k8s-hpa-manager/internal/storage"
)

// GitHubReleasesHandler lida com comparação de releases do GitHub
type GitHubReleasesHandler struct {
	deploymentRegistry *storage.DeploymentRegistry
	tokenStore         *storage.GitHubTokenStore
	logger             *zerolog.Logger
}

// NewGitHubReleasesHandler cria novo handler
func NewGitHubReleasesHandler(registry *storage.DeploymentRegistry, tokenStore *storage.GitHubTokenStore, logger *zerolog.Logger) *GitHubReleasesHandler {
	return &GitHubReleasesHandler{
		deploymentRegistry: registry,
		tokenStore:         tokenStore,
		logger:             logger,
	}
}

// GitHubRepoInfo representa informações do repositório GitHub
type GitHubRepoInfo struct {
	Owner string `yaml:"owner" json:"owner"`
	Repo  string `yaml:"repo" json:"repo"`
}

// DeploymentConfig representa um deployment configurado
type DeploymentConfig struct {
	Name           string         `yaml:"name" json:"name"`
	AppName        string         `yaml:"app_name" json:"app_name"`
	GitHubRepo     GitHubRepoInfo `yaml:"github_repo" json:"github_repo"`
	Version        string         `yaml:"version" json:"version"`
	LastPublished  string         `yaml:"last_published" json:"last_published"`
	Squad          string         `yaml:"squad" json:"squad"`
	ServiceNowTask string         `yaml:"servicenow_task" json:"servicenow_task"`
	Age            string         `yaml:"age" json:"age"`
}

// NamespaceConfig representa um namespace configurado
type NamespaceConfig struct {
	Name        string             `yaml:"name" json:"name"`
	Deployments []DeploymentConfig `yaml:"deployments" json:"deployments"`
}

// ClusterConfig representa um cluster configurado
type ClusterConfig struct {
	Name       string            `yaml:"name" json:"name"`
	Namespaces []NamespaceConfig `yaml:"namespaces" json:"namespaces"`
}

// GitHubReposConfig representa o arquivo de configuração hierárquico
type GitHubReposConfig struct {
	Clusters []ClusterConfig `yaml:"clusters"`
}

// getGitHubClient cria cliente GitHub autenticado
// Prioridade: 1) Token individual do usuário, 2) GITHUB_TOKEN global, 3) Não autenticado
func (h *GitHubReleasesHandler) getGitHubClient(c *gin.Context) *github.Client {
	var token string

	// 1. Tentar usar token individual do usuário
	if h.tokenStore != nil {
		userEmail := c.GetString("user_email")
		if userEmail != "" {
			userToken, err := h.tokenStore.GetToken(userEmail)
			if err == nil && userToken != "" {
				h.logger.Debug().Str("user", userEmail).Msg("Using user's individual GitHub token")
				token = userToken
			} else if err != nil {
				h.logger.Debug().Err(err).Str("user", userEmail).Msg("Failed to get user token, falling back")
			}
		}
	}

	// 2. Fallback para token do ambiente (global)
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
		if token != "" {
			h.logger.Debug().Msg("Using global GITHUB_TOKEN from environment")
		}
	}

	// 3. Usar token se disponível
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		return github.NewClient(tc)
	}

	// 4. Cliente não autenticado (rate limit 60 req/h)
	h.logger.Debug().Msg("Using unauthenticated GitHub client (60 req/h limit)")
	return github.NewClient(nil)
}

// loadReposConfig carrega configuração de repositórios
func (h *GitHubReleasesHandler) loadReposConfig() (*GitHubReposConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".k8s-hpa-manager", "github-repos.yaml")

	// Se não existir, retornar config vazia
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		h.logger.Warn().Str("path", configPath).Msg("GitHub repos config not found; returning empty list")
		return &GitHubReposConfig{Clusters: []ClusterConfig{}}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config GitHubReposConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &config, nil
}

// GetRepos retorna lista hierárquica de repositórios configurados
// GET /api/v1/github/repos
func (h *GitHubReleasesHandler) GetRepos(c *gin.Context) {
	config, err := h.loadReposConfig()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to load repos config")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calcular totais para estatísticas
	totalClusters := len(config.Clusters)
	totalNamespaces := 0
	totalDeployments := 0

	for _, cluster := range config.Clusters {
		totalNamespaces += len(cluster.Namespaces)
		for _, namespace := range cluster.Namespaces {
			totalDeployments += len(namespace.Deployments)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"clusters":          config.Clusters,
		"total_clusters":    totalClusters,
		"total_namespaces":  totalNamespaces,
		"total_deployments": totalDeployments,
	})
}

// GetReleases retorna releases de um repositório
// GET /api/v1/github/repos/:owner/:repo/releases
func (h *GitHubReleasesHandler) GetReleases(c *gin.Context) {
	owner := c.Param("owner")
	repo := c.Param("repo")

	client := h.getGitHubClient(c)

	// Buscar releases
	releases, _, err := client.Repositories.ListReleases(context.Background(), owner, repo, &github.ListOptions{
		PerPage: 50, // Últimos 50 releases
	})

	if err != nil {
		h.logger.Error().Err(err).Str("owner", owner).Str("repo", repo).Msg("Failed to list releases")
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to fetch releases: %v", err)})
		return
	}

	// Simplificar resposta (remover campos desnecessários)
	simpleReleases := make([]gin.H, 0, len(releases))
	for _, rel := range releases {
		simpleReleases = append(simpleReleases, gin.H{
			"tag_name":     rel.GetTagName(),
			"name":         rel.GetName(),
			"body":         rel.GetBody(),
			"created_at":   rel.GetCreatedAt(),
			"published_at": rel.GetPublishedAt(),
			"prerelease":   rel.GetPrerelease(),
			"draft":        rel.GetDraft(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"owner":    owner,
		"repo":     repo,
		"releases": simpleReleases,
		"total":    len(simpleReleases),
	})
}

// CompareReleases compara duas releases (tags)
// GET /api/v1/github/repos/:owner/:repo/compare/:base...:head
func (h *GitHubReleasesHandler) CompareReleases(c *gin.Context) {
	owner := c.Param("owner")
	repo := c.Param("repo")
	basehead := c.Param("basehead") // Formato: "base...head"

	// Parsear base e head
	parts := strings.Split(basehead, "...")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid format; expected 'base...head'"})
		return
	}

	base := parts[0]
	head := parts[1]

	client := h.getGitHubClient(c)

	// Comparar commits
	comparison, _, err := client.Repositories.CompareCommits(context.Background(), owner, repo, base, head, &github.ListOptions{
		PerPage: 250, // Máximo de commits
	})

	if err != nil {
		h.logger.Error().Err(err).Str("owner", owner).Str("repo", repo).Str("base", base).Str("head", head).Msg("Failed to compare releases")
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to compare: %v", err)})
		return
	}

	// Buscar release notes de ambas as tags
	baseRelease, _, _ := client.Repositories.GetReleaseByTag(context.Background(), owner, repo, base)
	headRelease, _, _ := client.Repositories.GetReleaseByTag(context.Background(), owner, repo, head)

	baseReleaseNotes := ""
	if baseRelease != nil {
		baseReleaseNotes = baseRelease.GetBody()
	}

	headReleaseNotes := ""
	if headRelease != nil {
		headReleaseNotes = headRelease.GetBody()
	}

	// Simplificar commits
	simpleCommits := make([]gin.H, 0, len(comparison.Commits))
	for _, commit := range comparison.Commits {
		author := "unknown"
		if commit.Commit != nil && commit.Commit.Author != nil {
			author = commit.Commit.Author.GetName()
		}

		message := ""
		if commit.Commit != nil {
			message = commit.Commit.GetMessage()
		}

		date := ""
		if commit.Commit != nil && commit.Commit.Author != nil {
			date = commit.Commit.Author.GetDate().String()
		}

		url := commit.GetHTMLURL()

		simpleCommits = append(simpleCommits, gin.H{
			"sha":     commit.GetSHA(),
			"message": message,
			"author":  author,
			"date":    date,
			"url":     url,
		})
	}

	// Simplificar arquivos alterados
	simpleFiles := make([]gin.H, 0, len(comparison.Files))
	for _, file := range comparison.Files {
		// Extrair extensão
		extension := filepath.Ext(file.GetFilename())
		if extension == "" && strings.Contains(file.GetFilename(), "Dockerfile") {
			extension = "Dockerfile"
		}

		simpleFiles = append(simpleFiles, gin.H{
			"filename":  file.GetFilename(),
			"status":    file.GetStatus(),
			"additions": file.GetAdditions(),
			"deletions": file.GetDeletions(),
			"extension": extension,
			"patch":     file.GetPatch(), // Diff completo (para análise de IA)
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"base_tag":           base,
		"head_tag":           head,
		"commits":            simpleCommits,
		"files_changed":      simpleFiles,
		"ahead_by":           comparison.GetAheadBy(),
		"behind_by":          comparison.GetBehindBy(),
		"base_release_notes": baseReleaseNotes,
		"head_release_notes": headReleaseNotes,
	})
}

// SearchDeployments busca deployments na base de conhecimento por app name
// GET /api/v1/github/deployments/search?app_name=X
func (h *GitHubReleasesHandler) SearchDeployments(c *gin.Context) {
	appName := c.Query("app_name")

	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing app_name parameter"})
		return
	}

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	records, err := h.deploymentRegistry.SearchByAppName(appName)
	if err != nil {
		h.logger.Error().Err(err).Str("app_name", appName).Msg("Failed to search deployments")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"app_name":    appName,
		"deployments": records,
		"total":       len(records),
	})
}

// GetProductionDeployment detecta versão em produção baseado no app name
// GET /api/v1/github/deployments/production?app_name=X
func (h *GitHubReleasesHandler) GetProductionDeployment(c *gin.Context) {
	appName := c.Query("app_name")

	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing app_name parameter"})
		return
	}

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	record, err := h.deploymentRegistry.GetProductionVersion(appName)
	if err != nil {
		// Não encontrado não é erro 500, é 404
		if strings.Contains(err.Error(), "deployment não encontrado") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		h.logger.Error().Err(err).Str("app_name", appName).Msg("Failed to get production deployment")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": record.DeploymentName,
		"namespace":  record.Namespace,
		"cluster":    record.Cluster,
		"version":    record.Version,
		"image":      record.FullImage,
		"status":     record.Status,
		"last_seen":  record.LastSeen,
	})
}

// GetAllVersions retorna mapa de versões de um app
// GET /api/v1/github/deployments/all-versions?app_name=X
func (h *GitHubReleasesHandler) GetAllVersions(c *gin.Context) {
	appName := c.Query("app_name")

	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing app_name parameter"})
		return
	}

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	versionMap, err := h.deploymentRegistry.GetAllVersions(appName)
	if err != nil {
		h.logger.Error().Err(err).Str("app_name", appName).Msg("Failed to get all versions")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Contar total de deployments
	totalDeployments := 0
	for _, deployments := range versionMap {
		totalDeployments += len(deployments)
	}

	c.JSON(http.StatusOK, gin.H{
		"app_name":          appName,
		"versions":          versionMap,
		"total_versions":    len(versionMap),
		"total_deployments": totalDeployments,
	})
}

// GetDeploymentsRegistry retorna todos os deployments do registry
// GET /api/v1/github/deployments/registry?cluster=X&namespace=Y&only_valid_versions=true
func (h *GitHubReleasesHandler) GetDeploymentsRegistry(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	onlyValidVersions := c.Query("only_valid_versions") == "true"

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	records, err := h.deploymentRegistry.GetAll(cluster, namespace, onlyValidVersions)
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("cluster", cluster).
			Str("namespace", namespace).
			Bool("only_valid_versions", onlyValidVersions).
			Msg("Failed to get deployments from registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calcular idade (age) para cada deployment
	now := time.Now()
	type DeploymentWithAge struct {
		storage.DeploymentRecord
		Age string `json:"age"`
	}

	enrichedRecords := make([]DeploymentWithAge, 0, len(records))
	for _, record := range records {
		age := calculateAge(record.LastSeen, now)
		enrichedRecords = append(enrichedRecords, DeploymentWithAge{
			DeploymentRecord: record,
			Age:              age,
		})
	}

	// Estatísticas
	validVersions := 0
	for _, r := range records {
		if isValidSemver(r.Version) {
			validVersions++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"deployments":           enrichedRecords,
		"total":                 len(records),
		"valid_versions":        validVersions,
		"invalid_versions":      len(records) - validVersions,
		"filters_applied": gin.H{
			"cluster":             cluster,
			"namespace":           namespace,
			"only_valid_versions": onlyValidVersions,
		},
	})
}

// calculateAge calcula idade humana legível
func calculateAge(lastSeen time.Time, now time.Time) string {
	if lastSeen.IsZero() {
		return "unknown"
	}

	duration := now.Sub(lastSeen)

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	return fmt.Sprintf("%dm", minutes)
}

// isValidSemver verifica se versão segue padrão semver (x.x.x ou x.x.x-x)
func isValidSemver(version string) bool {
	if version == "" {
		return false
	}

	// Remover prefixo "v" se existir
	version = strings.TrimPrefix(version, "v")

	// Padrão: dígitos.dígitos.dígitos ou dígitos.dígitos.dígitos-dígitos
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}

	// Verificar se os dois primeiros são números
	for i := 0; i < 2; i++ {
		if _, err := fmt.Sscanf(parts[i], "%d", new(int)); err != nil {
			return false
		}
	}

	// Terceiro pode ter -X no final (ex: 5-2)
	lastPart := parts[2]
	if strings.Contains(lastPart, "-") {
		subParts := strings.Split(lastPart, "-")
		if len(subParts) != 2 {
			return false
		}
		// Validar ambas as partes
		for _, p := range subParts {
			if _, err := fmt.Sscanf(p, "%d", new(int)); err != nil {
				return false
			}
		}
	} else {
		// Apenas número
		if _, err := fmt.Sscanf(lastPart, "%d", new(int)); err != nil {
			return false
		}
	}

	return true
}

// CompareReleasesWithRegistry busca versão em produção e compara com nova tag no GitHub
// GET /api/v1/github/compare?release=vv-retira-geolocalizacao&new_tag=2.5.8-1
func (h *GitHubReleasesHandler) CompareReleasesWithRegistry(c *gin.Context) {
	releaseName := c.Query("release")
	newTag := c.Query("new_tag")

	if releaseName == "" || newTag == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetros 'release' e 'new_tag' são obrigatórios"})
		return
	}

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	h.logger.Info().
		Str("release", releaseName).
		Str("new_tag", newTag).
		Msg("Comparing GitHub releases")

	// 1. Buscar versão em produção na base de dados
	prodDeployment, err := h.findProductionVersion(releaseName)
	if err != nil {
		h.logger.Error().Err(err).Str("release", releaseName).Msg("Failed to find production version")
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Versão em produção não encontrada para '%s': %v", releaseName, err)})
		return
	}

	h.logger.Info().
		Str("release", releaseName).
		Str("production_version", prodDeployment.Version).
		Str("cluster", prodDeployment.Cluster).
		Msg("Found production version")

	// 2. Montar informações do repositório GitHub
	// Por padrão, assume que o nome da release é o nome do repo em viavarejo-internal
	owner := "viavarejo-internal"
	repo := releaseName

	// 3. Criar cliente GitHub
	githubClient := h.getGitHubClient(c)
	ctx := context.Background()

	// 4. Normalizar versões (converter x-x-x-x para x.x.x-x)
	baseTag := normalizeVersion(prodDeployment.Version)
	headTag := normalizeVersion(newTag)

	h.logger.Debug().
		Str("owner", owner).
		Str("repo", repo).
		Str("base", baseTag).
		Str("base_original", prodDeployment.Version).
		Str("head", headTag).
		Str("head_original", newTag).
		Msg("Fetching comparison from GitHub")

	comparison, _, err := githubClient.Repositories.CompareCommits(
		ctx,
		owner,
		repo,
		baseTag,
		headTag,
		&github.ListOptions{},
	)

	if err != nil {
		h.logger.Error().
			Err(err).
			Str("owner", owner).
			Str("repo", repo).
			Str("base", baseTag).
			Str("head", headTag).
			Msg("Failed to compare commits on GitHub")
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao comparar no GitHub: %v", err)})
		return
	}

	// 5. Extrair commits
	commits := make([]map[string]interface{}, 0, len(comparison.Commits))
	for _, commit := range comparison.Commits {
		author := "unknown"
		if commit.Commit != nil && commit.Commit.Author != nil && commit.Commit.Author.Name != nil {
			author = *commit.Commit.Author.Name
		}

		date := time.Time{}
		if commit.Commit != nil && commit.Commit.Author != nil && commit.Commit.Author.Date != nil {
			date = commit.Commit.Author.Date.Time
		}

		message := ""
		if commit.Commit != nil && commit.Commit.Message != nil {
			message = *commit.Commit.Message
		}

		url := ""
		if commit.HTMLURL != nil {
			url = *commit.HTMLURL
		}

		commits = append(commits, map[string]interface{}{
			"sha":     commit.GetSHA(),
			"message": message,
			"author":  author,
			"date":    date,
			"url":     url,
		})
	}

	// 6. Extrair arquivos alterados
	files := make([]map[string]interface{}, 0, len(comparison.Files))
	for _, file := range comparison.Files {
		files = append(files, map[string]interface{}{
			"filename":  file.GetFilename(),
			"status":    file.GetStatus(),
			"additions": file.GetAdditions(),
			"deletions": file.GetDeletions(),
			"changes":   file.GetChanges(),
			"patch":     file.GetPatch(),
		})
	}

	// 7. Montar URLs
	repoURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	compareURL := fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", owner, repo, baseTag, headTag)

	// 8. Calcular age do deployment em produção
	age := calculateAge(prodDeployment.LastSeen, time.Now())

	// 9. Retornar resultado completo
	c.JSON(http.StatusOK, gin.H{
		"production_deployment": gin.H{
			"id":               prodDeployment.ID,
			"deployment_name":  prodDeployment.DeploymentName,
			"namespace":        prodDeployment.Namespace,
			"cluster":          prodDeployment.Cluster,
			"version":          prodDeployment.Version,
			"squad":            prodDeployment.Squad,
			"servicenow_task":  prodDeployment.ServiceNowTask,
			"last_seen":        prodDeployment.LastSeen,
			"age":              age,
		},
		"base_tag":       baseTag,
		"head_tag":       headTag,
		"commits":        commits,
		"files":          files,
		"total_commits":  len(commits),
		"total_files":    len(files),
		"repository_url": repoURL,
		"compare_url":    compareURL,
	})
}

// findProductionVersion busca deployment em produção na base de dados
// Prioriza clusters com nome contendo "prod" ou "prd"
func (h *GitHubReleasesHandler) findProductionVersion(releaseName string) (*storage.DeploymentRecord, error) {
	// Buscar todos os deployments com esse nome
	records, err := h.deploymentRegistry.SearchByAppName(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to search deployments: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("nenhum deployment encontrado com nome '%s'", releaseName)
	}

	// Priorizar clusters de produção
	for _, record := range records {
		clusterLower := strings.ToLower(record.Cluster)
		if strings.Contains(clusterLower, "prod") || strings.Contains(clusterLower, "prd") {
			return &record, nil
		}
	}

	// Fallback: retornar primeiro encontrado
	h.logger.Warn().
		Str("release", releaseName).
		Msg("No production cluster found, returning first deployment")
	return &records[0], nil
}

// normalizeVersion converte versões com hífen para o padrão GitHub
// Exemplo: "2-5-5-2" → "2.5.5-2" (substitui os 2 primeiros hífens por pontos)
func normalizeVersion(version string) string {
	// Remove prefixo "v" se existir
	version = strings.TrimPrefix(version, "v")

	// Contar hífens
	hyphens := strings.Count(version, "-")

	if hyphens >= 3 {
		// Formato: x-x-x-x → x.x.x-x
		// Substituir os 3 primeiros hífens por pontos
		parts := strings.Split(version, "-")
		if len(parts) >= 4 {
			return fmt.Sprintf("%s.%s.%s-%s", parts[0], parts[1], parts[2], strings.Join(parts[3:], "-"))
		}
	} else if hyphens == 2 {
		// Formato: x-x-x → x.x.x
		version = strings.ReplaceAll(version, "-", ".")
	}

	return version
}
