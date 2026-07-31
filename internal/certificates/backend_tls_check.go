package certificates

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// backendTLSLogWindow é a janela de tempo lida de cada pod do ingress-controller — janela de
// tempo (SinceSeconds), não TailLines fixo: mais robusto a diferenças de volume de tráfego entre
// clusters (um controller com tráfego alto pode rotacionar um tailLines fixo em segundos).
const backendTLSLogWindow = 20 * time.Minute

// backendTLSErrorPattern casa linhas de log do ingress-nginx que indicam falha de handshake TLS
// entre o controller e o pod backend (re-encryption via backend-protocol HTTPS/GRPCS ou
// ssl-passthrough). Heurístico: cobre os padrões de erro mais comuns documentados do nginx/OpenSSL,
// não uma lista exaustiva.
var backendTLSErrorPattern = regexp.MustCompile(`(?i)(ssl_do_handshake\(\) failed|x509: certificate signed by unknown authority|ssl handshake.*upstream|upstream ssl certificate)`)

// PodRef identifica um pod (namespace+nome) — usado tanto para os pods do ingress-controller
// achados por FindIngressControllerPods quanto internamente por BackendTLSCheckResult.
type PodRef struct {
	Namespace string
	Name      string
}

// BackendTLSCheckResult é o resultado do "Diagnóstico Avançado" (Fase 8) — heurístico, best-effort.
// IMPORTANTE: Checked=true + Signals vazio NUNCA significa "handshake confirmado sem erro" — só
// que nenhum sinal foi encontrado na janela de log analisada, que pode não cobrir o momento em que
// o problema ocorreu (rotação de log, réplica diferente, etc.). É uma ferramenta assistiva de
// "onde olhar", não uma fonte de verdade — mesmo espírito de TrustedByPublicCA=false em
// ChainValidationResult, tratado como neutro em vez de erro.
type BackendTLSCheckResult struct {
	Checked        bool     `json:"checked"`
	ControllerPods []string `json:"controller_pods,omitempty"`
	Signals        []string `json:"signals,omitempty"` // linhas de log que bateram no padrão, truncadas
	Notes          []string `json:"notes,omitempty"`
}

// podLogsFetcher abstrai a leitura de log de 1 pod — permite testar analyzeBackendTLSLogs
// isoladamente (função pura) e CheckIngressBackendTLS (orquestração) sem tocar rede/API real,
// mesmo espírito de dialHostForCertFn em tls_dial_enrich.go.
type podLogsFetcher func(ctx context.Context, pod PodRef) (string, error)

// FindIngressControllerPods busca pods do ingress-controller em QUALQUER namespace (não assume
// namespace fixo "ingress-nginx" — instalações via Helm podem usar outro nome), tentando labels
// em ordem de especificidade — mesmo padrão de lista ordenada de label-candidates já validado em
// findHostNetworkPod (internal/web/handlers/nodepools_conntrack.go), adaptado pra este caso (não
// filtra por node, já que o controller pode ter várias réplicas em nós diferentes — queremos
// todas, não a primeira).
func (s *Scanner) FindIngressControllerPods(ctx context.Context, cluster string) ([]PodRef, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	labelCandidates := []string{
		"app.kubernetes.io/name=ingress-nginx,app.kubernetes.io/component=controller",
		"app.kubernetes.io/name=ingress-nginx",
		"app.kubernetes.io/component=controller",
		"app=nginx-ingress-controller",
		"k8s-app=nginx-ingress-controller",
	}

	for _, label := range labelCandidates {
		pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			LabelSelector: label,
			FieldSelector: "status.phase=Running",
		})
		if err != nil || len(pods.Items) == 0 {
			continue
		}
		refs := make([]PodRef, 0, len(pods.Items))
		for _, p := range pods.Items {
			refs = append(refs, PodRef{Namespace: p.Namespace, Name: p.Name})
		}
		return refs, nil
	}

	return nil, fmt.Errorf("nenhum pod do ingress-controller encontrado neste cluster")
}

// ReadPodLogTail lê os últimos `since` de log de um pod (SinceSeconds, não TailLines — ver
// backendTLSLogWindow). Timeout curto: nunca deve travar o Diagnóstico Avançado.
func (s *Scanner) ReadPodLogTail(ctx context.Context, cluster string, pod PodRef, since time.Duration) (string, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return "", fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	sinceSeconds := int64(since.Seconds())
	opts := &corev1.PodLogOptions{SinceSeconds: &sinceSeconds}

	out, err := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts).DoRaw(readCtx)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CheckIngressBackendTLS é o "Diagnóstico Avançado" (Fase 8): acha TODOS os pods do
// ingress-controller (a réplica que atendeu a requisição problemática pode ser qualquer uma), lê
// a janela de log de cada uma, e delega a análise pra analyzeBackendTLSLogs (função pura,
// testável). Melhor-esforço: nenhuma falha aqui vira erro pro chamador, só Checked=false + Notes.
func (s *Scanner) CheckIngressBackendTLS(ctx context.Context, cluster string, hosts []string) *BackendTLSCheckResult {
	pods, err := s.FindIngressControllerPods(ctx, cluster)
	if err != nil || len(pods) == 0 {
		return &BackendTLSCheckResult{
			Checked: false,
			Notes:   []string{"pod do ingress-controller não encontrado neste cluster"},
		}
	}

	fetcher := func(ctx context.Context, pod PodRef) (string, error) {
		return s.ReadPodLogTail(ctx, cluster, pod, backendTLSLogWindow)
	}

	return checkBackendTLSWithFetcher(ctx, pods, hosts, fetcher)
}

// checkBackendTLSWithFetcher separa a orquestração (chamar fetcher pra cada pod) da análise pura
// (analyzeBackendTLSLogs) — permite testar a orquestração com um fetcher fake, sem rede real.
func checkBackendTLSWithFetcher(ctx context.Context, pods []PodRef, hosts []string, fetcher podLogsFetcher) *BackendTLSCheckResult {
	logsByPod := make(map[string]string, len(pods))
	var readErrors []string

	for _, pod := range pods {
		podKey := pod.Namespace + "/" + pod.Name
		logs, err := fetcher(ctx, pod)
		if err != nil {
			readErrors = append(readErrors, fmt.Sprintf("%s: %s", podKey, err.Error()))
			continue
		}
		logsByPod[podKey] = logs
	}

	result := analyzeBackendTLSLogs(logsByPod, hosts)
	result.Notes = append(result.Notes, readErrors...)
	return result
}

// analyzeBackendTLSLogs é a lógica pura de análise — regex + agregação, testável com strings
// fixas em memória, sem client-go/rede real (mesmo espírito de buildTLSDialResult em
// tls_dial_enrich.go). Uma linha só conta como "signal" se bater o padrão de erro DE VERDADE E
// mencionar um dos hosts — sem o filtro de host, qualquer erro TLS de QUALQUER outro Ingress no
// mesmo controller apareceria como falso-positivo pro certificado sendo inspecionado.
func analyzeBackendTLSLogs(logsByPod map[string]string, hosts []string) *BackendTLSCheckResult {
	result := &BackendTLSCheckResult{Checked: true}

	for podKey := range logsByPod {
		result.ControllerPods = append(result.ControllerPods, podKey)
	}

	for podKey, logText := range logsByPod {
		for _, line := range strings.Split(logText, "\n") {
			if !backendTLSErrorPattern.MatchString(line) {
				continue
			}
			if !lineMentionsAnyHost(line, hosts) {
				continue
			}
			signal := fmt.Sprintf("[%s] %s", podKey, truncateLine(line, 300))
			result.Signals = append(result.Signals, signal)
		}
	}

	if len(result.Signals) == 0 {
		result.Notes = append(result.Notes, "nenhum sinal de falha de handshake TLS com o backend encontrado na janela de log analisada — isso NÃO confirma que o handshake está funcionando, só que não foi possível observar um erro nessa janela")
	}

	return result
}

// lineMentionsAnyHost reporta se line contém algum dos hosts (case-insensitive) — o error log do
// nginx tipicamente inclui "host: ..."/"server: ..." no contexto da linha de erro de SSL upstream.
func lineMentionsAnyHost(line string, hosts []string) bool {
	lower := strings.ToLower(line)
	for _, h := range hosts {
		if strings.Contains(lower, strings.ToLower(h)) {
			return true
		}
	}
	return false
}

func truncateLine(line string, max int) string {
	line = strings.TrimSpace(line)
	if len(line) <= max {
		return line
	}
	return line[:max] + "…"
}
