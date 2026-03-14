package ai

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
)

// FallbackProvider tenta o provider primário e, em caso de 403 (sem permissão IAM),
// faz fallback transparente para o provider secundário (credenciais do servidor).
// Útil quando o token OAuth do usuário não tem a role aiplatform.endpoints.predict.
type FallbackProvider struct {
	primary  Provider
	fallback Provider
}

// NewFallbackProvider cria um FallbackProvider.
func NewFallbackProvider(primary, fallback Provider) *FallbackProvider {
	return &FallbackProvider{primary: primary, fallback: fallback}
}

// Analyze tenta o provider primário; se 403/PERMISSION_DENIED, usa o fallback.
func (f *FallbackProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	result, err := f.primary.Analyze(ctx, prompt)
	if err == nil {
		return result, nil
	}

	errStr := err.Error()
	if strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "PERMISSION_DENIED") ||
		strings.Contains(errStr, "permissão negada") {
		log.Warn().
			Str("primary_provider", f.primary.GetName()).
			Str("fallback_provider", f.fallback.GetName()).
			Msg("Vertex AI: conta sem role aiplatform.endpoints.predict, usando provider do servidor como fallback")
		return f.fallback.Analyze(ctx, prompt)
	}

	return "", err
}

// GetModel retorna o modelo do provider primário.
func (f *FallbackProvider) GetModel() string {
	return f.primary.GetModel()
}

// IsAvailable retorna true se qualquer um dos providers estiver disponível.
func (f *FallbackProvider) IsAvailable(ctx context.Context) bool {
	return f.primary.IsAvailable(ctx) || f.fallback.IsAvailable(ctx)
}

// GetName retorna o nome do provider primário.
func (f *FallbackProvider) GetName() string {
	return f.primary.GetName()
}
