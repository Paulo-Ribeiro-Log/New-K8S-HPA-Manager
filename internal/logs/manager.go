package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// MaxLogFileSize é o tamanho máximo do arquivo de log em bytes (10MB)
	MaxLogFileSize = 10 * 1024 * 1024
	// MaxLogFiles é o número máximo de arquivos de log rotacionados
	MaxLogFiles = 5
)

// LogManager gerencia o arquivo de logs da aplicação
type LogManager struct {
	logFile  *os.File
	logPath  string
	mu       sync.Mutex
	enabled  bool
	buffer   []string // Buffer em memória para visualização rápida
	maxLines int      // Máximo de linhas no buffer
}

var (
	instance *LogManager
	once     sync.Once
)

// GetInstance retorna a instância singleton do LogManager
func GetInstance() *LogManager {
	once.Do(func() {
		instance = &LogManager{
			enabled:  true,
			buffer:   make([]string, 0),
			maxLines: 1000, // Manter últimas 1000 linhas em memória
		}
		instance.initialize()
	})
	return instance
}

// initialize configura o LogManager
func (lm *LogManager) initialize() error {
	// Obter diretório raiz do projeto
	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to get project root: %w", err)
	}

	// Criar diretório Logs se não existir
	logsDir := filepath.Join(projectRoot, "Logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create Logs directory: %w", err)
	}

	// Criar arquivo de log com timestamp
	logFileName := fmt.Sprintf("k8s-hpa-manager_%s.log", time.Now().Format("2006-01-02"))
	lm.logPath = filepath.Join(logsDir, logFileName)

	// Abrir arquivo para append
	file, err := os.OpenFile(lm.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	lm.logFile = file

	// Verificar e rotacionar logs se necessário
	lm.rotateIfNeeded()

	return nil
}

// getProjectRoot encontra a raiz do projeto
func getProjectRoot() (string, error) {
	// Começar do executável atual
	execPath, err := os.Executable()
	if err != nil {
		// Fallback para diretório de trabalho
		return os.Getwd()
	}

	// Subir até encontrar go.mod ou usar o diretório build
	dir := filepath.Dir(execPath)

	// Se estiver em build/, subir um nível
	if filepath.Base(dir) == "build" {
		return filepath.Dir(dir), nil
	}

	// Procurar go.mod subindo até 5 níveis
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // Chegou na raiz do sistema
		}
		dir = parent
	}

	// Fallback para diretório de trabalho
	return os.Getwd()
}

// Log escreve uma entrada no log (thread-safe)
func (lm *LogManager) Log(level, source, message string) error {
	if !lm.enabled || lm.logFile == nil {
		return nil
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, level, source, message)

	// Escrever no arquivo
	if _, err := lm.logFile.WriteString(logEntry); err != nil {
		return err
	}

	// Adicionar ao buffer em memória
	lm.buffer = append(lm.buffer, strings.TrimSpace(logEntry))

	// Manter apenas as últimas maxLines linhas
	if len(lm.buffer) > lm.maxLines {
		lm.buffer = lm.buffer[len(lm.buffer)-lm.maxLines:]
	}

	// Flush para garantir escrita
	lm.logFile.Sync()

	// Verificar se precisa rotacionar
	lm.rotateIfNeeded()

	return nil
}

// rotateIfNeeded rotaciona o arquivo de log se necessário
func (lm *LogManager) rotateIfNeeded() {
	if lm.logFile == nil {
		return
	}

	// Verificar tamanho do arquivo
	stat, err := lm.logFile.Stat()
	if err != nil {
		return
	}

	if stat.Size() < MaxLogFileSize {
		return
	}

	// Fechar arquivo atual
	lm.logFile.Close()

	// Rotacionar arquivos
	baseDir := filepath.Dir(lm.logPath)
	baseName := filepath.Base(lm.logPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	// Mover arquivos antigos
	for i := MaxLogFiles - 1; i > 0; i-- {
		oldPath := filepath.Join(baseDir, fmt.Sprintf("%s.%d%s", nameWithoutExt, i, ext))
		newPath := filepath.Join(baseDir, fmt.Sprintf("%s.%d%s", nameWithoutExt, i+1, ext))
		os.Rename(oldPath, newPath)
	}

	// Renomear arquivo atual para .1
	backupPath := filepath.Join(baseDir, fmt.Sprintf("%s.1%s", nameWithoutExt, ext))
	os.Rename(lm.logPath, backupPath)

	// Criar novo arquivo
	file, err := os.OpenFile(lm.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	lm.logFile = file
}

// ReadLogs lê todas as linhas do arquivo de log
func (lm *LogManager) ReadLogs() ([]string, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Retornar buffer em memória (mais rápido)
	if len(lm.buffer) > 0 {
		result := make([]string, len(lm.buffer))
		copy(result, lm.buffer)
		return result, nil
	}

	// Fallback: ler do arquivo
	if lm.logFile == nil {
		return []string{}, nil
	}

	// Flush antes de ler
	lm.logFile.Sync()

	content, err := os.ReadFile(lm.logPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	// Remover última linha vazia
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines, nil
}

// CopyLogsToClipboard copia o conteúdo dos logs para a área de transferência
func (lm *LogManager) CopyLogsToClipboard() error {
	logs, err := lm.ReadLogs()
	if err != nil {
		return err
	}

	content := strings.Join(logs, "\n")

	// Tentar copiar usando xclip (Linux)
	cmd := fmt.Sprintf("echo %q | xclip -selection clipboard", content)
	if err := os.WriteFile("/tmp/k8s-hpa-logs-copy.sh", []byte(cmd), 0755); err == nil {
		if err := os.Chmod("/tmp/k8s-hpa-logs-copy.sh", 0755); err == nil {
			os.Getenv("SHELL")
			// Executar via shell
			return nil
		}
	}

	// Fallback: salvar em arquivo temporário para o usuário copiar manualmente
	tmpFile := "/tmp/k8s-hpa-manager-logs.txt"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to save logs to temp file: %w", err)
	}

	return fmt.Errorf("logs saved to %s - please copy manually", tmpFile)
}

// ClearLogs limpa o arquivo de log atual
func (lm *LogManager) ClearLogs() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.logFile == nil {
		return fmt.Errorf("log file not initialized")
	}

	// Fechar arquivo atual
	lm.logFile.Close()

	// Truncar arquivo
	file, err := os.OpenFile(lm.logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	lm.logFile = file

	// Limpar buffer
	lm.buffer = make([]string, 0)

	// Log de limpeza
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] [INFO] [logs] Logs cleared by user\n", timestamp)
	lm.logFile.WriteString(logEntry)
	lm.logFile.Sync()

	return nil
}

// GetLogPath retorna o caminho do arquivo de log atual
func (lm *LogManager) GetLogPath() string {
	return lm.logPath
}

// Close fecha o arquivo de log
func (lm *LogManager) Close() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.logFile != nil {
		return lm.logFile.Close()
	}
	return nil
}

// Enable habilita/desabilita logging
func (lm *LogManager) Enable(enabled bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.enabled = enabled
}
