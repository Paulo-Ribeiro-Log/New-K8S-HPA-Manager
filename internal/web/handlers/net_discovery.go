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
	"k8s-hpa-manager/internal/storage"
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
	// netDiscoveryProbeTimeoutSec — DEFAULT de quanto esperar por resposta de CADA salto antes de
	// marcar como "não respondeu" e seguir pro próximo, quando o usuário não escolhe outro valor
	// (ver RunNetDiscoveryRequest.ProbeTimeoutSec). 2s é generoso o bastante pra rede corporativa/
	// VPN comum sem deixar um hop morto travar o traceroute inteiro por muito tempo.
	netDiscoveryProbeTimeoutSec = 2
	// netDiscoveryProbeTimeoutMaxSec — teto pro override do usuário. Pedido explícito do usuário
	// (descartar a hipótese de rede lenta/alta latência atrás de um cofre PAM antes de aceitar que
	// é bloqueio de verdade) — nunca ilimitado, sempre um valor finito e razoável. 8, não 10: com
	// netDiscoveryMaxHops=30, o pior caso (todos os saltos sem resposta) precisa caber dentro de
	// netDiscoveryOverallTimeoutCap com folga pras fases seguintes — achado real via teste
	// (TestComputeOverallTimeout_ExtendsForLargerProbeTimeout): 10s×30=300s já estoura o cap de
	// 280s sozinho, sem sobrar nem o buffer de 30s — exatamente o bug que este mecanismo existe
	// pra evitar, reintroduzido no próprio valor máximo permitido. 8s×30=240s+30s buffer=270s,
	// cabe com margem.
	netDiscoveryProbeTimeoutMaxSec = 8
	// netDiscoveryOverallTimeout — teto absoluto DEFAULT pro comando inteiro (30 hops × pior caso
	// de espera no timeout padrão), rede de segurança contra um traceroute que nunca termina
	// sozinho. Ver computeOverallTimeout — estende dinamicamente quando o usuário aumenta
	// ProbeTimeoutSec, senão um timeout de sonda maior faria o traceroute ser abortado no meio
	// pelo teto ANTIGO antes mesmo de terminar de tentar todos os saltos.
	netDiscoveryOverallTimeout = 90 * time.Second
	// netDiscoveryOverallTimeoutCap — nunca ultrapassado, mesmo com ProbeTimeoutSec no máximo —
	// fica abaixo de netDiscoveryPodActiveDeadline (300s) pra nunca deixar o contexto Go esperar
	// mais tempo do que o pod de descoberta (modo pod) sobrevive de qualquer forma.
	netDiscoveryOverallTimeoutCap = 280 * time.Second
)

// computeOverallTimeout estende o teto absoluto da descoberta quando o usuário pede um timeout de
// sonda maior que o default — sem isso, um ProbeTimeoutSec alto faria o pior caso (todos os
// netDiscoveryMaxHops saltos sem resposta) facilmente estourar o teto FIXO antigo (90s: 30×2s já
// usa 60s, sobrando pouca margem pras fases seguintes), abortando o traceroute no meio pelo
// contexto em vez de terminar normalmente com "não alcançado". +30s de folga cobre fingerprint/
// enrich/crossref (fases que rodam depois, best-effort, mas ainda dentro do mesmo contexto).
func computeOverallTimeout(probeTimeoutSec int) time.Duration {
	worstCase := time.Duration(probeTimeoutSec*netDiscoveryMaxHops)*time.Second + 30*time.Second
	if worstCase < netDiscoveryOverallTimeout {
		return netDiscoveryOverallTimeout
	}
	if worstCase > netDiscoveryOverallTimeoutCap {
		return netDiscoveryOverallTimeoutCap
	}
	return worstCase
}

// NetDiscoveryHop é um salto do caminho de rede — Fase 1 só tem o essencial (índice/IP/latência/
// se respondeu); campos de enriquecimento (DNS reverso, ASN, cloud, recurso K8s conhecido,
// fingerprint de SO) chegam nas Fases 2-4 sem precisar mudar este contrato, só adicionar campos.
type NetDiscoveryHop struct {
	Index    int     `json:"index"`
	IP       string  `json:"ip,omitempty"` // vazio = hop não respondeu ("* * *")
	RTTMs    float64 `json:"rtt_ms,omitempty"`
	TimedOut bool    `json:"timed_out"`
	IsTarget bool    `json:"is_target"` // true quando este hop é o próprio destino resolvido

	// Enriquecimento passivo (Fase 3, net_discovery_enrich.go) — SEMPRE vazios no evento SSE
	// "hop" ao vivo (enriquecimento roda só depois de todos os saltos coletados, best-effort);
	// preenchidos só na lista final de NetDiscoveryResult.Hops, entregue no evento "complete".
	ReverseDNS  string `json:"reverse_dns,omitempty"`
	ASN         string `json:"asn,omitempty"`
	ASNOrg      string `json:"asn_org,omitempty"`
	CloudMatch  string `json:"cloud_match,omitempty"` // "aws" | "gcp" | ""
	CloudRegion string `json:"cloud_region,omitempty"`

	// InternalRef — cross-reference com a frota K8s (Fase 4, net_discovery_crossref.go).
	// Enriquecimento BÔNUS, nunca pré-requisito (seção 3.8 do plano): preenchido só quando o IP
	// bate com um Node/Pod/Service conhecido — do cache persistido (qualquer modo) ou de uma
	// busca ao vivo (só modo pod, onde já existe um clientset/cluster real em mãos).
	InternalRef *NetDiscoveryInternalRef `json:"internal_ref,omitempty"`
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

// netDiscoveryTCPPort — porta DEFAULT usada pelas sondas TCP do tcptraceroute quando o usuário não
// escolhe outra explicitamente. 443 escolhida como default (HTTPS é o serviço mais universalmente
// exposto hoje) — não confirma nem descarta que o destino tenha essa porta aberta de verdade;
// tcptraceroute mostra "[open]"/"[closed]" no salto final quando consegue determinar, mas o
// CAMINHO em si (o que interessa nesta Fase 1) já é obtido mesmo que a porta não esteja aberta —
// cada salto intermediário responde ao TTL expirado independente do estado da porta final.
//
// Bug real corrigido — relatado ao vivo pelo usuário: "rodando contra servidores windows dentro
// do delinea vault falha miseravelmente" (traceroute nunca alcançava o destino, SO detectado
// errado). Causa: 443 fixo funciona bem pra alvos web/Linux (a maioria), mas um servidor Windows
// típico atrás de um cofre PAM (Delinea, CyberArk, etc.) raramente tem 443 aberto — só as portas
// de acesso remoto (3389 RDP, 445 SMB, 5985/5986 WinRM), tipicamente com QUALQUER outra porta
// filtrada/derrubada silenciosamente por firewall. Uma sonda TCP na porta 443 contra esse alvo
// nunca recebe resposta (nem RST de "porta fechada", nem nada) — indistinguível de "hop
// inexistente", então o ÚLTIMO salto (o próprio destino) aparece como timeout mesmo com o host
// vivo e alcançável por outras portas; sem alcançar o destino via TCP, o fingerprint (Fase 2)
// também fica sem sinal de porta pra confirmar Windows, caindo no fallback de TTL — que, se algo
// no caminho (o próprio gateway do cofre, por exemplo) responder ao ping com uma TTL "tipo Linux",
// produz um palpite de SO ERRADO em vez de simplesmente "sem sinal". Corrigido dando ao usuário
// controle sobre a porta da sonda (nunca adivinhando automaticamente uma topologia de rede que
// este código não tem como observar) — `RunNetDiscoveryRequest.ProbePort` (opcional, 0 = usa o
// default 443 sem nenhuma mudança de comportamento pro caso comum).
const netDiscoveryTCPPort = 443

// tracerouteArgs monta os argumentos do tcptraceroute — mesmos nos dois modos (pod/local), só o
// mecanismo de execução muda. `port` já vem resolvido pelo chamador (default ou escolha do
// usuário, ver netDiscoveryTCPPort).
//
// Achado real, testado ao vivo contra a imagem netshoot: o `traceroute` de dentro dela é o applet
// do BusyBox, que NÃO tem `-T` (modo TCP) — só UDP (default) ou `-I` (ICMP). `tcptraceroute` é um
// binário PRÓPRIO e separado, também presente na mesma imagem, propositalmente feito pra
// traceroute via TCP (mais provável de atravessar firewall corporativo/NSG de nuvem do que UDP/
// ICMP puro, ver seção 3.1 do plano) — usa ele diretamente em vez de tentar simular com o
// traceroute genérico. Confirmado ao vivo contra 8.8.8.8: alcançou o destino com sucesso; contra
// um alvo inalcançável, devolve exit code 1 mas ainda assim imprime os hops que respondeu — por
// isso o chamador (runDiscovery) só considera falha total quando NENHUM hop foi coletado.
func tracerouteArgs(targetIP string, port, probeTimeoutSec int) []string {
	return []string{
		"tcptraceroute", "-n",
		"-w", strconv.Itoa(probeTimeoutSec),
		"-q", "1", // 1 sonda por salto — favorece velocidade/fluidez da animação sobre riqueza estatística (Fase 1)
		"-m", strconv.Itoa(netDiscoveryMaxHops),
		targetIP, strconv.Itoa(port),
	}
}

// runTracerouteInPod executa tcptraceroute dentro do pod de teste via exec SPDY, chamando `onLine`
// pra cada linha de stdout conforme ela chega (ver streamCommandLines). Mesma limitação de
// CAP_NET_RAW já documentada pro modo ICMP do Teste de Latência (runICMPProbe) se aplica aqui
// também, herdada da necessidade de ler respostas ICMP Time-Exceeded via socket raw — não
// contornável por este código.
func runTracerouteInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, targetIP string, port, probeTimeoutSec int, onLine func(line string)) error {

	return streamCommandLines(ctx, func(stdout io.Writer) error {
		return execCmdInPodStreaming(ctx, clientset, restConfig, namespace, podName, netDiscoveryPodContainer, tracerouteArgs(targetIP, port, probeTimeoutSec), stdout)
	}, onLine)
}

// runTracerouteLocal — mesma lógica, mas via `docker run` no host (modo local, ver
// db_test_docker.go pro precheck de Docker já compartilhado). `--network host` — precisa da rede
// real do host pra alcançar destinos remotos (ex: infraestrutura na VPN corporativa até a Kyndryl,
// confirmado pelo usuário como sendo a mesma VPN já usada pro AKS/GCP), um container isolado na
// rede bridge padrão do Docker não teria essa rota.
func runTracerouteLocal(ctx context.Context, targetIP string, port, probeTimeoutSec int, onLine func(line string)) error {
	// Nome explícito (não só --rm) — achado real, testado ao vivo: cancelar a descoberta
	// (context cancelado) mata o CLIENTE `docker run` via SIGKILL, mas isso não garante que o
	// daemon pare o CONTAINER remoto (--rm só roda quando o cliente sai limpo, não quando é
	// morto abruptamente) — mesma classe de bug já documentada pro Teste de Banco de Dados/Kafka
	// (cleanupCancelledDockerContainer). Sem um nome conhecido, não haveria como limpar
	// ativamente — só o reaper periódico (até 10min depois) resolveria.
	containerName := fmt.Sprintf("net-discovery-trace-%s", uuid.New().String()[:8])
	args := append([]string{
		"run", "--rm", "--network=host",
		"--name", containerName,
		"--label", netDiscoveryDockerLabel,
		netDiscoveryPodImage,
	}, tracerouteArgs(targetIP, port, probeTimeoutSec)...)

	err := streamCommandLines(ctx, func(stdout io.Writer) error {
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

	if ctx.Err() != nil {
		cleanupCancelledDockerContainer(containerName)
	}
	return err
}

// ─── Handler: endpoint SSE + rotas ─────────────────────────────────────────────

// NetDiscoveryHandler orquestra a "Descoberta de Rede" sob demanda — mesmo padrão de
// LatencyTestHandler (start retorna session_id, cliente conecta no stream, cancel força parada).
type NetDiscoveryHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	registry       *storage.NetDiscoveryRegistryStore // Fase 4 — cache-on-read do cross-reference K8s; nil desabilita a camada sem quebrar nada (best-effort)
	cancelFuncs    sync.Map                           // sessionID -> context.CancelFunc
	runningUsers   sync.Map                           // userEmail -> struct{} — "uma descoberta por vez por usuário"
	seenClusters   sync.Map                           // cluster -> struct{} — só varre órfãos onde este handler já criou pod
}

// NewNetDiscoveryHandler cria o handler e inicia a varredura periódica de pods/containers órfãos
// em background (mesmo padrão de NewLatencyTestHandler/NewDBTestHandler).
func NewNetDiscoveryHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker, registry *storage.NetDiscoveryRegistryStore) *NetDiscoveryHandler {
	h := &NetDiscoveryHandler{kubeManager: km, tracker: tracker, historyTracker: ht, registry: registry}
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
	// ProbePort — porta TCP usada pelas sondas do tcptraceroute. 0 (ou ausente) usa o default
	// netDiscoveryTCPPort=443, sem mudança de comportamento pro caso comum (web/Linux). Ver
	// comentário completo em netDiscoveryTCPPort — override necessário pra alvos onde 443 está
	// filtrado (ex: servidor Windows atrás de cofre PAM, só com 3389/445/5985/5986 abertos).
	ProbePort int `json:"probe_port,omitempty"`
	// ProbeTimeoutSec — segundos de espera por resposta de CADA salto antes de seguir pro
	// próximo. 0 (ou ausente) usa o default netDiscoveryProbeTimeoutSec=2. Pedido explícito do
	// usuário: descartar a hipótese de rede lenta/alta latência antes de aceitar que um alvo é
	// genuinamente bloqueado (ver computeOverallTimeout — o teto geral da descoberta se estende
	// automaticamente quando este valor sobe, pra não abortar o traceroute no meio).
	ProbeTimeoutSec int `json:"probe_timeout_sec,omitempty"`
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
	if req.ProbePort == 0 {
		req.ProbePort = netDiscoveryTCPPort
	} else if req.ProbePort < 1 || req.ProbePort > 65535 {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_PROBE_PORT", "probe_port deve estar entre 1 e 65535"))
		return
	}
	if req.ProbeTimeoutSec == 0 {
		req.ProbeTimeoutSec = netDiscoveryProbeTimeoutSec
	} else if req.ProbeTimeoutSec < 1 || req.ProbeTimeoutSec > netDiscoveryProbeTimeoutMaxSec {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_PROBE_TIMEOUT",
			fmt.Sprintf("probe_timeout_sec deve estar entre 1 e %d", netDiscoveryProbeTimeoutMaxSec)))
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
	ctx, cancel := context.WithTimeout(context.Background(), computeOverallTimeout(req.ProbeTimeoutSec))
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

	// sniHost/originalHostname — hostname ORIGINAL digitado pelo usuário, quando a busca foi por
	// hostname (não por IP direto). Usado em dois pontos que precisam de um nome real, não só o
	// IP: o fingerprint HTTP/TLS (SNI/Host virtual-hosting, ver net_discovery_fingerprint.go — bug
	// real corrigido, "certificados sempre fake") e o enriquecimento de DNS reverso do salto-alvo
	// (ver enrichHops — bug real corrigido, "hostname incorreto atrás de bastion/cofre"). Vazio
	// quando `resolved==false` (a busca já era por IP puro, sem hostname real disponível).
	sniHost := targetIP
	originalHostname := ""
	if resolved {
		originalHostname = strings.TrimSpace(req.Target)
		sniHost = originalHostname
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

	// podClientset — capturado só no modo pod (Fase 4: cross-reference K8s). Permanece nil no
	// modo local; crossReferenceHops trata nil como "só consultar o cache persistido, nunca
	// disparar busca ao vivo" (ver comentário de crossReferenceIP em net_discovery_crossref.go) —
	// por isso a mesma chamada, mais abaixo, cobre os dois modos sem precisar de um `if` extra.
	var podClientset kubernetes.Interface

	if req.Mode == netDiscoveryModeLocal {
		fingerprintProbe = func(ctx context.Context, ip string) (string, error) {
			return runFingerprintLocal(ctx, ip, sniHost)
		}
		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s (modo local, porta %d, timeout %ds/salto)...", targetIP, req.ProbePort, req.ProbeTimeoutSec), 0.2, nil)
		if err := runTracerouteLocal(ctx, targetIP, req.ProbePort, req.ProbeTimeoutSec, onLine); err != nil && len(hops) == 0 {
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
		podClientset = clientset

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
			return runFingerprintInPod(ctx, clientset, restConfig, req.Namespace, podName, ip, sniHost)
		}

		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s (porta %d, timeout %ds/salto)...", targetIP, req.ProbePort, req.ProbeTimeoutSec), 0.2, nil)
		if err := runTracerouteInPod(ctx, clientset, restConfig, req.Namespace, podName, targetIP, req.ProbePort, req.ProbeTimeoutSec, onLine); err != nil && len(hops) == 0 {
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

	// Enriquecimento passivo por salto (Fase 3) — DNS reverso/ASN/faixa de nuvem, sempre do
	// backend (nunca precisa do pod/container, são consultas DNS/HTTP simples). Muta `hops`
	// in-place; roda mesmo se o fingerprint acima falhou (camadas independentes, uma não bloqueia
	// a outra).
	send("enrich", "in_progress", "Enriquecendo rota (DNS reverso, ASN, nuvem)...", 0.97, nil)
	enrichHops(ctx, hops, originalHostname)

	// Cross-reference K8s (Fase 4) — última camada, sempre best-effort. `podClientset` é nil no
	// modo local; crossReferenceHops/crossReferenceIP tratam isso consultando só o cache
	// persistido, nunca disparando uma busca ao vivo (ver net_discovery_crossref.go).
	send("crossref", "in_progress", "Cruzando com clusters K8s conhecidos...", 0.99, nil)
	h.crossReferenceHops(ctx, hops, podClientset, req.Cluster)

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
