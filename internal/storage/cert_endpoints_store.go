package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// certEndpointChecksRetention — quantas checagens ficam guardadas por endpoint. "Histórico
// leve", não uma série temporal completa (diferente de snat_history.db) — poda a cada inserção
// pra não crescer sem limite mesmo em endpoints checados com frequência.
const certEndpointChecksRetention = 20

// CertEndpoint representa um endpoint externo cadastrado livremente pelo usuário (host:porta
// fora de qualquer cluster K8s) pra ter seu certificado TLS monitorado — ver
// EXTERNAL-CERT-MONITOR-PLAN.md.
type CertEndpoint struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	SNI        string    `json:"sni,omitempty"`
	GroupLabel string    `json:"group_label,omitempty"`
	Enabled    bool      `json:"enabled"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// CertEndpointCheck é o resultado persistido de uma checagem (sucesso ou falha) contra um
// CertEndpoint — shape de storage, independente de internal/certificates.EndpointCheckResult; o
// handler converte um no outro (mesma separação já usada pelos demais stores desta app).
type CertEndpointCheck struct {
	ID           int64     `json:"id"`
	EndpointID   int64     `json:"endpoint_id"`
	CheckedAt    time.Time `json:"checked_at"`
	Success      bool      `json:"success"`
	ErrorMessage string    `json:"error_message,omitempty"`

	Subject      string     `json:"subject,omitempty"`
	Issuer       string     `json:"issuer,omitempty"`
	SerialNumber string     `json:"serial_number,omitempty"`
	NotBefore    *time.Time `json:"not_before,omitempty"`
	NotAfter     *time.Time `json:"not_after,omitempty"`
	DNSNames     []string   `json:"dns_names,omitempty"`
	ChainLength  int        `json:"chain_length,omitempty"`

	Status        string `json:"status,omitempty"`
	DaysRemaining int    `json:"days_remaining,omitempty"`

	TrustedByPublicCA bool `json:"trusted_by_public_ca"`
}

// CertEndpointWithStatus é o shape consumido pela listagem — endpoint + última checagem
// (LatestCheck nil quando o endpoint ainda nunca foi checado).
type CertEndpointWithStatus struct {
	CertEndpoint
	LatestCheck *CertEndpointCheck `json:"latest_check,omitempty"`
}

// CertEndpointsStore persiste a lista de endpoints externos e o histórico leve de checagens em
// um banco SQLite próprio.
type CertEndpointsStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const certEndpointsSchema = `
CREATE TABLE IF NOT EXISTS cert_endpoints (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT     NOT NULL,
    host        TEXT     NOT NULL,
    port        INTEGER  NOT NULL DEFAULT 443,
    sni         TEXT,
    group_label TEXT,
    enabled     INTEGER  NOT NULL DEFAULT 1,
    created_by  TEXT     NOT NULL,
    created_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS cert_endpoint_checks (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id           INTEGER  NOT NULL,
    checked_at            DATETIME NOT NULL,
    success               INTEGER  NOT NULL,
    error_message         TEXT,
    subject               TEXT,
    issuer                TEXT,
    serial_number         TEXT,
    not_before            DATETIME,
    not_after             DATETIME,
    dns_names             TEXT,
    chain_length          INTEGER,
    status                TEXT,
    days_remaining        INTEGER,
    trusted_by_public_ca  INTEGER
);

CREATE INDEX IF NOT EXISTS idx_cert_endpoint_checks_endpoint ON cert_endpoint_checks(endpoint_id, checked_at DESC);
`

func NewCertEndpointsStore(dbPath string) (*CertEndpointsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório cert-endpoints: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir cert-endpoints.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("ping cert-endpoints.db: %w", err)
	}
	if _, err := db.Exec(certEndpointsSchema); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("criar schema cert-endpoints: %w", err)
	}
	return &CertEndpointsStore{db: db}, nil
}

func (s *CertEndpointsStore) Close() error {
	return s.db.Close()
}

// Create insere um novo endpoint cadastrado. Enabled sempre nasce true — desabilitar é uma ação
// explícita separada (Update).
func (s *CertEndpointsStore) Create(e CertEndpoint) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`INSERT INTO cert_endpoints (name, host, port, sni, group_label, enabled, created_by, created_at)
		 VALUES (?,?,?,?,?,1,?,?)`,
		e.Name, e.Host, e.Port, nullableString(e.SNI), nullableString(e.GroupLabel), e.CreatedBy, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("inserir cert_endpoint: %w", err)
	}
	return res.LastInsertId()
}

// Update sobrescreve os campos editáveis de um endpoint existente (nunca created_by/created_at).
func (s *CertEndpointsStore) Update(id int64, e CertEndpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE cert_endpoints SET name=?, host=?, port=?, sni=?, group_label=?, enabled=? WHERE id=?`,
		e.Name, e.Host, e.Port, nullableString(e.SNI), nullableString(e.GroupLabel), e.Enabled, id,
	)
	if err != nil {
		return fmt.Errorf("atualizar cert_endpoint: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("atualizar cert_endpoint: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete remove um endpoint e todo o seu histórico de checagens. Sem FK/cascade no schema (não é
// um padrão usado em nenhum outro store desta app) — a limpeza do histórico é manual, numa
// transação pra não deixar checagens órfãs se o processo morrer no meio.
func (s *CertEndpointsStore) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("excluir cert_endpoint: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM cert_endpoint_checks WHERE endpoint_id=?`, id); err != nil {
		return fmt.Errorf("excluir histórico do cert_endpoint: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM cert_endpoints WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("excluir cert_endpoint: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("excluir cert_endpoint: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// List retorna todos os endpoints cadastrados, mais recente primeiro. Nunca retorna nil.
func (s *CertEndpointsStore) List() ([]CertEndpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, name, host, port, sni, group_label, enabled, created_by, created_at
		 FROM cert_endpoints ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar cert_endpoints: %w", err)
	}
	defer rows.Close()

	endpoints := []CertEndpoint{}
	for rows.Next() {
		e, err := scanCertEndpoint(rows)
		if err != nil {
			continue
		}
		endpoints = append(endpoints, e)
	}
	return endpoints, nil
}

// ListWithLatestCheck retorna todos os endpoints já com a checagem mais recente embutida — um
// único SELECT com subquery de correlação em vez de N+1 (List + GetLatestCheck por item), já que
// esse é o shape consumido diretamente pela listagem da UI.
func (s *CertEndpointsStore) ListWithLatestCheck() ([]CertEndpointWithStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT e.id, e.name, e.host, e.port, e.sni, e.group_label, e.enabled, e.created_by, e.created_at,
		       c.id, c.checked_at, c.success, c.error_message, c.subject, c.issuer, c.serial_number,
		       c.not_before, c.not_after, c.dns_names, c.chain_length, c.status, c.days_remaining,
		       c.trusted_by_public_ca
		FROM cert_endpoints e
		LEFT JOIN cert_endpoint_checks c ON c.id = (
			SELECT id FROM cert_endpoint_checks
			WHERE endpoint_id = e.id ORDER BY checked_at DESC LIMIT 1
		)
		ORDER BY e.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listar cert_endpoints com status: %w", err)
	}
	defer rows.Close()

	result := []CertEndpointWithStatus{}
	for rows.Next() {
		var e CertEndpoint
		var sni, groupLabel sql.NullString
		var checkID sql.NullInt64
		var checkedAt sql.NullTime
		var success sql.NullBool
		var errMsg, subject, issuer, serial, dnsNamesJSON, status sql.NullString
		var notBefore, notAfter sql.NullTime
		var chainLength, daysRemaining sql.NullInt64
		var trustedByPublicCA sql.NullBool

		if err := rows.Scan(
			&e.ID, &e.Name, &e.Host, &e.Port, &sni, &groupLabel, &e.Enabled, &e.CreatedBy, &e.CreatedAt,
			&checkID, &checkedAt, &success, &errMsg, &subject, &issuer, &serial,
			&notBefore, &notAfter, &dnsNamesJSON, &chainLength, &status, &daysRemaining,
			&trustedByPublicCA,
		); err != nil {
			continue
		}
		e.SNI = sni.String
		e.GroupLabel = groupLabel.String

		item := CertEndpointWithStatus{CertEndpoint: e}
		if checkID.Valid {
			check := CertEndpointCheck{
				ID:                checkID.Int64,
				EndpointID:        e.ID,
				CheckedAt:         checkedAt.Time,
				Success:           success.Bool,
				ErrorMessage:      errMsg.String,
				Subject:           subject.String,
				Issuer:            issuer.String,
				SerialNumber:      serial.String,
				ChainLength:       int(chainLength.Int64),
				Status:            status.String,
				DaysRemaining:     int(daysRemaining.Int64),
				TrustedByPublicCA: trustedByPublicCA.Bool,
			}
			if notBefore.Valid {
				check.NotBefore = &notBefore.Time
			}
			if notAfter.Valid {
				check.NotAfter = &notAfter.Time
			}
			if dnsNamesJSON.Valid && dnsNamesJSON.String != "" {
				_ = json.Unmarshal([]byte(dnsNamesJSON.String), &check.DNSNames)
			}
			item.LatestCheck = &check
		}
		result = append(result, item)
	}
	return result, nil
}

// RecordCheck insere o resultado de uma checagem e poda o histórico do endpoint pra manter só as
// últimas certEndpointChecksRetention entradas — feito na mesma transação da inserção, pra nunca
// deixar o histórico temporariamente maior que o limite entre chamadas concorrentes.
func (s *CertEndpointsStore) RecordCheck(check CertEndpointCheck) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("registrar checagem: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	dnsNamesJSON := ""
	if len(check.DNSNames) > 0 {
		b, err := json.Marshal(check.DNSNames)
		if err == nil {
			dnsNamesJSON = string(b)
		}
	}

	res, err := tx.Exec(
		`INSERT INTO cert_endpoint_checks
		 (endpoint_id, checked_at, success, error_message, subject, issuer, serial_number,
		  not_before, not_after, dns_names, chain_length, status, days_remaining, trusted_by_public_ca)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		check.EndpointID, time.Now().UTC(), check.Success, nullableString(check.ErrorMessage),
		nullableString(check.Subject), nullableString(check.Issuer), nullableString(check.SerialNumber),
		nullableTime(check.NotBefore), nullableTime(check.NotAfter), nullableString(dnsNamesJSON),
		check.ChainLength, nullableString(check.Status), check.DaysRemaining, check.TrustedByPublicCA,
	)
	if err != nil {
		return 0, fmt.Errorf("registrar checagem: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("registrar checagem: %w", err)
	}

	if _, err := tx.Exec(
		`DELETE FROM cert_endpoint_checks WHERE endpoint_id=? AND id NOT IN (
			SELECT id FROM cert_endpoint_checks WHERE endpoint_id=? ORDER BY checked_at DESC LIMIT ?
		)`,
		check.EndpointID, check.EndpointID, certEndpointChecksRetention,
	); err != nil {
		return 0, fmt.Errorf("podar histórico de checagens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("registrar checagem: %w", err)
	}
	return id, nil
}

// GetLatestCheck retorna a checagem mais recente de um endpoint, ou nil se ele nunca foi
// checado (sql.ErrNoRows não é tratado como erro aqui — "nunca checado" é um estado válido).
func (s *CertEndpointsStore) GetLatestCheck(endpointID int64) (*CertEndpointCheck, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, endpoint_id, checked_at, success, error_message, subject, issuer, serial_number,
		        not_before, not_after, dns_names, chain_length, status, days_remaining, trusted_by_public_ca
		 FROM cert_endpoint_checks WHERE endpoint_id=? ORDER BY checked_at DESC LIMIT 1`,
		endpointID,
	)
	if err != nil {
		return nil, fmt.Errorf("última checagem: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	c, err := scanCertEndpointCheck(rows)
	if err != nil {
		return nil, fmt.Errorf("última checagem: %w", err)
	}
	return &c, nil
}

// GetHistory retorna as últimas `limit` checagens de um endpoint, mais recente primeiro.
func (s *CertEndpointsStore) GetHistory(endpointID int64, limit int) ([]CertEndpointCheck, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, endpoint_id, checked_at, success, error_message, subject, issuer, serial_number,
		        not_before, not_after, dns_names, chain_length, status, days_remaining, trusted_by_public_ca
		 FROM cert_endpoint_checks WHERE endpoint_id=? ORDER BY checked_at DESC LIMIT ?`,
		endpointID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("histórico de checagens: %w", err)
	}
	defer rows.Close()

	checks := []CertEndpointCheck{}
	for rows.Next() {
		c, err := scanCertEndpointCheck(rows)
		if err != nil {
			continue
		}
		checks = append(checks, c)
	}
	return checks, nil
}

// scanCertEndpoint escaneia uma linha de cert_endpoints (rows.Scan aceita *sql.Rows diretamente,
// sem precisar de interface — mantido como função solta só pra não duplicar a lista de campos
// entre List e outros métodos que um dia listem endpoints filtrados).
func scanCertEndpoint(rows *sql.Rows) (CertEndpoint, error) {
	var e CertEndpoint
	var sni, groupLabel sql.NullString
	if err := rows.Scan(&e.ID, &e.Name, &e.Host, &e.Port, &sni, &groupLabel, &e.Enabled, &e.CreatedBy, &e.CreatedAt); err != nil {
		return CertEndpoint{}, err
	}
	e.SNI = sni.String
	e.GroupLabel = groupLabel.String
	return e, nil
}

func scanCertEndpointCheck(rows *sql.Rows) (CertEndpointCheck, error) {
	var c CertEndpointCheck
	var errMsg, subject, issuer, serial, dnsNamesJSON, status sql.NullString
	var notBefore, notAfter sql.NullTime

	if err := rows.Scan(
		&c.ID, &c.EndpointID, &c.CheckedAt, &c.Success, &errMsg, &subject, &issuer, &serial,
		&notBefore, &notAfter, &dnsNamesJSON, &c.ChainLength, &status, &c.DaysRemaining, &c.TrustedByPublicCA,
	); err != nil {
		return CertEndpointCheck{}, err
	}
	c.ErrorMessage = errMsg.String
	c.Subject = subject.String
	c.Issuer = issuer.String
	c.SerialNumber = serial.String
	c.Status = status.String
	if notBefore.Valid {
		c.NotBefore = &notBefore.Time
	}
	if notAfter.Valid {
		c.NotAfter = &notAfter.Time
	}
	if dnsNamesJSON.Valid && dnsNamesJSON.String != "" {
		_ = json.Unmarshal([]byte(dnsNamesJSON.String), &c.DNSNames)
	}
	return c, nil
}

// nullableString converte "" para NULL — evita gravar string vazia em colunas opcionais
// (sni/group_label/error_message/...), mantendo a distinção "nunca preenchido" (NULL) vs
// "preenchido com vazio" (não deveria acontecer, mas NULL é a representação mais correta).
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}
