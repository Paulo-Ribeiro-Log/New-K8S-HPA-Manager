package handlers

import (
	"bufio"
	"context"
	"fmt"
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
	Source    string `json:"source"`  // "cluster-autoscaler" | "k8s-events"
	Details   string `json:"details"` // linhas brutas para exibição
}

var (
	reKlogPrefix = regexp.MustCompile(`^[IWEF](\d{4}) (\d{2}:\d{2}:\d{2})`)
	reNodeRemove = regexp.MustCompile(`(?i)(?:removing|deleting) node[:\s]+([\w][\w.-]+)`)
	reScaleDown  = regexp.MustCompile(`(?i)scale[- ]down[^"]*removing node[:\s]+([\w][\w.-]+)`)
)

// GetRemovedNodes retorna nodes removidos recentemente a partir de:
//  1. Logs do pod cluster-autoscaler (kube-system)
//  2. Eventos Kubernetes com involvedObject.kind=Node
//
// Query params: cluster (obrigatório), pool (opcional — filtra por nome do pool)
func (h *NodePoolHandler) GetRemovedNodes(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	pool := strings.TrimSpace(c.Query("pool"))
	if cluster == "" {
		c.JSON(400, gin.H{"error": "cluster obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	removed := map[string]*RemovedNodeInfo{}

	for _, n := range fetchCALogs(ctx, client, pool) {
		removed[n.Name] = n
	}
	for _, n := range fetchNodeEvents(ctx, client, pool) {
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

	c.JSON(200, gin.H{"removed_nodes": result})
}

// ── cluster-autoscaler logs ───────────────────────────────────────────────────

func fetchCALogs(ctx context.Context, client kubernetes.Interface, pool string) []*RemovedNodeInfo {
	pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app=cluster-autoscaler",
	})
	if err != nil || len(pods.Items) == 0 {
		return nil
	}

	tailLines := int64(3000)
	req := client.CoreV1().Pods("kube-system").GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{
		TailLines: &tailLines,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil
	}
	defer stream.Close() //nolint:errcheck

	poolLower := strings.ToLower(pool)
	seen := map[string]*RemovedNodeInfo{}
	contextBuf := map[string][]string{}
	currentTS := ""

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1*1024*1024), 1*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if m := reKlogPrefix.FindStringSubmatch(line); m != nil {
			currentTS = parseKlogTS(m[1], m[2])
		}

		lineLower := strings.ToLower(line)
		isRemoval := strings.Contains(lineLower, "removing node") ||
			strings.Contains(lineLower, "deleting node") ||
			(strings.Contains(lineLower, "scale-down") && strings.Contains(lineLower, "node"))
		if !isRemoval {
			continue
		}

		nodeName := extractNodeNameCA(line)
		if nodeName == "" {
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

	for name, n := range seen {
		n.Details = strings.Join(contextBuf[name], "\n")
	}

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result
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

func fetchNodeEvents(ctx context.Context, client kubernetes.Interface, pool string) []*RemovedNodeInfo {
	events, err := client.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=Node",
	})
	if err != nil {
		return nil
	}

	poolLower := strings.ToLower(pool)
	removalReasons := map[string]bool{
		"RemovingNode": true, "ScaleDown": true, "NodeNotReady": true,
		"Killing": true, "PreemptingNode": true, "EvictionThresholdMet": true,
	}

	seen := map[string]*RemovedNodeInfo{}

	for _, evt := range events.Items {
		msgLower := strings.ToLower(evt.Message)
		if !removalReasons[evt.Reason] &&
			!strings.Contains(msgLower, "remov") &&
			!strings.Contains(msgLower, "delet") &&
			!strings.Contains(msgLower, "scale down") &&
			!strings.Contains(msgLower, "evict") {
			continue
		}

		nodeName := evt.InvolvedObject.Name
		if nodeName == "" {
			continue
		}
		if pool != "" && !strings.Contains(strings.ToLower(nodeName), poolLower) {
			continue
		}

		ts := ""
		if !evt.LastTimestamp.IsZero() {
			ts = evt.LastTimestamp.UTC().Format(time.RFC3339)
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

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result
}

func rtrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
