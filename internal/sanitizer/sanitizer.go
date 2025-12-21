package sanitizer

import (
	"regexp"
	"sync"
)

// Sanitizer é responsável por sanitizar dados sensíveis
type Sanitizer struct {
	config *SanitizationConfig
	mu     sync.RWMutex
}

// New cria um novo Sanitizer com configuração padrão
func New() *Sanitizer {
	return &Sanitizer{
		config: DefaultConfig(),
	}
}

// NewWithConfig cria um novo Sanitizer com configuração customizada
func NewWithConfig(config *SanitizationConfig) *Sanitizer {
	return &Sanitizer{
		config: config,
	}
}

// SetConfig atualiza a configuração (thread-safe)
func (s *Sanitizer) SetConfig(config *SanitizationConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// GetConfig retorna cópia da configuração atual (thread-safe)
func (s *Sanitizer) GetConfig() *SanitizationConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy para evitar modificação externa
	configCopy := *s.config
	configCopy.SensitiveKeys = make([]string, len(s.config.SensitiveKeys))
	copy(configCopy.SensitiveKeys, s.config.SensitiveKeys)

	configCopy.CustomPatterns = make(map[string]string, len(s.config.CustomPatterns))
	for k, v := range s.config.CustomPatterns {
		configCopy.CustomPatterns[k] = v
	}

	return &configCopy
}

// SanitizeText sanitiza um texto aplicando todos os patterns habilitados
func (s *Sanitizer) SanitizeText(text string) string {
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()

	sanitized := text
	var count int

	// IPv4
	if config.MaskIPs {
		sanitized, count = SanitizePattern(sanitized, IPv4Pattern, IPReplacement)
	}

	// Email
	if config.MaskEmails {
		sanitized, count = SanitizePattern(sanitized, EmailPattern, EmailReplacement)
	}

	// Tokens
	if config.MaskTokens {
		// JWT tokens
		sanitized, count = SanitizePattern(sanitized, JWTPattern, JWTReplacement)

		// Bearer tokens
		sanitized, count = SanitizePattern(sanitized, BearerTokenPattern, BearerReplacement)

		// UUIDs (podem ser tokens de sessão)
		sanitized, count = SanitizePattern(sanitized, UUIDPattern, UUIDReplacement)

		// API Keys com contexto
		sanitized, count = SanitizePatternWithGroup(sanitized, APIKeyPattern, APIKeyReplacement)

		// Passwords com contexto
		sanitized, count = SanitizePatternWithGroup(sanitized, PasswordPattern, PasswordReplacement)

		// Base64 longas (podem ser tokens)
		sanitized, count = SanitizePattern(sanitized, Base64Pattern, Base64Replacement)
	}

	// Custom patterns
	for pattern, replacement := range config.CustomPatterns {
		// Ignora erros de regex inválida (apenas skip)
		if regexPattern, err := regexp.Compile(pattern); err == nil {
			sanitized, _ = SanitizePattern(sanitized, regexPattern, replacement)
		}
	}

	// Suprimir warning de unused
	_ = count

	return sanitized
}

// SanitizeTextWithResult sanitiza um texto e retorna resultado detalhado
func (s *Sanitizer) SanitizeTextWithResult(text string) *SanitizationResult {
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()

	result := &SanitizationResult{
		Original:    text,
		Sanitized:   text,
		MaskedItems: make(map[string]int),
		Warnings:    []string{},
	}

	var count int

	// IPv4
	if config.MaskIPs {
		result.Sanitized, count = SanitizePattern(result.Sanitized, IPv4Pattern, IPReplacement)
		if count > 0 {
			result.MaskedItems["ipv4"] = count
		}
	}

	// Email
	if config.MaskEmails {
		result.Sanitized, count = SanitizePattern(result.Sanitized, EmailPattern, EmailReplacement)
		if count > 0 {
			result.MaskedItems["email"] = count
		}
	}

	// Tokens
	if config.MaskTokens {
		// JWT
		result.Sanitized, count = SanitizePattern(result.Sanitized, JWTPattern, JWTReplacement)
		if count > 0 {
			result.MaskedItems["jwt"] = count
		}

		// Bearer
		result.Sanitized, count = SanitizePattern(result.Sanitized, BearerTokenPattern, BearerReplacement)
		if count > 0 {
			result.MaskedItems["bearer"] = count
		}

		// UUID
		result.Sanitized, count = SanitizePattern(result.Sanitized, UUIDPattern, UUIDReplacement)
		if count > 0 {
			result.MaskedItems["uuid"] = count
		}

		// API Keys
		result.Sanitized, count = SanitizePatternWithGroup(result.Sanitized, APIKeyPattern, APIKeyReplacement)
		if count > 0 {
			result.MaskedItems["apikey"] = count
		}

		// Passwords
		result.Sanitized, count = SanitizePatternWithGroup(result.Sanitized, PasswordPattern, PasswordReplacement)
		if count > 0 {
			result.MaskedItems["password"] = count
		}

		// Base64
		result.Sanitized, count = SanitizePattern(result.Sanitized, Base64Pattern, Base64Replacement)
		if count > 0 {
			result.MaskedItems["base64"] = count
		}
	}

	// Custom patterns
	for pattern, replacement := range config.CustomPatterns {
		regexPattern, err := regexp.Compile(pattern)
		if err != nil {
			result.Warnings = append(result.Warnings, "invalid custom pattern: "+pattern)
			continue
		}

		result.Sanitized, count = SanitizePattern(result.Sanitized, regexPattern, replacement)
		if count > 0 {
			result.MaskedItems["custom_"+pattern] = count
		}
	}

	return result
}

// SanitizeMultiple sanitiza múltiplos textos de uma vez (útil para batch processing)
func (s *Sanitizer) SanitizeMultiple(texts []string) []string {
	sanitized := make([]string, len(texts))

	for i, text := range texts {
		sanitized[i] = s.SanitizeText(text)
	}

	return sanitized
}
