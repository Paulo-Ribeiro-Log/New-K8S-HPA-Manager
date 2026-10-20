package web

import (
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"

	"github.com/gin-gonic/gin"
)

// profilingEnabled: o profiling só existe com K8S_HPA_PPROF=1 (desligado por padrão, não há rota registrada).
func profilingEnabled() bool { return os.Getenv("K8S_HPA_PPROF") == "1" }

// registerProfilingRoutes expõe, atrás da mesma autenticação de /api/v1, o profiling do runtime do Go para investigar
// consumo de memória/CPU do servidor:
//
//	GET /api/v1/debug/runtime            resumo (heap, goroutines, GC) em JSON
//	GET /api/v1/debug/pprof/:name        heap, allocs, goroutine, block, mutex, threadcreate, profile (CPU, ?seconds=N), trace, cmdline
//
// Uso: curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/debug/pprof/heap -o heap.pb.gz && go tool pprof heap.pb.gz
func registerProfilingRoutes(api *gin.RouterGroup) {
	if !profilingEnabled() {
		return
	}
	api.GET("/debug/runtime", func(c *gin.Context) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		c.JSON(http.StatusOK, gin.H{
			"goroutines":        runtime.NumGoroutine(),
			"heap_alloc_mb":     m.HeapAlloc >> 20,
			"heap_inuse_mb":     m.HeapInuse >> 20,
			"heap_sys_mb":       m.HeapSys >> 20,
			"heap_objects":      m.HeapObjects,
			"stack_inuse_mb":    m.StackInuse >> 20,
			"sys_mb":            m.Sys >> 20,
			"num_gc":            m.NumGC,
			"gc_pause_total_ms": m.PauseTotalNs / 1e6,
			"num_cpu":           runtime.NumCPU(),
		})
	})
	api.GET("/debug/pprof/:name", func(c *gin.Context) {
		switch name := c.Param("name"); name {
		case "profile":
			pprof.Profile(c.Writer, c.Request)
		case "trace":
			pprof.Trace(c.Writer, c.Request)
		case "cmdline":
			pprof.Cmdline(c.Writer, c.Request)
		default:
			// heap, allocs, goroutine, block, mutex, threadcreate; nome desconhecido dá 404
			pprof.Handler(name).ServeHTTP(c.Writer, c.Request)
		}
	})
}
