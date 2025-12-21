package sanitizer

import "regexp"

// Regex patterns para detecção de dados sensíveis
var (
	// IPv4Pattern detecta endereços IPv4 (ex: 192.168.1.1)
	IPv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	// EmailPattern detecta endereços de email
	EmailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// JWTPattern detecta tokens JWT (eyJ...)
	JWTPattern = regexp.MustCompile(`eyJ[a-zA-Z0-9_\-]*\.eyJ[a-zA-Z0-9_\-]*\.[a-zA-Z0-9_\-]*`)

	// BearerTokenPattern detecta Bearer tokens
	BearerTokenPattern = regexp.MustCompile(`Bearer\s+[a-zA-Z0-9_\-\.]+`)

	// UUIDPattern detecta UUIDs (8-4-4-4-12)
	UUIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

	// Base64Pattern detecta strings base64 longas (>20 chars)
	Base64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}`)

	// APIKeyPattern detecta API keys comuns (prefixo conhecido + string longa)
	APIKeyPattern = regexp.MustCompile(`(?i)(api[_\-]?key|apikey|key)["\s:=]+([a-zA-Z0-9_\-]{20,})`)

	// PasswordPattern detecta passwords em contexto (password=... ou password: ...)
	PasswordPattern = regexp.MustCompile(`(?i)(password|passwd|pwd)["\s:=]+([^\s,;"'}]+)`)

	// AzureClientSecretPattern detecta Azure client secrets
	AzureClientSecretPattern = regexp.MustCompile(`[a-zA-Z0-9~_\-\.]{34,}`)

	// GCPKeyPattern detecta chaves GCP
	GCPKeyPattern = regexp.MustCompile(`"type":\s*"service_account"`)
)

// Replacements padrão para cada tipo de dado
const (
	IPReplacement        = "X.X.X.X"
	EmailReplacement     = "user@REDACTED"
	JWTReplacement       = "eyJ***REDACTED***"
	BearerReplacement    = "Bearer ***REDACTED***"
	UUIDReplacement      = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
	Base64Replacement    = "***BASE64_REDACTED***"
	SecretReplacement    = "***REDACTED***"
	PasswordReplacement  = "***PASSWORD_REDACTED***"
	APIKeyReplacement    = "***APIKEY_REDACTED***"
)

// IsSensitiveKey verifica se uma chave é considerada sensível
func IsSensitiveKey(key string, sensitiveKeys []string) bool {
	keyLower := regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(
		regexp.MustCompile(`\s+`).ReplaceAllString(key, ""),
		"",
	)

	for _, sensitiveKey := range sensitiveKeys {
		if regexp.MustCompile(`(?i)`+sensitiveKey).MatchString(keyLower) {
			return true
		}
	}

	return false
}

// SanitizePattern aplica um pattern de sanitização
func SanitizePattern(text string, pattern *regexp.Regexp, replacement string) (string, int) {
	matches := pattern.FindAllString(text, -1)
	count := len(matches)

	if count > 0 {
		text = pattern.ReplaceAllString(text, replacement)
	}

	return text, count
}

// SanitizePatternWithGroup aplica sanitização preservando grupo de captura
// Ex: password=secret123 → password=***REDACTED***
func SanitizePatternWithGroup(text string, pattern *regexp.Regexp, replacement string) (string, int) {
	matches := pattern.FindAllStringSubmatch(text, -1)
	count := len(matches)

	if count > 0 {
		text = pattern.ReplaceAllString(text, "$1"+replacement)
	}

	return text, count
}
