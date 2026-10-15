// Package azlogin diz se o login do Azure CLI (`az login`) ainda vale e identifica, na saída de
// um comando `az` que falhou, quando o motivo é login expirado.
//
// Por que não dá para saber antes: o access token do `az` dura ~1h e fica em cache; o refresh
// token expira por política do tenant (Conditional Access / sign-in frequency), revogação ou troca
// de senha — nada disso está no disco. `az account show` só lê o perfil local e funciona mesmo com
// o login expirado. A única prova é tentar emitir um token (`az account get-access-token`), que
// renova pela rede quando o access token em cache venceu.
//
// O servidor nunca roda `az login` sozinho: numa requisição web não há ninguém no terminal do
// servidor para completar o login. Quem detecta o login expirado devolve ErrLoginRequired, as
// rotas respondem com o código AZ_LOGIN_REQUIRED e o frontend mostra o aviso.
package azlogin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrLoginRequired: o `az` precisa de novo login. O texto é exibido na UI e o frontend também o
// reconhece pela frase "az login" (lib/api/client.ts, evento "azure-login-required").
var ErrLoginRequired = errors.New("o login do Azure expirou — rode `az login` no terminal onde o servidor está rodando")

// Code é o código de erro das respostas HTTP quando err é ErrLoginRequired.
const Code = "AZ_LOGIN_REQUIRED"

type State string

const (
	StateValid         State = "valid"          // emitiu um token agora
	StateLoginRequired State = "login_required" // o Azure AD recusou: precisa de `az login`
	StateUnknown       State = "unknown"        // timeout ou erro de rede — NÃO é "válido"
	StateNotInstalled  State = "not_installed"  // não há `az` no PATH (ambiente só EKS/GKE)
)

type Status struct {
	State     State      `json:"state"`
	ExpiresOn *time.Time `json:"expires_on,omitempty"` // fim do access token atual (renovado sozinho enquanto o login valer)
	Detail    string     `json:"detail,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

const (
	checkTimeout = 20 * time.Second // cold start do az no WSL (~1–2 s) + renovação pela VPN
	cacheTTL     = 60 * time.Second
	maxDetail    = 500
)

// runAz é trocado nos testes.
var runAz = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "az", args...).Output()
}

var (
	mu     sync.Mutex // serializa a checagem: chamadas simultâneas reaproveitam o resultado
	cached *Status
)

// Check devolve o estado do login, com cache de 60 s (force ignora o cache).
func Check(ctx context.Context, force bool) Status {
	mu.Lock()
	defer mu.Unlock()
	if !force && cached != nil && time.Since(cached.CheckedAt) < cacheTTL {
		return *cached
	}
	st := check(ctx)
	cached = &st
	return st
}

func check(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	now := time.Now()
	out, err := runAz(ctx, "account", "get-access-token",
		"--query", "{epoch:expires_on,local:expiresOn}", "--output", "tsv", "--only-show-errors")
	if err != nil {
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return Status{State: StateNotInstalled, Detail: "Azure CLI (az) não encontrado no PATH", CheckedAt: now}
		case ctx.Err() == context.DeadlineExceeded:
			return Status{State: StateUnknown, Detail: fmt.Sprintf("o az não respondeu em %s", checkTimeout), CheckedAt: now}
		}
		stderr := stderrOf(err)
		if IsLoginRequiredOutput(stderr) {
			return Status{State: StateLoginRequired, Detail: trim(stderr), CheckedAt: now}
		}
		return Status{State: StateUnknown, Detail: trim(firstNonEmpty(stderr, err.Error())), CheckedAt: now}
	}
	st := Status{State: StateValid, CheckedAt: now}
	if exp, ok := parseExpiry(string(out)); ok {
		st.ExpiresOn = &exp
	}
	return st
}

// MarkLoginRequired registra no cache que um comando real falhou por login expirado, para o
// status (e o aviso da UI) refletir isso sem esperar a próxima checagem.
func MarkLoginRequired(detail string) {
	mu.Lock()
	defer mu.Unlock()
	cached = &Status{State: StateLoginRequired, Detail: trim(detail), CheckedAt: time.Now()}
}

// Marcas de "precisa de az login" na saída do az/MSAL. "AADSTS" cobre os erros do Azure AD
// (70043 refresh token expirado por sign-in frequency, 50173/50078/50076, 700082...).
var loginRequiredMarkers = []string{
	"AADSTS",
	"az login",
	"interaction_required",
	"invalid_grant",
	"TokenCreatedWithOutdatedPolicies", // CAE: sessão revogada
	"refresh token has expired",
}

// IsLoginRequiredOutput diz se a saída de erro de um comando az indica login expirado/ausente.
func IsLoginRequiredOutput(out string) bool {
	for _, m := range loginRequiredMarkers {
		if strings.Contains(out, m) {
			return true
		}
	}
	return false
}

// Classify devolve ErrLoginRequired (com a saída do az como detalhe) quando a saída indica login
// expirado; senão devolve err como veio. Use no caminho de erro de qualquer comando `az`.
func Classify(err error, output string) error {
	if err == nil {
		return nil
	}
	if IsLoginRequiredOutput(output) || IsLoginRequiredOutput(stderrOf(err)) {
		detail := trim(firstNonEmpty(strings.TrimSpace(output), stderrOf(err)))
		MarkLoginRequired(detail)
		return fmt.Errorf("%w\n%s", ErrLoginRequired, detail)
	}
	return err
}

// parseExpiry lê "<epoch>\t<data local>" (az >= 2.54 tem expires_on em epoch; antes só expiresOn
// no fuso local, "2006-01-02 15:04:05.000000").
func parseExpiry(out string) (time.Time, bool) {
	fields := strings.Split(strings.Trim(out, "\r\n "), "\t") // não TrimSpace: o \t inicial separa um epoch vazio
	if len(fields) > 0 {
		if sec, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64); err == nil && sec > 0 {
			return time.Unix(sec, 0), true
		}
	}
	if len(fields) > 1 {
		for _, layout := range []string{"2006-01-02 15:04:05.000000", "2006-01-02 15:04:05"} {
			if t, err := time.ParseInLocation(layout, strings.TrimSpace(fields[1]), time.Local); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func stderrOf(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxDetail {
		return s[:maxDetail] + "…"
	}
	return s
}
