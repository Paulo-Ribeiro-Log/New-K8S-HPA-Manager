package storage

import (
	"fmt"
	"time"
)

// Custo efetivo (amortizado, Cost Management) dos discos do cluster na janela da cobertura:
// kind "os" = disco do SO dos nodes de um pool (cobrado no recurso do VMSS); kind "disk" = disco
// gerenciado avulso pelo nome (PVCs: pvc-<uid>). Gravado junto com a cobertura de reserva.
type DiskCost struct {
	Cluster    string
	Kind       string // os | disk
	Name       string // pool (os) ou nome do disco (disk), em minúsculas
	Cost       float64
	Currency   string
	WindowDays int
	FetchedAt  time.Time
}

const finopsDiskCostSchema = `
CREATE TABLE IF NOT EXISTS disk_cost (
    cluster     TEXT NOT NULL,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL,
    cost        REAL NOT NULL,
    currency    TEXT NOT NULL DEFAULT '',
    window_days INTEGER NOT NULL,
    fetched_at  DATETIME NOT NULL,
    PRIMARY KEY (cluster, kind, name)
);`

// ReplaceDiskCost troca o custo de discos gravado do cluster.
func (s *FinOpsRightsizingStore) ReplaceDiskCost(cluster string, rows []DiskCost) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(finopsDiskCostSchema); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM disk_cost WHERE cluster = ?`, cluster); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO disk_cost (cluster, kind, name, cost, currency, window_days, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			cluster, r.Kind, r.Name, r.Cost, r.Currency, r.WindowDays, r.FetchedAt); err != nil {
			return fmt.Errorf("gravar custo de disco %s/%s: %w", r.Kind, r.Name, err)
		}
	}
	return tx.Commit()
}

// ListDiskCost devolve o custo de discos gravado do cluster (vazio = nunca consultado).
func (s *FinOpsRightsizingStore) ListDiskCost(cluster string) ([]DiskCost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(finopsDiskCostSchema); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT cluster, kind, name, cost, currency, window_days, fetched_at FROM disk_cost WHERE cluster = ?`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiskCost
	for rows.Next() {
		var r DiskCost
		if err := rows.Scan(&r.Cluster, &r.Kind, &r.Name, &r.Cost, &r.Currency, &r.WindowDays, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
