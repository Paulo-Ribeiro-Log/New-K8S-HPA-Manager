package handlers

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// Os headers da requisição trazem o JWT (Authorization) e cookies de sessão: nunca podem ir para o
// log do servidor, nem em nível Debug (o zerolog usa Debug por padrão).
func TestCompareReleasesNaoLogaHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.DebugLevel)
	h := NewGitHubReleasesHandler(nil, nil, nil, &logger, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer SEGREDO-JWT-123")
	c.Request.Header.Set("Cookie", "sessao=SEGREDO-COOKIE-456")
	c.Params = gin.Params{
		{Key: "owner", Value: "org"},
		{Key: "repo", Value: "app"},
		{Key: "basehead", Value: "formato-invalido"}, // responde 400 sem precisar de client do GitHub
	}

	h.CompareReleases(c)

	if rec.Code != 400 {
		t.Fatalf("status = %d, quer 400", rec.Code)
	}
	out := buf.String()
	for _, segredo := range []string{"SEGREDO-JWT-123", "SEGREDO-COOKIE-456", "Authorization", "Cookie"} {
		if strings.Contains(out, segredo) {
			t.Errorf("o log contém %q:\n%s", segredo, out)
		}
	}
}
