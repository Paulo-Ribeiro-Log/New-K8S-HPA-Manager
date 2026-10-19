package promwal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// cleanEnv simula o cluster: o recurso Prometheus (anotações/replicas), os pods e os comandos kubectl.
type cleanEnv struct {
	t           *testing.T
	cs          *fake.Clientset
	annotations map[string]string
	replicas    int
	calls       []string
	failExec    string // se o comando exec contiver isto, falha
	onExec      func() // chamado a cada exec (ex.: cancelar o ctx)
	allow       bool
}

func newCleanEnv(t *testing.T) *cleanEnv {
	e := &cleanEnv{t: t, annotations: map[string]string{}, replicas: 2, allow: true}
	e.cs = fake.NewSimpleClientset(crashPod("prometheus-prom-0", "node-a"), crashPod("prometheus-prom-1", "node-b"))

	e.cs.PrependReactor("create", "selfsubjectaccessreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		rev := a.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
		rev.Status.Allowed = e.allow
		return true, rev, nil
	})
	e.cs.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
		if p.Labels[deepPodLabelKey] == cleanPodLabelVal {
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "sh", Ready: true}}
		}
		return false, nil, nil
	})
	return e
}

func (e *cleanEnv) collector(ctx context.Context) *Cleaner {
	run := func(rctx context.Context, _, _ string, args ...string) ([]byte, error) {
		if err := rctx.Err(); err != nil { // como o exec.CommandContext real
			return nil, err
		}
		line := strings.Join(args, " ")
		e.calls = append(e.calls, line)
		switch args[0] {
		case "get":
			ann := ""
			for k, v := range e.annotations {
				ann += fmt.Sprintf("%q:%q,", k, v)
			}
			return []byte(fmt.Sprintf(`{"metadata":{"name":"prom","annotations":{%s}},"spec":{"replicas":%d}}`, strings.TrimSuffix(ann, ","), e.replicas)), nil
		case "patch":
			p := args[len(args)-1]
			switch {
			case strings.Contains(p, AnnotationOriginalReplicas) && strings.Contains(p, "null"): // restauração
				var patch struct {
					Metadata struct {
						Annotations map[string]any `json:"annotations"`
					} `json:"metadata"`
				}
				_ = json.Unmarshal([]byte(p), &patch)
				for k, v := range patch.Metadata.Annotations {
					if sv, ok := v.(string); ok {
						e.annotations[k] = sv
					}
				}
				delete(e.annotations, AnnotationOriginalReplicas)
				e.replicas = 2
				for _, n := range []string{"prometheus-prom-0", "prometheus-prom-1"} { // o operator recria os pods, já Ready
					pod := crashPod(n, "node")
					pod.Status.ContainerStatuses[0].Ready = true
					_, _ = e.cs.CoreV1().Pods("monitoring").Create(context.Background(), pod, metav1.CreateOptions{})
				}
			case strings.Contains(p, AnnotationOriginalReplicas):
				e.annotations[AnnotationOriginalReplicas] = "2"
			case strings.Contains(p, `"replicas":0`):
				e.replicas = 0
				for _, n := range []string{"prometheus-prom-0", "prometheus-prom-1"} {
					_ = e.cs.CoreV1().Pods("monitoring").Delete(context.Background(), n, metav1.DeleteOptions{})
				}
			}
			return nil, nil
		case "exec":
			if e.onExec != nil {
				e.onExec()
			}
			cmd := args[len(args)-1]
			switch {
			case e.failExec != "" && strings.Contains(cmd, e.failExec):
				return nil, errors.New("exec falhou")
			case strings.Contains(cmd, "meta.json"):
				return []byte(`{"maxTime": 1760000000000}`), nil
			case strings.Contains(cmd, "wc -l") && strings.Contains(cmd, "/prometheus/wal"):
				return []byte("0\n"), nil
			case strings.Contains(cmd, "wc -l"):
				return []byte("3\n"), nil
			case strings.Contains(cmd, "du -sk"):
				return []byte("2048\twal\n512\tchunks_head\n"), nil
			}
		}
		return nil, nil
	}
	return &Cleaner{&Collector{Client: e.cs, Run: run}}
}

func (e *cleanEnv) has(substr string) bool {
	for _, c := range e.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func (e *cleanEnv) tempPods() int {
	l, _ := e.cs.CoreV1().Pods("monitoring").List(context.Background(), metav1.ListOptions{LabelSelector: deepPodLabelKey + "=" + cleanPodLabelVal})
	return len(l.Items)
}

var cleanReq = CleanupRequest{Cluster: "ctx", Namespace: "monitoring", Name: "prom",
	Targets: []Target{{Pod: "prometheus-prom-0", PVC: "data-prometheus-prom-0"}}}

func TestExecuteFluxoCompleto(t *testing.T) {
	e := newCleanEnv(t)
	var steps []string
	res, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, func(s Step) { steps = append(steps, s.Key) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restored || !res.Ready || len(res.Cleaned) != 1 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if got := res.Cleaned[0]; got.BlocksPreserved != 3 || got.WALBytes != 2048*1024 || got.LastBlockEndsAt == nil {
		t.Errorf("resultado do alvo errado: %+v", got)
	}
	if _, ok := e.annotations[AnnotationOriginalReplicas]; ok {
		t.Error("a anotação de restauração deveria ter sido removida")
	}
	if e.replicas != 2 {
		t.Errorf("réplicas finais = %d, esperado 2", e.replicas)
	}
	if e.tempPods() != 0 {
		t.Error("pod temporário de limpeza não foi removido")
	}
	if !e.has("rm -rf /prometheus/wal/*") {
		t.Error("o rm do wal/ não foi executado")
	}
	// A ordem importa: anotar → desligar → apagar → religar.
	idx := func(sub string) int {
		for i, c := range e.calls {
			if strings.Contains(c, sub) {
				return i
			}
		}
		return -1
	}
	a, d, w, r := idx(`"annotations":{"`+AnnotationOriginalReplicas+`":"2"`), idx(`"replicas":0`), idx("rm -rf"), idx(`"replicas":2`)
	if !(a >= 0 && a < d && d < w && w < r) {
		t.Errorf("ordem errada (anotar=%d desligar=%d apagar=%d religar=%d):\n%s", a, d, w, r, strings.Join(e.calls, "\n"))
	}
	if steps[len(steps)-1] != "done" {
		t.Errorf("último passo = %v", steps)
	}
}

func TestExecuteReligaSempreEmErro(t *testing.T) {
	e := newCleanEnv(t)
	e.failExec = "rm -rf"
	res, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, nil)
	if err == nil || !strings.Contains(err.Error(), "apagando") {
		t.Fatalf("esperava erro de exec, veio %v", err)
	}
	if !res.Restored || e.replicas != 2 {
		t.Fatalf("deveria religar mesmo com erro: restored=%v replicas=%d", res.Restored, e.replicas)
	}
	if e.tempPods() != 0 {
		t.Error("pod temporário vazou")
	}
}

func TestExecuteReligaSempreEmCancelamento(t *testing.T) {
	e := newCleanEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.onExec = cancel // cancela no meio da limpeza
	_, err := e.collector(ctx).Execute(ctx, cleanReq, nil)
	if err == nil {
		t.Fatal("esperava erro após cancelamento")
	}
	if e.replicas != 2 || e.has(`"replicas":0`) == false {
		t.Fatalf("deveria ter desligado e religado: replicas=%d", e.replicas)
	}
	if _, ok := e.annotations[AnnotationOriginalReplicas]; ok {
		t.Error("anotação deveria ter sido removida ao religar")
	}
}

func TestExecuteRecusaLimpezaPendente(t *testing.T) {
	e := newCleanEnv(t)
	e.annotations[AnnotationOriginalReplicas] = "2"
	_, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, nil)
	if err == nil || !strings.Contains(err.Error(), "interrompida") {
		t.Fatalf("esperava recusa por limpeza pendente, veio %v", err)
	}
	if e.has("patch") {
		t.Error("não deveria alterar nada com limpeza pendente")
	}
}

func TestExecuteSemPermissaoNaoDesliga(t *testing.T) {
	e := newCleanEnv(t)
	e.allow = false
	_, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, nil)
	if err == nil || !strings.Contains(err.Error(), "sem permissão") {
		t.Fatalf("esperava erro de permissão, veio %v", err)
	}
	if e.has("patch") || e.replicas != 2 {
		t.Error("sem permissão não pode desligar o Prometheus")
	}
}

func TestRestore(t *testing.T) {
	e := newCleanEnv(t)
	c := e.collector(context.Background())
	if _, err := c.Restore(context.Background(), "ctx", "monitoring", "prom"); err == nil {
		t.Fatal("sem anotação deveria recusar")
	}
	e.annotations[AnnotationOriginalReplicas] = "2"
	e.replicas = 0
	n, err := c.Restore(context.Background(), "ctx", "monitoring", "prom")
	if err != nil || n != 2 || e.replicas != 2 {
		t.Fatalf("restore: n=%d err=%v replicas=%d", n, err, e.replicas)
	}
}

func TestAnalisePendenteDeRestauracao(t *testing.T) {
	r := Analyze(Input{PendingRestore: true, PendingRestoreReplicas: 2, Pods: []PodInfo{{Name: "p", Ready: true}}}, testNow)
	if r.Verdict != VerdictInvestigate || r.PendingRestoreReplicas == nil || *r.PendingRestoreReplicas != 2 {
		t.Fatalf("pendência deveria virar investigar com réplicas=2: %+v", r)
	}
	if parseReplicas("x") != 1 || parseReplicas("3") != 3 {
		t.Error("parseReplicas")
	}
}

func TestExecuteGravaContadorDeReincidencia(t *testing.T) {
	base := testNow
	now = func() time.Time { return base }
	t.Cleanup(func() { now = time.Now })

	e := newCleanEnv(t)
	// Duas limpezas anteriores já registradas na anotação.
	e.annotations[AnnotationCleanupCount] = "5"
	e.annotations[AnnotationCleanupHistory] = base.Add(-72*time.Hour).UTC().Format(time.RFC3339) + "," + base.Add(-24*time.Hour).UTC().Format(time.RFC3339)

	if _, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.annotations[AnnotationCleanupCount]; got != "6" {
		t.Errorf("contador = %q, esperado 6", got)
	}
	if got := e.annotations[AnnotationCleanupLastAt]; got != base.UTC().Format(time.RFC3339) {
		t.Errorf("last-at = %q", got)
	}
	if dates := parseDates(e.annotations[AnnotationCleanupHistory]); len(dates) != 3 {
		t.Errorf("histórico deveria ter 3 datas: %v", e.annotations[AnnotationCleanupHistory])
	}
	if _, ok := e.annotations[AnnotationOriginalReplicas]; ok {
		t.Error("anotação de restauração deveria ter sido removida")
	}
}

func TestExecuteSemApagarNaoConta(t *testing.T) {
	e := newCleanEnv(t)
	e.failExec = "rm -rf" // falha antes de apagar qualquer coisa
	if _, err := e.collector(context.Background()).Execute(context.Background(), cleanReq, nil); err == nil {
		t.Fatal("esperava erro")
	}
	if _, ok := e.annotations[AnnotationCleanupCount]; ok {
		t.Error("limpeza que não apagou nada não pode contar como recuperação")
	}
}

func TestExecuteParcialConta(t *testing.T) {
	e := newCleanEnv(t)
	req := cleanReq
	req.Targets = []Target{{Pod: "prometheus-prom-0", PVC: "data-prometheus-prom-0"}, {Pod: "prometheus-prom-1", PVC: "data-prometheus-prom-1"}}
	n := 0
	e.onExec = func() {} // o 2º PVC falha no rm
	run := e.collector(context.Background())
	inner := run.Run
	run.Run = func(ctx context.Context, c, ns string, args ...string) ([]byte, error) {
		if args[0] == "exec" && strings.Contains(args[len(args)-1], "rm -rf") {
			if n++; n == 2 {
				return nil, errors.New("exec falhou")
			}
		}
		return inner(ctx, c, ns, args...)
	}
	res, err := run.Execute(context.Background(), req, nil)
	if err == nil || len(res.Cleaned) != 1 {
		t.Fatalf("esperava limpeza parcial com erro: cleaned=%d err=%v", len(res.Cleaned), err)
	}
	if e.annotations[AnnotationCleanupCount] != "1" {
		t.Errorf("limpeza parcial (algo foi apagado) deveria contar: %v", e.annotations)
	}
}
