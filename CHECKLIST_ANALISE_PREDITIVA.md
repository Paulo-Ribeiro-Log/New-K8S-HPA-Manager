# Checklist de Melhorias - Análise Preditiva

**Criado em**: 06/02/2026
**Progresso geral**: 0/24 itens (0%)

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

## Fase 1 - Quick Wins (2-3 dias) | Prioridade: ALTA

### 1.1 Resumo de Ação no Topo do Modal
- [ ] Criar componente `PredictionActionSummary`
- [ ] Exibir status geral (Saudável/Atenção/Crítico) com cor
- [ ] Mostrar quantidade de ações recomendadas
- [ ] Botão direto para aplicar correção principal
- [ ] Indicar próxima revisão recomendada
- **Arquivos**: `DeploymentsTab.tsx`
- **Estimativa**: 4h

**Exemplo de implementação**:
```tsx
<div className="bg-yellow-50 border-l-4 border-yellow-500 p-4">
  <h3>⚠️ 1 Ação Recomendada</h3>
  <p>CPU Usage está em tendência de alta (+15%/semana).</p>
  <p>Estimativa: Atingirá 90% do limit em <strong>~3 dias</strong>.</p>
  <Button>Aplicar: Aumentar CPU limit para 2 cores</Button>
</div>
```

### 1.2 Tempo até Crítico (hours_to_critical)
- [ ] Calcular no backend quando métrica atingirá threshold crítico
- [ ] Adicionar campo `hours_to_critical` no response
- [ ] Exibir no frontend com urgência visual
- [ ] Cores: verde (>7d), amarelo (1-7d), vermelho (<24h)
- **Arquivos**: `collector.go`, `models.go`, `DeploymentsTab.tsx`
- **Estimativa**: 4h

### 1.3 Badges de Confiança nas Previsões
- [ ] Adicionar campo `confidence_percent` em cada previsão
- [ ] Exibir badge com porcentagem (ex: "78% certeza")
- [ ] Tooltip explicando o que afeta a confiança
- **Arquivos**: `analyzer.go`, `models.go`, `DeploymentsTab.tsx`
- **Estimativa**: 2h

### 1.4 Colorir Métricas (Semáforo Visual)
- [ ] CPU: verde (<60%), amarelo (60-80%), vermelho (>80%)
- [ ] Memória: verde (<70%), amarelo (70-85%), vermelho (>85%)
- [ ] Tendências: verde (estável/descendo), amarelo (subindo leve), vermelho (subindo rápido)
- [ ] Aplicar cores nos cards de métricas do modal
- **Arquivos**: `DeploymentsTab.tsx`
- **Estimativa**: 2h

---

## Fase 2 - Análise de Custo (3-4 dias) | Prioridade: ALTA

### 2.1 Calcular Custo Atual do Deployment
- [ ] Criar `internal/monitoring/predictions/cost_analyzer.go`
- [ ] Fórmula: `(cpu_requests * preco_cpu_hora) + (mem_requests * preco_mem_hora) * replicas * 720h`
- [ ] Suportar preços customizáveis via config (Azure default)
- [ ] Adicionar `current_monthly_cost` no response
- **Arquivos**: `cost_analyzer.go` (novo), `collector.go`, `models.go`
- **Estimativa**: 4h

**Estrutura proposta**:
```go
type CostAnalysis struct {
    CurrentMonthlyCost     float64 `json:"current_monthly_cost"`
    RecommendedCost        float64 `json:"recommended_cost"`
    MonthlySavings         float64 `json:"monthly_savings"`
    PaybackPeriodDays      int     `json:"payback_period_days"`
    CostPerMillionRequests float64 `json:"cost_per_million_requests"`
    Currency               string  `json:"currency"` // BRL, USD
}
```

### 2.2 Custo de Cada Recomendação
- [ ] Calcular custo antes/depois para cada recomendação
- [ ] Exibir: "Escalar para 4 réplicas = +R$ 120/mês"
- [ ] Incluir no card da recomendação
- **Arquivos**: `cost_analyzer.go`, `analyzer.go`, `DeploymentsTab.tsx`
- **Estimativa**: 3h

### 2.3 Economia de Downsizing
- [ ] Detectar recursos subutilizados (CPU <30%, Mem <40%)
- [ ] Calcular economia mensal potencial
- [ ] Exibir: "Reduzir CPU = -R$ 50/mês sem impacto"
- **Arquivos**: `cost_analyzer.go`, `DeploymentsTab.tsx`
- **Estimativa**: 3h

### 2.4 ROI Dashboard (Card no Modal)
- [ ] Card resumo: Custo Atual | Custo Otimizado | Economia
- [ ] Trade-off visual: custo vs performance
- [ ] Gráfico de barras comparativo
- **Arquivos**: `DeploymentsTab.tsx`
- **Estimativa**: 4h

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

| Fase | Itens | Completos | Progresso | Estimativa |
|------|-------|-----------|-----------|------------|
| Fase 1 - Quick Wins | 4 | 0 | 0% | 2-3 dias |
| Fase 2 - Custo | 4 | 0 | 0% | 3-4 dias |
| Fase 3 - Sazonalidade | 4 | 0 | 0% | 4-5 dias |
| Fase 4 - Feedback Loop | 4 | 0 | 0% | 3-4 dias |
| Fase 5 - Métricas | 5 | 0 | 0% | 2-3 dias |
| Fase 6 - UX Modal | 3 | 0 | 0% | 2-3 dias |
| **TOTAL** | **24** | **0** | **0%** | **16-22 dias** |

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

### Preços de Referência (Azure AKS - Brasil Sul)
```go
const (
    // Preços aproximados por hora (verificar Azure Pricing Calculator)
    PriceCPUCoreHour  = 0.05  // USD por vCPU/hora
    PriceMemGBHour    = 0.01  // USD por GB/hora
    HoursPerMonth     = 720   // 24h * 30 dias
    USDToBRL          = 5.0   // Taxa de câmbio aproximada
)
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
