package promwal

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FixOption é uma correção de agendamento para o pod que não cabe em nenhum node: um novo nodeSelector (e as
// tolerations que ele exige) para o recurso Prometheus. Só é oferecida se, simulada contra os nodes lidos, algum
// node passa a servir ao pod (nodeSelector, afinidade, taints, recursos livres e zona do volume).
type FixOption struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Pool  string   `json:"pool,omitempty"`
	Nodes []string `json:"nodes"` // nodes que passam em todas as checagens
	// NodeSelector é o seletor COMPLETO resultante (vazio = remover o nodeSelector).
	NodeSelector map[string]string `json:"node_selector"`
	// Tolerations a acrescentar às que o recurso já tem.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	Notes       []string            `json:"notes,omitempty"`    // avisos: não impedem a aplicação
	Excluded    []string            `json:"excluded,omitempty"` // nodes do pool descartados e o motivo
	// Blocked: a correção não resolveria (nenhum node do pool passa); a UI mostra o motivo e não deixa aplicar.
	Blocked string `json:"blocked,omitempty"`
}

// LabelAdvice é a recomendação preferível quando o nodeSelector do pod não casa com nenhum node: devolver o label
// ao node pool dedicado (não mexe no recurso Prometheus, então nenhum upgrade a desfaz). A ferramenta só mostra o
// comando: chamadas de escrita à nuvem ficam com o usuário.
type LabelAdvice struct {
	Selector map[string]string `json:"selector"`
	Pools    []LabelAdvicePool `json:"pools"`
	Command  string            `json:"command,omitempty"`
	Warning  string            `json:"warning,omitempty"`
}

type LabelAdvicePool struct {
	Pool    string `json:"pool"`
	Nodes   int    `json:"nodes"`
	Current string `json:"current,omitempty"` // valor atual dos labels do seletor nesse pool
}

func labelAdvice(p PodInfo, nodes []corev1.Node) *LabelAdvice {
	if len(p.NodeSelector) == 0 {
		return nil
	}
	pools := map[string]*LabelAdvicePool{}
	var key string
	for i := range nodes {
		n := &nodes[i]
		matches := true
		for k, v := range p.NodeSelector {
			if n.Labels[k] != v {
				matches = false
			}
		}
		if matches {
			return nil // algum node já tem o label: o problema não é o label faltando
		}
		k, v := nodePoolLabel(n)
		if v == "" {
			continue
		}
		if key == "" {
			key = k
		}
		lp := pools[v]
		if lp == nil {
			var cur []string
			for _, sk := range sortedKeys(p.NodeSelector) {
				if val, ok := n.Labels[sk]; ok {
					cur = append(cur, sk+"="+val)
				}
			}
			lp = &LabelAdvicePool{Pool: v, Current: strings.Join(cur, ", ")}
			pools[v] = lp
		}
		lp.Nodes++
	}
	if len(pools) == 0 {
		return nil
	}
	names := make([]string, 0, len(pools))
	for name := range pools {
		names = append(names, name)
	}
	sort.Strings(names)
	adv := &LabelAdvice{Selector: p.NodeSelector}
	for _, name := range names {
		adv.Pools = append(adv.Pools, *pools[name])
	}

	labels := selectorString(p.NodeSelector)
	switch key {
	case "eks.amazonaws.com/nodegroup":
		adv.Command = "aws eks update-nodegroup-config --cluster-name <cluster> --nodegroup-name <node-group> --labels 'addOrUpdateLabels={" + strings.ReplaceAll(labels, ", ", ",") + "}'"
	case "cloud.google.com/gke-nodepool":
		adv.Command = "gcloud container node-pools update <node-pool> --cluster <cluster> --region <região> --node-labels=" + strings.ReplaceAll(labels, ", ", ",")
		adv.Warning = "--node-labels substitui TODOS os labels do node pool: inclua os que ele já tem (gcloud container node-pools describe)."
	default:
		adv.Command = "az aks nodepool update --resource-group <grupo-de-recursos> --cluster-name <cluster> --name <node-pool> --labels " + strings.ReplaceAll(labels, ", ", " ")
		adv.Warning = "--labels substitui TODOS os labels do node pool: inclua os que ele já tem (az aks nodepool show ... --query nodeLabels)."
	}
	return adv
}

// fixOptions propõe, por node pool, um seletor pelo label do pool (que sobrevive à recriação do node, ao
// contrário de um label aplicado à mão) com as tolerations dos taints do pool, e a remoção do nodeSelector.
// Cada opção é validada node a node; sem node aprovado ela volta Blocked com o motivo.
func fixOptions(p PodInfo, nodes []corev1.Node, facts PlacementFacts) []FixOption {
	type poolGroup struct {
		key, val string
		nodes    []*corev1.Node
	}
	groups := map[string]*poolGroup{}
	for i := range nodes {
		k, v := nodePoolLabel(&nodes[i])
		if v == "" {
			continue
		}
		g := groups[v]
		if g == nil {
			g = &poolGroup{key: k, val: v}
			groups[v] = g
		}
		g.nodes = append(g.nodes, &nodes[i])
	}
	pools := make([]string, 0, len(groups))
	for name := range groups {
		pools = append(pools, name)
	}
	sort.Strings(pools)

	var opts []FixOption
	for _, name := range pools {
		g := groups[name]
		var tols []corev1.Toleration
		for _, n := range g.nodes {
			for _, t := range n.Spec.Taints {
				if t.Effect == corev1.TaintEffectPreferNoSchedule || tolerates(append(append([]corev1.Toleration{}, p.Tolerations...), tols...), t) {
					continue
				}
				tols = append(tols, corev1.Toleration{Key: t.Key, Operator: corev1.TolerationOpEqual, Value: t.Value, Effect: t.Effect})
			}
		}
		sel := map[string]string{g.key: g.val}
		fitting := fittingNodes(p, sel, tols, g.nodes)
		if len(fitting) == 0 {
			continue // nodeSelector/afinidade/taints continuam barrando: nem vale mostrar
		}
		o := FixOption{
			ID:           "pool:" + name,
			Label:        fmt.Sprintf("Usar o node pool %s (nodeSelector %s=%s)", name, g.key, g.val),
			Pool:         name,
			NodeSelector: sel,
			Tolerations:  tols,
		}
		for _, t := range tols {
			o.Notes = append(o.Notes, "adiciona a toleration "+formatToleration(t))
		}
		if len(p.NodeSelector) > 0 {
			o.Notes = append(o.Notes, "substitui o nodeSelector atual ("+selectorString(p.NodeSelector)+")")
		}
		if isSystemPool(g.nodes) {
			o.Notes = append(o.Notes, "pool de SISTEMA: o Prometheus passa a disputar CPU e memória com CoreDNS, metrics-server e outros addons")
		}
		validateOption(&o, p, nodes, fitting, facts)
		opts = append(opts, o)
	}

	if len(p.NodeSelector) > 0 {
		all := make([]*corev1.Node, 0, len(nodes))
		for i := range nodes {
			all = append(all, &nodes[i])
		}
		if fitting := fittingNodes(p, map[string]string{}, nil, all); len(fitting) > 0 {
			o := FixOption{
				ID:           "no-selector",
				Label:        "Remover o nodeSelector",
				NodeSelector: map[string]string{},
				Notes:        []string{"o pod pode cair em qualquer node sem taint, inclusive pools de aplicação e de sistema"},
			}
			validateOption(&o, p, nodes, fitting, facts)
			opts = append(opts, o)
		}
	}
	return opts
}

// validateOption descarta os nodes sem recursos livres ou em zona diferente da do volume e acrescenta os avisos.
func validateOption(o *FixOption, p PodInfo, nodes []corev1.Node, fitting []string, facts PlacementFacts) {
	byName := map[string]*corev1.Node{}
	for i := range nodes {
		byName[nodes[i].Name] = &nodes[i]
	}
	for _, name := range fitting {
		n := byName[name]
		if why := nodeBlocker(p, n, facts); why != "" {
			o.Excluded = append(o.Excluded, name+": "+why)
			continue
		}
		o.Nodes = append(o.Nodes, name)
	}

	if facts.RequestsErr != "" {
		o.Notes = append(o.Notes, "recursos livres NÃO verificados ("+facts.RequestsErr+")")
	} else if p.CPURequestMilli == 0 && p.MemRequestBytes == 0 {
		o.Notes = append(o.Notes, "o pod não declara request de CPU/memória: recursos livres não verificados")
	}
	if facts.PVCNote != "" {
		o.Notes = append(o.Notes, "volume: "+facts.PVCNote)
	}
	if len(o.Nodes) == 0 {
		o.Blocked = "nenhum node do pool passa nas checagens: " + strings.Join(o.Excluded, "; ")
	}
}

// nodeBlocker diz por que um node que passa no nodeSelector/taints ainda não serve: sem CPU/memória livres para o
// request do pod ou em zona diferente da do volume já criado. "" = serve (ou não deu para verificar).
func nodeBlocker(p PodInfo, n *corev1.Node, facts PlacementFacts) string {
	var why []string
	if facts.RequestsErr == "" && (p.CPURequestMilli > 0 || p.MemRequestBytes > 0) {
		used := facts.NodeRequests[n.Name]
		freeCPU := n.Status.Allocatable.Cpu().MilliValue() - used.CPUMilli
		freeMem := n.Status.Allocatable.Memory().Value() - used.MemBytes
		if p.CPURequestMilli > freeCPU {
			why = append(why, fmt.Sprintf("CPU livre %dm, o pod pede %dm", max(freeCPU, 0), p.CPURequestMilli))
		}
		if p.MemRequestBytes > freeMem {
			why = append(why, fmt.Sprintf("memória livre %s, o pod pede %s", formatBytes(max(freeMem, 0)), formatBytes(p.MemRequestBytes)))
		}
	}
	if facts.PVCZone != "" {
		for _, k := range zoneKeys {
			if z := n.Labels[k]; z != "" {
				if z != facts.PVCZone {
					why = append(why, fmt.Sprintf("o volume está na zona %s e o node na %s", facts.PVCZone, z))
				}
				break
			}
		}
	}
	return strings.Join(why, "; ")
}

func isSystemPool(nodes []*corev1.Node) bool {
	for _, n := range nodes {
		if n.Labels["kubernetes.azure.com/mode"] == "system" {
			return true
		}
		for _, t := range n.Spec.Taints {
			if t.Key == "CriticalAddonsOnly" {
				return true
			}
		}
	}
	return false
}

// fittingNodes devolve os nodes que servem ao pod depois de trocar o nodeSelector e somar as tolerations.
func fittingNodes(p PodInfo, selector map[string]string, extra []corev1.Toleration, nodes []*corev1.Node) []string {
	q := p
	q.NodeSelector = selector
	q.Tolerations = append(append([]corev1.Toleration{}, p.Tolerations...), extra...)
	var out []string
	for _, n := range nodes {
		if evalNode(q, n).fit.Fits {
			out = append(out, n.Name)
		}
	}
	sort.Strings(out)
	return out
}

func nodePoolLabel(n *corev1.Node) (key, val string) {
	for _, k := range []string{"agentpool", "kubernetes.azure.com/agentpool", "eks.amazonaws.com/nodegroup", "cloud.google.com/gke-nodepool"} {
		if v := n.Labels[k]; v != "" {
			return k, v
		}
	}
	return "", ""
}

func selectorString(sel map[string]string) string {
	parts := make([]string, 0, len(sel))
	for _, k := range sortedKeys(sel) {
		parts = append(parts, k+"="+sel[k])
	}
	return strings.Join(parts, ", ")
}

func formatToleration(t corev1.Toleration) string {
	s := t.Key
	if t.Value != "" {
		s += "=" + t.Value
	}
	if t.Effect != "" {
		s += ":" + string(t.Effect)
	}
	return s
}

// SchedulingRequest é a correção a aplicar no recurso Prometheus (spec.nodeSelector e spec.tolerations).
type SchedulingRequest struct {
	Cluster      string
	Namespace    string
	Name         string
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
}

// SchedulingResult descreve o que mudou (só identificação de labels/taints: nada sensível).
type SchedulingResult struct {
	BeforeSelector   map[string]string `json:"before_node_selector"`
	AfterSelector    map[string]string `json:"after_node_selector"`
	AddedTolerations []string          `json:"added_tolerations,omitempty"`
	RecreatedPods    []string          `json:"recreated_pods,omitempty"`
	Managed          string            `json:"managed_by,omitempty"` // Helm / Argo CD: a mudança pode ser revertida por eles
	Warnings         []string          `json:"warnings,omitempty"`
}

const (
	schedulingSyncWait = 30 * time.Second
	schedulingPoll     = 2 * time.Second
)

type schedulingCR struct {
	prometheusCR
	Spec struct {
		NodeSelector map[string]string   `json:"nodeSelector"`
		Tolerations  []corev1.Toleration `json:"tolerations"`
	} `json:"spec"`
}

// ApplyScheduling grava o nodeSelector (substituindo o atual) e soma as tolerations no recurso Prometheus (merge
// patch via kubectl; o prometheus-operator regera o StatefulSet). Depois recria os pods que nunca foram agendados,
// porque o StatefulSet não troca sozinho um pod preso em Pending (sem nó, sem dados no volume).
func (c *Collector) ApplyScheduling(ctx context.Context, req SchedulingRequest) (SchedulingResult, error) {
	var res SchedulingResult
	for k, v := range req.NodeSelector {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return res, fmt.Errorf("nodeSelector inválido: chave e valor não podem ser vazios")
		}
	}
	for _, t := range req.Tolerations {
		if t.Key == "" {
			return res, fmt.Errorf("toleration inválida: informe a chave do taint")
		}
	}

	raw, err := c.Run(ctx, req.Cluster, req.Namespace, "get", "prometheus", req.Name, "-o", "json")
	if err != nil {
		return res, fmt.Errorf("lendo o recurso Prometheus %s/%s: %w", req.Namespace, req.Name, err)
	}
	var cr schedulingCR
	if err := json.Unmarshal(raw, &cr); err != nil {
		return res, fmt.Errorf("resposta inválida do recurso Prometheus: %w", err)
	}
	res.BeforeSelector = nonNil(cr.Spec.NodeSelector)
	res.AfterSelector = nonNil(req.NodeSelector)
	switch {
	case cr.Metadata.Labels["app.kubernetes.io/managed-by"] == "Helm" || cr.Metadata.Annotations["meta.helm.sh/release-name"] != "":
		res.Managed = "Helm"
	case cr.Metadata.Labels["argocd.argoproj.io/instance"] != "" || cr.Metadata.Annotations["argocd.argoproj.io/tracking-id"] != "":
		res.Managed = "Argo CD"
	}

	// Merge patch de mapa só mescla chaves: as que saem do seletor precisam de null explícito.
	selPatch := map[string]any{}
	for k := range cr.Spec.NodeSelector {
		selPatch[k] = nil
	}
	for k, v := range req.NodeSelector {
		selPatch[k] = v
	}
	spec := map[string]any{"nodeSelector": selPatch}

	tols := cr.Spec.Tolerations
	for _, t := range req.Tolerations {
		if !hasToleration(tols, t) {
			tols = append(tols, t)
			res.AddedTolerations = append(res.AddedTolerations, formatToleration(t))
		}
	}
	if len(res.AddedTolerations) > 0 {
		spec["tolerations"] = tols // lista é substituída inteira no merge patch: já inclui as existentes
	}
	patch, err := json.Marshal(map[string]any{"spec": spec})
	if err != nil {
		return res, err
	}
	if _, err := c.Run(ctx, req.Cluster, req.Namespace, "patch", "prometheus.monitoring.coreos.com", req.Name, "--type", "merge", "-p", string(patch)); err != nil {
		return res, fmt.Errorf("alterando o recurso Prometheus %s/%s: %w", req.Namespace, req.Name, err)
	}
	if res.Managed != "" {
		res.Warnings = append(res.Warnings, "O recurso é gerenciado por "+res.Managed+": um upgrade/sync reescreve o nodeSelector e as tolerations. Leve a mudança para os values/manifestos para ela persistir.")
	}

	recreated, warn := c.recreateUnscheduled(ctx, req)
	res.RecreatedPods = recreated
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	return res, nil
}

// recreateUnscheduled espera o operator refletir o novo seletor no StatefulSet e apaga os pods sem nó (Pending), para
// o StatefulSet recriá-los já com o seletor novo. Pod que nunca rodou não tem dado a perder; o PVC não é tocado.
func (c *Collector) recreateUnscheduled(ctx context.Context, req SchedulingRequest) ([]string, string) {
	sts := "prometheus-" + req.Name
	deadline := time.Now().Add(schedulingSyncWait)
	for {
		s, err := c.Client.AppsV1().StatefulSets(req.Namespace).Get(ctx, sts, metav1.GetOptions{})
		if err == nil && sameSelector(s.Spec.Template.Spec.NodeSelector, req.NodeSelector) {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, "O operator ainda não refletiu o novo seletor no StatefulSet " + sts + ": o pod Pending será refeito quando ele atualizar (se não for, apague-o)."
		}
		time.Sleep(schedulingPoll)
	}

	pods, err := c.Client.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{LabelSelector: defaultCRLabel + "=" + req.Name})
	if err != nil {
		return nil, "Não foi possível listar os pods para recriá-los: " + err.Error()
	}
	var done []string
	for _, p := range pods.Items {
		if p.Spec.NodeName != "" || p.Status.Phase != corev1.PodPending {
			continue
		}
		if err := c.Client.CoreV1().Pods(req.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{}); err != nil {
			return done, fmt.Sprintf("Não foi possível recriar o pod %s: %v", p.Name, err)
		}
		done = append(done, p.Name)
	}
	return done, ""
}

func hasToleration(list []corev1.Toleration, t corev1.Toleration) bool {
	for _, e := range list {
		if e.Key == t.Key && e.Operator == t.Operator && e.Value == t.Value && e.Effect == t.Effect {
			return true
		}
	}
	return false
}

func sameSelector(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
