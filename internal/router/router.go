package router

import (
	"github.com/gin-gonic/gin"

	"github.com/yes-man-engineer/baize_backend/internal/handler"
	"github.com/yes-man-engineer/baize_backend/internal/middleware"
)

func New(dev bool, corsOrigins []string, h *handler.ProjectHandler) *gin.Engine {
	if !dev {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.Logger(), middleware.CORS(corsOrigins))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.POST("/projects", h.Start)
		api.GET("/projects/:token", h.Detail)
	}

	return r
}
