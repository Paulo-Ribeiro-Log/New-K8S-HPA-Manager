package analyzers

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDBAnalyzer testa conectividade MongoDB
type MongoDBAnalyzer struct{}

// Check executa ping no MongoDB e retorna latência
func (a *MongoDBAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Configurar timeout
	clientOptions := options.Client().
		ApplyURI(connStr).
		SetServerSelectionTimeout(time.Duration(timeout) * time.Second).
		SetConnectTimeout(time.Duration(timeout) * time.Second)

	// Conectar
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return false, 0, fmt.Errorf("falha ao conectar: %w", err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Disconnect(disconnectCtx)
	}()

	// Ping para validar conectividade
	if err := client.Ping(ctx, nil); err != nil {
		return false, 0, fmt.Errorf("ping falhou: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// GetDetails retorna informações do servidor MongoDB (version, uptime, connections)
func (a *MongoDBAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	clientOptions := options.Client().
		ApplyURI(connStr).
		SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Disconnect(disconnectCtx)
	}()

	// Obter informações do servidor via serverStatus
	var result bson.M
	err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&result)
	if err != nil {
		return nil, err
	}

	details := make(map[string]interface{})

	// Extrair campos relevantes
	if version, ok := result["version"].(string); ok {
		details["version"] = version
	}
	if uptime, ok := result["uptime"].(float64); ok {
		details["uptime_seconds"] = int64(uptime)
	}
	if connections, ok := result["connections"].(bson.M); ok {
		if current, ok := connections["current"].(int32); ok {
			details["connections_current"] = current
		}
		if available, ok := connections["available"].(int32); ok {
			details["connections_available"] = available
		}
	}

	return details, nil
}
