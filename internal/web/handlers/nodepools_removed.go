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

// GetRemovedNodes retorna nodes removidos recentemente a partir de:
//  1. Logs do pod cluster-autoscaler (tenta vários label selectors + busca por nome)
//  2. Eventos Kubernetes com involvedObject.kind=Node
//  3. Azure Activity Log via az CLI (AKS — fallback)
//
// Query params: cluster (obrigatório), pool (opcional)
// O campo _debug na resposta descreve o que cada fonte encontrou.
func (h *NodePoolHandler) GetRemovedNodes(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	pool := strings.TrimSpace(c.Query("pool"))
	if cluster == "" {
		c.JSON(400, gin.H{"error": "cluster obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	removed := map[string]*RemovedNodeInfo{}
	var debugLines []string

	// ── Fonte 1: CA logs ──────────────────────────────────────────────────────
	caNodes, caDebug := fetchCALogsV2(ctx, client, pool)
	debugLines = append(debugLines, caDebug...)
	for _, n := range caNodes {
		removed[n.Name] = n
	}

	// ── Fonte 2: Eventos K8s em Node objects ──────────────────────────────────
	evtNodes, evtDebug := fetchNodeEventsV2(ctx, client, pool)
	debugLines = append(debugLines, evtDebug...)
	for _, n := range evtNodes {
		if _, exists := removed[n.Name]; !exists {
			removed[n.Name] = n
		}
	}

	// ── Fonte 3: Azure Activity Log (az CLI) ──────────────────────────────────
	azNodes, azDebug := fetchAzureActivityLog(ctx, cluster, pool)
	debugLines = append(debugLines, azDebug...)
	for _, n := range azNodes {
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

	c.JSON(200, gin.H{
		"removed_nodes": result,
		"_debug":        debugLines,
	})
}

// ── cluster-autoscaler logs ───────────────────────────────────────────────────

// labelCandidates lista os seletores de label a tentar para encontrar o pod CA.
var labelCandidates = []string{
	"app=cluster-autoscaler",
	"component=cluster-autoscaler",
	"k8s-app=cluster-autoscaler",
	"app.kubernetes.io/name=cluster-autoscaler",
}

func fetchCALogsV2(ctx context.Context, client kubernetes.Interface, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string

	// Tentar encontrar o pod CA por vários label selectors
	var caPodName string
	for _, sel := range labelCandidates {
		pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
			LabelSelector: sel,
		})
		if err != nil {
			debug = append(debug, fmt.Sprintf("[CA] label selector %q: erro %v", sel, err))
			continue
		}
		if len(pods.Items) > 0 {
			caPodName = pods.Items[0].Name
			debug = append(debug, fmt.Sprintf("[CA] pod encontrado com label %q: %s", sel, caPodName))
			break
		}
		debug = append(debug, fmt.Sprintf("[CA] label selector %q: nenhum pod", sel))
	}

	// Fallback: busca pelo nome do pod
	if caPodName == "" {
		pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, p := range pods.Items {
				if strings.Contains(strings.ToLower(p.Name), "cluster-autoscaler") ||
					strings.Contains(strings.ToLower(p.Name), "autoscaler") {
					caPodName = p.Name
					debug = append(debug, fmt.Sprintf("[CA] pod encontrado por nome: %s", caPodName))
					break
				}
			}
		}
	}

	if caPodName == "" {
		debug = append(debug, "[CA] pod cluster-autoscaler não encontrado em kube-system")
		return nil, debug
	}

	tailLines := int64(5000)
	req := client.CoreV1().Pods("kube-system").GetLogs(caPodName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		debug = append(debug, fmt.Sprintf("[CA] erro ao ler logs de %s: %v", caPodName, err))
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

		lineLower := strings.ToLower(line)
		isRemoval := strings.Contains(lineLower, "removing node") ||
			strings.Contains(lineLower, "deleting node") ||
			strings.Contains(lineLower, "node removed") ||
			(strings.Contains(lineLower, "scale-down") && strings.Contains(lineLower, "node")) ||
			(strings.Contains(lineLower, "scaledown") && strings.Contains(lineLower, "node"))
		if !isRemoval {
			continue
		}
		matchLines++

		nodeName := extractNodeNameCA(line)
		if nodeName == "" {
			debug = append(debug, fmt.Sprintf("[CA] linha sem nome de node: %.120s", line))
			continue
		}
		if pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower) {
			continue
		}

		if existing, ok := seen[nodeName]; ok {
			contextBuf[nodeName] = append(contextBuf[nodeName], line)
			if existing.RemovedAt == "" && currentTS != "" {
				existing.RemovedAt = currentTS
			}
		} else {
			seen[nodeName] = &RemovedNodeInfo{
				Name:      nodeName,
				RemovedAt: currentTS,
				Reason:    caLineSummary(line),
				Source:    "cluster-autoscaler",
			}
			contextBuf[nodeName] = []string{line}
		}
	}

	debug = append(debug, fmt.Sprintf("[CA] %s: %d linhas lidas, %d com remoção, %d nodes únicos", caPodName, totalLines, matchLines, len(seen)))

	for name, n := range seen {
		n.Details = strings.Join(contextBuf[name], "\n")
	}

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

func extractNodeNameCA(line string) string {
	for _, re := range []*regexp.Regexp{reScaleDown, reNodeRemove} {
		if m := re.FindStringSubmatch(line); len(m) > 1 {
			name := strings.TrimRight(m[1], ",;: \t")
			if len(name) > 3 {
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
	if len(line) > 250 {
		return line[:250] + "..."
	}
	return line
}

// parseKlogTS converte "MMDD" + "HH:MM:SS" do klog para RFC3339 UTC.
func parseKlogTS(mmdd, hhmmss string) string {
	if len(mmdd) != 4 || len(hhmmss) < 8 {
		return ""
	}
	now := time.Now().UTC()
	ts, err := time.ParseInLocation("2006 01 02 15:04:05",
		fmt.Sprintf("%d %s %s %s", now.Year(), mmdd[:2], mmdd[2:], hhmmss[:8]),
		time.UTC)
	if err != nil {
		return ""
	}
	if ts.After(now.Add(24 * time.Hour)) {
		ts = ts.AddDate(-1, 0, 0)
	}
	return ts.Format(time.RFC3339)
}

// ── eventos Kubernetes em Node objects ───────────────────────────────────────

func fetchNodeEventsV2(ctx context.Context, client kubernetes.Interface, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string

	// Busca todos os eventos (sem field selector — compatibilidade máxima) e filtra em código
	events, err := client.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		debug = append(debug, fmt.Sprintf("[Events] erro ao listar: %v", err))
		return nil, debug
	}

	poolLower := strings.ToLower(pool)
	removalReasons := map[string]bool{
		"RemovingNode": true, "ScaleDown": true, "NodeNotReady": true,
		"Killing": true, "PreemptingNode": true, "EvictionThresholdMet": true,
		"NodeHasDiskPressure": true, "NodeHasMemoryPressure": true,
	}

	seen := map[string]*RemovedNodeInfo{}
	total, matched := 0, 0

	for _, evt := range events.Items {
		if evt.InvolvedObject.Kind != "Node" {
			continue
		}
		total++

		msgLower := strings.ToLower(evt.Message)
		if !removalReasons[evt.Reason] &&
			!strings.Contains(msgLower, "remov") &&
			!strings.Contains(msgLower, "delet") &&
			!strings.Contains(msgLower, "scale down") &&
			!strings.Contains(msgLower, "evict") &&
			!strings.Contains(msgLower, "terminat") {
			continue
		}
		matched++

		nodeName := evt.InvolvedObject.Name
		if pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower) {
			continue
		}

		ts := ""
		if !evt.LastTimestamp.IsZero() {
			ts = evt.LastTimestamp.UTC().Format(time.RFC3339)
		} else if !evt.EventTime.IsZero() {
			ts = evt.EventTime.UTC().Format(time.RFC3339)
		}

		detail := fmt.Sprintf("[%s] %s: %s", ts, evt.Reason, evt.Message)

		if existing, ok := seen[nodeName]; ok {
			existing.Details += "\n" + detail
			if ts > existing.RemovedAt {
				existing.RemovedAt = ts
				existing.Reason = fmt.Sprintf("%s: %s", evt.Reason, rtrunc(evt.Message, 120))
			}
		} else {
			seen[nodeName] = &RemovedNodeInfo{
				Name:      nodeName,
				RemovedAt: ts,
				Reason:    fmt.Sprintf("%s: %s", evt.Reason, rtrunc(evt.Message, 120)),
				Source:    "k8s-events",
				Details:   detail,
			}
		}
	}

	debug = append(debug, fmt.Sprintf("[Events] total=%d Node events=%d removal-related=%d nodes únicos=%d", len(events.Items), total, matched, len(seen)))

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

// ── Azure Activity Log (az CLI) ───────────────────────────────────────────────

type azActivityEntry struct {
	OperationName struct {
		Value string `json:"value"`
	} `json:"operationName"`
	EventTimestamp string `json:"eventTimestamp"`
	Status         struct {
		Value string `json:"value"`
	} `json:"status"`
	ResourceID string `json:"resourceId"`
	Caller     string `json:"caller"`
	Properties struct {
		StatusCode string `json:"statusCode"`
		Message    string `json:"message"`
	} `json:"properties"`
}

func fetchAzureActivityLog(ctx context.Context, cluster, pool string) ([]*RemovedNodeInfo, []string) {
	var debug []string

	// Verificar se az CLI está disponível
	if _, err := exec.LookPath("az"); err != nil {
		debug = append(debug, "[AzureActivity] az CLI não encontrado — ignorando")
		return nil, debug
	}

	// Janela de 48h
	since := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02T15:04:05Z")
	cmd := exec.CommandContext(ctx, "az", "monitor", "activity-log", "list",
		"--start-time", since,
		"--query", fmt.Sprintf("[?contains(to_string(operationName.value), 'delete') && contains(to_string(resourceId), '%s')]", strings.ToLower(cluster)),
		"-o", "json",
	)
	out, err := cmd.Output()
	if err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] erro ao executar az: %v", err))
		return nil, debug
	}

	var entries []azActivityEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] erro ao parsear JSON: %v — raw: %.100s", err, string(out)))
		return nil, debug
	}

	debug = append(debug, fmt.Sprintf("[AzureActivity] %d entradas retornadas", len(entries)))

	poolLower := strings.ToLower(pool)
	seen := map[string]*RemovedNodeInfo{}

	for _, e := range entries {
		op := strings.ToLower(e.OperationName.Value)
		rid := strings.ToLower(e.ResourceID)
		if !strings.Contains(op, "delete") {
			continue
		}
		// Extrair nome do node/VM a partir do resourceId
		// Formato típico: .../virtualMachineScaleSets/<vmss>/virtualMachines/<idx>
		// ou .../virtualMachines/<name>
		parts := strings.Split(rid, "/")
		if len(parts) == 0 {
			continue
		}
		candidate := parts[len(parts)-1]
		// Tentar reconstruir nome do node AKS: aks-<pool>-XXXXX-vmss<idx>
		if pool != "" && !strings.Contains(candidate, poolLower) {
			continue
		}
		if candidate == "" || len(candidate) < 4 {
			continue
		}

		ts := e.EventTimestamp
		reason := fmt.Sprintf("Operação Azure: %s (status: %s)", e.OperationName.Value, e.Status.Value)
		if e.Properties.Message != "" {
			reason += " — " + rtrunc(e.Properties.Message, 100)
		}
		detail := fmt.Sprintf("[%s] %s\nResourceId: %s\nCaller: %s",
			ts, e.OperationName.Value, e.ResourceID, e.Caller)

		if _, exists := seen[candidate]; !exists {
			seen[candidate] = &RemovedNodeInfo{
				Name:      candidate,
				RemovedAt: ts,
				Reason:    reason,
				Source:    "azure-activity",
				Details:   detail,
			}
		}
	}

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

func rtrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
