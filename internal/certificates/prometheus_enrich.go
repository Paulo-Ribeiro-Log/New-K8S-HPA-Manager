package certificates

import (
	"context"
	"fmt"
	"strconv"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// prometheusEnrichTimeout limita quanto tempo EnrichWithPrometheus espera pelo Prometheus do
// cluster — nunca deve travar a resposta de validate-chain.
const prometheusEnrichTimeout = 8 * time.Second

// LivePropagationResult reporta se a atualização de um certificado já se propagou de fato para
// todas as réplicas do ingress-nginx do cluster — cruzando o serial do leaf cert já validado com o
// que cada réplica reporta estar servindo agora, via as métricas nginx_ingress_controller_ssl_*.
// Checked=false significa "não deu pra checar" (Prometheus indisponível, certificado não está
// atrás de ingress-nginx, etc.) — nunca é tratado como erro pela validação principal.
// Method identifica qual mecanismo produziu o resultado: "prometheus-nginx" (métricas do
// ingress-nginx, campos TotalReplicasFound/ReplicasCurrent/ReplicasStale contam réplicas de pod) ou
// "tls-dial" (handshake TLS direto via EnrichWithTLSDial, os mesmos campos contam hosts públicos
// alcançados em vez de réplicas de pod — usado quando o Prometheus não conseguiu checar, ex:
// clusters GKE Gateway API sem ingress-nginx).
type LivePropagationResult struct {
	Checked            bool       `json:"checked"`
	Method             string     `json:"method,omitempty"`
	TotalReplicasFound int        `json:"total_replicas_found"`
	ReplicasCurrent    int        `json:"replicas_current"`
	ReplicasStale      []string   `json:"replicas_stale,omitempty"` // kubernetes_pod_name (nginx) ou hostname (tls-dial) das réplicas/hosts com serial diferente do atual
	LiveIssuerCN       string     `json:"live_issuer_cn,omitempty"`
	LiveExpiresAt      *time.Time `json:"live_expires_at,omitempty"`
	Notes              []string   `json:"notes,omitempty"`
}

// LeafSerialDecimal parseia certPEM e retorna o serial do leaf cert em decimal
// (leaf.SerialNumber.String()) — formato que bate direto com o label serial_number reportado pelo
// Prometheus, usado como entrada de EnrichWithPrometheus. Diferente do hex usado em
// CertificateInfo.SerialNumber/RollbackBackupInfo.SerialNumber (formatSerialNumber, %X) — não
// confundir os dois formatos.
func LeafSerialDecimal(certPEM []byte) (string, error) {
	certs, err := parsePEMChain(certPEM)
	if err != nil {
		return "", fmt.Errorf("certificado PEM inválido: %w", err)
	}
	if len(certs) == 0 {
		return "", fmt.Errorf("nenhum certificado encontrado no PEM")
	}
	return certs[0].SerialNumber.String(), nil
}

// EnrichWithPrometheus consulta o Prometheus do cluster (nginx_ingress_controller_ssl_certificate_info
// / nginx_ingress_controller_ssl_expire_time_seconds, rotuladas por secret_name+namespace) e cruza
// o serial reportado por cada réplica do ingress-nginx com leafSerialDecimal (o serial do leaf cert
// recém-validado, em decimal — ex: leaf.SerialNumber.String() — não confundir com o formato hex
// usado em CertificateInfo.SerialNumber/RollbackBackupInfo.SerialNumber).
//
// Melhor-esforço: qualquer falha (Prometheus não descoberto/indisponível para o cluster, erro na
// query, nenhuma série encontrada — Secret não referenciado por um Ingress atrás do ingress-nginx,
// ou cluster usa outro ingress controller) retorna Checked:false com uma nota explicativa, nunca um
// erro — isso é um enriquecimento opcional, igual ao enrichWithOpenSSL de validate.go, nunca uma
// fonte de verdade obrigatória.
func EnrichWithPrometheus(cluster, namespace, secretName, leafSerialDecimal string) *LivePropagationResult {
	ctx, cancel := context.WithTimeout(context.Background(), prometheusEnrichTimeout)
	defer cancel()

	promc, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		return &LivePropagationResult{
			Checked: false,
			Method:  "prometheus-nginx",
			Notes:   []string{fmt.Sprintf("Prometheus indisponível para este cluster: %s", err.Error())},
		}
	}

	certQuery := fmt.Sprintf(`nginx_ingress_controller_ssl_certificate_info{secret_name=%q,namespace=%q}`, secretName, namespace)
	certResult, err := promc.Query(ctx, certQuery)
	if err != nil {
		return &LivePropagationResult{
			Checked: false,
			Method:  "prometheus-nginx",
			Notes:   []string{fmt.Sprintf("erro ao consultar Prometheus: %s", err.Error())},
		}
	}

	// Segunda query é só um plus (LiveExpiresAt) — se falhar, não impede o resultado principal.
	expireQuery := fmt.Sprintf(`nginx_ingress_controller_ssl_expire_time_seconds{secret_name=%q,namespace=%q}`, secretName, namespace)
	expireResult, _ := promc.Query(ctx, expireQuery)

	return buildLivePropagationResult(certResult, expireResult, leafSerialDecimal)
}

// buildLivePropagationResult é a lógica pura de comparação entre o que o Prometheus reportou e o
// serial do leaf atual — separada de EnrichWithPrometheus só pra ser testável sem precisar mockar
// HTTP (promclient.PrometheusClient não é uma interface hoje, não vale a pena introduzir uma só
// para isso).
func buildLivePropagationResult(certResult, expireResult *promclient.QueryResult, leafSerialDecimal string) *LivePropagationResult {
	result := &LivePropagationResult{Method: "prometheus-nginx"}

	if certResult == nil || len(certResult.Data.Result) == 0 {
		result.Checked = false
		result.Notes = append(result.Notes, "certificado não encontrado atrás de ingress-nginx neste cluster — o Secret pode não estar referenciado por nenhum Ingress, ou o cluster usa outro ingress controller")
		return result
	}

	result.Checked = true
	result.TotalReplicasFound = len(certResult.Data.Result)

	for _, series := range certResult.Data.Result {
		serial := series.Metric["serial_number"]
		if serial == leafSerialDecimal {
			result.ReplicasCurrent++
		} else if podName := series.Metric["kubernetes_pod_name"]; podName != "" {
			result.ReplicasStale = append(result.ReplicasStale, podName)
		}
		if result.LiveIssuerCN == "" {
			result.LiveIssuerCN = series.Metric["issuer_common_name"]
		}
	}

	if len(result.ReplicasStale) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d de %d réplica(s) do ingress-nginx ainda servem um certificado diferente do atual — propagação pode estar em andamento",
			len(result.ReplicasStale), result.TotalReplicasFound,
		))
	}

	result.LiveExpiresAt = latestExpireTimestamp(expireResult)

	return result
}

// latestExpireTimestamp extrai o timestamp de expiração mais recente entre as réplicas que
// responderam — best-effort: séries malformadas (valor não-numérico, formato inesperado) são
// ignoradas silenciosamente, nunca propagam erro.
func latestExpireTimestamp(expireResult *promclient.QueryResult) *time.Time {
	if expireResult == nil {
		return nil
	}

	var latest *time.Time
	for _, series := range expireResult.Data.Result {
		if len(series.Value) != 2 {
			continue
		}
		valStr, ok := series.Value[1].(string)
		if !ok {
			continue
		}
		epochFloat, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		t := time.Unix(int64(epochFloat), 0)
		if latest == nil || t.After(*latest) {
			latest = &t
		}
	}
	return latest
}
