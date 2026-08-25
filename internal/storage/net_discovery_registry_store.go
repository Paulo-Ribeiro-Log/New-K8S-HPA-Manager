package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// NetDiscoveryIPCacheEntry é o cache-on-read do cross-reference K8s da Descoberta de Rede (Fase 4
// do IP-ROUTE-DISCOVERY-PLAN.md) — "esse IP é o node/pod/service X do cluster Y", enriquecimento
// BÔNUS (ver seção 3.8 do plano), nunca pré-requisito. Decisão delegada ao Claude Code pelo
// usuário: cache-on-read leve, nem live query pura nem scan periódico completo da frota — a
// tabela se popula sozinha de leitura em leitura, nunca por um scan de fundo dedicado.
type NetDiscoveryIPCacheEntry struct {
	IP        string
	Kind      string // "node" | "pod" | "service"
	Name      string // já formatado pra exibição — ex: "Deployment/checkout-api" quando Kind=pod
	Namespace string // vazio quando Kind=node
	Cluster   string
	CachedAt  time.Time
}

// NetDiscoveryRegistryStore persiste o cache num SQLite próprio (mesmo padrão de
// notes_store.go/cert_endpoints_store.go — WAL mode). O TTL (diferenciado por Kind: curto pra
// pod, longo pra node/service) é decidido pelo CHAMADOR (internal/web/handlers), não aqui — esta
// store só guarda o dado e devolve CachedAt, mesmo princípio de RegistryLastSeen no pacote
// spinnaker (freshness computada por quem consome, não pela camada de persistência).
type NetDiscoveryRegistryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const netDiscoveryRegistrySchema = `
CREATE TABLE IF NOT EXISTS net_discovery_ip_cache (
    ip         TEXT     PRIMARY KEY,
    kind       TEXT     NOT NULL,
    name       TEXT     NOT NULL,
    namespace  TEXT,
    cluster    TEXT     NOT NULL,
    cached_at  DATETIME NOT NULL
);
`

func NewNetDiscoveryRegistryStore(dbPath string) (*NetDiscoveryRegistryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório net-discovery-registry: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir net-discovery-registry.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping net-discovery-registry.db: %w", err)
	}
	if _, err := db.Exec(netDiscoveryRegistrySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema net-discovery-registry: %w", err)
	}
	return &NetDiscoveryRegistryStore{db: db}, nil
}

func (s *NetDiscoveryRegistryStore) Close() error {
	return s.db.Close()
}

// Get devolve a entrada em cache pra um IP, ou sql.ErrNoRows se nunca foi vista. Não filtra por
// TTL — o chamador decide se o CachedAt ainda é fresco o bastante (ver comentário do struct).
func (s *NetDiscoveryRegistryStore) Get(ip string) (*NetDiscoveryIPCacheEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var e NetDiscoveryIPCacheEntry
	var namespace sql.NullString
	err := s.db.QueryRow(
		`SELECT ip, kind, name, namespace, cluster, cached_at FROM net_discovery_ip_cache WHERE ip=?`,
		ip,
	).Scan(&e.IP, &e.Kind, &e.Name, &namespace, &e.Cluster, &e.CachedAt)
	if err != nil {
		return nil, err // inclui sql.ErrNoRows
	}
	e.Namespace = namespace.String
	return &e, nil
}

// Upsert grava/atualiza a entrada de um IP — "cache-on-read": chamado sempre que uma consulta ao
// vivo contra o K8s encontra uma correspondência, nunca por um scan de fundo dedicado.
func (s *NetDiscoveryRegistryStore) Upsert(e NetDiscoveryIPCacheEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO net_discovery_ip_cache (ip, kind, name, namespace, cluster, cached_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(ip) DO UPDATE SET kind=excluded.kind, name=excluded.name,
		     namespace=excluded.namespace, cluster=excluded.cluster, cached_at=excluded.cached_at`,
		e.IP, e.Kind, e.Name, e.Namespace, e.Cluster, e.CachedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert net_discovery_ip_cache: %w", err)
	}
	return nil
}
