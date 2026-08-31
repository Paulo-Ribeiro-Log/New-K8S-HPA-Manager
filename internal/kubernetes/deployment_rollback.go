package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// ─── Rollback de Deployment sem Helm ("Modo K8s nativo") — equivalente ao `kubectl rollout undo`,
// pedido explícito do usuário com o passo a passo `kubectl` como referência (histórico de revisões
// via `rollout history`, undo via `rollout undo --to-revision`, acompanhamento via `rollout
// status`). Cobre Deployments não gerenciados pelo Helm (Kustomize/`kubectl apply` puro) — pra
// Deployments Helm-gerenciados, o rollback correto é via `helm rollback` (endpoint já existente em
// internal/pkg/helm), nunca este caminho, que causaria drift (Helm nunca ficaria sabendo do patch).
//
// Mecanismo: o K8s não guarda um "histórico" à parte — o controller de Deployment mantém um
// ReplicaSet por revisão (escalado a 0 quando não é o atual), cada um com a annotation
// `deployment.kubernetes.io/revision` e o PodTemplateSpec exato daquela revisão. "Undo pra revisão
// N" é simplesmente copiar `Spec.Template` daquele ReplicaSet de volta pro Deployment — é
// literalmente o que `kubectl rollout undo` faz por baixo. Só existem revisões dentro de
// `spec.revisionHistoryLimit` (default 10) — as demais já foram removidas pelo garbage collector.

const (
	deploymentRevisionAnnotation    = "deployment.kubernetes.io/revision"
	deploymentChangeCauseAnnotation = "kubernetes.io/change-cause"
)

// DeploymentRevision é uma revisão histórica reconstruída a partir de um ReplicaSet vivo.
type DeploymentRevision struct {
	Revision    int64     `json:"revision"`
	ReplicaSet  string    `json:"replicaSet"`
	ChangeCause string    `json:"changeCause,omitempty"`
	Images      []string  `json:"images"`
	Replicas    int32     `json:"replicas"` // spec.replicas do ReplicaSet — desejado NAQUELA revisão, não o atual
	CreatedAt   time.Time `json:"createdAt"`
	IsCurrent   bool      `json:"isCurrent"`
	// RestartedAt — annotation kubectl.kubernetes.io/restartedAt do PodTemplateSpec DESTA revisão
	// (ver deployment_insights.go). Um valor presente indica que essa revisão especificamente foi
	// criada por um `kubectl rollout restart`/"Rollout Restart" da aplicação, não por mudança de
	// imagem/config — útil pra distinguir "revisão de restart" de "revisão de deploy" na lista.
	RestartedAt *time.Time `json:"restartedAt,omitempty"`
}

// findOwnedReplicaSets lista os ReplicaSets do namespace cujo OwnerReference aponta pro Deployment
// (por UID — nunca por nome/label selector, que pode colidir entre recursos diferentes).
func (c *Client) findOwnedReplicaSets(ctx context.Context, namespace string, dep *appsv1.Deployment) ([]appsv1.ReplicaSet, error) {
	rsList, err := c.clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list replicasets in %s/%s: %w", c.cluster, namespace, err)
	}
	owned := make([]appsv1.ReplicaSet, 0, len(rsList.Items))
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.UID == dep.UID {
				owned = append(owned, rs)
				break
			}
		}
	}
	return owned, nil
}

func replicaSetRevision(rs *appsv1.ReplicaSet) int64 {
	v, _ := strconv.ParseInt(rs.Annotations[deploymentRevisionAnnotation], 10, 64)
	return v
}

func replicaSetImages(rs *appsv1.ReplicaSet) []string {
	images := make([]string, 0, len(rs.Spec.Template.Spec.Containers))
	for _, ctr := range rs.Spec.Template.Spec.Containers {
		images = append(images, ctr.Image)
	}
	return images
}

// ListDeploymentRevisions — equivalente a `kubectl rollout history deployment/X`. Retorna sempre
// ordenado por revisão decrescente (mais recente primeiro), nunca nil (slice vazio quando não há
// nenhum ReplicaSet retido — Deployment recém-criado ou revisionHistoryLimit=0).
func (c *Client) ListDeploymentRevisions(ctx context.Context, namespace, name string) ([]DeploymentRevision, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
	}
	currentRevision := replicaSetRevisionFromAnnotations(dep.Annotations)

	owned, err := c.findOwnedReplicaSets(ctx, namespace, dep)
	if err != nil {
		return nil, err
	}

	revisions := make([]DeploymentRevision, 0, len(owned))
	for i := range owned {
		rs := &owned[i]
		rev := replicaSetRevision(rs)
		if rev == 0 {
			continue // ReplicaSet sem annotation de revisão reconhecível — não deveria acontecer, mas é defensivo
		}
		replicas := int32(0)
		if rs.Spec.Replicas != nil {
			replicas = *rs.Spec.Replicas
		}
		revisions = append(revisions, DeploymentRevision{
			Revision:    rev,
			ReplicaSet:  rs.Name,
			ChangeCause: rs.Spec.Template.Annotations[deploymentChangeCauseAnnotation],
			Images:      replicaSetImages(rs),
			Replicas:    replicas,
			CreatedAt:   rs.CreationTimestamp.Time,
			IsCurrent:   rev == currentRevision,
			RestartedAt: parsePodRestartedAt(rs.Spec.Template.Annotations),
		})
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	return revisions, nil
}

// replicaSetRevisionFromAnnotations lê a revisão ATUAL do próprio Deployment (annotation copiada
// do ReplicaSet ativo pelo controller) — usada só pra marcar IsCurrent, nunca como fonte de
// rollback em si (o alvo do rollback sempre vem de um ReplicaSet real).
func replicaSetRevisionFromAnnotations(annotations map[string]string) int64 {
	v, _ := strconv.ParseInt(annotations[deploymentRevisionAnnotation], 10, 64)
	return v
}

// GetDeploymentRevisionPreviewYAML monta o YAML do Deployment COMO FICARIA se revertido pra
// `targetRevision` — pega o Deployment atual e troca só `spec.template` pelo do ReplicaSet daquela
// revisão, sem persistir nada. Existe pra alimentar o diff visual (atual vs. proposto) que o modal
// de rollback exige ANTES de liberar a confirmação — nunca aplica nada sozinho.
func (c *Client) GetDeploymentRevisionPreviewYAML(ctx context.Context, namespace, name string, targetRevision int64) (string, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
	}
	owned, err := c.findOwnedReplicaSets(ctx, namespace, dep)
	if err != nil {
		return "", err
	}
	var target *appsv1.ReplicaSet
	for i := range owned {
		if replicaSetRevision(&owned[i]) == targetRevision {
			target = &owned[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("revisão %d não encontrada (pode ter saído da janela de revisionHistoryLimit)", targetRevision)
	}

	preview := dep.DeepCopy()
	preview.Spec.Template = *target.Spec.Template.DeepCopy()
	preview.ManagedFields = nil
	preview.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	preview.Status = appsv1.DeploymentStatus{}

	yamlBytes, err := yaml.Marshal(preview)
	if err != nil {
		return "", fmt.Errorf("failed to marshal preview: %w", err)
	}
	return string(yamlBytes), nil
}

// ErrRollbackSameRevision — achado explicitamente pra nunca deixar o usuário "reverter" pra onde
// já está (mesmo guard já usado no HelmRollbackModal.tsx existente).
type ErrRollbackSameRevision struct{ Revision int64 }

func (e *ErrRollbackSameRevision) Error() string {
	return fmt.Sprintf("o Deployment já está na revisão %d — nada para reverter", e.Revision)
}

// RollbackDeploymentToRevision — equivalente a `kubectl rollout undo --to-revision=N`. Sempre
// re-busca o Deployment (nunca reaproveita um objeto obtido antes no fluxo do chamador) — proteção
// contra corrida: se outra pessoa aplicou um deploy novo entre o usuário abrir o modal e confirmar,
// a checagem de "já está nesta revisão" reflete o estado REAL no instante do apply, não um
// snapshot antigo. Grava `changeCause` no PodTemplateSpec (não no metadata do Deployment) — é de
// lá que o controller copia pra dentro do PRÓXIMO ReplicaSet, o mesmo lugar que `kubectl rollout
// history` sempre leu.
func (c *Client) RollbackDeploymentToRevision(ctx context.Context, namespace, name string, targetRevision int64, changeCause string) (*appsv1.Deployment, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
	}

	currentRevision := replicaSetRevisionFromAnnotations(dep.Annotations)
	if currentRevision != 0 && currentRevision == targetRevision {
		return nil, &ErrRollbackSameRevision{Revision: targetRevision}
	}

	owned, err := c.findOwnedReplicaSets(ctx, namespace, dep)
	if err != nil {
		return nil, err
	}
	var target *appsv1.ReplicaSet
	for i := range owned {
		if replicaSetRevision(&owned[i]) == targetRevision {
			target = &owned[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("revisão %d não encontrada (pode ter saído da janela de revisionHistoryLimit)", targetRevision)
	}

	dep.Spec.Template = *target.Spec.Template.DeepCopy()
	if changeCause != "" {
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations[deploymentChangeCauseAnnotation] = changeCause
	}

	updated, err := c.clientset.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to rollback deployment %s/%s/%s to revision %d: %w", c.cluster, namespace, name, targetRevision, err)
	}
	return updated, nil
}

// DeploymentRolloutStatus é o snapshot reportado a cada tick do polling pós-rollback (ver
// WaitDeploymentRolloutComplete) — mesma informação que `kubectl rollout status` mostra.
type DeploymentRolloutStatus struct {
	UpdatedReplicas   int32 `json:"updatedReplicas"`
	AvailableReplicas int32 `json:"availableReplicas"`
	Replicas          int32 `json:"replicas"`
	DesiredReplicas   int32 `json:"desiredReplicas"`
	Complete          bool  `json:"complete"`
}

// WaitDeploymentRolloutComplete faz polling (mesmo princípio de `kubectl rollout status`, que
// também é só um watch/poll) até o rollout terminar — critério idêntico ao usado pelo próprio
// kubectl: geração observada em dia, réplicas atualizadas e disponíveis batendo com o desejado, e
// nenhuma réplica "velha" sobrando. `onTick` é chamado a cada iteração (inclusive a primeira,
// antes de qualquer sleep) pra alimentar o streaming SSE do chamador sem silêncio.
func (c *Client) WaitDeploymentRolloutComplete(ctx context.Context, namespace, name string, onTick func(DeploymentRolloutStatus)) error {
	const pollInterval = 2 * time.Second
	deadline := time.Now().Add(5 * time.Minute)

	for {
		dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("deployment %s/%s/%s não existe mais", c.cluster, namespace, name)
			}
			return fmt.Errorf("failed to poll deployment %s/%s/%s: %w", c.cluster, namespace, name, err)
		}

		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		status := DeploymentRolloutStatus{
			UpdatedReplicas:   dep.Status.UpdatedReplicas,
			AvailableReplicas: dep.Status.AvailableReplicas,
			Replicas:          dep.Status.Replicas,
			DesiredReplicas:   desired,
		}
		status.Complete = dep.Status.ObservedGeneration >= dep.Generation &&
			status.UpdatedReplicas >= desired &&
			status.AvailableReplicas >= desired &&
			status.Replicas == status.UpdatedReplicas

		if onTick != nil {
			onTick(status)
		}
		if status.Complete {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout aguardando rollout de %s/%s/%s (5min) — verifique eventos do namespace pra diagnosticar", c.cluster, namespace, name)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
