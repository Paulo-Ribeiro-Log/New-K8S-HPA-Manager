package healthcheck

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

// ConfigChecker valida ConfigMaps e Secrets
type ConfigChecker struct{}

// NewConfigChecker cria um novo config checker
func NewConfigChecker() *ConfigChecker {
	return &ConfigChecker{}
}

// CheckAll valida todos os ConfigMaps e Secrets necessários
func (c *ConfigChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string) []ConfigHealth {
	// TODO: Implementar no Dia 3
	// - Verificar se ConfigMaps/Secrets existem
	// - Validar chaves obrigatórias
	// - Validar formato de valores (connection strings, etc)
	// - Retornar lista de ConfigHealth
	return []ConfigHealth{}
}
