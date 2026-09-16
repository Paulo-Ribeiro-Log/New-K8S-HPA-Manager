package handlers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

// TestReportSFKey_MesmosParametrosMesmaChave — F4.1 do FINOPS-IMPROVEMENTS-PLAN.md. Cobre a
// propriedade que realmente importa pro dedup estar CORRETO: chamadas com os MESMOS parâmetros
// (o caso real de duas abas/duplo-clique) sempre caem na mesma chave; qualquer parâmetro
// diferente NUNCA pode colidir com outra chave — dedupar requisições diferentes devolveria a
// resposta errada (ou pularia um efeito colateral, ex: persist_rightsizing) pro chamador errado.
func TestReportSFKey_MesmosParametrosMesmaChave(t *testing.T) {
	base := reportSFKey("akspriv-abastecimento-hlg-admin", []string{"ns1", "ns2"}, 30, true, "", true)
	again := reportSFKey("akspriv-abastecimento-hlg-admin", []string{"ns1", "ns2"}, 30, true, "", true)
	if base != again {
		t.Fatalf("mesmos parâmetros produziram chaves diferentes: %q vs %q", base, again)
	}
}

func TestReportSFKey_ParametrosDiferentesNuncaColidem(t *testing.T) {
	base := reportSFKey("cluster-a", []string{"ns1"}, 30, true, "", true)
	variants := []string{
		reportSFKey("cluster-b", []string{"ns1"}, 30, true, "", true),                 // cluster diferente
		reportSFKey("cluster-a", []string{"ns2"}, 30, true, "", true),                 // namespace diferente
		reportSFKey("cluster-a", nil, 30, true, "", true),                             // sem namespace (comportamento real diferente — GetReport inteiro vs filtrado)
		reportSFKey("cluster-a", []string{"ns1"}, 7, true, "", true),                  // window_days diferente
		reportSFKey("cluster-a", []string{"ns1"}, 30, false, "", true),                // with_prometheus diferente
		reportSFKey("cluster-a", []string{"ns1"}, 30, true, "http://prom:9090", true), // prometheus_url diferente
		reportSFKey("cluster-a", []string{"ns1"}, 30, true, "", false),                // persist_rightsizing diferente
	}
	seen := map[string]bool{base: true}
	for i, v := range variants {
		if seen[v] {
			t.Errorf("variante %d colidiu com uma chave já vista (%q) — deveria ser distinta", i, v)
		}
		seen[v] = true
	}
}

func TestRightsizingScanSFKey_MesmosParametrosMesmaChave(t *testing.T) {
	base := rightsizingScanSFKey("cluster-a", 30, "")
	again := rightsizingScanSFKey("cluster-a", 30, "")
	if base != again {
		t.Fatalf("mesmos parâmetros produziram chaves diferentes: %q vs %q", base, again)
	}
}

func TestRightsizingScanSFKey_ParametrosDiferentesNuncaColidem(t *testing.T) {
	base := rightsizingScanSFKey("cluster-a", 30, "")
	variants := []string{
		rightsizingScanSFKey("cluster-b", 30, ""),
		rightsizingScanSFKey("cluster-a", 7, ""),
		rightsizingScanSFKey("cluster-a", 30, "http://prom:9090"),
	}
	seen := map[string]bool{base: true}
	for i, v := range variants {
		if seen[v] {
			t.Errorf("variante %d colidiu com uma chave já vista (%q) — deveria ser distinta", i, v)
		}
		seen[v] = true
	}
}

// TestSingleflight_ChamadasConcorrentesIdenticasReaproveitamUmaSoExecucao valida o mecanismo em
// si (golang.org/x/sync/singleflight, já vendorizado e usado por getFreshEKSToken/
// GetFreshGKEToken nesta app) contra o cenário real do F4.1: N goroutines disparando a MESMA
// chave ao mesmo tempo (duplo-clique/duas abas) devem resultar em UMA ÚNICA execução da função
// cara, com todas as goroutines recebendo o mesmo resultado — nunca uma chamada por goroutine.
func TestSingleflight_ChamadasConcorrentesIdenticasReaproveitamUmaSoExecucao(t *testing.T) {
	var group singleflight.Group
	var executions int32

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	start := make(chan struct{})
	results := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // solta todas as goroutines juntas, maximizando a chance real de corrida
			v, _, _ := group.Do("cluster-x|30|", func() (interface{}, error) {
				atomic.AddInt32(&executions, 1)
				// Alarga deliberadamente a janela de corrida (o scan real leva 40-60s, tempo mais
				// que suficiente pras outras goroutines chegarem no Do() do MESMO cluster antes
				// desta terminar) — sem isso, uma execução instantânea poderia terminar e remover
				// a entrada em voo antes de todas as 20 goroutines chamarem group.Do(), tornando o
				// teste flaky por um artefato de agendamento, não por uma falha real do dedup.
				time.Sleep(50 * time.Millisecond)
				return 42, nil
			})
			results[idx] = v.(int)
		}(i)
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&executions); got != 1 {
		t.Errorf("esperava exatamente 1 execução da função cara pra %d chamadas concorrentes com a MESMA chave, teve %d", concurrency, got)
	}
	for i, r := range results {
		if r != 42 {
			t.Errorf("goroutine %d recebeu resultado %d, esperava 42 (mesmo resultado compartilhado)", i, r)
		}
	}
}

// TestSingleflight_ChavesDiferentesNuncaSaoDedupidas confirma o outro lado da mesma garantia:
// chaves DIFERENTES sempre disparam execuções independentes — nunca um cluster "rouba" o
// resultado computado pra outro.
func TestSingleflight_ChavesDiferentesNuncaSaoDedupidas(t *testing.T) {
	var group singleflight.Group
	var executions int32

	var wg sync.WaitGroup
	keys := []string{"cluster-a|30|", "cluster-b|30|", "cluster-c|30|"}
	wg.Add(len(keys))
	for _, k := range keys {
		go func(key string) {
			defer wg.Done()
			_, _, _ = group.Do(key, func() (interface{}, error) {
				atomic.AddInt32(&executions, 1)
				return key, nil
			})
		}(k)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&executions); got != int32(len(keys)) {
		t.Errorf("esperava %d execuções independentes pra %d chaves distintas, teve %d", len(keys), len(keys), got)
	}
}
