package finops

import (
	"errors"
	"testing"
)

const deadlineErr = `Post "https://prometheus-akspriv-oferta-prd.viavarejo.com.br/api/v1/query": context deadline exceeded`

func enricherWithErrors(errs map[string]string) *PrometheusEnricher {
	e := &PrometheusEnricher{}
	for label, msg := range errs {
		e.recordWorkloadQueryError(label, errors.New(msg))
	}
	return e
}

// Caso real (akspriv-oferta-prd): as 8 queries PESADAS de container estouraram o tempo e nenhuma
// de HPA (leve) falhou — o Prometheus estava no ar. Não é rede/VPN.
func TestHeavyQueryTimeoutsOnly_RealIncident(t *testing.T) {
	e := enricherWithErrors(map[string]string{
		"cpu_p95": deadlineErr, "cpu_avg": deadlineErr, "mem_p95": deadlineErr, "mem_avg": deadlineErr,
		"cpu_max_value": deadlineErr, "mem_max_value": deadlineErr, "cpu_max_range": deadlineErr, "mem_max_range": deadlineErr,
	})
	if !e.HeavyQueryTimeoutsOnly() {
		t.Error("só timeouts de query pesada: deveria ser true")
	}
}

func TestHeavyQueryTimeoutsOnly_NotWhenNetworkOrLightQueryFails(t *testing.T) {
	// Até a query LEVE de HPA estourou: o Prometheus não está respondendo — rede/indisponível.
	if enricherWithErrors(map[string]string{"cpu_p95": deadlineErr, "hpa_avg": deadlineErr}).HeavyQueryTimeoutsOnly() {
		t.Error("query de HPA (leve) também falhou: não é só custo de query pesada")
	}
	// Erro que não é timeout (conexão recusada, 5xx...) nunca é "só timeout".
	if enricherWithErrors(map[string]string{"cpu_p95": deadlineErr, "mem_p95": "dial tcp: connection refused"}).HeavyQueryTimeoutsOnly() {
		t.Error("conexão recusada misturada: não é só timeout")
	}
	// Sem erro nenhum (coleta ok, mesmo vazia): false.
	if (&PrometheusEnricher{}).HeavyQueryTimeoutsOnly() {
		t.Error("sem erros deveria ser false")
	}
}
