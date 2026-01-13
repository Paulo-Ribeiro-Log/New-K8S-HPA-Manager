package analyzers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// KafkaAnalyzer testa conectividade Kafka
type KafkaAnalyzer struct{}

// Check testa conexão com broker Kafka
func (a *KafkaAnalyzer) Check(ctx context.Context, broker string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Parse broker address (pode ser "host:port" ou lista separada por vírgula)
	brokers := strings.Split(broker, ",")
	for i, b := range brokers {
		brokers[i] = strings.TrimSpace(b)
	}

	// Criar connection com timeout
	dialer := &kafka.Dialer{
		Timeout:   time.Duration(timeout) * time.Second,
		DualStack: true,
	}

	// Conectar ao primeiro broker
	conn, err := dialer.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return false, 0, fmt.Errorf("falha ao conectar ao broker: %w", err)
	}
	defer conn.Close()

	// Obter metadata do broker (valida conectividade)
	_, err = conn.Brokers()
	if err != nil {
		return false, 0, fmt.Errorf("falha ao obter metadata: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// GetDetails retorna informações do cluster Kafka
func (a *KafkaAnalyzer) GetDetails(ctx context.Context, broker string) (map[string]interface{}, error) {
	brokers := strings.Split(broker, ",")
	for i, b := range brokers {
		brokers[i] = strings.TrimSpace(b)
	}

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	conn, err := dialer.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	details := make(map[string]interface{})

	// Obter lista de brokers
	brokerList, err := conn.Brokers()
	if err == nil {
		details["broker_count"] = len(brokerList)
	}

	// Obter lista de partitions (topics)
	partitions, err := conn.ReadPartitions()
	if err == nil {
		topicSet := make(map[string]bool)
		for _, p := range partitions {
			topicSet[p.Topic] = true
		}
		details["topic_count"] = len(topicSet)
		details["partition_count"] = len(partitions)
	}

	// Obter controller ID
	controller, err := conn.Controller()
	if err == nil {
		details["controller_id"] = controller.ID
	}

	return details, nil
}
