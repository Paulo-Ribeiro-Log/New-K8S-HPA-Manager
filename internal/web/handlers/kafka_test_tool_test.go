package handlers

import (
	"reflect"
	"testing"
)

func TestBuildKafkaOffsetQueryArgsMulti(t *testing.T) {
	topics := []kafkaTopicPartitions{
		{Topic: "orders", Partitions: 2},
		{Topic: "payments", Partitions: 1},
	}
	got := buildKafkaOffsetQueryArgsMulti(topics, -1)
	want := []string{"-Q", "-t", "orders:0:-1", "-t", "orders:1:-1", "-t", "payments:0:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseKafkaOffsetLinesWithTopic(t *testing.T) {
	raw := "orders [0] offset 105\norders [1] offset 42\npayments [0] offset 7\n"
	got := parseKafkaOffsetLinesWithTopic(raw)
	want := map[string]int64{
		"orders\x000":   105,
		"orders\x001":   42,
		"payments\x000": 7,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
