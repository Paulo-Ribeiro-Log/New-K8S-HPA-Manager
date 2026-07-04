package handlers

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	scanFleetConcurrency       = 8
	scanFleetPerClusterTimeout = 30 * time.Second
)

// fleetClusterResult é o resultado do scan de um único cluster dentro do ScanFleet.
type fleetClusterResult struct {
	Cluster         string             `json:"cluster"`
	Reachable       bool               `json:"reachable"`
	IAMAdminAccess  []iamAdminMatchDTO `json:"iamAdminAccess"`
	NamespaceAccess *fleetNamespaceHit `json:"namespaceAccess,omitempty"`
	Error           string             `json:"error,omitempty"`
}

type fleetNamespaceHit struct {
	AnyAccess bool `json:"anyAccess"`
}

// genericBaselineResources são os recursos que QUALQUER usuário autenticado enxerga por padrão
// (NetworkPolicies do Calico, self-review) — usados pra distinguir "acesso real" de ruído
// genérico ao classificar anyAccess no scan de frota (versão simplificada da lógica de
// categorias já usada na Visão Geral do frontend, aqui só como sim/não por cluster).
var genericBaselineResources = map[string]bool{
	"selfsubjectaccessreviews": true,
	"selfsubjectrulesreviews":  true,
	"selfsubjectreviews":       true,
	"networkpolicies":          true,
	"globalnetworkpolicies":    true,
}

func hasNonGenericAccess(rules []authorizationv1.ResourceRule) bool {
	for _, r := range rules {
		for _, res := range r.Resources {
			if res == "*" || !genericBaselineResources[res] {
				return true
			}
		}
	}
	return false
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
		"groupsResolutionError": groupsErrStr,
		"results":               results,
	})
}

// scanOneFleetCluster roda a checagem de IAM (sempre) e RBAC (se namespace informado) para um
// único cluster — isolado numa função própria pra tolerar falha por cluster sem derrubar o scan.
func (h *AccessCheckHandler) scanOneFleetCluster(ctx context.Context, clusterName, namespace, email string, allGroups []matchedGroupDTO, groupIDs []string) fleetClusterResult {
	result := fleetClusterResult{Cluster: clusterName, IAMAdminAccess: []iamAdminMatchDTO{}}

	// IAM: chamada ao Azure Resource Manager, independe de conectividade com o kube-apiserver.
	if iamMatches, err := findIAMAdminBypass(ctx, clusterName, allGroups); err == nil {
		result.IAMAdminAccess = iamMatches
	}

	cfg, err := h.kubeManager.GetRestConfig(clusterName)
	if err != nil {
		result.Reachable = false
		result.Error = "inacessível — verifique VPN/conectividade"
		return result
	}
	result.Reachable = true

	if namespace == "" {
		return result
	}

	cfg.Impersonate = rest.ImpersonationConfig{UserName: email, Groups: groupIDs}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		result.Error = "falha ao montar client impersonado: " + err.Error()
		return result
	}

	review := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
	}
	rulesResult, err := clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		if isImpersonationForbidden(err) {
			result.Error = "servidor sem permissão de impersonate neste cluster"
		} else {
			result.Error = "falha ao consultar RBAC: " + err.Error()
		}
		return result
	}

	result.NamespaceAccess = &fleetNamespaceHit{AnyAccess: hasNonGenericAccess(rulesResult.Status.ResourceRules)}
	return result
}
