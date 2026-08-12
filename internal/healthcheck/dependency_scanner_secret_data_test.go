package healthcheck

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtractSecretDataEntries_DecodedTextValue(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "minha-secret"},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"testeteste": []byte("teste"),
		},
	}

	entries := extractSecretDataEntries("cluster-a", "ns-a", secret)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.DataKey != "testeteste" {
		t.Errorf("DataKey = %q, want %q", e.DataKey, "testeteste")
	}
	if e.IsBinary {
		t.Errorf("expected IsBinary=false for plain text value")
	}
	if e.ValueDecoded != "teste" {
		t.Errorf("ValueDecoded = %q, want %q", e.ValueDecoded, "teste")
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("teste"))
	if e.ValueBase64 != wantB64 {
		t.Errorf("ValueBase64 = %q, want %q", e.ValueBase64, wantB64)
	}
	if e.Truncated {
		t.Errorf("expected Truncated=false for short value")
	}
	if e.Cluster != "cluster-a" || e.Namespace != "ns-a" || e.ResourceName != "minha-secret" {
		t.Errorf("unexpected location fields: %+v", e)
	}
	if e.ResourceKind != "secret" {
		t.Errorf("ResourceKind = %q, want %q", e.ResourceKind, "secret")
	}
	if e.ResourceSubtype != string(corev1.SecretTypeOpaque) {
		t.Errorf("ResourceSubtype = %q, want %q", e.ResourceSubtype, corev1.SecretTypeOpaque)
	}
}

func TestExtractSecretDataEntries_BinaryValue(t *testing.T) {
	// Bytes que não formam UTF-8 válido (ex: início de uma sequência multi-byte sem continuação).
	binary := []byte{0xff, 0xfe, 0x00, 0x01, 0x02}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cert-secret"},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": binary,
		},
	}

	entries := extractSecretDataEntries("cluster-a", "ns-a", secret)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if !e.IsBinary {
		t.Errorf("expected IsBinary=true for invalid UTF-8 value")
	}
	if e.ValueDecoded != "" {
		t.Errorf("expected empty ValueDecoded for binary value, got %q", e.ValueDecoded)
	}
	wantB64 := base64.StdEncoding.EncodeToString(binary)
	if e.ValueBase64 != wantB64 {
		t.Errorf("ValueBase64 = %q, want %q", e.ValueBase64, wantB64)
	}
}

func TestExtractSecretDataEntries_Truncation(t *testing.T) {
	big := bytes.Repeat([]byte("a"), maxSecretValueIndexLen+500)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "big-secret"},
		Data: map[string][]byte{
			"blob": big,
		},
	}

	entries := extractSecretDataEntries("cluster-a", "ns-a", secret)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if !e.Truncated {
		t.Errorf("expected Truncated=true for value larger than maxSecretValueIndexLen")
	}
	if len(e.ValueDecoded) != maxSecretValueIndexLen {
		t.Errorf("ValueDecoded length = %d, want %d (capped)", len(e.ValueDecoded), maxSecretValueIndexLen)
	}
}

func TestExtractSecretDataEntries_MultipleKeys(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-secret"},
		Data: map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
			"key3": []byte("value3"),
		},
	}

	entries := extractSecretDataEntries("cluster-a", "ns-a", secret)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (one per data key), got %d", len(entries))
	}
}

func TestExtractSecretDataEntries_EmptyData(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-secret"},
		Data:       map[string][]byte{},
	}

	entries := extractSecretDataEntries("cluster-a", "ns-a", secret)
	if entries != nil {
		t.Errorf("expected nil entries for Secret with no Data, got %+v", entries)
	}
}

func TestExtractConfigMapDataEntries_PlainTextValue(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "minha-config"},
		Data: map[string]string{
			"app.properties": "teste",
		},
	}

	entries := extractConfigMapDataEntries("cluster-a", "ns-a", cm)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.ResourceKind != "configmap" {
		t.Errorf("ResourceKind = %q, want %q", e.ResourceKind, "configmap")
	}
	if e.ResourceName != "minha-config" {
		t.Errorf("ResourceName = %q, want %q", e.ResourceName, "minha-config")
	}
	if e.ResourceSubtype != "" {
		t.Errorf("expected empty ResourceSubtype for configmap, got %q", e.ResourceSubtype)
	}
	// ConfigMap.Data é sempre texto — nunca binário, mesmo que o conteúdo "pareça" binário.
	if e.IsBinary {
		t.Errorf("expected IsBinary=false for ConfigMap.Data entry")
	}
	if e.ValueDecoded != "teste" {
		t.Errorf("ValueDecoded = %q, want %q", e.ValueDecoded, "teste")
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("teste"))
	if e.ValueBase64 != wantB64 {
		t.Errorf("ValueBase64 = %q, want %q (recodificação sintética, ConfigMap.Data não é base64 no manifesto)", e.ValueBase64, wantB64)
	}
}

func TestExtractConfigMapDataEntries_BinaryDataValue(t *testing.T) {
	binary := []byte{0xff, 0xfe, 0x00, 0x01, 0x02}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "icone-config"},
		BinaryData: map[string][]byte{
			"icon.png": binary,
		},
	}

	entries := extractConfigMapDataEntries("cluster-a", "ns-a", cm)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if !e.IsBinary {
		t.Errorf("expected IsBinary=true for invalid UTF-8 BinaryData value")
	}
	if e.ValueDecoded != "" {
		t.Errorf("expected empty ValueDecoded for binary value, got %q", e.ValueDecoded)
	}
}

func TestExtractConfigMapDataEntries_DataAndBinaryDataCombined(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mixed-config"},
		Data: map[string]string{
			"config.yaml": "chave: valor",
		},
		BinaryData: map[string][]byte{
			"icon.png": {0xff, 0xd8, 0xff},
		},
	}

	entries := extractConfigMapDataEntries("cluster-a", "ns-a", cm)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (1 Data + 1 BinaryData), got %d: %+v", len(entries), entries)
	}
}

func TestExtractConfigMapDataEntries_Truncation(t *testing.T) {
	big := strings.Repeat("a", maxSecretValueIndexLen+500)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "big-config"},
		Data: map[string]string{
			"blob": big,
		},
	}

	entries := extractConfigMapDataEntries("cluster-a", "ns-a", cm)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if !e.Truncated {
		t.Errorf("expected Truncated=true for value larger than maxSecretValueIndexLen")
	}
	if len(e.ValueDecoded) != maxSecretValueIndexLen {
		t.Errorf("ValueDecoded length = %d, want %d (capped)", len(e.ValueDecoded), maxSecretValueIndexLen)
	}
}

func TestExtractConfigMapDataEntries_EmptyData(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-config"},
	}

	entries := extractConfigMapDataEntries("cluster-a", "ns-a", cm)
	if entries != nil {
		t.Errorf("expected nil entries for ConfigMap with no Data/BinaryData, got %+v", entries)
	}
}
