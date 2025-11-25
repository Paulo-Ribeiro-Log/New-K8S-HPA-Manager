package alerts

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client é um cliente para buscar alertas do Prometheus
type Client struct {
	prometheusURL string
	httpClient    *http.Client
}

// NewClient cria um novo cliente de alertas
func NewClient(prometheusURL string) *Client {
	return &Client{
		prometheusURL: prometheusURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}

// GetAlerts busca todos os alertas ativos do Prometheus
func (c *Client) GetAlerts() ([]Alert, error) {
	url := fmt.Sprintf("%s/api/v1/alerts", c.prometheusURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar alertas: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("erro HTTP %d ao buscar alertas", resp.StatusCode)
	}

	var response PrometheusAlertsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("resposta da API não foi sucesso: %s", response.Status)
	}

	return response.Data.Alerts, nil
}

// GetAlertsByFilter busca alertas aplicando filtros
func (c *Client) GetAlertsByFilter(filter AlertFilter) ([]Alert, error) {
	allAlerts, err := c.GetAlerts()
	if err != nil {
		return nil, err
	}

	var filtered []Alert
	for _, alert := range allAlerts {
		if c.matchesFilter(alert, filter) {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}

// matchesFilter verifica se um alerta corresponde aos filtros
func (c *Client) matchesFilter(alert Alert, filter AlertFilter) bool {
	// Filtro por severidade
	if len(filter.Severity) > 0 {
		severity := alert.Labels["severity"]
		found := false
		for _, s := range filter.Severity {
			if severity == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filtro por estado
	if len(filter.State) > 0 {
		found := false
		for _, s := range filter.State {
			if alert.State == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filtro por namespace
	if filter.Namespace != "" {
		if alert.Labels["namespace"] != filter.Namespace {
			return false
		}
	}

	// Filtro por nomes de alertas
	if len(filter.AlertNames) > 0 {
		alertName := alert.Labels["alertname"]
		found := false
		for _, name := range filter.AlertNames {
			if alertName == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// GetAlertSummary retorna um resumo de alertas por severidade
func (c *Client) GetAlertSummary() (*AlertSummary, error) {
	alerts, err := c.GetAlerts()
	if err != nil {
		return nil, err
	}

	summary := &AlertSummary{}
	for _, alert := range alerts {
		if alert.State != "firing" {
			continue
		}

		severity := alert.Labels["severity"]
		switch severity {
		case "critical":
			summary.Critical++
		case "warning":
			summary.Warning++
		case "info":
			summary.Info++
		}
		summary.Total++
	}

	return summary, nil
}

// GetHPAAlerts busca alertas relevantes para HPAs
func (c *Client) GetHPAAlerts() ([]HPAAlert, error) {
	// Nomes de alertas relevantes para HPAs
	hpaAlertNames := []string{
		"KubeHpaMaxedOut",
		"KubeHpaReplicasMismatch",
		"CPUThrottlingHigh",
		"Eventos OOMKilled",
		"Eventos CrashLoopBackOff",
		"KubePodCrashLooping",
		"KubePodNotReady",
		"KubeDeploymentReplicasMismatch",
	}

	filter := AlertFilter{
		AlertNames: hpaAlertNames,
		State:      []string{"firing"},
	}

	alerts, err := c.GetAlertsByFilter(filter)
	if err != nil {
		return nil, err
	}

	var hpaAlerts []HPAAlert
	for _, alert := range alerts {
		hpaAlert := HPAAlert{
			AlertName:   alert.Labels["alertname"],
			Severity:    alert.Labels["severity"],
			State:       alert.State,
			Description: alert.Annotations["description"],
			Namespace:   alert.Labels["namespace"],
			Deployment:  alert.Labels["deployment"],
			Pod:         alert.Labels["pod"],
			Container:   alert.Labels["container"],
		}

		if alert.ActiveAt != nil {
			hpaAlert.ActiveAt = *alert.ActiveAt
		}

		// Adicionar sugestão de mitigação
		hpaAlert.Mitigation = c.getMitigationForHPAAlert(hpaAlert.AlertName)

		hpaAlerts = append(hpaAlerts, hpaAlert)
	}

	return hpaAlerts, nil
}

// GetNodePoolAlerts busca alertas relevantes para Node Pools
func (c *Client) GetNodePoolAlerts() ([]NodePoolAlert, error) {
	// Nomes de alertas relevantes para Node Pools
	nodeAlertNames := []string{
		"KubeNodeNotReady",
		"KubeNodeUnreachable",
		"KubeletDown",
		"KubeletTooManyPods",
		"NodeFilesystemAlmostOutOfSpace",
		"NodeFilesystemSpaceFillingUp",
		"KubeNodeReadinessFlapping",
		"KubeletPlegDurationHigh",
		"KubeletPodStartUpLatencyHigh",
		"NodeHighNumberConntrackEntriesUsed",
		"NodeNetworkInterfaceFlapping",
	}

	filter := AlertFilter{
		AlertNames: nodeAlertNames,
		State:      []string{"firing"},
	}

	alerts, err := c.GetAlertsByFilter(filter)
	if err != nil {
		return nil, err
	}

	var nodeAlerts []NodePoolAlert
	for _, alert := range alerts {
		nodeAlert := NodePoolAlert{
			AlertName:   alert.Labels["alertname"],
			Severity:    alert.Labels["severity"],
			State:       alert.State,
			Description: alert.Annotations["description"],
			Node:        alert.Labels["node"],
		}

		if alert.ActiveAt != nil {
			nodeAlert.ActiveAt = *alert.ActiveAt
		}

		// Adicionar sugestão de mitigação
		nodeAlert.Mitigation = c.getMitigationForNodeAlert(nodeAlert.AlertName)

		nodeAlerts = append(nodeAlerts, nodeAlert)
	}

	return nodeAlerts, nil
}

// getMitigationForHPAAlert retorna sugestão de mitigação para alertas de HPA
func (c *Client) getMitigationForHPAAlert(alertName string) *MitigationSuggestion {
	mitigations := map[string]MitigationSuggestion{
		"KubeHpaMaxedOut": {
			AutoMitigable: true,
			Action:        "Aumentar maxReplicas do HPA",
			Priority:      "high",
			Steps: []string{
				"Aumentar spec.maxReplicas em +50%",
				"Aplicar mudança via kubectl ou interface web",
			},
		},
		"CPUThrottlingHigh": {
			AutoMitigable: true,
			Action:        "Aumentar CPU limit ou replicas",
			Priority:      "medium",
			Steps: []string{
				"Opção 1: Aumentar resources.limits.cpu",
				"Opção 2: Aumentar maxReplicas do HPA",
			},
		},
		"Eventos OOMKilled": {
			AutoMitigable: true,
			Action:        "Aumentar memory limit",
			Priority:      "critical",
			Steps: []string{
				"Aumentar resources.limits.memory em +50% ou mais",
				"Aplicar mudança e monitorar",
			},
		},
		"Eventos CrashLoopBackOff": {
			AutoMitigable: false,
			Action:        "Investigar logs do pod",
			Priority:      "critical",
			Steps: []string{
				"kubectl logs <pod> --previous",
				"kubectl describe pod <pod>",
				"Analisar erro e corrigir aplicação",
			},
		},
		"KubePodCrashLooping": {
			AutoMitigable: false,
			Action:        "Investigar logs do pod",
			Priority:      "high",
			Steps: []string{
				"kubectl logs <pod> --previous",
				"Analisar causa do crash",
			},
		},
	}

	if mitigation, exists := mitigations[alertName]; exists {
		return &mitigation
	}

	// Default para alertas desconhecidos
	return &MitigationSuggestion{
		AutoMitigable: false,
		Action:        "Investigação manual necessária",
		Priority:      "medium",
		Steps:         []string{"Analisar alerta e contexto", "Definir ação apropriada"},
	}
}

// getMitigationForNodeAlert retorna sugestão de mitigação para alertas de Node Pool
func (c *Client) getMitigationForNodeAlert(alertName string) *MitigationSuggestion {
	mitigations := map[string]MitigationSuggestion{
		"KubeletTooManyPods": {
			AutoMitigable: true,
			Action:        "Aumentar count do Node Pool",
			Priority:      "high",
			Steps: []string{
				"Verificar se múltiplos nodes têm este alerta",
				"Aumentar node pool count em +2 nodes",
				"Aplicar mudança via interface web",
			},
		},
		"NodeFilesystemAlmostOutOfSpace": {
			AutoMitigable: false,
			Action:        "Limpar disco ou aumentar disk size",
			Priority:      "high",
			Steps: []string{
				"Limpar imagens Docker antigas: docker system prune",
				"Ou aumentar osDiskSizeGB do Node Pool",
			},
		},
		"KubeNodeNotReady": {
			AutoMitigable: false,
			Action:        "Investigar node",
			Priority:      "critical",
			Steps: []string{
				"kubectl describe node <node>",
				"Verificar eventos e condições",
				"Reiniciar node se necessário",
			},
		},
	}

	if mitigation, exists := mitigations[alertName]; exists {
		return &mitigation
	}

	// Default
	return &MitigationSuggestion{
		AutoMitigable: false,
		Action:        "Investigação manual necessária",
		Priority:      "medium",
		Steps:         []string{"Analisar alerta e node", "Definir ação apropriada"},
	}
}
