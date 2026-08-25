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
}

// crossReferenceHops enriquece cada hop com InternalRef quando aplicável — chamado uma vez ao
// final de runDiscovery, depois de enrichHops (Fase 3), mesmo padrão de camada independente/
// best-effort desta ferramenta. `clientset` nil (modo local) faz cada IP só consultar o cache já
// persistido — ver crossReferenceIP.
func (h *NetDiscoveryHandler) crossReferenceHops(ctx context.Context, hops []NetDiscoveryHop, clientset kubernetes.Interface, cluster string) {
	for i := range hops {
		if hops[i].IP == "" {
			continue
		}
		hops[i].InternalRef = h.crossReferenceIP(ctx, hops[i].IP, clientset, cluster)
	}
}

// crossReferenceIP resolve um IP contra o cache/frota K8s — sempre best-effort, nunca retorna
// erro (mesmo espírito das Fases 2/3: uma falha aqui nunca esconde o resultado principal). `nil`
// clientset (modo local) faz esta função consultar SÓ o cache persistido, nunca uma busca live.
func (h *NetDiscoveryHandler) crossReferenceIP(ctx context.Context, ip string, clientset kubernetes.Interface, cluster string) *NetDiscoveryInternalRef {
	if h.registry == nil {
		return nil
	}

	// Gate: só IPs privados (RFC1918) são sequer candidatos — Node/Pod/Service IPs desta app
	// são sempre privados na prática (nenhum cluster gerenciado expõe IP público diretamente
	// nesses objetos); IPs públicos nunca vão bater aqui, então nem vale consultar cache/K8s
	// pra eles — economia real, não só teórica, já que a maioria dos saltos de um traceroute
	// público é justamente IP público.
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || !parsedIP.IsPrivate() {
		return nil
	}

	if cached, err := h.registry.Get(ip); err == nil {
		ttl := netDiscoveryNodeServiceCacheTTL
		if cached.Kind == "pod" {
			ttl = netDiscoveryPodCacheTTL
		}
		if time.Since(cached.CachedAt) < ttl {
			return &NetDiscoveryInternalRef{Kind: cached.Kind, Name: cached.Name, Namespace: cached.Namespace, Cluster: cached.Cluster}
		}
	}

	if clientset == nil {
		return nil // modo local — sem cluster-alvo natural, não dispara busca live (ver comentário do arquivo)
	}

	ref := liveLookupIPInCluster(ctx, clientset, cluster, ip)
	if ref != nil {
		_ = h.registry.Upsert(storage.NetDiscoveryIPCacheEntry{
			IP: ip, Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Cluster: ref.Cluster, CachedAt: time.Now(),
		})
	}
	return ref
}

// liveLookupIPInCluster varre Nodes → Services → Pods (nesta ordem — nodes é a lista mais barata
// e resolve o caso mais comum de traceroute, "esse hop é um node do cluster") procurando um match
// exato de IP. Cada List() é UMA chamada por kind (não N chamadas), mesmo padrão de custo já
// usado no resto desta app (ex: ListDeployments faz 1 List de Pods por namespace, não por
// deployment). Retorna no primeiro match — um IP não deveria bater em mais de um kind ao mesmo
// tempo num cluster saudável.
func liveLookupIPInCluster(ctx context.Context, clientset kubernetes.Interface, cluster, ip string) *NetDiscoveryInternalRef {
	lookupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if nodes, err := clientset.CoreV1().Nodes().List(lookupCtx, metav1.ListOptions{}); err == nil {
		for _, n := range nodes.Items {
			for _, addr := range n.Status.Addresses {
				if (addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP) && addr.Address == ip {
					return &NetDiscoveryInternalRef{Kind: "node", Name: n.Name, Cluster: cluster}
				}
			}
		}
	}

	if svcs, err := clientset.CoreV1().Services("").List(lookupCtx, metav1.ListOptions{}); err == nil {
		for _, s := range svcs.Items {
			if s.Spec.ClusterIP == ip {
				return &NetDiscoveryInternalRef{Kind: "service", Name: s.Name, Namespace: s.Namespace, Cluster: cluster}
			}
			for _, ing := range s.Status.LoadBalancer.Ingress {
				if ing.IP == ip {
					return &NetDiscoveryInternalRef{Kind: "service", Name: s.Name, Namespace: s.Namespace, Cluster: cluster}
				}
			}
		}
	}

	if pods, err := clientset.CoreV1().Pods("").List(lookupCtx, metav1.ListOptions{}); err == nil {
		for _, p := range pods.Items {
			if p.Status.PodIP == ip || p.Status.HostIP == ip {
				// resolveOwnerDisplayName (configmaps_usage.go) já resolve até o workload dono
				// (Deployment/DaemonSet/StatefulSet/Job) — mesmo mecanismo já usado no badge de
				// uso de ConfigMaps, reaproveitado sem duplicar lógica.
				return &NetDiscoveryInternalRef{Kind: "pod", Name: resolveOwnerDisplayName(&p), PodName: p.Name, Namespace: p.Namespace, Cluster: cluster}
			}
		}
	}

	return nil
}
