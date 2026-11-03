package appremoval

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const storeTTL = 2 * time.Hour

type stored struct {
	cluster string
	result  *Result
	at      time.Time
}

// Store guarda os scans em memória (nunca em disco) para que execução e verificação partam do
// que o servidor viu, não do que o cliente enviar. Entradas expiram em 2h.
type Store struct {
	mu  sync.Mutex
	m   map[string]stored
	now func() time.Time
}

func NewStore() *Store {
	return &Store{m: map[string]stored{}, now: time.Now}
}

// Put guarda o resultado, grava o ScanID nele e devolve o ID.
func (s *Store) Put(cluster string, r *Result) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.m {
		if now.Sub(v.at) > storeTTL {
			delete(s.m, k)
		}
	}
	r.ScanID = id
	s.m[id] = stored{cluster: cluster, result: r, at: now}
	return id
}

// Get devolve o cluster e o resultado, ou ok=false se não existe ou expirou.
func (s *Store) Get(id string) (cluster string, r *Result, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, found := s.m[id]
	if !found || s.now().Sub(v.at) > storeTTL {
		delete(s.m, id)
		return "", nil, false
	}
	return v.cluster, v.result, true
}
