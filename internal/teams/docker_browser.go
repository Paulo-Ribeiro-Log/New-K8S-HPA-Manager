package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog"
)

// Modo Docker pro browser do Teams — origem: o Chromium embutido do Rod (revisão pinada e
// antiga, ver internal/browser) crasha com SIGTRAP ao renderizar o Teams v2 (confirmado 3x ao
// vivo, ver BROWSER-CONSOLIDATION-STUDY.md Fase 2). A imagem `selenium/standalone-chrome` traz um
// Chrome atual e mantido (validado ao vivo: browserVersion 151.x, bem além da revisão pinada do
// Rod) rodando num container isolado — sem depender de instalar/atualizar Chrome no host WSL2.
// Como o login precisa de interação humana real (SSO/MFA), a imagem expõe noVNC (porta mapeada
// em teamsDockerVNCPort): o usuário abre essa URL no PRÓPRIO navegador pra ver e operar a tela do
// Chrome de dentro do container, enquanto o Rod fala CDP puro com ele.
//
// Ordem de tentativa em getBrowser() (browser_manager.go), quando habilitado: Docker → Chrome do
// sistema → embed do Rod (último recurso, sabidamente instável pro Teams v2 — ver estudo).
//
// Validado ao vivo com sucesso no fluxo principal (container sobe, sessão Selenium criada, CDP
// conecta, navega, aguenta bem além do ponto onde o embed sempre crashava com SIGTRAP) — MAS um
// 2º teste na mesma sessão expôs um bug real e severo: o servidor Go inteiro PANICAVA e morria
// (não só a extração falhava) quando o proxy WebSocket do Selenium Grid entregava um frame CDP
// malformado — goroutine interna do go-rod (`consumeMessages`, spawnada pelo próprio Start(), sem
// nenhum recover() no meio do caminho) chamava utils.E(err) num erro de json.Unmarshal, e
// utils.E() faz panic() puro. Corrigido com um patch manual no vendor (ver
// vendor/github.com/go-rod/rod/lib/cdp/client.go — comentário "PATCH MANUAL" no topo de
// consumeMessages) que troca o panic por log+continue. Esse bug é uma falha real do go-rod
// exposta pela camada extra de proxy do Selenium (conexão direta a um Chrome local aparentemente
// nunca dispara esse caso-limite) — o patch protege TODOS os usos de go-rod na app (ServiceNow
// incluso), não só o modo Docker.
//
// Por segurança, mesmo com o patch aplicado, o modo Docker fica ATRÁS de opt-in explícito
// (teamsDockerEnableEnvVar) até validar de novo com mais uso real — diferente da decisão inicial
// "Docker por padrão", revertida depois do crash. Chrome do sistema continua sendo o padrão.
const teamsDockerEnableEnvVar = "K8S_HPA_TEAMS_DOCKER_BROWSER"

func teamsDockerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(teamsDockerEnableEnvVar)))
	return v == "true" || v == "1"
}

const (
	teamsDockerImage         = "selenium/standalone-chrome:latest"
	teamsDockerContainerName = "k8s-hpa-manager-teams-chrome"
	// Portas fixas mapeadas no host — deliberadamente fora da faixa "óbvia" (4444/7900 são os
	// padrões de um Selenium Grid local, que o usuário pode já ter rodando por conta própria)
	// pra reduzir chance de colisão.
	teamsDockerSeleniumPort = "14444"
	teamsDockerVNCPort      = "17900"
	teamsDockerLabel        = "app=k8s-hpa-manager-teams-browser"
)

// dockerMu protege o estado do browser Docker persistente — mesmo padrão de browserMgr em
// browser_manager.go (reaproveitado entre RunDiscovery/ScanConversations/SendBatch), mas mantido
// separado do browser.Manager compartilhado porque o ciclo de vida é fundamentalmente diferente
// (spawna+conecta a um browser REMOTO via sessão WebDriver, não um processo local via
// launcher.New() — browser.Manager/Launch não cobre esse caso).
var (
	dockerMu        sync.Mutex
	dockerBrowser   *rod.Browser
	dockerSessionID string
	dockerVNCURL    string
)

// TeamsDockerVNCURL retorna a última URL noVNC conhecida (pro usuário abrir e interagir com o
// login) — vazio se o modo Docker nunca foi usado com sucesso nesta execução do servidor.
func TeamsDockerVNCURL() string {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	return dockerVNCURL
}

// dockerAvailable checa rápido se o binário docker existe e o daemon responde — sem cache
// próprio (diferente de checkDockerStatus em internal/web/handlers/db_test_docker.go, que já
// cacheia por 20s no nível do handler HTTP): aqui é chamado no máximo 1x por getBrowser(), já
// naturalmente pouco frequente (browser persistente, ver browser_manager.go).
func dockerAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(cctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

// getDockerBrowser retorna um *rod.Browser conectado ao Chrome rodando dentro do container
// selenium/standalone-chrome, subindo o container e criando uma sessão WebDriver se necessário.
// Reaproveita a mesma conexão entre chamadas (igual browser.Manager) — só recria se a conexão
// morreu ou nunca existiu.
func getDockerBrowser(logger *zerolog.Logger) (*rod.Browser, error) {
	dockerMu.Lock()
	defer dockerMu.Unlock()

	if dockerBrowser != nil {
		if _, err := dockerBrowser.Pages(); err == nil {
			return dockerBrowser, nil
		}
		logger.Warn().Msg("[Teams] Browser Docker persistente morreu — relançando")
		dockerBrowser = nil
		dockerSessionID = ""
	}

	ctx := context.Background()
	if !dockerAvailable(ctx) {
		return nil, fmt.Errorf("Docker não disponível (binário ausente do PATH ou daemon não responde)")
	}

	if err := ensureContainerRunning(ctx, logger); err != nil {
		return nil, err
	}

	cdpURL, sessionID, err := createSeleniumSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar sessão Selenium: %w", err)
	}

	b := rod.New().ControlURL(cdpURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar via CDP ao container: %w", err)
	}

	dockerBrowser = b
	dockerSessionID = sessionID
	dockerVNCURL = fmt.Sprintf("http://localhost:%s/?autoconnect=1&resize=scale", teamsDockerVNCPort)
	logger.Info().Str("vnc_url", dockerVNCURL).Str("session_id", sessionID).
		Msg("[Teams] Browser Docker (selenium/standalone-chrome) pronto — abra a URL VNC pra ver/interagir com o login")
	return b, nil
}

// ensureContainerRunning reaproveita o container existente (nome fixo) se já estiver rodando;
// caso contrário remove qualquer resquício parado com o mesmo nome e sobe um novo, esperando o
// Grid Selenium reportar "ready".
func ensureContainerRunning(ctx context.Context, logger *zerolog.Logger) error {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", teamsDockerContainerName).Output()
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		// Container já rodando — ainda assim confirma que o Grid responde antes de devolver
		// (pode estar num restart/boot ainda em andamento).
		return waitSeleniumReady(ctx, logger)
	}
	// Não existe ou está parado (crash anterior, `docker stop` manual etc.) — remove qualquer
	// resquício antes de recriar; `docker run --name` falha se o nome já existir, mesmo parado.
	exec.CommandContext(ctx, "docker", "rm", "-f", teamsDockerContainerName).Run() //nolint:errcheck

	logger.Info().Str("image", teamsDockerImage).Msg("[Teams] Subindo container Docker do Chrome (selenium/standalone-chrome)...")
	args := []string{
		"run", "-d",
		"--name", teamsDockerContainerName,
		"--label", teamsDockerLabel,
		"--shm-size", "2g",
		"-p", teamsDockerSeleniumPort + ":4444",
		"-p", teamsDockerVNCPort + ":7900",
		"-e", "SE_VNC_NO_PASSWORD=1",
		"-e", "SE_NODE_MAX_SESSIONS=1",
		teamsDockerImage,
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(runCtx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker run falhou: %v — %s", err, strings.TrimSpace(string(out)))
	}

	return waitSeleniumReady(ctx, logger)
}

// waitSeleniumReady faz polling em GET /status até o Grid reportar {"value":{"ready":true}} —
// validado ao vivo contra a imagem real (~5-10s de boot em cache local; mais na primeira vez com
// pull). 60s de teto — generoso o suficiente pra um pull a frio na pior hipótese, sem travar
// indefinidamente se o container subir quebrado.
func waitSeleniumReady(ctx context.Context, logger *zerolog.Logger) error {
	deadline := time.Now().Add(60 * time.Second)
	url := fmt.Sprintf("http://localhost:%s/status", teamsDockerSeleniumPort)
	client := &http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			var body struct {
				Value struct {
					Ready bool `json:"ready"`
				} `json:"value"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if decodeErr == nil && body.Value.Ready {
				logger.Info().Msg("[Teams] Container Docker pronto (Selenium Grid ready)")
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout esperando o container Docker do Chrome ficar pronto")
}

// createSeleniumSession cria uma sessão WebDriver no Grid do container e extrai a URL CDP
// (capacidade "se:cdp" do Selenium 4.x, que faz proxy pro debugger do Chrome interno) — é assim
// que o Rod (que fala CDP puro, não WebDriver) consegue controlar um browser gerenciado pelo
// Selenium sem reimplementar o protocolo WebDriver. Validado ao vivo contra a imagem real: a URL
// devolvida usa o endereço INTERNO do container na rede Docker (ex: "ws://100.64.0.2:4444/..."),
// inalcançável a partir do host — rewriteToLocalhost troca isso pela porta mapeada no host.
func createSeleniumSession(ctx context.Context) (cdpURL, sessionID string, err error) {
	body := `{"capabilities":{"alwaysMatch":{"browserName":"chrome"}}}`
	url := fmt.Sprintf("http://localhost:%s/session", teamsDockerSeleniumPort)

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Value struct {
			SessionID    string `json:"sessionId"`
			Capabilities struct {
				SeCdp string `json:"se:cdp"`
			} `json:"capabilities"`
			// Selenium devolve erro dentro de "value" também (não só via status HTTP não-2xx).
			Error   string `json:"error"`
			Message string `json:"message"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("resposta inesperada do Selenium (status %d): %w", resp.StatusCode, err)
	}
	if result.Value.SessionID == "" {
		msg := result.Value.Message
		if msg == "" {
			msg = result.Value.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("status HTTP %d sem sessionId", resp.StatusCode)
		}
		return "", "", fmt.Errorf("Selenium não criou sessão: %s", msg)
	}
	if result.Value.Capabilities.SeCdp == "" {
		return "", "", fmt.Errorf("Selenium não retornou capacidade se:cdp (versão da imagem sem suporte a CDP passthrough?)")
	}

	return rewriteToLocalhost(result.Value.Capabilities.SeCdp), result.Value.SessionID, nil
}

// rewriteToLocalhost troca o host:porta de uma URL ws://<interno>/<path> pela porta mapeada no
// host (teamsDockerSeleniumPort em localhost), preservando o path — o Grid roteia
// "/session/<id>/se/cdp" pelo mesmo servidor HTTP que escuta em :4444 (mapeado), então só o
// host:porta precisa mudar, não o resto da URL.
func rewriteToLocalhost(wsURL string) string {
	idx := strings.Index(wsURL, "/session/")
	if idx == -1 {
		return wsURL
	}
	return "ws://localhost:" + teamsDockerSeleniumPort + wsURL[idx:]
}

// CloseDockerBrowser encerra a sessão Selenium e remove o container — chamado junto de
// CloseBrowser() no shutdown do servidor (ver server.go), pra não deixar o container órfão
// rodando em segundo plano.
func CloseDockerBrowser() {
	dockerMu.Lock()
	sessionID := dockerSessionID
	dockerBrowser = nil
	dockerSessionID = ""
	dockerVNCURL = ""
	dockerMu.Unlock()

	if sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		url := fmt.Sprintf("http://localhost:%s/session/%s", teamsDockerSeleniumPort, sessionID)
		if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil); err == nil {
			http.DefaultClient.Do(req) //nolint:errcheck
		}
		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exec.CommandContext(ctx, "docker", "rm", "-f", teamsDockerContainerName).Run() //nolint:errcheck
}
