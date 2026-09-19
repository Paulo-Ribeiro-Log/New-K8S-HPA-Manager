package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

const (
	unattachedDisksTimeout = 90 * time.Second
	// aggregatedList pagina por número de discos, não de zonas. 500 é o máximo aceito pela API.
	disksPageSize = 500
	// Teto de segurança contra loop infinito de paginação (500 × 100 = 50 mil discos).
	disksMaxPages = 100
)

// gcpDisk cobre os campos usados de compute.disks (a resposta do `gcloud compute disks list
// --format=json` tem exatamente o mesmo formato da REST API, por isso um parser só serve aos dois).
type gcpDisk struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	SizeGb                string            `json:"sizeGb"`                // a API serializa int64 como string
	ProvisionedIops       string            `json:"provisionedIops"`       // Hyperdisk (int64 como string)
	ProvisionedThroughput string            `json:"provisionedThroughput"` // Hyperdisk, MiB/s (int64 como string)
	Type                  string            `json:"type"`                  // URL .../diskTypes/pd-ssd
	Status                string            `json:"status"`
	Users                 []string          `json:"users"`
	CreationTimestamp     string            `json:"creationTimestamp"`
	LastDetachTimestamp   string            `json:"lastDetachTimestamp"`
	Labels                map[string]string `json:"labels"`
	Description           string            `json:"description"`
	Zone                  string            `json:"zone"`   // URL — vazio em disco regional
	Region                string            `json:"region"` // URL — só em disco regional
	SelfLink              string            `json:"selfLink"`
}

// ListUnattachedDisks lista os Persistent Disks sem nenhum usuário (users vazio) de um projeto.
//
// Primeiro tenta a Compute REST API com o token da própria app (mesmo GetFreshGKEToken usado pelo
// resto do pacote — não depende da sessão `gcloud auth login` local, que expira sozinha); só cai
// no gcloud CLI se não houver token ou a chamada REST falhar.
func ListUnattachedDisks(ctx context.Context, projectID string) ([]models.UnattachedDisk, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("projeto GCP do cluster não identificado — rode o autodiscover (gke-clusters-config.json sem projectId)")
	}

	listCtx, cancel := context.WithTimeout(ctx, unattachedDisksTimeout)
	defer cancel()

	var apiErr error
	if token := GetFreshGKEToken(listCtx); token != "" {
		disks, err := listDisksViaAPI(listCtx, projectID, token)
		if err == nil {
			return unattachedFromDisks(disks), nil
		}
		apiErr = err
	}

	disks, cliErr := listDisksViaCLI(listCtx, projectID)
	if cliErr != nil {
		if apiErr != nil {
			return nil, fmt.Errorf("Compute API: %v; gcloud CLI: %w", apiErr, cliErr)
		}
		return nil, cliErr
	}
	return unattachedFromDisks(disks), nil
}

func listDisksViaAPI(ctx context.Context, projectID, token string) ([]gcpDisk, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var all []gcpDisk
	pageToken := ""

	for page := 0; page < disksMaxPages; page++ {
		q := url.Values{}
		q.Set("maxResults", strconv.Itoa(disksPageSize))
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		endpoint := fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/aggregated/disks?%s",
			url.PathEscape(projectID), q.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("compute API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		disks, next, err := parseAggregatedDisks(body)
		if err != nil {
			return nil, err
		}
		all = append(all, disks...)
		if next == "" {
			return all, nil
		}
		pageToken = next
	}
	return nil, fmt.Errorf("compute API: mais de %d páginas de discos — abortado", disksMaxPages)
}

// parseAggregatedDisks extrai os discos de uma página de aggregatedList: items é um mapa
// "zones/<zona>" | "regions/<região>" → {disks: [...]} (zonas sem disco trazem só um warning).
func parseAggregatedDisks(body []byte) (disks []gcpDisk, nextPageToken string, err error) {
	var resp struct {
		Items map[string]struct {
			Disks []gcpDisk `json:"disks"`
		} `json:"items"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse aggregated disks: %w", err)
	}
	for _, scope := range resp.Items {
		disks = append(disks, scope.Disks...)
	}
	return disks, resp.NextPageToken, nil
}

func listDisksViaCLI(ctx context.Context, projectID string) ([]gcpDisk, error) {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, fmt.Errorf("sem token GCP da app e gcloud CLI não encontrado — autentique via Auto-Descobrir Clusters ou instale o Google Cloud SDK")
	}
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "list",
		"--project", projectID, "--format=json")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gcloud compute disks list: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("gcloud compute disks list: %w", err)
	}
	var disks []gcpDisk
	if err := json.Unmarshal(out, &disks); err != nil {
		return nil, fmt.Errorf("parse gcloud compute disks list: %w", err)
	}
	return disks, nil
}

// unattachedFromDisks mantém só os discos sem nenhum usuário (VM). Disco regional ou multi-writer
// pode ter vários users; qualquer um presente significa "em uso".
func unattachedFromDisks(disks []gcpDisk) []models.UnattachedDisk {
	out := make([]models.UnattachedDisk, 0)
	for _, d := range disks {
		if len(d.Users) > 0 {
			continue
		}
		out = append(out, gcpDiskToModel(d))
	}
	return out
}

// lastSegment devolve o trecho depois da última "/" de uma URL de recurso GCP.
func lastSegment(u string) string {
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return u
}

// regionFromZone: "southamerica-east1-a" → "southamerica-east1".
func regionFromZone(zone string) string {
	if i := strings.LastIndex(zone, "-"); i > 0 && len(zone)-i-1 == 1 {
		return zone[:i]
	}
	return zone
}

func gcpDiskToModel(d gcpDisk) models.UnattachedDisk {
	zone := lastSegment(d.Zone)
	location := regionFromZone(zone)
	if zone == "" {
		location = lastSegment(d.Region) // disco regional
	}
	sizeGB, _ := strconv.ParseFloat(d.SizeGb, 64)
	iops, _ := strconv.ParseFloat(d.ProvisionedIops, 64)
	mbps, _ := strconv.ParseFloat(d.ProvisionedThroughput, 64)

	id := d.SelfLink
	if id == "" {
		id = d.Name
	}
	disk := models.UnattachedDisk{
		Provider:        "gcp",
		ID:              id,
		Name:            d.Name,
		Location:        location,
		Zone:            zone,
		SizeGB:          sizeGB,
		ProvisionedIOPS: iops,
		ProvisionedMBps: mbps,
		DiskType:        lastSegment(d.Type),
		DiskState:       d.Status,
		CreatedAt:       d.CreationTimestamp,
		UnattachedSince: d.LastDetachTimestamp,
		Tags:            d.Labels,
	}
	fillGCPK8sOrigin(&disk, d)
	return disk
}

// fillGCPK8sOrigin procura a origem K8s em dois lugares, porque depende do driver: o in-tree
// (kubernetes.io/gce-pd) grava um JSON em `description`; o CSI (pd.csi.storage.gke.io) usa labels
// (que não aceitam "/" nem "." — daí o "_" no lugar).
func fillGCPK8sOrigin(disk *models.UnattachedDisk, d gcpDisk) {
	if desc := strings.TrimSpace(d.Description); strings.HasPrefix(desc, "{") {
		var meta map[string]string
		if json.Unmarshal([]byte(desc), &meta) == nil {
			disk.K8sPVCName = meta["kubernetes.io/created-for/pvc/name"]
			disk.K8sPVCNamespace = meta["kubernetes.io/created-for/pvc/namespace"]
			disk.K8sPVName = meta["kubernetes.io/created-for/pv/name"]
		}
	}
	for k, v := range d.Labels {
		switch strings.ToLower(k) {
		case "kubernetes_io_created-for_pvc_name":
			disk.K8sPVCName = firstNonEmpty(disk.K8sPVCName, v)
		case "kubernetes_io_created-for_pvc_namespace":
			disk.K8sPVCNamespace = firstNonEmpty(disk.K8sPVCNamespace, v)
		case "kubernetes_io_created-for_pv_name":
			disk.K8sPVName = firstNonEmpty(disk.K8sPVName, v)
		}
	}
	// PDs provisionados pelo GKE in-tree se chamam "gke-<cluster>-<hash>-pvc-<uuid>" — o nome do
	// cluster (possivelmente truncado) vem embutido no nome do disco.
	if strings.HasPrefix(d.Name, "gke-") && strings.Contains(d.Name, "-pvc-") {
		disk.K8sClusterHint = d.Name
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
