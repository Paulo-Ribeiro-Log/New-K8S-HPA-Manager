package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

func TestRedeSomaSoOsPodsDoDeploymentEAlinhaPorTempo(t *testing.T) {
	f := &promFake{data: map[string][]sample{
		"container_network_receive_bytes_total":           {{map[string]string{}, "2000"}},
		"container_network_transmit_bytes_total":          {{map[string]string{}, "500"}},
		"container_network_receive_packets_dropped_total": {{map[string]string{}, "1.5"}},
	}}
	now := time.Now()
	s, err := CollectNetworkSeries(context.Background(), f, Item{"c1", "calc", "frete-hub"}, now.Add(-30*time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Times) != 2 || len(s.RxBps) != 2 || len(s.TxBps) != 2 || len(s.ErrPs) != 2 {
		t.Fatalf("o fake devolve 2 pontos (ts 1 e 2 s): %+v", s)
	}
	if s.RxBps[1] == nil || *s.RxBps[1] != 2000 || *s.TxBps[1] != 500 || *s.ErrPs[1] != 1.5 {
		t.Errorf("valores errados: rx=%v tx=%v err=%v", s.RxBps[1], s.TxBps[1], s.ErrPs[1])
	}
	if s.StepSeconds != 60 {
		t.Errorf("30 min cabem em passo de 1 min, veio %d s", s.StepSeconds)
	}
	for _, q := range f.queries {
		if !strings.Contains(q, `namespace="calc"`) || !strings.Contains(q, `frete-hub-[a-z0-9]{5,10}-[a-z0-9]{5}`) {
			t.Errorf("a consulta tem de casar só os pods do Deployment: %s", q)
		}
		if !strings.Contains(q, "max by (pod,interface)") {
			t.Errorf("sem o max por interface a série duplicada dobraria a soma: %s", q)
		}
	}
}

func TestRedeJanelaENomeInvalidos(t *testing.T) {
	now := time.Now()
	f := &promFake{}
	if _, err := CollectNetworkSeries(context.Background(), f, Item{"c", "calc", "a;b"}, now.Add(-time.Hour), now); err == nil {
		t.Error("nome inválido deveria falhar")
	}
	if _, err := CollectNetworkSeries(context.Background(), f, Item{"c", "calc", "x"}, now, now.Add(-time.Hour)); err == nil {
		t.Error("fim antes do início deveria falhar")
	}
	if _, err := CollectNetworkSeries(context.Background(), f, Item{"c", "calc", "x"}, now.Add(-30*24*time.Hour), now); err == nil {
		t.Error("30 dias passa da retenção e deveria falhar")
	}
	if len(f.queries) != 0 {
		t.Errorf("pedido inválido não pode consultar o Prometheus: %d consultas", len(f.queries))
	}
}

func TestRedeStepNuncaAbaixoDeUmMinutoENoMaximo150Pontos(t *testing.T) {
	now := time.Now()
	if s := netStep(now.Add(-10*time.Minute), now); s != time.Minute {
		t.Errorf("10 min: %v", s)
	}
	s := netStep(now.Add(-14*24*time.Hour), now)
	if pts := (14 * 24 * time.Hour) / s; pts > netMaxPoints {
		t.Errorf("14 dias dá %d pontos com passo %v", pts, s)
	}
}

func TestRedeFalhaTotalEFalhaParcial(t *testing.T) {
	now := time.Now()
	if _, err := CollectNetworkSeries(context.Background(), &promFake{failAll: true}, Item{"c", "calc", "x"}, now.Add(-time.Hour), now); err == nil {
		t.Error("as três falhando deve dar erro")
	}
	f := &promFake{data: map[string][]sample{"container_network_receive_bytes_total": {{map[string]string{}, "10"}}}, fail: map[string]bool{"container_network_transmit_bytes_total": true}}
	f.data["container_network_transmit_bytes_total"] = []sample{{map[string]string{}, "1"}}
	s, err := CollectNetworkSeries(context.Background(), f, Item{"c", "calc", "x"}, now.Add(-time.Hour), now)
	if err != nil || len(s.Notes) != 1 || s.RxBps[len(s.RxBps)-1] == nil || s.TxBps[len(s.TxBps)-1] != nil {
		t.Errorf("falha parcial: err=%v notes=%v %+v", err, s.Notes, s)
	}
}

type linkFake struct{ liveFake }

func (*linkFake) EntityURL(id string) string { return "https://dt.example/ui/entity/" + id }

func TestLinksDoDynatraceSoQuandoOClienteSabeMontar(t *testing.T) {
	svcs := []dynatrace.ServiceRef{{ID: "SERVICE-B", Name: "b"}, {ID: "SERVICE-A", Name: "a"}, {ID: "", Name: "sem id"}}
	if got := liveLinks(&liveFake{}, svcs); got != nil {
		t.Errorf("sem EntityURL não há link: %v", got)
	}
	got := liveLinks(&linkFake{}, svcs)
	if len(got) != 2 || got[0].Name != "a" || got[0].URL != "https://dt.example/ui/entity/SERVICE-A" {
		t.Errorf("links: %+v", got)
	}
}

func TestEntityURLDoClienteReal(t *testing.T) {
	c, err := dynatrace.NewClient("https://abc12345.live.dynatrace.com/api/v2", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.EntityURL("SERVICE-1A2B"); got != "https://abc12345.live.dynatrace.com/ui/entity/SERVICE-1A2B" {
		t.Errorf("EntityURL = %q", got)
	}
	if c.EntityURL("") != "" {
		t.Error("id vazio não gera link")
	}
}
