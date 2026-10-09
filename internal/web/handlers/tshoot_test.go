package handlers

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTshootPodSpec(t *testing.T) {
	p := tshootPodSpec("ns1", "tshoot-abc123", "sre@empresa.com")
	if p.Namespace != "ns1" || p.Labels[tshootLabel] != "true" || p.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("metadados/restartPolicy: %+v", p.ObjectMeta)
	}
	if p.Spec.ActiveDeadlineSeconds == nil || *p.Spec.ActiveDeadlineSeconds != int64(tshootMaxLifetime.Seconds()) {
		t.Error("o pod precisa de prazo máximo de vida (backstop se o servidor morrer)")
	}
	c := p.Spec.Containers[0]
	if c.Image != tshootImage || strings.Contains(strings.Join(c.Command, " "), "infinity") {
		t.Errorf("container: %v %v", c.Image, c.Command)
	}
	if c.Resources.Limits.Cpu().IsZero() || c.Resources.Requests.Memory().IsZero() {
		t.Error("requests/limits são exigidos por políticas comuns (Kyverno/Gatekeeper)")
	}
	if n := newTshootName(); !strings.HasPrefix(n, "tshoot-") || len(n) != len("tshoot-")+6 || n == newTshootName() {
		t.Errorf("nome gerado: %q", n)
	}
}

func tshootPod(name, phase string, labeled bool) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns1"}, Status: corev1.PodStatus{Phase: corev1.PodPhase(phase)}}
	if labeled {
		p.Labels = map[string]string{tshootLabel: "true"}
	}
	return p
}

func TestCleanupStaleTshootPods(t *testing.T) {
	cs := fake.NewSimpleClientset([]runtime.Object{
		tshootPod("tshoot-velho", "Failed", true),  // prazo estourado: sobra
		tshootPod("tshoot-fim", "Succeeded", true), // terminou: sobra
		tshootPod("tshoot-ativo", "Running", true), // pode ser sessão de outra instância: não tocar
		tshootPod("app-falho", "Failed", false),    // não é tshoot: não tocar
	}...)
	removed := cleanupStaleTshootPods(context.Background(), cs, "ns1")
	if strings.Join(removed, ",") != "tshoot-velho,tshoot-fim" && strings.Join(removed, ",") != "tshoot-fim,tshoot-velho" {
		t.Errorf("removidos: %v", removed)
	}
	for _, keep := range []string{"tshoot-ativo", "app-falho"} {
		if _, err := cs.CoreV1().Pods("ns1").Get(context.Background(), keep, metav1.GetOptions{}); err != nil {
			t.Errorf("%s não deveria ter sido apagado", keep)
		}
	}
}

func TestWaitTshootReady(t *testing.T) {
	ctx := context.Background()
	running := tshootPod("tshoot-ok", "Running", true)
	cs := fake.NewSimpleClientset(running)
	if err := waitTshootReady(ctx, cs, "ns1", "tshoot-ok", func(string) {}); err != nil {
		t.Errorf("Running deveria estar pronto: %v", err)
	}

	pull := tshootPod("tshoot-pull", "Pending", true)
	pull.Status.ContainerStatuses = []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "pull access denied"}}}}
	cs = fake.NewSimpleClientset(pull)
	err := waitTshootReady(ctx, cs, "ns1", "tshoot-pull", func(string) {})
	if err == nil || !strings.Contains(err.Error(), "ImagePullBackOff") {
		t.Errorf("imagem que não baixa deveria falhar na hora: %v", err)
	}

	failed := tshootPod("tshoot-morto", "Failed", true)
	cs = fake.NewSimpleClientset(failed)
	if err := waitTshootReady(ctx, cs, "ns1", "tshoot-morto", func(string) {}); err == nil {
		t.Error("pod que terminou antes de ficar pronto deveria dar erro")
	}
}

func TestNodeShellPodSpec(t *testing.T) {
	p := nodeShellPodSpec("default", newNodeShellName(), "aks-nodepool1-123-vmss000000", "sre@empresa.com")
	if !strings.HasPrefix(p.Name, "node-shell-") || p.Spec.NodeName != "aks-nodepool1-123-vmss000000" {
		t.Errorf("nome/node: %s %s", p.Name, p.Spec.NodeName)
	}
	if !p.Spec.HostPID || !p.Spec.HostNetwork || !p.Spec.HostIPC || p.Spec.DNSPolicy != corev1.DNSClusterFirstWithHostNet {
		t.Error("precisa dos namespaces do host (como kubectl debug node)")
	}
	c := p.Spec.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Error("container precisa ser privilegiado")
	}
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/host" || p.Spec.Volumes[0].HostPath == nil || p.Spec.Volumes[0].HostPath.Path != "/" {
		t.Error("o / do node precisa estar em /host")
	}
	if len(p.Spec.Tolerations) != 1 || p.Spec.Tolerations[0].Operator != corev1.TolerationOpExists {
		t.Error("precisa tolerar qualquer taint (nodes de sistema/cordonados)")
	}
	// mesmos backstops do tshoot: label da limpeza de sobras e prazo máximo de vida
	if p.Labels[tshootLabel] != "true" || p.Spec.ActiveDeadlineSeconds == nil || p.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Error("node shell precisa dos mesmos backstops do --rm")
	}
}
