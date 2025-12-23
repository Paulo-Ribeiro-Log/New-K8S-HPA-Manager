package sanitizer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// SanitizeKubernetesPod sanitiza dados sensíveis de um Pod
func (s *Sanitizer) SanitizeKubernetesPod(pod *corev1.Pod) (*corev1.Pod, error) {
	// Deep copy para não modificar original
	podCopy := pod.DeepCopy()

	// Sanitizar variáveis de ambiente de todos os containers
	for i := range podCopy.Spec.Containers {
		s.sanitizeContainerEnvVars(&podCopy.Spec.Containers[i])
	}

	// Sanitizar init containers
	for i := range podCopy.Spec.InitContainers {
		s.sanitizeContainerEnvVars(&podCopy.Spec.InitContainers[i])
	}

	// Sanitizar annotations
	podCopy.Annotations = s.sanitizeMap(podCopy.Annotations)

	return podCopy, nil
}

// sanitizeContainerEnvVars sanitiza variáveis de ambiente de um container
func (s *Sanitizer) sanitizeContainerEnvVars(container *corev1.Container) {
	for i := range container.Env {
		envVar := &container.Env[i]

		// Verifica se a chave da env var é sensível
		if IsSensitiveKey(envVar.Name, s.config.SensitiveKeys) {
			// APENAS sanitiza o VALUE (valor direto da env var)
			envVar.Value = SecretReplacement

			// NÃO sanitiza ValueFrom - preserva referências para análise
			// A AI precisa ver qual key está sendo referenciada
			// O valor real virá do ConfigMap/Secret, não do manifesto
		}
	}
}

// SanitizeKubernetesSecret sanitiza dados de um Secret
// IMPORTANTE: Mantém as CHAVES, mas remove os VALORES
func (s *Sanitizer) SanitizeKubernetesSecret(secret *corev1.Secret) (*corev1.Secret, error) {
	secretCopy := secret.DeepCopy()

	// Sanitiza todos os valores (mas preserva chaves para análise estrutural)
	for key := range secretCopy.Data {
		secretCopy.Data[key] = []byte(SecretReplacement)
	}

	for key := range secretCopy.StringData {
		secretCopy.StringData[key] = SecretReplacement
	}

	return secretCopy, nil
}

// SanitizeKubernetesConfigMap sanitiza dados de um ConfigMap
func (s *Sanitizer) SanitizeKubernetesConfigMap(cm *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	cmCopy := cm.DeepCopy()

	// Sanitiza apenas chaves sensíveis
	for key, value := range cmCopy.Data {
		// Connection strings: MASCARA senha mas preserva estrutura completa
		if isConnectionString(value) {
			cmCopy.Data[key] = MaskConnectionString(value) // Mascara senha, mantém sintaxe
			continue
		}

		if IsSensitiveKey(key, s.config.SensitiveKeys) {
			if len(value) > 20 {
				// Valores longos: mascara parcialmente (NÃO trunca)
				cmCopy.Data[key] = MaskPartial(value, 4)
			} else {
				cmCopy.Data[key] = SecretReplacement
			}
		} else {
			// Sanitiza o conteúdo (pode conter IPs, emails, etc)
			cmCopy.Data[key] = s.SanitizeText(value)
		}
	}

	for key := range cmCopy.BinaryData {
		if IsSensitiveKey(key, s.config.SensitiveKeys) {
			cmCopy.BinaryData[key] = []byte(SecretReplacement)
		}
	}

	return cmCopy, nil
}

// isConnectionString detecta se um valor é uma connection string
func isConnectionString(value string) bool {
	// Padrões comuns de connection strings
	patterns := []string{
		`^mongodb://`,
		`^postgres://`,
		`^mysql://`,
		`^redis://`,
		`^amqp://`,
		`^https?://[^@]+@`, // http/https com auth
		`:.*@.*:`,          // user:password@host:port
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, value); matched {
			return true
		}
	}

	return false
}

// sanitizeMap sanitiza valores de um map[string]string
func (s *Sanitizer) sanitizeMap(data map[string]string) map[string]string {
	if data == nil {
		return nil
	}

	sanitized := make(map[string]string, len(data))

	for key, value := range data {
		if IsSensitiveKey(key, s.config.SensitiveKeys) {
			sanitized[key] = SecretReplacement
		} else {
			sanitized[key] = s.SanitizeText(value)
		}
	}

	return sanitized
}

// SanitizeKubernetesEvent sanitiza dados de um Event
func (s *Sanitizer) SanitizeKubernetesEvent(event *corev1.Event) (*corev1.Event, error) {
	eventCopy := event.DeepCopy()

	// Sanitiza mensagem do evento (pode conter IPs, logs com secrets, etc)
	eventCopy.Message = s.SanitizeText(eventCopy.Message)

	return eventCopy, nil
}

// SanitizeJSON sanitiza um objeto JSON arbitrário
func (s *Sanitizer) SanitizeJSON(jsonData interface{}) (interface{}, error) {
	// Serializa para string
	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Sanitiza o texto JSON
	sanitizedText := s.SanitizeText(string(jsonBytes))

	// Deserializa de volta
	var sanitized interface{}
	if err := json.Unmarshal([]byte(sanitizedText), &sanitized); err != nil {
		// Se falhar ao deserializar, retorna string sanitizada
		return sanitizedText, nil
	}

	// Recursivamente sanitiza chaves sensíveis
	return s.sanitizeJSONRecursive(sanitized), nil
}

// sanitizeJSONRecursive percorre estrutura JSON e sanitiza valores sensíveis
func (s *Sanitizer) sanitizeJSONRecursive(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{}, len(v))
		for key, value := range v {
			if IsSensitiveKey(key, s.config.SensitiveKeys) {
				sanitized[key] = SecretReplacement
			} else {
				sanitized[key] = s.sanitizeJSONRecursive(value)
			}
		}
		return sanitized

	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, value := range v {
			sanitized[i] = s.sanitizeJSONRecursive(value)
		}
		return sanitized

	case string:
		return s.SanitizeText(v)

	default:
		return v
	}
}

// SanitizeYAML sanitiza conteúdo YAML (converte para JSON, sanitiza, reconverte)
func (s *Sanitizer) SanitizeYAML(yamlContent string) (string, error) {
	// Para YAML, aplicamos sanitização de texto diretamente
	// (evita problemas de conversão YAML ↔ JSON)
	return s.SanitizeText(yamlContent), nil
}

// SanitizeLogs sanitiza linhas de log
func (s *Sanitizer) SanitizeLogs(logs string) string {
	lines := strings.Split(logs, "\n")
	sanitizedLines := make([]string, len(lines))

	for i, line := range lines {
		sanitizedLines[i] = s.SanitizeText(line)
	}

	return strings.Join(sanitizedLines, "\n")
}
