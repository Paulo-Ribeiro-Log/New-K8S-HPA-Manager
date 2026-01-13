package collectors

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ResourceValidator valida recursos e suas referências
type ResourceValidator struct {
	clientset *kubernetes.Clientset
}

// NewResourceValidator cria um novo validador
func NewResourceValidator(clientset *kubernetes.Clientset) *ResourceValidator {
	return &ResourceValidator{clientset: clientset}
}

// ValidateConfigMapReferences valida referências de ConfigMap em um Pod
func (v *ResourceValidator) ValidateConfigMapReferences(ctx context.Context, pod *corev1.Pod, cmName string, namespace string) *ValidationResult {
	result := &ValidationResult{
		ResourceType: "ConfigMap",
		ResourceName: cmName,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	// Buscar ConfigMap
	cm, err := v.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		result.Exists = false
		result.Issues = append(result.Issues, fmt.Sprintf("ConfigMap '%s' NÃO EXISTE no namespace '%s'", cmName, namespace))
		result.Suggestions = append(result.Suggestions, fmt.Sprintf("Criar ConfigMap: kubectl create configmap %s -n %s --from-literal=key=value", cmName, namespace))
		return result
	}

	result.Exists = true
	fmt.Printf("    ✅ ConfigMap '%s' EXISTE - iniciando validação de referências...\n", cmName)

	// Extrair keys disponíveis
	availableKeys := make(map[string]bool)
	for key := range cm.Data {
		availableKeys[key] = true
	}
	for key := range cm.BinaryData {
		availableKeys[key] = true
	}

	fmt.Printf("    📊 Keys disponíveis: %v\n", getKeysFromMap(availableKeys))

	// Validar referências em env vars
	referencedKeys := v.extractConfigMapKeysFromPod(pod, cmName)
	fmt.Printf("    🔍 Keys referenciadas no Pod: %v\n", referencedKeys)

	// Comparar
	missingKeys := []string{}
	for _, refKey := range referencedKeys {
		if !availableKeys[refKey] {
			missingKeys = append(missingKeys, refKey)
		}
	}

	if len(missingKeys) > 0 {
		result.Issues = append(result.Issues,
			fmt.Sprintf("Keys referenciadas que NÃO EXISTEM no ConfigMap: %v", missingKeys))
		result.Suggestions = append(result.Suggestions,
			fmt.Sprintf("Adicionar keys faltantes ao ConfigMap ou corrigir referências no Pod"))
	}

	// Validar sintaxe dos valores YAML/JSON
	for key, value := range cm.Data {
		if strings.Contains(key, "yaml") || strings.Contains(key, "yml") {
			if err := yaml.Unmarshal([]byte(value), &map[string]interface{}{}); err != nil {
				result.Issues = append(result.Issues,
					fmt.Sprintf("Key '%s' contém YAML INVÁLIDO: %v", key, err))
				result.Suggestions = append(result.Suggestions,
					fmt.Sprintf("Corrigir sintaxe YAML da key '%s'", key))
			}
		}
	}

	if len(result.Issues) == 0 {
		result.Issues = append(result.Issues, "ConfigMap existe e todas as keys estão corretas")
		result.Suggestions = append(result.Suggestions, "Investigar outros problemas: permissões, mountPath, formato dos valores")
	}

	return result
}

// extractConfigMapKeysFromPod extrai todas as keys de ConfigMap referenciadas no Pod
func (v *ResourceValidator) extractConfigMapKeysFromPod(pod *corev1.Pod, cmName string) []string {
	keys := []string{}

	// Verificar env vars
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				if env.ValueFrom.ConfigMapKeyRef.Name == cmName {
					keys = append(keys, env.ValueFrom.ConfigMapKeyRef.Key)
				}
			}
		}
	}

	// Verificar volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil && volume.ConfigMap.Name == cmName {
			if len(volume.ConfigMap.Items) > 0 {
				for _, item := range volume.ConfigMap.Items {
					keys = append(keys, item.Key)
				}
			} else {
				// Se não especifica items, usa todas as keys
				keys = append(keys, "(todas as keys)")
			}
		}
	}

	return keys
}

// ValidateSecretReferences valida referências de Secret em um Pod
func (v *ResourceValidator) ValidateSecretReferences(ctx context.Context, pod *corev1.Pod, secretName string, namespace string) *ValidationResult {
	result := &ValidationResult{
		ResourceType: "Secret",
		ResourceName: secretName,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	// Buscar Secret
	secret, err := v.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		result.Exists = false
		result.Issues = append(result.Issues, fmt.Sprintf("Secret '%s' NÃO EXISTE no namespace '%s'", secretName, namespace))
		result.Suggestions = append(result.Suggestions, fmt.Sprintf("Criar Secret: kubectl create secret generic %s -n %s --from-literal=key=value", secretName, namespace))
		return result
	}

	result.Exists = true
	fmt.Printf("    ✅ Secret '%s' EXISTE - iniciando validação de referências...\n", secretName)

	// Extrair keys disponíveis
	availableKeys := make(map[string]bool)
	for key := range secret.Data {
		availableKeys[key] = true
	}

	fmt.Printf("    📊 Keys disponíveis: %v\n", getKeysFromMap(availableKeys))

	// Validar referências
	referencedKeys := v.extractSecretKeysFromPod(pod, secretName)
	fmt.Printf("    🔍 Keys referenciadas no Pod: %v\n", referencedKeys)

	missingKeys := []string{}
	for _, refKey := range referencedKeys {
		if !availableKeys[refKey] {
			missingKeys = append(missingKeys, refKey)
		}
	}

	if len(missingKeys) > 0 {
		result.Issues = append(result.Issues,
			fmt.Sprintf("Keys referenciadas que NÃO EXISTEM no Secret: %v", missingKeys))
		result.Suggestions = append(result.Suggestions,
			fmt.Sprintf("Adicionar keys faltantes ao Secret"))
	}

	if len(result.Issues) == 0 {
		result.Issues = append(result.Issues, "Secret existe e todas as keys estão corretas")
		result.Suggestions = append(result.Suggestions, "Investigar permissões do ServiceAccount")
	}

	return result
}

// extractSecretKeysFromPod extrai todas as keys de Secret referenciadas no Pod
func (v *ResourceValidator) extractSecretKeysFromPod(pod *corev1.Pod, secretName string) []string {
	keys := []string{}

	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				if env.ValueFrom.SecretKeyRef.Name == secretName {
					keys = append(keys, env.ValueFrom.SecretKeyRef.Key)
				}
			}
		}
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == secretName {
			if len(volume.Secret.Items) > 0 {
				for _, item := range volume.Secret.Items {
					keys = append(keys, item.Key)
				}
			} else {
				keys = append(keys, "(todas as keys)")
			}
		}
	}

	return keys
}

// ValidateServiceReference valida se um Service existe e tem configuração correta
func (v *ResourceValidator) ValidateServiceReference(ctx context.Context, pod *corev1.Pod, serviceName string, namespace string) *ValidationResult {
	result := &ValidationResult{
		ResourceType: "Service",
		ResourceName: serviceName,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	// Buscar Service
	svc, err := v.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		result.Exists = false
		result.Issues = append(result.Issues, fmt.Sprintf("Service '%s' NÃO EXISTE no namespace '%s'", serviceName, namespace))
		result.Suggestions = append(result.Suggestions, "Criar Service com selector e ports corretos")
		return result
	}

	result.Exists = true
	fmt.Printf("    ✅ Service '%s' EXISTE - iniciando validação...\n", serviceName)

	// Validar selector do Service vs labels do Pod
	podLabels := pod.Labels
	svcSelector := svc.Spec.Selector

	if len(svcSelector) == 0 {
		result.Issues = append(result.Issues, "Service não tem selector definido")
	} else {
		mismatch := false
		missingLabels := []string{}

		for key, value := range svcSelector {
			if podLabels[key] != value {
				mismatch = true
				missingLabels = append(missingLabels, fmt.Sprintf("%s=%s", key, value))
			}
		}

		if mismatch {
			result.Issues = append(result.Issues,
				fmt.Sprintf("Selector do Service não corresponde aos labels do Pod. Esperado: %v, Pod tem: %v",
					missingLabels, podLabels))
			result.Suggestions = append(result.Suggestions, "Corrigir selector do Service ou labels do Pod")
		}
	}

	// Validar ports
	containerPorts := []int32{}
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			containerPorts = append(containerPorts, port.ContainerPort)
		}
	}

	for _, svcPort := range svc.Spec.Ports {
		targetPort := svcPort.TargetPort.IntVal
		if targetPort == 0 && svcPort.TargetPort.StrVal != "" {
			// TargetPort é named port, precisa resolver
			result.Issues = append(result.Issues,
				fmt.Sprintf("Service usa named port '%s' - verifique se container define este nome",
					svcPort.TargetPort.StrVal))
		} else {
			found := false
			for _, containerPort := range containerPorts {
				if targetPort == containerPort {
					found = true
					break
				}
			}
			if !found && len(containerPorts) > 0 {
				result.Issues = append(result.Issues,
					fmt.Sprintf("TargetPort %d do Service não corresponde a nenhum containerPort do Pod (disponíveis: %v)",
						targetPort, containerPorts))
				result.Suggestions = append(result.Suggestions,
					fmt.Sprintf("Ajustar targetPort para corresponder aos containerPorts do Pod"))
			}
		}
	}

	if len(result.Issues) == 0 {
		result.Issues = append(result.Issues, "Service existe e configuração está correta")
		result.Suggestions = append(result.Suggestions, "Investigar conectividade de rede ou endpoints")
	}

	return result
}

// getKeysFromMap helper para extrair keys de um map
func getKeysFromMap(m map[string]bool) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ValidateIngressReference valida se um Ingress existe e tem configuração correta
func (v *ResourceValidator) ValidateIngressReference(ctx context.Context, serviceName string, namespace string) *ValidationResult {
	result := &ValidationResult{
		ResourceType: "Ingress",
		ResourceName: serviceName,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	// Buscar Service primeiro
	svc, err := v.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		result.Exists = false
		result.Issues = append(result.Issues, fmt.Sprintf("Service backend '%s' não existe", serviceName))
		return result
	}

	result.Exists = true
	fmt.Printf("    ✅ Service backend '%s' EXISTE\n", serviceName)

	// Listar Ingresses que referenciam este service
	ingresses, err := v.clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("Erro ao listar Ingresses: %v", err))
		return result
	}

	referencedBy := []string{}
	for _, ing := range ingresses.Items {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP != nil {
				for _, path := range rule.HTTP.Paths {
					if path.Backend.Service != nil && path.Backend.Service.Name == serviceName {
						referencedBy = append(referencedBy, ing.Name)

						// Validar se porta do Ingress corresponde a porta do Service
						found := false
						for _, svcPort := range svc.Spec.Ports {
							if svcPort.Port == path.Backend.Service.Port.Number ||
								svcPort.Name == path.Backend.Service.Port.Name {
								found = true
								break
							}
						}
						if !found {
							result.Issues = append(result.Issues,
								fmt.Sprintf("Ingress '%s' referencia porta que não existe no Service", ing.Name))
						}
					}
				}
			}
		}
	}

	if len(referencedBy) > 0 {
		result.Suggestions = append(result.Suggestions,
			fmt.Sprintf("Service é referenciado pelos Ingresses: %v", referencedBy))
	}

	return result
}

// ValidatePVCReference valida se um PersistentVolumeClaim existe
func (v *ResourceValidator) ValidatePVCReference(ctx context.Context, pvcName string, namespace string) *ValidationResult {
	result := &ValidationResult{
		ResourceType: "PersistentVolumeClaim",
		ResourceName: pvcName,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	pvc, err := v.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		result.Exists = false
		result.Issues = append(result.Issues, fmt.Sprintf("PVC '%s' NÃO EXISTE no namespace '%s'", pvcName, namespace))
		result.Suggestions = append(result.Suggestions, "Criar PersistentVolumeClaim ou verificar nome correto")
		return result
	}

	result.Exists = true
	fmt.Printf("    ✅ PVC '%s' EXISTE\n", pvcName)

	// Validar status do PVC
	if pvc.Status.Phase != corev1.ClaimBound {
		result.Issues = append(result.Issues,
			fmt.Sprintf("PVC existe mas não está Bound (status: %s)", pvc.Status.Phase))

		if pvc.Status.Phase == corev1.ClaimPending {
			result.Suggestions = append(result.Suggestions,
				"PVC está Pending - verificar se existe StorageClass compatível e PersistentVolume disponível")
		}
	}

	return result
}
