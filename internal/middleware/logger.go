package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("cost", time.Since(start)),
		)
	}
}

// Recovery 兜底 panic，避免单个请求打挂进程。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		logger.Error("panic", zap.Any("error", err), zap.String("path", c.Request.URL.Path))
		c.AbortWithStatusJSON(500, gin.H{"code": 50000, "message": "internal error"})
	})
}
