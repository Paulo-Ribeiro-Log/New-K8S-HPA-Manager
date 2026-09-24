package handlers

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Antes, falhas de az/aws/gcloud chegavam ao usuário só como "exit status 1".
func TestSnatCLIError_IncluiStderrSemWarnings(t *testing.T) {
	ctx := context.Background()
	_, err := exec.CommandContext(ctx, "sh", "-c",
		`echo "WARNING: The behavior of this command has been altered by the following extension: aks-preview" >&2; echo "ERROR: AADSTS700082: The refresh token has expired" >&2; exit 1`).Output()
	if err == nil {
		t.Fatal("esperava erro do comando")
	}
	msg := snatCLIError(ctx, err).Error()
	if !strings.Contains(msg, "AADSTS700082") || strings.Contains(msg, "aks-preview") {
		t.Fatalf("mensagem inesperada: %q", msg)
	}
}
