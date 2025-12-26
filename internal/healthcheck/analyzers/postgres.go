package analyzers

import (
	"context"
	"fmt"
)

// PostgresAnalyzer testa conectividade PostgreSQL
type PostgresAnalyzer struct{}

// Check executa SELECT 1 para validar conexão PostgreSQL
func (a *PostgresAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	// TODO: Implementar com github.com/jackc/pgx/v5
	// Por enquanto, retorna erro informativo
	return false, 0, fmt.Errorf("PostgreSQL analyzer não implementado: requer dependência 'github.com/jackc/pgx/v5'")
}

// GetDetails retorna versão e tamanho do banco PostgreSQL
func (a *PostgresAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	// TODO: Implementar SELECT version() e pg_database_size()
	return nil, fmt.Errorf("PostgreSQL analyzer não implementado")
}
