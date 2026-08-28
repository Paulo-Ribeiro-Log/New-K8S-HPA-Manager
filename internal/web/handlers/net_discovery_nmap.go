package handlers

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ─── "Descoberta de Rede" — Detecção avançada de serviço (nmap), OPT-IN explícito.
//
// Avaliação registrada (pedido do usuário: "avalie" incorporar nmap) — testado ao vivo contra
// scanme.nmap.org (host oficial do próprio nmap, autorizado pra isso) ANTES de escrever qualquer
// linha de produção, mesmo rigor já aplicado nas fases anteriores desta ferramenta:
//
//   - `nmap -sT -sV` (version detection, SEM SYN scan) dá serviço E VERSÃO reais (ex: "OpenSSH
//     6.6.1p1 Ubuntu", "Apache httpd 2.4.7 (Ubuntu)") — bem mais rico que "porta aberta/fechada"
//     do nc atual (netDiscoveryFingerprintScript, net_discovery_fingerprint.go).
//   - `-sT` (TCP connect scan explícito, nunca SYN scan) testado COM e SEM a capability NET_RAW
//     (`docker run --cap-drop=NET_RAW`) — funciona idêntico nos dois casos. Não introduz NENHUM
//     requisito de privilégio novo além do que nc/curl já precisam hoje.
//   - `nmap -O` (fingerprint de SO) foi EXCLUÍDO desta decisão: exige NET_RAW de verdade (falha
//     duro sem ela, "Couldn't open a raw socket") e, mesmo COM o privilégio, teve confiabilidade
//     baixa nos testes ("could not find at least 1 open and 1 closed port", múltiplos palpites
//     conflitantes) — a heurística de porta já existente (inferOSGuess) continua sendo o sinal
//     primário de SO, sem mudança nenhuma.
//   - Achado DECISIVO: toda invocação do nmap tem um PISO FIXO de ~7s, independente de quantas
//     portas (testado com 18, 2 e 1) ou intensidade (`-T4`, `--version-intensity 0`) — nenhuma
//     flag reduz esse piso (é overhead de calibração/inicialização do próprio nmap contra o RTT
//     do alvo, não proporcional ao trabalho). Comparado ao fingerprint rápido de sempre (~2s, tudo
//     em paralelo), isso é 3-4x mais lento — inaceitável como parte do fluxo PADRÃO desta
//     ferramenta, cuja filosofia central (Fase 1) é resultado ao vivo e rápido.
//
// Decisão: nunca no caminho padrão — só quando o usuário liga explicitamente
// (RunNetDiscoveryRequest.AdvancedServiceScan), rodando DEPOIS do fingerprint rápido de sempre
// (net_discovery.go/runDiscovery) e só nas portas que ELE já confirmou abertas
// (fingerprint.OpenPorts) — nunca varre de novo as portas fechadas, que só pagariam o piso de
// ~7s sem adicionar informação nova (já sabemos que estão fechadas).

// netDiscoveryAdvancedScanTimeoutSec — teto rígido (wrapper `timeout` no próprio comando, não só
// o timeout do contexto Go) pro subprocesso nmap, generoso sobre o piso observado de ~7-8s pra
// dar margem em redes mais lentas (VPN corporativa, alvo remoto) sem deixar rodar indefinidamente.
const netDiscoveryAdvancedScanTimeoutSec = 25

// netDiscoveryAdvancedScanBudget — extensão do teto geral da descoberta quando o usuário liga
// AdvancedServiceScan. Aplicada pelo chamador (net_discovery.go/net_discovery_batch.go) SEMPRE
// respeitando netDiscoveryOverallTimeoutCap por cima — nunca ultrapassa esse cap, que por sua vez
// já fica abaixo de netDiscoveryPodActiveDeadline (mesma garantia de segurança já usada pras
// outras dimensões, ver computeOverallTimeout).
const netDiscoveryAdvancedScanBudget = 30 * time.Second

// NetDiscoveryServiceVersion é um serviço/versão detectado pelo nmap numa porta específica —
// sempre uma porta que o fingerprint rápido (nc) já tinha confirmado aberta.
type NetDiscoveryServiceVersion struct {
	Port    int    `json:"port"`
	Service string `json:"service,omitempty"`
	Version string `json:"version,omitempty"`
}

// nmapAdvancedScanArgs monta o comando nmap — `-sT` (TCP connect, nunca SYN scan — funciona sem
// NET_RAW, ver comentário no topo do arquivo), `-sV` (version detection), `-Pn` (não faz "host
// discovery" via ping — já sabemos que o alvo responde, traceroute/fingerprint já confirmaram),
// `-n` (sem resolução DNS — já temos isso via enrichHops), `-oG -` (saída greppable pro parser
// abaixo). `timeout N` como wrapper externo — mesmo padrão de segurança já usado por outras
// ferramentas desta app, nunca confia só no timeout interno do próprio comando (nmap tem os seus
// próprios `--host-timeout`/`--max-rtt-timeout`, mas um wrapper externo garante o corte mesmo se
// esses mecanismos internos falharem por algum motivo).
func nmapAdvancedScanArgs(targetIP string, ports []int) []string {
	portStrs := make([]string, len(ports))
	for i, p := range ports {
		portStrs[i] = strconv.Itoa(p)
	}
	return []string{
		"timeout", strconv.Itoa(netDiscoveryAdvancedScanTimeoutSec),
		"nmap", "-sT", "-sV", "-Pn", "-n", "--version-intensity", "5",
		"-p", strings.Join(portStrs, ","), "-oG", "-", targetIP,
	}
}

// runAdvancedScanInPod/runAdvancedScanLocal — mesmo padrão dual pod/local já usado pelo restante
// desta ferramenta. No modo pod, roda no MESMO pod/container já em pé (não cria um segundo).
func runAdvancedScanInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, targetIP string, ports []int) (string, error) {
	return execCmdInPod(ctx, clientset, restConfig, namespace, podName, netDiscoveryPodContainer, nmapAdvancedScanArgs(targetIP, ports))
}

func runAdvancedScanLocal(ctx context.Context, targetIP string, ports []int) (string, error) {
	// Nome explícito — mesmo achado real documentado em runTracerouteLocal/runFingerprintLocal
	// (net_discovery.go/net_discovery_fingerprint.go): cancelamento no meio mataria o CLIENTE
	// docker run sem garantir que o container remoto pare junto.
	containerName := fmt.Sprintf("net-discovery-nmap-%s", uuid.New().String()[:8])
	args := append([]string{
		"run", "--rm", "--network=host",
		"--name", containerName,
		"--label", netDiscoveryDockerLabel,
		netDiscoveryPodImage,
	}, nmapAdvancedScanArgs(targetIP, ports)...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()

	if ctx.Err() != nil {
		cleanupCancelledDockerContainer(containerName)
	}
	return string(out), err
}

// parseNmapGreppableOutput extrai porta/serviço/versão da linha "Host: ... Ports: ..." da saída
// -oG. Formato real confirmado ao vivo contra scanme.nmap.org: cada porta é
// "PORT/STATE/PROTO/OWNER/SERVICE/RPC/VERSION/" (campos separados por "/", entradas separadas por
// ", "). Só devolve portas com state "open" — defensivo (já só varremos portas que o fingerprint
// rápido confirmou abertas, mas um estado pode mudar entre as duas checagens) — e só quando há
// algum texto de serviço reconhecido (senão não agrega nada sobre o que o nc já sabia).
//
// `fields[6:len(fields)-1]` (não só `fields[6]`) reconstrói o campo de versão preservando
// eventuais "/" embutidos na própria string de versão — o formato sempre termina com um "/" extra
// (campo vazio final), então o índice de versão nunca é o último, mas pode se espalhar por mais
// de um campo se a versão contiver "/".
func parseNmapGreppableOutput(output string) []NetDiscoveryServiceVersion {
	var results []NetDiscoveryServiceVersion

	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "\tPorts: ")
		if idx == -1 {
			continue
		}
		portsData := line[idx+len("\tPorts: "):]

		for _, entry := range strings.Split(portsData, ", ") {
			fields := strings.Split(strings.TrimSpace(entry), "/")
			if len(fields) < 5 {
				continue
			}
			port, err := strconv.Atoi(fields[0])
			if err != nil {
				continue
			}
			if fields[1] != "open" {
				continue
			}
			service := strings.TrimSpace(fields[4])

			var version string
			switch {
			case len(fields) > 7:
				version = strings.TrimSpace(strings.Join(fields[6:len(fields)-1], "/"))
			case len(fields) == 7:
				version = strings.TrimSpace(fields[6])
			}

			if service == "" && version == "" {
				continue
			}
			results = append(results, NetDiscoveryServiceVersion{Port: port, Service: service, Version: version})
		}
	}

	return results
}
