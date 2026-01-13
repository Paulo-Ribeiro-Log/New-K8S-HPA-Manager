package sanitizer

// SanitizationConfig define configurações de sanitização
type SanitizationConfig struct {
	// MaskIPs se verdadeiro, mascara endereços IPv4
	MaskIPs bool

	// MaskEmails se verdadeiro, mascara endereços de email
	MaskEmails bool

	// MaskTokens se verdadeiro, mascara tokens JWT, Bearer tokens, API keys
	MaskTokens bool

	// MaskSecrets se verdadeiro, mascara valores de secrets do Kubernetes
	MaskSecrets bool

	// MaskEnvVars se verdadeiro, mascara variáveis de ambiente sensíveis
	MaskEnvVars bool

	// CustomPatterns padrões customizados adicionais (regex → replacement)
	CustomPatterns map[string]string

	// SensitiveKeys lista de chaves consideradas sensíveis (case-insensitive)
	// Ex: ["password", "token", "secret", "apikey"]
	SensitiveKeys []string
}

// DefaultConfig retorna configuração padrão (tudo habilitado)
func DefaultConfig() *SanitizationConfig {
	return &SanitizationConfig{
		MaskIPs:     true,
		MaskEmails:  true,
		MaskTokens:  true,
		MaskSecrets: true,
		MaskEnvVars: true,
		SensitiveKeys: []string{
			"password", "passwd", "pwd",
			"secret", "token", "apikey", "api_key", "api-key",
			"authorization", "auth",
			"certificate", "cert", "key",
			"private", "credential",
		},
		CustomPatterns: make(map[string]string),
	}
}

// SanitizationResult representa o resultado de uma operação de sanitização
type SanitizationResult struct {
	// Original texto original (apenas para debug, não deve ser exposto)
	Original string

	// Sanitized texto sanitizado
	Sanitized string

	// MaskedItems contador de itens mascarados por tipo
	MaskedItems map[string]int

	// Warnings avisos gerados durante sanitização
	Warnings []string
}
