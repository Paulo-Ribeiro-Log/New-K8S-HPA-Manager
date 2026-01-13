package analyzers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisAnalyzer testa conectividade Redis
type RedisAnalyzer struct{}

// Check executa PING command no Redis
func (a *RedisAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Parse connection string
	opt, err := redis.ParseURL(connStr)
	if err != nil {
		return false, 0, fmt.Errorf("connection string inválida: %w", err)
	}

	// Configurar timeouts
	opt.DialTimeout = time.Duration(timeout) * time.Second
	opt.ReadTimeout = time.Duration(timeout) * time.Second
	opt.WriteTimeout = time.Duration(timeout) * time.Second

	// Conectar
	client := redis.NewClient(opt)
	defer client.Close()

	// PING command
	pong, err := client.Ping(ctx).Result()
	if err != nil {
		return false, 0, fmt.Errorf("PING falhou: %w", err)
	}

	if pong != "PONG" {
		return false, 0, fmt.Errorf("resposta inesperada do PING: %s", pong)
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// GetDetails retorna informações do servidor Redis (INFO command)
func (a *RedisAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	opt, err := redis.ParseURL(connStr)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)
	defer client.Close()

	// INFO command
	info, err := client.Info(ctx).Result()
	if err != nil {
		return nil, err
	}

	// Parse INFO response (key:value pairs)
	details := make(map[string]interface{})
	lines := strings.Split(info, "\r\n")

	for _, line := range lines {
		if strings.Contains(line, ":") && !strings.HasPrefix(line, "#") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := parts[0]
				value := parts[1]

				// Extrair apenas campos relevantes
				switch key {
				case "redis_version":
					details["version"] = value
				case "uptime_in_seconds":
					details["uptime_seconds"] = value
				case "connected_clients":
					details["connected_clients"] = value
				case "used_memory_human":
					details["used_memory"] = value
				case "maxmemory_human":
					details["max_memory"] = value
				}
			}
		}
	}

	return details, nil
}
