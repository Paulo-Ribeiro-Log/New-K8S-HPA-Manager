package handlers

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PortForwardManager gerencia port-forwards ativos para pods do Kiali
type PortForwardManager struct {
	forwards map[string]*PortForward // key: cluster-name
	mu       sync.RWMutex
}

// PortForward representa um port-forward ativo
type PortForward struct {
	Cluster    string
	LocalPort  int
	RemotePort int
	PodName    string
	Namespace  string
	Context    string
	cmd        *exec.Cmd
	Ready      bool
	CreatedAt  time.Time
}

var (
	portForwardManager = &PortForwardManager{
		forwards: make(map[string]*PortForward),
	}
)

// GetPortForward retorna port-forward existente ou nil
func (m *PortForwardManager) GetPortForward(cluster string) *PortForward {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.forwards[cluster]
}

// CreatePortForward cria um port-forward para o pod do Kiali
func (m *PortForwardManager) CreatePortForward(cluster, context, namespace, podName string, localPort, remotePort int) (*PortForward, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verificar se já existe port-forward para este cluster
	if existing, ok := m.forwards[cluster]; ok {
		if existing.cmd != nil && existing.cmd.Process != nil {
			fmt.Printf("[PortForward] Port-forward já existe para cluster %s (porta %d)\n", cluster, existing.LocalPort)
			return existing, nil
		}
		// Port-forward anterior morreu, remover
		delete(m.forwards, cluster)
	}

	// Matar qualquer processo kubectl port-forward na porta 20001
	fmt.Printf("[PortForward] Limpando port-forwards antigos na porta %d...\n", localPort)
	killCmd := exec.Command("sh", "-c", fmt.Sprintf("lsof -ti:%d | xargs kill -9 2>/dev/null || true", localPort))
	killCmd.Run()
	time.Sleep(500 * time.Millisecond)

	fmt.Printf("[PortForward] Criando port-forward: %s/%s -> localhost:%d:%d\n", namespace, podName, localPort, remotePort)

	// Comando kubectl port-forward
	cmd := exec.Command("kubectl",
		"--context", context,
		"-n", namespace,
		"port-forward",
		fmt.Sprintf("pod/%s", podName),
		fmt.Sprintf("%d:%d", localPort, remotePort),
	)

	// Iniciar port-forward
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("erro ao iniciar port-forward: %w", err)
	}

	pf := &PortForward{
		Cluster:    cluster,
		LocalPort:  localPort,
		RemotePort: remotePort,
		PodName:    podName,
		Namespace:  namespace,
		Context:    context,
		cmd:        cmd,
		Ready:      false,
		CreatedAt:  time.Now(),
	}

	m.forwards[cluster] = pf

	// Aguardar port-forward ficar pronto (verificar conectividade)
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)

			// Testar conectividade
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", localPort))
			if err == nil {
				resp.Body.Close()
				pf.Ready = true
				fmt.Printf("[PortForward] ✅ Port-forward pronto e testado: localhost:%d -> %s/%s:%d\n", localPort, namespace, podName, remotePort)
				return
			}
		}
		fmt.Printf("[PortForward] ⚠️ Port-forward pode não estar pronto após 10 segundos\n")
		pf.Ready = true // Marcar como pronto de qualquer forma
	}()

	return pf, nil
}

// StopPortForward para um port-forward específico
func (m *PortForwardManager) StopPortForward(cluster string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pf, ok := m.forwards[cluster]
	if !ok {
		return fmt.Errorf("port-forward não encontrado para cluster %s", cluster)
	}

	if pf.cmd != nil && pf.cmd.Process != nil {
		fmt.Printf("[PortForward] Parando port-forward para cluster %s\n", cluster)
		if err := pf.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("erro ao parar port-forward: %w", err)
		}
	}

	delete(m.forwards, cluster)
	return nil
}

// GetOrCreateKialiPortForward obtém port-forward existente ou cria novo
func GetOrCreateKialiPortForward(cluster, context, namespace, podName string) (*PortForward, error) {
	// Verificar se já existe
	if pf := portForwardManager.GetPortForward(cluster); pf != nil {
		if pf.Ready {
			return pf, nil
		}
		// Aguardar ficar pronto
		fmt.Printf("[PortForward] Aguardando port-forward ficar pronto...\n")
		for i := 0; i < 15; i++ {
			if pf.Ready {
				return pf, nil
			}
			time.Sleep(1 * time.Second)
		}
		return pf, nil
	}

	// Criar novo port-forward
	// Usar porta local 20001 fixa (porta padrão do Kiali)
	localPort := 20001
	remotePort := 20001

	pf, err := portForwardManager.CreatePortForward(cluster, context, namespace, podName, localPort, remotePort)
	if err != nil {
		return nil, err
	}

	// Aguardar ficar pronto (até 15 segundos)
	fmt.Printf("[PortForward] Aguardando port-forward ficar pronto...\n")
	for i := 0; i < 15; i++ {
		if pf.Ready {
			return pf, nil
		}
		time.Sleep(1 * time.Second)
	}

	if !pf.Ready {
		return nil, fmt.Errorf("port-forward não ficou pronto após 15 segundos")
	}

	return pf, nil
}

// CleanupOldPortForwards limpa port-forwards antigos (> 1 hora)
func CleanupOldPortForwards() {
	portForwardManager.mu.Lock()
	defer portForwardManager.mu.Unlock()

	now := time.Now()
	for cluster, pf := range portForwardManager.forwards {
		if now.Sub(pf.CreatedAt) > 1*time.Hour {
			fmt.Printf("[PortForward] Limpando port-forward antigo para cluster %s\n", cluster)
			if pf.cmd != nil && pf.cmd.Process != nil {
				pf.cmd.Process.Kill()
			}
			delete(portForwardManager.forwards, cluster)
		}
	}
}

// GetKialiLocalURL retorna URL local do Kiali via port-forward
func GetKialiLocalURL(cluster, context string) (string, error) {
	// Buscar pod do Kiali
	cmd := exec.Command("kubectl",
		"--context", context,
		"-n", "istio-system",
		"get", "pods",
		"-l", "app=kiali",
		"-o", "jsonpath={.items[0].metadata.name}",
	)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("erro ao buscar pod do Kiali: %w", err)
	}

	podName := strings.TrimSpace(string(output))
	if podName == "" {
		return "", fmt.Errorf("pod do Kiali não encontrado")
	}

	fmt.Printf("[PortForward] Pod do Kiali encontrado: %s\n", podName)

	// Criar ou obter port-forward
	pf, err := GetOrCreateKialiPortForward(cluster, context, "istio-system", podName)
	if err != nil {
		return "", err
	}

	// Retornar URL local
	url := fmt.Sprintf("http://localhost:%d/", pf.LocalPort)
	return url, nil
}
