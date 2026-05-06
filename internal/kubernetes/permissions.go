package kubernetes

import (
	"context"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NamespacePermissions representa o que o usuário atual pode fazer num namespace específico,
// conforme reportado pelo K8s via SelfSubjectRulesReview. É a fonte de verdade — não depende
// de grupos AD, apenas do que o RBAC do cluster permite para o token em uso.
type NamespacePermissions struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`

	// HPAs
	CanListHPA   bool `json:"canListHPA"`
	CanGetHPA    bool `json:"canGetHPA"`
	CanUpdateHPA bool `json:"canUpdateHPA"` // update ou patch = pode escalar

	// Pods
	CanListPods bool `json:"canListPods"`
	CanExecPods bool `json:"canExecPods"`
	CanViewLogs bool `json:"canViewLogs"`

	// Deployments
	CanListDeployments  bool `json:"canListDeployments"`
	CanUpdateDeployment bool `json:"canUpdateDeployment"`

	// Secrets
	CanWriteSecrets bool `json:"canWriteSecrets"`

	// ConfigMaps
	CanWriteConfigMaps bool `json:"canWriteConfigMaps"`

	// CronJobs
	CanWriteCronJobs bool `json:"canWriteCronJobs"`

	// Services
	CanWriteServices bool `json:"canWriteServices"`

	// Ingress
	CanWriteIngress bool `json:"canWriteIngress"`

	// StatefulSets
	CanWriteStatefulSets bool `json:"canWriteStatefulSets"`

	// DaemonSets
	CanWriteDaemonSets bool `json:"canWriteDaemonSets"`

	// Pods (write = delete/kill/restart)
	CanWritePods bool `json:"canWritePods"`

	// Indica que o K8s não conseguiu listar todas as regras (wildcard policies complexas).
	// Quando true, assume-se acesso total para não bloquear o usuário indevidamente.
	Incomplete bool `json:"incomplete"`
}

// CheckNamespacePermissions chama SelfSubjectRulesReview no namespace indicado e
// retorna o que o usuário atual (baseado no kubeconfig do servidor) pode fazer.
func CheckNamespacePermissions(ctx context.Context, client kubernetes.Interface, cluster, namespace string) (*NamespacePermissions, error) {
	review := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{
			Namespace: namespace,
		},
	}

	result, err := client.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	perms := &NamespacePermissions{
		Cluster:    cluster,
		Namespace:  namespace,
		Incomplete: result.Status.Incomplete,
	}

	// Se o K8s não conseguiu listar todas as regras, assume acesso total para não bloquear.
	if result.Status.Incomplete {
		perms.CanListHPA = true
		perms.CanGetHPA = true
		perms.CanUpdateHPA = true
		perms.CanListPods = true
		perms.CanExecPods = true
		perms.CanViewLogs = true
		perms.CanListDeployments = true
		perms.CanUpdateDeployment = true
		perms.CanWriteSecrets = true
		perms.CanWriteConfigMaps = true
		perms.CanWriteCronJobs = true
		perms.CanWriteServices = true
		perms.CanWriteIngress = true
		perms.CanWriteStatefulSets = true
		perms.CanWriteDaemonSets = true
		perms.CanWritePods = true
		return perms, nil
	}

	for _, rule := range result.Status.ResourceRules {
		applyResourceRule(rule, perms)
	}
	return perms, nil
}

// applyResourceRule interpreta uma ResourceRule do SelfSubjectRulesReview e preenche
// os campos booleanos de NamespacePermissions. Trata wildcards em verbs, resources e apiGroups.
func applyResourceRule(rule authorizationv1.ResourceRule, p *NamespacePermissions) {
	hasVerb := func(target string) bool {
		for _, v := range rule.Verbs {
			if v == "*" || v == target {
				return true
			}
		}
		return false
	}
	hasResource := func(target string) bool {
		for _, r := range rule.Resources {
			if r == "*" || r == target {
				return true
			}
		}
		return false
	}
	inGroup := func(targets ...string) bool {
		for _, g := range rule.APIGroups {
			if g == "*" {
				return true
			}
			for _, t := range targets {
				if g == t {
					return true
				}
			}
		}
		return false
	}

	// HPAs — autoscaling/v1, autoscaling/v2
	if inGroup("autoscaling", "") && hasResource("horizontalpodautoscalers") {
		if hasVerb("list") || hasVerb("watch") {
			p.CanListHPA = true
		}
		if hasVerb("get") {
			p.CanGetHPA = true
		}
		if hasVerb("update") || hasVerb("patch") {
			p.CanUpdateHPA = true
		}
	}

	// Pods — core group ("")
	if inGroup("") && hasResource("pods") {
		if hasVerb("list") || hasVerb("watch") {
			p.CanListPods = true
		}
	}
	if inGroup("") && hasResource("pods/exec") {
		if hasVerb("create") {
			p.CanExecPods = true
		}
	}
	if inGroup("") && hasResource("pods/log") {
		if hasVerb("get") {
			p.CanViewLogs = true
		}
	}

	// Deployments — apps group
	if inGroup("apps") && hasResource("deployments") {
		if hasVerb("list") || hasVerb("watch") {
			p.CanListDeployments = true
		}
		if hasVerb("update") || hasVerb("patch") {
			p.CanUpdateDeployment = true
		}
	}

	// Secrets — core group
	if inGroup("") && hasResource("secrets") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteSecrets = true
		}
	}

	// ConfigMaps — core group
	if inGroup("") && hasResource("configmaps") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteConfigMaps = true
		}
	}

	// CronJobs — batch group
	if inGroup("batch") && hasResource("cronjobs") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteCronJobs = true
		}
	}

	// Services — core group
	if inGroup("") && hasResource("services") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteServices = true
		}
	}

	// Ingress — networking.k8s.io group
	if inGroup("networking.k8s.io") && hasResource("ingresses") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteIngress = true
		}
	}

	// StatefulSets — apps group
	if inGroup("apps") && hasResource("statefulsets") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteStatefulSets = true
		}
	}

	// DaemonSets — apps group
	if inGroup("apps") && hasResource("daemonsets") {
		if hasVerb("update") || hasVerb("patch") || hasVerb("create") || hasVerb("delete") {
			p.CanWriteDaemonSets = true
		}
	}

	// Pods write (delete = restart/kill) — core group
	if inGroup("") && hasResource("pods") {
		if hasVerb("delete") || hasVerb("update") || hasVerb("patch") || hasVerb("create") {
			p.CanWritePods = true
		}
	}
}
