package handlers

import (
	"reflect"
	"testing"
)

func TestParseJourneys(t *testing.T) {
	if got := parseJourneys(" Logistica, backoffice ,logistica,,"); !reflect.DeepEqual(got, []string{"backoffice", "logistica"}) {
		t.Errorf("parseJourneys = %v", got)
	}
	if got := parseJourneys(""); len(got) != 0 {
		t.Errorf("vazio deveria ser todas ([]), got %v", got)
	}
}
