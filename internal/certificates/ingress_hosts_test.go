package certificates

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveIngressHosts(t *testing.T) {
	client := fake.NewSimpleClientset(
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "app-ingress", Namespace: "prod"},
			Spec: networkingv1.IngressSpec{
				TLS: []networkingv1.IngressTLS{
					{SecretName: "app-tls", Hosts: []string{"app.example.com", "www.example.com"}},
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "other-ingress", Namespace: "prod"},
			Spec: networkingv1.IngressSpec{
				TLS: []networkingv1.IngressTLS{
					{SecretName: "other-tls", Hosts: []string{"other.example.com"}},
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "app-ingress-other-ns", Namespace: "staging"},
			Spec: networkingv1.IngressSpec{
				TLS: []networkingv1.IngressTLS{
					{SecretName: "app-tls", Hosts: []string{"staging.example.com"}},
				},
			},
		},
	)

	hosts := resolveIngressHosts(context.Background(), client, "prod", "app-tls")

	if len(hosts) != 2 {
		t.Fatalf("esperava 2 hosts, obteve %d: %v", len(hosts), hosts)
	}
	want := map[string]bool{"app.example.com": true, "www.example.com": true}
	for _, h := range hosts {
		if !want[h] {
			t.Errorf("host inesperado: %s", h)
		}
	}
}

func TestResolveIngressHosts_SemMatch(t *testing.T) {
	client := fake.NewSimpleClientset(
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "app-ingress", Namespace: "prod"},
			Spec: networkingv1.IngressSpec{
				TLS: []networkingv1.IngressTLS{
					{SecretName: "other-tls", Hosts: []string{"other.example.com"}},
				},
			},
		},
	)

	hosts := resolveIngressHosts(context.Background(), client, "prod", "app-tls")
	if len(hosts) != 0 {
		t.Errorf("esperava nenhum host, obteve %v", hosts)
	}
}
