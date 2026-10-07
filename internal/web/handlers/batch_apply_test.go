package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Mesmo formato do ~/scripts/cronjobs/restart-kafka-consumidor.yaml (SA + Role + RoleBinding +
// CronJob num arquivo só), fora de ordem de propósito para testar a ordenação.
const batchSampleYAML = `apiVersion: batch/v1
kind: CronJob
metadata:
  name: restart-kafka-consumidor-new
  namespace: adanalytics-prd
spec:
  schedule: "15,45 * * * 1-5"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: restart-kafka-consumidor-new
          restartPolicy: Never
          containers:
            - name: kubectl
              image: registry.k8s.io/kubectl:v1.30.0
---
# comentário solto entre documentos
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: restart-kafka-consumidor-new
  namespace: adanalytics-prd
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: restart-kafka-consumidor-new
  namespace: adanalytics-prd
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: restart-kafka-consumidor-new
subjects:
  - kind: ServiceAccount
    name: restart-kafka-consumidor-new
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: restart-kafka-consumidor-new
  namespace: adanalytics-prd
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "patch"]
`

func TestParseBatchManifest_MultiDoc(t *testing.T) {
	objs, ns, _, err := parseBatchManifest(batchSampleYAML, "adanalytics-prd")
	if err != nil {
		t.Fatal(err)
	}
	if ns != "adanalytics-prd" || len(objs) != 4 {
		t.Fatalf("ns=%q objs=%d", ns, len(objs))
	}
	got := []string{}
	for _, u := range objs {
		got = append(got, u.GetKind())
	}
	if strings.Join(got, ",") != "ServiceAccount,Role,RoleBinding,CronJob" {
		t.Errorf("ordem de aplicação = %v", got)
	}
	// subject ServiceAccount sem namespace recebe o namespace do manifesto
	subj := objs[2].Object["subjects"].([]interface{})[0].(map[string]interface{})
	if subj["namespace"] != "adanalytics-prd" {
		t.Errorf("subject namespace = %v", subj["namespace"])
	}
}

func TestParseBatchManifest_Namespace(t *testing.T) {
	// sem seletor: usa o namespace declarado no YAML
	if _, ns, _, err := parseBatchManifest(batchSampleYAML, ""); err != nil || ns != "adanalytics-prd" {
		t.Errorf("ns=%q err=%v", ns, err)
	}
	// seletor diferente do YAML: recusa em vez de mover
	if _, _, _, err := parseBatchManifest(batchSampleYAML, "outro-ns"); err == nil || !strings.Contains(err.Error(), "outro-ns") {
		t.Errorf("deveria recusar namespace divergente: %v", err)
	}
	// sem namespace nenhum
	job := "apiVersion: batch/v1\nkind: Job\nmetadata:\n  generateName: x-\nspec: {}\n"
	if _, _, _, err := parseBatchManifest(job, ""); err == nil {
		t.Error("deveria exigir namespace")
	}
	// documento sem namespace recebe o do seletor
	objs, ns, _, err := parseBatchManifest(job, "ns1")
	if err != nil || ns != "ns1" || objs[0].GetNamespace() != "ns1" {
		t.Errorf("ns=%q err=%v", ns, err)
	}
}

func TestParseBatchManifest_Rejects(t *testing.T) {
	cases := map[string]string{
		"vazio":          "   \n",
		"só comentário":  "# nada\n---\n",
		"kind proibido":  "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: x\n",
		"sem workload":   "apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: x\n  namespace: a\n",
		"apiVersion":     "apiVersion: batch/v1beta1\nkind: CronJob\nmetadata:\n  name: x\n  namespace: a\n",
		"sem nome":       "apiVersion: batch/v1\nkind: CronJob\nmetadata:\n  namespace: a\n",
		"yaml quebrado":  "apiVersion: batch/v1\nkind: [Job\n",
		"sem apiVersion": "kind: Job\nmetadata:\n  name: x\n",
	}
	for name, y := range cases {
		if _, _, _, err := parseBatchManifest(y, "a"); err == nil {
			t.Errorf("%s: deveria dar erro", name)
		}
	}
}

func TestParseAdmissionViolations(t *testing.T) {
	gk := `admission webhook "validation.gatekeeper.sh" denied the request: [require-owner-label] you must provide labels: {"owner"}
[block-latest-tag] container <kubectl> usa a tag latest`
	v := parseAdmissionViolations(gk)
	if len(v) != 2 || v[0].Engine != "Gatekeeper" || v[0].Policy != "require-owner-label" || v[1].Policy != "block-latest-tag" {
		t.Errorf("gatekeeper: %+v", v)
	}

	ky := "admission webhook \"validate.kyverno.svc-fail\" denied the request: \n\nresource CronJob/adanalytics-prd/x was blocked due to the following policies \n\nrequire-requests-limits:\n  validate-resources: 'validation error: CPU and memory resource requests and limits are required. rule validate-resources failed at path /spec/jobTemplate/spec/template/spec/containers/0/resources/limits/'\ndisallow-latest-tag:\n  validate-image-tag: 'validation error: Using a mutable image tag e.g. latest is not allowed.'\n"
	v = parseAdmissionViolations(ky)
	if len(v) != 2 || v[0].Engine != "Kyverno" || v[0].Policy != "require-requests-limits" || v[0].Rule != "validate-resources" || !strings.HasPrefix(v[0].Message, "validation error: CPU") || v[1].Policy != "disallow-latest-tag" {
		t.Errorf("kyverno: %+v", v)
	}

	if v := parseAdmissionViolations(`admission webhook "x.example.com" denied the request: nope`); len(v) != 1 || v[0].Engine != "webhook" || v[0].Message != "nope" {
		t.Errorf("genérico: %+v", v)
	}
	if v := parseAdmissionViolations("cronjobs.batch \"x\" is forbidden: User cannot create"); v != nil {
		t.Errorf("erro sem webhook não é violação: %+v", v)
	}
}

// fakeAPI imita o suficiente da API do Kubernetes para o fluxo de apply: GET (existe ou 404),
// PATCH de server-side apply e POST, registrando cada chamada. `deny` simula um webhook do
// Kyverno recusando o recurso com esse nome.
type fakeAPI struct {
	mu       sync.Mutex
	existing map[string]bool // "resource/name"
	deny     string
	calls    []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // .../namespaces/<ns>/<resource>[/<name>]
	resource, name := "", ""
	for i, p := range parts {
		if p == "namespaces" && i+2 < len(parts) {
			resource = parts[i+2]
			if i+3 < len(parts) {
				name = parts[i+3]
			}
		}
	}
	q := r.URL.Query()
	f.calls = append(f.calls, fmt.Sprintf("%s %s/%s dryRun=%s fv=%s fm=%s", r.Method, resource, name, q.Get("dryRun"), q.Get("fieldValidation"), q.Get("fieldManager")))
	status := func(code int, reason, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": reason, "message": msg, "code": code})
	}
	switch r.Method {
	case http.MethodGet:
		if !f.existing[resource+"/"+name] {
			status(404, "NotFound", resource+" not found")
			return
		}
	case http.MethodPatch, http.MethodPost:
		if name == f.deny && name != "" {
			status(400, "BadRequest", "admission webhook \"validate.kyverno.svc-fail\" denied the request: \n\nresource blocked\n\nrequire-labels:\n  check-owner: 'validation error: label owner is required.'\n")
			return
		}
		if q.Get("dryRun") == "" {
			f.existing[resource+"/"+name] = true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"ServiceAccount","metadata":{"name":"x"}}`))
}

func newFakeAPI(t *testing.T, f *fakeAPI) kubernetes.Interface {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestBatchApplyOne_DryRunAndApply(t *testing.T) {
	f := &fakeAPI{existing: map[string]bool{"serviceaccounts/restart-kafka-consumidor-new": true}}
	cs := newFakeAPI(t, f)
	objs, _, _, err := parseBatchManifest(batchSampleYAML, "adanalytics-prd")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// dry-run: SA já existe (update), o resto é create; nada é gravado
	for _, u := range objs {
		r := batchApplyOne(ctx, cs, u, true)
		want := "create"
		if u.GetKind() == "ServiceAccount" {
			want = "update"
		}
		if r.Status != "ok" || r.Action != want {
			t.Errorf("dry-run %s: %+v", u.GetKind(), r)
		}
	}
	if f.existing["cronjobs/restart-kafka-consumidor-new"] {
		t.Fatal("dry-run não pode gravar")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "PATCH") && (!strings.Contains(c, "dryRun=All") || !strings.Contains(c, "fv=Strict") || !strings.Contains(c, "fm=k8s-hpa-manager")) {
			t.Errorf("chamada de dry-run sem os parâmetros esperados: %s", c)
		}
	}

	// apply de verdade
	for _, u := range objs {
		if r := batchApplyOne(ctx, cs, u, false); r.Status != "ok" {
			t.Errorf("apply %s: %+v", u.GetKind(), r)
		}
	}
	if !f.existing["cronjobs/restart-kafka-consumidor-new"] || !f.existing["roles/restart-kafka-consumidor-new"] {
		t.Errorf("recursos não aplicados: %v", f.existing)
	}
}

func TestBatchApplyOne_PolicyDenied(t *testing.T) {
	f := &fakeAPI{existing: map[string]bool{}, deny: "restart-kafka-consumidor-new"}
	cs := newFakeAPI(t, f)
	objs, _, _, _ := parseBatchManifest(batchSampleYAML, "adanalytics-prd")
	r := batchApplyOne(context.Background(), cs, objs[3], true) // CronJob
	if r.Status != "error" || len(r.Violations) != 1 || r.Violations[0].Engine != "Kyverno" || r.Violations[0].Rule != "check-owner" || r.Hint == "" {
		t.Errorf("violação do Kyverno não interpretada: %+v", r)
	}
}

func TestBatchApplyOne_ExistingJobAndGenerateName(t *testing.T) {
	f := &fakeAPI{existing: map[string]bool{"jobs/meu-job": true}}
	cs := newFakeAPI(t, f)
	ctx := context.Background()

	objs, _, _, _ := parseBatchManifest("apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: meu-job\nspec: {}\n", "a")
	if r := batchApplyOne(ctx, cs, objs[0], true); r.Status != "error" || !strings.Contains(r.Error, "já existe") || r.Hint == "" {
		t.Errorf("Job existente deveria ser recusado: %+v", r)
	}

	objs, _, _, _ = parseBatchManifest("apiVersion: batch/v1\nkind: Job\nmetadata:\n  generateName: meu-job-\nspec: {}\n", "a")
	if r := batchApplyOne(ctx, cs, objs[0], true); r.Status != "ok" || r.Action != "create" {
		t.Errorf("Job com generateName: %+v", r)
	}
	if last := f.calls[len(f.calls)-1]; !strings.HasPrefix(last, "POST jobs/") || !strings.Contains(last, "dryRun=All") {
		t.Errorf("generateName deveria usar POST com dry-run: %s", last)
	}
}

func TestBatchReferenceWarnings(t *testing.T) {
	f := &fakeAPI{existing: map[string]bool{}}
	cs := newFakeAPI(t, f)
	// só o CronJob, sem a SA/Role no YAML e sem existir no cluster
	cj := strings.SplitN(batchSampleYAML, "---", 2)[0]
	objs, _, _, err := parseBatchManifest(cj, "adanalytics-prd")
	if err != nil {
		t.Fatal(err)
	}
	w := batchReferenceWarnings(context.Background(), cs, objs)
	if len(w) != 1 || !strings.Contains(w[0], "ServiceAccount") {
		t.Errorf("deveria avisar da ServiceAccount inexistente: %v", w)
	}
	// com o manifesto completo, nada a avisar
	objs, _, _, _ = parseBatchManifest(batchSampleYAML, "adanalytics-prd")
	if w := batchReferenceWarnings(context.Background(), cs, objs); len(w) != 0 {
		t.Errorf("manifesto completo não deveria gerar aviso: %v", w)
	}
}
