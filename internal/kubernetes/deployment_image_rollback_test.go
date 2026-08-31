package kubernetes

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestDeploymentWithTwoContainers(t *testing.T) *fake.Clientset {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-faturamento-nf-estoque",
			Namespace: "faturamento-prd",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "harbor.viavarejo.com.br/modernizacao-faturamento/ms-faturamento-nf-estoque:1.0.4-1",
							Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
							Env:   []corev1.EnvVar{{Name: "PROFILE", Value: "prd"}},
						},
						{
							Name:  "istio-proxy",
							Image: "istio/proxyv2:1.20.0",
						},
					},
				},
			},
		},
	}
	return fake.NewSimpleClientset(dep)
}

func TestSetDeploymentContainerImages_PatchesOnlyTargetContainer(t *testing.T) {
	cs := newTestDeploymentWithTwoContainers(t)
	client := NewClient(cs, "test-cluster")

	updated, err := client.SetDeploymentContainerImages(context.Background(), "faturamento-prd", "ms-faturamento-nf-estoque", map[string]string{
		"app": "harbor.viavarejo.com.br/modernizacao-faturamento/ms-faturamento-nf-estoque:1.0.3-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	containers := updated.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers preserved, got %d", len(containers))
	}

	var app, sidecar *corev1.Container
	for i := range containers {
		switch containers[i].Name {
		case "app":
			app = &containers[i]
		case "istio-proxy":
			sidecar = &containers[i]
		}
	}
	if app == nil || sidecar == nil {
		t.Fatalf("expected both containers to survive the patch, got %+v", containers)
	}

	if app.Image != "harbor.viavarejo.com.br/modernizacao-faturamento/ms-faturamento-nf-estoque:1.0.3-1" {
		t.Errorf("expected app image to be updated, got %q", app.Image)
	}
	// Campos que NÃO fazem parte do patch (ports/env) precisam sobreviver intactos — confirma que
	// o patch estratégico não sobrescreveu o container inteiro, só o campo image.
	if len(app.Ports) != 1 || app.Ports[0].ContainerPort != 8080 {
		t.Errorf("expected app ports to survive the patch untouched, got %+v", app.Ports)
	}
	if len(app.Env) != 1 || app.Env[0].Name != "PROFILE" {
		t.Errorf("expected app env to survive the patch untouched, got %+v", app.Env)
	}

	// O container NÃO mencionado no patch precisa continuar exatamente como estava.
	if sidecar.Image != "istio/proxyv2:1.20.0" {
		t.Errorf("expected sidecar image to remain untouched, got %q", sidecar.Image)
	}
}

func TestSetDeploymentContainerImages_RejectsUnknownContainer(t *testing.T) {
	cs := newTestDeploymentWithTwoContainers(t)
	client := NewClient(cs, "test-cluster")

	_, err := client.SetDeploymentContainerImages(context.Background(), "faturamento-prd", "ms-faturamento-nf-estoque", map[string]string{
		"nao-existe": "some/image:tag",
	})
	if err == nil {
		t.Fatal("expected error for unknown container name, got nil")
	}
}

func TestSetDeploymentContainerImages_RejectsEmptyImages(t *testing.T) {
	cs := newTestDeploymentWithTwoContainers(t)
	client := NewClient(cs, "test-cluster")

	_, err := client.SetDeploymentContainerImages(context.Background(), "faturamento-prd", "ms-faturamento-nf-estoque", map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty images map, got nil")
	}
}
