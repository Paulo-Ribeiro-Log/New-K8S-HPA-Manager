package finops

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestListAllPaged_JuntaPaginas(t *testing.T) {
	pages := map[string][]int{"": {1, 2}, "p2": {3, 4}, "p3": {5}}
	next := map[string]string{"": "p2", "p2": "p3", "p3": ""}
	var calls int

	got, err := listAllPaged(context.Background(), metav1.ListOptions{FieldSelector: "x=y"},
		func(_ context.Context, o metav1.ListOptions) ([]int, string, error) {
			calls++
			if o.Limit != listPageSize || o.FieldSelector != "x=y" {
				t.Fatalf("opções não propagadas: %+v", o)
			}
			return pages[o.Continue], next[o.Continue], nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || calls != 3 {
		t.Fatalf("esperava 5 itens em 3 chamadas, veio %v em %d", got, calls)
	}
}

func TestListAllPaged_RecomecaUmaVezSeContinueExpira(t *testing.T) {
	expired := apierrors.NewResourceExpired("continue expirado")
	var calls int

	got, err := listAllPaged(context.Background(), metav1.ListOptions{},
		func(_ context.Context, o metav1.ListOptions) ([]int, string, error) {
			calls++
			switch {
			case calls == 1:
				return []int{1}, "p2", nil
			case calls == 2:
				return nil, "", expired // expira na 2ª página da 1ª tentativa
			case o.Continue == "":
				return []int{1}, "p2", nil
			default:
				return []int{2}, "", nil
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("itens duplicados/perdidos no recomeço: %v", got)
	}

	// Expirar de novo na segunda tentativa → devolve o erro (sem loop infinito).
	_, err = listAllPaged(context.Background(), metav1.ListOptions{},
		func(context.Context, metav1.ListOptions) ([]int, string, error) { return nil, "", expired })
	if !apierrors.IsResourceExpired(err) {
		t.Fatalf("esperava erro 410, veio %v", err)
	}
}
