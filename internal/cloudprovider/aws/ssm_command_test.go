package aws

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"caminho simples", "/etc/nginx/ssl/tls.crt", "'/etc/nginx/ssl/tls.crt'"},
		{"vazio", "", "''"},
		{"com aspa simples embutida", "it's/a/path", `'it'\''s/a/path'`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShellQuote(tt.input); got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, esperado %q", tt.input, got, tt.want)
			}
		})
	}
}

// installFakeAWSCLI escreve um script `aws` fake no PATH (via um diretório temp prependado), que
// simula o ciclo real de `ssm send-command` + `ssm get-command-invocation` — inclusive a janela
// de inconsistência eventual (InvocationDoesNotExist nas primeiras N consultas, antes de resolver
// pro status terminal pedido). Usa um arquivo contador em disco porque cada invocação do fake é um
// processo NOVO (sem estado em memória compartilhado entre chamadas, igual ao aws CLI real).
func installFakeAWSCLI(t *testing.T, pendingPolls int, finalStatus, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	counterFile := filepath.Join(dir, "poll-counter")
	if err := os.WriteFile(counterFile, []byte("0"), 0o644); err != nil {
		t.Fatalf("criar counter file: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$2" = "send-command" ]; then
  echo '{"Command":{"CommandId":"cmd-fake-123"}}'
  exit 0
fi
if [ "$2" = "get-command-invocation" ]; then
  count=$(cat %q)
  count=$((count + 1))
  echo "$count" > %q
  if [ "$count" -le %d ]; then
    echo "An error occurred (InvocationDoesNotExist)" >&2
    exit 254
  fi
  printf '{"Status":"%s","StandardOutputContent":"%s","StandardErrorContent":"%s"}'
  exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`, counterFile, counterFile, pendingPolls, finalStatus, stdout, stderr)

	scriptPath := filepath.Join(dir, "aws")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("escrever fake aws: %v", err)
	}

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("setar PATH: %v", err)
	}
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
}

func TestRunShellCommand_Sucesso(t *testing.T) {
	installFakeAWSCLI(t, 0, "Success", "TRANSFER_OK", "")

	result, err := RunShellCommand(context.Background(), "test-profile", "us-east-1", "i-12345", []string{"echo TRANSFER_OK"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if result.Status != "Success" {
		t.Errorf("Status = %q, esperado Success", result.Status)
	}
	if result.StandardOutputContent != "TRANSFER_OK" {
		t.Errorf("StandardOutputContent = %q, esperado TRANSFER_OK", result.StandardOutputContent)
	}
}

func TestRunShellCommand_ToleraInconsistenciaEventualAntesDeResolver(t *testing.T) {
	oldInterval := ssmCommandPollInterval
	ssmCommandPollInterval = 10 * time.Millisecond
	defer func() { ssmCommandPollInterval = oldInterval }()

	// 3 consultas retornam InvocationDoesNotExist antes de resolver — confirma que o polling
	// não desiste na primeira falha (a inconsistência eventual real do AWS é justamente isso).
	installFakeAWSCLI(t, 3, "Success", "ok", "")

	result, err := RunShellCommand(context.Background(), "test-profile", "us-east-1", "i-12345", []string{"echo ok"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if result.Status != "Success" {
		t.Errorf("Status = %q, esperado Success", result.Status)
	}
}

func TestRunShellCommand_StatusFailedNaoViraErroGo(t *testing.T) {
	// Um comando que RODOU e terminou com Failed não é um erro de infraestrutura (RunShellCommand
	// em si funcionou perfeitamente) — quem decide o que fazer com Status=Failed é o CHAMADOR
	// (handler HTTP), não esta função, que só reporta o resultado terminal como ele é.
	installFakeAWSCLI(t, 0, "Failed", "", "cat: /etc/nginx/ssl/tls.crt: No such file or directory")

	result, err := RunShellCommand(context.Background(), "test-profile", "us-east-1", "i-12345", []string{"cat /etc/nginx/ssl/tls.crt"})
	if err != nil {
		t.Fatalf("erro inesperado (Status=Failed não deveria virar erro Go): %v", err)
	}
	if result.Status != "Failed" {
		t.Errorf("Status = %q, esperado Failed", result.Status)
	}
	if result.StandardErrorContent == "" {
		t.Error("esperava StandardErrorContent preenchido com o motivo real da falha")
	}
}

func TestRunShellCommand_TimeoutQuandoNuncaResolve(t *testing.T) {
	oldInterval, oldTimeout := ssmCommandPollInterval, ssmCommandPollTimeout
	ssmCommandPollInterval = 5 * time.Millisecond
	ssmCommandPollTimeout = 30 * time.Millisecond
	defer func() { ssmCommandPollInterval, ssmCommandPollTimeout = oldInterval, oldTimeout }()

	// pendingPolls muito maior que o tempo total disponível — nunca chega a resolver antes do
	// deadline encolhido.
	installFakeAWSCLI(t, 1000, "Success", "ok", "")

	_, err := RunShellCommand(context.Background(), "test-profile", "us-east-1", "i-12345", []string{"echo ok"})
	if err == nil {
		t.Fatal("esperava erro de timeout, mas RunShellCommand retornou sucesso")
	}
}
