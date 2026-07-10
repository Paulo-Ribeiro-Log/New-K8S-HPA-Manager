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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/storage"
)

// GitHubReleasesHandler lida com comparação de releases do GitHub
type GitHubReleasesHandler struct {
	deploymentRegistry *storage.DeploymentRegistry
	tokenStore         *storage.GitHubTokenStore
	kubeConfigManager  interface {
		GetClient(cluster string) (kubernetes.Interface, error)
	}
	logger *zerolog.Logger
}

// NewGitHubReleasesHandler cria novo handler
func NewGitHubReleasesHandler(registry *storage.DeploymentRegistry, tokenStore *storage.GitHubTokenStore, kubeManager interface {
	GetClient(cluster string) (kubernetes.Interface, error)
}, logger *zerolog.Logger) *GitHubReleasesHandler {
	return &GitHubReleasesHandler{
		deploymentRegistry: registry,
		tokenStore:         tokenStore,
		kubeConfigManager:  kubeManager,
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
func (h *GitHubReleasesHandler) getGitHubClient(c *gin.Context) (*github.Client, error) {
	var token string
	var tokenSource string

	// 1. Tentar usar token individual do usuário
	if h.tokenStore != nil {
		// Prioridade: 1) contexto RBAC (Azure AD), 2) header X-GitHub-Email (fallback)
		userEmail := c.GetString("user_email")
		if userEmail == "" {
			userEmail = strings.TrimSpace(c.GetHeader("X-GitHub-Email"))
		}
		h.logger.Debug().Str("github_user_email", userEmail).Msg("🔍 Checking user identity for GitHub token")

		if userEmail != "" {
			h.logger.Info().Str("email", userEmail).Msg("✅ Using user identity for GitHub token")
			userToken, err := h.tokenStore.GetToken(userEmail)
			if err != nil {
				h.logger.Error().Err(err).Str("user", userEmail).Msg("❌ Failed to get user token from store")
				return nil, fmt.Errorf("token não encontrado para o email %s. Configure seu token no modal.", userEmail)
			}
			if userToken == "" {
				h.logger.Error().Str("user", userEmail).Msg("❌ User token is empty")
				return nil, fmt.Errorf("token vazio para o email %s. Configure seu token no modal.", userEmail)
			}

			h.logger.Info().Str("user", userEmail).Msg("✅ Using user's individual GitHub token")
			token = userToken
			tokenSource = fmt.Sprintf("user:%s", userEmail)
		} else {
			h.logger.Debug().Msg("X-GitHub-Email header not present, trying environment token")
		}
	} else {
		h.logger.Debug().Msg("Token store is nil")
	}

	// 2. Fallback para token do ambiente (global)
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
		if token != "" {
			h.logger.Info().Msg("✅ Using global GITHUB_TOKEN from environment")
			tokenSource = "environment"
		}
	}

	// 3. Usar token se disponível
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		client := github.NewClient(tc)
		h.logger.Info().Str("token_source", tokenSource).Msg("GitHub client authenticated")
		return client, nil
	}

	// 4. Cliente não autenticado (rate limit 60 req/h)
	h.logger.Warn().Msg("⚠️ Using unauthenticated GitHub client (60 req/h limit)")
	return github.NewClient(nil), nil
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

// ListUserRepos retorna repositórios acessíveis pelo token do usuário
// GET /api/v1/github/user/repos?org=casas-bahia&search=geolocalizacao
func (h *GitHubReleasesHandler) ListUserRepos(c *gin.Context) {
	org := c.Query("org")
	search := c.Query("search")

	client, err := h.getGitHubClient(c)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get GitHub client")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
			"hint":  "Configure seu token GitHub no modal de configurações",
		})
		return
	}
	ctx := context.Background()

	var allRepos []*github.Repository

	if org != "" {
		// Listar repos da organização
		opts := &github.RepositoryListByOrgOptions{
			ListOptions: github.ListOptions{PerPage: 100},
		}
		repos, _, err := client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			h.logger.Error().Err(err).Str("org", org).Msg("Failed to list org repos")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Falha ao listar repositórios da organização '%s': %v. Verifique se a organização existe e se o token tem acesso.", org, err),
			})
			return
		}
		allRepos = repos
	} else {
		// Listar repos do usuário autenticado
		opts := &github.RepositoryListOptions{
			ListOptions: github.ListOptions{PerPage: 100},
		}
		repos, _, err := client.Repositories.List(ctx, "", opts)
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to list user repos")
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Falha ao listar repositórios: %v", err)})
			return
		}
		allRepos = repos
	}

	// Filtrar por busca se fornecido
	var filteredRepos []gin.H
	for _, repo := range allRepos {
		repoName := repo.GetName()
		repoFullName := repo.GetFullName()

		if search != "" {
			if !strings.Contains(strings.ToLower(repoName), strings.ToLower(search)) {
				continue
			}
		}

		filteredRepos = append(filteredRepos, gin.H{
			"name":      repoName,
			"full_name": repoFullName,
			"owner":     repo.GetOwner().GetLogin(),
			"private":   repo.GetPrivate(),
			"html_url":  repo.GetHTMLURL(),
			"clone_url": repo.GetCloneURL(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"repos":  filteredRepos,
		"total":  len(filteredRepos),
		"org":    org,
		"search": search,
	})
}

// GetReleases retorna releases de um repositório
// GET /api/v1/github/repos/:owner/:repo/releases
func (h *GitHubReleasesHandler) GetReleases(c *gin.Context) {
	owner := c.Param("owner")
	repo := c.Param("repo")

	client, err := h.getGitHubClient(c)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get GitHub client")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
			"hint":  "Configure seu token GitHub no modal de configurações",
		})
		return
	}

	// Buscar releases — 100 por página (máximo da API GitHub)
	releases, _, err := client.Repositories.ListReleases(context.Background(), owner, repo, &github.ListOptions{
		PerPage: 100,
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

	// DEBUG: Log ALL headers
	h.logger.Debug().Msg("=== ALL REQUEST HEADERS ===")
	for key, values := range c.Request.Header {
		for _, value := range values {
			h.logger.Debug().Str("header", key).Str("value", value).Msg("Header received")
		}
	}
	h.logger.Debug().Msg("=== END HEADERS ===")

	// Parsear base e head
	parts := strings.Split(basehead, "...")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid format; expected 'base...head'"})
		return
	}

	base := parts[0]
	head := parts[1]

	h.logger.Info().
		Str("owner", owner).
		Str("repo", repo).
		Str("base", base).
		Str("head", head).
		Msg("Comparing releases")

	client, err := h.getGitHubClient(c)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get GitHub client")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
			"hint":  "Configure seu token GitHub no modal de configurações",
		})
		return
	}

	// Verificar se estamos autenticados e qual usuário
	user, resp, err := client.Users.Get(context.Background(), "")
	if err != nil {
		h.logger.Warn().
			Err(err).
			Msg("Failed to get authenticated user - using unauthenticated client")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Token GitHub inválido ou não configurado. Configure seu token primeiro.",
			"hint":  "Clique no botão 'Token' para configurar seu token GitHub",
		})
		return
	}

	h.logger.Info().
		Str("github_user", user.GetLogin()).
		Int("rate_limit_remaining", resp.Rate.Remaining).
		Msg("Authenticated GitHub request")

	// Construir URL para referência
	githubWebURL := fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", owner, repo, base, head)

	// Testar se o repositório existe e é acessível
	repoInfo, resp, err := client.Repositories.Get(context.Background(), owner, repo)
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("owner", owner).
			Str("repo", repo).
			Int("status_code", resp.StatusCode).
			Str("github_web_url", githubWebURL).
			Msg("Failed to access repository")

		// ✅ Detectar erro SAML específico (403 com mensagem de SAML enforcement)
		errorStr := err.Error()
		if resp.StatusCode == 403 && strings.Contains(errorStr, "SAML") {
			h.logger.Warn().
				Str("owner", owner).
				Str("repo", repo).
				Msg("SAML authorization required for organization")

			c.JSON(http.StatusForbidden, gin.H{
				"error":      "Token válido, mas não autorizado para a organização com SAML SSO",
				"error_type": "saml_authorization_required",
				"message":    fmt.Sprintf("Seu token GitHub está válido, mas precisa ser re-autorizado para a organização '%s' que usa SAML SSO.", owner),
				"instructions": []string{
					"1. Acesse: https://github.com/settings/tokens",
					"2. Encontre seu token e clique em 'Configure SSO'",
					fmt.Sprintf("3. Procure '%s' e clique em 'Authorize'", owner),
					"4. Complete a autenticação SSO",
					"5. Tente novamente a comparação",
				},
				"github_settings_url": "https://github.com/settings/tokens",
				"github_web_url":      githubWebURL,
				"status_code":         resp.StatusCode,
			})
			return
		}

		if resp.StatusCode == 404 {
			// Tentar buscar repositórios do usuário/organização para ajudar no diagnóstico
			var repoSuggestions []string
			listOpts := &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{PerPage: 100},
			}

			repos, _, listErr := client.Repositories.ListByOrg(context.Background(), owner, listOpts)
			if listErr == nil && len(repos) > 0 {
				h.logger.Info().Int("total_repos", len(repos)).Str("org", owner).Msg("Found accessible repos in org")
				// Procurar repos com nomes similares
				for _, r := range repos {
					repoName := r.GetName()
					if strings.Contains(strings.ToLower(repoName), strings.ToLower(repo)) ||
						strings.Contains(strings.ToLower(repo), strings.ToLower(repoName)) {
						repoSuggestions = append(repoSuggestions, repoName)
					}
					if len(repoSuggestions) >= 5 {
						break
					}
				}
			} else if listErr != nil {
				h.logger.Error().Err(listErr).Str("org", owner).Msg("Failed to list org repos")
			}

			errorMsg := fmt.Sprintf("Repositório '%s/%s' não encontrado via API do GitHub.\n", owner, repo)
			errorMsg += fmt.Sprintf("A URL web funciona: %s\n", githubWebURL)
			errorMsg += "\nPossíveis causas:\n"
			errorMsg += "1. Token não tem acesso a este repositório específico\n"
			errorMsg += "2. Token precisa das permissões: 'repo' (full control) ou 'public_repo'\n"
			errorMsg += "3. Nome do repositório pode estar ligeiramente diferente\n"
			errorMsg += fmt.Sprintf("\nVocê pode acessar pelo browser: %s", githubWebURL)

			response := gin.H{
				"error":      errorMsg,
				"error_type": "not_found",
				"details": gin.H{
					"owner":          owner,
					"repo":           repo,
					"github_web_url": githubWebURL,
					"api_url":        fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo),
					"status_code":    resp.StatusCode,
				},
			}

			if len(repoSuggestions) > 0 {
				response["suggestions"] = gin.H{
					"message": fmt.Sprintf("Encontrados %d repositórios acessíveis com nome similar:", len(repoSuggestions)),
					"repos":   repoSuggestions,
				}
			}

			c.JSON(http.StatusNotFound, response)
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":          fmt.Sprintf("Falha ao acessar repositório: %v", err),
			"error_type":     "unknown",
			"status_code":    resp.StatusCode,
			"github_web_url": githubWebURL,
		})
		return
	}

	h.logger.Info().
		Str("repo_full_name", repoInfo.GetFullName()).
		Bool("private", repoInfo.GetPrivate()).
		Msg("Repository accessible")

	// Comparar commits
	comparison, _, err := client.Repositories.CompareCommits(context.Background(), owner, repo, base, head, &github.ListOptions{
		PerPage: 250, // Máximo de commits
	})

	if err != nil {
		h.logger.Error().Err(err).Str("owner", owner).Str("repo", repo).Str("base", base).Str("head", head).Msg("Failed to compare releases")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":              fmt.Sprintf("Falha ao comparar tags '%s' e '%s': %v. Verifique se as tags existem no repositório.", base, head, err),
			"github_compare_url": fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", owner, repo, base, head),
		})
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
		"compare_url":        githubWebURL,
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

	// Normalizar versão para formato x.x.x-x (ex: "2-5-5-2" → "2.5.5-2")
	normalizedVersion := normalizeVersion(record.Version)

	c.JSON(http.StatusOK, gin.H{
		"deployment":      record.DeploymentName,
		"namespace":       record.Namespace,
		"cluster":         record.Cluster,
		"version":         normalizedVersion, // ✅ Versão normalizada (x.x.x-x)
		"image":           record.FullImage,
		"status":          record.Status,
		"created_at":      record.CreatedAt, // ✅ Data de criação real do deployment (sem fallback)
		"last_seen":       record.LastSeen,  // Data do último scan
		"squad":           record.Squad,
		"servicenow_task": record.ServiceNowTask,
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
		"deployments":      enrichedRecords,
		"total":            len(records),
		"valid_versions":   validVersions,
		"invalid_versions": len(records) - validVersions,
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

	// 🔍 DEBUG: Log ALL headers received
	h.logger.Debug().Msg("=== ALL REQUEST HEADERS ===")
	for key, values := range c.Request.Header {
		for _, value := range values {
			h.logger.Debug().Str("header", key).Str("value", value).Msg("Header received")
		}
	}
	h.logger.Debug().Msg("=== END HEADERS ===")

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
	// Owner: query param ?org=X, fallback para "casas-bahia"
	owner := c.DefaultQuery("org", "casas-bahia")
	repo := releaseName

	// 3. Criar cliente GitHub
	githubClient, err := h.getGitHubClient(c)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get GitHub client")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
			"hint":  "Configure seu token GitHub no modal de configurações",
		})
		return
	}
	ctx := context.Background()

	// 4. Normalizar versões (converter x-x-x-x para x.x.x-x)
	baseTag := normalizeVersion(prodDeployment.Version)
	headTag := normalizeVersion(newTag)

	h.logger.Info().
		Str("owner", owner).
		Str("repo", repo).
		Str("base", baseTag).
		Str("base_original", prodDeployment.Version).
		Str("head", headTag).
		Str("head_original", newTag).
		Msg("Comparing GitHub releases")

	// Comparar diretamente — sem pré-validação de tags por lista paginada.
	// A pre-validação via ListTags(PerPage:100) falhava em repos com >100 tags
	// pois tags antigas não apareciam na primeira página.
	comparison, resp, err := githubClient.Repositories.CompareCommits(
		ctx,
		owner,
		repo,
		baseTag,
		headTag,
		&github.ListOptions{},
	)

	if err != nil {
		statusCode := http.StatusInternalServerError
		errorMsg := fmt.Sprintf("Erro ao comparar tags '%s' e '%s': %v", baseTag, headTag, err)

		if resp != nil {
			switch resp.StatusCode {
			case http.StatusNotFound:
				statusCode = http.StatusNotFound
				errorMsg = fmt.Sprintf(
					"Repositório '%s/%s' ou tags '%s'/'%s' não encontradas no GitHub. "+
						"Verifique se as tags existem e se o token tem acesso ao repositório.",
					owner, repo, baseTag, headTag,
				)
			case http.StatusUnprocessableEntity:
				statusCode = http.StatusUnprocessableEntity
				errorMsg = fmt.Sprintf(
					"Tags '%s' ou '%s' não existem no repositório %s/%s.",
					baseTag, headTag, owner, repo,
				)
			case http.StatusForbidden:
				statusCode = http.StatusForbidden
				errorMsg = "Token sem permissão para acessar este repositório. Verifique SSO/SAML."
			}
		}

		h.logger.Error().
			Err(err).
			Str("owner", owner).
			Str("repo", repo).
			Str("base", baseTag).
			Str("head", headTag).
			Msg("Failed to compare commits on GitHub")

		c.JSON(statusCode, gin.H{
			"error":              errorMsg,
			"github_compare_url": fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", owner, repo, baseTag, headTag),
		})
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
			"id":              prodDeployment.ID,
			"deployment_name": prodDeployment.DeploymentName,
			"namespace":       prodDeployment.Namespace,
			"cluster":         prodDeployment.Cluster,
			"version":         prodDeployment.Version,
			"squad":           prodDeployment.Squad,
			"servicenow_task": prodDeployment.ServiceNowTask,
			"last_seen":       prodDeployment.LastSeen,
			"age":             age,
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

// ScanDeployments escaneia deployments de um cluster e popula a base de dados
// POST /api/v1/github/deployments/scan?cluster=X
func (h *GitHubReleasesHandler) ScanDeployments(c *gin.Context) {
	clusterName := c.Query("cluster")

	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetro 'cluster' é obrigatório"})
		return
	}

	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment registry not available"})
		return
	}

	h.logger.Info().Str("cluster", clusterName).Msg("Starting deployment scan")

	ctx := context.Background()

	// 1. Obter cliente Kubernetes para o cluster
	client, err := h.kubeConfigManager.GetClient(clusterName)
	if err != nil {
		h.logger.Error().Err(err).Str("cluster", clusterName).Msg("Failed to get Kubernetes client")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to connect to cluster",
			"details": err.Error(),
		})
		return
	}

	// 2. Listar todos os namespaces do cluster
	namespaceList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		h.logger.Error().Err(err).Str("cluster", clusterName).Msg("Failed to list namespaces")

		// ✅ VERIFICAÇÃO DE VPN: Detectar erros de conectividade (DNS lookup, no such host, timeout)
		errorMsg := err.Error()
		isVPNError := strings.Contains(errorMsg, "no such host") ||
			strings.Contains(errorMsg, "lookup") ||
			strings.Contains(errorMsg, "dial tcp") ||
			strings.Contains(errorMsg, "i/o timeout") ||
			strings.Contains(errorMsg, "connection refused") ||
			strings.Contains(errorMsg, "privatelink")

		if isVPNError {
			h.logger.Warn().
				Str("cluster", clusterName).
				Msg("VPN connection issue detected")

			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "VPN desconectada ou cluster inacessível",
				"details": "Não foi possível resolver o DNS do cluster. Isso geralmente indica que a VPN está desconectada.",
				"cluster": clusterName,
				"suggestions": []string{
					"1. Verifique se você está conectado à VPN corporativa",
					"2. Tente reconectar à VPN",
					"3. Verifique se o cluster está ligado e acessível",
					"4. Execute: kubectl get namespaces --context " + clusterName,
				},
				"error_type":      "vpn_disconnected",
				"technical_error": errorMsg,
			})
			return
		}

		// Outros erros (não relacionados a VPN)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":      "Failed to list namespaces",
			"details":    errorMsg,
			"cluster":    clusterName,
			"error_type": "kubernetes_api_error",
		})
		return
	}

	namespaces := []string{}
	for _, ns := range namespaceList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	h.logger.Info().
		Str("cluster", clusterName).
		Int("namespaces", len(namespaces)).
		Msg("Found namespaces")

	// 3. Escanear todos os tipos de workload em cada namespace
	counts := map[string]int{
		"Deployment":  0,
		"CronJob":     0,
		"StatefulSet": 0,
		"DaemonSet":   0,
	}
	totalSaved := 0
	errors := []string{}

	upsert := func(record storage.DeploymentRecord) {
		if err := h.deploymentRegistry.UpsertDeployment(record); err != nil {
			h.logger.Warn().Err(err).
				Str("cluster", clusterName).
				Str("kind", record.ResourceKind).
				Str("name", record.DeploymentName).
				Msg("Failed to save workload to registry")
			errors = append(errors, fmt.Sprintf("%s/%s/%s: %v", record.ResourceKind, record.Namespace, record.DeploymentName, err))
		} else {
			totalSaved++
		}
	}

	for _, namespace := range namespaces {
		// Deployments
		if deploymentList, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			counts["Deployment"] += len(deploymentList.Items)
			for i := range deploymentList.Items {
				upsert(h.extractDeploymentRecord(&deploymentList.Items[i], clusterName))
			}
		} else {
			errors = append(errors, fmt.Sprintf("Deployment/%s: %v", namespace, err))
		}

		// CronJobs
		if cronList, err := client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			counts["CronJob"] += len(cronList.Items)
			for i := range cronList.Items {
				upsert(h.extractCronJobRecord(&cronList.Items[i], clusterName))
			}
		} else {
			errors = append(errors, fmt.Sprintf("CronJob/%s: %v", namespace, err))
		}

		// StatefulSets
		if ssList, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			counts["StatefulSet"] += len(ssList.Items)
			for i := range ssList.Items {
				upsert(h.extractStatefulSetRecord(&ssList.Items[i], clusterName))
			}
		} else {
			errors = append(errors, fmt.Sprintf("StatefulSet/%s: %v", namespace, err))
		}

		// DaemonSets
		if dsList, err := client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			counts["DaemonSet"] += len(dsList.Items)
			for i := range dsList.Items {
				upsert(h.extractDaemonSetRecord(&dsList.Items[i], clusterName))
			}
		} else {
			errors = append(errors, fmt.Sprintf("DaemonSet/%s: %v", namespace, err))
		}
	}

	totalFound := counts["Deployment"] + counts["CronJob"] + counts["StatefulSet"] + counts["DaemonSet"]

	h.logger.Info().
		Str("cluster", clusterName).
		Int("found", totalFound).
		Int("saved", totalSaved).
		Int("errors", len(errors)).
		Msg("Workload scan completed")

	response := gin.H{
		"cluster":            clusterName,
		"namespaces_scanned": len(namespaces),
		"deployments_found":  counts["Deployment"],
		"cronjobs_found":     counts["CronJob"],
		"statefulsets_found": counts["StatefulSet"],
		"daemonsets_found":   counts["DaemonSet"],
		"total_found":        totalFound,
		"total_saved":        totalSaved,
		"status":             "success",
	}

	if len(errors) > 0 {
		response["errors"] = errors
		response["status"] = "partial_success"
	}

	c.JSON(http.StatusOK, response)
}

// extractDeploymentRecord extrai informações do deployment para criar um registro
// Baseado na lógica de internal/healthcheck/deployment_checker.go:populateDeploymentRegistry
func (h *GitHubReleasesHandler) extractDeploymentRecord(deployment *appsv1.Deployment, clusterName string) storage.DeploymentRecord {
	// Extrair versão de labels
	version := deployment.Labels["app.kubernetes.io/version"]

	// Extrair app name de labels
	appName := deployment.Labels["app.kubernetes.io/name"]
	if appName == "" {
		appName = deployment.Labels["app"]
	}
	if appName == "" {
		appName = deployment.Name // Fallback para nome do deployment
	}

	// Extrair squad do label devops.k8s.io/squad (busca em labels e annotations)
	squad := deployment.Labels["devops.k8s.io/squad"]
	if squad == "" && deployment.Annotations != nil {
		squad = deployment.Annotations["devops.k8s.io/squad"]
	}

	// Extrair ServiceNow task number (busca em labels e annotations)
	// Formato esperado: "CHG0001234" ou apenas "0001234"
	servicenowTask := deployment.Labels["devops.k8s.io/servicenow-task-number"]
	if servicenowTask == "" && deployment.Annotations != nil {
		servicenowTask = deployment.Annotations["devops.k8s.io/servicenow-task-number"]
	}
	// Garantir prefixo CHG se não estiver presente
	if servicenowTask != "" && !strings.HasPrefix(servicenowTask, "CHG") {
		servicenowTask = "CHG" + servicenowTask
	}

	// Extrair imagem e tag do primeiro container
	var fullImage, imageTag string
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		fullImage = deployment.Spec.Template.Spec.Containers[0].Image
		imageTag = storage.ExtractVersionFromImage(fullImage)

		// Se não tem version label, usar tag da imagem
		if version == "" {
			version = imageTag
		}
	}

	// Determinar status de saúde básico
	status := "healthy"
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	readyReplicas := deployment.Status.ReadyReplicas

	if readyReplicas == 0 && desiredReplicas > 0 {
		status = "critical"
	} else if readyReplicas < desiredReplicas {
		status = "warning"
	}

	// Criar registro
	return storage.DeploymentRecord{
		DeploymentName:  deployment.Name,
		Namespace:       deployment.Namespace,
		Cluster:         clusterName,
		Version:         version,
		ImageTag:        imageTag,
		FullImage:       fullImage,
		ReplicasCurrent: int(readyReplicas),
		ReplicasDesired: int(desiredReplicas),
		AppName:         appName,
		Status:          status,
		Squad:           squad,
		ServiceNowTask:  servicenowTask,
		ResourceKind:    "Deployment",
		CreatedAt:       deployment.CreationTimestamp.Time,
	}
}

// extractLabelsMetadata extrai squad, servicenow task e app name de labels/annotations
func extractLabelsMetadata(labels, annotations map[string]string, fallbackName string) (appName, squad, servicenowTask string) {
	appName = labels["app.kubernetes.io/name"]
	if appName == "" {
		appName = labels["app"]
	}
	if appName == "" {
		appName = fallbackName
	}

	squad = labels["devops.k8s.io/squad"]
	if squad == "" {
		squad = annotations["devops.k8s.io/squad"]
	}

	servicenowTask = labels["devops.k8s.io/servicenow-task-number"]
	if servicenowTask == "" {
		servicenowTask = annotations["devops.k8s.io/servicenow-task-number"]
	}
	if servicenowTask != "" && !strings.HasPrefix(servicenowTask, "CHG") {
		servicenowTask = "CHG" + servicenowTask
	}
	return
}

// extractFirstContainerImage extrai imagem e tag do primeiro container de uma lista
func extractFirstContainerImage(containers []corev1.Container) (fullImage, imageTag string) {
	if len(containers) > 0 {
		fullImage = containers[0].Image
		imageTag = storage.ExtractVersionFromImage(fullImage)
	}
	return
}

// extractCronJobRecord extrai um DeploymentRecord a partir de um CronJob
func (h *GitHubReleasesHandler) extractCronJobRecord(cj *batchv1.CronJob, clusterName string) storage.DeploymentRecord {
	labels := cj.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := cj.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	version := labels["app.kubernetes.io/version"]
	appName, squad, servicenowTask := extractLabelsMetadata(labels, annotations, cj.Name)
	fullImage, imageTag := extractFirstContainerImage(cj.Spec.JobTemplate.Spec.Template.Spec.Containers)
	if version == "" {
		version = imageTag
	}

	return storage.DeploymentRecord{
		DeploymentName: cj.Name,
		Namespace:      cj.Namespace,
		Cluster:        clusterName,
		Version:        version,
		ImageTag:       imageTag,
		FullImage:      fullImage,
		AppName:        appName,
		Status:         "healthy",
		Squad:          squad,
		ServiceNowTask: servicenowTask,
		ResourceKind:   "CronJob",
		CreatedAt:      cj.CreationTimestamp.Time,
	}
}

// extractStatefulSetRecord extrai um DeploymentRecord a partir de um StatefulSet
func (h *GitHubReleasesHandler) extractStatefulSetRecord(ss *appsv1.StatefulSet, clusterName string) storage.DeploymentRecord {
	labels := ss.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := ss.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	version := labels["app.kubernetes.io/version"]
	appName, squad, servicenowTask := extractLabelsMetadata(labels, annotations, ss.Name)
	fullImage, imageTag := extractFirstContainerImage(ss.Spec.Template.Spec.Containers)
	if version == "" {
		version = imageTag
	}

	desiredReplicas := int32(1)
	if ss.Spec.Replicas != nil {
		desiredReplicas = *ss.Spec.Replicas
	}
	readyReplicas := ss.Status.ReadyReplicas

	status := "healthy"
	if readyReplicas == 0 && desiredReplicas > 0 {
		status = "critical"
	} else if readyReplicas < desiredReplicas {
		status = "warning"
	}

	return storage.DeploymentRecord{
		DeploymentName:  ss.Name,
		Namespace:       ss.Namespace,
		Cluster:         clusterName,
		Version:         version,
		ImageTag:        imageTag,
		FullImage:       fullImage,
		ReplicasCurrent: int(readyReplicas),
		ReplicasDesired: int(desiredReplicas),
		AppName:         appName,
		Status:          status,
		Squad:           squad,
		ServiceNowTask:  servicenowTask,
		ResourceKind:    "StatefulSet",
		CreatedAt:       ss.CreationTimestamp.Time,
	}
}

// extractDaemonSetRecord extrai um DeploymentRecord a partir de um DaemonSet
func (h *GitHubReleasesHandler) extractDaemonSetRecord(ds *appsv1.DaemonSet, clusterName string) storage.DeploymentRecord {
	labels := ds.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := ds.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	version := labels["app.kubernetes.io/version"]
	appName, squad, servicenowTask := extractLabelsMetadata(labels, annotations, ds.Name)
	fullImage, imageTag := extractFirstContainerImage(ds.Spec.Template.Spec.Containers)
	if version == "" {
		version = imageTag
	}

	desiredReplicas := ds.Status.DesiredNumberScheduled
	readyReplicas := ds.Status.NumberReady

	status := "healthy"
	if readyReplicas == 0 && desiredReplicas > 0 {
		status = "critical"
	} else if readyReplicas < desiredReplicas {
		status = "warning"
	}

	return storage.DeploymentRecord{
		DeploymentName:  ds.Name,
		Namespace:       ds.Namespace,
		Cluster:         clusterName,
		Version:         version,
		ImageTag:        imageTag,
		FullImage:       fullImage,
		ReplicasCurrent: int(readyReplicas),
		ReplicasDesired: int(desiredReplicas),
		AppName:         appName,
		Status:          status,
		Squad:           squad,
		ServiceNowTask:  servicenowTask,
		ResourceKind:    "DaemonSet",
		CreatedAt:       ds.CreationTimestamp.Time,
	}
}

// CommitFile faz commit de um arquivo em um repositório GitHub.
// Cria o arquivo se não existir; atualiza se já existir (busca SHA automaticamente).
// POST /api/v1/github/commit-file
func (h *GitHubReleasesHandler) CommitFile(c *gin.Context) {
	var req struct {
		// Destino resolvido a partir da URL de pasta colada pelo usuário
		Owner    string `json:"owner"`
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		BasePath string `json:"base_path"` // caminho da pasta, ex: "jobs/producao"
		Filename string `json:"filename"`  // nome do arquivo, ex: "relatorio-job.yaml"
		Content  string `json:"content"`
		Message  string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Owner == "" || req.Repo == "" || req.Filename == "" || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner, repo, filename e content são obrigatórios"})
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	// Montar caminho completo
	filePath := req.Filename
	if req.BasePath != "" {
		filePath = strings.TrimRight(req.BasePath, "/") + "/" + req.Filename
	}

	if req.Message == "" {
		req.Message = fmt.Sprintf("chore: adiciona %s", filePath)
	}

	client, err := h.getGitHubClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
			"hint":  "Configure seu token GitHub no modal de configurações",
		})
		return
	}

	ctx := context.Background()

	// Buscar SHA do arquivo existente (necessário para update)
	var existingSHA *string
	existing, _, _, getErr := client.Repositories.GetContents(ctx, req.Owner, req.Repo, filePath,
		&github.RepositoryContentGetOptions{Ref: req.Branch})
	if getErr == nil && existing != nil {
		existingSHA = existing.SHA
	}

	opts := &github.RepositoryContentFileOptions{
		Message: github.String(req.Message),
		Content: []byte(req.Content),
		Branch:  github.String(req.Branch),
		SHA:     existingSHA,
	}

	result, _, err := client.Repositories.CreateFile(ctx, req.Owner, req.Repo, filePath, opts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("erro ao commitar: %v", err)})
		return
	}

	fileURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", req.Owner, req.Repo, req.Branch, filePath)
	commitURL := ""
	if result.Commit.HTMLURL != nil {
		commitURL = *result.Commit.HTMLURL
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"file_url":   fileURL,
		"commit_url": commitURL,
		"created":    existingSHA == nil,
	})
}
