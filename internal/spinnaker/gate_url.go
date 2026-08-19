package spinnaker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// gateHostRe extrai `var gateHost = 'https://...';` do settings.js do Deck — mesmo mecanismo
// usado pra descobrir spinnaker-hlg-api.viavarejo.com.br/spinnaker-prd-api.viavarejo.com.br
// durante a investigação (ver seção 1/10.1 do plano). Deliberado: guardar só a URL do Deck na
// configuração do usuário e resolver o Gate em runtime, em vez de hardcodear/adivinhar o
// sufixo "-api" — different instalações Spinnaker podem nomear o Gate de forma diferente.
var gateHostRe = regexp.MustCompile(`var\s+gateHost\s*=\s*'([^']+)'`)

type gateURLCacheEntry struct {
	gateURL    string
	resolvedAt time.Time
}

var (
	gateURLCacheMu sync.Mutex
	gateURLCache   = make(map[string]gateURLCacheEntry)
)

// gateURLCacheTTL — o gateHost não muda em runtime; 24h é conservador o suficiente pra nunca
// precisar reiniciar o servidor pra pegar uma mudança de config do Halyard, sem bater no Deck
// a cada chamada.
const gateURLCacheTTL = 24 * time.Hour

// ResolveGateURL descobre a URL do Gate a partir da URL do Deck (settings.js), com cache em
// memória. deckBaseURL pode vir com ou sem "/" final.
func ResolveGateURL(ctx context.Context, httpClient *http.Client, deckBaseURL string) (string, error) {
	deckBaseURL = strings.TrimRight(deckBaseURL, "/")
	if deckBaseURL == "" {
		return "", fmt.Errorf("URL do Deck vazia")
	}

	gateURLCacheMu.Lock()
	if entry, ok := gateURLCache[deckBaseURL]; ok && time.Since(entry.resolvedAt) < gateURLCacheTTL {
		gateURLCacheMu.Unlock()
		return entry.gateURL, nil
	}
	gateURLCacheMu.Unlock()

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deckBaseURL+"/settings.js", nil)
	if err != nil {
		return "", fmt.Errorf("montar requisição pro settings.js: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("buscar settings.js de %s: %w", deckBaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("settings.js de %s retornou status %d", deckBaseURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB é bem mais que suficiente
	if err != nil {
		return "", fmt.Errorf("ler settings.js: %w", err)
	}

	match := gateHostRe.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("settings.js de %s não contém 'var gateHost = ...' — formato inesperado", deckBaseURL)
	}
	gateURL := strings.TrimRight(string(match[1]), "/")
	if gateURL == "" {
		return "", fmt.Errorf("gateHost vazio no settings.js de %s", deckBaseURL)
	}

	gateURLCacheMu.Lock()
	gateURLCache[deckBaseURL] = gateURLCacheEntry{gateURL: gateURL, resolvedAt: time.Now()}
	gateURLCacheMu.Unlock()

	return gateURL, nil
}
