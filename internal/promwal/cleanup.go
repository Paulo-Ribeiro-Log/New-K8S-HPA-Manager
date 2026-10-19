package promwal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	podsGoneWait   = 5 * time.Minute
	cleanReadyWait = 3 * time.Minute // montar o PVC num nó novo pode demorar (attach do disco)
	promReadyWait  = 10 * time.Minute
)

// Target é uma réplica a limpar: o pod (só para relatório) e o PVC cujo wal/ será apagado.
type Target struct {
	Pod string `json:"pod"`
	PVC string `json:"pvc"`
}

// Step é um evento de progresso da limpeza.
type Step struct {
	Key      string
	Message  string
	Progress float64 // 0..1
}

type CleanupRequest struct {
	Cluster, Namespace, Name string
	Targets                  []Target
}

// TargetResult é o que foi limpo em um PVC e o que se perdeu.
type TargetResult struct {
	Pod             string     `json:"pod"`
	PVC             string     `json:"pvc"`
	LastBlockEndsAt *time.Time `json:"last_block_ends_at,omitempty"`
	WALBytes        int64      `json:"wal_bytes"`
	ChunksHeadBytes int64      `json:"chunks_head_bytes"`
	BlocksPreserved int        `json:"blocks_preserved"`
}

type CleanupResult struct {
	Cleaned  []TargetResult `json:"cleaned"`
	Restored bool           `json:"restored"`
	Ready    bool           `json:"ready"` // o Prometheus voltou a ficar Ready depois de religado
	Warnings []string       `json:"warnings,omitempty"`
}

// Cleaner executa a limpeza. A confirmação do usuário e a validação da análise são do chamador (handler).
type Cleaner struct{ *Collector }

// requiredAccess são as permissões que a limpeza usa; checadas ANTES de desligar o Prometheus, para não
// desligá-lo e só então descobrir que não dá para criar o pod.
var requiredAccess = []authorizationv1.ResourceAttributes{
	{Verb: "patch", Group: "monitoring.coreos.com", Resource: "prometheuses"},
	{Verb: "list", Resource: "pods"},
	{Verb: "create", Resource: "pods"},
	{Verb: "delete", Resource: "pods"},
	{Verb: "create", Resource: "pods", Subresource: "exec"},
}

// CheckAccess verifica via SelfSubjectAccessReview se o usuário do kubeconfig pode executar a limpeza.
func (c *Collector) CheckAccess(ctx context.Context, ns string) error {
	var denied []string
	for _, ra := range requiredAccess {
		ra.Namespace = ns
		rev, err := c.Client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx,
			&authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &ra}},
			metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("verificando permissões no cluster: %w", err)
		}
		if !rev.Status.Allowed {
			res := ra.Resource
			if ra.Subresource != "" {
				res += "/" + ra.Subresource
			}
			denied = append(denied, ra.Verb+" "+res)
		}
	}
	if len(denied) > 0 {
		return fmt.Errorf("sem permissão no cluster em %s: %s", ns, strings.Join(denied, ", "))
	}
	return nil
}

// Restore religa um Prometheus que uma limpeza interrompida deixou desligado e remove a anotação.
func (c *Cleaner) Restore(ctx context.Context, cluster, ns, name string) (int32, error) {
	cr, err := c.readCR(ctx, cluster, ns, name)
	if err != nil {
		return 0, err
	}
	v := cr.Metadata.Annotations[AnnotationOriginalReplicas]
	if v == "" {
		return 0, fmt.Errorf("%s/%s não tem limpeza pendente: nada a restaurar", ns, name)
	}
	n := parseReplicas(v)
	if err := c.restoreCR(ctx, cluster, ns, name, n, nil); err != nil {
		return 0, err
	}
	return n, nil
}

// restoreCR religa o Prometheus e remove a anotação de restauração. extra são anotações adicionais gravadas no
// MESMO patch (o contador de reincidência: só vale se o religar der certo).
func (c *Collector) restoreCR(ctx context.Context, cluster, ns, name string, replicas int32, extra map[string]any) error {
	annotations := map[string]any{AnnotationOriginalReplicas: nil}
	for k, v := range extra {
		annotations[k] = v
	}
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": annotations},
		"spec":     map[string]any{"replicas": replicas},
	})
	_, err := c.Run(ctx, cluster, ns, "patch", "prometheus", name, "--type", "merge", "-p", string(patch))
	return err
}

// Execute desliga o Prometheus, apaga wal/ e chunks_head/ dos PVCs alvo e religa. O religar acontece SEMPRE
// (erro, cancelamento do ctx, pânico): a anotação gravada antes de desligar permite restaurar mesmo se este
// processo morrer (ver Restore). Os blocos compactados (pastas 01…) não são tocados.
func (c *Cleaner) Execute(ctx context.Context, req CleanupRequest, emit func(Step)) (res CleanupResult, err error) {
	step := func(key, msg string, p float64) {
		if emit != nil {
			emit(Step{Key: key, Message: msg, Progress: p})
		}
	}
	if len(req.Targets) == 0 {
		return res, errors.New("nenhum PVC alvo informado")
	}

	step("access", "Verificando permissões no cluster", 0.02)
	if err = c.CheckAccess(ctx, req.Namespace); err != nil {
		return res, err
	}

	cr, err := c.readCR(ctx, req.Cluster, req.Namespace, req.Name)
	if err != nil {
		return res, err
	}
	if v := cr.Metadata.Annotations[AnnotationOriginalReplicas]; v != "" {
		return res, fmt.Errorf("há uma limpeza anterior interrompida (réplicas originais: %s): restaure antes de limpar de novo", v)
	}
	orig := int32(1)
	if cr.Spec.Replicas != nil {
		orig = *cr.Spec.Replicas
	}
	if orig < 1 {
		return res, fmt.Errorf("o Prometheus já está com replicas=%d (desligado); religue-o antes", orig)
	}
	sp := c.statefulSetSpec(ctx, req.Namespace, req.Name)

	step("annotate", fmt.Sprintf("Gravando a anotação de restauração (réplicas originais: %d)", orig), 0.05)
	annotate, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{AnnotationOriginalReplicas: fmt.Sprint(orig)}}})
	if _, err = c.Run(ctx, req.Cluster, req.Namespace, "patch", "prometheus", req.Name, "--type", "merge", "-p", string(annotate)); err != nil {
		return res, fmt.Errorf("gravando anotação de restauração: %w", err)
	}

	// Daqui em diante o Prometheus pode estar desligado: o defer religa sempre.
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		step("restore", fmt.Sprintf("Religando o Prometheus com %d réplica(s)", orig), 0.9)
		// Conta como recuperação toda limpeza em que algo foi apagado (mesmo que uma etapa posterior tenha falhado).
		var extra map[string]any
		if len(res.Cleaned) > 0 {
			extra = cleanupAnnotations(cr.Metadata.Annotations, now())
		}
		if rerr := c.restoreCR(rctx, req.Cluster, req.Namespace, req.Name, orig, extra); rerr != nil {
			err = errors.Join(err, fmt.Errorf("FALHA AO RELIGAR o Prometheus (%v): use a restauração da ferramenta ou `kubectl --context %s -n %s patch prometheus %s --type merge -p '{\"spec\":{\"replicas\":%d}}'`",
				rerr, req.Cluster, req.Namespace, req.Name, orig))
			return
		}
		res.Restored = true
		step("ready", "Aguardando o Prometheus ficar Ready", 0.95)
		if c.waitPrometheusReady(rctx, req, promReadyWait) {
			res.Ready = true
		} else {
			res.Warnings = append(res.Warnings, "O Prometheus foi religado mas ainda não ficou Ready: acompanhe os pods (o replay do WAL vazio deve ser rápido).")
		}
		step("done", "Concluído", 1)
	}()

	step("scale-down", "Desligando o Prometheus (replicas 0)", 0.1)
	if _, err = c.Run(ctx, req.Cluster, req.Namespace, "patch", "prometheus", req.Name, "--type", "merge", "-p", `{"spec":{"replicas":0}}`); err != nil {
		return res, fmt.Errorf("desligando o Prometheus: %w", err)
	}
	step("wait-pods", "Aguardando os pods terminarem", 0.2)
	if err = c.waitNoPods(ctx, req.Namespace, req.Name, podsGoneWait); err != nil {
		return res, err
	}

	for i, t := range req.Targets {
		base := 0.3 + 0.55*float64(i)/float64(len(req.Targets))
		span := 0.55 / float64(len(req.Targets))
		var tr TargetResult
		if tr, err = c.cleanTarget(ctx, req, t, sp, base, span, step); err != nil {
			return res, fmt.Errorf("limpando %s: %w", t.PVC, err)
		}
		res.Cleaned = append(res.Cleaned, tr)
	}
	return res, nil
}

func (c *Cleaner) cleanTarget(ctx context.Context, req CleanupRequest, t Target, sp stsSpec, base, span float64, step func(string, string, float64)) (TargetResult, error) {
	tr := TargetResult{Pod: t.Pod, PVC: t.PVC}
	if t.PVC == "" {
		return tr, errors.New("PVC não informado")
	}
	step("mount", "Montando "+t.PVC+" em um pod temporário", base)
	name, err := c.startTempPod(ctx, req.Namespace, tempPod("prometheus-wal-cleanup", cleanPodLabelVal, t.PVC, "", sp, false), cleanReadyWait)
	if err != nil {
		return tr, err
	}
	defer c.removeTempPod(req.Namespace, name)

	v, err := c.inspectVolume(ctx, req.Cluster, req.Namespace, name)
	if err != nil {
		return tr, err
	}
	tr.WALBytes, tr.ChunksHeadBytes, tr.BlocksPreserved = v.WALBytes, v.ChunksHeadBytes, v.Blocks
	msg := fmt.Sprintf("%s: %d bloco(s) preservado(s); apagando WAL (%s) e chunks_head (%s)", t.PVC, v.Blocks, formatBytes(v.WALBytes), formatBytes(v.ChunksHeadBytes))
	if !v.LastBlockMaxTime.IsZero() {
		lt := v.LastBlockMaxTime
		tr.LastBlockEndsAt = &lt
		msg += fmt.Sprintf("; métricas depois de %s serão perdidas", lt.Local().Format("02/01/2006 15:04"))
	} else {
		msg += "; sem blocos compactados: TODO o histórico será perdido"
	}
	step("wipe", msg, base+span*0.5)

	if _, err := c.Run(ctx, req.Cluster, req.Namespace, "exec", name, "--", "sh", "-c",
		"rm -rf /prometheus/wal/* /prometheus/chunks_head/* /prometheus/lock"); err != nil {
		return tr, fmt.Errorf("apagando wal/chunks_head: %w", err)
	}
	out, err := c.Run(ctx, req.Cluster, req.Namespace, "exec", name, "--", "sh", "-c", "ls -A /prometheus/wal /prometheus/chunks_head 2>/dev/null | wc -l")
	if err != nil {
		return tr, fmt.Errorf("verificando a limpeza: %w", err)
	}
	if n := strings.TrimSpace(string(out)); n != "0" {
		return tr, fmt.Errorf("wal/chunks_head ainda têm %s entrada(s) depois da limpeza", n)
	}
	step("cleaned", t.PVC+" limpo", base+span)
	return tr, nil
}

func (c *Collector) waitNoPods(ctx context.Context, ns, crName string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		pods, err := c.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: defaultCRLabel + "=" + crName})
		if err == nil && len(pods.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("os pods do Prometheus não terminaram em %s", wait)
		case <-time.After(pollEvery):
		}
	}
}

// waitPrometheusReady espera todos os pods do Prometheus ficarem Ready. Não é erro de limpeza se não ficar.
func (c *Cleaner) waitPrometheusReady(ctx context.Context, req CleanupRequest, wait time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		pods, err := c.Client.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{LabelSelector: defaultCRLabel + "=" + req.Name})
		if err == nil && len(pods.Items) > 0 {
			ready := true
			for i := range pods.Items {
				if !podInfo(&pods.Items[i]).Ready {
					ready = false
				}
			}
			if ready {
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollEvery):
		}
	}
}
