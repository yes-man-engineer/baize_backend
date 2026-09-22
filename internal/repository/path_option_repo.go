package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

type PathOptionRepo struct{ db *gorm.DB }

func NewPathOptionRepo(db *gorm.DB) *PathOptionRepo { return &PathOptionRepo{db: db} }

// ReplaceAll 重新生成候选路径时整体替换。
func (r *PathOptionRepo) ReplaceAll(ctx context.Context, projectID string, paths []model.PathOption) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&model.PathOption{}).Error; err != nil {
			return err
		}
		if len(paths) == 0 {
			return nil
		}
		return tx.Create(&paths).Error
	})
}

func (r *PathOptionRepo) ListByProject(ctx context.Context, projectID string) ([]model.PathOption, error) {
	var list []model.PathOption
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("sort_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *PathOptionRepo) GetInProject(ctx context.Context, projectID, pathID string) (*model.PathOption, error) {
	var p model.PathOption
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND id = ?", projectID, pathID).
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
