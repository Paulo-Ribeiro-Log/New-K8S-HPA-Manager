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

	// Aplicar APENAS mascaramento de tokens (base64 + connection strings)
	if config.MaskTokens {
		// 1. Connection strings - mascara APENAS a senha (entre : e @)
		// Exemplo: user:senha@host -> user:se**ha@host
		connStrPattern := regexp.MustCompile(`([^:@\s]+):([^:@\s]+)(@[^\s]+)`)
		sanitized = connStrPattern.ReplaceAllStringFunc(sanitized, func(match string) string {
			parts := connStrPattern.FindStringSubmatch(match)
			if len(parts) == 4 {
				user := parts[1]
				password := parts[2]
				rest := parts[3] // @host:port/db

				// Mascara apenas a senha
				maskedPassword := MaskPartial(password, 2)
				return user + ":" + maskedPassword + rest
			}
			return match
		})

		// 2. Base64 longas (>30 chars) - mascara parcialmente
		// Exemplo: MDFhghthghthghthghthghthghthghtTRk4= -> MDF****************************TRk4=
		sanitized = Base64Pattern.ReplaceAllStringFunc(sanitized, func(match string) string {
			if len(match) > 30 {
				return MaskPartial(match, 3)
			}
			return match // Mantém base64 curtas intactas
		})
	}

	// Custom patterns (se houver)
	for pattern, replacement := range config.CustomPatterns {
		if regexPattern, err := regexp.Compile(pattern); err == nil {
			sanitized, _ = SanitizePattern(sanitized, regexPattern, replacement)
		}
	}

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

	// Aplicar APENAS mascaramento de tokens (base64 + connection strings)
	if config.MaskTokens {
		// 1. Connection strings - mascara APENAS a senha
		connStrPattern := regexp.MustCompile(`([^:@\s]+):([^:@\s]+)(@[^\s]+)`)
		matches := connStrPattern.FindAllString(result.Sanitized, -1)
		if len(matches) > 0 {
			result.Sanitized = connStrPattern.ReplaceAllStringFunc(result.Sanitized, func(match string) string {
				parts := connStrPattern.FindStringSubmatch(match)
				if len(parts) == 4 {
					return parts[1] + ":" + MaskPartial(parts[2], 2) + parts[3]
				}
				return match
			})
			result.MaskedItems["connection_string"] = len(matches)
		}

		// 2. Base64 longas - mascara parcialmente
		matches = Base64Pattern.FindAllString(result.Sanitized, -1)
		count = 0
		result.Sanitized = Base64Pattern.ReplaceAllStringFunc(result.Sanitized, func(match string) string {
			if len(match) > 30 {
				count++
				return MaskPartial(match, 3)
			}
			return match
		})
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
