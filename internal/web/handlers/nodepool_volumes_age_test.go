package handlers

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFlagIdleOld(t *testing.T) {
	now := time.Now()
	items := []VolumeItem{{Idle: true}, {Idle: true}, {Idle: false}, {Idle: true}}
	setAge(&items[0], metav1.NewTime(now.AddDate(0, 0, -90)), now)
	setAge(&items[1], metav1.NewTime(now.AddDate(0, 0, -5)), now)
	setAge(&items[2], metav1.NewTime(now.AddDate(0, 0, -90)), now)
	var sum VolumesSummary
	flagIdleOld(items, &sum)
	if items[0].AgeDays == nil || *items[0].AgeDays != 90 {
		t.Fatalf("idade = %v", items[0].AgeDays)
	}
	if len(items[0].Issues) != 1 || len(items[1].Issues) != 0 || len(items[2].Issues) != 0 || len(items[3].Issues) != 0 || sum.IdleOld != 1 {
		t.Fatalf("%+v %+v", items, sum)
	}
}
