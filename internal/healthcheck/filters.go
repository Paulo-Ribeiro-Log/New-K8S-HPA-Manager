package healthcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// FilterType tipo de filtro
type FilterType string

const (
	FilterTypeExact     FilterType = "exact"      // Match exato: namespace + nome
	FilterTypeNamespace FilterType = "namespace"  // Match por namespace inteiro
	FilterTypeCategory  FilterType = "category"   // Match por categoria de problema
)

// FilterCategory categorias de problemas conhecidos
type FilterCategory string

const (
	// Deployments
	CategoryDeploymentNoProbes         FilterCategory = "deployment_no_probes"          // Deployment sem probes
	CategoryDeploymentNoLivenessProbe  FilterCategory = "deployment_no_liveness_probe"  // Deployment sem liveness probe
	CategoryDeploymentNoReadinessProbe FilterCategory = "deployment_no_readiness_probe" // Deployment sem readiness probe

	// ConfigMaps
	CategoryConfigMapEmpty FilterCategory = "configmap_empty" // ConfigMap vazio

	// Secrets
	CategorySecretEmpty           FilterCategory = "secret_empty"             // Secret vazio
	CategorySecretServiceAccount  FilterCategory = "secret_service_account"   // Service account token
	CategorySecretInvalidValues   FilterCategory = "secret_invalid_values"    // Secret com valores inválidos
)

// FilterRule representa uma regra de filtro
type FilterRule struct {
	ID           string         `json:"id"`            // UUID único
	Type         FilterType     `json:"type"`          // Tipo de filtro (exact, namespace, category)
	ResourceType ResourceType   `json:"resource_type"` // Deployment, ConfigMap, Secret
	Namespace    string         `json:"namespace"`     // Namespace (obrigatório para exact)
	Name         string         `json:"name"`          // Nome (obrigatório para exact)
	Category     FilterCategory `json:"category"`      // Categoria (obrigatório para category)
	Reason       string         `json:"reason"`        // Descrição legível do filtro
	CreatedAt    string         `json:"created_at"`    // ISO timestamp
	CreatedBy    string         `json:"created_by"`    // Email do usuário que criou
}

// FiltersConfig arquivo de configuração de filtros
type FiltersConfig struct {
	Version string       `json:"version"` // Versão do schema
	Rules   []FilterRule `json:"rules"`   // Lista de regras
}

// FilterManager gerencia filtros de falsos positivos
type FilterManager struct {
	configPath string
	config     FiltersConfig
	mu         sync.RWMutex
}

// NewFilterManager cria um novo gerenciador de filtros
func NewFilterManager(configPath string) (*FilterManager, error) {
	fm := &FilterManager{
		configPath: configPath,
		config: FiltersConfig{
			Version: "1.0",
			Rules:   []FilterRule{},
		},
	}

	// Carregar configuração existente (se houver)
	if err := fm.Load(); err != nil {
		// Se arquivo não existe, criar com filtros padrão
		if os.IsNotExist(err) {
			fm.initializeDefaults()
			if err := fm.Save(); err != nil {
				return nil, fmt.Errorf("failed to save default filters: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to load filters config: %w", err)
		}
	}

	log.Info().
		Str("config_path", configPath).
		Int("rules_count", len(fm.config.Rules)).
		Msg("Filter manager initialized")

	return fm, nil
}

// initializeDefaults inicializa filtros padrão
func (fm *FilterManager) initializeDefaults() {
	defaultRules := []FilterRule{
		// ===== FILTROS POR CATEGORIA =====

		// ConfigMaps vazios (TODOS)
		{
			ID:           "category-configmap-empty",
			Type:         FilterTypeCategory,
			ResourceType: ResourceConfigMap,
			Category:     CategoryConfigMapEmpty,
			Reason:       "Ignorar todos ConfigMaps vazios (comum em ingress, cert-manager, etc)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// Secrets vazios de sistema
		{
			ID:           "category-secret-service-account",
			Type:         FilterTypeCategory,
			ResourceType: ResourceSecret,
			Category:     CategorySecretServiceAccount,
			Reason:       "Ignorar service account tokens (gerados automaticamente pelo K8s)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// ===== FILTROS POR NAMESPACE =====

		// kube-system (ignorar deployments sem probes)
		{
			ID:           "namespace-kube-system-deployments",
			Type:         FilterTypeNamespace,
			ResourceType: ResourceDeployment,
			Namespace:    "kube-system",
			Reason:       "Ignorar deployments sem probes em kube-system (componentes de sistema)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// calico-system
		{
			ID:           "namespace-calico-system",
			Type:         FilterTypeNamespace,
			ResourceType: ResourceDeployment,
			Namespace:    "calico-system",
			Reason:       "Ignorar deployments sem probes em calico-system (CNI)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// dynatrace
		{
			ID:           "namespace-dynatrace",
			Type:         FilterTypeNamespace,
			ResourceType: ResourceDeployment,
			Namespace:    "dynatrace",
			Reason:       "Ignorar deployments sem probes em dynatrace (observabilidade)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// istio-system
		{
			ID:           "namespace-istio-system",
			Type:         FilterTypeNamespace,
			ResourceType: ResourceDeployment,
			Namespace:    "istio-system",
			Reason:       "Ignorar deployments sem probes em istio-system (service mesh)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},

		// ===== FILTROS EXATOS (recursos específicos problemáticos) =====

		// Dynatrace webhook certs (secret com valores inválidos)
		{
			ID:           "exact-dynatrace-webhook-certs",
			Type:         FilterTypeExact,
			ResourceType: ResourceSecret,
			Namespace:    "dynatrace",
			Name:         "dynatrace-webhook-certs",
			Reason:       "Secret de certificados gerado automaticamente (pode ter valores vazios inicialmente)",
			CreatedAt:    "2025-12-30T00:00:00Z",
			CreatedBy:    "system",
		},
	}

	fm.config.Rules = defaultRules
}

// Load carrega configuração do arquivo
func (fm *FilterManager) Load() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	data, err := os.ReadFile(fm.configPath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &fm.config); err != nil {
		return fmt.Errorf("failed to parse filters config: %w", err)
	}

	return nil
}

// Save salva configuração no arquivo
func (fm *FilterManager) Save() error {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Criar diretório se não existir
	dir := filepath.Dir(fm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(fm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal filters config: %w", err)
	}

	if err := os.WriteFile(fm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write filters config: %w", err)
	}

	log.Debug().
		Str("config_path", fm.configPath).
		Int("rules_count", len(fm.config.Rules)).
		Msg("Filters config saved")

	return nil
}

// AddRule adiciona nova regra de filtro
func (fm *FilterManager) AddRule(rule FilterRule) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Validar campos obrigatórios por tipo
	switch rule.Type {
	case FilterTypeExact:
		if rule.ResourceType == "" || rule.Namespace == "" || rule.Name == "" {
			return fmt.Errorf("filtro exact requer resource_type, namespace e name")
		}
	case FilterTypeNamespace:
		if rule.ResourceType == "" || rule.Namespace == "" {
			return fmt.Errorf("filtro namespace requer resource_type e namespace")
		}
	case FilterTypeCategory:
		if rule.ResourceType == "" || rule.Category == "" {
			return fmt.Errorf("filtro category requer resource_type e category")
		}
	default:
		return fmt.Errorf("tipo de filtro inválido: %s", rule.Type)
	}

	// Verificar duplicatas
	for _, existing := range fm.config.Rules {
		if fm.rulesAreEqual(existing, rule) {
			return fmt.Errorf("regra duplicada")
		}
	}

	fm.config.Rules = append(fm.config.Rules, rule)

	log.Info().
		Str("type", string(rule.Type)).
		Str("resource_type", string(rule.ResourceType)).
		Str("namespace", rule.Namespace).
		Str("name", rule.Name).
		Str("category", string(rule.Category)).
		Msg("Filter rule added")

	return nil
}

// rulesAreEqual compara duas regras
func (fm *FilterManager) rulesAreEqual(a, b FilterRule) bool {
	return a.Type == b.Type &&
		a.ResourceType == b.ResourceType &&
		a.Namespace == b.Namespace &&
		a.Name == b.Name &&
		a.Category == b.Category
}

// RemoveRule remove regra de filtro por ID
func (fm *FilterManager) RemoveRule(id string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	newRules := []FilterRule{}
	found := false

	for _, rule := range fm.config.Rules {
		if rule.ID == id {
			found = true
			log.Info().
				Str("id", id).
				Str("type", string(rule.Type)).
				Msg("Filter rule removed")
			continue
		}
		newRules = append(newRules, rule)
	}

	if !found {
		return fmt.Errorf("regra não encontrada: %s", id)
	}

	fm.config.Rules = newRules
	return nil
}

// GetRules retorna todas as regras
func (fm *FilterManager) GetRules() []FilterRule {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Copiar para evitar race conditions
	rules := make([]FilterRule, len(fm.config.Rules))
	copy(rules, fm.config.Rules)
	return rules
}

// ShouldFilter verifica se um recurso deve ser filtrado
// Parâmetros:
//   - resourceType: Deployment, ConfigMap, Secret
//   - namespace: namespace do recurso
//   - name: nome do recurso
//   - message: mensagem de erro/warning (usado para category matching)
func (fm *FilterManager) ShouldFilter(resourceType ResourceType, namespace, name, message string) bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	for _, rule := range fm.config.Rules {
		// Ignorar se tipo de recurso diferente
		if rule.ResourceType != resourceType {
			continue
		}

		switch rule.Type {
		case FilterTypeExact:
			// Match exato: namespace + nome
			if rule.Namespace == namespace && rule.Name == name {
				log.Debug().
					Str("type", "exact").
					Str("namespace", namespace).
					Str("name", name).
					Msg("Resource filtered (exact match)")
				return true
			}

		case FilterTypeNamespace:
			// Match por namespace inteiro
			if rule.Namespace == namespace {
				log.Debug().
					Str("type", "namespace").
					Str("namespace", namespace).
					Str("name", name).
					Msg("Resource filtered (namespace match)")
				return true
			}

		case FilterTypeCategory:
			// Match por categoria (análise da mensagem)
			if fm.matchesCategory(rule.Category, message) {
				log.Debug().
					Str("type", "category").
					Str("category", string(rule.Category)).
					Str("namespace", namespace).
					Str("name", name).
					Msg("Resource filtered (category match)")
				return true
			}
		}
	}

	return false
}

// matchesCategory verifica se mensagem match com categoria
func (fm *FilterManager) matchesCategory(category FilterCategory, message string) bool {
	messageLower := strings.ToLower(message)

	switch category {
	case CategoryDeploymentNoProbes:
		return strings.Contains(messageLower, "liveness e readiness probes não configurados")

	case CategoryDeploymentNoLivenessProbe:
		return strings.Contains(messageLower, "liveness probe não configurado")

	case CategoryDeploymentNoReadinessProbe:
		return strings.Contains(messageLower, "readiness probe não configurado")

	case CategoryConfigMapEmpty:
		return strings.Contains(messageLower, "configmap vazio")

	case CategorySecretEmpty:
		return strings.Contains(messageLower, "secret vazio")

	case CategorySecretServiceAccount:
		return strings.Contains(messageLower, "service account") ||
			strings.Contains(messageLower, "token")

	case CategorySecretInvalidValues:
		return strings.Contains(messageLower, "valores inválidos")

	default:
		return false
	}
}

// GetStats retorna estatísticas de filtros
func (fm *FilterManager) GetStats() map[string]int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	stats := map[string]int{
		"total":     len(fm.config.Rules),
		"exact":     0,
		"namespace": 0,
		"category":  0,
	}

	for _, rule := range fm.config.Rules {
		stats[string(rule.Type)]++
	}

	return stats
}
