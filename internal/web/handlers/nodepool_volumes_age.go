package handlers

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// idleOldDays: volume ocioso criado há mais que isso vira candidato a limpeza.
const idleOldDays = 30

// setAge grava a criação e a idade em dias. O Kubernetes não guarda desde quando o volume está
// ocioso; a idade é só o teto desse tempo.
func setAge(it *VolumeItem, created metav1.Time, now time.Time) {
	if created.IsZero() {
		return
	}
	d := int(now.Sub(created.Time).Hours() / 24)
	if d < 0 {
		d = 0
	}
	it.CreatedAt = created.UTC().Format(time.RFC3339)
	it.AgeDays = &d
}

// flagIdleOld aponta o volume ocioso com mais de idleOldDays de criado.
func flagIdleOld(items []VolumeItem, sum *VolumesSummary) {
	for i := range items {
		it := &items[i]
		if it.Idle && it.AgeDays != nil && *it.AgeDays > idleOldDays {
			it.Issues = append(it.Issues, fmt.Sprintf("Ocioso e criado há %d dias: candidato a limpeza", *it.AgeDays))
			sum.IdleOld++
		}
	}
}
