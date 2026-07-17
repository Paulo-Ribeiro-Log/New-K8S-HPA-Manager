package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
)

// Preview busca uma amostra dos dados REAIS de uma tabela/collection/chave específica — só
// leitura, sempre com um teto de linhas/documentos (nunca um dump completo). Diferente do estágio
// Browse do Run (metadados/estatísticas via catálogo), isso roda uma query de verdade contra o
// objeto escolhido: SELECT ... LIMIT N (Postgres/MySQL), find().limit(N) (Mongo) ou o comando
// certo pro TYPE da chave (Redis). Síncrono, sem SSE — mesmo padrão de ListTopics/TopicsOverview
// do Teste de Kafka: uma consulta pontual, não precisa de progresso incremental.
// POST /api/v1/db-test/preview
func (h *DBTestHandler) Preview(c *gin.Context) {
	var req DBPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	engine, ok := validateDBTestRequest(c, &req.RunDBTestRequest)
	if !ok {
		return
	}

	req.Object = strings.TrimSpace(req.Object)
	if req.Object == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_OBJECT", "object é obrigatório"))
		return
	}
	if !dbTestObjectNameRegex.MatchString(req.Object) {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_OBJECT", "object contém caracteres não permitidos — só letras, dígitos, _, . e -"))
		return
	}
	if engine.buildPreview == nil {
		c.JSON(http.StatusBadRequest, errorResponse("PREVIEW_NOT_SUPPORTED", "amostra de dados não suportada para este engine"))
		return
	}

	req.Database = strings.TrimSpace(req.Database)
	if req.Limit <= 0 {
		req.Limit = dbTestPreviewDefaultLimit
	}
	if req.Limit > dbTestPreviewMaxLimit {
		req.Limit = dbTestPreviewMaxLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	req.SortColumn = strings.TrimSpace(req.SortColumn)
	if req.SortColumn != "" && !dbTestObjectNameRegex.MatchString(req.SortColumn) {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_SORT_COLUMN", "sort_column contém caracteres não permitidos — só letras, dígitos, _, . e -"))
		return
	}
	req.SortDir = strings.ToLower(strings.TrimSpace(req.SortDir))
	if req.SortDir == "" {
		req.SortDir = "asc"
	}
	if req.SortDir != "asc" && req.SortDir != "desc" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_SORT_DIR", "sort_dir deve ser asc ou desc"))
		return
	}

	// Mesmo piso generoso do estágio Browse (ver dbTestBrowseMinTimeoutMs) — uma consulta real de
	// dados contra um banco remoto tem o mesmo perfil de custo (ou pior, já que lê linhas de
	// verdade em vez de só metadados).
	effectiveTimeoutMs := req.TimeoutMs
	if effectiveTimeoutMs < dbTestBrowseMinTimeoutMs {
		effectiveTimeoutMs = dbTestBrowseMinTimeoutMs
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(),
		dbTestEphemeralReadyTimeout+time.Duration(effectiveTimeoutMs)*time.Millisecond+5*time.Second)
	defer cancel()

	var clientset kubernetes.Interface
	if req.Cluster != "" {
		var err error
		clientset, err = h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
	}

	conn := dbConnParams{
		Mode:          req.Auth.Mode,
		Host:          req.Host,
		Port:          req.Port,
		Database:      req.Database,
		ConnStr:       req.Auth.ConnectionString,
		UseTLS:        req.Auth.UseTLS,
		SkipTLSVerify: req.Auth.SkipTLSVerify,
		AuthMechanism: req.Auth.AuthMechanism,
	}
	if req.Auth.Mode == "userpass" {
		username, password, err := resolveDBCredentials(ctx, clientset, &req.Auth)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("CREDENTIALS_ERROR", err.Error()))
			return
		}
		conn.Username = username
		conn.Password = password
	}
	if req.Auth.Mode == "connstring" && req.Auth.ConnStringRef != nil {
		connStr, err := resolveDBConnString(ctx, clientset, &req.Auth)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("CREDENTIALS_ERROR", err.Error()))
			return
		}
		conn.ConnStr = connStr
	}
	if req.HostConfigMapRef != nil {
		host, port, err := resolveDBHostPort(ctx, clientset, req.HostConfigMapRef)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CONFIGMAP_ERROR", err.Error()))
			return
		}
		conn.Host = host
		conn.Port = port
	}
	// effectiveDatabase (fallback pro banco embutido na connection string) não se aplica ao Redis
	// — lá Database é o índice numérico 0-15, não um nome de banco extraível de path de URI da
	// mesma forma (redis-cli já resolve isso sozinho via -u quando Mode é connstring).
	if req.Engine != "redis" {
		conn.Database = effectiveDatabase(conn)
		if conn.Database == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_DATABASE", "database é obrigatório pra visualizar dados (ou já embutido na connection string)"))
			return
		}
	}

	var run dbExecFunc
	if req.ExecutionMode == "local" {
		image := engine.image
		containerName := "k8s-hpa-dbpreview-" + uuid.New().String()
		run = func(ctx context.Context, script string) (string, error) {
			return execLocalDocker(ctx, image, containerName, dbTestDockerLabel, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
		podName, targetContainer, err := resolveRunningPodForDeployment(ctx, clientset, req.Namespace, req.Deployment)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("POD_NOT_FOUND", err.Error()))
			return
		}
		containerName, err := getOrCreateDBEphemeralContainer(ctx, clientset, req.Namespace, podName, targetContainer, engine.image)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("EPHEMERAL_CONTAINER_ERROR", err.Error()))
			return
		}
		if err := waitDBEphemeralContainerRunning(ctx, clientset, req.Namespace, podName, containerName, dbTestEphemeralReadyTimeout); err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("EPHEMERAL_CONTAINER_ERROR", err.Error()))
			return
		}
		ns, pod, container := req.Namespace, podName, containerName
		run = func(ctx context.Context, script string) (string, error) {
			return execCmdInPod(ctx, clientset, restConfig, ns, pod, container, []string{"sh", "-c", script})
		}
	}

	pv := dbPreviewParams{
		Object:     req.Object,
		SortColumn: req.SortColumn,
		SortDir:    req.SortDir,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}
	script := engine.buildPreview(conn, pv, ceilSeconds(effectiveTimeoutMs))
	stdout, err := run(ctx, script)
	// parseRedisPreviewMeta remove a marca interna de TYPE (só existe na saída do Redis — pra
	// Postgres/MySQL/Mongo a regex não casa e devolve stdout inalterado) e calcula HasMore pra
	// list/zset, os únicos tipos Redis com paginação real por índice. Chamado incondicionalmente
	// (inclusive no caminho de erro) pra nunca vazar a marca interna no RawOutput mostrado.
	stdout, redisHasMore := parseRedisPreviewMeta(stdout, req.Limit)
	if err != nil {
		raw := strings.TrimSpace(stdout)
		if raw == "" {
			raw = extractStderr(err)
		}
		c.JSON(http.StatusOK, DBPreviewResponse{Status: "failed", Message: "Falha ao buscar amostra de dados", Offset: req.Offset, Limit: req.Limit, RawOutput: raw})
		return
	}

	resp := DBPreviewResponse{Status: "ok", Offset: req.Offset, Limit: req.Limit, RawOutput: stdout, HasMore: redisHasMore}
	if engine.parsePreviewOutput != nil {
		rows, parseErr := engine.parsePreviewOutput(stdout)
		if parseErr != nil {
			resp.Message = "Amostra obtida, mas não foi possível estruturar a saída — ver saída bruta: " + parseErr.Error()
		} else {
			resp.Rows = rows
			resp.Truncated = len(rows) >= req.Limit
			resp.HasMore = len(rows) >= req.Limit
			resp.Message = fmt.Sprintf("%d linha(s)/documento(s) na amostra (offset %d)", len(rows), req.Offset)
			if len(rows) == 0 {
				resp.Message = "Nenhum dado encontrado (tabela/collection vazia, filtro sem resultado, ou fim da paginação)"
			}
		}
	} else {
		resp.Message = "Amostra obtida — engine não estrutura em linhas, ver saída bruta"
		if req.Offset > 0 || redisHasMore {
			resp.Message += fmt.Sprintf(" (offset %d)", req.Offset)
		}
	}

	c.JSON(http.StatusOK, resp)
}
