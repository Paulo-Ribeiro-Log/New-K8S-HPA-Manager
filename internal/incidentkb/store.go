// Package incidentkb implementa uma base de conhecimento local de incidentes
// reais de cluster — sintoma, causa raiz e resolução aplicada — separada da
// documentação interna de desenvolvimento do projeto. Cada registro nasce de
// um diagnóstico (manual ou de IA) revisado e confirmado por um analista, e
// fica disponível para consulta em diagnósticos futuros.
package incidentkb

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Store persiste e busca Incidents como arquivos Markdown em baseDir.
type Store struct {
	baseDir string
}

// NewStore cria (se necessário) baseDir e retorna um Store pronto para uso.
func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório da base de conhecimento: %w", err)
	}
	return &Store{baseDir: baseDir}, nil
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "na"
	}
	return strings.ToLower(s)
}

func newID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Save grava um novo incidente. Preenche ID e CreatedAt quando ausentes.
func (s *Store) Save(inc *Incident) (*Incident, error) {
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now()
	}
	if inc.ID == "" {
		id, err := newID()
		if err != nil {
			return nil, fmt.Errorf("erro ao gerar ID: %w", err)
		}
		inc.ID = id
	}

	name := fmt.Sprintf("%s_%s_%s_%s_%s.md",
		inc.CreatedAt.Format("2006-01-02"),
		slugify(inc.Cluster),
		slugify(inc.Namespace),
		slugify(inc.ResourceName),
		inc.ID,
	)

	data, err := toMarkdown(inc)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(s.baseDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("erro ao gravar incidente: %w", err)
	}

	return inc, nil
}

// List retorna todos os incidentes, mais recentes primeiro.
func (s *Store) List() ([]*Incident, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("erro ao listar base de conhecimento: %w", err)
	}

	incidents := make([]*Incident, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.baseDir, e.Name()))
		if err != nil {
			continue
		}
		inc, err := fromMarkdown(data)
		if err != nil {
			continue
		}
		incidents = append(incidents, inc)
	}

	sort.Slice(incidents, func(i, j int) bool {
		return incidents[i].CreatedAt.After(incidents[j].CreatedAt)
	})

	return incidents, nil
}

// GetByID retorna um incidente específico, ou nil se não encontrado.
func (s *Store) GetByID(id string) (*Incident, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, inc := range all {
		if inc.ID == id {
			return inc, nil
		}
	}
	return nil, nil
}

// Search faz busca textual simples (por palavra-chave) em sintoma, causa raiz,
// resolução e tags, com filtros opcionais de cluster/namespace/tipo de recurso.
// Sem vector DB — pontuação por contagem de ocorrências dos termos da query.
func (s *Store) Search(query string, filters SearchFilters) ([]SearchResult, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(query))

	results := make([]SearchResult, 0)
	for _, inc := range all {
		if filters.Cluster != "" && !strings.EqualFold(inc.Cluster, filters.Cluster) {
			continue
		}
		if filters.Namespace != "" && !strings.EqualFold(inc.Namespace, filters.Namespace) {
			continue
		}
		if filters.ResourceType != "" && !strings.EqualFold(inc.ResourceType, filters.ResourceType) {
			continue
		}

		score := scoreIncident(inc, terms)
		if len(terms) > 0 && score == 0 {
			continue
		}
		results = append(results, SearchResult{Incident: inc, Score: score})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Incident.CreatedAt.After(results[j].Incident.CreatedAt)
	})

	if filters.Limit > 0 && len(results) > filters.Limit {
		results = results[:filters.Limit]
	}

	return results, nil
}

func scoreIncident(inc *Incident, terms []string) int {
	haystack := strings.ToLower(strings.Join([]string{
		inc.Symptom, inc.RootCause, inc.Resolution,
		inc.ResourceType, inc.ResourceName,
		strings.Join(inc.Tags, " "),
	}, " "))

	score := 0
	for _, t := range terms {
		score += strings.Count(haystack, t)
	}
	return score
}
