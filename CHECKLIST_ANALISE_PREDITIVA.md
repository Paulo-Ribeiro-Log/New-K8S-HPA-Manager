# Checklist de Melhorias - Análise Preditiva

**Criado em**: 06/02/2026
**Última atualização**: 06/02/2026
**Progresso geral**: 8/24 itens (33%) - Fase 1 + Fase 2 completas

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

## Fase 3 - Sazonalidade (4-5 dias) | Prioridade: MÉDIA

### 3.1 Detectar Padrões Horários
- [ ] Coletar métricas agrupadas por hora do dia (últimos 7 dias)
- [ ] Calcular média por hora (0-23h)
- [ ] Identificar peak_hours e low_hours
- [ ] Calcular peak_multiplier (quanto sobe no pico vs média)
- **Arquivos**: `collector.go`, `queries.go`, `models.go`
- **Estimativa**: 6h

**Query Prometheus proposta**:
```promql
avg_over_time(
  sum(rate(container_cpu_usage_seconds_total{pod=~"deployment-.*"}[5m])) by (pod)
[7d:1h])
```

### 3.2 Detectar Padrões Semanais
- [ ] Agrupar métricas por dia da semana
- [ ] Identificar high_days e low_days
- [ ] Calcular weekend_reduction (% de queda no fim de semana)
- **Arquivos**: `collector.go`, `models.go`
- **Estimativa**: 4h

**Estrutura proposta**:
```go
type SeasonalPatterns struct {
    Hourly struct {
        PeakHours      []int   `json:"peak_hours"`      // [14, 15, 16]
        LowHours       []int   `json:"low_hours"`       // [2, 3, 4]
        PeakMultiplier float64 `json:"peak_multiplier"` // 1.8
    } `json:"hourly"`
    Weekly struct {
        HighDays         []string `json:"high_days"`         // ["monday", "tuesday"]
        LowDays          []string `json:"low_days"`          // ["saturday", "sunday"]
        WeekendReduction float64  `json:"weekend_reduction"` // 0.4
    } `json:"weekly"`
}
```

### 3.3 Ajustar Previsões por Sazonalidade
- [ ] Descontar sazonalidade das tendências
- [ ] Evitar falsos positivos (pico normal vs crescimento real)
- [ ] Flag: "Este aumento é padrão sazonal, não tendência"
- **Arquivos**: `analyzer.go`
- **Estimativa**: 4h

### 3.4 Gráfico de "Típico Dia/Semana"
- [ ] Componente visual mostrando padrão esperado
- [ ] Linha de "baseline" vs "atual"
- [ ] Destacar desvios do padrão
- **Arquivos**: `DeploymentsTab.tsx` (Recharts)
- **Estimativa**: 4h

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

## Fase 5 - Métricas Adicionais (2-3 dias) | Prioridade: BAIXA

### 5.1 RPS (Requests por Segundo)
- [ ] Query Prometheus: `rate(http_requests_total[5m])`
- [ ] Adicionar ao contexto enviado para IA
- [ ] Exibir no modal: "Carga atual: 150 req/s"
- **Arquivos**: `queries.go`, `collector.go`, `DeploymentsTab.tsx`
- **Estimativa**: 2h

### 5.2 Error Rate %
- [ ] Query: `rate(http_requests_total{status=~"5.."})/rate(http_requests_total)`
- [ ] Threshold: verde (<1%), amarelo (1-5%), vermelho (>5%)
- [ ] Correlacionar com CPU/Memória alta
- **Arquivos**: `queries.go`, `collector.go`
- **Estimativa**: 2h

### 5.3 Latência P99
- [ ] Query: `histogram_quantile(0.99, ...)`
- [ ] Comparar com SLA (se configurado)
- [ ] Alertar se P99 > SLA target
- **Arquivos**: `queries.go`, `collector.go`
- **Estimativa**: 2h

### 5.4 Eventos de OOMKill
- [ ] Buscar eventos do Kubernetes com reason=OOMKilled
- [ ] Contar ocorrências nos últimos 7 dias
- [ ] Priorizar recomendação de aumentar memória
- **Arquivos**: `collector.go`
- **Estimativa**: 3h

### 5.5 Uptime % (30 dias)
- [ ] Calcular baseado em disponibilidade de réplicas
- [ ] Fórmula: (tempo com replicas>=1) / (tempo total)
- [ ] Exibir: "Uptime 30d: 99.7%"
- **Arquivos**: `collector.go`, `DeploymentsTab.tsx`
- **Estimativa**: 3h

---

## Fase 6 - UX do Modal (2-3 dias) | Prioridade: MÉDIA

### 6.1 Novo Layout do Modal
- [ ] Reorganizar seções por prioridade de decisão
- [ ] Resumo executivo sempre no topo
- [ ] Métricas detalhadas em accordion colapsável
- [ ] Cards de ação com botões diretos
- **Arquivos**: `DeploymentsTab.tsx`
- **Estimativa**: 6h

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

### 6.2 Accordion para Dados Detalhados
- [ ] Métricas brutas em seção colapsável
- [ ] Aplicações concorrentes em seção colapsável
- [ ] Histórico de tendências em seção colapsável
- [ ] Iniciar colapsado para UX mais limpa
- **Arquivos**: `DeploymentsTab.tsx`
- **Estimativa**: 2h

### 6.3 Comparação Visual Antes/Depois
- [ ] Mostrar gráfico simulado após aplicar recomendação
- [ ] Toggle: "Ver projeção se aplicar"
- [ ] Linha pontilhada mostrando melhoria esperada
- **Arquivos**: `DeploymentsTab.tsx` (Recharts)
- **Estimativa**: 4h

---

## Resumo de Progresso

| Fase | Itens | Completos | Progresso | Status |
|------|-------|-----------|-----------|--------|
| Fase 1 - Quick Wins | 4 | 4 | ✅ 100% | Implementado 06/02/2026 |
| Fase 2 - Custo | 4 | 4 | ✅ 100% | Implementado 06/02/2026 |
| Fase 3 - Sazonalidade | 4 | 0 | 0% | Pendente |
| Fase 4 - Feedback Loop | 4 | 0 | 0% | Pendente |
| Fase 5 - Métricas | 5 | 0 | 0% | Pendente |
| Fase 6 - UX Modal | 3 | 0 | 0% | Pendente |
| **TOTAL** | **24** | **8** | **33%** | Em progresso |

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
