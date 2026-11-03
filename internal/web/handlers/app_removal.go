package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/appremoval"
)

// AppRemovalHandler implementa a ferramenta "Remoção de Aplicação" (Tools menu). Nesta fase só
// existe leitura: lista os alvos possíveis e faz o scan das dependências. Nada é alterado no cluster.
type AppRemovalHandler struct{}

func NewAppRemovalHandler() *AppRemovalHandler { return &AppRemovalHandler{} }

// Candidates lista workloads e releases Helm do namespace.
func (h *AppRemovalHandler) Candidates(c *gin.Context) {
	cluster, namespace := c.Param("cluster"), c.Param("namespace")
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster e namespace são obrigatórios"))
		return
	}
	items, err := appremoval.Candidates(c.Request.Context(), cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"candidates": items})
}

// Scan descobre tudo o que pertence à aplicação (nome de workload ou de release Helm).
func (h *AppRemovalHandler) Scan(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace e nome são obrigatórios"))
		return
	}
	resources, err := appremoval.Collect(c.Request.Context(), cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SCAN_ERROR", err.Error()))
		return
	}
	result, err := appremoval.Scan(namespace, name, resources)
	if errors.Is(err, appremoval.ErrNotFound) {
		c.JSON(http.StatusNotFound, errorResponse("NOT_FOUND", err.Error()))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SCAN_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, result)
}
