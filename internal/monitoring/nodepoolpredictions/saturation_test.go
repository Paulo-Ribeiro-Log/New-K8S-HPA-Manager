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
