package certificates

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// Scanner escaneia certificados TLS em múltiplos clusters
type Scanner struct {
	kubeManager   *config.KubeConfigManager
	rollbackStore *RollbackStore // pode ser nil — backup vira no-op nesse caso (ver backupBeforeOverwrite)
}

// NewScanner cria um novo Scanner. rollbackStore pode ser nil (ex: em testes) — nesse caso
// UploadCertificate simplesmente não faz backup antes de sobrescrever.
func NewScanner(km *config.KubeConfigManager, rollbackStore *RollbackStore) *Scanner {
	return &Scanner{
		kubeManager:   km,
		rollbackStore: rollbackStore,
	}
}

// ScanClusters escaneia certificados TLS nos clusters especificados
func (s *Scanner) ScanClusters(ctx context.Context, req ScanRequest) (*ScanResult, error) {
	result := &ScanResult{
		ScannedAt: time.Now(),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, cluster := range req.Clusters {
		wg.Add(1)
		go func(clusterName string) {
			defer wg.Done()

			certs, scanned, err := s.scanCluster(ctx, clusterName, req.Namespaces, req.Filter)
			if err != nil {
				log.Error().Err(err).Str("cluster", clusterName).Msg("Erro ao escanear cluster")
				return
			}

			mu.Lock()
			result.Certificates = append(result.Certificates, certs...)
			result.TotalScanned += scanned
			mu.Unlock()
		}(cluster)
	}

	wg.Wait()

	// Calcular resumo
	for _, cert := range result.Certificates {
		switch cert.Status {
		case "valid":
			result.Summary.Valid++
		case "expiring":
			result.Summary.Expiring++
		case "expired":
			result.Summary.Expired++
		}
	}
	result.Summary.Total = len(result.Certificates)

	return result, nil
}

// scanCluster escaneia um único cluster
func (s *Scanner) scanCluster(ctx context.Context, cluster string, namespaces []string, filter string) ([]CertificateInfo, int, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	// Listar secrets TLS
	tlsSecrets, err := s.listTLSSecrets(ctx, clientset, namespaces)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao listar secrets TLS em %s: %w", cluster, err)
	}

	log.Info().
		Str("cluster", cluster).
		Int("tlsSecrets", len(tlsSecrets)).
		Msg("Secrets TLS encontrados")

	// Parsear cada secret
	var certs []CertificateInfo
	for i := range tlsSecrets {
		info, err := ParseTLSSecret(&tlsSecrets[i], cluster)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Str("secret", tlsSecrets[i].Name).
				Str("namespace", tlsSecrets[i].Namespace).
				Msg("Erro ao parsear certificado, ignorando")
			continue
		}

		// Validação de cadeia (Fase 1, validate.go) já calculada no scan — mesma função usada por
		// "Validar Cadeia"/Upload/Rollback, sem alterar seu comportamento. Melhor-esforço: erro
		// aqui não derruba o certificado do resultado do scan, só fica sem ChainValidation.
		if cv, cvErr := ValidateCertificateChain(tlsSecrets[i].Data["tls.crt"], tlsSecrets[i].Data["tls.key"]); cvErr == nil {
			info.ChainValidation = cv
		} else {
			log.Warn().
				Err(cvErr).
				Str("cluster", cluster).
				Str("secret", tlsSecrets[i].Name).
				Str("namespace", tlsSecrets[i].Namespace).
				Msg("Erro ao validar cadeia durante o scan, ignorando")
		}

		certs = append(certs, *info)
	}

	// Cross-reference com Ingresses e Gateway API — cada um retorna o mapa host -> owners
	// referente à sua própria fonte; combinados antes de detectar conflito de host (B), pra pegar
	// também o caso Ingress-vs-Gateway (migração incompleta pra Gateway API).
	ingressHostOwners := s.crossRefIngresses(ctx, clientset, cluster, certs)
	gatewayHostOwners := s.crossRefGateways(cluster, certs)
	applyConfigIssues(certs, mergeHostOwners(ingressHostOwners, gatewayHostOwners))

	// Aplicar filtro
	if filter != "" && filter != "all" {
		certs = applyFilter(certs, filter)
	}

	return certs, len(tlsSecrets), nil
}

// listTLSSecrets lista todos os Secrets do tipo kubernetes.io/tls
func (s *Scanner) listTLSSecrets(ctx context.Context, clientset kubernetes.Interface, namespaces []string) ([]corev1.Secret, error) {
	var result []corev1.Secret

	listOpts := metav1.ListOptions{
		FieldSelector: "type=kubernetes.io/tls",
	}

	if len(namespaces) == 0 {
		// Listar em todos os namespaces
		secrets, err := clientset.CoreV1().Secrets("").List(ctx, listOpts)
		if err != nil {
			return nil, err
		}
		result = append(result, secrets.Items...)
	} else {
		for _, ns := range namespaces {
			secrets, err := clientset.CoreV1().Secrets(ns).List(ctx, listOpts)
			if err != nil {
				log.Warn().Err(err).Str("namespace", ns).Msg("Erro ao listar secrets no namespace")
				continue
			}
			result = append(result, secrets.Items...)
		}
	}

	return result, nil
}

// crossRefIngresses cruza certificados com Ingresses para preencher UsedByIngresses. Retorna o
// mapa host -> owners (pra detecção de conflito de host, config_issues.go/detectHostConflicts) —
// combinado pelo chamador (scanCluster) com o equivalente de Gateway API (crossRefGateways).
func (s *Scanner) crossRefIngresses(ctx context.Context, clientset kubernetes.Interface, cluster string, certs []CertificateInfo) map[string][]hostOwner {
	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Listar todos os ingresses
	ingresses, err := kubeClient.ListIngresses(ctx, nil, "", true)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Erro ao listar ingresses para cross-reference")
		return nil
	}

	// Construir mapa: namespace/secretName → []IngressRef, e host -> owners
	ingressMap := make(map[string][]IngressRef)
	hostOwners := make(map[string][]hostOwner)
	for _, ing := range ingresses {
		// Precisamos acessar o ingress raw para pegar spec.tls e annotations (backend-protocol/
		// ssl-passthrough) — IngressSummary não carrega nenhum dos dois.
		rawIng, err := clientset.NetworkingV1().Ingresses(ing.Namespace).Get(ctx, ing.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		backendTLS := isBackendTLSIngress(rawIng.Annotations)

		for _, tls := range rawIng.Spec.TLS {
			if tls.SecretName == "" {
				continue
			}
			key := fmt.Sprintf("%s/%s", ing.Namespace, tls.SecretName)
			ref := IngressRef{
				Name:         ing.Name,
				Namespace:    ing.Namespace,
				Hosts:        tls.Hosts,
				IngressClass: ing.IngressClass, // já resolvido por buildIngressSummary (com fallback de annotation)
				BackendTLS:   backendTLS,
			}
			ingressMap[key] = append(ingressMap[key], ref)

			for _, h := range tls.Hosts {
				hostOwners[h] = append(hostOwners[h], hostOwner{
					Kind:         "ingress",
					Namespace:    ing.Namespace,
					Name:         ing.Name,
					SecretName:   tls.SecretName,
					IngressClass: ing.IngressClass,
				})
			}
		}
	}

	// Preencher UsedByIngresses nos certificados
	for i := range certs {
		key := fmt.Sprintf("%s/%s", certs[i].Namespace, certs[i].SecretName)
		if refs, ok := ingressMap[key]; ok {
			certs[i].UsedByIngresses = refs
		}
	}

	return hostOwners
}

// isBackendTLSIngress reporta se o Ingress usa TLS re-encryption/passthrough pro backend — via
// nginx.ingress.kubernetes.io/backend-protocol (HTTPS/GRPCS) ou .../ssl-passthrough=true.
// Superfície de risco que o Diagnóstico Avançado (backend_tls_check.go) cobre sob demanda.
func isBackendTLSIngress(annotations map[string]string) bool {
	proto := strings.ToUpper(annotations["nginx.ingress.kubernetes.io/backend-protocol"])
	if proto == "HTTPS" || proto == "GRPCS" {
		return true
	}
	return annotations["nginx.ingress.kubernetes.io/ssl-passthrough"] == "true"
}

// CopyCertificate copia um certificado de um cluster/namespace para outros destinos
func (s *Scanner) CopyCertificate(ctx context.Context, req CopyRequest) (int, error) {
	// Obter secret de origem
	sourceClient, err := s.kubeManager.GetClient(req.SourceCluster)
	if err != nil {
		return 0, fmt.Errorf("erro ao obter client do cluster origem %s: %w", req.SourceCluster, err)
	}

	sourceSecret, err := sourceClient.CoreV1().Secrets(req.SourceNamespace).Get(ctx, req.SecretName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("erro ao obter secret %s/%s no cluster %s: %w", req.SourceNamespace, req.SecretName, req.SourceCluster, err)
	}

	copied := 0

	for _, targetCluster := range req.TargetClusters {
		targetClient, err := s.kubeManager.GetClient(targetCluster)
		if err != nil {
			log.Error().Err(err).Str("cluster", targetCluster).Msg("Erro ao obter client do cluster destino")
			continue
		}

		for _, targetNs := range req.TargetNamespaces {
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      req.SecretName,
					Namespace: targetNs,
					Labels:    sourceSecret.Labels,
					Annotations: map[string]string{
						"certificates.k8s-hpa-manager/copied-from":    fmt.Sprintf("%s/%s/%s", req.SourceCluster, req.SourceNamespace, req.SecretName),
						"certificates.k8s-hpa-manager/copied-at":     time.Now().UTC().Format(time.RFC3339),
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": sourceSecret.Data["tls.crt"],
					"tls.key": sourceSecret.Data["tls.key"],
				},
			}

			// Copiar annotations originais
			if sourceSecret.Annotations != nil {
				for k, v := range sourceSecret.Annotations {
					if newSecret.Annotations[k] == "" {
						newSecret.Annotations[k] = v
					}
				}
			}

			// Tentar atualizar, se não existir, criar
			_, err := targetClient.CoreV1().Secrets(targetNs).Update(ctx, newSecret, metav1.UpdateOptions{})
			if err != nil {
				// Tentar criar
				_, err = targetClient.CoreV1().Secrets(targetNs).Create(ctx, newSecret, metav1.CreateOptions{})
				if err != nil {
					log.Error().
						Err(err).
						Str("cluster", targetCluster).
						Str("namespace", targetNs).
						Str("secret", req.SecretName).
						Msg("Erro ao copiar certificado")
					continue
				}
			}

			log.Info().
				Str("targetCluster", targetCluster).
				Str("targetNamespace", targetNs).
				Str("secret", req.SecretName).
				Msg("Certificado copiado com sucesso")
			copied++
		}
	}

	return copied, nil
}

// UploadCertificate faz upload de um certificado para os destinos especificados
func (s *Scanner) UploadCertificate(ctx context.Context, req UploadRequest) (int, error) {
	// Validar PEM
	if err := ValidatePEM([]byte(req.TLSCrt), []byte(req.TLSKey)); err != nil {
		return 0, fmt.Errorf("certificado inválido: %w", err)
	}

	uploaded := 0

	for _, targetCluster := range req.TargetClusters {
		targetClient, err := s.kubeManager.GetClient(targetCluster)
		if err != nil {
			log.Error().Err(err).Str("cluster", targetCluster).Msg("Erro ao obter client do cluster destino")
			continue
		}

		for _, targetNs := range req.TargetNamespaces {
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      req.Name,
					Namespace: targetNs,
					Annotations: map[string]string{
						"certificates.k8s-hpa-manager/uploaded-at": time.Now().UTC().Format(time.RFC3339),
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": []byte(req.TLSCrt),
					"tls.key": []byte(req.TLSKey),
				},
			}

			s.backupBeforeOverwrite(ctx, targetClient, targetCluster, targetNs, req.Name)

			// Tentar atualizar, se não existir, criar
			_, err := targetClient.CoreV1().Secrets(targetNs).Update(ctx, newSecret, metav1.UpdateOptions{})
			if err != nil {
				_, err = targetClient.CoreV1().Secrets(targetNs).Create(ctx, newSecret, metav1.CreateOptions{})
				if err != nil {
					log.Error().
						Err(err).
						Str("cluster", targetCluster).
						Str("namespace", targetNs).
						Str("secret", req.Name).
						Msg("Erro ao fazer upload do certificado")
					continue
				}
			}

			log.Info().
				Str("targetCluster", targetCluster).
				Str("targetNamespace", targetNs).
				Str("secret", req.Name).
				Msg("Certificado uploaded com sucesso")
			uploaded++
		}
	}

	return uploaded, nil
}

// GetCertificateDetails retorna detalhes completos de um certificado específico
func (s *Scanner) GetCertificateDetails(ctx context.Context, cluster, namespace, name string) (*CertificateInfo, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao obter secret %s/%s: %w", namespace, name, err)
	}

	if secret.Type != corev1.SecretTypeTLS {
		return nil, fmt.Errorf("secret %s/%s não é do tipo kubernetes.io/tls", namespace, name)
	}

	info, err := ParseTLSSecret(secret, cluster)
	if err != nil {
		return nil, err
	}

	// Cross-reference com Ingresses
	certs := []CertificateInfo{*info}
	s.crossRefIngresses(ctx, clientset, cluster, certs)

	return &certs[0], nil
}

// backupBeforeOverwrite salva o conteúdo ATUAL do Secret (se existir) antes de UploadCertificate
// sobrescrevê-lo — melhor-esforço: se o Secret ainda não existe (primeira instalação) não há o que
// salvar; se rollbackStore for nil (ex: testes) ou o backup falhar por qualquer motivo, só loga um
// aviso e segue com o upload normalmente — nunca bloqueia a atualização do certificado.
func (s *Scanner) backupBeforeOverwrite(ctx context.Context, targetClient kubernetes.Interface, cluster, namespace, secretName string) {
	if s.rollbackStore == nil {
		return
	}

	existing, err := targetClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return // Secret não existe ainda — nada a fazer backup
	}

	if _, err := s.rollbackStore.Backup(cluster, namespace, secretName, existing); err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("secret", secretName).
			Msg("Erro ao fazer backup do certificado anterior antes de sobrescrever")
	}
}

// GetRawTLSSecret busca um Secret kubernetes.io/tls e retorna o PEM bruto de tls.crt/tls.key —
// usado pela validação de cadeia sob demanda (ValidateInstalledChain) e pelo backup de rollback
// (RollbackStore.Backup, Fase 2), que precisam do conteúdo bruto, não do CertificateInfo parseado.
func (s *Scanner) GetRawTLSSecret(ctx context.Context, cluster, namespace, name string) (tlsCrt, tlsKey []byte, err error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao obter secret %s/%s: %w", namespace, name, err)
	}
	if secret.Type != corev1.SecretTypeTLS {
		return nil, nil, fmt.Errorf("secret %s/%s não é do tipo kubernetes.io/tls", namespace, name)
	}

	return secret.Data["tls.crt"], secret.Data["tls.key"], nil
}

// applyFilter aplica filtro nos certificados
func applyFilter(certs []CertificateInfo, filter string) []CertificateInfo {
	var filtered []CertificateInfo

	for _, cert := range certs {
		switch filter {
		case "ingress":
			if len(cert.UsedByIngresses) > 0 {
				filtered = append(filtered, cert)
			}
		case "common":
			// Certificados usados por mais de um ingress ou namespace
			if len(cert.UsedByIngresses) > 1 {
				filtered = append(filtered, cert)
			} else if len(cert.UsedByIngresses) == 1 {
				// Verificar se é usado em múltiplos hosts
				if len(cert.UsedByIngresses[0].Hosts) > 1 {
					filtered = append(filtered, cert)
				}
			}
		default:
			filtered = append(filtered, cert)
		}
	}

	return filtered
}
