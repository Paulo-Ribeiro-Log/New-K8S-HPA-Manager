package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims define o payload do token JWT.
// Contém identidade e permissões embutidas para que o backend não precise
// consultar o Azure AD a cada request — apenas no momento do login.
type JWTClaims struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	IsSRE bool   `json:"is_sre"`
	jwt.RegisteredClaims
}

// JWTManager gerencia geração e validação de tokens JWT.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// NewJWTManager cria um JWTManager. Secret deve ter pelo menos 32 bytes para HS256.
// TTL define o tempo de vida do token (padrão recomendado: 8h).
func NewJWTManager(secret []byte, ttl time.Duration) *JWTManager {
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &JWTManager{
		secret: secret,
		ttl:    ttl,
		issuer: "k8s-hpa-manager",
	}
}

// IsConfigured retorna true se um secret foi configurado com comprimento mínimo seguro.
// Quando false, o sistema opera em modo de token estático (backward compat).
func (m *JWTManager) IsConfigured() bool {
	return len(m.secret) >= 32
}

// Generate cria um JWT assinado com HS256 para o usuário informado.
func (m *JWTManager) Generate(email, name string, isSRE bool) (string, error) {
	if !m.IsConfigured() {
		return "", errors.New("JWT não configurado: K8S_HPA_JWT_SECRET ausente ou menor que 32 bytes")
	}

	now := time.Now()
	claims := JWTClaims{
		Email: email,
		Name:  name,
		IsSRE: isSRE,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("erro ao assinar JWT: %w", err)
	}
	return signed, nil
}

// Validate valida a assinatura e expiração do token. Retorna os claims se válido.
func (m *JWTManager) Validate(tokenStr string) (*JWTClaims, error) {
	if !m.IsConfigured() {
		return nil, errors.New("JWT não configurado")
	}

	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("algoritmo de assinatura inesperado: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("token expirado")
		}
		return nil, fmt.Errorf("token inválido: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("claims inválidos")
	}
	return claims, nil
}

// TTL retorna o tempo de vida configurado para os tokens.
func (m *JWTManager) TTL() time.Duration {
	return m.ttl
}
