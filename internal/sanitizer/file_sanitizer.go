package sanitizer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SanitizeToFile sanitiza dados e salva em arquivo temporário
// Retorna: (caminhoArquivoSanitizado, caminhoArquivoOriginal, error)
func (s *Sanitizer) SanitizeToFile(originalData string, resourceType, resourceName string) (string, string, error) {
	// Criar diretório de debug se não existir
	debugDir := filepath.Join(os.TempDir(), "k8s-hpa-ai-diagnostics")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create debug directory: %w", err)
	}

	// Timestamp para nomes únicos
	timestamp := time.Now().Format("20060102-150405")
	baseFilename := fmt.Sprintf("%s_%s_%s", resourceType, resourceName, timestamp)

	// Caminho do arquivo original (sem sanitização)
	originalFile := filepath.Join(debugDir, fmt.Sprintf("%s_ORIGINAL.txt", baseFilename))
	// Caminho do arquivo sanitizado
	sanitizedFile := filepath.Join(debugDir, fmt.Sprintf("%s_SANITIZED.txt", baseFilename))

	// Salvar arquivo ORIGINAL
	if err := os.WriteFile(originalFile, []byte(originalData), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write original file: %w", err)
	}

	// Sanitizar dados
	sanitized := s.SanitizeText(originalData)

	// Salvar arquivo SANITIZADO
	if err := os.WriteFile(sanitizedFile, []byte(sanitized), 0644); err != nil {
		// Se falhar ao salvar sanitizado, remove original para evitar inconsistência
		os.Remove(originalFile)
		return "", "", fmt.Errorf("failed to write sanitized file: %w", err)
	}

	return sanitizedFile, originalFile, nil
}

// SanitizeLogsToFile sanitiza logs completos e salva em arquivos
// Retorna: (caminhoArquivoSanitizado, caminhoArquivoOriginal, error)
func (s *Sanitizer) SanitizeLogsToFile(logs string, cluster, namespace, podName string) (string, string, error) {
	// Criar diretório de debug se não existir
	debugDir := filepath.Join(os.TempDir(), "k8s-hpa-ai-diagnostics")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create debug directory: %w", err)
	}

	// Timestamp para nomes únicos
	timestamp := time.Now().Format("20060102-150405")
	baseFilename := fmt.Sprintf("logs_%s_%s_%s_%s", cluster, namespace, podName, timestamp)

	// Caminho do arquivo original (sem sanitização)
	originalFile := filepath.Join(debugDir, fmt.Sprintf("%s_ORIGINAL.log", baseFilename))
	// Caminho do arquivo sanitizado
	sanitizedFile := filepath.Join(debugDir, fmt.Sprintf("%s_SANITIZED.log", baseFilename))

	// Salvar arquivo ORIGINAL
	if err := os.WriteFile(originalFile, []byte(logs), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write original log file: %w", err)
	}

	// Sanitizar logs (linha por linha)
	sanitized := s.SanitizeLogs(logs)

	// Salvar arquivo SANITIZADO
	if err := os.WriteFile(sanitizedFile, []byte(sanitized), 0644); err != nil {
		// Se falhar ao salvar sanitizado, remove original para evitar inconsistência
		os.Remove(originalFile)
		return "", "", fmt.Errorf("failed to write sanitized log file: %w", err)
	}

	return sanitizedFile, originalFile, nil
}

// SanitizeFileWithReport sanitiza dados e retorna relatório detalhado
func (s *Sanitizer) SanitizeFileWithReport(originalData string, resourceType, resourceName string) (string, string, *SanitizationResult, error) {
	// Criar diretório de debug
	debugDir := filepath.Join(os.TempDir(), "k8s-hpa-ai-diagnostics")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create debug directory: %w", err)
	}

	// Timestamp para nomes únicos
	timestamp := time.Now().Format("20060102-150405")
	baseFilename := fmt.Sprintf("%s_%s_%s", resourceType, resourceName, timestamp)

	// Caminhos dos arquivos
	originalFile := filepath.Join(debugDir, fmt.Sprintf("%s_ORIGINAL.txt", baseFilename))
	sanitizedFile := filepath.Join(debugDir, fmt.Sprintf("%s_SANITIZED.txt", baseFilename))
	reportFile := filepath.Join(debugDir, fmt.Sprintf("%s_REPORT.txt", baseFilename))

	// Salvar arquivo ORIGINAL
	if err := os.WriteFile(originalFile, []byte(originalData), 0644); err != nil {
		return "", "", nil, fmt.Errorf("failed to write original file: %w", err)
	}

	// Sanitizar com relatório detalhado
	result := s.SanitizeTextWithResult(originalData)

	// Salvar arquivo SANITIZADO
	if err := os.WriteFile(sanitizedFile, []byte(result.Sanitized), 0644); err != nil {
		os.Remove(originalFile)
		return "", "", nil, fmt.Errorf("failed to write sanitized file: %w", err)
	}

	// Gerar relatório
	report := fmt.Sprintf(`RELATÓRIO DE SANITIZAÇÃO
========================
Recurso: %s/%s
Data: %s

ESTATÍSTICAS:
`, resourceType, resourceName, timestamp)

	for itemType, count := range result.MaskedItems {
		report += fmt.Sprintf("  - %s: %d ocorrências\n", itemType, count)
	}

	if len(result.Warnings) > 0 {
		report += "\nAVISOS:\n"
		for _, warning := range result.Warnings {
			report += fmt.Sprintf("  - %s\n", warning)
		}
	}

	report += fmt.Sprintf("\nARQUIVOS GERADOS:\n")
	report += fmt.Sprintf("  - Original:   %s\n", originalFile)
	report += fmt.Sprintf("  - Sanitizado: %s\n", sanitizedFile)
	report += fmt.Sprintf("  - Relatório:  %s\n", reportFile)

	// Salvar relatório
	if err := os.WriteFile(reportFile, []byte(report), 0644); err != nil {
		// Não falha se relatório não puder ser salvo (arquivos principais OK)
		fmt.Printf("Warning: failed to write report file: %v\n", err)
	}

	return sanitizedFile, originalFile, result, nil
}

// GetDebugDirectory retorna o diretório onde arquivos de debug são salvos
func GetDebugDirectory() string {
	return filepath.Join(os.TempDir(), "k8s-hpa-ai-diagnostics")
}

// CleanupOldFiles remove arquivos de debug mais antigos que X horas
func CleanupOldFiles(maxAgeHours int) error {
	debugDir := GetDebugDirectory()

	// Verifica se diretório existe
	if _, err := os.Stat(debugDir); os.IsNotExist(err) {
		return nil // Nada para limpar
	}

	now := time.Now()
	maxAge := time.Duration(maxAgeHours) * time.Hour

	return filepath.Walk(debugDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Pula diretórios
		if info.IsDir() {
			return nil
		}

		// Remove arquivo se muito antigo
		if now.Sub(info.ModTime()) > maxAge {
			fmt.Printf("Cleaning up old debug file: %s\n", path)
			return os.Remove(path)
		}

		return nil
	})
}
