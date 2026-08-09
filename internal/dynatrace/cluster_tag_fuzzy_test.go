package dynatrace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// entitiesResponse é o shape mínimo devolvido por /api/v2/entities usado pelos testes abaixo.
type fakeEntitiesResponse struct {
	Entities []Entity `json:"entities"`
}

// TestListEntitiesByClusterTagFuzzy_MatchesValueThatDoesNotMatchExactly cobre o cenário real: um
// SERVICE alimentado só por ingestão OTLP direta (sem OneAgent) tem a tag k8s.cluster.name com um
// alias "amigável" (ex: "eks-asaplog-prd") diferente do nome do context K8s usado internamente
// pelo app (ex: "asaplog-production") — o fallback fuzzy precisa achar essa entidade mesmo assim,
// via token distintivo ("asaplog") + token de ambiente ("prd").
func TestListEntitiesByClusterTagFuzzy_MatchesValueThatDoesNotMatchExactly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fakeEntitiesResponse{
			Entities: []Entity{
				{
					EntityID:    "SERVICE-1",
					DisplayName: "checkout-api",
					Type:        "SERVICE",
					Tags:        []Tag{{Key: "k8s.cluster.name", Value: "eks-asaplog-prd"}},
				},
				{
					// Mesmo produto ("asaplog"), ambiente DIFERENTE — nunca deve casar.
					EntityID:    "SERVICE-2",
					DisplayName: "checkout-api-hlg",
					Type:        "SERVICE",
					Tags:        []Tag{{Key: "k8s.cluster.name", Value: "eks-asaplog-hlg"}},
				},
				{
					// Produto totalmente diferente, mesmo ambiente — nunca deve casar.
					EntityID:    "SERVICE-3",
					DisplayName: "outro-produto",
					Type:        "SERVICE",
					Tags:        []Tag{{Key: "k8s.cluster.name", Value: "eks-outro-produto-prd"}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	stubs, err := client.listEntitiesByClusterTagFuzzy(context.Background(), "asaplog-production", "SERVICE")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(stubs) != 1 {
		t.Fatalf("esperava 1 entidade casada, got %d: %+v", len(stubs), stubs)
	}
	if stubs[0].EntityID.ID != "SERVICE-1" {
		t.Errorf("EntityID = %q, want %q", stubs[0].EntityID.ID, "SERVICE-1")
	}
}

// TestListEntitiesByClusterTagFuzzy_SemTokenDeAmbiente_NaoArrisca garante o mesmo comportamento
// conservador de fuzzyResolveEntityIDByName: sem token de ambiente reconhecível no nome original,
// não tenta casar nada (retorna vazio em vez de arriscar uma correlação errada).
func TestListEntitiesByClusterTagFuzzy_SemTokenDeAmbiente_NaoArrisca(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fakeEntitiesResponse{}) //nolint:errcheck
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	stubs, err := client.listEntitiesByClusterTagFuzzy(context.Background(), "cluster-sem-marcador-de-ambiente", "SERVICE")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(stubs) != 0 {
		t.Errorf("esperava 0 entidades (sem token de ambiente, não deve arriscar), got %d", len(stubs))
	}
	if called {
		t.Error("não deveria ter chamado a API — deve retornar cedo, sem gastar uma requisição")
	}
}

// TestListEntitiesByCluster_FallsThroughToFuzzyTag cobre a cadeia completa: quando os fallbacks 1
// (dt.host_group.id) e 2 (k8s.cluster.name exato) não acham nada, ListEntitiesByCluster cai pro
// fallback 3 fuzzy — e ainda assim encontra a entidade.
func TestListEntitiesByCluster_FallsThroughToFuzzyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selector := r.URL.Query().Get("entitySelector")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(selector, `type("KUBERNETES_CLUSTER")`):
			// Cluster classicFullStack: nunca cria KUBERNETES_CLUSTER.
			json.NewEncoder(w).Encode(fakeEntitiesResponse{}) //nolint:errcheck
		case strings.Contains(selector, `tag("dt.host_group.id:`):
			json.NewEncoder(w).Encode(fakeEntitiesResponse{}) //nolint:errcheck
		case strings.Contains(selector, `tag("k8s.cluster.name:`):
			// Fallback 2 (valor exato) não acha nada — o valor real da tag é "amigável", não bate.
			json.NewEncoder(w).Encode(fakeEntitiesResponse{}) //nolint:errcheck
		case strings.Contains(selector, `tag("k8s.cluster.name")`):
			// Fallback 3 (fuzzy): devolve a entidade com o alias "amigável".
			json.NewEncoder(w).Encode(fakeEntitiesResponse{ //nolint:errcheck
				Entities: []Entity{
					{
						EntityID:    "SERVICE-1",
						DisplayName: "checkout-api",
						Type:        "SERVICE",
						Tags:        []Tag{{Key: "k8s.cluster.name", Value: "eks-asaplog-prd"}},
					},
				},
			})
		default:
			t.Errorf("entitySelector inesperado: %s", selector)
			json.NewEncoder(w).Encode(fakeEntitiesResponse{}) //nolint:errcheck
		}
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	stubs, err := client.ListEntitiesByCluster(context.Background(), "asaplog-production", "SERVICE")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(stubs) != 1 || stubs[0].EntityID.ID != "SERVICE-1" {
		t.Fatalf("esperava 1 entidade (SERVICE-1) via fallback fuzzy, got %+v", stubs)
	}
}
