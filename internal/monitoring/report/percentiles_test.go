package report

import (
	"reflect"
	"testing"
)

func TestNormalizePercentiles(t *testing.T) {
	casos := []struct {
		in   []int
		want []int
	}{
		{nil, []int{99}},
		{[]int{}, []int{99}},
		{[]int{99, 50, 95}, []int{50, 95, 99}},
		{[]int{95, 95, 50, 95}, []int{50, 95}},
		{[]int{99}, []int{99}},
	}
	for _, c := range casos {
		got, err := NormalizePercentiles(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v -> %v (%v), esperava %v", c.in, got, err, c.want)
		}
	}
	for _, bad := range [][]int{{90}, {50, 0}, {-1}, {100}} {
		if _, err := NormalizePercentiles(bad); err == nil {
			t.Errorf("%v: esperava erro", bad)
		}
	}
	// o padrão devolvido é uma cópia: alterar não muda o padrão global
	d, _ := NormalizePercentiles(nil)
	d[0] = 1
	if DefaultPercentiles[0] != 99 {
		t.Errorf("o padrão global foi alterado: %v", DefaultPercentiles)
	}
}
