package kubernetes

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deploymentFor(labels, annotations, podLabels, podAnnotations map[string]string, images ...string) *appsv1.Deployment {
	containers := make([]corev1.Container, 0, len(images))
	for _, img := range images {
		containers = append(containers, corev1.Container{Image: img})
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels, Annotations: podAnnotations},
				Spec:       corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func TestIsCompanyManagedDeployment(t *testing.T) {
	cases := []struct {
		name string
		dep  *appsv1.Deployment
		want bool
	}{
		{
			name: "imagem harbor",
			dep:  deploymentFor(nil, nil, nil, nil, "harbor.viavarejo.com.br/time/app:1.0"),
			want: true,
		},
		{
			name: "imagem gcbregistry",
			dep:  deploymentFor(nil, nil, nil, nil, "gcbregistry.io/time/app:1.0"),
			want: true,
		},
		{
			name: "label do workload com prefixo devops.k8s.io",
			dep:  deploymentFor(map[string]string{"devops.k8s.io/team": "sre"}, nil, nil, nil, "nginx:latest"),
			want: true,
		},
		{
			name: "label do pod template com prefixo app.via.com.br",
			dep:  deploymentFor(nil, nil, map[string]string{"app.via.com.br/squad": "logistica"}, nil, "nginx:latest"),
			want: true,
		},
		{
			name: "annotation do workload artifact.spinnaker.io",
			dep:  deploymentFor(nil, map[string]string{"artifact.spinnaker.io/location": "prd"}, nil, nil, "nginx:latest"),
			want: true,
		},
		{
			name: "annotation do pod template artifact.spinnaker.io",
			dep:  deploymentFor(nil, nil, nil, map[string]string{"artifact.spinnaker.io/version": "1.2.3"}, "nginx:latest"),
			want: true,
		},
		{
			name: "init container com imagem harbor",
			dep: func() *appsv1.Deployment {
				d := deploymentFor(nil, nil, nil, nil, "nginx:latest")
				d.Spec.Template.Spec.InitContainers = []corev1.Container{{Image: "harbor.viavarejo.com.br/time/init:1.0"}}
				return d
			}(),
			want: true,
		},
		{
			name: "ferramenta de terceiro sem nenhum sinal",
			dep:  deploymentFor(map[string]string{"app.kubernetes.io/name": "grafana"}, nil, nil, nil, "grafana/grafana:10.0.0"),
			want: false,
		},
		{
			name: "sem labels/annotations/containers",
			dep:  deploymentFor(nil, nil, nil, nil),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCompanyManagedDeployment(tc.dep)
			if got != tc.want {
				t.Errorf("isCompanyManagedDeployment() = %v, want %v", got, tc.want)
			}
		})
	}
}
