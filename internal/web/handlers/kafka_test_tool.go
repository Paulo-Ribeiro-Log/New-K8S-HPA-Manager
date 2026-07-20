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

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/web/sse"
)

const (
	// kafkaTestPodImage: kcat (ex-kafkacat) cobre TCP+protocolo+SASL+produce/consume com um único
	// binário leve. Tag FIXADA de propósito (nunca `latest`). É a imagem do EPHEMERAL CONTAINER
	// anexado a um pod já rodando do Deployment alvo (não um pod novo) — ver
	// resolvePodForDeployment/getOrCreateKafkaEphemeralContainer: o teste precisa refletir
	// a identidade de rede real do Deployment (NetworkPolicy/Istio avaliam por label/service
	// account do pod, não por namespace inteiro), então herdar o namespace de rede de um pod real
	// é o que faz o resultado ser confiável.
	//
	// Usa o rebuild de terceiro `ueisele/kcat` (mesma versão 1.7.1 do kcat, mesma sintaxe de
	// comando) em vez da imagem oficial `edenhill/kcat:1.7.1` porque esta última empacota
	// librdkafka 1.8.2, que NÃO tem o método `sasl.oauthbearer.method=oidc` (só chegou no
	// librdkafka ~1.9 via KIP-768) — confirmado empiricamente: `docker run --rm edenhill/kcat:1.7.1
	// -X sasl.oauthbearer.method=oidc ...` retorna `No such configuration property`. Sem esse
	// método, OAUTHBEARER exigiria um callback externo de renovação de token que o kcat (CLI, não
	// biblioteca) nunca implementou (ver https://github.com/edenhill/kcat/issues/172, em aberto
	// desde 2019). `ueisele/kcat:1.7.1-librdkafka2.1.1` foi testado e aceita os flags de OIDC sem
	// erro de configuração — necessário pra suportar Azure Event Hub via Azure AD (ver
	// buildKcatAuthFlags, branch OAUTHBEARER).
	kafkaTestPodImage = "ueisele/kcat:1.7.1-librdkafka2.1.1"
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

	// kafkaTopicsOverviewCap limita quantos tópicos entram na consulta de offsets em lote da
	// visão geral (Partições + ~Mensagens, "TopicsOverview") — sem teto, um broker com centenas de
	// tópicos geraria um comando `-Q` gigante (uma entrada -t por partição de cada tópico) e uma
	// varredura pesada no lado do broker. Mesmo espírito de segurança do dbRedisScanCap
	// (db_test_tool.go): corta em vez de tentar tudo de uma vez. Tópicos além do teto ainda
	// aparecem na lista simples (sem estatística), só não entram na consulta de offsets.
	kafkaTopicsOverviewCap = 200
)

const (
	kafkaSASLMechanismPlain       = "PLAIN"
	kafkaSASLMechanismScramSHA256 = "SCRAM-SHA-256"
	kafkaSASLMechanismScramSHA512 = "SCRAM-SHA-512"
	// kafkaSASLMechanismOAuthBearer — Azure AD (service principal) via OIDC client credentials.
	// Usado principalmente pra Event Hub quando a política de segurança exige AAD em vez de SAS
	// (connection string, já coberto pelo mecanismo PLAIN com usuário $ConnectionString).
	kafkaSASLMechanismOAuthBearer = "OAUTHBEARER"
)

var kafkaValidSASLMechanisms = map[string]bool{
	kafkaSASLMechanismPlain:       true,
	kafkaSASLMechanismScramSHA256: true,
	kafkaSASLMechanismScramSHA512: true,
	kafkaSASLMechanismOAuthBearer: true,
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

// kafkaOffsetLineWithTopicRegex é a mesma linha de kafkaOffsetLineRegex, mas capturando também o
// nome do tópico — necessário quando o `-Q` é batelado com partições de VÁRIOS tópicos na mesma
// invocação (ver buildKafkaOffsetQueryArgsMulti/TopicsOverview), já que o número da partição
// sozinho não identifica de qual tópico ela é.
var kafkaOffsetLineWithTopicRegex = regexp.MustCompile(`(\S+)\s+\[(\d+)\]\s+offset\s+(-?\d+)`)

// KafkaSASLConfig descreve autenticação SASL opcional pro teste. Username/Password OU SecretRef
// (mutuamente exclusivos — SecretRef tem prioridade se ambos vierem preenchidos por engano).
// Os campos OAuth* só se aplicam quando Mechanism == OAUTHBEARER — nesse caso Username/Password/
// SecretRef ficam vazios/ignorados (ver buildKcatAuthFlags).
type KafkaSASLConfig struct {
	Mechanism     string          `json:"mechanism"` // PLAIN | SCRAM-SHA-256 | SCRAM-SHA-512 | OAUTHBEARER
	UseTLS        bool            `json:"use_tls"`
	SkipTLSVerify bool            `json:"skip_tls_verify"`
	Username      string          `json:"username,omitempty"`
	Password      string          `json:"password,omitempty"`
	SecretRef     *KafkaSecretRef `json:"secret_ref,omitempty"`

	// OAuthClientID/OAuthClientSecret: credenciais do App Registration (service principal) no
	// Azure AD. OAuthTokenEndpointURL: endpoint de token do tenant, ex:
	// https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token. OAuthScope: escopo do
	// recurso, ex: https://<namespace>.servicebus.windows.net/.default (opcional — alguns
	// tenants/providers não exigem).
	OAuthClientID         string `json:"oauth_client_id,omitempty"`
	OAuthClientSecret     string `json:"oauth_client_secret,omitempty"`
	OAuthTokenEndpointURL string `json:"oauth_token_endpoint_url,omitempty"`
	OAuthScope            string `json:"oauth_scope,omitempty"`
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
	// ExecutionMode decide ONDE o teste roda: "pod" (default) — ephemeral container anexado a um
	// pod real do Deployment, reflete NetworkPolicy/Istio — ou "local" — subprocesso Docker direto
	// no host do servidor da aplicação, útil quando o broker é alcançável diretamente da rede do
	// servidor (VPN, endpoint público) e não faz sentido refletir a identidade de rede de um pod
	// específico. Mesmo campo/semântica de DBTestRequest.ExecutionMode (db_test_tool.go).
	ExecutionMode string `json:"execution_mode"` // pod | local
	Cluster       string `json:"cluster"`
	Namespace     string `json:"namespace"`
	// Deployment identifica de QUAL workload o teste deve partir — o handler resolve um pod
	// Running desse Deployment e anexa um ephemeral container nele, pra refletir a identidade de
	// rede real (NetworkPolicy/Istio) daquele workload específico, não de um pod avulso genérico.
	// Só usado/obrigatório quando ExecutionMode="pod".
	Deployment string `json:"deployment"`
	// PodName/ContainerName são opcionais — quando vazios, comportamento padrão de sempre
	// (primeiro pod Running do Deployment, primeiro container dele). Preenchidos quando o usuário
	// escolhe explicitamente um pod/container específico (Deployment com múltiplas réplicas, ver
	// resolvePodForDeployment).
	PodName        string           `json:"pod_name,omitempty"`
	ContainerName  string           `json:"container_name,omitempty"`
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

// listRunningPodsForDeployment lista os pods em estado Running que pertencem ao Deployment
// informado, via o próprio label selector do Deployment (não heurística de nome/label "app" — o
// mesmo tipo de atalho frágil já usado no frontend pra outra finalidade, DeploymentsTab.tsx).
// Compartilhada entre resolvePodForDeployment (escolhe um) e o endpoint de listagem pro seletor
// de pod/container do frontend (mostra todos).
func listRunningPodsForDeployment(ctx context.Context, clientset kubernetes.Interface, namespace, deploymentName string) ([]corev1.Pod, error) {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("falha ao buscar deployment %s: %w", deploymentName, err)
	}

	sel, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("selector inválido no deployment %s: %w", deploymentName, err)
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return nil, fmt.Errorf("falha ao listar pods do deployment %s: %w", deploymentName, err)
	}

	running := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && len(pod.Spec.Containers) > 0 {
			running = append(running, pod)
		}
	}
	return running, nil
}

// resolvePodForDeployment escolhe o pod/container-alvo do teste. Com requestedPod vazio, mantém o
// comportamento padrão de sempre (primeiro pod Running do Deployment, primeiro container dele —
// retrocompatível com quem não usa o seletor de pod/container do frontend). Com requestedPod
// preenchido, valida que aquele pod específico ainda existe, está Running e pertence ao
// Deployment (via o mesmo label selector), e usa requestedContainer (ou o primeiro container do
// pod, se vazio). O nome do container devolvido é usado como TargetContainerName do ephemeral
// container — não afeta o namespace de rede (compartilhado automaticamente por qualquer ephemeral
// container do pod) — só mantém paridade com o padrão já exigido pela Debug Container existente
// em podexec.go.
func resolvePodForDeployment(ctx context.Context, clientset kubernetes.Interface, namespace, deploymentName, requestedPod, requestedContainer string) (podName, containerName string, err error) {
	running, err := listRunningPodsForDeployment(ctx, clientset, namespace, deploymentName)
	if err != nil {
		return "", "", err
	}

	if requestedPod == "" {
		if len(running) == 0 {
			return "", "", fmt.Errorf("nenhum pod Running encontrado pra o deployment %s/%s", namespace, deploymentName)
		}
		return running[0].Name, running[0].Spec.Containers[0].Name, nil
	}

	for _, pod := range running {
		if pod.Name != requestedPod {
			continue
		}
		if requestedContainer == "" {
			return pod.Name, pod.Spec.Containers[0].Name, nil
		}
		for _, ct := range pod.Spec.Containers {
			if ct.Name == requestedContainer {
				return pod.Name, ct.Name, nil
			}
		}
		return "", "", fmt.Errorf("container %q não encontrado no pod %s", requestedContainer, requestedPod)
	}

	return "", "", fmt.Errorf("pod %q não encontrado, não está Running, ou não pertence mais ao deployment %s/%s — atualize a lista de pods e escolha outro", requestedPod, namespace, deploymentName)
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
			username, err = decodeSecretValueBase64(username)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", userKey, err)
			}
			password, err = decodeSecretValueBase64(password)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", passKey, err)
			}
		}
		return username, password, nil
	}
	return sasl.Username, sasl.Password, nil
}

// decodeSecretValueBase64 decodifica uma string em base64 — tenta StdEncoding primeiro (com
// padding, o formato mais comum) e cai pra RawStdEncoding (sem padding) se falhar, já que fontes
// externas (ex: AKV) nem sempre preservam o `=` de padding. Faz `TrimSpace` antes: base64 exportado
// via `echo valor | base64` (sem `-n`) ou copiado manualmente costuma vir com `\n` no final, o que
// quebra o decode mesmo o conteúdo sendo válido. Compartilhada entre Kafka e Teste de Banco de Dados
// (mesmo padrão de credencial via Secret K8s nos dois).
func decodeSecretValueBase64(v string) (string, error) {
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

// kafkaValidateOAuthBearerFields confere os campos obrigatórios pra OAUTHBEARER (client
// credentials OIDC) — client ID/secret e o endpoint de token do tenant Azure AD. Scope é
// opcional (nem todo provider/tenant exige). Retorna "" se válido.
func kafkaValidateOAuthBearerFields(sasl *KafkaSASLConfig) string {
	if strings.TrimSpace(sasl.OAuthClientID) == "" ||
		strings.TrimSpace(sasl.OAuthClientSecret) == "" ||
		strings.TrimSpace(sasl.OAuthTokenEndpointURL) == "" {
		return "oauth_client_id, oauth_client_secret e oauth_token_endpoint_url são obrigatórios quando mechanism é OAUTHBEARER"
	}
	return ""
}

// buildKcatAuthFlags monta os `-X key=value` do kcat a partir da config SASL/TLS resolvida.
// Sem `sasl`, ainda cobre o caso "só TLS, sem autenticação" (security.protocol=SSL). Pra
// OAUTHBEARER, username/password são ignorados (autenticação via client credentials OIDC, ver
// KafkaSASLConfig.OAuth*) — TLS é sempre exigido (Event Hub via Azure AD nunca aceita texto puro).
func buildKcatAuthFlags(sasl *KafkaSASLConfig, username, password string) []string {
	if sasl == nil {
		return nil
	}
	isOAuthBearer := sasl.Mechanism == kafkaSASLMechanismOAuthBearer
	var flags []string
	hasCreds := username != "" || password != "" || isOAuthBearer
	switch {
	case hasCreds && sasl.UseTLS:
		flags = append(flags, "-X", "security.protocol=SASL_SSL")
	case hasCreds:
		flags = append(flags, "-X", "security.protocol=SASL_PLAINTEXT")
	case sasl.UseTLS:
		flags = append(flags, "-X", "security.protocol=SSL")
	}
	switch {
	case isOAuthBearer:
		flags = append(flags, "-X", "sasl.mechanisms="+kafkaSASLMechanismOAuthBearer)
		flags = append(flags, "-X", "sasl.oauthbearer.method=oidc")
		flags = append(flags, "-X", "sasl.oauthbearer.client.id="+sasl.OAuthClientID)
		flags = append(flags, "-X", "sasl.oauthbearer.client.secret="+sasl.OAuthClientSecret)
		flags = append(flags, "-X", "sasl.oauthbearer.token.endpoint.url="+sasl.OAuthTokenEndpointURL)
		if sasl.OAuthScope != "" {
			flags = append(flags, "-X", "sasl.oauthbearer.scope="+sasl.OAuthScope)
		}
	case hasCreds:
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

// extractStderr extrai o texto de stderr de um erro devolvido por execCmdInPod/execLocalDocker
// (formato "stream: %v (stderr: %s)" / "exec: %v (stderr: %s)") — usado só como fallback quando o
// stdout do próprio comando (que já vem com "2>&1", ver buildConnectivity/buildBrowse em
// db_test_tool.go) veio vazio, ou seja, quando o comando nunca chegou a rodar de fato.
//
// Nesse caso o stderr embutido no erro TAMBÉM costuma vir vazio (ex: "docker" não instalado no
// servidor — cmd.Run() falha em exec.LookPath antes de qualquer processo existir, sem stdout nem
// stderr) — o texto útil está no "%v" antes de "(stderr: ", não dentro dos parênteses. Devolver só
// o conteúdo dos parênteses nesse caso jogava fora a única informação disponível, resultando em
// "" (frontend mostra "(sem saída)"). Aqui, se o conteúdo extraído vier vazio, cai pra mensagem
// completa do erro.
func extractStderr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	idx := strings.Index(msg, "(stderr: ")
	if idx == -1 {
		return msg
	}
	rest := strings.TrimSuffix(msg[idx+len("(stderr: "):], ")")
	if strings.TrimSpace(rest) == "" {
		return msg
	}
	return rest
}

// kafkaExecFunc abstrai ONDE o script do kcat roda — dentro de um ephemeral container
// (execCmdInPod, modo "pod") ou dentro de um container Docker local no host do servidor
// (execLocalDocker, modo "local"). Mesmo padrão de dbExecFunc em db_test_tool.go: as stages não
// sabem qual dos dois é, só chamam a função — evita duplicar a lógica de cada estágio entre os
// dois modos de execução.
type kafkaExecFunc func(ctx context.Context, script string) (string, error)

// runKafkaConnectivityStage roda `kcat -L` (metadata) — cobre TCP+DNS+protocolo Kafka+SASL num
// único exec, classificando o resultado por padrões conhecidos de erro do rdkafka.
func runKafkaConnectivityStage(ctx context.Context, run kafkaExecFunc, broker string, authFlags []string, saslConfigured bool, timeoutMs int) KafkaStageResult {
	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cmd := buildKcatCommand(broker, authFlags, "-L")
	script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))

	output, execErr := run(ctx, script)
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
func runKafkaProduceConsumeStage(ctx context.Context, run kafkaExecFunc, broker, topic string, authFlags []string, timeoutMs int) KafkaProduceConsumeResult {
	marker := "k8s-hpa-manager-test-" + uuid.New().String()
	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	start := time.Now()

	produceCmd := buildKcatCommand(broker, authFlags, "-P", "-t", topic)
	produceScript := wrapKafkaScript(fmt.Sprintf("printf '%%s' %s | timeout %ds %s 2>&1", quoteShellArg(marker), timeoutSec, produceCmd))
	produceRaw, execErr := run(ctx, produceScript)
	if execErr != nil {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Falha ao executar o produce no pod", RawOutput: extractStderr(execErr)}
	}
	produceOut, produceExit, ok := splitKafkaExitMarker(produceRaw)
	if !ok || produceExit != 0 {
		return KafkaProduceConsumeResult{Status: "produce_failed", Message: "Falha ao produzir mensagem de teste", RawOutput: produceOut}
	}

	consumeCmd := buildKcatCommand(broker, authFlags, "-C", "-t", topic, "-o", "beginning", "-c", fmt.Sprintf("%d", kafkaTestConsumeMaxMessages), "-e")
	consumeScript := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, consumeCmd))
	consumeRaw, execErr := run(ctx, consumeScript)
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
func runKafkaViewTopicStage(ctx context.Context, run kafkaExecFunc, broker, topic string, maxMessages int, authFlags []string, timeoutMs int) KafkaTopicViewResult {
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

	output, execErr := run(ctx, script)
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

// kafkaTopicPartitions é um par tópico+contagem de partições, na ORDEM em que deve aparecer no
// comando `-Q` batelado (ver buildKafkaOffsetQueryArgsMulti) — slice em vez de map pra manter
// determinismo (útil pra depurar a saída bruta e pra testes).
type kafkaTopicPartitions struct {
	Topic      string
	Partitions int
}

// buildKafkaOffsetQueryArgsMulti é a versão em lote de buildKafkaOffsetQueryArgs: monta um único
// `-Q` cobrindo TODAS as partições de TODOS os tópicos informados, com o MESMO timestamp especial
// (-1 ou -2) — usado pela visão geral de tópicos (TopicsOverview) pra evitar 2 execs por tópico
// (o que ficaria caro com muitos tópicos). Continua respeitando a mesma regra de nunca misturar
// timestamps -1/-2 na mesma chamada (ver comentário de runKafkaOffsetCountStage sobre o bug de
// dedup do kcat 1.7.1) — aqui só varia o tópico/partição, o timestamp é fixo pra chamada inteira.
func buildKafkaOffsetQueryArgsMulti(topics []kafkaTopicPartitions, timestamp int) []string {
	args := []string{"-Q"}
	for _, t := range topics {
		for p := 0; p < t.Partitions; p++ {
			args = append(args, "-t", fmt.Sprintf("%s:%d:%d", t.Topic, p, timestamp))
		}
	}
	return args
}

// parseKafkaOffsetLinesWithTopic é a versão multi-tópico de parseKafkaOffsetLines — chave
// "topico\x00partição" evita colisão entre partições de mesmo número em tópicos diferentes (ex:
// partição 0 existe em praticamente todo tópico).
func parseKafkaOffsetLinesWithTopic(raw string) map[string]int64 {
	result := make(map[string]int64)
	for _, m := range kafkaOffsetLineWithTopicRegex.FindAllStringSubmatch(raw, -1) {
		p, _ := strconv.Atoi(m[2])
		offset, _ := strconv.ParseInt(m[3], 10, 64)
		result[fmt.Sprintf("%s\x00%d", m[1], p)] = offset
	}
	return result
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
func runKafkaOffsetCountStage(ctx context.Context, run kafkaExecFunc, broker, topic string, authFlags []string, timeoutMs int) KafkaOffsetCountResult {
	timeoutSec := (timeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	runStep := func(extraArgs ...string) (raw string, exitCode int, execErr error) {
		cmd := buildKcatCommand(broker, authFlags, extraArgs...)
		script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))
		output, err := run(ctx, script)
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
	go startKafkaTestContainerReaper()
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

	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "pod"
	}
	if req.ExecutionMode != "pod" && req.ExecutionMode != "local" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_EXECUTION_MODE", "execution_mode deve ser pod ou local"))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Broker = strings.TrimSpace(req.Broker)
	if req.Broker == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "broker é obrigatório"))
		return
	}
	if req.ExecutionMode == "pod" && (req.Cluster == "" || req.Namespace == "" || req.Deployment == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios quando execution_mode é pod"))
		return
	}

	if req.SASL != nil {
		req.SASL.Mechanism = strings.ToUpper(strings.TrimSpace(req.SASL.Mechanism))
		if req.SASL.Mechanism != "" && !kafkaValidSASLMechanisms[req.SASL.Mechanism] {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_MECHANISM", "mechanism deve ser PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 ou OAUTHBEARER"))
			return
		}
		if req.SASL.Mechanism == kafkaSASLMechanismOAuthBearer {
			if msg := kafkaValidateOAuthBearerFields(req.SASL); msg != "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_OAUTH_FIELDS", msg))
				return
			}
		}
		if req.SASL.SecretRef != nil {
			req.SASL.SecretRef.Namespace = strings.TrimSpace(req.SASL.SecretRef.Namespace)
			req.SASL.SecretRef.Name = strings.TrimSpace(req.SASL.SecretRef.Name)
			if req.SASL.SecretRef.Namespace == "" || req.SASL.SecretRef.Name == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_SECRET_REF", "secret_ref precisa de namespace e name"))
				return
			}
			// No modo "local" cluster/namespace/deployment não são necessários pro teste em si
			// (roda direto no host do servidor), MAS ler o Secret da credencial SASL ainda precisa
			// de um cluster pra falar com a API do K8s — mesmo raciocínio de usesK8sRef em
			// db_test_tool.go.
			if req.ExecutionMode == "local" && req.Cluster == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_CLUSTER", "cluster é obrigatório quando sasl.secret_ref é usado, mesmo em execution_mode local"))
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
	// ExecutionMode — mesmo campo de RunKafkaTestRequest.ExecutionMode (pod|local, default pod).
	ExecutionMode string `json:"execution_mode"`
	Cluster       string `json:"cluster"`
	Namespace     string `json:"namespace"`
	Deployment    string `json:"deployment"`
	// PodName/ContainerName — mesmo campo/semântica de RunKafkaTestRequest, ver comentário lá.
	PodName       string           `json:"pod_name,omitempty"`
	ContainerName string           `json:"container_name,omitempty"`
	Broker        string           `json:"broker"`
	SASL          *KafkaSASLConfig `json:"sasl,omitempty"`
	TimeoutMs     int              `json:"timeout_ms"`
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

	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "pod"
	}
	if req.ExecutionMode != "pod" && req.ExecutionMode != "local" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_EXECUTION_MODE", "execution_mode deve ser pod ou local"))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Broker = strings.TrimSpace(req.Broker)
	if req.Broker == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "broker é obrigatório"))
		return
	}
	if req.ExecutionMode == "pod" && (req.Cluster == "" || req.Namespace == "" || req.Deployment == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios quando execution_mode é pod"))
		return
	}
	if req.SASL != nil {
		req.SASL.Mechanism = strings.ToUpper(strings.TrimSpace(req.SASL.Mechanism))
		if req.SASL.Mechanism != "" && !kafkaValidSASLMechanisms[req.SASL.Mechanism] {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_MECHANISM", "mechanism deve ser PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 ou OAUTHBEARER"))
			return
		}
		if req.SASL.Mechanism == kafkaSASLMechanismOAuthBearer {
			if msg := kafkaValidateOAuthBearerFields(req.SASL); msg != "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_OAUTH_FIELDS", msg))
				return
			}
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

	var clientset kubernetes.Interface
	if req.Cluster != "" {
		var err error
		clientset, err = h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
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

	var run kafkaExecFunc
	if req.ExecutionMode == "local" {
		image := kafkaTestPodImage
		localContainerName := "k8s-hpa-kafkatest-" + uuid.New().String()
		run = func(ctx context.Context, script string) (string, error) {
			return execLocalDocker(ctx, image, localContainerName, kafkaTestDockerLabel, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
		podName, targetContainer, err := resolvePodForDeployment(ctx, clientset, req.Namespace, req.Deployment, req.PodName, req.ContainerName)
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
		ns, pod, container := req.Namespace, podName, containerName
		run = func(ctx context.Context, script string) (string, error) {
			return execCmdInPod(ctx, clientset, restConfig, ns, pod, container, []string{"sh", "-c", script})
		}
	}

	timeoutSec := (req.TimeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cmd := buildKcatCommand(req.Broker, authFlags, "-L")
	script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))
	output, execErr := run(ctx, script)
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

// KafkaTopicOverviewEntry é uma linha da visão geral de tópicos — mesmo espírito do "All Stats" do
// MongoDB Compass usado no Teste de Banco de Dados, mas sem coluna de tamanho em disco: o kcat não
// expõe isso (exigiria os scripts nativos do Kafka — kafka-log-dirs.sh — ou JMX, fora de escopo).
// Só Partições (metadata, grátis) e ~Mensagens (latest-earliest por partição, somado).
type KafkaTopicOverviewEntry struct {
	Topic      string `json:"topic"`
	Partitions int    `json:"partitions"`
	// MessageCount é -1 (nunca omitido, ver json tag) quando o tópico ficou de fora da consulta de
	// offsets por causa do kafkaTopicsOverviewCap — o frontend distingue de um tópico realmente
	// vazio (0), que é um valor válido.
	MessageCount int64 `json:"message_count"`
	// DiskBytes é -1 quando não calculado — só é preenchido no modo "local" (via kafka-log-dirs
	// numa imagem completa do Kafka, ver kafka_test_logdirs.go); no modo "pod" fica sempre -1
	// (o kcat não expõe tamanho em disco). Mesma convenção de sentinel do MessageCount.
	DiskBytes int64 `json:"disk_bytes"`
}

// TopicsOverviewResponse é o resultado da visão geral de tópicos.
type TopicsOverviewResponse struct {
	Topics []KafkaTopicOverviewEntry `json:"topics"`
	// Truncated é true quando havia mais tópicos que kafkaTopicsOverviewCap — só os primeiros (em
	// ordem alfabética) entram na consulta de offsets em lote; os demais aparecem sem contagem.
	Truncated bool `json:"truncated,omitempty"`
	// DiskUsageWarning é preenchido só no modo "local" quando a chamada best-effort ao
	// kafka-log-dirs falha (ver fetchKafkaTopicDiskUsage) — a Visão geral continua útil sem a
	// coluna de disco, então isso não vira um erro fatal da requisição inteira.
	DiskUsageWarning string `json:"disk_usage_warning,omitempty"`
	RawOutput        string `json:"raw_output,omitempty"`
}

// TopicsOverview lista todos os tópicos do broker com Partições + ~Mensagens (soma de latest-
// earliest por partição) — visão equivalente ao "All Stats" do MongoDB Compass, adaptada ao que o
// kcat consegue expor. Mesmo setup de pod/ephemeral container de ListTopics; 3 execs no total: (1)
// `-L` pra descobrir tópicos+partições, (2) `-Q` em lote pra TODOS os tópicos com timestamp -1
// (fim), (3) idem com timestamp -2 (início) — nunca -1 e -2 juntos na mesma chamada (bug de dedup
// do kcat 1.7.1, ver runKafkaOffsetCountStage). Síncrono (sem SSE), mesmo raciocínio de custo de
// ListTopics: rápido mesmo contando o tempo de subir o ephemeral container.
// POST /api/v1/kafka-test/topics/overview
func (h *KafkaTestHandler) TopicsOverview(c *gin.Context) {
	var req ListTopicsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "pod"
	}
	if req.ExecutionMode != "pod" && req.ExecutionMode != "local" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_EXECUTION_MODE", "execution_mode deve ser pod ou local"))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Broker = strings.TrimSpace(req.Broker)
	if req.Broker == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "broker é obrigatório"))
		return
	}
	if req.ExecutionMode == "pod" && (req.Cluster == "" || req.Namespace == "" || req.Deployment == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios quando execution_mode é pod"))
		return
	}
	if req.SASL != nil {
		req.SASL.Mechanism = strings.ToUpper(strings.TrimSpace(req.SASL.Mechanism))
		if req.SASL.Mechanism != "" && !kafkaValidSASLMechanisms[req.SASL.Mechanism] {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_MECHANISM", "mechanism deve ser PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 ou OAUTHBEARER"))
			return
		}
		if req.SASL.Mechanism == kafkaSASLMechanismOAuthBearer {
			if msg := kafkaValidateOAuthBearerFields(req.SASL); msg != "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_OAUTH_FIELDS", msg))
				return
			}
		}
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = kafkaTestDefaultTimeoutMs
	}
	if req.TimeoutMs > kafkaTestMaxTimeoutMs {
		req.TimeoutMs = kafkaTestMaxTimeoutMs
	}

	// Até 3 execs no pior caso (metadata + 2 consultas de offset) — timeout total cobre todos.
	ctx, cancel := context.WithTimeout(c.Request.Context(),
		kafkaTestEphemeralReadyTimeout+3*time.Duration(req.TimeoutMs)*time.Millisecond+5*time.Second)
	defer cancel()

	var clientset kubernetes.Interface
	if req.Cluster != "" {
		var err error
		clientset, err = h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
	}

	var authFlags []string
	var saslUsername, saslPassword string
	if req.SASL != nil {
		var credErr error
		saslUsername, saslPassword, credErr = resolveKafkaCredentials(ctx, clientset, req.SASL)
		if credErr != nil {
			c.JSON(http.StatusBadRequest, errorResponse("CREDENTIALS_ERROR", credErr.Error()))
			return
		}
		authFlags = buildKcatAuthFlags(req.SASL, saslUsername, saslPassword)
	}

	var run kafkaExecFunc
	if req.ExecutionMode == "local" {
		image := kafkaTestPodImage
		localContainerName := "k8s-hpa-kafkatest-" + uuid.New().String()
		run = func(ctx context.Context, script string) (string, error) {
			return execLocalDocker(ctx, image, localContainerName, kafkaTestDockerLabel, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
			return
		}
		podName, targetContainer, err := resolvePodForDeployment(ctx, clientset, req.Namespace, req.Deployment, req.PodName, req.ContainerName)
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
		ns, pod, container := req.Namespace, podName, containerName
		run = func(ctx context.Context, script string) (string, error) {
			return execCmdInPod(ctx, clientset, restConfig, ns, pod, container, []string{"sh", "-c", script})
		}
	}

	timeoutSec := (req.TimeoutMs + 999) / 1000
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	runStep := func(extraArgs ...string) (raw string, exitCode int, execErr error) {
		cmd := buildKcatCommand(req.Broker, authFlags, extraArgs...)
		script := wrapKafkaScript(fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd))
		output, err := run(ctx, script)
		if err != nil {
			return extractStderr(err), -1, err
		}
		text, code, ok := splitKafkaExitMarker(output)
		if !ok {
			return text, -1, nil
		}
		return text, code, nil
	}

	metaRaw, metaExit, execErr := runStep("-L")
	if execErr != nil || metaExit != 0 {
		c.JSON(http.StatusBadGateway, errorResponse("EXEC_ERROR", "Falha ao consultar metadados do broker: "+metaRaw))
		return
	}

	var allTopics []kafkaTopicPartitions
	for _, m := range kafkaTopicNameRegex.FindAllStringSubmatch(metaRaw, -1) {
		partitions, _ := strconv.Atoi(m[2])
		if partitions == 0 {
			continue
		}
		allTopics = append(allTopics, kafkaTopicPartitions{Topic: m[1], Partitions: partitions})
	}
	sort.Slice(allTopics, func(i, j int) bool { return allTopics[i].Topic < allTopics[j].Topic })

	if len(allTopics) == 0 {
		c.JSON(http.StatusOK, TopicsOverviewResponse{Topics: []KafkaTopicOverviewEntry{}, RawOutput: metaRaw})
		return
	}

	truncated := false
	queryTopics := allTopics
	if len(queryTopics) > kafkaTopicsOverviewCap {
		queryTopics = queryTopics[:kafkaTopicsOverviewCap]
		truncated = true
	}

	latestRaw, latestExit, execErr := runStep(buildKafkaOffsetQueryArgsMulti(queryTopics, -1)...)
	if execErr != nil || latestExit != 0 {
		c.JSON(http.StatusBadGateway, errorResponse("EXEC_ERROR", "Falha ao consultar os offsets mais recentes: "+latestRaw))
		return
	}
	earliestRaw, earliestExit, execErr := runStep(buildKafkaOffsetQueryArgsMulti(queryTopics, -2)...)
	if execErr != nil || earliestExit != 0 {
		c.JSON(http.StatusBadGateway, errorResponse("EXEC_ERROR", "Falha ao consultar os offsets mais antigos: "+earliestRaw))
		return
	}

	latestMap := parseKafkaOffsetLinesWithTopic(latestRaw)
	earliestMap := parseKafkaOffsetLinesWithTopic(earliestRaw)

	entries := make([]KafkaTopicOverviewEntry, 0, len(allTopics))
	queried := make(map[string]bool, len(queryTopics))
	for _, t := range queryTopics {
		queried[t.Topic] = true
		var total int64
		for p := 0; p < t.Partitions; p++ {
			key := fmt.Sprintf("%s\x00%d", t.Topic, p)
			total += latestMap[key] - earliestMap[key]
		}
		entries = append(entries, KafkaTopicOverviewEntry{Topic: t.Topic, Partitions: t.Partitions, MessageCount: total, DiskBytes: -1})
	}
	for _, t := range allTopics {
		if queried[t.Topic] {
			continue
		}
		entries = append(entries, KafkaTopicOverviewEntry{Topic: t.Topic, Partitions: t.Partitions, MessageCount: -1, DiskBytes: -1})
	}

	rawOutput := metaRaw + "\n---\n" + latestRaw + "\n---\n" + earliestRaw

	// Tamanho real em disco (kafka-log-dirs) — só no modo "local" (ver kafka_test_logdirs.go pro
	// porquê da imagem completa não ser usada no modo "pod"). Best-effort: se falhar, a coluna
	// fica vazia (DiskBytes=-1) em vez de derrubar a Visão geral inteira, que já tem dado útil sem
	// isso (Partições + ~Mensagens).
	var diskUsageWarning string
	if req.ExecutionMode == "local" {
		topicNames := make([]string, len(queryTopics))
		for i, t := range queryTopics {
			topicNames[i] = t.Topic
		}
		diskByTopic, diskErr := fetchKafkaTopicDiskUsage(ctx, req.Broker, req.SASL, saslUsername, saslPassword, topicNames)
		if diskErr != nil {
			diskUsageWarning = "Tamanho em disco indisponível: " + diskErr.Error()
		} else {
			for i := range entries {
				if size, ok := diskByTopic[entries[i].Topic]; ok {
					entries[i].DiskBytes = size
				}
			}
		}
	}

	c.JSON(http.StatusOK, TopicsOverviewResponse{
		Topics:           entries,
		Truncated:        truncated,
		DiskUsageWarning: diskUsageWarning,
		RawOutput:        rawOutput,
	})
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

	// clientset só é resolvido quando necessário — modo "local" sem SASL via Secret não precisa
	// tocar o cluster K8s em nenhum momento (mesmo raciocínio de db_test_tool.go runTest).
	var clientset kubernetes.Interface
	if req.Cluster != "" {
		var err error
		clientset, err = h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			fail("falha ao conectar no cluster", err)
			return
		}
	}

	var username, password string
	authFlags := []string(nil)
	if req.SASL != nil {
		var err error
		username, password, err = resolveKafkaCredentials(ctx, clientset, req.SASL)
		if err != nil {
			fail("falha ao resolver credenciais", err)
			return
		}
		authFlags = buildKcatAuthFlags(req.SASL, username, password)
	}

	var run kafkaExecFunc
	var podName, containerName string

	if req.ExecutionMode == "local" {
		send("local_exec", "in_progress", fmt.Sprintf("Executando localmente via Docker (%s)...", kafkaTestPodImage), 0.3)
		image := kafkaTestPodImage
		localContainerName := "k8s-hpa-kafkatest-" + sessionID
		run = func(ctx context.Context, script string) (string, error) {
			return execLocalDocker(ctx, image, localContainerName, kafkaTestDockerLabel, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			fail("falha ao obter configuração do cluster", err)
			return
		}

		send("resolve_deployment", "in_progress", fmt.Sprintf("Localizando pod Running do deployment %q...", req.Deployment), 0.15)
		resolvedPod, targetContainer, err := resolvePodForDeployment(ctx, clientset, req.Namespace, req.Deployment, req.PodName, req.ContainerName)
		if err != nil {
			fail("falha ao localizar pod do deployment", err)
			return
		}
		podName = resolvedPod

		send("ephemeral_container", "in_progress", fmt.Sprintf("Anexando container de teste no pod %s...", podName), 0.3)
		containerName, err = getOrCreateKafkaEphemeralContainer(ctx, clientset, req.Namespace, podName, targetContainer)
		if err != nil {
			fail("falha ao anexar ephemeral container", err)
			return
		}
		if err := waitKafkaEphemeralContainerRunning(ctx, clientset, req.Namespace, podName, containerName, kafkaTestEphemeralReadyTimeout); err != nil {
			fail("ephemeral container não ficou pronto", err)
			return
		}

		ns, pod, container := req.Namespace, podName, containerName
		run = func(ctx context.Context, script string) (string, error) {
			return execCmdInPod(ctx, clientset, restConfig, ns, pod, container, []string{"sh", "-c", script})
		}
	}

	send("connectivity", "in_progress", "Testando conectividade e protocolo Kafka...", 0.5)
	connectivity := runKafkaConnectivityStage(ctx, run, req.Broker, authFlags, req.SASL != nil, req.TimeoutMs)

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
			result.ProduceConsume = runKafkaProduceConsumeStage(ctx, run, req.Broker, req.Topic, authFlags, req.TimeoutMs)
		}
	}

	if req.CountOffsets {
		if connectivity.Status != kafkaStageOK {
			result.OffsetCount = KafkaOffsetCountResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar contar offsets"}
		} else {
			send("count_offsets", "in_progress", fmt.Sprintf("Contando offsets do tópico %q...", req.Topic), 0.78)
			result.OffsetCount = runKafkaOffsetCountStage(ctx, run, req.Broker, req.Topic, authFlags, req.TimeoutMs)
		}
	}

	if req.ViewTopic {
		if connectivity.Status != kafkaStageOK {
			result.ViewTopic = KafkaTopicViewResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar ler o tópico"}
		} else {
			send("view_topic", "in_progress", fmt.Sprintf("Lendo mensagens existentes do tópico %q...", req.Topic), 0.9)
			result.ViewTopic = runKafkaViewTopicStage(ctx, run, req.Broker, req.Topic, req.ViewMaxMessages, authFlags, req.TimeoutMs)
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
