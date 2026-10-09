package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Catálogo de capacidades de SKUs de VM Azure por região (F3 da Deep Analysis): disco de SO efêmero,
// zonas, vCPUs por core (SMT) e restrições da assinatura. Vem da API Microsoft.Compute/skus via
// `az rest`, com o filtro de região aplicado no servidor (~5 s). `az vm list-skus` foi descartado: no
// brazilsouth não terminou em 8 minutos (filtra no cliente, depois de baixar o catálogo global). Cache
// em disco por região (7 dias) e atualização em segundo plano (uma por vez por região).

const (
	skuCatalogTTL     = 7 * 24 * time.Hour
	skuCatalogTimeout = 2 * time.Minute
)

// SKUCapabilities são as capacidades de uma SKU numa região.
type SKUCapabilities struct {
	Name            string   `json:"name"`
	VCPU            int      `json:"vcpu"`
	MemGB           float64  `json:"mem_gb"`
	VCPUsPerCore    int      `json:"vcpus_per_core"`
	EphemeralOSDisk bool     `json:"ephemeral_os_disk"`
	Zones           []string `json:"zones"`
	// Restricted: indisponível para a assinatura na região inteira.
	Restricted        bool   `json:"restricted"`
	RestrictionReason string `json:"restriction_reason,omitempty"`
}

// SKUCatalog é o catálogo de uma região.
type SKUCatalog struct {
	Region    string                     `json:"region"`
	FetchedAt time.Time                  `json:"fetched_at"`
	SKUs      map[string]SKUCapabilities `json:"skus"` // chave: nome em minúsculas
}

// Status do catálogo devolvido por SKUCatalogStore.Get.
const (
	SKUCatalogFresh       = "fresh"       // em cache e dentro do TTL
	SKUCatalogStale       = "stale"       // em cache, vencido (atualizando em segundo plano)
	SKUCatalogLoading     = "loading"     // sem cache, carregando em segundo plano
	SKUCatalogUnavailable = "unavailable" // sem az ou sem região
)

type azureSKUJSON struct {
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
	LocationInfo []struct {
		Location string   `json:"location"`
		Zones    []string `json:"zones"`
	} `json:"locationInfo"`
	Capabilities []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"capabilities"`
	Restrictions []struct {
		Type            string `json:"type"`
		ReasonCode      string `json:"reasonCode"`
		RestrictionInfo struct {
			Zones []string `json:"zones"`
		} `json:"restrictionInfo"`
	} `json:"restrictions"`
}

// ParseAzureSKUList interpreta o catálogo de SKUs da região — resposta da API ({"value": [...]}) ou
// lista pura (formato de `az vm list-skus`): só VMs Standard_*, zonas já sem as restritas para a
// assinatura.
func ParseAzureSKUList(raw []byte, region string) (map[string]SKUCapabilities, error) {
	var items []azureSKUJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		var page struct {
			Value []azureSKUJSON `json:"value"`
		}
		if err2 := json.Unmarshal(raw, &page); err2 != nil {
			return nil, fmt.Errorf("catálogo de SKUs ilegível: %w", err)
		}
		items = page.Value
	}
	out := make(map[string]SKUCapabilities, len(items))
	for _, it := range items {
		if !strings.EqualFold(it.ResourceType, "virtualMachines") || !strings.HasPrefix(it.Name, "Standard_") {
			continue
		}
		c := SKUCapabilities{Name: it.Name, Zones: []string{}}
		for _, cap := range it.Capabilities {
			switch cap.Name {
			case "vCPUs":
				c.VCPU, _ = strconv.Atoi(cap.Value)
			case "MemoryGB":
				c.MemGB, _ = strconv.ParseFloat(cap.Value, 64)
			case "vCPUsPerCore":
				c.VCPUsPerCore, _ = strconv.Atoi(cap.Value)
			case "EphemeralOSDiskSupported":
				c.EphemeralOSDisk = strings.EqualFold(cap.Value, "True")
			}
		}
		restrictedZones := map[string]bool{}
		for _, r := range it.Restrictions {
			switch r.Type {
			case "Location":
				c.Restricted, c.RestrictionReason = true, r.ReasonCode
			case "Zone":
				for _, z := range r.RestrictionInfo.Zones {
					restrictedZones[z] = true
				}
			}
		}
		for _, li := range it.LocationInfo {
			if !strings.EqualFold(li.Location, region) {
				continue
			}
			for _, z := range li.Zones {
				if !restrictedZones[z] {
					c.Zones = append(c.Zones, z)
				}
			}
		}
		sort.Strings(c.Zones)
		out[strings.ToLower(it.Name)] = c
	}
	return out, nil
}

// skuCatalogRunner busca o JSON bruto do catálogo de uma região (substituível nos testes).
type skuCatalogRunner func(ctx context.Context, region string) ([]byte, error)

func azListSKUs(ctx context.Context, region string) ([]byte, error) {
	if _, err := exec.LookPath("az"); err != nil {
		return nil, fmt.Errorf("az CLI não encontrado")
	}
	sub, err := azOutput(ctx, "account", "show", "--query", "id", "-o", "tsv")
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.Compute/skus?api-version=2021-07-01&$filter=%s",
		strings.TrimSpace(string(sub)), url.PathEscape("location eq '"+region+"'"))
	return azOutput(ctx, "rest", "--method", "get", "--url", endpoint, "-o", "json")
}

func azOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", append(args, "--only-show-errors")...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("az %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", args[0], err)
	}
	return out, nil
}

// SKUCatalogStore mantém o catálogo por região em memória + disco e o atualiza em segundo plano.
type SKUCatalogStore struct {
	dir    string
	runner skuCatalogRunner
	mu     sync.Mutex
	mem    map[string]*SKUCatalog
	// loading: canal fechado quando a atualização em curso da região termina.
	loading map[string]chan struct{}
	lastErr map[string]string
}

// NewSKUCatalogStore cria o store com cache em dir (vazio = ~/.k8s-hpa-manager).
func NewSKUCatalogStore(dir string) *SKUCatalogStore {
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager")
	}
	return &SKUCatalogStore{dir: dir, runner: azListSKUs, mem: map[string]*SKUCatalog{}, loading: map[string]chan struct{}{}, lastErr: map[string]string{}}
}

var regionRe = regexp.MustCompile(`^[a-z0-9]+$`)

func (s *SKUCatalogStore) file(region string) string {
	return filepath.Join(s.dir, "finops-sku-catalog-"+region+".json")
}

// Get devolve o catálogo da região (pode ser nil) e o status. Sem cache ou vencido, dispara a
// atualização em segundo plano e devolve o que houver. LastError traz o erro da última atualização.
func (s *SKUCatalogStore) Get(region string) (*SKUCatalog, string, string) {
	region = strings.ToLower(strings.TrimSpace(region))
	if !regionRe.MatchString(region) {
		return nil, SKUCatalogUnavailable, ""
	}
	s.mu.Lock()
	cat := s.mem[region]
	if cat == nil {
		if raw, err := os.ReadFile(s.file(region)); err == nil {
			var c SKUCatalog
			if json.Unmarshal(raw, &c) == nil && len(c.SKUs) > 0 {
				cat = &c
				s.mem[region] = cat
			}
		}
	}
	lastErr := s.lastErr[region]
	status := SKUCatalogFresh
	switch {
	case cat == nil:
		status = SKUCatalogLoading
	case time.Since(cat.FetchedAt) > skuCatalogTTL:
		status = SKUCatalogStale
	}
	if status != SKUCatalogFresh && s.loading[region] == nil {
		done := make(chan struct{})
		s.loading[region] = done
		go s.refresh(region, done)
	}
	s.mu.Unlock()
	return cat, status, lastErr
}

// GetWait é Get, mas sem catálogo em cache espera a carga terminar por até wait (a primeira carga
// leva segundos). Com cache vencido não espera: devolve o vencido e atualiza em segundo plano.
func (s *SKUCatalogStore) GetWait(region string, wait time.Duration) (*SKUCatalog, string, string) {
	cat, status, lastErr := s.Get(region)
	if status != SKUCatalogLoading || wait <= 0 {
		return cat, status, lastErr
	}
	s.mu.Lock()
	done := s.loading[strings.ToLower(strings.TrimSpace(region))]
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(wait):
		}
	}
	return s.Get(region)
}

// refresh atualiza o catálogo da região (contexto próprio: não depende da requisição que disparou).
func (s *SKUCatalogStore) refresh(region string, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), skuCatalogTimeout)
	defer cancel()
	defer close(done)
	start := time.Now()
	err := s.refreshSync(ctx, region)
	s.mu.Lock()
	delete(s.loading, region)
	if err != nil {
		s.lastErr[region] = err.Error()
	} else {
		delete(s.lastErr, region)
	}
	s.mu.Unlock()
	if err != nil {
		log.Warn().Err(err).Str("region", region).Msg("FinOps: falha ao atualizar catálogo de SKUs")
		return
	}
	log.Info().Str("region", region).Dur("elapsed", time.Since(start)).Msg("FinOps: catálogo de SKUs atualizado")
}

func (s *SKUCatalogStore) refreshSync(ctx context.Context, region string) error {
	raw, err := s.runner(ctx, region)
	if err != nil {
		return err
	}
	skus, err := ParseAzureSKUList(raw, region)
	if err != nil {
		return err
	}
	if len(skus) == 0 {
		return fmt.Errorf("nenhuma SKU de VM retornada para a região %s", region)
	}
	cat := &SKUCatalog{Region: region, FetchedAt: time.Now(), SKUs: skus}
	if data, err := json.Marshal(cat); err == nil {
		if mkErr := os.MkdirAll(s.dir, 0o755); mkErr == nil {
			tmp := s.file(region) + ".tmp"
			if os.WriteFile(tmp, data, 0o644) == nil {
				_ = os.Rename(tmp, s.file(region))
			}
		}
	}
	s.mu.Lock()
	s.mem[region] = cat
	s.mu.Unlock()
	return nil
}
