package analyzers

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
)

// EventHubAnalyzer testa conectividade Azure EventHub
type EventHubAnalyzer struct{}

// Check testa conexão com EventHub
func (a *EventHubAnalyzer) Check(ctx context.Context, connStr string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Criar cliente com timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Criar producer client (valida connection string)
	client, err := azeventhubs.NewProducerClientFromConnectionString(connStr, "", nil)
	if err != nil {
		return false, 0, fmt.Errorf("connection string inválida: %w", err)
	}
	defer client.Close(timeoutCtx)

	// Obter propriedades do EventHub (valida conectividade)
	_, err = client.GetEventHubProperties(timeoutCtx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("falha ao obter propriedades: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// GetDetails retorna informações do EventHub
func (a *EventHubAnalyzer) GetDetails(ctx context.Context, connStr string) (map[string]interface{}, error) {
	client, err := azeventhubs.NewProducerClientFromConnectionString(connStr, "", nil)
	if err != nil {
		return nil, err
	}
	defer client.Close(ctx)

	// Obter propriedades do hub
	props, err := client.GetEventHubProperties(ctx, nil)
	if err != nil {
		return nil, err
	}

	details := make(map[string]interface{})
	details["name"] = props.Name
	details["partition_count"] = len(props.PartitionIDs)
	details["created_at"] = props.CreatedOn.Format(time.RFC3339)

	// Obter propriedades de cada partition
	if len(props.PartitionIDs) > 0 {
		partitionProps, err := client.GetPartitionProperties(ctx, props.PartitionIDs[0], nil)
		if err == nil {
			details["sample_partition_id"] = partitionProps.PartitionID
			details["sample_partition_beginning_sequence"] = partitionProps.BeginningSequenceNumber
			details["sample_partition_last_sequence"] = partitionProps.LastEnqueuedSequenceNumber
		}
	}

	return details, nil
}
