package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ─── Rollback de Deployment — "Modo Imagem" (troca só a tag da imagem, sem tocar no resto do
// manifesto) — item 1 do procedimento interno de rollback manual desta empresa, o método mais
// simples/rápido dos 4 modos: só válido quando NENHUM manifesto (ConfigMap/Ingress/Deployment/
// Service) foi alterado desde a versão-alvo — nesse caso reverter só a imagem já reverte 100% do
// comportamento da aplicação. Nunca oferecido pelo frontend pra Deployments Helm-gerenciados (ver
// DeploymentRollbackModal.tsx) — mesmo risco de drift já documentado pro Modo K8s nativo.

// SetDeploymentContainerImages troca a imagem de um ou mais containers do PodTemplateSpec — via
// patch estratégico (StrategicMergePatchType), equivalente a `kubectl set image
// deployment/X container=image:tag`. O merge key de `containers` é `name` (definido no schema da
// API do K8s), então o patch só afeta `.image` do container casado por nome — nunca sobrescreve
// env/ports/resources/volumeMounts dos containers, sejam os alterados ou os demais.
func (c *Client) SetDeploymentContainerImages(ctx context.Context, namespace, name string, images map[string]string) (*appsv1.Deployment, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("nenhuma imagem informada")
	}

	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
	}

	knownContainers := make(map[string]bool, len(dep.Spec.Template.Spec.Containers))
	for _, ctr := range dep.Spec.Template.Spec.Containers {
		knownContainers[ctr.Name] = true
	}
	for containerName := range images {
		if !knownContainers[containerName] {
			return nil, fmt.Errorf("container %q não existe neste Deployment", containerName)
		}
	}

	type containerImagePatch struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	patchContainers := make([]containerImagePatch, 0, len(images))
	for containerName, image := range images {
		patchContainers = append(patchContainers, containerImagePatch{Name: containerName, Image: image})
	}
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": patchContainers,
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patch: %w", err)
	}

	updated, err := c.clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to patch deployment image: %w", err)
	}
	return updated, nil
}
