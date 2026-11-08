package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

// Varredura periódica de volumes: enquanto o servidor está ligado, lê os clusters marcados a cada
// scanInterval e guarda o que merece atenção (enchendo, ocioso antigo, sem snapshot). A tela mostra
// um selo no botão Volumes e um painel com a lista. Roda no processo do servidor, que se desliga
// sem aba aberta: não é um serviço 24x7.

const (
	scanInterval   = 30 * time.Minute
	scanFirstDelay = time.Minute
	scanTimeout    = 90 * time.Second
	scanMaxFinding = 200
	scanMaxWatched = 50
)

type ScanFinding struct {
	Kind      string `json:"kind"` // critical | idle_old | no_snapshot
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
	New       bool   `json:"new,omitempty"` // não estava na varredura anterior
}

type ScanCounts struct {
	Critical   int `json:"critical"`
	IdleOld    int `json:"idle_old"`
	NoSnapshot int `json:"no_snapshot"`
}

type ScanResult struct {
	Cluster      string        `json:"cluster"`
	ScannedAt    string        `json:"scanned_at"`
	Error        string        `json:"error,omitempty"`
	UsageWarning string        `json:"usage_warning,omitempty"`
	Counts       ScanCounts    `json:"counts"`
	NewCount     int           `json:"new_count"`
	Findings     []ScanFinding `json:"findings"`
	Truncated    bool          `json:"truncated,omitempty"`
}

// VolumeScanStorage é o que o handler precisa de storage.VolumeScanStore.
type VolumeScanStorage interface {
	SetWatched(clusters []string) error
	Watched() ([]string, error)
	SaveResult(cluster string, at time.Time, payload string) error
	Results() (map[string]storage.VolumeScanRow, error)
}

type volumeScanState struct {
	store   VolumeScanStorage
	running atomic.Bool
}

func (h *NodePoolHandler) SetVolumeScanStore(s VolumeScanStorage) { h.scan.store = s }

func findingKey(f ScanFinding) string { return f.Kind + "|" + f.Namespace + "|" + f.Name }

var scanKindOrder = map[string]int{"critical": 0, "idle_old": 1, "no_snapshot": 2}

// buildScanResult resume uma leitura de volumes em achados e marca os novos em relação a prev.
func buildScanResult(cluster string, data *volumesData, prev *ScanResult, now time.Time) *ScanResult {
	r := &ScanResult{Cluster: cluster, ScannedAt: now.UTC().Format(time.RFC3339), UsageWarning: data.UsageWarning, Findings: []ScanFinding{}}
	var all []ScanFinding
	for _, it := range data.Items {
		name := it.PVC
		if name == "" {
			name = it.PV
		}
		switch {
		case it.UsageClass == "critical":
			d := fmt.Sprintf("%.0f%% usado", it.UsagePercent)
			if it.DaysToFull != nil {
				d += fmt.Sprintf(" · enche em ~%d d", *it.DaysToFull)
			}
			all = append(all, ScanFinding{Kind: "critical", Namespace: it.Namespace, Name: name, Detail: d})
			r.Counts.Critical++
		case it.Idle && it.AgeDays != nil && *it.AgeDays > idleOldDays:
			d := fmt.Sprintf("ocioso, criado há %d d", *it.AgeDays)
			if it.IdleDays != nil {
				d += fmt.Sprintf(" · visto ocioso há %d d", *it.IdleDays)
			}
			all = append(all, ScanFinding{Kind: "idle_old", Namespace: it.Namespace, Name: name, Detail: d})
			r.Counts.IdleOld++
		case data.Summary.SnapshotsKnown && it.Kind == "pvc" && it.PVCStatus == "Bound" && !it.Idle && it.Snapshots == 0:
			all = append(all, ScanFinding{Kind: "no_snapshot", Namespace: it.Namespace, Name: name, Detail: "sem snapshot pronto"})
			r.Counts.NoSnapshot++
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return scanKindOrder[all[i].Kind] < scanKindOrder[all[j].Kind] })
	seen := map[string]bool{}
	if prev != nil {
		for _, f := range prev.Findings {
			seen[findingKey(f)] = true
		}
	}
	for i := range all {
		if prev != nil && !seen[findingKey(all[i])] {
			all[i].New = true
			if all[i].Kind != "no_snapshot" { // "sem snapshot" novo não é motivo de aviso
				r.NewCount++
			}
		}
	}
	if len(all) > scanMaxFinding {
		all, r.Truncated = all[:scanMaxFinding], true
	}
	r.Findings = append(r.Findings, all...)
	return r
}

func (h *NodePoolHandler) previousScan(cluster string) *ScanResult {
	rows, err := h.scan.store.Results()
	if err != nil {
		return nil
	}
	row, ok := rows[cluster]
	if !ok {
		return nil
	}
	var r ScanResult
	if json.Unmarshal([]byte(row.Payload), &r) != nil {
		return nil
	}
	return &r
}

func (h *NodePoolHandler) scanOne(ctx context.Context, cluster string) {
	cctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	prev := h.previousScan(cluster)
	now := time.Now()
	var res *ScanResult
	if data, verr := h.collectVolumes(cctx, cluster, true, false); verr != nil {
		res = &ScanResult{Cluster: cluster, ScannedAt: now.UTC().Format(time.RFC3339), Error: verr.Message, Findings: []ScanFinding{}}
		if prev != nil { // mantém o último estado conhecido junto do erro
			res.Counts, res.Findings, res.Truncated = prev.Counts, prev.Findings, prev.Truncated
		}
	} else {
		res = buildScanResult(cluster, data, prev, now)
	}
	if b, err := json.Marshal(res); err == nil {
		_ = h.scan.store.SaveResult(cluster, now, string(b))
	}
}

// runVolumeScan varre os clusters marcados (um por vez). Só uma varredura por vez.
func (h *NodePoolHandler) runVolumeScan(ctx context.Context) {
	if h.scan.store == nil || !h.scan.running.CompareAndSwap(false, true) {
		return
	}
	defer h.scan.running.Store(false)
	clusters, err := h.scan.store.Watched()
	if err != nil {
		return
	}
	for _, c := range clusters {
		if ctx.Err() != nil {
			return
		}
		h.scanOne(ctx, c)
	}
}

// StartVolumeScanner liga a varredura periódica até ctx terminar.
func (h *NodePoolHandler) StartVolumeScanner(ctx context.Context) {
	if h.scan.store == nil {
		return
	}
	go func() {
		t := time.NewTimer(scanFirstDelay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.runVolumeScan(ctx)
				t.Reset(scanInterval)
			}
		}
	}()
}

// GetVolumeScan devolve a lista marcada e o último resultado de cada cluster.
func (h *NodePoolHandler) GetVolumeScan(c *gin.Context) {
	if h.scan.store == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": false, "watched": []string{}, "results": []ScanResult{}}})
		return
	}
	watched, err := h.scan.store.Watched()
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "STORE_ERROR", err)
		return
	}
	rows, _ := h.scan.store.Results()
	results := []ScanResult{}
	for _, w := range watched {
		if row, ok := rows[w]; ok {
			var r ScanResult
			if json.Unmarshal([]byte(row.Payload), &r) == nil {
				results = append(results, r)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"enabled": true, "watched": watched, "results": results,
		"interval_minutes": int(scanInterval.Minutes()), "scanning": h.scan.running.Load(),
	}})
}

// SetVolumeScanWatch troca a lista de clusters varridos.
func (h *NodePoolHandler) SetVolumeScanWatch(c *gin.Context) {
	var req struct {
		Clusters []string `json:"clusters"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Clusters) > scanMaxWatched || h.scan.store == nil {
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("lista inválida (máximo %d clusters)", scanMaxWatched))
		return
	}
	for _, name := range req.Clusters {
		if len(name) > 300 {
			volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("nome de cluster inválido"))
			return
		}
	}
	if err := h.scan.store.SetWatched(req.Clusters); err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "STORE_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RunVolumeScan dispara uma varredura agora, em segundo plano.
func (h *NodePoolHandler) RunVolumeScan(c *gin.Context) {
	if h.scan.store == nil {
		volumeActionFail(c, http.StatusServiceUnavailable, "SCAN_DISABLED", fmt.Errorf("a varredura não está ligada neste servidor"))
		return
	}
	if h.scan.running.Load() {
		c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"scanning": true}})
		return
	}
	go h.runVolumeScan(context.Background())
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"scanning": true}})
}
