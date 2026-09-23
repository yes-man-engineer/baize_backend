package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

type MessageRepo struct{ db *gorm.DB }

func NewMessageRepo(db *gorm.DB) *MessageRepo { return &MessageRepo{db: db} }

func (r *MessageRepo) Create(ctx context.Context, m *model.Message) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// ListByProject 按时间正序返回整段对话，拼给模型看的就是这个顺序。
func (r *MessageRepo) ListByProject(ctx context.Context, projectID string) ([]model.Message, error) {
	var out []model.Message
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at asc, id asc").
		Find(&out).Error
	return out, err
}
