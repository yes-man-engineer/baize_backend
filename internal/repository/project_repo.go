package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

var ErrNotFound = errors.New("记录不存在")

type ProjectRepo struct{ db *gorm.DB }

func NewProjectRepo(db *gorm.DB) *ProjectRepo { return &ProjectRepo{db: db} }

func (r *ProjectRepo) Create(ctx context.Context, p *model.Project) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *ProjectRepo) GetByToken(ctx context.Context, token string) (*model.Project, error) {
	var p model.Project
	err := r.db.WithContext(ctx).Where("token = ?", token).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProjectRepo) Save(ctx context.Context, p *model.Project) error {
	return r.db.WithContext(ctx).Save(p).Error
}
