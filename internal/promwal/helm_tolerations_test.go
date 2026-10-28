package promwal

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const testManifest = `---
apiVersion: v1
kind: Secret
metadata:
  name: prometheus-grafana
data:
  admin-password: c2VjcmV0bw==
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus-kube-state-metrics
spec:
  template:
    spec:
      tolerations:
        - effect: NoSchedule
          key: prometheus-only
          operator: Equal
          value: "true"
        - effect: NoSchedule
          key: kubernetes.azure.com/scalesetpriority
          operator: Equal
          value: spot
---
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: prometheus-kube-prometheus-prometheus
spec:
  nodeSelector:
    agentpool: prometheus
  resources:
    requests: {cpu: 500m, memory: 2Gi}
    limits: {memory: 4Gi}
  tolerations:
    - key: dedicated
      operator: Exists
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: sem-tolerations
`

func TestWorkloadsWithTolerations(t *testing.T) {
	got := workloadsWithTolerations(testManifest)
	if len(got) != 2 {
		t.Fatalf("esperava 2 recursos com tolerations (sem Secret/ConfigMap), veio %+v", got)
	}
	if got[0].Kind != "Deployment" || got[0].Tolerations[0] != "prometheus-only=true:NoSchedule" ||
		got[0].Tolerations[1] != "kubernetes.azure.com/scalesetpriority=spot:NoSchedule" {
		t.Fatalf("Deployment = %+v", got[0])
	}
	if got[1].Kind != "Prometheus" || got[1].Tolerations[0] != "dedicated" || len(got[1].NodeSelector) != 1 || got[1].NodeSelector[0] != "agentpool=prometheus" ||
		len(got[1].Resources) != 1 || got[1].Resources[0] != "requests cpu=500m mem=2Gi · limits mem=4Gi" {
		t.Fatalf("Prometheus = %+v", got[1])
	}
}

func TestDefaultTolerationsYAML_EhValido(t *testing.T) {
	r := HelmTolerationsRequest{Namespace: "monitoring", Release: "prometheus", ValuesYAML: DefaultTolerationsYAML}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Chart != DefaultHelmChart || r.Repo != DefaultHelmRepo {
		t.Fatalf("defaults não aplicados: %+v", r)
	}
}

func TestHelmValidate_RejeitaEntradasPerigosas(t *testing.T) {
	base := HelmTolerationsRequest{Namespace: "monitoring", Release: "prometheus", ValuesYAML: "a: 1"}
	for name, mut := range map[string]func(*HelmTolerationsRequest){
		"release como flag": func(r *HelmTolerationsRequest) { r.Release = "--set" },
		"namespace":         func(r *HelmTolerationsRequest) { r.Namespace = "a b" },
		"versão":            func(r *HelmTolerationsRequest) { r.Version = "-1" },
		"repo http":         func(r *HelmTolerationsRequest) { r.Repo = "http://x.io" },
		"repo local":        func(r *HelmTolerationsRequest) { r.Repo = "file:///etc" },
		"yaml vazio":        func(r *HelmTolerationsRequest) { r.ValuesYAML = " " },
		"yaml não-mapa":     func(r *HelmTolerationsRequest) { r.ValuesYAML = "- a\n- b" },
	} {
		r := base
		mut(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestApplyHelmTolerations_ArgsEResumoSemSegredo(t *testing.T) {
	var args []string
	var override string
	run := func(_ context.Context, a ...string) ([]byte, error) {
		args = a
		for i, x := range a {
			if x == "-f" {
				b, _ := os.ReadFile(a[i+1])
				override = string(b)
			}
		}
		out, _ := json.Marshal(map[string]interface{}{
			"name": "prometheus", "version": 7,
			"info":     map[string]string{"status": "pending-upgrade"},
			"chart":    map[string]interface{}{"metadata": map[string]string{"version": "45.31.1"}},
			"manifest": testManifest,
		})
		return out, nil
	}
	res, err := ApplyHelmTolerations(context.Background(), run, HelmTolerationsRequest{
		Namespace: "monitoring", Release: "prometheus", Version: "45.31.1", ValuesYAML: DefaultTolerationsYAML, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"upgrade prometheus kube-prometheus-stack", "--repo " + DefaultHelmRepo, "--version 45.31.1",
		"-n monitoring", "--reuse-values", "--dry-run", "-o json"} {
		if !strings.Contains(line, want) {
			t.Errorf("faltou %q em: %s", want, line)
		}
	}
	if override != DefaultTolerationsYAML {
		t.Fatal("o override gravado no -f difere do YAML enviado")
	}
	if res.Revision != 7 || res.ChartVersion != "45.31.1" || !res.DryRun || len(res.Workloads) != 2 {
		t.Fatalf("resultado = %+v", res)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "c2VjcmV0bw") || strings.Contains(string(b), "admin-password") {
		t.Fatal("o resumo vazou conteúdo do Secret")
	}
	if !strings.Contains(res.Command, "helm upgrade prometheus kube-prometheus-stack --repo") || !strings.Contains(res.Command, "--dry-run") {
		t.Fatalf("command = %s", res.Command)
	}
}

func TestDetectHelmRelease(t *testing.T) {
	run := func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"name":"outro","chart":"nginx-1.0.0","status":"deployed"},{"name":"prometheus","chart":"kube-prometheus-stack-45.31.1","status":"deployed"}]`), nil
	}
	r, err := DetectHelmRelease(context.Background(), run, "monitoring")
	if err != nil || r == nil || r.Release != "prometheus" || r.ChartVersion != "45.31.1" {
		t.Fatalf("release = %+v, err = %v", r, err)
	}
}

func TestUserTolerations_TiraAsAutomaticasDoApiserver(t *testing.T) {
	secs := int64(300)
	in := []corev1.Toleration{
		{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: &secs},
		{Key: "prometheus-only", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule},
	}
	got := userTolerations(in)
	if len(got) != 1 || got[0].Key != "prometheus-only" {
		t.Fatalf("got %+v", got)
	}
}

func TestHelmHistory_E_Rollback(t *testing.T) {
	var got [][]string
	run := func(_ context.Context, a ...string) ([]byte, error) {
		got = append(got, a)
		if a[0] == "history" {
			return []byte(`[{"revision":1,"updated":"2026-10-01T10:00:00Z","status":"superseded","chart":"kube-prometheus-stack-45.31.1","app_version":"v0.63.0","description":"Install complete"},{"revision":2,"updated":"2026-10-28T10:00:00Z","status":"deployed","chart":"kube-prometheus-stack-45.31.1","app_version":"v0.63.0","description":"Upgrade complete"}]`), nil
		}
		return nil, nil
	}
	revs, err := HelmHistory(context.Background(), run, "monitoring", "prometheus")
	if err != nil || len(revs) != 2 || revs[1].Revision != 2 || revs[1].Status != "deployed" {
		t.Fatalf("revs = %+v, err = %v", revs, err)
	}
	if err := HelmRollback(context.Background(), run, "monitoring", "prometheus", 1, true); err != nil {
		t.Fatal(err)
	}
	if line := strings.Join(got[1], " "); line != "rollback prometheus 1 -n monitoring --dry-run" {
		t.Fatalf("rollback = %s", line)
	}
	if HelmRollback(context.Background(), run, "monitoring", "--set", 1, false) == nil || HelmRollback(context.Background(), run, "monitoring", "prometheus", 0, false) == nil {
		t.Fatal("deveria rejeitar release com cara de flag e revisão 0")
	}
}

func TestHelmEnv_IsolaARepositoriesYaml(t *testing.T) {
	env := helmEnv("/tmp/x")
	var got string
	for _, e := range env {
		if strings.HasPrefix(e, "HELM_REPOSITORY_CONFIG=") {
			got = e
		}
	}
	if got != "HELM_REPOSITORY_CONFIG=/tmp/x/repositories.yaml" {
		t.Fatalf("HELM_REPOSITORY_CONFIG = %q", got)
	}
}
