package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"k8s-hpa-manager/internal/config"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RemovedNodeInfo descreve um node removido recentemente.
type RemovedNodeInfo struct {
	Name      string `json:"name"`
	RemovedAt string `json:"removed_at"` // RFC3339 ou ""
	Reason    string `json:"reason"`
	Source    string `json:"source"`  // "cluster-autoscaler" | "k8s-events" | "azure-activity"
	Details   string `json:"details"` // linhas brutas para exibição
}

var (
	reKlogPrefix = regexp.MustCompile(`^[IWEF](\d{4}) (\d{2}:\d{2}:\d{2})`)
	reNodeRemove = regexp.MustCompile(`(?i)(?:removing|deleting) node[:\s]+([\w][\w.-]+)`)
	reScaleDown  = regexp.MustCompile(`(?i)scale[- ]down[^"]*removing node[:\s]+([\w][\w.-]+)`)
)

// GetRemovedNodes retorna nodes removidos ou não-saudáveis a partir de:
//  1. Logs do pod cluster-autoscaler (somente quando acessível — não em AKS managed CA)
//  2. Eventos Kubernetes em kube-system (onde o CA do AKS registra scale-down)
//  3. Azure Activity Log via az CLI usando o nodeResourceGroup do AKS (janela 7 dias)
//  4. Nodes K8s com status NotReady/Unknown ou cordoned (ainda existentes)
func (h *NodePoolHandler) GetRemovedNodes(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	pool := strings.TrimSpace(c.Query("pool"))
	if cluster == "" {
		c.JSON(400, gin.H{"error": "cluster obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	clusterCfg := h.kubeManager.GetClusterConfig(cluster)

	removed := map[string]*RemovedNodeInfo{}
	var debugLines []string

	// ── Fonte 1: logs do pod CA (somente quando não é AKS managed) ───────────
	caNodes, caDebug := fetchCALogs(ctx, client, pool)
	debugLines = append(debugLines, caDebug...)
	for _, n := range caNodes {
		removed[n.Name] = n
	}

	// ── Fonte 2: eventos em kube-system e default (CA do AKS registra aqui) ──
	evtNodes, evtDebug := fetchNodeEventsV2(ctx, client, pool)
	debugLines = append(debugLines, evtDebug...)
	for _, n := range evtNodes {
		if _, exists := removed[n.Name]; !exists {
			removed[n.Name] = n
		}
	}

	// ── Fonte 3: Azure Activity Log via nodeResourceGroup (7 dias) ───────────
	azNodes, azDebug := fetchAzureActivityLog(ctx, clusterCfg, pool)
	debugLines = append(debugLines, azDebug...)
	for _, n := range azNodes {
		if _, exists := removed[n.Name]; !exists {
			removed[n.Name] = n
		}
	}

	// ── Fonte 4: Nodes K8s com NotReady/Unknown ou cordoned ──────────────────
	unhealthyNodes, uhDebug := fetchUnhealthyNodes(ctx, client, pool)
	debugLines = append(debugLines, uhDebug...)
	for _, n := range unhealthyNodes {
		if _, exists := removed[n.Name]; !exists {
			removed[n.Name] = n
		}
	}

	result := make([]*RemovedNodeInfo, 0, len(removed))
	for _, n := range removed {
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].RemovedAt > result[j].RemovedAt
	})

	c.JSON(200, gin.H{"removed_nodes": result, "_debug": debugLines})
}

// ── cluster-autoscaler logs (self-hosted CA) ─────────────────────────────────

var labelCandidates = []string{
	"app=cluster-autoscaler",
	"component=cluster-autoscaler",
	"k8s-app=cluster-autoscaler",
	"app.kubernetes.io/name=cluster-autoscaler",
}

func fetchCALogs(ctx context.Context, client kubernetes.Interface, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string

	var caPodName string
	for _, sel := range labelCandidates {
		pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err != nil {
			continue
		}
		if len(pods.Items) > 0 {
			caPodName = pods.Items[0].Name
			debug = append(debug, fmt.Sprintf("[CA] pod encontrado label=%q: %s", sel, caPodName))
			break
		}
	}

	// Fallback por nome — mas excluir coredns-autoscaler e similares
	if caPodName == "" {
		pods, _ := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
		for _, p := range pods.Items {
			n := strings.ToLower(p.Name)
			if strings.Contains(n, "cluster-autoscaler") {
				caPodName = p.Name
				debug = append(debug, fmt.Sprintf("[CA] pod encontrado por nome: %s", caPodName))
				break
			}
		}
	}

	if caPodName == "" {
		debug = append(debug, "[CA] pod cluster-autoscaler não encontrado (AKS managed CA não expõe pod)")
		return nil, debug
	}

	tailLines := int64(5000)
	stream, err := client.CoreV1().Pods("kube-system").GetLogs(caPodName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).Stream(ctx)
	if err != nil {
		debug = append(debug, fmt.Sprintf("[CA] erro ao ler logs: %v", err))
		return nil, debug
	}
	defer stream.Close() //nolint:errcheck

	poolLower := strings.ToLower(pool)
	seen := map[string]*RemovedNodeInfo{}
	contextBuf := map[string][]string{}
	currentTS := ""
	totalLines, matchLines := 0, 0

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		totalLines++
		if m := reKlogPrefix.FindStringSubmatch(line); m != nil {
			currentTS = parseKlogTS(m[1], m[2])
		}
		ll := strings.ToLower(line)
		if !strings.Contains(ll, "removing node") && !strings.Contains(ll, "deleting node") &&
			!(strings.Contains(ll, "scale-down") && strings.Contains(ll, "node")) {
			continue
		}
		matchLines++
		nodeName := extractNodeNameCA(line)
		if nodeName == "" || (pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower)) {
			continue
		}
		if _, ok := seen[nodeName]; ok {
			contextBuf[nodeName] = append(contextBuf[nodeName], line)
		} else {
			seen[nodeName] = &RemovedNodeInfo{Name: nodeName, RemovedAt: currentTS, Reason: caLineSummary(line), Source: "cluster-autoscaler"}
			contextBuf[nodeName] = []string{line}
		}
	}
	for name, n := range seen {
		n.Details = strings.Join(contextBuf[name], "\n")
	}
	debug = append(debug, fmt.Sprintf("[CA] %s: %d linhas, %d remoções, %d nodes únicos", caPodName, totalLines, matchLines, len(seen)))

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

func extractNodeNameCA(line string) string {
	for _, re := range []*regexp.Regexp{reScaleDown, reNodeRemove} {
		if m := re.FindStringSubmatch(line); len(m) > 1 {
			if name := strings.TrimRight(m[1], ",;: \t"); len(name) > 3 {
				return name
			}
		}
	}
	return ""
}

func caLineSummary(line string) string {
	if idx := strings.Index(line, "] "); idx >= 0 {
		msg := strings.TrimSpace(line[idx+2:])
		if len(msg) > 250 {
			return msg[:250] + "..."
		}
		return msg
	}
	return rtrunc(line, 250)
}

func parseKlogTS(mmdd, hhmmss string) string {
	if len(mmdd) != 4 || len(hhmmss) < 8 {
		return ""
	}
	now := time.Now().UTC()
	ts, err := time.ParseInLocation("2006 01 02 15:04:05",
		fmt.Sprintf("%d %s %s %s", now.Year(), mmdd[:2], mmdd[2:], hhmmss[:8]), time.UTC)
	if err != nil {
		return ""
	}
	if ts.After(now.Add(24 * time.Hour)) {
		ts = ts.AddDate(-1, 0, 0)
	}
	return ts.Format(time.RFC3339)
}

// ── eventos K8s em kube-system e default ─────────────────────────────────────

func fetchNodeEventsV2(ctx context.Context, client kubernetes.Interface, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string
	poolLower := strings.ToLower(pool)

	removalReasons := map[string]bool{
		"RemovingNode": true, "ScaleDown": true, "ScaleDownEmpty": true,
		"NodeNotReady": true, "Killing": true, "PreemptingNode": true,
		"EvictionThresholdMet": true, "ScaleUpNotNeeded": true,
	}

	seen := map[string]*RemovedNodeInfo{}
	total, matched := 0, 0

	// Buscar em kube-system E default (onde o CA do AKS normalmente registra)
	for _, ns := range []string{"kube-system", "default", ""} {
		events, err := client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			debug = append(debug, fmt.Sprintf("[Events] ns=%q erro: %v", ns, err))
			continue
		}
		for _, evt := range events.Items {
			total++
			msgLower := strings.ToLower(evt.Message)
			isRemoval := removalReasons[evt.Reason] ||
				strings.Contains(msgLower, "scale down") ||
				strings.Contains(msgLower, "removing node") ||
				strings.Contains(msgLower, "terminat") ||
				strings.Contains(msgLower, "evict")
			if !isRemoval {
				continue
			}

			// Nome do node: pode estar no InvolvedObject ou na mensagem
			nodeName := ""
			if evt.InvolvedObject.Kind == "Node" {
				nodeName = evt.InvolvedObject.Name
			} else {
				// Tentar extrair nome do node da mensagem (ex: "Removing node aks-...")
				nodeName = extractNodeNameCA(evt.Message)
			}
			if nodeName == "" {
				continue
			}
			if pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower) {
				continue
			}
			matched++

			ts := ""
			if !evt.LastTimestamp.IsZero() {
				ts = evt.LastTimestamp.UTC().Format(time.RFC3339)
			} else if !evt.EventTime.IsZero() {
				ts = evt.EventTime.UTC().Format(time.RFC3339)
			}
			detail := fmt.Sprintf("[%s] ns=%s reason=%s: %s", ts, evt.Namespace, evt.Reason, evt.Message)

			if existing, ok := seen[nodeName]; ok {
				existing.Details += "\n" + detail
				if ts > existing.RemovedAt {
					existing.RemovedAt = ts
					existing.Reason = fmt.Sprintf("%s: %s", evt.Reason, rtrunc(evt.Message, 120))
				}
			} else {
				seen[nodeName] = &RemovedNodeInfo{
					Name: nodeName, RemovedAt: ts, Source: "k8s-events",
					Reason:  fmt.Sprintf("%s: %s", evt.Reason, rtrunc(evt.Message, 120)),
					Details: detail,
				}
			}
		}
		if ns == "" {
			break // listagem global — não iterar mais
		}
	}
	debug = append(debug, fmt.Sprintf("[Events] %d eventos verificados, %d relacionados a remoção, %d nodes únicos", total, matched, len(seen)))

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

// ── Azure Activity Log via az CLI ─────────────────────────────────────────────

type azActivityEntry struct {
	OperationName  struct{ Value string `json:"value"` } `json:"operationName"`
	EventTimestamp string                                 `json:"eventTimestamp"`
	Status         struct{ Value string `json:"value"` } `json:"status"`
	ResourceID     string                                 `json:"resourceId"`
	Caller         string                                 `json:"caller"`
}

func fetchAzureActivityLog(ctx context.Context, clusterCfg *config.ClusterConfig, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string

	if _, err := exec.LookPath("az"); err != nil {
		debug = append(debug, "[AzureActivity] az CLI não encontrado")
		return nil, debug
	}

	if clusterCfg == nil {
		debug = append(debug, "[AzureActivity] ClusterConfig não encontrado — clusters-config.json pode estar desatualizado")
		return nil, debug
	}
	debug = append(debug, fmt.Sprintf("[AzureActivity] cluster=%s rg=%s", clusterCfg.Name, clusterCfg.ResourceGroup))

	// Passo 1: obter o nodeResourceGroup (MC_...) via az aks show
	clusterName := strings.TrimSuffix(clusterCfg.Name, "-admin")
	nodeRGArgs := []string{"aks", "show",
		"--name", clusterName,
		"--resource-group", clusterCfg.ResourceGroup,
		"--query", "nodeResourceGroup",
		"-o", "tsv",
	}
	if clusterCfg.SubscriptionID != "" {
		nodeRGArgs = append(nodeRGArgs, "--subscription", clusterCfg.SubscriptionID)
	}
	nodeRGOut, err := exec.CommandContext(ctx, "az", nodeRGArgs...).Output()
	if err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] az aks show falhou: %v", err))
		return nil, debug
	}
	nodeRG := strings.TrimSpace(string(nodeRGOut))
	if nodeRG == "" {
		debug = append(debug, "[AzureActivity] nodeResourceGroup vazio")
		return nil, debug
	}
	debug = append(debug, fmt.Sprintf("[AzureActivity] nodeResourceGroup=%s", nodeRG))

	// Passo 2: activity log no nodeResourceGroup — últimos 7 dias
	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	logArgs := []string{"monitor", "activity-log", "list",
		"--resource-group", nodeRG,
		"--start-time", since,
		"-o", "json",
	}
	if clusterCfg.SubscriptionID != "" {
		logArgs = append(logArgs, "--subscription", clusterCfg.SubscriptionID)
	}
	logOut, err := exec.CommandContext(ctx, "az", logArgs...).Output()
	if err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] activity-log list falhou: %v", err))
		return nil, debug
	}

	var entries []azActivityEntry
	if err := json.Unmarshal(logOut, &entries); err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] parse erro: %v — raw: %.80s", err, string(logOut)))
		return nil, debug
	}
	debug = append(debug, fmt.Sprintf("[AzureActivity] %d entradas no log de %s", len(entries), nodeRG))

	poolLower := strings.ToLower(pool)
	seen := map[string]*RemovedNodeInfo{}
	matched := 0

	for _, e := range entries {
		op := strings.ToLower(e.OperationName.Value)
		// Apenas operações de delete bem-sucedidas em VMs de VMSS
		if !strings.Contains(op, "delete") || !strings.Contains(op, "virtualmachine") {
			continue
		}
		if !strings.EqualFold(e.Status.Value, "Succeeded") && !strings.EqualFold(e.Status.Value, "Accepted") {
			continue
		}

		// Extrair nome do node a partir do resourceId
		// Ex: .../virtualMachineScaleSets/aks-userpool-12345678-vmss/virtualMachines/3
		parts := strings.Split(e.ResourceID, "/")
		// Encontrar o índice do VMSS e construir o nome do node
		nodeName := ""
		for i, p := range parts {
			if strings.EqualFold(p, "virtualmachinescalesets") && i+1 < len(parts) {
				vmssName := parts[i+1]
				// Verificar se o próximo segmento é "virtualMachines" e pegar o índice
				if i+3 < len(parts) && strings.EqualFold(parts[i+2], "virtualmachines") {
					vmIdx := parts[i+3]
					// Nome do node AKS: aks-<pool>-XXXXXXXX-vmss<idx_hex>
					// O índice numérico precisa ser convertido para hex com 6 chars
					if idx := vmssInstanceToNodeName(vmssName, vmIdx); idx != "" {
						nodeName = idx
					} else {
						nodeName = vmssName + "-" + vmIdx
					}
				} else {
					nodeName = vmssName
				}
				break
			}
		}

		if nodeName == "" {
			continue
		}
		if pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower) {
			continue
		}
		matched++

		ts := e.EventTimestamp
		reason := fmt.Sprintf("Azure VMSS delete (status: %s)", e.Status.Value)
		detail := fmt.Sprintf("[%s] %s\nResourceId: %s\nCaller: %s",
			ts, e.OperationName.Value, e.ResourceID, e.Caller)

		if _, exists := seen[nodeName]; !exists {
			seen[nodeName] = &RemovedNodeInfo{
				Name: nodeName, RemovedAt: ts, Reason: reason,
				Source: "azure-activity", Details: detail,
			}
		}
	}
	debug = append(debug, fmt.Sprintf("[AzureActivity] %d operações de delete VM corresponderam, %d nodes únicos", matched, len(seen)))

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

// vmssInstanceToNodeName converte o nome do VMSS + índice numérico no nome do node AKS.
// AKS usa: aks-<pool>-XXXXXXXX-vmss + índice em hex com 6 chars, ex: "000001".
func vmssInstanceToNodeName(vmssName, instanceIdx string) string {
	// instanceIdx é um número decimal, ex: "3"
	var idx int
	if _, err := fmt.Sscanf(instanceIdx, "%d", &idx); err != nil {
		return ""
	}
	return fmt.Sprintf("%s%06x", vmssName, idx)
}

// ── Nodes K8s com NotReady/Unknown ou cordoned (ainda existentes no cluster) ─

func fetchUnhealthyNodes(ctx context.Context, client kubernetes.Interface, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string
	poolLower := strings.ToLower(pool)

	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		debug = append(debug, fmt.Sprintf("[Unhealthy] erro ao listar nodes: %v", err))
		return nil, debug
	}

	var result []*RemovedNodeInfo
	for _, node := range nodeList.Items {
		name := node.Name
		if pool != "" && !strings.Contains(strings.ToLower(name), poolLower) {
			continue
		}

		cordoned := node.Spec.Unschedulable
		readyStatus := corev1.ConditionUnknown
		readyMsg := ""
		lastTS := ""
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				readyStatus = cond.Status
				readyMsg = cond.Message
				if !cond.LastTransitionTime.IsZero() {
					lastTS = cond.LastTransitionTime.UTC().Format(time.RFC3339)
				}
				break
			}
		}

		// Apenas inclui se não estiver pronto OU se estiver cordoned
		if readyStatus == corev1.ConditionTrue && !cordoned {
			continue
		}

		source := "k8s-node-notready"
		label := "NotReady"
		if cordoned && readyStatus == corev1.ConditionTrue {
			source = "k8s-node-cordoned"
			label = "Cordoned"
		} else if cordoned {
			source = "k8s-node-cordoned"
			label = "Cordoned+NotReady"
		}

		reason := fmt.Sprintf("%s: %s", label, rtrunc(readyMsg, 200))
		details := fmt.Sprintf("Status Ready: %s\nCordoned: %v\nMensagem: %s", readyStatus, cordoned, readyMsg)

		result = append(result, &RemovedNodeInfo{
			Name:      name,
			RemovedAt: lastTS,
			Reason:    reason,
			Source:    source,
			Details:   details,
		})
	}
	debug = append(debug, fmt.Sprintf("[Unhealthy] %d nodes K8s analisados, %d não-saudáveis no pool", len(nodeList.Items), len(result)))
	return result, debug
}

func rtrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
