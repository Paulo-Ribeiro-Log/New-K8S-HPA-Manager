# NOTA: Integração Stress Test Parcial

## Problema Identificado

O arquivo `internal/models/stress_test.go` existe e está sintaticamente correto, mas o compilador Go não está reconhecendo os tipos definidos nele (StressTestMetrics, PeakMetrics, etc.).

Comando `go list -f '{{.GoFiles}}' ./internal/models` retorna apenas: `[baseline.go types.go]`, ignorando stress_test.go.

## Solução Temporária

Comentados temporariamente no engine.go:
- Campo `stressMetrics *models.StressTestMetrics`
- Métodos: `captureBaseline()`, `compareWithBaseline()`, `finalizeStressTest()`

## Próximos Passos

1. Investigar por que stress_test.go não está sendo incluído no pacote models
2. Possível solução: deletar e recriar stress_test.go
3. Reativar código comentado após resolver o problema

## Status Atual

✅ BaselineCollector - Implementado e funcionando
✅ StressComparator - Implementado e funcionando  
✅ Schema SQLite - Implementado
⚠️ Integração no Engine - Parcialmente implementada (código comentado)

