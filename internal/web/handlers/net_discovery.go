package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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
	// netDiscoveryProbeCount — DEFAULT de quantas sondas TCP por salto (-q do tcptraceroute) quando
	// o usuário não escolhe outro valor. Fase A do roadmap de maturidade profissional
	// (IP-ROUTE-DISCOVERY-PLAN.md): 1 sonda (valor original desta feature) não permite distinguir
	// "hop lento" de "hop com perda intermitente de pacote" — o padrão de diagnóstico esperado em
	// qualquer ferramenta de rede séria (mtr, ThousandEyes) é reportar perda % e variação de
	// latência por hop, o que exige mais de 1 amostra. 3 é o mesmo valor convencional já usado por
	// ping/mtr.
	netDiscoveryProbeCount = 3
	// netDiscoveryProbeCountMax — teto pro override do usuário. Mantido conservador (não maior): o
	// pior caso do traceroute cresce LINEARMENTE com o número de sondas (ver
	// computeOverallTimeout) — um valor mais alto combinado com ProbeTimeoutSec no máximo
	// ultrapassaria netDiscoveryOverallTimeoutCap num cenário só moderadamente adverso, não só no
	// pior caso patológico (todos os saltos 100% silenciosos). Aceito como trade-off: combos
	// extremos (ProbeTimeoutSec E ProbeCount ambos no máximo) ainda podem bater no cap antes de
	// esgotar o pior caso teórico — mesmo tipo de trade-off já aceito só para ProbeTimeoutSec antes
	// desta fase (ver netDiscoveryOverallTimeoutCap abaixo), não uma classe nova de risco.
	netDiscoveryProbeCountMax = 3
	// netDiscoveryOverallTimeout — teto absoluto DEFAULT pro comando inteiro (30 hops × pior caso
	// de espera no timeout padrão × sondas por salto), rede de segurança contra um traceroute que
	// nunca termina sozinho. Ver computeOverallTimeout — estende dinamicamente quando o usuário
	// aumenta ProbeTimeoutSec/ProbeCount, senão um timeout/contagem de sonda maior faria o
	// traceroute ser abortado no meio pelo teto ANTIGO antes mesmo de terminar de tentar todos os
	// saltos.
	netDiscoveryOverallTimeout = 90 * time.Second
	// netDiscoveryOverallTimeoutCap — nunca ultrapassado, mesmo com ProbeTimeoutSec no máximo —
	// fica abaixo de netDiscoveryPodActiveDeadline (300s) pra nunca deixar o contexto Go esperar
	// mais tempo do que o pod de descoberta (modo pod) sobrevive de qualquer forma.
	netDiscoveryOverallTimeoutCap = 280 * time.Second
)

// computeOverallTimeout estende o teto absoluto da descoberta quando o usuário pede um timeout ou
// número de sondas por salto maior que o default — sem isso, um ProbeTimeoutSec/ProbeCount alto
// faria o pior caso (todos os netDiscoveryMaxHops saltos, cada um com probeCount sondas, sem
// resposta) facilmente estourar o teto FIXO antigo, abortando o traceroute no meio pelo contexto em
// vez de terminar normalmente com "não alcançado". +30s de folga cobre fingerprint/enrich/crossref
// (fases que rodam depois, best-effort, mas ainda dentro do mesmo contexto).
func computeOverallTimeout(probeTimeoutSec, probeCount int) time.Duration {
	worstCase := time.Duration(probeTimeoutSec*netDiscoveryMaxHops*probeCount)*time.Second + 30*time.Second
	if worstCase < netDiscoveryOverallTimeout {
		return netDiscoveryOverallTimeout
	}
	if worstCase > netDiscoveryOverallTimeoutCap {
		return netDiscoveryOverallTimeoutCap
	}
	return worstCase
}

// netDiscoveryTargetTimeout — teto pra UM alvo, já incorporando o orçamento extra da detecção
// avançada de serviço (nmap, opt-in — ver net_discovery_nmap.go) quando ligada. O cap
// (netDiscoveryOverallTimeoutCap) é reaplicado DEPOIS de somar o orçamento extra — nunca deixa o
// total ultrapassar o mesmo teto absoluto já usado pras outras dimensões (garante que o contexto
// Go nunca espera mais do que o pod de descoberta sobrevive, mesmo com AdvancedServiceScan
// ligado). Extraída pra ser reaproveitada por Run() (net_discovery.go) e RunBatch()
// (net_discovery_batch.go) sem duplicar a mesma soma+cap nos dois lugares.
func netDiscoveryTargetTimeout(probeTimeoutSec, probeCount int, advancedServiceScan bool) time.Duration {
	total := computeOverallTimeout(probeTimeoutSec, probeCount)
	if advancedServiceScan {
		total += netDiscoveryAdvancedScanBudget
		if total > netDiscoveryOverallTimeoutCap {
			total = netDiscoveryOverallTimeoutCap
		}
	}
	return total
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

	// LossPct/RTTMinMs/RTTMaxMs/ProbesSent/ProbesReceived — Fase A do roadmap de maturidade
	// profissional (IP-ROUTE-DISCOVERY-PLAN.md): com múltiplas sondas por salto (ver
	// tracerouteArgs/RunNetDiscoveryRequest.ProbeCount), um hop passa a ter estatística real em vez
	// de uma única amostra — distingue "hop lento" (RTT alto, LossPct=0) de "hop com perda
	// intermitente" (LossPct>0 e <100, mesmo com RTT baixo nas sondas que responderam). LossPct
	// omitido (zero value) quando não há perda — 0% é o caso comum, não vale poluir o JSON.
	LossPct        float64 `json:"loss_pct,omitempty"`
	RTTMinMs       float64 `json:"rtt_min_ms,omitempty"`
	RTTMaxMs       float64 `json:"rtt_max_ms,omitempty"`
	ProbesSent     int     `json:"probes_sent,omitempty"`
	ProbesReceived int     `json:"probes_received,omitempty"`

	// Enriquecimento passivo (Fase 3, net_discovery_enrich.go) — SEMPRE vazios no evento SSE
	// "hop" ao vivo (enriquecimento roda só depois de todos os saltos coletados, best-effort);
	// preenchidos só na lista final de NetDiscoveryResult.Hops, entregue no evento "complete".
	ReverseDNS  string `json:"reverse_dns,omitempty"`
	ASN         string `json:"asn,omitempty"`
	ASNOrg      string `json:"asn_org,omitempty"`
	CloudMatch  string `json:"cloud_match,omitempty"` // "aws" | "gcp" | "azure" | ""
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

// tracerouteFloatToken casa um token de valor numérico isolado (ex: "1.234", sempre seguido de
// "ms" como token separado na saída do tcptraceroute) — usado pelo parser token-a-token de
// parseTracerouteLine (Fase A), não mais uma regex solta sobre a linha inteira (ver comentário
// completo na função).
var tracerouteFloatToken = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
var tracerouteIPToken = regexp.MustCompile(`^([0-9]{1,3}(?:\.[0-9]{1,3}){3}|[0-9a-fA-F:]+)$`)

// parseTracerouteLine extrai um NetDiscoveryHop de uma linha de stdout do tcptraceroute, ou
// (nil, false) se a linha não for reconhecida como linha de salto (ex: o cabeçalho
// "traceroute to X (X), 30 hops max..." que o comando imprime antes do primeiro salto).
//
// Fase A do roadmap de maturidade profissional (IP-ROUTE-DISCOVERY-PLAN.md): com -q > 1 (múltiplas
// sondas por salto, ver tracerouteArgs), cada linha carrega N resultados de sonda em sequência — um
// "X.Y ms" por sonda que respondeu, um "*" solto por sonda que não respondeu — precedidos, quando
// ao menos uma sonda respondeu, pelo IP do salto (impresso uma única vez, não repetido a cada sonda
// subsequente da mesma linha, confirmado pelo formato real já coberto pelos testes desta função:
// TestParseTracerouteLine_MixedProbesInSameHop). O parser caminha TOKEN A TOKEN em vez de só somar
// todos os valores "ms" soltos na linha inteira (como a v1 desta função fazia via regex —
// tracerouteRTTRegex, removida) — sem isso não dá pra saber QUANTAS sondas foram de fato enviadas
// (e portanto não dá pra calcular perda %), só a média das que responderam.
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

	i := 0
	if tracerouteIPToken.MatchString(fields[0]) {
		hop.IP = fields[0]
		hop.IsTarget = hop.IP == targetIP
		i = 1
	}

	var rtts []float64
	probesSent := 0
	for i < len(fields) {
		tok := fields[i]
		switch {
		case tok == "*":
			// Sonda enviada, sem resposta.
			probesSent++
			i++
		case tracerouteFloatToken.MatchString(tok) && i+1 < len(fields) && fields[i+1] == "ms":
			// Sonda enviada, respondeu com este RTT.
			if v, err := strconv.ParseFloat(tok, 64); err == nil {
				rtts = append(rtts, v)
				probesSent++
			}
			i += 2
		case hop.IP == "" && tracerouteIPToken.MatchString(tok):
			// Caso raro, não coberto pelos exemplos reais já testados: a 1ª sonda da linha não
			// respondeu (sem IP ainda) mas uma sonda posterior respondeu — o IP só aparece na
			// primeira sonda que de fato respondeu, não necessariamente na primeira da linha.
			hop.IP = tok
			hop.IsTarget = hop.IP == targetIP
			i++
		default:
			// Token desconhecido (ex: sufixo "[open]"/"[closed]" que o tcptraceroute anexa no
			// salto final quando consegue determinar o estado da porta, ver
			// TestParseTracerouteLine_TcptracerouteOpenPortSuffix) — ignora sem interromper o
			// parsing do restante da linha.
			i++
		}
	}

	hop.ProbesSent = probesSent
	hop.ProbesReceived = len(rtts)
	if probesSent > 0 {
		hop.LossPct = 100 * float64(probesSent-len(rtts)) / float64(probesSent)
	}
	// TimedOut = nem o IP foi identificado, nem nenhuma sonda respondeu — mesma semântica de
	// antes desta fase (hop "* * *" completo), preservada mesmo com perda parcial contando à parte
	// via LossPct (um hop com 1 de 3 sondas respondendo NÃO é TimedOut, é um hop com 66% de perda).
	hop.TimedOut = hop.IP == "" && len(rtts) == 0

	if len(rtts) > 0 {
		sum, min, max := 0.0, rtts[0], rtts[0]
		for _, v := range rtts {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		hop.RTTMs = sum / float64(len(rtts))
		hop.RTTMinMs = min
		hop.RTTMaxMs = max
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
func tracerouteArgs(targetIP string, port, probeTimeoutSec, probeCount int) []string {
	return []string{
		"tcptraceroute", "-n",
		"-w", strconv.Itoa(probeTimeoutSec),
		"-q", strconv.Itoa(probeCount), // Fase A: sondas/salto configuráveis (default 3, ver netDiscoveryProbeCount) — permite loss%/jitter reais, não mais fixo em 1
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
	namespace, podName, targetIP string, port, probeTimeoutSec, probeCount int, onLine func(line string)) error {

	return streamCommandLines(ctx, func(stdout io.Writer) error {
		return execCmdInPodStreaming(ctx, clientset, restConfig, namespace, podName, netDiscoveryPodContainer, tracerouteArgs(targetIP, port, probeTimeoutSec, probeCount), stdout)
	}, onLine)
}

// runTracerouteLocal — mesma lógica, mas via `docker run` no host (modo local, ver
// db_test_docker.go pro precheck de Docker já compartilhado). `--network host` — precisa da rede
// real do host pra alcançar destinos remotos (ex: infraestrutura na VPN corporativa até a Kyndryl,
// confirmado pelo usuário como sendo a mesma VPN já usada pro AKS/GCP), um container isolado na
// rede bridge padrão do Docker não teria essa rota.
func runTracerouteLocal(ctx context.Context, targetIP string, port, probeTimeoutSec, probeCount int, onLine func(line string)) error {
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
	}, tracerouteArgs(targetIP, port, probeTimeoutSec, probeCount)...)

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
	historyStore   *storage.NetDiscoveryHistoryStore  // Fase 5 — histórico de descobertas por alvo; nil desabilita persistência/consulta sem quebrar o fluxo principal (best-effort)
	cancelFuncs    sync.Map                           // sessionID -> context.CancelFunc
	runningUsers   sync.Map                           // userEmail -> struct{} — "uma descoberta por vez por usuário"
	seenClusters   sync.Map                           // cluster -> struct{} — só varre órfãos onde este handler já criou pod
}

// NewNetDiscoveryHandler cria o handler e inicia a varredura periódica de pods/containers órfãos
// em background (mesmo padrão de NewLatencyTestHandler/NewDBTestHandler).
func NewNetDiscoveryHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker, registry *storage.NetDiscoveryRegistryStore, historyStore *storage.NetDiscoveryHistoryStore) *NetDiscoveryHandler {
	h := &NetDiscoveryHandler{kubeManager: km, tracker: tracker, historyTracker: ht, registry: registry, historyStore: historyStore}
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

// netDiscoveryHistoryLimit — quantas execuções passadas devolver por consulta (seção 10.1 do
// plano: "últimas N, ex: 3"). Suficiente pro banner de contexto sem virar uma listagem completa.
const netDiscoveryHistoryLimit = 3

// NetDiscoveryHistoryEntry é uma execução passada, pronta pro frontend — `Result` já vem
// desserializado (não o JSON cru como string) pra reaproveitar direto os mesmos componentes que
// já sabem renderizar um `NetDiscoveryResult` (fingerprint, tabela de saltos, etc.), sem o
// frontend precisar fazer um segundo `JSON.parse`.
type NetDiscoveryHistoryEntry struct {
	TargetInput string              `json:"target_input"`
	TargetIP    string              `json:"target_ip"`
	Mode        string              `json:"mode"`
	Reached     bool                `json:"reached"`
	HopsCount   int                 `json:"hops_count"`
	CreatedAt   time.Time           `json:"created_at"`
	CreatedBy   string              `json:"created_by,omitempty"`
	Result      *NetDiscoveryResult `json:"result,omitempty"`
}

// History — GET /api/v1/net-discovery/history?target=<texto>. Devolve as últimas execuções
// conhecidas pra um alvo (Fase 5 — Histórico de Descobertas, IP-ROUTE-DISCOVERY-PLAN.md seção
// 10.1) — pra que o frontend mostre "última busca: ..." antes mesmo do usuário rodar uma nova
// descoberta. Sem RequireSREGroup() (é consulta, mesmo padrão de leitura do resto desta
// ferramenta). `historyStore == nil` (falhou ao inicializar) ou alvo nunca visto devolvem lista
// vazia — nunca erro, best-effort igual às outras camadas desta ferramenta.
func (h *NetDiscoveryHandler) History(c *gin.Context) {
	target := strings.TrimSpace(c.Query("target"))
	if target == "" || h.historyStore == nil {
		c.JSON(http.StatusOK, gin.H{"entries": []NetDiscoveryHistoryEntry{}})
		return
	}

	records, err := h.historyStore.GetRecentByTarget(target, netDiscoveryHistoryLimit)
	if err != nil {
		log.Warn().Str("target", target).Err(err).Msg("NetDiscovery: falha ao consultar histórico (não bloqueia a UI)")
		c.JSON(http.StatusOK, gin.H{"entries": []NetDiscoveryHistoryEntry{}})
		return
	}

	entries := make([]NetDiscoveryHistoryEntry, 0, len(records))
	for _, r := range records {
		entry := NetDiscoveryHistoryEntry{
			TargetInput: r.TargetInput,
			TargetIP:    r.TargetIP,
			Mode:        r.Mode,
			Reached:     r.Reached,
			HopsCount:   r.HopsCount,
			CreatedAt:   r.CreatedAt,
			CreatedBy:   r.CreatedBy,
		}
		var result NetDiscoveryResult
		if jsonErr := json.Unmarshal([]byte(r.ResultJSON), &result); jsonErr == nil {
			entry.Result = &result
		}
		entries = append(entries, entry)
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
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
	// ProbeCount — quantas sondas TCP por salto (-q do tcptraceroute). 0 (ou ausente) usa o default
	// netDiscoveryProbeCount=3. Fase A do roadmap de maturidade profissional: mais de 1 sonda
	// permite calcular perda de pacote (%) e faixa de latência (min/max) por salto, não só uma
	// única amostra — ver comentário completo em netDiscoveryProbeCount/parseTracerouteLine.
	ProbeCount int `json:"probe_count,omitempty"`
	// ClientCertPEM/ClientKeyPEM — certificado de cliente opcional pra mTLS (Fase 2, pedido
	// explícito do usuário depois de perguntar se ter o certificado "que já existe nesses
	// clusters/servidores" ajudaria a descoberta). Só faz diferença quando o alvo exige
	// `ClientAuth: RequireAnyClientCert` — achado real (ver comentário completo em
	// netDiscoveryFingerprintScript): o ganho confirmado é destravar a checagem HTTP
	// (header Server:/status), não necessariamente subject/issuer do TLS, que na maioria dos
	// terminadores já vem antes do servidor checar o certificado do cliente. Deliberadamente NUNCA
	// persistido em nenhum lugar (nem SQLite, nem localStorage no frontend) — transitório, só pra
	// esta execução; ver validateClientCertPair/buildMTLSStdinPayload pra como o par viaja até o
	// script sem tocar argv/log. Os dois vazios (caso comum) = sem mTLS, comportamento idêntico ao
	// de antes desta feature.
	ClientCertPEM string `json:"client_cert_pem,omitempty"`
	ClientKeyPEM  string `json:"client_key_pem,omitempty"`
	// ExtraPorts — Fase D do roadmap de maturidade profissional: portas extras que o usuário pede
	// pra verificar no fingerprint do destino, além das ~18 portas curadas já checadas por padrão
	// (netDiscoveryFingerprintPorts) — útil pra troubleshooting de uma aplicação específica cuja
	// porta não está na lista fixa (ex: 8081, 9000). Opcional, no máximo netDiscoveryExtraPortsMax.
	ExtraPorts []int `json:"extra_ports,omitempty"`
	// AdvancedServiceScan — detecção avançada de serviço via nmap (-sT -sV), OPT-IN explícito.
	// Roda DEPOIS do fingerprint rápido de sempre, só nas portas que ele já confirmou abertas.
	// Nunca ligado por padrão — ver avaliação/decisão completa em net_discovery_nmap.go (achado
	// decisivo: nmap tem um piso fixo de ~7s por invocação, 3-4x mais lento que o fingerprint
	// padrão, inaceitável como parte do fluxo rápido desta ferramenta).
	AdvancedServiceScan bool `json:"advanced_service_scan,omitempty"`
}

const (
	netDiscoveryModePod   = "pod"
	netDiscoveryModeLocal = "local"
)

// normalizeProbeSettings valida/normaliza probePort, probeTimeoutSec e probeCount (0 = default) —
// extraída de dentro de Run() na Fase 5 (item P4, lote de múltiplos alvos) pra ser reaproveitada
// por RunBatch sem duplicar a mesma checagem de faixa nos dois handlers. probeCount adicionado na
// Fase A do roadmap de maturidade profissional (múltiplas sondas por salto).
func normalizeProbeSettings(probePort, probeTimeoutSec, probeCount int) (port, timeoutSec, count int, errCode, errMsg string) {
	port = probePort
	if port == 0 {
		port = netDiscoveryTCPPort
	} else if port < 1 || port > 65535 {
		return 0, 0, 0, "INVALID_PROBE_PORT", "probe_port deve estar entre 1 e 65535"
	}

	timeoutSec = probeTimeoutSec
	if timeoutSec == 0 {
		timeoutSec = netDiscoveryProbeTimeoutSec
	} else if timeoutSec < 1 || timeoutSec > netDiscoveryProbeTimeoutMaxSec {
		return 0, 0, 0, "INVALID_PROBE_TIMEOUT", fmt.Sprintf("probe_timeout_sec deve estar entre 1 e %d", netDiscoveryProbeTimeoutMaxSec)
	}

	count = probeCount
	if count == 0 {
		count = netDiscoveryProbeCount
	} else if count < 1 || count > netDiscoveryProbeCountMax {
		return 0, 0, 0, "INVALID_PROBE_COUNT", fmt.Sprintf("probe_count deve estar entre 1 e %d", netDiscoveryProbeCountMax)
	}
	return port, timeoutSec, count, "", ""
}

// validateClientCertPair confirma, ANTES de sequer iniciar a descoberta, que o par cert+chave
// (mTLS opcional) forma um par TLS válido — falha rápido com erro claro em vez de deixar o erro
// aparecer só dentro do script bash horas depois (ilegível pro usuário, indistinguível de "o
// servidor não respondeu"). Também garante que nenhum dos dois PEMs contém, por acidente, uma
// linha idêntica ao marcador usado pra separar os dois blocos no stdin
// (netDiscoveryMTLSSplitMarker) — extremamente improvável (marcador só usado por esta ferramenta),
// mas garantiria corrupção silenciosa do split se acontecesse.
func validateClientCertPair(certPEM, keyPEM string) error {
	if strings.Contains(certPEM, netDiscoveryMTLSSplitMarker) || strings.Contains(keyPEM, netDiscoveryMTLSSplitMarker) {
		return fmt.Errorf("certificado ou chave contém uma linha reservada internamente — não deveria acontecer com um PEM normal")
	}
	if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
		return fmt.Errorf("certificado/chave de cliente inválidos: %w", err)
	}
	return nil
}

// validateClientCertRequest — mesmo padrão de normalizeProbeSettings (extraída pra ser
// reaproveitada por Run() e RunBatch() sem duplicar a mesma checagem nos dois handlers; achado
// real de code review: essa checagem de mTLS estava duplicada byte-a-byte enquanto
// normalizeProbeSettings já tinha sido extraída exatamente pra evitar esse padrão nos campos
// vizinhos probe_port/probe_timeout_sec). Devolve errCode/errMsg vazios quando não há nada a
// reportar (par vazio ou par válido).
func validateClientCertRequest(certPEM, keyPEM string) (errCode, errMsg string) {
	if (certPEM != "") != (keyPEM != "") {
		return "INVALID_CLIENT_CERT", "certificado e chave de cliente (mTLS) devem ser fornecidos juntos, ou nenhum dos dois"
	}
	if certPEM != "" {
		if err := validateClientCertPair(certPEM, keyPEM); err != nil {
			return "INVALID_CLIENT_CERT", err.Error()
		}
	}
	return "", ""
}

// validateExtraPorts — Fase D do roadmap de maturidade profissional: valida as portas extras
// pedidas pelo usuário pro fingerprint (RunNetDiscoveryRequest.ExtraPorts) antes de sequer montar o
// argv do script — cada porta precisa ser um TCP/UDP port válido (1-65535), e o total não pode
// ultrapassar netDiscoveryExtraPortsMax (evita alongar demais o loop paralelo do script e limita
// abuso). Mesmo padrão de normalizeProbeSettings/validateClientCertRequest — extraída pra ser
// reaproveitada por Run() e RunBatch() sem duplicar a checagem.
func validateExtraPorts(ports []int) (errCode, errMsg string) {
	if len(ports) > netDiscoveryExtraPortsMax {
		return "INVALID_EXTRA_PORTS", fmt.Sprintf("no máximo %d portas extras", netDiscoveryExtraPortsMax)
	}
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return "INVALID_EXTRA_PORTS", fmt.Sprintf("porta extra inválida: %d (deve estar entre 1 e 65535)", p)
		}
	}
	return "", ""
}

// buildMTLSStdinPayload monta o único stream de stdin entregue ao script (cert + marcador + chave)
// — ver netDiscoveryFingerprintScript sobre por que o par nunca vai como argv. `certPEM == ""`
// (mTLS não configurado, caso comum) devolve um leitor vazio — o exec recebe EOF imediato e o
// script sequer tenta ler stdin nesse caso.
func buildMTLSStdinPayload(certPEM, keyPEM string) io.Reader {
	if certPEM == "" || keyPEM == "" {
		return strings.NewReader("")
	}
	cert := strings.TrimRight(certPEM, "\n")
	key := strings.TrimRight(keyPEM, "\n")
	return strings.NewReader(cert + "\n" + netDiscoveryMTLSSplitMarker + "\n" + key + "\n")
}

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
	probePort, probeTimeoutSec, probeCount, errCode, errMsg := normalizeProbeSettings(req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount)
	if errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}
	req.ProbePort = probePort
	req.ProbeTimeoutSec = probeTimeoutSec
	req.ProbeCount = probeCount

	if errCode, errMsg := validateClientCertRequest(req.ClientCertPEM, req.ClientKeyPEM); errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}
	if errCode, errMsg := validateExtraPorts(req.ExtraPorts); errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
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
	ctx, cancel := context.WithTimeout(context.Background(), netDiscoveryTargetTimeout(req.ProbeTimeoutSec, req.ProbeCount, req.AdvancedServiceScan))
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
	} else {
		// Bug real corrigido, relatado ao vivo: "o certificado ainda não é reconhecido" contra um
		// IP PRIVADO digitado diretamente (10.107.51.135, sem hostname nenhum) — mesmo depois da
		// correção do SNI pro caso "busca por hostname", esse caso continuava travado no IP cru
		// como SNI, porque não existe hostname NENHUM digitado pelo usuário pra usar aqui.
		// Confirmado ao vivo: `tls_subject`/`tls_issuer` vinham como
		// "Kubernetes Ingress Controller Fake Certificate" (mesmo placeholder de SNI desconhecido
		// já documentado nesta app) contra um Ingress interno real. Corrigido tentando um DNS
		// REVERSO antes do fingerprint (não confundir com o enrichHops da Fase 3, que roda DEPOIS
		// — tarde demais, o SNI já foi usado) — é comum existir PTR mesmo pra IP privado quando a
		// empresa tem zona DNS interna própria (split-horizon), alcançável pela mesma VPN que já
		// resolve outros recursos internos. Só um palpite best-effort (não é a mesma garantia do
		// texto que o usuário efetivamente digitou) — por isso NUNCA usado pra preencher
		// `originalHostname` (que segue vazio nesse ramo); só ajuda o SNI/Host do fingerprint, o
		// ReverseDNS final do salto-alvo continua vindo do PTR normal em enrichHops, sem
		// tratamento especial.
		ptrCtx, ptrCancel := context.WithTimeout(ctx, 3*time.Second)
		if names, ptrErr := net.DefaultResolver.LookupAddr(ptrCtx, targetIP); ptrErr == nil && len(names) > 0 {
			sniHost = strings.TrimSuffix(names[0], ".")
		}
		ptrCancel()
	}

	// hops := []NetDiscoveryHop{} — não "var hops []NetDiscoveryHop" (bug real achado em code
	// review): tcptraceroute pode sair com exit 0 sem nenhuma linha reconhecida por
	// parseTracerouteLine (o guard de falha abaixo só dispara com err != nil && len(hops) == 0
	// simultaneamente) — um slice nil vira "hops":null no JSON (NetDiscoveryResult.Hops não tem
	// omitempty), e o frontend faz result.hops.map(...) sem guard em 3 lugares (copyResult, a
	// tabela, o export PDF), quebrando em runtime em vez de mostrar uma lista vazia.
	hops := []NetDiscoveryHop{}
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

	// advancedScanProbe (nmap, opt-in) — mesmo princípio do fingerprintProbe: roda no MESMO pod/
	// container já em pé, nunca cria um segundo. Só é de fato chamado mais abaixo quando
	// req.AdvancedServiceScan==true E o fingerprint rápido achou ao menos 1 porta aberta — ver
	// net_discovery_nmap.go pro racional completo dessa decisão.
	var advancedScanProbe func(context.Context, string, []int) (string, error)

	// podClientset — capturado só no modo pod (Fase 4: cross-reference K8s). Permanece nil no
	// modo local; crossReferenceHops trata nil como "só consultar o cache persistido, nunca
	// disparar busca ao vivo" (ver comentário de crossReferenceIP em net_discovery_crossref.go) —
	// por isso a mesma chamada, mais abaixo, cobre os dois modos sem precisar de um `if` extra.
	var podClientset kubernetes.Interface

	// mTLS opcional (ver validateClientCertPair no Run() — já validado como par válido antes de
	// chegar aqui). mtlsFlag nunca é sensível (só "1"/"0", vai como argv); o payload em si (cert+
	// chave) vai só por stdin, construído uma vez e consumido uma única vez mais abaixo (o
	// fingerprint roda uma única chamada por descoberta).
	mtlsFlag := "0"
	if req.ClientCertPEM != "" {
		mtlsFlag = "1"
	}
	mtlsStdin := buildMTLSStdinPayload(req.ClientCertPEM, req.ClientKeyPEM)
	// extraPortsArg — Fase D do roadmap de maturidade profissional (ver validateExtraPorts em
	// Run(), já validado antes de chegar aqui).
	extraPortsArg := formatExtraPortsArg(req.ExtraPorts)

	if req.Mode == netDiscoveryModeLocal {
		fingerprintProbe = func(ctx context.Context, ip string) (string, error) {
			return runFingerprintLocal(ctx, ip, sniHost, mtlsFlag, extraPortsArg, mtlsStdin)
		}
		advancedScanProbe = func(ctx context.Context, ip string, ports []int) (string, error) {
			return runAdvancedScanLocal(ctx, ip, ports)
		}
		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s (modo local, porta %d, timeout %ds/salto, %d sondas/salto)...", targetIP, req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount), 0.2, nil)
		if err := runTracerouteLocal(ctx, targetIP, req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount, onLine); err != nil && len(hops) == 0 {
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
			return runFingerprintInPod(ctx, clientset, restConfig, req.Namespace, podName, ip, sniHost, mtlsFlag, extraPortsArg, mtlsStdin)
		}
		advancedScanProbe = func(ctx context.Context, ip string, ports []int) (string, error) {
			return runAdvancedScanInPod(ctx, clientset, restConfig, req.Namespace, podName, ip, ports)
		}

		send("probe_run", "in_progress", fmt.Sprintf("Traçando rota até %s (porta %d, timeout %ds/salto, %d sondas/salto)...", targetIP, req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount), 0.2, nil)
		if err := runTracerouteInPod(ctx, clientset, restConfig, req.Namespace, podName, targetIP, req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount, onLine); err != nil && len(hops) == 0 {
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
		if sniHost != targetIP {
			// Transparência sobre um achado real: um IP pode ter dezenas de PTR diferentes
			// (ingress compartilhado) — expõe qual hostname foi de fato usado no SNI/Host, pra
			// nunca deixar parecer que o certificado/HTTP encontrado "é do IP" sem mais contexto.
			fp.ProbedHost = sniHost
		}
		fingerprint = &fp
	}

	// Detecção avançada de serviço (nmap), OPT-IN explícito — roda DEPOIS do fingerprint rápido
	// de sempre e só nas portas que ELE já confirmou abertas (nunca varre de novo as fechadas, que
	// só pagariam o piso fixo de ~7s do nmap sem adicionar informação nova). Best-effort: uma
	// falha aqui não derruba o resultado principal, mesmo espírito das outras camadas opcionais.
	// Ver net_discovery_nmap.go pro racional completo da decisão (avaliado e testado ao vivo antes
	// de implementar).
	if req.AdvancedServiceScan && fingerprint != nil && len(fingerprint.OpenPorts) > 0 {
		send("advanced_scan", "in_progress", fmt.Sprintf("Detecção avançada de serviço (nmap) em %d porta(s)...", len(fingerprint.OpenPorts)), 0.94, nil)
		if scanOutput, scanErr := advancedScanProbe(ctx, targetIP, fingerprint.OpenPorts); scanErr != nil {
			log.Warn().Str("target", targetIP).Err(scanErr).Msg("NetDiscovery: detecção avançada de serviço (nmap) falhou (não bloqueia o resultado principal)")
		} else {
			fingerprint.ServiceVersions = parseNmapGreppableOutput(scanOutput)
		}
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

	// Fallback de SO via cross-reference K8s (Fase 4) — mesmo achado real de duas rodadas
	// anteriores: (1) contra um alvo K8s típico (ICMP bloqueado por NSG, nenhuma das ~18 portas
	// curadas é específica de K8s), inferOSGuess sempre ficava sem sinal, mesmo o alvo sendo
	// genuinamente K8s/Linux; (2) um match de CACHE (até 24h de idade) pode estar desatualizado,
	// então só um match AO VIVO desta própria execução conta como sinal — ver o comentário
	// completo (com os dois achados) em inferOSGuess (net_discovery_fingerprint.go), que agora é o
	// ÚNICO lugar que decide/explica o veredito (achado de code review: a versão anterior
	// reimplementava essa decisão aqui, fora do mecanismo documentado). Aqui só resolve QUAL
	// InternalRef (se algum) usar e reinvoca inferOSGuess com ele — nunca sobrescreve um veredito
	// já derivado de porta/TTL (inferOSGuess já prioriza esses sinais internamente).
	if fingerprint != nil && fingerprint.OSGuess == "" {
		for i := range hops {
			if hops[i].IsTarget && hops[i].InternalRef != nil {
				fingerprint.OSGuess, fingerprint.OSConfidence = inferOSGuess(fingerprint.TTL, fingerprint.OpenPorts, hops[i].InternalRef)
				break
			}
		}
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
	h.saveDiscoveryHistory(req, userInfo, result)
}

// saveDiscoveryHistory persiste a execução concluída no Histórico de Descobertas (Fase 5) — store
// DIFERENTE de logHistory/HistoryTracker (auditoria genérica "quem fez o quê" da app inteira);
// este é específico pra "o que já se sabe sobre este alvo", consultável de volta via History().
// Best-effort: falha ou historyStore==nil só loga em Warn, nunca afeta o resultado já enviado ao
// cliente (a descoberta em si já terminou com sucesso nesse ponto).
func (h *NetDiscoveryHandler) saveDiscoveryHistory(req RunNetDiscoveryRequest, userInfo history.UserInfo, result NetDiscoveryResult) {
	if h.historyStore == nil {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		log.Warn().Err(err).Msg("NetDiscovery: falha ao serializar resultado pro histórico (não bloqueia)")
		return
	}
	err = h.historyStore.Save(storage.NetDiscoveryHistoryRecord{
		TargetInput: req.Target,
		TargetIP:    result.TargetIP,
		Mode:        req.Mode,
		Reached:     result.Reached,
		HopsCount:   len(result.Hops),
		ResultJSON:  string(resultJSON),
		CreatedAt:   time.Now(),
		CreatedBy:   userInfo.Email,
	})
	if err != nil {
		log.Warn().Err(err).Msg("NetDiscovery: falha ao salvar histórico (não bloqueia)")
	}
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
