package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/teams"
)

// TeamsHandler gerencia extração de aprovações SRE do Microsoft Teams (Mr.ViaBot).
type TeamsHandler struct {
	logger     *zerolog.Logger
	cacheDir   string
	cacheMu    sync.RWMutex
	cache      *teamsCache
	refreshing bool
}

type teamsCache struct {
	Items       []teams.ApprovalItem `json:"items"`
	LastUpdated time.Time            `json:"last_updated"`
}

// NewTeamsHandler cria handler com diretório de cache em ~/.k8s-hpa-manager/teams-cache/.
func NewTeamsHandler(logger *zerolog.Logger) *TeamsHandler {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}
	cacheDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-cache")
	os.MkdirAll(cacheDir, 0700) //nolint:errcheck

	h := &TeamsHandler{
		logger:   logger,
		cacheDir: cacheDir,
	}
	h.loadCacheFromDisk()
	return h
}

// GetApprovalsToday retorna apenas as CHGs do dia atual (filtradas por ExtractedAt).
// O cache interno guarda até 2 dias — use SearchCHG para buscar além da lista de hoje.
// GET /api/v1/teams/approvals/today
func (h *TeamsHandler) GetApprovalsToday(c *gin.Context) {
	h.cacheMu.RLock()
	cached := h.cache
	refreshing := h.refreshing
	h.cacheMu.RUnlock()

	if cached != nil {
		today := time.Now()
		var todayItems []teams.ApprovalItem
		for _, item := range cached.Items {
			if item.ExtractedAt.Year() == today.Year() && item.ExtractedAt.YearDay() == today.YearDay() {
				todayItems = append(todayItems, item)
			}
		}
		if todayItems == nil {
			todayItems = []teams.ApprovalItem{}
		}
		needsRefresh := time.Since(cached.LastUpdated) >= 8*time.Hour
		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"items":         todayItems,
			"last_updated":  cached.LastUpdated,
			"needs_refresh": needsRefresh,
			"refreshing":    refreshing,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"items":         []teams.ApprovalItem{},
		"last_updated":  nil,
		"needs_refresh": true,
		"refreshing":    refreshing,
	})
}

// SearchCHG busca uma CHG em todo o cache (últimos 2 dias).
// Mais rápido que uma varredura do Teams — responde em ms se estiver no cache.
// GET /api/v1/teams/approvals/search?chg=CHG0455046
func (h *TeamsHandler) SearchCHG(c *gin.Context) {
	chg := strings.ToUpper(strings.TrimSpace(c.Query("chg")))
	if chg == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro chg obrigatório"})
		return
	}

	h.cacheMu.RLock()
	cached := h.cache
	h.cacheMu.RUnlock()

	if cached != nil {
		for _, item := range cached.Items {
			if strings.ToUpper(item.CHG) == chg {
				c.JSON(http.StatusOK, gin.H{"found": true, "item": item})
				return
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"found": false})
}

// RefreshApprovals dispara extração completa do Teams e atualiza o cache.
// POST /api/v1/teams/approvals/refresh
// Bloqueia até a extração terminar (pode levar ~90s — Chrome abre e navega).
func (h *TeamsHandler) RefreshApprovals(c *gin.Context) {
	h.cacheMu.Lock()
	if h.refreshing {
		h.cacheMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "Extração já em andamento",
		})
		return
	}
	h.refreshing = true
	h.cacheMu.Unlock()

	defer func() {
		h.cacheMu.Lock()
		h.refreshing = false
		h.cacheMu.Unlock()
	}()

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}

	extractor := teams.NewExtractor(homeDir, h.logger)
	result, err := extractor.Extract()
	if err != nil {
		h.logger.Error().Err(err).Msg("[Teams] Falha na extração de aprovações")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Mesclar: manter itens das últimas 48h que não foram capturados hoje
	twoDaysAgo := time.Now().Add(-48 * time.Hour)
	newCHGs := map[string]bool{}
	for _, item := range result.Items {
		newCHGs[strings.ToUpper(item.CHG)] = true
	}
	mergedItems := append([]teams.ApprovalItem{}, result.Items...)
	h.cacheMu.RLock()
	oldCache := h.cache
	h.cacheMu.RUnlock()
	if oldCache != nil {
		for _, item := range oldCache.Items {
			if item.ExtractedAt.After(twoDaysAgo) && !newCHGs[strings.ToUpper(item.CHG)] {
				mergedItems = append(mergedItems, item)
			}
		}
	}

	newCache := &teamsCache{
		Items:       mergedItems,
		LastUpdated: result.ExtractedAt,
	}
	h.cacheMu.Lock()
	h.cache = newCache
	h.cacheMu.Unlock()
	h.saveCacheToDisk(newCache)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"items":        result.Items,
		"last_updated": result.ExtractedAt,
		"needs_refresh": false,
	})
}

func (h *TeamsHandler) loadCacheFromDisk() {
	path := filepath.Join(h.cacheDir, "approvals-cache.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c teamsCache
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	if len(c.Items) == 0 {
		return
	}
	// Sempre carrega — TTL só afeta needs_refresh na resposta, não oculta os dados
	h.cache = &c
	age := time.Since(c.LastUpdated).Round(time.Minute)
	h.logger.Info().Time("last_updated", c.LastUpdated).Dur("age", age).Int("items", len(c.Items)).
		Msg("[Teams] Cache de aprovações carregado do disco")
}

func (h *TeamsHandler) saveCacheToDisk(c *teamsCache) {
	path := filepath.Join(h.cacheDir, "approvals-cache.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0600) //nolint:errcheck
}
