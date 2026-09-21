package storage

import (
	"fmt"
	"time"
)

// PoolPricingCoverage é o custo amortizado de UM modelo de preço (reserva, Savings Plan, sob demanda,
// spot) num node pool, na janela consultada ao Cost Management. Snapshot por cluster: uma nova
// atualização substitui a anterior (não acumula histórico). As participações (%) são calculadas na
// leitura por finops.BuildPoolCoverage — o pacote storage não importa finops.
type PoolPricingCoverage struct {
	Cluster    string    `json:"cluster"`
	NodePool   string    `json:"node_pool"`
	Model      string    `json:"model"`
	Cost       float64   `json:"cost"`
	Currency   string    `json:"currency,omitempty"`
	WindowDays int       `json:"window_days"`
	FetchedAt  time.Time `json:"fetched_at"`
}

const finopsPricingCoverageSchema = `
CREATE TABLE IF NOT EXISTS pool_pricing_coverage (
    cluster     TEXT NOT NULL,
    node_pool   TEXT NOT NULL,
    model       TEXT NOT NULL,
    cost        REAL,
    currency    TEXT,
    window_days INTEGER,
    fetched_at  DATETIME NOT NULL,
    PRIMARY KEY (cluster, node_pool, model)
);
`

// ReplacePoolPricingCoverage substitui (delete+insert numa transação) a cobertura do cluster: cada
// atualização é um retrato completo, e um pool que sumiu do cluster não deve ficar "preso".
func (s *FinOpsRightsizingStore) ReplacePoolPricingCoverage(cluster string, rows []PoolPricingCoverage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM pool_pricing_coverage WHERE cluster = ?`, cluster); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO pool_pricing_coverage (cluster, node_pool, model, cost, currency, window_days, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(cluster, r.NodePool, r.Model, r.Cost, r.Currency, r.WindowDays, r.FetchedAt); err != nil {
			return fmt.Errorf("gravar cobertura %s/%s/%s: %w", cluster, r.NodePool, r.Model, err)
		}
	}
	return tx.Commit()
}

// ListPoolPricingCoverage devolve a cobertura gravada do cluster (vazio = nunca consultada).
func (s *FinOpsRightsizingStore) ListPoolPricingCoverage(cluster string) ([]PoolPricingCoverage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT cluster, node_pool, model, COALESCE(cost,0), COALESCE(currency,''), COALESCE(window_days,0), fetched_at
FROM pool_pricing_coverage WHERE cluster = ? ORDER BY node_pool, model`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PoolPricingCoverage{}
	for rows.Next() {
		var r PoolPricingCoverage
		if err := rows.Scan(&r.Cluster, &r.NodePool, &r.Model, &r.Cost, &r.Currency, &r.WindowDays, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
