package healthcheck

import (
	"context"
	"fmt"

	"k8s-hpa-manager/internal/monitoring/alerts"
	"k8s-hpa-manager/internal/monitoring/discovery"
)

// PrometheusAlertsTargetSource reaproveita alerts.Client.GetAlerts() (mesmo cliente já usado pela
// aba de Alertas/Dashboard, nunca antes ligado ao Health Check) — filtra por State=="firing" e
// extrai Labels["namespace"] quando presente. Alertas sem esse label são ignorados pra fins de
// triagem (não contribuem um namespace, mas não quebram nada) — mesma filosofia de "nunca inventa
// sinal" já usada no Dynatrace (extractEnvHint) e no Access Checker. Não há garantia de que toda
// regra de alerta desta empresa tenha o label namespace (ver seção 1.3/4.1 do plano).
type PrometheusAlertsTargetSource struct {
	// ignoredAlerts (Fase 4 — TriageIgnoreManager): alertnames que nunca devem contribuir
	// namespace nenhum, mesmo firing. Leitura de mapa nil é segura em Go — não precisa ser
	// inicializado quando não há supressão configurada.
	ignoredAlerts map[string]struct{}
}

func NewPrometheusAlertsTargetSource(ignoredAlerts map[string]struct{}) *PrometheusAlertsTargetSource {
	return &PrometheusAlertsTargetSource{ignoredAlerts: ignoredAlerts}
}

func (s *PrometheusAlertsTargetSource) Name() string {
	return "Prometheus"
}

func (s *PrometheusAlertsTargetSource) Resolve(_ context.Context, cluster string) TargetSourceResult {
	promURL := discovery.GetPrometheusURL(cluster)
	if promURL == "" || !discovery.IsEndpointAvailable(promURL) {
		return TargetSourceResult{Available: false}
	}

	client := alerts.NewClient(promURL)
	allAlerts, err := client.GetAlerts()
	if err != nil {
		return TargetSourceResult{Available: false, Err: err}
	}

	nsSet := make(map[string]struct{})
	reasons := make(map[string][]string)
	for _, a := range allAlerts {
		if a.State != "firing" {
			continue
		}
		alertName := a.Labels["alertname"]
		if _, ignored := s.ignoredAlerts[alertName]; ignored { // Fase 4 — supressão por alertname
			continue
		}
		ns := a.Labels["namespace"]
		if ns == "" {
			continue // sem label — não contribui, mas a fonte continua Available
		}
		severity := a.Labels["severity"]
		reason := fmt.Sprintf("Prometheus: %s (%s)", alertName, severity)
		nsSet[ns] = struct{}{}
		reasons[ns] = append(reasons[ns], reason)
	}

	namespaces := make([]string, 0, len(nsSet))
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}

	return TargetSourceResult{Available: true, Namespaces: namespaces, Reasons: reasons}
}
