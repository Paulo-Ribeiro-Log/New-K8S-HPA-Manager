package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/models"
)

// Aba Nodes: listagem de todos os nodes do cluster, YAML editável, apply e delete. Cordon,
// uncordon e drain reaproveitam CordonNode/UncordonNode/DrainNodeWithProgress (client.go).

// nodePoolLabels: label do node pool por provider/provisionador, em ordem de preferência.
var nodePoolLabels = []string{
	"kubernetes.azure.com/agentpool",
	"agentpool",
	"eks.amazonaws.com/nodegroup",
	"cloud.google.com/gke-nodepool",
	"karpenter.sh/nodepool",
}

// ListClusterNodes lista todos os nodes com uso de CPU/memória (Metrics Server, se disponível)
// e contagem de pods — 3 chamadas ao todo (nodes, métricas, pods), não uma por node.
func (c *Client) ListClusterNodes(ctx context.Context) ([]models.ClusterNodeSummary, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes in cluster %s: %w", c.cluster, err)
	}

	usage := map[string][2]int64{} // node → {cpuMillis, memBytes}
	metricsOK := false
	if c.metricsClient != nil {
		if list, err := c.metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{}); err == nil {
			metricsOK = true
			for _, m := range list.Items {
				usage[m.Name] = [2]int64{m.Usage.Cpu().MilliValue(), m.Usage.Memory().Value()}
			}
		}
	}

	// ResourceVersion "0": servido do cache do apiserver — bem mais leve em clusters com milhares
	// de pods; a contagem pode estar alguns segundos atrasada, aceitável para a listagem.
	podCount := map[string]int{}
	if pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		ResourceVersion: "0",
		FieldSelector:   "status.phase!=Succeeded,status.phase!=Failed",
	}); err == nil {
		for _, p := range pods.Items {
			if p.Spec.NodeName != "" {
				podCount[p.Spec.NodeName]++
			}
		}
	}

	out := make([]models.ClusterNodeSummary, 0, len(nodes.Items))
	for i := range nodes.Items {
		n := &nodes.Items[i]
		s := summarizeNode(n)
		s.PodsCount = podCount[n.Name]
		s.CPUUsage, s.MemUsage = -1, -1
		if metricsOK {
			if u, ok := usage[n.Name]; ok {
				s.CPUUsage, s.MemUsage = u[0], u[1]
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func summarizeNode(n *corev1.Node) models.ClusterNodeSummary {
	s := models.ClusterNodeSummary{
		Name:             n.Name,
		Status:           "Unknown",
		Unschedulable:    n.Spec.Unschedulable,
		Roles:            nodeRoles(n.Labels),
		Zone:             n.Labels["topology.kubernetes.io/zone"],
		InstanceType:     n.Labels["node.kubernetes.io/instance-type"],
		KubeletVersion:   n.Status.NodeInfo.KubeletVersion,
		OSImage:          n.Status.NodeInfo.OSImage,
		ContainerRuntime: n.Status.NodeInfo.ContainerRuntimeVersion,
		Taints:           []string{},
		Pressures:        []string{},
		CreatedAt:        n.CreationTimestamp.Time,
		Age:              formatAge(n.CreationTimestamp.Time),
		CPUAllocatable:   n.Status.Allocatable.Cpu().MilliValue(),
		MemAllocatable:   n.Status.Allocatable.Memory().Value(),
		PodsCapacity:     n.Status.Allocatable.Pods().Value(),
	}
	for _, l := range nodePoolLabels {
		if v := n.Labels[l]; v != "" {
			s.NodePool = v
			break
		}
	}
	for _, cond := range n.Status.Conditions {
		switch {
		case cond.Type == corev1.NodeReady:
			switch cond.Status {
			case corev1.ConditionTrue:
				s.Status = "Ready"
			case corev1.ConditionFalse:
				s.Status = "NotReady"
			}
		case cond.Status == corev1.ConditionTrue:
			// MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable e condições de
			// node-problem-detector: todas indicam problema quando True.
			s.Pressures = append(s.Pressures, string(cond.Type))
		}
	}
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			s.InternalIP = a.Address
			break
		}
	}
	for _, t := range n.Spec.Taints {
		taint := t.Key
		if t.Value != "" {
			taint += "=" + t.Value
		}
		s.Taints = append(s.Taints, taint+":"+string(t.Effect))
	}
	return s
}

// nodeRoles extrai roles de node-role.kubernetes.io/<role> e kubernetes.io/role.
func nodeRoles(labels map[string]string) []string {
	roles := []string{}
	for k, v := range labels {
		if role, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok && role != "" {
			roles = append(roles, role)
		} else if k == "kubernetes.io/role" && v != "" {
			roles = append(roles, v)
		}
	}
	sort.Strings(roles)
	return roles
}

// GetNodeManifest devolve o YAML editável do node: sem status (subrecurso, somente leitura e
// enorme — lista de imagens) nem campos gerenciados pelo servidor. Status/condições ficam no
// describe.
func (c *Client) GetNodeManifest(ctx context.Context, name string) (*models.NodeManifest, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s in cluster %s: %w", name, c.cluster, err)
	}

	raw, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}
	obj := map[string]interface{}{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	obj["apiVersion"], obj["kind"] = "v1", "Node"
	delete(obj, "status")
	if md, ok := obj["metadata"].(map[string]interface{}); ok {
		for _, f := range []string{"managedFields", "creationTimestamp", "resourceVersion", "uid", "generation", "selfLink"} {
			delete(md, f)
		}
	}

	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal node %s to YAML: %w", name, err)
	}
	return &models.NodeManifest{
		Cluster: c.cluster,
		Name:    name,
		YAML:    string(yamlBytes),
		Status:  summarizeNode(node).Status,
		Age:     formatAge(node.CreationTimestamp.Time),
	}, nil
}

// ApplyNode aplica o YAML do node via server-side apply (dryRun = validação). Só metadata
// (labels, annotations) e spec (taints, unschedulable...) são aplicáveis — status é descartado.
func (c *Client) ApplyNode(ctx context.Context, yamlContent, fieldManager, enforceName string, dryRun, force bool) (*corev1.Node, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj); err != nil {
		return nil, fmt.Errorf("YAML inválido: %w", err)
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("YAML do node vazio")
	}
	if kind, _ := obj["kind"].(string); kind != "" && !strings.EqualFold(kind, "Node") {
		return nil, fmt.Errorf("kind esperado Node, recebido %s", kind)
	}
	obj["apiVersion"], obj["kind"] = "v1", "Node"
	delete(obj, "status")

	md, _ := obj["metadata"].(map[string]interface{})
	if md == nil {
		md = map[string]interface{}{}
		obj["metadata"] = md
	}
	name, _ := md["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = enforceName
	}
	if enforceName != "" && name != enforceName {
		return nil, fmt.Errorf("nome do node não confere: esperado %s, recebido %s", enforceName, name)
	}
	md["name"] = name
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "creationTimestamp", "generation", "selfLink"} {
		delete(md, f)
	}

	payload, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	if fieldManager == "" {
		fieldManager = "web-node-editor"
	}
	opts := metav1.PatchOptions{FieldManager: fieldManager, Force: &force}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	result, err := c.clientset.CoreV1().Nodes().Patch(ctx, name, types.ApplyPatchType, payload, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply node %s in cluster %s: %w", name, c.cluster, err)
	}
	return result, nil
}

// DeleteNode remove o objeto Node da API (a VM continua existindo na cloud).
func (c *Client) DeleteNode(ctx context.Context, name string) error {
	if err := c.clientset.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete node %s in cluster %s: %w", name, c.cluster, err)
	}
	return nil
}

// NodeWorkloads agrupa os pods (não finalizados) de um node por namespace, com os deployments a
// que pertencem. O deployment vem do ReplicaSet dono menos o sufixo pod-template-hash (convenção
// do controller de Deployment) — sem chamada extra à API por ReplicaSet.
func (c *Client) NodeWorkloads(ctx context.Context, nodeName string) ([]models.NodeNamespaceWorkloads, error) {
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Succeeded,status.phase!=Failed", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	byNs := map[string]*models.NodeNamespaceWorkloads{}
	deps := map[string]map[string]bool{}
	for i := range pods.Items {
		p := &pods.Items[i]
		ns := byNs[p.Namespace]
		if ns == nil {
			ns = &models.NodeNamespaceWorkloads{Namespace: p.Namespace, Deployments: []string{}}
			byNs[p.Namespace] = ns
			deps[p.Namespace] = map[string]bool{}
		}
		ns.Pods++
		if d := podDeploymentName(p); d != "" {
			deps[p.Namespace][d] = true
		} else {
			ns.OtherPods++
		}
	}

	out := make([]models.NodeNamespaceWorkloads, 0, len(byNs))
	for name, ns := range byNs {
		for d := range deps[name] {
			ns.Deployments = append(ns.Deployments, d)
		}
		sort.Strings(ns.Deployments)
		out = append(out, *ns)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace < out[j].Namespace })
	return out, nil
}

// podDeploymentName devolve o Deployment dono do pod (via ReplicaSet "<deployment>-<hash>"), ou "".
func podDeploymentName(p *corev1.Pod) string {
	hash := p.Labels["pod-template-hash"]
	for _, o := range p.OwnerReferences {
		if o.Kind == "ReplicaSet" && hash != "" {
			if d, ok := strings.CutSuffix(o.Name, "-"+hash); ok && d != "" {
				return d
			}
		}
	}
	return ""
}
