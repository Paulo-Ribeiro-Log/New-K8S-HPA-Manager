package finops

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodeWithDisk(labels map[string]string, ephemeralStorage string) corev1.Node {
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: labels}}
	if ephemeralStorage != "" {
		n.Status.Capacity = corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(ephemeralStorage)}
	}
	return n
}

func TestOSDiskFromNode(t *testing.T) {
	cases := []struct {
		name string
		node corev1.Node
		want OSDiskInfo
	}{
		{"efêmero de 64 GB (monitoringspot)",
			nodeWithDisk(map[string]string{"kubernetes.azure.com/storageprofile": "ephemeral", "kubernetes.azure.com/storagetier": "Premium_LRS"}, "62914556Ki"),
			OSDiskInfo{SizeGB: 60, Source: "node", Ephemeral: true, AzureType: "Premium SSD"}},
		{"gerenciado de 128 GB: sistema de arquivos ~124 GiB vira P10",
			nodeWithDisk(map[string]string{"kubernetes.azure.com/storageprofile": "managed", "kubernetes.azure.com/storagetier": "Premium_LRS"}, "129886128Ki"),
			OSDiskInfo{SizeGB: 128, Source: "node", AzureType: "Premium SSD"}},
		{"gerenciado Standard SSD de 256 GB",
			nodeWithDisk(map[string]string{"kubernetes.azure.com/storageprofile": "managed", "kubernetes.azure.com/storagetier": "StandardSSD_LRS"}, "260046848Ki"),
			OSDiskInfo{SizeGB: 256, Source: "node", AzureType: "Standard SSD"}},
		{"sem dado: padrão de 128 GB Premium",
			nodeWithDisk(map[string]string{}, ""),
			OSDiskInfo{SizeGB: 128, Source: "default", AzureType: "Premium SSD"}},
	}
	for _, c := range cases {
		if got := osDiskFromNode(c.node); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
