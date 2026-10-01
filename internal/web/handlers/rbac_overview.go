package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
)

// RBAC por namespace/grupo (aba Namespaces): cruza as squads declaradas no namespace
// (label/annotation squads.devops.k8s.io/<id>, onde <id> costuma ser o object ID do grupo no
// Entra ID e o valor da annotation é o nome da squad) com os subjects das RoleBindings e
// ClusterRoleBindings. Uma única chamada devolve tudo; o frontend monta as visões "por
// namespace" e "por grupo". Só leitura.

const squadKeyPrefix = "squads.devops.k8s.io/"

type RBACOverviewHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewRBACOverviewHandler(km *config.KubeConfigManager) *RBACOverviewHandler {
	return &RBACOverviewHandler{kubeManager: km}
}

// RBACSquad é uma squad declarada num namespace.
type RBACSquad struct {
	Namespace    string `json:"namespace"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	InLabel      bool   `json:"in_label"`
	InAnnotation bool   `json:"in_annotation"`
}

type RBACBinding struct {
	Kind      string           `json:"kind"` // RoleBinding | ClusterRoleBinding
	Name      string           `json:"name"`
	Namespace string           `json:"namespace,omitempty"`
	RoleKind  string           `json:"role_kind"` // Role | ClusterRole
	RoleName  string           `json:"role_name"`
	Subjects  []rbacv1.Subject `json:"subjects"`
}

type RBACOverview struct {
	Namespaces []string                       `json:"namespaces"`
	Squads     []RBACSquad                    `json:"squads"`
	Bindings   []RBACBinding                  `json:"bindings"`
	Roles      map[string][]rbacv1.PolicyRule `json:"roles"` // chave: rbacRoleKey; ausente = role inexistente/ilegível
	Warnings   []string                       `json:"warnings"`
}

// rbacRoleKey: "ClusterRole/<nome>" ou "Role/<namespace>/<nome>" — mesmo formato no frontend.
func rbacRoleKey(kind, namespace, name string) string {
	if kind == "ClusterRole" {
		return "ClusterRole/" + name
	}
	return "Role/" + namespace + "/" + name
}

// GET /api/v1/rbac/overview?cluster=...
func (h *RBACOverviewHandler) Overview(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "MISSING_PARAMETER", "message": "Parameter 'cluster' is required"}})
		return
	}
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "CLIENT_ERROR", "message": fmt.Sprintf("Failed to get client: %v", err)}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	ov, err := buildRBACOverview(ctx, clientset)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "error": gin.H{"code": "RBAC_READ_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ov})
}

// buildRBACOverview lê namespaces, bindings e roles. Falha só se não conseguir ler nenhum tipo
// de binding; os demais erros (ex: sem permissão de listar Roles) viram warnings.
func buildRBACOverview(ctx context.Context, clientset kubernetes.Interface) (*RBACOverview, error) {
	ov := &RBACOverview{Namespaces: []string{}, Squads: []RBACSquad{}, Bindings: []RBACBinding{}, Roles: map[string][]rbacv1.PolicyRule{}, Warnings: []string{}}
	warn := func(what string, err error) { ov.Warnings = append(ov.Warnings, fmt.Sprintf("%s: %v", what, err)) }

	if nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err != nil {
		warn("listar namespaces", err)
	} else {
		for _, ns := range nsList.Items {
			ov.Namespaces = append(ov.Namespaces, ns.Name)
			ov.Squads = append(ov.Squads, extractSquads(ns.Name, ns.Labels, ns.Annotations)...)
		}
		sort.Strings(ov.Namespaces)
	}

	rbErr, crbErr := error(nil), error(nil)
	if rbs, err := clientset.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{}); err != nil {
		rbErr = err
		warn("listar RoleBindings", err)
	} else {
		for _, b := range rbs.Items {
			ov.Bindings = append(ov.Bindings, RBACBinding{Kind: "RoleBinding", Name: b.Name, Namespace: b.Namespace, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name, Subjects: nonNilSubjects(b.Subjects)})
		}
	}
	if crbs, err := clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err != nil {
		crbErr = err
		warn("listar ClusterRoleBindings", err)
	} else {
		for _, b := range crbs.Items {
			ov.Bindings = append(ov.Bindings, RBACBinding{Kind: "ClusterRoleBinding", Name: b.Name, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name, Subjects: nonNilSubjects(b.Subjects)})
		}
	}
	if rbErr != nil && crbErr != nil {
		return nil, fmt.Errorf("sem permissão para ler o RBAC do cluster (RoleBindings: %v; ClusterRoleBindings: %v)", rbErr, crbErr)
	}

	// Só as roles referenciadas por algum binding vão na resposta.
	referenced := map[string]bool{}
	for _, b := range ov.Bindings {
		referenced[rbacRoleKey(b.RoleKind, b.Namespace, b.RoleName)] = true
	}
	if crs, err := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{}); err != nil {
		warn("listar ClusterRoles", err)
	} else {
		for _, r := range crs.Items {
			if k := rbacRoleKey("ClusterRole", "", r.Name); referenced[k] {
				ov.Roles[k] = nonNilRules(r.Rules)
			}
		}
	}
	if roles, err := clientset.RbacV1().Roles("").List(ctx, metav1.ListOptions{}); err != nil {
		warn("listar Roles", err)
	} else {
		for _, r := range roles.Items {
			if k := rbacRoleKey("Role", r.Namespace, r.Name); referenced[k] {
				ov.Roles[k] = nonNilRules(r.Rules)
			}
		}
	}
	return ov, nil
}

// extractSquads lê as chaves squads.devops.k8s.io/<id> de labels e annotations. O nome vem da
// annotation (a label só tem valor vazio); quebras de linha do YAML viram espaço simples.
func extractSquads(namespace string, labels, annotations map[string]string) []RBACSquad {
	byID := map[string]*RBACSquad{}
	get := func(id string) *RBACSquad {
		if s, ok := byID[id]; ok {
			return s
		}
		s := &RBACSquad{Namespace: namespace, ID: id}
		byID[id] = s
		return s
	}
	for k := range labels {
		if id := strings.TrimPrefix(k, squadKeyPrefix); id != k && id != "" {
			get(id).InLabel = true
		}
	}
	for k, v := range annotations {
		if id := strings.TrimPrefix(k, squadKeyPrefix); id != k && id != "" {
			s := get(id)
			s.InAnnotation = true
			s.Name = strings.Join(strings.Fields(v), " ")
		}
	}
	out := make([]RBACSquad, 0, len(byID))
	for _, s := range byID {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func nonNilSubjects(s []rbacv1.Subject) []rbacv1.Subject {
	if s == nil {
		return []rbacv1.Subject{}
	}
	return s
}

func nonNilRules(r []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	if r == nil {
		return []rbacv1.PolicyRule{}
	}
	return r
}
