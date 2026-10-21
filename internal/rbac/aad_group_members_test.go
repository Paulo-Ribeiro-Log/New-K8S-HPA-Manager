package rbac

import "testing"

func TestParseGetByIDs(t *testing.T) {
	got, err := parseGetByIDs([]byte(`{"value":[{"id":"09E0FE45-A425-4090-9D19-935E3D01E79D","displayName":"AZ_C-AKS_X"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got["09e0fe45-a425-4090-9d19-935e3d01e79d"]; !ok || g.DisplayName != "AZ_C-AKS_X" {
		t.Errorf("chave deveria estar em minúsculas: %+v", got)
	}
}

func TestMemberTypeFromOData(t *testing.T) {
	cases := map[string]string{
		"#microsoft.graph.user":             "user",
		"#microsoft.graph.group":            "group",
		"#microsoft.graph.servicePrincipal": "servicePrincipal",
		"#microsoft.graph.device":           "other",
		"":                                  "other",
	}
	for in, want := range cases {
		if got := memberTypeFromOData(in); got != want {
			t.Errorf("memberTypeFromOData(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestSortMembersGruposPrimeiro(t *testing.T) {
	ms := []GroupMember{
		{DisplayName: "zeca", Type: "user"},
		{DisplayName: "Ana", Type: "user"},
		{DisplayName: "sub", Type: "group"},
	}
	sortMembers(ms)
	if ms[0].Type != "group" || ms[1].DisplayName != "Ana" || ms[2].DisplayName != "zeca" {
		t.Errorf("ordem inesperada: %+v", ms)
	}
}
