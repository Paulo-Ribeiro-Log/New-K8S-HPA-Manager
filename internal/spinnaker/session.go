package spinnaker

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// sessionCacheTTL — quanto tempo reaproveitar uma sessão (cookie SESSION) sem relogar. TTL do
// cookie do Spring Security nunca foi medido de verdade (item em aberto no plano, seção 6) —
// 20min é conservador o suficiente pra evitar logins repetidos numa sequência de chamadas do
// batch endpoint (Fase 2), mas curto o bastante pra não arriscar usar uma sessão morta por
// muito tempo. Self-healing via ErrSessionExpired (client.go) cobre o caso de expirar antes.
const sessionCacheTTL = 20 * time.Minute

type sessionCacheEntry struct {
	client     *Client
	loggedInAt time.Time
}

var (
	sessionCacheMu sync.Mutex
	sessionCache   = make(map[string]*sessionCacheEntry) // chave: gateURL
)

// GetSessionClient devolve um *Client autenticado pro gateURL informado, reaproveitando sessão
// em cache quando ainda válida (por gateURL — não há credencial por usuário logado nesta app,
// só o Perfil SSO único do servidor, ver seção 3 do plano). Faz login (ou relogin) sob demanda.
func GetSessionClient(ctx context.Context, gateURL, ssoProfileDir, loginIdentifier string) (*Client, error) {
	sessionCacheMu.Lock()
	if entry, ok := sessionCache[gateURL]; ok && time.Since(entry.loggedInAt) < sessionCacheTTL {
		sessionCacheMu.Unlock()
		return entry.client, nil
	}
	sessionCacheMu.Unlock()

	return relogin(ctx, gateURL, ssoProfileDir, loginIdentifier)
}

// InvalidateSession remove a sessão em cache pro gateURL — chamar quando uma chamada autenticada
// falhar com ErrSessionExpired, antes de tentar de novo.
func InvalidateSession(gateURL string) {
	sessionCacheMu.Lock()
	delete(sessionCache, gateURL)
	sessionCacheMu.Unlock()
}

func relogin(ctx context.Context, gateURL, ssoProfileDir, loginIdentifier string) (*Client, error) {
	client, err := NewClient(gateURL)
	if err != nil {
		return nil, fmt.Errorf("criar cliente Spinnaker: %w", err)
	}
	if err := client.Login(ctx, ssoProfileDir, loginIdentifier); err != nil {
		return nil, fmt.Errorf("login no Spinnaker (%s): %w", gateURL, err)
	}

	sessionCacheMu.Lock()
	sessionCache[gateURL] = &sessionCacheEntry{client: client, loggedInAt: time.Now()}
	sessionCacheMu.Unlock()

	return client, nil
}

// WithSession executa fn com um cliente autenticado, tentando relogar automaticamente uma vez
// se fn devolver ErrSessionExpired (mesmo padrão de self-healing já documentado no CLAUDE.md
// pra roundtrippers EKS/GKE desta app).
func WithSession(ctx context.Context, gateURL, ssoProfileDir, loginIdentifier string, fn func(*Client) error) error {
	client, err := GetSessionClient(ctx, gateURL, ssoProfileDir, loginIdentifier)
	if err != nil {
		return err
	}

	err = fn(client)
	if err == nil {
		return nil
	}
	if err != ErrSessionExpired {
		return err
	}

	InvalidateSession(gateURL)
	client, err = relogin(ctx, gateURL, ssoProfileDir, loginIdentifier)
	if err != nil {
		return err
	}
	return fn(client)
}
