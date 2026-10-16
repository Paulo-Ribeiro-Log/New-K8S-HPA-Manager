package aws

import "testing"

func TestParseCostAndUsage(t *testing.T) {
	body := []byte(`{"ResultsByTime":[
	  {"Groups":[{"Keys":["Amazon Elastic Compute Cloud - Compute"],"Metrics":{"AmortizedCost":{"Amount":"1200.5","Unit":"USD"}}},
	             {"Keys":["EC2 - Other"],"Metrics":{"AmortizedCost":{"Amount":"80","Unit":"USD"}}}]},
	  {"Groups":[{"Keys":["Amazon Elastic Compute Cloud - Compute"],"Metrics":{"AmortizedCost":{"Amount":"99.5","Unit":"USD"}}},
	             {"Keys":["x"],"Metrics":{"AmortizedCost":{"Amount":"nan?","Unit":"USD"}}}]}
	]}`)
	sc, err := parseCostAndUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if sc.ByService["Amazon Elastic Compute Cloud - Compute"] != 1300 || sc.ByService["EC2 - Other"] != 80 || sc.Currency != "USD" {
		t.Errorf("custo por serviço: %+v", sc)
	}
	if _, ok := sc.ByService["x"]; ok {
		t.Error("valor inválido deve ser ignorado")
	}
}
