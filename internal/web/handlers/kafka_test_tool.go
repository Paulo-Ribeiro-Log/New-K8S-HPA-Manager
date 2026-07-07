package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/web/sse"
)

const (
	// kafkaTestPodImage: kcat (ex-kafkacat) cobre TCP+protocolo+SASL+produce/consume com um único
	// binário leve. Tag FIXADA de propósito (nunca `latest`). É a imagem do EPHEMERAL CONTAINER
	// anexado a um pod já rodando do Deployment alvo (não um pod novo) — ver
	// resolveRunningPodForDeployment/getOrCreateKafkaEphemeralContainer: o teste precisa refletir
	// a identidade de rede real do Deployment (NetworkPolicy/Istio avaliam por label/service
	// account do pod, não por namespace inteiro), então herdar o namespace de rede de um pod real
	// é o que faz o resultado ser confiável.
	kafkaTestPodImage = "edenhill/kcat:1.7.1"
	// kafkaTestEphemeralReadyTimeout/PollInterval: mesmo padrão de waitForEphemeralContainer
	// (podexec.go), usado pela Debug Container já existente no terminal de Pods.
	kafkaTestEphemeralReadyTimeout = 30 * time.Second
	kafkaTestEphemeralPollInterval = 500 * time.Millisecond

	// Guardrails — teto hardcoded, não confiar só na validação do frontend.
	kafkaTestDefaultTimeoutMs = 5000
	kafkaTestMaxTimeoutMs     = 15000
	// kafkaTestConsumeMaxMessages limita quantas mensagens o estágio de consumo lê procurando o
	// marcador de teste — não usa offset exato pré-produce (ver kafka_test_tool.go doc), então lê
	// desde o início do tópico até esse teto como compromisso simplicidade/custo.
	kafkaTestConsumeMaxMessages = 50
)

const (
	kafkaSASLMechanismPlain       = "PLAIN"
	kafkaSASLMechanismScramSHA256 = "SCRAM-SHA-256"
	kafkaSASLMechanismScramSHA512 = "SCRAM-SHA-512"
)

var kafkaValidSASLMechanisms = map[string]bool{
	kafkaSASLMechanismPlain:       true,
	kafkaSASLMechanismScramSHA256: true,
	kafkaSASLMechanismScramSHA512: true,
}

// Classificação do estágio de conectividade — deriva de UM único `kcat -L` (ver runKafkaConnectivityStage).
const (
	kafkaStageOK            = "ok"
	kafkaStageTCPFailed     = "tcp_failed"
	kafkaStageAuthFailed    = "auth_failed"
	kafkaStageTLSFailed     = "tls_failed"
	kafkaStageUnknownFailed = "unknown_failed"
	kafkaStageSkipped       = "skipped"
)

// kafkaNetworkErrorRegex casa mensagens de falha de rede/DNS do rdkafka (biblioteca usada pelo
// kcat) — "Connect to ipv4#x.x.x.x:9092 failed", "Failed to resolve 'host:port'", "Local: Host
// resolution failure", "Local: Broker transport failure".
var kafkaNetworkErrorRegex = regexp.MustCompile(`(?i)(connect to .* failed|failed to resolve|host resolution failure|broker transport failure)`)
var kafkaAuthErrorRegex = regexp.MustCompile(`(?i)sasl`)
var kafkaTLSErrorRegex = regexp.MustCompile(`(?i)ssl`)
var kafkaBrokerCountRegex = regexp.MustCompile(`(\d+)\s+brokers?:`)
var kafkaTopicCountRegex = regexp.MustCompile(`(\d+)\s+topics?:`)

// KafkaSASLConfig descreve autenticação SASL opcional pro teste. Username/Password OU SecretRef
// (mutuamente exclusivos — SecretRef tem prioridade se ambos vierem preenchidos por engano).
type KafkaSASLConfig struct {
	Mechanism     string          `json:"mechanism"` // PLAIN | SCRAM-SHA-256 | SCRAM-SHA-512
	UseTLS        bool            `json:"use_tls"`
	SkipTLSVerify bool            `json:"skip_tls_verify"`
	Username      string          `json:"username,omitempty"`
	Password      string          `json:"password,omitempty"`
	SecretRef     *KafkaSecretRef `json:"secret_ref,omitempty"`
}

// KafkaSecretRef aponta pra um Secret K8s de onde ler username/password — nunca trafega de volta
// pro frontend, só é usado server-side pra montar o comando dentro do pod efêmero.
type KafkaSecretRef struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	UsernameKey string `json:"username_key"`
	PasswordKey string `json:"password_key"`
}

// RunKafkaTestRequest é o body do POST /kafka-test/run.
type RunKafkaTestRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	// Deployment identifica de QUAL workload o teste deve partir — o handler resolve um pod
	// Running desse Deployment e anexa um ephemeral container nele, pra refletir a identidade de
	// rede real (NetworkPolicy/Istio) daquele workload específico, não de um pod avulso genérico.
	Deployment     string           `json:"deployment"`
	Broker         string           `json:"broker"` // "host:porta" — tipicamente um broker EXTERNO ao cluster
	SASL           *KafkaSASLConfig `json:"sasl,omitempty"`
	ProduceConsume bool             `json:"produce_consume"`
	Topic          string           `json:"topic,omitempty"`
	// ConfirmProduce precisa ser true explicitamente pra rodar o estágio de produce/consume —
	// diferente de TCP/protocolo (só leitura), produce ESCREVE uma mensagem real no tópico.
	ConfirmProduce bool `json:"confirm_produce"`
	TimeoutMs      int  `json:"timeout_ms"`
}

// KafkaStageResult é o resultado de um estágio individual do teste.
type KafkaStageResult struct {
	Status      string `json:"status"` // ok | tcp_failed | auth_failed | tls_failed | unknown_failed | skipped
	Message     string `json:"message"`
	RawOutput   string `json:"raw_output"`
	BrokerCount int    `json:"broker_count,omitempty"`
	TopicCount  int    `json:"topic_count,omitempty"`
}

// KafkaProduceConsumeResult é o resultado do round-trip de produce/consume.
type KafkaProduceConsumeResult struct {
	Status      string `json:"status"` // ok | produce_failed | not_found | skipped
	Message     string `json:"message"`
	RoundTripMs int64  `json:"round_trip_ms,omitempty"`
	RawOutput   string `json:"raw_output"`
}

// KafkaTestResult é o resultado completo de uma execução do teste de Kafka.
type KafkaTestResult struct {
	// TargetPod é o pod real (do Deployment escolhido) onde o ephemeral container do teste foi
	// anexado — transparência de qual carga específica foi tocada, já que isso é uma mutação
	// (ainda que pequena) num pod real, não um recurso descartável criado do zero.
	TargetPod      string                    `json:"target_pod"`
	Connectivity   KafkaStageResult          `json:"connectivity"`
	ProduceConsume KafkaProduceConsumeResult `json:"produce_consume"`
}

// createKafkaTestPod cria o pod efêmero kcat no namespace alvo — mesmo padrão de
// createTestPod (latency_test_tool.go), só trocando imagem/label.
// resolveRunningPodForDeployment acha um pod Running que pertence ao Deployment informado, via o
// próprio label selector do Deployment (não heurística de nome/label "app" — o mesmo tipo de
// atalho frágil já usado no frontend pra outra finalidade, DeploymentsTab.tsx). Devolve também o
// nome do primeiro container não-init do pod, usado como TargetContainerName do ephemeral
// container (não afeta o namespace de rede — isso já é compartilhado automaticamente por
// qualquer ephemeral container do pod — só mantém paridade com o padrão já exigido pela Debug
// Container existente em podexec.go).
func resolveRunningPodForDeployment(ctx context.Context, clientset kubernetes.Interface, namespace, deploymentName string) (podName, containerName string, err error) {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("falha ao buscar deployment %s: %w", deploymentName, err)
	}

	sel, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return "", "", fmt.Errorf("selector inválido no deployment %s: %w", deploymentName, err)
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return "", "", fmt.Errorf("falha ao listar pods do deployment %s: %w", deploymentName, err)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if len(pod.Spec.Containers) == 0 {
			continue
		}
		return pod.Name, pod.Spec.Containers[0].Name, nil
	}

	return "", "", fmt.Errorf("nenhum pod Running encontrado pra o deployment %s/%s", namespace, deploymentName)
}

// getOrCreateKafkaEphemeralContainer anexa um ephemeral container kcat no pod real informado —
// réplica não-interativa de getOrCreateEphemeralContainer/createEphemeralContainer (podexec.go),
// usado pela Debug Container já existente no terminal de Pods. Sem Stdin/TTY (só preciso executar
// scripts via execCmdInPod, não uma sessão interativa) e sem callback de progresso (não há
// WebSocket aqui, o progresso já vai via SSE em runTest). Reusa um container existente com a
// mesma imagem+alvo se ainda estiver Running, evitando acumular containers a cada teste repetido.
func getOrCreateKafkaEphemeralContainer(ctx context.Context, clientset kubernetes.Interface, namespace, podName, targetContainer string) (containerName string, err error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("falha ao buscar pod %s: %w", podName, err)
	}

	for _, ec := range pod.Spec.EphemeralContainers {
		if ec.Image != kafkaTestPodImage || ec.TargetContainerName != targetContainer {
			continue
		}
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name == ec.Name && status.State.Running != nil {
				return ec.Name, nil
			}
		}
	}

	containerName = fmt.Sprintf("kafka-test-%d", time.Now().Unix())

	patchData, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"ephemeralContainers": []map[string]interface{}{
				{
					"name":                containerName,
					"image":               kafkaTestPodImage,
					"command":             []string{"sleep", "300"},
					"imagePullPolicy":     "IfNotPresent",
					"targetContainerName": targetContainer,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("falha ao montar patch do ephemeral container: %w", err)
	}

	_, err = clientset.CoreV1().Pods(namespace).Patch(
		ctx, podName, types.StrategicMergePatchType, patchData, metav1.PatchOptions{}, "ephemeralcontainers",
	)
	if err != nil {
		return "", fmt.Errorf("falha ao anexar ephemeral container: %w", err)
	}

	return containerName, nil
}

// waitKafkaEphemeralContainerRunning espera o ephemeral container ficar Running — réplica direta
// de waitForEphemeralContainer (podexec.go), sem dependência do handler de WebSocket.
func waitKafkaEphemeralContainerRunning(ctx context.Context, clientset kubernetes.Interface, namespace, podName, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar status do pod: %w", err)
		}
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name != containerName {
				continue
			}
			if status.State.Running != nil {
				return nil
			}
			if status.State.Terminated != nil {
				return fmt.Errorf("ephemeral container terminou inesperadamente: %s", status.State.Terminated.Reason)
			}
			if status.State.Waiting != nil && status.State.Waiting.Reason != "ContainerCreating" {
				return fmt.Errorf("ephemeral container aguardando: %s", status.State.Waiting.Reason)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(kafkaTestEphemeralPollInterval):
		}
	}
	return fmt.Errorf("timeout esperando ephemeral container ficar pronto (%s)", timeout)
}

// resolveKafkaCredentials devolve (username, password) — da fonte manual ou de um Secret K8s
// (lido diretamente via clientset; client-go já decodifica base64 em secret.Data). Nunca expõe a
// senha de volta pro chamador HTTP — só é usada aqui, server-side, pra montar o comando do pod.
func resolveKafkaCredentials(ctx context.Context, clientset kubernetes.Interface, sasl *KafkaSASLConfig) (username, password string, err error) {
	if sasl.SecretRef != nil {
		ref := sasl.SecretRef
		secret, getErr := clientset.CoreV1().Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if getErr != nil {
			return "", "", fmt.Errorf("falha ao ler secret %s/%s: %w", ref.Namespace, ref.Name, getErr)
		}
		userKey := ref.UsernameKey
		if userKey == "" {
			userKey = "username"
		}
		passKey := ref.PasswordKey
		if passKey == "" {
			passKey = "password"
		}
		userBytes, ok := secret.Data[userKey]
		if !ok {
			return "", "", fmt.Errorf("chave %q não encontrada no secret %s/%s", userKey, ref.Namespace, ref.Name)
		}
		passBytes, ok := secret.Data[passKey]
		if !ok {
			return "", "", fmt.Errorf("chave %q não encontrada no secret %s/%s", passKey, ref.Namespace, ref.Name)
		}
		return string(userBytes), string(passBytes), nil
	}
	return sasl.Username, sasl.Password, nil
}

// buildKcatAuthFlags monta os `-X key=value` do kcat a partir da config SASL/TLS resolvida.
// Sem `sasl`, ainda cobre o caso "só TLS, sem autenticação" (security.protocol=SSL).
func buildKcatAuthFlags(sasl *KafkaSASLConfig, username, password string) []string {
	if sasl == nil {
		return nil
	}
	var flags []string
	hasCreds := username != "" || password != ""
	switch {
	case hasCreds && sasl.UseTLS:
		flags = append(flags, "-X", "security.protocol=SASL_SSL")
	case hasCreds:
		flags = append(flags, "-X", "security.protocol=SASL_PLAINTEXT")
	case sasl.UseTLS:
		flags = append(flags, "-X", "security.protocol=SSL")
	}
	if hasCreds {
		mechanism := sasl.Mechanism
		if mechanism == "" {
			mechanism = kafkaSASLMechanismPlain
		}
		flags = append(flags, "-X", "sasl.mechanisms="+mechanism)
		flags = append(flags, "-X", "sasl.username="+username)
		flags = append(flags, "-X", "sasl.password="+password)
	}
	if sasl.UseTLS && sasl.SkipTLSVerify {
		flags = append(flags, "-X", "enable.ssl.certificate.verification=false")
	}
	return flags
}

// quoteShellArg envolve um argumento em aspas simples pra uso seguro dentro de `sh -c "..."`,
// escapando aspas simples internas (padrão POSIX: fecha aspa, escapa aspa literal, reabre aspa).
func quoteShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildKcatCommand monta a linha de comando kcat completa (broker + flags de auth + args extras),
// já com cada argumento individualmente quotado pra `sh -c`.
func buildKcatCommand(broker string, authFlags []string, extraArgs ...string) string {
	parts := []string{"kcat", "-b", quoteShellArg(broker)}
	for _, f := range authFlags {
		parts = append(parts, quoteShellArg(f))
	}
	for _, a := range extraArgs {
		parts = append(parts, quoteShellArg(a))
	}
	return strings.Join(parts, " ")
}

// extractStderr extrai o texto de stderr de um erro devolvido por execCmdInPod (formato
// "stream: %v (stderr: %s)") — usado só pra classificação/exibição, não muda execCmdInPod.
func extractStderr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	idx := strings.Index(msg, "(stderr: ")
	if idx == -1 {
		return msg
	}
	rest := msg[idx+len("(stderr: "):]
	return strings.TrimSuffix(rest, ")")
}

// runKafkaConnectivityStage roda `kcat -L` (metadata) — cobre TCP+DNS+protocolo Kafka+SASL num
// único exec, classificando o resultado por padrões conhecidos de erro do rdkafka.
func runKafkaConnectivityStage(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName, broker string, authFlags []string, timeoutMs int) KafkaStageResult {

	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cmd := buildKcatCommand(broker, authFlags, "-L")
	script := fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)

	stdout, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", script})
	if err != nil {
		raw := extractStderr(err)
		switch {
		case kafkaNetworkErrorRegex.MatchString(raw):
			return KafkaStageResult{Status: kafkaStageTCPFailed, Message: "Não conseguiu conectar no broker (rede/DNS)", RawOutput: raw}
		case kafkaAuthErrorRegex.MatchString(raw):
			return KafkaStageResult{Status: kafkaStageAuthFailed, Message: "Falha de autenticação SASL", RawOutput: raw}
		case kafkaTLSErrorRegex.MatchString(raw):
			return KafkaStageResult{Status: kafkaStageTLSFailed, Message: "Falha de handshake TLS/SSL", RawOutput: raw}
		default:
			return KafkaStageResult{Status: kafkaStageUnknownFailed, Message: "Falha não classificada — ver saída bruta", RawOutput: raw}
		}
	}

	// Nota: `2>&1` acima junta stdout+stderr no `stdout` de sucesso, já que aqui não há stderr
	// separado a extrair (execCmdInPod só devolve stdout puro no caso sem erro).
	result := KafkaStageResult{Status: kafkaStageOK, Message: "Conectividade e protocolo Kafka OK", RawOutput: stdout}
	if m := kafkaBrokerCountRegex.FindStringSubmatch(stdout); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &result.BrokerCount)
	}
	if m := kafkaTopicCountRegex.FindStringSubmatch(stdout); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &result.TopicCount)
	}
	return result
}

// runKafkaProduceConsumeStage produz uma mensagem com um marcador único e tenta consumi-la de
// volta — compromisso de simplicidade: lê até kafkaTestConsumeMaxMessages desde o início do
// tópico em vez de calcular o offset exato pré-produce (ver doc do plano).
func runKafkaProduceConsumeStage(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName, broker, topic string, authFlags []string, timeoutMs int) KafkaProduceConsumeResult {

	marker := "k8s-hpa-manager-test-" + uuid.New().String()
	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	start := time.Now()

	produceCmd := buildKcatCommand(broker, authFlags, "-P", "-t", topic)
	produceScript := fmt.Sprintf("printf '%%s' %s | timeout %ds %s 2>&1", quoteShellArg(marker), timeoutSec, produceCmd)
	produceOut, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", produceScript})
	if err != nil {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Falha ao produzir mensagem de teste", RawOutput: extractStderr(err)}
	}

	consumeCmd := buildKcatCommand(broker, authFlags, "-C", "-t", topic, "-o", "beginning", "-c", fmt.Sprintf("%d", kafkaTestConsumeMaxMessages), "-e")
	consumeScript := fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, consumeCmd)
	consumeOut, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", consumeScript})
	roundTrip := time.Since(start).Milliseconds()
	if err != nil {
		raw := extractStderr(err)
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Mensagem produzida, mas falha ao consumir de volta", RawOutput: produceOut + "\n---\n" + raw}
	}

	rawOutput := produceOut + "\n---\n" + consumeOut
	if strings.Contains(consumeOut, marker) {
		return KafkaProduceConsumeResult{Status: "ok", Message: "Mensagem de teste produzida e consumida com sucesso", RoundTripMs: roundTrip, RawOutput: rawOutput}
	}
	return KafkaProduceConsumeResult{
		Status:    "not_found",
		Message:   fmt.Sprintf("Mensagem produzida, mas não encontrada consumindo as últimas %d mensagens do tópico (pode estar além desse alcance)", kafkaTestConsumeMaxMessages),
		RawOutput: rawOutput,
	}
}

// ─── Handler: endpoint SSE + rotas ─────────────────────────────────────────────

// KafkaTestHandler orquestra o teste de Kafka sob demanda — mesmo esqueleto do LatencyTestHandler
// (SSE, lock de 1-teste-por-usuário), mas SEM criar/limpar pod: o teste anexa um ephemeral
// container num pod real já existente (ver resolveRunningPodForDeployment), então não há recurso
// descartável pra varrer — nenhum sweeper de órfãos é necessário aqui.
type KafkaTestHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	cancelFuncs    sync.Map // sessionID -> context.CancelFunc
	runningUsers   sync.Map // userEmail -> struct{} — "um teste por vez por usuário"
}

// NewKafkaTestHandler cria o handler do teste de Kafka.
func NewKafkaTestHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *KafkaTestHandler {
	return &KafkaTestHandler{kubeManager: km, tracker: tracker, historyTracker: ht}
}

// Run inicia o teste de Kafka e retorna um session_id para streaming SSE.
// POST /api/v1/kafka-test/run
func (h *KafkaTestHandler) Run(c *gin.Context) {
	var req RunKafkaTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Broker = strings.TrimSpace(req.Broker)
	if req.Cluster == "" || req.Namespace == "" || req.Deployment == "" || req.Broker == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace, deployment e broker são obrigatórios"))
		return
	}

	if req.SASL != nil {
		req.SASL.Mechanism = strings.ToUpper(strings.TrimSpace(req.SASL.Mechanism))
		if req.SASL.Mechanism != "" && !kafkaValidSASLMechanisms[req.SASL.Mechanism] {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_MECHANISM", "mechanism deve ser PLAIN, SCRAM-SHA-256 ou SCRAM-SHA-512"))
			return
		}
		if req.SASL.SecretRef != nil {
			req.SASL.SecretRef.Namespace = strings.TrimSpace(req.SASL.SecretRef.Namespace)
			req.SASL.SecretRef.Name = strings.TrimSpace(req.SASL.SecretRef.Name)
			if req.SASL.SecretRef.Namespace == "" || req.SASL.SecretRef.Name == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_SECRET_REF", "secret_ref precisa de namespace e name"))
				return
			}
		}
	}

	if req.ProduceConsume {
		req.Topic = strings.TrimSpace(req.Topic)
		if req.Topic == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_TOPIC", "topic é obrigatório para produzir/consumir"))
			return
		}
		// Guardrail: produce ESCREVE estado real num tópico — exige confirmação explícita,
		// nunca só a presença do campo topic.
		if !req.ConfirmProduce {
			c.JSON(http.StatusBadRequest, errorResponse("PRODUCE_NOT_CONFIRMED", "confirm_produce precisa ser true para publicar uma mensagem de teste real no tópico"))
			return
		}
	}

	if req.TimeoutMs <= 0 {
		req.TimeoutMs = kafkaTestDefaultTimeoutMs
	}
	if req.TimeoutMs > kafkaTestMaxTimeoutMs {
		req.TimeoutMs = kafkaTestMaxTimeoutMs
	}

	userInfo := GetUserInfoForHistory(c)

	lockKey := userInfo.Email
	if lockKey == "" {
		lockKey = "unknown"
	}
	if _, alreadyRunning := h.runningUsers.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("TEST_ALREADY_RUNNING",
			"você já tem um teste de Kafka em andamento — aguarde terminar ou cancele antes de iniciar outro"))
		return
	}

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(sessionID)
		defer h.runningUsers.Delete(lockKey)
		h.runTest(ctx, sessionID, req, userInfo)
	}()

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// Stream conecta o cliente ao fluxo SSE de um teste em andamento.
// GET /api/v1/kafka-test/stream/:sessionId
func (h *KafkaTestHandler) Stream(c *gin.Context) {
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

// Cancel força a parada de um teste em andamento — o cleanup do pod roda de qualquer forma.
// POST /api/v1/kafka-test/cancel/:sessionId
func (h *KafkaTestHandler) Cancel(c *gin.Context) {
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

// runTest executa o fluxo completo (criar pod → aguardar ready → conectividade →
// produce/consume opcional), reportando progresso via SSE a cada etapa.
func (h *KafkaTestHandler) runTest(ctx context.Context, sessionID string, req RunKafkaTestRequest, userInfo history.UserInfo) {
	start := time.Now()

	send := func(evtType, phase, message string, progress float64) {
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      evtType,
			Phase:     phase,
			Message:   message,
			Progress:  progress,
			Timestamp: time.Now(),
			Cluster:   req.Cluster,
		})
	}

	fail := func(stage string, err error) {
		send("error", "failed", fmt.Sprintf("%s: %v", stage, err), 1.0)
		h.logHistory(req, userInfo, start, nil, fmt.Errorf("%s: %w", stage, err))
	}

	send("init", "started", "Iniciando teste de Kafka...", 0.05)

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

	var username, password string
	authFlags := []string(nil)
	if req.SASL != nil {
		username, password, err = resolveKafkaCredentials(ctx, clientset, req.SASL)
		if err != nil {
			fail("falha ao resolver credenciais", err)
			return
		}
		authFlags = buildKcatAuthFlags(req.SASL, username, password)
	}

	send("resolve_deployment", "in_progress", fmt.Sprintf("Localizando pod Running do deployment %q...", req.Deployment), 0.15)
	podName, targetContainer, err := resolveRunningPodForDeployment(ctx, clientset, req.Namespace, req.Deployment)
	if err != nil {
		fail("falha ao localizar pod do deployment", err)
		return
	}

	send("ephemeral_container", "in_progress", fmt.Sprintf("Anexando container de teste no pod %s...", podName), 0.3)
	containerName, err := getOrCreateKafkaEphemeralContainer(ctx, clientset, req.Namespace, podName, targetContainer)
	if err != nil {
		fail("falha ao anexar ephemeral container", err)
		return
	}
	if err := waitKafkaEphemeralContainerRunning(ctx, clientset, req.Namespace, podName, containerName, kafkaTestEphemeralReadyTimeout); err != nil {
		fail("ephemeral container não ficou pronto", err)
		return
	}

	send("connectivity", "in_progress", "Testando conectividade e protocolo Kafka...", 0.5)
	connectivity := runKafkaConnectivityStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, authFlags, req.TimeoutMs)

	result := KafkaTestResult{
		TargetPod:      podName,
		Connectivity:   connectivity,
		ProduceConsume: KafkaProduceConsumeResult{Status: "skipped"},
	}

	if req.ProduceConsume {
		if connectivity.Status != kafkaStageOK {
			result.ProduceConsume = KafkaProduceConsumeResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar produzir"}
		} else {
			send("produce_consume", "in_progress", fmt.Sprintf("Produzindo e consumindo mensagem de teste no tópico %q...", req.Topic), 0.8)
			result.ProduceConsume = runKafkaProduceConsumeStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, req.Topic, authFlags, req.TimeoutMs)
		}
	}

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "complete",
		Phase:     "completed",
		Message:   "Teste de Kafka concluído",
		Progress:  1.0,
		Timestamp: time.Now(),
		Cluster:   req.Cluster,
		Result:    result,
	})
	h.logHistory(req, userInfo, start, &result, nil)
}

// logHistory registra a execução no HistoryTracker. Nunca inclui username/password — só
// broker/topic/namespace/resultado resumido, mesmo quando a fonte da credencial foi manual.
func (h *KafkaTestHandler) logHistory(req RunKafkaTestRequest, userInfo history.UserInfo, start time.Time, result *KafkaTestResult, opErr error) {
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
		"namespace":       req.Namespace,
		"deployment":      req.Deployment,
		"broker":          req.Broker,
		"produce_consume": req.ProduceConsume,
	}
	if req.Topic != "" {
		after["topic"] = req.Topic
	}
	if req.SASL != nil {
		after["sasl_mechanism"] = req.SASL.Mechanism
		after["use_tls"] = req.SASL.UseTLS
	}
	if result != nil {
		after["target_pod"] = result.TargetPod
		after["connectivity_status"] = result.Connectivity.Status
		after["produce_consume_status"] = result.ProduceConsume.Status
	}

	h.historyTracker.Log(history.HistoryEntry{
		UserEmail: userInfo.Email,
		UserName:  userInfo.Name,
		Action:    "kafka_test",
		Resource:  req.Broker,
		Cluster:   req.Cluster,
		Status:    status,
		After:     after,
		Duration:  time.Since(start).Milliseconds(),
		ErrorMsg:  errMsg,
	})
}
