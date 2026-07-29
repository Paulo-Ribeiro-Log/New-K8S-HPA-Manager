package certificates

import (
	"encoding/json"
	"testing"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// parseQueryResult desserializa um QueryResult a partir de um literal JSON — imita o formato real
// da API /api/v1/query do Prometheus, evitando escrever à mão os structs anônimos de
// promclient.QueryResult.
func parseQueryResult(t *testing.T, raw string) *promclient.QueryResult {
	t.Helper()
	var result promclient.QueryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("erro ao parsear QueryResult de teste: %v", err)
	}
	return &result
}

func TestBuildLivePropagationResult_TodasReplicasAtualizadas(t *testing.T) {
	certResult := parseQueryResult(t, `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{"metric": {"kubernetes_pod_name": "nginx-1", "serial_number": "123", "issuer_common_name": "Sectigo RSA OV CA"}, "value": [1700000000, "1"]},
				{"metric": {"kubernetes_pod_name": "nginx-2", "serial_number": "123", "issuer_common_name": "Sectigo RSA OV CA"}, "value": [1700000000, "1"]}
			]
		}
	}`)

	result := buildLivePropagationResult(certResult, nil, "123")

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if result.TotalReplicasFound != 2 {
		t.Errorf("TotalReplicasFound = %d, esperado 2", result.TotalReplicasFound)
	}
	if result.ReplicasCurrent != 2 {
		t.Errorf("ReplicasCurrent = %d, esperado 2", result.ReplicasCurrent)
	}
	if len(result.ReplicasStale) != 0 {
		t.Errorf("esperava 0 réplicas stale, obtive %v", result.ReplicasStale)
	}
	if result.LiveIssuerCN != "Sectigo RSA OV CA" {
		t.Errorf("LiveIssuerCN = %q, esperado %q", result.LiveIssuerCN, "Sectigo RSA OV CA")
	}
}

func TestBuildLivePropagationResult_PropagacaoIncompleta(t *testing.T) {
	certResult := parseQueryResult(t, `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{"metric": {"kubernetes_pod_name": "nginx-1", "serial_number": "123"}, "value": [1700000000, "1"]},
				{"metric": {"kubernetes_pod_name": "nginx-2", "serial_number": "999"}, "value": [1700000000, "1"]},
				{"metric": {"kubernetes_pod_name": "nginx-3", "serial_number": "999"}, "value": [1700000000, "1"]}
			]
		}
	}`)

	result := buildLivePropagationResult(certResult, nil, "123")

	if !result.Checked {
		t.Fatal("esperava Checked=true")
	}
	if result.TotalReplicasFound != 3 {
		t.Errorf("TotalReplicasFound = %d, esperado 3", result.TotalReplicasFound)
	}
	if result.ReplicasCurrent != 1 {
		t.Errorf("ReplicasCurrent = %d, esperado 1", result.ReplicasCurrent)
	}
	if len(result.ReplicasStale) != 2 {
		t.Fatalf("esperava 2 réplicas stale, obtive %d: %v", len(result.ReplicasStale), result.ReplicasStale)
	}
	expectStale := map[string]bool{"nginx-2": true, "nginx-3": true}
	for _, pod := range result.ReplicasStale {
		if !expectStale[pod] {
			t.Errorf("réplica stale inesperada: %s", pod)
		}
	}
	if len(result.Notes) == 0 {
		t.Error("esperava uma nota avisando sobre propagação incompleta")
	}
}

func TestBuildLivePropagationResult_SemSerieEncontrada(t *testing.T) {
	certResult := parseQueryResult(t, `{"status": "success", "data": {"resultType": "vector", "result": []}}`)

	result := buildLivePropagationResult(certResult, nil, "123")

	if result.Checked {
		t.Error("esperava Checked=false quando nenhuma série é encontrada")
	}
	if len(result.Notes) == 0 {
		t.Error("esperava uma nota explicando por que não foi possível checar")
	}
}

func TestBuildLivePropagationResult_CertResultNil(t *testing.T) {
	result := buildLivePropagationResult(nil, nil, "123")

	if result.Checked {
		t.Error("esperava Checked=false quando certResult é nil")
	}
}

func TestLatestExpireTimestamp_EscolheOMaisRecente(t *testing.T) {
	expireResult := parseQueryResult(t, `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{"metric": {"kubernetes_pod_name": "nginx-1"}, "value": [1700000000, "1700000000"]},
				{"metric": {"kubernetes_pod_name": "nginx-2"}, "value": [1700000000, "1800000000"]}
			]
		}
	}`)

	got := latestExpireTimestamp(expireResult)
	if got == nil {
		t.Fatal("esperava um timestamp não-nil")
	}
	want := time.Unix(1800000000, 0)
	if !got.Equal(want) {
		t.Errorf("timestamp = %v, esperado %v", got, want)
	}
}

func TestLatestExpireTimestamp_ResultNil(t *testing.T) {
	if got := latestExpireTimestamp(nil); got != nil {
		t.Errorf("esperava nil, obtive %v", got)
	}
}

func TestLatestExpireTimestamp_ValorInvalidoIgnorado(t *testing.T) {
	expireResult := parseQueryResult(t, `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{"metric": {"kubernetes_pod_name": "nginx-1"}, "value": [1700000000, "not-a-number"]}
			]
		}
	}`)

	if got := latestExpireTimestamp(expireResult); got != nil {
		t.Errorf("esperava nil pra valor não-numérico, obtive %v", got)
	}
}

func TestLeafSerialDecimal(t *testing.T) {
	now := time.Now()
	leaf := genCert(t, "serial.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)

	got, err := LeafSerialDecimal(concatPEM(leaf))
	if err != nil {
		t.Fatalf("LeafSerialDecimal falhou: %v", err)
	}
	if got != leaf.cert.SerialNumber.String() {
		t.Errorf("LeafSerialDecimal = %q, esperado %q", got, leaf.cert.SerialNumber.String())
	}
}

func TestLeafSerialDecimal_PEMInvalido(t *testing.T) {
	if _, err := LeafSerialDecimal([]byte("not a pem")); err == nil {
		t.Error("esperava erro para PEM inválido")
	}
}
