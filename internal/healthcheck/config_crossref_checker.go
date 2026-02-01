package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ConfigCrossRefHealth representa a saúde de ConfigMaps/Secrets com cross-reference
type ConfigCrossRefHealth struct {
	Name         string       `json:"name"`
	Namespace    string       `json:"namespace"`
	ResourceType string       `json:"resource_type"` // "ConfigMap" ou "Secret"
	Status       HealthStatus `json:"status"`

	// Cross-reference
	IsOrphan       bool     `json:"is_orphan"`        // Não referenciado por nenhum pod/deployment
	ReferencedBy   []string `json:"referenced_by"`    // Lista de recursos que referenciam
	MissingRefs    []string `json:"missing_refs"`     // Referências que não existem
	UnusedKeys     []string `json:"unused_keys"`      // Chaves não utilizadas (opcional)

	// Metadados
	KeyCount    int       `json:"key_count"`
	CreatedAt   time.Time `json:"created_at"`
	LastUpdated time.Time `json:"last_updated,omitempty"`

	// Mensagem e Sugestões
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions"`
}

// MissingReference representa uma referência faltando
type MissingReference struct {
	ResourceType string `json:"resource_type"` // "ConfigMap" ou "Secret"
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	ReferencedBy string `json:"referenced_by"` // Pod/Deployment que referencia
	RefType      string `json:"ref_type"`      // "env", "envFrom", "volume"
}

// ConfigCrossRefChecker verifica cross-references de ConfigMaps e Secrets
type ConfigCrossRefChecker struct {
	// Namespaces de sistema a ignorar
	systemNamespaces map[string]bool
}

// NewConfigCrossRefChecker cria um novo ConfigCrossRefChecker
func NewConfigCrossRefChecker() *ConfigCrossRefChecker {
	return &ConfigCrossRefChecker{
		systemNamespaces: map[string]bool{
			"kube-system":     true,
			"kube-public":     true,
			"kube-node-lease": true,
			"istio-system":    true,
			"cert-manager":    true,
			"ingress-nginx":   true,
		},
	}
}

// CheckAll verifica ConfigMaps e Secrets nos namespaces especificados
func (c *ConfigCrossRefChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int) ([]ConfigCrossRefHealth, []MissingReference) {
	results := []ConfigCrossRefHealth{}
	missingRefs := []MissingReference{}

	for _, ns := range namespaces {
		// Pular namespaces de sistema
		if c.systemNamespaces[ns] {
			continue
		}

		// Listar ConfigMaps
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Falha ao listar ConfigMaps")
			continue
		}

		// Listar Secrets
		secrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Falha ao listar Secrets")
			continue
		}

		// Listar Pods para verificar referências
		pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Falha ao listar Pods")
			pods = &corev1.PodList{}
		}

		// Criar mapa de referências
		configMapRefs, secretRefs := c.buildReferenceMap(pods.Items)

		// Verificar ConfigMaps
		for _, cm := range configMaps.Items {
			// Ignorar ConfigMaps de sistema
			if c.isSystemResource(cm.Name) {
				continue
			}

			health := c.checkConfigMap(&cm, configMapRefs[cm.Name])
			results = append(results, health)
		}

		// Verificar Secrets
		for _, secret := range secrets.Items {
			// Ignorar Secrets de sistema
			if c.isSystemResource(secret.Name) || c.isSystemSecret(&secret) {
				continue
			}

			health := c.checkSecret(&secret, secretRefs[secret.Name])
			results = append(results, health)
		}

		// Verificar referências faltando
		missing := c.checkMissingReferences(pods.Items, configMaps.Items, secrets.Items, ns)
		missingRefs = append(missingRefs, missing...)
	}

	return results, missingRefs
}

// buildReferenceMap cria mapa de ConfigMaps/Secrets referenciados por pods
func (c *ConfigCrossRefChecker) buildReferenceMap(pods []corev1.Pod) (map[string][]string, map[string][]string) {
	configMapRefs := make(map[string][]string)
	secretRefs := make(map[string][]string)

	for _, pod := range pods {
		podRef := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

		// Verificar volumes
		for _, vol := range pod.Spec.Volumes {
			if vol.ConfigMap != nil {
				configMapRefs[vol.ConfigMap.Name] = append(configMapRefs[vol.ConfigMap.Name], podRef)
			}
			if vol.Secret != nil {
				secretRefs[vol.Secret.SecretName] = append(secretRefs[vol.Secret.SecretName], podRef)
			}
			if vol.Projected != nil {
				for _, source := range vol.Projected.Sources {
					if source.ConfigMap != nil {
						configMapRefs[source.ConfigMap.Name] = append(configMapRefs[source.ConfigMap.Name], podRef)
					}
					if source.Secret != nil {
						secretRefs[source.Secret.Name] = append(secretRefs[source.Secret.Name], podRef)
					}
				}
			}
		}

		// Verificar containers
		for _, container := range pod.Spec.Containers {
			// EnvFrom
			for _, envFrom := range container.EnvFrom {
				if envFrom.ConfigMapRef != nil {
					configMapRefs[envFrom.ConfigMapRef.Name] = append(configMapRefs[envFrom.ConfigMapRef.Name], podRef)
				}
				if envFrom.SecretRef != nil {
					secretRefs[envFrom.SecretRef.Name] = append(secretRefs[envFrom.SecretRef.Name], podRef)
				}
			}

			// Env com valueFrom
			for _, env := range container.Env {
				if env.ValueFrom != nil {
					if env.ValueFrom.ConfigMapKeyRef != nil {
						configMapRefs[env.ValueFrom.ConfigMapKeyRef.Name] = append(configMapRefs[env.ValueFrom.ConfigMapKeyRef.Name], podRef)
					}
					if env.ValueFrom.SecretKeyRef != nil {
						secretRefs[env.ValueFrom.SecretKeyRef.Name] = append(secretRefs[env.ValueFrom.SecretKeyRef.Name], podRef)
					}
				}
			}
		}

		// Verificar init containers também
		for _, container := range pod.Spec.InitContainers {
			for _, envFrom := range container.EnvFrom {
				if envFrom.ConfigMapRef != nil {
					configMapRefs[envFrom.ConfigMapRef.Name] = append(configMapRefs[envFrom.ConfigMapRef.Name], podRef)
				}
				if envFrom.SecretRef != nil {
					secretRefs[envFrom.SecretRef.Name] = append(secretRefs[envFrom.SecretRef.Name], podRef)
				}
			}
		}
	}

	return configMapRefs, secretRefs
}

// checkConfigMap verifica um ConfigMap específico
func (c *ConfigCrossRefChecker) checkConfigMap(cm *corev1.ConfigMap, referencedBy []string) ConfigCrossRefHealth {
	health := ConfigCrossRefHealth{
		Name:         cm.Name,
		Namespace:    cm.Namespace,
		ResourceType: "ConfigMap",
		KeyCount:     len(cm.Data) + len(cm.BinaryData),
		CreatedAt:    cm.CreationTimestamp.Time,
		ReferencedBy: referencedBy,
		Suggestions:  []string{},
	}

	// Verificar se é órfão
	if len(referencedBy) == 0 {
		health.IsOrphan = true
		health.Status = StatusWarning
		health.Message = "ConfigMap não é referenciado por nenhum pod"
		health.Suggestions = append(health.Suggestions, "Verificar se este ConfigMap ainda é necessário")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl get cm %s -n %s -o yaml", cm.Name, cm.Namespace))
		return health
	}

	// ConfigMap referenciado e sem problemas
	health.Status = StatusHealthy
	health.Message = fmt.Sprintf("ConfigMap referenciado por %d pod(s)", len(referencedBy))

	return health
}

// checkSecret verifica um Secret específico
func (c *ConfigCrossRefChecker) checkSecret(secret *corev1.Secret, referencedBy []string) ConfigCrossRefHealth {
	health := ConfigCrossRefHealth{
		Name:         secret.Name,
		Namespace:    secret.Namespace,
		ResourceType: "Secret",
		KeyCount:     len(secret.Data) + len(secret.StringData),
		CreatedAt:    secret.CreationTimestamp.Time,
		ReferencedBy: referencedBy,
		Suggestions:  []string{},
	}

	// Verificar se é órfão
	if len(referencedBy) == 0 {
		health.IsOrphan = true
		health.Status = StatusWarning
		health.Message = "Secret não é referenciado por nenhum pod"
		health.Suggestions = append(health.Suggestions, "Verificar se este Secret ainda é necessário")
		health.Suggestions = append(health.Suggestions, "Secrets órfãos podem representar risco de segurança")
		return health
	}

	// Secret referenciado e sem problemas
	health.Status = StatusHealthy
	health.Message = fmt.Sprintf("Secret referenciado por %d pod(s)", len(referencedBy))

	return health
}

// checkMissingReferences verifica se há referências a ConfigMaps/Secrets que não existem
func (c *ConfigCrossRefChecker) checkMissingReferences(pods []corev1.Pod, configMaps []corev1.ConfigMap, secrets []corev1.Secret, namespace string) []MissingReference {
	missing := []MissingReference{}

	// Criar sets de recursos existentes
	existingCMs := make(map[string]bool)
	for _, cm := range configMaps {
		existingCMs[cm.Name] = true
	}

	existingSecrets := make(map[string]bool)
	for _, s := range secrets {
		existingSecrets[s.Name] = true
	}

	for _, pod := range pods {
		podRef := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

		// Verificar volumes
		for _, vol := range pod.Spec.Volumes {
			if vol.ConfigMap != nil && !existingCMs[vol.ConfigMap.Name] {
				// ConfigMap opcional não gera erro
				if vol.ConfigMap.Optional != nil && *vol.ConfigMap.Optional {
					continue
				}
				missing = append(missing, MissingReference{
					ResourceType: "ConfigMap",
					Name:         vol.ConfigMap.Name,
					Namespace:    namespace,
					ReferencedBy: podRef,
					RefType:      "volume",
				})
			}
			if vol.Secret != nil && !existingSecrets[vol.Secret.SecretName] {
				if vol.Secret.Optional != nil && *vol.Secret.Optional {
					continue
				}
				missing = append(missing, MissingReference{
					ResourceType: "Secret",
					Name:         vol.Secret.SecretName,
					Namespace:    namespace,
					ReferencedBy: podRef,
					RefType:      "volume",
				})
			}
		}

		// Verificar containers
		for _, container := range pod.Spec.Containers {
			for _, envFrom := range container.EnvFrom {
				if envFrom.ConfigMapRef != nil && !existingCMs[envFrom.ConfigMapRef.Name] {
					if envFrom.ConfigMapRef.Optional != nil && *envFrom.ConfigMapRef.Optional {
						continue
					}
					missing = append(missing, MissingReference{
						ResourceType: "ConfigMap",
						Name:         envFrom.ConfigMapRef.Name,
						Namespace:    namespace,
						ReferencedBy: podRef,
						RefType:      "envFrom",
					})
				}
				if envFrom.SecretRef != nil && !existingSecrets[envFrom.SecretRef.Name] {
					if envFrom.SecretRef.Optional != nil && *envFrom.SecretRef.Optional {
						continue
					}
					missing = append(missing, MissingReference{
						ResourceType: "Secret",
						Name:         envFrom.SecretRef.Name,
						Namespace:    namespace,
						ReferencedBy: podRef,
						RefType:      "envFrom",
					})
				}
			}

			for _, env := range container.Env {
				if env.ValueFrom != nil {
					if env.ValueFrom.ConfigMapKeyRef != nil && !existingCMs[env.ValueFrom.ConfigMapKeyRef.Name] {
						if env.ValueFrom.ConfigMapKeyRef.Optional != nil && *env.ValueFrom.ConfigMapKeyRef.Optional {
							continue
						}
						missing = append(missing, MissingReference{
							ResourceType: "ConfigMap",
							Name:         env.ValueFrom.ConfigMapKeyRef.Name,
							Namespace:    namespace,
							ReferencedBy: podRef,
							RefType:      "env",
						})
					}
					if env.ValueFrom.SecretKeyRef != nil && !existingSecrets[env.ValueFrom.SecretKeyRef.Name] {
						if env.ValueFrom.SecretKeyRef.Optional != nil && *env.ValueFrom.SecretKeyRef.Optional {
							continue
						}
						missing = append(missing, MissingReference{
							ResourceType: "Secret",
							Name:         env.ValueFrom.SecretKeyRef.Name,
							Namespace:    namespace,
							ReferencedBy: podRef,
							RefType:      "env",
						})
					}
				}
			}
		}
	}

	return missing
}

// isSystemResource verifica se é um recurso de sistema
func (c *ConfigCrossRefChecker) isSystemResource(name string) bool {
	systemPrefixes := []string{
		"kube-",
		"istio-",
		"default-token-",
		"sh.helm.release",
	}

	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// isSystemSecret verifica se é um Secret de sistema
func (c *ConfigCrossRefChecker) isSystemSecret(secret *corev1.Secret) bool {
	// Secrets de service account
	if secret.Type == corev1.SecretTypeServiceAccountToken {
		return true
	}

	// Secrets de TLS gerenciados pelo sistema
	if secret.Type == corev1.SecretTypeTLS {
		// Verificar se é gerenciado por cert-manager ou similar
		if _, ok := secret.Annotations["cert-manager.io/certificate-name"]; ok {
			return true
		}
	}

	// Docker registry secrets do sistema
	if secret.Type == corev1.SecretTypeDockerConfigJson {
		if strings.HasPrefix(secret.Name, "default-") {
			return true
		}
	}

	return false
}

// GetOrphanCount retorna número de recursos órfãos
func GetOrphanCount(configs []ConfigCrossRefHealth) int {
	count := 0
	for _, cfg := range configs {
		if cfg.IsOrphan {
			count++
		}
	}
	return count
}

// GetMissingRefCount retorna número de referências faltando
func GetMissingRefCount(missing []MissingReference) int {
	return len(missing)
}
