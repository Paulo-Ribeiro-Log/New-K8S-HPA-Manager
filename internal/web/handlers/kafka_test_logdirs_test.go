package handlers

import (
	"strings"
	"testing"
)

func TestEscapeJaasPropertyValue(t *testing.T) {
	cases := map[string]string{
		`simple`:         `simple`,
		`with"quote`:     `with\"quote`,
		`with\backslash`: `with\\backslash`,
		`a\"b`:           `a\\\"b`,
	}
	for in, want := range cases {
		if got := escapeJaasPropertyValue(in); got != want {
			t.Errorf("escapeJaasPropertyValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildKafkaClientPropertiesFile(t *testing.T) {
	t.Run("nil sasl", func(t *testing.T) {
		if got := buildKafkaClientPropertiesFile(nil, "", ""); got != "" {
			t.Errorf("expected empty string for nil sasl, got %q", got)
		}
	})

	t.Run("plain with creds, no tls", func(t *testing.T) {
		sasl := &KafkaSASLConfig{Mechanism: "PLAIN"}
		got := buildKafkaClientPropertiesFile(sasl, "alice", `p"ss`)
		if !strings.Contains(got, "security.protocol=SASL_PLAINTEXT") {
			t.Errorf("missing SASL_PLAINTEXT protocol: %q", got)
		}
		if !strings.Contains(got, "sasl.mechanism=PLAIN") {
			t.Errorf("missing sasl.mechanism=PLAIN: %q", got)
		}
		if !strings.Contains(got, `username="alice" password="p\"ss"`) {
			t.Errorf("missing escaped JAAS config: %q", got)
		}
		if !strings.Contains(got, "PlainLoginModule") {
			t.Errorf("missing PlainLoginModule: %q", got)
		}
	})

	t.Run("scram with tls and skip verify", func(t *testing.T) {
		sasl := &KafkaSASLConfig{Mechanism: "SCRAM-SHA-512", UseTLS: true, SkipTLSVerify: true}
		got := buildKafkaClientPropertiesFile(sasl, "bob", "secret")
		if !strings.Contains(got, "security.protocol=SASL_SSL") {
			t.Errorf("missing SASL_SSL protocol: %q", got)
		}
		if !strings.Contains(got, "ScramLoginModule") {
			t.Errorf("missing ScramLoginModule: %q", got)
		}
		if !strings.Contains(got, "ssl.endpoint.identification.algorithm=") {
			t.Errorf("missing hostname verification disable: %q", got)
		}
	})

	t.Run("tls only, no creds", func(t *testing.T) {
		sasl := &KafkaSASLConfig{UseTLS: true}
		got := buildKafkaClientPropertiesFile(sasl, "", "")
		if !strings.Contains(got, "security.protocol=SSL") {
			t.Errorf("missing SSL protocol: %q", got)
		}
		if strings.Contains(got, "sasl.mechanism=") {
			t.Errorf("should not have sasl.mechanism without creds: %q", got)
		}
	})

	t.Run("no tls, no creds", func(t *testing.T) {
		sasl := &KafkaSASLConfig{}
		got := buildKafkaClientPropertiesFile(sasl, "", "")
		if !strings.Contains(got, "security.protocol=PLAINTEXT") {
			t.Errorf("missing PLAINTEXT protocol: %q", got)
		}
	})
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"simple", `{"a":1}`, `{"a":1}`},
		{"with prefix line", "Querying brokers for log directories information\n{\"a\":1}", `{"a":1}`},
		{"nested", `noise {"a":{"b":2}} trailing`, `{"a":{"b":2}}`},
		{"no json", "no json here", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractJSONObject(c.raw); got != c.want {
				t.Errorf("extractJSONObject(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestParseKafkaLogDirsOutput(t *testing.T) {
	raw := `Querying brokers for log directories information
{"version":1,"brokers":[
  {"broker":0,"logDirs":[{"logDir":"/data","error":null,"partitions":[
    {"partition":"orders-0","size":1000,"offsetLag":0,"isFuture":false},
    {"partition":"orders-1","size":2000,"offsetLag":0,"isFuture":false},
    {"partition":"payments-0","size":500,"offsetLag":0,"isFuture":false}
  ]}]},
  {"broker":1,"logDirs":[{"logDir":"/data","error":null,"partitions":[
    {"partition":"orders-0","size":999,"offsetLag":0,"isFuture":false},
    {"partition":"orders-1","size":2000,"offsetLag":0,"isFuture":false}
  ]}]}
]}`
	got := parseKafkaLogDirsOutput(raw)
	// orders: max(1000,999) + max(2000,2000) = 1000 + 2000 = 3000
	if got["orders"] != 3000 {
		t.Errorf("orders = %d, want 3000", got["orders"])
	}
	if got["payments"] != 500 {
		t.Errorf("payments = %d, want 500", got["payments"])
	}
}

func TestParseKafkaLogDirsOutputSkipsErroredLogDir(t *testing.T) {
	raw := `{"version":1,"brokers":[{"broker":0,"logDirs":[{"logDir":"/data","error":"kafka.common.KafkaStorageException","partitions":[{"partition":"orders-0","size":1000,"offsetLag":0,"isFuture":false}]}]}]}`
	got := parseKafkaLogDirsOutput(raw)
	if len(got) != 0 {
		t.Errorf("expected no topics from errored logDir, got %v", got)
	}
}

func TestParseKafkaLogDirsOutputInvalidJSON(t *testing.T) {
	if got := parseKafkaLogDirsOutput("not json at all"); got != nil {
		t.Errorf("expected nil for invalid input, got %v", got)
	}
}

func TestBuildKafkaLogDirsScript(t *testing.T) {
	sasl := &KafkaSASLConfig{Mechanism: "PLAIN"}
	script := buildKafkaLogDirsScript("broker:9092", sasl, "alice", "secret", []string{"orders", "payments"})
	if !strings.Contains(script, "kafka-log-dirs") {
		t.Errorf("script missing kafka-log-dirs: %q", script)
	}
	if !strings.Contains(script, "base64 -d") {
		t.Errorf("script missing base64 decode: %q", script)
	}
	if !strings.Contains(script, "--command-config /tmp/kafka-client.properties") {
		t.Errorf("script missing --command-config: %q", script)
	}
}
