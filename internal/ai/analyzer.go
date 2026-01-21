package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s-hpa-manager/internal/aierrors"
	"k8s-hpa-manager/internal/collectors"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/sanitizer"
	"k8s-hpa-manager/internal/storage"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
)

// Analyzer orquestra análises AI de recursos Kubernetes
type Analyzer struct {
	provider       Provider
	contextBuilder *collectors.ContextBuilder
	promptBuilder  *PromptBuilder
	sanitizer      *sanitizer.Sanitizer
	historyStore   *storage.AIHistoryStore
	// Rastreamento de erros recentes
	lastError     string
	lastErrorTime time.Time
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
		// Registrar erro para exibir no painel de status
		a.recordError(err)
		// Incrementar contador de chamadas (falha)
		aierrors.IncrementAICall(req.UserEmail, false)
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// 5. Processar resultado (limpar erro anterior se sucesso)
	a.lastError = ""
	a.lastErrorTime = time.Time{}
	// Incrementar contador de chamadas (sucesso)
	aierrors.IncrementAICall(req.UserEmail, true)

	// FASE 3: Tentar fazer parse do JSON estruturado
	result := &AnalysisResult{
		ID:           uuid.New().String(),
		ResourceType: req.ResourceType,
		Cluster:      req.Cluster,
		Namespace:    req.Namespace,
		ResourceName: req.ResourceName,
		Provider:     a.provider.GetName(),
		Model:        a.provider.GetModel(),
		ResponseTime: time.Since(startTime).Seconds(),
		AnalyzedAt:   time.Now(),
	}

	// Tentar parse JSON estruturado (NOVO - FASE 3)
	if err := a.parseStructuredResponse(analysis, result); err != nil {
		// Fallback: usar formato legado
		fmt.Printf("[AI] Failed to parse structured JSON, using legacy format: %v\n", err)
		result.Analysis = analysis
		result.Suggestions = a.extractSuggestions(analysis)
	} else {
		// Sucesso: JSON estruturado parseado
		fmt.Printf("[AI] Successfully parsed structured JSON response\n")
		// Manter também no campo Analysis para backward compatibility
		result.Analysis = analysis
	}

	// FASE 3.5: Extrair metadados do recurso para cabeçalho profissional
	result.ResourceMetadata = a.buildResourceMetadata(diagCtx, req)

	// 6. Salvar no histórico
	if a.historyStore != nil {
		suggestionsJSON, _ := json.Marshal(result.Suggestions)

		// FASE 2.1: Serializar métricas Prometheus para persistência
		var prometheusMetricsNullable sql.NullString
		if diagCtx.PrometheusMetrics != nil {
			metricsData, err := json.Marshal(diagCtx.PrometheusMetrics)
			if err == nil {
				prometheusMetricsNullable = sql.NullString{String: string(metricsData), Valid: true}
			} else {
				fmt.Printf("Warning: failed to serialize Prometheus metrics: %v\n", err)
			}
		}

		// FASE 3.5: Serializar metadados do recurso para persistência
		var resourceMetadataNullable sql.NullString
		if result.ResourceMetadata != nil {
			metadataData, err := json.Marshal(result.ResourceMetadata)
			if err == nil {
				resourceMetadataNullable = sql.NullString{String: string(metadataData), Valid: true}
			} else {
				fmt.Printf("Warning: failed to serialize Resource metadata: %v\n", err)
			}
		}

		historyRecord := &storage.HistoryRecord{
			ID:                result.ID,
			ResourceType:      result.ResourceType,
			Cluster:           result.Cluster,
			Namespace:         result.Namespace,
			ResourceName:      result.ResourceName,
			Provider:          result.Provider,
			Model:             result.Model,
			Analysis:          result.Analysis,
			Suggestions:       string(suggestionsJSON),
			PrometheusMetrics: prometheusMetricsNullable, // FASE 2.1
			ResourceMetadata:  resourceMetadataNullable,  // FASE 3.5 - 08/01/2026
			TokensUsed:        result.TokensUsed,
			ResponseTime:      result.ResponseTime,
			AnalyzedAt:        result.AnalyzedAt,
			UserEmail:         req.UserEmail,
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

// GetProvider retorna o provider AI configurado no analyzer
func (a *Analyzer) GetProvider() Provider {
	return a.provider
}

// GetProviderStatus retorna status do provider AI
func (a *Analyzer) GetProviderStatus(ctx context.Context) *ProviderStatus {
	status := &ProviderStatus{
		Provider: a.provider.GetName(),
		Model:    a.provider.GetModel(),
	}

	// IMPORTANTE: Usar apenas IsAvailable() do provider
	// NÃO fazer chamada real à IA para economizar tokens
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Verificar apenas conectividade (sem consumir tokens da API)
	if a.provider.IsAvailable(checkCtx) {
		status.Available = true
		// Se tiver erro recente (últimos 5 minutos), mostrar como aviso
		if a.lastError != "" && time.Since(a.lastErrorTime) < 5*time.Minute {
			status.Error = fmt.Sprintf("⚠️ Último erro (%s): %s", 
				formatDuration(time.Since(a.lastErrorTime)), a.lastError)
		}
	} else {
		status.Available = false
		// Se tiver erro recente, usar ele
		if a.lastError != "" && time.Since(a.lastErrorTime) < 10*time.Minute {
			status.Error = fmt.Sprintf("❌ %s (há %s)", 
				a.lastError, formatDuration(time.Since(a.lastErrorTime)))
		} else {
			status.Error = "Provider indisponível. Verifique configuração ou conectividade."
		}
	}

	return status
}

// formatDuration formata duração de forma legível
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dmin", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// recordError registra um erro recente da AI para exibir no painel de status
func (a *Analyzer) recordError(err error) {
	a.lastError = a.parseProviderError(err)
	a.lastErrorTime = time.Now()
}

// parseProviderError analisa erro do provider e retorna mensagem amigável
func (a *Analyzer) parseProviderError(err error) string {
	errMsg := err.Error()
	lowerMsg := strings.ToLower(errMsg)

	// Quota exceeded (HTTP 429)
	if strings.Contains(lowerMsg, "429") ||
		strings.Contains(lowerMsg, "quota") ||
		strings.Contains(lowerMsg, "resource_exhausted") ||
		strings.Contains(lowerMsg, "rate limit") {
		return "Quota exceeded - API limit reached. Please check your plan or wait before retrying."
	}

	// Authentication failed (HTTP 401/403)
	if strings.Contains(lowerMsg, "401") ||
		strings.Contains(lowerMsg, "403") ||
		strings.Contains(lowerMsg, "unauthorized") ||
		strings.Contains(lowerMsg, "forbidden") ||
		strings.Contains(lowerMsg, "invalid api key") ||
		strings.Contains(lowerMsg, "authentication") {
		return "Authentication failed - Invalid or expired API key. Please check your credentials."
	}

	// Model not found (HTTP 404)
	if strings.Contains(lowerMsg, "404") ||
		strings.Contains(lowerMsg, "not found") ||
		strings.Contains(lowerMsg, "model") && strings.Contains(lowerMsg, "does not exist") {
		return fmt.Sprintf("Model not found - The model '%s' is not available. Please select a valid model.", a.provider.GetModel())
	}

	// Connection timeout
	if strings.Contains(lowerMsg, "timeout") ||
		strings.Contains(lowerMsg, "context deadline exceeded") {
		return "Connection timeout - Unable to reach the AI provider. Please check your network connection."
	}

	// Connection refused
	if strings.Contains(lowerMsg, "connection refused") ||
		strings.Contains(lowerMsg, "no such host") ||
		strings.Contains(lowerMsg, "dial tcp") {
		return "Connection failed - Unable to connect to the AI provider. Check if the service is running."
	}

	// Bad request (HTTP 400)
	if strings.Contains(lowerMsg, "400") ||
		strings.Contains(lowerMsg, "bad request") ||
		strings.Contains(lowerMsg, "invalid_request") {
		return "Bad request - The request format is invalid. Please check your configuration."
	}

	// Service unavailable (HTTP 503)
	if strings.Contains(lowerMsg, "503") ||
		strings.Contains(lowerMsg, "service unavailable") {
		return "Service unavailable - The AI provider is temporarily unavailable. Please try again later."
	}

	// Generic error (retorna primeiras 200 chars da mensagem)
	if len(errMsg) > 200 {
		return fmt.Sprintf("Error: %s...", errMsg[:200])
	}
	return fmt.Sprintf("Error: %s", errMsg)
}

// parseStructuredResponse faz parse do JSON estruturado retornado pela IA (FASE 3)
func (a *Analyzer) parseStructuredResponse(response string, result *AnalysisResult) error {
	// Estrutura temporária para parse do JSON
	var structuredResponse struct {
		ExecutiveSummary  ExecutiveSummary           `json:"executive_summary"`
		RootCauseAnalysis RootCauseAnalysis          `json:"root_cause_analysis"`
		ImpactAssessment  ImpactAssessment           `json:"impact_assessment"`
		Recommendations   []ActionableRecommendation `json:"recommendations"`
	}

	// Limpar markdown wrappers (```json ... ```)
	cleanedResponse := strings.TrimSpace(response)
	cleanedResponse = strings.TrimPrefix(cleanedResponse, "```json")
	cleanedResponse = strings.TrimPrefix(cleanedResponse, "```")
	cleanedResponse = strings.TrimSuffix(cleanedResponse, "```")
	cleanedResponse = strings.TrimSpace(cleanedResponse)

	// Tentar fazer unmarshal do JSON
	if err := json.Unmarshal([]byte(cleanedResponse), &structuredResponse); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Preencher resultado com dados estruturados
	result.ExecutiveSummary = structuredResponse.ExecutiveSummary
	result.RootCauseAnalysis = structuredResponse.RootCauseAnalysis
	result.ImpactAssessment = structuredResponse.ImpactAssessment
	result.Recommendations = structuredResponse.Recommendations

	return nil
}

// buildResourceMetadata extrai metadados do recurso para cabeçalho profissional (FASE 3.5)
func (a *Analyzer) buildResourceMetadata(diagCtx *collectors.DiagnosticContext, req *AnalysisRequest) *ResourceMetadata {
	fmt.Printf("[DEBUG] buildResourceMetadata called for %s/%s\n", diagCtx.Namespace, diagCtx.ResourceName)
	fmt.Printf("[DEBUG] IncludeMetrics in request: %v\n", req.IncludeMetrics)

	metadata := &ResourceMetadata{
		Name:        diagCtx.ResourceName,
		Namespace:   diagCtx.Namespace,
		Cluster:     diagCtx.Cluster,
		Type:        diagCtx.ResourceType,
		CollectedAt: diagCtx.CollectedAt,
	}

	// Extrair dados específicos por tipo de recurso
	switch diagCtx.ResourceType {
	case "Pod":
		if diagCtx.Pod != nil && diagCtx.Pod.Manifest != nil {
			a.extractPodMetadata(diagCtx.Pod, metadata)
		}
	case "Deployment":
		if diagCtx.Deployment != nil {
			a.extractDeploymentMetadata(diagCtx.Deployment, metadata)
		}
	case "HPA":
		if diagCtx.HPA != nil {
			a.extractHPAMetadata(diagCtx.HPA, metadata)
		}
	case "Node":
		if diagCtx.Node != nil {
			a.extractNodeMetadata(diagCtx.Node, metadata)
		}
	}

	// Adicionar métricas Prometheus/MetricsServer se disponíveis
	fmt.Printf("[DEBUG] diagCtx.PrometheusMetrics is nil: %v\n", diagCtx.PrometheusMetrics == nil)
	if diagCtx.PrometheusMetrics != nil {
		fmt.Printf("[DEBUG] PrometheusMetrics: CPU=%.2fm, Memory=%.2fMi\n",
			diagCtx.PrometheusMetrics.CPUUsageCurrent,
			diagCtx.PrometheusMetrics.MemoryUsageCurrent)
		a.enrichMetadataWithPrometheus(diagCtx.PrometheusMetrics, metadata)
		fmt.Printf("[DEBUG] After enrichment: CPU=%s, Memory=%s\n",
			metadata.Resources.CPU.Current,
			metadata.Resources.Memory.Current)
	} else {
		fmt.Println("[DEBUG] PrometheusMetrics is nil - metrics will show N/A")
	}

	return metadata
}

// extractPodMetadata extrai metadados de um Pod
func (a *Analyzer) extractPodMetadata(podCtx *collectors.PodContext, metadata *ResourceMetadata) {
	pod := podCtx.Manifest

	// Status e Phase
	metadata.Phase = string(pod.Status.Phase)
	metadata.Status = a.getPodStatus(pod)

	// Node
	metadata.NodeName = pod.Spec.NodeName

	// Age (tempo desde criação)
	metadata.Age = formatDuration(time.Since(pod.CreationTimestamp.Time))

	// Versão da aplicação (labels comuns)
	if pod.Labels != nil {
		// Tentar extrair versão de labels padrão
		if version, ok := pod.Labels["app.kubernetes.io/version"]; ok {
			metadata.Version = version
		} else if version, ok := pod.Labels["version"]; ok {
			metadata.Version = version
		}

		// Guardar labels importantes
		metadata.Labels = make(map[string]string)
		importantLabels := []string{"app", "app.kubernetes.io/name", "app.kubernetes.io/component", "tier", "environment"}
		for _, key := range importantLabels {
			if value, ok := pod.Labels[key]; ok {
				metadata.Labels[key] = value
			}
		}
	}

	// Restart count total
	totalRestarts := int32(0)
	for _, cs := range pod.Status.ContainerStatuses {
		totalRestarts += cs.RestartCount
	}
	metadata.RestartCount = totalRestarts

	// Imagens dos containers
	metadata.ContainerImages = make([]string, 0)
	for _, container := range pod.Spec.Containers {
		metadata.ContainerImages = append(metadata.ContainerImages, container.Image)
	}

	// Recursos (CPU/Memory requests e limits)
	metadata.Resources = a.extractPodResources(pod)
}

// extractPodResources extrai recursos de CPU/Memory do Pod
func (a *Analyzer) extractPodResources(pod *corev1.Pod) ResourceSummary {
	summary := ResourceSummary{
		CPU:    ResourceDetail{Current: "N/A", Request: "N/A", Limit: "N/A"},
		Memory: ResourceDetail{Current: "N/A", Request: "N/A", Limit: "N/A"},
	}

	// Somar requests e limits de todos os containers
	var totalCPURequest, totalCPULimit int64
	var totalMemoryRequest, totalMemoryLimit int64

	for _, container := range pod.Spec.Containers {
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				totalCPURequest += cpu.MilliValue()
			}
			if memory := container.Resources.Requests.Memory(); memory != nil {
				totalMemoryRequest += memory.Value()
			}
		}
		if container.Resources.Limits != nil {
			if cpu := container.Resources.Limits.Cpu(); cpu != nil {
				totalCPULimit += cpu.MilliValue()
			}
			if memory := container.Resources.Limits.Memory(); memory != nil {
				totalMemoryLimit += memory.Value()
			}
		}
	}

	// Formatar valores legíveis
	if totalCPURequest > 0 {
		summary.CPU.Request = fmt.Sprintf("%dm", totalCPURequest)
	}
	if totalCPULimit > 0 {
		summary.CPU.Limit = fmt.Sprintf("%dm", totalCPULimit)
	}
	if totalMemoryRequest > 0 {
		summary.Memory.Request = formatMemory(totalMemoryRequest)
	}
	if totalMemoryLimit > 0 {
		summary.Memory.Limit = formatMemory(totalMemoryLimit)
	}

	return summary
}

// enrichMetadataWithPrometheus enriquece metadados com métricas do Prometheus
func (a *Analyzer) enrichMetadataWithPrometheus(metrics *collectors.PrometheusMetrics, metadata *ResourceMetadata) {
	// CPU atual
	if metrics.CPUUsageCurrent > 0 {
		metadata.Resources.CPU.Current = fmt.Sprintf("%.0fm", metrics.CPUUsageCurrent)
	}

	// Memory atual
	if metrics.MemoryUsageCurrent > 0 {
		metadata.Resources.Memory.Current = fmt.Sprintf("%.0fMi", metrics.MemoryUsageCurrent)
	}

	// Réplicas (se disponível)
	if metrics.DesiredReplicas > 0 || metrics.ReadyReplicas > 0 {
		metadata.Replicas = &ReplicaInfo{
			Current: metrics.ReadyReplicas,
			Ready:   metrics.ReadyReplicas,
			Desired: metrics.DesiredReplicas,
		}
	}

	// Restart count do Prometheus (sobrescreve se maior)
	if metrics.RestartCount > metadata.RestartCount {
		metadata.RestartCount = metrics.RestartCount
	}
}

// getPodStatus determina status do Pod baseado em suas condições
func (a *Analyzer) getPodStatus(pod *corev1.Pod) string {
	// Verificar se está em CrashLoopBackOff ou outros estados de erro
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" ||
				reason == "ErrImagePull" || reason == "CreateContainerConfigError" {
				return reason
			}
		}
		if cs.State.Terminated != nil {
			return "Terminated: " + cs.State.Terminated.Reason
		}
	}

	// Se todos containers estão prontos, é Running
	allReady := true
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			allReady = false
			break
		}
	}
	if allReady && pod.Status.Phase == corev1.PodRunning {
		return "Running"
	}

	// Retornar phase padrão
	return string(pod.Status.Phase)
}

// formatMemory formata bytes para formato legível (Mi, Gi)
func formatMemory(bytes int64) string {
	const (
		Mi = 1024 * 1024
		Gi = 1024 * 1024 * 1024
	)

	if bytes >= Gi {
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(Gi))
	}
	return fmt.Sprintf("%.0fMi", float64(bytes)/float64(Mi))
}

// extractDeploymentMetadata extrai metadados de um Deployment (placeholder)
func (a *Analyzer) extractDeploymentMetadata(deployCtx *collectors.DeploymentContext, metadata *ResourceMetadata) {
	// TODO: Implementar extração de metadados de Deployment
	// Similar ao Pod, mas focando em réplicas, rollout status, etc
	metadata.Status = "Deployment metadata extraction not implemented"
}

// extractHPAMetadata extrai metadados de um HPA (placeholder)
func (a *Analyzer) extractHPAMetadata(hpaCtx *collectors.HPAContext, metadata *ResourceMetadata) {
	// TODO: Implementar extração de metadados de HPA
	// Focar em min/max replicas, current replicas, target metrics
	metadata.Status = "HPA metadata extraction not implemented"
}

// extractNodeMetadata extrai metadados de um Node (placeholder)
func (a *Analyzer) extractNodeMetadata(nodeCtx *collectors.NodeContext, metadata *ResourceMetadata) {
	// TODO: Implementar extração de metadados de Node
	// Focar em condições, capacity, allocatable, taints
	metadata.Status = "Node metadata extraction not implemented"
}
