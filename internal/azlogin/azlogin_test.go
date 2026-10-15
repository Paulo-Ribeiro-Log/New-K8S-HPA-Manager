package azlogin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// exitErr gera um *exec.ExitError real com o stderr dado.
func exitErr(t *testing.T, stderr string) error {
	t.Helper()
	_, err := exec.Command("sh", "-c", "exit 1").Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("esperava ExitError, veio %v", err)
	}
	ee.Stderr = []byte(stderr)
	return ee
}

func withAz(t *testing.T, fn func(ctx context.Context, args ...string) ([]byte, error)) {
	t.Helper()
	old := runAz
	runAz = fn
	mu.Lock()
	cached = nil
	mu.Unlock()
	t.Cleanup(func() {
		runAz = old
		mu.Lock()
		cached = nil
		mu.Unlock()
	})
}

func TestCheckStates(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  func(t *testing.T) error
		want State
	}{
		{"válido com epoch", "1792080000\t2026-10-15 13:00:00.000000\n", nil, StateValid},
		{"refresh token expirado", "", func(t *testing.T) error {
			return exitErr(t, "ERROR: AADSTS70043: The refresh token has expired or is invalid due to sign-in frequency checks by conditional access.\nInteractive authentication is needed. Please run:\naz login --scope https://management.core.windows.net//.default")
		}, StateLoginRequired},
		{"nunca logado", "", func(t *testing.T) error { return exitErr(t, "ERROR: Please run 'az login' to setup account.") }, StateLoginRequired},
		{"erro de rede não é válido", "", func(t *testing.T) error {
			return exitErr(t, "ERROR: HTTPSConnectionPool(host='login.microsoftonline.com'): Max retries exceeded")
		}, StateUnknown},
		{"az ausente", "", func(t *testing.T) error { return &exec.Error{Name: "az", Err: exec.ErrNotFound} }, StateNotInstalled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAz(t, func(ctx context.Context, args ...string) ([]byte, error) {
				if tc.err != nil {
					return nil, tc.err(t)
				}
				return []byte(tc.out), nil
			})
			st := Check(context.Background(), true)
			if st.State != tc.want {
				t.Fatalf("estado %q, quer %q (detalhe %q)", st.State, tc.want, st.Detail)
			}
			if tc.want == StateValid && (st.ExpiresOn == nil || st.ExpiresOn.Unix() != 1792080000) {
				t.Errorf("expires_on = %v", st.ExpiresOn)
			}
		})
	}
}

// Timeout do az (renovação lenta pela VPN) vira "unknown" — antes a validação devolvia sucesso.
func TestCheckTimeoutIsUnknown(t *testing.T) {
	withAz(t, func(ctx context.Context, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if st := Check(ctx, true); st.State != StateUnknown {
		t.Fatalf("estado %q, quer unknown", st.State)
	}
}

func TestCheckCache(t *testing.T) {
	calls := 0
	withAz(t, func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		return []byte("1792080000\t\n"), nil
	})
	Check(context.Background(), false)
	Check(context.Background(), false)
	if calls != 1 {
		t.Fatalf("az chamado %d vezes, quer 1 (cache)", calls)
	}
	Check(context.Background(), true)
	if calls != 2 {
		t.Fatalf("force não ignorou o cache: %d chamadas", calls)
	}
}

func TestParseExpiryLocalFallback(t *testing.T) {
	got, ok := parseExpiry("\t2026-10-15 13:00:00.000000")
	want := time.Date(2026, 10, 15, 13, 0, 0, 0, time.Local)
	if !ok || !got.Equal(want) {
		t.Fatalf("parseExpiry = %v, %v; quer %v", got, ok, want)
	}
}

func TestClassify(t *testing.T) {
	withAz(t, func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil })
	base := fmt.Errorf("exit status 1")

	err := Classify(base, "ERROR: AADSTS700082: The refresh token has expired due to inactivity.")
	if !errors.Is(err, ErrLoginRequired) || !strings.Contains(err.Error(), "AADSTS700082") {
		t.Fatalf("Classify = %v; quer ErrLoginRequired com o detalhe", err)
	}
	if cached == nil || cached.State != StateLoginRequired {
		t.Errorf("Classify deveria marcar o cache como login_required")
	}

	if err := Classify(base, "ERROR: (ResourceNotFound) The Resource was not found"); err != base {
		t.Errorf("erro comum não deveria mudar: %v", err)
	}
	if Classify(nil, "AADSTS") != nil {
		t.Error("Classify(nil) deveria ser nil")
	}
}
