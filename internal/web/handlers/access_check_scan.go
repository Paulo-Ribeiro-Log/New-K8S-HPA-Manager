package handlers

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

const (
	scanFleetConcurrency          = 8
	scanFleetPerClusterTimeout    = 45 * time.Second
	scanFleetNamespaceConcurrency = 6
)

// fleetClusterResult é o resultado do scan de um único cluster dentro do ScanFleet.
type fleetClusterResult struct {
	Cluster         string             `json:"cluster"`
	Reachable       bool               `json:"reachable"`
	IAMAdminAccess  []iamAdminMatchDTO `json:"iamAdminAccess"`
	NamespaceAccess *fleetNamespaceHit `json:"namespaceAccess,omitempty"`
	Error           string             `json:"error,omitempty"`
}

// fleetNamespaceHit é o resultado do acesso RBAC real (não apenas conectividade) do analista
// num cluster. Quando um namespace específico foi informado, `Namespaces` tem no máximo 1 item
// (o próprio namespace, se houver acesso). Quando nenhum namespace foi informado, `Namespaces`
// lista TODOS os namespaces (não-sistema) onde o analista tem acesso real a algum recurso —
// é isso que responde "onde ele pode de fato manipular aplicações", ao contrário de
// `Reachable` (que só indica que o SERVIDOR consegue falar com o kube-apiserver do cluster).
type fleetNamespaceHit struct {
	AnyAccess  bool     `json:"anyAccess"`
	Namespaces []string `json:"namespaces"`
}

// workloadAccessProbes são checados em ordem via SelfSubjectAccessReview até o primeiro
// "Allowed" — um teste definitivo (equivalente a `kubectl auth can-i list pods -n <ns> --as
// <email>`), sem a ressalva de incompletude que o SelfSubjectRulesReview tem para políticas
// RBAC complexas (a API do K8s documenta que pode devolver um subconjunto incompleto de regras
// nesses casos — usar SelfSubjectRulesReview pra decidir "tem acesso ou não" gerava falso
// negativo real quando o acesso do analista vinha de bindings que a review não enumerava).
var workloadAccessProbes = []struct{ group, resource, verb string }{
	{"", "pods", "list"},
	{"apps", "deployments", "list"},
	{"", "secrets", "list"},
	{"", "configmaps", "list"},
}

// hasWorkloadAccess testa se o client (já impersonado) consegue manipular algum recurso de
// workload comum no namespace — para no primeiro "Allowed". Só retorna erro se TODAS as
// tentativas falharem (nenhuma resposta definitiva do cluster).
func hasWorkloadAccess(ctx context.Context, clientset kubernetes.Interface, namespace string) (bool, error) {
	var lastErr error
	for _, p := range workloadAccessProbes {
		sar := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Verb:      p.verb,
					Group:     p.group,
					Resource:  p.resource,
				},
			},
		}
		result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		if result.Status.Allowed {
			return true, nil
		}
	}
	return false, lastErr
}

// ScanFleet varre todos os clusters AKS cadastrados verificando acesso admin via IAM (sempre) e,
// opcionalmente, acesso RBAC num namespace específico — responde "esse analista tem acesso
// elevado em algum lugar da nossa frota?" sem precisar testar cluster por cluster manualmente.
//
// GET /api/v1/access-check/scan-fleet?email=&namespace=
func (h *AccessCheckHandler) ScanFleet(c *gin.Context) {
	email := strings.TrimSpace(c.Query("email"))
	namespace := c.Query("namespace")
	if email == "" {
		c.JSON(400, errorResponse("MISSING_PARAMS", "parâmetro 'email' é obrigatório"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Second)
	defer cancel()

	// Resolver grupos UMA única vez — reaproveitado por todos os clusters do scan (cache de
	// 10min em ResolveVVCloudGroups/GetAllGroups já evita chamadas az repetidas).
	allGroups, groupsErr := h.aadLookup.GetAllGroups(ctx, email)
	// Slices vazios, não nil — nil vira `null` no JSON e engana checks tipo `campo &&` no
	// frontend (ver comentário equivalente em access_check.go/buildImpersonatedClient).
	allGroupDTOs := []matchedGroupDTO{}
	matched := []matchedGroupDTO{}
	groupIDs := []string{}
	for _, g := range allGroups {
		dto := matchedGroupDTO{ID: g.ID, DisplayName: g.DisplayName}
		allGroupDTOs = append(allGroupDTOs, dto)
		if strings.HasPrefix(g.DisplayName, vvCloudGroupPrefix) {
			groupIDs = append(groupIDs, g.ID)
			matched = append(matched, dto)
		}
	}
	var groupsErrStr string
	if groupsErr != nil {
		groupsErrStr = groupsErr.Error()
	}

	clusters, err := loadClusterConfig()
	if err != nil {
		c.JSON(500, errorResponse("CONFIG_LOAD_FAILED", "falha ao carregar config de clusters AKS: "+err.Error()))
		return
	}

	results := make([]fleetClusterResult, len(clusters))
	sem := make(chan struct{}, scanFleetConcurrency)
	var wg sync.WaitGroup

	for i, cfg := range clusters {
		wg.Add(1)
		go func(i int, clusterName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			clusterCtx, cancel := context.WithTimeout(ctx, scanFleetPerClusterTimeout)
			defer cancel()

			results[i] = h.scanOneFleetCluster(clusterCtx, clusterName, namespace, email, allGroupDTOs, groupIDs)
		}(i, cfg.ClusterName)
	}
	wg.Wait()

	h.logAccessCheck(c, "(todos os clusters)", namespace, email, "", "scan-fleet", nil)

	c.JSON(200, gin.H{
		"email":                 email,
		"matchedGroups":         matched,
		"allGroups":             allGroupDTOs,
		"groupsResolutionError": groupsErrStr,
		"results":               results,
	})
}

// scanOneFleetCluster roda a checagem de IAM (sempre) e RBAC real para um único cluster —
// isolado numa função própria pra tolerar falha por cluster sem derrubar o scan. RBAC é
// verificado no namespace informado (se houver) ou, na ausência dele, varrendo TODOS os
// namespaces não-sistema do cluster — sem isso, "Reachable" (mera conectividade de rede do
// servidor com o kube-apiserver, nada relacionado ao analista) era o único sinal exibido,
// dando a falsa impressão de que o analista tinha acesso a qualquer cluster alcançável.
func (h *AccessCheckHandler) scanOneFleetCluster(ctx context.Context, clusterName, namespace, email string, allGroups []matchedGroupDTO, groupIDs []string) fleetClusterResult {
	result := fleetClusterResult{Cluster: clusterName, IAMAdminAccess: []iamAdminMatchDTO{}}

	// IAM: chamada ao Azure Resource Manager, independe de conectividade com o kube-apiserver.
	if iamMatches, err := findIAMAdminBypass(ctx, h.kubeManager, clusterName, allGroups); err == nil {
		result.IAMAdminAccess = iamMatches
	}

	cfg, err := h.kubeManager.GetRestConfig(clusterName)
	if err != nil {
		result.Reachable = false
		result.Error = "inacessível — verifique VPN/conectividade"
		return result
	}
	result.Reachable = true

	// Namespaces a checar: o informado, ou todos os não-sistema do cluster (via client SEM
	// impersonation — listar namespaces é uma operação do servidor, não do analista).
	targetNamespaces := []string{namespace}
	if namespace == "" {
		plainClientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			result.Error = "falha ao montar client: " + err.Error()
			return result
		}
		nsList, err := plainClientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			result.Error = "falha ao listar namespaces: " + err.Error()
			return result
		}
		targetNamespaces = targetNamespaces[:0]
		for _, ns := range nsList.Items {
			if !kubeclient.IsSystemNamespace(ns.Name) {
				targetNamespaces = append(targetNamespaces, ns.Name)
			}
		}
	}

	cfg.Impersonate = rest.ImpersonationConfig{UserName: email, Groups: groupIDs}
	impersonated, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		result.Error = "falha ao montar client impersonado: " + err.Error()
		return result
	}

	var (
		mu         sync.Mutex
		accessible = []string{} // nunca nil — vira `null` no JSON e engana `campo &&` no frontend
		wg         sync.WaitGroup
	)
	sem := make(chan struct{}, scanFleetNamespaceConcurrency)
	var firstErr error

	for _, ns := range targetNamespaces {
		if ctx.Err() != nil {
			break // timeout do cluster estourou — retorna parcial em vez de nada
		}
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			allowed, err := hasWorkloadAccess(ctx, impersonated, ns)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if allowed {
				mu.Lock()
				accessible = append(accessible, ns)
				mu.Unlock()
			}
		}(ns)
	}
	wg.Wait()

	if len(accessible) == 0 && firstErr != nil {
		// Só reporta erro se NENHUM namespace respondeu — parcial com pelo menos 1 hit já
		// é um resultado útil (ex: timeout no meio da varredura de uma frota grande).
		if isImpersonationForbidden(firstErr) {
			result.Error = "servidor sem permissão de impersonate neste cluster"
		} else {
			result.Error = "falha ao consultar RBAC: " + firstErr.Error()
		}
		return result
	}

	sort.Strings(accessible)
	result.NamespaceAccess = &fleetNamespaceHit{AnyAccess: len(accessible) > 0, Namespaces: accessible}
	return result
}
