package dao

import (
	"context"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

func CreateMessage(ctx context.Context, m *model.Message) error {
	return db.WithContext(ctx).Create(m).Error
}

// ListMessages 按时间正序返回整段对话，拼给模型看的就是这个顺序。
func ListMessages(ctx context.Context, projectID string) ([]model.Message, error) {
	var out []model.Message
	err := db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at asc, id asc").
		Find(&out).Error
	return out, err
}
