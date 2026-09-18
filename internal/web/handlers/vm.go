package handlers

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/models"
)

// vmInstanceCacheEntry é uma entrada de cache de listagem de instâncias VM para um profile+região
// — mesmo padrão de nodePoolCacheEntry/nodePoolCacheTTL (nodepools.go), TTL curto pois o usuário
// pode start/stop uma instância e esperar ver o novo estado logo em seguida.
type vmInstanceCacheEntry struct {
	instances []models.VMInstance
	exp       time.Time
}

// vmInstanceCacheTTL — mesmo racional do nodePoolCacheTTL, mas mais curto: instâncias VM mudam de
// estado (running/stopped) por ação direta do usuário nesta mesma ferramenta, então um TTL de 2min
// deixaria a lista "atrasada" por tempo demais logo após um start/stop. invalidateCache é chamado
// explicitamente após qualquer power action, então este TTL só cobre o caso de alguém mudar o
// estado por fora da aplicação (console AWS, outro operador).
const vmInstanceCacheTTL = 20 * time.Second

// VMHandler gerencia requisições relacionadas a VMs/instâncias standalone (fora de qualquer
// cluster K8s) — hoje só AWS EC2 implementado, ver internal/cloudprovider/aws/ec2.go. O campo
// "provider" em cada rota é resolvido contra um switch simples (não um registry) porque só existe
// uma implementação por ora — generalizar pra um registry de verdade fica pra quando um segundo
// provider (Azure VM/GCE) for implementado.
type VMHandler struct {
	historyTracker *history.HistoryTracker

	cache   map[string]*vmInstanceCacheEntry
	cacheMu sync.RWMutex
}

// NewVMHandler cria um novo handler de VMs.
func NewVMHandler(ht *history.HistoryTracker) *VMHandler {
	return &VMHandler{
		historyTracker: ht,
		cache:          make(map[string]*vmInstanceCacheEntry),
	}
}

// resolveEC2Provider monta um AWSEC2Provider pro profile/região informados — hoje o único
// provider suportado; providers != "aws" retornam erro claro em vez de silenciosamente cair no
// AWS (evita confundir "não implementado ainda" com "funcionou mas devolveu vazio").
func resolveEC2Provider(provider, region, profile string) (*awsprovider.AWSEC2Provider, error) {
	if provider != "" && provider != "aws" {
		return nil, fmt.Errorf("provider '%s' ainda não implementado — só AWS EC2 por enquanto", provider)
	}
	if profile == "" {
		return nil, fmt.Errorf("profile é obrigatório")
	}
	return awsprovider.NewAWSEC2Provider(region, profile), nil
}

func vmCacheKey(profile, region string) string {
	return profile + "/" + region
}

// ListProfiles lista os profiles AWS configurados no host do servidor — reaproveita
// config.ListAWSProfiles (internal/config/eks_discovery.go), já usado pelo autodiscovery de
// clusters EKS, sem duplicar a chamada `aws configure list-profiles`.
func (h *VMHandler) ListProfiles(c *gin.Context) {
	profiles, err := config.ListAWSProfiles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_PROFILES_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": profiles})
}

// ListInstances lista as instâncias EC2 de um profile+região, com cache curto (vmInstanceCacheTTL).
func (h *VMHandler) ListInstances(c *gin.Context) {
	profile := c.Query("profile")
	region := c.Query("region")
	provider := c.Query("provider")
	forceRefresh := c.Query("refresh") == "true"

	key := vmCacheKey(profile, region)

	if !forceRefresh {
		h.cacheMu.RLock()
		if entry, ok := h.cache[key]; ok && time.Now().Before(entry.exp) {
			instances := entry.instances
			h.cacheMu.RUnlock()
			c.JSON(http.StatusOK, gin.H{"success": true, "data": instances})
			return
		}
		h.cacheMu.RUnlock()
	}

	ec2, err := resolveEC2Provider(provider, region, profile)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	instances, err := ec2.ListInstances(c.Request.Context(), models.VMFilter{})
	if err != nil {
		log.Error().Err(err).Str("profile", profile).Str("region", region).Msg("Erro ao listar instâncias EC2")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_INSTANCES_ERROR", "message": err.Error()},
		})
		return
	}

	h.cacheMu.Lock()
	h.cache[key] = &vmInstanceCacheEntry{instances: instances, exp: time.Now().Add(vmInstanceCacheTTL)}
	h.cacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"success": true, "data": instances})
}

// invalidateCache remove a entrada de cache de um profile+região — chamado após qualquer power
// action, mesmo padrão de invalidateNodePoolCache (nodepools.go).
func (h *VMHandler) invalidateCache(profile, region string) {
	h.cacheMu.Lock()
	delete(h.cache, vmCacheKey(profile, region))
	h.cacheMu.Unlock()
}

// GetInstance retorna os detalhes de uma única instância (sem cache — chamada pontual, não faz
// parte do polling de lista).
func (h *VMHandler) GetInstance(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	provider := c.Query("provider")

	ec2, err := resolveEC2Provider(provider, region, profile)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	instance, err := ec2.GetInstance(c.Request.Context(), instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "GET_INSTANCE_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": instance})
}

// powerActionRequest é o corpo esperado por Start/Stop/RebootInstance.
type powerActionRequest struct {
	Provider string `json:"provider"`
	Region   string `json:"region" binding:"required"`
	Profile  string `json:"profile" binding:"required"`
}

// StartInstance, StopInstance, RebootInstance disparam a transição de energia e registram a ação
// no HistoryTracker — mesmo padrão de auditoria já usado por qualquer operação destrutiva desta
// app (ex: cert-rollback em certificates.go).
func (h *VMHandler) StartInstance(c *gin.Context) {
	h.powerAction(c, "vm-start", func(p *awsprovider.AWSEC2Provider, ctx *gin.Context, id string) error {
		return p.StartInstance(ctx.Request.Context(), id)
	})
}
func (h *VMHandler) StopInstance(c *gin.Context) {
	h.powerAction(c, "vm-stop", func(p *awsprovider.AWSEC2Provider, ctx *gin.Context, id string) error {
		return p.StopInstance(ctx.Request.Context(), id)
	})
}
func (h *VMHandler) RebootInstance(c *gin.Context) {
	h.powerAction(c, "vm-reboot", func(p *awsprovider.AWSEC2Provider, ctx *gin.Context, id string) error {
		return p.RebootInstance(ctx.Request.Context(), id)
	})
}

func (h *VMHandler) powerAction(c *gin.Context, action string, fn func(*awsprovider.AWSEC2Provider, *gin.Context, string) error) {
	instanceID := c.Param("instanceId")

	var req powerActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("region e profile são obrigatórios: %v", err)},
		})
		return
	}

	ec2, err := resolveEC2Provider(req.Provider, req.Region, req.Profile)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}

	status := "success"
	errMsg := ""
	if fnErr := fn(ec2, c, instanceID); fnErr != nil {
		status = "error"
		errMsg = fnErr.Error()
	}

	entry := CreateHistoryEntry(c, action, instanceID, req.Profile+"/"+req.Region, status, nil, nil, 0, errMsg)
	if logErr := h.historyTracker.Log(entry); logErr != nil {
		log.Warn().Err(logErr).Msg("erro ao registrar ação de VM no history tracker")
	}

	if errMsg != "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "POWER_ACTION_ERROR", "message": errMsg},
		})
		return
	}

	h.invalidateCache(req.Profile, req.Region)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "ação disparada com sucesso"}})
}
