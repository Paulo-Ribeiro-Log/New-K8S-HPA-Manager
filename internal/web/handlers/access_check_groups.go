package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/config"
)

// Grupos do Entra ID presentes num cluster (aba "Grupos do cluster" de Verificar Acesso): os
// administradores do AKS (aadProfile) e os grupos citados nos subjects das RoleBindings e
// ClusterRoleBindings. Para cada grupo resolve o nome e, sob demanda, os membros. Só leitura.

var guidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	groupSourceAKSAdmin = "aks-admin"
	groupSourceRBAC     = "rbac"
)

// ClusterGroupBinding resume uma binding que cita o grupo.
type ClusterGroupBinding struct {
	Kind      string `json:"kind"` // RoleBinding | ClusterRoleBinding
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	RoleKind  string `json:"role_kind"`
	RoleName  string `json:"role_name"`
}

// ClusterGroup é um grupo encontrado no cluster. DisplayName vazio = não resolvido no Entra ID
// (o subject pode ser um nome e não um Object ID, ou o ID não existe mais).
type ClusterGroup struct {
	ID          string                `json:"id"` // Object ID em minúsculas, ou o texto do subject quando não é GUID
	DisplayName string                `json:"displayName"`
	IsObjectID  bool                  `json:"isObjectId"`
	Builtin     bool                  `json:"builtin"` // grupo interno do Kubernetes (system:*), não é do Entra ID
	Risks       []ClusterGroupRisk    `json:"risks"`   // bindings fora do padrão num grupo interno aberto (ex.: system:unauthenticated)
	Sources     []string              `json:"sources"`
	Bindings    []ClusterGroupBinding `json:"bindings"`
}

// ClusterGroupsResult é a resposta de GET /access-check/cluster-groups.
type ClusterGroupsResult struct {
	Cluster  string         `json:"cluster"`
	Groups   []ClusterGroup `json:"groups"`
	Warnings []string       `json:"warnings"`
}

// mergeClusterGroups junta os admins do AKS e os subjects Group das bindings, uma entrada por
// grupo. A chave é o Object ID em minúsculas; subject que não é GUID (nome do grupo) mantém o
// texto como está.
func mergeClusterGroups(aksAdminIDs []string, bindings []ClusterGroupBinding, subjects [][]rbacv1.Subject) []ClusterGroup {
	byKey := map[string]*ClusterGroup{}
	get := func(raw string) *ClusterGroup {
		key := strings.TrimSpace(raw)
		isID := guidRe.MatchString(key)
		if isID {
			key = strings.ToLower(key)
		}
		g, ok := byKey[key]
		if !ok {
			g = &ClusterGroup{ID: key, IsObjectID: isID, Builtin: isBuiltinSubject(key), Sources: []string{}, Bindings: []ClusterGroupBinding{}, Risks: []ClusterGroupRisk{}}
			byKey[key] = g
		}
		return g
	}
	addSource := func(g *ClusterGroup, s string) {
		for _, x := range g.Sources {
			if x == s {
				return
			}
		}
		g.Sources = append(g.Sources, s)
	}

	for _, id := range aksAdminIDs {
		if strings.TrimSpace(id) != "" {
			addSource(get(id), groupSourceAKSAdmin)
		}
	}
	for i, b := range bindings {
		for _, s := range subjects[i] {
			// system:anonymous é um User, mas é a mesma porta aberta que system:unauthenticated.
			isAnonymous := s.Kind == rbacv1.UserKind && s.Name == "system:anonymous"
			if (s.Kind != rbacv1.GroupKind && !isAnonymous) || strings.TrimSpace(s.Name) == "" {
				continue
			}
			g := get(s.Name)
			addSource(g, groupSourceRBAC)
			g.Bindings = append(g.Bindings, b)
		}
	}

	out := make([]ClusterGroup, 0, len(byKey))
	for _, g := range byKey {
		g.Risks = bindingRisks(*g)
		out = append(out, *g)
	}
	return out
}

// isBuiltinSubject: grupos internos do Kubernetes (system:authenticated, system:masters...).
func isBuiltinSubject(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "system:")
}

// Roles que o próprio Kubernetes liga por padrão aos grupos abertos. Qualquer outra binding em
// system:unauthenticated / system:anonymous / system:authenticated é acesso concedido a quem não
// foi autorizado individualmente (anônimo, ou qualquer identidade válida do cluster).
var (
	defaultUnauthenticatedRoles = map[string]bool{"system:public-info-viewer": true}
	defaultAuthenticatedRoles   = map[string]bool{
		"system:basic-user": true, "system:discovery": true, "system:public-info-viewer": true,
		"system:service-account-issuer-discovery": true,
	}
	// Roles que só leem os endpoints de descoberta da API (/api, /apis, /version, /healthz):
	// não expõem nenhum dado do cluster.
	discoveryOnlyRoles = map[string]bool{
		"system:discovery": true, "system:basic-user": true, "system:public-info-viewer": true,
		"system:service-account-issuer-discovery": true,
	}
	writeAdminRoles = map[string]bool{"cluster-admin": true, "admin": true, "edit": true}
)

const (
	riskLow      = "baixo"
	riskHigh     = "alto"
	riskCritical = "crítico"
)

// ClusterGroupRisk é uma binding que dá permissão a um grupo aberto além do padrão do Kubernetes.
type ClusterGroupRisk struct {
	Severity string `json:"severity"` // baixo | alto | crítico
	Who      string `json:"who"`      // quem recebe o acesso, em português
	Binding  string `json:"binding"`  // ex.: "ClusterRoleBinding system:discovery"
	Role     string `json:"role"`     // ex.: "ClusterRole/system:discovery"
	Message  string `json:"message"`  // o que isso significa na prática
}

// bindingRisks devolve um item por binding fora do padrão nos grupos abertos a todos.
func bindingRisks(g ClusterGroup) []ClusterGroupRisk {
	var allowed map[string]bool
	var who string
	anonymous := false
	switch strings.ToLower(g.ID) {
	case "system:unauthenticated", "system:anonymous":
		allowed, who, anonymous = defaultUnauthenticatedRoles, "Anônimo (sem login)", true
	case "system:authenticated":
		allowed, who = defaultAuthenticatedRoles, "Qualquer usuário logado no cluster"
	default:
		return []ClusterGroupRisk{}
	}
	risks := []ClusterGroupRisk{}
	for _, b := range g.Bindings {
		if allowed[b.RoleName] {
			continue
		}
		where := b.Kind + " " + b.Name
		if b.Namespace != "" {
			where = b.Kind + " " + b.Namespace + "/" + b.Name
		}
		severity, msg := classifyOpenBinding(b, anonymous)
		risks = append(risks, ClusterGroupRisk{
			Severity: severity, Who: who, Binding: where, Role: b.RoleKind + "/" + b.RoleName, Message: msg,
		})
	}
	return risks
}

func classifyOpenBinding(b ClusterGroupBinding, anonymous bool) (severity, message string) {
	scope := "no cluster inteiro"
	if b.Namespace != "" {
		scope = "no namespace " + b.Namespace
	}
	switch {
	case discoveryOnlyRoles[b.RoleName]:
		msg := "A role só permite consultar os endpoints de descoberta da API (versão e lista de recursos), sem acesso a nenhum dado do cluster."
		if anonymous {
			msg += " Era o padrão do Kubernetes antes da 1.14; vale remover o grupo anônimo dessa binding."
		}
		return riskLow, msg
	case writeAdminRoles[b.RoleName]:
		return riskCritical, "A role permite criar, alterar e apagar recursos " + scope + ". Ninguém deveria ter isso sem autenticação individual."
	default:
		return riskHigh, "A role concede permissões sobre recursos " + scope + " sem autorização individual. Confira o que a role permite."
	}
}

// sortClusterGroups: admins do AKS primeiro, depois por nome (ou ID, quando sem nome).
func sortClusterGroups(gs []ClusterGroup) {
	label := func(g ClusterGroup) string {
		if g.DisplayName != "" {
			return strings.ToLower(g.DisplayName)
		}
		return strings.ToLower(g.ID)
	}
	isAdmin := func(g ClusterGroup) bool {
		for _, s := range g.Sources {
			if s == groupSourceAKSAdmin {
				return true
			}
		}
		return false
	}
	sort.SliceStable(gs, func(i, j int) bool {
		if ai, aj := isAdmin(gs[i]), isAdmin(gs[j]); ai != aj {
			return ai
		}
		return label(gs[i]) < label(gs[j])
	})
}

// listBindingGroups lê RoleBindings e ClusterRoleBindings; falha só se nenhum dos dois puder ser lido.
func listBindingGroups(ctx context.Context, cs kubernetes.Interface) ([]ClusterGroupBinding, [][]rbacv1.Subject, []string) {
	var (
		bindings []ClusterGroupBinding
		subjects [][]rbacv1.Subject
		warnings []string
	)
	if rbs, err := cs.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{}); err != nil {
		warnings = append(warnings, "listar RoleBindings: "+err.Error())
	} else {
		for _, b := range rbs.Items {
			bindings = append(bindings, ClusterGroupBinding{Kind: "RoleBinding", Name: b.Name, Namespace: b.Namespace, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name})
			subjects = append(subjects, b.Subjects)
		}
	}
	if crbs, err := cs.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err != nil {
		warnings = append(warnings, "listar ClusterRoleBindings: "+err.Error())
	} else {
		for _, b := range crbs.Items {
			bindings = append(bindings, ClusterGroupBinding{Kind: "ClusterRoleBinding", Name: b.Name, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name})
			subjects = append(subjects, b.Subjects)
		}
	}
	return bindings, subjects, warnings
}

// GetClusterGroups lista os grupos do Entra ID presentes no cluster.
//
// GET /api/v1/access-check/cluster-groups?cluster=
func (h *AccessCheckHandler) GetClusterGroups(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "parâmetro 'cluster' é obrigatório"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	result := ClusterGroupsResult{Cluster: cluster, Groups: []ClusterGroup{}, Warnings: []string{}}

	// Admins do AKS: só clusters do clusters-config.json (AKS). Falha vira aviso, a parte do
	// RBAC continua valendo.
	var aksAdmins []string
	if cc := h.kubeManager.GetClusterConfig(cluster); cc != nil {
		ids, err := h.aksAdminGroupIDs(ctx, cc)
		if err != nil {
			result.Warnings = append(result.Warnings, "grupos admin do AKS: "+err.Error())
		}
		aksAdmins = ids
	}

	var bindings []ClusterGroupBinding
	var subjects [][]rbacv1.Subject
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		result.Warnings = append(result.Warnings, "conectar no cluster: "+err.Error())
	} else {
		var warns []string
		bindings, subjects, warns = listBindingGroups(ctx, clientset)
		result.Warnings = append(result.Warnings, warns...)
	}

	groups := mergeClusterGroups(aksAdmins, bindings, subjects)

	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.IsObjectID {
			ids = append(ids, g.ID)
		}
	}
	names, err := h.groupDirectory.ResolveGroupNames(ctx, ids)
	if err != nil {
		result.Warnings = append(result.Warnings, "resolver nomes no Entra ID: "+err.Error())
	}
	for i := range groups {
		if info, ok := names[groups[i].ID]; ok {
			groups[i].DisplayName = info.DisplayName
		} else if !groups[i].IsObjectID {
			groups[i].DisplayName = groups[i].ID // subject por nome: o próprio texto é o nome
		}
	}
	sortClusterGroups(groups)
	result.Groups = groups

	if len(result.Groups) == 0 && len(result.Warnings) > 0 {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_GROUPS_FAILED", strings.Join(result.Warnings, "; ")))
		return
	}
	c.JSON(http.StatusOK, result)
}

// aksAdminGroupIDs lê o aadProfile do cluster no ARM, usando resource group e subscription do
// clusters-config.json.
func (h *AccessCheckHandler) aksAdminGroupIDs(ctx context.Context, cc *config.ClusterConfig) ([]string, error) {
	if cc.ResourceGroup == "" {
		return nil, errors.New("resource group do cluster não informado no clusters-config.json")
	}
	sub := cc.SubscriptionID
	if sub == "" {
		sub = cc.Subscription
	}
	arm, err := azure.NewARMClient(ctx, sub)
	if err != nil {
		return nil, err
	}
	return arm.AKSAdminGroupIDs(ctx, arm.SubscriptionID, cc.ResourceGroup, strings.TrimSuffix(cc.Name, "-admin"))
}

// GetGroupMembers lista os membros de um grupo pelo Object ID.
//
// GET /api/v1/access-check/group-members?id=<guid>[&transitive=1]
func (h *AccessCheckHandler) GetGroupMembers(c *gin.Context) {
	id := strings.TrimSpace(c.Query("id"))
	if !guidRe.MatchString(id) {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_GROUP_ID", "parâmetro 'id' deve ser o Object ID (GUID) do grupo"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	members, err := h.groupDirectory.GroupMembers(ctx, id, c.Query("transitive") == "1")
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("GROUP_MEMBERS_FAILED", err.Error()))
		return
	}
	c.JSON(http.StatusOK, members)
}
