package handlers

import (
	"context"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/storage"
)

// ─── "Descoberta de Rede" — Fase 4 (IP-ROUTE-DISCOVERY-PLAN.md, seção 3.8): cross-reference
// contra a frota K8s desta app — ENRIQUECIMENTO BÔNUS, nunca pré-requisito (ver seção 3.2/3.3/3.4
// pra identificação de SO/serviço, que funciona sozinha independente desta camada). Acende só
// quando o IP por acaso é um node/pod/service de um cluster do kubeconfig.
//
// Cache-on-read persistido (decisão delegada ao Claude Code pelo usuário) — nunca um scan de
// fundo dedicado: a tabela se popula sozinha de leitura em leitura.
//
// Escopo desta rodada, deliberado: só roda no MODO POD, onde já existe um `clientset` real e um
// cluster explicitamente escolhido pelo usuário. No modo local (o default desta ferramenta —
// ver seção 4.1 do plano) não há nenhum "cluster alvo" natural na requisição — inventar uma
// iteração sobre TODOS os clusters do kubeconfig a cada consulta seria exatamente o "scan
// completo da frota a cada consulta" que a decisão de cache-on-read foi pensada pra evitar. No
// modo local, esta camada só CONSULTA o cache já persistido (por descobertas anteriores em modo
// pod) — nunca dispara uma busca live nova. Ver "Limitação conhecida" no CLAUDE.md.

// netDiscoveryPodCacheTTL/netDiscoveryNodeServiceCacheTTL — TTL diferenciado por Kind (seção 3.8
// do plano): IP de Pod é efêmero (reagendamento/restart reusa o mesmo IP em minutos/horas) —
// TTL curto evita um falso positivo perigoso (afirmar "é o Pod X" quando já é outro pod há
// horas). IP de Node/Service é muito mais estável — TTL bem mais longo, mesmo valor já usado
// pelo Node Pool Registry/pricers desta app.
const (
	netDiscoveryPodCacheTTL         = 2 * time.Hour
	netDiscoveryNodeServiceCacheTTL = 24 * time.Hour
)

// NetDiscoveryInternalRef é o resultado de um cross-reference bem-sucedido — exposto no JSON de
// cada NetDiscoveryHop quando aplicável.
type NetDiscoveryInternalRef struct {
	Kind      string `json:"kind"`               // "node" | "pod" | "service"
	Name      string `json:"name"`               // já formatado pra exibição (ex: "Deployment/checkout-api" quando Kind=pod)
	PodName   string `json:"pod_name,omitempty"` // só quando Kind=pod — nome literal do Pod object (Name já é o owner)
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster"`
	// FromCache/MatchedAt — transparência de frescor, pedido explícito do usuário depois de
	// apontar um risco real: um match vindo do cache (até 24h de idade pra Node/Service) pode
	// estar DESATUALIZADO — o IP pode ter mudado de dono desde então — e apresentar isso sem
	// indicar a idade faria a ferramenta parecer estar reportando o cenário atual quando na
	// verdade é uma foto antiga. `FromCache=false` só quando o match veio de uma busca AO VIVO
	// nesta própria execução (sempre o caso no modo pod, quando o registry ainda não tinha nada
	// pra esse IP ou o registro estava vencido); `MatchedAt` é sempre preenchido (agora, pro caso
	// live; o `CachedAt` original, pro caso cache) — mesmo princípio de transparência já usado em
	// `RegistryStale`/`LatestKnownExecutionAt` (Spinnaker) e `ProbedHost` (Fase 3 desta própria
	// ferramenta): nunca esconder a idade de um dado que pode ter mudado.
	FromCache bool      `json:"from_cache"`
	MatchedAt time.Time `json:"matched_at"`
}

// crossReferenceHops enriquece cada hop com InternalRef quando aplicável — chamado uma vez ao
// final de runDiscovery, depois de enrichHops (Fase 3), mesmo padrão de camada independente/
// best-effort desta ferramenta. `clientset` nil (modo local) faz cada IP só consultar o cache já
// persistido — ver crossReferenceIP.
//
// Achado real de code review: a versão anterior chamava crossReferenceIP por salto, e cada
// cache-miss disparava 3 List() (Nodes/Services/Pods) do zero dentro de liveLookupIPInCluster —
// numa rota modo pod com N saltos privados/cache-frio (plausível quando o caminho fica dentro da
// rede de pods/services de um cluster), isso listava a MESMA frota 3×N vezes sequencialmente
// contra o kube-apiserver, contrariando o próprio princípio de custo já documentado no resto desta
// app (ex: ListDeployments faz 1 List de Pods por namespace, não por deployment). Corrigido com
// uma varredura em 2 passadas: (1) resolve o que der só via cache (nunca toca rede); (2) se sobrou
// pelo menos 1 salto pendente E há clientset (modo pod), busca a frota (fetchK8sFleet) UMA ÚNICA
// vez pra TODA a descoberta e casa cada salto pendente contra esse snapshot em memória — mesmo
// padrão de custo já usado por enrichHops (Fase 3, faixas de nuvem calculadas uma vez, não por
// salto).
func (h *NetDiscoveryHandler) crossReferenceHops(ctx context.Context, hops []NetDiscoveryHop, clientset kubernetes.Interface, cluster string) {
	if h.registry == nil {
		return
	}

	var pending []int
	for i := range hops {
		if hops[i].IP == "" || !isCrossRefCandidateIP(hops[i].IP) {
			continue
		}
		if ref := h.cacheLookup(hops[i].IP); ref != nil {
			hops[i].InternalRef = ref
			continue
		}
		if clientset != nil {
			pending = append(pending, i)
		}
	}

	if len(pending) == 0 {
		return // nada pendente, ou modo local (clientset nil — nunca dispara busca ao vivo)
	}

	fleet := fetchK8sFleet(ctx, clientset)
	now := time.Now()
	for _, i := range pending {
		ref := matchIPInFleet(fleet, cluster, hops[i].IP)
		if ref == nil {
			continue
		}
		ref.FromCache = false
		ref.MatchedAt = now
		hops[i].InternalRef = ref
		_ = h.registry.Upsert(storage.NetDiscoveryIPCacheEntry{
			IP: hops[i].IP, Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Cluster: ref.Cluster, CachedAt: now,
		})
	}
}

// isCrossRefCandidateIP — só IPs privados (RFC1918) são sequer candidatos a cross-reference:
// Node/Pod/Service IPs desta app são sempre privados na prática (nenhum cluster gerenciado expõe
// IP público diretamente nesses objetos); IPs públicos nunca vão bater aqui, então nem vale
// consultar cache/K8s pra eles — economia real, não só teórica, já que a maioria dos saltos de um
// traceroute público é justamente IP público.
func isCrossRefCandidateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.IsPrivate()
}

// cacheLookup consulta só o registry persistido (nunca dispara rede) — TTL diferenciado por Kind,
// compartilhado entre crossReferenceIP (single-IP) e crossReferenceHops (batch), pra nunca
// duplicar essa regra em dois lugares.
func (h *NetDiscoveryHandler) cacheLookup(ip string) *NetDiscoveryInternalRef {
	cached, err := h.registry.Get(ip)
	if err != nil {
		return nil
	}
	ttl := netDiscoveryNodeServiceCacheTTL
	if cached.Kind == "pod" {
		ttl = netDiscoveryPodCacheTTL
	}
	if time.Since(cached.CachedAt) >= ttl {
		return nil
	}
	return &NetDiscoveryInternalRef{
		Kind: cached.Kind, Name: cached.Name, Namespace: cached.Namespace, Cluster: cached.Cluster,
		FromCache: true, MatchedAt: cached.CachedAt,
	}
}

// crossReferenceIP resolve um IP contra o cache/frota K8s — sempre best-effort, nunca retorna
// erro (mesmo espírito das Fases 2/3: uma falha aqui nunca esconde o resultado principal). `nil`
// clientset (modo local) faz esta função consultar SÓ o cache persistido, nunca uma busca live.
// Entrada pública de UM IP só (usada por testes/eventuais chamadores futuros fora do fluxo de
// descoberta em lote) — crossReferenceHops NÃO chama esta função (ver comentário lá em cima sobre
// por que o caminho em lote busca a frota uma vez em vez de reusar este método por salto).
func (h *NetDiscoveryHandler) crossReferenceIP(ctx context.Context, ip string, clientset kubernetes.Interface, cluster string) *NetDiscoveryInternalRef {
	if h.registry == nil {
		return nil
	}
	if !isCrossRefCandidateIP(ip) {
		return nil
	}
	if ref := h.cacheLookup(ip); ref != nil {
		return ref
	}
	if clientset == nil {
		return nil // modo local — sem cluster-alvo natural, não dispara busca live (ver comentário do arquivo)
	}

	ref := matchIPInFleet(fetchK8sFleet(ctx, clientset), cluster, ip)
	if ref != nil {
		now := time.Now()
		ref.FromCache = false
		ref.MatchedAt = now
		_ = h.registry.Upsert(storage.NetDiscoveryIPCacheEntry{
			IP: ip, Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Cluster: ref.Cluster, CachedAt: now,
		})
	}
	return ref
}

// k8sFleetSnapshot — Nodes/Services/Pods buscados de uma vez, reaproveitados por todos os saltos
// que precisarem de busca ao vivo numa mesma descoberta (ver crossReferenceHops).
type k8sFleetSnapshot struct {
	nodes []corev1.Node
	svcs  []corev1.Service
	pods  []corev1.Pod
}

// fetchK8sFleet — UMA chamada List() por kind (Nodes/Services/Pods), nunca por salto. Best-effort
// por kind: uma falha isolada (ex: sem RBAC pra listar Pods) não derruba os outros dois — o
// snapshot resultante simplesmente não tem match nesse kind, mesmo espírito do resto da camada.
func fetchK8sFleet(ctx context.Context, clientset kubernetes.Interface) k8sFleetSnapshot {
	lookupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var snap k8sFleetSnapshot
	if nodes, err := clientset.CoreV1().Nodes().List(lookupCtx, metav1.ListOptions{}); err == nil {
		snap.nodes = nodes.Items
	}
	if svcs, err := clientset.CoreV1().Services("").List(lookupCtx, metav1.ListOptions{}); err == nil {
		snap.svcs = svcs.Items
	}
	if pods, err := clientset.CoreV1().Pods("").List(lookupCtx, metav1.ListOptions{}); err == nil {
		snap.pods = pods.Items
	}
	return snap
}

// matchIPInFleet varre um snapshot já buscado — Nodes → Services → Pods (nesta ordem: nodes é a
// lista mais barata e resolve o caso mais comum de traceroute, "esse hop é um node do cluster").
// Retorna no primeiro match — um IP não deveria bater em mais de um kind ao mesmo tempo num
// cluster saudável.
func matchIPInFleet(snap k8sFleetSnapshot, cluster, ip string) *NetDiscoveryInternalRef {
	for _, n := range snap.nodes {
		for _, addr := range n.Status.Addresses {
			if (addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP) && addr.Address == ip {
				return &NetDiscoveryInternalRef{Kind: "node", Name: n.Name, Cluster: cluster}
			}
		}
	}

	for _, s := range snap.svcs {
		if s.Spec.ClusterIP == ip {
			return &NetDiscoveryInternalRef{Kind: "service", Name: s.Name, Namespace: s.Namespace, Cluster: cluster}
		}
		for _, ing := range s.Status.LoadBalancer.Ingress {
			if ing.IP == ip {
				return &NetDiscoveryInternalRef{Kind: "service", Name: s.Name, Namespace: s.Namespace, Cluster: cluster}
			}
		}
	}

	for _, p := range snap.pods {
		if p.Status.PodIP == ip || p.Status.HostIP == ip {
			// resolveOwnerDisplayName (configmaps_usage.go) já resolve até o workload dono
			// (Deployment/DaemonSet/StatefulSet/Job) — mesmo mecanismo já usado no badge de
			// uso de ConfigMaps, reaproveitado sem duplicar lógica.
			return &NetDiscoveryInternalRef{Kind: "pod", Name: resolveOwnerDisplayName(&p), PodName: p.Name, Namespace: p.Namespace, Cluster: cluster}
		}
	}

	return nil
}
