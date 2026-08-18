package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/spinnaker"
	"k8s-hpa-manager/internal/storage"
)

// SpinnakerHandler expõe a configuração do usuário (login, URLs, projeto) e a detecção de
// rollback via Spinnaker (Fase 2 do SPINNAKER-INTEGRATION-PLAN.md). deploymentRegistry pode ser
// nil (graceful degradation, mesmo padrão do GitHubReleasesHandler) — sem ele, RolloutStatusBatch
// não tem como saber a versão vigente de cada Deployment, então retorna erro claro.
type SpinnakerHandler struct {
	baseDir            string
	deploymentRegistry *storage.DeploymentRegistry
	logger             *zerolog.Logger
}

// NewSpinnakerHandler cria o handler. baseDir é ~/.k8s-hpa-manager (mesmo diretório do Perfil
// SSO já existente — sso_profile.json e spinnaker_config.json vivem lado a lado).
func NewSpinnakerHandler(baseDir string, registry *storage.DeploymentRegistry, logger *zerolog.Logger) *SpinnakerHandler {
	return &SpinnakerHandler{
		baseDir:            baseDir,
		deploymentRegistry: registry,
		logger:             logger,
	}
}

// GetConfig — GET /api/v1/spinnaker/config. Nunca expõe segredo (não há segredo aqui — a
// senha continua só no Perfil SSO já existente, /api/v1/sso/profile).
func (h *SpinnakerHandler) GetConfig(c *gin.Context) {
	cfg, err := spinnaker.LoadConfig(h.baseDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// SaveConfig — POST /api/v1/spinnaker/config.
func (h *SpinnakerHandler) SaveConfig(c *gin.Context) {
	var cfg spinnaker.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	if err := spinnaker.SaveConfig(h.baseDir, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// ListProjects — GET /api/v1/spinnaker/projects?env=hlg|prd. Login + GET /projects reais
// (seção 10.2 do plano) — sem cache aqui ainda (lista de projetos muda raramente, mas Fase 1
// não tem infra de cache HTTP-level; a Fase 2.5/frontend pode decidir cachear client-side).
func (h *SpinnakerHandler) ListProjects(c *gin.Context) {
	env := c.Query("env")
	cfg, gateURL, err := h.resolveGateForEnv(c.Request.Context(), env)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var projects []spinnaker.Project
	err = spinnaker.WithSession(c.Request.Context(), gateURL, h.baseDir, cfg.EffectiveLoginIdentifier(), func(cl *spinnaker.Client) error {
		var innerErr error
		projects, innerErr = cl.ListProjects(c.Request.Context())
		return innerErr
	})
	if err != nil {
		h.logf("ListProjects falhou (env=%s): %v", env, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "falha ao consultar projetos no Spinnaker: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, projects)
}

// RolloutStatusBatch — GET /api/v1/spinnaker/rollout-status/batch?cluster=&namespace=&env=.
// Endpoint em lote desde o início (seção 9.1/7 do plano) — uma busca de execuções por
// application do projeto configurado, resolvida contra todos os Deployments já coletados no
// DeploymentRegistry pro cluster/namespace informados. Devolve um mapa "appName/namespace" →
// RollbackInfo, pra a lista de Deployments (Fase 3) resolver tudo de uma vez, sem N chamadas.
func (h *SpinnakerHandler) RolloutStatusBatch(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace") // vazio = todos os namespaces do cluster
	env := c.Query("env")

	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if h.deploymentRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deployment Registry indisponível — rode um scan na aba GitHub Releases primeiro"})
		return
	}

	cfg, gateURL, err := h.resolveGateForEnv(c.Request.Context(), env)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cfg.SelectedProject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nenhum projeto Spinnaker selecionado no perfil do usuário"})
		return
	}

	records, err := h.deploymentRegistry.GetAll(cluster, namespace, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "consultar Deployment Registry: " + err.Error()})
		return
	}
	if len(records) == 0 {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	loginIdentifier := cfg.EffectiveLoginIdentifier()

	var applications []string
	var executionsByApp = map[string][]spinnaker.Execution{}
	err = spinnaker.WithSession(ctx, gateURL, h.baseDir, loginIdentifier, func(cl *spinnaker.Client) error {
		projects, err := cl.ListProjects(ctx)
		if err != nil {
			return err
		}
		for _, p := range projects {
			if strings.EqualFold(p.Name, cfg.SelectedProject) {
				applications = p.Config.Applications
				break
			}
		}
		if len(applications) == 0 {
			return nil // projeto sem applications, ou não encontrado — resolvido abaixo
		}
		for _, app := range applications {
			// Limit alto aqui não ajuda — testado ao vivo (Limit 30/150/1000 contra a application
			// real "logreversa", projeto "SRE Logistica"): as 3 chamadas devolveram EXATAMENTE as
			// mesmas 10 execuções, mesma mais antiga (~28 dias atrás). O Gate capa a busca numa
			// janela de tempo própria (não documentada, não exposta como parâmetro em
			// SearchOptions), independente do "limit" pedido — não é uma limitação desta
			// integração. Na prática: só deployments com pelo menos 1 execução registrada nessa
			// janela aparecem com dado real; os demais (a maioria, numa application/squad que não
			// redeploya toda hora) legitimamente não têm badge — "matched: false" é o resultado
			// correto pra esse caso, não um bug. Mantido em 30 (mesmo custo, sem ganho medido acima
			// disso).
			execs, err := cl.SearchExecutions(ctx, app, spinnaker.SearchOptions{Limit: 30})
			if err != nil {
				h.logf("SearchExecutions falhou pra application %s: %v", app, err)
				continue // uma application falhando não deve derrubar o lote inteiro
			}
			executionsByApp[app] = execs
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "falha ao consultar o Spinnaker: " + err.Error()})
		return
	}
	if len(applications) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "projeto '" + cfg.SelectedProject + "' não encontrado (ou sem applications) no Spinnaker"})
		return
	}

	// junta as execuções de todas as applications do projeto — DetectRollback filtra por
	// nameApp/namespace internamente, então não precisa saber de qual application veio cada uma.
	var allExecutions []spinnaker.Execution
	for _, execs := range executionsByApp {
		allExecutions = append(allExecutions, execs...)
	}

	deckURL, _ := cfg.DeckURLForEnv(env)
	result := make(map[string]*spinnaker.RollbackInfo, len(records))
	for _, rec := range records {
		// ImageTag primeiro, não Version — achado real: DeploymentRecord.Version
		// (app.kubernetes.io/version) sai sanitizado por alguns Helm charts (ex: convair-helm,
		// usado pelas apps da squad Reversa/Dat) trocando "." por "-" (ex: "0.0.2-6" vira
		// "0-0-2-6"), enquanto o Spinnaker sempre usa o formato com ponto real
		// (trigger.Parameters["Application Version"], confirmado ao vivo). Com Version primeiro,
		// a comparação de versão em DetectRollback nunca batia pra nenhum deployment dessas
		// squads — o badge nunca aparecia em lugar nenhum, silenciosamente (Matched sempre
		// false). ImageTag é extraído direto da imagem do container, sem essa sanitização.
		version := rec.ImageTag
		if version == "" {
			version = rec.Version
		}
		// DeploymentName, não AppName — 2º achado real do mesmo chart convair-helm: pelo menos
		// 2 deployments reais (dat-documento-vendas-api, ms-dat-satex-core) têm
		// app.kubernetes.io/name literalmente igual ao nome do CHART ("convair-helm"), não ao
		// nome do próprio microsserviço — o chart nunca sobrescreve esse label com um valor
		// por-instância. Com AppName, dois deployments diferentes colidiam na mesma chave de
		// resultado ("convair-helm/dat-prd", um sobrescrevendo o outro) e nunca batiam contra
		// trigger.AppName() do Spinnaker (que sempre reporta o nome real do nameApp, confirmado
		// ao vivo: "dat-documento-vendas-api"). DeploymentName é o nome do objeto Deployment no
		// K8s — sempre único, sempre confiável, e é exatamente o valor que o Spinnaker usa.
		info := spinnaker.DetectRollback(allExecutions, rec.DeploymentName, rec.Namespace, version)
		if info.Matched && info.SpinnakerExecutionID != "" {
			info.SpinnakerExecutionURL = buildExecutionURL(deckURL, cfg.SelectedProject, applicationForExecution(executionsByApp, info.SpinnakerExecutionID), info.SpinnakerExecutionID)
		}
		result[rec.DeploymentName+"/"+rec.Namespace] = info
	}

	c.JSON(http.StatusOK, result)
}

// applicationForExecution acha de qual application veio um executionID — só usado pra montar o
// deep-link do Deck, que exige o nome da application na URL.
func applicationForExecution(byApp map[string][]spinnaker.Execution, executionID string) string {
	for app, execs := range byApp {
		for _, ex := range execs {
			if ex.ID == executionID {
				return app
			}
		}
	}
	return ""
}

// buildExecutionURL monta o link direto pra execução no Deck, mesmo formato da URL que o
// usuário passou durante a investigação: {deck}/#/projects/{project}/applications/{app}/executions/{id}
func buildExecutionURL(deckURL, project, application, executionID string) string {
	if deckURL == "" || application == "" || executionID == "" {
		return ""
	}
	deckURL = strings.TrimRight(deckURL, "/")
	return deckURL + "/#/projects/" + url.PathEscape(project) + "/applications/" + url.PathEscape(application) + "/executions/" + url.PathEscape(executionID)
}

// resolveGateForEnv carrega a config do usuário e resolve a URL real do Gate (via settings.js
// do Deck) pro ambiente pedido.
func (h *SpinnakerHandler) resolveGateForEnv(ctx context.Context, env string) (spinnaker.Config, string, error) {
	cfg, err := spinnaker.LoadConfig(h.baseDir)
	if err != nil {
		return spinnaker.Config{}, "", err
	}
	deckURL, err := cfg.DeckURLForEnv(env)
	if err != nil {
		return spinnaker.Config{}, "", err
	}
	gateURL, err := spinnaker.ResolveGateURL(ctx, nil, deckURL)
	if err != nil {
		return spinnaker.Config{}, "", err
	}
	return cfg, gateURL, nil
}

func (h *SpinnakerHandler) logf(format string, args ...interface{}) {
	if h.logger == nil {
		return
	}
	h.logger.Warn().Msgf(format, args...)
}
