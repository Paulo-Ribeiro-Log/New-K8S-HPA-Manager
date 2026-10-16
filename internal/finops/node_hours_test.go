package finops

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

func TestBucketNodeHours(t *testing.T) {
	loc := time.UTC
	mon := time.Date(2026, 10, 12, 0, 0, 0, 0, loc) // segunda
	pt := func(at time.Time, v float64) model.SamplePair {
		return model.SamplePair{Timestamp: model.TimeFromUnixNano(at.UnixNano()), Value: model.SampleValue(v)}
	}
	m := model.Matrix{{
		Metric: model.Metric{"pool": "app"},
		Values: []model.SamplePair{
			pt(mon.Add(10*time.Hour), 4), pt(mon.AddDate(0, 0, 7).Add(10*time.Hour), 6), // seg 10h: 4 e 6 → 5
			pt(mon.AddDate(0, 0, 6).Add(23*time.Hour), 1), // domingo 23h
		},
	}, {Metric: model.Metric{}, Values: []model.SamplePair{pt(mon, 9)}}}
	nh := bucketNodeHours(m, loc)
	if nh.Pools["app"][10] != 5 || nh.Pools["app"][167] != 1 || nh.Pools["app"][11] != -1 || nh.Hours != 2 || len(nh.Pools) != 1 {
		t.Errorf("buckets: hours=%d seg10=%v dom23=%v pools=%d", nh.Hours, nh.Pools["app"][10], nh.Pools["app"][167], len(nh.Pools))
	}
}
