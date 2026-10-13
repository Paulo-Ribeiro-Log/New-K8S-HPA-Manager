package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/auth"
	"k8s-hpa-manager/internal/history"
)

// O Change History tira o autor daqui: sem isso, 65% das entradas ficavam sem usuário.
func TestJWTMiddlewaresPublishIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jm := auth.NewJWTManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	token, err := jm.Generate("ana@corp", "Ana Silva", true)
	if err != nil {
		t.Fatal(err)
	}

	for name, mw := range map[string]gin.HandlerFunc{
		"header":    JWTAuthMiddleware(jm, ""),
		"websocket": WebSocketJWTAuthMiddleware(jm, ""),
	} {
		var fromGin, fromReq history.UserInfo
		r := gin.New()
		r.GET("/x", mw, func(c *gin.Context) {
			fromGin = history.UserFromContext(c)
			fromReq = history.UserFromContext(c.Request.Context())
		})
		req := httptest.NewRequest(http.MethodGet, "/x?token="+token, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", name, w.Code)
		}
		want := history.UserInfo{Email: "ana@corp", Name: "Ana Silva"}
		if fromGin != want || fromReq != want {
			t.Errorf("%s: gin=%+v request=%+v, quer %+v", name, fromGin, fromReq, want)
		}
	}
}
