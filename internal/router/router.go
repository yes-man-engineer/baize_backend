package router

import (
	"github.com/gin-gonic/gin"

	"github.com/yes-man-engineer/baize_backend/internal/handler"
	"github.com/yes-man-engineer/baize_backend/internal/middleware"
	"github.com/yes-man-engineer/baize_backend/internal/service"
)

func New(dev bool, corsOrigins []string) *gin.Engine {
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
		api.POST("/projects", handler.Decorate(service.StartProject))
		api.GET("/projects/:project_id", handler.Decorate(service.GetDetail))
		api.POST("/projects/:project_id/messages", handler.DecorateStream(service.Reply))
	}

	return r
}
