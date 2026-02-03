package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PVChecker valida PersistentVolumes e PersistentVolumeClaims
type PVChecker struct{}

// NewPVChecker cria um novo PV/PVC checker
func NewPVChecker() *PVChecker {
	return &PVChecker{}
}

// PVProgressCallback tipo de callback para progresso
type PVProgressCallback func(namespace, name, message string, status HealthStatus, current, total int)

// CheckAll valida todos os PVCs nos namespaces especificados
func (c *PVChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, applyFilters bool, progressCallback PVProgressCallback) []PVCHealth {
	results := []PVCHealth{}

	// Criar contexto com timeout se especificado
	workCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Primeiro, contar total de PVCs para calcular progresso
	totalPVCs := 0
	for _, ns := range namespaces {
		pvcs, err := client.CoreV1().PersistentVolumeClaims(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list PVCs")
			continue
		}
		totalPVCs += len(pvcs.Items)
	}

	currentPVC := 0

	for _, ns := range namespaces {
		// Listar PVCs no namespace
		pvcs, err := client.CoreV1().PersistentVolumeClaims(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list PVCs")
			continue
		}

		for _, pvc := range pvcs.Items {
			currentPVC++

			// Filtrar PVCs de sistema se applyFilters estiver habilitado
			if applyFilters && c.isSystemPVC(&pvc) {
				continue
			}

			// Publicar evento: validando PVC
			if progressCallback != nil {
				progressCallback(ns, pvc.Name,
					fmt.Sprintf("Validando PVC %s/%s...", ns, pvc.Name),
					StatusHealthy, currentPVC, totalPVCs)
			}

			health := c.validatePVC(workCtx, client, ns, &pvc)
			results = append(results, health)

			// Publicar resultado
			if progressCallback != nil {
				summary := c.getHealthSummary(health)
				progressCallback(ns, pvc.Name,
					fmt.Sprintf("PVC %s/%s: %s", ns, pvc.Name, summary), health.Status, currentPVC, totalPVCs)
			}
		}
	}

	return results
}

// isSystemPVC verifica se é um PVC de sistema que deve ser ignorado
func (c *PVChecker) isSystemPVC(pvc *corev1.PersistentVolumeClaim) bool {
	// Ignorar namespaces de sistema
	systemNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "istio-system", "cert-manager"}
	for _, ns := range systemNamespaces {
		if pvc.Namespace == ns {
			return true
		}
	}

	// Ignorar PVCs com prefixos de sistema
	systemPrefixes := []string{"etcd-", "kube-"}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(pvc.Name, prefix) {
			return true
		}
	}

	return false
}

// getHealthSummary gera resumo compacto do health check
func (c *PVChecker) getHealthSummary(health PVCHealth) string {
	if health.Status == StatusHealthy {
		return "OK"
	}
	return health.Message
}

// validatePVC valida um PVC específico
func (c *PVChecker) validatePVC(ctx context.Context, client kubernetes.Interface, namespace string, pvc *corev1.PersistentVolumeClaim) PVCHealth {
	health := PVCHealth{
		Name:             pvc.Name,
		Namespace:        namespace,
		Phase:            string(pvc.Status.Phase),
		StorageClassName: c.getStorageClassName(pvc),
		AccessModes:      c.getAccessModes(pvc),
		CheckedAt:        time.Now(),
		Issues:           []PVIssue{},
		Suggestions:      []string{},
	}

	// Extrair capacidade solicitada
	if req, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		health.RequestedStorage = req.String()
		health.RequestedBytes = req.Value()
	}

	// Verificar se tem PV vinculado
	if pvc.Spec.VolumeName != "" {
		health.VolumeName = pvc.Spec.VolumeName
		health.IsBound = pvc.Status.Phase == corev1.ClaimBound

		// Buscar informações do PV
		pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
		if err == nil {
			health.VolumeExists = true
			if cap, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
				health.ActualStorage = cap.String()
				health.ActualBytes = cap.Value()
			}
			health.ReclaimPolicy = string(pv.Spec.PersistentVolumeReclaimPolicy)
			health.VolumeMode = c.getVolumeMode(pv)

			// Calcular utilização (se disponível via métricas - placeholder)
			// Por enquanto, apenas verificar se capacity >= requested
			if health.ActualBytes > 0 && health.RequestedBytes > 0 {
				health.UsagePercent = float64(health.RequestedBytes) / float64(health.ActualBytes) * 100
			}
		} else {
			health.VolumeExists = false
			health.Issues = append(health.Issues, PVIssue{
				Type:        "volume",
				Description: fmt.Sprintf("PV '%s' referenciado nao existe", pvc.Spec.VolumeName),
				Severity:    "critical",
			})
			health.Suggestions = append(health.Suggestions, "Verificar se o PV foi deletado acidentalmente ou se o nome esta correto")
		}
	}

	// Verificar status do PVC
	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		health.IsPending = true
		health.Issues = append(health.Issues, PVIssue{
			Type:        "status",
			Description: "PVC esta em estado Pending - aguardando provisionamento ou binding",
			Severity:    "warning",
		})
		health.Suggestions = append(health.Suggestions,
			"Verificar se StorageClass existe e tem provisioner configurado",
			"Verificar eventos do PVC: kubectl describe pvc "+pvc.Name+" -n "+namespace,
			"Verificar se há PVs disponíveis com capacidade e access mode compatíveis")

		// Verificar há quanto tempo está pending
		if pvc.CreationTimestamp.Time.Before(time.Now().Add(-5 * time.Minute)) {
			health.Issues = append(health.Issues, PVIssue{
				Type:        "status",
				Description: fmt.Sprintf("PVC esta Pending ha mais de 5 minutos (criado em %s)", pvc.CreationTimestamp.Format(time.RFC3339)),
				Severity:    "critical",
			})
		}

	case corev1.ClaimLost:
		health.Issues = append(health.Issues, PVIssue{
			Type:        "status",
			Description: "PVC esta em estado Lost - PV vinculado foi deletado",
			Severity:    "critical",
		})
		health.Suggestions = append(health.Suggestions,
			"Recriar o PV ou deletar e recriar o PVC",
			"Verificar se houve perda de dados")

	case corev1.ClaimBound:
		// OK - mas verificar outras condições
		health.IsBound = true
	}

	// Verificar StorageClass
	if health.StorageClassName == "" {
		health.Issues = append(health.Issues, PVIssue{
			Type:        "config",
			Description: "PVC nao tem StorageClass definida - usando default (pode causar problemas)",
			Severity:    "warning",
		})
		health.Suggestions = append(health.Suggestions, "Definir storageClassName explicitamente para garantir comportamento previsível")
	} else {
		// Verificar se StorageClass existe
		_, err := client.StorageV1().StorageClasses().Get(ctx, health.StorageClassName, metav1.GetOptions{})
		if err != nil {
			health.StorageClassExists = false
			health.Issues = append(health.Issues, PVIssue{
				Type:        "config",
				Description: fmt.Sprintf("StorageClass '%s' nao existe no cluster", health.StorageClassName),
				Severity:    "critical",
			})
			health.Suggestions = append(health.Suggestions,
				"Criar a StorageClass ou alterar o PVC para usar uma existente",
				"Listar StorageClasses disponíveis: kubectl get sc")
		} else {
			health.StorageClassExists = true
		}
	}

	// Verificar access modes
	if len(health.AccessModes) == 0 {
		health.Issues = append(health.Issues, PVIssue{
			Type:        "config",
			Description: "PVC nao tem access modes definidos",
			Severity:    "warning",
		})
	}

	// Verificar se é ReadWriteMany em cloud provider (pode ser problema)
	for _, mode := range health.AccessModes {
		if mode == "ReadWriteMany" {
			health.Issues = append(health.Issues, PVIssue{
				Type:        "config",
				Description: "PVC usa ReadWriteMany - verificar se StorageClass suporta este modo",
				Severity:    "warning",
			})
			health.Suggestions = append(health.Suggestions,
				"ReadWriteMany requer storage que suporte acesso concorrente (NFS, Azure Files, etc.)",
				"Discos gerenciados (Azure Disk, EBS) geralmente suportam apenas ReadWriteOnce")
			break
		}
	}

	// Verificar capacidade muito pequena
	if health.RequestedBytes > 0 && health.RequestedBytes < 1*1024*1024*1024 { // < 1Gi
		health.Issues = append(health.Issues, PVIssue{
			Type:        "config",
			Description: fmt.Sprintf("PVC tem capacidade muito pequena (%s) - pode encher rapidamente", health.RequestedStorage),
			Severity:    "warning",
		})
	}

	// Verificar reclaim policy
	if health.ReclaimPolicy == "Delete" && health.IsBound {
		health.Issues = append(health.Issues, PVIssue{
			Type:        "config",
			Description: "PV usa ReclaimPolicy=Delete - dados serao perdidos se PVC for deletado",
			Severity:    "warning",
		})
		health.Suggestions = append(health.Suggestions, "Considerar ReclaimPolicy=Retain para dados importantes")
	}

	// Determinar status geral
	health.Status = c.determineStatus(health)
	health.Message = c.generateMessage(health)

	return health
}

// getStorageClassName extrai o nome da StorageClass
func (c *PVChecker) getStorageClassName(pvc *corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	// Fallback para annotation (deprecated)
	if className, ok := pvc.Annotations["volume.beta.kubernetes.io/storage-class"]; ok {
		return className
	}
	return ""
}

// getAccessModes converte access modes para strings
func (c *PVChecker) getAccessModes(pvc *corev1.PersistentVolumeClaim) []string {
	modes := []string{}
	for _, mode := range pvc.Spec.AccessModes {
		modes = append(modes, string(mode))
	}
	return modes
}

// getVolumeMode retorna o modo do volume
func (c *PVChecker) getVolumeMode(pv *corev1.PersistentVolume) string {
	if pv.Spec.VolumeMode != nil {
		return string(*pv.Spec.VolumeMode)
	}
	return "Filesystem" // default
}

// determineStatus determina o status geral do PVC
func (c *PVChecker) determineStatus(health PVCHealth) HealthStatus {
	hasCritical := false
	hasWarning := false

	for _, issue := range health.Issues {
		if issue.Severity == "critical" {
			hasCritical = true
		} else if issue.Severity == "warning" {
			hasWarning = true
		}
	}

	if hasCritical {
		return StatusCritical
	}
	if hasWarning {
		return StatusWarning
	}
	return StatusHealthy
}

// generateMessage gera a mensagem principal do PVC
func (c *PVChecker) generateMessage(health PVCHealth) string {
	if health.Status == StatusHealthy {
		return fmt.Sprintf("OK: PVC %s/%s bound com %s (%s)",
			health.Namespace, health.Name, health.ActualStorage, health.StorageClassName)
	}

	// Construir mensagem com problemas
	var problems []string
	for _, issue := range health.Issues {
		if issue.Severity == "critical" {
			problems = append(problems, fmt.Sprintf("CRITICO: %s", issue.Description))
		} else {
			problems = append(problems, fmt.Sprintf("AVISO: %s", issue.Description))
		}
	}

	if len(problems) == 0 {
		return fmt.Sprintf("PVC %s/%s com status desconhecido", health.Namespace, health.Name)
	}

	return strings.Join(problems, " | ")
}

// CheckPVUsage verifica utilização de storage via métricas (se disponível)
// Esta função pode ser expandida para usar metrics-server ou Prometheus
func (c *PVChecker) CheckPVUsage(ctx context.Context, client kubernetes.Interface, pvcName, namespace string) (used, capacity resource.Quantity, err error) {
	// Placeholder - em produção, isso consultaria métricas do kubelet ou Prometheus
	// kubectl top pv não existe nativamente, então usamos métricas de pods

	// Por enquanto, retornar valores zerados
	return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("metrics collection not implemented")
}
