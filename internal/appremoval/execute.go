package appremoval

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrInvalidTarget: item pedido não faz parte do scan atual, não é removível ou nada foi selecionado.
	ErrInvalidTarget = errors.New("seleção inválida")
	// ErrStale: o recurso foi recriado depois do scan que o usuário viu (UID diferente).
	ErrStale = errors.New("o scan está desatualizado")
	// ErrNotFoundInCluster: o recurso já não existe (ao remover, conta como já removido).
	ErrNotFoundInCluster = errors.New("recurso não encontrado")
)

// Target é um item escolhido pelo usuário. UID é o que o scan mostrou (precondition).
type Target struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// Status de cada item da execução.
const (
	StatusDeleted   = "deleted"
	StatusSimulated = "simulated"
	StatusNotFound  = "not_found"
	StatusSkipped   = "skipped"
	StatusFailed    = "failed"
)

// Outcome é o resultado de um item.
type Outcome struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Runner executa as operações no cluster; trocável nos testes.
type Runner interface {
	// CurrentUID devolve o UID atual do recurso, ou ErrNotFoundInCluster.
	CurrentUID(ctx context.Context, cluster, namespace, kind, name string) (string, error)
	Delete(ctx context.Context, cluster, namespace, kind, name string, dryRun bool) error
	HelmUninstall(ctx context.Context, cluster, namespace, release string, dryRun bool) error
}

// Plan valida a seleção contra o scan atual (nunca confia no que veio do cliente) e devolve os
// alvos na ordem de remoção do scan. Itens removidos por um release Helm só podem sair pelo release.
func Plan(scan *Result, selected []Target) ([]Target, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: nenhum item selecionado", ErrInvalidTarget)
	}
	byKey := make(map[string]Item, len(scan.Items))
	for _, it := range scan.Items {
		byKey[key(it.Kind, it.Name)] = it
	}
	want := make(map[string]Target, len(selected))
	for _, t := range selected {
		k := key(t.Kind, t.Name)
		it, ok := byKey[k]
		if !ok {
			return nil, fmt.Errorf("%w: %s não faz parte do scan atual", ErrInvalidTarget, k)
		}
		if it.Kind != KindHelmRelease && resourceFor(it.Kind) == "" {
			return nil, fmt.Errorf("%w: tipo %s não é removível por esta ferramenta", ErrInvalidTarget, it.Kind)
		}
		if it.RemovedByHelm {
			return nil, fmt.Errorf("%w: %s é removido pelo release Helm, selecione o release", ErrInvalidTarget, k)
		}
		if it.UID != "" && t.UID != "" && it.UID != t.UID {
			return nil, fmt.Errorf("%w: %s foi recriado depois do scan", ErrStale, k)
		}
		want[k] = Target{Kind: it.Kind, Name: it.Name, UID: it.UID}
	}
	plan := make([]Target, 0, len(want))
	for _, it := range scan.Items {
		if t, ok := want[key(it.Kind, it.Name)]; ok {
			plan = append(plan, t)
		}
	}
	return plan, nil
}

// Run executa o plano em ordem. Para na primeira falha (apagar config com o workload ainda de
// pé deixaria a aplicação quebrada) e marca o restante como "skipped". `on` recebe cada resultado.
func Run(ctx context.Context, r Runner, cluster, namespace string, plan []Target, dryRun bool, on func(o Outcome, done, total int)) []Outcome {
	outcomes := make([]Outcome, 0, len(plan))
	emit := func(o Outcome) {
		outcomes = append(outcomes, o)
		if on != nil {
			on(o, len(outcomes), len(plan))
		}
	}
	for i, t := range plan {
		if ctx.Err() != nil {
			for _, rest := range plan[i:] {
				emit(Outcome{Kind: rest.Kind, Name: rest.Name, Status: StatusSkipped, Message: "execução interrompida"})
			}
			break
		}
		o := runOne(ctx, r, cluster, namespace, t, dryRun)
		emit(o)
		if o.Status == StatusFailed {
			for _, rest := range plan[i+1:] {
				emit(Outcome{Kind: rest.Kind, Name: rest.Name, Status: StatusSkipped,
					Message: fmt.Sprintf("não executado: falha em %s/%s", t.Kind, t.Name)})
			}
			break
		}
	}
	return outcomes
}

func runOne(ctx context.Context, r Runner, cluster, namespace string, t Target, dryRun bool) Outcome {
	o := Outcome{Kind: t.Kind, Name: t.Name}
	done := StatusDeleted
	if dryRun {
		done = StatusSimulated
	}

	if t.Kind == KindHelmRelease {
		if err := r.HelmUninstall(ctx, cluster, namespace, t.Name, dryRun); err != nil {
			o.Status, o.Message = StatusFailed, err.Error()
			return o
		}
		o.Status = done
		return o
	}

	uid, err := r.CurrentUID(ctx, cluster, namespace, t.Kind, t.Name)
	switch {
	case errors.Is(err, ErrNotFoundInCluster):
		o.Status, o.Message = StatusNotFound, "já não existe"
		return o
	case err != nil:
		o.Status, o.Message = StatusFailed, err.Error()
		return o
	case t.UID != "" && uid != "" && uid != t.UID:
		o.Status, o.Message = StatusSkipped, "recriado depois do scan (UID diferente)"
		return o
	}
	if err := r.Delete(ctx, cluster, namespace, t.Kind, t.Name, dryRun); err != nil {
		o.Status, o.Message = StatusFailed, err.Error()
		return o
	}
	o.Status = done
	return o
}
