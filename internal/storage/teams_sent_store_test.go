package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTeamsSentStore(t *testing.T) {
	s, err := NewTeamsSentStore(filepath.Join(t.TempDir(), "teams_sent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	b1 := TeamsSentBatch{ID: "b1", SentAt: now.Add(-48 * time.Hour), SentBy: "a@x", Preview: "Aviso 1", Total: 3, OK: 2}
	b2 := TeamsSentBatch{ID: "b2", SentAt: now, SentBy: "b@x", Preview: "Aviso 2", Total: 1, OK: 1}
	if err := s.SaveBatch(b1, []TeamsSentMessage{
		{ThreadID: "t1", DisplayName: "Ana", MessageID: "m1"},
		{ThreadID: "t2", DisplayName: "Bia", MessageID: "m2"},
		{ThreadID: "t3", DisplayName: "Falhou", MessageID: ""}, // sem message_id: não entra
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBatch(b2, []TeamsSentMessage{{ThreadID: "t1", DisplayName: "Ana", MessageID: "m9"}}); err != nil {
		t.Fatal(err)
	}

	bs, err := s.ListBatches(10)
	if err != nil || len(bs) != 2 || bs[0].ID != "b2" {
		t.Fatalf("lotes = %+v err=%v", bs, err)
	}
	msgs, _ := s.ListMessages("b1")
	if len(msgs) != 2 || msgs[0].DisplayName != "Ana" || msgs[0].State != TeamsSentStateSent {
		t.Fatalf("mensagens b1 = %+v", msgs)
	}

	// editar m1, apagar m2: contagens por estado
	if err := s.MarkState(TeamsSentStateEdited, [][2]string{{"t1", "m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkState(TeamsSentStateDeleted, [][2]string{{"t2", "m2"}}); err != nil {
		t.Fatal(err)
	}
	bs, _ = s.ListBatches(10)
	var got TeamsSentBatch
	for _, b := range bs {
		if b.ID == "b1" {
			got = b
		}
	}
	if got.Active != 1 || got.Edited != 1 || got.Deleted != 1 || got.OK != 2 {
		t.Errorf("contagens b1 = %+v", got)
	}
	// mesma thread em lote diferente não é afetada (a chave é thread+message_id)
	msgs2, _ := s.ListMessages("b2")
	if msgs2[0].State != TeamsSentStateSent {
		t.Errorf("b2 não deveria mudar: %+v", msgs2[0])
	}

	// retenção: só o b1 (48 h) sai
	n, err := s.PruneOlderThan(now.Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v", n, err)
	}
	if m, _ := s.ListMessages("b1"); len(m) != 0 {
		t.Errorf("mensagens do b1 deveriam sair com o lote: %+v", m)
	}
	if err := s.DeleteBatch("b2"); err != nil {
		t.Fatal(err)
	}
	if bs, _ := s.ListBatches(10); len(bs) != 0 {
		t.Errorf("sobrou lote: %+v", bs)
	}
}
