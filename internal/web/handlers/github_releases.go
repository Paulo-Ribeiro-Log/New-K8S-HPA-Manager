package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	logger             *zerolog.Logger
}

// NewGitHubReleasesHandler cria novo handler
func NewGitHubReleasesHandler(registry *storage.DeploymentRegistry, logger *zerolog.Logger) *GitHubReleasesHandler {
	return &GitHubReleasesHandler{
		deploymentRegistry: registry,
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
func (h *GitHubReleasesHandler) getGitHubClient(c *gin.Context) *github.Client {
	// Tentar usar token do ambiente (fallback)
	token := os.Getenv("GITHUB_TOKEN")

	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		return github.NewClient(tc)
	}

	// Cliente não autenticado (rate limit 60 req/h)
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
