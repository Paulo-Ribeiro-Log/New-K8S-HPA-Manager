package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ─── Insights de runtime de um Deployment — enriquecimento sob demanda, nunca chamado no hot path
// de listagem. Motivado por uma investigação real: `ms-faturamento-nf-legado` estava com
// spec.replicas=0 há mais de 2 anos (desde o último deploy real), mas Service e Ingress
// continuavam existindo e apontando pra ele — qualquer requisição na rota bateria num backend sem
// nenhum pod (503 do ingress-controller), sem nenhum sinal disso visível na UI. Cobre duas
// perguntas que o troubleshooting manual via kubectl respondeu, mas que deveriam estar visíveis
// direto na aplicação: (1) quando foi o último `kubectl rollout restart` (annotation
// kubectl.kubernetes.io/restartedAt do pod template), e (2) existe alguma "rota fachada" — Service
// (e Ingress, quando houver) selecionando os pods deste Deployment sem nenhum endpoint pronto.

// podRestartedAtAnnotation — mesma annotation que RolloutRestart (client.go) já escreve ao
// reiniciar um Deployment/DaemonSet/StatefulSet via esta aplicação (e que `kubectl rollout
// restart` também escreve, de onde a convenção veio).
const podRestartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// DeploymentRuntimeInsights é o resultado do enriquecimento — usado tanto pelo painel de
// visualização (DeploymentsTab.tsx) quanto pelo modal de Rollback (DeploymentRollbackModal.tsx).
type DeploymentRuntimeInsights struct {
	RestartedAt    *time.Time      `json:"restartedAt,omitempty"`
	DanglingRoutes []DanglingRoute `json:"danglingRoutes,omitempty"`
}

// DanglingRoute descreve um Service (e, quando encontrados, os hosts de Ingress que apontam pra
// ele) que seleciona os pods deste Deployment mas não tem nenhum endpoint pronto no momento.
type DanglingRoute struct {
	ServiceName string   `json:"serviceName"`
	Hosts       []string `json:"hosts,omitempty"`
}

// GetDeploymentRuntimeInsights busca RestartedAt + rotas sem backend. Best-effort na parte de
// dangling routes (Services/Endpoints/Ingress) — uma falha nessas chamadas nunca derruba
// RestartedAt, que já vem do próprio Deployment já buscado com sucesso.
func (c *Client) GetDeploymentRuntimeInsights(ctx context.Context, namespace, name string) (*DeploymentRuntimeInsights, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
	}

	insights := &DeploymentRuntimeInsights{
		RestartedAt: parsePodRestartedAt(dep.Spec.Template.Annotations),
	}

	if len(dep.Spec.Template.Labels) == 0 {
		return insights, nil
	}

	svcList, err := c.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return insights, nil // best-effort
	}

	var matched []corev1.Service
	for _, svc := range svcList.Items {
		if serviceSelectorMatches(svc.Spec.Selector, dep.Spec.Template.Labels) {
			matched = append(matched, svc)
		}
	}
	if len(matched) == 0 {
		return insights, nil
	}

	var ingresses []networkingv1.Ingress
	if ingList, ingErr := c.clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{}); ingErr == nil {
		ingresses = ingList.Items
	}

	for _, svc := range matched {
		// EndpointSlice (discovery.k8s.io/v1), não a API clássica `v1 Endpoints` (deprecada desde
		// 1.33 — emite warning de log a cada chamada, confirmado ao vivo contra um cluster real
		// nesta investigação). Cada Service pode ter mais de 1 EndpointSlice (paginação nativa do
		// controller pra Services com muitos endpoints), por isso soma-se Ready em TODAS as
		// slices, nunca só a primeira.
		slices, sliceErr := c.clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "kubernetes.io/service-name=" + svc.Name,
		})
		if sliceErr != nil || len(slices.Items) == 0 {
			continue // sem EndpointSlice nenhuma pra este Service — sem sinal confiável, pula
		}
		readyCount := 0
		for _, slice := range slices.Items {
			for _, ep := range slice.Endpoints {
				if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
					readyCount++
				}
			}
		}
		if readyCount > 0 {
			continue
		}
		insights.DanglingRoutes = append(insights.DanglingRoutes, DanglingRoute{
			ServiceName: svc.Name,
			Hosts:       ingressHostsForService(ingresses, svc.Name),
		})
	}

	return insights, nil
}

// serviceSelectorMatches replica a lógica já usada por serviceIPsForLabels (client.go) — o
// selector do Service precisa ser subconjunto dos labels do pod template.
func serviceSelectorMatches(selector, podLabels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if podLabels[k] != v {
			return false
		}
	}
	return true
}

// ingressHostsForService varre as regras de cada Ingress do namespace procurando backends
// apontando pro Service informado, retornando os hosts correspondentes (deduplicados, na ordem
// de descoberta).
func ingressHostsForService(ingresses []networkingv1.Ingress, serviceName string) []string {
	seen := make(map[string]bool)
	var hosts []string
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	for _, ing := range ingresses {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil && path.Backend.Service.Name == serviceName {
					add(rule.Host)
				}
			}
		}
		if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil && ing.Spec.DefaultBackend.Service.Name == serviceName {
			add("(default backend)")
		}
	}
	return hosts
}

// parsePodRestartedAt lê a annotation kubectl.kubernetes.io/restartedAt (RFC3339, mesmo formato
// que RolloutRestart grava). Retorna nil quando ausente ou não parseável — nunca inventa um valor.
func parsePodRestartedAt(annotations map[string]string) *time.Time {
	raw := annotations[podRestartedAtAnnotation]
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &t
}
