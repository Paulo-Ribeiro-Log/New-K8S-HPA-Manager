package storage

import (
	"path/filepath"
	"testing"
)

func newTestNotesStore(t *testing.T) *NotesStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "notes-test.db")
	store, err := NewNotesStore(dbPath)
	if err != nil {
		t.Fatalf("NewNotesStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNotesStore_SaveListUpdateDelete(t *testing.T) {
	store := newTestNotesStore(t)

	id, err := store.Save(Note{
		Cluster:   "cluster-a",
		Tab:       "hpa",
		Content:   "primeira nota",
		UserEmail: "paulo@example.com",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == 0 {
		t.Fatalf("Save retornou id zero")
	}

	notes, err := store.List("cluster-a", "hpa")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("List retornou %d notas, esperava 1", len(notes))
	}
	if notes[0].Content != "primeira nota" || notes[0].UserEmail != "paulo@example.com" {
		t.Fatalf("nota retornada com dados incorretos: %+v", notes[0])
	}
	if notes[0].CreatedAt.IsZero() || notes[0].UpdatedAt.IsZero() {
		t.Fatalf("CreatedAt/UpdatedAt não deveriam ser zero: %+v", notes[0])
	}

	got, err := store.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != id {
		t.Fatalf("GetByID retornou id %d, esperava %d", got.ID, id)
	}

	if err := store.Update(id, "nota editada"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID pós-update: %v", err)
	}
	if got.Content != "nota editada" {
		t.Fatalf("Update não persistiu: content=%q", got.Content)
	}

	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	notes, err = store.List("cluster-a", "hpa")
	if err != nil {
		t.Fatalf("List pós-delete: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("List pós-delete retornou %d notas, esperava 0", len(notes))
	}
}

func TestNotesStore_SaveCreatesNewEntry_NeverOverwrites(t *testing.T) {
	store := newTestNotesStore(t)

	if _, err := store.Save(Note{Cluster: "c1", Tab: "dynatrace", Content: "nota 1", UserEmail: "a@x.com"}); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, err := store.Save(Note{Cluster: "c1", Tab: "dynatrace", Content: "nota 2", UserEmail: "a@x.com"}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	notes, err := store.List("c1", "dynatrace")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("esperava 2 entradas (diário, nunca sobrescreve), obteve %d", len(notes))
	}
	// Mais recente primeiro.
	if notes[0].Content != "nota 2" || notes[1].Content != "nota 1" {
		t.Fatalf("ordem inesperada: %+v", notes)
	}
}

func TestNotesStore_List_IsolatedByClusterAndTab(t *testing.T) {
	store := newTestNotesStore(t)

	mustSave := func(cluster, tab, content string) {
		t.Helper()
		if _, err := store.Save(Note{Cluster: cluster, Tab: tab, Content: content, UserEmail: "a@x.com"}); err != nil {
			t.Fatalf("Save(%s,%s): %v", cluster, tab, err)
		}
	}

	mustSave("cluster-a", "hpa", "a/hpa")
	mustSave("cluster-a", "dynatrace", "a/dynatrace")
	mustSave("cluster-b", "hpa", "b/hpa")

	notes, err := store.List("cluster-a", "hpa")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 || notes[0].Content != "a/hpa" {
		t.Fatalf("isolamento por cluster+tab falhou: %+v", notes)
	}

	notes, err = store.List("cluster-a", "nodepools")
	if err != nil {
		t.Fatalf("List (escopo sem notas): %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("esperava lista vazia para escopo sem notas, obteve %d", len(notes))
	}
}
