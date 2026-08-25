package handlers

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ─── "Descoberta de Rede" — Fase 2 (IP-ROUTE-DISCOVERY-PLAN.md, seções 3.2/3.3/3.4): fingerprint
// do DESTINO (não por hop, só o alvo final) — heurística de TTL, banner grab de porta, HTTP/TLS.
// Roda como um único script combinado (ping+portas em paralelo+curl+openssl), UMA execução extra
// depois do traceroute (mesmo pod/container já em pé no modo pod — não cria um segundo), pra não
// multiplicar exec/docker-run. Escopo deliberadamente limitado (seção 3.3 do plano): não é um
// scan de porta completo, é um conjunto curado de portas conhecidas com timeout curto.

// netDiscoveryFingerprintPorts — mesma lista curada da seção 3.3 do plano.
var netDiscoveryFingerprintPorts = []int{22, 80, 443, 445, 3389, 21, 25, 587, 53, 3306, 5432, 6379, 27017, 9200, 8080, 8443, 5985, 5986}

// netDiscoveryMTLSSplitMarker — sentinela usado pra separar, dentro de UM ÚNICO stream de stdin,
// os dois blocos PEM (certificado de cliente + chave privada) que netDiscoveryFingerprintScript
// grava em dois arquivos temporários via `awk`. Precisa bater EXATAMENTE com a linha usada no
// script abaixo — os dois são mantidos em sincronia manualmente (raw string Go não permite
// interpolar uma const dentro de um literal de backtick). Ver comentário completo em
// buildMTLSStdinPayload (net_discovery.go) sobre por que mTLS viaja só por stdin, nunca por argv.
const netDiscoveryMTLSSplitMarker = "___NETDISC_MTLS_SPLIT___"

// netDiscoveryFingerprintScript roda dentro do pod/container (nunca do backend — mesmo vantage
// point da rede que o traceroute já usou, importante especialmente no modo pod, onde o alvo pode
// só ser alcançável de dentro daquele cluster específico). Marcadores "@@TAG" no início de cada
// linha de saída tornam o parsing em Go trivial e robusto, sem depender de posição/ordem (os
// checks de porta rodam em paralelo via `&`+`wait`, a ordem de chegada não é determinística).
//
// "$1" é o IP-alvo (onde conectar de fato) e "$2" é o HOST usado como SNI/Host header (pode ser
// igual ao IP quando não há hostname real conhecido) — ambos passados como argumento posicional
// (não interpolados na string do script), mesma prática de segurança de sempre, mesmo já vindo
// validados (net.ParseIP ou resolução DNS bem-sucedida).
//
// Bug real corrigido, achado ao vivo pelo usuário testando contra um host atrás de um cofre
// Delinea (bastion/PAM): "os certificados não são revelados e sempre retornam fake". Causa: a v1
// deste script sempre usava o IP CRU como SNI (`-servername "$IP"`) e como Host virtual
// (`http://$IP/`) — mas SNI é, por definição, sempre um NOME, nunca um endereço IP; qualquer
// terminador TLS que roteia por SNI (ingress-nginx, ALB, CDN, o próprio proxy do cofre) não
// reconhece um IP como SNI válido e cai no comportamento padrão pra SNI desconhecido — que pode
// ser servir um certificado de placeholder (o "Kubernetes Ingress Controller Fake Certificate" já
// documentado noutro lugar desta app, internal/certificates/parser.go) OU, em alguns terminadores,
// simplesmente REJEITAR o handshake por completo. Confirmado ao vivo ANTES de escrever este
// comentário (contra um host real por trás de Cloudflare, SNI-roteado): `-servername "$IP"` →
// "Could not read certificate from <stdin>" (falha total, explica "certificados não são
// revelados"); `-servername "example.com"` (o nome real) → certificado correto extraído sem
// problema nenhum.
//
// Corrigido: quando o usuário buscou por HOSTNAME (não por IP — ver sniHost em net_discovery.go),
// $2 carrega esse hostname original, usado como SNI (openssl `-servername`) e via `curl --resolve
// "$HOST:porta:$IP"` (força a conexão TCP pro IP já resolvido, mas com Host/SNI corretos na
// camada HTTP/TLS — sem depender de uma nova resolução DNS dentro do container, que poderia até
// divergir do IP que o traceroute de fato alcançou). Quando a busca já era por IP direto, $2 == $1
// e o comportamento é idêntico ao de antes (nada muda nesse caso — não há hostname real pra usar).
//
// mTLS (certificado de cliente) — pedido explícito do usuário depois de perguntar se ter o
// certificado "que já existe nesses clusters/servidores" ajudaria a descoberta: útil quando o
// servidor exige `ClientAuth: RequireAnyClientCert` (mesmo sintoma "certificate required" já
// documentado no Monitor de Certificados Externos). "$3" é "1"/"0" (MTLS habilitado ou não —
// nunca sensível, único dado que vai como argv). O PAR cert+chave em si NUNCA vai como argumento
// de linha de comando (ficaria visível via `ps`/`/proc/<pid>/cmdline` dentro do próprio
// container) — viaja só pelo STREAM de stdin do exec (ver execCmdInPodWithStdin/
// buildMTLSStdinPayload), um bloco PEM depois do outro separados pela linha
// netDiscoveryMTLSSplitMarker; `awk` grava os dois em arquivos temporários (`mktemp`, apagados no
// fim do script, `trap` não usado de propósito — o `rm -f` final já cobre o caminho feliz, e o
// pod/container inteiro é destruído ao fim da descoberta de qualquer forma, mesmo se o script for
// interrompido no meio). Quando MTLS=0 (caso comum, sem mudança de comportamento), o script nunca
// sequer tenta ler stdin.
//
// Achado real, verificado ao vivo ANTES de assumir o efeito óbvio ("sem cert, nada é lido"): o
// certificado de cliente NÃO é pré-requisito pra `openssl s_client` extrair subject/issuer na
// maioria dos casos — o servidor manda o PRÓPRIO certificado cedo na troca (ServerHello/
// Certificate), ANTES de sequer checar se o cliente apresentou o dele; um `s_client` sem `-cert`
// contra um servidor real com `tls.RequireAndVerifyClientCert` (Go stdlib) ainda recebe e exibe o
// certificado do servidor normalmente, só a REQUISIÇÃO HTTP em si (`curl`) é que falha por
// completo sem o cert (handshake nunca fecha o suficiente pra trocar dados de aplicação). Ou seja:
// o ganho real e confirmado do mTLS aqui é destravar `@@HTTP`/`@@HTTPS` (Server: header, status),
// não necessariamente `@@TLS` — que já costumava funcionar mesmo sem cert de cliente, na maioria
// dos terminadores. Validado ao vivo, ponta a ponta via API real (não só o script isolado): contra
// um servidor Go de teste com `tls.RequireAndVerifyClientCert` genuíno, `client_cert_used:true` +
// `http_server` só apareceram COM o certificado; SEM ele, `tls_subject`/`tls_issuer` continuaram
// batendo, mas `http_server` ficou ausente — confirma a distinção na prática, não só em teoria.
// Pode haver terminadores mais estritos (WAF, proxy que derruba a conexão ANTES do ServerHello sem
// certificado de cliente numa camada mais baixa) onde nem o certificado do servidor aparece sem
// mTLS — não descarta o valor da feature, só evita prometer mais do que o mecanismo garante.
const netDiscoveryFingerprintScript = `
IP="$1"
HOST="$2"
MTLS="$3"
echo "@@TTL $(ping -c 1 -W 2 "$IP" 2>&1 | grep -o 'ttl=[0-9]*' | cut -d= -f2)"
for p in 22 80 443 445 3389 21 25 587 53 3306 5432 6379 27017 9200 8080 8443 5985 5986; do
  ( if nc -z -w1 "$IP" "$p" 2>/dev/null; then echo "@@PORT $p OPEN"; else echo "@@PORT $p CLOSED"; fi ) &
done
wait
CERTFILE=""
KEYFILE=""
CURLCERT=""
OPENSSLCERT=""
if [ "$MTLS" = "1" ]; then
  CERTFILE=$(mktemp)
  KEYFILE=$(mktemp)
  awk -v certf="$CERTFILE" -v keyf="$KEYFILE" 'BEGIN{target=certf} /^___NETDISC_MTLS_SPLIT___$/{target=keyf; next} {print > target}'
  CURLCERT="--cert $CERTFILE --key $KEYFILE"
  OPENSSLCERT="-cert $CERTFILE -key $KEYFILE"
fi
HTTP=$(curl -sI --max-time 3 $CURLCERT --resolve "$HOST:80:$IP" "http://$HOST/" 2>/dev/null | grep -i '^server:' | head -1 | tr -d '\r')
echo "@@HTTP $HTTP"
HTTPS=$(curl -skI --max-time 3 $CURLCERT --resolve "$HOST:443:$IP" "https://$HOST/" 2>/dev/null | grep -i '^server:' | head -1 | tr -d '\r')
echo "@@HTTPS $HTTPS"
TLSOUT=$(echo | timeout 3 openssl s_client -connect "$IP:443" -servername "$HOST" $OPENSSLCERT 2>/dev/null | openssl x509 -noout -subject -issuer 2>/dev/null | tr '\n' '|')
echo "@@TLS $TLSOUT"
echo "@@MTLS_USED $MTLS"
if [ -n "$CERTFILE" ]; then rm -f "$CERTFILE" "$KEYFILE"; fi
`

// NetDiscoveryFingerprint é o resultado da Fase 2 — sempre heurística, nunca certeza (ver seção
// 3.2 do plano: "deve ser rotulada como tal na UI, mesmo princípio de TrustedByPublicCA").
type NetDiscoveryFingerprint struct {
	TTL          int    `json:"ttl,omitempty"`
	OSGuess      string `json:"os_guess,omitempty"`      // "linux" | "windows" | ""
	OSConfidence string `json:"os_confidence,omitempty"` // explica de onde veio o palpite — nunca só o resultado seco
	OpenPorts    []int  `json:"open_ports,omitempty"`
	IsWebServer  bool   `json:"is_web_server"`
	HTTPServer   string `json:"http_server,omitempty"` // header Server: quando presente (HTTP ou HTTPS, o que responder)
	TLSSubject   string `json:"tls_subject,omitempty"`
	TLSIssuer    string `json:"tls_issuer,omitempty"`
	// ProbedHost — hostname REALMENTE usado como SNI/Host no HTTP(S)/TLS (ver sniHost em
	// runDiscovery). Só preenchido quando difere do IP alvo (ou seja, quando algum hostname real
	// foi usado — digitado pelo usuário OU descoberto via PTR pra um IP buscado direto). Existe
	// pra deixar TRANSPARENTE um achado real: um IP pode ter DEZENAS de registros PTR diferentes
	// (ingress compartilhado, um por serviço) — a ordem que o resolver DNS devolve é
	// essencialmente arbitrária, então o certificado/HTTP aqui pode ser de um serviço DIFERENTE
	// do que o usuário pretendia investigar quando buscou só pelo IP. Sem este campo, o
	// certificado apareceria como se fosse "do IP", escondendo essa ambiguidade real.
	ProbedHost string `json:"probed_host,omitempty"`
	// ClientCertUsed — true quando um certificado de cliente (mTLS) foi apresentado NESTA
	// tentativa (ver RunNetDiscoveryRequest.ClientCertPEM). Não confirma sucesso — só que o
	// mecanismo foi acionado; sucesso/falha real já se reflete em TLSSubject/TLSIssuer presentes
	// ou vazios, mesmo princípio de nunca duplicar um veredito que já existe noutro campo.
	ClientCertUsed bool `json:"client_cert_used,omitempty"`
}

// runFingerprintInPod/runFingerprintLocal — mesmo padrão dual pod/local do traceroute (Fase 1),
// mas AQUI sempre bloqueante (não streaming) — é um único resultado consolidado no final, não
// uma sequência de saltos aparecendo ao vivo; não faz sentido animar porta a porta.
//
// `sniHost` — hostname original digitado pelo usuário (quando a busca foi por hostname) ou igual
// a `targetIP` (quando a busca já era por IP direto, sem hostname real disponível) — ver
// comentário completo em netDiscoveryFingerprintScript sobre por que isso importa pro HTTP/TLS.
//
// `mtlsStdin` — stream de stdin pro script (payload do certificado de cliente via
// buildMTLSStdinPayload, ou um leitor vazio quando mTLS não foi configurado — ver
// netDiscoveryFingerprintScript sobre por que o par cert+chave nunca vai como argv). `mtlsFlag`
// é sempre "1" ou "0", nunca sensível.
func runFingerprintInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, targetIP, sniHost, mtlsFlag string, mtlsStdin io.Reader) (string, error) {
	return execCmdInPodWithStdin(ctx, clientset, restConfig, namespace, podName, netDiscoveryPodContainer,
		[]string{"sh", "-c", netDiscoveryFingerprintScript, "sh", targetIP, sniHost, mtlsFlag}, mtlsStdin)
}

func runFingerprintLocal(ctx context.Context, targetIP, sniHost, mtlsFlag string, mtlsStdin io.Reader) (string, error) {
	// Nome explícito — mesmo achado real documentado em runTracerouteLocal (net_discovery.go):
	// cancelamento no meio do fingerprint mataria o CLIENTE docker run sem garantir que o
	// container remoto pare junto.
	containerName := fmt.Sprintf("net-discovery-fp-%s", uuid.New().String()[:8])
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--network=host",
		"--name", containerName,
		"--label", netDiscoveryDockerLabel, netDiscoveryPodImage,
		"sh", "-c", netDiscoveryFingerprintScript, "sh", targetIP, sniHost, mtlsFlag)
	cmd.Stdin = mtlsStdin
	out, err := cmd.CombinedOutput()

	if ctx.Err() != nil {
		cleanupCancelledDockerContainer(containerName)
	}
	return string(out), err
}

var (
	fingerprintHTTPServerHeaderRegex = regexp.MustCompile(`(?i)^server:\s*(.+)$`)
	fingerprintOpenSSLFieldRegex     = regexp.MustCompile(`^(?:subject|issuer)=\s*(.+)$`)
)

// extractServerHeader extrai só o VALOR do header Server: (ex: "Server: cloudflare" → "cloudflare")
// — a linha de origem já vem pré-filtrada pelo grep no script (só chega aqui se bateu), mas ainda
// carrega o nome do header, que a UI não precisa mostrar de novo.
func extractServerHeader(line string) string {
	m := fingerprintHTTPServerHeaderRegex.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// extractOpenSSLField extrai só o VALOR de uma linha "subject=..."/"issuer=..." do openssl x509
// — mesmo princípio de extractServerHeader.
func extractOpenSSLField(field string) string {
	m := fingerprintOpenSSLFieldRegex.FindStringSubmatch(strings.TrimSpace(field))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// inferOSGuess aplica a heurística da seção 3.2/3.3 do plano — portas conhecidas de Windows/Linux
// são sinal MAIS confiável que TTL sozinho (checadas primeiro); TTL entra só como fallback quando
// nenhuma porta característica foi encontrada aberta. Nunca retorna um veredito sem explicar a
// origem em OSConfidence — mesmo princípio de fraseologia neutra usado no resto da app
// (TrustedByPublicCA, ChainValidationResult): a UI não deve nunca apresentar isto como certeza.
func inferOSGuess(ttl int, openPorts []int) (guess, confidence string) {
	windowsPorts := []int{3389, 445, 5985, 5986}
	for _, p := range windowsPorts {
		if containsInt(openPorts, p) {
			return "windows", "porta " + strconv.Itoa(p) + " aberta (RDP/SMB/WinRM) — indício mais confiável que TTL sozinho"
		}
	}
	if containsInt(openPorts, 22) {
		return "linux", "porta 22 (SSH) aberta — indício mais confiável que TTL sozinho"
	}

	if ttl <= 0 {
		return "", "sem sinal suficiente pra inferir o sistema operacional"
	}
	switch {
	case ttl <= 64:
		return "linux", "heurística de TTL (observado " + strconv.Itoa(ttl) + ", TTL inicial provável 64) — Linux/Unix/macOS mais provável, mas é só heurística"
	case ttl <= 128:
		return "windows", "heurística de TTL (observado " + strconv.Itoa(ttl) + ", TTL inicial provável 128) — Windows mais provável, mas é só heurística"
	default:
		return "", "TTL observado (" + strconv.Itoa(ttl) + ") não bate com nenhum padrão conhecido de SO"
	}
}

// parseFingerprintOutput interpreta a saída do netDiscoveryFingerprintScript (marcadores @@TAG),
// robusta contra ordem de chegada (os checks de porta rodam em paralelo no script) e contra
// tags ausentes (ex: TLS vazio quando a porta 443 nem abriu — não é erro, só não há dado).
func parseFingerprintOutput(output string) NetDiscoveryFingerprint {
	var fp NetDiscoveryFingerprint

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "@@TTL "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "@@TTL "))); err == nil {
				fp.TTL = n
			}
		case strings.HasPrefix(line, "@@PORT "):
			fields := strings.Fields(strings.TrimPrefix(line, "@@PORT "))
			if len(fields) == 2 && fields[1] == "OPEN" {
				if p, err := strconv.Atoi(fields[0]); err == nil {
					fp.OpenPorts = append(fp.OpenPorts, p)
				}
			}
		case strings.HasPrefix(line, "@@HTTP "):
			if v := extractServerHeader(strings.TrimPrefix(line, "@@HTTP ")); v != "" && fp.HTTPServer == "" {
				fp.HTTPServer = v
			}
		case strings.HasPrefix(line, "@@HTTPS "):
			if v := extractServerHeader(strings.TrimPrefix(line, "@@HTTPS ")); v != "" && fp.HTTPServer == "" {
				fp.HTTPServer = v
			}
		case strings.HasPrefix(line, "@@TLS "):
			parts := strings.Split(strings.TrimPrefix(line, "@@TLS "), "|")
			if len(parts) >= 1 {
				fp.TLSSubject = extractOpenSSLField(parts[0])
			}
			if len(parts) >= 2 {
				fp.TLSIssuer = extractOpenSSLField(parts[1])
			}
		case strings.HasPrefix(line, "@@MTLS_USED "):
			fp.ClientCertUsed = strings.TrimSpace(strings.TrimPrefix(line, "@@MTLS_USED ")) == "1"
		}
	}

	sort.Ints(fp.OpenPorts)
	fp.IsWebServer = containsInt(fp.OpenPorts, 80) || containsInt(fp.OpenPorts, 443) || fp.TLSSubject != "" || fp.HTTPServer != ""
	fp.OSGuess, fp.OSConfidence = inferOSGuess(fp.TTL, fp.OpenPorts)
	return fp
}
