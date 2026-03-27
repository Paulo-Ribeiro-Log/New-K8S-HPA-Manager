package finops

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	DefaultExchangeRate  = 5.50         // fallback quando API falha
	exchangeRateCacheTTL = 1 * time.Hour
	exchangeRateAPIURL   = "https://economia.awesomeapi.com.br/json/last/USD-BRL"
)

// ExchangeRateProvider busca e armazena em cache a cotação USD→BRL
type ExchangeRateProvider struct {
	mu      sync.RWMutex
	rate    float64
	date    string
	expires time.Time
}

// NewExchangeRateProvider cria um novo provider de cotação
func NewExchangeRateProvider() *ExchangeRateProvider {
	return &ExchangeRateProvider{}
}

// Get retorna a cotação atual USD→BRL e a data da cotação.
// Usa cache de 1h; retorna fallback (5.50) se a API estiver indisponível.
func (e *ExchangeRateProvider) Get() (rate float64, date string) {
	e.mu.RLock()
	if e.rate > 0 && time.Now().Before(e.expires) {
		rate, date = e.rate, e.date
		e.mu.RUnlock()
		return
	}
	e.mu.RUnlock()

	fetched, fetchedDate, err := fetchExchangeRate()
	if err != nil {
		log.Warn().Err(err).Float64("fallback", DefaultExchangeRate).
			Msg("Falha ao buscar cotação USD/BRL, usando fallback")
		return DefaultExchangeRate, time.Now().Format("2006-01-02")
	}

	e.mu.Lock()
	e.rate = fetched
	e.date = fetchedDate
	e.expires = time.Now().Add(exchangeRateCacheTTL)
	e.mu.Unlock()

	return fetched, fetchedDate
}

// fetchExchangeRate busca a cotação na API pública
func fetchExchangeRate() (float64, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(exchangeRateAPIURL)
	if err != nil {
		return 0, "", fmt.Errorf("requisição cotação: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("API cotação retornou status %d", resp.StatusCode)
	}

	var result map[string]struct {
		Bid        string `json:"bid"`
		CreateDate string `json:"create_date"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", fmt.Errorf("decodificar cotação: %w", err)
	}

	usdBRL, ok := result["USDBRL"]
	if !ok {
		return 0, "", fmt.Errorf("campo USDBRL não encontrado")
	}

	var rate float64
	if _, err := fmt.Sscanf(usdBRL.Bid, "%f", &rate); err != nil {
		return 0, "", fmt.Errorf("parsear cotação '%s': %w", usdBRL.Bid, err)
	}

	date := usdBRL.CreateDate
	if len(date) >= 10 {
		date = date[:10]
	}

	log.Info().Float64("rate", rate).Str("date", date).Msg("Cotação USD/BRL obtida")
	return rate, date, nil
}
