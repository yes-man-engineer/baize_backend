package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨域放行。
// allowed 为空（本地开发）时放开全部来源；配了就只认白名单里的 Origin。
// 上域名前务必在 .env 里配 CORS_ORIGINS；
// 如果前端由 nginx 同源反代，这个中间件可以整个去掉。
func CORS(allowed []string) gin.HandlerFunc {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o = strings.TrimSpace(o); o != "" {
			set[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		switch {
		case len(set) == 0:
			c.Header("Access-Control-Allow-Origin", "*")
		case set[origin]:
			c.Header("Access-Control-Allow-Origin", origin)
			// 同一个 URL 对不同 Origin 返回不同的头，不加 Vary 会被缓存串台。
			c.Header("Vary", "Origin")
		}
		// 白名单模式下，不在名单里的 Origin 不发这个头，浏览器自己会拦。

		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
