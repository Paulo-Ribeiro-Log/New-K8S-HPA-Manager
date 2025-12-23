package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s-hpa-manager/internal/collectors"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/sanitizer"
	"k8s-hpa-manager/internal/storage"

	"github.com/google/uuid"
)

// Analyzer orquestra análises AI de recursos Kubernetes
type Analyzer struct {
	provider       Provider
	contextBuilder *collectors.ContextBuilder
	promptBuilder  *PromptBuilder
	sanitizer      *sanitizer.Sanitizer
	historyStore   *storage.AIHistoryStore
}

// NewAnalyzer cria um novo Analyzer
func NewAnalyzer(
	provider Provider,
	kubeManager *kubernetes.KubeManager,
	historyStore *storage.AIHistoryStore,
) *Analyzer {
	return &Analyzer{
		provider:       provider,
		contextBuilder: collectors.NewContextBuilder(kubeManager),
		promptBuilder:  NewPromptBuilder(),
		sanitizer:      sanitizer.New(),
		historyStore:   historyStore,
	}
}

// Analyze executa análise completa de um recurso
func (a *Analyzer) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error) {
	startTime := time.Now()

	// 1. Coletar contexto de diagnóstico
	contextReq := &collectors.ContextRequest{
		ResourceType:    req.ResourceType,
		Cluster:         req.Cluster,
		Namespace:       req.Namespace,
		ResourceName:    req.ResourceName,
		IncludeLogs:     req.IncludeLogs,
		IncludeMetrics:  req.IncludeMetrics,
		IncludeDescribe: req.IncludeDescribe,
		LogTailLines:    500,
	}

	diagCtx, err := a.contextBuilder.BuildContext(ctx, contextReq)
	if err != nil {
		return nil, fmt.Errorf("failed to build diagnostic context: %w", err)
	}

	// 2. Sanitizar contexto
	sanitizedCtx, err := a.sanitizeContext(diagCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to sanitize context: %w", err)
	}

	// 3. Construir prompt
	prompt, err := a.promptBuilder.BuildPrompt(sanitizedCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to build prompt: %w", err)
	}

	// 4. Enviar para AI
	analysis, err := a.provider.Analyze(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// 5. Processar resultado
	result := &AnalysisResult{
		ID:           uuid.New().String(),
		ResourceType: req.ResourceType,
		Cluster:      req.Cluster,
		Namespace:    req.Namespace,
		ResourceName: req.ResourceName,
		Provider:     a.provider.GetName(),
		Model:        a.provider.GetModel(),
		Analysis:     analysis,
		Suggestions:  a.extractSuggestions(analysis),
		ResponseTime: time.Since(startTime).Seconds(),
		AnalyzedAt:   time.Now(),
	}

	// 6. Salvar no histórico
	if a.historyStore != nil {
		suggestionsJSON, _ := json.Marshal(result.Suggestions)
		historyRecord := &storage.HistoryRecord{
			ID:           result.ID,
			ResourceType: result.ResourceType,
			Cluster:      result.Cluster,
			Namespace:    result.Namespace,
			ResourceName: result.ResourceName,
			Provider:     result.Provider,
			Model:        result.Model,
			Analysis:     result.Analysis,
			Suggestions:  string(suggestionsJSON),
			TokensUsed:   result.TokensUsed,
			ResponseTime: result.ResponseTime,
			AnalyzedAt:   result.AnalyzedAt,
			UserEmail:    req.UserEmail,
		}

		if err := a.historyStore.Save(historyRecord); err != nil {
			// Log error mas não falha a análise
			fmt.Printf("Warning: failed to save analysis to history: %v\n", err)
		}
	}

	return result, nil
}

// sanitizeContext sanitiza dados sensíveis do contexto
func (a *Analyzer) sanitizeContext(diagCtx *collectors.DiagnosticContext) (*collectors.DiagnosticContext, error) {
	// Sanitizar logs de pods
	if diagCtx.Pod != nil {
		for containerName, logs := range diagCtx.Pod.Logs {
			diagCtx.Pod.Logs[containerName] = a.sanitizer.SanitizeLogs(logs)
		}
		for containerName, logs := range diagCtx.Pod.PreviousLogs {
			diagCtx.Pod.PreviousLogs[containerName] = a.sanitizer.SanitizeLogs(logs)
		}

		// Sanitizar manifesto do pod
		if diagCtx.Pod.Manifest != nil {
			sanitizedPod, err := a.sanitizer.SanitizeKubernetesPod(diagCtx.Pod.Manifest)
			if err == nil {
				diagCtx.Pod.Manifest = sanitizedPod
			}
		}
	}

	// Sanitizar eventos
	for i := range diagCtx.Events {
		sanitizedEvent, err := a.sanitizer.SanitizeKubernetesEvent(&diagCtx.Events[i])
		if err == nil {
			diagCtx.Events[i] = *sanitizedEvent
		}
	}

	// Sanitizar kubectl describe output
	if diagCtx.DescribeOutput != "" {
		diagCtx.DescribeOutput = a.sanitizer.SanitizeText(diagCtx.DescribeOutput)
	}

	// Investigation já vem sanitizada (ConfigMaps truncados em 100 chars, Secrets sem valores)
	// Não precisa sanitizar novamente

	return diagCtx, nil
}

// extractSuggestions extrai sugestões do texto da análise
func (a *Analyzer) extractSuggestions(analysis string) []Suggestion {
	suggestions := []Suggestion{}

	// Regex para comandos kubectl (linhas começando com $)
	commandPattern := regexp.MustCompile(`(?m)^\$\s*(.+)$`)
	commands := commandPattern.FindAllStringSubmatch(analysis, -1)

	// Inferir tipo e prioridade baseado em palavras-chave
	for _, match := range commands {
		if len(match) < 2 {
			continue
		}

		command := strings.TrimSpace(match[1])
		suggestion := Suggestion{
			Command:  command,
			Priority: a.inferPriority(command, analysis),
			Type:     a.inferType(command),
		}

		// Tentar extrair descrição do contexto ao redor do comando
		suggestion.Description = a.extractDescription(command, analysis)

		suggestions = append(suggestions, suggestion)
	}

	// Se não houver comandos, criar sugestão genérica baseada em palavras-chave
	if len(suggestions) == 0 {
		genericSuggestion := a.createGenericSuggestion(analysis)
		if genericSuggestion != nil {
			suggestions = append(suggestions, *genericSuggestion)
		}
	}

	return suggestions
}

// inferPriority infere prioridade baseado em palavras-chave
func (a *Analyzer) inferPriority(command, analysis string) string {
	lowerCommand := strings.ToLower(command)
	lowerAnalysis := strings.ToLower(analysis)

	// Critical keywords
	if strings.Contains(lowerAnalysis, "critical") || strings.Contains(lowerAnalysis, "down") ||
		strings.Contains(lowerAnalysis, "failed") || strings.Contains(lowerCommand, "delete") {
		return "critical"
	}

	// High priority keywords
	if strings.Contains(lowerAnalysis, "high") || strings.Contains(lowerCommand, "restart") ||
		strings.Contains(lowerCommand, "rollback") || strings.Contains(lowerCommand, "scale") {
		return "high"
	}

	// Medium priority keywords
	if strings.Contains(lowerAnalysis, "medium") || strings.Contains(lowerCommand, "update") ||
		strings.Contains(lowerCommand, "patch") {
		return "medium"
	}

	return "low"
}

// inferType infere tipo de sugestão baseado no comando
func (a *Analyzer) inferType(command string) string {
	lowerCommand := strings.ToLower(command)

	if strings.Contains(lowerCommand, "logs") || strings.Contains(lowerCommand, "describe") ||
		strings.Contains(lowerCommand, "get") || strings.Contains(lowerCommand, "events") {
		return "investigate"
	}

	if strings.Contains(lowerCommand, "delete") {
		return "delete"
	}

	if strings.Contains(lowerCommand, "scale") {
		return "scale"
	}

	if strings.Contains(lowerCommand, "rollback") || strings.Contains(lowerCommand, "undo") {
		return "rollback"
	}

	if strings.Contains(lowerCommand, "patch") || strings.Contains(lowerCommand, "edit") ||
		strings.Contains(lowerCommand, "apply") {
		return "update"
	}

	return "fix"
}

// extractDescription extrai descrição do contexto ao redor do comando
func (a *Analyzer) extractDescription(command, analysis string) string {
	// Procura por linhas antes do comando que podem conter descrição
	lines := strings.Split(analysis, "\n")
	for i, line := range lines {
		if strings.Contains(line, command) && i > 0 {
			// Retorna linha anterior como descrição
			prevLine := strings.TrimSpace(lines[i-1])
			// Remove markdown headers e bullets
			prevLine = strings.TrimPrefix(prevLine, "#")
			prevLine = strings.TrimPrefix(prevLine, "*")
			prevLine = strings.TrimPrefix(prevLine, "-")
			prevLine = strings.TrimSpace(prevLine)

			if prevLine != "" && len(prevLine) < 200 {
				return prevLine
			}
		}
	}

	return "Execute this command to investigate or fix the issue"
}

// createGenericSuggestion cria sugestão genérica se não houver comandos
func (a *Analyzer) createGenericSuggestion(analysis string) *Suggestion {
	lowerAnalysis := strings.ToLower(analysis)

	// Detectar prioridade baseado em palavras-chave
	priority := "medium"
	if strings.Contains(lowerAnalysis, "critical") || strings.Contains(lowerAnalysis, "failed") ||
		strings.Contains(lowerAnalysis, "crítico") || strings.Contains(lowerAnalysis, "falhou") {
		priority = "critical"
	} else if strings.Contains(lowerAnalysis, "high") || strings.Contains(lowerAnalysis, "warning") ||
		strings.Contains(lowerAnalysis, "alto") || strings.Contains(lowerAnalysis, "aviso") {
		priority = "high"
	}

	return &Suggestion{
		Type:        "investigate",
		Description: "Revise a análise e tome as ações apropriadas",
		Priority:    priority,
	}
}

// GetProviderStatus retorna status do provider AI
func (a *Analyzer) GetProviderStatus(ctx context.Context) *ProviderStatus {
	status := &ProviderStatus{
		Provider: a.provider.GetName(),
		Model:    a.provider.GetModel(),
	}

	// Verificar disponibilidade com timeout curto
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	status.Available = a.provider.IsAvailable(checkCtx)

	if !status.Available {
		status.Error = "Provider is not available or unreachable"
	}

	return status
}
