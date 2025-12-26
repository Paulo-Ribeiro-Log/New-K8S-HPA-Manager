package analyzers

import (
	"context"
	"fmt"
)

// EventHubAnalyzer testa conectividade Azure EventHub
type EventHubAnalyzer struct{}

// Check testa conexão com EventHub
func (a *EventHubAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	// TODO: Implementar com github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs
	// Por enquanto, retorna erro informativo
	return false, 0, fmt.Errorf("EventHub analyzer não implementado: requer dependência 'github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs'")
}

// GetDetails retorna informações do EventHub
func (a *EventHubAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	// TODO: Implementar hub properties retrieval
	return nil, fmt.Errorf("EventHub analyzer não implementado")
}
