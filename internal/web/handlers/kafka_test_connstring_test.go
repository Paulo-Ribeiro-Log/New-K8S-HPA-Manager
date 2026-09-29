package handlers

import (
	"context"
	"encoding/base64"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveKafkaAuthConnectionStringFromSecret(t *testing.T) {
	const cs = "Endpoint=sb://ns.servicebus.windows.net/;SharedAccessKeyName=k;SharedAccessKey=v="
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "evh"},
		Data: map[string][]byte{
			"plain": []byte(cs + "\n"),
			"b64":   []byte(base64.StdEncoding.EncodeToString([]byte(cs))),
			"ruim":  []byte("nao-e-connection-string"),
		},
	})

	t.Run("deriva broker e usa $ConnectionString", func(t *testing.T) {
		sasl := &KafkaSASLConfig{SecretRef: &KafkaSecretRef{Namespace: "app", Name: "evh", ConnectionStringKey: "plain"}}
		if _, err := normalizeKafkaConnectionString("", sasl); err != nil {
			t.Fatal(err)
		}
		broker, user, pass, flags, err := resolveKafkaAuth(context.Background(), clientset, "", sasl)
		if err != nil {
			t.Fatal(err)
		}
		if broker != "ns.servicebus.windows.net:9093" || user != "$ConnectionString" || pass != cs || len(flags) == 0 {
			t.Errorf("broker=%q user=%q pass=%q flags=%v", broker, user, pass, flags)
		}
	})

	t.Run("base64", func(t *testing.T) {
		sasl := &KafkaSASLConfig{SecretRef: &KafkaSecretRef{Namespace: "app", Name: "evh", ConnectionStringKey: "b64", Base64Decode: true}}
		_, _, pass, _, err := resolveKafkaAuth(context.Background(), clientset, "", sasl)
		if err != nil || pass != cs {
			t.Errorf("pass=%q err=%v", pass, err)
		}
	})

	t.Run("valor inválido", func(t *testing.T) {
		sasl := &KafkaSASLConfig{SecretRef: &KafkaSecretRef{Namespace: "app", Name: "evh", ConnectionStringKey: "ruim"}}
		if _, _, _, _, err := resolveKafkaAuth(context.Background(), clientset, "", sasl); err == nil {
			t.Error("esperava erro")
		}
	})

	t.Run("chave inexistente", func(t *testing.T) {
		sasl := &KafkaSASLConfig{SecretRef: &KafkaSecretRef{Namespace: "app", Name: "evh", ConnectionStringKey: "nada"}}
		if _, _, _, _, err := resolveKafkaAuth(context.Background(), clientset, "", sasl); err == nil {
			t.Error("esperava erro")
		}
	})
}
