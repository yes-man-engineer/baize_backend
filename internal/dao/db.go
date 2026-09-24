// Package dao 是数据库的唯一出入口。
//
// 连接是包级的，进程起来时 Init 一次，之后谁都不用再传。
// 想读写数据库只能调这个包里的公开函数，*gorm.DB 不外泄。
package dao

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

var db *gorm.DB

// Init 建立 MySQL 连接并自动建表，在 main 里调一次。
func Init(dsn string, dev bool) error {
	level := gormlogger.Silent
	if dev {
		level = gormlogger.Info
	}

	conn, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(level),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	if err := conn.AutoMigrate(&model.Project{}, &model.Message{}); err != nil {
		return fmt.Errorf("自动建表失败: %w", err)
	}

	db = conn
	return nil
}
