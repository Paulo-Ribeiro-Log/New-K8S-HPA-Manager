package analyzers

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PostgresAnalyzer testa conectividade PostgreSQL
type PostgresAnalyzer struct{}

// Check executa SELECT 1 para validar conexão PostgreSQL
func (a *PostgresAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Parse connection string e configurar timeout
	config, err := pgx.ParseConfig(connStr)
	if err != nil {
		return false, 0, fmt.Errorf("connection string inválida: %w", err)
	}

	config.ConnectTimeout = time.Duration(timeout) * time.Second

	// Conectar
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return false, 0, fmt.Errorf("falha ao conectar: %w", err)
	}
	defer conn.Close(ctx)

	// SELECT 1 para validar conexão
	var result int
	err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		return false, 0, fmt.Errorf("query falhou: %w", err)
	}

	if result != 1 {
		return false, 0, fmt.Errorf("resposta inesperada do SELECT: %d", result)
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// GetDetails retorna versão e tamanho do banco PostgreSQL
func (a *PostgresAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	details := make(map[string]interface{})

	// Obter versão
	var version string
	err = conn.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err == nil {
		details["version"] = version
	}

	// Obter tamanho do banco atual
	var dbSize int64
	err = conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&dbSize)
	if err == nil {
		details["database_size_bytes"] = dbSize
	}

	// Obter número de conexões ativas
	var activeConnections int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state = 'active'").Scan(&activeConnections)
	if err == nil {
		details["active_connections"] = activeConnections
	}

	return details, nil
}
