package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// SKUPricingCoverage é o custo amortizado que um SKU tem sob reserva ou Savings Plan numa subscription
// (janela consultada ao Cost Management). Alimenta o índice "SKUs cobertos", usado pra marcar e
// oferecer alternativas de troca de tier. Snapshot por subscription: cada atualização substitui a
// anterior.
type SKUPricingCoverage struct {
	SubscriptionID string    `json:"subscription_id"`
	SKU            string    `json:"sku"`
	Model          string    `json:"model"` // reservation | savingsplan
	Cost           float64   `json:"cost"`
	Currency       string    `json:"currency,omitempty"`
	WindowDays     int       `json:"window_days"`
	FetchedAt      time.Time `json:"fetched_at"`
}

const finopsSKUCoverageSchema = `
CREATE TABLE IF NOT EXISTS sku_pricing_coverage (
    subscription_id TEXT NOT NULL,
    sku             TEXT NOT NULL,
    model           TEXT NOT NULL,
    cost            REAL,
    currency        TEXT,
    window_days     INTEGER,
    fetched_at      DATETIME NOT NULL,
    PRIMARY KEY (subscription_id, sku, model)
);
CREATE TABLE IF NOT EXISTS sku_pricing_coverage_meta (
    subscription_id TEXT PRIMARY KEY,
    fetched_at      DATETIME NOT NULL
);
`

// ReplaceSKUPricingCoverage substitui o retrato da subscription. A hora da consulta vai numa tabela
// própria (meta): uma subscription sem NENHUM SKU coberto continua registrada como "consultada", pra o
// TTL não repetir a chamada a cada scan.
func (s *FinOpsRightsizingStore) ReplaceSKUPricingCoverage(subscriptionID string, rows []SKUPricingCoverage, fetchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM sku_pricing_coverage WHERE subscription_id = ?`, subscriptionID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO sku_pricing_coverage (subscription_id, sku, model, cost, currency, window_days, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(subscriptionID, r.SKU, r.Model, r.Cost, r.Currency, r.WindowDays, fetchedAt); err != nil {
			return fmt.Errorf("gravar cobertura de SKU %s/%s: %w", subscriptionID, r.SKU, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO sku_pricing_coverage_meta (subscription_id, fetched_at) VALUES (?, ?)
ON CONFLICT(subscription_id) DO UPDATE SET fetched_at = excluded.fetched_at`, subscriptionID, fetchedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSKUPricingCoverage devolve o índice de TODAS as subscriptions consultadas (reservas de escopo
// compartilhado valem entre subscriptions, então o índice é da frota, não de uma subscription só).
func (s *FinOpsRightsizingStore) ListSKUPricingCoverage() ([]SKUPricingCoverage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT subscription_id, sku, model, COALESCE(cost,0), COALESCE(currency,''), COALESCE(window_days,0), fetched_at
FROM sku_pricing_coverage ORDER BY sku, model, subscription_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SKUPricingCoverage{}
	for rows.Next() {
		var r SKUPricingCoverage
		if err := rows.Scan(&r.SubscriptionID, &r.SKU, &r.Model, &r.Cost, &r.Currency, &r.WindowDays, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SKUPricingCoverageFetchedAt devolve quando a subscription foi consultada pela última vez (zero =
// nunca), para o TTL do refresh automático.
func (s *FinOpsRightsizingStore) SKUPricingCoverageFetchedAt(subscriptionID string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var t time.Time
	err := s.db.QueryRow(`SELECT fetched_at FROM sku_pricing_coverage_meta WHERE subscription_id = ?`, subscriptionID).Scan(&t)
	if err == sql.ErrNoRows || err != nil {
		return time.Time{}
	}
	return t
}
