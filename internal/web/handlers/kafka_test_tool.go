package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	// kafkaTestViewDefaultMessages/MaxMessages limitam o estágio de visualização (só leitura) —
	// lê as últimas N mensagens já existentes no tópico via offset negativo do kcat (-o -N).
	kafkaTestViewDefaultMessages = 10
	kafkaTestViewMaxMessages     = 50
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

// kafkaMechanismHintRegex tenta extrair, de uma mensagem de erro de handshake SASL, os
// mecanismos que o BROKER realmente aceita — o Kafka geralmente informa isso quando o client
// tenta um mecanismo errado (ex: "Unsupported SASL mechanism: PLAIN not in [SCRAM-SHA-512]").
// Best-effort/heurístico — a saída bruta continua sempre disponível se isso não casar.
var kafkaMechanismHintRegex = regexp.MustCompile(`(?i)mechanisms?[^A-Za-z0-9]{0,20}((?:PLAIN|SCRAM-SHA-256|SCRAM-SHA-512|GSSAPI|OAUTHBEARER)(?:[,\s]+(?:PLAIN|SCRAM-SHA-256|SCRAM-SHA-512|GSSAPI|OAUTHBEARER))*)`)
var kafkaTLSErrorRegex = regexp.MustCompile(`(?i)ssl`)
var kafkaBrokerCountRegex = regexp.MustCompile(`(\d+)\s+brokers?:`)
var kafkaTopicCountRegex = regexp.MustCompile(`(\d+)\s+topics?:`)

// kafkaPartitionCountRegex extrai o número de partições da linha de metadados de UM tópico
// específico (`kcat -L -t <topico>`) — ex: `topic "meu-topico" with 3 partitions:`. Quando o
// tópico não existe, o kcat ainda sai com código 0 e imprime "with 0 partitions: Broker: Unknown
// topic or partition" — daí `partitionCount == 0` ser o sinal de "tópico não encontrado", não um
// erro de exec.
var kafkaPartitionCountRegex = regexp.MustCompile(`with (\d+) partitions`)

// kafkaTopicNameRegex extrai nomes de tópicos da listagem de metadados completa (`kcat -L`, sem
// `-t`) — usado pelo endpoint de busca de tópicos. Formato: `  topic "nome" with N partitions:`.
var kafkaTopicNameRegex = regexp.MustCompile(`topic\s+"([^"]+)"\s+with\s+(\d+)\s+partitions`)

// kafkaOffsetLineRegex extrai partição+offset da saída do modo `-Q` (query de offset por
// timestamp) — formato confirmado contra um broker real: `<topico> [<partição>] offset <N>`.
var kafkaOffsetLineRegex = regexp.MustCompile(`\[(\d+)\]\s+offset\s+(-?\d+)`)

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
	// Base64Decode decodifica username/password mais uma vez depois de ler do Secret — necessário
	// quando o VALOR sincronizado da fonte externa (ex: Azure Key Vault via external-secrets) já é,
	// ele mesmo, uma string em base64 (prática comum pra evitar caracteres especiais em connection
	// strings). O client-go já decodifica o base64 "de transporte" do Secret automaticamente — isso
	// aqui é uma camada A MAIS em cima disso, opcional.
	Base64Decode bool `json:"base64_decode,omitempty"`
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
	Topic          string           `json:"topic,omitempty"` // usado por ProduceConsume e/ou ViewTopic
	// ConfirmProduce precisa ser true explicitamente pra rodar o estágio de produce/consume —
	// diferente de TCP/protocolo (só leitura), produce ESCREVE uma mensagem real no tópico.
	ConfirmProduce bool `json:"confirm_produce"`
	// ViewTopic lê (só leitura, sem escrever nada — não precisa de ConfirmProduce) as últimas
	// mensagens já existentes no tópico informado em Topic.
	ViewTopic       bool `json:"view_topic"`
	ViewMaxMessages int  `json:"view_max_messages,omitempty"` // default 10, teto 50
	// CountOffsets lê (só leitura) os offsets mais antigo/mais recente de cada partição do tópico
	// informado em Topic, e deriva a contagem de mensagens atualmente retidas — não precisa de
	// ConfirmProduce, não escreve nada no broker.
	CountOffsets bool `json:"count_offsets"`
	TimeoutMs    int  `json:"timeout_ms"`
}

// KafkaStageResult é o resultado de um estágio individual do teste.
type KafkaStageResult struct {
	Status      string `json:"status"` // ok | tcp_failed | auth_failed | tls_failed | unknown_failed | skipped
	Message     string `json:"message"`
	RawOutput   string `json:"raw_output"`
	BrokerCount int    `json:"broker_count,omitempty"`
	TopicCount  int    `json:"topic_count,omitempty"`
	// SuggestedMechanism vem de kafkaMechanismHintRegex quando o erro de auth_failed menciona os
	// mecanismos SASL que o broker realmente aceita — extração best-effort, pode vir vazia mesmo
	// quando o broker informou isso num formato que a regex não reconhece.
	SuggestedMechanism string `json:"suggested_mechanism,omitempty"`
}

// KafkaProduceConsumeResult é o resultado do round-trip de produce/consume.
type KafkaProduceConsumeResult struct {
	Status      string `json:"status"` // ok | produce_failed | not_found | skipped
	Message     string `json:"message"`
	RoundTripMs int64  `json:"round_trip_ms,omitempty"`
	RawOutput   string `json:"raw_output"`
}

// KafkaMessage é uma mensagem lida do tópico pelo estágio de visualização (só leitura).
type KafkaMessage struct {
	Partition int32 `json:"partition"`
	Offset    int64 `json:"offset"`
	// TimestampMs vem do campo "ts" do kcat (epoch ms) — 0 quando o broker não reporta timestamp
	// de criação da mensagem (versões antigas de protocolo).
	TimestampMs int64  `json:"timestamp_ms,omitempty"`
	Key         string `json:"key,omitempty"`
	Payload     string `json:"payload"`
	// Binary = true quando Key/Payload contém o caractere de substituição U+FFFD — sinal de que o
	// kcat já substituiu bytes inválidos de UTF-8 antes de emitir o JSON (payload binário de
	// verdade, ex: protobuf/Avro, ou uma mensagem de tópico interno do Kafka como
	// __consumer_offsets). Os bytes originais já se perderam nesse ponto — não é recuperável.
	Binary bool `json:"binary,omitempty"`
}

// KafkaTopicViewResult é o resultado do estágio de visualização (só leitura) de um tópico.
type KafkaTopicViewResult struct {
	Status    string         `json:"status"` // ok | failed | skipped
	Message   string         `json:"message"`
	Messages  []KafkaMessage `json:"messages,omitempty"`
	RawOutput string         `json:"raw_output"`
}

// KafkaOffsetPartition é o par de offsets (mais antigo/mais recente) de uma partição — Count é a
// diferença entre eles, ou seja, quantas mensagens estão ATUALMENTE retidas nessa partição (não o
// total histórico já produzido, já que a política de retenção pode ter apagado mensagens antigas
// — earliest só reflete o que o broker ainda guarda).
type KafkaOffsetPartition struct {
	Partition int32 `json:"partition"`
	Earliest  int64 `json:"earliest"`
	Latest    int64 `json:"latest"`
	Count     int64 `json:"count"`
}

// KafkaOffsetCountResult é o resultado do estágio de contagem de offsets (só leitura) de um tópico.
type KafkaOffsetCountResult struct {
	Status        string                 `json:"status"` // ok | not_found | failed | skipped
	Message       string                 `json:"message"`
	TotalMessages int64                  `json:"total_messages,omitempty"`
	Partitions    []KafkaOffsetPartition `json:"partitions,omitempty"`
	RawOutput     string                 `json:"raw_output"`
}

// KafkaTestResult é o resultado completo de uma execução do teste de Kafka.
type KafkaTestResult struct {
	// TargetPod é o pod real (do Deployment escolhido) onde o ephemeral container do teste foi
	// anexado — transparência de qual carga específica foi tocada, já que isso é uma mutação
	// (ainda que pequena) num pod real, não um recurso descartável criado do zero.
	TargetPod string `json:"target_pod"`
	// EphemeralContainer é o nome exato do container anexado — ephemeral containers não podem
	// ser removidos via API do K8s (ficam listados no pod até ele reiniciar); esse nome permite
	// conferir o estado dele depois via `kubectl get pod <target_pod> -o
	// jsonpath='{.status.ephemeralContainerStatuses}'`.
	EphemeralContainer string                    `json:"ephemeral_container"`
	Connectivity       KafkaStageResult          `json:"connectivity"`
	ProduceConsume     KafkaProduceConsumeResult `json:"produce_consume"`
	ViewTopic          KafkaTopicViewResult      `json:"view_topic"`
	OffsetCount        KafkaOffsetCountResult    `json:"offset_count"`
}

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
		username, password = string(userBytes), string(passBytes)
		if ref.Base64Decode {
			username, err = decodeKafkaSecretValue(username)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", userKey, err)
			}
			password, err = decodeKafkaSecretValue(password)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", passKey, err)
			}
		}
		return username, password, nil
	}
	return sasl.Username, sasl.Password, nil
}

// decodeKafkaSecretValue decodifica uma string em base64 — tenta StdEncoding primeiro (com
// padding, o formato mais comum) e cai pra RawStdEncoding (sem padding) se falhar, já que fontes
// externas (ex: AKV) nem sempre preservam o `=` de padding. Faz `TrimSpace` antes: base64 exportado
// via `echo valor | base64` (sem `-n`) ou copiado manualmente costuma vir com `\n` no final, o que
// quebra o decode mesmo o conteúdo sendo válido.
func decodeKafkaSecretValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if decoded, err := base64.StdEncoding.DecodeString(v); err == nil {
		return string(decoded), nil
	}
	decoded, err := base64.RawStdEncoding.DecodeString(v)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
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

// kafkaExitMarker é anexado ao final de todo script kcat rodado pela ferramenta — necessário
// porque os 3 estágios usam `2>&1` pra juntar stdout+stderr do kcat num único texto (é esse texto
// combinado que as regexes de classificação de erro precisam inspecionar), mas execCmdInPod
// DESCARTA o stdout capturado sempre que o comando remoto sai com código != 0 (só devolve o
// stderr "puro" da própria sessão de exec no erro — que fica vazio aqui, já que tudo foi
// redirecionado pro stdout do processo pelo `2>&1`). Sem esse wrapper, qualquer falha real do
// kcat (SASL errado, TLS errado, rede) perdia o texto de diagnóstico e caía sempre no bucket
// genérico "unknown_failed", sem nunca rodar as regexes de classificação — mesmo bug de classe já
// contornado em runICMPProbe/runHTTPProbe (latency_test_tool.go, com `; true`), só que lá não há
// necessidade de saber o código de saída real, e aqui há. A correção: o `sh -c` some sempre sai 0
// (`; echo "marker$?"`), e o código de saída real vai embutido no fim da saída — permite tanto
// preservar o texto quanto saber se o comando teve sucesso.
const kafkaExitMarker = "\n___KAFKA_TEST_EXIT_CODE___:"

// wrapKafkaScript envolve um script pra nunca deixar o `sh -c` sair != 0 (evita que execCmdInPod
// descarte a saída) e imprime o código de saída real do comando interno numa marca ao final.
func wrapKafkaScript(script string) string {
	return fmt.Sprintf(`{ %s; }; __rc=$?; printf '%s%%d' "$__rc"`, script, kafkaExitMarker)
}

// splitKafkaExitMarker separa o texto de diagnóstico do código de saída real embutido por
// wrapKafkaScript. Se a marca não for encontrada (não deveria acontecer), trata como falha —
// mais seguro do que assumir sucesso silenciosamente.
func splitKafkaExitMarker(output string) (text string, exitCode int, ok bool) {
	idx := strings.LastIndex(output, kafkaExitMarker)
	if idx == -1 {
		return output, -1, false
	}
	text = output[:idx]
	code, err := strconv.Atoi(strings.TrimSpace(output[idx+len(kafkaExitMarker):]))
	if err != nil {
		return text, -1, false
	}
	return text, code, true
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
	namespace, podName, containerName, broker string, authFlags []string, saslConfigured bool, timeoutMs int) KafkaStageResult {

	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cmd := buildKcatCommand(broker, authFlags, "-L")
	script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))

	output, execErr := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", script})
	if execErr != nil {
		return KafkaStageResult{Status: kafkaStageUnknownFailed, Message: "Falha ao executar o teste no pod", RawOutput: extractStderr(execErr)}
	}

	raw, exitCode, ok := splitKafkaExitMarker(output)
	if !ok || exitCode != 0 {
		switch {
		case kafkaNetworkErrorRegex.MatchString(raw):
			return KafkaStageResult{Status: kafkaStageTCPFailed, Message: "Não conseguiu conectar no broker (rede/DNS)", RawOutput: raw}
		case kafkaAuthErrorRegex.MatchString(raw):
			message := "Falha de autenticação SASL"
			if !saslConfigured {
				message = "TCP conectou, mas o broker exige autenticação SASL — ligue \"Autenticação SASL\" e informe as credenciais"
			}
			result := KafkaStageResult{Status: kafkaStageAuthFailed, Message: message, RawOutput: raw}
			if m := kafkaMechanismHintRegex.FindStringSubmatch(raw); len(m) == 2 {
				result.SuggestedMechanism = strings.TrimSpace(m[1])
				result.Message += fmt.Sprintf(" (o broker indicou aceitar: %s)", result.SuggestedMechanism)
			}
			return result
		case kafkaTLSErrorRegex.MatchString(raw):
			return KafkaStageResult{Status: kafkaStageTLSFailed, Message: "Falha de handshake TLS/SSL", RawOutput: raw}
		default:
			return KafkaStageResult{Status: kafkaStageUnknownFailed, Message: "Falha não classificada — ver saída bruta", RawOutput: raw}
		}
	}

	result := KafkaStageResult{Status: kafkaStageOK, Message: "Conectividade e protocolo Kafka OK", RawOutput: raw}
	if m := kafkaBrokerCountRegex.FindStringSubmatch(raw); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &result.BrokerCount)
	}
	if m := kafkaTopicCountRegex.FindStringSubmatch(raw); len(m) == 2 {
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
	produceScript := wrapKafkaScript(fmt.Sprintf("printf '%%s' %s | timeout %ds %s 2>&1", quoteShellArg(marker), timeoutSec, produceCmd))
	produceRaw, execErr := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", produceScript})
	if execErr != nil {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Falha ao executar o produce no pod", RawOutput: extractStderr(execErr)}
	}
	produceOut, produceExit, ok := splitKafkaExitMarker(produceRaw)
	if !ok || produceExit != 0 {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Falha ao produzir mensagem de teste", RawOutput: produceOut}
	}

	consumeCmd := buildKcatCommand(broker, authFlags, "-C", "-t", topic, "-o", "beginning", "-c", fmt.Sprintf("%d", kafkaTestConsumeMaxMessages), "-e")
	consumeScript := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, consumeCmd))
	consumeRaw, execErr := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", consumeScript})
	roundTrip := time.Since(start).Milliseconds()
	if execErr != nil {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Mensagem produzida, mas falha ao executar o consume no pod", RawOutput: produceOut + "\n---\n" + extractStderr(execErr)}
	}
	consumeOut, consumeExit, ok := splitKafkaExitMarker(consumeRaw)
	if !ok || consumeExit != 0 {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Mensagem produzida, mas falha ao consumir de volta", RawOutput: produceOut + "\n---\n" + consumeOut}
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

// kcatJSONMessage é o formato de linha JSON que o kcat emite em modo `-C -J` (uma linha por
// mensagem consumida, mais linhas de diagnóstico começando com "%" que não são JSON válido e são
// ignoradas na hora de popular Messages — ainda aparecem em RawOutput).
type kcatJSONMessage struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Offset    int64  `json:"offset"`
	TS        int64  `json:"ts"`
	Key       string `json:"key"`
	Payload   string `json:"payload"`
}

// runKafkaViewTopicStage lê (só leitura, nada é escrito) as últimas `maxMessages` mensagens já
// existentes no tópico via offset negativo do kcat (-o -N lê as N mensagens antes do fim de cada
// partição) — não precisa de ConfirmProduce porque não muta nada no broker.
func runKafkaViewTopicStage(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName, broker, topic string, maxMessages int, authFlags []string, timeoutMs int) KafkaTopicViewResult {

	if maxMessages <= 0 {
		maxMessages = kafkaTestViewDefaultMessages
	}
	if maxMessages > kafkaTestViewMaxMessages {
		maxMessages = kafkaTestViewMaxMessages
	}

	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	cmd := buildKcatCommand(broker, authFlags, "-C", "-t", topic, "-o", fmt.Sprintf("-%d", maxMessages), "-c", fmt.Sprintf("%d", maxMessages), "-e", "-J")
	script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))

	output, execErr := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", script})
	if execErr != nil {
		return KafkaTopicViewResult{Status: "failed", Message: "Falha ao executar a leitura no pod", RawOutput: extractStderr(execErr)}
	}
	stdout, exitCode, ok := splitKafkaExitMarker(output)
	if !ok || exitCode != 0 {
		return KafkaTopicViewResult{Status: "failed", Message: "Falha ao ler mensagens do tópico", RawOutput: stdout}
	}

	var messages []KafkaMessage
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue // linha de diagnóstico do kcat (ex: "%4|...|..."), não é uma mensagem
		}
		var raw kcatJSONMessage
		if jsonErr := json.Unmarshal([]byte(line), &raw); jsonErr != nil {
			continue
		}
		messages = append(messages, KafkaMessage{
			Partition:   raw.Partition,
			Offset:      raw.Offset,
			TimestampMs: raw.TS,
			Key:         raw.Key,
			Payload:     raw.Payload,
			Binary:      strings.ContainsRune(raw.Key, utf8.RuneError) || strings.ContainsRune(raw.Payload, utf8.RuneError),
		})
	}

	if len(messages) == 0 {
		return KafkaTopicViewResult{Status: "ok", Message: "Nenhuma mensagem encontrada no tópico (ou tópico vazio)", RawOutput: stdout}
	}

	binaryCount := 0
	for _, m := range messages {
		if m.Binary {
			binaryCount++
		}
	}
	message := fmt.Sprintf("%d mensagem(ns) lida(s)", len(messages))
	if binaryCount > 0 {
		message += fmt.Sprintf(" — %d parece(m) conter dados binários (não-UTF8); exibição pode estar incompleta, ver nota na mensagem", binaryCount)
	}

	return KafkaTopicViewResult{
		Status:    "ok",
		Message:   message,
		Messages:  messages,
		RawOutput: stdout,
	}
}

// buildKafkaOffsetQueryArgs monta os argumentos `-Q -t topico:partição:timestamp` pra todas as
// partições de 0 a partitionCount-1 com o MESMO timestamp especial (-1 = offset mais recente/fim,
// -2 = offset mais antigo/início — semântica padrão do protocolo Kafka ListOffsets).
func buildKafkaOffsetQueryArgs(topic string, partitionCount int, timestamp int) []string {
	args := make([]string, 0, 1+partitionCount*2)
	args = append(args, "-Q")
	for p := 0; p < partitionCount; p++ {
		args = append(args, "-t", fmt.Sprintf("%s:%d:%d", topic, p, timestamp))
	}
	return args
}

// parseKafkaOffsetLines extrai partição→offset da saída do modo `-Q` do kcat.
func parseKafkaOffsetLines(raw string) map[int32]int64 {
	result := make(map[int32]int64)
	for _, m := range kafkaOffsetLineRegex.FindAllStringSubmatch(raw, -1) {
		p, _ := strconv.Atoi(m[1])
		offset, _ := strconv.ParseInt(m[2], 10, 64)
		result[int32(p)] = offset
	}
	return result
}

// runKafkaOffsetCountStage lê (só leitura, nada é escrito) o offset mais antigo e o mais recente
// de cada partição do tópico e deriva quantas mensagens estão atualmente retidas (latest -
// earliest, por partição, somado). Precisa de 3 execs: (1) `-L -t topico` pra descobrir o número
// de partições — também serve pra detectar tópico inexistente, já que o kcat sai com código 0 e
// imprime "with 0 partitions: Broker: Unknown topic or partition" nesse caso, em vez de um erro
// de exec; (2) `-Q` com timestamp -1 (fim) pra cada partição; (3) `-Q` com timestamp -2 (início)
// pra cada partição.
//
// Os dois `-Q` são feitos em EXECS SEPARADOS de propósito — testado empiricamente contra um
// broker real que o kcat 1.7.1 devolve o MESMO valor pras duas consultas quando -1 e -2 da MESMA
// partição aparecem juntos numa única invocação `-Q` (limitação/bug não documentado da própria
// ferramenta, provável dedup interno por partição na hora de montar o batch de queries).
func runKafkaOffsetCountStage(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName, broker, topic string, authFlags []string, timeoutMs int) KafkaOffsetCountResult {

	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	runStep := func(extraArgs ...string) (raw string, exitCode int, execErr error) {
		cmd := buildKcatCommand(broker, authFlags, extraArgs...)
		script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))
		output, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", script})
		if err != nil {
			return extractStderr(err), -1, err
		}
		text, code, ok := splitKafkaExitMarker(output)
		if !ok {
			return text, -1, nil
		}
		return text, code, nil
	}

	metaRaw, metaExit, execErr := runStep("-L", "-t", topic)
	if execErr != nil {
		return KafkaOffsetCountResult{Status: "failed", Message: "Falha ao executar a consulta de metadados no pod", RawOutput: metaRaw}
	}
	if metaExit != 0 {
		return KafkaOffsetCountResult{Status: "failed", Message: "Falha ao consultar metadados do tópico", RawOutput: metaRaw}
	}
	m := kafkaPartitionCountRegex.FindStringSubmatch(metaRaw)
	if m == nil {
		return KafkaOffsetCountResult{Status: "failed", Message: "Não foi possível determinar o número de partições do tópico — ver saída bruta", RawOutput: metaRaw}
	}
	partitionCount, _ := strconv.Atoi(m[1])
	if partitionCount == 0 {
		return KafkaOffsetCountResult{Status: "not_found", Message: fmt.Sprintf("Tópico %q não encontrado no broker", topic), RawOutput: metaRaw}
	}

	latestRaw, latestExit, execErr := runStep(buildKafkaOffsetQueryArgs(topic, partitionCount, -1)...)
	if execErr != nil || latestExit != 0 {
		return KafkaOffsetCountResult{Status: "failed", Message: "Falha ao consultar os offsets mais recentes", RawOutput: metaRaw + "\n---\n" + latestRaw}
	}
	earliestRaw, earliestExit, execErr := runStep(buildKafkaOffsetQueryArgs(topic, partitionCount, -2)...)
	if execErr != nil || earliestExit != 0 {
		return KafkaOffsetCountResult{Status: "failed", Message: "Falha ao consultar os offsets mais antigos", RawOutput: metaRaw + "\n---\n" + latestRaw + "\n---\n" + earliestRaw}
	}

	latestMap := parseKafkaOffsetLines(latestRaw)
	earliestMap := parseKafkaOffsetLines(earliestRaw)

	partitions := make([]KafkaOffsetPartition, 0, partitionCount)
	var total int64
	for p := 0; p < partitionCount; p++ {
		latest := latestMap[int32(p)]
		earliest := earliestMap[int32(p)]
		count := latest - earliest
		total += count
		partitions = append(partitions, KafkaOffsetPartition{Partition: int32(p), Earliest: earliest, Latest: latest, Count: count})
	}

	return KafkaOffsetCountResult{
		Status:        "ok",
		Message:       fmt.Sprintf("%d partição(ões), %d mensagem(ns) retida(s) atualmente no tópico", partitionCount, total),
		TotalMessages: total,
		Partitions:    partitions,
		RawOutput:     metaRaw + "\n---\n" + latestRaw + "\n---\n" + earliestRaw,
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

	req.Topic = strings.TrimSpace(req.Topic)
	if (req.ProduceConsume || req.ViewTopic || req.CountOffsets) && req.Topic == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_TOPIC", "topic é obrigatório para produzir/consumir, visualizar mensagens ou contar offsets"))
		return
	}
	if req.ProduceConsume {
		// Guardrail: produce ESCREVE estado real num tópico — exige confirmação explícita,
		// nunca só a presença do campo topic. ViewTopic não precisa disso — é só leitura.
		if !req.ConfirmProduce {
			c.JSON(http.StatusBadRequest, errorResponse("PRODUCE_NOT_CONFIRMED", "confirm_produce precisa ser true para publicar uma mensagem de teste real no tópico"))
			return
		}
	}
	if req.ViewMaxMessages < 0 {
		req.ViewMaxMessages = 0
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

// ListTopicsRequest é o body do POST /kafka-test/topics — usado pelo campo de busca de tópicos no
// frontend, pra listar os tópicos existentes no broker sem precisar rodar o teste completo.
type ListTopicsRequest struct {
	Cluster    string           `json:"cluster"`
	Namespace  string           `json:"namespace"`
	Deployment string           `json:"deployment"`
	Broker     string           `json:"broker"`
	SASL       *KafkaSASLConfig `json:"sasl,omitempty"`
	TimeoutMs  int              `json:"timeout_ms"`
}

// ListTopicsResponse é o resultado da listagem de tópicos.
type ListTopicsResponse struct {
	Topics    []string `json:"topics"`
	RawOutput string   `json:"raw_output,omitempty"`
}

// ListTopics resolve um pod do Deployment, anexa (ou reaproveita) o ephemeral container kcat e
// lista os tópicos existentes no broker — usado pelo campo de busca de tópicos no frontend, pra
// não obrigar o usuário a digitar o nome exato de cor. Síncrono (sem SSE): é uma única chamada
// `kcat -L`, rápida mesmo contando o tempo de subir o ephemeral container. Mesma identidade de
// rede do Deployment escolhido (NetworkPolicy/Istio) — os tópicos listados refletem exatamente o
// que aquele workload consegue enxergar, igual ao restante da ferramenta.
// POST /api/v1/kafka-test/topics
func (h *KafkaTestHandler) ListTopics(c *gin.Context) {
	var req ListTopicsRequest
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
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = kafkaTestDefaultTimeoutMs
	}
	if req.TimeoutMs > kafkaTestMaxTimeoutMs {
		req.TimeoutMs = kafkaTestMaxTimeoutMs
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(),
		kafkaTestEphemeralReadyTimeout+time.Duration(req.TimeoutMs)*time.Millisecond+5*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return
	}
	restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return
	}

	var authFlags []string
	if req.SASL != nil {
		username, password, credErr := resolveKafkaCredentials(ctx, clientset, req.SASL)
		if credErr != nil {
			c.JSON(http.StatusBadRequest, errorResponse("CREDENTIALS_ERROR", credErr.Error()))
			return
		}
		authFlags = buildKcatAuthFlags(req.SASL, username, password)
	}

	podName, targetContainer, err := resolveRunningPodForDeployment(ctx, clientset, req.Namespace, req.Deployment)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("POD_NOT_FOUND", err.Error()))
		return
	}
	containerName, err := getOrCreateKafkaEphemeralContainer(ctx, clientset, req.Namespace, podName, targetContainer)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("EPHEMERAL_CONTAINER_ERROR", err.Error()))
		return
	}
	if err := waitKafkaEphemeralContainerRunning(ctx, clientset, req.Namespace, podName, containerName, kafkaTestEphemeralReadyTimeout); err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("EPHEMERAL_CONTAINER_ERROR", err.Error()))
		return
	}

	timeoutSec := (req.TimeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cmd := buildKcatCommand(req.Broker, authFlags, "-L")
	script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))
	output, execErr := execCmdInPod(ctx, clientset, restConfig, req.Namespace, podName, containerName, []string{"sh", "-c", script})
	if execErr != nil {
		c.JSON(http.StatusBadGateway, errorResponse("EXEC_ERROR", extractStderr(execErr)))
		return
	}
	raw, exitCode, ok := splitKafkaExitMarker(output)
	if !ok || exitCode != 0 {
		c.JSON(http.StatusOK, ListTopicsResponse{Topics: []string{}, RawOutput: raw})
		return
	}

	var topics []string
	for _, m := range kafkaTopicNameRegex.FindAllStringSubmatch(raw, -1) {
		topics = append(topics, m[1])
	}
	sort.Strings(topics)

	c.JSON(http.StatusOK, ListTopicsResponse{Topics: topics})
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
	connectivity := runKafkaConnectivityStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, authFlags, req.SASL != nil, req.TimeoutMs)

	result := KafkaTestResult{
		TargetPod:          podName,
		EphemeralContainer: containerName,
		Connectivity:       connectivity,
		ProduceConsume:     KafkaProduceConsumeResult{Status: "skipped"},
		ViewTopic:          KafkaTopicViewResult{Status: "skipped"},
		OffsetCount:        KafkaOffsetCountResult{Status: "skipped"},
	}

	if req.ProduceConsume {
		if connectivity.Status != kafkaStageOK {
			result.ProduceConsume = KafkaProduceConsumeResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar produzir"}
		} else {
			send("produce_consume", "in_progress", fmt.Sprintf("Produzindo e consumindo mensagem de teste no tópico %q...", req.Topic), 0.65)
			result.ProduceConsume = runKafkaProduceConsumeStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, req.Topic, authFlags, req.TimeoutMs)
		}
	}

	if req.CountOffsets {
		if connectivity.Status != kafkaStageOK {
			result.OffsetCount = KafkaOffsetCountResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar contar offsets"}
		} else {
			send("count_offsets", "in_progress", fmt.Sprintf("Contando offsets do tópico %q...", req.Topic), 0.78)
			result.OffsetCount = runKafkaOffsetCountStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, req.Topic, authFlags, req.TimeoutMs)
		}
	}

	if req.ViewTopic {
		if connectivity.Status != kafkaStageOK {
			result.ViewTopic = KafkaTopicViewResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar ler o tópico"}
		} else {
			send("view_topic", "in_progress", fmt.Sprintf("Lendo mensagens existentes do tópico %q...", req.Topic), 0.9)
			result.ViewTopic = runKafkaViewTopicStage(ctx, clientset, restConfig, req.Namespace, podName, containerName, req.Broker, req.Topic, req.ViewMaxMessages, authFlags, req.TimeoutMs)
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
		after["ephemeral_container"] = result.EphemeralContainer
		after["connectivity_status"] = result.Connectivity.Status
		after["produce_consume_status"] = result.ProduceConsume.Status
		after["view_topic_status"] = result.ViewTopic.Status
		after["count_offsets_status"] = result.OffsetCount.Status
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
