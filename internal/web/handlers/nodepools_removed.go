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
	CreatedAt string `json:"created_at"` // RFC3339 ou "" — disponível apenas para nodes ainda no cluster
	Reason    string `json:"reason"`
	Source    string `json:"source"`  // "cluster-autoscaler" | "k8s-events" | "azure-activity"
	Details   string `json:"details"` // linhas brutas para exibição
	// Category destaca o tipo de interrupção quando identificado: "spot-eviction" (despejo de
	// VM spot) | "scheduled-event" (Azure Scheduled Event: terminate/reboot/redeploy/freeze) | "".
	Category string `json:"category,omitempty"`
	// Quem executou a remoção e a causa provável — o que responde "por que esse node sumiu?".
	InitiatedBy     string `json:"initiated_by,omitempty"`      // ex: "Identidade do control plane do AKS (…)", "Usuário x@y"
	InitiatedByKind string `json:"initiated_by_kind,omitempty"` // aks | user | service-principal | managed-identity | platform
	LikelyCause     string `json:"likely_cause,omitempty"`
	// Quão específica é a LikelyCause: 2 = evento K8s/log do CA (ex: scale-down, despejo spot),
	// 1 = deduzida do Activity Log (genérica). A mais específica prevalece no merge.
	causePriority int
}

const (
	nodeCategorySpot      = "spot-eviction"
	nodeCategoryScheduled = "scheduled-event"
)

// classifyNodeInterruption identifica eventos/conditions de spot e de Azure Scheduled Events a
// partir do reason/type e da mensagem. Ex. real: reason=SpotEvictionIncoming, msg="Preempt Started."
// (o filtro antigo só olhava a mensagem, procurando "evict"/"terminat", e descartava esse evento).
// Os Scheduled Events chegam ao K8s pelo node-problem-detector do AKS como eventos/conditions
// "<Tipo>Scheduled" (ex: PreemptScheduled, TerminateScheduled) — Preempt é o despejo de spot.
// eventCause descreve responsável/causa para eventos K8s reconhecíveis (0 = não reconhecido).
func eventCause(reason, category string) (who, kind, cause string, priority int) {
	switch category {
	case nodeCategorySpot:
		return "Plataforma Azure", initiatorPlatform, "Despejo de VM spot pelo Azure (falta de capacidade ou preço acima do máximo)", 2
	case nodeCategoryScheduled:
		if reason == "" {
			return "Plataforma Azure", initiatorPlatform, "Manutenção agendada pelo Azure", 2
		}
		return "Plataforma Azure", initiatorPlatform, fmt.Sprintf("Manutenção agendada pelo Azure (%s)", reason), 2
	}
	switch reason {
	case "ScaleDown", "ScaleDownEmpty", "RemovingNode":
		return "cluster-autoscaler", initiatorAKS, "Scale-down do cluster autoscaler (node vazio ou subutilizado)", 2
	}
	return "", "", "", 0
}

func classifyNodeInterruption(reason, message string) string {
	r, m := strings.ToLower(reason), strings.ToLower(message)
	// O NPD também emite o estado "sem evento": reason NoVMEventScheduled, mensagem
	// `Node condition VMEventScheduled is now: False, ... "VM has no scheduled event"` — sinal
	// negativo (node saudável), não interrupção. Falso positivo real num node recém-criado.
	if strings.HasPrefix(r, "no") || strings.Contains(m, "is now: false") ||
		strings.Contains(m, "has no scheduled") || strings.Contains(m, "no scheduled event") {
		return ""
	}
	if strings.Contains(r, "spot") || strings.Contains(r, "preempt") ||
		strings.Contains(m, "preempt") || strings.Contains(m, "spot eviction") || strings.Contains(m, "spotevict") {
		return nodeCategorySpot
	}
	for _, t := range []string{"terminatescheduled", "rebootscheduled", "redeployscheduled", "freezescheduled", "vmeventscheduled"} {
		if strings.Contains(r, t) {
			return nodeCategoryScheduled
		}
	}
	if strings.Contains(m, "scheduled event") {
		return nodeCategoryScheduled
	}
	return ""
}

// mergeRemovedNode junta a informação de uma fonte adicional num node já encontrado: a categoria
// (spot/scheduled) prevalece sobre "sem categoria" e os detalhes são acumulados.
func mergeRemovedNode(dst, src *RemovedNodeInfo) {
	if dst.Category == "" && src.Category != "" {
		dst.Category = src.Category
		dst.Reason = src.Reason
	}
	// Causa: a mais específica vence (evento/log do CA > dedução do Activity Log). Responsável:
	// o Activity Log identifica a identidade exata, então ele completa/substitui um genérico.
	if src.LikelyCause != "" && src.causePriority > dst.causePriority {
		dst.LikelyCause = src.LikelyCause
		dst.causePriority = src.causePriority
	}
	if src.InitiatedBy != "" && (dst.InitiatedBy == "" || src.Source == "azure-activity") {
		dst.InitiatedBy = src.InitiatedBy
		dst.InitiatedByKind = src.InitiatedByKind
	}
	if dst.RemovedAt == "" {
		dst.RemovedAt = src.RemovedAt
	}
	if src.Details != "" && !strings.Contains(dst.Details, src.Details) {
		if dst.Details != "" {
			dst.Details += "\n"
		}
		dst.Details += src.Details
	}
	if dst.CreatedAt == "" {
		dst.CreatedAt = src.CreatedAt
	}
}

var (
	reKlogPrefix = regexp.MustCompile(`^[IWEF](\d{4}) (\d{2}:\d{2}:\d{2})`)
	// Aceita nomes com ou sem aspas: "removing empty node "aks-..." e "removing node aks-..."
	reNodeRemove = regexp.MustCompile(`(?i)(?:removing|deleting)(?:\s+\w+)*\s+node[:\s]+"?([\w][\w.-]+)`)
	reScaleDown  = regexp.MustCompile(`(?i)scale[- ]down.*?removing(?:\s+\w+)*\s+node[:\s]+"?([\w][\w.-]+)`)
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
		if existing, ok := removed[n.Name]; ok {
			mergeRemovedNode(existing, n)
		} else {
			removed[n.Name] = n
		}
	}

	// ── Fonte 3: Azure Activity Log via nodeResourceGroup (7 dias) ───────────
	azNodes, azDebug := fetchAzureActivityLog(ctx, clusterCfg, pool)
	debugLines = append(debugLines, azDebug...)
	for _, n := range azNodes {
		if existing, ok := removed[n.Name]; ok {
			mergeRemovedNode(existing, n)
		} else {
			removed[n.Name] = n
		}
	}

	// ── Fonte 4: Nodes K8s com NotReady/Unknown ou cordoned ──────────────────
	unhealthyNodes, uhDebug := fetchUnhealthyNodes(ctx, client, pool)
	debugLines = append(debugLines, uhDebug...)
	for _, n := range unhealthyNodes {
		if existing, ok := removed[n.Name]; ok {
			mergeRemovedNode(existing, n)
		} else {
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
			seen[nodeName] = &RemovedNodeInfo{Name: nodeName, RemovedAt: currentTS, Reason: caLineSummary(line), Source: "cluster-autoscaler",
				InitiatedBy: "cluster-autoscaler", InitiatedByKind: initiatorAKS,
				LikelyCause: "Scale-down do cluster autoscaler (node vazio ou subutilizado)", causePriority: 2}
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
			category := classifyNodeInterruption(evt.Reason, evt.Message)
			isRemoval := category != "" || removalReasons[evt.Reason] ||
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

			reason := fmt.Sprintf("%s: %s", evt.Reason, rtrunc(evt.Message, 120))
			who, kind, cause, prio := eventCause(evt.Reason, category)
			if existing, ok := seen[nodeName]; ok {
				if prio > existing.causePriority {
					existing.InitiatedBy, existing.InitiatedByKind, existing.LikelyCause, existing.causePriority = who, kind, cause, prio
				}
				existing.Details += "\n" + detail
				if ts > existing.RemovedAt {
					existing.RemovedAt = ts
					if existing.Category == "" || category != "" {
						existing.Reason = reason
					}
				}
				// Spot/scheduled é a informação mais útil — não deixa um evento genérico mais
				// recente (ex: NodeNotReady logo depois do despejo) esconder a causa.
				if existing.Category == "" && category != "" {
					existing.Category = category
					existing.Reason = reason
				}
			} else {
				seen[nodeName] = &RemovedNodeInfo{
					Name: nodeName, RemovedAt: ts, Source: "k8s-events",
					Reason: reason, Details: detail, Category: category,
					InitiatedBy: who, InitiatedByKind: kind, LikelyCause: cause, causePriority: prio,
				}
			}
		}
		if ns == "" {
			break // listagem global — não iterar mais
		}
	}
	debug = append(debug, fmt.Sprintf("[Events] %d eventos verificados, %d relacionados a remoção, %d nodes únicos", total, matched, len(seen)))
	debug = append(debug, "[Events] eventos K8s (inclusive SpotEvictionIncoming/Scheduled Events) ficam ~1h no cluster — despejos mais antigos aparecem só como remoção (Activity Log), sem a indicação de spot")

	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result, debug
}

// ── Azure Activity Log via az CLI ─────────────────────────────────────────────

type azActivityEntry struct {
	OperationName struct {
		Value          string `json:"value"`
		LocalizedValue string `json:"localizedValue"`
	} `json:"operationName"`
	EventTimestamp string `json:"eventTimestamp"`
	Status         struct {
		Value string `json:"value"`
	} `json:"status"`
	ResourceID    string                 `json:"resourceId"`
	Caller        string                 `json:"caller"`
	CorrelationID string                 `json:"correlationId"`
	Claims        map[string]interface{} `json:"claims"`
	Properties    map[string]interface{} `json:"properties"`
	HTTPRequest   struct {
		ClientIPAddress string `json:"clientIpAddress"`
	} `json:"httpRequest"`
}

// aksIdentities são as identidades do cluster usadas para reconhecer o "caller" do Activity Log.
// Todos os valores em minúsculas (GUIDs e resource IDs).
type aksIdentities struct {
	ids       map[string]string // objectId/clientId/principalId → descrição
	resources map[string]string // resource ID da managed identity → descrição
}

// aksResourceProviderAppID é o app first-party do AKS ("AzureContainerService"), que aparece como
// appid quando a própria plataforma do AKS age no VMSS.
const aksResourceProviderAppID = "7319c514-987d-4e9b-ac3d-d38c4f427f4c"

// Tipos de responsável (InitiatedByKind).
const (
	initiatorAKS      = "aks"
	initiatorUser     = "user"
	initiatorSP       = "service-principal"
	initiatorMI       = "managed-identity"
	initiatorPlatform = "platform"
)

func claimStr(claims map[string]interface{}, key string) string {
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// resolveActivityCaller traduz quem executou a operação (caller + claims) para algo legível.
func resolveActivityCaller(e azActivityEntry, ids aksIdentities) (who, kind string) {
	caller := strings.TrimSpace(e.Caller)
	appID := strings.ToLower(claimStr(e.Claims, "appid"))
	oid := strings.ToLower(claimStr(e.Claims, "http://schemas.microsoft.com/identity/claims/objectidentifier"))
	if oid == "" {
		oid = strings.ToLower(claimStr(e.Claims, "oid"))
	}
	mirid := strings.ToLower(claimStr(e.Claims, "xms_mirid"))

	if mirid != "" {
		if d, ok := ids.resources[mirid]; ok {
			return d, initiatorAKS
		}
		if strings.Contains(mirid, "/providers/microsoft.containerservice/managedclusters/") {
			return "Identidade do cluster AKS (system-assigned)", initiatorAKS
		}
		parts := strings.Split(mirid, "/")
		return fmt.Sprintf("Managed identity %s", parts[len(parts)-1]), initiatorMI
	}
	for _, id := range []string{oid, appID, strings.ToLower(caller)} {
		if d, ok := ids.ids[id]; ok && id != "" {
			return d, initiatorAKS
		}
	}
	if appID == aksResourceProviderAppID {
		return "Serviço do AKS (Azure Container Service)", initiatorAKS
	}
	if strings.Contains(caller, "@") || strings.EqualFold(claimStr(e.Claims, "idtyp"), "user") {
		name := claimStr(e.Claims, "name")
		if name != "" && !strings.EqualFold(name, caller) {
			return fmt.Sprintf("Usuário %s (%s)", name, caller), initiatorUser
		}
		return fmt.Sprintf("Usuário %s", caller), initiatorUser
	}
	if caller != "" {
		if appID != "" && appID != strings.ToLower(caller) {
			return fmt.Sprintf("Service principal %s (appid %s)", caller, appID), initiatorSP
		}
		return fmt.Sprintf("Service principal %s", caller), initiatorSP
	}
	return "Plataforma Azure", initiatorPlatform
}

// activityLikelyCause descreve a causa provável a partir de quem fez e do tipo de operação.
func activityLikelyCause(kind, op string) string {
	op = strings.ToLower(op)
	wholeVMSS := strings.HasSuffix(op, "virtualmachinescalesets/delete")
	switch kind {
	case initiatorAKS:
		if wholeVMSS {
			return "Operação do AKS: node pool removido (VMSS inteiro excluído)"
		}
		return "Operação do AKS: scale-down do cluster autoscaler, redução manual do pool (portal/az aks nodepool scale) ou upgrade/reimage do pool"
	case initiatorUser:
		return "Ação manual de usuário (portal ou CLI) direto no VMSS"
	case initiatorSP:
		return "Automação com service principal (pipeline, Terraform ou script)"
	case initiatorMI:
		return "Automação com managed identity (fora do AKS)"
	default:
		return "Plataforma Azure (ex.: despejo de VM spot ou manutenção)"
	}
}

// requestInstanceIDs extrai instanceIds do requestbody ({"instanceIds":["3","10"]}) — é por ele
// que dá para chegar ao node exato quando a operação é "delete instances" no VMSS.
func requestInstanceIDs(props map[string]interface{}) []string {
	raw, _ := props["requestbody"].(string)
	if raw == "" {
		return nil
	}
	var body struct {
		InstanceIDs []string `json:"instanceIds"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil {
		return nil
	}
	return body.InstanceIDs
}

// parseActivityRemovals transforma as entradas do Activity Log em nodes removidos, com responsável
// e causa provável. Pura (sem az) para ser testável.
func parseActivityRemovals(entries []azActivityEntry, ids aksIdentities, pool string) []*RemovedNodeInfo {
	// instanceIds podem vir só numa das entradas da operação (Started/Accepted/Succeeded) —
	// agrupa por correlationId.
	instByCorr := map[string][]string{}
	for _, e := range entries {
		if inst := requestInstanceIDs(e.Properties); len(inst) > 0 && e.CorrelationID != "" {
			instByCorr[e.CorrelationID] = inst
		}
	}

	poolLower := strings.ToLower(pool)
	seen := map[string]*RemovedNodeInfo{}
	for _, e := range entries {
		op := strings.ToLower(e.OperationName.Value)
		if !strings.Contains(op, "delete") || !strings.Contains(op, "virtualmachine") {
			continue
		}
		if !strings.EqualFold(e.Status.Value, "Succeeded") && !strings.EqualFold(e.Status.Value, "Accepted") {
			continue
		}

		// .../virtualMachineScaleSets/<vmss>[/virtualMachines/<idx>]
		vmss, vmIdx := "", ""
		parts := strings.Split(e.ResourceID, "/")
		for i, p := range parts {
			if strings.EqualFold(p, "virtualmachinescalesets") && i+1 < len(parts) {
				vmss = parts[i+1]
				if i+3 < len(parts) && strings.EqualFold(parts[i+2], "virtualmachines") {
					vmIdx = parts[i+3]
				}
				break
			}
		}
		if vmss == "" || (pool != "" && !strings.Contains(strings.ToLower(vmss), poolLower)) {
			continue
		}

		var names []string
		instances := []string{vmIdx}
		if vmIdx == "" {
			instances = instByCorr[e.CorrelationID]
		}
		for _, idx := range instances {
			if idx == "" {
				continue
			}
			if n := vmssInstanceToNodeName(vmss, idx); n != "" {
				names = append(names, n)
			} else {
				names = append(names, vmss+"-"+idx)
			}
		}
		if len(names) == 0 {
			names = []string{vmss} // instância não identificada: fica o VMSS
		}

		who, kind := resolveActivityCaller(e, ids)
		cause := activityLikelyCause(kind, op)
		opLabel := e.OperationName.LocalizedValue
		if opLabel == "" {
			opLabel = e.OperationName.Value
		}
		lines := []string{
			fmt.Sprintf("[%s] %s (%s)", e.EventTimestamp, opLabel, e.Status.Value),
			"Responsável: " + who,
			"Causa provável: " + cause,
			"ResourceId: " + e.ResourceID,
			"Caller: " + e.Caller,
		}
		if v := claimStr(e.Claims, "appid"); v != "" {
			lines = append(lines, "AppId: "+v)
		}
		if v := claimStr(e.Claims, "xms_mirid"); v != "" {
			lines = append(lines, "Managed identity: "+v)
		}
		if e.HTTPRequest.ClientIPAddress != "" {
			lines = append(lines, "IP de origem: "+e.HTTPRequest.ClientIPAddress)
		}
		if e.CorrelationID != "" {
			lines = append(lines, "CorrelationId: "+e.CorrelationID)
		}
		if vmIdx == "" && len(instByCorr[e.CorrelationID]) == 0 {
			lines = append(lines, "Instância não identificada no Activity Log (sem instanceIds) — exibindo o VMSS")
		}
		detail := strings.Join(lines, "\n")

		for _, name := range names {
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = &RemovedNodeInfo{
				Name: name, RemovedAt: e.EventTimestamp, Source: "azure-activity",
				Reason:      fmt.Sprintf("%s — %s", opLabel, who),
				Details:     detail,
				InitiatedBy: who, InitiatedByKind: kind, LikelyCause: cause, causePriority: 1,
			}
		}
	}
	result := make([]*RemovedNodeInfo, 0, len(seen))
	for _, n := range seen {
		result = append(result, n)
	}
	return result
}

// loadAKSIdentities lê as identidades do cluster a partir do JSON do `az aks show`.
func loadAKSIdentities(raw []byte) (nodeRG string, ids aksIdentities) {
	ids = aksIdentities{ids: map[string]string{}, resources: map[string]string{}}
	var show struct {
		NodeRG      string `json:"nodeRG"`
		CPPrincipal string `json:"cpPrincipal"`
		UAI         map[string]struct {
			PrincipalID string `json:"principalId"`
			ClientID    string `json:"clientId"`
		} `json:"uai"`
		Kubelet *struct {
			ClientID   string `json:"clientId"`
			ObjectID   string `json:"objectId"`
			ResourceID string `json:"resourceId"`
		} `json:"kubelet"`
		SPClientID string `json:"spClientId"`
	}
	if json.Unmarshal(raw, &show) != nil {
		return "", ids
	}
	add := func(id, desc string) {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" && id != "msi" {
			ids.ids[id] = desc
		}
	}
	add(show.CPPrincipal, "Identidade do control plane do AKS (system-assigned)")
	for res, v := range show.UAI {
		parts := strings.Split(res, "/")
		desc := fmt.Sprintf("Identidade do control plane do AKS (%s)", parts[len(parts)-1])
		ids.resources[strings.ToLower(res)] = desc
		add(v.PrincipalID, desc)
		add(v.ClientID, desc)
	}
	if show.Kubelet != nil {
		parts := strings.Split(show.Kubelet.ResourceID, "/")
		desc := fmt.Sprintf("Identidade kubelet do AKS (%s)", parts[len(parts)-1])
		if show.Kubelet.ResourceID != "" {
			ids.resources[strings.ToLower(show.Kubelet.ResourceID)] = desc
		}
		add(show.Kubelet.ObjectID, desc)
		add(show.Kubelet.ClientID, desc)
	}
	add(show.SPClientID, "Service principal do cluster AKS")
	return strings.TrimSpace(show.NodeRG), ids
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

	// Passo 1: nodeResourceGroup (MC_...) + identidades do cluster (para reconhecer o caller)
	clusterName := strings.TrimSuffix(clusterCfg.Name, "-admin")
	showArgs := []string{"aks", "show",
		"--name", clusterName,
		"--resource-group", clusterCfg.ResourceGroup,
		"--query", "{nodeRG:nodeResourceGroup, cpPrincipal:identity.principalId, uai:identity.userAssignedIdentities, kubelet:identityProfile.kubeletidentity, spClientId:servicePrincipalProfile.clientId}",
		"-o", "json",
	}
	if clusterCfg.SubscriptionID != "" {
		showArgs = append(showArgs, "--subscription", clusterCfg.SubscriptionID)
	}
	showOut, err := exec.CommandContext(ctx, "az", showArgs...).Output()
	if err != nil {
		debug = append(debug, fmt.Sprintf("[AzureActivity] az aks show falhou: %v", err))
		return nil, debug
	}
	nodeRG, ids := loadAKSIdentities(showOut)
	if nodeRG == "" {
		debug = append(debug, "[AzureActivity] nodeResourceGroup vazio")
		return nil, debug
	}
	debug = append(debug, fmt.Sprintf("[AzureActivity] nodeResourceGroup=%s, %d identidades do cluster conhecidas", nodeRG, len(ids.ids)))

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

	result := parseActivityRemovals(entries, ids, pool)
	debug = append(debug, fmt.Sprintf("[AzureActivity] %d nodes removidos identificados", len(result)))
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

		// Conditions do node-problem-detector (AKS) para Scheduled Events ativos: ex.
		// PreemptScheduled=True (spot prestes a ser despejado), TerminateScheduled=True.
		category, catLine, catType := "", "", ""
		for _, cond := range node.Status.Conditions {
			if cond.Status != corev1.ConditionTrue {
				continue
			}
			if cat := classifyNodeInterruption(string(cond.Type), cond.Message); cat != "" && cond.Type != corev1.NodeReady {
				category, catType = cat, string(cond.Type)
				catLine = fmt.Sprintf("Condition %s=True: %s %s", cond.Type, cond.Reason, cond.Message)
				if !cond.LastTransitionTime.IsZero() {
					lastTS = cond.LastTransitionTime.UTC().Format(time.RFC3339)
				}
				break
			}
		}

		// Apenas inclui se não estiver pronto, se estiver cordoned ou com Scheduled Event ativo
		if readyStatus == corev1.ConditionTrue && !cordoned && category == "" {
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
		who, kind, cause, prio := eventCause(catType, category)
		if category != "" {
			if readyStatus == corev1.ConditionTrue && !cordoned {
				source = "k8s-node-scheduled-event"
			}
			reason = rtrunc(catLine, 200)
			details = catLine + "\n" + details
		}

		createdAt := ""
		if !node.CreationTimestamp.IsZero() {
			createdAt = node.CreationTimestamp.UTC().Format(time.RFC3339)
		}
		result = append(result, &RemovedNodeInfo{
			Name:        name,
			RemovedAt:   lastTS,
			CreatedAt:   createdAt,
			Reason:      reason,
			Source:      source,
			Details:     details,
			Category:    category,
			InitiatedBy: who, InitiatedByKind: kind, LikelyCause: cause, causePriority: prio,
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

// ── Eventos K8s filtrados por node específico ────────────────────────────────

// NodeEvent representa um evento Kubernetes formatado para exibição.
type NodeEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Age       string `json:"age"`
	Count     int32  `json:"count"`
	From      string `json:"from"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// GetNodeEvents retorna eventos K8s relacionados a um node específico,
// buscando em todos os namespaces (útil para eventos do cluster-autoscaler em kube-system).
func (h *NodePoolHandler) GetNodeEvents(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	nodeName := strings.TrimSpace(c.Query("node"))
	if cluster == "" || nodeName == "" {
		c.JSON(400, gin.H{"error": "cluster e node obrigatórios"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	// Lista global (namespace="") para pegar eventos do kube-system e outros em uma chamada
	evtList, err := client.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("erro ao listar eventos: %v", err)})
		return
	}

	nodeNameLower := strings.ToLower(nodeName)
	seen := map[string]bool{}
	var events []NodeEvent

	for _, evt := range evtList.Items {
		// Match: evento diretamente no node OU mensagem menciona o node pelo nome
		directNode := evt.InvolvedObject.Kind == "Node" &&
			strings.EqualFold(evt.InvolvedObject.Name, nodeName)
		mentionsNode := strings.Contains(strings.ToLower(evt.Message), nodeNameLower)

		if !directNode && !mentionsNode {
			continue
		}

		// Deduplicar por UID
		uid := string(evt.UID)
		if seen[uid] {
			continue
		}
		seen[uid] = true

		var t time.Time
		if !evt.LastTimestamp.IsZero() {
			t = evt.LastTimestamp.UTC()
		} else if !evt.EventTime.IsZero() {
			t = evt.EventTime.UTC()
		}

		ts := ""
		age := ""
		if !t.IsZero() {
			ts = t.Format(time.RFC3339)
			age = formatAge(time.Since(t))
		}

		from := evt.Source.Component

		events = append(events, NodeEvent{
			Type:      evt.Type,
			Reason:    evt.Reason,
			Age:       age,
			Count:     evt.Count,
			From:      from,
			Message:   evt.Message,
			Timestamp: ts,
		})
	}

	// Mais recente primeiro
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp > events[j].Timestamp
	})

	c.JSON(200, gin.H{"events": events})
}
