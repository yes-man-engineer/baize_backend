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
	"github.com/yes-man-engineer/baize_backend/internal/dao"
	"github.com/yes-man-engineer/baize_backend/internal/llm"
	"github.com/yes-man-engineer/baize_backend/internal/router"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.IsDev())
	defer logger.Sync()

	if err := initFrame(cfg); err != nil {
		logger.Error("[main] 初始化失败", zap.Error(err))
		logger.Sync()
		os.Exit(1)
	}

	r := router.New(cfg.IsDev(), cfg.CORSOrigins)

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

// initFrame 把进程级的连接都建起来，只报错不决定进程死活，退不退由 main 定。
func initFrame(cfg *config.Config) error {
	if err := dao.Init(cfg.DSN(), cfg.IsDev()); err != nil {
		return err
	}

	// 没有 key 照样能起，只是涉及模型的接口会报错。
	// 本地调别的接口时不该被这个挡住。
	if cfg.LLMAPIKey == "" {
		logger.Warn("LLM_API_KEY 为空，涉及模型的接口会直接报错")
	}

	llm.Init(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		cfg.LLMTemperature,
		time.Duration(cfg.LLMTimeoutSeconds)*time.Second,
	)

	return nil
}
