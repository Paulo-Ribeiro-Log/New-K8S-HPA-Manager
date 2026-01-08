package reports

import (
	"strings"
	"testing"
	"time"

	"k8s-hpa-manager/internal/ai"
)

// createSampleAnalysis cria análise de exemplo para testes
func createSampleAnalysis() *ai.AnalysisResult {
	now := time.Now()

	return &ai.AnalysisResult{
		ID:           "test-analysis-123",
		ResourceType: "Pod",
		Cluster:      "test-cluster",
		Namespace:    "default",
		ResourceName: "nginx-pod-abc123",
		Provider:     "ollama",
		Model:        "llama3.2:3b",
		Analysis:     "# Análise Detalhada\n\nO pod está em estado CrashLoopBackOff devido a erro de configuração.\n\n## Causa Raiz\n\nConfigMap ausente.",
		Suggestions: []ai.Suggestion{
			{
				Type:        "fix",
				Description: "Criar ConfigMap com as configurações necessárias",
				Command:     "kubectl create configmap app-config --from-file=config.yaml",
				Priority:    "high",
			},
		},
		TokensUsed:   1500,
		ResponseTime: 3.5,
		AnalyzedAt:   now,
	}
}

// createStructuredAnalysis cria análise estruturada com todas as seções
func createStructuredAnalysis() *ai.AnalysisResult {
	now := time.Now()

	return &ai.AnalysisResult{
		ID:           "test-structured-456",
		ResourceType: "Deployment",
		Cluster:      "production-cluster",
		Namespace:    "apps",
		ResourceName: "api-deployment",
		Provider:     "claude",
		Model:        "claude-sonnet-4",

		// Seções estruturadas
		ExecutiveSummary: ai.ExecutiveSummary{
			Severity:      "critical",
			Status:        "unhealthy",
			QuickSummary:  "Deployment apresenta falhas críticas com 0/3 réplicas disponíveis",
			TimeToResolve: "15 minutos",
		},

		RootCauseAnalysis: ai.RootCauseAnalysis{
			Symptom:        "CrashLoopBackOff em todos os pods",
			ProbableCauses: []string{"ConfigMap ausente", "Credenciais inválidas", "Limite de recursos insuficiente"},
			Evidence:       []string{"Error: config.yaml not found", "Failed to connect to database", "OOMKilled"},
			Confidence:     "high",
		},

		ImpactAssessment: ai.ImpactAssessment{
			Severity:         "critical",
			AffectedUsers:    "100% dos usuários",
			DowntimeEstimate: "30 minutos",
			SLABreach:        true,
			BusinessImpact:   "API completamente indisponível, bloqueando operações críticas",
		},

		Recommendations: []ai.ActionableRecommendation{
			{
				Priority:     1,
				Title:        "Criar ConfigMap ausente",
				Description:  "ConfigMap 'app-config' não foi encontrado. Criar com as configurações necessárias.",
				Commands:     []string{"kubectl create configmap app-config --from-file=config.yaml", "kubectl rollout restart deployment/api-deployment"},
				TimeEstimate: "5 minutos",
				RiskLevel:    "low",
				ImpactLevel:  "high",
			},
			{
				Priority:     2,
				Title:        "Aumentar limites de memória",
				Description:  "Pods sendo OOMKilled. Aumentar memory limit de 512Mi para 1Gi.",
				Commands:     []string{"kubectl set resources deployment/api-deployment --limits=memory=1Gi"},
				TimeEstimate: "2 minutos",
				RiskLevel:    "medium",
				ImpactLevel:  "high",
			},
		},

		CurrentMetrics: &ai.ResourceMetrics{
			CPUUsage:        250.5,
			CPURequest:      500.0,
			CPULimit:        1000.0,
			MemoryUsage:     512.0,
			MemoryRequest:   768.0,
			MemoryLimit:     1024.0,
			RestartCount:    5,
			ReadyReplicas:   0,
			DesiredReplicas: 3,
		},

		ResourceMetadata: &ai.ResourceMetadata{
			Name:         "api-deployment",
			Namespace:    "apps",
			Cluster:      "production-cluster",
			Type:         "Deployment",
			Version:      "v1.2.3",
			Status:       "Unhealthy",
			Phase:        "Failed",
			Age:          "2d5h",
			RestartCount: 5,
			NodeName:     "worker-node-01",
			ContainerImages: []string{
				"myapp/api:v1.2.3",
				"nginx:1.21.0",
			},
			Labels: map[string]string{
				"app":     "api",
				"env":     "production",
				"version": "v1.2.3",
			},
			Resources: ai.ResourceSummary{
				CPU: ai.ResourceDetail{
					Current: "250.5m",
					Request: "500m",
					Limit:   "1000m",
				},
				Memory: ai.ResourceDetail{
					Current: "512Mi",
					Request: "768Mi",
					Limit:   "1Gi",
				},
			},
			CollectedAt: now,
		},

		TokensUsed:   3500,
		ResponseTime: 8.2,
		AnalyzedAt:   now,
	}
}

func TestGeneratePDF(t *testing.T) {
	t.Run("PDF básico - análise legada", func(t *testing.T) {
		analysis := createSampleAnalysis()

		pdfBytes, err := GeneratePDF(analysis)
		if err != nil {
			t.Fatalf("GeneratePDF() falhou: %v", err)
		}

		if len(pdfBytes) == 0 {
			t.Fatal("PDF gerado está vazio")
		}

		// Verificar cabeçalho PDF
		if !strings.HasPrefix(string(pdfBytes[:4]), "%PDF") {
			t.Error("PDF não tem cabeçalho válido")
		}

		t.Logf("✅ PDF gerado com sucesso: %d bytes", len(pdfBytes))
	})

	t.Run("PDF estruturado completo", func(t *testing.T) {
		analysis := createStructuredAnalysis()

		pdfBytes, err := GeneratePDF(analysis)
		if err != nil {
			t.Fatalf("GeneratePDF() falhou: %v", err)
		}

		if len(pdfBytes) == 0 {
			t.Fatal("PDF gerado está vazio")
		}

		// Verificar cabeçalho PDF
		if !strings.HasPrefix(string(pdfBytes[:4]), "%PDF") {
			t.Error("PDF não tem cabeçalho válido")
		}

		t.Logf("✅ PDF estruturado gerado com sucesso: %d bytes", len(pdfBytes))
	})
}

func TestGenerateMarkdown(t *testing.T) {
	t.Run("Markdown básico - análise legada", func(t *testing.T) {
		analysis := createSampleAnalysis()

		mdBytes, err := GenerateMarkdown(analysis)
		if err != nil {
			t.Fatalf("GenerateMarkdown() falhou: %v", err)
		}

		if len(mdBytes) == 0 {
			t.Fatal("Markdown gerado está vazio")
		}

		mdContent := string(mdBytes)

		// Verificar estrutura básica
		if !strings.Contains(mdContent, "# ANALISE DE DIAGNOSTICO") {
			t.Error("Markdown não contém título principal")
		}

		if !strings.Contains(mdContent, analysis.ResourceName) {
			t.Error("Markdown não contém nome do recurso")
		}

		if !strings.Contains(mdContent, analysis.Cluster) {
			t.Error("Markdown não contém cluster")
		}

		// Verificar que NÃO contém emojis
		emojis := []string{"📊", "🔍", "⚠️", "✅", "❌"}
		for _, emoji := range emojis {
			if strings.Contains(mdContent, emoji) {
				t.Errorf("Markdown contém emoji proibido: %s", emoji)
			}
		}

		t.Logf("✅ Markdown gerado com sucesso: %d bytes", len(mdBytes))
	})

	t.Run("Markdown estruturado completo", func(t *testing.T) {
		analysis := createStructuredAnalysis()

		mdBytes, err := GenerateMarkdown(analysis)
		if err != nil {
			t.Fatalf("GenerateMarkdown() falhou: %v", err)
		}

		mdContent := string(mdBytes)

		// Verificar seções estruturadas
		expectedSections := []string{
			"## SUMARIO EXECUTIVO",
			"## ANALISE DE CAUSA RAIZ",
			"## IMPACTO E SEVERIDADE",
			"## ACOES RECOMENDADAS",
		}

		for _, section := range expectedSections {
			if !strings.Contains(mdContent, section) {
				t.Errorf("Markdown não contém seção: %s", section)
			}
		}

		// Verificar dados do executive summary (uppercase no Markdown)
		if !strings.Contains(mdContent, strings.ToUpper(analysis.ExecutiveSummary.Severity)) {
			t.Errorf("Markdown não contém severity (esperado: %s)", strings.ToUpper(analysis.ExecutiveSummary.Severity))
		}

		t.Logf("✅ Markdown estruturado gerado com sucesso: %d bytes", len(mdBytes))
	})
}

func TestGenerateCSV(t *testing.T) {
	t.Run("CSV básico - análise legada", func(t *testing.T) {
		analysis := createSampleAnalysis()

		csvBytes, err := GenerateCSV(analysis)
		if err != nil {
			t.Fatalf("GenerateCSV() falhou: %v", err)
		}

		if len(csvBytes) == 0 {
			t.Fatal("CSV gerado está vazio")
		}

		csvContent := string(csvBytes)

		// Verificar cabeçalho CSV (título principal)
		if !strings.Contains(csvContent, "ANALISE DE DIAGNOSTICO") {
			t.Error("CSV não contém cabeçalho principal")
		}

		// Verificar campos obrigatórios
		requiredFields := []string{
			analysis.ResourceName,
			analysis.Cluster,
			analysis.Namespace,
			analysis.Provider,
		}

		for _, field := range requiredFields {
			if !strings.Contains(csvContent, field) {
				t.Errorf("CSV não contém campo obrigatório: %s", field)
			}
		}

		t.Logf("✅ CSV gerado com sucesso: %d bytes", len(csvBytes))
	})

	t.Run("CSV estruturado completo", func(t *testing.T) {
		analysis := createStructuredAnalysis()

		csvBytes, err := GenerateCSV(analysis)
		if err != nil {
			t.Fatalf("GenerateCSV() falhou: %v", err)
		}

		csvContent := string(csvBytes)

		// Verificar seções estruturadas
		if !strings.Contains(csvContent, "SUMARIO EXECUTIVO") {
			t.Error("CSV não contém seção de sumário executivo")
		}

		if !strings.Contains(csvContent, "ACOES RECOMENDADAS") {
			t.Error("CSV não contém seção de recomendações")
		}

		// Verificar que recommendations estão no CSV
		if !strings.Contains(csvContent, analysis.Recommendations[0].Title) {
			t.Error("CSV não contém título da primeira recomendação")
		}

		t.Logf("✅ CSV estruturado gerado com sucesso: %d bytes", len(csvBytes))
	})
}

func TestNoEmojisInReports(t *testing.T) {
	analysis := createStructuredAnalysis()

	// Lista de emojis proibidos
	forbiddenEmojis := []string{
		"📊", "🔍", "⚠️", "✅", "❌", "🚀", "💡", "🎯",
		"📈", "📉", "🔥", "⚡", "🌟", "💻", "🐛", "🔧",
	}

	t.Run("PDF sem emojis", func(t *testing.T) {
		pdfBytes, err := GeneratePDF(analysis)
		if err != nil {
			t.Fatalf("GeneratePDF() falhou: %v", err)
		}

		pdfContent := string(pdfBytes)
		for _, emoji := range forbiddenEmojis {
			if strings.Contains(pdfContent, emoji) {
				t.Errorf("PDF contém emoji proibido: %s", emoji)
			}
		}

		t.Log("✅ PDF livre de emojis")
	})

	t.Run("Markdown sem emojis", func(t *testing.T) {
		mdBytes, err := GenerateMarkdown(analysis)
		if err != nil {
			t.Fatalf("GenerateMarkdown() falhou: %v", err)
		}

		mdContent := string(mdBytes)
		for _, emoji := range forbiddenEmojis {
			if strings.Contains(mdContent, emoji) {
				t.Errorf("Markdown contém emoji proibido: %s", emoji)
			}
		}

		t.Log("✅ Markdown livre de emojis")
	})

	t.Run("CSV sem emojis", func(t *testing.T) {
		csvBytes, err := GenerateCSV(analysis)
		if err != nil {
			t.Fatalf("GenerateCSV() falhou: %v", err)
		}

		csvContent := string(csvBytes)
		for _, emoji := range forbiddenEmojis {
			if strings.Contains(csvContent, emoji) {
				t.Errorf("CSV contém emoji proibido: %s", emoji)
			}
		}

		t.Log("✅ CSV livre de emojis")
	})
}
