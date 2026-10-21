package azure

import (
	"reflect"
	"testing"
)

func TestParseAKSAdminGroupIDs(t *testing.T) {
	body := []byte(`{"properties":{"aadProfile":{"managed":true,"adminGroupObjectIDs":["09E0FE45-A425-4090-9D19-935E3D01E79D"," ","b1"]}}}`)
	got, err := parseAKSAdminGroupIDs(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"09e0fe45-a425-4090-9d19-935e3d01e79d", "b1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseAKSAdminGroupIDsSemAAD(t *testing.T) {
	got, err := parseAKSAdminGroupIDs([]byte(`{"properties":{}}`))
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("esperava lista vazia não nula, got %v err %v", got, err)
	}
}
