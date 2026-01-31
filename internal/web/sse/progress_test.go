package sse

import (
	"testing"
	"time"
)

func TestNewReplayBuffer(t *testing.T) {
	rb := NewReplayBuffer(10)

	if rb.maxEvents != 10 {
		t.Errorf("maxEvents: esperado 10, obteve %d", rb.maxEvents)
	}
	if len(rb.events) != 0 {
		t.Errorf("events deve iniciar vazio, tem %d", len(rb.events))
	}
}

func TestReplayBufferAdd(t *testing.T) {
	rb := NewReplayBuffer(3)

	// Adicionar 3 eventos
	for i := 1; i <= 3; i++ {
		rb.Add(ProgressEvent{
			ID:      "test",
			Type:    "progress",
			Message: "Evento " + string(rune('0'+i)),
		})
	}

	events := rb.GetAll()
	if len(events) != 3 {
		t.Errorf("esperado 3 eventos, obteve %d", len(events))
	}
}

func TestReplayBufferFIFO(t *testing.T) {
	rb := NewReplayBuffer(2) // Buffer pequeno

	// Adicionar 3 eventos (vai remover o primeiro)
	rb.Add(ProgressEvent{ID: "1", Message: "Primeiro"})
	rb.Add(ProgressEvent{ID: "2", Message: "Segundo"})
	rb.Add(ProgressEvent{ID: "3", Message: "Terceiro"})

	events := rb.GetAll()
	if len(events) != 2 {
		t.Errorf("esperado 2 eventos (buffer cheio), obteve %d", len(events))
	}

	// Primeiro evento deve ter sido removido (FIFO)
	if events[0].Message != "Segundo" {
		t.Errorf("primeiro evento deveria ser 'Segundo', obteve '%s'", events[0].Message)
	}
	if events[1].Message != "Terceiro" {
		t.Errorf("segundo evento deveria ser 'Terceiro', obteve '%s'", events[1].Message)
	}
}

func TestReplayBufferClear(t *testing.T) {
	rb := NewReplayBuffer(10)

	rb.Add(ProgressEvent{ID: "1", Message: "Test"})
	rb.Add(ProgressEvent{ID: "2", Message: "Test2"})

	if len(rb.GetAll()) != 2 {
		t.Error("buffer deveria ter 2 eventos")
	}

	rb.Clear()

	if len(rb.GetAll()) != 0 {
		t.Error("buffer deveria estar vazio após Clear()")
	}
}

func TestReplayBufferGetAllReturnsCopy(t *testing.T) {
	rb := NewReplayBuffer(10)
	rb.Add(ProgressEvent{ID: "1", Message: "Original"})

	// Obter eventos e modificar
	events := rb.GetAll()
	events[0].Message = "Modificado"

	// Verificar que o original não foi alterado
	original := rb.GetAll()
	if original[0].Message != "Original" {
		t.Error("GetAll deve retornar cópia, não referência")
	}
}

func TestProgressTrackerReplayBuffer(t *testing.T) {
	pt := NewProgressTracker()

	sessionID := "test-session-123"
	event := ProgressEvent{
		ID:        sessionID,
		Type:      "progress",
		Message:   "Teste",
		Progress:  0.5,
		Timestamp: time.Now(),
	}

	// Enviar evento (cliente não conectado)
	pt.SendToClient(sessionID, event)

	// Buscar do replay buffer
	events := pt.GetReplayEvents(sessionID)
	if len(events) != 1 {
		t.Errorf("esperado 1 evento no buffer, obteve %d", len(events))
	}

	if events[0].Message != "Teste" {
		t.Errorf("evento deveria ter mensagem 'Teste', obteve '%s'", events[0].Message)
	}
}

func TestProgressTrackerClearReplayBuffer(t *testing.T) {
	pt := NewProgressTracker()

	sessionID := "test-clear-123"

	// Adicionar eventos
	pt.SendToClient(sessionID, ProgressEvent{ID: sessionID, Message: "1"})
	pt.SendToClient(sessionID, ProgressEvent{ID: sessionID, Message: "2"})

	if len(pt.GetReplayEvents(sessionID)) != 2 {
		t.Error("buffer deveria ter 2 eventos")
	}

	// Limpar
	pt.ClearReplayBuffer(sessionID)

	if len(pt.GetReplayEvents(sessionID)) != 0 {
		t.Error("buffer deveria estar vazio após limpeza")
	}
}

func TestProgressTrackerMultipleSessions(t *testing.T) {
	pt := NewProgressTracker()

	session1 := "session-1"
	session2 := "session-2"

	// Enviar eventos para sessões diferentes
	pt.SendToClient(session1, ProgressEvent{ID: session1, Message: "S1-E1"})
	pt.SendToClient(session1, ProgressEvent{ID: session1, Message: "S1-E2"})
	pt.SendToClient(session2, ProgressEvent{ID: session2, Message: "S2-E1"})

	// Verificar isolamento
	events1 := pt.GetReplayEvents(session1)
	events2 := pt.GetReplayEvents(session2)

	if len(events1) != 2 {
		t.Errorf("session1 deveria ter 2 eventos, tem %d", len(events1))
	}
	if len(events2) != 1 {
		t.Errorf("session2 deveria ter 1 evento, tem %d", len(events2))
	}

	// Limpar uma sessão não afeta a outra
	pt.ClearReplayBuffer(session1)

	if len(pt.GetReplayEvents(session1)) != 0 {
		t.Error("session1 deveria estar vazia")
	}
	if len(pt.GetReplayEvents(session2)) != 1 {
		t.Error("session2 não deveria ter sido afetada")
	}
}

func TestProgressTrackerReplayBufferWithConnectedClient(t *testing.T) {
	pt := NewProgressTracker()

	sessionID := "connected-client"

	// Criar e conectar cliente
	client := NewClient(sessionID)
	pt.AddClient(client)

	// Enviar evento
	event := ProgressEvent{ID: sessionID, Message: "Com cliente conectado"}
	pt.SendToClient(sessionID, event)

	// Evento deve estar tanto no canal do cliente quanto no buffer
	select {
	case received := <-client.Channel:
		if received.Message != "Com cliente conectado" {
			t.Error("cliente deveria ter recebido o evento")
		}
	default:
		t.Error("cliente deveria ter evento no canal")
	}

	// Buffer também deve ter o evento
	events := pt.GetReplayEvents(sessionID)
	if len(events) != 1 {
		t.Error("buffer também deveria ter o evento")
	}

	pt.RemoveClient(sessionID)
}
