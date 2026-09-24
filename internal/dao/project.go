package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

// ErrNotFound 仓储层统一的「查不到」，上层据此返回 404。
var ErrNotFound = errors.New("记录不存在")

func CreateProject(ctx context.Context, p *model.Project) error {
	return db.WithContext(ctx).Create(p).Error
}

func GetProject(ctx context.Context, id string) (*model.Project, error) {
	var p model.Project
	err := db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func SaveProject(ctx context.Context, p *model.Project) error {
	return db.WithContext(ctx).Save(p).Error
}
