package finops

import (
	"math"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/storage"
)

// Saída REAL do PerfBenchScript rodando na imagem netshoot (com 3 amostras na época; o parser não
// depende da quantidade). Mostra também a variação real entre amostras num host ruidoso.
const perfBenchSample = `@@CPU 11th Gen Intel(R) Core(TM) i7-1165G7 @ 2.80GHz
@@NPROC 8
@@PY 14.580
@@PY 10.099
@@PY 3.166
@@RSA 473.0
`

func TestParsePerfBenchOutput(t *testing.T) {
	r, err := ParsePerfBenchOutput(perfBenchSample)
	if err != nil {
		t.Fatal(err)
	}
	if r.CPUModel != "11th Gen Intel(R) Core(TM) i7-1165G7 @ 2.80GHz" || r.VCPUs != 8 {
		t.Errorf("cpu/nproc errados: %q %d", r.CPUModel, r.VCPUs)
	}
	if r.PyBest != 14.58 || r.PySamples != 3 || r.RSASignPerSec != 473.0 {
		t.Errorf("py/rsa errados: best=%v n=%d rsa=%v", r.PyBest, r.PySamples, r.RSASignPerSec)
	}
	if want := (14.58 - 3.166) / 14.58; math.Abs(r.PySpread-want) > 1e-9 {
		t.Errorf("spread = %v, want %v", r.PySpread, want)
	}
}

func TestParsePerfBenchOutput_RSAMelhorDeN(t *testing.T) {
	r, err := ParsePerfBenchOutput("@@CPU x\n@@NPROC 4\n@@PY 10\n@@RSA 1131.0\n@@RSA 2653.0\n@@RSA 1285.0\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.RSASignPerSec != 2653 {
		t.Errorf("RSA deve ser a MELHOR das amostras (uma só é refém de ruído), got %v", r.RSASignPerSec)
	}
}

func TestParsePerfBenchOutput_SemAmostraEhErro(t *testing.T) {
	if _, err := ParsePerfBenchOutput("@@CPU x\n@@NPROC 4\npython3: not found\n"); err == nil {
		t.Fatal("sem nenhuma amostra deveria ser erro (não inventar número)")
	}
}

func TestSeriesKey(t *testing.T) {
	same := [][2]string{
		{"Standard_F4s_v2", "Standard_F8s_v2"},
		{"Standard_F4s_v2", "Standard_f2s_v2"}, // o registro tem SKUs em caixa diferente
		{"Standard_D4s_v4", "Standard_D8s_v4"},
	}
	for _, p := range same {
		if a, b := SeriesKey(p[0]), SeriesKey(p[1]); a == "" || a != b {
			t.Errorf("%s e %s deveriam ser a mesma série: %q vs %q", p[0], p[1], a, b)
		}
	}
	diff := [][2]string{
		{"Standard_F4s_v2", "Standard_D4s_v4"},
		{"Standard_D4s_v4", "Standard_D4as_v4"}, // AMD ≠ Intel
		{"Standard_D4s_v4", "Standard_D4s_v5"},
	}
	for _, p := range diff {
		if SeriesKey(p[0]) == SeriesKey(p[1]) {
			t.Errorf("%s e %s NÃO são a mesma série", p[0], p[1])
		}
	}
	if k := SeriesKey("n1-standard-2"); k != "" {
		t.Errorf("SKU fora do padrão Azure não infere série, got %q", k)
	}
}

func rec(sku string, score, spread float64) storage.NodePerfBenchmark {
	return storage.NodePerfBenchmark{Cluster: "c", NodeName: sku + "-n", SKU: sku, CPUModel: "Xeon " + sku, PyScore: score, PySpread: spread}
}

func TestBuildSKUPerf(t *testing.T) {
	got := BuildSKUPerf([]storage.NodePerfBenchmark{
		rec("Standard_F4s_v2", 100, 0.05),
		rec("Standard_f4s_v2", 110, 0.05), // mesma SKU, caixa diferente → agrega
		rec("Standard_F4s_v2", 300, 0.60), // ruidoso: fica de fora quando há medições boas
		rec("Standard_D4s_v4", 90, 0.70),  // só ruidosas → usa, mas marca
		rec("Standard_D4s_v4", 100, 0.80),
	})
	f := got["standard_f4s_v2"]
	if f.Nodes != 2 || f.Score != 105 || f.Noisy {
		t.Errorf("F4s_v2: nodes=%d score=%v noisy=%v (esperado 2/105/false)", f.Nodes, f.Score, f.Noisy)
	}
	d := got["standard_d4s_v4"]
	if d.Nodes != 2 || d.Score != 95 || !d.Noisy {
		t.Errorf("D4s_v4: nodes=%d score=%v noisy=%v (esperado 2/95/true)", d.Nodes, d.Score, d.Noisy)
	}
}

func TestLookupPerf_InfereDaMesmaSerie(t *testing.T) {
	by := BuildSKUPerf([]storage.NodePerfBenchmark{rec("Standard_F4s_v2", 100, 0.02)})
	p, ok := LookupPerf("Standard_F2s_v2", by)
	if !ok || p.Source != "same_series" || p.Score != 100 {
		t.Fatalf("F2s_v2 deveria herdar da F4s_v2 (mesma série): %+v ok=%v", p, ok)
	}
	if _, ok := LookupPerf("Standard_D4s_v4", by); ok {
		t.Error("outra série sem medição NÃO pode ser inferida")
	}
}

func TestComparePerf_Niveis(t *testing.T) {
	by := map[string]PerfInfo{
		"standard_f4s_v2": {Score: 100, Nodes: 2, Source: "measured"},
		"standard_d4s_v4": {Score: 84, Nodes: 2, Source: "measured", CPUModels: []string{"Xeon A"}},
		"standard_d8s_v4": {Score: 130, Nodes: 1, Source: "measured"},
		"standard_e4s_v4": {Score: 100, Nodes: 1, Source: "measured"},
		"standard_b4s_v2": {Score: 94, Nodes: 1, Source: "measured"},
	}
	cases := []struct {
		alt, level string
	}{
		{"Standard_D4s_v4", "slower"},
		{"Standard_D8s_v4", "faster"},
		{"Standard_E4s_v4", "equivalent"},
		{"Standard_B4s_v2", "slightly_slower"},
	}
	for _, c := range cases {
		got := ComparePerf("Standard_F4s_v2", c.alt, by)
		if got == nil || got.Level != c.level {
			t.Errorf("%s: level=%v, want %s", c.alt, got, c.level)
		}
	}
	slower := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", by)
	if math.Abs(slower.Relative-0.84) > 1e-9 || !strings.Contains(slower.Note, "16%") || !strings.Contains(slower.Note, "19%") {
		t.Errorf("nota/relativo da comparação 0,84 errados: %+v", slower)
	}
	if !strings.Contains(slower.Note, "I/O, rede e throttling") {
		t.Errorf("a nota deve deixar claro que só mede CPU por thread: %q", slower.Note)
	}
}

func TestComparePerf_SemMedicaoENoMesmoSKU(t *testing.T) {
	by := map[string]PerfInfo{"standard_f4s_v2": {Score: 100, Nodes: 1, Source: "measured"}}
	if c := ComparePerf("Standard_F4s_v2", "standard_F4S_v2", by); c != nil {
		t.Errorf("mesmo SKU (caixa diferente) não tem o que comparar: %+v", c)
	}
	c := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", by)
	if c == nil || c.Level != "unknown" || c.Relative != 0 || !strings.Contains(c.Note, "Standard_D4s_v4") {
		t.Errorf("sem medição deve dizer o que falta medir, sem número inventado: %+v", c)
	}
	// SKU atual sem medição → aponta o atual, não a alternativa.
	c = ComparePerf("Standard_D4s_v4", "Standard_F4s_v2", by)
	if c.Level != "unknown" || !strings.Contains(c.Note, "Standard_D4s_v4") {
		t.Errorf("deveria apontar o SKU atual sem medição: %+v", c)
	}
}

func TestComparePerf_SerieInferidaEhSinalizada(t *testing.T) {
	by := map[string]PerfInfo{
		"standard_f4s_v2": {Score: 100, Nodes: 1, Source: "measured"},
		"standard_d4s_v4": {Score: 100, Nodes: 1, Source: "measured"},
	}
	c := ComparePerf("Standard_F4s_v2", "Standard_D8s_v4", by) // D8s_v4 nunca medido: herda da série
	if c.Source != "same_series" || !strings.Contains(c.Note, "mesma série") {
		t.Errorf("inferência por série precisa ser identificada: %+v", c)
	}
}

func TestParsePerfBenchOutput_Features(t *testing.T) {
	r, err := ParsePerfBenchOutput("@@CPU x\n@@NPROC 4\n@@FEAT adx avx2 avx512f \n@@PY 10\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Features) != 3 || r.Features[2] != "avx512f" {
		t.Errorf("features mal lidas: %v", r.Features)
	}
	// Linha @@FEAT vazia (CPU sem nenhuma das extensões) não pode virar uma feature "".
	r, _ = ParsePerfBenchOutput("@@CPU x\n@@FEAT \n@@PY 10\n")
	if len(r.Features) != 0 {
		t.Errorf("@@FEAT vazio deveria dar zero features, got %v", r.Features)
	}
}

// Números REAIS medidos no HLG (Xeon 8370C nos dois): F4s_v2 não expõe as extensões de criptografia
// que o D4s_v4 expõe — e o RSA fica ~2x mais lento. O comparativo tem que dizer isso.
func TestComparePerf_CriptoDiferenteNoMesmoProcessador(t *testing.T) {
	const model = "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz"
	recs := []storage.NodePerfBenchmark{
		{SKU: "Standard_F4s_v2", CPUModel: model, PyScore: 18.3, PySpread: 0.1, RSASignPerSec: 1300, CPUFeatures: "adx avx2 avx512f"},
		{SKU: "Standard_F4s_v2", CPUModel: model, PyScore: 18.2, PySpread: 0.1, RSASignPerSec: 1250, CPUFeatures: "adx avx2 avx512f"},
		{SKU: "Standard_D4s_v4", CPUModel: model, PyScore: 18.9, PySpread: 0.1, RSASignPerSec: 2650, CPUFeatures: "adx avx2 avx512f avx512ifma sha_ni vaes vpclmulqdq"},
	}
	by := BuildSKUPerf(recs)

	f := by["standard_f4s_v2"]
	if f.RSAScore != 1275 || len(f.Features) != 3 {
		t.Errorf("F4s_v2: rsa=%v features=%v", f.RSAScore, f.Features)
	}

	// Trocar D4s_v4 por F4s_v2 (o que o rightsizing costuma sugerir): CPU geral equivalente, cripto pior.
	c := ComparePerf("Standard_D4s_v4", "Standard_F4s_v2", by)
	if c.Level != "equivalent" {
		t.Errorf("CPU geral deveria ser equivalente (18,3 vs 18,9), got %s (rel %.3f)", c.Level, c.Relative)
	}
	if c.CryptoLevel != "slower" || c.CryptoRelative > 0.5 {
		t.Errorf("cripto deveria ser ~0,48x e 'slower': %s %.3f", c.CryptoLevel, c.CryptoRelative)
	}
	for _, want := range []string{"avx512ifma", "sha_ni", "vaes", "vpclmulqdq"} {
		found := false
		for _, l := range c.LostFeatures {
			found = found || l == want
		}
		if !found {
			t.Errorf("LostFeatures deveria incluir %s: %v", want, c.LostFeatures)
		}
	}
	if !strings.Contains(c.Note, "Criptografia (RSA/TLS)") || !strings.Contains(c.Note, "sha_ni") {
		t.Errorf("a nota precisa avisar sobre criptografia e citar o que falta: %q", c.Note)
	}

	// O caminho inverso (F → D) melhora a cripto e NÃO lista features perdidas.
	rev := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", by)
	if rev.CryptoLevel != "faster" || len(rev.LostFeatures) != 0 {
		t.Errorf("F→D: cripto 'faster' sem features perdidas, got %s %v", rev.CryptoLevel, rev.LostFeatures)
	}
}

func TestComparePerf_CriptoSemDadoNaoInventa(t *testing.T) {
	by := map[string]PerfInfo{
		"standard_f4s_v2": {Score: 100, Nodes: 1, Source: "measured"}, // medição antiga: sem RSA
		"standard_d4s_v4": {Score: 100, Nodes: 1, Source: "measured", RSAScore: 2000},
	}
	c := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", by)
	if c.CryptoRelative != 0 || c.CryptoLevel != "" || strings.Contains(c.Note, "Criptografia") {
		t.Errorf("sem RSA nos dois lados não há comparativo de cripto: %+v", c)
	}
}

func recAt(cluster, pool, sku string, score, rsa float64) storage.NodePerfBenchmark {
	return storage.NodePerfBenchmark{Cluster: cluster, NodePool: pool, NodeName: cluster + pool + sku, SKU: sku, CPUModel: "Xeon", PyScore: score, PySpread: 0.05, RSASignPerSec: rsa}
}

// O SKU não fixa o processador: o F4s_v2 do HLG (Ice Lake) não vale como medida do F4s_v2 do PRD.
func TestPerfSet_ForPool_PoolProprioPrevaleceSobreFrota(t *testing.T) {
	ps := BuildPerfSet([]storage.NodePerfBenchmark{
		recAt("hlg", "spot", "Standard_F4s_v2", 100, 1300), // outro cluster
		recAt("prd", "calc", "Standard_F4s_v2", 60, 700),   // o próprio pool (host mais antigo)
		recAt("hlg", "np", "Standard_D4s_v4", 100, 2600),
	})

	// Pool de PRD com medição própria: usa a DELE (60), com escopo "pool".
	m := ps.ForPool("prd", "calc")
	cur := m["standard_f4s_v2"]
	if cur.Score != 60 || cur.Scope != "pool" {
		t.Errorf("SKU atual deve vir da medição do próprio pool: %+v", cur)
	}
	if m["standard_d4s_v4"].Scope != "fleet" {
		t.Errorf("alternativa só existe medida em outros clusters → fleet: %+v", m["standard_d4s_v4"])
	}
	c := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", m)
	if c.CurrentScope != "pool" || strings.Contains(c.Note, "só em outros clusters") {
		t.Errorf("atual medido no pool: sem aviso de escopo: %+v", c)
	}
	if want := 100.0 / 60.0; math.Abs(c.Relative-want) > 1e-9 {
		t.Errorf("relativo deveria usar o F4s_v2 DO POOL (60), não o do HLG (100): %.3f want %.3f", c.Relative, want)
	}

	// Pool sem medição própria: cai na frota, e a nota avisa que o processador pode ser outro.
	m2 := ps.ForPool("prd", "outro-pool")
	// A frota agrega TODAS as medições do SKU (100 do HLG e 60 do PRD → mediana 80), com escopo fleet.
	if m2["standard_f4s_v2"].Score != 80 || m2["standard_f4s_v2"].Scope != "fleet" {
		t.Errorf("sem medição própria: frota com escopo fleet: %+v", m2["standard_f4s_v2"])
	}
	c2 := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", m2)
	if c2.CurrentScope != "fleet" || !strings.Contains(c2.Note, "só em outros clusters") || !strings.Contains(c2.Note, "meça este pool") {
		t.Errorf("SKU atual emprestado da frota precisa ser sinalizado: %+v", c2)
	}
}

func TestPerfSet_Vazio(t *testing.T) {
	var ps PerfSet // zero value (sem store): não pode dar panic nem inventar nada
	if m := ps.ForPool("c", "p"); len(m) != 0 {
		t.Errorf("conjunto vazio → mapa vazio, got %v", m)
	}
	if c := ComparePerf("Standard_F4s_v2", "Standard_D4s_v4", ps.ForPool("c", "p")); c.Level != "unknown" {
		t.Errorf("sem medição nenhuma → unknown: %+v", c)
	}
}
