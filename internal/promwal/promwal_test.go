package promwal

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const gi = int64(1) << 30

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const replayLog = `level=info msg="Replaying WAL, this may take a while"
level=info msg="WAL segment loaded" segment=40 maxSegment=118`

func oomPod(name, log string) PodInfo {
	return PodInfo{Name: name, Phase: "Running", WaitingReason: "CrashLoopBackOff", Restarts: 9,
		LastTermReason: "OOMKilled", LastExitCode: 137, LogTail: log}
}

func TestAnalyzeVeredito(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want Verdict
	}{
		{"sem pods", Input{}, VerdictInvestigate},
		{"saudável", Input{Pods: []PodInfo{{Name: "p-0", Ready: true, Restarts: 2}}}, VerdictHealthy},
		{"OOM no replay com limit baixo", Input{MemoryLimitBytes: 2 * gi, Pods: []PodInfo{oomPod("p-0", replayLog)}}, VerdictIncreaseMemory},
		{"OOM no replay com limit no teto", Input{MemoryLimitBytes: 8 * gi, Pods: []PodInfo{oomPod("p-0", replayLog)}}, VerdictCleanWAL},
		{"OOM depois de servir não é WAL", Input{MemoryLimitBytes: 16 * gi, Pods: []PodInfo{
			oomPod("p-0", replayLog+"\nlevel=info msg=\"Server is ready to receive web requests.\"")}}, VerdictIncreaseMemory},
		{"OOM sem limit", Input{Pods: []PodInfo{oomPod("p-0", replayLog)}}, VerdictInvestigate},
		{"corrupção", Input{MemoryLimitBytes: 2 * gi, Pods: []PodInfo{{Name: "p-0", WaitingReason: "CrashLoopBackOff",
			LogTail: `level=warn msg="Encountered WAL read error, attempting repair" err="unexpected end of segment"`}}}, VerdictCleanWAL},
		{"imagem não baixa", Input{Pods: []PodInfo{{Name: "p-0", WaitingReason: "ImagePullBackOff"}}}, VerdictInvestigate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Analyze(tc.in, testNow).Verdict; got != tc.want {
				t.Fatalf("veredito = %s, esperado %s", got, tc.want)
			}
		})
	}
}

func TestAnalyzeSugestaoEAvisos(t *testing.T) {
	r := Analyze(Input{MemoryLimitBytes: 2 * gi, HelmManaged: true, ThanosSidecar: true, Pods: []PodInfo{
		oomPod("p-0", replayLog), {Name: "p-1", Ready: true},
	}}, testNow)
	if r.SuggestedMemory != "4Gi" {
		t.Errorf("sugestão = %q, esperado 4Gi", r.SuggestedMemory)
	}
	joined := strings.Join(r.Warnings, "\n")
	for _, want := range []string{"Helm/Argo", "p-1", "Thanos"} {
		if !strings.Contains(joined, want) {
			t.Errorf("faltou aviso sobre %q em:\n%s", want, joined)
		}
	}
	if got := suggestMemory(6 * gi); got != "8Gi" {
		t.Errorf("sugestão limitada ao teto = %q, esperado 8Gi", got)
	}
	if got := suggestMemory(0); got != "" {
		t.Errorf("sem limit não sugere, veio %q", got)
	}
}

func TestAnalyzeEvidenciaDoReplay(t *testing.T) {
	r := Analyze(Input{MemoryLimitBytes: 2 * gi, Pods: []PodInfo{oomPod("p-0", replayLog)}}, testNow)
	if ev := strings.Join(r.Pods[0].Evidence, "|"); !strings.Contains(ev, "40 de 118") || !strings.Contains(ev, "OOMKilled") {
		t.Errorf("evidências sem segmento/OOM: %s", ev)
	}
}

func TestAnalyzeLimparWALExigeJanela(t *testing.T) {
	base := Input{MemoryLimitBytes: 8 * gi, Pods: []PodInfo{oomPod("p-0", replayLog)}}

	r := Analyze(base, testNow)
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "análise profunda") {
		t.Errorf("sem deep deveria pedir a análise profunda: %v", r.Warnings)
	}

	base.Deep = []DeepInfo{{Pod: "p-0", PVC: "data-p-0", Blocks: 5, WALBytes: 20 << 20,
		LastBlockMaxTime: testNow.Add(-30 * time.Hour)}}
	r = Analyze(base, testNow)
	if len(r.Deep) != 1 || r.Deep[0].LostWindowHours < 29.9 || r.Deep[0].LostWindowHours > 30.1 {
		t.Fatalf("janela perdida incorreta: %+v", r.Deep)
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "pequeno") {
		t.Errorf("WAL de 20Mi deveria gerar aviso de WAL pequeno: %v", r.Warnings)
	}

	base.Deep[0].LastBlockMaxTime = time.Time{}
	if !strings.Contains(strings.Join(Analyze(base, testNow).Warnings, "\n"), "TODO o histórico") {
		t.Error("sem blocos compactados deveria avisar que todo o histórico seria perdido")
	}
}

func TestParsers(t *testing.T) {
	meta := `{"ulid":"A","maxTime": 1760000000000}` + "\n" + `{"ulid":"B","maxTime": 1760086400000}`
	if got := lastMaxTime(meta); got.UnixMilli() != 1760086400000 {
		t.Errorf("lastMaxTime = %v", got)
	}
	if !lastMaxTime("").IsZero() {
		t.Error("sem meta.json deveria ser zero")
	}
	wal, chunks := parseDU("2048\twal\n512\tchunks_head\n")
	if wal != 2048*1024 || chunks != 512*1024 {
		t.Errorf("parseDU = %d, %d", wal, chunks)
	}
}

func TestStoreExpira(t *testing.T) {
	s := NewStore()
	cur := testNow
	now = func() time.Time { return cur }
	t.Cleanup(func() { now = time.Now })

	r := s.Put(Report{CreatedAt: cur})
	if r.ID == "" {
		t.Fatal("Put deveria atribuir o ID")
	}
	if _, ok := s.Get(r.ID); !ok {
		t.Fatal("análise recente deveria existir")
	}
	cur = cur.Add(AnalysisTTL + time.Second)
	if _, ok := s.Get(r.ID); ok {
		t.Fatal("análise expirada não deveria valer")
	}
}

const crJSON = `{
 "metadata": {"name": "prom", "labels": {"app.kubernetes.io/managed-by": "Helm"}},
 "spec": {"replicas": 2, "resources": {"limits": {"memory": "2Gi"}},
  "thanos": {"image": "x"},
  "storage": {"volumeClaimTemplate": {"spec": {"resources": {"requests": {"storage": "100Gi"}}}}}}}`

func fakeRunner(calls *[]string) Runner {
	return func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		switch {
		case args[0] == "get":
			return []byte(crJSON), nil
		case args[0] == "exec" && strings.Contains(args[len(args)-1], "meta.json"):
			return []byte(`{"maxTime": 1760000000000}`), nil
		case args[0] == "exec" && strings.Contains(args[len(args)-1], "wc -l"):
			return []byte("7\n"), nil
		case args[0] == "exec":
			return []byte("4096\twal\n1024\tchunks_head\n"), nil
		}
		return nil, nil
	}
}

func crashPod(name, node string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "monitoring", Labels: map[string]string{defaultCRLabel: "prom"}},
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{{Name: promContainer, Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")}}}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data-" + name}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: promContainer, RestartCount: 5,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}},
		}}},
	}
}

func TestCollectEDeep(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus-prom", Namespace: "monitoring"},
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"}}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: promContainer, VolumeMounts: []corev1.VolumeMount{{Name: "data", SubPath: "prometheus-db"}}}}}},
		},
	}
	cs := fake.NewSimpleClientset(crashPod("prometheus-prom-0", "node-a"), sts)

	// O pod temporário nasce Ready (o fake não tem kubelet).
	var created *corev1.Pod
	cs.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
		if p.Labels[deepPodLabelKey] == deepPodLabelVal {
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "sh", Ready: true}}
			created = p.DeepCopy()
		}
		return false, nil, nil
	})

	var calls []string
	c := &Collector{Client: cs, Run: fakeRunner(&calls)}

	in, err := c.Collect(context.Background(), "ctx", "monitoring", "prom")
	if err != nil {
		t.Fatal(err)
	}
	if in.Replicas != 2 || in.MemoryLimitBytes != 4*gi || in.PVCSizeBytes != 100*gi || !in.HelmManaged || !in.ThanosSidecar {
		t.Fatalf("CRD/pod lidos errado: %+v", in)
	}
	if len(in.Pods) != 1 || in.Pods[0].LastTermReason != "OOMKilled" || in.Pods[0].PVC != "data-prometheus-prom-0" || in.Pods[0].LogTail == "" {
		t.Fatalf("pod lido errado: %+v", in.Pods)
	}

	if errs := c.CollectDeep(context.Background(), &in); len(errs) != 0 {
		t.Fatal(errs)
	}
	if created == nil || created.Spec.NodeName != "node-a" {
		t.Fatalf("pod temporário deveria ser fixado no nó do Prometheus: %+v", created)
	}
	vm := created.Spec.Containers[0].VolumeMounts[0]
	if !vm.ReadOnly || vm.SubPath != "prometheus-db" || !created.Spec.Volumes[0].PersistentVolumeClaim.ReadOnly {
		t.Errorf("o volume deve ser montado read-only e com o subPath do StatefulSet: %+v", vm)
	}
	if left, _ := cs.CoreV1().Pods("monitoring").List(context.Background(), metav1.ListOptions{LabelSelector: deepPodLabelKey + "=" + deepPodLabelVal}); len(left.Items) != 0 {
		t.Errorf("pod temporário não foi removido: %d restante(s)", len(left.Items))
	}
	if len(in.Deep) != 1 || in.Deep[0].Blocks != 7 || in.Deep[0].WALBytes != 4096*1024 || in.Deep[0].LastBlockMaxTime.UnixMilli() != 1760000000000 {
		t.Fatalf("deep lido errado: %+v", in.Deep)
	}
}

func TestDeepSemNoNaoMonta(t *testing.T) {
	cs := fake.NewSimpleClientset()
	var calls []string
	c := &Collector{Client: cs, Run: fakeRunner(&calls)}
	in := &Input{Namespace: "monitoring", Name: "prom", Pods: []PodInfo{{Name: "p-0", PVC: "data-p-0"}}}
	if errs := c.CollectDeep(context.Background(), in); len(errs) != 1 {
		t.Fatalf("pod sem nó deveria gerar erro e não criar pod, errs=%v", errs)
	}
	if pods, _ := cs.CoreV1().Pods("monitoring").List(context.Background(), metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Error("não deveria ter criado pod")
	}
}
