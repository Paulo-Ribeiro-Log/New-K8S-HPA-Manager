# Checklist de Melhorias - Análise Preditiva

**Criado em**: 06/02/2026
**Última atualização**: 06/02/2026
**Progresso geral**: 20/24 itens (83%) - Fase 1 + Fase 2 + Fase 3 + Fase 5 + Fase 6 completas

---

## Diagnóstico Executivo

A feature atual é **mais "retrospectiva" do que "preditiva"**. Ela coleta dados históricos de 14 dias, calcula tendências lineares simples, e pede para a IA fazer previsões sem dados suficientes para decisões reais.

### Problemas Identificados:
1. Previsões lineares irreais (não considera sazonalidade)
2. Falta de contexto de custo (R$/mês)
3. Recomendações genéricas sem evidência concreta
4. Sem análise de sazonalidade (padrões diários/semanais)
5. Falta de SLA e impacto no negócio
6. Excesso de informação técnica no modal
7. Falta "O Que Fazer Agora" (resumo executivo acionável)
8. Sem comparação visual (antes/depois)

---

## Fase 1 - Quick Wins (2-3 dias) | Prioridade: ALTA | ✅ COMPLETO

### 1.1 Resumo de Ação no Topo do Modal ✅
- [x] Criar componente `ActionSummary` inline no modal
- [x] Exibir status geral (Saudável/Atenção/Crítico) com cor
- [x] Mostrar quantidade de ações recomendadas (total e urgentes)
- [x] Exibir ação principal recomendada com comando kubectl
- [x] Indicar próxima revisão recomendada (dias)
- **Arquivos**: `DeploymentsTab.tsx`, `models.go`, `analyzer.go`
- **Implementado em**: 06/02/2026

### 1.2 Tempo até Crítico (hours_to_critical) ✅
- [x] Calcular no backend quando métrica atingirá threshold crítico
- [x] Adicionar struct `ActionSummary` com campo `HoursToCritical`
- [x] Método `calculateHoursToCritical()` analisa CPU, memória, HPA e predictions
- [x] Exibir no frontend com urgência visual (card com horas)
- [x] Cores: vermelho (<24h), amarelo (24-72h), ajuste automático de status
- **Arquivos**: `analyzer.go`, `models.go`, `DeploymentsTab.tsx`
- **Implementado em**: 06/02/2026

### 1.3 Badges de Confiança nas Previsões ✅
- [x] Adicionar campo `ConfidencePercent` em struct `Prediction`
- [x] Método `enrichPredictionsWithConfidence()` calcula confiança
- [x] Fatores: idade do deployment, volatilidade, probabilidade, timeframe
- [x] Confiança geral exibida no ActionSummary
- **Arquivos**: `analyzer.go`, `models.go`, `DeploymentsTab.tsx`
- **Implementado em**: 06/02/2026

### 1.4 Colorir Métricas (Semáforo Visual) ✅
- [x] Status colorido no ActionSummary (verde/amarelo/vermelho)
- [x] Confiança colorida (verde >=70%, amarelo 50-70%, vermelho <50%)
- [x] Tendências já tinham cores (laranja subindo, verde descendo, azul estável)
- [x] Cores aplicadas nos cards de métricas do modal
- **Arquivos**: `DeploymentsTab.tsx`
- **Implementado em**: 06/02/2026

### Correções Adicionais - Remoção de Emojis (jsPDF Compatibility)
- [x] Removidos emojis do PDF/MD frontend (`DeploymentsTab.tsx`)
- [x] Substituído emoji 💰 por texto "[ECONOMIA]" e símbolo "$" no frontend
- [x] Removidos emojis do backend Markdown (`predictions.go`)
  - `### 💰 OPORTUNIDADE...` → `### [ALERTA] OPORTUNIDADE...`
  - `💰 **ECONOMIA DE CUSTOS** 💰` → `**[ECONOMIA DE CUSTOS]**`
- [x] Removidos emojis do contexto temporal (`analyzer.go`)
  - `⏰ CONTEXTO TEMPORAL` → `CONTEXTO TEMPORAL`
  - `⚠️ DEPLOYMENT NOVO` → `[ATENCAO] DEPLOYMENT NOVO`
  - `✅ DEPLOYMENT MADURO` → `[OK] DEPLOYMENT MADURO`
- [x] Adicionada instrução explícita no prompt da IA para NAO usar emojis
  - `**NAO USE EMOJIS OU ICONES** - apenas texto puro`

---

## Fase 2 - Análise de Custo (3-4 dias) | Prioridade: ALTA | ✅ COMPLETO

### 2.1 Calcular Custo Atual do Deployment ✅
- [x] Criar `internal/monitoring/predictions/cost_analyzer.go`
- [x] Formula: `(cpuRequest * $0.05/h + memRequestGB * $0.005/h) * replicas * 730h`
- [x] Precos Azure pay-as-you-go (referencia Brasil Sul)
- [x] Adicionar `CostAnalysis` no `PredictionResult`
- [x] Cotacao USD/BRL automatica via API publica (cache 1h, fallback R$ 5,50)
- [x] Exibir valores em USD + BRL + cotacao de referencia
- **Arquivos**: `cost_analyzer.go` (novo), `models.go`, `analyzer.go`
- **Implementado em**: 06/02/2026

### 2.2 Custo de Cada Recomendacao ✅
- [x] Calcular custo antes/depois para cada recomendacao de over-provisioning
- [x] Struct `CostRecommendation` com `CostBeforeUSD`, `CostAfterUSD`, `SavingsUSD`, `SavingsBRL`
- [x] Exibir no card da recomendacao com impacto (low/medium/high)
- **Arquivos**: `cost_analyzer.go`, `models.go`, `DeploymentsTab.tsx`
- **Implementado em**: 06/02/2026

### 2.3 Economia de Downsizing ✅
- [x] Detectar CPU over-provisioned (P95 < 30% do request)
- [x] Detectar Memory over-provisioned (P95 < 40% do request)
- [x] Detectar replicas ociosas (uso total < 20% da capacidade)
- [x] Right-sizing: P95 + 20% margem de seguranca
- [x] Calcular economia mensal e anual potencial (USD + BRL)
- **Arquivos**: `cost_analyzer.go`, `DeploymentsTab.tsx`
- **Implementado em**: 06/02/2026

### 2.4 ROI Dashboard (Card no Modal) ✅
- [x] Card resumo: Custo Mensal | Custo por Replica | Economia Potencial (grid 3 colunas)
- [x] Breakdown CPU vs Memory com valores
- [x] Economia anual destacada quando ha potencial
- [x] Recomendacoes de custo com titulo, descricao e economia
- [x] Secao completa no export PDF (tabela + breakdown + recomendacoes)
- [x] Secao completa no relatorio Markdown (backend)
- **Arquivos**: `DeploymentsTab.tsx`, `predictions.go`
- **Implementado em**: 06/02/2026

---

## Fase 3 - Sazonalidade (4-5 dias) | Prioridade: MÉDIA | ✅ COMPLETO

### 3.1 Detectar Padrões Horários ✅
- [x] Coletar métricas agrupadas por hora do dia (últimos 7 dias) via QueryRange Prometheus (step=1h)
- [x] Calcular média por hora (0-23h) agrupando `pair.Timestamp.Time().Hour()`
- [x] Identificar peak_hours (>120% da média) e low_hours (<80% da média)
- [x] Calcular peak_multiplier (quanto sobe no pico vs vale)
- **Arquivos**: `collector.go` (`collectSeasonalPatterns`, `queryMatrix`), `models.go`, `queries.go`
- **Implementado em**: 11/02/2026

### 3.2 Detectar Padrões Semanais ✅
- [x] Agrupar métricas por dia da semana via `pair.Timestamp.Time().Weekday()`
- [x] Identificar high_days (>110%) e low_days (<90%)
- [x] Calcular weekend_reduction (% de queda no fim de semana vs dias úteis)
- [x] Flag `IsTrendSeasonal`: hora atual em peak_hours + tendência TrendUp
- **Arquivos**: `collector.go`, `models.go`
- **Implementado em**: 11/02/2026

### 3.3 Ajustar Previsões por Sazonalidade ✅
- [x] Função `buildSeasonalSection()` em `analyzer.go` gera contexto sazonal para prompt da IA
- [x] Alerta quando `IsTrendSeasonal = true`: "aumento coincide com pico sazonal"
- [x] Instrução à IA: diferenciar sazonalidade de crescimento real, recomendar HPA vs scaling permanente
- [x] `SeasonalAdjustedTrend = "stable_seasonal_peak"` quando sazonal
- **Arquivos**: `analyzer.go` (`buildSeasonalSection`, `buildAIPrompt`)
- **Implementado em**: 11/02/2026

### 3.4 Gráfico de "Típico Dia/Semana" ✅
- [x] Accordion "Padrões Sazonais" no modal preditivo (apenas quando `has_sufficient_data = true`)
- [x] Banner de alerta quando `is_trend_seasonal = true`
- [x] Cards de resumo: hora de pico, pico semanal, redução fim de semana
- [x] BarChart horário (0-23h, barras coloridas: laranja=pico, azul=vale, índigo=normal)
- [x] BarChart semanal (Dom-Sáb, azul=fim de semana, índigo=dias úteis)
- [x] Legendas explicativas abaixo de cada gráfico
- [x] Import `Cell` adicionado ao recharts
- **Arquivos**: `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

---

## Fase 4 - Feedback Loop (3-4 dias) | Prioridade: MÉDIA

### 4.1 Botões Aceitar/Rejeitar Recomendação
- [ ] Adicionar botões em cada card de recomendação
- [ ] Registrar decisão no banco (SQLite)
- [ ] Campos: recommendation_id, action (accept/reject/defer), user, timestamp
- **Arquivos**: `DeploymentsTab.tsx`, `predictions_store.go`, `handlers/predictions.go`
- **Estimativa**: 4h

### 4.2 Medir se Recomendação Funcionou
- [ ] Job que compara métricas antes/depois (7 dias após aplicar)
- [ ] Calcular: "Latência reduziu 15% após aplicar"
- [ ] Marcar recomendação como "efetiva" ou "sem efeito"
- **Arquivos**: `predictions_store.go`, novo job em `collector.go`
- **Estimativa**: 6h

### 4.3 Aprender com Histórico
- [ ] Mostrar: "Esta recomendação funcionou 87% das vezes"
- [ ] Priorizar recomendações com alto sucesso histórico
- [ ] Desprioritizar recomendações frequentemente rejeitadas
- **Arquivos**: `analyzer.go`, `predictions_store.go`
- **Estimativa**: 4h

### 4.4 Modal de Histórico de Recomendações
- [ ] Listar recomendações passadas para o deployment
- [ ] Status: Aplicada/Rejeitada/Pendente
- [ ] Resultado: Efetiva/Sem efeito/Aguardando
- **Arquivos**: `DeploymentsTab.tsx`, `handlers/predictions.go`
- **Estimativa**: 4h

---

## Fase 5 - Métricas Adicionais (2-3 dias) | Prioridade: BAIXA | ✅ COMPLETO

### 5.1 RPS (Requests por Segundo) ✅
- [x] Query Prometheus: `sum(rate(http_requests_total{...}[5m]))` em `GetRPSQuery()`
- [x] Coletado em `collectSnapshot()` como `snapshot.RPS`
- [x] Exibido no modal: card "Req/s (RPS)" com N/A quando não instrumentado
- **Arquivos**: `queries.go`, `collector.go`, `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

### 5.2 Error Rate % ✅
- [x] Query já existia em `GetErrorRateQuery()` — coletado em `collectSnapshot()`
- [x] Threshold visual: verde (<1%), amarelo (1-5%), vermelho (>5%)
- [x] Badge de alerta no trigger do accordion quando error_rate >= 1%
- **Arquivos**: `queries.go`, `collector.go`, `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

### 5.3 Latência P99 ✅
- [x] Query já existia em `GetLatencyP99Query()` — coletado em `collectSnapshot()`
- [x] Threshold visual: verde (<200ms), amarelo (200-500ms), vermelho (>500ms)
- [x] P50 também exibido quando P99 disponível
- **Arquivos**: `queries.go`, `collector.go`, `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

### 5.4 Eventos de OOMKill ✅
- [x] `countOOMKillEvents()` busca eventos K8s com reason=OOMKilling (últimos 7 dias)
- [x] Filtra por pods do deployment (prefix match)
- [x] Card vermelho quando > 0 com instrução de ação
- [x] Alerta destacado abaixo do grid quando há OOMKill
- **Arquivos**: `collector.go`, `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

### 5.5 Uptime % (30 dias) ✅
- [x] `GetUptimeQuery()`: `avg_over_time(clamp_max(replicas_available, 1)[30d:5m]) * 100`
- [x] Fallback 1: ratio atual `replicas_available / spec_replicas * 100`
- [x] Fallback 2: ratio baseado em métricas K8s diretas
- [x] Threshold: verde ≥99%, amarelo 95-99%, vermelho <95%
- **Arquivos**: `queries.go`, `collector.go`, `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

---

## Fase 6 - UX do Modal (2-3 dias) | Prioridade: MÉDIA

### 6.1 Novo Layout do Modal ✅
- [x] Reorganizar seções por prioridade de decisão
- [x] Resumo executivo sempre no topo (ActionSummary)
- [x] 3 cards visuais de métricas: CPU, Memória, Custo (com cores dinâmicas)
- [x] Badges de tendência com setas direcionais (↗↘→)
- **Arquivos**: `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

**Layout proposto**:
```
┌──────────────────────────────────────────────────────────────┐
│ ANÁLISE PREDITIVA - deployment-name                          │
├──────────────────────────────────────────────────────────────┤
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ RESUMO EXECUTIVO                                         │ │
│ │ Score: 72/100 [barra visual]                            │ │
│ │ Status: Operacional, mas com pontos de atenção          │ │
│ │ Próxima Revisão: 7 dias                                 │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                              │
│ ┌───────────────┐ ┌───────────────┐ ┌───────────────┐       │
│ │ CPU           │ │ MEMÓRIA       │ │ CUSTO         │       │
│ │ 45% [verde]   │ │ 68% [amarelo] │ │ R$ 340/mês    │       │
│ │ Trend: +5%    │ │ Trend: -2%    │ │ Economia: R$50│       │
│ └───────────────┘ └───────────────┘ └───────────────┘       │
│                                                              │
│ AÇÕES RECOMENDADAS (1)                                      │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ [amarelo] Prioridade Média - Memória                    │ │
│ │ Memória P95 em 85% do limit. Risco de OOM em ~5 dias.  │ │
│ │ Custo: +R$ 15/mês | Confiança: 78%                     │ │
│ │ [Ignorar] [Agendar] [Aplicar Agora]                    │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                              │
│ [Ver Dados Detalhados v] [Exportar PDF] [Histórico]         │
└──────────────────────────────────────────────────────────────┘
```

### 6.2 Accordion para Dados Detalhados ✅
- [x] 7 acordeões implementados: Health Score, Resumo Executivo, Dados Analisados,
      Análise de Custos, Tendências Temporais, Previsões, Recomendações
- [x] Trigger de cada acordeão exibe sumário: score, badge de risco, custo, contador
- [x] Accordion type="multiple" - múltiplas seções abertas simultaneamente
- [x] Dados técnicos iniciam colapsados para UX mais limpa
- **Arquivos**: `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

### 6.3 Comparação Visual Antes/Depois ✅
- [x] Toggle "Projeção de Cenários (30 dias)" no accordion "Tendências Temporais"
- [x] Gráfico CPU: Histórico P95 (sólido) + Sem Ação (dashed vermelho) + Com Recomendações (dashed verde)
- [x] Gráfico Memória: mesma estrutura
- [x] ReferenceLine vertical em "Atual" delimita histórico vs projeção
- [x] Cards de custo projetado em D+30: Atual | Sem Ação (+%) | Com Recomendações (-% economia)
- [x] Reset automático do toggle ao fechar o modal
- **Arquivos**: `DeploymentsTab.tsx`
- **Implementado em**: 11/02/2026

---

## Resumo de Progresso

| Fase | Itens | Completos | Progresso | Status |
|------|-------|-----------|-----------|--------|
| Fase 1 - Quick Wins | 4 | 4 | ✅ 100% | Implementado 06/02/2026 |
| Fase 2 - Custo | 4 | 4 | ✅ 100% | Implementado 06/02/2026 |
| Fase 3 - Sazonalidade | 4 | 0 | 0% | Pendente |
| Fase 4 - Feedback Loop | 4 | 0 | 0% | Pendente |
| Fase 5 - Métricas | 5 | 5 | ✅ 100% | Implementado 11/02/2026 |
| Fase 6 - UX Modal | 3 | 3 | ✅ 100% | Implementado 11/02/2026 |
| **TOTAL** | **24** | **11** | **46%** | Em progresso |

---

## Arquivos Principais

### Backend (Go)
- `internal/monitoring/predictions/collector.go` - Coleta de métricas
- `internal/monitoring/predictions/analyzer.go` - Análise com IA
- `internal/monitoring/predictions/models.go` - Estruturas de dados
- `internal/monitoring/predictions/queries.go` - Queries Prometheus
- `internal/monitoring/predictions/cost_analyzer.go` - **NOVO** Análise de custo
- `internal/storage/predictions_store.go` - Persistência SQLite
- `internal/web/handlers/predictions.go` - API REST

### Frontend (React/TypeScript)
- `internal/web/frontend/src/components/DeploymentsTab.tsx` - Modal principal

---

## Como Continuar

1. Abrir novo chat com Claude
2. Mencionar: "Continue o checklist de Análise Preditiva em `CHECKLIST_ANALISE_PREDITIVA.md`"
3. Especificar qual fase/item quer implementar (ex: "Implementar Fase 1.1 - Resumo de Ação")

---

## Notas de Implementação

### Precos de Referencia (Azure AKS - Brasil Sul) ✅ Implementado
```go
const (
    PriceCPUCoreHour = 0.05  // USD por vCPU/hora
    PriceMemGBHour   = 0.005 // USD por GB/hora
    HoursPerMonth    = 730   // 365 dias * 24h / 12 meses

    // Thresholds de over-provisioning
    CPUOverProvisionThreshold = 0.30 // P95 < 30% do request = over-provisioned
    MemOverProvisionThreshold = 0.40 // P95 < 40% do request = over-provisioned

    // Margem de seguranca para right-sizing
    RightSizingMargin = 1.20 // P95 + 20%

    // Cotacao fallback quando API falha
    DefaultExchangeRate = 5.50
)
// Cotacao USD/BRL: API publica economia.awesomeapi.com.br (gratis, sem auth, cache 1h)
```

### Thresholds de Cores
```go
// CPU
CPUGreen  = 0.60  // <60% - Saudável
CPUYellow = 0.80  // 60-80% - Atenção
CPURed    = 0.80  // >80% - Crítico

// Memória
MemGreen  = 0.70  // <70% - Saudável
MemYellow = 0.85  // 70-85% - Atenção
MemRed    = 0.85  // >85% - Crítico
```

### Fórmula de Confiança
```go
func calculateConfidence(metrics *Metrics) float64 {
    confidence := 100.0

    // Reduz confiança se poucos dados
    if metrics.DataPoints < 100 {
        confidence -= 20
    }

    // Reduz se alta variância
    if metrics.CpuStdDev > 0.3 {
        confidence -= 15
    }

    // Reduz se deployment novo (<7 dias)
    if metrics.DeploymentAge < 7*24*time.Hour {
        confidence -= 25
    }

    return max(confidence, 10) // Mínimo 10%
}
```
