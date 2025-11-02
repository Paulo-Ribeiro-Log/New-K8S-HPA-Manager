package validation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ValidationError representa um erro de validação com contexto
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (valor: %s)", e.Field, e.Message, e.Value)
}

// ValidationResult contém o resultado de uma validação
type ValidationResult struct {
	Valid  bool               `json:"valid"`
	Errors []*ValidationError `json:"errors,omitempty"`
}

// AddError adiciona um erro ao resultado
func (r *ValidationResult) AddError(field, value, message string) {
	r.Valid = false
	r.Errors = append(r.Errors, &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	})
}

// CPU Validation
// Formatos válidos: "100m", "0.5", "1", "2.5"
var cpuRegex = regexp.MustCompile(`^(\d+\.?\d*|\.\d+)m?$`)

// ValidateCPU valida formato de CPU do Kubernetes
func ValidateCPU(value string) error {
	if value == "" {
		return nil // Empty is valid (optional field)
	}

	value = strings.TrimSpace(value)
	if !cpuRegex.MatchString(value) {
		return &ValidationError{
			Field:   "cpu",
			Value:   value,
			Message: "Formato inválido. Use: 100m (milicores), 0.5, 1, ou 2 (cores)",
		}
	}

	// Parse value to check if it's positive
	numStr := strings.TrimSuffix(value, "m")
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil || num <= 0 {
		return &ValidationError{
			Field:   "cpu",
			Value:   value,
			Message: "Valor deve ser positivo",
		}
	}

	// Check reasonable limits (max 128 cores)
	if strings.HasSuffix(value, "m") {
		if num > 128000 { // 128 cores = 128000m
			return &ValidationError{
				Field:   "cpu",
				Value:   value,
				Message: "Valor muito alto (máximo: 128 cores ou 128000m)",
			}
		}
	} else {
		if num > 128 {
			return &ValidationError{
				Field:   "cpu",
				Value:   value,
				Message: "Valor muito alto (máximo: 128 cores)",
			}
		}
	}

	return nil
}

// Memory Validation
// Formatos válidos: "128Mi", "1Gi", "512Mi", "2Gi"
var memoryRegex = regexp.MustCompile(`^(\d+)(Mi|Gi|M|G|Ki|K|Ti|T|Pi|P|Ei|E)$`)

// ValidateMemory valida formato de memória do Kubernetes
func ValidateMemory(value string) error {
	if value == "" {
		return nil // Empty is valid (optional field)
	}

	value = strings.TrimSpace(value)
	if !memoryRegex.MatchString(value) {
		return &ValidationError{
			Field:   "memory",
			Value:   value,
			Message: "Formato inválido. Use: 128Mi, 1Gi, 512Mi, 2Gi (K8s format)",
		}
	}

	matches := memoryRegex.FindStringSubmatch(value)
	if len(matches) < 3 {
		return &ValidationError{
			Field:   "memory",
			Value:   value,
			Message: "Formato inválido",
		}
	}

	num, _ := strconv.ParseInt(matches[1], 10, 64)
	unit := matches[2]

	// Check if value is positive
	if num <= 0 {
		return &ValidationError{
			Field:   "memory",
			Value:   value,
			Message: "Valor deve ser positivo",
		}
	}

	// Convert to bytes for validation
	bytes := num
	switch unit {
	case "Ki", "K":
		bytes *= 1024
	case "Mi", "M":
		bytes *= 1024 * 1024
	case "Gi", "G":
		bytes *= 1024 * 1024 * 1024
	case "Ti", "T":
		bytes *= 1024 * 1024 * 1024 * 1024
	case "Pi", "P":
		bytes *= 1024 * 1024 * 1024 * 1024 * 1024
	case "Ei", "E":
		bytes *= 1024 * 1024 * 1024 * 1024 * 1024 * 1024
	}

	// Check reasonable limits (max 1Ti = 1099511627776 bytes)
	maxBytes := int64(1024 * 1024 * 1024 * 1024) // 1Ti
	if bytes > maxBytes {
		return &ValidationError{
			Field:   "memory",
			Value:   value,
			Message: "Valor muito alto (máximo: 1Ti)",
		}
	}

	// Warn about very small values (< 64Mi)
	minBytes := int64(64 * 1024 * 1024) // 64Mi
	if bytes < minBytes {
		return &ValidationError{
			Field:   "memory",
			Value:   value,
			Message: "Valor muito baixo (mínimo recomendado: 64Mi)",
		}
	}

	return nil
}

// Target CPU/Memory Utilization (1-100%)
func ValidateTargetPercentage(value int32, fieldName string) error {
	if value < 1 || value > 100 {
		return &ValidationError{
			Field:   fieldName,
			Value:   fmt.Sprintf("%d", value),
			Message: "Valor deve estar entre 1 e 100 (%)",
		}
	}
	return nil
}

// Replicas Validation
func ValidateReplicas(min, max, current *int32) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Min replicas
	if min != nil {
		if *min < 1 {
			result.AddError("min_replicas", fmt.Sprintf("%d", *min), "Mínimo deve ser >= 1")
		}
		if *min > 100 {
			result.AddError("min_replicas", fmt.Sprintf("%d", *min), "Mínimo muito alto (máximo recomendado: 100)")
		}
	}

	// Max replicas
	if max != nil {
		if *max < 1 {
			result.AddError("max_replicas", fmt.Sprintf("%d", *max), "Máximo deve ser >= 1")
		}
		if *max > 1000 {
			result.AddError("max_replicas", fmt.Sprintf("%d", *max), "Máximo muito alto (máximo recomendado: 1000)")
		}
	}

	// Min <= Max
	if min != nil && max != nil && *min > *max {
		result.AddError("min_replicas", fmt.Sprintf("%d", *min), "Mínimo não pode ser maior que máximo")
	}

	// Current replicas
	if current != nil {
		if *current < 0 {
			result.AddError("replicas", fmt.Sprintf("%d", *current), "Réplicas não podem ser negativas")
		}
		if *current > 1000 {
			result.AddError("replicas", fmt.Sprintf("%d", *current), "Réplicas muito alto (máximo recomendado: 1000)")
		}
	}

	return result
}

// Node Pool Validation
func ValidateNodePool(nodeCount, minCount, maxCount *int32) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Node count
	if nodeCount != nil {
		if *nodeCount < 0 {
			result.AddError("node_count", fmt.Sprintf("%d", *nodeCount), "Node count não pode ser negativo")
		}
		if *nodeCount > 100 {
			result.AddError("node_count", fmt.Sprintf("%d", *nodeCount), "Node count muito alto (máximo recomendado: 100)")
		}
	}

	// Min count
	if minCount != nil {
		if *minCount < 0 {
			result.AddError("min_count", fmt.Sprintf("%d", *minCount), "Min count não pode ser negativo")
		}
		if *minCount > 100 {
			result.AddError("min_count", fmt.Sprintf("%d", *minCount), "Min count muito alto (máximo recomendado: 100)")
		}
	}

	// Max count
	if maxCount != nil {
		if *maxCount < 1 {
			result.AddError("max_count", fmt.Sprintf("%d", *maxCount), "Max count deve ser >= 1")
		}
		if *maxCount > 100 {
			result.AddError("max_count", fmt.Sprintf("%d", *maxCount), "Max count muito alto (máximo recomendado: 100)")
		}
	}

	// Min <= Max
	if minCount != nil && maxCount != nil && *minCount > *maxCount {
		result.AddError("min_count", fmt.Sprintf("%d", *minCount), "Min count não pode ser maior que max count")
	}

	return result
}

// ValidateHPAUpdate valida uma atualização de HPA completa
func ValidateHPAUpdate(minReplicas, maxReplicas *int32, targetCPU, targetMemory *int32, cpuRequest, memoryRequest, cpuLimit, memoryLimit string) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Validate replicas
	replicasResult := ValidateReplicas(minReplicas, maxReplicas, nil)
	if !replicasResult.Valid {
		result.Valid = false
		result.Errors = append(result.Errors, replicasResult.Errors...)
	}

	// Validate target percentages
	if targetCPU != nil {
		if err := ValidateTargetPercentage(*targetCPU, "target_cpu"); err != nil {
			result.AddError("target_cpu", fmt.Sprintf("%d", *targetCPU), err.(*ValidationError).Message)
		}
	}
	if targetMemory != nil {
		if err := ValidateTargetPercentage(*targetMemory, "target_memory"); err != nil {
			result.AddError("target_memory", fmt.Sprintf("%d", *targetMemory), err.(*ValidationError).Message)
		}
	}

	// Validate resources
	if cpuRequest != "" {
		if err := ValidateCPU(cpuRequest); err != nil {
			result.AddError("cpu_request", cpuRequest, err.(*ValidationError).Message)
		}
	}
	if memoryRequest != "" {
		if err := ValidateMemory(memoryRequest); err != nil {
			result.AddError("memory_request", memoryRequest, err.(*ValidationError).Message)
		}
	}
	if cpuLimit != "" {
		if err := ValidateCPU(cpuLimit); err != nil {
			result.AddError("cpu_limit", cpuLimit, err.(*ValidationError).Message)
		}
	}
	if memoryLimit != "" {
		if err := ValidateMemory(memoryLimit); err != nil {
			result.AddError("memory_limit", memoryLimit, err.(*ValidationError).Message)
		}
	}

	// Validate request <= limit
	if cpuRequest != "" && cpuLimit != "" {
		reqVal := parseCPUToMillicores(cpuRequest)
		limVal := parseCPUToMillicores(cpuLimit)
		if reqVal > limVal {
			result.AddError("cpu_request", cpuRequest, "CPU request não pode ser maior que CPU limit")
		}
	}
	if memoryRequest != "" && memoryLimit != "" {
		reqVal := parseMemoryToBytes(memoryRequest)
		limVal := parseMemoryToBytes(memoryLimit)
		if reqVal > limVal {
			result.AddError("memory_request", memoryRequest, "Memory request não pode ser maior que memory limit")
		}
	}

	return result
}

// Helper: Convert CPU string to millicores
func parseCPUToMillicores(cpu string) int64 {
	cpu = strings.TrimSpace(cpu)
	if strings.HasSuffix(cpu, "m") {
		numStr := strings.TrimSuffix(cpu, "m")
		val, _ := strconv.ParseInt(numStr, 10, 64)
		return val
	}
	val, _ := strconv.ParseFloat(cpu, 64)
	return int64(val * 1000)
}

// Helper: Convert memory string to bytes
func parseMemoryToBytes(memory string) int64 {
	memory = strings.TrimSpace(memory)
	matches := memoryRegex.FindStringSubmatch(memory)
	if len(matches) < 3 {
		return 0
	}

	num, _ := strconv.ParseInt(matches[1], 10, 64)
	unit := matches[2]

	bytes := num
	switch unit {
	case "Ki", "K":
		bytes *= 1024
	case "Mi", "M":
		bytes *= 1024 * 1024
	case "Gi", "G":
		bytes *= 1024 * 1024 * 1024
	case "Ti", "T":
		bytes *= 1024 * 1024 * 1024 * 1024
	}
	return bytes
}
