package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/storage"
)

// DependenciesHandler gerencia requisições de dependências externas
type DependenciesHandler struct {
	scanner  *healthcheck.DependencyScanner
	registry *storage.DependencyRegistry
}

// NewDependenciesHandler cria um novo handler de dependências
func NewDependenciesHandler(scanner *healthcheck.DependencyScanner, registry *storage.DependencyRegistry) *DependenciesHandler {
	return &DependenciesHandler{
		scanner:  scanner,
		registry: registry,
	}
}

// ScanRequest representa uma requisição de scan
type ScanRequest struct {
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces"`
}

// Scan executa um scan de dependências em clusters e persiste no SQLite
// POST /api/v1/dependencies/scan
func (h *DependenciesHandler) Scan(c *gin.Context) {
	var req ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Dados inválidos",
				"details": err.Error(),
			},
		})
		return
	}

	if len(req.Clusters) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Pelo menos um cluster deve ser especificado",
			},
		})
		return
	}

	log.Info().
		Strs("clusters", req.Clusters).
		Strs("namespaces", req.Namespaces).
		Msg("Starting dependency scan request")

	// Executar scan em todos os clusters e persistir
	var totalFound, totalSaved, totalErrors int
	var allResults []*healthcheck.DependencyScanResult

	for _, cluster := range req.Clusters {
		startTime := time.Now()

		result, err := h.scanner.Scan(c.Request.Context(), cluster, req.Namespaces)
		if err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("Failed to scan cluster")
			totalErrors++
			continue
		}

		allResults = append(allResults, result)

		// Persistir cada dependência encontrada no SQLite
		savedCount := 0
		for _, dep := range result.Dependencies {
			record := &storage.DependencyRecord{
				ServiceName: dep.ServiceName,
				ServiceType: string(dep.ServiceType),
				TopicName:   dep.TopicName,
				Cluster:     dep.Cluster,
				Namespace:   dep.Namespace,
				Deployment:  dep.Deployment,
				SourceType:  dep.SourceType,
				SourceName:  dep.SourceName,
				SourceKey:   dep.SourceKey,
			}

			if err := h.registry.UpsertDependency(record); err != nil {
				log.Warn().Err(err).
					Str("service", dep.ServiceName).
					Str("cluster", cluster).
					Msg("Failed to persist dependency")
			} else {
				savedCount++
			}
		}

		totalFound += len(result.Dependencies)
		totalSaved += savedCount

		// Registrar histórico do scan
		durationMs := time.Since(startTime).Milliseconds()
		if err := h.registry.RecordScan(cluster, len(result.Namespaces), len(result.Dependencies), savedCount, durationMs); err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("Failed to record scan history")
		}

		log.Info().
			Str("cluster", cluster).
			Int("found", len(result.Dependencies)).
			Int("saved", savedCount).
			Int64("duration_ms", durationMs).
			Msg("Cluster scan completed")
	}

	// Agregar resultados para resposta
	aggregated := aggregateScanResults(allResults)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    aggregated,
		"scan_summary": gin.H{
			"clusters_scanned":   len(req.Clusters),
			"dependencies_found": totalFound,
			"dependencies_saved": totalSaved,
			"errors":             totalErrors,
		},
	})
}

// ScanCluster executa scan em um único cluster (para auto-scan)
// POST /api/v1/dependencies/scan/:cluster
func (h *DependenciesHandler) ScanCluster(c *gin.Context) {
	cluster := c.Param("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Cluster é obrigatório",
			},
		})
		return
	}

	log.Info().Str("cluster", cluster).Msg("Starting single cluster dependency scan")

	startTime := time.Now()

	result, err := h.scanner.Scan(c.Request.Context(), cluster, nil)
	if err != nil {
		// Detectar erro de VPN
		errorMsg := err.Error()
		isVPNError := strings.Contains(errorMsg, "no such host") ||
			strings.Contains(errorMsg, "dial tcp") ||
			strings.Contains(errorMsg, "i/o timeout")

		if isVPNError {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"error": gin.H{
					"message": "Cluster inacessível. Verifique a conexão VPN.",
					"details": errorMsg,
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao escanear cluster",
				"details": errorMsg,
			},
		})
		return
	}

	// Persistir dependências
	savedCount := 0
	for _, dep := range result.Dependencies {
		record := &storage.DependencyRecord{
			ServiceName: dep.ServiceName,
			ServiceType: string(dep.ServiceType),
			Cluster:     dep.Cluster,
			Namespace:   dep.Namespace,
			Deployment:  dep.Deployment,
			SourceType:  dep.SourceType,
			SourceName:  dep.SourceName,
			SourceKey:   dep.SourceKey,
		}

		if err := h.registry.UpsertDependency(record); err != nil {
			log.Warn().Err(err).Str("service", dep.ServiceName).Msg("Failed to persist dependency")
		} else {
			savedCount++
		}
	}

	durationMs := time.Since(startTime).Milliseconds()

	// Registrar histórico
	if err := h.registry.RecordScan(cluster, len(result.Namespaces), len(result.Dependencies), savedCount, durationMs); err != nil {
		log.Warn().Err(err).Msg("Failed to record scan history")
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cluster":             cluster,
			"namespaces_scanned":  len(result.Namespaces),
			"dependencies_found":  len(result.Dependencies),
			"dependencies_saved":  savedCount,
			"duration_ms":         durationMs,
		},
	})
}

// Search busca dependências por nome de serviço no SQLite (busca reversa)
// GET /api/v1/dependencies/search?q=rdsh-regional01
func (h *DependenciesHandler) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Parâmetro 'q' (query) é obrigatório",
			},
		})
		return
	}

	log.Info().Str("query", query).Msg("Searching for dependency usage in registry")

	// Buscar no SQLite (não precisa de clusters selecionados)
	results, err := h.registry.SearchByServiceName(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha na busca",
				"details": err.Error(),
			},
		})
		return
	}

	// Agrupar por serviço
	grouped := groupRegistryByService(results)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"query":       query,
			"total_found": len(results),
			"results":     results,
			"by_service":  grouped,
		},
	})
}

// GetRegistry retorna todas as dependências do SQLite
// GET /api/v1/dependencies/registry
func (h *DependenciesHandler) GetRegistry(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	serviceType := c.Query("type")

	deps, err := h.registry.GetAll(cluster, namespace, serviceType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao buscar registry",
				"details": err.Error(),
			},
		})
		return
	}

	// Agrupar por serviço
	grouped := groupRegistryByService(deps)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total":       len(deps),
			"dependencies": deps,
			"by_service":  grouped,
		},
	})
}

// GetStats retorna estatísticas do registry
// GET /api/v1/dependencies/stats
func (h *DependenciesHandler) GetStats(c *gin.Context) {
	stats, err := h.registry.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao obter estatísticas",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// GetScanHistory retorna histórico de scans
// GET /api/v1/dependencies/scan/history
func (h *DependenciesHandler) GetScanHistory(c *gin.Context) {
	history, err := h.registry.GetScanHistory(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao obter histórico",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    history,
	})
}

// GetClusters retorna lista de clusters únicos no registry
// GET /api/v1/dependencies/clusters
func (h *DependenciesHandler) GetClusters(c *gin.Context) {
	clusters, err := h.registry.GetUniqueClusters()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao obter clusters",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    clusters,
	})
}

// Export exporta todas as dependências em formato CSV, JSON ou Markdown
// GET /api/v1/dependencies/export?format=csv|json|md&cluster=...
func (h *DependenciesHandler) Export(c *gin.Context) {
	format := c.DefaultQuery("format", "json")
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	serviceType := c.Query("type")

	log.Info().
		Str("format", format).
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("type", serviceType).
		Msg("Exporting dependencies from registry")

	deps, err := h.registry.GetAll(cluster, namespace, serviceType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao exportar dependências",
				"details": err.Error(),
			},
		})
		return
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")

	switch format {
	case "csv":
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=dependencies_%s.csv", timestamp))

		writer := csv.NewWriter(c.Writer)
		defer writer.Flush()

		// Header
		writer.Write([]string{
			"Service Name", "Service Type", "Cluster", "Namespace", "Deployment",
			"Source Type", "Source Name", "Source Key", "First Seen", "Last Seen",
		})

		// Dados
		for _, dep := range deps {
			writer.Write([]string{
				dep.ServiceName,
				dep.ServiceType,
				dep.Cluster,
				dep.Namespace,
				dep.Deployment,
				dep.SourceType,
				dep.SourceName,
				dep.SourceKey,
				dep.FirstSeen.Format(time.RFC3339),
				dep.LastSeen.Format(time.RFC3339),
			})
		}

	case "json":
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=dependencies_%s.json", timestamp))

		// Agrupar para melhor visualização
		grouped := groupRegistryByService(deps)

		// Obter estatísticas
		stats, _ := h.registry.GetStats()

		exportData := gin.H{
			"exported_at":        time.Now(),
			"total_dependencies": len(deps),
			"unique_services":    len(grouped),
			"stats":              stats,
			"dependencies":       deps,
			"by_service":         grouped,
		}

		encoder := json.NewEncoder(c.Writer)
		encoder.SetIndent("", "  ")
		encoder.Encode(exportData)

	case "md", "markdown":
		c.Header("Content-Type", "text/markdown")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=dependencies_%s.md", timestamp))

		// Agrupar por serviço para o relatório
		grouped := groupRegistryByService(deps)

		// Obter estatísticas
		stats, _ := h.registry.GetStats()

		// Gerar Markdown
		var md strings.Builder

		md.WriteString("# Relatório de Dependências Externas\n\n")
		md.WriteString(fmt.Sprintf("**Data de Exportação:** %s\n\n", time.Now().Format("02/01/2006 15:04:05")))

		// Filtros aplicados
		if cluster != "" || namespace != "" || serviceType != "" {
			md.WriteString("## Filtros Aplicados\n\n")
			if cluster != "" {
				md.WriteString(fmt.Sprintf("- **Cluster:** %s\n", cluster))
			}
			if namespace != "" {
				md.WriteString(fmt.Sprintf("- **Namespace:** %s\n", namespace))
			}
			if serviceType != "" {
				md.WriteString(fmt.Sprintf("- **Tipo:** %s\n", serviceType))
			}
			md.WriteString("\n")
		}

		// Sumário
		md.WriteString("## Sumário\n\n")
		md.WriteString(fmt.Sprintf("| Métrica | Valor |\n"))
		md.WriteString(fmt.Sprintf("|---------|-------|\n"))
		md.WriteString(fmt.Sprintf("| Total de Dependências | %d |\n", len(deps)))
		md.WriteString(fmt.Sprintf("| Serviços Únicos | %d |\n", len(grouped)))
		if stats != nil {
			if uniqueClusters, ok := stats["unique_clusters"].(int); ok {
				md.WriteString(fmt.Sprintf("| Clusters | %d |\n", uniqueClusters))
			}
			if uniqueNamespaces, ok := stats["unique_namespaces"].(int); ok {
				md.WriteString(fmt.Sprintf("| Namespaces | %d |\n", uniqueNamespaces))
			}
		}
		md.WriteString("\n")

		// Por tipo de serviço
		if stats != nil {
			if byType, ok := stats["by_type"].(map[string]int); ok && len(byType) > 0 {
				md.WriteString("## Por Tipo de Serviço\n\n")
				md.WriteString("| Tipo | Quantidade |\n")
				md.WriteString("|------|------------|\n")
				for svcType, count := range byType {
					md.WriteString(fmt.Sprintf("| %s | %d |\n", svcType, count))
				}
				md.WriteString("\n")
			}
		}

		// Detalhamento por serviço
		md.WriteString("## Dependências por Serviço\n\n")
		for serviceName, serviceDeps := range grouped {
			md.WriteString(fmt.Sprintf("### %s\n\n", serviceName))
			md.WriteString(fmt.Sprintf("**Tipo:** %s | **Usada em:** %d local(is)\n\n", serviceDeps[0].ServiceType, len(serviceDeps)))

			md.WriteString("| Cluster | Namespace | Deployment | Fonte | Chave |\n")
			md.WriteString("|---------|-----------|------------|-------|-------|\n")
			for _, dep := range serviceDeps {
				deployment := dep.Deployment
				if deployment == "" {
					deployment = "-"
				}
				sourceKey := dep.SourceKey
				if sourceKey == "" {
					sourceKey = "-"
				}
				md.WriteString(fmt.Sprintf("| %s | %s | %s | %s/%s | %s |\n",
					dep.Cluster, dep.Namespace, deployment, dep.SourceType, dep.SourceName, sourceKey))
			}
			md.WriteString("\n")
		}

		// Footer
		md.WriteString("---\n\n")
		md.WriteString("*Relatório gerado automaticamente pelo K8s HPA Manager*\n")

		c.Writer.WriteString(md.String())

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Formato inválido. Use 'csv', 'json' ou 'md'",
			},
		})
	}
}

// GetServiceUsage retorna onde um serviço específico é usado
// GET /api/v1/dependencies/service/:serviceName
func (h *DependenciesHandler) GetServiceUsage(c *gin.Context) {
	serviceName := c.Param("serviceName")
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Nome do serviço é obrigatório",
			},
		})
		return
	}

	log.Info().Str("service_name", serviceName).Msg("Getting service usage from registry")

	results, err := h.registry.GetServiceUsage(serviceName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao buscar uso do serviço",
				"details": err.Error(),
			},
		})
		return
	}

	// Agrupar por cluster > namespace > deployment
	usage := make(map[string]map[string][]string)
	for _, dep := range results {
		if usage[dep.Cluster] == nil {
			usage[dep.Cluster] = make(map[string][]string)
		}
		if dep.Deployment != "" {
			usage[dep.Cluster][dep.Namespace] = append(usage[dep.Cluster][dep.Namespace], dep.Deployment)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"service_name": serviceName,
			"total_usages": len(results),
			"usages":       results,
			"by_cluster":   usage,
		},
	})
}

// ClearCache limpa o cache de scans em memória (não afeta SQLite)
// POST /api/v1/dependencies/cache/clear
func (h *DependenciesHandler) ClearCache(c *gin.Context) {
	h.scanner.ClearCache()

	log.Info().Msg("Dependency scan cache cleared")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Cache limpo com sucesso",
	})
}

// aggregateScanResults agrega resultados de múltiplos scans
func aggregateScanResults(results []*healthcheck.DependencyScanResult) gin.H {
	var allDeps []healthcheck.ExternalDependency
	uniqueServices := make(map[string]bool)
	byType := make(map[string]int)
	var totalDeployments, totalConfigMaps, totalSecrets int

	for _, r := range results {
		allDeps = append(allDeps, r.Dependencies...)

		for _, dep := range r.Dependencies {
			uniqueServices[dep.ServiceName] = true
			byType[string(dep.ServiceType)]++
		}

		totalDeployments += r.Stats.DeploymentsScanned
		totalConfigMaps += r.Stats.ConfigMapsScanned
		totalSecrets += r.Stats.SecretsScanned
	}

	// Agrupar por serviço
	grouped := groupDependenciesByService(allDeps)

	return gin.H{
		"dependencies":    allDeps,
		"by_service":      grouped,
		"total":           len(allDeps),
		"unique_services": len(uniqueServices),
		"by_type":         byType,
		"scanned": gin.H{
			"deployments": totalDeployments,
			"configmaps":  totalConfigMaps,
			"secrets":     totalSecrets,
		},
	}
}

// groupDependenciesByService agrupa dependências por nome de serviço
func groupDependenciesByService(deps []healthcheck.ExternalDependency) map[string][]healthcheck.ExternalDependency {
	grouped := make(map[string][]healthcheck.ExternalDependency)

	for _, dep := range deps {
		grouped[dep.ServiceName] = append(grouped[dep.ServiceName], dep)
	}

	return grouped
}

// groupRegistryByService agrupa registros de dependências por nome de serviço
func groupRegistryByService(deps []storage.DependencyRecord) map[string][]storage.DependencyRecord {
	grouped := make(map[string][]storage.DependencyRecord)

	for _, dep := range deps {
		grouped[dep.ServiceName] = append(grouped[dep.ServiceName], dep)
	}

	return grouped
}
