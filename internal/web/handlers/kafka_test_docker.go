package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// kafkaTestDockerLabel identifica containers criados pelo modo "local" do Teste de Kafka — mesmo
// mecanismo do Teste de Banco de Dados (execLocalDocker/reapOrphanedContainersByLabel, ver
// db_test_docker.go), label separado só pra não misturar containers órfãos das duas ferramentas
// no `docker ps --filter label=...`.
const kafkaTestDockerLabel = "app=k8s-hpa-manager-kafka-test"

// DockerStatus — GET /api/v1/kafka-test/docker-status. Reaproveita checkDockerStatus/
// DBDockerStatusResult (db_test_docker.go) — a checagem (binário no PATH + daemon respondendo) não
// tem nada de específico de banco de dados, é sobre o Docker do host, então serve igual pro modo
// "local" do Teste de Kafka. Sem RequireSREGroup(): leitura informacional sobre o próprio
// servidor, mesmo padrão do endpoint equivalente do Teste de Banco de Dados.
func (h *KafkaTestHandler) DockerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, checkDockerStatus(c.Request.Context()))
}

// startKafkaTestContainerReaper roda em background pela vida inteira do processo — chamado uma
// vez em NewKafkaTestHandler, mesmo padrão de startDBTestContainerReaper.
func startKafkaTestContainerReaper() {
	ticker := time.NewTicker(dbTestContainerReapInterval)
	defer ticker.Stop()
	for range ticker.C {
		reapOrphanedContainersByLabel(kafkaTestDockerLabel, "KafkaTest")
	}
}
