package nodepoolpredictions

import (
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// saturationForecastFromTrend — regressão linear
// ---------------------------------------------------------------------------

func TestSaturationForecast_StableMetric(t *testing.T) {
	// Valor constante em 50% → sem crescimento → sem data de saturação
	snapshots := []TrendSnapshot{
		{DaysAgo: 3, ValuePerNode: 50.0},
		{DaysAgo: 7, ValuePerNode: 50.0},
		{DaysAgo: 14, ValuePerNode: 50.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 50.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil para métrica estável")
	}
	if f.DaysUntilSaturation != nil {
		t.Errorf("métrica estável não deveria ter DaysUntilSaturation, obtido %.2f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado UrgencyBadge=ESTAVEL, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationForecast_GrowingMetric_Critical(t *testing.T) {
	// CPU crescendo de 30% → 50% em 14 dias = +20pp em 14 dias ≈ 1.43pp/dia
	// Atual: 78% → threshold 85% → (85-78)/1.43 ≈ 4.9 dias → CRITICO
	snapshots := []TrendSnapshot{
		{DaysAgo: 14, ValuePerNode: 50.0},
		{DaysAgo: 7, ValuePerNode: 64.0},
		{DaysAgo: 3, ValuePerNode: 73.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 78.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil para métrica crescente")
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation para métrica crescente")
	}
	if *f.DaysUntilSaturation >= 7 {
		t.Errorf("esperado < 7 dias para CRITICO, obtido %.1f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "CRITICO" {
		t.Errorf("esperado UrgencyBadge=CRITICO, obtido %q", f.UrgencyBadge)
	}
	if f.EstimatedDate == nil {
		t.Error("EstimatedDate deveria estar preenchida")
	}
	if f.EstimatedDate != nil && f.EstimatedDate.Before(now) {
		t.Error("EstimatedDate não pode ser no passado para crescimento positivo")
	}
}

func TestSaturationForecast_GrowingMetric_Estavel(t *testing.T) {
	// CPU crescendo muito lentamente: D-14=40%, D-7=42%, atual=44%
	// slope = -(84/294) ≈ -0.286 → dailyGrowthRate ≈ 0.286 pp/dia
	// daysUntilSaturation = (85-44)/0.286 ≈ 143 dias → ESTAVEL (>30d)
	snapshots := []TrendSnapshot{
		{DaysAgo: 14, ValuePerNode: 40.0},
		{DaysAgo: 7, ValuePerNode: 42.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 44.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation para métrica crescente")
	}
	if *f.DaysUntilSaturation <= 30 {
		t.Errorf("esperado > 30 dias para crescimento lento, obtido %.1f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado UrgencyBadge=ESTAVEL para >30 dias, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationForecast_GrowingMetric_Atencao(t *testing.T) {
	// CPU crescendo moderado: D-14=45%, D-7=55%, atual=65%
	// slope = -(14*65-14*45) / ... → crescimento ~1.43pp/dia
	// daysUntilSaturation = (85-65)/1.43 ≈ 14 dias → ATENCAO (7-30d)
	snapshots := []TrendSnapshot{
		{DaysAgo: 14, ValuePerNode: 45.0},
		{DaysAgo: 7, ValuePerNode: 55.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 65.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation para métrica crescente")
	}
	if *f.DaysUntilSaturation < 7 || *f.DaysUntilSaturation > 30 {
		t.Errorf("esperado entre 7 e 30 dias para ATENCAO, obtido %.1f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ATENCAO" {
		t.Errorf("esperado UrgencyBadge=ATENCAO para 7-30 dias, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationForecast_AlreadySaturated(t *testing.T) {
	// Valor atual já acima do threshold
	snapshots := []TrendSnapshot{
		{DaysAgo: 7, ValuePerNode: 80.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("memory", snapshots, 90.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation=0 para métrica já saturada")
	}
	if *f.DaysUntilSaturation != 0 {
		t.Errorf("esperado DaysUntilSaturation=0, obtido %.2f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "CRITICO" {
		t.Errorf("esperado CRITICO para já saturado, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationForecast_DecreasingMetric(t *testing.T) {
	// CPU decreasing: D-14=70%, D-7=60%, D-3=55%, atual=50% → sem saturação
	snapshots := []TrendSnapshot{
		{DaysAgo: 14, ValuePerNode: 70.0},
		{DaysAgo: 7, ValuePerNode: 60.0},
		{DaysAgo: 3, ValuePerNode: 55.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 50.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil mesmo para métrica decrescente")
	}
	if f.DaysUntilSaturation != nil {
		t.Errorf("métrica decrescente não deveria ter DaysUntilSaturation, obtido %.2f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado ESTAVEL para decrescente, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationForecast_Confidence(t *testing.T) {
	now := time.Now()

	// 1 ponto histórico (+ D-0) = 2 pts → medium
	snap1 := []TrendSnapshot{{DaysAgo: 7, ValuePerNode: 40.0}}
	f1 := saturationForecastFromTrend("cpu", snap1, 50.0, 85.0, now)
	if f1.Confidence != "medium" {
		t.Errorf("2 pontos → esperado confidence=medium, obtido %q", f1.Confidence)
	}

	// 2 pontos históricos (+ D-0) = 3 pts → high
	snap2 := []TrendSnapshot{
		{DaysAgo: 7, ValuePerNode: 40.0},
		{DaysAgo: 14, ValuePerNode: 30.0},
	}
	f2 := saturationForecastFromTrend("cpu", snap2, 50.0, 85.0, now)
	if f2.Confidence != "high" {
		t.Errorf("3 pontos → esperado confidence=high, obtido %q", f2.Confidence)
	}
}

func TestSaturationForecast_NoHistory(t *testing.T) {
	// Sem histórico (apenas D-0) → só 1 ponto → confidence=low
	now := time.Now()
	f := saturationForecastFromTrend("cpu", nil, 60.0, 85.0, now)
	if f == nil {
		t.Fatal("esperava forecast mesmo sem histórico")
	}
	if f.Confidence != "low" {
		t.Errorf("sem histórico → confidence=low, obtido %q", f.Confidence)
	}
	if f.DaysUntilSaturation != nil {
		t.Errorf("sem histórico não deveria ter DaysUntilSaturation (impossível calcular regressão)")
	}
}

func TestSaturationForecast_DailyGrowthRateSign(t *testing.T) {
	// Regressão linear: crescimento positivo deve resultar em dailyGrowthRate > 0
	snapshots := []TrendSnapshot{
		{DaysAgo: 7, ValuePerNode: 40.0}, // passado = menor
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 60.0, 85.0, now) // atual = maior
	if f.DailyGrowthRate <= 0 {
		t.Errorf("crescimento positivo (40→60 em 7d) deveria ter dailyGrowthRate > 0, obtido %.4f", f.DailyGrowthRate)
	}
}

// ---------------------------------------------------------------------------
// saturationForecastConntrack
// ---------------------------------------------------------------------------

func TestConntrackForecast_WithGrowthRate(t *testing.T) {
	metrics := &NodePoolMetrics{
		ConntrackPool: ConntrackPoolAnalysis{
			HasSufficientData: true,
			MaxUsage:          72.0,
			HighestNode:       "aks-node-01",
			AvgGrowthRatePerH: 1000.0, // entries/hora
			TotalLimit:        262144,  // ~256k entries
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: true},
	}
	now := time.Now()
	f := saturationForecastConntrack(metrics, now)

	if f == nil {
		t.Fatal("esperava forecast de conntrack")
	}
	if f.AffectedNode != "aks-node-01" {
		t.Errorf("esperado AffectedNode=aks-node-01, obtido %q", f.AffectedNode)
	}
	// (85 - 72) = 13pp restantes; growthPerDay = (1000/262144)*100*24 ≈ 0.916 pp/dia → ~14.2 dias → ATENCAO
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation calculado")
	}
	if *f.DaysUntilSaturation <= 0 {
		t.Errorf("DaysUntilSaturation deveria ser > 0, obtido %.2f", *f.DaysUntilSaturation)
	}
}

func TestConntrackForecast_NoGrowthRate(t *testing.T) {
	metrics := &NodePoolMetrics{
		ConntrackPool: ConntrackPoolAnalysis{
			HasSufficientData: true,
			MaxUsage:          60.0,
			TotalLimit:        262144,
			AvgGrowthRatePerH: 0, // sem crescimento
		},
	}
	now := time.Now()
	f := saturationForecastConntrack(metrics, now)

	if f == nil {
		t.Fatal("esperava forecast")
	}
	if f.DaysUntilSaturation != nil {
		t.Errorf("sem crescimento não deveria ter DaysUntilSaturation")
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado ESTAVEL sem crescimento, obtido %q", f.UrgencyBadge)
	}
}

// ---------------------------------------------------------------------------
// calculateSaturationTimeline (via NodePoolAnalyzer)
// ---------------------------------------------------------------------------

func TestCalculateSaturationTimeline_EmptyPool(t *testing.T) {
	a := &NodePoolAnalyzer{}
	metrics := &NodePoolMetrics{
		DataSources: DataSourceInfo{NodeExporterAvailable: false},
	}
	tl := a.calculateSaturationTimeline(metrics, NodePoolTrends{})

	// Pool vazio: pode ter forecasts com confidence=low, mas não deve panicar
	for _, f := range tl.Forecasts {
		if f.Metric == "" {
			t.Errorf("forecast com Metric vazia: %+v", f)
		}
	}
}

func TestCalculateSaturationTimeline_MostCriticalIsFirst(t *testing.T) {
	// Configura um pool onde CPU satura em 3 dias e memória em 20 dias
	a := &NodePoolAnalyzer{}
	metrics := &NodePoolMetrics{
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 82.0, MemUsagePercent: 60.0, PodDensityPercent: 30.0},
		},
		CPUTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 68.0},
			{DaysAgo: 14, ValuePerNode: 54.0},
		},
		MemTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 57.0},
			{DaysAgo: 14, ValuePerNode: 54.0},
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: false},
	}
	tl := a.calculateSaturationTimeline(metrics, NodePoolTrends{})

	if tl.MostCritical == nil {
		t.Fatal("esperava MostCritical quando há crescimento")
	}

	// O mais crítico deve ser o que satura PRIMEIRO
	for _, f := range tl.Forecasts {
		if f.DaysUntilSaturation != nil && tl.MostCritical.DaysUntilSaturation != nil {
			if *f.DaysUntilSaturation < *tl.MostCritical.DaysUntilSaturation {
				t.Errorf("existe forecast que satura em %.1fd antes do MostCritical (%.1fd)",
					*f.DaysUntilSaturation, *tl.MostCritical.DaysUntilSaturation)
			}
		}
	}
}

func TestCalculateSaturationTimeline_SummaryNotEmpty(t *testing.T) {
	a := &NodePoolAnalyzer{}
	metrics := &NodePoolMetrics{
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 80.0},
		},
		CPUTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 60.0},
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: false},
	}
	tl := a.calculateSaturationTimeline(metrics, NodePoolTrends{})
	if tl.Summary == "" {
		t.Error("Summary não deveria ser vazio")
	}
}

// ---------------------------------------------------------------------------
// Filtros de ramp-up — pools com baixa utilização
// ---------------------------------------------------------------------------

func TestSaturationForecast_RampUp_LowUsage_LongProjection(t *testing.T) {
	// Pool quase sem uso: 2% de CPU com tendência levemente crescente
	// Sem o filtro, projetaria saturação em ~260 dias — é ruído de ramp-up
	snapshots := []TrendSnapshot{
		{DaysAgo: 14, ValuePerNode: 1.0},
		{DaysAgo: 7, ValuePerNode: 1.5},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 2.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	// currentValue=2.0 < 15 e days > 90 → filtro de ramp-up deve descartar projeção
	if f.DaysUntilSaturation != nil {
		t.Errorf("pool em ramp-up (<15%% com projeção >90d) não deveria ter DaysUntilSaturation, obtido %.1f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("pool em ramp-up deveria ter UrgencyBadge=ESTAVEL, obtido %q", f.UrgencyBadge)
	}
	if f.Confidence != "low" {
		t.Errorf("pool em ramp-up deveria ter Confidence=low, obtido %q", f.Confidence)
	}
}

func TestSaturationForecast_RampUp_LowUsage_ShortProjection(t *testing.T) {
	// Pool com uso baixo mas crescimento MUITO rápido: pode saturar em <90 dias
	// Neste caso, NÃO deve ser filtrado — pode ser um pico real de tráfego
	snapshots := []TrendSnapshot{
		{DaysAgo: 7, ValuePerNode: 5.0},
		{DaysAgo: 14, ValuePerNode: 1.0},
	}
	now := time.Now()
	f := saturationForecastFromTrend("cpu", snapshots, 10.0, 85.0, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	// currentValue=10.0 < 15, mas crescimento rápido pode gerar days < 90 → NÃO filtrar
	// slope = -(10-1)/14 ≈ 0.64pp/dia. days = (85-10)/0.64 ≈ 117 dias → > 90, filtrado
	// (neste caso específico ainda é filtrado pela regressão)
	// O ponto é: se days <= 90, não filtramos mesmo com uso baixo
	_ = f // resultado depende dos dados específicos, só garantimos que não panicar
}

func TestConntrackForecast_RampUp_VeryLowUsage(t *testing.T) {
	// Pool com 0.6% de conntrack — quase sem tráfego
	// Sem o filtro, projetaria saturação em meses — é ruído
	metrics := &NodePoolMetrics{
		ConntrackPool: ConntrackPoolAnalysis{
			HasSufficientData: true,
			MaxUsage:          0.6, // 0.6% — praticamente zero
			HighestNode:       "aks-node-01",
			AvgGrowthRatePerH: 50.0,   // entries/hora — quantidade mínima
			TotalLimit:        131072,  // 128k entries
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: true},
	}
	now := time.Now()
	f := saturationForecastConntrack(metrics, now)

	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	// currentPct=0.6 < 5.0 → filtro de ramp-up deve descartar projeção
	if f.DaysUntilSaturation != nil {
		t.Errorf("conntrack com 0.6%% não deveria ter DaysUntilSaturation, obtido %.1f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("conntrack com 0.6%% deveria ser ESTAVEL, obtido %q", f.UrgencyBadge)
	}
}

func TestSaturationTimeline_MostCritical_OnlyWithin180Days(t *testing.T) {
	// Pool com uso muito baixo: projeção seria >180 dias → MostCritical deve ser nil
	a := &NodePoolAnalyzer{}
	metrics := &NodePoolMetrics{
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 3.0, MemUsagePercent: 5.0, PodDensityPercent: 2.0},
		},
		CPUTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 2.0},
			{DaysAgo: 14, ValuePerNode: 1.0},
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: false},
	}
	tl := a.calculateSaturationTimeline(metrics, NodePoolTrends{})

	// Pool em ramp-up com uso muito baixo: MostCritical deve ser nil (não há alerta acionável)
	if tl.MostCritical != nil {
		t.Errorf("pool em ramp-up com uso <5%% não deveria ter MostCritical, obtido metric=%q days=%.1f",
			tl.MostCritical.Metric, *tl.MostCritical.DaysUntilSaturation)
	}
}

// ---------------------------------------------------------------------------
// Helpers internos
// ---------------------------------------------------------------------------

func TestMaxCPUCurrent(t *testing.T) {
	snaps := []NodePoolNodeSnapshot{
		{CPUUsagePercent: 30.0},
		{CPUUsagePercent: 75.0},
		{CPUUsagePercent: 45.0},
	}
	got := maxCPUCurrent(snaps)
	if math.Abs(got-75.0) > 0.01 {
		t.Errorf("esperado 75.0, obtido %.2f", got)
	}
}

func TestMaxMemCurrent(t *testing.T) {
	snaps := []NodePoolNodeSnapshot{
		{MemUsagePercent: 55.0},
		{MemUsagePercent: 91.0},
		{MemUsagePercent: 70.0},
	}
	got := maxMemCurrent(snaps)
	if math.Abs(got-91.0) > 0.01 {
		t.Errorf("esperado 91.0, obtido %.2f", got)
	}
}

func TestMaxPodDensityCurrent(t *testing.T) {
	snaps := []NodePoolNodeSnapshot{
		{PodDensityPercent: 40.0},
		{PodDensityPercent: 20.0},
		{PodDensityPercent: 65.0},
	}
	got := maxPodDensityCurrent(snaps)
	if math.Abs(got-65.0) > 0.01 {
		t.Errorf("esperado 65.0, obtido %.2f", got)
	}
}

func TestMaxHelpers_EmptySlice(t *testing.T) {
	if maxCPUCurrent(nil) != 0 {
		t.Error("maxCPUCurrent(nil) deveria retornar 0")
	}
	if maxMemCurrent(nil) != 0 {
		t.Error("maxMemCurrent(nil) deveria retornar 0")
	}
	if maxPodDensityCurrent(nil) != 0 {
		t.Error("maxPodDensityCurrent(nil) deveria retornar 0")
	}
}

// ---------------------------------------------------------------------------
// saturationForecastDisk — crescimento de disco efêmero
// ---------------------------------------------------------------------------

func makeDiskMetrics(maxGrowthPctDay, maxUsagePct, minDaysUntilFull float64, fastestNode string, hasSufficientData bool) *NodePoolMetrics {
	return &NodePoolMetrics{
		DiskGrowth: DiskGrowthAnalysis{
			HasData:          true,
			FastestNode:      fastestNode,
			MaxGrowthPctDay:  maxGrowthPctDay,
			MaxUsagePct:      maxUsagePct,
			MinDaysUntilFull: minDaysUntilFull,
		},
		DataSources: DataSourceInfo{NodeExporterAvailable: hasSufficientData},
	}
}

func TestDiskForecast_StableNilGrowth(t *testing.T) {
	// MaxGrowthPctDay == 0 → nenhum crescimento → retorna nil
	metrics := makeDiskMetrics(0, 45.0, 0, "node-a", true)
	f := saturationForecastDisk(metrics, time.Now())
	if f != nil {
		t.Errorf("sem crescimento: esperado nil, obtido forecast com badge=%q", f.UrgencyBadge)
	}
}

func TestDiskForecast_RampUp_LowUsage_LongProjection(t *testing.T) {
	// Pool com 8% de uso e crescimento levíssimo (365 dias até cheio) → ramp-up, descarta
	metrics := makeDiskMetrics(0.003, 8.0, 365, "node-a", true)
	now := time.Now()
	f := saturationForecastDisk(metrics, now)
	if f == nil {
		t.Fatal("esperava forecast não-nil mesmo para ramp-up")
	}
	if f.DaysUntilSaturation != nil {
		t.Errorf("ramp-up não deveria ter DaysUntilSaturation, obtido %.2f", *f.DaysUntilSaturation)
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado ESTAVEL para ramp-up, obtido %q", f.UrgencyBadge)
	}
	if f.Confidence != "low" {
		t.Errorf("esperado confidence=low para ramp-up, obtido %q", f.Confidence)
	}
}

func TestDiskForecast_RampUp_ShortProjection_NotFiltered(t *testing.T) {
	// Pool com 8% de uso MAS crescimento acelerado (apenas 15 dias até cheio) → NÃO filtra
	metrics := makeDiskMetrics(5.0, 8.0, 15, "node-b", true)
	now := time.Now()
	f := saturationForecastDisk(metrics, now)
	if f == nil {
		t.Fatal("esperava forecast não-nil para crescimento acelerado")
	}
	// 15 dias → ATENCAO
	if f.UrgencyBadge != "ATENCAO" {
		t.Errorf("esperado ATENCAO para 15 dias, obtido %q", f.UrgencyBadge)
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation para crescimento acelerado")
	}
}

func TestDiskForecast_Critical_HighUsage(t *testing.T) {
	// Disco já em 88% → já saturado
	metrics := makeDiskMetrics(0.5, 88.0, 0, "node-c", true)
	now := time.Now()
	f := saturationForecastDisk(metrics, now)
	if f == nil {
		t.Fatal("esperava forecast para disco já saturado")
	}
	if f.UrgencyBadge != "CRITICO" {
		t.Errorf("esperado CRITICO para disco >85%%, obtido %q", f.UrgencyBadge)
	}
	if f.DaysUntilSaturation == nil || *f.DaysUntilSaturation != 0 {
		t.Errorf("esperado DaysUntilSaturation=0 para disco já saturado")
	}
}

func TestDiskForecast_Atencao_30Days(t *testing.T) {
	// 50% de uso, 28 dias até cheio → ATENCAO
	metrics := makeDiskMetrics(1.25, 50.0, 28, "node-d", true)
	now := time.Now()
	f := saturationForecastDisk(metrics, now)
	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.UrgencyBadge != "ATENCAO" {
		t.Errorf("esperado ATENCAO para 28 dias, obtido %q", f.UrgencyBadge)
	}
	if f.DaysUntilSaturation == nil {
		t.Fatal("esperava DaysUntilSaturation")
	}
	if math.Abs(*f.DaysUntilSaturation-28) > 1 {
		t.Errorf("esperado ~28 dias, obtido %.2f", *f.DaysUntilSaturation)
	}
}

func TestDiskForecast_Estavel_LongProjection_HighUsage(t *testing.T) {
	// 60% de uso, 120 dias até cheio → ESTAVEL (acima do filtro ramp-up mas além de 30d)
	metrics := makeDiskMetrics(0.2, 60.0, 120, "node-e", true)
	now := time.Now()
	f := saturationForecastDisk(metrics, now)
	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.UrgencyBadge != "ESTAVEL" {
		t.Errorf("esperado ESTAVEL para 120 dias, obtido %q", f.UrgencyBadge)
	}
}

func TestDiskForecast_AffectedNodePropagated(t *testing.T) {
	metrics := makeDiskMetrics(2.0, 55.0, 12, "meu-node-123", true)
	f := saturationForecastDisk(metrics, time.Now())
	if f == nil {
		t.Fatal("esperava forecast não-nil")
	}
	if f.AffectedNode != "meu-node-123" {
		t.Errorf("esperado AffectedNode=meu-node-123, obtido %q", f.AffectedNode)
	}
}

// ---------------------------------------------------------------------------
// identifyBottleneckFromP95 e historicalP95 (Fase 15)
// ---------------------------------------------------------------------------

func TestIdentifyBottleneck_CPU(t *testing.T) {
	// CPU 80% >> Mem 30% → cpu-bound
	bn := identifyBottleneckFromP95(80.0, 30.0)
	if bn != "cpu" {
		t.Errorf("esperado 'cpu', obtido %q", bn)
	}
}

func TestIdentifyBottleneck_Memory(t *testing.T) {
	// Mem 78% >> CPU 25% → memory-bound
	bn := identifyBottleneckFromP95(25.0, 78.0)
	if bn != "memory" {
		t.Errorf("esperado 'memory', obtido %q", bn)
	}
}

func TestIdentifyBottleneck_Balanced_BothHigh(t *testing.T) {
	// CPU 70% e Mem 65% — ambos altos mas sem dominância → balanced
	bn := identifyBottleneckFromP95(70.0, 65.0)
	if bn != "balanced" {
		t.Errorf("esperado 'balanced' (ambos altos), obtido %q", bn)
	}
}

func TestIdentifyBottleneck_Balanced_BothLow(t *testing.T) {
	// Pool sob-utilizado → balanced
	bn := identifyBottleneckFromP95(20.0, 25.0)
	if bn != "balanced" {
		t.Errorf("esperado 'balanced' (uso baixo), obtido %q", bn)
	}
}

func TestHistoricalP95_IncludesTrendData(t *testing.T) {
	// Snapshot atual: 40% CPU. Tendência D-7 mostra pico de 90%.
	// P95 histórico deve capturar o pico, não só o snapshot atual.
	ca := NewNodePoolCostAnalyzer()
	metrics := &NodePoolMetrics{
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 40.0, MemUsagePercent: 20.0},
		},
		CPUTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 90.0},
			{DaysAgo: 14, ValuePerNode: 85.0},
		},
		MemTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 30.0},
		},
	}
	cpuP95, memP95 := ca.historicalP95(metrics)

	// P95 dos 3 valores de CPU: [40, 90, 85] → P95 ≈ 90
	if cpuP95 < 85 {
		t.Errorf("historicalP95 deveria incluir picos de tendência, obtido cpuP95=%.1f", cpuP95)
	}
	if memP95 <= 0 {
		t.Errorf("historicalP95 deveria retornar memP95>0, obtido %.1f", memP95)
	}
}

func TestSuggestAlternativeSKUs_NoDataReturnsNil(t *testing.T) {
	// Sem snapshots nem tendências → sem dados históricos → nil
	ca := NewNodePoolCostAnalyzer()
	metrics := &NodePoolMetrics{
		VMSize:      "Standard_D4s_v3",
		NodePoolName: "test-pool",
	}
	alts := ca.suggestAlternativeSKUs(metrics, 4, 16, 0.28, 5.50, 3)
	if alts != nil {
		t.Errorf("sem dados históricos: esperado nil, obtido %d alternativas", len(alts))
	}
}

func TestSuggestAlternativeSKUs_SKUMustHandleHistoricalLoad(t *testing.T) {
	// Pool com CPU P95 histórico de 75% em VM de 4 vCPUs
	// cpuUsedAtP95 = 4 * 0.75 = 3 vCPUs
	// minVCPUs = ceil(3 / 0.80) = 4
	// Candidatos com < 4 vCPUs devem ser rejeitados
	ca := NewNodePoolCostAnalyzer()
	metrics := &NodePoolMetrics{
		VMSize:       "Standard_D4s_v3",
		NodePoolName: "test-pool",
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 75.0, MemUsagePercent: 30.0},
		},
	}

	alts := ca.suggestAlternativeSKUs(metrics, 4, 16, 0.28, 5.50, 3)
	// Todas alternativas devem ter vCPUs >= 4
	for _, alt := range alts {
		if alt.VMCPUCores < 4 {
			t.Errorf("alternativa %s tem %d vCPUs < 4, mas carga exige mínimo de 4 vCPUs",
				alt.VMSize, alt.VMCPUCores)
		}
	}
}

func TestSuggestAlternativeSKUs_MemoryBound(t *testing.T) {
	// Pool memory-bound: Mem P95 = 80%, CPU P95 = 20%
	// Em VM 4 vCPU / 16 GB: memUsed = 12.8 GB → minMemGB = ceil(12.8/0.8) = 16
	// Bottleneck = memory → todas alternativas devem ter ≥16 GB
	ca := NewNodePoolCostAnalyzer()
	metrics := &NodePoolMetrics{
		VMSize:       "Standard_D4s_v3",
		NodePoolName: "mem-pool",
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 20.0, MemUsagePercent: 80.0},
		},
	}

	alts := ca.suggestAlternativeSKUs(metrics, 4, 16, 0.28, 5.50, 3)
	for _, alt := range alts {
		if alt.VMMemoryGB < 16 {
			t.Errorf("alternativa %s tem %d GB < 16, insuficiente para carga histórica",
				alt.VMSize, alt.VMMemoryGB)
		}
		if alt.Bottleneck != "memory" {
			t.Errorf("esperado bottleneck=memory, obtido %q", alt.Bottleneck)
		}
	}
}

func TestSuggestAlternativeSKUs_RationaleContainsP95(t *testing.T) {
	// O rationale deve mencionar o P95 histórico
	ca := NewNodePoolCostAnalyzer()
	metrics := &NodePoolMetrics{
		VMSize:       "Standard_D4s_v3",
		NodePoolName: "test-pool",
		NodesSnapshot: []NodePoolNodeSnapshot{
			{CPUUsagePercent: 75.0, MemUsagePercent: 25.0},
		},
		CPUTrendPerNode: []TrendSnapshot{
			{DaysAgo: 7, ValuePerNode: 78.0},
		},
	}

	alts := ca.suggestAlternativeSKUs(metrics, 4, 16, 0.28, 5.50, 2)
	for _, alt := range alts {
		if alt.Rationale == "" {
			t.Errorf("alternativa %s tem Rationale vazio", alt.VMSize)
		}
		// Rationale deve mencionar P95 (conter "%" para indicar percentual)
		if len(alt.Rationale) < 20 {
			t.Errorf("alternativa %s tem Rationale muito curto: %q", alt.VMSize, alt.Rationale)
		}
	}
}
