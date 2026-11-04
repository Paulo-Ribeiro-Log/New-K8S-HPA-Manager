package finops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	hpaListConcurrency = 8
	hpaDeniedSample    = 5 // quantos namespaces negados aparecem no aviso
)

// listHPAs lista os HPAs do cluster para o relatório.
//
// Primeiro tenta o escopo de cluster (uma chamada). Se ele for negado (Forbidden), o que é comum
// quando o IAM/RBAC dá `list` de HPA só por namespace (no GKE, container.horizontalPodAutoscalers.list
// ausente), tenta namespace a namespace nos `namespaces` informados. Namespaces também negados ficam
// sem dado de HPA e entram no aviso devolvido: sem isso o relatório inteiro falhava por causa de um
// enriquecimento (HPA mín/máx/atual) que não é essencial para custo e capacidade.
//
// Qualquer erro que não seja Forbidden continua sendo erro.
func listHPAs(ctx context.Context, client kubernetes.Interface, namespaces []string) ([]autoscalingv2.HorizontalPodAutoscaler, []string, error) {
	list, err := client.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err == nil {
		return list.Items, nil, nil
	}
	if !apierrors.IsForbidden(err) {
		return nil, nil, err
	}
	log.Warn().Err(err).Int("namespaces", len(namespaces)).
		Msg("FinOps: listagem de HPAs no escopo de cluster negada — tentando por namespace")
	return listHPAsByNamespace(ctx, client, namespaces)
}

func listHPAsByNamespace(ctx context.Context, client kubernetes.Interface, namespaces []string) ([]autoscalingv2.HorizontalPodAutoscaler, []string, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		items    []autoscalingv2.HorizontalPodAutoscaler
		denied   []string
		firstErr error
	)
	sem := make(chan struct{}, hpaListConcurrency)
	for _, ns := range namespaces {
		ns := ns
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			l, err := client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				items = append(items, l.Items...)
			case apierrors.IsForbidden(err):
				denied = append(denied, ns)
			case firstErr == nil:
				firstErr = fmt.Errorf("listar HPAs do namespace %s: %w", ns, err)
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	sort.Strings(denied)
	return items, hpaDeniedWarnings(denied, len(namespaces)), nil
}

// hpaDeniedWarnings monta o aviso mostrado no relatório; vazio quando nada foi negado.
func hpaDeniedWarnings(denied []string, total int) []string {
	if len(denied) == 0 {
		return nil
	}
	const hint = "Peça permissão de list em horizontalpodautoscalers (RBAC do cluster ou, no GKE, container.horizontalPodAutoscalers.list)."
	if len(denied) == total {
		return []string{fmt.Sprintf(
			"Sem permissão para listar HPAs, nem por namespace: os %d namespaces analisados ficam sem dados de HPA (mín/máx/atual), "+
				"então workloads escalados por HPA aparecem como se não tivessem. %s", total, hint)}
	}
	sample := denied
	more := ""
	if len(sample) > hpaDeniedSample {
		sample = sample[:hpaDeniedSample]
		more = fmt.Sprintf(" e mais %d", len(denied)-hpaDeniedSample)
	}
	return []string{fmt.Sprintf(
		"Sem permissão para listar HPAs em %d de %d namespaces (%s%s): esses workloads aparecem sem dados de HPA. %s",
		len(denied), total, strings.Join(sample, ", "), more, hint)}
}
