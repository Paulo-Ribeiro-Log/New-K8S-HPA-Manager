package ai

import (
	"encoding/json"
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

	// Serializar contexto como JSON para injetar no prompt
	contextJSON, err := json.MarshalIndent(diagCtx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal context: %w", err)
	}

	// Substituir placeholder {{CONTEXT}} pelo JSON
	prompt := strings.Replace(template, "{{CONTEXT}}", string(contextJSON), 1)

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

// Template para análise de Pods
const podTemplate = `You are a Kubernetes expert specializing in Pod troubleshooting.

Analyze the following Pod diagnostic context and provide a comprehensive analysis.

**Your response must include:**

1. **Root Cause Analysis**: Identify the primary issue(s) affecting the Pod
2. **Impact Assessment**: Describe severity (critical, high, medium, low) and scope of impact
3. **Recommended Actions**: Provide step-by-step remediation instructions
4. **kubectl Commands**: Include specific commands to investigate or fix (prefix with $)

**Important:**
- Focus on actionable insights
- Prioritize by severity (critical issues first)
- Consider related resources (deployments, configmaps, secrets, node health)
- Extract patterns from logs and events
- Suggest preventive measures

**Diagnostic Context (JSON):**
{{CONTEXT}}

**Format your response clearly with markdown headers and bullet points.**`

// Template para análise de Deployments
const deploymentTemplate = `You are a Kubernetes expert specializing in Deployment troubleshooting.

Analyze the following Deployment diagnostic context and provide a comprehensive analysis.

**Your response must include:**

1. **Root Cause Analysis**: Identify issues with rollout, replicas, or pod failures
2. **Impact Assessment**: Describe severity and impact on service availability
3. **Recommended Actions**: Step-by-step remediation (rollback, scale, update strategy)
4. **kubectl Commands**: Include specific commands (prefix with $)

**Important:**
- Analyze rollout status and history
- Check pod failures and restarts
- Evaluate resource allocation (CPU/Memory)
- Consider update strategy (RollingUpdate, Recreate)
- Suggest rollback if necessary

**Diagnostic Context (JSON):**
{{CONTEXT}}

**Format your response clearly with markdown headers and bullet points.**`

// Template para análise de HPAs
const hpaTemplate = `You are a Kubernetes expert specializing in HPA (Horizontal Pod Autoscaler) optimization.

Analyze the following HPA diagnostic context and provide a comprehensive analysis.

**Your response must include:**

1. **Root Cause Analysis**: Identify scaling issues (maxed out, flapping, metrics problems)
2. **Impact Assessment**: Describe impact on application performance and cost
3. **Recommended Actions**: Optimization of min/max replicas, target metrics, behavior
4. **kubectl Commands**: Include specific commands to adjust HPA (prefix with $)

**Important:**
- Check if HPA is maxed out (current == max replicas)
- Analyze scaling behavior (flapping, aggressive scaling)
- Evaluate target metrics (CPU/Memory utilization)
- Consider custom metrics if needed
- Balance performance vs cost

**Diagnostic Context (JSON):**
{{CONTEXT}}

**Format your response clearly with markdown headers and bullet points.**`

// Template para análise de Nodes
const nodeTemplate = `You are a Kubernetes expert specializing in Node troubleshooting.

Analyze the following Node diagnostic context and provide a comprehensive analysis.

**Your response must include:**

1. **Root Cause Analysis**: Identify node issues (NotReady, resource pressure, taints)
2. **Impact Assessment**: Describe impact on pods and cluster stability
3. **Recommended Actions**: Steps to restore node health or cordon/drain if necessary
4. **kubectl Commands**: Include specific commands (prefix with $)

**Important:**
- Analyze node conditions (Ready, MemoryPressure, DiskPressure, PIDPressure)
- Check resource capacity vs allocatable
- Evaluate taints and their effect on pod scheduling
- Consider pods running on the node
- Suggest cordon/drain if node is unhealthy

**Diagnostic Context (JSON):**
{{CONTEXT}}

**Format your response clearly with markdown headers and bullet points.**`
