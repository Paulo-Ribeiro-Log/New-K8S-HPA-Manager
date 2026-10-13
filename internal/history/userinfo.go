package history

import (
	"context"

	"github.com/gin-gonic/gin"
)

// UserInfo contém informações do usuário para auditoria
type UserInfo struct {
	Email string
	Name  string
}

type userCtxKey struct{}

// WithUser devolve um ctx que carrega o autor das operações. O middleware JWT coloca o usuário
// do token no c.Request.Context() — assim código fora dos handlers (ex: kubernetes.Client) que só
// recebe ctx também grava o autor no histórico.
func WithUser(ctx context.Context, u UserInfo) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

// DetachedUserContext é um ctx sem prazo/cancelamento que só carrega o autor de ctx — para
// goroutines que gravam no histórico depois que a requisição já terminou.
func DetachedUserContext(ctx context.Context) context.Context {
	return WithUser(context.Background(), UserFromContext(ctx))
}

// UserFromContext lê o autor de um ctx: chave própria (WithUser) ou, num *gin.Context, as chaves
// "user_email"/"user_name" setadas pelo middleware. Sem usuário, devolve vazio.
func UserFromContext(ctx context.Context) UserInfo {
	if ctx == nil {
		return UserInfo{}
	}
	if u, ok := ctx.Value(userCtxKey{}).(UserInfo); ok && u.Email != "" {
		return u
	}
	if c, ok := ctx.(*gin.Context); ok {
		if c.Request != nil {
			if u, ok := c.Request.Context().Value(userCtxKey{}).(UserInfo); ok && u.Email != "" {
				return u
			}
		}
		var u UserInfo
		u.Email = c.GetString("user_email")
		u.Name = c.GetString("user_name")
		return u
	}
	return UserInfo{}
}

// GetCurrentUserInfo obtém o usuário da requisição (claims do JWT, via middleware). Antes caía
// no `az account show` sem timeout quando o contexto não tinha e-mail — travava a operação
// auditada e gravava a conta do az, não quem fez o login.
func GetCurrentUserInfo(c *gin.Context) UserInfo {
	return UserFromContext(c)
}
