package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// kafkaFullImage é usada SÓ no modo "local" (Docker no host do servidor) pra calcular o tamanho
// real em disco por tópico na Visão geral de tópicos — o `kcat` (kafkaTestPodImage) não expõe
// isso, só os scripts nativos do Kafka (kafka-log-dirs) conseguem. Não usada no modo "pod" de
// propósito: puxar uma imagem >1GB (JVM + Kafka completo) pro NODE do cluster a cada Ephemeral
// Container pesaria no armazenamento compartilhado do node; no modo local, a imagem fica só no
// Docker do próprio servidor da aplicação (cache normal de camadas), sem esse efeito colateral —
// ver decisão registrada no KAFKA-TEST-PLAN.md. Tag fixada de propósito (nunca `latest`).
//
// Assumpção não validada contra uma imagem real: `kafka-log-dirs` está disponível no PATH das
// imagens `confluentinc/cp-kafka` (prática usual do empacotamento Confluent, scripts sem sufixo
// `.sh` symlinkados em /usr/bin) — se a tag usada não tiver o binário, o erro aparece no raw_output
// da chamada (best-effort, não derruba o resto da Visão geral de tópicos).
const kafkaFullImage = "confluentinc/cp-kafka:7.7.1"

// kafkaLogDirsTimeoutSec é maior que o timeout normal do kcat (kafkaTestMaxTimeoutMs, teto 15s) —
// kafka-log-dirs sobe uma JVM inteira a cada chamada, o que sozinho já leva alguns segundos, antes
// mesmo de falar com o broker.
const kafkaLogDirsTimeoutSec = 30

// escapeJaasPropertyValue escapa `\` e `"` pra uso seguro como literal dentro de
// `sasl.jaas.config` (que segue a mesma sintaxe de string literal Java) — sem isso, um usuário ou
// senha contendo `"` quebraria o parsing do JAAS config e vazaria pro restante da linha.
func escapeJaasPropertyValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// buildKafkaClientPropertiesFile monta o conteúdo de um client.properties pros scripts nativos do
// Kafka (kafka-log-dirs, kafka-topics, etc) — equivalente ao buildKcatAuthFlags, mas pro
// vocabulário de propriedade dos clientes Java (`sasl.mechanism` singular, não
// `sasl.mechanisms` do librdkafka/kcat; JAAS config em vez de username/password soltos).
//
// LIMITAÇÃO CONHECIDA: não suporta OAUTHBEARER — com esse mecanismo username/password ficam
// vazios (autenticação é via client credentials OIDC, ver KafkaSASLConfig.OAuth*), então
// `hasCreds` fica false e a função pula o bloco de SASL inteiro, gerando um client.properties SEM
// autenticação nenhuma. Efeito: a coluna de tamanho em disco (kafka-log-dirs, só no modo `local`)
// falha silenciosamente pra brokers OAUTHBEARER — o resto do teste (kcat, buildKcatAuthFlags) não
// é afetado. Suporte a OAUTHBEARER aqui exigiria o login module Java
// (org.apache.kafka.common.security.oauthbearer.OAuthBearerLoginModule) com sintaxe de JAAS
// própria — não implementado ainda, ver KAFKA-TEST-PLAN.md.
func buildKafkaClientPropertiesFile(sasl *KafkaSASLConfig, username, password string) string {
	if sasl == nil {
		return ""
	}
	hasCreds := username != "" || password != ""
	var lines []string
	switch {
	case hasCreds && sasl.UseTLS:
		lines = append(lines, "security.protocol=SASL_SSL")
	case hasCreds:
		lines = append(lines, "security.protocol=SASL_PLAINTEXT")
	case sasl.UseTLS:
		lines = append(lines, "security.protocol=SSL")
	default:
		lines = append(lines, "security.protocol=PLAINTEXT")
	}
	if hasCreds {
		mechanism := sasl.Mechanism
		if mechanism == "" {
			mechanism = kafkaSASLMechanismPlain
		}
		lines = append(lines, "sasl.mechanism="+mechanism)
		loginModule := "org.apache.kafka.common.security.plain.PlainLoginModule"
		if mechanism == kafkaSASLMechanismScramSHA256 || mechanism == kafkaSASLMechanismScramSHA512 {
			loginModule = "org.apache.kafka.common.security.scram.ScramLoginModule"
		}
		lines = append(lines, fmt.Sprintf(
			`sasl.jaas.config=%s required username="%s" password="%s";`,
			loginModule, escapeJaasPropertyValue(username), escapeJaasPropertyValue(password),
		))
	}
	// Equivalente ao enable.ssl.certificate.verification=false do librdkafka não existe como
	// propriedade simples nos clientes Java — desabilitar só a checagem de hostname (não a CA
	// inteira) é o melhor-esforço possível sem uma SSL engine factory customizada. Se o broker usar
	// certificado self-signed, essa chamada pode falhar mesmo com SkipTLSVerify — best-effort, não
	// derruba o resto da Visão geral de tópicos (ver TopicsOverview).
	if sasl.UseTLS && sasl.SkipTLSVerify {
		lines = append(lines, "ssl.endpoint.identification.algorithm=")
	}
	return strings.Join(lines, "\n") + "\n"
}

// buildKafkaLogDirsScript monta o script `sh -c` completo rodado dentro do container da imagem
// completa do Kafka: grava o client.properties (via base64 — evita qualquer problema de quoting
// do conteúdo dentro do `sh -c`, já que o alfabeto base64 não tem caractere especial de shell) e
// roda `kafka-log-dirs --describe` pros tópicos informados.
func buildKafkaLogDirsScript(broker string, sasl *KafkaSASLConfig, username, password string, topics []string) string {
	props := buildKafkaClientPropertiesFile(sasl, username, password)
	propsB64 := base64.StdEncoding.EncodeToString([]byte(props))
	topicList := strings.Join(topics, ",")
	cmd := fmt.Sprintf(
		"kafka-log-dirs --bootstrap-server %s --describe --topic-list %s --command-config /tmp/kafka-client.properties",
		quoteShellArg(broker), quoteShellArg(topicList),
	)
	script := fmt.Sprintf(
		"echo %s | base64 -d > /tmp/kafka-client.properties && %s",
		quoteShellArg(propsB64), cmd,
	)
	return fmt.Sprintf("timeout %ds sh -c %s 2>&1", kafkaLogDirsTimeoutSec, quoteShellArg(script))
}

// kafkaLogDirsOutput espelha o JSON impresso por `kafka-log-dirs --describe` (formato documentado
// do Kafka, versão 1): uma linha de texto solta ("Querying brokers...") seguida da linha JSON —
// extractJSONObject (abaixo) isola só a parte JSON antes do Unmarshal.
type kafkaLogDirsOutput struct {
	Brokers []struct {
		LogDirs []struct {
			Error      *string `json:"error"`
			Partitions []struct {
				// Partition vem no formato "topico-N" (nome do tópico + índice da partição
				// separados por hífen) — como nomes de tópico podem conter hífen, usar
				// LastIndex pra separar (ver parseKafkaLogDirsOutput).
				Partition string `json:"partition"`
				Size      int64  `json:"size"`
			} `json:"partitions"`
		} `json:"logDirs"`
	} `json:"brokers"`
}

// extractJSONObject isola o primeiro objeto JSON balanceado (por chaves) dentro de raw — usado
// porque kafka-log-dirs imprime uma linha de texto solta antes do JSON de verdade.
func extractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start == -1 {
		return ""
	}
	depth := 0
	for i := start; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

// parseKafkaLogDirsOutput extrai bytes-em-disco por tópico a partir da saída de
// `kafka-log-dirs --describe`. Uma mesma partição aparece uma vez POR RÉPLICA (líder + followers,
// cada um em seu broker) — somar todas as ocorrências superestimaria o tamanho pelo fator de
// replicação. Em vez disso, pega o MAIOR valor visto por partição (aproximação do tamanho real dos
// dados, não do total ocupado somando todas as réplicas) e só então soma por tópico.
func parseKafkaLogDirsOutput(raw string) map[string]int64 {
	jsonPart := extractJSONObject(raw)
	if jsonPart == "" {
		return nil
	}
	var parsed kafkaLogDirsOutput
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		return nil
	}

	maxPerPartition := make(map[string]int64) // "topico\x00partição" -> maior size visto
	for _, broker := range parsed.Brokers {
		for _, logDir := range broker.LogDirs {
			if logDir.Error != nil {
				continue
			}
			for _, p := range logDir.Partitions {
				idx := strings.LastIndex(p.Partition, "-")
				if idx == -1 {
					continue
				}
				topic := p.Partition[:idx]
				key := topic + "\x00" + p.Partition[idx+1:]
				if existing, ok := maxPerPartition[key]; !ok || p.Size > existing {
					maxPerPartition[key] = p.Size
				}
			}
		}
	}

	byTopic := make(map[string]int64)
	for key, size := range maxPerPartition {
		topic := key[:strings.IndexByte(key, 0)]
		byTopic[topic] += size
	}
	return byTopic
}

// fetchKafkaTopicDiskUsage roda kafka-log-dirs num container Docker local (imagem completa do
// Kafka, só usada no modo "local" da Visão geral de tópicos) e devolve bytes-em-disco por tópico.
// Best-effort: erro aqui não derruba a Visão geral inteira (ver TopicsOverview), só deixa a coluna
// de disco vazia ("—" no frontend, disk_bytes=-1).
func fetchKafkaTopicDiskUsage(ctx context.Context, broker string, sasl *KafkaSASLConfig, username, password string, topics []string) (map[string]int64, error) {
	logDirsCtx, cancel := context.WithTimeout(ctx, (kafkaLogDirsTimeoutSec+5)*time.Second)
	defer cancel()

	script := buildKafkaLogDirsScript(broker, sasl, username, password, topics)
	containerName := "k8s-hpa-kafkatest-logdirs-" + fmt.Sprintf("%d", time.Now().UnixNano())
	raw, err := execLocalDocker(logDirsCtx, kafkaFullImage, containerName, kafkaTestDockerLabel, script)
	if err != nil {
		return nil, fmt.Errorf("falha ao rodar kafka-log-dirs: %w (saída: %s)", err, raw)
	}
	byTopic := parseKafkaLogDirsOutput(raw)
	if byTopic == nil {
		return nil, fmt.Errorf("não foi possível interpretar a saída do kafka-log-dirs: %s", raw)
	}
	return byTopic, nil
}
