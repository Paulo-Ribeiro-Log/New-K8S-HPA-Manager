package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// VPNStatusResponse representa o status da conexão VPN
type VPNStatusResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// CheckVPNConnection verifica conectividade VPN usando kubectl cluster-info
// Similar à função validateVPNConnection da TUI (internal/tui/message.go:754-785)
func CheckVPNConnection(c *gin.Context) {
	err := testKubernetesConnectivity()

	response := VPNStatusResponse{
		Timestamp: time.Now().Unix(),
	}

	if err != nil {
		response.Connected = false
		response.Message = fmt.Sprintf("VPN desconectada: %s", err.Error())
		c.JSON(http.StatusServiceUnavailable, response)
		return
	}

	response.Connected = true
	response.Message = "VPN conectada - Kubernetes acessível"
	c.JSON(http.StatusOK, response)
}

// testKubernetesConnectivity testa conectividade real com Kubernetes
// Usa kubectl cluster-info com timeout de 6 segundos
// Retorna nil se conectado, erro se VPN desconectada
func testKubernetesConnectivity() error {
	// Criar contexto com timeout de 6 segundos
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	// kubectl cluster-info - comando leve para testar conectividade
	testCmd := exec.CommandContext(ctx, "kubectl", "cluster-info")

	// Canal para resultado
	done := make(chan error, 1)

	go func() {
		output, err := testCmd.CombinedOutput()
		outputStr := string(output)

		// Se kubectl conseguiu responder (mesmo que seja erro de auth), VPN está OK
		// Procurar por "Kubernetes control plane" ou "running at" na saída
		if err == nil || strings.Contains(outputStr, "running at") || strings.Contains(outputStr, "Kubernetes") {
			done <- nil
		} else {
			done <- fmt.Errorf("kubectl falhou: %w (output: %s)", err, outputStr)
		}
	}()

	// Aguardar resultado ou timeout
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Timeout - VPN provavelmente desconectada
		if testCmd.Process != nil {
			testCmd.Process.Kill()
		}
		return fmt.Errorf("timeout ao acessar Kubernetes - VPN pode estar desconectada")
	}
}
