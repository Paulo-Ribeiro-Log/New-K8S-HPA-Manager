package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestCertEndpointsStore(t *testing.T) *CertEndpointsStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cert-endpoints-test.db")
	store, err := NewCertEndpointsStore(dbPath)
	if err != nil {
		t.Fatalf("NewCertEndpointsStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCertEndpointsStore_CreateListUpdateDelete(t *testing.T) {
	store := newTestCertEndpointsStore(t)

	id, err := store.Create(CertEndpoint{
		Name:      "AD Datacenter SP",
		Host:      "ad.datacenter.local",
		Port:      443,
		CreatedBy: "paulo@example.com",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create retornou id zero")
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List retornou %d endpoints, esperava 1", len(list))
	}
	if list[0].Name != "AD Datacenter SP" || list[0].Port != 443 || !list[0].Enabled {
		t.Fatalf("endpoint retornado com dados incorretos: %+v", list[0])
	}
	if list[0].SNI != "" || list[0].GroupLabel != "" {
		t.Fatalf("esperava SNI/GroupLabel vazios, obtive %+v", list[0])
	}

	err = store.Update(id, CertEndpoint{
		Name:       "AD Datacenter SP (renomeado)",
		Host:       "ad.datacenter.local",
		Port:       8443,
		SNI:        "ad-internal.local",
		GroupLabel: "windows",
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	list, err = store.List()
	if err != nil {
		t.Fatalf("List após update: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List após update retornou %d, esperava 1", len(list))
	}
	got := list[0]
	if got.Name != "AD Datacenter SP (renomeado)" || got.Port != 8443 || got.SNI != "ad-internal.local" ||
		got.GroupLabel != "windows" || got.Enabled {
		t.Fatalf("endpoint após update incorreto: %+v", got)
	}

	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = store.List()
	if err != nil {
		t.Fatalf("List após delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List após delete retornou %d, esperava 0", len(list))
	}
}

func TestCertEndpointsStore_GetByID(t *testing.T) {
	store := newTestCertEndpointsStore(t)

	id, err := store.Create(CertEndpoint{Name: "svc", Host: "svc.local", Port: 8443, CreatedBy: "a@b.com"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "svc" || got.Host != "svc.local" || got.Port != 8443 {
		t.Fatalf("GetByID retornou dados incorretos: %+v", got)
	}

	if _, err := store.GetByID(999); err == nil {
		t.Fatal("esperava erro (sql.ErrNoRows) para id inexistente")
	}
}

func TestCertEndpointsStore_UpdateDeleteInexistente(t *testing.T) {
	store := newTestCertEndpointsStore(t)

	if err := store.Update(999, CertEndpoint{Name: "x", Host: "x", Port: 443}); err == nil {
		t.Fatal("esperava erro ao atualizar id inexistente")
	}
	if err := store.Delete(999); err == nil {
		t.Fatal("esperava erro ao excluir id inexistente")
	}
}

func TestCertEndpointsStore_RecordCheckAndLatest(t *testing.T) {
	store := newTestCertEndpointsStore(t)

	id, err := store.Create(CertEndpoint{Name: "svc", Host: "svc.local", Port: 443, CreatedBy: "a@b.com"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Sem checagem ainda.
	latest, err := store.GetLatestCheck(id)
	if err != nil {
		t.Fatalf("GetLatestCheck (vazio): %v", err)
	}
	if latest != nil {
		t.Fatalf("esperava nil antes de qualquer checagem, obtive %+v", latest)
	}

	notAfter := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	notBefore := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	_, err = store.RecordCheck(CertEndpointCheck{
		EndpointID:        id,
		Success:           true,
		Subject:           "svc.local",
		Issuer:            "Internal CA",
		SerialNumber:      "ABC123",
		NotBefore:         &notBefore,
		NotAfter:          &notAfter,
		DNSNames:          []string{"svc.local", "svc-alt.local"},
		ChainLength:       2,
		Status:            "valid",
		DaysRemaining:     60,
		TrustedByPublicCA: false,
	})
	if err != nil {
		t.Fatalf("RecordCheck: %v", err)
	}

	latest, err = store.GetLatestCheck(id)
	if err != nil {
		t.Fatalf("GetLatestCheck: %v", err)
	}
	if latest == nil {
		t.Fatal("esperava checagem não-nula após RecordCheck")
	}
	if latest.Subject != "svc.local" || latest.Status != "valid" || latest.DaysRemaining != 60 {
		t.Fatalf("checagem retornada incorreta: %+v", latest)
	}
	if len(latest.DNSNames) != 2 || latest.DNSNames[0] != "svc.local" {
		t.Fatalf("DNSNames incorretos: %v", latest.DNSNames)
	}
	if latest.NotAfter == nil || !latest.NotAfter.Equal(notAfter) {
		t.Fatalf("NotAfter incorreto: %+v (esperado %v)", latest.NotAfter, notAfter)
	}

	// ListWithLatestCheck deve trazer o mesmo resultado embutido.
	withStatus, err := store.ListWithLatestCheck()
	if err != nil {
		t.Fatalf("ListWithLatestCheck: %v", err)
	}
	if len(withStatus) != 1 || withStatus[0].LatestCheck == nil {
		t.Fatalf("ListWithLatestCheck incorreto: %+v", withStatus)
	}
	if withStatus[0].LatestCheck.Status != "valid" {
		t.Fatalf("LatestCheck.Status = %q, esperado valid", withStatus[0].LatestCheck.Status)
	}
}

func TestCertEndpointsStore_RecordCheckFalha(t *testing.T) {
	store := newTestCertEndpointsStore(t)
	id, _ := store.Create(CertEndpoint{Name: "svc", Host: "svc.local", Port: 443, CreatedBy: "a@b.com"})

	_, err := store.RecordCheck(CertEndpointCheck{
		EndpointID:   id,
		Success:      false,
		ErrorMessage: "connection refused",
	})
	if err != nil {
		t.Fatalf("RecordCheck (falha): %v", err)
	}

	latest, err := store.GetLatestCheck(id)
	if err != nil {
		t.Fatalf("GetLatestCheck: %v", err)
	}
	if latest == nil || latest.Success {
		t.Fatalf("esperava checagem com Success=false, obtive %+v", latest)
	}
	if latest.ErrorMessage != "connection refused" {
		t.Fatalf("ErrorMessage = %q, esperado 'connection refused'", latest.ErrorMessage)
	}
	if latest.NotAfter != nil {
		t.Fatalf("esperava NotAfter nil numa checagem falha, obtive %v", latest.NotAfter)
	}
}

func TestCertEndpointsStore_RecordCheckPodaHistorico(t *testing.T) {
	store := newTestCertEndpointsStore(t)
	id, _ := store.Create(CertEndpoint{Name: "svc", Host: "svc.local", Port: 443, CreatedBy: "a@b.com"})

	// Insere mais checagens que o limite de retenção — cada uma precisa de um checked_at
	// estritamente crescente pra ORDER BY checked_at DESC ser determinístico (RecordCheck usa
	// time.Now(), então uma pequena pausa evita timestamps idênticos no mesmo milissegundo).
	total := certEndpointChecksRetention + 5
	for i := 0; i < total; i++ {
		if _, err := store.RecordCheck(CertEndpointCheck{
			EndpointID: id,
			Success:    true,
			Status:     "valid",
		}); err != nil {
			t.Fatalf("RecordCheck #%d: %v", i, err)
		}
	}

	history, err := store.GetHistory(id, 1000)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != certEndpointChecksRetention {
		t.Fatalf("GetHistory retornou %d checagens, esperava %d (poda não funcionou)",
			len(history), certEndpointChecksRetention)
	}
}
