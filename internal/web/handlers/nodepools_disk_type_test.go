package handlers

import "testing"

func TestNodeOSDiskType(t *testing.T) {
	aksLabels := map[string]string{"agentpool": "user1", "storageprofile": "managed"}
	cases := []struct {
		name      string
		labels    map[string]string
		poolType  string
		wantEph   bool
		wantLabel string
	}{
		{"AKS efêmero pelo pool, mesmo com label storageprofile=managed", aksLabels, "Ephemeral", true, "Ephemeral OS Disk"},
		{"AKS gerenciado pelo pool", aksLabels, "Managed", false, "Managed OS Disk"},
		{"AKS sem cache do pool", aksLabels, "", false, "OS Disk (tipo desconhecido)"},
		{"AKS sem cache com label ephemeral-os", map[string]string{"storageprofile": "managed", "kubernetes.azure.com/ephemeral-os": "true"}, "", true, "Ephemeral OS Disk"},
		{"GKE com tipo do pool", map[string]string{"cloud.google.com/gke-nodepool": "p"}, "pd-balanced", false, "Persistent Disk (pd-balanced)"},
		{"EKS", map[string]string{"eks.amazonaws.com/nodegroup": "ng"}, "", false, "EBS Volume"},
	}
	for _, c := range cases {
		eph, label := nodeOSDiskType(c.labels, c.poolType)
		if eph != c.wantEph || label != c.wantLabel {
			t.Errorf("%s: got (%v, %q), want (%v, %q)", c.name, eph, label, c.wantEph, c.wantLabel)
		}
	}
}
