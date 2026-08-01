package discovery

import (
	"context"
	"fmt"
)

// PortForwardTarget descreve um Service ClusterIP dentro de um cluster a alcançar via túnel —
// usado quando o Prometheus do cluster não tem URL externa direta (nem override manual de URL,
// nem GMP funcional), mas tem uma instalação in-cluster (ex: kube-prometheus-stack completo) só
// acessível internamente. Caso real que motivou isso: cluster GKE com GMP habilitado mas sem
// PodMonitoring completo pro kube-state-metrics real (métricas de HPA/resources ausentes no GMP),
// enquanto o Prometheus in-cluster já tem tudo via seus próprios ServiceMonitors — só falta
// alcançá-lo.
type PortForwardTarget struct {
	Namespace string
	Service   string
	Port      int
}

// isZero indica que nenhum override de port-forward foi configurado.
func (t PortForwardTarget) isZero() bool {
	return t.Namespace == "" || t.Service == "" || t.Port == 0
}

// portForwardFunc, quando definido, abre (ou reusa um já aberto — lifecycle/cache fica por conta
// da implementação) um túnel para namespace/service/port de um cluster, retornando a URL local
// (http://127.0.0.1:<porta>/) pronta pra uso.
//
// Não podemos importar internal/config aqui: esse pacote depende transitivamente deste
// (internal/cloudprovider/gcp → internal/ai → internal/collectors → internal/monitoring/client →
// internal/monitoring/discovery) — importar na direção contrária fecharia um import cycle, mesmo
// motivo de gcp_auth.go. Ligado em internal/config.KubeConfigManager.DiscoverClusters via
// SetPortForwardFunc, mesmo padrão de SetGCPTokenFunc.
var portForwardFunc func(ctx context.Context, cluster string, target PortForwardTarget) (string, error)

// SetPortForwardFunc registra a função usada para abrir túneis de port-forward.
func SetPortForwardFunc(fn func(ctx context.Context, cluster string, target PortForwardTarget) (string, error)) {
	portForwardFunc = fn
}

// openPortForward abre o túnel para o target e retorna a URL local, ou erro se nenhum
// portForwardFunc foi registrado (nunca panica).
func openPortForward(ctx context.Context, cluster string, target PortForwardTarget) (string, error) {
	if portForwardFunc == nil {
		return "", fmt.Errorf("port-forward não disponível: hook não registrado (DiscoverClusters ainda não rodou?)")
	}
	return portForwardFunc(ctx, cluster, target)
}
