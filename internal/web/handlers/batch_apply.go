package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	sigsyaml "sigs.k8s.io/yaml"
)

// Criação de Job/CronJob a partir de YAML (botão "Novo" da aba CronJobs).
//
// Aceita manifesto com vários documentos — o caso comum é o CronJob vir junto com o que ele
// precisa para rodar (ServiceAccount, Role, RoleBinding, ConfigMap), como num `kubectl apply -f`.
// Fluxo em duas fases: (1) dry-run no servidor de TODOS os documentos — o que já passa pelos
// webhooks de admissão (Gatekeeper/Kyverno rodam no dry-run) — e (2) só se tudo passar, aplica de
// verdade, em ordem de dependência. Aplica com server-side apply e validação estrita de campos
// (mesmo comportamento do `kubectl apply`: cria ou atualiza; campo desconhecido é erro).

const batchFieldManager = "k8s-hpa-manager"

type batchKindInfo struct {
	apiVersion string
	resource   string
	order      int // ordem de aplicação (dependências primeiro)
	client     func(kubernetes.Interface) rest.Interface
}

// Só recursos de namespace que um Job/CronJob costuma precisar. Fora disso (cluster-scoped,
// Deployments, Secrets...) a tela não é o lugar: o pedido é recusado com explicação.
var batchKinds = map[string]batchKindInfo{
	"ServiceAccount": {"v1", "serviceaccounts", 0, func(c kubernetes.Interface) rest.Interface { return c.CoreV1().RESTClient() }},
	"ConfigMap":      {"v1", "configmaps", 1, func(c kubernetes.Interface) rest.Interface { return c.CoreV1().RESTClient() }},
	"Role":           {"rbac.authorization.k8s.io/v1", "roles", 2, func(c kubernetes.Interface) rest.Interface { return c.RbacV1().RESTClient() }},
	"RoleBinding":    {"rbac.authorization.k8s.io/v1", "rolebindings", 3, func(c kubernetes.Interface) rest.Interface { return c.RbacV1().RESTClient() }},
	"Job":            {"batch/v1", "jobs", 4, func(c kubernetes.Interface) rest.Interface { return c.BatchV1().RESTClient() }},
	"CronJob":        {"batch/v1", "cronjobs", 4, func(c kubernetes.Interface) rest.Interface { return c.BatchV1().RESTClient() }},
}

const batchAllowedKinds = "ServiceAccount, ConfigMap, Role, RoleBinding, Job e CronJob"

type batchApplyRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	YAML      string `json:"yaml"`
	DryRun    bool   `json:"dry_run"`
}

// BatchViolation é uma violação de política devolvida por um webhook de admissão.
type BatchViolation struct {
	Engine  string `json:"engine"`           // Gatekeeper | Kyverno | webhook
	Policy  string `json:"policy,omitempty"` // constraint (Gatekeeper) ou policy (Kyverno)
	Rule    string `json:"rule,omitempty"`   // regra (Kyverno)
	Message string `json:"message"`
}

// BatchResourceResult é o resultado de um documento do manifesto.
type BatchResourceResult struct {
	Kind       string           `json:"kind"`
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace"`
	Action     string           `json:"action"` // create | update
	Status     string           `json:"status"` // ok | error | pending (não aplicado porque um anterior falhou)
	Error      string           `json:"error,omitempty"`
	Hint       string           `json:"hint,omitempty"`
	Violations []BatchViolation `json:"violations,omitempty"`
}

type batchApplyResponse struct {
	Success   bool                  `json:"success"`
	DryRun    bool                  `json:"dry_run"`
	Applied   bool                  `json:"applied"` // false = parou na validação (nada foi alterado no cluster)
	Namespace string                `json:"namespace"`
	Resources []BatchResourceResult `json:"resources"`
	Warnings  []string              `json:"warnings,omitempty"`
}

// ApplyBatchManifest — POST /api/v1/batch/apply (também atende /jobs e /cronjobs/new)
// Body: { cluster, namespace, yaml, dry_run }
func (h *CronJobHandler) ApplyBatchManifest(c *gin.Context) {
	var req batchApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'cluster' é obrigatório"})
		return
	}
	objs, ns, warnings, err := parseBatchManifest(req.YAML, req.Namespace)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}
	ctx := c.Request.Context()
	start := time.Now()
	warnings = append(warnings, batchReferenceWarnings(ctx, client, objs)...)

	resp := batchApplyResponse{DryRun: req.DryRun, Namespace: ns, Warnings: warnings, Resources: make([]BatchResourceResult, len(objs))}

	// Fase 1: dry-run de tudo (nada muda no cluster). Para na validação se algo falhar.
	valid := true
	for i, u := range objs {
		resp.Resources[i] = batchApplyOne(ctx, client, u, true)
		if resp.Resources[i].Status != "ok" {
			valid = false
		}
	}
	if !valid || req.DryRun {
		resp.Success = valid
		c.JSON(http.StatusOK, resp)
		return
	}

	// Fase 2: aplica de verdade, em ordem. Sem rollback: se um falhar, os seguintes ficam
	// "pending" e a resposta diz exatamente o que já foi aplicado.
	resp.Applied, resp.Success = true, true
	var failMsg string
	for i, u := range objs {
		resp.Resources[i] = batchApplyOne(ctx, client, u, false)
		if resp.Resources[i].Status != "ok" {
			resp.Success = false
			failMsg = resp.Resources[i].Error
			for j := i + 1; j < len(objs); j++ {
				resp.Resources[j] = BatchResourceResult{Kind: objs[j].GetKind(), Name: batchObjName(objs[j]), Namespace: ns, Status: "pending"}
			}
			break
		}
	}

	if h.historyTracker != nil {
		names := make([]string, 0, len(resp.Resources))
		for _, r := range resp.Resources {
			names = append(names, fmt.Sprintf("%s/%s:%s:%s", r.Kind, r.Name, r.Action, r.Status))
		}
		status := "success"
		if !resp.Success {
			status = "failed"
		}
		entry := CreateHistoryEntry(c, "apply_batch_manifest", ns+"/"+batchMainName(objs), req.Cluster, status, nil,
			map[string]interface{}{"resources": names, "yaml": req.YAML}, time.Since(start).Milliseconds(), failMsg)
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}
	c.JSON(http.StatusOK, resp)
}

// parseBatchManifest separa os documentos, valida kinds/namespace e devolve os objetos em ordem
// de aplicação, todos já com o namespace final.
func parseBatchManifest(content, namespace string) ([]*unstructured.Unstructured, string, []string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, "", nil, errors.New("o YAML está vazio")
	}
	var objs []*unstructured.Unstructured
	var warnings []string
	reader := yamlutil.NewYAMLReader(bufio.NewReader(strings.NewReader(content)))
	for docN := 1; ; docN++ {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", nil, fmt.Errorf("documento %d: YAML inválido: %v", docN, err)
		}
		js, err := sigsyaml.YAMLToJSON(raw)
		if err != nil {
			return nil, "", nil, fmt.Errorf("documento %d: YAML inválido: %v", docN, err)
		}
		if t := strings.TrimSpace(string(js)); t == "null" || t == "" {
			docN-- // documento vazio (só comentários ou "---" sobrando) não conta
			continue
		}
		u := &unstructured.Unstructured{}
		if err := u.UnmarshalJSON(js); err != nil {
			return nil, "", nil, fmt.Errorf("documento %d: %v (todo documento precisa de apiVersion e kind)", docN, err)
		}
		kind := u.GetKind()
		info, ok := batchKinds[kind]
		if !ok {
			return nil, "", nil, fmt.Errorf("documento %d: kind %q não é aceito aqui — permitidos: %s (sempre no namespace). Recursos de cluster (ClusterRole, ClusterRoleBinding, Namespace) e workloads como Deployment devem ir pelo fluxo normal de deploy", docN, kind, batchAllowedKinds)
		}
		if u.GetAPIVersion() != info.apiVersion {
			return nil, "", nil, fmt.Errorf("documento %d: %s com apiVersion %q — use %q", docN, kind, u.GetAPIVersion(), info.apiVersion)
		}
		if u.GetName() == "" && !(kind == "Job" && u.GetGenerateName() != "") {
			return nil, "", nil, fmt.Errorf("documento %d: %s sem metadata.name", docN, kind)
		}
		objs = append(objs, u)
	}
	if len(objs) == 0 {
		return nil, "", nil, errors.New("nenhum recurso encontrado no YAML")
	}

	// Namespace: o do seletor (ou, sem seletor, o do 1º documento que declarar) vale para todos.
	// Documento que declara OUTRO namespace é recusado em vez de movido em silêncio: referências
	// internas (ex: subjects da RoleBinding) continuariam apontando para o namespace antigo.
	target := namespace
	if target == "" {
		for _, u := range objs {
			if u.GetNamespace() != "" {
				target = u.GetNamespace()
				break
			}
		}
	}
	if target == "" {
		return nil, "", nil, errors.New("namespace é obrigatório: selecione um namespace ou informe metadata.namespace no YAML")
	}
	hasWorkload := false
	for _, u := range objs {
		if ns := u.GetNamespace(); ns != "" && ns != target {
			return nil, "", nil, fmt.Errorf("%s/%s está no namespace %q, mas o namespace selecionado é %q — ajuste o YAML ou o seletor (não movemos sozinho para não deixar referências, como os subjects da RoleBinding, apontando para o namespace errado)", u.GetKind(), batchObjName(u), ns, target)
		}
		u.SetNamespace(target)
		u.SetManagedFields(nil)
		u.SetResourceVersion("")
		u.SetUID("")
		if k := u.GetKind(); k == "Job" || k == "CronJob" {
			hasWorkload = true
		}
		if u.GetKind() == "RoleBinding" {
			warnings = append(warnings, fillRoleBindingSubjects(u, target)...)
		}
	}
	if !hasWorkload {
		return nil, "", nil, errors.New("o YAML precisa ter pelo menos um Job ou CronJob")
	}
	sort.SliceStable(objs, func(i, j int) bool { return batchKinds[objs[i].GetKind()].order < batchKinds[objs[j].GetKind()].order })
	return objs, target, warnings, nil
}

// fillRoleBindingSubjects completa o namespace de subjects ServiceAccount sem namespace
// (obrigatório para ServiceAccount) e avisa quando apontam para outro namespace.
func fillRoleBindingSubjects(u *unstructured.Unstructured, ns string) []string {
	subjects, found, _ := unstructured.NestedSlice(u.Object, "subjects")
	if !found {
		return nil
	}
	var warnings []string
	for i, s := range subjects {
		m, ok := s.(map[string]interface{})
		if !ok || m["kind"] != "ServiceAccount" {
			continue
		}
		switch sns, _ := m["namespace"].(string); {
		case sns == "":
			m["namespace"] = ns
			subjects[i] = m
		case sns != ns:
			warnings = append(warnings, fmt.Sprintf("RoleBinding %s: o subject ServiceAccount %v está no namespace %q, diferente de %q", u.GetName(), m["name"], sns, ns))
		}
	}
	_ = unstructured.SetNestedSlice(u.Object, subjects, "subjects")
	return warnings
}

// batchReferenceWarnings avisa sobre ServiceAccount/Role referenciados que não estão no
// manifesto nem existem no cluster — o recurso é aceito, mas o pod/binding não funcionaria.
func batchReferenceWarnings(ctx context.Context, client kubernetes.Interface, objs []*unstructured.Unstructured) []string {
	inBundle := map[string]bool{}
	for _, u := range objs {
		inBundle[u.GetKind()+"/"+u.GetName()] = true
	}
	exists := func(kind, ns, name string) bool {
		if inBundle[kind+"/"+name] {
			return true
		}
		info := batchKinds[kind]
		err := info.client(client).Get().Namespace(ns).Resource(info.resource).Name(name).Do(ctx).Error()
		return err == nil || !apierrors.IsNotFound(err) // sem permissão de leitura: não acusa
	}
	var warnings []string
	for _, u := range objs {
		ns := u.GetNamespace()
		var podSpec []string
		switch u.GetKind() {
		case "CronJob":
			podSpec = []string{"spec", "jobTemplate", "spec", "template", "spec"}
		case "Job":
			podSpec = []string{"spec", "template", "spec"}
		case "RoleBinding":
			if kind, _, _ := unstructured.NestedString(u.Object, "roleRef", "kind"); kind == "Role" {
				if name, _, _ := unstructured.NestedString(u.Object, "roleRef", "name"); name != "" && !exists("Role", ns, name) {
					warnings = append(warnings, fmt.Sprintf("RoleBinding %s referencia a Role %q, que não está no YAML nem existe no namespace", u.GetName(), name))
				}
			}
		}
		if podSpec != nil {
			sa, _, _ := unstructured.NestedString(u.Object, append(podSpec, "serviceAccountName")...)
			if sa != "" && sa != "default" && !exists("ServiceAccount", ns, sa) {
				warnings = append(warnings, fmt.Sprintf("%s %s usa a ServiceAccount %q, que não está no YAML nem existe no namespace — os pods não sobem", u.GetKind(), batchObjName(u), sa))
			}
		}
	}
	return warnings
}

// batchApplyOne aplica (ou só valida, com dryRun) um documento.
func batchApplyOne(ctx context.Context, client kubernetes.Interface, u *unstructured.Unstructured, dryRun bool) BatchResourceResult {
	kind, ns := u.GetKind(), u.GetNamespace()
	info := batchKinds[kind]
	rc := info.client(client)
	res := BatchResourceResult{Kind: kind, Name: batchObjName(u), Namespace: ns, Action: "create"}

	body, err := u.MarshalJSON()
	if err != nil {
		return batchFail(res, err)
	}

	// Job só com generateName: POST normal (o apply exige nome).
	if u.GetName() == "" {
		req := rc.Post().Namespace(ns).Resource(info.resource).Param("fieldManager", batchFieldManager).Param("fieldValidation", "Strict")
		if dryRun {
			req = req.Param("dryRun", "All")
		}
		return batchResult(res, req.Body(body).Do(ctx).Error())
	}

	getErr := rc.Get().Namespace(ns).Resource(info.resource).Name(u.GetName()).Do(ctx).Error()
	if getErr == nil {
		res.Action = "update"
		if kind == "Job" {
			res.Status, res.Error = "error", fmt.Sprintf("o Job %s já existe — Jobs não podem ser alterados depois de criados", u.GetName())
			res.Hint = "Use outro metadata.name ou troque por metadata.generateName para gerar um nome novo a cada execução."
			return res
		}
	}

	req := rc.Patch(types.ApplyPatchType).Namespace(ns).Resource(info.resource).Name(u.GetName()).
		Param("fieldManager", batchFieldManager).Param("fieldValidation", "Strict")
	if dryRun {
		req = req.Param("dryRun", "All")
	}
	return batchResult(res, req.Body(body).Do(ctx).Error())
}

func batchResult(res BatchResourceResult, err error) BatchResourceResult {
	if err == nil {
		res.Status = "ok"
		return res
	}
	return batchFail(res, err)
}

func batchFail(res BatchResourceResult, err error) BatchResourceResult {
	msg := err.Error()
	res.Status, res.Error = "error", msg
	res.Violations = parseAdmissionViolations(msg)
	switch {
	case len(res.Violations) > 0:
		res.Hint = "Barrado por política de admissão. Ajuste o YAML conforme as violações abaixo."
	case strings.Contains(msg, "attempting to grant RBAC permissions not currently held"):
		res.Hint = "O Kubernetes só deixa criar uma Role com permissões que o seu usuário já tem (proteção contra escalada de privilégio)."
	case apierrors.IsConflict(err) || strings.Contains(msg, "conflict"):
		res.Hint = "Campos deste recurso são gerenciados por outra ferramenta (kubectl apply, Helm...). Altere pela mesma ferramenta ou remova os campos em conflito do YAML."
	case apierrors.IsForbidden(err):
		res.Hint = "Seu usuário não tem permissão para esta operação neste namespace."
	case strings.Contains(msg, "unknown field") || strings.Contains(msg, "strict decoding error"):
		res.Hint = "Campo inexistente para este recurso (erro de digitação ou campo que não existe na versão do cluster)."
	}
	return res
}

var (
	gatekeeperViolationRe = regexp.MustCompile(`\[([^\]]+)\]\s*([^\[]+)`)
	kyvernoPolicyRe       = regexp.MustCompile(`^([A-Za-z0-9][\w.-]*):\s*$`)
	kyvernoRuleRe         = regexp.MustCompile(`^\s+([\w.-]+):\s*(.+)$`)
)

// parseAdmissionViolations extrai as violações das mensagens de recusa do Gatekeeper e do
// Kyverno. Formato desconhecido de webhook vira uma violação genérica com a mensagem toda.
func parseAdmissionViolations(msg string) []BatchViolation {
	i := strings.Index(msg, "denied the request:")
	if !strings.Contains(msg, "admission webhook") || i < 0 {
		return nil
	}
	body := strings.TrimSpace(msg[i+len("denied the request:"):])
	var out []BatchViolation
	switch {
	case strings.Contains(msg, "gatekeeper"):
		for _, m := range gatekeeperViolationRe.FindAllStringSubmatch(body, -1) {
			out = append(out, BatchViolation{Engine: "Gatekeeper", Policy: strings.TrimSpace(m[1]), Message: strings.TrimSpace(m[2])})
		}
	case strings.Contains(msg, "kyverno"):
		policy := ""
		for _, line := range strings.Split(body, "\n") {
			if m := kyvernoPolicyRe.FindStringSubmatch(line); m != nil {
				policy = m[1]
				continue
			}
			if m := kyvernoRuleRe.FindStringSubmatch(line); m != nil && policy != "" {
				out = append(out, BatchViolation{Engine: "Kyverno", Policy: policy, Rule: m[1], Message: strings.Trim(strings.TrimSpace(m[2]), "'\"")})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, BatchViolation{Engine: "webhook", Message: body})
	}
	return out
}

func batchObjName(u *unstructured.Unstructured) string {
	if u.GetName() != "" {
		return u.GetName()
	}
	return u.GetGenerateName() + "(gerado)"
}

// batchMainName é o nome do Job/CronJob do manifesto, para o registro de histórico.
func batchMainName(objs []*unstructured.Unstructured) string {
	for _, u := range objs {
		if k := u.GetKind(); k == "CronJob" || k == "Job" {
			return u.GetKind() + "/" + batchObjName(u)
		}
	}
	return ""
}
