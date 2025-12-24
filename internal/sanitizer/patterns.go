package sanitizer

import (
	"regexp"
	"strings"
)

// Regex patterns para detecção de dados sensíveis
var (
	// IPv4Pattern - DESABILITADO (IPs não são mascarados)
	// IPv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	// EmailPattern - DESABILITADO (emails não são mascarados)
	// EmailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// CertificatePattern detecta referências a certificados (.cert/.key)
	// Ex: "certificado-tls.cert" -> "certificado<certificado-tls>"
	CertificatePattern = regexp.MustCompile(`([a-zA-Z0-9\-_]+)\.(cert|key)`)

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
	IPReplacement       = "X.X.X.X"
	EmailReplacement    = "user@REDACTED"
	JWTReplacement      = "eyJ***REDACTED***"
	BearerReplacement   = "Bearer ***REDACTED***"
	UUIDReplacement     = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
	Base64Replacement   = "***BASE64_REDACTED***"
	SecretReplacement   = "***REDACTED***"
	PasswordReplacement = "***PASSWORD_REDACTED***"
	APIKeyReplacement   = "***APIKEY_REDACTED***"
)

// MaskPartial mascara parcialmente um valor mostrando início e fim
// Ex: uso genérico com showChars simétrico
func MaskPartial(value string, showChars int) string {
	if len(value) <= showChars*2 {
		// Se muito curto, mostra tudo
		return value
	}

	prefix := value[:showChars]
	suffix := value[len(value)-showChars:]
	middle := len(value) - (showChars * 2)
	mask := strings.Repeat("*", middle)

	return prefix + mask + suffix
}

// MaskPassword mascara senha de connection string (4 primeiros + 3 últimos)
// Ex: "s6Yxbn1I9i98GHIJcJdc" -> "s6Yx*************Jdc"
func MaskPassword(password string) string {
	showPrefix := 4
	showSuffix := 3

	if len(password) <= showPrefix+showSuffix {
		// Se muito curto, mostra tudo
		return password
	}

	prefix := password[:showPrefix]
	suffix := password[len(password)-showSuffix:]
	middle := len(password) - (showPrefix + showSuffix)
	mask := strings.Repeat("*", middle)

	return prefix + mask + suffix
}

// MaskBase64 mascara base64 mostrando 3 primeiros + 3/4 últimos (mantém "=" se existir)
// Ex: "MDFhghthghthghthghthghthghthghtTRk4=" -> "MDF*****************************Rk4="
func MaskBase64(value string) string {
	showPrefix := 3
	showSuffix := 3

	// Se termina com "=", mostra 4 caracteres no final (incluindo o "=")
	if strings.HasSuffix(value, "=") {
		showSuffix = 4
	}

	// Se muito curto, mostra tudo (não mascara)
	if len(value) <= showPrefix+showSuffix {
		return value
	}

	prefix := value[:showPrefix]
	suffix := value[len(value)-showSuffix:]
	middle := len(value) - (showPrefix + showSuffix)
	mask := strings.Repeat("*", middle)

	return prefix + mask + suffix
}

// MaskConnectionString mascara senha em connection string preservando estrutura completa
// Ex: "mongodb://user:password@host:27017/db" -> "mongodb://user:pass****ord@host:27017/db"
// Suporta senhas com @ interno (ex: "MyP@ssw0rd")
func MaskConnectionString(connStr string) string {
	// 1. Extrair protocolo (se houver)
	protoPattern := regexp.MustCompile(`^(mongodb|postgres|postgresql|mysql|redis|amqp|https?)://(.+)$`)
	protoMatches := protoPattern.FindStringSubmatch(connStr)

	var protocol, content string
	if len(protoMatches) == 3 {
		protocol = protoMatches[1] + "://"
		content = protoMatches[2]
	} else {
		// Sem protocolo - verificar se é connection string (hostname com 3+ partes)
		if !strings.Contains(connStr, "@") || !strings.Contains(connStr, ":") {
			return connStr // Não é connection string
		}
		// Verificar se hostname tem múltiplas partes (ex: mdbp-logreversa-1.dc.nova)
		atIdx := strings.LastIndex(connStr, "@")
		if atIdx == -1 {
			return connStr
		}
		hostname := connStr[atIdx+1:]
		// Hostname deve ter pelo menos 2 pontos (3 partes) para não ser email simples
		if strings.Count(hostname, ".") < 2 {
			return connStr // Provavelmente é email, não connection string
		}
		protocol = ""
		content = connStr
	}

	// 2. Processar content: user:password@host
	// Usar ÚLTIMO @ como delimitador (senha pode ter @ interno)
	lastAtIdx := strings.LastIndex(content, "@")
	if lastAtIdx == -1 {
		return connStr
	}

	beforeAt := content[:lastAtIdx]
	afterAt := content[lastAtIdx+1:]

	colonIdx := strings.Index(beforeAt, ":")
	if colonIdx == -1 {
		return connStr
	}

	user := beforeAt[:colonIdx]
	password := beforeAt[colonIdx+1:]

	// 3. Mascara senha (4 primeiros + 3 últimos)
	maskedPassword := MaskPassword(password)

	return protocol + user + ":" + maskedPassword + "@" + afterAt
}

// IsSensitiveKey verifica se uma chave é considerada sensível
func IsSensitiveKey(key string, sensitiveKeys []string) bool {
	keyLower := regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(
		regexp.MustCompile(`\s+`).ReplaceAllString(key, ""),
		"",
	)

	for _, sensitiveKey := range sensitiveKeys {
		if regexp.MustCompile(`(?i)` + sensitiveKey).MatchString(keyLower) {
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
