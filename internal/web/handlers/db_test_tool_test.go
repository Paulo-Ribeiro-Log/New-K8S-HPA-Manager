package handlers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveDBCredentials_Manual(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	auth := &DBAuthConfig{Username: "u", Password: "p"}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "u" || password != "p" {
		t.Errorf("got (%q, %q), want (u, p)", username, password)
	}
}

func TestResolveDBCredentials_SecretRaw(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("meu-usuario"),
			"password": []byte("minha-senha"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password"},
	}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "meu-usuario" || password != "minha-senha" {
		t.Errorf("got (%q, %q), want (meu-usuario, minha-senha)", username, password)
	}
}

func TestResolveDBCredentials_SecretBase64Decode(t *testing.T) {
	// "meu-usuario" e "minha-senha" em base64 — simula valor sincronizado via AKV já
	// codificado em base64 (além do base64 "de transporte" do próprio Secret, que o
	// client-go já decodifica antes de chegar em secret.Data).
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("bWV1LXVzdWFyaW8="),
			"password": []byte("bWluaGEtc2VuaGE="),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password", Base64Decode: true},
	}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "meu-usuario" || password != "minha-senha" {
		t.Errorf("got (%q, %q), want (meu-usuario, minha-senha)", username, password)
	}
}

func TestResolveDBCredentials_SecretBase64DecodeInvalid(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("não é base64 válido!!"),
			"password": []byte("minha-senha"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password", Base64Decode: true},
	}

	_, _, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err == nil {
		t.Fatal("esperava erro pra valor não-base64 com Base64Decode marcado, veio nil")
	}
}

func TestResolveDBCredentials_SecretKeyMissing(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("u"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "senha"},
	}

	_, _, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err == nil {
		t.Fatal("esperava erro de chave ausente, veio nil")
	}
}
