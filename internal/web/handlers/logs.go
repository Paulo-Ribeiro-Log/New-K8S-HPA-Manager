package handlers

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LogsHandler gerencia os logs da aplicação
type LogsHandler struct {
	logBuffer *LogBuffer
}

// LogBuffer mantém logs em memória
type LogBuffer struct {
	mu      sync.RWMutex
	logs    []string
	maxSize int
}

// NewLogBuffer cria um novo buffer de logs
func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		logs:    make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adiciona uma entrada de log
func (lb *LogBuffer) Add(entry string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.logs = append(lb.logs, entry)

	// Manter apenas os últimos maxSize logs
	if len(lb.logs) > lb.maxSize {
		lb.logs = lb.logs[len(lb.logs)-lb.maxSize:]
	}
}

// GetAll retorna todos os logs
func (lb *LogBuffer) GetAll() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Criar cópia para evitar race conditions
	logs := make([]string, len(lb.logs))
	copy(logs, lb.logs)
	return logs
}

// Clear limpa todos os logs
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.logs = make([]string, 0, lb.maxSize)
}

// NewLogsHandler cria um novo handler de logs
func NewLogsHandler(logBuffer *LogBuffer) *LogsHandler {
	return &LogsHandler{
		logBuffer: logBuffer,
	}
}

// GetLogs retorna os logs da aplicação
func (h *LogsHandler) GetLogs(c *gin.Context) {
	// Coletar logs de várias fontes
	var allLogs bytes.Buffer

	// 1. Logs do buffer em memória (logs da aplicação web)
	bufferLogs := h.logBuffer.GetAll()
	if len(bufferLogs) > 0 {
		allLogs.WriteString("=== Application Logs (In-Memory) ===\n")
		for _, log := range bufferLogs {
			allLogs.WriteString(log)
			allLogs.WriteString("\n")
		}
		allLogs.WriteString("\n")
	}

	// 2. Logs do servidor web (se houver arquivo de log)
	webLogPath := "/tmp/k8s-hpa-manager-web-*.log"
	webLogs, err := readLatestLogFile(webLogPath)
	if err == nil && webLogs != "" {
		allLogs.WriteString("=== Web Server Logs ===\n")
		allLogs.WriteString(webLogs)
		allLogs.WriteString("\n")
	}

	// 3. Logs do sistema (journalctl - se disponível)
	// Opcional: descomentar se quiser incluir logs do systemd
	/*
		systemLogs := getSystemLogs()
		if systemLogs != "" {
			allLogs.WriteString("=== System Logs (journalctl) ===\n")
			allLogs.WriteString(systemLogs)
			allLogs.WriteString("\n")
		}
	*/

	c.JSON(200, gin.H{
		"logs":      allLogs.String(),
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "k8s-hpa-manager",
	})
}

// ClearLogs limpa os logs da aplicação
func (h *LogsHandler) ClearLogs(c *gin.Context) {
	h.logBuffer.Clear()

	c.JSON(200, gin.H{
		"message": "Logs cleared successfully",
	})
}

// readLatestLogFile lê o arquivo de log mais recente que corresponde ao pattern
func readLatestLogFile(pattern string) (string, error) {
	// Encontrar arquivos que correspondem ao pattern
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no log files found")
	}

	// Pegar o arquivo mais recente
	var latestFile string
	var latestTime time.Time

	for _, file := range matches {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		if latestFile == "" || info.ModTime().After(latestTime) {
			latestFile = file
			latestTime = info.ModTime()
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no valid log files found")
	}

	// Lê só a cauda do arquivo (últimos 500KB) via Seek, em vez de carregar o arquivo
	// inteiro em memória com os.ReadFile — o arquivo de log do processo em background
	// (stdout/stderr de runInBackground, cmd/web.go) cresce sem rotação ao longo da
	// sessão (chegou a 25MB numa sessão real de um dia), e esse endpoint é chamado a
	// cada 3s pelo auto-refresh do LogViewer.tsx: um os.ReadFile aqui fazia cada
	// chamada ficar mais lenta conforme o arquivo crescia — inclusive um loop que se
	// realimenta, já que cada chamada a este endpoint é ela mesma registrada no
	// arquivo pelo gin.Logger() (mitigado à parte via SkipPaths em server.go).
	const maxSize = 500 * 1024 // 500 KB
	f, err := os.Open(latestFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	var offset int64
	if info.Size() > maxSize {
		offset = info.Size() - maxSize
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}

	content, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// getSystemLogs obtém logs do sistema via journalctl (opcional)
/*
func getSystemLogs() string {
	cmd := exec.Command("journalctl", "-u", "k8s-hpa-manager", "-n", "100", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}
*/
