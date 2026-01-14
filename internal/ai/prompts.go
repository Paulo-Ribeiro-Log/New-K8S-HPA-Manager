package ai

import (
	"fmt"
	"strings"

	"k8s-hpa-manager/internal/collectors"
)

// PromptBuilder constrói prompts para AI
type PromptBuilder struct {
	templates map[string]string
}

// NewPromptBuilder cria um novo PromptBuilder
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{
		templates: defaultTemplates(),
	}
}

// BuildPrompt constrói prompt completo a partir do contexto
func (pb *PromptBuilder) BuildPrompt(diagCtx *collectors.DiagnosticContext) (string, error) {
	// Obter template baseado no tipo de recurso
	template, ok := pb.templates[diagCtx.ResourceType]
	if !ok {
		return "", fmt.Errorf("no template found for resource type: %s", diagCtx.ResourceType)
	}

	// Construir resumo simplificado do contexto para facilitar a análise
	simplifiedContext := pb.buildSimplifiedContext(diagCtx)

	// Substituir placeholder {{CONTEXT}} pelo contexto simplificado
	prompt := strings.Replace(template, "{{CONTEXT}}", simplifiedContext, 1)

	return prompt, nil
}

// defaultTemplates retorna templates padrão para cada tipo de recurso
func defaultTemplates() map[string]string {
	return map[string]string{
		"Pod":        podTemplate,
		"Deployment": deploymentTemplate,
		"HPA":        hpaTemplate,
		"Node":       nodeTemplate,
	}
}

// buildSimplifiedContext constrói um contexto simplificado focando em problemas
func (pb *PromptBuilder) buildSimplifiedContext(diagCtx *collectors.DiagnosticContext) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("## RECURSO\n"))
	builder.WriteString(fmt.Sprintf("Tipo: %s\n", diagCtx.ResourceType))
	builder.WriteString(fmt.Sprintf("Nome: %s\n", diagCtx.ResourceName))
	builder.WriteString(fmt.Sprintf("Namespace: %s\n", diagCtx.Namespace))
	builder.WriteString(fmt.Sprintf("Cluster: %s\n\n", diagCtx.Cluster))

	// 🔍 INVESTIGAÇÃO AUTOMÁTICA - MOVER PARA O TOPO (antes dos eventos)
	// Tem prioridade sobre mensagens de erro em eventos
	if diagCtx.Investigation != nil {
		pb.addInvestigationResults(&builder, diagCtx.Investigation)
	}

	// Eventos (focar em erros)
	if len(diagCtx.Events) > 0 {
		builder.WriteString("## EVENTOS RECENTES\n")
		builder.WriteString("⚠️ ATENÇÃO: Se VALIDAÇÃO AUTOMÁTICA encontrou problemas, NÃO PEÇA kubectl get/describe/logs - DADOS JÁ COLETADOS\n")
		builder.WriteString("⚠️ Se investigação confirmou recurso EXISTE, IGNORE 'not found' nos eventos abaixo\n\n")
		errorCount := 0
		for _, event := range diagCtx.Events {
			// Priorizar eventos de erro/warning
			if event.Type == "Warning" || event.Type == "Error" ||
				strings.Contains(strings.ToLower(event.Reason), "failed") ||
				strings.Contains(strings.ToLower(event.Reason), "error") ||
				strings.Contains(strings.ToLower(event.Reason), "backoff") {
				builder.WriteString(fmt.Sprintf("- [%s] %s: %s\n", event.Type, event.Reason, event.Message))
				errorCount++
				if errorCount >= 10 { // Limitar a 10 eventos
					break
				}
			}
		}
		// Se não há erros, mostrar alguns eventos normais
		if errorCount == 0 {
			for i, event := range diagCtx.Events {
				if i >= 5 {
					break
				}
				builder.WriteString(fmt.Sprintf("- [%s] %s: %s\n", event.Type, event.Reason, event.Message))
			}
		}
		builder.WriteString("\n")
	}

	// Contexto específico por tipo de recurso
	if diagCtx.Pod != nil {
		pb.addPodContext(&builder, diagCtx.Pod)
	}

	// kubectl describe output (se disponível)
	if diagCtx.DescribeOutput != "" {
		builder.WriteString("## KUBECTL DESCRIBE\n")
		// Limitar a 3000 caracteres para não sobrecarregar
		if len(diagCtx.DescribeOutput) > 3000 {
			builder.WriteString(diagCtx.DescribeOutput[:3000] + "\n...(truncated)\n\n")
		} else {
			builder.WriteString(diagCtx.DescribeOutput + "\n\n")
		}
	}

	return builder.String()
}

// addPodContext adiciona contexto específico de Pod
func (pb *PromptBuilder) addPodContext(builder *strings.Builder, podCtx *collectors.PodContext) {
	if podCtx.Manifest != nil {
		pod := podCtx.Manifest

		// ALERTAS CRÍTICOS NO TOPO
		hasHighRestarts := false
		maxRestarts := int32(0)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.RestartCount > maxRestarts {
				maxRestarts = cs.RestartCount
			}
			if cs.RestartCount > 5 {
				hasHighRestarts = true
			}
		}

		if hasHighRestarts {
			builder.WriteString("════════════════════════════════════════════════════\n")
			builder.WriteString("🚨🚨🚨 ALERTA CRÍTICO - RESTARTS EXCESSIVOS 🚨🚨🚨\n")
			builder.WriteString("════════════════════════════════════════════════════\n")
			builder.WriteString(fmt.Sprintf("⚠️ Container(s) com %d RESTARTS - CRASHANDO REPETIDAMENTE\n", maxRestarts))
			builder.WriteString("⚠️ CAUSA: Verifique LOGS abaixo (seção LOGS DO POD)\n")
			builder.WriteString("⚠️ NÃO é problema de recursos ausentes (ConfigMap/Secret)\n")
			builder.WriteString("⚠️ Provável: erro na aplicação, configuração incorreta, dependência falhando\n")
			builder.WriteString("════════════════════════════════════════════════════\n\n")
		}

		builder.WriteString("## STATUS DO POD\n")
		builder.WriteString(fmt.Sprintf("Fase: %s\n", pod.Status.Phase))
		builder.WriteString(fmt.Sprintf("Node: %s\n", pod.Spec.NodeName))

		// Status dos containers
		builder.WriteString("\nContainers:\n")
		for _, cs := range pod.Status.ContainerStatuses {
			builder.WriteString(fmt.Sprintf("  - %s:\n", cs.Name))
			builder.WriteString(fmt.Sprintf("      Ready: %v\n", cs.Ready))
			builder.WriteString(fmt.Sprintf("      Restarts: %d", cs.RestartCount))
			if cs.RestartCount > 5 {
				builder.WriteString(" ⚠️ ALTO!\n")
			} else {
				builder.WriteString("\n")
			}

			if cs.State.Waiting != nil {
				builder.WriteString(fmt.Sprintf("      Estado: Waiting (%s)\n", cs.State.Waiting.Reason))
				if cs.State.Waiting.Message != "" {
					builder.WriteString(fmt.Sprintf("      Mensagem: %s\n", cs.State.Waiting.Message))
				}
			} else if cs.State.Terminated != nil {
				builder.WriteString(fmt.Sprintf("      Estado: Terminated (%s)\n", cs.State.Terminated.Reason))
				if cs.State.Terminated.Message != "" {
					builder.WriteString(fmt.Sprintf("      Mensagem: %s\n", cs.State.Terminated.Message))
				}
			} else if cs.State.Running != nil {
				builder.WriteString("      Estado: Running\n")
			}

			if cs.LastTerminationState.Terminated != nil {
				builder.WriteString(fmt.Sprintf("      Última Terminação: %s - %s\n",
					cs.LastTerminationState.Terminated.Reason,
					cs.LastTerminationState.Terminated.Message))
			}
		}
		builder.WriteString("\n")

		// ConfigMaps e Secrets referenciados
		if len(podCtx.RelatedConfigMaps) > 0 {
			builder.WriteString(fmt.Sprintf("ConfigMaps referenciados: %s\n", strings.Join(podCtx.RelatedConfigMaps, ", ")))
		}
		if len(podCtx.RelatedSecrets) > 0 {
			builder.WriteString(fmt.Sprintf("Secrets referenciados: %s\n", strings.Join(podCtx.RelatedSecrets, ", ")))
		}
		builder.WriteString("\n")
	}

	// Logs (últimas linhas com erros)
	if len(podCtx.Logs) > 0 {
		builder.WriteString("════════════════════════════════════════════════════\n")
		builder.WriteString("📋 LOGS DO POD - ANALISE AQUI PARA ENCONTRAR ERRO 📋\n")
		builder.WriteString("════════════════════════════════════════════════════\n")
		builder.WriteString("⚠️ NÃO peça 'kubectl logs' - OS LOGS ESTÃO ABAIXO\n")
		builder.WriteString("════════════════════════════════════════════════════\n\n")

		for containerName, logs := range podCtx.Logs {
			lines := strings.Split(logs, "\n")
			// Pegar últimas 20 linhas ou linhas com "error", "failed", "exception"
			errorLines := []string{}
			normalLines := []string{}

			for _, line := range lines {
				lowerLine := strings.ToLower(line)
				if strings.Contains(lowerLine, "error") ||
					strings.Contains(lowerLine, "failed") ||
					strings.Contains(lowerLine, "exception") ||
					strings.Contains(lowerLine, "fatal") {
					errorLines = append(errorLines, line)
				} else if len(line) > 0 {
					normalLines = append(normalLines, line)
				}
			}

			builder.WriteString(fmt.Sprintf("\nContainer: %s\n", containerName))
			if len(errorLines) > 0 {
				builder.WriteString("Erros encontrados:\n")
				for i, line := range errorLines {
					if i >= 10 {
						break
					}
					builder.WriteString(fmt.Sprintf("  %s\n", line))
				}
			} else {
				// Se não há erros, mostrar últimas 10 linhas
				start := len(normalLines) - 10
				if start < 0 {
					start = 0
				}
				for _, line := range normalLines[start:] {
					builder.WriteString(fmt.Sprintf("  %s\n", line))
				}
			}
		}
		builder.WriteString("\n")
	}

	// LOGS ANTERIORES (ANTES DO ÚLTIMO RESTART) - CRÍTICO PARA CRASHLOOP!
	if len(podCtx.PreviousLogs) > 0 {
		builder.WriteString("════════════════════════════════════════════════════\n")
		builder.WriteString("🔥 LOGS ANTERIORES (ANTES DO CRASH) - ANALISE AQUI! 🔥\n")
		builder.WriteString("════════════════════════════════════════════════════\n")
		builder.WriteString("⚠️ Container crashou! Estes são os logs DE QUANDO CRASHOU\n")
		builder.WriteString("⚠️ FOQUE NAS ÚLTIMAS LINHAS - O ERRO ESTÁ AQUI!\n")
		builder.WriteString("════════════════════════════════════════════════════\n\n")

		for containerName, logs := range podCtx.PreviousLogs {
			lines := strings.Split(logs, "\n")
			// Pegar últimas 30 linhas - onde geralmente está o erro
			start := len(lines) - 30
			if start < 0 {
				start = 0
			}

			builder.WriteString(fmt.Sprintf("\n🔥 Container '%s' - ÚLTIMAS 30 LINHAS ANTES DO CRASH:\n", containerName))
			builder.WriteString("────────────────────────────────────────────────────\n")
			for _, line := range lines[start:] {
				if len(strings.TrimSpace(line)) > 0 {
					builder.WriteString(fmt.Sprintf("%s\n", line))
				}
			}
			builder.WriteString("────────────────────────────────────────────────────\n")
		}
		builder.WriteString("\n")
	}
}

// addInvestigationResults adiciona resultados da investigação automática
func (pb *PromptBuilder) addInvestigationResults(builder *strings.Builder, investigation *collectors.InvestigationResult) {
	builder.WriteString("## 🔍 INVESTIGAÇÃO E VALIDAÇÃO AUTOMÁTICA (PRIORIDADE MÁXIMA)\n")
	builder.WriteString("⚠️ ESTAS INFORMAÇÕES TÊM PRIORIDADE ABSOLUTA - SÃO RESULTADOS DE VALIDAÇÃO REAL NO CLUSTER\n\n")

	// VALIDAÇÕES AUTOMÁTICAS (NOVO - PRIORIDADE MÁXIMA)
	if len(investigation.Validations) > 0 {
		builder.WriteString("### 🎯 DIAGNÓSTICO AUTOMÁTICO JÁ EXECUTADO:\n\n")

		for _, validation := range investigation.Validations {
			if validation.Exists {
				builder.WriteString(fmt.Sprintf("#### ✅ %s '%s' EXISTE NO CLUSTER\n\n", validation.ResourceType, validation.ResourceName))
			} else {
				builder.WriteString(fmt.Sprintf("#### ❌ %s '%s' NÃO EXISTE\n\n", validation.ResourceType, validation.ResourceName))
			}

			if len(validation.Issues) > 0 {
				builder.WriteString("**🔴 CAUSA RAIZ IDENTIFICADA:**\n")
				for _, issue := range validation.Issues {
					builder.WriteString(fmt.Sprintf("- %s\n", issue))
				}
				builder.WriteString("\n")
			}

			if len(validation.Suggestions) > 0 {
				builder.WriteString("**✅ SOLUÇÃO:**\n")
				for _, suggestion := range validation.Suggestions {
					builder.WriteString(fmt.Sprintf("- %s\n", suggestion))
				}
				builder.WriteString("\n")
			}
		}

		builder.WriteString("⚠️ **ATENÇÃO AI**: DIAGNÓSTICO COMPLETO ACIMA. Apresente ao usuário de forma clara.\n")
		builder.WriteString("❌ NÃO peça kubectl logs/get/describe - TODOS OS DADOS JÁ FORAM COLETADOS\n\n")
	}

	// Recursos faltantes
	if len(investigation.MissingResources) > 0 {
		builder.WriteString("### Recursos Faltantes (confirmado via busca no cluster):\n")
		for _, missing := range investigation.MissingResources {
			builder.WriteString(fmt.Sprintf("- **%s '%s'** (namespace: %s)\n",
				missing.Type, missing.Name, missing.Namespace))
			builder.WriteString(fmt.Sprintf("  Motivo: %s\n", missing.Reason))
		}
		builder.WriteString("\n")
	}

	// Alternativas encontradas
	if len(investigation.FoundAlternatives) > 0 {
		builder.WriteString("### Recursos Encontrados:\n")
		for _, alt := range investigation.FoundAlternatives {
			if alt.Similarity == "exact_match" {
				builder.WriteString(fmt.Sprintf("- ✅ **%s '%s' EXISTE**\n", alt.Type, alt.SearchName))

				if alt.Content != nil {
					builder.WriteString(fmt.Sprintf("  📊 Keys: %v\n", alt.Content.Keys))
				}
			} else {
				builder.WriteString(fmt.Sprintf("- Buscado: **%s '%s'**, Encontrado: **'%s'** (%s)\n",
					alt.Type, alt.SearchName, alt.FoundName, alt.Similarity))
			}
		}
		builder.WriteString("\n")
	}

	// Recomendações
	if len(investigation.Recommendations) > 0 {
		builder.WriteString("### Recomendações:\n")
		for _, rec := range investigation.Recommendations {
			builder.WriteString(fmt.Sprintf("%s\n", rec))
		}
		builder.WriteString("\n")
	}
}

// Template para análise de Pods (FASE 3 - JSON Estruturado)
const podTemplate = `Você é um especialista sênior em Kubernetes e troubleshooting de aplicações.

REGRAS ABSOLUTAS:
1. RETORNE APENAS JSON VÁLIDO (sem markdown, sem '''json''')
2. COPIE trechos literais dos logs fornecidos no contexto
3. Se não há logs: escreva "LOGS NÃO DISPONÍVEIS"
4. NUNCA invente dados ou use exemplos genéricos
5. INVESTIGUE PROFUNDAMENTE - não pare no sintoma, encontre a CAUSA RAIZ
6. NÃO USE EMOJIS nos textos do JSON

**INSTRUÇÕES DE ANÁLISE PROFUNDA:**

Quando encontrar ERROS DE CONEXÃO (timeout, connection refused, unreachable):
1. NÃO sugira apenas "restart" - isso é workaround, não solução
2. INVESTIGUE o PORQUÊ da falha de conexão:
   - Connection string está correta? (verifique ConfigMap)
   - Credenciais estão válidas? (verifique Secret)
   - Serviço de destino está disponível? (kubectl get service)
   - Network policies estão bloqueando? (kubectl get networkpolicy)
   - DNS está resolvendo? (kubectl exec pod -- nslookup <service>)

Quando encontrar ERROS DE APLICAÇÃO (exceptions, stack traces):
1. Identifique o erro EXATO da stack trace (linha, método, mensagem)
2. Analise se é erro de configuração, código ou dependência
3. Verifique se ConfigMaps/Secrets necessários existem
4. Sugira comandos para verificar cada hipótese

Quando encontrar CONFIGMAP/SECRET NÃO ENCONTRADO (CreateContainerConfigError):
1. Se o nome tem padrão hash (ex: app-name-abc123), é gerado por Kustomize/Helm
2. A CAUSA RAIZ é: pipeline de deploy falhou ou ConfigMap não foi criado
3. VERIFIQUE a seção "INVESTIGAÇÃO AUTOMÁTICA" - ela lista ConfigMaps existentes com mesmo prefixo
4. SUGIRA: (a) Re-executar pipeline de deploy, (b) Verificar logs CI/CD, (c) Criar ConfigMap manualmente se urgente
5. NÃO sugira apenas "criar o ConfigMap" - investigue POR QUE não foi criado

**FORMATO DA RESPOSTA (JSON OBRIGATÓRIO):**

Retorne APENAS este JSON (sem texto adicional, sem markdown wrappers):

{
  "executive_summary": {
    "severity": "critical|high|medium|low",
    "status": "unhealthy|degraded|healthy",
    "quick_summary": "Resumo de 1-2 frases do problema principal",
    "time_to_resolve": "estimativa de tempo (ex: 15 minutes, 1 hour)"
  },
  "root_cause_analysis": {
    "symptom": "Descrição do sintoma observado (ex: CrashLoopBackOff com 15 restarts)",
    "probable_causes": [
      "Causa provável 1 (ex: Connection string incorreta no ConfigMap)",
      "Causa provável 2 (ex: MongoDB service não está disponível)",
      "Causa provável 3 (ex: Credenciais inválidas no Secret)"
    ],
    "evidence": [
      "Evidência 1: trecho literal do log mostrando erro",
      "Evidência 2: evento K8s relevante",
      "Evidência 3: estado do container"
    ],
    "confidence": "high|medium|low"
  },
  "impact_assessment": {
    "severity": "critical|high|medium|low",
    "affected_users": "Descrição de quem é afetado (ex: Todos os usuários, 50%, Nenhum)",
    "downtime_estimate": "Estimativa de downtime (ex: 30 minutes, ongoing)",
    "sla_breach": true|false,
    "business_impact": "Descrição do impacto no negócio"
  },
  "recommendations": [
    {
      "priority": 1,
      "title": "Título da ação (ex: Corrigir connection string no ConfigMap)",
      "description": "Descrição detalhada do que fazer e por quê",
      "commands": [
        "kubectl get configmap <nome> -n <namespace> -o yaml",
        "kubectl edit configmap <nome> -n <namespace>"
      ],
      "time_estimate": "estimativa (ex: 5 minutes, 30 minutes)",
      "risk_level": "low|medium|high",
      "impact_level": "high|medium|low"
    }
  ]
}

**REGRAS DE SEVERITY:**
- critical: Pod completamente inoperante, serviço down
- high: Pod em CrashLoopBackOff, serviço degradado
- medium: Warnings, restarts ocasionais, performance degradada
- low: Pod healthy mas com otimizações possíveis

**REGRAS DE PRIORITY:**
- 1: Ação crítica, resolver imediatamente
- 2: Ação importante, resolver hoje
- 3: Ação recomendada, resolver esta semana
- 4-5: Otimizações, não urgente

**CONTEXTO COM OS DADOS REAIS:**
{{CONTEXT}}

RETORNE APENAS O JSON ACIMA (SEM TEXTO ADICIONAL, SEM MARKDOWN WRAPPERS).
PORTUGUÊS BRASILEIRO NOS TEXTOS DO JSON.`

// Template para análise de Deployments
const deploymentTemplate = `Você é um especialista em Kubernetes especializado em troubleshooting de Deployments.

Analise o seguinte contexto de diagnóstico do Deployment e forneça uma análise abrangente.

**Sua resposta deve incluir:**

1. **Análise da Causa Raiz**: Identifique problemas com rollout, réplicas ou falhas de pods
2. **Avaliação de Impacto**: Descreva severidade e impacto na disponibilidade do serviço
3. **Ações Recomendadas**: Remediação passo-a-passo (rollback, escala, estratégia de atualização)
4. **Comandos kubectl**: Inclua comandos específicos (prefixe com $)

**Importante:**
- Analise status do rollout e histórico
- Verifique falhas e restarts de pods
- Avalie alocação de recursos (CPU/Memória)
- Considere estratégia de atualização (RollingUpdate, Recreate)
- Sugira rollback se necessário

**Contexto de Diagnóstico (JSON):**
{{CONTEXT}}

**Formate sua resposta claramente com cabeçalhos markdown e bullet points. RESPONDA SEMPRE EM PORTUGUÊS BRASILEIRO.**`

// Template para análise de HPAs
const hpaTemplate = `Você é um especialista em Kubernetes especializado em otimização de HPA (Horizontal Pod Autoscaler).

Analise o seguinte contexto de diagnóstico do HPA e forneça uma análise abrangente.

**Sua resposta deve incluir:**

1. **Análise da Causa Raiz**: Identifique problemas de escalamento (máximo atingido, oscilação, problemas de métricas)
2. **Avaliação de Impacto**: Descreva impacto na performance da aplicação e custo
3. **Ações Recomendadas**: Otimização de réplicas min/max, métricas alvo, comportamento
4. **Comandos kubectl**: Inclua comandos específicos para ajustar o HPA (prefixe com $)

**Importante:**
- Verifique se o HPA está no máximo (current == max replicas)
- Analise comportamento de escala (oscilação, escala agressiva)
- Avalie métricas alvo (utilização de CPU/Memória)
- Considere métricas customizadas se necessário
- Balance performance vs custo

**Contexto de Diagnóstico (JSON):**
{{CONTEXT}}

**Formate sua resposta claramente com cabeçalhos markdown e bullet points. RESPONDA SEMPRE EM PORTUGUÊS BRASILEIRO.**`

// Template para análise de Nodes
const nodeTemplate = `Você é um especialista em Kubernetes especializado em troubleshooting de Nodes.

Analise o seguinte contexto de diagnóstico do Node e forneça uma análise abrangente.

**Sua resposta deve incluir:**

1. **Análise da Causa Raiz**: Identifique problemas do node (NotReady, pressão de recursos, taints)
2. **Avaliação de Impacto**: Descreva impacto nos pods e estabilidade do cluster
3. **Ações Recomendadas**: Passos para restaurar saúde do node ou cordon/drain se necessário
4. **Comandos kubectl**: Inclua comandos específicos (prefixe com $)

**Importante:**
- Analise condições do node (Ready, MemoryPressure, DiskPressure, PIDPressure)
- Verifique capacidade de recursos vs allocatable
- Avalie taints e seu efeito no agendamento de pods
- Considere pods rodando no node
- Sugira cordon/drain se o node estiver com problemas

**Contexto de Diagnóstico (JSON):**
{{CONTEXT}}

**Formate sua resposta claramente com cabeçalhos markdown e bullet points. RESPONDA SEMPRE EM PORTUGUÊS BRASILEIRO.**`
