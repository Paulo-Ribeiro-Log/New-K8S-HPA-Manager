package finops

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"k8s-hpa-manager/internal/storage"
)

// Medição de desempenho de CPU por SKU/node — responde "trocar F4s_v2 por D4s_v4 muda o tempo de
// resposta das APIs?" com dado do próprio ambiente.
//
// Por que medir em vez de tabelar: a documentação da Microsoft diz que o SKU NÃO fixa o processador.
// A série Fsv2 roda em Xeon Skylake 8168, Cascade Lake 8272CL, Ice Lake 8370C ou Emerald Rapids 8573C
// ("sizes may run on any of the listed processors"); a Dsv4 em Cascade Lake, Ice Lake, Sapphire
// Rapids 8473C ou Emerald Rapids — e as duas anunciam o mesmo turbo all-core de 3,4 GHz e
// hyper-threading. Ou seja, "F é mais rápida que D" pode ser verdade num host e falso noutro; só o
// "model name" real do node e um benchmark dizem o que cada pool entrega.

const (
	// PerfNoisySpread: acima disso ((max-min)/max entre amostras) a medição do node é tratada como
	// ruidosa (vizinho barulhento, throttling do host) e só entra na mediana se não houver nada melhor.
	PerfNoisySpread = 0.25

	// Faixas da comparação de desempenho por thread (alternativa ÷ atual).
	// ±5%: a própria medição varia 6–27% entre amostras num node (visto no HLG); uma faixa menor
	// rotularia ruído como diferença.
	perfEquivalentBand    = 0.05
	perfSlightlySlowerMin = 0.90
)

// PerfBenchScript roda dentro do pod de medição (imagem já usada pelas outras ferramentas de teste,
// com python3 e openssl). 5 amostras de ~1,2 s do mesmo loop de inteiros (proxy de código de
// aplicação genérico, single-thread; a estatística usa a MELHOR amostra e o espalhamento sinaliza
// ruído) + RSA-2048 do OpenSSL, melhor de 3 (proxy de terminação TLS). Não mede rede nem disco: é uma medida de
// CPU por thread, que é o que muda com o SKU/processador.
const PerfBenchScript = `
echo "@@CPU $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ *//')"
echo "@@NPROC $(nproc)"
echo "@@FEAT $(grep -m1 '^flags' /proc/cpuinfo | tr ' ' '\n' | grep -E '^(avx2|avx512f|avx512ifma|vaes|vpclmulqdq|sha_ni|adx)$' | sort | tr '\n' ' ')"
for i in 1 2 3 4 5; do
python3 - <<'PY'
import time
def work():
    n = 0
    for i in range(1, 400000):
        n += (i * i) % 7 + (i ^ (i >> 3))
    return n
t0 = time.perf_counter(); it = 0
while time.perf_counter() - t0 < 1.2:
    work(); it += 1
print("@@PY %.3f" % (it / (time.perf_counter() - t0)))
PY
done
for i in 1 2 3; do openssl speed -seconds 1 rsa2048 2>/dev/null | awk '/^rsa 2048/ {print "@@RSA " $6}'; done
`

// PerfBenchResult é a saída já interpretada do script de um node.
type PerfBenchResult struct {
	CPUModel      string
	VCPUs         int
	PyBest        float64 // melhor amostra
	PySpread      float64 // (max-min)/max
	PySamples     int
	RSASignPerSec float64
	// Features: extensões de CPU que a VM enxerga (subconjunto que importa pra criptografia/vetores).
	// O mesmo "model name" pode expor conjuntos diferentes conforme a série do SKU — foi assim que se
	// explicou o RSA 2x mais lento do F4s_v2 frente ao D4s_v4 no MESMO Xeon 8370C.
	Features []string
}

// ParsePerfBenchOutput interpreta as linhas "@@TAG valor" do script. Exige ao menos uma amostra de
// Python — sem ela não há o que comparar (ex: python3 ausente na imagem).
func ParsePerfBenchOutput(out string) (PerfBenchResult, error) {
	var r PerfBenchResult
	var py []float64
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "@@CPU "):
			r.CPUModel = strings.TrimSpace(strings.TrimPrefix(line, "@@CPU "))
		case strings.HasPrefix(line, "@@FEAT"):
			r.Features = strings.Fields(strings.TrimPrefix(line, "@@FEAT"))
		case strings.HasPrefix(line, "@@NPROC "):
			r.VCPUs, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "@@NPROC ")))
		case strings.HasPrefix(line, "@@PY "):
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "@@PY ")), 64); err == nil && v > 0 {
				py = append(py, v)
			}
		case strings.HasPrefix(line, "@@RSA "):
			// Melhor de N, como no Python: uma amostra só é refém de qualquer ruído do host.
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "@@RSA ")), 64); err == nil && v > r.RSASignPerSec {
				r.RSASignPerSec = v
			}
		}
	}
	if len(py) == 0 {
		return r, fmt.Errorf("benchmark não retornou nenhuma amostra (python3/openssl ausentes na imagem?)")
	}
	min, max := py[0], py[0]
	for _, v := range py {
		min, max = math.Min(min, v), math.Max(max, v)
	}
	r.PyBest, r.PySamples = max, len(py)
	if max > 0 {
		r.PySpread = (max - min) / max
	}
	return r, nil
}

// PerfInfo resume o desempenho medido de um SKU (mediana entre os nodes medidos).
type PerfInfo struct {
	Score     float64  `json:"score"` // mediana da melhor amostra por node (iterações/s, por thread)
	Nodes     int      `json:"nodes"` // nodes que embasaram a mediana
	CPUModels []string `json:"cpu_models"`
	// RSAScore: mediana de assinaturas RSA-2048/s (proxy de criptografia/TLS). 0 = não medido.
	RSAScore float64 `json:"rsa_score,omitempty"`
	// Features: extensões de CPU presentes em TODOS os nodes medidos do SKU (interseção — só o que é
	// garantido). Vazio quando nenhuma medição registrou (medições anteriores a este campo).
	Features []string `json:"features,omitempty"`
	// Noisy: TODAS as medições disponíveis foram ruidosas — o número existe mas é pouco confiável.
	Noisy bool `json:"noisy,omitempty"`
	// Source: "measured" (este SKU foi medido) | "same_series" (inferido de outro tamanho da mesma
	// série — mesma série = mesmos processadores por vCPU, segundo a documentação).
	Source string `json:"source"`
	// Scope: onde a medição foi feita, em relação ao pool sendo analisado — "pool" (nos nodes deste
	// próprio pool) ou "fleet" (em outros pools/clusters que usam o mesmo SKU). Importa porque o SKU
	// não fixa o processador: o mesmo F4s_v2 pode estar em Ice Lake no HLG e em Skylake no PRD.
	Scope string `json:"scope,omitempty"`
}

// PerfSet reúne as medições por SKU da frota inteira e por pool, pra resolver o SKU ATUAL de um pool
// com a medição dos nodes DELE quando existir (em vez de emprestar a de outro cluster).
type PerfSet struct {
	Fleet  map[string]PerfInfo            // chave: SKU em minúsculas
	ByPool map[string]map[string]PerfInfo // chave: cluster|pool → SKU em minúsculas
}

// BuildPerfSet agrega as medições da frota e de cada pool.
func BuildPerfSet(recs []storage.NodePerfBenchmark) PerfSet {
	byPoolRecs := map[string][]storage.NodePerfBenchmark{}
	for _, r := range recs {
		k := r.Cluster + "|" + r.NodePool
		byPoolRecs[k] = append(byPoolRecs[k], r)
	}
	ps := PerfSet{Fleet: BuildSKUPerf(recs), ByPool: make(map[string]map[string]PerfInfo, len(byPoolRecs))}
	for k, rs := range byPoolRecs {
		ps.ByPool[k] = BuildSKUPerf(rs)
	}
	return ps
}

// ForPool devolve o mapa por SKU a usar ao comparar as alternativas deste pool: a frota inteira
// (Scope "fleet"), com o SKU do próprio pool sobrescrito pela medição dos nodes DELE (Scope "pool")
// quando existir. Só as alternativas (SKUs que o pool não roda) dependem sempre da frota.
func (ps PerfSet) ForPool(cluster, pool string) map[string]PerfInfo {
	out := make(map[string]PerfInfo, len(ps.Fleet))
	for k, v := range ps.Fleet {
		v.Scope = "fleet"
		out[k] = v
	}
	for k, v := range ps.ByPool[cluster+"|"+pool] {
		v.Scope = "pool"
		out[k] = v
	}
	return out
}

// SeriesKey identifica a série do SKU Azure ignorando o tamanho: Standard_F4s_v2 e Standard_F8s_v2
// → "f|s_v2". Case-insensitive (o registro tem "Standard_f4s_v2" e "Standard_F4s_v2"). Vazio
// quando o nome não segue o padrão (SKUs de outras clouds, tamanhos "constrained vCPU").
var azureSeriesRe = regexp.MustCompile(`^standard_([a-z]+)\d+([a-z0-9]*_v\d+)$`)

func SeriesKey(sku string) string {
	m := azureSeriesRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(sku)))
	if m == nil {
		return ""
	}
	return m[1] + "|" + m[2]
}

func median(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// BuildSKUPerf agrega as medições (frota inteira) por SKU. Chave: SKU em minúsculas. Por SKU, usa
// só as medições não-ruidosas quando existirem; se todas forem ruidosas, usa todas e marca Noisy.
func BuildSKUPerf(recs []storage.NodePerfBenchmark) map[string]PerfInfo {
	type acc struct {
		good, all []float64
		rsa       []float64
		feats     map[string]int // feature -> nº de nodes que a têm
		featNodes int            // nodes com registro de features
		models    map[string]bool
	}
	by := map[string]*acc{}
	for _, r := range recs {
		key := strings.ToLower(strings.TrimSpace(r.SKU))
		if key == "" || r.PyScore <= 0 {
			continue
		}
		a := by[key]
		if a == nil {
			a = &acc{models: map[string]bool{}, feats: map[string]int{}}
			by[key] = a
		}
		a.all = append(a.all, r.PyScore)
		if r.PySpread <= PerfNoisySpread {
			a.good = append(a.good, r.PyScore)
		}
		if r.CPUModel != "" {
			a.models[r.CPUModel] = true
		}
		if r.RSASignPerSec > 0 {
			a.rsa = append(a.rsa, r.RSASignPerSec)
		}
		if fs := strings.Fields(r.CPUFeatures); len(fs) > 0 {
			a.featNodes++
			for _, f := range fs {
				a.feats[f]++
			}
		}
	}
	out := make(map[string]PerfInfo, len(by))
	for key, a := range by {
		used, noisy := a.good, false
		if len(used) == 0 {
			used, noisy = a.all, true
		}
		models := make([]string, 0, len(a.models))
		for m := range a.models {
			models = append(models, m)
		}
		sort.Strings(models)
		var feats []string
		for f, n := range a.feats {
			if n == a.featNodes { // interseção: só o que TODOS os nodes medidos têm
				feats = append(feats, f)
			}
		}
		sort.Strings(feats)
		out[key] = PerfInfo{Score: median(used), Nodes: len(used), CPUModels: models, Noisy: noisy, Source: "measured",
			RSAScore: median(a.rsa), Features: feats}
	}
	return out
}

// LookupPerf devolve o desempenho do SKU: medido diretamente, ou inferido de outro tamanho da mesma
// série (por thread o desempenho é o mesmo dentro da série). ok=false quando não há nada.
func LookupPerf(sku string, bySKU map[string]PerfInfo) (PerfInfo, bool) {
	if p, ok := bySKU[strings.ToLower(strings.TrimSpace(sku))]; ok {
		return p, true
	}
	series := SeriesKey(sku)
	if series == "" {
		return PerfInfo{}, false
	}
	var scores, rsas []float64
	var featSets [][]string
	allPool := true
	models := map[string]bool{}
	nodes, allNoisy := 0, true
	for other, p := range bySKU {
		if SeriesKey(other) != series {
			continue
		}
		scores = append(scores, p.Score)
		if p.RSAScore > 0 {
			rsas = append(rsas, p.RSAScore)
		}
		featSets = append(featSets, p.Features)
		allPool = allPool && p.Scope == "pool"
		nodes += p.Nodes
		allNoisy = allNoisy && p.Noisy
		for _, m := range p.CPUModels {
			models[m] = true
		}
	}
	if len(scores) == 0 {
		return PerfInfo{}, false
	}
	ms := make([]string, 0, len(models))
	for m := range models {
		ms = append(ms, m)
	}
	sort.Strings(ms)
	scope := "fleet"
	if allPool {
		scope = "pool"
	}
	return PerfInfo{Score: median(scores), Nodes: nodes, CPUModels: ms, Noisy: allNoisy, Source: "same_series",
		Scope: scope, RSAScore: median(rsas), Features: intersectStrings(featSets)}, true
}

func intersectStrings(sets [][]string) []string {
	if len(sets) == 0 {
		return nil
	}
	count := map[string]int{}
	for _, set := range sets {
		for _, f := range set {
			count[f]++
		}
	}
	var out []string
	for f, n := range count {
		if n == len(sets) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// missingFeatures: o que o SKU atual enxerga e a alternativa não (ordem estável).
func missingFeatures(cur, alt []string) []string {
	has := map[string]bool{}
	for _, f := range alt {
		has[f] = true
	}
	var out []string
	for _, f := range cur {
		if !has[f] {
			out = append(out, f)
		}
	}
	return out
}

// perfCryptoBand: diferença de RSA abaixo disso não vira aviso (ruído de medição).
const perfCryptoBand = 0.15

// PerfComparison é a comparação de desempenho por thread entre o SKU atual do pool e uma alternativa.
type PerfComparison struct {
	Relative  float64  `json:"relative,omitempty"` // alternativa ÷ atual (1,00 = igual; <1 = mais lenta)
	Level     string   `json:"level"`              // faster | equivalent | slightly_slower | slower | unknown
	Source    string   `json:"source"`             // measured | same_series | unknown
	CPUModels []string `json:"cpu_models,omitempty"`
	Noisy     bool     `json:"noisy,omitempty"`
	// CurrentScope: "pool" quando o SKU ATUAL foi medido nos nodes deste pool; "fleet" quando o
	// número vem de outros clusters (o processador do pool pode ser outro).
	CurrentScope string `json:"current_scope,omitempty"`
	// CryptoRelative: RSA-2048/s da alternativa ÷ do SKU atual (0 = sem dado dos dois lados). Separado
	// de Relative de propósito: o mesmo processador pode render igual em código genérico e 2x diferente
	// em criptografia, conforme as extensões (SHA-NI, VAES, AVX-512 IFMA) que a série expõe à VM.
	CryptoRelative float64 `json:"crypto_relative,omitempty"`
	// CryptoLevel: faster | equivalent | slower ("" = sem dado).
	CryptoLevel string `json:"crypto_level,omitempty"`
	// LostFeatures: extensões que o SKU atual tem e a alternativa NÃO expõe.
	LostFeatures []string `json:"lost_features,omitempty"`
	Note         string   `json:"note"`
}

const perfDisclaimer = " Mede só CPU por thread; o tempo de resposta real também depende de I/O, rede e throttling por limit."

// ComparePerf compara a alternativa com o SKU atual. nil quando é o mesmo SKU. Nunca inventa um
// número: sem medição de um dos lados devolve Level "unknown" explicando o que falta medir.
func ComparePerf(currentSKU, altSKU string, bySKU map[string]PerfInfo) *PerfComparison {
	if strings.EqualFold(strings.TrimSpace(currentSKU), strings.TrimSpace(altSKU)) {
		return nil
	}
	cur, curOK := LookupPerf(currentSKU, bySKU)
	alt, altOK := LookupPerf(altSKU, bySKU)
	if !curOK || !altOK {
		missing := altSKU
		if !curOK && !altOK {
			missing = currentSKU + " e " + altSKU
		} else if !curOK {
			missing = currentSKU
		}
		return &PerfComparison{Level: "unknown", Source: "unknown",
			Note: "Sem medição de desempenho para " + missing + " — use \"Medir desempenho de CPU\" num pool que use esse SKU."}
	}

	c := &PerfComparison{Relative: alt.Score / cur.Score, CPUModels: alt.CPUModels, Noisy: cur.Noisy || alt.Noisy, Source: "measured", CurrentScope: cur.Scope}
	if cur.Source != "measured" || alt.Source != "measured" {
		c.Source = "same_series"
	}
	pct := math.Abs(c.Relative-1) * 100
	switch {
	case c.Relative >= 1+perfEquivalentBand:
		c.Level, c.Note = "faster", fmt.Sprintf("≈ %.0f%% mais rápido por thread que o SKU atual.", pct)
	case c.Relative >= 1-perfEquivalentBand:
		c.Level, c.Note = "equivalent", "Desempenho por thread equivalente ao SKU atual (diferença dentro de ±5%, a margem de ruído da medição)."
	case c.Relative >= perfSlightlySlowerMin:
		c.Level, c.Note = "slightly_slower", fmt.Sprintf("≈ %.0f%% mais lento por thread que o SKU atual.", pct)
	default:
		c.Level = "slower"
		c.Note = fmt.Sprintf("≈ %.0f%% mais lento por thread — APIs limitadas por CPU tendem a gastar ~%.0f%% mais tempo de CPU por requisição.", pct, (1/c.Relative-1)*100)
	}
	if c.Source == "same_series" {
		c.Note += " (inferido de outro tamanho da mesma série)"
	}
	if cur.RSAScore > 0 && alt.RSAScore > 0 {
		c.CryptoRelative = alt.RSAScore / cur.RSAScore
		switch {
		case c.CryptoRelative > 1+perfCryptoBand:
			c.CryptoLevel = "faster"
		case c.CryptoRelative < 1-perfCryptoBand:
			c.CryptoLevel = "slower"
		default:
			c.CryptoLevel = "equivalent"
		}
		c.LostFeatures = missingFeatures(cur.Features, alt.Features)
		if c.CryptoLevel != "equivalent" {
			c.Note += fmt.Sprintf(" Criptografia (RSA/TLS): %.1f× o SKU atual", c.CryptoRelative)
			if c.CryptoLevel == "slower" && len(c.LostFeatures) > 0 {
				c.Note += " — sem " + strings.Join(c.LostFeatures, ", ")
			}
			c.Note += "."
		}
	}
	if cur.Scope == "fleet" {
		c.Note += " O SKU atual foi medido só em outros clusters, não neste pool — o processador aqui pode ser outro; meça este pool."
	}
	if c.Noisy {
		c.Note += " Medição ruidosa — repita."
	}
	c.Note += perfDisclaimer
	return c
}
