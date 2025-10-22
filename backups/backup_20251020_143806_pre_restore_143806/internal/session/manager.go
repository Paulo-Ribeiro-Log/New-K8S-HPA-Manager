package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

// Manager gerencia as sessões de configuração
type Manager struct {
	sessionDir string
	templates  []models.SessionTemplate
}

// SessionFolder representa os tipos de pastas de sessão
type SessionFolder string

const (
	FolderHPAUpscale    SessionFolder = "HPA-Upscale"
	FolderHPADownscale  SessionFolder = "HPA-Downscale"
	FolderNodeUpscale   SessionFolder = "Node-Upscale"
	FolderNodeDownscale SessionFolder = "Node-Downscale"
)

// NewManager cria um novo gerenciador de sessões
func NewManager() (*Manager, error) {
	// Criar diretório de sessões no home do usuário
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "sessions")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Criar subdiretórios para cada tipo de sessão
	folders := []SessionFolder{FolderHPAUpscale, FolderHPADownscale, FolderNodeUpscale, FolderNodeDownscale}
	for _, folder := range folders {
		folderPath := filepath.Join(sessionDir, string(folder))
		if err := os.MkdirAll(folderPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create session folder %s: %w", folder, err)
		}
	}

	manager := &Manager{
		sessionDir: sessionDir,
		templates:  getDefaultTemplates(),
	}

	return manager, nil
}

// SaveSession salva uma sessão com nome personalizado
func (m *Manager) SaveSession(session *models.Session) error {
	return m.SaveSessionToFolder(session, "")
}

// SaveSessionToFolder salva uma sessão em uma subpasta específica
func (m *Manager) SaveSessionToFolder(session *models.Session, folder SessionFolder) error {
	if session.Name == "" {
		return fmt.Errorf("session name cannot be empty")
	}

	// Validar nome da sessão
	if err := m.validateSessionName(session.Name); err != nil {
		return err
	}

	// Garantir que metadados estão preenchidos
	if session.Metadata == nil {
		session.Metadata = m.generateMetadata(session.Changes)
	}

	// Definir timestamps
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}

	// Definir usuário se não especificado
	if session.CreatedBy == "" {
		if user := os.Getenv("USER"); user != "" {
			session.CreatedBy = user
		} else {
			session.CreatedBy = "unknown"
		}
	}

	// Serializar para JSON
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Salvar no arquivo
	filename := fmt.Sprintf("%s.json", session.Name)
	var filePath string
	if folder != "" {
		folderPath := filepath.Join(m.sessionDir, string(folder))
		filePath = filepath.Join(folderPath, filename)
	} else {
		filePath = filepath.Join(m.sessionDir, filename)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to save session file: %w", err)
	}

	return nil
}

// ListSessionFolders retorna todas as pastas de sessão disponíveis
func (m *Manager) ListSessionFolders() []SessionFolder {
	return []SessionFolder{FolderHPAUpscale, FolderHPADownscale, FolderNodeUpscale, FolderNodeDownscale}
}

// ListSessionsInFolder retorna as sessões de uma pasta específica
func (m *Manager) ListSessionsInFolder(folder SessionFolder) ([]models.Session, error) {
	folderPath := filepath.Join(m.sessionDir, string(folder))

	// Verificar se a pasta existe
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		return []models.Session{}, nil // Retornar lista vazia se pasta não existir
	}

	files, err := os.ReadDir(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session folder %s: %w", folder, err)
	}

	var sessions []models.Session

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		sessionName := strings.TrimSuffix(file.Name(), ".json")
		session, err := m.LoadSessionFromFolder(sessionName, folder)
		if err != nil {
			// Log error mas continue listando outras sessões
			continue
		}

		sessions = append(sessions, *session)
	}

	// Ordenar por data de criação (mais recente primeiro)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions, nil
}

// LoadSession carrega uma sessão pelo nome (compatibilidade retroativa - busca na raiz)
func (m *Manager) LoadSession(name string) (*models.Session, error) {
	return m.LoadSessionFromFolder(name, "")
}

// LoadSessionFromFolder carrega uma sessão de uma pasta específica
func (m *Manager) LoadSessionFromFolder(name string, folder SessionFolder) (*models.Session, error) {
	filename := fmt.Sprintf("%s.json", name)
	var filePath string
	if folder != "" {
		folderPath := filepath.Join(m.sessionDir, string(folder))
		filePath = filepath.Join(folderPath, filename)
	} else {
		filePath = filepath.Join(m.sessionDir, filename)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

// ListSessions lista todas as sessões salvas
func (m *Manager) ListSessions() ([]models.Session, error) {
	files, err := os.ReadDir(m.sessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var sessions []models.Session

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		sessionName := strings.TrimSuffix(file.Name(), ".json")
		session, err := m.LoadSession(sessionName)
		if err != nil {
			// Log error mas continue listando outras sessões
			continue
		}

		sessions = append(sessions, *session)
	}

	// Ordenar por data de criação (mais recente primeiro)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions, nil
}

// DeleteSession remove uma sessão (compatibilidade retroativa)
func (m *Manager) DeleteSession(name string) error {
	return m.DeleteSessionFromFolder(name, "")
}

// DeleteSessionFromFolder remove uma sessão de uma pasta específica
func (m *Manager) DeleteSessionFromFolder(name string, folder SessionFolder) error {
	filename := fmt.Sprintf("%s.json", name)
	var filePath string
	if folder != "" {
		folderPath := filepath.Join(m.sessionDir, string(folder))
		filePath = filepath.Join(folderPath, filename)
	} else {
		filePath = filepath.Join(m.sessionDir, filename)
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	return nil
}

// RenameSession renomeia uma sessão (compatibilidade retroativa)
func (m *Manager) RenameSession(oldName, newName string) error {
	return m.RenameSessionInFolder(oldName, newName, "")
}

// RenameSessionInFolder renomeia uma sessão em uma pasta específica
func (m *Manager) RenameSessionInFolder(oldName, newName string, folder SessionFolder) error {
	if err := m.validateSessionName(newName); err != nil {
		return err
	}

	// Construir caminho do arquivo antigo
	var oldPath string
	if folder != "" {
		folderPath := filepath.Join(m.sessionDir, string(folder))
		oldPath = filepath.Join(folderPath, fmt.Sprintf("%s.json", oldName))
	} else {
		oldPath = filepath.Join(m.sessionDir, fmt.Sprintf("%s.json", oldName))
	}

	// Carregar sessão
	session, err := m.LoadSessionFromFolder(oldName, folder)
	if err != nil {
		return err
	}

	// Atualizar nome
	session.Name = newName

	// Salvar com novo nome na pasta correta
	var saveErr error
	if folder != "" {
		saveErr = m.SaveSessionToFolder(session, folder)
	} else {
		saveErr = m.SaveSession(session)
	}

	if saveErr != nil {
		return saveErr
	}

	// Remover arquivo antigo
	return os.Remove(oldPath)
}

// GenerateSessionName gera um nome de sessão baseado no template
func (m *Manager) GenerateSessionName(baseName, templatePattern string, changes []models.HPAChange) string {
	name := templatePattern

	// Substituir variáveis
	name = strings.ReplaceAll(name, "{action}", baseName)
	name = strings.ReplaceAll(name, "{timestamp}", time.Now().Format("02-01-06_15:04:05"))
	name = strings.ReplaceAll(name, "{date}", time.Now().Format("02-01-06"))
	name = strings.ReplaceAll(name, "{time}", time.Now().Format("15:04:05"))

	if user := os.Getenv("USER"); user != "" {
		name = strings.ReplaceAll(name, "{user}", user)
	} else {
		name = strings.ReplaceAll(name, "{user}", "unknown")
	}

	// Extrair informações dos changes
	if len(changes) > 0 {
		clustersMap := make(map[string]bool)
		for _, change := range changes {
			clustersMap[change.Cluster] = true
		}

		var clusters []string
		for cluster := range clustersMap {
			clusters = append(clusters, cluster)
		}

		if len(clusters) == 1 {
			name = strings.ReplaceAll(name, "{cluster}", clusters[0])
			// Extrair ambiente do nome do cluster
			if env := extractEnvironment(clusters[0]); env != "" {
				name = strings.ReplaceAll(name, "{env}", env)
			}
		} else {
			name = strings.ReplaceAll(name, "{cluster}", "multi-cluster")
			name = strings.ReplaceAll(name, "{env}", "multi-env")
		}

		name = strings.ReplaceAll(name, "{hpa_count}", fmt.Sprintf("%d", len(changes)))
	}

	// Limpar variáveis não substituídas
	name = strings.ReplaceAll(name, "{cluster}", "unknown")
	name = strings.ReplaceAll(name, "{env}", "unknown")
	name = strings.ReplaceAll(name, "{hpa_count}", "0")

	return name
}

// GetTemplates retorna os templates disponíveis
func (m *Manager) GetTemplates() []models.SessionTemplate {
	return m.templates
}

// SessionExists verifica se uma sessão existe
func (m *Manager) SessionExists(name string) bool {
	filename := fmt.Sprintf("%s.json", name)
	filepath := filepath.Join(m.sessionDir, filename)

	_, err := os.Stat(filepath)
	return !os.IsNotExist(err)
}

// CreateAutoSave cria uma sessão de auto-save
func (m *Manager) CreateAutoSave(changes []models.HPAChange) (*models.Session, error) {
	name := fmt.Sprintf("autosave_%s", time.Now().Format("02-01-06_15:04:05"))

	session := &models.Session{
		Name:        name,
		CreatedAt:   time.Now(),
		Description: "Auto-saved session",
		Changes:     changes,
		Metadata:    m.generateMetadata(changes),
		RollbackData: &models.RollbackData{
			OriginalStateCaptured:   true,
			CanRollback:             true,
			RollbackScriptGenerated: false,
		},
	}

	if err := m.SaveSession(session); err != nil {
		return nil, err
	}

	// Limpar autosaves antigos (manter apenas 5)
	m.cleanupAutoSaves()

	return session, nil
}

// validateSessionName valida o nome da sessão
func (m *Manager) validateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}

	if len(name) > 50 {
		return fmt.Errorf("session name cannot exceed 50 characters")
	}

	// Verificar caracteres permitidos
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return fmt.Errorf("session name can only contain letters, numbers, underscore and dash")
		}
	}

	return nil
}

// generateMetadata gera metadados para uma sessão
func (m *Manager) generateMetadata(changes []models.HPAChange) *models.SessionMetadata {
	clustersMap := make(map[string]bool)
	namespacesMap := make(map[string]bool)

	for _, change := range changes {
		clustersMap[change.Cluster] = true
		namespacesMap[fmt.Sprintf("%s/%s", change.Cluster, change.Namespace)] = true
	}

	var clusters []string
	for cluster := range clustersMap {
		clusters = append(clusters, cluster)
	}

	return &models.SessionMetadata{
		ClustersAffected: clusters,
		NamespacesCount:  len(namespacesMap),
		HPACount:         len(changes),
		TotalChanges:     len(changes),
	}
}

// cleanupAutoSaves remove autosaves antigos, mantendo apenas os 5 mais recentes
func (m *Manager) cleanupAutoSaves() {
	files, err := os.ReadDir(m.sessionDir)
	if err != nil {
		return
	}

	var autoSaves []os.DirEntry
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "autosave_") && strings.HasSuffix(file.Name(), ".json") {
			autoSaves = append(autoSaves, file)
		}
	}

	// Se temos mais de 5 autosaves, remover os mais antigos
	if len(autoSaves) > 5 {
		// Ordenar por data de modificação
		sort.Slice(autoSaves, func(i, j int) bool {
			info1, _ := autoSaves[i].Info()
			info2, _ := autoSaves[j].Info()
			return info1.ModTime().After(info2.ModTime())
		})

		// Remover os mais antigos
		for i := 5; i < len(autoSaves); i++ {
			os.Remove(filepath.Join(m.sessionDir, autoSaves[i].Name()))
		}
	}
}

// extractEnvironment extrai o ambiente do nome do cluster
func extractEnvironment(clusterName string) string {
	lower := strings.ToLower(clusterName)
	if strings.Contains(lower, "prod") {
		return "production"
	}
	if strings.Contains(lower, "dev") {
		return "development"
	}
	if strings.Contains(lower, "staging") || strings.Contains(lower, "stage") {
		return "staging"
	}
	if strings.Contains(lower, "test") {
		return "testing"
	}
	return ""
}

// getDefaultTemplates retorna os templates padrão para nomes de sessão
func getDefaultTemplates() []models.SessionTemplate {
	return []models.SessionTemplate{
		{
			Name:        "Action + Cluster + Timestamp",
			Pattern:     "{action}_{cluster}_{timestamp}",
			Description: "Ex: Up-sizing_aks-teste-prd_19-09-24_14:23:45",
			Variables: map[string]string{
				"action":    "Ação customizada",
				"cluster":   "Nome do cluster principal",
				"timestamp": "dd-mm-yy_hh:mm:ss",
			},
		},
		{
			Name:        "Action + Environment + Date",
			Pattern:     "{action}_{env}_{date}",
			Description: "Ex: Scale-down_production_19-09-24",
			Variables: map[string]string{
				"action": "Ação customizada",
				"env":    "Ambiente (dev/prod/staging)",
				"date":   "dd-mm-yy",
			},
		},
		{
			Name:        "Timestamp + Action + User",
			Pattern:     "{timestamp}_{action}_{user}",
			Description: "Ex: 19-09-24_14:23_Emergency-scale_admin",
			Variables: map[string]string{
				"timestamp": "dd-mm-yy_hh:mm:ss",
				"action":    "Ação customizada",
				"user":      "Usuário atual do sistema",
			},
		},
		{
			Name:        "Quick Save",
			Pattern:     "Quick-save_{timestamp}",
			Description: "Ex: Quick-save_19-09-24_14:25:12",
			Variables: map[string]string{
				"timestamp": "dd-mm-yy_hh:mm:ss",
			},
		},
	}
}
