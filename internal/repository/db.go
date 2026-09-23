package repository

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

// NewDB 建立 MySQL 连接并自动建表。
func NewDB(dsn string, dev bool) (*gorm.DB, error) {
	level := gormlogger.Silent
	if dev {
		level = gormlogger.Info
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(level),
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	if err := db.AutoMigrate(&model.Project{}, &model.Message{}); err != nil {
		return nil, fmt.Errorf("自动建表失败: %w", err)
	}

	return db, nil
}
