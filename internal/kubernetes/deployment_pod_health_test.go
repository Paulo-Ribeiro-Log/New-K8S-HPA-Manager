package kubernetes

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStripReplicaSetHashSuffix(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"pedido-entrega-service-78dc64dc87", "pedido-entrega-service"},
		{"visitas-service-5449c9f979", "visitas-service"},
		{"casas-bahia-api-abc123", "casas-bahia-api"}, // owner com hífen legítimo no nome
		{"sem-hash", ""}, // sufixo curto demais pra ser hash
		{"", ""},
		{"nome-com-MAIUSCULA", ""}, // sufixo com maiúscula não é um hash válido de ReplicaSet
	}
	for _, c := range cases {
		got := stripReplicaSetHashSuffix(c.name)
		if got != c.want {
			t.Errorf("stripReplicaSetHashSuffix(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeploymentNameForPod(t *testing.T) {
	podWithRS := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "pedido-entrega-service-78dc64dc87"},
			},
		},
	}
	name, ok := deploymentNameForPod(podWithRS)
	if !ok || name != "pedido-entrega-service" {
		t.Errorf("deploymentNameForPod() = (%q, %v), want (%q, true)", name, ok, "pedido-entrega-service")
	}

	podDaemonSet := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "kube-proxy"},
			},
		},
	}
	if _, ok := deploymentNameForPod(podDaemonSet); ok {
		t.Errorf("deploymentNameForPod() para pod de DaemonSet deveria retornar ok=false")
	}

	podNoOwner := &corev1.Pod{}
	if _, ok := deploymentNameForPod(podNoOwner); ok {
		t.Errorf("deploymentNameForPod() para pod sem owner deveria retornar ok=false")
	}
}

func TestPodIssueReason(t *testing.T) {
	now := metav1.NewTime(time.Now())

	cases := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "pod saudável",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Ready: true},
					},
				},
			},
			want: "",
		},
		{
			name: "terminating tem prioridade sobre phase",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			},
			want: "Terminating",
		},
		{
			name: "pending",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			},
			want: "Pending",
		},
		{
			name: "failed",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodFailed},
			},
			want: "Failed",
		},
		{
			name: "crashloopbackoff mesmo com fase Running",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
							},
						},
					},
				},
			},
			want: "CrashLoopBackOff",
		},
		{
			name: "imagepullbackoff",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
							},
						},
					},
				},
			},
			want: "ImagePullBackOff",
		},
		{
			name: "running mas container não-ready sem waiting reason conhecido",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Ready: false},
						{Ready: true},
					},
				},
			},
			want: "1 container(s) not ready",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := podIssueReason(c.pod)
			if got != c.want {
				t.Errorf("podIssueReason() = %q, want %q", got, c.want)
			}
		})
	}
}
