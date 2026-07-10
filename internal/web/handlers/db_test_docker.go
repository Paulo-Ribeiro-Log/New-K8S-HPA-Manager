package handlers

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// ─── Pré-checagem de Docker (modo "local") ─────────────────────────────────

// dbDockerStatusCacheTTL — mesmo padrão de cache de CLI externa documentado no CLAUDE.md
// (ex: IsGcloudAuthActive em internal/cloudprovider/gcp/auth.go): TTL curto porque o usuário pode
// instalar o Docker e clicar em "Verificar novamente" segundos depois.
const dbDockerStatusCacheTTL = 20 * time.Second

var (
	dbDockerStatusMu     sync.Mutex
	dbDockerStatusCache  DBDockerStatusResult
	dbDockerStatusExpiry time.Time
)

// DBDockerStatusResult é a resposta de GET /api/v1/db-test/docker-status.
type DBDockerStatusResult struct {
	Installed     bool   `json:"installed"`
	DaemonRunning bool   `json:"daemon_running"`
	Error         string `json:"error,omitempty"`
	// Reason classifica a falha pra o frontend escolher qual bloco de instruções mostrar — cada
	// causa tem um fix completamente diferente (instalar vs. permissão vs. reconfigurar rede do
	// daemon), mostrar sempre o mesmo "instale o Docker" quando ele já está instalado e rodando
	// seria confuso. Vazio quando Ready().
	Reason string `json:"reason,omitempty"`
}

// Valores possíveis de DBDockerStatusResult.Reason — espelhados no frontend (DatabaseTestTab.tsx)
// pra escolher o snippet de instruções certo.
const (
	dbDockerReasonNotInstalled         = "not_installed"
	dbDockerReasonPermissionDenied     = "permission_denied"
	dbDockerReasonAddressPoolExhausted = "address_pool_exhausted"
	dbDockerReasonDaemonUnreachable    = "daemon_unreachable"
)

// Ready — atalho usado pelo frontend pra decidir se libera o botão "Executar Teste" no modo local.
func (r DBDockerStatusResult) Ready() bool { return r.Installed && r.DaemonRunning }

// checkDockerStatus resolve se o binário `docker` existe no PATH do servidor e se o daemon
// responde — as duas checagens que o modo "local" do Teste de Banco de Dados precisa pra rodar
// `docker run`. Cacheado por dbDockerStatusCacheTTL pra não disparar um exec a cada render do
// frontend.
func checkDockerStatus(ctx context.Context) DBDockerStatusResult {
	dbDockerStatusMu.Lock()
	if time.Now().Before(dbDockerStatusExpiry) {
		cached := dbDockerStatusCache
		dbDockerStatusMu.Unlock()
		return cached
	}
	dbDockerStatusMu.Unlock()

	result := computeDockerStatus(ctx)

	dbDockerStatusMu.Lock()
	dbDockerStatusCache = result
	dbDockerStatusExpiry = time.Now().Add(dbDockerStatusCacheTTL)
	dbDockerStatusMu.Unlock()

	return result
}

func computeDockerStatus(ctx context.Context) DBDockerStatusResult {
	if _, err := exec.LookPath("docker"); err != nil {
		return DBDockerStatusResult{
			Installed: false,
			Error:     "comando \"docker\" não encontrado no PATH do servidor",
			Reason:    dbDockerReasonNotInstalled,
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err == nil {
		return DBDockerStatusResult{Installed: true, DaemonRunning: true}
	}

	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	if strings.Contains(strings.ToLower(msg), "permission denied") {
		return DBDockerStatusResult{
			Installed:     true,
			DaemonRunning: false,
			Error:         "usuário do servidor sem permissão pro socket do Docker — rode: sudo usermod -aG docker $USER (e reabra o terminal)",
			Reason:        dbDockerReasonPermissionDenied,
		}
	}

	// "docker info" falhou por não conseguir alcançar o socket ("Cannot connect to the Docker
	// daemon...") — mensagem genérica demais pra saber POR QUE o daemon está fora do ar. Quando o
	// dockerd crasha na subida (ex: erro de rede), ele sai do processo inteiro antes de deixar o
	// socket disponível — a causa real só aparece no journal do systemd, nunca na resposta do
	// cliente `docker`. Melhor esforço: consulta journalctl (funciona sem sudo no Ubuntu/WSL2 pro
	// usuário no grupo `adm`); se não der (sem systemd, sem permissão, serviço com outro nome),
	// cai pro erro genérico sem quebrar a checagem.
	if reason := diagnoseDockerServiceFailure(ctx); reason != "" {
		return DBDockerStatusResult{
			Installed:     true,
			DaemonRunning: false,
			Error:         dockerReasonMessage(reason),
			Reason:        reason,
		}
	}

	return DBDockerStatusResult{
		Installed:     true,
		DaemonRunning: false,
		Error:         "daemon do Docker não respondeu: " + msg,
		Reason:        dbDockerReasonDaemonUnreachable,
	}
}

// diagnoseDockerServiceFailure procura no journal do systemd (unidade docker.service) por um
// motivo específico e já conhecido de falha na subida do daemon. Devolve "" se não achar nada
// (journalctl indisponível, sem permissão, ou nenhum padrão reconhecido) — best-effort, nunca
// bloqueia a checagem principal.
func diagnoseDockerServiceFailure(ctx context.Context) string {
	jCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(jCtx, "journalctl", "-u", "docker.service", "--no-pager", "-n", "50").Output()
	if err != nil {
		return ""
	}
	if strings.Contains(strings.ToLower(string(out)), "fully subnetted") {
		return dbDockerReasonAddressPoolExhausted
	}
	return ""
}

// dockerReasonMessage traduz um Reason (achado por diagnoseDockerServiceFailure) na mensagem
// exibida em DBDockerStatusResult.Error.
func dockerReasonMessage(reason string) string {
	switch reason {
	case dbDockerReasonAddressPoolExhausted:
		return "Docker não conseguiu criar a rede padrão — provável conflito com rotas da VPN corporativa cobrindo as faixas de IP privadas"
	default:
		return "daemon do Docker não respondeu"
	}
}

// DockerStatus — GET /api/v1/db-test/docker-status. Leitura informacional sobre o próprio
// servidor (não sobre um cluster/recurso do usuário), por isso sem RequireSREGroup(), mesmo
// padrão de outros status checks GET já existentes na app.
func (h *DBTestHandler) DockerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, checkDockerStatus(c.Request.Context()))
}

// ─── Reaper de containers órfãos ────────────────────────────────────────────

// dbTestDockerLabel identifica containers criados pelo modo "local" do Teste de Banco de Dados —
// usado tanto pra limpeza imediata (por nome) quanto pro reaper periódico (por label, via
// `docker ps --filter`).
const dbTestDockerLabel = "app=k8s-hpa-manager-db-test"

// dbTestContainerReapInterval e dbTestContainerMaxAge — mesmo espírito do cleanupLoop em
// internal/web/sse/progress.go (ticker periódico, remove o que passou da idade máxima). 10min é
// generoso: o timeout máximo de um estágio é dbTestMaxTimeoutMs (15s), então nenhum container
// legítimo passa de ~1min rodando — qualquer coisa além de 10min é órfã (cancelamento que não
// conseguiu limpar sozinho, crash do servidor, etc.).
const (
	dbTestContainerReapInterval = 5 * time.Minute
	dbTestContainerMaxAge       = 10 * time.Minute
)

// cleanupCancelledDockerContainer tenta remover, best-effort, um container cujo `docker run` (CLI)
// foi morto por cancelamento de contexto — o container pode ter ficado órfão rodando no daemon,
// já que o CLI não teve chance de rodar sua própria lógica de `--rm` (SIGKILL não é interceptável).
// Roda em goroutine própria com contexto novo (o original já está cancelado) — não bloqueia
// execLocalDocker nem propaga erro; o reaper periódico é a rede de segurança se isso falhar.
func cleanupCancelledDockerContainer(name string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput(); err != nil {
			log.Warn().Str("container", name).Err(err).Str("output", string(out)).
				Msg("DBTest: falha ao limpar container após cancelamento")
		}
	}()
}

// startDBTestContainerReaper roda em background pela vida inteira do processo — chamado uma vez
// em NewDBTestHandler, mesmo padrão de `go pt.cleanupLoop()` em NewProgressTracker
// (internal/web/sse/progress.go).
func startDBTestContainerReaper() {
	ticker := time.NewTicker(dbTestContainerReapInterval)
	defer ticker.Stop()
	for range ticker.C {
		reapOrphanedDBTestContainers()
	}
}

// reapOrphanedDBTestContainers remove containers do Teste de Banco de Dados (modo local) que
// ficaram rodando além de dbTestContainerMaxAge. Se `docker` não estiver instalado no servidor,
// vira no-op silencioso (não spamma log a cada tick num ambiente que nunca teve Docker).
func reapOrphanedDBTestContainers() {
	if _, err := exec.LookPath("docker"); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ids, err := listDBTestContainerIDs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("DBTest reaper: falha ao listar containers")
		return
	}
	if len(ids) == 0 {
		return
	}

	stale, err := staleDBTestContainerIDs(ctx, ids, time.Now())
	if err != nil {
		log.Warn().Err(err).Msg("DBTest reaper: falha ao inspecionar containers")
		return
	}

	for _, id := range stale {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if out, err := exec.CommandContext(rmCtx, "docker", "rm", "-f", id).CombinedOutput(); err != nil {
			log.Warn().Str("container", id).Err(err).Str("output", string(out)).Msg("DBTest reaper: falha ao remover container órfão")
		} else {
			log.Info().Str("container", id).Msg("DBTest reaper: container órfão removido")
		}
		rmCancel()
	}
}

func listDBTestContainerIDs(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label="+dbTestDockerLabel, "-q").Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// staleDBTestContainerIDs inspeciona os containers em lote (`docker inspect`, um exec só) e
// devolve os que passaram de dbTestContainerMaxAge — separado em função pura (recebe `now` e os
// dados já parseados) pra ser testável sem precisar de um daemon Docker real.
func staleDBTestContainerIDs(ctx context.Context, ids []string, now time.Time) ([]string, error) {
	args := append([]string{"inspect", "-f", "{{.Id}}|{{.Created}}"}, ids...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, err
	}
	return filterStaleContainers(string(out), now, dbTestContainerMaxAge), nil
}

// filterStaleContainers parseia a saída de `docker inspect -f '{{.Id}}|{{.Created}}'` (uma linha
// por container, .Created em RFC3339Nano) e devolve os IDs mais velhos que maxAge. Função pura —
// sem exec.Command — pra dar pra testar com timestamps fake.
func filterStaleContainers(inspectOutput string, now time.Time, maxAge time.Duration) []string {
	var stale []string
	for _, line := range strings.Split(inspectOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		id, createdRaw := parts[0], parts[1]
		created, err := time.Parse(time.RFC3339Nano, createdRaw)
		if err != nil {
			continue
		}
		if now.Sub(created) > maxAge {
			stale = append(stale, id)
		}
	}
	return stale
}
