package report

import (
	"fmt"
	"sort"
)

// DefaultPercentiles é o que se coleta quando o pedido não diz: só o P99 (o comportamento original).
var DefaultPercentiles = []int{99}

// allowedPercentiles são os percentis que o relatório sabe coletar.
var allowedPercentiles = map[int]bool{50: true, 95: true, 99: true}

// NormalizePercentiles valida a escolha do operador: vazio vale o padrão (P99); repetidos somem; a
// lista sai em ordem crescente. Percentil fora de P50/P95/P99 é erro (não existe consulta para ele).
func NormalizePercentiles(in []int) ([]int, error) {
	if len(in) == 0 {
		return append([]int(nil), DefaultPercentiles...), nil
	}
	seen := map[int]bool{}
	var out []int
	for _, p := range in {
		if !allowedPercentiles[p] {
			return nil, fmt.Errorf("percentil %d não suportado: use 50, 95 e/ou 99", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out, nil
}
