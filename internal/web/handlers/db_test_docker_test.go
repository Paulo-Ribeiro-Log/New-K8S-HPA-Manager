package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestFilterStaleContainers(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Minute).Format(time.RFC3339Nano)
	stale := now.Add(-11 * time.Minute).Format(time.RFC3339Nano)
	borderline := now.Add(-10*time.Minute - time.Second).Format(time.RFC3339Nano)

	output := "id-fresh|" + fresh + "\n" +
		"id-stale|" + stale + "\n" +
		"id-borderline|" + borderline + "\n" +
		"\n" + // linha vazia no meio da saída deve ser ignorada
		"malformada-sem-pipe\n"

	got := filterStaleContainers(output, now, dbTestContainerMaxAge)

	want := map[string]bool{"id-stale": true, "id-borderline": true}
	if len(got) != len(want) {
		t.Fatalf("esperava %d containers órfãos, veio %d: %v", len(want), len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("id inesperado marcado como órfão: %q", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("faltou marcar como órfão: %v", want)
	}
}

func TestFilterStaleContainersEmptyAndInvalidInput(t *testing.T) {
	now := time.Now()
	if got := filterStaleContainers("", now, dbTestContainerMaxAge); got != nil {
		t.Errorf("saída vazia deveria devolver nil, veio %v", got)
	}
	if got := filterStaleContainers("id|data-invalida\n", now, dbTestContainerMaxAge); got != nil {
		t.Errorf("timestamp inválido deveria ser ignorado (não removido por segurança), veio %v", got)
	}
}

func TestDockerReasonMessage(t *testing.T) {
	cases := map[string]string{
		dbDockerReasonAddressPoolExhausted: "rede padrão",
		"algo-desconhecido":                "daemon do Docker não respondeu",
		"":                                 "daemon do Docker não respondeu",
	}
	for reason, wantSubstr := range cases {
		got := dockerReasonMessage(reason)
		if !strings.Contains(got, wantSubstr) {
			t.Errorf("reason=%q: esperava conter %q, veio %q", reason, wantSubstr, got)
		}
	}
}
