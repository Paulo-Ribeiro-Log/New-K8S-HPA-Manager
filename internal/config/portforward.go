package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"k8s-hpa-manager/internal/monitoring/discovery"
)

// portForwardEntry representa um túnel já aberto e cacheado.
type portForwardEntry struct {
	localURL string
	stopCh   chan struct{}
	lastUsed time.Time
}

// Cache de túneis de port-forward por cluster+namespace+service+port. Evita abrir um túnel SPDY
// novo (handshake caro) a cada chamada — reusa o mesmo túnel enquanto ele continuar em uso, e um
// goroutine de limpeza fecha túneis ociosos, mesmo padrão de clientCache/restConfigs
// (clientTTL/clientCleanupInterval) já usado neste arquivo.
var (
	portForwardCache      = make(map[string]*portForwardEntry)
	portForwardCacheMu    sync.Mutex
	portForwardCleanupSet bool
)

const (
	portForwardIdleTTL      = 30 * time.Minute
	portForwardCleanupEvery = 10 * time.Minute
	portForwardReadyTimeout = 15 * time.Second
)

func portForwardCacheKey(cluster string, target discovery.PortForwardTarget) string {
	return cluster + "|" + target.Namespace + "|" + target.Service + "|" + strconv.Itoa(target.Port)
}

// ensurePortForwardCleanup inicia (uma única vez por processo) o goroutine que fecha túneis
// ociosos há mais de portForwardIdleTTL.
func ensurePortForwardCleanup() {
	portForwardCacheMu.Lock()
	defer portForwardCacheMu.Unlock()
	if portForwardCleanupSet {
		return
	}
	portForwardCleanupSet = true
	go func() {
		ticker := time.NewTicker(portForwardCleanupEvery)
		defer ticker.Stop()
		for range ticker.C {
			portForwardCacheMu.Lock()
			for key, entry := range portForwardCache {
				if time.Since(entry.lastUsed) > portForwardIdleTTL {
					close(entry.stopCh)
					delete(portForwardCache, key)
					log.Debug().Str("key", key).Msg("Túnel port-forward ocioso encerrado")
				}
			}
			portForwardCacheMu.Unlock()
		}
	}()
}

// OpenPortForward abre (ou reusa um já aberto) um túnel local para namespace/service/port de um
// cluster, retornando a URL local (http://127.0.0.1:<porta>/) pronta pra uso. Resolve um pod
// Running por trás do Service (port-forward do K8s sempre mira um pod, nunca o Service
// diretamente — mesmo mecanismo usado por `kubectl port-forward svc/X`) via o selector do próprio
// Service. Assinatura compatível com discovery.SetPortForwardFunc — ligada em DiscoverClusters.
func (k *KubeConfigManager) OpenPortForward(ctx context.Context, cluster string, target discovery.PortForwardTarget) (string, error) {
	ensurePortForwardCleanup()

	key := portForwardCacheKey(cluster, target)

	portForwardCacheMu.Lock()
	if entry, ok := portForwardCache[key]; ok {
		entry.lastUsed = time.Now()
		url := entry.localURL
		portForwardCacheMu.Unlock()
		return url, nil
	}
	portForwardCacheMu.Unlock()

	restConfig, err := k.GetRestConfig(cluster)
	if err != nil {
		return "", fmt.Errorf("falha ao obter rest config: %w", err)
	}
	clientset, err := k.GetClient(cluster)
	if err != nil {
		return "", fmt.Errorf("falha ao obter client K8s: %w", err)
	}

	svc, err := clientset.CoreV1().Services(target.Namespace).Get(ctx, target.Service, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("falha ao obter service %s/%s: %w", target.Namespace, target.Service, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return "", fmt.Errorf("service %s/%s não tem selector — não é possível resolver o pod por trás dele", target.Namespace, target.Service)
	}

	var selector strings.Builder
	i := 0
	for k2, v2 := range svc.Spec.Selector {
		if i > 0 {
			selector.WriteString(",")
		}
		selector.WriteString(k2 + "=" + v2)
		i++
	}

	pods, err := clientset.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "", fmt.Errorf("falha ao listar pods do service %s/%s: %w", target.Namespace, target.Service, err)
	}
	var podName string
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			podName = p.Name
			break
		}
	}
	if podName == "" {
		return "", fmt.Errorf("nenhum pod Running encontrado por trás do service %s/%s", target.Namespace, target.Service)
	}

	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return "", fmt.Errorf("falha ao criar transporte SPDY: %w", err)
	}

	reqURL := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, reqURL)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	var errOut strings.Builder

	pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", target.Port)}, stopCh, readyCh, io.Discard, &errOut)
	if err != nil {
		close(stopCh)
		return "", fmt.Errorf("falha ao criar port-forwarder: %w", err)
	}

	go func() {
		if fwErr := pf.ForwardPorts(); fwErr != nil {
			log.Warn().
				Err(fwErr).
				Str("cluster", cluster).
				Str("namespace", target.Namespace).
				Str("service", target.Service).
				Str("stderr", errOut.String()).
				Msg("Túnel port-forward encerrado com erro")
		}
	}()

	select {
	case <-readyCh:
	case <-time.After(portForwardReadyTimeout):
		close(stopCh)
		return "", fmt.Errorf("timeout (%s) esperando túnel pro pod %s/%s ficar pronto: %s", portForwardReadyTimeout, target.Namespace, podName, errOut.String())
	}

	ports, err := pf.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return "", fmt.Errorf("falha ao obter porta local do túnel: %w", err)
	}

	localURL := fmt.Sprintf("http://127.0.0.1:%d/", ports[0].Local)

	portForwardCacheMu.Lock()
	portForwardCache[key] = &portForwardEntry{localURL: localURL, stopCh: stopCh, lastUsed: time.Now()}
	portForwardCacheMu.Unlock()

	log.Info().
		Str("cluster", cluster).
		Str("namespace", target.Namespace).
		Str("service", target.Service).
		Str("pod", podName).
		Int("remote_port", target.Port).
		Uint16("local_port", ports[0].Local).
		Msg("Túnel port-forward aberto")

	return localURL, nil
}
