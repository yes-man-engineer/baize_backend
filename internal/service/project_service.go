// Package service 编排业务流程，是产品规则真正落地的地方。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/yes-man-engineer/baize_backend/internal/model"
	"github.com/yes-man-engineer/baize_backend/internal/repository"
)

var ErrEmptyMessage = errors.New("第一句话不能为空")

const (
	// tokenBytes 16 字节 → 32 位十六进制。没有账号体系，这串就是访问凭证，
	// 必须用 crypto/rand，猜得到就等于别人能翻你的项目。
	tokenBytes = 16

	// titleRunes 标题截断长度，按字符不按字节，否则中文会被截半个。
	titleRunes = 24
)

type ProjectService struct {
	projects *repository.ProjectRepo
	messages *repository.MessageRepo
}

func NewProjectService(p *repository.ProjectRepo, m *repository.MessageRepo) *ProjectService {
	return &ProjectService{projects: p, messages: m}
}

// Detail 是聊天页刷新时要的全部东西。
type Detail struct {
	Project  *model.Project  `json:"project"`
	Messages []model.Message `json:"messages"`
}

// Start 用第一条消息开一个项目。
//
// 这里一次模型都不调：用户点完发送要立刻跳进聊天页，等模型回话是下一个接口的事。
// 标题也因此只能从他这句话里截，不然页面标题栏会先空着再跳字。
func (s *ProjectService) Start(ctx context.Context, content string) (*Detail, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrEmptyMessage
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}

	project := &model.Project{
		Token:  token,
		Status: model.StatusChatting,
		Title:  truncate(content, titleRunes),
		Facts:  model.Facts{},
	}
	if err := s.projects.Create(ctx, project); err != nil {
		return nil, err
	}

	first := &model.Message{
		ProjectID: project.ID,
		Role:      model.RoleUser,
		Content:   content,
	}
	if err := s.messages.Create(ctx, first); err != nil {
		return nil, err
	}

	return &Detail{Project: project, Messages: []model.Message{*first}}, nil
}

func (s *ProjectService) Detail(ctx context.Context, token string) (*Detail, error) {
	project, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}

	messages, err := s.messages.ListByProject(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	return &Detail{Project: project, Messages: messages}, nil
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// truncate 按字符截断，超长补省略号。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
