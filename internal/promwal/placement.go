package promwal

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// placementNote diz o que o diagnóstico por node avalia e o que não avalia (não pretende reproduzir o scheduler).
const placementNote = "Avalia nodeSelector, nodeAffinity obrigatória, taints/tolerations e cordon. Não avalia recursos livres, afinidade entre pods nem volume."

// NodeFit diz se um node serve ao pod e, se não, por quê.
type NodeFit struct {
	Node    string   `json:"node"`
	Pool    string   `json:"pool,omitempty"`
	Fits    bool     `json:"fits"`
	Reasons []string `json:"reasons,omitempty"`
}

// Placement é o diagnóstico node a node de um pod que não foi agendado.
type Placement struct {
	Nodes   []NodeFit `json:"nodes,omitempty"`
	Summary string    `json:"summary"`
	Note    string    `json:"note"`
	// Unavailable: por que não foi possível diagnosticar (ex.: sem permissão para listar nodes).
	Unavailable string `json:"unavailable,omitempty"`
	// Options: correções que fariam algum node servir ao pod (só quando nenhum serve hoje).
	Options []FixOption `json:"options,omitempty"`
	// LabelAdvice: quando o seletor do pod não casa com nenhum node, o caminho preferível é devolver o label ao node pool.
	LabelAdvice *LabelAdvice `json:"label_advice,omitempty"`
}

type nodeEval struct {
	fit         NodeFit
	selectorBad []string // nodeSelector / nodeAffinity
	taintBad    []string // taints não tolerados / cordon
}

// diagnosePlacement avalia cada node contra as restrições do pod. nodesErr != "" = a listagem de nodes falhou.
func diagnosePlacement(p PodInfo, nodes []corev1.Node, nodesErr string) *Placement {
	return diagnosePlacementFacts(p, nodes, nodesErr, PlacementFacts{})
}

// diagnosePlacementFacts é o diagnóstico com os dados extras (recursos livres, zona do volume) que validam as correções.
func diagnosePlacementFacts(p PodInfo, nodes []corev1.Node, nodesErr string, facts PlacementFacts) *Placement {
	pl := &Placement{Note: placementNote}
	if nodesErr != "" {
		pl.Unavailable = nodesErr
		pl.Summary = "Não foi possível diagnosticar por node: " + nodesErr
		return pl
	}
	if len(nodes) == 0 {
		pl.Summary = "O cluster não tem nodes visíveis para o pod."
		return pl
	}

	evals := make([]nodeEval, 0, len(nodes))
	for i := range nodes {
		evals = append(evals, evalNode(p, &nodes[i]))
	}
	sort.Slice(evals, func(i, j int) bool { return evals[i].fit.Node < evals[j].fit.Node })
	for _, e := range evals {
		pl.Nodes = append(pl.Nodes, e.fit)
	}
	pl.Summary = placementSummary(p, evals, nodes)
	if !anyFits(evals) {
		pl.Options = fixOptions(p, nodes, facts)
		pl.LabelAdvice = labelAdvice(p, nodes)
	}
	return pl
}

func anyFits(evals []nodeEval) bool {
	for _, e := range evals {
		if e.fit.Fits {
			return true
		}
	}
	return false
}

func evalNode(p PodInfo, n *corev1.Node) nodeEval {
	e := nodeEval{fit: NodeFit{Node: n.Name, Pool: nodePool(n)}}

	for _, k := range sortedKeys(p.NodeSelector) {
		want := p.NodeSelector[k]
		got, ok := n.Labels[k]
		switch {
		case !ok:
			e.selectorBad = append(e.selectorBad, fmt.Sprintf("falta o label %s=%s", k, want))
		case got != want:
			e.selectorBad = append(e.selectorBad, fmt.Sprintf("label %s=%s (o pod exige %s=%s)", k, got, k, want))
		}
	}
	e.selectorBad = append(e.selectorBad, affinityFailures(p.NodeAffinity, n)...)

	if n.Spec.Unschedulable {
		e.taintBad = append(e.taintBad, "node cordonado (unschedulable)")
	}
	for _, t := range n.Spec.Taints {
		if t.Effect == corev1.TaintEffectPreferNoSchedule || tolerates(p.Tolerations, t) {
			continue
		}
		e.taintBad = append(e.taintBad, "taint não tolerado: "+formatTaint(t))
	}

	e.fit.Reasons = append(append([]string{}, e.selectorBad...), e.taintBad...)
	e.fit.Fits = len(e.fit.Reasons) == 0
	return e
}

func placementSummary(p PodInfo, evals []nodeEval, nodes []corev1.Node) string {
	var fits, selectorOK []nodeEval
	for _, e := range evals {
		if e.fit.Fits {
			fits = append(fits, e)
		}
		if len(e.selectorBad) == 0 {
			selectorOK = append(selectorOK, e)
		}
	}
	names := func(es []nodeEval) string {
		var s []string
		for _, e := range es {
			s = append(s, e.fit.Node)
		}
		return strings.Join(s, ", ")
	}

	switch {
	case len(fits) > 0:
		return fmt.Sprintf("%d node(s) passam em nodeSelector, nodeAffinity e taints (%s): o bloqueio é outro (recursos livres, afinidade entre pods ou volume). Veja a mensagem do scheduler.", len(fits), names(fits))

	case len(selectorOK) == 0:
		what := selectorDescription(p)
		s := fmt.Sprintf("Nenhum dos %d node(s) atende a %s.", len(evals), what)
		if len(p.NodeSelector) > 0 {
			s += " " + labelCensus(p.NodeSelector, nodes)
		}
		return s + " Se o label (ou o taint) foi aplicado à mão em um node que depois foi recriado (eviction do spot, upgrade, escala), ele se perdeu: defina-o no node pool ou ajuste o seletor do recurso Prometheus."

	default:
		var parts []string
		for _, e := range selectorOK {
			parts = append(parts, fmt.Sprintf("%s (%s)", e.fit.Node, strings.Join(e.taintBad, "; ")))
		}
		return fmt.Sprintf("Só %d node(s) atendem ao seletor, mas estão bloqueados: %s. Adicione a toleration ao recurso Prometheus ou libere o node.", len(selectorOK), strings.Join(parts, " | "))
	}
}

func selectorDescription(p PodInfo) string {
	var parts []string
	for _, k := range sortedKeys(p.NodeSelector) {
		parts = append(parts, fmt.Sprintf("%s=%s", k, p.NodeSelector[k]))
	}
	switch {
	case len(parts) > 0 && p.NodeAffinity != nil:
		return "nodeSelector (" + strings.Join(parts, ", ") + ") e nodeAffinity do pod"
	case len(parts) > 0:
		return "nodeSelector (" + strings.Join(parts, ", ") + ")"
	default:
		return "nodeAffinity do pod"
	}
}

// labelCensus conta quantos nodes têm cada label do seletor e lista os que têm a chave com outro valor.
func labelCensus(sel map[string]string, nodes []corev1.Node) string {
	var out []string
	for _, k := range sortedKeys(sel) {
		with, other := 0, []string{}
		for i := range nodes {
			switch got, ok := nodes[i].Labels[k]; {
			case ok && got == sel[k]:
				with++
			case ok:
				other = append(other, fmt.Sprintf("%s (%s=%s)", nodes[i].Name, k, got))
			}
		}
		s := fmt.Sprintf("%d de %d têm %s=%s", with, len(nodes), k, sel[k])
		if len(other) > 0 {
			sort.Strings(other)
			s += "; com outro valor: " + strings.Join(other, ", ")
		}
		out = append(out, s)
	}
	return strings.Join(out, ". ") + "."
}

// tolerates segue a semântica do Kubernetes: effect vazio casa qualquer efeito; chave vazia só com Exists.
func tolerates(tols []corev1.Toleration, t corev1.Taint) bool {
	for _, tol := range tols {
		if tol.Effect != "" && tol.Effect != t.Effect {
			continue
		}
		if tol.Key != "" && tol.Key != t.Key {
			continue
		}
		switch tol.Operator {
		case corev1.TolerationOpExists:
			return true
		case "", corev1.TolerationOpEqual:
			if tol.Key != "" && tol.Value == t.Value {
				return true
			}
		}
	}
	return false
}

func formatTaint(t corev1.Taint) string {
	s := t.Key
	if t.Value != "" {
		s += "=" + t.Value
	}
	return s + ":" + string(t.Effect)
}

// affinityFailures avalia a nodeAffinity obrigatória: os termos são OU, as expressões de cada termo são E. Sem
// termo satisfeito, devolve as falhas do termo que mais se aproxima (menos expressões falhando).
func affinityFailures(na *corev1.NodeSelector, n *corev1.Node) []string {
	if na == nil || len(na.NodeSelectorTerms) == 0 {
		return nil
	}
	var best []string
	for i, term := range na.NodeSelectorTerms {
		var fails []string
		for _, req := range term.MatchExpressions {
			if !matchRequirement(req, n.Labels[req.Key], hasKey(n.Labels, req.Key)) {
				fails = append(fails, "nodeAffinity: "+describeRequirement(req))
			}
		}
		for _, req := range term.MatchFields {
			if req.Key == "metadata.name" && !matchRequirement(req, n.Name, true) {
				fails = append(fails, "nodeAffinity: "+describeRequirement(req))
			}
		}
		if len(fails) == 0 {
			return nil
		}
		if i == 0 || len(fails) < len(best) {
			best = fails
		}
	}
	return best
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

func matchRequirement(r corev1.NodeSelectorRequirement, val string, present bool) bool {
	in := func() bool {
		for _, v := range r.Values {
			if v == val {
				return true
			}
		}
		return false
	}
	switch r.Operator {
	case corev1.NodeSelectorOpIn:
		return present && in()
	case corev1.NodeSelectorOpNotIn:
		return !present || !in()
	case corev1.NodeSelectorOpExists:
		return present
	case corev1.NodeSelectorOpDoesNotExist:
		return !present
	case corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt:
		if !present || len(r.Values) != 1 {
			return false
		}
		have, err1 := strconv.ParseInt(val, 10, 64)
		want, err2 := strconv.ParseInt(r.Values[0], 10, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		if r.Operator == corev1.NodeSelectorOpGt {
			return have > want
		}
		return have < want
	}
	return false
}

func describeRequirement(r corev1.NodeSelectorRequirement) string {
	if len(r.Values) == 0 {
		return fmt.Sprintf("%s %s", r.Key, r.Operator)
	}
	return fmt.Sprintf("%s %s [%s]", r.Key, r.Operator, strings.Join(r.Values, ", "))
}

func nodePool(n *corev1.Node) string {
	for _, k := range []string{"agentpool", "kubernetes.azure.com/agentpool", "eks.amazonaws.com/nodegroup", "cloud.google.com/gke-nodepool"} {
		if v := n.Labels[k]; v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
