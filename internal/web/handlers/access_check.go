package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/rbac"
)

// vvCloudGroupPrefix é o prefixo dos grupos AAD que concedem acesso a workloads dos clusters.
const vvCloudGroupPrefix = "VV_CLOUD_"

// AccessCheckHandler verifica, via impersonation nativa do K8s, se um analista (e-mail)
// tem acesso a um cluster/namespace — reproduz `kubectl auth can-i --as <email>`.
type AccessCheckHandler struct {
	kubeManager    *config.KubeConfigManager
	aadLookup      *rbac.AADGroupLookup
	historyTracker *history.HistoryTracker
}

// NewAccessCheckHandler cria o handler de verificação de acesso.
func NewAccessCheckHandler(kubeManager *config.KubeConfigManager, historyTracker *history.HistoryTracker) *AccessCheckHandler {
	return &AccessCheckHandler{
		kubeManager:    kubeManager,
		aadLookup:      rbac.NewAADGroupLookup(vvCloudGroupPrefix),
		historyTracker: historyTracker,
	}
}

// matchedGroupDTO é o formato exposto ao frontend para cada grupo AAD confirmado.
type matchedGroupDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// groupResolution agrega tudo que veio da resolução de grupos AAD do e-mail — usado para
// montar a impersonation e também para exibir ao SRE (aba "Todos os Grupos AAD").
type groupResolution struct {
	matched []matchedGroupDTO // subconjunto com prefixo vvCloudGroupPrefix — usado no --as-group
	all     []matchedGroupDTO // TODOS os grupos do e-mail, sem filtro — só para exibição
	err     string
}

// buildImpersonatedClient resolve os grupos do e-mail (filtrando o prefixo configurado
// para a impersonation, mas retornando a lista completa também) e monta um clientset K8s
// impersonando esse usuário (+ grupos), reaproveitando toda a lógica de auth por provider
// já resolvida em kubeManager.GetRestConfig.
func (h *AccessCheckHandler) buildImpersonatedClient(ctx context.Context, cluster, email string) (kubernetes.Interface, groupResolution) {
	cfg, err := h.kubeManager.GetRestConfig(cluster)
	if err != nil {
		return nil, groupResolution{}
	}

	var resolution groupResolution

	allGroups, err := h.aadLookup.GetAllGroups(ctx, email)
	if err != nil {
		resolution.err = err.Error()
	}

	groupIDs := make([]string, 0, len(allGroups))
	for _, g := range allGroups {
		dto := matchedGroupDTO{ID: g.ID, DisplayName: g.DisplayName}
		resolution.all = append(resolution.all, dto)
		if strings.HasPrefix(g.DisplayName, vvCloudGroupPrefix) {
			groupIDs = append(groupIDs, g.ID)
			resolution.matched = append(resolution.matched, dto)
		}
	}

	cfg.Impersonate = rest.ImpersonationConfig{
		UserName: email,
		Groups:   groupIDs,
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, resolution
	}

	return clientset, resolution
}

// isImpersonationForbidden detecta o erro específico de falta de permissão `impersonate`
// na identidade do servidor — distinto de "o analista não tem acesso ao recurso".
func isImpersonationForbidden(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.IsForbidden(err) && strings.Contains(err.Error(), "impersonate")
}

// GetRules retorna as regras de acesso do analista num cluster/namespace, equivalente a
// `kubectl auth can-i --list -n <ns> --as <email>`.
//
// GET /api/v1/access-check/rules?cluster=&namespace=&email=
func (h *AccessCheckHandler) GetRules(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	email := strings.TrimSpace(c.Query("email"))
	if cluster == "" || namespace == "" || email == "" {
		c.JSON(400, errorResponse("MISSING_PARAMS", "parâmetros 'cluster', 'namespace' e 'email' são obrigatórios"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	clientset, groups := h.buildImpersonatedClient(ctx, cluster, email)
	if clientset == nil {
		c.JSON(500, errorResponse("CLUSTER_UNREACHABLE", "falha ao conectar no cluster "+cluster))
		return
	}

	review := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
	}
	result, err := clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})

	h.logAccessCheck(c, cluster, namespace, email, "", "", err)

	if err != nil {
		if isImpersonationForbidden(err) {
			c.JSON(403, errorResponse("IMPERSONATION_NOT_ALLOWED",
				"o servidor não tem permissão para impersonar usuários neste cluster — solicite ao time de infra um ClusterRoleBinding com verbo 'impersonate' sobre 'users'/'groups'"))
			return
		}
		c.JSON(500, errorResponse("RULES_REVIEW_FAILED", "falha ao consultar permissões: "+err.Error()))
		return
	}

	c.JSON(200, gin.H{
		"resourceRules":         result.Status.ResourceRules,
		"nonResourceRules":      result.Status.NonResourceRules,
		"incomplete":            result.Status.Incomplete,
		"matchedGroups":         groups.matched,
		"allGroups":             groups.all,
		"groupsResolutionError": groups.err,
	})
}

// CanI verifica um verbo/recurso pontual, equivalente a
// `kubectl auth can-i <verb> <resource> -n <ns> --as <email>`.
//
// GET /api/v1/access-check/can-i?cluster=&namespace=&email=&verb=&group=&resource=&subresource=&name=
func (h *AccessCheckHandler) CanI(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	email := strings.TrimSpace(c.Query("email"))
	verb := c.Query("verb")
	resource := c.Query("resource")
	if cluster == "" || namespace == "" || email == "" || verb == "" || resource == "" {
		c.JSON(400, errorResponse("MISSING_PARAMS", "parâmetros 'cluster', 'namespace', 'email', 'verb' e 'resource' são obrigatórios"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	clientset, groups := h.buildImpersonatedClient(ctx, cluster, email)
	if clientset == nil {
		c.JSON(500, errorResponse("CLUSTER_UNREACHABLE", "falha ao conectar no cluster "+cluster))
		return
	}

	sar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace:   namespace,
				Verb:        verb,
				Group:       c.Query("group"),
				Resource:    resource,
				Subresource: c.Query("subresource"),
				Name:        c.Query("name"),
			},
		},
	}
	result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})

	h.logAccessCheck(c, cluster, namespace, email, verb, resource, err)

	if err != nil {
		if isImpersonationForbidden(err) {
			c.JSON(403, errorResponse("IMPERSONATION_NOT_ALLOWED",
				"o servidor não tem permissão para impersonar usuários neste cluster — solicite ao time de infra um ClusterRoleBinding com verbo 'impersonate' sobre 'users'/'groups'"))
			return
		}
		c.JSON(500, errorResponse("ACCESS_REVIEW_FAILED", "falha ao verificar acesso: "+err.Error()))
		return
	}

	c.JSON(200, gin.H{
		"allowed":               result.Status.Allowed,
		"denied":                result.Status.Denied,
		"reason":                result.Status.Reason,
		"matchedGroups":         groups.matched,
		"allGroups":             groups.all,
		"groupsResolutionError": groups.err,
	})
}

// logAccessCheck registra a consulta no HistoryTracker — é uma checagem sensível sobre
// permissões de terceiros, vale trilha de auditoria de quem consultou o quê.
func (h *AccessCheckHandler) logAccessCheck(c *gin.Context, cluster, namespace, email, verb, resource string, opErr error) {
	if h.historyTracker == nil {
		return
	}
	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}

	entry := CreateHistoryEntry(c, "access_check", namespace, cluster, status, nil, map[string]interface{}{
		"email_analisado": email,
		"verb":            verb,
		"resource":        resource,
	}, 0, errMsg)
	h.historyTracker.Log(entry)
}
