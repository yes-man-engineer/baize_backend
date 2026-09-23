package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/internal/config"
	"github.com/yes-man-engineer/baize_backend/internal/handler"
	"github.com/yes-man-engineer/baize_backend/internal/repository"
	"github.com/yes-man-engineer/baize_backend/internal/router"
	"github.com/yes-man-engineer/baize_backend/internal/service"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.IsDev())
	defer logger.Sync()

	db, err := repository.NewDB(cfg.DSN(), cfg.IsDev())
	if err != nil {
		logger.Error("数据库初始化失败", zap.Error(err))
		os.Exit(1)
	}

	if cfg.LLMAPIKey == "" {
		logger.Warn("LLM_API_KEY 为空，涉及模型的接口会直接报错")
	}

	projectSvc := service.NewProjectService(
		repository.NewProjectRepo(db),
		repository.NewMessageRepo(db),
	)

	r := router.New(cfg.IsDev(), cfg.CORSOrigins, handler.NewProjectHandler(projectSvc))

	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("服务启动", zap.String("port", cfg.AppPort), zap.String("env", cfg.AppEnv))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("服务异常退出", zap.Error(err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("收到退出信号，开始关闭")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("关闭超时", zap.Error(err))
	}
}
