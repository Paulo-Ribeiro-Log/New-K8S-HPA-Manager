package handlers

import (
	"fmt"
	"net/http"

	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/session"

	"github.com/gin-gonic/gin"
)

// SessionsHandler handles session-related endpoints
type SessionsHandler struct {
	sessionManager *session.Manager
}

// NewSessionsHandler creates a new sessions handler
func NewSessionsHandler() *SessionsHandler {
	// Reutilizar o SessionManager existente - EXATAMENTE como no TUI
	sessionManager, err := session.NewManager()
	if err != nil {
		// Em caso de erro, ainda criar o handler mas log o erro
		// TODO: Adicionar logging adequado
		sessionManager = nil
	}

	return &SessionsHandler{
		sessionManager: sessionManager,
	}
}

// SessionListResponse represents a list of sessions
type SessionListResponse struct {
	Sessions []SessionSummary `json:"sessions"`
	Count    int              `json:"count"`
}

// SessionSummary represents a session summary for lists
type SessionSummary struct {
	Name         string                  `json:"name"`
	CreatedAt    string                  `json:"created_at"`
	CreatedBy    string                  `json:"created_by"`
	Description  string                  `json:"description,omitempty"`
	TemplateUsed string                  `json:"template_used"`
	Metadata     *models.SessionMetadata `json:"metadata"`
	Folder       string                  `json:"folder"`
}

// SessionFoldersResponse represents available session folders
type SessionFoldersResponse struct {
	Folders []SessionFolderInfo `json:"folders"`
}

// SessionFolderInfo represents folder information
type SessionFolderInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int    `json:"count"`
}

// SessionTemplatesResponse represents available session templates
type SessionTemplatesResponse struct {
	Templates []models.SessionTemplate `json:"templates"`
}

// SaveSessionRequest represents request to save a session
type SaveSessionRequest struct {
	Name        string                  `json:"name" binding:"required"`
	Folder      string                  `json:"folder" binding:"required"`
	Description string                  `json:"description"`
	Template    string                  `json:"template" binding:"required"`
	Changes     []models.HPAChange      `json:"changes"`
	NodePools   []models.NodePoolChange `json:"node_pool_changes"`
}

// ListAllSessions returns all sessions from all folders
func (h *SessionsHandler) ListAllSessions(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	allSessions := []SessionSummary{}

	// Usar as MESMAS constantes do TUI
	folders := []session.SessionFolder{
		session.FolderHPAUpscale,
		session.FolderHPADownscale,
		session.FolderNodeUpscale,
		session.FolderNodeDownscale,
		session.FolderRollback,
	}

	for _, folder := range folders {
		sessions, err := h.sessionManager.ListSessionsInFolder(folder)
		if err != nil {
			continue // Skip folders with errors
		}

		for _, sess := range sessions {
			summary := SessionSummary{
				Name:         sess.Name,
				CreatedAt:    sess.CreatedAt.Format("2006-01-02 15:04:05"),
				CreatedBy:    sess.CreatedBy,
				Description:  sess.Description,
				TemplateUsed: sess.TemplateUsed,
				Metadata:     sess.Metadata,
				Folder:       string(folder),
			}
			allSessions = append(allSessions, summary)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": SessionListResponse{
			Sessions: allSessions,
			Count:    len(allSessions),
		},
	})
}

// ListSessionFolders returns available session folders with counts
func (h *SessionsHandler) ListSessionFolders(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	// Usar os mesmos folders do TUI
	folders := []SessionFolderInfo{
		{
			Name:        string(session.FolderHPAUpscale),
			Description: "HPA scale up sessions",
			Count:       h.getSessionCountInFolder(session.FolderHPAUpscale),
		},
		{
			Name:        string(session.FolderHPADownscale),
			Description: "HPA scale down sessions",
			Count:       h.getSessionCountInFolder(session.FolderHPADownscale),
		},
		{
			Name:        string(session.FolderNodeUpscale),
			Description: "Node pool scale up sessions",
			Count:       h.getSessionCountInFolder(session.FolderNodeUpscale),
		},
		{
			Name:        string(session.FolderNodeDownscale),
			Description: "Node pool scale down sessions",
			Count:       h.getSessionCountInFolder(session.FolderNodeDownscale),
		},
		{
			Name:        string(session.FolderRollback),
			Description: "Rollback sessions",
			Count:       h.getSessionCountInFolder(session.FolderRollback),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": SessionFoldersResponse{
			Folders: folders,
		},
	})
}

// ListSessionsInFolder returns sessions from a specific folder
func (h *SessionsHandler) ListSessionsInFolder(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	folderName := c.Param("folder")

	// Validar nome da pasta usando as constantes do TUI
	folder, err := h.parseSessionFolder(folderName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_FOLDER",
				"message": fmt.Sprintf("Invalid folder name: %s", folderName),
			},
		})
		return
	}

	sessions, err := h.sessionManager.ListSessionsInFolder(folder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list sessions: %v", err),
			},
		})
		return
	}

	// Converter para summaries
	summaries := make([]SessionSummary, len(sessions))
	for i, sess := range sessions {
		summaries[i] = SessionSummary{
			Name:         sess.Name,
			CreatedAt:    sess.CreatedAt.Format("2006-01-02 15:04:05"),
			CreatedBy:    sess.CreatedBy,
			Description:  sess.Description,
			TemplateUsed: sess.TemplateUsed,
			Metadata:     sess.Metadata,
			Folder:       folderName,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": SessionListResponse{
			Sessions: summaries,
			Count:    len(summaries),
		},
	})
}

// GetSession returns a specific session
func (h *SessionsHandler) GetSession(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	sessionName := c.Param("name")
	folder := c.Query("folder")

	var sess *models.Session
	var err error

	if folder != "" {
		// Carregar de pasta específica - USAR MÉTODOS DO TUI
		sessionFolder, parseErr := h.parseSessionFolder(folder)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_FOLDER",
					"message": fmt.Sprintf("Invalid folder name: %s", folder),
				},
			})
			return
		}
		sess, err = h.sessionManager.LoadSessionFromFolder(sessionName, sessionFolder)
	} else {
		// Buscar em todas as pastas - USAR MÉTODO DO TUI
		sess, err = h.sessionManager.LoadSession(sessionName)
	}

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_NOT_FOUND",
				"message": fmt.Sprintf("Session not found: %s", sessionName),
			},
		})
		return
	}

	// Adicionar o campo folder à resposta se foi especificado
	response := map[string]interface{}{
		"name":              sess.Name,
		"created_at":        sess.CreatedAt,
		"created_by":        sess.CreatedBy,
		"description":       sess.Description,
		"template_used":     sess.TemplateUsed,
		"metadata":          sess.Metadata,
		"changes":           sess.Changes,
		"node_pool_changes": sess.NodePoolChanges,
		"resource_changes":  sess.ResourceChanges,
		"rollback_data":     sess.RollbackData,
	}

	// Adicionar folder se foi especificado na query
	if folder != "" {
		response["folder"] = folder
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// SaveSession saves a new session
func (h *SessionsHandler) SaveSession(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	var req SaveSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request: %v", err),
			},
		})
		return
	}

	// Validar nome da pasta usando as constantes do TUI
	folder, err := h.parseSessionFolder(req.Folder)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_FOLDER",
				"message": fmt.Sprintf("Invalid folder name: %s", req.Folder),
			},
		})
		return
	}

	// Criar sessão usando a MESMA estrutura do TUI
	session := &models.Session{
		Name:            req.Name,
		Description:     req.Description,
		TemplateUsed:    req.Template,
		Changes:         req.Changes,
		NodePoolChanges: req.NodePools,
		// CreatedAt e CreatedBy serão preenchidos pelo SessionManager
		// Metadata será gerado automaticamente pelo SessionManager
		RollbackData: &models.RollbackData{
			OriginalStateCaptured:   true,
			CanRollback:             true,
			RollbackScriptGenerated: false,
		},
	}

	// Salvar sessão usando o MESMO método do TUI
	err = h.sessionManager.SaveSessionToFolder(session, folder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SAVE_ERROR",
				"message": fmt.Sprintf("Failed to save session: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"message":      "Session saved successfully",
			"session_name": session.Name,
			"folder":       req.Folder,
		},
	})
}

// DeleteSession deletes a session
func (h *SessionsHandler) DeleteSession(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	sessionName := c.Param("name")
	folder := c.Query("folder")

	var err error

	if folder != "" {
		// Deletar de pasta específica - USAR MÉTODOS DO TUI
		sessionFolder, parseErr := h.parseSessionFolder(folder)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_FOLDER",
					"message": fmt.Sprintf("Invalid folder name: %s", folder),
				},
			})
			return
		}
		err = h.sessionManager.DeleteSessionFromFolder(sessionName, sessionFolder)
	} else {
		// Deletar de todas as pastas - USAR MÉTODO DO TUI
		err = h.sessionManager.DeleteSession(sessionName)
	}

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": fmt.Sprintf("Failed to delete session: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"message":      "Session deleted successfully",
			"session_name": sessionName,
		},
	})
}

// RenameSession renames a session
func (h *SessionsHandler) RenameSession(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	oldName := c.Param("name")
	folder := c.Query("folder")

	var request struct {
		NewName string `json:"new_name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	var err error

	if folder != "" {
		// Renomear em pasta específica
		sessionFolder, parseErr := h.parseSessionFolder(folder)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_FOLDER",
					"message": fmt.Sprintf("Invalid folder name: %s", folder),
				},
			})
			return
		}
		err = h.sessionManager.RenameSessionInFolder(oldName, request.NewName, sessionFolder)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "FOLDER_REQUIRED",
				"message": "Folder parameter is required for rename operation",
			},
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "RENAME_ERROR",
				"message": fmt.Sprintf("Failed to rename session: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"message":  "Session renamed successfully",
			"old_name": oldName,
			"new_name": request.NewName,
		},
	})
}

// UpdateSession updates an existing session content
func (h *SessionsHandler) UpdateSession(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	folder := c.Query("folder")

	if folder == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "FOLDER_REQUIRED",
				"message": "Folder parameter is required for update operation",
			},
		})
		return
	}

	// Parse da sessão do body
	var updatedSession models.Session
	if err := c.ShouldBindJSON(&updatedSession); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid session data: %v", err),
			},
		})
		return
	}

	// Parse do folder
	sessionFolder, parseErr := h.parseSessionFolder(folder)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_FOLDER",
				"message": fmt.Sprintf("Invalid folder name: %s", folder),
			},
		})
		return
	}

	// Atualizar metadata
	updatedSession.Metadata = &models.SessionMetadata{
		ClustersAffected: make([]string, 0),
		NamespacesCount:  0,
		HPACount:         len(updatedSession.Changes),
		NodePoolCount:    len(updatedSession.NodePoolChanges),
		ResourceCount:    len(updatedSession.ResourceChanges),
		TotalChanges:     len(updatedSession.Changes) + len(updatedSession.NodePoolChanges) + len(updatedSession.ResourceChanges),
	}

	// Coletar clusters afetados
	clusterMap := make(map[string]bool)
	namespaceMap := make(map[string]bool)

	for _, change := range updatedSession.Changes {
		clusterMap[change.Cluster] = true
		namespaceMap[fmt.Sprintf("%s/%s", change.Cluster, change.Namespace)] = true
	}

	for _, change := range updatedSession.NodePoolChanges {
		clusterMap[change.Cluster] = true
	}

	for cluster := range clusterMap {
		updatedSession.Metadata.ClustersAffected = append(updatedSession.Metadata.ClustersAffected, cluster)
	}
	updatedSession.Metadata.NamespacesCount = len(namespaceMap)

	// Salvar a sessão atualizada
	err := h.sessionManager.SaveSessionToFolder(&updatedSession, sessionFolder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UPDATE_ERROR",
				"message": fmt.Sprintf("Failed to update session: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"message":      "Session updated successfully",
			"session_name": updatedSession.Name,
		},
	})
}

// GetSessionTemplates returns available session templates
func (h *SessionsHandler) GetSessionTemplates(c *gin.Context) {
	if h.sessionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_MANAGER_ERROR",
				"message": "Session manager not initialized",
			},
		})
		return
	}

	// Usar EXATAMENTE o mesmo método do TUI
	templates := h.sessionManager.GetTemplates()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": SessionTemplatesResponse{
			Templates: templates,
		},
	})
}

// Helper methods

func (h *SessionsHandler) getSessionCountInFolder(folder session.SessionFolder) int {
	if h.sessionManager == nil {
		return 0
	}

	sessions, err := h.sessionManager.ListSessionsInFolder(folder)
	if err != nil {
		return 0
	}
	return len(sessions)
}

func (h *SessionsHandler) parseSessionFolder(folderName string) (session.SessionFolder, error) {
	// Usar EXATAMENTE as mesmas constantes do TUI
	switch folderName {
	case string(session.FolderHPAUpscale):
		return session.FolderHPAUpscale, nil
	case string(session.FolderHPADownscale):
		return session.FolderHPADownscale, nil
	case string(session.FolderNodeUpscale):
		return session.FolderNodeUpscale, nil
	case string(session.FolderNodeDownscale):
		return session.FolderNodeDownscale, nil
	case string(session.FolderRollback):
		return session.FolderRollback, nil
	default:
		return "", fmt.Errorf("invalid folder name: %s", folderName)
	}
}
