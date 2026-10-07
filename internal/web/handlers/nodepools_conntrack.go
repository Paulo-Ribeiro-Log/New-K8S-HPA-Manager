package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ConntrackNodeStats estatísticas de conntrack de um nó
type ConntrackNodeStats struct {
	NodeName string `json:"node_name"`
	Count    int64  `json:"count"`
	Max      int64  `json:"max"`
	Buckets  int64  `json:"buckets"`
	// MaxMapCount é o sysctl vm.max_map_count do nó (não é de conntrack, mas é lido no mesmo
	// exec — /proc/sys/vm é global do host, visível de qualquer container). -1 = não lido.
	MaxMapCount int64 `json:"max_map_count"`
	// Contadores de descarte do conntrack (somados entre CPUs, acumulados desde o boot do nó),
	// lidos de /proc/net/stat/nf_conntrack ou, se o kernel não tiver esse arquivo (ex: AKS
	// 5.15-azure, sem CONFIG_NF_CONNTRACK_PROCFS), de `conntrack -S` (netlink). -1 = não lido.
	//   Drop:         tabela cheia e pacote descartado — o "nf_conntrack: table full, dropping packet" do dmesg
	//   EarlyDrop:    tabela cheia e uma conexão antiga despejada para abrir espaço
	//   InsertFailed: falha ao inserir a entrada (ex: corrida de DNS via UDP)
	Drop          int64               `json:"drop"`
	EarlyDrop     int64               `json:"early_drop"`
	InsertFailed  int64               `json:"insert_failed"`
	DropPerCPU    []ConntrackCPUDrops `json:"drop_per_cpu,omitempty"` // os mesmos contadores, por CPU
	DropSource    string              `json:"drop_source,omitempty"`  // "procfs" | "conntrack -S"
	DropError     string              `json:"drop_error,omitempty"`   // por que os contadores não foram lidos
	UptimeSeconds int64               `json:"uptime_seconds"`         // uptime do nó, para dar escala aos contadores (-1 = não lido)
	UsagePct      float64             `json:"usage_pct"`
	Status        string              `json:"status"` // ok / warning / critical / error
	ProbeMethod   string              `json:"probe_method"`
	Error         string              `json:"error,omitempty"`
}

// ConntrackResponse resposta do endpoint de conntrack
type ConntrackResponse struct {
	NodePool  string               `json:"node_pool"`
	Cluster   string               `json:"cluster"`
	Nodes     []ConntrackNodeStats `json:"nodes"`
	FetchedAt time.Time            `json:"fetched_at"`
}

// GetConntrackStats retorna estatísticas de conntrack para os nós de um node pool
// (parâmetro nodepool) ou, se nodepool for omitido, para TODOS os nós do cluster —
// usado pelo ConntrackAlertWidget pra alertar sobre qualquer node saturado sem exigir
// que um pool específico esteja selecionado.
// GET /api/v1/nodepools/conntrack?cluster=X[&nodepool=Y]
func (h *NodePoolHandler) GetConntrackStats(c *gin.Context) {
	cluster := c.Query("cluster")
	nodepool := c.Query("nodepool")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao conectar ao cluster: %v", err)})
		return
	}

	restConfig, err := h.kubeManager.GetRestConfig(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao obter restConfig: %v", err)})
		return
	}

	var nodes []string
	if nodepool == "" {
		nodes, err = resolveAllClusterNodes(ctx, clientset)
	} else {
		nodes, err = resolveNodePoolNodes(ctx, clientset, nodepool)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(nodes) == 0 {
		c.JSON(http.StatusOK, ConntrackResponse{
			NodePool: nodepool, Cluster: cluster,
			Nodes: []ConntrackNodeStats{}, FetchedAt: time.Now(),
		})
		return
	}

	// Coletar stats em paralelo
	results := make([]ConntrackNodeStats, len(nodes))
	var wg sync.WaitGroup
	for i, nodeName := range nodes {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = probeConntrack(ctx, clientset, restConfig, name)
		}(i, nodeName)
	}
	wg.Wait()

	c.JSON(http.StatusOK, ConntrackResponse{
		NodePool: nodepool, Cluster: cluster,
		Nodes: results, FetchedAt: time.Now(),
	})
}

// resolveNodePoolNodes retorna os nomes dos nós pertencentes ao node pool.
//
// Bug real corrigido (achado ao vivo investigando um relato de "nomes de node na lista de pods
// que não batem com a listagem de node groups" — mesma classe de bug já corrigida em
// nodePoolLabel/nodepools_snat.go, mas nunca replicada aqui): os dois seletores tentados eram
// EXCLUSIVAMENTE do AKS (kubernetes.azure.com/agentpool, agentpool) — nenhum dos dois existe em
// nó EKS/GKE. O fallback por substring no NOME do nó também nunca batia pra EKS, já que os nomes
// seguem a convenção `ip-10-0-x-y.ec2.internal` (não contêm o nome do node group). Resultado
// confirmado ao vivo contra um cluster EKS real: /nodepools/conntrack sempre devolvia
// `"nodes": []` pra QUALQUER node pool, sem erro nenhum — silenciosamente vazio, indistinguível
// de "conntrack indisponível". Corrigido reaproveitando nodePoolLabel (nodepools_snat.go, já
// cobre AKS/EKS/GKE) pra resolver a QUAL pool cada nó pertence, em vez de tentar adivinhar via
// seletor de label fixo por cloud.
func resolveNodePoolNodes(ctx context.Context, clientset kubernetes.Interface, nodepool string) ([]string, error) {
	all, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar nós: %v", err)
	}

	var names []string
	for _, n := range all.Items {
		if nodePoolLabel(n.Labels) == nodepool {
			names = append(names, n.Name)
		}
	}
	if len(names) > 0 {
		return names, nil
	}

	// Fallback: filtrar por substring no nome do nó — cobre clusters onde nenhum label
	// conhecido bate (nodePoolLabel caiu no "default").
	for _, n := range all.Items {
		if strings.Contains(strings.ToLower(n.Name), strings.ToLower(nodepool)) {
			names = append(names, n.Name)
		}
	}
	return names, nil
}

// resolveAllClusterNodes retorna os nomes de todos os nós do cluster, sem filtro de pool.
func resolveAllClusterNodes(ctx context.Context, clientset kubernetes.Interface) ([]string, error) {
	all, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar nós: %v", err)
	}
	names := make([]string, len(all.Items))
	for i, n := range all.Items {
		names[i] = n.Name
	}
	return names, nil
}

// probeConntrack lê as métricas de conntrack de um nó via exec em pod com hostNetwork:true.
// Lê /proc/sys/net/netfilter/nf_conntrack_* — qualquer pod hostNetwork tem acesso
// ao conntrack do host sem necessidade de container privilegiado.
//
// Bug real corrigido (achado ao vivo contra um cluster EKS real, mesma investigação que revelou
// o bug de resolveNodePoolNodes acima): imagens EKS modernas (kube-proxy v1.34, aws-node,
// aws-eks-nodeagent, eks-pod-identity-agent — testado empiricamente contra
// 602401143452.dkr.ecr.us-east-1.amazonaws.com/eks/kube-proxy:v1.34.3-eksbuild.5) não têm `sh`
// nem `cat` — são imagens mínimas, sem nenhum utilitário de shell. O exec sempre falhava com
// "executable file not found in $PATH", mas só DEPOIS de já ter escolhido esse pod como único
// candidato — sem chance de tentar outro. Corrigido: probeConntrack agora tenta TODOS os
// candidatos hostNetwork do nó em ordem de preferência (findHostNetworkPodCandidates), não só o
// primeiro — segue pro próximo assim que um exec falha, em vez de desistir. Confirmado ao vivo
// que isso resolve: o daemonset do CrowdStrike Falcon (falcon-node-sensor, hostNetwork:true,
// rodando em namespace PRÓPRIO — não kube-system) tem shell completo e serve como fallback real
// nesse cluster. Não é garantido existir em todo cluster (é uma ferramenta de terceiro), mas o
// mecanismo de múltiplos candidatos + busca em TODOS os namespaces (não só kube-system) generaliza
// bem pra qualquer agente hostNetwork com shell que o cluster tiver (Datadog, Dynatrace, etc.).
func probeConntrack(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, nodeName string) ConntrackNodeStats {
	stats := ConntrackNodeStats{NodeName: nodeName, Status: "error", Drop: -1, EarlyDrop: -1, InsertFailed: -1, UptimeSeconds: -1}

	candidates, err := findHostNetworkPodCandidates(ctx, clientset, nodeName)
	if err != nil || len(candidates) == 0 {
		if err == nil {
			err = fmt.Errorf("nenhum pod hostNetwork Running encontrado no nó")
		}
		stats.Error = fmt.Sprintf("nenhum pod hostNetwork encontrado no nó: %v", err)
		return stats
	}

	// Ler os quatro sysctls, os contadores de descarte e o uptime em um único exec.
	// /proc/net é por network namespace — num pod hostNetwork é o do host.
	cmd := []string{
		"sh", "-c",
		"printf '%s\\n%s\\n%s\\n%s\\n' " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_count 2>/dev/null || echo -1) " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_max 2>/dev/null || echo -1) " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_buckets 2>/dev/null || echo -1) " +
			"$(cat /proc/sys/vm/max_map_count 2>/dev/null || echo -1); " +
			"echo " + conntrackStatMarker + "; " +
			"if [ -r /proc/net/stat/nf_conntrack ]; then echo procfs; cat /proc/net/stat/nf_conntrack; " +
			"elif command -v conntrack >/dev/null 2>&1; then echo conntrack-S; conntrack -S 2>&1; " +
			"else echo none; fi; " +
			"echo " + conntrackUptimeMarker + "; cat /proc/uptime 2>/dev/null",
	}

	var lastErr error
	var triedPods []string
	for _, cand := range candidates {
		output, execErr := execCmdInPod(ctx, clientset, restConfig, cand.Namespace, cand.PodName, cand.ContainerName, cmd)
		triedPods = append(triedPods, fmt.Sprintf("%s/%s", cand.Namespace, cand.PodName))
		if execErr != nil {
			lastErr = execErr
			continue
		}

		sysctlPart, statPart, uptimePart := splitConntrackProbeOutput(output)
		lines := strings.Split(strings.TrimSpace(sysctlPart), "\n")
		if len(lines) < 2 {
			lastErr = fmt.Errorf("resposta inesperada: %q", output)
			continue
		}

		stats.ProbeMethod = fmt.Sprintf("exec:%s/%s", cand.Namespace, cand.PodName)
		stats.Count = parseInt64(lines[0])
		stats.Max = parseInt64(lines[1])
		if len(lines) >= 3 {
			stats.Buckets = parseInt64(lines[2])
		}
		stats.MaxMapCount = -1
		if len(lines) >= 4 {
			stats.MaxMapCount = parseInt64(lines[3])
		}
		dc := parseDropCounters(statPart)
		stats.Drop, stats.EarlyDrop, stats.InsertFailed = dc.Drop, dc.EarlyDrop, dc.InsertFailed
		stats.DropPerCPU, stats.DropSource, stats.DropError = dc.PerCPU, dc.Source, dc.Err
		stats.UptimeSeconds = parseUptimeSeconds(uptimePart)
		if stats.Max > 0 {
			stats.UsagePct = float64(stats.Count) / float64(stats.Max) * 100
		}
		switch {
		case stats.UsagePct >= 90:
			stats.Status = "critical"
		case stats.UsagePct >= 70:
			stats.Status = "warning"
		default:
			stats.Status = "ok"
		}
		return stats
	}

	stats.Error = fmt.Sprintf("exec falhou em todos os %d candidato(s) hostNetwork (%s): %v", len(triedPods), strings.Join(triedPods, ", "), lastErr)
	return stats
}

const (
	conntrackStatMarker   = "--NFSTAT--"
	conntrackUptimeMarker = "--UPTIME--"
)

// splitConntrackProbeOutput separa a saída do exec do probeConntrack em: sysctls (1 por
// linha), conteúdo de /proc/net/stat/nf_conntrack e conteúdo de /proc/uptime.
func splitConntrackProbeOutput(output string) (sysctls, stat, uptime string) {
	sysctls, rest, _ := strings.Cut(output, conntrackStatMarker)
	stat, uptime, _ = strings.Cut(rest, conntrackUptimeMarker)
	return sysctls, stat, uptime
}

// ConntrackCPUDrops são os contadores de descarte de uma CPU (-1 = coluna ausente nesta fonte/kernel).
type ConntrackCPUDrops struct {
	CPU          int   `json:"cpu"`
	Drop         int64 `json:"drop"`
	EarlyDrop    int64 `json:"early_drop"`
	InsertFailed int64 `json:"insert_failed"`
}

// dropCounters é o resultado da leitura dos contadores de descarte de um nó.
type dropCounters struct {
	Drop, EarlyDrop, InsertFailed int64 // somados entre as CPUs (-1 = não lido)
	PerCPU                        []ConntrackCPUDrops
	Source, Err                   string
}

// parseDropCounters interpreta a seção de contadores do exec: a 1ª linha diz a fonte
// ("procfs", "conntrack-S" ou "none") e o resto é a saída dela.
func parseDropCounters(section string) dropCounters {
	res := dropCounters{Drop: -1, EarlyDrop: -1, InsertFailed: -1}
	head, body, _ := strings.Cut(strings.TrimSpace(section), "\n")
	switch strings.TrimSpace(head) {
	case "procfs":
		res.Source = "procfs"
		res.PerCPU = parseNfConntrackStat(body)
	case "conntrack-S":
		res.Source = "conntrack -S"
		res.PerCPU = parseConntrackS(body)
	case "none":
		res.Err = "kernel sem /proc/net/stat/nf_conntrack e pod sem o binário conntrack"
		return res
	default:
		res.Err = "leitura dos contadores não executada no nó"
		return res
	}
	res.Drop, res.EarlyDrop, res.InsertFailed = sumCPUDrops(res.PerCPU)
	if res.Drop < 0 && res.EarlyDrop < 0 && res.InsertFailed < 0 {
		msg := strings.TrimSpace(body)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		res.Err = fmt.Sprintf("%s sem os contadores esperados: %s", res.Source, msg)
		res.PerCPU = nil
	}
	return res
}

// sumCPUDrops soma cada contador entre as CPUs; contador ausente em todas → -1.
func sumCPUDrops(rows []ConntrackCPUDrops) (drop, earlyDrop, insertFailed int64) {
	drop, earlyDrop, insertFailed = -1, -1, -1
	add := func(total *int64, v int64) {
		if v < 0 {
			return
		}
		if *total < 0 {
			*total = 0
		}
		*total += v
	}
	for _, r := range rows {
		add(&drop, r.Drop)
		add(&earlyDrop, r.EarlyDrop)
		add(&insertFailed, r.InsertFailed)
	}
	return
}

// parseConntrackS lê a saída de `conntrack -S`: uma linha por CPU no formato
// "cpu=0 found=986 ... insert_failed=4 drop=4 early_drop=939985 ...". Linhas sem "cpu="
// (ex: "Operation not permitted" sem CAP_NET_ADMIN) são ignoradas.
func parseConntrackS(text string) []ConntrackCPUDrops {
	var rows []ConntrackCPUDrops
	for _, line := range strings.Split(text, "\n") {
		row := ConntrackCPUDrops{CPU: -1, Drop: -1, EarlyDrop: -1, InsertFailed: -1}
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "cpu":
				row.CPU = int(n)
			case "drop":
				row.Drop = n
			case "early_drop":
				row.EarlyDrop = n
			case "insert_failed":
				row.InsertFailed = n
			}
		}
		if row.CPU >= 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// parseNfConntrackStat lê /proc/net/stat/nf_conntrack: a 1ª linha traz os nomes das colunas
// (variam entre versões de kernel, por isso a busca é pelo nome); as demais são uma por CPU,
// na ordem, com valores em hexa.
func parseNfConntrackStat(text string) []ConntrackCPUDrops {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 2 {
		return nil
	}
	cols := map[string]int{}
	for i, name := range strings.Fields(lines[0]) {
		cols[name] = i
	}
	var rows []ConntrackCPUDrops
	for cpu, l := range lines[1:] {
		f := strings.Fields(l)
		get := func(name string) int64 {
			idx, ok := cols[name]
			if !ok || idx >= len(f) {
				return -1
			}
			v, err := strconv.ParseInt(f[idx], 16, 64)
			if err != nil {
				return -1
			}
			return v
		}
		rows = append(rows, ConntrackCPUDrops{CPU: cpu, Drop: get("drop"), EarlyDrop: get("early_drop"), InsertFailed: get("insert_failed")})
	}
	return rows
}

// parseUptimeSeconds lê o 1º campo de /proc/uptime ("12345.67 54321.00"). -1 se não lido.
func parseUptimeSeconds(text string) int64 {
	f := strings.Fields(text)
	if len(f) == 0 {
		return -1
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return -1
	}
	return int64(v)
}

// hostNetworkPodCandidate identifica um pod hostNetwork:true Running num nó, candidato a exec
// pra leitura dos sysctls de conntrack.
type hostNetworkPodCandidate struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// hostNetworkKnownLabels — candidatos ordenados por preferência (conhecidos primeiro). kube-proxy
// é o mais comum a todos os providers; os demais cobrem CNIs que também rodam hostNetwork:true
// (azure-npm no AKS, aws-node — o daemonset do VPC CNI da AWS — no EKS; anetd/netd no GKE
// Dataplane V2, que substitui kube-proxy por um agente Cilium). Só usados pra ORDENAR — qualquer
// outro pod hostNetwork encontrado (ver findHostNetworkPodCandidates) entra depois como fallback,
// nunca é descartado.
var hostNetworkKnownLabels = []string{
	"k8s-app=kube-proxy",
	"component=kube-proxy",
	"k8s-app=aws-node",
	"k8s-app=azure-npm",
	"app=azure-cni-networkmonitor",
	"k8s-app=cilium",
	"k8s-app=netd",
	"k8s-app=gke-metadata-server",
}

// findHostNetworkPodCandidates localiza TODOS os pods hostNetwork:true Running no nó alvo, em
// QUALQUER namespace (não só kube-system — bug real corrigido, ver comentário em probeConntrack:
// agentes de terceiro com shell, como o CrowdStrike Falcon, costumam rodar no próprio namespace),
// ordenados por preferência: primeiro os candidatos "conhecidos" (hostNetworkKnownLabels, na
// ordem declarada), depois qualquer outro pod hostNetwork como fallback genérico.
func findHostNetworkPodCandidates(ctx context.Context, clientset kubernetes.Interface, nodeName string) ([]hostNetworkPodCandidate, error) {
	all, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar pods: %v", err)
	}

	knownRank := make(map[string]int, len(hostNetworkKnownLabels))
	for i, label := range hostNetworkKnownLabels {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) == 2 {
			knownRank[parts[0]+"="+parts[1]] = i
		}
	}
	matchesKnown := func(labels map[string]string) (int, bool) {
		for _, label := range hostNetworkKnownLabels {
			parts := strings.SplitN(label, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if labels[parts[0]] == parts[1] {
				return knownRank[label], true
			}
		}
		return 0, false
	}

	type ranked struct {
		cand hostNetworkPodCandidate
		rank int
	}
	var known []ranked
	var others []hostNetworkPodCandidate
	for _, p := range all.Items {
		if !p.Spec.HostNetwork || len(p.Spec.Containers) == 0 {
			continue
		}
		cand := hostNetworkPodCandidate{Namespace: p.Namespace, PodName: p.Name, ContainerName: p.Spec.Containers[0].Name}
		if rank, ok := matchesKnown(p.Labels); ok {
			known = append(known, ranked{cand, rank})
		} else {
			others = append(others, cand)
		}
	}
	sort.Slice(known, func(i, j int) bool { return known[i].rank < known[j].rank })

	result := make([]hostNetworkPodCandidate, 0, len(known)+len(others))
	for _, k := range known {
		result = append(result, k.cand)
	}
	result = append(result, others...)
	return result, nil
}

// execCmdInPod executa um comando em um pod via SPDY e retorna o stdout
func execCmdInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName string, cmd []string) (string, error) {

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("SPDY executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		// stdout.String() é devolvido mesmo no erro — os scripts dos test tools (Kafka/DB/Latência)
		// redirecionam stderr do cliente pra dentro do próprio stdout (`... 2>&1`), então a mensagem
		// de erro real (ex: "connection refused" do psql) está em stdout, não no stream de stderr
		// separado do exec (que fica vazio nesse caso). Devolver "" aqui descartava esse texto e
		// deixava quem chama sem nenhuma saída bruta pra mostrar ao usuário.
		return stdout.String(), fmt.Errorf("stream: %v (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

// execCmdInPodStreaming é a variante de execCmdInPod que escreve o stdout do exec DIRETO num
// io.Writer fornecido pelo chamador, em vez de bufferizar tudo numa bytes.Buffer só devolvida no
// final — usado pela Descoberta de Rede (net_discovery.go) pra processar cada linha do traceroute
// conforme ela chega (ver streamCommandLines lá), viabilizando o grafo do frontend se desenhar em
// tempo real em vez de só aparecer inteiro quando o comando termina. stderr continua bufferizado
// à parte (comandos desta ferramenta não precisam dele linha a linha, só como texto de erro caso
// o exec falhe).
func execCmdInPodStreaming(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName string, cmd []string, stdout io.Writer) error {

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("SPDY executor: %v", err)
	}

	var stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return fmt.Errorf("stream: %v (stderr: %s)", err, stderr.String())
	}
	return nil
}

// execCmdInPodWithStdin é a variante de execCmdInPod que também alimenta o STDIN do comando a
// partir de um io.Reader — usado pela Descoberta de Rede (net_discovery_fingerprint.go) pra
// entregar um certificado de cliente (mTLS) pro script dentro do pod sem que o conteúdo (e
// principalmente a CHAVE PRIVADA) apareça em nenhum argv/cmdline visível via `ps`/`/proc` dentro
// do próprio container — só o comando POSICIONAL "MTLS=1/0" (não sensível) vai como argumento;
// o par cert+chave viaja só pelo stream de stdin do exec, lido uma única vez pelo script via
// `awk` e gravado em arquivos temporários apagados no fim do próprio script. `stdin` pode ser um
// `strings.NewReader("")` (sem cert) sem custo real — o exec recebe EOF imediato e o script,
// nesse caso, nem chega a tentar ler stdin (ver netDiscoveryFingerprintScript). Reaproveita 100%
// o resto da mecânica de execCmdInPod — não duplicado por completo, só a diferença de Stdin.
func execCmdInPodWithStdin(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName string, cmd []string, stdin io.Reader) (string, error) {

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("SPDY executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stdout.String(), fmt.Errorf("stream: %v (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// ─── Conntrack History ────────────────────────────────────────────────────────

// ConntrackHistoryPoint representa um ponto no tempo da série histórica de conntrack
type ConntrackHistoryPoint struct {
	Timestamp int64   `json:"ts"`        // Unix timestamp (segundos)
	Count     float64 `json:"count"`     // nf_conntrack_entries naquele instante
	Max       float64 `json:"max"`       // nf_conntrack_entries_limit (pode ser 0 se não disponível)
	UsagePct  float64 `json:"usage_pct"` // count/max*100 (0 se max=0)
}

// ConntrackNodeHistoryResponse resposta do endpoint de histórico por nó
type ConntrackNodeHistoryResponse struct {
	NodeName            string                  `json:"node_name"`
	Hours               int                     `json:"hours"`
	StepMinutes         int                     `json:"step_minutes"`
	OffsetDays          int                     `json:"offset_days"`
	Points              []ConntrackHistoryPoint `json:"points"`
	PrometheusAvailable bool                    `json:"prometheus_available"`
	Error               string                  `json:"error,omitempty"`
}

// GetConntrackNodeHistory retorna série histórica de conntrack de um nó via Prometheus.
// GET /api/v1/nodepools/conntrack/history?cluster=X&node=Y&hours=6&step=5&offset_days=0
// offset_days desloca a janela inteira N dias para trás (mesmo horário do dia), permitindo
// comparar o mesmo período de hoje com D-1/D-2/D-3.
// Se Prometheus não estiver disponível, retorna prometheus_available=false sem dados.
func (h *NodePoolHandler) GetConntrackNodeHistory(c *gin.Context) {
	cluster := c.Query("cluster")
	nodeName := c.Query("node")
	if cluster == "" || nodeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros cluster e node são obrigatórios"})
		return
	}

	hours := 6
	if v := c.Query("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
			hours = n
		}
	}
	stepMinutes := 5
	if v := c.Query("step"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 60 {
			stepMinutes = n
		}
	}
	offsetDays := 0
	if v := c.Query("offset_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 7 {
			offsetDays = n
		}
	}

	resp := ConntrackNodeHistoryResponse{
		NodeName:    nodeName,
		Hours:       hours,
		StepMinutes: stepMinutes,
		OffsetDays:  offsetDays,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Obter IP interno do nó via K8s API
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		resp.Error = fmt.Sprintf("erro ao conectar ao cluster: %v", err)
		c.JSON(http.StatusOK, resp)
		return
	}

	nodeIP, err := getNodeInternalIP(ctx, clientset, nodeName)
	if err != nil {
		resp.Error = fmt.Sprintf("nó não encontrado: %v", err)
		c.JSON(http.StatusOK, resp)
		return
	}

	// Reutiliza cliente cacheado por cluster (evita redescoberta do endpoint a cada request)
	prom, promErr := h.getPromClient(cluster)
	if promErr != nil {
		resp.PrometheusAvailable = false
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.PrometheusAvailable = true

	// Instance no formato que o node_exporter registra: <IP>:9100
	// Dots escapados para PromQL regex: "10.0.0.1:9100" → "10\\.0\\.0\\.1:9100"
	instanceLabel := strings.ReplaceAll(nodeIP+":9100", ".", `\\.`)

	end := time.Now().Add(-time.Duration(offsetDays) * 24 * time.Hour)
	start := end.Add(-time.Duration(hours) * time.Hour)
	step := time.Duration(stepMinutes) * time.Minute

	entriesQuery := fmt.Sprintf(`node_nf_conntrack_entries{instance=~"%s"}`, instanceLabel)
	limitQuery := fmt.Sprintf(`node_nf_conntrack_entries_limit{instance=~"%s"}`, instanceLabel)

	entriesResult, err := prom.QueryRange(ctx, entriesQuery, start, end, step)
	if err != nil {
		resp.Error = fmt.Sprintf("erro ao consultar Prometheus (entries): %v", err)
		c.JSON(http.StatusOK, resp)
		return
	}

	// Mapa timestamp → limit (melhor esforço)
	limitByTS := map[int64]float64{}
	if limitResult, limitErr := prom.QueryRange(ctx, limitQuery, start, end, step); limitErr == nil {
		for _, series := range limitResult.Data.Result {
			for _, pair := range series.Values {
				ts, val, ok := parseRangePair(pair)
				if ok {
					limitByTS[ts] = val
				}
			}
		}
	}

	if len(entriesResult.Data.Result) == 0 {
		resp.Error = "sem dados de conntrack no Prometheus para este nó (node_exporter instalado?)"
		c.JSON(http.StatusOK, resp)
		return
	}

	points := make([]ConntrackHistoryPoint, 0)
	for _, pair := range entriesResult.Data.Result[0].Values {
		ts, count, ok := parseRangePair(pair)
		if !ok {
			continue
		}
		max := limitByTS[ts]
		var usagePct float64
		if max > 0 {
			usagePct = count / max * 100
		}
		points = append(points, ConntrackHistoryPoint{
			Timestamp: ts,
			Count:     count,
			Max:       max,
			UsagePct:  usagePct,
		})
	}
	resp.Points = points

	c.JSON(http.StatusOK, resp)
}

// parseRangePair extrai (timestamp, value) de um elemento []interface{} do Prometheus query_range.
// Formato: [unix_float, "value_string"]
func parseRangePair(pair interface{}) (ts int64, val float64, ok bool) {
	arr, isArr := pair.([]interface{})
	if !isArr || len(arr) < 2 {
		return 0, 0, false
	}
	tsFloat, tsOK := arr[0].(float64)
	valStr, valOK := arr[1].(string)
	if !tsOK || !valOK {
		return 0, 0, false
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, 0, false
	}
	return int64(tsFloat), v, true
}

// getNodeInternalIP retorna o IP interno (InternalIP) de um nó pelo nome
func getNodeInternalIP(ctx context.Context, clientset kubernetes.Interface, nodeName string) (string, error) {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP && addr.Address != "" {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("IP interno não encontrado para o nó %s", nodeName)
}
