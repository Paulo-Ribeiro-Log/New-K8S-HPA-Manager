package promwal

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// AnalysisTTL é por quanto tempo uma análise autoriza a limpeza (fase 3): dado velho não vale.
const AnalysisTTL = 10 * time.Minute

// Store guarda as análises recentes em memória, por analysis_id.
type Store struct {
	mu      sync.Mutex
	reports map[string]Report
}

func NewStore() *Store { return &Store{reports: map[string]Report{}} }

// Put atribui um ID ao relatório, guarda e descarta as análises expiradas.
func (s *Store) Put(r Report) Report {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	r.ID = hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.reports {
		if now().Sub(old.CreatedAt) > AnalysisTTL {
			delete(s.reports, id)
		}
	}
	s.reports[r.ID] = r
	return r
}

// Get devolve a análise se ela existe e ainda não expirou.
func (s *Store) Get(id string) (Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	if !ok || now().Sub(r.CreatedAt) > AnalysisTTL {
		delete(s.reports, id)
		return Report{}, false
	}
	return r, true
}

// Take devolve a análise e a consome (de forma atômica): cada análise autoriza uma única limpeza.
func (s *Store) Take(id string) (Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	delete(s.reports, id)
	if !ok || now().Sub(r.CreatedAt) > AnalysisTTL {
		return Report{}, false
	}
	return r, true
}
