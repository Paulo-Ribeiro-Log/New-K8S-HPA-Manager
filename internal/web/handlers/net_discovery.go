package handlers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/web/sse"
)

// ─── "Descoberta de Rede" — Fase 1 (IP-ROUTE-DISCOVERY-PLAN.md): traceroute básico + grafo ao
// vivo. Sem nenhuma camada de enriquecimento ainda (DNS reverso/ASN/nuvem/cross-reference K8s/
// fingerprint de SO são Fases 2-4) — só descobre o caminho de rede hop-a-hop e transmite cada
// salto assim que ele responde, pedido explícito do usuário: "desenhar na tela um fluxo da rota...
// ficaria lindo o acompanhar do processo". Reaproveita o mesmo padrão dual pod/local já
// estabelecido pelo Teste de Latência/Kafka/Banco de Dados — nunca um mecanismo novo.

const (
	// netDiscoveryPodImage — mesma imagem já usada pelo Teste de Latência (nicolaka/netshoot),
	// já traz traceroute/mtr/dig/whois/nc prontos. Ver latencyTestPodImage (latency_test_tool.go)
	// pro comentário completo sobre a escolha/versão fixada.
	netDiscoveryPodImage                = latencyTestPodImage
	netDiscoveryPodContainer            = "netshoot"
	netDiscoveryPodActiveDeadline int64 = 300
	netDiscoveryPodReadyTimeout         = 60 * time.Second

	// netDiscoveryDockerLabel identifica containers do modo "local" — reaproveita o reaper
	// periódico compartilhado (reapOrphanedContainersByLabel, db_test_docker.go), só muda o label
	// pra não misturar órfãos desta ferramenta com os do Teste de Banco de Dados/Kafka.
	netDiscoveryDockerLabel = "app=k8s-hpa-manager-net-discovery"

	// netDiscoveryMaxHops — teto de saltos (mesmo default do traceroute clássico).
	netDiscoveryMaxHops = 30
	// netDiscoveryProbeTimeoutSec — quanto esperar por resposta de CADA salto antes de marcar
	// como "não respondeu" e seguir pro próximo. 2s é generoso o bastante pra rede corporativa/VPN
	// sem deixar um hop morto travar o traceroute inteiro por muito tempo.
	netDiscoveryProbeTimeoutSec = 2
	// netDiscoveryOverallTimeout — teto absoluto pro comando inteiro (30 hops × pior caso de
	// espera), rede de segurança contra um traceroute que nunca termina sozinho.
	netDiscoveryOverallTimeout = 90 * time.Second
)

// NetDiscoveryHop é um salto do caminho de rede — Fase 1 só tem o essencial (índice/IP/latência/
// se respondeu); campos de enriquecimento (DNS reverso, ASN, cloud, recurso K8s conhecido,
// fingerprint de SO) chegam nas Fases 2-4 sem precisar mudar este contrato, só adicionar campos.
type NetDiscoveryHop struct {
	Index    int     `json:"index"`
	IP       string  `json:"ip,omitempty"` // vazio = hop não respondeu ("* * *")
	RTTMs    float64 `json:"rtt_ms,omitempty"`
	TimedOut bool    `json:"timed_out"`
	IsTarget bool    `json:"is_target"` // true quando este hop é o próprio destino resolvido
}

// NetDiscoveryResult é o payload final do evento "complete".
type NetDiscoveryResult struct {
	TargetInput    string            `json:"target_input"`    // o que o usuário digitou (IP ou hostname)
	TargetIP       string            `json:"target_ip"`       // IP resolvido, o que foi de fato traçado
	TargetResolved bool              `json:"target_resolved"` // false só quando TargetInput já era um IP (nada pra resolver)
	Hops           []NetDiscoveryHop `json:"hops"`
	Reached        bool              `json:"reached"` // true se o último hop bateu com TargetIP
	// Fingerprint — Fase 2 (net_discovery_fingerprint.go). Ponteiro nil quando o probe falhou
	// (ex: timeout muito curto, alvo bloqueou tudo) — nunca bloqueia o resultado principal do
	// traceroute, é um enriquecimento best-effort igual aos outros desta app.
	Fingerprint *NetDiscoveryFingerprint `json:"fingerprint,omitempty"`
}

// isIPAddress detecta se `raw` já é um IPv4/IPv6 literal (net.ParseIP cobre os dois formatos).
func isIPAddress(raw string) bool {
	return net.ParseIP(raw) != nil
}

// resolveTarget aceita IP OU hostname/FQDN (pedido explícito do usuário, seção 6.1 do plano —
// entrada bidirecional, sem seletor de "tipo" separado) e devolve sempre um IP pra traçar. Quando
// já é um IP, `resolved=false` (nada foi resolvido, só validado). Prefere IPv4 no resultado do
// LookupHost (mtr/traceroute -T lidam melhor com IPv4 na maioria dos ambientes desta app — AKS/
// GCP/Kyndryl não expõem IPv6 hoje, confirmação que se aplicar no futuro pode remover essa
// preferência).
func resolveTarget(ctx context.Context, raw string) (ip string, resolved bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("informe um IP ou hostname")
	}
	if isIPAddress(raw) {
		return raw, false, nil
	}

	resolver := &net.Resolver{}
	addrs, lookupErr := resolver.LookupHost(ctx, raw)
	if lookupErr != nil || len(addrs) == 0 {
		return "", false, fmt.Errorf("não foi possível resolver %q: %w", raw, lookupErr)
	}
	for _, a := range addrs {
		if net.ParseIP(a).To4() != nil {
			return a, true, nil
		}
	}
	return addrs[0], true, nil // só teve IPv6 disponível
}

// tracerouteHopLineRegex casa uma linha de saída de `traceroute -n` (DNS já desabilitado — a
// resolução de hostname por hop fica pras Fases 2+, que fazem PTR de forma controlada, não a
// resolução ad-hoc e lenta do próprio traceroute por salto). Formato: " N  IP  X ms  Y ms  Z ms"
// ou " N  * * *" quando o salto não respondeu a nenhuma sonda.
var tracerouteHopLineRegex = regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
var tracerouteRTTRegex = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*ms`)
var tracerouteIPToken = regexp.MustCompile(`^([0-9]{1,3}(?:\.[0-9]{1,3}){3}|[0-9a-fA-F:]+)$`)

// parseTracerouteLine extrai um NetDiscoveryHop de uma linha de stdout do traceroute, ou
// (nil, false) se a linha não for reconhecida como linha de salto (ex: o cabeçalho
// "traceroute to X (X), 30 hops max..." que o comando imprime antes do primeiro salto).
func parseTracerouteLine(line string, targetIP string) (*NetDiscoveryHop, bool) {
	m := tracerouteHopLineRegex.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	index, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, false
	}
	rest := strings.TrimSpace(m[2])

	hop := &NetDiscoveryHop{Index: index}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return nil, false
	}
	if tracerouteIPToken.MatchString(fields[0]) {
		hop.IP = fields[0]
		hop.IsTarget = hop.IP == targetIP
	} else {
		// primeiro token não é IP (ex: "*") — hop não respondeu a nenhuma sonda.
		hop.TimedOut = true
	}

	// Média das amostras de RTT que responderam (traceroute manda -q sondas por salto; algumas
	// podem individualmente virar "*" mesmo com outras respondendo — não é "hop todo perdido"
	// nesse caso, só uma sonda específica).
	rtts := tracerouteRTTRegex.FindAllStringSubmatch(rest, -1)
	if len(rtts) > 0 {
		var sum float64
		for _, r := range rtts {
			v, _ := strconv.ParseFloat(r[1], 64)
			sum += v
		}
		hop.RTTMs = sum / float64(len(rtts))
	} else if hop.IP == "" {
		hop.TimedOut = true
	}

	return hop, true
}

// lineCallbackWriter implementa io.Writer só pra dar aos dois modos de execução (pod exec via
// SPDY / docker run local) uma forma comum de processar a saída LINHA A LINHA conforme ela chega,
// em vez de esperar o comando inteiro terminar pra só então parsear tudo de uma vez — é o que
// viabiliza o salto aparecer no grafo do frontend em tempo real (pedido explícito do usuário).
// Implementado via io.Pipe + bufio.Scanner rodando em goroutine própria, não bytes.Buffer.
func streamCommandLines(ctx context.Context, run func(stdout io.Writer) error, onLine func(line string)) error {
	pr, pw := io.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		// Linhas de saída de traceroute são curtas, mas o buffer padrão do Scanner (64KB) já é
		// bem mais que suficiente — sem necessidade de aumentar.
		for scanner.Scan() {
			onLine(scanner.Text())
		}
	}()

	runErr := run(pw)
	_ = pw.Close()
	<-done // espera o scanner drenar tudo que já foi escrito antes de retornar

	return runErr
}

// netDiscoveryTCPPort — porta usada pelas sondas TCP do tcptraceroute. 443 escolhida como default
// (HTTPS é o serviço mais universalmente exposto hoje) — não confirma nem descarta que o destino
// tenha essa porta aberta de verdade; tcptraceroute mostra "[open]"/"[closed]" no salto final
// quando consegue determinar, mas o CAMINHO em si (o que interessa nesta Fase 1) já é obtido
// mesmo que a porta não esteja aberta — cada salto intermediário responde ao TTL expirado
// independente do estado da porta final.
const netDiscoveryTCPPort = 443

// tracerouteArgs monta os argumentos do tcptraceroute — mesmos nos dois modos (pod/local), só o
// mecanismo de execução muda.
//
// Achado real, testado ao vivo contra a imagem netshoot: o `traceroute` de dentro dela é o applet
// do BusyBox, que NÃO tem `-T` (modo TCP) — só UDP (default) ou `-I` (ICMP). `tcptraceroute` é um
// binário PRÓPRIO e separado, também presente na mesma imagem, propositalmente feito pra
// traceroute via TCP (mais provável de atravessar firewall corporativo/NSG de nuvem do que UDP/
// ICMP puro, ver seção 3.1 do plano) — usa ele diretamente em vez de tentar simular com o
// traceroute genérico. Confirmado ao vivo contra 8.8.8.8: alcançou o destino com sucesso; contra
// um alvo inalcançável, devolve exit code 1 mas ainda assim imprime os hops que respondeu — por
// isso o chamador (runDiscovery) só considera falha total quando NENHUM hop foi coletado.
func tracerouteArgs(targetIP string) []string {
	return []string{
		"tcptraceroute", "-n",
		"-w", strconv.Itoa(netDiscoveryProbeTimeoutSec),
		"-q", "1", // 1 sonda por salto — favorece velocidade/fluidez da animação sobre riqueza estatística (Fase 1)
		"-m", strconv.Itoa(netDiscoveryMaxHops),
		targetIP, strconv.Itoa(netDiscoveryTCPPort),
	}
}

// runTracerouteInPod executa tcptraceroute dentro do pod de teste via exec SPDY, chamando `onLine`
// pra cada linha de stdout conforme ela chega (ver streamCommandLines). Mesma limitação de
// CAP_NET_RAW já documentada pro modo ICMP do Teste de Latência (runICMPProbe) se aplica aqui
// também, herdada da necessidade de ler respostas ICMP Time-Exceeded via socket raw — não
// contornável por este código.
func runTracerouteInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, targetIP string, onLine func(line string)) error {

	return streamCommandLines(ctx, func(stdout io.Writer) error {
		return execCmdInPodStreaming(ctx, clientset, restConfig, namespace, podName, netDiscoveryPodContainer, tracerouteArgs(targetIP), stdout)
	}, onLine)
}

// runTracerouteLocal — mesma lógica, mas via `docker run` no host (modo local, ver
// db_test_docker.go pro precheck de Docker já compartilhado). `--network host` — precisa da rede
// real do host pra alcançar destinos remotos (ex: infraestrutura na VPN corporativa até a Kyndryl,
// confirmado pelo usuário como sendo a mesma VPN já usada pro AKS/GCP), um container isolado na
// rede bridge padrão do Docker não teria essa rota.
func runTracerouteLocal(ctx context.Context, targetIP string, onLine func(line string)) error {
	args := append([]string{
		"run", "--rm", "--network=host",
		"--label", netDiscoveryDockerLabel,
		netDiscoveryPodImage,
	}, tracerouteArgs(targetIP)...)

	return streamCommandLines(ctx, func(stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdout = stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if stderr.Len() > 0 {
				return fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
			}
			return err
		}
		return nil
	}, onLine)
}

// ─── Handler: endpoint SSE + rotas ─────────────────────────────────────────────

// NetDiscoveryHandler orquestra a "Descoberta de Rede" sob demanda — mesmo padrão de
// LatencyTestHandler (start retorna session_id, cliente conecta no stream, cancel força parada).
type NetDiscoveryHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	cancelFuncs    sync.Map // sessionID -> context.CancelFunc
	runningUsers   sync.Map // userEmail -> struct{} — "uma descoberta por vez por usuário"
	seenClusters   sync.Map // cluster -> struct{} — só varre órfãos onde este handler já criou pod
}

// NewNetDiscoveryHandler cria o handler e inicia a varredura periódica de pods/containers órfãos
// em background (mesmo padrão de NewLatencyTestHandler/NewDBTestHandler).
func NewNetDiscoveryHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *NetDiscoveryHandler {
	h := &NetDiscoveryHandler{kubeManager: km, tracker: tracker, historyTracker: ht}
	go h.sweepOrphanPods()
	go startNetDiscoveryContainerReaper()
	return h
}

// DockerStatus — GET /api/v1/net-discovery/docker-status. Reaproveita checkDockerStatus
// (db_test_docker.go) — mesmo padrão do Teste de Kafka: a checagem é sobre o Docker do host, não
// tem nada específico desta ferramenta. Sem RequireSREGroup() (leitura informacional).
func (h *NetDiscoveryHandler) DockerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, checkDockerStatus(c.Request.Context()))
}

const (
	netDiscoverySweepInterval = 5 * time.Minute
	// netDiscoveryOrphanAge é bem acima do netDiscoveryPodActiveDeadline (5min) — mesmo raciocínio
	// de latencyTestOrphanAge.
	netDiscoveryOrphanAge = 10 * time.Minute
)

func (h *NetDiscoveryHandler) sweepOrphanPods() {
	ticker := time.NewTicker(netDiscoverySweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		h.seenClusters.Range(func(key, _ interface{}) bool {
			h.sweepClusterOrphans(key.(string))
			return true
		})
	}
}

func (h *NetDiscoveryHandler) sweepClusterOrphans(cluster string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=net-discovery-tool",
	})
	if err != nil {
		return
	}

	for _, pod := range pods.Items {
		if time.Since(pod.CreationTimestamp.Time) < netDiscoveryOrphanAge {
			continue
		}
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		gracePeriod := int64(0)
		_ = clientset.CoreV1().Pods(pod.Namespace).Delete(delCtx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
		delCancel()
	}
}

// startNetDiscoveryContainerReaper — reaproveita o reaper genérico já compartilhado entre o Teste
// de Banco de Dados e o Teste de Kafka (reapOrphanedContainersByLabel, db_test_docker.go), só com
// o label desta ferramenta.
func startNetDiscoveryContainerReaper() {
	ticker := time.NewTicker(dbTestContainerReapInterval)
	defer ticker.Stop()
	for range ticker.C {
		reapOrphanedContainersByLabel(netDiscoveryDockerLabel, "NetDiscovery")
	}
}

// createNetDiscoveryPod — mesmo padrão de createTestPod (latency_test_tool.go), label/nome
// próprios desta ferramenta pra não confundir na hora de inspecionar o cluster manualmente.
func createNetDiscoveryPod(ctx context.Context, clientset kubernetes.Interface, namespace string) (podName string, cleanup func(), err error) {
	podName = fmt.Sprintf("net-discovery-%s", uuid.New().String()[:8])
	activeDeadline := netDiscoveryPodActiveDeadline

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "net-discovery-tool",
				"created-by": "k8s-hpa-manager",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &activeDeadline,
			Containers: []corev1.Container{
				{
					Name:    netDiscoveryPodContainer,
					Image:   netDiscoveryPodImage,
					Command: []string{"sleep", "300"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	if _, err = clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("falha ao criar pod de descoberta: %w", err)
	}

	cleanup = func() {
		delCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		gracePeriod := int64(0)
		_ = clientset.CoreV1().Pods(namespace).Delete(delCtx, podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
	}

	return podName, cleanup, nil
}

// RunNetDiscoveryRequest é o body do POST /run.
type RunNetDiscoveryRequest struct {
	Target string `json:"target"` // IP ou hostname/FQDN — resolveTarget decide qual é qual
	Mode   string `json:"mode"`   // "pod" | "local"
	// Cluster/Namespace só fazem sentido (e são exigidos) no modo "pod".
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}

const (
	netDiscoveryModePod   = "pod"
	netDiscoveryModeLocal = "local"
)

// Run inicia a descoberta e retorna um session_id pra streaming SSE.
// POST /api/v1/net-discovery/run
func (h *NetDiscoveryHandler) Run(c *gin.Context) {
	var req RunNetDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	req.Target = strings.TrimSpace(req.Target)
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Target == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "target (IP ou hostname) é obrigatório"))
		return
	}
	if req.Mode != netDiscoveryModePod && req.Mode != netDiscoveryModeLocal {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_MODE", "mode deve ser 'pod' ou 'local'"))
		return
	}
	if req.Mode == netDiscoveryModePod && (req.Cluster == "" || req.Namespace == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster e namespace são obrigatórios no modo pod"))
		return
	}

	userInfo := GetUserInfoForHistory(c)

	lockKey := userInfo.Email
	if lockKey == "" {
		lockKey = "unknown"
	}
	if _, alreadyRunning := h.runningUsers.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("DISCOVERY_ALREADY_RUNNING",
			"você já tem uma descoberta de rede em andamento — aguarde terminar ou cancele antes de iniciar outra"))
		return
	}

	sessionID := uuid.New().String()
	ctx, cancel := context.WithTimeout(context.Background(), netDiscoveryOverallTimeout)
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(sessionID)
		defer h.runningUsers.Delete(lockKey)
		defer cancel()
		h.runDiscovery(ctx, sessionID, req, userInfo)
	}()

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// Stream conecta o cliente ao fluxo SSE de uma descoberta em andamento — idêntico ao Stream do
// Teste de Latência (mesmo broker compartilhado, GetProgressTracker()).
func (h *NetDiscoveryHandler) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(event)); err != nil {
				return
			}
			c.Writer.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

// Cancel força a parada de uma descoberta em andamento.
// POST /api/v1/net-discovery/cancel/:sessionId
func (h *NetDiscoveryHandler) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	if val, ok := h.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
		h.cancelFuncs.Delete(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}

// runDiscovery executa o fluxo completo, reportando progresso via SSE — um evento "hop" por
// salto conforme ele é descoberto (pedido explícito do usuário: acompanhar o processo ao vivo,
// não só o resultado final), e um evento "complete" com a lista inteira ordenada ao terminar.
func (h *NetDiscoveryHandler) runDiscovery(ctx context.Context, sessionID string, req RunNetDiscoveryRequest, userInfo history.UserInfo) {
	start := time.Now()

	send := func(evtType, phase, message string, progress float64, result interface{}) {
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      evtType,
			Phase:     phase,
			Message:   message,
			Progress:  progress,
			Timestamp: time.Now(),
			Cluster:   req.Cluster,
			Result:    result,
		})
	}

	fail := func(stage string, err error) {
		send("error", "failed", fmt.Sprintf("%s: %v", stage, err), 1.0, nil)
		h.logHistory(req, userInfo, start, nil, fmt.Errorf("%s: %w", stage, err))
	}

	send("init", "started", "Resolvendo alvo...", 0.05, nil)
	targetIP, resolved, err := resolveTarget(ctx, req.Target)
	if err != nil {
		fail("falha ao resolver alvo", err)
		return
	}

	var hops []NetDiscoveryHop
	var hopsMu sync.Mutex
	onLine := func(line string) {
		hop, ok := parseTracerouteLine(line, targetIP)
		if !ok {
			return
		}
		hopsMu.Lock()
		hops = append(hops, *hop)
		progress := 0.3 + 0.6*float64(hop.Index)/float64(netDiscoveryMaxHops)
		if progress > 0.9 {
			progress = 0.9
		}
		hopsMu.Unlock()
		send("hop", "in_progress", fmt.Sprintf("Salto %d descoberto", hop.Index), progress, *hop)
	}

	// fingerprintProbe (Fase 2) roda depois do traceroute, no MESMO pod/container já em pé —
	// evita criar um segundo pod/subir um segundo container só pra isso, e garante o mesmo
	// vantage point de rede do traceroute (importante sobretudo no modo pod: o alvo pode só ser
	// alcançável de dentro daquele cluster específico).
	var fingerprintProbe func(context.Context, string) (string, error)

	if req.Mode == netDiscoveryModeLocal {
		fingerprintProbe = runFingerprintLocal
		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s (modo local)...", targetIP), 0.2, nil)
		if err := runTracerouteLocal(ctx, targetIP, onLine); err != nil && len(hops) == 0 {
			fail("falha ao executar traceroute local", err)
			return
		}
	} else {
		clientset, err := h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			fail("falha ao conectar no cluster", err)
			return
		}
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			fail("falha ao obter configuração do cluster", err)
			return
		}

		h.seenClusters.Store(req.Cluster, struct{}{})

		send("pod_create", "in_progress", "Criando pod de descoberta...", 0.1, nil)
		podName, cleanup, err := createNetDiscoveryPod(ctx, clientset, req.Namespace)
		if err != nil {
			fail("falha ao criar pod de descoberta", err)
			return
		}
		defer cleanup()

		send("pod_wait", "in_progress", "Aguardando pod ficar pronto...", 0.15, nil)
		if err := waitPodRunning(ctx, clientset, req.Namespace, podName, netDiscoveryPodReadyTimeout); err != nil {
			fail("pod de descoberta não ficou pronto", err)
			return
		}

		fingerprintProbe = func(ctx context.Context, ip string) (string, error) {
			return runFingerprintInPod(ctx, clientset, restConfig, req.Namespace, podName, ip)
		}

		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s...", targetIP), 0.2, nil)
		if err := runTracerouteInPod(ctx, clientset, restConfig, req.Namespace, podName, targetIP, onLine); err != nil && len(hops) == 0 {
			fail("falha ao executar traceroute", err)
			return
		}
	}

	reached := len(hops) > 0 && hops[len(hops)-1].IP == targetIP

	// Fingerprint do destino (Fase 2) — best-effort, nunca bloqueia o resultado principal do
	// traceroute (mesmo espírito de outras camadas opcionais desta app, ex: contexto histórico
	// no Teste de Latência): uma falha aqui só significa "sem fingerprint", não "descoberta
	// falhou". Roda mesmo quando o destino não foi alcançado pelo traceroute (reached=false) —
	// TTL/portas/HTTP são checagens independentes, não dependem do traceroute ter completado.
	send("fingerprint", "in_progress", "Identificando o destino (SO/serviço)...", 0.92, nil)
	var fingerprint *NetDiscoveryFingerprint
	if fpOutput, fpErr := fingerprintProbe(ctx, targetIP); fpErr != nil {
		log.Warn().Str("target", targetIP).Err(fpErr).Msg("NetDiscovery: fingerprint do destino falhou (não bloqueia o resultado principal)")
	} else {
		fp := parseFingerprintOutput(fpOutput)
		fingerprint = &fp
	}

	result := NetDiscoveryResult{
		TargetInput:    req.Target,
		TargetIP:       targetIP,
		Fingerprint:    fingerprint,
		TargetResolved: resolved,
		Hops:           hops,
		Reached:        reached,
	}

	msg := fmt.Sprintf("Rota traçada: %d saltos", len(hops))
	if !reached {
		msg += " (destino não confirmado dentro do limite de saltos — ver seção 8 do plano: hops podem não responder por firewall/NAT)"
	}
	send("complete", "completed", msg, 1.0, result)
	h.logHistory(req, userInfo, start, &result, nil)
}

// logHistory registra a execução no HistoryTracker — mesmo padrão de auditoria do Teste de
// Latência (gera tráfego de rede real contra um alvo, vale trilha).
func (h *NetDiscoveryHandler) logHistory(req RunNetDiscoveryRequest, userInfo history.UserInfo, start time.Time, result *NetDiscoveryResult, opErr error) {
	if h.historyTracker == nil {
		return
	}
	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}

	after := map[string]interface{}{
		"target": req.Target,
		"mode":   req.Mode,
	}
	if result != nil {
		after["target_ip"] = result.TargetIP
		after["hop_count"] = len(result.Hops)
		after["reached"] = result.Reached
	}

	h.historyTracker.Log(history.HistoryEntry{
		UserEmail: userInfo.Email,
		UserName:  userInfo.Name,
		Action:    "net_discovery",
		Resource:  req.Target,
		Cluster:   req.Cluster,
		Status:    status,
		After:     after,
		Duration:  time.Since(start).Milliseconds(),
		ErrorMsg:  errMsg,
	})
}
