package analyzers

import (
	"context"
	"fmt"
)

// MongoDBAnalyzer testa conectividade MongoDB
type MongoDBAnalyzer struct{}

// Check executa ping no MongoDB e retorna latência
func (a *MongoDBAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	// TODO: Implementar com go.mongodb.org/mongo-driver/mongo
	// Por enquanto, retorna erro informativo
	return false, 0, fmt.Errorf("MongoDB analyzer não implementado: requer dependência 'go.mongodb.org/mongo-driver/mongo'")
}

// GetDetails retorna informações do servidor MongoDB (version, uptime, connections)
func (a *MongoDBAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	// TODO: Implementar serverStatus command
	return nil, fmt.Errorf("MongoDB analyzer não implementado")
}
