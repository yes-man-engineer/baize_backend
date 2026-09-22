package logger

import (
	"go.uber.org/zap"
)

var log *zap.Logger

// Init 初始化全局 logger，dev 为 true 时输出可读格式。
func Init(dev bool) {
	var err error
	if dev {
		log, err = zap.NewDevelopment()
	} else {
		log, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
}

func L() *zap.Logger {
	if log == nil {
		Init(true)
	}
	return log
}

func Info(msg string, fields ...zap.Field)  { L().Info(msg, fields...) }
func Warn(msg string, fields ...zap.Field)  { L().Warn(msg, fields...) }
func Error(msg string, fields ...zap.Field) { L().Error(msg, fields...) }

func Sync() { _ = L().Sync() }
