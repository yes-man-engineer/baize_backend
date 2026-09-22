package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func serve(t *testing.T, allowed []string, origin, method string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORS(allowed))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(method, "/x", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORS(t *testing.T) {
	const allow = "Access-Control-Allow-Origin"

	t.Run("没配白名单时放开全部（本地开发）", func(t *testing.T) {
		w := serve(t, nil, "https://anything.com", http.MethodGet)
		if got := w.Header().Get(allow); got != "*" {
			t.Errorf("%s = %q, 期望 *", allow, got)
		}
	})

	t.Run("白名单内的来源按原样回显", func(t *testing.T) {
		w := serve(t, []string{"https://baize.com"}, "https://baize.com", http.MethodGet)
		if got := w.Header().Get(allow); got != "https://baize.com" {
			t.Errorf("%s = %q, 期望回显来源", allow, got)
		}
		if w.Header().Get("Vary") != "Origin" {
			t.Error("缺 Vary: Origin，响应会被缓存串台")
		}
	})

	t.Run("白名单外的来源不发头", func(t *testing.T) {
		w := serve(t, []string{"https://baize.com"}, "https://evil.com", http.MethodGet)
		if got := w.Header().Get(allow); got != "" {
			t.Errorf("%s = %q, 白名单外不该发这个头", allow, got)
		}
	})

	t.Run("预检直接 204", func(t *testing.T) {
		w := serve(t, []string{"https://baize.com"}, "https://baize.com", http.MethodOptions)
		if w.Code != http.StatusNoContent {
			t.Errorf("OPTIONS = %d, 期望 204", w.Code)
		}
	})
}
