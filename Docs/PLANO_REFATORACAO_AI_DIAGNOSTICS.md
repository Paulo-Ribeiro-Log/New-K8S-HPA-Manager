# Plano de Refatoração - AI Diagnostics

**Data de Criação:** 06 de janeiro de 2026
**Status:** 🔵 Planejamento Concluído - Aguardando Implementação
**Estimativa Total:** 26-34 horas (~5-6 dias de trabalho)

---

## 🎯 Objetivo
Transformar AI Diagnostics de análise textual simples em sistema estruturado com 4 seções (Sumário Executivo, Causa Raiz, Impacto, Ações Recomendadas), integração com Prometheus, e exportação profissional PDF/MD/CSV.

## 📋 Decisões do Usuário
- ✅ Provider: Respeitar configuração da aba AI Diagnostics (já existe)
- ✅ Visualização: AMBOS - Modal fullscreen + Página dedicada
- ✅ Seções: Sumário Executivo, Causa Raiz Detalhada, Impacto e Severidade, Ações Recomendadas
- ✅ Integrar métricas Prometheus (CPU/Memory atual)

## 📦 Arquivos a Modificar/Criar

### CRIAR (2 arquivos novos):
- [ ] `internal/ai/reports/generator.go` - Exportação PDF/Markdown/CSV
- [ ] `internal/web/frontend/src/components/AIAnalysisView.tsx` - Componente reutilizável de visualização

### MODIFICAR (12 arquivos existentes):
**Backend (7 arquivos):**
- [ ] `internal/ai/models.go` - Adicionar structs estruturados
- [ ] `internal/collectors/models.go` - Adicionar PrometheusMetrics
- [ ] `internal/collectors/context_builder.go` - Coletar métricas Prometheus
- [ ] `internal/ai/prompts.go` - Forçar JSON estruturado
- [ ] `internal/ai/analyzer.go` - Parse JSON, fallback para legado
- [ ] `internal/web/handlers/ai_diagnostics.go` - Endpoints de exportação
- [ ] `internal/web/server.go` - Rotas de exportação

**Frontend (5 arquivos):**
- [ ] `internal/web/frontend/src/types/ai.ts` - Novos tipos TypeScript
- [ ] `internal/web/frontend/src/lib/api/client.ts` - Método exportAIReport()
- [ ] `internal/web/frontend/src/components/AIAnalysisModal.tsx` - Usar <AIAnalysisView>
- [ ] `internal/web/frontend/src/pages/AIAnalysisPage.tsx` - Usar <AIAnalysisView>
- [ ] `internal/web/frontend/src/components/AIDiagnosticsTab.tsx` - Filtros avançados

---

## 🔄 FASE 1: Backend - Estrutura de Dados (3-4h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Média
**Dependências:** Nenhuma

### Arquivo: `internal/ai/models.go`

**Adicionar após linha 45** (após `AnalysisResult` existente):

```go
// NOVOS STRUCTS - Análise Estruturada

// ExecutiveSummary - Resumo executivo
type ExecutiveSummary struct {
    Severity      string `json:"severity"`       // critical, high, medium, low
    Status        string `json:"status"`         // unhealthy, degraded, healthy
    QuickSummary  string `json:"quick_summary"`  // 1-2 frases
    TimeToResolve string `json:"time_to_resolve"` // ex: "15 minutes"
}

// RootCauseAnalysis - Análise de causa raiz
type RootCauseAnalysis struct {
    Symptom        string   `json:"symptom"`
    ProbableCauses []string `json:"probable_causes"` // Multi-hipótese
    Evidence       []string `json:"evidence"`        // Trechos de logs
    Confidence     string   `json:"confidence"`      // high, medium, low
}

// ImpactAssessment - Avaliação de impacto
type ImpactAssessment struct {
    Severity         string `json:"severity"`
    AffectedUsers    string `json:"affected_users"`
    DowntimeEstimate string `json:"downtime_estimate"`
    SLABreach        bool   `json:"sla_breach"`
    BusinessImpact   string `json:"business_impact"`
}

// ActionableRecommendation - Ação priorizada
type ActionableRecommendation struct {
    Priority      int      `json:"priority"`        // 1-5 (1 = mais crítico)
    Title         string   `json:"title"`
    Description   string   `json:"description"`
    Commands      []string `json:"commands"`        // kubectl/az
    TimeEstimate  string   `json:"time_estimate"`
    RiskLevel     string   `json:"risk_level"`
    ImpactLevel   string   `json:"impact_level"`
}

// ResourceMetrics - Métricas Prometheus
type ResourceMetrics struct {
    CPUUsage        float64 `json:"cpu_usage"`
    CPURequest      float64 `json:"cpu_request"`
    CPULimit        float64 `json:"cpu_limit"`
    MemoryUsage     float64 `json:"memory_usage"`
    MemoryRequest   float64 `json:"memory_request"`
    MemoryLimit     float64 `json:"memory_limit"`
    RestartCount    int32   `json:"restart_count"`
    ReadyReplicas   int32   `json:"ready_replicas"`
    DesiredReplicas int32   `json:"desired_replicas"`
}
```

**Modificar struct `AnalysisResult` (linhas 6-45)**:

```go
type AnalysisResult struct {
    ID                string       `json:"id"`
    ResourceType      string       `json:"resource_type"`
    Cluster           string       `json:"cluster"`
    Namespace         string       `json:"namespace"`
    ResourceName      string       `json:"resource_name"`

    // 🆕 Estrutura nova (4 seções)
    ExecutiveSummary    ExecutiveSummary           `json:"executive_summary,omitempty"`
    RootCauseAnalysis   RootCauseAnalysis          `json:"root_cause_analysis,omitempty"`
    ImpactAssessment    ImpactAssessment           `json:"impact_assessment,omitempty"`
    Recommendations     []ActionableRecommendation `json:"recommendations,omitempty"`

    // Campos legados (manter compatibilidade)
    Analysis          string       `json:"analysis,omitempty"`
    Suggestions       []Suggestion `json:"suggestions,omitempty"`

    // Métricas (NOVO)
    CurrentMetrics    *ResourceMetrics `json:"current_metrics,omitempty"`

    // Metadados
    Provider          string       `json:"provider"`
    Model             string       `json:"model,omitempty"`
    TokensUsed        int          `json:"tokens_used,omitempty"`
    ResponseTime      float64      `json:"response_time,omitempty"`
    AnalyzedAt        time.Time    `json:"analyzed_at"`
    Error             string       `json:"error,omitempty"`
}
```

**Riscos:**
- Breaking changes em código que consome `AnalysisResult`
- **Mitigação:** Manter campos legados (`analysis`, `suggestions`) por compatibilidade

**Testes:**
- [ ] Estrutura serializa/deserializa JSON corretamente
- [ ] Análises antigas do SQLite ainda são exibidas sem erros

---

## 🔄 FASE 2: Backend - Coleta Prometheus (2-3h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Média
**Dependências:** Fase 1 concluída

### Arquivo: `internal/collectors/models.go`

**Adicionar após linha 24**:

```go
// PrometheusMetrics - Métricas coletadas do Prometheus
type PrometheusMetrics struct {
    CPUUsageCurrent    float64
    MemoryUsageCurrent float64
    CPURequest         float64
    CPULimit           float64
    MemoryRequest      float64
    MemoryLimit        float64
    RestartCount       int32
    ReadyReplicas      int32
    DesiredReplicas    int32
    CollectedAt        time.Time
}
```

**Modificar `DiagnosticContext` (linhas 9-23)**:

```go
type DiagnosticContext struct {
    // ... campos existentes
    PrometheusMetrics *PrometheusMetrics // NOVO
}
```

### Arquivo: `internal/collectors/context_builder.go`

**Adicionar função após linha ~470**:

```go
// collectPrometheusMetrics coleta métricas do Prometheus
func (c *ContextCollector) collectPrometheusMetrics(
    ctx context.Context,
    cluster, namespace, podName string,
) (*PrometheusMetrics, error) {
    promClient, err := promclient.NewPrometheusClient(cluster)
    if err != nil {
        return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
    }

    metrics := &PrometheusMetrics{CollectedAt: time.Now()}

    // CPU atual (média 5 min)
    cpuQuery := fmt.Sprintf(
        `avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container!="POD",container!=""}[5m]))`,
        namespace, podName,
    )
    if cpuResult, err := promClient.QueryScalar(ctx, cpuQuery); err == nil {
        metrics.CPUUsageCurrent = cpuResult * 1000 // millicores
    }

    // Memory atual
    memQuery := fmt.Sprintf(
        `avg(container_memory_working_set_bytes{namespace="%s",pod="%s",container!="POD",container!=""})`,
        namespace, podName,
    )
    if memResult, err := promClient.QueryScalar(ctx, memQuery); err == nil {
        metrics.MemoryUsageCurrent = memResult / (1024 * 1024) // MB
    }

    // CPU Request/Limit
    cpuReqQuery := fmt.Sprintf(
        `avg(kube_pod_container_resource_requests{namespace="%s",pod="%s",resource="cpu"})`,
        namespace, podName,
    )
    if cpuReq, err := promClient.QueryScalar(ctx, cpuReqQuery); err == nil {
        metrics.CPURequest = cpuReq * 1000 // millicores
    }

    // Memory Request/Limit, Restart Count (similar)
    // ... (implementar queries completas)

    return metrics, nil
}
```

**Modificar `CollectPodContext` (linha ~100)**:

```go
// Antes de retornar, coletar métricas Prometheus
prometheusMetrics, err := c.collectPrometheusMetrics(ctx, cluster, namespace, podName)
if err != nil {
    c.logger.Warn().Err(err).Msg("Failed to collect Prometheus metrics")
}

return DiagnosticContext{
    // ... campos existentes
    PrometheusMetrics: prometheusMetrics, // NOVO
}, nil
```

**Riscos:**
- Prometheus inacessível ou queries falhando
- **Mitigação:** Função retorna erro não-fatal, logs warn mas continua análise

**Testes:**
- [ ] Queries retornam valores corretos (CPU/Memory/Replicas)
- [ ] Graceful degradation quando Prometheus inacessível

---

## 🔄 FASE 3: Backend - Prompts JSON (4-5h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Alta
**Dependências:** Fases 1 e 2 concluídas

### Arquivo: `internal/ai/prompts.go`

**Substituir `buildPodDiagnosticPrompt` (linhas ~23-168)**:

```go
// NOVO TEMPLATE - Força retorno em JSON estruturado
func buildPodDiagnosticPrompt(context collectors.DiagnosticContext, sanitizedLogs map[string]string) string {
    // ... manter coleta de contexto existente

    return fmt.Sprintf(`Você é um especialista em diagnóstico Kubernetes.

CONTEXTO DO PROBLEMA:
Cluster: %s
Namespace: %s
Pod: %s

INFORMAÇÕES DISPONÍVEIS:
%s

MÉTRICAS ATUAIS (Prometheus):
%s

INSTRUÇÕES:
Retorne APENAS JSON válido (sem markdown, sem \`\`\`json\`\`\`) com esta estrutura:

{
  "executive_summary": {
    "severity": "critical|high|medium|low",
    "status": "unhealthy|degraded|healthy",
    "quick_summary": "Resumo de 1-2 frases explicando o problema",
    "time_to_resolve": "15 minutes"
  },
  "root_cause_analysis": {
    "symptom": "CrashLoopBackOff",
    "probable_causes": ["Causa 1", "Causa 2", "Causa 3"],
    "evidence": ["Log linha X", "Evento Y", "Métrica Z"],
    "confidence": "high|medium|low"
  },
  "impact_assessment": {
    "severity": "critical|high|medium|low",
    "affected_users": "Todos|50%%|Nenhum",
    "downtime_estimate": "30 minutes",
    "sla_breach": true|false,
    "business_impact": "Descrição do impacto no negócio"
  },
  "recommendations": [
    {
      "priority": 1,
      "title": "Ação mais crítica",
      "description": "Descrição detalhada da ação",
      "commands": ["kubectl comando1", "kubectl comando2"],
      "time_estimate": "5 minutes",
      "risk_level": "low|medium|high",
      "impact_level": "high"
    }
  ]
}

REGRAS IMPORTANTES:
1. Retorne APENAS JSON válido (sem markdown, sem explicações extras)
2. Severidade: critical (pod down), high (degraded), medium (warnings), low (info)
3. Prioridade: 1 = mais crítico, 5 = menos crítico
4. NUNCA sugerir "restart deployment" sem investigar causa raiz
5. Se timeout/connection: investigar ConfigMap, Secret, DNS, network policies

ANÁLISE PROFUNDA OBRIGATÓRIA:
- Se timeout: validar connection strings, credenciais, DNS resolution
- Se OOMKilled: comparar memory usage vs limits, verificar leaks
- Se CrashLoopBackOff: analisar últimas 30 linhas ANTES do crash
`,
        context.Cluster,
        context.Namespace,
        context.ResourceName,
        buildContextSection(context, sanitizedLogs),
        buildPrometheusMetricsSection(context.PrometheusMetrics),
    )
}
```

### Arquivo: `internal/ai/analyzer.go`

**Modificar `AnalyzeResource` (linhas ~120-134)**:

```go
// Parse JSON estruturado da resposta
var structuredResponse struct {
    ExecutiveSummary  ai.ExecutiveSummary           `json:"executive_summary"`
    RootCauseAnalysis ai.RootCauseAnalysis          `json:"root_cause_analysis"`
    ImpactAssessment  ai.ImpactAssessment           `json:"impact_assessment"`
    Recommendations   []ai.ActionableRecommendation `json:"recommendations"`
}

// Limpar markdown wrappers (IA pode insistir em usar)
cleanedResponse := strings.TrimSpace(response)
cleanedResponse = strings.TrimPrefix(cleanedResponse, "```json")
cleanedResponse = strings.TrimPrefix(cleanedResponse, "```")
cleanedResponse = strings.TrimSuffix(cleanedResponse, "```")
cleanedResponse = strings.TrimSpace(cleanedResponse)

if err := json.Unmarshal([]byte(cleanedResponse), &structuredResponse); err != nil {
    a.logger.Error().
        Err(err).
        Str("response_preview", cleanedResponse[:min(200, len(cleanedResponse))]).
        Msg("Failed to parse JSON, falling back to legacy format")

    // Fallback: salvar como análise legada
    return &AnalysisResult{
        Analysis: response, // Campo legado
        // ... outros campos
    }, nil
}

// Converter métricas Prometheus
var currentMetrics *ai.ResourceMetrics
if context.PrometheusMetrics != nil {
    currentMetrics = &ai.ResourceMetrics{
        CPUUsage:        context.PrometheusMetrics.CPUUsageCurrent,
        CPURequest:      context.PrometheusMetrics.CPURequest,
        CPULimit:        context.PrometheusMetrics.CPULimit,
        MemoryUsage:     context.PrometheusMetrics.MemoryUsageCurrent,
        MemoryRequest:   context.PrometheusMetrics.MemoryRequest,
        MemoryLimit:     context.PrometheusMetrics.MemoryLimit,
        RestartCount:    context.PrometheusMetrics.RestartCount,
        ReadyReplicas:   context.PrometheusMetrics.ReadyReplicas,
        DesiredReplicas: context.PrometheusMetrics.DesiredReplicas,
    }
}

// Construir resultado estruturado
result := &AnalysisResult{
    ExecutiveSummary:  structuredResponse.ExecutiveSummary,
    RootCauseAnalysis: structuredResponse.RootCauseAnalysis,
    ImpactAssessment:  structuredResponse.ImpactAssessment,
    Recommendations:   structuredResponse.Recommendations,
    CurrentMetrics:    currentMetrics,
    // ... metadados
}

return result, nil
```

**Riscos:**
- IA retornar JSON inválido ou incompleto
- **Mitigação:** Fallback para formato legado, logs detalhados

**Testes:**
- [ ] Claude retorna JSON válido (testar 5+ análises)
- [ ] Gemini retorna JSON válido (testar 5+ análises)
- [ ] Fallback funciona quando JSON inválido

---

## 🔄 FASE 4: Backend - Exportação (3-4h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Média
**Dependências:** Fases 1-3 concluídas

### Arquivo: `internal/ai/reports/generator.go` (CRIAR)

```go
package reports

import (
    "bytes"
    "encoding/csv"
    "fmt"
    "strings"
    "time"

    "github.com/jung-kurt/gofpdf"
    "k8s-hpa-manager/internal/ai"
)

// GeneratePDF gera relatório PDF profissional (sem emojis)
func GeneratePDF(result *ai.AnalysisResult) ([]byte, error) {
    pdf := gofpdf.New("P", "mm", "A4", "")
    pdf.AddPage()

    // Header azul (padrão Health Check/Predictions)
    pdf.SetFillColor(41, 128, 185) // RGB azul
    pdf.Rect(0, 0, 210, 40, "F")

    pdf.SetTextColor(255, 255, 255)
    pdf.SetFont("Arial", "B", 20)
    pdf.SetY(15)
    pdf.CellFormat(0, 10, "ANALISE DE DIAGNOSTICO - AI", "", 1, "C", false, 0, "")

    pdf.SetFont("Arial", "", 10)
    pdf.SetY(25)
    pdf.CellFormat(0, 5, fmt.Sprintf("Gerado em: %s", time.Now().Format("02/01/2006 15:04:05")), "", 1, "C", false, 0, "")

    // Reset cor
    pdf.SetTextColor(0, 0, 0)
    pdf.SetY(50)

    // SUMÁRIO EXECUTIVO
    pdf.SetFont("Arial", "B", 12)
    pdf.CellFormat(0, 8, "SUMARIO EXECUTIVO", "", 1, "L", false, 0, "")

    // Badge de severity (colorido)
    severityColor := getSeverityColor(result.ExecutiveSummary.Severity)
    pdf.SetFillColor(severityColor[0], severityColor[1], severityColor[2])
    pdf.SetFont("Arial", "B", 10)
    pdf.CellFormat(30, 6, strings.ToUpper(result.ExecutiveSummary.Severity), "", 0, "C", true, 0, "")
    pdf.Ln(8)

    pdf.SetFont("Arial", "", 10)
    pdf.MultiCell(0, 5, result.ExecutiveSummary.QuickSummary, "", "L", false)

    // CAUSA RAIZ, IMPACTO, RECOMENDAÇÕES (similar)
    // ... (implementar seções completas)

    var buf bytes.Buffer
    if err := pdf.Output(&buf); err != nil {
        return nil, fmt.Errorf("failed to generate PDF: %w", err)
    }

    return buf.Bytes(), nil
}

// getSeverityColor retorna RGB para severity badge
func getSeverityColor(severity string) [3]uint8 {
    switch strings.ToLower(severity) {
    case "critical":
        return [3]uint8{239, 68, 68} // Vermelho
    case "high":
        return [3]uint8{249, 115, 22} // Laranja
    case "medium":
        return [3]uint8{234, 179, 8} // Amarelo
    case "low":
        return [3]uint8{34, 197, 94} // Verde
    default:
        return [3]uint8{156, 163, 175} // Cinza
    }
}

// GenerateMarkdown gera relatório Markdown profissional
func GenerateMarkdown(result *ai.AnalysisResult) ([]byte, error) {
    var md strings.Builder

    md.WriteString("# ANALISE DE DIAGNOSTICO - AI\n\n")
    md.WriteString(fmt.Sprintf("**Gerado em:** %s\n\n", time.Now().Format("02/01/2006 15:04:05")))

    md.WriteString("## SUMARIO EXECUTIVO\n\n")
    md.WriteString(fmt.Sprintf("**Severidade:** `%s`\n\n", strings.ToUpper(result.ExecutiveSummary.Severity)))
    md.WriteString(fmt.Sprintf("%s\n\n", result.ExecutiveSummary.QuickSummary))

    // CAUSA RAIZ, IMPACTO, RECOMENDAÇÕES (similar)
    // ... (implementar seções completas)

    return []byte(md.String()), nil
}

// GenerateCSV gera relatório CSV para Excel/BI
func GenerateCSV(result *ai.AnalysisResult) ([]byte, error) {
    var buf bytes.Buffer
    writer := csv.NewWriter(&buf)

    // Headers
    headers := []string{
        "Timestamp", "Cluster", "Namespace", "Resource_Type", "Resource_Name",
        "Severity", "Status", "Quick_Summary", "Symptom", "Confidence",
        "Affected_Users", "Downtime", "SLA_Breach", "Priority",
        "Recommendation_Title", "Risk_Level", "Commands",
    }

    if err := writer.Write(headers); err != nil {
        return nil, fmt.Errorf("failed to write CSV headers: %w", err)
    }

    // Dados (uma linha por recomendação)
    for _, rec := range result.Recommendations {
        row := []string{
            result.AnalyzedAt.Format("2006-01-02 15:04:05"),
            result.Cluster,
            result.Namespace,
            result.ResourceType,
            result.ResourceName,
            result.ExecutiveSummary.Severity,
            result.ExecutiveSummary.Status,
            result.ExecutiveSummary.QuickSummary,
            result.RootCauseAnalysis.Symptom,
            result.RootCauseAnalysis.Confidence,
            result.ImpactAssessment.AffectedUsers,
            result.ImpactAssessment.DowntimeEstimate,
            fmt.Sprintf("%t", result.ImpactAssessment.SLABreach),
            fmt.Sprintf("%d", rec.Priority),
            rec.Title,
            rec.RiskLevel,
            strings.Join(rec.Commands, "; "),
        }

        if err := writer.Write(row); err != nil {
            return nil, fmt.Errorf("failed to write CSV row: %w", err)
        }
    }

    writer.Flush()
    return buf.Bytes(), nil
}
```

### Arquivo: `internal/web/handlers/ai_diagnostics.go`

**Adicionar endpoints (após linha 200)**:

```go
// GetReportPDF exporta análise como PDF
func (h *AIHandler) GetReportPDF(c *gin.Context) {
    analysisID := c.Param("id")

    result, err := h.storage.GetAnalysis(analysisID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "Analysis not found"})
        return
    }

    pdfBytes, err := reports.GeneratePDF(result)
    if err != nil {
        h.logger.Error().Err(err).Msg("Failed to generate PDF")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
        return
    }

    filename := fmt.Sprintf("diagnostico_%s_%s.pdf",
        result.ResourceName,
        result.AnalyzedAt.Format("2006-01-02"),
    )

    c.Header("Content-Type", "application/pdf")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
    c.Data(http.StatusOK, "application/pdf", pdfBytes)
}

// GetReportMarkdown e GetReportCSV (similar)
```

### Arquivo: `internal/web/server.go`

**Adicionar rotas (após linha 170)**:

```go
aiGroup.GET("/report/:id/pdf", aiHandler.GetReportPDF)
aiGroup.GET("/report/:id/markdown", aiHandler.GetReportMarkdown)
aiGroup.GET("/report/:id/csv", aiHandler.GetReportCSV)
```

**Dependências:**
```bash
go get github.com/jung-kurt/gofpdf
```

**Riscos:**
- PDF muito grande (>10MB)
- **Mitigação:** Exportação NÃO inclui logs brutos, apenas análise

**Testes:**
- [ ] PDF gerado sem erros (Adobe Reader, Chrome)
- [ ] Markdown formatado (VS Code preview)
- [ ] CSV abre no Excel sem encoding issues

---

## 🔄 FASE 5: Frontend - Componente Visualização (5-6h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Alta
**Dependências:** Fases 1-4 concluídas

### Arquivo: `internal/web/frontend/src/components/AIAnalysisView.tsx` (CRIAR)

```typescript
import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion';
import { AlertTriangle, CheckCircle2, AlertCircle, Info, Star, Download } from 'lucide-react';
import { AnalysisResult } from '@/types/ai';
import { apiClient } from '@/lib/api/client';
import { useToast } from '@/hooks/useToast';

interface AIAnalysisViewProps {
  analysis: AnalysisResult;
  showExportButtons?: boolean;
}

export const AIAnalysisView: React.FC<AIAnalysisViewProps> = ({
  analysis,
  showExportButtons = true,
}) => {
  const { toast } = useToast();
  const [exportingFormat, setExportingFormat] = React.useState<string | null>(null);

  // Exportação
  const handleExport = async (format: 'pdf' | 'markdown' | 'csv') => {
    setExportingFormat(format);

    try {
      const blob = await apiClient.exportAIReport(analysis.id, format);
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;

      const extension = format === 'markdown' ? 'md' : format;
      a.download = `diagnostico_${analysis.resource_name}_${new Date(analysis.analyzed_at).toISOString().split('T')[0]}.${extension}`;

      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);

      toast({
        title: 'Exportação concluída',
        description: `Relatório exportado como ${format.toUpperCase()}`,
      });
    } catch (error) {
      toast({
        title: 'Erro na exportação',
        description: error instanceof Error ? error.message : 'Falha ao exportar',
        variant: 'destructive',
      });
    } finally {
      setExportingFormat(null);
    }
  };

  // Severity badge config
  const getSeverityBadge = (severity: string) => {
    const config = {
      critical: { icon: AlertTriangle, variant: 'destructive' as const, label: 'CRÍTICO' },
      high: { icon: AlertCircle, variant: 'destructive' as const, label: 'ALTO' },
      medium: { icon: Info, variant: 'default' as const, label: 'MÉDIO' },
      low: { icon: CheckCircle2, variant: 'secondary' as const, label: 'BAIXO' },
    };
    return config[severity.toLowerCase()] || config.low;
  };

  const severityConfig = getSeverityBadge(analysis.executive_summary.severity);
  const SeverityIcon = severityConfig.icon;

  // Priority stars
  const renderPriorityStars = (priority: number) => {
    return (
      <div className="flex items-center gap-1">
        {Array.from({ length: 5 }).map((_, i) => (
          <Star
            key={i}
            className={`h-4 w-4 ${i < priority ? 'fill-yellow-400 text-yellow-400' : 'text-gray-300'}`}
          />
        ))}
      </div>
    );
  };

  return (
    <div className="space-y-6">
      {/* Header com botões de exportação */}
      {showExportButtons && (
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleExport('pdf')}
            disabled={exportingFormat === 'pdf'}
          >
            <Download className="mr-2 h-4 w-4" />
            {exportingFormat === 'pdf' ? 'Exportando...' : 'PDF'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleExport('markdown')}
            disabled={exportingFormat === 'markdown'}
          >
            <Download className="mr-2 h-4 w-4" />
            {exportingFormat === 'markdown' ? 'Exportando...' : 'Markdown'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleExport('csv')}
            disabled={exportingFormat === 'csv'}
          >
            <Download className="mr-2 h-4 w-4" />
            {exportingFormat === 'csv' ? 'Exportando...' : 'CSV'}
          </Button>
        </div>
      )}

      {/* 1. SUMÁRIO EXECUTIVO */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <span>Sumário Executivo</span>
            <Badge variant={severityConfig.variant} className="flex items-center gap-2">
              <SeverityIcon className="h-4 w-4" />
              {severityConfig.label}
            </Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-lg">{analysis.executive_summary.quick_summary}</p>

          <div className="grid grid-cols-3 gap-4">
            <div>
              <div className="text-sm text-muted-foreground mb-1">Status</div>
              <Badge variant="outline">{analysis.executive_summary.status}</Badge>
            </div>
            <div>
              <div className="text-sm text-muted-foreground mb-1">Tempo Estimado</div>
              <div className="font-medium">{analysis.executive_summary.time_to_resolve}</div>
            </div>
            <div>
              <div className="text-sm text-muted-foreground mb-1">Provedor IA</div>
              <div className="font-medium">{analysis.provider} ({analysis.model})</div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 2. ANÁLISE DE CAUSA RAIZ */}
      <Card>
        <CardHeader>
          <CardTitle>Análise de Causa Raiz</CardTitle>
        </CardHeader>
        <CardContent>
          <Accordion type="single" collapsible className="w-full">
            <AccordionItem value="symptom">
              <AccordionTrigger>
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-5 w-5 text-orange-500" />
                  <span className="font-semibold">Sintoma Identificado</span>
                </div>
              </AccordionTrigger>
              <AccordionContent>
                <p className="text-base">{analysis.root_cause_analysis.symptom}</p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value="causes">
              <AccordionTrigger>
                <div className="flex items-center gap-2">
                  <Info className="h-5 w-5 text-blue-500" />
                  <span className="font-semibold">Causas Prováveis</span>
                </div>
              </AccordionTrigger>
              <AccordionContent>
                <ol className="list-decimal list-inside space-y-2">
                  {analysis.root_cause_analysis.probable_causes.map((cause, idx) => (
                    <li key={idx} className="text-base">{cause}</li>
                  ))}
                </ol>
                <div className="mt-4">
                  <Badge variant="outline">
                    Confiança: {analysis.root_cause_analysis.confidence.toUpperCase()}
                  </Badge>
                </div>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value="evidence">
              <AccordionTrigger>
                <div className="flex items-center gap-2">
                  <CheckCircle2 className="h-5 w-5 text-green-500" />
                  <span className="font-semibold">Evidências</span>
                </div>
              </AccordionTrigger>
              <AccordionContent>
                <ul className="list-disc list-inside space-y-2">
                  {analysis.root_cause_analysis.evidence.map((ev, idx) => (
                    <li key={idx} className="text-base">{ev}</li>
                  ))}
                </ul>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </CardContent>
      </Card>

      {/* 3. IMPACTO E SEVERIDADE */}
      <Card>
        <CardHeader>
          <CardTitle>Impacto e Severidade</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
            <Card>
              <CardContent className="pt-6">
                <div className="text-2xl font-bold">{analysis.impact_assessment.affected_users}</div>
                <p className="text-xs text-muted-foreground mt-1">Usuários Afetados</p>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="pt-6">
                <div className="text-2xl font-bold">{analysis.impact_assessment.downtime_estimate}</div>
                <p className="text-xs text-muted-foreground mt-1">Downtime Estimado</p>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="pt-6">
                <div className="text-2xl font-bold">
                  {analysis.impact_assessment.sla_breach ? (
                    <span className="text-red-500">SIM</span>
                  ) : (
                    <span className="text-green-500">NÃO</span>
                  )}
                </div>
                <p className="text-xs text-muted-foreground mt-1">Violação de SLA</p>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="pt-6">
                <Badge variant={severityConfig.variant} className="text-sm">
                  {severityConfig.label}
                </Badge>
                <p className="text-xs text-muted-foreground mt-1">Severidade</p>
              </CardContent>
            </Card>
          </div>

          <div className="bg-muted p-4 rounded-lg">
            <h4 className="font-semibold mb-2">Impacto no Negócio</h4>
            <p className="text-sm">{analysis.impact_assessment.business_impact}</p>
          </div>
        </CardContent>
      </Card>

      {/* 4. AÇÕES RECOMENDADAS */}
      <Card>
        <CardHeader>
          <CardTitle>Ações Recomendadas (Priorizadas)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {analysis.recommendations
              .sort((a, b) => a.priority - b.priority)
              .map((rec, idx) => (
                <Card key={idx} className="border-l-4 border-l-blue-500">
                  <CardHeader>
                    <div className="flex items-start justify-between">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          {renderPriorityStars(rec.priority)}
                          <span className="text-sm text-muted-foreground">
                            Prioridade {rec.priority}
                          </span>
                        </div>
                        <CardTitle className="text-lg">{rec.title}</CardTitle>
                      </div>
                      <Badge
                        variant={
                          rec.risk_level === 'high'
                            ? 'destructive'
                            : rec.risk_level === 'medium'
                            ? 'default'
                            : 'secondary'
                        }
                      >
                        Risco: {rec.risk_level}
                      </Badge>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <p className="text-sm">{rec.description}</p>

                    {rec.commands.length > 0 && (
                      <div>
                        <h5 className="text-sm font-semibold mb-2">Comandos:</h5>
                        <div className="bg-muted p-3 rounded-md space-y-1 font-mono text-xs">
                          {rec.commands.map((cmd, cmdIdx) => (
                            <div key={cmdIdx} className="whitespace-pre-wrap break-all">
                              $ {cmd}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    <div className="flex items-center gap-4 text-sm text-muted-foreground">
                      <span>⏱️ {rec.time_estimate}</span>
                      <span>📈 Impacto: {rec.impact_level}</span>
                    </div>
                  </CardContent>
                </Card>
              ))}
          </div>
        </CardContent>
      </Card>

      {/* 5. MÉTRICAS ATUAIS (se disponível) */}
      {analysis.current_metrics && (
        <Card>
          <CardHeader>
            <CardTitle>Métricas Atuais (Prometheus)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-6">
              {/* CPU */}
              <div className="space-y-2">
                <h4 className="font-semibold">CPU</h4>
                <div className="space-y-1 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Uso Atual:</span>
                    <span className="font-medium">
                      {analysis.current_metrics.cpu_usage.toFixed(2)} millicores
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Request:</span>
                    <span>{analysis.current_metrics.cpu_request.toFixed(2)} millicores</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Limit:</span>
                    <span>{analysis.current_metrics.cpu_limit.toFixed(2)} millicores</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Utilização:</span>
                    <Badge
                      variant={
                        (analysis.current_metrics.cpu_usage / analysis.current_metrics.cpu_limit) * 100 > 80
                          ? 'destructive'
                          : 'secondary'
                      }
                    >
                      {((analysis.current_metrics.cpu_usage / analysis.current_metrics.cpu_limit) * 100).toFixed(1)}%
                    </Badge>
                  </div>
                </div>
              </div>

              {/* Memory */}
              <div className="space-y-2">
                <h4 className="font-semibold">Memória</h4>
                <div className="space-y-1 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Uso Atual:</span>
                    <span className="font-medium">
                      {analysis.current_metrics.memory_usage.toFixed(2)} MB
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Request:</span>
                    <span>{analysis.current_metrics.memory_request.toFixed(2)} MB</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Limit:</span>
                    <span>{analysis.current_metrics.memory_limit.toFixed(2)} MB</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Utilização:</span>
                    <Badge
                      variant={
                        (analysis.current_metrics.memory_usage / analysis.current_metrics.memory_limit) * 100 > 80
                          ? 'destructive'
                          : 'secondary'
                      }
                    >
                      {((analysis.current_metrics.memory_usage / analysis.current_metrics.memory_limit) * 100).toFixed(1)}%
                    </Badge>
                  </div>
                </div>
              </div>

              {/* Réplicas */}
              <div className="space-y-2 col-span-2">
                <h4 className="font-semibold">Réplicas</h4>
                <div className="flex items-center gap-6 text-sm">
                  <div>
                    <span className="text-muted-foreground">Prontas/Desejadas: </span>
                    <span className="font-medium">
                      {analysis.current_metrics.ready_replicas}/{analysis.current_metrics.desired_replicas}
                    </span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Restart Count: </span>
                    <Badge variant={analysis.current_metrics.restart_count > 5 ? 'destructive' : 'secondary'}>
                      {analysis.current_metrics.restart_count}
                    </Badge>
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
};
```

### Arquivo: `internal/web/frontend/src/types/ai.ts`

**Adicionar tipos (após linha 30)**:

```typescript
export interface ExecutiveSummary {
  severity: 'critical' | 'high' | 'medium' | 'low';
  status: string;
  quick_summary: string;
  time_to_resolve: string;
}

export interface RootCauseAnalysis {
  symptom: string;
  probable_causes: string[];
  evidence: string[];
  confidence: 'high' | 'medium' | 'low';
}

export interface ImpactAssessment {
  severity: 'critical' | 'high' | 'medium' | 'low';
  affected_users: string;
  downtime_estimate: string;
  sla_breach: boolean;
  business_impact: string;
}

export interface ActionableRecommendation {
  priority: number; // 1-5
  title: string;
  description: string;
  commands: string[];
  time_estimate: string;
  risk_level: 'low' | 'medium' | 'high';
  impact_level: string;
}

export interface ResourceMetrics {
  cpu_usage: number;
  cpu_request: number;
  cpu_limit: number;
  memory_usage: number;
  memory_request: number;
  memory_limit: number;
  restart_count: number;
  ready_replicas: number;
  desired_replicas: number;
}

// Atualizar AnalysisResult
export interface AnalysisResult {
  id: string;
  resource_type: string;
  cluster: string;
  namespace: string;
  resource_name: string;

  // Estrutura nova
  executive_summary: ExecutiveSummary;
  root_cause_analysis: RootCauseAnalysis;
  impact_assessment: ImpactAssessment;
  recommendations: ActionableRecommendation[];

  // Campos legados
  analysis?: string;
  suggestions?: Suggestion[];

  // Métricas
  current_metrics?: ResourceMetrics;

  // Metadados
  provider: string;
  model: string;
  tokens_used?: number;
  response_time?: number;
  analyzed_at: string;
  error?: string;
}
```

### Arquivo: `internal/web/frontend/src/lib/api/client.ts`

**Adicionar (após linha 800)**:

```typescript
async exportAIReport(
  analysisId: string,
  format: 'pdf' | 'markdown' | 'csv'
): Promise<Blob> {
  const response = await fetch(`/api/v1/ai/report/${analysisId}/${format}`);

  if (!response.ok) {
    throw new Error(`Failed to export report: ${response.statusText}`);
  }

  return response.blob();
}
```

**Riscos:**
- Componente muito grande (700+ linhas)
- **Mitigação:** Dividir em sub-componentes se necessário

**Testes:**
- [ ] Renderiza 4 seções corretamente
- [ ] Priority stars (1-5) corretas
- [ ] Exportação funciona (PDF/MD/CSV)
- [ ] Responsivo (mobile/tablet/desktop)

---

## 🔄 FASE 6: Frontend - Refatorar Modal/Página (2h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Baixa
**Dependências:** Fase 5 concluída

### Arquivo: `AIAnalysisModal.tsx`

**Substituir conteúdo (manter estrutura Dialog)**:

```typescript
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { AIAnalysisView } from './AIAnalysisView';
import { ScrollArea } from '@/components/ui/scroll-area';

export function AIAnalysisModal({ analysis, open, onOpenChange }) {
  if (!analysis) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-7xl max-h-[90vh] p-0">
        <DialogHeader className="p-6 pb-0">
          <DialogTitle className="text-2xl">
            Análise de Diagnóstico - {analysis.resource_type}/{analysis.resource_name}
          </DialogTitle>
          <div className="text-sm text-muted-foreground">
            {analysis.cluster} / {analysis.namespace}
          </div>
        </DialogHeader>

        <ScrollArea className="h-full px-6 pb-6">
          <AIAnalysisView analysis={analysis} showExportButtons={true} />
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
```

### Arquivo: `AIAnalysisPage.tsx`

**Substituir renderização** (manter navegação):

```typescript
{selectedAnalysis ? (
  <AIAnalysisView analysis={selectedAnalysis} showExportButtons={true} />
) : (
  <div className="flex items-center justify-center h-96">
    <p className="text-muted-foreground">
      Selecione uma análise do histórico para visualizar
    </p>
  </div>
)}
```

**Testes:**
- [ ] Modal exibe análise completa
- [ ] Página renderiza sem modal
- [ ] Exportação funciona em ambos

---

## 🔄 FASE 7: Frontend - Histórico Avançado (3h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Média
**Dependências:** Fases 5-6 concluídas

### Arquivo: `AIDiagnosticsTab.tsx`

**Adicionar filtros (antes da tabela)**:

```typescript
const [filters, setFilters] = useState({
  cluster: '',
  namespace: '',
  severity: '',
  provider: '',
  dateFrom: '',
  dateTo: '',
});

<Card>
  <CardHeader><CardTitle>Filtros</CardTitle></CardHeader>
  <CardContent>
    <div className="grid grid-cols-3 md:grid-cols-6 gap-4">
      <div>
        <Label>Cluster</Label>
        <Select value={filters.cluster} onValueChange={(value) => setFilters({ ...filters, cluster: value })}>
          <SelectTrigger><SelectValue placeholder="Todos" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="">Todos</SelectItem>
            {uniqueClusters.map(c => <SelectItem key={c} value={c}>{c}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      {/* Namespace, Severity, Provider similar */}

      <div>
        <Label>Data Início</Label>
        <Input
          type="date"
          value={filters.dateFrom}
          onChange={(e) => setFilters({ ...filters, dateFrom: e.target.value })}
        />
      </div>

      <div>
        <Label>Data Fim</Label>
        <Input
          type="date"
          value={filters.dateTo}
          onChange={(e) => setFilters({ ...filters, dateTo: e.target.value })}
        />
      </div>
    </div>
  </CardContent>
</Card>
```

**Adicionar badges na tabela**:

```typescript
<TableRow className="cursor-pointer hover:bg-muted" onClick={() => handleView(item.id)}>
  <TableCell>{new Date(item.analyzed_at).toLocaleString('pt-BR')}</TableCell>
  <TableCell>{item.cluster}</TableCell>
  <TableCell>{item.namespace}</TableCell>
  <TableCell>{item.resource_type}/{item.resource_name}</TableCell>
  <TableCell>
    <Badge variant={getSeverityVariant(item.executive_summary?.severity || item.priority)}>
      <AlertTriangle className="h-3 w-3 mr-1" />
      {(item.executive_summary?.severity || item.priority).toUpperCase()}
    </Badge>
  </TableCell>
  <TableCell>{item.provider}</TableCell>
  <TableCell>
    <Button variant="outline" size="sm">Visualizar</Button>
  </TableCell>
</TableRow>
```

**Testes:**
- [ ] Filtros funcionam corretamente
- [ ] Tabela ordenada por data
- [ ] Badges de severity corretos

---

## 🔄 FASE 8: Testes e Validação (4-5h)

**Status:** ⬜ Não Iniciado
**Complexidade:** Média
**Dependências:** Todas as fases anteriores

### Checklist de Validação

**Backend:**
- [ ] `AnalysisResult` serializa/deserializa JSON corretamente
- [ ] Campos legados mantidos por compatibilidade
- [ ] Análises antigas exibidas sem erros
- [ ] Prometheus queries retornam métricas corretas
- [ ] Graceful degradation quando Prometheus inacessível
- [ ] Claude retorna JSON válido (5+ análises testadas)
- [ ] Gemini retorna JSON válido (5+ análises testadas)
- [ ] Fallback funciona quando JSON inválido
- [ ] Limpeza de markdown wrappers funciona
- [ ] PDF gerado sem erros (Adobe Reader, Chrome)
- [ ] Markdown formatado corretamente (VS Code)
- [ ] CSV abre no Excel sem encoding issues
- [ ] Filenames seguem padrão `diagnostico_{deployment}_{data}.{ext}`

**Frontend:**
- [ ] Renderiza 4 seções corretamente
- [ ] Badges de severity com cores corretas
- [ ] Priority stars (1-5) renderizam corretamente
- [ ] Métricas Prometheus formatadas (2 decimais)
- [ ] Componente responsivo (mobile, tablet, desktop)
- [ ] Modal fullscreen exibe análise completa
- [ ] Página dedicada renderiza análise
- [ ] Exportação funciona em ambos (modal e página)
- [ ] Botões de exportação não duplicam downloads
- [ ] Filtros funcionam (cluster, namespace, severity, provider, data)
- [ ] Tabela ordenada por data (mais recente primeiro)
- [ ] Click em row abre modal de visualização
- [ ] Badges de severity corretos na tabela

**End-to-End:**
- [ ] Botão "Analisar com AI" executa análise
- [ ] Análise salva no histórico (SQLite)
- [ ] Análise aparece na aba AI Diagnostics
- [ ] Exportação gera relatório com dados corretos

### Casos de Teste

**Cenário 1: CrashLoopBackOff**
- Cluster: `abastecimento-hlg`
- Pod: `ms-faturamento-api-7d8f5b9c4d-xk7m2`
- Esperado:
  - Severity: `critical`
  - Symptom: "CrashLoopBackOff"
  - Probable Causes: connection timeout, missing config, OOM
  - Evidence: últimas 30 linhas de logs anteriores

**Cenário 2: OOMKilled**
- Cluster: `producao-prd`
- Pod: `ms-estoque-worker-5f8b7c9d4e-abc12`
- Esperado:
  - Severity: `high`
  - Symptom: "OOMKilled"
  - Recommendations: aumentar memory limit (Priority 1)
  - Current Metrics: Memory Usage > 95% do limit

**Cenário 3: Healthy Pod**
- Cluster: `desenvolvimento-dev`
- Pod: `nginx-demo-12345`
- Esperado:
  - Severity: `low`
  - Status: "healthy"
  - Recommendations: otimizações opcionais (Priority 4-5)

### Comandos de Teste

```bash
# Teste de race conditions
go test -v ./internal/ai -race
go test -v ./internal/collectors -race

# Teste de parsing JSON
curl http://localhost:8080/api/v1/ai/analyze \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "cluster": "abastecimento-hlg",
    "namespace": "default",
    "resource_type": "Pod",
    "resource_name": "ms-faturamento-api-7d8f5b9c4d-xk7m2"
  }' | jq .

# Teste de exportação PDF
curl http://localhost:8080/api/v1/ai/report/{id}/pdf > test.pdf

# Teste de exportação Markdown
curl http://localhost:8080/api/v1/ai/report/{id}/markdown > test.md
```

---

## ⚙️ Dependências

**Go:**
```bash
go get github.com/jung-kurt/gofpdf
```

**Frontend:**
- Componentes shadcn/ui já existem (Accordion, Badge, Card)
- Ícones lucide-react já instalados

---

## ⏱️ Estimativa Total

| Fase | Duração | Risco |
|------|---------|-------|
| FASE 1: Estrutura de Dados | 3-4h | Médio |
| FASE 2: Coleta Prometheus | 2-3h | Médio |
| FASE 3: Prompts JSON | 4-5h | Alto |
| FASE 4: Exportação | 3-4h | Médio |
| FASE 5: Componente Visualização | 5-6h | Alto |
| FASE 6: Modal/Página | 2h | Baixo |
| FASE 7: Histórico | 3h | Médio |
| FASE 8: Testes | 4-5h | Médio |
| **TOTAL** | **26-34 horas** | **~5-6 dias** |

---

## 🚨 Riscos e Mitigações

| Risco | Mitigação |
|-------|-----------|
| IA retornar JSON inválido | Fallback para formato legado (campo `analysis`) |
| Prometheus inacessível | Graceful degradation, logs warn mas continua |
| Breaking changes em código legado | Manter campos `analysis` e `suggestions` |
| Queries Prometheus incompatíveis | Usar OR operator (padrão predictions/queries.go) |
| PDF muito grande (>10MB) | Exportação NÃO inclui logs brutos |
| Componente muito grande (700+ LOC) | Dividir em sub-componentes se necessário |

---

## ✅ Critérios de Aceite

- [ ] 100% dos testes unitários Go passam
- [ ] Claude retorna JSON estruturado (5+ análises testadas)
- [ ] Exportação PDF/MD/CSV sem emojis quebrados
- [ ] Modal e página renderizam 4 seções corretamente
- [ ] Filtros de histórico funcionam
- [ ] Tempo de análise <30s (Claude), <10s (Gemini)
- [ ] Nenhum erro de console no navegador React
- [ ] Histórico com 100+ análises ainda performático

---

## 📝 Próximos Passos

1. ✅ Plano validado e salvo na raiz do projeto
2. Criar branch: `feature/ai-diagnostics-refactor`
3. Implementar fases sequencialmente (FASE 1 → FASE 8)
4. Testar cada fase isoladamente antes de avançar
5. Atualizar `CLAUDE.md` ao final com novas features

---

**Última Atualização:** 06 de janeiro de 2026
**Autor:** Claude Code + Paulo Ribeiro
**Arquivo:** `/home/paulo/Scripts/Scripts GO/New-K8s-HPA-Manager/Scale_HPA/PLANO_REFATORACAO_AI_DIAGNOSTICS.md`
