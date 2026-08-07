package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"k8s-hpa-manager/internal/certificates"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
)

// certEndpointsCheckAllConcurrency limita quantos handshakes TLS rodam em paralelo no
// "Verificar agora" — mesmo padrão de scanFleetConcurrency (access_check_scan.go).
const certEndpointsCheckAllConcurrency = 8

// certEndpointsCheckAllTimeout é o teto por endpoint dentro de CheckAll — CheckEndpointTLS já
// tem seu próprio timeout interno (5s), isto é só uma rede de segurança extra.
const certEndpointsCheckAllTimeout = 10 * time.Second

// CertEndpointsHandler gerencia a lista de endpoints externos (fora de qualquer cluster K8s)
// monitorados por handshake TLS — ver EXTERNAL-CERT-MONITOR-PLAN.md.
type CertEndpointsHandler struct {
	store *storage.CertEndpointsStore
}

// NewCertEndpointsHandler cria o handler.
func NewCertEndpointsHandler(store *storage.CertEndpointsStore) *CertEndpointsHandler {
	return &CertEndpointsHandler{store: store}
}

func certEndpointUserEmail(c *gin.Context) string {
	email, _ := c.Get("user_email")
	s := fmt.Sprintf("%v", email)
	if s == "" || s == "<nil>" {
		return "default"
	}
	return s
}

// certEndpointRequestBody é o shape compartilhado por Create/Update — os campos editáveis do
// endpoint. Port tem default 443 quando omitido/zero (0 nunca é uma porta válida de verdade).
type certEndpointRequestBody struct {
	Name       string `json:"name" binding:"required"`
	Host       string `json:"host" binding:"required"`
	Port       int    `json:"port"`
	SNI        string `json:"sni"`
	GroupLabel string `json:"group_label"`
	Enabled    *bool  `json:"enabled"` // ponteiro: distingue "não enviado" (Update parcial) de false
}

// List — GET /api/v1/cert-endpoints
func (h *CertEndpointsHandler) List(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusOK, []storage.CertEndpointWithStatus{})
		return
	}
	list, err := h.store.ListWithLatestCheck()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// History — GET /api/v1/cert-endpoints/:id/history?limit=
func (h *CertEndpointsHandler) History(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if h.store == nil {
		c.JSON(http.StatusOK, []storage.CertEndpointCheck{})
		return
	}
	history, err := h.store.GetHistory(id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, history)
}

// Create — POST /api/v1/cert-endpoints
func (h *CertEndpointsHandler) Create(c *gin.Context) {
	var body certEndpointRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido: " + err.Error()})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	port := body.Port
	if port == 0 {
		port = 443
	}
	id, err := h.store.Create(storage.CertEndpoint{
		Name:       body.Name,
		Host:       body.Host,
		Port:       port,
		SNI:        body.SNI,
		GroupLabel: body.GroupLabel,
		CreatedBy:  certEndpointUserEmail(c),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Update — PUT /api/v1/cert-endpoints/:id
func (h *CertEndpointsHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	var body certEndpointRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido: " + err.Error()})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	existing, err := h.store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "endpoint não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	port := body.Port
	if port == 0 {
		port = 443
	}
	enabled := existing.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	if err := h.store.Update(id, storage.CertEndpoint{
		Name:       body.Name,
		Host:       body.Host,
		Port:       port,
		SNI:        body.SNI,
		GroupLabel: body.GroupLabel,
		Enabled:    enabled,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete — DELETE /api/v1/cert-endpoints/:id
func (h *CertEndpointsHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	if err := h.store.Delete(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "endpoint não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// CheckOne — POST /api/v1/cert-endpoints/:id/check — dispara uma checagem sob demanda pra um
// único endpoint (ex: logo depois de cadastrar, ou um "recheck" pontual na listagem).
func (h *CertEndpointsHandler) CheckOne(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	endpoint, err := h.store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "endpoint não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	check := runAndRecordCertEndpointCheck(c.Request.Context(), h.store, *endpoint)
	c.JSON(http.StatusOK, check)
}

// CheckAll — POST /api/v1/cert-endpoints/check-all — roda o handshake TLS de todos os endpoints
// habilitados em paralelo (semáforo, mesmo padrão de scanOneFleetCluster) e retorna a listagem
// já atualizada. Endpoints desabilitados (Enabled=false) são pulados — mantêm o LatestCheck que
// já tinham, sem re-checar.
func (h *CertEndpointsHandler) CheckAll(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusOK, []storage.CertEndpointWithStatus{})
		return
	}
	endpoints, err := h.store.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sem := make(chan struct{}, certEndpointsCheckAllConcurrency)
	var wg sync.WaitGroup
	ctx := c.Request.Context()

	for _, e := range endpoints {
		if !e.Enabled {
			continue
		}
		wg.Add(1)
		go func(endpoint storage.CertEndpoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			checkCtx, cancel := context.WithTimeout(ctx, certEndpointsCheckAllTimeout)
			defer cancel()
			runAndRecordCertEndpointCheck(checkCtx, h.store, endpoint)
		}(e)
	}
	wg.Wait()

	list, err := h.store.ListWithLatestCheck()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// runAndRecordCertEndpointCheck roda o handshake TLS de verdade (certificates.CheckEndpointTLS)
// e persiste o resultado — ponto único que converte certificates.EndpointCheckResult (domínio)
// em storage.CertEndpointCheck (storage), usado tanto por CheckOne quanto por CheckAll. Falha ao
// persistir é só logada (não impede o chamador de ver o resultado da checagem em si).
func runAndRecordCertEndpointCheck(ctx context.Context, store *storage.CertEndpointsStore, endpoint storage.CertEndpoint) storage.CertEndpointCheck {
	result := certificates.CheckEndpointTLS(ctx, endpoint.Host, endpoint.Port, endpoint.SNI)

	check := storage.CertEndpointCheck{
		EndpointID:        endpoint.ID,
		CheckedAt:         time.Now().UTC(),
		Success:           result.Success,
		ErrorMessage:      result.ErrorMessage,
		Subject:           result.Subject,
		Issuer:            result.Issuer,
		SerialNumber:      result.SerialNumber,
		DNSNames:          result.DNSNames,
		ChainLength:       result.ChainLength,
		Status:            result.Status,
		DaysRemaining:     result.DaysRemaining,
		TrustedByPublicCA: result.TrustedByPublicCA,
	}
	if !result.NotBefore.IsZero() {
		nb := result.NotBefore
		check.NotBefore = &nb
	}
	if !result.NotAfter.IsZero() {
		na := result.NotAfter
		check.NotAfter = &na
	}

	if id, err := store.RecordCheck(check); err == nil {
		check.ID = id
	}
	return check
}
