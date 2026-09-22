package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

type PlanItemRepo struct{ db *gorm.DB }

func NewPlanItemRepo(db *gorm.DB) *PlanItemRepo { return &PlanItemRepo{db: db} }

// ReplaceAll 重新生成方案时整体替换，保证条目和对话同步。
func (r *PlanItemRepo) ReplaceAll(ctx context.Context, projectID string, items []model.PlanItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&model.PlanItem{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		return tx.Create(&items).Error
	})
}

func (r *PlanItemRepo) ListByProject(ctx context.Context, projectID string) ([]model.PlanItem, error) {
	var list []model.PlanItem
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("sort_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *PlanItemRepo) GetInProject(ctx context.Context, projectID, itemID string) (*model.PlanItem, error) {
	var item model.PlanItem
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND id = ?", projectID, itemID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PlanItemRepo) Save(ctx context.Context, item *model.PlanItem) error {
	return r.db.WithContext(ctx).Save(item).Error
}
