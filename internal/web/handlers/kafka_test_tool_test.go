package handlers

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildKcatAuthFlags(t *testing.T) {
	t.Run("nil sasl", func(t *testing.T) {
		if got := buildKcatAuthFlags(nil, "", ""); got != nil {
			t.Errorf("expected nil for nil sasl, got %v", got)
		}
	})

	t.Run("plain with creds and tls (comportamento preservado)", func(t *testing.T) {
		sasl := &KafkaSASLConfig{Mechanism: kafkaSASLMechanismPlain, UseTLS: true}
		got := buildKcatAuthFlags(sasl, "alice", "secret")
		want := []string{
			"-X", "security.protocol=SASL_SSL",
			"-X", "sasl.mechanisms=PLAIN",
			"-X", "sasl.username=alice",
			"-X", "sasl.password=secret",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("oauthbearer monta flags OIDC, ignora username/password", func(t *testing.T) {
		sasl := &KafkaSASLConfig{
			Mechanism:             kafkaSASLMechanismOAuthBearer,
			UseTLS:                true,
			OAuthClientID:         "client-123",
			OAuthClientSecret:     "shh",
			OAuthTokenEndpointURL: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
			OAuthScope:            "https://ns.servicebus.windows.net/.default",
		}
		got := buildKcatAuthFlags(sasl, "", "")
		joined := strings.Join(got, " ")
		for _, want := range []string{
			"security.protocol=SASL_SSL",
			"sasl.mechanisms=OAUTHBEARER",
			"sasl.oauthbearer.method=oidc",
			"sasl.oauthbearer.client.id=client-123",
			"sasl.oauthbearer.client.secret=shh",
			"sasl.oauthbearer.token.endpoint.url=https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
			"sasl.oauthbearer.scope=https://ns.servicebus.windows.net/.default",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing flag %q in %v", want, got)
			}
		}
		if strings.Contains(joined, "sasl.username=") || strings.Contains(joined, "sasl.password=") {
			t.Errorf("oauthbearer não deveria montar sasl.username/sasl.password: %v", got)
		}
	})

	t.Run("oauthbearer sem scope omite o flag", func(t *testing.T) {
		sasl := &KafkaSASLConfig{
			Mechanism:             kafkaSASLMechanismOAuthBearer,
			UseTLS:                true,
			OAuthClientID:         "client-123",
			OAuthClientSecret:     "shh",
			OAuthTokenEndpointURL: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		}
		got := buildKcatAuthFlags(sasl, "", "")
		if strings.Contains(strings.Join(got, " "), "sasl.oauthbearer.scope=") {
			t.Errorf("scope vazio não deveria gerar flag: %v", got)
		}
	})
}

func TestKafkaValidateOAuthBearerFields(t *testing.T) {
	t.Run("todos os campos presentes", func(t *testing.T) {
		sasl := &KafkaSASLConfig{
			OAuthClientID:         "id",
			OAuthClientSecret:     "secret",
			OAuthTokenEndpointURL: "https://example.com/token",
		}
		if msg := kafkaValidateOAuthBearerFields(sasl); msg != "" {
			t.Errorf("expected no error, got %q", msg)
		}
	})

	t.Run("client_id faltando", func(t *testing.T) {
		sasl := &KafkaSASLConfig{OAuthClientSecret: "secret", OAuthTokenEndpointURL: "https://example.com/token"}
		if msg := kafkaValidateOAuthBearerFields(sasl); msg == "" {
			t.Error("expected validation error, got none")
		}
	})

	t.Run("token_endpoint_url faltando", func(t *testing.T) {
		sasl := &KafkaSASLConfig{OAuthClientID: "id", OAuthClientSecret: "secret"}
		if msg := kafkaValidateOAuthBearerFields(sasl); msg == "" {
			t.Error("expected validation error, got none")
		}
	})
}

func TestBuildKafkaOffsetQueryArgsMulti(t *testing.T) {
	topics := []kafkaTopicPartitions{
		{Topic: "orders", Partitions: 2},
		{Topic: "payments", Partitions: 1},
	}
	got := buildKafkaOffsetQueryArgsMulti(topics, -1)
	want := []string{"-Q", "-t", "orders:0:-1", "-t", "orders:1:-1", "-t", "payments:0:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseKafkaOffsetLinesWithTopic(t *testing.T) {
	raw := "orders [0] offset 105\norders [1] offset 42\npayments [0] offset 7\n"
	got := parseKafkaOffsetLinesWithTopic(raw)
	want := map[string]int64{
		"orders\x000":   105,
		"orders\x001":   42,
		"payments\x000": 7,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
