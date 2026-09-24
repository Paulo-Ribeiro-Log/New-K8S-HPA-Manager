package finops

import (
	"context"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// listPageSize limita o tamanho de cada página das listagens cluster-wide do FinOps.
//
// Motivo: restConfig.Timeout (30s, ver config/kubeconfig.go) vale para a requisição INTEIRA,
// incluindo a leitura do corpo. Em cluster grande via VPN, um único List de todos os Pods ou
// ReplicaSets (RS carrega o pod template completo, ×revisionHistoryLimit por Deployment) passa
// de dezenas de MB e estoura o timeout no meio da leitura — "unexpected error when reading
// response body ... context deadline exceeded". Paginando, cada página é uma requisição
// própria com seu próprio orçamento de 30s.
const listPageSize = 500

// listAllPaged percorre todas as páginas de um List (Limit/Continue). Se o continue token
// expirar (410 Gone — etcd compactou no meio da paginação), recomeça do zero uma única vez.
func listAllPaged[T any](
	ctx context.Context,
	opts metav1.ListOptions,
	list func(context.Context, metav1.ListOptions) ([]T, string, error),
) ([]T, error) {
	for attempt := 0; ; attempt++ {
		var all []T
		o := opts
		o.Limit = listPageSize
		o.Continue = ""
		var err error
		for {
			var items []T
			var next string
			items, next, err = list(ctx, o)
			if err != nil {
				break
			}
			all = append(all, items...)
			if next == "" {
				return all, nil
			}
			o.Continue = next
		}
		if attempt == 0 && apierrors.IsResourceExpired(err) {
			log.Warn().Err(err).Msg("FinOps: continue token expirou durante a paginação — recomeçando a listagem")
			continue
		}
		return nil, err
	}
}

func listAllPods(ctx context.Context, client kubernetes.Interface, opts metav1.ListOptions) ([]corev1.Pod, error) {
	return listAllPaged(ctx, opts, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Pod, string, error) {
		l, err := client.CoreV1().Pods("").List(ctx, o)
		if err != nil {
			return nil, "", err
		}
		return l.Items, l.Continue, nil
	})
}

func listAllReplicaSets(ctx context.Context, client kubernetes.Interface) ([]appsv1.ReplicaSet, error) {
	return listAllPaged(ctx, metav1.ListOptions{}, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.ReplicaSet, string, error) {
		l, err := client.AppsV1().ReplicaSets("").List(ctx, o)
		if err != nil {
			return nil, "", err
		}
		return l.Items, l.Continue, nil
	})
}
