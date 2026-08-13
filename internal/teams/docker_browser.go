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

// TeamsDockerEnabled expõe teamsDockerEnabled() pro handler HTTP decidir se vale a pena o
// frontend fazer polling de TeamsDockerVNCURL() — sem isso, um cliente numa instância com o modo
// Docker desligado ficaria fazendo polling à toa esperando uma URL que nunca vai aparecer.
func TeamsDockerEnabled() bool {
	return teamsDockerEnabled()
}

const (
	teamsDockerImage         = "selenium/standalone-chrome:latest"
	teamsDockerContainerName = "k8s-hpa-manager-teams-chrome"
	// Portas do container em --network host (ver comentário em ensureContainerRunning) — como
	// não há mapeamento de porta nesse modo, o container escuta direto nas portas padrão da
	// própria imagem (4444 Selenium, 7900 noVNC), não numa faixa customizada. Bug real corrigido
	// (achado ao vivo testando o refresh de verdade): com bridge network (modo original, portas
	// 14444/17900 mapeadas), o container não enxergava a VPN corporativa do host (rota via tun0 +
	// DNS interno 10.255.255.254) — `curl https://teams.microsoft.com/v2/` de dentro do container
	// dava timeout total, enquanto o host resolvia/conectava em <300ms. Confirmado isolando a
	// causa: o mesmo curl com `--network host` funcionou de primeira. Trade-off aceito: risco de
	// colisão se o usuário já tiver um Selenium Grid local rodando nessas portas — pior que a
	// alternativa (feature inteira inutilizável em qualquer rede corporativa com VPN).
	teamsDockerSeleniumPort = "4444"
	teamsDockerVNCPort      = "7900"
	teamsDockerLabel        = "app=k8s-hpa-manager-teams-browser"

	// teamsDockerProfileVolume/teamsDockerProfilePath — volume nomeado do Docker (gerenciado pelo
	// próprio Docker, sem os problemas de permissão de um bind-mount direto do host — ver
	// comentário em ensureProfileVolumeOwnership) usado como perfil PERSISTENTE do Chrome dentro
	// do container, passado via `--user-data-dir` na criação da sessão WebDriver
	// (createSeleniumSession). Bug real corrigido (achado ao vivo, relatado pelo usuário depois
	// de um login+MFA 100% bem-sucedido e um scroll adaptativo de até 90s ainda assim não achar
	// nenhuma mensagem): sem perfil persistente, TODO refresh no modo Docker parte de um Chrome
	// zero — não só exige login+MFA de novo a cada vez, como o Teams v2 nem consegue *encontrar*
	// a conversa do Mr.ViaBot na barra lateral num perfil recém-criado (diferente de "esperar mais
	// tempo", que não resolve — confirmado ao vivo: DOM continuou vazio mesmo depois do teto de
	// 90s). Com o perfil persistindo entre execuções (via volume, sobrevive a `docker rm`/restart
	// do servidor), o primeiro login continua exigindo MFA normalmente, mas os próximos refreshes
	// reaproveitam a MESMA sessão Azure AD + histórico do Teams já sincronizado — mesmo
	// comportamento "quente" que o Chrome do sistema sempre teve.
	teamsDockerProfileVolume = "k8s-hpa-manager-teams-chrome-profile"
	teamsDockerProfilePath   = "/home/seluser/chrome-profile"
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
	dockerSessionAt time.Time
	dockerVNCURL    string
	dockerMFANumber string // ver SetTeamsDockerMFANumber/TeamsDockerMFANumber abaixo
)

// SetTeamsDockerMFANumber registra o número do "number matching" do Authenticator detectado na
// tela de MFA (browser.DetectMFANumber) — chamado de dentro do loop de espera em discover.go.
// Passar "" limpa o valor (login avançou de fase ou terminou).
func SetTeamsDockerMFANumber(n string) {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	dockerMFANumber = n
}

// TeamsDockerMFANumber expõe o último número de MFA detectado — o frontend mostra isso no modal
// em vez do usuário ter que abrir o noVNC pra enxergar a tela e copiar o número manualmente. Vazio
// quando nenhuma tela de MFA foi detectada (ainda não chegou nessa fase, ou já passou dela).
func TeamsDockerMFANumber() string {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	return dockerMFANumber
}

// teamsDockerSessionMaxAge — teto preventivo de idade da sessão Docker reaproveitada, com margem
// de segurança abaixo de teamsDockerSessionTimeoutSecs (o Grid expira a sessão sozinho depois
// desse tempo, mas o websocket CDP em cima dela não fecha de forma limpa quando isso acontece —
// ver bug real abaixo). Passado esse teto, getDockerBrowser relança preventivamente em vez de
// tentar reaproveitar uma sessão que já pode estar morta.
const teamsDockerSessionMaxAge = 20 * time.Minute

// teamsDockerSessionTimeoutSecs configura SE_NODE_SESSION_TIMEOUT do Grid (segundos) — o padrão
// da imagem é 300s (5min), curto demais pro tempo real que RunDiscovery/ScanConversations/
// SendBatch podem levar entre chamadas (browser persistente, reaproveitado por potencialmente
// várias operações ao longo de uma sessão de uso). 1800s (30min) dá folga generosa.
const teamsDockerSessionTimeoutSecs = "1800"

// pagesHealthTimeout — teto de tempo pra checagem de saúde "o browser Docker reaproveitado ainda
// responde?" (dockerBrowser.Pages()).
//
// Bug real corrigido (achado ao vivo): quando a sessão Selenium expira no lado do Grid, o
// websocket CDP correspondente NÃO fecha de forma limpa — Pages() (que espera uma resposta CDP)
// trava indefinidamente, sem erro nem timeout. Como operationMu serializa RunDiscovery/
// ScanConversations/SendBatch entre si, isso travava as TRÊS operações pra sempre (não um crash —
// pior, um hang silencioso, sem log nenhum indicando o que aconteceu) até reiniciar o servidor.
// Reproduzido ao vivo: 2ª chamada de RunDiscovery ficou >4min parada exatamente nesse ponto, ~5min
// depois da sessão anterior ter sido criada (bate com o SE_NODE_SESSION_TIMEOUT default de 300s).
const pagesHealthTimeout = 5 * time.Second

// pagesWithTimeout chama browser.Pages() com um teto de tempo — Pages() em si não aceita context,
// então uma goroutine + select é o único jeito de limitar uma chamada síncrona travada.
func pagesWithTimeout(b *rod.Browser, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		_, err := b.Pages()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		// A goroutine acima pode nunca terminar (mesmo bug que motivou este helper) — ela vaza,
		// mas é inofensiva: só bloqueada num canal com buffer 1 que nunca mais é lido, sem reter
		// nada além disso. Aceitável frente à alternativa (travar a chamada real).
		return fmt.Errorf("timeout (%s) verificando browser Docker existente — sessão provavelmente expirada", timeout)
	}
}

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
		if age := time.Since(dockerSessionAt); age > teamsDockerSessionMaxAge {
			logger.Info().Dur("age", age).Msg("[Teams] Sessão Docker passou da idade máxima segura — relançando preventivamente")
			dockerBrowser = nil
			dockerSessionID = ""
		} else if err := pagesWithTimeout(dockerBrowser, pagesHealthTimeout); err == nil {
			return dockerBrowser, nil
		} else {
			logger.Warn().Err(err).Msg("[Teams] Browser Docker persistente morreu/parou de responder — relançando")
			dockerBrowser = nil
			dockerSessionID = ""
		}
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
	dockerSessionAt = time.Now()
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

	if err := ensureProfileVolumeOwnership(ctx, logger); err != nil {
		// Não crítico o bastante pra abortar — sem o volume utilizável o Chrome ainda funciona
		// (só sem persistência), createSeleniumSession vai simplesmente falhar mais na frente se
		// o --user-data-dir for de fato inacessível, com erro claro nesse ponto.
		logger.Warn().Err(err).Msg("[Teams] Falha ao garantir permissão do volume de perfil persistente — seguindo mesmo assim")
	}

	logger.Info().Str("image", teamsDockerImage).Msg("[Teams] Subindo container Docker do Chrome (selenium/standalone-chrome)...")
	// --network host (em vez de -p <porta>:<porta>): o container precisa da mesma rota de rede do
	// host pra alcançar teams.microsoft.com/login.microsoftonline.com através da VPN corporativa
	// (tun0) — o bridge network padrão do Docker não tem essa rota. Ver comentário nas constantes
	// de porta acima pro bug real que motivou a mudança.
	args := []string{
		"run", "-d",
		"--name", teamsDockerContainerName,
		"--label", teamsDockerLabel,
		"--shm-size", "2g",
		"--network", "host",
		"-v", teamsDockerProfileVolume + ":" + teamsDockerProfilePath,
		"-e", "SE_VNC_NO_PASSWORD=1",
		"-e", "SE_NODE_MAX_SESSIONS=1",
		"-e", "SE_NODE_SESSION_TIMEOUT=" + teamsDockerSessionTimeoutSecs,
		teamsDockerImage,
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(runCtx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker run falhou: %v — %s", err, strings.TrimSpace(string(out)))
	}

	return waitSeleniumReady(ctx, logger)
}

// ensureProfileVolumeOwnership corrige a permissão do volume nomeado usado como perfil
// persistente do Chrome (teamsDockerProfileVolume) — validado ao vivo: um volume Docker nomeado
// recém-criado nasce dono de root:root (0755), mas o Chrome dentro do container roda como
// "seluser" (não-root, por padrão da imagem selenium/standalone-chrome) — sem essa correção,
// `--user-data-dir` aponta pra um diretório que o Chrome não tem permissão de escrita, e a
// criação da sessão WebDriver falha com "cannot create default profile directory". Descobre o
// UID/GID reais de "seluser" dinamicamente (via `id -u`/`id -g` rodado dentro da própria imagem)
// em vez de hardcodar — a imagem é `:latest`, então o UID pode mudar entre pulls futuros.
// Idempotente: seguro rodar toda vez que o container é recriado, mesmo que o volume já exista com
// a permissão certa (chown de novo não tem efeito colateral).
func ensureProfileVolumeOwnership(ctx context.Context, logger *zerolog.Logger) error {
	idCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(idCtx, "docker", "run", "--rm", teamsDockerImage, "sh", "-c", "id -u; id -g").Output()
	if err != nil {
		return fmt.Errorf("erro ao descobrir uid/gid de seluser: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return fmt.Errorf("saída inesperada de id -u/-g: %q", string(out))
	}
	uid, gid := fields[0], fields[1]

	chownCtx, cancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel2()
	args := []string{
		"run", "--rm", "--user", "root", "--entrypoint", "chown",
		"-v", teamsDockerProfileVolume + ":/mnt",
		teamsDockerImage,
		"-R", uid + ":" + gid, "/mnt",
	}
	if out, err := exec.CommandContext(chownCtx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("chown do volume falhou: %v — %s", err, strings.TrimSpace(string(out)))
	}
	logger.Debug().Str("uid", uid).Str("gid", gid).Msg("[Teams] Volume de perfil persistente com permissão corrigida")
	return nil
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
//
// --user-data-dir aponta pro volume persistente montado em teamsDockerProfilePath (ver
// ensureContainerRunning/ensureProfileVolumeOwnership) — sem isso, o Selenium cria um profile
// temporário novo por sessão (comportamento padrão, pensado pra isolamento de testes), perdendo
// login/histórico do Teams a cada refresh.
func createSeleniumSession(ctx context.Context) (cdpURL, sessionID string, err error) {
	body := fmt.Sprintf(`{"capabilities":{"alwaysMatch":{"browserName":"chrome","goog:chromeOptions":{"args":["--user-data-dir=%s"]}}}}`, teamsDockerProfilePath)
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
	dockerSessionAt = time.Time{}
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
