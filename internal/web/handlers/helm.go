package handlers

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	helmservice "k8s-hpa-manager/internal/helm"
	helm "k8s-hpa-manager/internal/pkg/helm"
)

// HelmHandler serve as the HTTP layer for helm operations.
type HelmHandler struct {
	service *helmservice.Service
}

// NewHelmHandler builds a new HelmHandler instance.
func NewHelmHandler(service *helmservice.Service) *HelmHandler {
	return &HelmHandler{service: service}
}

// List returns helm releases for the provided cluster/namespace.
func (h *HelmHandler) List(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	filter := strings.TrimSpace(c.Query("search"))
	statuses := splitCSV(c.Query("statuses"))
	limit := parseIntDefault(c.DefaultQuery("limit", "0"))

	releases, err := h.service.ListReleases(
		c.Request.Context(),
		cluster,
		helm.ListReleasesOptions{
			Namespace: namespace,
			Filter:    filter,
			Statuses:  statuses,
			Limit:     limit,
		},
	)
	if err != nil {
		h.respondInternalError(c, "LIST_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"releases": releases,
			"count":    len(releases),
		},
	})
}

// Get returns details for a specific helm release.
func (h *HelmHandler) Get(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	detail, err := h.service.GetRelease(
		c.Request.Context(),
		cluster,
		helm.GetReleaseOptions{
			Namespace: namespace,
			Release:   release,
		},
	)
	if err != nil {
		h.respondInternalError(c, "GET_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    detail,
	})
}

// History returns the revision history for a release.
func (h *HelmHandler) History(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	limit := parseIntDefault(c.DefaultQuery("limit", "0"))

	revisions, err := h.service.History(
		c.Request.Context(),
		cluster,
		helm.HistoryOptions{
			Namespace: namespace,
			Release:   release,
			Limit:     limit,
		},
	)
	if err != nil {
		h.respondInternalError(c, "HISTORY_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"revisions": revisions,
			"count":     len(revisions),
		},
	})
}

// GetRevisionValues returns the values for a specific revision of a release.
func (h *HelmHandler) GetRevisionValues(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	revisionStr := strings.TrimSpace(c.Param("revision"))
	revision, err := strconv.Atoi(revisionStr)
	if err != nil || revision <= 0 {
		h.respondBadRequest(c, "INVALID_REVISION", err)
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))

	// Fetch release details for the specific revision
	detail, err := h.service.GetRelease(
		c.Request.Context(),
		cluster,
		helm.GetReleaseOptions{
			Namespace: namespace,
			Release:   release,
			Revision:  revision,
		},
	)
	if err != nil {
		h.respondInternalError(c, "GET_REVISION_VALUES_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"revision":       revision,
			"valuesRaw":      detail.ValuesRaw,
			"valuesRendered": detail.ValuesRendered,
		},
	})
}

// Install handles helm install requests.
func (h *HelmHandler) Install(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	var payload helmInstallPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		h.respondBadRequest(c, "INVALID_PAYLOAD", err)
		return
	}

	if payload.ReleaseName == "" {
		h.respondMissingParam(c, "releaseName")
		return
	}
	if payload.ChartRef == "" {
		h.respondMissingParam(c, "chartRef")
		return
	}

	resp, err := h.service.Execute(
		c.Request.Context(),
		cluster,
		helm.HelmActionRequest{
			Namespace:   payload.Namespace,
			ReleaseName: payload.ReleaseName,
			Action:      helm.ActionInstall,
			ChartRef:    payload.ChartRef,
			Version:     payload.Version,
			ValuesYAML:  payload.ValuesYAML,
			Force:       payload.Force,
			DryRun:      payload.DryRun,
		},
	)
	if err != nil {
		h.respondInternalError(c, "INSTALL_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// Upgrade handles helm upgrade requests.
func (h *HelmHandler) Upgrade(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	var payload helmUpgradePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		h.respondBadRequest(c, "INVALID_PAYLOAD", err)
		return
	}

	resp, err := h.service.Execute(
		c.Request.Context(),
		cluster,
		helm.HelmActionRequest{
			Namespace:   payload.Namespace,
			ReleaseName: release,
			Action:      helm.ActionUpgrade,
			ChartRef:    payload.ChartRef,
			Version:     payload.Version,
			ValuesYAML:  payload.ValuesYAML,
			Force:       payload.Force,
			DryRun:      payload.DryRun,
		},
	)
	if err != nil {
		h.respondInternalError(c, "UPGRADE_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// Rollback handles helm rollback requests.
func (h *HelmHandler) Rollback(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	var payload helmRollbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		h.respondBadRequest(c, "INVALID_PAYLOAD", err)
		return
	}

	if payload.TargetRevision <= 0 {
		h.respondBadRequest(c, "INVALID_REVISION", nil)
		return
	}

	resp, err := h.service.Execute(
		c.Request.Context(),
		cluster,
		helm.HelmActionRequest{
			Namespace:      payload.Namespace,
			ReleaseName:    release,
			Action:         helm.ActionRollback,
			TargetRevision: payload.TargetRevision,
			Force:          payload.Force,
		},
	)
	if err != nil {
		h.respondInternalError(c, "ROLLBACK_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// Uninstall handles helm uninstall requests.
func (h *HelmHandler) Uninstall(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		h.respondMissingParam(c, "cluster")
		return
	}

	release := strings.TrimSpace(c.Param("release"))
	if release == "" {
		h.respondMissingParam(c, "release")
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))

	resp, err := h.service.Execute(
		c.Request.Context(),
		cluster,
		helm.HelmActionRequest{
			Namespace:   namespace,
			ReleaseName: release,
			Action:      helm.ActionUninstall,
		},
	)
	if err != nil {
		h.respondInternalError(c, "UNINSTALL_ERROR", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// StreamOperation proxies downstream operation log streaming.
func (h *HelmHandler) StreamOperation(c *gin.Context) {
	operationID := strings.TrimSpace(c.Param("operationId"))
	if operationID == "" {
		h.respondMissingParam(c, "operationId")
		return
	}

	updates, cancel, err := h.service.StreamOperation(c.Request.Context(), operationID)
	if err != nil {
		h.respondInternalError(c, "STREAM_ERROR", err)
		return
	}
	defer cancel()

	c.Stream(func(w io.Writer) bool {
		event, ok := <-updates
		if !ok {
			return false
		}

		c.SSEvent("helm-operation", event)
		return true
	})
}

type helmInstallPayload struct {
	ReleaseName string `json:"releaseName"`
	Namespace   string `json:"namespace"`
	ChartRef    string `json:"chartRef"`
	Version     string `json:"version"`
	ValuesYAML  string `json:"valuesYaml"`
	Force       bool   `json:"force"`
	DryRun      bool   `json:"dryRun"`
}

type helmUpgradePayload struct {
	Namespace  string `json:"namespace"`
	ChartRef   string `json:"chartRef"`
	Version    string `json:"version"`
	ValuesYAML string `json:"valuesYaml"`
	Force      bool   `json:"force"`
	DryRun     bool   `json:"dryRun"`
}

type helmRollbackPayload struct {
	Namespace      string `json:"namespace"`
	TargetRevision int    `json:"targetRevision"`
	Force          bool   `json:"force"`
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	var cleaned []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func parseIntDefault(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func (h *HelmHandler) respondMissingParam(c *gin.Context, field string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"error": gin.H{
			"code":    "MISSING_PARAMETER",
			"message": "Parameter '" + field + "' is required",
		},
	})
}

func (h *HelmHandler) respondBadRequest(c *gin.Context, code string, err error) {
	message := "invalid request payload"
	if err != nil {
		message = err.Error()
	}

	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

func (h *HelmHandler) respondInternalError(c *gin.Context, code string, err error) {
	message := "internal error"
	if err != nil {
		message = err.Error()
	}

	c.JSON(http.StatusInternalServerError, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}
