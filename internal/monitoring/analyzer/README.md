# Analyzer Package

Detector de anomalias para HPA Watchdog - Fase 1 (MVP).

## 📋 Anomalias Detectadas

### Fase 1 - MVP (Implementado) ✅

| # | Anomalia | Severidade | Condição | Duração |
|---|----------|------------|----------|---------|
| 1 | **Oscillation** | 🔴 Critical | >5 mudanças réplicas | 5min |
| 2 | **Maxed Out** | 🔴 Critical | replicas=max + CPU>target+20% | 2min |
| 3 | **OOMKilled** | 🔴 Critical | Pod killed por OOM | - |
| 4 | **Pods Not Ready** | 🔴 Critical | Pods not ready | 3min |
| 5 | **High Error Rate** | 🔴 Critical | >5% erros 5xx | 2min |

## 🚀 Uso

```go
import (
    "github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/analyzer"
    "github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/storage"
)

// Criar detector
cache := storage.NewTimeSeriesCache(nil)
detector := analyzer.NewDetector(cache, nil)

// Detectar anomalias
result := detector.Detect()

fmt.Printf("Found %d anomalies\n", len(result.Anomalies))

for _, anomaly := range result.Anomalies {
    fmt.Printf("%s: %s\n", anomaly.Type, anomaly.Message)
    fmt.Printf("Actions: %v\n", anomaly.Actions)
}
```

## 🔍 Detalhes das Anomalias

### 1. Oscillation
- **Condição**: >5 mudanças de réplicas em 5min
- **Usa**: `ts.Stats.ReplicaChanges`
- **Ações**: Aumentar stabilizationWindow, revisar targets

### 2. Maxed Out
- **Condição**: `CurrentReplicas == MaxReplicas` + `CPU > Target+20%` por 2min
- **Usa**: Latest snapshot + checkMinDuration
- **Ações**: Aumentar maxReplicas, verificar capacidade cluster

### 3. OOMKilled
- **Status**: Placeholder (requer integração K8s events)
- **TODO**: Implementar

### 4. Pods Not Ready
- **Condição**: `Ready == false` por 3min
- **TODO**: Melhorar com contagem real de pods
- **Ações**: Verificar logs, readiness probe, dependências

### 5. High Error Rate
- **Condição**: `ErrorRate > 5%` por 2min (requer Prometheus)
- **Usa**: Latest snapshot + checkMinDuration
- **Ações**: Verificar logs, dependências, considerar scale up

## ⚙️ Configuração

```go
config := &analyzer.DetectorConfig{
    OscillationMaxChanges: 5,
    OscillationWindow:     5 * time.Minute,
    MaxedOutCPUDeviation:  20.0, // %
    MaxedOutMinDuration:   2 * time.Minute,
    ErrorRateThreshold:    5.0,  // %
    ErrorRateMinDuration:  2 * time.Minute,
    NotReadyThreshold:     70.0, // %
    NotReadyMinDuration:   3 * time.Minute,
    AlertCooldown:         5 * time.Minute,
}

detector := analyzer.NewDetector(cache, config)
```

## 📊 DetectionResult

```go
type DetectionResult struct {
    Anomalies []Anomaly
    Checked   int
    Timestamp time.Time
}

// Métodos úteis
counts := result.GetAnomalyCount()           // map[AnomalyType]int
critical := result.GetBySeverity(Critical)   // []Anomaly
cluster := result.GetByCluster("production") // []Anomaly
```

## 🧪 Testes

```bash
go test ./internal/analyzer/... -v
```

**12 testes, todos passando:**
- TestNewDetector
- TestDetectOscillation
- TestDetectMaxedOut
- TestDetectMaxedOut_NotMaxed
- TestDetectHighErrorRate
- TestDetectHighErrorRate_NoPrometheus
- TestDetectPodsNotReady
- TestDetectMultipleAnomalies
- TestGetAnomalyCount
- TestGetBySeverity
- TestGetByCluster
- TestMinDuration

## 🔄 Integração

```
Monitoring Loop (30s)
├─ Collector coleta HPASnapshot
├─ Storage.Add(snapshot)
├─ Analyzer.Detect()        ← NOVO!
│  ├─ Analisa stats + snapshots
│  ├─ Aplica regras de detecção
│  ├─ Verifica duração mínima
│  └─ Retorna anomalias
└─ TUI exibe result.Anomalies
```

## 📚 Referências

- [ANOMALY_DETECTION.md](../../docs/ANOMALY_DETECTION.md)
- [ANOMALY_DETECTION_SUMMARY.md](../../docs/ANOMALY_DETECTION_SUMMARY.md)
- [storage/README.md](../storage/README.md)

---

**Status:** ✅ Fase 1 Implementada
**Testes:** ✅ 12/12 Passando
**Próximo:** Fase 2 (mais 5 anomalias)
