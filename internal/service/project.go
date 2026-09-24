// Package service 是业务逻辑，产品规则真正落地的地方。
//
// 这里的函数只认业务参数和业务响应，不碰 HTTP、不碰 gin，
// 也不接数据库连接，读写数据库调 dao 的公开函数。
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/yes-man-engineer/baize_backend/internal/dao"
	"github.com/yes-man-engineer/baize_backend/internal/model"
)

var ErrEmptyMessage = errors.New("第一句话不能为空")

// titleRunes 标题截断长度，按字符不按字节，否则中文会被截半个。
const titleRunes = 24

type StartProjectReq struct {
	Content string `json:"content"`
}

type GetDetailReq struct {
	ProjectID string `json:"project_id"`
}

// Detail 是聊天页刷新时要的全部东西。
type Detail struct {
	Project  *model.Project  `json:"project"`
	Messages []model.Message `json:"messages"`
}

// StartProject 用第一条消息开一个项目。
//
// 这里一次模型都不调：用户点完发送要立刻跳进聊天页，等模型回话是下一个接口的事。
// 标题也因此只能从他这句话里截，不然页面标题栏会先空着再跳字。
func StartProject(ctx context.Context, req StartProjectReq) (*Detail, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrEmptyMessage
	}

	project := &model.Project{
		Status: model.StatusChatting,
		Title:  truncate(content, titleRunes),
		Facts:  model.Facts{},
	}
	if err := dao.CreateProject(ctx, project); err != nil {
		return nil, err
	}

	first := &model.Message{
		ProjectID: project.ID,
		Role:      model.RoleUser,
		Content:   content,
	}
	if err := dao.CreateMessage(ctx, first); err != nil {
		return nil, err
	}

	return &Detail{Project: project, Messages: []model.Message{*first}}, nil
}

func GetDetail(ctx context.Context, req GetDetailReq) (*Detail, error) {
	project, err := dao.GetProject(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	messages, err := dao.ListMessages(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	return &Detail{Project: project, Messages: messages}, nil
}

// truncate 按字符截断，超长补省略号。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
